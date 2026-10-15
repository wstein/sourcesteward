# Feature 006: Published CLI JSON contracts

## User scenarios and testing
### US1 (P1): Validate automation results
A caller validates real `stw version --json` and `stw inspect --json` results using public, versioned schemas. Successful inspection and operational failures have one documented contract; usage and writer failures do not promise a JSON object.
Acceptance: actual command output for version, ordinary Git attached/detached/unborn/bare/linked worktrees, unsupported managed backends and operational failure validates. Invalid discriminants, HEAD combinations, identifier lengths and missing mandatory members fail.
### US2 (P2): Consume stable published contracts
A caller downloads a documented schema URL and can validate offline using repository files. Additive output fields remain compatible; incompatible output changes require a new schema version and URL.
Acceptance: each declared URL maps to the same checked-in JSON bytes in the site artifact; no remote schema fetching occurs during validation.

## Requirements
- FR001: Define version1 contracts for implemented version and inspection JSON only; preserve runtime output bytes.
- FR002: Express all required fields, discriminants, nullable identity fields, status/repository and HEAD/object-format invariants, including operational-error results.
- FR003: Keep unknown object fields valid for additive evolution; consumers ignore unknown fields and handle unknown diagnostic codes.
- FR004: Validate real CLI output and independently authored invalid fixtures in CI; reject invalid schema syntax and unresolved or external references offline.
- FR005: Publish stable versioned URLs, document exit codes and compatibility policy, and exclude invented future-command contracts.
- FR006: Test dependencies must not enter the runtime executable dependency graph.

## Edge cases
Unborn HEAD has a branch ref and null oid; detached HEAD has null ref and an oid. SHA256 is64 hexadecimal characters and SHA1 is40. Bare repositories have null worktree. Error/unsupported results have null repository and a nonempty diagnostics array. Usage errors may have no JSON; output failure may leave partial bytes. Paths are opaque UTF8 strings, including control characters, not URI formats. Additive unknown fields/codes remain valid.

## Key entities
Version result, inspection result, repository identity, HEAD, evidence and diagnostic match feature005's result model.

## Success criteria
All real supported result variants validate; each authored negative invariant fails. Validation works without network access. Every schema URL is represented byte-for-byte in the generated site artifact. No supported CLI output changes.

## Assumptions and scope
Feature005 is the inspection contract dependency. The existing packager publishes the authoritative schemas verbatim; feature007 preserves those URLs. Remote publication follows PR integration, not this implementation handoff. No runtime validation, command additions or generic error envelope for version usage failures.
