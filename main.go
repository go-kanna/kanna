// Command kanna runs the kanna code generators of a module, each //go:generate
// go tool kanna-* line once, in the order what they write and read asks for.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"

	"github.com/go-kanna/internal/diag"

	"github.com/go-kanna/kanna/internal/module"
	"github.com/go-kanna/kanna/internal/plan"
	"github.com/go-kanna/kanna/internal/run"
)

// exitUsage is the code to exit with when the command line itself is wrong.
const exitUsage = 2

const usage = `kanna runs the kanna code generators of a module, in the order they need.

Usage:
  kanna gen [--plan | --check] [packages]

kanna gen runs each //go:generate go tool kanna-* line once, in an order worked
out from what each reads and writes. The packages default to ./..., and bring
along what writes what they read, not what reads them.

Flags of kanna gen:
  --plan   print what would run, in order and why, and run nothing
  --check  compare what is on disk with what would be generated, writing nothing;
           KANNA_CHECK=1 does the same
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
		fmt.Fprintf(e.stderr, "kanna gen: --plan and --check do not go together\n\n%s", usage)
		return exitUsage
	}
	for _, pattern := range fs.Args() {
		if strings.HasPrefix(pattern, "-") {
			fmt.Fprintf(e.stderr, "kanna gen: flags go before the packages: %s\n\n%s", pattern, usage)
			return exitUsage
		}
	}

	byEnv, err := checkEnv(e.getenv)
	if err != nil {
		fmt.Fprintf(e.stderr, "kanna gen: %v\n", err)
		return exitUsage
	}

	p, goroot, err := makePlan(ctx, e.dir, fs.Args())
	if err != nil {
		fmt.Fprintf(e.stderr, "kanna gen: %v\n", err)
		return run.ExitFailure
	}

	if *showPlan {
		p.Write(e.stdout)
	}
	if len(p.Problems) > 0 {
		diag.Write(e.stderr, e.dir, p.Problems)
		return run.ExitFailure
	}
	if *showPlan {
		return run.ExitOK
	}
	return run.Run(ctx, p, run.Config{GOROOT: goroot, Check: *check || byEnv, Out: e.stdout})
}

// makePlan plans a run of what patterns, read from dir, select in the module of
// dir, and returns it with the GOROOT it is to run with.
func makePlan(ctx context.Context, dir string, patterns []string) (*plan.Plan, string, error) {
	m, err := module.Find(dir)
	if err != nil {
		return nil, "", fmt.Errorf("find the module: %w", err)
	}
	selection, err := m.Select(dir, patterns)
	if err != nil {
		return nil, "", fmt.Errorf("read the packages: %w", err)
	}
	goroot, err := goRoot(ctx)
	if err != nil {
		return nil, "", err
	}
	p, err := plan.Make(ctx, plan.Config{
		Module: m, Selection: selection, Patterns: patterns, GOROOT: goroot, Ask: plan.Ask(goroot),
	})
	if err != nil {
		return nil, "", fmt.Errorf("plan: %w", err)
	}
	return p, goroot, nil
}

// goRoot returns the GOROOT of the go command, as go generate has it.
func goRoot(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "go", "env", "GOROOT").Output()
	if err != nil {
		return "", fmt.Errorf("run go env GOROOT: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// checkEnv reports whether KANNA_CHECK turns on --check, as it does -check in
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
