// SPDX-License-Identifier: EUPL-1.2
package workspace

import "context"

type RetirementAction struct {
	Name     string   `json:"name"`
	Target   string   `json:"target"`
	Decision string   `json:"decision"`
	Reason   string   `json:"reason"`
	Proof    string   `json:"proof"`
	Effects  []string `json:"effects"`
	Blockers []string `json:"blockers"`
}
type RetirementEntry struct {
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	Digest     string `json:"digest"`
	Device     uint64 `json:"device"`
	Inode      uint64 `json:"inode"`
	Size       int64  `json:"size"`
	Mode       uint32 `json:"mode"`
	Executable bool   `json:"executable"`
}
type RetirementManifest struct {
	Entries  []RetirementEntry `json:"entries"`
	Digest   string            `json:"digest"`
	Complete bool              `json:"complete"`
}
type RetirementStoredFile struct {
	Path, Digest string
	Size         int64
	Executable   bool
}
type retirementManifestAdmission struct {
	Backend             string
	NativeMetadataPaths []string
	StoredFiles         []RetirementStoredFile
}
type RetirementSibling struct {
	Identity PathIdentity `json:"identity"`
	Kind     string       `json:"kind"`
}
type RetirementObservation struct {
	Expected               Snapshot            `json:"expected"`
	Target                 NativeIdentity      `json:"target"`
	Destination            PathIdentity        `json:"destination"`
	DestinationParent      PathIdentity        `json:"destination_parent"`
	RegistrationParent     PathIdentity        `json:"registration_parent"`
	Manifest               RetirementManifest  `json:"manifest"`
	Metadata               RetirementManifest  `json:"metadata"`
	Siblings               []RetirementSibling `json:"siblings"`
	SiblingDigest          string              `json:"sibling_digest"`
	LockReason             string              `json:"lock_reason"`
	WorkspacePaths         []string            `json:"workspace_paths"`
	WorkspacePathsComplete bool                `json:"workspace_paths_complete"`
	Proof                  string              `json:"proof"`
	Actions                []RetirementAction  `json:"actions"`
	Limitations            []string            `json:"limitations"`
}
type RetirementPlanRecord struct {
	SchemaVersion         int                   `json:"schema_version"`
	PlanID                string                `json:"plan_id"`
	SourcePath            string                `json:"source_path"`
	StateDir              string                `json:"state_dir"`
	Backend               string                `json:"backend"`
	Digest                string                `json:"digest"`
	CreationPlan          MutationPlan          `json:"creation_plan"`
	CreationJournalDigest string                `json:"creation_journal_digest"`
	Task                  TaskRecord            `json:"task"`
	Expected              RetirementObservation `json:"expected"`
	RemoveRegistration    bool                  `json:"remove_registration"`
	RemoveFiles           string                `json:"remove_files"`
	Effects               []string              `json:"effects"`
	RecoveryLimits        []string              `json:"recovery_limits"`
}
type RetirementJournal struct {
	SchemaVersion    int             `json:"schema_version"`
	PlanID           string          `json:"plan_id"`
	PlanDigest       string          `json:"plan_digest"`
	Phase            string          `json:"phase"`
	ExpectedSnapshot Snapshot        `json:"expected_snapshot"`
	PlannedRecovery  TidyRecovery    `json:"planned_recovery"`
	Recovery         *TidyRecovery   `json:"recovery"`
	Actions          []ActionOutcome `json:"actions"`
	Diagnostics      []Diagnostic    `json:"diagnostics"`
}
type retirementAdapter interface {
	Observe(context.Context, string, string, TaskRecord, MutationPlan) (RetirementObservation, error)
	PrepareRecovery(context.Context, RetirementPlanRecord) (TidyRecovery, TidyNativeOutcome, error)
	RemoveRegistration(context.Context, RetirementPlanRecord, TidyRecovery, Snapshot) (TidyNativeOutcome, error)
	RemoveFiles(context.Context, RetirementPlanRecord, TidyRecovery, Snapshot) (TidyNativeOutcome, error)
}

func retirementRecoveryFor(p RetirementPlanRecord) TidyRecovery {
	return tidyRecoveryFor(TidyPlanRecord{PlanID: p.PlanID, Backend: p.Backend, Expected: p.Expected.Expected}, TidyCandidate{Kind: "workspace", Name: p.Task.TaskID, CommitID: p.Expected.Target.CommitID, ChangeID: p.Expected.Target.ChangeID})
}
