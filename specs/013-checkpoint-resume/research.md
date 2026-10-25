# Research: Checkpoint, Resume and Selective Undo

> **Owner-decision amendment — 2026-10-25:** all former OQ IDs are decided;
> [current profiles](../016-stacked-pr-coordination/contracts/owner-decisions.md) supersede earlier undecided recommendations.
> Saved-plan metadata defaults to .work; selected bytes/safety copies remain external.
> This is a documentation target, not implementation or native acceptance.

**Date**: 2026-10-25. Design research uses repository source and cited official
documentation; no native runtime experiment or product acceptance was executed.
The owner adopted the recommendations on2026-10-25; concrete native capabilities remain unmeasured.

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

Spec 012 stack observation supplies optional sampled identities. Its dated
[OQ-NET decision](../012-stack-inspection/spec.md#owner-decisions-2026-10-25)
selects `--online` only, default offline with no alias. Those opt-in remote reads
remain outside 013, which makes no network request. Spec 014 isolated integration preflight/replay creates no
checkpoint root or reusable candidate store; predicted-clean grants no apply.
Spec 015's proposed recovery adapter remains operation-specific, with plumbing/
CAS replay (D7) unchanged and publication handed to 016 (D8). Specs 020/022/025/030
provide bounded remote observations, not local ownership or rollback authority.
Spec 041 mode is an accident guard, not a security boundary; Spec 042 task
attribution is independent of commit identity. Spec 043 landing is human-only and
separate. Spec 011 recovery roots retain committed history within their limits.

**Decision**: revalidate each consumer's inputs and permissions. No observation,
checkpoint, local restore or undo authorizes remote writes or owner resync.
013target exits0/1/2 align with decided CLI revision2; CURRENT 015 code 3 persists until its explicit incomplete 1 mapping task.

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
head, no workspace working-copy commit effect (the invoker included), immediate pre/post head checks,
and observed divergent-head refusal without stw-initiated resolution. Reversing an older
operation over later work stays unsupported until a separate accepted capability.

jj provides no operation-head CAS. Recommend a mutating invocation explicitly
based on `--at-operation=EXPECTED_HEAD` under OQ-2, marked unmeasured: the new
operation can become a sibling of a concurrent native writer's operation. Pre/
post samples support detection and partial/changed-since reporting of observed
races, not prevention, exclusive ownership or detection of every ABA/integrated-
away change. Other native commands can later integrate divergent heads, including
before the post-sample; stw's no-auto-merge rule constrains its own requested
effects only. These residual limits must be accepted with native conformance,
and must appear in every jj mutation result; Git's one-ref CAS remains distinct.

## Owner decisions (2026-10-25)

All listed former OQ IDs are **DECIDED by the owner on 2026-10-25**. Unlisted
choices adopt the source recommendations; implementation/native measurement and
review gates remain, never inferred as passing. The concrete [owner profiles](../016-stacked-pr-coordination/contracts/owner-decisions.md)
distinguish saved-plan metadata, external capture payloads, trusted policy,
optional host-only signing and CLI exit-contract revision 2.

| Decision ID | Dated disposition | Remaining implementation/verification gate |
| --- | --- | --- |
| OQ-1 | DECIDED: adopt measured capture matrix recommendation: explicit ordinary Git tracked index/disk paths first, jj independently measured without implicit snapshot. | Unmeasured/special/index/conflicted/ignored capabilities remain unavailable. |
| OQ-2 | DECIDED: adopt narrow inverse/restore recommendations, occupied-destination refusal and native jj direct-parent/all-workspace/no-head-CAS limits. | Native semantics, signing and expected-state effects need conformance; no broad undo fallback or invoker working-copy change. |
| OQ-3 | DECIDED: selected bytes and independent safety copies remain EXTERNAL, private, quota bounded and separately deleted; .work is saved-plan metadata only. | Payload-role validation/capture/drop implementation and evidence; no automatic expiry. |
| OQ-4 | DECIDED: trusted workspace/task/native layout/actor policy supplies ownership; labels/config/session hints do not. | Missing/foreign/shared proof refuses affected local action. |
| OQ-5 | DECIDED: adopt characterized typed durability/root seam, separate namespaces and explicit stored restore plan; shared human/shortening-timeout vocabulary. | Implementation/migration/conformance remains; metadata defaults to validated workspace .work while --payload-dir stays explicit/external. |

## Needs owner decision

Compatibility anchor only: the IDs above are settled owner decisions, not open
questions. Remaining unavailable capabilities require implementation/measurement
evidence, not an invented additional owner answer.
