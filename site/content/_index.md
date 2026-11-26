+++
title = "SourceSteward"
+++

Plan Git changes, inspect branch history, and hand off work across repositories.

SourceSteward is a Rust CLI for standalone Git repositories and workspaces
with submodules. Preview where staged fixes belong, see which files would
conflict in a rebase, and inspect the history of ref changes.

## Get started

Install with Homebrew on macOS or Linux.

```sh
brew install wstein/tap/stw
```

On macOS or Linux, you can also use the installer. It verifies
the archive's SHA-256 and installs into `~/.local/bin`.

```sh
curl -fsSL https://wstein.github.io/sourcesteward/install.sh | sh
```

Set `STW_INSTALL_DIR` to use another directory, or append `-s -- v0.6.0` to
the `sh` command to install a specific release.

Run a read-only status check in a Git checkout.

```sh
stw status --offline
```

SourceSteward is experimental software. Review the plan before running an
operation that changes Git history.

## Choose a guide

- [Prepare changes for review](docs/worktrees-and-handoff/) with Absorb, Restack, and worktree checkpoints.
- [Inspect and recover branch history](docs/cleanup-and-recovery/) with Tidy and Oplog.
- [Inspect and sync a workspace](docs/status-and-sync/) across the main repository and its submodules.
- [Review pull requests and watch GitHub activity](docs/review-and-watch/) with readiness checks and notifications.
