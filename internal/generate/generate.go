// Package generate reads //go:generate lines as go generate does, and runs what
// they say in the environment it gives a command. A line that goes through it
// runs as it would under go generate, in another order.
package generate

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
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
		if after, ok := bytes.CutPrefix(text, []byte("//go:generate")); ok && len(after) > 0 && (after[0] == ' ' || after[0] == '\t') {
			found = append(found, Directive{
				File:    path,
				Line:    line,
				Package: pkg,
				Text:    string(bytes.TrimSuffix(after[1:], []byte("\r"))),
			})
		}
	}
	return found
}

// Env returns the variables go generate sets for a command of d, with goroot
// as what it takes GOROOT to be. A command starts from the environment of the
// process with these added.
func (d Directive) Env(goroot string) []string {
	return []string{
		"GOROOT=" + goroot,
		"GOARCH=" + goarch(),
		"GOOS=" + goos(),
		"GOFILE=" + filepath.Base(d.File),
		"GOLINE=" + strconv.Itoa(d.Line),
		"GOPACKAGE=" + d.Package,
		"DOLLAR=$",
		"PATH=" + filepath.Join(goroot, "bin") + string(os.PathListSeparator) + os.Getenv("PATH"),
		"PWD=" + filepath.Dir(d.File),
	}
}

func goarch() string {
	if v := os.Getenv("GOARCH"); v != "" {
		return v
	}
	return runtime.GOARCH
}

func goos() string {
	if v := os.Getenv("GOOS"); v != "" {
		return v
	}
	return runtime.GOOS
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
