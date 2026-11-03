# Specification: `stw status` compact output by default, with `--long` for detailed cards

**Created**: 2026-11-02 | **Status**: implemented in source, independent review pending (spec-light: this page, contracts and tests).
Issue #378. It **amends** [Spec 037](../037-stw-status-overview/spec.md) and [Spec 033](../033-stw-status/spec.md): the default
human output of online `stw status` becomes compact to fit within a single terminal window without scrolling; the full
multi-line repository cards and detailed instructions are retained under `--long` (`-l`).

## Context & Problem

In workspaces containing multiple repositories, branches, and worktrees (such as Anchorage), running `stw status` routines
emits 70–80+ lines of text. This is driven by:
1. Multi-line cards (5–7 lines per repository) printed for *every* repository, even when completely clean.
2. Repetitive low-priority (`blue` / `violet`) informational items (e.g. 6 separate "ahead gitlink" items, individual stash
   counts, unintegrated branches) where each item prints a 2-line block including a `run:` remedy command.

This verbose output pushes previous terminal context off screen on every invocation, causing friction for a high-frequency
command.

## CLI synopsis

```
stw status [PATH] [--long|-l] [--offline] [--fetch] [--worktrees] [--rules [--rules-path PATH]] [--icons unicode|nerd|ascii] [--timeout DURATION] [--review] [--stats] [--verbose] [--json]
```

- `--long`, `-l`: output full multi-line repository cards (including remote lines, issues lines, and detailed worktree / branch / gitlink lines) and expanded action instructions (`run:` and `hint:` for every item). Default: compact mode.
- `--json`: machine output is completely identical (schema 2 online, schema 1 offline) regardless of whether `--long` or `-l` is passed.
- `--offline`: prints the local half alone byte for byte as before the online default (pinned to Spec 033 per Spec 037 US2).

## Compact Formatting Rules (Default)

### 1. Needs Action Block
- **Priority ordering**: preserves red, yellow, green, blue, violet order.
- **Single-line findings**: each item is rendered on a single line: `[icon] [level] [repo] [text] [URL]`.
- **No `run:` or `hint:` commands**: individual remedy instructions and `run:` lines are omitted across all priority levels in compact mode to fit the complete status output within a standard terminal without scrolling. All remedy commands and hints are available via `--long` (`-l`).

### 2. Repositories Block
In default compact mode, each repository is rendered as a **single line**:
```
[marker][name]  [branch]  [sync/tracking]  [changes]
```
- Multi-line indentation cards (`remote: ...`, `issues: ...`, `stash ...`, `branches: ...`, `gitlink: ...`, and linked worktree lists) are suppressed in compact mode because all actionable findings are already surfaced in the Needs Action block.
- A repository requiring attention or failing to read is marked with the appropriate icon marker (`▲` or `✖`).

### 3. Summary & Diagnostics
- Summary line (`summary: ... | N repositories | N notices`) remains unchanged at the bottom.
- Notices block is printed when notices exist.
- `--stats` and `--verbose` behave identically in compact and long modes.

## Long Formatting Rules (`--long` / `-l`)
- Produces the comprehensive Spec 037 output:
  - Full multi-line repository cards for every repository.
  - Every action item (including all blue and violet items) has its own dedicated line and `run:` remedy.
  - Linked worktrees are listed when `--worktrees` is combined with `--long`.
