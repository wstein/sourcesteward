# Tasks: Bounded official go-gh transport

**Input**: spec.md, plan.md, research.md, data-model.md and contracts/transport.md.
**Workflow**: design → tasks → independent analyze → failing tests → implement → independent review/verification.

## Phase 1: Setup

- [x] T001 Record baseline checks and pre-code independent artifact analysis in specs/022-bounded-go-gh/analysis.md and verification.md; resolve blocking findings before code.

## Phase 2: Foundations

- [x] T002 Pin github.com/cli/go-gh/v2 v2.16.2 in go.mod/go.sum and review dependency/license footprint in specs/022-bounded-go-gh/research.md.

## Phase 3: US1 — Authenticated inspection

Goal: preserve selected-host credentials and existing native operation evidence.
Independent test: authenticated fixed HTTP fixtures yield equivalent PR/CI Result fields.

- [x] T003 [US1] Write failing selected-host routing/auth/preflight tests in internal/hosting/transport_test.go, including 16 KiB-capped token validation and neutral-cwd gh auth token --hostname HOST and local repo mode without gh.
- [x] T004 [US1] Implement explicit SDK HTTP construction and bounded gh authentication in internal/hosting/transport.go; preserve apiReader and native operations in internal/hosting/operations.go.

## Phase 4: US2 — Bounded transport effects

Goal: refuse excess, wrong destinations and credential leakage under cancellation/concurrency.
Independent test: instrumented transports prove exact request count, bytes, closure and safe diagnostics.

- [x] T005 [US2] Write failing raw/normalized success/error body, cancellation, redirect, identity/compressed-response refusal, endpoint/config refusal and concurrent token-isolation tests in internal/hosting/transport_test.go; enforce 1,048,576 bytes plus one sentinel per body.
- [x] T006 [US2] Implement guarded fixed GET/HTTPS request allowance, body closure/caps and safe errors in internal/hosting/transport.go; preserve 30 seconds, 10 pages and 1000 records in internal/hosting/operations.go.

## Phase 5: US3 — Tier 1 and JSON compatibility

Goal: preserve real native Git/jj operations, shared CLI behavior and published schemas.
Independent test: both pinned backend lanes perform in-process fake HTTP PR/CI and native effects checks; executable fixtures retain dispatch/auth refusal coverage.

- [x] T007 [P] [US3] Write failing shared-operation CLI/schema fixtures in internal/cli/hosting_test.go and internal/contracts/hosting_test.go using injected HTTP rather than canned Results; retain unchanged schema assertions.
- [x] T008 [US3] Add InspectWithTransport in internal/hosting/operations.go and unexported CLI injection in internal/cli/hosting.go; adapt contract success fixtures without production endpoint overrides.
- [x] T009 [US3] Adapt scripts/smoke-hosting.py and native operation/schema/effect tests in internal/contracts/hosting_test.go for real Git/jj fake-HTTP PR/CI success/effect conformance plus real binary/private-extension local/auth-refusal checks; preserve minimum/current CI lanes in .github/workflows/ci.yml.

## Phase 6: Review and handoff

- [x] T010 [P] Update transport truth in docs/hosting.md, ARCHITECTURE.md and specs/022-bounded-go-gh/verification.md; document caller proxy trust, compressed-response refusal and limits without claiming live keyring/Enterprise acceptance.
- [x] T011 Independently review integrated internal/hosting transport and all fixture changes; record findings/fixes in specs/022-bounded-go-gh/verification.md and rerun affected tests.
- [ ] T012 Run make check, go test -race ./..., go mod verify, git diff --check and both native lanes; record exact commands/results and schema byte comparison in specs/022-bounded-go-gh/verification.md.
- [ ] T013 Reconcile tasks/evidence, docs/progress.md and docs/feature-status.json; make focused Conventional Commits and open the separate source PR with exact-head CI gaps recorded.

## Dependencies and parallel work

T001 gates code. T002–T004 establish SDK/auth; T005 precedes T006. US1 and US2 share transport.go and execute sequentially within one author worktree. T007 can run in an adapter worktree after the interface freezes, alongside transport test authoring. T008 follows T007 and the shared seam agreement. T009 follows working SDK integration. T010 can proceed separately once transport behavior stabilizes. T011 precedes final T012; T013 follows all source gates. Review findings may add focused regression tasks without falsely checking failed gates.

## Implementation strategy

Deliver US1 first behind the unchanged apiReader interface, then harden US2, then integrate US3 fixtures. The PR includes all three stories: authenticated operation alone is not acceptance without bounded errors and both native lanes. Tests fail before the corresponding implementation. No live API, release, user extension installation or deletion is required.
