# Tasks: `stw sync`

**Gate**: all runtime boxes stay unchecked until the design review of the exact head of this spec is CLEAR. Tests use temporary repositories with a bare remote and linked worktrees; never a real user repository. Failing test first for each task. Each slice updates `docs/commands.md` and related docs for what it ships.

## Slice 0 — Design

- [ ] T001 Independent review of the exact committed head of this spec (strongest tier, executed checks).

## Slice 1a — Classification and `--plan`

- [ ] T002 Failing tests for the classification table (`fast-forward`, `up-to-date`, `ahead`, `diverged`, `upstream-gone`, untracked branch ignored, dirty worktree `skipped-dirty`) over a temporary repo with bare remote and linked worktrees; then `stw sync --plan [PATH] [--remote NAME] [--prune]` with the single fetch, bounded output, human and `--json`; no local ref, worktree or stash change.
- [ ] T003 Failing tests then jj and unsupported path refusal before any effect; usage errors exit 2; exit-code table (`upstream-gone` exits 0).
- [ ] T004 JSON schema for the sync result with valid and invalid fixtures; docs entry in `docs/commands.md` and `docs/json-contracts.md`.

## Slice 1b — Real run: fast-forward, prune, journal

- [ ] T005 Failing tests then the journal record (`sync` record type on the Spec 029/031 mechanism) written before each ref change and completed after, with recovery ref.
- [ ] T006 Failing tests then `stw sync [PATH]`: fast-forward of a checked-out clean branch in each linked worktree, `update-ref` with expected old OID for non-checked-out branches, per-item failure isolation, lost race reported `failed`.
- [ ] T007 Failing tests then `--prune`: pruned refs reported by before/after comparison; without it no remote-tracking ref removed; local branches never deleted.
- [ ] T008 No-merge-commit and no-lost-commit mutant tests; dirty worktree byte-identical after a run without `--autostash`.
- [ ] T009 FR-012 tests: a run installs no hook and leaves no process; source check that no other command package imports the sync operation.

## Slice 2 — `--rebase`

- [ ] T010 Failing tests then rebase of a clean checked-out diverged branch; conflict aborts with unchanged tip and worktree; with `--autostash` the stash is reapplied right after the abort (tested together with T011); non-checked-out diverged stays `diverged`.

## Slice 3 — `--autostash`

- [ ] T011 Failing tests then stash, operate, reapply; reapply conflict keeps the stash and reports `autostash-conflict` with the stash OID; clean reapply drops it; rebase-conflict abort followed by stash reapply.

## Slice 4 — Restore

- [ ] T012 Failing tests then `stw sync restore [PATH] [--run ID]`: `update-ref` for non-checked-out branches, `git reset --keep` for checked-out branches in a clean worktree, `changed-since` for dirty or moved-since, idempotent rerun.

## Slice 5 — Submodules

- [ ] T013 Failing tests then `--submodules`: sync, fast-forward update of clean direct submodules to the recorded commit, dirty or diverged skipped, gitlinks never changed, no `--remote`.

## Slice 6 — Acceptance

- [ ] T014 Feature status, progress, a source smoke script and real acceptance evidence; open items 1 and 2 of the spec reviewed.

Traceability: FR-001 T002/T006; FR-002 T006; FR-003 T006/T008; FR-004 T010; FR-005 T002/T008/T011; FR-006 T006; FR-007 T005; FR-008 T007; FR-009 T006; FR-010 T013; FR-011 T003; FR-012 T009; FR-013 T006; FR-014 T002/T004.
