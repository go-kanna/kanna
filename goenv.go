package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/build"
	"os/exec"
	"strings"
)

// contextFormat has go list print the fields of its build context that decide
// which files it reads, a line for each, in the order goContext takes them.
const contextFormat = `{{context.GOROOT}}
{{context.GOOS}}
{{context.GOARCH}}
{{context.CgoEnabled}}
{{context.Compiler}}
{{join context.BuildTags " "}}
{{join context.ToolTags " "}}
{{join context.ReleaseTags " "}}`

// goContext returns the build context of the go command in dir, which goes by
// the environment, GOFLAGS, and what go env -w saved. It asks about package
// unsafe, which is there whatever state the module is in.
func goContext(ctx context.Context, dir string) (build.Context, error) {
	cmd := exec.CommandContext(ctx, "go", "list", "-f", contextFormat, "unsafe")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		if exit, ok := errors.AsType[*exec.ExitError](err); ok {
			return build.Context{}, fmt.Errorf("read the go environment: %s", bytes.TrimSpace(exit.Stderr))
		}
		return build.Context{}, fmt.Errorf("read the go environment: %w", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != 8 {
		return build.Context{}, fmt.Errorf("read the go environment: %q", out)
	}
	return build.Context{
		GOROOT:      lines[0],
		GOOS:        lines[1],
		GOARCH:      lines[2],
		CgoEnabled:  lines[3] == "true",
		Compiler:    lines[4],
		BuildTags:   strings.Fields(lines[5]),
		ToolTags:    strings.Fields(lines[6]),
		ReleaseTags: strings.Fields(lines[7]),
	}, nil
}
