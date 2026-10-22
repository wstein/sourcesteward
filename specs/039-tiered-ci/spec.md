# Specification: tiered, faster CI

**Created**: 2026-10-22 | **Status**: proposed (spec-light; open questions decided by the owner 2026-10-22, see "Decisions": this page, [contracts/tiers.md](contracts/tiers.md) and
[plan.md](plan.md) are the whole design; documentation only, no workflow or script is changed by this specification). It
**amends** the CI tier design recorded in [development](../../docs/development.md) (the `CONFORMANCE_TIER` expression and the
`ci-full` label) and keeps the shard design of `scripts/ci_shards.py`.

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
| `Install distro sandbox` step (apt update and install of bubblewrap, apparmor, profiles) | median 104 s (8 to 260 s, see below) | 13 (`check` and 12 shards) | about 22.5 *M* (13 x the median; 19% of the run) |
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

### Queueing

The sample does not reproduce the owner's 25 minutes: the longest wait from run creation to the first job start was 14.4 min and the
longest wait of one job (`started_at - created_at`) 11.9 min (run 39860237175). Of 58 runs with jobs, 21 had a job that waited 5 min
or more and 3 a job that waited 10 min or more; the median run waited 46.5 s for its first job. In the same run 39860237175 the
`suite` jobs were created about 12 min after their shards had finished, so the 36 min wall time of that push is partly orchestration
delay and not test time. A run with 17 jobs asks the runner pool for 17 jobs at once; the target design asks for 3 to 4 on most pushes.

## Goals

- **G1.** T1 on every pull request head, target **under 5 minutes** wall time (a goal; if WP-1 measures more, the target becomes the measured p90 plus 20% and this specification is amended, Decision D3): formatting, vet, documentation and architecture checks,
  and plain `go test` of the packages affected by the diff and their reverse dependencies.
- **G2.** The heavy suite (race on the concurrent packages and the 12 conformance shards) runs once per stack: on the **top** pull request
  of a stack, and on an unstacked pull request when it is ready for review or carries `ci-full`.
- **G3.** Keep the invariant: a PR is Ready (mergeable) only when a green T2 exists on a head that contains its exact head (its own head for
  the top or an unstacked PR, the stack tip's head for the others), so nothing the owner merges was untested at the T2 level, and `main` is
  re-tested at T3 after each merge.
- **G4.** Keep the required status names stable (`check` and `Git/jj conformance (<lane>) / suite`) and make them always report.
- **G5.** Spend fewer runner minutes: remove the measured duplication and the sandbox install where a tier does not need it.
- **G6.** Stay inside the existing security model: `pull_request` only, least privilege, pinned actions, no new secret.

## Non-goals

- No workflow, script, test or settings change in the pull request of this specification. Branch protection, rulesets, labels and
  environments are the owner's; this document only lists them.
- No merge queue, no self-hosted runner, no change of the shard design, the pinned Git/jj lanes or the Pages deploy.
- No skipping of tests on `main`: T3 stays complete.
- No change of the Ready rule of the workspace `AGENTS.md`; this specification states how its evidence is produced. A non-top PR stays
  Draft with the blocker "T2 pending on #N" until the tip's T2 evidence exists.

## The tier model

| Tier | Runs on | Content | Target wall time |
| --- | --- | --- | --- |
| **T1** | Every pull request head (every `opened`, `synchronize`, `reopened`, `ready_for_review` event) and every push to `main` | `gofmt`, `go vet`, `go mod verify`, tidy check, Python and Node script tests, `scripts/check-docs.py`, `scripts/check-architecture.py`, shard coverage check, Pages artifact check, plain `go test` (no `-race`) of the affected packages. A documentation-only change runs T1 without the Go tests. | under 5 min |
| **T2** | The top pull request of a stack; an unstacked non-draft pull request; any pull request carrying `ci-full` (see "When T2 runs") | T1 plus the **`current`** Git/jj conformance lane only (shards 0 to 5): `-race` for the **race list**, plain `go test` for the other packages, real backends | the current shard time, about 12 min plus queue |
| **T3** | Every push to `main` (post-merge) and nightly | The full matrix as today: `-race` on all packages, both lanes (`minimum` and `current`), all shards | unchanged, about 14 min median |

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

Whether T1 needs the sandbox install (median 104 s, 8 to 260 s) is **unmeasured**: `STW_REQUIRE_NATIVE_SANDBOX=1` makes the sandbox-dependent packages fail
without it. The T1 job installs the sandbox only when the affected set contains a package that needs it, and WP-1 measures which part
of the install is `apt-get update`. The fixed T1 floor from the measured steps is checkout 2 s, setup-go 12 s, Node 1 s, Hugo 1 s, static
checks 24 s, shard coverage 6 s, import boundaries 7 s, Pages artifact 1 s = about 55 s without the sandbox, about 160 s with it (median install), plus the
affected tests (unmeasured: WP-1 records `go test -json` per-package elapsed). The 5 minute target is feasible when the affected tests fit in about 2 minutes; WP-1 measures the widest case (`all`) and the target is then set by Decision D3.

### The race list

`-race` matters only where code starts goroutines or takes locks, explicitly or implicitly. The list is **derived from the code on every
run** by `scripts/ci_changes.py race-packages`, not maintained by hand, and the derivation is **AST based** (Go `go/parser`, no text
search, so comments and strings never count). A package is on it when a non-test file contains: a `go` statement; an import of `sync`,
`sync/atomic` or `golang.org/x/sync/...`; a channel type; a `time.AfterFunc`; or an **implicit goroutine**, meaning an `os/exec`
command whose `Stdin`, `Stdout` or `Stderr` is assigned something other than an `*os.File` (the standard library then starts copying
goroutines), or an `io.Copy` or `io.Pipe` between such streams. A package is also on it when one of its test files calls `t.Parallel()` or
starts a goroutine. Reverse dependencies for T1 come from `go list -json` (`Imports`, `TestImports`, `XTestImports`) or
`go list -deps`, not from the race derivation. Measured on `origin/main` (`9f7ae9e`) by searching the sources (the AST derivation of
WP-1 must reproduce or extend this table, and WP-1 records any difference):

| On the race list | Why (non-test source unless noted) |
| --- | --- |
| `cmd/stw` | signal handling goroutine |
| `internal/hosting` | `dispatch.go`, `tidy_evidence_session.go`, `watch_reader.go`; `transport.go` runs `os/exec`; tests with goroutines |
| `internal/inspection` | `inventory_stream.go`, `pins_run.go`, `repos.go`; `os/exec` with buffered stdio (`git.go`, `jj.go`); parallel tests |
| `internal/pulls` | `cycle.go` worker pool |
| `internal/release` | `build.go`, `build_checkout_view.go`, `schema.go`; `os/exec` and `io.Copy` (`vcs_git.go`, `attest.go`); parallel tests |
| `internal/restack` | implicit goroutines: `git.go` lines 104 to 111 give `exec.Cmd` a `bytes.Reader` as `Stdin` and `limitedBuffer` values as `Stdout` and `Stderr` |
| `internal/stwsync` | `journal.go`, `status.go`, `sync.go`; `os/exec` with buffered stdio (`git.go`) |
| `internal/watchcmd` | `observer.go` |
| `internal/workspace` | `git_planning.go`, `native_ref_transaction.go`, `native_stream.go`, `tidy_v3_*`; `os/exec` and `io.Copy` pipes |
| `internal/watch`, `internal/acceptance` | tests only (`t.Parallel()` or goroutines) |

This corrects the first draft, which listed `internal/restack` as off the list. Not on it today: `internal/cli`, `internal/contracts`,
`internal/overview`, `internal/pullscmd`, `internal/releaseworkflow`, `internal/statusremote`, `internal/untrusted`, and `cmd/gh-jj`
(it passes `os.Stdout` and `os.Stderr`, both `*os.File`, to `internal/cli`). WP-1 verifies by AST that `internal/cli` reaches `os/exec`
only through listed packages. `internal/statusremote` and the Spec 037 worker pools (`Remote` orchestration, WP-R) join the list
automatically when their goroutines land. A package that only calls a listed package is not listed (the integration of `internal/cli`
with the cycles is exercised by T3). `internal/workspace`, the only large package, is raced in all shards as today. The saving of this
list is **unmeasured**: the per-package race times are not in the run data, so WP-1 records them before the list is used to cut
anything; the race list does not change shard 1 to 4.

## When T2 runs and who decides

The decision is a pure function, implemented in `scripts/ci_tier.py` (new) and tested as a truth table
([contracts/tiers.md](contracts/tiers.md)). A `tier` job calls it and publishes `t2_required`, `reason`, `evidence`, `stack_tip` and
`stack_line` as job outputs and in the step summary. The job **checks out only the base ref** (`pull_request.base.sha`, the trusted
copy of the scripts) with `contents: read`, runs the **base copy** of `ci_tier.py` and `ci_changes.py` and never executes a script from
the pull request; it reads the PR's file list through the API (`GET pulls/<n>/files`; a list that is truncated or fails means
`docs_only=false`). Its token permissions are `contents: read`, `pull-requests: read`, `checks: read` and `actions: read`; none writes.

**Open same-repo children.** The children of a PR are the **open** pull requests whose base is its head branch **and whose head
repository equals the base repository** (`GET repos/{repo}/pulls?state=open&base=<head.ref>&per_page=100`, filtered on
`head.repo.full_name`). A fork PR that targets an agent branch is not a child: it never makes its parent `deferred`, it is itself
treated as a PR whose own T2 is required (it cannot be a tip of a same-repo stack). A closed or merged PR is not a child.

**Top of stack, deterministic.** A pull request is *stacked* when its base branch is not the default branch. It is *the top* when it has
no open same-repo child. The rule uses only the event payload and read-only API calls, so the same inputs always give the same result.

**Evidence, never by name alone.** T2 evidence for a head SHA is accepted only when it comes from a workflow run that (a) belongs to this
repository's `.github/workflows/ci.yml` or `.github/workflows/ci-full.yml` (the run `path`), (b) has `event` equal to `pull_request`,
(c) has `head_repository` equal to the base repository, (d) has `head_sha` equal to the SHA asked for, (e) is created by the GitHub
Actions app, and (f) whose `Git/jj conformance (current) / suite` job concluded `success`. It is read from
`GET repos/{repo}/actions/runs?head_sha=<sha>&event=pull_request` and the run's jobs, and the accepted run id is printed. A same-named
check emitted by a fork or another app is ignored (it fails (c) or (e)). **Residual risk, accepted:** a `pull_request` run tests the
merge commit of the head with the base **as it was when the run started**; when the base moves afterwards, the evidence describes an
older merge. T3 on `main` after each merge covers this (I3), and the merge by rebase changes the SHAs anyway.

**Triggers of T2** (the first matching row of the table in [contracts/tiers.md](contracts/tiers.md) applies; the child check comes
before the default-branch check, so the bottom PR of a stack does not run T2 on every Ready head):

| State of the pull request | T1 | T2 on its own head | Reason shown |
| --- | --- | --- | --- |
| documentation-only | yes (no tests) | no | `docs-only` |
| same-head T2 evidence of its own | yes | no (evidence reused) | `evidence from run <id>` |
| carries `ci-full` (any state) | yes | yes | `label` |
| Draft, not labelled | yes | no | `draft` |
| not Draft, has an open same-repo child | yes | no; Ready only with tip evidence (below) | `deferred-to-tip #N` |
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

Today the conformance caller job is skipped on a stacked PR, so these two names do not report at all there: a ruleset requiring them
would leave such a PR `Expected` forever, and GitHub treats a skipped required job as passed. The workflow change of WP-3 therefore
**always calls** the conformance workflow and passes `t2_required`; the `suite` job passes on `success`, passes on `skipped` only when
the `tier` reason is `docs-only`, `draft`, own evidence, tip evidence or (lane `minimum`) `minimum lane runs on T3` on a pull request (reason printed), and fails otherwise (including `T2 pending on #N` and `t2_required` true) (the existing docs-only rule becomes one
row of the same truth table). Never require a shard name, `changes`, `tier`, `pages` or a `Race tests` step. The `tier` job name is
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
- T3 nightly uses its own group `t3-nightly` and never cancels a push run.

## Security

- Triggers stay `pull_request` and `push`/`schedule`/`workflow_dispatch` on the default branch. **No `pull_request_target`, no
  `workflow_run`**: a PR never runs with a write-capable token or a secret, and a fork cannot influence a privileged job.
- Least privilege per job: `tier` gets `contents: read`, `pull-requests: read`, `checks: read`, `actions: read`; shards and `check` keep
  `contents: read`; only `pages` has `pages: write` and `id-token: write`, as today. The label is applied by the foreman with its own
  credentials, never by a workflow.
- All actions stay pinned by commit SHA; no new third-party action is added. `gh` is used from the runner image with the job token.
- The tier decision runs **trusted code only**: the `tier` job checks out the base ref, runs the base copy of the scripts and reads the PR
  file list through the API; it never runs a script from the PR. The `affected` package list (T1 only) is computed in the `check` job,
  which already checks out the PR with `contents: read` and no secret; a tampered script can at worst shrink its own T1 tests, never T2 or
  T3, which the `tier` job decides, and a change to `.github/` or `scripts/` is classified `all` by the trusted copy.
- Cached Git build and hash-pinned backends are unchanged.

## Estimated saving (measured versus modelled)

**Measured** (section "Measured baseline"): full run 116 runner min, completed run wall 19.1 min, `check` race step 7.9 min, the
`minimum` lane 54.3 runner min (sum of its six job medians), the Spec 037 stack 1635 runner min in 22 runs. **Modelled** (every figure
marked *M*): T1 = 5 runner min is the **target** (*M*); T2 = 116 - 7.9 (the `check` race step, duplicated by shard 0) - 54.3 (the
`minimum` lane moves to T3, Decision D1) = about 54 runner min (*M*). The model assumes the Spec 037 run counts per PR.

| Scenario | Measured today | Modelled after (*M*) | Saving (*M*) |
| --- | --- | --- | --- |
| The Spec 037 stack (22 PR runs), optimistic: 1 T2 on the tip, 21 T1 | 1635 runner min | 54 + 21 x 5 = 159 | 1476 min (90%) |
| The same stack, pessimistic: every one of the 6 runs of the tip is a T2, the other 16 are T1 | 1635 runner min | 6 x 54 + 16 x 5 = 404 | 1231 min (75%) |
| Wall time to a result on a non-top PR head | 19.1 min (PR on `main`), 38.0 min (stacked path) | 5 min **target** (*M*) | 14 to 33 min (*M*) |
| Wall time of the top PR | 19.1 min | about 12 min of shards plus queue, **unmeasured** until WP-3 | not claimed |
| Jobs requested per push of a non-top PR | 17 (or 4) | 2 (`tier`, which replaces `changes`, and `check`) **target** | 15 jobs less per push |
| Jobs requested per T2 run | 17 | 9 (`changes`/`tier`, `check`, 6 `current` shards, 1 `suite`; the `minimum` caller adds one pass-through `suite`) *M* | 7 to 8 less |

The model does not assume any change to the cancelled-run share (25% of runner minutes today); the saving of cancelled runs is
automatic because a cancelled T1 costs at most 5 minutes against up to 37. T3 gets no saving: every push to `main` still costs about
119 runner min. Further levers are **unmeasured** and are WP-1 tasks: the sandbox install (D8) and rebalancing shard 0 and shard 1.

## Risks

| ID | Risk | Mitigation |
| --- | --- | --- |
| R1 | A flaky T2 job blocks the top PR (a 12 minute job failing at minute 11) | An agent may re-run a failed T2 job once per head only for an infrastructure failure (Decision D2), never for a test failure; a failure after the re-run is real. Flakes are listed in `docs/progress.md` with a fix owner. Not retried silently by the workflow. |
| R2 | A race only T3 finds (a package off the race list, or a path the affected set missed) | T3 runs after every merge; a red `main` is reported by `stw status` (Spec 037 `ci-main`). The foreman opens a fix PR at once and prepares a revert PR; the **owner** decides and merges the revert. The cause (package not on the list, or an uncovered path) gets a regression test and the derivation rule or fail-closed rule is widened. |
| R3 | A stack change without event leaves a top PR with a deferred `suite` (child closed without merge) | The foreman checks "is it the top now" when it closes a child and labels; the Ready rule (I1) requires the API evidence of a green T2, not only a green status; the foreman removes `ci-full` from a PR that stops being the top (D5). |
| R7 | Evidence for a non-top PR comes from a descendant tip and from a merge commit made with an older base | The ancestry check ties it to the exact head; T3 on `main` after every merge covers base drift (I3); the cost of re-testing the tip after a lower push is accepted (see "Evidence for a non-top PR"). |
| R4 | The derived affected set misses a dependency (a test reads a file outside its package) | Fail closed rules in section "T1: affected packages"; every miss found by T3 widens them with a test. |
| R5 | Evidence reuse trusts a check on the same SHA | Evidence is filtered by workflow path, event `pull_request`, head repository equal to the base repository, GitHub Actions app and the run id; a check name alone is never accepted (a fork can emit a same-named check). |
| R6 | Required names stop reporting after a refactor | `scripts/test_ci_workflow.py` pins the names and the always-call rule; the owner's list above is the contract. |

## Rollout

Each step is a pull request of its own, stacked linearly, test-first (see [plan.md](plan.md)).

1. **WP-1** measure and extend `ci_changes.py` (no workflow change): `affected`, `race-packages`, tests, per-package timings.
2. **WP-2** the T1 job: `check` without the race step, affected tests, sandbox only when needed.
3. **WP-3** tier routing: `ci_tier.py`, the `tier` job, the always-called conformance workflow with `t2_required`, label (`ci-full.yml` only) and `ready_for_review` (`ci.yml`)
   triggers, evidence reuse, concurrency group, the new `suite` truth table, docs.
4. **WP-4** T3: post-merge full matrix and the nightly schedule (always runs, Decision D7), race
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
  `workflow_run`; `ci.yml` has `ready_for_review` and not `labeled`, `ci-full.yml` has `labeled`; `edited` absent), the `tier` job checks out only the base ref, per-job `permissions`, actions pinned by SHA, the
  conformance caller has no `if:`, the `suite` truth table including the new skipped row, required names present, the `t2-` group in both
  callers, the nightly schedule only on the default branch, the `minimum` lane absent from T2 and present on T3, the body line `T2 runs on the top PR #N` format (informational, printed, not a gate), the lane in the `t2-` group key.
- A dry run on the specification branch itself (WP-1) compares `affected` for the last 60 PR heads with what a full run would have
  needed (read only), so the fail-closed rules are checked against real diffs before WP-2.

## Decisions (owner, 2026-10-22)

All former open questions are decided. A Draft carrying `ci-full` is the only Draft that runs T2 (D4 and D5 read together).

| ID | Decision | Rationale |
| --- | --- | --- |
| D1 (Q1) | T2 runs the **`current`** lane (and race) only; the `minimum` lane runs on T3 (push to `main` and nightly). On a pull request the `minimum` `suite` passes with reason `minimum lane runs on T3` so its required name still reports. | Halves the T2 conformance cost (54.3 of about 116 runner min measured) and the supported floor is still tested after every merge. |
| D2 (Q2) | An agent may re-run a failed T2 job **once** per head when the failure is infrastructure (runner lost, network, apt or install step, cancelled by the platform), never for a test failure, and reports the re-run in the PR. | Infrastructure noise must not cost a rebase cycle; a test failure is evidence. |
| D3 (Q3) | The T1 target stays 5 minutes as a goal. If WP-1 measures more, the target becomes the measured p90 plus 20% and this specification is amended. | No guessing before the affected-test time is measured. |
| D4 (Q4) | A non-Draft PR reaches the `unstacked`, `stack-tip` or `deferred-to-tip` row (so `ready_for_review` starts T2 where required) when no `ci-full` label is present and no same-head evidence exists; Drafts never run T2 without the label. | A Ready PR can never be untested. |
| D5 (Q5) | The foreman agent may add `ci-full` to its own top-of-stack PR (PR metadata, not a setting) and removes it when the PR stops being the top. | The label is the deterministic trigger; a stale label would re-run T2 on pushes that no longer need it. |
| D6 (Q6) | The tier decision runs the base copy of the scripts from a base-ref-only checkout (F9); the `affected` list of T1 is computed from the PR copy inside `check`. | A PR cannot classify itself out of T2; the cost is one small trusted job. This reverses the first answer ("PR copy for now"). |
| D7 (Q7) | A nightly T3 **always** runs on schedule and also runs the `minimum` lane. | It guards drift in action versions, runner images and toolchains, which a skip on quiet days would hide. |
| D8 (Q8) | No change to the sandbox install (median 104 s, 8 to 260 s) before it is measured; see the WP-1 task below with its decision gate. An install-once artifact, if chosen, carries a sha256 recorded as a job output of the producing job and verified by every consumer job. | The cost is measured (535 steps, 869 runner min in the sample); the saving of any remedy is modelled from that distribution. |
| D9 (Q9) | A PR that is not the top of its stack is Ready only with exact-head independent review CLEAR, T1 green **and** a green T2 on a tip whose head contains its head (compare API ancestry plus filtered evidence); until then it stays Draft with the blocker `T2 pending on #N`. Its body states `T2 runs on the top PR #N`, informational only. The owner merges in stack order and T3 on `main` follows each merge. | Revised after review (F1): the earlier "CLEAR plus T1" rule could mark untested code mergeable. The cost is a re-test of the tip after any lower push. Residual risk: R7. |

### Measurement tasks left to WP-1 (unmeasured items)

0. Replay the truth table over the last 60 PR heads and list which rows each would hit.
1. Plain and race `go test -json` elapsed per package; p90 of T1 for the `affected` and `all` cases (feeds D3).
2. D8 gate: split the sandbox step into `apt-get update` and install; measure the `actions/cache` hit rate for the package set (apt archives) and
   an install-once artifact reused by the 13 jobs. **Gate:** no workflow change for the sandbox before these numbers are recorded in
   `docs/progress.md` and the owner picks one option; skip-where-unneeded for T1 follows only if the affected packages are shown not to need it.
3. The cost of the race list against `-race` on all packages (feeds the claim that the list saves anything).
4. Replay of `affected` over the last 60 PR heads (fail-closed check).
