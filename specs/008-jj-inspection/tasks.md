# Tasks: Native jj Identity Inspection

**Input**: [plan.md](plan.md), [spec.md](spec.md), [data model](data-model.md), [CLI contract](contracts/inspect.md).
**Tests**: Required by policy; record failing behavior tests before implementation. Mandatory read-only sandbox design is documented in research.md; pre-implementation analysis gates source work.

## Phase 1: Setup
- [ ] T001 Verify mandatory Linux bubblewrap/macOS sandbox-exec enforcement and native read-side persistence with pinned-source evidence in specs/008-jj-inspection/research.md; record exact backend versions in scripts/backend-releases.json.
- [ ] T002 Analyze FR/task/contract consistency and review dependency policy in specs/008-jj-inspection/analysis.md before source implementation.

## Phase 2: Foundation
- [ ] T003 Write failing runner/configuration/cancellation/output-bound tests in internal/inspection/jj_test.go, including read-side index/type repair and zero-write enforcement failure.
- [ ] T004 Implement fixed native jj execution and required Linux/macOS read-only sandbox enforcement and bounded opaque config metadata isolation (no unavailable/denied fallback) in internal/inspection/jj.go, preserving CLI architecture boundaries, 64 KiB per-command output, 10-second whole-operation deadline, private outside-target temporary config cleanup and no raw diagnostic leakage.

## Phase 3: US1 — Native backend identity (P1)
**Goal**: Select and inspect native jj without changing ordinary Git semantics.
**Independent test**: Real standalone/colocated/secondary/nested fixtures report correct native identities; ordinary Git remains byte-identical and ambiguous ownership refuses.
- [ ] T005 [US1] Write failing ownership/layout/secondary pointer/ambiguous/missing-tool tests in internal/inspection/inspection_test.go and internal/inspection/jj_test.go.
- [ ] T006 [US1] Extend ownership discovery and native identity dispatch in internal/inspection/detection.go and internal/inspection/inspection.go without Git fallback or initialization.
- [ ] T007 [US1] Add optional JJRepository result model in internal/inspection/result.go: nonempty UTF-8 workspace_root/metadata_dir/repository_dir/workspace_name, change_id32reverse-hex k-z, commit_id40hex and operation_id128hex; preserve existing v1 Git serialization.
- [ ] T008 [US1] Write failing renderer/exit/help-locality tests then implement quoted human native identity rendering in internal/cli/inspect_test.go and internal/cli/inspect.go.

## Phase 4: US2 — Effects and observation qualifications (P1)
**Goal**: Stored identity observations never snapshot, merge operations or repair original metadata.
**Independent test**: Before/after bytes and helper sentinels remain unchanged for edits, stale workspace, divergent heads, corruption and errors.
- [ ] T009 [US2] Write failing real-fixture effects/config/env tests in internal/inspection/jj_test.go, including edited/untracked files, stale workspace, divergent operation heads, missing/corrupt index and type files; prove fixed built-ins/config exclusion never invoke helpers rather than claiming Linux sandbox exec denial.
- [ ] T010 [US2] Pin operation observations and selected workspace identity in internal/inspection/jj.go; detect changed observations, reject malformed output and include working_copy='stored-unsnapshotted' qualification with explicit freshness/concurrency limitation.
- [ ] T011 [US2] Add stable jj-unavailable/jj-version-unsupported/jj-sandbox-unavailable/jj-failed/jj-timeout/jj-output-limit diagnostics and failure tests in internal/inspection/inspection.go and internal/inspection/jj_test.go; retain ambiguity/GitButler schema1 refusal.

## Phase 5: US3 — Compatible schemas and conformance (P1)
**Goal**: Publish native jj schema2 and prove required minimum/current backend behavior without changing v1.
**Independent test**: Real outputs validate offline; invalid combinations fail; both pinned conformance lanes fail on missing tools or absent scenarios.
- [ ] T012 [P] [US3] Write failing real-output/negative schema tests in internal/contracts/contracts_test.go for schema2 backend jj, repository=null, complete jj_repository only on success, distinct ID alphabets/lengths and qualification; assert both v1 files remain byte-identical.
- [ ] T013 [US3] Implement schemas/v2/inspect.schema.json and offline compilation in internal/contracts/contracts_test.go without runtime dependency changes.
- [ ] T014 [US3] Write failing publication/allowlist/symlink/extra-file tests then update scripts/package-pages.py, scripts/test_docs.py and scripts/test_hugo.py and scripts/check-site.py to publish v1/v2 verbatim at stable project URLs.
- [ ] T015 [P] [US3] Implement checked pinned Git2.46.0/2.56.0 and jj0.45.1/0.46.0 installers with release hash verification in scripts/install-backends.py and scripts/backend-releases.json; test checksum/version/missing-tool refusal.
- [ ] T016 [US3] Implement mandatory real-fixture mode STW_REQUIRE_JJ=1 and actual CLI smoke coverage in internal/inspection/jj_test.go and scripts/smoke-jj-inspection.py; prove unavailable backend and zero executed mandatory cases fail.
- [ ] T017 [US3] Wire minimum/current paired backend conformance jobs in .github/workflows/ci.yml with exact version assertions, full Go/schema tests and race checks; preserve Pages dependency on all same-revision gates.

## Phase 6: Verification and Handoff
- [ ] T018 Update implemented scope/limits/schema guidance in README.md, docs/backends.md, docs/json-contracts.md, docs/agent-guide.md, ARCHITECTURE.md and llms.txt; verify generated command reference through existing Hugo checks.
- [ ] T019 Run gofmt, make check, race tests, build, real CLI/conformance and packaging checks; record actual versions/results/skips and FR/SC acceptance evidence in specs/008-jj-inspection/verification.md and docs/feature-status.json/docs/progress.md.
- [ ] T020 Run post-implementation artifact analysis and independent exact-revision source/effects review; fix concrete findings and record clearance in specs/008-jj-inspection/analysis.md and verification.md before atomic Conventional Commits/PR handoff.

## Dependencies and Execution Order
T001→T002→T003→T004 blocks native implementation. US1 T005→T006/T007→T008 follows foundation. US2 T009 begins before effect implementation T010/T011 and may share US1 fixture preparation. US3 tests T012 precede T013; publication tests precede T014. Installer T015 can run independently after the design gate; required execution T016 and CI T017 depend on real native fixtures. Final T018–T020 follows all stories. Each story has independent acceptance checks; none authorizes a partial Tier 1 release without the other required safety/contracts evidence.

## Parallel Opportunities
After analysis, separate ownership allows native adapter/fixtures (T005–T011), schema lane (T012–T014) and backend installer/CI lane (T015–T017). Agree result fields before schema tests and fixture entry points before CI. Shared files are coordinator-integrated, never edited concurrently.

## Implementation Strategy
Deliver reviewed policy separately; commit specification first after analysis. Implement failing tests and bounded native identity, then effect enforcement, schemas/publication and conformance in focused Conventional Commits. MVP is US1 plus all zero-write guards; it remains local until US2/US3 and independent review pass. Open PRs, preserving worktrees and any user changes; do not merge main automatically.
