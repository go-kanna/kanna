package fixture

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"

	"github.com/go-kanna/kanna/internal/diag"
	"github.com/go-kanna/kanna/internal/exit"
	"github.com/go-kanna/kanna/internal/output"
	"github.com/go-kanna/kanna/internal/packages"
	"github.com/go-kanna/kanna/internal/scan"
)

// defaultOutputFile is the name given to the generated file inside the
// destination directory.
const defaultOutputFile = "fixture_gen.go"

// Config holds the options for a single generation run. Each field corresponds
// to exactly one flag, so a run is described entirely by what was typed.
type Config struct {
	Source      string
	Destination string
	Package     string
	Excludes    []string
	Check       bool
}

// CLI is the command-line entry point for the fixture generator. Out and Err
// default to os.Stdout/os.Stderr when constructed via NewCLI.
type CLI struct {
	Out     io.Writer
	Err     io.Writer
	Version string

	// Dir is the directory the source pattern and the destination are resolved
	// against. Empty means the process working directory, which is what
	// `go generate` sets to the directory of the file carrying the directive.
	Dir string
}

// NewCLI constructs a CLI with default writers and the given version string.
func NewCLI(version string) CLI {
	return CLI{
		Out:     os.Stdout,
		Err:     os.Stderr,
		Version: version,
	}
}

// Run parses args (excluding the program name) and returns one of the codes in
// package exit: Error when generation fails (load errors, nothing left to
// generate, write errors), Usage when the invocation itself is wrong (bad flags,
// a destination naming the source package).
func (c CLI) Run(args []string) int {
	// Only a leading "help" is the subcommand. Scanning every argument for it
	// would let a flag value named "help" turn the run into a silent no-op.
	if len(args) > 0 && args[0] == "help" {
		printUsage(c.Out)
		return exit.OK
	}

	cfg, showVersion, err := parseFlags(args, c.Err)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(c.Out)
			return exit.OK
		}
		return exit.Usage
	}

	if showVersion {
		fmt.Fprintln(c.Out, c.Version)
		return exit.OK
	}

	return c.generate(cfg)
}

func (c CLI) generate(cfg Config) int {
	dest := output.Resolve(c.Dir, cfg.Destination)

	absDest, err := filepath.Abs(dest)
	if err != nil {
		fmt.Fprintln(c.Err, "resolve destination:", err)
		return exit.Error
	}

	// Validate what can be checked before paying for the package load.
	pkgName, err := output.PackageName(cfg.Package, absDest, defaultOutputFile)
	if err != nil {
		fmt.Fprintln(c.Err, err)
		return exit.Usage
	}
	if !token.IsIdentifier(pkgName) {
		fmt.Fprintf(c.Err, "invalid package name %q; use -package to override\n", pkgName)
		return exit.Usage
	}

	res, err := packages.Load([]string{cfg.Source}, packages.Config{Dir: c.Dir})
	if err != nil {
		fmt.Fprintln(c.Err, err)
		return exit.Error
	}

	// One source package at a time: the generated file qualifies every type with
	// a single package name, so a pattern matching several has no valid output.
	pkgs := scan.DedupePackages(res.Packages)
	if len(pkgs) != 1 {
		fmt.Fprintf(c.Err, "-source must match exactly one package, %s matched %d\n", cfg.Source, len(pkgs))
		return exit.Usage
	}
	pkg := pkgs[0]

	if err := packages.Importable(pkg); err != nil {
		fmt.Fprintln(c.Err, err)
		return exit.Usage
	}

	structs, ds := scan.Structs(res.Packages)
	c.printDiags(ds)
	if diag.HasErrors(ds) {
		return exit.Error
	}

	// Generating into the source package would make the file import its own
	// package and break compilation. Compare with symlinks resolved so an
	// aliased path (e.g., /tmp vs /private/tmp on macOS) cannot bypass the check.
	if pkg.Dir != "" && output.ResolvePath(absDest) == output.ResolvePath(pkg.Dir) {
		fmt.Fprintln(c.Err, "-destination must be a different package from -source")
		return exit.Usage
	}

	targets, tds := Targets(structs)
	c.printDiags(tds)
	if diag.HasErrors(tds) {
		return exit.Error
	}

	kept := c.applyExcludes(targets, cfg.Excludes, pkg.PkgPath)
	if len(kept) == 0 {
		fmt.Fprintf(c.Err, "no fixture targets in %s\n", pkg.PkgPath)
		return exit.Error
	}

	plans, imports := Plans(kept, pkg.PkgPath, pkg.Name)

	graphs, graphImports, gds := GraphPlans(structs, kept, pkg.PkgPath, pkg.Name)
	c.printDiags(gds)
	if diag.HasErrors(gds) {
		return exit.Error
	}
	imports = slices.Compact(slices.Sorted(slices.Values(append(imports, graphImports...))))

	// The destination is meant to hold hand-written files too, so what is
	// already there decides whether this output can compile.
	if ds := clashes(output.DeclaredNames(absDest, pkgName, defaultOutputFile), plans, graphs); len(ds) > 0 {
		c.printDiags(ds)
		return exit.Error
	}

	out, err := Emit(EmitParams{
		PackageName: pkgName,
		SourceName:  pkg.Name,
		SourcePath:  pkg.PkgPath,
		Imports:     imports,
		Plans:       plans,
		Graphs:      graphs,
	})
	if err != nil {
		fmt.Fprintln(c.Err, err)
		return exit.Error
	}

	path := filepath.Join(dest, defaultOutputFile)

	if cfg.Check {
		if err := output.CheckUpToDate(path, out); err != nil {
			fmt.Fprintln(c.Err, err)
			return exit.Error
		}
		return exit.OK
	}

	if err := output.Write(path, out); err != nil {
		fmt.Fprintln(c.Err, err)
		return exit.Error
	}

	fmt.Fprintln(c.Out, path)

	if missing := missingRequires(out, imports, pkg.PkgPath, absDest); len(missing) > 0 {
		fmt.Fprintf(c.Err, "note: generated code imports %s; run 'go mod tidy'\n", strings.Join(missing, ", "))
	}

	return exit.OK
}

// applyExcludes drops the named targets, warning about any name that matched
// nothing. A typo would otherwise silently leave the fixture in place, which is
// the opposite of what the author asked for.
func (c CLI) applyExcludes(targets []Target, excludes []string, pkgPath string) []Target {
	if len(excludes) == 0 {
		return targets
	}

	known := make(map[string]bool, len(targets))
	for _, tg := range targets {
		known[tg.Name] = true
	}

	var (
		excluded = make(map[string]bool, len(excludes))
		diags    []diag.Diag
	)

	for _, name := range excludes {
		if !known[name] {
			diags = append(diags, diag.Warningf(token.Position{},
				"-exclude %s matches no struct in %s", name, pkgPath))
		}
		excluded[name] = true
	}

	c.printDiags(diags)

	return slices.DeleteFunc(slices.Clone(targets), func(tg Target) bool {
		return excluded[tg.Name]
	})
}

func (c CLI) printDiags(ds []diag.Diag) {
	if len(ds) == 0 {
		return
	}
	fmt.Fprintln(c.Err, diag.Format(ds))
}

// clashes reports every identifier the generated file would redeclare.
//
// Writing the file anyway would leave a package that no longer compiles, and the
// error would name the generated file rather than the collision, so this is
// refused up front.
func clashes(declared map[string]token.Position, plans []Plan, graphs []GraphPlan) []diag.Diag {
	if len(declared) == 0 {
		return nil
	}

	emitted := make([]string, 0, len(plans)+2*len(graphs)+2)
	for _, p := range plans {
		emitted = append(emitted, p.Name)
	}
	for _, gp := range graphs {
		emitted = append(emitted, gp.Name, gp.Ctor)
	}
	if needsCounter(graphs) {
		emitted = append(emitted, "nextPK")
	}
	if needsHelper(plans) || graphNeedsHelper(graphs) {
		emitted = append(emitted, "mustGenerate")
	}

	var diags []diag.Diag
	for _, name := range emitted {
		pos, ok := declared[name]
		if !ok {
			continue
		}

		diags = append(diags, diag.Errorf(pos,
			"the generated file would redeclare %s", name).
			WithHints("rename this declaration, or generate into a directory of its own"))
	}

	return diags
}

// missingRequires returns the imports of the generated code that the destination
// module's go.mod does not require yet, sorted. It is best-effort: an unknown
// module layout just suppresses the note.
//
// sourcePath is a candidate like any other. When the destination sits in a
// different module from the source — the case where the note is worth anything —
// it is the require most likely to be missing.
func missingRequires(out []byte, imports []string, sourcePath, destDir string) []string {
	gomod := findGoMod(destDir)
	if gomod == "" {
		return nil
	}

	//nolint:gosec // the go.mod path is discovered next to the destination
	data, err := os.ReadFile(gomod)
	if err != nil {
		return nil
	}

	f, err := modfile.Parse(gomod, data, nil)
	if err != nil {
		return nil
	}

	// The destination's own module counts as available, so a source package that
	// lives alongside the fixtures needs no require at all.
	mods := make([]string, 0, len(f.Require)+1)
	if f.Module != nil {
		mods = append(mods, f.Module.Mod.Path)
	}
	for _, r := range f.Require {
		mods = append(mods, r.Mod.Path)
	}

	candidates := append(slices.Clone(imports), sourcePath)
	slices.Sort(candidates)
	candidates = slices.Compact(candidates)

	var missing []string

	for _, path := range candidates {
		// The standard library never appears in go.mod.
		if !strings.Contains(strings.SplitN(path, "/", 2)[0], ".") {
			continue
		}
		// Match against the import line actually written, so a candidate the
		// output ended up not needing is never reported.
		if provided(mods, path) || !bytes.Contains(out, []byte(strconv.Quote(path))) {
			continue
		}

		missing = append(missing, path)
	}

	return missing
}

// provided reports whether one of mods supplies importPath.
//
// A module path is a prefix of every package it provides, which is as close as
// this gets without resolving the module graph. Erring toward "provided" is the
// right direction: a note that fails to appear is a smaller cost than one
// telling the reader to require a module they already have.
func provided(mods []string, importPath string) bool {
	for _, mod := range mods {
		if importPath == mod || strings.HasPrefix(importPath, mod+"/") {
			return true
		}
	}

	return false
}

// findGoMod walks up from dir to locate the enclosing go.mod file.
func findGoMod(dir string) string {
	// Normalize so the parent walk reaches the filesystem root even for
	// relative inputs.
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}

	for {
		path := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(path); err == nil {
			return path
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}

		dir = parent
	}
}

// parseFlags reads args into a Config. The middle return value reports whether
// --version was asked for, which suppresses the checks on the other flags.
func parseFlags(args []string, stderr io.Writer) (Config, bool, error) {
	fs := flag.NewFlagSet("kanna-fixture", flag.ContinueOnError)
	fs.SetOutput(stderr)

	// The caller prints the usage, because only it knows whether this is a
	// request for help, which belongs on stdout, or a mistake, which does not.
	fs.Usage = func() {}

	var (
		cfg         Config
		exclude     string
		showVersion bool
	)

	fs.StringVar(&cfg.Source, "source", "", "source package to scan (relative path or import path)")
	fs.StringVar(&cfg.Destination, "destination", "", "output directory for the generated file")
	fs.StringVar(&cfg.Package, "package", "", "generated package name (defaults to what the destination declares)")
	fs.StringVar(&exclude, "exclude", "", "comma-separated type names to exclude (e.g., -exclude Foo,Bar)")
	fs.BoolVar(&cfg.Check, "check", false, "verify the output is up to date instead of writing it")
	fs.BoolVar(&showVersion, "version", false, "print version")

	if err := fs.Parse(args); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			printUsage(stderr)
		}

		return Config{}, false, fmt.Errorf("parse flags: %w", err)
	}

	if showVersion {
		return cfg, true, nil
	}

	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected argument %q\n", fs.Arg(0))
		printUsage(stderr)

		return Config{}, false, errors.New("unexpected argument")
	}

	if cfg.Source == "" || cfg.Destination == "" {
		fmt.Fprintln(stderr, "-source and -destination are required")
		printUsage(stderr)

		return Config{}, false, errors.New("missing required flags")
	}

	cfg.Excludes = splitExcludes(exclude)

	return cfg, false, nil
}

func splitExcludes(s string) []string {
	if s == "" {
		return nil
	}

	var names []string

	for name := range strings.SplitSeq(s, ",") {
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}

	return names
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "kanna-fixture — generate plain fixture functions from Go structs")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  kanna-fixture -source <pkg> -destination <dir> [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  -source <pkg>        source package to scan (relative path or import path)")
	fmt.Fprintln(w, "  -destination <dir>   output directory for the generated file")
	fmt.Fprintln(w, "  -package <name>      generated package name (defaults to what the destination declares)")
	fmt.Fprintln(w, "  -exclude <names>     comma-separated type names to exclude (e.g., -exclude Foo,Bar)")
	fmt.Fprintln(w, "  -check               verify the output is up to date instead of writing it")
	fmt.Fprintln(w, "  --version            print version")
	fmt.Fprintln(w, "  -h, --help           show this help")
}
