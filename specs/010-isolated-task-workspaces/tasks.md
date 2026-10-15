# Tasks: Isolated Task Workspaces

**Input**: [plan](plan.md), [spec](spec.md), [research](research.md), [model](data-model.md),
[CLI contract](contracts/workspace.md), [interface freeze](contracts/interface.md).
Tests are mandatory under repository policy and requested review/test scope. Write failing
behavior tests before implementation. No source work before independent cross-artifact analysis.

## Phase 1: Setup
- [x] T001 Finalize fixed shared interface and analysis gates in specs/010-isolated-task-workspaces/contracts/interface.md and analysis.md.
- [ ] T002 Record native Git/jj argument, helper and sandbox research with exact experiments in specs/010-isolated-task-workspaces/verification.md.

## Phase 2: Foundation
- [ ] T003 Write failing bounds, secure-path/store, journal durability, lock, rename/symlink ancestor replacement and invalid/foreign evidence tests in internal/workspace/state_test.go and paths_test.go.
- [ ] T004 Implement shared models and bounded diagnostic/results in internal/workspace/result.go; identifiers are nonempty ASCII `[A-Za-z0-9][A-Za-z0-9._-]*` at most128bytes, paths/base validUTF8 at most4096bytes.
- [ ] T005 Implement validated directory-descriptor-anchored os.Root/no-follow accesses with identity revalidation on replacement; current-user mode0700 symlink-free disjoint store and no-follow exclusive mode0600 record IO in internal/workspace/state.go and paths.go; reject unknown/duplicate JSON fields and records above1MiB before decode.
- [ ] T006 Implement immutable plan digest, exclusive cooperative lock, file+directory fsync and pre-action journal state machine in internal/workspace/state.go; fail before native action if durability fails.

## Phase 3: US1 — Start an isolated checkout (P1)
**Independent test**: plan and authorized apply from an explicit base in disposable native Git/jj;
verify checkout contents/native registration, durable ownership and byte-identical source state.
- [ ] T007 [P] [US1] Write failing Git create/stale-base/ref/destination/config-hook/filter/race/checkout-limit tests in internal/workspace/git_test.go.
- [ ] T008 [P] [US1] Write failing native jj create/operation-stale/standalone/colocated/source-unsnapshotted/two-phase tests in internal/workspace/jj_test.go.
- [ ] T009 [US1] Implement plan creation and stale revalidation in internal/workspace/workspace.go; bind native source/base/reference/config/registration and parent identity, refuse occupied/aliased destinations.
- [ ] T010 [P] [US1] Implement fixed detached locked full Git checkout at resolved OID with hooks/filter/helper refusal and least-write/no-network sandbox in internal/workspace/git.go and git_sandbox.go; bound tree to1024entries/64MiB stored blobs before action.
- [ ] T011 [P] [US1] Implement native jj add with source snapshot suppression, operation pin, git.colocate=false and owned destination-only materialization in internal/workspace/jj.go and jj_sandbox.go; never raw Git fallback or source workspace update.
- [ ] T012 [US1] Implement authorized apply, exclusive0700 destination reservation, prepared journal before native changes and task ownership finalization in internal/workspace/workspace.go; preserve nonzero partial effects on errors.
- [ ] T013 [US1] Verify reserved destination/native registration/source preservation before completed outcome in internal/workspace/workspace_test.go; never adopt foreign registrations or delete on failure.

## Phase 4: US3 — Bounded human/agent contract (P1)
**Independent test**: real CLI success/refusal/cancel/error JSON validates; both native lanes execute
all mandatory safety cases, reject missing capabilities and preserve unrelated state.
- [ ] T014 [P] [US3] Write failing CLI syntax/authorization/shared-output/exit-status tests in internal/cli/workspace_test.go.
- [ ] T015 [P] [US3] Write workspace schema fixture tests including failures/partial effects and old schema regressions in internal/contracts/workspace_test.go.
- [ ] T016 [US3] Implement Cobra command family and shared result rendering in internal/cli/workspace.go; allow operation package only in scripts/check-architecture.py and its tests.
- [ ] T017 [US3] Define independent schemas/v1/workspace.schema.json with preserved native identities and complete success/error/result variants; maintain prior contracts unchanged.
- [ ] T018 [US3] Enforce10second observation/60second apply deadlines,64KiB command output,1MiB encoded records/results and1024 aggregate observations before unbounded allocation in internal/workspace/workspace.go and adapter runners.
- [ ] T019 [P] [US3] Implement mandatory exact case manifest/native effect snapshots/helper sentinels in scripts/smoke-workspaces.py and scripts/test-smoke-workspaces.py; incomplete or missing execution is failure.
- [ ] T020 [US3] Run mandatory smoke-workspaces.py in both pinned Git/jj lanes in .github/workflows/ci.yml, retaining all existing conformance gates.

## Phase 5: US2 — Handoff and retirement preparation (P2)
**Independent test**: handoff/retire-plan revalidate owned native identity without mutation; interrupted
creation remains distinguishable from completed and foreign destinations are retained.
- [ ] T021 [P] [US2] Write failing completed-retry/incomplete-registration/materialization/store-failure/cancel/concurrent lock and state ancestor rename/symlink replacement tests in internal/workspace/recovery_test.go.
- [ ] T022 [P] [US2] Write failing handoff/retire ownership/dirty-unknown/native-identity mismatch/no-delete tests in internal/workspace/handoff_test.go.
- [ ] T023 [US2] Implement retry reconciliation using journal-proven exact owned identity in internal/workspace/workspace.go and state.go; no automatic rollback, deletion, lock stealing or path-based adoption.
- [ ] T024 [US2] Implement handoff and retire-plan with native observations, retention evidence/recovery limits and explicit Spec011 delegation in internal/workspace/handoff.go.
- [ ] T025 [US2] Add human/JSON handoff/retirement and partial-retry CLI fixtures in internal/cli/workspace_test.go and internal/contracts/workspace_test.go.

## Phase 6: Review and handoff
- [ ] T026 [P] Publish capability/authorization/recovery/bounds guide in docs/workspaces.md, docs/backends.md, docs/agent-guide.md and docs/json-contracts.md; add explicit guide/schema Pages allowlist and docs index.
- [ ] T027 Update ARCHITECTURE.md and AGENTS.md to describe reviewed operation layer and supported mutations without claiming tidy/deletion.
- [ ] T028 Run independent cross-artifact convergence and exact-revision source/safety review; record resolved findings in specs/010-isolated-task-workspaces/analysis.md and verification.md.
- [ ] T029 Run gofmt, make check, race tests, both mandatory native lanes and quickstart scenarios; record actual commands/revisions/gaps in specs/010-isolated-task-workspaces/verification.md.
- [ ] T030 Update tasks, docs/progress.md and docs/feature-status.json honestly; make focused Conventional Commits and open/attach PR with acceptance evidence.

## Dependencies and parallel work
T001–002 precede source. T003–006 establish common types/store. US1 adapter tests/implementations
T007/T010 andT008/T011 can run in separate worktrees after interface freeze and analysis. Core
T009/T012–013 integrates adapters. US3 CLI/schema T014–017 and conformance T019 prepare in parallel
on disjoint files, then integrate after core API exists; bound enforcement T018 applies throughout.
US2 tests may prepare after foundation, but retry/handoff implementation requires completed US1.
All stories plus final review/checks are required for this WI/PR; no native backend is deferred.

Parallel example: Git author owns git*.go; jj author owns jj*.go; core owns workspace/state/paths/
handoff and common result types; CLI/schema author owns internal/cli/workspace*, schema and contract
tests; conformance author owns smoke script and its tests. Root serializes shared CI/docs integration.
Each author writes a failing behavior test, then a focused implementation commit. Any interface
change is coordinated before implementation; no duplicate common types or broad generic runner.

## Requirement and success coverage
| Requirement | Tasks | Acceptance evidence |
| --- | --- | --- |
| FR-001 | T007–013 | Native creation/content both backends/versions |
| FR-002 | T003–006,T009,T012 | Durable plan/authorize/journal and action outcomes |
| FR-003 | T007–013,T021,T023 | Base/op/ref/config/parent/destination race refusals |
| FR-004 | T005–006,T012–013,T022–024 | Durable explicit owner/foreign retention |
| FR-005 | T007–013,T019 | Source byte/effect/helper sentinels |
| FR-006 | T021,T023–025 | Interrupted versus completed retry/handoff |
| FR-007 | T022,T024–025 | Retirement evidence/no deletion |
| FR-008 | T007–011,T019–020 | Native ownership/capability/no fallback |
| FR-009 | T014–017,T025 | Same-model human/JSON/schema failures |
| FR-010 | T003–006,T018,T021 | Bound/cancel/incomplete-effect cases |
| FR-011 | T019–020,T028–030 | Mandatory minimum/current gates |
| SC-001 | T007–025,T029 | All acceptance scenarios/version lanes |
| SC-002 | T007–013,T019,T021–024 | Before/after effect and failure assertions |
| SC-003 | T015,T017,T025,T029 | Real result/schema/old-contract validation |
| SC-004 | T001,T028–030 | Traceability, independent analysis/review/evidence |
