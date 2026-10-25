// SPDX-License-Identifier: EUPL-1.2

package stackpublish

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// Pinned identity of the UNPUBLISHED forge-effect-plan proposal (Spec 016 data model).
const (
	SchemaVersion       = 1
	KindForgeEffectPlan = "forge-effect-plan"
	OperationPlan       = "plan"
)

// ActionKind is the closed set of selectable forge effects. KindComment is the deferred original
// draft kind: it is recognized only to be refused, and can never execute.
type ActionKind string

// Action kinds.
const (
	KindPushTopic       ActionKind = "push-topic"
	KindCreatePR        ActionKind = "create-pr"
	KindEditBody        ActionKind = "edit-body"
	KindRetarget        ActionKind = "retarget"
	KindReady           ActionKind = "ready"
	KindNativeLink      ActionKind = "native-link"
	KindNativeUnstack   ActionKind = "native-unstack"
	KindCommentDeferred ActionKind = "comment"
)

// ActionState is the lifecycle state of one selected action.
type ActionState string

// Action states.
const (
	StatePlanned    ActionState = "planned"
	StateBlocked    ActionState = "blocked"
	StatePrepared   ActionState = "prepared"
	StateCompleted  ActionState = "completed"
	StateRefused    ActionState = "refused"
	StateFailed     ActionState = "failed"
	StateUnknown    ActionState = "unknown"
	StateNotStarted ActionState = "not-started"
)

// GuardResult is the typed result of one guard. An unknown relevant guard blocks.
type GuardResult string

// Guard results.
const (
	GuardPass          GuardResult = "pass"
	GuardFail          GuardResult = "fail"
	GuardUnknown       GuardResult = "unknown"
	GuardNotApplicable GuardResult = "not-applicable"
)

// Reason is a stable, command-local refusal reason (contracts/forge-effects.md).
type Reason string

// Stable refusal reasons.
const (
	ReasonOwnershipUnknown       Reason = "ownership-unknown"
	ReasonNotOwned               Reason = "not-owned"
	ReasonAttributionMismatch    Reason = "attribution-mismatch"
	ReasonAttributionUnknown     Reason = "attribution-unknown"
	ReasonPlanStale              Reason = "plan-stale"
	ReasonAuthorizationMissing   Reason = "authorization-missing"
	ReasonPermissionUnknown      Reason = "permission-unknown"
	ReasonCapabilityUnavailable  Reason = "capability-unavailable"
	ReasonRemoteChanged          Reason = "remote-changed"
	ReasonHeadChanged            Reason = "head-changed"
	ReasonIdentityMismatch       Reason = "identity-mismatch"
	ReasonIdentityConfigForeign  Reason = "identity-config-foreign"
	ReasonSignaturePolicyUnknown Reason = "signature-policy-unknown"
	ReasonReviewMissing          Reason = "review-missing"
	ReasonChecksUnknown          Reason = "checks-unknown"
	ReasonChecksNotGreen         Reason = "checks-not-green"
	ReasonRebaseableUnknown      Reason = "rebaseable-unknown"
	ReasonScopeNotAccepted       Reason = "scope-not-accepted"
	ReasonNativeRewritePending   Reason = "native-rewrite-pending"
	ReasonStackTooDeep           Reason = "stack-too-deep"
	ReasonOutcomeUnknown         Reason = "outcome-unknown"
	ReasonBoundExceeded          Reason = "bound-exceeded"
	ReasonCancelled              Reason = "cancelled"
	ReasonJournalFailed          Reason = "journal-failed"
)

// Repository binds a plan to exactly one resolved repository. Native Git commit ids are full
// 40-hex SHA-1 values in this first GitHub publication capability.
type Repository struct {
	Backend             string `json:"backend"`
	LayoutIdentity      string `json:"layout_identity"`
	Forge               string `json:"forge"`
	Host                string `json:"host"`
	Owner               string `json:"owner"`
	Name                string `json:"name"`
	SelectionProvenance string `json:"selection_provenance"`
	DefaultRef          string `json:"default_ref"`
	DefaultCommitID     string `json:"default_commit_id"`
	DefaultSource       string `json:"default_source"`
}

// Target names the exact own task ref or own PR a selected action addresses; never a URL.
type Target struct {
	Ref          *string `json:"ref"`
	PullRequest  *int    `json:"pull_request"`
	PullRequests []int   `json:"pull_requests"`
	TrunkBase    *string `json:"trunk_base"`
}

// Expected is the relevant full local/remote state an action was planned against.
type Expected struct {
	OldSHA      *string `json:"old_sha"`
	NewSHA      *string `json:"new_sha"`
	HeadSHA     *string `json:"head_sha"`
	BaseRef     *string `json:"base_ref"`
	PlannedBase *string `json:"planned_base"`
	BodyDigest  *string `json:"body_digest"`
}

// Guard is one typed guard outcome with its stable reason when it did not pass.
type Guard struct {
	Name   string      `json:"name"`
	Result GuardResult `json:"result"`
	Reason Reason      `json:"reason"`
}

// Action is one individually selected effect. It carries no authorization: authorization exists
// only at apply, for this action id and the exact plan id, and is never persisted.
type Action struct {
	ID        string      `json:"action_id"`
	Kind      ActionKind  `json:"kind"`
	Target    Target      `json:"target"`
	Expected  Expected    `json:"expected"`
	Effects   []string    `json:"effects"`
	DependsOn []string    `json:"depends_on"`
	Guards    []Guard     `json:"guards"`
	State     ActionState `json:"state"`
	Blocker   *Reason     `json:"blocker"`
}

// TaskBinding binds the plan to the own task.
type TaskBinding struct {
	TaskID string `json:"task_id"`
}

// ModeSummary records terminal facts per stream and marker NAMES only (never values).
type ModeSummary struct {
	Derived        string   `json:"derived"`
	StdinTerminal  bool     `json:"stdin_terminal"`
	StdoutTerminal bool     `json:"stdout_terminal"`
	Markers        []string `json:"markers"`
}

// PlanOptions are the caller's disclosed selections. Online permits bounded reads only.
type PlanOptions struct {
	Online   bool     `json:"online"`
	Selected []string `json:"selected"`
}

// Blocker explains why an action (or the plan) cannot proceed.
type Blocker struct {
	ActionID *string `json:"action_id"`
	Reason   Reason  `json:"reason"`
}

// Boundary reports what the operation touched. Offline operation requests no network.
type Boundary struct {
	LocalReads         int  `json:"local_reads"`
	PrivateStateWrites int  `json:"private_state_writes"`
	NetworkRequested   bool `json:"network_requested"`
	NetworkAttempted   bool `json:"network_attempted"`
	Requests           int  `json:"requests"`
	Complete           bool `json:"complete"`
	Cancelled          bool `json:"cancelled"`
}

// Failure is the explicit error object of a result; null when there is none.
type Failure struct {
	Code    Reason `json:"code"`
	Message string `json:"message"`
}

// Status is the outcome class of a whole operation and fixes its process exit status.
type Status string

// Statuses.
const (
	StatusComplete   Status = "complete"
	StatusRefused    Status = "refused"
	StatusIncomplete Status = "incomplete"
	StatusPartial    Status = "partial"
	StatusCancelled  Status = "cancelled"
	StatusFailed     Status = "failed"
	StatusUnknown    Status = "unknown"
	StatusUsage      Status = "usage"
)

// Process exit statuses of the unified toolbelt contract (revision 2): 0, 1, 2 only.
const (
	ExitComplete = 0
	ExitRefused  = 1
	ExitUsage    = 2
)

// ExitCode maps a status to its exit status. An unrecognized status fails closed as 1.
func ExitCode(s Status) int {
	switch s {
	case StatusComplete:
		return ExitComplete
	case StatusUsage:
		return ExitUsage
	}
	return ExitRefused
}

// ValidationError is a typed refusal to build or admit a model value. Usage marks errors the
// caller made before any effect (exit status 2). It never carries the offending input text.
type ValidationError struct {
	Code  string
	Field string
	Usage bool
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return e.Code
	}
	return e.Code + ": " + e.Field
}

// CapabilitySet names the action kinds that are admitted for execution. Nothing is measured and
// reviewed yet, so the production set is empty and every kind is capability-unavailable.
type CapabilitySet map[ActionKind]bool

// Has reports whether the kind is admitted.
func (c CapabilitySet) Has(k ActionKind) bool { return c[k] }

// PlanInput carries everything a plan is derived from; the clock value is passed in.
type PlanInput struct {
	CreatedAt    time.Time
	Repository   *Repository
	Task         TaskBinding
	Mode         ModeSummary
	Limits       Limits
	Options      PlanOptions
	Actions      []Action
	Blockers     []Blocker
	Boundary     Boundary
	Error        *Failure
	Capabilities CapabilitySet
}

// Plan is an immutable, validated forge-effect plan. It is built only by NewPlan and exposes
// copies, so a caller cannot change it after validation.
type Plan struct{ doc planDoc }

type planDoc struct {
	SchemaVersion int         `json:"schema_version"`
	Kind          string      `json:"kind"`
	Operation     string      `json:"operation"`
	PlanID        *string     `json:"plan_id"`
	Saved         bool        `json:"saved"`
	CreatedAt     string      `json:"created_at"`
	Repository    *Repository `json:"repository"`
	Task          TaskBinding `json:"task"`
	Mode          ModeSummary `json:"mode"`
	Limits        Limits      `json:"limits"`
	Options       PlanOptions `json:"options"`
	Actions       []Action    `json:"actions"`
	Blockers      []Blocker   `json:"blockers"`
	Boundary      Boundary    `json:"boundary"`
	Error         *Failure    `json:"error"`
}

// NewPlan validates the input and returns an immutable, unsaved plan (saved=false, plan_id=null).
// Every slice and pointer is copied, so later changes to the input cannot reach the plan.
func NewPlan(in PlanInput) (Plan, error) {
	d := planDoc{SchemaVersion: SchemaVersion, Kind: KindForgeEffectPlan, Operation: OperationPlan, CreatedAt: createdAt(in.CreatedAt),
		Task: in.Task, Mode: canonicalMode(in.Mode), Limits: in.Limits, Options: PlanOptions{Online: in.Options.Online, Selected: sortedStrings(in.Options.Selected)},
		Blockers: cloneBlockers(in.Blockers), Boundary: in.Boundary, Error: cloneFailure(in.Error)}
	for _, a := range in.Actions {
		d.Actions = append(d.Actions, cloneAction(a))
	}
	if d.Actions == nil {
		d.Actions = []Action{}
	}
	if in.Repository != nil {
		r := *in.Repository
		d.Repository = &r
	}
	if err := validPlan(d, in.Capabilities); err != nil {
		return Plan{}, err
	}
	return Plan{doc: d}, nil
}

// createdAt renders the creation time, empty for the zero time (which NewPlan refuses).
func createdAt(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// ID is the content identity of the canonical plan: the SHA-256 of its JSON with plan_id null,
// saved false and no disclosed save write, so saving a plan never changes its id.
func (p Plan) ID() string {
	canonical := p.doc
	canonical.PlanID, canonical.Saved = nil, false
	canonical.Boundary.PrivateStateWrites = 0
	raw, err := json.Marshal(canonical)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// Saved reports whether the plan was persisted.
func (p Plan) Saved() bool { return p.doc.Saved }

// PlanID returns the persisted id, or nil for an unsaved plan.
func (p Plan) PlanID() *string { return cloneString(p.doc.PlanID) }

// Repository returns a copy of the bound repository, or nil.
func (p Plan) Repository() *Repository {
	if p.doc.Repository == nil {
		return nil
	}
	r := *p.doc.Repository
	return &r
}

// Actions returns a deep copy of the selected actions.
func (p Plan) Actions() []Action {
	out := make([]Action, 0, len(p.doc.Actions))
	for _, a := range p.doc.Actions {
		out = append(out, cloneAction(a))
	}
	return out
}

// Limits returns the limits the plan was built with.
func (p Plan) Limits() Limits { return p.doc.Limits }

// Options returns a copy of the options.
func (p Plan) Options() PlanOptions {
	return PlanOptions{Online: p.doc.Options.Online, Selected: cloneStrings(p.doc.Options.Selected)}
}

// Task returns the task binding.
func (p Plan) Task() TaskBinding { return p.doc.Task }

// MarshalJSON renders the one human/JSON model.
func (p Plan) MarshalJSON() ([]byte, error) { return json.Marshal(p.doc) }

// SelectActions returns the displayed candidates named by ids, in candidate order. Unknown or
// duplicate ids are usage errors; no id selects nothing.
func SelectActions(candidates []Action, ids []string) ([]Action, error) {
	known := map[string]bool{}
	for _, c := range candidates {
		known[c.ID] = true
	}
	want := map[string]bool{}
	for _, id := range ids {
		if want[id] {
			return nil, &ValidationError{Code: "selection-duplicate", Field: "select", Usage: true}
		}
		if !known[id] {
			return nil, &ValidationError{Code: "selection-unknown", Field: "select", Usage: true}
		}
		want[id] = true
	}
	out := []Action{}
	for _, c := range candidates {
		if want[c.ID] {
			out = append(out, cloneAction(c))
		}
	}
	return out, nil
}

// StoreState is the verdict of the (separate) validation of a plan metadata store.
type StoreState string

// Store states a caller reports. Anything but StoreValidated refuses a save.
const (
	StoreValidated        StoreState = "validated"
	StoreWorkspaceUnknown StoreState = "workspace-unknown"
	StoreExportRisk       StoreState = "plan-store-export-risk"
	StoreFull             StoreState = "plan-store-full"
	StoreUnsafe           StoreState = "plan-store-unsafe"
)

// SaveRequest is what the caller asked for and what it knows about the target store.
type SaveRequest struct {
	Save     bool
	StateDir string // explicit --state-dir; empty selects the default workspace metadata store
	Store    StoreState
}

// SaveTarget names where an admitted save would write.
type SaveTarget string

// Save targets.
const (
	TargetNone             SaveTarget = ""
	TargetWorkspaceDefault SaveTarget = "workspace-default"
	TargetExplicitDir      SaveTarget = "explicit-state-dir"
)

// SaveAdmission is the pure verdict on persisting a plan. It performs no write.
type SaveAdmission struct {
	Persist         bool
	NothingSelected bool
	Target          SaveTarget
}

// AdmitSave decides whether a plan may be persisted. It writes nothing. Without Save, and for an
// empty selection even with Save, nothing persists. A nonempty save needs a store the caller
// validated, and writes the default workspace metadata store or the explicit absolute directory.
func (p Plan) AdmitSave(req SaveRequest) (SaveAdmission, error) {
	if !req.Save {
		return SaveAdmission{}, nil
	}
	if !p.doc.finished() {
		return SaveAdmission{}, &ValidationError{Code: "plan-incomplete", Field: "boundary"}
	}
	if len(p.doc.Actions) == 0 {
		return SaveAdmission{NothingSelected: true}, nil
	}
	target := TargetWorkspaceDefault
	if req.StateDir != "" {
		if !absolutePath(req.StateDir) {
			return SaveAdmission{}, &ValidationError{Code: "state-dir-invalid", Field: "state_dir", Usage: true}
		}
		target = TargetExplicitDir
	}
	switch req.Store {
	case StoreValidated:
		return SaveAdmission{Persist: true, Target: target}, nil
	case StoreWorkspaceUnknown, StoreExportRisk, StoreFull:
		return SaveAdmission{}, &ValidationError{Code: string(req.Store), Field: "store"}
	}
	return SaveAdmission{}, &ValidationError{Code: string(StoreUnsafe), Field: "store"}
}

// WithSaved returns the saved form of the plan (saved=true, plan_id=ID()) after an admitted save.
// It records the one disclosed private-state write and leaves the original plan unchanged.
func (p Plan) WithSaved(a SaveAdmission) (Plan, error) {
	if !p.doc.finished() {
		return Plan{}, &ValidationError{Code: "plan-incomplete", Field: "boundary"}
	}
	if !a.Persist || a.Target == TargetNone || len(p.doc.Actions) == 0 {
		return Plan{}, &ValidationError{Code: "save-not-admitted", Field: "save"}
	}
	if p.doc.Saved {
		return Plan{}, &ValidationError{Code: "already-saved", Field: "save"}
	}
	saved := p
	id := p.ID()
	saved.doc.PlanID, saved.doc.Saved = &id, true
	saved.doc.Boundary.PrivateStateWrites = 1
	return saved, nil
}

// AdmitApply refuses a plan that is unsaved or whose id is not exactly the one the caller named.
func (p Plan) AdmitApply(planID string) error {
	if !isLowerHex(planID, 64) {
		return &ValidationError{Code: "plan-id-invalid", Field: "plan", Usage: true}
	}
	if !p.doc.Saved || p.doc.PlanID == nil {
		return &ValidationError{Code: "plan-not-saved", Field: "plan"}
	}
	if !p.doc.finished() {
		return &ValidationError{Code: "plan-incomplete", Field: "boundary"}
	}
	if planID != *p.doc.PlanID || planID != p.ID() {
		return &ValidationError{Code: "plan-not-found", Field: "plan"}
	}
	return nil
}

// finished reports a plan whose evidence is complete: not truncated, not cancelled, no error. Only
// such a plan may be saved or applied (FR-010: failed evidence never becomes permission).
func (d planDoc) finished() bool {
	return d.Boundary.Complete && !d.Boundary.Cancelled && d.Error == nil
}
