package orm_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-kanna/kanna/internal/diag"
	"github.com/go-kanna/kanna/internal/gen/orm"
	"github.com/go-kanna/kanna/internal/packages"
	"github.com/go-kanna/kanna/internal/pkgtest"
	"github.com/go-kanna/kanna/internal/scan"
)

// TestEmitMatchesOrmgenOutput holds the port to what ormgen generated: every
// top-level declaration must be byte-identical to the goldens committed from
// ormgen\'s example. The header and the import block are the only intended
// differences, so the comparison walks named declarations instead of whole
// files — that also absorbs ormgen\'s per-source-file output splitting.
func TestEmitMatchesOrmgenOutput(t *testing.T) {
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

	got := declsOf(t, "emitted", out)

	want := map[string]string{}
	goldens, err := filepath.Glob(filepath.Join("testdata", "parity", "*.golden"))
	if err != nil || len(goldens) == 0 {
		t.Fatalf("no goldens: %v", err)
	}
	for _, path := range goldens {
		data, err := os.ReadFile(path) //nolint:gosec // paths come from the testdata glob above
		if err != nil {
			t.Fatal(err)
		}
		maps.Copy(want, declsOf(t, path, data))
	}

	// The four goldens hold 25 declarations; a comparison that saw far
	// fewer silently compared nothing.
	if len(want) < 20 || len(got) < 20 {
		t.Fatalf("suspiciously few declarations: want %d, got %d", len(want), len(got))
	}

	for name, wantText := range want {
		gotText, ok := got[name]
		if !ok {
			t.Errorf("missing declaration %s", name)
			continue
		}
		if gotText != wantText {
			t.Errorf("declaration %s differs:\n--- ormgen ---\n%s\n--- kanna ---\n%s", name, wantText, gotText)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("extra declaration %s", name)
		}
	}
}

// declsOf maps each named top-level declaration to its source text, doc
// comment included.
func declsOf(t *testing.T, label string, src []byte) map[string]string {
	t.Helper()

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, label, src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v\n%s", label, err, src)
	}

	decls := make(map[string]string)
	for _, decl := range f.Decls {
		var (
			name string
			doc  *ast.CommentGroup
		)
		switch d := decl.(type) {
		case *ast.FuncDecl:
			name = d.Name.Name
			doc = d.Doc
		case *ast.GenDecl:
			if d.Tok != token.VAR {
				continue // import blocks differ by design
			}
			spec, ok := d.Specs[0].(*ast.ValueSpec)
			if !ok {
				t.Fatalf("%s: var declaration without a value spec", label)
			}
			name = spec.Names[0].Name
			doc = d.Doc
		default:
			continue
		}

		start := decl.Pos()
		if doc != nil {
			start = doc.Pos()
		}
		decls[name] = string(src[fset.Position(start).Offset:fset.Position(decl.End()).Offset])
	}
	return decls
}
