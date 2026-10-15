# Plan: JSON schemas

## Technical context
Hand-authored JSON Schema2020-12 under schemas/v1; Go tests in internal/contracts exercise the real CLI entry point and a built executable. Pin github.com/santhosh-tekuri/jsonschema/v6 v6.0.3 as a test-only dependency. Its established validator includes standard metaschemas and draft2020-12; compiler loading is replaced with a deny-network loader. No custom validation engine and no runtime dependency.

## Constitution check
Humans/agents share unchanged CLI output. Tests use temporary Git repositories and no real user metadata. No mutation feature or backend adaptation. Dependencies justified and pinned. Reviewed spec/plan/tasks precede failing-first implementation. Independent review and actual source checks precede publication. All gates pass at design; recheck after implementation.

## Design
Authoritative paths schemas/v1/version.schema.json and inspect.schema.json; IDs https://wstein.github.io/sourcesteward/schemas/v1/{version,inspect}.schema.json. Internal $defs only; no external refs. Unknown fields remain allowed. Conditional constraints relate status/backend/repository, bare/worktree, HEAD state/ref/oid and object format/oid length. Diagnostic codes stay open strings; documented initial codes belong to implementation, not an exhaustive future prohibition.

## Dependencies and sequencing
Inspection005 supplies exact JSON results before final tests can run. The current static packager serves both schemas verbatim via an explicit allowlist and verifies byte equality; Hugo007 preserves this contract. This feature adds schema contracts/tests/docs; no backend copying. Root coordinates rebase onto005 before source acceptance.

## Verification
Run failing tests before schemas exist, then positive real-output validation and negative contract fixtures, offline compiler checks and runtime dependency exclusion. Run make check and race tests. Record source versus pending publication evidence. Independent reviewer checks final integrated SHA.
