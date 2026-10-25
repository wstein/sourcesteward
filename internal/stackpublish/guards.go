// SPDX-License-Identifier: EUPL-1.2

package stackpublish

// Names of the guards every apply requires. Ownership applies to every kind; the Spec 042 D5
// attribution gate applies to push-topic and ready.
const (
	GuardOwnership   = "ownership"
	GuardAttribution = "attribution"
)

// ApplyRequest is what the caller asked apply to do. LocalAuthorize is the established local
// --authorize flag: it is carried only so that it can be shown to grant nothing remote.
type ApplyRequest struct {
	PlanID          string
	Online          bool
	AuthorizeRemote []string
	LocalAuthorize  bool
}

// Permission is the fresh remote permission fact for one action.
type Permission string

// Permissions. Anything but PermissionGranted refuses.
const (
	PermissionGranted Permission = "granted"
	PermissionDenied  Permission = "denied"
	PermissionUnknown Permission = "unknown"
)

// Fresh is the evidence read immediately before one action: its current expected state, the
// permission and the guards the callers computed from it.
type Fresh struct {
	Expected   Expected
	Permission Permission
	Guards     []Guard
}

// Decision is the verdict on one action. Prepared means every guard passed and the durable
// intent may be recorded next; nothing here has written anything.
type Decision struct {
	ActionID string
	State    ActionState
	Reason   *Reason
}

// Evaluation is the ordered result of the sequential guard evaluation.
type Evaluation struct {
	Decisions []Decision
	cancelled bool
	bounded   bool
}

// Status classifies the evaluation: complete only when every action's effect completed.
func (e Evaluation) Status() Status {
	switch {
	case e.cancelled:
		return StatusCancelled
	case e.bounded:
		return StatusIncomplete
	case len(e.Decisions) == 0:
		return StatusUnknown
	}
	for _, d := range e.Decisions {
		switch d.State {
		case StateRefused:
			return StatusRefused
		case StateFailed:
			return StatusFailed
		case StateUnknown:
			return StatusUnknown
		case StateCompleted:
		default:
			return StatusUnknown
		}
	}
	return StatusComplete
}

// authorizedSet turns the remote authorizations into a set. An authorization names an action of
// this exact plan once; an old-plan, extra, repeated or empty id is a usage error.
func authorizedSet(actions []Action, ids []string) (map[string]bool, error) {
	known := map[string]bool{}
	for _, a := range actions {
		known[a.ID] = true
	}
	set := map[string]bool{}
	for _, id := range ids {
		switch {
		case set[id]:
			return nil, &ValidationError{Code: "authorization-duplicate", Field: "authorize_remote", Usage: true}
		case !known[id]:
			return nil, &ValidationError{Code: "authorization-unknown-action", Field: "authorize_remote", Usage: true}
		}
		set[id] = true
	}
	return set, nil
}

func refused(a Action, r Reason) Decision {
	return Decision{ActionID: a.ID, State: StateRefused, Reason: &r}
}

// Run is the step API of apply. The caller drives it one action at a time: Begin names the next
// action, the caller fetches that action's fresh evidence (after every earlier effect was
// performed and recorded), Complete evaluates it, the caller performs the effect and Advance
// records its outcome. The pure package never fetches and never performs anything.
type Run struct {
	actions    []Action
	authorized map[string]bool
	caps       CapabilitySet
	budget     *Budget
	next       int
	current    *Action // begun, awaiting Complete
	prepared   bool    // completed as prepared, awaiting Advance
	stopped    bool
	finished   bool // sealed by Finish
	eval       Evaluation
}

// NewRun admits the stored plan and the authorizations; usage errors return before any step.
func NewRun(plan Plan, req ApplyRequest, caps CapabilitySet, budget *Budget) (*Run, error) {
	if !req.Online {
		return nil, &ValidationError{Code: "online-required", Field: "online", Usage: true}
	}
	if err := plan.AdmitApply(req.PlanID); err != nil {
		return nil, err
	}
	if budget == nil {
		return nil, &ValidationError{Code: "apply-incomplete", Field: "budget"}
	}
	actions := plan.Actions()
	authorized, err := authorizedSet(actions, req.AuthorizeRemote)
	if err != nil {
		return nil, err
	}
	return &Run{actions: actions, authorized: authorized, caps: caps, budget: budget}, nil
}

func (r *Run) record(d Decision) Decision {
	r.eval.Decisions = append(r.eval.Decisions, d)
	if d.State != StatePrepared {
		r.stopped = true
	}
	return d
}

// stoppedByBudget records a cancelled or exhausted budget; nothing further may start.
func (r *Run) stoppedByBudget() bool {
	err := r.budget.Err()
	if err == nil {
		return false
	}
	if be, ok := err.(*BoundError); ok && be.Reason == ReasonCancelled {
		r.eval.cancelled = true
	} else {
		r.eval.bounded = true
	}
	r.stopped = true
	return true
}

// Begin returns the next action that needs fresh evidence. ok is false when the run is over,
// including when the next action was refused without evidence (blocked, capability, authorization)
// or the budget stopped it. It is an error to begin while an action awaits Complete or Advance:
// the previous effect must be recorded first.
func (r *Run) Begin() (Action, bool, error) {
	if r.finished || r.current != nil || r.prepared {
		return Action{}, false, &ValidationError{Code: "step-out-of-order", Field: "begin"}
	}
	if r.stopped || r.next >= len(r.actions) {
		return Action{}, false, nil
	}
	a := r.actions[r.next]
	r.next++
	if r.stoppedByBudget() {
		r.eval.Decisions = append(r.eval.Decisions, Decision{ActionID: a.ID, State: StateNotStarted})
		return Action{}, false, nil
	}
	switch {
	case a.State == StateBlocked && a.Blocker != nil:
		r.record(refused(a, *a.Blocker))
		return Action{}, false, nil
	case !r.caps.Has(a.Kind):
		r.record(refused(a, ReasonCapabilityUnavailable))
		return Action{}, false, nil
	case !r.authorized[a.ID]:
		r.record(refused(a, ReasonAuthorizationMissing))
		return Action{}, false, nil
	}
	r.current = &a
	return cloneAction(a), true, nil
}

// Complete evaluates the begun action with the evidence the caller fetched for it. An action
// that cannot be evaluated (no begun action) is never prepared.
func (r *Run) Complete(f Fresh, ok bool) Decision {
	if r.finished || r.current == nil {
		return Decision{State: StateRefused, Reason: reasonRef(ReasonPlanStale)}
	}
	a := *r.current
	r.current = nil
	if r.stoppedByBudget() {
		return r.record(Decision{ActionID: a.ID, State: StateNotStarted})
	}
	switch {
	case !ok:
		return r.record(refused(a, ReasonPlanStale))
	case staleCheck(a.Expected, f.Expected) != "":
		return r.record(refused(a, staleCheck(a.Expected, f.Expected)))
	case f.Permission != PermissionGranted:
		return r.record(refused(a, ReasonPermissionUnknown))
	}
	if reason, bad := guardReason(a.Kind, f.Guards); bad {
		return r.record(refused(a, reason))
	}
	r.prepared = true
	return r.record(Decision{ActionID: a.ID, State: StatePrepared})
}

// Advance records the outcome of the prepared action's effect: completed, failed or unknown. Any
// outcome but completed stops the run; the truth of the outcome is kept, never upgraded.
func (r *Run) Advance(outcome ActionState) error {
	if r.finished || !r.prepared {
		return &ValidationError{Code: "step-out-of-order", Field: "advance"}
	}
	switch outcome {
	case StateCompleted:
	case StateFailed, StateUnknown:
		r.stopped = true
	default:
		return &ValidationError{Code: "outcome-invalid", Field: "advance"}
	}
	r.prepared = false
	r.eval.Decisions[len(r.eval.Decisions)-1].State = outcome
	return nil
}

// Finish seals the run (every later step is refused) and returns the ordered result; actions
// never reached are not-started. A Run is for one goroutine: the caller serializes the steps.
func (r *Run) Finish() Evaluation {
	r.finished = true
	out := Evaluation{cancelled: r.eval.cancelled, bounded: r.eval.bounded, Decisions: append([]Decision{}, r.eval.Decisions...)}
	for i := len(out.Decisions); i < len(r.actions); i++ {
		out.Decisions = append(out.Decisions, Decision{ActionID: r.actions[i].ID, State: StateNotStarted})
	}
	return out
}

func samePtr(a, b *string) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }

// staleCheck compares the fresh state with the planned one. A moved remote value or PR head has
// its own typed reason; any other difference means the plan no longer describes the world.
func staleCheck(planned, now Expected) Reason {
	switch {
	case !samePtr(planned.OldSHA, now.OldSHA):
		return ReasonRemoteChanged
	case !samePtr(planned.HeadSHA, now.HeadSHA):
		return ReasonHeadChanged
	case !samePtr(planned.NewSHA, now.NewSHA), !samePtr(planned.BaseRef, now.BaseRef), !samePtr(planned.PlannedBase, now.PlannedBase), !samePtr(planned.BodyDigest, now.BodyDigest):
		return ReasonPlanStale
	}
	return ""
}

func reasonRef(r Reason) *Reason { return &r }

// requiredGuards are the guards that must be present and passing for every kind.
func requiredGuards(k ActionKind) []struct {
	name   string
	reason Reason
} {
	type req = struct {
		name   string
		reason Reason
	}
	out := []req{{GuardOwnership, ReasonOwnershipUnknown}}
	if k == KindPushTopic || k == KindReady {
		out = append(out, req{GuardAttribution, ReasonAttributionUnknown})
	}
	return out
}

// guardReason returns the refusal of the first guard that is malformed, failed or unknown, then
// of the first required guard that is absent. Unknown relevant evidence never passes.
func guardReason(k ActionKind, gs []Guard) (Reason, bool) {
	passed := map[string]bool{}
	for _, g := range gs {
		if validGuard(g, "guard") != nil {
			return ReasonPlanStale, true
		}
		if g.Result == GuardFail || g.Result == GuardUnknown {
			return g.Reason, true
		}
		if g.Result == GuardPass {
			passed[g.Name] = true
		}
	}
	for _, r := range requiredGuards(k) {
		if !passed[r.name] {
			return r.reason, true
		}
	}
	return "", false
}
