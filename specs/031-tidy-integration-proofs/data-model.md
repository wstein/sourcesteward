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

`oid`, `source` (`discovered`, `hint`), `pr` (Spec 029 identity tuple or null), `refs`, `coverage` (`complete`, `incomplete`), `result` (`github-merged-head`, `no-merged-pr`, `discovery-incomplete`, `discovery-budget-exhausted`, `ambiguous-needs-hint`). The stored list is capped (256 records, 512 refs, 128 KiB) outside the aggregate bound; overflow is dropped with a `discovery-records-truncated` note ([contract](contracts/pr-discovery.md#stored-discovery-list)).

## ConfigNote (plan level)

`key_class` (closed set from the [configuration policy](contracts/configuration-policy.md)), `disposition` (`neutralized`, `review-reason`, `hard-stop`, `refused`, `retained`), `digest` (SHA-256 of the sanitized key/value records, no values stored beyond class), `apply_effect` (`none`, `refuses-apply`).

## RestorePlan and RestoreJournal

Unpublished v3 additions ([restore contract](contracts/restore.md)); the Spec 029 V3Journal is read, never modified.

| Field | Type | Rule |
| --- | --- | --- |
| `plan_id`, `action_id` | string | from the V3Journal entry (existing identifier limits) |
| `ref` | string | full `refs/heads/` name from the journal, or the `--as` name; validated ref name (FR-043) |
| `oid` | string | full OID from the journal, equal to the recovery ref target |
| `recovery_ref` | string | `refs/stw/recovery/v3/<plan>/<hash>` |
| `class`, `flags`, `decision` | as ProofEvidence | copied from the journal entry for display only |
| `state` | enum `restorable`, `already-present`, `name-taken`, `recovery-missing`, `journal-incomplete` | FR-042 |
| `reason` | closed enum or null | `invalid-ref-name`, `symbolic-reference`, `recovery-ref-absent`, `recovery-ref-retargeted` (target differs from the journal OID), `recovery-ref-symbolic`, `object-unreadable`, `no-confirmed-recovery`; plan-level refusal `unsupported-mode` (journal mode other than `local-refs`) and `journal-unreadable` |
| `kind` (journal record) | enum `intent`, `outcome` | write-ahead intent before the ref effect, outcome after |
| `outcome` (record) | enum `created`, `created-recovered`, `name-taken`, `symbolic-reference`, `recovery-missing`, `error` | an intent without outcome is an interrupted run |
| `by`, `at` (record) | string, timestamp | who and when, restore journal only |

The restore journal is a new append-only kind (`restore`) in its own namespace of the private store ([contract](contracts/restore.md#state-and-journal)). The intent is persisted before the ref effect and the outcome after; neither rewrites an earlier record. A rerun that finds the ref at the journal OID appends `created-recovered` when an intent has no outcome and writes nothing otherwise. Invariants: `restorable` requires a direct recovery ref whose target equals the journal OID, a readable commit and a free (absent) name; every other state writes no record and no effect; the V3Journal bytes are unchanged by any restore.

## HistoryEntry

`plan_id`, `time`, `mode`, `deleted`, `retained`, `restorable` (integers), `recovery_refs` (`present`, `partial`, `absent`), `decision_kinds` (counts of `human` and `ai`), `journal_status` (`complete`, `incomplete`, `unreadable`), newest first, with a truncation marker `{unit, shown, total}` (FR-047). A journal of mode other than `local-refs` shows its `mode` and `restorable` 0.

## Plan binding

The plan identity includes proof policy version, the selected proof kinds, `--accept-whitespace-differences`, the decision file digest, the discovery coverage and the neutralized configuration digest.
