# Tasks: Pull request merge-state explanation

**Input**: `spec.md`, `plan.md`, `research.md`, `data-model.md`, `contracts/merge-state.md`, `analysis.md`.
**Tests**: Required by constitution principle IV. Write each behavior test and observe it fail before the implementation. Task checkboxes record execution, not design approval.
**Work items**: Tasks are grouped into five work items, WI1 to WI5. Each is one reviewable change that builds on the previous one and ends with `make check` green; WI order is the stacking order of the pull requests. Each task names the requirements (FR) and stories (US) it covers; success criteria (SC) are named where a task is their evidence. A source-only run never counts as acceptance for a lane it did not execute.

## Phase 1: Analysis

- [ ] T001 Record independent cross-artifact analysis in `specs/025-pr-merge-state/analysis.md`, resolve every critical, high and medium finding before any code, and preserve review provenance.

## WI1 — Transport and prelude prerequisites (additive, no behavior change)

- [ ] T002 [FR-007, FR-013, US2] Add failing tests then implement the typed failure class in `internal/hosting/transport.go`: `401` unauthorized, `403` forbidden, `403` with `X-RateLimit-Remaining: 0` and `429` rate-limited, `404` not-found, other non-2xx other-http, over-cap too-large, transport and canceled; retrievable with `errors.As`; existing error messages byte-identical (existing transport tests stay unchanged and green).
- [ ] T003 [FR-004, FR-008, FR-013, SC-005] Add failing tests then extend the closed allowlist to exactly the Sources table shapes (`repos/O/N`, `branches/BASE`, `branches/BASE/protection`, `rules/branches/BASE?per_page=100&page=1..10`, `compare/TIP...SHA?per_page=1&page=1`, `pulls/NUM/reviews?per_page=100&page=1..10`). Refusal tests for every other shape before credentials are attached: non-GET, extra or reordered query, page 11, uppercase or short SHA, ref outside the supported set (control bytes, `..`, `%`, `#`, `?`, leading `.` or `-`, `.lock`, 201 bytes), encoded slash, userinfo, fragment, opaque URL, other host or port, redirects, compressed bodies; the existing guard rules stay.
- [ ] T004 [FR-002, FR-016] Add failing characterization tests for the current `inspect()` prelude (options, native remotes, candidate selection, API client creation, every `hosting-*` diagnostic and `boundary.network` value), then extract it as an internal function used by `inspect()` and the new operation; all existing hosting, CLI and contract tests and the hosting schema bytes stay unchanged.
- [ ] T005 [FR-005] Add failing tests then expose the check-run `app.id` to the new operation through an internal decoder; the published `Check` struct and `hosting.schema.json` are unchanged (byte comparison against the base).

## WI2 — Result, schema and pure derivation

- [ ] T006 [FR-011, SC-007] Add failing schema fixture tests then add `schemas/v1/merge-state.schema.json` and `internal/contracts/merge_state_test.go` with `ok`, `incomplete`, `error`, permission-limited, closed-pull-request and incident fixtures in `internal/cli/testdata/merge-state-*.json`; per-code `detail` validation; constants for `verdict`, `kind` and bounds; a drift guard that the copied identity, repository and candidate definitions equal the hosting schema's; a byte comparison that `hosting.schema.json`, the `hosting-*.json` fixtures and every other schema are unchanged.
- [ ] T007 [FR-005, FR-006, US1, US2, US3, US5] Add failing table tests then implement the result types and `deriveItems` in `internal/hosting/mergestate_result.go` and `mergestate_items.go`: one named test row per Items table row and per confidence branch, including `head-behind-base` under strict true, false, unreadable with `behind` and unreadable otherwise; `required-check` states with pinned and unpinned app ids, duplicate and mixed-app records, status-only contexts and unreadable collections; `merge-methods` known and hidden; `auto-merge-method-not-allowed` only from a readable source; `review-required` with counts below, at and above the requirement and with changes requested; `classic-protection-unreadable`; `blocked-unexplained` precedence; closed pull request suppression. A coverage test fails if an Items table code has no row.
- [ ] T008 [FR-005, FR-009, FR-011, SC-004] Add failing tests then implement the invariants of the data model: `waits` never `unknown`, `unknown` if and only if a reason, source outcome and reason mapping from failure classes, status and diagnostic mapping, summary arithmetic, deterministic ordering, bounds (300 items, 100 required contexts, 50 rulesets) yielding `merge-state-limit`, and the rule that no item is derived from a `denied`, `not-found` or `failed` source except as `unknown`.

## WI3 — Sources and orchestration

- [ ] T009 [FR-004, FR-008, FR-009, FR-012, US6] Add failing tests then implement endpoint builders, base-ref validation and per-source decoders in `internal/hosting/mergestate_sources.go` with a malformed-shape table: missing or mistyped fields, invalid UTF-8, non-hex or short SHAs, unknown merge method strings, `null` settings, duplicate ids, `total_count` mismatch, short and full pages, a tenth full page, oversized bodies.
- [ ] T010 [FR-004, FR-009, FR-010, US1, US6] Add failing tests then implement `ExplainMergeState` and `ExplainMergeStateWithTransport` in `internal/hosting/mergestate.go`: request order and dependencies, the counting wrapper with the 50-request cap and per-source counts, skipping rules (`not-needed`, `skipped-not-open`, `ref-unsupported`), pagination bounds, the resample and `merge-state-changed` for head, base, state, draft, auto-merge and tip changes, cancellation and the 30-second deadline, the 1 MiB result bound, and the incident fixture end to end.
- [ ] T011 [FR-007, SC-004, US2] Add failing permission-matrix tests replaying the measured shapes of research.md (M6 to M10): admin full read; read-only on classic-protected, ruleset-only and unprotected branches; anonymous `401`; `null` repository settings; each optional source `denied`, `not-found`, `rate-limited` and `failed`; the pull request itself `404`. Assert no fixture yields an absence claim, a wrong strict, review or merge-method value, or an `ok` status where a source failed.
- [ ] T012 [FR-012, SC-005, US6] Add failing hostile-input tests: titles, context names, refs, ruleset names, logins and `mergeable_state` with control bytes, ANSI, bidirectional controls and over-length text; response bombs and redirects; compressed bodies; a credential-bearing error body; a repository URL pretending to be an endpoint. Prove nothing becomes an endpoint, command or markup, no raw body or token reaches a diagnostic, and every recorded request is an allowlisted `GET`.

## WI4 — CLI, rendering and Tier 1 conformance

- [ ] T013 [FR-001, FR-006, FR-011, SC-003, US3, US4] Add failing CLI tests then implement the `merge-state` leaf in `internal/cli/hosting_merge_state.go` and register it in `internal/cli/hosting.go` for `stw hosting` and `gh jj`: argument validation (usage exit 2, no JSON), the flags of Spec 020, human output grouped as in the contract with escaping, JSON from the same result, exit statuses 0 and 1, the wording invariants (fixed notice, labelled GitHub line, forbidden positive wording over every fixture's human and JSON output), and the extended injection seam.
- [ ] T014 [FR-003, FR-014, FR-015, SC-001, SC-006, US4] Add failing native conformance tests in `internal/contracts` using real temporary repositories and fake HTTP: ordinary and linked Git worktrees, standalone, colocated and secondary jj; `gh jj merge-state` on Git refuses; ambiguous, GitButler and bare refusals happen before any request; the incident fixture yields the same items on both backends through both entry points; sentinels for refs, files, the jj stored working copy and operation log, and a transport recorder proving only allowlisted GETs; both pinned lanes.
- [ ] T015 [FR-015] Extend `scripts/smoke-hosting.py` (and its existing unit tests) with the new leaf for local resolution failures, `gh jj` refusal on Git and hermetic authentication refusal (`hosting-api-unavailable`, `network` false); wire nothing new into `.github/workflows/ci.yml` beyond the existing smoke call and add no platform.

## WI5 — Documentation and acceptance evidence

- [ ] T016 [FR-006, FR-014] Update `docs/hosting.md` (command, sources, items, confidence, permission limits, what the output does not prove), `docs/json-contracts.md`, `docs/agent-guide.md`, `ARCHITECTURE.md`, `docs/README.md`, `llms.txt` and the generated command reference through `cmd/stw-docs` and the existing documentation tests; `python3 scripts/check-docs.py` passes. Do not run a site build without approval.
- [ ] T017 [FR-015, SC-002, SC-008] Run `make check`, `go test -count=1 -race ./...`, `go mod verify`, `python3 scripts/check-architecture.py`, `git diff --check` and the smoke script on the existing lanes; record exact versions, platforms and outcomes in `specs/025-pr-merge-state/verification.md`, labelling arm64 and darwin results as local evidence and listing everything not run, including the unverified items of research.md that stayed unverified.
- [ ] T018 Update `docs/progress.md` and `docs/feature-status.json` (`planned` to `implemented`, evidence link) with the actual evidence; leave release, tag, publication, live-network acceptance, GraphQL, update-branch and merge automation out of this feature.

## Dependencies and parallel work

T001 gates all code. WI1 first: it touches `transport.go` and `operations.go` and must rebase over any parallel hosting work; T002 and T003 share `transport.go` and run in one worktree, T004 and T005 may follow in parallel. WI2 depends on WI1 only for T005 (the app id) and may start the schema and derivation work (T006 to T008) in parallel once the type names are frozen. WI3 needs WI1 and WI2. WI4 needs WI3; T014 and T015 may run in parallel after T013. WI5 last. A task whose failing test cannot be observed to fail is not complete.

## Coverage map

| Requirement or criterion | Tasks |
| --- | --- |
| FR-001 | T013 |
| FR-002 | T004, T014 |
| FR-003 | T014 |
| FR-004 | T003, T009, T010 |
| FR-005 | T005, T007, T008 |
| FR-006 | T007, T013, T016 |
| FR-007 | T002, T011 |
| FR-008 | T003, T009 |
| FR-009 | T008, T009, T010 |
| FR-010 | T010 |
| FR-011 | T006, T008, T013 |
| FR-012 | T009, T012 |
| FR-013 | T002, T003 |
| FR-014 | T014, T016 |
| FR-015 | T014, T015, T017 |
| FR-016 | T004, T017 |
| SC-001 | T010, T014 |
| SC-002 | T017 |
| SC-003 | T013 |
| SC-004 | T008, T011 |
| SC-005 | T003, T012, T014 |
| SC-006 | T014 |
| SC-007 | T006 |
| SC-008 | T017 |
