package i18n

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/text/language"

	"github.com/go-kanna/kanna/internal/exit"
)

// Config holds the options for a single generation run. Each field corresponds
// to exactly one flag, so a run is described entirely by what was typed.
type Config struct {
	Locales string
	Default string
	Out     string
	Package string
	Check   bool
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
	model, warnings, err := Analyze(c.resolve(cfg.Locales), tag)
	if err != nil {
		fmt.Fprintln(c.Err, err)
		return exit.Error
	}
	for _, w := range warnings {
		fmt.Fprintln(c.Err, "warning:", w)
	}

	pkg := cfg.Package
	if pkg == "" {
		pkg = filepath.Base(filepath.Dir(c.resolveAbs(cfg.Out)))
	}

	src, err := Render(model, pkg)
	if err != nil {
		fmt.Fprintln(c.Err, err)
		return exit.Error
	}

	out := c.resolve(cfg.Out)
	if cfg.Check {
		if err := checkUpToDate(out, src); err != nil {
			fmt.Fprintln(c.Err, err)
			return exit.Error
		}
		return exit.OK
	}

	if err := writeOutput(out, src); err != nil {
		fmt.Fprintln(c.Err, err)
		return exit.Error
	}
	fmt.Fprintln(c.Out, out)
	return exit.OK
}

// resolve interprets a path against Dir. An absolute path is already anchored,
// and an empty Dir means the process working directory, so both pass through.
func (c CLI) resolve(path string) string {
	if c.Dir == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(c.Dir, path)
}

// resolveAbs is resolve followed by Abs, for deriving the package name from a
// relative -out such as "." whose base would otherwise name nothing.
func (c CLI) resolveAbs(path string) string {
	resolved := c.resolve(path)
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return resolved
	}
	return abs
}

// checkUpToDate reports whether the file at path holds exactly src, separating
// staleness from failures that say nothing about it.
func checkUpToDate(path string, src []byte) error {
	existing, err := os.ReadFile(filepath.Clean(path))
	switch {
	case errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("%s has not been generated yet (run go generate)", path)
	case err != nil:
		return fmt.Errorf("read %s: %w", path, err)
	}
	if !bytes.Equal(existing, src) {
		return fmt.Errorf("%s is out of date (run go generate)", path)
	}
	return nil
}

// writeOutput writes the generated source, refusing to replace a file that was
// not generated: -out points wherever the flag says, and a typo must not cost
// anyone a hand-written file.
func writeOutput(path string, src []byte) error {
	existing, err := os.ReadFile(filepath.Clean(path))
	switch {
	case err == nil:
		if bytes.Equal(existing, src) {
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
	// Generated Go source should be readable like the rest of the package,
	// matching what gofmt/go generate produce by default.
	//nolint:gosec // generated source is meant to be world-readable
	if err := os.WriteFile(path, src, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
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
	fs.StringVar(&cfg.Out, "out", "messages/messages.gen.go", "output file path")
	fs.StringVar(&cfg.Package, "package", "", "package name of the generated file")
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
	fmt.Fprintln(w, "  -out <path>       output file path (default: messages/messages.gen.go)")
	fmt.Fprintln(w, "  -package <name>   package name of the generated file (default: base of the output directory)")
	fmt.Fprintln(w, "  -check            verify the output is up to date instead of writing it")
	fmt.Fprintln(w, "  --version         print version")
	fmt.Fprintln(w, "  -h, --help        show this help")
}
