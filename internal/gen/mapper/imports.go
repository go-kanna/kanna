package mapper

import (
	"bytes"
	"strings"

	"github.com/go-kanna/kanna/internal/imports"
)

// newImportTracker returns the tracker for a file generated into outputPkgPath.
//
// The emitter names its temporaries v0, err0, i0, e0 and so on, so a package
// whose name looks like one of those is pushed aside before it can shadow it.
// "src" is the parameter every generated function takes.
func newImportTracker(outputPkgPath string) *imports.Tracker {
	return imports.New(outputPkgPath, func(base string) string {
		if base == "src" || isTempPattern(base) {
			return base + "pkg"
		}
		return base
	})
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

// writeImports renders the import block: standard library first, then the rest.
func writeImports(buf *bytes.Buffer, t *imports.Tracker) {
	imports.Render(buf, t.Entries(), func(e imports.Entry) int {
		if isStdlibPath(e.Path) {
			return 0
		}
		return 1
	})
}

// isStdlibPath reports whether path names a standard library package.
//
// The heuristic is the usual one: only a domain-shaped first element belongs to
// a module. A module path without a dot is misfiled here, which costs a grouping
// in the output and nothing else.
func isStdlibPath(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}
