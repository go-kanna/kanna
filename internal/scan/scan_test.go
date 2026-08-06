package scan_test

import (
	"go/token"
	"slices"
	"testing"

	"github.com/go-kanna/kanna/internal/diag"
	"github.com/go-kanna/kanna/internal/internaltest"
	"github.com/go-kanna/kanna/internal/ir"
	"github.com/go-kanna/kanna/internal/packages"
	"github.com/go-kanna/kanna/internal/scan"
)

func TestStructs_ReportsUnexportedTypesAndFields(t *testing.T) {
	t.Parallel()

	pkg := internaltest.LoadFile(t, `package test

type Exported struct {
	Public  string
	private int
}

type unexported struct {
	Public string
}
`)

	structs, ds := scan.Structs([]*packages.Package{pkg})
	assertNoErrors(t, ds)

	if got, want := structNames(structs), []string{"Exported", "unexported"}; !slices.Equal(got, want) {
		t.Fatalf("structs = %v, want %v", got, want)
	}

	fields := structs[0].Fields
	if got, want := len(fields), 2; got != want {
		t.Fatalf("Exported fields = %d, want %d", got, want)
	}
	if fields[0].Name != "Public" || !fields[0].Exported {
		t.Errorf("field 0 = %+v, want exported Public", fields[0])
	}
	if fields[1].Name != "private" || fields[1].Exported {
		t.Errorf("field 1 = %+v, want unexported private", fields[1])
	}
}

func TestStructs_PreservesDeclarationOrder(t *testing.T) {
	t.Parallel()

	// Declared in reverse alphabetical order to prove the result is not simply
	// the alphabetically sorted output of types.Scope.Names.
	pkg := internaltest.LoadFile(t, `package test

type Zebra struct{}
type Mango struct{}
type Apple struct{}
`)

	structs, ds := scan.Structs([]*packages.Package{pkg})
	assertNoErrors(t, ds)

	if got, want := structNames(structs), []string{"Zebra", "Mango", "Apple"}; !slices.Equal(got, want) {
		t.Errorf("structs = %v, want declaration order %v", got, want)
	}
}

func TestStructs_PreservesFieldOrder(t *testing.T) {
	t.Parallel()

	pkg := internaltest.LoadFile(t, `package test

type Row struct {
	Zebra string
	Apple string
	Mango string
}
`)

	structs, ds := scan.Structs([]*packages.Package{pkg})
	assertNoErrors(t, ds)

	names := make([]string, 0, len(structs[0].Fields))
	for _, f := range structs[0].Fields {
		names = append(names, f.Name)
	}
	if want := []string{"Zebra", "Apple", "Mango"}; !slices.Equal(names, want) {
		t.Errorf("fields = %v, want %v", names, want)
	}
}

func TestStructs_ReportsEmbeddedFields(t *testing.T) {
	t.Parallel()

	pkg := internaltest.LoadFile(t, `package test

type Base struct {
	ID string
}

type Child struct {
	Base
	Name string
}
`)

	structs, ds := scan.Structs([]*packages.Package{pkg})
	assertNoErrors(t, ds)

	child := findStruct(t, structs, "Child")
	if got, want := len(child.Fields), 2; got != want {
		t.Fatalf("Child fields = %d, want %d", got, want)
	}

	embedded := child.Fields[0]
	if !embedded.Embedded {
		t.Errorf("field 0 Embedded = false, want true")
	}
	if embedded.Name != "Base" {
		t.Errorf("field 0 Name = %q, want %q (the embedded type name)", embedded.Name, "Base")
	}
	if child.Fields[1].Embedded {
		t.Errorf("field 1 Embedded = true, want false")
	}
}

func TestStructs_ReportsBlankFields(t *testing.T) {
	t.Parallel()

	// Blank fields carry the DI generator's arg/override/returns declarations, so
	// the shared model must not drop them.
	src := `package test

type DB struct{}

type Container struct {
	_    DB ` + "`di:\"arg\"`" + `
	_    DB ` + "`di:\"with=NewDB\"`" + `
	Real DB
}
`
	pkg := internaltest.LoadFile(t, src)

	structs, ds := scan.Structs([]*packages.Package{pkg})
	assertNoErrors(t, ds)

	fields := findStruct(t, structs, "Container").Fields
	if got, want := len(fields), 3; got != want {
		t.Fatalf("fields = %d, want %d", got, want)
	}
	for i := range 2 {
		if fields[i].Name != "_" {
			t.Errorf("field %d Name = %q, want %q", i, fields[i].Name, "_")
		}
		if fields[i].Embedded {
			t.Errorf("field %d Embedded = true, want false", i)
		}
	}
	if got, ok := fields[0].Tag.Lookup("di"); !ok || got != "arg" {
		t.Errorf(`field 0 Tag.Lookup("di") = %q, %v; want %q, true`, got, ok, "arg")
	}
}

func TestStructs_ReportsTags(t *testing.T) {
	t.Parallel()

	src := `package test

type Row struct {
	Name string ` + "`json:\"name\" orm:\"column=name\"`" + `
	Skip string
}
`
	pkg := internaltest.LoadFile(t, src)

	structs, ds := scan.Structs([]*packages.Package{pkg})
	assertNoErrors(t, ds)

	fields := structs[0].Fields
	if got, ok := fields[0].Tag.Lookup("orm"); !ok || got != "column=name" {
		t.Errorf(`Tag.Lookup("orm") = %q, %v; want %q, true`, got, ok, "column=name")
	}
	if got, ok := fields[0].Tag.Lookup("json"); !ok || got != "name" {
		t.Errorf(`Tag.Lookup("json") = %q, %v; want %q, true`, got, ok, "name")
	}
	if _, ok := fields[1].Tag.Lookup("orm"); ok {
		t.Error("untagged field reported a tag value")
	}
}

func TestStructs_ReportsGenericStructs(t *testing.T) {
	t.Parallel()

	pkg := internaltest.LoadFile(t, `package test

type Box[T any] struct {
	Value T
}
`)

	structs, ds := scan.Structs([]*packages.Package{pkg})
	assertNoErrors(t, ds)

	box := findStruct(t, structs, "Box")
	if got := box.Named.TypeParams().Len(); got != 1 {
		t.Errorf("TypeParams().Len() = %d, want 1", got)
	}
}

func TestStructs_ExcludesAliasesAndNonStructTypes(t *testing.T) {
	t.Parallel()

	pkg := internaltest.LoadFile(t, `package test

type Real struct{}

type Alias = Real

type Count int

type Handler func() error

type Reader interface{ Read() error }
`)

	structs, ds := scan.Structs([]*packages.Package{pkg})
	assertNoErrors(t, ds)

	if got, want := structNames(structs), []string{"Real"}; !slices.Equal(got, want) {
		t.Errorf("structs = %v, want %v", got, want)
	}
}

func TestStructs_SkipsGeneratedFiles(t *testing.T) {
	t.Parallel()

	// The marker may sit in its own comment group below a license header or a
	// build constraint, which is how real generated files usually look.
	tests := []struct {
		name   string
		header string
	}{
		{
			name:   "marker alone",
			header: "// Code generated by kanna. DO NOT EDIT.\n",
		},
		{
			name:   "marker in the same group as a copyright line",
			header: "// Copyright 2026\n// Code generated by kanna. DO NOT EDIT.\n",
		},
		{
			name:   "marker in its own group below a copyright line",
			header: "// Copyright 2026\n\n// Code generated by kanna. DO NOT EDIT.\n",
		},
		{
			name:   "marker below a build constraint",
			header: "//go:build !ignore\n\n// Code generated by kanna. DO NOT EDIT.\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pkg := internaltest.LoadPackage(t, map[string]string{
				"a_handwritten.go": "package test\n\ntype Handwritten struct{}\n",
				"b_generated.go":   tt.header + "\npackage test\n\ntype Generated struct{}\n",
			})

			structs, ds := scan.Structs([]*packages.Package{pkg})
			assertNoErrors(t, ds)

			if got, want := structNames(structs), []string{"Handwritten"}; !slices.Equal(got, want) {
				t.Errorf("structs = %v, want %v", got, want)
			}
		})
	}
}

func TestStructs_KeepsHandwrittenFilesMentioningTheMarkerLater(t *testing.T) {
	t.Parallel()

	pkg := internaltest.LoadFile(t, `package test

// Row is handwritten even though the words appear below.
// Code generated by kanna. DO NOT EDIT.
type Row struct{}
`)

	structs, ds := scan.Structs([]*packages.Package{pkg})
	assertNoErrors(t, ds)

	if got, want := structNames(structs), []string{"Row"}; !slices.Equal(got, want) {
		t.Errorf("structs = %v, want %v", got, want)
	}
}

func TestStructs_CollectsDocComments(t *testing.T) {
	t.Parallel()

	pkg := internaltest.LoadFile(t, `package test

// Documented is a struct with its own doc comment.
//kanna:something
type Documented struct{}

// Solo documents a declaration holding one spec.
type (
	Solo struct{}
)

type Undocumented struct{}
`)

	structs, ds := scan.Structs([]*packages.Package{pkg})
	assertNoErrors(t, ds)

	documented := findStruct(t, structs, "Documented")
	if want := []string{
		"// Documented is a struct with its own doc comment.",
		"//kanna:something",
	}; !slices.Equal(documented.Doc, want) {
		t.Errorf("Documented.Doc = %q, want %q", documented.Doc, want)
	}

	// A GenDecl comment stands in when the declaration holds a single spec.
	solo := findStruct(t, structs, "Solo")
	if want := []string{"// Solo documents a declaration holding one spec."}; !slices.Equal(solo.Doc, want) {
		t.Errorf("Solo.Doc = %q, want %q", solo.Doc, want)
	}

	if got := findStruct(t, structs, "Undocumented").Doc; len(got) != 0 {
		t.Errorf("Undocumented.Doc = %q, want empty", got)
	}
}

func TestStructs_DoesNotShareGroupedDocAcrossSpecs(t *testing.T) {
	t.Parallel()

	// A comment on a grouped declaration documents the group, not each type. If
	// it were inherited, one //kanna:container directive would silently apply to
	// every type in the group.
	pkg := internaltest.LoadFile(t, `package test

// These types are grouped.
//kanna:container name=NewX
type (
	First  struct{}
	Second struct{}
)
`)

	structs, ds := scan.Structs([]*packages.Package{pkg})
	assertNoErrors(t, ds)

	for _, name := range []string{"First", "Second"} {
		if got := findStruct(t, structs, name).Doc; len(got) != 0 {
			t.Errorf("%s.Doc = %q, want empty", name, got)
		}
	}
}

func TestStructs_DeduplicatesByImportPath(t *testing.T) {
	t.Parallel()

	// With Tests enabled, go/packages returns a package and its in-package test
	// variant under one PkgPath. Loading the same source twice reproduces that
	// shape: the struct must be reported once.
	src := `package test

type Row struct{}
`
	first := internaltest.LoadFile(t, src)
	second := internaltest.LoadFile(t, src)

	structs, ds := scan.Structs([]*packages.Package{first, second})
	assertNoErrors(t, ds)

	if got, want := structNames(structs), []string{"Row"}; !slices.Equal(got, want) {
		t.Errorf("structs = %v, want %v", got, want)
	}
}

func TestStructs_ReportsPackageMetadata(t *testing.T) {
	t.Parallel()

	pkg := internaltest.LoadFile(t, `package test

type Row struct{}
`)

	structs, ds := scan.Structs([]*packages.Package{pkg})
	assertNoErrors(t, ds)

	s := structs[0]
	if s.PkgPath != "test" || s.PkgName != "test" {
		t.Errorf("PkgPath, PkgName = %q, %q; want %q, %q", s.PkgPath, s.PkgName, "test", "test")
	}
	if s.Pos.Filename != "test.go" {
		t.Errorf("Pos.Filename = %q, want %q", s.Pos.Filename, "test.go")
	}
}

func TestStructs_SkipsNilPackages(t *testing.T) {
	t.Parallel()

	structs, ds := scan.Structs([]*packages.Package{nil})
	assertNoErrors(t, ds)

	if len(structs) != 0 {
		t.Errorf("structs = %v, want empty", structNames(structs))
	}
}

func TestStructs_ReportsMissingTypeInformation(t *testing.T) {
	t.Parallel()

	structs, ds := scan.Structs([]*packages.Package{{PkgPath: "broken"}})

	if !diag.HasErrors(ds) {
		t.Fatal("Structs() reported no error for a package without type information")
	}
	if len(structs) != 0 {
		t.Errorf("structs = %v, want empty", structNames(structs))
	}
}

func TestStructs_ReportsLoadErrorsWithPosition(t *testing.T) {
	t.Parallel()

	pkg := internaltest.LoadFile(t, `package test

type Row struct{}
`)
	pkg.Errors = []packages.Error{{Pos: "broken.go:12:34", Msg: "some load failure"}}

	structs, ds := scan.Structs([]*packages.Package{pkg})

	if !diag.HasErrors(ds) {
		t.Fatal("Structs() did not surface pkg.Errors as diagnostics")
	}
	if got, want := ds[0].Pos.Filename, "broken.go"; got != want {
		t.Errorf("diag Pos.Filename = %q, want %q", got, want)
	}
	if got, want := ds[0].Pos.Line, 12; got != want {
		t.Errorf("diag Pos.Line = %d, want %d", got, want)
	}
	if got, want := ds[0].Pos.Column, 34; got != want {
		t.Errorf("diag Pos.Column = %d, want %d", got, want)
	}
	if got, want := ds[0].Message, "some load failure"; got != want {
		t.Errorf("diag Message = %q, want %q", got, want)
	}
	// Structs that still resolved are returned so callers can report as much as
	// possible before bailing out.
	if got, want := structNames(structs), []string{"Row"}; !slices.Equal(got, want) {
		t.Errorf("structs = %v, want %v", got, want)
	}
}

func TestStructs_ReportsLoadErrorsWithoutPosition(t *testing.T) {
	t.Parallel()

	pkg := internaltest.LoadFile(t, "package test\n")
	pkg.Errors = []packages.Error{{Msg: "no position here"}}

	_, ds := scan.Structs([]*packages.Package{pkg})

	if !diag.HasErrors(ds) {
		t.Fatal("Structs() did not surface pkg.Errors as diagnostics")
	}
	if got := ds[0].Pos.Filename; got != "" {
		t.Errorf("diag Pos.Filename = %q, want empty", got)
	}
}

func TestResolveTypeExpr_LocalType(t *testing.T) {
	t.Parallel()

	pkg := internaltest.LoadFile(t, `package test

type Row struct{}
`)

	got, err := scan.ResolveTypeExpr(pkg.Fset, pkg.Types, posOf(t, pkg, "Row"), "*Row")
	if err != nil {
		t.Fatalf("ResolveTypeExpr: %v", err)
	}
	if want := "*test.Row"; got.String() != want {
		t.Errorf("ResolveTypeExpr() = %q, want %q", got.String(), want)
	}
}

func TestResolveTypeExpr_QualifiedType(t *testing.T) {
	t.Parallel()

	// A qualified name only resolves when the expression is evaluated at a
	// position inside the file, because imports live in file scope rather than
	// package scope.
	pkg := internaltest.LoadFile(t, `package test

import "io"

type Row struct {
	W io.Writer
}
`)

	got, err := scan.ResolveTypeExpr(pkg.Fset, pkg.Types, posOf(t, pkg, "Row"), "io.Writer")
	if err != nil {
		t.Fatalf("ResolveTypeExpr: %v", err)
	}
	if want := "io.Writer"; got.String() != want {
		t.Errorf("ResolveTypeExpr() = %q, want %q", got.String(), want)
	}
}

func TestResolveTypeExpr_Errors(t *testing.T) {
	t.Parallel()

	pkg := internaltest.LoadFile(t, `package test

const Answer = 42

type Row struct{}
`)
	pos := posOf(t, pkg, "Row")

	tests := []struct {
		name string
		expr string
	}{
		{name: "undefined name", expr: "Missing"},
		{name: "not a type", expr: "Answer"},
		{name: "malformed", expr: "*"},
		{name: "unimported package", expr: "io.Writer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := scan.ResolveTypeExpr(pkg.Fset, pkg.Types, pos, tt.expr); err == nil {
				t.Errorf("ResolveTypeExpr(%q) returned nil error", tt.expr)
			}
		})
	}
}

func TestResolveTypeExpr_MissingInputs(t *testing.T) {
	t.Parallel()

	pkg := internaltest.LoadFile(t, "package test\n")

	if _, err := scan.ResolveTypeExpr(nil, pkg.Types, token.NoPos, "int"); err == nil {
		t.Error("ResolveTypeExpr() returned nil error for a missing file set")
	}
	if _, err := scan.ResolveTypeExpr(pkg.Fset, nil, token.NoPos, "int"); err == nil {
		t.Error("ResolveTypeExpr() returned nil error for a missing package")
	}
}

func posOf(t *testing.T, pkg *packages.Package, name string) token.Pos {
	t.Helper()

	obj := pkg.Types.Scope().Lookup(name)
	if obj == nil {
		t.Fatalf("%q not found in package scope", name)
	}
	return obj.Pos()
}

func structNames(structs []ir.Struct) []string {
	names := make([]string, 0, len(structs))
	for _, s := range structs {
		names = append(names, s.Name)
	}
	return names
}

func findStruct(t *testing.T, structs []ir.Struct, name string) ir.Struct {
	t.Helper()

	for _, s := range structs {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("struct %q not found in %v", name, structNames(structs))
	return ir.Struct{}
}

func assertNoErrors(t *testing.T, ds []diag.Diag) {
	t.Helper()

	if diag.HasErrors(ds) {
		t.Fatalf("unexpected error diagnostics: %s", diag.Format(ds))
	}
}
