# Contract: tier decision, outputs and status names (Spec 039)

Normative text for [Spec 039](../spec.md). It describes what `scripts/ci_changes.py`, `scripts/ci_tier.py` and the workflows must
produce; nothing here exists yet.

## `scripts/ci_changes.py`

| Invocation | Output on stdout | Notes |
| --- | --- | --- |
| `ci_changes.py` (no argument, as today) | `docs_only=true|false` | byte for byte unchanged |
| `ci_changes.py affected` | `affected=<space separated import paths or ./...>`, `affected_reason=docs-only|packages|all` | fails closed to `all` |
| `ci_changes.py race-packages` | `race=<space separated import paths>` | derived from the sources, sorted, never hand-edited |

Exit status is 0 in every case that prints a result; an internal error prints the fail-closed value (`docs_only=false`,
`affected=./...`, `affected_reason=all`) and still exits 0, so a classifier error runs more tests, never fewer.

## `scripts/ci_tier.py`

Input: event name, action, draft flag, base ref, head ref, head SHA, default branch, labels, head repository equals base repository, the
list of open pull requests whose base is the head ref (from one API call), the T2 evidence for the head SHA (from one API call) and
`docs_only`. Output (job outputs and step summary):

| Output | Values |
| --- | --- |
| `t2_required` | `true`, `false` |
| `reason` | `docs-only`, `draft`, `label`, `unstacked`, `stack-tip`, `deferred-to-tip #N`, `ready`, `evidence from run <id>`, `main`, `schedule` |
| `stack_line` | the expected body line `T2 runs on the top PR #N` for a non-top PR, else empty |
| `stack_tip` | the number of the top PR that carries the evidence, or empty |
| `tier` | `T1`, `T2`, `T3` |

Truth table (a row applies in this order; the first match wins):

| # | Event | Condition | `t2_required` | `reason` |
| --- | --- | --- | --- | --- |
| 1 | push to the default branch, `schedule`, `workflow_dispatch` | always | `true` (T3) | `main` or `schedule` |
| 2 | pull_request | an API read failed | `true` | the failing source |
| 3 | pull_request | `docs_only` | `false` | `docs-only` |
| 4 | pull_request | green T2 evidence exists for the head SHA | `false` (evidence) | `evidence from run <id>` |
| 5 | pull_request | label `ci-full` present | `true` | `label` |
| 6 | pull_request | Draft | `false` | `draft` |
| 7 | pull_request | head repository differs from base repository | `true` | `unstacked` |
| 8 | pull_request | base is the default branch | `true` | `unstacked` |
| 9 | pull_request | an open PR has this head ref as base | `false` | `deferred-to-tip #N` |
| 10 | pull_request | otherwise (stacked, no child) | `true` | `stack-tip` |

`ready_for_review` is the same table: after a flip from Draft, row 6 no longer applies.

## Aggregate truth table (`suite`, per lane)

| `shard` result | Event | `t2_required` | `suite` |
| --- | --- | --- | --- |
| `success` | any | any | pass |
| `skipped` | pull_request | `false` (reason printed) | pass |
| `skipped` | pull_request, lane `minimum` | any | pass (reason `minimum lane runs on T3`) |
| `skipped` | pull_request | evidence reused | pass (reason `evidence from run <id>`) |
| `skipped` | pull_request | `true` | fail |
| `skipped` | push, schedule, dispatch | any | fail |
| `failure`, `cancelled` | any | any | fail |

## Status names (stable)

`check`, `Git/jj conformance (minimum) / suite` (T3 only; passes with a reason on a pull request), `Git/jj conformance (current) / suite`. Informational, never required: `tier`,
`changes`, every `shard N`, `pages`. The conformance caller job has no `if:` so the two `suite` names always report.
