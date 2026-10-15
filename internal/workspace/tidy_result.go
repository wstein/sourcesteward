// SPDX-License-Identifier: EUPL-1.2
package workspace

import "context"

type TidyPlanOptions struct {
	Path, Base, StateDir string
	Refs                 []string
	Task, Owner          string
	RemoveRegistration   bool
	RemoveFiles          string
}
type TidyApplyOptions struct {
	StateDir, PlanID string
	Authorize        bool
}
type TidyCandidate struct {
	Kind           string   `json:"kind"`
	Name           string   `json:"name"`
	CommitID       string   `json:"commit_id"`
	ChangeID       string   `json:"change_id"`
	Decision       string   `json:"decision"`
	Reason         string   `json:"reason"`
	Proof          string   `json:"proof"`
	WorkspacePaths []string `json:"workspace_paths"`
	Limitations    []string `json:"limitations"`
}
type TidyObservation struct {
	// WorkspacePaths contains every registered workspace root, independent of candidate selection.
	WorkspacePaths         []string
	WorkspacePathsComplete bool
	Expected               Snapshot
	Candidates             []TidyCandidate
	Limitations            []string
}
type TidyPlanRecord struct {
	SchemaVersion  int             `json:"schema_version"`
	PlanID         string          `json:"plan_id"`
	SourcePath     string          `json:"source_path"`
	StateDir       string          `json:"state_dir"`
	Backend        string          `json:"backend"`
	Digest         string          `json:"digest"`
	Expected       Snapshot        `json:"expected"`
	SelectedRefs   []string        `json:"selected_refs"`
	Candidates     []TidyCandidate `json:"candidates"`
	Effects        []string        `json:"effects"`
	RecoveryLimits []string        `json:"recovery_limits"`
}
type TidyResult struct {
	Retirement    *RetirementPlanRecord `json:"retirement,omitempty"`
	SchemaVersion int                   `json:"schema_version"`
	Command       string                `json:"command"`
	Status        string                `json:"status"`
	Backend       string                `json:"backend"`
	Plan          *TidyPlanRecord       `json:"plan"`
	Actions       []ActionOutcome       `json:"actions"`
	Diagnostics   []Diagnostic          `json:"diagnostics"`
	Limitations   []string              `json:"limitations"`
}
type tidyAdapter interface {
	Observe(context.Context, string, string) (TidyObservation, error)
}

func newTidyResult(command string) TidyResult {
	return TidyResult{SchemaVersion: 1, Command: command, Status: "error", Backend: "unknown", Actions: []ActionOutcome{}, Diagnostics: []Diagnostic{}, Limitations: []string{}}
}
func (r TidyResult) Error() string {
	if len(r.Diagnostics) > 0 {
		return r.Diagnostics[0].Message
	}
	return "tidy operation failed"
}
