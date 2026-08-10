// Package i18n generates a typed message package from a directory of locale
// files: one constructor per message, validated across locales, with the
// translations themselves compiled in. Nothing the output does at run time
// re-reads the locale files.
package i18n

import (
	"fmt"
	"go/token"
	"strings"
)

// importAlias names the runtime package in generated code. buildMessage
// rejects parameters that would shadow it.
const importAlias = "i18n"

// FuncName converts a message key to an exported Go function name:
// "user.not_found" becomes "UserNotFound". Keys are ASCII-only (validated
// against [a-z][a-z0-9_]* per segment at parse time), so byte slicing is
// safe here.
func FuncName(key string) string {
	var b strings.Builder
	for _, part := range splitIdent(key) {
		b.WriteString(upperFirst(part))
	}
	return b.String()
}

// ParamName converts a placeholder name to a camelCase Go parameter name:
// "user_name" becomes "userName". Names are ASCII-only, as with FuncName.
func ParamName(name string) (string, error) {
	parts := splitIdent(name)
	if len(parts) == 0 {
		return "", fmt.Errorf("parameter %q has no usable name", name)
	}
	var b strings.Builder
	b.WriteString(parts[0])
	for _, part := range parts[1:] {
		b.WriteString(upperFirst(part))
	}
	out := b.String()
	if out[0] >= '0' && out[0] <= '9' {
		return "", fmt.Errorf("parameter %q maps to %q, which is not a valid Go identifier", name, out)
	}
	if token.IsKeyword(out) {
		return "", fmt.Errorf("parameter %q maps to the Go keyword %q", name, out)
	}
	return out, nil
}

func splitIdent(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == '.' || r == '_' })
}

func upperFirst(s string) string {
	return strings.ToUpper(s[:1]) + s[1:]
}
