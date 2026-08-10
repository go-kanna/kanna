// Package locale parses locale files (YAML or TOML) into flat message
// catalogs.
//
// Nested mappings are flattened into dot-joined keys (user.not_found). A
// message declares its plural forms under an explicit "plural" mapping, keyed
// by CLDR category (zero, one, two, few, many, other) and always defining
// "other". The marker is explicit so that intent never has to be guessed from
// shape: a mapping that merely happens to use category names as keys is
// ordinary nesting.
package locale

import (
	"errors"
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/text/language"

	"github.com/go-kanna/kanna/i18n"
	"github.com/go-kanna/kanna/internal/gen/i18n/template"
)

// CountParam is the parameter that selects the plural form of a message. It is
// the runtime's constant: what is validated against it here is selected by it
// there.
const CountParam = i18n.CountParam

// PluralCategories lists the CLDR plural categories in canonical order.
var PluralCategories = i18n.PluralCategories()

// Error is a problem in a locale file, positioned where it sits. Line is 0
// when the format provides no position (TOML); ParseFile fills Filename in.
//
// Carrying the position as data rather than formatted into the message is what
// lets the caller hand it to the diag layer, where editors and CI can consume
// file:line references.
type Error struct {
	Pos token.Position
	Msg string
}

func (e *Error) Error() string {
	if e.Pos.Filename == "" && !e.Pos.IsValid() {
		return e.Msg
	}
	return e.Pos.String() + ": " + e.Msg
}

// errorAt builds a positioned Error; line 0 means unknown.
func errorAt(line int, format string, args ...any) error {
	return &Error{Pos: token.Position{Line: line}, Msg: fmt.Sprintf(format, args...)}
}

// Entry is a single message in a catalog.
type Entry struct {
	Key string

	// Line is the position of the key in its file; 0 when the format provides
	// no position info.
	Line int

	Single template.Template            // valid when Plural is nil
	Plural map[string]template.Template // keyed by CLDR category; nil unless plural
}

// Params returns the parameters of the entry in generation order. Plural
// entries always take CountParam (int) first; parameters of all variants
// follow by first appearance in canonical category order. As within a single
// template, a bare placeholder inherits a kind annotated explicitly in
// another variant; only conflicting explicit annotations are an error.
func (e Entry) Params() ([]template.Param, error) {
	if e.Plural == nil {
		return e.Single.Params(), nil
	}
	params := []template.Param{{Name: CountParam, Kind: template.KindInt}}
	index := map[string]int{CountParam: 0}
	explicit := map[string]bool{CountParam: true}
	for _, category := range PluralCategories {
		tmpl, ok := e.Plural[category]
		if !ok {
			continue
		}
		for _, p := range tmpl.Params() {
			if p.Name == CountParam {
				continue
			}
			at, seen := index[p.Name]
			if !seen {
				index[p.Name] = len(params)
				explicit[p.Name] = tmpl.Explicit(p.Name)
				params = append(params, p)
				continue
			}
			if !tmpl.Explicit(p.Name) {
				continue
			}
			if explicit[p.Name] && params[at].Kind != p.Kind {
				return nil, fmt.Errorf(
					"key %q: parameter %q is %s in one plural form and %s in another",
					e.Key, p.Name, params[at].Kind, p.Kind,
				)
			}
			params[at].Kind = p.Kind
			explicit[p.Name] = true
		}
	}
	return params, nil
}

// Catalog is the set of messages of a single language.
type Catalog struct {
	Tag language.Tag

	// File is the path the catalog was parsed from; empty when it came from
	// bytes.
	File string

	Entries map[string]Entry
}

// ParseFile parses a locale file, deriving the language from the filename
// stem (e.g., "en.yaml" is English) and the format from the extension.
func ParseFile(path string) (Catalog, error) {
	parse, ok := parserFor(path)
	if !ok {
		return Catalog{}, fmt.Errorf("%s: unsupported file extension %q", path, filepath.Ext(path))
	}
	tag, err := TagFromPath(path)
	if err != nil {
		return Catalog{}, err
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path is supplied by the library user by design
	if err != nil {
		return Catalog{}, fmt.Errorf("read locale file: %w", err)
	}
	c, err := parse(tag, data)
	if err != nil {
		var pe *Error
		if errors.As(err, &pe) {
			pe.Pos.Filename = path
			return Catalog{}, pe
		}
		return Catalog{}, fmt.Errorf("%s: %w", path, err)
	}
	c.File = path
	return c, nil
}

// SupportedFile reports whether the file has a supported locale format
// extension (.yaml, .yml, or .toml, case-insensitive).
func SupportedFile(path string) bool {
	_, ok := parserFor(path)
	return ok
}

// parserFor maps a file extension to its parser. It is the single registry
// of supported locale formats.
func parserFor(path string) (func(language.Tag, []byte) (Catalog, error), bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		return ParseYAML, true
	case ".toml":
		return ParseTOML, true
	default:
		return nil, false
	}
}

// TagFromPath derives the language tag from the filename stem.
func TagFromPath(path string) (language.Tag, error) {
	base := filepath.Base(path)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	tag, err := language.Parse(stem)
	if err != nil {
		return language.Und, fmt.Errorf("cannot derive language from %q: %w", base, err)
	}
	return tag, nil
}

// node is a format-independent document node: either a string scalar or a
// mapping. Format front ends reject anything else during conversion.
type node struct {
	line     int // 1-based; 0 when the format provides no position info
	mapping  bool
	str      string  // scalar value; valid when mapping is false
	children []child // valid when mapping is true
}

type child struct {
	key  string
	line int // position of the key; 0 when unknown
	node node
}

func catalogFrom(tag language.Tag, root node) (Catalog, error) {
	c := Catalog{Tag: tag, Entries: make(map[string]Entry)}
	if err := walkMapping(root, "", c.Entries); err != nil {
		return Catalog{}, err
	}
	return c, nil
}

func walkMapping(n node, prefix string, entries map[string]Entry) error {
	seen := make(map[string]bool)
	for _, ch := range n.children {
		if !template.ValidName(ch.key) {
			return errorAt(ch.line, "invalid key %q: must match [a-z][a-z0-9_]*", ch.key)
		}
		if seen[ch.key] {
			return errorAt(ch.line, "duplicate key %q", ch.key)
		}
		seen[ch.key] = true
		key := ch.key
		if prefix != "" {
			key = prefix + "." + ch.key
		}
		if !ch.node.mapping {
			tmpl, err := parseTemplate(key, ch.node)
			if err != nil {
				return err
			}
			entries[key] = Entry{Key: key, Line: ch.line, Single: tmpl}
			continue
		}
		if len(ch.node.children) == 0 {
			return errorAt(ch.node.line, "key %q: empty mapping", key)
		}
		if group, ok := pluralMarker(ch.node); ok {
			if len(ch.node.children) > 1 {
				return errorAt(ch.node.line,
					"key %q: %q cannot share its mapping with other keys", key, pluralKey)
			}
			entry, err := parsePluralGroup(key, group)
			if err != nil {
				return err
			}
			entry.Line = ch.line
			entries[key] = entry
			continue
		}
		if err := walkMapping(ch.node, key, entries); err != nil {
			return err
		}
	}
	return nil
}

func parseTemplate(key string, n node) (template.Template, error) {
	tmpl, err := template.Parse(n.str)
	if err != nil {
		return template.Template{}, errorAt(n.line, "key %q: %v", key, err)
	}
	return tmpl, nil
}

// pluralKey is the mapping key that declares a message's plural forms.
const pluralKey = "plural"

// pluralMarker returns the plural-forms mapping when n declares one: a child
// named "plural" whose value is a mapping. A scalar child of that name is an
// ordinary message, so the only name a locale file cannot use freely is a
// mapping-valued "plural".
func pluralMarker(n node) (node, bool) {
	for _, ch := range n.children {
		if ch.key == pluralKey && ch.node.mapping {
			return ch.node, true
		}
	}
	return node{}, false
}

func parsePluralGroup(key string, n node) (Entry, error) {
	variants := make(map[string]template.Template, len(n.children))
	for _, ch := range n.children {
		if !isPluralCategory(ch.key) {
			return Entry{}, errorAt(ch.line, "key %q: %q is not a plural category (%s)",
				key, ch.key, strings.Join(PluralCategories, ", "))
		}
		if _, ok := variants[ch.key]; ok {
			return Entry{}, errorAt(ch.line, "duplicate key %q", ch.key)
		}
		variantKey := key + "." + ch.key
		if ch.node.mapping {
			return Entry{}, errorAt(ch.node.line, "key %q: plural form must be a string", variantKey)
		}
		tmpl, err := parseTemplate(variantKey, ch.node)
		if err != nil {
			return Entry{}, err
		}
		for _, p := range tmpl.Params() {
			if p.Name == CountParam && p.Kind == template.KindNumber {
				return Entry{}, errorAt(ch.node.line, "key %q: parameter %q must be int in plural forms",
					variantKey, CountParam)
			}
		}
		variants[ch.key] = tmpl
	}
	if _, ok := variants["other"]; !ok {
		return Entry{}, errorAt(n.line, "key %q: plural group must define %q", key, "other")
	}
	return Entry{Key: key, Plural: variants}, nil
}

func isPluralCategory(s string) bool {
	return slices.Contains(PluralCategories, s)
}
