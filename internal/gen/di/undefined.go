package di

import (
	"go/ast"
	"slices"
	"strconv"
	"strings"

	"github.com/go-kanna/kanna/internal/diag"
	"github.com/go-kanna/kanna/internal/packages"
	"github.com/go-kanna/kanna/internal/scan"
)

// dropUndefinedConstructors removes the type errors this run resolves on its own:
// a call to a constructor it is about to generate, from a package whose generated
// file does not exist yet. A test is the usual case — the test that calls newEnv
// sits in the same package as the env container, so there is no way to keep the
// caller out of the scan — and a first run over a package that calls its own
// constructor is the same situation. Every other error stays.
func dropUndefinedConstructors(ds []diag.Diag, containers []Container, pkgs []*packages.Package) []diag.Diag {
	if len(containers) == 0 {
		return ds
	}

	own := make(map[string]bool, 2*len(containers))
	for _, c := range containers {
		name := constructorNameFor(c)
		own[c.PkgPath+"."+name] = true
		own[c.PkgPath+".Must"+name] = true
	}

	byFile := make(map[string]*packages.Package)
	for _, pkg := range scan.DedupePackages(pkgs) {
		for _, f := range pkg.CompiledGoFiles {
			byFile[f] = pkg
		}
	}

	return slices.DeleteFunc(slices.Clone(ds), func(d diag.Diag) bool {
		expr, ok := strings.CutPrefix(d.Message, "undefined: ")
		if !ok {
			return false
		}
		pkg, ok := byFile[d.Pos.Filename]
		if !ok {
			return false
		}
		return own[referencedName(pkg, d.Pos.Filename, expr)]
	})
}

// referencedName turns the expression of an "undefined: <expr>" error into a
// "<import path>.<name>" key. A bare name belongs to the package of the file; a
// qualified one to whichever package the file imports under that qualifier.
func referencedName(pkg *packages.Package, filename, expr string) string {
	qualifier, name, qualified := strings.Cut(expr, ".")
	if !qualified {
		return pkg.PkgPath + "." + expr
	}
	if pkg.Fset == nil {
		return ""
	}

	for _, file := range pkg.Syntax {
		if pkg.Fset.Position(file.Pos()).Filename != filename {
			continue
		}
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			if importedAs(pkg, spec, path) == qualifier {
				return path + "." + name
			}
		}
	}
	return ""
}

// importedAs returns the name an import spec puts in scope: its alias when it
// has one, otherwise the imported package's own name.
func importedAs(pkg *packages.Package, spec *ast.ImportSpec, path string) string {
	if spec.Name != nil {
		return spec.Name.Name
	}
	if imp := pkg.Imports[path]; imp != nil {
		return imp.Name
	}
	return ""
}
