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
		Association: "OWNER", Body: fmt.Sprintf("Review: %s head=%s patch-id=%s pr=7\nbody", verdict, head, patch)}
}

func carry(id int64, min int, from, to, patch string) Comment {
	return Comment{ID: id, CreatedAt: at(min), UpdatedAt: at(min), URL: fmt.Sprintf("https://github.com/o/r/pull/7#issuecomment-%d", id),
		Association: "OWNER", Body: fmt.Sprintf("Carried-over: from=%s to=%s patch-id=%s pr=7\n", from, to, patch)}
}

func free(id int64, min int, body string) Comment {
	return Comment{ID: id, CreatedAt: at(min), UpdatedAt: at(min), Body: body, Association: "OWNER", URL: fmt.Sprintf("https://github.com/o/r/pull/7#issuecomment-%d", id)}
}

// by returns c as posted by an author with the given association.
func by(c Comment, association string) Comment {
	c.Association = association
	return c
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
		{"strict line for another pr is unparsed", in(Comment{ID: 1, CreatedAt: at(5), Association: "OWNER", Body: "Review: CLEAR head=" + H + " patch-id=" + P + " pr=8"}), StateUnparsed},
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

// evaluateWithin fails the test when Evaluate does not return in time: the chain check must be
// polynomial, because every commenter of a public repository can post carry-over lines.
func evaluateWithin(t *testing.T, in Input) Result {
	t.Helper()
	done := make(chan Result, 1)
	go func() { done <- Evaluate(in) }()
	select {
	case r := <-done:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("Evaluate did not finish within 5s: the chain check is not polynomial")
		return Result{}
	}
}

func TestEvaluateManySelfCarryOvers(t *testing.T) {
	H, A, P := hex("a"), hex("b"), hex("1")
	var cs []Comment
	for i := 0; i < 40; i++ {
		cs = append(cs, carry(int64(10+i), -50+i, A, A, P))
	}
	cs = append(cs, carry(99, 5, A, H, P))
	if got := evaluateWithin(t, in(cs...)).State; got != StateNone {
		t.Fatalf("self carry-overs without a CLEAR: %q", got)
	}
	cs = append([]Comment{rev(1, -100, "CLEAR", A, P)}, cs...)
	if r := evaluateWithin(t, in(cs...)); r.State != StateClearCarried || r.Head != A {
		t.Fatalf("self carry-overs are skipped, the CLEAR still carries: %+v", r)
	}
}

func TestEvaluateManyHeadsManyLinks(t *testing.T) {
	// Eight heads, every ordered pair carried at distinct times: the number of chains is exponential,
	// the number of (head, position) states is not.
	P := hex("1")
	hs := []string{}
	for i := 0; i < 8; i++ {
		hs = append(hs, strings.Repeat(string("cdef0123"[i]), 40))
	}
	cs := []Comment{rev(1, -1000, "FINDINGS", hs[0], P)}
	id := int64(10)
	min := -900
	for round := 0; round < 6; round++ {
		for i := range hs {
			for j := range hs {
				if i != j {
					cs = append(cs, carry(id, min, hs[i], hs[j], P))
					id++
					min++
				}
			}
		}
	}
	cs = append(cs, carry(9999, 5, hs[7], hex("a"), P))
	if got := evaluateWithin(t, in(cs...)).State; got != StateNone {
		t.Fatalf("no CLEAR anywhere: %q", got)
	}
	cs[0] = rev(1, -1000, "CLEAR", hs[0], P)
	if got := evaluateWithin(t, in(cs...)).State; got != StateClearCarried {
		t.Fatalf("a CLEAR at the start of a dense graph: %q", got)
	}
}

func TestEvaluateRepeatedHeadEndsTheChain(t *testing.T) {
	H, A, B, P := hex("a"), hex("b"), hex("c"), hex("1")
	// A is carried to B and back to A; the back link repeats A and is not followed, so the carry from
	// A to H rests on the direct CLEAR of A. It is superseded by FINDINGS on A in between.
	cs := []Comment{rev(1, -30, "CLEAR", A, P), carry(2, -20, A, B, P), carry(3, -10, B, A, P),
		rev(4, -5, "FINDINGS", A, P), carry(5, 5, A, H, P)}
	if got := Evaluate(in(cs...)).State; got != StateNone {
		t.Fatalf("a repeated head must not revive a superseded CLEAR: %q", got)
	}
	cs = []Comment{carry(2, -20, A, B, P), carry(3, -10, B, A, P), carry(5, 5, A, H, P)}
	if got := Evaluate(in(cs...)).State; got != StateNone {
		t.Fatalf("a loop is no origin: %q", got)
	}
}

func TestEvaluateLoopBackToARepeatedHead(t *testing.T) {
	// A is carried to B and back to A. The link back repeats A and ends the chain, so the newer direct
	// CLEAR of A with another patch-id stays the latest verdict of A and the carry-over of P fails.
	H, A, B, P, P2 := hex("a"), hex("b"), hex("c"), hex("1"), hex("2")
	cs := []Comment{rev(1, -30, "CLEAR", A, P), carry(2, -20, A, B, P), rev(3, -15, "CLEAR", A, P2),
		carry(4, -10, B, A, P), carry(5, 5, A, H, P)}
	if r := Evaluate(in(cs...)); r.State != StateClearStale || r.PatchID != P2 {
		t.Fatalf("loop back to A: %+v", r)
	}
	// The loop closes later in the chain: A to B to C, then back to B.
	C := hex("d")
	cs = []Comment{rev(1, -30, "CLEAR", A, P), carry(2, -25, A, B, P), carry(3, -20, B, C, P), rev(4, -15, "CLEAR", B, P2),
		carry(5, -10, C, B, P), carry(6, 5, B, H, P)}
	if r := Evaluate(in(cs...)); r.State != StateClearStale || r.PatchID != P2 {
		t.Fatalf("loop back to B: %+v", r)
	}
}

func TestCarryOverAfterFindingsDoesNotReuseAnEarlierCheck(t *testing.T) {
	// X is carried to H twice, FINDINGS for X lie between. The first carry-over is valid, the second is not.
	H, X, P := hex("a"), hex("b"), hex("1")
	cs := []Comment{rev(1, -30, "CLEAR", X, P), carry(2, 1, X, H, P), rev(3, 3, "FINDINGS", X, P), carry(4, 5, X, H, P)}
	r := Evaluate(in(cs...))
	if r.State != StateClearCarried || !strings.HasSuffix(r.URL, "-2") {
		t.Fatalf("the carry-over after FINDINGS is not valid: %+v", r)
	}
}

func TestChainWorkIsBounded(t *testing.T) {
	P := hex("1")
	hs := []string{}
	for i := 0; i < 8; i++ {
		hs = append(hs, strings.Repeat(string("cdef0123"[i]), 40))
	}
	cs := []Comment{rev(1, -1000, "CLEAR", hs[0], P)}
	id, min := int64(10), -900
	for round := 0; round < 4; round++ {
		for i := range hs {
			for j := range hs {
				if i != j {
					cs = append(cs, carry(id, min, hs[i], hs[j], P))
					id++
					min++
				}
			}
		}
	}
	cs = append(cs, carry(9999, 5, hs[7], hex("a"), P))
	strict, _, _ := parseAll(7, cs)
	ch := newChains(strict)
	done := make(chan struct{})
	go func() { ch.carried(len(strict) - 1); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("chain evaluation did not finish within 5s")
	}
	if ch.calls > len(strict) {
		t.Fatalf("%d carry-over checks for %d comments", ch.calls, len(strict))
	}
}

func TestOnlyOwnerMemberCollaboratorCount(t *testing.T) {
	H, P := hex("a"), hex("1")
	for assoc, want := range map[string]string{
		"OWNER": StateClear, "MEMBER": StateClear, "COLLABORATOR": StateClear,
		"CONTRIBUTOR": StateNone, "FIRST_TIME_CONTRIBUTOR": StateNone, "FIRST_TIMER": StateNone, "MANNEQUIN": StateNone, "NONE": StateNone,
		"": StateNone, "owner": StateNone, "OWNER ": StateNone, "OWNERS": StateNone,
	} {
		r := Evaluate(in(by(rev(1, 5, "CLEAR", H, P), assoc)))
		if r.State != want {
			t.Errorf("CLEAR by %q: state %q want %q", assoc, r.State, want)
		}
		if want == StateClear && r.Association != assoc {
			t.Errorf("CLEAR by %q: association %q", assoc, r.Association)
		}
		if want == StateNone && (r.Ignored != 1 || r.Association != "" || r.URL != "" || r.Verdict != "") {
			t.Errorf("CLEAR by %q must be counted and leave no evidence: %+v", assoc, r)
		}
	}
}

func TestForeignCommentsHaveNoEffect(t *testing.T) {
	H, O, P := hex("a"), hex("b"), hex("1")
	cases := []struct {
		name    string
		in      Input
		state   string
		ignored int
	}{
		{"foreign FINDINGS after an owner CLEAR", in(rev(1, 5, "CLEAR", H, P), by(rev(2, 6, "FINDINGS", H, P), "NONE")), StateClear, 1},
		{"foreign CLEAR after owner FINDINGS", in(rev(1, 5, "FINDINGS", H, P), by(rev(2, 6, "CLEAR", H, P), "CONTRIBUTOR")), StateFindings, 1},
		{"foreign unparsed comment after an owner CLEAR", in(rev(1, 5, "CLEAR", H, P), by(free(2, 6, "Verdict: FINDINGS"), "NONE")), StateClear, 1},
		{"foreign unparsed comment alone", in(by(free(1, 6, "Verdict: CLEAR"), "NONE")), StateNone, 1},
		{"foreign chat is not counted", in(by(free(1, 6, "looks good to me"), "NONE")), StateNone, 0},
		{"foreign carry-over", in(rev(1, -10, "CLEAR", O, P), by(carry(2, 5, O, H, P), "FIRST_TIME_CONTRIBUTOR")), StateClearStale, 1},
		{"foreign CLEAR as origin of an owner carry-over", in(by(rev(1, -10, "CLEAR", O, P), "NONE"), carry(2, 5, O, H, P)), StateNone, 1},
		{"foreign link in the middle of a chain", in(rev(1, -30, "CLEAR", hex("c"), P), by(carry(2, -20, hex("c"), O, P), "NONE"), carry(3, 5, O, H, P)), StateClearStale, 1},
		{"foreign CLEAR for an older head is no stale CLEAR", in(by(rev(1, 5, "CLEAR", O, P), "NONE")), StateNone, 1},
		{"foreign strict line for another pr", in(by(Comment{ID: 1, CreatedAt: at(5), Association: "NONE", Body: "Review: CLEAR head=" + H + " patch-id=" + P + " pr=8"}, "NONE")), StateNone, 1},
		{"foreign line and an owner unparsed comment", in(by(rev(1, 5, "CLEAR", H, P), "NONE"), free(2, 6, "Verdict: CLEAR")), StateUnparsed, 1},
		{"many foreign lines are all counted", in(by(rev(1, 5, "CLEAR", H, P), "NONE"), by(rev(2, 6, "CLEAR", H, P), "NONE"), by(free(3, 7, "Urteil: CLEAR"), "NONE")), StateNone, 3},
	}
	for _, c := range cases {
		r := Evaluate(c.in)
		if r.State != c.state || r.Ignored != c.ignored {
			t.Errorf("%s: state %q ignored %d, want %q and %d", c.name, r.State, r.Ignored, c.state, c.ignored)
		}
	}
}

func TestAssociationOfTheDecisiveComment(t *testing.T) {
	H, O, P := hex("a"), hex("b"), hex("1")
	if r := Evaluate(in(by(rev(1, -10, "CLEAR", O, P), "MEMBER"), by(carry(2, 5, O, H, P), "COLLABORATOR"))); r.State != StateClearCarried || r.Association != "COLLABORATOR" {
		t.Fatalf("carried: the carry-over is the decisive comment: %+v", r)
	}
	if r := Evaluate(in(by(rev(1, 5, "CLEAR", O, P), "MEMBER"))); r.State != StateClearStale || r.Association != "MEMBER" {
		t.Fatalf("stale: %+v", r)
	}
	if r := Evaluate(in(by(rev(1, 5, "FINDINGS", H, P), "COLLABORATOR"))); r.State != StateFindings || r.Association != "COLLABORATOR" {
		t.Fatalf("findings: %+v", r)
	}
}

func TestNeedsHeadTimeIgnoresForeignComments(t *testing.T) {
	H, P := hex("a"), hex("1")
	if NeedsHeadTime(7, H, []Comment{by(rev(1, 5, "CLEAR", H, P), "NONE")}) {
		t.Fatal("a foreign review of the head needs no commit read")
	}
	if !NeedsHeadTime(7, H, []Comment{by(rev(1, 5, "CLEAR", H, P), "NONE"), by(rev(2, 5, "CLEAR", H, P), "OWNER")}) {
		t.Fatal("an owner review of the head needs the commit date")
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
		{"truncated clear is not ready-mergeable", func(h *HintInput) { h.Truncated = true }, []string{HintTruncatedReviewUnknown}},
		{"truncated carried is not ready-mergeable", func(h *HintInput) { h.Truncated, h.State = true, StateClearCarried }, []string{HintTruncatedReviewUnknown}},
		{"truncated draft clear", func(h *HintInput) { h.Truncated, h.Draft, h.Group = true, true, "draft" }, []string{HintClearNotReady, HintTruncatedReviewUnknown}},
		{"truncated none", func(h *HintInput) { h.Truncated, h.State = true, StateNone }, []string{HintNoFinalReview, HintTruncatedReviewUnknown}},
		{"truncated but not read", func(h *HintInput) { h.Truncated, h.ReviewRead, h.State = true, false, "" }, nil},
		{"foreign review ignored", func(h *HintInput) { h.Ignored = 2 }, []string{HintForeignReviewIgnored, HintReadyMergeable}},
		{"foreign review ignored, no review", func(h *HintInput) { h.Ignored, h.State = 1, StateNone }, []string{HintNoFinalReview, HintForeignReviewIgnored}},
		{"foreign review but comments not read", func(h *HintInput) { h.Ignored, h.ReviewRead, h.State = 1, false, "" }, nil},
		{"both", func(h *HintInput) { h.Ignored, h.Truncated, h.Base, h.ParentMerged = 1, true, "p", true }, []string{HintBaseNotDefault, HintParentMergedRetarget, HintTruncatedReviewUnknown, HintForeignReviewIgnored}},
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
