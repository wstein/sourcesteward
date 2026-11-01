# Tasks: `stw lint pr` (Spec 050)

- [x] T1 Rule table and engine with a test per rule and mutation proof (`internal/lintpr`).
- [x] T2 Lint guard mode and comment shape (`internal/hosting/lint_reads.go`).
- [x] T3 Command, document, schema `lint-pr.schema.json` and CLI wiring (`internal/lintprcmd`, `internal/cli/lint.go`).
- [x] T4 Docs: `docs/lint.md`, `docs/commands.md`, `docs/json-contracts.md`, `docs/hosting.md`, `docs/progress.md`.
- [ ] T5 Independent review of the exact head and the owner's acceptance.
- [ ] T6 Follow-ups: use `internal/reviewstate` for the review line after Spec 046 lands; a CI job with a checkout of agent-rules for the anchor test.
