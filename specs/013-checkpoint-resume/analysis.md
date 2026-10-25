# Current Owner-Decision Analysis: Spec 013

**Owner decisions: 2026-10-25. Author cross-artifact analysis only.** All former
OQ IDs are settled and indexed in [the decision index](../016-stacked-pr-coordination/research.md#cross-spec-decision-index).
The [concrete profiles](../016-stacked-pr-coordination/contracts/owner-decisions.md) distinguish workspace .work metadata from
external capture payloads, trusted policy, host-only optional local 015 re-sign
and CLI exit-contract revision 2. No new implementation/native/helper/forge test
or independent CLEAR is claimed. Historical implementation evidence stays at
its recorded revision; current hidden 015 code still returns 3 until the new task.

## Current findings and gates

| ID | Disposition | Remaining evidence |
| --- | --- | --- |
| D1 | All 39 dispositions are recorded as direct owner answers or adopted recommendations; cross-spec owner overrides are marked, including --online-only/single-repo and default .work saved-plan metadata. | Exact current artifact review and source/native conformance; no invented native acceptance. |
| D2 | Metadata and external selected-byte/safety stores are distinct roles with explicit modes/race/layout/quota/export-risk guards. | Red-first shared store and role-separation tasks; no auto-ignore edits/expiry. |
| D3 | Optional actual re-sign belongs only to 015 fixed trusted HOST helper/verifier; no stw keys, no helper means unavailable. | Measured profile/external-agent trust limits, bottom-up signed parents and final verified SHA before review; 016 receipt validation only. |
| D4 | Target CLI exit-contract revision 2 is 0/1/2, no legacy 3 selector; JSON schema 1 frozen independently. | Red dispatch/help/JSON cases and root.go/restack.go mapping task; CURRENT code 3 not misreported changed. |

## Individual requirement coverage and literal future tests

Each current definition has one row, real task IDs and a literal future test.
Coverage is planned design mapping; unchecked tasks are not execution evidence.

| Requirement | Task IDs | Literal future test idea |
| --- | --- | --- |
| FR-001 | T002, T010, T041, T042 | Plan with no selections: assert zero saved bytes/native/store effects; explicitly select one index path: assert only that pair. Reject a checkpoint reader presented with a `type: sync` record; preserve sync characterization after seam extraction. Run sync retention with 013 checkpoint/undo roots and receipts present: assert all survive; collision/unowned-prefix mutations refuse. |
| FR-002 | T012 | Give one Git path distinct staged and unstaged bytes and verify separate receipts; give jj distinct stored commit/change/op ids and unsnapshotted bytes and assert no identity substitution or implicit snapshot. |
| FR-003 | T015, T041, T042 | Fail payload/file/directory/receipt durability before completion and assert checkpoint incomplete with retained evidence; delete a payload while retaining metadata and assert restore unavailable. With umask 0000 assert new directory/file modes 0700/0600; refuse in-repository/linked-worktree, foreign-owned and 0666 stores. Exceed checkpoint/total physical quota: refuse without eviction. Confirm drop of one checkpoint: remove only exclusive owned bytes/roots; crash mid-drop and verify exact remaining receipts/byte counts and zero content logs/output. Restore over unique current work, complete the restore, then drop its source checkpoint: assert all independent safety-copy bytes, records and roots survive. Delete them only through a separately named safety-id-confirmed plan; pending dependencies refuse. |
| FR-004 | T019, T021 | Resume a dirty owned checkpoint fixture and assert unchanged refs/index/disk/jj oplog/state store; authorize a separate restore then race selected disk bytes and assert refusal with no overwrite. |
| FR-005 | T008, T041, T042 | Select an excluded `.env`/credential/transcript input and assert refusal; pass inert malicious task text and assert no environment/transcript discovery, execution or payload disclosure. |
| FR-006 | T017, T021, T023 | Crash after native effect before completion; inspect known/unknown actions and ensure no automatic rollback. Give only a jj operation id without saved disk bytes and assert no disk recovery promise. |
| FR-007 | T004 | Run jj colocation with native jj unavailable and a usable Git present: assert explicit refusal and zero raw Git writes; ambiguous/GitButler/foreign ownership also refuses. |
| FR-008 | T025 | Render success and typed error in human/JSON with ANSI/bidirectional/non-UTF-8 path data; compare facts and reversible paths, assert no prompt/pager or TTY dependency, proposed exits 0/1/2 and CURRENT 015 code 3 recorded separately from decided revision 2 incomplete 1. Verify exact admitted format/timeout/spelling choices, reject `--online` and redundant `--offline` as proposed 013 usage errors without changing 033/037, and require authorization plus plan id for effects. |
| FR-009 | T006, T027 | Exceed each count/byte/deadline cap and cancel between actions: assert incomplete/partial with completed effects retained and no next mutation; fake helpers/hooks/network record zero execution/requests. |
| FR-010 | T035 | Remove one mandatory minimum/current native/backend/platform execution from the evidence matrix and assert acceptance remains blocked; include changed ownership, concurrency and interruption cases for each admitted capability. |
| FR-011 | T029 | Plan explicit native commit/merge/operation targets and verify immutable exact target/effect/recovery bindings; mutate the target after plan and assert fresh expected-state refusal. |
| FR-012 | T031 | Plan one target with inverse vs history restoration and assert distinct effects; omit or give a non-parent merge parent and assert refusal without first-parent guessing; unmet required signing refuses with zero signing-helper execution and explicit signature-loss limits. Assert Git inverse candidates use private plumbing quarantine and leave original objects/index/disk unchanged until the explicitly journaled promotion/CAS. |
| FR-013 | T029 | Introduce a foreign/shared/unknown target or unselected rewritten descendant and assert refusal; fail pre-action root/payload receipt and assert no target effect; substitute another feature's root and assert no update/deletion. Hold the inverse destination in the caller's clean workspace or a linked workspace, then add dirty selected paths or unreadable occupancy: assert changed-since refusal and unchanged index/disk/ref. |
| FR-014 | T033 | Request selective jj undo where native operation restoration also affects an unselected bookmark/workspace: assert unsupported/refusal, no broad op undo and no Git fallback. Test later operations from another workspace, restore-view vs older-op inverse distinction, non-direct-parent and divergent heads, and a head race before/after apply; assert snapshot-suppressed/pinned argv, unchanged all-workspace working-copy maps including the invoker, refusal/partial truth and unavailable after required op abandonment/GC. A desired view changing only the invoking working-copy commit must refuse without snapshot/disk/update-stale effects; stale/unknown baseline refuses, and any future stale/update capability needs separate safety capture. Inject a native operation between pre-check and pinned mutation: report observed siblings as partial/changed-since and initiate no merge. Exercise post-check failure and already-integrated/ABA samples; assert mandatory no-head-CAS, missed-race and external-auto-integration limitations, never prevention/always-detect wording. |
| FR-015 | T041, T042 | Render unsaved by default and save only with explicit --save to admitted ignored/untracked .work while capture bytes require external payload-dir; reject inside/aliased/raced payload, preserve safety copies and no-auto-expiry. |
| SC-001 | T036 | Execute each quickstart story row on every admitted native version/platform; unsupported cases produce typed limits, missing required execution remains an explicit blocker. |
| SC-002 | T037, T041, T042 | Compare whole-fixture sentinels after each safety/failure/crash case and fail a mutant that writes an unselected path, runs a hook or sends a request; verify exact partial/unknown effects. |
| SC-003 | T038 | Validate all complete future success/error fixtures offline and compare all existing published schema/fixture byte digests to baseline; reject malformed or success-shaped incomplete output. |
| SC-004 | T039 | Remove one FR's native acceptance execution or mark an unrun case passed; require the coverage audit to flag the missing evidence before handoff. |

## Metrics and task state

19 individual FR/SC definitions, 42 task ids, 0 historical
checked tasks; 42 unchecked. Coverage 19/19 (100% design
mapping), zero requirement orphans. No implementation checkbox is changed by
owner answers or this analysis. All new exit/store/signing tasks are unchecked.
Task dependencies remain red test → implementation → native/effect conformance
→ exact-source independent review. Spec 043 human-only landing remains separate.

## Compatibility and acceptance boundary

012 online means only --online; old alias/batch unavailable now. 013 payloads
and safety copies stay external; .work only holds private minimal metadata with
explicit export risk. 014 preflight never signs or mutates originals. 015 D7/D8
and unsigned D9 prediction stay settled; optional actual signing needs the host
profile and final signed-SHA review. 016 remote flags cannot invoke that local
helper. 043 identity authority is trusted workspace policy, global config only
corroborates, with existing human confirmation/shape/retarget/deletion gates.
No build, installation, helper run, schema-file edit or live write is claimed.

## Historical evidence preservation

The [complete pre-owner-answer analysis](analysis-before-owner-decisions.md) is
retained as a dated snapshot, including executed independent-review provenance,
old mappings and recorded limitations. Its old current/pending wording is
historical only and does not override the owner-decision target above.
