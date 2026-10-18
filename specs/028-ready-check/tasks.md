# Tasks: Ready inspection and review evidence

**Input**: [spec.md](spec.md), [plan.md](plan.md), [data-model.md](data-model.md), [research.md](research.md), [contracts](contracts/ready-check.md), [quickstart.md](quickstart.md).

**Gate**: Independent specification and corrected-plan CLEAR at `88c9b8b2c8f5ecd6240c5065d0b9bd6de62632cc` (rebased specification `23b22c0b5829a1fab07dea935ee61ce795d830cc`), zero open findings. This task list requires independent complete-artifact CLEAR before any implementation. All tasks remain unchecked; source-design approval does not authorize deployment resources or settings.

**Tests**: Required by FR-019 and the constitution. Record a meaningful failing test before each behavior change, then pass it and refactor. Temporary CLI compilation inside source tests is routine verification; product/site/image builds and installation require separate approval. Future paths below are planned files, not existing capabilities.

## Phase 1: Setup and compatibility baseline

- [ ] T001 Record the accepted full-artifact SHA, requirement traceability and implementation evidence ledger in `specs/028-ready-check/analysis.md`; coordinate ownership of shared `internal/hosting/transport.go`, `scripts/check-architecture.py` and schema/site inventories before edits.
- [ ] T002 Capture existing hosting schema/result compatibility assertions in `internal/contracts/ready_check_test.go` and `internal/hosting/ready_test.go`; keep Spec 025 explanatory and preserve existing published schema/fixture bytes.

## Phase 2: Shared foundations

These tasks establish the common pure input/result and typed reader seams. No story may substitute its own evaluation rules.

- [ ] T003 Write failing closed-input tests in `internal/readiness/policy_test.go` and `internal/readiness/report_test.go` for duplicate keys, BOM/UTF-8/trailing tokens, unknown fields/version, integer overflow, depth 16, 16 KiB lock, 64 KiB policy/report, bounded strings and every schema field constraint; use the three approved schemas in `specs/028-ready-check/contracts/` as the exact shape baseline.
- [ ] T004 Implement immutable input parsing/types in `internal/readiness/policy.go` and `internal/readiness/report.go`; preserve exact raw-byte digests and closed shapes/nullability; expose typed inputs for separately tested semantic predicates in US2.
- [ ] T005 Write result/reduction/purity tests in `internal/readiness/evaluate_test.go` and `scripts/test_architecture.py`: quote and enforce `schema_version / kind` = `1` / `ready-check`, `observed_at` = `RFC3339 UTC supplied by the operation`, `runtime_attestation=unavailable`, `backend / identity` = `Existing native Git/jj identity, nullable only on early failure`, nullable head repository ID and `Nullable raw mergeable/rebaseable and bounded mergeable_state`; require deterministic reason order and error → blocked → indeterminate → eligible reduction.
- [ ] T006 Implement shared result/evaluator scaffolding in `internal/readiness/evaluate.go` and architecture purity enforcement in `scripts/check-architecture.py`; reject filesystem/process/environment/clock/network dependencies and preserve explicit input timestamps and completeness.
- [ ] T007 Write failing fixed-reader fixtures in `internal/hosting/ready_sources_test.go` for every endpoint/outcome in `contracts/ready-check.md`, including full statuses with creator, checks with App ID, complete open-PR enumeration, denied/ambiguous absence, typed activation, redirects and `2022-11-28` preservation.
- [ ] T008 Implement additive typed readers in `internal/hosting/ready_sources.go` and the necessary guarded extension in `internal/hosting/transport.go`; preserve old endpoint guards/tests and carry source identity, completeness and request accounting without arbitrary URLs or raw error-body disclosure.

## Phase 3: User Story 2 — Authenticate a review report (P1)

**Goal**: One project-policy review projection, independently testable with immutable samples before CLI/publication integration. This P1 story precedes US1 because full eligibility consumes authenticated review evidence.

**Independent test**: Keep a success label while mutating each report/status/issuer/binding field; only the complete authentic case passes. No live publication is required.

- [ ] T009 [P] [US2] Write failing authentication/history/tier tests in `internal/readiness/review_test.go`: newest status before issuer selection, numeric creator/comment-author IDs, exact digest/locator, repository/PR/head/base/policy/full-file-scope, unique tier ranks and valid prefixes/references, renamed paths, rounds/findings including open Low, configured-model versus declaration/runtime limits and no private identifiers.
- [ ] T010 [P] [US2] Write failing policy-loading/transition tests in `internal/hosting/ready_policy_test.go`: explicit outside-target regular lock, pinned policy source identity, raw digests, complete authenticated lock equality including all nine transition fields/null/whitespace, independent bootstrap routing and start/final drift; port R3 vectors from `contracts/counterexamples.md`.
- [ ] T011 [US2] Implement review predicates in `internal/readiness/evaluate.go` and report semantic helpers in `internal/readiness/report.go`; use generic configured policy, full-PR scope and explicit declared-execution rules, never infer runtime attestation or parse CrewBook gitnotes as authority.
- [ ] T012 [US2] Implement lock/pinned-policy/activation loading in `internal/hosting/ready_policy.go`; enforce approved-channel capabilities and complete-byte authentication, old-baseline transition approval and undeployed caller-mode limits without writing activation or following candidate routing.
- [ ] T013 [US2] Integrate review collection/resampling in `internal/hosting/ready.go` with tests in `internal/hosting/ready_test.go`; require a complete singleton open-PR head association, full files/status/report metadata and stable complete lock bytes; incomplete or changed samples cannot preserve positive review.

**Checkpoint**: Review fixtures pass independently, with explicit unavailable proof and no project-specific default account/model/context.

## Phase 4: User Story 1 — Assess Ready eligibility (P1)

**Goal**: Read-only human/JSON eligibility for one explicit PR, including required CI, effective constraints and deployed-check evidence.

**Independent test**: A complete stable fixture is eligible; independently removing each prerequisite is blocked/indeterminate. Draft alone does not block and already-Ready alone does not pass.

- [ ] T014 [P] [US1] Write failing CI/constraint/deployment tests in `internal/readiness/constraints_test.go`: effective requirement union/source pins, newest check/status, head versus test-merge, missing/neutral/skipped checks, self-source exclusion, explicit REST rebaseable/mergeable and allowed rebase, strict base inclusion, hidden/unsupported effective rules and stale deployed envelope/full-lock digest.
- [ ] T015 [P] [US1] Write CLI/golden and output-schema tests in `internal/cli/ready_test.go`, `internal/cli/testdata/ready-eligible.json` and `internal/contracts/ready_check_test.go`; cover explicit positive NUMBER/selectors, no interaction, exit 0/1/2, escaped human values and agreement of human/JSON identities/reasons/limitations.
- [ ] T016 [US1] Complete pure CI/constraints/deployment reduction in `internal/readiness/evaluate.go`; keep review independent of CI/self-pending and enforce all required components for final eligibility with known failures retaining other unknowns.
- [ ] T017 [US1] Complete bounded operation collection/final resamples in `internal/hosting/ready.go`; preserve source-specific unknown/absence semantics, current base inclusion and both sampled explicit rebaseability values, without retries or favorable-state polling.
- [ ] T018 [US1] Add `ready-check` parsing/rendering in `internal/cli/ready.go` and registration in `internal/cli/root.go`; call hosting operations only, preserve native selector ownership, expose the same jj-only leaf and the fixed sampled-eligibility/merge-versus-Ready notice.
- [ ] T019 [US1] Add `schemas/v1/ready-check.schema.json`, `schemas/v1/ready-policy.schema.json` and `schemas/v1/review-report.schema.json` plus offline validation in `internal/contracts/ready_check_test.go`; retain input closure/limits and publish only reviewed contract fields. Coordinate required schema inventory updates in `scripts/package-pages.py` and `scripts/test_docs.py` with their owner; do not execute packaging or alter existing schema bytes.

**Checkpoint**: Caller-policy CLI is useful without a deployed check. Installed-profile eligibility remains unknown until its accepted trust/read capabilities are proven; no false installed-gate success.

## Phase 5: User Story 3 — Protect merge with a separate check (P1)

**Goal**: A separately deployable check-only publisher consuming the exact evaluator review projection. Source completion and authorized hosted rollout are distinct checkpoints.

**Independent test**: Fake adapter tests prove complete projection agreement and lifecycle behavior without credentials. Separately authorized disposable-host tests must prove source authenticity and actual required-check enforcement before rollout completion.

- [ ] T020 [US3] Write failing adapter/projection tests in `scripts/test_review_evidence.py`: fixed immutable executable/artifact identity, strict result/envelope parsing, cancellation/incomplete/error rejection, complete review despite unrelated CI pending, no self-event loop, no untrusted code/artifact/cache execution and only owned check writes.
- [ ] T021 [US3] Extend `scripts/test_review_evidence.py` with every R1/R2 vector in `contracts/counterexamples.md`: revoke prior successes before evaluation, failure for observed report/status/transition/read invalidations, cancellation and uncertain-write outcomes, same-head open/reopen/retarget collisions, shared-key lock ownership, stale writer, before/after resamples and reserved revocation budget. Assert the old success is actually made blocking, not merely that no new success appeared.
- [ ] T022 [US3] Implement generic fixed check adapter/controller in `scripts/review-evidence.py`; consume only a separately approved immutable stw artifact, never build or duplicate review predicates. Enforce repository/head/context/App serialization, complete singleton association, exact envelope/full-lock binding, failure rather than neutral/skipped, readbacks, 60-second/120-request total and 20 mutation/20 readback caps, no retries and explicit enforcement-unknown on unconfirmed revocation.
- [ ] T023 [US3] Add evaluator-to-publisher agreement fixtures in `internal/contracts/ready_check_test.go` and `scripts/test_review_evidence.py`; assert identical review projections and independent final CLI CI/deployment requirements, including a pending self check and no active profile.
- [ ] T024 [US3] Prepare the concrete deployment capability/permissions/source inventory in `specs/028-ready-check/deployment-acceptance.md` against the approved alternatives in `contracts/github-check.md`; verify native workflow availability, human-mediated limits and suitable existing-App possibility before proposing new infrastructure. This read-only proposal selects no resource and requests only a concrete necessary owner operation.
- [ ] T025 [US3] After explicit owner selection/authorization only, specify the exact profile workflow/configuration paths and credential/activation bootstrap in `specs/028-ready-check/deployment-acceptance.md`, then obtain independent review before creating or activating them. An unproven variable name, default branch or generic Actions App identity cannot satisfy the source-authenticity gate; unsupported capability blocks this task.
- [ ] T026 [US3] After separate hosted-test/resource authorization only, record disposable-host acceptance in `specs/028-ready-check/deployment-acceptance.md`: hostile workflow/root/activation changes, spoofed sources, complete transitions, same-head collisions, event omissions/races, least privilege and old-success revocation. Preserve failures/unknowns; source fixtures cannot substitute for these measurements.
- [ ] T027 [US3] Only after the actual trusted check exists and passes, prepare the exact minimal settings diff and rollback with observed check/source/head evidence in `specs/028-ready-check/deployment-acceptance.md`; preserve existing requirements, await the owner's specific settings operation and record its result. Never apply settings, merge or mark rollout complete merely because this task exists.

**Checkpoint**: T020–T023 can complete as inactive source. T024–T027 remain explicit rollout gates; no default deployment, App/secret/environment creation or manual Ready restriction is implied.

## Phase 6: User Story 4 — Retain bounded read-only operation (P1)

**Goal**: Preserve Tier 1 ownership/effects and bounded refusal across native backends and hostile hosting input.

**Independent test**: Native temporary repositories plus fake HTTP demonstrate equal decisions and backend-specific identities/effects; every source/budget failure terminates without writes to the inspected repository.

- [ ] T028 [P] [US4] Write failing Git/jj native effects tests in `internal/contracts/ready_native_test.go`: ordinary/linked Git, standalone/colocated/secondary jj, jj-only extension, dirty state, refs/index/worktree files, jj operation log/snapshot behavior, unsupported/missing native tool and resolution before network.
- [ ] T029 [P] [US4] Write failing hostility/budget/race tests in `internal/hosting/ready_bounds_test.go`: exact page exhaustion and duplicates, all byte/time/request/row limits from `contracts/ready-check.md`, cancellation, malformed metadata, redirects, final head/base/activation/status/report/association drift and output overflow.
- [ ] T030 [US4] Close conformance gaps in `internal/hosting/ready.go`, `internal/hosting/ready_sources.go` and `internal/hosting/ready_policy.go` without weakening existing native discovery/transport guarantees; make T028–T029 pass and document backend-specific limits in `specs/028-ready-check/analysis.md`.
- [ ] T031 [US4] Add controlled real-native smoke scenarios in `scripts/smoke-hosting.py` and corresponding `scripts/test_hosting_smoke.py` fixtures; keep live HTTP fake by default, declared backend minimum/current capability lanes explicit, and run separately authorized hosted smoke only under T026.

## Phase 7: Documentation and final evidence

- [ ] T032 Update `ARCHITECTURE.md`, `docs/harness.md`, `docs/progress.md`, `docs/roadmap.md`, `docs/feature-status.json` and `specs/028-ready-check/quickstart.md` for implemented source and remaining deployment limits; preserve Spec 025 observations and generic project-policy boundaries. Keep planned/implemented/accepted states distinct.
- [ ] T033 Run applicable source/race/architecture/docs/schema/CLI/Python checks and authorized native capability lanes; record exact commands, exits, versions, SHA and any excluded/unavailable checks in `specs/028-ready-check/analysis.md`. Obtain independent exact-source review and rerun affected checks after fixes; no product/site/image build or installation without its separate approval.
- [ ] T034 Cross-check all story scenarios and FR/SC coverage in `specs/028-ready-check/analysis.md`, including positive and negative fixtures, all declared residual races and T024–T027 status. Keep rollout incomplete until actual source/check/profile/owner-settings evidence satisfies SC-005/006; source completion alone is not feature deployment.

## Dependencies, parallel work and delivery

Execution graph: T001–T002 → T003–T008 → US2 (T009–T013) → US1 (T014–T019) → US3 source (T020–T023). US4 tests (T028–T029) can begin after foundations; conformance fixes T030–T031 follow US1. Final source verification follows US1, US2, US3 source and US4. Rollout T024 → T025 → T026 → T027 follows its separate permissions/capability gates and never blocks bounded source preparation by inventing an infrastructure choice.

All four stories are P1. US2 precedes US1 for their real dependency; US3 consumes that same completed review projection. Independent tests use contract fixtures, not an independently reimplemented evaluator. The useful source MVP is US2 + US1 + US4 (read-only caller-policy eligibility), followed by inactive US3 publisher source. It cannot be advertised as an installed merge gate.

Parallel examples after prerequisites: US2 T009/T010 use distinct test files; US1 T014/T015 use distinct test/golden files; US4 T028/T029 use distinct native/HTTP fixture files. US3 T020–T022 intentionally serialize because they own the same Python files. Do not parallelize shared `evaluate.go`, hosting transport, root registration or schema inventories without explicit ownership handoff. Each behavior task follows its failing tests; each increment receives focused source checks and review. No task starts implementation before the full-design gate.

## Requirement and success-criterion coverage

| Requirement | Tasks |
| --- | --- |
| FR-001 | T015, T017, T018, T029 |
| FR-002 | T018, T028, T030, T031 |
| FR-003 | T005, T006, T016, T023 |
| FR-004 | T005, T015, T016, T018, T019 |
| FR-005 | T003, T004, T009, T011, T012 |
| FR-006 | T010, T012, T013, T024, T025, T026 |
| FR-007 | T007, T009, T011, T013 |
| FR-008 | T005, T009, T011, T032 |
| FR-009 | T009, T011, T013 |
| FR-010 | T007, T008, T014, T016, T017 |
| FR-011 | T014, T016, T017 |
| FR-012 | T003, T007, T008, T020, T022, T029, T030 |
| FR-013 | T010, T013, T017, T021, T022, T029 |
| FR-014 | T014, T016, T020, T022, T023 |
| FR-015 | T020, T021, T022, T024, T025, T026 |
| FR-016 | T024, T025, T026, T034 |
| FR-017 | T027, T032, T034 |
| FR-018 | T002, T019, T032 |
| FR-019 | T001, T003, T005, T007, T009, T010, T014, T015, T020, T021, T028, T029, T033, T034 |
| SC-001 | T002–T019, T028–T030, T034 |
| SC-002 | T014, T016, T020, T023 |
| SC-003 | T015, T018, T019 |
| SC-004 | T028–T031 |
| SC-005 | T024–T026 |
| SC-006 | T001, T025–T027, T033, T034 |

All 34 tasks are planned: US1 six, US2 five, US3 eight, US4 four, shared/setup eight and final evidence three. Conditional rollout tasks remain unchecked until their explicit authorization and evidence exist.
