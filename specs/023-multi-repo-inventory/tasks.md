# Tasks: Multi-repository inventory

**Input**: [spec.md](spec.md), [plan.md](plan.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/repos.md](contracts/repos.md), [analysis.md](analysis.md).
**Tests**: Required by constitution principle IV. Write each behavior test and see it fail first. Checkboxes record execution, not design approval.
**Work items**: WI1 to WI5 in stacking order; each is one reviewable change ending with `make check` green. Tasks name the requirements (FR), stories (US) and success criteria (SC) they cover.

## Phase 1: Analysis

- [ ] T001 Record independent cross-artifact analysis in specs/023-multi-repo-inventory/analysis.md and resolve every critical, high and medium finding before code.

## WI1 — Contract, types and command skeleton

- [ ] T002 [FR-001, FR-012, SC-004] Failing contract tests, then schemas/v1/repos.schema.json, internal/cli/testdata/repos-*.json (ok, partial, error, skipped, omitted inventory, duplicate) and internal/contracts/repos_test.go: fixtures validate offline, invariants of the data model are enforced (null arrays rejected, `ok` with error rejected), every embedded inventory validates against inventory.schema.json, existing fixtures stay valid.
- [ ] T003 [FR-003, FR-012, FR-013] Failing tests, then internal/inspection/repos_result.go: types, code and limit constants (one place), `ValidateReposOptions` (0 or more than 16 paths, empty, NUL, invalid UTF-8, more than 4,096 bytes), overall-status rule, JSON size estimate.
- [ ] T004 [FR-001, FR-014, US5] Failing CLI tests, then internal/cli/repos.go and registration: argument handling, text and JSON from one result, exit mapping 0/1/2 with the precedence of the spec, no stdin read, no process started by the CLI package; `make check` architecture test unchanged.

## WI2 — Paths, duplicates, nesting

- [ ] T005 [FR-003, FR-004, US6] Failing tests, then internal/inspection/repos_paths.go and the entry check in repos.go: resolved path, `invalid-path`, missing path, file path, symlink alias accepted with both paths reported, subdirectory and uninitialized-submodule directory give `not-repository-root` with the inventory discarded (real temporary repositories; fake inventory for pure cases).
- [ ] T006 [FR-005, US6] Failing tests: pure path duplicates dropped before any process; Git main plus linked worktree, and jj workspace plus secondary workspace (jj lane) give `duplicate-repository` for the later one with `duplicate_of`; nested listed repositories set `contained_in`; a submodule checked out in a linked worktree of its superproject is not a duplicate of the main worktree's.

## WI3 — Discovery

- [ ] T007 [FR-006, FR-008, FR-009, US3] Failing tests, then internal/inspection/repos_discover.go: HEAD `.gitmodules` regular-file check, key parsing (names with dots, paths with spaces and newlines), gitlink confirmation by literal pathspec, entry without gitlink counted in `ignored`, no `.gitmodules`, unborn HEAD, over-64 KiB `.gitmodules` (`discovery-failed`), non-regular `.gitmodules` blob (crafted tree), sorting by path bytes, 16-entry cap with `discovery-limit` leaving no partial adds.
- [ ] T008 [FR-008, US3] Failing hostile tests: registered paths `..`, absolute, leading dash, `a/../b`, symlinked path and symlinked intermediate component, `.git` pointer outside the superproject modules path, in-place `.git` directory accepted, linked-worktree modules path, `extensions.submodulePathConfig` name containing a slash on the current Git lane; process-free proof (a fake runner records that no Git command ran in a refused member).
- [ ] T009 [FR-006, US3] Failing tests: uninitialized submodule is `skipped` without a process; explicit-plus-discovered duplicate keeps the explicit entry; `--submodules` on a jj path gives `not-applicable`; discovery not recursive.

## WI4 — Orchestration, bounds, facts

- [ ] T010 [FR-002, FR-007, FR-010, FR-013, US2, US5] Failing tests with an injected inventory function: four workers at most, order independent of completion order (randomized delays), per-entry panic recovery (`member-failed`), total deadline (`inventory-timeout` for started, `inventory-not-started` for unstarted), cancellation, one failing entry leaves others intact; `go test -race` case.
- [ ] T011 [FR-011, SC-001, US4] Failing tests, then internal/inspection/repos_facts.go: derivation table on Git and jj inventory fixtures (jj `dirty` unknown), stale, foreign and unverified counts, conflicted references, summary arithmetic equals recomputation from entries.
- [ ] T012 [FR-013, US5] Failing tests: output bound with oversized fake inventories drops later inventories to `inventory-limit` keeping facts; fallback to facts-only when the final re-check fails; result stays valid and under 8 MiB.
- [ ] T013 [FR-002, SC-001] Integration tests against real temporary Git repositories (linked worktree, stale worktree, dirty, nested, submodules): each embedded inventory equals `Inventory` for the same path.

## WI5 — Documentation, conformance, acceptance

- [ ] T014 [P] [FR-014] Write docs/repos.md; update docs/json-contracts.md, docs/commands.md, docs/agent-guide.md (the command, its exit statuses, limits), docs/README.md, README scope line and llms.txt; keep `python3 scripts/check-docs.py` passing. State limitations (no ahead/behind, Git-only discovery, 60 s).
- [ ] T015 [P] Ensure the generated command reference includes `stw repos` through the existing docs generator; run no site build without approval.
- [ ] T016 [FR-015, FR-016, SC-002, SC-003, SC-005] Add scripts/smoke-repos.py and scripts/test_repos_smoke.py in the style of smoke-inventory.py: five-member workspace (Git, Git with linked worktree, Git superproject with submodules, jj and jj with secondary workspace on the jj lanes), a hostile member with hook, filter, fsmonitor, textconv and credential-helper sentinels, a foreign-owned member where the environment allows (otherwise recorded as not exercised), a network sentinel (registered remote helper that fails the test), per-member byte-level sentinels (refs, HEAD, index bytes and mtime, reflogs, configuration, no lock files, `.gitmodules` unchanged; jj working copy and operation log), two jj members run in parallel; wire into the existing minimum and current lanes without adding platforms.
- [ ] T017 [SC-005] Run `make check`, `go test -race ./...`, `python3 scripts/check-architecture.py` and the smoke script; record versions, platforms and outcomes in specs/023-multi-repo-inventory/verification.md, labelling arm64 and darwin as local evidence and listing anything not run.
- [ ] T018 Update docs/progress.md and docs/feature-status.json with actual evidence; leave release, tag and publication out.

## Dependencies

WI1 first. WI2 and WI3 may proceed in either order after WI1 (WI3 uses WI2's entry checks). WI4 after WI2 and WI3. WI5 last; T014 may start once the WI1 contract text is stable. If Spec 021's registry reader reaches main first, T007 reuses it behind the `discover` seam without contract change.
