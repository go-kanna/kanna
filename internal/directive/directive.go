// Package directive parses the //kanna:<key> comment directives that the
// generators share.
//
// Every generator writes into one namespace and distinguishes itself by the key
// that follows it: //kanna:container for di, //kanna:ignore for fixture.
// Recognizing them here keeps the syntax from drifting apart per generator,
// which matters because the rule below is easy to get subtly wrong.
package directive

import (
	"fmt"
	"go/token"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-kanna/kanna/internal/diag"
)

// namespace prefixes every kanna directive.
const namespace = "kanna:"

// Directive is the outcome of looking for one directive key.
type Directive struct {
	// Found reports whether a //kanna:<key> line was seen.
	Found bool

	// Args holds whatever followed the key on that line, with surrounding
	// whitespace removed. It is empty for a marker-only directive.
	Args string
}

// Messages are the problems found while scanning.
//
// They carry no position because the caller knows which declaration the comment
// lines came from; see Diags.
type Messages struct {
	Errors   []string
	Warnings []string
}

// Diags renders the messages as diagnostics reported at pos.
func (m Messages) Diags(pos token.Position) []diag.Diag {
	ds := make([]diag.Diag, 0, len(m.Errors)+len(m.Warnings))
	for _, msg := range m.Errors {
		ds = append(ds, diag.Errorf(pos, "%s", msg))
	}
	for _, msg := range m.Warnings {
		ds = append(ds, diag.Warningf(pos, "%s", msg))
	}
	return ds
}

// Find scans comment lines for //kanna:<key> and returns it with its arguments.
//
// At most one such line is honored; a second is an error rather than a silent
// override. A line separating the tag from the "//" by whitespace is reported as
// a warning and not honored: go/ast treats only an adjacent pair as a directive,
// so a spaced line stays in the doc comment and surfaces in `go doc` and on
// pkg.go.dev. Keeping the namespace out of documentation is the reason kanna
// spells its directives this way at all, so honoring the spaced form would
// undo it.
func Find(lines []string, key string) (Directive, Messages) {
	tag := namespace + key

	var (
		d    Directive
		msgs Messages
	)

	for _, line := range lines {
		body, ok := strings.CutPrefix(strings.TrimSpace(line), "//")
		if !ok {
			continue
		}

		args, ok := arguments(strings.TrimSpace(body), tag)
		if !ok {
			continue
		}

		if !strings.HasPrefix(body, tag) {
			msgs.Warnings = append(msgs.Warnings, fmt.Sprintf(
				"%q is not recognized as a directive; write it as %q with no space",
				strings.TrimSpace(line), "//"+tag))
			continue
		}

		if d.Found {
			msgs.Errors = append(msgs.Errors, "duplicate //"+tag+" directive")
			continue
		}

		d.Found = true
		d.Args = args
	}

	return d, msgs
}

// arguments matches tag at the start of body, either exactly or followed by
// whitespace, and returns what comes after it.
//
// A non-whitespace character right after the tag (kanna:ignoreXYZ) is rejected
// so that a longer key sharing this one's prefix is not mistaken for it.
func arguments(body, tag string) (string, bool) {
	rest, ok := strings.CutPrefix(body, tag)
	if !ok {
		return "", false
	}
	if rest == "" {
		return "", true
	}
	if r, _ := utf8.DecodeRuneInString(rest); !unicode.IsSpace(r) {
		return "", false
	}
	return strings.TrimSpace(rest), true
}
