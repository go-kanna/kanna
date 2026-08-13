package relation_test

import (
	"strings"
	"testing"

	"github.com/go-kanna/kanna/internal/diag"
	"github.com/go-kanna/kanna/internal/packages"
	"github.com/go-kanna/kanna/internal/pkgtest"
	"github.com/go-kanna/kanna/internal/relation"
	"github.com/go-kanna/kanna/internal/scan"
)

func graphsOf(t *testing.T, src string) ([]relation.Graph, []diag.Diag) {
	t.Helper()

	pkg := pkgtest.LoadFileAs(t, "model", src)
	structs, ds := scan.Structs([]*packages.Package{pkg})
	if diag.HasErrors(ds) {
		t.Fatalf("scan: %s", diag.Format(ds))
	}
	return relation.BuildGraphs(structs)
}

func mustGraphs(t *testing.T, src string) []relation.Graph {
	t.Helper()

	graphs, ds := graphsOf(t, src)
	if diag.HasErrors(ds) {
		t.Fatalf("unexpected errors: %s", diag.Format(ds))
	}
	return graphs
}

func graphFor(t *testing.T, graphs []relation.Graph, root string) relation.Graph {
	t.Helper()

	for _, g := range graphs {
		if g.Root == root {
			return g
		}
	}
	t.Fatalf("no graph for %s in %+v", root, graphs)
	return relation.Graph{}
}

func wantGraphError(t *testing.T, src, substr string) {
	t.Helper()

	_, ds := graphsOf(t, src)
	if !diag.HasErrors(ds) {
		t.Fatalf("expected error containing %q, got none", substr)
	}
	if got := diag.Format(ds); !strings.Contains(got, substr) {
		t.Errorf("diags = %q, want substring %q", got, substr)
	}
}

const chainModel = `package model

//kanna:table
type Company struct {
	ID   int64
	Name string
}

//kanna:table
type Department struct {
	ID        int64
	CompanyID int64
	Company   *Company ` + "`orm:\"belongs_to,foreign_key:company_id\"`" + `
	Name      string
}

//kanna:table
type Employee struct {
	ID           int64
	DepartmentID int64
	Department   *Department ` + "`orm:\"belongs_to,foreign_key:department_id\"`" + `
	ManagerID    *int64
	Manager      *Employee ` + "`orm:\"belongs_to,foreign_key:manager_id\"`" + `
	Name         string
}
`

func TestBuildGraphsChain(t *testing.T) {
	t.Parallel()

	graphs := mustGraphs(t, chainModel)
	if len(graphs) != 2 {
		t.Fatalf("graphs = %d (%+v), want 2 (Department, Employee)", len(graphs), graphs)
	}

	g := graphFor(t, graphs, "Employee")
	wantNodes := []string{"Company", "Department", "Employee"}
	if len(g.Nodes) != len(wantNodes) {
		t.Fatalf("nodes = %+v, want %v", g.Nodes, wantNodes)
	}
	for i, want := range wantNodes {
		if g.Nodes[i].Table != want || g.Nodes[i].Field != want {
			t.Errorf("node %d = %+v, want table and field %q", i, g.Nodes[i], want)
		}
		if g.Nodes[i].PKField != "ID" || !g.Nodes[i].PKIsInt {
			t.Errorf("node %d key = %+v, want integer ID", i, g.Nodes[i])
		}
	}

	// The nullable Manager edge must neither add a node nor wire anything.
	if len(g.Wires) != 2 {
		t.Fatalf("wires = %+v, want 2", g.Wires)
	}
	w := g.Wires[0]
	if w.FKField != "CompanyID" || g.Nodes[w.From].Table != "Department" || g.Nodes[w.To].Table != "Company" {
		t.Errorf("wire 0 = %+v", w)
	}
	w = g.Wires[1]
	if w.FKField != "DepartmentID" || g.Nodes[w.From].Table != "Employee" || g.Nodes[w.To].Table != "Department" {
		t.Errorf("wire 1 = %+v", w)
	}

	// Department needs only Company.
	d := graphFor(t, graphs, "Department")
	if len(d.Nodes) != 2 || d.Nodes[0].Table != "Company" || d.Nodes[1].Table != "Department" {
		t.Errorf("Department graph nodes = %+v", d.Nodes)
	}
}

const twoParentsModel = `package model

//kanna:table
type User struct {
	ID   string
	Name string
}

//kanna:table
type Post struct {
	ID       int64
	AuthorID string
	Author   User ` + "`orm:\"belongs_to,foreign_key:author_id\"`" + `
	EditorID string
	Editor   User ` + "`orm:\"belongs_to,foreign_key:editor_id\"`" + `
}
`

func TestBuildGraphsTwoParentsOfOneTable(t *testing.T) {
	t.Parallel()

	g := graphFor(t, mustGraphs(t, twoParentsModel), "Post")
	fields := []string{g.Nodes[0].Field, g.Nodes[1].Field, g.Nodes[2].Field}
	want := []string{"Author", "Editor", "Post"}
	for i := range want {
		if fields[i] != want[i] {
			t.Errorf("fields = %v, want %v", fields, want)
			break
		}
	}
	if g.Nodes[0].PKIsInt {
		t.Error("string primary key reported as integer")
	}
	if len(g.Wires) != 2 {
		t.Fatalf("wires = %+v, want 2", g.Wires)
	}
}

const diamondModel = `package model

//kanna:table
type Company struct {
	ID int64
}

//kanna:table
type User struct {
	ID        int64
	CompanyID int64
	Company   *Company ` + "`orm:\"belongs_to,foreign_key:company_id\"`" + `
}

//kanna:table
type Post struct {
	ID       int64
	AuthorID int64
	Author   User ` + "`orm:\"belongs_to,foreign_key:author_id\"`" + `
	EditorID int64
	Editor   User ` + "`orm:\"belongs_to,foreign_key:editor_id\"`" + `
}
`

func TestBuildGraphsDiamondNaming(t *testing.T) {
	t.Parallel()

	g := graphFor(t, mustGraphs(t, diamondModel), "Post")

	fields := make([]string, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		fields = append(fields, n.Field)
	}
	want := []string{"AuthorCompany", "Author", "EditorCompany", "Editor", "Post"}
	for i := range want {
		if i >= len(fields) || fields[i] != want[i] {
			t.Fatalf("fields = %v, want %v", fields, want)
		}
	}
}

func TestBuildGraphsRequiredCycle(t *testing.T) {
	t.Parallel()

	wantGraphError(t, `package model

//kanna:table
type A struct {
	ID        int64
	PartnerID int64
	Partner   *B `+"`orm:\"belongs_to,foreign_key:partner_id\"`"+`
}

//kanna:table
type B struct {
	ID      int64
	OwnerID int64
	Owner   *A `+"`orm:\"belongs_to,foreign_key:owner_id\"`"+`
}
`, "required relations cycle back through")
}

func TestBuildGraphsMissingForeignKeyColumn(t *testing.T) {
	t.Parallel()

	wantGraphError(t, `package model

//kanna:table
type Company struct {
	ID int64
}

//kanna:table
type Department struct {
	ID      int64
	Company *Company `+"`orm:\"belongs_to,foreign_key:company_id\"`"+`
}
`, `foreign_key "company_id" is not a column of Department`)
}

func TestBuildGraphsTargetNotATable(t *testing.T) {
	t.Parallel()

	wantGraphError(t, `package model

type Company struct {
	ID int64
}

//kanna:table
type Department struct {
	ID        int64
	CompanyID int64
	Company   *Company `+"`orm:\"belongs_to,foreign_key:company_id\"`"+`
}
`, "relation target Company is not a //kanna:table struct")
}

func TestBuildGraphsNullableParentsMakeNoGraph(t *testing.T) {
	t.Parallel()

	graphs := mustGraphs(t, `package model

//kanna:table
type User struct {
	ID int64
}

//kanna:table
type Session struct {
	ID     int64
	UserID *int64
	User   *User `+"`orm:\"belongs_to,foreign_key:user_id\"`"+`
}
`)
	if len(graphs) != 0 {
		t.Errorf("graphs = %+v, want none: the only parent is optional", graphs)
	}
}

func TestBuildGraphsNonComparableDuplicateKey(t *testing.T) {
	t.Parallel()

	wantGraphError(t, `package model

//kanna:table
type User struct {
	ID []byte
}

//kanna:table
type Post struct {
	ID       int64
	AuthorID []byte
	Author   User `+"`orm:\"belongs_to,foreign_key:author_id\"`"+`
	EditorID []byte
	Editor   User `+"`orm:\"belongs_to,foreign_key:editor_id\"`"+`
}
`, "primary key type is not comparable")
}
