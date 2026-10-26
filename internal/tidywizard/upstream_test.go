// SPDX-License-Identifier: EUPL-1.2

package tidywizard

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/wstein/sourcesteward/internal/workspace"
)

func upstreamObservation(state workspace.UpstreamState) workspace.WorktreeObservation {
	return workspace.WorktreeObservation{Branches: []workspace.BranchEvidence{{Name: "refs/heads/topic", OID: "a1", Proof: "ancestor", AuthorHint: "Ana", Upstream: state}}}
}

// Spec 044 T004: the fact travels from the observation to the branch candidate, is text only for
// a later renderer and never part of the JSON, and never changes a verdict or a proof.
func TestUpstreamGoneReachesTheBranchCandidateWithoutChangingTheOutput(t *testing.T) {
	var docs [][]byte
	for _, state := range []workspace.UpstreamState{workspace.UpstreamUnknown, workspace.UpstreamNotGone, workspace.UpstreamGone} {
		a, _ := testAdapter(upstreamObservation(state), nil)
		f, err := a.Facts(context.Background(), "r")
		if err != nil || len(f.Branches) != 1 || f.Branches[0].Upstream != state {
			t.Fatalf("%q: %+v %v", state, f.Branches, err)
		}
		res := Classify(context.Background(), a, []string{"r"}, Config{Caller: "werner", CWD: "/ws/r", Now: old.Add(RecentWindow * 4)})
		if len(res.Candidates) != 1 {
			t.Fatalf("%q: %+v", state, res.Candidates)
		}
		c := res.Candidates[0]
		if c.Upstream != state || c.Verdict != VerdictSafe || c.Proof != ProofAncestor || c.Evidence != "" {
			t.Fatalf("%q: %+v", state, c)
		}
		raw, err := json.Marshal(res.Candidates)
		if err != nil {
			t.Fatal(err)
		}
		docs = append(docs, raw)
	}
	if string(docs[0]) != string(docs[1]) || string(docs[1]) != string(docs[2]) {
		t.Fatalf("the JSON of the candidates depends on the fact:\n%s\n%s\n%s", docs[0], docs[1], docs[2])
	}
}

// A worktree, a tracking reference and a retained branch carry no upstream fact into the row.
func TestUpstreamGoneIsOnlyForBranchCandidates(t *testing.T) {
	obs := upstreamObservation(workspace.UpstreamGone)
	obs.Branches = append(obs.Branches, workspace.BranchEvidence{Name: "refs/heads/work", OID: "b2", Proof: "none", Ahead: 2, Upstream: workspace.UpstreamGone})
	a, _ := testAdapter(obs, nil)
	res := Classify(context.Background(), a, []string{"r"}, Config{Caller: "werner", CWD: "/ws/r", Now: old.Add(RecentWindow * 4)})
	for _, c := range res.Candidates {
		if c.Kind != KindBranch || c.Upstream != workspace.UpstreamGone {
			t.Fatalf("%+v", c)
		}
	}
	if len(res.Candidates) != 2 || res.Candidates[1].Verdict != VerdictRetain || res.Candidates[1].Reason != "unmerged" {
		t.Fatalf("%+v", res.Candidates)
	}
}
