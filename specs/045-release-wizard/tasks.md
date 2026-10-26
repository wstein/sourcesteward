# Tasks: `stw release` wizard (Spec 045)

**Input**: [specification](spec.md), [plan](plan.md). Tests are mandatory: write the failing test, watch it fail for the right reason, then
implement. A box is ticked only by the pull request that implements the task, with its evidence in the pull request body or a per-package file
(never `tasks.md` or `docs/progress.md`; those are set by the last pull request of a wave). Nothing here builds, tags, dispatches, publishes or
pushes a default branch; the real effects happen only in the owner's runs on the spike repository. All boxes are open: this pull request is the specification only.

**Gate**: no task starts before the independent review of this specification is CLEAR and the owner has answered D1 to D13 (the
recommendations apply until then). No code that touches the tag or the dispatch effect is written before T004.

## Phase 1

### R1 gaps

- [ ] T001 Failing test and implementation of `check` text-rule cross-check of `ci.release_workflow` at the evaluated commit: findings `release-workflow-not-dispatchable` and `release-workflow-missing-input`, bounded line scan, no new dependency, exit 3 (G2, G3, D8).
- [ ] T002 Failing tests and implementation of `plan D --for publish --version V` for a commit without a tag: publish gates evaluated, annotation gate `not-applicable` (G10).
- [ ] T003 Failing tests and implementation of the signer-workflow derivation for `record` and `verify` from the contract, with the flag as override only (G5); the one evidence format and one script signature in the contract and the workflow templates (G4); the draft handling in the workflow contract (G6).
- [ ] T003a Add `.stw/release.toml` to `stw` and `stw release check` to `make check` (G1); add `run-name` and the dispatchable inputs to the build workflow template (G2, G8).

### Security review first

- [ ] T004 **Independent security review before code** of the tag step and the dispatch step: typed-line binding, fresh re-verification and TOCTOU, no force, agent-mode refusal, journal grants nothing, token handling, the local-tag resume; the questions of the security analysis. Records its findings as a file `evidence/T004.md`.

### R2 journal

- [ ] T005 Failing tests (SC-007) and implementation of the journal: location and 0700 checks (reuse the state-directory checks), closed event set, hash chain, bounds (64 lines, 64 KiB, 256 files), corruption and over-limit degrade to evidence only, no secret (sentinel test).

### R3 state

- [ ] T006 Failing table tests (SC-001) and implementation of the state derivation for states 0 to 14 from git, hosting double and journal; evidence overrides a contradicting journal.
- [ ] T007 Failing tests and implementation of the run and draft reads (run name, status, conclusion; draft assets) over the existing hosting transport, bounded; `build-in-flight` guard (G8).
- [ ] T008 Failing tests and implementation of `stw release status [--json]` and the `release-status` schema; the plain-language sentence table for every gate, state and finding with the "no sentence fails" test (SC-009).

### R4 shell

- [ ] T009 Failing tests (SC-003, SC-004) and implementation of bare `stw release`: human mode starts the wizard, agent mode or no terminal prints the status and refuses writes; mode detection reused from Spec 041; answers and typed lines from `/dev/tty` only.
- [ ] T010 Failing tests and implementation of steps 1 to 3 (product, commit and version with explained gates, plan and approval) and the dispatch through `dispatch` plan and `--apply` (SC-005): exactly one POST, `plan-stale` refusal, journal lines.
- [ ] T011 Failing tests and implementation of step 4 (wait) and the resume decisions of the decision table, including next version, higher version and `flow-unsupported` (FR-012).
- [ ] T012 Failing interruption tests (SC-002) for every row of the interruption table and the repairs of the journal.

### R5 assets and record

- [ ] T013 Failing tests and implementation of the bounded draft-asset download and the verification of step 5 (hash recomputation, attestation read, mismatch stops); the new read is added to the closed endpoint list and tested.
- [ ] T014 Failing tests and implementation of step 6: clean topic branch only, `record --write`, the record-against-draft table, the printed commit, push and pull request commands; nothing is committed or pushed (SC-008).

### R6 tag (after T004)

- [ ] T015 Failing tests (SC-006) and implementation of step 7 (prospective publish gates) and step 8: fresh checks before the word and before the tag, annotated tag object for the commit id with the message, owner's tagger identity (refuse if absent), one non-forced push of `refs/tags/V`, remote verification, local-only resume, journal lines.
- [ ] T016 Failing tests and implementation of the `ruleset ok` acknowledgement while the advisory is not `present` (D12) and the abandoned version rule (D13).
- [ ] T017 Mutation probes of the typed-line comparison, the fresh re-verification, the agent-mode refusal and the no-force push; each fails a test.

### R7 docs

- [ ] T018 `docs/release.md` (wizard first, states, journal, steps, the manual route), `docs/commands.md`, `docs/json-contracts.md`, `docs/agent-guide.md`, `docs/workspaces.md`, pointers in Specs 026 and 034, `docs/roadmap.md`, `docs/feature-status.json`; `python3 scripts/check-docs.py` clean; unhide the group when the owner agrees (D11).

### R8 spike rounds (owner runs the approvals)

- [ ] T019 Round 1 on `wstein/stw-release-spike`: states 0 to 13 walked once with the real approvals, interrupted after dispatch and after the tag and resumed; evidence in the pull request body.
- [ ] T020 Round 2: a failing build reaches state 5 and the next version; no tag is re-created.
- [ ] T021 Round 3: a second product; independent review of the exact head; then the `stw` and `whr` v0.2.0 releases with the wizard (each its own owner-approved run).

## Phase 2

- [ ] T022 Decide D2 again with the owner after round 1 (an expert `stw release tag`), D5 (tag signing) and the tag-first flow support; implement only what was approved.
- [ ] T023 Polish: more products per screen, release notes assistance, progress detail for slow builds.
