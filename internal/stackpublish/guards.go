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
func (e Evaluation) Status() Status { return StatusUnknown }

// EvaluateApply admits the stored plan and evaluates its actions strictly one after another.
// Usage errors (offline, wrong plan, extra or duplicate authorization ids) return an error before
// anything is evaluated. The first action that is not prepared stops the run: the rest are
// not-started. A cancelled or exhausted budget starts nothing further.
func EvaluateApply(plan Plan, req ApplyRequest, caps CapabilitySet, budget *Budget, fresh FreshSource) (Evaluation, error) {
	return Evaluation{}, nil
}
