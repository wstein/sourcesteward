# Data model additions (unpublished v3 result and plan)

All additions are optional-additive within the unpublished v3 schema defined by Spec 029; v1/v2 schemas are untouched.

## ProofEvidence (per distinct tip OID)

| Field | Type | Rule |
| --- | --- | --- |
| `kind` | enum `ancestor`, `patch-exact`, `merge-noop`, `patch-id`, `no-net-change`, `github-merged-head` | FR-001 |
| `strength` | enum `ancestry`, `content-present`, `exact-patch-history`, `history-equivalent`, `modulo-whitespace`, `nothing-unmerged`, `forge-attested` | derived from kind |
| `classification` | enum `proven`, `partial`, `would-add-content`, `conflict`, `no-net-change`, `whitespace-significant-difference`, `not-evaluated` | `proven` only per FR-002..004 |
| `base_selector`, `base_oid`, `tip_oid` | string | full OIDs |
| `matched`, `unmatched`, `empty`, `skipped_merges` | integer >= 0 | patch kinds only |
| `subclass` | enum `integrated-modulo-whitespace` or null | `patch-id` with verbatim mismatch |
| `whitespace_significant_paths` | array of at most 16 paths | present iff `whitespace-significant-difference` |
| `not_evaluated_reason` | enum `proof-limit`, `proof-budget-exhausted`, `git-unsafe-configuration`, `object-dir-unavailable`, `unsupported-metadata`, `merge-attributes-present`, `history-override`, `ambiguous-merge-base`, `gitlink-change` | present iff `not-evaluated` |
| `refs` | array of full ref names | every selected ref with this tip |

Invariants: `matched + unmatched + empty` equals the evaluated non-merge branch-side commits; `proven` for `patch-exact` and `patch-id` requires `unmatched = 0` and `matched >= 1`; `no-net-change` is proven only with equal trees and is never reported as integrated; the journal entry stores branch, tip OID, class, strength and subclass; the headline kind is the strongest `proven` entry; a ref in a protected class carries evidence but its disposition stays retained.

## DiscoveryRecord (online)

`oid`, `source` (`discovered`, `hint`), `pr` (Spec 029 identity tuple or null), `refs`, `coverage` (`complete`, `truncated`), `result` (`github-merged-head`, `no-merged-pr`, `discovery-truncated`, `discovery-budget-exhausted`, `ambiguous-needs-hint`).

## ConfigNote (plan level)

`key_class` (closed set from the [configuration policy](contracts/configuration-policy.md)), `disposition` (`neutralized`, `proof-not-evaluated`, `refused`, `retained`), `digest` (SHA-256 of the sanitized key/value records, no values stored beyond class), `apply_effect` (`none`, `refuses-apply`).

## Plan binding

The plan identity includes proof policy version, the selected proof kinds, the discovery coverage and the neutralized configuration digest.
