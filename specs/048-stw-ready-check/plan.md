# Plan: `stw ready-check` (Spec 048)

Spec-light. One PR with a commit series.

1. `internal/readycheck/check.go`: pure `Evaluate(Facts) []Condition`, `Ready`. Tests per condition (pass, fail, unknown) first.
2. `internal/readycheck/run.go`, `command.go`: `Run` gathers the facts through `Source` (the watch reader plus `RepoInfo`) and a
   `ReviewFunc`; `Prepare` and `Execute` validate, print and map exits. Fake forge tests, including that no head means no checks read.
3. `internal/cli/readycheck.go`: the cobra command over `hosting.NewPullsReaderWithTransport`; end-to-end test over the guarded
   transport (GET only, four requests, exit 0/1/2).
4. `schemas/v1/ready-check.schema.json`, contract tests (valid, broken, real output), freeze digest, Pages exclusion.
5. Docs: `docs/commands.md`, `docs/json-contracts.md`, `docs/feature-status.json`.

Follow-up once Spec 046 is merged: replace `readycheck.ReviewUnavailable` in `internal/cli/readycheck.go` by a `ReviewFunc` over
`internal/reviewstate` and add the comment read to the run; a test with a CLEAR, carried, stale and findings comment thread.
