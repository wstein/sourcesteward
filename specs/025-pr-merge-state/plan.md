# Implementation Plan: Pull request merge-state explanation

**Branch**: `codex/025-pr-merge-state` | **Date**: 2026-10-16 | **Spec**: [spec.md](spec.md)

## Summary

Add `stw hosting merge-state NUMBER` and the same leaf under `gh jj`, one shared operation in `internal/hosting` that reuses Spec 020's native ownership, remote discovery and repository selection, reads nine fixed GitHub REST sources through the Spec 022 bounded transport (extended additively), derives a list of waiting conditions with evidence and an explicit confidence through a pure, table-driven function, and returns one typed result rendered as text or as the new `merge-state` JSON schema. Permission-limited reads become `unknown` with a reason; the result never asserts mergeability. No existing contract changes.

## Technical Context

**Language/Version**: Go 1.27.2; existing supported Git and jj versions (minimum and current lanes); `gh` >= 2.102.0 for authentication only.

**Primary Dependencies**: Existing Cobra v1.10.2, `github.com/cli/go-gh/v2` v2.16.2 (Spec 022), the standard library and the existing JSON Schema test dependency. No new dependency.

**Storage**: None.

**Testing**: Go table tests for the pure derivation; fake HTTP through the Spec 022 `InspectWithTransport`-style seam with real temporary Git and jj repositories; schema fixtures; effect sentinels; `scripts/smoke-hosting.py` extension; existing architecture and documentation checks.

**Target Platform**: Linux amd64/arm64, darwin arm64 as for hosting. CI evidence exists for ubuntu-24.04 amd64 only; other results are local evidence.

**Project Type**: CLI with two thin entry points and shared operations.

**Performance Goals**: 30 seconds total, at most 50 requests, 1 MiB per response and for the result, at most 1000 records per collection, 300 items.

**Constraints**: Fixed GET only, closed endpoint allowlist, no GraphQL, no write, no redirect, selected-host credential only, hostile text, no mergeability claim.

**Scale/Scope**: One selected repository, one explicit pull request, its base branch rules and exact head evidence.

## Constitution Check

Pre-research assessment: PASS. Post-design assessment: PASS.

- **I. Shared contract**: one typed result renders text and versioned JSON; no prompt, editor or pager; exit 0, 1, 2 with a stated mapping.
- **II. Evidence before actions**: the result is evidence with explicit confidence and a constant `verdict: not-asserted`; it authorizes no merge, update or approval.
- **III. Ownership and recovery**: read-only; native ownership is verified first and `--repo` cannot bypass it; no recovery claim is needed.
- **IV. Specify and verify**: failing tests first; independent analysis before code; real temporary Git and jj repositories with fake HTTP; hostile text, bounds, moved heads, cancellation and permission-limited fixtures; effect sentinels including jj working-copy and operation-log checks; no live network in acceptance.
- **V. Tier 1 peers**: Git and jj use the same operation and the Spec 020 resolution; `gh jj` stays jj-only by its existing policy. No capability gap exists between backends for this feature, and the result differs only in the preserved native identity.
- **Scope**: network reads are disclosed in the contract. Merge, update-branch, auto-merge changes, approvals, GraphQL, queue state and publication remain separate specifications. No amendment or exception is required.

## Project Structure

```text
internal/hosting/mergestate.go           # operation: options, orchestration, request budget, resample
internal/hosting/mergestate_result.go    # result types, constants, status and diagnostic mapping
internal/hosting/mergestate_sources.go   # endpoint builders, ref validation, per-source readers and decoders
internal/hosting/mergestate_items.go     # pure derivation: evidence -> items (the Items table)
internal/hosting/operations.go           # prelude extracted as an internal function; no behavior change
internal/hosting/transport.go            # additive: allowlist shapes, typed failure class
internal/hosting/*_test.go               # table tests, fake HTTP, adversarial and sentinel tests
internal/cli/hosting.go                  # leaf registration for both entry points; injection seam extended
internal/cli/hosting_merge_state.go      # flags, text rendering (escaping, wording invariants)
schemas/v1/merge-state.schema.json       # new contract; hosting.schema.json unchanged
internal/cli/testdata/merge-state-*.json # schema-valid ok, incomplete, error and permission-limited observations
internal/contracts/merge_state_test.go   # fixtures against the schema, drift guard, unchanged hosting bytes
scripts/smoke-hosting.py                 # local and hermetic authentication-refusal cases for the new leaf
docs/hosting.md, docs/json-contracts.md, docs/agent-guide.md, ARCHITECTURE.md, docs/README.md, llms.txt, docs/progress.md
```

`internal/cli` renders and never starts a process or opens a network connection; `scripts/check-architecture.py` needs no exception because the CLI already may import `internal/hosting`.

## Design Decisions

1. **Pure derivation core.** Evidence is gathered into one struct (`mergeEvidence`: each source's outcome, failure class and decoded values) and `deriveItems(evidence) []Item` is a pure function that implements the Items table row by row. Every row, every permission combination and every confidence rule is then testable without HTTP, in a table test that names the row it covers. Collection and derivation are separate so a transport change cannot change an explanation.
2. **Prelude extraction.** `inspect()` in `internal/hosting/operations.go` validates options, resolves native remotes, selects the repository and creates the API client in one function. The new operation needs the same steps; they are extracted into an internal function used by both, behavior unchanged and guarded by characterization tests, before any new code depends on it. Required collateral changes: `ValidateOptions` rejects any `Kind` other than repo, pr and ci, so the new operation has its own options type and validation; `hostingCommands`, `hostingCommandWith`, `newRootWithHosting`, `runCLIWithHosting` and the `hosting_transport_test.go` helpers take one `hostingOperation` returning `hosting.Result`, so their signatures grow a second operation of the new result type; the deferred cancel and output-limit handling in `inspect()` is tied to `Result` and is reimplemented for the new result. The diagnostics and `hosting-*` codes stay the same.
3. **Transport extension is additive (ordering with Spec 022).** Spec 022 is implemented on `origin/main` and is a prerequisite. The extension adds: (a) the closed allowlist shapes of the Sources table, an explicit extension of Spec 022 FR-005 and its transport contract (the existing `fixedEndpoint` expression gains the new shapes with their own bounded character classes, and the guard keeps refusing `RawPath`, userinfo, fragments and non-GET); (b) a typed error carrying the failure class from the response status (`401` unauthorized, `403` forbidden unless `X-RateLimit-Remaining: 0` or `Retry-After` is present, then rate-limited, `404` not-found, `429` rate-limited, other non-2xx other-http, an exceeded cap too-large, undecodable shape is decided by the caller as malformed), with unchanged message text; (c) the app id of check runs exposed to the new operation only, leaving the published `Check` and `hosting.schema.json` untouched. These requirements are written as the transport-neutral interface of the contract, so a `gh api`-based transport would satisfy them from the process exit status and the `HTTP NNN` text that `gh api` prints on standard error (seen for a `404` on 2026-10-16); the plan does not depend on go-gh internals; a subprocess transport may not see headers and may classify the header-based rate-limit cases as `other-http` (research). Parallel hosting work (including any Spec 022 follow-up) touching `transport.go` or `operations.go` must rebase; this feature adds code beside, not inside, the existing functions except for the two changes above.
4. **Request order and budget.** Order: `pull-request`; then, when open and the base ref is supported, `repository`, `base-branch`, `branch-protection` and `branch-rules`; then `compare` (needs the branch tip); then `check-runs` and `commit-statuses` when the required set is non-empty or unknown; then `reviews` when a required count is above zero or unknown; finally the resample of `pull-request` and `base-branch`. A counting wrapper around the existing `apiReader` enforces the 50-request cap, counts requests per source for the result and checks every endpoint against the allowlist a second time before the transport sees it.
5. **Pagination.** `branch-rules`, `reviews`, `check-runs` and `commit-statuses` use manual pages of at most 100 records, at most 10 pages. Check runs reuse the `total_count` proof of Spec 020; the compare read is a single request (`page=2`); rules, statuses and reviews have no total, so a page shorter than 100 proves the end and a full tenth page does not (`merge-state-limit`, incomplete), as Spec 020 does for statuses. Duplicate ids are a shape failure.
6. **Confidence is derived in one place.** The Items table is the only source of the effect, confidence and reason of each item; `deriveItems` has no other place that sets them. A test iterates the table's codes and fails if a code lacks a test row, and another fails if any item has `waits` with `unknown`, or an `unknown` item without a reason.
7. **Derived ordering and bounds.** Items follow the table order then subject bytes; all lists are bounded before rendering (300 items, 100 required contexts, 50 rule entries); exceeding a bound is `merge-state-limit` and the result `incomplete`, never a silent truncation.
8. **Resample.** After all other reads the pull request and base branch are read again; head and base SHAs and refs, `state`, `draft`, `merged`, auto-merge method and the branch tip are compared; a difference is `merge-state-changed`. Evidence is not claimed consistent beyond that sample.
9. **Rendering invariants.** `internal/cli/hosting_merge_state.go` renders only fields of the result. The only text that mentions mergeability is the fixed notice and the labelled GitHub line; a test scans every fixture's human and JSON output for forbidden positive wording. Names, contexts, refs, ruleset names and logins are escaped with the existing control-character escaping and rendered with `%q`-style quoting as in `renderHosting`.
10. **Schema.** `merge-state.schema.json` is self-contained; identity, repository and candidate definitions are copied from the hosting schema and compared in a contract test. Per-code `detail` validation uses `if/then`. `hosting.schema.json`, `internal/cli/testdata/hosting-*.json` and all other schemas are compared byte for byte against the base in the tests.
11. **Entry points and injection.** `hostingCommands(requireJJ, run)` registers the new leaf for both `stw hosting` and `gh jj`; the existing operation-injection seam (`runCLIWithHosting`, `InspectWithTransport`) is extended by an equivalent `ExplainMergeStateWithTransport` so CLI tests use real shared operations with fake HTTP, as in Spec 022, with no production endpoint override.
12. **Exit statuses.** `ok` returns normally (exit 0); `incomplete` and `error` return `runtimeFailure` (exit 1) after the full result is written, as `hostingLeaf` does today for non-`ok` results; usage errors exit 2 with no JSON. There is no strict status.
13. **No polling, no auto-retry.** A `null` `mergeable` is reported, not retried, so the operation stays bounded and deterministic.
14. **Conformance.** Fake-HTTP success conformance runs in the Go tests on real native Git (ordinary and linked worktree) and jj (standalone, colocated, secondary) repositories with before/after state sentinels (refs, files, jj working copy and operation log), on the existing minimum and current lanes. The binary smoke (`scripts/smoke-hosting.py`) gains the new leaf for local resolution failures, `gh jj` Git refusal and hermetic authentication refusal, as the existing leaves have.

## Risks

- GitHub's mergeability behavior is partly opaque (measured: `blocked` with no readable cause, lazily `null`); the contract answers with `unknown` items instead of guessing, and the spec lists what stays unverified.
- Permission matrices not measured here (push-but-not-admin, Enterprise, fine-grained tokens) could return shapes the fixtures do not cover; every unexpected shape or status degrades to `unknown` or `incomplete`, never to a claim.
- The `compare` response can be large; a `too-large` outcome makes `head-behind-base` `unknown`. A cheaper source for `behind_by` is not available over REST.
- Rate limits and secondary limits are documented, not measured; `rate-limited` is classified conservatively.
- Rebase risk with parallel hosting work in `transport.go` and `operations.go`; the two additive changes are the first work item so conflicts surface early.
- Item semantics for `neutral`/`skipped`, `changes_requested` and duplicate check names rest on documented behavior and are `inferred` for that reason.
