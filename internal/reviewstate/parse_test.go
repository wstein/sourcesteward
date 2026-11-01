// SPDX-License-Identifier: EUPL-1.2
package reviewstate

import (
	"strings"
	"testing"
)

var (
	h1 = strings.Repeat("a", 40)
	h2 = strings.Repeat("b", 40)
	p1 = strings.Repeat("1", 40)
)

func TestParseLineStrictReview(t *testing.T) {
	got := ParseLine("Review: CLEAR head="+h1+" patch-id="+p1+" pr=7\nfurther text", 7)
	want := Line{Kind: KindReview, Verdict: "CLEAR", Head: h1, PatchID: p1, PR: 7}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
	if g := ParseLine("Review: FINDINGS head="+h1+" patch-id="+strings.Repeat("f", 64)+" pr=7\r\nx", 7); g.Kind != KindReview || g.Verdict != "FINDINGS" {
		t.Fatalf("CRLF or 64-hex patch-id refused: %+v", g)
	}
	if g := ParseLine("Review: CLEAR head="+h1+" patch-id="+p1+" pr=7", 7); g.Kind != KindReview {
		t.Fatalf("single line refused: %+v", g)
	}
}

func TestParseLineStrictCarried(t *testing.T) {
	got := ParseLine("Carried-over: from="+h1+" to="+h2+" patch-id="+p1+" pr=12\n", 12)
	want := Line{Kind: KindCarried, From: h1, To: h2, PatchID: p1, PR: 12}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestParseLineNotStrictIsUnparsedOrIgnored(t *testing.T) {
	cases := []struct {
		name, body string
		kind       Kind
	}{
		{"old verdict", "Verdict: CLEAR", KindUnparsed},
		{"old verdict upper with head", "VERDICT: CLEAR for head " + h1, KindUnparsed},
		{"german", "Urteil: CLEAR", KindUnparsed},
		{"german without clear", "Urteil: in Ordnung", KindUnparsed},
		{"findings word", "Findings: 3 open", KindUnparsed},
		{"carried over words", "Carried over from the old head", KindUnparsed},
		{"review colon", "review: looks fine", KindUnparsed},
		{"upper hex", "Review: CLEAR head=" + strings.ToUpper(h1) + " patch-id=" + p1 + " pr=7", KindUnparsed},
		{"short patch id", "Review: CLEAR head=" + h1 + " patch-id=" + strings.Repeat("1", 20) + " pr=7", KindUnparsed},
		{"missing pr", "Review: CLEAR head=" + h1 + " patch-id=" + p1, KindUnparsed},
		{"leading space", " Review: CLEAR head=" + h1 + " patch-id=" + p1 + " pr=7", KindUnparsed},
		{"double space", "Review:  CLEAR head=" + h1 + " patch-id=" + p1 + " pr=7", KindUnparsed},
		{"trailing text", "Review: CLEAR head=" + h1 + " patch-id=" + p1 + " pr=7 ok", KindUnparsed},
		{"leading zero pr", "Review: CLEAR head=" + h1 + " patch-id=" + p1 + " pr=07", KindUnparsed},
		{"other pr", "Review: CLEAR head=" + h1 + " patch-id=" + p1 + " pr=8", KindUnparsed},
		{"carried other pr", "Carried-over: from=" + h1 + " to=" + h2 + " patch-id=" + p1 + " pr=8", KindUnparsed},
		{"verdict on second line only", "thanks\nVerdict: CLEAR", KindIgnored},
		{"word inside a word", "This is clearly fine", KindIgnored},
		{"plain comment", "Please rebase.", KindIgnored},
		{"empty", "", KindIgnored},
		{"only review prose", "I will review this tomorrow", KindIgnored},
	}
	for _, c := range cases {
		if got := ParseLine(c.body, 7).Kind; got != c.kind {
			t.Errorf("%s: kind %v want %v", c.name, got, c.kind)
		}
	}
}
