# Cross-artifact analysis: Stack inspection and plan (v2)

**Status**: Rounds 1 to 3 resolved in the artifacts; round 4 pending. Round 0 is the author draft (a28d23e).

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

### Round 2

Reviewer: independent reviewer (coordinator relay). Reviewed revision: 775290d. Verdict: NOT READY (2 high, 3 medium, 9 low). Findings were measured in scratch repositories by the reviewer; jj facts on 0.46.0 and 0.45.1.

| Id | Sev | Finding | Resolution |
| --- | --- | --- | --- |
| H1 | High | A squash or one-commit rebase merge on a stale `--base` dropped the parent's content | Plan rules: a layer is landed only by patch equivalence or a merged PR whose `merge_commit_sha` is a local ancestor of `--base`; otherwise group 3 (stale-base) for every merge kind, jj step included. Supersedes the round 1 dropped-set text |
| H2 | High | Group 2 "fetch, run again" never terminated on Git; suppressed `link` after the maintainer's own rebase | Fetch-first only for missing objects (`run-again`); new group 4 `reconcile-upstream` note terminates for native rewrites already fetched and for behind/diverged upstreams. Supersedes the round 1 H1 upstream rule |
| M1 | Med | A squash of a single-commit layer is patch-equivalent | Quickstart, D9 rows (one- and multi-commit), T010, US2 independent test corrected |
| M2 | Med | New plan rules lacked scenarios and tests | US2 scenarios 6 to 10; T006 and T029 extended |
| M3 | Med | plan.md D6 and D7 outdated | D6 and D7 rewritten for the groups, landed set, stale base, reconcile, blocked layer and `native_checked` |
| L1 | Low | M3 confound overcorrected; lag 4 s and 5 s | M3, delegation table and U6 reworded to "confounded, ordering suggests native, plain-chain retarget unmeasured" |
| L2 | Low | `+` not in the grammar | Argument rules say only a trailing `-` needs quoting and `+` is `unsafe-name` |
| L3 | Low | Group 5 wording | Groups renumbered 1 to 6; group 6 only when groups 2 to 5 produced no step |
| L4 | Low | `blocked.layer` for `merged-layer-not-bottom` | The lowest landed layer above a not-landed one (spec, data model, scenario 9) |
| L5 | Low | `native_checked` after a failed remote read | True only when the remote section exists and the pull request and native stack sources are `ok`; spec and data model aligned |
| L6 | Low | Full page bypassed the Findings table | `pull-request-ambiguous` row covers it (`truncated`, `incomplete`, `stack-limit`) |
| L7 | Low | Human text for `stale-base` | Contract gives the text of all three notes |
| L8 | Low | jj working-copy effect undisclosed | Disclosed in the group 5 text and the step effect in the data model |
| L9 | Low | `link` after `rebase` implies a non-fast-forward push | `link` is never printed beside a `rebase` and requires upstream `same`, `none` or `ahead`; disclosed |
| J | Info | jj facts | M13 records them as measured; U1 narrowed; T002 re-runs them; printed flag is `-o` |

### Round 3

Reviewer: independent reviewer (coordinator relay). Reviewed revision: 4f2fafd. Verdict: NOT READY (1 high, 3 medium, 6 low). Measured in scratch repositories; round 2 H1 holds and group 4 terminates after the maintainer's own rebase.

| Id | Sev | Finding | Resolution |
| --- | --- | --- | --- |
| G2-LOOP | High | Group 2 never ends when a PR head cannot be fetched (deleted branch, fork head, merge commit in no branch); the spike's PR #8 head `6e019b0` is in that state | Heads of merged and closed PRs are never required and `head-sha-differs` is limited to open PRs; plan-only `--after-fetch` turns a still-missing object into the terminating `object-unreachable` note; scenario 8; T006, T020, T029. Measured `refs/pull/8/head` still holds `6e019b0` (research M13). Supersedes the round 2 group 2 text |
| Med-A | Med | `reconcile-upstream` advice harmful (rebase onto the upstream; lease push over GitHub's rewrite); false when upstream is `same` | Replaced by evidence-chosen notes `take-upstream`, `native-rewrite-pending`, `upstream-ahead`, `push-needed`, `diverged-undecided`; none advises rebasing onto the upstream; `native-rewrite-pending` claims no difference. Supersedes the round 2 group 4 text |
| Med-B | Med | Stale text said native plan is fetch and observe | US3-4, T006, T029, M3 and offline rows, D1, quickstart, R5, delegation table aligned with groups 1 to 6 |
| Med-C | Med | `"main@origin"` fails as a jj literal | Argument rules and group 5 write `"name"@"remote"`; T006 tests both forms |
| L1 | Low | Quickstart quoting; `s1-` silently the parent when `s1` exists | Quickstart shows `-o '"main"'`; research and Argument rules state the silent meaning |
| L2 | Low | jj divergence claim too strong | M13 and scenario 8a: local rebase keeps the divergence, a remote rewrite conflicts the bookmark and FR-004 refuses |
| L3 | Low | jj step also moves non-layer bookmarks | Disclosed in group 5 and the data model |
| L4 | Low | Landed definition contradicted for a patch-equivalent layer on a stale base | Group 3 is evaluated before landing and wins |
| L5 | Low | Upstream relation `unknown` gave an empty plan | Covered by group 2 (fetch, or `object-unreachable` after `--after-fetch`) |
| L6 | Low | SC-007 traceability | FR-to-scenario map in tasks.md; T001 mapped to SC-007 |
