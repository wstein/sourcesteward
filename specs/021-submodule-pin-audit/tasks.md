# Tasks: Submodule pin audit

**Input**: [spec.md](spec.md), [plan.md](plan.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/pins.md](contracts/pins.md), [analysis.md](analysis.md).
**Tests**: Required by constitution principle IV. Write each behavior test and observe it fail before the implementation. Task checkboxes record execution, not design approval.
**Work items**: Tasks are grouped into five work items, WI1 to WI5. Each is one reviewable change that builds on the previous one and ends with `make check` green; WI order is the stacking order of the pull requests. Each task names the requirements (FR) and stories (US) it covers.

## Phase 1: Analysis

- [X] T001 Record independent cross-artifact analysis in specs/021-submodule-pin-audit/analysis.md, resolve every critical or high finding before any code, and preserve review provenance.

## WI1 — Contract, refusal path and exit status

- [ ] T002 [FR-014, SC-004] Add failing schema fixture tests then add schemas/v1/pins.schema.json and internal/contracts/pins_test.go with success, incomplete, refused and error fixtures in internal/cli/testdata/pins-*.json; the schema pins the finding-code list and the refusal codes; existing fixtures stay valid.
- [ ] T003 [FR-014] Add failing tests then define PinsResult, Boundary, Submodule, Checkout, Publication and Finding in internal/inspection/pins_result.go, the normative finding table, `ValidatePinsOptions`, deterministic ordering, arrays never null, and the completeness rules of the data model.
- [ ] T004 [P] [FR-002, US5] Add failing tests then implement marker-based ownership detection and the refusal table in internal/inspection/pins.go: Git worktree and linked worktree proceed; standalone, colocated and secondary jj and GitButler give `unsupported-backend`; conflicting markers give `ambiguous-backend`; bare gives `bare-repository`; none gives `not-repository`. Prove no process starts on any refusal.
- [ ] T005 [FR-001, FR-014, US5] Add failing CLI tests then add internal/cli/pins.go (`stw pins [PATH] [--json] [--strict]`), text rendering with control-character escaping, JSON from the same result, and the typed strict error and exit-status precedence in internal/cli/root.go (usage 2; refusal, failure, cancellation or incomplete 1; strict error findings 3; otherwise 0).

## WI2 — Registry and safety

- [ ] T006 [P] [FR-009, US3] Add failing tests then implement worktree and Git-directory validation in internal/inspection/pins_safe.go: no symlinks, no `..`, no absolute paths, no non-UTF-8 paths; expected location from `rev-parse --git-path modules` joined with the submodule name or an in-place `.git` inside the submodule; refuse external `commondir`, alternates and mismatching `core.worktree`; record and recheck directory identity. Include a linked-worktree fixture where the Git directory is under `worktrees/<id>/modules`.
- [ ] T007 [P] [FR-010, US3] Add failing tests then implement the ownership check for submodule worktrees and Git directories and the `submodule-unsafe-owner` refusal before any Git process runs there; record in the verification note which cases the test environment can and cannot exercise.
- [ ] T008 [FR-003, FR-017, US4] Add failing tests then implement the streaming runner in internal/inspection/pins_run.go with retained and scan caps, then registry enumeration in internal/inspection/pins_git.go from HEAD gitlinks, staged entries and the HEAD `.gitmodules` blob (includes disabled): orphan gitlink, entry without gitlink, inactive, `gitmodules-modified` including a symlinked and an unreadable worktree file, unusual names (spaces, newline, leading dash, case-only).
- [ ] T009 [P] [FR-004, US3] Add failing tests then implement URL handling: credential redaction, `url-has-credentials`, scheme validation, and the network sentinel (a registered remote helper that fails the test when invoked); no URL is ever contacted or run.
- [ ] T010 [FR-010, US3] Add failing hostile-fixture tests (hooks, clean and process filters on stat-dirty files, fsmonitor, textconv, external diff, credential helper, alias, `.git` pointer outside, symlinked directory, hostile `core.worktree`) and prove none executes; reuse the existing isolation prefix and extend it where the runner lacks it.

## WI3 — Evidence and findings

- [ ] T011 [FR-005, US2] Add failing tests then implement checkout evidence: initialized state, checked-out commit, branch or detached, relation to the pin with each side counted separately and capped at 10,000 with a `capped` flag, unborn HEAD, shallow and partial repositories, replaced objects never honored.
- [ ] T012 [P] [FR-005, FR-008, US2] Add failing tests then implement dirty state by reusing the inventory comparison that never asks Git to hash content (refactor to an explicit Git directory as its own commit with the inventory tests as the safety net), `unknown` where a filter would be needed, bounded untracked presence, and the index sentinel (bytes and modification time unchanged).
- [ ] T013 [FR-006, US1] Add failing tests then implement publication evidence with `for-each-ref --contains --count=17 refs/remotes/`: `reachable`, `unreachable`, `unknown` with reason, truncation flag, advisory `FETCH_HEAD` time, remote name per ref; local branches and tags never count; fork-only containment and multi-remote URL-match fixtures.
- [ ] T014 [P] [FR-007] Add failing tests then implement the tracking relation from the `.gitmodules` branch or the remote default ref with bounded counts.
- [ ] T015 [FR-013, US4] Add failing tests then implement nested-submodule detection from the submodule HEAD tree and `submodule-unsupported-backend` for submodules that are themselves jj- or GitButler-managed.
- [ ] T016 [FR-003, FR-004, SC-001] Add failing tests then implement findings derivation from the normative table and the summary, including `pin-unpublished` for the incident fixture in a main worktree and in a linked worktree.
- [ ] T017 [FR-011, FR-012, FR-014] Add failing tests then implement bounds and consistency: 256-submodule cap, per-submodule and total deadlines, cancellation, output beyond 1 MiB, a subprocess exceeding its scan allowance, resample of superproject HEAD, index identity and each submodule HEAD, incomplete results with `pins-limit` or `pins-state-changed`, an unborn and a detached superproject HEAD.

## WI4 — Documentation and contract publication

- [ ] T018 [P] [FR-015] Write docs/pins.md (command, evidence, findings, exit statuses, limits, what a finding does and does not prove); update docs/backends.md (Git supported, jj and GitButler unsupported for submodules), docs/agent-guide.md (exit status 3 and the precedence), docs/json-contracts.md, README scope line, docs/README.md knowledge map and llms.txt; keep `python3 scripts/check-docs.py` passing.
- [ ] T019 [P] Ensure the generated command reference includes `stw pins` through cmd/stw-docs and the existing docs tests; do not run a site build without approval.

## WI5 — Conformance and acceptance evidence

- [ ] T020 [FR-016, SC-002, SC-003, SC-005] Add scripts/smoke-pins.py and scripts/test_pins_smoke.py in the style of smoke-inventory.py: real superproject and submodule fixtures on the Git lanes, effect sentinels (refs, HEAD, index bytes and modification time, reflogs, configuration, no lock files, network sentinel), and the jj refusal on the jj lanes with working-copy and operation-log sentinels; wire both into the existing ubuntu-24.04 conformance matrix without adding platforms.
- [ ] T021 [SC-005] Run `make check`, `go test -race ./...`, `python3 scripts/check-architecture.py` and the smoke scripts; record exact versions, platforms and outcomes in specs/021-submodule-pin-audit/verification.md, labelling linux arm64 and darwin arm64 results as local evidence not covered by CI and listing anything not run.
- [ ] T022 Update docs/progress.md, docs/feature-status.json and the roadmap with the actual evidence; leave release, tag, publication and new CI platforms out of this feature.

## Dependencies

WI1 before everything. WI2 before WI3 (evidence needs validated submodule directories). WI4 may start after the WI1 contract text is stable and finishes after WI3. WI5 last. Tasks marked [P] within a work item touch different files and may run in parallel.
