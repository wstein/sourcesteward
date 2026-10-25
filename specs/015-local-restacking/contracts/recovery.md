# Proposed contract: restack journal and recovery

**Status (2026-10-25): UNPUBLISHED, unimplemented.** Reuse
[Spec 032 journal conventions](../../032-stw-sync/spec.md#journal-and-restore)
and Spec 013 checkpoint/restore semantics. This is a proposed typed integration,
not a claim that `internal/stwsync` exports a shared restack API today.

## Existing evidence and planned seam

At base `3d4c867a451217a2942564a7dcb4736d45a09b70`,
`internal/stwsync/journal.go` has private `sync` records under
`<git-common-dir>/stw/sync`, entry states `prepared`, `completed`, `failed`,
create-only `refs/stw/recovery/sync/RUN_ID/N`, known planned/new ids, file sync,
atomic rename and directory-sync attempt. Restore validates the record type,
ids and branch scope and compares current/old/planned-new values. Native sync
is Git-only. That implementation is evidence of the pattern, not proof of the
stronger restack durability, private-state or native jj requirements.

The proposed seam extracts/reuses those durability, recovery-root and typed
record-validation rules with unchanged sync characterization tests; it must
not copy another independent journal writer or reinterpret a sync record as a
restack action. Proposed restack records use a distinct `restack` record type,
versioned envelope and namespace `refs/stw/recovery/restack/RUN_ID/N`, inside the
same recovery architecture. A typed restack storage location/index and migration
must be reviewed with Specs 032/013 before implementation (OQ-5). Existing sync
files and retention behavior stay unchanged.

## Durable action record

For each one-ref action store immutable plan id/digest, run/action id, native
repository and task ownership bindings, exact target ref, old parent/head,
new parent, known actual candidate/new head, mapping/evidence digest, recovery
root and per-stage outcome. Author/committer policy and operation identity for
jj are explicit. Paths and refs in records are data, never executable commands
or authority to operate on a different repository.

Before the target ref move: create the old-head root without overwriting any
existing ref; durably write intent and the known candidate id. A collision,
unsafe path, missing object, directory sync failure or record failure stops the
target move. If a crash leaves a root without a durable record, retain and report
it for inspection; a root alone authorizes no restoration or cleanup. Recovery
roots preserve committed objects only, not untracked files or worktree content.

After the move: record completion and actual resampled ref. A completion-write
failure means `partial`/unknown, not an implicit rollback. The original record
and root stay available. Shared record identities and checks prevent a retry
from silently becoming a new run with a second action.

## Reconciliation and retry

| Record/native observation | Report and permitted next step |
| --- | --- |
| Valid known candidate, current target equals old head | No move proven; after fresh guards an explicitly authorized retry may perform the one planned CAS |
| Valid known candidate, current target equals recorded candidate/new head | `already-applied`; complete missing evidence/journal only after binding validation, never replay again |
| Current target differs from both old and known new | `changed-since`; preserve all roots, no forward replay or restore |
| Missing/unknown candidate or missing recovery root, malformed/foreign record, unreadable native state | `journal-incomplete` or typed record refusal; preserve and inspect; no inferred success |
| Old and new indistinguishable or an observed ABA/ownership/config race | Unknown/refusal; equality of a later sample cannot prove no intervening edit |

Retry is not permission to fix a partially updated owner workspace: no owner
workspace is updated by this slice. No automatic rollback, reset, abort, GC,
root expiry or retention deletion occurs. Spec 032 keeps ten completed sync
runs; that number is not silently adopted as restack deletion authority.

## Separately authorized restore

Proposed syntax, not an implemented command:

```text
stw restack restore-plan --run RUN_ID --state-dir DIR [--json]
stw restack restore --plan RESTORE_PLAN_ID --state-dir DIR --authorize-local [--json]
```

The restore plan names one exact action/root, repository, current expected new
head, retained old head, ownership and only the reverse ref movement. Revalidate
all apply guards: owned unoccupied topic ref, native layout, protections,
configuration/hooks, attribution and recovery-root value. Current equals recorded
new head: CAS new→old; equals old: `already-restored`; anything else:
`changed-since`, unchanged. A symbolic/dangling/replaced ref or root refuses.
Never delete a ref to achieve restore and never overwrite the owner's checked-out
branch. Journal the restore itself before the reverse movement and retain both
old and new committed roots where needed by the reviewed recovery design.

Spec 013 owns checkpoint comparison and selective undo. It can reference this
run/action and native mapping; a selective undo is a new reviewed action, not
an automatic rewrite of a journal entry or a blanket `jj op undo`.

## Recovery limits

No restoration promise covers unsaved buffers, untracked/ignored files,
worktree/index changes, arbitrary hooks/config execution, object mtimes,
expired/deleted roots, unreachable unpinned objects, provider state, pushes,
PR base edits or review/check results. This slice runs none of those original
worktree or remote effects. Recovery cannot revoke a remote publication. Partial
outcomes retain evidence and exact limits; restoration is not a transaction
across native metadata and private files. Native jj recovery needs its own
operation/bookmark capability proof, not Git recovery refs as a fallback.

The future result/exit contract is the [apply proposal](restack-apply.md#proposed-result-shape-and-exit-status).
