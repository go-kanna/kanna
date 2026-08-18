package relation

import (
	"go/token"
	"go/types"
	"slices"
	"strings"

	"github.com/go-kanna/kanna/internal/diag"
	"github.com/go-kanna/kanna/internal/directive"
	"github.com/go-kanna/kanna/internal/ir"
)

// Graph is the requirement closure of one root table: every record an
// instance of the root needs to exist, in foreign-key insertion order, with
// the assignments that keep the keys consistent.
type Graph struct {
	Root  string      // root table struct name
	Nodes []GraphNode // parents before children; the root is the last node
	Wires []GraphWire
	Pos   token.Position // the root struct's position
}

// GraphNode is one record in the closure.
type GraphNode struct {
	Table   string // struct name
	Field   string // name the node goes by in the graph, unique within it
	PKField string // the table's primary-key Go field
}

// GraphWire is one foreign-key assignment: node From's FKField takes node
// To's primary key.
type GraphWire struct {
	From, To int // indices into Nodes
	FKField  string
}

// graphTable is a //kanna:table struct interpreted as far as the graph needs.
type graphTable struct {
	name         string
	pos          token.Position
	pkField      string
	pkType       types.Type
	pkComparable bool
	parents      []graphParent
	broken       bool // carries error diagnostics; graphs touching it are skipped
}

// graphParent is one belongs_to edge: the declaring table needs the target
// when the foreign key cannot be NULL.
type graphParent struct {
	field    string // relation field name
	target   string // target struct name
	fkField  string // Go field holding the foreign key
	fkType   types.Type
	required bool // the foreign-key field is not a pointer
	pos      token.Position
}

// BuildGraphs interprets the //kanna:table structs of a package and returns a
// graph for every table that cannot exist alone — one with at least one
// belongs_to whose foreign key is not nullable. Malformed input the graph
// depends on is a positioned error, never a silent skip; tables no graph
// touches are not examined beyond their tags.
func BuildGraphs(structs []ir.Struct) ([]Graph, []diag.Diag) {
	tables, diags := graphTables(structs)

	var graphs []Graph
	for _, s := range structs {
		t, ok := tables[s.Name]
		if !ok {
			continue
		}
		g, ds := buildGraph(t, tables)
		diags = append(diags, ds...)
		if g != nil {
			graphs = append(graphs, *g)
		}
	}
	return graphs, diags
}

// graphTables interprets every //kanna:table struct: its primary key and its
// belongs_to edges, on top of the shared field classification. Structs
// without the directive are not tables and are ignored entirely.
func graphTables(structs []ir.Struct) (map[string]graphTable, []diag.Diag) {
	tables := make(map[string]graphTable)
	var diags []diag.Diag

	for _, s := range structs {
		d, _ := directive.Find(s.Doc, "table")
		if !d.Found {
			continue
		}

		c := classifyTable(s)
		diags = append(diags, c.diags...)
		t := graphTable{name: s.Name, pos: s.Pos, broken: c.broken}

		for _, r := range c.relations {
			if r.tag.Kind != "belongs_to" {
				continue // children and join tables are not requirements
			}
			p, ds := parentEdge(s, r.field, r.tag)
			diags = append(diags, ds...)
			if p == nil {
				t.broken = true
				continue
			}
			t.parents = append(t.parents, *p)
		}

		candidates := make([]PKCandidate, len(c.columns))
		for i, col := range c.columns {
			candidates[i] = PKCandidate{Name: col.field.Name, Explicit: col.explicitPK}
		}
		if picks := PickPrimaryKey(candidates); len(picks) == 1 {
			pk := c.columns[picks[0]].field
			t.pkField = pk.Name
			t.pkType = pk.Type
			t.pkComparable = types.Comparable(pk.Type)
		}

		// The foreign-key columns of this table's own edges resolve here,
		// where the column model is at hand.
		for i := range t.parents {
			p := &t.parents[i]
			col, found := columnNamed(c.columns, p.fkField)
			if !found {
				diags = append(diags, diag.Errorf(p.pos,
					"%s.%s: foreign_key %q is not a column of %s", s.Name, p.field, p.fkField, s.Name))
				t.broken = true
				continue
			}
			p.fkField = col.field.Name
			p.fkType = col.field.Type
			_, isPtr := PointerElem(col.field.Type)
			p.required = !isPtr
		}

		tables[s.Name] = t
	}

	return tables, diags
}

// parentEdge interprets one belongs_to field far enough to know what it
// requires. The foreign-key column resolves later, once the declaring table's
// column model exists; until then fkField carries the column name.
func parentEdge(s ir.Struct, f ir.Field, rel *RelationTag) (*graphParent, []diag.Diag) {
	core := f.Type
	if elem, isPtr := PointerElem(core); isPtr {
		core = elem
	}
	named, ok := StructNamed(core)
	if !ok {
		return nil, []diag.Diag{diag.Errorf(f.Pos,
			"%s.%s: a belongs_to field must be a struct or a pointer to one", s.Name, f.Name)}
	}
	if p := named.Obj().Pkg(); p != nil && s.Named != nil && p != s.Named.Obj().Pkg() {
		return nil, []diag.Diag{diag.Errorf(f.Pos,
			"%s.%s: relation target %s lives in %s; relations resolve within one package",
			s.Name, f.Name, named.Obj().Name(), p.Path())}
	}

	return &graphParent{
		field:   f.Name,
		target:  named.Obj().Name(),
		fkField: rel.ForeignKey, // still a column name here
		pos:     f.Pos,
	}, nil
}

// column is one column-backed field with its resolved column name.
type column struct {
	field      ir.Field
	name       string
	explicitPK bool
}

// columnNamed finds the column carrying the given column name.
func columnNamed(columns []column, name string) (column, bool) {
	for _, c := range columns {
		if c.name == name {
			return c, true
		}
	}
	return column{}, false
}

// graphBuilder accumulates one graph during the walk from its root.
type graphBuilder struct {
	tables map[string]graphTable
	g      Graph
	paths  [][]string // parallel to g.Nodes: relation fields from the root
	onPath map[string]bool
	diags  []diag.Diag
}

// buildGraph assembles the closure for one root, or nothing when the root
// stands alone. Any table the closure touches must be intact: a broken or
// keyless requirement fails the graph rather than producing a partial one.
func buildGraph(root graphTable, tables map[string]graphTable) (*Graph, []diag.Diag) {
	if !hasRequired(root) {
		return nil, nil
	}
	if root.broken {
		return nil, nil // its own diagnostics explain why
	}

	b := &graphBuilder{
		tables: tables,
		g:      Graph{Root: root.name, Pos: root.pos},
		onPath: map[string]bool{root.name: true},
	}
	if _, ok := b.add(root, nil); !ok {
		return nil, b.diags
	}
	if ds := b.nameNodes(); len(ds) > 0 {
		return nil, append(b.diags, ds...)
	}
	if ds := b.checkDuplicates(); len(ds) > 0 {
		return nil, append(b.diags, ds...)
	}
	return &b.g, b.diags
}

func hasRequired(t graphTable) bool {
	for _, p := range t.parents {
		if p.required {
			return true
		}
	}
	return false
}

// add appends t's required parents and then t itself, returning t's node
// index. The path holds the relation fields walked from the root.
func (b *graphBuilder) add(t graphTable, path []string) (int, bool) {
	type pending struct {
		fkField string
		parent  int
	}
	var wires []pending

	for _, p := range t.parents {
		if !p.required {
			continue
		}
		target, ok := b.tables[p.target]
		if !ok {
			b.diags = append(b.diags, diag.Errorf(p.pos,
				"%s.%s: relation target %s is not a //kanna:table struct", t.name, p.field, p.target))
			return 0, false
		}
		if target.broken {
			return 0, false // its own diagnostics explain why
		}
		if target.pkField == "" {
			b.diags = append(b.diags, diag.Errorf(target.pos,
				"%s has no primary key; name a field ID or tag one with orm:\",primary_key\"", target.name))
			return 0, false
		}
		if b.onPath[p.target] {
			b.diags = append(b.diags, diag.Errorf(p.pos,
				"%s.%s: required relations cycle back through %s, so no insertion order can satisfy them",
				t.name, p.field, p.target))
			return 0, false
		}

		fkCore := p.fkType
		if elem, isPtr := PointerElem(fkCore); isPtr {
			fkCore = elem
		}
		if !types.Identical(fkCore, target.pkType) {
			b.diags = append(b.diags, diag.Errorf(p.pos,
				"%s.%s: foreign key %s.%s has type %s, but the %s primary key is %s",
				t.name, p.field, t.name, p.fkField, typeName(p.fkType), target.name, typeName(target.pkType)))
			return 0, false
		}

		b.onPath[p.target] = true
		parentIdx, ok := b.add(target, append(slices.Clone(path), p.field))
		delete(b.onPath, p.target)
		if !ok {
			return 0, false
		}
		wires = append(wires, pending{fkField: p.fkField, parent: parentIdx})
	}

	b.g.Nodes = append(b.g.Nodes, GraphNode{Table: t.name, PKField: t.pkField})
	b.paths = append(b.paths, path)
	self := len(b.g.Nodes) - 1
	for _, w := range wires {
		b.g.Wires = append(b.g.Wires, GraphWire{From: self, To: w.parent, FKField: w.fkField})
	}
	return self, true
}

// nameNodes settles each node's field name: the relation field that pulled it
// in, extended upward through the path while two nodes still read the same.
// The root, whose path is empty, goes by its table name.
func (b *graphBuilder) nameNodes() []diag.Diag {
	names := make([]string, len(b.g.Nodes))
	depth := make([]int, len(b.g.Nodes))
	for i := range names {
		depth[i] = 1
		names[i] = b.nodeName(i, 1)
	}

	for {
		collides := make(map[string][]int, len(names))
		for i, n := range names {
			collides[n] = append(collides[n], i)
		}

		grew := false
		var stuck []string
		for name, group := range collides {
			if len(group) < 2 {
				continue
			}
			groupGrew := false
			for _, i := range group {
				if depth[i] < len(b.paths[i]) {
					depth[i]++
					names[i] = b.nodeName(i, depth[i])
					groupGrew = true
				}
			}
			if groupGrew {
				grew = true
			} else {
				stuck = append(stuck, name)
			}
		}

		if len(stuck) > 0 {
			slices.Sort(stuck)
			return []diag.Diag{diag.Errorf(b.g.Pos,
				"%s: graph field name %s is claimed by more than one relation path; rename one of the fields",
				b.g.Root, strings.Join(stuck, ", "))}
		}
		if !grew {
			break
		}
	}

	// The graph type declares these methods, and a field sharing a method's
	// name does not compile.
	for _, n := range names {
		if n == "Wire" || n == "Records" {
			return []diag.Diag{diag.Errorf(b.g.Pos,
				"%s: a graph field would be named %s, which collides with the generated method; rename the relation field",
				b.g.Root, n)}
		}
	}

	for i, n := range names {
		b.g.Nodes[i].Field = n
	}
	return nil
}

// nodeName joins the last depth path elements; the root falls back to its
// table name.
func (b *graphBuilder) nodeName(i, depth int) string {
	path := b.paths[i]
	if len(path) == 0 {
		return b.g.Nodes[i].Table
	}
	return strings.Join(path[len(path)-min(depth, len(path)):], "")
}

// checkDuplicates rejects a graph holding several records of a table whose
// primary key is not comparable: telling shared nodes apart in Records
// compares keys, and that comparison has to compile.
func (b *graphBuilder) checkDuplicates() []diag.Diag {
	counts := make(map[string]int, len(b.g.Nodes))
	for _, n := range b.g.Nodes {
		counts[n.Table]++
	}
	// Walk nodes, not the map, so the same table is named on every run.
	for _, n := range b.g.Nodes {
		if counts[n.Table] < 2 || b.tables[n.Table].pkComparable {
			continue
		}
		return []diag.Diag{diag.Errorf(b.g.Pos,
			"%s: %s appears more than once in the graph, but its primary key type is not comparable, "+
				"and telling shared records apart compares keys", b.g.Root, n.Table)}
	}
	return nil
}

// typeName renders a type with package-name qualifiers, for diagnostics.
func typeName(t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string { return p.Name() })
}
