# Implemented commands and examples

This guide describes the current main branch, not every installed version. Run
`stw version` and `stw --help` to check your checkout or binary. SourceSteward is
pre-release; Git and jj are native Tier 1 backends. GitButler integration is planned.

## What works today

Every data command supports human output and `--json`; help is available at each level.

| Commands | Use | Effects |
| --- | --- | --- |
| `stw help`, `stw --help`, `stw version` | Discover commands and version | No repository or credentials required |
| `stw inspect [PATH]` | Native repository and workspace identity | Read-only local observation |
| `stw inventory [PATH]` | Workspaces, references, base candidates, file scopes and true/false/unknown queries | Read-only local observation; no fetch or snapshot |
| `stw repos PATH... [--submodules]` | Inventory up to 16 repositories (Git and jj) in one bounded run with a cross-repository summary; optionally their direct Git submodules | Read-only local observation; no fetch or snapshot |
| `stw sync [PATH] [--plan] [--rebase\|--ff-only\|--merge] [--autostash\|--no-autostash] [--prune\|--no-prune]` | Do what `git pull` does in every repository of the workspace (root and nested submodules, main worktrees, any branch with an upstream), from any directory inside it; Git only | One `git fetch` per repository, then a journaled change of the current branch ref only. `--plan` (alias `--dry-run`) changes remote-tracking refs only |
| `stw status [PATH] [--offline] [--fetch] [--worktrees] [--icons SET] [--timeout D]` | What needs action in the workspace, ordered by priority with a command to run for each item, then every repository (branch, upstream, changes, conflicts, operation, stashes, unmerged branches, worktrees, gitlink drift) and the pull requests, release drafts, unreleased tags and CI of its GitHub repository; Git only locally | Read-only. Online by default with `GET` requests through the `gh` token of the hosting commands; `--offline` reads no credential and sends nothing; `--fetch` adds one `git fetch` per repository (remote-tracking refs only) |
| `stw workspace plan [PATH]` | Store an isolated Git worktree/jj workspace creation plan | Writes private plan state; does not create the workspace |
| `stw workspace apply` | Create the workspace described by a stored plan | Explicitly authorized local writes with stale-state checks |
| `stw workspace handoff [PATH]` | Describe an owned task and remaining work | Observes task/store evidence; does not launch an agent |
| `stw workspace retire-plan [PATH]` | Prepare retention and retirement evidence | Preparation only; does not delete a workspace |
| `stw tidy plan [PATH]` | Review branch/bookmark integration or owned-workspace retirement | No deletion; optional private stored plan |
| `stw tidy apply` | Apply the reviewed reference or retirement plan | Explicit local authorization; recovery evidence and fresh checks |
| `stw hosting repo [PATH]` | Resolve local GitHub repository candidates | Local only; no gh or authentication required |
| `stw hosting pr NUMBER [PATH]` | Inspect one explicitly numbered PR; `--json-version 2`, `--compact` and `--expect-head SHA` add lifecycle and merge evidence | Authenticated, bounded GitHub reads; one GET; no retry on rate limits |
| `stw hosting prs SHA [PATH]` | List the PRs GitHub associates with a commit (one page of 30) | Authenticated, one bounded GET; no PR selection, no retry |
| `stw hosting ci NUMBER [PATH]` | Inspect checks/statuses at that PR's exact head | Authenticated, bounded reads; no rerun or merge-eligibility claim |
| `gh jj repo`, `gh jj pr`, `gh jj prs`, `gh jj ci` | Use the same hosting operations as a gh extension | Requires jj ownership; `repo` is local, PR/CI read GitHub |

Native ownership chooses Git or jj, including jj for colocated repositories. Unsupported
or uncertain states refuse; raw Git never substitutes for jj. Read-only native jj
requires usable Linux bubblewrap or macOS sandbox-exec and reports stored-unsnapshotted
qualifications rather than claiming live disk freshness. See [backend readiness](backends.md).

## Release source commands (feature acceptance pending)

The hidden `stw release` group currently registers `check`, `plan`, `verify`,
`record`, `build` and `dispatch`; inspect them with `stw release --help`. These
are source implementations of planned, unreleased Spec 026, not a completed
release facility. `plan --online` adds bounded GitHub gate reads and
`verify --online` adds pinned attestation verification. Offline local checks
can pass while the verdict remains `incomplete` (exit 1). Source implementation
does not establish live workflow/release acceptance.

`check`, `plan` and `verify` execute no project commands; `record` does not build
and writes only when selected with `--write`. `build` executes declared project
commands and requires separate build authorization as well as its runtime
preconditions. `dispatch` performs online reads to print a stateless request plan;
only explicit `--apply PLAN_ID` attempts one workflow-dispatch POST, with no retry.
An accepted request is not a completed build. See the [current command/acceptance matrix](release.md),
[owner preparation](releasing.md), [backend limits](backends.md#release-source-capabilities)
and [unpublished source schemas](json-contracts.md#release-source-schemas-unpublished).

## Inspect a repository and query its state

From a source checkout, build the development executable once:

```sh
make build
./bin/stw --help
./bin/stw version --json
./bin/stw inspect . --json
./bin/stw inventory . --json
./bin/stw inventory . --query dirty --scope disk --scope untracked --json
./bin/stw inventory . --query ref-exists --ref topic --json
```

Examples below use `stw` on PATH (`go install ./cmd/stw`), or replace it with
`./bin/stw`. A known false query exits 0; unknown/incomplete evidence exits 1.
Check the query value and qualifications, not just the exit code. Reference selectors
are literal native names/identities, not arbitrary Git expressions or jj revsets.
See [inventory](inventory.md) for ancestry, paths, scopes and backend limits.

## Inventory several repositories

```sh
stw repos ~/work/app ~/work/lib
stw repos ~/work/umbrella --submodules --json
```

One result lists every path in argument order with its status, backend, error, derived facts
and the embedded `stw inventory` result. Exit 0 means every member was fully observed; exit 1
means a member is partial or failed, with the cause on its entry (gitlinks and stale worktrees
commonly are, and very long change or reference listings are truncated with a disclosure, because
the limits of single-repository inventory apply to each member). See [multi-repository inventory](repos.md).

## Bring the workspace and its submodules up to date

```sh
stw sync                      # root repository and submodules on a branch, like git pull
stw sync --plan ~/work/ws     # fetch only, say what a run would do
stw sync --rebase --autostash # override the configuration for one run
```

`stw sync` is a manual, Git-only command ([Spec 032](../specs/032-stw-sync/spec.md)) that does what `git pull`
would do in every repository of a workspace, from any directory inside it. It resolves the top-level superproject (also
from inside a submodule, a nested submodule or a linked worktree) and syncs that root and every initialized submodule,
nested ones included, parent first and then submodules depth-first. Only the main worktree of each repository is
synced, on whatever branch it sits (main, develop, a feature branch) when that branch has an upstream; linked worktrees
are never touched or listed (started inside one, a note says so). A detached HEAD (a pinned submodule) is reported
with its short OID and left alone, an uninitialized submodule is reported and never cloned. There is no apply step:
`stw sync` is the action, `--plan` (alias `--dry-run`) is the preview.

It is never stricter than `git pull`, with one deliberate exception (owner decision 2026-10-24): it never creates a merge commit on its own. Per repository it runs `git fetch --no-tags --no-write-fetch-head
--no-recurse-submodules` for the branch's upstream remote, then follows your Git configuration as `git pull`
does: `pull.rebase` and `branch.NAME.rebase` decide whether a diverged branch is rebased (`--rebase`/`--ff-only`
override), `pull.ff=only` forbids it, `rebase.autoStash` and `merge.autoStash` stash and restore a dirty tree
(`--autostash`/`--no-autostash` override) and `fetch.prune` prunes (`--prune`/`--no-prune` override). A branch that can be fast-forwarded is fast-forwarded, always without a merge commit (also with `--merge` and
`pull.ff=false`, which `stw sync` ignores). A diverged branch is rebased only with `--rebase` or when `pull.rebase` /
`branch.NAME.rebase` is `true`, `interactive` or `merges` (the last keeps local merge commits while rebasing, as git
does); it is merged only with the explicit `--merge`, which excludes `--rebase` and `--ff-only` (usage error, exit 2) and
overrides `pull.rebase` and `pull.ff=only`. No Git setting enables a merge commit: with `pull.rebase=false`, `pull.ff=true|false`,
`merge.ff` or none at all, a diverged branch is reported as `diverged` (`needs a choice: use --rebase or --merge`) and left unchanged. The earlier
`--no-rebase` and `--no-ff` flags are gone. `pull.ff=only` (or `--ff-only`) refuses a diverged branch even when `pull.rebase=true`; `--rebase` overrides it. A dirty tree (submodule changes ignored, as git does) is fine when the update touches
none of its files, as for `git merge --ff-only`; with a rebase configured it needs `rebase.autoStash` or `--autostash`, even for a fast-forward, as for `git pull --rebase`. A failure in one repository does not stop the others.

Output is one line per repository, then a summary line (colour only on a terminal with `NO_COLOR` unset):

```
SourceSteward sync: 3 repositories
.        main     fast-forwarded 2 commits 3f2a1b0..9c4d7e1
app      develop  skipped: diverged (2 ahead, 5 behind) - needs a choice: use --rebase or --merge, or set pull.rebase=true
app/lib  -        skipped: detached at 8d1e2f3 (pinned)
summary: 1 fast-forwarded, 1 diverged, 1 skipped detached
```

Results per repository: `up-to-date`, `ahead`, `fast-forwarded`, `rebased`, `merged` (plan: `fast-forward`, `rebase`, `merge`),
`diverged`, `skipped-dirty` (local changes the update would overwrite, or git refused), `skipped-detached`,
`skipped-no-upstream` (also when the remote branch is gone), `skipped-busy` (rebase, merge, cherry-pick, revert or
bisect in progress, or locked), `skipped-uninitialized`,
`conflict` (a rebase stopped on a conflict and is left in progress, as git leaves it; git's own message is printed),
`autostash-conflict` (the update is done, the stash is kept) and `failed`. Exit status: 1 only when the run is
refused (a jj-owned or non-Git path) or a repository ended `failed`, `conflict` or `autostash-conflict`; skipped
repositories exit 0; usage errors exit 2 before any effect.

Before each branch change a `sync` journal record (`.git/stw/sync/RUN_ID.json` of that repository) and a recovery
ref `refs/stw/recovery/sync/RUN_ID/N` are written and the record is completed afterwards; HEAD and `ORIG_HEAD` are
checked around `git merge --ff-only NEW_OID` and `git rebase`. A submodule HEAD moved by the superproject is not
touched. As its last step a run that completed a journaled branch change in a repository removes, in that repository,
the records and recovery refs of the runs older than the ten most recent completed runs (the current run is always
kept and counts as one); a run with an incomplete record is kept, a recovery ref is deleted only inside its own run's
namespace and while it still points at the journaled old OID, and no other ref is removed. A recovery ref that is
symbolic, has another name or a bad old OID is kept with its record. Retention is best effort and silent: a failure
leaves the older runs in place. Repositories that no run changes keep their old runs until their next change. Run IDs
carry microseconds from this release on (older IDs have second resolution), and runs are ordered by their ID.
`stw sync restore` comes in a later slice. See the
[JSON contract](json-contracts.md#sync-schema-v1-unpublished).

## Show what needs action in the workspace

```sh
stw status                  # needs action, every repository, notices, summary; from anywhere inside the workspace
stw status --offline        # the local state alone, no request and no credential
stw status --worktrees      # also list the linked worktrees
stw status --fetch --json   # fetch first, then one JSON object (schema 2)
```

`stw status` ([Spec 037](../specs/037-stw-status-overview/spec.md), amending [Spec 033](../specs/033-stw-status/spec.md)) is
the read-only "what needs me" view and has [its own page](status.md). It finds the workspace the way `stw sync` does and
changes nothing; only `--fetch` runs one `git fetch` per repository (remote-tracking refs only). It lists the items that
need action by priority (red, yellow, green, blue, violet), each with a link and a command that `stw` never runs itself, then
the Spec 033 block of every repository with a `remote:` line, the notices about anything that could not be read, and a
summary line. The local half marks a repository `!` when an operation is in progress, there are conflicts, staged, unstaged
or untracked files, it is behind its upstream or diverged, a branch has no upstream, HEAD is detached or a gitlink is behind
or unrelated; commits ahead, stashes, unmerged branches, linked worktrees and a merely *ahead* gitlink are shown but do not
count. The remote half reads pull requests, release drafts, unreleased tags and CI for each GitHub repository, with 15 s per
request and 90 s overall (`--timeout`). Exit status 0 also when there is something to do, when no token is available or
the deadline cut a read; 1 when a request that was made failed, a repository cannot be read or the run fails; 2 for usage
errors.

```
SourceSteward status: 3 repositories in /work/ws   (--offline)
! .         main     origin/main behind 2  1 unstaged
    stash 1
    9 linked worktrees (7 locked)
  app       develop  origin/develop up to date  clean
! app/lib   (detached)  detached at 8d1e2f3  clean
    gitlink: recorded 7d8a55f, checked out ahead (89a4673)
summary: 2 of 3 repositories need attention
```

JSON contracts: [schema 2](json-contracts.md#status-schema-v2-unpublished) online,
[schema 1](json-contracts.md#status-schema-v1-unpublished) with `--offline`.

## Watch pull requests

`stw watch` ([Spec 035](../specs/035-stw-watch/spec.md)) is the read-only pull request watcher that
prints transitions as JSON Lines; see [watch](watch.md).

## List open pull requests

`stw pulls` ([Spec 036](../specs/036-stw-pulls/spec.md)) prints a read-only, stateless snapshot of every open pull
request (bots included) of the workspace, grouped into `ready`, `checks`, `conflict`, `draft` and `blocked`; `--owner`
adds the repositories of another user or organisation. See [pulls](pulls.md).

## List open issues

`stw issues` ([Spec 038](../specs/038-stw-issues/spec.md)) prints a read-only, stateless snapshot of every open issue (pull requests
excluded, bots included) of the workspace, each with one priority (`red` for a `security` label, `yellow` when assigned to you or labelled
`bug`, `violet` for bots, `blue` otherwise) and a suggested `gh issue view` for red and yellow rows. `--label`, `--assignee` (also `@me` and
`@none`) and `--author` filter locally; `--owner` adds the repositories of another user or organisation. See [issues](issues.md).

## Create an isolated task workspace

Choose an existing source, an unused absolute destination and a private mode0700
state directory outside source/destination/repository metadata. Replace the example
paths and labels. Git may use `HEAD`; jj may use `@` as its native base.

```sh
stw workspace plan /path/to/source --base HEAD \
  --destination /path/to/task --task issue-42 --owner developer \
  --state-dir /path/to/private-state --json
```

Read the returned plan, effects and recovery limits. Only when you intend those local
writes, use its generated ID in a separate apply command:

```sh
stw workspace apply --plan PLAN_ID --state-dir /path/to/private-state --authorize --json
stw workspace handoff /path/to/task --state-dir /path/to/private-state --json
stw workspace retire-plan /path/to/task --state-dir /path/to/private-state --json
```

Git creates a detached worktree; jj creates a native named workspace/change. No setup
scripts, fetch or push runs. Retirement preparation does not remove registrations or
files. See [task workspaces](workspaces.md) for prerequisites and refusal cases.

## Review local cleanup

Select an explicit integration base and native branch/bookmark; do not assume a
particular trunk or remote name. This example stores a reference-cleanup plan:

```sh
stw tidy plan /path/to/repository --base main --ref finished-task \
  --state-dir /path/to/private-state --json
```

Ancestry evidence is bounded; missing upstreams alone do not prove work disposable.
Uncertain or workspace-used references retain. After reviewing the stored plan:

```sh
stw tidy apply --plan PLAN_ID --state-dir /path/to/private-state --authorize --json
```

Owned-workspace retirement is also implemented, with separate registration and
exact-path file selections. It is not implicit branch cleanup. Read the
[tidy guide](tidy.md) before using `--remove-registration` or `--remove-files`.
Recovery preserves declared evidence within documented limits; automatic restoration
and general checkpoint/resume are not implemented.

Explicit `--contract 3` adds Git merged-head proof (`--online --remote NAME`), local
tracking cleanup (`--tracking-remote NAME --online`) and separately declared completed
legacy retirement. Default invocations remain offline. Online apply needs both `--online`
and `--authorize` for its exact stored effects. See the [tidy guide](tidy.md) for canonical
paths, completion/protection repeats, precise native/provider proof and recovery limits.

## Inspect GitHub PRs and CI

```sh
stw hosting repo . --json
stw hosting pr 42 . --repo owner/project --json
stw hosting ci 42 . --repo owner/project --json
stw hosting pr 42 . --repo owner/project --json --json-version 2 --expect-head FULL_SHA
stw hosting prs FULL_SHA . --repo owner/project --compact
```

Repository resolution is offline. PR/CI requires gh 2.102.0+ and the caller's selected-host
authentication; it uses bounded go-gh/v2 HTTPS reads. No push, PR edit, merge or CI
rerun occurs. Ambiguous repository candidates require explicit selection. Check
completeness and exact head identities; sampled success does not establish merge eligibility.
See [hosting](hosting.md) for hosts, bounds, authentication and installation.

The optional extension is built separately and shares the same operations:

```sh
make extension
# Optional: installs into your gh profile; choose explicitly if another gh jj exists.
gh extension install ./bin/gh-jj
gh jj repo /path/to/jj-workspace --json
gh jj pr 42 /path/to/jj-workspace --repo owner/project --json
gh jj ci 42 /path/to/jj-workspace --repo owner/project --json
```

## Planned features — not commands available today

Stack inspection/plan (Spec 012), checkpoint/resume (013), conflict diagnosis/preflight
(014), local restacking (015), stacked PR coordination/writes (016), work transfer
(017), amend/fixup/split editing (018), upstream synchronization (019) and native
GitButler integration remain planned. Current inventory, recovery evidence and
read-only PR/CI do not implement those broader workflows. See the [roadmap](roadmap.md)
and [feature status](feature-status.json) for scope and evidence.
