# Tasks: Stacked PR Coordination

> **Owner-decision amendment — 2026-10-25:** all former OQ IDs are decided;
> [current profiles](contracts/owner-decisions.md) supersede earlier undecided recommendations.
> Default saved-plan metadata is .work; remote authority never runs local signing.
> This is a documentation target, not implementation or native acceptance.

**Status**: Future work only; all 38 tasks are unchecked. No new implementation,
conformance or review result is implied by this list. Paths below are proposed
future source/evidence files. Owner-decision gates in research block the affected
write work; no new dependency is required. TDD applies: execute meaningful failing
behavior tests before each implementation and record real outcomes.

## Phase 1: Setup and capability decisions

Confirm source facts, accepted choices and native gaps before affected work.

- [ ] T001 Characterize existing hosting/mode/D5 behavior and frozen published/hidden-v1 contracts in `internal/stackpublish/compatibility_test.go`; characterize CURRENT hidden 015 incomplete exit 3 separately from decided revision-2 target 1 and no transport writes; characterize separately tracked issue #241 without claiming a docs change repairs replay code. (FR-011, SC-001, SC-003, SC-004).
- [ ] T002 Record accepted owner decisions or explicit blockers in `specs/016-stacked-pr-coordination/research.md` before affected surface/trust/signing/store/write work; do not treat recommendations as approval. (FR-017, FR-021, FR-022, FR-023, SC-004).
- [ ] T003 Define the narrow operation/hosting architecture seam and no generic execution/network escape in `internal/stackpublish/architecture_test.go`; write failing guard/mutation probes before any allowlist change. (FR-021, FR-027, FR-030, SC-002, SC-008).
- [ ] T004 Record native Git/jj minimum/current version/platform capabilities and unsupported publication shapes in `specs/016-stacked-pr-coordination/verification.md` (future file); missing lanes stay gaps. (FR-007, FR-008, FR-011, SC-001).
- [ ] T005 Write failing backend/colocation/GitButler and distinct native identity tests in `internal/stackpublish/backend_test.go`; prove native jj publication refusal never invokes Git. (FR-007, FR-008, SC-001, SC-002).
## Phase 2: Foundations

Immutable models, bounded validation, own scope and action-specific gates block all stories.

- [ ] T006 Write failing canonical plan/id/selection/intent tests in `internal/stackpublish/plan_test.go`: one repository, 16 actions, empty selection, changed effects give new id, no authorization inheritance; unsaved default has saved=false/null id and zero store/plan writes, explicit --save uses the validated default .work metadata store or explicit --state-dir, empty selection never saves and apply rejects unsaved/missing ids; empty plan still binds one resolved repository, null-repository complete plan is invalid, and bare CLI help emits no JSON result. (FR-003, FR-009, FR-012, FR-022, SC-002, SC-004).
- [ ] T007 [P] Write failing limit/cancellation/index-order tests in `internal/stackpublish/bounds_test.go`: 50 requests, 10 pages/collection, 1000 records, 1 MiB raw/normalized/result, 64 KiB input/body, 60 s total, 30 s request/push, 5 s local read, 512-byte display, 2 KiB sanitized error, at most four read workers and one shared budget. (FR-010, FR-027, SC-002, SC-008).
- [ ] T008 Implement typed plan/result/limits and immutable validation and explicit-save persistence admission in `internal/stackpublish/model.go` and `internal/stackpublish/bounds.go`; enforce every T006/T007 constraint, explicit nulls, one human/JSON model, full 40-hex first GitHub publication ids and no id truncation. Pin schema_version 1, kind forge-effect-plan, action kinds push-topic/create-pr/edit-body/retarget/ready/native-link/native-unstack, state planned/blocked/prepared/completed/refused/failed/unknown/not-started and guard pass/fail/unknown/not-applicable; deferred comment cannot execute. (FR-003, FR-009, FR-010, FR-012, FR-022, SC-003, SC-008).
- [ ] T009 [P] Write failing per-action authorization/stale/permission tests in `internal/stackpublish/guards_test.go`; exact plan plus authorization for EACH action, local --authorize never remote permission. (FR-004, FR-012, SC-002).
- [ ] T010 Implement pure sequential action guard evaluation in `internal/stackpublish/guards.go`; fail unknown relevant evidence before intent/write and reject extra/duplicate unauthorized action ids. (FR-003, FR-004, FR-012, SC-002).
- [ ] T011 [P] Write failing ownership/mode/D5 tests in `internal/stackpublish/ownership_test.go`: own task only, every real D5 row, unreadable versus absent record, Codex session absent, no public session id. (FR-013, FR-028, SC-002, SC-008).
- [ ] T012 Integrate trusted task ownership and 041 mode/042 D5 through `internal/stackpublish/ownership.go`; no caller-label/prefix/author hint widens agent own branches/PRs/bodies. (FR-013, FR-028, SC-002, SC-008).
## Phase 3: US1 — Review relationships and selected effects (P1, offline MVP)

Independent test: supplied verified identities, no network by default, no selected effects without selection.

- [ ] T013 [US1] Write failing supplied-observation/identity/offline/composition tests in `internal/stackpublish/relationships_test.go`: names alone never verified, stale head/base and foreign repo refuse dependent actions, no implicit auth/fetch. Bind producer/command/kind/schema_version/exact inputs; feed a published 035 restack-needed event as 012 restack-advised finding and reject it without using its merge-commit new-parent as trunk/target. Reject 014 preflight without accepted full-H attribute-source/private-context binding or with unaccepted jj aggregate semantics, and reject any 013 checkpoint/safety-copy/jj-operation sample as remote authority or generic CAS. (FR-001, FR-002, FR-024, SC-002).
- [ ] T014 [US1] Implement bounded relationship consumption, explicit online reads and pure derivation in `internal/stackpublish/relationships.go`; use 040 indexed fanout for independent reads only, full source statuses and shared operation budget, independent explicit 015 receipt allowed. (FR-001, FR-002, FR-024, FR-027, SC-002, SC-008).
- [ ] T015 [P] [US1] Write failing native-rewrite/dependency/open-depth tests in `internal/stackpublish/dependencies_test.go`: pending/unknown rewrite blocks, true code dependency and bottom-to-top order, depth above two blocks publication, unrelated docs target default. (FR-019, FR-029, SC-002).
- [ ] T016 [US1] Implement selected-action/dependency derivation and empty-default offline MVP in `internal/stackpublish/plan.go`; explicit own stack order, no automatic replay/sync, no native rewrite competition. (FR-003, FR-019, FR-029, SC-002).
## Phase 4: US2 — Publish only authorized own work (P1)

Independent test: temporary bare remote/fake forge, exact native vectors, full-range policy and own-PR effects.

- [ ] T017 [US2] Write failing full-lease/ref/actual-review/owner-range/signature tests in `internal/stackpublish/push_test.go`: full remote-default..new-head includes foreign parent, unknown class becomes code, actual SHA before review, every rewrite-tier row, no helper/default/tag/force/bare lease. Recompute publication class from the union of per-commit paths across observed remote-default..actual-new-head; a docs-only child above an open code parent, parent code changes cancelled in the net tree diff, or unknown parent/path evidence must produce effective code even when the validated 015 moved-scope class is docs-only. Map only accepted 015 review_gate.change_class to the 016-only moved_scope_change_class alias after producer/kind/version/scope/head validation; reject fabricated alias, wrong-head/version and top-level/effective-field confusion. A later wording-only effective-code fix still requires the code gate because the workspace 2026-10-17 exception is explicitly unsupported here. (FR-006, FR-014, FR-015, FR-016, FR-017, SC-002, SC-005).
- [ ] T018 [US2] Implement publication receipt validation in `internal/stackpublish/publication.go`: validated source review_gate.change_class adapter to the 016-only moved-scope alias plus independently recomputed publication-range per-commit path union including open parents, effective code when either class is code/unknown, same-count all-= pure_rebase, docs/code tier mapping, trusted owner-only author+committer and config-origin checks, explicit unsigned/signature-dropped policy; block missing implementation/measurement evidence. (FR-015, FR-016, FR-017, SC-005).
- [ ] T019 [US2] Implement only the admitted own-topic native Git push edge in `internal/stackpublish/push.go`; exact full ref/full actual new SHA/full observed remote SHA lease, no automatic lease refresh, at most 200 identity commits, no missing-object fetch; keep absent-ref creation disabled until measured. (FR-004, FR-006, FR-014, SC-002, SC-005).
- [ ] T020 [P] [US2] Write failing fixed method/path/body/auth/redirect/endpoint/helper tests in `internal/hosting/stack_write_test.go`; current GET modes remain byte-compatible/read-only and all unaccepted writes make zero requests. (FR-021, FR-030, SC-002, SC-008).
- [ ] T021 [US2] After OQ-WRITES acceptance, implement only its reviewed additive closed write capability in `internal/hosting/stack_write.go`; accepted fixed methods/paths/body fields, selected-origin auth and existing raw/normalized bounds, no arbitrary API/GraphQL/extension execution. (FR-021, FR-030, SC-002, SC-008).
- [ ] T022 [P] [US2] Write failing own-PR/body/retarget race and deferred-comment refusal tests in `internal/stackpublish/pr_actions_test.go`; before/after state/head/base, foreign/raced body refusal, explicit base-only effect and old Ready invalidation. (FR-003, FR-013, FR-020, SC-002, SC-006).
- [ ] T023 [US2] Implement admitted explicit own PR create/body/retarget actions in `internal/stackpublish/pr_actions.go`; dependent reads sequential, permission unknown refuses, head must remain unchanged after retarget, unknown completion delegates to reconciliation; standalone comment execution remains deferred. (FR-013, FR-020, SC-002, SC-006).
- [ ] T024 [US2] Implement capability-gated native link/unstack plan entries in `internal/stackpublish/native_stack.go` with failing tests in `internal/stackpublish/native_stack_test.go`; inert GH_REPO/numeric bottom-to-top/explicit-base reference only until measurements, no -R/dependency execution or raw Git in jj. (FR-007, FR-008, FR-019, SC-001, SC-002).
## Phase 5: US3 — Reconcile partial outcomes (P1)

Independent test: crash/lost-response/journal failures and fresh retry never duplicate an unknown effect.

- [ ] T025 [US3] Write failing safe-store/intent/outcome/durability/crash tests in `internal/stackpublish/journal_test.go`: user-owned real 0700 metadata namespace under validated workspace .work (or explicit admitted metadata override), no symlink component, immutable bindings, no write after failed intent, retain unresolved records without expiry. Assert immutable old intent/outcome bytes never change, forged/missing/previous-digest chain refuses, missing completion is unknown and bounded intent+outcome quota is reserved before effect. (FR-005, FR-022, SC-002, SC-006).
- [ ] T026 [US3] Implement accepted typed durable action integration in `internal/workspace/stack_action_record.go` and `internal/stackpublish/journal.go`; immutable create-only intent/reservation before effect and linked outcome fact after, bound attempt/previous digest and derived state only, no mutation of sync v1 contract or session/credential capture. (FR-005, FR-022, SC-002, SC-006).
- [ ] T027 [US3] Write failing lost-response/unknown/retry/cancel tests then implement reconciliation in `internal/stackpublish/reconcile_test.go` and `internal/stackpublish/reconcile.go`; prove completion before skip, proven nonexecution before new attempt, unknown never duplicates and unstarted suffix remains not-started. (FR-005, FR-010, SC-002, SC-006, SC-008).
- [ ] T028 [US3] Write failing owner-resync/remote-undo tests then implement inert handoff in `internal/stackpublish/resync_test.go` and `internal/stackpublish/resync.go`; full old pushed/new SHA, fetch/prune and local-commit rebase versus destructive reset data, never owner-checkout execution. (FR-024, FR-026, SC-002, SC-006).
## Phase 6: US4 — Ready and owner handoff (P1)

Independent test: exact-head gate matrix and both conditional landing sequences as unexecuted data.

- [ ] T029 [US4] Write failing Ready exact-head/CI/rebaseability/scope tests in `internal/stackpublish/ready_test.go`; test each unknown/false/moved source, out-of-scope follow-up allowed but own unmet acceptance blocked, docs push exception never Ready exception. (FR-018, SC-007).
- [ ] T030 [US4] Implement pure fresh Ready receipt gate in `internal/stackpublish/ready.go`; required-context union/source completeness, exact-current-head independent CLEAR, true rebaseability and accepted scope, no 025 inference or old-base reuse. (FR-018, SC-007).
- [ ] T031 [US4] Wire separately selected/authorized Ready action through admitted bounded forge edge in `internal/stackpublish/ready_action.go`; apply 042 D5 before every attempt and reread relevant head/base immediately before write. (FR-004, FR-018, FR-028, SC-002, SC-007).
- [ ] T032 [US4] Write failing conditional owner landing tests then implement data-only handoff in `internal/stackpublish/owner_handoff_test.go` and `internal/stackpublish/owner_handoff.go`; readable-off versus on/unreadable order, 043 unmeasured-shape/re-read/deletion gates, zero land/merge/default push. (FR-025, FR-026, SC-002, SC-007).
## Phase 7: US5 — Bounded native contract (P1)

Independent test: real native temporary fixtures, fake transport, hostile inputs, limits and compatibility.

- [ ] T033 [US5] Write failing shared human/JSON/usage/escaping/secret tests then implement CLI parser/render adapter in `internal/cli/stack_publish_test.go` and `internal/cli/stack_publish.go`; accepted flags only, --save alone may create only the validated .work metadata namespace; preview creates none, proposed --offline rejected as usage 2 rather than importing 033/037 online-default semantics, no shell/process/HTTP in CLI, correct escaping and no session ids, unified target 0/1/2 with CURRENT hidden 015 code 3 until its explicit revision-2 task; never freeze the old exit mapping or change JSON/schema bytes. (FR-009, FR-023, FR-030, SC-003, SC-008).
- [ ] T034 [P] [US5] Exercise native Git/jj ownership, unsupported publication and no-write sentinels through real operation/CLI edges in `internal/acceptance/stack_publish_native_test.go`; preserve native commit/change/operation distinctions and existing contracts. (FR-007, FR-008, SC-001, SC-002, SC-003).
- [ ] T035 [US5] Run accepted minimum/current native/platform capability conformance in `internal/acceptance/stack_publish_conformance_test.go` and record actual failures/gaps in `specs/016-stacked-pr-coordination/verification.md`; missing evidence never passes. (FR-011, SC-001, SC-002, SC-005).
- [ ] T036 [US5] Execute independent exact-source review and meaningful mutants against lease, whole-range identity/class, signature, Ready, duplicate-prevention and budget gates; record immutable evidence in `specs/016-stacked-pr-coordination/verification.md`, preserving separate build/live-operation authorization. (FR-011, FR-015, FR-016, FR-017, FR-018, SC-002, SC-005, SC-006, SC-007, SC-008).
## Phase 8: Documentation and final acceptance

- [ ] T037 Update actual capability/commands/status/handoff and per-FR/SC evidence in `docs/commands.md`, `docs/progress.md` and `specs/016-stacked-pr-coordination/analysis.md`; do not turn source implementation into accepted runtime/live publication. (FR-023, FR-024, FR-025, FR-026, SC-004).
- [ ] T038 Complete final bidirectional FR/SC task/test audit, required source/native checks and exact-head acceptance analysis in `specs/016-stacked-pr-coordination/analysis.md` and `specs/016-stacked-pr-coordination/verification.md`; preserve all remaining owner/CI/live gates. (FR-011, SC-001, SC-004).

## Dependencies and parallel opportunities

T001–T005 → T006–T012 → US1. US2 depends on US1 binding plus accepted OQs and
T025/T026/T027 before any write capability can ship. US3 can be developed with
fake actions after foundations; it does not wait for a live push. US4 depends
on T029 → T030 → T031 and accepted Ready transport; T032 is data-only. US5 tests
can be authored alongside their matching story after models stabilize; acceptance
T035/T036 follows the declared capability implementation. T037/T038 finish only
after actual evidence, never merely because this task list was written.

Examples of file-disjoint parallel work after prerequisites: US1 T013 versus T015;
US2 T017 versus T020 versus T022; US3 journal fixtures T025 versus inert resync T028;
US4 Ready fixtures T029 versus owner-handoff T032; US5 native fixtures T034 versus
CLI fixture authoring T033. Tests must fail before the matching implementation;
parallel authors never edit shared source/model/status files at the same time.
No task-level parallelism authorizes remote concurrent writes or an implicit
multi-repository action plan. Independent reads alone use indexed 040 fanout.

## Incremental delivery

Ship offline US1 only as a separately accepted MVP. Enable each later native/
forge action only after its own measurement, adopted owner direction, durable retry guards,
source review and conformance; blocked kinds stay capability-unavailable. Use
independent PRs for unrelated work and at most a two-level stack for a named real
code dependency. Preserve per-PR scope acceptance: later unbuilt features need
not block a ready independent slice, while missing acceptance within its scope does.

## Owner-decision implementation tasks (all unchecked, 2026-10-25)

- [ ] T039 [FR-022, FR-031, SC-002, SC-008] Write red tests in internal/workspace/plan_store_test.go for trusted unique workspace root/default .work metadata vs external payload role, ignored/untracked checks, tracked/negated-ignore/ambiguous root refusal, explicit 0700/0600 under umask 0000, symlink/hardlink/layout/actor/race/fsync failure, record 1 MiB/count 256/physical 32 MiB quota, full refusal and zero auto-ignore/config/index edits or unresolved expiry; reserve intent+64 KiB-outcome fact count/bytes before effects and keep missing-outcome unknown on later I/O failure.
- [ ] T040 [FR-022, FR-031, SC-002, SC-008] After T039 is red, implement ONE shared metadata-store profile in internal/workspace/plan_store.go used by local/remote plan adapters. Explicit save may create only the validated namespace; preview/read creates nothing; enforce quotas, fsync immutable typed records and export-risk notices, no selected payload bytes/session/key material. Existing sync v1 untouched.
- [ ] T041 [US2] [FR-017, FR-032, SC-002, SC-005] Write red signed-receipt tests in internal/stackpublish/signature_receipt_test.go: missing/unmeasured helper, unknown/unmet policy, wrong signer/parent/old→unsigned→signed mapping, lost signature provenance, unsigned-vs-final SHA review mismatch, remote authorization never executes signer, valid final verified receipt then fresh class/identity/lease guards.
- [ ] T042 [US2] [FR-017, FR-032, SC-002, SC-005] After T041 is red, implement only final signed-receipt/public-policy validation and explicit local-plan handoff data in internal/stackpublish/signature_receipt.go. Reuse 015-owned local signing receipt; no duplicate helper/key code, implicit local apply, unsigned fallback or review of predicted/unsigned head.
- [ ] T043 [FR-031, SC-002, SC-006] Write red metadata cleanup-plan/apply tests in internal/workspace/plan_store_cleanup_test.go: exact completed ids/digests, dependency/unresolved/foreign/race refusal, explicit local plan+authorization, partial receipts and quota recovery using explicitly admitted separate maintenance metadata pool 128 facts/8 MiB bound to full target namespace; full target cannot block cleanup intent/outcome, and both pools full must refuse before delete; no auto-expiry or payload/native-ref deletion.
- [ ] T044 [FR-031, SC-002, SC-006] After T043 is red, implement only shared validated metadata cleanup in internal/workspace/plan_store_cleanup.go with per-record expected digest/identity and durable outcome in the separate bounded maintenance pool, not the full target pool; reserve maintenance capacity before deletion. Explicit local cleanup is distinct from remote plan authorization and from 013 payload/safety-copy drop; retain unresolved records forever unless resolved and separately selected.
