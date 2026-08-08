package di_test

import (
	"slices"
	"testing"

	"github.com/go-kanna/kanna/internal/diag"
	"github.com/go-kanna/kanna/internal/gen/di"
	"github.com/go-kanna/kanna/internal/packages"
	"github.com/go-kanna/kanna/internal/pkgtest"
)

func TestProviders_AcceptsProviderShapes(t *testing.T) {
	t.Parallel()

	src := `package test

import "io"

type DB struct{}
type Greeter interface{ Greet() string }
type Alias = DB

func NewDB() *DB                       { return nil }
func NewValue() DB                     { return DB{} }
func NewGreeter() Greeter              { return nil }
func NewWriter() io.Writer             { return nil }
func NewAliased() Alias                { return DB{} }
func NewAnonymous() interface{ M() }   { return nil }
func NewWithError() (*DB, error)       { return nil, nil }
func NewWithArgs(a *DB, b Greeter) *DB { return nil }
`
	providers, ds := providersOf(t, src)
	assertNoErrors(t, ds)

	// Declaration order, not the alphabetical order of the package scope.
	want := []string{
		"NewDB", "NewValue", "NewGreeter", "NewWriter",
		"NewAliased", "NewAnonymous", "NewWithError", "NewWithArgs",
	}
	if got := providerNames(providers); !slices.Equal(got, want) {
		t.Errorf("providers = %v, want %v", got, want)
	}
}

func TestProviders_SkipsNonProviderShapes(t *testing.T) {
	t.Parallel()

	src := `package test

type DB struct{}

func NoResults()                    {}
func ReturnsError() error           { return nil }
func ReturnsBasic() string          { return "" }
func ReturnsSlice() []DB            { return nil }
func ReturnsMap() map[string]DB     { return nil }
func ReturnsFunc() func() *DB       { return nil }
func ReturnsPtrToPtr() **DB         { return nil }
func TooMany() (*DB, DB, error)     { return nil, DB{}, nil }
func SecondNotError() (*DB, string) { return nil, "" }
func ErrorFirst() (error, *DB)      { return nil, nil }
`
	providers, ds := providersOf(t, src)
	assertNoErrors(t, ds)

	if got := providerNames(providers); len(got) != 0 {
		t.Errorf("providers = %v, want none", got)
	}
}

func TestProviders_OmitsTheVariadicParameter(t *testing.T) {
	t.Parallel()

	// A variadic parameter is a slice, and no provider can produce a slice, so
	// resolving it would fail every time. Go allows omitting the argument, which
	// is what the generated call does.
	src := `package test

type Opt func()
type Foo struct{}

func NewFoo(name string, opts ...Opt) *Foo { return nil }
`
	providers, ds := providersOf(t, src)
	assertNoErrors(t, ds)

	if got, want := len(providers), 1; got != want {
		t.Fatalf("providers = %d, want %d", got, want)
	}
	if got, want := len(providers[0].Params), 1; got != want {
		t.Fatalf("Params = %d, want %d (the variadic one is dropped)", got, want)
	}
	if got, want := providers[0].Params[0].String(), "string"; got != want {
		t.Errorf("Params[0] = %q, want %q", got, want)
	}
}

func TestProviders_SkipsMethods(t *testing.T) {
	t.Parallel()

	src := `package test

type DB struct{}
type Factory struct{}

func (f Factory) NewDB() *DB  { return nil }
func (f *Factory) Build() *DB { return nil }

func NewFactory() *Factory { return nil }
`
	providers, ds := providersOf(t, src)
	assertNoErrors(t, ds)

	// A package scope holds only top-level declarations, so methods never appear.
	if got, want := providerNames(providers), []string{"NewFactory"}; !slices.Equal(got, want) {
		t.Errorf("providers = %v, want %v", got, want)
	}
}

func TestProviders_ReportsResultAndParams(t *testing.T) {
	t.Parallel()

	src := `package test

type DB struct{}
type Greeter interface{ Greet() string }

func NewDB(g Greeter, n int) (*DB, error) { return nil, nil }
`
	providers, ds := providersOf(t, src)
	assertNoErrors(t, ds)

	if got, want := len(providers), 1; got != want {
		t.Fatalf("providers = %d, want %d", got, want)
	}

	p := providers[0]
	if p.FuncName != "NewDB" {
		t.Errorf("FuncName = %q, want %q", p.FuncName, "NewDB")
	}
	if p.PkgPath != "test" || p.PkgName != "test" {
		t.Errorf("PkgPath, PkgName = %q, %q; want %q, %q", p.PkgPath, p.PkgName, "test", "test")
	}
	if want := "*test.DB"; p.Result.String() != want {
		t.Errorf("Result = %q, want %q", p.Result.String(), want)
	}
	if !p.ReturnsError {
		t.Error("ReturnsError = false, want true")
	}
	if got, want := len(p.Params), 2; got != want {
		t.Fatalf("Params = %d, want %d", got, want)
	}
	if got, want := p.Params[0].String(), "test.Greeter"; got != want {
		t.Errorf("Params[0] = %q, want %q", got, want)
	}
	if got, want := p.Params[1].String(), "int"; got != want {
		t.Errorf("Params[1] = %q, want %q", got, want)
	}
	if p.Pos.Filename != "test.go" {
		t.Errorf("Pos.Filename = %q, want %q", p.Pos.Filename, "test.go")
	}
}

func TestProviders_ReportsNoParamsAsNil(t *testing.T) {
	t.Parallel()

	src := `package test

type DB struct{}

func NewDB() *DB { return nil }
`
	providers, ds := providersOf(t, src)
	assertNoErrors(t, ds)

	if got := providers[0].Params; got != nil {
		t.Errorf("Params = %v, want nil", got)
	}
	if providers[0].ReturnsError {
		t.Error("ReturnsError = true, want false")
	}
}

func TestProviders_IncludesGeneratedFiles(t *testing.T) {
	t.Parallel()

	// A constructor emitted by an earlier run is an ordinary provider; skipping
	// generated files would hide exactly what this generator produced.
	pkg := pkgtest.LoadPackage(t, map[string]string{
		"a_handwritten.go": `package test

type DB struct{}

func NewDB() *DB { return nil }
`,
		"b_generated.go": `// Code generated by kanna. DO NOT EDIT.

package test

func NewGeneratedDB() *DB { return nil }
`,
	})

	providers, ds := di.Providers([]*packages.Package{pkg})
	assertNoErrors(t, ds)

	if got, want := providerNames(providers), []string{"NewDB", "NewGeneratedDB"}; !slices.Equal(got, want) {
		t.Errorf("providers = %v, want %v", got, want)
	}

	// Which file a provider came from decides whether an ambiguity involving it
	// can be explained, so the flag has to survive the scan.
	for _, p := range providers {
		wantGenerated := p.FuncName == "NewGeneratedDB"
		if p.Generated != wantGenerated {
			t.Errorf("%s Generated = %v, want %v", p.FuncName, p.Generated, wantGenerated)
		}
	}
}

func TestProviders_SkipsUnexportedNothingSpecial(t *testing.T) {
	t.Parallel()

	// Unexported functions are providers too: resolution happens within a
	// package as often as across one.
	src := `package test

type DB struct{}

func newDB() *DB { return nil }
`
	providers, ds := providersOf(t, src)
	assertNoErrors(t, ds)

	if got, want := providerNames(providers), []string{"newDB"}; !slices.Equal(got, want) {
		t.Errorf("providers = %v, want %v", got, want)
	}
}

func TestProviders_SkipsNilPackages(t *testing.T) {
	t.Parallel()

	providers, ds := di.Providers([]*packages.Package{nil})
	assertNoErrors(t, ds)

	if len(providers) != 0 {
		t.Errorf("providers = %v, want none", providerNames(providers))
	}
}

func TestProviders_ReportsMissingTypeInformation(t *testing.T) {
	t.Parallel()

	providers, ds := di.Providers([]*packages.Package{{PkgPath: "broken"}})

	assertErrorContains(t, ds, "no type information")
	if len(providers) != 0 {
		t.Errorf("providers = %v, want none", providerNames(providers))
	}
}

func TestProviders_KeepsOnePackagePerImportPath(t *testing.T) {
	t.Parallel()

	// Providers must come from the same package variant the structs came from,
	// or the types they resolve against belong to a different type-check run.
	newPkg := func(id, src string) *packages.Package {
		pkg := pkgtest.LoadFile(t, src)
		pkg.ID = id
		return pkg
	}

	plain := newPkg("test", `package test

type DB struct{}

func NewDB() *DB { return nil }
`)
	variant := newPkg("test [test.test]", `package test

type DB struct{}

func NewDB() *DB     { return nil }
func NewTestDB() *DB { return nil }
`)

	providers, ds := di.Providers([]*packages.Package{plain, variant})
	assertNoErrors(t, ds)

	if got, want := providerNames(providers), []string{"NewDB", "NewTestDB"}; !slices.Equal(got, want) {
		t.Errorf("providers = %v, want %v", got, want)
	}
}

func providersOf(t *testing.T, src string) ([]di.Provider, []diag.Diag) {
	t.Helper()

	return di.Providers([]*packages.Package{pkgtest.LoadFile(t, src)})
}

func providerNames(providers []di.Provider) []string {
	names := make([]string, 0, len(providers))
	for _, p := range providers {
		names = append(names, p.FuncName)
	}
	return names
}
