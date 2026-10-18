# Specification Quality Checklist: Complete local tidy with explicit integration evidence

**Purpose**: Author validation before independent specification re-review, not an independent CLEAR.
**Created**: 2026-10-18
**Feature**: [Specification](../spec.md)

## Content quality

- [x] User outcomes and four selected findings are explicit; no implementation or acceptance claim is made.
- [x] Mandatory user stories, requirements, success criteria and assumptions are complete.
- [x] Requirements describe observable behavior, trust/effect boundaries and necessary limits; no language/library/module implementation is prescribed.
- [x] Technical native/provider identities are necessary product constraints; implementation APIs and command sequencing remain for the reviewed plan.

## Requirement completeness

- [x] No unresolved clarification markers or repeated feature-approval request remains.
- [x] Online proof requires exact head, stable repository/PR identity, chosen target reachability and final resampling; similarity alone cannot authorize deletion (US1, FR-006–FR-010).
- [x] Bounded discovery plus explicit hints covers multiple real local refs, while ambiguous and incomplete evidence retains (US1, FR-006/008/025).
- [x] Legacy manually created attached worktrees have an actual acceptance path, with explicit completion intent and separate registration/files authority (US2, FR-011–FR-016).
- [x] Active/protected/current/locked/foreign/dirty/ignored-extra state remains protected; clean and merged facts do not prove activity or ownership (US2).
- [x] Metadata record/aggregate budgets are derived from existing entry/path limits, cover all safety consumers and preserve all-path attributes/fingerprints (US3, FR-017–FR-019).
- [x] Local tracking cleanup has complete remote-absence, mapping/configuration and exact local-ref proof; symbolic/base refs and server refs remain protected (US4, FR-020–FR-024).
- [x] Online effects, bounded failures, recovery, freshness, retries, compatibility and backend gaps are explicit (FR-001–FR-005/025–FR-028).
- [x] Retirement stored/disk user-content bounds are amended explicitly for Git v3, with all resampling/metadata/process/framing/time budgets and realistic 315–358-file/301,117-byte SVG acceptance (US2-6/7, FR-029–FR-032, SC-002).
- [x] Every requirement has a scenario or measurable outcome: FR-001–FR-010 → US1/SC-001/005; FR-011–FR-016 → US2/SC-002; FR-017–FR-019 → US3/SC-003; FR-020–FR-024 → US4/SC-004; FR-025–FR-028 → all failure scenarios/SC-005/006; FR-029–FR-032 → US2-6/7/SC-002.
- [x] Edge cases, assumptions, exclusions and dependencies are named. No automatic adoption, raw cleanup or unbounded fallback.

## Readiness and gates

- [x] [Contract amendments](../contract-amendments.md) identifies exact Spec 010/011 changes and unchanged inventory/hosting/schema boundaries.
- [x] Prior private RC observations are motivation and fixture shape, not current apply authorization; original denominators stay fixed.
- [x] Proposed success is measurable without depending on implementation choices; real cleanup counts are not promised before fresh plans.
- [ ] Independent specification review CLEAR on the exact committed revision.
- [ ] Subsequent reviewed plan, tasks and consistency/security analysis before runtime implementation.
- [ ] Implementation, source/native verification and authorized candidate reruns; intentionally outside this specification gate.

## Notes

This author checklist assesses requirement quality only. The installed workflow requires "Review the generated spec before planning." No plan/tasks are produced before that independent gate. No extension hooks are configured. Build/install/activation require their separately named approval; no source check or specification review authorizes them.

## Source verification at specification handoff

- `umask 022; make check`: incomplete. Formatting, all Go tests, vet and module verification passed. Python ran 83 tests: 82 passed; `test_packaging_excludes_source_and_binds_revision` errored because `.tools/hugo` is absent. Inspection confirms that test performs an actual documentation build, so it was not enabled or rerun. No site or CLI artifact was built.
- `python3 scripts/check-docs.py`: passed (256 authored documents). `python3 -B scripts/check-architecture.py`: passed. Staged whitespace check passed.
- `node --test scripts/test_theme.mjs`: unavailable (`node: command not found`); no tool installation attempted.
- These checks cover unchanged runtime code and new specification text; they are not acceptance evidence for the proposed behavior. No live repository cleanup or provider investigation was repeated.

## Stacked-integration correction after prior CLEAR gates

The prior specification `99539328` and plan `5272dc1e` were independently CLEAR and are preserved at `codex/tidy-dogfood-spec-clear` and `codex/tidy-dogfood-plan-clear`. New measured evidence found seven of nine exact merged-head examples targeted intermediate stack branches. FR-006/007, US1-6, SC-001/002 and dependent plan/model/CLI/provider contracts now use complete repository-scoped discovery without original-base filtering, retain the original base tuple as resampled provenance, and require the reported merge result to be a native ancestor of the exact selected local base. The redundant proposed integration-branch flag is removed; no runtime or published flag existed. Wrong repository, missing/nonreachable merge result, changed tuple and ambiguity still refuse. Renewed independent specification/plan review is pending; tasks cannot be finalized or runtime started on the previous CLEAR alone.

## Independent review round 1 corrections

Exact candidate `4310194e0c60381ff908db277735c3b03d096399` was NOT CLEAR with two Medium findings and no Low findings. It is preserved at local branch `codex/tidy-dogfood-spec-review1`.

1. The original specification retained retirement's 64 KiB stored/disk file cap, so all six measured legacy targets would fail on their 301,117-byte SVG. FR-029–FR-032, US2-6/7 and SC-002 now explicitly amend every relevant Git v3 content boundary, retain old v2/jj and metadata limits, account for all nine/twenty-four user-content passes, and require realistic scale/deadline and hostile-stream/actor-write acceptance. The amendment proposal names the binding WI3 contract/plan/docs and source consumers. No runtime acceptance is claimed.
2. The corrected commit carries truthful `Refs: Spec 029`; there is no invented issue number.

Correction verification: offline docs (256 authored documents), architecture and staged whitespace checks passed on the corrected content. Full Go/race checks are not repeated for unchanged runtime code. The prior Hugo-build exclusion and unavailable Node remain disclosed above. Exact corrected-revision independent re-review is still pending; no plan/tasks or implementation may start before CLEAR.
