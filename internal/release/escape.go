// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Escape is the one function that renders repository, tag, path and process text for output.
// Control characters, format characters (such as bidirectional overrides), line separators,
// invalid UTF-8 and the backslash itself are written as escapes, so hostile text cannot forge
// output lines or hide content; human and JSON renderers both use it.
func Escape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(&b, `\x%02x`, s[i])
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x80 && (r < 0x20 || r == 0x7f):
			fmt.Fprintf(&b, `\x%02x`, r)
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == 0x2028 || r == 0x2029 || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r):
			fmt.Fprintf(&b, `\u{%x}`, r)
		default:
			b.WriteRune(r)
		}
		i += size
	}
	return b.String()
}
