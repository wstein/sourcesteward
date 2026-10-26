# Implementation Plan: Tidy integration proofs, review report and bounded planning

**Branch**: `codex/spec-tidy-integration-proofs` | **Date**: 2026-10-19 | **Spec**: [spec.md](spec.md)
**Status**: Plan candidate, not independently reviewed. Baseline: Spec 029 source at `417c42e03a3842c9a2e63c16d94f16614431eaff` (stage2-final); it will be rebased onto a corrected stage2 and every source path below must be re-confirmed by T002.

## Summary

Seven slices over the existing contract 3 code, none adding a repository write effect except G: (A) bounded streaming reads, (B) configuration policy, (C) simple forge-free proofs (`ancestor`, git `patch-id`, `merge-noop`, `no-net-change`, with `whitespace-differs` flag and review reasons), (D) PR discovery per distinct OID, (E) review report and reviewer decisions, (F) real-repository plan-only acceptance, (G) restore and history of deleted branches (the one new local effect, authorized and compare-and-swap). Language and dependencies as Spec 029 (Go standard library, existing Cobra, go-gh, jsonschema); no new dependency, no new network permission in native sandboxes. It also amends Spec 029's patch-similarity rule, collection completeness and planning time in `contract-amendments.md`.

## Constitution check

| Principle | Evidence |
| --- | --- |
| I shared human/agent contract | One typed result and one report data set rendered for humans (Markdown) and agents (JSON); offline useful. |
| II evidence before consequential actions | Proofs, reasons, reports and decisions bind into the plan; apply re-evaluates and needs owner authorization; a decision is never authorization. |
| III ownership and recovery | Protected refs retain regardless of proof or decision; recovery refs unchanged and their limits stated; restore is plan-first, authorized, never overwrites and is journaled. |
| IV specify and verify | Failing tests first, golden reports, sentinel and mutant killers; independent review gate T001. |
| V Tier 1 peers | Git-only proofs; jj/GitButler get no Git writes and no claimed proofs. |

No constitution exception.

## Affected source areas (to confirm in T002)

- `specs/029-tidy-dogfood-safety/contract-amendments.md` (the only Spec 029 file touched, documentation only);
- `internal/workspace/git.go` (`gitSafetyWithPrimary` refusal list, `gitRead`), `native_sandbox.go` (`sandboxEnvironment`, `nativeOutputLimit`), `native_stream.go`, `tidy_v3_proof.go`, `tidy_v3_refs.go`, `tidy_v3_options.go`, `tidy_v3_result.go`; new `tidy_v3_proof_native.go` (patch-id, merge-noop, no-net-change, reasons), `tidy_v3_report.go` (data), `tidy_v3_report_render.go` (Markdown and JSON), `tidy_v3_decisions.go`, and a private object-directory helper;
- `internal/hosting/tidy_evidence_pr.go` (`discoverPRHints`), `transport.go` route allow-list;
- `internal/cli` for `restore`, `history`, `--integration-proof`, `--accept-whitespace-differences`, `--report-dir`, `--consolidation-range`, `--decisions`, `--pr`; `schemas/v3` candidate plus candidate report and decision schemas; smoke scripts for real-binary lanes (owner-gated build).

## Decisions

### Decision: limits (fixes #80)

Stream the v1 reads through `executeNativeStream` with record parsing, bounded memory and the Spec 029 constants (1,024 records, 4,224 bytes per record, 4,325,376 stdout and 65,536 stderr bytes). Raising a global constant weakens every buffered native read; documenting v3 as the only path leaves v1 callers broken. Refusals stay hard.

### Decision: configuration (fixes #79)

Refuse what a planning command would execute or cannot evaluate reproducibly; neutralize what only runs in commands tidy never runs or what command-line configuration disables. Apply is stricter than plan and never skips a hook silently. Attribute sources other than the pinned tip tree become review reasons, not refusals.

### Decision: simple proofs, visible flags (owner direction)

Git's own `patch-id`, `merge-noop` and `no-net-change` are the deletable defaults. Whitespace is a visible flag (`whitespace-differs`) from git's own `--verbatim` comparison, not a normalization or a file list; binary decisions are left to Git (`diff-tree --numstat`) and used only for rendering and for never raising the whitespace flag. Cheap guards that prevent real loss (merge commits in range, partial or multiset mismatch, gitlinks, attribute sources, diff attributes, merge drivers) produce review reasons and report entries. Known limits are listed under Limits in the spec instead of becoming rules.

### Decision: review report and decisions in scope

The report and decision file make the judgment cheap for a human and an AI. `git range-diff` pairing against a user-named consolidation range, previously deferred, is in scope as evidence only. Bounded sizes, deterministic ordering and golden fixtures keep it testable. Decisions are bound to the exact tip OID, recorded in plan and journal, and never authorize apply.

### Decision: restore and history (owner-approved amendment)

Deletion is made more liberal by this Spec on the promise that recovery covers it, so the restore path is specified, tested and proven on the dogfood set. `stw tidy restore` is plan then apply like tidy, reads the Spec 029 journal and recovery refs, never overwrites, creates the branch with a compare-and-swap, needs the same explicit authorization as `tidy apply` and appends a restore journal record; `stw tidy history` is a read-only view over the same reader. Worktree, configuration and reflog restore are excluded (T045 may add configuration keys later). Sequenced before the dogfood cleanup.

### Decision: discovery endpoint

No second endpoint (for contract 3; the wizard's per-tip read of Spec 044 is a dated amendment of FR-013). Keep the Spec 029 closed-PR collection (10 pages), fix per-OID aggregation, hint idempotency, selector-naming errors and coverage reporting, and share the Spec 029 online budget with explicit `discovery-budget-exhausted`. T021 measures why #81 required hints; candidates (commit-associated PR listing, issue search by SHA) are revisited only on that evidence in a later amendment.

### Decision: forge-free budget

The forge-free proof phase has its own 60 seconds (per command 10 s, per tip 20 s): offline plan 10 + 60 = 70 s; online plan 10 + 60 + 30 = 100 s; apply 60 s unchanged (amended in Spec 029). The report has its own bounds and is built from data the proofs already computed plus bounded commands.

## Phases

- **P1 Gate**: independent review, baseline characterization, reproduction of #79/#80/#81 (T001..T003).
- **P2 Slice A limits** (T004..T006).
- **P3 Slice B configuration** (T007..T011).
- **P4 Slice C proofs** (T012..T020).
- **P5 Slice D discovery** (T021..T026).
- **P6 Slice E review report and decisions** (T027..T036).
- **P7 Slice F** docs, review, measurement, owner-gated acceptance (T037..T045).
- **P8 Slice G restore and history** (T046..T053), before the dogfood cleanup.

## Protections

| Concern | Protection |
| --- | --- |
| `merge-tree` writes objects | private object directory (FR-009), byte-compare test, alternates hard stop |
| Whitespace-insensitive patch-id | visible `whitespace-differs` flag, report with visible whitespace, opt-in flag or decision |
| Merge commits hide edits | `merge-commits-in-range` reason |
| `merge=union`/`binary` attributes fake a clean merge | `merge-attribute` and `attribute-source` reasons, `--attr-source` pin |
| Grafts/shallow fake history | hard stops |
| Submodule opened by merge engine | `gitlink-change` pre-check |
| Page cap hides PRs | coverage `incomplete`, positive evidence kept |
| Report leaks branch content | only unintegrated diff, 0600, redaction, sensitive-file note |
| AI or human decision is wrong | exact tip binding, kind marked, recovery ref, never authorization |
| Rebase onto a corrected stage2 moves code | spec paths marked to confirm; only new files here |

## Decisions made as defaults (revisable by the owner)

1. Git patch-id is a default proof; `whitespace-differs` needs `--accept-whitespace-differences` or a decision.
2. Bounds: 1,024 branch-side and 4,096 base-side commits, 60 s forge-free phase; report defaults 256 entries, 4 MiB per file, 64 KiB per diff, overridable by `--report-max-entries`, `--report-max-bytes` and `--report-max-diff-bytes` within hard ceilings (1,024, 16 MiB, 1 MiB).
3. No second discovery endpoint; `incomplete` coverage.
4. Plan-only acceptance by source test runner; any CLI build stays an owner gate.
5. Range-diff pairing is in scope as evidence only.

## Open questions

- **OQ-1**: authorize a named CLI build for the binary acceptance lane (T042), or record it as a gap?
- **OQ-2**: should `kind: ai` decisions need an extra confirmation flag before they make a ref deletable?
