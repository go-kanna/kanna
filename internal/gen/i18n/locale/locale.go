// Package locale parses locale files (YAML or TOML) into flat message
// catalogs.
//
// Nested mappings are flattened into dot-joined keys (user.not_found). A
// mapping whose keys are all CLDR plural categories (zero, one, two, few,
// many, other) is a plural group rather than nesting; it must define "other".
package locale

import (
	"fmt"
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
var PluralCategories = i18n.PluralCategories

// Entry is a single message in a catalog.
type Entry struct {
	Key    string
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
	Tag     language.Tag
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
		return Catalog{}, fmt.Errorf("%s: %w", path, err)
	}
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
			return fmt.Errorf("invalid key %q%s: must match [a-z][a-z0-9_]*", ch.key, at(ch.line))
		}
		if seen[ch.key] {
			return fmt.Errorf("duplicate key %q%s", ch.key, at(ch.line))
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
			entries[key] = Entry{Key: key, Single: tmpl}
			continue
		}
		if len(ch.node.children) == 0 {
			return fmt.Errorf("key %q%s: empty mapping", key, at(ch.node.line))
		}
		plural, err := isPluralGroup(key, ch.node)
		if err != nil {
			return err
		}
		if plural {
			entry, err := parsePluralGroup(key, ch.node)
			if err != nil {
				return err
			}
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
		return template.Template{}, fmt.Errorf("key %q%s: %w", key, at(n.line), err)
	}
	return tmpl, nil
}

// isPluralGroup reports whether the mapping is a plural group: all of its
// keys are plural categories. A mix of plural categories and other keys is
// an error.
func isPluralGroup(key string, n node) (bool, error) {
	pluralKeys := 0
	for _, ch := range n.children {
		if isPluralCategory(ch.key) {
			pluralKeys++
		}
	}
	if pluralKeys == 0 {
		return false, nil
	}
	if pluralKeys != len(n.children) {
		return false, fmt.Errorf("key %q%s: cannot mix plural categories with other keys", key, at(n.line))
	}
	return true, nil
}

func parsePluralGroup(key string, n node) (Entry, error) {
	variants := make(map[string]template.Template, len(n.children))
	for _, ch := range n.children {
		if _, ok := variants[ch.key]; ok {
			return Entry{}, fmt.Errorf("duplicate key %q%s", ch.key, at(ch.line))
		}
		variantKey := key + "." + ch.key
		if ch.node.mapping {
			return Entry{}, fmt.Errorf("key %q%s: plural form must be a string", variantKey, at(ch.node.line))
		}
		tmpl, err := parseTemplate(variantKey, ch.node)
		if err != nil {
			return Entry{}, err
		}
		for _, p := range tmpl.Params() {
			if p.Name == CountParam && p.Kind == template.KindNumber {
				return Entry{}, fmt.Errorf("key %q%s: parameter %q must be int in plural forms",
					variantKey, at(ch.node.line), CountParam)
			}
		}
		variants[ch.key] = tmpl
	}
	if _, ok := variants["other"]; !ok {
		return Entry{}, fmt.Errorf("key %q%s: plural group must define %q", key, at(n.line), "other")
	}
	return Entry{Key: key, Plural: variants}, nil
}

func isPluralCategory(s string) bool {
	return slices.Contains(PluralCategories, s)
}

func at(line int) string {
	if line == 0 {
		return ""
	}
	return fmt.Sprintf(" (line %d)", line)
}
