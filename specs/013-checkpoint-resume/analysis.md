# Cross-artifact Analysis: Checkpoint, Resume and Selective Undo

**Date**: 2026-10-25. Author consistency analysis of the spec, plan, research,
model, three contracts, quickstart, tasks and checklist using the installed
Spec Kit analyze workflow. This is not an independent review or runtime evidence.
The analyzer phase is read-only; this requested durable report incorporates its
findings as documentation authoring. No product implementation was analyzed as
complete and no future task is marked done.

## Context and result

Existing-feature resolution used `SPECIFY_FEATURE_DIRECTORY=specs/013-checkpoint-resume`
and `SPECIFY_FEATURE_NO_PERSIST=1`; no new feature or persistent config was created.
Installed plan/task setup and the analysis prerequisite check resolved this
feature successfully. No `.specify/extensions.yml` exists, so no extension hook
was registered or run. Source facts are backed by the named `internal/stwsync`
and `internal/workspace` files. Adjacent 012/015 designs supply vocabulary and
operation boundaries, not evidence that their proposals or 013 are implemented.

The proposed artifacts cover all 14 FRs and 4 SCs individually. Five explicit
owner decisions remain; they block affected implementation, not this artifact
review. No deliberate constitution exception, duplicate requirement, missing
requirement/task coverage or unassigned task was found in this author audit.
Independent exact-head review remains a separate handoff gate.

## Findings and decision blockers

| ID | Category | Severity | Location | Finding and required next action |
| --- | --- | --- | --- | --- |
| D1 | Capability | HIGH | research.md OQ-1; plan.md phases 1–2 | Git/jj capture primitives, special-state admission and exact native lanes are unmeasured. Owner chooses the initial matrix after spikes; T001 blocks T013/T014 enablement. |
| D2 | Semantics | HIGH | research.md OQ-2; selective-undo contract | Native inverse/history/operation undo, descendant and signature policies require a choice and measured effects. Keep unsupported until T001; no broad jj fallback. |
| D3 | Retention | HIGH | research.md OQ-3; data model PayloadReceipt | Payload quota/drop admission remains an owner choice; external location and explicit owner-only modes are mandatory privacy guards. Keep automatic deletion disabled; sync's ten finished runs is not 013 cleanup authority. |
| D4 | Ownership | HIGH | research.md OQ-4; all apply guards | Trusted actor/task/ref/change ownership and sharedness evidence need an approved source. Unknown evidence refuses, never inferred from configuration or offline absence. |
| D5 | Integration | HIGH | research.md OQ-5; recovery contract | Sync's private v1 types are not a generic journal API. Characterize first and review a typed extraction with unchanged sync behavior; no duplicated writer. Stored restore-plan spelling and common flag admission also remain OQ-5 interface choices. |

No unresolved choice is presented as an approved default. Proposed numeric bounds
and deterministic privacy exclusions are design constraints pending implementation
review, with enforceable future tests. OQ-1 to OQ-5 are policy/capability blockers
with recommendations, not missing text to be filled silently.

## Individual requirement coverage

Every row contains explicit task ids and a literal future test idea. A task
mapping means planned coverage, never evidence that the test ran or passed.

| Requirement | Tasks | Literal test idea |
| --- | --- | --- |
| FR-001 | T001–T003, T010, T011 | Plan with no selections: assert zero saved bytes/native/store effects; explicitly select one index path: assert only that pair. Reject a checkpoint reader presented with a `type: sync` record; preserve sync characterization after seam extraction. Run sync retention with 013 checkpoint/undo roots and receipts present: assert all survive; collision/unowned-prefix mutations refuse. |
| FR-002 | T007, T012–T014 | Give one Git path distinct staged and unstaged bytes and verify separate receipts; give jj distinct stored commit/change/op ids and unsnapshotted bytes and assert no identity substitution or implicit snapshot. |
| FR-003 | T015, T016, T018 | Fail payload/file/directory/receipt durability before completion and assert checkpoint incomplete with retained evidence; delete a payload while retaining metadata and assert restore unavailable. With umask 0000 assert new directory/file modes 0700/0600; refuse in-repository/linked-worktree, foreign-owned and 0666 stores. Exceed checkpoint/total physical quota: refuse without eviction. Confirm drop of one checkpoint: remove only exclusive owned bytes/roots; crash mid-drop and verify exact remaining receipts/byte counts and zero content logs/output. Restore over unique current work, complete the restore, then drop its source checkpoint: assert all independent safety-copy bytes, records and roots survive. Delete them only through a separately named safety-id-confirmed plan; pending dependencies refuse. |
| FR-004 | T019–T022 | Resume a dirty owned checkpoint fixture and assert unchanged refs/index/disk/jj oplog/state store; authorize a separate restore then race selected disk bytes and assert refusal with no overwrite. |
| FR-005 | T008, T009 | Select an excluded `.env`/credential/transcript input and assert refusal; pass inert malicious task text and assert no environment/transcript discovery, execution or payload disclosure. |
| FR-006 | T017, T018, T021, T023, T024 | Crash after native effect before completion; inspect known/unknown actions and ensure no automatic rollback. Give only a jj operation id without saved disk bytes and assert no disk recovery promise. |
| FR-007 | T004, T005, T014, T033, T034 | Run jj colocation with native jj unavailable and a usable Git present: assert explicit refusal and zero raw Git writes; ambiguous/GitButler/foreign ownership also refuses. |
| FR-008 | T025, T026 | Render success and typed error in human/JSON with ANSI/bidirectional/non-UTF-8 path data; compare facts and reversible paths, assert no prompt/pager or TTY dependency, proposed exits 0/1/2 and unchanged 015 v1 incomplete exit 3. Verify exact admitted format/timeout/spelling choices, reject `--online` and redundant `--offline` as proposed 013 usage errors without changing 033/037, and require authorization plus plan id for effects. |
| FR-009 | T006, T007, T027, T028 | Exceed each count/byte/deadline cap and cancel between actions: assert incomplete/partial with completed effects retained and no next mutation; fake helpers/hooks/network record zero execution/requests. |
| FR-010 | T035, T040 | Remove one mandatory minimum/current native/backend/platform execution from the evidence matrix and assert acceptance remains blocked; include changed ownership, concurrency and interruption cases for each admitted capability. |
| FR-011 | T029, T030 | Plan explicit native commit/merge/operation targets and verify immutable exact target/effect/recovery bindings; mutate the target after plan and assert fresh expected-state refusal. |
| FR-012 | T031, T032 | Plan one target with inverse vs history restoration and assert distinct effects; omit or give a non-parent merge parent and assert refusal without first-parent guessing; unmet required signing refuses with zero signing-helper execution and explicit signature-loss limits. Assert Git inverse candidates use private plumbing quarantine and leave original objects/index/disk unchanged until the explicitly journaled promotion/CAS. |
| FR-013 | T022, T029, T030 | Introduce a foreign/shared/unknown target or unselected rewritten descendant and assert refusal; fail pre-action root/payload receipt and assert no target effect; substitute another feature's root and assert no update/deletion. Hold the inverse destination in the caller's clean workspace or a linked workspace, then add dirty selected paths or unreadable occupancy: assert changed-since refusal and unchanged index/disk/ref. |
| FR-014 | T033, T034 | Request selective jj undo where native operation restoration also affects an unselected bookmark/workspace: assert unsupported/refusal, no broad op undo and no Git fallback. Test later operations from another workspace, restore-view vs older-op inverse distinction, non-direct-parent and divergent heads, and a head race before/after apply; assert snapshot-suppressed/pinned argv, unchanged other workspaces, refusal/partial truth and unavailable after required op abandonment/GC. |
| SC-001 | T035, T036 | Execute each quickstart story row on every admitted native version/platform; unsupported cases produce typed limits, missing required execution remains an explicit blocker. |
| SC-002 | T027, T028, T037 | Compare whole-fixture sentinels after each safety/failure/crash case and fail a mutant that writes an unselected path, runs a hook or sends a request; verify exact partial/unknown effects. |
| SC-003 | T025, T026, T038 | Validate all complete future success/error fixtures offline and compare all existing published schema/fixture byte digests to baseline; reject malformed or success-shaped incomplete output. |
| SC-004 | T039, T040 | Remove one FR's native acceptance execution or mark an unrun case passed; require the coverage audit to flag the missing evidence before handoff. |

All task ids T001–T040 occur in the mapping. T001 is the shared decision gate;
T040 is the common independent review/check/evidence gate. Story tests and
implementation tasks intentionally pair under the same requirement.

## Constitution and cross-feature alignment

Principles I–V are represented by bounded shared output, explicit selected plans
and fresh guards, pre-action recovery/ownership, red-first native evidence, and
native Git/jj capability/refusal boundaries. Captured selected bytes are distinct
from retained committed roots and metadata. Resume comparison stays offline and
read-only; restore/undo are separate actions. No guarantee extends to untracked
or ignored bytes unless explicitly selected and admitted, or to unsaved buffers,
remote publication, arbitrary hooks or other external effects.

Spec 032 facts remain narrow: private sync v1 records, prepared/completed/failed
and handled-incomplete restored; durable preparation before branch effects,
known planned-new reconciliation vs unknown rebase journal-incomplete; expected-
new ref restoration and idle-clean-main reset path only there. Retention counts
ten finished runs (no prepared entries, so failed/restored may count), not ten
successful captures. It protects incomplete prepared records. None of its
retention or main-worktree behavior enables 013 deletion/reset.

012 stack observation, 014 isolated integration preflight/replay and 015 local
restack can supply optional sampled evidence with consumer revalidation.
[Spec 012 OQ-NET](../012-stack-inspection/spec.md) keeps online scope/flag naming
open; 013's offline boundary does not silently settle it. Preserve 015 D7 plumbing/CAS, D8 forge handoff and hidden
v1 exit 3. 013 proposes 0/1/2 only for its new protocol. 016 forge effects and
043 human-only landing remain separate; local undo cannot reverse publication.
041 mode evidence is an accident guard; 042 attribution is independent from
commit identity and never synthesized from configuration/session-name matches.
040 pure decisions/bounded edge effects and deterministic fanout remain the
concurrency rule if needed. No hooks/helpers/network are introduced here.

## Metrics and readiness

| Measure | Result |
| --- | --- |
| Functional requirements / success criteria | 14 / 4 |
| Individually task-and-test mapped requirements | 18 of 18 (100% planned coverage) |
| Future tasks | 40, all unchecked |
| Setup / foundations / US1 / US2 / US3 / US4 / final tasks | 1 / 8 / 9 / 6 / 4 / 6 / 6 |
| Unmapped tasks / uncovered requirements | 0 / 0 |
| Open owner decisions | 5, HIGH blockers for affected implementation |
| Author-detected constitutional conflicts / critical findings | 0 / 0 |
| Independent review / native product acceptance | pending / not executed |

Next action: independent documentation review, resolve
OQ-1 through OQ-5 with measured capability evidence, then execute only approved
slices red-first. Artifact completeness is reviewable; affected implementation
is not ready until its decisions are resolved. Publication and PR readiness are
controlled by the coordinator and exact-head independent review/green CI.
