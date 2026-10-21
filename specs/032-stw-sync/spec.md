# Feature Specification: `stw sync`, one reviewed command to bring local branches up to date

**Branch**: `docs/spec-stw-sync` | **Created**: 2026-10-21
**Status**: Draft specification; no runtime implementation. Requires an independent review of the exact head before tasks start.
**Input**: Owner request: one convenient manual command that replaces the routine `git pull --rebase --prune` run in every repository (and the submodule sync/update step), with the same few options, applied to all checked-out worktrees of a repository.

## Scope and decisions

`stw sync` is a manual, explicit, Git-only command. It is not a daemon, a hook, a schedule or an agent background task, and nothing in SourceSteward starts it implicitly. Like `git pull` it is a **one-step command**: it fetches, classifies every branch from the fresh refs and applies the result. `--plan` (alias `--dry-run`) fetches (remote-tracking refs only; no local branch, worktree or stash changes) and prints what a real run would do, writing nothing else. Every local branch ref a run changes is journaled first (recovery ref plus journal record, Spec 029/031 mechanism), and `stw sync restore` undoes a run. What is journaled and what is only reported is stated in [Journal and restore](#journal-and-restore). The explicit two-step `plan`/`apply --plan ID` forms of `stw tidy` are deliberately **not** offered: a stored plan would be stale against a fetch and adds a re-classification step for no gain in a command that only fast-forwards or rebases the owner's own branches.

Decisions (owner preference: simple logic over covering every edge case; the repositories are the owner's own, so there is no hostile-repository threat model beyond the existing rule that repository content is data and ref names are never passed as options):

1. **Small option set.** `--rebase`, `--prune`, `--autostash`, `--submodules`, `--remote NAME` (default `origin`), `--json`. No `--force`, no merge strategy, no `--no-rebase`.
2. **Never a merge commit.** Without `--rebase` a branch is only fast-forwarded. A diverged branch is reported, never merged or rewritten.
3. **Dirty worktrees are skipped and reported** unless `--autostash` is given. A dirty worktree means tracked or staged changes; untracked files do not count (the Git operation itself refuses on a real collision and that branch is reported failed).
4. **Rebase is conflict-free or nothing.** With `--rebase`, local commits of a checked-out branch are rebased onto its upstream. On any conflict the rebase is aborted (`git rebase --abort`), the branch is unchanged and reported `conflict`.
5. **Branches not checked out anywhere** are updated by a ref update only when the move is a pure fast-forward. `--rebase` never applies to them (it would need a temporary worktree); a diverged one is reported `diverged`.
6. **Git only.** A jj-owned path (including a colocated one) is refused with a clear message; jj has its own fetch and is a later spec. This is a declared limit, not a silent fallback to raw Git.
7. **Network.** One `git fetch --no-tags --no-write-fetch-head --no-recurse-submodules [--prune|--no-prune] REMOTE` per run for the selected remote is the only network effect, also with `--plan`; `--no-prune` is passed explicitly so a `fetch.prune` setting never removes refs without `--prune`. A remote whose configured fetch refspecs write outside `refs/remotes/REMOTE/*` is refused before the fetch (`sync-refspec-unsafe`). `--submodules` adds no network effect (US5). Classification always uses the refs the fetch just produced.
8. **Configuration and hooks.** `stw sync` runs on the owner's own repository, so the owner's Git configuration and hooks apply as for plain Git; the strict admission of `stw tidy apply` (refusing `core.hooksPath`, `submodule.*` and similar) does **not** apply, and a repository with such settings is not refused. Hooks run during real runs and during the plan's fetch (for example `post-merge`, `post-rewrite`, `post-checkout`, `reference-transaction`); stw installs none and does not skip any. A hook that makes Git exit non-zero makes that item `failed`; a hook that moves a branch is caught by the expected-value checks (FR-002). The only configuration stw overrides is what would make Git write beyond the journaled ref, in one fixed table:

| Git call | Fixed override | Why |
| --- | --- | --- |
| every Git call that can touch a worktree (`merge`, `rebase`, `stash`, `reset`, `submodule`) | `-c submodule.recurse=false` | a configured recursion would move submodule HEADs outside the journal |
| `rebase` | `--no-autostash --no-update-refs` and `-c rebase.updateRefs=false` | `rebase.updateRefs` would move other branch refs that have no journal record; stw does its own autostash (D4) |
| `merge` | `--ff-only --no-autostash` | no merge commit; no hidden stash |
| `fetch` | `--no-tags --no-write-fetch-head --no-recurse-submodules` and `--prune` or `--no-prune` as chosen | decision 7 |

Everything else (user hooks, `core.hooksPath`, credential and URL settings) is left as the owner configured it.

### Mapping to `git pull --rebase --prune`

| `stw sync` | Git equivalent | Notes |
| --- | --- | --- |
| `--prune` | `git fetch --prune` | removes stale `refs/remotes/REMOTE/*`; local branches are never deleted (that is `stw tidy`) |
| `--rebase` | `git pull --rebase` (`git rebase UPSTREAM`) | only checked-out branches; abort on conflict |
| (none) | `git pull --ff-only` | `git merge --ff-only` for checked-out, ref update for the others |
| `--autostash` | `git pull --autostash` | stash tracked changes, run, reapply by stash OID with `--index`; a failed reapply keeps the stash and is reported |
| `--submodules` | `git submodule sync` then `git submodule update --no-fetch` (no `--init`) | see US5; no clone, no fetch |
| `--remote NAME` | the remote argument of `git fetch` | one remote per run |
| `--plan` / `--dry-run` | no direct equivalent (`git fetch` then inspect) | fetch only; no local change |

## User scenarios and acceptance

### US1 — Fast-forward every tracking branch of one repository (P1, slice 1)

The owner runs `stw sync .` (or `stw sync --plan .` first) in a repository without submodules.

1. **Given** the default branch checked out and clean, behind its upstream by two commits, **When** the sync runs, **Then** it is fast-forwarded and reported `fast-forwarded` with old and new OID.
2. **Given** a local branch with an upstream that is checked out in no worktree and is behind, **Then** its ref is updated by `git update-ref` with the expected old OID, and reported `fast-forwarded`.
3. **Given** a branch already equal to its upstream, **Then** it is reported `up-to-date` and nothing is written.
4. **Given** a branch with no upstream, **Then** it is not listed as an action (counted as `untracked`) and never touched.
5. **Given** a branch ahead of its upstream only, **Then** it is `ahead` and unchanged.
6. **Given** a branch with local commits and upstream commits and no `--rebase`, **Then** it is `diverged`, unchanged, no merge commit exists afterwards.
7. **Given** an upstream whose remote branch was deleted (`gone`), **Then** the local branch is reported `upstream-gone` and kept. This result appears only when the remote-tracking ref is absent, which a fetch causes only with `--prune` (without it the stale ref remains and the branch classifies against it).
8. **Given** a branch whose worktree is busy (rebase, merge, cherry-pick, revert or bisect in progress, or the worktree is locked), **Then** it is `skipped-busy` with the reason, nothing in that worktree or on that branch is touched, and the run goes on. A branch that a worktree's in-progress rebase or bisect is working on counts as checked out in that worktree. A worktree with a detached HEAD and no operation in progress holds no branch and is not an item.

### US2 — Prune stale remote-tracking refs (P1, slice 1)

With `--prune` the run fetches with `git fetch --prune REMOTE` and the result lists the remote-tracking refs the fetch removed (compared before and after). Without `--prune` no remote-tracking ref is removed. With `--plan --prune` the fetch also prunes: remote-tracking refs are the one thing `--plan` may change. The report lists at most 256 removed refs and the full count (`pruned_total`).

### US3 — Rebase local commits (P1, slice 2)

1. **Given** `--rebase` and a clean checked-out diverged branch whose rebase is conflict-free, **Then** it is rebased and reported `rebased` with old and new OID.
2. **Given** a conflicting rebase, **Then** the rebase is aborted and, with `--autostash`, the autostash is re-applied right after the abort, so the branch tip and worktree are as before; it is reported `conflict` and the other branches still proceed.
3. **Given** `--rebase` and a diverged branch not checked out anywhere, **Then** it is `diverged`, unchanged.
4. A rebase moves only the checked-out branch: other branch refs are never moved (`--no-update-refs`, decision 8).

### US4 — Dirty worktrees and `--autostash` (P1, slice 3)

1. **Given** a dirty worktree and no `--autostash`, **Then** its branch is `skipped-dirty`, nothing in that worktree is touched, and the run goes on.
2. **Given** `--autostash` and a dirty worktree, **Then** tracked changes are stashed, the fast-forward or rebase runs, and the stash is reapplied with `--index` so staged state returns. The stash is identified by its OID, never by its `stash@{N}` index (the stash list is shared by all worktrees). If the reapply conflicts, the stash is kept, the worktree is left as Git leaves it, and the entry is `autostash-conflict` with the stash OID.
3. A branch checked out in a dirty worktree is skipped even if a second clean worktree exists; each worktree is judged on its own, a branch is checked out in at most one.

### US5 — Submodules (P2, slice 5)

With `--submodules`, for the direct (non-recursive) submodules of the repository: run `git submodule sync` (without `--recursive`), then bring each initialized, clean submodule to the commit the superproject records, only when that is a fast-forward of its current HEAD: `git submodule update --no-fetch --checkout` (never `--init`, `--remote`, `--recursive` or `--force`). So `--submodules` **never clones and never fetches**; decision 7 holds.

- **Unreachable** means the submodule is not initialized, or its recorded commit is not present in its own object store. It is reported `unreachable` and skipped; the owner fetches or inits it with plain Git.
- Results per submodule: `updated`, `up-to-date`, `skipped-dirty` (tracked or staged changes in the submodule), `diverged` (the recorded commit is not a fast-forward of HEAD, including HEAD ahead of it), `unreachable`, `failed`. `unreachable`, `skipped-dirty`, `diverged` and `failed` make the exit status 1.
- Detached HEAD is the normal submodule state and is not an error. Sync never changes a gitlink in the superproject and never runs branch sync inside a submodule (run `stw sync` in it for that).
- A submodule HEAD move and the configuration writes of `git submodule sync` are **reported only**: they are neither journaled nor restored by `stw sync restore` (see Journal and restore).

### US6 — Undo (P2, slice 4)

`stw sync restore [PATH] [--run ID]` (default: the latest run in the journal) moves each branch the run changed back to its journaled old OID, only if the branch still points at the journaled new OID. A branch not checked out anywhere is restored by `update-ref` with the expected value; a checked-out branch only when its worktree is clean, with `git reset --keep OLD` in that worktree. A dirty worktree or a branch that moved since is reported `changed-since` and left alone. A branch already at the journaled old OID is `already-restored` (restore is idempotent). It does not undo pruned remote-tracking refs (a fetch restores them), a stash reapply or anything under `--submodules`.

## Requirements

| ID | Required behavior | Acceptance |
| --- | --- | --- |
| FR-001 | `stw sync [PATH]` fetches once, then for every local branch with an upstream under the selected remote and every worktree classifies (the closed result lists of the [output contract](#output-contract)) and applies; `--plan`/`--dry-run` prints the same classification as would-do results and changes no local ref, worktree or stash. Bounded output | US1 |
| FR-002 | Each item is acted on with the old OID it was classified with as the expected value. A non-checked-out branch: `update-ref` with the old value. A checked-out branch: the worktree's `HEAD` is read first and must equal the planned old OID, then `git merge --ff-only PLANNED_NEW_OID` (the OID, not the ref name) runs; afterwards `HEAD` must equal the planned new OID and `ORIG_HEAD` the old OID, which the completed journal record stores. Any mismatch is reported `failed` with reason `head-moved` or `raced` for that item and is never repaired by a reset | US1, T017 |
| FR-003 | Fast-forward only unless `--rebase`; no merge commit is ever created; no history rewrite without `--rebase` | US1.6 |
| FR-004 | `--rebase` rebases checked-out, clean (or autostashed) diverged branches; conflict aborts and leaves the branch unchanged | US3 |
| FR-005 | Dirty worktrees are skipped and reported unless `--autostash`; no stash, reset or checkout without it | US4 |
| FR-006 | Non-checked-out branches change only by ref update with expected old OID and only for a pure fast-forward | US1.2 |
| FR-007 | Before each branch ref change a journal record (run ID, branch, old OID, planned new OID, stash OID when autostashed) and a recovery ref in the sync namespace are written, using the Spec 029/031 journal mechanism with its own `sync` record type; the record is completed after the change. Restore of an incomplete record and retention are defined in [Journal and restore](#journal-and-restore) | US6, T005, T019 |
| FR-008 | `--prune` is the only way remote-tracking refs are removed; local branches are never deleted | US2 |
| FR-009 | A branch failure (conflict, dirty, ref race, non-zero Git exit) never stops the other items; the run ends with a complete per-item report | US3.2 |
| FR-010 | `--submodules` behaves exactly as US5, direct submodules only, no clone and no fetch | US5 |
| FR-015 | A branch checked out in a busy or locked worktree is `skipped-busy` and untouched (US1.8) | T015 |
| FR-011 | A jj-owned or unsupported path is refused before any effect; Git is never substituted for jj | scope 6 |
| FR-012 | Manual command only: no hook installation, no daemon, no schedule, no automatic invocation from other commands | test: a run installs nothing and leaves no process; a source check that no other command package imports the sync operation |
| FR-013 | Git arguments are built from fixed words and validated full ref names; branch names are never passed as options (`--` and full `refs/heads/...` forms); the fixed configuration overrides of decision 8 and no others; hooks of the repository run as Git would run them for the same operation | T016, T018 |
| FR-014 | Human output and `--json` from one result model (see below); exit codes 0/1/2 | output |

## Output contract

Human output: one line per item `BRANCH  WORKTREE  RESULT  OLD..NEW` plus a summary line (counts per result, fetch performed, refs pruned, submodules). `--json` prints exactly one final object with `schema_version`, `command: "sync"`, `mode` (`sync`, `plan` or `restore`), `run_id` (null for `plan`), `remote`, `options` (`rebase`, `prune`, `autostash`, `submodules`), `fetch` (`performed`, `pruned_refs[]` at most 256, `pruned_total`), `items[]` (`branch`, `worktree` or null, `result`, `old_oid`, `new_oid`, `reason`), `submodules[]` (`path`, `result`, `old_oid`, `new_oid`), `summary` (`total`, `by_result`, `untracked`), `error` (null, or `code` and `message` of a refused or failed run, with no items; for example a jj-owned path, an unknown remote, a failed fetch, `sync-refspec-unsafe`, more than 1024 branches) and explicit nulls instead of omitted keys. The JSON schema file is published with the implementation like the other stw schemas (task T004).

Closed result lists (`untracked` is only a count and never an item):

| Mode | Item results |
| --- | --- |
| `plan` | `fast-forward`, `rebase` (only with `--rebase`), `up-to-date`, `ahead`, `diverged`, `skipped-dirty`, `skipped-busy`, `upstream-gone` (only after `--prune` removed the remote-tracking ref) |
| `sync` | `fast-forwarded`, `rebased`, `up-to-date`, `ahead`, `diverged`, `skipped-dirty`, `skipped-busy`, `upstream-gone`, `conflict`, `autostash-conflict`, `failed` |
| `restore` | `restored`, `already-restored`, `changed-since`, `journal-incomplete`, `failed` |

Submodule results (`submodules[]`) are `updated`, `up-to-date`, `skipped-dirty`, `diverged`, `unreachable`, `failed`. In `plan` mode `submodules[]` is empty. Slice 1a (`feat/032-sync-plan`) ships the `plan` list without `rebase` and `skipped-busy` and the `error` object; those two results are added by T015 and T010.

Exit codes: 0 when no item needed the owner's attention (`fast-forwarded`, `rebased`, `up-to-date`, `ahead`, `untracked`; `upstream-gone` is only a note, so a routine `--prune` run exits 0); 1 when the run failed or any item ended `skipped-dirty`, `conflict`, `diverged` (not rebased), `skipped-busy`, `autostash-conflict`, `changed-since`, `journal-incomplete`, `unreachable` (submodule) or `failed` (the report is still complete); 2 for usage errors before any effect (including, until the real-run slice, a missing `--plan`).

## Journal and restore

| Change | Journaled and restorable by `stw sync restore` |
| --- | --- |
| Local branch ref move (fast-forward, rebase, `update-ref`) | Yes: journal record plus recovery ref, restored with the expected-value rules of US6 |
| Autostash entry | Journaled (stash OID) and kept on `autostash-conflict`; the reapply and drop are not undone |
| Pruned remote-tracking refs | No: reported; the next fetch restores them |
| Submodule HEAD move, `git submodule sync` configuration writes | No: reported with old and new OID only. The old commit stays in the submodule's object store; the owner returns with `git -C SUBMODULE checkout OLD` |
| Worktree file content | No, except what `git reset --keep` restores for a branch move |

- **Recovery refs** live under `refs/stw/recovery/sync/RUN_ID/N` (one per journaled branch move; the namespace of Spec 029/031 recovery refs, `sync` sub-namespace).
- **Incomplete record** (the process died between the record and its completion): a fast-forward record has a planned new OID, so restore compares the branch: at the planned new OID it is restored to the old OID, at the old OID it is `already-restored`, anywhere else `changed-since`. A rebase record has no planned new OID, so it is `journal-incomplete` and left alone; the recovery ref names the old tip for a manual reset. Nothing is ever replayed forward.
- **Retention**: a real run, as its last step, removes the recovery refs and records of runs older than the 10 most recent completed runs of that repository. Runs with an incomplete record are kept until a restore has dealt with them. No other ref is ever removed.

## Comparison with `git pull --rebase --prune`

- `git pull` works on the one checked-out branch of one worktree; `stw sync` covers every worktree and every tracking branch, including branches checked out nowhere.
- `git pull` can create a merge commit depending on configuration; `stw sync` cannot.
- `git pull` stops at the first conflict with a rebase in progress; `stw sync` aborts that branch, reports it and continues.
- `git pull` has no dry run, journal or undo; `stw sync` has `--plan`, a journal and `restore`.
- `git pull` has many more options (strategies, `--force`, verbosity, `--no-ff`, tags); `stw sync` deliberately has five. Anything else stays raw Git.
- Both run the repository's own Git hooks for the underlying operations and both depend on the user's Git configuration for remotes and upstreams; `stw sync` adds no network access beyond the one `git fetch`.

## Open items

1. **`stw workspace plan` fails on this repository.** `stw workspace plan` on the SourceSteward repository itself fails with `git-checkout-limit: base checkout exceeds 1024 leaf entries`, so the stw worktree helper cannot create isolated worktrees here. To be addressed separately (a limit or a sparse/partial-base decision for workspace plans); it is not part of this spec. Authors of this spec's slices use a throwaway `git clone --no-local` meanwhile.
2. **Consequence for dogfooding.** Until item 1 is solved, `stw sync` itself cannot be developed through its own workspace flow on this repository, and a repository above the same inventory limits will make `stw sync` report a bounded partial result; slice 1 must use the existing bounded readers (Spec 027) rather than a new unbounded one.
3. Whether `stw sync` should later offer an all-repositories form like `stw repos PATH...` (up to 16 paths) is deferred until slice 1 is used.
4. jj support (`jj git fetch` and bookmark moves) is a later spec.
5. Owner decisions taken in this spec that the owner may change: `--submodules` never clones or fetches (an uninitialized or incompletely fetched submodule is `unreachable`); submodule moves are reported but not restorable; ten runs of recovery refs are retained.

## Success criteria

SC-001: every result class has a real temporary-repository test (no real user repository) with a bare remote and several worktrees. SC-002: no test or mutant can produce a merge commit or lose a local commit. SC-003: dirty-worktree content is byte-identical after a run without `--autostash`. SC-004: sync then restore returns every moved branch ref to its old OID.
