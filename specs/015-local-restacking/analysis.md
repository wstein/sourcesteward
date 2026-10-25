# Current Owner-Decision Analysis: Spec 015

**Owner decisions: 2026-10-25. Author cross-artifact analysis only.** All former
OQ IDs are settled and indexed in [the decision index](../016-stacked-pr-coordination/research.md#cross-spec-decision-index).
The [concrete profiles](../016-stacked-pr-coordination/contracts/owner-decisions.md) distinguish workspace .work metadata from
external capture payloads, trusted policy, host-only optional local015 re-sign
and CLI exit-contract revision2. No new implementation/native/helper/forge test
or independent CLEAR is claimed. Historical implementation evidence stays at
its recorded revision; current hidden015code still returns3 until the new task.

## Current findings and gates

| ID | Disposition | Remaining evidence |
| --- | --- | --- |
| D1 | All39 owner directions are DECIDED, including --online-only/single-repo and default .work saved-plan metadata. | Exact current artifact review and source/native conformance; no invented native acceptance. |
| D2 | Metadata and external selected-byte/safety stores are distinct roles with explicit modes/race/layout/quota/export-risk guards. | Red-first shared store and role-separation tasks; no auto-ignore edits/expiry. |
| D3 | Optional actual re-sign belongs only to015 fixed trusted HOST helper/verifier; no stw keys, no helper means unavailable. | Measured profile/external-agent trust limits, bottom-up signed parents and final verified SHA before review;016 receipt validation only. |
| D4 | Target CLI exit-contract revision2 is0/1/2, no legacy3 selector; JSON schema 1 frozen independently. | Red dispatch/help/JSON cases and root.go/restack.go mapping task; CURRENT code 3 not misreported changed. |

## Individual requirement coverage and literal future tests

Each current definition has one row, real task IDs and a literal future test.
Coverage is planned design mapping; unchecked tasks are not execution evidence.

| Requirement | Task IDs | Literal future test idea |
| --- | --- | --- |
| FR-001 | T003, T028 | Existing `TestPlanResolvesTheExplicitInput`; proposed `TestApplyPlanNeedsExplicitSelection` proves direct D6 inputs need no 012 implementation |
| FR-002 | T014, T027, T033 | Existing `TestEffectsListDescendantRefsWorktreesAndHooks`; proposed `TestApplyCASMovesOnlyOwnedTopic` checks declared ref and untouched HEAD/index/files |
| FR-003 | T005, T027, T038 | Existing `TestRepositoryHazardsAreRefusedByName`; proposed immutable/shared/native-descendant fixtures in `TestJJRestackRejectsUnselectedDescendants` |
| FR-004 | T031, T032, T038 | Proposed `TestRestackRecoveryPreparedBeforeTargetMove` pins old head; `TestJJRestackPreservesNativeIdentities` distinguishes change/commit/operation |
| FR-005 | T005, T029, T038 | Existing `TestDirtyTargetAndOperationsInProgressAreRefused`; proposed `TestApplyConflictStaysInQuarantine` and jj stored-conflict refusal |
| FR-006 | T015, T040, T041 | Existing `TestLeaseAndResyncTexts`; proposed `TestRestackTextsRemainInertData` records zero push/fetch/reset starts |
| FR-007 | T005, T018, T038 | Existing `TestJJAndNonRepositoriesAreRefusedWithoutAGitFallback`; proposed `TestJJRestackNeverFallsBackToGit` on colocated ownership |
| FR-008 | T018, T040 | Existing `TestRestackHumanOutputComesFromTheResultAndIsInert`; proposed human/JSON fixture parity in `TestRestackFutureExitTableAndV1Freeze` |
| FR-009 | T016, T029, T040 | Existing `TestAResultOverTheByteBoundIsIncomplete`; proposed `TestApplyBoundStopsBeforeNextEffect` retains partial actions |
| FR-010 | T021, T042, T049 | Run the existing `TestAPlanWritesNothingToTheRepositoryOnAnyPath` and enabled apply fixtures on named minimum/current lanes; missing lane is a blocker T049 records separately authorized fixed-host helper/verifier, bounds/confinement and final signed-chain evidence; unavailable enforcement must leave the capability disabled before T048. |
| FR-011 | T002, T003 | Existing `TestValidateOptionsRefusesNonExplicitRevisions` and `TestOldParentMustBeAnAncestorOfTheBranch` reject HEAD/expressions and wrong range |
| FR-012 | T011, T012, T019 | Existing `TestAPlanWritesNothingToTheRepositoryOnAnyPath` and `TestTheSentinelNoticesAnObjectWrite` prove content sentinels/control |
| FR-013 | T006, T007, T008, T009, T010, T011, T013, T014, T015, T024 | Existing `TestRangePairingMarksEqualChangedAndUnpairedCommits`; fixture bundle additionally covers cherry/own diffs/identity/conflicts/overlap/effects/texts; future clean-plan limitation emission fixture from T024 |
| FR-014 | T002, T003, T004, T005, T013 | Existing `TestPlanRefusals`, `TestAttributeHazardsOnTouchedPathsAreRefused`, `TestBranchCheckedOutElsewhereIsReportedAndRefused`; T004 remains the optional characterization/extraction skip, not executed evidence |
| FR-015 | T009, T011, T024, T030 | Existing `TestIdentityComesOnlyFromTheFlag`, `TestAPlaceholderIdentityIsAMismatchNotExpected`; future actual author/committer fixture never borrows prediction pass |
| FR-016 | T010, T018 | Existing `TestSemanticBreaksAreNeverReported` and `TestOutcomeTableOverTheFullCartesianProduct` keep clean replay from asserting safety |
| FR-017 | T017, T018, T020, T040, T045, T046 | Existing `TestRestackPlanFixturesValidate`, `TestRestackExitStatuses`; proposed `TestRestackFutureExitTableAndV1Freeze` preserves JSON/schema bytes, characterizes CURRENT code 3, and requires revision-2 incomplete 1 after T045/T046 |
| FR-018 | T016, T029 | Existing `TestRangeTooLargeIsIncomplete`, `TestMoreThanMaxPathsIsIncompleteNeverTruncated`, `TestACancelledPlanIsIncompleteAndDeadlineExpiryToo`; future same numeric bound edges |
| FR-019 | T025, T027, T028, T033, T034 | Proposed `TestApplyPlanNeedsExplicitSelection`: missing saved plan/id/local flag yields no effect/prompt; unrelated or modified plan refuses |
| FR-020 | T029, T030 | Proposed `TestApplyReplaysExactTwoDotRange`: three-layer fixture replays only exact old-parent-exclusive range onto full new parent, never porcelain/original rr-cache/another ref; reference commands pin `rebase.updateRefs=false` and `rerere.enabled=false` |
| FR-021 | T025, T027, T028, T033, T034 | Proposed `TestApplyResamplesEveryGuardBeforeCAS`: inject new worktree, foreign attribution, backend/common-dir/config/protection/head change before write |
| FR-022 | T025, T029, T030, T047, T048, T049 | Proposed `TestApplyRejectsAnyNonEqualRow` and `TestApplyReadsActualAuthorAndCommitter`: `!`, `<`, `>`, unpaired, unknown or leaked actual committer block target CAS; `TestApplyNeverRunsSigningHelper` and `TestApplyReportsDroppedSignatures` cover unsigned candidates and signature-policy refusal; actual-apply identity flags cannot widen the trusted owner; push fixture covers foreign open-parent commits in observed-default..new-head and local/global config origins T049 records separately authorized fixed-host helper/verifier, bounds/confinement and final signed-chain evidence; unavailable enforcement must leave the capability disabled before T048. |
| FR-023 | T025, T031, T032, T033, T034 | Proposed `TestRestackRecoveryPreparedBeforeTargetMove`: crash/write/fsync/rename/root-collision faults stop before target move with retained evidence |
| FR-024 | T033, T034, T035, T036 | Proposed `TestRestackCrashReconcilesEveryBoundary` and `TestRestackRestoreRequiresSeparatePlanAndCAS`: `restore --run` preview/optional save never moves refs; `--plan ID --authorize` selects the stored reverse plan; conflicting flags exit2; current third value remains untouched |
| FR-025 | T040, T041 | Proposed `TestRestackReceiptPreservesChangeClassReviewPolicy` and `TestRemoteHandoffRejectsOtherSHAOrMissingAuthorization`: docs-only checks/equality need no pre-push review; code pure rebase permits exact-new-SHA middle-tier CLEAR; 016 reclassifies the full publication-range per-commit path union so docs-only child receipts cannot hide code/unknown open-parent paths; old/predicted/other SHA cannot satisfy required review; Ready always exact-head CLEAR plus green checks |
| FR-026 | T040, T041 | Proposed `TestRestackTextsRemainInertData`: recorder forbids reset/switch/push/link/retarget; consumer requires separate observed full remote SHA and plan/action auth |
| FR-027 | T025, T037, T038, T039 | Proposed `TestJJRestackRejectsChangedOperation` and `TestJJRestackNeverFallsBackToGit`: missing capability/sandbox refuses with original native state unchanged |
| FR-028 | T029, T030, T038, T039 | Proposed `TestApplyConflictStaysInQuarantine`: original Git operation sentinel absent; jj stored conflict remains and refuses without resolution/snapshot |
| FR-029 | T025, T027, T040, T041, T045, T046 | Proposed `TestRestackFutureExitTableAndV1Freeze`: future success/refused/partial/incomplete/usage 0/1/2, actual v1 incomplete remains3 and byte-identical |
| FR-030 | T027, T028, T041 | Proposed `TestApplyPlanRevalidatesObservationInputs`: changed exact parent/head/backend/semantics, incomplete 014 result or unknown native-stack management refuses |
| FR-031 | T027, T029, T030, T031, T032, T038, T040, T043, T047, T048, T049 | Proposed `TestRestackJournalRejectsForeignTypeAndPath` and `TestRestackHostileRecordsAreNotInstructions`: symlink/replacement/foreign-record/credential/helper/signing-program sentinels; `--no-gpg-sign` and per-source `signature-dropped` fixtures remain proposed T049 records separately authorized fixed-host helper/verifier, bounds/confinement and final signed-chain evidence; unavailable enforcement must leave the capability disabled before T048. |
| FR-032 | T029, T030, T033, T034, T040, T043 | Proposed `TestApplyBoundStopsBeforeNextEffect`: 201 commits, 2001 paths, 1MiB+1 bytes, fake deadline/cancel retain partial state with exit1 |
| FR-033 | T031, T032, T035, T036 | Proposed `TestRestackKeepsIncompleteRecovery` and `TestRestackUnknownNewHeadRetains`: no automatic pruning/reset, no untracked/remote restoration claim |
| FR-034 | T025, T026, T037, T042, T043, T044, T049 | Proposed capability matrix fixture has missing minimum jj lane and must remain blocked; T044 requires exact-head independent review before publication T049 records separately authorized fixed-host helper/verifier, bounds/confinement and final signed-chain evidence; unavailable enforcement must leave the capability disabled before T048. |
| FR-035 | T045, T046 | Force typed restackIncomplete through root dispatch: target1, ordinary errors1, usage2, complete0; restore3 mutant fails, no legacy selector and unchanged schema1/JSON/help cases. |
| FR-036 | T030, T047, T048, T049 | Without trusted measured host helper refuse; with explicitly authorized fixture sign each actual parent bottom-up, verify public signer/loss provenance and final head before review/CAS, no key reads or unsigned fallback. T049 records separately authorized fixed-host helper/verifier, bounds/confinement and final signed-chain evidence; unavailable enforcement must leave the capability disabled before T048. |
| SC-001 | T021, T042, T049 | Native lane matrix executes every enabled acceptance fixture, marks minimum/current/backend omissions unverified, never pass T049 records separately authorized fixed-host helper/verifier, bounds/confinement and final signed-chain evidence; unavailable enforcement must leave the capability disabled before T048. |
| SC-002 | T012, T022, T033, T040, T042, T043, T047, T048, T049 | Existing `TestAPlanWritesNothingToTheRepositoryOnAnyPath`; proposed sentinel comparison covers all declared/undeclared effects and unknown partial outcomes T049 records separately authorized fixed-host helper/verifier, bounds/confinement and final signed-chain evidence; unavailable enforcement must leave the capability disabled before T048. |
| SC-003 | T017, T020, T040, T042, T045, T046 | Existing `TestRestackPlanInvariants`; proposed future fixture validation plus byte comparison of every existing schema/fixture |
| SC-004 | T022, T026, T043, T044 | Literal audit: extract every FR/SC id and require one trace row naming an existing task plus a test/fixture idea; missing T021 execution never becomes accepted |
| SC-005 | T029, T030, T033, T034, T042, T047, T048, T049 | Proposed `TestApplyCASMovesOnlyOwnedTopic` plus negative equality/actual-identity fixtures prove only one named ref moves with all rows equal T049 records separately authorized fixed-host helper/verifier, bounds/confinement and final signed-chain evidence; unavailable enforcement must leave the capability disabled before T048. |
| SC-006 | T035, T036, T043 | Proposed `TestRestackCrashReconcilesEveryBoundary` and `TestRestackRestoreRequiresSeparatePlanAndCAS` exercise old/new/third/missing state at each crash boundary |
| SC-007 | T040, T041, T043 | Proposed `TestRemoteHandoffRejectsOtherSHAOrMissingAuthorization` checks docs-only pre-push exemption and code pure-rebase middle-tier allowance, rejects missing/stale/other-SHA review where required, missing auth, foreign open-parent identities, widened caller identity list, foreign user-config name/email values, unrelated user-option secret redaction and remote-head changes; 015 write recorder stays empty |
| SC-008 | T037, T038, T039, T042 | Proposed `TestJJRestackPreservesNativeIdentities` with operation/bookmark/conflict/native sentinel matrix on both versions; disabled capability returns explicit refusal |

## Metrics and task state

44 individual FR/SC definitions, 49 task ids, 20 historical
checked tasks; 29 unchecked. Coverage 44/44 (100% design
mapping), zero requirement orphans. No implementation checkbox is changed by
owner answers or this analysis. All new exit/store/signing tasks are unchecked.
Task dependencies remain red test → implementation → native/effect conformance
→ exact-source independent review. Spec043 human-only landing remains separate.

## Compatibility and acceptance boundary

012 online means only --online; old alias/batch unavailable now. 013 payloads
and safety copies stay external; .work only holds private minimal metadata with
explicit export risk. 014 preflight never signs or mutates originals. 015 D7/D8
and unsigned D9 prediction stay settled; optional actual signing needs the host
profile and final signed-SHA review. 016 remote flags cannot invoke that local
helper. 043 identity authority is trusted workspace policy, global config only
corroborates, with existing human confirmation/shape/retarget/deletion gates.
No build, installation, helper run, schema-file edit or live write is claimed.

Original T001–T024 retain 20 checked tasks: T004 deliberately skipped, T021/T023/T024 open. T018/verification recorded old exits0/1/2/3 honestly; T045/T046 target revised mapping and do not rewrite that history.


## Historical evidence preservation

The [complete pre-owner-answer analysis](analysis-before-owner-decisions.md) is
retained as a dated snapshot, including executed independent-review provenance,
old mappings and recorded limitations. Its old current/pending wording is
historical only and does not override the owner-decision target above.
