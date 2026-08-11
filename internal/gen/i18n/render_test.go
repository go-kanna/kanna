package i18n_test

import (
	"bytes"
	"flag"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/language"

	"github.com/go-kanna/kanna/internal/diag"

	rti18n "github.com/go-kanna/kanna/i18n"
	i18n "github.com/go-kanna/kanna/internal/gen/i18n"
)

var update = flag.Bool("update", false, "update golden files")

func TestRender_golden(t *testing.T) {
	t.Parallel()

	got := renderBasic(t)

	golden := filepath.Join("testdata", "basic", "i18n_gen.go.golden")
	if *update {
		if err := os.WriteFile(golden, got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(filepath.Clean(golden))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("Render() differs from %s; run go test ./internal/gen/i18n -update to refresh\ngot:\n%s", golden, got)
	}
}

func TestRender_outputParses(t *testing.T) {
	t.Parallel()

	got := renderBasic(t)
	if _, err := parser.ParseFile(token.NewFileSet(), "i18n_gen.go", got, parser.AllErrors); err != nil {
		t.Errorf("generated code does not parse: %v", err)
	}
}

func TestRender_deterministic(t *testing.T) {
	t.Parallel()

	if !bytes.Equal(renderBasic(t), renderBasic(t)) {
		t.Error("Render() is not deterministic")
	}
}

// An empty default locale must still yield a compilable file: the runtime
// import is unused in that case and gofmt does not strip unused imports.
func TestRender_emptyModel(t *testing.T) {
	t.Parallel()

	got, err := i18n.Render(i18n.Model{}, "messages")
	if err != nil {
		t.Fatalf("Render() returned error: %v", err)
	}
	if strings.Contains(string(got), "import") {
		t.Errorf("empty model output contains an unused import:\n%s", got)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "i18n_gen.go", got, parser.AllErrors); err != nil {
		t.Errorf("generated code does not parse: %v", err)
	}
}

// A model with messages but no catalogs only arises outside the CLI, but the
// promise stands there too: no unused import, because format.Source would not
// strip it and the file would not compile.
func TestRender_messagesWithoutCatalogs(t *testing.T) {
	t.Parallel()

	got, err := i18n.Render(i18n.Model{Messages: []i18n.Message{
		{Key: "greeting", FuncName: "Greeting", Params: []i18n.Param{}},
	}}, "messages")
	if err != nil {
		t.Fatalf("Render() returned error: %v", err)
	}
	if strings.Contains(string(got), "x/text/language") {
		t.Errorf("output imports the language package nothing uses:\n%s", got)
	}
	if !strings.Contains(string(got), "func Greeting()") {
		t.Errorf("output lacks the constructor:\n%s", got)
	}
}

func TestRender_invalidPackageName(t *testing.T) {
	t.Parallel()

	for _, pkg := range []string{"", "1bad", "my-pkg", "_", "func"} {
		if _, err := i18n.Render(i18n.Model{}, pkg); err == nil {
			t.Errorf("Render(%q) returned nil error", pkg)
		}
	}
}

// The bundle a generated package builds is the analyzed catalogs run through
// Segments(). Building one here the same way and localizing through it closes
// the gap the golden test leaves: that the embedded form not only parses but
// renders the same strings the locale files promised.
func TestRender_embeddedCatalogRoundTrip(t *testing.T) {
	t.Parallel()

	model, ds := i18n.Analyze(filepath.Join("testdata", "basic", "locales"), language.English)
	if diag.HasErrors(ds) {
		t.Fatalf("Analyze() reported errors: %s", diag.Format(ds))
	}

	catalogs := make([]rti18n.Catalog, 0, len(model.Catalogs))
	for _, c := range model.Catalogs {
		entries := make(map[string]rti18n.Entry, len(c.Entries))
		for key, entry := range c.Entries {
			if entry.Plural == nil {
				entries[key] = rti18n.Entry{Single: entry.Single.Segments()}
				continue
			}
			plural := make(map[string]rti18n.Template, len(entry.Plural))
			for category, tmpl := range entry.Plural {
				plural[category] = tmpl.Segments()
			}
			entries[key] = rti18n.Entry{Plural: plural}
		}
		catalogs = append(catalogs, rti18n.Catalog{Lang: c.Tag.String(), Entries: entries})
	}
	bundle := rti18n.NewBundle(model.DefaultTag.String(), catalogs...)

	tests := []struct {
		name string
		tag  language.Tag
		msg  rti18n.Message
		want string
	}{
		{
			name: "en argument",
			tag:  language.English,
			msg:  rti18n.Message{Key: "hello", Args: []rti18n.Arg{{Name: "name", Value: "World"}}},
			want: "Hello, World!",
		},
		{
			name: "en plural one",
			tag:  language.English,
			msg:  rti18n.Message{Key: "items_count", Args: []rti18n.Arg{{Name: "count", Value: 1}}},
			want: "You have 1 item.",
		},
		{
			name: "en number formatting",
			tag:  language.English,
			msg:  rti18n.Message{Key: "total_price", Args: []rti18n.Arg{{Name: "price", Value: 1234.56}}},
			want: "Total: 1,234.56",
		},
		{
			name: "ja plural has only other",
			tag:  language.Japanese,
			msg:  rti18n.Message{Key: "items_count", Args: []rti18n.Arg{{Name: "count", Value: 1}}},
			want: "アイテムが1個あります。",
		},
		{
			name: "unloaded language falls back to default",
			tag:  language.French,
			msg:  rti18n.Message{Key: "greeting"},
			want: "Hello!",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := bundle.Localizer(tt.tag).Localize(tt.msg); got != tt.want {
				t.Errorf("Localize(%q) = %q, want %q", tt.msg.Key, got, tt.want)
			}
		})
	}
}

func renderBasic(t *testing.T) []byte {
	t.Helper()
	model, ds := i18n.Analyze(filepath.Join("testdata", "basic", "locales"), language.English)
	if len(ds) != 0 {
		t.Fatalf("Analyze() reported diagnostics: %s", diag.Format(ds))
	}
	src, err := i18n.Render(model, "messages")
	if err != nil {
		t.Fatal(err)
	}
	return src
}
