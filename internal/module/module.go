// Package module reads a Go module from its files, as they are written by hand.
//
// A generator's load needs packages that compile, which they do not before
// generators have run, and the go command cannot be asked about a package
// whose output is gone. What decides the order of generators has to come out
// the same either way, so this reads no more than each file says of itself:
// nothing is type-checked, and what kanna generators wrote is left out.
package module

import (
	"cmp"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/go-kanna/internal/output"
	"golang.org/x/mod/modfile"

	"github.com/go-kanna/kanna/internal/generate"
)

// Module is a Go module, as its files have it.
type Module struct {
	// Dir is the directory of its go.mod.
	Dir string

	// Path is its module path.
	Path string

	// Packages holds its packages by import path: every directory with a Go
	// file, whichever the build leaves out.
	Packages map[string]*Package

	// Directives holds the //go:generate lines go generate would see, sorted by
	// directory, file, and line.
	Directives []generate.Directive

	// beneath holds the modules it requires whose paths are under its own,
	// whose packages are not its packages, for all that their paths look so.
	beneath []string
}

// Package is a directory of Go files.
type Package struct {
	// Path is its import path.
	Path string

	// Dir is its directory.
	Dir string

	// Name is what its package clause says, or the name of Dir when no file
	// tells.
	Name string

	// Imports and TestImports hold what its files import, sorted, leaving out
	// what kanna generators wrote. An import a generator writes is one of its
	// output: what the run reads tells more, and does not come and go with the
	// output.
	Imports, TestImports []string

	// Generated holds the files kanna generators wrote, by absolute path, each
	// with its package clause.
	Generated map[string]string

	// Foreign names what wrote every file of the package, when none of that is
	// kanna's, as for a package buf generates, or holds "something" for a
	// marker that does not say.
	Foreign []string
}

// Find returns the module holding dir: the nearest go.mod at dir or above.
// Tags are the additional build tags of the go command running its generators.
func Find(dir string, tags ...string) (*Module, error) {
	for d := dir; ; d = filepath.Dir(d) {
		data, err := os.ReadFile(filepath.Join(d, "go.mod")) //nolint:gosec // the go.mod the go command would use
		switch {
		case err == nil:
			return load(d, data, tags)
		case !errors.Is(err, fs.ErrNotExist):
			return nil, err //nolint:wrapcheck // a *fs.PathError, which names the file
		case filepath.Dir(d) == d:
			return nil, fmt.Errorf("no go.mod in %s or above it", dir)
		}
	}
}

func load(dir string, gomod []byte, tags []string) (*Module, error) {
	name := filepath.Join(dir, "go.mod")
	f, err := modfile.ParseLax(name, gomod, nil)
	if err != nil {
		return nil, fmt.Errorf("read the module: %w", err)
	}
	if f.Module == nil {
		return nil, fmt.Errorf("%s declares no module", name)
	}

	m := &Module{Dir: dir, Path: f.Module.Mod.Path, Packages: make(map[string]*Package)}
	for _, r := range f.Require {
		if strings.HasPrefix(r.Mod.Path, m.Path+"/") {
			m.beneath = append(m.beneath, r.Mod.Path)
		}
	}
	r := newReader(tags)
	err = Walk(dir, func(file string) error { return r.read(m, file) })
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", m.Path, err)
	}

	for _, p := range m.Packages {
		slices.Sort(p.Imports)
		p.Imports = slices.Compact(p.Imports)
		slices.Sort(p.TestImports)
		p.TestImports = slices.Compact(p.TestImports)
		if f := r.foreign[p.Path]; f != nil && !f.handWritten {
			p.Foreign = slices.Sorted(maps.Keys(f.generators))
		}
		if p.Name == "" {
			p.Name = filepath.Base(p.Dir)
		}
	}
	slices.SortFunc(m.Directives, func(a, b generate.Directive) int {
		if c := strings.Compare(filepath.Dir(a.File), filepath.Dir(b.File)); c != 0 {
			return c
		}
		if c := strings.Compare(a.File, b.File); c != 0 {
			return c
		}
		return a.Line - b.Line
	})
	return m, nil
}

// Walk calls fn with every Go file of the module at dir, as ./... sees them: it
// leaves out testdata, vendor, what starts with a dot or an underscore, and any
// module nested in it.
func Walk(dir string, fn func(file string) error) error {
	return filepath.WalkDir(dir, func(file string, d fs.DirEntry, err error) error { //nolint:wrapcheck // Load wraps it
		switch {
		case err != nil:
			return err
		case d.IsDir():
			if file != dir && skip(file, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		case !strings.HasSuffix(d.Name(), ".go") || !d.Type().IsRegular():
			return nil
		}
		return fn(file)
	})
}

func skip(dir, name string) bool {
	if name == "testdata" || name == "vendor" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		return true
	}
	_, err := os.Stat(filepath.Join(dir, "go.mod"))
	return err == nil
}

// generatedBy matches the marker of a generated file, for the generator it names.
var generatedBy = regexp.MustCompile(`(?m)^// Code generated by (\S+?)\.?\s.*DO NOT EDIT\.\r?$`)

// reader reads the files of a module.
type reader struct {
	// build and generate tell the files the build has from those go generate
	// looks at, which has the tag "generate" too.
	build, generate build.Context

	foreign map[string]*foreign
}

// foreign is what the files of a package say of what wrote them.
type foreign struct {
	generators  map[string]bool
	handWritten bool
}

func newReader(tags []string) *reader {
	r := &reader{build: build.Default, generate: build.Default, foreign: make(map[string]*foreign)}
	r.build.BuildTags = append(slices.Clone(r.build.BuildTags), tags...)
	r.generate.BuildTags = append(slices.Clone(r.build.BuildTags), "generate")
	return r
}

// read adds the file to the package of its directory in m.
func (r *reader) read(m *Module, file string) error {
	dir, name := filepath.Split(file)
	dir = filepath.Clean(dir)
	src, err := os.ReadFile(file) //nolint:gosec // a file of the module
	if err != nil {
		return err //nolint:wrapcheck // a *fs.PathError, which names the file
	}
	p := m.packageAt(dir)

	inBuild, err := r.build.MatchFile(dir, name)
	if err != nil {
		return nil //nolint:nilerr // the build reports a file it cannot read
	}
	inGenerate, err := r.generate.MatchFile(dir, name)
	if err != nil {
		return nil //nolint:nilerr // the build reports a file it cannot read
	}

	// A file that does not parse past its package clause still has what it
	// said up to there. The build reports the rest.
	f, _ := parser.ParseFile(token.NewFileSet(), file, src, parser.ImportsOnly)
	pkgName := ""
	if f != nil && f.Name != nil {
		pkgName = f.Name.Name
	}

	generator, generated := generatorOf(src)
	if generated && isKanna(generator) {
		if pkgName == "" {
			pkgName = p.Name
		}
		if p.Name == "" && !strings.HasSuffix(name, "_test.go") {
			p.Name = pkgName
		}
		p.Generated[file] = pkgName
		return nil
	}

	if inGenerate {
		m.Directives = append(m.Directives, generate.Scan(file, src, pkgName)...)
	}
	if !inBuild && !inGenerate {
		return nil
	}
	if !strings.HasSuffix(name, "_test.go") {
		if pkgName != "" && (p.Name == "" || inBuild) {
			p.Name = pkgName
		}
		r.tell(p.Path, generator, generated)
	}
	if f == nil || !inBuild {
		return nil
	}
	addImports(p, f, name)
	return nil
}

// addImports adds what f, a file called name, imports to p.
func addImports(p *Package, f *ast.File, name string) {
	for _, spec := range f.Imports {
		imported, err := strconv.Unquote(spec.Path.Value)
		switch {
		case err != nil:
		case strings.HasSuffix(name, "_test.go"):
			p.TestImports = append(p.TestImports, imported)
		default:
			p.Imports = append(p.Imports, imported)
		}
	}
}

// tell records that a file of the package at path was written by generator, or
// by hand when generated is false.
func (r *reader) tell(path, generator string, generated bool) {
	f := r.foreign[path]
	if f == nil {
		f = &foreign{generators: make(map[string]bool)}
		r.foreign[path] = f
	}
	if generated {
		f.generators[cmp.Or(generator, "something")] = true
	} else {
		f.handWritten = true
	}
}

// generatorOf returns the generator src's marker names, and whether src carries
// one.
func generatorOf(src []byte) (string, bool) {
	if !output.IsGenerated(src) {
		return "", false
	}
	m := generatedBy.FindSubmatch(src)
	if m == nil {
		return "", true
	}
	return string(m[1]), true
}

// isKanna reports whether generator is one of the kanna generators, or the
// placeholder of the internal/packages module.
func isKanna(generator string) bool {
	return generator == "kanna" || strings.HasPrefix(generator, "kanna-")
}

// Contains reports whether dir, by absolute path, is in m.
func (m *Module) Contains(dir string) bool {
	rel, err := filepath.Rel(m.Dir, dir)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// PackageOf returns the import path of the package a file at path is in, or
// is to be in. It returns false for a file outside m.
func (m *Module) PackageOf(file string) (string, bool) {
	dir := filepath.Dir(file)
	if !m.Contains(dir) {
		return "", false
	}
	return m.importPath(dir), true
}

// DirOf returns the directory of the package at importPath in m, whether it
// exists or not. It returns false for a path outside m, as one that a module it
// requires holds is, whatever its path looks like.
func (m *Module) DirOf(importPath string) (string, bool) {
	if importPath == m.Path {
		return m.Dir, true
	}
	rel, ok := strings.CutPrefix(importPath, m.Path+"/")
	if !ok || slices.ContainsFunc(m.beneath, func(mod string) bool {
		return importPath == mod || strings.HasPrefix(importPath, mod+"/")
	}) {
		return "", false
	}
	return filepath.Join(m.Dir, filepath.FromSlash(rel)), true
}

// Rel names path relative to the module, with forward slashes, for showing it.
func (m *Module) Rel(file string) string {
	rel, err := filepath.Rel(m.Dir, file)
	if err != nil {
		return file
	}
	return filepath.ToSlash(rel)
}

// Handwritten returns what a load is to read in place of the files kanna
// generators wrote, for the packages to read as they do before any has run.
func (m *Module) Handwritten() map[string][]byte {
	overlay := make(map[string][]byte)
	for _, p := range m.Packages {
		for file, name := range p.Generated {
			overlay[file] = fmt.Appendf(nil, "// Code generated by kanna. DO NOT EDIT.\n\npackage %s\n", name)
		}
	}
	return overlay
}

// packageAt returns the package of dir, adding it to m when it is the first
// file of it.
func (m *Module) packageAt(dir string) *Package {
	importPath := m.importPath(dir)
	if p := m.Packages[importPath]; p != nil {
		return p
	}
	p := &Package{Path: importPath, Dir: dir, Generated: make(map[string]string)}
	m.Packages[importPath] = p
	return p
}

// importPath returns the import path of dir, which has to be in m.
func (m *Module) importPath(dir string) string {
	rel, err := filepath.Rel(m.Dir, dir)
	if err != nil || rel == "." {
		return m.Path
	}
	return path.Join(m.Path, filepath.ToSlash(rel))
}
