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
PrepareRecovery(context.Context, TidyPlanRecord, TidyCandidate) (TidyRecovery, Snapshot, TidyNativeOutcome, error)
Delete(context.Context, TidyPlanRecord, TidyCandidate, TidyRecovery, Snapshot) (TidyNativeOutcome, error)
```
Coordinator owns durable journal and authorization; adapters return exact native effects even on
errors after start. Native recovery root names derive persisted PlanID plus stable candidate action hash, never raw ref paths.
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

## Required pre-action and native delta proof
Prepared durable journal PREDECLARES exact deterministic recovery root name and expected target
identity before PrepareRecovery. This function returns Attempted/Complete/Effects even on errors:
root creation may have happened despite timeout/cancel/nonzero native exit. Such outcomes remain
partial with the predeclared recovery pointer, never reported no-effects. Target deletion requires
confirmed exact recovery-root target, fresh validated state and durable confirmed-root journal first.

Git: support loose and packed local branches. Native update-ref may rewrite packed-refs when deleting
a packed target; explicitly grant only required refs/recovery namespace, exact packed-refs/lock and
associated canonical Git metadata, protecting every unrelated sibling regular file/directory as
read-only. Refuse symlink/special/oversized or unknown metadata/grants. If narrow confinement cannot
be enforced, retain/refuse that capability explicitly before mutation; never broad common-dir write
fallback. Tests must pack refs then delete one target while preserving unrelated loose/packed refs,
source/worktree metadata and bytes outside declared native effects; include pin/delete failure paths.

jj: each successful own operation must have the exact expected previous operation as its sole
operation parent, current operation/heads must match that operation, and full bounded observations
must differ only by the predeclared recovery bookmark addition or selected target removal. Selected
base resolution, workspace registrations/identities, safety state and every unrelated reference
remain equal. Concurrent operation/heads or unrelated ref deltas refuse and report already completed
own effects. Operation IDs are opaque native identities; never infer ordering from text or simply
replace Expected with arbitrary postaction snapshot. This is sampling, not atomic compare-and-swap.

## Plan purpose and backward compatibility
WI1 stored plans describe observation/private persistence only and MUST refuse ApplyTidy, even if
the caller adds --authorize. WI2 newly generated plans explicitly enumerate PROPOSED apply effects:
private prepared/recovery journals; exact native recovery-root creation; exact selected local reference
deletion; retained native recovery roots and manual restoration limits. Planning itself still changes
no native state. Recognized fixed versioned Effects/RecoveryLimits templates, backend and exact target
binding form the plan-purpose marker and are strictly validated with the plan digest. Unknown,
read-only WI1 or modified templates refuse with replan-required; matching plan shape/digest alone never grants mutation.
No new required v1 fields or altered old schema bytes. Tests apply a real WI1 observation plan under
WI2 and require refusal with zero native effects, then generate a new explicit WI2 plan successfully.
