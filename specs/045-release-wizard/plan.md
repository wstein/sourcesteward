# Plan: `stw release` wizard (Spec 045)

Test-first throughout, temporary repositories and a fake hosting transport only; the real target is the spike repository
`wstein/stw-release-spike`, where the owner runs the real approvals. No work package builds, tags, dispatches or publishes on its own; the
real effects happen only in the owner's runs of the spike rounds.

| Package | Seam | Content | Depends on |
| --- | --- | --- | --- |
| R1 gaps | `internal/release`, contracts, workflow templates, `Makefile` | G1 contract for `stw`, G2 and G3 `check` cross-check by text rules, G5 signer derivation, G8 run name, G10 `plan --for publish` for a commit; one evidence format and script signature (G4); the draft handling (G6) | none |
| R2 journal | new `internal/releasewizard/journal` | location and safety checks (reuse of the state-directory checks), closed events, hash chain, bounds, no secrets | none |
| R3 state | new `internal/releasewizard/state` | evidence readers over the existing hosting reads plus run and draft reads, the state table, the decision table, `status` and the `release-status` schema | R1 (G8, G10), R2 |
| R4 shell | `internal/releasewizard`, `internal/cli/release.go` | mode detection reuse, screens, typed lines on `/dev/tty`, steps 1 to 4, 7, 9, 10 (read-only and `dispatch`), resume | R3 |
| R5 assets and record | `internal/releasewizard`, hosting download read | step 5 (bounded download, verify), step 6 (record on a topic branch) | R3 |
| R6 tag | `internal/releasewizard/tag` | step 8: fresh checks, annotated tag object, one non-forced push, remote verification, journal | R3, R4 and the security review T004 |
| R7 docs | `docs/release.md` and the others of the docs impact | wizard first, manual route kept, unhide the group (D11) | R1 to R6 |
| R8 spike rounds | the spike repository | the three rounds of the test plan; evidence in the pull request body | R1 to R7 |

Phase 1 ships R1 to R8 (the spike, then `stw` and `whr` v0.2.0). R1 and R2 are independent and may run in parallel (different directories); R4 and R5 edit
different files; R6 waits for the security review. Stack at most two levels, name the files each author touches.

**Evidence.** Per package in the pull request body or a file `evidence/Rn.md` in this directory, never in `tasks.md` or `docs/progress.md`; checkboxes and status
lines are set only by the last pull request of a wave.

**Verification.** A table test per state and per interruption row (SC-001, SC-002); the hosting double fails on every unexpected write method (SC-003, SC-005);
mutation probes of the typed-line check, the fresh re-verification before the tag and the agent-mode refusal; a before and after repository snapshot (SC-008);
the existing release tests unchanged (SC-010). Independent review of each package head; an independent security review of the design and of the exact
head of R6 and the dispatch part of R4.

**Rollback.** The wizard is additive (new packages, a new subcommand and a changed bare command); reverting R4 to R6 restores the six expert commands exactly.
The gap fixes are additive findings and flags with their own tests.
