# Status and sync

[SourceSteward 101](../README.md) · [Command reference](command-reference.md) · [Review state in `stw status --review`](review-and-watch.md#review-state-in-stw-status---review) · [Local rebase proof](worktrees-and-handoff.md#local-rebase-proof-stw-prove-new)

Without a flag `sync` only fast-forwards: it never merges, rebases, stashes, resets or pushes. It refuses (exit 1,
nothing changed) when a tracked local change overlaps a file the update touches, when untracked files are in the way,
or when a merge, rebase or similar operation is running; a diverged branch is reported (`skip`) and left alone. Local
changes elsewhere are kept. `--rebase` and `--merge` (exclusive) handle a diverged branch with the installed `git`,
`--autostash` and `--no-autostash` (exclusive) choose the stash, see "`sync --rebase` and `--merge`".

## `sync --rebase` and `--merge`

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

## Submodules: the gitlinks of the index

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

## `sync --prune`, `--no-prune`, `--submodules`

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

### Open issues

Online status lists open issues for the selected GitHub repositories under their
repository cards. It uses the GraphQL issue, label, and assignee connections.
The REST counts remain a separate summary. If pagination or an API read fails,
the card retains the issues and metadata already read and marks the list
incomplete. An empty result is shown only after a successful read.

In `stw status --json`, each repository has an `open_issues` field.
The value is `null` when the query was skipped, for example with `--offline`,
without GitHub credentials, or without a GitHub repository slug. A query result
contains `id` (the GitHub repository node ID or null), `coverage`
(`complete` or `incomplete`), `error` (the first failure or null), and
`issues`. A successful query that finds no open issues reports
`"coverage": "complete"` and `"issues": []`. Each issue has
`number`, `title`, `url`, `updated_at`, `labels`, and `assignees`.

The [live acceptance recipe](../acceptance/rezept-stw-status-issues.sh) checks the
issue list across a main repository and two submodules. It requires a built
`stw` binary and a GitHub token with read access to the fixture repositories.

## Status flags

Ports of the flags of the Go `stw status` (`legacy/sourcesteward/internal/cli/status.go`). Without a flag nothing changes, and
`--json` is the same document plus the additive fields above. `--review`, `--timeout`, `--stats` and `--verbose` are a usage error (exit 2)
with `--offline`.

| Flag | What it does |
| --- | --- |
| `--long`, `-l` | The text only: under `Needs action` every reason is explained from the facts of the repository (what is staged, how far behind, which operation), under `Repositories` each repository gets a card (`path`, `remote`, `head`, `upstream`, `changes` by kind, `branches`, linked worktrees, `stash`, `gitlink`). The blocks stay in their order, `Notices` and the summary line stay last. The JSON is the same as without it. No command is printed for someone else to run. |
| `--fetch` | `git fetch origin` of every repository before it is read, so that ahead/behind and `Branches` are current. The one thing that is not read-only, and it changes remote-tracking refs only (`refs/remotes/origin/*`, FETCH_HEAD, objects). It goes through the same fetch as `sync --apply` (ssh and local-path remotes: the installed `git` through the runner, `"via": "git"`, fixed argv, no hook, `gc.auto=0`; https: gix, no program, the GitHub token only for github.com), always with `--no-prune` (wins over `fetch.prune`) and `--no-prune-tags` (so `pruneTags` creates no tag), no tags, no submodules. A repository whose configured fetch refspec writes outside `refs/remotes/origin/` is not fetched (it would write branches). A failed fetch is a detail line, a notice and `fetch.state` `failed`, exit 0; the status of that repository is still shown. It works with `--offline`: `--offline` stops the GitHub reads and the token, not the fetch. The `agent-rules` checkout is not fetched (`sync --rules --apply` does that). |
| `--worktrees` | One line per linked worktree under its repository: path, branch or `detached`, then `dirty`, `locked` (with `: reason`), `prunable`, `missing`. `dirty` is any tracked change or untracked file in that worktree (a worktree that cannot be read counts as dirty). Registrations whose checkout is gone are listed too (`missing`, and `prunable` unless locked); the main checkout is never listed. The `Worktrees` count keeps its meaning (existing linked checkouts, no stale registration, no main checkout). Sorted by path. |
| `--review` | Also reads the review state of the open pull requests from conversation comments and submitted GitHub reviews (each list bounded to 500 entries, none after `--timeout`), at most 16 reads at a time, at most 200 pull requests per run (repository order, newest pull request first; the rest is `not read` with a notice). Each pull request line gets `review <state>` (`clear`, `clear-carried`, `findings`, `no-review`, `unverified-author`, `?`), each repository a line `reviews: 1 clear, 2 ?` and, with `--long`, the evidence per pull request. See "Review state in `stw status --review`". `Counts` do not change. Not with `--offline` (usage error, exit 2). |
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
