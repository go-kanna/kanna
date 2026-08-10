// Package mapper generates mapping functions between two struct types, wiring
// in the converters a project registers for the field types Go cannot convert
// on its own.
//
// Unlike the other generators, the types to map are named on the command line
// rather than found by scanning. What is scanned is the converter package: the
// mapper.Register calls in it are read statically, and the functions they name
// are called directly by the generated code.
package mapper

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/go-kanna/kanna/internal/packages"
)

// mapperPkgPath is the import path of the registration API scanned for
// converter declarations.
const mapperPkgPath = "github.com/go-kanna/kanna/mapper"

// converter is a conversion function registered via mapper.Register or
// mapper.RegisterE.
type converter struct {
	fn     *types.Func
	src    types.Type
	dst    types.Type
	hasErr bool
	pos    token.Position
}

// converterTable holds registered converters indexed by (src, dst) pair.
// Lookup uses types.Identical, which handles type aliases transparently.
type converterTable struct {
	converters []converter
}

func (t converterTable) lookup(src, dst types.Type) (converter, bool) {
	for _, c := range t.converters {
		if types.Identical(c.src, src) && types.Identical(c.dst, dst) {
			return c, true
		}
	}
	return converter{}, false
}

func (t *converterTable) add(c converter) error {
	if existing, ok := t.lookup(c.src, c.dst); ok {
		return fmt.Errorf("%s: converter from %s to %s is already registered at %s",
			c.pos, typeLabel(c.src), typeLabel(c.dst), existing.pos)
	}
	t.converters = append(t.converters, c)
	return nil
}

// typeLabel renders a type with package-name qualifiers for error messages.
func typeLabel(t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string { return p.Name() })
}

// extractConverters scans pkgs for mapper.Register and mapper.RegisterE
// calls and builds the converter table used to resolve field conversions.
// outputPkgPath is the package the generated code will live in; converters
// must be callable from it.
func extractConverters(pkgs []*packages.Package, outputPkgPath string) (converterTable, error) {
	var table converterTable
	var errs []error
	for _, pkg := range pkgs {
		// Reading a call means reading what it refers to. Without that the scan
		// cannot say whether a call is a registration, and reporting nothing
		// would look like a package with no converters.
		if pkg.TypesInfo == nil {
			errs = append(errs, fmt.Errorf("package %s was loaded without type information", pkg.PkgPath))
			continue
		}
		for _, file := range pkg.Syntax {
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				c, ok, err := registeredConverter(pkg, call, outputPkgPath)
				if err != nil {
					errs = append(errs, err)
					return true
				}
				if ok {
					if err := table.add(c); err != nil {
						errs = append(errs, err)
					}
				}
				return true
			})
		}
	}
	if len(errs) > 0 {
		return converterTable{}, errors.Join(errs...)
	}
	return table, nil
}

// registeredConverter decodes call as a mapper registration; ok reports
// whether call is one.
func registeredConverter(pkg *packages.Package, call *ast.CallExpr, outputPkgPath string) (converter, bool, error) {
	ident := calleeIdent(call.Fun)
	if ident == nil {
		return converter{}, false, nil
	}
	callee, ok := pkg.TypesInfo.Uses[ident].(*types.Func)
	if !ok || callee.Pkg() == nil || callee.Pkg().Path() != mapperPkgPath {
		return converter{}, false, nil
	}
	if callee.Name() != "Register" && callee.Name() != "RegisterE" {
		return converter{}, false, nil
	}

	pos := pkg.Fset.Position(call.Pos())
	inst, ok := pkg.TypesInfo.Instances[ident]
	if !ok || inst.TypeArgs.Len() != 2 || len(call.Args) != 1 {
		return converter{}, false, fmt.Errorf("%s: cannot determine registered types for %s call", pos, callee.Name())
	}
	fn, err := converterFunc(pkg, call.Args[0], outputPkgPath)
	if err != nil {
		return converter{}, false, err
	}
	return converter{
		fn:     fn,
		src:    types.Unalias(inst.TypeArgs.At(0)),
		dst:    types.Unalias(inst.TypeArgs.At(1)),
		hasErr: callee.Name() == "RegisterE",
		pos:    pos,
	}, true, nil
}

// calleeIdent returns the identifier naming the called function,
// unwrapping qualified identifiers and explicit type arguments.
func calleeIdent(expr ast.Expr) *ast.Ident {
	switch e := expr.(type) {
	case *ast.Ident:
		return e
	case *ast.SelectorExpr:
		return e.Sel
	case *ast.IndexExpr:
		return calleeIdent(e.X)
	case *ast.IndexListExpr:
		return calleeIdent(e.X)
	default:
		return nil
	}
}

// converterFunc validates that arg references a named function callable
// from the output package and returns it.
func converterFunc(pkg *packages.Package, arg ast.Expr, outputPkgPath string) (*types.Func, error) {
	pos := pkg.Fset.Position(arg.Pos())
	var ident *ast.Ident
	switch e := arg.(type) {
	case *ast.Ident:
		ident = e
	case *ast.SelectorExpr:
		ident = e.Sel
	case *ast.IndexExpr, *ast.IndexListExpr:
		return nil, fmt.Errorf("%s: generic converter functions are not supported", pos)
	case *ast.FuncLit:
		return nil, fmt.Errorf("%s: converter must be a named function, not a function literal", pos)
	default:
		return nil, fmt.Errorf("%s: converter must be a reference to a named function", pos)
	}
	fn, ok := pkg.TypesInfo.Uses[ident].(*types.Func)
	if !ok {
		return nil, fmt.Errorf("%s: converter must be a named function", pos)
	}
	if fn.Signature().Recv() != nil {
		return nil, fmt.Errorf("%s: converter must not be a method", pos)
	}
	if fn.Signature().TypeParams().Len() > 0 {
		return nil, fmt.Errorf("%s: generic converter functions are not supported", pos)
	}
	if !fn.Exported() && fn.Pkg() != nil && fn.Pkg().Path() != outputPkgPath {
		return nil, fmt.Errorf("%s: converter %s must be exported to be callable from generated code", pos, fn.Name())
	}
	return fn, nil
}
