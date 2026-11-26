# SourceSteward roadmap

`stw` is experimental. This roadmap states what comes next and in which order. It is a plan, not a promise.
Only the next milestone is binding. Later milestones are planned and may change.

Last reviewed: 2026-11-26.

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

## Milestones

| Milestone | Status | Theme | Content | Done when |
| --- | --- | --- | --- | --- |
| 0.6.0 | released | Release | [SourceSteward v0.6.0](https://github.com/wstein/sourcesteward/releases/tag/v0.6.0) | The tag points to the checked build commit. The Linux x86-64 and macOS ARM64 archives passed smoke tests. |
| 0.6.x | next | Close acceptances | The open `needs-live-acceptance` issues ([#2](https://github.com/wstein/sourcesteward/issues/2), [#3](https://github.com/wstein/sourcesteward/issues/3), [#4](https://github.com/wstein/sourcesteward/issues/4), [#5](https://github.com/wstein/sourcesteward/issues/5), [#9](https://github.com/wstein/sourcesteward/issues/9)). Stable macOS tests. The ref-drift check on worktree apply. | Each label is removed or the open rest is named. |
| 0.7 | planned | Stack | `stw restack apply`: conflict-free, compare-and-swap per ref, backup refs, `range-diff` of the result. Undo through the operation log. The stack input contract from [#9](https://github.com/wstein/sourcesteward/issues/9). | Accepted on a real stack, including abort and a moved ref. |
| 0.8 | planned | Evidence | The local CLEAR receipt contract ([#8](https://github.com/wstein/sourcesteward/issues/8)): schema, binding to head and repository, reviewer trust. `stw prove` and `stw review carry-over` without GitHub. Then the push and workflow policy in Rust. | The decision on #8 is recorded. A receipt is verified across the host and the reviewer environment. |
| 0.9 | planned | Conflicts | Deferred conflict resolution in a separate worktree, without in-memory rerere. `stw inspect --blast-radius`. A guide for handing a conflict to an agent. | A real conflict is resolved over two stack levels. |
| 1.0 | planned | Stable | All commands accepted. JSON schemas frozen and documented. Linux and macOS tested. One full mutation run. Security findings triaged. | The documentation states the compatibility promise. |

## Not planned

| Idea | Reason |
| --- | --- |
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
