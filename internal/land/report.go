// SPDX-License-Identifier: EUPL-1.2

package land

// Reason codes (typed refusals). Spec 043 FR-060 to FR-068, FR-081.
const (
	ReasonHeadChanged      = "head-changed"
	ReasonChainNotLinear   = "chain-not-linear"
	ReasonBaseNotInChain   = "base-not-in-chain"
	ReasonObjectMissing    = "object-missing"
	ReasonNotFastForward   = "not-fast-forward"
	ReasonSideChild        = "stack-side-child"
	ReasonChecksNotGreen   = "checks-not-green"
	ReasonChecksPending    = "checks-pending"
	ReasonTipChecksPending = "tip-checks-pending"
	ReasonHeadChangedNoChk = "head-changed-no-checks"
	ReasonChecksMissing    = "checks-missing"
	ReasonChecksUnknown    = "checks-unknown"
	ReasonRangeMismatch    = "range-mismatch"
	ReasonRangeTooLarge    = "range-too-large"
	ReasonRulesetForbids   = "ruleset-forbids-push"
	ReasonLandUnmeasured   = "land-unmeasured"
	ReasonDraft            = "draft"
	ReasonFork             = "fork-unsupported"
	ReasonNotOpen          = "not-open"
	ReasonAlreadyMerged    = "already-merged-elsewhere"
	ReasonConflicting      = "conflicting"
	ReasonMergeabilityNone = "mergeability-not-computed"
	ReasonIdentityMismatch = "identity-mismatch"
	ReasonIdentityPolicy   = "identity-policy-missing"
	ReasonRemoteMismatch   = "remote-repo-mismatch"
	ReasonReadFailed       = "read-failed"
	ReasonSideChildUnknown = "side-children-unknown"
	ReasonAgentMode        = "agent-mode"
	ReasonApplyUnavailable = "apply-unavailable"

	// Details.
	DetailNativeStackShape = "native-stack-shape"
	DetailRebaseNeeded     = "rebase needed"
)

// Results of a plan or an apply.
const (
	ResultLandable        = "landable"
	ResultRefused         = "refused"
	ResultAlreadyLanded   = "already-landed"
	ResultLandedNotMarked = "landed-not-marked"
)

// Values of review_evidence, operations and the schema version.
const (
	ReviewFound   = "found"
	ReviewNone    = "none"
	ReviewUnknown = "unknown"
	OperationPlan = "plan"
	SchemaVersion = 1
)

const (
	maxOffendingCommits     = 10
	maxDetailBytes          = 240
	pushKind, fetchKind     = "push", "fetch"
	effectPlanned, effectOK = "planned", "done"
)

// Precondition is one line of the preconditions array. Number is the pull request it concerns, 0
// for a precondition of the chain or the repository (omitted in JSON then).
type Precondition struct {
	ID     string `json:"id"`
	Number int    `json:"number,omitempty"`
	OK     bool   `json:"ok"`
	Value  string `json:"value"`
	Reason string `json:"reason,omitempty"`
}

// Reason is one failing precondition with its typed code.
type Reason struct {
	Code   string `json:"code"`
	Number int    `json:"number,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// ChainEntry is one pull request of the chain table (FR-079).
type ChainEntry struct {
	Number         int    `json:"number"`
	Head           string `json:"head"`
	BaseRef        string `json:"base_ref"`
	ReviewEvidence string `json:"review_evidence"`
	Checks         string `json:"checks"`
	State          string `json:"state"`
	Outcome        string `json:"outcome"`
}

// Effect is one command a landing would run; argv is the exact vector.
type Effect struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Argv    []string `json:"argv"`
	Outcome string   `json:"outcome"`
	Detail  string   `json:"detail,omitempty"`
}

// Inputs echo the validated command line.
type Inputs struct {
	Stack     []int             `json:"stack"`
	Reviewed  map[string]string `json:"reviewed"`
	AckDraft  bool              `json:"ack_draft"`
	Remote    string            `json:"remote"`
	DeleteBr  bool              `json:"delete_branch"`
	PlanOnly  bool              `json:"plan"`
	RangeMax  int               `json:"range_bound"`
	WaitLimit string            `json:"wait,omitempty"`
}

// Report is the one JSON document of `stw land --stack ... --json` (schemas/v1/land.schema.json).
type Report struct {
	SchemaVersion int            `json:"schema_version"`
	Command       string         `json:"command"`
	Operation     string         `json:"operation"`
	Mode          Mode           `json:"mode"`
	Repository    string         `json:"repository"`
	Inputs        Inputs         `json:"inputs"`
	Preconditions []Precondition `json:"preconditions"`
	Chain         []ChainEntry   `json:"chain"`
	DefaultTip    string         `json:"default_tip,omitempty"`
	PlanDigest    string         `json:"plan_digest,omitempty"`
	Landable      bool           `json:"landable"`
	ApplyAllowed  bool           `json:"apply_allowed"`
	Effects       []Effect       `json:"effects"`
	Children      []int          `json:"children"`
	Result        string         `json:"result"`
	Reasons       []Reason       `json:"reasons"`
	Notes         []string       `json:"notes"`
	Truncated     bool           `json:"truncated"`
}
