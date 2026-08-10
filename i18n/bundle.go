package i18n

import (
	"fmt"
	"slices"
	"strings"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// Bundle holds the message catalogs of all of a package's languages. Generated
// code builds one at init and never mutates it, which is what makes the
// Localizers it hands out safe for concurrent use.
type Bundle struct {
	defaultTag language.Tag
	catalogs   map[language.Tag]Catalog
	printers   map[language.Tag]*message.Printer
	tags       []language.Tag // default first, rest sorted
	chains     [][]layer      // fallback chain per tags index
	matcher    language.Matcher
}

// NewBundle builds a Bundle from embedded catalogs. Messages missing from a
// requested language fall back to defaultLang, which every bundle must carry a
// catalog for. Passing no catalogs at all is allowed and yields a bundle whose
// Localizers render every message as its key.
//
// It panics on a malformed language tag, a duplicated language, or a missing
// default catalog: the catalogs are generated from files kanna-i18n already
// validated, so any of these means the caller assembled the bundle by hand and
// got it wrong, and there is no later moment at which the mistake would be
// more visible.
func NewBundle(defaultLang string, catalogs ...Catalog) *Bundle {
	b := &Bundle{
		defaultTag: mustTag(defaultLang),
		catalogs:   make(map[language.Tag]Catalog, len(catalogs)),
		printers:   make(map[language.Tag]*message.Printer, len(catalogs)),
	}

	for _, c := range catalogs {
		tag := mustTag(c.Lang)
		if _, ok := b.catalogs[tag]; ok {
			panic(fmt.Sprintf("i18n: locale %s appears twice", tag))
		}
		b.catalogs[tag] = c
		b.printers[tag] = message.NewPrinter(tag)
	}

	if _, ok := b.catalogs[b.defaultTag]; len(catalogs) > 0 && !ok {
		panic(fmt.Sprintf("i18n: default locale %s has no catalog", b.defaultTag))
	}

	b.buildMatcher()
	b.buildChains()
	return b
}

func mustTag(lang string) language.Tag {
	tag, err := language.Parse(lang)
	if err != nil {
		panic(fmt.Sprintf("i18n: invalid language tag %q: %v", lang, err))
	}
	return tag
}

// buildMatcher caches the matcher and its tag list so Localizer does not pay
// for matcher construction on every call. The default language comes first so
// that unmatched requests resolve to it; the rest are sorted for determinism.
//
// With no catalogs at all the matcher stays nil, which is what makes Localizer
// hand out its zero value instead of matching against nothing.
func (b *Bundle) buildMatcher() {
	if len(b.catalogs) == 0 {
		return
	}
	rest := make([]language.Tag, 0, len(b.catalogs)-1)
	for t := range b.catalogs {
		if t != b.defaultTag {
			rest = append(rest, t)
		}
	}
	slices.SortFunc(rest, func(a, b language.Tag) int { return strings.Compare(a.String(), b.String()) })
	b.tags = append([]language.Tag{b.defaultTag}, rest...)
	b.matcher = language.NewMatcher(b.tags)
}

// buildChains precomputes the fallback chain of every matchable tag. The chain
// depends only on which catalogs exist, and the bundle never changes after
// construction, so rebuilding it per Localizer call would buy nothing but
// allocations on every request.
func (b *Bundle) buildChains() {
	b.chains = make([][]layer, len(b.tags))
	for i, tag := range b.tags {
		var layers []layer
		seen := make(map[language.Tag]bool)
		for t := tag; t != language.Und && !seen[t]; t = t.Parent() {
			seen[t] = true
			if _, ok := b.catalogs[t]; ok {
				layers = append(layers, b.layer(t))
			}
		}
		if !seen[b.defaultTag] {
			layers = append(layers, b.layer(b.defaultTag))
		}
		b.chains[i] = layers
	}
}
