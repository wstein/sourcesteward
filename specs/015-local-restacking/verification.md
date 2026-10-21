# Verification: `stw restack plan` (first slice)

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

- Git 2.46.0 and the pinned CI lanes; macOS; the jj lanes (only the `.jj` directory refusal ran). The quarantine behaviour of the reviewer experiment on other Git versions and pack-threshold packing are unmeasured.
- Ambiguous and GitButler ownership have no offline fixture.
- The 60 s time bound is tested by deadline expiry, not by waiting.
- `git range-diff` output is parsed by its line grammar; a Git version that prints another form yields `unknown` marks, never `=`.
- The prediction is not a rebase: renames beyond merge-ort defaults, rerere, whitespace options and commit encodings can differ (`prediction-is-not-apply`).
- No build, release, tag, install, push of anything but the topic branch or network use happened.
