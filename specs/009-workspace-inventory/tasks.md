# Tasks: Workspace, State and Reference Inventory

All tasks require focused Conventional Commits. Tests precede behavior implementation. No source implementation before analysis clears. All FRs remain in scope; unsupported native observations must be explicit, not silently deferred.

## Phase 1 — Setup/design
- [ ] T001 Review spec/constitution and freeze CLI/model contracts in specs/009-workspace-inventory/contracts/inventory.md (FR-001–015).
- [ ] T002 Independently analyze spec/plan/tasks and resolve findings in specs/009-workspace-inventory/analysis.md before implementation (FR-009).

## Phase 2 — Foundation
- [ ] T003 Write failing common options/result/cancellation/record-limit tests in internal/inspection/inventory_test.go (FR-007/008/010).
- [ ] T004 Implement InventoryOptions, InventoryResult and typed observations in internal/inspection/inventory.go; enforce ten-second total, 65,536 native bytes, 1,048,576 serialized bytes and 1,024 total records; arrays never null (FR-007/008).
- [ ] T005 Preserve ownership selection and isolate a reusable safe jj session callback in internal/inspection/jj.go with regression tests in internal/inspection/jj_test.go (FR-005/006).

## Phase 3 — US1: registered workspace inventory (P1)
Independent test: Git linked + jj standalone/colocated/secondary workspace registrations return native identities; missing/foreign paths remain explicit and unchanged.
- [ ] T006 [P] [US1] Write failing Git registered-worktree/missing/bare/unborn/detached/foreign-path tests in internal/inspection/inventory_git_test.go (FR-001/002/004/006).
- [ ] T007 [P] [US1] Write failing native jj registration/root-optional/foreign-manager tests in internal/inspection/inventory_jj_test.go (FR-001/002/004/006).
- [ ] T008 [US1] Implement bounded registered Git worktree parsing in internal/inspection/inventory_git.go; task_owner remains unknown, registration path is nullable (FR-001/002/004).
- [ ] T009 [US1] Implement operation-pinned native jj workspace enumeration in internal/inspection/inventory_jj.go; preserve separate commit/change/operation IDs and unknown absent roots (FR-001/002/004/006).

## Phase 4 — US2: qualified state (P1)
Independent test: distinguish staged/disk/stored/untracked/conflict scopes; unsnapshotted jj disk observations remain unknown; target/effect hashes unchanged.
- [ ] T010 [P] [US2] Add failing Git staged/disk/untracked/conflict/unreadable/concurrent-state tests in internal/inspection/inventory_git_test.go (FR-003/005/008).
- [ ] T011 [P] [US2] Add failing jj stored-vs-disk/conflict/stale-operation tests in internal/inspection/inventory_jj_test.go (FR-003/005/008).
- [ ] T012 [US2] Implement fixed read-only Git status observations with no optional locks/helpers/index refresh and freshness sampling in internal/inspection/inventory_git.go (FR-003/005/008).
- [ ] T013 [US2] Implement jj native stored state and explicit disk/staged capability qualifications in internal/inspection/inventory_jj.go (FR-003/005/006).

## Phase 5 — US3: bounded human/structured contract (P1)
Independent test: same model human/JSON, errors/schema/regressions, mandatory native versions, malicious helpers and limit/cancellation cases.
- [ ] T014 [P] [US3] Write failing Cobra parsing/human-JSON/predicate-only known-false exit0/explicit-extra-scope partial-error exit1/syntax exit2 tests in internal/cli/inventory_test.go (FR-007/008/010).
- [ ] T015 [P] [US3] Write failing inventory success/partial/error schema fixtures in internal/contracts/contracts_test.go (FR-007/009).
- [ ] T016 [US3] Add CLI parsing/rendering from shared result only in internal/cli/inventory.go and register in internal/cli/root.go (FR-007/008).
- [ ] T017 [US3] Publish new inventory-only schema v1 in schemas/v1/inventory.schema.json and validate actual output fixtures without modifying inspect/version contracts (FR-007/009).
- [ ] T018 [US3] Add real-process zero-write/helper/network/missing-backend/unsupported-version/limit/concurrency fixtures in scripts/smoke-inventory.py (FR-005/006/008/009).
- [ ] T019 [US3] Add mandatory minimum/current inventory smoke to .github/workflows/ci.yml; missing backend/sandbox evidence fails acceptance (FR-009).

## Phase 6 — US4: predicates and literal files (P1)
Independent test: true/false/unknown separate from failure; literal unusual filenames, rename/deletion and unsupported scopes disclose exact completeness.
- [ ] T020 [P] [US4] Add failing native Git predicate/invalid-operand/literal-file/rename/deletion/root-vs-multiparent stored tests in internal/inspection/inventory_git_test.go (FR-010/014).
- [ ] T021 [P] [US4] Add failing native jj predicate/literal-file/merged-parent stored-diff/unsupported-staging tests in internal/inspection/inventory_jj_test.go (FR-010/014).
- [ ] T022 [US4] Implement clean/dirty/ref-exists/ancestor predicates and literal workspace/cwd file filtering in internal/inspection/inventory_git.go (FR-010/014).
- [ ] T023 [US4] Implement native jj predicates and stored path records with explicit disk/staging gaps in internal/inspection/inventory_jj.go (FR-010/014).

## Phase 7 — US5: references, integration, bases (P1)
Independent test: explicit remote samples/base/revision, multiple candidates, no implicit fetch or naming guesses, containment distinct from ancestry and publication.
- [ ] T024 [P] [US5] Add failing Git remote/upstream/containment/local-only/ambiguous-base tests in internal/inspection/inventory_git_test.go (FR-011/012/013/015).
- [ ] T025 [P] [US5] Add failing jj bookmark/remote/conflicted-reference/ancestry/base provenance tests in internal/inspection/inventory_jj_test.go (FR-011/012/013/015).
- [ ] T026 [US5] Implement Git reference/base candidate inventory and sampled reachability in internal/inspection/inventory_git.go; no main/master/origin assumptions (FR-011/012/013/015).
- [ ] T027 [US5] Implement native jj bookmarks/remote relations/base candidate qualifications and sampled reachability in internal/inspection/inventory_jj.go; alias config remains data, never evaluated (FR-011/012/013/015).

## Phase 8 — Polish/review/evidence
- [ ] T028 Update documented capabilities, schema URL/packaging and guidance in docs/backends.md, docs/json-contracts.md, docs/agent-guide.md and scripts/package-pages.py.
- [ ] T029 Run gofmt, make check, go test -race ./..., go vet ./..., go mod verify and git diff --check; record exact commands/results in specs/009-workspace-inventory/verification.md.
- [ ] T030 Obtain independent exact-revision source/safety/schema review and resolve findings; rerun analysis in specs/009-workspace-inventory/analysis.md.
- [ ] T031 Update docs/progress.md and docs/feature-status.json honestly; open implementation PR after focused commits and attach it; record hosted minimum/current evidence or remaining acceptance gaps in specs/009-workspace-inventory/verification.md.

## Dependencies and parallel execution
T001→T002→T003/T004→T005 gates adapter implementation. US1 precedes US2; US4/US5 need native inventory foundations. CLI/schema US3 can proceed against frozen models in parallel with adapters. Within US1, US2, US4 and US5, Git and jj tests/implementation are independent isolated-worktree assignments; do not overlap edits to shared jj setup/model/CLI without coordination. T018/T019 require integrated adapters; T029→T030→T031 completes handoff.

Suggested internal MVP is US1, but PR acceptance includes all five stories and FR-001–015; an MVP is not grounds to mark omitted requirements complete.

## Requirement traceability
FR-001:T006–009; FR-002:T006–009; FR-003:T010–013; FR-004:T006–009; FR-005:T005,T010–013,T018; FR-006:T005–009,T013,T018; FR-007:T003–004,T014–017; FR-008:T003–004,T010–012,T014,T016,T018; FR-009:T002,T015,T017–019,T029–031; FR-010:T003,T014,T020–023; FR-011:T024–027; FR-012:T024–027; FR-013:T024–027; FR-014:T020–023; FR-015:T024–027.

31 tasks: setup2, foundation3, US1=4, US2=4, US3=6, US4=4, US5=4, polish4. All task checkboxes remain unchecked until actual execution evidence exists.
