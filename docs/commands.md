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
| `stw workspace plan [PATH]` | Store an isolated Git worktree/jj workspace creation plan | Writes private plan state; does not create the workspace |
| `stw workspace apply` | Create the workspace described by a stored plan | Explicitly authorized local writes with stale-state checks |
| `stw workspace handoff [PATH]` | Describe an owned task and remaining work | Observes task/store evidence; does not launch an agent |
| `stw workspace retire-plan [PATH]` | Prepare retention and retirement evidence | Preparation only; does not delete a workspace |
| `stw tidy plan [PATH]` | Review branch/bookmark integration or owned-workspace retirement | No deletion; optional private stored plan |
| `stw tidy apply` | Apply the reviewed reference or retirement plan | Explicit local authorization; recovery evidence and fresh checks |
| `stw hosting repo [PATH]` | Resolve local GitHub repository candidates | Local only; no gh or authentication required |
| `stw hosting pr NUMBER [PATH]` | Inspect one explicitly numbered PR | Authenticated, bounded GitHub reads |
| `stw hosting ci NUMBER [PATH]` | Inspect checks/statuses at that PR's exact head | Authenticated, bounded reads; no rerun or merge-eligibility claim |
| `gh jj repo`, `gh jj pr`, `gh jj ci` | Use the same hosting operations as a gh extension | Requires jj ownership; `repo` is local, PR/CI read GitHub |

Native ownership chooses Git or jj, including jj for colocated repositories. Unsupported
or uncertain states refuse; raw Git never substitutes for jj. Read-only native jj
requires usable Linux bubblewrap or macOS sandbox-exec and reports stored-unsnapshotted
qualifications rather than claiming live disk freshness. See [backend readiness](backends.md).

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

## Inspect GitHub PRs and CI

```sh
stw hosting repo . --json
stw hosting pr 42 . --repo owner/project --json
stw hosting ci 42 . --repo owner/project --json
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
