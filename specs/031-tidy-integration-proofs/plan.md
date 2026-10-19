# Implementation Plan: Tidy integration proofs, PR discovery and bounded planning

**Branch**: `codex/spec-tidy-integration-proofs` | **Date**: 2026-10-19 | **Spec**: [spec.md](spec.md)
**Status**: Plan candidate, not independently reviewed. Baseline: Spec 029 source at `417c42e03a3842c9a2e63c16d94f16614431eaff` (stage2-final); it will be rebased onto a corrected stage2 and every source path below must be re-confirmed by T002.

## Summary

Five slices over the existing contract 3 code, none adding an effect class: (A) bounded streaming reads, (B) configuration policy, (C) forge-free proofs (`merge-noop` default, `patch-exact` opt-in, `patch-id` evidence), (D) PR discovery per distinct OID, (E) real-repository plan-only acceptance. It also amends Spec 029's patch-similarity rule in `contract-amendments.md`. Language and dependencies as Spec 029 (Go standard library, existing Cobra, go-gh, jsonschema); no new dependency, no new network permission in native sandboxes.

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

- `specs/029-tidy-dogfood-safety/contract-amendments.md` (the only Spec 029 file touched, documentation only);
- `internal/workspace/git.go` (`gitSafetyWithPrimary` refusal list, `gitRead` for `for-each-ref`/`worktree list`), `native_sandbox.go` (`sandboxEnvironment`, `nativeOutputLimit`), `native_stream.go` (`executeNativeStream*`), `tidy_v3_proof.go`, `tidy_v3_refs.go`, `tidy_v3_options.go`, `tidy_v3_result.go`; new `tidy_v3_proof_native.go` for merge-noop/patch evidence/patch-exact and a private object-directory helper.
- `internal/hosting/tidy_evidence_pr.go` (`discoverPRHints` currently keeps `[]tidyPRHint` per OID), `transport.go` route allow-list.
- `internal/cli` for `--integration-proof` and `--pr` handling; `schemas/v3` candidate; smoke scripts `scripts/smoke-tidy.py` extension for real-binary lanes (owner-gated build).

## Decisions

### Decision: limits (fixes #80)

Options: (1) raise `nativeOutputLimit`; (2) stream v1 reads; (3) document v3 as the supported path. Chosen: **(2)**. Raising a global constant weakens every buffered native read; documenting v3 leaves v1 callers broken at about 146 refs. Streaming reuses `executeNativeStream` (Spec 029 unit A) with record parsing, bounded memory and the Spec 029 constants (1,024 records, 4,224 bytes per record, 4,325,376 stdout and 65,536 stderr bytes per stream). Refusals stay hard: one over the bound refuses with the named limit. 64 KiB remains for non-collection reads. Rationale for the exact numbers: 1,024 equals the existing `observationLimit`; 4,224 x 1,024 is the already reviewed Spec 029 maximum, so no new reviewed arithmetic.

### Decision: configuration (fixes #79)

Refuse versus neutralize follows the execution surface (research): refuse what the planning command would execute or cannot evaluate reproducibly; neutralize what is executable only on commands tidy never runs or can disable by command-line configuration. Apply is stricter than plan: it never skips a hook silently. The cost is a plan that says "plannable, not appliable" for hook-configured repositories; that is intentional and visible.

### Decision: proof strength and defaults

Default deletion proofs are `ancestor` and `merge-noop`. `git cherry` patch-ids ignore whitespace (reproduced on Git 2.56), so `patch-id` is evidence only. `patch-exact` (verbatim patch-id, whitespace preserved) is a deletion proof only when the owner names it in `--integration-proof`; even then it proves history, not present content. `merge-noop` requires a net change (`B^{tree} != M^{tree}`), one merge base and no gitlink difference. These are conservative defaults selected for the owner and revisable; the dogfood gain of the default set is the 5 merge-noop branches, with the 21 patch-id branches visible as evidence.

### Decision: discovery endpoint

No second endpoint. Keep the Spec 029 closed-PR collection (10 pages), fix per-OID aggregation, hint idempotency, selector-naming errors and coverage reporting (T022..T025), and share the Spec 029 online budget (128 requests, 30 s) with explicit `discovery-budget-exhausted`. T021 measures why #81 required hints; candidate endpoints (commit-associated PR listing, issue search by SHA) are revisited only on that evidence in a later amendment, `truncated` being the honest result until then.

### Decision: superseded-candidate evidence aid

The range-diff evidence aid for consolidated work (99 branches in the measurement) becomes its own later Spec. It is not specified, bounded or tasked here.

### Decision: forge-free budget

The 60-second forge-free proof deadline is its own budget, separate from the online 30-second deadline; planning offline uses only the former.

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
| `merge-tree` writes objects | private object directory (FR-009), byte-compare test, alternates refusal |
| Whitespace-blind patch-id | evidence only; `patch-exact` verbatim and opt-in |
| Empty-net-change branch | `B^{tree} != M^{tree}` precondition |
| Grafts/shallow/replace refs fake history | retain or ignore with notes (FR-010) |
| Submodule opened by merge engine | gitlink-difference pre-check |
| Patch-id of huge diffs is slow | commit-count bounds, aggregate deadline, retain on exhaustion |
| Neutralization hides a real hook problem at apply | apply refuses hook-running effects; plan says non-appliable |
| Rebase onto a corrected stage2 moves code | spec paths marked to confirm; only new files here |
| Page cap hides PRs | coverage `truncated`, never absence |

## Decisions made as conservative defaults (revisable by the owner)

1. Default deletion proofs `ancestor`, `merge-noop`; `patch-id` evidence only; `patch-exact` opt-in.
2. Bounds 1,024 branch-side and 4,096 base-side commits, 60 s forge-free deadline separate from the online 30 s.
3. No second discovery endpoint; report `truncated`.
4. Plan-only acceptance by source test runner; any CLI build stays an owner gate.
5. Range-diff aid is a later Spec.

## Open questions

- **OQ-1**: does the owner accept these defaults, in particular opt-in `patch-exact` (loses the 16 patch-id-only branches as default deletions)?
- **OQ-2**: authorize a named CLI build for the binary acceptance lane (T031), or record it as a gap?
