# Specification Analysis: Stacked PR Coordination

**Date**: 2026-10-25 | **Scope**: Author cross-artifact analysis of specification,
plan, research, model, contracts, quickstart and tasks. This is not independent
CLEAR, implementation acceptance or live forge evidence.

## Findings and disposition

| ID | Category/severity | Location | Assessment and disposition |
| --- | --- | --- | --- |
| A1 | Decision / blocker | research OQ-SURFACE/POLICY/WRITES/NATIVE/SIGN/STORE/EXIT | Seven explicit owner-decision groups remain; each blocks its affected capability, with recommendations rather than invented answers. Offline evidence consumption can proceed separately. |
| A2 | Compatibility / addressed | spec FR-006/014, contracts | Old no-force first slice amended only for owned-topic full observed-SHA lease; all default/tag/merge/delete/settings prohibitions retained. Hidden 015 v1 bytes and exit 3 remain frozen. |
| A3 | Safety / addressed | receipts, T017–T019/T029–T031 | Separate docs/code before-push matrix from unconditional exact-current-head Ready gates; compute class/full parent-inclusive identity range and use actual candidate SHA. Signing remains gated. |
| A4 | Cross-spec / addressed | spec boundary, research, receipts | 012 observation is advisory, 014 is isolated prediction, 015 is local plumbing/CAS, 013/032 local recovery, 016 selected forge effects, 043 separate human landing/deletion. Native rewrites and conditional 043 order remain qualified. |
| A5 | Traceability / addressed | tables below and tasks | Each FR/SC has individual task tags and a literal test idea. All tasks are future unchecked work, not executable acceptance evidence. |

Standalone comment execution is deferred original-draft scope under OQ-WRITES;
initial agent PR text writes are explicitly own-body edits only. 012 depth advice
does not block observation; 016 separately gates agent writes above two levels.

No intended constitution exception, uncovered FR/SC or unassigned task was found
in this author analysis. Unresolved trust/transport/signing/storage/native choices
are explicit hard gates for affected writes, not claims that implementation may
select a policy on its own. Independent exact-head artifact review is still needed.

## Individual requirement coverage

Every task id below exists in tasks.md with the matching literal requirement tag.
Every tagged task is included here (bidirectional mapping); none is checked.

| Requirement | Task ids | Literal future test idea |
| --- | --- | --- |
| FR-001 | T013, T014 | Same branch name with different head repository/SHA never verifies a PR relationship. |
| FR-002 | T013, T014 | Offline supplied PR URLs/evidence cause zero auth, HTTP, fetch and cache calls. |
| FR-003 | T006, T008, T010, T016, T022 | Empty selection has zero actions; each selected effect exposes exact target and blocker. |
| FR-004 | T009, T010, T019, T031 | Move head/base or revoke permission before action two; action two refuses before its intent/write. |
| FR-005 | T025, T026, T027 | Lose response after success; reconcile once without repeating or claiming local restore undoes it. |
| FR-006 | T017, T019 | Try main, tags, delete, force, bare lease, merge and auto-merge; zero forbidden vectors/requests. |
| FR-007 | T004, T005, T024, T034 | Native Git publication and native jj observation have distinct capability results; no forged parity. |
| FR-008 | T004, T005, T024, T034 | Colocated jj publication refuses without Git push; commit/change/operation ids remain distinct. |
| FR-009 | T008, T033 | Render identical typed refusal/partial evidence in human and JSON; freeze all legacy schema bytes. |
| FR-010 | T007, T008, T027 | Exhaust each request/record/body/result/deadline cap and cancel; no complete-success or next effect. |
| FR-011 | T001, T004, T035, T036, T038 | Omit one mandatory minimum/current native lane; acceptance remains blocked, not passed/skipped green. |
| FR-012 | T006, T008, T009, T010 | Authorize one action with wrong plan or local authorization only; no remote write occurs. |
| FR-013 | T011, T012, T022, T023 | Foreign task with matching branch prefix/author or raced body ownership refuses own-PR/body action. |
| FR-014 | T017, T019 | Race remote old SHA after sampling; full ref/full old/full actual new lease rejects without retry widening. |
| FR-015 | T017, T018, T036 | Docs/code/unknown and equal/non-equal range fixtures enforce each tier; predicted SHA never reviewed. |
| FR-016 | T017, T018, T036 | Owner-only child above foreign-committer open parent fails full default..new-head identity gate. |
| FR-017 | T002, T017, T018, T036 | Signed source has equal range-diff but signature-dropped; unresolved/required-signature policy blocks. |
| FR-018 | T029, T030, T031, T036 | Remove each current-head CLEAR/required-green/true-rebaseable/accepted-scope fact; Ready refuses. |
| FR-019 | T015, T016, T024 | Pending native rewrite blocks competing push; link reference has GH_REPO/numeric order/base and zero launch. |
| FR-020 | T022, T023 | Retarget child then change head; partial head-changed is reported and old Ready receipt is invalid. |
| FR-021 | T002, T003, T020, T021 | Current GET mode rejects every proposed write; new admitted mode rejects arbitrary method/path/body/URL. |
| FR-022 | T002, T006, T008, T025, T026 | Fail durable intent before remote write; zero writes; fail outcome persistence after success yields unknown. |
| FR-023 | T002, T033, T037 | Accepted future shared flag fixtures preserve current surfaces and hidden 015 incomplete exit 3. |
| FR-024 | T013, T014, T028, T037 | Local preflight/apply receipt alone cannot start sync/push/link/checkpoint/Ready or authenticate. |
| FR-025 | T032, T037 | Render readable-off and on/unreadable owner orders; zero landing/default push/delete calls. |
| FR-026 | T028, T032, T037 | Resync uses full old pushed SHA, actual new SHA and labelled destructive reset path; never executes. |
| FR-027 | T003, T007, T014 | Reverse independent read completion; output remains input-index ordered under one shared budget. |
| FR-028 | T011, T012, T031 | Exercise every D5 row; no recorded session remains ungated, current absent with recorded refuses, Codex never guessed. |
| FR-029 | T015, T016 | Three open levels block 016 writes while 012 observation remains advisory; unrelated docs target main. |
| FR-030 | T003, T020, T021, T033 | Hostile controls/body/URL/helper/session/token sentinels remain escaped/bounded/unexecuted and absent publicly. |
| SC-001 | T001, T004, T005, T024, T034, T035, T038 | Run each accepted native/backend/version/platform fixture and leave one unavailable lane explicitly blocked. |
| SC-002 | T003, T005, T006, T007, T009, T010, T011, T012, T013, T014, T015, T016, T017, T019, T020, T021, T022, T023, T024, T025, T026, T027, T028, T031, T032, T034, T035, T036 | Compare native/config/remote sentinels and recorder logs for forbidden writes/helpers/ownership bypass. |
| SC-003 | T001, T008, T033, T034 | Validate complete/error/partial fixtures offline, reject invalid binding and compare frozen legacy bytes. |
| SC-004 | T001, T002, T006, T037, T038 | Audit every FR/SC row against real unchecked task tags and a literal behavior test idea. |
| SC-005 | T017, T018, T019, T035, T036 | Mutation bypasses full-parent identity, actual-SHA review tier or signature evidence and each mutant fails. |
| SC-006 | T022, T023, T025, T026, T027, T028, T036 | Crash at every intent/write/outcome point; unknown completion produces no duplicate retry effect. |
| SC-007 | T029, T030, T031, T032, T036 | Ready matrix fails one gate at a time; both conditional landing handoffs are data only. |
| SC-008 | T003, T007, T008, T011, T012, T014, T020, T021, T027, T033, T036 | Exceed every cap, reverse read completion and inject secrets/session/control bytes; bounded ordered escaped output. |

## Story and task audit

| Task group | Story/role | Independent acceptance |
| --- | --- | --- |
| T001–T005 | Setup/decisions/native gap | Current contracts characterized, explicit OQs and no capability fabricated |
| T006–T012 | Foundations | Canonical plan, bounded data, per-action guards and real mode/ownership/D5 cases |
| T013–T016 | US1 | Offline verified relationship/empty-selection MVP, native/dependency gates |
| T017–T024 | US2 | Exact admitted own-topic/own-PR effects and full publication receipt policy |
| T025–T028 | US3 | Durable intent/outcome, unknown retry and inert resync data |
| T029–T032 | US4 | Fresh Ready matrix, conditional owner landing data without execution |
| T033–T036 | US5 | Shared bounded native human/JSON conformance and independent executed review |
| T037–T038 | Final acceptance | Accurate durable capability status and complete task/evidence audit |

All 38 tasks are mapped; 30/30 FRs and 8/8 SCs have task coverage and a literal
test idea (100% design traceability, **0 implemented tasks** in this new feature).
There are seven owner-decision groups, zero claimed native acceptance results
and zero independent-review conclusions. T025–T027 precede enabling any US2
write despite story organization; T029/T030 precede Ready. Parallel markers and
examples name file-disjoint work only. Authoring a future test is not passing it.

## Constitution and compatibility assessment

Human/structured parity, expected-state plans, ownership, recovery limits, TDD,
native capability gaps and no raw Git fallback are explicit. Existing transport
is GET-only; writes remain disabled until an additive reviewed closed allowlist
and accepted measurements. No endpoint/native effect is fabricated, and no
repository/helper/provider execution is imported through generic forwarding.
Actual candidate signing/identity and exact-head review cannot be replaced by
prediction equality. Session ids remain only in existing 042 private task state,
never new public or journal evidence; D5 no-session behavior is preserved.

Future 016 0/1/2 does not migrate hidden 015 v1 exit 3. JSON blocks are unpublished
illustrations; this artifact set adds no JSON/schema/code/generated file. 012's
network scope/name and shared human/timeout admission remain open. 013/014/015
new design files in sibling changes are not assumed present on main: cross-spec
links here target existing specifications, while this feature's own files link
within this self-contained set.

## Verification and next bounded step

Documentation/whitespace/typo and Spec Kit prerequisite checks are author source
checks only; their actual executed results belong in the review handoff. No Go,
product/site build, native conformance, installation, live API write, merge,
tag, release or default-branch push is established by this analysis.

Resolve each affected owner decision, obtain independent review of the exact
artifact head, then implement accepted capabilities test-first using tasks.md.
Keep unavailable capabilities blocked and record measured source/native evidence
before updating implementation checkboxes. Ready for a future PR is independently
gated by its exact head, required CI, rebaseability and accepted scope.
