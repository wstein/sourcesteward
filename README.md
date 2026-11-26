# sourcesteward (stw)

![SourceSteward — Explainable version control](docs/assets/banner.svg)

[![CI](https://github.com/wstein/sourcesteward/actions/workflows/stw-ci.yml/badge.svg)](https://github.com/wstein/sourcesteward/actions/workflows/stw-ci.yml)
[![Docs](https://img.shields.io/badge/docs-SourceSteward-0b2545)](https://wstein.github.io/sourcesteward/)
[![Line coverage](https://wstein.github.io/sourcesteward/coverage.svg)](https://github.com/wstein/sourcesteward/actions/workflows/stw-pages.yml)
[![GitHub release](https://img.shields.io/github/v/release/wstein/sourcesteward)](https://github.com/wstein/sourcesteward/releases)
[![Open issues](https://img.shields.io/github/issues/wstein/sourcesteward)](https://github.com/wstein/sourcesteward/issues)
[![License EUPL-1.2](https://img.shields.io/badge/license-EUPL--1.2-0b2545)](LICENSE)

`stw` is a Rust CLI for inspecting Git workspaces, previewing changes, and
preparing work for review. It can report on a main repository and its
checked-out submodules in one run.

> **Experimental software.** SourceSteward is provided without warranty.
> Commands such as `stw watch`, `stw status`, and `stw absorb` are read-only.
> Apply commands such as `stw absorb --apply` can rewrite Git history.
> Review the plan and try write operations on a disposable clone first.

## Motivation

When work spans a repository and its submodules, changes and branches can
be easy to miss. SourceSteward shows what needs attention across the
workspace and lets you preview branch cleanup before changing anything.
Review and handoff commands help humans and agents pick up work with the
same evidence.

## SourceSteward 101

From this checkout, install with Cargo and the pinned Rust toolchain.

```sh
cargo install --locked --path .
stw --version
```

From the root of a Git workspace, start with two read-only commands.

```sh
stw status --root . --offline
stw tidy plan --root . --offline
```

`status` reports branches, local changes, upstream divergence based on local
refs, and submodule pin drift. The **Needs action** section shows which
repositories warrant attention. Omit `--offline` to include GitHub data
when credentials are available. Add `--json` for a machine-readable report.

`tidy plan` shows which branches qualify for cleanup and which need review
or should be kept. Read the reason for each decision before applying a plan.

## Guides

| Guide | Covers |
| --- | --- |
| [Status and sync](docs/status-and-sync.md) | Submodules, GitHub issues, JSON output, fast-forward, and sync recovery |
| [Branch cleanup and recovery](docs/cleanup-and-recovery.md) | Cleanup evidence, applying and restoring plans, backup refs, and the op log |
| [Worktrees and handoff](docs/worktrees-and-handoff.md) | Checkpoint and resume, checkout-free inspection, selective commits, absorb, and restack |
| [Pull request review and watch](docs/review-and-watch.md) | Review evidence, carry-over, readiness, lint, and watch events |
| [Development and compatibility](docs/development.md) | Build and test, Git requirements, implementation details, and known limits |

Use the [command reference](docs/command-reference.md) for flags and
syntax. The [migration plan](docs/migration-plan.md) tracks the cutover from
[SourceSteward Go](https://github.com/wstein/sourcesteward-go).
[Brand assets](brand/README.md)
