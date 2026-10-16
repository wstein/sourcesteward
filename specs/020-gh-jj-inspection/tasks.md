# Tasks: Shared GitHub inspection and gh jj entry point

**Input**: [spec.md](spec.md), [plan.md](plan.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/hosting.md](contracts/hosting.md), [contracts/transport.md](contracts/transport.md).
**Tests**: Required by user/policy. Write behavior tests and observe failure before implementation. Task checkboxes record execution, not design approval; unchecked implementation tasks are not unchecked specification-quality gates.

## Phase 1: Setup

- [X] T001 Record pre-implementation cross-artifact analysis and requirement coverage in specs/020-gh-jj-inspection/analysis.md; resolve findings before code and preserve independent review provenance.

## Phase 2: Foundation

- [X] T002 Define shared Options/Result and ValidateOptions in internal/hosting/hosting.go, with schema_version1, nullable observation objects, non-null arrays, explicit complete/network boundaries and usage validation; begin with failing tests in internal/hosting/hosting_test.go (FR-001, FR-008, FR-010).

## Phase 3: US1 — Local repository resolution (P1, MVP)

**Independent test**: Temporary Git/worktree and native colocated/standalone/secondary jj resolve unchanged local state with gh absent; ambiguity and wrong ownership refuse.

- [X] T003 [P] [US1] Add failing native remote tests then implement inspection.Remotes(ctx, identity Result) RemoteResult in internal/inspection/remotes.go and internal/inspection/remotes_test.go, preserving native sandbox/config isolation, bounded fetch/push observations and before/after identity/remote consistency (FR-002, FR-004).
- [X] T004 [US1] Add failing URL/selection tests then implement local resolution in internal/hosting/resolve.go and internal/hosting/resolve_test.go: host defaults github.com, explicit host is validated, no origin/main assumption, owner/name and remote selectors, deduplication/provenance, credential/query/fragment rejection and incomplete/ambiguous refusal (FR-003, FR-004).

## Phase 4: US2 — Explicit PR inspection (P1)

**Independent test**: Fake gh records only fixed GET paths; numbered PR identity validates; authentication/missing/malformed/cancelled evidence refuses without exposing secrets.

- [X] T005 [P] [US2] Add failing transport process tests then implement bounded neutral-cwd gh adapter in internal/hosting/gh.go and internal/hosting/gh_test.go; support initial tested gh 2.102.0 compatibility, 30-second total deadline, 1 MiB stdout/stderr limits, trusted executable resolution, sanitized environment and declared user auth inputs (FR-006, FR-007, FR-008).
- [X] T006 [US2] Add failing numbered PR tests then implement internal/hosting/pr.go and internal/hosting/pr_test.go, requiring positive explicit number, matching base repository and full head/base SHA, owner/name head/base repository strings, bounded sanitized diagnostics and no heuristic mapping (FR-005, FR-010, FR-011).

## Phase 5: US3 — Exact-head CI inspection (P1)

**Independent test**: Deterministic paginated checks/statuses at exact PR head preserve individual evidence; moved heads, duplicate IDs, inaccessible data and limit exhaustion refuse completeness.

- [X] T007 [US3] Add failing CI tests then implement internal/hosting/ci.go and internal/hosting/ci_test.go: fixed exact-SHA endpoints, manual at most 10 pages per collection and 1000 combined records, duplicate/shape/total checks, final PR identity resample, partial evidence with complete=false on failure and no merge/pass aggregate (FR-008, FR-009).

## Phase 6: US4 — Shared CLI/schema and conformance (P1)

**Independent test**: Shared normalized JSON/human observations through both entry points; extension rejects Git; real gh dispatch works in isolated private installation; old schemas remain valid.

- [X] T008 [P] [US4] Add failing command tests then implement shared parsing/rendering in internal/cli/hosting.go and internal/cli/hosting_test.go plus thin cancellation entry point cmd/gh-jj/main.go; preserve exit0/1/2 and escaped human text with no CLI process/network execution (FR-001, FR-002, FR-010).
- [X] T009 [P] [US4] Define schemas/v1/hosting.schema.json and real success/error fixtures in internal/cli/testdata/; validate offline complete output including retained partial failures, nullability, error diagnostics and 1 MiB final serialization bound while regressing existing published contracts (FR-008, FR-010).
- [X] T010 [US4] Add make extension -> bin/gh-jj/gh-jj in Makefile; document optional local directory installation and extension-name collision in docs/hosting.md, without installing in the real user profile or creating release automation (FR-011).
- [X] T011 [US4] Extend scripts/check-architecture.py and native conformance scripts/CI to cover both command entry points, minimum/current Git/jj ownership/effect sentinels and real gh dispatch in temporary GH_CONFIG_DIR; mandatory missing tools/execution block acceptance rather than skip (FR-004, FR-012).

## Phase 7: Review and handoff

- [X] T012 Run quickstart scenarios and independent source/security/contract review, fix findings, run make check plus race/native conformance, and record exact actual evidence and remaining hosted gates in specs/020-gh-jj-inspection/verification.md (FR-012, SC-001–SC-004).
- [X] T013 Update ARCHITECTURE.md, docs/README.md, docs/json-contracts.md, docs/agent-guide.md, docs/progress.md, docs/feature-status.json and llms.txt with bounded implemented capabilities and deferred release/mutation scope; commit focused Conventional Commits and open a PR without merge/release/live installation or remote mutation (FR-010, FR-011).

## Dependencies and execution order

T001 is the design-analysis gate before implementation. T002 establishes shared types. T003 and T005 may proceed in separate worktrees after T002; T004 needs T003, T006 needs T004/T005, T007 needs T006. CLI/schema work T008/T009 may proceed against frozen types in separate owned files while core proceeds. T010 follows CLI entry point; T011 follows integrated operations and dispatch. T012/T013 complete review and durable handoff. No unchecked acceptance execution is silently called complete.

US1 is the local MVP. US2 extends explicit read requests using US1; US3 adds exact-head CI through US2. US4 integrates all stories rather than introducing alternate hosting logic.

## Parallel examples

- US1: native adapter author owns internal/inspection/remotes*; resolver author owns internal/hosting/resolve* after interface freeze.
- US2: transport author owns internal/hosting/gh* while CLI author prepares argument tests in internal/cli/hosting_test.go; avoid overlapping shared result files.
- US3: CI author owns internal/hosting/ci* while reviewer authors independent malformed/pagination fixtures before source review.
- US4: CLI author owns internal/cli and cmd/gh-jj; schema author owns schemas/v1/hosting.schema.json and contract fixtures; conformance author owns scripts only after command API freezes.

## Implementation strategy

Commit analyzed specification/design first, native observations and shared hosting operations next, entry point/schema next, then conformance/docs/evidence. Test-first each logical slice; preserve atomic focused Conventional Commits. Open one implementation PR after integrated independent review and local checks, with exact remaining hosted evidence explicit. No mutation or release WI is smuggled into read-only scope.

Task count: 13 (setup 1, foundation 1, US1 2, US2 2, US3 1, US4 4, review/handoff 2).
