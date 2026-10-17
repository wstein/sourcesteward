# Cross-artifact analysis: Stack inspection and plan (v2)

**Status**: Round 1 resolved in the artifacts; round 2 pending. Round 0 is the author draft (a28d23e).

Rounds are recorded below as: reviewer, reviewed revision, verdict (`READY` or `NOT READY`), a table of findings with severity, artifact and section, resolution, and a marker for any earlier resolution that a later round supersedes. The coverage map of [tasks.md](tasks.md) is the starting point for the requirement and success-criterion coverage check.

## Questions for the first independent round

1. Does every Findings table code have a rule that can be decided from the data model fields, on both backends, without a read that is not on the process allowlist?
2. Are the plan rules complete for the three merge shapes, the native and plain chain, and a layer with several reference names?
3. Do the commit-count, subject and result bounds fit the scan allowance of the shared runner?
4. Is the Linkage rule consistent with Spec 016 FR-001 (no linkage by names alone)?
5. Which statements about GitHub behavior are claimed as measured but rest on the closed spike stack only?

## Rounds

### Round 1

Reviewer: independent reviewer (coordinator relay). Reviewed revision: a28d23e. Verdict: NOT READY (2 high, 6 medium, 8 low). The reviewer re-measured the Git claims and the GitHub measurements M3, M4 and M5; they hold.

| Id | Sev | Finding | Resolution |
| --- | --- | --- | --- |
| H1 | High | Offline plan prints a rebase chain that races with GitHub's rewrite of a native stack; jj `stack-behind-base` row contradicted US2-2 | Plan rules groups: an upstream `behind`/`diverged` gives fetch and observe; every offline plan carries `native_checked: false` and the boundary limitation; US2-2 clarified (a moved base needs no PR evidence, an applied or merged parent does) |
| H2 | High | Plan rules not deterministic | Dropped set defined per kind (`rebase`, `rewritten`, `merge`), non-prefix blocks (`merged-layer-not-bottom`), merge kind with layers still in range gives a `stale-base` observe step, ordered groups with precedence; D9 table updated |
| M1 | Med | Native rebase gives relation `diverged`, not `remote-ahead` | US3-4, the finding rule and D9 changed; `unknown` before fetch |
| M2 | Med | Full page of five undefined | Linkage rule: ambiguous with `truncated`, never missing, `incomplete` with `stack-limit`; LIM-4 paging limits do not apply to the fixed shapes |
| M3 | Med | `expected` lacked the base commit id | `new_parent_commit_id` on the first rebase step (null later); data model, contract example and spec updated |
| M4 | Med | 016 US1 overlap; L2 versus 016 FR-006 | 016 amendment re-scopes US1 and notes FR-006 for L2 and L5; later-effects and plan updated |
| M5 | Med | jj names as revset literals; `-d` versus `--onto` | Argument rules quote jj names as revset string literals; U1 and T002 measure the literal form and flag on 0.45.1 and 0.46.0 |
| M6 | Med | progress.md edit hit the 009 entry | Reverted; wording applied to the 012 entry |
| L1 | Low | US2-4 versus rule | US2-4 aligned: every layer, bottom first |
| L2 | Low | `plan` null rule | null for `show` or `error`, non-null otherwise (data model, T003) |
| L3 | Low | M3 confound (branch deletion retarget) | Delegation table, M3 and U6 (3 to 4 s lag) updated |
| L4 | Low | U3 head filter | Recorded as M4a; only refs containing `/` unverified |
| L5 | Low | `stack-member-not-local` noise | Restricted to open members |
| L6 | Low | rebase needs checkout and clean worktree; `merge.ff` | Disclosed on the step effect and in the Plan rules; quickstart uses `-c merge.ff=true` |
| L7 | Low | `cat-file -e` not on the allowlist | Listed in plan.md |
| L8 | Low | 012 versus 025 treatment of denied sources; SC disposition | Stated in the status section; disposition row for SC-001 to SC-004 added |
