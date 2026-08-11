package orm_test

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-kanna/kanna/internal/diag"
	"github.com/go-kanna/kanna/internal/gen/orm"
	"github.com/go-kanna/kanna/internal/packages"
	"github.com/go-kanna/kanna/internal/pkgtest"
	"github.com/go-kanna/kanna/internal/scan"
)

var update = flag.Bool("update", false, "rewrite the emit golden from the current output")

// TestEmitGolden pins the emitted file for the ported ormgen example model.
// The original port was validated declaration-by-declaration against the
// output ormgen itself had committed; the golden has since absorbed kanna's
// deliberate fixes (nil-safe join scans among them), so it is now the record
// of what kanna emits. Regenerate with -update after reviewing a change.
func TestEmitGolden(t *testing.T) {
	t.Parallel()

	modelSrc, err := os.ReadFile(filepath.Join("testdata", "parity", "model.go.txt"))
	if err != nil {
		t.Fatal(err)
	}

	pkg := pkgtest.LoadFileAs(t, "model", string(modelSrc))
	structs, ds := scan.Structs([]*packages.Package{pkg})
	if diag.HasErrors(ds) {
		t.Fatalf("scan: %s", diag.Format(ds))
	}
	tables, ds := orm.Tables(structs)
	if diag.HasErrors(ds) {
		t.Fatalf("plan: %s", diag.Format(ds))
	}

	out, err := orm.Emit(orm.EmitParams{
		PackageName: "query",
		SourceName:  "model",
		SourcePath:  "example.com/model",
		Tables:      tables,
	})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	golden := filepath.Join("testdata", "parity", "orm_gen.go.golden")
	if *update {
		if err := os.WriteFile(golden, out, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(golden) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, want) {
		t.Errorf("emitted file differs from the golden; run with -update after reviewing\n--- got ---\n%s", out)
	}
}

// emitFor plans src and emits it as package query for tests below.
func emitFor(t *testing.T, src string) string {
	t.Helper()

	pkg := pkgtest.LoadFileAs(t, "model", src)
	structs, ds := scan.Structs([]*packages.Package{pkg})
	if diag.HasErrors(ds) {
		t.Fatalf("scan: %s", diag.Format(ds))
	}
	tables, ds := orm.Tables(structs)
	if diag.HasErrors(ds) {
		t.Fatalf("plan: %s", diag.Format(ds))
	}
	out, err := orm.Emit(orm.EmitParams{
		PackageName: "query",
		SourceName:  "model",
		SourcePath:  "example.com/model",
		Tables:      tables,
	})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	return string(out)
}

func TestEmitNamedIntPrimaryKey(t *testing.T) {
	t.Parallel()

	got := emitFor(t, `package model

type Key int64

//kanna:table
type Job struct {
	ID Key
}
`)
	for _, want := range []string{
		"func setJobPK(v *model.Job, id int64) {",
		"v.ID = model.Key(id)",
		"scanJob, jobColumnValuePairs, setJobPK,",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated file lacks %q:\n%s", want, got)
		}
	}
}

func TestEmitPointerForeignKeyPreloaders(t *testing.T) {
	t.Parallel()

	got := emitFor(t, `package model

//kanna:table
type User struct {
	ID    int
	Posts []Post `+"`"+`orm:"has_many,foreign_key:user_id"`+"`"+`
}

//kanna:table
type Post struct {
	ID     int
	UserID *int
	Title  string
}
`)
	for _, want := range []string{
		"if r.UserID != nil {",
		"byFK[*r.UserID] = append(byFK[*r.UserID], r)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated file lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "byFK[r.UserID]") {
		t.Errorf("generated file indexes the map with a pointer key:\n%s", got)
	}
}

func TestEmitManyToManyWithDifferingKeyTypes(t *testing.T) {
	t.Parallel()

	got := emitFor(t, `package model

//kanna:table
type User struct {
	ID   int
	Tags []Tag `+"`"+`orm:"many_to_many,join_table:user_tags,foreign_key:user_id,references:tag_code"`+"`"+`
}

//kanna:table
type Tag struct {
	Code string `+"`"+`orm:",primary_key"`+"`"+`
	Name string
}
`)
	for _, want := range []string{
		"orm.QueryJoinTable[int, string]",
		"byPK := make(map[string]model.Tag)",
		"byPK[r.Code] = r",
		`scope.In("code", targetIDs)`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated file lacks %q:\n%s", want, got)
		}
	}
}

func TestEmitImportsThirdPackageKeyTypes(t *testing.T) {
	t.Parallel()

	got := emitFor(t, `package model

import "net/netip"

//kanna:table
type Host struct {
	ID    netip.Addr
	Peers []Peer `+"`"+`orm:"has_many,foreign_key:host_id"`+"`"+`
}

//kanna:table
type Peer struct {
	ID     int
	HostID netip.Addr
}
`)
	for _, want := range []string{
		"\"net/netip\"",
		"make(map[netip.Addr][]model.Peer)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated file lacks %q:\n%s", want, got)
		}
	}
}

func TestEmitRejectsShadowedQualifier(t *testing.T) {
	t.Parallel()

	pkg := pkgtest.LoadFileAs(t, "ids", `package ids

//kanna:table
type User struct {
	ID    int
	Posts []Post `+"`"+`orm:"has_many,foreign_key:user_id"`+"`"+`
}

//kanna:table
type Post struct {
	ID     int
	UserID int
}
`)
	structs, ds := scan.Structs([]*packages.Package{pkg})
	if diag.HasErrors(ds) {
		t.Fatalf("scan: %s", diag.Format(ds))
	}
	tables, ds := orm.Tables(structs)
	if diag.HasErrors(ds) {
		t.Fatalf("plan: %s", diag.Format(ds))
	}

	_, err := orm.Emit(orm.EmitParams{
		PackageName: "query",
		SourceName:  "ids",
		SourcePath:  "example.com/ids",
		Tables:      tables,
	})
	if err == nil || !strings.Contains(err.Error(), "shadow") {
		t.Fatalf("err = %v, want a shadowing error", err)
	}
}
