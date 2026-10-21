# Implementation Plan: `stw sync`

**Status**: Draft design, no runtime. **Spec**: [spec.md](spec.md)

## Approach

A new command `stw sync [PATH]` (one step, with `--plan`) and `stw sync restore`, beside `stw tidy`, reusing, not copying, the Spec 029/031 pieces: recovery refs and journal mechanism, bounded native Git reader (Spec 027 streaming), worktree inventory (Spec 009) and the JSON result conventions. Standard library first; Git is invoked through the existing native runner with fixed argument vectors and a neutral configuration policy where the existing tidy code already has one (the repository's own hooks run for fetch, merge and rebase, as with plain Git; the strict configuration admission of tidy does not apply, see spec decision 8 for the fixed overrides).

## Decisions

D1. One-step command like `git pull`: fetch, classify from fresh refs, apply. `--plan` fetches (remote-tracking refs only) and stops before any local change. Rejected: stored two-step plan/apply, because it needs re-classification after the fetch and buys nothing here.
D2. Per item classification uses `git rev-list --left-right --count BRANCH...UPSTREAM` and `merge-base --is-ancestor`. Busy worktrees (`rebase-merge`, `rebase-apply`, `MERGE_HEAD`, `CHERRY_PICK_HEAD`, `REVERT_HEAD`, `BISECT_LOG` in the worktree's git dir, or a lock from `git worktree list --porcelain`) make their branch `skipped-busy`; a branch named by `rebase-merge/head-name` or `BISECT_START` counts as checked out there. A checked-out branch is acted on inside its own worktree: read `HEAD` and require the planned old OID, then `git -C WORKTREE -c submodule.recurse=false merge --ff-only --no-autostash NEW_OID` or `rebase --no-autostash --no-update-refs` (with `-c rebase.updateRefs=false`), then check `HEAD` and `ORIG_HEAD`. A non-checked-out branch is acted on by `update-ref REF NEW OLD`.
D3. Rebase conflict handling is `git rebase --abort`, then (with autostash) reapplying the stash, and a verification that HEAD equals the old OID; if the abort itself fails the item is `failed` and the worktree is reported for manual action (no reset).
D4. Autostash uses `git stash push` of tracked changes. The stash ref is shared by all worktrees, so the new entry is identified by reading `refs/stash` before and after and requiring a new commit whose first parent is this worktree's `HEAD`; its OID is journaled. Reapply is `git stash apply --index STASH_OID`; on success the entry is dropped by locating the OID in `git stash list --format=%H` immediately before `git stash drop stash@{N}`, and an OID that is no longer found is left alone. A failed reapply never drops the stash (stricter than `git pull --autostash`, a documented difference).
D5. Journal: one record per branch ref change, written and fsynced before the change, completed after, using the Spec 029/031 journal mechanism with its own `sync` record type. That mechanism is keyed by plan ID and its restore only recreates absent refs, so sync adds three things on top: a run ID in place of the plan ID, recovery refs under `refs/stw/recovery/sync/RUN_ID/N`, and its own restore that applies the expected-value update (`update-ref`, or `reset --keep` in a clean worktree) and handles incomplete records and retention as in the spec section "Journal and restore" (10 runs kept, pruned by a real run as its last step). Reuse of the 029/031 record encoding and fsync code is the aim; the keying, namespace and restore rules are new code.
D6. Submodules run `git submodule sync` and `git submodule update --no-fetch --checkout` (never `--init`), preceded by a per-submodule check: initialized, clean, recorded commit present in its object store (else `unreachable`) and a fast-forward of HEAD (else `diverged`). The HEAD move is reported with old and new OID and not journaled.
D7. No new dependency.

## Constitution check

Git Tier 1 only, jj refused explicitly. Plan then apply with stale-state checks. No raw Git substitution for jj. Repository content treated as data. Manual command only. Hooks of the repository run as with Git itself (spec decision 8).

## Slices (PR-sized)

1a. Classification model, `stw sync --plan` output (human and JSON), JSON schema, jj refusal and usage errors; local-only except the one fetch. Command table entry in `docs/commands.md`. Implemented in PR #120; the `skipped-busy` classification (T015) is a small follow-up in 1b, not a rewrite of 1a.
1b. Real run for one repository without submodules: fast-forward of all worktrees and non-checked-out branches, `--prune`, journal; docs updated for the shipped behaviour.
2. `--rebase`.
3. `--autostash`.
4. `restore`.
5. `--submodules`.
6. Acceptance evidence, feature status and smoke script.

Each slice is a separate PR, stacked linearly, with failing tests first and its own docs; slice 1a is the first runtime PR after the design review of this spec.
