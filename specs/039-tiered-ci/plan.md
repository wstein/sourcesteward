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
| 3 tier routing | `ci_tier.py` and its truth-table tests; the `tier` job; conformance workflow always called with `t2_required` (T2 runs the `current` lane, `minimum` on T3 only), the `stack_line` check; `suite` truth table; `ready_for_review` and `labeled` triggers; evidence reuse; the shared `t2-<head ref>` group; shard 0 raced for the race list and plain for the rest on T2; `docs/development.md` and `docs/quality.md` updated; the owner's required-check list restated | every row of the contract passes; a stacked non-top PR reports both `suite` names green with `deferred-to-tip`; a skipped required shard on a top PR fails; independent review of the exact head |
| 4 T3 | push to `main` and a nightly `schedule` run the full matrix with `-race` on all packages (skipped when no new commit since the last green T3), group `t3-nightly`; T3 failure reporting rule (R2) in `docs/development.md`; remove the dead `CONFORMANCE_TIER` expression | the schedule exists only on the default branch; tests pin it; the Pages deploy still needs T3 green |

## Test plan and mutation targets

| Area | Tests | Mutation targets |
| --- | --- | --- |
| `affected` | reverse closure, test-import edges, fail-closed rows, `docs_only` unchanged | drop test imports; map a non-package path to nothing; return docs-only for an empty diff |
| race list | `sync`, goroutine, channel, test-only goroutine, `t.Parallel()`, none | ignore test files; ignore `sync/atomic` |
| tier decision | all ten rows, fork, API failure, evidence | swap rows 5 and 6; treat an API error as `false`; count a closed PR as a child |
| workflows | triggers, permissions, pinned actions, no `if:` on the conformance caller, `suite` rows, names, groups | add `pull_request_target`; add `edited`; skip the `suite` job on a stacked PR |

Verification evidence (commands, SHAs, review) goes to `docs/progress.md` at handoff; nothing is marked verified without it.
