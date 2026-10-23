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
CI is tiered so stacked PRs do not repeat the heavy suite. The `check` job (static checks
and the plain Go test suite) always runs; `-race` never runs on a pull request or a push
(Spec 039), only in the scheduled race workflow. The two `Git/jj conformance` lanes live in the reusable
workflow `.github/workflows/conformance.yml`. `ci.yml` calls it for pushes to `main`, for PRs
whose base is the default branch, and for any PR that carries the `ci-full` label when it is
opened or pushed; a PR stacked on another PR branch skips it otherwise. The separate workflow
`ci-full.yml` runs it when the `ci-full` label is applied (a `labeled` event), so labelling
never re-runs `check`. The owner or dispatcher puts `ci-full` on the top PR of a stack. A
skipped job reports a neutral result, so it does not block merging; the stack tip with
`ci-full` carries the evidence for the whole stack, and the post-merge push to `main` runs
everything again. When GitHub retargets a stacked PR to `main` after its parent merged, the
workflow does not react (no `edited` trigger) and the conformance job stays skipped: the
dispatcher restacks (a push) or applies `ci-full`.

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
skipped only when `docs_only` is `true` on a `pull_request`, and the lane's `suite` job (always
runs) passes on `success`, or on `skipped` only for that same docs-only pull request; failed,
cancelled or skipped-otherwise fails it. `check` (static and documentation checks, the Go test
suite) and the docs, architecture and spec checks inside it always run.
Never require a per-shard check: a shard skipped inside a matrix reports under the
unexpanded matrix name and would stay pending. Require only checks that always report:
`check` and `Git/jj conformance (<lane>) / suite`. Widen the allowlist
only for files that nothing reads; `scripts/test_ci_workflow.py` pins the classifier, the
event rules and the `suite` truth table.

Check names of the conformance jobs are prefixed by the calling job: a lane reports as
`Git/jj conformance (<lane>) / suite` (lane `minimum` or `current`), from `ci.yml` and from
`ci-full.yml` alike. No branch protection exists today; a future required check should
reference `Git/jj conformance (minimum) / suite` and `Git/jj conformance (current) / suite`.
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
list none. When the conformance tier applies to the event (the same expression as the
conformance `if:`, held once in the workflow-level `CONFORMANCE_TIER` value), `check` runs
`go test` on every package except `internal/workspace`, which the shards cover on
both lanes with jj; otherwise it runs `./...`, so nothing is skipped. The weights are local
non-race seconds from a timed `go test -json` run; refresh them after the heavy tests change
(the fixture speed-up work will do that).

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
