// Package run runs a plan, one task after the other, and says what happened to
// the working tree.
//
// What a run changed is read from the tree, around each task, rather than from
// what the generator says: it is then right for a generator that fails halfway
// too. Nothing is undone when a task fails, as what completed is sound.
package run

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/go-kanna/internal/deps"

	"github.com/go-kanna/kanna/internal/module"
	"github.com/go-kanna/kanna/internal/plan"
)

// The exit codes of a run.
const (
	ExitOK      = 0
	ExitFailure = 1 // a task failed, differs, or was not run
)

// Config says how to run a plan.
type Config struct {
	// GOROOT is what the directives run with.
	GOROOT string

	// Check compares what is on disk with what would be generated, writing
	// nothing.
	Check bool

	// Out receives the report.
	Out io.Writer
}

// state is how a task ended up.
type state int

const (
	done state = iota
	failed
	skipped
)

// result is what became of a task.
type result struct {
	task    *plan.Task
	state   state
	changes []change
	output  string
}

// change is a file a task changed.
type change struct {
	path   string
	action string
}

// Run runs p, which has to be free of Problems, and returns the code to exit
// with.
func Run(ctx context.Context, p *plan.Plan, cfg Config) int {
	if cfg.Check {
		return check(ctx, p, cfg)
	}

	results := make([]result, 0, len(p.Tasks))
	stopped := false
	for _, t := range p.Tasks {
		if stopped {
			results = append(results, result{task: t, state: skipped})
			continue
		}
		before := snapshot(p.Module)
		output, err := runTask(ctx, t, cfg)
		r := result{task: t, output: output, changes: diff(p.Module, before, snapshot(p.Module))}
		if err != nil {
			r.state, r.output = failed, withCause(output, err)
			stopped = true
		}
		results = append(results, r)
	}

	if stopped {
		reportFailure(cfg.Out, p.Module, results)
		return ExitFailure
	}
	reportSuccess(cfg.Out, p.Module, results)
	return ExitOK
}

// check checks the tasks of p in order. What comes after a task that differs
// would read what that task would write, which is not on disk, so it goes
// unchecked rather than being compared with what no run would give.
func check(ctx context.Context, p *plan.Plan, cfg Config) int {
	bad := make(map[*plan.Task]bool, len(p.Tasks))
	results := make([]result, 0, len(p.Tasks))
	for _, t := range p.Tasks {
		if slices.ContainsFunc(t.After, func(e plan.Edge) bool { return bad[e.From] }) {
			bad[t] = true
			results = append(results, result{task: t, state: skipped})
			continue
		}
		output, err := runTask(ctx, t, cfg)
		r := result{task: t, output: output}
		if err != nil {
			bad[t] = true
			r.state, r.output = failed, withCause(output, err)
		}
		results = append(results, r)
	}

	if len(bad) > 0 {
		reportCheckFailure(cfg.Out, p.Module, results)
		return ExitFailure
	}
	fmt.Fprintf(cfg.Out, "%s up to date\n", count(len(p.Tasks), "task"))
	return ExitOK
}

// withCause returns what a failed task printed, or why it could not run.
func withCause(output string, err error) string {
	if strings.TrimSpace(output) != "" {
		return output
	}
	return err.Error() + "\n"
}

// runTask runs t and returns what it printed.
func runTask(ctx context.Context, t *plan.Task, cfg Config) (string, error) {
	d := t.At[0]
	cmd := d.Command(ctx, cfg.GOROOT, t.Words())

	pending, err := pendingFile(t)
	if err != nil {
		return "", err
	}
	if pending != "" {
		defer os.Remove(pending) //nolint:errcheck // a file in the temporary directory
	}
	checkValue := ""
	if cfg.Check {
		checkValue = "1"
	}
	cmd.Env = append(cmd.Env, deps.PendingEnv+"="+pending, "KANNA_CHECK="+checkValue)

	output, err := cmd.CombinedOutput()
	return string(output), err
}

// pendingFile writes what t is to take as coming into a file for it, and
// returns its path, or "" when there is nothing to take so.
func pendingFile(t *plan.Task) (string, error) {
	if len(t.Pending) == 0 {
		return "", nil
	}
	f, err := os.CreateTemp("", "kanna-pending-")
	if err != nil {
		return "", fmt.Errorf("write what is pending: %w", err)
	}
	_, err = f.WriteString(strings.Join(t.Pending, "\n") + "\n")
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fmt.Errorf("write what is pending: %w", err)
	}
	return f.Name(), nil
}

// snapshot returns the Go files of m, as they stand. A generator replaces a file
// by renaming a new one over it, so what is read here tells any change apart.
func snapshot(m *module.Module) map[string]fs.FileInfo {
	files := make(map[string]fs.FileInfo)
	// A file that goes while this walks is a file not listed.
	_ = module.Walk(m.Dir, func(file string) error {
		if info, err := os.Lstat(file); err == nil {
			files[file] = info
		}
		return nil
	})
	return files
}

// diff returns how after differs from before, sorted by path.
func diff(m *module.Module, before, after map[string]fs.FileInfo) []change {
	var changes []change
	for _, path := range slices.Sorted(maps.Keys(after)) {
		was, ok := before[path]
		is := after[path]
		if !ok || !os.SameFile(was, is) || was.Size() != is.Size() || !was.ModTime().Equal(is.ModTime()) {
			changes = append(changes, change{path: m.Rel(path), action: "wrote"})
		}
	}
	for _, path := range slices.Sorted(maps.Keys(before)) {
		if _, ok := after[path]; !ok {
			changes = append(changes, change{path: m.Rel(path), action: "removed"})
		}
	}
	slices.SortFunc(changes, func(a, b change) int { return strings.Compare(a.path, b.path) })
	return changes
}

// reportSuccess reports a run that went through, in one line when there is
// nothing to tell.
func reportSuccess(w io.Writer, m *module.Module, results []result) {
	changed := changedFiles(results)
	quiet := changed == 0 && !slices.ContainsFunc(results, func(r result) bool {
		return strings.TrimSpace(r.output) != ""
	})
	if !quiet {
		rows := newTable(m, results)
		for _, r := range results {
			rows.write(w, "", r)
		}
	}
	fmt.Fprintf(w, "%s, %s changed\n", count(len(results), "task"), count(changed, "file"))
}

// reportFailure reports a run that a task stopped.
func reportFailure(w io.Writer, m *module.Module, results []result) {
	rows := newTable(m, results)
	counts := make(map[state]int)
	for _, r := range results {
		rows.write(w, []string{done: "completed", failed: "failed", skipped: "not run"}[r.state], r)
		counts[r.state]++
	}
	fmt.Fprintf(w, "\n%d completed, %d failed, %d not run\n", counts[done], counts[failed], counts[skipped])

	note := ""
	if changed := changedFiles(results); changed > 0 {
		note = "; completed changes remain in the working tree"
		if counts[done] == 0 {
			note = "; what it wrote remains in the working tree"
		}
	}
	fmt.Fprintf(w, "%s changed%s\n", count(changedFiles(results), "file"), note)
}

// reportCheckFailure reports a check that did not come out clean.
func reportCheckFailure(w io.Writer, m *module.Module, results []result) {
	rows := newTable(m, results)
	counts := make(map[state]int)
	for _, r := range results {
		rows.write(w, []string{done: "up to date", failed: "failed", skipped: "not checked"}[r.state], r)
		counts[r.state]++
	}
	fmt.Fprintf(w, "\n%d up to date, %d failed, %d not checked\n", counts[done], counts[failed], counts[skipped])
}

func changedFiles(results []result) int {
	files := make(map[string]bool)
	for _, r := range results {
		for _, c := range r.changes {
			files[c.path] = true
		}
	}
	return len(files)
}

// count says n of what noun names, as "1 file" or "3 files".
func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// table lays results out in columns.
type table struct {
	m                  *module.Module
	state, place, tool int
}

func newTable(m *module.Module, results []result) table {
	tb := table{m: m, state: len("not checked")}
	for _, r := range results {
		tb.place = max(tb.place, len(tb.placeOf(r.task)))
		tb.tool = max(tb.tool, len(r.task.Tool))
	}
	return tb
}

func (tb table) placeOf(t *plan.Task) string {
	return fmt.Sprintf("%s:%d", tb.m.Rel(t.At[0].File), t.At[0].Line)
}

// write writes the lines of r, starting with its label. A run that went through
// has no labels, and is indented in place of them.
func (tb table) write(w io.Writer, label string, r result) {
	start := "  "
	if label != "" {
		start = fmt.Sprintf("%-*s  ", tb.state, label)
	}
	head := fmt.Sprintf("%s%-*s  %-*s", start, tb.place, tb.placeOf(r.task), tb.tool, r.task.Tool)

	outcomes := make([]string, 0, len(r.changes))
	for _, c := range r.changes {
		outcomes = append(outcomes, c.action+" "+c.path)
	}
	if len(outcomes) == 0 && r.state == done && label == "" {
		outcomes = []string{"unchanged"}
	}
	if len(outcomes) == 0 {
		fmt.Fprintln(w, strings.TrimRight(head, " "))
	} else {
		fmt.Fprintf(w, "%s  %s\n", head, outcomes[0])
		for _, o := range outcomes[1:] {
			fmt.Fprintf(w, "%*s%s\n", len(head)+2, "", o)
		}
	}
	for line := range strings.Lines(strings.TrimSpace(r.output)) {
		fmt.Fprintf(w, "  %s", line)
		if !strings.HasSuffix(line, "\n") {
			fmt.Fprintln(w)
		}
	}
}
