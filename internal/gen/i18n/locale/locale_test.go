package locale_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"golang.org/x/text/language"

	"github.com/go-kanna/kanna/internal/gen/i18n/locale"
	"github.com/go-kanna/kanna/internal/gen/i18n/template"
)

func TestParseFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "en.yaml"), "greeting: \"Hello!\"\n")
	writeFile(t, filepath.Join(dir, "ja.toml"), "greeting = \"こんにちは！\"\n")

	en, err := locale.ParseFile(filepath.Join(dir, "en.yaml"))
	if err != nil {
		t.Fatalf("ParseFile(en.yaml) returned error: %v", err)
	}
	if en.Tag != language.English {
		t.Errorf("en.Tag = %v, want %v", en.Tag, language.English)
	}
	if got := render(en.Entries["greeting"].Single, nil); got != "Hello!" {
		t.Errorf("greeting = %q", got)
	}

	ja, err := locale.ParseFile(filepath.Join(dir, "ja.toml"))
	if err != nil {
		t.Fatalf("ParseFile(ja.toml) returned error: %v", err)
	}
	if ja.Tag != language.Japanese {
		t.Errorf("ja.Tag = %v, want %v", ja.Tag, language.Japanese)
	}
	if got := render(ja.Entries["greeting"].Single, nil); got != "こんにちは！" {
		t.Errorf("greeting = %q", got)
	}

	writeFile(t, filepath.Join(dir, "de.YAML"), "greeting: \"Hallo!\"\n")
	de, err := locale.ParseFile(filepath.Join(dir, "de.YAML"))
	if err != nil {
		t.Fatalf("ParseFile(de.YAML) returned error: %v", err)
	}
	if de.Tag != language.German {
		t.Errorf("de.Tag = %v, want %v", de.Tag, language.German)
	}
}

func TestParseFile_error(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "en.txt"), "greeting: hi\n")
	writeFile(t, filepath.Join(dir, "messages.yaml"), "greeting: hi\n")

	for _, path := range []string{
		filepath.Join(dir, "en.txt"),        // unsupported extension
		filepath.Join(dir, "messages.yaml"), // stem is not a language
		filepath.Join(dir, "fr.yaml"),       // does not exist
	} {
		if _, err := locale.ParseFile(path); err == nil {
			t.Errorf("ParseFile(%q) returned nil error", path)
		}
	}
}

func TestEntry_Params(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		key  string
		want []template.Param
	}{
		{
			name: "no params",
			src:  "greeting: \"Hello!\"\n",
			key:  "greeting",
			want: nil,
		},
		{
			name: "single entry",
			src:  "price: \"{name} costs {price:number}\"\n",
			key:  "price",
			want: []template.Param{
				{Name: "name", Kind: template.KindString},
				{Name: "price", Kind: template.KindNumber},
			},
		},
		{
			name: "plural entry always takes count first",
			src:  "items:\n  one: \"One item\"\n  other: \"Many items\"\n",
			key:  "items",
			want: []template.Param{{Name: "count", Kind: template.KindInt}},
		},
		{
			name: "plural params follow canonical category order",
			src:  "items:\n  one: \"{count} item of {b}\"\n  other: \"{count} items of {a} and {b}\"\n",
			key:  "items",
			want: []template.Param{
				{Name: "count", Kind: template.KindInt},
				{Name: "b", Kind: template.KindString},
				{Name: "a", Kind: template.KindString},
			},
		},
		{
			name: "bare variant inherits explicit kind from earlier variant",
			src:  "items:\n  one: \"{count} file, {size:number} bytes\"\n  other: \"{count} files, {size} bytes\"\n",
			key:  "items",
			want: []template.Param{
				{Name: "count", Kind: template.KindInt},
				{Name: "size", Kind: template.KindNumber},
			},
		},
		{
			name: "bare variant inherits explicit kind from later variant",
			src:  "items:\n  one: \"{count} file, {size} bytes\"\n  other: \"{count} files, {size:number} bytes\"\n",
			key:  "items",
			want: []template.Param{
				{Name: "count", Kind: template.KindInt},
				{Name: "size", Kind: template.KindNumber},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			entry := mustParseYAML(t, tt.src).Entries[tt.key]
			got, err := entry.Params()
			if err != nil {
				t.Fatalf("Params() returned error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Params() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEntry_Params_kindConflict(t *testing.T) {
	t.Parallel()

	c := mustParseYAML(t, "items:\n  one: \"{n:int} item\"\n  other: \"{n:number} items\"\n")
	if _, err := c.Entries["items"].Params(); err == nil {
		t.Error("Params() returned nil error for conflicting kinds")
	}
}

func TestSupportedFile(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"en.yaml", "en.yml", "ja.toml", "de.YAML", "locales/en.yaml"} {
		if !locale.SupportedFile(path) {
			t.Errorf("SupportedFile(%q) = false, want true", path)
		}
	}
	for _, path := range []string{"en.json", "en", "en.yaml.bak", "README.md"} {
		if locale.SupportedFile(path) {
			t.Errorf("SupportedFile(%q) = true, want false", path)
		}
	}
}

func TestTagFromPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want language.Tag
	}{
		{path: "locales/en.yaml", want: language.English},
		{path: "ja.yml", want: language.Japanese},
		{path: "ja.toml", want: language.Japanese},
		{path: "zh-Hans.yaml", want: language.SimplifiedChinese},
		{path: "en_US.yaml", want: language.AmericanEnglish},
	}
	for _, tt := range tests {
		got, err := locale.TagFromPath(tt.path)
		if err != nil {
			t.Errorf("TagFromPath(%q) returned error: %v", tt.path, err)
			continue
		}
		if got != tt.want {
			t.Errorf("TagFromPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}

	for _, path := range []string{"messages.yaml", "locales/grüße.yaml", ".yaml"} {
		if _, err := locale.TagFromPath(path); err == nil {
			t.Errorf("TagFromPath(%q) returned nil error", path)
		}
	}
}

// assertTargetCatalog checks the catalog parsed from the reference schema
// shared by the YAML and TOML tests.
func assertTargetCatalog(t *testing.T, c locale.Catalog) {
	t.Helper()

	wantKeys := []string{"greeting", "hello", "items_count", "total_price", "user.not_found", "user.deleted"}
	if len(c.Entries) != len(wantKeys) {
		t.Errorf("len(Entries) = %d, want %d", len(c.Entries), len(wantKeys))
	}
	for _, key := range wantKeys {
		if _, ok := c.Entries[key]; !ok {
			t.Errorf("Entries[%q] is missing", key)
		}
	}

	got := render(c.Entries["user.deleted"].Single, map[string]string{"name": "Alice"})
	if got != "User Alice has been deleted." {
		t.Errorf("user.deleted = %q", got)
	}

	plural := c.Entries["items_count"]
	if plural.Plural == nil {
		t.Fatal("items_count is not a plural entry")
	}
	if len(plural.Plural) != 2 {
		t.Errorf("len(items_count.Plural) = %d, want 2", len(plural.Plural))
	}
	if got := render(plural.Plural["one"], map[string]string{"count": "1"}); got != "You have 1 item." {
		t.Errorf("items_count.one = %q", got)
	}
	if single := c.Entries["greeting"]; single.Plural != nil {
		t.Error("greeting should not be a plural entry")
	}
}

func render(tmpl template.Template, args map[string]string) string {
	return tmpl.Render(func(p template.Param) string { return args[p.Name] })
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
