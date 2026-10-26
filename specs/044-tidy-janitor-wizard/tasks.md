# Tasks: `stw tidy` janitor wizard (Spec 044)

**Input**: [specification](spec.md), [plan](plan.md). Tests are mandatory: write the failing test, watch it fail for the right reason, then
implement. A box is ticked only by the pull request that implements the task, with its evidence in `docs/progress.md` (last pull request
of a wave). Nothing here builds, releases, tags, applies a plan to a real repository or pushes the default branch. All boxes are open:
this pull request is the specification only.

**Gate**: no task starts before the independent review of this specification is CLEAR and the owner has answered D1 to D5 (the others may
follow).

## Phase 1 (v0.2.0)

### P1 risk

- [ ] T001 Failing table test for the ladder (SC-003): strong and equivalent proofs, `no-work`, each corroboration, R1, R2, R4, the cap at orange, red reasons; then implement `internal/tidywizard/risk`.
- [ ] T002 Failing test and implementation for the family rule and group keys (letters of the directory name, at least 3 members, scratchpads, hostile names fall to "other").
- [ ] T003 Failing test and implementation for the group level (worst row) and the counts per level.

### P2 evidence

- [ ] T004 Failing test and implementation of the upstream-gone fact from the local reference database for branches (no network, no write); an unreadable state is "not known", never strong.
- [ ] T005 Failing tests with the hosting double (SC-005): merged head makes strong with `pr`; `unmerged` with a merged head stays red; failure, 403 and incomplete collection change nothing and add a notice; request count within Spec 031 FR-018; the double fails on any write method. Implement over the existing discovery.

### P3 render

- [ ] T006 Failing test and implementation of the sentence table for every reason, proof and risk word; a code without a sentence or an action fails (SC-010).
- [ ] T007 Failing golden tests (SC-007, SC-012) and implementation of the grouped overview, the retain table and `--rows`; the old row view is `--rows`.
- [ ] T008 Failing tests and implementation of the additive JSON fields (`risk`, `risk_reasons`, `group`, `pr`, `summary` per level), schema, fixtures and freeze digest; classify `--rows` in the flag test (SC-011).
- [ ] T009 Failing test and implementation of the `you` label from the global Git identity (FR-009): unreadable means none; scope unchanged.

### P4 steps

- [ ] T010 Failing grammar table test (SC-006) and implementation of the selection parser: numbers, ranges, selectors, `except`, junk, 257 bytes, red numbers.
- [ ] T011 Failing tests (SC-001, SC-002, SC-004) and implementation of janitor choosability and `a` as all green; agent mode byte-identical to Spec 041.
- [ ] T012 Failing transcript tests and implementation of the group steps: keys, defaults by level, `l`, `r`, `q`, three invalid answers, EOF is nothing chosen.
- [ ] T013 Failing tests (SC-008) and implementation of the final summary before the unchanged typed prompt; the prompt text byte-equal; wrong word, bare `y`, stdin input and no terminal refuse and store nothing.
- [ ] T014 Failing tests (SC-009) for hostile names in every screen, group key and example row; narrow terminal, no terminal, all red, nothing to clean, interrupt (edge-case table).

### P5 docs

- [ ] T015 `docs/tidy.md` wizard section with the amendment list against Spec 041, the ladder, the grammar and the retain table (examples pasted from the golden); `docs/commands.md`, `docs/json-contracts.md`, `docs/progress.md`, `docs/roadmap.md`, `docs/feature-status.json`; pointer in Spec 041; `python3 scripts/check-docs.py` clean.
- [ ] T016 Dogfood dry run on a copy of the live workspace (no apply), recorded in `docs/progress.md`; independent review of the exact head.

## Phase 2 (v0.3.0)

- [ ] T017 Failing tests and implementation of `stw workspace done TASK [--merged PR]` (SC-013): additive `done` block, idempotent, state directory only, attribution gate (Spec 042 D5), schema check.
- [ ] T018 Failing tests and implementation of C3 and R3 in the ladder; a done mark without a proof never makes a row choosable.
- [ ] T019 Failing tests and implementation of `stw tidy adopt PATTERN=OWNER` with its safeguards (human mode, absolute glob, existing linked worktrees only, listing and `y`, adoption record, never a verdict, never `task-record`).
- [ ] T020 Owner decisions D8 (`github-merged-head` as a wizard proof) and D10 (TUI) recorded; implement only what was approved.
- [ ] T021 Workspace `AGENTS.md` step for agents (D11, the owner's file, owner's approval); docs for phase 2; independent review of the exact head.
