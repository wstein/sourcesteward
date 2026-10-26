# Contract: tier decision, outputs and status names (Spec 039)

Normative text for [Spec 039](../spec.md). It describes what `scripts/ci_changes.py`, `scripts/ci_tier.py` and the workflows must
produce; nothing here exists yet.

## `scripts/ci_changes.py`

| Invocation | Output on stdout | Notes |
| --- | --- | --- |
| `ci_changes.py` (no argument, as today) | `docs_only=true|false` | byte for byte unchanged |
| `ci_changes.py affected` | `affected=<space separated import paths or ./...>`, `affected_reason=docs-only|packages|all` | fails closed to `all` |

Exit status is 0 in every case that prints a result; an internal error prints the fail-closed value (`docs_only=false`,
`affected=./...`, `affected_reason=all`) and still exits 0, so a classifier error runs more tests, never fewer.

## `scripts/ci_tier.py`

Runs in the `tier` job from the **base-ref checkout** (a guard against script-level self-classification only; the workflow YAML itself comes from the PR, see Spec 039 "Security"). Input: event name, action, draft flag, base ref, head ref, head SHA,
default branch, labels, head repository equals base repository, the open pull requests whose base is the head ref **filtered to a head
repository equal to the base repository**, the PR file list from the API, own T2 evidence for the head SHA, and tip evidence (compare
ancestry plus filtered T2 evidence) for each descendant tip. Evidence is accepted only from a run whose workflow path is
`.github/workflows/ci.yml` or `ci-full.yml`, event `pull_request`, head repository equal to the base repository, created by the GitHub
Actions app, in which **all six `current` shard jobs concluded `success`** (executed, not skipped). A `suite` job is never evidence by itself: it also passes with skipped shards (draft, docs-only, reused evidence, `minimum` lane), and a reused-evidence run must not be chained.

| Output | Values |
| --- | --- |
| `t2_required` | `true`, `false` |
| `reason` | `docs-only`, `draft`, `label`, `unstacked`, `stack-tip`, `deferred-to-tip #N`, `T2 pending on #N`, `evidence from run <id>`, `evidence from tip #N run <id>`, `main`, `schedule` |
| `stack_line` | the expected informational body line `T2 runs on the top PR #N` for a non-top PR, else empty (never a gate) |
| `stack_tip` | the number of the tip that carries the evidence, or empty |
| `affected_reason` | `docs-only`, `packages`, `all`; `all` for any changed path under `.github/` or `scripts/`, which `check` honours |
| `tier` | `T1`, `T2`, `T3` |

Truth table (a row applies in this order; the first match wins; the child row precedes the default-branch row):

| # | Event | Condition | `t2_required` | `reason` |
| --- | --- | --- | --- | --- |
| 1 | push to the default branch (`race.yml` never runs the `tier` job: it is a separate workflow without this table) | always | `true` (T3: plain matrix, both lanes, no `-race`) | `main` |
| 2 | pull_request | an API read failed | `true` | the failing source |
| 3 | pull_request | `docs_only` | `false` | `docs-only` |
| 4 | pull_request | filtered T2 evidence exists for the head SHA | `false` (evidence) | `evidence from run <id>` |
| 5 | pull_request | label `ci-full` present | `true` | `label` |
| 6 | pull_request | Draft | `false` | `draft` (WP-2 target; until WP-2 every PR runs the shards, amendment 2026-10-26) |
| 7 | pull_request | head repository differs from base repository (fork) | `true` | `unstacked` |
| 8 | pull_request | an open same-repo PR has this head ref as base, and tip evidence exists | `false` (evidence) | `evidence from tip #N run <id>` |
| 9 | pull_request | an open same-repo PR has this head ref as base, no tip evidence | `false`, `suite` fails | `T2 pending on #N` (`deferred-to-tip #N` in the summary; WP-2 target, see the amendment of 2026-10-26: T1 cannot run `internal/workspace` plain) |
| 10 | pull_request | base is the default branch | `true` | `unstacked` |
| 11 | pull_request | otherwise (stacked, no same-repo child) | `true` | `stack-tip` |

`ready_for_review` is not a row: after a flip from Draft, row 6 no longer applies and the PR reaches rows 8 to 11. A fork PR that
targets a branch of the base repository is never counted as a child in rows 8 and 9 (mutation target and test row).

## Aggregate truth table (`suite`, per lane)

| `shard` result | Event | `t2_required` | `suite` |
| --- | --- | --- | --- |
| `success` | any | any | pass |
| `skipped` | pull_request | `false` (reason printed) | pass |
| `skipped` | pull_request, lane `minimum` | any | pass (reason `minimum lane runs on T3`) |
| `skipped` | pull_request | own or tip evidence (rows 4 and 8) | pass (reason with the run id) |
| `skipped` | pull_request | `T2 pending on #N` (row 9) | fail |
| `skipped` | pull_request | `true` | fail |
| `skipped` | push, schedule, dispatch | any | fail |
| `failure`, `cancelled` | any | any | fail |

## Status names (stable)

`check`, `Git/jj conformance (minimum) / suite` (T3 only; passes with a reason on a pull request), `Git/jj conformance (current) / suite`. Informational, never required: `tier`,
`changes`, every `shard N`, `pages`. The conformance caller job has no `if:` so the two `suite` names always report.

## `ci_changes.py --files-from`

`ci_changes.py affected --files-from FILE` classifies a newline separated path list (the PR file list from the API) by path only. Output:
`affected_reason=docs-only|packages|all`. Any path under `.github/` or `scripts/` gives `all`; so does any non-package, non-prose path, an
empty list and an unreadable file.

## `race.yml` and the `race-failure` issue

Triggers: `schedule` (nightly and weekly crons) and `workflow_dispatch`; no `pull_request` trigger. **Both jobs have `if: github.ref == 'refs/heads/main'`** (a dispatch can name any ref); a workflow test pins it. Job `race`
runs the conformance shards with `race: true` (nightly: all packages, lane `current`; weekly: lane `minimum`). Job `report`
(`needs: race`, `if: failure() || cancelled()`): `permissions: contents: read, actions: read, issues: write`; checks out the default branch only; finds an
open issue with the label `race-failure` (comment on it) or creates one (so at most one is open); the body holds the run link, the tier,
the lane, the reason (failed, including a job timeout, or cancelled) and at most 20 test names matched by `^--- FAIL: (Test\w+)`; the log text itself is never copied.
