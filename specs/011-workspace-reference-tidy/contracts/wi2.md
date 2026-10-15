# WI2 Apply and Recovery Interface

```go
type TidyRecovery struct {
    Kind, Name, CommitID, ChangeID, OperationID string
}
type TidyNativeOutcome struct {
    Attempted, Complete bool
    Effects, Limitations []string
}
// Add to tidyAdapter for WI2 after independent interface analysis:
PrepareRecovery(context.Context, TidyPlanRecord, TidyCandidate) (TidyRecovery, Snapshot, error)
Delete(context.Context, TidyPlanRecord, TidyCandidate, TidyRecovery, Snapshot) (TidyNativeOutcome, error)
```
Coordinator owns durable journal and authorization; adapters return exact native effects even on
errors after start. Native recovery root names derive generated plan/action IDs, never raw ref paths.
Kinds git-recovery-ref/jj-recovery-bookmark; full IDs and native operation reflect backend distinctions.
Git collisionchecked zero-OID creation precedes targetCAS. jj recovery creation advances operation;
fresh Expected snapshot is returned and checked, never silently accepted unrelated concurrent change.

Apply requires --authorize, immutable stored plan and same secure state. No arbitrary JSON/stdin plan.
Only nativeancestry-proven delete decisions with no workspaces are eligible. Any remote-counterpart
jj bookmark retains, including untracked remote targets. Recovery never changes that rule.
Each action outcome records recoveryroot creation, deletion, completed/incomplete/refused and remaining
work. A prepared/incomplete journal cannot authorize blind repetition; completed retries verify exact
postconditions and preserve recovery evidence. Native roots remain after success and are not GC'd.
Published WI1 schema remains unchanged; WI2 output must validate it or use a separately versioned
contract if new fields/discriminants are needed. Schema compatibility is an independent analyze gate.
