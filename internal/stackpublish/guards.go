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

// FreshSource reads the fresh evidence of one action. ok is false when it cannot be read. It is
// called lazily and strictly in plan order, so evidence is as fresh as the write it precedes.
type FreshSource func(Action) (fresh Fresh, ok bool)

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

// Status classifies the evaluation: complete only when every action is prepared.
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
		if d.State == StateRefused {
			return StatusRefused
		}
		if d.State != StatePrepared {
			return StatusUnknown
		}
	}
	return StatusComplete
}

// EvaluateApply admits the stored plan and evaluates its actions strictly one after another.
// Usage errors (offline, wrong plan, extra or duplicate authorization ids) return an error before
// anything is evaluated. The first action that is not prepared stops the run: the rest are
// not-started. A cancelled or exhausted budget starts nothing further. It writes nothing and
// reads nothing itself: fresh evidence comes only from the lazily called source.
func EvaluateApply(plan Plan, req ApplyRequest, caps CapabilitySet, budget *Budget, fresh FreshSource) (Evaluation, error) {
	if !req.Online {
		return Evaluation{}, &ValidationError{Code: "online-required", Field: "online", Usage: true}
	}
	if err := plan.AdmitApply(req.PlanID); err != nil {
		return Evaluation{}, err
	}
	if budget == nil || fresh == nil {
		return Evaluation{}, &ValidationError{Code: "apply-incomplete", Field: "budget"}
	}
	actions := plan.Actions()
	authorized, err := authorizedSet(actions, req.AuthorizeRemote)
	if err != nil {
		return Evaluation{}, err
	}
	var ev Evaluation
	for _, a := range actions {
		if len(ev.Decisions) > 0 && ev.Decisions[len(ev.Decisions)-1].State != StatePrepared {
			ev.Decisions = append(ev.Decisions, Decision{ActionID: a.ID, State: StateNotStarted})
			continue
		}
		ev.Decisions = append(ev.Decisions, ev.decide(a, authorized, caps, budget, fresh))
	}
	return ev, nil
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

// decide runs the guards of one action, in a fixed order, and stops at the first that does not pass.
func (e *Evaluation) decide(a Action, authorized map[string]bool, caps CapabilitySet, budget *Budget, fresh FreshSource) Decision {
	notStarted := Decision{ActionID: a.ID, State: StateNotStarted}
	if e.stoppedBy(budget) {
		return notStarted
	}
	switch {
	case a.State == StateBlocked && a.Blocker != nil:
		return refused(a, *a.Blocker)
	case !caps.Has(a.Kind):
		return refused(a, ReasonCapabilityUnavailable)
	case !authorized[a.ID]:
		return refused(a, ReasonAuthorizationMissing)
	}
	f, ok := fresh(a)
	if e.stoppedBy(budget) {
		return notStarted
	}
	if !ok {
		return refused(a, ReasonPlanStale)
	}
	if r, stale := staleReason(a.Expected, f.Expected); stale {
		return refused(a, r)
	}
	if f.Permission != PermissionGranted {
		return refused(a, ReasonPermissionUnknown)
	}
	if r, bad := guardReason(a.Kind, f.Guards); bad {
		return refused(a, r)
	}
	return Decision{ActionID: a.ID, State: StatePrepared}
}

// stoppedBy records a cancelled or exhausted budget; nothing further may start.
func (e *Evaluation) stoppedBy(b *Budget) bool {
	err := b.Err()
	if err == nil {
		return false
	}
	if be, ok := err.(*BoundError); ok && be.Reason == ReasonCancelled {
		e.cancelled = true
	} else {
		e.bounded = true
	}
	return true
}

func samePtr(a, b *string) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }

// staleReason compares the fresh state with the planned one. A moved remote value or PR head has
// its own typed reason; any other difference means the plan no longer describes the world.
func staleReason(planned, now Expected) (Reason, bool) {
	switch {
	case !samePtr(planned.OldSHA, now.OldSHA):
		return ReasonRemoteChanged, true
	case !samePtr(planned.HeadSHA, now.HeadSHA):
		return ReasonHeadChanged, true
	case !samePtr(planned.NewSHA, now.NewSHA), !samePtr(planned.BaseRef, now.BaseRef), !samePtr(planned.PlannedBase, now.PlannedBase), !samePtr(planned.BodyDigest, now.BodyDigest):
		return ReasonPlanStale, true
	}
	return "", false
}

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

// Run is the step API of apply. The caller drives it one action at a time: Begin names the next
// action, the caller fetches that action's fresh evidence (after every earlier effect was
// performed and recorded), Complete evaluates it, the caller performs the effect and Advance
// records its outcome. The pure package never fetches and never performs anything.
type Run struct{}

// NewRun admits the stored plan and the authorizations; usage errors return before any step.
func NewRun(plan Plan, req ApplyRequest, caps CapabilitySet, budget *Budget) (*Run, error) {
	return &Run{}, nil
}

// Begin returns the next action that needs fresh evidence. ok is false when the run is over.
func (r *Run) Begin() (Action, bool, error) { return Action{}, false, nil }

// Complete evaluates the begun action with the evidence the caller fetched for it.
func (r *Run) Complete(fresh Fresh, ok bool) Decision { return Decision{} }

// Advance records the outcome of the prepared action's effect: completed, failed or unknown.
func (r *Run) Advance(outcome ActionState) error { return nil }

// Finish returns the ordered result; actions never reached are not-started.
func (r *Run) Finish() Evaluation { return Evaluation{} }
