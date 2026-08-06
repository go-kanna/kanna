// Package pkgtest provides helpers for tests that need a loaded package.
// Nothing here is part of a public API.
package pkgtest

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"testing"

	"github.com/go-kanna/kanna/internal/packages"
)

// LoadFile type-checks src as a single file named "test.go".
func LoadFile(t *testing.T, src string) *packages.Package {
	t.Helper()

	return LoadPackage(t, map[string]string{"test.go": src})
}

// LoadPackage type-checks the given files and assembles the minimal
// packages.Package that the scan layer needs, which avoids putting a real module
// on disk for every test. Files are parsed in sorted filename order so that
// declaration positions are deterministic.
func LoadPackage(t *testing.T, files map[string]string) *packages.Package {
	t.Helper()

	fset := token.NewFileSet()
	syntax := make([]*ast.File, 0, len(files))
	for _, name := range slices.Sorted(maps.Keys(files)) {
		f, err := parser.ParseFile(fset, name, files[name], parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		syntax = append(syntax, f)
	}

	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Defs:  make(map[*ast.Ident]types.Object),
		Uses:  make(map[*ast.Ident]types.Object),
	}
	conf := &types.Config{Importer: importer.Default()}
	pkg, err := conf.Check("test", fset, syntax, info)
	if err != nil {
		t.Fatalf("check: %v", err)
	}

	return &packages.Package{
		PkgPath:   "test",
		Name:      "test",
		Syntax:    syntax,
		Types:     pkg,
		TypesInfo: info,
		Fset:      fset,
	}
}
