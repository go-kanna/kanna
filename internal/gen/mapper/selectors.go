package mapper

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Env carries the environment provided by go generate.
type Env struct {
	// GoFile is the basename of the file containing the go:generate
	// directive; empty when the generator runs outside go generate.
	GoFile string
	// GoPackage is the name of the package containing the directive;
	// empty when unknown.
	GoPackage string
	// Dir is the directory the generator runs in.
	Dir string
}

type importDecl struct {
	alias string // explicit local name; empty for unnamed and blank imports
	path  string
}

// importScope holds the import declarations visible to a go:generate
// directive. Imports of $GOFILE take priority over those of the other
// files in the package.
type importScope struct {
	gofile []importDecl
	others []importDecl
}

// collectImports parses the non-test Go files in env.Dir and collects
// their import declarations. Files belonging to another package are
// skipped; the expected package comes from env.GoPackage, or from the
// package clause of $GOFILE when GOPACKAGE is not set.
func collectImports(env Env) (importScope, error) {
	entries, err := os.ReadDir(env.Dir)
	if err != nil {
		return importScope{}, fmt.Errorf("read package directory: %w", err)
	}
	fset := token.NewFileSet()

	wantPkg := env.GoPackage
	if wantPkg == "" && env.GoFile != "" {
		file, err := parser.ParseFile(fset, filepath.Join(env.Dir, env.GoFile), nil, parser.PackageClauseOnly)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return importScope{}, fmt.Errorf("$GOFILE %q not found in %s", env.GoFile, env.Dir)
			}
			return importScope{}, fmt.Errorf("parse %s: %w", env.GoFile, err)
		}
		wantPkg = file.Name.Name
	}
	if wantPkg == "" {
		wantPkg, err = singlePackageName(fset, env.Dir, entries)
		if err != nil {
			return importScope{}, err
		}
	}

	var scope importScope
	foundGoFile := false
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		isGoFile := env.GoFile != "" && name == env.GoFile
		if strings.HasSuffix(name, "_test.go") && !isGoFile {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(env.Dir, name), nil, parser.ImportsOnly)
		if err != nil {
			return importScope{}, fmt.Errorf("parse %s: %w", name, err)
		}
		if !isGoFile && wantPkg != "" && file.Name.Name != wantPkg {
			continue
		}
		if isGoFile {
			foundGoFile = true
			scope.gofile = fileImportDecls(file)
		} else {
			scope.others = append(scope.others, fileImportDecls(file)...)
		}
	}
	if env.GoFile != "" && !foundGoFile {
		return importScope{}, fmt.Errorf("$GOFILE %q not found in %s", env.GoFile, env.Dir)
	}
	return scope, nil
}

// singlePackageName determines which package to scan when the
// environment names none. Mixing imports from unrelated packages would
// make selector resolution unreliable, so multiple packages in the
// directory are an error.
func singlePackageName(fset *token.FileSet, dir string, entries []os.DirEntry) (string, error) {
	names := make(map[string]bool)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.PackageClauseOnly)
		if err != nil {
			return "", fmt.Errorf("parse %s: %w", name, err)
		}
		names[file.Name.Name] = true
	}
	if len(names) <= 1 {
		for name := range names {
			return name, nil
		}
		return "", nil
	}
	sorted := slices.Sorted(maps.Keys(names))
	return "", fmt.Errorf(
		"%s contains multiple packages (%s): set $GOPACKAGE or run kanna-mapper via go generate",
		dir, strings.Join(sorted, ", "),
	)
}

func fileImportDecls(file *ast.File) []importDecl {
	var decls []importDecl
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		alias := ""
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		switch alias {
		case ".":
			continue // dot imports provide no selector
		case "_":
			alias = "" // blank imports are matched by package name
		}
		decls = append(decls, importDecl{alias: alias, path: path})
	}
	return decls
}

// importPaths returns every import path in scope, deduplicated and
// sorted. All of them are loaded in bulk: unnamed imports need their
// package names for selector matching, and any of them may hold the
// types named in -types.
func (s importScope) importPaths() []string {
	var paths []string
	for _, decls := range [][]importDecl{s.gofile, s.others} {
		for _, d := range decls {
			paths = append(paths, d.path)
		}
	}
	slices.Sort(paths)
	return slices.Compact(paths)
}

// resolveSelector resolves a package selector to an import path. pkgNames
// maps import paths of unnamed imports to their actual package names.
func (s importScope) resolveSelector(sel string, pkgNames map[string]string) (string, error) {
	for _, decls := range [][]importDecl{s.gofile, s.others} {
		var matches []string
		for _, d := range decls {
			name := d.alias
			if name == "" {
				name = pkgNames[d.path]
			}
			if name == sel && !slices.Contains(matches, d.path) {
				matches = append(matches, d.path)
			}
		}
		switch len(matches) {
		case 0:
		case 1:
			return matches[0], nil
		default:
			return "", fmt.Errorf("package selector %q is ambiguous: it matches %s", sel, strings.Join(matches, " and "))
		}
	}
	return "", fmt.Errorf(
		"cannot resolve package selector %q: import the package in this package's files or use a full import path in -types",
		sel,
	)
}
