// Package template parses message templates such as "Hello, {name}!".
//
// A placeholder is written as {name} or {name:kind}, where kind is one of
// "string", "int", and "number" (string when omitted). Use "int" for plain
// integers rendered as-is (e.g., counts) and "number" for float64 values
// rendered with locale-aware formatting (e.g., 1,234.56). Literal braces are
// escaped as {{ and }}.
//
// Parsing happens at generation time only. What the runtime renders is the
// parsed form, handed over as i18n.Segment values.
package template

import (
	"fmt"
	"slices"
	"strings"

	"github.com/go-kanna/kanna/i18n"
)

// Kind is the declared kind of a template parameter. It is the runtime's own
// vocabulary: what is parsed here is embedded there.
type Kind = i18n.Kind

const (
	KindString = i18n.KindString // {name} or {name:string}
	KindInt    = i18n.KindInt    // {name:int}
	KindNumber = i18n.KindNumber // {name:number}, carried as float64
)

// GoType returns the Go type of values of a kind in generated code. It panics
// on an unknown Kind: silently defaulting would generate wrongly typed
// constructors, and this is only reachable from the generator.
func GoType(k Kind) string {
	switch k {
	case KindString:
		return "string"
	case KindInt:
		return "int"
	case KindNumber:
		return "float64"
	default:
		panic(fmt.Sprintf("template: unknown Kind %d", k))
	}
}

// Param is a parameter appearing in a template.
type Param struct {
	Name string
	Kind Kind
}

// Template is a parsed message template.
type Template struct {
	segments []segment
	params   []Param
	explicit map[string]bool
}

// Parse parses a message template. A bare placeholder inherits an explicit
// kind annotated elsewhere in the template; conflicting explicit annotations
// are an error.
func Parse(src string) (Template, error) {
	var t Template
	index := make(map[string]int)
	explicit := make(map[string]bool)
	var literal strings.Builder
	flush := func() {
		if literal.Len() > 0 {
			t.segments = append(t.segments, segment{literal: literal.String(), param: -1})
			literal.Reset()
		}
	}
	for i := 0; i < len(src); {
		switch {
		case strings.HasPrefix(src[i:], "{{"):
			literal.WriteByte('{')
			i += 2
		case strings.HasPrefix(src[i:], "}}"):
			literal.WriteByte('}')
			i += 2
		case src[i] == '}':
			return Template{}, fmt.Errorf("unmatched %q at index %d", "}", i)
		case src[i] == '{':
			end := strings.IndexByte(src[i:], '}')
			if end < 0 {
				return Template{}, fmt.Errorf("unclosed placeholder at index %d", i)
			}
			p, hasKind, err := parsePlaceholder(src[i+1 : i+end])
			if err != nil {
				return Template{}, fmt.Errorf("placeholder at index %d: %w", i, err)
			}
			idx, seen := index[p.Name]
			if !seen {
				idx = len(t.params)
				index[p.Name] = idx
				t.params = append(t.params, p)
			}
			if hasKind {
				if explicit[p.Name] && t.params[idx].Kind != p.Kind {
					return Template{}, fmt.Errorf("parameter %q declared as both %s and %s", p.Name, t.params[idx].Kind, p.Kind)
				}
				t.params[idx].Kind = p.Kind
				explicit[p.Name] = true
			}
			flush()
			t.segments = append(t.segments, segment{param: idx})
			i += end + 1
		default:
			literal.WriteByte(src[i])
			i++
		}
	}
	flush()
	t.explicit = explicit
	return t, nil
}

// Params returns the template parameters in order of first appearance.
func (t Template) Params() []Param {
	return slices.Clone(t.params)
}

// Explicit reports whether the parameter's kind was annotated explicitly in
// this template rather than defaulted to string.
func (t Template) Explicit(name string) bool {
	return t.explicit[name]
}

// Render assembles the template, resolving each placeholder through resolve.
func (t Template) Render(resolve func(p Param) string) string {
	var b strings.Builder
	for _, seg := range t.segments {
		if seg.param < 0 {
			b.WriteString(seg.literal)
			continue
		}
		b.WriteString(resolve(t.params[seg.param]))
	}
	return b.String()
}

// Segments returns the parsed template in the runtime's embedded form, which
// is what the generator writes into the output. A bare placeholder carries the
// kind it inherited from an explicit annotation elsewhere in the template.
func (t Template) Segments() []i18n.Segment {
	segs := make([]i18n.Segment, 0, len(t.segments))
	for _, seg := range t.segments {
		if seg.param < 0 {
			segs = append(segs, i18n.Segment{Text: seg.literal})
			continue
		}
		p := t.params[seg.param]
		segs = append(segs, i18n.Segment{Param: p.Name, Kind: p.Kind})
	}
	return segs
}

// ValidName reports whether s is a valid parameter or catalog key segment
// name: a lowercase letter followed by lowercase letters, digits, or
// underscores ([a-z][a-z0-9_]*).
func ValidName(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		switch {
		case 'a' <= c && c <= 'z':
		case c == '_' || ('0' <= c && c <= '9'):
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// segment is either a literal text (param < 0) or a placeholder referencing
// Template.params by index.
type segment struct {
	literal string
	param   int
}

func parsePlaceholder(body string) (Param, bool, error) {
	name, kindName, hasKind := strings.Cut(body, ":")
	p := Param{Name: name}
	if hasKind {
		kind, ok := parseKind(kindName)
		if !ok {
			return Param{}, false, fmt.Errorf("unknown kind %q", kindName)
		}
		p.Kind = kind
	}
	if !ValidName(name) {
		return Param{}, false, fmt.Errorf("invalid name %q", name)
	}
	return p, hasKind, nil
}

func parseKind(s string) (Kind, bool) {
	switch s {
	case "string":
		return KindString, true
	case "int":
		return KindInt, true
	case "number":
		return KindNumber, true
	default:
		return 0, false
	}
}
