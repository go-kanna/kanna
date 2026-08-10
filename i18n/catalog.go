package i18n

// The types in this file are the compiled form of a locale file. kanna-i18n
// parses and validates the file at generation time and emits these as
// literals, so nothing here checks anything: by the time a Catalog exists,
// every diagnosable mistake has already failed the generation that would have
// produced it.

// CountParam is the parameter that selects the plural form of a message.
const CountParam = "count"

// PluralCategories lists the CLDR plural categories in canonical order. They
// are the valid keys of Entry.Plural.
var PluralCategories = []string{"zero", "one", "two", "few", "many", "other"}

// Kind is the declared kind of a template parameter. It decides how an
// argument value is formatted, not what type it must have.
type Kind uint8

const (
	// KindString renders the value as it is.
	KindString Kind = iota
	// KindInt renders an integer plainly, without locale formatting.
	KindInt
	// KindNumber renders a numeric value with the locale's conventions
	// (e.g., 1,234.56 in English and 1.234,56 in German).
	KindNumber
)

// String returns the kind's name as written in a placeholder annotation.
func (k Kind) String() string {
	switch k {
	case KindString:
		return "string"
	case KindInt:
		return "int"
	case KindNumber:
		return "number"
	default:
		return "invalid"
	}
}

// Segment is one piece of a message template: literal text, or a placeholder
// naming the argument whose value takes its place. A segment is one or the
// other; an empty Param means text.
type Segment struct {
	Text  string
	Param string
	Kind  Kind
}

// Template is a message template, already split into segments. What was
// written as "Hello, {name}!" arrives here as three segments.
type Template struct {
	Segments []Segment
}

// render assembles the template, resolving each placeholder through resolve.
func (t Template) render(resolve func(name string, kind Kind) string) string {
	var b []byte
	for _, s := range t.Segments {
		if s.Param == "" {
			b = append(b, s.Text...)
			continue
		}
		b = append(b, resolve(s.Param, s.Kind)...)
	}
	return string(b)
}

// Entry is a single message of one language. Single holds the template of an
// ordinary message; a plural message carries one variant per CLDR category in
// Plural instead, always including "other".
type Entry struct {
	Single Template
	Plural map[string]Template
}

// Catalog is the set of messages of a single language.
type Catalog struct {
	// Lang is the language as a BCP 47 tag, e.g. "en" or "pt-BR".
	Lang    string
	Entries map[string]Entry
}
