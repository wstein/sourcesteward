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
		{"base is a branch without a parent", CondBase, StatusFail, func(f *Facts) { f.BaseRef = "codex/parent" }, "codex/parent"},
		{"base unknown default", CondBase, StatusUnknown, func(f *Facts) { f.DefaultBranch = ""; f.RepoErr = "repo unreadable" }, "repo unreadable"},
		{"base empty default without a read error", CondBase, StatusUnknown, func(f *Facts) { f.DefaultBranch = "" }, "default branch"},
		{"base parents not read", CondBase, StatusUnknown, func(f *Facts) { f.BaseRef = "codex/parent"; f.ParentErr = "list unreadable" }, "list unreadable"},
		{"blocker named", CondBlocker, StatusFail, func(f *Facts) { f.Body = "Blocker: Independent review pending\n" }, "Independent review pending"},
		{"blocker bullet bold", CondBlocker, StatusFail, func(f *Facts) { f.Body = "- **Blocker:** waits for #12\n" }, "waits for #12"},
		{"blocker empty value", CondBlocker, StatusFail, func(f *Facts) { f.Body = "Blocker:\n" }, "no value"},
		{"blocker heading", CondBlocker, StatusFail, func(f *Facts) { f.Body = "## Blocker\nIndependent review pending\n" }, "Independent review pending"},
		{"blocker heading after blank line", CondBlocker, StatusFail, func(f *Facts) { f.Body = "## Blocker\n\nwaits for #5\n\n## Tests\nx\n" }, "waits for #5"},
		{"blocker heading without a section", CondBlocker, StatusFail, func(f *Facts) { f.Body = "## Blocker\n\n## Tests\nx\n" }, "no value"},
		{"blocker heading with a list", CondBlocker, StatusFail, func(f *Facts) { f.Body = "## Blocker\n- none\n- also waits for #6\n" }, "waits for #6"},
		{"blocker with a qualifier", CondBlocker, StatusFail, func(f *Facts) { f.Body = "**Blocker (Draft):** review pending\n" }, "review pending"},
		{"blocker numbered", CondBlocker, StatusFail, func(f *Facts) { f.Body = "1. Blocker: review pending\n" }, "review pending"},
		{"blocker named in bold", CondBlocker, StatusFail, func(f *Facts) { f.Body = "**Draft, named blocker:** review pending\n" }, "review pending"},
		{"blocker after a sentence", CondBlocker, StatusFail, func(f *Facts) { f.Body = "Draft. Blocker: review pending\n" }, "review pending"},
		{"blocker heading with a colon", CondBlocker, StatusFail, func(f *Facts) { f.Body = "## Blocker: review pending\n" }, "review pending"},
		{"blocker in a quote", CondBlocker, StatusFail, func(f *Facts) { f.Body = "> Blocker: review pending\n" }, "review pending"},
		{"blocker bare in a quote", CondBlocker, StatusFail, func(f *Facts) { f.Body = "> Blocker\nreview pending\n" }, "review pending"},
		{"blocker bare in a list", CondBlocker, StatusFail, func(f *Facts) { f.Body = "- Blocker\nreview pending\n" }, "review pending"},
		{"blocker bare numbered", CondBlocker, StatusFail, func(f *Facts) { f.Body = "1. Blocker\nreview pending\n" }, "review pending"},
		{"blocker bare in bold", CondBlocker, StatusFail, func(f *Facts) { f.Body = "**Blocker**\nreview pending\n" }, "review pending"},
		{"blocker heading with more words", CondBlocker, StatusFail, func(f *Facts) { f.Body = "## Known blocker\nreview pending\n" }, "review pending"},
		{"blocker plural", CondBlocker, StatusFail, func(f *Facts) { f.Body = "Blockers: review pending\n" }, "review pending"},
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
	for _, body := range []string{"", "Blocker: none", "Blocker: None.", "blocker: keiner", "Blocker: keine", "Blocker: no", "Blocker: n/a", "* Blocker: -", "- **Blocker:** none",
		"**Blocker (Draft):** none", "1. Blocker: none", "## Blocker\nnone\n\n## Tests\nBlocker-free text\n", "## Blocker\n\n- none\n", "## Blocker: none",
		"Blocker:\nnone\n\nPurpose text follows the section.", "Known blocker\nreview pending\n", "Mentions a blocker in prose.", "Unblocker: x", "The blocker list is short and has one entry: none of them"} {
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

func TestStackChildPassesOnlyWithAnOpenParentPullRequest(t *testing.T) {
	f := good()
	f.BaseRef = "codex/parent"
	f.ParentPR = 12
	c := cond(t, f, CondBase)
	if c.Status != StatusPass || !strings.Contains(c.Evidence, "#12") || !strings.Contains(c.Evidence, "codex/parent") {
		t.Fatalf("%+v", c)
	}
	f.ParentPR = 0
	f.Body = "Blocker: none\nMerge #12 first.\n"
	if c := cond(t, f, CondBase); c.Status != StatusFail {
		t.Fatalf("the text line no longer counts: %+v", c)
	}
	f = good()
	f.ParentPR = 12 // a parent does not matter when the base is the default branch
	if c := cond(t, f, CondBase); c.Status != StatusPass || strings.Contains(c.Evidence, "#12") {
		t.Fatalf("%+v", c)
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

// Every field that reaches the evidence is hostile and long; the schema allows 400 characters.
func TestEveryEvidenceStaysWithinTheSchemaLimit(t *testing.T) {
	hostile := strings.Repeat("\x1b\x00\u00e4", 600)
	f := Facts{State: hostile, Head: head, BaseRef: hostile, DefaultBranch: hostile, Body: "Blocker: " + hostile, Mergeable: no(), MergeableState: hostile,
		Rebaseable: no(), ChecksClass: hostile, Review: Review{State: hostile, Head: hostile}, PullErr: hostile}
	for _, mut := range []func(*Facts){func(*Facts) {}, func(f *Facts) {
		f.PullErr = ""
		f.RepoErr = hostile
		f.ChecksErr = hostile
		f.ReviewErr = hostile
		f.ParentErr = hostile
	},
		func(f *Facts) { f.PullErr = ""; f.Review.State = "clear" }} {
		g := f
		mut(&g)
		for _, c := range Evaluate(g) {
			if n := len([]rune(c.Evidence)); n > 400 || len(c.Evidence) > 400 {
				t.Errorf("%s evidence has %d characters, %d bytes", c.ID, n, len(c.Evidence))
			}
		}
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
