package mapper

import (
	"errors"
	"fmt"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-kanna/kanna/internal/output"
	"github.com/go-kanna/kanna/internal/packages"
)

// generate executes one invocation: it loads the involved packages in
// one pass, resolves the requested pairs, and writes (or checks) the
// generated file.
func generate(cfg Config, env Env) error {
	scope, err := collectImports(env)
	if err != nil {
		return err
	}
	outDir, outFile := cfg.Output, defaultOutputFile

	// Resolving a selector needs each imported package's name, not its types.
	names, err := packages.LoadNames(scope.importPaths(), packages.Config{Dir: env.Dir})
	if err != nil {
		return fmt.Errorf("resolve package names: %w", err)
	}

	patterns, err := loadPatterns(cfg, env, scope, names, outDir)
	if err != nil {
		return err
	}

	ld, err := loadAll(patterns, env.Dir)
	if err != nil {
		return err
	}
	outAbs := outDir
	if !filepath.IsAbs(outAbs) {
		outAbs = filepath.Join(env.Dir, outAbs)
	}
	outPkg := ld.byDir[filepath.Clean(outAbs)]
	pkgName, outPkgPath, err := outputIdentity(cfg, env, outDir, outPkg)
	if err != nil {
		return err
	}

	pairs := make([]pairSpec, 0, len(cfg.Pairs))
	for _, pair := range cfg.Pairs {
		src, err := resolveTypeRef(pair.Src, ld, scope, names, outPkg)
		if err != nil {
			return err
		}
		dst, err := resolveTypeRef(pair.Dst, ld, scope, names, outPkg)
		if err != nil {
			return err
		}
		pairs = append(pairs, pairSpec{Src: src, Dst: dst})
	}

	ignores := make(map[fieldKey]bool, len(cfg.Ignores))
	for _, ig := range cfg.Ignores {
		pkgPath, err := resolvePkgPath(ig.Type, scope, names, outPkgPath)
		if err != nil {
			return err
		}
		ignores[fieldKey{PkgPath: pkgPath, Type: ig.Type.Name, Field: ig.Field}] = true
	}

	converterPkgs := make([]*packages.Package, 0, len(cfg.ConverterPkgs))
	seenConverters := make(map[string]bool, len(cfg.ConverterPkgs))
	for _, pattern := range cfg.ConverterPkgs {
		pkg, err := ld.byPattern(pattern, env.Dir)
		if err != nil {
			return err
		}
		// The same package may be named twice (e.g., once as a directory
		// and once as an import path); scanning it twice would report
		// every converter as a duplicate registration.
		if seenConverters[pkg.PkgPath] {
			continue
		}
		seenConverters[pkg.PkgPath] = true
		converterPkgs = append(converterPkgs, pkg)
	}
	table, err := extractConverters(converterPkgs, outPkgPath)
	if err != nil {
		return err
	}

	plans, err := resolvePlans(resolveConfig{
		Fset:      ld.fset,
		Pairs:     pairs,
		Conv:      table,
		Ignores:   ignores,
		Direction: cfg.Direction,
	})
	if err != nil {
		return err
	}

	code, err := emitFile(pkgName, outPkgPath, plans)
	if err != nil {
		return err
	}
	outPath := filepath.Join(outAbs, outFile)
	if cfg.Check {
		//nolint:wrapcheck // the message already names the file and the remedy
		return output.CheckUpToDate(outPath, code)
	}
	//nolint:wrapcheck // the message already names the file and the remedy
	return output.Write(outPath, code)
}

// defaultOutputFile is the name given to the generated file inside the
// destination directory.
const defaultOutputFile = "mapper_gen.go"

// loaded indexes the packages of the single bulk packages.Load call.
type loaded struct {
	fset   *token.FileSet
	byPath map[string]*packages.Package
	byDir  map[string]*packages.Package
}

// loadPatterns lists the packages the run refers to, which is what gets loaded
// in full.
//
// Only the packages actually named are included. Every import of every file in
// the directory would also resolve every selector, but it would type-check a
// great deal of code the output never mentions.
func loadPatterns(cfg Config, env Env, scope importScope, names map[string]string, outDir string) ([]string, error) {
	patterns := []string{"."}
	if outDir != "." {
		// A missing or empty output directory is fine: the file lands in a
		// fresh package, so there is nothing to load from it.
		abs := outDir
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(env.Dir, outDir)
		}
		if dirHasGoFiles(abs) {
			patterns = append(patterns, dirPattern(outDir))
		}
	}
	patterns = append(patterns, cfg.ConverterPkgs...)

	refs := make([]TypeRef, 0, len(cfg.Pairs)*2+len(cfg.Ignores))
	for _, pair := range cfg.Pairs {
		refs = append(refs, pair.Src, pair.Dst)
	}
	for _, ig := range cfg.Ignores {
		refs = append(refs, ig.Type)
	}

	for _, ref := range refs {
		switch {
		case ref.Pkg == "":
			// A type in the output package, which is already covered above.
		case ref.IsImportPath():
			patterns = append(patterns, ref.Pkg)
		default:
			path, err := scope.resolveSelector(ref.Pkg, names)
			if err != nil {
				return nil, err
			}
			patterns = append(patterns, path)
		}
	}

	slices.Sort(patterns)

	return slices.Compact(patterns), nil
}

func loadAll(patterns []string, dir string) (*loaded, error) {
	// Reading mapper.Register calls means reading what an expression refers to,
	// which the loader only keeps when asked.
	res, err := packages.Load(patterns, packages.Config{Dir: dir, TypesInfo: true})
	if err != nil {
		return nil, fmt.Errorf("load packages: %w", err)
	}
	pkgs := res.Packages

	// Load already folds a dependency's failure into the package that imports
	// it, so the roots carry everything worth reporting.
	var errs []error
	for _, p := range pkgs {
		for _, e := range p.Errors {
			errs = append(errs, errors.New(e.Error()))
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	if len(pkgs) == 0 {
		return nil, errors.New("no packages loaded")
	}

	ld := &loaded{
		fset:   res.Fset,
		byPath: make(map[string]*packages.Package, len(pkgs)),
		byDir:  make(map[string]*packages.Package, len(pkgs)),
	}
	for _, pkg := range pkgs {
		ld.byPath[pkg.PkgPath] = pkg
		if len(pkg.GoFiles) > 0 {
			ld.byDir[filepath.Dir(pkg.GoFiles[0])] = pkg
		}
	}
	return ld, nil
}

func dirHasGoFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			return true
		}
	}
	return false
}

func dirPattern(dir string) string {
	if filepath.IsAbs(dir) || strings.HasPrefix(dir, ".") {
		return dir
	}
	return "./" + dir
}

// byPattern finds the loaded package for a -converters argument,
// which is a directory path (starting with "." or absolute) or an import
// path.
func (l *loaded) byPattern(pattern, baseDir string) (*packages.Package, error) {
	if strings.HasPrefix(pattern, ".") || filepath.IsAbs(pattern) {
		abs := pattern
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(baseDir, pattern)
		}
		if pkg, ok := l.byDir[filepath.Clean(abs)]; ok {
			return pkg, nil
		}
		return nil, fmt.Errorf("no package found in %s", pattern)
	}
	if pkg, ok := l.byPath[pattern]; ok {
		return pkg, nil
	}
	return nil, fmt.Errorf("package %s not loaded", pattern)
}

func outputIdentity(cfg Config, env Env, outDir string, outPkg *packages.Package) (name, path string, err error) {
	if outPkg != nil {
		path = outPkg.PkgPath
	}

	// Every file in a directory has to agree on the package clause, so -package
	// cannot override what is already there: the result would be a file that
	// nothing can compile alongside its neighbours.
	if cfg.Package != "" && outPkg != nil && cfg.Package != outPkg.Name {
		return "", "", fmt.Errorf("-package %s conflicts with package %s, which %s already declares",
			cfg.Package, outPkg.Name, outDir)
	}

	name = cfg.Package
	if name == "" && outPkg != nil {
		name = outPkg.Name
	}
	if name == "" && outDir == "." {
		name = env.GoPackage
	}
	if name == "" {
		return "", "", errors.New("cannot determine the output package name; pass -package")
	}
	return name, path, nil
}

func resolveTypeRef(
	ref TypeRef, ld *loaded, scope importScope, names map[string]string, outPkg *packages.Package,
) (types.Type, error) {
	var pkg *packages.Package
	switch {
	case ref.Pkg == "":
		if outPkg == nil {
			return nil, fmt.Errorf("type %s: no package in the output directory to resolve it against", ref.Name)
		}
		pkg = outPkg
	case ref.IsImportPath():
		pkg = ld.byPath[ref.Pkg]
		if pkg == nil {
			return nil, fmt.Errorf("package %s not loaded", ref.Pkg)
		}
	default:
		path, err := scope.resolveSelector(ref.Pkg, names)
		if err != nil {
			return nil, err
		}
		pkg = ld.byPath[path]
		if pkg == nil {
			return nil, fmt.Errorf("package %s (selector %s) not loaded", path, ref.Pkg)
		}
	}
	obj := pkg.Types.Scope().Lookup(ref.Name)
	if obj == nil {
		return nil, fmt.Errorf("type %s not found in %s", ref.Name, pkg.PkgPath)
	}
	tn, ok := obj.(*types.TypeName)
	if !ok {
		return nil, fmt.Errorf("%s.%s is not a type", pkg.PkgPath, ref.Name)
	}
	t := tn.Type()
	if ref.Pointer {
		t = types.NewPointer(t)
	}
	return t, nil
}

func resolvePkgPath(ref TypeRef, scope importScope, names map[string]string, outPkgPath string) (string, error) {
	switch {
	case ref.Pkg == "":
		return outPkgPath, nil
	case ref.IsImportPath():
		return ref.Pkg, nil
	default:
		return scope.resolveSelector(ref.Pkg, names)
	}
}
