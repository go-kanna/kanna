// Package packages wraps golang.org/x/tools/go/packages with the loading mode
// and build-tag handling used by kanna. The upstream Package type is
// re-exported as an alias so callers do not need to import the upstream
// package directly.
package packages

import (
	"errors"
	"fmt"
	"go/token"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Package is an alias for golang.org/x/tools/go/packages.Package, re-exported
// so callers can refer to it through this package alone.
type Package = packages.Package

// Error is an alias for golang.org/x/tools/go/packages.Error, re-exported so
// callers can inspect load failures without importing the upstream package.
type Error = packages.Error

// Config controls how packages are loaded.
type Config struct {
	// Dir is the directory the patterns are resolved against. Empty means the
	// current working directory of the process.
	Dir string
	// BuildTags is the list of build tags to pass via the -tags flag.
	BuildTags []string
	// Tests indicates whether test files are included in the load.
	Tests bool

	// TypesInfo requests the per-expression type-checking results.
	//
	// A generator that reads declarations does not need them; one that reads
	// what an expression refers to does. Resolving the callee of a call, or the
	// type arguments a generic call was instantiated with, is only possible
	// through this. It is opt-in because retaining the maps costs memory that a
	// scan over declarations has no use for.
	TypesInfo bool
}

// Result contains the packages loaded for the requested patterns.
type Result struct {
	Packages []*Package

	// Fset is the file set the packages were parsed with; every package in
	// Packages shares it. Callers need it to resolve a token.Pos into a position
	// or to evaluate an expression in a package's scope.
	Fset *token.FileSet
}

// Load loads Go packages matching the given patterns (e.g. "./...").
func Load(patterns []string, cfg Config) (*Result, error) {
	if len(patterns) == 0 {
		return nil, errors.New("packages: no package patterns")
	}

	// NeedDeps is deliberately absent: it would parse and type-check every
	// transitive dependency from source, while the types kanna needs are already
	// resolved from export data without it. Dropping it more than halves the
	// load time.
	mode := packages.NeedName |
		packages.NeedFiles |
		packages.NeedCompiledGoFiles |
		packages.NeedImports |
		packages.NeedTypes |
		packages.NeedSyntax

	if cfg.TypesInfo {
		mode |= packages.NeedTypesInfo
	}

	fset := token.NewFileSet()
	pc := &packages.Config{
		Mode:  mode,
		Tests: cfg.Tests,
		Dir:   cfg.Dir,
		Fset:  fset,
	}

	if tags := joinTags(cfg.BuildTags); tags != "" {
		pc.BuildFlags = []string{"-tags=" + tags}
	}

	pkgs, err := packages.Load(pc, patterns...)
	if err != nil {
		return nil, fmt.Errorf("packages: Load: %w", err)
	}

	return &Result{Packages: pkgs, Fset: fset}, nil
}

// LoadNames maps each pattern's import path to its package name, without
// type-checking anything.
//
// A generator that has to turn a package selector such as "model" into an import
// path needs the names and nothing more. Loading those packages in full would
// type-check code the output never refers to, which for a directory's worth of
// imports means most of the standard library.
func LoadNames(patterns []string, cfg Config) (map[string]string, error) {
	if len(patterns) == 0 {
		return map[string]string{}, nil
	}

	pc := &packages.Config{
		Mode: packages.NeedName,
		Dir:  cfg.Dir,
	}
	if tags := joinTags(cfg.BuildTags); tags != "" {
		pc.BuildFlags = []string{"-tags=" + tags}
	}

	pkgs, err := packages.Load(pc, patterns...)
	if err != nil {
		return nil, fmt.Errorf("packages: Load: %w", err)
	}

	// A package that failed to load has no name, and a caller resolving
	// selectors would report that as "cannot resolve" — blaming the selector for
	// a problem in the package it names. Report the real failure instead.
	var errs []error
	names := make(map[string]string, len(pkgs))
	for _, p := range pkgs {
		for _, e := range p.Errors {
			errs = append(errs, errors.New(e.Error()))
		}
		if p.PkgPath != "" && p.Name != "" {
			names[p.PkgPath] = p.Name
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	return names, nil
}

// joinTags concatenates tag values with a single space, dropping empties.
// go list expects space-separated build tags as a single -tags argument.
func joinTags(tags []string) string {
	nonEmpty := make([]string, 0, len(tags))
	for _, t := range tags {
		if t = strings.TrimSpace(t); t != "" {
			nonEmpty = append(nonEmpty, t)
		}
	}
	return strings.Join(nonEmpty, " ")
}
