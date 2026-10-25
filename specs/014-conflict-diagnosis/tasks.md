# Tasks: Conflict diagnosis and integration preflight

> **Owner-decision amendment — 2026-10-25:** all former OQ IDs are decided;
> [current profiles](../016-stacked-pr-coordination/contracts/owner-decisions.md) supersede earlier undecided recommendations.
> Shared human/shortening-timeout flags adopted; preflight remains offline/helper-free.
> This is a documentation target, not implementation or native acceptance.

**Input**: [spec](spec.md), [plan](plan.md), [research](research.md),
[data-model](data-model.md), [contracts](contracts/cli.md).
**Status**: proposed future work; every task is unchecked. No implementation,
product test, build or native acceptance was performed in this artifact refresh.

Tests are required by constitution IV and the owner TDD instruction. Each behavior
pair starts with a failing test, records the expected failure, implements the
smallest accepted behavior, then refactors with the tests green. Do not implement
an OQ-gated variant before its decision and measurements. Each capability can
remain explicitly unavailable while other accepted units proceed.

## Phase 1 — decisions and design gate

- [ ] T001 Verify/encode the dated decisions for OQ-SEM/TOPO/HUNK/SCRATCH/JJCONF/FLAGS/EXIT and their affected capabilities in specs/014-conflict-diagnosis/research.md; adopted recommendations require native evidence; Spec 015 D7 remains and exit revision 2 targets incomplete 1 (current code 3). (Coverage: SC-004).
- [ ] T002 Obtain independent complete-artifact exact-head design review and reconcile accepted contract/coverage findings in specs/014-conflict-diagnosis/analysis.md before implementation. (Coverage: SC-004).

## Phase 2 — shared foundation

- [ ] T003 Write failing capability/conformance probes in internal/conflict/capability_test.go for minimum/current Git/jj on supported Linux/macOS, no original writes/snapshots, no fallback and unavailable confinement; run only in disposable fixtures under later implementation authorization. (Coverage: FR-006, FR-009, FR-021, SC-001).
- [ ] T004 Execute the accepted native capability study, record exact versions/semantic profiles/platform limits and unmeasured refusals in specs/014-conflict-diagnosis/research.md; characterize internal/restack/replay.go and hidden v1 results before any proposed neutral-seam extraction. (Coverage: FR-006, FR-009, FR-021, FR-022, SC-001).
- [ ] T005 Write failing typed-boundary and scratch-admission tests in internal/conflict/boundary_test.go for original write/network denial, source/scratch replacement, path byte round-trip, status derivation and bound exhaustion; constants are “at most 200 moved commits”, “at most 2,000 paths”, “1 MiB final result”, “60 seconds total”, “10 seconds per native process”, “512 MiB scratch writes”, “at most 32 hunks”, “4 KiB per hunk”, “64 KiB total hunk content” and “at most 100 diagnostics”. (Coverage: FR-008, FR-011, FR-014, FR-020, SC-006).
- [ ] T006 Implement pure typed results/derivation in internal/conflict/result.go and internal/conflict/derive.go plus the reviewed effect shell in internal/conflict/scratch.go; enforce T005 bounds, 8 MiB process output, 16 KiB path and 2 KiB diagnostic bounds, create-only 0700 ownership and source read-only confinement; optional independent reads use only Spec 040 internal/fanout with width at most four and indexed results. (Coverage: FR-008, FR-011, FR-014, FR-020, SC-006).

## Phase 3 — US1 existing conflict diagnosis (P1, MVP)

**Independent test:** Git interrupted merge/rebase and native jj stored conflict
fixtures report their distinct participants and preserve all original state.

- [ ] T007 [P] [US1] Write failing Git interrupted merge/rebase tests in internal/conflict/observe_git_test.go for unmerged stage absence, text/binary/rename-delete/directory-file kinds, malformed metadata and non-conflict marker text; assert no staging, resolution, continue/abort, git status/index refresh, fsmonitor or helper execution under the binding sanitized runner. (Coverage: FR-001, FR-002, FR-004).
- [ ] T008 [US1] Implement bounded fixed native Git conflict observation in internal/conflict/observe_git.go; unknown participants/types stay explicit and disk is never represented as stored index evidence. (Coverage: FR-001, FR-002, FR-004).
- [ ] T009 [P] [US1] Write failing native jj tests in internal/conflict/observe_jj_test.go for selected/non-current conflicted commits, stored working-copy selection, multi-sided terms, operation change, colocation/secondary workspace, unsupported version, isolated HOME/XDG/JJ_CONFIG, hostile user-config merge tools and no-snapshot enforcement. (Coverage: FR-001, FR-002, FR-004, FR-006, FR-012, FR-021).
- [ ] T010 [US1] Implement accepted native jj stored conflict observation in internal/conflict/observe_jj.go with pinned full commit/change/operation ids; unsupported capability and ambiguous/GitButler ownership refuse with no Git fallback. (Coverage: FR-001, FR-002, FR-004, FR-006, FR-012, FR-021).

## Phase 4 — US2 bounded resolver handoff (P2)

**Independent test:** hostile content remains data, default handoff emits no body,
and optional accepted context contains only explicitly selected bounded evidence.

- [ ] T011 [US2] Write failing body-free handoff tests in internal/conflict/handoff_test.go for secret/environment/transcript sentinels, control/bidi/non-UTF-8 paths, unknown native terms and inert explanatory next steps; no model/provider/prompt/network/helper is invoked. (Coverage: FR-003, FR-005, FR-015, FR-019, SC-007).
- [ ] T012 [US2] Implement default minimal handoff values in internal/conflict/handoff.go using byte-preserving paths, sanitized displays, bounded participant/type metadata and no body/config/environment dumps. (Coverage: FR-003, FR-005, FR-015, FR-019, SC-007).
- [ ] T013 [US2] After OQ-HUNK acceptance, write failing selected-context tests in internal/conflict/context_test.go for explicit path scope, stored/disk/scratch origin, reliable/unknown ranges, binary/sensitive-policy omissions, disk race and “at most 32 hunks”, “4 KiB per hunk”, “64 KiB total hunk content”; otherwise test capability-unavailable. (Coverage: FR-005, FR-019, SC-007).
- [ ] T014 [US2] Implement only the accepted context capability in internal/conflict/context.go with exact selected-path reads, no symlink following, byte/line bounds, omission/freshness reasons and no claim of complete secret detection or marker-based resolution. (Coverage: FR-005, FR-019, SC-007).

## Phase 5 — US4 isolated preflight/replay (P1)

**Independent test:** explicit disposable restack/rebase/accepted merge fixtures
predict their declared native semantics without original writes; unsupported
profiles refuse and cleanup/race/cancellation remains visible.

- [ ] T015 [US4] Write failing Git linear replay tests in internal/conflict/replay_git_test.go for explicit old-parent..source-head onto destination, two-commit clean replay, first conflict with not-evaluated suffix, missing objects, empty/redundant/merge commits and malicious merge attributes, LFS/filter attributes, moved .gitattributes and missing source-tree attribute evidence; assert binding 015 environment/config pins and hazard refusals before replay, no porcelain or original object/ref write. (Coverage: FR-010, FR-011, FR-016, FR-017, SC-005).
- [ ] T033 [US4] Before T016 implementation, write and observe failing sampled-attribute regression/conformance tests in internal/conflict/attribute_source_test.go for untracked, modified and index-only .gitattributes containing f merge=union that would falsely clean a real conflict; require actual full-H --attr-source pin, private admin/worktree/index isolation, H/N checks and repeated guards/receipt binding, kill a removed-pin mutant, and refuse unsupported version or missing enforcement. (Coverage: FR-014, FR-017, FR-018, SC-005, SC-006).
- [ ] T016 [US4] Implement the accepted plumbing/quarantine replay kernel in internal/conflict/replay_git.go, reusing a characterized neutral seam with internal/restack/replay.go only through a separately reviewed behavior-preserving extraction; preserve hidden-v1 outputs and never retain/import candidate objects. Depends on red T033; enforce the bound full-H --attr-source on every actual merge-tree call in guarded private admin/worktree/index state, with no mixed original attributes or unsupported-version fallback. (Coverage: FR-010, FR-011, FR-016, FR-017, FR-022, SC-005).
- [ ] T017 [US4] Write failing exact binding/resample tests in internal/conflict/binding_test.go for full native ids, ref targets, ordered range, strategy/options/version, layout/ownership and operation changes; a clean preview or supplied PR/stack context never authorizes a consumer action. (Coverage: FR-002, FR-010, FR-013, FR-016, FR-018, FR-022, SC-008).
- [ ] T018 [US4] Implement pure binding comparison and changed-input derivation in internal/conflict/binding.go plus operation-edge sampling in internal/conflict/preflight.go; expose sampled/ABA limits, reject mismatches and require every 013/015/016 consumer to revalidate its own gates. (Coverage: FR-002, FR-010, FR-013, FR-016, FR-018, FR-022, SC-008).
- [ ] T019 [US4] After OQ-SEM/TOPO acceptance, write failing native merge-profile tests in internal/conflict/merge_git_test.go for explicit two-head/base interpretation, accepted text/binary/rename/mode/gitlink cases, multiple-base/octopus/unsupported refusal and no replay substitution; absent profile remains unavailable. (Coverage: FR-010, FR-016, SC-005).
- [ ] T020 [US4] Implement only accepted measured native merge prediction in internal/conflict/merge_git.go with explicit semantic profile/options, bounded affected paths and reliable optional hunk origin; refuse unmeasured shapes without fallback or submodule traversal. (Coverage: FR-010, FR-016, SC-005).
- [ ] T021 [US4] Write failing scratch lifecycle tests in internal/conflict/scratch_test.go for cancellation at every boundary, child reaping, quota/disk failure, deadline, object-store leak, scratch replacement and cleanup failure; sentinels cover original tree/index/HEAD/refs/config/objects and jj stored working-copy/oplog. (Coverage: FR-004, FR-008, FR-011, FR-012, FR-014, FR-017, FR-020, SC-002, SC-006).
- [ ] T022 [US4] Complete scratch-only lifecycle/refusal handling in internal/conflict/scratch.go with the binding runner/environment/config/attribute hazard policy, source write/network denial, owned-handle cleanup, residue reporting, incomplete status and zero automatic retry/import/recovery-root/journal creation. (Coverage: FR-008, FR-011, FR-012, FR-014, FR-020, SC-002, SC-006).
- [ ] T023 [P] [US4] Write failing native jj preview tests in internal/conflict/preflight_jj_test.go for independent scratch store/oplog, pinned commit/change/operation identity, explicit parent/descendant scope, OQ-JJCONF inherited-conflict/later-resolution cases and missing-node aggregate refusal, original no-snapshot, accepted merge/replay limits and unsupported native isolation refusal. (Coverage: FR-006, FR-011, FR-012, FR-014, FR-016, FR-018, FR-021, SC-006).
- [ ] T024 [US4] Implement only measured accepted OQ-JJCONF native jj graph/aggregate preview capability in internal/conflict/preflight_jj.go; keep unavailable profiles explicit and never simulate with raw Git in colocated jj or share writable original stores. (Coverage: FR-006, FR-011, FR-012, FR-014, FR-021, SC-006).

## Phase 6 — US3 bounded human/structured contract (P1)

**Independent test:** both output modes share facts/status, malformed usage acts
before effects, all bounds fail honestly and hidden restack v1 stays unchanged.

- [ ] T025 [US3] Write failing CLI tests in internal/cli/conflict_test.go for group default-none/help, explicit leaves/selectors, text-default/--json parity with no TTY or stdin/editor/pager, fixed 60-second/10-second bounds, decided --human/--json exclusivity and positive shortening --timeout, while --state-dir/--online/--offline refuse usage before effects, unsupported capability, 0 complete conflict, 1 incomplete/refusal/cancel and 2 usage; fixed notice appears in every admitted result. (Coverage: FR-007, FR-008, FR-013, FR-015, FR-020, SC-008).
- [ ] T026 [US3] Wire parsing/rendering in internal/cli/conflict.go through the reviewed operation only, preserve architectural import limits and escape terminal text; enforce single final object and output-write failure without a second JSON document. (Coverage: FR-007, FR-013, FR-015, FR-020, SC-008).
- [ ] T027 [US3] Write failing contract/compatibility tests in internal/conflict/contract_test.go for complete/incomplete/error fixtures, all finite bounds, byte-path round-trip, shared 015 diagnostic mapping and operation-specific status distinctions, no readiness/authorization fields, no network and unchanged published/frozen JSON files plus decided hidden-restack target incomplete 1 after the 015 revision 2 code task, with current code 3 recorded separately. (Coverage: FR-005, FR-007, FR-013, FR-020, FR-022, SC-003, SC-007, SC-008).
- [ ] T028 [US3] Only after accepted implementation/conformance design, add a separate self-contained proposed conflict-evidence schema and fixtures under schemas/v1/ with additive publication wiring; preserve every existing schema/fixture/digest and implement bounded typed serialization in internal/conflict/render.go. (Coverage: FR-007, FR-020, SC-003).

## Phase 7 — conformance and review evidence

- [ ] T029 Execute every selected-capability scenario from specs/014-conflict-diagnosis/quickstart.md in minimum/current native Git/jj Linux/macOS lanes, including ownership/concurrency/failure, Git first-conflict suffix and accepted native jj complete descendant coverage; record unavailable/missing lanes as blockers in specs/014-conflict-diagnosis/verification.md. (Coverage: FR-009, SC-001, SC-002, SC-003, SC-005, SC-006).
- [ ] T030 Run targeted mutation probes in internal/conflict/safety_mutation_test.go that remove quarantine/write denial, swap backend, allow helpers/network, drop suffix, weaken resampling, leak unselected content, hide cleanup failure or turn unknown into clean; record killed/surviving mutants in specs/014-conflict-diagnosis/verification.md. (Coverage: FR-009, SC-002, SC-006, SC-007, SC-008).
- [ ] T031 Update only real implementation/effect/contract evidence and per-FR/SC traceability in specs/014-conflict-diagnosis/analysis.md and specs/014-conflict-diagnosis/verification.md; never mark absent execution, old 015 measurements or self-review as accepted 014 evidence. (Coverage: FR-009, SC-004).
- [ ] T032 Run change-appropriate source/format/test/conformance checks and obtain independent exact-current-head source review; record actual check names/outcomes and remaining owner/capability blockers in specs/014-conflict-diagnosis/verification.md before publication/Ready gates, without merge/deploy or runtime activation. (Coverage: FR-009, SC-004).

## Dependencies and parallel execution

T001–T004 gate affected capabilities; T005 red precedes T006. US1 follows the
foundation; T007→T008 and T009→T010 can proceed in disjoint native files after
shared values stabilize. US2 requires US1 and its own T011→T012 and gated
T013→T014; hunk enablement can wait without blocking default handoff.
US4 uses foundation plus its profile decisions; T033 must fail before T016 and
pass with the enforced actual attribute-source implementation. Each red task precedes its paired
implementation. T017→T018 binding is required before a preview is exposed;
T021→T022 lifecycle is required before any preview acceptance. Native jj
T023→T024 can run beside Git replay on disjoint files, but shared runners,
neutral-seam extraction and scratch.go edits serialize. Merge T019→T020 waits
for its profile decisions, not a guessed fallback. US3 T025→T026 and T027→T028
integrates only completed selected story units; final T029→T030→T031→T032
records acceptance and independent review. No new concurrency implementation
outside Spec 040 internal/fanout is authorized.

## Implementation strategy and counts

MVP: foundation + US1 + body-free US2 + US3 and their required conformance;
optional hunk, merge and native jj preview remain capability-unavailable until
individually accepted. Deliver isolated replay as the next increment and preserve
no-original-write semantics at every step. Test red first, implement, refactor,
review the exact head, then meet publication gates. No automatic runtime activation.

33 tasks: setup 2, foundation 4, US1 4, US2 4, US4 11, US3 4, final 4.
T033 retains a newly appended stable id but executes as the red attribute gate
before T016; existing task ids are not renumbered.
[P] marks T007/T009 and T023 as disjoint opportunities, not permission to ignore
shared prerequisites. Every task has a path and every new checkbox is unchecked.
