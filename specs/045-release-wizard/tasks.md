# Tasks: `stw release` wizard (Spec 045)

**Input**: [specification](spec.md), [plan](plan.md). Tests are mandatory: write the failing test, watch it fail for the right reason, then implement. A box is ticked only by
the pull request that implements the task, with its evidence in the pull request body or a per-package file (never `tasks.md` or `docs/progress.md`; those are set by the last
pull request of a wave). Nothing here builds, tags, dispatches, publishes or pushes a default branch; the real effects happen only in the owner's runs. All boxes are open: this pull
request is the specification only.

**Gate**: no task starts before the independent review of this specification is CLEAR and the owner has answered D1 to D13 (the recommendations apply until then; D14 and D15 are decided on 2026-10-26). No code that touches the tag or the dispatch effect is written before T004.

## Phase 1a (the spike)

### R1 gaps

- [ ] T001 Failing test and implementation of the `check` cross-check of `ci.release_workflow` at the evaluated commit: findings `release-workflow-not-dispatchable` and `release-workflow-missing-input`, a bounded text scan inside `internal/release`, no dependency, exit 3 (G2, G3, D8; SC-014).
- [ ] T002 Failing tests and implementation of `plan D --for publish --version V` for a commit without a tag: publish gates evaluated, annotation gate `not-applicable` (G10).
- [ ] T003a Failing test, then add `.stw/release.toml` to `stw` and `stw release check` to `make check` (G1; SC-013).
- [ ] T003b Failing tests and implementation of the one evidence format and one script signature in the contract and the workflow templates (G4; SC-015).
- [ ] T003c Failing tests and implementation of the signer-workflow derivation for `record` and `verify`, the flag as override only (G5; SC-016).
- [ ] T003d Failing test and implementation of the draft handling of the dispatch-flow build workflow template without the repository variable (G6; SC-017).
- [ ] T003e Failing test and fix of the doubled code in `tag-unresolved` (G11; SC-021).
- [ ] T003f Add `run-name: release-build PRODUCT VERSION COMMIT` and the dispatchable inputs to the build workflow template and the contract text (G2, G8).

### Security review first

- [ ] T004 **Independent security review before code** of the tag step and the dispatch step: typed-line binding, the `dispatching`-before-POST rule, run binding, the `conflict` rules, the journal-only-restricts rule, `flock`, no force, no hooks, no signing, the push URL check, the unsandboxed push, agent-mode refusal, token handling, the local-tag resume, the ruleset rule. Findings in `evidence/T004.md`.

### R2 journal

- [ ] T005 Failing tests (SC-007) and implementation of the journal: location, `0700` with explicit chmod under `umask 0000`, closed event set including `dispatching`, hash chain, bounds, the 256-file limit, corruption and over-limit degrade to evidence only with acknowledgements, `flock` read-only fallback for a second wizard, no secret (sentinel test).

### R3 state

- [ ] T006 Failing table tests (SC-001) and implementation of the state derivation for states 0 to 15 and every mapping rule including each `conflict` case; the journal only restricts (mutation: a journal-only line changes nothing).
- [ ] T007 Failing tests and implementation of the run, draft and tag reads over the existing hosting transport, bounded, and of the run binding (workflow path, event, branch, actor, run name, created after `dispatching`, latest attempt).
- [ ] T008 Failing tests and implementation of `stw release status [--json]`, its exit codes, the offline default and the `release-status` schema; the plain-language sentence table for every gate, state and finding with the "no sentence fails" test (SC-009).

### R4 shell

- [ ] T009 Failing tests (SC-003, SC-004) and implementation of bare `stw release`: human mode starts the wizard, agent mode or no terminal prints the offline status and refuses wizard effects; mode detection reused from Spec 041; answers and typed lines from `/dev/tty` only.
- [ ] T010 Failing tests and implementation of steps 1 to 3 (product, commit and version with explained gates, plan and approval) and the dispatch (SC-005): `dispatching` before the POST, exactly one POST, POST error goes to state 2, `plan-stale` refusal.
- [ ] T010a Failing tests and implementation of waiting on `ci-run-pending` and the `ci-run-absent` stop (SC-019; spike items 8, 12, 14).
- [ ] T010b Failing tests and implementation of FR-015: after taking the `flock`, the wizard re-derives the state and re-reads the journal before any step and redraws on change (SC-023).
- [ ] T011 Failing tests and implementation of step 4 (wait up to 30 minutes), state 2 with `no-run VERSION`, and the decision table including next version, higher version, `flow-unsupported` and `artifact-kind-unsupported` (FR-012).
- [ ] T012a Failing tests and implementation of the state-2 exits (wait longer, `no-run VERSION` leaving state 2 for `ready`, abandon), the binding epochs, the exclusion of concluded failed runs, the `conflict` on an active or successful earlier-epoch run and the vanished-tag signals V1 to V4 with the negative cases of SC-024 (a normal pre-tag release never stops).
- [ ] T012b Failing tests and implementation of leaving `conflict` (SC-025): the conflict kinds and their typed actions (`abandon`, `no-tag`, `no-run`), mapping rule 1a, the evidence table and the read-only commands, the crashed tag push in both outcomes, the `tag-push-rejected` and `tag-push-released` journal events.
- [ ] T012 Failing interruption tests (SC-002) for every row of the interruption table and the journal repairs.

### R5 assets and record

- [ ] T013 Failing tests and implementation of the bounded draft-asset download and the verification of step 5 (hash recomputation, attestation read, mismatch stops); the new read is added to the closed endpoint list and tested.
- [ ] T014 Failing tests and implementation of step 6: clean topic branch only, `record --write`, the record-against-draft table, the printed commands; nothing is committed or pushed (SC-008).

### R6 tag (after T004)

- [ ] T015 Failing tests (SC-006) and implementation of step 7 (prospective publish gates) and step 8: fresh checks before the word and before the tag, the unsigned hook-free tag object for the commit id, the owner's tagger identity (refuse if absent), the push URL check, one non-forced `git push --no-verify` of `<tag-object-id>:refs/tags/V`, API verification that the tag peels to exactly D, local-only resume, journal lines.
- [ ] T016 Failing tests (SC-022) and implementation of the ruleset rule (`partial` and `absent` refuse; `unknown` needs `ruleset ok` at every attempt, never stored) and of the abandoned-version rule (D13).
- [ ] T016b Measure whether the repository activity or events API exposes tag deletions (spike); the result decides whether signal V4 of the vanished-tag evidence is used.
- [ ] T016a Capture the real rulesets response of the spike repository and find why `RulesetsKnown` is false with an admin token (`internal/hosting/release.go:208`); failing test from the captured response; fix or explain; the advisory reaches `present` (SC-020).
- [ ] T017 Mutation probes of the typed-line comparison, the journal-only-restricts rule, the `dispatching`-before-POST order, the fresh re-verification, the push URL check, the no-force push and the agent-mode refusal; each fails a test.

### R7 docs and R8 spike rounds

- [ ] T018 `docs/release.md`, `docs/releasing.md`, `docs/commands.md`, `docs/json-contracts.md`, `docs/agent-guide.md`, `docs/workspaces.md`, pointers in Specs 026 and 034, `docs/roadmap.md`, `docs/feature-status.json`; `python3 scripts/check-docs.py` clean; unhide the group when the owner agrees (D11; SC-018).
- [ ] T018a Write the spike's workflows from the Spec 026 workflow contract: `release-build.yml`, `release-publish.yml` and its CI workflow. Test infrastructure for the spike only: the workflows obtain the `stw` gate tool by building it from a **pinned full commit SHA** of `wstein/sourcesteward` (`fetch-depth: 1`, pinned Go version, never from the commit being released and never from a moving branch), as the Spec 026 workflow contract does for `stw`'s own repository; real projects use a released, checksum-pinned and attestation-verified `stw` once one exists.
- [ ] T019 Round 1 on `wstein/stw-release-spike` (the spike has its own `release-build.yml` and `release-publish.yml`): states 0 to 13 walked once with the real approvals, the dispatch plan observed, interrupted after dispatch and after the tag and resumed; evidence in the pull request body (SC-012).
- [ ] T020 Round 2: a failing build reaches state 5 and the next version; no tag is re-created.
- [ ] T021 Round 3: a second product; independent review of the exact head.

## Real releases now (D14) and phase 1b (retrofit, target v0.2.1)

- [ ] T029 Write the **release protocol** as part of the real v0.2.0 releases of `stw` and `whr` (by hand, tag-first flow, a hand-set annotated tag): `specs/045-release-wizard/protocol/v0.2.0-stw.md` and `v0.2.0-whr.md`, each step with the exact command, its result and what was checked; it is the test case for the **shared steps** of the wizard: a mapping table in the protocol names, for each hand step (choose commit and version, the gates, tag, verify the published release), the wizard step it corresponds to, and marks the steps that differ (the tag-first order: tag, then build, then draft; the dispatch flow: build, draft, record, then tag, then publish workflow). SC-012 compares the wizard's walk against the protocol on the shared steps only; the dispatch-specific steps are proven by the spike rounds.

- [ ] T024 `stw`: Spec 026 WI8 (T035 to T039) implemented and reviewed: `release-build.yml`, `release-publish.yml`, `release-repro.yml`, workflow rule tests, with the Spec 045 inputs and run name.
- [ ] T025 `stw`: Spec 026 T047, the self-hosting switch (the `v0.1.0` record, the bootstrap workflow removed in the same change as the first dispatch-flow tag path).
- [ ] T026 `whr`: a `.stw/release.toml`, `release-build.yml` and `release-publish.yml`, and its review, after the owner states its release policy (Spec 026 wave 2, polyglot).
- [ ] T027 The `stw` and `whr` v0.2.1 releases with the wizard, each an owner-approved run, after T024 to T026 and the spike rounds.

## Phase 2

- [ ] T022 Decide D2 again with the owner after round 1 (an expert `stw release tag`), D5 (tag signing) and the tag-first flow and `oci-image` support; implement only what was approved.
- [ ] T023 Polish: more products per screen, release notes assistance, progress detail for slow builds, a workflow template generator (spike item 4).
- [ ] T028 D15 (decided: no refusal): document in `docs/release.md` and `docs/agent-guide.md` that expert `dispatch --apply` has no agent-mode check, that it only starts a build that makes a draft, and that the tag ruleset and the owner's tag are the protection.
