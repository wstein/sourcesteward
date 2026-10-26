# Specification: tiered, faster CI

**Created**: 2026-10-22 | **Binding owner decision (2026-10-22): no `-race` on pull requests**, race runs nightly and weekly (D10). | **Status**: proposed (spec-light; open questions decided by the owner 2026-10-22, see "Decisions": this page, [contracts/tiers.md](contracts/tiers.md) and
[plan.md](plan.md) are the whole design; documentation only, no workflow or script is changed by this specification). It
**amends** the CI tier design recorded in [development](../../docs/development.md) (the former `CONFORMANCE_TIER` expression, removed
on 2026-10-26, and the `ci-full` label) and keeps the shard design of `scripts/ci_shards.py`.

> **Amendment 2026-10-26 (owner decision, answer "Ja, jeder PR gleich"; corrected the same day by "C yes").** First decision: every
> pull request, stacked or not, ran the `current` conformance shards, because a stacked PR skipped them and `check` then ran
> `internal/workspace` plain and timed out after 40 minutes (PR 299, run 40309873142); that cost about 78 runner minutes in 7 jobs per
> push, and a restack of three PRs needs about 21 jobs (limit 20).
>
> **Correction 2026-10-26 (owner decision "C yes": the heavy conformance gate runs only on the tip of a stack).** This restores the
> original wish below. The `current` shards run for a push to `main`, for every pull request whose base is the default branch, for the
> tip of a stack (a pull request whose head branch is not the base branch of any open pull request) and for a pull request with the
> `ci-full` label; documentation-only pull requests still skip them. A pull request whose head branch **is** the base branch of
> another open pull request of this repository (a head from a branch of this repository, not a fork) runs only the light lane: `check`
> without `internal/workspace`, which the tip contains and tests. `check` and `Git/jj conformance (current) / suite` still report on every
> pull request: `suite` passes with skipped shards for such a pull request, the same mechanism as for a documentation-only one. The
> decision is made by the `changes` job of `ci.yml` with `scripts/ci_stack.py` from the open pull requests (`pull-requests: read` for that
> job only) and fails closed: any doubt or lookup failure means tip. It is evaluated when the run starts (a new push, `reopened`, or a
> re-run of the jobs): opening or closing a child pull request starts no run of its parent. A stack of three costs one heavy run
> (about 78 runner minutes, 7 jobs) plus two light runs (about 10 minutes, 2 jobs each) per push cycle. Statements below that T2
> evidence must exist before a parent is Ready, or that a parent's `suite` fails until then (`T2 pending on #N`), describe the WP-2
> target and are replaced by this rule: the heavy gate of the tip covers every commit below it.

> **Status note (2026-10-26):** WP-1 (PR 166) and WP-3 (PR 167) are merged; WP-2 (tier routing) is not implemented, so the feature stays `planned`.

## Problem (owner, 2026-10-22)

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
| `Install distro sandbox` step (apt update and install of bubblewrap, apparmor, profiles) | median 104 s (8 to 260 s, see below); WP-1 read the job logs: 90 s of it is `needrestart` waiting for `php8.3-fpm`, `apt-get update` is 5 s, download and unpack 7 s | 13 (`check` and 12 shards) | about 22.5 *M* (13 x the median; 19% of the run) |
| `check`: `Race tests` step (all packages except `internal/workspace`) | 476 s | 1 | 7.9 |
| shard 0 (the same packages, again under `-race`, with real backends) | 506 s test step, 11.2 to 11.7 min job | 2 (one per lane) | 23 |
| shards 1 to 4 (`internal/workspace` under `-race`) | 334 to 518 s test step, 7.5 to 10.8 min job | 8 | about 70 |
| shard 5 (real CLI smoke checks) | 356 s test step, 8.0 to 8.6 min job | 2 | 16.6 |
| `check`: static checks (`make check-static`), shard coverage check, import boundaries, Pages artifact | 24 s, 6 s, 7 s, 1 s | 1 | 0.6 |
| `suite` aggregates | 3 s | 2 | 0.1 |

Per-job medians of the 12 conformance jobs (minimum / current lane): shard 0 11.2 / 11.7, shard 1 9.7 / 10.8, shard 2 8.3 / 8.0, shard 3
7.6 / 7.5, shard 4 8.9 / 9.4, shard 5 8.6 / 8.0 min. Shard 0 and shard 1 are the long poles (up to 12.8 and 12.4 min).

Two facts follow directly from the workflow text and the table: with the conformance tier on, the non-workspace packages run under
`-race` three times per run (`check`, shard 0 of both lanes, which has the same packages, `-race` and a superset of backends), and
almost every job pays the sandbox install before its first test.

**Sandbox install distribution** (measured, all 535 `Install distro sandbox` steps of the 60 runs, read only from the jobs API):
minimum 8 s, median 104 s, 90th percentile 130 s, maximum 260 s, mean 97 s; 82 steps took under 30 s and 448 took 100 s or more. The
spread is real: in runs 39813750402, 39826908439 and 39821664266 the step took 10 s and in run 39821664058 it took 130 s. The 535 steps
sum to about 869 runner min, 19% of the 4616 runner min of the sample (measured). Any saving from changing the step is therefore
**modelled** from this distribution (Decision D8), not claimed as 103 s per job.

### Share of the run that executes under `-race` (measured)

The test steps that run under `-race` today are the `check` race step (476 s) and, per lane, shard 0 (506 s) plus the four
`internal/workspace` shards (334, 375, 451 and 518 s = 1678 s): 476 + 2 x (506 + 1678) = 4844 s = **80.7 runner min of the 116**
(medians of the 16 PR runs on `main`). Shard 5 (real CLI smoke checks, 356 s per lane) does not use `-race`. How much of those 80.7 min
is the cost of the race detector itself is **unmeasured on CI** (the committed weights file holds local non-race seconds, not CI seconds).
WP-1 **measured locally** (one 10-core VM, not the CI runner; `docs/progress.md`): the race detector adds 10% to 38% of the test time (two runs: wall 13 min 46 s
against 16 min 27 s, and 11 min 49 s against 16 min 4 s), so the saving beyond the removed `check` race step (7.9 min) is real but smaller than the 80.7 min;
the CI ratio is read from the first plain run.

### Queueing

The sample does not reproduce the owner's 25 minutes: the longest wait from run creation to the first job start was 14.4 min and the
longest wait of one job (`started_at - created_at`) 11.9 min (run 39860237175). Of 58 runs with jobs, 21 had a job that waited 5 min
or more and 3 a job that waited 10 min or more; the median run waited 46.5 s for its first job. In the same run 39860237175 the
`suite` jobs were created about 12 min after their shards had finished, so the 36 min wall time of that push is partly orchestration
delay and not test time. A run with 17 jobs asks the runner pool for 17 jobs at once; the target design asks for 3 to 4 on most pushes.

## Goals

- **G1.** T1 on every pull request head, target **under 5 minutes** wall time (a goal; if WP-1 measures more, the target becomes the measured p90 plus 20% and this specification is amended, Decision D3): formatting, vet, documentation and architecture checks,
  and plain `go test` of the packages affected by the diff and their reverse dependencies.
- **G2.** The conformance suite (real Git and jj, lane `current`, **no `-race`**) runs once per stack: on the **top** pull request of a
  stack, and on an unstacked pull request when it is ready for review or carries `ci-full`.
- **G3.** Keep the invariant: a PR is Ready (mergeable) only when a green T2 exists on a head that contains its exact head (its own head for
  the top or an unstacked PR, the stack tip's head for the others), so nothing the owner merges was untested at the T2 level, and `main` is
  re-tested at T3 after each merge.
- **G3a.** No `-race` on pull requests (owner decision D10). Race coverage moves to a nightly run of all packages and a weekly run of the
  `minimum` lane, and a failure opens one tracked issue.
- **G4.** Keep the required status names stable (`check` and `Git/jj conformance (<lane>) / suite`) and make them always report.
- **G5.** Spend fewer runner minutes: remove the measured duplication and the sandbox install where a tier does not need it.
- **G6.** Stay inside the existing security model: `pull_request` only, least privilege, pinned actions, no new secret.

## Non-goals

- No workflow, script, test or settings change in the pull request of this specification. Branch protection, rulesets, labels and
  environments are the owner's; this document only lists them.
- No merge queue, no self-hosted runner, no change of the shard design, the pinned Git/jj lanes or the Pages deploy.
- No skipping of tests on `main`: T3 stays complete (plain `go test`, both lanes); only the race detector moves to the scheduled runs.
- No change of the Ready rule of the workspace `AGENTS.md`; this specification states how its evidence is produced. A non-top PR stays
  Draft with the blocker "T2 pending on #N" until the tip's T2 evidence exists.

## The tier model

| Tier | Runs on | Content | Target wall time |
| --- | --- | --- | --- |
| **T1** | Every pull request head (every `opened`, `synchronize`, `reopened`, `ready_for_review` event) | `gofmt`, `go vet`, `go mod verify`, tidy check, Python and Node script tests, `scripts/check-docs.py`, `scripts/check-architecture.py`, shard coverage check, Pages artifact check, plain `go test` (**no `-race`**) of the affected packages and their reverse dependencies. A documentation-only change runs T1 without the Go tests, except that prose which a Go test reads (declared in `scripts/ci-test-reads.json`) runs the tests of its readers. | under 5 min (goal, D3) |
| **T2** | The top pull request of a stack; an unstacked non-draft pull request; any pull request carrying `ci-full` (see "When T2 runs") | T1 plus the Git/jj **conformance** with real git and jj, lane **`current`** only, **no `-race`** (shards 0 to 5) | the current shard time, about 12 min plus queue (race removal unmeasured) |
| **T3** | Every push to `main` (post-merge) | T1 plus the normal (non-race) full matrix: `go test ./...` and conformance, **both** lanes (`minimum` and `current`), all shards | about 14 min median today, less without race (unmeasured) |
| **T4** | Schedule (cron), always, also on quiet days | **Nightly:** `go test -race ./...` of **all** packages (lane `current`, real backends). **Weekly:** the `minimum` lane with `-race`. A failure opens or updates one issue (see "Race coverage") | not time critical |

Required checks and the Ready rule see T1, T2 and T3 only; T4 never runs on a pull request.

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
5. **API file-list mode.** `ci_changes.py affected --files-from FILE` takes a newline separated path list (the API file list of the pull
   request) instead of a git diff and classifies by path only: documentation prose gives `docs-only`; **any path under `.github/` or
   `scripts/`** gives `affected_reason=all`; any other non-package path gives `all`. The trusted `tier` job runs this mode and publishes
   `affected_reason`; the `check` job **honours** it: when `affected_reason` is `all` it tests every package, otherwise it computes the
   package list from `go list` on the PR checkout. A pull request that edits `scripts/ci_changes.py` therefore always gets T1 `all`.

A documentation-only change (the existing classifier) runs T1 without tests and without installing the sandbox, unless it changes prose that a Go test reads (`README.md`, `docs/agent-guide.md` and two spec files today): then the plain tests of those readers run.

Whether T1 needs the sandbox install (median 104 s, 8 to 260 s; WP-1 re-read 105 s from the job logs) is **not decided**: `STW_REQUIRE_NATIVE_SANDBOX=1` makes the sandbox-dependent packages fail
without it. The T1 job installs the sandbox only when the affected set contains a package that needs it, and WP-1 measures which part
of the install is `apt-get update`. The fixed T1 floor from the measured steps is checkout 2 s, setup-go 12 s, Node 1 s, Hugo 1 s, static
checks 24 s, shard coverage 6 s, import boundaries 7 s, Pages artifact 1 s = about 55 s without the sandbox, about 160 s with it (median install), plus the
affected tests (WP-1, measured locally, `docs/progress.md`: all 20 packages with tests take 11 min 49 s, of which `internal/workspace` 698 s; without it the longest package takes 170 s; CI speed unmeasured). T1 never runs `internal/workspace` plain: a plain run exceeds the 40 minute timeout (run 40309873142), so `ci.yml` now leaves it to the conformance shards on every event (amendment 2026-10-26); WP-2 must shard or defer it, never run it in T1. The 5 minute target is feasible when the affected tests fit in about 2 minutes; WP-1 measures the widest case (`all`) and the target is then set by Decision D3.

### Race coverage (owner decision D10, 2026-10-22)

There is **no race list** and no derivation machinery: `-race` is not run on pull requests at all. Race coverage is the scheduled tier T4:

- **Nightly** (cron, runs every night, also when `main` has not changed: it also guards drift of action versions, runner images and
  toolchains): `go test -race ./...` of **all** packages on the `current` lane with real backends. It reuses the conformance shards with
  an input `race: true` (shard 0 plus the four `internal/workspace` shards; the serial single-job form was measured at 30.3 and 38.1 min in
  runs 39821664058 and 39813750402, so the sharded form is preferred).
- **Weekly** (cron): the same with the `minimum` lane and `-race`.
- **On failure** the workflow opens or comments on **one** issue (below). The foreman agent then fixes it by a normal pull request with a
  reproducing test (verified locally with `go test -race` of that package by the author; the next nightly confirms).
- Spec 040 "concurrency discipline" (goroutines confined to one package) may later narrow the nightly run to the packages that start
  goroutines; this specification does not depend on it.

**Failure issue (`race-failure`).** A workflow `race.yml` (`schedule`, `workflow_dispatch` on the default branch) has a job `race` and a
job `report` (`needs: race`, `if: failure() || cancelled()`). **Both jobs carry `if: github.ref == 'refs/heads/main'`**, because `workflow_dispatch` can run on any ref and `issues: write` and the race runs must never run on a topic branch; a workflow test pins this. The issue states the reason (failed, including a job timeout, or cancelled). `report` has `permissions: contents: read, actions: read, issues: write` (issues write
exists **only** in this workflow), checks out **`main` only**, runs only trusted repository scripts and never PR code. It lists open
issues with the label `race-failure`; if one exists it adds a comment, otherwise it creates the issue, so at most one is open. The body
contains the run link, the tier (nightly or weekly), the lane and the failing test names extracted from the failed job log by a strict
pattern (`^--- FAIL: (Test\w+)` and `WARNING: DATA RACE` count) with a bound on the number of names; log text is untrusted data and is
never copied except those matched names. Closing the issue is the owner's or the foreman's act after the fix lands.

**Residual risk, stated honestly.** A data race is found the next night instead of on the pull request. A race that no test exercises
is found by neither. **Ready no longer implies a race-clean head**: it implies a head that passed plain tests (and, for the top PR,
conformance) and whose `main` is race-tested every night. The cost of that is accepted by the owner for the sake of the speed.

## When T2 runs and who decides

The decision is a pure function, implemented in `scripts/ci_tier.py` (new) and tested as a truth table
([contracts/tiers.md](contracts/tiers.md)). A `tier` job calls it and publishes `t2_required`, `reason`, `evidence`, `stack_tip` and
`stack_line`, `affected_reason` as job outputs and in the step summary. The job **checks out only the base ref**
(`pull_request.base.sha`), runs the **base copy** of `ci_tier.py` and `ci_changes.py`, and reads the PR's file list through the API
(`GET pulls/<n>/files`; a list that is truncated or fails means `docs_only=false` and `affected_reason=all`). Its token permissions are
`contents: read`, `pull-requests: read`, `checks: read` and `actions: read`; none writes. **This guards only against a script-level
self-classification** (a PR editing `ci_changes.py` or `ci_tier.py`). It is not a trust boundary: see "Security".

**Open same-repo children.** The children of a PR are the **open** pull requests whose base is its head branch **and whose head
repository equals the base repository** (`GET repos/{repo}/pulls?state=open&base=<head.ref>&per_page=100`, filtered on
`head.repo.full_name`). A fork PR that targets an agent branch is not a child: it never makes its parent `deferred`, it is itself
treated as a PR whose own T2 is required (it cannot be a tip of a same-repo stack). A closed or merged PR is not a child.

**Top of stack, deterministic.** A pull request is *stacked* when its base branch is not the default branch. It is *the top* when it has
no open same-repo child. The rule uses only the event payload and read-only API calls, so the same inputs always give the same result.

**Evidence, never by name alone.** T2 evidence for a head SHA is accepted only when it comes from a workflow run that (a) belongs to this
repository's `.github/workflows/ci.yml` or `.github/workflows/ci-full.yml` (the run `path`), (b) has `event` equal to `pull_request`,
(c) has `head_repository` equal to the base repository, (d) has `head_sha` equal to the SHA asked for, (e) is created by the GitHub
Actions app, and (f) in which **all six `current` shard jobs concluded `success`** (executed: a `skipped` shard does not count). The
`suite` job is **not** evidence by itself, because it also passes with skipped shards (draft, docs-only, reused evidence, and the
`minimum` lane); a Draft-push run or a docs-only run leaves a green `suite` on its own SHA and must never count, and neither may a
suite that is itself reused evidence (no chaining). The reason of the `tier` job is not exposed by the API, so the executed shards are
the test. Evidence is read from `GET repos/{repo}/actions/runs?head_sha=<sha>&event=pull_request` and the run's jobs, and the accepted
run id is printed. A same-named
check emitted by a fork or another app is ignored (it fails (c) or (e)). **Residual risk, accepted:** a `pull_request` run tests the
merge commit of the head with the base **as it was when the run started**; when the base moves afterwards, the evidence describes an
older merge. T3 on `main` after each merge covers this (I3), and the merge by rebase changes the SHAs anyway.

**Triggers of T2** (T2 means conformance, never `-race`; the first matching row of the table in [contracts/tiers.md](contracts/tiers.md) applies; the child check comes
before the default-branch check, so the bottom PR of a stack does not run T2 on every Ready head):

| State of the pull request | T1 | T2 on its own head | Reason shown |
| --- | --- | --- | --- |
| documentation-only | yes (no tests) | no | `docs-only` |
| same-head T2 evidence of its own | yes | no (evidence reused) | `evidence from run <id>` |
| carries `ci-full` (any state) | yes | yes | `label` |
| Draft, not labelled | yes | no | `draft` |
| not Draft, has an open same-repo child | yes (without `internal/workspace`) | no; the tip runs it (amendment 2026-10-26) | `deferred-to-tip #N` |
| not Draft, base is the default branch, no child | yes | yes | `unstacked` |
| not Draft, stacked, no child (the top) | yes | yes | `stack-tip` |

`ready_for_review` is not a row of its own: it changes a Draft into a non-Draft, so the same event then reaches the `unstacked`,
`stack-tip` or `deferred-to-tip` row (the old reason `ready` is removed). That is how T2 starts as the safety net (D4) when no label
was applied.

**Who triggers it.** "Ready means mergeable", so T2 must be **green before the flip to Ready**, not started by it. The foreman applies
`ci-full` to the top PR (Draft, with its named blocker "T2 pending") as soon as the head is frozen for the independent review, so T2
runs while the review runs. The foreman removes the label when the PR stops being the top (D5). `ci.yml` reacts to `opened`,
`synchronize`, `reopened` and `ready_for_review` only; the `labeled` event triggers `ci-full.yml` only (as today), so labelling never
re-runs T1. Later pushes to a labelled PR re-run T1 and T2 through `ci.yml`.

**Evidence for a non-top PR (the invariant, F1).** A PR with an open same-repo child is Ready **only when** a green T2 exists on a head
that **contains its head**. The `tier` job finds the descendant tips (following open same-repo children to the PRs without a child),
and for each tip asks `GET repos/{repo}/compare/<P.head_sha>...<tip.head_sha>`: `status` of `ahead` or `identical` means the tip contains
the head (an ancestor check done by the server). It then asks for T2 evidence on the tip head as defined above. If one tip qualifies,
`suite` of the non-top PR passes with reason `evidence from tip #N run <id>`; otherwise `suite` **fails** with `T2 pending on #N` and
the PR stays Draft with that blocker. The tip's run finishing does not produce an event on the lower PR, so the foreman re-evaluates by
re-running the seconds-long `tier` job of the lower PR (a re-evaluation of live API state, not a retry of a failed test, so D2 does not
apply). **Cost:** a push to any lower PR after the tip's T2 changes its head, so the tip no longer contains it (compare says
`diverged`); the stack must be restacked and the tip re-tested before the lower PR can be Ready. This is the price of the invariant.
With several children, any one descendant tip that qualifies is enough.

**Stack changes.**

| Change | Effect | Rule |
| --- | --- | --- |
| A child PR is opened on the parent | the parent is no longer the top; a T2 already run on it stays valid evidence for itself | no action |
| A fork PR is opened on the parent's branch | no effect on the parent (not a same-repo child) | none |
| The child is closed without merge | the parent becomes the top silently (no event reaches it) | the foreman applies `ci-full` or pushes; the one gap, risk R3 |
| The bottom PR is merged by rebase | GitHub retargets the child to the default branch, its SHAs change and the foreman restacks | the restack is a push and recomputes the tier; no `edited` trigger is added (a title or body edit must not restart or cancel a T2 run) |
| A rebase of the whole stack | every head changes | T1 on each pushed head, T2 only on the top and only if labelled or non-Draft; the foreman labels after the last push of the stack |
| A push to a lower PR after the tip's T2 | the tip no longer contains the lower head | restack the tip and re-test it (cost stated above) |

**The invariant.** (I1) A PR is Ready only when a green T2 (filtered as above) exists on a head that contains its exact head: its own
head for the top or an unstacked PR (mechanical: its `suite` fails closed when shards were required and skipped), the tip head for a
non-top PR (mechanical: the compare and evidence read of the `tier` job, `suite` red until it passes). (I2) The owner merges in stack
order; a non-top PR that is not yet covered stays Draft with the blocker `T2 pending on #N`. (I3) The owner merges by rebase, which
rewrites SHAs, so every merge triggers T3 on the new `main` head; that is the safety net for base drift. The line `T2 runs on the top
PR #N` in a non-top PR's body is **informational** (the `tier` job prints the expected line in its summary; the foreman copies it). It
is not a gate, because no `edited` trigger exists; the gate is the API evidence check.

## Required checks and rulesets (owner settings; listed only)

No branch protection exists today ([development](../../docs/development.md)). When it is added, require **exactly** these names, which stay
stable through every change below:

| Check name | Meaning after this specification | Always reports |
| --- | --- | --- |
| `check` | T1 (static checks, affected tests, architecture); no longer contains the race step | yes |
| `Git/jj conformance (minimum) / suite` | T3 verdict of the minimum lane; on a pull request it passes with reason `minimum lane runs on T3` (Decision D1) | must be made to (see below) |
| `Git/jj conformance (current) / suite` | T2 verdict of the current lane (T3 on `main`) | must be made to (see below) |

Before the amendment of 2026-10-26 the conformance caller job was skipped on a stacked PR, so these two names did not report at all there: a ruleset requiring them
would leave such a PR `Expected` forever, and GitHub treats a skipped required job as passed. The workflow change of WP-2 therefore
**always calls** the conformance workflow and passes `t2_required`; the `suite` job passes on `success`, passes on `skipped` only when
the `tier` reason is `docs-only`, `draft`, own evidence, tip evidence or (lane `minimum`) `minimum lane runs on T3` on a pull request (reason printed), and fails otherwise (including `T2 pending on #N` and `t2_required` true) (the existing docs-only rule becomes one
row of the same truth table). Never require a shard name, `changes`, `tier`, `pages`, a `Race tests` step or anything from `race.yml` (T4 never runs on a pull request). The `tier` job name is
informational. The owner decides whether to enable required checks, "dismiss stale approvals" and "require branches to be up to date"
(the last one would turn every merge of a stack into a rebase plus a T1/T2 re-run and is **not** recommended with the rebase-merge flow).

## Concurrency

- The workflow-level group stays `ci-<workflow>-<ref>` with `cancel-in-progress` for pull requests: a newer push supersedes T1 and T2 of
  the older head (measured: 28 of 48 PR runs were superseded, which T1's low cost makes cheap).
- T2 gets its own job-level group `t2-<tier>-<head ref>-<lane>` (the lane is part of the key, so one lane never cancels the other and a
  required `suite` is never reported `cancelled` because of its sibling) with `cancel-in-progress: true` on pull requests, used by **both** callers (`ci.yml` and `ci-full.yml`).
  Today the label run lives in `ci-full-<ref>` with `cancel-in-progress: false`, so a push cannot cancel a label run for an older head;
  with one group a push supersedes the stale T2, and evidence reuse prevents a duplicate for the same SHA.
- Pushes to `main` keep `cancel-in-progress: false`: a post-merge run always finishes; GitHub keeps at most one pending run per group.
- T4 uses its own groups `t4-nightly` and `t4-weekly`, never cancels a push run, and is not cancelled by one.

## Security

- Triggers stay `pull_request` and `push`/`schedule`/`workflow_dispatch` on the default branch. **No `pull_request_target`, no
  `workflow_run`**: a PR never runs with a write-capable token or a secret, and a fork cannot influence a privileged job.
- Least privilege per job: `tier` gets `contents: read`, `pull-requests: read`, `checks: read`, `actions: read`; shards and `check` keep
  `contents: read`; only `pages` has `pages: write` and `id-token: write`, as today. The label is applied by the foreman with its own
  credentials, never by a workflow.
- All actions stay pinned by commit SHA; no new third-party action is added. `gh` is used from the runner image with the job token.
- **What the base-ref checkout does and does not protect.** On `pull_request` the workflow YAML itself comes from the PR's merge commit, so
  a same-repo pull request that edits `ci.yml` (or `ci-full.yml`, `conformance.yml`) can change the `tier` job, drop a step, or make a
  `suite` report success. The base-ref checkout guards only the script-level case (a PR editing `ci_changes.py` or `ci_tier.py` cannot
  classify itself, F15). The real boundary is the **owner's review of `.github/`** (CODEOWNERS, `* @wstein`, exists today) and **T3 on
  `main` after every merge**, which runs the merged workflows and tests, not the PR's. No claim in this specification goes beyond that.
- The `affected` package list (T1 only) is computed in the `check` job from the PR checkout with `contents: read` and no secret; a change
  under `.github/` or `scripts/` is `all` by the trusted classification. `race.yml` is the only workflow with `issues: write`; it runs
  `main` and never PR code.
- Cached Git build and hash-pinned backends are unchanged.

## Estimated saving (measured versus modelled)

**Measured** (section "Measured baseline"): full run 116 runner min, completed run wall 19.1 min, the `check` race step 7.9 min, the
`minimum` lane 54.3 runner min (sum of its six job medians), 80.7 runner min in test steps that run under `-race`, the Spec 037 stack
1635 runner min in 22 runs. **Modelled** (every figure marked *M*): T1 = 5 runner min is the **target** (*M*); T2 = 116 - 7.9 (the
`check` race step, removed by D10) - 54.3 (the `minimum` lane moves to T3 and T4, D1) = about 54 runner min (*M*). That T2 figure is an
**upper bound** of the saving of that kind: removing `-race` from the `current` conformance shards saves more, but the local ratio is only 1.1 to 1.4
(WP-1, measured locally) and the CI ratio is unmeasured, so no further saving is claimed. The model assumes the Spec 037 run counts per PR.

| Scenario | Measured today | Modelled after (*M*) | Saving (*M*) |
| --- | --- | --- | --- |
| The Spec 037 stack (22 PR runs), optimistic: 1 T2 on the tip, 21 T1 | 1635 runner min | 54 + 21 x 5 = 159 | 1476 min (90%) or more |
| The same stack, pessimistic: every one of the 6 runs of the tip is a T2, the other 16 are T1 | 1635 runner min | 6 x 54 + 16 x 5 = 404 | 1231 min (75%) or more |
| Wall time to a result on a non-top PR head | 19.1 min (PR on `main`), 38.0 min (stacked path) | 5 min **target** (*M*) | 14 to 33 min (*M*) |
| Wall time of the top PR | 19.1 min | about 12 min of shards plus queue or less, **unmeasured** until WP-2 | not claimed |
| Jobs requested per push of a non-top PR | 17 (or 4) | 2 (`tier`, which replaces `changes`, and `check`) **target** | 15 jobs less per push |
| Jobs requested per T2 run | 17 | 9 (`tier`, `check`, 6 `current` shards, 1 `suite`; the `minimum` caller adds one pass-through `suite`) *M* | 7 to 8 less |
| Cost added by T4 (per week) | 0 | 7 nightly x about 54 + 1 weekly x about 54 = about 430 runner min *M* (a `-race` run of one lane, from the medians above) | an added cost, off the pull request path |

The model does not assume any change to the cancelled-run share (25% of runner minutes today); the saving of cancelled runs is
automatic because a cancelled T1 costs at most 5 minutes against up to 37. T3 on `main` still costs about 119 runner min per push
today (measured); its race part is removed (unmeasured on CI; locally 10 to 38% of test time). Further levers: the sandbox install (D8; WP-1 measured median 105 s from job logs,
of which `needrestart` 92 s; an apt cache would save under 10 s) and rebalancing shard 0 and shard 1.

## Risks

| ID | Risk | Mitigation |
| --- | --- | --- |
| R1 | A flaky T2 job blocks the top PR (a 12 minute job failing at minute 11) | An agent may re-run a failed T2 job once per head only for an infrastructure failure (Decision D2), never for a test failure; a failure after the re-run is real. Flakes are listed in `docs/progress.md` with a fix owner. Not retried silently by the workflow. |
| R2 | A defect only the plain T3 finds on `main` (a path the affected set missed) | T3 runs after every merge; a red `main` is reported by `stw status` (Spec 037 `ci-main`). The foreman opens a fix PR at once and prepares a revert PR; the **owner** decides and merges the revert. The miss gets a regression test and the fail-closed rule is widened. |
| R8 | A data race is found the next night instead of on the PR; a race no test exercises is found by neither; Ready no longer implies a race-clean head (D10) | The nightly `-race ./...` of all packages and the weekly `minimum` lane open one `race-failure` issue; the foreman fixes it by a normal PR with a reproducing test. Accepted by the owner. |
| R9 | The issue workflow is an injection or spam surface | Only `race.yml` has `issues: write`; it checks out `main` only, runs no PR code, copies only strictly matched test names from the log, and keeps at most one open `race-failure` issue. |
| R3 | A stack change without event leaves a top PR with a deferred `suite` (child closed without merge) | The foreman checks "is it the top now" when it closes a child and labels; the Ready rule (I1) requires the API evidence of a green T2, not only a green status; the foreman removes `ci-full` from a PR that stops being the top (D5). |
| R7 | Evidence for a non-top PR comes from a descendant tip and from a merge commit made with an older base | The ancestry check ties it to the exact head; T3 on `main` after every merge covers base drift (I3); the cost of re-testing the tip after a lower push is accepted (see "Evidence for a non-top PR"). |
| R4 | The derived affected set misses a dependency (a test reads a file outside its package) | Fail closed rules in section "T1: affected packages"; every miss found by T3 widens them with a test. |
| R5 | Evidence reuse trusts a check on the same SHA | Evidence is filtered by workflow path, event `pull_request`, head repository equal to the base repository, GitHub Actions app and the run id; a check name alone is never accepted (a fork can emit a same-named check). |
| R6 | Required names stop reporting after a refactor | `scripts/test_ci_workflow.py` pins the names and the always-call rule; the owner's list above is the contract. |

## Rollout

Each step is a pull request of its own, stacked linearly, test-first (see [plan.md](plan.md)).

1. **WP-1** measure and T1: `ci_changes.py affected` (and `--files-from`), per-package and plain-versus-race timings, the replay over 60 PR heads, then the T1 job (`check` without the race step, affected plain tests, `affected_reason` honoured).
2. **WP-2** tier routing: `ci_tier.py`, the `tier` job, the always-called conformance workflow with `t2_required` (lane `current`, no `-race`), label (`ci-full.yml` only) and `ready_for_review` (`ci.yml`) triggers, filtered evidence, concurrency group, the `suite` truth table, T3 on push to `main` without race, docs.
3. **WP-3** T4: `race.yml` (nightly `go test -race ./...` of all packages, weekly `minimum` lane with race, both always) and the issue reporter (`race-failure`, one open issue, `issues: write` only there), tests, docs.
4. **WP-4** optional: the sandbox install option chosen at the D8 gate (cache or install-once artifact with a sha256), only after the WP-1 numbers.

## Test plan

- `scripts/test_ci_changes.py` (extend the existing `DocsOnly*` tests in `scripts/test_ci_workflow.py` or add a module): reverse
  dependency closure on a synthetic import graph; test-import edges; fail-closed rows (workflow, `go.mod`, testdata outside a package,
  non-merge checkout, empty diff, `go list` failure); the `docs_only` output byte for byte; the `--files-from` mode (docs-only, `.github/`
  and `scripts/` paths give `all`, a PR that edits `scripts/ci_changes.py` gets T1 `all`).
- `scripts/test_ci_tier.py`: the truth table of the section "When T2 runs" (every row, plus draft x label x stacked x tip x fork x event
  combinations), evidence reuse with a fake check-run reader, API failure (fails closed: T2 required), child detection with a fake pull
  request list.
- `scripts/test_ci_workflow.py` additions in the style of the existing structure tests: triggers (no `pull_request_target`, no
  `workflow_run`; `ci.yml` has `ready_for_review` and not `labeled`, `ci-full.yml` has `labeled`; `edited` absent), the `tier` job checks out only the base ref, per-job `permissions`, actions pinned by SHA, the
  conformance caller has no `if:`, the `suite` truth table including the new skipped row, required names present, the `t2-` group in both
  callers, `race.yml`: schedule and `workflow_dispatch` only, `issues: write` only there, `main` checkout only, no `pull_request` trigger anywhere that has `-race`, both `race.yml` jobs guarded by `if: github.ref == 'refs/heads/main'`, no `-race` in `ci.yml`, `ci-full.yml` and the conformance calls from them; the `minimum` lane absent from T2 and present on T3 and the weekly run; the body line `T2 runs on the top PR #N` format (informational, printed, not a gate), the lane in the `t2-` group key.
- A dry run on the specification branch itself (WP-1) compares `affected` for the last 60 PR heads with what a full run would have
  needed (read only), so the fail-closed rules are checked against real diffs before WP-2.

## Decisions (owner, 2026-10-22)

All former open questions are decided. A Draft carrying `ci-full` is the only Draft that runs T2 (D4 and D5 read together).

| ID | Decision | Rationale |
| --- | --- | --- |
| D1 (Q1) | T2 on a PR runs only the **`current`** lane (conformance, no `-race`); the `minimum` lane runs on T3 (push to `main`) and weekly with `-race` (T4). On a pull request the `minimum` `suite` passes with reason `minimum lane runs on T3` so its required name still reports. | Halves the T2 conformance cost (54.3 of about 116 runner min measured) and the supported floor is still tested after every merge. |
| D2 (Q2) | An agent may re-run a failed T2 job **once** per head when the failure is infrastructure (runner lost, network, apt or install step, cancelled by the platform), never for a test failure, and reports the re-run in the PR. | Infrastructure noise must not cost a rebase cycle; a test failure is evidence. |
| D3 (Q3) | The T1 target stays 5 minutes as a goal. If WP-1 measures more, the target becomes the measured p90 plus 20% and this specification is amended. | No guessing before the affected-test time is measured. |
| D4 (Q4) | A non-Draft PR reaches the `unstacked`, `stack-tip` or `deferred-to-tip` row (so `ready_for_review` starts T2 where required) when no `ci-full` label is present and no same-head evidence exists; Drafts never run T2 without the label. | A Ready PR can never be untested. |
| D5 (Q5) | The foreman agent may add `ci-full` to its own top-of-stack PR (PR metadata, not a setting) and removes it when the PR stops being the top. | The label is the deterministic trigger; a stale label would re-run T2 on pushes that no longer need it. |
| D6 (Q6) | The tier decision runs the base copy of the scripts from a base-ref-only checkout (F9); the `affected` list of T1 is computed from the PR copy inside `check`, and `affected_reason=all` for `.github/` and `scripts/` comes from the trusted job (F15). | Stops a PR from classifying itself through its scripts. It is not a trust boundary: the workflow YAML comes from the PR, so the boundary is CODEOWNERS review of `.github/` plus T3 on `main` (F14). |
| D7 (Q7) | The nightly T4 **always** runs on schedule (also on quiet days): `go test -race ./...` of all packages, `current` lane. The weekly T4 runs the `minimum` lane with `-race`. | Guards races and drift in action versions, runner images and toolchains, which a skip on quiet days would hide. |
| D8 (Q8) | No change to the sandbox install (median 104 s, 8 to 260 s) before it is measured; see the WP-1 task below with its decision gate. An install-once artifact, if chosen, carries a sha256 recorded as a job output of the producing job and verified by every consumer job. | The cost is measured (535 steps, 869 runner min in the sample); the saving of any remedy is modelled from that distribution. |
| D9 (Q9) | A PR that is not the top of its stack is Ready only with exact-head independent review CLEAR, T1 green **and** a green T2 on a tip whose head contains its head (compare API ancestry plus filtered evidence); until then it stays Draft with the blocker `T2 pending on #N`. Its body states `T2 runs on the top PR #N`, informational only. The owner merges in stack order and T3 on `main` follows each merge. | Revised after review (F1): the earlier "CLEAR plus T1" rule could mark untested code mergeable. The cost is a re-test of the tip after any lower push. Residual risk: R7. |
| D10 (owner, binding) | **No `-race` on pull requests.** T1 and T2 run plain tests; T3 on `main` runs the normal matrix without race; T4 runs `go test -race ./...` of all packages nightly and the `minimum` lane with race weekly, and a failure opens or comments on one `race-failure` issue (`race.yml`, `issues: write` only there, `main` checkout only). The foreman fixes it by a normal PR with a reproducing test. No race-list derivation. Spec 040 may later narrow the nightly run. | Speed on the PR path is worth finding a data race the next night; the residual risk is R8 and Ready no longer implies a race-clean head. |

### Measurement tasks left to WP-1 (unmeasured items)

0. Replay the truth table over the last 60 PR heads and list which rows each would hit.
1. Plain and race `go test -json` elapsed per package and the plain-versus-race ratio of the `current` conformance shards on the CI runner (the saving of D10 beyond the removed `check` step is unmeasured until then); p90 of T1 for the `affected` and `all` cases (feeds D3).
2. D8 gate: split the sandbox step into `apt-get update` and install; measure the `actions/cache` hit rate for the package set (apt archives) and
   an install-once artifact reused by the 13 jobs. **Gate:** no workflow change for the sandbox before these numbers are recorded in
   `docs/progress.md` and the owner picks one option; skip-where-unneeded for T1 follows only if the affected packages are shown not to need it.
3. The wall time and runner minutes of the sharded nightly `-race` run of all packages (the T4 cost line is modelled until then).
4. Replay of `affected` over the last 60 PR heads (fail-closed check).
