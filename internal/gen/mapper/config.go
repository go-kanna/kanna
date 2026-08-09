package mapper

import "strings"

// Direction controls which mapping functions are generated for a type pair.
type Direction string

const (
	// DirectionBoth generates both Src-to-Dst and Dst-to-Src functions.
	DirectionBoth Direction = "both"
	// DirectionTo generates only the Src-to-Dst function.
	DirectionTo Direction = "to"
	// DirectionFrom generates only the Dst-to-Src function.
	DirectionFrom Direction = "from"
)

// TypeRef identifies a type referenced in a flag.
type TypeRef struct {
	// Pkg is a package selector ("model"), a full import path
	// ("github.com/acme/app/internal/model"), or empty for a type in the
	// output package.
	Pkg     string
	Name    string
	Pointer bool
}

// IsImportPath reports whether Pkg is a full import path rather than a
// selector to be resolved from the go:generate file's imports.
func (r TypeRef) IsImportPath() bool {
	return strings.Contains(r.Pkg, "/")
}

// TypePair is a single SRC:DST declaration from -types.
type TypePair struct {
	Src TypeRef
	Dst TypeRef
}

// FieldRef identifies a struct field from -ignore.
type FieldRef struct {
	Type  TypeRef
	Field string
}

// Config is the parsed command-line configuration. Each field corresponds to
// exactly one flag, so a run is described entirely by what was typed.
type Config struct {
	Pairs         []TypePair
	ConverterPkgs []string
	Output        string
	Ignores       []FieldRef
	Direction     Direction
	// Package overrides the output package name when GOPACKAGE is not set.
	Package string
	// Check verifies that generated files are up to date instead of
	// writing them.
	Check bool
}
