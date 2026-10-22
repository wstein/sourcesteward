# Plan: tiered, faster CI (Spec 039)

Test-first throughout (a failing test before behaviour). Each work package is a pull request for one middle-tier author, stacked
linearly, and ends with its tests green and `make check` clean. Workflow edits are code for review purposes (independent review of the
exact head). Seams: `scripts/ci_changes.py` (diff classification, kept byte-compatible), a new `scripts/ci_tier.py` (the pure tier
decision), `scripts/ci_shards.py` (package lists), `.github/workflows/{ci,conformance,ci-full}.yml` and `scripts/test_ci_workflow.py`.
Order: **WP-1**, then WP-2, WP-3, WP-4 in sequence. No workflow may be triggered or merged by the author; the owner merges.

| WP | Scope | Done when |
| --- | --- | --- |
| 1 measure and T1 | `ci_changes.py affected` and `--files-from` (fail closed, `.github/` and `scripts/` give `all`), tests with synthetic graphs and a temporary Git repository; read-only measurements: per-package plain and race `go test -json` elapsed, the plain-versus-race ratio of the `current` shards, the sandbox decision-gate numbers (D8), the replay over the last 60 PR heads; then the T1 job: `check` without the race step, affected plain tests, `affected_reason` honoured, docs-only runs no Go tests, shard coverage check kept | tests pass; the measurements are recorded in `docs/progress.md` and the figures marked unmeasured in Spec 039 are filled in or the spec is amended (D3); `check` keeps its name |
| 2 tier routing | `ci_tier.py` and its truth-table tests; the `tier` job (base-ref checkout, API file list); the conformance workflow always called with `t2_required` (lane `current`, no `-race`; `minimum` on T3 only); the informational `stack_line`; `suite` truth table; the `ready_for_review` trigger in `ci.yml` and the `labeled` trigger in `ci-full.yml` only (`ci.yml` does not react to `labeled`); evidence filtered by workflow path, event, head repository, app and the six executed `current` shards, with compare-API ancestry for non-top PRs; the shared `t2-<tier>-<head ref>-<lane>` group; T3 on push to `main` without race; `docs/development.md` and `docs/quality.md` updated; the owner's required-check list restated | every row of the contract passes; a non-top PR reports `suite` green only with tip evidence (`T2 pending on #N` otherwise); a Draft or docs-only suite is never evidence; independent review of the exact head |
| 3 T4 | `race.yml`: nightly `go test -race ./...` of all packages and weekly `minimum` lane with race, both always; the `report` job and its reporter script (one open `race-failure` issue, strict test-name pattern, `issues: write` only here, `main` checkout only); `docs/development.md` | the schedule exists only on the default branch; tests pin the permissions, the triggers and the reporter; a simulated failure fixture produces one issue and a second failure only a comment |
| 4 optional sandbox | the D8 option chosen by the owner at the gate (apt cache or an install-once artifact whose sha256 is a job output of the producer and verified by every consumer) | only after the WP-1 numbers; the saving is reported as measured |

## Test plan and mutation targets

| Area | Tests | Mutation targets |
| --- | --- | --- |
| `affected` | reverse closure, test-import edges, fail-closed rows, `--files-from`, a PR editing `scripts/ci_changes.py` gets T1 `all`, `docs_only` unchanged | drop test imports; map a non-package path to nothing; return docs-only for an empty diff; ignore `.github/` or `scripts/` |
| tier decision | all rows of the contract, fork PR as a child (must not defer the parent), API failure, evidence from a same-named check of another app or a fork (rejected), compare `diverged` | swap rows 5 and 6; put the default-branch row before the child row; treat an API error as `false`; count a closed PR or a fork PR as a child; accept evidence by check name alone; **accept a draft-row or docs-only suite as evidence**; **accept a chained evidence suite**; accept a run with a skipped `current` shard |
| workflows | triggers (`ci.yml` has no `labeled`, `ci-full.yml` has it), permissions, pinned actions, no `if:` on the conformance caller, `suite` rows, names, groups with the lane, no `-race` in `ci.yml` and `ci-full.yml`, `race.yml` triggers and permissions | add `pull_request_target`; add `edited`; skip the `suite` job on a stacked PR; add `issues: write` outside `race.yml`; add a `pull_request` trigger to `race.yml` |
| reporter | one issue for the first failure, a comment for the second, only matched test names in the body, bounded count | open a second issue; copy raw log text |

Verification evidence (commands, SHAs, review) goes to `docs/progress.md` at handoff; nothing is marked verified without it.
