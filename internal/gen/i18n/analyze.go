package i18n

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/text/language"

	"github.com/go-kanna/kanna/internal/gen/i18n/locale"
	"github.com/go-kanna/kanna/internal/gen/i18n/template"
)

// Model is the input to code generation. The signatures derive from the
// default locale; the catalogs are every locale, because the translations are
// embedded in the output rather than loaded beside it.
type Model struct {
	DefaultTag language.Tag
	Messages   []Message        // sorted by key
	Catalogs   []locale.Catalog // default first, rest sorted by tag
}

// Message is one message constructor to generate.
type Message struct {
	Key      string
	FuncName string
	Plural   bool
	Params   []Param
}

// Param is one parameter of a generated constructor.
type Param struct {
	Name   string // placeholder name in the template
	GoName string // camelCase Go parameter name
	GoType string
}

// Warning is a non-fatal finding, such as a locale missing translations.
type Warning string

// Analyze loads every locale file in dir, builds the generation model from
// the default locale, and validates the other locales against it.
func Analyze(dir string, defaultLang language.Tag) (Model, []Warning, error) {
	catalogs, warnings, err := loadCatalogs(dir)
	if err != nil {
		return Model{}, nil, err
	}
	def, defWarnings, err := defaultCatalog(catalogs, defaultLang, dir)
	if err != nil {
		return Model{}, nil, err
	}
	warnings = append(warnings, defWarnings...)
	model, err := buildModel(def)
	if err != nil {
		return Model{}, nil, err
	}
	index := messageIndex(model)
	for _, c := range catalogs {
		if c.Tag == def.Tag {
			continue
		}
		w, err := crossCheck(model, index, c)
		if err != nil {
			return Model{}, nil, err
		}
		warnings = append(warnings, w...)
	}
	model.Catalogs = orderCatalogs(catalogs, def.Tag)
	return model, warnings, nil
}

// orderCatalogs puts the default locale first and sorts the rest by tag, so
// the embedded bundle is deterministic and reads default-first, the way the
// runtime treats it.
func orderCatalogs(catalogs []locale.Catalog, def language.Tag) []locale.Catalog {
	out := make([]locale.Catalog, 0, len(catalogs))
	for _, c := range catalogs {
		if c.Tag == def {
			out = append(out, c)
		}
	}
	rest := make([]locale.Catalog, 0, len(catalogs))
	for _, c := range catalogs {
		if c.Tag != def {
			rest = append(rest, c)
		}
	}
	slices.SortFunc(rest, func(a, b locale.Catalog) int {
		return strings.Compare(a.Tag.String(), b.Tag.String())
	})
	return append(out, rest...)
}

func messageIndex(m Model) map[string]Message {
	index := make(map[string]Message, len(m.Messages))
	for _, msg := range m.Messages {
		index[msg.Key] = msg
	}
	return index
}

// defaultCatalog resolves the default locale among the loaded catalogs the
// same way the runtime matches languages, so en-US.yaml satisfies -default
// en. Unrelated languages are rejected, but same-base variants are accepted
// even at low matcher confidence (zh-Hant for zh scores Low despite being
// the right pick). An exact tag match wins outright; when several variants
// could serve a bare default, the implicit CLDR pick is reported as a
// warning.
func defaultCatalog(catalogs []locale.Catalog, lang language.Tag, dir string) (locale.Catalog, []Warning, error) {
	tags := make([]language.Tag, len(catalogs))
	for i, c := range catalogs {
		if c.Tag == lang {
			return c, nil, nil
		}
		tags[i] = c.Tag
	}
	_, idx, conf := language.NewMatcher(tags).Match(lang)
	matched := catalogs[idx]
	if conf < language.High && !sameBase(lang, matched.Tag) {
		return locale.Catalog{}, nil, fmt.Errorf("default locale %s not found in %s (available: %v)", lang, dir, tags)
	}
	var warnings []Warning
	if candidates := sameBaseTags(tags, lang); len(candidates) > 1 {
		warnings = append(warnings, Warning(fmt.Sprintf(
			"default locale %s is ambiguous (candidates: %v); using %s, pass an exact -default to override",
			lang, candidates, matched.Tag,
		)))
	}
	return matched, warnings, nil
}

func sameBaseTags(tags []language.Tag, lang language.Tag) []language.Tag {
	var out []language.Tag
	for _, t := range tags {
		if sameBase(t, lang) {
			out = append(out, t)
		}
	}
	return out
}

func sameBase(a, b language.Tag) bool {
	baseA, _ := a.Base()
	baseB, _ := b.Base()
	return baseA == baseB
}

// loadCatalogs loads every locale file in dir. Files whose stem is not a
// language tag (config.yaml and the like) are skipped with a warning rather
// than failing the run, keeping typos visible without banning cohabitation.
func loadCatalogs(dir string) ([]locale.Catalog, []Warning, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("read locale directory: %w", err)
	}
	seen := make(map[language.Tag]string)
	var catalogs []locale.Catalog
	var warnings []Warning
	for _, e := range entries {
		if e.IsDir() || !locale.SupportedFile(e.Name()) {
			continue
		}
		// Pre-check the stem separately from ParseFile so that non-locale
		// files are skipped with a warning while broken locale files below
		// still fail the run. The duplicate tag derivation is deliberate.
		if _, err := locale.TagFromPath(e.Name()); err != nil {
			warnings = append(warnings, Warning(fmt.Sprintf("skipping %s: %v", e.Name(), err)))
			continue
		}
		c, err := locale.ParseFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, nil, fmt.Errorf("load locale file: %w", err)
		}
		if prev, ok := seen[c.Tag]; ok {
			return nil, nil, fmt.Errorf("locale %s defined by both %s and %s", c.Tag, prev, e.Name())
		}
		seen[c.Tag] = e.Name()
		catalogs = append(catalogs, c)
	}
	if len(catalogs) == 0 {
		return nil, nil, fmt.Errorf("no locale files found in %s", dir)
	}
	return catalogs, warnings, nil
}

func buildModel(def locale.Catalog) (Model, error) {
	keys := slices.Sorted(maps.Keys(def.Entries))
	funcNames := make(map[string]string, len(keys))
	messages := make([]Message, 0, len(keys))
	for _, key := range keys {
		entry := def.Entries[key]
		params, err := entry.Params()
		if err != nil {
			return Model{}, fmt.Errorf("locale %s: %w", def.Tag, err)
		}
		msg, err := buildMessage(entry, params)
		if err != nil {
			return Model{}, fmt.Errorf("locale %s: %w", def.Tag, err)
		}
		if prev, ok := funcNames[msg.FuncName]; ok {
			return Model{}, fmt.Errorf("keys %q and %q both generate func %s", prev, key, msg.FuncName)
		}
		funcNames[msg.FuncName] = key
		messages = append(messages, msg)
	}
	return Model{DefaultTag: def.Tag, Messages: messages}, nil
}

func buildMessage(entry locale.Entry, params []template.Param) (Message, error) {
	msg := Message{
		Key:      entry.Key,
		FuncName: FuncName(entry.Key),
		Plural:   entry.Plural != nil,
		Params:   make([]Param, 0, len(params)),
	}
	goNames := make(map[string]string, len(params))
	for _, p := range params {
		goName, err := ParamName(p.Name)
		if err != nil {
			return Message{}, fmt.Errorf("key %q: %w", entry.Key, err)
		}
		if goName == importAlias {
			return Message{}, fmt.Errorf(
				"key %q: parameter %q conflicts with the generated import alias %q",
				entry.Key, p.Name, importAlias,
			)
		}
		if prev, ok := goNames[goName]; ok {
			return Message{}, fmt.Errorf(
				"key %q: parameters %q and %q both map to Go parameter %q",
				entry.Key, prev, p.Name, goName,
			)
		}
		goNames[goName] = p.Name
		msg.Params = append(msg.Params, Param{Name: p.Name, GoName: goName, GoType: template.GoType(p.Kind)})
	}
	return msg, nil
}

// crossCheck validates a translation against the generation model: unknown
// keys, shape mismatches, and unknown parameters are errors, while keys
// missing from the translation are warnings because the runtime falls back
// to the default language. index maps message keys to model messages and is
// built once by Analyze.
func crossCheck(model Model, index map[string]Message, other locale.Catalog) ([]Warning, error) {
	for _, key := range slices.Sorted(maps.Keys(other.Entries)) {
		entry := other.Entries[key]
		defMsg, ok := index[key]
		if !ok {
			return nil, fmt.Errorf("locale %s: key %q does not exist in default locale %s", other.Tag, key, model.DefaultTag)
		}
		if (entry.Plural != nil) != defMsg.Plural {
			return nil, fmt.Errorf("locale %s: key %q: plural shape differs from default locale", other.Tag, key)
		}
		params, err := entry.Params()
		if err != nil {
			return nil, fmt.Errorf("locale %s: %w", other.Tag, err)
		}
		for _, p := range params {
			at := slices.IndexFunc(defMsg.Params, func(dp Param) bool { return dp.Name == p.Name })
			if at < 0 {
				return nil, fmt.Errorf(
					"locale %s: key %q: parameter %q does not exist in default locale",
					other.Tag, key, p.Name,
				)
			}
			// A bare placeholder cannot be distinguished from an explicit
			// :string annotation, so only non-string kinds are compared.
			if p.Kind != template.KindString && template.GoType(p.Kind) != defMsg.Params[at].GoType {
				return nil, fmt.Errorf(
					"locale %s: key %q: parameter %q has type %s, but default locale has %s",
					other.Tag, key, p.Name, template.GoType(p.Kind), defMsg.Params[at].GoType,
				)
			}
		}
	}
	var missing []string
	for _, msg := range model.Messages {
		if _, ok := other.Entries[msg.Key]; !ok {
			missing = append(missing, msg.Key)
		}
	}
	if len(missing) > 0 {
		return []Warning{Warning(fmt.Sprintf("locale %s: missing keys: %s", other.Tag, strings.Join(missing, ", ")))}, nil
	}
	return nil, nil
}
