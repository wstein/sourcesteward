# Tasks: CLI JSON schemas

## Setup and design
- [x] T001 Create reviewed spec/plan/research/model/contracts/quickstart and checklist in specs/006-json-schemas.
- [x] T002 Obtain root design review and implementation release; integrate feature005 when available.

## US1: Validated results
- [x] T003 Write failing tests in internal/contracts/contracts_test.go for missing schemas, real CLI outputs, invalid fixtures and offline loading.
- [x] T004 Pin test-only validator in go.mod/go.sum; implement schemas/v1/version.schema.json and inspect.schema.json.
- [ ] T005 Verify attached/detached/unborn/bare/linked Git, managed backend refusal and operational errors with real command output; assert runtime dependency exclusion.

## US2: Published contracts
- [x] T006 Document schemas and compatibility in docs/json-contracts.md; extend scripts/package-pages.py with an exact two-schema allowlist and symlink/unexpected-file safety tests; coordinate preserved URLs with007.
- [ ] T007 Run make check, race tests and analysis; write docs/json-schema-verification.md with measured evidence and pending live publication.
- [ ] T008 Independent final source review, focused Conventional Commits and coordinator PR handoff.

Dependencies: T001→T002→T003→T004→T005; T006 followsT004 and can be independent of output fixtures. T007 afterT005/T006; T008 last. Current packager byte-equality tests are required before006 acceptance;007 preserves the same publication contract.
