# Plan: `stw release` wizard (Spec 045)

Test-first throughout, temporary repositories and a fake hosting transport only; the real target is the spike repository `wstein/stw-release-spike`, where the owner
runs the real approvals. No work package builds, tags, dispatches or publishes on its own; the real effects happen only in the owner's runs.

## Phases

- **Now (D14):** `stw` and `whr` v0.2.0 are released by hand with today's tag-first flow; the steps are written down as a release protocol (T029), the wizard's test case.
- **1a (the spike):** packages R1 to R8 below, on the spike repository, which gets its own `release-build.yml`, `release-publish.yml` and CI workflow (T018a; the gate tool is built from a pinned source revision).
- **1b (retrofit, target v0.2.1):** `stw` needs Spec 026 WI8 (T035 to T039: `release-build.yml`, `release-publish.yml`, `release-repro.yml`, workflow rule tests) and T047 (the self-hosting switch:
  the `v0.1.0` record and removal of the bootstrap workflow, which would otherwise also build on the wizard's tag push); `whr` needs a `.stw/release.toml` and the two workflows (a Spec 026
  wave 2 polyglot project). Packages R9 to R11.
- **2:** polish and the owner's later decisions (D2, D5, `oci-image`, the tag-first flow).

| Package | Seam | Content | Depends on |
| --- | --- | --- | --- |
| R1 gaps | `internal/release`, contracts, workflow templates, `Makefile` | G1 contract for `stw` (T003a), G2 and G3 `check` cross-check (T001), G4 one format (T003b), G5 signer derivation (T003c), G6 draft handling (T003d), G8 run name, G10 `plan --for publish` (T002), G11 message (T003e) | none |
| R2 journal | new `internal/releasewizard/journal` | location, `0700` with explicit chmod, closed events incl. `dispatching` before the POST, hash chain, bounds, `flock`, no secrets | none |
| R3 state | new `internal/releasewizard/state` | evidence readers over the existing hosting reads plus run and draft reads, run binding, the state table and mapping rules with `conflict`, the decision table, `status` and the `release-status` schema | R1 (G8, G10), R2 |
| R4 shell | `internal/releasewizard`, `internal/cli/release.go` | mode detection reuse, screens, typed lines on `/dev/tty`, steps 1 to 4, 7, 9, 10, waiting on pending CI, resume | R3 |
| R5 assets and record | `internal/releasewizard`, hosting download read | step 5 (bounded download, verify), step 6 (record on a topic branch) | R3 |
| R6 tag | `internal/releasewizard/tag` | step 8: fresh checks, unsigned hook-free tag object, push URL check, explicit refspec, remote verification, ruleset rule, journal | R3, R4 and the security review T004 |
| R7 docs | `docs/release.md` and the others of the docs impact | wizard first, manual route kept, the amendments, unhide the group (D11) | R1 to R6 |
| R8 spike rounds | the spike repository | its workflows (T018a), then the three rounds of the test plan; evidence in the pull request body | R1 to R7 |
| R9 stw workflows | `.github/workflows`, Spec 026 WI8 | `release-build.yml`, `release-publish.yml`, `release-repro.yml` and their rule tests for `stw` | Spec 026 T035 to T039 |
| R10 stw self-hosting | Spec 026 T047 | the `v0.1.0` record, the bootstrap workflow removed | R9, the `v0.1.0` release |
| R11 whr contract | `workharbor` | `.stw/release.toml`, the two workflows, its own review | owner decision on its release policy |

R1 and R2 are independent and may run in parallel (different directories); R4 and R5 edit different files; R6 waits for the security review. Stack at most two levels, name the files
each author touches.

**Evidence.** Per package in the pull request body or a file `evidence/Rn.md` in this directory, never in `tasks.md` or `docs/progress.md`; checkboxes and status lines are set only
by the last pull request of a wave.

**Verification.** A table test per state, per `conflict` rule and per interruption row (SC-001, SC-002); the hosting double fails on every unexpected write method (SC-003, SC-005);
mutation probes of the typed-line check, the journal-only-restricts rule, the fresh re-verification before the tag, the push URL check and the agent-mode refusal; a before and after
repository snapshot (SC-008); the existing release tests unchanged (SC-010). Independent review of each package head; an independent security review of the design and of the exact head
of R6 and the dispatch part of R4.

**Rollback.** The wizard is additive (new packages, a new subcommand and a changed bare command); reverting R4 to R6 restores the six expert commands exactly. The gap fixes are
additive findings and flags with their own tests.
