// SPDX-License-Identifier: EUPL-1.2
package risk

import (
	"reflect"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
)

func TestLadder(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	base := Facts{Kind: "worktree", Verdict: "safe", Proof: "ancestor", Scope: owner.ScopeOwn, HasCreationRecord: true}
	cases := []struct {
		name    string
		change  func(*Facts)
		level   Level
		reasons []string
	}{
		{"ancestor", func(*Facts) {}, Green, []string{"proof-ancestor"}},
		{"no net change", func(f *Facts) { f.Proof = "no-net-change" }, Green, []string{"proof-no-net-change"}},
		{"tracking branch absent", func(f *Facts) { f.Kind = "tracking-ref"; f.Proof = "github-branch-absent" }, Green, []string{"proof-github-branch-absent"}},
		{"no work", func(f *Facts) { f.NoWork = true }, Green, []string{"no-own-commit"}},
		{"merged head", func(f *Facts) { f.Proof = "patch-id"; f.MergedHead = true }, Green, []string{"pr-merged-head"}},
		{"patch id", func(f *Facts) { f.Proof = "patch-id" }, Yellow, []string{"proof-equivalent"}},
		{"merge noop", func(f *Facts) { f.Proof = "merge-noop" }, Yellow, []string{"proof-equivalent"}},
		{"gone changes no level", func(f *Facts) { f.Kind = "branch"; f.Proof = "patch-id"; f.UpstreamGone = true }, Yellow, []string{"proof-equivalent", "upstream-gone"}},
		{"gone keeps strong green", func(f *Facts) { f.Kind = "branch"; f.UpstreamGone = true }, Green, []string{"proof-ancestor", "upstream-gone"}},
		{"gone is branch only", func(f *Facts) { f.UpstreamGone = true }, Green, []string{"proof-ancestor"}},
		{"gone cannot prove", func(f *Facts) { f.Kind = "branch"; f.Verdict = "retain"; f.Proof = "none"; f.UpstreamGone = true }, Red, []string{"not-integrated", "upstream-gone"}},
		{"R1 strong", func(f *Facts) { f.Scope = owner.ScopeOther; f.LastActivity = now.Add(-48 * time.Hour) }, Yellow, []string{"proof-ancestor", "recent-activity"}},
		{"R1 equivalent", func(f *Facts) {
			f.Scope = owner.ScopeUnknown
			f.Proof = "patch-id"
			f.LastActivity = now.Add(-48 * time.Hour)
		}, Orange, []string{"proof-equivalent", "recent-activity"}},
		{"R1 own exempt", func(f *Facts) { f.LastActivity = now.Add(-48 * time.Hour) }, Green, []string{"proof-ancestor"}},
		{"R1 branch exempt", func(f *Facts) {
			f.Kind = "branch"
			f.Scope = owner.ScopeOther
			f.LastActivity = now.Add(-48 * time.Hour)
		}, Green, []string{"proof-ancestor"}},
		{"R2", func(f *Facts) {
			f.Proof = "patch-id"
			f.Scope = owner.ScopeUnknown
			f.HasCreationRecord = false
			f.LastActivity = now.Add(-73 * time.Hour)
		}, Orange, []string{"proof-equivalent", "nobody-vouches"}},
		{"R2 strong exempt", func(f *Facts) {
			f.Scope = owner.ScopeUnknown
			f.HasCreationRecord = false
			f.LastActivity = now.Add(-73 * time.Hour)
		}, Green, []string{"proof-ancestor"}},
		{"R2 path convention vouches", func(f *Facts) {
			f.Proof = "patch-id"
			f.Scope = owner.ScopeUnknown
			f.HasCreationRecord = false
			f.LastActivity = now.Add(-73 * time.Hour)
			f.OwnerSource = owner.SourcePathConvention
		}, Yellow, []string{"proof-equivalent"}},
		{"R2 branch exempt", func(f *Facts) {
			f.Kind = "branch"
			f.Proof = "patch-id"
			f.Scope = owner.ScopeUnknown
			f.HasCreationRecord = false
		}, Yellow, []string{"proof-equivalent"}},
		{"cap at orange", func(f *Facts) {
			f.Proof = "patch-id"
			f.Scope = owner.ScopeUnknown
			f.HasCreationRecord = false
			f.LastActivity = now.Add(-48 * time.Hour)
		}, Orange, []string{"proof-equivalent", "recent-activity", "nobody-vouches"}},
		{"R4 strong", func(f *Facts) { f.Verdict = "decision"; f.Reason = "lock-mismatch" }, Orange, []string{"proof-ancestor", "decision-row"}},
		{"R4 equivalent", func(f *Facts) { f.Verdict = "decision"; f.Proof = "merge-noop"; f.Reason = "lock-mismatch" }, Orange, []string{"proof-equivalent", "decision-row"}},
		{"record missing no proof", func(f *Facts) { f.Verdict = "retain"; f.Proof = "none"; f.Reason = "registration-record-missing" }, Red, []string{"not-integrated"}},
		{"not judged", func(f *Facts) { f.Verdict = "retain"; f.Reason = "evidence-incomplete" }, Red, []string{"not-judged"}},
		{"dirt under structural reason", func(f *Facts) { f.Verdict = "retain"; f.Reason = "registration-record-missing"; f.Dirty = true }, Red, []string{"uncommitted-files"}},
		{"extras under structural reason", func(f *Facts) { f.Verdict = "retain"; f.Reason = "locked-foreign"; f.IgnoredExtras = true }, Red, []string{"extra-files"}},
		{"incomplete under structural reason", func(f *Facts) { f.Verdict = "retain"; f.Reason = "protected"; f.Incomplete = true }, Red, []string{"not-judged"}},
		{"all red facts", func(f *Facts) {
			f.Verdict = "retain"
			f.Proof = "none"
			f.Dirty = true
			f.IgnoredExtras = true
			f.Incomplete = true
		}, Red, []string{"not-integrated", "uncommitted-files", "extra-files", "not-judged"}},
		{"dirty safe cannot be lifted", func(f *Facts) { f.Dirty = true; f.MergedHead = true }, Red, []string{"uncommitted-files"}},
		{"unmerged remains red with merged hint", func(f *Facts) { f.Verdict = "retain"; f.Proof = "none"; f.Reason = "unmerged"; f.MergedHead = true }, Red, []string{"not-integrated"}},
		{"dirty reason without exposed fact", func(f *Facts) { f.Verdict = "retain"; f.Reason = "dirty" }, Red, []string{"uncommitted-files"}},
		{"extras reason without exposed fact", func(f *Facts) { f.Verdict = "retain"; f.Reason = "ignored-extras" }, Red, []string{"extra-files"}},
		{"primary", func(f *Facts) { f.Verdict = "retain"; f.Proof = "none"; f.Primary = true }, Kept, []string{}},
		{"gone stale", func(f *Facts) {
			f.Kind = "stale-worktree"
			f.Verdict = "retain"
			f.Proof = "none"
			f.DirectoryMissing = true
		}, Kept, []string{}},
		{"integrated structural", func(f *Facts) { f.Verdict = "retain"; f.Reason = "registration-record-missing" }, Kept, []string{}},
		{"unknown proof", func(f *Facts) { f.Proof = "future-proof" }, Red, []string{"not-judged"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := base
			tc.change(&f)
			got := Evaluate(f, now)
			if got.Level != tc.level || !reflect.DeepEqual(got.Reasons, tc.reasons) {
				t.Fatalf("got %+v want %s %v", got, tc.level, tc.reasons)
			}
		})
	}
}

func TestRecentWindowBoundaries(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		age  time.Duration
		want Level
	}{
		{"24h minus one", 24*time.Hour - time.Nanosecond, Yellow}, {"24h", 24 * time.Hour, Yellow},
		{"24h plus one", 24*time.Hour + time.Nanosecond, Yellow}, {"72h", 72 * time.Hour, Yellow},
		{"72h plus one", 72*time.Hour + time.Nanosecond, Green}, {"future", -time.Hour, Yellow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Evaluate(Facts{Kind: "worktree", Verdict: "safe", Proof: "ancestor", Scope: owner.ScopeOther, LastActivity: now.Add(-tc.age)}, now)
			if got.Level != tc.want {
				t.Fatalf("age %s got %+v want %s", tc.age, got, tc.want)
			}
		})
	}
	f := Facts{Kind: "worktree", Verdict: "safe", Proof: "ancestor", Scope: owner.ScopeOther}
	if got := Evaluate(f, now); got.Level != Yellow {
		t.Fatalf("missing timestamp must not lower risk: %+v", got)
	}
}

func TestLadderRejectsUnrecognizedOrContradictoryFacts(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		facts  Facts
		reason string
	}{
		{"missing kind", Facts{Verdict: "safe", Proof: "ancestor"}, "not-judged"},
		{"typo kind", Facts{Kind: "Worktree", Verdict: "safe", Proof: "ancestor"}, "not-judged"},
		{"decision kind", Facts{Kind: "unknown", Verdict: "decision", Proof: "ancestor"}, "not-judged"},
		{"unknown proof merged", Facts{Kind: "branch", Verdict: "safe", Proof: "future", MergedHead: true}, "not-judged"},
		{"unknown proof no work", Facts{Kind: "worktree", Verdict: "safe", Proof: "future", NoWork: true}, "not-judged"},
		{"tracking none merged", Facts{Kind: "tracking-ref", Verdict: "safe", Proof: "none", MergedHead: true}, "not-integrated"},
		{"missing proof", Facts{Kind: "worktree", Verdict: "retain", Proof: ""}, "not-integrated"},
		{"retained typo none", Facts{Kind: "Worktree", Verdict: "retain", Proof: "none"}, "not-integrated"},
		{"no work cannot cancel retained none", Facts{Kind: "worktree", Verdict: "retain", Proof: "none", Reason: "registration-record-missing", NoWork: true}, "not-integrated"},
		{"no work cannot cancel safe none", Facts{Kind: "worktree", Verdict: "safe", Proof: "none", NoWork: true}, "not-integrated"},
		{"unmerged tracking with proof", Facts{Kind: "tracking-ref", Verdict: "retain", Proof: "ancestor", Reason: "unmerged"}, "not-integrated"},
		{"unmerged branch with proof", Facts{Kind: "branch", Verdict: "safe", Proof: "ancestor", Reason: "unmerged"}, "not-integrated"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Evaluate(tc.facts, now)
			if got.Level != Red || !reflect.DeepEqual(got.Reasons, []string{tc.reason}) {
				t.Fatalf("got %+v; want red/%s", got, tc.reason)
			}
		})
	}
	// An explicitly missing directory has no existing worktree contents to classify.
	got := Evaluate(Facts{Kind: "worktree", Verdict: "retain", Proof: "none", DirectoryMissing: true}, now)
	if got.Level != Kept || len(got.Reasons) != 0 {
		t.Fatalf("missing directory: %+v", got)
	}
}

func TestLadderUnknownScopeIsConservative(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	for _, scope := range []owner.Scope{"", "typo", owner.ScopeUnknown, owner.ScopeOther} {
		t.Run(string(scope), func(t *testing.T) {
			f := Facts{Kind: "worktree", Verdict: "safe", Proof: "ancestor", Scope: scope, LastActivity: now.Add(-30 * time.Hour)}
			if got := Evaluate(f, now); got.Level != Yellow || !reflect.DeepEqual(got.Reasons, []string{"proof-ancestor", "recent-activity"}) {
				t.Fatalf("recent: %+v", got)
			}
			f.Proof = "patch-id"
			f.LastActivity = now.Add(-73 * time.Hour)
			want := []string{"proof-equivalent", "nobody-vouches"}
			level := Orange
			if scope == owner.ScopeOther {
				want = []string{"proof-equivalent"}
				level = Yellow
			}
			if got := Evaluate(f, now); got.Level != level || !reflect.DeepEqual(got.Reasons, want) {
				t.Fatalf("equivalent: %+v, want %s/%v", got, level, want)
			}
		})
	}
}

func TestAllReasonsIsCompleteAndIndependent(t *testing.T) {
	want := []string{"proof-ancestor", "proof-no-net-change", "proof-github-branch-absent", "no-own-commit", "pr-merged-head", "proof-equivalent", "recent-activity", "nobody-vouches", "decision-row", "upstream-gone", "not-integrated", "uncommitted-files", "extra-files", "not-judged"}
	if got := AllReasons(); !reflect.DeepEqual(got, want) {
		t.Fatalf("catalog: %v want %v", got, want)
	}
	got := AllReasons()
	got[0] = "mutated"
	if !reflect.DeepEqual(AllReasons(), want) {
		t.Fatal("caller changed shared reason catalog")
	}
	// Each spelling is exported so P3 does not duplicate the package's literals.
	constants := []string{ReasonProofAncestor, ReasonProofNoNetChange, ReasonProofGitHubBranchAbsent, ReasonNoOwnCommit, ReasonPRMergedHead, ReasonProofEquivalent, ReasonRecentActivity, ReasonNobodyVouches, ReasonDecisionRow, ReasonUpstreamGone, ReasonNotIntegrated, ReasonUncommittedFiles, ReasonExtraFiles, ReasonNotJudged}
	if !reflect.DeepEqual(constants, want) {
		t.Fatalf("reason constants: %v", constants)
	}
}

func TestLadderConservativeBackendAndStructuralCases(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		facts   Facts
		level   Level
		reasons []string
	}{
		{"tracking proof none structural reason", Facts{Kind: "tracking-ref", Verdict: "retain", Proof: "none", Reason: "protected"}, Red, []string{"not-integrated"}},
		{"bare jj proof is unknown", Facts{Kind: "worktree", Verdict: "safe", Proof: "empty-working-copy", Scope: owner.ScopeOwn, HasCreationRecord: true}, Red, []string{"not-judged"}},
		{"bare jj proof cannot be lifted by no work", Facts{Kind: "worktree", Verdict: "safe", Proof: "empty-working-copy", NoWork: true}, Red, []string{"not-judged"}},
		{"bare jj proof cannot be lifted by merged head", Facts{Kind: "branch", Verdict: "decision", Proof: "empty-working-copy", MergedHead: true}, Red, []string{"not-judged"}},
		{"retained bare jj proof stays retained", Facts{Kind: "worktree", Verdict: "retain", Proof: "empty-working-copy", Reason: "registration-record-missing"}, Kept, []string{}},
		{"jj integrated parent proof is equivalent", Facts{Kind: "worktree", Verdict: "safe", Proof: "empty-working-copy-integrated-parent", Scope: owner.ScopeOwn, HasCreationRecord: true}, Yellow, []string{"proof-equivalent"}},
		{"retained unknown kind stays retained", Facts{Kind: "future-kind", Verdict: "retain", Proof: "ancestor", NoWork: true, MergedHead: true}, Kept, []string{}},
		{"retained unknown proof stays retained", Facts{Kind: "worktree", Verdict: "retain", Proof: "future-proof", Reason: "registration-record-missing", NoWork: true, MergedHead: true}, Kept, []string{}},
		{"retained jj stays retained", Facts{Kind: "worktree", Verdict: "retain", Proof: "empty-working-copy-integrated-parent", Reason: "jj-retirement-unsupported", Scope: owner.ScopeOwn}, Kept, []string{}},
		{"unrecognized verdict stays unselectable", Facts{Kind: "branch", Verdict: "sfae", Proof: "ancestor"}, Kept, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Evaluate(tc.facts, now)
			if got.Level != tc.level || !reflect.DeepEqual(got.Reasons, tc.reasons) {
				t.Fatalf("got %+v want %s/%v", got, tc.level, tc.reasons)
			}
		})
	}
}

func TestLadderClarificationTwoRetainsStructuralRows(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name  string
		facts Facts
	}{
		{"stale with incomplete evidence", Facts{Kind: "stale-worktree", Verdict: "retain", Proof: "none", DirectoryMissing: true, Incomplete: true}},
		{"primary with observed dirt", Facts{Kind: "worktree", Verdict: "retain", Proof: "none", Primary: true, Dirty: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Evaluate(tc.facts, now)
			if got.Level != Kept || !reflect.DeepEqual(got.Reasons, []string{}) {
				t.Fatalf("clarification 2 requires kept with no risk reasons, got %+v", got)
			}
		})
	}
}
