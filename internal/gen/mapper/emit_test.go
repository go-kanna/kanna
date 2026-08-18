package mapper_test

import (
	"bytes"
	"flag"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/go-kanna/kanna/internal/gen/mapper"
)

var update = flag.Bool("update", false, "rewrite golden files")

// Golden files are real Go source inside the fixture packages, so a golden that
// stops compiling fails the fixture load every test here depends on. `go build`
// skips testdata, but the loader type-checks it. If the emitter breaks a golden
// package, fix the emitter and regenerate with:
//
//	go test ./internal/gen/mapper -run TestEmitFile -update
func checkGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("read golden: %v (run with -update to create)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("generated code differs from %s\n--- got ---\n%s", path, got)
	}
}

func TestEmitFile(t *testing.T) {
	t.Parallel()

	pairs, table, model := employeePairs(t)
	plans, _, err := mapper.ResolvePlans(mapper.ResolveConfig{
		Fset:  model.Fset,
		Pairs: pairs,
		Conv:  table,
		Ignores: map[mapper.FieldKey]bool{
			{PkgPath: model.PkgPath, Type: "Employee", Field: "CreatedAt"}: true,
		},
		Direction: mapper.DirectionBoth,
	})
	if err != nil {
		t.Fatalf("resolve plans: %v", err)
	}

	got, err := mapper.EmitFile("output", fixturePrefix+"output", plans)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	checkGolden(t, filepath.Join("testdata", "output", "output_gen.go"), got)
}

func TestEmitFileSelfImport(t *testing.T) {
	t.Parallel()

	model := fixture(t, "model")
	protolike := fixture(t, "protolike")
	pairs := []mapper.PairSpec{
		{Src: namedType(t, model, "Address"), Dst: types.NewPointer(namedType(t, protolike, "Address"))},
	}
	plans, _, err := mapper.ResolvePlans(mapper.ResolveConfig{
		Fset:      model.Fset,
		Pairs:     pairs,
		Direction: mapper.DirectionBoth,
	})
	if err != nil {
		t.Fatalf("resolve plans: %v", err)
	}

	got, err := mapper.EmitFile("model", model.PkgPath, plans)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	checkGolden(t, filepath.Join("testdata", "model", "model_gen.go"), got)
}

func TestImportTrackerCollisions(t *testing.T) {
	t.Parallel()

	got := mapper.TrackerNames("example.com/output",
		types.NewPackage("example.com/a/model", "model"),
		types.NewPackage("example.com/b/model", "model"),
		types.NewPackage("example.com/a/model", "model"), // repeated: stable name
		types.NewPackage("example.com/src", "src"),       // reserved identifier
		types.NewPackage("example.com/gen/v1", "v1"),     // temp-variable pattern
		types.NewPackage("example.com/output", "output"), // output package: unqualified
	)
	want := []string{"model", "model2", "model", "srcpkg", "v1pkg", ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
