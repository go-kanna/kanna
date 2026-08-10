package di

import (
	"go/types"
	"path"

	"github.com/go-kanna/kanna/internal/imports"
)

// Imports tracks the packages an emitted file references and assigns each a
// non-conflicting alias. The container's own package is never included.
//
// Naming is delegated to the shared tracker; what stays here is which packages
// a DI plan references, which is the part no other generator shares.
type Imports struct {
	containerPkg string
	tracker      *imports.Tracker
}

// ImportEntry is a single line in the generated import block.
type ImportEntry = imports.Entry

// NewImports constructs an Imports tracker for an emitted file in containerPkg.
//
// Reserved names are kept away from imports. The generated body declares err and
// v unconditionally, and assignNames keeps step variables clear of import
// aliases; reserving the same names here closes the other half of that, so
// neither side can shadow the other.
func NewImports(containerPkg string, reserved ...string) *Imports {
	tracker := imports.New(containerPkg, nil)
	tracker.Reserve(reserved...)

	return &Imports{
		containerPkg: containerPkg,
		tracker:      tracker,
	}
}

// AddType records the package of every named type referenced by t, recursing
// into pointers, slices, arrays, maps, channels, signatures, and generic type
// arguments.
func (im *Imports) AddType(t types.Type) {
	if t == nil {
		return
	}
	for _, p := range collectPkgs(t) {
		im.addPackage(p)
	}
}

// AddProvider records the provider's declaring package.
func (im *Imports) AddProvider(p *Provider) {
	if p == nil {
		return
	}
	im.addByPath(p.PkgPath, p.PkgName)
}

// QualifyType renders t as it should appear in source code, prefixing types from
// imported packages with their assigned alias and leaving same-package types
// unqualified.
func (im *Imports) QualifyType(t types.Type) string {
	return types.TypeString(t, im.qualify)
}

// QualifyProvider returns the call expression for p (e.g. "foo.NewBar"), or just
// the function name when p lives in the container's package.
func (im *Imports) QualifyProvider(p *Provider) string {
	if p == nil {
		return ""
	}
	if p.PkgPath == "" || p.PkgPath == im.containerPkg {
		return p.FuncName
	}
	alias, ok := im.tracker.Lookup(p.PkgPath)
	if !ok {
		alias = path.Base(p.PkgPath)
	}
	return alias + "." + p.FuncName
}

// Sorted returns one entry per imported package, sorted by path. The container's
// own package is excluded.
func (im *Imports) Sorted() []ImportEntry {
	return im.tracker.Entries()
}

// Taken returns every local name the imports occupy, including the ones
// reserved at construction. assignNames keeps the body's identifiers clear of
// these.
func (im *Imports) Taken() []string {
	return im.tracker.Taken()
}

func (im *Imports) addPackage(p *types.Package) {
	if p == nil {
		return
	}
	im.addByPath(p.Path(), p.Name())
}

func (im *Imports) addByPath(pkgPath, baseName string) {
	im.tracker.Add(pkgPath, baseName)
}

// qualify is the package-qualification function passed to types.TypeString.
//
// It looks up rather than records: by the time anything is rendered the imports
// have already been collected, and a package reached only here is one
// collectPkgs deliberately did not follow. Recording it would add an import the
// output never mentions.
func (im *Imports) qualify(p *types.Package) string {
	if p == nil {
		return ""
	}
	if p.Path() == im.containerPkg {
		return ""
	}
	if alias, ok := im.tracker.Lookup(p.Path()); ok {
		return alias
	}
	return p.Name()
}

// collectPkgs walks t and returns each named-type package it references.
//
// The walk descends through the type shapes that may appear in the generated
// source: pointers, slices/arrays/maps/channels, signature parameters and
// results, named types' type arguments, and the methods/fields of anonymous
// *types.Interface and *types.Struct values (which types.TypeString writes
// verbatim into the output, so their internal references must be imported).
// It deliberately does NOT descend into a *types.Named's Underlying form: only
// the named type itself is qualified in the output, so following its underlying
// methods/fields would record packages that don't appear in the generated source
// and would trip "imported and not used".
func collectPkgs(t types.Type) []*types.Package {
	var out []*types.Package
	seen := map[*types.Package]struct{}{}

	// record notes the package a named or aliased type is written against. The
	// alias is not resolved first: types.TypeString prints the alias by its own
	// name, so that is the package the output has to import.
	record := func(obj *types.TypeName) {
		if obj == nil {
			return
		}
		pkg := obj.Pkg()
		if pkg == nil {
			return
		}
		if _, ok := seen[pkg]; ok {
			return
		}
		seen[pkg] = struct{}{}
		out = append(out, pkg)
	}

	var walk func(types.Type)
	walk = func(t types.Type) {
		if t == nil {
			return
		}
		switch tt := t.(type) {
		case *types.Alias:
			record(tt.Obj())
			for arg := range tt.TypeArgs().Types() {
				walk(arg)
			}
		case *types.Named:
			record(tt.Obj())
			if ta := tt.TypeArgs(); ta != nil {
				for arg := range ta.Types() {
					walk(arg)
				}
			}
		case *types.Pointer:
			walk(tt.Elem())
		case *types.Slice:
			walk(tt.Elem())
		case *types.Array:
			walk(tt.Elem())
		case *types.Map:
			walk(tt.Key())
			walk(tt.Elem())
		case *types.Chan:
			walk(tt.Elem())
		case *types.Signature:
			if params := tt.Params(); params != nil {
				for v := range params.Variables() {
					walk(v.Type())
				}
			}
			if results := tt.Results(); results != nil {
				for v := range results.Variables() {
					walk(v.Type())
				}
			}
		case *types.Interface:
			// Anonymous interface — types.TypeString prints each method
			// signature verbatim, so we must record packages they reference.
			// Named interfaces never reach this case because *types.Named is
			// matched earlier without descending into Underlying.
			for m := range tt.Methods() {
				walk(m.Type())
			}
		case *types.Struct:
			// Anonymous struct — same reasoning as the Interface case.
			for f := range tt.Fields() {
				walk(f.Type())
			}
		}
	}
	walk(t)
	return out
}
