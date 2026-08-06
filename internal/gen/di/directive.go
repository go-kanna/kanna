package di

import (
	"errors"
	"fmt"
	"go/token"
	"strings"
	"unicode"
	"unicode/utf8"
)

// directiveTag is the prefix that marks a //kanna:container line.
//
// Every kanna generator shares the "kanna:" namespace and distinguishes itself
// by the key that follows.
const directiveTag = "kanna:container"

// ParsedDirective captures the values parsed from one or more comment lines
// containing //kanna:container ... .
//
// All fields are optional. ReturnsExpr is the raw textual type expression
// (e.g. "greeter.Greeter") and is resolved to a concrete types.Type later by
// the caller (which has access to the surrounding type-checked package).
type ParsedDirective struct {
	// Found is true when at least one //kanna:container line was seen.
	Found bool
	// Name overrides the constructor name. Empty means not specified.
	Name string
	// ReturnsExpr is the textual return type. Empty means not specified.
	ReturnsExpr string
	// Must is the parsed must mode. MustUnset means not specified.
	Must MustMode
}

// ParseDirective scans the given comment lines for //kanna:container and returns
// the parsed values together with any error messages.
//
// Comment lines may include the leading "//" or "// "; both forms are accepted.
// At most one //kanna:container line is allowed per call; duplicates produce an
// error message.
func ParseDirective(commentLines []string) (ParsedDirective, []string) {
	var (
		pd   ParsedDirective
		errs []string
	)

	for _, line := range commentLines {
		body, ok := stripCommentMarker(line)
		if !ok {
			continue
		}
		rest, ok := stripDirectivePrefix(body)
		if !ok {
			continue
		}
		if pd.Found {
			errs = append(errs, "duplicate //kanna:container directive")
			continue
		}
		pd.Found = true

		for tok := range strings.FieldsSeq(rest) {
			if err := applyDirectiveToken(&pd, tok); err != nil {
				errs = append(errs, err.Error())
			}
		}
	}

	return pd, errs
}

// stripCommentMarker removes a leading "//" (and any surrounding whitespace)
// from a comment line.
func stripCommentMarker(line string) (string, bool) {
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "//") {
		return "", false
	}
	s = strings.TrimPrefix(s, "//")
	return strings.TrimSpace(s), true
}

// stripDirectivePrefix matches "kanna:container" exactly or followed by
// whitespace (any kind: space, tab, etc.), returning the trailing arguments.
//
// A non-whitespace character right after the tag (e.g. "kanna:containerXYZ") is
// rejected so the prefix is not mistaken for our directive.
func stripDirectivePrefix(body string) (string, bool) {
	if !strings.HasPrefix(body, directiveTag) {
		return "", false
	}
	rest := body[len(directiveTag):]
	if rest == "" {
		return "", true
	}
	r, _ := utf8.DecodeRuneInString(rest)
	if !unicode.IsSpace(r) {
		return "", false
	}
	return strings.TrimSpace(rest), true
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
