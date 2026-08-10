// Package imports assigns the local names a generated file uses for the
// packages it imports.
//
// Every generator that writes qualified type names faces the same problem: two
// imported packages can share a name, and either can collide with an identifier
// the generated body declares. Getting that wrong produces a file that does not
// compile, or worse, one that compiles against the wrong package.
package imports

import (
	"fmt"
	"go/types"
	"io"
	"path"
	"slices"
	"strconv"
	"strings"
)

// Entry is one line of a generated import block.
type Entry struct {
	Path  string
	Alias string
}

// Tracker assigns each imported package a local name that collides with nothing
// else in the generated file.
type Tracker struct {
	selfPkg string
	rename  func(string) string
	aliases map[string]string
	used    map[string]bool
}

// New returns a Tracker for a file generated into selfPkg. That package is never
// imported and never qualified, because the file is already in it.
//
// rename gets the chance to rewrite a package's name before numbering starts,
// and may be nil. It exists because generators keep their identifiers apart in
// two different ways, and both have to survive: a generator that knows the exact
// names its body declares calls Reserve and lets the collision resolve to a
// number, while one whose body names follow a pattern (v0, err1, ...) cannot
// enumerate them and rewrites the base instead.
func New(selfPkg string, rename func(base string) string) *Tracker {
	return &Tracker{
		selfPkg: selfPkg,
		rename:  rename,
		aliases: map[string]string{},
		used:    map[string]bool{},
	}
}

// Reserve marks names as taken, so no import can be given one. An import that
// would have taken a reserved name is numbered instead.
func (t *Tracker) Reserve(names ...string) {
	for _, n := range names {
		t.used[n] = true
	}
}

// Add records pkgPath under a unique local name and returns that name. Recording
// the same path again returns the name already chosen, so callers may add
// freely. The generated file's own package records nothing and returns "".
func (t *Tracker) Add(pkgPath, pkgName string) string {
	if pkgPath == "" || pkgPath == t.selfPkg {
		return ""
	}
	if alias, ok := t.aliases[pkgPath]; ok {
		return alias
	}

	alias := t.unique(baseName(pkgPath, pkgName))
	t.aliases[pkgPath] = alias
	t.used[alias] = true

	return alias
}

// Lookup returns the name recorded for pkgPath. Unlike Add it records nothing,
// which is what a caller wants when it has already collected the imports it
// intends to write and must not acquire more while rendering.
func (t *Tracker) Lookup(pkgPath string) (string, bool) {
	alias, ok := t.aliases[pkgPath]
	return alias, ok
}

// Qualifier is a types.Qualifier. It records each package it is asked about, so
// rendering a type through types.TypeString collects the imports that type
// needs. The generated file's own package qualifies to "", which is how
// TypeString omits it.
func (t *Tracker) Qualifier(p *types.Package) string {
	if p == nil {
		return ""
	}
	return t.Add(p.Path(), p.Name())
}

// Taken returns every local name that is spoken for, sorted: the aliases
// assigned so far plus whatever Reserve claimed.
//
// A generator that names identifiers in the body it emits uses this to stay
// clear of them. Entries is not enough for that, because a reserved name has no
// import behind it.
func (t *Tracker) Taken() []string {
	names := make([]string, 0, len(t.used))
	for n := range t.used {
		names = append(names, n)
	}
	slices.Sort(names)

	return names
}

// Entries returns one entry per recorded import, sorted by path.
func (t *Tracker) Entries() []Entry {
	out := make([]Entry, 0, len(t.aliases))
	for p, a := range t.aliases {
		out = append(out, Entry{Path: p, Alias: a})
	}
	slices.SortFunc(out, func(a, b Entry) int {
		return strings.Compare(a.Path, b.Path)
	})

	return out
}

// Render writes an import block for entries.
//
// group assigns each entry a group number; groups are written in ascending
// order separated by a blank line, which is the one thing gofmt will not
// rearrange and therefore the only way a generator controls import grouping. A
// nil group puts everything together.
//
// An alias that repeats what the path already says is left out, so the block
// reads the way a person would have written it.
func Render(w io.Writer, entries []Entry, group func(Entry) int) {
	if len(entries) == 0 {
		return
	}

	buckets := map[int][]Entry{}
	for _, e := range entries {
		g := 0
		if group != nil {
			g = group(e)
		}
		buckets[g] = append(buckets[g], e)
	}

	groups := make([]int, 0, len(buckets))
	for g := range buckets {
		groups = append(groups, g)
	}
	slices.Sort(groups)

	fmt.Fprint(w, "import (\n")
	for i, g := range groups {
		if i > 0 {
			fmt.Fprint(w, "\n")
		}
		for _, e := range buckets[g] {
			writeSpec(w, e)
		}
	}
	fmt.Fprint(w, ")\n")
}

func writeSpec(w io.Writer, e Entry) {
	if e.Alias == "" || e.Alias == path.Base(e.Path) {
		fmt.Fprintf(w, "\t%q\n", e.Path)
		return
	}
	fmt.Fprintf(w, "\t%s %q\n", e.Alias, e.Path)
}

// baseName picks the name to start from: the package's own, falling back to the
// last path element for a package whose name is not known.
func baseName(pkgPath, pkgName string) string {
	base := pkgName
	if base == "" {
		base = path.Base(pkgPath)
	}
	if base == "" || base == "." || base == "/" {
		base = "pkg"
	}
	return base
}

// unique returns base, or the first free variation of it.
func (t *Tracker) unique(base string) string {
	if t.rename != nil {
		base = t.rename(base)
	}

	candidate := base
	for n := 2; t.used[candidate]; n++ {
		candidate = base + strconv.Itoa(n)
	}

	return candidate
}
