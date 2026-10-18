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

- [ ] T401 [FR-001, FR-004, FR-007, FR-012] Add native primary/linked submodule and separate-Git-directory reference-plan/recovery/delete tests in internal/workspace/tidy_git_test.go and tidy_git_apply_test.go. Keep checked-out branches and all workspace-used tips retained; verify exact recovery/selected-target deltas, packed-ref handling and preserved primary/sibling/index/config bytes. Changed layout or config, including after recovery creation, must refuse the next effect without deleting the recovery root.
- [ ] T402 [FR-004, FR-006, FR-007, FR-012] Add owned lifecycle tests in internal/workspace/retirement_git_test.go: create from each admitted primary/linked layout, prove complete ownership and clean state, record recovery, then remove only the selected task registration/files. Protect source, primary, other registrations and superproject metadata. Retain dirty/unknown/foreign/current tasks; test target/source pointer replacement, config changes, cancellation/partial outcomes and verified completed retry. Registration-only Git removal stays unsupported.
- [ ] T403 [FR-001, FR-005, FR-008, FR-012] Make only necessary consumer plumbing changes after failing tests; reuse the shared admission/fingerprint rather than introducing independent weaker proof. Rerun native jj local-bookmark recovery/deletion for standalone/colocated layouts on both versions, retaining tracked/untracked remote counterparts, conflict/base/recovery/workspace-used cases and source Git/WC state; no abandonment or remote reference effects.
- [ ] T404 [FR-009, FR-010, FR-011, FR-012, SC-001–SC-004] Extend the tidy/apply/retirement smoke manifests and tests with the new layout cases; independently review the exact integrated source, run required source checks and mandatory minimum/current native/schema acceptance, and record effects, versions, SHA and unrun gates. Any needed binary/documentation build requires separate owner authorization; historical baseline tests do not close this task.

FR-012 maps to T401–T404 plus Spec 010 T031–T038. T401/T402 depend on the reviewed shared
proof; T403 preserves native bookmark semantics, and T404 is the essential v0.1.0 acceptance
gate. No cleanup of real development or user repositories is part of these tests.
