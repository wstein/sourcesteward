# Data model additions (unpublished v3 result and plan)

All additions are additive within the unpublished v3 schema defined by Spec 029; v1/v2 schemas are untouched.

## ProofEvidence (per distinct tip OID)

| Field | Type | Rule |
| --- | --- | --- |
| `class` | enum `ancestor`, `patch-id`, `merge-noop`, `no-net-change`, `github-merged-head`, `reviewed-integrated`, `reviewed-drop` | FR-001 |
| `strength` | enum `ancestry`, `history-equivalent`, `content-present`, `nothing-unmerged`, `forge-attested`, `reviewer-decision` | derived from class |
| `disposition` | enum `proven`, `needs-review`, `retained` | FR-001, FR-031 |
| `reasons` | array of the closed reason enum of the [reason table](contracts/integration-proofs.md#review-reasons) | non-empty iff `needs-review` or `retained` |
| `flags` | array, `whitespace-differs` or `accepted-whitespace-differences` | patch-id |
| `base_selector`, `base_oid`, `tip_oid` | string | full OIDs |
| `matched`, `unmatched`, `empty`, `skipped_merges` | integer >= 0 | patch-id only; `matched + unmatched + empty` equals evaluated non-merge branch-side commits (multiset counts) |
| `refs` | array of full ref names | every selected ref with this tip |
| `decision` | Decision or null | FR-040 |

Invariants: `proven` for `patch-id` requires `unmatched = 0`, `matched >= 1`, `skipped_merges = 0` and no reasons (a `whitespace-differs` flag is deletable only with `--accept-whitespace-differences` or a decision); `no-net-change` is never reported as integrated; a ref in a protected class or with a hard stop is `retained` with evidence; the journal entry stores branch, tip OID, class, flags and, for decisions, reviewer, kind, decision and reason.

## ReviewSummary (plan result)

Counts per disposition and per reason; `report_written` boolean; `entries`, `omitted`.

## Report and ReportEntry

As defined in the [review report contract](contracts/review-report.md): `schema_version`, `reviewer_guidance`, `summary`, `base`, `entries[]` with `branch`, `tip_oid`, `base_oid`, `merge_base_oid`, `reasons`, `commits`, `proofs_tried`, `unintegrated_diff`, `candidate_base_commits`, `range_pairing`, `suggested_verdict`, `questions`, `allowed_decisions`. Per-path `files[]` carries mode, type change and kind (FR-003c). Truncation markers are `{unit, shown, total}`. Candidate JSON schema validated by tests, unpublished.

## Decision

`branch`, `tip_oid`, `base_oid`, `decision` (`integrated`, `keep`, `drop`), `reason`, `reviewer`, `kind` (`human`, `ai`); bounds in the [decisions contract](contracts/decisions.md). Recorded in the plan and journal with `applied` (boolean) and the note (`stale-decision`, `decision-not-applicable`) when ignored.

## DiscoveryRecord (online)

`oid`, `source` (`discovered`, `hint`), `pr` (Spec 029 identity tuple or null), `refs`, `coverage` (`complete`, `incomplete`), `result` (`github-merged-head`, `no-merged-pr`, `discovery-incomplete`, `discovery-budget-exhausted`, `ambiguous-needs-hint`).

## ConfigNote (plan level)

`key_class` (closed set from the [configuration policy](contracts/configuration-policy.md)), `disposition` (`neutralized`, `review-reason`, `hard-stop`, `refused`, `retained`), `digest` (SHA-256 of the sanitized key/value records, no values stored beyond class), `apply_effect` (`none`, `refuses-apply`).

## Plan binding

The plan identity includes proof policy version, the selected proof kinds, `--accept-whitespace-differences`, the decision file digest, the discovery coverage and the neutralized configuration digest.
