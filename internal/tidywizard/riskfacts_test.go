// SPDX-License-Identifier: EUPL-1.2

package tidywizard

import (
	"reflect"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/tidywizard/risk"
)

// riskOf classifies the facts like the wizard does and evaluates the risk of the one candidate.
func riskOf(t *testing.T, r RepoFacts) (Candidate, risk.Assessment) {
	t.Helper()
	c := only(t, r, cfg())
	return c, risk.Evaluate(RiskFacts(c, 0), now)
}

func TestRiskFactsMapEveryClassifierShape(t *testing.T) {
	long := now.Add(-100 * time.Hour)
	tree := func(f func(*Worktree)) RepoFacts {
		w := stwTree("a")
		w.LastActivity = long
		f(&w)
		return repo([]Worktree{w}, nil)
	}
	tests := []struct {
		name    string
		facts   RepoFacts
		level   risk.Level
		reasons []string
	}{
		{"own worktree, equal patches", tree(func(*Worktree) {}), risk.Yellow, []string{risk.ReasonProofEquivalent}},
		{"own worktree, ancestor", tree(func(w *Worktree) { w.Proof = ProofAncestor }), risk.Green, []string{risk.ReasonProofAncestor}},
		{"other owner with a record, old activity", tree(func(w *Worktree) { w.TaskOwner = "codex" }), risk.Yellow, []string{risk.ReasonProofEquivalent}},
		{"other owner, activity between 24 and 72 hours", tree(func(w *Worktree) { w.TaskOwner = "codex"; w.LastActivity = now.Add(-30 * time.Hour) }), risk.Orange, []string{risk.ReasonProofEquivalent, risk.ReasonRecentActivity}},
		{"unregistered worktree without own commit", tree(func(w *Worktree) {
			w.Lock = LockNone
			w.Record = RecordNone
			w.TaskOwner = ""
			w.Proof = ProofAncestor
			w.HasOwnCommit = false
		}), risk.Green, []string{risk.ReasonNoOwnCommit}},
		{"unregistered, nobody vouches", tree(func(w *Worktree) {
			w.Lock = LockNone
			w.Record = RecordNone
			w.TaskOwner = ""
			w.Path = "/somewhere/else"
		}), risk.Orange, []string{risk.ReasonProofEquivalent, risk.ReasonNobodyVouches}},
		{"decision row", tree(func(w *Worktree) { w.Record = RecordMismatch; w.TaskOwner = "" }), risk.Orange, []string{risk.ReasonProofEquivalent, risk.ReasonNobodyVouches, risk.ReasonDecisionRow}},
		{"unmerged worktree", tree(func(w *Worktree) { w.Proof = ProofNone; w.Ahead = 2 }), risk.Red, []string{risk.ReasonNotIntegrated}},
		{"dirty worktree", tree(func(w *Worktree) { w.Dirty = true }), risk.Red, []string{risk.ReasonUncommittedFiles}},
		{"ignored extras", tree(func(w *Worktree) { w.IgnoredExtras = true }), risk.Red, []string{risk.ReasonExtraFiles}},
		{"not judged", tree(func(w *Worktree) { w.Incomplete = "deadline-exceeded" }), risk.Red, []string{risk.ReasonNotJudged}},
		{"record missing, not integrated", tree(func(w *Worktree) { w.Record = RecordMissing; w.Proof = ProofNone }), risk.Red, []string{risk.ReasonNotIntegrated}},
		{"record missing, integrated", tree(func(w *Worktree) { w.Record = RecordMissing }), risk.Kept, []string{}},
		{"recently active, integrated", tree(func(w *Worktree) { w.TaskOwner = "codex"; w.LastActivity = now.Add(-time.Hour) }), risk.Kept, []string{}},
		{"primary checkout", tree(func(w *Worktree) { w.Primary = true; w.Proof = ProofNone }), risk.Kept, []string{}},
		{"stale registration", repo([]Worktree{{Path: "/ws/wt/gone", DirMissing: true, Lock: LockNone}}, nil), risk.Kept, []string{}},
		{"branch, ancestor", repo(nil, []Branch{{Name: "feat/b", Proof: ProofAncestor}}), risk.Green, []string{risk.ReasonProofAncestor}},
		{"branch, equal patches", repo(nil, []Branch{{Name: "feat/b", Proof: ProofPatchID}}), risk.Yellow, []string{risk.ReasonProofEquivalent}},
		{"branch, unmerged", repo(nil, []Branch{{Name: "feat/b", Proof: ProofNone, Ahead: 1}}), risk.Red, []string{risk.ReasonNotIntegrated}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, a := riskOf(t, tt.facts)
			if a.Level != tt.level || !reflect.DeepEqual(a.Reasons, tt.reasons) {
				t.Fatalf("got %s %v, want %s %v", a.Level, a.Reasons, tt.level, tt.reasons)
			}
		})
	}
}

func TestRiskFactsOfATrackingReferenceAndAMergedPullRequest(t *testing.T) {
	c := Candidate{ID: "tracking-ref:r:refs/remotes/origin/x", Repo: "r", Kind: KindTrackingRef, Verdict: VerdictSafe, Proof: ProofBranchAbsent, Scope: owner.ScopeUnknown, Effects: []string{"delete-tracking-ref:refs/remotes/origin/x"}}
	if a := risk.Evaluate(RiskFacts(c, 0), now); a.Level != risk.Green || !reflect.DeepEqual(a.Reasons, []string{risk.ReasonProofGitHubBranchAbsent}) {
		t.Fatalf("tracking reference: %+v", a)
	}
	b := Candidate{ID: "branch:r:b", Repo: "r", Kind: KindBranch, Verdict: VerdictSafe, Proof: ProofPatchID, Effects: []string{"delete-branch:b"}}
	if a := risk.Evaluate(RiskFacts(b, 0), now); a.Level != risk.Yellow {
		t.Fatalf("without a merged pull request: %+v", a)
	}
	if a := risk.Evaluate(RiskFacts(b, 17), now); a.Level != risk.Green || !reflect.DeepEqual(a.Reasons, []string{risk.ReasonPRMergedHead}) {
		t.Fatalf("a merged pull request: %+v", a)
	}
	// A merged pull request never lifts a retained row.
	u := Candidate{ID: "branch:r:u", Repo: "r", Kind: KindBranch, Verdict: VerdictRetain, Reason: "unmerged", Proof: ProofNone}
	if a := risk.Evaluate(RiskFacts(u, 17), now); a.Level != risk.Red {
		t.Fatalf("unmerged with a merged pull request must stay red: %+v", a)
	}
}

func TestRiskFactsCarryTheObservedFields(t *testing.T) {
	at := now.Add(-5 * time.Hour)
	c := Candidate{Kind: KindWorktree, Verdict: VerdictSafe, Reason: "unregistered", Evidence: "no-work", Proof: ProofAncestor, Scope: owner.ScopeOther, OwnerSource: owner.SourceTaskRecord, LastActivity: at}
	f := RiskFacts(c, 0)
	want := risk.Facts{Kind: "worktree", Verdict: "safe", Proof: "ancestor", Reason: "unregistered", Scope: owner.ScopeOther, OwnerSource: owner.SourceTaskRecord, HasCreationRecord: true, NoWork: true, LastActivity: at}
	if !reflect.DeepEqual(f, want) {
		t.Fatalf("facts\n got %+v\nwant %+v", f, want)
	}
	for reason, check := range map[string]func(risk.Facts) bool{
		"primary-checkout":    func(f risk.Facts) bool { return f.Primary },
		"current-worktree":    func(f risk.Facts) bool { return f.Primary },
		"dirty":               func(f risk.Facts) bool { return f.Dirty },
		"ignored-extras":      func(f risk.Facts) bool { return f.IgnoredExtras },
		"evidence-incomplete": func(f risk.Facts) bool { return f.Incomplete },
	} {
		if !check(RiskFacts(Candidate{Kind: KindWorktree, Verdict: VerdictRetain, Reason: reason}, 0)) {
			t.Errorf("reason %s sets no fact", reason)
		}
	}
	if !RiskFacts(Candidate{Kind: KindStaleWorktree, Verdict: VerdictRetain}, 0).DirectoryMissing {
		t.Error("a stale registration is a missing directory")
	}
	if RiskFacts(Candidate{Kind: KindWorktree, Verdict: VerdictRetain, Reason: "unmerged"}, 0).DirectoryMissing {
		t.Error("an existing directory is not missing")
	}
}
