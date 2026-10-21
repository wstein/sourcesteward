// SPDX-License-Identifier: EUPL-1.2
package restack

import (
	"time"

	"github.com/wstein/sourcesteward/internal/inspection"
)

// The documented bounds of FR-018.
const (
	MaxCommits     = 200
	MaxPaths       = 2000
	MaxResultBytes = 1 << 20
	TimeoutSeconds = 60
)

// Notice is the constant disclaimer of every result (FR-016).
const Notice = "Read-only evidence; it does not assert that the restacked branch builds, passes tests or is semantically correct, and it authorizes nothing."

// Limitations that every ok result carries.
const (
	LimitSemanticBreaks   = "semantic-breaks-not-detected"
	LimitPredictionNotRun = "prediction-is-not-apply"
)

// Result is the restack-plan schema 1 object (specs/015-local-restacking/contracts/restack-plan.md).
type Result struct {
	SchemaVersion int                `json:"schema_version"`
	Kind          string             `json:"kind"`
	Status        string             `json:"status"`
	Backend       string             `json:"backend"`
	Identity      *inspection.Result `json:"identity"`
	Input         Input              `json:"input"`
	Outcome       string             `json:"outcome"`
	Notice        string             `json:"notice"`
	Evidence      *Evidence          `json:"evidence"`
	Effects       Effects            `json:"effects"`
	Texts         Texts              `json:"texts"`
	Limitations   []string           `json:"limitations"`
	Diagnostics   []Diagnostic       `json:"diagnostics"`
	Boundary      Boundary           `json:"boundary"`
	ObservedAt    string             `json:"observed_at"`
}

// Diagnostic is a stable code with fixed message text and bounded detail data.
type Diagnostic struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

// Input is the resolved explicit input.
type Input struct {
	Branch    BranchInput `json:"branch"`
	OldParent ParentInput `json:"old_parent"`
	NewParent ParentInput `json:"new_parent"`
	Base      *Base       `json:"base"`
	Moved     Moved       `json:"moved"`
	Remote    string      `json:"remote"`
}

type BranchInput struct {
	Ref     string  `json:"ref"`
	Short   string  `json:"short"`
	OldHead *string `json:"old_head"`
}

type ParentInput struct {
	Given string  `json:"given"`
	OID   *string `json:"oid"`
}

// Base holds the old parent (checked to be an ancestor of the old head) and the merge base of the
// old and the new parent.
type Base struct {
	Old string `json:"old"`
	New string `json:"new"`
}

type Moved struct {
	Count   int           `json:"count"`
	Commits []MovedCommit `json:"commits"`
}

type MovedCommit struct {
	OID           string   `json:"oid"`
	Parents       []string `json:"parents"`
	Tree          string   `json:"tree"`
	Author        string   `json:"author"`
	Committer     string   `json:"committer"`
	Subject       string   `json:"subject"`
	SubjectSHA256 string   `json:"subject_sha256"`
}

// Evidence is the bundle of FR-013 (a) to (f); effects and texts are top-level.
type Evidence struct {
	Replay           Replay        `json:"replay"`
	RangePairing     RangePairing  `json:"range_pairing"`
	ParentDropped    ParentDropped `json:"parent_dropped"`
	EmptyAfterReplay []string      `json:"empty_after_replay"`
	OwnDiffEquality  OwnDiffs      `json:"own_diff_equality"`
	Identity         IdentityCheck `json:"identity"`
	Conflicts        Conflicts     `json:"conflicts"`
	Overlap          Overlap       `json:"overlap"`
}

type Replay struct {
	State           string  `json:"state"`
	CommitsReplayed int     `json:"commits_replayed"`
	FinalTree       *string `json:"final_tree"`
	// TreeEqualsOneShot is true when the replayed final tree equals the single three-way merge of
	// the branch tip, the old parent and the new parent; null when it was not computed.
	TreeEqualsOneShot *bool `json:"tree_equals_branch_tip_plus_parent_delta"`
}

type PairRow struct {
	Old  string `json:"old"`
	Mark string `json:"mark"`
}

type RangePairing struct {
	Rows     []PairRow `json:"rows"`
	AllEqual bool      `json:"all_equal"`
}

type DroppedRow struct {
	OID    string `json:"oid"`
	Mark   string `json:"mark"`
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
}

type ParentDropped struct {
	Rows       []DroppedRow `json:"rows"`
	AllDropped bool         `json:"all_dropped"`
	State      string       `json:"state"`
	Reason     string       `json:"reason,omitempty"`
}

type OwnDiffRow struct {
	Old              string `json:"old"`
	State            string `json:"state"`
	OldPatchID       string `json:"old_patch_id"`
	PredictedPatchID string `json:"predicted_patch_id"`
}

type OwnDiffs struct {
	Rows     []OwnDiffRow `json:"rows"`
	AllEqual bool         `json:"all_equal"`
}

type IdentityMismatch struct {
	Commit string `json:"commit"`
	Role   string `json:"role"`
	Value  string `json:"value"`
}

type IdentityCheck struct {
	State      string             `json:"state"`
	Source     string             `json:"source"`
	Reason     string             `json:"reason,omitempty"`
	Expected   []string           `json:"expected"`
	Mismatches []IdentityMismatch `json:"mismatches"`
}

type ConflictItem struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

type Conflicts struct {
	Predicted       bool           `json:"predicted"`
	Commit          *string        `json:"commit"`
	Items           []ConflictItem `json:"items"`
	ReplayStoppedAt *string        `json:"replay_stopped_at"`
}

type Overlap struct {
	Paths     []string `json:"paths"`
	Count     int      `json:"count"`
	Indicator string   `json:"indicator"`
	Reason    string   `json:"reason,omitempty"`
	Note      string   `json:"note"`
}

type Effects struct {
	AffectedRefs []AffectedRef `json:"affected_refs"`
	Worktrees    []Worktree    `json:"worktrees"`
	Hooks        []HookInfo    `json:"hooks"`
	Hazards      []string      `json:"hazards"`
	Writes       Writes        `json:"writes"`
}

type AffectedRef struct {
	Ref             string `json:"ref"`
	OID             string `json:"oid"`
	ContainsOldHead bool   `json:"contains_old_head"`
}

type Worktree struct {
	Path   string  `json:"path"`
	Branch *string `json:"branch"`
	State  string  `json:"state"`
}

type HookInfo struct {
	Name                      string `json:"name"`
	Present                   bool   `json:"present"`
	WouldRunByPorcelainRebase bool   `json:"would_run_by_porcelain_rebase"`
	RunBySTW                  bool   `json:"run_by_stw"`
}

type Writes struct {
	Refs     bool   `json:"refs"`
	Index    bool   `json:"index"`
	Worktree bool   `json:"worktree"`
	Config   bool   `json:"config"`
	Objects  string `json:"objects"`
}

type Texts struct {
	LeasePush *LeasePush `json:"lease_push"`
	Resync    *Resync    `json:"resync"`
}

type LeasePush struct {
	Assignment     string `json:"assignment"`
	Text           string `json:"text"`
	ObservedOldSHA string `json:"observed_old_sha"`
	NewSHAKnown    bool   `json:"new_sha_known"`
}

type Resync struct {
	Fetch           string `json:"fetch"`
	RebaseOnto      string `json:"rebase_onto"`
	ResetHard       string `json:"reset_hard"`
	DestructiveNote string `json:"destructive_note"`
}

type Boundary struct {
	MaxCommits     int     `json:"max_commits"`
	MaxPaths       int     `json:"max_paths"`
	MaxResultBytes int     `json:"max_result_bytes"`
	TimeoutSeconds int     `json:"timeout_seconds"`
	Reached        *string `json:"reached"`
}

// Status, outcome and diagnostic-code vocabulary.
const (
	StatusOK         = "ok"
	StatusIncomplete = "incomplete"
	StatusError      = "error"

	OutcomeClean     = "evidence-clean"
	OutcomeAttention = "needs-attention"
	OutcomeConflicts = "conflicts-predicted"
	OutcomeNone      = "none"
)

func newResult(o Options, now func() time.Time) Result {
	short := o.Branch
	if len(short) > len("refs/heads/") && short[:len("refs/heads/")] == "refs/heads/" {
		short = short[len("refs/heads/"):]
	}
	return Result{
		SchemaVersion: 1, Kind: "restack-plan", Status: StatusError, Backend: "unknown", Outcome: OutcomeNone, Notice: Notice,
		Input: Input{
			Branch: BranchInput{Ref: o.Branch, Short: short}, OldParent: ParentInput{Given: o.OldParent}, NewParent: ParentInput{Given: o.NewParent},
			Moved: Moved{Commits: []MovedCommit{}}, Remote: o.Remote,
		},
		Effects:     Effects{AffectedRefs: []AffectedRef{}, Worktrees: []Worktree{}, Hooks: []HookInfo{}, Hazards: []string{}, Writes: Writes{Objects: "quarantine-only"}},
		Limitations: []string{LimitSemanticBreaks, LimitPredictionNotRun}, Diagnostics: []Diagnostic{},
		Boundary:   Boundary{MaxCommits: MaxCommits, MaxPaths: MaxPaths, MaxResultBytes: MaxResultBytes, TimeoutSeconds: TimeoutSeconds},
		ObservedAt: now().UTC().Format(time.RFC3339),
	}
}
