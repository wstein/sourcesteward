# stw (SourceSteward)

Implementation scope, feature decisions, dependencies, and acceptance gates: [Rust migration plan](docs/migration-plan.md).

Everyday git and GitHub workflows for agents and people, in one binary. Written in Rust on
[gitoxide](https://github.com/GitoxideLabs/gitoxide) (`gix`, pinned to `=0.90.0`) and `clap`.
stw starts the installed `git` in one place only, the runner (`src/git_runner.rs`; `tests/it/no_binaries.rs` allows a process
nowhere else): today for the fetch of `sync` with ssh and local-path remotes and for `sync restore` (`git reset --keep`,
`git update-ref`), and for workspace checkpoint, retirement preview, worktree entry, and resume. The recovery refs and the journal of a `sync` run are written through gix, so a run needs no `git` for them
(and the repository's own config cannot block them). `status`, `tidy` and `sync` start no
program for any repository content: gix reads only, filter drivers (`filter.<name>.clean/smudge/process`) are dropped
from the in-memory configuration (a file with a `filter=` attribute is then compared unconverted, so a content-rewriting
driver such as git-lfs can make it look modified, and `sync` refuses, before any write, a fast-forward that
would write such a path), https fetch uses no credential helper, and an empty `PATH` still serves
every gix read. There is no `gh` and no `ssh` of stw's own. Later `push`,
`range-diff` and conflict resolution in a worktree use the same runner. Each step that ran through it carries `"via": "git"`
in the JSON output; without a usable `git` (missing, older than the step needs, unknown option) the step reports
`unsupported` (exit 1, typed). See `chartroom/zettel/stw runs git only for push, fetch over ssh, range-diff and resolve.md`.

jj (Jujutsu) is not supported (owner decision 2026-11-11). Git is the standard; functions that jj offers (undo/oplog,
conflict resolution) are added to stw instead of maintaining a second repository model. jj is an officially unsupported
tool: it may work in a colocated Git repository, without a test and without a promise. stw has no jj special code (no
detection, no warning, no test).

```
stw status    [PATH].. [--long|-l] [--fetch] [--worktrees] [--review] [--icons unicode|nerd|ascii] [--timeout DURATION] [--stats] [--verbose]
              [--depth N] [--limit N] [--json] [--offline] [--root PATH] [--repo NAME]... [--rules-path PATH]
stw tidy plan  [--json] [--offline] [--root PATH] [--repo NAME]... [--all] [--backup-days N] [--similarity PCT] [--similarity-low PCT] [--no-similarity] [--no-rebase-evidence] [--explain]
stw tidy apply   [--plan FILE] [--manifest-dir DIR] [--backup-days N] [--json] [--offline] [--root PATH] [--repo NAME]...
stw tidy restore [--run ID] [--branch NAME] [--manifest-dir DIR] [--json]
stw tidy history [--limit N] [--manifest-dir DIR] [--json]
stw sync      [--plan | --dry-run] [--prune | --no-prune] [--submodules] [--json] [--offline] [--root PATH] [--repo NAME]...
              [--rules] [--rules-path PATH]
stw sync restore [PATH] [--run ID] [--json] [--root PATH]
stw add       OWNER/REPO [PATH] [--dry-run] [--json] [--root PATH]
stw workspace checkpoint --task NAME [--include PATH]... [--plan] [--repo DIR] [--json]
stw workspace inspect    --task NAME [--repo DIR] [--json]
stw workspace retirement-plan --task NAME [--repo DIR] [--json]
stw workspace worktree-plan REV DESTINATION [--repo DIR] [--json]
stw workspace worktree-apply PLAN [--repo DIR] [--json]
stw workspace resume     CHECKPOINT DESTINATION [--repo DIR] [--json]
stw review carry-over [--pr N|none] [--fetch] [--checkout PATH] [--json] OLD_BASE OLD_HEAD NEW_BASE NEW_HEAD
stw prove     ALT NEU [--base BASIS] [--old-parent REV] [--pr N|none] [--json] [--root PATH] [--repo NAME]
stw restack plan [BRANCH...] [--onto BASIS] [--old-parent REV] [--json] [--root PATH] [--repo NAME]
stw absorb    [--plan] [--apply --plan-sum SUM] [--base REV] [--max-stack N] [--ambiguous-newest] [--human | --json] [--root PATH] [--repo NAME]
stw ready-check NUMBER [--repo OWNER/NAME] [--human | --json]
stw lint pr   PR [PATH | --repo OWNER/NAME] [--owner-identity "Name <email>"]... [--human | --json]
stw lint review FILE [--base REV --head REV] [--pr N|none] [--checkout PATH] [--human | --json]
stw lint pr-text FILE [--ready] [--human | --json]
```

| Command | What it does | Writes |
| --- | --- | --- |
| `status` | counts per repository and in total, then per repository: branch, upstream ahead/behind, staged/unstaged/untracked/conflicts, stash, running operation, submodule pin drift, open pull requests, the CI of the default branch (`ci-main`, see "CI of the default branch in `stw status`") | nothing; with `--fetch` only the remote-tracking refs of `origin` (and FETCH_HEAD, objects) |
| `tidy plan` | the plan only: per local branch `delete`, `review` or `keep`, with the proof. `tidy` alone prints the usage (exit 2) | nothing |
| `tidy apply` | carries the plan out: deletes the local branches it marks `delete`, removes clean linked worktrees of such branches and dead worktree registrations. No confirmation flag; `--yes` is an unknown flag | local branches, worktrees, a manifest outside the repository |
| `tidy restore` | re-creates the branches of a run from its manifest | local branches |
| `tidy history` | lists the stored manifests (the journals of `tidy apply`), newest first, with the branches each recorded and how many `tidy restore` could create now | nothing |
| `add` | binds an EXISTING GitHub repository as a project (git submodule) of the workspace, see "stw add" | the clone directory, `.gitmodules`, the index, `.git/modules`, one local commit |
| `prove` | new, no Go counterpart: proves locally that a rewritten branch is the old one rebased and nothing else (eight checks, one `Carried-over:` line for `proved`), see "Local rebase proof" | nothing |
| `restack plan` | port of the single-branch prediction of the Go `restack plan` plus a whole-stack preview (new): which level of a stack of branches rebases cleanly onto a new base, where a conflict arises (files) and which level only inherits it, see "Stack preview" | nothing (no ref, no file, no object) |
| `absorb` | new, no Go counterpart: the PLAN of folding the staged changes into the unpushed commits that own the changed lines, see "`stw absorb plan`". `--apply --plan-sum SUM` folds that plan after an op log backup; `stw oplog undo` is the way back | without `--apply`: nothing. With it: fixup objects, `refs/stw/backup/<run>/1`, the branch, the op log |
| `ready-check` | the seven read-only conditions for a pull request | nothing |
| `lint pr` | the commits, the text and the comments of one pull request against the machine-checkable working rules, see "PR lint" | nothing |
| `lint review`, `lint pr-text` | new, no Go counterpart: the text of a review comment or of a PR text from a file against the same rules (and the patch-id of `BASE...HEAD` against a local repository), see "Text files: `lint review`, `lint pr-text`" | nothing |
| `sync restore` | moves the branches a `sync` run changed back to their journaled old tips, only where a branch still stands on the tip the run left (see "`sync restore`") | local branches (and the work tree of a checked-out one), the state of an incomplete journal record |
| `sync` | like Go it acts by default: fetch `origin` (any remote kind gix can serve, see "Remote kinds") and fast-forward the checked-out branch, index and working tree included. `--plan` (alias `--dry-run`, the same flag): like Go it fetches `origin` first (not with `--offline`) and says what would be fast-forwarded and why not | without `--plan`/`--dry-run`; with it only the remote-tracking refs of `origin` (and FETCH_HEAD, objects); `--apply` is a hidden alias without effect (see "Deviations from Go in these flags", 1) |

Without a flag `sync` only fast-forwards: it never merges, rebases, stashes, resets or pushes. It refuses (exit 1,
nothing changed) when a tracked local change overlaps a file the update touches, when untracked files are in the way,
or when a merge, rebase or similar operation is running; a diverged branch is reported (`skip`) and left alone. Local
changes elsewhere are kept. `--rebase` and `--merge` (exclusive) handle a diverged branch with the installed `git`,
`--autostash` and `--no-autostash` (exclusive) choose the stash, see "`sync --rebase` and `--merge`".

### Workspace checkpoints

Use `stw workspace worktree-plan REV DESTINATION --json` to resolve a local
revision to a commit, without creating a worktree. Save the result's `plan`
object outside the source worktree, then run
`stw workspace worktree-apply PLAN --json` in the same repository. Apply checks
that the plan identifies the Git common directory, that the pinned SHA-1
commit is available locally, and that the canonical destination is still
vacant. The destination's parent must already exist.

Apply checks out the saved commit in a native linked worktree with detached
HEAD, even if the original revision name now resolves elsewhere. It leaves
the source checkout, its index, and existing refs in place. If Git reports a
checkout error, inspect the reported destination and `git worktree list` for
partial state before retrying.

Use `stw workspace checkpoint --task NAME --plan --json` to inspect the selection,
then omit `--plan` to save it. Use `--include PATH` for each explicitly selected
non-ignored untracked file. Paths are relative to the worktree root. The
checkpoint records the source base, staged index contents, and working-tree
contents separately. Git publishes it under `refs/stw/tasks/NAME` only if the
ref still matches the value observed at the start. Earlier checkpoints remain
reachable through the task history. Source branch, index, and files are left as
they were. If the command stops around publication, `workspace inspect` reports
the checkpoint currently selected by the task ref.

`workspace retirement-plan --task NAME --json` compares the current HEAD,
index, and worktree with the task's published checkpoint. It reports staged
differences, worktree differences (including uncaptured untracked files), ignored
files, local-only commits, and active Git operations. On Unix it also reports
executable-bit changes even if the repository disables Git's file-mode checks.
Local-only commits are counted against all local remote-tracking refs, without
fetching. If none exist, every commit reachable from HEAD is local-only.
The preview returns successfully with `ready: false` if any of these checks finds
a blocker or if HEAD moved from the checkpoint base. It writes only a temporary
index. Keep the worktree idle during inspection and rerun the preview if its
state changes.

`workspace resume CHECKPOINT DESTINATION` creates a fresh detached linked worktree
at the recorded base. It restores the index and worktree separately, so selected
untracked files remain untracked. Choose a fresh destination with an existing
parent. A failed or interrupted restore leaves a sibling
`DESTINATION.stw-handoff-incomplete` marker. Use a fresh destination to retry.

This initial implementation uses the local Git runner (Git 2.46 or newer and a
configured Git identity for checkpoint commits). It supports SHA-1 repositories
with full worktrees and a regular index. Sparse checkouts, submodules, Git
operations in progress during capture or resume, worktree attributes, and
special index flags are refused.
Keep the source idle during capture. HEAD or index changes detected before
publication cause a refusal. Checkpoints survive loss of a worktree while their
Git objects remain available. Recovery after VM loss needs independent storage.

### `sync --rebase` and `--merge`

Port of the Go flags (`internal/stwsync/apply.go`, `config.go`). `--rebase` rebases the local commits of a diverged
branch (ahead and behind its upstream) onto the fetched upstream: `git rebase --no-update-refs --fork-point
refs/remotes/origin/<branch>`; `--merge` merges the fetched tip: `git merge --no-edit --ff <commit>`. A branch that is only
behind is fast-forwarded as without a flag (never a merge commit). `--plan` says `would rebase A local commit(s) onto B new`
or `would merge B new commit(s) into A local`, fetches like a run (not with `--offline`) and changes nothing else. Both need Git 2.38 (rebase) or 2.36 (merge), checked before git starts.
Tracked local changes (staged or not; untracked files do not count) refuse the run with the result `skip` ("cannot rebase
with local changes (use --autostash)") unless `--autostash` is given; `--autostash` stashes and reapplies them (git's own
autostash), `--no-autostash` is the default. The flag decides every time: the argument vector of git always carries
`--autostash` or `--no-autostash`, so `rebase.autoStash` and `merge.autoStash` never apply. Every run that changes a branch
writes the journal entry first (recovery ref and record `prepared`, no planned tip), so `stw sync restore` moves a rebased or
merged branch back to the tip it had. A conflict stops the rebase or merge as git leaves it (result `conflict`, exit 1, the
reason names `git rebase --continue|--abort` or `git commit|git merge --abort`, `detail` has git's message, the record is
`failed` with the reason "stopped on a conflict"). A conflict while the stash is put back is `autostash-conflict` (exit 1, the
stash is kept: `git stash pop` or `git stash drop`). After a successful run the branch must stand on a commit that has the
fetched upstream tip below it, else the result is `error` (`raced`). JSON: `result` is `rebased`, `merged`, `rebase`, `merge`,
`conflict`, `autostash-conflict`; `options` carry `rebase` (`true` or null), `merge` and `autostash` (`true`, `false` or null);
`summary` carries `rebased`, `merged`, `would_rebase`, `would_merge` and `conflicts`; a repository with a conflict has `detail`.
Deviations from Go, one by one:

1. No Git configuration is followed (`pull.rebase`, `branch.<name>.rebase`, `pull.ff`, `rebase.autoStash`,
   `merge.autoStash`, `pull.rebase=merges`): only the flags decide. The argument vector pins what a setting could
   otherwise change: `--no-autostash`/`--autostash`, `--no-update-refs` (`rebase.updateRefs`), `--no-rebase-merges`
   (`rebase.rebaseMerges`: a local merge commit is flattened), `--ff` and `--no-verify-signatures`
   (`merge.verifySignatures` would start the configured `gpg.program`). A set `branch.<name>.mergeoptions` refuses a
   `--merge` (`error`, nothing changes; `--plan` says `skip` and "would be refused"): git would add its options to the command line. A diverged branch with `pull.rebase` set is skipped and
   the reason says "stw does not follow it: use --rebase". There is no `--ff-only` (it is the default) and no
   `--rebase-merges`.
2. No hook of the repository runs (`core.hooksPath=/dev/null`, as for every git call of stw); Go ran them
   (`pre-rebase`, `post-rewrite`, `post-merge`, ...). No editor, no signing (`commit.gpgsign=false`).
3. A repository whose own config names a `filter.*` driver, a `merge.<name>.driver` or another key that starts a program is
   refused (`error`); nothing changes. Go runs git as configured.
4. The dirty gate is stricter for `--merge`: any tracked change refuses it without `--autostash`; Go refused only when a
   changed file overlapped the update (`git merge` itself refuses then).
5. The stash entry of an `autostash-conflict` is not claimed: the result has no `stash_oid` and the record no `stash_oid`
   (Go reads the stash reflog). The record has no `orig_head`.
6. The record of a stopped run has the state `failed` (Go: `conflict`).
7. A branch that is only behind is fast-forwarded by gix also with `--rebase` or `--merge` and tracked local changes,
   unless they overlap the update (the check of a plain `sync`); Go refuses the rebase then (`sync.go:392`: `git pull
   --rebase` refuses a dirty tree, even for a fast-forward). `--autostash` has no effect on a fast-forward: nothing is stashed.

Not a deviation: the gate looks at tracked changes without a changed submodule, as Go does (`--ignore-submodules=all`); git
itself also ignores them for a rebase.

### Submodules: the gitlinks of the index

The repositories of a workspace are the gitlinks of the root's index (mode 160000, any conflict stage, each path once, in
path order; `repo::submodule_paths`). `.gitmodules` is not read for that: a gitlink needs no entry there, a path that
`.gitmodules` names without a gitlink is no repository (a clone that no gitlink names is never listed), and a root with
gitlinks but no `.gitmodules` file is a workspace. The superproject of a checkout is the nearest parent repository whose
index has a gitlink at that path, and the workspace root is the top-level one (up to 16 levels, `repo::topmost_superproject`).
The nearest repository is found as git does (`discover`: `GIT_CEILING_DIRECTORIES` and the owner check apply, and only the
nearest repository counts: a repository in between that has no gitlink there ends the search).
Nested gitlinks (in a submodule's own index) are listed depth first, after the submodule that holds them, named `outer/inner`
(`repo::list_repos`); a gitlink without a checkout is never descended into. More than 256 repositories (the root included) is
a usage error (exit 2, text on stderr). Go fails the run with exit 1 and the JSON error code `sync-limit`, and it counts
uninitialized and unreachable entries too; Rust counts only checked-out repositories. Only `stw add` still reads `.gitmodules` (it refuses a path that the file names
already) and still takes the nearest superproject as its target.

A gitlink path is untrusted (a repository can name it). It becomes a repository of `status`, `sync`, `tidy plan` and `tidy apply` only if it
passes the rules of `stw add` (`[A-Za-z0-9._/-]`, relative, no `..`, no `-x` component, no git-special name), no component
is a symbolic link, and the real path of the directory is below the real path of the workspace root. Any other path is
never listed and nothing there is read or written (no fetch, no fast-forward, no PR lookup). The repository name is the
normalized path (no trailing `/`); a directory named twice is listed once. The refusal is reported once per path, escaped,
as `submodule path not accepted: <path>: <reason>` (a path with a space or other character outside the set: `rename with git
mv; stw handles only [A-Za-z0-9._/-]`), and never changes the exit code, except `sync --submodules` in a run (see below):

- `status`, `tidy plan`, `tidy apply`: a line in the notices (`status`: `Notices`; `tidy`: a parenthesized line at the end), exit
  unchanged; in JSON the additive top-level field `rejected_paths` `[{path (escaped), reason}]` (`schema_version` stays 2).
  No entry in `repos`, nothing in the counts or the repository total. `tidy apply` never touches such a path.
- `sync`: `--plan`/`--dry-run`, and a run without `--submodules`: an entry in `repos` with result `skip` and the text as `reason` (exit
  unchanged). A run with `--submodules`: result `error` ("unreachable: ..."), exit 1, like Go's `unreachable`, which arises
  only there; `--plan --submodules` shows `skip` ("would be unreachable").
- Not reported when `--repo` names repositories; an unknown `--repo` (also a refused path) is a usage error.

Deviations from Go: Go lists the same gitlinks (`ls-files --stage`, `sync.go:262-276`; absolute paths and `..` are filtered
there) and walks nested submodules, as Rust does now; it checks charset and symbolic links only for a `sync --submodules` run
(`validSubmodulePath`/`symlinkedPath`, `sync.go:166`). Rust checks every gitlink path of every level in every command (a nested
path is checked against the repository that holds it and reported below the root, `outer/inner`). Git cannot hold an absolute path or a `..` segment in an index and a
clone brings none, so such a path of `.gitmodules` is ignored (earlier Rust reported it as refused). The order is the path
order of the index (earlier: the order of `.gitmodules`). Go counts a gitlink at stage 1 to 3 once, as Rust does.
The workspace root of a checkout is found by repeating the nearest-superproject step up to 16 times (Go: `git rev-parse
--show-superproject-working-tree`, `sync.go:215`); the two agree on a plain nested checkout. They differ for a
superproject that is a linked worktree: Go resolves the main worktree at every level (`mainWorktree`), Rust takes the
linked worktree itself (also without nesting; checked by the reviewer, not covered by a test). An unreadable index gives an empty list (Go: the run fails with `cannot list the submodules`, `sync.go:265`).
Not nested-aware: `stw add` (target: the nearest superproject). `tidy plan` and `tidy apply` name repositories through
`list_repos` and therefore accept a nested one (`--repo outer/inner`); how `tidy apply` treats a repository that contains
a checked-out submodule is unchanged (`tidy_apply.rs`), and its behavior on a nested repository is UNKNOWN (not tested). `lint-pr` counts the GitHub slugs of all repositories of
the walk, nested ones included, and refuses a workspace with more than one slug without `--repo`.

### `sync --prune`, `--no-prune`, `--submodules`

Ported from the Go flags; like everything in `sync` they act in a run. `--plan`/`--dry-run` fetches (and prunes) too, as Go's plan does,
and writes nothing but the remote-tracking refs of `origin`, FETCH_HEAD and objects (see deviation 14 below); `--plan --offline` fetches
nothing and only says what the flags would do.

- `--prune`: the fetch of `origin` runs `git fetch --prune --no-prune-tags`, so remote-tracking refs that are gone on
  `origin` are deleted. `--no-prune` runs `git fetch --no-prune --no-prune-tags` (overrides `fetch.prune`). The two exclude each other (exit 2).
  Without either flag no option is passed: for a remote fetched by git (ssh, local path) `fetch.prune` of the
  configuration applies as it does for `git fetch`; gix, which fetches `https://`, never prunes.
  Reach: only `refs/remotes/origin/*` (without `origin/HEAD`). `--prune` refuses (typed `refused-refspec`, nothing fetched,
  `result: error`) when a configured `remote.origin.fetch` writes outside that namespace (for example `+refs/tags/*:refs/tags/*`);
  tags are never pruned (`--no-prune-tags` wins over `remote.origin.pruneTags`); no local branch is touched. The prune is part of the
  fetch, so it happens before the fast-forward: a branch whose upstream ref was pruned reads `skip: upstream branch is gone`.
  The refs of `refs/remotes/origin/*` are listed before and after the fetch, also after a failed one: the entry carries
  `pruned` (the deleted names, JSON), the reason says how many, and `summary.pruned` the total. Pruning is not undone.
  Plan: it prunes as a run does (`pruned`, `summary.pruned`). `--plan --offline` fetches nothing: it carries `prune` (`on`/`off`) and, with
  `--prune`, `tracking_refs` (how many refs could be reached); which of them are gone is only known from the remote, so it does not name them.
- `--submodules`: after every repository was synced, each pinned submodule (a gitlink of the index, checked out, detached HEAD) is
  moved to the commit the superproject's index records, when that is a fast-forward of its HEAD: `git submodule sync` and
  `git submodule update --no-fetch --checkout` in the superproject, through the runner (`GIT_ALLOW_PROTOCOL=none`, hooks off,
  no recursion, never `--init`, `--remote`, `--recursive` or `--force`). The submodule's own config is refused when it
  names a `filter.*` driver or another key that runs a command (`refused-config`). No gitlink, index or superproject commit is
  written. Entry: `result` `submodule-move` (dry run) or `submodule-moved`, JSON `submodule` {state, old, new, note}; states
  `moved`, `would-move`, `up-to-date` (`none`), `skipped-dirty`, `not-fast-forward` (both `skip`), `unreachable`, `failed`
  (both `error`, exit 1). A not initialized submodule (no checkout) and a recorded commit the submodule lacks are
  `unreachable`; the move is not journaled and not undone; URL rewrites of `submodule sync` are reported in `note`.
  The differences from Go are listed below.

Deviations from Go in these flags:
1. Like Go, `stw sync` acts by default and `--plan` and `--dry-run` (the same flag, in any combination) only report (owner
   decision 2026-11-11; until then Rust was dry-run by default). Differences: (a) the plan fetches like Go's, see deviation 14 below (until 2026-11-13 the Rust plan fetched nothing); (b) `--apply` is accepted as a hidden alias without effect (it is no Go flag; it is
   kept so that earlier calls work) and is not in the help; with `--plan` or `--dry-run` it is a usage error (exit 2); (c) the plan
   shows the prune scope and `would-move` for the submodules. It predicts a move from the gitlink in the index before the
   superproject's own fast-forward; a run takes the gitlink after it, so the two can differ.
2. `--prune` is always done by the installed git, also for an `https://` remote (gix cannot prune, and stw deletes refs
   through git's own fetch only). That git gets no GitHub token from stw: a private https repository needs a git credential
   helper (the error says so). Without a flag gix never prunes an `https://` remote; Go always used git and so `fetch.prune`.
3. `--prune` refuses, in a run, a `remote.origin.fetch` that writes outside `refs/remotes/origin/` (`refused-refspec`,
   nothing fetched); Go checked it in the plan only. Tags are never pruned (`--no-prune-tags` wins over `remote.origin.pruneTags`).
4. Only `origin` is fetched and pruned; Go prunes the remote of the branch.
5. JSON `pruned` is a list of ref names (Go: a number); `summary.pruned` is the total.
6. (Removed: Rust walks nested gitlinks depth-first as Go does, with `--submodules` acting on a nested pinned submodule
   against the submodule that holds its gitlink; README "Submodules: the gitlinks of the index".)
7. A submodule path outside `[A-Za-z0-9._/-]` is `unreachable` (Go: only absolute, `..`, empty segments, option-like and
   control characters); a path through a symbolic link is `unreachable` in both (Go `symlinkedPath`). Paths from `.gitmodules` are reported escaped.
8. `unreachable` is result `error` (run) or `skip` ("would be unreachable", `--plan`); Go has the result `unreachable`.
9. A run fetches `origin` of a pinned submodule too (Rust fetches every repository; Go fetches branch repositories only).
   A recorded commit that `origin` has is then present before the move, where Go says `unreachable`. If that fetch fails
   (private https without token, `unsupported-remote`, `refused-refspec` with `--prune`), the result is `error` and the
   submodule is not moved; Go would still move it. Chosen because a move after a failed fetch would act on a state the
   run could not refresh, and the failure stays visible.
10. A submodule config that names a `filter.*` driver or another key that starts a program is refused
    (`refused-config`); nothing moves.
11. The superproject's recorded commit comes from the index (as in Go); an uninitialized submodule is listed only without
    `--repo`. The submodule's local-change check uses gix status (tracked changes, untracked files excluded); whether a
    change inside a nested submodule counts is UNKNOWN (Go ignores it: `--ignore-submodules=all`).
12. `--no-prune` also passes `--no-prune-tags`; Go does not pass `--no-prune-tags`, so with `fetch.pruneTags` or
    `remote.origin.pruneTags` configured Go's fetch creates tags there. (`sync` without a prune flag keeps the Go
    behavior, see "Status flags".)
13. The Git configuration `pull.rebase`, `branch.<name>.rebase`, `pull.ff`, `rebase.autoStash` and `merge.autoStash` is not
    followed (Go follows it: rebase, merge commit, autostash). A fast-forward stays a plain fast-forward, without stash and
    merge commit; a diverged branch is skipped, and when `pull.rebase` or `branch.<name>.rebase` is set the reason adds
    "stw sync never rebases (not ported)". `--rebase`, `--merge`, `--autostash`, `--no-autostash` are an open package.
14. The plan fetches `origin` like Go's `--plan` (owner decisions 2026-11-13) and prints "Only the remote-tracking refs were fetched; nothing
    else was written" (offline: "Nothing was fetched either: based on the last fetch"; after a failed fetch: "The fetch failed for N
    repositories; their plan is based on the last fetch"). Differences from Go, one by one:
    (a) the fetch is Rust's fetch of a run (`origin`, `git` through the runner or gix for https, `--no-tags`), not Go's argv
    (`fetch --no-tags --no-write-fetch-head --no-recurse-submodules -- <remote of the upstream>`); (b) the plan passes `--no-prune-tags`
    also without a prune flag (Go: not), so `fetch.pruneTags` and `remote.origin.pruneTags` cannot delete a local tag; `fetch.prune` still applies;
    (c) FETCH_HEAD is written by the git path (Go: never; gix 0.90 writes none), so after a git fetch `status` sees the plan's fetch;
    `last_fetch_seconds_ago` in the plan is the value before its fetch; (d) `origin` is fetched for every repository the run would fetch,
    also one on a detached HEAD or without upstream and a pinned submodule (Go skips these); a repository without `origin` is `no-remote`,
    not an error; (e) a failed fetch is result `error` with `fetch: failed|unsupported|unsupported-remote`, exit 1 (Go: `failed`, exit 1);
    (f) the refspec check of Go's plan (`sync-refspec-unsafe`) is typed `refused-refspec` (`git_error`) and applies with and without a
    prune flag; (g) `--plan --offline` fetches nothing (Go has no `--offline`); (h) `remote_moved` is always null in a plan (the dry run asked
    GitHub before; the plan has the fetched state); (i) timeout: the runner cuts a git fetch after 120 s (Go: 5 minutes); the gix path for
    https has no bound (not changed); (j) `--rules --plan` fetches too, see "agent-rules".
15. The run of `sync --rules` refuses a configured fetch refspec of `origin` that writes outside `refs/remotes/origin/` before it fetches
    (the port of Go's `rulesFetch` -> `checkRefspecs`, `sync-refspec-unsafe`; the check is the one of the plan, `check_scope`, with the
    text "sync --rules must not write a local ref; nothing was fetched"): exit 1, fetch `failed`, nothing written. Differences from Go, one by one:
    (a) Go does not fetch then and goes on with the state "as of last fetch" (`as_of_last_fetch`) and can still fast-forward to the last fetched
    `origin/<default>`; Rust refuses the whole run, as it does for a plan (and as for a failed fetch); (b) with `--offline` nothing is
    fetched and no refspec is checked (as in Go with `noFetch`); (c) the refspec is shown escaped and cut at 200 characters.
16. Similar patches in `tidy plan` (new, Go has no percentage measure; README "Similar patches in `tidy plan`"): the evidence class `similar-patches` (decision `review`, never
    `delete`) with `--similarity`, `--similarity-low` and `--no-similarity`, the JSON object `similarity` and `summary.similar`. Limits of the measure: reformatting counts as a
    difference (only the white space at the ends of a line is ignored); a rename shows only in `similarity_no_path`, not in the class; a squash or a split commit is not recognised (class
    `partial`); the window of 7 days (author time) and "without candidate" means no commit on the base with the same subject or at least half of the changed files in the window; 100 %
    can still mean different behavior when the context on the base differs; several commits of the branch can have the same partner; a commit of more than 20 000 changed lines or a blob over
    1 MiB has no value and prevents `landed`; hunks come from gix (Myers), not from git's xdiff, so a multiset can differ from git's in rare cases (UNKNOWN until the oracle). A shallow clone gives
    a plan with the boundary commits of the base left out (`skipped.shallow`); for a branch that the boundary hides, see deviation 19.
17. Rebase evidence in `tidy plan` (new, Go has no identity or rebase stage; README "Rebase evidence in `tidy plan`"): the evidence class `rebase-identity` (decision `review`, never `delete`),
    `--no-rebase-evidence`, the JSON objects `rebase` and `landed_via`, `summary.rebase`, and the read of the commits of a merged pull request. Limits: the mailmap is not applied (raw author mail
    and name are compared, a changed mail gives no match); times have a resolution of one second and only the epoch seconds are compared (a different zone offset is noted, `tz_differs`); a
    squash merge is recognised only through the pull request (`landed_via` squash, basis `pr-api`), never from the commits alone; a rebase followed by `git merge --ff-only` (the push flow of
    this workspace) leaves the branch commits on the base, so the tip is an ancestor and `landed_via` says `merge`/`ancestor`; a content value below 95 % per commit (the owner's floor) is
    a hint, not a proof; the floor, `RB_STOP_RUN` (64) and the 30 requests per repository are constants not calibrated on real data; an amend after landing keeps author time and subject and
    is caught only by the content value and the later committer time.
18. `tidy plan --explain` (new; Go has `--explain` with a `next_command` per kept candidate, issue 388): Rust prints the evidence only and never a command for someone else to run
    (rule of this README, "Output for agents"), so there is no `next command`; the text is derived from the data of the plan (README "Explanation in `tidy plan --explain`"), the JSON is
    unchanged (it already holds the data), and the flag is not on `tidy apply`.
19. Shallow clones in `tidy plan` (new; README "Shallow clones"): Go has no rule for them. A branch whose commits reach the boundary of a shallow clone, or that has no partner in the part of the base
    that is there, is `review` with the proof `shallow-history` (never `delete`, never `unmerged`), JSON `shallow`. Limit: the boundary is what `.git/shallow` lists (a merge commit too) or a commit with a parent that is
    missing; the behavior in a partial clone (`--filter`, no shallow file) is UNKNOWN.

## agent-rules

`sync` and `status` print one line about the rules repository (`wstein/dotagents`) when the workspace has a checkout:
`agent-rules: current | behind N commits of origin/<default> | ahead N commits ... | diverged | dirty | not on default branch | not found`.
In a whr workspace the checkout is the git submodule `.agents` (normally a detached HEAD at the commit the superproject
records); a plain checkout on the default branch works too. It is `--rules-path PATH` (`sync` and `status`), else
`<workspace>/.agents`; nothing else is searched. The line reads the local `origin/<default>` ("as of last fetch").

`sync --rules` handles that checkout like `sync` handles the repositories. With `--plan` or `--dry-run` it fetches `origin` like Go's
`--rules --plan` (`rules.go` `rulesFetch`; not with `--offline`) and changes nothing else: it prints the current rules commit and what it
would do (how far `origin/<default>` is ahead, the new commits and files). Differences from Go: a refspec that writes outside
`refs/remotes/origin/` and a failed fetch refuse (exit 1) as in a run (Go notes "as of last fetch" and goes on); the plan passes
`--no-prune-tags`; FETCH_HEAD is written by the git path (Go: `--no-write-fetch-head`). The run (`--rules` without `--plan`) checks the refspecs of `origin` before it fetches, like Go's `rulesFetch`:
one that writes outside `refs/remotes/origin/` refuses (exit 1, fetch `failed`, nothing written; deviation 15 of the flags). Otherwise it fetches `origin` (not with `--offline`) and fast-forwards HEAD to
`origin/<default>`: the branch of a plain checkout, or the detached HEAD of a submodule. A checkout that is only ahead of
`origin/<default>` (or already current) is not an error: nothing to fast-forward, exit 0. The rules checkout is never part
of the generic repository loop: `sync` and `status` leave it out of the repositories list (the resolved `--rules-path` or
`.agents`), so only `--rules` ever touches it. The `tidy` commands skip it too (the default `.agents`): `--repo .agents` is an unknown repository there, and a plan entry naming it is not in this workspace. Refusals (exit 1, nothing changed) are only for cases where something
could be lost or the update cannot be done: changed files in the working tree; diverged (local commits not in
`origin/<default>` and new ones upstream); another branch than the default one checked out; the fetch failed; no checkout
at the path; the path is not a git repository; the checkout has no remote `origin`, no commits, or `origin` has no default
branch (no `origin/HEAD`, `origin/main` or `origin/master`, or `origin/HEAD` points to a missing ref); the checkout cannot be read; the fast-forward itself
refuses (changed files, untracked files in the way, unmerged paths); the commit walk or diff fails. Every new commit must be by an owner (port of the Go check):
the author is an owner identity, the committer an owner identity or an `--allowed-committer`; otherwise the update is refused
(`not by an owner identity: <sha7> author|committer Name <email>`, three named, then `(and N more)`), in a plan as well as in a
run, before anything is written. `--owner-identity "Name <email>"` (repeatable) names the owners; the default is
`user.name` and `user.email` of the Git configuration seen from the workspace root. `--allowed-committer "Name <email>"`
(repeatable) adds committers only, such as the GitHub rebase-merge committer `<login>@users.noreply.github.com`. The name is
compared exactly, the email without regard to case. Both flags are a usage error without `--rules` and without the form
`Name <email>`. Deviations from Go: when the workspace root has no identity the configuration of the rules checkout is used
before the update is refused for lack of any owner identity (`no owner identity`); the list of at most three faults is
built in the order of the walk, not of `git log`; the info line `new commits by ... (not the configured git identity)`
stays for what is not refused.

stw never commits in the workspace: after updating a submodule the line `.agents has a new commit, recorded as modified
in the workspace (not committed)` says that its gitlink shows as modified there; the owner commits it. The new commit is only kept once the workspace records it: `git submodule update` can restore the recorded commit (`whr-up` runs it when a submodule is not yet initialized). Before the update the
new commits and changed files are listed; files under `rules/`, `skills/`, `guards/`, `adapters/` and `install.sh` are
marked `!`. After it: the change applies to NEW sessions; running agents keep their loaded rules. stw never runs
`install.sh` and never writes to `~/.claude`.

JSON (`schema_version` stays 2; the field is additive): `sync` and `status` carry `agent_rules` {path, state, branch, head, detached, submodule, default_ref, ahead, behind, dirty_files,
fetch, last_fetch_seconds_ago, requested, action (`none`, `would-update`, `updated`, `refused`), reason, commits, files,
flagged_files, foreign_authors, updated}.

Shared options: `--json`, `--offline` (no GitHub, no fetch), `--root PATH` (default: the repository or
superproject of the current directory, else `$STW_ROOT`), `--repo NAME` (repeatable; an unknown name is a usage error).

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | everything asked for worked (also when repositories need attention, and in `status` also when a GitHub read failed: that is a notice, Go exits 1 there) |
| 1 | the command ran, but a repository could not be read; `sync` failed in one; `sync restore` found no run or left an entry `changed-since`, `journal-incomplete` or `failed`; `tidy apply` failed a step (a branch, worktree, registration or the `info/exclude` file; the manifest could not be written); `tidy restore` left an entry not restored (`not-overwritten`, `missing-object`) |
| 2 | usage error or unusable workspace; nothing was done |
| 3 | `lint pr`, `lint review`, `lint pr-text`: an error finding (`lint pr` also: a list cut at its bound); the result is printed and is not a pass |

`lint review` and `lint pr-text` read nothing from the network: 0 no error finding, 3 an error finding, 2 usage error (also an unreadable or non-UTF-8 FILE, a revision that does not resolve, no merge base).

## Output for agents

stw executes what it does and never prints a command for someone else to run: no `next:` lines in the text output
and no `next_steps` field in JSON (one exception: the op log hook hint of `stw status` names `stw oplog hooks install`, owner decision 2026-11-13). Notes say what happened or why something was skipped.
`--json` prints one compact JSON object. `schema_version` is `2` (version 1 had `next_steps` in `status`, `tidy-plan`
and `sync`; there is no `next_steps` any more); fields are only added within a version, a
removal or a changed meaning raises it. Common top-level fields: `schema_version`, `command` (`status`,
`tidy-plan`, `tidy-apply`, `tidy-restore`, `tidy-history`, `sync`; `prove` has its own layout, see "Local rebase proof"), `root`, `engine`, `repos`, `summary`. `status` and `tidy-plan` also carry `github` (bool),
`github_auth` (where the token came from, never the token) and `github_note` (why GitHub was not read). `status` also carries `notices` (below).
Every repository entry has `name`, `path` and `error` (string or null). `tidy-plan` carries `read_only` (always true); `sync` carries `mode` (`dry-run` for `--plan`/`--dry-run`, else `apply`).

`summary`: `status` {total, attention}; `tidy-plan` {delete, delete_with_worktree, review, keep, similar, rebase} (`similar`: the branches with the proof `similar-patches`, `rebase`: those with `rebase-identity`; both are counted in `review`, too. `delete_with_worktree`: kept branches whose worktree `apply` will remove, so `apply` deletes them; they are not in `keep`. A worktree that `apply` leaves alone because it is dirty, locked or has unpushed commits keeps its branch in `keep`); `sync` {total, fast_forwarded,
would_fast_forward, skipped, errors}.

- `status` repo: `counts` (below), `state`, `branch`, `detached`, `head`, `upstream`, `upstream_gone`, `ahead`, `behind`,
  `changes{staged,unstaged,untracked,conflicts}`, `stash`, `operation`, `gitlink{recorded,state}`, `slug`,
  `attention`, `reasons`, `oplog_hooks{state,reason,missing}` (null when the repository could not be read; see the hook hint under "`stw oplog hooks status | install | uninstall | print`"), `ci_main{read,error,branch,head,class,source}` (the CI of the default branch, null members when it was not read; see "CI of the default branch in `stw status`"), `pull_requests{read,error,open[{number,title,head,draft,author,review,review_state,hints,base,default_branch,merge_state}]}`. `review` of a pull request is null unless `--review` read it (REST has no `reviewDecision`; then the status label of the review state, see "Review state in `stw status --review`"); `review_state` is null or the state document; `merge_state` is gh's `mergeStateStatus` value, taken from REST `mergeable_state`.
  Additive, from the flags (see "Status flags"): top level `options{fetch,worktrees,icons,review}` (`icons` is always null: the JSON carries no glyphs), per repository `pull_requests.review` (null without `--review`, and also with it when the pull request list of the repository was not read; else `{states{clear,clear_carried,findings,no_review,unverified_author,unknown},hints{clear_not_ready,base_not_default},not_read,limit}`), per pull request `review_state`, `hints`, `base` and `default_branch` (below) and, only with `--stats` or `--verbose`, `stats{requests,rate_limit_limit,rate_limit_remaining,rate_limit_reset,duration_ms}` (the three rate-limit values are null until GitHub reported them; `rate_limit_reset` is `YYYY-MM-DDTHH:MM:SSZ`; no URL, no token); per repository `fetch{state,via,error}` (`state`: `not-run`, `ok`, `no-remote`, `failed`, `unsupported-remote`, `unsupported`; `via`: `git`, `gix` or null), `fetch_error` (the error text of a failed fetch, else null) and `worktree_list` (null without `--worktrees`, else `[{path,branch,detached,dirty,missing,locked,prunable}]`: `locked` is null or the lock reason, empty when none was given; the main checkout is not in it).
- `status` notices: top level `notices`, an array that is always present (`[]` when there is nothing to say), additive in schema 2. It is made from the same list as the text block `Notices (N)`: N is its length, element i is line i, the order is fixed (the GitHub note; then per repository in list order `fetch-failed`, `prs-not-read`, `issues-not-read`, `ci-not-read`, `review-not-read`, `review-limit`; then `rate-limited` of the run; then `rejected-path`). Element: `code` (string, below), `source` (string: `github`, `fetch`, `pulls`, `issues`, `ci`, `reviews`, `submodules`), `repos` (array of repository names the notice is about, escaped and cut to 300 characters like in the text; `[]` for a notice of the run), `message` (string, the text line without its two leading spaces, text from outside escaped like in the text: no control character, no bidi override), `count` (integer, at least 1: the pull requests or reads the notice names; 1 for the others). `github_note`, `pull_requests.error`, `fetch_error` and the issues `state` stay as they are. Codes (a closed list; a new code is an additive change):

  | `code` | `source` | `repos` | `count` | meaning (Go: same meaning) |
  |---|---|---|---|---|
  | `offline` | `github` | none | 1 | `--offline`: GitHub not read (Go reason `offline`) |
  | `no-token` | `github` | none | 1 | no token found: GitHub not read (Go reason `no-token`) |
  | `github-not-read` | `github` | none | 1 | GitHub not read for another reason (not produced today) |
  | `fetch-failed` | `fetch` | the repository | 1 | `--fetch` failed for it; the cause is in `message` |
  | `prs-not-read` | `pulls` | the repository | 1 | the open pull requests could not be read (401, 403, 404, 429, 5xx, unreadable answer, `deadline:` of `--timeout`); the cause is in `message` |
  | `issues-not-read` | `issues` | the repository | 1 | the open issues could not be read; same causes |
  | `ci-not-read` | `ci` | the repository | 1 | the CI of the default branch could not be read (denied: 403 on check runs and on Actions runs, 401, 404, 429, 5xx, an unusable or incomplete answer, `deadline:`); the cause is in `message` (Go: a notice per error code, "forbidden: default branch CI of 2 repositories not read") |
  | `review-not-read` | `reviews` | the repository | pull requests whose comments were not read | `--review`: review state `?` for them (denied, missing, over a bound, `--timeout`); first cause in `message` |
  | `review-limit` | `reviews` | the repository | pull requests beyond the limit of 200 | `--review`: not read because of the limit |
  | `rate-limited` | `reviews` | none | reads not started | `--review`: a rate limit stopped the comment reads (Go reason `rate-limited`) |
  | `rejected-path` | `submodules` | none | 1 | a `.gitmodules` path that is not a plain directory below the root was not used; the path is in `message` |

  The cause of a failed read (rate limit, 401, 403, `deadline`) is not a code of its own: it is part of `message` (Rust has no typed error there; Go's `unauthorized`, `forbidden`, `deadline` reasons have no counterpart as codes). A repository that cannot be read locally has no notice: its `error` is set and the exit code is 1. Differences to the notice of Go (`legacy/sourcesteward/internal/overview/notice.go`), one by one: (1) Go creates no notice for `offline` (it skips that reason), Rust creates `offline`; (2) Go sets `source` to `remote` for `no-token`, `deadline` and `rate-limited`, Rust uses `github`, `pulls`, `issues`, `reviews`, `fetch`, `submodules`; (3) Go groups all repositories of one code and source into ONE notice (`repos` lists them, also for `no-token`), Rust creates one notice per repository and source, and for `offline`, `no-token`, `rate-limited` and `rejected-path` `repos` is `[]`; (4) Go names a repository `OWNER/NAME` when the slug is valid, Rust uses the local name; (5) Go sorts by code, source, message, Rust has the fixed order above; (6) the Go schema limits `message` to 512 characters (I did not find the cut in the code), Rust escapes it and cuts the text from outside (300 characters for names, 200 for causes); (7) `count` exists only in Rust; (8) Go's codes `unauthorized`, `forbidden`, `deadline`, `drafts-not-visible` and the others have no counterpart in Rust; (9) the failed `ci-main` source is the code `ci-not-read` per repository with the cause in `message` (Go: the error code of the read, e.g. `forbidden`, grouped over repositories). The Rust document is not valid against the Go schema v2 anyway (fields are missing), `count` does not change that. `notices[].repos` is not a join key for hostile names: `repos[].name` is raw, `notices[].repos` is escaped and cut.
- `status` counts: per repository `counts`, in total the top-level `totals`, same shape: `pull_requests{state,open,bot,truncated}`,
  `issues{state,open,bug,truncated}`, `branches{local,remote}`, `worktrees`, `stash` (all numbers or null). `state` is `ok`,
  `not-read` (`--offline` or no token), `no-remote` (no GitHub origin; left out of a total) or `failed` (the read failed or the
  repository cannot be read; a notice says why); `open` and `bot`/`bug` are null unless `ok`, never 0 for a missing read.
  `truncated` is true when one page of 100 was full: the numbers are lower bounds. `bot`: author type `Bot` or a `[bot]`
  login; `bug`: a label named `bug`, case-insensitive; issues exclude pull requests. `branches.remote` counts
  `refs/remotes/origin/*` without `HEAD` as of the last fetch; `worktrees` counts linked worktrees whose directory exists, relative links included (not the main checkout, no stale registrations).
  A total is a number only when every repository with a GitHub origin gave one, else null with `state` `failed` (any), `not-read`, or `no-remote` (no repository has a GitHub origin). Schema rule:
  `counts` and `totals` are additive, `schema_version` stays `2`; `day briefing` reads this contract, so a removal or a changed
  meaning raises the version.
- `tidy-plan` repo: `base`, `base_commit`, `slug`, `note`, `branches[{branch,commit,decision,proof,detail,unique_commits,upstream,upstream_track,worktree,similarity,rebase,squash,shallow,landed_via}]` (`shallow` is null or `{unreadable_commits,without_partner,base_cut}`, see "Shallow clones"; `similarity` is null or the object of "Similar patches in `tidy plan`"; `rebase` is null or the object of "Rebase evidence in `tidy plan`"; `landed_via` is always there) (`worktree` is null, or `{path,removable_proof,outside_worktrees_dir,blocker}` for a `keep`/`in-worktree` branch that `tidy apply` may remove with its worktree; `blocker` is null when `apply` will remove both, else the reason it leaves them),
  `stale_worktree_registrations[{path,locked,lock_reason}]`. Proofs: `in-worktree`, `open-pull-request`, `merged-ancestor`, `equal-patches`,
  `merged-pull-request-head`, `backup-older-than-days`, `backup-recent`, `rebase-identity`, `similar-patches`, `shallow-history`, `pull-request-merged-branch-moved`, `upstream-gone`, `unmerged`.
- `sync` repo: `branch`, `upstream`, `ahead`, `behind`, `dirty_files`, `fetch` (`not-run`, `skipped-offline`,
  `ok`, `failed`, `no-remote`, `unsupported-remote`), `last_fetch_seconds_ago`, `remote_moved`, `result`
  (`none`, `fast-forward`, `fast-forwarded`, `skip`, `error`; with `--submodules` also `submodule-move`, `submodule-moved`), `reason`.
  Additive: top-level `run_id` (the ID of the run when it moved a branch, else null; always null with `--plan`/`--dry-run`) and per repository `journal` {n, recovery_ref, state} (only for a branch the run changed or tried to change after the checks: see "The journal of a `sync` run"), `prune` (`on`/`off`, only with `--prune`/`--no-prune`), `pruned` (names deleted by the fetch; with a flag, or when a ref was pruned),
  `tracking_refs` (`--plan --offline` with `--prune`), `submodule` {state, old, new, note} (`--submodules`); top level `options` {prune: true/false/null, submodules, dry_run (additive: true for `--plan`/`--dry-run`)}
  and in `summary` `submodules_moved`, `would_move_submodules`, `pruned`.

## Status text

`stw status` prints blocks separated by a blank line: `Counts` (one aligned line per repository, then `All`:
`PRs n (bot n) | Issues n (bug n) | Branches local/remote | Worktrees n | Stash n`; `n/a` is not read, `?` is failed, `n+` is a
lower bound at a full page), `Needs action (N)` (repositories with an attention reason), `Repositories (N)`, `Notices (N)`
(why GitHub was not read, failed reads per repository; text from outside is shown with escapes; the JSON `notices` has the same list, see "Output for agents"), the `agent-rules` block, then the summary line. A failed GitHub read is
a notice: exit 0. Exit 1 stays for a repository that cannot be read locally. Under a repository of a GitHub origin a line `main CI green (main@163ada585103, via check runs)` says what the CI of the head of the default branch is (`none`, `pending`, `green`, `red`; `via Actions runs` when check runs were denied), or `main CI not read (cause)`; a red head is the attention reason `main-red` (see "CI of the default branch in `stw status`"). Under a repository one more line says when the op log hooks are not installed (a hint only; see "`stw oplog hooks status | install | uninstall | print`").

## Status flags

Ports of the flags of the Go `stw status` (`legacy/sourcesteward/internal/cli/status.go`). Without a flag nothing changes, and
`--json` is the same document plus the additive fields above. `--review`, `--timeout`, `--stats` and `--verbose` are a usage error (exit 2)
with `--offline`.

| Flag | What it does |
| --- | --- |
| `--long`, `-l` | The text only: under `Needs action` every reason is explained from the facts of the repository (what is staged, how far behind, which operation), under `Repositories` each repository gets a card (`path`, `remote`, `head`, `upstream`, `changes` by kind, `branches`, linked worktrees, `stash`, `gitlink`). The blocks stay in their order, `Notices` and the summary line stay last. The JSON is the same as without it. No command is printed for someone else to run. |
| `--fetch` | `git fetch origin` of every repository before it is read, so that ahead/behind and `Branches` are current. The one thing that is not read-only, and it changes remote-tracking refs only (`refs/remotes/origin/*`, FETCH_HEAD, objects). It goes through the same fetch as `sync --apply` (ssh and local-path remotes: the installed `git` through the runner, `"via": "git"`, fixed argv, no hook, `gc.auto=0`; https: gix, no program, the GitHub token only for github.com), always with `--no-prune` (wins over `fetch.prune`) and `--no-prune-tags` (so `pruneTags` creates no tag), no tags, no submodules. A repository whose configured fetch refspec writes outside `refs/remotes/origin/` is not fetched (it would write branches). A failed fetch is a detail line, a notice and `fetch.state` `failed`, exit 0; the status of that repository is still shown. It works with `--offline`: `--offline` stops the GitHub reads and the token, not the fetch. The `agent-rules` checkout is not fetched (`sync --rules --apply` does that). |
| `--worktrees` | One line per linked worktree under its repository: path, branch or `detached`, then `dirty`, `locked` (with `: reason`), `prunable`, `missing`. `dirty` is any tracked change or untracked file in that worktree (a worktree that cannot be read counts as dirty). Registrations whose checkout is gone are listed too (`missing`, and `prunable` unless locked); the main checkout is never listed. The `Worktrees` count keeps its meaning (existing linked checkouts, no stale registration, no main checkout). Sorted by path. |
| `--review` | Also reads the review state of the open pull requests from their comments: one comment read per pull request (a list read of at most 500 comments, which is up to 6 requests of 100, none after the `--timeout`), at most 16 reads at a time, at most 200 pull requests per run (repository order, newest pull request first; the rest is `not read` with a notice). Each pull request line gets `review <state>` (`clear`, `clear-carried`, `findings`, `no-review`, `unverified-author`, `?`), each repository a line `reviews: 1 clear, 2 ?` and, with `--long`, the evidence per pull request. See "Review state in `stw status --review`". `Counts` do not change. Not with `--offline` (usage error, exit 2). |
| `--icons unicode\|nerd\|ascii` | The marker in front of a repository line: attention `▲`, cannot be read `✖` (unicode), `U+F071` and `U+F057` of Font Awesome (nerd, needs a Nerd Font), `!` for both (ascii). Default: unicode when stdout is a terminal, the locale is UTF-8 (the first of `LC_ALL`, `LC_CTYPE`, `LANG` that is set decides) and `TERM` is not `dumb`; ascii otherwise (a pipe, a file). An explicit value always wins. Text only. |
| `--timeout DURATION` | The overall deadline of the GitHub reads, 1s to 10m, default 90s, written as Go does (`90s`, `2m`, `1m30s`, `1.5s`). Every request is cut at the deadline and none starts after it (one request also takes at most 30 s). The cut reads are notices (`pull requests not read (deadline: ...)`), their counters `?`, exit 0. The fetch of `--fetch` and the local reads are not part of it. |
| `PATH..` | Only these repositories, in one line each with the stack of the current branch; `--json` is the same document plus `kind`, `depth`, `stack`. A PATH that holds a repository is that repository with its nested submodules, any other directory is scanned below for repositories. Not with `--long`, `--icons`, `--worktrees`, `--review`, `--stats`, `--verbose`, `--repo` (usage error). Without a PATH: the repositories of the workspace, as before. See "`stw status PATH`". |
| `--depth N`, `--limit N` | Only when a PATH is a directory that is scanned (usage error, exit 2, otherwise): how many levels below it repositories and their submodules are listed (0 to 64, default 16; a cut is a notice) and the most repositories of a run (1 to 1024, default 256; more is a usage error, exit 2). |
| `--stats` | One line after the summary: `stats: API: 4 reqs (4,896/5,000 left, reset 22:13 UTC) \| 1.4s elapsed (local 110ms, remote 1.3s)`. `local` includes the fetch of `--fetch`; `elapsed` ends before the `agent-rules` read. With `--json` the member `stats`. |
| `--verbose` | Implies `--stats`. Before the stats line: `Diagnostic breakdown:` with the requests and the summed request time per repository and source (`pulls`, `issues`; the comment reads of `--review` count as `pulls`). With `--json` the breakdown and the stats line go to stderr, so stdout stays one document. |

### Differences from the Go `stw status` flags

- `--long`: the Go default is a compact overview (one line per repository), `--long` the expanded one. The Rust default text is
  not compact: it already prints the detail lines of Go's long layout (operation, stash, gitlink drift, pull requests), and
  `--long` adds the card and the explanations on top. There is no compact mode. The explanations are sentences from the facts;
  Go prints the suggested command (`run:`) and a `hint:` per item, Rust never prints a command (see "Output for agents"). Go's
  action items with priorities (red to violet), their links and the sources release drafts and tags do not exist in Rust:
  the remote half is pull requests, issues and the CI of the default branch (`ci-main`, below).
- `--icons`: Rust has one marker per repository line and no priority levels, so the glyph sets cover the two markers only
  (Go also puts the priority glyph in front of every action line). The glyphs are those of Go. Go keeps the ascii marker in its
  local layout unless `--icons` is given; Rust has one layout, so the terminal default applies to every run, `--offline` and a run
  without a token included. The default also asks whether stdout is a terminal, as Go does.
- `--fetch`: Go runs `git fetch --no-tags --no-write-fetch-head --no-recurse-submodules --no-prune` for every remote kind with at
  most 16 in flight. Rust uses the fetch of `sync` (see above): https remotes are fetched by gix, the git path writes
  FETCH_HEAD, the refspec scope check is an addition, `--no-prune-tags` is passed as well (Go omits it, so with
  `fetch.pruneTags` or `remote.origin.pruneTags` Go's fetch creates tags; Rust's does not), the `agent-rules` checkout is not
  fetched, and the GitHub token is offered for github.com https unless `--offline` is given. At most 16 repositories are worked on
  at once, as in Go; this bound is shared by `sync` and `tidy`, whose results keep the repository order.
- Correction candidate, not changed: `sync` without `--prune`/`--no-prune` runs `git fetch --no-tags` without
  `--no-prune-tags` (as Go `stwsync` does), so `fetch.pruneTags=true` or `remote.origin.pruneTags=true` makes it create tags.
  `sync` is a mutating command and keeps the Go behavior until the owner decides.
- `--timeout`: Go's deadline also covers its other sources (repo, releases, tags, ci-release, viewer) and cuts a request at 15 s; Rust cuts
  at 30 s. A failed GitHub read is a notice and exit 0 in Rust (Go: exit 1), see "Exit codes". The notices come per repository
  (Go: once per source).
- `--stats`, `--verbose`: Go counts every source it has; Rust has `pulls`, `issues`, `ci-main` (the reads of the default branch CI: repository, branch, check runs, statuses, Actions runs) and `other`. `cached 304s` is always 0
  (Rust sends no ETags) and `consumed` equals the requests. There is no `viewer` line. The rate limit is the one of the answer
  with the smallest remaining count. Go's `--verbose` text output puts the breakdown before the stats line as Rust does.
- `--worktrees`: Go counts missing and prunable registrations as linked worktrees and shows, even without the flag, a line
  `N linked worktrees (1 dirty, 1 locked, ...)` and the JSON counts `linked`, `dirty`, `locked`, `prunable`, `missing`; Rust keeps
  its `Worktrees` count (existing checkouts only), has no such summary line or counts, and lists the stale registrations only with
  the flag. Go shows the lines only in its long layout, Rust always with the flag. Go lists in the order of `git worktree list`,
  Rust sorted by path. A relative link (a `..` in its path) is listed and `missing` is checked, `prunable` is not evaluated for it.
- Before this change `status` had no equivalent of `--worktrees`: only the number in `Worktrees` and, in `tidy plan`, the worktree
  of a kept branch and the stale registrations. The list of linked worktrees with their state is new.
- `--review`: see "Review state in `stw status --review`" for the differences.
- JSON: Go's schema 2 differs from the Rust one as a whole (see the migration plan); the new fields here are only additive to
  the Rust document, `schema_version` stays `2`.

### CI of the default branch in `stw status`

The source `ci-main` of Go, ported (owner decision 2026-11-17; before that the Go sources CI, release drafts and tags were
left out on purpose). For every repository with a GitHub origin and a token (not with `--offline`) `status` reads the default
branch (`GET /repos/OWNER/NAME`, then `GET /repos/OWNER/NAME/branches/NAME` for its head) and the CI of that head, and gives it
one class: `none` (no check run, no run, no status), `pending`, `green` or `red`. The class is the one of `ready-check`
(`ready_check::check_class`; a failed, cancelled, timed out, `action_required` or `startup_failure` run or a failed or
errored status is `red`, a run that is not completed or a pending status is `pending`, else `green`): one rule for both commands.
Of the commit statuses the newest of every context counts, as in Go. A list that was read only in part, or a run without a
state, is not read (no class): partial evidence could hide a failure.

Which runs are read depends on the answer of GitHub, never on the kind of the token:

1. `GET /repos/OWNER/NAME/commits/SHA/check-runs` (needs `Checks: Read`; a classic token with the scope `repo` has it). `source` is `check-runs`.
2. Only when that answers 403 (not 401, 404, 429, 5xx, not a rate limit): `GET /repos/OWNER/NAME/actions/runs?head_sha=SHA&per_page=100`
   (`Actions: Read`; a private repository gives a fine-grained token no check runs, because `Checks` is not offered for fine-grained tokens, so that read ends here). Per workflow and event the newest run of
   that commit counts (`created_at`, then `run_attempt`, then `id`); a run of another commit is ignored; the run is classified
   like a check run. No run is the class `none`: that says there is no Actions run for the head, not that there is no CI (see
   "What the two ways see"); it is not an error and no finding. `source` is `actions-runs`.
3. `GET /repos/OWNER/NAME/commits/SHA/statuses` in both ways, as Go.

What the two ways see differs, and the `source` says which one was used:

- `check-runs` lists every execution of every app that reports checks (Actions and other apps) as its own entry.
- `actions-runs` sees Actions workflows only. Check runs of other apps are invisible, so `none` there means: no Actions run for this
  commit (a repository with checks of another app can still have a CI).
- The rule "the newest run per workflow and event" keeps one run of several independent runs of the same workflow and event (for example
  the `dynamic` runs of Dependabot updates, or `issue_comment` runs): an older red run is hidden by a newer green one, while
  `check-runs` shows it red. Observed on `cli/cli` at `trunk`: `check-runs` listed each of three Dependabot executions, the Actions way
  kept one per workflow and event.
- Observed 2026-11-17: the same fine-grained token read check runs of public repositories (`via check runs`) and got 403 for a private one.

When both ways are refused the read is `forbidden` and the cause names both rights: `forbidden: the token can read neither check
runs nor Actions runs; it needs Checks: Read (classic token: scope repo) or Actions: Read`. Any other failure (401, 404, rate limit,
5xx, network, an unusable answer, `--timeout`) is `not read` with its cause.

Output. Text, under the repository: `main CI green (main@163ada585103, via check runs)`, `... via Actions runs)`, or
`main CI not read (cause)`; no line without a read. A red head is the attention reason `main-red` (Go: the red action
`main-red`); `--long` explains it without a command. A read that failed is the notice `ci-not-read` (source `ci`), and exit 0.
JSON, per repository `ci_main{read,error,branch,head,class,source}`: `read` true with `branch`, `head` (full commit id), `class`
and `source` (`check-runs` or `actions-runs`); `read` false with `error` (the cause, null when nothing was asked: `--offline`, no
token, no GitHub origin) and the rest null. `schema_version` stays `2` (additive, see "Output for agents"). `status PATH..` reads
the same source and carries it in the JSON, in the notices and in the attention mark; its one-line row adds ` | main red` for a red head and
nothing for the other classes. The requests are counted as `ci-main` in `--stats` and `--verbose`. `ready-check` is unchanged: it
reads check runs only, a 403 there is unknown evidence, and it never reads Actions runs.
Mutations of this source have the prefix `CM`.

Deviations from Go, one by one:

1. The fallback to the Actions runs on a 403 for check runs is new (Go reports `forbidden`); it makes a fine-grained token without `Checks`
   usable for a private repository. An empty list is the class `none` (Go: the same class for an empty check list), and no finding.
2. Go's `ci-main` also says whether the local clone lacks the head of origin (`behind_origin`, the action `main-behind-origin`). Rust does
   not: `status` already shows `behind` of the branch against its upstream, and the commit is not looked up in the object store here.
3. Go reads the default branch from its `repo` source (one read for several sources) and the head with the branch read; Rust reads the
   repository document and the branch itself, two requests per repository, counted as `ci-main`. A default branch whose name has a
   character outside `A-Z a-z 0-9 . _ - /`, a name longer than 200 characters (Go: 255), a `..`, a `//` or a leading or trailing `/` is not read (`not read`), it is never escaped into a path.
4. Go reads at most a bounded number of pages and uses what it got; Rust takes the bounds of the list reads (100 per page, 10 pages, 500
   items, 60 s) and treats a list that hit one of them, or that had a malformed entry, as not read (no class from partial evidence).
   Go's check run text (title, summary, text) is not read: no shape classification of a red head.
5. Go ranks a red head as the red action `main-red` among its priorities; Rust has no priorities: a red head is the reason `main-red`
   (the repository needs attention); the other classes name no reason. A class `pending` is not an attention reason (Go: no action either).
6. The notice: Go groups the repositories of one error code (`forbidden: default branch CI of 2 repositories not read`); Rust has one
   notice `ci-not-read` per repository with the cause in the message (see the notice differences above).
7. Exit code: Go exits 1 when a source could not be read; Rust exits 0 for a failed GitHub read in `status` (the existing rule, see "Exit codes"). Unchanged.
8. Go has the JSON source `ci_main` with `branch`, `head`, `class`, `behind_origin`; Rust has `ci_main{read,error,branch,head,class,source}` per
   repository (no `behind_origin`; `source` and `read`, `error` are new).
9. The two ways see different things (new, see "What the two ways see"): `none` via Actions runs means no Actions run, not no CI; checks of other apps are
   invisible there; the newest run per workflow and event hides older independent runs of the same pair, which `check-runs` lists one by one (Go has
   no second way). Use `source` to tell which one an answer is.
10. The text line: Go prints `remote: ..., main CI red` as part of one `remote:` line of the repository; Rust prints its own line `main CI red (main@..., via ...)`.

### `stw status PATH`

Port of the spike `spikes/stw/status-all` (new behavior without a Go counterpart; the live acceptance of the spike by the owner is not yet done). It replaces `stw status --all` (owner decision 2026-11-13). `PATH` is an argument of
`status`, not a command. It builds on the row of `status` (`collect_local`, `read_remote`, `reasons`, `collect_notices`, the
GitHub client of `src/github.rs`) and on the walk of the nested submodules (`repo::list_repos_depth`, "Submodules: the gitlinks of the index"); what it adds is the
resolution of the paths, the stack of the current branch, the depth bound and the one-line text (`src/status_all.rs`).

Which repositories:

- No PATH: the repositories of the workspace, the default text and JSON of `status` (unchanged).
- A PATH that holds a repository (a `.git` directory or file: a checkout or a linked worktree): that repository and its nested
  submodules, depth-first, as `status` lists a workspace. It is no scan: `--depth` and `--limit` do not apply.
- Any other directory: it is scanned below for directories with a `.git` entry (depth-first, children in byte order of their
  names, symbolic links not followed, a repository is not entered, a bare repository is not found); each repository found is
  listed with its nested submodules. `--depth N` counts the levels below the directory: a child is level 1, a submodule is one
  level below its repository, a found repository at level L lists submodules down to level N. `--limit N` bounds the repositories of the run.
- Several PATHs are listed in the order of the arguments, sorted inside one PATH. A repository that comes more than once (`.` and
  `..`, a symbolic link, a submodule of an earlier repository) is listed once, by its canonical path, at its first mention
  (`kind` and `depth` are those of that mention).
- A PATH that does not exist, is no directory or cannot be read is a usage error (exit 2); nothing is printed.
- The agent-rules checkout (`--rules-path`, default `<workspace>/.agents`) is not listed by a scan or as a submodule unless it is named as a PATH.
  The workspace root for this is `--root`, else the one `status` finds from the current directory, else the current directory; no workspace is needed for a PATH.

The key of a repository, in the text, the notices and the JSON member `path`, is its path relative to the current directory
(`.` for the directory itself, `..` components for a repository next to it; the absolute path when the argument was absolute and the repository lies
outside the current directory). The name (`name` in the JSON) is display only, so two repositories with the same name differ by their key.

Text (the blocks of the default text are not printed):

    status  paths=<args>  repositories=<n>[  depth<=<depth>]

    Repositories (<n>)
    <mark> <key>  <branch>  <sync>  <dirty> stack <chain>  PRs <n> | Issues <n> [(<reason>)]

    Totals  PRs <n> | Issues <n> | ahead <n> | behind <n>

    Notices (<n>)                      (only when n > 0)
      <text>

    <a> of <n> repositories need attention

`depth<=` is printed when a directory was scanned. `<mark>` is `!` when the repository has an attention reason (`reasons()` unchanged), else a blank; `<key>` is padded to 28 characters.
`<sync>` is `<upstream> +<ahead>/-<behind>`, `no upstream`, `upstream gone`, `(unborn)` or `-` (detached; the branch column then
shows `(<head>)`). `<dirty>` is `clean` or the non-zero counts `<n> conflicts <n> staged <n> unstaged <n> untracked`. `<chain>` is the
local upstream chain of the current branch, top-most local parent first (`main<-a<-b`), `-` without a local parent. A local parent
is `branch.<b>.remote = .` with `branch.<b>.merge = refs/heads/<p>` (upstream links only; no other stack source). Because a branch
name may contain `<-`, the JSON `stack.chain` is an array. A repository that cannot be read is `! <key>  (<error>)`. The GitHub
cell is `PRs n | Issues n` (`n+` at a full page), `?` for a failed read with the reason in brackets (`unauthorized`,
`forbidden`, `not-found`, `rate-limited`, `timeout`, `unreadable`, `unreachable`, `error`; read from the message of the client),
`n/a` with `no-github-origin`, `offline` or `no-token`. `Totals`: a number only when every repository with a GitHub origin gave
one (`n+` when a page was full), else `n/a` (the JSON `totals` has the state); `ahead` and `behind` are sums over the rows.
Exit codes as `status`: 1 when a repository cannot be read, 0 otherwise (a failed GitHub read is a notice); an unusable
PATH, a flag that does not fit or more than `--limit` repositories is exit 2.

JSON (`status PATH.. --json`): the document of `status --json` (`schema_version` 2, `command` `status`) plus per repository `kind` (`root`: the repository that was
named or found, or `submodule`), `depth` (0 for the root) and `stack` (`{"chain": [...] | null, "links": n}`; `null` for a detached or unborn head and for a
repository that cannot be read; `links` is the number of branches of the repository with a local parent), and `path` is the key. `options.paths` is the list of the arguments as
given (empty without a PATH, in every `status --json`); `options.depth`, `options.limit` and `options.timeout` (whole seconds of `--timeout`) are there only when a directory was
scanned. `kind`, `depth` and `stack` exist only with a PATH. Notices that only a PATH has: `depth-limit` (source `submodules`, `<key>: submodules below depth <n> not
listed`; source `scan`, `<key>: repositories below depth <n> not listed`, for a scanned directory that has repositories below the bound) `no-repositories` (source `scan`, `<key>: no repository found`, for a scanned directory in which nothing was found; for a bare repository with `(a bare repository has no work tree and is not listed)`; exit stays 0) and `stack-cycle` (source `stack`, `<key>: stack cycle at <branch>`; the chain then ends with the branch that repeats).
A refused gitlink path (`rejected_paths`, notice `rejected-path`) is named below the key of its repository, once.

`stw status` (with and without a PATH) looks into the submodules of every repository: a submodule whose HEAD or work tree differs from the gitlink
of its parent is an `unstaged` path of the parent, and a submodule that cannot be read makes its parent unreadable (its row is an error row).
`sync` and `sync restore` check submodules as before (see deviation 14).

Deviations from the spike and from Go, one by one:

1. GitHub: the product client (`src/github.rs`, token rules, no `gh`) instead of `gh api`; the reasons are read from its messages
   (`classify`, the start of the message only); `not-logged-in` and `gh-missing` do not exist, `no-token` does; a 403 is
   `forbidden` even when GitHub meant a rate limit (the client does not tell them apart; the spike said `rate-limited`); the stop after a rate limit is the one of
   `status` (none for the plain reads; `--review` has its own).
2. Walk: `repo::list_repos_depth`. An uninitialized submodule is not listed (spike: an error row `not initialised`) and a symlinked or
   otherwise refused gitlink path is a `rejected-path` notice and no row (spike: an error row). Neither makes the exit code 1.
3. Name: display only; the key is the path relative to the current directory (spike: the name, the root `.`). A name that occurs twice
   (a root and a submodule `ws`) is no ambiguity any more (this replaces the former deviation 13).
4. Notices: those of `status` (`offline`, `no-token`, `prs-not-read`, `issues-not-read`, `rejected-path`, ...) with their wording,
   not the spike codes (`github-offline`, `github-read-failed`, ...). New: `depth-limit`, `stack-cycle`.
5. Exit code of an unreadable repository: 1, the rule of `status` (spike: 0, its open question 7). Recommendation: keep 1.
6. Exit code above the limit: 2, a usage error (`the paths hold more than <n> repositories`; spike: 1; Go: 1 with the code `sync-limit`). The same
   code as `status` without a PATH; the owner decides whether 1 is wanted (recommendation: keep 2, one rule for every
   command).
7. Text from outside is shown with escapes (`escape_for_text`: `\u{202e}`, `\u{1b}`, `\n`; cut at 300 characters), not as `?`.
8. `reasons` contain `operation:*` (the spike left a running rebase out); the one-line row does not name the operation, only the
   mark `!` shows it.
9. Pull requests and issues are independent: one can be a number and the other `?` (spike: both `?`). `Totals` shows `n+` at a full
   page (spike: the bare number).
10. `--timeout DURATION` is the overall deadline of the GitHub reads (default 90s, not with `--offline`), as `status` (spike: seconds
    per `gh` call, its open question 10, recommendation followed). `options.timeout` is its whole seconds; `options.offline` is
    not added (the spike had it).
11. A PATH is not combined with `--long`, `--icons`, `--worktrees`, `--review`, `--stats`, `--verbose`, `--repo` (the one-line text has no
    place for them). `--fetch` works.
12. `--all` is gone, without an alias (owner decision 2026-11-13). Go has `stw status --all`; Rust has `stw status PATH..` instead. The
    spike keeps its own interface.
13. The scan is new (the spike and Go list the submodules of one workspace): directories are scanned for repositories, `--depth` counts levels of
    directories and submodules together, a repository named by a PATH has no depth bound.
14. `stw status` never leaves the inspection of the submodules in the work tree out (`index_worktree_submodules(None)` is not used for it). The spike and Go
    (`--ignore-submodules=all`) leave a changed submodule out of its parent, and the earlier `--all` of the Rust product did too. Owner decision 2026-11-13:
    only `status` checks in full; the gate of `sync --rebase/--merge` and `sync restore` keep the check of Go (`status::changes_opts(.., ignore_submodule_changes = true)`):
    a changed submodule is no local change of its superproject there, and one that cannot be read does not stop it (unchanged from `3dece37`).
15. The CI of the default branch (`ci-main`, see "CI of the default branch in `stw status`") is read for every repository with a GitHub origin as in `status` without a PATH. Its
    data is in the JSON (`repos[].ci_main`), its failure is the notice `ci-not-read`, a red head is the attention reason `main-red` (mark `!`) and ` | main red` in the row;
    the other classes have no cell (the compact row stays as it was). Neither Go nor the spike has `status PATH`.

Open questions (recommendation each):

- Spike question 4, stack source: upstream links only, as the spike (implemented); a richer source (toml, pull requests) later
  through the same field `stack`. Not decided by the owner (the "Stapelmodell" is open).
- Spike question 7, exit code of an unreadable submodule: exit 1 like `status` (deviation 5). Recommendation: keep.
- Exit code above `--limit` (deviation 6): recommendation 2.
- Should `kind` and `depth` also be in the default `status --json` (additive, the walk is nested there too)? Recommendation: yes,
  later, with `stack`.
- Mutations of this flag have the prefix `AL` (`SA` is taken by `stw add`).

## Backup branches

Branches named `r9-*`, `r10-*`, `pre-restack*`, `archive/*` or `backup/*` are backups. `tidy plan` classifies them
after the proofs (a backup that is an ancestor of the base, has equal patches or a merged pull request is `delete`
by that proof) and before `review`: a backup is never `review`. Without a proof it is `keep` (`backup-recent`)
while the tip commit is younger than N days and `delete` (`backup-older-than-days`, "backup older than N days")
from exactly N days on. `--backup-days N`, default 7, on `tidy plan` and `tidy apply`.

The age is the committer date of the tip commit, not the date the branch was created. Weakness: an old commit
under a freshly created backup name looks old and is `delete` at once. The reflog was measured as an alternative
and is not used: a branch created locally has a `branch: Created from ...` entry that survives `gc`, but a clone
has no local branch and no reflog for it (only remote-tracking refs), a branch created in another clone has none
here, and entries expire (`gc.reflogExpire`, 90 days by default). The plan is reversible with `tidy restore`, within limits: `apply` deletes the ref together with its reflog, so an
unmerged backup commit survives only until `git gc` prunes unreachable objects (about two weeks by default,
`gc.pruneExpire`); after that `restore` reports `missing-object`. Commits that are merged or on a remote stay
recoverable.

## Similar patches in `tidy plan`

A branch can have commits without an equal patch on the base (`equal-patches` fails) although the change is on the base: a cherry-pick with a
conflict resolution, a rebase that touched a line. The similarity stage compares each such commit (the `+` commits of `git cherry`) with the commits of
the base. Method and its limits: `spikes/stw/tidy-similarity/README.md`; code: `src/tidy_similarity.rs` and the stage in `src/tidy.rs`.

Proof `similar-patches`, decision `review`, never `delete`: every commit of the branch without an equal patch has a commit on the base that is at least
`--similarity PCT` (default 95) similar; the detail says `min`, `median` and the threshold. `tidy apply` does not act on it (it acts on `delete`
only), so such a branch is not deleted by `apply`; the plan prints no command for it. The order: after `equal-patches`, `merged-pull-request-head` and
the backup proofs, before `pull-request-merged-branch-moved`, `upstream-gone` and `unmerged`. The gates stay: an open pull request keeps the branch
(`open-pull-request`), a branch checked out in a worktree is `in-worktree` (the `similarity` object stays in the entry), a backup branch is never `review`.
The stage runs only where the proof would be `pull-request-merged-branch-moved`, `upstream-gone` or `unmerged`.

Flags on `tidy plan`: `--similarity PCT` (1 to 100, default 95), `--similarity-low PCT` (1 to 100, default 70, not above `--similarity`; else exit 2),
`--no-similarity` (the stage does not run: the text plan is the plan without it, JSON has `similarity: null`; conflicts with the other two). `tidy apply` has
no flag and plans without the stage. The window of 7 days (a candidate is at most 7 days older than the commit, author time) is a constant. The index of the base
holds the commits from one window before the oldest `+` commit on; a commit whose parent is missing (the boundary of a shallow clone) is left out and counted.

JSON, additive in schema 2, per branch `similarity` (null where the stage did not run: a proof applies, a gate holds, `--no-similarity`):
`class` (`landed`: all `+` commits at or above the threshold; `modified`: all at or above `threshold_low`, one below the threshold; `partial`: one without candidate, without
value or below `threshold_low`; `none`: no commit to judge), `threshold`, `threshold_low`, `window_days`, `plus_commits`, `at_or_above`, `min` and `median` (percent with one decimal or null),
`without_candidate`, `skipped{merges,empty,too_large,shallow}` (`shallow`: commits of the base left out of the index), `bounds{candidates_cut,main_commits_indexed}` and
`commits[{commit,subject,outcome,skipped,candidate,candidate_subject,reason,similarity,similarity_no_path,evidence}]`. `outcome` is `compared`, `no-candidate` or `skipped`
(`skipped`: `merge`, `empty` or `too-large`); `reason` says how the candidate was found (`subject`, `subject-stripped`, `files`, else `none`); `evidence` has up to 5 lines
`{path,sign,line,count_a,count_b}` for a commit below the threshold (untrusted text, escaped). A value that rounds to a threshold but is below it is shown one tenth lower.
Text: one line per `similar-patches` branch with the figures, and `(N of them similar-patches)` in the summary line.
`skipped.merges` and `skipped.empty` are always 0 in the plan: the `+` set holds only commits that have a patch (a merge commit and an empty commit have none), so these two keys
count only where the function of the stage is called with such commits (it is a pure function with its own tests).

## Rebase evidence in `tidy plan`

GitHub's rebase merge writes every commit of a pull request again (new ID, new committer, new parent) and keeps author, author time and subject. The stage maps the
commits of a branch to the first-parent chain of the base by that key and says how the branch landed. Method and limits: `spikes/stw/tidy-similarity/README.md`
(packages 1b and 2b); code: `src/tidy_rebase.rs` and the stage in `src/tidy.rs`.

Proof `rebase-identity`, decision `review`, never `delete`: every commit of the branch (merge commits of the branch do not count) has a commit on the chain with the same author
name, author mail (lower case), author second and subject, the targets are one block in branch order, every commit has a content value of at least 95 % (the owner's floor) and
none is a later version. The detail reads `rebase-identity (3 of 3 commits, run of 3, min 100 %)`. `tidy apply` does not act on it (it acts on `delete` only and plans without the
stage), a plan file edited to say `delete` for such a branch is refused (`the proof no longer holds`), and the plan prints no command. The order: after `equal-patches`,
`merged-pull-request-head` and the backup proofs, before `similar-patches`, `pull-request-merged-branch-moved`, `upstream-gone` and `unmerged` (the existing proofs and the gates keep
their place; a branch in a worktree is `in-worktree` and its `rebase` object stays). A hint of the stage (identity without one run, a partial landing) takes nothing away from the similarity stage: it judges every commit as before, so
the default plan is never weaker than the plan with `--no-rebase-evidence`. A stage that fails for a branch (or the index of the base) leaves the plan standing: `rebase` is null and the detail says `; rebase evidence not computed (<reason>)`.

Flag: `--no-rebase-evidence` (the stage does not run: no `rebase-identity`, `rebase` is null, no pull request commits are read; `landed_via` stays `merge`/`ancestor` for an ancestor and
is `unknown` otherwise; independent of `--no-similarity`). The thresholds are constants, not flags (the floor, the stop after 64 old author times, a window of at most 50 000 chain commits).
The patch IDs come from the `PatchCache` of the `equal-patches` walk; one `SimCache` serves the rebase and the similarity stage. The two stages walk the base twice (all ancestors for the
similarity index, the first-parent chain for the rebase index): they ask for different commits.

Pull request (read only, optional): where a branch would be `pull-request-merged-branch-moved`, `upstream-gone` or `unmerged`, has a merged pull request, and the commits do not already
prove it, one request `GET pulls/N/commits?per_page=100` reads the commits of the pull request (at most 30 requests per repository, the rest is `request-limit`; a full page of 100 is `too-many-commits`). The token
needs `Pull requests: Read` and `Metadata: Read`, no more. Without network, token or with a rate limit the plan uses the commits alone: no error, no change of the exit code; the reason is in
`rebase.api.reason`: `offline`, `no-token`, `no-pull-request`, `rate-limited` (GitHub said so), `request-limit` (the 30 requests are used), `request-failed`, `evaluate-failed` (the local check failed), `too-many-commits`, `not-merged`, `merge-commit-not-in-clone`, `not-on-base`, `not-needed`
(a proof that needs no pull request). The mapping: two parents of the merge commit are `merge`; one parent and the k commits behind it equal to the k pull request commits (author, time,
subject) are `rebase` (strong only when nothing remains and the pull request head is in the branch, else a hint); one parent and an author time equal to `merged_at` plus or minus 2 s are `squash`. `merge` and `squash` are strong only when the branch tip is the pull request head or behind it; a branch with commits after the head gets a hint.

JSON, additive in schema 2: `landed_via {kind merge|rebase|squash|unknown, basis ancestor|commit-map|pr-api|single-commit-map|none, strength strong|hint|none}` always; precedence: the tip is an
ancestor of the base (`merge`), a strong commit mapping (`rebase`), the pull request, one commit with an equal patch but no identity (`unknown`, a hint for a squash), else `unknown`. The
decision never depends on it. Per branch `rebase` (null where not computed: `--no-rebase-evidence`, an open pull request, a merged ancestor, a backup, a failure):
`state`, `strength`, `n`, `mapped`, `by_patch_id`, `by_identity`, `by_similarity`, `unmapped`, `ambiguous`, `contiguous`, `in_order`, `gap`, `runs`, `run_len`, `partial`, `content_ok`, `modified`,
`skipped{merges,empty}`, `window{commits,stop,cut,stopped_at}` (`stop`: `root`, `time`, `window`, `missing`: a shallow clone, the walk ends there), `api{state,reason,pr,merge_commit,merge_parents,k,
tip_matches_run,head_in_branch,kind,strength}`, `mapping[{commit,subject,main,position,method,value,identity,tz_differs,ambiguous,later}]` (`method`: `patch-id`, `identity`, `similarity`, `api`, `none`;
`value` in percent or null) and `remaining[{commit,subject,files,files_total}]` (at most 20 paths, escaped, untrusted text). `summary.rebase` counts the branches with `rebase-identity`; they are
counted in `review`, too. Text: one line per `rebase-identity` branch, `(N of them rebase-identity)` in the summary line, and `; landed via <kind> (<basis>, <strength>)` on a line whose
branch landed by a mapping or the pull request (not for an ancestor).

## Squash evidence in `tidy plan`

A squash merge writes the whole branch as one commit on the base (new ID, one parent, the author time of the merge, mostly `(#N)` in the subject). The squash stage is layer 1 of the
squash metrics (package 5a; specification `scratch/stw-tidy-squash/spec.md`, summary in `spikes/stw/tidy-similarity/README.md`): it reads what is cheap and exact, the merged pull
request of the branch and the subjects on the base. It proves nothing about the content (layer 2, package 5b, will) and decides nothing: **it never changes a decision or a proof**, and a
squash is never `delete`. Code: `src/tidy_squash.rs` (pure functions) and the stage in `src/tidy.rs`; tests `tests/it/tidy_squash.rs` and `tests/it/tidy_squash_plan.rs`; mutations `SQ11` to `SQ56`
(a gap in the numbers: the others belong to packages 5b and 5c).

For which branches: those whose proof is `pull-request-merged-branch-moved`, `upstream-gone`, `unmerged` or `similar-patches` (also where a worktree makes them `in-worktree`). Not for a deletion proof,
an open pull request, a backup branch, a branch without a commit of its own or `rebase-identity` (a strong rebase did not land as a squash): `squash` is null there. There is no flag; the stage
runs with `--offline` and without a token, too.

Pull request (`squash.pr`): from the list of pull requests the plan already holds (no request of its own). The pull request of the branch is the merged one with this head branch whose head is in
the repository itself (`head.repo.full_name` equals the slug of `origin`, compared without regard to case; a fork with the same branch name and a deleted fork do not count; with no slug nothing is
compared); where the plan names a merged pull request already (the proof `pull-request-merged-branch-moved`) that one, else the latest merge time, then the higher number. The existing proofs still
match by branch name alone (a fork pull request with the name of a branch still counts there; open question). `merge_commit_on_base` is true when `merge_commit_sha` is an ancestor of the base and has
exactly one parent; two parents are a real merge commit (false, `merge_commit_parents` 2). `head_is_tip`, `head_in_branch` (the head is the tip or an ancestor of it) and `commits_after_head`
(commits of the branch that are not in the head; only where the head is in the branch). A commit or head that is not in the clone, or a walk that fails on a missing object, leaves the answer null.
Where the proof is `pull-request-merged-branch-moved` and the merge commit is on the base the detail gets `; pull request merge commit <sha7> on <base>, N commit(s) after the pull request head`
(or `, the pull request head is not in the branch`). Without a usable pull request: `state` `not-read` and `reason` `offline`, `no-token`, `no-pull-request` (the list has none for the branch),
`rate-limited` or `request-failed`: no error, no change of the exit code.

Subject (`squash.subject`): the commits of the first-parent chain of the base (the same walk as the rebase index, no diff), non-merge, with an author time at most 7 days older than the oldest commit of
the branch. The candidate, strongest first: (1) the merge commit of the pull request where it is on the base (`basis` `merge-commit`, the sure one); (2) the commit whose subject ends on `(#N)` with
the number of the pull request (`pr-number`); (3) a commit whose title equals the title of a branch commit (`subject`). Title: the subject without a trailing `(#N)`, a leading `<digits>: ` (JBS issue)
and leading `[group]` words; a subject that is only such decoration is never compared. Of several, the oldest (ties by ID); `candidates` counts what the basis found. Further: `pr_number_in_subject`
(the number at the end of the candidate's subject, whatever it is), `subject_equal`, `subjects_total` (non-merge commits of the branch), `subjects_in_body` (branch subjects that are a line `* <subject>`
of the candidate's body), `pull_request_trailer` (the body has `Pull-request: .../pull/N` with the number of the pull request), `window{days,commits,cut}` (commits of the base read; `cut`: the walk
stopped at 50 000). The candidate subject is masked like all commit text. The class of a squash is not set here: the same title with other content ("revert and again") would match.

JSON, additive in schema 2: per branch `squash` null or `{pr, subject}`; `pr`: `state`, `reason`, `number`, `merged` (true where read), `merge_commit`, `merge_commit_in_clone`, `merge_commit_on_base`,
`merge_commit_parents`, `head_is_tip`, `head_in_branch`, `commits_after_head`; `subject`: `candidate`, `basis`, `candidate_subject`, `candidates`, `pr_number_in_subject`, `subject_equal`,
`subjects_total`, `subjects_in_body`, `pull_request_trailer`, `window`. No new key in `summary`. `landed_via` is unchanged (the pull request mapping of the rebase stage already gives `squash`).
Text without `--explain`: only the detail above is new. With `--explain`: `squash: pull request #N merged, merge commit <sha7> is on <base> with 1 parent(s); the head is in the branch, K commit(s)
after it`, or `squash: pull request not read (<reason>)`, then `squash: subject: candidate <sha7> '<subject>' (<basis>), N candidate(s), number #M in the subject, title equal to a branch commit: yes|no, S
of T branch subject(s) listed in its body, Pull-request trailer: yes|no` or `squash: subject: no commit on <base> with a title of one of the T branch commit(s)`, and a line where the walk was cut.

Deviations from the specification (5a), one by one: (1) the detail says `pull request merge commit` where the spec says `squash merge commit`: a one-parent merge commit is also a rebase of one commit
(this owner's repositories merge by rebase); (2) `squash.pr` has `merge_commit_in_clone` (spec: a reason text for a missing object) and the subject object has `basis`, `candidate_subject`, `candidates` and
`window`; (3) a pull request whose `head.repo` is empty counts as a fork where the slug is known; (4) no squash object for `rebase-identity` and for deletion proofs (spec silent); (5) the oldest of
several candidates is taken (the spec leaves the tie open). Not here: the content layer, the classes `squashed-patches` and `partial`, `summary.squashed`, the oracle (5b, 5c), `landed_via` from layer 2.

## Shallow clones

In a clone made with `git clone --depth N` (or `--shallow-since`, `--shallow-exclude`) the commits listed in `.git/shallow` are the boundary: for git they have no parents, whether the
parents are in the object database or not (another ref can hold them), and `git log` stops there. Before this rule `tidy plan` failed for the whole repository with
`An object with id ... could not be found` as soon as a branch reached a boundary commit whose parent is missing (`--depth 3` and `--depth 4` of a small repository; the ledger entry of
2026-11-18). Now the plan is made and nothing is concluded from a part of the history:

- The commits of `base..tip` are those of `git rev-list tip ^base` in this clone (`repo::range_commits`): a listed commit has no parents on either side. The walk of gix hides everything
  behind a listed commit on the `base` side where the objects are there, and so counts fewer commits than git; it is used only in a full clone. `unique_commits`, the upstream
  counts, the walks of the rebase and squash stages and the first-parent index of the rebase stage follow the same view.
- A commit of `base..tip` that is at the boundary (listed, a merge commit too, or with a parent that is missing) is `unreadable` (`shallow.unreadable_commits`): what is behind it is not
  known and a non-merge one has no patch to read. The equal-patches walk (`unmatched_commits`) does not read it, and as long as one exists the branch has no `equal-patches` proof; the
  similarity and rebase judgment of that branch's commits does not run (`similarity` and `rebase` are null). The squash stage is not changed by this rule: it runs where its own rule says
  (it reads heads and subjects, no patch).
- A commit of the base at the boundary is no partner. Where the walk over `tip..base` reached one while a commit of the branch still had no partner (`shallow.base_cut`), a partner may lie behind it
  (`shallow.without_partner` commits have none in the part that is there).
- Where either holds and no other proof applies (the proof would be `unmerged`), the branch is `review` with the proof `shallow-history` and the detail names the cause and `git fetch --unshallow`.
  Never `delete`, never `keep` with `unmerged`. A proof that needs no history (`merged-ancestor`, a merged pull request with exactly this head, `open-pull-request`, a backup name) is unchanged;
  `pull-request-merged-branch-moved` and `upstream-gone` stay `review` with their proof; a stage that finds evidence for a branch with only `base_cut` still replaces `shallow-history` by its own
  `review` proof (`similar-patches`, `rebase-identity`).
- JSON per branch `shallow`: null (a full clone, or nothing hidden) or `{unreadable_commits, without_partner, base_cut}`. `--explain` says `why: review: N of the M commit(s) not in <base> reach the shallow boundary: their patches are unknown, ...`
  or `... have a patch without a partner in the part of it this clone holds; the boundary hides the rest`. `similarity.skipped.shallow` is unchanged.
- Not covered: a partial clone (`--filter`; UNKNOWN); a shallow clone whose branch is merged into the base, with the
  commits on the base inside the clone, is judged as in a full clone.

## Explanation in `tidy plan --explain`

`stw tidy plan --explain` prints under each branch of the text plan (indented by four spaces) the evidence that led to the decision, or the evidence that is missing. It reads
only the data the plan already holds (classes, numbers, reasons): no diff, no request, no other read; it interprets nothing, prints no command and gives no advice. The decision, the
order of the branches and the JSON are the same with and without the flag, so with `--json` the flag changes nothing and is no error. Most of the data is in the JSON already (`similarity.commits[].evidence`,
`rebase.mapping` with `ambiguous`, `later` and `tz_differs`, `rebase.remaining`, `rebase.api.reason`, `rebase.api.head_in_branch`, `landed_via`); only in the text are the table of the pull
request mapping, the list `remaining after the pull request head` and the number of the merged pull request of a branch whose proof a later stage replaced (the counts of the
sentences are derived from `unique_commits` and the `similarity` and `rebase` objects). `tidy apply` has no such flag (exit 2). `tidy plan` has no `--long`; the flag does not
need one. Without the flag the text is byte for byte the text it was. The same plan gives the same text.

Lines, in this order (a section is left out where its stage did not run: `--no-similarity` leaves out `similarity:`, `--no-rebase-evidence` leaves out `mapping:`, `remaining:` and `pull request:`):

- `why: <decision>: <sentence>`: the proof that decided, or the proof that is missing (`keep: 2 of 4 commit(s) have no patch partner on origin/main`). Where `similar-patches` or
  `rebase-identity` took the place of `pull-request-merged-branch-moved` or `upstream-gone`, the sentence ends with `; pull request #N was merged and the branch moved on` or
  `; upstream <name> is gone` (the plan keeps the number of the merged pull request for this; the JSON does not carry it).
- `landed via: <kind> (<basis>, <strength>)` where the branch landed by a mapping, the pull request or an ancestor.
- `delete` branches get these two lines and nothing more (the proof is clear). The sentence counts only what the plan proves: `N commit(s) not in <base>` includes merge and empty commits,
  `P of the N commit(s) not in <base> have a patch without a partner there` counts only commits that have a patch.
- `similarity: class <class>, N commit(s) judged, A at or above 95 %, lowest x %, median y %, W without candidate`, then a line where the base was read from a shallow clone (`N commit(s) of the base were left out of the index (shallow boundary)`) or the candidate list of a commit was cut
  (`the list of candidates of a commit was cut at 100`), then for each commit below the threshold (at most 20, then
  `… N more commit(s)`): `<sha7> '<subject>' best candidate <sha7> <value> % (<reason>; without path <value> %)`, `no candidate` or `skipped (too-large)`, and below it up to 5 evidence
  lines `<sign> <path>: <line>` (a line of at most 120 characters, cut with `…`).
- `mapping: strength <s>, M of N commit(s) mapped (patch-id a, identity b, similarity c), contiguous|not contiguous (gap g), in order|not in order`, then for each mapped commit (at most 20,
  then `… N more`): `<sha7> -> <sha7> <method> <value> %`, with `(later version, hint only)` where the branch commit was committed after its commit on the base, and `(time zone differs)`; a commit whose key matches more than one free commit on the base and no neighbour decides is `<sha7> -> ambiguous (more than one commit on the base has the
  same author, time and subject)` and does not count as mapped; a
  `modified: a commit is at least 70 % and below 95 % like its commit on <base> (hint only)` line where a value is between the two thresholds.
- `remaining: N commit(s) without a commit on <base>` and one line per commit with its files (at most 20 commits, at most 20 paths over all of them).
- `pull request: PR mapping not read (<reason>)` with the reason of `rebase.api.reason`; where the pull request was read: `pull request: #N <kind> (<strength>), k commit(s), the head is in
  (or not in) the branch` and, for a rebase only, `, r commit(s) of the branch come after it` (only the commit mapping of a rebase lists them), then `hint only: ...` or `strong, but ...`
  with the reason why the pull request mapping is a hint (commits after the head; for a merge or a squash `the branch has commits after the pull request head`, derived from
  `head_in_branch` because the commits are not listed; the head not in the branch) and that it gives no proof of its own (only the commit mapping makes `rebase-identity`), the `pull request mapping:` table (method `api`) and
  `remaining after the pull request head`.
- `squash: ...` (where the branch has a `squash` object; no flag leaves it out): the pull request line (`squash: pull request #N merged, merge commit <sha7> is on|is not on <base>|is not in the clone|has N parent(s), not one`,
  `; the head is the tip of the branch|is in the branch, K commit(s) after it|is not in the branch|could not be checked`, or `squash: pull request not read (<reason>)`), the subject line and, where the walk over the base was cut, one line saying so. Layer 1 only: no proof.

All text from a commit (subjects, paths, lines) is escaped (control, invisible and direction characters; bytes that are not UTF-8 as `\xNN`), and so is the name of the upstream.

## Oracle of `tidy plan`

The oracle compares `tidy plan` with real repositories and with what GitHub says (package 4; the numbers are in `spikes/stw/tidy-similarity/README.md`). It is read-only and starts nothing but `git`, `gh` and `cargo`:

```
ORACLE_STW=target/release/stw bash tests/oracle/tidy_oracle.sh /abs/path/outside/the/git/tree
```

The directory (absolute, without `..`, outside a Git tree or ignored by it) takes the clones, the raw GitHub data (`gh api`, stops below 500 remaining requests), the results and the report `bericht.md`; nothing else is written and
nothing is deleted unless `ORACLE_CLEAN=1`. Variables: `ORACLE_HUNK` (slug or path of a repository with many commits, default `flix/flix`), `ORACLE_REBASE` (slugs with merged pull requests, default the three repositories
of the owner), `ORACLE_RAWDIR` (same guards as the output directory), `ORACLE_LOCK` (lock file for cargo, warns where its directory is missing), `ORACLE_PRS`, `ORACLE_HUNK_PRS`, `ORACLE_HUNK_COMMITS`, `ORACLE_MATRIX_COMMITS`. The report has the hunk oracle (the keys of gix against `git diff -U0 --no-renames`), the matrix
(the same commits against every `diff.algorithm`, with and without the indent heuristic), the rebase oracle (every merged pull request as a branch: strong `landed_via` rebase onto the commits GitHub wrote, or `equal-patches`;
a pull request into another branch is named `stacked`) and the cost of `stw tidy plan` (seconds, peak memory). The same tests run alone with `STW_ORACLE_HUNK_REPO`, `STW_ORACLE_REBASE_REPOS` and `STW_ORACLE_OUT` set:
`cargo test --test it oracle_tidy -- --ignored --nocapture` (they skip with a message without the variable; a finding is a result, `STW_ORACLE_STRICT=1` makes it a failure). The comparisons are pure functions in
`src/tidy_oracle.rs` with ordinary tests in `tests/it/tidy_oracle.rs`; mutations `OR1` to `OR35`. No new deviation from the Go behavior.

## tidy apply and tidy restore

`tidy apply` acts on a fresh plan, or on `--plan FILE` (the output of `tidy plan --json`). With `--plan`, only the
`delete` entries and the `keep`/`in-worktree` entries that name a removable `worktree` are candidates, and each
one is verified again against a fresh plan before anything happens: the branch must still point at the planned
commit and the proof must still hold (a proof that needs GitHub holds only without `--offline`); for a worktree
entry the branch must still be checked out in the planned worktree path. A branch still held by a dead
registration is skipped (the file has no field for registrations, so `apply --plan` does not remove them).
The backup age is judged by the `--backup-days` of the `apply` run (default 7), not the one the plan was made
with. Otherwise the entry is skipped with the reason. `review` and `keep`
branches, the base branch, branches checked out in a worktree that stays, and remote state are never touched.
`tidy plan` shows the plan and changes nothing; `tidy apply` executes it, with no confirmation flag. The safety is the
manifest written first, the fresh check of every entry and the proof rules above.

Order per repository: verify, write the manifest (fsync of file and directory), remove dead registrations, remove
worktrees, delete branches (the ref is deleted only if it still has the planned commit). If the manifest cannot
be written nothing is deleted (exit 1). A failure in one branch does not stop the others (exit 1).

Worktree places: `<repo>/.worktrees/<name>` is the normal place. A worktree anywhere else is marked `outside
.worktrees/` in the text plan and `outside_worktrees_dir: true` in JSON. It is removed on the same proof as any other
(below), the mark only tells where it was. The text plan lists every kept branch whose worktree is removable, also
without `--all`.

Worktrees: a linked worktree whose branch would be `delete` is removed only if it is not the main worktree, not
locked, has no staged, unstaged or untracked file and no running operation, contains no checked-out submodule,
its `.git` file points at its registration, no other worktree (or any other git repository, registered or not)
lies inside it (an agent's worktree under `.worktrees/` of a worktree would go with it), and its tip is in the base branch or its upstream (nothing unpushed).
A registration whose checkout path contains `..` (what `git worktree add --relative-paths` leaves, or a hand-made
`gitdir`) is never followed: it is not removed, not judged clean or dead, and its branch is not deleted. It is
reported once, as `skipped`, with its path shown without `..` (display only: nothing is read, tested or removed
through the normalized path, because `..` after a symbolic link names another directory than the lexical one).
Only the worktree directory and its registration are removed (ignored files in it go with the directory, as
`status` does not count them as changes). Otherwise
worktree and branch stay, with the reason. Unlocked registrations whose directory is gone are removed and locked ones are reported, but only when `apply`
uses a live plan: a run with `--plan FILE` leaves registrations alone (the file has no field for them). A removed worktree is not recorded for `restore` (only the branch is); its path is in the manifest.

`.worktrees/`: every `tidy apply` makes sure the repository's `info/exclude` (in the common git directory, found
through gix) lists `.worktrees/`, so agent worktrees under `<repo>/.worktrees/` never show up as untracked. The
line is appended once (the file is opened for append; bytes already there, CRLF included, stay as they are) (`.worktrees/`, `/.worktrees/`, `.worktrees` and `/.worktrees` count as present), existing content
is kept, the `.gitignore` is never touched. `--json`: per repository `exclude` {path, action (`added`, `present`,
`failed`), reason}, null for a repository without a work tree.

Manifest: JSON Lines, one file per run, `<DIR>/<run>.jsonl`, written before the first deletion. `DIR` is
`--manifest-dir`, else `~/Library/Application Support/stw/manifests` (macOS) or
`$XDG_DATA_HOME/stw/manifests` / `~/.local/share/stw/manifests`. The run ID is `<ms>-<pid>-<n>` and sorts by time.
Each line: `manifest_version` (1), `run`, `repo` (path), `branch`, `sha`, `proof`, `time` (seconds), `worktree`
(path or null).

`tidy restore` reads the newest run (or `--run ID`; `--branch NAME` limits it) and creates each branch at its
`sha`. It is idempotent: a branch that exists at the `sha` is `already-present`; one at another commit is
`not-overwritten` (never moved); a commit that is gone from the repository is `missing-object`. Exit 1 unless
every entry is restored or present. A run that was killed after the manifest and before the deletions restores
as `already-present`. The reflog entry (`stw: tidy restore <run>`) uses the identity rules below. Left behind by
a delete: `branch.<name>.*` configuration (upstream) stays, so a restored branch keeps its upstream.

### tidy history

`stw tidy history [--limit N] [--manifest-dir DIR] [--json]` is the port of the Go `tidy history` (read-only; Go lists the stored
contract-3 journals). Here the journals are the manifests of `tidy apply` above, and their format is not changed: `tidy restore` reads
the same files. One row per manifest, newest run first (run IDs sort by time, so the first row is the run `tidy restore` takes
without `--run`): `run`, `time` (modification time of the file, `YYYY-MM-DDTHH:MM:SSZ`), `mode` (`local-refs`, `unknown` when not read),
`deleted` (entries of the manifest: the branches the run recorded before it deleted them; a run killed before the deletions
counts too), `restorable` (of those, how many `tidy restore` could create now: the repository opens, the branch does not exist, the commit
is in it) and `status`: `complete`, `incomplete` (not checked because of a bound), `unreadable` (not JSON or not a manifest entry; listed with
0 counts). The entries are data: `repo` must be an absolute path, the branch a valid `refs/heads/` name, `sha` 40 lower-case hex digits,
otherwise the entry is not restorable. Files that are no manifests (other extension, a directory, a symbolic link, a hidden
or unusual name) are not listed. A directory that does not exist is an empty history. Nothing is written and no repository is changed; no
`git`, `gh` or `ssh` is started.

Bounds, as in Go, each one visible: at most 4096 manifests are scanned (`scan_truncated`), `--limit N` (1 to 1024, default 256; a value
outside it, a repeated flag or an unknown flag is a usage error, exit 2) rows are shown, at most 65536 entries are checked against the
repositories, and the check stops after 30 seconds. A bound that was reached is a marker `{unit, shown, total}` with `unit` `journals`,
`objects` or `checked-journals` (text: `truncated: <unit> <shown> of <total>`). `--json`: `schema_version` 2, `command` `tidy-history`,
`read_only` true, `manifest_dir`, `engine`, `status` (`ok`), `total`, `scan_truncated`, `entries[{run, time, mode, deleted, restorable,
status}]`, `truncated[]`. Text: `N journal(s) in <dir>, newest first`, one line per row, the markers; text from outside is escaped.

Deviations from Go: (1) the journals are the `tidy apply` manifests (one JSON Lines file per run, outside the repository), not the plan and
journal files of the contract-3 state directory; `--contract`, `--state-dir`, `PATH`, `tidy restore --list` and the contract-4
(administrative registration prune) history are not ported, `--manifest-dir` is the state directory; (2) the fields `plan_id`, `retained`,
`recovery_refs`, `decision_kinds` and `run_mode` have no counterpart (a manifest has no plan, no recovery refs, no reviewer decisions and no run
mode); `run` is the run ID; (3) `status` cannot say `incomplete` for a killed run, since the manifest is written before the first deletion
and has no completion mark; (4) the order is by run ID (Go: modification time); (5) a missing directory is an empty history (Go: an error);
(6) the unit of the entry bound is named `objects` as in Go but counts manifest entries; the 30 second budget and the bounds are the Go values.

Output: `--json` has `command` `tidy-apply` / `tidy-restore`, `run`, per branch `action`
(`deleted`, `skipped`, `failed`) and `reason`, `manifest` (path), worktrees and registrations.
It carries state and results only, no commands. Limits: a file created in a worktree after the check and before
the removal is lost with it; the age rule for backups is only as good as the commit date (above).

## GitHub access

Token order: `GH_TOKEN`, `GITHUB_TOKEN`, `~/.config/gh/hosts.yml` (`oauth_token`), macOS keychain (the item `gh`
creates). The token is never printed or logged. Without a token everything works except pull request data
(and the notes say so) and fetching private repositories (the error says to set `GH_TOKEN` or `GITHUB_TOKEN`).
Only `github.com` remotes are used for REST; the fetch offers the token to `github.com` over https only, as user
`x-access-token`. HTTPS from `ureq` uses the platform trust store (on Linux, the system CA bundle). Certificate
verification stays on. The Mozilla roots bundled with ureq are not used. The check class of a head needs `Checks: Read` (`ready-check`, and `status` first; a classic token has it with the scope `repo`; `Checks` is not offered for fine-grained tokens, which get 403 for check runs of a private
repository), or `Actions: Read`, which `status` (not `ready-check`) tries when GitHub answers 403 for check runs (see "CI of the default branch in `stw status`"). Both ways also read `/statuses`, which needs `Commit statuses: Read`. `STW_GITHUB_API_URL` points the REST calls at another base URL (tests, GitHub Enterprise).

## stw add

`stw add OWNER/REPO [PATH]` (`REPO` alone takes the owner of the workspace's GitHub origin; `PATH` defaults to
`projects/REPO`, relative to the workspace root, which must be the top level of a git repository with a `.git`
directory). The root is `--root`, else the superproject of the repository of the current directory, else that
repository (a workspace without `.gitmodules` is fine), and only without any repository `$STW_ROOT`. It never creates a repository (that stays in `whr-add`) and needs read access only.

1. gix clones `https://github.com/OWNER/REPO.git` into `PATH`, a directory this run creates itself, without a work
   tree. The token (if any) reaches only the gix transport, through the same token-only closure as `sync`; it is
   offered for `github.com` over https only. The user's and the system git config are not read for the clone.
2. The clone is checked before a file is written: its own config sets no key that runs a program, `origin` is exactly
   the canonical URL, there is a commit, and no `.gitattributes` of the tree (any case, any depth, the NTFS short name)
   and no `info/attributes` sets a `filter` (git-lfs and git-crypt repositories are refused for now).
3. Through the runner (`via: git`): `reset --hard` writes the work tree, `submodule add -- <url> <path>` and
   `submodule absorbgitdirs -- <path>` bind it (no transport: `GIT_ALLOW_PROTOCOL=none`), and one commit
   `add REPO submodule` holds `.gitmodules` and the gitlink only. The commit is never signed
   (`commit.gpgsign=false`, `tag.gpgsign=false`), uses no attributes file (`core.attributesFile=/dev/null`) and takes
   author and committer from the git config; without `user.name` and `user.email` the run stops with exit 2 before it
   touches anything.

Refused with exit 1 and the workspace unchanged: a workspace config that sets a key of the list in
`git_runner::refused_by_add` (filter, gpg, signing, diff/merge/tool commands, program-starting `core` keys, aliases,
pagers, trailers, per-remote programs, `submodule.*.update`, plus the runner's own list), a workspace for which
`git check-attr filter diff merge -- .gitmodules` (work tree with index fallback, and `--cached`; through the runner)
prints anything but `unspecified`, prints something unexpected or fails (stw does not parse attribute files for this),
a symlinked `.gitattributes`, an attributes file of the clone (any `.gitattributes` of the tree, `info/attributes`)
that names the attribute `filter` in any spelling (no pattern matching: `filter=x`, `-filter`, `"*"filter=x`, in a
macro), an existing `PATH` or index
entry, a symlink in `PATH` or `.git/modules/PATH`, an existing `.git/modules/PATH`, a registered submodule or
`[submodule "PATH"]` section, a `.gitmodules` that is a symlink or has uncommitted changes, an empty repository. Names
and paths: `[A-Za-z0-9._-]` for names (not `.`, `..`, `-x`); `PATH` relative, `[A-Za-z0-9._/-]`, no empty, `.`, `..`
or `-x` component, none of `.git`, `.git.*`, `.gitmodules`, `.gitattributes`, `.gitignore`, `.mailmap` in any case
(exit 2). A failure before the commit restores `.gitmodules`, the index and the workspace config (saved right before
the first git step, so a change another process made during the clone stays), `.git/modules/PATH` (only when this
run's absorb created it; `.git/modules` is checked for symlinks and for an existing `PATH` again right before the
absorb) and the directories this run created. After the commit nothing is rolled back. `--dry-run` runs every check and
changes nothing. `--json` has `read_only` (true for a dry run), `clone_via` (`gix`) and `via` (`git`), both null for a
dry run. A clone error masks the token; an unknown repository and a refused token both read "not found or no access". Evidence and open points: the spike
`spikes/stw/add/README.md` (Q1-Q8). Not verified: see "Not verified".

## Remote kinds and the git runner

`sync` (a run) fetches `origin` by remote kind:

| Remote | Fetched by | `via` | On failure |
| --- | --- | --- | --- |
| `ssh://`, `user@host:path`, local path, `file://` | installed `git fetch` through the runner | `git` | `fetch: failed`; `fetch: unsupported` when git is missing, too old or lacks an option |
| `https://` | gix, no program; with `sync --prune` the installed git through the runner (README "sync --prune") | `gix`, with `--prune` `git` | `fetch: failed` |
| `http://`, `git://` | gix, no program | `gix` | `fetch: unsupported-remote` (message ends with `use git directly for it`) |
| other transports (`ext::`, `fd::`) | nobody | none | `fetch: unsupported-remote` |

gix would start `ssh` or `git-upload-pack` itself for the first row; stw never hands such a URL to gix
(`tests/it/remote_kinds.rs` puts fake `ssh`, `git-upload-pack` and `git` first on `PATH` and checks that only the runner
starts `git`). `via`, and `git_error` (`not-found`, `version`, `unknown-option`, `timeout`, `output-limit`, `exit`,
`spawn`, `repository`, `refused-config`) appear in the repository entry of `sync` and in `agent_rules` only when a fetch ran. Credentials in a remote URL
and GitHub tokens are never printed. `status` without `--fetch`, `tidy plan` and `sync --plan --offline` never fetch; `sync --plan` fetches like a run. `git://` was not exercised
against a server (UNKNOWN beyond the error path).

Runner rules (`src/git_runner.rs`):

- Fixed argument vector per operation (`Op`), never composed from free text: `git -C <absolute repo> -c core.fsmonitor=false
  -c submodule.recurse=false -c fetch.recurseSubmodules=false -c gc.auto=0 -c maintenance.auto=false
  -c protocol.ext.allow=never -c core.hooksPath=/dev/null fetch --quiet --no-tags --no-recurse-submodules
  --upload-pack=git-upload-pack origin`. No shell, no repository hook, no repository-chosen upload-pack program. An empty,
  relative, missing or non-repository path is refused; the process working directory is never used.
- Repository config that runs commands is refused: `core.sshCommand`, `core.gitProxy`, `core.askPass`,
  `core.alternateRefsCommand`, any `credential.*`, `url.*.insteadOf`/`pushInsteadOf`, `remote.origin.receivepack`,
  `remote.origin.vcs`, and any `include`, `includeIf` or `hook` section (git follows include files, so a refused key
  could hide in one) in the local or worktree config give `fetch: unsupported` with `git_error: refused-config` and the
  key name; the same keys in the user's own config are allowed (a repository that sets `credential.helper` locally cannot
  be fetched over ssh by stw; use git directly).
- Environment allowlist: `PATH`, `HOME`, `USER`, `LOGNAME`, `XDG_CONFIG_HOME`, `TMPDIR`, `SSH_AUTH_SOCK`, `SSH_AGENT_PID`,
  `GIT_SSH`, `GIT_SSH_COMMAND`, `GIT_SSH_VARIANT`, `GIT_ASKPASS`, `SSH_ASKPASS`, `SSH_ASKPASS_REQUIRE`, `DISPLAY`, the proxy
  variables and `SSL_CERT_FILE`/`SSL_CERT_DIR`; `PATH` is passed on with its absolute entries only (an empty or relative entry would let git run `ssh` from the worktree). Everything else is dropped: all other `GIT_*` (`GIT_DIR`, `GIT_WORK_TREE`,
  `GIT_INDEX_FILE`, `GIT_CONFIG_*`, `GIT_EXEC_PATH`, `GIT_TRACE*`, author and committer variables), `EDITOR`, `VISUAL`,
  `PAGER`, `LANG*`, `LC_*`, `GH_TOKEN`, `GITHUB_TOKEN`. Fixed: `LC_ALL=C`, `GIT_TERMINAL_PROMPT=0`, `GIT_OPTIONAL_LOCKS=0`,
  `GIT_PAGER=cat`, `GIT_EDITOR=true`, `GIT_ALLOW_PROTOCOL=file:ssh:git:http:https`, `GIT_CEILING_DIRECTORIES` = parent of
  the repository. The user's Git configuration and identity stay in effect; stw sets no author or committer.
- Time (120 s) and output (1 MiB per stream) are bounded; the process group depends on the terminal. Without a terminal on stdin or stderr (agents, CI) git runs in its own process group, and a timeout or output limit kills the whole group, helpers holding the pipes included. With a terminal git stays in stw's foreground group, as plain git: prompts (ssh host key, passphrase) and Ctrl-C work, and a timeout or output limit kills git only, so a helper it started can outlive it. ssh options are the user's (`BatchMode` is not forced): without a terminal a host-key prompt cannot be answered and blocks until the time limit. `git --version` is read once
  per run; the minimum per command is in "Git versions" below. A spawn that fails with ETXTBSY is retried.
- Errors are typed (`GitError`) and redacted: URL user info, GitHub token shapes, `Authorization:` values and the values of
  `GH_TOKEN`/`GITHUB_TOKEN` never reach output.
- Not done: Windows (executable lookup, path form), push.

## Git versions

Only `src/git_runner.rs` starts `git`; everything else (status, tidy, restack plan, lint pr, review lines, the merge
simulation, patch-id, the fast-forward and the journal of `sync`) runs in gix and needs no installed Git. Every runner call
checks `git --version` against the minimum of its `Op` (`Op::min_version`) before it starts anything; an older Git, or a
version text that cannot be read, is refused (fail closed, `git_error: version`, result `unsupported`) with
`stw needs Git X.Y or newer for <command> (found Z)`.

| Command | Minimum Git | Deciding feature | Evidence (RelNotes) |
| --- | --- | --- | --- |
| `status`, `tidy`, `restack plan`, `lint pr`, `lint pr-text`, `review lines` | none (no Git call) | gix only | `tests/it/no_binaries.rs` |
| `lint review` with `--base` and `--head` | 2.30 | `git diff` and `git patch-id --stable`, the fail-closed value; without those two flags no Git call | `tests/it/lint_files.rs` |
| `status --fetch`, `sync` (default), `sync --prune`, `--no-prune` | 2.30 | `fetch --prune --no-prune-tags` (2.17); 2.30 is the fail-closed value, formally 2.17 | 2.17.0 |
| `sync --submodules`, `sync restore`, `add` | 2.36 | `-c core.fsmonitor=false` is a boolean only since 2.36 (before, the value names a hook command); these ops read the index | 2.36.0 (config/core) |
| `workspace` | 2.46 | `GIT_NO_LAZY_FETCH` prevents local checkpoint and resume from fetching missing objects | 2.46.0 |
| `absorb` | 2.46 | `GIT_NO_LAZY_FETCH` is honored since 2.46; an older git would fetch the missing objects of a partial clone; the one call is `git blame` | 2.46.0 (backends.md of the Go repository) |
| `review carry-over` (with and without `--fetch`), `prove` | 2.41 | `GIT_ATTR_SOURCE` (2.40 ignores it silently, so a `-diff` attribute could hide a change from `range-diff`) | 2.41.0 |

The 2.36 bound is documentary: with Git 2.35.3 no test of `add`, `sync --submodules` or `sync restore` failed (48 failures,
all range-diff or merge-tree tests that need 2.40/2.41; calls seen: check-attr 146, reset --keep 32, reset --hard 19,
submodule add 19, absorbgitdirs 17, commit 17, submodule sync 5, update 4), so no failure was observed there.
The 2.41 bound was shown: with 2.40.1 a worktree `.gitattributes` with `* -diff` made `range-diff` report "equal" for a
changed commit; with 2.41.0 and 2.43.0 it reported "not equal".

Test-only requirements (not needed at run time):

| Where | Requirement | Feature |
| --- | --- | --- |
| `tests/it/replay.rs` (oracle) | 2.40 | `git merge-tree --write-tree --merge-base` (2.38 for `--write-tree`) |
| `tests/it/carryover*.rs`, `prove` tests | 2.41 | real `range-diff` with `GIT_ATTR_SOURCE` |
| test repositories | 2.28 | `git init -b` |
| `tests/oracle/*.sh` | 2.23 | `git switch` |

Distributions (not checked against their packages): Ubuntu 24.04 ships Git 2.43.0 and covers every command. Debian 12 ships
2.39.x and loses `review carry-over` and `prove` (shown by the version check with a 2.39.2 build, not by the Debian package);
`sync --submodules`, `sync restore` and `add` stay available there. The suite ran green with 2.43.0 and 2.41.0, and with
2.40.1 the carry-over and prove tests fail at the version check (they need 2.41) and the rest passes.

## GitHub reads for review and readiness (internal API)

`stw::github::reads` (`stw status --review` uses `issue_comments`): `Client::issue_comments`, `pr_reviews`, `check_runs`, `commit_statuses`. Every request is counted in the stats (`stw status --stats`) and none starts after the `--timeout`.
REST GET, 100 per page, under a `Budget` (10 pages, 500 items, 16 KiB text per body, 60 s). A list that hit a bound, lost a
page or had a malformed entry is `Observed` with `incomplete: Some(reason)`; `Read::is_complete()` is true only for a fully
read list, so partial or unreadable evidence cannot make a pull request Ready. 401, 403 (no permission, for example Checks)
and 429/exhausted rate limit are `Read::Denied`, 404 is `NotFound`, other failures `Failed`; none is a crash. GitHub text is
untrusted: cut, then escaped reversibly (`\\`, `\n`, `\r`, `\t`, `\u{hex}` for control, zero-width and direction
characters). Slug and SHA are validated before they enter a path. Tests: `tests/it/github_reads.rs` with the fixtures in
`tests/fixtures/github/` (see its README for which are recorded and which are hand-written).

## Review state from comments (internal API, M3)

`stw::review` (used by `stw status --review`, below). Pure functions on the comments read by
`github::reads`; nothing is posted and nothing is marked Ready.

- Grammar (first line of the comment, nothing else on it): `Review: <CLEAR|FINDINGS> head=<40 lower-case hex> patch-id=<40 hex> pr=<n|none>` and
  `Carried-over: from=<40 hex> to=<40 hex> patch-id=<40 hex> pr=<n|none>`. Fixed keys and order, single spaces, no Markdown, tab or CR.
  `n` is a decimal number from 1 without leading zeros. `pr=none` is a local review (scope `Local`); dotagents is to adopt
  `pr=([1-9][0-9]*|none)`. A line that reads like a review line but breaks the grammar is never accepted: it is the finding
  `unparsed-review-variant`. Other text is not a review line.
- `evaluate(scope, head, comments, trusted)` and `evaluate_read(.., &Read<Comment>, ..)`. Status: `clear`, `clear-carried`,
  `findings`, `no-review`, `unverified-author`, `unknown`. Trust is a list of logins (case-insensitive, whole login, an empty
  login never; empty list: nobody); `author_association` is not a gate. Only comments of trusted logins decide. Lines of other
  authors are findings (`untrusted-author`); if they are the only lines for the head the state is `unverified-author`, never
  `clear`. Lines about another head that play no part in the decision are `stale-head`.
- A trusted line with another `pr=` (or `pr=none` in a pull request scope, a number in the local scope) is the finding `wrong-pr`.
  If it names the head (a review of it or a carry-over to it) and is newer than the decision, or there is no decision, the state is
  `unknown`; it also ends any carry-over chain that passes through the head it names.
- The latest line for the head by `(created_at, id)` wins: a later `FINDINGS` cancels a `CLEAR`, a later `CLEAR` replaces `FINDINGS`.
  A carry-over to the head counts if the latest line before it that names `from` is a `CLEAR` review, or a carry-over that is
  itself valid. An invalid carry-over to `from` is skipped and the search continues with the earlier lines that name `from`
  (so an older CLEAR of `from` still carries); a trusted line of another pr that names `from` ends the search with an invalid
  chain. A chain is invalid (finding `invalid-carry-chain`, with the reason in `detail`) when: no such source exists; every link
  must declare the patch-id of the root CLEAR (`patch-id-mismatch`; the declared values are compared, nothing is recomputed, M4
  does that from git); a link returns to a head the chain already cleared (`repeated-head`); a `FINDINGS` review of any head of
  the chain (the root and every intermediate head, not the final head) was posted after that head was cleared
  (`member-findings`); more than 16 links. The final head's own later `FINDINGS` simply wins as the latest line. Chains are
  computed iteratively, so thousands of carry-overs are fine.
- `unknown` (with `reasons`): the read is partial, denied, not found or failed; a body has a malformed escape; a trusted line has no
  usable `created_at`; a trusted `unparsed-review-variant` or a trusted wrong-pr line naming the head is newer than the decision (or
  there is no decision). It is never read as `no-review`.
- Provenance per line: comment id, author, `created_at`, `updated_at`, `edited`. An edited comment is only flagged: a decisive or
  chain comment with `edited` counts normally and the flag is in the result. `ReviewState::to_json()` is the stable JSON
  (version 1); `stw status --review` prints it as `review_state`.
- Tests: the dotagents corpus `tests/review_corpus/review-line-examples.md` (copy of `docs/review-line-examples.md`, dotagents
  c7c343c), `tests/it/review_lines.rs`, data-driven cases `tests/review_states/*.json` (`tests/review_states.rs`), `tests/it/review_read.rs`.

## Review state in `stw status --review`

`stw status --review` reads, for each open pull request, its conversation comments (one list read of `GET /repos/OWNER/NAME/issues/N/comments`, 100 per page) and derives the state with `review::evaluate_read` for the exact head commit of the pull request (`head.sha`). Nothing is posted. Trust: the logins of the comments whose `author_association` is OWNER, MEMBER or COLLABORATOR (as in Go) are the trusted authors; lines of anybody else are findings and never decide.

- Text: the pull request line ends `[<head branch>, review <state>, <merge state>]`; the states are `clear`, `clear-carried`, `findings`, `no-review`, `unverified-author` and `?` (`unknown`); `review not read` for a pull request beyond the limit. Under the pull requests of a repository one line counts them (`reviews: 1 clear, 2 no-review, 1 ? (hints: 1 clear-not-ready)`, zero counts left out). A pull request with hints ends `... CLEAN] {clear-not-ready, base-not-default}`. `--long` adds under each pull request the decisive comment (`decided by comment ID of LOGIN at TIME: <the parsed line>`), `carried through N links, from ...`, the findings (at most 5: `finding <kind>: comment ID of LOGIN`, with the cut and escaped line for an unparsed variant) and why a state is `?` (`unknown because: ...`). `--long` also explains each hint (`hint clear-not-ready: ...`, `hint base-not-default: the base is X, the default branch is Y`). Only the parsed line, validated hex and numbers, is ever printed from a comment; any other comment text is not. Logins, titles and every other text from outside are escaped and cut.
- A comment read that failed (401, 403, 404, 429, other status, unreadable answer), hit its bound (page budget, 500 comments, time) or the `--timeout` gives `review: "unknown"` (`?`), never a verdict, and one notice per repository: `NAME: review state not read for N pull requests (#NUMBER: reason)`. Pull requests beyond the limit of 200 give `review: null` and a notice. Exit stays 0 (as for the other failed remote reads). `Counts` and `totals` do not change.
- JSON (additive, `schema_version` stays 2): per pull request `review` (the status label or null) and `review_state` (null, or `ReviewState::to_json()` version 1: `pr`, `head`, `status`, `decisive`, `chain`, `findings[{kind,comment_id,author,detail}]`, `reasons`; plus `read_error`, null for a complete read; `detail` and every text from the forge are escaped and cut at 120 characters), `hints` (array of hint names, null without `--review`), `base` and `default_branch` (escaped and cut at 200 characters, null when the list had none); per repository `pull_requests.review` (`{states, not_read, limit}`) and top level `options.review`.
- A rate limit answer (429, or 403 with an exhausted limit, `Retry-After` or the words "rate limit"/"abuse detection" in the body) stops the comment reads that have not started, in all repositories: they are `?` and one notice `rate limit reached, N reads skipped (review state ?)` names them (deviation 9). Reads in flight end on their own.
- Requests: one read per pull request (up to 6 requests for a very long thread), at most 16 at a time, at most 200 pull requests per run; with `--stats` or `--verbose` they count as `pulls`.

Deviations from the Go `--review` (`internal/pulls/review.go`, `internal/reviewstate`; Go cases `state_test.go`):

1. States: Go has `none`, `findings`, `clear`, `clear-carried`, `clear-stale`, `unparsed`; Rust has the M3 states `no-review`, `findings`, `clear`, `clear-carried`, `unverified-author`, `unknown`. A CLEAR of another head is `no-review` with the finding `stale-head` (Go: `clear-stale`); an unparsable or wrong-pull-request line is `unknown` or a finding (Go: `unparsed`); a failed read is `unknown` (Go: the review stays unset and the source of the repository is an error).
2. Grammar: the M3 parser, not the Go regular expression (exactly 40 hex patch-ids, no CR, `pr=` of any width from 1 without leading zeros; see `docs/migration-plan.md`, deviations D1 to D8). A line for another pull request is the finding `wrong-pr`, not `unparsed`.
3. The commit date of the head is not read: Go reads one commit per pull request whose comments name the head and ignores comments older than that commit. Rust has no second read; the verdict is bound to the exact head id instead (M3), so a comment can only name a commit id that existed when it was written. A comment that was edited later still counts at its `created_at` (Go ignores a comment older than the head commit): an old CLEAR that its author edited afterwards to name the head is `clear`, and only the flag `edited` (shown by `--long` and in the JSON) marks it. That grants nothing new: only the author or someone with write access can edit a comment (on GitHub that includes OWNER, MEMBER and COLLABORATOR, the trusted associations), and a later FINDINGS wins.
4. Carry-over: the chain rules of M3 (up to 16 links, patch-ids of all links equal, a `FINDINGS` of a chain head ends the chain) instead of Go's up to 8 links; the patch-ids are compared as declared (`stw review carry-over` proves them from git, `stw status` does not).
5. Trust is per login: a login is trusted when one of its comments has the association OWNER, MEMBER or COLLABORATOR (Go: per comment). Go's `ignored` count and the hint `foreign-review-ignored` are the findings `untrusted-author` and the state `unverified-author`.
6. Long threads: Go reads the first page and the last five pages and marks the state `truncated` (hint `truncated-review-unknown`); Rust reads from the first page up to the budget (500 comments) and then says `unknown` with the reason. A verdict on a page that was not read is never guessed.
7. Hints: `clear-not-ready` (state `clear` or `clear-carried` and the pull request is a draft) and `base-not-default` (the base branch differs from the default branch) are ported with Go's conditions and names; they need no read of their own. Differences: the default branch comes from `base.repo.default_branch` of the pull request list, not from a repository read (Go: `RepoInfo`), and a pull request without `base.ref` or without a default branch has no `base-not-default` (Go: only an unknown default branch has none); the comparison is exact, as in Go. Not ported, on purpose: `no-final-review`, `truncated-review-unknown` and `foreign-review-ignored` (the states `no-review`, `unknown` and `unverified-author` already say it), `ready-mergeable` (needs the verdict groups of the Go readiness check, which Rust does not have here, so the Go counts per group do not exist either). Not ported, follow-up candidate: `parent-merged-retarget` (one more read of the closed pull requests per repository with stacked pull requests).
8. Output: Go prints one count line per repository (`reviews: 1 clear, 2 none (hints: ...)`) and the JSON `pulls.data.review{states,hints}`; Rust prints the same kind of count line (with the two hints), the state and the hints of every pull request in the text and in the JSON (`review`, `review_state`) and `pull_requests.review{states,not_read,limit}`. The JSON documents are not the Go ones (see the migration plan).
9. Limit: Go reads the rows of the pulls cycle within `--limit` (default 200, drafts included, newest first); Rust reads at most 200 pull requests per run, in repository order, each repository newest first (a repository has at most 100 open pull requests in the list), with no `--limit` flag. Go reads four repositories at a time and the comments of a repository one after the other; Rust reads at most 16 comment lists at a time. Go stops every further request after the first rate-limit evidence; Rust does the same for the comment reads: after a 429 (or a 403 with `x-ratelimit-remaining: 0`, with `Retry-After`, or with "rate limit" or "abuse detection" in the first 4096 bytes of the body, as in Go) seen by any request of the run, no comment read starts any more, in any repository; the reads in flight (at most 15 besides the one that was limited) end on their own, the ones not started are `?` with the read error `not read: GitHub had reached the rate limit, ...` and are named by one notice for the run (`rate limit reached, N reads skipped (review state ?)`), the exit stays 0. Differences: Go also stops the other sources of the run (releases, issues, CI), Rust stops only the comment reads of `--review` (the pull request and issue lists are read before them); Rust does not wait or retry, as Go.
10. Go exits 1 when a review read failed (a failed remote source); Rust prints the notice and exits 0, as for the other remote reads.

## Carry-over check: `stw review carry-over` (M4)

`stw review carry-over --pr <N|none> [--fetch] [--checkout PATH] [--json] OLD_BASE OLD_HEAD NEW_BASE NEW_HEAD` (read-only; posts
nothing; `stw` does not read the review it is about). Full lower-case 40-hex ids. All three conditions must hold: (1) equal patch-id of
`BASE...HEAD`, old and new, each against its own base (in process, `via: gix`); (2) no merge commit in `NEW_BASE..NEW_HEAD` (gix);
(3) `git range-diff OLD_BASE..OLD_HEAD NEW_BASE..NEW_HEAD` shows `=` for every commit at the same position (`via: git`, Git 2.41+).
A fourth condition, added with `stw prove` and checked only when the three held: the patch-id of every commit is equal, old against new at the same position (`range-diff` shows only "Binary files differ" for a binary change, so a binary file with other contents in an intermediate commit passed the three; Go compares the commits themselves, so this is nearer to Go; JSON `commit_patches{state,differing,via}`, reason `commit-patch-differs`). A step that could not run is `unknown`; the answer is then no. Only for a yes the output ends with
`Carried-over: from=<old head> to=<new head> patch-id=<40 hex> pr=<n|none>` (a line of the M3 grammar). Exit 0 yes, 1 no or a
step could not run (result printed), 2 usage. `--fetch` fetches `refs/pull/N/head` of `origin` into `refs/stw/carry-over/pr-N`
(`via: git`, for an ssh (also `ssh+git://`, `git+ssh://`), scp-like, `file://` or local-path `origin` only, with `GIT_ALLOW_PROTOCOL=file:ssh` so that a `url.<base>.insteadOf` of the user's config cannot turn it into https; an http, https or git:// `origin` is refused typed (`transport`, exit 1, answer no) before git starts, because the https fetch stays with gix and is not built for this refspec; a `core.sshCommand` of the user's config runs as with `FetchOrigin`; no tags, no FETCH_HEAD, empty refmap) and requires it to equal `NEW_HEAD`.
The range-diff step pins what could hide a change (empty attribute source, no `diff.ignoreSubmodules`, size limit, context) and refuses,
typed unsupported, a repository config with `diff.external`, `diff.<driver>.textconv|command` or an active `info/attributes`.
`git range-diff` is told `--no-notes`. It also says no where Go says yes when only the author of a commit changed, and for an
identical patch twice in a range (range-diff pairs it across). JSON schema 2 (`pr`: number or `"none"`; `via` per step; `read_errors`). Deviations from the Go command and the mapping of its 46
tests: header of `tests/it/carryover.rs`; plan: `docs/migration-plan.md`, "Next slice".

## Local rebase proof: `stw prove` (new)

New behavior without a Go counterpart (AGENTS.md "Scripts first, then product": the owner accepts it live; recipe in the
scratch directory of the workspace). `stw prove ALT NEU [--base BASIS] [--old-parent REV] [--pr N|none] [--json] [--root PATH] [--repo NAME]`
(read-only, local, no pull request and no network). `ALT` is the old branch or head, `NEU` the new one: one revision each (a
branch name, a tag, a commit id; not a range). It proves that `NEU` is `ALT` moved by a rebase and nothing else. The first line
of the output is the verdict, `proved` or `not proved`; then one line per check with its state (`ok`, `failed`, `unknown`),
its reason and `via`; for `proved` the last line is the finished line
`Carried-over: from=<ALT head> to=<NEU head> patch-id=<40 hex> pr=<n|none>` (`--pr N`; default `none`).

| # | Check | Reason when not `ok` | via |
| --- | --- | --- | --- |
| 1 | `range-diff`: `git range-diff OLD_BASE..ALT NEW_BASE..NEU` shows `=` for every commit at the same position | `range-diff-not-equal`, `range-empty` | `git` |
| 2 | `commit-patches`: the patch-id of every commit is equal, old against new at the same position (`range-diff` shows only "Binary files differ" for a binary change, so it cannot tell two binary contents apart); asked only when checks 1, 3 and 4 held, else `not-run` | `commit-patch-differs` | `gix` |
| 3 | `patch-id`: equal patch-id of `BASE...HEAD`, old and new | `patch-id-differs`, `patch-id-empty` | `gix` |
| 4 | `merge-commits`: no merge commit in `NEW_BASE..NEU` | `merge-commits` | `gix` |
| 5 | `files`: the same set of changed paths (name-only, without rename detection, both sides the same way) | `files-differ` | `gix` |
| 6 | `identity`: author and committer of every new commit are `user.name <user.email>` of the Git configuration (read only; the email is compared without regard to case) | `identity-mismatch`, `identity-unknown` | `gix` |
| 7 | `trailers`: every `Co-Authored-By` line of an old commit is in the new commit at the same position | `trailer-missing` | `gix` |
| 8 | `session-links`: no session link in the text of a new commit (the rule of `lint pr`) | `session-link` | `gix` |

`--old-parent REV` (same name as in `restack plan` and the Go `restack plan`) is the old parent of `ALT`, the old base: for a child branch whose parent was rebased (`git rebase --onto NEWPARENT OLDPARENT ALT`: `--old-parent OLDPARENT --base NEWPARENT`). `REV` must be an ancestor of `ALT` (`old-parent-not-ancestor`) and not `ALT` itself (`old-range-empty`); the old range is then `REV..ALT` instead of `merge-base..ALT`. Without the option nothing changes; a failed range-diff then hints at both options.

Checks 1 to 4 are the machine of `stw review carry-over` (`carryover::check`: range-diff, per-commit patch-ids, cumulative patch-id, merge commits), not a second implementation. The old base is
the merge base of `ALT` and `NEU`. Without `--base` the new base is the same commit (a rewrite on the same base: amend, reword,
reorder, squash); with `--base BASIS` it is `BASIS`, the base `NEU` was rebased onto, which must be an ancestor of `NEU`
(`base-not-below-new`, with a hint to name the new parent with `--base` when `--old-parent` made the old base the new base too; `ends.base_source` is then `old-parent`). An empty old or new range is `range-empty` (git range-diff needs two non-empty ranges). Without `--base` for a rebase onto a new base the new range holds the commits of the new base, the range-diff
fails and a hint names `--base`. Problems before any check: `unknown-revision` (a revision that names nothing or no commit),
`old-parent-not-ancestor`, `old-range-empty`, `range-empty`, `no-merge-base`, `same-commit` (`ALT` and `NEU` are one commit: nothing to prove, a deviation from the Go carry-over, which
accepts identical heads), `base-not-below-new`; the verdict is `not proved` and the checks do not run. Merge commits of the old range are left out of the pairing (range-diff and the per-commit patch-ids leave them out too); a linearized `NEU` can be proved, and the trailer line says how many were left out. A step that could not run is `unknown`
and the verdict is `not proved`; there is never a line then. `identity-unknown`: no `user.name` and `user.email` in the Git
configuration; stw never sets them. The identity check is as strict as `lint pr`: a rebased commit of another author is not proved.
Git boundary: only `range-diff` runs through the installed git (`Op::RangeDiff`, Git 2.41+, `via: git`; gix has no range-diff);
revisions, merge base, commit walk, trees, patch-id (`src/patch_id.rs`), identity and trailers are read in process. Commit
subjects, names and paths from the repository are untrusted: text and JSON carry them escaped (`escape_untrusted`).
The repository is the one of the current directory, which must belong to the workspace (`--root`, else the repository or
superproject of the current directory, else `$STW_ROOT`; the root and its checked-out submodules that `list_repos` accepts, never a
path that `.gitmodules` names outside the root), or `--repo NAME`; without a repository around the current directory the workspace must
have exactly one. Exit 0 `proved`, 1 `not proved` (result printed), 2 usage.
JSON (schema 2): `schema_version`, `command: "prove"`, `verdict`, `proved`, `pr` (number or `"none"`), `request{old,new,base,old_parent}`,
`ends{old_head,new_head,old_base,new_base,base_source,old_base_source}` (`old_base_source`: `merge-base` or `flag`; `base_source`: `merge-base`, `flag` or `old-parent`; null when the revisions did not
resolve), `input[]` and `checks[]` (each `{id,state,via,reasons[],detail,items[]}`), `reasons[]` (all reasons that apply),
`hint` (the `--base` hint or null) and `line` (the `Carried-over:` line for `proved`, else null).

## `stw absorb plan`

**Purpose.** After review feedback or a late fix, the staged hunks belong back into the commits that introduced the lines.
`stw absorb` computes the PLAN of that: for every staged hunk the commit that owns it (or why none does), the `fixup!`
commits that would fold the hunks in (one per target commit, hunks named) and how many commits would get new SHAs. It folds
nothing: no add, commit, rebase, ref, file, object or index refresh, no network, no hook, no editor. `--apply --plan-sum SUM`
folds that plan: the checksum is recomputed and a mismatch writes nothing (exit 1, `plan-sum-mismatch`). A match copies
HEAD to `refs/stw/backup/<run>/1` and appends that update to the op log before any fixup object, writes one fixup commit
per target with gix, then runs `git rebase --autosquash` (`GIT_SEQUENCE_EDITOR=true`, hooks off, as every runner call).
The output names `stw oplog undo <ref> --to <old> --expect <new> --keep --yes`. `--apply` without `--plan-sum` is
`plan-sum-required` (exit 1), not an unknown flag. New behavior without a Go counterpart; the specification, the 64 counter-proofs
of the Python spike and the comparison with git-absorb are in `spikes/stw/absorb/README.md` (continuation of Spec 018,
"Fix up"); decisions D-20261119-07 to D-20261119-11 and D-20261119-12 to D-20261119-20 (`dayboard/DECISIONS.md`): git-absorb is parked (-12); the later apply is native in Rust, fixups written with gix, rewrite by `git rebase --autosquash` with `GIT_SEQUENCE_EDITOR=true`, an op log snapshot first, git-absorb is never called, the BSD-3 notice stays (-13); no flag is the read-only plan, writing only with `--apply` and the plan checksum (Q1, -14); nothing staged is exit 1 `nothing-staged` (Q2, -15); an ambiguous hunk is skipped, `--ambiguous-newest` takes the newest (Q4, -16); one fixup per target commit (Q8, -17); a partial clone with missing objects is `partial-clone-unsupported`, no lazy fetch (Q11, -18); Q3, Q5, Q6, Q7, Q9, Q10, Q12 and Q13 follow the recommendations of the spike README (-19); order: live acceptance of the spike first, this read-only port in parallel, the apply only after the owner accepted the spike and the answers (-20).

```
stw absorb [--plan] [--apply --plan-sum SUM] [--base REV] [--max-stack N] [--ambiguous-newest] [--human | --json] [--root PATH] [--repo NAME]
```

| Flag | Meaning |
| --- | --- |
| (none), `--plan`, `--human` | the plan as text, one line per fact (the default); `--plan` is accepted next to `--apply` |
| `--apply --plan-sum SUM` | fold the plan whose `plan_sum` is SUM. A mismatch writes nothing. The output names the backup ref and `stw oplog undo` |
| `--json` | the plan as one JSON document (not with `--human`) |
| `--base REV` | the stack is `REV..HEAD`; REV must be an ancestor of HEAD; the stack limit does not apply |
| `--max-stack N` | at most N commits in the stack, default 10 (the default of git-absorb; its `absorb.maxStack` is not read) |
| `--ambiguous-newest` | a hunk whose lines come from several commits goes to the newest of them (the behavior of git-absorb) instead of being skipped as `ambiguous` |
| `--root PATH`, `--repo NAME` | as for `prove` and `restack plan`: the repository is the one of the current directory unless `--repo` names another of the workspace |

There is no `--offline`: nothing leaves the machine. Git only; `jj` and GitButler checkouts are refused.

**The method** is that of git-absorb 0.9.0. The stack is the first-parent chain of HEAD that no remote-tracking branch
contains (and, without `--base`, no other local branch), cut at the first merge commit, the first commit whose author is not the
`user.name`/`user.email` of the Git configuration, or the limit. Staged changes only (the index against HEAD; the work tree is
never read) and only modified text files: added, deleted, renamed (a delete plus an add, rename detection is off), type-changed,
submodule, symbolic link, binary and mode-only changes are listed under `files_skipped`. Each hunk (a zero-context Myers diff
without the indent heuristic) is moved before the changes of the stack commits, newest first; the target is the newest commit
whose changes do not commute with it (`basis`: `overlaps-lines`, or `file-added` when that commit created the file). The removed
lines are blamed in HEAD (`origins`: evidence, not the decision). A hunk that cannot be placed is `skipped` with a `reason`:
`no-candidate` (commutes with every stack commit and its removed lines, if any, come from the stack; pure insertions land
here), `ambiguous` (the removed lines come from two or more commits; the target that would be chosen stays named),
`conflict` (the removed lines are not where the commuted hunk says in the target's blob), `outside-range` (the lines come from a commit beyond
the stack), `published` (from a commit a remote-tracking branch contains), `foreign` (from a commit by another author;
priority published, foreign, outside-range).

**Output** (`schema_version` 1; keys are added, never renamed). Keys in this order, all always present: `schema_version`,
`command` (`absorb`), `mode` (`plan`), `head`, `branch`, `index_digest` (SHA-256 of `git ls-files -s -z`), `base` (as given or
null), `max_stack`, `stack` (`sha`, `subject`, newest first), `stack_end` (`root`, `merge`, `foreign`, `limit`, `base`,
`published`, `branch`), `hunks` (`id` `h1`.., `path`, `old` and `new` `{start, count}` as in `@@ -a,b +c,d @@`, `assignment`
`absorbed`|`skipped`, `target` `{sha, subject}`|null, `basis`, `reason`, `origins` `[{sha, subject, lines}]`), `files_skipped`
(`path`, `reason`: `added`, `deleted`, `typechange`, `submodule`, `symlink`, `binary`, `mode-only`, `other`), `fixups` (`target`,
`subject` `fixup! <subject>` or `fixup! <full SHA>` when the subject is not unique in the stack, `hunks`), `rewrite` (`count`,
`commits`: the old SHAs of the target commits and all their descendants, oldest first; `ref`; `new_shas`: null, the successors
are unknown before the fold), `summary`, `effects` (`writes: []`, `index_refresh`, `network`, `hooks`, `prompts`: all empty or
false), `plan_sum`. `plan_sum` is the SHA-256 over the whole document (ref, index digest, hunks with assignment and reason,
fixups, rewrite), the options and the paths unmasked, as sorted-key compact JSON with ASCII escapes (the encoding of the spike,
`absorb::canonical_json`); a fold would recompute the plan and refuse on another checksum. The unmasked paths add nothing a
changed path does not already change in the document (the index digest covers them); they are in the sum as specified. Paths,
subjects and the branch name are masked in text and JSON: control, bidi and zero-width characters, line and paragraph
separators and every byte of an invalid UTF-8 sequence become `?`. The text lists the same facts (`absorb plan (read-only): head
.. on .. , stack n commit(s), ends at ..`, `h1 path -3,1 +3,2 -> sha7 subject (basis)` or `.. skipped: reason`, `file path
skipped: reason`, `fixup! subject <- h1, h3`, `rewrite: n commit(s) get new SHAs (oldest sha7); nothing was written`) and names no
command. The same state gives the same bytes (no time, no random, `LC_ALL=C` for blame).

**Exit codes and error codes.** 0: a plan was produced, also when every hunk is skipped. 1: a refusal with a typed code (on stderr
as `stw absorb: <code>: <message>`; with `--json` also the document `{"schema_version":1,"command":"absorb","error":{"code","message"}}`
on stdout). 2: usage error (unknown flag, `--plan-sum` without `--apply`, `--max-stack 0`, no repository). `--apply` without `--plan-sum` is exit 1 `plan-sum-required`.

| Code | Meaning |
| --- | --- |
| `nothing-staged` | the index equals HEAD (git-absorb exits 0 here) |
| `unmerged-index` | the index has unmerged paths |
| `not-owned` | HEAD itself is by another author |
| `published` | HEAD itself is contained in a remote-tracking branch |
| `no-commits`, `detached-head`, `no-identity`, `bad-base` | no commit yet; HEAD on no branch; `user.name` or `user.email` unset; `--base` is no commit or no ancestor of HEAD |
| `environment-override` | `GIT_DIR`, `GIT_WORK_TREE`, `GIT_INDEX_FILE`, `GIT_COMMON_DIR` or `GIT_OBJECT_DIRECTORY` is set and not empty |
| `unsupported-backend`, `ambiguous-backend`, `invalid-metadata` | a `.jj` directory (in the working directory or above it up to the repository root) or GitButler metadata (`.git/gitbutler`, `refs/gitbutler/*`, `refs/heads/gitbutler/workspace`); both; a `.jj` that is no directory. Detected first; no fallback to Git writes |
| `partial-clone-unsupported` | the repository is a partial clone (`remote.*.promisor` or `extensions.partialclone`) and a read failed: objects are missing; nothing is fetched, ever |
| `blame-failed` | `git blame` of the removed lines failed (no git, too old, an error) or the path is not UTF-8 text: no plan without that evidence, never an empty `origins` |
| `git-failed` | any other unreadable object, ref or index |

**Git boundary and safety.** Everything is read through gix; the one `git` call is `git blame --porcelain --no-textconv
--ignore-revs-file= -L a,b <sha> -- <path>` (`Op::BlameLines`, `--literal-pathspecs`, `GIT_NO_LAZY_FETCH=1`, `GIT_OPTIONAL_LOCKS=0`,
no hook, no pager, floor git 2.46), once per hunk with removed lines. gix itself runs `git config -lz --show-origin ...` once to
find the system configuration of the installation, and the runner `git --version`; both read-only. A refusal at backend
detection runs no blame. External diff, textconv, hooks, the repository's diff settings and the work tree do not reach the plan
(tests). The repository tree and the index file are byte-equal before and after, also with a stale stat cache.

**Limits, named.** Only the first-parent chain; a hunk is checked against the stack commits' blobs, not replayed (the fold is
not built). The attributes `-diff`, `binary` and textconv are not consulted (a file git would show as "Binary files differ"
because of an attribute is planned as text; NUL in the first 8000 bytes still means binary, as in git). Commit messages are
read as stored (`i18n.commitEncoding` is not applied, no mailmap), the subject is the first paragraph on one line (`%s`). A
remote-tracking ref that is no commit is ignored (`git log --remotes` would fail). A repository with `skip-worktree`, sparse or
intent-to-add entries, linked work trees and bare repositories are not specially treated. A path that is not UTF-8 text has no
blame: a hunk that removes lines there is `blame-failed`; pure insertions there are planned. The set of commits a remote-tracking
ref contains is computed in full for every plan (cost grows with the history).

**Deviations**, one by one. From git-absorb 0.9.0 (inherited from the spike): (1) a hunk whose removed lines come from several
commits is skipped as `ambiguous`, `--ambiguous-newest` restores git-absorb (the worst silent error is half a hunk in one commit);
(2) a modified symlink is skipped, git-absorb treats it as text; (3) nothing staged is exit 1 `nothing-staged`, git-absorb exits 0;
(4) commits a remote-tracking branch contains are never targets (`published`), git-absorb does not read them; (5) `absorb.maxStack`
and `--no-limit` are not read; (6) per hunk reasons instead of one warning; (7) one fixup per target commit in the plan, git-absorb
one per hunk unless `oneFixupPerCommit`; (8) the diff is gix's Myers without indent heuristic, git-absorb uses libgit2: no
disagreement in 237 + 28 + 1 comparisons, unproven in general. From the Python spike: (9) reads run in process; `git` is only
`blame` (the spike ran plumbing: `rev-parse`, `log`, `diff-index`, `diff-tree`, `cat-file`, ...); (10) `--repo` is a workspace
repository name and `--root` is the workspace, as in the other stw commands (the spike took a path); (11) JSON is one compact line
(the spike printed `", "` and `": "` separators; the documents are equal after parsing, key order and `plan_sum` included);
(12) `--human` is the text; (13) the search for `.jj` starts in the current directory (the spike: the `--repo` path); (14) a branch
is excluded from the other local branches by its exact name (the spike passed it to `git log --exclude=` as a pattern; ref names
cannot hold glob characters, so no difference is observable); (15) `base` in the document is masked like the other text (the
checksum has it unmasked); (16) a path that is not UTF-8 text has no blame, see above (the spike handled it); (17) attributes
(`-diff`) are not consulted, see above (the spike let git decide); (18) the output of one `git blame` is bound by the runner (1 MiB per stream): a hunk that removes about 15,000 lines or more gets `blame-failed` (the spike has no bound and plans it); (19) a file whose only staged change is the mode and that is binary is `binary` here, the spike says `mode-only`; (20) a sparse index is not handled (see the limits): the spike did not handle it either, no deviation to the spike, named because the result is wrong, not refused.

**License.** The commutation test (`src/absorb_commute.rs`) and the stack and target rules (`src/absorb.rs`) are a translation of
git-absorb 0.9.0 (BSD 3-Clause, Copyright (c) 2018, Stephen Jung) through the accepted spike. Both files carry
`SPDX-License-Identifier: EUPL-1.2 AND BSD-3-Clause`; the license text is `LICENSE-git-absorb-BSD-3` in this directory and must stay
with every redistribution, in source and in binary form (documentation or other materials). The test files `tests/it/absorb_*.rs`
carry the same line where they take cases from the spike.

**Cost.** Worst case of the spike README (a synthetic history of 2450 commits, a 3000-line file whose lines 0 to 999 every commit
changed, a staged insertion at line 2000 with no candidate, `--base` the root: all 2449 commit diffs are read): the spike 10.0 to
10.4 s (one `git diff-tree` per commit); this port, debug build (the one `cargo test` runs), `--base <root>`: 6.24 to 6.30 s,
27 MB peak (3 runs, this host, aarch64); the plan document is equal to the spike's (checksum included). Release build (`opt-level 3`, `lto` switched off for the measurement, as the profile of `Cargo.toml` says thin): 0.49 to 0.62 s, 16 MB
(3 runs, same host and history; the git-absorb figure of the spike README is 0.64 to 0.73 s). A stack of 10 (the default) reads ten commit diffs. The work is the Myers diff of each commit's version of the file; the
set of commits of the remote-tracking branches is the other cost, in a repository of 1462 commits it does not show.

**Tests and counter-proofs.** `tests/it/absorb_*.rs` (115 tests: units, the 91 cases of the spike as CLI tests on throwaway
repositories, the read-only proofs), `tests/it/sha256.rs` (4), `tests/it/git_runner_blame.rs` (5); each test commit was red against the stub
before the implementation followed. The oracle `tests/it/absorb_oracle.rs` compares the plan with the output of the spike on 27
named scenarios and 60 pseudo-random repositories, in the default mode, with `--ambiguous-newest` and with `--max-stack 3`
(237 documents equal, key order and `plan_sum` included; skipped, with a message, when `python3` or the spike is missing);
a second comparison on 14 commits of a clone of this workspace (28 staged changes, the Rust plan against the spike): 28 equal.
git-absorb itself is not installed on this host; the comparison with it is the one in `spikes/stw/absorb/README.md` (162 of 163).
Mutations `AB1` to `AB99` in `tests/mutation/mutate.sh`: 99 of 99 killed at their named test (the first run left five alive: `AB34` was an equivalent mutant and is now the limit that hides the commit proving the limit was hit, `AB64` was equivalent because the git directory is the common directory in an ordinary repository and now removes both lookups, `AB70` and `AB71` named a test that does not reach the added side of a replacement, `AB80` named the oracle test, which cannot run in the mutation copy because the spike is not there, so the subject has its own test). The oracle test is therefore not part of any kill.

**UNKNOWN (not checked).** git older than 2.47.3 (2.46 is a claim from the release notes); macOS; the `extensions.partialclone`
key alone (no test; the promisor key is tested); a type change of a file inside a stack commit; the `conflict` reason from a real
history (it needs a hunk the commutation test places and the blob check refuses: not constructed, tested as a verdict
and as `lines_match` only; a mutation that always says consistent is not killed); an installed jj or GitButler (markers only);
linked work trees, bare repositories; a sparse index (observed: `files_skipped` then names directory entries as `added` and `deleted`, e.g. `b/`, and `index_digest` differs from the SHA-256 of `git ls-files -s -z`; a typed refusal `sparse-index-unsupported` is a behavior fix, not done, in the ledger); `GIT_DIR` and the other overrides in a caller's environment are
refused, not served; a very large diff is read in full by gix (the output of `blame` is bound, deviation 18); the comparison with
git-absorb versions other than 0.9.0.

**Observation.** After the watch port landed there are two SHA-256 implementations in the crate: `src/sha256.rs` (the checksum of this plan) and `watch::statedir::sha256`. They are not merged here (ledger).

## Stack preview: `stw restack plan` (M1/M2: port plus new)

`stw restack plan [BRANCH...] [--onto BASIS] [--old-parent REV] [--json] [--root PATH] [--repo NAME]` (read-only: no ref, no file,
no object on disk, no network, no `git` process; a test compares the whole directory including `.git` before and after). It says
before a rebase of a stack which level goes through cleanly, where a conflict arises (the files) and which level only inherits it.
Two parts:

* **Port** (single branch): the prediction of the hidden Go `restack plan --branch --old-parent --new-parent`, here
  `stw restack plan BRANCH --old-parent REV --onto BASIS`: the commits of `REV..BRANCH` are replayed in memory onto `BASIS` (the M1
  engine, `src/replay.rs`, gix `merge_trees`); predicted conflicts with the paths and kinds, commits that become empty, a merge or root commit refused,
  an old parent that is no ancestor refused, unrelated histories refused. Go cases: `go_*` in `tests/it/restack_plan.rs`.
* **New** (no Go counterpart; owner go, live acceptance open): the whole stack. `A B C` are local branches, bottom to top, each sits on
  the one before (`not-stacked` otherwise); level n is replayed onto the new tip of level n-1. **Without a branch the chain is derived
  from the local branches** (owner decision "explicit plus auto-detection"): it starts at the checked-out branch and goes down; the
  branches whose tip is already part of the base are no levels; below each branch the nearest local branch is the next level. Anything
  ambiguous is refused, never guessed, and the explicit spelling is suggested (`instead: stw restack plan b b2 --onto main`):
  two names at one commit, a fork (two candidates, none below the other), a detached `HEAD` (`detached-head`), the checked-out branch
  already in the base (`nothing-to-restack`), no common history with the base (`no-merge-base`). Branches above the starting branch
  are not in the plan; a note names them. The local default branch (`main`, from `origin/HEAD`) is never a level, also when it is ahead of `origin/main` (a note says so);
  checked out, it is refused (`default-branch`).

`--onto` is the new base (any revision); default: the default branch of the repository (`origin/HEAD`, else `origin/main`,
`origin/master`), else a usage error. The old base of the bottom level is `--old-parent` or the merge base of the bottom branch and the base;
the old base of the next level is the old tip of the one below.

Output: the first line is the verdict, `plan: clean` or `plan: conflicts at A (files x, y)` (also `plan: refused at B (...)`,
`plan: unknown at B (...)`, `plan: refused (code: ...)` for an input that stops before any level), then per level the branch, the old
and new base, the number of commits and the state:

| state | meaning |
| --- | --- |
| `clean` | every commit replays without conflict (commits that become empty are dropped, as `git rebase` does) |
| `conflict` | a commit conflicts; the files and kinds are listed; the replay stops there |
| `inherited-conflict` | a level below has a conflict; this level depends on how it is resolved and is not computed (a deviation from the spike, which went on with conflict-marker commits) |
| `already-upstream` | every commit is empty after the replay and its patch-id is in the base: `git rebase` drops them |
| `became-empty` | every commit is empty after the replay, the base has the change in another form |
| `refused` | a merge or root commit in the level |
| `unknown` | the result would be clean but the replay is not safe here (below), or a bound (500 commits) was reached |
| `not-computed` | above a level that is `refused` or `unknown` |

A refusal before any level (no levels computed) prints no level count.
Exit 0 for `plan: clean` (the states `clean`, `already-upstream`, `became-empty` only), 1 for conflicts, refused or unknown (the plan is
printed), 2 usage (also: no `--onto` and no default branch, a branch that does not exist). JSON (schema 2, `command: "restack-plan"`):
`read_only`, `plan` (the first line), `verdict` (`clean`, `conflicts`, `refused`, `unknown`), `source` (`explicit`, `auto-detected`),
`onto{rev,commit}`, `levels[]` (`branch`, `tip`, `old_base`, `new_base` or null, `commits`, `state`, `reasons[]`, `files[]`,
`conflicts[{commit,path,kind}]`, `dropped`, `steps[{commit,status,subject}]`), `summary{levels,clean,conflict,inherited_conflict,became_empty,already_upstream,refused,unknown,not_computed}`,
`refusal{code,message,suggestion}` or null and `notes[]`. Branch names, paths, subjects and reasons come from the repository and are
escaped (`escape_untrusted`) in text and JSON.

**Limits of the replay, and the difference to a real `git rebase`.** The plan is a prediction by an in-memory approximation (M1,
header of `src/replay.rs`), tested against `git rebase` as the oracle in disposable repositories (`tests/it/restack_plan.rs`: the plan says
clean exactly when `git rebase` succeeds, and a predicted conflict names the files git leaves unmerged). It is not a run of `git rebase`:
* Directory renames: gix has none; the engine reports a conflict where git renames a directory silently or conflicts (conservative:
  it may say conflict where git succeeds, never clean where git conflicts, in the cases tested).
* Rename detection and diff algorithm are pinned (`merge.renames=true`, `merge.renameLimit=7000`, myers). 7000 is the default of git for a merge (documented, and measured
  with `git merge-tree` on 1050 inexactly renamed files: git pairs them, a limit of 1000 does not); gix counts like git (sources times destinations
  above the limit squared skips the inexact pairing). A repository configuration with another `merge.renames`, `diff.renames`,
  `merge.renameLimit`, `diff.renameLimit` (any value but 7000) or `diff.algorithm` makes a level that would be clean `unknown`.
* Binary files: both sides changed is a conflict, as in git; no binary merge driver runs.
* Submodules: a level with a commit that changes a gitlink is `unknown` unless it conflicts.
* Merge drivers and attributes: no driver runs. A `merge` attribute, the built-in macro `binary` (it sets `-merge`; git stops with "Cannot merge binary files") or a macro definition (`[attr]name ...`, reported where it is defined, used or not) that sets either, in any `.gitattributes` of the new base, of a branch tip or added or changed by a commit of the
  replayed range, or in `info/attributes` of the common git directory (also seen from a linked worktree), makes a level that would be clean `unknown`. Attribute files of the user (`core.attributesFile`) and of the work tree
  that are not committed are not read.
* rerere is unknown to the plan: a conflict that `rerere` could resolve is still reported as a conflict.
* `git rebase` drops a commit whose patch-id is in the base (`--cherry-pick`); the engine drops a commit only when its merge leaves the
  base unchanged. A patch that the base made and later reverted comes through here and is skipped by git.
* Not looked at: a dirty work tree or a running operation (git refuses a rebase there), hooks, `rebase.*` and `pull.rebase` settings,
  line-ending conversion (`merge.renormalize`, `core.autocrlf`), sparse checkout, a branch checked out in another worktree.
The plan never says `clean` when one of the listed uncertainties applies and the result would be clean (`unknown` with the reason).

**Deviations from the Go `restack plan`** (the Go command is hidden until its conformance evidence exists), one by one: (1) visible,
with the form `BRANCH... --onto BASIS [--old-parent REV]`; branches are short names of local branches (Go: fully qualified `refs/heads/...`),
`--onto` is any revision (Go: 40-hex id or qualified ref), the old parent is optional (default: the merge base); (2) not ported: the
range pairing (needs `git range-diff`; this command runs no git), the cherry classification of the parent's commits, the own-diff
equality, the identities (`--identity`) and the outcome table (`evidence-clean`), the overlap risk, the affected refs, worktrees and
hooks, the lease-push and resync texts (stw prints no commands), `--expect-head`, `--remote`, the `PATH` argument (here `--root`/`--repo`), the
attribute and repository hazard refusals (replaced by `unknown`), the refusal of a dirty target, an operation in progress and a bare
repository, the jj and GitButler refusal (decision 8), the exit status 3 (incomplete) and the result byte bound; (3) the replay is the
gix engine, not `git merge-tree`: the kinds agree with git, the paths of `rename/rename`, `rename/delete`, `file/directory` and
`directory-rename` differ (header of `src/replay.rs`); (4) exit codes: 0 only for `plan: clean` (Go: 0 for every complete plan); (5) own output and JSON layout (Go: its own schema).

## Ready check: `stw ready-check`

`stw ready-check NUMBER [--repo OWNER/NAME] [--human | --json]` reads one pull request and reports `open`, `review`, `checks`, `base`, `blocker`, `conflict`, and `rebaseable`. Every condition must pass; an unreadable or incomplete dependency is unknown and does not pass. Draft is reported but is not a condition. A non-default base passes only when another open pull request has that head branch; the lowest matching number is shown. Review state uses the same trusted-author and carry-over rules as `status --review`. Check runs and commit statuses are read at the exact head. The command uses GET only and does not mark ready or post anything.

`--json` prints schema 2 with `command`, `time`, `host`, `repo`, `number`, `head`, `draft`, `ready`, `conditions` and `errors`. Text is the default. Exit 0 means all conditions pass, 1 means a condition did not pass or a read failed, and 2 means invalid arguments. `--repo` selects a repository; otherwise the workspace must contain exactly one GitHub repository. The GitHub API base follows `STW_GITHUB_API_URL`; there is no `--host` flag. Reads are bounded by the existing GitHub reader. A cut list, malformed CI fields, a truncated status context, or missing check-state evidence is unknown. For each commit-status context only its newest observation counts.

## PR lint: `stw lint pr` (M8)

`stw lint pr PR [PATH | --repo OWNER/NAME] [--owner-identity "Name <email>"]... [--human | --json]` (read-only, GET only). Reads the
pull request, its commits (3 pages of 100; the pull request's own commit count must not be above the commits read) and its
conversation comments (10 pages of 30) and reports one finding per violated rule with rule ID, severity and location (PR title or
text, comment ID, commit SHA, line). The rule table is `src/lint_pr.rs` (`RULES`; `--json` lists it): no session link in commit
messages, title, text and comments; author and committer of every commit are an owner identity (`--owner-identity`, repeatable,
else `user.name`/`user.email` of the Git configuration of the workspace; none: usage); a `Co-Authored-By:` trailer with name and
email; Conventional Commit subjects; the PR text about 25 lines (warning above 25, error above 30), a `Blocker` line and an
`Author agent` line; the first line of a comment that claims to be a review is `Review: <CLEAR|FINDINGS> head=<40 hex>
patch-id=<40 hex> pr=<n>` (or the Carried-over form) for this pull request, and such a comment has at most 15 lines. The repository
is `--repo` or the single GitHub `origin` of the workspace of PATH (default: the current directory). Text of the forge is untrusted:
the human list escapes control, invisible and direction-changing characters and cuts each message at 120 characters.
Exit 0: no error finding (warnings do not fail). Exit 3: an error finding, or a list cut at its bound
(`truncated.commits` / `truncated.comments`; `summary.passed` is false). Exit 1: a failed read or no token (no output). Exit 2: usage.
`--json` is the document of schema 1 of the Go command (`schema_version`, `command: "lint pr"`, `time`, `host`, `repo`,
`pull_request`, `head`, `options`, `rules`, `findings`, `summary`, `truncated`; keys are never omitted). Deviations from the Go
command and the mapping of its 15 + 17 + 3 tests: headers of `tests/it/lint_pr.rs` and `tests/it/lint_pr_cmd.rs`; plan:
`docs/migration-plan.md`, "Slice 3".

## Text files: `stw lint review`, `stw lint pr-text` (new)

New behavior without a Go counterpart; read-only, no network. `lint pr-text` and `lint review` without `--base`/`--head` start no `git`. With `--base` and `--head` the revisions are resolved in process (a tag is peeled to its commit; no merge base is a usage error), and the patch-id is computed by git: `git diff BASE...HEAD` and `git patch-id --stable` through the runner (`Op::DiffRange`, `Op::PatchIdStable`, minimum Git 2.30). The in-process patch-id of `patch_id.rs` is not used here: it follows git's defaults only (see "Patch ID: the value of `git diff | git patch-id --stable`", deviations 1 to 5), and a review line carries git's value for the repository and the user's configuration.
Both read one UTF-8 text from FILE and report findings in the shape of `lint pr` (`--json`: `schema_version` 1, `command`
`lint review` or `lint pr-text`, `time`, `file`, `options`, `rules`, `findings` with `rule`, `severity`, `location` (`kind`
`file`, `line`), `message`, `summary`). Shared rule IDs and functions with `lint pr`: `session-link`, `pr-body-length`,
`pr-blocker-line`, `review-line-format`, `review-comment-length` (`src/lint_files.rs` calls `lint_pr`); the first line is read by
`review::parse_strict`, no second grammar. Own IDs: `text-line-endings`, `review-patch-id`, `review-head`,
`pr-generated-line` (`FILE_RULES`). Exit 0 no error finding (a warning does not fail), 3 an error finding, 2 usage.

`stw lint review FILE [--base REV --head REV] [--pr N|none] [--checkout PATH]`: the first line is exactly
`Review: <CLEAR|FINDINGS> head=<40 hex> patch-id=<40 hex> pr=<n|none>` or `Carried-over: from=<40 hex> to=<40 hex> patch-id=<40 hex>
pr=<n|none>` (fixed keys and order, lower-case hex, no markdown, no blank line before it); at most 15 lines (blank lines at the end
do not count); no carriage return in any line; no session trailer or
code-session URL (the word session alone is no finding). `--pr` compares `pr=`. `--base` and `--head` (only together; `--checkout` or the repository around the current
directory; names, ids or tags) compute the patch-id of `BASE...HEAD` (the first field of `git diff BASE...HEAD | git patch-id --stable`, by git)
and compare it with `patch-id=`, and compare `head=` (`to=`) with the id HEAD resolves to. An empty diff has no patch-id and never
matches.

`stw lint pr-text FILE [--ready]`: about 25 lines (warning above 25, error above 30, the limits of `lint pr`); exactly one Blocker
line, `Blocker: <text>` for a Draft (the default) or, with `--ready`, exactly `Blocker: None.`; the last non-blank line is
`🤖 Generated with [Claude Code](https://claude.com/claude-code)`; no session link; no carriage return. A `#n` that names an issue
of another repository is not checked (planned as its own step).

Fixtures: real comments and PR texts of three pull requests, stored unchanged in `tests/fixtures/lint/` (its README names the
sources); tests: `tests/it/lint_files.rs` (23 cases); mutations `LF1` to `LF39`. Source of the limits and the line forms:
`claude/CLAUDE.md` (review comment, 15 lines, session links) and `claude/skills/agent-workflow/SKILL.md`, section 9 (PR text, 25
lines, the Blocker line, the review line); the attribution line is the one of the tool (`dotagents-adapter.md`). The rules are
not compared with the current dotagents text rule by rule: UNKNOWN, as for `lint pr`.

## The op log: `stw oplog timeline | evolog | undo-plan | undo` (new)

New behavior without a Go counterpart: the three read-only commands over the logs that the accepted spikes
`spikes/stw/oplog-reflog`, `oplog-hooks` and `oplog-refhook` write. The format is the contract in
`docs/oplog-format.md` (reviewed CLEAR); this package builds the readers only. Not part of it, each its own package:
the hook install (stw's own `refs.log` lines and the rotation are the next section, `stw oplog undo` the section after, `stw oplog hooks` the one after that). Nothing is written by these commands (tests compare the repository byte for byte before and
after), no hook is installed and no `git` program runs. `--root PATH` and `--repo NAME` as for `stw prove`; `--json`.

- `stw oplog timeline [--ref GLOB] [--remotes] [--all]`: the reflogs (`HEAD` of the main and the linked worktrees,
  `refs/heads`, `refs/stash`, `refs/stw`; `refs/remotes` with `--remotes`, every other ref with `--all`) and `refs.log`
  as one list sorted by time, then by file order. A reflog row and the `refs.log` line of the same ref and new value are
  one entry (origin `reflog`, flag `covered`); a line no reflog row has is its own entry (origin `hook` or `stw`, flag
  `hook-only`; `old-unknown` for a delete without a marker). Markers are joined as in the spec (3.3). A reflog row without
  a line is `uncovered` (the hook was installed then), `pre-install`, `hook-absent` (after an uninstall),
  `unknown-window` (no `events-refhook.log`) or `not-expected` (`refs/remotes` while `remotes=off`; `refs/bisect/*`,
  `refs/worktree/*`, `refs/rewritten/*`). A `HEAD` row is covered by a line of any `refs/heads/*` ref with the same new
  value. Groups are `suspected` only (same text stem, within one second). The gaps list is always printed: `backend`,
  `config`, `truncated`, `empty-reflog`, `unreadable`, `bad`, `capture-gap`, `rotated`, `uncovered`, `pre-install`,
  `hook-absent`, `no-install-record`, `still-open`, `suspected`. JSON: `schema_version` 1, `command` `oplog timeline`,
  `entries[]`, `other_lines[]` (a well-formed line with an unknown context: kept, not in the timeline), `gaps[]`.
- `stw oplog evolog REV [--limit N]`: the chain of recorded rewrites (`rewrites.log`, both generations) from a commit
  back, with the semantics of the spike: first of equal pairs wins, self pairs ignored, `merge=N`, `split=N`, `cycle`,
  `seen-above`, `limit`; an unknown kind is shown. The reflog of the branch is compared with the pairs (`commit (amend)`
  and `rebase ... (finish)` entries): `uncovered-rewrite`, `pre-install`, `hook-absent`, `unknown-window`.
- `stw oplog undo-plan REF [--to OLD | --steps N] [--keep]`: the checks of the spike: the ref exists and is no symbolic ref, a local
  branch (other refs are previewed and marked not undoable), not checked out in any worktree (hint `--keep` or `git reset --keep`; with `--keep` see the section on `stw oplog undo`),
  no registration without a readable git dir, no running operation (`rebase-merge`, `rebase-apply`, `MERGE_HEAD`,
  `CHERRY_PICK_HEAD`, `REVERT_HEAD`, `BISECT_LOG`, `sequencer` in the git dirs), the ref still equals the newest reflog
  value, the target is not the creation entry and is a commit; notes: ancestry and the commits that only the reflog would
  keep. Exit 1 when a check is refused. When all pass it prints the `stw oplog undo` command that applies it
  (`--json`: new key `apply`, null unless executable).

Rules shared with the spec: a log that is a symbolic link, not a regular file, in a symbolic `stw` or `stw/oplog`
directory, or over 64 MiB is not read (`unreadable`); a line is skipped and counted as `bad` when it is over 4 KB,
holds a NUL byte, is no UTF-8, has too few fields, a wrong hash, time or name, or has a field 7 or later that is not
`key=value` (a glued line); an empty line is skipped and counts as nothing (a lost lock race of two writers can leave one; a line of white space is still `bad`); all text from logs is escaped (`escape_untrusted`). Deviations from the spikes and the
spec, one by one: (1) reflogs are read from the files directly, not through the gix reflog iterator (same lines; the
reftable backend is reported as `backend` and no reflog row is shown); (2) a `refs.log` line that a reflog row covers is
not listed twice (the spec says the two sources are merged; the spike had no hook rows); (3) the timeline has no
`--since`, no `expiring`, `tags` and `deleted-branch` gaps and no `no-reflog` gap of the reflog spike (later); (4) JSON of
the spikes is not reproduced field for field: `command`, `engine` and `schema_version` 1 as in the other stw documents;
(5) `undo-plan` counts the commits that only the reflog would keep with a gix walk (at most 10 000) against every other ref
and `HEAD`, not with `git rev-list`; (6) the evolog counts as capture gaps only the post-rewrite kinds (`amend`,
`rebase`, `other`) in `errors.log`; the spike counted every line of the file. UNKNOWN: SHA-256 repositories (spec Q9; the
parser accepts 64 digits, nothing ran), macOS, and a reflog read while another process writes it.

### stw writes its own ref updates (package 2)

Hooks never see gix writes, so stw appends them itself to the same `refs.log` (six TAB fields, context `stw`, names
`%XX`-encoded and cut at 200 bytes with the djb2 suffix, spec 3). Logged: `stw sync` (recovery ref create and prune,
branch fast-forward and put-back; the branch, not `HEAD`), `stw tidy apply` (branch delete), `stw tidy restore` (branch
create). Skipped: `old == new`, names other than `HEAD` and `refs/*`, `refs/remotes/**` without `remotes=on`. The line is
written even with `core.logAllRefUpdates=false`. A write never fails the command: a failure is a `write-failed` gap line
and one notice on stderr. Rotation (spec 6): after an append, a log above 10 MiB is renamed to `<name>.1` (one old
generation, replacing an older one) under `try_lock` on `rotate.lock`; a held lock means no rotation and no waiting; a
`rotated` gap line names the file. The timeline reports `truncated` for a file that rotated twice (ledger nit N2).
Open questions with recommendations: spec section 10, Q13 to Q18. Deviations from the spec text: the `rotated` gap line
carries the file name instead of `-` (Q15); `rust-version` 1.89 (Q18).

### `stw oplog undo` and the backup refs (package 3)

New behavior without a Go counterpart; the owner accepts it live (recipe: `scratch/abnahme-rezepte/rezept-stw-oplog-undo.md`).

`stw oplog undo REF --to OLD --expect NEW --yes` moves one local branch back to an older value of its reflog. REF is a
branch name or a full ref name; `--to` is a lower-case hex prefix of an old reflog value (as in `undo-plan`; no `--steps`);
`--expect` is the full current value; `--yes` is required. It runs the checks of `undo-plan` (the same function), then:
(1) takes a run number: the larger of the counter ref `refs/stw/backup-counter` (a blob `<run> <pid>-<nanos>`, compare-and-swap, up to 5
tries against other writers) and the highest run that exists, plus one (a run number is never reused; a counter that was deleted or lowered by hand does not hand out a taken number), (2) creates `refs/stw/backup/<run>/1` on the current value
(create-only: an existing ref of that name is an error, whatever its value), (3) removes the backup refs of all but the 10
highest runs (the counter and the run just taken are never removed; names that are not `<run>/<k>` with canonical positive decimals, that is no sign and no leading zero, are left alone: `+1/1` and `01/1` are no runs), (4) sets
the branch to OLD only if it still holds NEW (`MustExistAndMatch`, no-deref); a changed ref leaves the backup and the
branch as the other writer left it. Reflog entries for the backup ref and the branch (message `stw oplog undo`) are written
also with `core.logAllRefUpdates=false`. `refs.log` lines (context `stw`) are written for the counter, the backup ref, each
pruned ref and the branch update; a failed append is a notice and a `write-failed` gap, the undo stands.
Refused with exit 1 and nothing written: a failed check of `undo-plan` (a branch checked out in a worktree gets the hint
`--keep`), `--expect` not equal to the current value.
Usage errors (exit 2): no `--yes`, `--expect` not a full lower-case hash, a ref that is no local branch.
Only the ref moves: index, working tree, other worktrees and pushes are not covered.

`--keep` (package 5; recipe: `scratch/abnahme-rezepte/rezept-stw-oplog-undo-keep.md`; rules and differences: `docs/oplog-format.md`, 8a):
`stw oplog undo REF --to OLD --expect NEW --keep --yes` (and `undo-plan ... --keep`) undoes the branch that the current worktree has
checked out. stw runs `git reset --keep OLD` through the runner (hooks off, no `--hard`), after the usual backup ref, and writes the
`refs.log` line. Refused (exit 1, nothing written): the branch is not the `HEAD` branch of this worktree (detached, or another branch),
another worktree holds it, a staged or unstaged change or an unmerged path (untracked files stay and do not count), a running merge or
other operation, a gitlink that differs between the two values, `core.sparseCheckout`, no usable git or git older than 2.36, a target
that is not 40 hex digits. A git failure after the backup (an untracked file in the way, a `filter.*` driver) is exit 1; the branch is
unchanged and the backup stays; a git that was killed or timed out (120 s) may have written part of the work tree, and the message says so. Also refused: a path the target adds where an untracked or ignored file lies (the ignored ones are not protected by git), an intent-to-add, assume-unchanged or skip-worktree entry. `undo-plan --keep` starts `git --version`. `--json` gains `keep` (undo and undo-plan) and, for `undo`, `via` (`git`, else null): additive, `schema_version` stays 1. Mutations `OK1` to `OK50`.
Output: backup ref and the way back as a command (`stw oplog undo REF --to <old> --expect <new> --yes`); in the text output (here and in `undo-plan`: `Apply with:`) a ref name with shell characters is written in POSIX single quotes, other names as they are; the JSON fields `way_back` and `apply` keep the raw name. `--json`
(`schema_version` 1, `command` `oplog undo`): `ref`, `executed`, `previous`, `current`, `backup_ref`, `run`, `pruned`,
`way_back`, `notices`, and with package 5 `keep`, `via` (additive, `schema_version` stays 1); a refusal prints the `undo-plan` document with `command` `oplog undo` and `executed` false.
Deviations from the spec text: none in behavior; the spec sections 8 and Q12 now say the reflog mechanism works with
gix 0.90, and Q7 (`<run>/<k>`) is decided by the owner (2026-11-13). UNKNOWN: macOS, ext4,
SHA-256 repositories, a concurrent `git` writing the same refs.

### `stw oplog hooks status | install | uninstall | print` (package 4)

New behavior without a Go counterpart; the owner accepts it live (recipe: `scratch/abnahme-rezepte/rezept-stw-oplog-hooks-install.md`).
Rules and deviations: `docs/oplog-format.md`, section 11.

`stw oplog hooks install --yes` writes the two hooks of the spikes (`post-rewrite` -> `rewrites.log`, `reference-transaction` ->
`refs.log`; the script texts are the spike texts, `src/oplog/hooks/*.sh`) into the hooks directory of one repository, `uninstall --yes` removes
them, `status` shows where they are, `print NAME` prints a script (for a chain by hand). Opt-in per repository: nothing else installs a hook.
Without `--yes` the plan is printed, nothing is written, exit 1.
- `core.hooksPath` (any config source, `GIT_CONFIG_COUNT`, `GIT_CONFIG_PARAMETERS`, also empty) is read, never written: when it is
  set, `install` refuses and names the place the hooks would belong. No git config is written.
- A hook of the same name that is not ours is never overwritten or rebuilt: refusal with a chain proposal. One refusal refuses both.
- The directory is `<git-common-dir>/hooks` (linked worktrees share it and the logs); a symbolic link there is refused; a submodule is
  `--repo NAME`. `uninstall` removes only a regular file whose line 2 is our header; the logs stay.
- `install` appends `install` to `events.log` and `events-refhook.log` (the window the timeline and `evolog` read), `uninstall` appends `uninstall`.
- No git program runs (`PATH=/nonexistent` in the tests); the installed hooks are tested end to end with a real `git` (amend, tag, branch delete).
`--json`: `schema_version` 1, `command` `oplog hooks <sub>`. Mutations `OH1` to `OH48`.

The hint in `stw status` (new behavior without a Go counterpart; the owner accepts it live, recipe `scratch/abnahme-rezepte/rezept-stw-status-hook-hint.md`): under each repository that was read, one line when at least one of the two hooks is not ours, nothing when both are ours and nothing blocks them. `op log hooks: not installed (post-rewrite, reference-transaction); install with: stw oplog hooks install` names the hooks that are missing. Where `install` would refuse (`core.hooksPath` set, a hook that is not ours, a hooks directory that is a symbolic link) the line is `op log hooks: not active, install is refused here (<reason>); see: stw oplog hooks status`, without the install command. A set `core.hooksPath` or a linked hooks directory counts as blocked even when both files are ours, because git does not run them there. Only a hint: status installs nothing and writes nothing, starts no program, the exit code does not change, and a repository that could not be read has no line. It is the one place where stw prints a command (owner decision 2026-11-13); the JSON has the state only. JSON (additive, `schema_version` stays 2; also in `status PATH`, whose one-line text has no hint line): per repository `oplog_hooks`, null for a repository that could not be read, else `{state, reason, missing}`: `state` `installed`, `missing` or `blocked`; `reason` null, `hooks-path`, `foreign-hook` or `hooks-dir-symlink`; `missing` the names of the hooks that are not ours. There is no switch to hide the hint (no other status hint has one). Mutations `HH1` to `HH11`.

## Watch scenarios (port corpus)

`tests/fixtures/watch/` holds 279 scenarios, the test corpus for the port of `stw watch` (the Go `stw watch` stays the
reference for those scenarios). `stw watch --once`, `stw watch --stream`, and `stw watch --status` are in this crate:
a bounded open-list reader and a cycle that emits `new-pr`, `head-changed`, `closed`, `watch-error`, and
`watch-recovered`. `--stream` repeats that cycle until SIGINT, SIGTERM, or `--max-idle`, with `--interval` (minimum
10s, default 60s). `--once` is one poll. `--status` reads the heartbeat and the accepted repository list. A restart
reprints a journalled event that was not printed. Checks, mergeability, restack, and `--until-event` are not in this
command, and the 279 scenarios are not all replayed by it. The Go state format stays unsupported. There is no poll
jitter. `--stream` is not the default. The scenarios are generated from the Go tests of
`legacy/sourcesteward` and are independent of the Go file formats: each gives the inputs (observations per cycle, answers of a
fake forge, flags of the command) and the expected semantic outputs (events with kind, repository, PR, details, order and
step, a semantic view of the accepted state, exit codes). Event ids and journal sequence numbers are only additional
information, marked as format-dependent. Format and semantics: [`tests/fixtures/watch/FORMAT.md`](tests/fixtures/watch/FORMAT.md).

```
bash tests/oracle/watch_scenarios.sh GO_SRC OUTDIR   # GO_SRC: the Go source tree (read only, copied), OUTDIR: absolute, ignored by Git
```

The driver copies the Go tree into a private work directory, lays the exporter (`tests/oracle/watch_scenarios/`, Go test
files) into the copy, runs `go test` there and writes the scenarios and `index.json`; two exports are byte-identical. The corpus
in the tree is written with `WATCH_SCENARIOS_WRITE_FIXTURES=1` and the path `tests/fixtures/watch`. `tests/it/watch_scenarios.rs`
checks only the structure (required fields, unique names, step order, FORMAT.md names every field); the semantics are played in
the packages of the port. Mutations `WS1` to `WS39`.

The corpus holds 279 scenarios: 194 of kind `watch-cycles` (the core: diff, debounce, checks classes, timeout,
backoff, restack, health, baseline, lost state, replay and crash points of every kind, generated forge sequences), 33
`vectors` (pure functions: classification, shape, backoff, ids, restack candidates, human lines, config, remotes, flags, URL
and rate-limit rules), 14 `observer-session` and 25 `http-reader` (the observer and the HTTP reader
against a fake forge: the answers of the forge as data), 13 `run-loop` (the command with a fake clock: modes, exit
codes). Not exportable as scenarios (written by hand in the packages W1 and W4): the journal file (truncated and corrupt tails),
modes and checks of the state directory (work tree, `.stw`, symlinks), the lock and the second watcher, the heartbeat file, signals,
cross builds, the source checks of the Go packages (no imports, no write verbs), the `gh` token lookup, per-request deadlines and
cancellation of the HTTP client, the layout of the conditional-request store, and the config and FIFO checks that need files.

## Watch core (port, W1)

First package of the port of `stw watch` to Rust (owner decisions D-20261119-02 to -05): the core, `src/watch/` (`mod.rs`,
`event.rs`, `state.rs`, `apply.rs`, `journal.rs`, `lock.rs`, `statedir.rs`). Pure logic and files: no network (the reader is W2), no
cycle (diff, debounce, checks classes, backoff, restack choice: W3), no command, signals or config (W4). Times are parameters, there is no
clock. Tests: `tests/it/watch_core.rs` (own tests and the replay of the corpus of "Watch scenarios"); mutations `WK1` to `WK59`.
New direct dependencies, both pinned exactly: `serde` `=1.0.229` (derive; the formats are written with it) and `serde_json` `=1.0.154`
(was `1`; feature `raw_value` added, no new crate). SHA-256 is own code (`statedir::sha256`, FIPS 180-4 and NIST vectors, padding
boundaries checked against GNU `sha256sum`): the gix feature would add `sha2` and change gix's object-format support. No `libc`, no process.
Licenses: `serde` and `serde_json` are `MIT OR Apache-2.0`; both are in `Cargo.lock` already through gix and ureq.

**The formats are the Rust formats of the port, not the Go ones** (D-20261119-05: a new baseline when switching). So a Go state
directory is never opened by mistake: the directory id hashes the prefix `stw-watch-rs1` (Go: `stw-watch-v1`), and `state.json` carries
`format: "stw-watch-state"`, which a Go file lacks. Both file formats are versioned: `schema_version` 1; another version, another format or any
unknown key is `CorruptState`, never guessed.

State directory `$XDG_STATE_HOME/stw/watch/<id>/` (else `$HOME/.local/state/...`), `<id>` = the first 16 hex digits of SHA-256 over
`stw-watch-rs1\n`, the host, `\n` and `workspace:<root>` or `repos:` plus the lower-cased, deduplicated, sorted repositories joined by `\n`.
`--state-dir` must be absolute (usage error otherwise). The directory has mode 0700 (an existing one with another mode is refused), its files
0600, and it is never inside a Git work tree (a `.git` entry, file or directory, in it or above it; symbolic links are resolved first) or
below `.stw`; nothing is created when the answer is a refusal. Files: `state.json`, `journal.jsonl`, `lock`, `heartbeat`.

`journal.jsonl`: one event line each (the envelope `schema_version, seq, id, time, repo, pr, kind, details`, every key always, details with
exactly the keys of the contract, lists never null), append-only, fsynced before the event is printed, never rotated. `seq` is the `seq` of the
last complete line plus one, read from the lines, never from a file offset (the spike's `tell() - (1 << 20)` error); the file is validated
line by line with memory bounded by one line (limit 4 MiB). Bytes after the last newline are a truncated write: `Journal::junk_bytes`
reports them, `after` ignores them and the next append cuts them off. A complete line that is not a valid event, a `seq` that is not the
previous plus one, a line that is not UTF-8 or longer than the limit is `CorruptState` (not repaired).

`state.json`: one compact JSON line (maps ordered, so the file is deterministic), written atomically (temporary file, fsync, rename, fsync of
the directory): `format`, `schema_version`, `applied_seq`, `printed_seq`, `interval_seconds`, `cycle`, `repos_epoch`, `repos` (null before the first
cycle), `skipped`, `repo` (per repository: `baselined`, `since`, `last_ok`, `default_branch`, `conditional` (opaque, W2/W3), `prs` keyed by
number (`number`, `title`, `author`, `labels`, `draft`, `head`, `head_ref`, `base`, `fork`, `generation`, `epochs` {head, checks, mergeable, ready,
base}, `class` (`""`, `none`, `pending`, `green`, `red`), `class_candidate` (null or a class), `pending_since`, `pending_names`,
`timeout_fired`, `mergeable`, `mergeable_state`, `conflicting`, `behind`, `mergeable_candidate`, `detail_due`, `gone`), `closed` (at most 5000),
`restack_due`, `restack_done`) and `health` per repository and `""` for the run. `seq` is not in it.

Order of a cycle, which W3 follows (as Go `Watcher.Cycle`): the cycle emits an event and applies it to `Core::state` at once (`State::apply`),
then does its own bookkeeping that is no event (`cycle`, `last_ok`, debounce candidates, `detail_due`, silent class changes), and
`Core::commit` then journals each event (fsync), prints it and writes `state.json` with `applied_seq` = `printed_seq` = the last seq.
`commit` does not apply: a second application would undo the bookkeeping (head-changed resets the checks and sets `detail_due`).

Crash model (the "Replay effects" table of Spec 035): an event is journalled exactly once. `Core::open` replays the lines past `applied_seq`
into the state (`apply` is idempotent: counters are raised to the value in the event id and never lowered, every other effect sets or
removes) and prints those past `printed_seq` again (at-least-once). A missing `state.json` with a journal is rebuilt from the journal silently
(no reprint, the set of repositories is learned again, the first cycle is a baseline), also without a forced baseline (as Go). A corrupt
`state.json`, or a state whose `applied_seq` or `printed_seq` is beyond the journal, is an error unless the baseline is forced; then it
is rebuilt the same way. After an error the session is unusable until reopened.

Lock: `std::fs::File::try_lock` on `lock` (D3; no `libc`), a second watcher gets `WatchError::Locked(pid)` (exit 3 in W4), retried for about one
second so a `--status` probe does not look like a second watcher; `holder` probes with a shared lock and never creates the file. The lock
file gets mode 0600 when it is taken (also when it existed with another mode).
`read_status` (`--status --json`): fresh when the heartbeat is younger than `max(3 x interval, 180 s)`; reads only, no lock taken.

Event ids keep the Go form `REPO#PR:KIND:KEY` (`-` for an absent part); the corpus proves that the key functions rebuild every id of the 194
cycle scenarios and that ids are unique per scenario.

Deviations from the Go behavior (named one by one; D numbers are those of the port plan):

1. New file formats and a new directory id (D-20261119-05), see above.
2. D3: the lock is `File::try_lock`; no `lock_other` stub.
3. D4: time is UTC with `Z`, whole seconds, passed in; `format_utc` is the inverse of `github::parse_utc`.
4. D6: text from outside is masked with the project rule (`sanitize`: bytes that are not UTF-8 as `\xNN`, `\`, control, invisible and direction
   characters as escapes, cut in bytes at a character boundary) instead of Go's `?` and space.
5. D7: `serde_json` does not escape `<`, `>`, `&`, U+2028 and U+2029; consumers read JSON, lines are not byte-equal to Go's.
6. D8: no JSON-schema validator; the strict decoder refuses unknown and repeated keys, unknown kinds, schema versions other than 1, trailing data,
   null lists and numbers that are not non-negative integers. Like Go, a missing detail key is the zero value (the corpus case "decode missing
   detail key").
7. `id_counter` accepts digits only (Go's `Atoi` also took `+5`).
8. `repos` is `null` instead of an empty slice for "not set yet", so an empty set is not mistaken for the first run.
9. `restack-needed` keeps `argv` (D-20261119-03). The wired command fills it for class 1; `--human` is still later.
10. Lock: an error other than "busy" (the file cannot be opened or locked) is `Io` at once, not `Locked` after five attempts.
11. `lock::holder` reports "not held" when the probe fails with an error other than "busy"; Go reports "held".
12. `journal.jsonl` is opened as it is: a symbolic link is followed (as Go); the state directory itself is checked (mode, work tree), its files are not.
13. A red check keeps `shape` empty. Go classifies the failing set; this cycle does not.
14. `restack-needed` is class 1 only: the child base equals the merged head ref. The contains class (same base, the child commits include the old parent) is absent, and a class-1 child is not checked against the commit list.
15. The 194 `watch-cycles` scenarios are not replayed by this cycle. The checks, mergeability, timeout, and class-1 restack above are covered by hand-written tests in `tests/it/watch_command.rs`.
16. `checks-pending-timeout` is fixed at 45 minutes inside the command. There is no flag. Zero would disable it; the command never passes zero.

What the corpus drives in W1 (`tests/it/watch_core.rs`): vectors `event_id`, `id_counter`, `event_json` (22 cases), `workspace_id` (which inputs share an
id; the Go id values are format-dependent), `default_state_dir`, `status_freshness_seconds`, `merged_due_head_fork`: 8 of the 33 vectors
(the others belong to the cycle, the reader, the command or are Go-only). `watch-cycles`: all 194 scenarios (a) as an `apply` sequence with the
silent baseline of the first read and the state-changing file operations of the steps, compared with `expected_state_view`, (b) through the session
(`Core::open`, `commit`, restart, crash after each durable step, delete or corrupt the state, forced baseline) against the printed ordinals of every step and
the journal ordinals (108 crashed steps: append 41, print 37, state 30; 6 deleted and 1 corrupted state), (c) crash variants against their crash-free
variant (same journal, same replayed state), and the 3 status probes. Compared in (a): `repos`, `skipped`, `repos_epoch`; per PR the set, `head`, `base`,
`draft`, `generation`, the epochs head/mergeable/ready/base (checks epoch at most the view's); closed records; health episode, stall episode, open error,
stall, last code, failures at most the view's. **Not mapped in W1 (cycle effects, W3)**: debounce candidates, backoff times (`next_try`), check classes and
the silent `pending`/`none` class changes (and so the exact checks epoch), `pending_*`, `timeout_fired`, mergeable fields, `detail_due`, `gone`,
`last_ok`, `since`, `cycle`, `interval_seconds`, `restack_due`/`restack_done` after the restack (the cycle clears them), `hints`, `state_after`, the restacker choice.
Not corpus-driven but written by hand: journal truncation and corruption, modes, work-tree refusal, atomic files, the lock (same process and a child process),
the heartbeat file. Not run: the observer-session, http-reader and run-loop scenarios (W2, W4).

## Watch reader (port, W2)

`src/watch_reader/` is the reader and observer of `stw watch` (package W2 of the port; the Go reference is
`internal/hosting/watch_*.go`, `internal/watchcmd/observer.go`, `pullsread.go` and the remote rules of `resolve.go`). It has no
cycle (W3), no state directory or journal (W1) and no command (W4). Every request is a GET; there is no write path (a test scans
the sources for write calls). The token comes from the client (`WatchHttp::from_client`), goes only into the `Authorization`
header and never into an output, an error text, an observation or the store (`token_never_in_output`). No `git` or `gh`
process, no new crate: the transport is `ureq` as in `src/github.rs`, with the same platform trust store.

- `http.rs`: the pure rules (endpoint allowlist, branch segment encoding and guard, Link header, `X-RateLimit-*`, Retry-After,
  `assess_rate_limit`, `rate_limited_until`, `split_remote`, `parse_remote`, `valid_host`) and `WatchHttp`, the reader:
  conditional GET (`If-None-Match`, a 304 is a typed result that returns the validator that was sent), `list_open_pulls`
  (Link paging, 25 per page, halving to 12 on a body over 1 MiB and restart at page 1 with the halved size kept in memory only, a
  probe past a full last page after a 304, bounds 80 / 167 pages and `max_open_prs` 1 to 2000), a deadline of at most 30 s per
  request, a body limit of 1 MiB (also by `Content-Length`), no redirects. Time is never read: the caller passes Unix seconds.
- `cond_store.rs`: `CondStore`, the validators the core keeps between cycles, and its serialized form (see below).
- `obs.rs`: the types of the observation: `RepoObservation` (`default_branch`, `prs`, `gone`, the replacement `cond`), `PrObs`
  (with `DetailObs` mergeable and state, and `ChecksObs` runs, statuses and `via`), `GoneObs` (merged, merge commit, `method_hint`
  merge / rebase-or-squash / unknown), `Hints` and `KnownPr` (what the accepted state tells the observer), `ObserveError`
  (code, run-wide, wait) and `ObserveFailure` (an error, or `Canceled`). `checks_class` gives `none|pending|green|red`; the shape of
  a red head and the failing names belong to W3. The texts are raw GitHub text; they are escaped where an event is made.
- `host.rs`: `HostObserver` over the trait `WatchRead` (`WatchHttp` implements it; the tests use a fake that mirrors the Go
  one). It reads only what the state makes necessary: the list every cycle, `branches/BASE` once per base, `pulls/N` for a new
  PR, a new head, a moved base, after a merge, when due and on the slow refresh, check runs and statuses for pending, unobserved
  or changed PRs (a stored candidate is confirmed with a read without validators), and `pulls/N` once for a PR that left the
  list. `after_cycle` reads `rate_limit` (free): below 10 percent of the limit the interval is to be doubled, below 50
  remaining a hold is armed that the next `observe` reports once as a run-wide `rate-limited` (reset plus 5 s).
  `read_pull_commits` reads a page of `pulls/N/commits` for the restack. For W3: `observe(repo, &Hints, Option<&CondStore>,
  &AtomicBool)` returns the snapshot and the replacement store; the core keeps the store.

Endpoints (all GET, checked against the allowlist before a credential leaves): `pulls?state=open&per_page=25|12&page=N`,
`pulls/N`, `pulls/N/commits?per_page=100&page=1..3`, `branches/SEG`, `commits/SHA`, `commits/SHA/check-runs` and `.../statuses`
(`per_page=100`, pages 1 to 10), `rate_limit`, and (new) `actions/runs?head_sha=SHA` (pages 1 to 10).

Error classes (`Code`): `rate-limited` (429, or 403 with remaining 0, a Retry-After header or the text "rate limit" or "abuse
detection" in the first 4096 bytes), `unauthorized` (401), `forbidden` (other 403, and 404 with its status kept), `http-5xx`,
`malformed-response` (other statuses, bad bodies, an unsolicited 304, a redirect other than 301/308), `too-many-open-prs`,
`page-too-large`, `repo-moved` (301/308, never followed; the new name only for a `/repos/OWNER/NAME/` path on the API host),
`network` (connection failure, deadline). Messages are fixed text of at most 200 bytes.

The store of validators (`schema_version` 1, a new Rust layout; the Go `Conditional` layout is not used): `{"schema_version": 1,
"list": {"per_page": 0|12|25, "pages": [{"etag", "link", "items": [...]}]}, "tips": {BRANCH: {"etag", "sha"}},
"validators": {"pulls/N" | "checks/SHA/runs" | "checks/SHA/statuses": ETAG}}` in a fixed key order with sorted maps. `from_json` is
strict (unknown fields, other versions, bad ETags, SHAs, keys, sizes) and bounded (80 / 167 pages, 25 items per page, 20 000
validators, 5 000 tips); a refused store means one unconditional round. `prune` drops what is no longer open.

Tests: `tests/it/watch_reader.rs`. Replayed from the corpus: all 25 `http-reader` scenarios against a raw HTTP server on
127.0.0.1 (port 0; every request is checked for GET, bearer token and `If-None-Match`), all 14 `observer-session` scenarios
against a fake reader (the Go test fake; the `conditional` flag is, as in the export, the last validator sent under that
call name), and 14 `vectors` files with 198 cases (remotes and hosts, URL rules, branch segments, allowlist, Link, rate limit,
Retry-After, pull detail, default branch, commit list) plus the class field of 43 cases of `classify_checks`. Not replayed in
W2, by design: the other 15 `vectors` files (backoff, freshness, ids, event JSON, restack candidates, human lines, config,
flags, workspace; packages W1, W3, W4), 194 `watch-cycles` and 13 `run-loop` scenarios. Not mappable from the three kinds: 0.
Own tests without a Go counterpart: the store (round trip, golden bytes, 25 refusals, bounds, prune), the Actions fallback,
each rate-limit evidence alone, pages of check runs and statuses, a missing base branch, the request timeout, a refused
connection, hostile Link headers, the page bound of an endless list, masked diagnostics. Mutations `WR1` to `WR89`.

Deviations from Go (W2 part of the list of the plan, section 4):

- D5: error texts are fixed Rust texts (no go-gh text), the code mapping is unchanged; `message` never carries a body or the token.
- D6: the observation holds raw GitHub text; escaping (`escape_untrusted`) is applied where an event or a diagnostic is made. The
  one diagnostic of this layer (a base name that is refused) is escaped here.
- D9: rate-limit and backoff times are Unix seconds passed in; nothing reads the clock, nothing sleeps.
- D10: the default branch is guessed as in Go (`main`, then `master`, from the bases of the open PRs); `base.repo.default_branch`
  of the list items is not used.
- D11 (new): a 403 on `check-runs` falls back to the Actions runs of the head (`actions/runs?head_sha=`, the newest run per
  workflow and event, `status_ci::latest_runs`); the runs have no output text, `ChecksObs::via` is `ActionsRuns`, one line per
  repository on `take_diagnostics` ("checks via actions-runs"). Both refused: `forbidden`. A 404 is not a fallback.
- The corpus ties the observer to a Go `Reader` interface; here it is the trait `WatchRead` (same calls, same order).
- A cancellation (`ObserveFailure::Canceled`) is noticed when a read fails, as in Go; there is no context, a request ends at its
  deadline of 30 s at the latest. Signals belong to W4.
- U+FFFD in a branch name is refused (it is what invalid UTF-8 becomes in the exported corpus); the other branch rules are Go's.
- Dates: `merged_at` is validated with an own RFC 3339 check (`valid_rfc3339`: Go's `time.Parse` form with fraction and offset).
- JSON types are checked strictly where Go's decoder is (a number where a string is expected is `malformed-response`); a value
  of the right type but empty is kept as empty.
- A tip SHA must be 40 lower-case hex (Go: 40 characters), so that a stored tip always passes the strict store.
- A validator that is empty is not stored (Go stored the empty string); a read without validator is the same.
- `max_open_prs` is a `usize`: the corpus values -1 and 0 are the refused value 0.
- The call accounting (`take_calls`) is the reader's own, as in Go; `Client::stats` is not fed.

## Build and test

```
cargo build --release          # target/release/stw
cargo test                     # unit and fixture tests, no network
cargo test --test it -- --ignored network   # clones a public repo, rewinds it, fetches and fast-forwards
bash tests/mutation/mutate.sh SCRATCH    # 1640 mutations, each must fail the named test
bash tests/oracle/shim_proof.sh stw ROOT SCRATCH   # fake git/gh/ssh first on PATH: 0 calls
bash tests/oracle/tidy_oracle.sh OUTDIR   # oracle of tidy plan against real repositories and GitHub (network; see Oracle of tidy plan)
bash tests/oracle/watch_scenarios.sh GO_SRC OUTDIR   # exports the watch scenarios from the Go sources (see Watch scenarios)
```

Integration tests live in one binary: new tests go to `tests/it/<name>.rs` with a `mod <name>;` line in `tests/it/main.rs` (one link instead of one per file; `slug_cases`, `review_states`, `snapshots_cli` and `snapshots_symlink` stay separate binaries).
`tests/oracle/*.sh` use the `git` binary **only as a reference** in tests (patch ID, fast-forward results).
Test helpers (dev-dependencies only, not in the binary): `proptest` (`tests/it/patchid_props.rs`, small case count),
`insta` (`tests/snapshots_cli.rs`; snapshots in `tests/snapshots/`, review with `cargo insta review` or
`INSTA_UPDATE=always cargo test --test snapshots_cli`), `datatest-stable` (`tests/slug_cases.rs`: one `.url` and
`.slug` file pair per case in `tests/slug_cases/`).
`cargo clippy --all-targets -- -D warnings` and `cargo fmt --check` are clean.

## Reflog identity and order of writes

Reflog entries (fast-forward of the branch and `HEAD`, update of the remote-tracking refs by the fetch) need a
committer. stw resolves it in three steps: `GIT_COMMITTER_NAME`/`GIT_COMMITTER_EMAIL`, then `committer.*` and
`user.name`/`user.email` of the Git configuration (as gix resolves them), and finally the fallback
`stw <stw@localhost>`. The fallback lives in memory only: it is never written to a configuration, and stw creates no
commits, so it is never an author or committer of a commit. `GIT_AUTHOR_*` is not used.
Rule for the environment variables: they count only when `GIT_COMMITTER_NAME` and `GIT_COMMITTER_EMAIL` are both
set and non-empty. Otherwise both are ignored (and the `EMAIL` variable with them) and the Git configuration, then
the fallback, decides; a name from the environment is never combined with an email from elsewhere.

A `sync` run writes in this order: all checks first (dirty paths, overlap, untracked files in the way), then the
index lock (`.git/index.lock`, taken here and held until the index is committed), then the branch ref (the write
that can still fail for reasons no check sees: permissions, locks, reflog), then working tree files, then the
index (written into the lock, which is renamed over the index last). There is no transaction across ref, files
and index:

- A refused check, a held `index.lock` (another git process or a stale lock file) or a failed ref update leaves
  index, working tree and `HEAD` untouched (`tests/it/sync.rs`). The foreign lock file is not removed.
- If writing files fails, the ref is put back to the old commit (reflog entry `stw: undo fast-forward ...`). Files
  the checkout already wrote stay; the index was not touched. `git status` shows those files as changes.
- If writing or committing the index fails, the ref is put back; all files are already new, the index is the old
  one.
- If the ref cannot be put back either, the error names both failures. `HEAD` then stays on the new commit while
  files and index are old or partly new.
- If the process is killed (or the machine loses power) after the ref moved and before the index was written,
  `HEAD` points to the new commit while files and index are old or partly new; stw does not detect or repair this
  on the next run. `git status` shows the update as local changes. A kill at any point after the index lock
  was taken (before the ref moved, during the checkout, during the index write) also leaves a stale
  `.git/index.lock`; no signal handler could prevent that for SIGKILL or a power loss. Remove that lock file
  first (only when no git process runs), otherwise git refuses every command that writes the index. If the
  ref had not moved yet, nothing else changed. To finish the update:
  `git reset --hard HEAD` (tested in a scratch repository; it also discards unrelated local changes). Without
  discarding anything else: `git checkout HEAD -- <paths>` with the paths of
  `git diff --name-only --diff-filter=d <old commit> HEAD`, then `git rm --cached` and remove the files of
  `--diff-filter=D`. A plain `git checkout HEAD -- <path>` fails for a path that the update deleted ("pathspec
  did not match"; tested). To go back: `git reset --hard <old commit>` (same caveat; not tested), where the old
  commit is the second-to-last entry of `git reflog show <branch>`.

## The journal of a `sync` run

Every branch a run moves is journaled first, like in Go (`internal/stwsync/journal.go`; the record is the same format
and a record of either tool is read by the other). After all checks passed and the index lock is held, and before the
branch ref moves, `sync` creates the recovery ref `refs/stw/recovery/sync/<run id>/<n>` (create-only, written through gix with a reflog, no program
is started; it holds the old tip, so the objects stay reachable) and then writes the record `<git common dir>/stw/sync/<run id>.json` (state `prepared`,
with the planned new tip); after the move the record says `completed` (or `failed`). The record is replaced atomically
(temporary file, fsync, rename, fsync of the directory); the first write refuses an existing file. Directories are `0700`, files
`0600`; a symbolic link as `stw`, `sync` or a record is refused. If the recovery ref or the record cannot be written, the
branch is not moved (`journal-failed`, `result: error`, exit 1). A refused check (local change on a touched file,
untracked file in the way) leaves neither record nor recovery ref, and so does a repository that was not changed: a run
without a change writes no journal and has `run_id: null`. `--plan`/`--dry-run` write neither (the test covers the file tree and
`refs/stw`; the plan's own fetch writes the remote-tracking refs).

Run ID: `YYYYMMDDTHHMMSS` + 6 digits of microseconds + `Z-` + 8 hex digits (UTC time and a random suffix, like Go); one ID for
all repositories of a run. The latest run is the highest ID in this order. Record (`schema_version` 1): `type` `sync`, `run_id`,
`remote`, `started`, `entries[{n, ref, worktree, old_oid, planned_new_oid, new_oid, orig_head, stash_oid, recovery_ref,
state, reason}]`.

Retention (as in Go): after a run that completed a change in a repository, only the ten most recent completed runs of that
repository keep record and recovery refs. A recovery ref is deleted only when the record names it inside its own run
(`refs/stw/recovery/sync/<run id>/<n>`), it is not symbolic and still points at the recorded old tip; if one stays, so does
its record. A record with a `prepared` entry (a killed run) is kept until a restore has dealt with it. Files in the directory
that are not run records are ignored. Not undone and not journaled: pruned remote-tracking refs and submodule moves.

## `sync restore`

`stw sync restore [PATH] [--run ID]` undoes the branch moves of one run (port of `internal/stwsync/restore.go`). The run is
`--run ID`, else the highest run ID found in the journals of the workspace (the order of the IDs, never a modification time).
For every entry of the record in every repository of the workspace the result is one of:

- `restored`: the branch stood on the tip the run left and is back on the journaled old tip. A branch that is checked out in the
  main worktree moves with `git reset --keep <old tip>` (index and files with it) and only when that worktree has no tracked
  change (an untracked file does not count, nor does a submodule that moved: the `--ignore-submodules` rule of Go); a branch that
  is checked out nowhere moves with `git update-ref --no-deref <ref> <old tip> <expected tip>` (compare and swap). Never forced.
- `already-restored`: the branch is on the old tip already (a second restore; a run that stopped before it moved the branch).
- `changed-since`: anything else that is not the tip the run left, and nothing is touched: a new commit (`the branch moved since the
  run`), the branch is gone, symbolic or checked out in another worktree (linked, relative or dead registration), a local change
  in the checked-out worktree, an operation (merge, rebase, ...) in progress in it, or git refused the expected value.
- `journal-incomplete`: a record that was never completed and names no planned new tip (the reason names the recovery ref for a manual reset).
- `failed`: `bad-record` (a ref that is not a valid `refs/heads/<name>`, an old or new tip that is not 40 lower-case hex
  digits), `old-object-missing`, `raced` (the branch was not on the old tip after the reset), `git-failed: ...`, or a record or
  journal directory that cannot be used (not JSON, another run's or type's record, a symbolic link, too large).

A record that a crash left in state `prepared` is dealt with: a branch that did not move is `already-restored`, one that moved to the
planned tip is `restored`; either way the entry becomes `restored` in the record. A restore never deletes a recovery ref or a record
(retention of the next run does).

The record is data and is not trusted: the repository to work in is the one whose git directory holds the record, taken from the
workspace (gitlink paths that are not accepted are never used, see "Submodules: the gitlinks of the index"; they are listed as `rejected_paths`), the
`worktree` field is never read, a ref and an OID are checked before any git argument is built, and every git step runs through the runner
(`"via": "git"`, hooks off, fixed argument vector, `reset --keep` refused when the repository's own config names a `filter.*`
driver). Not undone, because not journaled: pruned remote-tracking refs, stash reapplies, submodule moves. At most 16 repositories at a time.

Output: text `sync restore of run <id>`, one line per entry (`name`, `branch`, result, reason), then the counts. `--json`:
`schema_version` 2, `command` `sync`, `mode` `restore`, `root`, `engine`, `run_id` (null when refused), `repos[{name, path, branch,
result, old_oid, new_oid, reason, via}]` (`old_oid`: where the branch stood before the restore, `new_oid`: where it stands after a
`restored`/`already-restored`, null otherwise; `via` only when a git step ran), `summary{total, restored, already_restored,
changed_since, journal_incomplete, failed}`, `error` (null, or `{code, message}`: `sync-restore-no-run`),
`rejected_paths`. Exit 1 when `error` is set or an entry is `changed-since`, `journal-incomplete` or `failed`; a refusal in text mode prints
`stw: sync: <code>: <message>` to stderr. `restore` together with any sync option (`--plan`, `--dry-run`, `--apply`, `--prune`,
`--no-prune`, `--submodules`, `--rules`, `--rules-path`, `--offline`, `--repo`) is a usage error (exit 2, nothing done).

Deviations from Go: (1) the workspace is `PATH` or `--root` (both is a usage error), and the repositories are the root and the gitlinks of the index
(nested ones included, as Go walks them); (2) a malformed `--run` is a usage error (exit 2; Go: a refused
result, exit 1); (3) a record is also refused when a `ref` is not a valid branch name by the rules of git, an OID is not 40 digits
(a 64 digit OID of a SHA-256 repository is `bad-record`: stw has no SHA-256 support) or the file is a symbolic link; (4) any merge, rebase, bisect, cherry-pick or revert
running in any worktree of the repository (main, linked, relative or dead registration) stops the restore of that repository (Go: only an
operation that involves the branch); (5) a branch ref that is symbolic is `changed-since` before the tips are compared; (6) an unusable journal
directory or record is reported as a `failed` entry of that repository, never skipped; (7) the JSON layout is the Rust one (`repos[]` as in
`sync`, `summary` keys with underscores, `rejected_paths`), the text has no per-branch `moved` arrows but the abbreviated tips in the reason;
(8) `git reset --keep` is not a compare and swap: between the check and the reset a commit could be added to the branch; the result is then
`raced` or the commit is only in the reflog (Go has the same window); (9) `reset --keep` and `update-ref` run through the runner, which
refuses a repository whose own git config names a program (`core.sshCommand`, a credential helper, `include`, `filter.*` for the reset, ...):
the entry is `failed` with `git-failed: the repository's own git config sets <key>` (Go has no such refusal; a run itself is not affected);
(10) like Go and `git checkout`, `reset --keep` overwrites an ignored file that lies in the way of a file of the old tip (it only protects
tracked local changes and untracked files that are not ignored).

## Known limits of a `sync` run

It refuses, and changes nothing, in a few cases where git would proceed: a file that is deleted locally and
upstream, a rename that only changes the case of a path, and a tracked file that upstream replaces by a
directory (reported as "changes between file and directory"). A new submodule is checked out as an empty
directory, like git does; its content is not cloned. If the checkout itself fails halfway (disk full, permissions
on a single file), the ref is restored but files already written stay; `git status` shows them.

## Own code where gix has no API

- Patch ID (`git patch-id --stable`, for squash/rebase-merge detection): `src/patch_id.rs` and `src/patch_rename.rs`; the section
  "Patch ID: the value of `git diff | git patch-id --stable`" below says what it equals and where it deviates.
  Paths are hashed as git writes them (quoted, raw bytes). Git's ID ignores white space in file names and in file
  content (`a b.txt` equals `ab.txt`; `\tcc x` equals `  cc   x`), so the `equal-patches` proof also requires an
  identical digest of the diff lines with their white space (file names included): a change that differs from main
  only in white space keeps the branch.
- Commit similarity (`src/tidy_similarity.rs`, wired into `tidy plan` as the proof `similar-patches` (README "Similar patches in `tidy plan`"); specification and accepted method in `spikes/stw/tidy-similarity/README.md`):
  a commit is the multiset of its changed lines (`<path> NUL <sign> <trimmed line>`, context 0, no rename detection), two commits are compared with
  Dice `2 * |A ∩ B| / (|A| + |B|)`, decided in integers (`200 * I >= pct * (A + B)`; the display rounds half up to one decimal and never shows the
  threshold for a value below it). Candidates on main: same subject, same subject without `type(scope):`, or at least half of the changed files in
  common, at most 7 days older (author time), at most 100 per commit. Classes per branch `landed` (all >= 95), `modified` (all >= 70), `partial`, `none`.
  Hunks come from gix-imara-diff (Myers), not from `git diff -U0`; whether the multisets differ from git's is UNKNOWN until the oracle. Limits: a commit with more
  than 20 000 keys or a blob over 1 MiB is `too_large` (no value, prevents `landed`). Reusable parts: `Keys::sum`, containment as (numerator, denominator),
  `tree_entries` (cumulative diff `base...tip`), `MainIndex::candidates` and `in_window` apart from the comparison. Mutations `TS1` to `TS51` and `TP1` to `TP31` (the plan).
- Rebase-merge proof (`src/tidy_rebase.rs`, wired into `tidy plan` as the proof `rebase-identity` (README "Rebase evidence in `tidy plan`"); specification in `spikes/stw/tidy-similarity/README.md`, section
  package 1b): a rebase merge writes a branch commit again with the same author, author time and subject. `RebaseIndex` walks the first-parent chain
  of main once; a branch commit maps to the chain commit with the same key (author mail in lower case, author seconds, subject) and the exact author
  name, after an equal patch. Several commits with one key are assigned as a multiset by their neighbours (`ambiguous` without one). `contiguous`,
  `in_order`, `gap`, `runs`: the targets form one block in branch order. Strong (class `rebase-identity`, decision `review`, never `delete`): all
  commits mapped, one ordered block, content at least 95 % per commit (at least 70 % and below 95 % is `modified`), no later version (amend after landing), at least one identity.
  The index stops by a run of 64 old author times, never by the committer time; the boundary commit of a shallow clone is skipped. The pull request (`evaluate_api`, on given inputs, no request) and
  `landed_via` (merge, rebase, squash, unknown with basis and strength). Mutations `RB1` to `RB68`; the plan wiring (rebase stage, pull request reads, `landed_via`, flag): `TR1` to `TR52`.
- Squash evidence, layer 1 (`src/tidy_squash.rs`, wired into `tidy plan` as `squash` per branch (README "Squash evidence in `tidy plan`"); specification `scratch/stw-tidy-squash/spec.md`, package 5a):
  the merged pull request (merge commit on the base with one parent, the head in the branch, commits after it) and the subject on the base (`(#N)`, title equality, list in the body, `Pull-request`
  trailer); no diff, no request, no class, no decision. Mutations `SQ11` to `SQ56` (5a part).
- `tidy plan --explain`: `src/tidy_explain.rs` (the text) and the flag in `src/cli.rs`; tests `tests/it/tidy_explain.rs`; mutations `TX1` to `TX38`.
- The oracle of `tidy plan`: `src/tidy_oracle.rs` (pure comparisons), `tests/it/tidy_oracle.rs`, the ignored tests `tests/it/oracle_tidy.rs`, the driver `tests/oracle/tidy_oracle.sh`; mutations `OR1` to `OR35`.
- Fast-forward with checkout: `src/sync/fast_forward.rs` (checks first, writes after; index tree cache removed by hand, gix issue 2421).
- Untracked collapsing for nested repositories: `src/status.rs`.
- Worktree registration HEAD (gix `Proxy` has none): `src/repo/worktrees.rs`.

gix has no API stability promise before 1.0: the pin is exact and updates are deliberate.


## Patch ID: the value of `git diff | git patch-id --stable`

The patch ID that `stw prove`, `stw review carry-over`, `tidy`, `restack` and `stw dev patch-id` print and compare is the first field
of `git patch-id --stable` on the diff of a commit against its first parent as `git diff` and `git show` print it with their built-in
defaults, except the indent heuristic of the hunks (deviation 3): renames on (50 %, `diff.renameLimit` 1000), copies off, no break detection, `core.quotePath` on. Before 2026-11-19 a rename was
a deletion plus an addition (`git diff-tree --no-renames`), a file diff without hunks was a unit of its own and a binary file
hashed full blob ids; that was not git's value, and `stw lint review` rejected a `Carried-over:` line that `stw prove` had printed for a commit
with a rename (debt ledger 2026-11-17, "prove/carry-over gix patch-id rename mismatch"). Measured then: 14 of 14 commits with a rename in
three real histories and 16 of 21 cases of the fixture oracle differed from git; now 14 of 14 and 21 of 21 are equal, and the 66 pinned fixtures of
`tests/it/patchid_git.rs` (48 rename and tie cases, 18 units cases) (units, binary files, type changes, gitlinks included) are equal. Sources: git `builtin/patch-id.c` (`get_one_patchid`), `diffcore-rename.c`, `diffcore-delta.c`, `diff.c`;
the behavior is checked against the installed git, not copied from the source text.

- Renames (`src/patch_rename.rs`): exact renames first (same blob id, the same base name preferred), then the base-name pass (a base name
  that occurs once among the remaining sources and once among the remaining destinations is scored first; such a pair needs 75 %, git's
  `min_basename_score` = 30000 + 0.5 * 30000, a pair below it is left to the matrix), then the similarity matrix
  (byte-span estimate of `diffcore-delta.c`: lines of at most 64 bytes hashed, a CR before an LF ignored, score `copied * 60000 / larger size`,
  threshold 30000 inclusive, a source is used once; the 4 candidate slots per destination are filled as git's `record_if_better` fills them,
  so equal candidates keep their slot order and the order of ties is git's, not the qsort of the C library; sources used by an exact or base-name
  rename are not candidates). A rename prints `similarity index N%`, `rename from`,
  `rename to` (the percentage is hashed, so it has to be git's integer), then the `---`/`+++` and hunks of the old against the new file; an exact
  rename has no hunks; the exact pass looks at no more than 100 identical candidates per destination, as git does. Skipped above `1000 * 1000` source/destination pairs (git warns on stderr and prints no renames). Symlinks and gitlinks
  are renames only when exact and only with equal modes (a symlink is never paired with a regular file of the same blob, a gitlink only with a
  gitlink; a regular file may change 100644 to 100755). A change of the file type (file, symlink, gitlink) is a deletion plus an addition, as git prints it.
- Units (`--stable`): a file diff without hunks (mode change, empty file, exact rename) is hashed together with the next file diff, a binary
  file adds the abbreviated blob ids of its `index` line and ends the unit, the `diff` line after it is not hashed, the last unit is added even
  when it is empty. The abbreviation length is the one gix gives the repository (`core.abbrev`, else from the object count, at least 7).
- The second digest of `PatchKey` (used with the patch ID by the `equal-patches` proof) follows the same units, keeps white space, hashes full
  blob ids for binary files and also the lines git skips.

Deviations from git, one by one:

1. The user's configuration is not read: `diff.renames=false` or `copies`, `diff.renameLimit`, `-M<n>`, `-C`, `-B` give another output in git.
   The result must not depend on a configuration (a review line carries it); `stw lint review --base/--head` still asks git itself.
2. `.gitattributes` (`binary`, `-diff`, a diff driver, a text conversion) is not read.
3. Hunk placement: git's default `diff.indentHeuristic` (on since 2.14) moves a hunk to the position that reads best; gix-imara-diff runs here without that
   heuristic, so a hunk that can slide differs. Measured against `git diff` (review of 2026-11-19): 25 of 300 commits of the sourcesteward path and 11 of 300
   commits of this repository differ, and all 25 of the first set equal `git diff --no-indent-heuristic`. It is a cause of different patch IDs for equal
   patches (a missed `equal-patches` proof), not of equal IDs for different patches.
4. The abbreviation length of a binary file's `index` line: gix computes the automatic length from its own object count, git from a sample of loose
   objects; near a power of 2 the lengths can differ.
5. Not covered by a mutation: a NUL byte after the first 8000 bytes of a text file
   (git stops hashing the line there; this code does as well, without a test).

The values are git's from version 2.31 on and were checked with git 2.47.3 only (UNKNOWN: 2.31 to 2.46). The base-name pass (`find_basename_matches`,
`min_basename_score`) is in `diffcore-rename.c` at tag v2.31.0 and not at v2.30.0 (commit bd24aa2f97, "diffcore-rename: guide inexact rename detection
based on basenames", an ancestor of v2.31.0); with git 2.30, which `stw lint review` accepts as its minimum, git itself gives other values for commits where
the base-name pass decides, while `stw lint review` still compares with the value of the installed git. Git also reads `GIT_BASENAME_FACTOR` (a test hook,
not read here).

Oracles (read-only on real repositories, throwaway repositories under `/tmp/stw-prove-patchid`): `tests/oracle/patch_id_rename_oracle.sh fixtures|repo`
(commits with renames), `tests/oracle/patch_id_oracle.sh` (sample of commits), the ignored tests `patchid_git::pinned::oracle_against_git` and
`patchid_git::units::oracle_against_git` (run git on the fixture repositories and compare); the ordinary tests pin the git values. Mutations `PI1` to `PI32`.

## Not verified

`stw add`: the gix clone over https was not run in the test suite (the tests replace the clone by a local copy; the
checks, the git steps and the rollback run for real); a run against a live GitHub repository is a manual check. Git
older than 2.47.3, macOS, a `url.<x>.insteadOf` rewrite (the clone ignores the user's config), concurrent runs on one
`PATH`, and a race between the checks and the removal of a clone directory are not covered. The clone's attribute
scan refuses a little too much (a line naming `filter` anywhere, also in an unrelated pattern); `git check-attr` is
trusted for the workspace (its `text`/`eol`/`working-tree-encoding` effects on `.gitmodules` are not asked). `GIT_ATTR_NOSYSTEM=1` keeps `/etc/gitattributes` out of every git step of `stw add`; that this works against a real system attributes file was not checked live (no root). The
`.git/modules` check before the absorb narrows the race window but does not close
it (no `dirfd`/`O_NOFOLLOW`).

Automated fetch of a private repository over https (the credential answer is unit-tested; one manual run against a private repository worked); `git://` behavior of gix; conflicted
index during a `sync` run; sparse checkout, `skip-worktree`, case-insensitive path collisions; a real git-lfs
during fast-forward (a `filter` attribute on a path the update writes and a `filter.*` driver in the config are refused, tested); Windows, and Linux beyond the test suite (the original measurements ran on macOS arm64; the M0 review and tests ran on Linux aarch64; keychain code is macOS-only); cold-cache timings.
