# Research: tidy integration proofs

## Measured baseline (this repository, 2026-10-19)

| Fact | Value |
| --- | --- |
| Local branches / worktrees | 149 / 111 (131 non-main branches evaluated) |
| merge-noop (`merge-tree --write-tree` clean, tree equals base tree) | 5 |
| patch-id equivalent (`git cherry` semantics, all commits `-`), overlapping merge-noop | 21 |
| Clean merge that would add content | 33 |
| Conflict with base | 99 |
| Offline v3 `integration-not-proven` | 93 of 149 |
| Per-commit cherry-pick simulation: commits that conflict | 1,087 of 1,750 |

The measurement scripts are not committed; task T029 re-measures with a recorded command list so the numbers are reproducible evidence rather than a claim.

## Rejected: per-commit cherry-pick simulation

Replaying each commit of `M..B` onto the base conflicts for 1,087 of 1,750 commits because stacked commits depend on their predecessors: commit 3 of a stack touches lines introduced by commit 2, so applying it alone onto a base that has only the rebased result differs textually. A conflict is therefore not evidence of non-integration, and a clean pick is not evidence of integration (it can apply a change that is already present differently). The result is neither sound nor complete, and it is expensive (one merge per commit). The Spec uses the cumulative `merge-tree` and the set-based patch-id instead.

## Why these three kinds

- `ancestor` is exact but misses every rewrite (rebase-merge, squash-merge).
- `patch-id` recognizes rebase-merge: rebased commits keep their patch-id when context lines do not change. It is order independent and forge-free. It does not see squash-merge (one commit with the combined patch). It cannot tell whether the change is still present.
- `merge-noop` recognizes rebase-merge and squash-merge whose content landed unchanged, and present content only. A branch whose changes were later modified on the base conflicts or would add content and is not proven.

Neither is a heuristic: both are deterministic Git computations with defined refusals. Nothing else is accepted.

## Object writes by merge-tree

`git merge-tree --write-tree` writes the merged tree (and conflict blobs) into the object database. A read-only planner must not leave objects in the inspected repository, so the Spec requires a private temporary object directory with the repository as an alternate. This is checked by byte comparison of the object directory before and after.

## Configuration execution surface

Hooks run for mutating commands; `core.fsmonitor` hooks run for status-like commands; filters, textconv and merge drivers run when attributes name a configured driver during content evaluation; `submodule.<name>.update=!cmd` runs only on `git submodule update`, which tidy never runs. Existing code already neutralizes `core.hooksPath` and `core.fsmonitor` for inventory and release reads with `-c`, which is the precedent for FR-019. Spec 029's safety collector instead refuses any `submodule.*` key and `core.hooksPath`, which is the behavior #79 reports; the refusal is correct for apply (hooks would run) and over-broad for read-only planning.

## Open measurements before implementation

1. Why did #81 need hints: more than 1,000 closed PRs (page cap), duplicate OIDs, or the unproven-hint path? T002 reproduces with fakes and records the cause before any change.
2. Whether the closed-PR collection covers this repository's 149 refs; the Anchorage umbrella uses a different repository and remote.
