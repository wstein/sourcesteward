# Bounded repository inspection acceptance

Feature: [005](../specs/005-repository-inspection/spec.md). Author implementation evidence, pending independent exact-revision source review and coordinator publication.

## Failing-first observations
Initial Go identity/managed/corrupt-reference tests failed because Inspect was undefined. The
new architecture test rejected the proposed CLI → inspection import. CLI contract tests then
failed with unknown --json for inspect. Implementation followed those observed failures.
Expanded fixture coverage subsequently checks the complete bounded contract.

## Acceptance exercised
- Ordinary, nested, linked, bare, SHA-256, attached/detached/unborn repositories and newline/Unicode paths.
- jj markers, nested Git ownership boundaries, private/common GitButler metadata, packed refs, prefix collisions and ambiguous markers.
- Corrupt branch refs, missing/noncommit HEAD objects, malformed HEAD and unsupported symbolic targets.
- Byte snapshots across identity shapes and helper/trace sentinels under hostile inherited GIT_* variables.
- Missing Git, cancellation before/during subprocess work, simulated ownership denial, output capture cap and concurrent HEAD change.
- Human/JSON success and errors, usage classification, stderr cardinality and unchanged version contract.
- Ten real-process smoke cases against disposable repositories.

The implementation uses Git 2.46+ show-ref --exists to distinguish absent branch from lookup
errors. It does not bypass safe.directory or execute managed-backend CLIs. Writer failures
remain runtime1 and may leave incomplete output.

## Limits
Detection is heuristic and may miss future/unregistered manager conventions. Only HEAD is
sampled twice; ABA and metadata routing/evidence races are not detected transactionally.
No status/index/file enumeration, remote reads, native adapters, cleanup or mutation exists.
Author tests ran on macOS with Apple Git2.54; ownership refusal is tested through a controlled
runner seam, not by changing ownership of a real user repository. Permissions/platform-specific
behavior remains subject to CI and independent review.

## Commands observed
`make check` passed (Go tests/vet/module verification, nine Python tests, docs/architecture/whitespace).
`go test -race ./...`, `make build` and `python3 -B scripts/smoke-inspection.py` passed.
Subsequent review fixes added cancellation at each query and invalid-UTF8 identity checks.
The invalid-UTF8 filesystem fixture is unavailable on this macOS filesystem and explicitly
skipped; direct invalid-path/ref runner tests cover rejection, and the process fixture runs
where the filesystem permits such names. Individual regression checks were added alongside
these fixes; foundational operation/CLI/architecture tests have observed failing-first evidence.

## Independent source review correction
The reviewer reproduced replacement refs hiding a raw blob as a commit. A new failing
regression reproduced the false success before the runner was fixed with --no-replace-objects.
Identity validation now checks raw HEAD object identity. Full checks and race/process
acceptance were rerun after this correction; final reviewer clearance remains pending.
