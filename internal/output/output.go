// Package output writes what a generator produced, with the guards every
// generator needs and none should reimplement: never replace a file that is
// not generated, tell staleness apart from I/O failure, and resolve paths
// against the directory the tool was invoked in.
package output

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
)

// Resolve interprets path against dir. An absolute path is already anchored,
// and an empty dir means the process working directory, so both pass through.
func Resolve(dir, path string) string {
	if dir == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dir, path)
}

// CheckUpToDate reports whether the file at path holds exactly want.
//
// The failure modes are deliberately distinct: a missing file has not been
// generated, a file without a generated-code marker is somebody's hand-written
// work that go generate would refuse to touch, and an unreadable file says
// nothing about staleness at all. Collapsing them into "out of date" sends the
// user to a go generate that cannot help.
func CheckUpToDate(path string, want []byte) error {
	existing, err := os.ReadFile(filepath.Clean(path))
	switch {
	case errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("%s has not been generated yet (run go generate)", path)
	case err != nil:
		return fmt.Errorf("read %s: %w", path, err)
	}
	if bytes.Equal(existing, want) {
		return nil
	}
	if !isGenerated(existing) {
		return fmt.Errorf(
			"%s lacks a generated-code header; it looks hand-written, and regenerating will refuse to replace it", path)
	}
	return fmt.Errorf("%s is out of date (run go generate)", path)
}

// Write writes generated source to path, creating the directory as needed. A
// file already holding exactly src is left alone. A file without a
// generated-code marker is refused: the flags point wherever they point, and a
// typo must not cost anyone a hand-written file.
func Write(path string, src []byte) error {
	existing, err := os.ReadFile(filepath.Clean(path))
	switch {
	case err == nil:
		if bytes.Equal(existing, src) {
			return nil
		}
		if !isGenerated(existing) {
			return fmt.Errorf("refusing to overwrite %s: it lacks a generated-code header", path)
		}
	case !errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("read existing output: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	// Generated Go source should be readable like the rest of the package,
	// matching what gofmt/go generate produce by default.
	//nolint:gosec // generated source is meant to be world-readable
	if err := os.WriteFile(path, src, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// isGenerated reports whether src carries the generated-code marker, by the
// same rules the rest of the toolchain uses.
//
// A prefix comparison is not enough: anything preceding the marker — a license
// header a company-wide tool prepends, a build constraint — would hide it and
// lock every later regeneration out of its own output. ast.IsGenerated reads
// the file the way Go defines it.
func isGenerated(src []byte) bool {
	f, err := parser.ParseFile(token.NewFileSet(), "", src, parser.PackageClauseOnly|parser.ParseComments)
	if err != nil {
		return false
	}
	return ast.IsGenerated(f)
}
