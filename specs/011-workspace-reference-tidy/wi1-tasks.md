# WI1 Tasks: Evidence-only Tidy Plans

## Setup and foundation
- [x] T001 Freeze native/core/CLI shapes and obtain independent WI1 analyze in specs/011-workspace-reference-tidy/contracts/interface.md and wi1-analysis.md.
- [x] T002 Write failing secure optionalstore/immutable digest/strict JSON/limits/double-observation tests in internal/workspace/tidy_test.go and state_test.go.
- [x] T003 Implement bounded common models/PlanTidy coordinator and secure lazy tidy namespaces preserving old records in internal/workspace/tidy.go and state.go; text4096bytes, IDs128bytes, aggregate1024, encoded1MiB.

## US1 — Review cleanup candidates
Independent test: integrated/unique/retained candidates in disposable native repositories; no native changes.
- [x] T004 [P] [US1] Write failing Git ancestry/base/symbolic/detached-linked-workspace/gone-upstream/empty-helper/stale tests in internal/workspace/tidy_git_test.go.
- [x] T005 [P] [US1] Write failing standalone/colocated jj ancestry/conflict/remote-counterpart/workspace-target/op-stale/no-snapshot tests in internal/workspace/tidy_jj_test.go and internal/inspection/jj_tidy_test.go.
- [x] T006 [P] [US1] Implement fixed bounded Git reference/worktree ancestry observation and complete revalidation in internal/workspace/tidy_git.go; preserve SHA1/SHA256 identities, protect selected base and matching workspaceHEADs.
- [x] T007 [P] [US1] Implement native jj sandboxed pinned operation observation and exact ancestry/reference/workspace identities in internal/inspection/jj_tidy.go and internal/workspace/tidy_jj.go; no fallback or snapshot.
- [x] T008 [US1] Implement literal selectedrefs filtering, missing/duplicate selector refusal, delete/retain reasons and optional immutable storage in internal/workspace/tidy.go; incomplete evidence never authorizes apply.

## US3 — Bounded human/agent contract
Independent test: both output modes share evidence; actual success/error fixtures validate and preserve old contracts.
- [x] T009 [P] [US3] Write failing Cobra syntax/shared-output/bounds/error tests and real schema fixtures in internal/cli/tidy_test.go and internal/contracts/tidy_test.go.
- [x] T010 [US3] Implement plan CLI and safely quoted human rendering in internal/cli/tidy.go; optional state, explicit base, repeat literal --ref, no process/filesystem imports.
- [x] T011 [US3] Add independent schemas/v1/tidy.schema.json without changing existing schemas; validate real native success/refusal/error outputs in internal/contracts/tidy_test.go.
- [x] T012 [P] [US3] Implement exact mandatory WI1 case manifest/effect snapshots/helper sentinels and both-lane gates in scripts/smoke-tidy.py, scripts/test_tidy_smoke.py and .github/workflows/ci.yml.

## Review and handoff
- [x] T013 [P] Document scope/proof/bounds/stateeffects/retention in docs/tidy.md, docs/backends.md, docs/json-contracts.md and publication allowlist; add public schema/guide checks.
- [x] T014 Run independent source review and final consistency analysis, gofmt, make check, race tests and mandatory minimum/current native lanes; record exact evidence in specs/011-workspace-reference-tidy/wi1-verification.md.
- [x] T015 Update scoped progress/status/task evidence, make focused Conventional Commits and open/attach WI1 PR; leave WI2/3 planned in docs/progress.md and docs/feature-status.json.

## Dependencies and parallel work
T001 precedes source; T002→T003 establishes shared foundation. Git T004→T006 and jj T005→T007
run in separate worktrees after frozen types. T008 integrates. CLI T009→T010→T011 and
conformance T012 prepare independently, integrate after core. T013 parallel docs; all tasks required
before T014→T015. MVP is complete WI1 only, not deletion. 15 tasks: US1 five, US3 four,
setup/foundation three, handoff three. All tasks carry IDs/paths and story tasks story labels.
