// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"encoding/json"
	"fmt"
	"reflect"
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

// escapeInvisible rewrites the characters of s that can hide or forge terminal output (C1
// controls, format characters such as bidirectional overrides, line and paragraph separators) as
// \u{...}. Unlike Escape it leaves the backslash alone, so a model string keeps its meaning.
func escapeInvisible(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) && r >= 0x80 || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) {
			fmt.Fprintf(&b, `\u{%x}`, r)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// escapeContract returns a copy of the model with escapeInvisible applied to every string.
func escapeContract(c *Contract) *Contract {
	if c == nil {
		return nil
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return c
	}
	var copied Contract
	if err := json.Unmarshal(raw, &copied); err != nil {
		return c
	}
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.String:
			v.SetString(escapeInvisible(v.String()))
		case reflect.Pointer:
			if !v.IsNil() {
				walk(v.Elem())
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				walk(v.Field(i))
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
		}
	}
	walk(reflect.ValueOf(&copied))
	return &copied
}
