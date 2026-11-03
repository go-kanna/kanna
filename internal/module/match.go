package module

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Selection is what the patterns of a command line select: the directories of a
// module they match.
type Selection struct {
	roots []func(dir string) bool
}

// Select reads patterns as the go command does, from the directory cwd: paths,
// which start with a dot or are absolute, and import paths of the module, each
// matching the one directory it names. A pattern with ... in it matches those
// whose path the ... matches any string in, slashes included, and a /... at its
// end matches the directory before it too. No patterns mean ./... .
func (m *Module) Select(cwd string, patterns []string) (Selection, error) {
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	var s Selection
	for _, pattern := range patterns {
		match, err := m.matcher(cwd, pattern)
		if err != nil {
			return Selection{}, fmt.Errorf("pattern %s: %w", pattern, err)
		}
		s.roots = append(s.roots, match)
	}
	return s, nil
}

// matcher returns what tells the directories pattern matches.
func (m *Module) matcher(cwd, pattern string) (func(dir string) bool, error) {
	literal, _, wild := strings.Cut(pattern, "...")
	if pattern == "..." {
		literal, wild = "./", true
	}
	if !wild {
		dir, err := m.resolve(cwd, pattern)
		return func(d string) bool { return d == dir }, err
	}

	// What comes before the first ... has to be in the module, as a directory
	// it names, or its start: a/b... names no directory a/b.
	base := literal
	if !strings.HasSuffix(base, "/") {
		base = path.Dir(base)
	}
	if _, err := m.resolve(cwd, strings.TrimSuffix(base, "/")); err != nil && !strings.HasPrefix(m.Path, base) {
		return nil, err
	}

	if isPath(pattern) {
		full := filepath.ToSlash(filepath.Join(cwd, filepath.FromSlash(pattern)))
		if filepath.IsAbs(pattern) {
			full = filepath.ToSlash(filepath.Clean(pattern))
		}
		re := wildcard(full)
		return func(d string) bool { return re.MatchString(filepath.ToSlash(d)) }, nil
	}
	re := wildcard(pattern)
	return func(d string) bool { return m.Contains(d) && re.MatchString(m.importPath(d)) }, nil
}

// isPath reports whether pattern is a path rather than an import path.
func isPath(pattern string) bool {
	return filepath.IsAbs(pattern) || pattern == "." || pattern == ".." ||
		strings.HasPrefix(pattern, "./") || strings.HasPrefix(pattern, "../")
}

// wildcard compiles pattern as the go command does: ... matches any string, and
// a /... at the end matches nothing too.
func wildcard(pattern string) *regexp.Regexp {
	re := regexp.QuoteMeta(pattern)
	if trimmed, ok := strings.CutSuffix(re, `/\.\.\.`); ok {
		re = trimmed + `(/\.\.\.)?`
	}
	return regexp.MustCompile("^" + strings.ReplaceAll(re, `\.\.\.`, ".*") + "$")
}

// resolve returns the directory name stands for, a path or an import path.
func (m *Module) resolve(cwd, name string) (string, error) {
	if filepath.IsAbs(name) {
		return m.within(filepath.Clean(name))
	}
	if isPath(name) {
		return m.within(filepath.Join(cwd, filepath.FromSlash(name)))
	}
	if dir, ok := m.DirOf(name); ok {
		return dir, nil
	}
	return "", fmt.Errorf("not in the module %s", m.Path)
}

func (m *Module) within(dir string) (string, error) {
	if !m.Contains(dir) {
		return "", fmt.Errorf("not in the module %s", m.Path)
	}
	return dir, nil
}

// Matches reports whether dir is selected.
func (s Selection) Matches(dir string) bool {
	return slices.ContainsFunc(s.roots, func(match func(string) bool) bool { return match(dir) })
}
