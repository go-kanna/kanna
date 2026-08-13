package fixture

import (
	"cmp"
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
	// KeyExpr replaces the fixture's key after construction: the shared
	// counter for integer keys, a UUID for string keys, so records stay
	// unique across graphs. Empty for key types with neither form, which are
	// unique only within their graph.
	KeyExpr     string
	UsesCounter bool
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
// plans. It never fails the run: kanna-orm owns tag enforcement, so here a
// malformed table, a graph whose record set cannot be built, or a name
// already taken by a fixture only costs the graphs involved, each skip
// explained by a warning.
func GraphPlans(structs []ir.Struct, targets []Target, pkgPath, pkgName string) ([]GraphPlan, []string, []diag.Diag) {
	graphs, diags := relation.BuildGraphs(structs)
	for i, d := range diags {
		if d.Severity == diag.SeverityError {
			diags[i].Severity = diag.SeverityWarning
		}
	}
	if len(graphs) == 0 {
		return nil, nil, diags
	}
	slices.SortStableFunc(graphs, func(a, b relation.Graph) int {
		return cmp.Compare(a.Root, b.Root)
	})

	byName := make(map[string]Target, len(targets))
	for _, tg := range targets {
		byName[tg.Name] = tg
	}

	inf := inferrer{pkgPath: pkgPath, pkgName: pkgName, targets: make(map[string]bool, len(targets))}
	for _, tg := range targets {
		inf.targets[tg.Name] = true
	}
	inf.graph = inf.referenceGraph(targets)

	// The generated file already declares one function per target, so a graph
	// whose type or constructor would take one of those names cannot land.
	taken := make(map[string]bool, len(targets))
	for _, tg := range targets {
		taken[tg.Name] = true
	}

	var plans []GraphPlan
	imports := make(map[string]bool)

	for _, g := range graphs {
		plan, pkgs, ds := inf.graphPlan(g, byName)
		diags = append(diags, ds...)
		if plan == nil {
			continue
		}
		if taken[plan.Name] || taken[plan.Ctor] {
			name := plan.Name
			if taken[plan.Ctor] {
				name = plan.Ctor
			}
			diags = append(diags, diag.Warningf(g.Pos,
				"skipping %s: %s is already declared in the generated file", plan.Name, name))
			continue
		}
		taken[plan.Name] = true
		taken[plan.Ctor] = true

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
		if expr, usesCounter, exprPkgs, ok := inf.keyExpr(pk.Type); ok {
			np.KeyExpr = expr
			np.UsesCounter = usesCounter
			pkgs = append(pkgs, exprPkgs...)
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

	if slices.ContainsFunc(plan.Nodes, func(n GraphNodePlan) bool { return n.UsesCounter }) {
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

// keyExpr builds the expression that replaces a graph record's key so it
// stays unique across graphs: the shared counter for integer keys wide enough
// not to wrap it, a UUID string for string keys. Anything else — narrow
// integers, named types from other packages, exotic comparables — reports
// false and stays unique only within its graph, through the regeneration
// loop when the table repeats.
func (inf inferrer) keyExpr(typ types.Type) (expr string, usesCounter bool, pkgs []string, ok bool) {
	b, qualifier, resolved := inf.basicOf(types.Unalias(typ))
	if !resolved {
		return "", false, nil, false
	}

	switch b.Kind() {
	case types.Int, types.Int32, types.Int64, types.Uint, types.Uint32, types.Uint64:
		conv := qualifier
		if conv == "" {
			conv = b.Name()
		}
		return conv + "(nextPK.Add(1))", true, nil, true
	case types.String:
		return qualify("gofakeit.UUID()", qualifier), false, []string{gofakeitImport}, true
	case types.Invalid, types.Bool, types.Int8, types.Int16, types.Uint8, types.Uint16, types.Uintptr,
		types.Float32, types.Float64, types.Complex64, types.Complex128, types.UnsafePointer,
		types.UntypedBool, types.UntypedInt, types.UntypedRune, types.UntypedFloat, types.UntypedComplex,
		types.UntypedString, types.UntypedNil:
		return "", false, nil, false
	default:
		return "", false, nil, false
	}
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
