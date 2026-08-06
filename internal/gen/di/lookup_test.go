package di_test

import (
	"go/types"
	"testing"

	"github.com/go-kanna/kanna/internal/gen/di"
	"github.com/go-kanna/kanna/internal/packages"
	"github.com/go-kanna/kanna/internal/pkgtest"
)

func TestIndex_LookupByRef(t *testing.T) {
	t.Parallel()

	a := di.Provider{PkgPath: "github.com/example/foo", PkgName: "foo", FuncName: "NewBar"}
	b := di.Provider{PkgPath: "github.com/example/foo2", PkgName: "foo", FuncName: "NewBar"}
	c := di.Provider{PkgPath: "github.com/example/baz", PkgName: "baz", FuncName: "NewBaz"}

	idx := di.NewIndex([]di.Provider{a, b, c})

	tests := []struct {
		name string
		ref  string
		want int
	}{
		{name: "fully qualified", ref: "github.com/example/foo.NewBar", want: 1},
		{name: "package short ambiguous", ref: "foo.NewBar", want: 2},
		{name: "bare unique", ref: "NewBaz", want: 1},
		{name: "bare ambiguous", ref: "NewBar", want: 2},
		{name: "not found", ref: "NoSuch", want: 0},
		{name: "empty ref", ref: "", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := idx.LookupByRef(tt.ref)
			if len(got) != tt.want {
				t.Errorf("LookupByRef(%q) returned %d, want %d", tt.ref, len(got), tt.want)
			}
		})
	}
}

func TestIndex_LookupByType(t *testing.T) {
	t.Parallel()

	// Two packages declaring a same-named type must not collide, since TypeKey
	// qualifies by import path.
	pkg := pkgtest.LoadFile(t, `package test

type DB struct{}
type Greeter interface{ Greet() string }

func NewDB() *DB          { return nil }
func NewGreeter() Greeter { return nil }
`)

	providers, ds := di.Providers([]*packages.Package{pkg})
	assertNoErrors(t, ds)

	idx := di.NewIndex(providers)

	dbPtr := providers[0].Result
	if got := idx.LookupByType(dbPtr); len(got) != 1 || got[0].FuncName != "NewDB" {
		t.Errorf("LookupByType(*DB) = %v, want NewDB", providerNames(deref(got)))
	}

	greeter := providers[1].Result
	if got := idx.LookupByType(greeter); len(got) != 1 || got[0].FuncName != "NewGreeter" {
		t.Errorf("LookupByType(Greeter) = %v, want NewGreeter", providerNames(deref(got)))
	}

	if got := idx.LookupByType(nil); got != nil {
		t.Errorf("LookupByType(nil) = %v, want nil", got)
	}

	if got := idx.LookupByType(types.Typ[types.Int]); len(got) != 0 {
		t.Errorf("LookupByType(int) = %v, want none", providerNames(deref(got)))
	}
}

func TestProviderName(t *testing.T) {
	t.Parallel()

	if got := di.ProviderName(nil); got != "<nil>" {
		t.Errorf("ProviderName(nil) = %q, want <nil>", got)
	}

	p := di.Provider{PkgPath: "github.com/x/y", FuncName: "F"}
	if got, want := di.ProviderName(&p), "github.com/x/y.F"; got != want {
		t.Errorf("ProviderName = %q, want %q", got, want)
	}

	p2 := di.Provider{FuncName: "F"}
	if got, want := di.ProviderName(&p2), "F"; got != want {
		t.Errorf("ProviderName(no pkg) = %q, want %q", got, want)
	}
}

func TestFormatCandidates(t *testing.T) {
	t.Parallel()

	a := di.Provider{PkgPath: "x", FuncName: "A"}
	b := di.Provider{PkgPath: "y", FuncName: "B"}

	got := di.FormatCandidates([]*di.Provider{&a, &b})
	if len(got) != 2 {
		t.Fatalf("got %d lines, want 2", len(got))
	}
	if got[0] != "- x.A" {
		t.Errorf("line[0] = %q, want %q", got[0], "- x.A")
	}
	if got[1] != "- y.B" {
		t.Errorf("line[1] = %q, want %q", got[1], "- y.B")
	}
}

func TestIndex_OwnsCopyOfInput(t *testing.T) {
	t.Parallel()

	in := []di.Provider{
		{PkgPath: "x", PkgName: "x", FuncName: "A"},
	}
	idx := di.NewIndex(in)

	// Mutate the caller's slice after construction.
	in[0].FuncName = "B"

	got := idx.LookupByRef("x.A")
	if len(got) != 1 || got[0].FuncName != "A" {
		t.Errorf("Index reflects caller mutation: got %+v, want FuncName=A", got)
	}

	if found := idx.LookupByRef("x.B"); len(found) != 0 {
		t.Errorf("LookupByRef(x.B) returned %d providers, want 0", len(found))
	}
}

func TestIndex_All_PreservesOrder(t *testing.T) {
	t.Parallel()

	in := []di.Provider{
		{PkgPath: "a", FuncName: "X"},
		{PkgPath: "b", FuncName: "Y"},
		{PkgPath: "c", FuncName: "Z"},
	}
	idx := di.NewIndex(in)

	got := idx.All()
	if len(got) != len(in) {
		t.Fatalf("len = %d, want %d", len(got), len(in))
	}
	for i := range in {
		if got[i].PkgPath != in[i].PkgPath || got[i].FuncName != in[i].FuncName {
			t.Errorf("got[%d] = %+v, want %+v", i, got[i], in[i])
		}
	}
}

func TestTypeKey_QualifiesByImportPath(t *testing.T) {
	t.Parallel()

	pkg := pkgtest.LoadFile(t, `package test

type DB struct{}

func NewDB() *DB { return nil }
`)

	providers, ds := di.Providers([]*packages.Package{pkg})
	assertNoErrors(t, ds)

	if got, want := di.TypeKey(providers[0].Result), "*test.DB"; got != want {
		t.Errorf("TypeKey = %q, want %q", got, want)
	}
	if got, want := di.TypeString(providers[0].Result), "*test.DB"; got != want {
		t.Errorf("TypeString = %q, want %q", got, want)
	}
}

func deref(ps []*di.Provider) []di.Provider {
	out := make([]di.Provider, 0, len(ps))
	for _, p := range ps {
		out = append(out, *p)
	}
	return out
}
