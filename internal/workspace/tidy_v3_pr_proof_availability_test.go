// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"testing"
)

// Issue 387: the plan says whether the merged-pull-request proof (wire word github-merged-head)
// was available. Offline it is unavailable and the forge-free proofs (patch-id among them) stay.
func TestTidyV3OfflinePlanReportsThePullRequestProofUnavailable(t *testing.T) {
	dir, _, expected, _, _ := fixtureTidyV3Merged(t)
	r := defaultTidyV3RefsCoordinator().plan(context.Background(), V3PlanOptions{Path: dir, Base: expected.BaseSelector, StateDir: privateDir(t), ProtectWorktrees: []string{}})
	if r.Status != "ok" || r.Plan == nil || validateV3Result(r) != nil {
		t.Fatalf("plan: %+v", r.Diagnostics)
	}
	pol := r.Plan.ProofPolicy
	if pol == nil || pol.PRProof != "unavailable-offline" || !pol.has("patch-id") || r.Boundary.NetworkAttempted {
		t.Fatalf("offline policy: %+v", pol)
	}
	for _, c := range r.Plan.Candidates {
		if c.Proof == "github-merged-head" {
			t.Fatalf("offline plan used the pull request proof: %+v", c)
		}
	}
}

func TestTidyV3OnlinePlanReportsThePullRequestProofAvailable(t *testing.T) {
	dir, _, expected, repo, rows := fixtureTidyV3Merged(t)
	c, _ := fixtureTidyV3Online(t, dir, repo, rows)
	r := c.plan(context.Background(), V3PlanOptions{Path: dir, Base: expected.BaseSelector, Online: true, Remote: "origin", IntegrationProofs: []string{"ancestor"}, StateDir: privateDir(t), ProtectWorktrees: []string{}})
	if r.Status != "ok" || r.Plan == nil || validateV3Result(r) != nil {
		t.Fatalf("plan: %+v", r.Diagnostics)
	}
	if pol := r.Plan.ProofPolicy; pol == nil || pol.PRProof != "available" {
		t.Fatalf("online policy: %+v", pol)
	}
	// An online plan cannot claim the proof was unavailable.
	forged := *r.Plan
	pol := *forged.ProofPolicy
	pol.PRProof = "unavailable-offline"
	forged.ProofPolicy = &pol
	forged.Digest, _ = v3PlanDigest(forged)
	if validateV3Plan(forged, forged.StateDir, forged.PlanID) == nil {
		t.Error("an online plan that claims the pull request proof unavailable was accepted")
	}
	merged := 0
	for _, c := range r.Plan.Candidates {
		if c.Decision == "delete" && c.Proof == "github-merged-head" && c.Evidence != nil && c.Evidence.GitHub != nil && c.Evidence.GitHub.Head.OID == c.OID {
			merged++
		}
	}
	if merged == 0 {
		t.Fatal("no branch proven by the merged pull request head")
	}
}

// The recorded availability must agree with the plan's online selection, and only a reference plan has one.
func TestTidyV3PullRequestProofAvailabilityIsValidated(t *testing.T) {
	dir, _, expected, _, _ := fixtureTidyV3Merged(t)
	r := defaultTidyV3RefsCoordinator().plan(context.Background(), V3PlanOptions{Path: dir, Base: expected.BaseSelector, StateDir: privateDir(t), ProtectWorktrees: []string{}})
	if r.Plan == nil {
		t.Fatal(r.Diagnostics)
	}
	for name, value := range map[string]string{"online claimed offline": "available", "unknown word": "maybe"} {
		p := *r.Plan
		pol := *p.ProofPolicy
		pol.PRProof = value
		p.ProofPolicy = &pol
		p.Digest, _ = v3PlanDigest(p)
		if validateV3Plan(p, p.StateDir, p.PlanID) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	p := *r.Plan
	pol := *p.ProofPolicy
	pol.PRProof = ""
	p.ProofPolicy = &pol
	p.Digest, _ = v3PlanDigest(p)
	if err := validateV3Plan(p, p.StateDir, p.PlanID); err != nil {
		t.Errorf("a plan that predates the field is rejected: %v", err)
	}
}

// Issue 387 acceptance: a branch whose tip equals the head of a merged pull request is deleted
// with the pull request proof; a branch of the same name at a different tip is retained.
func TestTidyV3MergedPullRequestHeadProvesOnlyTheExactTip(t *testing.T) {
	dir, _, expected, repo, rows := fixtureTidyV3Merged(t)
	tree := gitFixture(t, dir, "rev-parse", "HEAD^{tree}")
	root := gitFixture(t, dir, "rev-parse", "refs/heads/topic-x^")
	moved := gitFixture(t, dir, "commit-tree", tree, "-p", root, "-m", "amended after the merge")
	gitFixture(t, dir, "update-ref", "refs/heads/topic-xx", moved)
	c, _ := fixtureTidyV3Online(t, dir, repo, rows)
	r := c.plan(context.Background(), V3PlanOptions{Path: dir, Base: expected.BaseSelector, Online: true, Remote: "origin", IntegrationProofs: []string{"ancestor"}, ProtectWorktrees: []string{}})
	if r.Status != "ok" || r.Plan == nil {
		t.Fatalf("plan: %+v", r.Diagnostics)
	}
	exact := candidateNamed(t, r.Plan.Candidates, "refs/heads/topic-x")
	if exact.Decision != "delete" || exact.Proof != "github-merged-head" {
		t.Fatalf("exact merged head: %+v", exact)
	}
	other := candidateNamed(t, r.Plan.Candidates, "refs/heads/topic-xx")
	if other.Decision != "retain" || other.Proof == "github-merged-head" {
		t.Fatalf("same name, different tip than the merged head: %+v", other)
	}
}
