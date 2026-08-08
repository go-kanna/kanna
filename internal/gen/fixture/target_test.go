package fixture_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/go-kanna/kanna/internal/diag"
	"github.com/go-kanna/kanna/internal/gen/fixture"
	"github.com/go-kanna/kanna/internal/packages"
	"github.com/go-kanna/kanna/internal/pkgtest"
	"github.com/go-kanna/kanna/internal/scan"
)

// targetsOf runs the scan layer over src and selects the fixture targets, the
// same path the CLI takes.
func targetsOf(t *testing.T, src string) ([]fixture.Target, []diag.Diag) {
	t.Helper()

	pkg := pkgtest.LoadFile(t, src)

	structs, ds := scan.Structs([]*packages.Package{pkg})
	if diag.HasErrors(ds) {
		t.Fatalf("scan: %s", diag.Format(ds))
	}

	return fixture.Targets(structs)
}

func targetNames(targets []fixture.Target) []string {
	names := make([]string, 0, len(targets))
	for _, tg := range targets {
		names = append(names, tg.Name)
	}
	return names
}

func fieldNames(target fixture.Target) []string {
	names := make([]string, 0, len(target.Fields))
	for _, f := range target.Fields {
		names = append(names, f.Name)
	}
	return names
}

func assertNoDiags(t *testing.T, ds []diag.Diag) {
	t.Helper()

	if len(ds) > 0 {
		t.Fatalf("diagnostics = %s, want none", diag.Format(ds))
	}
}

func TestTargets_SortsByName(t *testing.T) {
	t.Parallel()

	// Declared in an order that is neither alphabetical nor reversed, so the
	// assertion cannot pass by accident.
	src := `package test

type User struct{ ID int64 }
type Article struct{ ID int64 }
type Session struct{ ID int64 }
`
	targets, ds := targetsOf(t, src)
	assertNoDiags(t, ds)

	want := []string{"Article", "Session", "User"}
	if got := targetNames(targets); !slices.Equal(got, want) {
		t.Errorf("targets = %v, want %v", got, want)
	}
}

func TestTargets_SkipsUnexportedTypes(t *testing.T) {
	t.Parallel()

	src := `package test

type User struct{ ID int64 }
type secret struct{ ID int64 }
`
	targets, ds := targetsOf(t, src)
	assertNoDiags(t, ds)

	if got, want := targetNames(targets), []string{"User"}; !slices.Equal(got, want) {
		t.Errorf("targets = %v, want %v", got, want)
	}
}

func TestTargets_SkipsIgnoredTypes(t *testing.T) {
	t.Parallel()

	src := `package test

//kanna:ignore
type Ignored struct{ ID int64 }

type Kept struct{ ID int64 }
`
	targets, ds := targetsOf(t, src)
	assertNoDiags(t, ds)

	if got, want := targetNames(targets), []string{"Kept"}; !slices.Equal(got, want) {
		t.Errorf("targets = %v, want %v", got, want)
	}
}

// A directive needs the tag against the comment marker; the spaced form would
// otherwise show up in the type's documentation. It stays a target and the
// author is told why.
func TestTargets_SpacedIgnoreIsNotADirective(t *testing.T) {
	t.Parallel()

	src := `package test

// kanna:ignore
type SpacedIgnore struct{ ID int64 }
`
	targets, ds := targetsOf(t, src)

	if got, want := targetNames(targets), []string{"SpacedIgnore"}; !slices.Equal(got, want) {
		t.Errorf("targets = %v, want %v", got, want)
	}

	if len(ds) != 1 || ds[0].Severity != diag.SeverityWarning {
		t.Fatalf("diagnostics = %s, want one warning", diag.Format(ds))
	}
	if !strings.Contains(ds[0].Message, `write it as "//kanna:ignore"`) {
		t.Errorf("warning = %q, want it to show the correct spelling", ds[0].Message)
	}
}

// An unexported type is out of scope before its comments are read, so a stray
// directive on one is not worth a diagnostic.
func TestTargets_IgnoresDirectivesOnUnexportedTypes(t *testing.T) {
	t.Parallel()

	src := `package test

// kanna:ignore
type secret struct{ ID int64 }
`
	targets, ds := targetsOf(t, src)
	assertNoDiags(t, ds)

	if len(targets) != 0 {
		t.Errorf("targets = %v, want none", targetNames(targets))
	}
}

func TestTargets_SkipsGenericStructsWithAWarning(t *testing.T) {
	t.Parallel()

	src := `package test

type Pair[T any] struct{ A, B T }

type User struct{ ID int64 }
`
	targets, ds := targetsOf(t, src)

	if got, want := targetNames(targets), []string{"User"}; !slices.Equal(got, want) {
		t.Errorf("targets = %v, want %v", got, want)
	}

	if len(ds) != 1 || ds[0].Severity != diag.SeverityWarning {
		t.Fatalf("diagnostics = %s, want one warning", diag.Format(ds))
	}
	if !strings.Contains(ds[0].Message, "skipping generic struct Pair") {
		t.Errorf("warning = %q, want it to name the skipped struct", ds[0].Message)
	}
}

func TestTargets_KeepsExportedFieldsInDeclarationOrder(t *testing.T) {
	t.Parallel()

	src := `package test

type User struct {
	ID        int64
	name      string
	Email     string
	CreatedAt int64
}
`
	targets, ds := targetsOf(t, src)
	assertNoDiags(t, ds)

	if len(targets) != 1 {
		t.Fatalf("targets = %v, want one", targetNames(targets))
	}

	want := []string{"ID", "Email", "CreatedAt"}
	if got := fieldNames(targets[0]); !slices.Equal(got, want) {
		t.Errorf("fields = %v, want %v", got, want)
	}
}

// An embedded field is a settable field named after its type, so it is kept and
// left for the inference rules to fill like any other.
func TestTargets_KeepsEmbeddedFields(t *testing.T) {
	t.Parallel()

	src := `package test

type Profile struct{ Bio string }

type Admin struct {
	Profile
	Level int
}
`
	targets, ds := targetsOf(t, src)
	assertNoDiags(t, ds)

	idx := slices.IndexFunc(targets, func(tg fixture.Target) bool { return tg.Name == "Admin" })
	if idx < 0 {
		t.Fatalf("targets = %v, want Admin among them", targetNames(targets))
	}

	want := []string{"Profile", "Level"}
	if got := fieldNames(targets[idx]); !slices.Equal(got, want) {
		t.Errorf("fields = %v, want %v", got, want)
	}
}

func TestTargets_NoStructs(t *testing.T) {
	t.Parallel()

	targets, ds := targetsOf(t, "package test\n\ntype ID int64\n")
	assertNoDiags(t, ds)

	if len(targets) != 0 {
		t.Errorf("targets = %v, want none", targetNames(targets))
	}
}
