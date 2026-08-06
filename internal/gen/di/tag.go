package di

import (
	"errors"
	"fmt"
	"strings"
)

// TagKind classifies the directive embedded in an di:"..." struct tag.
type TagKind int

const (
	// TagInvalid is the zero value used for parses that did not succeed.
	TagInvalid TagKind = iota
	// TagMarker corresponds to di:"" — the bare marker form.
	TagMarker
	// TagWith corresponds to di:"with=Foo" — explicit provider selection.
	TagWith
	// TagArg corresponds to di:"arg" or di:"arg=name" — an input
	// passed in via the constructor.
	TagArg
	// TagReturns corresponds to di:"returns" — return-type declaration.
	TagReturns
	// TagEmbed corresponds to di:"embed" — a constructor input whose
	// exported struct fields are exposed as resolution sources for the
	// containing container.
	TagEmbed
)

// ParsedTag holds the result of parsing the value of an di:"..." tag.
//
// The role of the field (RoleOut / RoleArg / RoleOverride / RoleReturnsOnly) is
// determined by the caller, which combines the tag with the field name ("_" vs.
// named) and produces a final Field.
type ParsedTag struct {
	Kind TagKind
	// With is the provider reference when Kind == TagWith.
	With string
	// ArgName is the optional input name when Kind == TagArg.
	// Empty means the name should be derived from the field type by the caller.
	ArgName string
}

// ParseTag parses the value of a struct field's di:"..." tag.
//
// Supported forms:
//
//   - ""              the marker form
//   - "with=<ref>"    explicit provider reference
//   - "arg"           input parameter (name derived from type)
//   - "arg=<name>"    input parameter with explicit name
//   - "returns"       return-type declaration
//   - "embed"         input parameter whose exported struct fields are
//     additionally exposed as resolution sources
func ParseTag(value string) (ParsedTag, error) {
	s := strings.TrimSpace(value)

	if s == "" {
		return ParsedTag{Kind: TagMarker}, nil
	}

	key, val, hasEq := strings.Cut(s, "=")

	switch key {
	case "with":
		if !hasEq || val == "" {
			return ParsedTag{}, errors.New(`di:"with=..." requires a provider reference`)
		}
		return ParsedTag{Kind: TagWith, With: val}, nil
	case "arg":
		if !hasEq {
			return ParsedTag{Kind: TagArg}, nil
		}
		if val == "" {
			return ParsedTag{}, errors.New(`di:"arg=..." requires a name`)
		}
		return ParsedTag{Kind: TagArg, ArgName: val}, nil
	case "returns":
		if hasEq {
			return ParsedTag{}, errors.New(`di:"returns" does not take a value`)
		}
		return ParsedTag{Kind: TagReturns}, nil
	case "embed":
		if hasEq {
			return ParsedTag{}, errors.New(`di:"embed" does not take a value`)
		}
		return ParsedTag{Kind: TagEmbed}, nil
	default:
		return ParsedTag{}, fmt.Errorf(`unknown di tag form %q`, s)
	}
}
