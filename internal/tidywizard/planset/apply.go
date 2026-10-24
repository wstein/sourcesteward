// SPDX-License-Identifier: EUPL-1.2

package planset

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/workspace"
)

// Refusal codes of Apply. A refusal happens before the first effect and changes nothing.
const (
	CodeAuthorizationRequired = "authorization-required"
	CodePlanSetNotFound       = "plan-set-not-found"
	CodePlanSetInvalid        = "plan-set-invalid"
	CodeScopeNotAuthorized    = "scope-not-authorized"
	CodeConfirmationRefused   = "confirmation-refused"
)

// Outcome statuses of one sub-plan.
const (
	StatusApplied = "applied"
	StatusStale   = "stale"
	StatusFailed  = "failed"
	StatusSkipped = "skipped"
)

// Step is one sub-plan with the selected items it covers, handed to the Applier.
type Step struct {
	SubPlan SubPlan
	Items   []Item
}

// Applier applies one stored sub-plan with the existing Spec 011/029/031 apply, which re-checks
// the evidence of the plan, records the journal and the recovery reference before its native
// effect and reports a changed fact as evidence-changed. It is the seam to internal/workspace.
type Applier interface {
	Apply(ctx context.Context, step Step) workspace.V3Result
}

// KindBranchLeg is the kind of the outcome of a dependent branch leg. It is not a sub-plan kind:
// the leg is derived from the stored retirement plan and adds nothing to the set.
const KindBranchLeg SubKind = "branch-leg"

// LegApplier is an optional interface of an Applier: after a successful retirement it applies the
// dependent branch leg of the retirement plan (Spec 041 FR-025). It reports false when the plan
// has no leg (for example HEAD was attached to the base branch).
type LegApplier interface {
	ApplyBranchLeg(ctx context.Context, step Step, retirement workspace.V3Result) (workspace.V3Result, bool)
}

// V3Applier is the Applier over workspace.ApplyTidyV3. LegOf and LegFunc default to
// workspace.TidyV3BranchLeg and workspace.ApplyTidyV3BranchLeg.
type V3Applier struct {
	ApplyFunc func(context.Context, workspace.V3ApplyOptions) workspace.V3Result
	LegOf     func(workspace.V3Plan) (workspace.V3BranchLeg, bool)
	LegFunc   func(context.Context, workspace.V3BranchLeg, workspace.V3Result) workspace.V3Result
}

// ApplyBranchLeg derives the leg from the plan of the successful retirement result and applies it
// through the existing leg apply, which re-observes the branch and keeps every Spec 011 protection.
func (a V3Applier) ApplyBranchLeg(ctx context.Context, _ Step, retirement workspace.V3Result) (workspace.V3Result, bool) {
	if retirement.Plan == nil {
		return workspace.V3Result{}, false
	}
	legOf, run := a.LegOf, a.LegFunc
	if legOf == nil {
		legOf = workspace.TidyV3BranchLeg
	}
	if run == nil {
		run = workspace.ApplyTidyV3BranchLeg
	}
	leg, ok := legOf(*retirement.Plan)
	if !ok {
		return workspace.V3Result{}, false
	}
	return run(ctx, leg, retirement), true
}

// Apply builds the apply options of the sub-plan kind and runs the existing apply. Authorization
// is given by the caller of Apply, which checked it for the whole set. A legacy completion repeats
// the declarations the set binds (owner and exact worktree); the protection set is empty, as the
// planner made it.
func (a V3Applier) Apply(ctx context.Context, s Step) workspace.V3Result {
	o := workspace.V3ApplyOptions{StateDir: s.SubPlan.StateDir, PlanID: s.SubPlan.ID, Authorize: true, ProtectWorktrees: []string{}}
	switch {
	case s.SubPlan.Kind == KindTracking:
		o.Online = true
	case s.SubPlan.Kind == KindRetire && len(s.Items) == 1 && (s.Items[0].Task == "" || s.Items[0].OwnerSource != owner.SourceTaskRecord):
		o.Owner, o.CompletedWorktree = s.Items[0].Owner, s.SubPlan.Worktree
	}
	return a.ApplyFunc(ctx, o)
}

// ApplyRequest asks for the application of a stored plan set. StateDirs are searched in order for
// the set. Authorize is the explicit authorization of this plan id (an answered prompt, or --yes
// with the id on the command line); it never stands for the typed confirmation of a set with
// authorized_scopes, which Confirm reads from the terminal. Mode is the evidence of this run.
type ApplyRequest struct {
	StateDirs []string
	PlanID    string
	Mode      owner.Evidence
	Authorize bool
	Confirm   owner.Opener
	Applier   Applier
}

// Outcome is the result of one sub-plan.
type Outcome struct {
	SubPlan  string  `json:"sub_plan"`
	Repo     string  `json:"repo"`
	Kind     SubKind `json:"kind"`
	Worktree string  `json:"worktree"`
	Status   string  `json:"status"`
	Code     string  `json:"code"`
	Message  string  `json:"message"`
}

// ApplyResult is the report of an application. Status is ok (every sub-plan applied), refused
// (a precondition failed, nothing changed), partial (some applied, some did not) or failed
// (none applied). Anything but ok is exit 1.
type ApplyResult struct {
	SchemaVersion int       `json:"schema_version"`
	Command       string    `json:"command"`
	Status        string    `json:"status"`
	Code          string    `json:"code"`
	Message       string    `json:"message"`
	PlanID        string    `json:"plan_id"`
	Outcomes      []Outcome `json:"outcomes"`
}

func refused(id, code, format string, args ...any) ApplyResult {
	return ApplyResult{SchemaVersion: 1, Command: "tidy-apply-set", Status: "refused", Code: code, Message: fmt.Sprintf(format, args...), PlanID: id, Outcomes: []Outcome{}}
}

func confirmWords(items []Item) []string {
	seen := map[string]bool{}
	for _, i := range items {
		if i.Scope != owner.ScopeOwn {
			seen[owner.ConfirmWord(owner.Owner{Label: i.Owner, Source: i.OwnerSource, Scope: i.Scope})] = true
		}
	}
	words := make([]string, 0, len(seen))
	for w := range seen {
		words = append(words, w)
	}
	sort.Strings(words)
	return words
}

// Find loads the plan set with that id from the first state directory that holds it. A set that
// is stored but invalid is an error, not a reason to look further.
func Find(stateDirs []string, id string) (Set, error) {
	for _, dir := range stateDirs {
		set, err := Load(dir, id)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		return set, err
	}
	return Set{}, ErrNotFound
}

// Apply applies a stored plan set one sub-plan at a time in the fixed order. It verifies the
// stored set (the id is recomputed from the content), refuses scope-not-authorized for an other
// or unknown item unless the human path wrote authorized_scopes for it, refuses an agent-mode
// apply of a set that carries authorized_scopes, asks for the typed confirmation of such a set,
// and otherwise needs Authorize. Each sub-plan is then applied by the existing apply, which
// re-checks its own evidence: a sub-plan that turned stale or failed is reported and the others
// continue. A sub-plan that was already completed replays as a verified no-op of that apply.
func Apply(ctx context.Context, req ApplyRequest) ApplyResult {
	set, err := Find(req.StateDirs, req.PlanID)
	switch {
	case errors.Is(err, ErrNotFound):
		return refused(req.PlanID, CodePlanSetNotFound, "no stored plan set %q in the known state directories", req.PlanID)
	case err != nil:
		return refused(req.PlanID, CodePlanSetInvalid, "the stored plan set %q is not valid: %v", req.PlanID, err)
	}
	if set.NeedsAuthorization() { // a set with authorized_scopes always has such an item (Validate)
		if req.Mode.Mode != owner.ModeHuman {
			return refused(set.PlanID, CodeScopeNotAuthorized, "only the human path applies an other or unknown item or a plan with authorized_scopes")
		}
		authorized := map[owner.Scope]bool{}
		for _, s := range set.AuthorizedScopes {
			authorized[s] = true
		}
		for _, i := range set.Items {
			if i.Scope != owner.ScopeOwn && !authorized[i.Scope] {
				return refused(set.PlanID, CodeScopeNotAuthorized, "item %s has scope %q and the plan carries no authorized_scopes for it", i.ID, i.Scope)
			}
		}
	}
	if set.NeedsAuthorization() {
		for _, w := range confirmWords(set.Items) {
			if req.Confirm == nil {
				return refused(set.PlanID, CodeConfirmationRefused, "typed confirmation %q failed: %v", w, owner.ErrNoTerminal)
			}
			if err := owner.Confirm(req.Confirm, w, set.PlanID); err != nil {
				return refused(set.PlanID, CodeConfirmationRefused, "typed confirmation %q failed: %v", w, err)
			}
		}
	} else if !req.Authorize {
		return refused(set.PlanID, CodeAuthorizationRequired, "applying a plan set needs the explicit authorization of its plan id")
	}
	return applySteps(ctx, set, req.Applier)
}

func applySteps(ctx context.Context, set Set, applier Applier) ApplyResult {
	byID := map[string]Item{}
	for _, i := range set.Items {
		byID[i.ID] = i
	}
	res := ApplyResult{SchemaVersion: 1, Command: "tidy-apply-set", PlanID: set.PlanID, Outcomes: []Outcome{}}
	applied := 0
	for _, sp := range set.Ordered() {
		o := Outcome{SubPlan: sp.ID, Repo: sp.Repo, Kind: sp.Kind, Worktree: sp.Worktree}
		step := Step{SubPlan: sp}
		for _, id := range sp.Items {
			step.Items = append(step.Items, byID[id])
		}
		if err := ctx.Err(); err != nil {
			o.Status, o.Code, o.Message = StatusSkipped, "cancelled", "the run was cancelled before this sub-plan"
			res.Outcomes = append(res.Outcomes, o)
			if sp.Kind == KindRetire && hasBranchEffect(step.Items) {
				if leg, ok := branchLegOutcome(ctx, applier, step, sp, o, workspace.V3Result{}); ok {
					res.Outcomes = append(res.Outcomes, leg)
				}
			}
			continue
		}
		r := applier.Apply(ctx, step)
		if r.Status == "ok" {
			o.Status = StatusApplied
			applied++
		} else {
			o.Status, o.Code, o.Message = failureOf(r)
		}
		res.Outcomes = append(res.Outcomes, o)
		if sp.Kind == KindRetire && hasBranchEffect(step.Items) {
			if leg, ok := branchLegOutcome(ctx, applier, step, sp, o, r); ok {
				if leg.Status == StatusApplied {
					applied++
				}
				res.Outcomes = append(res.Outcomes, leg)
			}
		}
	}
	switch {
	case applied == len(res.Outcomes):
		res.Status = "ok"
	case applied == 0:
		res.Status = "failed"
	default:
		res.Status = "partial"
	}
	return res
}

// failureOf classifies a failed apply: a changed fact is stale, anything else failed.
func failureOf(r workspace.V3Result) (status, code, message string) {
	code, message = r.Status, "the sub-plan was not applied"
	if len(r.Diagnostics) > 0 {
		code, message = r.Diagnostics[0].Code, r.Diagnostics[0].Message
	} else if len(r.Actions) > 0 {
		code, message = r.Actions[0].Status, r.Actions[0].Detail
	}
	if code == "evidence-changed" || strings.Contains(code, "stale") {
		return StatusStale, code, message
	}
	return StatusFailed, code, message
}

// hasBranchEffect reports whether the owner chose the deletion of the branch with the worktree.
func hasBranchEffect(items []Item) bool {
	for _, i := range items {
		for _, e := range i.Effects {
			if strings.HasPrefix(e, "delete-branch:") {
				return true
			}
		}
	}
	return false
}

// branchLegOutcome applies the dependent branch leg of a retirement that selected it. The leg runs
// only after, and only if, the retirement was applied; otherwise (or after cancellation) it is
// reported as skipped. It is not reported at all when the retirement plan has no leg.
func branchLegOutcome(ctx context.Context, applier Applier, step Step, sp SubPlan, retire Outcome, result workspace.V3Result) (Outcome, bool) {
	la, ok := applier.(LegApplier)
	if !ok {
		return Outcome{}, false
	}
	o := Outcome{SubPlan: sp.ID, Repo: sp.Repo, Kind: KindBranchLeg, Worktree: sp.Worktree}
	switch {
	case retire.Status != StatusApplied:
		o.Status, o.Code, o.Message = StatusSkipped, "retirement-not-applied", "the retirement this branch deletion depends on did not succeed; the branch was kept"
		return o, true
	case ctx.Err() != nil:
		o.Status, o.Code, o.Message = StatusSkipped, "cancelled", "the run was cancelled before the branch deletion"
		return o, true
	}
	r, has := la.ApplyBranchLeg(ctx, step, result)
	if !has {
		return Outcome{}, false
	}
	if r.Status == "ok" {
		o.Status = StatusApplied
	} else {
		o.Status, o.Code, o.Message = failureOf(r)
	}
	return o, true
}
