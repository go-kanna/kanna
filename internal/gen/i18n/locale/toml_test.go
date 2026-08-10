package locale_test

import (
	"testing"

	"golang.org/x/text/language"

	"github.com/go-kanna/kanna/internal/gen/i18n/locale"
)

func TestParseTOML(t *testing.T) {
	t.Parallel()

	src := `
greeting = "Hello!"
hello = "Hello, {name}!"
total_price = "Total: {price:number}"

[items_count]
one = "You have {count} item."
other = "You have {count} items."

[user]
not_found = "User not found."
deleted = "User {name} has been deleted."
`
	c, err := locale.ParseTOML(language.English, []byte(src))
	if err != nil {
		t.Fatalf("ParseTOML() returned error: %v", err)
	}
	assertTargetCatalog(t, c)
}

func TestParseTOML_empty(t *testing.T) {
	t.Parallel()

	c, err := locale.ParseTOML(language.English, []byte("# comment only\n"))
	if err != nil {
		t.Fatalf("ParseTOML() returned error: %v", err)
	}
	if len(c.Entries) != 0 {
		t.Errorf("len(Entries) = %d, want 0", len(c.Entries))
	}
}

func TestParseTOML_error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
	}{
		{name: "int value", src: "greeting = 123\n"},
		{name: "float value", src: "greeting = 1.5\n"},
		{name: "bool value", src: "greeting = true\n"},
		{name: "array value", src: "greeting = [\"a\"]\n"},
		{name: "datetime value", src: "greeting = 2024-01-01\n"},
		{name: "uppercase key", src: "Greeting = \"hi\"\n"},
		{name: "duplicate key", src: "a = \"x\"\na = \"y\"\n"},
		{name: "empty table", src: "[user]\n"},
		{name: "invalid template", src: "greeting = \"Hello, {name\"\n"},
		{name: "plural without other", src: "[items]\none = \"One\"\n"},
		{name: "plural mixed with normal keys", src: "[items]\none = \"One\"\ncustom = \"Custom\"\n"},
		{name: "plural variant not a string", src: "[items.other]\nnested = \"x\"\n"},
		{name: "plural count as number", src: "[items]\nother = \"{count:number} items\"\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := locale.ParseTOML(language.English, []byte(tt.src)); err == nil {
				t.Errorf("ParseTOML(%q) returned nil error", tt.src)
			}
		})
	}
}
