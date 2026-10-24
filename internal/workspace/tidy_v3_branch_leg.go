// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"os"
	"strings"
)

// V3BranchLeg is the dependent sub-plan of a retirement whose HEAD was attached
// to a local branch (Spec 041 FR-025). It is derived from the stored retirement
// plan, which already binds the head reference and OID; it adds no plan field.
// The branch is attached while the retirement is planned, so Spec 011 reference
// planning would retain it: the leg runs only after the retirement succeeded,
// plans again against the then unattached branch and deletes it through the
// unchanged Spec 011 reference deletion with all its protections.
type V3BranchLeg struct {
	// PlanID is the retirement plan the leg depends on.
	PlanID               string
	SourcePath, StateDir string
	Base, Ref, TipOID    string
	// Destination is the retired worktree; Policy is the retirement's stored
	// integration policy, so branch and worktree are judged alike.
	Destination string
	Policy      *V3ProofPolicy
	// RunMode is recorded in the journal of the branch deletion (Spec 041); the caller sets it.
	RunMode *V3RunMode
}

// TidyV3BranchLeg returns the leg of an eligible retirement plan, or false when
// the plan retains the worktree, its HEAD is detached, or its HEAD is attached
// to the base branch itself.
func TidyV3BranchLeg(p V3Plan) (V3BranchLeg, bool) {
	r := p.Retirement
	if r == nil || !v3RetirementEligible(*r) || r.Target.HeadRef == "" || tidyV3IsBaseBranch(p, r.Target.HeadRef) {
		return V3BranchLeg{}, false
	}
	return V3BranchLeg{PlanID: p.PlanID, SourcePath: p.SourcePath, StateDir: p.StateDir, Base: p.Expected.BaseSelector, Ref: r.Target.HeadRef, TipOID: r.Target.CommitID, Destination: r.Destination.Path, Policy: p.ProofPolicy}, true
}

func tidyV3IsBaseBranch(p V3Plan, ref string) bool {
	selector := p.Expected.BaseSelector
	return ref == selector || selector == "HEAD" && ref == p.Expected.Identity.HeadRef
}

// ApplyTidyV3BranchLeg runs the leg after the retirement it depends on. A
// retirement that did not succeed skips it. The branch is re-observed: its tip
// must still be the planned OID and Spec 011 must still delete it.
func ApplyTidyV3BranchLeg(ctx context.Context, leg V3BranchLeg, retirement V3Result) V3Result {
	r := newV3Result("tidy-apply")
	r.Backend = "git"
	if _, gone := os.Lstat(leg.Destination); retirement.Status != "ok" || retirement.Plan == nil || retirement.Plan.PlanID != leg.PlanID || !os.IsNotExist(gone) {
		r.Status = "refused"
		r.Actions = []ActionOutcome{{Name: "delete-branch", Status: "skipped", Detail: "The retirement this branch depends on did not succeed; the branch " + leg.Ref + " was kept."}}
		return r
	}
	o := V3PlanOptions{Path: leg.SourcePath, Base: leg.Base, StateDir: leg.StateDir, Refs: []string{leg.Ref}, ProtectWorktrees: []string{}}
	if leg.Policy != nil {
		o.IntegrationProofs, o.AcceptWhitespaceDifferences = leg.Policy.Kinds, leg.Policy.AcceptWhitespaceDifferences
	}
	planned := PlanTidyV3(ctx, o)
	if planned.Status != "ok" || planned.Plan == nil {
		return planned
	}
	for _, c := range planned.Plan.Candidates {
		if c.Name != leg.Ref {
			continue
		}
		if c.OID != leg.TipOID {
			return failTidyV3(r, failure("evidence-changed", "The branch tip differs from the planned OID."))
		}
		if c.Decision != "delete" {
			r.Status = "refused"
			r.Diagnostics = []Diagnostic{{Code: c.Reason, Message: "The branch " + leg.Ref + " is retained by Spec 011 protection " + strings.ReplaceAll(c.Reason, "-", " ") + "."}}
			r.Actions = []ActionOutcome{{Name: "delete-branch", Status: "refused", Detail: "The branch was kept."}}
			return r
		}
		return ApplyTidyV3(ctx, V3ApplyOptions{StateDir: leg.StateDir, PlanID: planned.Plan.PlanID, Authorize: true, ProtectWorktrees: []string{}, RunMode: leg.RunMode})
	}
	return failTidyV3(r, failure("unknown-reference", "The planned branch no longer exists."))
}
