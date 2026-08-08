package di

import (
	"errors"
	"fmt"
	"go/token"
	"strings"

	"github.com/go-kanna/kanna/internal/directive"
)

// directiveKey identifies this generator within the shared kanna namespace,
// spelling the directive //kanna:container.
const directiveKey = "container"

// ParsedDirective captures the values parsed from a //kanna:container line.
//
// All fields are optional. ReturnsExpr is the raw textual type expression
// (e.g. "greeter.Greeter") and is resolved to a concrete types.Type later by
// the caller (which has access to the surrounding type-checked package).
type ParsedDirective struct {
	// Found is true when a //kanna:container line was seen.
	Found bool
	// Name overrides the constructor name. Empty means not specified.
	Name string
	// ReturnsExpr is the textual return type. Empty means not specified.
	ReturnsExpr string
	// Must is the parsed must mode. MustUnset means not specified.
	Must MustMode
}

// ParseDirective scans the given comment lines for //kanna:container and returns
// the parsed values together with the messages the caller should report.
//
// Package directive owns the syntax shared with every other generator: the tag
// has to sit directly against the comment marker, and a second line is an error
// rather than a silent override. What is left here is only the meaning of the
// arguments.
func ParseDirective(commentLines []string) (ParsedDirective, directive.Messages) {
	d, msgs := directive.Find(commentLines, directiveKey)
	if !d.Found {
		return ParsedDirective{}, msgs
	}

	pd := ParsedDirective{Found: true}
	for tok := range strings.FieldsSeq(d.Args) {
		if err := applyDirectiveToken(&pd, tok); err != nil {
			msgs.Errors = append(msgs.Errors, err.Error())
		}
	}

	return pd, msgs
}

func applyDirectiveToken(pd *ParsedDirective, tok string) error {
	if tok == "" {
		return nil
	}

	key, value, hasEq := strings.Cut(tok, "=")

	switch key {
	case "name":
		if !hasEq || value == "" {
			return errors.New("directive name= requires a value")
		}
		if pd.Name != "" {
			return errors.New("directive name= specified more than once")
		}
		// The name becomes the generated constructor's identifier, so an invalid
		// one would only surface as a syntax error in the output.
		if !token.IsIdentifier(value) {
			return fmt.Errorf("directive name=%s is not a valid Go identifier", value)
		}
		pd.Name = value
	case "returns":
		if !hasEq || value == "" {
			return errors.New("directive returns= requires a value")
		}
		if pd.ReturnsExpr != "" {
			return errors.New("directive returns= specified more than once")
		}
		pd.ReturnsExpr = value
	case "must":
		if pd.Must != MustUnset {
			return errors.New("directive must specified more than once")
		}
		if !hasEq {
			pd.Must = MustOn
			return nil
		}
		switch value {
		case "true":
			pd.Must = MustOn
		case "false":
			pd.Must = MustOff
		default:
			return fmt.Errorf("directive must= must be true or false, got %q", value)
		}
	default:
		return fmt.Errorf("unknown directive key %q", key)
	}
	return nil
}
