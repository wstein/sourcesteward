# Validation guide: bounded official transport

Prerequisites: Go 1.27.2, Git, gh >=2.102.0, supported jj versions and existing native sandbox prerequisites. Use a disposable worktree and repository-owned test fixtures; no live token or API access is required.

1. Run `make check` before edits and record any baseline failure.
2. Run focused failing-first transport tests using `go test ./internal/hosting`; verify auth output limits, raw/normalized success/error limits, redirects, host isolation, cancellation and repository-linked config refusal.
3. Run `go test ./internal/cli ./internal/contracts` for equivalent shared operation output, native failure diagnostics and existing schema compatibility. Compare published schema files against the PR base; no schema changes are expected.
4. Run `make check`, `go test -race ./...`, `go mod verify` and `git diff --check` after integration.
5. Run the repository's minimum/current native Git/jj conformance lanes, including in-process fake HTTP PR/CI success and unchanged-state sentinels; run private executable/extension dispatch checks using authorized ignored local test builds. Record commands and exact versions in verification.md.

Expected outcomes: remote observations use fixed selected-host SDK GETs; local resolution works without authentication; overflow/cancellation/invalid evidence refuses; both native backends retain read-only effects and published JSON shapes. Tests use fixture tokens only. Missing/skipped native checks remain explicit handoff gaps; do not report them as passing. No extension installation in the user profile, release, live API call or deletion is part of validation.
