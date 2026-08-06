// Package scan extracts a generator-agnostic model of the struct types declared
// in a set of loaded packages.
//
// Scan deliberately applies no filtering beyond skipping generated files. The
// generators disagree on what is relevant — some opt in through a struct tag,
// others take a whole package and exclude by directive — so any filter placed
// here would skew the model toward one of them.
package scan

import (
	"cmp"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/go-kanna/kanna/internal/diag"
	"github.com/go-kanna/kanna/internal/ir"
	"github.com/go-kanna/kanna/internal/packages"
)

// Structs returns every struct type declared in pkgs, in declaration order.
//
// Files bearing the canonical "Code generated ... DO NOT EDIT." marker are
// skipped so that a generator never reads its own output. Everything else is
// reported: unexported types, unexported fields, embedded fields, and generic
// declarations. Type aliases are excluded because they name a type declared
// elsewhere rather than a new struct.
//
// A single import path can be loaded more than once — with Tests enabled,
// go/packages returns both a package and its in-package test variant under the
// same PkgPath — so a struct already reported for an import path is not reported
// again. Packages are visited in ID order to keep which variant wins stable.
//
// Load errors reported by go/packages are returned as error diagnostics. Any
// structs that could still be resolved are returned alongside them, so callers
// should check the diagnostics before using the result.
func Structs(pkgs []*packages.Package) ([]ir.Struct, []diag.Diag) {
	ordered := make([]*packages.Package, 0, len(pkgs))
	for _, pkg := range pkgs {
		if pkg != nil {
			ordered = append(ordered, pkg)
		}
	}
	slices.SortStableFunc(ordered, func(a, b *packages.Package) int {
		return cmp.Compare(a.ID, b.ID)
	})

	var (
		structs []ir.Struct
		diags   []diag.Diag
	)
	seen := make(map[string]bool)

	for _, pkg := range ordered {
		ss, ds := structsInPackage(pkg)
		diags = append(diags, ds...)

		for _, s := range ss {
			key := s.PkgPath + "." + s.Name
			if seen[key] {
				continue
			}
			seen[key] = true
			structs = append(structs, s)
		}
	}

	return structs, diags
}

func structsInPackage(pkg *packages.Package) ([]ir.Struct, []diag.Diag) {
	var diags []diag.Diag
	for _, e := range pkg.Errors {
		diags = append(diags, diag.Errorf(errorPosition(e), "%s", e.Msg))
	}

	if pkg.Types == nil {
		return nil, append(diags, diag.Errorf(token.Position{}, "package %s: no type information", pkg.PkgPath))
	}

	generated := generatedFiles(pkg)
	docs := docComments(pkg.Syntax)
	scope := pkg.Types.Scope()

	var structs []ir.Struct
	for _, name := range scope.Names() {
		obj, ok := scope.Lookup(name).(*types.TypeName)
		if !ok || obj.IsAlias() {
			continue
		}

		named, ok := obj.Type().(*types.Named)
		if !ok {
			continue
		}

		st, ok := named.Underlying().(*types.Struct)
		if !ok {
			continue
		}

		pos := positionOf(pkg, obj.Pos())
		if generated[pos.Filename] {
			continue
		}

		structs = append(structs, ir.Struct{
			PkgPath: pkg.PkgPath,
			PkgName: pkg.Name,
			Name:    name,
			Named:   named,
			Pos:     pos,
			Doc:     docs[name],
			Fields:  fieldsOf(pkg, st),
		})
	}

	// scope.Names() is sorted alphabetically; restore declaration order so the
	// model reflects the source. Callers that want another order impose it.
	slices.SortStableFunc(structs, func(a, b ir.Struct) int {
		if c := cmp.Compare(a.Pos.Filename, b.Pos.Filename); c != 0 {
			return c
		}
		return cmp.Compare(a.Pos.Offset, b.Pos.Offset)
	})

	return structs, diags
}

// fieldsOf converts the fields of st, preserving declaration order.
func fieldsOf(pkg *packages.Package, st *types.Struct) []ir.Field {
	fields := make([]ir.Field, 0, st.NumFields())
	for i := range st.NumFields() {
		f := st.Field(i)
		fields = append(fields, ir.Field{
			Name:     f.Name(),
			Type:     f.Type(),
			Tag:      reflect.StructTag(st.Tag(i)),
			Pos:      positionOf(pkg, f.Pos()),
			Exported: f.Exported(),
			Embedded: f.Embedded(),
		})
	}
	return fields
}

// generatedFiles returns the set of filenames in pkg that carry the generated
// code marker.
func generatedFiles(pkg *packages.Package) map[string]bool {
	if pkg.Fset == nil {
		return nil
	}

	generated := make(map[string]bool)
	for _, file := range pkg.Syntax {
		if file == nil || !ast.IsGenerated(file) {
			continue
		}
		generated[pkg.Fset.Position(file.Pos()).Filename] = true
	}
	return generated
}

// docComments maps each declared type name to the raw comment lines attached to
// its declaration.
//
// A comment on the enclosing GenDecl stands in only for a declaration holding a
// single TypeSpec. In a grouped declaration the GenDecl comment documents the
// group as a whole, so inheriting it would hand every type in the group the same
// directive.
func docComments(files []*ast.File) map[string][]string {
	docs := make(map[string][]string)

	for _, file := range files {
		if file == nil {
			continue
		}
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || ts.Name == nil {
					continue
				}
				cg := ts.Doc
				if cg == nil && len(gd.Specs) == 1 {
					cg = gd.Doc
				}
				if lines := commentLines(cg); len(lines) > 0 {
					docs[ts.Name.Name] = lines
				}
			}
		}
	}

	return docs
}

func commentLines(cg *ast.CommentGroup) []string {
	if cg == nil {
		return nil
	}
	lines := make([]string, 0, len(cg.List))
	for _, c := range cg.List {
		if c == nil {
			continue
		}
		lines = append(lines, c.Text)
	}
	return lines
}

// ResolveTypeExpr evaluates a textual type expression as if it appeared at pos
// in pkg.
//
// pos must fall inside one of the package's files. types.Eval resolves names in
// the scope enclosing pos, and only a file scope holds the package's imports, so
// passing token.NoPos evaluates in package scope where every qualified
// expression such as "greeter.Greeter" fails to resolve. The position of the
// declaration carrying the expression is a good choice.
func ResolveTypeExpr(fset *token.FileSet, pkg *types.Package, pos token.Pos, expr string) (types.Type, error) {
	if fset == nil || pkg == nil {
		return nil, errors.New("file set or package unavailable")
	}

	tv, err := types.Eval(fset, pkg, pos, expr)
	if err != nil {
		return nil, fmt.Errorf("types.Eval: %w", err)
	}
	if !tv.IsType() {
		return nil, errors.New("expression is not a type")
	}

	return tv.Type, nil
}

// errorPosition converts the position that go/packages records on a load error,
// so the diagnostic keeps pointing at the offending source instead of nowhere.
//
// packages.Error.Pos is formatted by token.Position.String, i.e.
// "file:line:col". Splitting from the right keeps a Windows drive letter in the
// filename.
func errorPosition(e packages.Error) token.Position {
	if e.Pos == "" {
		return token.Position{}
	}

	parts := strings.Split(e.Pos, ":")
	if len(parts) < 3 {
		return token.Position{Filename: e.Pos}
	}

	line, lineErr := strconv.Atoi(parts[len(parts)-2])
	col, colErr := strconv.Atoi(parts[len(parts)-1])
	if lineErr != nil || colErr != nil {
		return token.Position{Filename: e.Pos}
	}

	return token.Position{
		Filename: strings.Join(parts[:len(parts)-2], ":"),
		Line:     line,
		Column:   col,
	}
}

func positionOf(pkg *packages.Package, pos token.Pos) token.Position {
	if pkg.Fset == nil {
		return token.Position{}
	}
	return pkg.Fset.Position(pos)
}
