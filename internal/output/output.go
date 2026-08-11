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
	"strings"
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

// PackageName resolves the package name of a generated file placed in
// destDir, whose own previous output is named generatedFile. destDir must be
// absolute, so that a relative destination such as "." still yields a
// directory name.
//
// Every file in a directory has to agree on the package clause, so an
// override cannot win over what is already there: the result would be a file
// nothing can compile alongside its neighbours.
func PackageName(override, destDir, generatedFile string) (string, error) {
	declared := declaredPackage(destDir, generatedFile)

	if override != "" && declared != "" && override != declared {
		return "", fmt.Errorf("-package %s conflicts with package %s, which %s already declares",
			override, declared, destDir)
	}

	if override != "" {
		return override, nil
	}
	if declared != "" {
		return declared, nil
	}

	return filepath.Base(destDir), nil
}

// declaredPackage returns the package the Go files in dir already declare.
// The generated file itself is skipped so a name written by a previous run
// cannot pin the next one, and test files are skipped because they may sit in
// an external _test package. An unreadable directory yields an empty name.
func declaredPackage(dir, generatedFile string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	fset := token.NewFileSet()

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == generatedFile {
			continue
		}

		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.PackageClauseOnly)
		if err != nil {
			continue
		}

		return f.Name.Name
	}

	return ""
}

// DeclaredNames returns the top-level identifiers the destination package
// already declares, each mapped to the position it is declared at, so a
// generator can refuse to redeclare one.
//
// Only files declaring pkgName are read: an external test package shares the
// directory but not the namespace, so nothing it declares can clash. The
// generated file itself is skipped as well, since it is what gets replaced.
func DeclaredNames(dir, pkgName, generatedFile string) map[string]token.Position {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	fset := token.NewFileSet()
	names := make(map[string]token.Position)

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == generatedFile || !strings.HasSuffix(name, ".go") {
			continue
		}

		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil || f.Name.Name != pkgName {
			continue
		}

		for _, decl := range f.Decls {
			collectNames(fset, decl, names)
		}
	}

	return names
}

func collectNames(fset *token.FileSet, decl ast.Decl, into map[string]token.Position) {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		// A method belongs to its receiver's namespace, not the package's.
		if d.Recv == nil {
			into[d.Name.Name] = fset.Position(d.Name.Pos())
		}
	case *ast.GenDecl:
		for _, spec := range d.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				into[s.Name.Name] = fset.Position(s.Name.Pos())
			case *ast.ValueSpec:
				for _, id := range s.Names {
					into[id.Name] = fset.Position(id.Pos())
				}
			}
		}
	}
}

// ResolvePath resolves symlinks best-effort, falling back to the input when
// the path cannot be resolved (e.g., it does not exist yet). Comparing
// resolved paths keeps an aliased destination (such as /tmp vs /private/tmp
// on macOS) from bypassing a same-directory check.
func ResolvePath(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}

	return resolved
}
