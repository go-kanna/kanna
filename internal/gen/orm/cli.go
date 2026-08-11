package orm

import (
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-kanna/kanna/internal/diag"
	"github.com/go-kanna/kanna/internal/exit"
	"github.com/go-kanna/kanna/internal/output"
	"github.com/go-kanna/kanna/internal/packages"
	"github.com/go-kanna/kanna/internal/scan"
)

// defaultOutputFile is the name given to the generated file inside the
// destination directory.
const defaultOutputFile = "orm_gen.go"

// Config holds the options for a single generation run. Each field corresponds
// to exactly one flag, so a run is described entirely by what was typed.
type Config struct {
	Source      string
	Destination string
	Package     string
	Check       bool
}

// CLI is the command-line entry point for the orm generator. Out and Err
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
// package exit: Error when generation fails, Usage when the invocation itself
// is wrong.
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
	pkgName, err := packageName(cfg.Package, absDest)
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

	// One source package at a time: the generated file qualifies every type
	// with a single package name, so a pattern matching several has no valid
	// output.
	pkgs := scan.DedupePackages(res.Packages)
	if len(pkgs) != 1 {
		fmt.Fprintf(c.Err, "-source must match exactly one package, %s matched %d\n", cfg.Source, len(pkgs))
		return exit.Usage
	}
	pkg := pkgs[0]

	if err := importable(pkg); err != nil {
		fmt.Fprintln(c.Err, err)
		return exit.Usage
	}

	// Generating into the source package would make the file import its own
	// package and break compilation — and would hand the model package the
	// same stale-output chicken-and-egg problem di has. Compare with symlinks
	// resolved so an aliased path cannot bypass the check.
	if pkg.Dir != "" && resolvePath(absDest) == resolvePath(pkg.Dir) {
		fmt.Fprintln(c.Err, "-destination must be a different package from -source")
		return exit.Usage
	}

	structs, ds := scan.Structs(res.Packages)
	c.printDiags(ds)
	if diag.HasErrors(ds) {
		return exit.Error
	}

	tables, tds := Tables(structs)
	c.printDiags(tds)
	if diag.HasErrors(tds) {
		return exit.Error
	}
	if len(tables) == 0 {
		fmt.Fprintf(c.Err, "no //kanna:table structs in %s\n", pkg.PkgPath)
		return exit.Error
	}

	if ds := clashes(declaredNames(absDest, pkgName), tables); len(ds) > 0 {
		c.printDiags(ds)
		return exit.Error
	}

	out, err := Emit(EmitParams{
		PackageName: pkgName,
		SourceName:  pkg.Name,
		SourcePath:  pkg.PkgPath,
		Tables:      tables,
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

	return exit.OK
}

// importable reports why the generated code could not import pkg, or nil when
// it can.
func importable(pkg *packages.Package) error {
	// go/packages gives file patterns this synthetic path, which no other
	// package can import.
	if pkg.PkgPath == "command-line-arguments" {
		return errors.New("-source must name a package, not individual files")
	}

	if pkg.Name == "main" {
		return fmt.Errorf("-source names package main (%s), which the generated code cannot import", pkg.PkgPath)
	}

	return nil
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
func packageName(override, destDir string) (string, error) {
	declared := declaredPackage(destDir)

	// Every file in a directory has to agree on the package clause, so the
	// override cannot win over what is already there.
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

// declaredPackage returns the package the Go files in dir already declare. The
// generated file itself is skipped so a name written by a previous run cannot
// pin the next one, and test files are skipped because they may sit in an
// external _test package.
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

// declaredNames returns the top-level identifiers the destination package
// already declares, each mapped to the position it is declared at. The
// generated file is skipped, since it is what gets replaced.
func declaredNames(dir, pkgName string) map[string]token.Position {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	fset := token.NewFileSet()
	names := make(map[string]token.Position)

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == defaultOutputFile || !strings.HasSuffix(name, ".go") {
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

// clashes reports every identifier the generated file would redeclare.
func clashes(declared map[string]token.Position, tables []Table) []diag.Diag {
	if len(declared) == 0 {
		return nil
	}

	var diags []diag.Diag
	for _, name := range emittedNames(tables) {
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

// emittedNames lists the top-level identifiers Emit declares for tables.
func emittedNames(tables []Table) []string {
	var out []string
	for _, t := range tables {
		factory := factoryName(t.Name)
		out = append(out, factory,
			unexportedName(factory+"Columns"),
			unexportedName("scan"+t.Name),
			unexportedName(t.Name+"ColumnValuePairs"))
		if isIntType(t.PK().GoType) {
			out = append(out, unexportedName("set"+t.Name+"PK"))
		}
		created, updated := timestampFields(t)
		if len(created) > 0 {
			out = append(out, unexportedName("set"+t.Name+"CreatedAt"))
		}
		if len(updated) > 0 {
			out = append(out, unexportedName("set"+t.Name+"UpdatedAt"))
		}
		for _, r := range t.Relations {
			out = append(out, unexportedName("preload"+t.Name+r.FieldName))
		}
	}
	return out
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

// parseFlags reads args into a Config. The middle return value reports whether
// --version was asked for, which suppresses the checks on the other flags.
func parseFlags(args []string, stderr io.Writer) (Config, bool, error) {
	fs := flag.NewFlagSet("kanna-orm", flag.ContinueOnError)
	fs.SetOutput(stderr)

	// The caller prints the usage, because only it knows whether this is a
	// request for help, which belongs on stdout, or a mistake, which does not.
	fs.Usage = func() {}

	var (
		cfg         Config
		showVersion bool
	)

	fs.StringVar(&cfg.Source, "source", "", "source package to scan (relative path or import path)")
	fs.StringVar(&cfg.Destination, "destination", "", "output directory for the generated file")
	fs.StringVar(&cfg.Package, "package", "", "generated package name (defaults to what the destination declares)")
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

	return cfg, false, nil
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "kanna-orm — generate type-safe query code from annotated model structs")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  kanna-orm -source <pkg> -destination <dir> [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  -source <pkg>        source package to scan (relative path or import path)")
	fmt.Fprintln(w, "  -destination <dir>   output directory for the generated file")
	fmt.Fprintln(w, "  -package <name>      generated package name (defaults to what the destination declares)")
	fmt.Fprintln(w, "  -check               verify the output is up to date instead of writing it")
	fmt.Fprintln(w, "  --version            print version")
	fmt.Fprintln(w, "  -h, --help           show this help")
}
