// SPDX-License-Identifier: EUPL-1.2
package release

import "testing"

func TestEscapeMakesRepositoryTextInert(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain text /a/b-c.d", "plain text /a/b-c.d"},
		{"café \U0001F600", "café \U0001F600"},
		{"new\nline", `new\nline`},
		{"carriage\rreturn", `carriage\rreturn`},
		{"tab\there", `tab\there`},
		{"esc\x1b[31mred", `esc\x1b[31mred`},
		{"nul\x00byte", `nul\x00byte`},
		{"del\x7f", `del\x7f`},
		{"back\\slash", `back\\slash`},
		{"c1\u0085control", `c1\u{85}control`},
		{"bidi‮override", `bidi\u{202e}override`},
		{"zero​width", `zero\u{200b}width`},
		{"line separator", `line\u{2028}separator`},
		{"bom\ufeff", `bom\u{feff}`},
		{"bad\xffutf8", `bad\xffutf8`},
		{"\xc3", `\xc3`},
		{"", ""},
	}
	for _, c := range cases {
		if got := Escape(c.in); got != c.want {
			t.Errorf("Escape(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEscapeLeavesNoControlCharacters(t *testing.T) {
	for b := 0; b < 256; b++ {
		got := Escape(string([]byte{'a', byte(b), 'b'}))
		for _, r := range got {
			if r < 0x20 || r == 0x7f {
				t.Fatalf("byte %#x leaves a control character in %q", b, got)
			}
		}
	}
}
