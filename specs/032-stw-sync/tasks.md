# Tasks: `stw sync`

**Gate**: all runtime boxes stay unchecked until the design review of the exact head of this spec is CLEAR. Tests use temporary repositories with a bare remote and linked worktrees; never a real user repository. Failing test first for each task.

## Slice 0 — Design

- [ ] T001 Independent review of the exact committed head of this spec (strongest tier, executed checks).

## Slice 1 — One repository, no submodules, fast-forward and prune

- [ ] T002 Failing tests for the classification table (`fast-forward`, `up-to-date`, `ahead`, `diverged`, `upstream-gone`, untracked branch ignored) over a temporary repo with bare remote; then the local-only `stw sync plan [PATH] [--remote NAME]` with stored private plan, bounded output, human and `--json`.
- [ ] T003 Decide and implement the journal entry for a ref change by reusing the Spec 031 journal and recovery ref; failing tests for write-before-change and completion.
- [ ] T004 Failing tests then `stw sync apply --plan ID`: one fetch, fresh recompute, fast-forward of a checked-out clean branch (in each linked worktree), `update-ref` with expected old OID for non-checked-out branches, `changed` on stale state, per-item failure isolation.
- [ ] T005 Failing tests then `--prune`: `fetch --prune`, pruned refs reported by before/after comparison; without it no remote-tracking ref removed; local branches never deleted.
- [ ] T006 Failing tests then dirty worktree handling without `--autostash`: `skipped-dirty`, byte-identical worktree, other items proceed.
- [ ] T007 Failing tests then jj and unsupported path refusal before any effect; usage errors exit 2; exit-code table.
- [ ] T008 No-merge-commit and no-lost-commit mutant tests (a fast-forward implementation replaced by merge or reset must fail).
- [ ] T009 JSON schema `schemas/v2` (or the next schema family version in use) for the sync result, valid and invalid fixtures.

## Slice 2 — `--rebase`

- [ ] T010 Failing tests then rebase of a clean checked-out diverged branch; conflict aborts with unchanged tip and worktree; non-checked-out diverged stays `diverged`.

## Slice 3 — `--autostash`

- [ ] T011 Failing tests then stash, operate, reapply; reapply conflict keeps the stash and reports `autostash-conflict` with the stash OID; clean reapply drops it.

## Slice 4 — Restore

- [ ] T012 Failing tests then `stw sync restore --plan ID`: reset to old OID only when the tip is still the journaled new OID, `changed-since` otherwise, idempotent rerun.

## Slice 5 — Submodules

- [ ] T013 Failing tests then `--submodules`: sync, fast-forward update of clean direct submodules to the recorded commit, dirty or diverged skipped, gitlinks never changed, no `--remote`.

## Slice 6 — Documentation and acceptance

- [ ] T014 Update `docs/commands.md`, `docs/json-contracts.md`, `docs/feature-status.json`, `docs/progress.md`, the README of specs if any, and add a source smoke script; record real acceptance evidence; open items 1 and 2 of the spec reviewed.

Traceability: FR-001 T002; FR-002 T004; FR-003 T004/T008; FR-004 T010; FR-005 T006/T011; FR-006 T004; FR-007 T003; FR-008 T005; FR-009 T004; FR-010 T013; FR-011 T007; FR-012 T007 (no hook or schedule code path); FR-013 T004; FR-014 T002/T009.
