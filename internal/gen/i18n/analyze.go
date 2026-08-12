package i18n

import (
	"errors"
	"fmt"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/text/language"

	"github.com/go-kanna/kanna/internal/diag"
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

// Analyze loads every locale file in dir, builds the generation model from
// the default locale, and validates the other locales against it.
//
// Problems come back as diagnostics, errors and warnings alike, positioned in
// the locale files where the positions are known. The model is only meaningful
// when diag.HasErrors reports false.
func Analyze(dir string, defaultLang language.Tag) (Model, []diag.Diag) {
	catalogs, diags := loadCatalogs(dir)
	if diag.HasErrors(diags) {
		return Model{}, diags
	}

	def, defDiags := defaultCatalog(catalogs, defaultLang, dir)
	diags = append(diags, defDiags...)
	if diag.HasErrors(diags) {
		return Model{}, diags
	}

	model, modelDiags := buildModel(def)
	diags = append(diags, modelDiags...)
	if diag.HasErrors(diags) {
		// The index is incomplete, and cross-checking translations against an
		// incomplete index would blame them for keys the default failed on.
		return Model{}, diags
	}

	index := messageIndex(model)
	for _, c := range catalogs {
		if c.Tag == def.Tag {
			continue
		}
		diags = append(diags, crossCheck(model, index, c)...)
	}
	model.Catalogs = orderCatalogs(catalogs, def.Tag)
	return model, diags
}

// entryPos points a diagnostic at an entry of a catalog. TOML provides no line
// numbers, in which case the position is the file alone.
func entryPos(c locale.Catalog, entry locale.Entry) token.Position {
	return token.Position{Filename: c.File, Line: entry.Line}
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
func defaultCatalog(catalogs []locale.Catalog, lang language.Tag, dir string) (locale.Catalog, []diag.Diag) {
	tags := make([]language.Tag, len(catalogs))
	for i, c := range catalogs {
		if c.Tag == lang {
			return c, nil
		}
		tags[i] = c.Tag
	}
	_, idx, conf := language.NewMatcher(tags).Match(lang)
	matched := catalogs[idx]
	if conf < language.High && !sameBase(lang, matched.Tag) {
		return locale.Catalog{}, []diag.Diag{diag.Errorf(token.Position{},
			"default locale %s not found in %s (available: %v)", lang, dir, tags)}
	}
	var diags []diag.Diag
	if candidates := sameBaseTags(tags, lang); len(candidates) > 1 {
		diags = append(diags, diag.Warningf(token.Position{Filename: matched.File},
			"default locale %s is ambiguous (candidates: %v); using %s, pass an exact -default to override",
			lang, candidates, matched.Tag))
	}
	return matched, diags
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

// loadCatalogs loads every locale file in dir. Discovery is deliberately flat
// and nothing locale-shaped is skipped in silence: an entry named like a
// locale that cannot be read — a subdirectory such as en/, or en.json — is an
// error, because that language would otherwise drop out of the bundle with
// the build staying green. Mere cohabitation stays non-fatal: files that are
// not locale files warn (config.yaml and the like), as does a directory
// holding locale files; dot-prefixed entries are silent. A file that fails to
// parse is reported and the rest are still read, so one run surfaces every
// broken file.
func loadCatalogs(dir string) ([]locale.Catalog, []diag.Diag) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, []diag.Diag{diag.Errorf(token.Position{}, "read locale directory: %v", err)}
	}
	seen := make(map[language.Tag]string)
	var catalogs []locale.Catalog
	var diags []diag.Diag
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(dir, name)
		if entryIsDir(e, path) {
			if _, err := language.Parse(name); err == nil {
				diags = append(diags, diag.Errorf(token.Position{},
					"locale %s is a directory: locale discovery is flat, one file per language", name).
					WithHints(fmt.Sprintf("merge its files into a single %s.yaml (or .yml/.toml) directly in %s", name, dir)))
			} else if holdsLocaleFiles(path) {
				diags = append(diags, diag.Warningf(token.Position{},
					"skipping directory %s: it holds locale files, but discovery is flat", name).
					WithHints("move them directly into "+dir))
			}
			continue
		}
		if !locale.SupportedFile(name) {
			if _, err := locale.TagFromPath(name); err == nil {
				diags = append(diags, diag.Errorf(token.Position{Filename: path},
					"cannot read %s: unsupported locale file extension %q", name, filepath.Ext(name)).
					WithHints("supported extensions: "+strings.Join(locale.SupportedExtensions(), ", ")))
			} else {
				diags = append(diags, diag.Warningf(token.Position{Filename: path},
					"skipping %s: unsupported extension %q", name, filepath.Ext(name)))
			}
			continue
		}
		// Pre-check the stem separately from ParseFile so that non-locale
		// files are skipped with a warning while broken locale files below
		// still fail the run. The duplicate tag derivation is deliberate.
		if _, err := locale.TagFromPath(e.Name()); err != nil {
			diags = append(diags, diag.Warningf(token.Position{Filename: path}, "skipping %s: %v", e.Name(), err))
			continue
		}
		c, err := locale.ParseFile(path)
		if err != nil {
			diags = append(diags, localeDiag(path, err))
			continue
		}
		if prev, ok := seen[c.Tag]; ok {
			diags = append(diags, diag.Errorf(token.Position{Filename: path},
				"locale %s defined by both %s and %s", c.Tag, prev, e.Name()))
			continue
		}
		seen[c.Tag] = e.Name()
		catalogs = append(catalogs, c)
	}
	if len(catalogs) == 0 && !diag.HasErrors(diags) {
		diags = append(diags, diag.Errorf(token.Position{}, "no locale files found in %s", dir))
	}
	return catalogs, diags
}

// entryIsDir reports whether the entry is a directory, following symlinks so
// a linked locale directory is diagnosed like a real one.
func entryIsDir(e os.DirEntry, path string) bool {
	if e.IsDir() {
		return true
	}
	if e.Type()&fs.ModeSymlink == 0 {
		return false
	}
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// holdsLocaleFiles reports whether the directory directly contains a supported
// locale file. One level only: the point is to recognize a nested layout, not
// to search for one.
func holdsLocaleFiles(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && locale.SupportedFile(e.Name()) {
			return true
		}
	}
	return false
}

// localeDiag converts a locale error into a diagnostic, keeping the position
// it carries and anchoring position-less ones to the file.
func localeDiag(path string, err error) diag.Diag {
	var pe *locale.Error
	if errors.As(err, &pe) {
		return diag.Errorf(pe.Pos, "%s", pe.Msg)
	}
	return diag.Errorf(token.Position{Filename: path}, "%v", err)
}

func buildModel(def locale.Catalog) (Model, []diag.Diag) {
	keys := slices.Sorted(maps.Keys(def.Entries))
	funcNames := make(map[string]string, len(keys))
	messages := make([]Message, 0, len(keys))
	used := usedCategories(def.Tag)
	var diags []diag.Diag
	for _, key := range keys {
		entry := def.Entries[key]
		pos := entryPos(def, entry)
		if entry.Plural != nil {
			if missing := missingCategories(used, entry); len(missing) > 0 {
				diags = append(diags, diag.Warningf(pos,
					"locale %s: key %q: missing plural forms %s; counts needing them render with %q",
					def.Tag, key, strings.Join(missing, ", "), "other"))
			}
		}
		params, err := entry.Params()
		if err != nil {
			diags = append(diags, diag.Errorf(pos, "locale %s: %v", def.Tag, err))
			continue
		}
		msg, err := buildMessage(entry, params)
		if err != nil {
			diags = append(diags, diag.Errorf(pos, "locale %s: %v", def.Tag, err))
			continue
		}
		// The generated file declares one function of its own; no message may
		// take its name, or the output stops compiling.
		if msg.FuncName == localizerFunc {
			diags = append(diags, diag.Errorf(pos,
				"key %q generates func %s, which the generated package reserves for its accessor", key, localizerFunc))
			continue
		}
		if prev, ok := funcNames[msg.FuncName]; ok {
			diags = append(diags, diag.Errorf(pos,
				"keys %q and %q both generate func %s", prev, key, msg.FuncName))
			continue
		}
		funcNames[msg.FuncName] = key
		messages = append(messages, msg)
	}
	return Model{DefaultTag: def.Tag, Messages: messages}, diags
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
// to the default language. Every problem in the catalog is reported, not just
// the first. index maps message keys to model messages and is built once by
// Analyze.
func crossCheck(model Model, index map[string]Message, other locale.Catalog) []diag.Diag {
	var diags []diag.Diag
	used := usedCategories(other.Tag)
	for _, key := range slices.Sorted(maps.Keys(other.Entries)) {
		entry := other.Entries[key]
		pos := entryPos(other, entry)
		defMsg, ok := index[key]
		if !ok {
			diags = append(diags, diag.Errorf(pos,
				"locale %s: key %q does not exist in default locale %s", other.Tag, key, model.DefaultTag))
			continue
		}
		if (entry.Plural != nil) != defMsg.Plural {
			diags = append(diags, diag.Errorf(pos,
				"locale %s: key %q: plural shape differs from default locale", other.Tag, key))
			continue
		}
		if entry.Plural != nil {
			if missing := missingCategories(used, entry); len(missing) > 0 {
				diags = append(diags, diag.Warningf(pos,
					"locale %s: key %q: missing plural forms %s; counts needing them render with %q",
					other.Tag, key, strings.Join(missing, ", "), "other"))
			}
		}
		params, err := entry.Params()
		if err != nil {
			diags = append(diags, diag.Errorf(pos, "locale %s: %v", other.Tag, err))
			continue
		}
		for _, p := range params {
			at := slices.IndexFunc(defMsg.Params, func(dp Param) bool { return dp.Name == p.Name })
			if at < 0 {
				diags = append(diags, diag.Errorf(pos,
					"locale %s: key %q: parameter %q does not exist in default locale", other.Tag, key, p.Name))
				continue
			}
			// A bare placeholder cannot be distinguished from an explicit
			// :string annotation, so only non-string kinds are compared.
			if p.Kind != template.KindString && template.GoType(p.Kind) != defMsg.Params[at].GoType {
				diags = append(diags, diag.Errorf(pos,
					"locale %s: key %q: parameter %q has type %s, but default locale has %s",
					other.Tag, key, p.Name, template.GoType(p.Kind), defMsg.Params[at].GoType))
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
		diags = append(diags, diag.Warningf(token.Position{Filename: other.File},
			"locale %s: missing keys: %s", other.Tag, strings.Join(missing, ", ")))
	}
	return diags
}
