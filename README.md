# stw (SourceSteward)

Implementation scope, feature decisions, dependencies, and acceptance gates: [Rust migration plan](docs/migration-plan.md).

Everyday git and GitHub workflows for agents and people, in one binary. Written in Rust on
[gitoxide](https://github.com/GitoxideLabs/gitoxide) (`gix`, pinned to `=0.90.0`) and `clap`.
**Today stw itself starts no other program**: no `git`, no `gh`, no `ssh`. GitHub is reached over its REST API.
It works with stock macOS tooling and an empty `PATH`. (For ssh and local-path remotes gix starts `ssh` or
`git-upload-pack` itself during `sync --apply`, see "Remote kinds".)

Decided 2026-11-07, not implemented yet (milestone M0): `status`, `tidy` and `sync` over https start no program. `sync` for ssh and local-path remotes,
`push`, `range-diff` and conflict resolution in a worktree will run the installed `git`, through one runner with a fixed
argument vector and a scrubbed environment, each such step marked `"via":"git"` in the JSON output; without `git` the
step reports `unsupported`. See `chartroom/notes/stw runs git only for push, fetch over ssh, range-diff and resolve.md`.

```
stw status    [--json] [--offline] [--root PATH] [--repo NAME]... [--rules-path PATH]
stw tidy plan  [--json] [--offline] [--root PATH] [--repo NAME]... [--all] [--backup-days N]
stw tidy apply   [--plan FILE] [--manifest-dir DIR] [--backup-days N] [--json] [--offline] [--root PATH] [--repo NAME]...
stw tidy restore [--run ID] [--branch NAME] [--manifest-dir DIR] [--json]
stw sync      [--apply] [--json] [--offline] [--root PATH] [--repo NAME]...
              [--rules] [--rules-path PATH]
```

| Command | What it does | Writes |
| --- | --- | --- |
| `status` | per repository: branch, upstream ahead/behind, staged/unstaged/untracked/conflicts, stash, running operation, submodule pin drift, open pull requests | nothing |
| `tidy plan` | the plan only: per local branch `delete`, `review` or `keep`, with the proof. `tidy` alone prints the usage (exit 2) | nothing |
| `tidy apply` | carries the plan out: deletes the local branches it marks `delete`, removes clean linked worktrees of such branches and dead worktree registrations. No confirmation flag; `--yes` is an unknown flag | local branches, worktrees, a manifest outside the repository |
| `tidy restore` | re-creates the branches of a run from its manifest | local branches |
| `sync` | dry run: what would be fast-forwarded and why not. `--apply`: fetch `origin` (any remote kind gix can serve, see "Remote kinds") and fast-forward the checked-out branch, index and working tree included | only with `--apply` |

`sync --apply` never merges, rebases, stashes, resets or pushes. It refuses (exit 1, nothing changed) when a
tracked local change overlaps a file the update touches, when untracked files are in the way, when the branch
diverged, or when a merge, rebase or similar operation is running. Local changes elsewhere are kept.

## agent-rules

`sync` and `status` print one line about the rules repository (`wstein/dotagents`) when the workspace has a checkout:
`agent-rules: current | behind N commits of origin/<default> | ahead N commits ... | diverged | dirty | not on default branch | not found`.
In a whr workspace the checkout is the git submodule `.agents` (normally a detached HEAD at the commit the superproject
records); a plain checkout on the default branch works too. It is `--rules-path PATH` (`sync` and `status`), else
`<workspace>/.agents`; nothing else is searched. The line reads the local `origin/<default>` ("as of last fetch").

`sync --rules` handles that checkout like `sync` handles the repositories. Without `--apply` it changes nothing and fetches
nothing: it prints the current rules commit and what it would do from what is already fetched (how far `origin/<default>`
is ahead, the new commits and files). With `--apply` it fetches `origin` (not with `--offline`) and fast-forwards HEAD to
`origin/<default>`: the branch of a plain checkout, or the detached HEAD of a submodule. A checkout that is only ahead of
`origin/<default>` (or already current) is not an error: nothing to fast-forward, exit 0. The rules checkout is never part
of the generic repository loop: `sync` and `status` leave it out of the repositories list (the resolved `--rules-path` or
`.agents`), so only `--rules` ever touches it. The `tidy` commands skip it too (the default `.agents`): `--repo .agents` is an unknown repository there, and a plan entry naming it is not in this workspace. Refusals (exit 1, nothing changed) are only for cases where something
could be lost or the update cannot be done: changed files in the working tree; diverged (local commits not in
`origin/<default>` and new ones upstream); another branch than the default one checked out; the fetch failed; no checkout
at the path; the path is not a git repository; the checkout has no remote `origin`, no commits, or `origin` has no default
branch (no `origin/HEAD`, `origin/main` or `origin/master`, or `origin/HEAD` points to a missing ref); the checkout cannot be read; the fast-forward itself
refuses (changed files, untracked files in the way, unmerged paths); the commit walk or diff fails. There is no owner or author check. When the new
commits have authors or committers other than git `user.name`/`user.email` of the checkout (skipped when unset), one
info line lists them; nothing is refused for that.

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
| 0 | everything asked for worked (also when repositories need attention) |
| 1 | the command ran, but a repository could not be read; `sync --apply` failed in one; `tidy apply` failed a step (a branch, worktree, registration or the `info/exclude` file; the manifest could not be written); `tidy restore` left an entry not restored (`not-overwritten`, `missing-object`) |
| 2 | usage error or unusable workspace; nothing was done |

## Output for agents

stw executes what it does and never prints a command for someone else to run: no `next:` lines in the text output
and no `next_steps` field in JSON. Notes say what happened or why something was skipped.
`--json` prints one compact JSON object. `schema_version` is `2` (version 1 had `next_steps` in `status`, `tidy-plan`
and `sync`; there is no `next_steps` any more); fields are only added within a version, a
removal or a changed meaning raises it. Common top-level fields: `schema_version`, `command` (`status`,
`tidy-plan`, `tidy-apply`, `tidy-restore`, `sync`), `root`, `engine`, `repos`, `summary`. `status` and `tidy-plan` also carry `github` (bool),
`github_auth` (where the token came from, never the token) and `github_note` (why pull requests were not read).
Every repository entry has `name`, `path` and `error` (string or null). `tidy-plan` carries `read_only` (always true); `sync` carries `mode` (`dry-run` or `apply`).

`summary`: `status` {total, attention}; `tidy-plan` {delete, delete_with_worktree, review, keep} (`delete_with_worktree`: kept branches whose worktree `apply` will remove, so `apply` deletes them; they are not in `keep`. A worktree that `apply` leaves alone because it is dirty, locked or has unpushed commits keeps its branch in `keep`); `sync` {total, fast_forwarded,
would_fast_forward, skipped, errors}.

- `status` repo: `state`, `branch`, `detached`, `head`, `upstream`, `upstream_gone`, `ahead`, `behind`,
  `changes{staged,unstaged,untracked,conflicts}`, `stash`, `operation`, `gitlink{recorded,state}`, `slug`,
  `attention`, `reasons`, `pull_requests{read,error,open[{number,title,head,draft,author,review,merge_state}]}`. `review` of a pull request is always null (REST has no `reviewDecision`); `merge_state` is gh's `mergeStateStatus` value, taken from REST `mergeable_state`.
- `tidy-plan` repo: `base`, `base_commit`, `slug`, `note`, `branches[{branch,commit,decision,proof,detail,unique_commits,upstream,upstream_track,worktree}]` (`worktree` is null, or `{path,removable_proof,outside_worktrees_dir,blocker}` for a `keep`/`in-worktree` branch that `tidy apply` may remove with its worktree; `blocker` is null when `apply` will remove both, else the reason it leaves them),
  `stale_worktree_registrations[{path,locked,lock_reason}]`. Proofs: `in-worktree`, `open-pull-request`, `merged-ancestor`, `equal-patches`,
  `merged-pull-request-head`, `backup-older-than-days`, `backup-recent`, `pull-request-merged-branch-moved`, `upstream-gone`, `unmerged`.
- `sync` repo: `branch`, `upstream`, `ahead`, `behind`, `dirty_files`, `fetch` (`not-run`, `skipped-offline`,
  `ok`, `failed`, `no-remote`, `unsupported-remote`), `last_fetch_seconds_ago`, `remote_moved`, `result`
  (`none`, `fast-forward`, `fast-forwarded`, `skip`, `error`), `reason`.

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

Output: `--json` has `command` `tidy-apply` / `tidy-restore`, `run`, per branch `action`
(`deleted`, `skipped`, `failed`) and `reason`, `manifest` (path), worktrees and registrations.
It carries state and results only, no commands. Limits: a file created in a worktree after the check and before
the removal is lost with it; the age rule for backups is only as good as the commit date (above).

## GitHub access

Token order: `GH_TOKEN`, `GITHUB_TOKEN`, `~/.config/gh/hosts.yml` (`oauth_token`), macOS keychain (the item `gh`
creates). The token is never printed or logged. Without a token everything works except pull request data
(and the notes say so) and fetching private repositories (the error says to set `GH_TOKEN` or `GITHUB_TOKEN`).
Only `github.com` remotes are used for REST; the fetch offers the token to `github.com` over https only, as user
`x-access-token`. `STW_GITHUB_API_URL` points the REST calls at another base URL (tests, GitHub Enterprise).

## Remote kinds

`sync --apply` tries the fetch for every remote kind and reports what gix says. `https://` needs no other
program. For ssh remotes (`ssh://`, `user@host:path`) gix runs the system `ssh`, and for local paths and `file://` it
runs `git-upload-pack` (observed with fake programs on `PATH`; `tests/remote_kinds.rs`). When the fetch of a
non-https remote fails, the result is typed: `fetch: unsupported-remote`, `result: error`, exit 1, and the message
ends with `use git directly for it`. Until M0 this stays so; then these remotes are fetched via git. A failed https fetch is
`fetch: failed`. Credentials in a remote URL are never printed. `status`, `tidy plan` and dry-run `sync` never
fetch. `git://` was not exercised against a server (UNKNOWN beyond the error path).

## Build and test

```
cargo build --release          # target/release/stw
cargo test                     # unit and fixture tests, no network
cargo test --test network -- --ignored   # clones a public repo, rewinds it, fetches and fast-forwards
bash tests/mutation/mutate.sh SCRATCH    # 110 mutations, each must fail the named test
bash tests/oracle/shim_proof.sh stw ROOT SCRATCH   # fake git/gh/ssh first on PATH: 0 calls
```

`tests/oracle/*.sh` use the `git` binary **only as a reference** in tests (patch ID, fast-forward results).
Test helpers (dev-dependencies only, not in the binary): `proptest` (`tests/patchid_props.rs`, small case count),
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

`sync --apply` writes in this order: all checks first (dirty paths, overlap, untracked files in the way), then the
index lock (`.git/index.lock`, taken here and held until the index is committed), then the branch ref (the write
that can still fail for reasons no check sees: permissions, locks, reflog), then working tree files, then the
index (written into the lock, which is renamed over the index last). There is no transaction across ref, files
and index:

- A refused check, a held `index.lock` (another git process or a stale lock file) or a failed ref update leaves
  index, working tree and `HEAD` untouched (`tests/sync.rs`). The foreign lock file is not removed.
- If writing files fails, the ref is put back to the old commit (reflog entry `stw: undo fast-forward ...`). Files
  the checkout already wrote stay; the index was not touched. `git status` shows those files as changes.
- If writing or committing the index fails, the ref is put back; all files are already new, the index is the old
  one.
- If the ref cannot be put back either, the error names both failures. `HEAD` then stays on the new commit while
  files and index are old or partly new.
- If the process is killed (or the machine loses power) after the ref moved and before the index was written,
  `HEAD` points to the new commit while files and index are old or partly new; stw does not detect or repair this
  on the next run. `git status` shows the update as local changes. stw installs no signal handler, so a kill
  during the checkout or index write also leaves a stale `.git/index.lock`: remove that lock file first (only
  when no git process runs), otherwise git refuses every command that writes the index. To finish the update:
  `git reset --hard HEAD` (tested in a scratch repository; it also discards unrelated local changes). Without discarding anything
  else: `git checkout HEAD -- <paths>` with the paths of
  `git diff --name-only --diff-filter=d <old commit> HEAD`, then `git rm --cached` and remove the files of
  `--diff-filter=D`. A plain `git checkout HEAD -- <path>` fails for a path that the update deleted ("pathspec
  did not match"; tested). To go back: `git reset --hard <old commit>` (same caveat; not tested), where the old
  commit is the second-to-last entry of `git reflog show <branch>`.

## Known limits of `sync --apply`

It refuses, and changes nothing, in a few cases where git would proceed: a file that is deleted locally and
upstream, a rename that only changes the case of a path, and a tracked file that upstream replaces by a
directory (reported as "changes between file and directory"). A new submodule is checked out as an empty
directory, like git does; its content is not cloned. If the checkout itself fails halfway (disk full, permissions
on a single file), the ref is restored but files already written stay; `git status` shows them.

## Own code where gix has no API

- Patch ID (`git patch-id --stable`, for squash/rebase-merge detection): `src/patch_id.rs`. On 2424 real commits 2399 equal git (98.97 %);
  the others are 6 binary diffs and 19 text diffs with different hunk placement. A hunk-placement difference makes equal patches look different (a missed `equal-patches` proof).
  Paths are hashed as git writes them (quoted, raw bytes). Git ignores white space in file names too (`a b.txt` equals `ab.txt`), so the `equal-patches` proof also requires identical changed paths.
- Fast-forward with checkout: `src/sync/fast_forward.rs` (checks first, writes after; index tree cache removed by hand, gix issue 2421).
- Untracked collapsing for nested repositories: `src/status.rs`.
- Worktree registration HEAD (gix `Proxy` has none): `src/repo/worktrees.rs`.

gix has no API stability promise before 1.0: the pin is exact and updates are deliberate.

## Not verified

Automated fetch of a private repository over https (the credential answer is unit-tested; one manual run against a private repository worked); `git://` behavior of gix; conflicted
index during `--apply`; sparse checkout, `skip-worktree`, case-insensitive path collisions; LFS and filter drivers
during fast-forward; Linux and Windows (only macOS arm64 was run; keychain code is macOS-only); cold-cache timings.
