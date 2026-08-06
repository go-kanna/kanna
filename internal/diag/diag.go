// Package diag provides diagnostic types and helpers for reporting issues
// detected during code generation. A Diag carries a severity, an optional
// source position, a primary message, and zero or more hint lines that
// elaborate on it.
package diag

import (
	"fmt"
	"go/token"
	"strings"
)

// Severity classifies the importance of a diagnostic.
type Severity int

const (
	// SeverityError indicates a problem that prevents successful generation.
	SeverityError Severity = iota
	// SeverityWarning indicates a non-fatal issue.
	SeverityWarning
	// SeverityInfo is informational only.
	SeverityInfo
)

// String returns the lowercase name of the severity.
func (s Severity) String() string {
	switch s {
	case SeverityError:
		return "error"
	case SeverityWarning:
		return "warning"
	case SeverityInfo:
		return "info"
	default:
		return "unknown"
	}
}

// Diag is a diagnostic message attached to an optional source position.
type Diag struct {
	Severity Severity
	Pos      token.Position
	Message  string
	Hints    []string
}

// Errorf builds an error-severity Diag with a formatted message.
func Errorf(pos token.Position, format string, args ...any) Diag {
	return Diag{
		Severity: SeverityError,
		Pos:      pos,
		Message:  fmt.Sprintf(format, args...),
	}
}

// Warningf builds a warning-severity Diag with a formatted message.
func Warningf(pos token.Position, format string, args ...any) Diag {
	return Diag{
		Severity: SeverityWarning,
		Pos:      pos,
		Message:  fmt.Sprintf(format, args...),
	}
}

// Infof builds an info-severity Diag with a formatted message.
func Infof(pos token.Position, format string, args ...any) Diag {
	return Diag{
		Severity: SeverityInfo,
		Pos:      pos,
		Message:  fmt.Sprintf(format, args...),
	}
}

// WithHints returns a copy of d with additional hint lines appended. The
// receiver is not mutated.
func (d Diag) WithHints(hints ...string) Diag {
	if len(hints) == 0 {
		return d
	}
	out := d
	out.Hints = append(append([]string{}, d.Hints...), hints...)
	return out
}

// String renders the diagnostic as a single human-readable block:
//
//	<file>:<line>:<col>: <severity>: <message>
//	  <hint1>
//	  <hint2>
//
// When the position has no filename, the location prefix is omitted.
func (d Diag) String() string {
	var b strings.Builder
	if d.Pos.Filename != "" {
		fmt.Fprintf(&b, "%s: ", d.Pos.String())
	}
	fmt.Fprintf(&b, "%s: %s", d.Severity, d.Message)
	for _, h := range d.Hints {
		b.WriteByte('\n')
		b.WriteString("  ")
		b.WriteString(h)
	}
	return b.String()
}

// HasErrors reports whether any diagnostic in ds has SeverityError.
func HasErrors(ds []Diag) bool {
	for _, d := range ds {
		if d.Severity == SeverityError {
			return true
		}
	}
	return false
}

// Format renders all diagnostics, one per line block, with a blank line
// between successive blocks. The order of ds is preserved.
func Format(ds []Diag) string {
	if len(ds) == 0 {
		return ""
	}
	parts := make([]string, 0, len(ds))
	for _, d := range ds {
		parts = append(parts, d.String())
	}
	return strings.Join(parts, "\n")
}
