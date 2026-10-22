# Specification: tiered, faster CI

**Created**: 2026-10-23 | **Status**: proposed (spec-light: this page, [contracts/tiers.md](contracts/tiers.md) and
[plan.md](plan.md) are the whole design; documentation only, no workflow or script is changed by this specification). It
**amends** the CI tier design recorded in [development](../../docs/development.md) (the `CONFORMANCE_TIER` expression and the
`ci-full` label) and keeps the shard design of `scripts/ci_shards.py`.

## Problem (owner, 2026-10-23)

Every pull request runs the full matrix (race tests of all packages and 12 Git/jj conformance shards). A run takes 15 to 30 minutes,
a feature is 3 to 5 stacked PRs and each of them runs it, a rebase restarts every run, and the GitHub runner queue grows (runs stayed
`queued` for 25 minutes). The owner wants the tests (race and conformance) on the last PR of a stack only, something cheaper on the
others, and efficiency in general.

## Measured baseline

All numbers below were read on 2026-10-22 with `gh run list -R wstein/sourcesteward -w ci.yml --limit 60` and
`gh api repos/wstein/sourcesteward/actions/runs/<id>/jobs` (read only, no run was triggered). The 60 runs span 2026-10-22 16:24 to
22:11 UTC (5 h 47 min of wall clock). "Runner minutes" is the sum of `completed_at - started_at` over the jobs that ran. Medians
and ranges are over the runs named in the row. No number in this specification is a guess: a figure that is not in this section is
marked **target**, **model** or **unmeasured**.

### Population

| Fact | Value |
| --- | --- |
| Runs read | 60: 48 `pull_request`, 12 `push` to `main` (all 12 succeeded) |
| Pull request runs | 18 success, 28 cancelled by `cancel-in-progress`, 2 still running when read |
| Runner minutes, all 60 runs | 4616 (pull requests 3199, pushes to `main` 1417) |
| Runner minutes of the 28 cancelled runs | 1151 = 25% of all runner minutes, spent on heads that never completed |
| Median wall time at cancellation | 10.7 min |
| Jobs per run | 17 (full tier: `changes`, `check`, 12 shards, 2 `suite`, `pages`) or 4 (stacked path: `changes`, `check`, the skipped conformance caller, `pages`) |
| Stacked-path runs (4 jobs) | 11: 2 completed, 9 cancelled |

### The Spec 037 stack as one example (5 PRs, `feat/037-status-*`)

22 pull request runs, 15 of them cancelled, 7 completed, 1635 runner minutes, between 19:17 and 22:17 UTC (the last run was created
at 21:51). PR runs per branch: seams 2, action-model 3, pulls 5, releases 6, ci 6.

### Per run shape (completed runs, medians)

| Run kind | Run ids (examples) | n | Jobs | Runner min | Wall min | `check` job min | Race step min | Longest shard min | Longest job queue min |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| PR on `main` (full tier) | 39860559774, 39854286196, 39842994587, 39832314373 | 16 | 17 | 116 (107 to 123) | 19.1 (10.5 to 28.2) | 9.5 (6.8 to 11.1) | 7.8 (5.4 to 8.3) | 12.6 | 6.5 (max 11.8) |
| PR stacked on a PR branch | 39821664058, 39813750402 | 2 | 4 | 36 (34 to 39) | 38.0 (34.9 to 41.2) | 36.4 | 34.2 (30.3 and 38.1) | none | 3.8 (max 6.5) |
| Push to `main` | 39860237175, 39859979478, 39853502904, 39848063685 | 12 | 17 | 119 (111 to 123) | 14.0 (12.2 to 35.9) | 9.4 | 7.3 | 12.8 | 0.4 (max 11.9) |
| Cancelled stacked path | 39857734796, 39826908439, 39821664266, 39855251448 | 9 | 4 | 7 to 37 | 14.8 to 36.7 | up to 36.5 | up to 35.5 | none | 0 to 0.5 |

The stacked path is the slowest wall time of all (30 to 41 min), as the owner reported: `go test -race ./...` serial in one job, with
only two completed samples (the other nine were cancelled before the step finished, still at 18 to 36 min of race).

### Where the 116 runner minutes of a full run go (PR runs that succeeded, medians)

The rows overlap: the sandbox install is part of every job row, and the rows add up to about 119 against the median of 116.

| Item | Per job | Jobs per run | Runner min per run |
| --- | --- | --- | --- |
| `Install distro sandbox` step (apt update and install of bubblewrap, apparmor, profiles) | 103 to 105 s | 13 (`check` and 12 shards) | about 22.5 (19% of the run) |
| `check`: `Race tests` step (all packages except `internal/workspace`) | 476 s | 1 | 7.9 |
| shard 0 (the same packages, again under `-race`, with real backends) | 506 s test step, 11.2 to 11.7 min job | 2 (one per lane) | 23 |
| shards 1 to 4 (`internal/workspace` under `-race`) | 334 to 518 s test step, 7.5 to 10.8 min job | 8 | about 70 |
| shard 5 (real CLI smoke checks) | 356 s test step, 8.0 to 8.6 min job | 2 | 16.6 |
| `check`: static checks (`make check-static`), shard coverage check, import boundaries, Pages artifact | 24 s, 6 s, 7 s, 1 s | 1 | 0.6 |
| `suite` aggregates | 3 s | 2 | 0.1 |

Per-job medians of the 12 conformance jobs (minimum / current lane): shard 0 11.2 / 11.7, shard 1 9.7 / 10.8, shard 2 8.3 / 8.0, shard 3
7.6 / 7.5, shard 4 8.9 / 9.4, shard 5 8.6 / 8.0 min. Shard 0 and shard 1 are the long poles (up to 12.8 and 12.4 min).

Two facts follow directly from the workflow text and the table: with the conformance tier on, the non-workspace packages run under
`-race` three times per run (`check`, shard 0 of both lanes, which has the same packages, `-race` and a superset of backends), and every
job pays the 103 s sandbox install before its first test.

### Queueing

The sample does not reproduce the owner's 25 minutes: the longest wait from run creation to the first job start was 14.4 min and the
longest wait of one job (`started_at - created_at`) 11.9 min (run 39860237175). Of 58 runs with jobs, 21 had a job that waited 5 min
or more and 3 a job that waited 10 min or more; the median run waited 46.5 s for its first job. In the same run 39860237175 the
`suite` jobs were created about 12 min after their shards had finished, so the 36 min wall time of that push is partly orchestration
delay and not test time. A run with 17 jobs asks the runner pool for 17 jobs at once; the target design asks for 3 to 4 on most pushes.

## Goals

- **G1.** T1 on every pull request head, target **under 5 minutes** wall time: formatting, vet, documentation and architecture checks,
  and plain `go test` of the packages affected by the diff and their reverse dependencies.
- **G2.** The heavy suite (race on the concurrent packages and the 12 conformance shards) runs once per stack: on the **top** pull request
  of a stack, and on an unstacked pull request when it is ready for review or carries `ci-full`.
- **G3.** Keep the invariant: nothing the owner merges was untested at the T2 level, because the evidence for the exact head of the stack
  tip exists before any PR of the stack is merged, and `main` is re-tested at T3 after each merge.
- **G4.** Keep the required status names stable (`check` and `Git/jj conformance (<lane>) / suite`) and make them always report.
- **G5.** Spend fewer runner minutes: remove the measured duplication and the sandbox install where a tier does not need it.
- **G6.** Stay inside the existing security model: `pull_request` only, least privilege, pinned actions, no new secret.

## Non-goals

- No workflow, script, test or settings change in the pull request of this specification. Branch protection, rulesets, labels and
  environments are the owner's; this document only lists them.
- No merge queue, no self-hosted runner, no change of the shard design, the pinned Git/jj lanes or the Pages deploy.
- No skipping of tests on `main`: T3 stays complete.
- No change of the Ready rule of the workspace `AGENTS.md`; this specification states how its evidence is produced.

## The tier model

| Tier | Runs on | Content | Target wall time |
| --- | --- | --- | --- |
| **T1** | Every pull request head (every `opened`, `synchronize`, `reopened`, `ready_for_review` event) and every push to `main` | `gofmt`, `go vet`, `go mod verify`, tidy check, Python and Node script tests, `scripts/check-docs.py`, `scripts/check-architecture.py`, shard coverage check, Pages artifact check, plain `go test` (no `-race`) of the affected packages. A documentation-only change runs T1 without the Go tests. | under 5 min |
| **T2** | The top pull request of a stack; an unstacked non-draft pull request; any pull request carrying `ci-full` (see "When T2 runs") | T1 plus both Git/jj conformance lanes (shards 0 to 5): `-race` for the **race list**, plain `go test` for the other packages, real backends | the current shard time, about 12 min plus queue |
| **T3** | Every push to `main` (post-merge) and nightly | The full matrix as today: `-race` on all packages, both lanes, all shards | unchanged, about 14 min median |

### T1: affected packages

`scripts/ci_changes.py` is extended (it keeps its `docs_only` output byte for byte) with an `affected` result computed from the merge
commit it already inspects:

1. Changed Go source or test file or testdata under `internal/<pkg>/` or `cmd/<x>/` maps to that package.
2. The result adds the reverse dependencies, taken from `go list -f '{{.ImportPath}} {{join .Imports " "}} {{join .TestImports " "}}
   {{join .XTestImports " "}}' ./...` (test imports count: a test of package B that imports changed package A is affected).
3. **Fail closed.** Any changed path that is neither a documentation prose path (the current allowlist) nor under a package directory
   (`go.mod`, `go.sum`, `schemas/`, `docs/*.json`, `scripts/`, `.github/`, spec `contracts/` and `examples/`, the Makefile, anything
   read by a test through a relative path) selects **all** packages. An unreadable diff, a non-merge checkout or a failing `go list`
   selects all packages.
4. The output is a sorted newline list in `GITHUB_OUTPUT` plus the reason (`docs-only`, `packages`, `all`), printed in the step summary.

A documentation-only change (the existing classifier) runs T1 without tests and without installing the sandbox.

Whether T1 needs the 103 s sandbox install is **unmeasured**: `STW_REQUIRE_NATIVE_SANDBOX=1` makes the sandbox-dependent packages fail
without it. The T1 job installs the sandbox only when the affected set contains a package that needs it, and WP-1 measures which part
of the 103 s is `apt-get update`. The fixed T1 floor from the measured steps is checkout 2 s, setup-go 12 s, Node 1 s, Hugo 1 s, static
checks 24 s, shard coverage 6 s, import boundaries 7 s, Pages artifact 1 s = about 55 s without the sandbox, about 160 s with it, plus the
affected tests (unmeasured: WP-1 records `go test -json` per-package elapsed). The 5 minute target is feasible when the affected tests
fit in about 2 minutes; the cost of the widest case (`all`) is measured in WP-1 and reported before WP-2 is merged, and if T1 cannot meet
5 minutes in the `all` case it ships at the measured time and the open question Q3 decides.

### The race list

`-race` matters only where code starts goroutines or takes locks. The list is **derived from the code on every run** by
`scripts/ci_changes.py race-packages`, not maintained by hand: a package is on it when a non-test file imports `sync`, `sync/atomic` or
`golang.org/x/sync`, starts a goroutine or creates a channel, or when one of its test files calls `t.Parallel()` or starts a goroutine.
Measured on `origin/main` (`9f7ae9e`) by searching the sources:

| On the race list | Why (non-test source unless noted) |
| --- | --- |
| `cmd/stw` | signal handling goroutine |
| `internal/hosting` | `dispatch.go`, `tidy_evidence_session.go`, `watch_reader.go`; tests with goroutines |
| `internal/inspection` | `inventory_stream.go`, `pins_run.go`, `repos.go`; parallel tests |
| `internal/pulls` | `cycle.go` worker pool |
| `internal/release` | `build.go`, `build_checkout_view.go`, `schema.go`; parallel tests |
| `internal/stwsync` | `journal.go`, `status.go`, `sync.go` |
| `internal/watchcmd` | `observer.go` |
| `internal/workspace` | `git_planning.go`, `native_ref_transaction.go`, `native_stream.go`, `tidy_v3_*` |
| `internal/watch`, `internal/acceptance` | tests only (`t.Parallel()` or goroutines) |

Not on it today: `internal/cli`, `internal/contracts`, `internal/overview`, `internal/pullscmd`, `internal/releaseworkflow`,
`internal/restack`, `internal/statusremote`, `internal/untrusted`. `internal/statusremote` and the Spec 037 worker pools
(`Remote` orchestration, WP-R) join the list automatically when their goroutines land. A package that only calls a listed package is not
listed (the integration of `internal/cli` with the cycles is exercised by T3). `internal/workspace`, the only large package, is raced
in all shards as today. The saving of this list is **unmeasured**: the per-package race times are not in the run data, so WP-1 records
them before the list is used to cut anything; the race list does not change shard 1 to 4.

## When T2 runs and who decides

The decision is a pure function of the event, implemented in `scripts/ci_tier.py` (new) and tested as a truth table
([contracts/tiers.md](contracts/tiers.md)). A `tier` job (checkout-free, `contents: read`, `pull-requests: read`, `checks: read`)
calls it and publishes `t2_required`, `reason` and `stack_tip` as job outputs and in the step summary.

**Top of stack, deterministic.** A pull request is *stacked* when its base branch is not the default branch. It is *the top* when no
open pull request has its head branch as base: `GET repos/{repo}/pulls?state=open&base=<head.ref>&per_page=1` returns no element. A pull
request from a fork is never stacked (its head branch name is not a branch of the base repository) and never labelled by CI. The rule
uses only the event payload and one read-only API call, so the same inputs always give the same result.

**Triggers of T2:**

| State of the pull request | T1 | T2 | Reason shown |
| --- | --- | --- | --- |
| documentation-only | yes (no tests) | no | `docs-only` |
| Draft, not labelled | yes | no | `draft` |
| carries `ci-full` (any state) | yes | yes | `label` |
| not Draft, base is the default branch | yes | yes | `unstacked` |
| not Draft, stacked, an open PR is stacked on it | yes | no | `deferred-to-tip #N` |
| not Draft, stacked, no open PR is stacked on it (the top) | yes | yes | `stack-tip` |
| `ready_for_review` event on a PR that qualifies above | yes | yes unless the same head already has green T2 evidence | `ready` |

**Who triggers it.** The owner's rule "Ready means mergeable" makes green required checks part of Ready, so T2 must be **green before the
flip to Ready**, not started by it. Therefore the foreman applies the label `ci-full` to the top PR (Draft, with its named blocker
"T2 pending") as soon as the head is frozen for the independent review, so T2 runs while the review runs (this is the "push early"
rule). Ready then needs: exact-head review CLEAR, T1 and T2 green on that head, no other blocker. The `ready_for_review` trigger is
the **safety net**: a PR that is flipped to Ready without T2 evidence for its head gets T2 at that moment and shows pending, so a
non-tested head can never appear mergeable (its `suite` check is not green). A label applied after the last push uses the existing
`ci-full.yml` path (a `labeled` event, which does not re-run T1); later pushes to a labelled PR re-run T1 and T2 through `ci.yml`.

**Evidence reuse.** Before starting shards, the `tier` job asks `GET repos/{repo}/commits/{head.sha}/check-runs` whether both
`Git/jj conformance (<lane>) / suite` runs already concluded `success` for the same SHA; if so the new run reports `suite` as success with
the reason `evidence from run <id>` and starts no shard. This removes the duplicate that the `labeled` and `ready_for_review` events would
otherwise cause for a head that was just tested.

**Stack changes.**

| Change | Effect | Rule |
| --- | --- | --- |
| A child PR is opened on the parent | the parent is no longer the top; a T2 already run on it stays valid evidence | no action |
| The child is closed without merge | the parent becomes the top silently (no event reaches it) | the foreman applies `ci-full` or pushes; this is the one gap, listed as risk R3 |
| The bottom PR is merged by rebase | GitHub retargets the child to the default branch, its SHAs change and the foreman restacks | the restack is a push and recomputes the tier; no `edited` trigger is added (a title or body edit must not restart or cancel a T2 run) |
| A rebase of the whole stack | every head changes | T1 on each pushed head, T2 only on the top and only if labelled or ready; the foreman labels after the last push of the stack |

**The invariant.** (I1) The exact head of the top or unstacked PR has a green T2 (`suite` of both lanes) before the owner merges the
**first** PR of its stack. (I2) A non-top PR reports `suite` green with reason `deferred-to-tip #N`; it becomes Ready only when the
top PR's T2 is green on a head that **contains** its head (`git merge-base --is-ancestor`), and the Ready body names that tip head
SHA. (I3) The owner merges by rebase, which rewrites SHAs, so every merge triggers T3 on the new `main` head. I1 and I2 are procedure for
the cross-PR part (a job cannot be re-evaluated when another PR's run finishes without a `workflow_run` trigger, which this specification
does not add: see Security); I1 is mechanical for the top PR itself because its `suite` fails closed when shards were required and
skipped.

## Required checks and rulesets (owner settings; listed only)

No branch protection exists today ([development](../../docs/development.md)). When it is added, require **exactly** these names, which stay
stable through every change below:

| Check name | Meaning after this specification | Always reports |
| --- | --- | --- |
| `check` | T1 (static checks, affected tests, architecture); no longer contains the race step | yes |
| `Git/jj conformance (minimum) / suite` | T2 verdict of the minimum lane | must be made to (see below) |
| `Git/jj conformance (current) / suite` | T2 verdict of the current lane | must be made to (see below) |

Today the conformance caller job is skipped on a stacked PR, so these two names do not report at all there: a ruleset requiring them
would leave such a PR `Expected` forever, and GitHub treats a skipped required job as passed. The workflow change of WP-3 therefore
**always calls** the conformance workflow and passes `t2_required`; the `suite` job passes on `success`, passes on `skipped` only when
`t2_required` is `false` and the event is a pull request (reason printed), and fails otherwise (the existing docs-only rule becomes one
row of the same truth table). Never require a shard name, `changes`, `tier`, `pages` or a `Race tests` step. The `tier` job name is
informational. The owner decides whether to enable required checks, "dismiss stale approvals" and "require branches to be up to date"
(the last one would turn every merge of a stack into a rebase plus a T1/T2 re-run and is **not** recommended with the rebase-merge flow).

## Concurrency

- The workflow-level group stays `ci-<workflow>-<ref>` with `cancel-in-progress` for pull requests: a newer push supersedes T1 and T2 of
  the older head (measured: 28 of 48 PR runs were superseded, which T1's low cost makes cheap).
- T2 gets its own job-level group `t2-<head ref>` with `cancel-in-progress: true`, used by **both** callers (`ci.yml` and `ci-full.yml`).
  Today the label run lives in `ci-full-<ref>` with `cancel-in-progress: false`, so a push cannot cancel a label run for an older head;
  with one group a push supersedes the stale T2, and evidence reuse prevents a duplicate for the same SHA.
- Pushes to `main` keep `cancel-in-progress: false`: a post-merge run always finishes; GitHub keeps at most one pending run per group.
- T3 nightly uses its own group `t3-nightly` and never cancels a push run.

## Security

- Triggers stay `pull_request` and `push`/`schedule`/`workflow_dispatch` on the default branch. **No `pull_request_target`, no
  `workflow_run`**: a PR never runs with a write-capable token or a secret, and a fork cannot influence a privileged job.
- Least privilege per job: `changes`/`tier` get `contents: read`, `pull-requests: read`, `checks: read`; shards and `check` keep
  `contents: read`; only `pages` has `pages: write` and `id-token: write`, as today. The label is applied by the foreman with its own
  credentials, never by a workflow.
- All actions stay pinned by commit SHA; no new third-party action is added. `gh` is used from the runner image with the job token.
- The `tier` job and `ci_changes.py` run PR-controlled code (the PR's own copy of the script). This is accepted in proportion to the
  threat model: the owner reviews every change to `.github/` and `scripts/` (CODEOWNERS), such a change is classified `all` by the script
  and T3 re-tests `main` after the merge. Running the script from the base branch instead is open question Q6.
- Cached Git build and hash-pinned backends are unchanged.

## Estimated saving (model, from the measured medians above)

Model inputs, all measured except where marked: full run 116 runner min, completed run wall 19.1 min, `check` race step 7.9 min
(removable from the T2 run because shard 0 covers the same packages with the same flags: measured duplication), **target** T1 = 5 runner
min, T2 = 116 - 7.9 = about 108 runner min.

| Scenario | Measured today | Model after | Saving |
| --- | --- | --- | --- |
| The Spec 037 stack (22 PR runs), optimistic: 1 T2 on the tip, 21 T1 | 1635 runner min | 108 + 21 x 5 = 213 | 1422 min (87%) |
| The same stack, pessimistic: every one of the 6 runs of the tip is a T2, the other 16 are T1 | 1635 runner min | 6 x 108 + 16 x 5 = 728 | 907 min (55%) |
| Wall time to a result on a non-top PR head | 19.1 min (PR on `main`), 38.0 min (stacked path) | 5 min **target** | 14 to 33 min |
| Wall time of the top PR | 19.1 min | about 12 min of shards plus queue (unmeasured until WP-3) | not claimed |
| Jobs requested per push of a non-top PR | 17 (or 4) | 2 (`changes`/`tier`, `check`) **target** | 15 jobs less per push |

The model does not count the fixed T1 floor beyond the 5 minute target and does not assume any change to the cancelled-run share
(25% of runner minutes today); the saving of cancelled runs is automatic because a cancelled T1 costs at most 5 minutes against up to
37. Further levers, each **unmeasured** until WP-1 and listed as questions: removing the sandbox install where unneeded (22.5 min of
every full run), rebalancing shard 0 and shard 1 (the long poles), a single lane on T2.

## Risks

| ID | Risk | Mitigation |
| --- | --- | --- |
| R1 | A flaky T2 job blocks the top PR (a 12 minute job failing at minute 11) | Re-run failed jobs once per head (`gh run rerun --failed`), record job and test in the PR; a second failure on the same head is treated as real. Flakes are listed in `docs/progress.md` with a fix owner. Not retried silently by the workflow. |
| R2 | A race only T3 finds (a package off the race list, or a path the affected set missed) | T3 runs after every merge; a red `main` is reported by `stw status` (Spec 037 `ci-main`). The foreman opens a fix PR at once and prepares a revert PR; the **owner** decides and merges the revert. The cause (package not on the list, or an uncovered path) gets a regression test and the derivation rule or fail-closed rule is widened. |
| R3 | A stack change without event leaves a top PR with a deferred `suite` (child closed without merge) | The foreman checks "is it the top now" when it closes a child and labels; the Ready rule (I1) requires T2 evidence in the body, not only a green status. |
| R4 | The derived affected set misses a dependency (a test reads a file outside its package) | Fail closed rules in section "T1: affected packages"; every miss found by T3 widens them with a test. |
| R5 | Evidence reuse trusts a prior green check on the same SHA | The check runs are read from the same repository and the same workflow names; a SHA is immutable. Forks cannot produce them. |
| R6 | Required names stop reporting after a refactor | `scripts/test_ci_workflow.py` pins the names and the always-call rule; the owner's list above is the contract. |

## Rollout

Each step is a pull request of its own, stacked linearly, test-first (see [plan.md](plan.md)).

1. **WP-1** measure and extend `ci_changes.py` (no workflow change): `affected`, `race-packages`, tests, per-package timings.
2. **WP-2** the T1 job: `check` without the race step, affected tests, sandbox only when needed.
3. **WP-3** tier routing: `ci_tier.py`, the `tier` job, the always-called conformance workflow with `t2_required`, label and ready
   triggers, evidence reuse, concurrency group, the new `suite` truth table, docs.
4. **WP-4** T3: post-merge full matrix and the nightly schedule (skipped when `main` has no new commit since the last green T3), race
   only on T3 for the non-list packages.

## Test plan

- `scripts/test_ci_changes.py` (extend the existing `DocsOnly*` tests in `scripts/test_ci_workflow.py` or add a module): reverse
  dependency closure on a synthetic import graph; test-import edges; fail-closed rows (workflow, `go.mod`, testdata outside a package,
  non-merge checkout, empty diff, `go list` failure); the `docs_only` output byte for byte; race-list derivation on a temporary tree
  (`sync` import, goroutine in a test only, channel, none) and the current list on the real tree as a pinned expectation.
- `scripts/test_ci_tier.py`: the truth table of the section "When T2 runs" (every row, plus draft x label x stacked x tip x fork x event
  combinations), evidence reuse with a fake check-run reader, API failure (fails closed: T2 required), child detection with a fake pull
  request list.
- `scripts/test_ci_workflow.py` additions in the style of the existing structure tests: triggers (no `pull_request_target`, no
  `workflow_run`, `ready_for_review` and `labeled` present, `edited` absent), per-job `permissions`, actions pinned by SHA, the
  conformance caller has no `if:`, the `suite` truth table including the new skipped row, required names present, the `t2-` group in both
  callers, the nightly schedule only on the default branch.
- A dry run on the specification branch itself (WP-1) compares `affected` for the last 60 PR heads with what a full run would have
  needed (read only), so the fail-closed rules are checked against real diffs before WP-2.

## Open questions (owner)

| ID | Question | Recommendation |
| --- | --- | --- |
| Q1 | Run only the `current` lane on T2 and both lanes on T3? It halves the T2 conformance (a lane is about 53 of the 108 runner min). | Keep both lanes on T2 until the measured T2 time after WP-3 says otherwise; the minimum lane guards the supported floor. |
| Q2 | May an agent re-run a failed T2 job once per head (`gh run rerun --failed`) without asking? | Yes, once per head, recorded in the PR (R1). |
| Q3 | If T1 for the widest (`all`) case cannot meet 5 minutes, accept the measured time or split the Go tests into a second parallel job? | Accept the measured time first; split only if it exceeds 8 minutes. |
| Q4 | Is `ready_for_review` the safety net (T2 starts if no evidence exists), or should an unlabelled flip to Ready simply fail with a message? | The safety net: it never leaves a Ready PR untested and costs nothing when the label ran first. |
| Q5 | May the foreman add the `ci-full` label to the top PR itself (today it is "the owner or dispatcher")? | Yes: labels on its own PRs are within "PRs, issues and boards". |
| Q6 | Run `ci_changes.py` from the base branch instead of the PR copy (stops a PR from classifying itself as docs-only)? | Not now: proportionate to the review and T3; revisit if a bot or fork gets write access. |
| Q7 | Add a nightly T3 at all, given a push to `main` already runs it? In the sample 12 pushes ran in 6 hours. | Yes but skipped when no commit is new since the last green T3; it only guards quiet days and pin drift (Git/jj releases, apt). |
| Q8 | Which of the sandbox-install options: skip where unneeded, cache the apt archives, or move the profile activation into a prebuilt image? | Measure first (WP-1); skip where unneeded is the zero-risk step. |
| Q9 | Should the Ready evidence for non-top PRs of a stack be mechanical (a `workflow_run` or a re-runnable `stack-gate` job) instead of procedure? | Procedure for now (no `workflow_run`); revisit after the first stack under the new tiers. |
