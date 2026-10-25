# Tasks: Checkpoint, Resume and Selective Undo

**Input**: [spec](spec.md), [plan](plan.md), [research](research.md),
[data model](data-model.md), [capture](contracts/checkpoint.md),
[recovery](contracts/recovery.md), [undo](contracts/selective-undo.md).
**Status**: future implementation only, all tasks unchecked. Markdown authoring
is not completion of these tasks. OQ-1 through OQ-5 block affected behavior.
Paths below are proposed source/test targets; none is created by this amendment.

## Phase 1: Setup and owner decisions

- [ ] T001 Record owner choices OQ-1 through OQ-5, native admission matrix, approved storage/retention/ownership inputs, journal migration and CLI/common-flag admission in `specs/013-checkpoint-resume/research.md`; keep undecided capabilities disabled and preserve settled 015 D7/D8.

## Phase 2: Foundations — blocking every effect

- [ ] T002 Characterize current sync v1 durability/retention/restore in `internal/stwsync/journal_test.go` and `internal/stwsync/restore_test.go`, then add failing typed-seam tests for new record types, reserved checkpoint/undo root ownership, collision, unsafe record, sync-retention coexistence and fail-closed durability in proposed `internal/recovery/journal_test.go` (FR-001).
- [ ] T003 Extract/reuse only reviewed durable-write/root/validation primitives in proposed `internal/recovery/journal.go` with operation-specific adapters in `internal/stwsync/journal.go` and `internal/stwsync/restore.go`; preserve sync v1 bytes/semantics and its sync-only retention; enforce exact reserved 013 root/type/expected-value guards, no second generic writer (T002; OQ-5).
- [ ] T004 [P] Write failing native ownership/colocation/GitButler/missing-backend tests in `internal/checkpoint/ownership_test.go`; config/session/name identity never proves ownership (FR-007).
- [ ] T005 Implement approved native repository/layout/task/actor guards in `internal/checkpoint/ownership.go`, refusing ambiguous/shared/foreign/unknown evidence without Git fallback (T004; OQ-4).
- [ ] T006 [P] Write failing count/byte/deadline/cancellation tests in `internal/checkpoint/limits_test.go` for 256 paths, 8 MiB per payload, 32 MiB total, 16 KiB context, 1 MiB result, 2 KiB diagnostics, 30-second reads/plans and 120-second actions (FR-009).
- [ ] T007 Implement pure plan/result/native-identity types and enforced bounds in `internal/checkpoint/model.go` and `internal/checkpoint/limits.go`: default empty selection, opaque repository/type/version-bound ids, canonical plan digest, distinct full Git/jj commit/change/operation ids; reject malformed/truncated evidence before effects (T006).
- [ ] T008 [P] Write failing no-follow/traversal/alias/control-directory/sensitive-input/unusual-byte path and inert task-text tests in `internal/checkpoint/selection_test.go`, including empty/deleted vs empty-file distinction (FR-005).
- [ ] T009 Implement bounded byte-preserving selection/privacy/context validation in `internal/checkpoint/selection.go`; optional task text is caller-only inert data, no credential/environment/transcript discovery or execution (T008).

## Phase 3: US1 — Save a recoverable checkpoint (P1, MVP)

**Goal**: exact selected progress and receipt with no unrelated effects.
**Independent test**: staged and disk versions differ; capture only selected
layers/paths, inspect receipt, and prove excluded/untracked state unchanged.

- [ ] T010 [P] [US1] Write failing explicit path/layer, default-none, separate untracked/ignored selection and stale-plan tests in `internal/checkpoint/capture_plan_test.go` (FR-001).
- [ ] T011 [US1] Implement capture plans and exclusion reasons in `internal/checkpoint/capture_plan.go`, with disclosed private plan writes only for explicit selection and exact authorization-plus-plan guard (T010).
- [ ] T012 [P] [US1] Write failing native staged-vs-disk Git and stored-vs-disk jj capture/capability tests in `internal/checkpoint/capture_native_test.go`; a jj operation id without byte receipt never proves disk recovery (FR-002).
- [ ] T013 [US1] Implement only approved Git capture layers/types/modes/deletions in `internal/checkpoint/git_capture.go`, preserving unrelated index/disk and refusing unadmitted index/special states (T012; OQ-1).
- [ ] T014 [US1] Implement only independently admitted native jj capture in `internal/checkpoint/jj_capture.go`; disclose any approved snapshot effects, refuse unsupported states without raw Git (T012; OQ-1).
- [ ] T015 [P] [US1] Write failing external-store/native-linked-layout proof, explicit 0700/0600 modes under umask 0000, foreign/0666 refusal, 32 MiB checkpoint/owner-selected total physical quota, no-content-output, exact confirmed drop/dependency/CAS/partial-byte-count tests, completed-restore safety-copy survival after checkpoint drop and separate safety-id-confirmed deletion tests plus payload-integrity, byte-vs-metadata availability, root/receipt durability and incomplete-retention tests in `internal/checkpoint/store_test.go` (FR-003).
- [ ] T016 [US1] Implement approved private no-follow payload/manifest receipts in `internal/checkpoint/store.go`, separate from typed journal metadata; all selected required layers durable before complete; enforce external location, explicit 0700/0600 modes and both quotas; implement only approved exact planned/authorized/confirmed drop with owned-root CAS, dependency guards and partial receipts; preserve independently retained safety copies and require their separately named/authorized/safety-id-confirmed deletion, no implicit expiry/GC (T015; OQ-3/OQ-5).
- [ ] T017 [P] [US1] Write failing crash-after-payload/root/intent/effect/completion-write and missing-object/payload tests in `internal/checkpoint/capture_test.go`, asserting explicit partial/incomplete outcomes and protected evidence (FR-006).
- [ ] T018 [US1] Implement capture orchestration and per-effect accounting in `internal/checkpoint/capture.go` through the reviewed seam, with fresh guards and no silent stash/reset/rollback (T013–T017).

## Phase 4: US2 — Resume and restore safely (P1)

**Goal**: offline inspection first, separately authorized precise restoration.
**Independent test**: resume a saved checkpoint with zero native/store writes;
restore only a selected unchanged case, then refuse concurrent or missing bytes.

- [ ] T019 [P] [US2] Write failing offline read-only saved/current comparisons, missing/corrupt/unsupported recovery and zero Git/jj snapshot/oplog write tests with explicit ignore-working-copy/at-operation argv, all-workspace identities and missing saved operations in `internal/checkpoint/resume_test.go` (FR-004).
- [ ] T020 [US2] Implement read-only resume comparison and separate restore-plan creation in `internal/checkpoint/resume.go` and `internal/checkpoint/restore_plan.go`, native jj snapshot-suppressed/full-operation-pinned reads and all-workspace evidence, no repair/fetch/auto-merge and no inherited capture authorization (T019).
- [ ] T021 [P] [US2] Write failing restore guard, changed index/disk/ref/layout/ownership, active-workspace and missing-byte tests in `internal/checkpoint/restore_test.go`; no arbitrary reset or broad jj op restore (FR-004, FR-006).
- [ ] T022 [US2] Implement only approved restore primitives in `internal/checkpoint/restore.go`, durably saving pre-action selected bytes/native roots in independently identified safety-copy records/receipts/roots before effects, preserving their lifetime after completion or checkpoint drop and recording actual per-effect completion (T021; OQ-1/OQ-2/OQ-4/OQ-5).
- [ ] T023 [P] [US2] Write failing known-new/already-applied/changed-since/mixed/unknown-new-head retry and completion-write-failure tests in `internal/checkpoint/reconcile_test.go`, keeping prepared evidence protected (FR-006).
- [ ] T024 [US2] Implement typed reconciliation in `internal/checkpoint/reconcile.go`; unknown desired state is journal-incomplete, never blind reset, automatic rollback or inferred completion (T023).

## Phase 5: US3 — Bounded human/agent contract (P1)

**Goal**: equivalent evidence/refusals in both modes with explicit effects.
**Independent test**: validate complete success/error fixtures, compare human
qualifications, and exercise malformed/unsupported/cancelled paths with sentinels.

- [ ] T025 [P] [US3] Write failing human/JSON success/error/outcome/escaping/reversible-path and future 0/1/2 CLI contract tests in `internal/cli/checkpoint_test.go`, including no-TTY operation, exact admitted spellings/format flags, authorization plus plan id, offline `--online` and redundant `--offline` usage refusals (preserving 033/037 behavior) and the accepted/rejected `--human`/`--timeout` choices from OQ-5; preserve hidden 015 v1 exit 3 and published schema bytes (FR-008).
- [ ] T026 [US3] Implement thin noninteractive parsing/rendering in `internal/cli/checkpoint.go` and future unpublished schemas/fixtures in `internal/contracts/checkpoint_test.go` under approved registration; no schema publication before conformance (T025).
- [ ] T027 [P] [US3] Write failing helper/hook/config/text execution, network, deadline, output-excess and cancellation effect sentinels in `internal/checkpoint/effects_test.go` and `internal/cli/checkpoint_bounds_test.go` (FR-009).
- [ ] T028 [US3] Implement fixed native-runner/effect boundary and stop/reap/report behavior in `internal/checkpoint/runner.go`, with architecture allowlist checks in `scripts/check-architecture.py`; never enable an unadmitted backend/effect (T027).

## Phase 6: US4 — Selective explicit local undo (P1)

**Goal**: caller-selected semantics and scope with precise recovery limits.
**Independent test**: inverse vs restoration, explicit merge parent, descendant
closure, shared/foreign refusal and interrupted outcomes in admitted Git/jj cases.

- [ ] T029 [P] [US4] Write failing explicit commit/change/op target, complete path/ref/descendant effect set, shared/foreign/unknown publication/ownership refusal, clean caller/linked occupied inverse destinations, dirty selected paths, incomplete occupancy inventory and stale evidence tests in `internal/checkpoint/undo_plan_test.go` (FR-011, FR-013).
- [ ] T030 [US4] Implement separate immutable undo plans and fresh per-action ownership/effect/recovery guards in `internal/checkpoint/undo_plan.go`, retaining exact pre-action roots and selected bytes (T029; OQ-2/OQ-4).
- [ ] T031 [P] [US4] Write failing inverse-vs-history-restoration, explicit valid merge-parent, unsupported topology, conflict, path-narrowing, signature-loss disclosure, required-signing refusal, private quarantine/plumbing and original object/index/disk non-mutation tests in `internal/checkpoint/git_undo_test.go` (FR-012).
- [ ] T032 [US4] Implement only approved Git undo semantics in `internal/checkpoint/git_undo.go`, with private-quarantine plumbing, reviewed promotion/CAS and changed-since refusal for every occupied/dirty/unknown inverse destination; no real-worktree revert, stale held ref, default parent, unselected descendant rewrite, shared/foreign target, signing-helper execution, silently dropped signature policy or automatic restore (T031; OQ-2).
- [ ] T033 [P] [US4] Write failing native jj change/commit/operation distinctions, operation-id-without-bytes, broad-op-as-selective refusal, repository-wide workspace map (including invoking-workspace commit-change/stale/update-stale refusal), single-head/direct-parent admission, older-op restore-vs-inverse semantics, mandatory ignore-working-copy/at-operation argv, divergent/concurrent pre/post heads, missing abandoned/GC'd operation and interrupted/partial recovery tests in `internal/checkpoint/jj_undo_test.go` (FR-014).
- [ ] T034 [US4] Implement only approved native jj selective undo in `internal/checkpoint/jj_undo.go`, using the shared typed seam, approved version-specific argv, mandatory snapshot suppression/pinning and complete all-workspace/sole-head/direct-parent pre/post guards with zero working-copy commit changes, invoker included; stale/unknown workspace evidence refuses and no update-stale runs; divergent heads refuse without auto-merge, missing operations are unavailable and post-effect races partial; no Git or broad-op fallback (T033; OQ-2).

## Phase 7: Conformance, evidence and handoff

- [ ] T035 Execute mandatory minimum/current Git/jj supported-platform/version capability/failure/concurrency/ownership cases in `internal/checkpoint/conformance_test.go` and record actual lanes/gaps in `specs/013-checkpoint-resume/verification.md`; missing execution blocks admission (FR-010).
- [ ] T036 Execute every story scenario independently with the literal fixture matrix in `specs/013-checkpoint-resume/quickstart.md`, recording unsupported cases as limits and required unrun cases as blockers in `specs/013-checkpoint-resume/verification.md` (SC-001).
- [ ] T037 Prove state outside declared effects and zero undeclared helpers/network across safety/crash/failure cases in `internal/checkpoint/conformance_effects_test.go`, and record actual sentinels/mutation controls in `specs/013-checkpoint-resume/verification.md` (SC-002).
- [ ] T038 Validate complete structured success/error fixtures and byte-preserved existing published contracts in `internal/contracts/checkpoint_test.go`, recording native/output conformance before any schema publication (SC-003).
- [ ] T039 Reconcile each individual FR/SC with its acceptance execution/task and unresolved decision in `specs/013-checkpoint-resume/analysis.md`; never promote missing evidence to verified (SC-004).
- [ ] T040 Obtain independent exact-head source review, run scope-required repository checks and record actual outcomes/remaining gates in `specs/013-checkpoint-resume/verification.md` before PR readiness or capability exposure; no release, live apply or activation implied.

## Dependencies and execution order

T001 → T002 → T003 establishes the reviewed seam. T004/T006/T008 may be
written independently; each must fail for its new behavior before T005/T007/T009.
Foundations and approved decisions precede any effect. US1 builds capture;
US2 uses a complete checkpoint (tests may construct typed fixtures); US4 uses
the common guards/recovery seam without requiring an existing user checkpoint.
US3's shared result/limits/no-execution constraints are foundational: implement
T025–T028 before any story is exposed through the CLI, and exercise them against
each story. T018 requires T016 and T028; T022/T024 require T003, T005, T007,
T009, T016, T028 and their own tests; T030/T032/T034 require the same common
foundations plus US4 tests. Final evidence follows the admitted story set.

Every test task is red-first for new behavior; baseline sync characterization
stays green before/after extraction. No implementation task can satisfy its own
review. [P] marks independent test files, not concurrent native mutations.

## Parallel examples and delivery strategy

| Story | Independent parallel preparation after foundations |
| --- | --- |
| US1 | T010 capture selection tests, T012 native-layer tests, T015 storage tests and T017 crash tests use separate files |
| US2 | T019 comparison, T021 restore guards and T023 reconciliation tests use separate files |
| US3 | T025 CLI contract fixtures and T027 effect/bounds tests use separate files |
| US4 | T029 plan guards, T031 Git semantics and T033 jj semantics tests use separate files |

MVP is US1 only for individually admitted native capabilities, with all relevant
US3 safeguards and T035–T040 evidence. Unsupported peer capabilities stay explicit
and disabled. Deliver read-only resume next, then reviewed restore and undo
increments; each increment needs its own exact-head review and conformance.
Artifact readiness, source completion, PR readiness, release and live operations
are separate. No checkbox is ticked by this documentation amendment.
