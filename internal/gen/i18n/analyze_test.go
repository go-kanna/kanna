package i18n_test

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/text/language"

	"github.com/go-kanna/kanna/internal/diag"
	i18n "github.com/go-kanna/kanna/internal/gen/i18n"
)

// warningsOf filters the warning-severity messages out of diagnostics.
func warningsOf(ds []diag.Diag) []string {
	var out []string
	for _, d := range ds {
		if d.Severity == diag.SeverityWarning {
			out = append(out, d.Message)
		}
	}
	return out
}

// mustModel fails the test on any error diagnostic.
func mustModel(t *testing.T, model i18n.Model, ds []diag.Diag) i18n.Model {
	t.Helper()
	if diag.HasErrors(ds) {
		t.Fatalf("Analyze() reported errors: %s", diag.Format(ds))
	}
	return model
}

func TestAnalyze(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "en.yaml"), `
greeting: "Hello!"
hello: "Hello, {name}!"
items_count:
  plural:
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

[items_count.plural]
other = "アイテムが{count}個あります。"
`)

	m, ds := i18n.Analyze(dir, language.English)
	model := mustModel(t, m, ds)
	warnings := warningsOf(ds)

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
	got := warnings[0]
	for _, part := range []string{"ja", "user.deleted", "user.not_found"} {
		if !strings.Contains(got, part) {
			t.Errorf("warning %q does not mention %q", got, part)
		}
	}
}

// The catalogs ride along in the model because rendering embeds them. The
// default locale comes first, however its file sorts, and the rest follow by
// tag.
// The generated file declares func Localizer itself, so a message key that
// CamelCases to it must be rejected: the alternative is output that does not
// compile, reported by the consumer's build instead of by us.
func TestAnalyze_reservedFuncName(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "en.yaml"), "localizer: \"I break things\"\n")

	_, ds := i18n.Analyze(dir, language.English)
	if !diag.HasErrors(ds) {
		t.Fatal("Analyze() accepted a key that generates func Localizer")
	}
	if !strings.Contains(diag.Format(ds), "reserves") {
		t.Errorf("diagnostics %q do not explain the reservation", diag.Format(ds))
	}
}

// A parameter named after a predeclared identifier keeps working but is
// suffixed in the Go signature; the Arg name stays what the locale wrote.
func TestAnalyze_predeclaredParamName(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "en.yaml"), "failed: \"Reason: {error}\"\n")

	m, ds := i18n.Analyze(dir, language.English)
	model := mustModel(t, m, ds)
	p := model.Messages[0].Params[0]
	if p.GoName != "errorArg" || p.Name != "error" {
		t.Errorf("Param = {Name: %q, GoName: %q}, want {error, errorArg}", p.Name, p.GoName)
	}
}

func TestAnalyze_catalogOrder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "de.yaml"), "greeting: \"Hallo!\"\n")
	writeFile(t, filepath.Join(dir, "en.yaml"), "greeting: \"Hello!\"\n")
	writeFile(t, filepath.Join(dir, "ja.yaml"), "greeting: \"こんにちは！\"\n")

	m, ds := i18n.Analyze(dir, language.Japanese)
	model := mustModel(t, m, ds)

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
				"en.yaml": "items:\n  plural:\n    one: \"One\"\n    other: \"Many\"\n",
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
				"en.yaml": "items:\n  plural:\n    one: \"{n:int} item\"\n    other: \"{n:number} items\"\n",
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
			_, ds := i18n.Analyze(dir, language.English)
			if !diag.HasErrors(ds) {
				t.Fatal("Analyze() reported no errors")
			}
			if got := diag.Format(ds); !strings.Contains(got, tt.want) {
				t.Errorf("Analyze() diagnostics %q do not contain %q", got, tt.want)
			}
		})
	}
}

func TestAnalyze_skipsNonLocaleFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "en.yaml"), "greeting: \"Hello!\"\n")
	writeFile(t, filepath.Join(dir, "config.yaml"), "not: [a, locale, file]\n")

	m, ds := i18n.Analyze(dir, language.English)
	model := mustModel(t, m, ds)
	warnings := warningsOf(ds)
	if len(model.Messages) != 1 {
		t.Errorf("len(Messages) = %d, want 1", len(model.Messages))
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "skipping config.yaml") {
		t.Errorf("warnings = %v, want a skipping warning for config.yaml", warnings)
	}
}

func TestAnalyze_warnsOnSubdirectories(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "en.yaml"), "greeting: \"Hello!\"\n")
	if err := os.Mkdir(filepath.Join(dir, "ja"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "ja", "app.yaml"), "greeting: \"こんにちは\"\n")

	m, ds := i18n.Analyze(dir, language.English)
	model := mustModel(t, m, ds)
	warnings := warningsOf(ds)
	if len(model.Messages) != 1 {
		t.Errorf("len(Messages) = %d, want 1", len(model.Messages))
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "skipping directory ja") {
		t.Errorf("warnings = %v, want a skipping warning for the ja subdirectory", warnings)
	}
}

// The layout that motivated the warning: every locale in its own
// subdirectory. The run still fails with "no locale files found", but the
// warnings now say why nothing was found.
func TestAnalyze_subdirectoriesOnlyStillErrors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "en"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "en", "app.yaml"), "greeting: \"Hello!\"\n")

	_, ds := i18n.Analyze(dir, language.English)
	if !diag.HasErrors(ds) {
		t.Fatal("Analyze() reported no errors")
	}
	if got := diag.Format(ds); !strings.Contains(got, "no locale files found") {
		t.Errorf("diagnostics %q do not contain the no-locale-files error", got)
	}
	if ws := warningsOf(ds); len(ws) != 1 || !strings.Contains(ws[0], "skipping directory en") {
		t.Errorf("warnings = %v, want a skipping warning for the en subdirectory", ws)
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

			m, ds := i18n.Analyze(dir, tt.defaultLang)
			model := mustModel(t, m, ds)
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

	_, ds := i18n.Analyze(dir, language.English)
	if diag.HasErrors(ds) {
		t.Fatalf("Analyze() reported errors: %s", diag.Format(ds))
	}
	found := slices.ContainsFunc(warningsOf(ds), func(w string) bool {
		return strings.Contains(w, "ambiguous")
	})
	if !found {
		t.Errorf("warnings = %v, want an ambiguity warning", warningsOf(ds))
	}
}

func TestAnalyze_exactDefaultLocaleIsNotAmbiguous(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "en.yaml"), "greeting: \"Hello!\"\n")
	writeFile(t, filepath.Join(dir, "en-US.yaml"), "greeting: \"Hello!\"\n")

	m, ds := i18n.Analyze(dir, language.English)
	model := mustModel(t, m, ds)
	warnings := warningsOf(ds)
	if model.DefaultTag != language.English {
		t.Errorf("DefaultTag = %v, want %v", model.DefaultTag, language.English)
	}
	for _, w := range warnings {
		if strings.Contains(w, "ambiguous") {
			t.Errorf("unexpected ambiguity warning: %v", w)
		}
	}
}

// The point of reporting through diag: a problem in a locale file carries the
// file and line as data, so an editor or CI can jump to it.
// A plural group that skips forms the language actually uses renders those
// counts with "other" — grammatically wrong text, silently. CLDR knows which
// forms each language distinguishes, so the gap is warned about; a language
// that genuinely lacks a form (Japanese has no "one") warns about nothing.
func TestAnalyze_missingPluralForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		files map[string]string
		lang  language.Tag
		want  []string // substrings of the expected warnings, empty for none
	}{
		{
			name: "russian with only other is under-translated",
			files: map[string]string{
				"en.yaml": "items:\n  plural:\n    one: \"{count} item\"\n    other: \"{count} items\"\n",
				"ru.yaml": "items:\n  plural:\n    other: \"{count} шт.\"\n",
			},
			lang: language.English,
			want: []string{"locale ru", "missing plural forms one, few, many"},
		},
		{
			name: "japanese with only other is complete",
			files: map[string]string{
				"en.yaml": "items:\n  plural:\n    one: \"{count} item\"\n    other: \"{count} items\"\n",
				"ja.yaml": "items:\n  plural:\n    other: \"{count}個\"\n",
			},
			lang: language.English,
			want: nil,
		},
		{
			name: "the default locale is held to the same standard",
			files: map[string]string{
				"en.yaml": "items:\n  plural:\n    other: \"{count} items\"\n",
			},
			lang: language.English,
			want: []string{"locale en", "missing plural forms one"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			for name, body := range tt.files {
				writeFile(t, filepath.Join(dir, name), body)
			}

			_, ds := i18n.Analyze(dir, tt.lang)
			if diag.HasErrors(ds) {
				t.Fatalf("Analyze() reported errors: %s", diag.Format(ds))
			}

			warnings := strings.Join(warningsOf(ds), "\n")
			if len(tt.want) == 0 {
				if warnings != "" {
					t.Errorf("warnings = %q, want none", warnings)
				}
				return
			}
			for _, want := range tt.want {
				if !strings.Contains(warnings, want) {
					t.Errorf("warnings %q do not contain %q", warnings, want)
				}
			}
		})
	}
}

func TestAnalyze_diagnosticsCarryPositions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// The invalid key sits on line 3.
	writeFile(t, filepath.Join(dir, "en.yaml"), "greeting: \"Hello!\"\nhello: \"Hello, {name}!\"\nBAD: \"nope\"\n")

	_, ds := i18n.Analyze(dir, language.English)
	if !diag.HasErrors(ds) {
		t.Fatal("Analyze() reported no errors for an invalid key")
	}

	d := ds[0]
	if !strings.HasSuffix(d.Pos.Filename, "en.yaml") {
		t.Errorf("Pos.Filename = %q, want the locale file", d.Pos.Filename)
	}
	if d.Pos.Line != 3 {
		t.Errorf("Pos.Line = %d, want 3", d.Pos.Line)
	}
}

// Every broken file is reported in one run, not just the first.
func TestAnalyze_collectsAcrossFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "de.yaml"), "BAD: \"nope\"\n")
	writeFile(t, filepath.Join(dir, "en.yaml"), "ALSOBAD: \"nope\"\n")

	_, ds := i18n.Analyze(dir, language.English)
	errs := 0
	for _, d := range ds {
		if d.Severity == diag.SeverityError {
			errs++
		}
	}
	if errs != 2 {
		t.Errorf("errors = %d, want one per broken file:\n%s", errs, diag.Format(ds))
	}
}

func TestAnalyze_missingDirectory(t *testing.T) {
	t.Parallel()

	if _, ds := i18n.Analyze(filepath.Join(t.TempDir(), "nope"), language.English); !diag.HasErrors(ds) {
		t.Error("Analyze() reported no errors for a missing directory")
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
