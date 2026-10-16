# Tasks: Submodule pin audit

**Input**: [spec.md](spec.md), [plan.md](plan.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/pins.md](contracts/pins.md).
**Tests**: Required by constitution principle IV. Write each behavior test and observe it fail before the implementation. Task checkboxes record execution, not design approval.
**Work items**: Tasks are grouped into five work items, WI1 to WI5, each one reviewable change that builds on the previous one and ends with `make check` green. WI order is the stacking order of the pull requests.

## Phase 1: Analysis

- [ ] T001 Record independent cross-artifact analysis and requirement coverage in specs/021-submodule-pin-audit/analysis.md; resolve every critical or high finding before any code.

## WI1 — Contract and refusal path (foundation)

- [ ] T002 Add failing schema fixture tests then add schemas/v1/pins.schema.json and internal/contracts/pins_test.go with success, incomplete, refused and error fixtures in internal/cli/testdata/pins-*.json; existing fixtures stay valid.
- [ ] T003 Add failing tests then define PinsResult, Boundary, Submodule, Checkout, Publication, Finding and the finding-code table in internal/inspection/pins_result.go; `ValidatePinsOptions`; sorted deterministic output; arrays never null.
- [ ] T004 [P] Add failing ownership tests then implement the entry in internal/inspection/pins.go: Git worktree and linked worktree proceed; standalone, colocated and secondary jj, GitButler, ambiguous ownership and bare repositories return `refused` with the contract's codes and run no Git submodule command.
- [ ] T005 Add failing CLI tests then add internal/cli/pins.go wiring `stw pins [PATH] [--json] [--strict]`, text rendering with control-character escaping, JSON from the same result, and the strict status 3 in internal/cli/root.go; usage errors exit 2.

## WI2 — Registry and safety

- [ ] T006 [P] Add failing tests then implement path and Git-directory validation in internal/inspection/pins_safe.go: no symlinks, no `..`, no absolute paths, Git directory only under the common directory's `modules/` or an in-place `.git` inside the submodule; refusal code `submodule_path_unsafe`.
- [ ] T007 Add failing tests then implement registry enumeration in internal/inspection/pins_git.go from HEAD tree gitlinks, staged index entries and `.gitmodules` (NUL-delimited, isolated runner): orphan gitlink, entry without gitlink, inactive, unusual names (spaces, newline, leading dash, non-UTF-8, case-only).
- [ ] T008 [P] Add failing tests then implement URL handling: credential redaction, `url_has_credentials`, scheme validation, no URL is ever contacted or used as a command.
- [ ] T009 Add failing hostile-fixture tests (hooks, filters, fsmonitor, textconv, external diff, credential helper, alias, `.git` pointer outside, symlinked directory) and prove none executes; reuse the existing runner isolation.

## WI3 — Evidence and findings

- [ ] T010 Add failing tests then implement checkout evidence: initialized state, checked-out commit, branch or detached, relation to the pin with capped counts (`same`, `ahead`, `behind`, `diverged`, `unrelated`, `unknown`), shallow and partial repositories.
- [ ] T011 [P] Add failing tests then implement dirty state: tracked changes via the optional-lock-free status form, bounded untracked presence; sentinel proving index bytes and modification time unchanged.
- [ ] T012 Add failing tests then implement publication evidence: ancestor test against at most 16 `refs/remotes/*` tips, `reachable`, `unreachable`, `unknown` with reason, advisory `FETCH_HEAD` time; local branches and tags never count; fork-only containment fixture.
- [ ] T013 [P] Add failing tests then implement tracking relation from `.gitmodules` `branch` or the remote default ref, with bounded counts.
- [ ] T014 Add failing tests then implement findings derivation (the code table) and the summary, including `pin_unpublished` for the incident fixture (a gitlink pointing at a commit never pushed).
- [ ] T015 Add failing tests then implement bounds and consistency: 256-submodule cap, per-submodule and total deadlines, cancellation, output bound, resample of superproject HEAD, index identity and each submodule HEAD, incomplete results with `limit_exhausted` or `state_changed_during_audit`.

## WI4 — Documentation and contract publication

- [ ] T016 [P] Write docs/pins.md (command, evidence, findings, exit statuses, limits, what a finding does and does not prove); update docs/backends.md capability matrix (Git supported, jj and GitButler unsupported for submodules), README scope line, docs/README.md knowledge map and llms.txt; keep `python3 scripts/check-docs.py` passing.
- [ ] T017 [P] Ensure the generated command reference includes `stw pins` through cmd/stw-docs and the existing docs tests; do not run a site build without approval.

## WI5 — Conformance and acceptance evidence

- [ ] T018 Add scripts/smoke-pins.py and scripts/test_pins_smoke.py in the style of smoke-inventory.py: real superproject and submodule fixtures on the Git lanes, effect sentinels, and the jj refusal on the jj lanes; wire both into the CI conformance matrix for linux amd64 and arm64 and add a darwin arm64 lane.
- [ ] T019 Run `make check`, `go test -race ./...`, `python3 scripts/check-architecture.py` and the smoke scripts locally; record exact versions, platforms and outcomes, plus anything not run, in specs/021-submodule-pin-audit/verification.md.
- [ ] T020 Update docs/progress.md and the roadmap with the actual evidence; leave release, tag and publication out of this feature.

## Dependencies

WI1 before everything. WI2 before WI3 (evidence needs validated submodule directories). WI4 may start after WI1 contract text is stable and finishes after WI3. WI5 last. Tasks marked [P] within a work item touch different files and may run in parallel.
