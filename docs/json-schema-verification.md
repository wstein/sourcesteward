# JSON schema acceptance

Feature006 is implemented locally on codex/006-json-schemas, stacked on inspection005.
Independent final review and remote/Pages publication remain pending.

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
The following evidence-only update records these checks; final exact-revision review is pending.

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
