package mapper

import (
	"bytes"
	"fmt"
	"go/types"
	"slices"
	"strconv"
	"strings"
)

// importTracker assigns deterministic local names to imported packages
// and renders the import block.
type importTracker struct {
	outputPkgPath string
	names         map[string]string // import path -> local name
	used          map[string]bool   // local names taken
}

func newImportTracker(outputPkgPath string) *importTracker {
	return &importTracker{
		outputPkgPath: outputPkgPath,
		names:         make(map[string]string),
		used:          make(map[string]bool),
	}
}

// qualifier is a types.Qualifier that registers packages as they are
// rendered. The output package itself is never qualified.
func (t *importTracker) qualifier(p *types.Package) string {
	if p == nil || p.Path() == t.outputPkgPath {
		return ""
	}
	return t.addPath(p.Path(), p.Name())
}

// addPath registers an import and returns its local name. Names that
// would collide with other imports or generated identifiers are suffixed
// deterministically.
func (t *importTracker) addPath(path, name string) string {
	if existing, ok := t.names[path]; ok {
		return existing
	}
	base := name
	if base == "src" || isTempPattern(base) {
		base += "pkg"
	}
	candidate := base
	for n := 2; t.used[candidate]; n++ {
		candidate = base + strconv.Itoa(n)
	}
	t.names[path] = candidate
	t.used[candidate] = true
	return candidate
}

// isTempPattern reports whether name collides with the temporaries the
// emitter generates (v0, err0, i0, e0, ...).
func isTempPattern(name string) bool {
	for _, prefix := range []string{"v", "err", "i", "e"} {
		rest, ok := strings.CutPrefix(name, prefix)
		if !ok || rest == "" {
			continue
		}
		digits := true
		for _, r := range rest {
			if r < '0' || r > '9' {
				digits = false
				break
			}
		}
		if digits {
			return true
		}
	}
	return false
}

// write renders the import block: standard library first, then the rest,
// each group sorted by path.
func (t *importTracker) write(buf *bytes.Buffer) {
	if len(t.names) == 0 {
		return
	}
	paths := make([]string, 0, len(t.names))
	for path := range t.names {
		paths = append(paths, path)
	}
	slices.Sort(paths)

	var std, rest []string
	for _, path := range paths {
		if isStdlibPath(path) {
			std = append(std, path)
		} else {
			rest = append(rest, path)
		}
	}

	buf.WriteString("import (\n")
	for _, path := range std {
		t.writeSpec(buf, path)
	}
	if len(std) > 0 && len(rest) > 0 {
		buf.WriteByte('\n')
	}
	for _, path := range rest {
		t.writeSpec(buf, path)
	}
	buf.WriteString(")\n")
}

func (t *importTracker) writeSpec(buf *bytes.Buffer, path string) {
	name := t.names[path]
	if name == lastPathElem(path) {
		fmt.Fprintf(buf, "%q\n", path)
		return
	}
	fmt.Fprintf(buf, "%s %q\n", name, path)
}

func isStdlibPath(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

func lastPathElem(path string) string {
	return path[strings.LastIndex(path, "/")+1:]
}
