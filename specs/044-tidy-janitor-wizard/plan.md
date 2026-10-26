# Plan: `stw tidy` janitor wizard (Spec 044)

Test-first throughout, temporary repositories and a fake hosting transport only, no network, no build or release. Each work package is
for one middle-tier author and ends with its tests green and `make check` clean. The code lives where Spec 041 put it; nothing in the
apply path, the proofs, the plan set or the agent mode is edited.

| Package | Seam | Content | Depends on |
| --- | --- | --- | --- |
| P1 risk | new `internal/tidywizard/risk` (pure functions) | the ladder, `risk_reasons`, red reasons, group level, the family rule and group keys | none |
| P2 evidence | `internal/workspace` observers, `internal/tidywizard/adapter.go` | upstream-gone fact (read-only, local refs), `--online` merged-head lookup through the existing Spec 031 discovery | P1 (types) |
| P3 render | `internal/tidywizard/render`, `internal/cli/tidy_wizard.go` | sentence table, grouped text overview, retain table, `--rows`, additive JSON fields and schema | P1 |
| P4 steps | `internal/tidywizard/prompt`, `internal/cli/tidy_interactive.go` | group steps, keys, defaults by level, selection grammar, final summary; the typed prompt and apply path untouched | P1, P3 |
| P5 docs | `docs/tidy.md`, `docs/commands.md`, `docs/json-contracts.md`, `docs/progress.md`, `docs/roadmap.md`, `docs/feature-status.json` | wizard section, amendment list against Spec 041, dogfood note | P1 to P4 |
| P6 done (phase 2) | `internal/workspace`, `internal/cli` | `stw workspace done`, additive `done` block, attribution gate, use in the ladder (C3, R3) | P1 |
| P7 adopt (phase 2) | `internal/tidywizard/owner`, `internal/cli` | `stw tidy adopt`, adoption records, safeguards | P6 |

Phase 1 ships P1 to P5 as v0.2.0; phase 2 ships P6, P7 and the owner's decisions D8 and D10 as v0.3.0. P1 and P2 may run in parallel
(P2 touches observers and the adapter, P1 a new package). P3 and P4 edit different files; both wait for P1.

**Verification.** Mutation probes of every threshold of the ladder and of "at most orange" (SC-003); a transcript golden for the screens
(SC-007); a request-counting hosting double that fails on any write method (SC-005); the Spec 041 suites unchanged (SC-011). One dogfood
dry run of the wizard on a copy of the live workspace answering `q` at the final summary, never applying; applying is the owner's operation.

**Rollback.** The new text is additive: `--json` keeps its shape except additive fields, and the expert layer is untouched, so a revert of
P3 and P4 restores the Spec 041 wizard.
