# sourcesteward (stw)

![SourceSteward — Explainable version control](docs/assets/banner.svg)

[![CI](https://github.com/wstein/sourcesteward/actions/workflows/stw-ci.yml/badge.svg)](https://github.com/wstein/sourcesteward/actions/workflows/stw-ci.yml)
[![Docs](https://img.shields.io/badge/docs-SourceSteward-0b2545)](https://wstein.github.io/sourcesteward/)
[![Line coverage](https://wstein.github.io/sourcesteward/coverage.svg)](https://github.com/wstein/sourcesteward/actions/workflows/stw-pages.yml)
[![GitHub release](https://img.shields.io/github/v/release/wstein/sourcesteward)](https://github.com/wstein/sourcesteward/releases)
[![License EUPL-1.2](https://img.shields.io/badge/license-EUPL--1.2-0b2545)](LICENSE)

`stw` is a Rust CLI for working on Git changes before, during, and after
review. It helps you put a late fix into the right commit, find conflicts
before rebasing a branch stack, see how refs and commits changed, and
hand off work without losing the difference between staged and unstaged
changes.

> **Experimental software, provided without warranty.** Preview commands
> such as `stw absorb` and `stw restack plan` leave the repository untouched.
> Commands such as `stw absorb --apply` can rewrite Git history. Review the
> plan and try write operations on a disposable clone first.

## Motivation

A late fix often belongs in an earlier commit. A rebase can block a whole
branch stack at one file. A handoff can lose which changes were staged or
which revision was reviewed. SourceSteward gives each of those decisions a
preview you can read, save, or pass to another person or agent. The same
tools work in a standalone Git checkout or across a workspace with
submodules where the command supports workspace scope.

## SourceSteward 101

Install from this checkout with Cargo and the pinned Rust toolchain.

```sh
cargo install --locked --path .
stw --version
```

### Put staged fixes into the commits they belong to

```sh
stw absorb --json
```

Stage the intended edits first. `absorb` shows which local, unpublished
commit would receive each hunk, which hunks it would skip, and how many
commit SHAs would change. To apply the reviewed plan, pass its checksum to
`stw absorb --apply --plan-sum <sum>`. Apply rechecks the plan and the
resulting tree, leaves unstaged edits in place, and records a backup ref and
an undo command.

### Find rebase conflicts before moving a branch

```sh
stw restack plan --onto origin/main
```

Run this from a feature branch or name the branches from bottom to top.
The plan replays the stack in memory and reports the first conflicting
level, affected paths, and blocked descendants. Use
`stw restack conflict-report` to save the conflict and available blob versions
outside the source repository. A second person or agent can inspect them
with `stw restack show-conflict` and use that evidence to prepare a resolution.
When the rebase is complete, `stw prove` can check that the rewritten history
contains the same changes on the new base.

### Trace a rewrite and preview an undo

```sh
stw oplog timeline
stw oplog evolog HEAD
stw oplog undo-plan topic
```

The operation log combines Git reflogs and recorded ref updates into a
timeline. `evolog` follows recorded rewrites across amends and rebases.
Optional repository hooks capture future Git operations and make those
records available to the next person or agent. `undo-plan` checks whether
one local branch can return to an older reflog value. Applying the undo
checks the branch's expected current SHA and saves a backup ref.

### Save a handoff or review only the selected changes

```sh
stw workspace checkpoint --task renderer-fix --plan --json
stw workspace commit-plan --source index src/lib.rs --json
```

A checkpoint preserves the staged index and working-tree contents separately.
`stw workspace resume` restores them into a fresh linked worktree.
`commit-plan` previews a commit limited to the selected paths. For a published
pull request, `stw inspect github OWNER/REPO PR` reads the changed paths and
comparison context without checking out the branch.

### See what needs attention across a workspace

```sh
stw status --root . --offline
stw tidy plan --root . --offline
```

`status` reports local changes, branches, upstream divergence and submodule
pin drift. With GitHub access it can also list issues and review evidence.
`tidy plan` explains which branches qualify for cleanup and why.
`stw sync --plan` previews upstream updates. `stw add` registers an
existing GitHub repository as a submodule of the workspace.

### Check a pull request before handoff

```sh
stw ready-check 42 --repo OWNER/REPO
```

`ready-check` reports the review, checks, base and merge conditions for the
current PR head. `stw review carry-over` checks whether prior review evidence
still applies after a rewrite, and `stw lint pr` checks the PR against the
working rules. Use `stw watch --stream --all` for a live stream of GitHub
activity.

## Guides

| Guide | Covers |
| --- | --- |
| [Worktrees and handoff](docs/worktrees-and-handoff.md) | Absorb, restack conflicts, rebase proof, checkpoints, and selective commits |
| [Status and sync](docs/status-and-sync.md) | Submodules, GitHub issues, JSON output, fast-forward, and sync recovery |
| [Branch cleanup and recovery](docs/cleanup-and-recovery.md) | Cleanup evidence, applying and restoring plans, backup refs, and the op log |
| [Pull request review and watch](docs/review-and-watch.md) | Review evidence, carry-over, readiness, lint, and watch events |
| [Development and compatibility](docs/development.md) | Build and test, Git requirements, implementation details, and known limits |

Use the [command reference](docs/command-reference.md) for flags and
syntax. The [migration plan](docs/migration-plan.md) tracks the cutover from
[SourceSteward Go](https://github.com/wstein/sourcesteward-go).
[Brand assets](brand/README.md)
