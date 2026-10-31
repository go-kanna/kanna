package module

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// Selection is what the patterns of a command line select: the directories of a
// module they match.
type Selection struct {
	roots []selected
}

type selected struct {
	dir string
	all bool // and the directories under it
}

// Select reads patterns as the go command does, from the directory cwd: paths,
// which start with a dot or are absolute, and import paths of the module, each
// matching the one directory it names or, ending in /..., those under it too.
// No patterns mean ./... .
func (m *Module) Select(cwd string, patterns []string) (Selection, error) {
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	var s Selection
	for _, pattern := range patterns {
		dir, all := strings.CutSuffix(pattern, "/...")
		if pattern == "..." {
			dir, all = ".", true
		}
		abs, err := m.resolve(cwd, dir)
		if err != nil {
			return Selection{}, fmt.Errorf("pattern %s: %w", pattern, err)
		}
		s.roots = append(s.roots, selected{dir: abs, all: all})
	}
	return s, nil
}

// resolve returns the directory name stands for, a path or an import path.
func (m *Module) resolve(cwd, name string) (string, error) {
	if filepath.IsAbs(name) {
		return m.within(filepath.Clean(name))
	}
	if name == "." || name == ".." || strings.HasPrefix(name, "./") || strings.HasPrefix(name, "../") {
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
	return slices.ContainsFunc(s.roots, func(r selected) bool {
		if dir == r.dir {
			return true
		}
		return r.all && strings.HasPrefix(dir, r.dir+string(filepath.Separator))
	})
}
