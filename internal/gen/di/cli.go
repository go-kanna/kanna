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
	"github.com/go-kanna/kanna/internal/packages"
	"github.com/go-kanna/kanna/internal/scan"
)

// defaultOutputFile is the name given to the generated file in each package.
const defaultOutputFile = "di_gen.go"

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

	var (
		verbose     bool
		tagsRaw     string
		mustFlag    bool
		outputFile  string
		showVersion bool
	)
	fs.BoolVar(&verbose, "v", false, "verbose output")
	fs.BoolVar(&verbose, "verbose", false, "verbose output")
	fs.StringVar(&tagsRaw, "tags", "", "comma-separated build tags")
	fs.BoolVar(&mustFlag, "must", false, "generate MustNew* constructors that panic on error")
	fs.StringVar(&outputFile, "o", defaultOutputFile, "output file name (per package)")
	fs.BoolVar(&showVersion, "version", false, "print version")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			c.printUsage(c.Out)
			return exit.OK
		}
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
		fmt.Fprintln(c.Out, "output:", outputFile)
		if tagsRaw != "" {
			fmt.Fprintln(c.Out, "tags:", tagsRaw)
		}
		if mustFlag {
			fmt.Fprintln(c.Out, "must: true")
		}
	}

	res, err := packages.Load(patterns, packages.Config{
		Dir:       c.Dir,
		BuildTags: splitTags(tagsRaw),
	})
	if err != nil {
		fmt.Fprintln(c.Err, err)
		return exit.Error
	}
	if verbose {
		fmt.Fprintln(c.Out, "packages:", len(res.Packages))
	}

	structs, dsS := scan.Structs(res.Packages)
	c.printDiags(dsS)
	if diag.HasErrors(dsS) {
		return exit.Error
	}

	containers, dsC := Containers(res.Fset, structs)
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

	idx := NewIndex(providers)
	opts := Options{Must: mustFlag}

	// Render every package before touching the disk. A run that fails partway
	// through should leave the tree exactly as it found it, rather than updating
	// the packages it got to first and leaving the rest stale.
	var (
		pending []pendingFile
		failed  bool
	)

	for _, group := range groupByPackage(containers) {
		var plans []Plan
		for _, container := range group.containers {
			pl, ds := Build(container, idx, opts)
			c.printDiags(ds)
			if diag.HasErrors(ds) {
				failed = true
				continue
			}
			plans = append(plans, pl)
		}
		if len(plans) == 0 {
			continue
		}

		out, err := Emit(group.pkgName, plans)
		if err != nil {
			fmt.Fprintln(c.Err, err)
			failed = true
			continue
		}

		outDir := filepath.Dir(group.containers[0].Pos.Filename)
		pending = append(pending, pendingFile{
			path: filepath.Join(outDir, outputFile),
			data: out,
		})
	}

	if failed {
		return exit.Error
	}

	for _, f := range pending {
		// Generated Go source should be readable like the rest of the package,
		// matching what gofmt/go generate produce by default.
		//nolint:gosec // generated source is meant to be world-readable
		if err := os.WriteFile(f.path, f.data, 0o644); err != nil {
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

// containerGroup is the set of containers that live in a single package and will
// be emitted into one .go file together.
type containerGroup struct {
	pkgPath    string
	pkgName    string
	containers []Container
}

// groupByPackage groups containers by their declaring package, preserving the
// order in which packages are first seen.
func groupByPackage(cs []Container) []containerGroup {
	idxOf := map[string]int{}
	var out []containerGroup

	for _, c := range cs {
		i, ok := idxOf[c.PkgPath]
		if !ok {
			idxOf[c.PkgPath] = len(out)
			out = append(out, containerGroup{
				pkgPath:    c.PkgPath,
				pkgName:    c.PkgName,
				containers: []Container{c},
			})
			continue
		}
		out[i].containers = append(out[i].containers, c)
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
	fmt.Fprintln(w, "kanna-di — type-safe dependency-injection code generator for Go")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  kanna-di [flags] <packages>...")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  -o <file>          output file name per package (default: "+defaultOutputFile+")")
	fmt.Fprintln(w, "  --tags <list>      comma-separated build tags")
	fmt.Fprintln(w, "  --must             generate MustNew* constructors that panic on error")
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
