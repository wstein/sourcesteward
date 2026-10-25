# Analysis: Spec 015 historical review and proposed apply coverage

Historical T001 records the 2026-10-21 independent design review below and remains checked at its recorded SHA. The 2026-10-25 amendment analysis follows separately; historical CLEAR does not clear this changed specification or any proposed implementation.

## Reviews

| Round | Reviewed | Reviewer | Verdict |
| --- | --- | --- | --- |
| 1 | origin/main b0cc02d with PR #121 head 5738ba998cea2cf896328e1fa77920987cd56787 (design documents and implementation) | independent strongest-tier review (Opus) | NOT CLEAR: 1 medium, several low |
| 2 | delta at 3307bc49c224a5a8458f98bc5d55dceb0425ce1c (docs-only fixes on the restacked branch) | independent review | NOT CLEAR: 2 open (no task tracked emitting `rewrite-committer-not-predicted`; a stale `state` clause in the data model) and one nit (tasks.md header) |
| 3 | delta at 08cf56dd283367bc0e85d47c86cfd5dbe2db6ec0 (fixes of round 2) | independent review (Opus) | CLEAR |

An earlier review of the exact implementation head cb6bb64 was CLEAR with two wording follow-ups (object mtime caveat, WP2 wording), fixed in `5738ba998cea2cf896328e1fa77920987cd56787`.

## Findings and resolutions

| Id | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| F1 | medium | The identity check covers only the old moved commits; a by-hand rebase takes the new committer from the rebaser's configuration or environment, so `pass` can still feed `evidence-clean`. | Stated in FR-015, the data model, the contract and the quickstart (which adds the AGENTS.md `git log --format` check before a push). The limitation `rewrite-committer-not-predicted` is specified; the first-slice code does not yet emit the string, which is reserved and tracked by task T024 (not done in this docs-only change). |
| F2 | low | The contract promised partial evidence for `incomplete` and `evidence: null` elsewhere. | Both say `evidence: null` (what the code does); `boundary.reached` and the diagnostic name what stopped. |
| F3 | low | The data model listed `patch-equivalent-tree` and an undefined `present` state inconsistently. | `present` removed; the states are `dropped`, `patch-equivalent-tree`, `missing`, `unknown`. |
| F4 | low | Plan, tasks and spec still described reusing or extracting the tidy helpers. | Aligned with the T004 skip: own small helpers, `internal/workspace` unchanged. |
| F5 | low | FR-012 lacked the mtime caveat; the plan claimed `find -newer` stayed clean. | FR-012 and the plan say content-identical and disclaim mtimes. |
| F6 | low | `range-too-large` was listed as a refusal (exit 1) but is `incomplete` (exit 3). | The code and tests make it `incomplete`; the contract now lists it with the incomplete codes. |
| Q1 | request | T001 needs an analysis record. | This file. |
| Q2 | question | Keep the repository-local `core.hooksPath` refusal? | Kept: the plan refuses what the Spec 029/031 observation refuses; one sentence added to FR-014. No code change. |

## Verdict

Findings F1 to F6 are resolved in the documents, except the code emission of the F1 limitation string, tracked by T024. Round 2 found a stale `state` clause in the data model, a missing tracking task and a tasks.md header nit; all three are fixed in the round 3 commit. Verdict after the delta review: CLEAR at 08cf56dd283367bc0e85d47c86cfd5dbe2db6ec0.


## 2026-10-25 amendment: read-only consistency analysis

**Scope:** specification, plan, tasks, research, data model, quickstart, checklists
and four Markdown contracts. The installed Spec Kit analysis procedure is used
read-only after task generation, selecting this existing feature with
`SPECIFY_FEATURE_DIRECTORY=specs/015-local-restacking` and
`SPECIFY_FEATURE_NO_PERSIST=1`. No extension hooks/presets/overrides are configured
at the inspected base; no project-controlled command or arbitrary executable is
invoked. This record is an author-authored design analysis, **not independent
CLEAR** and not product acceptance. Prerequisite and documentation check outcomes
are recorded in [verification](verification.md#documentation-amendment-2026-10-25).

### Findings and dispositions

| Id | Severity | Evidence / disposition |
| --- | --- | --- |
| A1 | Resolved inconsistency | Original spec status called T001 pending while tasks/analysis show historical Opus CLEAR at `08cf56dd283367bc0e85d47c86cfd5dbe2db6ec0`; corrected status only, no checkbox/evidence rewritten |
| A2 | Owner decision, implementation blocker | v1 CLI `restackIncomplete` and T018 use exit3; OQ-1 preserves it and proposes explicit future 0/1/2 protocol, not an in-place change |
| A3 | Owner decision, implementation blocker | Trusted ref ownership/review issuer/freshness and actual committer/date inputs are undefined; OQ-2/OQ-3 refuse unproven input, with explicit recommendations |
| A4 | Owner decision, capability blocker | Attached author-worktree apply is normal manual workflow but D7 does not define safe worktree updates; OQ-6 marks unoccupied-only first apply as proposed narrowing |
| A5 | Owner decision, capability blocker | jj write/operation/bookmark/recovery guarantees are unmeasured; OQ-4/T037 keep capability unavailable, no invented native CAS/fallback |
| A6 | Owner decision, retention blocker | Existing sync writer is unexported and type-specific; OQ-5/T031/T032 propose typed seam with unchanged sync compatibility, no duplicated writer or automatic root pruning |
| A7 | Owner wording decision | Strict no-network wording and proposed 012 opt-in reads need owner reconciliation; 012 plan question 3 recommends keep-opt-in but is open, while recorded `--after-fetch` acceptance is separate. OQ-7 recommends offline default plus explicit bounded online/cached evidence without claiming owner approval; 015 stays offline |
| A8 | Resolved scope/timing | Actual candidate SHA appears before independent review; 015 receipt carries derived change class/pure-rebase evidence; 016 preserves docs-only checks/equality without pre-push review, code exact-new-SHA review and equal-pure-rebase middle-tier allowance. Ready always requires independent exact-head CLEAR and green checks; 043 human `--reviewed` is separate. Local apply never requires a prediction to be reviewed |
| A9 | Retained acceptance gap | T021/T023/T024 remain pending, T004 stays skipped, new tasks unchecked; no first-slice conformance or new apply verification inferred from docs checks |

No critical constitution conflict is proposed: mutation is explicit and scoped,
recovery precedes target changes, native capability gaps refuse and remote writes
stay separate. The open implementation choices are genuine blockers for their
named units. No assertion that every question is resolved or the feature is ready
for implementation is made.

### Requirement-to-task-to-literal-test coverage

Each row is one FR or SC. Existing test names are source references with only
historical 2026-10-21 execution evidence; future names are literal test ideas,
not tests added or run here. Product/native execution remains unverified as
qualified in verification.md. No row inherits coverage through a grouped range.

| Requirement | Task IDs | Literal test or concrete fixture idea | Evidence status |
| --- | --- | --- | --- |
| FR-001 | T003, T028 | Existing `TestPlanResolvesTheExplicitInput`; proposed `TestApplyPlanNeedsExplicitSelection` proves direct D6 inputs need no 012 implementation | Source/history; future pending |
| FR-002 | T014, T027, T033 | Existing `TestEffectsListDescendantRefsWorktreesAndHooks`; proposed `TestApplyCASMovesOnlyOwnedTopic` checks declared ref and untouched HEAD/index/files | Source/history; future pending |
| FR-003 | T005, T027, T038 | Existing `TestRepositoryHazardsAreRefusedByName`; proposed immutable/shared/native-descendant fixtures in `TestJJRestackRejectsUnselectedDescendants` | Source/history; future pending |
| FR-004 | T031, T032, T038 | Proposed `TestRestackRecoveryPreparedBeforeTargetMove` pins old head; `TestJJRestackPreservesNativeIdentities` distinguishes change/commit/operation | Future pending |
| FR-005 | T005, T029, T038 | Existing `TestDirtyTargetAndOperationsInProgressAreRefused`; proposed `TestApplyConflictStaysInQuarantine` and jj stored-conflict refusal | Source/history; future pending |
| FR-006 | T015, T040, T041 | Existing `TestLeaseAndResyncTexts`; proposed `TestRestackTextsRemainInertData` records zero push/fetch/reset starts | Source/history; future pending |
| FR-007 | T005, T018, T038 | Existing `TestJJAndNonRepositoriesAreRefusedWithoutAGitFallback`; proposed `TestJJRestackNeverFallsBackToGit` on colocated ownership | Source/history; future pending |
| FR-008 | T018, T040 | Existing `TestRestackHumanOutputComesFromTheResultAndIsInert`; proposed human/JSON fixture parity in `TestRestackFutureExitTableAndV1Freeze` | Source/history; future pending |
| FR-009 | T016, T029, T040 | Existing `TestAResultOverTheByteBoundIsIncomplete`; proposed `TestApplyBoundStopsBeforeNextEffect` retains partial actions | Source/history; future pending |
| FR-010 | T021, T042 | Run the existing `TestAPlanWritesNothingToTheRepositoryOnAnyPath` and enabled apply fixtures on named minimum/current lanes; missing lane is a blocker | T021 open; future pending |
| FR-011 | T002, T003 | Existing `TestValidateOptionsRefusesNonExplicitRevisions` and `TestOldParentMustBeAnAncestorOfTheBranch` reject HEAD/expressions and wrong range | Source/history |
| FR-012 | T011, T012, T019 | Existing `TestAPlanWritesNothingToTheRepositoryOnAnyPath` and `TestTheSentinelNoticesAnObjectWrite` prove content sentinels/control | Source/history; native gaps retained |
| FR-013 | T006, T007, T008, T009, T010, T011, T013, T014, T015, T024 | Existing `TestRangePairingMarksEqualChangedAndUnpairedCommits`; fixture bundle additionally covers cherry/own diffs/identity/conflicts/overlap/effects/texts; future clean-plan limitation emission fixture from T024 | Source/history; T024 pending |
| FR-014 | T002, T003, T004, T005, T013 | Existing `TestPlanRefusals`, `TestAttributeHazardsOnTouchedPathsAreRefused`, `TestBranchCheckedOutElsewhereIsReportedAndRefused`; T004 remains the optional characterization/extraction skip, not executed evidence | Source/history |
| FR-015 | T009, T011, T024, T030 | Existing `TestIdentityComesOnlyFromTheFlag`, `TestAPlaceholderIdentityIsAMismatchNotExpected`; future actual author/committer fixture never borrows prediction pass | Source/history; T024/future pending |
| FR-016 | T010, T018 | Existing `TestSemanticBreaksAreNeverReported` and `TestOutcomeTableOverTheFullCartesianProduct` keep clean replay from asserting safety | Source/history |
| FR-017 | T017, T018, T020, T040 | Existing `TestRestackPlanFixturesValidate`, `TestRestackExitStatuses`; proposed `TestRestackFutureExitTableAndV1Freeze` keeps schema bytes and exit3 | Source/history; future pending |
| FR-018 | T016, T029 | Existing `TestRangeTooLargeIsIncomplete`, `TestMoreThanMaxPathsIsIncompleteNeverTruncated`, `TestACancelledPlanIsIncompleteAndDeadlineExpiryToo`; future same numeric bound edges | Source/history; future pending |
| FR-019 | T025, T027, T028, T033, T034 | Proposed `TestApplyPlanNeedsExplicitSelection`: missing saved plan/id/local flag yields no effect/prompt; unrelated or modified plan refuses | Future pending |
| FR-020 | T029, T030 | Proposed `TestApplyReplaysExactTwoDotRange`: three-layer fixture replays only exact old-parent-exclusive range onto full new parent, never porcelain/original rr-cache/another ref; reference commands pin `rebase.updateRefs=false` and `rerere.enabled=false` | Future pending |
| FR-021 | T025, T027, T028, T033, T034 | Proposed `TestApplyResamplesEveryGuardBeforeCAS`: inject new worktree, foreign attribution, backend/common-dir/config/protection/head change before write | Future pending |
| FR-022 | T025, T029, T030 | Proposed `TestApplyRejectsAnyNonEqualRow` and `TestApplyReadsActualAuthorAndCommitter`: `!`, `<`, `>`, unpaired, unknown or leaked actual committer block target CAS; `TestApplyNeverRunsSigningHelper` and `TestApplyReportsDroppedSignatures` cover unsigned candidates and signature-policy refusal; actual-apply identity flags cannot widen the trusted owner; push fixture covers foreign open-parent commits in observed-default..new-head and local/global config origins | Future pending |
| FR-023 | T025, T031, T032, T033, T034 | Proposed `TestRestackRecoveryPreparedBeforeTargetMove`: crash/write/fsync/rename/root-collision faults stop before target move with retained evidence | Future pending |
| FR-024 | T033, T034, T035, T036 | Proposed `TestRestackCrashReconcilesEveryBoundary` and `TestRestackRestoreRequiresSeparatePlanAndCAS`: `restore --run` preview/optional save never moves refs; `--plan ID --authorize` selects the stored reverse plan; conflicting flags exit2; current third value remains untouched | Future pending |
| FR-025 | T040, T041 | Proposed `TestRestackReceiptPreservesChangeClassReviewPolicy` and `TestRemoteHandoffRejectsOtherSHAOrMissingAuthorization`: docs-only checks/equality need no pre-push review; code pure rebase permits exact-new-SHA middle-tier CLEAR; 016 reclassifies the full publication-range per-commit path union so docs-only child receipts cannot hide code/unknown open-parent paths; old/predicted/other SHA cannot satisfy required review; Ready always exact-head CLEAR plus green checks | Future pending; 016 consumer gate |
| FR-026 | T040, T041 | Proposed `TestRestackTextsRemainInertData`: recorder forbids reset/switch/push/link/retarget; consumer requires separate observed full remote SHA and plan/action auth | Future pending; 016 actions outside 015 |
| FR-027 | T025, T037, T038, T039 | Proposed `TestJJRestackRejectsChangedOperation` and `TestJJRestackNeverFallsBackToGit`: missing capability/sandbox refuses with original native state unchanged | Future pending; OQ-4 |
| FR-028 | T029, T030, T038, T039 | Proposed `TestApplyConflictStaysInQuarantine`: original Git operation sentinel absent; jj stored conflict remains and refuses without resolution/snapshot | Future pending |
| FR-029 | T025, T027, T040, T041 | Proposed `TestRestackFutureExitTableAndV1Freeze`: future success/refused/partial/incomplete/usage 0/1/2, actual v1 incomplete remains3 and byte-identical | Future pending; OQ-1 |
| FR-030 | T027, T028, T041 | Proposed `TestApplyPlanRevalidatesObservationInputs`: changed exact parent/head/backend/semantics, incomplete 014 result or unknown native-stack management refuses | Future pending |
| FR-031 | T027, T029, T030, T031, T032, T038, T040, T043 | Proposed `TestRestackJournalRejectsForeignTypeAndPath` and `TestRestackHostileRecordsAreNotInstructions`: symlink/replacement/foreign-record/credential/helper/signing-program sentinels; `--no-gpg-sign` and per-source `signature-dropped` fixtures remain proposed | Future pending |
| FR-032 | T029, T030, T033, T034, T040, T043 | Proposed `TestApplyBoundStopsBeforeNextEffect`: 201 commits, 2001 paths, 1MiB+1 bytes, fake deadline/cancel retain partial state with exit1 | Future pending |
| FR-033 | T031, T032, T035, T036 | Proposed `TestRestackKeepsIncompleteRecovery` and `TestRestackUnknownNewHeadRetains`: no automatic pruning/reset, no untracked/remote restoration claim | Future pending |
| FR-034 | T025, T026, T037, T042, T043, T044 | Proposed capability matrix fixture has missing minimum jj lane and must remain blocked; T044 requires exact-head independent review before publication | Future pending; documentation only |
| SC-001 | T021, T042 | Native lane matrix executes every enabled acceptance fixture, marks minimum/current/backend omissions unverified, never pass | T021 open; future pending |
| SC-002 | T012, T022, T033, T040, T042, T043 | Existing `TestAPlanWritesNothingToTheRepositoryOnAnyPath`; proposed sentinel comparison covers all declared/undeclared effects and unknown partial outcomes | Source/history; future pending |
| SC-003 | T017, T020, T040, T042 | Existing `TestRestackPlanInvariants`; proposed future fixture validation plus byte comparison of every existing schema/fixture | Source/history; future pending |
| SC-004 | T022, T026, T043, T044 | Literal audit: extract every FR/SC id and require one trace row naming an existing task plus a test/fixture idea; missing T021 execution never becomes accepted | This trace mapping; execution pending |
| SC-005 | T029, T030, T033, T034, T042 | Proposed `TestApplyCASMovesOnlyOwnedTopic` plus negative equality/actual-identity fixtures prove only one named ref moves with all rows equal | Future pending |
| SC-006 | T035, T036, T043 | Proposed `TestRestackCrashReconcilesEveryBoundary` and `TestRestackRestoreRequiresSeparatePlanAndCAS` exercise old/new/third/missing state at each crash boundary | Future pending |
| SC-007 | T040, T041, T043 | Proposed `TestRemoteHandoffRejectsOtherSHAOrMissingAuthorization` checks docs-only pre-push exemption and code pure-rebase middle-tier allowance, rejects missing/stale/other-SHA review where required, missing auth, foreign open-parent identities, widened caller identity list, foreign user-config name/email values, unrelated user-option secret redaction and remote-head changes; 015 write recorder stays empty | Future pending; consuming 016 acceptance |
| SC-008 | T037, T038, T039, T042 | Proposed `TestJJRestackPreservesNativeIdentities` with operation/bookmark/conflict/native sentinel matrix on both versions; disabled capability returns explicit refusal | Future pending; OQ-4 |

Coverage audit: **34/34 FRs and 8/8 SCs** individually mapped (42 rows), each
with task ids and a literal test or fixture idea. Every task-tag→requirement
and requirement-row→task edge agrees in both directions; historical checkbox
states/dates are unchanged and T004 remains an unexecuted optional extraction. There are **44 task ids**, 20
historically checked and 24 unchecked: four historical plus twenty proposed.
The current amendment runs no product tests and proves no capability acceptance.

### Unmapped/underspecified task audit

No task is orphaned from requirement coverage: T001/T023/T026/T044 are review
and analysis gates; T004 is the preserved optional extraction skip; T019 is the
historical architecture guard; remaining implementation tasks map above. Future
ownership/review-policy inputs, occupied-worktree scope, committer policy and
jj native writes remain deliberately blocked by OQ-1–OQ-6 and OQ-8. No unselected verify
command, keep-both resolution, descendant or remote effect is smuggled into the
implementation units. T025 records decisions rather than fabricating them.

### Next actions and limits

Independent reviewer should check this exact amended head, especially OQ-1
compatibility, OQ-2 provenance, OQ-6 scope and all crash/partial cases. Root
coordinates publication/review and required CI; neither is run by this author.
Only offline documentation checks are authorized for this amendment. Runtime
implementation requires the affected owner decisions, reviewed complete design
and the red-first/native evidence tasks. Historical acceptance remains separate.


## PR 238 independent review and author corrections (2026-10-25)

Independent Foreman Opus reviewed exact head
`98908cb293b6f6a90ace9a4bbc3ab60ae2b40a94` and returned **NOT CLEAR: seven open
findings** on [PR #238](https://github.com/wstein/sourcesteward/pull/238).
That verdict is retained; author corrections below are not independent CLEAR for
the new head. Corrections are appended focused commits, never a rewrite of the
reviewed history. Root owns the one correction push and subsequent review/CI.

| Finding | Author correction | Literal proposed fixture / remaining gate |
| --- | --- | --- |
| F1 medium: rewrite review tiering and human 043 statement | Derived moved `change_class`/`pure_rebase`, conservative 016 reclassification over every commit's paths in observed-default..actual-new-head (including open-parent code; unknown→code), docs-only local-check/equality exemption, code exact-new-SHA review with equal-pure-rebase middle-tier allowance; Ready always independent exact-head CLEAR + green required checks; 043 human `--reviewed` is separate | `TestRestackReceiptPreservesChangeClassReviewPolicy`: docs-only push without pre-push review, code pure-rebase middle tier, docs-only child with code/unknown open parent, net-tree cancellation and stale SHA; no product execution |
| F2 medium: trusted-owner full push range | Actual owner identity cannot be widened by flags; 016 checks every author/committer in observed-default..actual-new-head including parent commits. Config evidence renders origins/keys and only policy-needed name/email values; other user values omitted/redacted, no writes | `TestRemoteHandoffRejectsOtherSHAOrMissingAuthorization`: foreign parent, repeated foreign identity flag, local/global origin, unrelated user-option secret sentinel; trusted source remains OQ-2/OQ-3 |
| F3 medium: signature loss/helper effects | Explicit unsigned `--no-gpg-sign`, neutralized signing program/config/environment, per-commit `signature-dropped` even for `=`; OQ-8 refusal/disclosure choice linked to separate 043 OQ6 | `TestApplyNeverRunsSigningHelper`, `TestApplyReportsDroppedSignatures`; signed/required/unknown-policy scope unavailable pending owner decision; reviewer's native measurement attributed, not rerun |
| F4 low: reference command overrides | Human commands pin rebase.updateRefs=false/rerere.enabled=false; plumbing never reads/writes original rr-cache or moves other refs; v1 rendered text stays historical/frozen | `TestApplyReplaysExactTwoDotRange` with rr-cache and other-ref sentinels; no porcelain invocation |
| F5 low: finding provenance | Published `restack-needed` belongs to the main Spec 035 watch event; the current pending 012 PR #237 proposal is the distinct `restack-advised` finding. Earlier 015 heads used the pre-rename proposal name; neither name establishes stable-main 012 acceptance | Source/Markdown provenance only; pending 012 vocabulary is not acceptance; event code unchanged |
| F6 low: authorization/restore vocabulary | `--plan ID --state-dir DIR --authorize` and one `restore` leaf; `--run` previews, optional save stores reverse plan, separate explicit authorization applies it; rationale versus direct 032 restore recorded under OQ-1 | `TestRestackRestoreRequiresSeparatePlanAndCAS`: preview/save has no ref effect; mixed selection/apply flags exit2 |
| F7 low: task/analysis edges | Every task-tag/requirement-row edge is symmetric, including foundational FR-001–FR-005/FR-007 and signing-policy ownership; checkbox/dates unchanged | Markdown-only extraction audit: 42 rows, 44 tasks, 157 matching edges, historical 20 checked/24 unchecked; not native test evidence |

The shared brief's mistaken “owner-approved/settled 012 online reads” provenance
was also corrected. Main 012 plan question 3 still recommends keep-opt-in and is
open; recorded `--after-fetch` acceptance is separate. OQ-7 preserves a genuine
recommendation and the pending owner choice. It authorizes no 015 network effect.

Read-only installed Spec Kit analysis prerequisites passed again after corrected
tasks, with explicit existing feature/no-persist and no hooks/presets/overrides.
Cross-artifact audit preserves 34 FRs/8 SCs and the unchanged v1 schema/source,
eight explicit OQs, class-dependent pre-push versus independent Ready gates,
trusted-owner full-range identity, explicit signature loss, and bounded local
restore effects. Independent review of the exact corrected head is still pending;
the author does not promote the original NOT CLEAR verdict to CLEAR.
