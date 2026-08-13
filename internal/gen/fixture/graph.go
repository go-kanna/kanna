package fixture

import (
	"go/types"
	"maps"
	"slices"

	"github.com/go-kanna/kanna/internal/diag"
	"github.com/go-kanna/kanna/internal/ir"
	"github.com/go-kanna/kanna/internal/relation"
)

// atomicImport is the package the primary-key counter comes from.
const atomicImport = "sync/atomic"

// GraphPlan is the resolved body of one graph: a type bundling a root record
// with every record it needs, a constructor, Wire, and Records.
type GraphPlan struct {
	Name  string // type name, e.g. "EmployeeGraph"
	Ctor  string // constructor name, e.g. "NewEmployeeGraph"
	Nodes []GraphNodePlan
	Wires []GraphWirePlan
	Loops []GraphLoopPlan
}

// GraphNodePlan is one record of a graph.
type GraphNodePlan struct {
	Field string // graph field name
	Type  string // struct name in the source package, doubling as the fixture call
	// CounterConv converts the shared counter's int64 for this node's key;
	// empty when the key is not counter-assigned.
	CounterConv string
	PKField     string
	// DedupAgainst names earlier fields holding the same table, whose keys
	// Records compares before appending this node: sharing a record is done
	// by assignment, and assignment makes the keys equal.
	DedupAgainst []string
}

// GraphWirePlan is one foreign-key assignment in Wire.
type GraphWirePlan struct {
	FromField, FKField, ToField, ToPKField string
}

// GraphLoopPlan regenerates a fake key until it differs from every earlier
// record of the same table, for keys the counter does not assign.
type GraphLoopPlan struct {
	Field, PKField string
	Against        []string
	Regen          string
	NeedsHelper    bool
}

// GraphPlans turns the relation graphs of the source package into emission
// plans. A graph whose record set cannot be built — a node without a fixture
// function, or a duplicated table whose key cannot be kept distinct — is
// skipped with a warning rather than generated broken.
func GraphPlans(structs []ir.Struct, targets []Target, pkgPath, pkgName string) ([]GraphPlan, []string, []diag.Diag) {
	graphs, diags := relation.BuildGraphs(structs)
	if diag.HasErrors(diags) || len(graphs) == 0 {
		return nil, nil, diags
	}

	byName := make(map[string]Target, len(targets))
	for _, tg := range targets {
		byName[tg.Name] = tg
	}

	inf := inferrer{pkgPath: pkgPath, pkgName: pkgName, targets: make(map[string]bool, len(targets))}
	for _, tg := range targets {
		inf.targets[tg.Name] = true
	}
	inf.graph = inf.referenceGraph(targets)

	var plans []GraphPlan
	imports := make(map[string]bool)

	for _, g := range graphs {
		plan, pkgs, ds := inf.graphPlan(g, byName)
		diags = append(diags, ds...)
		if plan == nil {
			continue
		}
		for _, p := range pkgs {
			imports[p] = true
		}
		plans = append(plans, *plan)
	}

	return plans, slices.Sorted(maps.Keys(imports)), diags
}

func (inf inferrer) graphPlan(g relation.Graph, targets map[string]Target) (*GraphPlan, []string, []diag.Diag) {
	plan := GraphPlan{Name: g.Root + "Graph", Ctor: "New" + g.Root + "Graph"}
	var pkgs []string

	seenByTable := make(map[string][]int)
	for i, node := range g.Nodes {
		target, ok := targets[node.Table]
		if !ok {
			return nil, nil, []diag.Diag{diag.Warningf(g.Pos,
				"skipping %s: %s has no fixture function to build the graph from", plan.Name, node.Table)}
		}

		np := GraphNodePlan{Field: node.Field, Type: node.Table, PKField: node.PKField}

		pk, found := fieldNamed(target, node.PKField)
		if !found {
			// The primary key is unexported or otherwise absent from the
			// fixture's view, so the graph cannot assign or compare it.
			return nil, nil, []diag.Diag{diag.Warningf(g.Pos,
				"skipping %s: the %s primary key %s is not settable from the generated package",
				plan.Name, node.Table, node.PKField)}
		}

		earlier := seenByTable[node.Table]
		if conv, ok := inf.counterConv(pk.Type); ok {
			np.CounterConv = conv
		} else if len(earlier) > 0 {
			regen := inf.fieldExpr(pk, node.Table)
			if regen.expr == "" {
				return nil, nil, []diag.Diag{diag.Warningf(g.Pos,
					"skipping %s: %s appears more than once and no expression can keep %s distinct",
					plan.Name, node.Table, node.PKField)}
			}
			var against []string
			for _, e := range earlier {
				against = append(against, g.Nodes[e].Field)
			}
			plan.Loops = append(plan.Loops, GraphLoopPlan{
				Field:       node.Field,
				PKField:     node.PKField,
				Against:     against,
				Regen:       regen.expr,
				NeedsHelper: regen.needsHelper,
			})
			pkgs = append(pkgs, regen.pkgs...)
		}

		for _, e := range earlier {
			np.DedupAgainst = append(np.DedupAgainst, g.Nodes[e].Field)
		}
		seenByTable[node.Table] = append(earlier, i)
		plan.Nodes = append(plan.Nodes, np)
	}

	if slices.ContainsFunc(plan.Nodes, func(n GraphNodePlan) bool { return n.CounterConv != "" }) {
		pkgs = append(pkgs, atomicImport)
	}

	for _, w := range g.Wires {
		plan.Wires = append(plan.Wires, GraphWirePlan{
			FromField: g.Nodes[w.From].Field,
			FKField:   w.FKField,
			ToField:   g.Nodes[w.To].Field,
			ToPKField: g.Nodes[w.To].PKField,
		})
	}

	return &plan, pkgs, nil
}

// counterConv reports the conversion wrapping the shared int64 counter for an
// integer key: the basic type's own name, or the qualified named type. A named
// integer from another package is not renderable here and falls back to the
// regeneration loop.
func (inf inferrer) counterConv(typ types.Type) (string, bool) {
	b, qualifier, ok := inf.basicOf(types.Unalias(typ))
	if !ok || b.Info()&types.IsInteger == 0 {
		return "", false
	}
	if qualifier != "" {
		return qualifier, true
	}
	return b.Name(), true
}

// fieldNamed finds a target's field by name.
func fieldNamed(tg Target, name string) (ir.Field, bool) {
	for _, f := range tg.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return ir.Field{}, false
}
