package di_test

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"github.com/go-kanna/kanna/internal/gen/di"
)

func TestImports_RecordsTheAliasOwnPackage(t *testing.T) {
	t.Parallel()

	// os.FileInfo is an alias for io/fs.FileInfo. types.TypeString prints it as
	// os.FileInfo, so "os" is the package the output has to import — unaliasing
	// first would record io/fs and leave the generated file with "undefined: os".
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "t.go", `package app

import "os"

type Holder struct {
	FI os.FileInfo
}
`, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	conf := &types.Config{Importer: importer.Default()}
	pkg, err := conf.Check("app", fset, []*ast.File{f}, &types.Info{})
	if err != nil {
		t.Fatalf("check: %v", err)
	}

	holder, ok := pkg.Scope().Lookup("Holder").Type().Underlying().(*types.Struct)
	if !ok {
		t.Fatal("Holder is not a struct")
	}
	aliased := holder.Field(0).Type()

	im := di.NewImports("app")
	im.AddType(aliased)

	entries := im.Sorted()
	if len(entries) != 1 || entries[0].Path != "os" {
		t.Fatalf("imports = %v, want a single entry for os", entries)
	}
	if got, want := im.QualifyType(aliased), "os.FileInfo"; got != want {
		t.Errorf("QualifyType = %q, want %q", got, want)
	}
}

func TestImports_QualifyProvider_SamePackage(t *testing.T) {
	t.Parallel()

	im := di.NewImports("github.com/me/app")
	p := &di.Provider{PkgPath: "github.com/me/app", PkgName: "app", FuncName: "NewFoo"}
	if got := im.QualifyProvider(p); got != "NewFoo" {
		t.Errorf("Qualify = %q, want NewFoo", got)
	}
	if entries := im.Sorted(); len(entries) != 0 {
		t.Errorf("expected no imports, got %v", entries)
	}
}

func TestImports_QualifyProvider_DifferentPackage(t *testing.T) {
	t.Parallel()

	im := di.NewImports("github.com/me/app")
	p := &di.Provider{PkgPath: "github.com/me/foo", PkgName: "foo", FuncName: "NewBar"}
	im.AddProvider(p)

	if got := im.QualifyProvider(p); got != "foo.NewBar" {
		t.Errorf("Qualify = %q, want foo.NewBar", got)
	}

	es := im.Sorted()
	if len(es) != 1 || es[0].Path != "github.com/me/foo" || es[0].Alias != "foo" {
		t.Errorf("Sorted = %+v", es)
	}
}

func TestImports_AliasCollision(t *testing.T) {
	t.Parallel()

	im := di.NewImports("github.com/me/app")
	p1 := &di.Provider{PkgPath: "github.com/x/foo", PkgName: "foo", FuncName: "A"}
	p2 := &di.Provider{PkgPath: "github.com/y/foo", PkgName: "foo", FuncName: "B"}

	im.AddProvider(p1)
	im.AddProvider(p2)

	if got := im.QualifyProvider(p1); got != "foo.A" {
		t.Errorf("Qualify(p1) = %q, want foo.A", got)
	}
	if got := im.QualifyProvider(p2); got != "foo2.B" {
		t.Errorf("Qualify(p2) = %q, want foo2.B", got)
	}

	es := im.Sorted()
	if len(es) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(es))
	}
	if es[0].Path != "github.com/x/foo" || es[0].Alias != "foo" {
		t.Errorf("entry[0] = %+v", es[0])
	}
	if es[1].Path != "github.com/y/foo" || es[1].Alias != "foo2" {
		t.Errorf("entry[1] = %+v, want foo2", es[1])
	}
}

func TestImports_ReservedAlias(t *testing.T) {
	t.Parallel()

	im := di.NewImports("github.com/me/app", "log")
	p := &di.Provider{PkgPath: "github.com/me/log", PkgName: "log", FuncName: "Setup"}
	im.AddProvider(p)
	if got := im.QualifyProvider(p); got != "log2.Setup" {
		t.Errorf("Qualify = %q, want log2.Setup", got)
	}
}

func TestImports_AddTypeRecordsPackage(t *testing.T) {
	t.Parallel()

	// Type-check a small package that imports another package whose type
	// we'll feed into Imports.AddType.
	src := `package myapp

import "io"

type Foo struct {
	R io.Reader
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Defs:  make(map[*ast.Ident]types.Object),
		Uses:  make(map[*ast.Ident]types.Object),
	}
	pkg, err := (&types.Config{Importer: importer.Default()}).Check("myapp", fset, []*ast.File{f}, info)
	if err != nil {
		t.Fatalf("check: %v", err)
	}

	// Locate io.Reader through the field type of Foo.
	fooObj := pkg.Scope().Lookup("Foo")
	if fooObj == nil {
		t.Fatal("Foo not found")
	}
	st, ok := fooObj.Type().Underlying().(*types.Struct)
	if !ok {
		t.Fatalf("Foo is not a struct, got %T", fooObj.Type().Underlying())
	}
	if st.NumFields() == 0 {
		t.Fatal("Foo has no fields")
	}
	readerType := st.Field(0).Type() // io.Reader

	im := di.NewImports("myapp")
	im.AddType(readerType)

	es := im.Sorted()
	if len(es) != 1 || es[0].Path != "io" {
		t.Errorf("Sorted = %+v, want [io]", es)
	}

	if got := im.QualifyType(readerType); got != "io.Reader" {
		t.Errorf("QualifyType = %q, want io.Reader", got)
	}
}

func TestImports_AddType_AnonymousInterfaceWalksMethods(t *testing.T) {
	t.Parallel()

	// Anonymous interface that references io via a method signature.
	src := `package myapp
import "io"
type _ io.Reader
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Defs:  make(map[*ast.Ident]types.Object),
		Uses:  make(map[*ast.Ident]types.Object),
	}
	pkg, err := (&types.Config{Importer: importer.Default()}).Check("myapp", fset, []*ast.File{f}, info)
	if err != nil {
		t.Fatalf("check: %v", err)
	}

	tv, err := types.Eval(fset, pkg, f.Pos(), "interface{ Read(io.Reader) error }")
	if err != nil {
		t.Fatalf("eval: %v", err)
	}

	im := di.NewImports("myapp")
	im.AddType(tv.Type)

	found := false
	for _, e := range im.Sorted() {
		if e.Path == "io" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected io import via anonymous interface method, got %+v", im.Sorted())
	}
}

func TestImports_AddType_AnonymousStructWalksFields(t *testing.T) {
	t.Parallel()

	src := `package myapp
import "io"
type _ io.Reader
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Defs:  make(map[*ast.Ident]types.Object),
		Uses:  make(map[*ast.Ident]types.Object),
	}
	pkg, err := (&types.Config{Importer: importer.Default()}).Check("myapp", fset, []*ast.File{f}, info)
	if err != nil {
		t.Fatalf("check: %v", err)
	}

	tv, err := types.Eval(fset, pkg, f.Pos(), "struct{ R io.Reader }")
	if err != nil {
		t.Fatalf("eval: %v", err)
	}

	im := di.NewImports("myapp")
	im.AddType(tv.Type)

	found := false
	for _, e := range im.Sorted() {
		if e.Path == "io" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected io import via anonymous struct field, got %+v", im.Sorted())
	}
}

func TestImports_AddTypeContainerPackageIgnored(t *testing.T) {
	t.Parallel()

	src := `package myapp
type Foo struct{}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Defs:  make(map[*ast.Ident]types.Object),
		Uses:  make(map[*ast.Ident]types.Object),
	}
	pkg, err := (&types.Config{Importer: importer.Default()}).Check("myapp", fset, []*ast.File{f}, info)
	if err != nil {
		t.Fatalf("check: %v", err)
	}

	fooType := pkg.Scope().Lookup("Foo").Type()

	im := di.NewImports("myapp")
	im.AddType(fooType)

	if entries := im.Sorted(); len(entries) != 0 {
		t.Errorf("expected no imports for container's own package, got %v", entries)
	}
	if got := im.QualifyType(fooType); got != "Foo" {
		t.Errorf("QualifyType = %q, want Foo", got)
	}
}
