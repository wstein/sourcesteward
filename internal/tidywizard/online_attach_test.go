// SPDX-License-Identifier: EUPL-1.2

package tidywizard

import (
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard/risk"
	"github.com/wstein/sourcesteward/internal/workspace"
)

// The merged pull request of a tip reaches the candidate as its number (Spec 044 C2); anything
// else fails closed: no number, no upgrade.
func TestAttachPRSetsOnlyAMergedSameTipNumber(t *testing.T) {
	br := func(repo, name, tip string, v Verdict, reason string) Candidate {
		c := Candidate{ID: "branch:" + repo + ":" + name, Repo: repo, Kind: KindBranch, Verdict: v, Reason: reason, Proof: ProofPatchID, Tip: tip}
		if v == VerdictRetain {
			c.Proof = ProofNone
		}
		return c
	}
	cands := []Candidate{
		br("a", "merged", "t1", VerdictSafe, ""),
		br("a", "closed", "t2", VerdictSafe, ""),
		br("a", "unknown", "t3", VerdictSafe, ""),
		br("a", "unasked", "t4", VerdictSafe, ""),
		br("b", "other-repo", "t1", VerdictSafe, ""),
		br("a", "no-tip", "", VerdictSafe, ""),
		br("a", "unmerged-but-merged-head", "t5", VerdictRetain, "unmerged"),
		br("a", "zero-number", "t6", VerdictSafe, ""),
	}
	out := OnlineOutcome{Facts: map[TipKey]PREvidence{
		{Repo: "a", Tip: "t1"}: {State: PRMerged, Number: 7},
		{Repo: "a", Tip: "t2"}: {State: PRClosedUnmerged},
		{Repo: "a", Tip: "t3"}: {State: PRUnknown},
		{Repo: "a", Tip: "t5"}: {State: PRMerged, Number: 9},
		{Repo: "a", Tip: "t6"}: {State: PRMerged},
	}}
	out.AttachPR(cands)
	want := map[string]int{"merged": 7, "unmerged-but-merged-head": 9}
	for _, c := range cands {
		name := c.ID[len("branch:a:"):]
		if c.Repo == "b" {
			name = "other-repo"
		}
		if c.PR != want[name] {
			t.Errorf("%s: PR %d, want %d", name, c.PR, want[name])
		}
	}
	now := now
	// The ladder: the merged head makes a safe equivalent row green; a retained unmerged row stays red.
	if a := risk.Evaluate(RiskFacts(cands[0], cands[0].PR), now); a.Level != risk.Green || a.Reasons[0] != risk.ReasonPRMergedHead {
		t.Errorf("merged: %+v", a)
	}
	if a := risk.Evaluate(RiskFacts(cands[1], cands[1].PR), now); a.Level != risk.Yellow {
		t.Errorf("closed: %+v", a)
	}
	if a := risk.Evaluate(RiskFacts(cands[6], cands[6].PR), now); a.Level != risk.Red {
		t.Errorf("unmerged with a merged head must stay red: %+v", a)
	}
	// An empty outcome (offline) changes nothing.
	off := []Candidate{br("a", "merged", "t1", VerdictSafe, "")}
	OnlineOutcome{}.AttachPR(off)
	if off[0].PR != 0 {
		t.Error("an offline run set a number")
	}
}

// The upstream-gone fact of a branch reaches the ladder as corroboration: text only, no level.
func TestUpstreamGoneReachesTheLadderAsTextOnly(t *testing.T) {
	b := Candidate{Kind: KindBranch, Verdict: VerdictSafe, Proof: ProofPatchID, Upstream: workspace.UpstreamGone}
	a := risk.Evaluate(RiskFacts(b, 0), now)
	if a.Level != risk.Yellow || a.Reasons[len(a.Reasons)-1] != risk.ReasonUpstreamGone {
		t.Fatalf("%+v", a)
	}
	for _, u := range []workspace.UpstreamState{workspace.UpstreamUnknown, workspace.UpstreamNotGone} {
		b.Upstream = u
		if a := risk.Evaluate(RiskFacts(b, 0), now); len(a.Reasons) != 1 {
			t.Errorf("%q: %+v", u, a)
		}
	}
	// Only a branch carries it.
	w := Candidate{Kind: KindWorktree, Verdict: VerdictSafe, Proof: ProofPatchID, Upstream: workspace.UpstreamGone}
	if RiskFacts(w, 0).UpstreamGone {
		t.Error("a worktree has no upstream")
	}
}
