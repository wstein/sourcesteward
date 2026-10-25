// SPDX-License-Identifier: EUPL-1.2

package stackpublish

import (
	"reflect"
	"strings"
	"testing"
)

func goodPolicy() WorkspacePolicy {
	return WorkspacePolicy{Loaded: true, Owner: Identity{Name: "Werner Stein", Email: "werner@example.org"},
		Reviewers:     []Reviewer{{ID: "opus-reviewer", Tier: TierStrongest}, {ID: "sonnet-reviewer", Tier: TierMiddle}, {ID: "haiku-helper", Tier: TierHelper}},
		ReviewSources: []string{"pr-review"}, CheckSources: []string{"branch-protection+rulesets"}, ScopeSources: []string{"pr-body-scope"},
		RestackReceipts:  []ReceiptSpec{{Producer: "stw", Command: "restack apply", Kind: "restack-apply", SchemaVersion: 2}},
		RemoteSignatures: TriNo}
}

func goodReady() ReadyInput {
	return ReadyInput{Policy: goodPolicy(), Head: sha('a'), BaseSHA: sha('b'), AuthorID: "claude-author",
		Guards: []Guard{{Name: GuardOwnership, Result: GuardPass}, {Name: GuardAttribution, Result: GuardPass}},
		Review: &ReviewEvidence{Source: "pr-review", ReviewerID: "opus-reviewer", Verdict: VerdictClear, SubjectSHA: sha('a'), EvidenceDigest: strings.Repeat("e", 64), Locator: "pull/1#pullrequestreview-9"},
		Checks: &ChecksEvidence{Source: "branch-protection+rulesets", Complete: true, Required: []string{"check", "suite"},
			Runs: []CheckRun{{"check", sha('a'), "success"}, {"suite", sha('a'), "success"}, {"optional", sha('a'), "failure"}}},
		Rebase: &RebaseEvidence{Rebaseable: TriYes, EvaluatedHead: sha('a'), EvaluatedBase: sha('b')},
		Scope:  &ScopeEvidence{Source: "pr-body-scope", Accepted: TriYes, OutOfScope: []string{"later wave"}}}
}

func blockerList(r ReadyReceipt) []string {
	out := []string{}
	for _, b := range r.Blockers {
		out = append(out, b.String())
	}
	return out
}

func TestReadyWhenEveryGatePassesAndOutOfScopeWorkIsIgnored(t *testing.T) {
	r := EvaluateReady(goodReady())
	if !r.Ready() || len(r.Blockers) != 0 || r.Status() != StatusComplete || ExitCode(r.Status()) != 0 || r.Head != sha('a') || r.BaseSHA != sha('b') {
		t.Fatalf("%+v", r)
	}
	if gs := r.Guards(); len(gs) != 1 || gs[0].Name != GuardReady || gs[0].Result != GuardPass || gs[0].Reason != "" {
		t.Fatalf("%+v", gs)
	}
}

func TestEachUnknownFalseOrMovedSourceBlocksReady(t *testing.T) {
	cases := map[string]struct {
		mutate func(*ReadyInput)
		want   []string
	}{
		"policy not loaded":       {func(in *ReadyInput) { in.Policy.Loaded = false }, []string{"review-missing/policy-missing", "checks-unknown/policy-missing", "scope-not-accepted/policy-missing"}},
		"head is not a full sha":  {func(in *ReadyInput) { in.Head = "abc123" }, []string{"head-changed/head-invalid"}},
		"base is not a full sha":  {func(in *ReadyInput) { in.BaseSHA = strings.Repeat("b", 64) }, []string{"head-changed/base-invalid"}},
		"ownership guard missing": {func(in *ReadyInput) { in.Guards = in.Guards[1:] }, []string{"ownership-unknown/guard"}},
		"d5 guard fails": {func(in *ReadyInput) {
			in.Guards[1] = Guard{Name: GuardAttribution, Result: GuardFail, Reason: ReasonAttributionMismatch}
		}, []string{"attribution-mismatch/guard"}},
		"review absent":              {func(in *ReadyInput) { in.Review = nil }, []string{"review-missing/review-absent"}},
		"review of an older head":    {func(in *ReadyInput) { in.Review.SubjectSHA = sha('9') }, []string{"review-missing/review-wrong-head"}},
		"review of a short sha":      {func(in *ReadyInput) { in.Review.SubjectSHA = "aaaaaaa" }, []string{"review-missing/review-wrong-head"}},
		"review not clear":           {func(in *ReadyInput) { in.Review.Verdict = VerdictNotClear }, []string{"review-missing/review-not-clear"}},
		"review verdict unknown":     {func(in *ReadyInput) { in.Review.Verdict = "" }, []string{"review-missing/review-not-clear"}},
		"reviewer not allowlisted":   {func(in *ReadyInput) { in.Review.ReviewerID = "stranger" }, []string{"review-missing/review-untrusted"}},
		"review source not accepted": {func(in *ReadyInput) { in.Review.Source = "comment" }, []string{"review-missing/review-untrusted"}},
		"self review": {func(in *ReadyInput) {
			in.Review.ReviewerID = "claude-author"
			in.Policy.Reviewers = append(in.Policy.Reviewers, Reviewer{ID: "claude-author", Tier: TierStrongest})
		}, []string{"review-missing/review-not-independent"}},
		"review digest malformed":    {func(in *ReadyInput) { in.Review.EvidenceDigest = "abc" }, []string{"review-missing/review-evidence-invalid"}},
		"review locator empty":       {func(in *ReadyInput) { in.Review.Locator = "" }, []string{"review-missing/review-evidence-invalid"}},
		"checks absent":              {func(in *ReadyInput) { in.Checks = nil }, []string{"checks-unknown/checks-absent"}},
		"checks source not accepted": {func(in *ReadyInput) { in.Checks.Source = "ci-status-api" }, []string{"checks-unknown/checks-untrusted"}},
		"checks sources incomplete":  {func(in *ReadyInput) { in.Checks.Complete = false }, []string{"checks-unknown/checks-incomplete"}},
		"no required contexts":       {func(in *ReadyInput) { in.Checks.Required = nil }, []string{"checks-unknown/checks-no-required"}},
		"required run missing":       {func(in *ReadyInput) { in.Checks.Runs = in.Checks.Runs[:1] }, []string{"checks-unknown/checks-run-missing"}},
		"run reported for old head":  {func(in *ReadyInput) { in.Checks.Runs[1].HeadSHA = sha('9') }, []string{"checks-unknown/checks-stale-head"}},
		"required run failed":        {func(in *ReadyInput) { in.Checks.Runs[1].Conclusion = "failure" }, []string{"checks-not-green/checks-red"}},
		"required run pending":       {func(in *ReadyInput) { in.Checks.Runs[1].Conclusion = "" }, []string{"checks-not-green/checks-red"}},
		"one green one red same ctx": {func(in *ReadyInput) { in.Checks.Runs = append(in.Checks.Runs, CheckRun{"suite", sha('a'), "failure"}) }, []string{"checks-not-green/checks-red"}},
		"rebase absent":              {func(in *ReadyInput) { in.Rebase = nil }, []string{"rebaseable-unknown/rebaseable-absent"}},
		"rebase null":                {func(in *ReadyInput) { in.Rebase.Rebaseable = TriUnknown }, []string{"rebaseable-unknown/rebaseable-null"}},
		"rebase empty":               {func(in *ReadyInput) { in.Rebase.Rebaseable = "" }, []string{"rebaseable-unknown/rebaseable-null"}},
		"rebase false":               {func(in *ReadyInput) { in.Rebase.Rebaseable = TriNo }, []string{"rebaseable-unknown/rebaseable-false"}},
		"rebase for an old head":     {func(in *ReadyInput) { in.Rebase.EvaluatedHead = sha('9') }, []string{"rebaseable-unknown/rebaseable-stale"}},
		"rebase for an old base":     {func(in *ReadyInput) { in.Rebase.EvaluatedBase = sha('9') }, []string{"rebaseable-unknown/rebaseable-stale"}},
		"scope absent":               {func(in *ReadyInput) { in.Scope = nil }, []string{"scope-not-accepted/scope-absent"}},
		"scope source not accepted":  {func(in *ReadyInput) { in.Scope.Source = "chat" }, []string{"scope-not-accepted/scope-untrusted"}},
		"scope unknown":              {func(in *ReadyInput) { in.Scope.Accepted = TriUnknown }, []string{"scope-not-accepted/scope-unknown"}},
		"scope not accepted":         {func(in *ReadyInput) { in.Scope.Accepted = TriNo }, []string{"scope-not-accepted/scope-not-accepted"}},
		"unmet acceptance in scope":  {func(in *ReadyInput) { in.Scope.UnmetInScope = []string{"T030 test missing"} }, []string{"scope-not-accepted/scope-unmet-acceptance"}},
	}
	for name, tc := range cases {
		in := goodReady()
		tc.mutate(&in)
		r := EvaluateReady(in)
		if r.Ready() || !reflect.DeepEqual(blockerList(r), tc.want) {
			t.Errorf("%s: ready=%v blockers %v want %v", name, r.Ready(), blockerList(r), tc.want)
		}
		if r.Status() != StatusRefused || ExitCode(r.Status()) != 1 {
			t.Errorf("%s: status %v", name, r.Status())
		}
		if gs := r.Guards(); len(gs) != 1 || gs[0].Result != GuardFail || gs[0].Reason != r.Blockers[0].Reason {
			t.Errorf("%s: guards %+v", name, gs)
		}
	}
}

func TestAllBlockersAreNamedInAFixedOrder(t *testing.T) {
	in := goodReady()
	in.Guards = nil
	in.Review, in.Checks, in.Rebase, in.Scope = nil, nil, nil, nil
	want := []string{"ownership-unknown/guard", "attribution-unknown/guard", "review-missing/review-absent", "checks-unknown/checks-absent", "rebaseable-unknown/rebaseable-absent", "scope-not-accepted/scope-absent"}
	for i := 0; i < 20; i++ {
		if got := blockerList(EvaluateReady(in)); !reflect.DeepEqual(got, want) {
			t.Fatalf("%v", got)
		}
	}
}

func TestReadyEvidenceIsNotSharedWithTheCaller(t *testing.T) {
	in := goodReady()
	r := EvaluateReady(in)
	in.Review.Verdict = VerdictNotClear
	in.Checks.Runs[0].Conclusion = "failure"
	if !r.Ready() {
		t.Fatal("a later change of the caller's evidence changed the receipt")
	}
}

// A docs-only push is exempt from independent review before push; Ready never is.
func TestTheDocsOnlyPushExceptionNeverApplies(t *testing.T) {
	in := goodReady()
	in.Review = nil
	if r := EvaluateReady(in); r.Ready() {
		t.Fatal("Ready without an independent review of the exact current head")
	}
}

func TestAnyAllowlistedIndependentReviewerTierCountsForReady(t *testing.T) {
	for _, id := range []string{"opus-reviewer", "sonnet-reviewer", "haiku-helper"} {
		in := goodReady()
		in.Review.ReviewerID = id
		if !EvaluateReady(in).Ready() {
			t.Errorf("%s: an allowlisted independent reviewer's CLEAR counts for Ready", id)
		}
	}
}
