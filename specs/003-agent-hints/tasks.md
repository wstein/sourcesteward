# Tasks: Agent Hints

**Input**: [spec.md](spec.md), [plan.md](plan.md), research, data model and help contract.
**Tests**: Failing behavior tests first, required by repository policy. Checked boxes record completed preparation only; implementation stays unchecked until evidenced.

## Phase 1: Setup
- [x] T001 Read repository policy and resolve templates; create the feature artifacts in `specs/003-agent-hints/`.
- [x] T002 Obtain independent artifact review of `specs/003-agent-hints/spec.md`, `plan.md` and `tasks.md` before implementation.

## Phase 2: Foundational
- [x] T003 Confirm baseline checks and current command outputs; record any failures in `docs/progress.md`.

## Phase 3: User Story 1 — Discover guidance (P1)
**Independent test**: All four root-help forms show exactly one notice; version/subcommand/error cases preserve existing output and status.
- [x] T004 [US1] Add failing notice/absence tests in `internal/cli/root_test.go`, including unchanged version JSON and a writer failing only during the appended notice.
- [x] T005 [US1] Append the root-only notice in `internal/cli/root.go`; preserve ordinary help, stored help-write errors and the single diagnostic boundary.
- [x] T006 [US1] Exercise success, early/late write failure and cancellation cases in `internal/cli/root_test.go` and real `cmd/stw` invocation.

## Phase 4: User Story 2 — Use accurate guidance (P2)
**Independent test**: Build and execute the guide's supported examples; inspect claims against current help/version implementation.
- [x] T007 [P] [US2] Add `docs/agent-guide.md` with setup/use, JSON, exit meanings, task relevance and authority limits; mark backend/cleanup/recovery operations planned.

## Phase 5: User Story 3 — Validate destinations offline (P3)
**Independent test**: Local targets pass and missing raw-URL targets or broken index links fail source checks without network access.
- [x] T008 [US3] Add failing positive/negative destination coverage in `scripts/test_docs.py` for the two exact raw URLs and root `llms.txt` links.
- [x] T009 [US3] Extend `scripts/check-docs.py` with repository-owned URL mapping and offline index validation without weakening existing checks.
- [x] T010 [US3] Add root `llms.txt` troubleshooting/navigation links and connect `README.md`, `docs/README.md` and `docs/development.md` to the guide/index.

## Phase 6: Polish and handoff
- [x] T011 Run formatting, `make check`, real help/version examples and `git diff --check`; record actual evidence in `docs/progress.md` and feature evidence.
- [ ] T012 Obtain independent implementation review, resolve findings and update `docs/feature-status.json` and `specs/003-agent-hints/tasks.md` with verified results.
- [ ] T013 Make focused Conventional Commits for reviewed source changes; honor task authorization before any publication and preserve current Pages packaging scope.

## Dependencies and parallel execution
T001 → T002 → T003 precede implementation. T004 must fail before T005; T006 follows T005. T007 can run alongside the CLI story after review. T008 must fail before T009/T010; documentation authors coordinate ownership of index/checker files. T011 follows all three stories, then T012 → T013.

MVP: US1 plus real destination files is the smallest useful discovery increment; all three stories are required for this task's complete, checked handoff. No roadmap operation is implemented by this task list.
