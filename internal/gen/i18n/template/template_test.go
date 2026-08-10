package template_test

import (
	"reflect"
	"testing"

	"github.com/go-kanna/kanna/internal/gen/i18n/template"
)

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		src    string
		args   map[string]string
		want   string
		params []template.Param
	}{
		{
			name: "empty",
			src:  "",
			want: "",
		},
		{
			name: "literal only",
			src:  "Hello!",
			want: "Hello!",
		},
		{
			name:   "single placeholder",
			src:    "Hello, {name}!",
			args:   map[string]string{"name": "World"},
			want:   "Hello, World!",
			params: []template.Param{{Name: "name", Kind: template.KindString}},
		},
		{
			name: "explicit kinds",
			src:  "{count:int} items cost {price:number}",
			args: map[string]string{"count": "3", "price": "9.99"},
			want: "3 items cost 9.99",
			params: []template.Param{
				{Name: "count", Kind: template.KindInt},
				{Name: "price", Kind: template.KindNumber},
			},
		},
		{
			name:   "bare occurrence inherits explicit kind",
			src:    "{price:number} and {price}",
			args:   map[string]string{"price": "10"},
			want:   "10 and 10",
			params: []template.Param{{Name: "price", Kind: template.KindNumber}},
		},
		{
			name:   "explicit kind after bare occurrence",
			src:    "{price} and {price:number}",
			args:   map[string]string{"price": "10"},
			want:   "10 and 10",
			params: []template.Param{{Name: "price", Kind: template.KindNumber}},
		},
		{
			name:   "escaped braces",
			src:    "{{name}} is {name}",
			args:   map[string]string{"name": "Alice"},
			want:   "{name} is Alice",
			params: []template.Param{{Name: "name", Kind: template.KindString}},
		},
		{
			name:   "multibyte literals",
			src:    "こんにちは、{name}さん！",
			args:   map[string]string{"name": "太郎"},
			want:   "こんにちは、太郎さん！",
			params: []template.Param{{Name: "name", Kind: template.KindString}},
		},
		{
			name: "adjacent placeholders",
			src:  "{a}{b}",
			args: map[string]string{"a": "1", "b": "2"},
			want: "12",
			params: []template.Param{
				{Name: "a", Kind: template.KindString},
				{Name: "b", Kind: template.KindString},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tmpl, err := template.Parse(tt.src)
			if err != nil {
				t.Fatalf("Parse(%q) returned error: %v", tt.src, err)
			}
			got := tmpl.Render(func(p template.Param) string { return tt.args[p.Name] })
			if got != tt.want {
				t.Errorf("Render() = %q, want %q", got, tt.want)
			}
			if params := tmpl.Params(); !reflect.DeepEqual(params, tt.params) {
				t.Errorf("Params() = %v, want %v", params, tt.params)
			}
		})
	}
}

func TestParse_error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
	}{
		{name: "unclosed placeholder", src: "Hello, {name"},
		{name: "unmatched closing brace", src: "Hello }"},
		{name: "empty name", src: "{}"},
		{name: "name starting with digit", src: "{9lives}"},
		{name: "name starting with underscore", src: "{_x}"},
		{name: "uppercase name", src: "{Name}"},
		{name: "name with space", src: "{first name}"},
		{name: "unknown kind", src: "{price:float}"},
		{name: "empty kind", src: "{price:}"},
		{name: "conflicting kinds", src: "{x:int} {x:number}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := template.Parse(tt.src); err == nil {
				t.Errorf("Parse(%q) returned nil error", tt.src)
			}
		})
	}
}

func TestKind_GoType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind template.Kind
		want string
	}{
		{kind: template.KindString, want: "string"},
		{kind: template.KindInt, want: "int"},
		{kind: template.KindNumber, want: "float64"},
	}
	for _, tt := range tests {
		if got := template.GoType(tt.kind); got != tt.want {
			t.Errorf("GoType(%v) = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

func TestKind_GoType_unknownPanics(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("GoType() did not panic for an unknown Kind")
		}
	}()
	_ = template.GoType(template.Kind(99))
}

func TestTemplate_Params_returnsCopy(t *testing.T) {
	t.Parallel()

	tmpl, err := template.Parse("Hello, {name}!")
	if err != nil {
		t.Fatal(err)
	}
	tmpl.Params()[0].Kind = template.KindNumber
	if got := tmpl.Params()[0].Kind; got != template.KindString {
		t.Errorf("mutating the returned slice changed internal state: Kind = %v", got)
	}
}

func TestTemplate_Explicit(t *testing.T) {
	t.Parallel()

	tmpl, err := template.Parse("{name} costs {price:number}")
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.Explicit("name") {
		t.Error(`Explicit("name") = true, want false`)
	}
	if !tmpl.Explicit("price") {
		t.Error(`Explicit("price") = false, want true`)
	}
}

func TestTemplate_Render_kinds(t *testing.T) {
	t.Parallel()

	tmpl, err := template.Parse("{name} bought {count:int} items for {price:number}")
	if err != nil {
		t.Fatal(err)
	}
	got := tmpl.Render(func(p template.Param) string { return p.Kind.String() })
	want := "string bought int items for number"
	if got != want {
		t.Errorf("Render() = %q, want %q", got, want)
	}
}
