# Implementation Plan: Tidy integration proofs, PR discovery and bounded planning

**Branch**: `codex/spec-tidy-integration-proofs` | **Date**: 2026-10-19 | **Spec**: [spec.md](spec.md)
**Status**: Plan candidate, not independently reviewed. Baseline: Spec 029 source at `417c42e03a3842c9a2e63c16d94f16614431eaff` (stage2-final); it will be rebased onto a corrected stage2 and every source path below must be re-confirmed by T002.

## Summary

Five slices over the existing contract 3 code, none adding an effect class: (A) bounded streaming reads, (B) configuration policy, (C) forge-free proofs, (D) PR discovery per distinct OID, (E) real-repository acceptance. Language and dependencies as Spec 029 (Go standard library, existing Cobra, go-gh, jsonschema); no new dependency, no new network permission in native sandboxes.

## Constitution check

| Principle | Evidence |
| --- | --- |
| I shared human/agent contract | One typed result carries proofs, discovery and config notes; human and JSON agree; offline useful. |
| II evidence before consequential actions | Proofs and notes bind into the plan; apply re-evaluates; nothing deleted on heuristics (FR-005). |
| III ownership and recovery | Protected refs retain regardless of proof; recovery roots unchanged; no new effect. |
| IV specify and verify | Failing tests first for each task; sentinel and mutant killers; independent review gate T001. |
| V Tier 1 peers | Git-only proofs; jj/GitButler get no Git writes and no claimed proofs. |

No constitution exception.

## Affected source areas (to confirm in T002)

- `internal/workspace/git.go` (`gitSafetyWithPrimary` refusal list, `gitRead` for `for-each-ref`/`worktree list`), `native_sandbox.go` (`sandboxEnvironment`, `nativeOutputLimit`), `native_stream.go` (`executeNativeStream*`), `tidy_v3_proof.go`, `tidy_v3_refs.go`, `tidy_v3_options.go`, `tidy_v3_result.go`; new `tidy_v3_proof_native.go` for patch-id/merge-noop and a private object-directory helper.
- `internal/hosting/tidy_evidence_pr.go` (`discoverPRHints` currently keeps `[]tidyPRHint` per OID), `transport.go` route allow-list.
- `internal/cli` for `--integration-proof` and `--pr` handling; `schemas/v3` candidate; smoke scripts `scripts/smoke-tidy.py` extension for real-binary lanes (owner-gated build).

## Decisions

### Decision: limits (fixes #80)

Options: (1) raise `nativeOutputLimit`; (2) stream v1 reads; (3) document v3 as the supported path. Chosen: **(2)**. Raising a global constant weakens every buffered native read; documenting v3 leaves v1 callers broken at about 146 refs. Streaming reuses `executeNativeStream` (Spec 029 unit A) with record parsing, bounded memory and the Spec 029 constants (1,024 records, 4,224 bytes per record, 4,325,376 stdout and 65,536 stderr bytes per stream). Refusals stay hard: one over the bound refuses with the named limit. 64 KiB remains for non-collection reads. Rationale for the exact numbers: 1,024 equals the existing `observationLimit`; 4,224 x 1,024 is the already reviewed Spec 029 maximum, so no new reviewed arithmetic.

### Decision: configuration (fixes #79)

Refuse versus neutralize follows the execution surface (research): refuse what the planning command would execute or cannot evaluate reproducibly; neutralize what is executable only on commands tidy never runs or can disable by command-line configuration. Apply is stricter than plan: it never skips a hook silently. The cost is a plan that says "plannable, not appliable" for hook-configured repositories; that is intentional and visible.

### Decision: proof strength and defaults (OQ-1)

Default is all three forge-free kinds eligible, because the dogfood measurement (5 merge-noop, 21 patch-id) shows patch-id carries most of the gain and every deletion keeps a recovery root. The more conservative alternative (merge-noop and ancestor eligible, patch-id evidence-only unless `--integration-proof` names it) is one flag default away. The owner decides.

### Decision: discovery endpoint

Keep the Spec 029 closed-PR collection (10 pages) and fix per-OID aggregation, hint idempotency, selector-naming errors and coverage reporting first (T022..T025). Whether a second read-only endpoint is needed is decided by evidence from T003 and T021 (why #81 required hints, and whether the 1,000-record page cap truncates on this repository). Candidates to measure only: commit-associated PR listing (documented to return open PRs for commits outside the default branch, so likely unsuitable for rebase-merged heads) and issue search by SHA (separate rate limit). An endpoint is added only with research evidence, in the closed set, the route allow-list and tests together; until then `truncated` is honest.

### Decision: superseded-candidate evidence aid (deferred)

The 99 conflicting branches are consolidated work whose content was reworked into other commits. `git range-diff BRANCH_RANGE CONSOLIDATION_RANGE` can pair commits by patch similarity, and could be reported as `superseded-candidate` evidence for a user-named range, never as deletion proof. **Deferred** to a follow-up Spec: its cost is quadratic in the two range sizes (stacked branches with hundreds of commits), it needs its own bounds, user-input validation for the range, and an owner decision on how a human uses it, none of which is measured. This Spec reserves the classification name only in prose; no schema field, option or task is added. The owner may promote it after reading the 99-branch measurement.

## Phases

- **P1 Gate**: independent review of artifacts, baseline characterization and reproduction of #79/#80/#81 as failing tests (T001..T003).
- **P2 Slice A limits**: streaming reads (T004..T006).
- **P3 Slice B configuration**: planning environment, matrix, apply retention (T007..T011).
- **P4 Slice C proofs**: patch-id, merge-noop, order/budget, schema/CLI (T012..T020).
- **P5 Slice D discovery** (T021..T026).
- **P6 Slice E** docs, review, owner-gated build and real-repository acceptance (T027..T033).

## Risks

| Risk | Mitigation |
| --- | --- |
| `merge-tree` writes objects | private object directory (FR-009), byte-compare test |
| Patch-id of huge diffs is slow | commit-count bounds, aggregate deadline, retain on exhaustion |
| Neutralization hides a real hook problem at apply | apply refuses hook-running effects; plan says non-appliable |
| Rebase onto a corrected stage2 moves code | spec paths marked to confirm; only new files here |
| Page cap hides PRs | coverage `truncated`, never absence |

## Open questions

- **OQ-1**: default `--integration-proof`: all three, or ancestor and merge-noop only with patch-id opt-in?
- **OQ-2**: are the proposed numeric bounds (patch-id 1,024/4,096 commits, 60 s forge-free deadline) acceptable?
- **OQ-3**: promote the superseded-candidate aid to a follow-up Spec now, or after the acceptance counts?
- **OQ-4**: does the owner authorize a CLI build for the plan-only acceptance run (T030, T031), or should acceptance use a source test runner only?
- **OQ-5**: is a second discovery endpoint acceptable if T021 shows the page cap truncates this repository?
