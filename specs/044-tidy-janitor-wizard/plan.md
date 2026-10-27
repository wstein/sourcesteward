# Plan: `stw tidy` janitor wizard (Spec 044)

Test-first throughout, temporary repositories and a fake hosting transport only, no network, no build or release. Each work package is
for one middle-tier author and ends with its tests green and `make check` clean. The code lives where Spec 041 put it; nothing in the
proofs, the stale checks, the effects, the content of a plan set or the agent mode is edited. The one change to the apply entry and to
`planset.Build` is the D12 receipt (P4, after its security review).

| Package | Seam | Content | Depends on |
| --- | --- | --- | --- |
| P1 risk | new `internal/tidywizard/risk` (pure functions) | the ladder, `risk_reasons`, red reasons, group level, the family rule and group keys | none |
| P2 evidence | `internal/workspace` observers, `internal/tidywizard/adapter.go`, the hosting discovery caller | upstream-gone fact (read-only, local refs), observed dirt facts for the risk function, online-by-default reads with the budget, fallback and `--offline`, merged-head lookup through the existing Spec 031 discovery | P1 (types) |
| P3 render | `internal/tidywizard/render`, `internal/cli/tidy_wizard.go`, `internal/cli/tidy_mode.go` | sentence table, grouped text overview, retain table, `--rows` and `--offline` (flag classification), additive JSON fields and schema | P1 |
| P4 steps | `internal/tidywizard/prompt`, `internal/cli/tidy_interactive.go`, `internal/tidywizard/planset/apply.go`, `internal/tidywizard/planset/planset.go`, `internal/cli/tidy_planset.go`, `internal/cli/tidy.go` (passes no receipt) | group steps, keys, defaults by level, selection grammar, final summary; the single confirmation per run (D12: the confirmation receipt, after the independent security review T012c); the separate apply and the agent path untouched except that, in human mode, the store and apply answers are read from `/dev/tty` (D16) | P1, P3 |
| P5 docs | `docs/tidy.md`, `docs/commands.md`, `docs/json-contracts.md`, `docs/progress.md`, `docs/roadmap.md`, `docs/feature-status.json` | wizard section, amendment list against Spec 041, dogfood note | P1 to P4 |
| P6 done (phase 2) | `internal/workspace`, `internal/cli` | `stw workspace done`, additive `done` block, attribution gate, use in the ladder (C3, R3) | P1 |
| P7 adopt (phase 2) | `internal/tidywizard/owner`, `internal/cli` | `stw tidy adopt`, adoption records, safeguards | P6 |

Phase 1 ships P1 to P5 as v0.2.0; phase 2 ships P6, P7 and the TUI checklist (D10) as v0.3.0. P2 starts once the P1 types exist (a second
branch stacked on P1, at most two levels, or after P1 merged); the files differ (observers and adapter against a new package). P3 and P4 edit
different files; both wait for P1.

**Evidence.** Per package in the pull request body or a file `evidence/Pn.md` in this directory, never in `tasks.md` or `docs/progress.md`;
the checkboxes and status lines are set only by the last pull request of a wave.

**Verification.** Mutation probes of every threshold of the ladder and of "at most orange" (SC-003); a transcript golden for the screens
(SC-007); a request-counting hosting double with 7 members and about 466 tips that fails on any write method (SC-005); the Spec 041 suites unchanged (SC-011). One dogfood
dry run of the wizard on a copy of the live workspace answering `q` at the final summary, never applying; applying is the owner's operation.

**Rollback.** The new text is additive: `--json` keeps its shape except additive fields, and the expert layer is untouched (the D12 receipt is a separate commit that can be reverted alone; the D16 commit is **not** reverted without the D12 commit), so a revert of
P3 and P4 restores the Spec 041 wizard.

## Accepted T012b display composition (2026-10-27)

Apply the owner's [three accepted decisions](https://github.com/wstein/sourcesteward/pull/319)
through the [native-prune exception](spec.md#accepted-registration-prune-composition-2026-10-27).
A separate pure adapter consumes the existing typed registration evidence; it never
feeds native absence into the integration-strength ladder. Orange requires complete
admission and strict row gates. Verdict `retain` with the ownership-unproven or
absence-unproven reason is always kept with that exact reason; blocked/unknown and
contradictory safe rows are red/not-judged. Observation incompleteness alone does not
change this row mapping. No green/yellow, online promotion or PR attachment exists here.

Keep metadata-only groups/counts distinct from directories and branches. Only optional
registration `risk`/`risk_reasons` and separate `summary.registration_prunes` are added
to overview 4. Counts cover the whole observed filtered view before output cuts, not
selected effects. Existing candidate definitions, all original required row fields, hash
inputs/version domains, accepted expert effects and frozen v2/v3 schemas stay unchanged.
New display combinations may be stricter only when optional risk is present; old rows
without annotations still validate. Older strict readers may reject the additions.

After this artifact amendment merges, write failing adapter/schema/selection tests and
causal mutations before product changes. Keep no/default selection, exact disclosure,
strict owner/scope checks, agent own only, and no unknown ownership override. Manifest-2
wizard integration follows existing T012b3/T012b4 dependencies; the later D12 receipt
still needs T012c. This amendment implements no product path and marks no task complete.
