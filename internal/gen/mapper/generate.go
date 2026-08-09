package mapper

import (
	"bytes"
	"errors"
	"fmt"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strings"

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
	outDir, outFile := splitOutput(cfg.Output, env.GoFile)

	ld, err := loadAll(cfg, env, scope, outDir)
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

	names := make(map[string]string, len(ld.byPath))
	for path, pkg := range ld.byPath {
		names[path] = pkg.Name
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
		return checkUpToDate(outPath, code)
	}
	return writeOutput(outPath, code)
}

// defaultOutputFile names the generated file when $GOFILE is unset, which is
// what happens outside go generate. Under go generate the name derives from the
// file carrying the directive instead.
const defaultOutputFile = "mapper_gen.go"

// splitOutput interprets -output: a path ending in .go names the file
// directly; otherwise it is a directory and the file name derives from
// $GOFILE.
func splitOutput(output, goFile string) (dir, file string) {
	if strings.HasSuffix(output, ".go") {
		return filepath.Dir(output), filepath.Base(output)
	}
	name := defaultOutputFile
	if goFile != "" {
		name = strings.TrimSuffix(goFile, ".go") + "_gen.go"
	}
	return output, name
}

// loaded indexes the packages of the single bulk packages.Load call.
type loaded struct {
	fset   *token.FileSet
	byPath map[string]*packages.Package
	byDir  map[string]*packages.Package
}

func loadAll(cfg Config, env Env, scope importScope, outDir string) (*loaded, error) {
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
	patterns = append(patterns, scope.importPaths()...)
	patterns = append(patterns, cfg.ConverterPkgs...)
	for _, pair := range cfg.Pairs {
		for _, ref := range []TypeRef{pair.Src, pair.Dst} {
			if ref.IsImportPath() {
				patterns = append(patterns, ref.Pkg)
			}
		}
	}
	for _, ig := range cfg.Ignores {
		if ig.Type.IsImportPath() {
			patterns = append(patterns, ig.Type.Pkg)
		}
	}
	slices.Sort(patterns)
	patterns = slices.Compact(patterns)

	// Reading mapper.Register calls means reading what an expression refers to,
	// which the loader only keeps when asked.
	res, err := packages.Load(patterns, packages.Config{Dir: env.Dir, TypesInfo: true})
	if err != nil {
		return nil, fmt.Errorf("load packages: %w", err)
	}
	pkgs := res.Packages
	var errs []error
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		for _, e := range p.Errors {
			errs = append(errs, errors.New(e.Error()))
		}
	})
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

// byPattern finds the loaded package for a -converter-pkg argument,
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

func checkUpToDate(path string, code []byte) error {
	existing, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("%s is out of date: %w (run go generate)", path, err)
	}
	if !bytes.Equal(existing, code) {
		return fmt.Errorf("%s is out of date (run go generate)", path)
	}
	return nil
}

func writeOutput(path string, code []byte) error {
	existing, err := os.ReadFile(filepath.Clean(path))
	switch {
	case err == nil:
		if bytes.Equal(existing, code) {
			return nil
		}
		if !bytes.HasPrefix(existing, []byte("// Code generated ")) {
			return fmt.Errorf("refusing to overwrite %s: it lacks a generated-code header", path)
		}
	case !errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("read existing output: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := os.WriteFile(path, code, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
