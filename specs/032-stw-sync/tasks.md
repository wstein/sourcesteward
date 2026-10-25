# Tasks: `stw sync`

**Gate**: the design review of this spec is CLEAR (T001), so the runtime boxes below are ticked as the implementation was delivered (closing PR, 2026-10-25). Tests use temporary repositories with a bare remote and linked worktrees; never a real user repository. Failing test first for each task. Each slice updates `docs/commands.md` and related docs for what it ships.

## Slice 0 — Design

- [x] T001 Independent review of the exact committed head of this spec (strongest tier, executed checks). Evidence: Opus design review NOT CLEAR at b0cc02d with F1-F8, resolved by the amendment; Opus delta review CLEAR at 3d8d1da.

## Slice 1a — Classification and `--plan`

- [x] T002 Failing tests for the classification table (`fast-forward`, `up-to-date`, `ahead`, `diverged`, `upstream-gone`, untracked branch ignored, dirty worktree `skipped-dirty`) over a temporary repo with bare remote and linked worktrees; then `stw sync --plan [PATH] [--remote NAME] [--prune]` with the single fetch, bounded output, human and `--json`; no local ref, worktree or stash change.
- [x] T003 Failing tests then jj and unsupported path refusal before any effect; usage errors exit 2; exit-code table (`upstream-gone` exits 0).
- [x] T004 JSON schema for the sync result with valid and invalid fixtures; docs entry in `docs/commands.md` and `docs/json-contracts.md`.

## Slice 1b — Real run: fast-forward, prune, journal

- [x] T005 Failing tests then the journal record (`sync` record type on the Spec 029/031 mechanism, keyed by run ID) written before each ref change and completed after, with the recovery ref under `refs/stw/recovery/sync/RUN_ID/N`. Delivered: own `sync` record type, one file per run and repository, not the 029/031 code (see the spec's delivered contract).
- [x] T006 Failing tests then `stw sync [PATH]`: fast-forward of a checked-out clean branch in each linked worktree, `update-ref` with expected old OID for non-checked-out branches, per-item failure isolation, lost race reported `failed`. Delivered as amended by the workspace scope: the current branch of the main worktree of each repository; linked worktrees and `update-ref` of other branches are out of scope.
- [x] T015 Failing tests then `skipped-busy` in plan and run for an operation in progress in the repository (`rebase-merge`, `rebase-apply`, merge, cherry-pick, revert or bisect), in the root and in a submodule: the branch, the worktree and the operation state of the Git directory (all refs, HEAD, index, `MERGE_*`, `ORIG_HEAD`, `BISECT_*`, `rebase-*` files) stay unchanged, nothing is journaled and the exit status is 0 as in the delivered contract. Locked or linked worktrees are out of scope (linked worktrees are never touched; when stw is started inside one, a note names it). Delivered by `TestSkippedBusyForEveryOperationInPlanAndRun` and `TestSyncSkipsBusyRepos`.
- [x] T016 FR-013 hooks test: a repository with `post-merge`, `post-rewrite` and `reference-transaction` hooks and a `core.hooksPath` setting is not refused; the hooks run in a real run (marker files) and in the plan's fetch; a hook exiting non-zero makes only that item `failed`; stw installs and removes no hook. Delivered by #264 (`internal/stwsync/hooks_test.go`): hooks in a `core.hooksPath` directory run in a real run and in the plan's fetch (the `reference-transaction` hook only, a plan merges and rewrites nothing), a failing hook fails only its item, and no hook file is added, removed or changed.
- [x] T017 FR-002 race test: a hook or test seam moves the branch (and, for a checked-out branch, `HEAD`) between classification and apply; the item is `failed` with `head-moved` or `raced`, stw's own change is absent, and other items proceed. Includes the `ORIG_HEAD` record check after a normal `merge --ff-only`. Delivered (`TestLostRaceIsFailedAndNeverRepaired`, `TestOrigHeadAndPlannedTipAreCheckedAfterTheMerge`).
- [x] T018 Configuration policy test of decision 8 with a configuration that sets `submodule.recurse=true`, `rebase.updateRefs=true`, `rebase.autoStash=true`, `merge.autoStash=true`, `fetch.prune=true` and a branch stacked on another: no submodule HEAD moves, the stacked branch ref is untouched, no hidden stash appears, no ref is pruned without `--prune`; the exact argument vectors are asserted. Delivered by #264 (`internal/stwsync/policy_test.go`) as amended: `fetch.prune` from the configuration is honoured (never stricter than `git pull`) and `--no-prune` overrides it; the exact vectors are asserted through a recording Git wrapper.
- [x] T007 Failing tests then `--prune`: pruned refs reported by before/after comparison; without it no remote-tracking ref removed; local branches never deleted. Delivered (`TestSyncPruneFollowsFetchConfigAndTheFlags`).
- [x] T008 No-merge-commit and no-lost-commit mutant tests; dirty worktree byte-identical after a run without `--autostash`. Delivered (`TestNoMergeCommitAndNoLostCommitInAnyRepository` and the matrix tests; a merge commit now only with `--merge`).
- [x] T009 FR-012 tests: a run installs no hook and leaves no process; source check that no other command package imports the sync operation. Delivered by #264 (`internal/stwsync/process_test.go`, `internal/contracts/sync_boundary_test.go`): the source check lets only `internal/cli/sync.go` name `stwsync.Sync` and `stwsync.Restore`; the other command packages import `stwsync` for status and discovery types.

## Slice 2 — `--rebase`

- [x] T010 Failing tests then rebase of a clean checked-out diverged branch; conflict aborts with unchanged tip and worktree; with `--autostash` the stash is reapplied right after the abort (tested together with T011); non-checked-out diverged stays `diverged`. Delivered as amended: a conflict is left in progress as git leaves it (foreman decision), not aborted.

## Slice 3 — `--autostash`

- [x] T011 Failing tests then stash, operate, reapply; reapply conflict keeps the stash and reports `autostash-conflict` with the stash OID; clean reapply drops it; rebase-conflict abort followed by stash reapply. The stash is identified and applied by OID with `--index` (staged changes come back staged), and a second worktree stashing at the same time (shared stash list) never makes stw apply or drop the other worktree's entry (T020). Delivered as amended: git's own autostash, the kept entry reported and journaled as `stash_oid` (`stash_test.go`); no `--index` or OID apply.
- [x] T020 Failing tests then the stash identification of D4: the new entry is found by `refs/stash` before and after, a stash created concurrently by another worktree is not mistaken for it, the drop locates the OID in the current list, and a vanished OID is left alone. Delivered as amended: the entry is found by comparing the `refs/stash` reflog before and after; no drop by OID because stw never drops.

## Slice 4 — Restore

- [x] T012 Failing tests then `stw sync restore [PATH] [--run ID]`: `update-ref` for non-checked-out branches, `git reset --keep` for checked-out branches in a clean worktree, `changed-since` for dirty or moved-since, `already-restored` on a rerun. Delivered (`restore_test.go`).
- [x] T019 Failing tests then the incomplete-record and retention rules: an incomplete fast-forward record is restored, left, or `changed-since` by branch state; an incomplete rebase record is `journal-incomplete` and untouched; after 11 completed runs the oldest run's recovery refs and records are gone and the other runs and all non-sync refs are untouched; a run with an incomplete record is kept. Delivered (`restore_test.go`, `retention_test.go`).

## Slice 5 — Submodules

- [x] T013 Failing tests then `--submodules`: sync, fast-forward update of clean direct submodules to the recorded commit, dirty or diverged skipped, gitlinks never changed, no `--remote`, `--init` or `--recursive`; an uninitialized submodule and a missing recorded commit are `unreachable` with no clone or fetch (no network access, asserted with an unreachable remote); a HEAD ahead of the recorded commit is `diverged`; the report lists old and new OID and `restore` leaves submodules alone. Delivered as amended: results in `repos[]`, `unreachable` and `updated` (`submodules_test.go`).

## Slice 6 — Acceptance

- [x] T014 Feature status, progress, a source smoke script and acceptance evidence; open items 1 and 2 of the spec reviewed. Delivered: `scripts/smoke-sync.py` with `scripts/test_sync_smoke.py` (15 cases, wired into `.github/workflows/conformance.yml`), [verification](verification.md) with a real run on a throwaway fixture and a `--plan` of the live workspace, `docs/feature-status.json` `implemented`, `docs/progress.md`, open items reviewed in the spec.

Traceability: FR-001 T002/T006; FR-002 T006/T017; FR-003 T006/T008; FR-004 T010; FR-005 T002/T008/T011; FR-006 T006; FR-007 T005/T019; FR-008 T007; FR-009 T006; FR-010 T013; FR-011 T003; FR-012 T009; FR-013 T016/T018; FR-015 T015; FR-014 T002/T004.
