# Research: local restack evidence, apply and recovery

**Created**: 2026-10-25. Documentation/source inspection only; no new product
experiment, native conformance, build or remote operation was executed. Historical
measurements retain their original dates and limits in [verification](verification.md).

## R1: preserve implemented hidden v1

**Decision:** retain the 2026-10-21 D4–D9/D12/D13 choices and historical evidence.
**Rationale:** base `3d4c867a451217a2942564a7dcb4736d45a09b70` has
`internal/restack`, hidden `internal/cli/restack.go`, `schemas/v1/restack-plan.schema.json`
and the schema-freeze digest
`da0bdc60273a30a0f2e39482f534cb80f15f619451f2167ad0780cc44a2b5b48`.
The CLI maps complete/refused/usage/incomplete to 0/1/2/3. The fixed prediction
committer lives only in quarantine; the actual limitation
`rewrite-committer-not-predicted` remains reserved until T024. T001 was design
CLEAR at its recorded historical SHA, not this amendment's review. T004 was
skipped deliberately; T021 minimum/current and T023 exact-head review are open.
**Alternatives considered:** silently replacing v1 with a generic 0/1/2 apply
schema or marking conformance passed would erase known behavior/evidence. OQ-1
requires an explicit future selector/migration decision; no v1 change is made.

## R2: exact replay semantics with plumbing

**Decision:** preserve D7 quarantine/plumbing and a local CAS.
**Rationale:** exact `old_parent..old_head` onto full `new_parent` expresses the
owner's `--onto` intent without porcelain hooks/worktree effects. A complete
actual candidate must pair every moved commit `=` and pass own-diff and actual
author/committer checks; tree equality alone can hide dropped or reordered work.
The v1 observation cannot predict a real committer, so actual replay evidence
and SHA appear only after candidate creation. The derived change class/purity
feeds AGENTS.md: docs-only rewrite requires green local checks (and pure-rebase
equality) without pre-push review; code requires exact-new-SHA independent review
before push, middle tier allowed for equal pure rebase. Push-time identity
covers observed remote-default..actual-new-head, including open-parent commits,
against the trusted owner alone, with local/global config-origin evidence and
no caller-widened identity set or configuration write. Ready always requires
independent exact-head CLEAR and green required checks. No review blocks the
first local replay; Spec 043 human `--reviewed` remains its own owner statement.
**Alternatives considered:** executing porcelain rebase, updating all descendants,
using aggregate patch ids or treating `evidence-clean` as push permission are
excluded by D7, ownership or the semantic-break limitation. Actual committer/date
and occupied-worktree scope remain OQ-3/OQ-6 proposals, not new owner decisions.

## R2a: actual-candidate signatures (review F3)

**Decision:** proposed actual candidates are unsigned, with explicit
`--no-gpg-sign` and signing-program/config/environment neutralization. Never run
a signing helper. Report each source `gpgsig`/`gpgsig-sha256` as
`signature-dropped` when the actual candidate has no signature, even for `=` rows.
**Rationale:** PR #238's independent reviewer reported a Git 2.56.0 scratch
measurement: `commit-tree` with `commit.gpgSign=true` did not sign. The author
has not rerun that product experiment; this is attributed review evidence,
not new verification. Patch equality does not preserve cryptographic signatures.
**Alternatives considered:** silently dropping signatures or executing a signing
program would hide an effect. OQ-8 recommends refusing signed/required-signature
scope pending an explicit refusal/disclosure policy; signing is not added. See
Spec 043 OQ6 for its separate human landing question.

## R3: reuse recovery architecture

**Decision:** propose a typed restack extension through the Spec 032 durability
and recovery-root architecture, consumed by Spec 013 checkpoints/restores.
**Rationale:** source inspection of `internal/stwsync/journal.go`/`restore.go`
shows Git-only private journal types, prepared/completed/failed entries,
create-only `refs/stw/recovery/sync/RUN_ID/N`, old/planned-new/new values, file
sync/rename and a directory-sync attempt. An exact known new id makes crash
reconciliation conditional rather than blind rollback. Existing helpers are
not an exported common API and do not establish stronger restack durability.
**Alternatives considered:** copy another journal writer; replay arbitrary sync
records; auto-reset; adopt ten-run sync pruning as implicit restack deletion
permission. All rejected. OQ-5 covers storage/index/retention integration; incomplete
roots stay retained and no pruning is enabled by the first apply proposal.

## R4: peer capabilities without false equivalence

**Decision:** retain native jj refusal in v1 and propose a separately measured
native operation/bookmark capability before enabling jj apply.
**Rationale:** Spec 008/009 stored-unsnapshotted identity rules, Spec 040 bounded
functional-core design, Spec 041 ownership-as-view and Spec 042 attribution do
not prove exclusive operation publication or bounded jj descendant rewrites.
Commit, change and operation ids must stay distinct. A native spike must establish
the write and recovery interfaces; OQ-4 is unresolved.
**Alternatives considered:** Git fallback in colocated jj; borrowing Git ref CAS;
calling generic `jj op undo` as safe selective recovery. None establishes native
scope/recovery safety, so unsupported remains explicit.

## R5: stack toolbelt boundaries

**Decision:** observation (012) → optional isolated integration preflight/replay
evidence (014) → local restack plan/apply (015) → separately authorized forge
effect plan/apply (016). Checkpoint/restore/selective undo (013) reuses 032 durability.
**Rationale:** 011 protects worktree/reference ownership, 020/022 bound selected-host
reads, 025/030 bind PR/CI evidence without Ready/merge inference, 042 separates
attribution from authentication, and 043 is the human owner's distinct landing
invocation. D6 preserves direct explicit parent/head input without a hard 012
or 014 implementation dependency. Spec 012 at the inspected main has `parent-merged-child-stale` and
`base-mismatch` findings. `restack-needed` is a [Spec 035 watch event](../035-stw-watch/contracts/watch-events.md)
and is proposed as a 012 finding in pending [PR #237](https://github.com/wstein/sourcesteward/pull/237);
that pending vocabulary is not a stable-main 012 contract. All are observations,
not local or remote authorization. Native-stack management must not be raced.
**Alternatives considered:** local mutation authorizes lease push; a local old
head is treated as observed remote head; name matching or a closed PR proves
integration; 015 executes owner resync/retarget/landing. All violate the boundary.
OQ-7 retains settled 012 opt-in `--pull-requests` reads despite ambiguous strict
no-network phrasing; 015 remains offline with supplied qualified evidence only.

## Remaining research and decisions

[Needs owner decision](spec.md#needs-owner-decision) lists OQ-1 to OQ-7 with
recommendations and affected blockers. No missing authorization, identity source,
review issuer, freshness lifetime, retention policy or native API is invented.
The installed Spec Kit specify/plan/tasks structure is applied to this existing
feature with explicit feature selection and no persistence. No extension hooks,
presets or template overrides are configured at the inspected base. Plan and task
setup preserve existing files; final analysis is read-only. This record is a
design proposal and source audit, not an independent review or acceptance run.
