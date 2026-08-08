// Package fixture generates plain constructor functions that fill a struct with
// fake data, one per struct in a source package.
//
// The generated code is what the same fixture would look like written by hand: a
// variadic function taking setters, a composite literal, no framework. Nothing
// in this package appears at run time.
package fixture

import (
	"cmp"
	"go/token"
	"slices"

	"github.com/go-kanna/kanna/internal/diag"
	"github.com/go-kanna/kanna/internal/directive"
	"github.com/go-kanna/kanna/internal/ir"
)

// directiveKey identifies this generator within the shared kanna namespace,
// spelling the directive //kanna:ignore.
const directiveKey = "ignore"

// Target is a struct that gets a fixture function.
type Target struct {
	// Name is the type's identifier, which also names the generated function.
	Name string

	// Fields holds the exported fields in declaration order. Unexported ones are
	// dropped because the generated code lives in another package and cannot set
	// them.
	Fields []ir.Field
}

// Targets selects the structs that get a fixture function, sorted by name.
//
// Where di opts in through a struct tag, fixture takes the whole package and
// lets the author opt out with //kanna:ignore. That direction is the point: a
// struct added to the source package gets a fixture without anyone remembering
// to ask for one, which is what keeps fixtures from drifting behind the model.
//
// Sorting by name keeps the output stable when declarations are reordered or
// moved between files, so a diff on the generated file only ever shows a real
// change.
func Targets(structs []ir.Struct) ([]Target, []diag.Diag) {
	var (
		targets []Target
		diags   []diag.Diag
	)

	for _, s := range structs {
		// An unexported type cannot be named from the generated package, so it is
		// not a candidate and its comments are never read for a directive.
		if !token.IsExported(s.Name) {
			continue
		}

		d, msgs := directive.Find(s.Doc, directiveKey)
		diags = append(diags, msgs.Diags(s.Pos)...)
		if d.Found {
			continue
		}

		if s.Named != nil && s.Named.TypeParams().Len() > 0 {
			diags = append(diags, diag.Warningf(s.Pos,
				"skipping generic struct %s: a fixture function has no way to choose its type arguments", s.Name))
			continue
		}

		targets = append(targets, Target{
			Name:   s.Name,
			Fields: exportedFields(s.Fields),
		})
	}

	slices.SortStableFunc(targets, func(a, b Target) int {
		return cmp.Compare(a.Name, b.Name)
	})

	return targets, diags
}

// exportedFields keeps the fields the generated code can assign, in declaration
// order. An embedded field is kept and treated like any other, named after its
// type.
func exportedFields(fields []ir.Field) []ir.Field {
	kept := make([]ir.Field, 0, len(fields))
	for _, f := range fields {
		if f.Exported {
			kept = append(kept, f)
		}
	}
	return kept
}
