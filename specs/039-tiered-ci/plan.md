# Plan: tiered, faster CI (Spec 039)

Test-first throughout (a failing test before behaviour). Each work package is a pull request for one middle-tier author, stacked
linearly, and ends with its tests green and `make check` clean. Workflow edits are code for review purposes (independent review of the
exact head). Seams: `scripts/ci_changes.py` (diff classification, kept byte-compatible), a new `scripts/ci_tier.py` (the pure tier
decision), `scripts/ci_shards.py` (package lists), `.github/workflows/{ci,conformance,ci-full}.yml` and `scripts/test_ci_workflow.py`.
Order: **WP-1**, then WP-2, WP-3, WP-4 in sequence. No workflow may be triggered or merged by the author; the owner merges.

| WP | Scope | Done when |
| --- | --- | --- |
| 1 measure and classify | `ci_changes.py affected` and `race-packages` (fail closed), tests with synthetic graphs and a temporary Git repository; a read-only script or note recording the measurement tasks of Spec 039 (per-package `go test -json` elapsed (race and plain), the sandbox decision-gate numbers D8); replay of `affected` over the last 60 PR heads (read only). No workflow change | tests pass; the replay and timings are recorded in `docs/progress.md`; the Spec 039 figures marked unmeasured are filled in or the spec is amended |
| 2 T1 job | `check` without the race step: `go test` of `affected` (plain), sandbox only when the affected set needs it, docs-only runs no Go tests; shard coverage check kept; workflow text tests for the new `check` | `check` on a code PR meets the measured target; on a docs PR it runs no Go tests; names unchanged |
| 3 tier routing | `ci_tier.py` and its truth-table tests; the `tier` job; conformance workflow always called with `t2_required` (T2 runs the `current` lane, `minimum` on T3 only), the informational `stack_line` (printed, not a gate); `suite` truth table; the `ready_for_review` trigger in `ci.yml` and the `labeled` trigger in `ci-full.yml` only; evidence filtered by workflow path, event, head repository and app, with compare-API ancestry for non-top PRs; the `tier` job checks out only the base ref; the shared `t2-<tier>-<head ref>-<lane>` group; shard 0 raced for the race list and plain for the rest on T2; `docs/development.md` and `docs/quality.md` updated; the owner's required-check list restated | every row of the contract passes; a stacked non-top PR reports `suite` green only with tip evidence (`T2 pending on #N` otherwise); a skipped required shard on a top PR fails; independent review of the exact head |
| 4 T3 | push to `main` and a nightly `schedule` run the full matrix with `-race` on all packages (the nightly always runs, D7), group `t3-nightly`; T3 failure reporting rule (R2) in `docs/development.md`; remove the dead `CONFORMANCE_TIER` expression | the schedule exists only on the default branch; tests pin it; the Pages deploy still needs T3 green |

## Test plan and mutation targets

| Area | Tests | Mutation targets |
| --- | --- | --- |
| `affected` | reverse closure, test-import edges, fail-closed rows, `docs_only` unchanged | drop test imports; map a non-package path to nothing; return docs-only for an empty diff |
| race list | AST cases: `sync`, `go` statement, channel, test-only goroutine, `t.Parallel()`, `exec.Cmd` with a buffer as stdio, `io.Copy`, none; a comment or string mentioning `sync` does not count | ignore test files; ignore `sync/atomic`; ignore buffered `exec` stdio (restack) |
| tier decision | all eleven rows, fork PR as a child (must not defer the parent), API failure, evidence from a same-named check of another app or a fork (rejected), compare `diverged` | swap rows 5 and 6; put the default-branch row before the child row; treat an API error as `false`; count a closed PR or a fork PR as a child; accept evidence by check name alone |
| workflows | triggers, permissions, pinned actions, no `if:` on the conformance caller, `suite` rows, names, groups | add `pull_request_target`; add `edited`; skip the `suite` job on a stacked PR |

Verification evidence (commands, SHAs, review) goes to `docs/progress.md` at handoff; nothing is marked verified without it.
