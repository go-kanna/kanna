package relation

import (
	"strings"
	"unicode"
)

// SnakeCase converts a CamelCase name to snake_case mechanically, keeping
// consecutive uppercase letters together: "UserID" → "user_id", "QRImageID" →
// "qr_image_id". There is deliberately no initialism dictionary — one is never
// complete, and growing it would silently change what existing models infer.
// A name the mechanics get wrong (mixed-case acronyms like OAuth) takes its
// column or table name from the tag or directive instead.
func SnakeCase(s string) string {
	runes := []rune(s)
	var b strings.Builder
	for i, r := range runes {
		if unicode.IsUpper(r) {
			if i > 0 {
				prev := runes[i-1]
				next := rune(0)
				if i+1 < len(runes) {
					next = runes[i+1]
				}
				if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && unicode.IsLower(next)) {
					b.WriteByte('_')
				}
			}
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
