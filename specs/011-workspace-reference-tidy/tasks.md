# Tasks: Workspace and Reference Tidy

Three sequential implementation WIs; each gets plan → tasks → independent analyze → failing tests
→ implementation → independent review/verification → focused commits → PR.

- [WI1 plan](wi1-plan.md) and [WI1 tasks](wi1-tasks.md): evidence-only plans, US1/US3.
- WI2: local reference cleanup, US2/US3; separate plan/tasks follow before its implementation.
- WI3: owned retirement, US1/US2/US3; separate plan/tasks follow before its implementation.

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

This overview does not claim later WIs implemented or analyzed. WI1 is independently testable;
WI2 depends on accepted WI1; WI3 depends on accepted WI2 recovery/state contract.
