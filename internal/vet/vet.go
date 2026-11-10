// Package vet runs the vets of the generators a module runs, each of which
// checks the code calling what its generator writes.
package vet

import (
	"context"
	"errors"
	"fmt"
	"go/build"
	"go/token"
	"io"
	"os/exec"
	"strings"

	"github.com/go-kanna/internal/diag"

	"github.com/go-kanna/kanna/internal/generate"
	"github.com/go-kanna/kanna/internal/module"
	"github.com/go-kanna/kanna/internal/plan"
)

// Tools holds the generators that have a vet, in the order kanna vet runs them.
var Tools = []string{"kanna-value", "kanna-orm", "kanna-i18n"}

// Vet is the vet of a generator a module runs.
type Vet struct {
	// Tool is the generator, such as kanna-orm.
	Tool string

	// At is the first //go:generate line of the module that runs it.
	At generate.Directive
}

// Of returns the vets of the generators the //go:generate lines of m run with
// go tool, in the order of Tools.
func Of(m *module.Module, ctxt build.Context) []Vet {
	first := make(map[string]generate.Directive)
	for _, d := range m.Directives {
		words, err := d.Words(d.Env(ctxt))
		if err != nil || len(words) < 3 || words[0] != "go" || words[1] != "tool" {
			continue
		}
		if _, ok := first[words[2]]; !ok {
			first[words[2]] = d
		}
	}

	var vets []Vet
	for _, tool := range Tools {
		if d, ok := first[tool]; ok {
			vets = append(vets, Vet{Tool: tool, At: d})
		}
	}
	return vets
}

// Check returns what keeps each of vets from running in dir: a generator that
// is not a tool of the module, or one at a version with no vet. It asks the tool
// the go command resolves, whatever go.mod, a replace, or a workspace make it.
func Check(ctx context.Context, dir string, vets []Vet) []diag.Diag {
	var ds []diag.Diag
	for _, v := range vets {
		if _, stderr, err := goTool(ctx, dir, v.Tool, "vet", "-version"); err != nil {
			pos := token.Position{Filename: v.At.File, Line: v.At.Line}
			ds = append(ds, diag.Errorf(pos, "%v", whyNot(ctx, dir, v.Tool, stderr, err)))
		}
	}
	return ds
}

// whyNot says why tool did not answer vet -version, with stderr, what it
// reported. A tool that answers -version runs, so its vet is what is missing.
func whyNot(ctx context.Context, dir, tool, stderr string, err error) error {
	if _, ran := errors.AsType[*exec.ExitError](err); !ran {
		return fmt.Errorf("cannot run go tool %s: %w", tool, err)
	}
	if strings.Contains(stderr, "no such tool") {
		return fmt.Errorf("%s is not a tool of this module; add it: %s", tool, plan.GetTool(tool))
	}
	if err := plan.NotBuilt(tool, stderr, func(words ...string) string {
		out, _, err := goCmd(ctx, dir, words...)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(out)
	}); err != nil {
		return err //nolint:wrapcheck // says all there is to say
	}
	version, _, verr := goTool(ctx, dir, tool, "-version")
	if verr != nil {
		return fmt.Errorf("go tool %s vet -version: %w: %s", tool, err, strings.TrimSpace(stderr))
	}
	return fmt.Errorf("%s has no vet; update it: %s", strings.TrimSpace(version), plan.GetTool(tool))
}

// goTool runs tool through go tool, with args, in dir, and returns what it
// printed.
func goTool(ctx context.Context, dir, tool string, args ...string) (stdout, stderr string, err error) {
	return goCmd(ctx, dir, append([]string{"tool", tool}, args...)...)
}

// goCmd runs the go command with words in dir, and returns what it printed.
func goCmd(ctx context.Context, dir string, words ...string) (stdout, stderr string, err error) {
	cmd := exec.CommandContext(ctx, "go", words...) //nolint:gosec // a generator of the family
	cmd.Dir = dir
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	return out.String(), errOut.String(), err
}

// Config says how to run vets.
type Config struct {
	// Dir is where kanna vet runs, which the vets run in, and write the paths
	// of what they report from.
	Dir string

	// Tags holds the comma-separated build tags to load the packages with.
	Tags string

	// Patterns holds the packages to check.
	Patterns []string

	// Out receives what each vet prints.
	Out io.Writer
}

// Run runs vets, one after the other, writing what each prints below a line
// naming it, as go vet does for a package. It reports whether all of them
// passed.
func Run(ctx context.Context, cfg Config, vets []Vet) bool {
	passed := true
	for _, v := range vets {
		args := []string{"tool", v.Tool, "vet"}
		if cfg.Tags != "" {
			args = append(args, "-tags", cfg.Tags)
		}
		cmd := exec.CommandContext(ctx, "go", append(args, cfg.Patterns...)...) //nolint:gosec // a generator of the family
		cmd.Dir = cfg.Dir
		out, err := cmd.CombinedOutput()
		output := string(out)
		if err != nil {
			passed = false
			if strings.TrimSpace(output) == "" {
				output = err.Error()
			}
		}
		if strings.TrimSpace(output) != "" {
			fmt.Fprintf(cfg.Out, "# %s\n%s\n", v.Tool, strings.TrimRight(output, "\n"))
		}
	}
	return passed
}
