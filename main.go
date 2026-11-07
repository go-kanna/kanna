// Command kanna runs the kanna code generators of a module, each //go:generate
// go tool kanna-* line once, in the order what they write and read asks for,
// and the vets of those that have one.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"go/build"
	"go/token"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-kanna/internal/diag"

	"github.com/go-kanna/kanna/internal/module"
	"github.com/go-kanna/kanna/internal/plan"
	"github.com/go-kanna/kanna/internal/run"
	"github.com/go-kanna/kanna/internal/vet"
)

// exitUsage is the code to exit with when the command line itself is wrong.
const exitUsage = 2

const usage = `kanna runs the kanna code generators of a module, in the order they need,
and their vets.

Usage:
  kanna gen [-plan | -check] [packages]
  kanna vet [-tags list] [packages]

kanna gen runs each //go:generate go tool kanna-* line once, in an order worked
out from what each reads and writes. The packages default to ./..., and bring
along what writes what they read, not what reads them.

Flags of kanna gen:
  -plan   print what would run, in order and why, and run nothing
  -check  compare what is on disk with what would be generated, writing nothing;
          KANNA_CHECK=1 does the same

kanna vet runs the vet of each generator the module runs that has one, which
checks the code calling what the generator writes. The packages default to
./..., and the vets read what is generated as it is on disk, which kanna gen
-check tells is up to date.

Flags of kanna vet:
  -tags   load the packages with the comma-separated build tags, as the go
          command does
`

// env is where a command runs and reports.
type env struct {
	dir            string
	getenv         func(key string) string
	stdout, stderr io.Writer
}

func main() {
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "kanna: find the working directory: %v\n", err)
		os.Exit(run.ExitFailure)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := command(ctx, env{dir: dir, getenv: os.Getenv, stdout: os.Stdout, stderr: os.Stderr}, os.Args[1:])
	stop()
	os.Exit(code)
}

// command runs the command line args.
func command(ctx context.Context, e env, args []string) int {
	if len(args) == 0 {
		fmt.Fprint(e.stderr, usage)
		return exitUsage
	}
	switch args[0] {
	case "gen":
		return gen(ctx, e, args[1:])
	case "vet":
		return vetCommand(ctx, e, args[1:])
	case "help", "-h", "-help", "--help":
		fmt.Fprint(e.stdout, usage)
		return run.ExitOK
	}
	fmt.Fprintf(e.stderr, "kanna: unknown command %q\n\n%s", args[0], usage)
	return exitUsage
}

// gen runs kanna gen with args.
func gen(ctx context.Context, e env, args []string) int {
	fs := flag.NewFlagSet("kanna gen", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	showPlan := fs.Bool("plan", false, "")
	check := fs.Bool("check", false, "")

	err := fs.Parse(args)
	switch {
	case errors.Is(err, flag.ErrHelp):
		fmt.Fprint(e.stdout, usage)
		return run.ExitOK
	case err != nil:
		fmt.Fprintf(e.stderr, "kanna gen: %v\n\n%s", err, usage)
		return exitUsage
	case *showPlan && *check:
		fmt.Fprintf(e.stderr, "kanna gen: -plan and -check do not go together\n\n%s", usage)
		return exitUsage
	}
	if pattern, ok := flagAmong(fs.Args()); ok {
		fmt.Fprintf(e.stderr, "kanna gen: flags go before the packages: %s\n\n%s", pattern, usage)
		return exitUsage
	}

	byEnv, err := checkEnv(e.getenv)
	if err != nil {
		fmt.Fprintf(e.stderr, "kanna gen: %v\n", err)
		return exitUsage
	}

	// What generators report is by the paths the go command resolves, so the
	// module has to go by those too.
	dir, err := filepath.EvalSymlinks(e.dir)
	if err != nil {
		fmt.Fprintf(e.stderr, "kanna gen: %v\n", err)
		return run.ExitFailure
	}
	p, ctxt, err := makePlan(ctx, dir, fs.Args())
	if err != nil {
		fmt.Fprintf(e.stderr, "kanna gen: %v\n", err)
		return run.ExitFailure
	}

	if *showPlan {
		p.Write(e.stdout)
	}
	diag.Write(e.stderr, dir, p.Warnings)
	if len(p.Problems) > 0 {
		diag.Write(e.stderr, dir, p.Problems)
		return run.ExitFailure
	}
	if *showPlan {
		return run.ExitOK
	}
	return run.Run(ctx, p, run.Config{Build: ctxt, Check: *check || byEnv, Out: e.stdout, Err: e.stderr, Dir: dir})
}

// vetCommand runs kanna vet with args.
func vetCommand(ctx context.Context, e env, args []string) int {
	fs := flag.NewFlagSet("kanna vet", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	tags := fs.String("tags", "", "")

	err := fs.Parse(args)
	switch {
	case errors.Is(err, flag.ErrHelp):
		fmt.Fprint(e.stdout, usage)
		return run.ExitOK
	case err != nil:
		fmt.Fprintf(e.stderr, "kanna vet: %v\n\n%s", err, usage)
		return exitUsage
	}
	if pattern, ok := flagAmong(fs.Args()); ok {
		fmt.Fprintf(e.stderr, "kanna vet: flags go before the packages: %s\n\n%s", pattern, usage)
		return exitUsage
	}

	dir, err := filepath.EvalSymlinks(e.dir)
	if err != nil {
		fmt.Fprintf(e.stderr, "kanna vet: %v\n", err)
		return run.ExitFailure
	}
	ctxt, err := goContext(ctx, dir)
	if err != nil {
		fmt.Fprintf(e.stderr, "kanna vet: %v\n", err)
		return run.ExitFailure
	}
	if *tags != "" {
		// The vets are given the flag, which takes the place of the tags of
		// GOFLAGS for them, so it does here too, where the lines are found.
		ctxt.BuildTags = buildTags(*tags)
	}
	m, err := module.Find(dir, ctxt)
	if err != nil {
		fmt.Fprintf(e.stderr, "kanna vet: find the module: %v\n", err)
		return run.ExitFailure
	}

	vets := vet.Of(m, ctxt)
	if len(vets) == 0 {
		d := diag.Warningf(token.Position{}, "none of the generators the module runs has a vet").
			WithHints("those with one are " + strings.Join(vet.Tools, ", "))
		diag.Write(e.stderr, dir, []diag.Diag{d})
		return run.ExitOK
	}
	if ds := vet.Check(ctx, dir, vets); len(ds) > 0 {
		diag.Write(e.stderr, dir, ds)
		return run.ExitFailure
	}

	patterns := fs.Args()
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	if !vet.Run(ctx, vet.Config{Dir: dir, Tags: *tags, Patterns: patterns, Out: e.stderr}, vets) {
		return run.ExitFailure
	}
	return run.ExitOK
}

// buildTags reads the list of a -tags flag as the go command does: separated
// by commas, or by spaces as it once was.
func buildTags(list string) []string {
	return strings.FieldsFunc(list, func(r rune) bool { return r == ',' || r == ' ' })
}

// flagAmong returns the first of patterns that is a flag, which the flag
// package leaves there when it comes after a package.
func flagAmong(patterns []string) (string, bool) {
	for _, pattern := range patterns {
		if strings.HasPrefix(pattern, "-") {
			return pattern, true
		}
	}
	return "", false
}

// makePlan plans a run of what patterns, read from dir, select in the module of
// dir, and returns it with the build context it is to run with.
func makePlan(ctx context.Context, dir string, patterns []string) (*plan.Plan, build.Context, error) {
	ctxt, err := goContext(ctx, dir)
	if err != nil {
		return nil, build.Context{}, err
	}
	m, err := module.Find(dir, ctxt)
	if err != nil {
		return nil, build.Context{}, fmt.Errorf("find the module: %w", err)
	}
	selection, err := m.Select(dir, patterns)
	if err != nil {
		return nil, build.Context{}, fmt.Errorf("read the packages: %w", err)
	}
	p, err := plan.Make(ctx, plan.Config{
		Module: m, Selection: selection, Patterns: patterns, Build: ctxt, Ask: plan.Ask(ctxt),
	})
	if err != nil {
		return nil, build.Context{}, fmt.Errorf("plan: %w", err)
	}
	return p, ctxt, nil
}

// checkEnv reports whether KANNA_CHECK turns on -check, as it does -check in
// every generator. A value that is not a boolean is an error, rather than a run
// that writes what it was meant to check.
func checkEnv(getenv func(key string) string) (bool, error) {
	v := getenv("KANNA_CHECK")
	if v == "" {
		return false, nil
	}
	check, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("KANNA_CHECK=%q is not a boolean such as 1 or true", v)
	}
	return check, nil
}
