# Feature Specification: `stw sync`, one reviewed command to bring local branches up to date

**Branch**: `docs/spec-stw-sync` | **Created**: 2026-10-21
**Status**: Draft specification; no runtime implementation. Requires an independent review of the exact head before tasks start.
**Input**: Owner request: one convenient manual command that replaces the routine `git pull --rebase --prune` run in every repository (and the submodule sync/update step), with the same few options, applied to all checked-out worktrees of a repository.

## Scope and decisions

`stw sync` is a manual, explicit, Git-only command. It is not a daemon, a hook, a schedule or an agent background task, and nothing in SourceSteward starts it implicitly. It follows the stw model: `plan` shows exactly what will happen, `apply` does it, every ref it changes is journaled first, and a restore command undoes a sync (Spec 029 and 031 conventions: stored private plan, recovery refs, stale-state checks, bounded output).

Decisions (owner preference: simple logic over covering every edge case; the repositories are the owner's own, so there is no hostile-repository threat model beyond the existing rule that repository content is data and ref names are never passed as options):

1. **Small option set.** `--rebase`, `--prune`, `--autostash`, `--submodules`, `--remote NAME` (default `origin`), `--json`. No `--force`, no merge strategy, no `--no-rebase`.
2. **Never a merge commit.** Without `--rebase` a branch is only fast-forwarded. A diverged branch is reported, never merged or rewritten.
3. **Dirty worktrees are skipped and reported** unless `--autostash` is given. A dirty worktree means tracked or staged changes; untracked files do not count (the Git operation itself refuses on a real collision and that branch is reported failed).
4. **Rebase is conflict-free or nothing.** With `--rebase`, local commits of a checked-out branch are rebased onto its upstream. On any conflict the rebase is aborted (`git rebase --abort`), the branch is unchanged and reported `conflict`.
5. **Branches not checked out anywhere** are updated by a ref update only when the move is a pure fast-forward. `--rebase` never applies to them (it would need a temporary worktree); a diverged one is reported `diverged`.
6. **Git only.** A jj-owned path (including a colocated one) is refused with a clear message; jj has its own fetch and is a later spec. This is a declared limit, not a silent fallback to raw Git.
7. **Network.** `apply` runs one `git fetch` per sync (and per remote) and is the only network effect. `plan` reads local state only and never fetches; it classifies from the remote-tracking refs it finds and says so. `apply` recomputes every classification after the fetch and acts on the fresh state.

### Mapping to `git pull --rebase --prune`

| `stw sync` | Git equivalent | Notes |
| --- | --- | --- |
| `--prune` | `git fetch --prune` | removes stale `refs/remotes/REMOTE/*`; local branches are never deleted (that is `stw tidy`) |
| `--rebase` | `git pull --rebase` (`git rebase UPSTREAM`) | only checked-out branches; abort on conflict |
| (none) | `git pull --ff-only` | `git merge --ff-only` for checked-out, ref update for the others |
| `--autostash` | `git pull --autostash` | stash tracked changes, run, reapply; a failed reapply keeps the stash and is reported |
| `--submodules` | `git submodule sync` then `git submodule update --init` | see US5 |
| `--remote NAME` | the remote argument of `git fetch` | one remote per run |

## User scenarios and acceptance

### US1 — Fast-forward every tracking branch of one repository (P1, slice 1)

The owner runs `stw sync plan .` then `stw sync apply --plan ID` in a repository without submodules.

1. **Given** the default branch checked out and clean, behind its upstream by two commits, **When** apply runs, **Then** it is fast-forwarded and reported `fast-forwarded` with old and new OID.
2. **Given** a local branch with an upstream that is checked out in no worktree and is behind, **Then** its ref is updated by `git update-ref` with the expected old OID, and reported `fast-forwarded`.
3. **Given** a branch already equal to its upstream, **Then** it is reported `up-to-date` and nothing is written.
4. **Given** a branch with no upstream, **Then** it is not listed as an action (counted as `untracked`) and never touched.
5. **Given** a branch ahead of its upstream only, **Then** it is `ahead` and unchanged.
6. **Given** a branch with local commits and upstream commits and no `--rebase`, **Then** it is `diverged`, unchanged, no merge commit exists afterwards.
7. **Given** an upstream whose remote branch was deleted (`gone`), **Then** the local branch is reported `upstream-gone` and kept.

### US2 — Prune stale remote-tracking refs (P1, slice 1)

With `--prune`, apply runs `git fetch --prune REMOTE`. The plan lists the remote-tracking refs it would be reasonable to expect pruned only as "decided by the fetch"; the apply result lists the refs the fetch removed (compared before and after). Without `--prune` no remote-tracking ref is removed.

### US3 — Rebase local commits (P1, slice 2)

1. **Given** `--rebase` and a clean checked-out diverged branch whose rebase is conflict-free, **Then** it is rebased and reported `rebased` with old and new OID.
2. **Given** a conflicting rebase, **Then** the rebase is aborted, the branch tip and worktree are as before, reported `conflict`, and the other branches still proceed.
3. **Given** `--rebase` and a diverged branch not checked out anywhere, **Then** it is `diverged`, unchanged.

### US4 — Dirty worktrees and `--autostash` (P1, slice 3)

1. **Given** a dirty worktree and no `--autostash`, **Then** its branch is `skipped-dirty`, nothing in that worktree is touched, and the run goes on.
2. **Given** `--autostash` and a dirty worktree, **Then** tracked changes are stashed, the fast-forward or rebase runs, and the stash is reapplied. If the reapply conflicts, the stash is kept, the worktree is left as Git leaves it, and the entry is `autostash-conflict` with the stash OID.
3. A branch checked out in a dirty worktree is skipped even if a second clean worktree exists; each worktree is judged on its own, a branch is checked out in at most one.

### US5 — Submodules (P2, slice 5)

With `--submodules`, for the direct (non-recursive) submodules of the repository: run `git submodule sync`, then bring each initialized, clean submodule to the commit the superproject records, only when that is a fast-forward of its current HEAD (`git submodule update --init`, never `--remote`, never `--force`). A dirty, diverged or uninitialized-and-unreachable submodule is reported and skipped. Sync never changes a gitlink in the superproject and never runs branch sync inside a submodule (run `stw sync` in it for that). Detached HEAD is the normal submodule state and is not an error.

### US6 — Undo (P2, slice 4)

`stw sync restore --plan ID` resets each branch ref that the sync moved back to its journaled old OID, but only if the branch still points at the journaled new OID (otherwise `changed-since`, untouched). It does not undo pruned remote-tracking refs (a fetch restores them) and does not undo a stash reapply.

## Requirements

| ID | Required behavior | Acceptance |
| --- | --- | --- |
| FR-001 | `stw sync plan [PATH]` lists, for every local branch with an upstream under the selected remote and every worktree, the action it would take: `fast-forward`, `rebase`, `up-to-date`, `ahead`, `diverged`, `skipped-dirty`, `upstream-gone`. Local reads only, bounded output, stored as a private immutable plan like `stw tidy plan` | US1 |
| FR-002 | `stw sync apply --plan ID` fetches once (`--prune` if selected), recomputes each item from fresh state and acts only when the recomputed class equals a plan class it may perform; otherwise reports `changed` and does nothing for that item | US1 |
| FR-003 | Fast-forward only unless `--rebase`; no merge commit is ever created; no history rewrite without `--rebase` | US1.6 |
| FR-004 | `--rebase` rebases checked-out, clean (or autostashed) diverged branches; conflict aborts and leaves the branch unchanged | US3 |
| FR-005 | Dirty worktrees are skipped and reported unless `--autostash`; no stash, reset or checkout without it | US4 |
| FR-006 | Non-checked-out branches change only by ref update with expected old OID and only for a pure fast-forward | US1.2 |
| FR-007 | Before each ref change a journal entry (branch, old OID, planned new OID) and a recovery ref are written, following the Spec 029/031 journal format; the entry is completed after the change | US6 |
| FR-008 | `--prune` is the only way remote-tracking refs are removed; local branches are never deleted | US2 |
| FR-009 | A branch failure (conflict, dirty, ref race, non-zero Git exit) never stops the other items; the run ends with a complete per-item report | US3.2 |
| FR-010 | `--submodules` behaves exactly as US5, direct submodules only | US5 |
| FR-011 | A jj-owned or unsupported path is refused before any effect; Git is never substituted for jj | scope 6 |
| FR-012 | Manual command only: no hook installation, no daemon, no schedule, no automatic invocation from other commands | scope |
| FR-013 | Git arguments are built from fixed words and validated full ref names; branch names are never passed as options (`--` and full `refs/heads/...` forms); hooks of the repository run as Git would run them for the same operation | tests |
| FR-014 | Human output and `--json` from one result model (see below); exit codes 0/1/2 | output |

## Output contract

Human output: one line per item `BRANCH  WORKTREE  RESULT  OLD..NEW` plus a summary line (counts per result, fetch performed, refs pruned, submodules). `--json` prints exactly one final object with `schema_version`, `command: "sync"`, `mode` (`plan`, `apply` or `restore`), `plan_id`, `remote`, `options` (`rebase`, `prune`, `autostash`, `submodules`), `fetch` (`performed`, `pruned_refs[]`), `items[]` (`branch`, `worktree` or null, `result`, `old_oid`, `new_oid`, `reason`), `submodules[]` (`path`, `result`, `old_oid`, `new_oid`), `summary` (counts) and explicit nulls instead of omitted keys. The JSON schema file is published with the implementation like the other stw schemas (task T009).

Exit codes: 0 when every selected item ended `fast-forwarded`, `rebased`, `up-to-date`, `ahead` or `untracked` (nothing needed attention); 1 when the run failed or any item ended `diverged`, `conflict`, `skipped-dirty`, `autostash-conflict`, `upstream-gone`, `changed` or `failed` (the report is still complete); 2 for usage errors before any effect.

## Comparison with `git pull --rebase --prune`

- `git pull` works on the one checked-out branch of one worktree; `stw sync` covers every worktree and every tracking branch, including branches checked out nowhere.
- `git pull` can create a merge commit depending on configuration; `stw sync` cannot.
- `git pull` stops at the first conflict with a rebase in progress; `stw sync` aborts that branch, reports it and continues.
- `git pull` makes no plan, no journal and no undo; `stw sync` has all three.
- `git pull` has many more options (strategies, `--force`, verbosity, `--no-ff`, tags); `stw sync` deliberately has five. Anything else stays raw Git.
- Both run the repository's own Git hooks for the underlying operations and both depend on the user's Git configuration for remotes and upstreams; `stw sync` adds no network access beyond the one `git fetch`.

## Open items

1. **`stw workspace plan` fails on this repository.** `stw workspace plan` on the SourceSteward repository itself fails with `git-checkout-limit: base checkout exceeds 1024 leaf entries`, so the stw worktree helper cannot create isolated worktrees here. To be addressed separately (a limit or a sparse/partial-base decision for workspace plans); it is not part of this spec. Authors of this spec's slices use a throwaway `git clone --no-local` meanwhile.
2. **Consequence for dogfooding.** Until item 1 is solved, `stw sync` itself cannot be developed through its own workspace flow on this repository, and a repository above the same inventory limits will make `stw sync plan` report a bounded partial result; slice 1 must use the existing bounded readers (Spec 027) rather than a new unbounded one.
3. Whether `stw sync` should later offer an all-repositories form like `stw repos PATH...` (up to 16 paths) is deferred until slice 1 is used.
4. jj support (`jj git fetch` and bookmark moves) is a later spec.

## Success criteria

SC-001: every result class has a real temporary-repository test (no real user repository) with a bare remote and several worktrees. SC-002: no test or mutant can produce a merge commit or lose a local commit. SC-003: dirty-worktree content is byte-identical after a run without `--autostash`. SC-004: plan then restore returns every moved branch ref to its old OID.
