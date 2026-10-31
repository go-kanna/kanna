// Package generate reads //go:generate lines as go generate does, and runs what
// they say in the environment it gives a command. A line that goes through it
// runs as it would under go generate, in another order.
package generate

import (
	"bytes"
	"context"
	"errors"
	"go/build"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Directive is a //go:generate line.
type Directive struct {
	// File is the file it is in, by absolute path.
	File string

	// Line is its line, counting from 1.
	Line int

	// Package is what the file's package clause says.
	Package string

	// Text is what follows //go:generate, as written.
	Text string
}

// Scan returns the //go:generate lines of src, the file at path of package pkg,
// which go generate finds by looking at each line and at nothing else: one in a
// raw string counts too.
func Scan(path string, src []byte, pkg string) []Directive {
	var found []Directive
	line := 0
	for rest := src; len(rest) > 0; {
		line++
		var text []byte
		text, rest, _ = bytes.Cut(rest, []byte("\n"))
		after, ok := bytes.CutPrefix(text, []byte("//go:generate"))
		if !ok || len(after) == 0 || after[0] != ' ' && after[0] != '\t' {
			continue
		}
		found = append(found, Directive{
			File:    path,
			Line:    line,
			Package: pkg,
			Text:    string(bytes.TrimSuffix(after[1:], []byte("\r"))),
		})
	}
	return found
}

// Env returns the variables go generate sets for a command of d, with ctxt as
// its build context. A command starts from the environment of the process with
// these added.
func (d Directive) Env(ctxt build.Context) []string {
	return []string{
		"GOROOT=" + ctxt.GOROOT,
		"GOARCH=" + ctxt.GOARCH,
		"GOOS=" + ctxt.GOOS,
		"GOFILE=" + filepath.Base(d.File),
		"GOLINE=" + strconv.Itoa(d.Line),
		"GOPACKAGE=" + d.Package,
		"DOLLAR=$",
		"PATH=" + filepath.Join(ctxt.GOROOT, "bin") + string(os.PathListSeparator) + os.Getenv("PATH"),
		"PWD=" + filepath.Dir(d.File),
	}
}

// Words splits d into the words of its command: a word ends at a space or a
// tab, one in double quotes is read as a Go string, and $NAME and ${NAME} become
// what env, as Env gives it, or else the process's environment holds.
func (d Directive) Words(env []string) ([]string, error) {
	var words []string
	for line := strings.TrimLeft(d.Text, " \t"); line != ""; line = strings.TrimLeft(line, " \t") {
		var word string
		if line[0] == '"' {
			var err error
			if word, line, err = quoted(line); err != nil {
				return nil, err
			}
		} else {
			end := strings.IndexAny(line, " \t")
			if end < 0 {
				end = len(line)
			}
			word, line = line[:end], line[end:]
		}
		words = append(words, word)
	}

	for i, word := range words {
		words[i] = os.Expand(word, func(name string) string { return lookup(env, name) })
	}
	return words, nil
}

// Command returns the command words run for d, as go generate starts it: in the
// directory of d, with what Env gives on top of the environment of the process,
// and preferring what is in the GOROOT of ctxt for a bare name, for go to be the
// one that runs this.
func (d Directive) Command(ctx context.Context, ctxt build.Context, words []string) *exec.Cmd {
	name := words[0]
	if !strings.ContainsRune(name, filepath.Separator) {
		if path, err := exec.LookPath(filepath.Join(ctxt.GOROOT, "bin", name)); err == nil {
			name = path
		}
	}
	cmd := exec.CommandContext(ctx, name, words[1:]...) //nolint:gosec // what the directive says, as for go generate
	cmd.Args[0] = words[0]
	cmd.Dir = filepath.Dir(d.File)
	cmd.Env = append(os.Environ(), d.Env(ctxt)...)
	return cmd
}

// quoted returns the quoted word line starts with, unquoted, and what follows.
func quoted(line string) (word, rest string, err error) {
	for i := 1; i < len(line); i++ {
		switch line[i] {
		case '\\':
			i++
		case '"':
			word, err := strconv.Unquote(line[:i+1])
			if err != nil {
				return "", "", errors.New("bad quoted string")
			}
			rest := line[i+1:]
			if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
				return "", "", errors.New("expected a space after a quoted word")
			}
			return word, rest, nil
		}
	}
	return "", "", errors.New("a quoted word does not end")
}

func lookup(env []string, name string) string {
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, name+"="); ok {
			return v
		}
	}
	return os.Getenv(name)
}
