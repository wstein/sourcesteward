# Quickstart Validation: Checkpoint, Resume and Selective Undo

> **Owner-decision amendment — 2026-10-25:** all former OQ IDs are decided;
> [current profiles](../016-stacked-pr-coordination/contracts/owner-decisions.md) supersede earlier undecided recommendations.
> Saved-plan metadata defaults to .work; selected bytes/safety copies remain external.
> This is a documentation target, not implementation or native acceptance.

**Future validation guide, 2026-10-25.** All 013 command forms are proposed and
unimplemented. No scenario below was executed for this documentation amendment;
no product acceptance, build, live mutation or contract publication is claimed.

## Prerequisites and safe fixtures

Apply the dated [owner decisions](research.md#owner-decisions-2026-10-25), review the design,
implement red-first tasks, then use disposable temporary repositories/workspaces
and a separate private state directory. Pin the minimum/current admitted Git/jj
versions and required platforms in the acceptance record. Do not use an owner's
checkout, forge credentials or a network remote. A missing native executable,
sandbox or required lane is a blocker, never a skipped pass.

Create ordinary and linked Git fixtures and standalone/colocated/secondary jj
fixtures. Use fixture-only identity via the process environment, not persistent
Git config. Seed committed text, binary, symlink and deletion cases plus different
staged and unstaged bytes, explicit untracked/ignored entries and untouched
sentinel files. Keep secrets fake and use inert malicious strings only. Native
special states remain unsupported until separately admitted. Capture before/
after digests of files/index/refs/config/object roots and jj stored working-copy/
operation log; track private state writes separately from repository effects.

## Scenario matrix

| Scenario and proposed steps | Required observable outcome |
| --- | --- |
| US1.1: run `stw checkpoint plan` with explicit workspace/state directory but no path/layer selection | `nothing-selected`, zero capture/native/store mutation and no prompt |
| US1.1–1.3: plan exact tracked index/disk pairs, then separately choose an untracked/ignored path only where admitted | Included/excluded paths/layers/types/sizes and limits shown; no unrelated bytes or task context harvested; unsaved render has no writes/id; explicit --save alone permits the disclosed validated private plan write |
| US1.2: apply the exact stored plan with `--authorize` | Selected payload/receipts and typed evidence durable before complete success; index/disk/native effects match capability disclosure, unrelated sentinels preserved |
| US1.3: point at a forbidden sensitive/control path or inject context instructions | Typed refusal; no credential/environment/transcript collection, no execution |
| US1.4: use umask 0000; try a repository-local/linked-worktree, foreign or 0666 store; fill both quotas; then plan/authorize/confirm drop of one exact checkpoint and crash mid-drop | New state is 0700/0600; unsafe locations/modes/quota refuse. Drop touches only exclusive owned files/013 roots and reports remaining receipts/bytes; no payload content is logged or printed. |
| US2.1: run `stw checkpoint resume --checkpoint ID` offline | Saved/current ids and Git staged/disk or jj stored/disk differences; zero record/refs/index/disk/jj snapshot/oplog mutation |
| US2.2: create selected current edits, corrupt/miss a payload/root, or change ownership; plan and attempt selected restore | Explicit stale/unavailable/unsupported refusal, no overwrite and no implicit fetch/repair |
| US2.2: plan an admitted unchanged selected restore, then --save and authorize that exact restore plan separately | Only selected effects, pre-action recovery receipt before mutation, precise native limits; no inherited capture approval |
| US2.2: restore over uniquely edited current work, complete it, then drop the source checkpoint | Original capture may drop, but independent safety-copy bytes, records and undo/safety roots remain recoverable. Deletion requires a separate plan naming and confirming SAFETY_ID; no completion-triggered cleanup. |
| US2.3: crash after payload/root/intent/native effect and before completion, then inspect/reconcile | Per-action completed/not-started/unknown truth; retained incomplete evidence; known desired state does not replay; unknown new head never resets |
| US3.1: run complete observation/capture/error cases in human and JSON modes | Same native facts/outcome/limits, escaped terminal text and reversible path bytes; complete fixtures validate future unpublished contract |
| US3.2: select jj colocation, unavailable jj, ambiguous ownership and GitButler | Native jj or explicit refusal as applicable, zero raw Git write fallback |
| US3.3: exceed each bound, corrupt a record, timeout/cancel between effects or fail output | Exit 1 with incomplete/partial truth, no truncated applicable plan, no further action; bad syntax is exit 2 |
| US3.4: install fake hook/helper/alias/merge-driver/model/network sentinels | Zero undeclared execution/requests; hostile data escaped and inert; configuration hazard refuses |
| US3.5: omit a required backend/version/platform execution | Evidence record says blocked/unrun; acceptance remains incomplete |
| US4.1: plan the same target once as `inverse-change` and once as supported history/operation restoration | Distinct explicit native effects, full paths/layers/refs/descendants, original/new identities and recovery limits |
| US4.2: omit/wrongly select merge parent; create shared/foreign/unknown target, unselected descendant or concurrent edits | Typed refusal; no first-parent guess, broad op undo, remote lookup or target rewrite |
| US4.2: hold an inverse destination in the caller's clean workspace or another linked workspace; dirty selected paths or hide occupancy evidence | Changed-since refusal with exact blocker and unchanged refs/index/disk; admitted unoccupied Git candidates use private plumbing quarantine, no real-worktree revert or stale held ref. |
| US4.2: select a jj parent view with later work in another workspace, divergent heads or an abandoned/GC'd target, and race the operation head before/after apply | All-workspace evidence and pinned `--ignore-working-copy` reads/operations; only an admitted sole-head/direct-parent case can apply, every workspace working-copy commit change, including the invoker, refuses; missing operation is unavailable and an observed post-effect race is partial with no stw-initiated merge; jj has no head CAS and other native commands can integrate heads. |
| US4.2: choose a restored view that changes only the invoking jj working-copy commit, with snapshot suppression | Refuse before mutation. Otherwise the unchanged disk could become stale and a later update-stale could rewrite it outside the plan. No implicit update-stale; a future stale/update capability needs separate admission, selection and pre-action byte safety capture. |
| US4.3: inject a concurrent jj operation between the pre-check and the proposed pinned mutation, then sample heads; also fail the post-check or integrate heads externally before it | Observed race/unknown post-check is partial/changed-since with retained safety evidence and zero stw-initiated merge. Every jj mutation result discloses no head CAS, sampling may miss concurrent/ABA/integrated-away changes, and later external native commands may merge heads; no prevention or always-detect claim. |
| US4.3: authorize an admitted undo, force conflict/crash after a selected effect, then inspect | Only selected effects; partial/unknown state recorded, no automatic reset/abort/rollback; separately planned recovery only |

## Evidence and mutation controls

For each admitted capability record exact backend/platform/version, fixture name,
red-before-green proof, observed effects, command/result digest, outcome and
limitations in future `verification.md`. Record sync v1 regression behavior and
published contract byte digests separately. Keep 013 checkpoint/undo roots and
receipts beside more than ten finished sync runs: retention must leave all 013
state unchanged; collision or foreign-root substitution must refuse. Mutation probes must fail when the
implementation removes explicit selection/authorization, skips expected-state
or ownership guards, marks a payloadless checkpoint complete, broadens jj undo,
uses raw Git for jj, resets unknown new heads, drops incomplete evidence, runs a
helper/hook/network call or treats missing conformance as passed.

Compare sentinel evidence after every success and refusal. Re-run capture and
resume independently of undo, and undo from typed native fixtures independently
of an existing user checkpoint. Confirm proposed 013 exits 0/1/2 while historical
CURRENT hidden015code3 persists until decided target revision2 incomplete 1 task. Keep required unexecuted lanes
and unsupported capabilities visible. This guide grants no release, activation,
real-workspace apply, remote write or Spec 043 landing authority.
