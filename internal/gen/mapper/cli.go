package mapper

import (
	"errors"
	"flag"
	"fmt"
	"go/token"
	"io"
	"os"
	"strings"

	"github.com/go-kanna/kanna/internal/exit"
)

// CLI is the command-line entry point for the mapping generator. Out and Err
// default to os.Stdout/os.Stderr when constructed via NewCLI.
type CLI struct {
	Out     io.Writer
	Err     io.Writer
	Version string

	// Dir is the directory the generator runs in, which patterns and the
	// output path resolve against. Empty means the process working directory.
	Dir string

	// GoFile and GoPackage carry what `go generate` puts in the environment:
	// the file holding the directive, and the package it declares. Together
	// they decide which file's imports a package selector in -types resolves
	// against, so a run outside go generate has to make do with the directory.
	GoFile    string
	GoPackage string
}

// NewCLI constructs a CLI with default writers, the given version string, and
// the environment go generate provides.
func NewCLI(version string) CLI {
	return CLI{
		Out:       os.Stdout,
		Err:       os.Stderr,
		Version:   version,
		GoFile:    os.Getenv("GOFILE"),
		GoPackage: os.Getenv("GOPACKAGE"),
	}
}

// Run parses args (excluding the program name) and returns one of the codes in
// package exit: Error when generation fails (load errors, unresolvable pairs,
// write errors), Usage when the invocation itself is wrong.
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

	dir := c.Dir
	if dir == "" {
		dir, err = os.Getwd()
		if err != nil {
			fmt.Fprintln(c.Err, err)
			return exit.Error
		}
	}

	env := Env{GoFile: c.GoFile, GoPackage: c.GoPackage, Dir: dir}
	if err := generate(cfg, env); err != nil {
		fmt.Fprintln(c.Err, err)
		return exit.Error
	}

	return exit.OK
}

// parseFlags reads args into a Config. The middle return value reports whether
// --version was asked for, which suppresses the checks on the other flags.
func parseFlags(args []string, stderr io.Writer) (Config, bool, error) {
	fs := flag.NewFlagSet("kanna-mapper", flag.ContinueOnError)
	fs.SetOutput(stderr)

	// The caller prints the usage, because only it knows whether this is a
	// request for help, which belongs on stdout, or a mistake, which does not.
	fs.Usage = func() {}

	var types, converterPkgs, ignores listFlag
	fs.Var(&types, "types", "comma-separated SRC:DST type pairs; repeatable")
	fs.Var(&converterPkgs, "converter-pkg", "package containing mapper.Register calls; repeatable")
	fs.Var(&ignores, "ignore", "comma-separated destination fields to skip; repeatable")
	output := fs.String("output", ".", "output directory, or a file path ending in .go")
	direction := fs.String("direction", string(DirectionBoth), `which functions to generate: "both", "to", or "from"`)
	pkgName := fs.String("package", "", "output package name (defaults to $GOPACKAGE)")
	check := fs.Bool("check", false, "verify generated files are up to date instead of writing them")
	showVersion := fs.Bool("version", false, "print version")

	if err := fs.Parse(args); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			printUsage(stderr)
		}

		return Config{}, false, fmt.Errorf("parse arguments: %w", err)
	}

	if *showVersion {
		return Config{}, true, nil
	}

	cfg, err := buildConfig(types.values, converterPkgs.values, ignores.values,
		*output, *direction, *pkgName, *check)
	if err != nil {
		fmt.Fprintln(stderr, err)
		printUsage(stderr)

		return Config{}, false, err
	}

	return cfg, false, nil
}

// buildConfig validates the raw flag values and assembles the configuration.
func buildConfig(types, converterPkgs, ignores []string, output, direction, pkgName string, check bool,
) (Config, error) {
	if len(types) == 0 {
		return Config{}, errors.New("-types is required")
	}

	cfg := Config{
		ConverterPkgs: converterPkgs,
		Output:        output,
		Package:       pkgName,
		Check:         check,
	}

	for _, s := range types {
		pair, err := parsePair(s)
		if err != nil {
			return Config{}, err
		}
		cfg.Pairs = append(cfg.Pairs, pair)
	}
	for _, s := range ignores {
		ref, err := parseFieldRef(s)
		if err != nil {
			return Config{}, err
		}
		cfg.Ignores = append(cfg.Ignores, ref)
	}

	d, err := parseDirection(direction)
	if err != nil {
		return Config{}, err
	}
	cfg.Direction = d

	if cfg.Package != "" && !token.IsIdentifier(cfg.Package) {
		return Config{}, fmt.Errorf("invalid -package %q", cfg.Package)
	}
	if cfg.Output == "" {
		return Config{}, errors.New("-output must not be empty")
	}

	return cfg, nil
}

func parsePair(s string) (TypePair, error) {
	src, dst, ok := strings.Cut(s, ":")
	if !ok || src == "" || dst == "" || strings.Contains(dst, ":") {
		return TypePair{}, fmt.Errorf("invalid -types entry %q: want SRC:DST", s)
	}
	srcRef, err := parseTypeRef(src)
	if err != nil {
		return TypePair{}, fmt.Errorf("invalid -types entry %q: %w", s, err)
	}
	dstRef, err := parseTypeRef(dst)
	if err != nil {
		return TypePair{}, fmt.Errorf("invalid -types entry %q: %w", s, err)
	}
	return TypePair{Src: srcRef, Dst: dstRef}, nil
}

func parseTypeRef(s string) (TypeRef, error) {
	var ref TypeRef
	rest := strings.TrimPrefix(s, "*")
	ref.Pointer = rest != s
	if i := strings.LastIndex(rest, "."); i >= 0 {
		ref.Pkg = rest[:i]
		ref.Name = rest[i+1:]
	}
	if ref.Name == "" {
		ref.Name = rest
	}
	if !token.IsIdentifier(ref.Name) {
		return TypeRef{}, fmt.Errorf("%q is not a valid type name", ref.Name)
	}
	if ref.Pkg == "" && strings.Contains(rest, ".") {
		return TypeRef{}, fmt.Errorf("%q has an empty package selector", s)
	}
	if ref.Pkg != "" && !ref.IsImportPath() && !token.IsIdentifier(ref.Pkg) {
		return TypeRef{}, fmt.Errorf("%q is not a valid package selector", ref.Pkg)
	}
	return ref, nil
}

func parseFieldRef(s string) (FieldRef, error) {
	i := strings.LastIndex(s, ".")
	if i < 0 {
		return FieldRef{}, fmt.Errorf("invalid -ignore entry %q: want TYPE.FIELD", s)
	}
	typeSpec, field := s[:i], s[i+1:]
	if !token.IsIdentifier(field) {
		return FieldRef{}, fmt.Errorf("invalid -ignore entry %q: %q is not a valid field name", s, field)
	}
	ref, err := parseTypeRef(typeSpec)
	if err != nil {
		return FieldRef{}, fmt.Errorf("invalid -ignore entry %q: %w", s, err)
	}
	if ref.Pointer {
		return FieldRef{}, fmt.Errorf("invalid -ignore entry %q: pointer marker is not allowed", s)
	}
	return FieldRef{Type: ref, Field: field}, nil
}

func parseDirection(s string) (Direction, error) {
	switch d := Direction(s); d {
	case DirectionBoth, DirectionTo, DirectionFrom:
		return d, nil
	default:
		return "", fmt.Errorf(`invalid -direction %q: want "both", "to", or "from"`, s)
	}
}

// listFlag collects a repeatable, comma-separated flag into a slice.
type listFlag struct {
	values []string
}

var _ flag.Value = (*listFlag)(nil)

func (f *listFlag) String() string {
	return strings.Join(f.values, ",")
}

func (f *listFlag) Set(s string) error {
	for v := range strings.SplitSeq(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			f.values = append(f.values, v)
		}
	}
	return nil
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "kanna-mapper — generate struct-to-struct mapping functions for Go")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  kanna-mapper -types <SRC:DST> [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  -types <SRC:DST>        type pairs to map, comma-separated; repeatable")
	fmt.Fprintln(w, "  -converter-pkg <pkg>    package holding mapper.Register calls; repeatable")
	fmt.Fprintln(w, "  -ignore <TYPE.FIELD>    destination fields to skip; repeatable")
	fmt.Fprintln(w, "  -output <path>          output directory, or a file path ending in .go")
	fmt.Fprintln(w, `  -direction <dir>        "both" (default), "to", or "from"`)
	fmt.Fprintln(w, "  -package <name>         output package name (defaults to $GOPACKAGE)")
	fmt.Fprintln(w, "  -check                  verify the output is up to date instead of writing it")
	fmt.Fprintln(w, "  --version               print version")
	fmt.Fprintln(w, "  -h, --help              show this help")
}
