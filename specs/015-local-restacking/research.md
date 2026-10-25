# Research: local restack evidence, apply and recovery

> **Owner-decision amendment — 2026-10-25:** all former OQ IDs are decided;
> [current profiles](../016-stacked-pr-coordination/contracts/owner-decisions.md) supersede earlier undecided recommendations.
> Target exit revision 2 is 0/1/2; current code still 3. Optional actual re-sign is separately gated.
> This is a documentation target, not implementation or native acceptance.

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
schema or marking conformance passed would erase known behavior/evidence. The owner has now settled OQ-1 through the global exit answer: CLI exit-contract
revision 2 maps incomplete to 1 without a selector or JSON schema change.
CURRENT source still returns 3 until T045/T046 implement and verify that amendment;
this document changes no code.

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
and occupied-worktree scope are adopted OQ-3/OQ-6 owner decisions; native effects still need proof.

## R2a: actual-candidate signatures (review F3)

**Owner decision 2026-10-25:** default actual candidates remain unsigned with
explicit --no-gpg-sign and neutralized repository signing config. Default refuse
source-signature loss/unmet/unknown policy. Optional separately authorized
actual re-sign uses ONLY the measured fixed trusted HOST helper/verifier profile,
no stw keys and no unsigned fallback; absent helper disables it. The 015 adapter
owns this code once, 016 only consumes final verified receipt. Build signed parents
bottom-up, record old→unsigned→signed/loss/replacement, verify final chain and
compute final actual head BEFORE CAS/review. 013/014/D9 prediction stay helper-free.
No helper call or native acceptance was performed by this documentation.

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
the write and recovery interfaces; OQ-4 is adopted measure-before-enable direction.
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
`base-mismatch` findings. `restack-needed` is the published [Spec 035 watch event](../035-stw-watch/contracts/watch-events.md).
The merged 012 design uses the distinct nested finding code `restack-advised`;
the published 035 event code is unchanged. Consumers validate producer, kind,
version and exact inputs rather than diagnostic names. All are observations,
not local or remote authorization. Native-stack management must not be raced.
**Alternatives considered:** local mutation authorizes lease push; a local old
head is treated as observed remote head; name matching or a closed PR proves
integration; 015 executes owner resync/retarget/landing. All violate the boundary.
Owner decisions adopt 012's offline default and only explicit bounded `--online`
reads, with no old alias. `--after-fetch` remains separately selected local cache
observation. 015 remains offline with supplied qualified evidence only.

## Adopted research and remaining measurements

All eight stable OQ IDs are [owner decisions](spec.md#owner-decisions-2026-10-25)
dated 2026-10-25. The recommendations are adopted with trusted workspace identity/
review policy, immutable private action metadata and measured native capability
gates. Optional host-only signing and the exit revision have explicit new tasks;
none of these directions is implementation or native acceptance.
The installed Spec Kit specify/plan/tasks structure is applied to this existing
feature with explicit feature selection and no persistence. No extension hooks,
presets or template overrides are configured at the inspected base. Plan and task
setup preserve existing files; final analysis is read-only. This record is a
design proposal and source audit, not an independent review or acceptance run.

## Dated exit/signing decision implementation boundary

Owner 2026-10-25 chooses target CLI revision 2 0/1/2 with no legacy 3 selector, not
a new --contract flag prerequisite or JSON schema change. CURRENT root.go still
maps restackIncomplete to 3; T045/T046 own red tests/implementation. Optional
actual re-sign is 015-owned T047/T048 using fixed trusted HOST helper/verifier,
no stw keys and no unsigned fallback. D9 predictions and 013/014 stay unsigned/
helper-free. Actual signed parents are rebuilt bottom-up and verified final SHA
exists before CAS/review; 016 consumes receipts only. Adopted directions are not
native/helper acceptance.

## Optional host-helper measurement gate (T049)

T049 records the separately authorized disposable capability measurement before
T048 may enable signing. The host/verifier profile, confinement and bounds,
external-agent trust limits, final signed-chain mapping and public verification
must all have observed evidence; an unmeasured lane remains unavailable. This
section is a task destination, not a measurement result or authorization.
