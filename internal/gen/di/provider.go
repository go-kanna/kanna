package di

import (
	"cmp"
	"go/token"
	"go/types"
	"slices"

	"github.com/go-kanna/kanna/internal/diag"
	"github.com/go-kanna/kanna/internal/packages"
	"github.com/go-kanna/kanna/internal/scan"
)

// Providers scans pkgs for top-level functions that can serve as dependency
// providers.
//
// A provider is a top-level function whose results are one of:
//   - (T)        where T is a named type, a pointer to a named type, or an interface
//   - (T, error) the same shape plus a trailing error
//
// Functions with any other shape are skipped without a diagnostic; a package is
// free to hold functions that have nothing to do with injection.
//
// Unlike Containers, this does not skip generated files. A constructor emitted
// by an earlier run is an ordinary top-level function, and downstream containers
// legitimately resolve against it — skipping generated files would hide exactly
// the constructors this generator produced.
func Providers(pkgs []*packages.Package) ([]Provider, []diag.Diag) {
	var (
		providers []Provider
		diags     []diag.Diag
	)

	for _, pkg := range scan.DedupePackages(pkgs) {
		ps, ds := providersInPackage(pkg)
		providers = append(providers, ps...)
		diags = append(diags, ds...)
	}

	return providers, diags
}

func providersInPackage(pkg *packages.Package) ([]Provider, []diag.Diag) {
	if pkg.Types == nil {
		return nil, []diag.Diag{
			diag.Errorf(token.Position{}, "package %s: no type information", pkg.PkgPath),
		}
	}

	scope := pkg.Types.Scope()

	var providers []Provider
	for _, name := range scope.Names() {
		fn, ok := scope.Lookup(name).(*types.Func)
		if !ok {
			continue
		}

		sig, ok := fn.Type().(*types.Signature)
		if !ok {
			continue
		}

		result, returnsError, ok := providerResult(sig)
		if !ok {
			continue
		}

		providers = append(providers, Provider{
			PkgPath:      pkg.PkgPath,
			PkgName:      pkg.Name,
			FuncName:     name,
			Result:       result,
			Params:       paramTypes(sig),
			ReturnsError: returnsError,
			Pos:          positionOf(pkg, fn.Pos()),
		})
	}

	// scope.Names() is sorted alphabetically; restore declaration order to match
	// what scan reports for structs.
	slices.SortStableFunc(providers, func(a, b Provider) int {
		if c := cmp.Compare(a.Pos.Filename, b.Pos.Filename); c != 0 {
			return c
		}
		return cmp.Compare(a.Pos.Offset, b.Pos.Offset)
	})

	return providers, nil
}

// providerResult reports the value a signature provides, whether it also returns
// an error, and whether the signature has a provider shape at all.
func providerResult(sig *types.Signature) (result types.Type, returnsError, ok bool) {
	// Methods never reach here — a package scope holds only top-level
	// declarations — but a signature with a receiver is not a provider either.
	if sig.Recv() != nil {
		return nil, false, false
	}

	results := sig.Results()
	if results.Len() != 1 && results.Len() != 2 {
		return nil, false, false
	}

	first := results.At(0).Type()
	if isBuiltinError(first) || !isProviderResultType(first) {
		return nil, false, false
	}

	if results.Len() == 2 {
		// The second result must be exactly error.
		if !isBuiltinError(results.At(1).Type()) {
			return nil, false, false
		}
		return first, true, true
	}

	return first, false, true
}

// paramTypes extracts the parameter types of a signature in declaration order.
func paramTypes(sig *types.Signature) []types.Type {
	params := sig.Params()
	if params.Len() == 0 {
		return nil
	}

	out := make([]types.Type, 0, params.Len())
	for v := range params.Variables() {
		if v == nil {
			continue
		}
		out = append(out, v.Type())
	}
	return out
}

// isProviderResultType reports whether t is a valid first result for a provider:
// a named type, a pointer to a named type, or an interface. Aliases are resolved
// first so that a provider returning an alias is still recognized.
func isProviderResultType(t types.Type) bool {
	t = types.Unalias(t)
	return isNamedOrPtrToNamed(t) || isInterfaceType(t)
}

func isInterfaceType(t types.Type) bool {
	switch tt := t.(type) {
	case *types.Interface:
		return true
	case *types.Named:
		_, ok := tt.Underlying().(*types.Interface)
		return ok
	default:
		return false
	}
}

func isNamedOrPtrToNamed(t types.Type) bool {
	switch tt := t.(type) {
	case *types.Named:
		return true
	case *types.Pointer:
		_, ok := types.Unalias(tt.Elem()).(*types.Named)
		return ok
	default:
		return false
	}
}

func isBuiltinError(t types.Type) bool {
	obj := types.Universe.Lookup("error")
	if obj == nil {
		return false
	}
	return types.Identical(t, obj.Type())
}

func positionOf(pkg *packages.Package, pos token.Pos) token.Position {
	if pkg.Fset == nil {
		return token.Position{}
	}
	return pkg.Fset.Position(pos)
}
