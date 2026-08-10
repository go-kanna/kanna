package i18n_test

import (
	"math"
	"strconv"
	"testing"

	"golang.org/x/text/language"

	"github.com/go-kanna/kanna/i18n"
)

// The catalogs below are what kanna-i18n compiles the following locales into;
// the assertions on them are ported unchanged from the run-time-loading
// implementation, so they pin that embedding changed no rendering behavior.
//
//	# en.yaml
//	greeting: "Hello!"
//	hello: "Hello, {name}!"
//	items_count:
//	  one: "You have {count} item."
//	  other: "You have {count} items."
//	total_price: "Total: {price:number}"
//	user:
//	  not_found: "User not found."
//	  deleted: "User {name} has been deleted."
//
// ja.user.deleted is intentionally missing to exercise the fallback chain,
// and items_count has only "other" because Japanese has no "one" form.
func newBundle() *i18n.Bundle {
	return i18n.NewBundle("en",
		i18n.Catalog{Lang: "en", Entries: map[string]i18n.Entry{
			"greeting": {Single: tmpl(text("Hello!"))},
			"hello":    {Single: tmpl(text("Hello, "), param("name"), text("!"))},
			"items_count": {Plural: map[string]i18n.Template{
				"one":   tmpl(text("You have "), param("count"), text(" item.")),
				"other": tmpl(text("You have "), param("count"), text(" items.")),
			}},
			"total_price":    {Single: tmpl(text("Total: "), number("price"))},
			"user.not_found": {Single: tmpl(text("User not found."))},
			"user.deleted":   {Single: tmpl(text("User "), param("name"), text(" has been deleted."))},
		}},
		i18n.Catalog{Lang: "ja", Entries: map[string]i18n.Entry{
			"greeting": {Single: tmpl(text("こんにちは！"))},
			"hello":    {Single: tmpl(text("こんにちは、"), param("name"), text("さん！"))},
			"items_count": {Plural: map[string]i18n.Template{
				"other": tmpl(text("アイテムが"), param("count"), text("個あります。")),
			}},
			"total_price":    {Single: tmpl(text("合計: "), number("price"))},
			"user.not_found": {Single: tmpl(text("ユーザーが見つかりません。"))},
		}},
	)
}

func tmpl(segs ...i18n.Segment) i18n.Template {
	return i18n.Template{Segments: segs}
}

func text(s string) i18n.Segment {
	return i18n.Segment{Text: s}
}

func param(name string) i18n.Segment {
	return i18n.Segment{Param: name}
}

func number(name string) i18n.Segment {
	return i18n.Segment{Param: name, Kind: i18n.KindNumber}
}

// Named types exercise the reflection fallback in plural selection and
// argument formatting.
type (
	quantity int
	amount   float64
	username string
)

func msg(key string, args ...i18n.Arg) i18n.Message {
	return i18n.Message{Key: key, Args: args}
}

func TestLocalizer_Localize(t *testing.T) {
	t.Parallel()

	b := newBundle()
	en := b.Localizer(language.English)
	ja := b.Localizer(language.Japanese)

	tests := []struct {
		name string
		loc  i18n.Localizer
		msg  i18n.Message
		want string
	}{
		{name: "en static", loc: en, msg: msg("greeting"), want: "Hello!"},
		{
			name: "en string arg",
			loc:  en,
			msg:  msg("hello", i18n.Arg{Name: "name", Value: "World"}),
			want: "Hello, World!",
		},
		{
			name: "en plural one",
			loc:  en,
			msg:  msg("items_count", i18n.Arg{Name: "count", Value: 1}),
			want: "You have 1 item.",
		},
		{
			name: "en plural other",
			loc:  en,
			msg:  msg("items_count", i18n.Arg{Name: "count", Value: 5}),
			want: "You have 5 items.",
		},
		{
			name: "en plural zero uses other",
			loc:  en,
			msg:  msg("items_count", i18n.Arg{Name: "count", Value: 0}),
			want: "You have 0 items.",
		},
		{
			name: "en plural negative count",
			loc:  en,
			msg:  msg("items_count", i18n.Arg{Name: "count", Value: -1}),
			want: "You have -1 item.",
		},
		{
			name: "en number formatting",
			loc:  en,
			msg:  msg("total_price", i18n.Arg{Name: "price", Value: 1234.56}),
			want: "Total: 1,234.56",
		},
		{
			name: "number annotation formats int values",
			loc:  en,
			msg:  msg("total_price", i18n.Arg{Name: "price", Value: 1234567}),
			want: "Total: 1,234,567",
		},
		{
			name: "number annotation formats float32 values",
			loc:  en,
			msg:  msg("total_price", i18n.Arg{Name: "price", Value: float32(1234.5)}),
			want: "Total: 1,234.5",
		},
		{
			name: "named int type selects plural form",
			loc:  en,
			msg:  msg("items_count", i18n.Arg{Name: "count", Value: quantity(1)}),
			want: "You have 1 item.",
		},
		{
			name: "named float type with number annotation",
			loc:  en,
			msg:  msg("total_price", i18n.Arg{Name: "price", Value: amount(1234.56)}),
			want: "Total: 1,234.56",
		},
		{
			name: "named string type renders verbatim",
			loc:  en,
			msg:  msg("hello", i18n.Arg{Name: "name", Value: username("World")}),
			want: "Hello, World!",
		},
		{name: "en nested key", loc: en, msg: msg("user.not_found"), want: "User not found."},
		{name: "ja static", loc: ja, msg: msg("greeting"), want: "こんにちは！"},
		{
			name: "ja string arg",
			loc:  ja,
			msg:  msg("hello", i18n.Arg{Name: "name", Value: "太郎"}),
			want: "こんにちは、太郎さん！",
		},
		{
			name: "ja plural count 1 uses other",
			loc:  ja,
			msg:  msg("items_count", i18n.Arg{Name: "count", Value: 1}),
			want: "アイテムが1個あります。",
		},
		{
			name: "ja number formatting",
			loc:  ja,
			msg:  msg("total_price", i18n.Arg{Name: "price", Value: 1234.56}),
			want: "合計: 1,234.56",
		},
		{
			name: "ja missing key falls back to default language",
			loc:  ja,
			msg:  msg("user.deleted", i18n.Arg{Name: "name", Value: "Alice"}),
			want: "User Alice has been deleted.",
		},
		{name: "unknown key returns the key", loc: en, msg: msg("nope.key"), want: "nope.key"},
		{name: "missing arg renders placeholder", loc: en, msg: msg("hello"), want: "Hello, {name}!"},
		{
			name: "unexpected arg type falls back to fmt",
			loc:  en,
			msg:  msg("hello", i18n.Arg{Name: "name", Value: 42}),
			want: "Hello, 42!",
		},
		{
			name: "plural without count uses other",
			loc:  en,
			msg:  msg("items_count"),
			want: "You have {count} items.",
		},
		{
			name: "plural count as int64",
			loc:  en,
			msg:  msg("items_count", i18n.Arg{Name: "count", Value: int64(1)}),
			want: "You have 1 item.",
		},
		{
			name: "plural count as uint",
			loc:  en,
			msg:  msg("items_count", i18n.Arg{Name: "count", Value: uint(5)}),
			want: "You have 5 items.",
		},
		{
			name: "int32 arg renders plain",
			loc:  en,
			msg:  msg("hello", i18n.Arg{Name: "name", Value: int32(42)}),
			want: "Hello, 42!",
		},
		{
			name: "uint64 arg beyond int range still renders",
			loc:  en,
			msg:  msg("hello", i18n.Arg{Name: "name", Value: uint64(math.MaxUint64)}),
			want: "Hello, 18446744073709551615!",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.loc.Localize(tt.msg); got != tt.want {
				t.Errorf("Localize(%q) = %q, want %q", tt.msg.Key, got, tt.want)
			}
		})
	}
}

func TestBundle_Localizer_matching(t *testing.T) {
	t.Parallel()

	b := newBundle()

	tests := []struct {
		name string
		tag  language.Tag
		want string
	}{
		{name: "exact match", tag: language.Japanese, want: "こんにちは！"},
		{name: "region narrows to base language", tag: language.MustParse("ja-JP"), want: "こんにちは！"},
		{name: "en-US matches en", tag: language.AmericanEnglish, want: "Hello!"},
		{name: "unloaded language falls back to default", tag: language.French, want: "Hello!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := b.Localizer(tt.tag).Localize(msg("greeting")); got != tt.want {
				t.Errorf("Localizer(%v).Localize(greeting) = %q, want %q", tt.tag, got, tt.want)
			}
		})
	}
}

// The Russian tag is used only for its CLDR rules: modulo-based plural rules
// panic inside x/text when given a negative operand, guarding the
// math.MinInt negation overflow. English-like equality rules never hit it.
func TestLocalizer_pluralMinIntCount(t *testing.T) {
	t.Parallel()

	b := i18n.NewBundle("ru",
		i18n.Catalog{Lang: "ru", Entries: map[string]i18n.Entry{
			"items": {Plural: map[string]i18n.Template{
				"one":   tmpl(text("one:"), param("count")),
				"few":   tmpl(text("few:"), param("count")),
				"many":  tmpl(text("many:"), param("count")),
				"other": tmpl(text("other:"), param("count")),
			}},
		}},
	)
	got := b.Localizer(language.Russian).Localize(msg("items", i18n.Arg{Name: "count", Value: math.MinInt}))
	if want := "many:" + strconv.Itoa(math.MinInt); got != want {
		t.Errorf("Localize(items) = %q, want %q", got, want)
	}
}

func TestBundle_Localizer_germanNumberFormat(t *testing.T) {
	t.Parallel()

	b := i18n.NewBundle("en",
		i18n.Catalog{Lang: "de", Entries: map[string]i18n.Entry{
			"total_price": {Single: tmpl(text("Gesamt: "), number("price"))},
		}},
	)
	de := b.Localizer(language.German)
	got := de.Localize(msg("total_price", i18n.Arg{Name: "price", Value: 1234.56}))
	if want := "Gesamt: 1.234,56"; got != want {
		t.Errorf("Localize(total_price) = %q, want %q", got, want)
	}
}

func TestBundle_Localizer_parentChainFallback(t *testing.T) {
	t.Parallel()

	b := i18n.NewBundle("ja",
		i18n.Catalog{Lang: "ja", Entries: map[string]i18n.Entry{
			"greeting": {Single: tmpl(text("こんにちは！"))},
			"only_ja":  {Single: tmpl(text("ja only"))},
		}},
		i18n.Catalog{Lang: "en", Entries: map[string]i18n.Entry{
			"greeting": {Single: tmpl(text("Hello!"))},
			"only_en":  {Single: tmpl(text("EN only"))},
		}},
		i18n.Catalog{Lang: "en-GB", Entries: map[string]i18n.Entry{
			"greeting": {Single: tmpl(text("Hello, mate!"))},
		}},
	)

	enGB := b.Localizer(language.BritishEnglish)
	tests := []struct {
		name string
		key  string
		want string
	}{
		{name: "own catalog wins", key: "greeting", want: "Hello, mate!"},
		{name: "parent language before default", key: "only_en", want: "EN only"},
		{name: "default language last", key: "only_ja", want: "ja only"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := enGB.Localize(msg(tt.key)); got != tt.want {
				t.Errorf("Localize(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestBundle_Localizer_withoutDefaultCatalog(t *testing.T) {
	t.Parallel()

	// English itself is never included.
	b := i18n.NewBundle("en",
		i18n.Catalog{Lang: "ja", Entries: map[string]i18n.Entry{
			"greeting": {Single: tmpl(text("こんにちは！"))},
		}},
	)
	if got := b.Localizer(language.French).Localize(msg("greeting")); got != "こんにちは！" {
		t.Errorf("Localize(greeting) = %q, want こんにちは！", got)
	}
}

func TestLocalizer_zeroValue(t *testing.T) {
	t.Parallel()

	var zero i18n.Localizer
	if got := zero.Localize(msg("greeting")); got != "greeting" {
		t.Errorf("Localize(greeting) = %q, want %q", got, "greeting")
	}
	if got := i18n.NewBundle("en").Localizer(language.English).Localize(msg("x")); got != "x" {
		t.Errorf("empty bundle Localize(x) = %q, want %q", got, "x")
	}
}
