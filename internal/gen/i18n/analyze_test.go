package i18n_test

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/text/language"

	i18n "github.com/go-kanna/kanna/internal/gen/i18n"
)

func TestAnalyze(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "en.yaml"), `
greeting: "Hello!"
hello: "Hello, {name}!"
items_count:
  one: "You have {count} item."
  other: "You have {count} items."
total_price: "Total: {price:number}"
user:
  not_found: "User not found."
  deleted: "User {name} has been deleted."
`)
	writeFile(t, filepath.Join(dir, "ja.toml"), `
greeting = "こんにちは！"
hello = "こんにちは、{name}さん！"
total_price = "合計: {price}"

[items_count]
other = "アイテムが{count}個あります。"
`)

	model, warnings, err := i18n.Analyze(dir, language.English)
	if err != nil {
		t.Fatalf("Analyze() returned error: %v", err)
	}

	if model.DefaultTag != language.English {
		t.Errorf("DefaultTag = %v, want %v", model.DefaultTag, language.English)
	}
	want := []i18n.Message{
		{Key: "greeting", FuncName: "Greeting", Params: []i18n.Param{}},
		{Key: "hello", FuncName: "Hello", Params: []i18n.Param{
			{Name: "name", GoName: "name", GoType: "string"},
		}},
		{Key: "items_count", FuncName: "ItemsCount", Plural: true, Params: []i18n.Param{
			{Name: "count", GoName: "count", GoType: "int"},
		}},
		{Key: "total_price", FuncName: "TotalPrice", Params: []i18n.Param{
			{Name: "price", GoName: "price", GoType: "float64"},
		}},
		{Key: "user.deleted", FuncName: "UserDeleted", Params: []i18n.Param{
			{Name: "name", GoName: "name", GoType: "string"},
		}},
		{Key: "user.not_found", FuncName: "UserNotFound", Params: []i18n.Param{}},
	}
	if !reflect.DeepEqual(model.Messages, want) {
		t.Errorf("Messages = %+v, want %+v", model.Messages, want)
	}

	if len(warnings) != 1 {
		t.Fatalf("len(warnings) = %d, want 1: %v", len(warnings), warnings)
	}
	got := string(warnings[0])
	for _, part := range []string{"ja", "user.deleted", "user.not_found"} {
		if !strings.Contains(got, part) {
			t.Errorf("warning %q does not mention %q", got, part)
		}
	}
}

// The catalogs ride along in the model because rendering embeds them. The
// default locale comes first, however its file sorts, and the rest follow by
// tag.
func TestAnalyze_catalogOrder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "de.yaml"), "greeting: \"Hallo!\"\n")
	writeFile(t, filepath.Join(dir, "en.yaml"), "greeting: \"Hello!\"\n")
	writeFile(t, filepath.Join(dir, "ja.yaml"), "greeting: \"こんにちは！\"\n")

	model, _, err := i18n.Analyze(dir, language.Japanese)
	if err != nil {
		t.Fatalf("Analyze() returned error: %v", err)
	}

	got := make([]string, 0, len(model.Catalogs))
	for _, c := range model.Catalogs {
		got = append(got, c.Tag.String())
	}
	want := []string{"ja", "de", "en"}
	if !slices.Equal(got, want) {
		t.Errorf("catalog order = %v, want %v", got, want)
	}
}

func TestAnalyze_error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name:  "empty directory",
			files: map[string]string{},
			want:  "no locale files",
		},
		{
			name: "duplicate locale across formats",
			files: map[string]string{
				"en.yaml": "greeting: \"Hello!\"\n",
				"en.toml": "greeting = \"Hi!\"\n",
			},
			want: "defined by both",
		},
		{
			name: "default locale missing",
			files: map[string]string{
				"ja.yaml": "greeting: \"こんにちは！\"\n",
			},
			want: "default locale en not found",
		},
		{
			name: "invalid locale file",
			files: map[string]string{
				"en.yaml": "greeting: 123\n",
			},
			want: "must be a string",
		},
		{
			name: "key not in default locale",
			files: map[string]string{
				"en.yaml": "greeting: \"Hello!\"\n",
				"ja.yaml": "greeting: \"こんにちは！\"\nextra: \"余分\"\n",
			},
			want: "does not exist in default locale",
		},
		{
			name: "parameter not in default locale",
			files: map[string]string{
				"en.yaml": "hello: \"Hello, {name}!\"\n",
				"ja.yaml": "hello: \"こんにちは、{namae}さん！\"\n",
			},
			want: "parameter \"namae\"",
		},
		{
			name: "plural shape mismatch",
			files: map[string]string{
				"en.yaml": "items:\n  one: \"One\"\n  other: \"Many\"\n",
				"ja.yaml": "items: \"アイテム\"\n",
			},
			want: "plural shape differs",
		},
		{
			name: "func name collision",
			files: map[string]string{
				"en.yaml": "user_name: \"a\"\nuser:\n  name: \"b\"\n",
			},
			want: "both generate func UserName",
		},
		{
			name: "go parameter name collision",
			files: map[string]string{
				"en.yaml": "greeting: \"{a_b} {a__b}\"\n",
			},
			want: "both map to Go parameter",
		},
		{
			name: "parameter maps to Go keyword",
			files: map[string]string{
				"en.yaml": "greeting: \"{range}\"\n",
			},
			want: "keyword",
		},
		{
			name: "parameter shadows the import alias",
			files: map[string]string{
				"en.yaml": "greeting: \"{i18n}\"\n",
			},
			want: "import alias",
		},
		{
			name: "conflicting kinds across plural variants in default",
			files: map[string]string{
				"en.yaml": "items:\n  one: \"{n:int} item\"\n  other: \"{n:number} items\"\n",
			},
			want: "in one plural form",
		},
		{
			name: "conflicting kinds between default and translation",
			files: map[string]string{
				"en.yaml": "total_price: \"Total: {price:number}\"\n",
				"ja.yaml": "total_price: \"合計: {price:int}\"\n",
			},
			want: "but default locale has float64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			for name, content := range tt.files {
				writeFile(t, filepath.Join(dir, name), content)
			}
			_, _, err := i18n.Analyze(dir, language.English)
			if err == nil {
				t.Fatal("Analyze() returned nil error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Analyze() error %q does not contain %q", err, tt.want)
			}
		})
	}
}

func TestAnalyze_skipsNonLocaleFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "en.yaml"), "greeting: \"Hello!\"\n")
	writeFile(t, filepath.Join(dir, "config.yaml"), "not: [a, locale, file]\n")

	model, warnings, err := i18n.Analyze(dir, language.English)
	if err != nil {
		t.Fatalf("Analyze() returned error: %v", err)
	}
	if len(model.Messages) != 1 {
		t.Errorf("len(Messages) = %d, want 1", len(model.Messages))
	}
	if len(warnings) != 1 || !strings.Contains(string(warnings[0]), "skipping config.yaml") {
		t.Errorf("warnings = %v, want a skipping warning for config.yaml", warnings)
	}
}

// The default locale resolves like the runtime matcher: region- and
// script-qualified files satisfy a bare default language.
func TestAnalyze_defaultLocaleViaMatcher(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		file        string
		defaultLang language.Tag
		want        language.Tag
	}{
		{
			name:        "region variant",
			file:        "en-US.yaml",
			defaultLang: language.English,
			want:        language.AmericanEnglish,
		},
		{
			name:        "script variant",
			file:        "zh-Hant.yaml",
			defaultLang: language.Chinese,
			want:        language.TraditionalChinese,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, tt.file), "greeting: \"Hello!\"\n")

			model, _, err := i18n.Analyze(dir, tt.defaultLang)
			if err != nil {
				t.Fatalf("Analyze() returned error: %v", err)
			}
			if model.DefaultTag != tt.want {
				t.Errorf("DefaultTag = %v, want %v", model.DefaultTag, tt.want)
			}
		})
	}
}

func TestAnalyze_ambiguousDefaultLocale(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "en-GB.yaml"), "greeting: \"Hello!\"\n")
	writeFile(t, filepath.Join(dir, "en-US.yaml"), "greeting: \"Hello!\"\n")

	_, warnings, err := i18n.Analyze(dir, language.English)
	if err != nil {
		t.Fatalf("Analyze() returned error: %v", err)
	}
	found := slices.ContainsFunc(warnings, func(w i18n.Warning) bool {
		return strings.Contains(string(w), "ambiguous")
	})
	if !found {
		t.Errorf("warnings = %v, want an ambiguity warning", warnings)
	}
}

func TestAnalyze_exactDefaultLocaleIsNotAmbiguous(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "en.yaml"), "greeting: \"Hello!\"\n")
	writeFile(t, filepath.Join(dir, "en-US.yaml"), "greeting: \"Hello!\"\n")

	model, warnings, err := i18n.Analyze(dir, language.English)
	if err != nil {
		t.Fatalf("Analyze() returned error: %v", err)
	}
	if model.DefaultTag != language.English {
		t.Errorf("DefaultTag = %v, want %v", model.DefaultTag, language.English)
	}
	for _, w := range warnings {
		if strings.Contains(string(w), "ambiguous") {
			t.Errorf("unexpected ambiguity warning: %v", w)
		}
	}
}

func TestAnalyze_missingDirectory(t *testing.T) {
	t.Parallel()

	if _, _, err := i18n.Analyze(filepath.Join(t.TempDir(), "nope"), language.English); err == nil {
		t.Error("Analyze() returned nil error for a missing directory")
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
