package mapper

import (
	"bytes"
	"fmt"
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

// writeImports renders the import block: standard library first, then the rest,
// each group sorted by path.
func writeImports(buf *bytes.Buffer, t *imports.Tracker) {
	entries := t.Entries()
	if len(entries) == 0 {
		return
	}

	var std, rest []imports.Entry
	for _, e := range entries {
		if isStdlibPath(e.Path) {
			std = append(std, e)
		} else {
			rest = append(rest, e)
		}
	}

	buf.WriteString("import (\n")
	for _, e := range std {
		writeImportSpec(buf, e)
	}
	if len(std) > 0 && len(rest) > 0 {
		buf.WriteByte('\n')
	}
	for _, e := range rest {
		writeImportSpec(buf, e)
	}
	buf.WriteString(")\n")
}

func writeImportSpec(buf *bytes.Buffer, e imports.Entry) {
	if e.Alias == lastPathElem(e.Path) {
		fmt.Fprintf(buf, "%q\n", e.Path)
		return
	}
	fmt.Fprintf(buf, "%s %q\n", e.Alias, e.Path)
}

func isStdlibPath(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

func lastPathElem(path string) string {
	return path[strings.LastIndex(path, "/")+1:]
}
