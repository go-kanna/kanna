package plan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/build"
	"go/token"
	"os/exec"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/go-kanna/internal/deps"
	"github.com/go-kanna/internal/diag"

	"github.com/go-kanna/kanna/internal/generate"
	"github.com/go-kanna/kanna/internal/module"
)

// Task is one run of a generator: a //go:generate line, or several in one
// directory saying the same, which come to the same run.
type Task struct {
	// At holds the lines of the task, in the order they are in the module.
	At []generate.Directive

	// Tool is the generator, such as kanna-value.
	Tool string

	// Args holds what follows Tool on the line.
	Args []string

	// Report is what the generator answers of the run.
	Report deps.Report

	// After holds what the task comes after, each for one reason: what it
	// needs to be done, and what is shown of why.
	After []Edge

	// Ahead holds what the task comes before though it would better come
	// after: code that it checks calls what those write, and they call what it
	// writes. Pending names what they declare.
	Ahead []Edge

	// Pending names what the tasks of Ahead write, which a run of the task is
	// told to take as coming.
	Pending []string

	// Loose holds the tasks that write into a package of this one with
	// nothing to tell which of the two comes first.
	Loose []*Task
}

// Words returns the command line of the task, with extra after the tool's name.
func (t *Task) Words(extra ...string) []string {
	return slices.Concat([]string{"go", "tool", t.Tool}, extra, t.Args)
}

// Asker tells what a task reads and writes.
type Asker func(ctx context.Context, t *Task) (deps.Report, error)

// Ask asks the generator of t, which is how what a run reads and writes is
// known, whatever version of it the module resolves to.
func Ask(ctxt build.Context) Asker {
	return func(ctx context.Context, t *Task) (deps.Report, error) {
		for _, arg := range t.Args {
			if arg == "--" {
				break
			}
			name, _, _ := strings.Cut(arg, "=")
			if name == "-deps" || name == "--deps" {
				return deps.Report{}, fmt.Errorf("%s: -deps is reserved for kanna gen; remove it from the go:generate line", t.Tool)
			}
		}

		cmd := t.At[0].Command(ctx, ctxt, t.Words("-deps"))
		var stderr strings.Builder
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return deps.Report{}, whyNot(ctx, ctxt, t, stderr.String(), err)
		}

		var r deps.Report
		if err := json.Unmarshal(out, &r); err != nil {
			return deps.Report{}, fmt.Errorf("%s -deps answered what kanna cannot read: %w", t.Tool, err)
		}
		switch {
		case r.Protocol > deps.Protocol:
			return deps.Report{}, fmt.Errorf("%s answers -deps in protocol %d, and this kanna reads up to %d; update kanna",
				t.Tool, r.Protocol, deps.Protocol)
		case r.Protocol < 1:
			return deps.Report{}, fmt.Errorf("%s answered -deps with no protocol", t.Tool)
		}
		return r, nil
	}
}

// whyNot says why asking t failed, with stderr, what it reported.
func whyNot(ctx context.Context, ctxt build.Context, t *Task, stderr string, err error) error {
	exit, ran := errors.AsType[*exec.ExitError](err)
	switch {
	case !ran:
		return fmt.Errorf("cannot run %s: %w", strings.Join(t.Words(), " "), err)
	case strings.Contains(stderr, "flag provided but not defined: -deps"):
		version := t.Tool
		cmd := t.At[0].Command(ctx, ctxt, t.Words("-version")[:4])
		if out, err := cmd.Output(); err == nil {
			version = strings.TrimSpace(string(out))
		}
		return fmt.Errorf("%s is too old to tell kanna gen what it reads and writes; update it: %s",
			version, getTool(t.Tool))
	case strings.Contains(stderr, "no such tool"):
		return fmt.Errorf("%s is not a tool of this module; add it: %s", t.Tool, getTool(t.Tool))
	case strings.Contains(stderr, "exec format error"):
		return fmt.Errorf("%s cannot run on this machine: go tool builds it for %s/%s, as it does under go generate; "+
			"generate with the GOOS and GOARCH of this machine", t.Tool, ctxt.GOOS, ctxt.GOARCH)
	}
	return fmt.Errorf("%s -deps: %w: %s", t.Tool, exit, strings.TrimSpace(stderr))
}

// getTool returns the command that adds tool to a module.
func getTool(tool string) string {
	family := strings.TrimPrefix(tool, "kanna-")
	return "go get -tool github.com/go-kanna/" + family + "/cmd/" + tool + "@latest"
}

// mentionsKanna matches a line that runs a generator of the family, or kanna
// itself, in a way other than go tool kanna-*.
var mentionsKanna = regexp.MustCompile(`\bkanna-[a-z0-9]+\b|\bgo\s+tool\s+kanna\b`)

// tasksOf returns the tasks of the directives of m, and what is wrong with
// those that mean to run a generator in a way kanna gen does not take on. Other
// lines are none of its business.
func tasksOf(m *module.Module, ctxt build.Context) ([]*Task, []located) {
	var (
		tasks    []*Task
		byLine   = make(map[string]*Task)
		problems []located
	)
	for _, d := range m.Directives {
		words, err := d.Words(d.Env(ctxt))
		switch {
		case err != nil && mentionsKanna.MatchString(d.Text):
			problems = append(problems, atLine(d, "cannot read this line: %v", err))
			continue
		case err != nil:
			continue
		case !isGeneration(words):
			if mentionsKanna.MatchString(d.Text) {
				problems = append(problems, unsupported(d))
			}
			continue
		}

		key := dirOf(d) + "\x00" + strings.Join(words, "\x00")
		if t := byLine[key]; t != nil {
			t.At = append(t.At, d)
			continue
		}
		t := &Task{At: []generate.Directive{d}, Tool: words[2], Args: words[3:]}
		byLine[key] = t
		tasks = append(tasks, t)
	}
	return tasks, problems
}

// isGeneration reports whether words are the normal run of a generator. A
// subcommand of kanna-orm talks to a database, which kanna gen does not do.
func isGeneration(words []string) bool {
	if len(words) < 3 || words[0] != "go" || words[1] != "tool" || !strings.HasPrefix(words[2], "kanna-") {
		return false
	}
	return words[2] != "kanna-orm" || len(words) == 3 || words[3] != "describe"
}

// located is a problem at a directive.
type located struct {
	directive generate.Directive
	diag      diag.Diag
}

func atLine(d generate.Directive, format string, args ...any) located {
	return located{directive: d, diag: diag.Errorf(positionOf(d), format, args...)}
}

func unsupported(d generate.Directive) located {
	l := atLine(d, "kanna gen does not run this line: %s", d.Text)
	l.diag = l.diag.WithHints("it runs //go:generate go tool kanna-* lines, and leaves out a -command alias, " +
		"a shell, another executable, and kanna-orm describe")
	return l
}

func positionOf(d generate.Directive) token.Position {
	return token.Position{Filename: d.File, Line: d.Line}
}

// askAll asks every task, some at a time, and returns what is wrong with those
// it could not ask.
func askAll(ctx context.Context, ask Asker, tasks []*Task) map[*Task]diag.Diag {
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		slots  = make(chan struct{}, runtime.GOMAXPROCS(0))
		failed = make(map[*Task]diag.Diag)
	)
	for _, t := range tasks {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()

			r, err := ask(ctx, t)
			if err != nil {
				mu.Lock()
				defer mu.Unlock()
				failed[t] = diag.Errorf(positionOf(t.At[0]), "%v", err)
				return
			}
			t.Report = r
		})
	}
	wg.Wait()
	return failed
}
