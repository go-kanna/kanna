package i18n

import (
	"golang.org/x/text/feature/plural"
	"golang.org/x/text/language"

	"github.com/go-kanna/kanna/internal/gen/i18n/locale"
)

// usedCategories reports the plural categories a language's CLDR cardinal
// rules can actually produce for integer counts, in canonical order.
//
// x/text does not expose the category set of a language, so it is derived by
// sampling MatchPlural over enough integers to trip every rule: the rules
// branch on small values and on remainders modulo 10, 100, and (for a handful
// of languages such as Breton) 1,000,000, so the small range plus the powers
// of ten covers them.
func usedCategories(tag language.Tag) []string {
	found := make(map[string]bool, 3)
	sample := func(n int) {
		found[formName(plural.Cardinal.MatchPlural(tag, n, 0, 0, 0, 0))] = true
	}
	for n := range 1001 {
		sample(n)
	}
	for _, n := range []int{10_000, 100_000, 1_000_000, 2_000_000} {
		sample(n)
	}

	out := make([]string, 0, len(found))
	for _, category := range locale.PluralCategories {
		if found[category] {
			out = append(out, category)
		}
	}
	return out
}

// missingCategories reports the categories in used that the entry does not
// provide. Each is rendered with the "other" form at run time, which is
// usually grammatically wrong — the language distinguishes them for a
// reason — so the gap is worth a warning even though nothing crashes.
func missingCategories(used []string, entry locale.Entry) []string {
	var missing []string
	for _, category := range used {
		if _, ok := entry.Plural[category]; !ok {
			missing = append(missing, category)
		}
	}
	return missing
}

// formName is defined in the runtime for rendering; the generator needs the
// same mapping for validation.
func formName(f plural.Form) string {
	switch f { //nolint:exhaustive // plural.Other and anything unknown default to "other"
	case plural.Zero:
		return "zero"
	case plural.One:
		return "one"
	case plural.Two:
		return "two"
	case plural.Few:
		return "few"
	case plural.Many:
		return "many"
	default:
		return "other"
	}
}
