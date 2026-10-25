# Proposed Contract: Resume Comparison and Separately Authorized Restore

**UNPUBLISHED — unimplemented**, 2026-10-25. Reuses the proposed typed recovery
integration, not a second generic journal writer. See [research](../research.md),
[model](../data-model.md) and [shared limits/output](checkpoint.md).

## Offline read-only comparison

```text
stw checkpoint resume --checkpoint CHECKPOINT_ID --path WORKSPACE --state-dir PRIVATE_DIR [--json]
stw checkpoint restore-plan --checkpoint CHECKPOINT_ID --path WORKSPACE --state-dir PRIVATE_DIR --select-layer LAYER --select-path PATH [--json]
stw checkpoint restore --plan RESTORE_PLAN_ID --state-dir PRIVATE_DIR --authorize [--json]
```

The separate stored restore plan is proposed because 013 can replace explicitly
selected byte layers as well as native identities: the caller needs to review
current expected state, exact replacement scope and required pre-action byte
recovery before authorizing effects. This extends FR-004's inspection/action
separation; it is not a claim that 032's single `stw sync restore` command already
provides this protocol. That command keeps its existing narrow sync semantics.
The example `checkpoint restore-plan`/`checkpoint restore` spellings, stored-plan
interface and common flag admission remain under OQ-5/interface acceptance; an
accepted plan/apply command shape must preserve the same separate review,
expected-state checks and explicit action authorization. Reuse of journal
mechanisms does not require identical CLI spelling or amend 032's command.

For jj, resume uses explicit `--ignore-working-copy --at-operation` reads with full-id pinning after initial discovery and read-only isolation; stw initiates neither disk snapshots nor operation-head merging in those reads; other native commands remain outside that guarantee. Inventory all workspace identities; a missing/abandoned/GC'd saved operation is `unavailable`, and unknown/divergent current state cannot authorize restore. The [native operation guard](selective-undo.md#native-jj-operation-guard-oq-2-proposal) applies to any admitted operation restoration: the first slice refuses any workspace working-copy commit change, invoking workspace included, rather than leave it stale under snapshot suppression or run implicit update-stale.

Resume explicitly names a checkpoint, not an implicit latest entry. Read saved
manifest/receipt, validate repository/native/task bindings and integrity, compare
current selected state through read-only native inspection and bounded no-follow
disk reads. Report saved/current identities, staged vs unstaged Git differences,
jj stored vs unsnapshotted disk differences, retained/excluded/unavailable layers,
completeness, recovery limits and supported next actions as descriptions.
It writes no record, retained objects, refs, index, disk, jj snapshot or operation
log, and makes no network call. Missing backend/sandbox or unavailable payload
is reported clearly; no repair/snapshot/fetch is attempted. A checkpoint can be
inspectable while restore is unsupported. Current differences are observations,
not authorization to replace them or proof of exclusive ownership.

Restore-plan is a separate selected operation, with its own stored private plan
and id/digest. Select exact captured layers/paths/native targets. It lists the
complete effect set and pre-action recovery promise, expected present state,
retained desired state, limits, conflicts and ownership/protection/workspace
blockers. Existing disk changes outside selected paths are never overwritten.
Changed relevant state, symlink/alias replacement, missing/corrupt bytes or roots,
unsupported type/mode/index state, foreign/shared/unknown ownership, active
operations, changed layout/config or an incomplete checkpoint refuses apply.
No merge, stashing, file deletion outside selected missing markers or broader
native restoration is implied. No restore command is run from stored text.

## Recovery-root ownership and namespace reservation

Proposed OQ-5 reservation: checkpoint/capture roots use only
`refs/stw/recovery/checkpoint/CHECKPOINT_ID/N`; selective-undo action roots use
only `refs/stw/recovery/undo/UNDO_ID/N`. Pre-action safety copies for any restore
or undo use the distinct reserved `refs/stw/recovery/undo/safety/SAFETY_ID/N`
subtree and a self-contained `safety-copy` record keyed by SAFETY_ID. Generated ids are validated opaque components,
never a caller-provided path or a legacy tidy plan id; `N` is the positive action
index. The fixed `checkpoint`/`undo` components and `undo/safety` subtree are reserved for these typed 013
records, not valid legacy plan-id aliases; `safety` is not an admitted UNDO_ID. Admission must detect any pre-existing
namespace occupant/file-directory conflict and refuse without adoption.

| Owner/spec | Existing or proposed Git recovery namespace |
| --- | --- |
| Tidy v1/v2, Spec 011; `internal/workspace/tidy_apply_result.go` | Existing `refs/stw/recovery/PLAN_ID/HASH`; per-plan legacy ids, not a generic consumer namespace |
| Tidy v3, Specs 029/031; `internal/workspace/tidy_v3_result.go` | Existing `refs/stw/recovery/v3/PLAN_ID/HASH` |
| Sync, Spec 032; `internal/stwsync/journal.go` | Existing `refs/stw/recovery/sync/RUN_ID/N` and common-dir `stw/sync` records |
| Local restack, Spec 015 recovery proposal | Proposed `refs/stw/recovery/restack/RUN_ID/N`; not a 013-owned prefix |
| Checkpoint capture and selective undo, Spec 013 | Proposed reserved `checkpoint/CHECKPOINT_ID/N` and `undo/UNDO_ID/N` below `refs/stw/recovery/` |
| Pre-action safety copies for restore/undo, Spec 013 | Proposed reserved `refs/stw/recovery/undo/safety/SAFETY_ID/N`, independent safety-copy record/receipt/lifetime |

Create roots only when absent, with the expected missing/zero value; reject
symbolic roots. Any later update or explicit deletion requires its exact typed
013 receipt, full name and expected old OID. Never write into `stw/sync`, traverse
another feature's records or mutate an unowned ref/file, even if its name looks
like a recovery root. Sync retention reads only sync records and deletes only
its exact sync-run namespace at expected values, so it cannot collect 013 roots
or bytes. Conformance must keep 013 roots/receipts beside more than ten finished
sync runs and prove they survive sync retention unchanged.

jj root kind remains an explicit OQ-5 admission choice: recommend a retained
native commit plus operation-log evidence without creating a visible bookmark,
with measured native retention/GC limits. An operation id or hidden unpinned
commit alone is not a durable root; refuse any promised root-dependent recovery
until the mechanism is proved on admitted versions. Current tidy uses visible
`stw-recovery-PLAN_ID-HASH?` bookmarks: source includes the trailing `?`, and Spec
011's contract uses it to prevent Git auto-export. That version-qualified rule
is not a generic hidden-root mechanism. An ordinary visible bookmark with a
Git-exportable name may be published by all/selected/tracked bookmark pushes;
013 refuses visible-bookmark roots until publication risk and a safe native
mechanism are explicitly accepted and tested. Git recovery refs are also not
secret storage or proof against an explicit broad export; they retain committed
objects only, never private selected-byte payloads.

## Independent pre-action safety copies

A restore may replace current work that was never in its source checkpoint.
Before that effect, save the promised current bytes/native roots in a new
self-contained safety-copy record and private payload receipt with its own
SAFETY_ID, action/restore-plan binding and reserved safety roots. Its origin
checkpoint id is provenance only: neither bytes, ownership nor restoration
information depend on that checkpoint's continued existence. Explicit private
modes, external-store proof and the total physical quota apply; each safety
copy is bounded to 32 MiB selected bytes and an exceeded required capture refuses
the replacing effect. Durable safety capture and prepared intent precede mutation.

Safety copies have independent retention, including after successful completion;
there is no automatic expiry or cleanup when a restore completes or a source
checkpoint/plan is dropped. Checkpoint drop never selects safety-copy payloads,
records or roots, even for completed restores. Their only deletion path is a
separate plan that names the exact safety-copy id and requires its own explicit
authorization and safety-id confirmation. Pending action/receipt dependencies
refuse. See [explicit deletion](checkpoint.md#private-payload-storage-and-explicit-deletion).
A partial restore/drop retains safety receipts and exact remaining byte/root
availability; source-checkpoint deletion cannot remove that recovery evidence.

## Prepare, apply and reconcile

The new typed integration must preserve 032 characterization while supporting
013's reviewed record type. Before each selected mutation, durably retain the
pre-action native roots and selected pre-action byte layers necessary for the
explicit recovery promise, then persist prepared intent with expected old and
known desired new identities/fingerprints. Ref/root collisions, unsafe paths,
failed durability/integrity or incomplete scope stop the effect. Native roots
preserve committed history; selected disk/index bytes require their own receipt.

After the action, resample and durably record actual effects and completion.
Completion-write failure after an effect is partial/unknown, never a claim of
no effect or an automatic rollback. Stop before the next action. Multi-path
native/disk/private-store changes are not one transaction; each completed,
not-started and uncertain action is exposed. Restore is itself journaled and
requires fresh authorization; a retry reconciles instead of replaying blindly.

| Durable record plus current observation | Classification and allowed behavior |
| --- | --- |
| Valid typed record; all relevant current values equal recorded old values | No effect proven at those samples; separately authorized retry only after fresh full guards |
| All relevant current values equal known desired/new values | `already-applied`; validate bindings/receipts, complete missing evidence without repeating mutation |
| Values differ from both or selected effects are mixed | `changed-since` or `partial`; retain evidence, inspect exact per-action state, no blanket overwrite |
| Missing/unknown new identity, malformed/foreign record, corrupt payload, missing root or unreadable state | `journal-incomplete`/typed refusal; preserve evidence, no inferred restore |
| Equality with detected ABA, ownership/configuration race or incomplete scope | Unknown/refusal; later equality is not proof of uninterrupted ownership |
| jj sampled operation heads or a pinned mutation basis | No operation-head CAS: observed pre-effect races refuse and observed post-effect races/incomplete checks are partial; samples may miss native changes, and external commands may integrate divergent heads |

A separately selected reverse ref movement uses expected-current-new comparison;
current equals old is `already-restored`, any other value is `changed-since`.
Revalidate native occupancy/protection and exact root value. Spec 015's proposed
adapter admits only owned unoccupied topic refs and uses CAS. Spec 032's existing
idle-clean-main `reset --keep` path is not generalized or authorized for 013.
The exact 013 disk/index/native mutation primitives remain blocked by OQ-1/OQ-2;
no reset/checkout or broad jj operation command is selected by this document.

## Existing Spec 032 boundary and retention

Today `internal/stwsync` privately stores `sync` v1 in common-dir `stw/sync`, with
`prepared`/`completed`/`failed`, old/planned-new/new OIDs, recovery ref and stash
OID. Recovery roots are `refs/stw/recovery/sync/RUN_ID/N`. Known prepared
fast-forwards can reconcile by planned new tip; unknown prepared rebase tips
remain `journal-incomplete` and are never blindly reset. Its unoccupied-ref
restore is expected-new CAS; idle clean main-worktree restore uses `reset --keep`
only under 032's own guards. It retains ten finished runs and protects prepared
records; handled incomplete restore may record `restored`.

That behavior is unchanged. It restores neither untracked/general worktree/index
content, pruned refs, stash apply/drop nor submodule/configuration effects.
Do not relabel sync records, infer saved bytes from a stash field or use its
retention to delete checkpoint data. New payload location/quota/retention and
record namespaces/migration await OQ-3/OQ-5. Incomplete records stay protected;
this design authorizes no automatic expiry, GC or orphan deletion.

No restoration promise covers unsaved editor buffers, uncaptured files/layers,
expired or externally deleted payload/objects, arbitrary hooks/helpers, remote
publication, provider settings, review/check results or external side effects.
Native jj operation history is separate from disk payload retention. Reporting
unsupported recovery is required, not a reason to substitute a broader action.
