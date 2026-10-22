// SPDX-License-Identifier: EUPL-1.2

package untrusted

import (
	"strings"
	"testing"
)

func TestEscIsBounded(t *testing.T) {
	if got := Esc("a\nb\x1b[0m\u202e"); got != `a\nb\x1b[0m\u202e` {
		t.Fatalf("%q", got)
	}
	if got := Esc(strings.Repeat("x", MaxText+5)); got != strings.Repeat("x", MaxText)+"..." {
		t.Fatalf("len %d", len(got))
	}
	if got := Esc("a\r\tb\x7f\xff"); got != `a\r\tb\x7f\ufffd` {
		t.Fatalf("%q", got)
	}
}

func TestValidRepo(t *testing.T) {
	for _, s := range []string{"o/r", "wstein/source-steward", "a_b/c.d", "-a/b"} {
		if !ValidRepo(s) {
			t.Errorf("%q refused", s)
		}
	}
	for _, s := range []string{"", "o", "o/", "/r", "o/r/x", "o/r\n", ".a/b", "o/.r x", "o/r;x", "ö/r"} {
		if ValidRepo(s) {
			t.Errorf("%q accepted", s)
		}
	}
}

func TestValidReleaseTag(t *testing.T) {
	for _, s := range []string{"v1.0.0", "bdock/v0.1.0", "1.2.3", "release-2026.10", "v1+meta", strings.Repeat("a", 64)} {
		if !ValidReleaseTag(s) {
			t.Errorf("%q refused", s)
		}
	}
	bad := []string{"", "-x", "/x", "x/", "x.", "a..b", "a//b", "a b", "a~b", "a^b", "a:b", "a?b", "a*b", "a[b", `a\b`, "a@{b", "a\x00b", "a\nb", "x/.hidden", "x/y.lock", "bad\xff",
		"deadbeef1", strings.Repeat("a", 40), strings.Repeat("a", 256)}
	for _, s := range bad {
		if ValidReleaseTag(s) {
			t.Errorf("%q accepted", s)
		}
	}
}

// The bound of Spec 036 and 037 is 120 characters; the literal pins it, whatever MaxText says.
func TestEscKeepsExactly120Characters(t *testing.T) {
	if got := Esc(strings.Repeat("x", 120)); got != strings.Repeat("x", 120) {
		t.Fatalf("120 characters changed: len %d", len(got))
	}
	if got := Esc(strings.Repeat("x", 121)); got != strings.Repeat("x", 120)+"..." {
		t.Fatalf("121 characters: len %d", len(got))
	}
	if MaxText != 120 {
		t.Fatalf("MaxText = %d", MaxText)
	}
}
