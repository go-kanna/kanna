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
// Load errors reported by go/packages are returned as error diagnostics. Any
// structs that could still be resolved are returned alongside them, so callers
// should check the diagnostics before using the result.
func Structs(pkgs []*packages.Package) ([]ir.Struct, []diag.Diag) {
	var (
		structs []ir.Struct
		diags   []diag.Diag
	)

	for _, pkg := range pkgs {
		if pkg == nil {
			continue
		}
		ss, ds := structsInPackage(pkg)
		structs = append(structs, ss...)
		diags = append(diags, ds...)
	}

	return structs, diags
}

func structsInPackage(pkg *packages.Package) ([]ir.Struct, []diag.Diag) {
	var diags []diag.Diag
	for _, e := range pkg.Errors {
		diags = append(diags, diag.Errorf(token.Position{}, "%s", e))
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
		if file == nil || !IsGenerated(file) {
			continue
		}
		generated[pkg.Fset.Position(file.Pos()).Filename] = true
	}
	return generated
}

// docComments maps each declared type name to the raw comment lines attached to
// its declaration. A comment on the enclosing GenDecl is used when the TypeSpec
// itself carries none, which is how a grouped declaration documents its types.
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
				if cg == nil {
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

// IsGenerated reports whether file carries the canonical
// "Code generated ... DO NOT EDIT." comment before its package clause, per
// https://golang.org/s/generatedcode.
func IsGenerated(file *ast.File) bool {
	if file == nil || len(file.Comments) == 0 {
		return false
	}

	first := file.Comments[0]
	if first == nil || first.End() > file.Package {
		return false
	}

	for _, c := range first.List {
		if c == nil {
			continue
		}
		line := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
		if strings.HasPrefix(line, "Code generated") && strings.HasSuffix(line, "DO NOT EDIT.") {
			return true
		}
	}

	return false
}

// ResolveTypeExpr evaluates a textual type expression in the scope of pkg. The
// expression may reference any name visible from the package, including its
// imports, so callers are responsible for ensuring the expression uses names
// that package can see.
func ResolveTypeExpr(pkg *packages.Package, expr string) (types.Type, error) {
	if pkg.Types == nil || pkg.Fset == nil {
		return nil, errors.New("package types or fset unavailable")
	}

	tv, err := types.Eval(pkg.Fset, pkg.Types, token.NoPos, expr)
	if err != nil {
		return nil, fmt.Errorf("types.Eval: %w", err)
	}
	if !tv.IsType() {
		return nil, errors.New("expression is not a type")
	}

	return tv.Type, nil
}

func positionOf(pkg *packages.Package, pos token.Pos) token.Position {
	if pkg.Fset == nil {
		return token.Position{}
	}
	return pkg.Fset.Position(pos)
}
