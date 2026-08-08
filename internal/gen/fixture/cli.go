package fixture

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/parser"
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
	"github.com/go-kanna/kanna/internal/packages"
	"github.com/go-kanna/kanna/internal/scan"
)

// defaultOutputFile is the name given to the generated file inside the
// destination directory.
const defaultOutputFile = "fixture_gen.go"

// Config holds the options for a single generation run.
//
// Each field corresponds to exactly one flag. Keeping that correspondence is
// what lets the same run be described either by flags or by a kanna.yaml entry
// without one of them growing a setting the other cannot express.
type Config struct {
	Source      string
	Destination string
	Package     string
	Excludes    []string
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
	for _, raw := range args {
		switch raw {
		case "--version":
			fmt.Fprintln(c.Out, c.Version)
			return exit.OK
		case "-h", "--help", "help":
			printUsage(c.Out)
			return exit.OK
		}
	}

	cfg, err := parseFlags(args, c.Err)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exit.OK
		}
		return exit.Usage
	}

	return c.generate(cfg)
}

func (c CLI) generate(cfg Config) int {
	dest := c.resolve(cfg.Destination)

	absDest, err := filepath.Abs(dest)
	if err != nil {
		fmt.Fprintln(c.Err, "resolve destination:", err)
		return exit.Error
	}

	// Validate what can be checked before paying for the package load.
	pkgName := packageName(cfg.Package, absDest)
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
		return exit.Error
	}
	pkg := pkgs[0]

	structs, ds := scan.Structs(res.Packages)
	c.printDiags(ds)
	if diag.HasErrors(ds) {
		return exit.Error
	}

	// Generating into the source package would make the file import its own
	// package and break compilation. Compare with symlinks resolved so an
	// aliased path (e.g., /tmp vs /private/tmp on macOS) cannot bypass the check.
	if pkg.Dir != "" && resolvePath(absDest) == resolvePath(pkg.Dir) {
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

	out, err := Emit(EmitParams{
		PackageName: pkgName,
		SourceName:  pkg.Name,
		SourcePath:  pkg.PkgPath,
		Imports:     imports,
		Plans:       plans,
	})
	if err != nil {
		fmt.Fprintln(c.Err, err)
		return exit.Error
	}

	// Generated Go source should be readable like the rest of the package,
	// matching what gofmt/go generate produce by default.
	//nolint:gosec // generated source directories are meant to be world-readable
	if err := os.MkdirAll(dest, 0o755); err != nil {
		fmt.Fprintln(c.Err, "create destination:", err)
		return exit.Error
	}

	path := filepath.Join(dest, defaultOutputFile)

	//nolint:gosec // generated source is meant to be world-readable
	if err := os.WriteFile(path, out, 0o644); err != nil {
		fmt.Fprintf(c.Err, "write %s: %v\n", path, err)
		return exit.Error
	}

	fmt.Fprintln(c.Out, path)

	if missing := missingRequires(out, imports, absDest); len(missing) > 0 {
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

// resolve interprets a path against Dir. An absolute path is already anchored,
// and an empty Dir means the process working directory, so both pass through.
func (c CLI) resolve(path string) string {
	if c.Dir == "" || filepath.IsAbs(path) {
		return path
	}

	return filepath.Join(c.Dir, path)
}

func (c CLI) printDiags(ds []diag.Diag) {
	if len(ds) == 0 {
		return
	}
	fmt.Fprintln(c.Err, diag.Format(ds))
}

// packageName resolves the package name of the generated file. destDir must
// be absolute, so that a relative -destination such as "." still yields a
// directory name.
func packageName(override, destDir string) string {
	if override != "" {
		return override
	}

	if name := declaredPackage(destDir); name != "" {
		return name
	}

	return filepath.Base(destDir)
}

// declaredPackage returns the package the Go files in dir already declare;
// every file in a directory has to agree on it, so the generated file has no
// choice either. The generated file itself is skipped so a name written by a
// previous run cannot pin the next one, and test files are skipped because
// they may sit in an external _test package. An unreadable directory yields an
// empty name.
func declaredPackage(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	fset := token.NewFileSet()

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == defaultOutputFile {
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

// missingRequires returns the third-party imports of the generated code that
// the destination module's go.mod does not require yet, sorted. It is
// best-effort: an unknown module layout just suppresses the note.
func missingRequires(out []byte, imports []string, destDir string) []string {
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

	required := make(map[string]bool, len(f.Require))
	for _, r := range f.Require {
		required[r.Mod.Path] = true
	}

	candidates := append(slices.Clone(imports), gofakeitImport)
	slices.Sort(candidates)

	var missing []string

	for _, path := range candidates {
		// gofakeit is a candidate whether or not the output uses it, so match
		// against the import line the generator actually wrote.
		if required[path] || !bytes.Contains(out, []byte(strconv.Quote(path))) {
			continue
		}

		missing = append(missing, path)
	}

	return missing
}

// resolvePath resolves symlinks best-effort, falling back to the input when
// the path cannot be resolved (e.g., it does not exist yet).
func resolvePath(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}

	return resolved
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

func parseFlags(args []string, stderr io.Writer) (Config, error) {
	fs := flag.NewFlagSet("kanna-fixture", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		printUsage(stderr)
	}

	var (
		cfg     Config
		exclude string
	)

	fs.StringVar(&cfg.Source, "source", "", "source package to scan (relative path or import path)")
	fs.StringVar(&cfg.Destination, "destination", "", "output directory for the generated file")
	fs.StringVar(&cfg.Package, "package", "", "generated package name (defaults to the destination directory name)")
	fs.StringVar(&exclude, "exclude", "", "comma-separated type names to exclude (e.g., -exclude Foo,Bar)")

	if err := fs.Parse(args); err != nil {
		return Config{}, fmt.Errorf("parse flags: %w", err)
	}

	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected argument %q\n", fs.Arg(0))
		fs.Usage()

		return Config{}, errors.New("unexpected argument")
	}

	if cfg.Source == "" || cfg.Destination == "" {
		fmt.Fprintln(stderr, "-source and -destination are required")
		fs.Usage()

		return Config{}, errors.New("missing required flags")
	}

	cfg.Excludes = splitExcludes(exclude)

	return cfg, nil
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
	fmt.Fprintln(w, "  --version            print version")
	fmt.Fprintln(w, "  -h, --help           show this help")
}
