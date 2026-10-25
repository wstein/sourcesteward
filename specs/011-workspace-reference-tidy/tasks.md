# Tasks: Workspace and Reference Tidy

Three sequential implementation WIs; each gets plan → tasks → independent analyze → failing tests
→ implementation → independent review/verification → focused commits → PR.

- [WI1 plan](wi1-plan.md) and [WI1 tasks](wi1-tasks.md): evidence-only plans, US1/US3.
- [WI2 plan](wi2-plan.md) and [WI2 tasks](wi2-tasks.md): local reference cleanup, US2/US3; [independent design analysis cleared](wi2-analysis.md), implementation complete; separate verification/PR handoff.
- [WI3 plan](wi3-plan.md) and [WI3 tasks](wi3-tasks.md): owned retirement, US1/US2/US3; concrete interface and independent analysis cleared; implementation and local review/verification complete; PR38 handoff complete; hosted acceptance pending.

## Requirement traceability
| Requirement | WI1 tasks | Later execution gates |
| --- | --- | --- |
| FR-001 | T001–003,T008 | WI2 reuse mutation journals |
| FR-002 | T004–008 | WI2 per-action base/target validation |
| FR-003 | T006–008,T013 | Unsupported forge retains; no network |
| FR-004 | T004–008 | WI3 dirty/ownership/freshness retention |
| FR-005 | T005,T007,T013 | WI2 bookmark-only; WI3 separate forget/files |
| FR-006 | T013 | WI3 exact owned filesystem authorization |
| FR-007 | T002–003,T008 | WI2/WI3 durable roots, stale/partial outcomes |
| FR-008 | T004–007,T012 | Both versions/backends mandatory each WI |
| FR-009 | T009–011 | Every phase schema and same-result output |
| FR-010 | T002–003,T009,T012 | WI2/WI3 cancellation and effects preserved |
| FR-011 | T012,T014–015 | Missing mandatory lane blocks every handoff |
| SC-001 | T004–012,T014 | Required native scenarios per WI |
| SC-002 | T004–007,T012,T014 | Effects/sentinels per WI |
| SC-003 | T009–011,T014 | Real result + oldschema regressions |
| SC-004 | T001,T014–015 | Separate analysis/review/evidence per WI |

All three WIs are implemented and independently reviewed; per-WI evidence distinguishes local acceptance, hosted CI and owner merge gates. WI1 is independently testable;
WI2 depends on accepted WI1; WI3 depends on accepted WI2 recovery/state contract.


## WI2 requirement traceability
FR001→T201–203/T208; FR002→T204–209; FR003→T205/T207/T213 (forge unsupported retains);
FR004→T204–208; FR005→T205/T207/T213; FR006→WI3, not enabled here; FR007→T202–209;
FR008→T204–207/T212; FR009→T210–211; FR010→T202–203/T208–212; FR011→T212/T214–215.
SC001→T204–212/T214; SC002→T202–209/T212; SC003→T210–211/T214; SC004→T201/T214–215.

## Issue #62: shared layout consumers

These tasks extend the implemented WIs and depend on
[Spec 010 T031–T035](../010-isolated-task-workspaces/tasks.md#issue-62-native-git-layout-readiness).
They are not completed by the historical WI task checkmarks. Independent amendment analysis
precedes source work; tests fail before implementing any consumer change.

- [x] T401 [FR-001, FR-004, FR-007, FR-012] Add native primary/linked submodule and separate-Git-directory reference-plan/recovery/delete tests in internal/workspace/tidy_git_test.go and tidy_git_apply_test.go. Keep checked-out branches and all workspace-used tips retained; verify exact recovery/selected-target deltas, packed-ref handling and preserved primary/sibling/index/config bytes. Changed layout or config, including after recovery creation, must refuse the next effect without deleting the recovery root.
- [x] T402 [FR-004, FR-006, FR-007, FR-012] Add owned lifecycle tests in internal/workspace/retirement_git_test.go: create from each admitted primary/linked layout, prove complete ownership and clean state, record recovery, then remove only the selected task registration/files. Protect source, primary, other registrations and superproject metadata. Retain dirty/unknown/foreign/current tasks; test target/source pointer replacement, config changes, cancellation/partial outcomes and verified completed retry. Registration-only Git removal stays unsupported.
- [x] T403 [FR-001, FR-005, FR-008, FR-012] Make only necessary consumer plumbing changes after failing tests; reuse the shared admission/fingerprint rather than introducing independent weaker proof. Rerun native jj local-bookmark recovery/deletion for standalone/colocated layouts on both versions, retaining tracked/untracked remote counterparts, conflict/base/recovery/workspace-used cases and source Git/WC state; no abandonment or remote reference effects.
- [ ] T404 [FR-009, FR-010, FR-011, FR-012, SC-001–SC-004] Extend the tidy/apply/retirement smoke manifests and tests with the new layout cases; independently review the exact integrated source, run required source checks and mandatory minimum/current native/schema acceptance, and record effects, versions, SHA and unrun gates. Any needed binary/documentation build requires separate owner authorization; historical baseline tests do not close this task.

FR-012 maps to T401–T404 plus Spec 010 T031–T038. T401/T402 depend on the reviewed shared
proof; T403 preserves native bookmark semantics, and T404 is the essential v0.1.0 acceptance
gate. No cleanup of real development or user repositories is part of these tests.

Issue #62 source evidence is shared with [Spec 010 verification](../010-isolated-task-workspaces/verification.md#issue-62-source-implementation).
Focused native layout fixtures live in `internal/workspace/git_layout_test.go` and
`git_layout_lifecycle_test.go`; the common proof is consumed without separate tidy or
retirement admission shortcuts. Exact-source review and the fresh CLI layout matrix
remain T404 gates; the new source checkmarks do not mark overall release acceptance.

## Narrow missing-directory registration prune (T012b)

New future work for FR-013–FR-015 and the
[narrow prune contract](contracts/narrow-registration-prune.md). Existing checkmarks remain
historical; none verifies this amendment. Source work starts only after this spec PR is
independently CLEAR and owner-merged. Every red test must fail on its intended assertion.

- [ ] T501 Resolve with the owner only the exact frozen-schema/version/selector/restore interface and stored-plan compatibility policy; independently analyze the complete contract, including its already defined absence predicate and recovery bounds. Depends on the merged spec PR.
- [ ] T502 Write failing planner/validation tests for one exact owned locked missing registration: precise final-component ENOENT, complete no-follow accessible ancestor chain, original creation DestinationParent path/device/inode match and stable parent/common-dir mount identities. Refuse EACCES/EPERM/ENOTDIR/EIO/ESTALE, missing/unreadable ancestors, mount/device changes, symlinks and unsupported observation; include moved-but-recoverable and unmounted lookalikes, without requiring a deletion witness or native prunable marker. Cover the entire contract refusal matrix: exact admin parent and gitdir/commondir pointers, bare refusal, reviewed initialized-submodule admission, exact `stw:<task-id>` lock, original record/HEAD/index bindings, unlocked prunable sibling blocking the whole plan, exact-name versus prefix siblings, same-name remove/re-add between plan/apply and occupied restore/replay slots. Preserve all protected state; test supported Git minimum (at least 2.29) and current native/platform limits. Depends on T501.
- [ ] T503 Implement only the narrow planner and approved strict records/interface; preserve all existing plan modes and schemas under the selected compatibility policy. Depends on T502 observed red.
- [ ] T504 Write failing apply/payload tests for complete binary metadata and digest-bound sidecars (64 MiB/file, 256 MiB total, 1,024 entries, 64 KiB streaming buffers), exact-boundary acceptance and overflow refusal, private modes/durable failures, fresh revalidation at each boundary, replacement races, partial removal/cancellation and verified replay; assert no broad prune/unlock, user files or unrelated refs change. Depends on T503.
- [ ] T505 Implement payload preparation, history pin, confined removal and durable exact outcomes; run minimum/current native Git and platform confinement fixtures. Depends on T504 observed red.
- [ ] T506 Write failing recovery tests for separate authorization, exact raw restoration, name/path collision, corrupt/missing payload/pin, changed ancestors/mounts, interruption, native verification failure and safe refusal without no-replace capability. Include moving a locked checkout, metadata-only prune, payload restore while the original path remains absent, separately authorized native `git worktree repair` of the found checkout and read-only status/staged-content verification. A found relocated path does not itself refuse restoration; an occupied original path does. Depends on T505.
- [ ] T507 Implement the approved inspect/restore path and honest partial/limit reporting; independently review the exact head and record native acceptance, schema regressions and no-Git-fallback jj/colocated sentinels. Depends on T506 observed red. Owner merge of the expert implementation precedes wizard source work.
- [ ] T508 Complete Spec 041 T012b's new test/implementation pairs, fixture-derived documentation and exact-head acceptance in its final PR; only that accepted final PR may update completion state. Depends on accepted T507 and the Spec 041 dependencies.
