# Research: Checkpoint, Resume and Selective Undo

**Date**: 2026-10-25. Design research uses repository source and cited official
documentation; no native runtime experiment or product acceptance was executed.
Recommendations below are proposals, not owner decisions.

## Source facts and design choices

### R1 — Reuse recovery durability without inventing an existing generic API

**Evidence**: `internal/stwsync/journal.go` defines private `journalRecord`,
`journalEntry` and `journal` types. A `type: sync`, `schema_version: 1` record
lives in `<git-common-dir>/stw/sync/RUN_ID.json`. Before a branch move it creates
`refs/stw/recovery/sync/RUN_ID/N` at the old OID, create-only, and durably writes
`prepared` with old/planned-new values. Entries also carry actual new OID,
worktree, original HEAD, stash OID and reason. Completion is written after the
effect as `completed` or `failed`. Writes sync the file, rename atomically and
attempt directory sync; this is not proof of fail-closed directory durability.

**Decision**: specify a proposed typed integration/extraction of those mechanisms
with unchanged sync characterization, rather than claiming private sync v1 is a
generic exported checkpoint journal or copying its writer. New record types and
payload receipts need reviewed validation/storage migration (OQ-5).

**Alternatives considered**: another checkpoint journal writer would duplicate
crash/validation bugs; reinterpreting `sync` records would conflate authority and
retention. Both rejected by this design.

### R2 — State the narrow implemented restore and retention limits accurately

**Evidence**: `internal/stwsync/restore.go` validates sync type/run/ref/OIDs,
compares current tip with old or recorded new/planned-new, and reports changed
state rather than overwriting it. The unoccupied-ref path uses expected-value
`update-ref --no-deref`; its clean idle main-worktree path uses `reset --keep`.
This latter path is Spec 032's behavior only, not 013 apply permission. A prepared
fast-forward with a known planned tip can reconcile. A prepared rebase with no
known new tip reports `journal-incomplete`; an unknown new head must never be
blindly reset. `restored` marks an incomplete entry handled by restore.

The retention code retains the current plus up to nine other finished records
(ten completed/finished runs in the spec vocabulary); prepared entries stay
protected. Recovery-ref removal validates its exact run namespace and expected
old OID, preserving the record when roots remain. This existing sync policy does
not authorize deletion of 013 payloads, restack records or arbitrary roots.

**Decision**: carry forward expected-state/recovery principles, preserving
operation-specific behavior. Existing sync restore does not restore untracked
files, general index/worktree edits, pruned remote-tracking refs, stash apply/drop
or submodule/configuration effects. A checkpoint needs distinct selected bytes.

### R3 — Separate native capture evidence from recovery promises

Git can distinguish committed tree, index and disk layers; jj has working-copy,
change/commit and operation identities. Existing native read-only inspection
suppresses jj snapshots and operation-log mutation. Therefore resume comparison
must observe stored vs disk state explicitly without creating a new jj operation.
A jj operation ID alone does not prove saved disk bytes or selective restoration.

**Decision**: require per-backend capability admission and explicit selection
for each layer; unsupported states refuse without fallback. Do not infer mutation
capability from existing read-only support. Exact capture/undo mechanisms remain
unmeasured here (OQ-1/OQ-2), including conflicts, binaries and index extensions.

### R4 — Adjacent evidence never delegates authority

Spec 012 stack observation supplies optional sampled identities; its proposed
opt-in `--pull-requests` reads remain outside 013. [Spec 012 OQ-NET](../012-stack-inspection/spec.md)
keeps online scope and flag naming open; 013
makes no network request and does not settle that contract. Spec 014 isolated integration preflight/replay creates no
checkpoint root or reusable candidate store; predicted-clean grants no apply.
Spec 015's proposed recovery adapter remains operation-specific, with plumbing/
CAS replay (D7) unchanged and publication handed to 016 (D8). Specs 020/022/025/030
provide bounded remote observations, not local ownership or rollback authority.
Spec 041 mode is an accident guard, not a security boundary; Spec 042 task
attribution is independent of commit identity. Spec 043 landing is human-only and
separate. Spec 011 recovery roots retain committed history within their limits.

**Decision**: revalidate each consumer's inputs and permissions. No observation,
checkpoint, local restore or undo authorizes remote writes or owner resync.
Proposed 013 exits 0/1/2 leave implemented hidden 015 v1 exit 3 unchanged.

### R5 — jj operations span the repository and are version-dependent

The [official operation-log design](https://github.com/jj-vcs/jj/blob/main/docs/operation-log.md)
describes a repository-wide view, including each workspace's working-copy
commit; concurrent writers can leave divergent operation heads. A current view
is therefore not private to the invoking workspace. The [official CLI reference](https://docs.jj-vcs.dev/latest/cli-reference/)
distinguishes creating a new operation restoring an earlier view (undoing all
later operations) from reversing one earlier operation on top of current state.
These are not interchangeable selective undo semantics. Abandoned operation
history and unreachable objects may later be garbage-collected.

The same CLI reference documents snapshot suppression and recommends
`--at-op=@ --ignore-working-copy` for nonmutating operation-log inspection.
The existing `internal/inspection/jj.go` query path explicitly combines
`--ignore-working-copy` with `--at-operation`, first discovering then pinning
full ids. 013 requires both protections and a read-only sandbox for reads;
restore/undo invocations also suppress implicit working-copy snapshots/updates.
Missing target operations/objects are `unavailable`, never reconstructed by
fetch, auto-merge or a different operation. No abandon/GC runs here.

**Unmeasured**: no native operation experiment was performed for 013. Latest
upstream documentation is design context, not admitted argv or version evidence;
`op restore`, `op revert` and older `op undo` spellings/semantics need exact
minimum/current version conformance under OQ-2. Recommend the narrow first
operation-restoration slice only for the direct parent of one current operation
head, no other workspace working-copy effect, immediate pre/post head checks,
and divergent-head refusal without automatic resolution. Reversing an older
operation over later work stays unsupported until a separate accepted capability.

## Needs owner decision

| Id | Choice and recommendation | What remains blocked |
| --- | --- | --- |
| OQ-1 | Approve an initial native capture matrix after capability spikes. Recommend Git explicit ordinary tracked index/disk paths first; independently prove jj selected disk payload plus native identity capture without implicit snapshots. Keep conflict/index-extension/special-file/ignored capture unsupported until measured; no false parity. | Backend capture enablement; exact commands, admitted versions and special-state rules |
| OQ-2 | Choose undo capabilities and descendant policy. Recommend explicit inverse changes on owned local history first; for Git compute inverse commits by fixed plumbing in private object quarantine with reviewed promotion/CAS, never porcelain revert in a real worktree. Refuse a destination checked out/working-copy-used in any workspace (including the caller's) or with dirty selected paths, as changed-since with exact blocker; native jj needs its own proved mechanism; separately enable history/operation restoration only after proving exact target/descendant scope and expected-state checks. For jj recommend only the single current operation head's direct parent as the desired restored view, no other workspace working-copy effect, explicit pinned/snapshot-suppressed invocation, immediate pre/post head checks, divergent-head refusal and missing/GC'd operation unavailable (R5); older-operation inverse application is a different, initially unsupported capability. Broad `jj op undo` is never a selective fallback. Decide required-signature/source-signature handling for newly created inverse/history commits: disclose loss of signatures, refuse unmet signing policy, and run no signing helper. Spec 043 OQ6 is related context, not approval. | Undo mutation adapters, operation-restoration admission and signing capability |
| OQ-3 | Choose payload location, quota and retention/deletion policy. Require explicit 0700 directories/0600 files independent of umask, current-actor ownership and fail-closed canonical/native/layout proof outside repositories/worktrees. Recommend a caller-selected external store, 32 MiB per-checkpoint payload and 1 GiB total physical-store quota counting all metadata/staging/orphan/incomplete bytes. Recommend exact-checkpoint drop-plan/drop with stored plan, authorization and checkpoint-id confirmation; independent pre-action safety-copy ids/records/retention survive completed restore and checkpoint drop, with a separate named/authorized/safety-id-confirmed deletion plan only; dependency/expected-value guards and partial receipts remain mandatory. No automatic expiry/eviction or secure-erasure claim. Journal metadata retention is not byte retention. | Durable selected-byte store, total quota and explicit drop interface; no adoption of sync's ten-run deletion policy |
| OQ-4 | Select trusted ownership/authorization inputs for stored plans, payloads and affected refs/changes. Recommend Spec 010 task registration plus native layout/position evidence, explicit caller identity and current actor binding; missing/foreign/shared evidence refuses. Config/environment identity and session-name matches alone never prove ownership. | Apply admission and shared/published-history classification when offline proof is incomplete |
| OQ-5 | Review the typed recovery seam, versioned record envelope, namespace/location and migration across 032/013/015, plus proposed 013 command spellings/stored-plan interface and common flag admission. Recommend extract durable-write/root/validation primitives under characterization tests; keep sync v1 and operation-specific restore/retention behavior unchanged. New writers require fail-closed durability evidence. Reserve distinct `refs/stw/recovery/checkpoint/CHECKPOINT_ID/N` and `refs/stw/recovery/undo/UNDO_ID/N` prefixes, plus the independent `refs/stw/recovery/undo/safety/SAFETY_ID/N` subtree and self-contained safety-copy records; checkpoint drop cannot enter that subtree. Never enter sync/tidy/v3/restack ownership. Recommend a proved native jj commit/op-log retention mechanism without visible bookmarks; root-dependent recovery stays disabled until that proof exists. Recommend a separately stored selected-byte restore plan; exact `restore-plan`/`restore` vs plan/apply spelling, explicit `--human` alias and bounded `--timeout` override remain interface choices, not approval. | Shared extraction, new checkpoint/restore/undo record writers and CLI/interface admission |

The proposed numeric protocol bounds in the contract are conservative design
limits, not measured throughput. Implementation review may narrow them; widening
a bound or admitting a new effect requires contract/test review. These unresolved
choices are recorded intentionally instead of inventing owner approval or
reopening settled Spec 015 decisions.
