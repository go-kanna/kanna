// Package pkgtest provides helpers for tests that need a loaded package.
// Nothing here is part of a public API.
package pkgtest

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/go-kanna/kanna/internal/packages"
)

// defaultPkgName is the package name and import path given to a loaded package
// when the caller does not care which one it is.
const defaultPkgName = "test"

// LoadFile type-checks src as a single file named "test.go" in package "test".
func LoadFile(t *testing.T, src string) *packages.Package {
	t.Helper()

	return LoadPackage(t, map[string]string{"test.go": src})
}

// LoadFileAs is LoadFile for a package with the given name, which doubles as its
// import path. Use it when the name appears in what the test asserts on, such as
// generated output.
func LoadFileAs(t *testing.T, pkgName, src string) *packages.Package {
	t.Helper()

	return LoadPackageAs(t, pkgName, map[string]string{"test.go": src})
}

// LoadPackage type-checks the given files and assembles the minimal
// packages.Package that the scan layer needs, which avoids putting a real module
// on disk for every test.
func LoadPackage(t *testing.T, files map[string]string) *packages.Package {
	t.Helper()

	return LoadPackageAs(t, defaultPkgName, files)
}

// MustCompile type-checks files as a single package and fails the test if they
// do not form valid Go. Use it to confirm that generated source compiles against
// the source it was generated from; the offending file is included in the
// failure message.
func MustCompile(t *testing.T, pkgName string, files map[string]string) {
	t.Helper()

	fset := token.NewFileSet()
	syntax := make([]*ast.File, 0, len(files))
	for _, name := range slices.Sorted(maps.Keys(files)) {
		f, err := parser.ParseFile(fset, name, files[name], parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v\n--- %s ---\n%s", name, err, name, files[name])
		}
		syntax = append(syntax, f)
	}

	if _, err := (&types.Config{Importer: importer.Default()}).Check(pkgName, fset, syntax, nil); err != nil {
		var b strings.Builder
		for _, name := range slices.Sorted(maps.Keys(files)) {
			fmt.Fprintf(&b, "\n--- %s ---\n%s", name, files[name])
		}
		t.Fatalf("type-check: %v%s", err, b.String())
	}
}

// LoadPackageAs is LoadPackage for a package with the given name. Files are
// parsed in sorted filename order so that declaration positions are
// deterministic.
func LoadPackageAs(t *testing.T, pkgName string, files map[string]string) *packages.Package {
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
	pkg, err := conf.Check(pkgName, fset, syntax, info)
	if err != nil {
		t.Fatalf("check: %v", err)
	}

	return &packages.Package{
		PkgPath:   pkgName,
		Name:      pkgName,
		Syntax:    syntax,
		Types:     pkg,
		TypesInfo: info,
		Fset:      fset,
	}
}
