# Implementation Plan: `stw sync`

**Status**: Draft design, no runtime. **Spec**: [spec.md](spec.md)

## Approach

A new command group `stw sync plan|apply|restore` beside `stw tidy`, reusing, not copying, the Spec 029/031 pieces: private plan store and plan IDs, recovery refs and journal, bounded native Git reader (Spec 027 streaming), worktree inventory (Spec 009) and the JSON result conventions. Standard library first; Git is invoked through the existing native runner with fixed argument vectors and a neutral configuration policy where the existing tidy code already has one (the repository's own hooks are allowed to run for fetch, merge and rebase, as with plain Git).

## Decisions

D1. Plan is local-only; apply fetches and recomputes. Rejected: fetching in plan, because it writes refs outside a plan and makes the plan unstable.
D2. Per item classification uses `git rev-list --left-right --count BRANCH...UPSTREAM` and `merge-base --is-ancestor`; a checked-out branch is acted on inside its own worktree (`git -C WORKTREE merge --ff-only`, `rebase`), a non-checked-out branch by `update-ref REF NEW OLD`.
D3. Rebase conflict handling is `git rebase --abort` and a verification that HEAD equals the old OID; if the abort itself fails the item is `failed` and the worktree is reported for manual action (no reset).
D4. Autostash uses `git stash push` of tracked changes and `git stash apply` followed by drop only on success, so a failed reapply never loses the stash (stricter than `git pull --autostash`, which is a documented difference).
D5. Journal: one entry per ref change, written and fsynced before the change, completed after. The recovery ref keeps the old tip reachable. Restore reads the journal and applies the expected-old-OID update. Format identical to Spec 031's journal; no new journal kind unless the existing one cannot hold a `sync` entry (decide in T003).
D6. Submodules reuse `git submodule sync` and `git submodule update --init` verbatim, preceded by a clean/fast-forward check per submodule.
D7. No new dependency.

## Constitution check

Git Tier 1 only, jj refused explicitly. Plan then apply with stale-state checks. No raw Git substitution for jj. Repository content treated as data. Manual command only. Hooks of the repository run as with Git itself; the plan states this.

## Slices (PR-sized)

1. One repository, no submodules, fast-forward only, `--prune`, journal and plan/apply, human and JSON output. Includes all worktrees and non-checked-out branches.
2. `--rebase`.
3. `--autostash`.
4. `restore`.
5. `--submodules`.
6. Docs, command table, feature status, acceptance evidence and the smoke script.

Each slice is a separate PR, stacked linearly, with failing tests first; slice 1 is the first runtime PR after the design review of this spec.
