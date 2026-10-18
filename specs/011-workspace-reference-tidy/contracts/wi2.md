# WI2 Apply and Recovery Interface

```go
type TidyRecovery struct {
    Kind, Name, CommitID, ChangeID, OperationID string
}
type TidyNativeOutcome struct {
    Attempted, Complete bool
    Effects, Limitations []string
    Expected *Snapshot // internal verified native post-action state
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

## Internal verified post-action snapshot amendment
TidyNativeOutcome.Expected is internal-only, never a new published result/schema field. An adapter
may populate it only after full native exact-delta validation proves the permitted own action and
all unrelated state unchanged. Core performs a fresh Observe and requires exact Expected equality
before advancing durable journal state or authorizing the next action. Missing/unverified/unequal
Expected remains partial/refused; never accept an arbitrary postaction state as the new baseline.
This strengthens the analyzed per-action delta contract without weakening schema v1 compatibility.

## Native jj private execution context amendment
Existing colocated jj may reset source Git HEAD/index during bookmark operations even with
--ignore-working-copy/git.colocate=false. Native mutation therefore executes in a private bounded
context inside the existing isolation directory: exact copied working_copy checkout/tree_state/type
metadata and an absolute .jj/repo pointer to the same shared native repository; no .git, no init, no
workspace add/update/register and no migration. Only known current-user single-link regular files
are copied, with no-follow reads, input/output limits and source identity/content checks before/after.
Source HEAD/index/configuration/working_copy bytes and complete registered workspace list remain
unchanged. Private context creation/copy occurs only during bounded setup; context is read-only during native
execution. Native operations may write only already declared shared operation/object/index stores;
no private context, broad repository parent or source Git metadata write grant. Source configuration
helpers remain blocked by the existing isolated session configuration boundary. Exact native
sole-parent operation/bookmark delta proof remains mandatory, using actual source observation for
validation rather than trusting the private path as another owned workspace. Source read capability
and final identity remain unchanged. Failure to enforce this mechanism refuses colocated mutation.

Mandatory both-version experiments and conformance must verify no registration/init/source-write
effects, equal shared repository identities and complete expected native operation/ref deltas; proxy
research alone is not accepted shipping evidence. Tests include hostile metadata symlink/extra files,
bounds, copy/source replacement races and nonzero/cancel after native start. Recovery bookmark uses
fixed trailing ? to avoid native Git-ref export; both protections are required for colocated safety.

## Issue #62: layout proof through reference recovery and deletion

The [shared Git layout contract](../../010-isolated-task-workspaces/contracts/workspace.md#git-checkout-layout-admission-issue-62)
applies before recovery creation and before deletion, including from submodule-backed linked
sources. Native recovery/target deltas may advance only the already specified snapshot fields;
layout/configuration evidence and all unrelated workspace/index/ref state remain equal. A
changed mapping after recovery creation refuses deletion and preserves that recovery pointer.
No primary, superproject or common-parent write grant follows from layout admission. Existing
jj bookmark-only semantics and native private execution context remain unchanged.
