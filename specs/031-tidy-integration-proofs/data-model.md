# Data model additions (unpublished v3 result and plan)

All additions are optional-additive within the unpublished v3 schema defined by Spec 029; v1/v2 schemas are untouched.

## ProofEvidence (per distinct tip OID)

| Field | Type | Rule |
| --- | --- | --- |
| `kind` | enum `ancestor`, `patch-id`, `merge-noop`, `github-merged-head` | FR-001 |
| `strength` | enum `ancestry`, `content-present`, `history-equivalent`, `forge-attested` | derived from kind |
| `classification` | enum `proven`, `partial`, `would-add-content`, `conflict`, `not-evaluated` | `proven` only per FR-002..004 |
| `base_selector`, `base_oid`, `tip_oid` | string | full OIDs |
| `matched`, `unmatched`, `skipped_merges` | integer >= 0 | `patch-id` only |
| `not_evaluated_reason` | enum `proof-limit`, `proof-budget-exhausted`, `unsafe-configuration`, `object-dir-unavailable`, `unselected` | present iff `not-evaluated` |
| `refs` | array of full ref names | every selected ref with this tip |

Invariants: `matched + unmatched` equals the evaluated non-merge branch-side commits; `proven` requires `unmatched = 0` and `matched >= 1`; the headline kind is the strongest `proven` entry; a ref in a protected class carries evidence but its disposition stays retained.

## DiscoveryRecord (online)

`oid`, `source` (`discovered`, `hint`), `pr` (Spec 029 identity tuple or null), `refs`, `coverage` (`complete`, `truncated`), `result` (`github-merged-head`, `no-merged-pr`, `discovery-truncated`, `ambiguous-needs-hint`).

## ConfigNote (plan level)

`key_class` (closed set from the [configuration policy](contracts/configuration-policy.md)), `disposition` (`neutralized`, `refused`), `digest` (SHA-256 of the sanitized key/value records, no values stored beyond class), `apply_effect` (`none`, `refuses-apply`).

## Plan binding

The plan identity includes proof policy version, the selected proof kinds, the discovery coverage and the neutralized configuration digest.
