# Tasks: Local restacking, first slice (`stw restack plan`)

**Input**: `spec.md`, `plan.md`, `data-model.md`, `contracts/restack-plan.md`, `quickstart.md`.
**Tests**: Required by constitution principle IV. Write each behavior test, observe it fail, then implement; characterization tests (T004) are committed green before the refactor they guard. Every task names the requirements (FR) it covers; a mutation control (MC) is a deliberate defect the tests must catch, run and recorded in the task commit message. Task checkboxes record execution, not design approval. No task runs a standalone build, `go install`, a Pages build, a release or a push of anything but the author's topic branch.
**Work items**: WI1 (T002 to T005), WI2 (T006 to T010), WI3 (T011 to T016), WI4 (T017 to T020), WI5 (T021 to T023). Each ends with `make check` green and is one reviewable change; stack them linearly.

## Phase 1: Design and analysis

- [ ] T001 Independent design review of this specification set and cross-artifact analysis in `specs/015-local-restacking/analysis.md`; resolve critical, high and medium findings before any code; preserve provenance. (WP0)

## WI1 — Input, resolver and refusals

- [ ] T002 [FR-011, FR-014] Failing tests for flag syntax: non-fully-qualified refs, `HEAD`, abbreviations, revision expressions and duplicate flags exit 2 without touching Git. MC: accept `HEAD~1`.
- [ ] T003 [FR-011, FR-014] Failing tests then `internal/restack/input.go`: resolution to commit ids, `ref-not-found`, `ref-ambiguous`, `ref-not-commit`, `object-missing`, `head-changed`, `unrelated-histories`, `merge-commit-in-range`, `range-too-large`; moved range exactly `old_parent..old_head`. MC: resolve with `^{}`-less tag objects; use the three-dot range.
- [ ] T004 [FR-014] Characterization tests (green first) of the Spec 029/031 hazard and ownership safeguards and of the patch-id and touched-path proof helpers in `internal/workspace`, then the extraction refactor with unchanged results (no behavior change).
- [ ] T005 [FR-014] Failing tests then `hazards.go` and the dirty-target, `operation-in-progress`, branch-checked-out-elsewhere, symlink, `core.hooksPath`, `core.fsmonitor`, `include`, replace-ref, shallow, merge-driver and attribute refusals. MC: skip each hazard once.

## WI2 — Evidence library

- [ ] T006 [FR-013] Failing tests then range pairing of old commits against replayed commits with the `git range-diff` marks; fixtures for equal, changed and dropped commits. MC: ignore the `!` mark.
- [ ] T007 [FR-013] Failing tests then `git cherry` classification of `old.base..old_parent` against the new parent with tree equivalence for `+` rows. MC: treat `+` as dropped; compare whole trees instead of touched paths.
- [ ] T008 [FR-013] Failing tests then own-diff patch-id equality. MC: compare commit ids instead of patch ids.
- [ ] T009 [FR-013, FR-015] Failing tests then the identity check over author and committer: pass, fail with mismatch list, `unknown` with no expected identity, identities only from environment and flags; a sentinel proves no `git config` write and no `user.*` read. MC: report `pass` when no expected identity exists.
- [ ] T010 [FR-013, FR-016] Failing tests then the touched-paths overlap indicator, including the signature-change fixture: clean replay, `overlap: none`, `limitations` contains `semantic-breaks-not-detected`. MC: drop the limitation string.

## WI3 — Replay, effects and texts

- [ ] T011 [FR-012, FR-013] Failing tests then quarantine replay by `merge-tree` and `commit-tree`: clean multi-commit replay, tree equal to a real `git rebase --onto` of a copy of the fixture, committer from the environment. MC: write objects into the repository store.
- [ ] T012 [FR-012] Sentinel test: refs, config, index, worktree, hooks and the object-store listing are identical before and after a plan on every path (clean, conflict, refusal, cancellation, bound). MC: leave the quarantine behind.
- [ ] T013 [FR-013, FR-014] Failing tests then conflict prediction: first conflicting commit, paths and kinds, `replay_stopped_at`, allowlist class from the configuration file, empty default. MC: classify every conflict `allowlisted`.
- [ ] T014 [FR-013] Failing tests then effects: affected refs containing the old head, worktrees including one holding the branch, hook names by presence without reading content, `run_by_stw: false`. MC: execute a hook sentinel and require the test to catch it.
- [ ] T015 [FR-006, FR-013] Failing tests then lease and resync texts: exact formats of the contract, placeholder `<NEW_FULL_SHA>`, observed old full SHA, remote name only as text, refusal to emit an unquotable ref, no `--force`. MC: emit `--force`; emit a short SHA.
- [ ] T016 [FR-009, FR-018] Failing tests then bounds and cancellation: 200 commits, 2,000 paths, 1 MiB, 60 s each yield `incomplete` naming the bound, never a truncated `ok`. MC: truncate silently.

## WI4 — Result, schema and CLI

- [ ] T017 [FR-017] Failing tests then `schemas/v1/restack-plan.schema.json` and decoder: every fixture validates; required fields; additive-compatibility test (unknown enum values accepted as `unknown`); existing schemas and fixtures byte-identical. MC: remove a required field.
- [ ] T018 [FR-008, FR-017] Failing tests then `stw restack plan` wiring, exit codes 0, 1 and 2, human output from the same result with control characters escaped; jj, GitButler and ambiguous checkouts refused without Git fallback (`unsupported-backend`).
- [ ] T019 [FR-012] Update `scripts/check-architecture.py` (`internal/restack` network-free; process starts only in reviewed files; CLI imports only the reviewed operation) with a failing negative fixture first.
- [ ] T020 [FR-017] Allowlist and copy tests for the new schema (as the other published schemas); no Pages build is implied.

## WI5 — Conformance and records

- [ ] T021 [FR-010, SC-001] Conformance run on minimum and current Git for every acceptance scenario of the first slice; missing lane is not a pass.
- [ ] T022 [SC-002, SC-004] Traceability table FR to test; effect sentinels recorded; update `docs/commands.md`, `docs/progress.md` and, only with acceptance evidence, `docs/feature-status.json`.
- [ ] T023 Independent review of the exact head; at most two rounds per lineage.

## Later slices (not scheduled by this task list)

WP6 keep-both configuration classification, WP7 `restack apply` Git, WP8 jj peer, WP9 Spec 016 L2 push amendment, WP10 declared verify commands: each needs its own plan amendment, tasks, review and, where it mutates or runs code, explicit owner authorization.
