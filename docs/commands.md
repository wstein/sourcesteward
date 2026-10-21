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
| `stw sync [PATH] [--plan] [--remote NAME] [--prune]` (`--dry-run` is an alias of `--plan`) | Fetch one remote, classify every tracking branch and fast-forward it (`--plan` stops before the change); Git only | One `git fetch`; then fast-forwards of local branches, each journaled with a recovery ref. `--plan` changes remote-tracking refs only |
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

## Bring local branches up to date

```sh
stw sync --plan .
stw sync --prune .
stw sync --plan --prune --remote upstream --json ~/work/app
```

`stw sync` is a manual, Git-only command ([Spec 032](../specs/032-stw-sync/spec.md)). `--plan` (alias
`--dry-run`) only reports; without it the command applies the plan (fast-forward only: `--rebase`,
`--autostash`, `--submodules` and `restore` are later slices). It runs one
`git fetch --no-tags --no-write-fetch-head --no-recurse-submodules REMOTE` (default `origin`; `--prune` adds `--prune` and lists the
removed remote-tracking refs; without it no ref is pruned even if `fetch.prune` is set) and classifies each local branch that tracks that remote from the fresh
refs. The plan never changes local branches, worktrees, the index, tags, `FETCH_HEAD` or stashes. A run
fast-forwards a checked-out clean branch with `git merge --ff-only NEW_OID` in its worktree (HEAD must still be the
planned old OID before, the planned new OID and `ORIG_HEAD` the old OID after) and any other branch with `git update-ref`
and the expected old OID. Before each change a `sync` journal record (`.git/stw/sync/RUN_ID.json`) and a recovery ref
`refs/stw/recovery/sync/RUN_ID/N` are written; the record is completed afterwards. No merge commit is ever created, a
dirty worktree (tracked or staged changes) is skipped untouched, local branches are never deleted, and one failing
branch never stops the others. The repository's own hooks run as Git runs them. Results:

| Result | Meaning | Exit |
| --- | --- | --- |
| `fast-forward` (plan) / `fast-forwarded` (run) | upstream is ahead; the branch is (run) or would be (plan) fast-forwarded | 0 |
| `failed` | a run only: the change did not happen or was not verified; `reason` is `head-moved`, `raced`, `not-fast-forward`, `journal-failed: ...` or `git-failed: ...` (a hook exiting non-zero) | 1 |
| `up-to-date`, `ahead` | equal to, or only ahead of, its upstream | 0 |
| `upstream-gone` | the remote-tracking ref is missing (visible after `--prune`); the branch is kept | 0 |
| `skipped-dirty` | a fast-forward is due but its worktree has tracked or staged changes (untracked files do not count) | 1 |
| `diverged` | local and upstream both have new commits; never merged | 1 |

Branches without an upstream under the selected remote are only counted (`untracked`). Exit 1 also means
the run failed (a jj-owned or non-Git path, an unknown remote, a failed fetch, more than 1024 branches); `--json`
then still prints the result with `error` set. Usage errors exit 2 before any effect. Retention of old runs and `stw sync restore` come in later slices.
Output is one line per branch (`BRANCH WORKTREE RESULT OLD..NEW`) and a summary; see the
[JSON contract](json-contracts.md#sync-schema-v1-unpublished).

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
