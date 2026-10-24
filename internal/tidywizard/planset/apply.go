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
	// CodeBranchLegMismatch refuses the whole set before any effect: the branch the owner chose to
	// delete is not the one the retirement plan would delete, or the choice is ambiguous.
	CodeBranchLegMismatch = "branch-leg-mismatch"
	// CodeBranchLegAbsent is the code of a skipped leg: the owner chose a branch deletion but the
	// retirement plan has no leg (for example HEAD was attached to the base branch).
	CodeBranchLegAbsent = "branch-leg-absent"
)

const replayAction = "replay-completed-retirement"

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
	// Mode is the mode evidence of this run; the applier records it in the journal.
	Mode owner.Evidence
}

// runMode converts the evidence of the run to the form the journal records.
func runMode(e owner.Evidence) *workspace.V3RunMode {
	return &workspace.V3RunMode{Mode: string(e.Mode), StdinTTY: e.StdinTTY, StdoutTTY: e.StdoutTTY, Markers: append([]string{}, e.Markers...), Mine: e.Mine, ByOwner: append([]string{}, e.ByOwner...), Caller: e.Caller, CallerSource: e.CallerSource}
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

// LegChecker is an optional interface of an Applier: before the first effect of a set it checks
// that the branch deletion the owner chose with a worktree is the one the retirement plan's leg
// would perform. A returned error refuses the whole set.
type LegChecker interface {
	CheckBranchLeg(ctx context.Context, step Step) error
}

// V3Applier is the Applier over workspace.ApplyTidyV3. LegOf and LegFunc default to
// workspace.TidyV3BranchLeg and workspace.ApplyTidyV3BranchLeg.
type V3Applier struct {
	ApplyFunc func(context.Context, workspace.V3ApplyOptions) workspace.V3Result
	LegOf     func(workspace.V3Plan) (workspace.V3BranchLeg, bool)
	LegFunc   func(context.Context, workspace.V3BranchLeg, workspace.V3Result) workspace.V3Result
	// ReadFunc reads a stored retirement plan and its completion; default
	// workspace.ReadTidyV3Retirement.
	ReadFunc func(ctx context.Context, stateDir, planID string) (workspace.V3StoredRetirement, error)
}

func (a V3Applier) read() func(context.Context, string, string) (workspace.V3StoredRetirement, error) {
	if a.ReadFunc != nil {
		return a.ReadFunc
	}
	return workspace.ReadTidyV3Retirement
}

// chosenBranches returns the branch names the items chose to delete (effect delete-branch:NAME).
func chosenBranches(items []Item) []string {
	var out []string
	for _, i := range items {
		for _, e := range i.Effects {
			if name, ok := strings.CutPrefix(e, "delete-branch:"); ok {
				out = append(out, name)
			}
		}
	}
	return out
}

// CheckBranchLeg binds the leg to the chosen item: the retirement plan's leg must delete exactly
// the branch the owner chose, and the owner must have chosen at most one. A plan without a leg
// passes here; the missing leg is reported after the retirement.
func (a V3Applier) CheckBranchLeg(ctx context.Context, s Step) error {
	want := chosenBranches(s.Items)
	if len(want) == 0 {
		return nil
	}
	if len(want) != 1 {
		return fmt.Errorf("worktree %q chose %d branch deletions; exactly one is allowed", s.SubPlan.Worktree, len(want))
	}
	rt, err := a.read()(ctx, s.SubPlan.StateDir, s.SubPlan.ID)
	if err != nil {
		return fmt.Errorf("cannot read the retirement plan %s to check its branch deletion: %w", s.SubPlan.ID, err)
	}
	legOf := a.LegOf
	if legOf == nil {
		legOf = workspace.TidyV3BranchLeg
	}
	if leg, ok := legOf(rt.Plan); ok && leg.Ref != "refs/heads/"+want[0] {
		return fmt.Errorf("worktree %q: the owner chose to delete branch %q but the retirement plan would delete %q", s.SubPlan.Worktree, want[0], leg.Ref)
	}
	return nil
}

// ApplyBranchLeg derives the leg from the plan of the successful retirement result and applies it
// through the existing leg apply, which re-observes the branch and keeps every Spec 011 protection.
func (a V3Applier) ApplyBranchLeg(ctx context.Context, step Step, retirement workspace.V3Result) (workspace.V3Result, bool) {
	for _, act := range retirement.Actions {
		if act.Name == replayAction {
			return workspace.V3Result{Status: "ok", Actions: []workspace.ActionOutcome{{Name: "delete-branch", Status: "complete", Detail: "already deleted"}}}, true
		}
	}
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
	leg.RunMode = runMode(step.Mode)
	return run(ctx, leg, retirement), true
}

// Apply builds the apply options of the sub-plan kind and runs the existing apply. Authorization
// is given by the caller of Apply, which checked it for the whole set. A legacy completion repeats
// the declarations the set binds (owner and exact worktree); the protection set is empty, as the
// planner made it.
func (a V3Applier) Apply(ctx context.Context, s Step) workspace.V3Result {
	// A retirement that an earlier run completed, together with the branch deletion the owner chose
	// and that run also completed, is already applied. The ordinary apply would see the deleted
	// branch as a changed fact; a branch that is still there, or back at another tip, takes the
	// ordinary path and its stale detection.
	if s.SubPlan.Kind == KindRetire && hasBranchEffect(s.Items) {
		if rt, err := a.read()(ctx, s.SubPlan.StateDir, s.SubPlan.ID); err == nil && rt.Completed && rt.BranchGone {
			plan := rt.Plan
			return workspace.V3Result{Status: "ok", Plan: &plan, Actions: []workspace.ActionOutcome{{Name: replayAction, Status: "complete", Detail: "already applied; the branch was already deleted"}}}
		}
	}
	o := workspace.V3ApplyOptions{StateDir: s.SubPlan.StateDir, PlanID: s.SubPlan.ID, Authorize: true, ProtectWorktrees: []string{}, RunMode: runMode(s.Mode)}
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
	return applySteps(ctx, set, req.Applier, req.Mode)
}

func applySteps(ctx context.Context, set Set, applier Applier, mode owner.Evidence) ApplyResult {
	byID := map[string]Item{}
	for _, i := range set.Items {
		byID[i.ID] = i
	}
	if lc, ok := applier.(LegChecker); ok {
		for _, sp := range set.Ordered() {
			if sp.Kind != KindRetire {
				continue
			}
			step := Step{SubPlan: sp}
			for _, id := range sp.Items {
				step.Items = append(step.Items, byID[id])
			}
			if hasBranchEffect(step.Items) {
				if err := lc.CheckBranchLeg(ctx, step); err != nil {
					return refused(set.PlanID, CodeBranchLegMismatch, "%v", err)
				}
			}
		}
	}
	res := ApplyResult{SchemaVersion: 1, Command: "tidy-apply-set", PlanID: set.PlanID, Outcomes: []Outcome{}}
	applied := 0
	for _, sp := range set.Ordered() {
		o := Outcome{SubPlan: sp.ID, Repo: sp.Repo, Kind: sp.Kind, Worktree: sp.Worktree}
		step := Step{SubPlan: sp, Mode: mode}
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
	case retire.Status == StatusSkipped && retire.Code == "cancelled":
		o.Status, o.Code, o.Message = StatusSkipped, "cancelled", "the run was cancelled before the retirement this branch deletion depends on"
		return o, true
	case retire.Status != StatusApplied:
		o.Status, o.Code, o.Message = StatusSkipped, "retirement-not-applied", "the retirement this branch deletion depends on did not succeed; the branch was kept"
		return o, true
	case ctx.Err() != nil:
		o.Status, o.Code, o.Message = StatusSkipped, "cancelled", "the run was cancelled before the branch deletion"
		return o, true
	}
	r, has := la.ApplyBranchLeg(ctx, step, result)
	if !has {
		o.Status, o.Code, o.Message = StatusSkipped, CodeBranchLegAbsent, "the retirement plan has no branch deletion for the branch the owner chose; the branch was kept"
		return o, true
	}
	if r.Status == "ok" {
		o.Status = StatusApplied
	} else {
		o.Status, o.Code, o.Message = failureOf(r)
	}
	return o, true
}
