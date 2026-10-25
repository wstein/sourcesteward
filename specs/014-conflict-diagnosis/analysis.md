# Specification analysis report: Conflict diagnosis and integration preflight

**Date**: 2026-10-25 | **Scope**: complete feature artifacts. This is an author
consistency report, not independent review or implementation/conformance evidence.

## Analysis scope

Compared stories/FR/SC, design boundaries, semantic profiles, data fields, finite
limits, TDD dependencies and task paths. Checked constitution I–V and surrounding
Specs 011–016, 020, 022, 025, 030, 032, 040–043. Proposed neighboring seams and
contracts are dependencies, not claims that exported APIs already exist.

## Findings and owner decisions

| ID | Category | Severity | Locations | Finding | Recommendation/current gate |
| --- | --- | --- | --- | --- | --- |
| A1 | Underspecification retained | HIGH | spec.md OQ-SEM; contracts/preflight.md | Native merge strategy/base interpretation and empty/redundant replay treatment need an owner decision. | Recommend initial single-parent linear replay; affected merge/replay variants unavailable until accepted and measured (T001/T003/T004/T019/T020). |
| A2 | Capability scope retained | HIGH | spec.md OQ-TOPO; contracts/preflight.md | Rename/mode/gitlink, multiple-base, merge-commit and jj parent/descendant topologies are not globally proven. | Explicit measured capability allowlist; refuse every unaccepted shape (T003/T004/T015/T019/T023). |
| A3 | Privacy policy retained | HIGH | spec.md OQ-HUNK; contracts/context.md | Hunk locations/content and even filenames can reveal sensitive work; no comprehensive secret guarantee exists. | Body-free default; optional exact-path context stays disabled until accepted policy/conformance (T011–T014). |
| A4 | Isolation capability retained | HIGH | spec.md OQ-SCRATCH; plan.md | Native jj independent scratch store/oplog and platform write confinement need measured support. | No shared writable source store or raw Git fallback; unavailable enforcement refuses (T003–T006/T021–T024). |
| A6 | Native semantics retained | HIGH | spec.md OQ-JJCONF; data-model.md | Git stop-first semantics cannot describe native jj descendant conflict evolution or later resolution. | Keep jj preflight unavailable until accepted per-revision/aggregate semantics and complete bounded native graph evidence (T001/T003/T023/T024). |
| A7 | Flag alignment retained | LOW | spec.md OQ-FLAGS; contracts/cli.md | Shared human/timeout/offline flag admission is an unresolved interface choice. | Retain current text/JSON and fixed bounds; choose aliases/shortening override only after owner decision (T001/T025/T026). |
| A5 | Compatibility decision retained | MEDIUM | spec.md OQ-EXIT; contracts/cli.md | New 014 proposes 0/1/2 while hidden 015 v1 already returns 3 for incomplete. | Preserve 015 unchanged; any shared migration needs an explicit versioned decision (T001/T004/T027/T028). |

These are visible decision gates, not contradictions resolved by invention.
No constitution conflict or zero-coverage critical finding remains. Acceptance
of affected implementation variants is blocked until their decisions and measured
capabilities exist. Artifact review may proceed with those blockers explicit.

## Individual requirement coverage and literal future test ideas

Every FR and SC has its own row. Task association is design coverage, not a claim
that a test has been written or run. Each test description is a concrete case a
future implementation can execute in disposable fixtures.

| Requirement | Has task? | Task IDs | Literal test idea |
| --- | --- | --- | --- |
| FR-001 | Yes | T007–T010 | Build interrupted Git rebase and stored jj multi-sided conflict; assert distinct native state kinds/participant representations, never identical Git/jj operation labels. |
| FR-002 | Yes | T007–T010, T017–T018 | Remove Git stage-1 participant and provide unknown jj conflict term; assert explicit unknown, exact affected path, full known ids and source/freshness qualification. |
| FR-003 | Yes | T011–T012 | Put executable-looking resolver instructions in a conflicted blob; assert bounded inert handoff and a process recorder with no resolver/provider/suggested-command execution. |
| FR-004 | Yes | T007–T010, T021 | Diagnose interrupted operations and dirty jj disk; compare index/files/refs/oplog sentinels and assert zero stage/snapshot/continue/abort calls. |
| FR-005 | Yes | T011–T014, T027 | Use path bytes containing newline, ESC, bidi and invalid UTF-8 plus a secret body; assert byte-identical decoded path, escaped display and absent default body. |
| FR-006 | Yes | T003–T004, T009–T010, T023–T024 | Run in ordinary Git, colocated jj, missing jj and ambiguous/GitButler layouts; assert native selection or typed refusal and no raw Git fallback. |
| FR-007 | Yes | T025–T028 | Render the same complete conflict and admitted error as default text/--json with no TTY; compare identities/status/reasons, assert no stdin/pager/editor and frozen existing contract bytes. Pass current-draft --human/--timeout/--state-dir/--online/--offline and assert usage 2 before native reads/scratch effects. |
| FR-008 | Yes | T005–T006, T021–T022, T025 | Exhaust a process/result bound and cancel replay before step 2; assert exit 1/incomplete, explicit omissions and no clean full-range or authorization claim. |
| FR-009 | Yes | T003–T004, T029–T032 | Omit the minimum-jj conformance lane from the evidence matrix; assert the capability acceptance remains blocked rather than counted as a skipped pass. |
| FR-010 | Yes | T015–T020 | Diagnose an existing conflict, then preflight explicit merge and restack inputs; assert existing-native versus predicted-scratch origin and no semantics inference. |
| FR-011 | Yes | T005–T006, T015–T016, T021–T024 | Attempt a clean and conflicting replay while recording original tree/index/ref/object/oplog writes; assert only owned private scratch writes and denied source writes. |
| FR-012 | Yes | T009–T010, T021–T024 | Start from dirty jj disk and Git interrupted merge; assert no original snapshot/merge/abort, replace scratch handle before cleanup and assert replacement is retained. |
| FR-013 | Yes | T017–T018, T025–T027 | Move source ref after sampling a clean replay; assert unknown/incomplete and exact old/new samples; unchanged result carries fixed no-semantic/no-future-success notice. |
| FR-014 | Yes | T005–T006, T021–T024 | Set merge/filter/LFS/textconv/external-diff/hooks/fsmonitor config or attributes on either source tree, move .gitattributes and supply hostile jj user config; assert repository-hazard plus mandatory scrub/pins/refusal and no helpers or git status/index refresh in diagnosis. Remove confinement or fail cleanup and assert typed refusal/residue without fallback. |
| FR-015 | Yes | T011–T012, T025–T026 | Invoke the group with no leaf in an authenticated environment; assert help, zero native reads/scratch/auth/network calls and no prompt or implicit context refresh. |
| FR-016 | Yes | T015–T020, T023 | Omit old-parent from replay and supply an unaccepted multi-base merge; assert usage 2 for missing selector and capability refusal for topology, never guessed mainline/replay substitution; shared Git refusals use the 015 codes in the diagnostic mapping table. |
| FR-017 | Yes | T015–T016, T021 | Replay P..H containing two commits onto N with conflict in the first; assert original-parent merge base, first-conflict path and second commit not-evaluated, zero porcelain and source object writes. |
| FR-018 | Yes | T017–T018, T023 | Change strategy/options, selected ref target or jj operation between identical-looking requests; assert binding mismatch/unknown and distinct full commit/change/operation ids. |
| FR-019 | Yes | T011–T014 | Default handoff includes a secret-bearing unselected file; assert no body. Accepted selected hunk returns exact origin/span/limits; binary/sensitive-policy/unknown terms carry omission reasons. |
| FR-020 | Yes | T005–T006, T021–T022, T025–T028 | Supply 201 commits, 2,001 paths, 33 hunks and failed cleanup in separate fixtures; assert named bound/incomplete/residue, never silent complete truncation or predicted-clean success. |
| FR-021 | Yes | T003–T004, T009–T010, T023–T024 | Try jj preview with source and scratch sharing an operation store; assert capability-unavailable before replay and unchanged stored working copy/oplog, including colocation; also require unavailable native aggregate semantics to refuse without fake Git suffix or tip-only clean proof. |
| FR-022 | Yes | T004, T016–T018, T027 | Pass a preview with changed destination/profile to a fake consumer gate; assert rejection and no imported scratch object/recovery root/journal or apply/push/review authorization. |
| SC-001 | Yes | T003–T004, T029 | Execute each declared scenario for every enabled minimum/current native lane; unsupported shape yields explicit capability reason and a missing mandatory lane blocks acceptance. |
| SC-002 | Yes | T021–T022, T029–T030 | Inject native failure, cancellation and race while recorders watch hooks/helpers/network/source writes; assert zero undeclared effects and truthful incomplete/partial outcome. |
| SC-003 | Yes | T027–T029 | Validate future complete/incomplete/error JSON offline; compare all existing published/hidden schema/fixture digests and exercise hidden restack-plan incomplete exit 3 unchanged. |
| SC-004 | Yes | T001–T002, T031–T032 | Enumerate 22 FR plus 8 SC and require a task plus literal test idea per id; delete one mapping or native execution record and assert coverage/acceptance audit reports the gap. |
| SC-005 | Yes | T015–T016, T019–T020, T029 | Build clean and first-conflict two-commit Git replay plus accepted/unaccepted merge fixtures; assert exact failing commit/path/suffix and no unaccepted strategy fallback. |
| SC-006 | Yes | T005–T006, T021–T024, T029–T030 | Snapshot original tree/index/HEAD/refs/config/objects/jj state before clean/conflicted/cancelled runs and assert zero source writes plus confined scratch and visible cleanup failures. |
| SC-007 | Yes | T011–T014, T027, T030 | Place distinct sentinels in environment, default body and unselected blob; assert none appear; accepted selected hunks obey every byte/count cap and escaped display preserves path identity. |
| SC-008 | Yes | T017–T018, T025–T027, T030 | Feed predicted-clean with stale PR context, changed ref, truncated output and cancellation into separate cases; assert fixed limitation, exact binding/unknown outcome and no Ready/verified/mutation permission. |

## Task coverage and dependencies

T001/T002 resolve decisions/design for all stories; T003–T006 establish capability,
pure typed limits and effect enforcement. T007–T010 cover US1 (FR-001/002/004/006/021).
T011–T014 cover US2 (FR-003/005/015/019). T015–T024 cover US4
(FR-010–014/016–018/020–022). T025–T028 cover US3
(FR-007/008/015/020 and compatibility). T029–T032 cover FR-009 and SC-001–008.
No task is unmapped. Ranges here summarize tasks only; requirement coverage is
individually enumerated above, never hidden behind a grouped FR/SC row.

Every behavior has a red-test-before-implementation pair; native measurements
precede profile enablement. Shared scratch/CLI/neutral replay extraction serialize.
Independent Git/jj metadata tests can run in disjoint files; independent runtime
reads use only Spec 040 fanout with bounded indexed results. Git replay remains
sequential, native jj graph effects remain OQ-JJCONF-gated, and cleanup follows
child reaping. Future schema creation/publication
is outside this Markdown amendment and cannot amend frozen v1 implicitly.

## Constitution alignment

I: shared result/noninteractive bounded output. II: exact evidence binding and no
consequential action by this feature. III: preserve all originals, strict scratch
ownership, no invented recovery promise. IV: reviewed artifacts, red tests,
native/effect/mutation evidence and honest missing coverage. V: native ownership,
Git/jj capability-specific conformance, distinct identities and no fallback.
No exception/amendment proposed; OQ gates preserve these requirements.

## Metrics and current verification

- Requirements: 22 FR + 8 SC = 30 individually mapped; coverage 30/30 (100% design mapping).
- Tasks: 32, all unchecked; setup 2, foundation 4, US1 4, US2 4, US4 10, US3 4, final 4.
- Unmapped requirements/tasks: 0/0. Duplication findings: 0. Critical findings: 0.
- Retained decisions: 7 (5 HIGH capability/privacy/native-semantics findings, 1 MEDIUM compatibility finding, 1 LOW flag-alignment finding).
- Independent review: pending exact-current-head review; this report is author analysis only.
- Product/conformance/mutation tests, builds and full make check: not run; not claimed passing.

## Next actions

Resolve the affected owner decisions and measure their native capabilities before
implementation; obtain independent exact-head artifact review. Then execute the
accepted unit's red-first tasks and record actual effects/conformance evidence.
Publication gates belong to Spec 016; this feature supplies evidence, not
authorization or a publication-policy implementation.
