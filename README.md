# stw (SourceSteward)

Implementation scope, feature decisions, dependencies, and acceptance gates: [Rust migration plan](docs/migration-plan.md).

Everyday git and GitHub workflows for agents and people, in one binary. Written in Rust on
[gitoxide](https://github.com/GitoxideLabs/gitoxide) (`gix`, pinned to `=0.90.0`) and `clap`.
stw starts the installed `git` in one place only, the runner (`src/git_runner.rs`; `tests/it/no_binaries.rs` allows a process
nowhere else): today for the fetch of `sync --apply` with ssh and local-path remotes. `status`, `tidy` and `sync` start no
program for any repository content: gix reads only, filter drivers (`filter.<name>.clean/smudge/process`) are dropped
from the in-memory configuration (a file with a `filter=` attribute is then compared unconverted, so a content-rewriting
driver such as git-lfs can make it look modified, and `sync --apply` refuses, before any write, a fast-forward that
would write such a path), https fetch uses no credential helper, and an empty `PATH` still serves
every gix read. There is no `gh` and no `ssh` of stw's own. Later `push`,
`range-diff` and conflict resolution in a worktree use the same runner. Each step that ran through it carries `"via": "git"`
in the JSON output; without a usable `git` (missing, older than the step needs, unknown option) the step reports
`unsupported` (exit 1, typed). See `chartroom/notes/stw runs git only for push, fetch over ssh, range-diff and resolve.md`.

jj (Jujutsu) is not supported (owner decision 2026-11-11). Git is the standard; functions that jj offers (undo/oplog,
conflict resolution) are added to stw instead of maintaining a second repository model. jj is an officially unsupported
tool: it may work in a colocated Git repository, without a test and without a promise. stw has no jj special code (no
detection, no warning, no test).

```
stw status    [--long|-l] [--fetch] [--worktrees] [--review] [--icons unicode|nerd|ascii] [--timeout DURATION] [--stats] [--verbose]
              [--json] [--offline] [--root PATH] [--repo NAME]... [--rules-path PATH]
stw tidy plan  [--json] [--offline] [--root PATH] [--repo NAME]... [--all] [--backup-days N]
stw tidy apply   [--plan FILE] [--manifest-dir DIR] [--backup-days N] [--json] [--offline] [--root PATH] [--repo NAME]...
stw tidy restore [--run ID] [--branch NAME] [--manifest-dir DIR] [--json]
stw sync      [--apply] [--prune | --no-prune] [--submodules] [--json] [--offline] [--root PATH] [--repo NAME]...
              [--rules] [--rules-path PATH]
stw add       OWNER/REPO [PATH] [--dry-run] [--json] [--root PATH]
stw review carry-over [--pr N|none] [--fetch] [--checkout PATH] [--json] OLD_BASE OLD_HEAD NEW_BASE NEW_HEAD
stw lint pr   PR [PATH | --repo OWNER/NAME] [--owner-identity "Name <email>"]... [--human | --json]
```

| Command | What it does | Writes |
| --- | --- | --- |
| `status` | counts per repository and in total, then per repository: branch, upstream ahead/behind, staged/unstaged/untracked/conflicts, stash, running operation, submodule pin drift, open pull requests | nothing; with `--fetch` only the remote-tracking refs of `origin` (and FETCH_HEAD, objects) |
| `tidy plan` | the plan only: per local branch `delete`, `review` or `keep`, with the proof. `tidy` alone prints the usage (exit 2) | nothing |
| `tidy apply` | carries the plan out: deletes the local branches it marks `delete`, removes clean linked worktrees of such branches and dead worktree registrations. No confirmation flag; `--yes` is an unknown flag | local branches, worktrees, a manifest outside the repository |
| `tidy restore` | re-creates the branches of a run from its manifest | local branches |
| `add` | binds an EXISTING GitHub repository as a project (git submodule) of the workspace, see "stw add" | the clone directory, `.gitmodules`, the index, `.git/modules`, one local commit |
| `lint pr` | the commits, the text and the comments of one pull request against the machine-checkable working rules, see "PR lint" | nothing |
| `sync` | dry run: what would be fast-forwarded and why not. `--apply`: fetch `origin` (any remote kind gix can serve, see "Remote kinds") and fast-forward the checked-out branch, index and working tree included | only with `--apply` |

`sync --apply` never merges, rebases, stashes, resets or pushes. It refuses (exit 1, nothing changed) when a
tracked local change overlaps a file the update touches, when untracked files are in the way, when the branch
diverged, or when a merge, rebase or similar operation is running. Local changes elsewhere are kept.

### Submodule paths of `.gitmodules`

A path in `.gitmodules` is untrusted. It becomes a repository of `status`, `sync`, `tidy plan` and `tidy apply` only if it
passes the rules of `stw add` (`[A-Za-z0-9._/-]`, relative, no `..`, no `-x` component, no git-special name), no component
is a symbolic link, and the real path of the directory is below the real path of the workspace root. Any other path is
never listed and nothing there is read or written (no fetch, no fast-forward, no PR lookup). The repository name is the
normalized path (no trailing `/`); a directory named twice is listed once. The refusal is reported once per path, escaped,
as `submodule path not accepted: <path>: <reason>` (a path with a space or other character outside the set: `rename with git
mv; stw handles only [A-Za-z0-9._/-]`), and never changes the exit code, except `sync --apply --submodules` (see below):

- `status`, `tidy plan`, `tidy apply`: a line in the notices (`status`: `Notices`; `tidy`: a parenthesized line at the end), exit
  unchanged; in JSON the additive top-level field `rejected_paths` `[{path (escaped), reason}]` (`schema_version` stays 2).
  No entry in `repos`, nothing in the counts or the repository total. `tidy apply` never touches such a path.
- `sync`: dry run and `--apply` without `--submodules`: an entry in `repos` with result `skip` and the text as `reason` (exit
  unchanged). `sync --apply --submodules`: result `error` ("unreachable: ..."), exit 1, like Go's `unreachable`, which arises
  only there; the dry run with `--submodules` shows `skip` ("would be unreachable").
- Not reported when `--repo` names repositories; an unknown `--repo` (also a refused path) is a usage error.

Deviation from Go: Go lists the gitlinks of the index (`ls-files --stage`, `sync.go:262-276`; absolute paths and `..` are
filtered there, nested submodules are walked) and checks charset and symbolic links only for `sync --submodules --apply`
(`validSubmodulePath`/`symlinkedPath`, `sync.go:166`). Rust lists the paths of `.gitmodules` and checks them in every command.

### `sync --prune`, `--no-prune`, `--submodules`

Ported from the Go flags; like everything in `sync` they act only with `--apply`. The dry run says what they would do and
writes nothing (it never fetches).

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
  Dry run: `prune` (`on`/`off`) and, with `--prune`, `tracking_refs` (how many refs could be reached); which of them are gone
  is only known from the remote, so the dry run does not name them.
- `--submodules`: after every repository was synced, each pinned submodule of `.gitmodules` (checked out, detached HEAD) is
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
1. Rust is dry-run by default and the flags act only with `--apply` (Go acts by default, and its `--plan` fetches). The dry
   run shows the prune scope and `would-move` for the submodules instead of nothing. The dry run predicts a move from the
   gitlink in the index before the superproject's own fast-forward; `--apply` takes the gitlink after it, so the two can differ.
2. `--prune` is always done by the installed git, also for an `https://` remote (gix cannot prune, and stw deletes refs
   through git's own fetch only). That git gets no GitHub token from stw: a private https repository needs a git credential
   helper (the error says so). Without a flag gix never prunes an `https://` remote; Go always used git and so `fetch.prune`.
3. `--prune` refuses, in the apply, a `remote.origin.fetch` that writes outside `refs/remotes/origin/` (`refused-refspec`,
   nothing fetched); Go checked it in the plan only. Tags are never pruned (`--no-prune-tags` wins over `remote.origin.pruneTags`).
4. Only `origin` is fetched and pruned; Go prunes the remote of the branch.
5. JSON `pruned` is a list of ref names (Go: a number); `summary.pruned` is the total.
6. Rust visits the top-level submodules of `.gitmodules` only (its repository list is flat); Go walks nested ones depth-first.
7. A submodule path outside `[A-Za-z0-9._/-]` or through a symbolic link is `unreachable` (Go: only absolute, `..`,
   option-like and control characters). Paths from `.gitmodules` are reported escaped.
8. `unreachable` is result `error` (apply) or `skip` ("would be unreachable", dry run); Go has the result `unreachable`.
9. `--apply` fetches `origin` of a pinned submodule too (Rust fetches every repository; Go fetches branch repositories only).
   A recorded commit that `origin` has is then present before the move, where Go says `unreachable`. If that fetch fails
   (private https without token, `unsupported-remote`, `refused-refspec` with `--prune`), the result is `error` and the
   submodule is not moved; Go would still move it. Chosen because a move after a failed fetch would act on a state the
   run could not refresh, and the failure stays visible.
10. A submodule config that names a `filter.*` driver or another key that starts a program is refused
    (`refused-config`); nothing moves.
11. The superproject's recorded commit comes from the index (as in Go); an uninitialized submodule is listed only without
    `--repo`. The submodule's local-change check uses gix status (tracked changes, untracked files excluded); whether a
    change inside a nested submodule counts is UNKNOWN (Go ignores it: `--ignore-submodules=all`).
12. `--no-prune` also passes `--no-prune-tags`; Go gives `--no-prune-tags` not along, so with `fetch.pruneTags` or
    `remote.origin.pruneTags` configured Go's fetch creates tags there. (`sync --apply` without a prune flag keeps the Go
    behavior, see "Status flags".)

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
| 0 | everything asked for worked (also when repositories need attention, and in `status` also when a GitHub read failed: that is a notice, Go exits 1 there) |
| 1 | the command ran, but a repository could not be read; `sync --apply` failed in one; `tidy apply` failed a step (a branch, worktree, registration or the `info/exclude` file; the manifest could not be written); `tidy restore` left an entry not restored (`not-overwritten`, `missing-object`) |
| 2 | usage error or unusable workspace; nothing was done |
| 3 | `lint pr` only: an error finding, or a list cut at its bound (the result is printed and is not a pass) |

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

- `status` repo: `counts` (below), `state`, `branch`, `detached`, `head`, `upstream`, `upstream_gone`, `ahead`, `behind`,
  `changes{staged,unstaged,untracked,conflicts}`, `stash`, `operation`, `gitlink{recorded,state}`, `slug`,
  `attention`, `reasons`, `pull_requests{read,error,open[{number,title,head,draft,author,review,review_state,hints,base,default_branch,merge_state}]}`. `review` of a pull request is null unless `--review` read it (REST has no `reviewDecision`; then the status label of the review state, see "Review state in `stw status --review`"); `review_state` is null or the state document; `merge_state` is gh's `mergeStateStatus` value, taken from REST `mergeable_state`.
  Additive, from the flags (see "Status flags"): top level `options{fetch,worktrees,icons,review}` (`icons` is always null: the JSON carries no glyphs), per repository `pull_requests.review` (null without `--review`, and also with it when the pull request list of the repository was not read; else `{states{clear,clear_carried,findings,no_review,unverified_author,unknown},hints{clear_not_ready,base_not_default},not_read,limit}`), per pull request `review_state`, `hints`, `base` and `default_branch` (below) and, only with `--stats` or `--verbose`, `stats{requests,rate_limit_limit,rate_limit_remaining,rate_limit_reset,duration_ms}` (the three rate-limit values are null until GitHub reported them; `rate_limit_reset` is `YYYY-MM-DDTHH:MM:SSZ`; no URL, no token); per repository `fetch{state,via,error}` (`state`: `not-run`, `ok`, `no-remote`, `failed`, `unsupported-remote`, `unsupported`; `via`: `git`, `gix` or null), `fetch_error` (the error text of a failed fetch, else null) and `worktree_list` (null without `--worktrees`, else `[{path,branch,detached,dirty,missing,locked,prunable}]`: `locked` is null or the lock reason, empty when none was given; the main checkout is not in it).
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
- `tidy-plan` repo: `base`, `base_commit`, `slug`, `note`, `branches[{branch,commit,decision,proof,detail,unique_commits,upstream,upstream_track,worktree}]` (`worktree` is null, or `{path,removable_proof,outside_worktrees_dir,blocker}` for a `keep`/`in-worktree` branch that `tidy apply` may remove with its worktree; `blocker` is null when `apply` will remove both, else the reason it leaves them),
  `stale_worktree_registrations[{path,locked,lock_reason}]`. Proofs: `in-worktree`, `open-pull-request`, `merged-ancestor`, `equal-patches`,
  `merged-pull-request-head`, `backup-older-than-days`, `backup-recent`, `pull-request-merged-branch-moved`, `upstream-gone`, `unmerged`.
- `sync` repo: `branch`, `upstream`, `ahead`, `behind`, `dirty_files`, `fetch` (`not-run`, `skipped-offline`,
  `ok`, `failed`, `no-remote`, `unsupported-remote`), `last_fetch_seconds_ago`, `remote_moved`, `result`
  (`none`, `fast-forward`, `fast-forwarded`, `skip`, `error`; with `--submodules` also `submodule-move`, `submodule-moved`), `reason`.
  Additive: `prune` (`on`/`off`, only with `--prune`/`--no-prune`), `pruned` (names deleted by the fetch; with a flag, or when a ref was pruned),
  `tracking_refs` (dry run with `--prune`), `submodule` {state, old, new, note} (`--submodules`); top level `options` {prune: true/false/null, submodules}
  and in `summary` `submodules_moved`, `would_move_submodules`, `pruned`.

## Status text

`stw status` prints blocks separated by a blank line: `Counts` (one aligned line per repository, then `All`:
`PRs n (bot n) | Issues n (bug n) | Branches local/remote | Worktrees n | Stash n`; `n/a` is not read, `?` is failed, `n+` is a
lower bound at a full page), `Needs action (N)` (repositories with an attention reason), `Repositories (N)`, `Notices (N)`
(why GitHub was not read, failed reads per repository; text from outside is shown with escapes), the `agent-rules` block, then the summary line. A failed GitHub read is
a notice: exit 0. Exit 1 stays for a repository that cannot be read locally.

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
| `--stats` | One line after the summary: `stats: API: 4 reqs (4,896/5,000 left, reset 22:13 UTC) \| 1.4s elapsed (local 110ms, remote 1.3s)`. `local` includes the fetch of `--fetch`; `elapsed` ends before the `agent-rules` read. With `--json` the member `stats`. |
| `--verbose` | Implies `--stats`. Before the stats line: `Diagnostic breakdown:` with the requests and the summed request time per repository and source (`pulls`, `issues`; the comment reads of `--review` count as `pulls`). With `--json` the breakdown and the stats line go to stderr, so stdout stays one document. |

### Differences from the Go `stw status` flags

- `--long`: the Go default is a compact overview (one line per repository), `--long` the expanded one. The Rust default text is
  not compact: it already prints the detail lines of Go's long layout (operation, stash, gitlink drift, pull requests), and
  `--long` adds the card and the explanations on top. There is no compact mode. The explanations are sentences from the facts;
  Go prints the suggested command (`run:`) and a `hint:` per item, Rust never prints a command (see "Output for agents"). Go's
  action items with priorities (red to violet), their links and the sources release drafts, tags and CI do not exist in Rust:
  the remote half is pull requests and issues.
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
- Correction candidate, not changed: `sync --apply` without `--prune`/`--no-prune` runs `git fetch --no-tags` without
  `--no-prune-tags` (as Go `stwsync` does), so `fetch.pruneTags=true` or `remote.origin.pruneTags=true` makes it create tags.
  `sync` is a mutating command and keeps the Go behavior until the owner decides.
- `--timeout`: Go's deadline also covers its other sources (repo, releases, tags, CI, viewer) and cuts a request at 15 s; Rust cuts
  at 30 s. A failed GitHub read is a notice and exit 0 in Rust (Go: exit 1), see "Exit codes". The notices come per repository
  (Go: once per source).
- `--stats`, `--verbose`: Go counts every source it has; Rust has `pulls` and `issues` (and `other`). `cached 304s` is always 0
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

`sync --apply` fetches `origin` by remote kind:

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
and GitHub tokens are never printed. `status`, `tidy plan` and dry-run `sync` never fetch. `git://` was not exercised
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
  per run; the fetch needs 2.30.0 (UNKNOWN below 2.47.3: only that version was run). A spawn that fails with ETXTBSY is retried.
- Errors are typed (`GitError`) and redacted: URL user info, GitHub token shapes, `Authorization:` values and the values of
  `GH_TOKEN`/`GITHUB_TOKEN` never reach output.
- Not done: Windows (executable lookup, path form), `range-diff` minimum version, push.

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
- A rate limit answer (429, or 403 with an exhausted limit or `Retry-After`) stops the comment reads that have not started, in all repositories: they are `?` and one notice `rate limit reached, N reads skipped (review state ?)` names them (deviation 9). Reads in flight end on their own.
- Requests: one read per pull request (up to 6 requests for a very long thread), at most 16 at a time, at most 200 pull requests per run; with `--stats` or `--verbose` they count as `pulls`.

Deviations from the Go `--review` (`internal/pulls/review.go`, `internal/reviewstate`; Go cases `state_test.go`):

1. States: Go has `none`, `findings`, `clear`, `clear-carried`, `clear-stale`, `unparsed`; Rust has the M3 states `no-review`, `findings`, `clear`, `clear-carried`, `unverified-author`, `unknown`. A CLEAR of another head is `no-review` with the finding `stale-head` (Go: `clear-stale`); an unparsable or wrong-pull-request line is `unknown` or a finding (Go: `unparsed`); a failed read is `unknown` (Go: the review stays unset and the source of the repository is an error).
2. Grammar: the M3 parser, not the Go regular expression (exactly 40 hex patch-ids, no CR, `pr=` of any width from 1 without leading zeros; see `docs/migration-plan.md`, deviations D1 to D8). A line for another pull request is the finding `wrong-pr`, not `unparsed`.
3. The commit date of the head is not read: Go reads one commit per pull request whose comments name the head and ignores comments older than that commit. Rust has no second read; the verdict is bound to the exact head id instead (M3), so a comment can only name a commit id that existed when it was written. A comment that was edited later still counts at its `created_at` (Go ignores a comment older than the head commit): an old CLEAR that its author edited afterwards to name the head is `clear`, and only the flag `edited` (shown by `--long` and in the JSON) marks it. That grants nothing new: only the author or an administrator can edit a comment, both are trusted authors, and a later FINDINGS wins.
4. Carry-over: the chain rules of M3 (up to 16 links, patch-ids of all links equal, a `FINDINGS` of a chain head ends the chain) instead of Go's up to 8 links; the patch-ids are compared as declared (`stw review carry-over` proves them from git, `stw status` does not).
5. Trust is per login: a login is trusted when one of its comments has the association OWNER, MEMBER or COLLABORATOR (Go: per comment). Go's `ignored` count and the hint `foreign-review-ignored` are the findings `untrusted-author` and the state `unverified-author`.
6. Long threads: Go reads the first page and the last five pages and marks the state `truncated` (hint `truncated-review-unknown`); Rust reads from the first page up to the budget (500 comments) and then says `unknown` with the reason. A verdict on a page that was not read is never guessed.
7. Hints: `clear-not-ready` (state `clear` or `clear-carried` and the pull request is a draft) and `base-not-default` (the base branch differs from the default branch) are ported with Go's conditions and names; they need no read of their own. Differences: the default branch comes from `base.repo.default_branch` of the pull request list, not from a repository read (Go: `RepoInfo`), and a pull request without `base.ref` or without a default branch has no `base-not-default` (Go: only an unknown default branch has none); the comparison is exact, as in Go. Not ported, on purpose: `no-final-review`, `truncated-review-unknown` and `foreign-review-ignored` (the states `no-review`, `unknown` and `unverified-author` already say it), `ready-mergeable` (needs the verdict groups of the Go readiness check, which Rust does not have here, so the Go counts per group do not exist either). Not ported, follow-up candidate: `parent-merged-retarget` (one more read of the closed pull requests per repository with stacked pull requests).
8. Output: Go prints one count line per repository (`reviews: 1 clear, 2 none (hints: ...)`) and the JSON `pulls.data.review{states,hints}`; Rust prints the same kind of count line (with the two hints), the state and the hints of every pull request in the text and in the JSON (`review`, `review_state`) and `pull_requests.review{states,not_read,limit}`. The JSON documents are not the Go ones (see the migration plan).
9. Limit: Go reads the rows of the pulls cycle within `--limit` (default 200, drafts included, newest first); Rust reads at most 200 pull requests per run, in repository order, each repository newest first (a repository has at most 100 open pull requests in the list), with no `--limit` flag. Go reads four repositories at a time and the comments of a repository one after the other; Rust reads at most 16 comment lists at a time. Go stops every further request after the first rate-limit evidence; Rust does the same for the comment reads: after a 429 (or a 403 with `x-ratelimit-remaining: 0` or `Retry-After`) seen by any request of the run, no comment read starts any more, in any repository; the reads in flight (at most 15 besides the one that was limited) end on their own, the ones not started are `?` with the read error `not read: GitHub had reached the rate limit, ...` and are named by one notice for the run (`rate limit reached, N reads skipped (review state ?)`), the exit stays 0. Differences: Go also stops the other sources of the run (releases, issues, CI), Rust stops only the comment reads of `--review` (the pull request and issue lists are read before them); Rust does not wait or retry, as Go.
10. Go exits 1 when a review read failed (a failed remote source); Rust prints the notice and exits 0, as for the other remote reads.

## Carry-over check: `stw review carry-over` (M4)

`stw review carry-over --pr <N|none> [--fetch] [--checkout PATH] [--json] OLD_BASE OLD_HEAD NEW_BASE NEW_HEAD` (read-only; posts
nothing; `stw` does not read the review it is about). Full lower-case 40-hex ids. All three conditions must hold: (1) equal patch-id of
`BASE...HEAD`, old and new, each against its own base (in process, `via: gix`); (2) no merge commit in `NEW_BASE..NEW_HEAD` (gix);
(3) `git range-diff OLD_BASE..OLD_HEAD NEW_BASE..NEW_HEAD` shows `=` for every commit at the same position (`via: git`, Git 2.40+).
A step that could not run is `unknown`; the answer is then no. Only for a yes the output ends with
`Carried-over: from=<old head> to=<new head> patch-id=<40 hex> pr=<n|none>` (a line of the M3 grammar). Exit 0 yes, 1 no or a
step could not run (result printed), 2 usage. `--fetch` fetches `refs/pull/N/head` of `origin` into `refs/stw/carry-over/pr-N`
(`via: git`, for an ssh (also `ssh+git://`, `git+ssh://`), scp-like, `file://` or local-path `origin` only, with `GIT_ALLOW_PROTOCOL=file:ssh` so that a `url.<base>.insteadOf` of the user's config cannot turn it into https; an http, https or git:// `origin` is refused typed (`transport`, exit 1, answer no) before git starts, because the https fetch stays with gix and is not built for this refspec; a `core.sshCommand` of the user's config runs as with `FetchOrigin`; no tags, no FETCH_HEAD, empty refmap) and requires it to equal `NEW_HEAD`.
The range-diff step pins what could hide a change (empty attribute source, no `diff.ignoreSubmodules`, size limit, context) and refuses,
typed unsupported, a repository config with `diff.external`, `diff.<driver>.textconv|command` or an active `info/attributes`.
`git range-diff` is told `--no-notes`. It also says no where Go says yes when only the author of a commit changed, and for an
identical patch twice in a range (range-diff pairs it across). JSON schema 2 (`pr`: number or `"none"`; `via` per step; `read_errors`). Deviations from the Go command and the mapping of its 46
tests: header of `tests/it/carryover.rs`; plan: `docs/migration-plan.md`, "Next slice".

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

## Build and test

```
cargo build --release          # target/release/stw
cargo test                     # unit and fixture tests, no network
cargo test --test it -- --ignored network   # clones a public repo, rewinds it, fetches and fast-forwards
bash tests/mutation/mutate.sh SCRATCH    # 239 mutations, each must fail the named test
bash tests/oracle/shim_proof.sh stw ROOT SCRATCH   # fake git/gh/ssh first on PATH: 0 calls
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

`sync --apply` writes in this order: all checks first (dirty paths, overlap, untracked files in the way), then the
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

## Known limits of `sync --apply`

It refuses, and changes nothing, in a few cases where git would proceed: a file that is deleted locally and
upstream, a rename that only changes the case of a path, and a tracked file that upstream replaces by a
directory (reported as "changes between file and directory"). A new submodule is checked out as an empty
directory, like git does; its content is not cloned. If the checkout itself fails halfway (disk full, permissions
on a single file), the ref is restored but files already written stay; `git status` shows them.

## Own code where gix has no API

- Patch ID (`git patch-id --stable`, for squash/rebase-merge detection): `src/patch_id.rs`. On 2424 real commits 2399 equal git (98.97 %);
  the others are 6 binary diffs and 19 text diffs with different hunk placement. A hunk-placement difference makes equal patches look different (a missed `equal-patches` proof).
  Paths are hashed as git writes them (quoted, raw bytes). Git's ID ignores white space in file names and in file
  content (`a b.txt` equals `ab.txt`; `\tcc x` equals `  cc   x`), so the `equal-patches` proof also requires an
  identical digest of the diff lines with their white space (file names included): a change that differs from main
  only in white space keeps the branch.
- Fast-forward with checkout: `src/sync/fast_forward.rs` (checks first, writes after; index tree cache removed by hand, gix issue 2421).
- Untracked collapsing for nested repositories: `src/status.rs`.
- Worktree registration HEAD (gix `Proxy` has none): `src/repo/worktrees.rs`.

gix has no API stability promise before 1.0: the pin is exact and updates are deliberate.

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
index during `--apply`; sparse checkout, `skip-worktree`, case-insensitive path collisions; LFS and filter drivers
during fast-forward; Windows, and Linux beyond the test suite (the original measurements ran on macOS arm64; the M0 review and tests ran on Linux aarch64; keychain code is macOS-only); cold-cache timings.
