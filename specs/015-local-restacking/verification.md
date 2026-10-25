# Verification: `stw restack plan` (first slice)

> **Owner-decision amendment — 2026-10-25:** all former OQ IDs are decided;
> [current profiles](../016-stacked-pr-coordination/contracts/owner-decisions.md) supersede earlier undecided recommendations.
> Target exit revision 2 is 0/1/2; current code still 3. Optional actual re-sign is separately gated.
> This is a documentation target, not implementation or native acceptance.

Status: implemented in source on `feat/015-restack-plan`, hidden command, unpublished schema. This file records evidence; it does not accept the feature. Acceptance needs T021 (minimum and current Git lanes) and T023 (independent review of the exact head).

## What ran (2026-10-21, Git 2.56.0, Linux arm64)

- Test first for every task: each test file was observed red (compile failure or failing assertions) before its implementation.
- `gofmt -l`, `go vet ./...`, `go test ./internal/restack ./internal/cli ./internal/contracts`, the full `go test -timeout 40m ./...`, `python3 -B scripts/check-architecture.py`, `python3 -B scripts/check-docs.py`, `git diff --check`.
- Mutation controls (each a deliberate defect that a test must kill; survivors are named): recorded in the commit message of the task that owns the guard. Killed: non-explicit revision syntax, the ancestor check, the empty range, merge counting, the peel to a commit, unrelated histories, `--expect-head`, every configuration, metadata and attribute hazard once, the dirty, operation and other-worktree refusals, object writes outside the quarantine, the quarantine and home removal, the fixed committer, the preserved author, the empty-replay rule, the merge base, the one-shot cross-check, the range-diff `!` mark, the cherry arguments, `+` as dropped, the touched-path probe, the empty parent range, patch-id comparison, the identity pass rules, the overlap indicator, the lease and resync texts (`--force`, a placeholder, a short id, the character set), the nine clean conditions of the outcome table, the bounds, the exit mapping, output escaping, the architecture rules.
- Survivors, all equivalent or redundant: the three-dot range once the ancestor check holds; `--no-optional-locks` (the environment sets `GIT_OPTIONAL_LOCKS=0` too); `gc.auto=0` and `--no-renames` of the patch stream (pinned through configuration as well); the ambiguous-ownership mapping (no offline fixture).

## Traceability

| Requirement | Evidence (tests in `internal/restack` unless named) |
| --- | --- |
| FR-001, FR-011 | `options_test.go` (syntax), `input_test.go` (resolution, refusals, moved range exactly `old_parent..old_head`, ancestor check, annotated tags, ambiguity) |
| FR-002, FR-013 g/h | `effects_test.go` (affected refs, worktrees, hooks by presence, writes disclosure) |
| FR-003, FR-007 | `hazards_test.go` (jj and non-repository refusals without fallback, SHA-256, bare, non-canonical paths); `internal/cli/restack_test.go` |
| FR-006, D8 | `effects_test.go` (`TestLeaseAndResyncTexts`, `TestTextsAreOmittedForUnsafeValues`) |
| FR-008 | `internal/cli/restack_test.go` (exit statuses, human and JSON from one result, escaping) |
| FR-009, FR-018 | `plan_test.go` (paths, result bytes), `input_test.go` (commits), `sentinel_test.go` (cancel, deadline) |
| FR-010 | open: T021 |
| FR-012 | `sentinel_test.go` (byte snapshot of repository, worktrees and neighbours on every path; hooks, signing program and `gc.auto` planted; the control that a plain merge-tree writes), `replay_test.go` (quarantine removed on every path) |
| FR-013 a to f | `evidence_test.go`, `replay_test.go` |
| FR-014 | `input_test.go`, `hazards_test.go` |
| FR-015 | `evidence_test.go` (flag only, placeholder in the environment never expected), `replay_test.go` (fixed committer, no repository `user.*`), `hazards_test.go` (`local-identity-config`) |
| FR-016 | `evidence_test.go` (`TestSemanticBreaksAreNeverReported`), `outcome_test.go` (5,832-case table) |
| FR-017 | `internal/contracts/restack_test.go` (five fixtures, real output for every status, 24 invariants, additive compatibility), `schema_freeze_test.go` |
| T019 | `scripts/test_architecture.py` |

## Not verified

- Object mtimes: Git may freshen the mtime of an already-existing object in the user's `.git/objects` when `merge-tree` or `commit-tree` writes an identical object (content byte-identical). The sentinel compares content, so `objects: quarantine-only` is not claimed for mtimes.
- Git 2.46.0 and the pinned CI lanes; macOS; the jj lanes (only the `.jj` directory refusal ran). The quarantine behaviour of the reviewer experiment on other Git versions and pack-threshold packing are unmeasured.
- Ambiguous and GitButler ownership have no offline fixture.
- The 60 s time bound is tested by deadline expiry, not by waiting.
- `git range-diff` output is parsed by its line grammar; a Git version that prints another form yields `unknown` marks, never `=`.
- The prediction is not a rebase: renames beyond merge-ort defaults, rerere, whitespace options and commit encodings can differ (`prediction-is-not-apply`).
- No build, release, tag, install, push of anything but the topic branch or network use happened.

## Documentation amendment 2026-10-25

**Status:** proposed design only; no apply/recovery/jj implementation or native
acceptance. Original 2026-10-21 commands/results above are preserved as historical
evidence and were not rerun. Base source inspected:
`3d4c867a451217a2942564a7dcb4736d45a09b70`. Historical T001 remains checked;
T004 deliberately skipped and T021/T023/T024 remain open. New T025–T049 are unchecked planning work, including the later dated owner
amendment exit/signing tasks. Historical design CLEAR does not review this amendment.

### Checks actually performed for the Markdown amendment

- Installed Spec Kit plan setup selected the existing feature and preserved
  `plan.md`; task setup resolved the installed template without replacing the
  historical task list. Both used explicit `SPECIFY_FEATURE_DIRECTORY` and
  `SPECIFY_FEATURE_NO_PERSIST=1`, returned exit0, and created no feature-state file.
- Inspected installed skills and prerequisite/helper scripts before use; no
  extension hooks, presets or template overrides are configured at this base.
  Final read-only analysis prerequisite command with `--json --require-spec
  --require-tasks --include-tasks` returned exit0 and listed research, data model,
  contracts, quickstart and tasks for this existing feature.
- Markdown coverage bookkeeping (isolated Python, no imports from repository
  code) found 34 FRs + 8 SCs = 42 unique individual analysis rows, each with valid
  task ids and a literal test/fixture idea. All 44 task ids are unique; 20 retain
  historical checked state, 24 remain unchecked. This is traceability, not test
  execution or acceptance.
- Initial offline documentation check failed because the new analysis pointed
  to this verification section before it was written; that link is now present.
  Initial whitespace check found an added final blank line in tasks; removed.
- Initial scoped `typos specs/015-local-restacking` found the historical abbreviated
  commit id as a false spelling hit. Expanded that reference to its already
  recorded full historical SHA; no review fact or date changed.
- Final `python3 -I -B scripts/check-docs.py`: exit0, 356 authored documents.
  `git diff --check`: exit0. Scoped `typos specs/015-local-restacking`: exit0
  after removing the same historical abbreviated-id spelling hit from this
  explanatory entry. Final exact-head check results are also in the author handoff.
- Existing v1 restack schema digest inspected unchanged:
  `da0bdc60273a30a0f2e39482f534cb80f15f619451f2167ad0780cc44a2b5b48`.
  The amendment changes only the listed Spec 015 Markdown artifacts, with no
  code, tests, dependency, JSON/schema file, command guide/progress or machine
  status update.

### Evidence limits and remaining gates

No Go tests, formatting, vet, race check, module check, `make check`, build,
install, native Git/jj fixture, new mutation control, real-host/browser test,
live API call, push, PR or CI action was run for this amendment. Offline docs
checks are deliberately the authorized check scope, not substitutes for required
future implementation/source checks. Nothing absent is reported as passing.
New test names in tasks/analysis are proposals only, not files or measured runs.

All eight former [owner-choice IDs](spec.md#owner-decisions-2026-10-25) are
settled on2026-10-25. The current profile adopts trusted workspace policy,
validated .work plan metadata, external capture bytes, target exit revision2 and
optional015host-only re-sign; each native/source/helper capability remains
unimplemented or separately measured. This does not change recorded first-slice
execution or permit network/local apply here. Independent exact-head document
review and required PR CI are the coordinator's publication/readiness gates,
not acceptance of local apply. The first-slice T021/T023/T024 and each future
capability's minimum/current conformance remain separate product gates.

### PR 238 correction round (2026-10-25)

Foreman Opus returned NOT CLEAR with F1–F7 on exact original amendment head
`98908cb293b6f6a90ace9a4bbc3ab60ae2b40a94`. The author appended focused commits
for all seven findings, the corrected open 012-network decision provenance,
conservative full-publication-range change classification and minimal config
evidence disclosure. [Analysis](analysis.md#historical-evidence-preservation)
records each correction, fixture idea and remaining gate. No historical commit
was rewritten, no original task checkbox/date changed, and the author did not
push, edit the PR or run CI; root coordinates those separate actions.

Corrected-doc checks actually run: `python3 -I -B scripts/check-docs.py` exit0
(356 authored documents), scoped `typos specs/015-local-restacking` exit0 and
`git diff --check` over the appended correction range exit0. Installed Spec Kit
read-only prerequisite resolution again returned exit0 with this existing
feature selected and no-persist; no extension hooks/presets/overrides exist.
Isolated Markdown bookkeeping found 42 unique FR/SC rows, 44 unique task ids and
157 symmetric row/tag edges; historical 20 checked/24 unchecked remain intact.
Final frozen-head documentation checks are repeated for the coordinator handoff.

No Go/product/native fixture, new signing experiment, build, format/vet/module,
`make check`, browser/live-host/API or mutation test ran in this correction round.
New fixture names and policy fields remain UNPUBLISHED Markdown proposals. V1
schema bytes and source behavior, D4–D9/D12/D13 and T004/T021/T023/T024 limits
remain unchanged. The exact corrected head has no independent CLEAR yet; the
review and green required CI gates remain with the coordinator before Ready.

## Owner-decision amendment status (2026-10-25)

All earlier source execution and original 20/24 checkboxes remain historical
facts. They do not implement decided exit revision2 or host-only optional actual
re-sign. CURRENT root dispatch still returns3 incomplete; T045/T046 are red-first
work to target1 with schema1 bytes preserved. T047/T049/T048 own optional signing tests, separately authorized measurement and implementation;
no helper was run or key accessed in this documentation follow-up.

### Future T049 evidence destination (not executed)

Record the separately authorized host-helper measurement plan, exact executable/
verifier identities and versions, disposable fixture/signer policy, observed
I/O/environment/confinement/bound/cancellation effects, external-agent limits and
bottom-up final signed-chain verification here before enabling T048. Missing or
refused measurements stay explicit blockers; no helper was executed by this fix.
