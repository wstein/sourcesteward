// SPDX-License-Identifier: EUPL-1.2

package stackpublish

import "time"

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
func ExitCode(Status) int { return -1 }

// ValidationError is a typed refusal to build or admit a model value. Usage marks errors the
// caller made before any effect (exit status 2).
type ValidationError struct {
	Code  string
	Field string
	Usage bool
}

func (e *ValidationError) Error() string { return "" }

// CapabilitySet names the action kinds that are admitted for execution. Nothing is measured and
// reviewed yet, so the production set is empty and every kind is capability-unavailable.
type CapabilitySet map[ActionKind]bool

// Has reports whether the kind is admitted.
func (c CapabilitySet) Has(ActionKind) bool { return false }

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
func NewPlan(PlanInput) (Plan, error) { return Plan{}, nil }

// SelectActions returns the displayed candidates named by ids, in candidate order. Unknown or
// duplicate ids are usage errors; no id selects nothing.
func SelectActions(candidates []Action, ids []string) ([]Action, error) { return nil, nil }

// ID is the content identity of the canonical plan (plan_id null, saved false).
func (p Plan) ID() string { return "" }

// Saved reports whether the plan was persisted; PlanID is its id then, nil otherwise.
func (p Plan) Saved() bool { return false }

// PlanID returns the persisted id or nil.
func (p Plan) PlanID() *string { return nil }

// Repository returns a copy of the bound repository, or nil.
func (p Plan) Repository() *Repository { return nil }

// Actions returns a deep copy of the selected actions.
func (p Plan) Actions() []Action { return nil }

// Limits returns the limits the plan was built with.
func (p Plan) Limits() Limits { return Limits{} }

// Options returns a copy of the options.
func (p Plan) Options() PlanOptions { return PlanOptions{} }

// MarshalJSON renders the one human/JSON model.
func (p Plan) MarshalJSON() ([]byte, error) { return nil, nil }

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
	StateDir string // explicit --state-dir, empty for the default workspace metadata store
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

// AdmitSave decides whether a plan may be persisted. It writes nothing.
func (p Plan) AdmitSave(SaveRequest) (SaveAdmission, error) { return SaveAdmission{}, nil }

// WithSaved returns the saved form of the plan (saved=true, plan_id=ID()) after an admitted save.
func (p Plan) WithSaved(SaveAdmission) (Plan, error) { return Plan{}, nil }

// AdmitApply refuses a plan that is unsaved, has no id or whose id is not the id the caller named.
func (p Plan) AdmitApply(planID string) error { return nil }
