package locale_test

import (
	"testing"

	"golang.org/x/text/language"

	"github.com/go-kanna/kanna/internal/gen/i18n/locale"
)

func TestParseYAML(t *testing.T) {
	t.Parallel()

	src := `
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
`
	assertTargetCatalog(t, mustParseYAML(t, src))
}

func TestParseYAML_empty(t *testing.T) {
	t.Parallel()

	for _, src := range []string{"", "# comment only\n"} {
		c := mustParseYAML(t, src)
		if len(c.Entries) != 0 {
			t.Errorf("ParseYAML(%q): len(Entries) = %d, want 0", src, len(c.Entries))
		}
	}
}

func TestParseYAML_aliases(t *testing.T) {
	t.Parallel()

	src := `
brand: &brand "MyApp"
welcome: *brand
plans: &plans
  free: "Free plan"
  pro: "Pro plan"
pricing: *plans
`
	c := mustParseYAML(t, src)
	if got := render(c.Entries["welcome"].Single, nil); got != "MyApp" {
		t.Errorf("welcome = %q, want %q", got, "MyApp")
	}
	if got := render(c.Entries["pricing.pro"].Single, nil); got != "Pro plan" {
		t.Errorf("pricing.pro = %q, want %q", got, "Pro plan")
	}
}

func TestParseYAML_pluralOtherOnly(t *testing.T) {
	t.Parallel()

	c := mustParseYAML(t, "items_count:\n  plural:\n    other: \"アイテムが{count}個あります。\"\n")
	entry := c.Entries["items_count"]
	if entry.Plural == nil {
		t.Fatal("items_count is not a plural entry")
	}
	if got := render(entry.Plural["other"], map[string]string{"count": "5"}); got != "アイテムが5個あります。" {
		t.Errorf("items_count.other = %q", got)
	}
}

// The category names are ordinary keys everywhere except directly under a
// mapping-valued "plural". Both shapes below used to be misread: the first was
// silently reinterpreted as a plural group, the second was a hard error.
func TestParseYAML_categoryNamesAreOrdinaryKeys(t *testing.T) {
	t.Parallel()

	c := mustParseYAML(t, "errors:\n  other: \"boom\"\nstatus:\n  many: \"多数\"\n  active: \"有効\"\n")

	for _, key := range []string{"errors.other", "status.many", "status.active"} {
		entry, ok := c.Entries[key]
		if !ok {
			t.Errorf("Entries[%q] is missing", key)
			continue
		}
		if entry.Plural != nil {
			t.Errorf("Entries[%q] was read as a plural group", key)
		}
	}
}

// A scalar child named "plural" is an ordinary message; only a mapping-valued
// one is the marker.
func TestParseYAML_scalarPluralIsAnOrdinaryKey(t *testing.T) {
	t.Parallel()

	c := mustParseYAML(t, "modes:\n  plural: \"Plural\"\n  singular: \"Singular\"\n")
	if _, ok := c.Entries["modes.plural"]; !ok {
		t.Errorf("Entries[%q] is missing: %v", "modes.plural", c.Entries)
	}
}

func TestParseYAML_error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
	}{
		{name: "top-level sequence", src: "- a\n- b\n"},
		{name: "int value", src: "greeting: 123\n"},
		{name: "bool value", src: "greeting: true\n"},
		{name: "null value", src: "greeting:\n"},
		{name: "sequence value", src: "greeting:\n  - a\n"},
		{name: "uppercase key", src: "Greeting: hi\n"},
		{name: "key with dot", src: "\"a.b\": hi\n"},
		{name: "key starting with digit", src: "1st: hi\n"},
		{name: "non-string key", src: "123: hi\n"},
		{name: "duplicate key", src: "a: x\nb: y\na: z\n"},
		{name: "empty mapping", src: "user: {}\n"},
		{name: "invalid template", src: "greeting: \"Hello, {name\"\n"},
		{name: "plural without other", src: "items:\n  plural:\n    one: \"One\"\n"},
		{name: "plural sharing its mapping", src: "items:\n  plural:\n    other: \"x\"\n  custom: \"Custom\"\n"},
		{name: "non-category under plural", src: "items:\n  plural:\n    other: \"x\"\n    custom: \"y\"\n"},
		{name: "plural variant not a string", src: "items:\n  plural:\n    other:\n      nested: \"x\"\n"},
		{name: "plural count as number", src: "items:\n  plural:\n    other: \"{count:number} items\"\n"},
		{name: "multiple documents", src: "a: \"1\"\n---\nb: \"2\"\n"},
		{name: "malformed second document", src: "a: \"1\"\n---\nb: [\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := locale.ParseYAML(language.English, []byte(tt.src)); err == nil {
				t.Errorf("ParseYAML(%q) returned nil error", tt.src)
			}
		})
	}
}

func mustParseYAML(t *testing.T, src string) locale.Catalog {
	t.Helper()
	c, err := locale.ParseYAML(language.English, []byte(src))
	if err != nil {
		t.Fatalf("ParseYAML() returned error: %v", err)
	}
	return c
}
