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
	if err := gofakeit.Seed(1); err != nil {
		panic(err)
	}

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

	// A //kanna:table struct with required belongs_to relations also gets a
	// graph: the record plus everything it needs to exist, foreign keys wired,
	// records in insertion order.
	g := fixture.NewEmployeeGraph(func(g *fixture.EmployeeGraph) {
		g.Department.Name = "Engineering"
	})
	if g.Employee.DepartmentID != g.Department.ID || g.Department.CompanyID != g.Company.ID {
		panic("graph keys are not wired")
	}
	if g.Employee.ManagerID != nil {
		panic("the optional manager relation crept into the graph")
	}
	fmt.Printf("employee %s works in %s at %s\n", g.Employee.Name, g.Department.Name, g.Company.Name)
	fmt.Printf("records to insert, parents first: %d\n", len(g.Records()))

	// Sharing a parent is ordinary assignment; Wire makes the keys follow.
	colleague := fixture.NewEmployeeGraph()
	colleague.Company = g.Company
	colleague.Department = g.Department
	colleague.Wire()
	if colleague.Employee.DepartmentID != g.Employee.DepartmentID {
		panic("shared department did not rewire")
	}
	fmt.Printf("%s joins the same department\n", colleague.Employee.Name)
}
