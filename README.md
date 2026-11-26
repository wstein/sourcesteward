# sourcesteward (stw)

![SourceSteward — Explainable version control](docs/assets/banner.svg)

[![CI](https://github.com/wstein/sourcesteward/actions/workflows/stw-ci.yml/badge.svg)](https://github.com/wstein/sourcesteward/actions/workflows/stw-ci.yml)
[![License EUPL-1.2](https://img.shields.io/badge/license-EUPL--1.2-0b2545)](LICENSE)

`stw` is a Rust CLI for inspecting Git workspaces, previewing changes, and
preparing work for review. It can report on a main repository and its
checked-out submodules in one run.

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
