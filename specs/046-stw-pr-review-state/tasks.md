# Tasks: Spec 046

Tests are mandatory: failing test first. A box is ticked by the pull request that implements the task.

## PR 1 (review state)
- [x] T001 `internal/reviewstate`: strict grammar, unparsed detection (tests, mutations).
- [x] T002 `internal/reviewstate`: state machine, chains, trust rules 1 to 5, hints (tests, mutations).
- [x] T003 `internal/hosting`: comment and closed-list shapes, guard rows for all modes, decoders, reader methods.
- [x] T004 `internal/pulls`: review pass, default branch, closed list, filters, summary.
- [x] T005 `internal/pullscmd`: flags, validation, JSON document and human column; schema members and digest; contract tests.
- [x] T006 `internal/statusremote`, `internal/statuscmd`: counts in the `pulls` source; schema members and digest.
- [x] T007 Docs: commands, pulls, status, json-contracts, hosting, README, progress, feature status.

## PR 2 (owner list)
- [x] T008 `--mergeable`: order, reason, `then`, footer, document member, docs (PR 2).

## PR 3 (stack view)
- [x] T009 `--stack`: forest, rendering, footer, document member, docs (PR 3).
