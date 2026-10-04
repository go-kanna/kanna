package di

import (
	"errors"
	"flag"
	"fmt"
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

// defaultOutputFile is the name given to the generated file in each package.
// Containers declared in _test.go files go to testOutputFile instead, which
// keeps their constructors out of the package proper and lets an external test
// package hold containers of its own.
const (
	defaultOutputFile = "di_gen.go"
	testOutputFile    = "di_gen_test.go"
)

// CLI is the command-line entry point for the DI generator. Out and Err default
// to os.Stdout/os.Stderr when constructed via NewCLI.
type CLI struct {
	Out     io.Writer
	Err     io.Writer
	Version string

	// Dir is the directory package patterns are resolved against. Empty means
	// the process working directory, which is what `go generate` sets to the
	// directory of the file carrying the directive.
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
// package exit: Error when generation fails (missing providers, ambiguity, write
// errors), Usage when the invocation itself is wrong (bad flags, no patterns).
func (c CLI) Run(args []string) int {
	// Only a leading "help" is the subcommand. Scanning every argument for it
	// would let a flag value named "help" turn the run into a silent no-op.
	if len(args) > 0 && args[0] == "help" {
		c.printUsage(c.Out)
		return exit.OK
	}

	fs := flag.NewFlagSet("kanna-di", flag.ContinueOnError)
	fs.SetOutput(c.Err)

	// The usage below is printed by this function, which knows whether it is
	// answering -h (stdout) or reporting a mistake (stderr). Left to itself the
	// flag package would print its own on every parse error, so help would come
	// out twice and split across both streams.
	fs.Usage = func() {}

	var (
		verbose     bool
		tagsRaw     string
		mustFlag    bool
		checkFlag   bool
		showVersion bool
	)
	fs.BoolVar(&verbose, "v", false, "verbose output")
	fs.BoolVar(&verbose, "verbose", false, "verbose output")
	fs.StringVar(&tagsRaw, "tags", "", "comma-separated build tags")
	fs.BoolVar(&mustFlag, "must", false, "generate MustNew* constructors that panic on error")
	fs.BoolVar(&checkFlag, "check", false, "verify generated files are up to date instead of writing them")
	fs.BoolVar(&showVersion, "version", false, "print version")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			c.printUsage(c.Out)
			return exit.OK
		}
		c.printUsage(c.Err)
		return exit.Usage
	}

	if showVersion {
		fmt.Fprintln(c.Out, c.Version)
		return exit.OK
	}

	patterns := fs.Args()
	if len(patterns) == 0 {
		c.printUsage(c.Err)
		return exit.Usage
	}

	if verbose {
		fmt.Fprintf(c.Out, "output: %s, %s\n", defaultOutputFile, testOutputFile)
		if tagsRaw != "" {
			fmt.Fprintln(c.Out, "tags:", tagsRaw)
		}
		if mustFlag {
			fmt.Fprintln(c.Out, "must: true")
		}
	}

	res, dropped, err := load(patterns, packages.Config{
		Dir:       c.Dir,
		BuildTags: splitTags(tagsRaw),
		// Test files take part: a container declared in one gets its constructor
		// in testOutputFile, and providers declared there serve such containers.
		Tests: true,
	})
	if err != nil {
		fmt.Fprintln(c.Err, err)
		return exit.Error
	}
	if verbose {
		fmt.Fprintln(c.Out, "packages:", len(res.Packages))
	}

	structs, dsS := scan.Structs(res.Packages)

	// Containers come before the type-error gate: the errors this run is about
	// to fix are calls to the constructors it generates, and only the containers
	// say which those are.
	containers, dsC := Containers(res.Fset, structs)
	dsS = dropUndefinedConstructors(dsS, containers, res.Packages)
	c.printDiags(dsS)
	if diag.HasErrors(dsS) {
		for _, path := range dropped {
			fmt.Fprintf(c.Err, "note: %s is stale and was set aside; it is regenerated once the errors above are fixed\n",
				path)
		}
		return exit.Error
	}

	c.printDiags(dsC)
	if diag.HasErrors(dsC) {
		return exit.Error
	}
	if len(containers) == 0 {
		fmt.Fprintln(c.Err, "no container found")
		return exit.Error
	}

	providers, dsP := Providers(res.Packages)
	c.printDiags(dsP)
	if diag.HasErrors(dsP) {
		return exit.Error
	}

	if verbose {
		fmt.Fprintln(c.Out, "containers:", len(containers))
		fmt.Fprintln(c.Out, "providers:", len(providers))
	}

	plans, ds := BuildAll(containers, providers, Options{Must: mustFlag})
	c.printDiags(ds)
	failed := diag.HasErrors(ds)

	// Render every package before touching the disk. A run that fails partway
	// through should leave the tree exactly as it found it, rather than updating
	// the packages it got to first and leaving the rest stale.
	var pending []pendingFile
	claimed := map[string]string{} // output path → the package name written there
	for _, group := range groupByPackage(plans) {
		out, err := Emit(group.pkgName, group.plans)
		if err != nil {
			fmt.Fprintln(c.Err, err)
			failed = true
			continue
		}

		path := filepath.Join(filepath.Dir(group.plans[0].Container.Pos.Filename), group.outputFile())
		// A directory's in-package and external test packages would both write
		// testOutputFile, and one file declares one package.
		if other, taken := claimed[path]; taken {
			fmt.Fprintf(c.Err, "%s: both %s and %s declare test containers; "+
				"keep a directory's test containers in one package\n", path, other, group.pkgName)
			failed = true
			continue
		}
		claimed[path] = group.pkgName
		pending = append(pending, pendingFile{path: path, data: out})
	}

	if failed {
		return exit.Error
	}

	if checkFlag {
		// Every stale package is reported, not just the first: a CI failure
		// should name everything a single regeneration would fix.
		code := exit.OK
		for _, f := range pending {
			if err := output.CheckUpToDate(f.path, f.data); err != nil {
				fmt.Fprintln(c.Err, err)
				code = exit.Error
			}
		}
		return code
	}

	for _, f := range pending {
		if err := output.Write(f.path, f.data); err != nil {
			fmt.Fprintln(c.Err, err)
			return exit.Error
		}

		if verbose {
			fmt.Fprintln(c.Out, "generate:", f.path)
		} else {
			fmt.Fprintln(c.Out, f.path)
		}
	}

	return exit.OK
}

// pendingFile is a rendered package waiting to be written once every package has
// rendered successfully.
type pendingFile struct {
	path string
	data []byte
}

// planGroup is the set of plans whose containers live in a single package and
// will be emitted into one .go file together. Containers declared in test files
// form a group of their own, since they go to a different file.
type planGroup struct {
	pkgName string
	test    bool
	plans   []Plan
}

func (g planGroup) outputFile() string {
	if g.test {
		return testOutputFile
	}
	return defaultOutputFile
}

// groupByPackage groups plans by their container's package and by whether the
// container is declared in a test file, preserving the order in which groups are
// first seen.
func groupByPackage(plans []Plan) []planGroup {
	type key struct {
		pkgPath string
		test    bool
	}
	idxOf := map[key]int{}
	var out []planGroup

	for _, pl := range plans {
		c := pl.Container
		k := key{pkgPath: c.PkgPath, test: c.Test}
		i, ok := idxOf[k]
		if !ok {
			idxOf[k] = len(out)
			out = append(out, planGroup{pkgName: c.PkgName, test: c.Test, plans: []Plan{pl}})
			continue
		}
		out[i].plans = append(out[i].plans, pl)
	}
	return out
}

func (c CLI) printDiags(ds []diag.Diag) {
	if len(ds) == 0 {
		return
	}
	fmt.Fprintln(c.Err, diag.Format(ds))
}

func (c CLI) printUsage(w io.Writer) {
	fmt.Fprintln(w, "kanna-di — generate type-safe dependency-injection constructors from container structs")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  kanna-di [flags] <packages>...")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  --tags <list>      comma-separated build tags")
	fmt.Fprintln(w, "  --must             generate MustNew* constructors that panic on error")
	fmt.Fprintln(w, "  -check             verify generated files are up to date instead of writing them")
	fmt.Fprintln(w, "  -v, --verbose      verbose output")
	fmt.Fprintln(w, "  --version          print version")
	fmt.Fprintln(w, "  -h, --help         show this help")
}

func splitTags(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}

	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
