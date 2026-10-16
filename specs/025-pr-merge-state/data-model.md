# Data model: merge-state observation v1

Types live in `internal/hosting` beside the Spec 020 types and reuse the native `inspection.Result` and diagnostic types. JSON names are snake_case. Arrays are non-null even when empty; optional object values are `null`. All text fields are valid UTF-8, bounded as stated, and are data. The normative tables for item codes, confidence, reasons, source ids, outcomes, statuses and exit statuses are in [spec.md](spec.md); this file adds field shapes and invariants and does not repeat those tables, so the two cannot drift.

## MergeStateResult (`schema_version: 1`, `kind: "merge-state"`)

| Field | Type | Notes |
| --- | --- | --- |
| `schema_version` | integer | constant 1 |
| `kind` | string | constant `merge-state` |
| `status` | `ok` \| `incomplete` \| `error` | spec status table |
| `backend` | `git` \| `jj` \| `gitbutler` \| `ambiguous` \| `unknown` | as the hosting result |
| `identity` | native identity or `null` | the Spec 020 shapes, copied into this schema (see Schema) |
| `repository` | Repository or `null` | host, owner, name, selection as in Spec 020 |
| `candidates` | Candidate[] | as in Spec 020; provenance of remote selection |
| `pull_request` | PullRequestState or `null` | null for `error` before the first read |
| `github_reported` | GitHubReported or `null` | GitHub's own summary, never a conclusion |
| `base_branch` | BaseBranch or `null` | |
| `verdict` | string | constant `not-asserted` in every result |
| `notice` | string | constant: "Sampled evidence; it does not assert that the pull request can be merged or will merge, and it authorizes nothing." |
| `items` | Item[] | at most 300; order below |
| `sources` | Source[] | one entry per source id of the Sources table, in table order, including `skipped` |
| `summary` | Summary | |
| `diagnostics` | `{code, message}`[] | empty for `ok`; codes from the status table; messages fixed text, no response data |
| `boundary` | Boundary | |
| `observed_at` | RFC 3339 UTC | time of the final sample |

## PullRequestState

`number` (positive integer), `state` (`open` or `closed`), `merged` (boolean), `draft` (boolean), `head_sha` and `base_sha` (40-hex), `head_ref` and `base_ref` (bounded text), `base_repository` (`owner/name`, validated like Spec 020) and `head_repository` (the same, or `null` when GitHub reports no head repository), `auto_merge` (`{method: merge|squash|rebase|null}` or `null`; an unrecognized method string gives `method` `null`, never a read failure). Titles, URLs and logins are deliberately not carried.

## GitHubReported

`mergeable` (boolean or `null`), `mergeable_state` (string of at most 32 characters, escaped when rendered), `computed` (boolean, false when `mergeable` is null). Taken from the first pull request read. Labelled in human output as "GitHub reports". Used by the Items table only where it names these fields.

## BaseBranch

`ref` (validated or bounded text), `ref_supported` (boolean), `tip_sha` (40-hex or `null`), `protected` (boolean or `null` when the branch read did not succeed).

## Item

| Field | Type | Notes |
| --- | --- | --- |
| `code` | enum | the code column of the Items table |
| `subject` | string or `null` | context name, rule type or method; at most 256 bytes |
| `effect` | `waits` \| `may-wait` \| `info` | |
| `confidence` | `observed` \| `inferred` \| `unknown` | |
| `reason` | reason enum or `null` | non-null if and only if `confidence` is `unknown`; also non-null for no other confidence |
| `summary` | string | the fixed template of the code (below) with escaped parameters, at most 512 bytes; no other text |
| `detail` | object | keys per the table below; no other keys |
| `evidence` | `{source, fields[]}`[] | one to eight entries; `source` is a source id, `fields` the field names read from it |

Order: the order of the Items table, then `subject` by bytes, then `detail.app_id` (null first). `waits` is never `unknown`; `may-wait` is `observed` or `unknown`; `info` may have any confidence.

### Detail keys per code

| Code | `detail` keys |
| --- | --- |
| `pull-request-closed` | `merged` (boolean) |
| `draft`, `merge-conflict`, `mergeability-not-computed`, `required-checks-unknown`, `blocked-unexplained` | none (empty object) |
| `required-check` | `state` (`pending`, `failing`, `missing`, `successful`, `neutral-or-skipped`, `unreadable`), `origin` (array of `branch-protection`, `base-branch`, `branch-rules`), `app_id` (integer or `null`), `matches` (integer: number of matching records) |
| `strict-required-status-checks`, `linear-history-required`, `conversation-resolution-required` | `origin` (array as above) |
| `head-behind-base` | `behind_by`, `ahead_by` (non-negative integers, or `null` when `compare` was not `ok`), `strict` (boolean or `null`) |
| `auto-merge` | `method` (`merge`, `squash`, `rebase` or `null`) |
| `merge-methods` | `allowed` (array of methods, or `null` when unknown), `rule_limit` (array of methods from a rule; empty when `branch-rules` is `ok` and sets no limit; `null` only when `branch-rules` is not `ok`), `sources` (array of source ids that contributed) |
| `auto-merge-method-not-allowed` | `method`, `excluded_by` (source id) |
| `administrators-included` | `enabled` (boolean) |
| `classic-protection-unreadable` | `unreadable` (array of `strict`, `enforce_admins`, `required_reviews`, `conversation_resolution`, `linear_history`, `signatures`, `lock_branch`) |
| `review-required` | `required` (known lower bound or value; `null` when no bound is known), `required_complete` (boolean), `approvals`, `changes_requested` (non-negative integers, or `null` when `reviews` was not `ok`), `origin` |
| `merge-queue-required` | `merge_method` (string or `null`) |
| `rulesets-apply` | `rulesets` (at most 50 of `{id, source_type, source, rule_types[]}`; `id` integer, others bounded text) |
| `rule-not-evaluated` | `rule_type` (bounded text), `origin` |
| `branch-rules-unreadable` | `unreadable` (array of `required_checks`, `strict`, `reviews`, `linear_history`, `merge_queue`, `conversation_resolution`, `merge_methods`, `other_rules`) |

### Summary templates (normative; parameters in braces are escaped data)

| Code | Template |
| --- | --- |
| `pull-request-closed` | The pull request is {closed or merged}. |
| `draft` | The pull request is a draft. |
| `merge-conflict` | GitHub reports that the head conflicts with the base. |
| `mergeability-not-computed` | GitHub has not computed mergeability yet. |
| `required-check` | Required status context {subject}: {state}. |
| `required-checks-unknown` | The set of required status contexts is not fully known. |
| `strict-required-status-checks` | A rule requires the head to be up to date with the base. |
| `head-behind-base` | The head is {behind_by} commits behind the base branch tip; strict rule: {strict}. (when `behind_by` is null: Whether the head is behind the base branch tip is not known; strict rule: {strict}.) |
| `auto-merge` | Auto-merge is enabled with method {method}. |
| `merge-methods` | Merge methods not excluded by readable sources: {allowed}; rule limit: {rule_limit}. |
| `auto-merge-method-not-allowed` | Auto-merge method {method} is excluded by {excluded_by}. |
| `linear-history-required` | A rule requires linear history. |
| `administrators-included` | Rules apply to administrators: {enabled}. |
| `classic-protection-unreadable` | Classic branch protection may apply but its details are not readable: {unreadable}. |
| `review-required` | Reviews required: {required}{ or more}; approvals counted: {approvals}; changes requested: {changes_requested}. Reviewer eligibility is not evaluated. |
| `conversation-resolution-required` | A rule requires resolved conversations; REST does not expose their state. |
| `merge-queue-required` | A merge queue rule applies; queue state is not read. |
| `rulesets-apply` | {n} rulesets apply to the base branch. |
| `rule-not-evaluated` | Rule {subject} applies and is not evaluated by this command. |
| `branch-rules-unreadable` | Branch rules are not readable: {unreadable}. |
| `blocked-unexplained` | GitHub reports a blocking state that the readable evidence does not explain. |

A null parameter renders as `unknown`, in templates and in the fixed lines (a null `mergeable` prints `mergeable=unknown`), and an empty array renders as `none`; " or more" is added only when `required_complete` is false. An `unknown` item appends ` Reason: {reason}.` with the reason code. Human output consists only of these summaries and the **fixed lines** below.

### Fixed human lines (normative)

| Line | Form |
| --- | --- |
| Header | `Merge state: #{number} {owner}/{name} ({state}, {draft or not a draft})` |
| GitHub line | `GitHub reports: mergeable={mergeable} mergeable_state={mergeable_state} (GitHub's summary, not a conclusion)` |
| Group headings | `Waiting on:`, `May be waiting on:`, `Context:`; an empty group prints `  (none)` |
| Item | `  [{confidence}] {code}{ "subject"}: {summary} (sources: {source ids})` |
| No waiting item | `No waiting condition was found in the evidence read.` |
| Sources | `Sources: {id} {outcome}[ ({reason})]; …` |
| Completeness | `Complete: {yes or no}. Network reads: {requests_made} of at most {max_requests}.` |
| Notice | `Notice: ` followed by the constant `notice` |
| Error result | Header and GitHub lines are omitted when `pull_request` is null; the lines printed are the first diagnostic as `Error: {diagnostic code}: {message}` (on standard error as for the other hosting leaves, and also on standard output only when `--json` is absent so that the text form is self-contained; JSON mode prints the JSON object on standard output and the `Error:` line on standard error), the Sources line, the Completeness line and the Notice |

## Source

`id` (source id), `outcome` (`ok`, `denied`, `not-found`, `rate-limited`, `failed`, `skipped`), `failure_class` (`unauthorized`, `forbidden`, `not-found`, `rate-limited`, `other-http`, `too-large`, `malformed`, `transport`, `canceled`, or `null`), `requests` (integer, requests made for this source), `reason` (reason enum or `null`; non-null for `skipped`, `denied`, `not-found`, `rate-limited` and `failed` outcomes (`failed` uses `source-failed`, a dependency skip uses `dependency-unavailable`), `null` for `ok`). Outcome mapping from the failure class: `unauthorized` and `forbidden` give `denied`; `not-found` gives `not-found`; `rate-limited` gives `rate-limited`; every other class except `canceled` gives `failed`; `canceled` is never a source outcome, it ends the operation as `error` with `hosting-canceled`.

**Two-read sources (rule)**: when `pull-request` or `base-branch` is read twice and the first read is `ok` but the resample fails with any class except `canceled`, the outcome becomes `failed`, the reason `source-failed`, the class is kept and the status is `incomplete`. The `base-branch` resample is not requested when its first read was not `ok`.

## Summary

`{items, waits, may_wait, info, observed, inferred, unknown}`, all non-negative integers; `waits + may_wait + info = items` and `observed + inferred + unknown = items`.

## Boundary

`complete` (boolean), `network` (true once the first read was attempted, as in Spec 020), `requests_made` (integer), `max_requests` (const 50), `max_pages` (const 10), `max_records` (const 1000), `max_bytes` (const 1048576), `timeout_seconds` (const 30), `max_items` (const 300), `limitations` (string array including the fixed statements that mergeability is not asserted, that evidence is sampled, that bypass actors, review-thread resolution, merge queue state and reviewer eligibility are not read, and that reading a pull request can cause GitHub to compute its mergeability).

## State and completeness rules

- `status: "ok"` requires `boundary.complete: true`: every source has an outcome in `ok`, `denied`, `not-found` or `skipped`, the resample matched and no bound was exhausted. `denied` and `not-found` outcomes keep the result complete and produce `unknown` items.
- `status: "incomplete"` carries at least one of `merge-state-changed`, `merge-state-limit`, `merge-state-source-failed`; items derived before the problem are kept; an item that depends on a `failed` or `rate-limited` source is `unknown` with reason `source-failed` or `rate-limited`.
- `status: "error"` has `items: []` and `boundary.complete: false`. This is the normative rule for the null fields: `pull_request`, `github_reported` and `base_branch` are `null` unless the first pull request read succeeded and a later failure (cancellation, deadline, output limit) ended the operation, in which case the values already read are kept.
- Every item with confidence `observed` or `inferred` has at least one evidence entry whose source outcome is `ok`. No item is ever derived from a `denied`, `not-found` or `failed` outcome except as `unknown`; absence is never produced from such an outcome.
- A `closed` pull request has the single item `pull-request-closed` and every other source `skipped` with reason `skipped-not-open`, except `pull-request` and its resample.
- `verdict` and `notice` are constants in every status.

## Schema

`schemas/v1/merge-state.schema.json` is self-contained (internal references only, as the other published schemas): the native identity, repository and candidate definitions are copies of those in `hosting.schema.json`; a contract test asserts they stay equal to the hosting definitions so they cannot drift. Item `detail` is validated per `code` with `if/then`; the enum is closed for the published revision, codes are only ever added by a later revision of the schema, and consumers must tolerate codes they do not know. `hosting.schema.json` is not edited.
