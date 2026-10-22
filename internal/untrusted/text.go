// SPDX-License-Identifier: EUPL-1.2

// Package untrusted holds the checks and escapes for text that comes from outside the program (the
// forge, a repository, a path): the repository and release-tag validators that decide whether a value
// may enter a request or a suggested command, and the escape that makes any text safe for a terminal.
// It imports only the standard library, so every package may use it.
package untrusted

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxText is the number of characters Esc keeps.
const MaxText = 120

var repoName = regexp.MustCompile(`\A[A-Za-z0-9_-][A-Za-z0-9_.-]*/[A-Za-z0-9_-][A-Za-z0-9_.-]*\z`)

// ValidRepo reports whether s is an OWNER/NAME repository name.
func ValidRepo(s string) bool { return repoName.MatchString(s) }

// Esc escapes control and format characters (including bidirectional overrides) and bounds the length.
// It is exported for `stw pulls`, which prints untrusted forge text the same way (Spec 036).
func Esc(s string) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n >= MaxText {
			b.WriteString("...")
			break
		}
		n++
		switch {
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r == unicode.ReplacementChar || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r):
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ValidReleaseTag reports whether s is a release tag or branch name that Git accepts as a ref and that is not
// an ambiguous abbreviated hexadecimal identity (Spec 026). It does not make s safe for a shell: quote it.
func ValidReleaseTag(s string) bool {
	if s == "" || len(s) > 255 || !utf8.ValidString(s) || strings.HasPrefix(s, "-") || strings.HasPrefix(s, "/") || strings.HasSuffix(s, "/") || strings.HasSuffix(s, ".") || strings.ContainsAny(s, " ~^:?*[\\") || strings.Contains(s, "..") || strings.Contains(s, "@{") || strings.Contains(s, "//") || strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return false
	}
	for _, part := range strings.Split(s, "/") {
		if strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	// Abbreviated hexadecimal identities are ambiguous input, not resolved as refs.
	if len(s) < 64 {
		hex := true
		for _, c := range s {
			if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
				hex = false
			}
		}
		if hex {
			return false
		}
	}
	return true
}
