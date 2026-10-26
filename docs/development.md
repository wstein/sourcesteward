# Development

## Prerequisites and checks
Go 1.27.2+, Git 2.46+, Python 3, Node.js 22+ and pinned Hugo 0.168.0 are required for full development checks.
Node runs dependency-free theme behavior tests; CI selects Node 24. Python stages/checks public documentation and assets; the CLI has no Python dependency. Cobra v1.10.2 is pinned in go.mod/go.sum.

```sh
python3 scripts/install-hugo.py # once, checked official download
make check
go test -race ./...
go run ./cmd/stw version --json
```

`make check` checks formatting, Go tests, vet, module verification, local documentation links,
SVG structure, theme behavior/security, site assets and whitespace. It does not create release artifacts or mutate a
user repository. Tests construct a fresh command for each invocation.

## Git/jj conformance
Native jj fixture tests require stable jj 0.45.1+ and usable Linux bubblewrap or macOS
sandbox-exec. No host security restriction is disabled to make a sandbox pass. Hosts without
usable enforcement fail native inspection safely. Local tests may report explicit missing-tool
or sandbox skips; they are not conformance acceptance. CI sets `STW_REQUIRE_JJ=1`, so missing
executables, enforcement or mandatory cases fail instead of being counted as coverage.

Two pinned CI lanes use Git 2.46.0/jj 0.45.1 and Git 2.56.0/jj 0.46.0. Checked official
artifacts/source hashes are recorded in `scripts/backend-releases.json`. To install one lane
locally on a supported installer host, run `python3 scripts/install-backends.py minimum`
(or `current`) and put its ignored `.tools/conformance/<lane>/git/bin` and
`.tools/conformance/<lane>/jj` directories on PATH. Installation downloads/builds tools;
it is separate from the offline CLI. Run `python3 scripts/smoke-jj-inspection.py --preflight minimum`,
then `STW_REQUIRE_JJ=1 go test -count=1 -race ./...` and
`python3 scripts/smoke-jj-inspection.py`. Use the matching lane name for current versions.

Native inspection leaves target repository/workspace bytes unchanged. It creates and removes
private temporary isolation/probe files outside the target. Effects fixtures include edited
files, stale workspaces, divergent operation heads, configuration helpers and attempted
index/type-file repairs; source checks alone do not establish those guarantees.

## Build the local CLI

```sh
make build
./bin/stw version --json
```

The build target runs `go build -o bin/stw ./cmd/stw`. The ignored bin/ directory contains
local development output; this does not create a release or publish a binary.

## Agent guidance

The [agent guide](agent-guide.md) documents current command and output contracts;
[llms.txt](../llms.txt) indexes troubleshooting sources. Root help links to both raw
main-branch files without fetching them. Installed help remains authoritative for the
installed version. `make check` validates these repository-owned URL destinations
offline, including CLI notice links, along with ordinary local documentation links.
The public agent guide and llms.txt are included in the Pages artifact. Internal AGENTS.md,
specifications, progress and harness files remain excluded.

## Specification workflow
Feature designs live under specs/. The current native jj slice is
[Spec 008](../specs/008-jj-inspection/spec.md), with plan, tasks, analysis and verification.
The roadmap separates implemented inspection from broader future operations.

Project-local Codex skills were installed with:

```sh
specify init --here --force --non-interactive --integration codex --integration-options='--skills' --script sh
```

Do not rerun `--force` without reviewing managed-file changes. Spec Kit's current feature
pointer is machine-local and ignored. In a fresh clone, set it using the normal Specify flow
or point `SPECIFY_FEATURE_DIRECTORY` at `specs/001-project-foundation` for the setup scripts.
No extension hooks are configured. `specify preset resolve spec-template` resolves the active template.

## Website
The published site is static HTML/CSS with local SVG/font assets and one local theme
script, generated with pinned Hugo. The Theme select follows the system by default or
saves Light/Dark locally when storage is available. Returning to System clears that
preference. Without JavaScript the switch is hidden and CSS follows system appearance.
Visitors need no packages, external assets or service connection.

`docs/index.html` remains the landing source. Public documentation and command reference
are generated through the controlled build below; a raw HTTP server over docs/ previews
only the authored landing, not the final generated routes.

A single GitHub Actions workflow checks every PR and every push to `main`. Pushes to other
branches do not start a run of their own: the `pull_request` run of the open PR covers them,
so a branch push is never checked twice. The workflow has no tag trigger, and nothing else
depends on branch push runs (the Pages job is main-only; Dependabot PRs use `pull_request`).
Go module and build caches come from `actions/setup-go` (keyed by `go.sum`). The conformance
lanes also cache the Git-from-source prefix with `actions/cache`, keyed by runner OS, arch,
lane, the pinned Git version and the installer hash; a hit skips the build and the
preflight still asserts the exact Git version. Caches written by a branch or PR are visible
only to that ref and its children, and never to other forks' runs (GitHub's default scoping).
CI keeps the heavy suite out of the `check` job so every PR finishes in reasonable time. The `check` job (static checks
and the plain Go test suite) always runs; `-race` never runs on a pull request or a push
(Spec 039), only in the scheduled race workflow. The two `Git/jj conformance` lanes live in the reusable
workflow `.github/workflows/conformance.yml`. `ci.yml` calls it for every push to `main` and every
pull request, and the shards run only for the tip of a stack (below): `check` never runs
`internal/workspace` (a plain run exceeds the 40 minute timeout), so a PR that skips the shards
leaves that package to the tip. Documentation-only PRs skip the shards too (below). The separate workflow
`ci-full.yml` runs it when the `ci-full` label is applied (a `labeled` event), so labelling
never re-runs `check`; it runs both lanes, so the `current` lane runs a second time (about 68
runner minutes) next to the run `ci.yml` already made. An unlabelled pull request runs only the `current` lane; a PR with the
`ci-full` label runs both lanes on every push (`ci.yml`) and when labelled (`ci-full.yml`), and
the `minimum` lane also runs on pushes to `main` (the post-merge gate) and in the scheduled race
workflow. The label also forces the shards on a PR below the tip; it is optional and mainly gets the `minimum` lane
early on the tip of a stack (otherwise `main` runs it after the merge). The
post-merge push to `main` runs everything again. If the `changes` job fails, `conformance`
is skipped and the required `suite` check does not report, so the merge is blocked (fail closed).

Heavy gate on the tip of a stack (owner decision 2026-10-26, "C yes", Spec 039 amendment). The shards run
for a push to `main`, for every PR whose base is the default branch, for the tip of a stack and for a PR with
the `ci-full` label. A PR is below the tip when its head branch is the base branch of another open,
non-Draft PR of this repository whose head is also a branch of this repository. A Draft child does not
count (authors push early as Draft), so a forgotten Draft child cannot remove the heavy gate. Such a PR runs only the light lane: `check`
(without `internal/workspace`) and the aggregate `suite`, which passes with the skipped shards, the same
mechanism as for a documentation-only PR, so both required checks (`check` and
`Git/jj conformance (current) / suite`) still report on every PR. The `changes` job decides this with
`scripts/ci_stack.py`: it lists the open PRs with `gh api` (the job alone has `pull-requests: read`) and
looks for a child. Branch names reach the script only through the environment. Any doubt means tip, so the
shards run: a fork PR, a failed or incomplete listing, a listing without the PR itself, the default
branch as head branch, a child from a fork. If the `changes` job itself fails, `conformance` is skipped
and `suite` does not report, so the merge is blocked.

When the decision is made: when a run starts, that is a new push (also one that merging a child into this
PR's branch makes), a close and reopen, or "Re-run all jobs" in the Actions UI (the lookup is repeated live;
re-running only the conformance job reuses the stale `changes` output). Opening, editing, retargeting or
closing a child, and a merge that does not move this PR's head, start no run of this one, and no other
trigger exists. So (1) a PR that was a tip keeps its earlier heavy run when a child appears later, and its
next push runs only the light lane. (2) When a child is closed without merging, the parent's earlier
light result stays: it has no heavy run for its head. Re-run all jobs or close and reopen it before merging
it. (3) Merging the bottom PR by rebase makes GitHub retarget its child to `main`; that edit starts no run.
The restack push that a rebase-merge needs does, and then the PR is on `main` or the tip and runs the shards.

Residual risk and the rule for owner and authors. The heavy run of the tip covers the commits below it only
when the tip contains the parent's CURRENT head: a push to the parent after the child exists runs only the
light lane, and the tip does not contain that commit until it is restacked. The tip's shards also run
against its own base (the parent branch), never against `main`, and the required `suite` of a PR below the
tip is green without shards, so GitHub does not enforce the rule. Therefore: before merging a PR below the
tip, the latest heavy run of the tip must be green on a head that contains the parent's current head
(restack the tip after every push below it); otherwise apply the `ci-full` label to the parent, which runs
the shards for it. After a web rebase-merge the rewritten SHAs on `main` meet the shards only in the
post-merge run on `main`: that run is the last gate. With `stw land --stack` (one fast-forward of the tip) the
tip's run covers the chain exactly.

Cost: one push to a tip or a PR on `main` runs about 78 runner minutes (`check` about 10, six `current`
shards about 68) in 7 concurrent jobs, wall time about 14 minutes. A PR below the tip runs `check` and `suite`
about 10 minutes in 2 concurrent jobs (`check` and `suite`; plus the small `changes` job). A stack of three
PRs therefore costs one heavy run plus two light ones per push cycle (about 98 runner minutes; 7 + 2 + 2 =
11 concurrent jobs, not counting `changes`) instead of three heavy ones (about 234 minutes, 21 jobs, over
the limit of 20 concurrent jobs). A newer push cancels the older run
of the same PR; batch fixes into one push.

Documentation-only pull requests skip the conformance shards. The `changes` job of `ci.yml`
runs `scripts/ci_changes.py` on the pull request's merge commit and outputs `docs_only=true`
only when the event is `pull_request` and every file differing from the base tip is on a
prose allowlist: root `*.md`, top-level `docs/*.md`, and `specs/<feature>/*.md` (including
`checklists/`). Everything else counts as code: workflows, scripts, source, schemas, go.mod/go.sum,
`docs/*.json`, `docs/assets`, `docs/index.html`, spec `contracts/` and `examples/` (Go tests and
scripts read them), and any rename out of those paths. An empty or unreadable change set, a
checkout that is not a merge commit and every other event (push to `main`, `workflow_dispatch`,
schedule, tags, the `ci-full` label run) give `false`. There is no `paths-ignore`, so every
workflow still starts and reports. `conformance.yml` receives `docs_only`; its `shard` job is
skipped only when `docs_only` is `true` or `stack_tip` is `false` on a `pull_request`, and the lane's `suite` job (always
runs) passes on `success`, or on `skipped` only for such a pull request (docs-only or below the tip of a stack); failed,
cancelled or skipped-otherwise fails it. `check` (static and documentation checks, the Go test
suite) and the docs, architecture and spec checks inside it always run.
T1 tests only what a pull request can affect (Spec 039). The `changes` job also runs
`scripts/ci_changes.py affected`, which prints `affected` (the changed Go packages plus every
package that imports them, test imports included, plus the packages declared in `scripts/ci-test-reads.json`
whose tests read or build another package's files without importing it) and `affected_reason` (`docs-only`, `packages` or
`all`). `check` waits for `changes` but still runs when it failed; it runs plain `go test` of the
affected packages, of every package when the reason is `all` or the output is empty, and of nothing for
`docs-only`, which also skips the sandbox install. Prose that a Go test reads (for example `README.md`, read by
`internal/releaseworkflow`) is declared in `scripts/ci-test-reads.json` and is not `docs-only`: its readers' tests run. Any changed path outside a Go package and outside
documentation prose (`go.mod`, `schemas/`, `scripts/`, `.github/`, spec contracts), a deleted package,
a non-merge checkout, a failing `go list` and every event other than `pull_request` give `all`.
Any error of the classifier gives the same `all` with exit status 0. `scripts/test_ci_affected.py` scans the Go tests and
fails when a test reads or builds another package's files, or reads a prose file, and has no entry of the right kind in that file;
root-level `.go` files select the tests that build `./cmd/stw`.
`ci_changes.py affected --files-from FILE` classifies a path list by path only. `check` always
leaves `internal/workspace` to the shards and `ci_shards.py packages --within` drops it from the
affected list.

Never require a per-shard check: a shard skipped inside a matrix reports under the
unexpanded matrix name and would stay pending. Require only checks that always report:
`check` and `Git/jj conformance (<lane>) / suite`. Widen the allowlist
only for files that nothing reads; `scripts/test_ci_workflow.py` pins the classifier, the
event rules and the `suite` truth table.

Check names of the conformance jobs are prefixed by the calling job: a lane reports as
`Git/jj conformance (<lane>) / suite` (lane `minimum` or `current`), from `ci.yml` and from
`ci-full.yml` alike. Classic branch protection is not used; the ruleset `main` (id 25683357)
requires exactly `check` and `Git/jj conformance (current) / suite`. Never require
`Git/jj conformance (minimum) / suite`: an unlabelled pull request does not run the `minimum`
lane, so the check would stay pending forever. The `minimum` lane runs on pushes to `main`, for
the `ci-full` label and in the scheduled race runs.
The cache key also hashes `scripts/backend-releases.json`.

The heavy `internal/workspace` package is sharded. `scripts/ci_shards.py` lists its top-level
tests, assigns them to four shards by greedy longest-processing-time using the committed
`scripts/ci-test-weights.json` (unlisted tests get the default weight, so a new test is always
assigned) and prints each shard's `-run` regex. In `conformance.yml`, jobs `shard 1` to
`shard 4` run one shard each with `-count=1 -timeout 40m`; `shard 0` runs every other
package and `shard 5` the real-CLI smoke checks
(its own job, so the smoke scripts no longer lengthen shard 0). The aggregate job `suite` needs the shards of its own
lane only, so the lane's check name stays stable. The `check` job runs
`python3 scripts/ci_shards.py check`, which fails if the shards miss a test, run one twice or
list none. Whenever `check` runs Go tests (not for
documentation-only PRs; with reason `packages` only the affected list), it leaves out
`internal/workspace`, which the shards run with jj (the `current` lane on pull requests, both
lanes on `main` and with `ci-full`). The conformance caller has no `if:` and `check` no tier switch, so the two
cannot drift apart. The weights are local
non-race seconds from a timed `go test -json` run; refresh them after the heavy tests change
(the fixture speed-up work will do that).

Race coverage is the scheduled tier T4 in `.github/workflows/race.yml` (Spec 039); pull requests
run no `-race`. It triggers only on `schedule` and `workflow_dispatch` and both jobs are guarded
by `if: github.ref == 'refs/heads/main'`, because a dispatch can name any ref. Nightly (03:17 UTC)
job `race` calls `conformance.yml` with `race: true` for the `current` lane (all packages and the
four `internal/workspace` shards under `-race`); weekly (Sunday 04:17 UTC) it does the same for the
`minimum` lane. A manual dispatch takes a `lane` input. The runs always happen, also on quiet days,
and are never cancelled (groups `t4-nightly` and `t4-weekly`). When `race` fails or is cancelled,
job `report` (the only job in the repository with `issues: write`; it checks out `main` only)
runs `scripts/race_report.py`: it comments on the open issue labelled `race-failure` (option
`--label`, default `race-failure`), or creates it if none is open, so at most one is open. The
script creates the label (fixed colour) only when a lookup returns 404 and leaves an existing label
untouched; it exits 1 when any `gh` issue, label or comment call fails, so the report job turns red. The body holds the run link, the tier (nightly or weekly),
the lane, the reason (failed or timed out: a timed-out job normally reports as failed, so the two
are not told apart, and its log may be missing; or cancelled) and at most 20 test
names matched from the failed job logs by `^--- FAIL: (Test\w+)`; no other log text is ever
copied. If the logs cannot be fetched or hold no match, the report still files with "No test name
matched, see the run." The `report` jobs of both schedules share the concurrency group
`race-report`, so a Sunday's nightly and weekly reports cannot both create an issue. Close the
issue after the fix lands; the fix is a normal pull request with a reproducing test.

Cost and residual risk of T4 (modelled from the conformance medians, not measured): about 54
runner minutes per `-race` lane run, so seven nightly runs plus one weekly run are about 430
runner minutes a week, off the pull request path. A data race is found after the merge, up to a
day later (a week for a race only the `minimum` lane shows), and only as an issue; Ready does not
imply a race-clean head. A race that no test exercises is found by neither run.

Its check job builds and
validates the artifact from the exact run SHA; mandatory minimum/current Git/jj conformance
lanes must also pass before deployment. The deployment job runs only for main
pushes after all required jobs succeed, consuming its identical uploaded artifact without
rebuilding or checking out another revision. Pages uses GitHub Actions as its build source.
There is no PR/manual-dispatch deployment path, CLI release or Git-tag publication.

## Generated public documentation

The site uses pinned Hugo standard 0.168.0, repository-owned templates and local assets.
Install the official binary locally with `python3 scripts/install-hugo.py` (network required,
SHA256 checked); it writes ignored `.tools/hugo` without installing system packages.
Then run `make pages` to generate and validate ignored `dist/pages/` from the current revision.
The destination must not already exist; remove only your previous build output before rebuilding.
Set `HUGO` to an explicitly installed Hugo 0.168.0 executable if preferred. Other runtime
versions are rejected. Linux amd64 and macOS universal official downloads are pinned;
other platforms can supply the same official version manually.

Public guides come from an explicit allowlist, while command reference is generated from
Cobra metadata without executing command callbacks. Hugo shortcodes are rejected in guide
sources, unsafe raw HTML is disabled, and templates cannot execute tools or fetch remote
resources. Internal specifications, progress and agent instructions are not included in the
public artifact. Published schemas retain source bytes. CI validates the exact artifact and
main-only Pages deploys that same artifact after checks pass; `revision.txt` records its SHA.

## Waiting for pull request CI

`scripts/ci-wait.sh` is an interim, read-only, bounded wait for the workflow runs of a pull
request head (it will be replaced by the `stw` hosting command specified for #93). It needs
`gh` and `jq`.

```sh
scripts/ci-wait.sh [-R OWNER/REPO] [--head SHA] [--timeout SECONDS] \
  [--appear-timeout SECONDS] [--interval SECONDS] [--json] PR_NUMBER
```

It resolves the PR head, waits until at least one run exists for it (default 120 s), then until
every run for that head is completed. Both phases share one deadline (default 2700 s) that is
computed once. Polls back off from `--interval` (default 20 s) to at most 60 s, and each sample
re-checks the head; `--head` is the expected head. It prints one `workflow | conclusion | duration`
line per run, up to ten failed test names for failed runs (from a filtered, size-bounded excerpt
of `gh run view --log-failed`, never raw logs) and a final `outcome=<name> head=<sha7> elapsed=<s>`
line; `--json` prints the same data as JSON.

| Exit | Outcome | Meaning |
| --- | --- | --- |
| 0 | `success` | all runs succeeded or were skipped (skipped is neutral and listed) |
| 1 | `failure` | a run failed, was cancelled, timed out or needs action |
| 2 | `deadline` | the deadline passed with runs still active |
| 3 | `no-runs` | no run appeared for the head in time |
| 4 | `head-changed` | the PR head differs from the expected head |
| 5 | `unavailable` | `gh` error, rate limit or authentication problem, runs that vanished after being seen, or a run list that is still full at the hard limit of 300 (`evidence incomplete`); never reads as green |
| 64 | | usage error |

The tool never re-runs, cancels or dispatches workflows and never merges or changes a PR. Tests
use a fake `gh` through `GH`, with `CI_WAIT_SLEEP` and `CI_WAIT_NOW` replacing `sleep` and the clock.

## Pre-push self-check

Run `python3 scripts/selfcheck.py [BASE]` inside the worktree before every push, including a push
of review fixes. `BASE` defaults to `refs/remotes/origin/main`; `--head REF` checks another commit.
The tool is read-only, uses only the Python standard library and is covered by
`scripts/test_selfcheck.py`, which `make check` runs with the other script tests.

| Exit | Meaning |
| --- | --- |
| 0 | no FAIL (INFO lines may still need your attention) |
| 1 | at least one FAIL |
| 2 | usage or environment error: not a git repository, unknown base or head ref, no merge base (for example a shallow clone: fetch more history), a failing git command |

It reads only the lines added by `BASE...HEAD` (file names and hunks are read with explicit git
options, so host diff settings, spaces and non-ASCII characters in paths do not change the result)
and fails on: lowercase words glued to numbers in Markdown (`hidden015planner`, `Exit0`; identifiers
such as CamelCase, snake_case, ALL_CAPS and base64-like tokens never match, JSON is not checked,
code spans, fenced blocks, link targets, reference definitions and URLs are ignored, and a code
span silences a false positive; hex tokens of seven or more characters, `sha256sum`, `base64url`
and similar are accepted), an added blank line right after another one in Markdown, more than one
`Source:` line outside fences in a `chartroom/notes/*.md` note, `git diff --check` errors, a
claude.ai link or `Claude-Session` trailer in a commit message, and an author or committer that
differs from the expected identity.

The expected identity is `--owner 'NAME <EMAIL>'` (the reliable form), or else the current git
identity (`user.name` and `user.email`, read only; the tool never writes git configuration). A git
identity is printed on every run as `INFO identity: owner identity taken from git config`, and it
FAILs when it looks like a placeholder (a name of two characters or fewer, or an email without a
dotted domain), so a leaked throwaway identity does not become the expectation. With neither, the
identity check is skipped with an `INFO identity` line. A containing workspace may also require a
`Co-Authored-By` trailer: `--require-trailer` fails any commit without one in its trailer block.

`INFO` lines never fail the run. `claims` lists each added doc line that names a `file:line`, an
exit status or a `--flag`; `tick` flags an "N/M done" statement that differs from the ticked
tasks in a changed `tasks.md`; `encoding` names a file that is not valid UTF-8. Verify every
`claims` and `tick` line against the code and tests yourself (the tool cannot) and say so in the
PR body.
