// Command i18n renders the same messages in two languages, showing what
// kanna-i18n generates: typed constructors, and the translations themselves —
// no locale file is read at run time, and there is nothing to set up.
//
// Regenerate with:
//
//	go generate ./...
package main

import (
	"fmt"

	"golang.org/x/text/language"

	"github.com/go-kanna/kanna/examples/i18n/messages"
)

//go:generate go tool kanna-i18n

func main() {
	// A Localizer comes straight from the generated package. Compare the
	// run-time-loading shape this replaces: build a bundle, load each locale
	// file, hope the paths hold at run time.
	fmt.Println("=== English ===")
	en := messages.Localizer(language.English)
	fmt.Println(en.Localize(messages.Greeting()))
	fmt.Println(en.Localize(messages.Hello("World")))
	fmt.Println(en.Localize(messages.ItemsCount(1))) // plural: one
	fmt.Println(en.Localize(messages.ItemsCount(5))) // plural: other
	fmt.Println(en.Localize(messages.TotalPrice(1234.56)))
	fmt.Println(en.Localize(messages.UserNotFound()))
	fmt.Println(en.Localize(messages.UserDeleted("Alice")))

	fmt.Println("\n=== Japanese ===")
	ja := messages.Localizer(language.Japanese)
	fmt.Println(ja.Localize(messages.Greeting()))
	fmt.Println(ja.Localize(messages.Hello("太郎")))
	fmt.Println(ja.Localize(messages.ItemsCount(1))) // plural: other (Japanese has no "one" form)
	fmt.Println(ja.Localize(messages.ItemsCount(5))) // plural: other
	fmt.Println(ja.Localize(messages.TotalPrice(1234.56)))
	fmt.Println(ja.Localize(messages.UserNotFound()))
	fmt.Println(ja.Localize(messages.UserDeleted("Alice")))
}
