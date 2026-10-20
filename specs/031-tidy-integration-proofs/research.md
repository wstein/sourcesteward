# Research: tidy integration proofs

## Measured baseline (this repository, 2026-10-19)

| Fact | Value |
| --- | --- |
| Local branches / worktrees | 149 / 111 (131 non-main branches evaluated) |
| merge-noop (`merge-tree --write-tree` clean, tree equals base tree) | 5 |
| patch-id equivalent (measured with `git cherry`, which erases all whitespace; the Spec uses the same git patch-id with a visible whitespace flag, re-measured by T040), overlapping merge-noop | 21 |
| Clean merge that would add content | 33 |
| Conflict with base | 99 |
| Offline v3 `integration-not-proven` | 93 of 149 |
| Per-commit cherry-pick simulation: commits that conflict | 1,087 of 1,750 |

The measurement scripts are not committed; task T039 re-measures with a recorded command list, running every `merge-tree` with a private object directory, so the numbers are reproducible evidence rather than a claim.

## Rejected: per-commit cherry-pick simulation

Replaying each commit of `M..B` onto the base conflicts for 1,087 of 1,750 commits because stacked commits depend on their predecessors: commit 3 of a stack touches lines introduced by commit 2, so applying it alone onto a base that has only the rebased result differs textually. A conflict is therefore not evidence of non-integration, and a clean pick is not evidence of integration (it can apply a change that is already present differently). The result is neither sound nor complete, and it is expensive (one merge per commit). The Spec uses the cumulative `merge-tree` and the set-based patch-id instead.

## Why these classes

- `ancestor` is exact but misses every rewrite (rebase-merge, squash-merge).
- `patch-id` is git's own default patch-id (the semantics of `git cherry`): it recognizes rebased commits, is order independent and forge-free. It ignores whitespace (reproduced on Git 2.56: an indented call inside a Python block matched a base commit that does not indent it; `rm -rf "$D" /` matches `rm -rf "$D"/`), so a verbatim comparison from the same git (`patch-id --verbatim`) raises the visible flag `whitespace-differs`, which needs review instead of any custom normalization. It shows an empty commit as `+` with no counterpart on the base (observed), so empty commits are counted separately. Binary changes are hashed by content (`--binary`): measured on Git 2.47.3 and 2.56.0, two different binary changes to one path gave different patch-ids and the same change gave `-` in `git cherry`.
- `merge-noop` recognizes rebase-merge and squash-merge whose content landed unchanged, and present content only. It also proves reformatting-only branches when the base already contains the final content.
- A branch with no net change (added and reverted work) merges cleanly to the base tree but was never integrated; it has nothing unmerged, so it is its own deletable class `no-net-change`.

Everything else (the 99 consolidated branches) is not provable forge-free. Rounds 3 to 7 of review explored custom normalized digests, whitespace-significant file lists and binary-by-attribute classification; the owner decided that this is over-engineering for an edge case. The Spec instead flags, reports and lets a reviewer clear, and documents its scope limits under Limits.

## Why a review report and decisions

The cost of a wrong local-branch deletion is bounded by the recovery ref, so the valuable work is making the judgment cheap: one readable diff of what the base does not yet contain, the evidence that failed, and a machine-readable rendering an AI can evaluate. A decision file makes the verdict an explicit, attributable input (who, what, why) bound to the exact tip OID, and apply still needs owner authorization.

## Merge engine side effects

`merge-ort` may open initialized submodule repositories to resolve gitlink conflicts, applies rename detection and (with `merge.renormalize`) content conversion, and may trigger auto maintenance. The Spec fixes these with constant `-c` arguments and pre-checks gitlink differences. A grafts file can fake ancestry and a shallow clone gives a wrong merge base, so both make proofs not-evaluated; the repository's alternates file is ignored once `GIT_OBJECT_DIRECTORY` is set.

## Open measurements before implementation

1. Why did #81 need hints: more than 1,000 closed PRs (page cap), duplicate OIDs, or the unproven-hint path? T002 reproduces with fakes and records the cause before any change.
2. Whether the closed-PR collection covers this repository's 149 refs; the Anchorage umbrella uses a different repository and remote.
