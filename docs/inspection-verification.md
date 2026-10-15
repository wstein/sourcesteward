# Bounded repository inspection acceptance

Feature: [005](../specs/005-repository-inspection/spec.md). Implemented and independently verified; coordinator publication remains pending.

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
acceptance were rerun after this correction; final reviewer clearance is recorded below.

## Independent clearance
Independent review cleared exact source revision `1d8d7c513fa78a08cbb097701e4fe90e7ac23411`
with no remaining blocking findings. The reviewer independently ran make check (nine Python
cases and 51 authored documents), race tests, a separate binary build and ten process cases.
The replacement-object regression was independently observed failing before its fix and
passing afterward. The optional invalid-UTF8 cwd fixture remains unavailable on this macOS
filesystem; this limitation is not described as verified platform coverage.

This subsequent documentation-only commit records that clearance; it changes no implementation.
No branch was pushed or PR created by the implementation agent.

## Integration with merged site themes
The inspection feature incorporates main revision `d74341a8bb265c9381630fa4ebdac3cd40ad3466`
without rewriting published commits. Selective documentation conflict resolution retains both
site-theme and inspection knowledge-map links, feature records and milestone notes. The
existing future-inspection next action is replaced by this feature's reviewed current action.
All theme/CI/packaging source files match merged main exactly; inspection source files remain
unchanged from their independently cleared revision.

The integrated tree passes make check (12 Python cases, 12 Node theme cases, 60 authored
documents plus Go/module/architecture/whitespace checks), race tests, build and ten process
cases. Basic site asset/script allowlisting and packaging checks pass in that suite. No browser
reinspection is claimed: the merged site implementation is byte-identical to reviewed main.
Independent integration review is pending for this subsequent merge commit.
