package plan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/build"
	"go/token"
	"os/exec"
	"path"
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

// TagsFlag returns the flag that has a generator load its packages with tags,
// the comma-separated build tags of kanna gen -tags, or nothing for none.
func TagsFlag(tags string) []string {
	if tags == "" {
		return nil
	}
	return []string{"-tags", tags}
}

// Ask asks the generator of t, which is how what a run reads and writes is
// known, whatever version of it the module resolves to. The generator loads its
// packages with tags, as TagsFlag gives them.
func Ask(ctxt build.Context, tags string) Asker {
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

		cmd := t.At[0].Command(ctx, ctxt, t.Words(append(TagsFlag(tags), "-deps")...))
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
			version, GetTool(t.Tool))
	case strings.Contains(stderr, "no such tool"):
		return fmt.Errorf("%s is not a tool of this module; add it: %s", t.Tool, GetTool(t.Tool))
	case strings.Contains(stderr, "exec format error"):
		return fmt.Errorf("%s cannot run on this machine: go tool builds it for %s/%s, as it does under go generate; "+
			"generate with the GOOS and GOARCH of this machine", t.Tool, ctxt.GOOS, ctxt.GOARCH)
	}
	if err := NotBuilt(t.Tool, stderr, func(words ...string) string {
		out, err := t.At[0].Command(ctx, ctxt, append([]string{"go"}, words...)).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}); err != nil {
		return err
	}
	return fmt.Errorf("%s -deps: %w: %s", t.Tool, exit, strings.TrimSpace(stderr))
}

// familyBuild matches the line go tool prints above the errors of a package of
// the family it cannot build.
var familyBuild = regexp.MustCompile(`(?m)^# github\.com/go-kanna/`)

// NotBuilt returns why tool cannot run when stderr, what go tool printed, says
// that the go command cannot build it: a release of a generator does not build
// with the go-kanna/internal a newer one makes the module select. It returns nil
// when stderr says otherwise, or the module takes tool from no release, as from
// a workspace. goCmd runs the go command with words where tool runs and returns
// what it printed, or nothing when it fails.
func NotBuilt(tool, stderr string, goCmd func(words ...string) string) error {
	if !familyBuild.MatchString(stderr) {
		return nil
	}
	version := goCmd("list", "-m", "-f", "{{.Version}}", "github.com/go-kanna/"+strings.TrimPrefix(tool, "kanna-"))
	if version == "" {
		return nil
	}
	with := "the modules this module selects"
	if internal := goCmd("list", "-m", "-f", "{{.Version}}", "github.com/go-kanna/internal"); internal != "" {
		with = "github.com/go-kanna/internal " + internal + ", which this module selects"
	}
	return fmt.Errorf("%s %s does not build with %s; update it: %s, or update every tool: go get tool",
		tool, version, with, GetTool(tool))
}

// GetTool returns the command that adds tool to a module.
func GetTool(tool string) string {
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
				problems = append(problems, unsupported(d, words))
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

// isGeneration reports whether words are the normal run of a generator. Two
// subcommands are not: kanna-orm describe talks to a database, and the vet of a
// generator checks code and writes nothing, neither of which kanna gen does.
func isGeneration(words []string) bool {
	if len(words) < 3 || words[0] != "go" || words[1] != "tool" || !strings.HasPrefix(words[2], "kanna-") {
		return false
	}
	if len(words) == 3 {
		return true
	}
	switch words[3] {
	case "vet":
		return false
	case "describe":
		return words[2] != "kanna-orm"
	}
	return true
}

// located is a problem at a directive.
type located struct {
	directive generate.Directive
	diag      diag.Diag
}

func atLine(d generate.Directive, format string, args ...any) located {
	return located{directive: d, diag: diag.Errorf(positionOf(d), format, args...)}
}

func unsupported(d generate.Directive, words []string) located {
	l := atLine(d, "kanna gen does not run this line: %s", d.Text)
	if len(words) >= 3 && words[0] == "go" && words[1] == "tool" && strings.Contains(words[2], "/") {
		name := path.Base(words[2])
		if isGeneration(slices.Concat([]string{"go", "tool", name}, words[3:])) {
			l.diag = l.diag.WithHints(fmt.Sprintf("it tells a generator by its name: write go tool %s in place of go tool %s",
				name, words[2]))
			return l
		}
	}
	l.diag = l.diag.WithHints("it runs //go:generate go tool kanna-* lines, and leaves out a -command alias, " +
		"a shell, another executable, kanna-orm describe, and the vet of a generator")
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
