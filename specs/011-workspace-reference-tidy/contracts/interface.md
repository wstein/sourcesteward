# Tidy Shared Interface Freeze

All effects remain in internal/workspace and reviewed inspection jj helpers; CLI has no filesystem
or process access. Existing Spec010 exported types and JSON fields are unchanged.

```go
type TidyPlanOptions struct {
    Path, Base, StateDir string
    Refs []string
    Task, Owner string
    RemoveRegistration bool
    RemoveFiles string
}
type TidyApplyOptions struct { StateDir, PlanID string; Authorize bool }
func PlanTidy(context.Context, TidyPlanOptions) TidyResult
func ApplyTidy(context.Context, TidyApplyOptions) TidyResult
```

Core owns TidyResult/TidyPlanRecord/TidyCandidate/TidyJournal and fixed adapter observation/action
interfaces; native authors own only backend-specific helpers and tests. Use existing NativeIdentity,
Snapshot, PathIdentity, Diagnostic and ActionOutcome where semantically exact. Public CLI contract
controls flags; model controls JSON shape. WI1 concrete model fields are frozen below before parallel source work. No mutable init globals, duplicate stores, arbitrary command arguments or
CLI-side native execution. Native functions return known effects alongside errors after action start.
Core owns journals, per-action stale validation, safe retry and aggregate encoded output bounds.
Schema/CLI author synchronizes exact JSON shape before source implementation; no weakened old schemas.

## WI1 exact model
```go
type TidyCandidate struct {
    Kind, Name, CommitID, ChangeID, Decision, Reason, Proof string
    WorkspacePaths, Limitations []string
}
type TidyObservation struct {
    Expected Snapshot
    Candidates []TidyCandidate
    WorkspacePaths []string // all observed registrations, internal only
    Limitations []string
}
type TidyPlanRecord struct {
    SchemaVersion int
    PlanID, SourcePath, StateDir, Backend, Digest string
    Expected Snapshot
    SelectedRefs []string
    Candidates []TidyCandidate
    Effects, RecoveryLimits []string
}
type tidyAdapter interface {
    Observe(context.Context, string, string) (TidyObservation, error)
}
```
TidyResult mirrors existing workspace Result envelope except no Task, Plan points TidyPlanRecord.
JSON snake_case, arrays nonnil; Decision delete/retain, Proof native-ancestry for eligible deletion;
Kind branch for Git / bookmark for jj. Proof none or native-ancestry; retained candidates may still have ancestry proof. Reason is nonempty bounded text.
Optional StateDir: absent produces PlanID empty and no store writes; present stores generated ID.
Repeat Refs must be unique exact known native local names; unknown/duplicate selection refuses.
Core verifies full observation equality before persistence, validates secure state disjointness,
complete native IDs/Snapshot/digests and encoded/aggregate bounds. No candidate containing
workspace paths or unknown proof can be selected for deletion. Later WI2/3 types are refined and
reanalyzed before their source work without weakening WI1 published compatibility.

A retained jj bookmark with Reason ambiguous-target may have empty CommitID/ChangeID only when
Proof is none; it can never be deleted. Missing explicit names refuse the operation rather than
produce fabricated candidates. Full native IDs remain mandatory for every delete candidate.

All-registration confinement: internal TidyObservation.WorkspacePaths includes every observed native
workspace path, including detached Git registrations, jj workspaces without bookmarks and unselected
references. Core checks secure state disjointness against this complete bounded list, never only
selected candidate WorkspacePaths. This internal field does not alter published result/schema; before/
after observation equality and aggregate limits include it. Missing registration paths retain/refuse
rather than authorize a store overlapping another checkout.
