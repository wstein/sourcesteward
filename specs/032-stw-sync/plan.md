# Implementation Plan: `stw sync`

**Status**: Draft design, no runtime. **Spec**: [spec.md](spec.md)

## Approach

A new command `stw sync [PATH]` (one step, with `--plan`) and `stw sync restore`, beside `stw tidy`, reusing, not copying, the Spec 029/031 pieces: recovery refs and journal mechanism, bounded native Git reader (Spec 027 streaming), worktree inventory (Spec 009) and the JSON result conventions. Standard library first; Git is invoked through the existing native runner with fixed argument vectors and a neutral configuration policy where the existing tidy code already has one (the repository's own hooks are allowed to run for fetch, merge and rebase, as with plain Git).

## Decisions

D1. One-step command like `git pull`: fetch, classify from fresh refs, apply. `--plan` fetches (remote-tracking refs only) and stops before any local change. Rejected: stored two-step plan/apply, because it needs re-classification after the fetch and buys nothing here.
D2. Per item classification uses `git rev-list --left-right --count BRANCH...UPSTREAM` and `merge-base --is-ancestor`; a checked-out branch is acted on inside its own worktree (`git -C WORKTREE merge --ff-only`, `rebase`), a non-checked-out branch by `update-ref REF NEW OLD`.
D3. Rebase conflict handling is `git rebase --abort`, then (with autostash) reapplying the stash, and a verification that HEAD equals the old OID; if the abort itself fails the item is `failed` and the worktree is reported for manual action (no reset).
D4. Autostash uses `git stash push` of tracked changes and `git stash apply` followed by drop only on success, so a failed reapply never loses the stash (stricter than `git pull --autostash`, which is a documented difference).
D5. Journal: one record per ref change, written and fsynced before the change, completed after, using the Spec 029/031 journal mechanism with its own `sync` record type. The recovery ref keeps the old tip reachable. Restore applies the expected-value update (`update-ref`, or `reset --keep` in a clean worktree).
D6. Submodules reuse `git submodule sync` and `git submodule update --init` verbatim, preceded by a clean/fast-forward check per submodule.
D7. No new dependency.

## Constitution check

Git Tier 1 only, jj refused explicitly. Plan then apply with stale-state checks. No raw Git substitution for jj. Repository content treated as data. Manual command only. Hooks of the repository run as with Git itself; the plan states this.

## Slices (PR-sized)

1a. Classification model, `stw sync --plan` output (human and JSON), JSON schema, jj refusal and usage errors; local-only except the one fetch. Command table entry in `docs/commands.md`.
1b. Real run for one repository without submodules: fast-forward of all worktrees and non-checked-out branches, `--prune`, journal; docs updated for the shipped behaviour.
2. `--rebase`.
3. `--autostash`.
4. `restore`.
5. `--submodules`.
6. Acceptance evidence, feature status and smoke script.

Each slice is a separate PR, stacked linearly, with failing tests first and its own docs; slice 1a is the first runtime PR after the design review of this spec.
