// SPDX-License-Identifier: EUPL-1.2
package reviewstate

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)

func at(min int) time.Time { return t0.Add(time.Duration(min) * time.Minute) }

func hex(c string) string { return strings.Repeat(c, 40) }

func rev(id int64, min int, verdict, head, patch string) Comment {
	return Comment{ID: id, CreatedAt: at(min), UpdatedAt: at(min), URL: fmt.Sprintf("https://github.com/o/r/pull/7#issuecomment-%d", id),
		Body: fmt.Sprintf("Review: %s head=%s patch-id=%s pr=7\nbody", verdict, head, patch)}
}

func carry(id int64, min int, from, to, patch string) Comment {
	return Comment{ID: id, CreatedAt: at(min), UpdatedAt: at(min), URL: fmt.Sprintf("https://github.com/o/r/pull/7#issuecomment-%d", id),
		Body: fmt.Sprintf("Carried-over: from=%s to=%s patch-id=%s pr=7\n", from, to, patch)}
}

func free(id int64, min int, body string) Comment {
	return Comment{ID: id, CreatedAt: at(min), UpdatedAt: at(min), Body: body, URL: fmt.Sprintf("https://github.com/o/r/pull/7#issuecomment-%d", id)}
}

// in is a pull request 7 at head hex("a") whose head commit is dated at(0).
func in(cs ...Comment) Input {
	return Input{PR: 7, Head: hex("a"), HeadTime: at(0), Comments: cs}
}

func TestEvaluateStates(t *testing.T) {
	H, O := hex("a"), hex("b") // current and older head
	P := hex("1")
	cases := []struct {
		name  string
		in    Input
		state string
	}{
		{"no comments", in(), StateNone},
		{"plain comments only", in(free(1, 5, "please rebase")), StateNone},
		{"clear current head", in(rev(1, 5, "CLEAR", H, P)), StateClear},
		{"findings current head", in(rev(1, 5, "FINDINGS", H, P)), StateFindings},
		{"latest wins: findings after clear", in(rev(1, 5, "CLEAR", H, P), rev(2, 6, "FINDINGS", H, P)), StateFindings},
		{"latest wins: clear after findings", in(rev(1, 5, "FINDINGS", H, P), rev(2, 6, "CLEAR", H, P)), StateClear},
		{"same time: higher id wins", in(rev(2, 5, "FINDINGS", H, P), rev(1, 5, "CLEAR", H, P)), StateFindings},
		{"comment order in input does not matter", in(rev(2, 6, "CLEAR", H, P), rev(1, 5, "FINDINGS", H, P)), StateClear},
		{"comment exactly at head time counts", in(rev(1, 0, "CLEAR", H, P)), StateClear},
		{"comment before head commit is ignored", in(rev(1, -1, "CLEAR", H, P)), StateNone},
		{"head time unknown fails closed", Input{PR: 7, Head: H, Comments: []Comment{rev(1, 5, "CLEAR", H, P)}}, StateNone},
		{"clear for older head only", in(rev(1, 5, "CLEAR", O, P)), StateClearStale},
		{"findings for older head only", in(rev(1, 5, "FINDINGS", O, P)), StateNone},
		{"head prefix is not the head", in(rev(1, 5, "CLEAR", H[:39]+"b", P)), StateClearStale},
		{"carry-over", in(rev(1, -10, "CLEAR", O, P), carry(2, 5, O, H, P)), StateClearCarried},
		{"carry-over without a CLEAR", in(carry(2, 5, O, H, P)), StateNone},
		{"carry-over of findings", in(rev(1, -10, "FINDINGS", O, P), carry(2, 5, O, H, P)), StateNone},
		{"carry-over with other patch-id", in(rev(1, -10, "CLEAR", O, P), carry(2, 5, O, H, hex("2"))), StateClearStale},
		{"carry-over created before its CLEAR", in(carry(1, 1, O, H, P), rev(2, 5, "CLEAR", O, P)), StateClearStale},
		{"carry-over before head commit", in(rev(1, -20, "CLEAR", O, P), carry(2, -5, O, H, P)), StateClearStale},
		{"carry-over from the wrong head", in(rev(1, -10, "CLEAR", O, P), carry(2, 5, hex("c"), H, P)), StateClearStale},
		{"carry-over to another head", in(rev(1, -10, "CLEAR", O, P), carry(2, 5, O, hex("c"), P)), StateClearStale},
		{"origin superseded by findings before the carry", in(rev(1, -10, "CLEAR", O, P), rev(2, -5, "FINDINGS", O, P), carry(3, 5, O, H, P)), StateNone},
		{"findings after the carry-over wins", in(rev(1, -10, "CLEAR", O, P), carry(2, 5, O, H, P), rev(3, 6, "FINDINGS", H, P)), StateFindings},
		{"clear after carry-over is direct", in(rev(1, -10, "CLEAR", O, P), carry(2, 5, O, H, P), rev(3, 6, "CLEAR", H, P)), StateClear},
		{"carry-over after findings wins", in(rev(1, -10, "CLEAR", O, P), rev(2, 2, "FINDINGS", H, P), carry(3, 5, O, H, P)), StateClearCarried},
		{"two links", in(rev(1, -30, "CLEAR", hex("c"), P), carry(2, -20, hex("c"), O, P), carry(3, 5, O, H, P)), StateClearCarried},
		{"chain order broken", in(rev(1, -30, "CLEAR", hex("c"), P), carry(2, 6, hex("c"), O, P), carry(3, 5, O, H, P)), StateClearStale},
		{"unparsed only", in(free(1, 5, "Verdict: CLEAR")), StateUnparsed},
		{"unparsed older than decisive is irrelevant", in(free(1, 3, "Verdict: CLEAR"), rev(2, 5, "CLEAR", H, P)), StateClear},
		{"unparsed newer than decisive", in(rev(1, 5, "CLEAR", H, P), free(2, 6, "Urteil: FINDINGS")), StateUnparsed},
		{"unparsed newer than stale clear", in(rev(1, 5, "CLEAR", O, P), free(2, 6, "Verdict: CLEAR")), StateUnparsed},
		{"unparsed older than stale clear", in(free(1, 3, "Verdict: CLEAR"), rev(2, 5, "CLEAR", O, P)), StateClearStale},
		{"strict line for another pr is unparsed", in(Comment{ID: 1, CreatedAt: at(5), Body: "Review: CLEAR head=" + H + " patch-id=" + P + " pr=8"}), StateUnparsed},
	}
	for _, c := range cases {
		if got := Evaluate(c.in).State; got != c.state {
			t.Errorf("%s: state %q want %q", c.name, got, c.state)
		}
	}
}

func TestEvaluateChainOfNine(t *testing.T) {
	var cs []Comment
	heads := []string{}
	for i := 0; i < 10; i++ {
		heads = append(heads, strings.Repeat(string("cdef012345"[i]), 40))
	}
	cs = append(cs, rev(1, -100, "CLEAR", heads[0], hex("1")))
	for i := 0; i < 9; i++ { // 9 links from heads[0] to heads[9]
		cs = append(cs, carry(int64(10+i), -90+i, heads[i], heads[i+1], hex("1")))
	}
	cs = append(cs, carry(30, 5, heads[9], hex("a"), hex("1"))) // 10th link to H
	if got := Evaluate(in(cs...)).State; got != StateClearStale {
		t.Fatalf("a chain of ten links must not resolve: %q", got)
	}
	eight := []Comment{rev(1, -100, "CLEAR", heads[0], hex("1"))}
	for i := 0; i < 7; i++ {
		eight = append(eight, carry(int64(10+i), -90+i, heads[i], heads[i+1], hex("1")))
	}
	eight = append(eight, carry(30, 5, heads[7], hex("a"), hex("1"))) // 8 links
	if got := Evaluate(in(eight...)).State; got != StateClearCarried {
		t.Fatalf("a chain of eight links must resolve: %q", got)
	}
}

func TestEvaluateChainOfNineLinks(t *testing.T) {
	heads := []string{}
	for i := 0; i < 10; i++ {
		heads = append(heads, strings.Repeat(string("cdef012345"[i]), 40))
	}
	cs := []Comment{rev(1, -100, "CLEAR", heads[0], hex("1"))}
	for i := 0; i < 8; i++ { // 8 carry-overs reach heads[8]
		cs = append(cs, carry(int64(10+i), -90+i, heads[i], heads[i+1], hex("1")))
	}
	cs = append(cs, carry(30, 5, heads[8], hex("a"), hex("1"))) // the ninth link ends at H
	if got := Evaluate(in(cs...)).State; got != StateClearStale {
		t.Fatalf("nine links must not resolve: %q", got)
	}
}

func TestEvaluateCycleTerminates(t *testing.T) {
	O, P := hex("b"), hex("1")
	got := Evaluate(in(carry(1, 1, O, hex("c"), P), carry(2, 2, hex("c"), O, P), carry(3, 5, O, hex("a"), P))).State
	if got != StateNone {
		t.Fatalf("cycle without a CLEAR: %q", got)
	}
	// Equal times let a cycle chase itself: both carry-overs are "not after" each other.
	c, P2 := hex("c"), hex("1")
	got = Evaluate(in(carry(1, 1, O, c, P2), carry(2, 1, c, O, P2), carry(3, 5, O, hex("a"), P2))).State
	if got != StateNone {
		t.Fatalf("equal-time cycle: %q", got)
	}
}

func TestEvaluateEvidence(t *testing.T) {
	H, O, P := hex("a"), hex("b"), hex("1")
	r := Evaluate(in(rev(1, 5, "CLEAR", H, P)))
	if r.Verdict != "CLEAR" || r.Head != H || r.PatchID != P || !r.At.Equal(at(5)) || r.URL == "" || r.Carried || r.Edited {
		t.Fatalf("direct evidence: %+v", r)
	}
	c := rev(1, -10, "CLEAR", O, P)
	r = Evaluate(in(c, carry(2, 5, O, H, P)))
	if r.State != StateClearCarried || r.Verdict != "CLEAR" || r.Head != O || !r.Carried || !r.At.Equal(at(5)) || !strings.HasSuffix(r.URL, "-2") {
		t.Fatalf("carried evidence: %+v", r)
	}
	e := rev(1, 5, "CLEAR", H, P)
	e.UpdatedAt = at(9)
	if r := Evaluate(in(e)); !r.Edited || r.State != StateClear {
		t.Fatalf("edited: %+v", r)
	}
	r = Evaluate(Input{PR: 7, Head: H, HeadTime: at(0), Truncated: true, Comments: []Comment{rev(1, 5, "CLEAR", O, P)}})
	if r.State != StateClearStale || r.Verdict != "CLEAR" || r.Head != O || !r.Truncated {
		t.Fatalf("stale evidence: %+v", r)
	}
	if r := Evaluate(in(free(1, 5, "Verdict: CLEAR"))); r.Verdict != "" || r.Head != "" || r.URL != "" {
		t.Fatalf("unparsed carries no evidence: %+v", r)
	}
}

func TestNeedsHeadTime(t *testing.T) {
	H, O, P := hex("a"), hex("b"), hex("1")
	if NeedsHeadTime(7, H, nil) {
		t.Fatal("no comments")
	}
	if NeedsHeadTime(7, H, []Comment{rev(1, 5, "CLEAR", O, P), free(2, 5, "Verdict: CLEAR")}) {
		t.Fatal("older head and unparsed comments need no commit read")
	}
	if !NeedsHeadTime(7, H, []Comment{rev(1, 5, "FINDINGS", H, P)}) {
		t.Fatal("a review of the head needs the commit date")
	}
	if !NeedsHeadTime(7, H, []Comment{carry(1, 5, O, H, P)}) {
		t.Fatal("a carry-over to the head needs the commit date")
	}
}

func TestHints(t *testing.T) {
	base := HintInput{State: StateClear, Group: "ready", Base: "main", DefaultBranch: "main", ReviewRead: true}
	cases := []struct {
		name string
		mod  func(*HintInput)
		want []string
	}{
		{"ready-mergeable", func(*HintInput) {}, []string{HintReadyMergeable}},
		{"carried is ready-mergeable", func(h *HintInput) { h.State = StateClearCarried }, []string{HintReadyMergeable}},
		{"draft clear", func(h *HintInput) { h.Draft, h.Group = true, "draft" }, []string{HintClearNotReady}},
		{"draft carried", func(h *HintInput) { h.Draft, h.Group, h.State = true, "draft", StateClearCarried }, []string{HintClearNotReady}},
		{"draft clear in group ready", func(h *HintInput) { h.Draft, h.Group = true, "ready" }, []string{HintClearNotReady}},
		{"draft none", func(h *HintInput) { h.Draft, h.Group, h.State = true, "draft", StateNone }, []string{HintNoFinalReview}},
		{"non-draft not ready group", func(h *HintInput) { h.Group = "checks" }, nil},
		{"findings", func(h *HintInput) { h.State = StateFindings }, nil},
		{"none", func(h *HintInput) { h.State = StateNone }, []string{HintNoFinalReview}},
		{"stale", func(h *HintInput) { h.State = StateClearStale }, []string{HintNoFinalReview}},
		{"unparsed", func(h *HintInput) { h.State = StateUnparsed }, []string{HintNoFinalReview}},
		{"base not default", func(h *HintInput) { h.Base = "codex/parent" }, []string{HintBaseNotDefault}},
		{"base compare is exact", func(h *HintInput) { h.Base = "Main" }, []string{HintBaseNotDefault}},
		{"parent merged", func(h *HintInput) { h.Base, h.ParentMerged = "codex/parent", true }, []string{HintBaseNotDefault, HintParentMergedRetarget}},
		{"parent merged on default base is no hint", func(h *HintInput) { h.ParentMerged = true }, []string{HintReadyMergeable}},
		{"default branch unknown", func(h *HintInput) { h.DefaultBranch = "" }, nil},
		{"review not read", func(h *HintInput) { h.ReviewRead, h.State = false, "" }, nil},
		{"review not read, base known", func(h *HintInput) { h.ReviewRead, h.State, h.Base = false, "", "x" }, []string{HintBaseNotDefault}},
	}
	for _, c := range cases {
		h := base
		c.mod(&h)
		got := Hints(h)
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s: %v want %v", c.name, got, c.want)
		}
	}
}
