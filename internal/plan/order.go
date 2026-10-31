package plan

import (
	"fmt"
	"go/token"
	"slices"
	"strings"

	"github.com/go-kanna/internal/diag"

	"github.com/go-kanna/kanna/internal/module"
)

// order puts tasks in the order they are to run, and fills in After, Ahead,
// Pending, and Loose. Among tasks free to go first, the first of tasks does, so
// the order is the module's unless something says otherwise.
//
// Tasks waiting for each other only for edges that can be mended are not stuck:
// those edges are given up, a task running ahead of one it would better come
// after learns of it through Pending. A cycle that remains is returned, and the
// tasks it leaves out of the order are at the end.
func order(tasks []*Task, edges []Edge) ([]*Task, []Edge) {
	kept := make([]Edge, 0, len(edges))
	var givenUp []Edge
	reach := reachability(tasks, edges)
	for _, e := range edges {
		if e.mendable() && reach[e.From][e.To] && reach[e.To][e.From] {
			givenUp = append(givenUp, e)
		} else {
			kept = append(kept, e)
		}
	}

	ordered, stuck := sorted(tasks, kept)
	at := make(map[*Task]int, len(tasks))
	for i, t := range ordered {
		at[t] = i
	}
	for _, e := range givenUp {
		if at[e.From] < at[e.To] {
			kept = append(kept, e)
			continue
		}
		e.To.Ahead = append(e.To.Ahead, e)
		e.To.Pending = append(e.To.Pending, e.From.Report.Declares...)
	}
	for _, t := range tasks {
		slices.Sort(t.Pending)
		t.Pending = slices.Compact(t.Pending)
	}

	for _, e := range kept {
		e.To.After = append(e.To.After, e)
	}
	if len(stuck) > 0 {
		return append(ordered, stuck...), cycleAmong(stuck, kept)
	}
	return ordered, nil
}

// sorted returns tasks with each after what edges put before it, the first of
// tasks first among those free to go, and the tasks left waiting for each other.
func sorted(tasks []*Task, edges []Edge) (ordered, stuck []*Task) {
	waits := make(map[*Task]map[*Task]bool, len(tasks))
	for _, e := range edges {
		if waits[e.To] == nil {
			waits[e.To] = make(map[*Task]bool)
		}
		waits[e.To][e.From] = true
	}

	left := slices.Clone(tasks)
	for len(left) > 0 {
		i := slices.IndexFunc(left, func(t *Task) bool { return len(waits[t]) == 0 })
		if i < 0 {
			return ordered, left
		}
		t := left[i]
		left = slices.Delete(left, i, i+1)
		ordered = append(ordered, t)
		for _, w := range waits {
			delete(w, t)
		}
	}
	return ordered, nil
}

// reachability returns, for each task, the tasks that come after it through
// edges.
func reachability(tasks []*Task, edges []Edge) map[*Task]map[*Task]bool {
	next := make(map[*Task][]*Task, len(tasks))
	for _, e := range edges {
		next[e.From] = append(next[e.From], e.To)
	}

	reach := make(map[*Task]map[*Task]bool, len(tasks))
	for _, t := range tasks {
		seen := make(map[*Task]bool)
		var visit func(t *Task)
		visit = func(t *Task) {
			for _, n := range next[t] {
				if !seen[n] {
					seen[n] = true
					visit(n)
				}
			}
		}
		visit(t)
		reach[t] = seen
	}
	return reach
}

// cycleAmong returns a cycle of edges among tasks, each edge saying what its To
// waits for, starting from the first task in one.
func cycleAmong(tasks []*Task, edges []Edge) []Edge {
	among := slices.DeleteFunc(slices.Clone(edges), func(e Edge) bool {
		return !slices.Contains(tasks, e.From) || !slices.Contains(tasks, e.To)
	})
	reach := reachability(tasks, among)

	for _, start := range tasks {
		if !reach[start][start] {
			continue
		}
		// Walk from start along what it waits for, staying on tasks that get
		// back to it.
		var cycle []Edge
		seen := map[*Task]bool{start: true}
		var walk func(t *Task) bool
		walk = func(t *Task) bool {
			for _, e := range among {
				if e.To != t {
					continue
				}
				if e.From == start {
					cycle = append(cycle, e)
					return true
				}
				if !seen[e.From] && reach[start][e.From] {
					seen[e.From] = true
					cycle = append(cycle, e)
					if walk(e.From) {
						return true
					}
					cycle = cycle[:len(cycle)-1]
				}
			}
			return false
		}
		if walk(start) {
			return cycle
		}
	}
	return nil
}

// cycleDiag says what the tasks of cycle wait for.
func cycleDiag(m *module.Module, cycle []Edge) diag.Diag {
	hints := make([]string, len(cycle))
	for i, e := range cycle {
		hints[i] = fmt.Sprintf("%s waits for %s: %s", locationOf(m, e.To), locationOf(m, e.From), e.Reason(m))
	}
	names := make([]string, len(cycle))
	for i, e := range cycle {
		names[i] = e.To.Tool
	}
	return diag.Errorf(token.Position{}, "%s wait for each other", strings.Join(names, ", ")).WithHints(hints...)
}
