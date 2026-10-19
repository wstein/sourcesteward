# Tasks: Tidy integration proofs, PR discovery and bounded planning

**Gate**: all boxes unchecked; this list is a design artifact. Each behavior task starts with an observed failing test (TDD), then the implementation task, then relevant checks. Focused Conventional Commits per task group. No build, release, tag, installation or `apply` on a real repository happens in any task; T030 and T031 require explicit owner authorization named for that operation.

## Phase 1: Design gate and baseline

- [ ] T001 Obtain independent executed cross-artifact review of the exact committed `spec.md`, `plan.md`, `tasks.md`, `data-model.md` and contracts; resolve findings and have changed rules re-reviewed.
- [ ] T002 Characterize the stage2 baseline: confirm every path and symbol in the plan, freeze checksums of `schemas/v1/`, `schemas/v2/` and old stored-plan fixtures, and record the owner of each source file.
- [ ] T003 Add failing reproduction tests for #79 (`submodule.*`, `core.hooksPath`), #80 (150 refs/120 worktrees over 64 KiB) and #81 (shared OID, duplicate hint, rebase-merge offline) against temporary repositories and transport doubles; record the observed failure text and the cause of #81 (page cap, duplicates or hint path).

## Phase 2: Bounded streaming reads (FR-024..FR-026; fixes #80)

- [ ] T004 Add failing tests in `internal/workspace`: v1 and v3 plan with 150 refs and 120 worktrees; boundary fixtures 1,024 passes, 1,025 refuses `git-observation-limit`, 4,325,377 stdout bytes and 65,537 stderr bytes refuse with the named limit; record longer than 4,224 bytes refuses; parser holds one record (allocation assertion); v1/v2 output bytes unchanged.
- [ ] T005 Implement streamed `for-each-ref` and `worktree list --porcelain -z` parsing through the bounded stream executor for v1 and v3 callers; pass T004; keep `nativeOutputLimit` for non-collection reads.
- [ ] T006 Run the full v1/v2 conformance and published-schema byte checks after T005; fix regressions only.

## Phase 3: Configuration policy (FR-019..FR-023; fixes #79)

- [ ] T007 Add failing matrix tests C1..C11 from [configuration policy](contracts/configuration-policy.md), each with a marker-file helper, asserting plan success or the named refusal and an absent marker.
- [ ] T008 Add mutant tests M1/M2 (drop `-c core.hooksPath`; inherit global config) that must fail the matrix.
- [ ] T009 Implement the planning environment and the admission-read classification (neutralize versus refuse) and the plan `ConfigNote` records; pass T007 and T008.
- [ ] T010 Add failing apply tests: hook-configured repository plans but apply refuses ref deletion naming the key class; submodule-present retirement retains; existing refusals unchanged.
- [ ] T011 Implement the apply-side refusal and the plan `apply_effect` note; pass T010.

## Phase 4: Forge-free integration proofs (FR-001..FR-012)

- [ ] T012 Add failing proof-model tests: kinds, strengths, headline ordering, protected refs retain, evidence for `ancestor` unchanged from Spec 029.
- [ ] T013 Add failing `patch-id` tests: rebase-merge of three commits (3 of 3), reordered base-side commits, partial match, merge commits skipped and counted, empty-patch commit never matches, duplicate patch-id, commit-count bounds `proof-limit`, `cherry` output limits and malformed lines.
- [ ] T014 Implement `patch-id` through fixed argv under the planning environment; pass T012 and T013.
- [ ] T015 Add failing `merge-noop` tests: rebase-merge and squash-merge noop; revert-after-merge not noop while patch-id matches; conflict; would-add-content; ancestor short-circuit; refusal for `merge.*.driver`/`filter.*`/`diff.external`; repository `objects/`, refs and `packed-refs` byte-identical before and after planning.
- [ ] T016 Implement `merge-noop` with the private object directory and exit/output handling; pass T015.
- [ ] T017 Add failing order, budget and apply tests: proof order, 60 s aggregate budget with injected clock (remaining tips `proof-budget-exhausted`, plan valid), apply re-evaluates the recorded kind with fresh state and retains on change.
- [ ] T018 Implement ordering, budget and apply re-evaluation; pass T017.
- [ ] T019 Add failing CLI/schema tests for `--integration-proof` parsing (subsets, duplicates, unknown kinds exit 2 before native work), offline operation with no network/auth/state, plan-identity binding of policy and kinds, v3 candidate schema validation and human/JSON agreement.
- [ ] T020 Implement `--integration-proof`, result fields, plan binding and the v3 candidate schema additions; pass T019.

## Phase 5: Merged-PR discovery (FR-013..FR-018; fixes #81)

- [ ] T021 Research, with deterministic doubles and a recorded read-only measurement design, why #81 needed hints and whether the 1,000-record cap truncates this repository; record the decision on a second endpoint (OQ-5) in `research.md`.
- [ ] T022 Add failing discovery tests: 40 refs sharing 12 OIDs plan without `--pr`; duplicate identical hint accepted; same OID with two numbers, hint for an unselected OID and malformed hints fail with messages naming the selector (escaped, bounded); multiple admissible PRs need a hint; request assertions show only allow-listed GETs and one re-read per chosen PR.
- [ ] T023 Implement per-OID hint aggregation, idempotent hints and selector-naming errors; pass T022.
- [ ] T024 Add failing coverage tests: pagination ending within bounds is `complete`; a tenth full page is `truncated` with `discovery-truncated`, plan still completes and claims no absence; error and 404 never prove absence.
- [ ] T025 Implement coverage reporting; pass T024; add any endpoint decided in T021 to the closed set, route allow-list and tests together (omit if none).
- [ ] T026 Re-run Spec 029 online and tracking-ref conformance unchanged after T023 and T025.

## Phase 6: Documentation, review and real-repository acceptance (FR-027..FR-030)

- [ ] T027 Update public tidy docs, examples, capability limits, `docs/progress.md`, `docs/feature-status.json` and verification notes only to the behavior that exists; keep status `planned` until acceptance.
- [ ] T028 Run source checks (`gofmt`, `go test ./...`, `go vet ./...`, `go mod verify`, `python3 scripts/check-docs.py`, `python3 scripts/check-architecture.py`, `git diff --check`) on the exact source SHA and obtain independent executed review of that SHA; resolve findings.
- [ ] T029 Reproduce the dogfood measurement (merge-noop, patch-id, would-add, conflict counts and the rejected cherry-pick simulation) with a recorded command list into `verification.md` as baseline.
- [ ] T030 Owner gate: obtain explicit authorization for one named CLI build used only for plan-only acceptance.
- [ ] T031 With the authorized build, run `stw tidy plan` only (temporary state directory, never `apply`) on this repository (149 branches) and on the Anchorage umbrella; record counts before (stage2 baseline) and after per proof kind and disposition, plus every unexplained retention.
- [ ] T032 Record acceptance outcomes and gaps in `verification.md` and `docs/progress.md`; mark verified only with this evidence.
- [ ] T033 Owner gate: any `apply`, release, tag, publication or consumer activation is separately authorized and out of this task list.

## Dependencies and ownership

Table guard: one table, one row per task, prerequisites sorted and acyclic.

| Task | Prerequisites |
| --- | --- |
| T001 | none |
| T002 | T001 |
| T003 | T002 |
| T004 | T003 |
| T005 | T004 |
| T006 | T005 |
| T007 | T003 |
| T008 | T007 |
| T009 | T007, T008 |
| T010 | T009 |
| T011 | T010 |
| T012 | T003 |
| T013 | T012 |
| T014 | T009, T013 |
| T015 | T012, T014 |
| T016 | T009, T015 |
| T017 | T014, T016 |
| T018 | T017 |
| T019 | T018 |
| T020 | T019 |
| T021 | T003 |
| T022 | T021 |
| T023 | T022 |
| T024 | T023 |
| T025 | T024 |
| T026 | T025 |
| T027 | T006, T011, T020, T026 |
| T028 | T027 |
| T029 | T003 |
| T030 | T028, T029 |
| T031 | T030 |
| T032 | T031 |
| T033 | T032 |

Slices A, B and D may proceed in parallel in separate worktrees after T003 with disjoint files; slice C needs the planning environment (T009) because proof commands run under it. At most two editing authors. Shared files (`tidy_v3_result.go`, schema) have one owner at a time.

## Requirement and success-criterion trace

| Requirements | Tasks | Outcome |
| --- | --- | --- |
| FR-001..FR-002, FR-005..FR-007 | T012, T014, T017..T020 | proof kinds, order, no heuristics, selection |
| FR-003 | T013, T014 | patch-id semantics and refusals |
| FR-004, FR-009, FR-010 | T015, T016 | merge-noop, private object dir, refusals |
| FR-008, FR-011, FR-012 | T013, T017..T020 | bounds, offline binding, apply re-evaluation |
| FR-013..FR-017 | T022..T025 | per-OID discovery, hints, coverage |
| FR-018 | T021, T025 | endpoint decision from evidence |
| FR-019..FR-023 | T007..T011 | configuration policy and sentinels |
| FR-024..FR-026 | T004..T006 | streaming and hard bounds |
| FR-027, FR-029, FR-030 | T027..T032 | acceptance, schema, no new effects |
| FR-028 | all behavior tasks | failing tests first |
| SC-001 | T013..T018 | proof fixtures |
| SC-002 | T004..T006 | large plan fixtures |
| SC-003 | T007..T011 | sentinels and mutants |
| SC-004 | T022..T025 | discovery fixtures |
| SC-005 | T006, T020, T026 | contract byte stability |
| SC-006 | T029, T031, T032 | recorded counts |

There are 33 tasks. Every task has an unchecked sequential ID.
