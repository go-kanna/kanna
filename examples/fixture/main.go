// Command fixture builds model values from the fixtures kanna-fixture generated,
// showing that the output is ordinary Go you call directly.
//
// Regenerate with:
//
//	go generate ./...
package main

import (
	"fmt"

	"github.com/brianvoe/gofakeit/v7"

	"github.com/go-kanna/kanna/examples/fixture/fixture"
	"github.com/go-kanna/kanna/examples/fixture/model"
)

//go:generate go tool kanna-fixture -source ./model -destination ./fixture

func main() {
	// Generation is deterministic; the values are not. Seed the faker when a
	// test needs the same data twice.
	gofakeit.Seed(1)

	// Every field arrives filled, so a test only states what it cares about.
	published := fixture.Article(func(m *model.Article) {
		m.Title = "Generating fixtures"
		m.Status = model.StatusPublished
	})

	fmt.Printf("%s by %s (%s)\n", published.Title, published.Author.Name, published.Status)
	fmt.Printf("id=%s slug=%s\n", published.ID, published.Slug)

	// Setters compose, which is what makes a shared helper worth having.
	withAuthor := func(name string) func(*model.Article) {
		return func(m *model.Article) { m.Author.Name = name }
	}

	draft := fixture.Article(withAuthor("Bob"), func(m *model.Article) {
		m.Status = model.StatusDraft
	})

	fmt.Printf("%s by %s (%s)\n", draft.Title, draft.Author.Name, draft.Status)

	// A pointer field stays nil, and an ignored struct gets no fixture at all.
	user := fixture.User()
	fmt.Printf("nickname is nil: %t\n", user.Nickname == nil)
}
