package plan

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-kanna/internal/diag"
	"github.com/go-kanna/internal/packages"

	"github.com/go-kanna/kanna/internal/generate"
	"github.com/go-kanna/kanna/internal/module"
)

// Kind is why a task comes after another.
type Kind int

const (
	// Imports says it reads, through what it imports, a package the other
	// writes into.
	Imports Kind = iota

	// Names says it reads a package the other writes into because something
	// names it, as a with= of kanna-di does, which no import shows.
	Names

	// Reads says it reads what the other generates next to its own output.
	Reads

	// Tests says the tests it reads import a package the other writes into.
	Tests

	// Calls says code it checks calls what the other generates next to its
	// own output.
	Calls
)

// Edge says that To comes after From.
type Edge struct {
	From, To *Task
	Kind     Kind

	// Package is the import path of the package it is about.
	Package string

	// Via is what names Package, for Names.
	Via string
}

// Reason says why the edge is there, for m.
func (e Edge) Reason(m *module.Module) string {
	pkg := strings.TrimPrefix(e.Package, m.Path+"/")
	switch e.Kind {
	case Names:
		return fmt.Sprintf("%s names %s, which %s writes into", e.Via, pkg, e.From.Tool)
	case Reads:
		return fmt.Sprintf("reads what %s generates in %s", e.From.Tool, pkg)
	case Tests:
		return fmt.Sprintf("its tests read %s, which %s writes into", pkg, e.From.Tool)
	case Calls:
		return fmt.Sprintf("code in %s calls what %s generates", pkg, e.From.Tool)
	case Imports:
	}
	return fmt.Sprintf("reads %s, which %s writes into", pkg, e.From.Tool)
}

// mendable reports whether the two tasks can run the other way round. What
// they then lack is code to compile, which they can be told is coming, and not
// what to generate from, which they cannot do without.
func (e Edge) mendable() bool {
	return e.Kind == Tests || e.Kind == Calls
}

// graph is what the tasks of a module read and write of its packages.
type graph struct {
	m     *module.Module
	tasks []*Task

	// writers holds what writes into a package, by import path.
	writers map[string][]*Task

	// names holds the package name of a package of the module, or of one tasks
	// are to create, by import path.
	names map[string]string

	// reads holds what a task reads, by import path.
	reads map[*Task]map[string]read

	// contested holds what both reads and writes a package, by its import
	// path, for the packages where more than one does.
	contested map[string][]*Task

	edges []Edge
}

// read is how a task comes to read a package.
type read struct {
	kind Kind
	via  string
}

func newGraph(m *module.Module, tasks []*Task) (*graph, error) {
	g := &graph{
		m:       m,
		tasks:   tasks,
		writers: make(map[string][]*Task),
		names:   make(map[string]string),
		reads:   make(map[*Task]map[string]read, len(tasks)),
	}
	for path, p := range m.Packages {
		g.names[path] = p.Name
	}
	for _, t := range tasks {
		for _, w := range writtenPackages(m, t) {
			g.writers[w] = append(g.writers[w], t)
			if dir, ok := m.DirOf(w); ok && g.names[w] == "" {
				g.names[w] = filepath.Base(dir)
			}
		}
	}
	for _, t := range tasks {
		g.reads[t] = g.readsOf(t)
	}

	g.contested = g.findContested()
	for _, t := range tasks {
		g.addReads(t)
	}
	if err := g.addCalls(); err != nil {
		return nil, err
	}
	return g, nil
}

// writtenPackages returns the packages of m that t writes into, by import path.
func writtenPackages(m *module.Module, t *Task) []string {
	var pkgs []string
	for _, w := range t.Report.Writes {
		if pkg, ok := m.PackageOf(w); ok {
			pkgs = append(pkgs, pkg)
		}
	}
	slices.Sort(pkgs)
	return slices.Compact(pkgs)
}

// readsOf follows what t loads through what the module's hand-written code
// imports. A package's own tests count for the packages t loads, and for no
// other, which go/packages does not load the tests of either.
func (g *graph) readsOf(t *Task) map[string]read {
	found := make(map[string]read)
	var visit func(path string, how read)
	visit = func(path string, how read) {
		if _, inModule := g.m.DirOf(path); !inModule {
			return
		}
		if prev, seen := found[path]; seen && prev.kind <= how.kind {
			return
		}
		found[path] = how
		if p := g.m.Packages[path]; p != nil {
			for _, imp := range p.Imports {
				visit(imp, how)
			}
		}
	}

	for _, path := range t.Report.Packages {
		visit(path, read{kind: Imports})
	}
	imported := maps.Clone(found)
	for _, ref := range t.Report.Refs {
		paths := g.named(ref.Package)
		if slices.ContainsFunc(paths, func(path string) bool { _, ok := imported[path]; return ok }) {
			continue // the one it means, however many others share its name
		}
		for _, path := range paths {
			visit(path, read{kind: Names, via: ref.Via})
		}
	}
	if t.Report.Tests {
		for _, path := range t.Report.Packages {
			if p := g.m.Packages[path]; p != nil {
				for _, imp := range p.TestImports {
					visit(imp, read{kind: Tests})
				}
			}
		}
	}
	return found
}

// named returns the packages a Ref's name may be: by import path, by the end of
// one, or by its name.
func (g *graph) named(name string) []string {
	var paths []string
	for _, path := range slices.Sorted(maps.Keys(g.names)) {
		if path == name || strings.HasSuffix(path, "/"+name) || g.names[path] == name {
			paths = append(paths, path)
		}
	}
	return paths
}

// findContested returns the tasks that both read and write a package, by its
// import path, for the packages holding more than one. They cannot all come
// after the others, so what orders them has to be more than that.
func (g *graph) findContested() map[string][]*Task {
	contested := make(map[string][]*Task)
	for path, writers := range g.writers {
		var both []*Task
		for _, t := range writers {
			if _, ok := g.reads[t][path]; ok {
				both = append(both, t)
			}
		}
		if len(both) > 1 {
			contested[path] = both
		}
	}
	return contested
}

// addReads adds that t comes after what writes into what it reads.
func (g *graph) addReads(t *Task) {
	reads := g.reads[t]
	for _, path := range slices.Sorted(maps.Keys(reads)) {
		how := reads[path]
		for _, from := range g.writers[path] {
			switch {
			case from == t:
			case !slices.Contains(g.contested[path], t) || !slices.Contains(g.contested[path], from):
				g.edges = append(g.edges, Edge{From: from, To: t, Kind: how.kind, Package: path, Via: how.via})
			case from.Report.SourcesOnly && !t.Report.SourcesOnly:
				g.edges = append(g.edges, Edge{From: from, To: t, Kind: Reads, Package: path})
			}
		}
	}
}

// addCalls adds what the code of contested packages calls: a task checks the
// code of a package it writes into, so it comes after what writes what that
// code calls. Only the code can tell, and it is read as it is written by hand,
// so that what generators wrote has no say.
func (g *graph) addCalls() error {
	if len(g.contested) == 0 {
		return nil
	}

	var patterns []string
	for _, path := range slices.Sorted(maps.Keys(g.contested)) {
		if p := g.m.Packages[path]; p != nil {
			patterns = append(patterns, p.Dir)
		}
	}
	if len(patterns) == 0 {
		return nil
	}
	set, err := packages.Load(patterns, packages.Config{Dir: g.m.Dir, Tests: true, Overlay: g.m.Handwritten()})
	if err != nil {
		return fmt.Errorf("read the code of the packages: %w", err)
	}

	for _, path := range slices.Sorted(maps.Keys(g.contested)) {
		tasks := g.contested[path]
		dir, _ := g.m.DirOf(path)
		for _, from := range tasks {
			inCode, inTests := calledIn(set, dir, from)
			for _, to := range tasks {
				if to != from && (inCode || inTests && to.Report.Tests) {
					g.edges = append(g.edges, Edge{From: from, To: to, Kind: Calls, Package: path})
				}
			}
		}
	}
	return nil
}

// calledIn reports whether code in dir calls what t declares, and whether its
// tests do.
func calledIn(set packages.Set, dir string, t *Task) (inCode, inTests bool) {
	names := make(map[string]bool, len(t.Report.Declares))
	for _, name := range t.Report.Declares {
		names[name] = true
	}
	pending := set.Pending(names)
	for _, d := range set.Diags {
		if filepath.Dir(d.Pos.Filename) != dir || !pending(d) {
			continue
		}
		if strings.HasSuffix(d.Pos.Filename, "_test.go") {
			inTests = true
		} else {
			inCode = true
		}
	}
	return inCode, inTests
}

// selected returns the tasks selection asks for, by where their line is or
// where they write, and every task those need, in the order of g.tasks. What
// needs them is not among them.
func (g *graph) selected(selection module.Selection) []*Task {
	chosen := make(map[*Task]bool)
	var choose func(t *Task)
	choose = func(t *Task) {
		if chosen[t] {
			return
		}
		chosen[t] = true
		for _, e := range g.edges {
			if e.To == t {
				choose(e.From)
			}
		}
	}

	for _, t := range g.tasks {
		if g.matches(t, selection) {
			choose(t)
		}
	}
	return slices.DeleteFunc(slices.Clone(g.tasks), func(t *Task) bool { return !chosen[t] })
}

func (g *graph) matches(t *Task, selection module.Selection) bool {
	if slices.ContainsFunc(t.At, func(d generate.Directive) bool { return selection.Matches(dirOf(d)) }) {
		return true
	}
	return slices.ContainsFunc(t.Report.Writes, func(w string) bool { return selection.Matches(filepath.Dir(w)) })
}

// edgesAmong returns the edges between tasks.
func (g *graph) edgesAmong(tasks []*Task) []Edge {
	return slices.DeleteFunc(slices.Clone(g.edges), func(e Edge) bool {
		return !slices.Contains(tasks, e.From) || !slices.Contains(tasks, e.To)
	})
}

// markLoose fills in Loose: of the tasks that both read and write a package, the
// pairs nothing puts in an order.
func (g *graph) markLoose(tasks []*Task) {
	reach := reachability(tasks, g.edgesAmong(tasks))
	for _, path := range slices.Sorted(maps.Keys(g.contested)) {
		for _, t := range g.contested[path] {
			for _, other := range g.contested[path] {
				if t != other && slices.Contains(tasks, t) && slices.Contains(tasks, other) &&
					!reach[t][other] && !reach[other][t] && !slices.Contains(t.Loose, other) {
					t.Loose = append(t.Loose, other)
				}
			}
		}
	}
}

// missing reports what tasks read that is not there and that none of the module's
// tasks writes, each thing once.
func (g *graph) missing(tasks []*Task) []diag.Diag {
	var ds []diag.Diag
	reported := make(map[string]bool)
	for _, t := range tasks {
		ds = append(ds, missingInputs(g.m, t)...)
		reads := g.reads[t]
		for _, path := range slices.Sorted(maps.Keys(reads)) {
			if g.m.Packages[path] != nil || len(g.writers[path]) > 0 || reported[path] || g.hasGoFiles(path) {
				continue
			}
			reported[path] = true
			ds = append(ds, diag.Errorf(positionOf(t.At[0]), "%s reads %s, which is not there and which no task writes",
				t.Tool, strings.TrimPrefix(path, g.m.Path+"/")).
				WithHints("generate it first: kanna gen runs the kanna generators alone"))
		}
	}
	return ds
}

// hasGoFiles reports whether the directory of the package at path holds Go
// files, as that of a module nested in this one does, which is not read.
func (g *graph) hasGoFiles(path string) bool {
	dir, _ := g.m.DirOf(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(entries, func(e os.DirEntry) bool { return strings.HasSuffix(e.Name(), ".go") })
}

// external returns the packages tasks read that another generator wrote.
func (g *graph) external(tasks []*Task) []External {
	found := make(map[string][]string)
	for _, t := range tasks {
		for path := range g.reads[t] {
			if p := g.m.Packages[path]; p != nil && len(p.Foreign) > 0 {
				found[path] = p.Foreign
			}
		}
	}
	ext := make([]External, 0, len(found))
	for _, path := range slices.Sorted(maps.Keys(found)) {
		ext = append(ext, External{Package: path, Generators: found[path]})
	}
	return ext
}
