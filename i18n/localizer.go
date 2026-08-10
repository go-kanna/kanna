package i18n

import (
	"fmt"
	"math"
	"reflect"
	"strconv"

	"golang.org/x/text/feature/plural"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/number"
)

// Localizer renders messages in a fixed language. Obtain one from
// Bundle.Localizer. The zero value renders every message as its key.
type Localizer struct {
	layers []layer
}

// Localizer returns a Localizer for the loaded locale best matching tag
// (e.g., en-US matches en). Messages missing from the matched locale fall
// back through the loaded parents of its language tag (en-GB falls back to
// en) and finally through the default language, if its catalog is present.
func (b *Bundle) Localizer(tag language.Tag) Localizer {
	if b.matcher == nil {
		return Localizer{}
	}
	_, idx, _ := b.matcher.Match(tag)
	return Localizer{layers: b.chains[idx]}
}

// Localize renders the message in the Localizer's language. It never fails:
// a message missing from the language falls back to the default language
// (when its catalog is present) and finally to the message key itself, and a
// missing argument renders as the placeholder name in braces.
func (l Localizer) Localize(m Message) string {
	for _, ly := range l.layers {
		entry, ok := ly.entries[m.Key]
		if !ok {
			continue
		}
		return ly.render(entry, m)
	}
	return m.Key
}

// layer is one language in the fallback chain of a Localizer. Plural rules
// and number formatting follow the layer's own language, so a message that
// falls back to the default language is rendered entirely under the default
// language's conventions.
type layer struct {
	tag     language.Tag
	entries map[string]Entry
	printer *message.Printer
}

func (b *Bundle) layer(tag language.Tag) layer {
	return layer{
		tag:     tag,
		entries: b.catalogs[tag].Entries,
		printer: b.printers[tag],
	}
}

func (ly layer) render(entry Entry, m Message) string {
	tmpl := entry.Single
	if entry.Plural != nil {
		tmpl = ly.pluralVariant(entry, m)
	}
	return tmpl.render(func(name string, kind Kind) string {
		v, ok := lookupArg(m, name)
		if !ok {
			return "{" + name + "}"
		}
		return ly.format(v, kind)
	})
}

// pluralVariant selects the plural form for the message's count argument.
// Without a usable count, and for CLDR forms the catalog does not provide,
// it falls back to "other".
func (ly layer) pluralVariant(entry Entry, m Message) Template {
	form := "other"
	if v, ok := lookupArg(m, CountParam); ok {
		if n, ok := asInt(v); ok {
			form = formName(pluralForm(ly.tag, n))
		}
	}
	tmpl, ok := entry.Plural[form]
	if !ok {
		tmpl = entry.Plural["other"]
	}
	return tmpl
}

// format renders an argument. A value of a parameter annotated :number is
// formatted with the layer's locale conventions (e.g., 1,234.56) whatever
// its numeric type; otherwise the value type decides: strings verbatim,
// integers plain, and floats locale-formatted. Named types (type Price
// float64) are followed to their underlying kind via reflection.
func (ly layer) format(v any, kind Kind) string {
	// The concrete types cover everything a generated constructor passes; the
	// reflective cases below exist for hand-built Messages carrying named types
	// (type Price float64), and pay for exactly one reflect.ValueOf.
	switch v := v.(type) {
	case string:
		return v
	case int:
		if kind == KindNumber {
			return ly.printer.Sprint(number.Decimal(v))
		}
		return strconv.Itoa(v)
	case float64:
		return ly.printer.Sprint(number.Decimal(v))
	case float32:
		return ly.printer.Sprint(number.Decimal(v))
	case nil:
		return fmt.Sprint(v)
	}

	val := reflect.ValueOf(v)
	switch val.Kind() { //nolint:exhaustive // remaining kinds render via fmt.Sprint
	case reflect.String:
		return val.String()
	case reflect.Float32, reflect.Float64:
		return ly.printer.Sprint(number.Decimal(val.Float()))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if kind == KindNumber {
			return ly.printer.Sprint(number.Decimal(val.Int()))
		}
		if n := val.Int(); n >= math.MinInt && n <= math.MaxInt {
			return strconv.Itoa(int(n))
		}
		return fmt.Sprint(v)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if kind == KindNumber {
			return ly.printer.Sprint(number.Decimal(val.Uint()))
		}
		if n := val.Uint(); n <= math.MaxInt {
			return strconv.Itoa(int(n))
		}
		return fmt.Sprint(v)
	default:
		return fmt.Sprint(v)
	}
}

// asInt converts any integer value, including named integer types, to int
// for plural form matching. Generated code always passes int; this keeps
// hand-built Messages working.
func asInt(v any) (int, bool) {
	if n, ok := v.(int); ok {
		return n, true
	}
	if v == nil {
		return 0, false
	}
	val := reflect.ValueOf(v)
	switch val.Kind() { //nolint:exhaustive // non-integer kinds are not counts
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n := val.Int()
		if n < math.MinInt || n > math.MaxInt {
			return 0, false
		}
		return int(n), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n := val.Uint()
		if n > math.MaxInt {
			return 0, false
		}
		return int(n), true
	default:
		return 0, false
	}
}

func lookupArg(m Message, name string) (any, bool) {
	for _, a := range m.Args {
		if a.Name == name {
			return a.Value, true
		}
	}
	return nil, false
}

var formNames = map[plural.Form]string{
	plural.Zero:  "zero",
	plural.One:   "one",
	plural.Two:   "two",
	plural.Few:   "few",
	plural.Many:  "many",
	plural.Other: "other",
}

// formName returns the catalog category name of a CLDR plural form,
// defaulting to "other".
func formName(f plural.Form) string {
	if name, ok := formNames[f]; ok {
		return name
	}
	return "other"
}

func pluralForm(tag language.Tag, n int) plural.Form {
	if n == math.MinInt {
		n++ // |math.MinInt| is not representable; nudge to keep the negation below valid
	}
	if n < 0 {
		n = -n
	}
	return plural.Cardinal.MatchPlural(tag, n, 0, 0, 0, 0)
}
