# SourceSteward roadmap

`stw` is experimental. This roadmap states what comes next and in which order. It is a plan, not a promise.
Only the next milestone is binding. Later milestones are planned and may change.

Last reviewed: 2026-11-27.

## How this roadmap changes

- Only the next milestone is binding. Later milestones are planned and carry the date of the last review.
- The roadmap is revised at a milestone boundary. New ideas go to the [proposals](#proposals) list and are rated at the next boundary.
- Every change of order is recorded in the [changes](#changes) table with its date and reason.

## Principles

1. **Read first.** A command that writes has a plan preview, compare-and-swap on every ref it moves, a backup ref under `refs/stw/backup/<run>/<k>` and an entry in the operation log.
2. **Recovery through the operation log.** New writing commands should record recoverable changes for `stw oplog undo`. Existing `stw tidy restore` and `stw sync restore` remain available.
3. **No daemon and no writing to GitHub.** `stw` runs once per task and prints versioned JSON on request. See [what `stw` does not do](docs/what-stw-does-not-do.md).
4. **New behavior is proved before it ships.** Behavior without a Go counterpart goes through a spike, live acceptance by the owner, product code, and an independent review of the exact head. Ports of Go behavior name each deviation.
5. **JSON schemas are versioned.** Changes are additive. A breaking change raises `schema_version`.
6. **Pragmatic for people and agents, on top of Git.** `stw` offers a short working model that does not require the index or Git vocabulary for everyday work. Objects, remotes and hooks stay Git's, so `git` and `gh` can read every result.

## Milestones

| Milestone | Status | Theme | Content | Done when |
| --- | --- | --- | --- | --- |
| 0.6.0 | released | Release | [SourceSteward v0.6.0](https://github.com/wstein/sourcesteward/releases/tag/v0.6.0) | The tag points to the checked build commit. The Linux x86-64 and macOS ARM64 archives passed smoke tests. |
| 0.6.x | next | Close acceptances | The open `needs-live-acceptance` issues ([#3](https://github.com/wstein/sourcesteward/issues/3), [#4](https://github.com/wstein/sourcesteward/issues/4), [#5](https://github.com/wstein/sourcesteward/issues/5), [#9](https://github.com/wstein/sourcesteward/issues/9)). Stable macOS tests. | Each label is removed or the open rest is named. |
| 0.7 | planned | Working model and stack | A short working model for people and agents on top of Git ([#38](https://github.com/wstein/sourcesteward/issues/38)). Selecting hunks for a commit without staging ([#39](https://github.com/wstein/sourcesteward/issues/39)). `stw restack apply`: conflict-free, compare-and-swap per ref, backup refs, `range-diff` of the result; undo through the operation log; the stack input contract from [#9](https://github.com/wstein/sourcesteward/issues/9). | The owner sees all three live on a real task: a commit made from hunks without staging, a restack of a real stack, and an undo. Restack acceptance also covers abort and a ref that moved after planning. |
| 0.8 | planned | Evidence | References to commits that stay valid after rebase and restack ([#40](https://github.com/wstein/sourcesteward/issues/40)), deferred from 0.7 until the Gerrit and STB discussion is done; the spike ([whr-anchorage#53](https://github.com/wstein/whr-anchorage/pull/53)) is merged in the workspace; the Rust draft ([#42](https://github.com/wstein/sourcesteward/pull/42)) is open and not merged. The local CLEAR receipt contract ([#8](https://github.com/wstein/sourcesteward/issues/8)): schema, binding to head and repository, reviewer trust. `stw prove` and `stw review carry-over` without GitHub. Then the push and workflow policy in Rust. | The decision on #40 is recorded: built, or dropped after the Gerrit discussion. The decision on #8 is recorded. A receipt is verified across the host and the reviewer environment. |
| 0.9 | planned | Conflicts | Deferred conflict resolution in a separate worktree, without in-memory rerere. `stw inspect --blast-radius`. A guide for handing a conflict to an agent. Issues [#2](https://github.com/wstein/sourcesteward/issues/2) and [#10](https://github.com/wstein/sourcesteward/issues/10). | A real conflict is resolved over two stack levels. |
| 1.0 | planned | Stable | All commands accepted. JSON schemas frozen and documented. Linux and macOS tested. One full mutation run. Security findings triaged. | The documentation states the compatibility promise. |

## Not planned

| Idea | Reason |
| --- | --- |
| A repository model of its own (jj style) | Decision of 2026-11-11: Git stays the model. Functions such as undo and conflict handling are added to `stw`. |
| In-memory rerere in the replay | Decision 7 of the [migration plan](docs/migration-plan.md). Git's rerere works during resolution without `stw`. |
| A daemon or MCP server | Another kind of product with its own permission and token model. Revisit after 0.8 as a thin wrapper around `--json`. |
| Automatic sparse worktrees | Sparse checkout is [not covered by tests](docs/development.md) and is refused by `stw oplog undo --keep`. |
| Rule filtering by path glob | It belongs to the agent rules, not to a Git tool. |
| Automatic retargeting of child pull requests | It writes to GitHub. `watch` reports the event; a person or an agent runs the command. |

## Proposals

Propose a change as a [GitHub issue](https://github.com/wstein/sourcesteward/issues) and say which milestone it would affect. Proposals are rated at the next milestone boundary. No proposals are open.

## Changes

| Date | Change | Reason |
| --- | --- | --- |
| 2026-11-26 | Roadmap created. | Plan the work after the cutover to Rust (0.5.0) and the 0.6.0 release. |
| 2026-11-26 | Issue #2 moved from 0.6.x to 0.9. | It covers deferred conflict resolution, which the roadmap places in 0.9. Issue #10 is listed under 0.9 as well. |
| 2026-11-27 | 0.7 now covers the working model (#38), hunk selection without staging (#39), stable references after rewrites (#40) and `restack apply`. | Owner decision: establish early an interface that is pragmatic for people and agents, and reduce the cost of changing commit hashes. The milestone is large; it can be split at the boundary. |
| 2026-11-27 | Stable references after rewrites (#40) moved from 0.7 to 0.8. 0.7 covers #38, #39 and `restack apply`. | Owner decision: focus on the core properties and settle the Gerrit and STB direction first. The earlier row of this date is superseded for #40. |
| 2026-11-27 | Dropped "the ref-drift check on worktree apply" from 0.6.x. | Owner clarification (D-20261127-22): `worktree-apply` pins the commit SHA and still checks it out when a branch tip moved; missing pinned commit refuses. Tip drift is reported in JSON notices. Ref-drift refusal for recovery commands stays under [#10](https://github.com/wstein/sourcesteward/issues/10). |
