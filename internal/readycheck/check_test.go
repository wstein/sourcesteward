// SPDX-License-Identifier: EUPL-1.2
package readycheck

import (
	"strings"
	"testing"
)

const head = "3f2a9c1000000000000000000000000000000000"
const oldHead = "aaaaaaaa00000000000000000000000000000000"

func yes() *bool { b := true; return &b }
func no() *bool  { b := false; return &b }

// good is a pull request for which every condition passes.
func good() Facts {
	return Facts{State: "open", Draft: true, Head: head, BaseRef: "main", DefaultBranch: "main",
		Body: "Purpose.\n\nBlocker: none\n", Mergeable: yes(), Rebaseable: yes(), MergeableState: "draft",
		ChecksClass: "green", Review: Review{State: "clear", Head: head}}
}

func cond(t *testing.T, f Facts, id string) Condition {
	t.Helper()
	for _, c := range Evaluate(f) {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no condition %q", id)
	return Condition{}
}

func TestEveryConditionPassesOnTheGoodPullRequest(t *testing.T) {
	cs := Evaluate(good())
	want := []string{CondOpen, CondReview, CondChecks, CondBase, CondBlocker, CondConflict, CondRebaseable}
	if len(cs) != len(want) {
		t.Fatalf("got %d conditions", len(cs))
	}
	for i, c := range cs {
		if c.ID != want[i] || c.Status != StatusPass || c.Evidence == "" {
			t.Errorf("condition %d = %+v, want pass %s with evidence", i, c, want[i])
		}
	}
	if !Ready(cs) {
		t.Error("all pass must be ready")
	}
}

func TestEachConditionFailsForItsOwnReasonOnly(t *testing.T) {
	cases := []struct {
		name   string
		id     string
		status string
		mut    func(*Facts)
		sub    string
	}{
		{"closed", CondOpen, StatusFail, func(f *Facts) { f.State = "closed" }, "closed"},
		{"review none", CondReview, StatusFail, func(f *Facts) { f.Review = Review{State: "none"} }, "no CLEAR"},
		{"review findings", CondReview, StatusFail, func(f *Facts) { f.Review = Review{State: "findings", Head: head} }, "findings"},
		{"review stale", CondReview, StatusFail, func(f *Facts) { f.Review = Review{State: "clear-stale", Head: oldHead} }, "older head"},
		{"review clear for another head", CondReview, StatusFail, func(f *Facts) { f.Review = Review{State: "clear", Head: oldHead} }, "names"},
		{"review carried without flag", CondReview, StatusFail, func(f *Facts) { f.Review = Review{State: "clear-carried", Head: oldHead} }, "carried"},
		{"review unparsed", CondReview, StatusUnknown, func(f *Facts) { f.Review = Review{State: "unparsed"} }, "unparsed"},
		{"review read failed", CondReview, StatusUnknown, func(f *Facts) { f.ReviewErr = "comments unavailable" }, "comments unavailable"},
		{"checks red", CondChecks, StatusFail, func(f *Facts) { f.ChecksClass = "red" }, "red"},
		{"checks pending", CondChecks, StatusFail, func(f *Facts) { f.ChecksClass = "pending" }, "pending"},
		{"checks none", CondChecks, StatusFail, func(f *Facts) { f.ChecksClass = "none" }, "no checks"},
		{"checks read failed", CondChecks, StatusUnknown, func(f *Facts) { f.ChecksErr = "rate limited" }, "rate limited"},
		{"base is a branch", CondBase, StatusFail, func(f *Facts) { f.BaseRef = "codex/parent" }, "codex/parent"},
		{"base unknown default", CondBase, StatusUnknown, func(f *Facts) { f.DefaultBranch = ""; f.RepoErr = "repo unreadable" }, "repo unreadable"},
		{"blocker named", CondBlocker, StatusFail, func(f *Facts) { f.Body = "Blocker: Independent review pending\n" }, "Independent review pending"},
		{"blocker bullet bold", CondBlocker, StatusFail, func(f *Facts) { f.Body = "- **Blocker:** waits for #12\n" }, "waits for #12"},
		{"blocker empty value", CondBlocker, StatusFail, func(f *Facts) { f.Body = "Blocker:\n" }, "no value"},
		{"conflict dirty", CondConflict, StatusFail, func(f *Facts) { f.Mergeable = no(); f.MergeableState = "dirty" }, "conflict"},
		{"conflict state dirty only", CondConflict, StatusFail, func(f *Facts) { f.MergeableState = "dirty" }, "conflict"},
		{"conflict not computed", CondConflict, StatusUnknown, func(f *Facts) { f.Mergeable = nil }, "not computed"},
		{"rebaseable false", CondRebaseable, StatusFail, func(f *Facts) { f.Rebaseable = no() }, "rebaseable"},
		{"rebaseable not computed", CondRebaseable, StatusUnknown, func(f *Facts) { f.Rebaseable = nil }, "not computed"},
		{"pull read failed", CondRebaseable, StatusUnknown, func(f *Facts) { f.PullErr = "pull unreadable" }, "pull unreadable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := good()
			tc.mut(&f)
			cs := Evaluate(f)
			for _, c := range cs {
				if c.ID == tc.id {
					if c.Status != tc.status || !strings.Contains(c.Evidence, tc.sub) {
						t.Fatalf("%s = %+v, want %s containing %q", tc.id, c, tc.status, tc.sub)
					}
					continue
				}
				// Only the base failure case also reads the repository; others must stay pass.
				if tc.name == "pull read failed" || c.Status == StatusPass {
					continue
				}
				t.Errorf("unrelated condition %s is %s", c.ID, c.Status)
			}
			if Ready(cs) {
				t.Error("a non-pass condition must not be ready")
			}
		})
	}
}

func TestBlockerAcceptsNoneSpellingsAndIgnoresOtherText(t *testing.T) {
	for _, body := range []string{"", "Blocker: none", "Blocker: None.", "blocker: keiner", "* Blocker: -", "Mentions a blocker in prose: not a line.", "Unblocker: x"} {
		f := good()
		f.Body = body
		if c := cond(t, f, CondBlocker); c.Status != StatusPass {
			t.Errorf("%q = %+v", body, c)
		}
	}
	f := good()
	f.Body = "Blocker: none\nBlocker: stacked on #5\n"
	if c := cond(t, f, CondBlocker); c.Status != StatusFail || !strings.Contains(c.Evidence, "stacked on #5") {
		t.Errorf("second line must count: %+v", c)
	}
}

func TestStackChildPassesOnlyWithTheParentNote(t *testing.T) {
	f := good()
	f.BaseRef = "codex/parent"
	f.Body = "Blocker: none\nMerge #12 first.\n"
	c := cond(t, f, CondBase)
	if c.Status != StatusPass || !strings.Contains(c.Evidence, "#12") {
		t.Fatalf("%+v", c)
	}
	f.Body = "Blocker: none\nmerge #0 first\n"
	if c := cond(t, f, CondBase); c.Status != StatusFail {
		t.Fatalf("#0 is no parent: %+v", c)
	}
}

func TestReviewCarriedOverPasses(t *testing.T) {
	f := good()
	f.Review = Review{State: "clear-carried", Head: oldHead, Carried: true}
	if c := cond(t, f, CondReview); c.Status != StatusPass || !strings.Contains(c.Evidence, "carried") {
		t.Fatalf("%+v", c)
	}
}

func TestDraftIsReportedButNeverACondition(t *testing.T) {
	f := good()
	f.Draft = false
	if !Ready(Evaluate(f)) {
		t.Error("a pull request that is already ready for review still passes")
	}
}

func TestEvidenceIsEscapedAndBounded(t *testing.T) {
	f := good()
	f.Body = "Blocker: " + strings.Repeat("x\x1b[31m", 400)
	c := cond(t, f, CondBlocker)
	if strings.ContainsRune(c.Evidence, 0x1b) || len(c.Evidence) > 400 {
		t.Fatalf("evidence not escaped or bounded: %q", c.Evidence)
	}
}

func TestNothingEvaluatedIsNeverReady(t *testing.T) {
	if Ready(nil) || Ready([]Condition{}) {
		t.Error("no conditions must not be ready")
	}
	if Ready([]Condition{{ID: CondOpen, Status: StatusUnknown}}) {
		t.Error("unknown must not be ready")
	}
}
