# Tasks: `stw tidy` janitor wizard (Spec 044)

**Input**: [specification](spec.md), [plan](plan.md). Tests are mandatory: write the failing test, watch it fail for the right reason, then
implement. A box is ticked only by the pull request that implements the task, with its evidence in the pull request body or a per-package file (never `tasks.md` or `docs/progress.md`; those are set by the last pull request of a wave). Nothing here builds, releases, tags, applies a plan to a real repository or pushes the default branch. All boxes are open:
this pull request is the specification only.

**Gate**: no task starts before the independent review of this specification is CLEAR. All owner decisions D1 to D15 are made (2026-10-26); D12 (one typed word per run) additionally needs T012c before any code of T013s.

## Phase 1 (v0.2.0)

### P1 risk

- [ ] T001 Failing table test for the ladder (SC-003): strong and equivalent proofs, `no-work`, C1 changes nothing, R1, R2, R4, the cap at orange, every red cause (including a record-missing row with proof `none`, not judged, dirty, extras); expose the observed dirt facts to the risk function; then implement `internal/tidywizard/risk`.
- [ ] T002 Failing test and implementation for the family rule and group keys (leading ASCII lowercase letters of the first path component below `/tmp`, so `/tmp/workharbor-baseline-x/checkout` is `workharbor`; at least 3 members; scratchpads; a key is `[a-z]` only, hostile names).
- [ ] T003 Failing test and implementation for the group level (worst row) and the counts per level.

### P2 evidence

- [ ] T004 Failing test and implementation of the upstream-gone fact from the local reference database for branches (no network, no write); it adds text only and changes no level; an unreadable state is "not known".
- [ ] T005 Failing tests with a 7-member, about 466-tip hosting double (SC-005): merged head makes strong with `pr`; `unmerged` with a merged head stays red; per-member, per-run and tip caps; rate limit stops the run's online calls; every failure, not authenticated and non-github.com fall back offline with one notice and the same exit code; the double fails on any write method. Implement over the existing discovery.
- [ ] T005a Failing tests and implementation of the online default for the human wizard only (D13): `--offline` (accepted on `stw tidy` and `stw tidy plan`; refused with exit 2 in the expert layer), `--online --offline` exit 2, `plan`, `--json`, `--save`, agent mode and the expert layer stay offline by default; the `derived[]` `online` entry; flag classification of `--offline` and `--rows`.

### P3 render

- [ ] T006 Failing test and implementation of the sentence table for every reason, proof and risk word; a code without a sentence or an action fails (SC-010).
- [ ] T007 Failing golden tests (SC-007, SC-012) and implementation of the grouped overview, the retain table and `--rows`; the old row view is `--rows`.
- [ ] T008 Failing tests and implementation of the additive JSON fields (`risk`, `risk_reasons`, `group`, `pr`, `summary` per level), schema, fixtures and freeze digest; classify `--rows` in the flag test (SC-011).

### P4 steps

- [ ] T010 Failing grammar table test (SC-006) and implementation of the selection parser: numbers, ranges, selectors, `except`, junk, 257 bytes, red numbers.
- [ ] T011 Failing tests (SC-001, SC-002, SC-004) and implementation of janitor choosability, `a` as all green and `decision` rows by number only; agent mode byte-identical to Spec 041.
- [ ] T012 Failing transcript tests and implementation of the group steps: keys, defaults by level, `l`, `r`, `q`, three invalid answers, EOF is nothing chosen.
- [ ] T012c **Independent security review before code** of the D12 design (receipt, binding to the plan id, single use, other runs, agent path; the questions in the spec) and, after T013a, of the exact code head.
- [ ] T013a Failing tests (SC-014) and implementation of the confirmation receipt and the apply-path change; the separate apply and the agent path are byte-identical to Spec 041.
- [ ] T013 Failing tests (SC-008) and implementation of the final summary, whose last line states the directories deleted for good and the branches, directly before the unchanged typed prompt (and in the apply question); sorted typed words; the prompt text byte-equal; wrong word, bare `y`, stdin input and no terminal refuse and store nothing.
- [ ] T014 Failing tests (SC-009) for hostile names in every screen, group key and example row; narrow terminal, no terminal, all red, nothing to clean, interrupt (edge-case table).

### P5 docs

- [ ] T015 `docs/tidy.md` wizard section with the amendment list against Spec 041, the ladder, the grammar and the retain table (examples pasted from the golden); `docs/commands.md`, `docs/json-contracts.md`, `docs/roadmap.md`, `docs/feature-status.json`; status line in `docs/progress.md` only by the last pull request of the wave; pointers in Specs 041, 031 and 029; `python3 scripts/check-docs.py` clean.
- [ ] T016 Dogfood dry run on a copy of the live workspace (no apply), recorded in the pull request body; independent review of the exact head.

## Phase 2 (v0.3.0)

- [ ] T017 Failing tests and implementation of `stw workspace done TASK [--merged PR]` (SC-013): additive `done` block, idempotent, state directory only, attribution gate (Spec 042 D5), schema check.
- [ ] T018 Failing tests and implementation of C3 and R3 in the ladder; a done mark removes R2 and R3, never lifts above yellow, and never makes a row choosable without a proof.
- [ ] T019 Failing tests and implementation of `stw tidy adopt PATTERN=OWNER` with its safeguards (unlocked worktrees only, human mode, absolute glob, existing linked worktrees only, listing and `y`, adoption record, owner_source `adopted` in the overview schema, never a verdict, never `task-record`, no effect on stw-locked record-missing rows).
- [ ] T020 Revisit D8 (`github-merged-head` as a wizard proof) only after the dogfood run of phase 1; implement only what the owner approves then.
- [ ] T022 TUI checklist (D10, decided for phase 2): pinned dependency review first, the line wizard stays the fallback; failing tests for no terminal, narrow terminal and `NO_COLOR`.
- [ ] T021 Workspace `AGENTS.md` step for agents (D11, the owner's file, owner's approval); docs for phase 2; independent review of the exact head.
