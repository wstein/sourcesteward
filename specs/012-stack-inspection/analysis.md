# Cross-artifact analysis: Stack inspection and plan (v2)

**2026-10-25 status**: author cross-artifact analysis complete; independent exact-head review of this refresh is pending. Historical round 9 READY below applies only to its recorded revision and never to this changed head. Spec 012 remains unimplemented; no acceptance task is marked complete.

## Analysis method and provenance

The installed read-only Spec Kit analysis examined the existing feature’s spec, plan, tasks, model/contracts, constitution 1.2.0 and the referenced adjacent specs. Prerequisites resolved the existing feature with no persisted state or extension hooks. This document records the author report separately from that read-only operation; it is not independent clearance. The source base was `3d4c867a451217a2942564a7dcb4736d45a09b70`. Historical 2026-10-17 review rounds remain below with their own revisions and verdicts.

## Independent PR review and appended corrections

The [independent PR #237 review](https://github.com/wstein/sourcesteward/pull/237#pullrequestreview-5640826412) reviewed exact head `c3b39da683d815307efb81ec5abaf15c9204a06b` and returned **NOT CLEAR: nine open findings**, plus one non-blocking information item. The resolutions below are author changes appended after that reviewed head; they require an independent delta review of the exact corrected head. Neither this table nor the old historical READY transfers clearance.

| Finding | Author resolution and traceable artifact | Future assertion/task |
| --- | --- | --- |
| F1 | spec.md OQ-NET, plan question 3 and research/quickstart/contract/checklist remove invented network approval; scope and name remain open | T001/T020 gate the affected surface on an owner answer |
| F2 | FR-020, plan decision 16 and contract caller orchestration select bounded repositories with per-entry native evidence, shared budgets and aggregate exit 1 for any incomplete/refused/error outcome | T037 mixed Git/jj, reverse completion, independent samples, cancellation and aggregate outcomes |
| F3 | Commands vocabulary and OQ-FLAGS distinguish proposed --pull-requests from shared --online, human/default output, fixed deadline and no store; no unselected alias/flag is accepted | T020 selected/rejected flag matrix before implementation |
| F4 | data-model.md field-to-flag table maps fully qualified branch, old/new/expected head, Git SHA-1-only v1 limits, actual predecessor apply and explicit caller-only identity; diagnostics stay command-specific | T036 rejects unsupported ids, inferred identity, missing actual predecessor or stale values |
| F5 | depth retains local layers; qualified complete open_depth excludes only proven-landed layers, incomplete/error is null, and advice uses candidate open_depth >2 | T035 includes the local-depth-3/open-depth-2 example and unknown/incomplete cases |
| F6 | US2.12 and data model locate boundaries in referenced Layer/base/plan fields rather than claiming ids live in the finding; later destination remains null pending actual predecessor apply | T038 verifies child indices and boundary location; T036 checks the downstream mapping |
| F7 | tasks.md forward/reverse FR/SC/story tables and analysis rows now share exactly the task Coverage/US edges, including all added tasks and remote/acceptance entries | Read-only document coverage audit; future T001/T033 maintain traceability |
| F8 | native-stack finding and human summary retain M3’s head-deletion-enabled-only measurement; no disabled-setting result is called measured | T027 checks qualified summary wording |
| F9 | group 6 and contract always print a validated explicit trunk with --base, retain it in expected and block unknown/unsafe trunk without GitHub-default substitution | T006 tests non-default trunk, unknown/unsafe blocking and no wrong-base advice |
| R1 (additional review) | Every current inert Git replay argv includes fixed `-c rebase.updateRefs=false -c rerere.enabled=false` before rebase; historical measurement quotes remain labelled historical and 015 D7 is unchanged | T006 pins argv/display; T010 uses enabled-config/side-ref/rr-cache scratch fixtures to prove the fixed reference form |
| F10 | Session execution/publication instructions were removed from plan/tasks; analysis retains durable method, review provenance, checks and open evidence only | Documentation scope review; no product behavior added |

## Findings and owner questions

| ID | Category | Severity | Location | Result and next action |
| --- | --- | --- | --- | --- |
| OQ-NET | Scope/naming question | Medium | spec.md, Needs owner decision; FR-005/FR-007 | Opt-in network scope and naming were open recommendations, not approved decisions. Recommend offline default plus shared `--online` and an optional `--pull-requests` alias. The current draft proposes only `--pull-requests`; owner scope/name selection precedes remote-surface implementation. |
| OQ-EXIT | Compatibility question | Medium | spec.md, Needs owner decision; contracts/stack.md, Exit status | A future shared 0/1/2 convention differs from implemented hidden Spec 015 v1 incomplete exit 3. Recommendation: preserve v1 and require a versioned migration decision. Current unimplemented 012 stays 0/1/2 and changes no runtime/schema file. |
| OQ-FLAGS | Vocabulary question | Medium | spec.md, Shared toolbelt vocabulary; contracts/stack.md | Recommend `--human` exclusive with `--json` and caller `--timeout` that only shortens LIM-1. Current draft accepts neither; `--state-dir` is inapplicable because no store exists. Owner selection precedes those flag changes. |

These are explicit owner questions, not silently chosen answers or contradictory current execution rules. No critical/high defect or zero-coverage requirement remains in the current documentation design. They do not prevent review of this Markdown scope; future work changing either contract must wait for its decision. External/native measurements U1–U10 remain unverified and are future implementation gates.

## Individual requirement coverage and literal test ideas

Every FR and every SC has its own row. “Covered” means a proposed task and concrete future assertion exist, not that it was executed or passed.

| Requirement | Task IDs | Literal future test idea |
| --- | --- | --- |
| FR-001 | T020, T036 | Invoke show and plan with the same base/tip and compare observations; require owner-selected shared/network vocabulary before implementation and reject unselected flags; invoke the bare group and assert help with zero observation calls, prompts or mutation. |
| FR-002 | T008, T016 | Omit tip, supply an ambiguous ref and supply a remote tip; assert usage/ref refusal, then resolve explicit valid base/tip to exact native ids. |
| FR-003 | T002, T016, T017, T021 | Run a colocated and secondary jj fixture and assert the Git adapter call count is zero; unavailable jj refuses and declared gaps remain visible. |
| FR-004 | T002, T008, T009, T011, T016 | Build a three-layer linear range, then add a merge or divergent jj change; assert ordered partitioned layers for the first and explicit topology refusal for the second. |
| FR-005 | T013, T018, T025 | Run without pull-requests in a fixture with remotes/credentials available and assert zero HTTP/auth calls and unchanged cached refs/objects. |
| FR-006 | T004, T005, T009, T010, T017, T028, T038 | Iterate every Findings row, including both new notices; assert fixed summary/detail, deterministic order, evidence fields and no unknown-as-absence result after a denied source. |
| FR-007 | T023, T024, T025, T026, T028 | Enable pull-requests with fake HTTP and assert only the two exact GET shapes; redirect, extra query or malformed body must fail without another request. |
| FR-008 | T004, T023, T027, T038 | Return one name-matched PR with a different head SHA; assert link=name-matched, never verified, and no inferred authority. |
| FR-009 | T027, T029, T032, T036 | Return a native parent rebase-merge and rewritten child head; assert observe-only reconciliation and no local rebase, remote write or owner-landing invocation; native behavior summaries retain the M3 head-deletion-enabled-only measurement limit. |
| FR-010 | T002, T006, T010, T011, T017, T029, T036, T038 | Inspect a plain-chain rebase plan and assert the exact fixed `git -c rebase.updateRefs=false -c rerere.enabled=false rebase --onto` prefix plus bottom-up full old-tip boundaries; replace one linked worktree owner or use a stale base and assert blocked/observe precedence with no command started; a link for non-default trunk release includes explicit --base release, while unknown/unsafe trunk blocks without default substitution and expected records the trunk. |
| FR-011 | T005, T015, T019, T026 | Supply commit 501, layer 33, finding 401 or a response/result over 1 MiB; assert named stack-limit/incomplete rather than truncated success. |
| FR-012 | T015, T019, T025 | Move a ref, jj operation or PR head between initial read and resample; assert stack-changed and boundary.complete=false. |
| FR-013 | T003, T005, T020 | Validate proposed ok/incomplete/error fixture JSON offline after future schema implementation; assert human values agree and usage exit 2 emits no JSON, with all existing schema bytes unchanged. |
| FR-014 | T006, T014, T019, T020, T030 | Supply a ref with a control byte, bidi text and an unsafe command name; assert escaped bounded output and no unsafe argv/request or credential-bearing diagnostic. |
| FR-015 | T007, T013, T014, T018, T032 | Plant hook/filter/textconv/credential-helper markers and compare refs, index, files, objects, jj working copy and operation log before/after; assert no marker execution or target write. |
| FR-016 | T007, T012, T021, T022, T031, T033, T034 | Execute the future native minimum/current Git/jj matrix with fake HTTP and sentinels, then remove a required lane; assert it is recorded not-run and blocks acceptance. |
| FR-017 | T033, T037 | Assert CLI contains only parsing/rendering, no new dependency or process/network edge, and the later authorized architecture check rejects a planted forbidden import or ad hoc go statement. |
| FR-018 | T035 | Compare complete candidate open depths 2/3/32 and local depth 3 with one proven landed: only candidate >2 gets inferred advice, so 3 local/2 candidate gives none. Unknown landing stays a qualified upper bound, incomplete/error open_depth is null, closed-unmerged stays a candidate, and 33 local layers independently yields stack-limit without advice changing status/exit/blocking. |
| FR-019 | T036, T038 | Resolve child indices to Layer fork/tip and sampled base, map to fully qualified `--branch` plus exact old/new/expected-head flags, and require an actual predecessor apply before a later null destination can form a new plan. Reject jj/64-hex ids for Git SHA-1-only 015 v1; never derive `--identity` from 012. Closed-unmerged/stale parents never prove landing, and predicted heads, patch ids or sampled upstreams never become receipts/review/leases; diagnostic namespaces stay command-specific. |
| FR-020 | T037 | Select independent Git/jj repositories with distinct base/tip and online choices, finish them in reverse order, refuse one owner gate, change only one sample, cancel before/during workers and exhaust the shared 30-second/50-GET/1-MiB budgets. Assert indexed per-entry native identities/statuses/sampling and explicit incomplete/not-started outcomes, zero inherited auth or backend fallback, at most four workers for 1..16 caller entries, caller aggregate exit 1 whenever any member is incomplete/refused/error/canceled/not-started, exit 0 only when all observations complete (never readiness), usage exit 2 before invalid selection starts work, and unchanged single-PATH CLI/schema with no discovery or implicit network. |
| SC-001 | T021, T031 | Run all story scenarios on the declared native layouts/version lanes; hide one backend and assert the acceptance matrix cannot report a pass. |
| SC-002 | T010, T021, T029, T038 | Replay merge-commit, rebase-merge and squash fixtures: no merge drift, exact plain-chain replay boundaries and native observe-only handling; jj patch-equivalence remains an explicit gap. |
| SC-003 | T027, T028, T038 | Replay historical native-stack shapes with denied/malformed alternatives; assert name-only linkage never verifies and unreadable/closed-unmerged evidence never becomes absence or landing. |
| SC-004 | T013, T018, T021, T024, T030, T031, T036 | Surround every fixture with target-state/process/HTTP sentinels; assert no printed command runs, offline has no network and online records only allowlisted GETs. |
| SC-005 | T003, T020 | Validate each complete/incomplete/error proposed stack fixture after the future schema exists; byte-compare every pre-existing schema and fixture to the base. |
| SC-006 | T006, T030, T037 | Combine hostile ref text, moved references, cancellation and exhausted bounds; assert fixed statuses, validated printable argv and deterministic indexed outcomes. |
| SC-007 | T001, T012, T033, T034 | Enumerate FR/SC ids and require this individual task/test row for each; inject a missing task or skipped native lane and assert traceability/acceptance refuses completion. |
| SC-008 | T035 | Assert all-pending candidate depths 2/3/32 yield no notice/notice/notice, 3 local minus 1 proven-landed yields open_depth 2 and no notice, unknown landing never becomes an exact open-PR count, incomplete/error yields null, and 33 local layers hits the separate hard cap. |

## Coverage audit

- Functional requirements: 20/20 have individual task and literal test-idea coverage.
- Success criteria: 8/8 have individual task and literal test-idea coverage.
- Combined design coverage: 28/28 (100%); zero untraceable grouped requirements.
- Tasks: 38, all unchecked. Story labels: US1 6, US2 6, US3 7, US4 10, US5 2; setup/foundation/final evidence tasks 7. The retained work-item structure reflects existing reviewed dependencies rather than manufacturing a documentation PR stack.
- Forward/reverse requirement edges and story edges agree with every task’s explicit Coverage plus US label; the complete reverse task table is in tasks.md. FR-012 includes remote resampling T025, FR-016 includes actual-status recording T034, and SC-007 includes T012/T034; new story tasks T035/T038/T037/T036 appear under US1/US2/US4/US5 respectively.
- Unmapped tasks: none; T001 gates SC-007 traceability, T034 records actual acceptance, and the remaining tasks map directly through their story/FR/SC entries.
- Critical issues: 0. High issues: 0. Open owner questions: 3. Duplicate requirements needing consolidation: 0.
- Constitution alignment: no proposed exception. No prompt, implicit network, borrowed ownership, config-derived expected identity, original-workspace simulation, implicit remote action or agent landing is authorized.
- Requirement and task paths are explicit; future implementation work outside this feature directory is a proposal, not an edit authorized by this documentation refresh.

## Documentation verification and remaining acceptance

The offline `python3 -I -B scripts/check-docs.py` check passed for 352 authored documents. Targeted `/opt/bundledock/toolbox/bin/typos specs/012-stack-inspection` and `git diff --check` passed. The read-only document coverage audit passed: all 20 FR and 8 SC have individual tasks plus literal test ideas; all 38 unchecked tasks map back, and all forward/reverse requirement and story edges agree. Go tests, make targets, builds, native conformance, live GitHub calls and executable JSON/schema tests were not run for this Markdown-only change and are not passing acceptance claims.

Independent delta review of the exact corrected head and required CI remain pending. The unimplemented feature still needs its source tasks, owner selections for unresolved surfaces, native/backend/schema conformance and actual acceptance evidence. Historical clearance, author traceability and complete observations never imply implementation readiness or permission to execute an effect.

# Historical 2026-10-17 independent analysis rounds

**Historical status only**: READY after round 9 (0 high, 0 medium); the round 9 low and info wording findings were fixed by the author without a further review round, by owner decision. Round 0 is the author draft (a28d23e).

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

### Round 4

Reviewer: independent reviewer (coordinator relay). Reviewed revision: 6331f5a. Verdict: NOT READY (2 high, 3 medium, 4 low). Evidence in scratch repositories. The owner accepted `--after-fetch`.

| Id | Sev | Finding | Resolution |
| --- | --- | --- | --- |
| H1 | High | `push-needed` could not tell a local rewrite from a collaborator push; no lease value; native layers not excluded | Git only, non-native layer, `native_checked` true, tip descends from `--base` and `git cherry LAYER UPSTREAM` shows only `-`; otherwise `diverged-undecided`; jj always `diverged-undecided`; the second `cherry` call is on the process allowlist. Supersedes the round 3 `push-needed` rule |
| H2 | High | A `gone` upstream after `fetch --prune` kept the plan in group 2 and hid the rebase | Relation `gone` added; `gone`, no upstream and the upstream relations of dropped layers are outside groups 2 and 4; scenario 8b |
| M1 | Med | `run-again` text did not name `--after-fetch` | Contract text names the flag |
| M2 | Med | Native note chosen from the upstream alone | Chosen from the pull request head relation (`diverged`/`remote-ahead` take-upstream; same/`local-ahead` native-rewrite-pending); independent of upstream configuration |
| M3 | Med | Observe steps named no layer | One observe per affected layer, `layer` set (except `run-again`), ordered bottom first; texts name the layer |
| L1 | Low | T006 said `reconcile-upstream` | Aligned with the evidence-chosen notes |
| L2 | Low | `after_fetch` not echoed | Field in the result and the JSON example |
| L3 | Low | jj deleted bookmark after remote merge | Finding `layer-ref-deleted` and a plan block at that layer; no `jj rebase` step moves the landed commit |
| L4 | Low | `native-rewrite-pending` had no way out | Text points to GitHub's own stack handling |

### Round 5

Reviewer: independent reviewer (coordinator relay). Reviewed revision: 14b51b6. Verdict: NOT READY (2 high, 2 medium, 6 low). The orchestrator chose to simplify both High items by removing evidence-hungry advice.

| Id | Sev | Finding | Resolution |
| --- | --- | --- | --- |
| H1 | High | `git cherry` omits merge commits, so the `push-needed` proof could drop a collaborator's merge | `push-needed` removed; a diverged upstream always gives `diverged-undecided` with counts of upstream-only and local-only commits (merge commits counted) and no remedy; `Upstream.ahead_by`/`behind_by` added; `cherry LAYER UPSTREAM` removed from the allowlist; the statement that no advice can drop another person's commit is kept; L2 recomputes its own evidence. Supersedes the round 4 H1 rule (also resolves round 5 M1) |
| H2 | High | `layer-ref-deleted` cannot be derived on jj (no allowed read records the deletion) | Finding removed; stated jj gap `jj-landed-layer-undetectable` (boundary limitation on every jj result, Capability table); when `--pull-requests` shows a merged member and `stack-behind-base` is the only trigger the plan is one `observe` `landed-undetectable` with no jj rebase; scenario 8b, T017 and T029 updated; measurement entry M14. Supersedes the round 4 L3 resolution |
| M2 | Med | Upstream-based native wording survived | US3.4, R5 and the D9 row follow the pull request head relation |
| L1 | Low | Stale-base per layer unspecified | One `observe` per affected layer, bottom first, for groups 2 (`object-unreachable`) and 3 |
| L2 | Low | Group 6 used a nonexistent relation `none` and could end empty | Upstream absent, `same`, `ahead` or `gone` allowed; `behind`/`diverged` go to group 4 and `unknown` to group 2 |
| L3 | Low | Allowlist lacked the upstream reads | `%(upstream:track)`, `rev-parse` of the upstream commit and the two `rev-list --count` reads added |
| L4 | Low | T029 used a merged PR's head for `object-unreachable` | Open-pull-request head fixture; the spike head only shows that a merged head is never required |
| L5 | Low | FR-006 map omitted 8a and 8b | Added |
| L6 | Low | `object-unreachable` text unconditional | "if you have fetched (stw cannot verify)" |

### Round 6

Reviewer: independent reviewer (coordinator relay). Reviewed revision: 67d72e2. Verdict: NOT READY, no High (1 medium, 8 low, 2 info). jj results identical on 0.46.0 and 0.45.1.

| Id | Sev | Finding | Resolution |
| --- | --- | --- | --- |
| M1 | Med | The jj no-rebase guard never fires for a plain chain; the printed step moves the landed commit (empty after rebase-merge, conflicted after multi-commit squash) | Guard made exact and native-only (merged member's `head.sha` in the range); plain chains on jj declared unguarded and the outcomes disclosed in the Capability table and 8b; T017 and T029 cover the plain-chain jj case; `jj-landed-layer-undetectable` stays on every jj result |
| L1 | Low | Guard too broad (any merged member) and wrong bookmark claim | The head-in-range test replaces it; observe text reworded |
| L2 | Low | Allowlist could not tell `unknown` from `gone` | Unpeeled `rev-parse --verify --quiet` added; definitions in the data model and plan |
| L3 | Low | Group 4 note precedence ambiguous | (a) applies to every non-dropped layer of a native stack above a merged member; plain chains never get `take-upstream`; (b) otherwise |
| L4 | Low | `landed-undetectable` layer rule | Layer is the one containing the member head; data model and T003 if/then (`layer` null iff `run-again`) |
| L5 | Low | Two lease sources in L2 | Stale phrase removed |
| L6 | Low | Unbacked `link` push claim | Reworded; numeric link push marked unverified |
| L7 | Low | T017 not updated | Updated |
| L8 | Low | `gone` listed as detection; gone layer gets no hint; quickstart jj text | Table corrected; boundary limitation for a not-dropped `gone` layer; quickstart updated |
| I1 | Info | R1 said no jj binary | Reworded to the measured facts |
| I2 | Info | Allowlist said base and tip read once | Read at the first read and again at the resample |

### Round 7

Reviewer: independent reviewer (coordinator relay). Reviewed revision: 3b6bf3d. Verdict: NOT READY, no High (1 medium, 3 low, 3 info).

| Id | Sev | Finding | Resolution |
| --- | --- | --- | --- |
| M1 | Med | Group 4(a) fired for a merged lower member that is not a local layer, so the group 5 jj guard (`landed-undetectable`) was unreachable | (a) requires the merged lower member to be a linked local layer; a bookmark-less merged member goes to group 5; the precedence sentence is explicit (groups 2 to 5 in order, guard inside group 5) and the contradictory parenthetical is removed; T029 and scenario 8b cover it |
| L1 | Low | Squash outcome content dependent | Capability table states both outcomes; T017 tests both |
| L2 | Low | T029 not updated | Exact head-in-range trigger, plain-chain jj case and merged-head-outside-range case |
| L3 | Low | Round 6 measurements not recorded | Research M15; cited from the Capability table and plan R1 |
| I1 | Info | Unreachable clause "or a linked merged layer" | Removed; the guard is native-only |
| I2 | Info | gone/unknown verified on files backend only | Stated in M15 (reftable could not build the state) |
| I3 | Info | Several merged heads in different layers | One `landed-undetectable` observe per affected layer, bottom first |

### Round 8

Reviewer: independent reviewer (coordinator relay). Reviewed revision: fa1a56f. Verdict: NOT READY, no High (1 medium, 1 low, 3 info).

| Id | Sev | Finding | Resolution |
| --- | --- | --- | --- |
| M1 | Med | After limiting 4(a) to local layers the Git protection was lost: a native squash-merge with the local branch deleted printed a competing `git rebase --onto` | The group 5 guard is backend-independent (Git and jj): native stack, merged member `head.sha` in the range, `stack-behind-base` the only trigger gives one `landed-undetectable` observe per affected layer and no rebase; plain chains stay as before; wording ("branch or bookmark"), Capability table, data model, contract text, quickstart, T006 and T029 (Git cases, head outside range) updated. Supersedes the round 6 and 7 jj-only guard wording |
| L1 | Low | Scenario 8b Git sentence wrong for a native stack with `--pull-requests` | Limited to plain chains or runs without `--pull-requests`; the native local-`s1` case is the group 4 observe of 8a |
| I1 | Info | Rebase-merge or single-commit squash with local `s1` deleted blocks as `layer-partially-applied` | Sentence added in 8b |
| I2 | Info | Research native-stack row covered only local `s1` | Cross-reference to scenario 8b added |
| I3 | Info | Several-merged-heads fixture needs fake HTTP | Noted as fixture-only in T029 |

### Round 9

Reviewer: independent reviewer (coordinator relay). Reviewed revision: fd02dbf. Verdict: **READY** (0 high, 0 medium, 3 low, 3 info). The wording-level findings were fixed by the author; no further review round, by owner decision.

| Id | Sev | Finding | Resolution |
| --- | --- | --- | --- |
| L1 | Low | Precedence sentence still said jj guard | "group 5 guard (`landed-undetectable`, Git and jj)" |
| L2 | Low | Research cross-reference on the wrong row | Row split into local `s1` kept, local `s1` deleted with GitHub not yet rewritten (guard), and with GitHub already rewrote (4(b)) |
| L3 | Low | Capability table gave only jj outcomes | Git plain-chain outcome added (drop of commits already upstream, or add/add conflict), M16, and a Git counterpart in T029 |
| I1 | Info | "it goes to group 5" imprecise | Group 5 guard when the child's upstream is `same` or absent, 4(b) when `diverged` or `behind`; documented in 8b |
| I2 | Info | M15 head-in-range said jj only | M15 reworded; M16 records the Git measurement |
| I3 | Info | Weaker advice with local `s1` deleted | Stated in 8b with the reason |
