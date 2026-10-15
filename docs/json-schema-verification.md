# JSON schema acceptance

Feature006 is implemented locally on codex/006-json-schemas, stacked on inspection005.
Independent source review cleared revision98f65dff5dc8be465a2d89e728c0e56fcacab3ce with no
actionable findings. [PR #6](https://github.com/wstein/sourcesteward/pull/6) is open; schema Pages publication remains pending.

## Measured evidence
The initial schema tests failed because the schema files were absent. After implementation,
version/inspection contracts compile against embedded JSON Schema2020-12 metaschemas with
external loading disabled. Independently authored invalid status/backend/HEAD/oid and
required-field fixtures are rejected; additive object members and unknown diagnostic codes
are accepted. Static packaging first failed to include schema output, then passed byte
identity and unexpected-file/missing-schema/symlink-file/parent rejection tests after the fix.

Final integrated source candidate f0ab53648e0c14588a37c9676f593ca62bd3286d is stacked on
inspection dependency 1d8d7c513fa78a08cbb097701e4fe90e7ac23411. `make check` passed:
all Go tests and vet, module verification, ten Python tests, 61 authored-document checks,
architecture checks and whitespace. `go test -race ./...` passed. Contract fixtures execute
a freshly built real binary in temporary repositories; no backend code changed in006.
The evidence-only update records these checks. Independent review at98f65df additionally
ran all contract tests (14 JSON output fixtures, one usage case and18 negative invariants),
ten Python tests and whitespace checks; all passed.

Real CLI fixtures cover version; attached/detached/unborn HEAD; ordinary/bare/linked Git;
SHA1/SHA256; Unicode, space and newline paths; invalid UTF8 input; missing/non-repository
paths; jj/GitButler/ambiguous refusals; and usage with no JSON. JSON result exit status and
final newline are checked. Runtime `go list -deps ./cmd/stw` excludes the validator.

## Publication and scope
The exact two-file schema allowlist packages checked-in bytes at schemas/v1/version.schema.json
and inspect.schema.json. These are the paths of the documented versioned Pages URLs.
Actual live URL availability is not asserted before approved merge and successful Pages
publication. Feature007 must preserve URLs, bytes and source safety checks while replacing
the static packager with Hugo. No runtime JSON or backend behavior changes belong to006.

## Analysis
All six functional requirements have tasks: FR001 T003–T005; FR002 T003–T005; FR003 T004–T005;
FR004 T003–T005/T007; FR005 T006–T008; FR006 T004–T005. Eight tasks cover both user stories;
no constitution conflicts, uncovered requirements or unresolved design clarifications remain.
Independent design review corrected ambiguous error-diagnostics wording before implementation.

## Integration with merged site themes

Merge candidate c52f0890ce43abebf33b41fc391969c117e3cb18 integrates independently reviewed
inspection/theme dependency42c5638b99ea9a2b87e5ef0df7c7afb230ba6e44 without rewriting published
history. The sole textual conflict retained both schema and site-theme knowledge links.
Full `make check` initially exposed a theme packaging test fixture missing required schemas;
adding the authoritative schema source tree to that fixture preserves both safety contracts.
The rerun passed all Go tests/vet/module checks,13 Python cases,12 Node theme cases,
70 authored-document checks, architecture and whitespace. Runtime and schema files are
unchanged; prior race results still apply. The following documentation commit records
these results and corrects inherited merged PR4/5 progress. Final integration review is pending.

## Publication history correction (2026-10-15)
The statements above that integration happened "without rewriting published commits",
"without rebase" or "without force push" describe the topic branches before publication. To
make the stacked PRs rebase-mergeable, the branches were afterwards rebased onto main as
linear commits and force-pushed with a lease on the reviewed heads (PRs #6 and #7); the
integration merge commits were replaced by ordinary commits carrying the same changes. Each
rebased branch tree was verified byte-identical to its reviewed head before pushing, so the
review evidence still applies to the merged content. The commit IDs cited above (`1d8d7c5`,
`42c5638`, `98f65df`, `c52f089`) are pre-rebase branch commits and are not in main's history
(checked 2026-10-15).
