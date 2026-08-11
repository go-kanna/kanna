package i18n

import (
	"errors"
	"flag"
	"fmt"
	"go/token"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/text/language"

	"github.com/go-kanna/kanna/internal/diag"
	"github.com/go-kanna/kanna/internal/exit"
	"github.com/go-kanna/kanna/internal/output"
)

// defaultOutputFile is the name given to the generated file inside the
// destination directory.
const defaultOutputFile = "i18n_gen.go"

// Config holds the options for a single generation run. Each field corresponds
// to exactly one flag, so a run is described entirely by what was typed.
type Config struct {
	Locales     string
	Default     string
	Destination string
	Package     string
	Check       bool
}

// CLI is the command-line entry point for the message generator. Out and Err
// default to os.Stdout/os.Stderr when constructed via NewCLI.
type CLI struct {
	Out     io.Writer
	Err     io.Writer
	Version string

	// Dir is the directory the locale directory and the output path resolve
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
// package exit: Error when generation fails (unreadable locales, validation
// failures, write errors), Usage when the invocation itself is wrong.
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

	tag, err := language.Parse(cfg.Default)
	if err != nil {
		fmt.Fprintf(c.Err, "invalid -default %q: %v\n", cfg.Default, err)
		return exit.Usage
	}

	return c.generate(cfg, tag)
}

func (c CLI) generate(cfg Config, tag language.Tag) int {
	model, ds := Analyze(output.Resolve(c.Dir, cfg.Locales), tag)
	c.printDiags(ds)
	if diag.HasErrors(ds) {
		return exit.Error
	}

	pkg, err := output.PackageName(cfg.Package, c.resolveAbs(cfg.Destination), defaultOutputFile)
	if err != nil {
		fmt.Fprintln(c.Err, err)
		return exit.Usage
	}
	if !token.IsIdentifier(pkg) {
		fmt.Fprintf(c.Err, "invalid package name %q; use -package to override\n", pkg)
		return exit.Usage
	}

	src, err := Render(model, pkg)
	if err != nil {
		fmt.Fprintln(c.Err, err)
		return exit.Error
	}

	out := filepath.Join(output.Resolve(c.Dir, cfg.Destination), defaultOutputFile)
	if cfg.Check {
		if err := output.CheckUpToDate(out, src); err != nil {
			fmt.Fprintln(c.Err, err)
			return exit.Error
		}
		return exit.OK
	}

	if err := output.Write(out, src); err != nil {
		fmt.Fprintln(c.Err, err)
		return exit.Error
	}
	fmt.Fprintln(c.Out, out)
	return exit.OK
}

// resolveAbs resolves against Dir and then Abs, for deriving the package name
// from a relative -destination such as "." whose base would otherwise name
// nothing.
func (c CLI) resolveAbs(path string) string {
	resolved := output.Resolve(c.Dir, path)
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return resolved
	}
	return abs
}

// parseFlags reads args into a Config. The middle return value reports whether
// --version was asked for, which suppresses the checks on the other flags.
func parseFlags(args []string, stderr io.Writer) (Config, bool, error) {
	fs := flag.NewFlagSet("kanna-i18n", flag.ContinueOnError)
	fs.SetOutput(stderr)

	// The caller prints the usage, because only it knows whether this is a
	// request for help, which belongs on stdout, or a mistake, which does not.
	fs.Usage = func() {}

	var (
		cfg         Config
		showVersion bool
	)

	fs.StringVar(&cfg.Locales, "locales", "locales", "directory containing locale files")
	fs.StringVar(&cfg.Default, "default", "en", "default language defining the generated signatures")
	fs.StringVar(&cfg.Destination, "destination", "messages", "output directory for the generated file")
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
		return Config{}, true, nil
	}

	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected argument %q\n", fs.Arg(0))
		printUsage(stderr)
		return Config{}, false, errors.New("unexpected argument")
	}

	return cfg, false, nil
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "kanna-i18n — generate typed message constructors with the translations compiled in")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  kanna-i18n [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  -locales <dir>    directory containing locale files (default: locales)")
	fmt.Fprintln(w, "  -default <lang>   default language defining the generated signatures (default: en)")
	fmt.Fprintln(w, "  -destination <dir>  output directory for the generated file (default: messages)")
	fmt.Fprintln(w, "  -package <name>     package name of the generated file (defaults to what the destination declares)")
	fmt.Fprintln(w, "  -check            verify the output is up to date instead of writing it")
	fmt.Fprintln(w, "  --version         print version")
	fmt.Fprintln(w, "  -h, --help        show this help")
}

func (c CLI) printDiags(ds []diag.Diag) {
	if len(ds) == 0 {
		return
	}
	fmt.Fprintln(c.Err, diag.Format(ds))
}
