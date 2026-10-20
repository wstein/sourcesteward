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

## Measurements before implementation (T003, T021; recorded 2026-10-20)

### Why #81 needed hints (deterministic doubles, observed failures before the change)

The reproduction tests ran against the unchanged `DiscoverPullRequests`, the workspace coordinator and the CLI parser, with transport doubles and temporary repositories only (no live call from any test).

| Question | Observation before the change |
| --- | --- |
| Page cap | A closed collection of ten full pages with no authoritative end (`tidyPagination` returns `ErrTidyLimit` at page 10) failed the whole plan: `tidy hosting operation exceeded its request, byte or deadline budget`; found exact-head PRs were discarded with it. A repository with more than 1,000 closed PRs therefore could only be planned with `--pr`, which skips the collection. |
| Request budget | 120 distinct OIDs over ten full pages (1 session read, 1 revalidation, 10 pages) ran into the 128-request ceiling at the 117th candidate read and failed the whole plan with the same message instead of retaining the last 4 OIDs. |
| Duplicate OIDs | Discovery already keyed targets by distinct OID (`providerProofs`), so shared OIDs did not multiply reads. The defect was the hint path: a repeated identical `--pr OID=N` was a usage error (`contract 3 requires one exact mode, canonical paths and explicit online selectors`, with no selector named), so an operator who copied one hint per ref hit "duplicate hint" and an unselected-OID hint said only `PR hints must name selected full native heads and supported numbers.` |
| Ordering | Targets were sent in ref-name order while the budget rule needs ascending OID order; the coordinator now sorts them and hosting returns ascending rows. |
| Offline rebase-merge | Not a discovery defect: it is the missing forge-free proofs (patch-id, merge-noop) of Phase 3, which is outside this slice and not changed here. |

Cause of #81: hints were needed (1) when the closed collection hit its bound, which discarded all evidence, (2) when the request budget ran out, and (3) because identical repeated hints and shared OIDs were rejected at the usage layer. None of the three needs a second endpoint.

### Does the 1,000-record cap truncate the real repositories (read-only GET measurement)

Run with the owner's `gh` credentials, GET only, two requests per repository, one record per page so that only the `Link` header and no PR content is read:

```text
gh api -i 'repos/wstein/sourcesteward/pulls?state=closed&per_page=1&page=1'
  Link: ...page=2>; rel="next", ...page=79>; rel="last"           -> 79 closed PRs
gh api 'repos/wstein/sourcesteward/pulls?state=closed&per_page=100&page=10' --jq 'length'
  0                                                                 -> page 10 empty
gh api -i 'repos/wstein/anchorage-workspace/pulls?state=closed&per_page=1&page=1'
  Link: ...page=2>; rel="next", ...page=16>; rel="last"           -> 16 closed PRs
```

Both repositories are far below 1,000 records and one page of 100 covers sourcesteward's 79 closed PRs, so the cap does not truncate either of them today; the collection is `complete` for both. The 149 local refs are not blocked by coverage. The cap stays a bound for much older repositories and is reported as `incomplete` rather than as a failure.

### Request and byte accounting of the fixtures

| Fixture | Requests | Raw bytes |
| --- | --- | --- |
| 120 distinct OIDs, one candidate each, ten full pages | 1 preloaded session read + 1 revalidation + 10 pages + 116 PR reads = 128; last 4 OIDs `discovery-budget-exhausted` | about 0.7 MiB, far below the 32 MiB ceiling (a page is about 56 KiB, a PR read about 0.6 KiB) |
| 200 OIDs (budget never exceeded) | at most 128; 83 OIDs exhausted | same |
| 40 refs sharing 12 OIDs (workspace) | 12 targets in one discovery call, no hints | not applicable (double) |
| Tenth full page without an end | 1 + 10 pages + 1 PR read for the found OID | small |

## Result of implementation (work package C)

Per-OID results `github-merged-head`, `no-merged-pr`, `discovery-incomplete`, `discovery-budget-exhausted` and `ambiguous-needs-hint` and the coverage `complete` or `incomplete` travel in `TidyHeadEvidence` (`result`, `coverage`). The stored v3 plan is unchanged (undecided refs stay `integration-not-proven` retained); plan result `limitations` carry one bounded note per undecided result. A plan field carrying the full DiscoveryRecord list needs a v3 schema addition and is left to the owner decision recorded in the work package report.

## Stage2 baseline (T002, source `37b56fbf`)

Every path in the plan exists at this source; the new files (`tidy_v3_proof_native.go`, `tidy_v3_report*.go`, `tidy_v3_decisions.go`) do not exist yet, as intended. The plan's `native_stream.go` path is confirmed; the bounded stream executor is `executeNativeStream` there and the record readers are in `git_stream.go`. Symbols confirmed: `gitRead`, `gitSafetyWithPrimary` (`git.go`), `nativeOutputLimit` and `sandboxEnvironment` (`native_sandbox.go`), `observationLimit = 1024` (`result.go`), `gitMetadataRecordLimit = 4224` and `gitMetadataStreamLimit = 4325376` (`git_stream.go`), `maxPages = 10` (`internal/hosting/transport.go`), `DiscoverPullRequests` and `RevalidatePullRequest` (`tidy_evidence_pr.go`).

Collection reads that were buffered at 64 KiB: `git.go` (`Observe`, reference and registration digests), `tidy_git.go` (v1), `tidy_v3_proof.go` (v3), `tidy_git_apply.go` (`gitTidyReferenceMap`). `retirement_git.go` still reads `worktree list` buffered during retirement apply; see verification.md.

Frozen published bytes: `internal/contracts/schema_freeze_test.go` holds the SHA-256 of all 18 `schemas/v1/*.json` and 2 `schemas/v2/*.json` files. Old stored-plan fixtures are not files: they are constructed in tests (`v3LegacyTestPlan` in `internal/workspace/tidy_v3_state_test.go`, stored-plan cases in `workspace_test.go`), so the frozen evidence is those tests passing unchanged.

Source owners: `.github/CODEOWNERS` assigns every path to `@wstein`; by area, `internal/workspace` (native Git reads, tidy v1/v3 observation and apply) and `internal/hosting` (forge evidence) belong to the tidy feature owner, `internal/cli` to the command surface, `schemas/` to the published contracts.
