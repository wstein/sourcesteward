# CLI JSON contracts

Use `stw version --json`, `stw inspect [PATH] --json` or `stw inventory [PATH] --json` for structured results.
`schema_version` identifies the contract independently of the program version.

| Command | Authoritative schema | Stable publication URL |
| --- | --- | --- |
| `stw tidy plan/apply --json` (owned retirement) | [tidy.schema.json](../schemas/v2/tidy.schema.json) | [Retirement schema 2](https://wstein.github.io/sourcesteward/schemas/v2/tidy.schema.json) |
| `stw tidy plan --json` | [tidy.schema.json](../schemas/v1/tidy.schema.json) | [Tidy schema 1](https://wstein.github.io/sourcesteward/schemas/v1/tidy.schema.json) |
| `stw version --json` | [version.schema.json](../schemas/v1/version.schema.json) | [Version schema 1](https://wstein.github.io/sourcesteward/schemas/v1/version.schema.json) |
| `stw inspect --json` (Git and unselected/ambiguous ownership) | [inspect.schema.json](../schemas/v1/inspect.schema.json) | [Inspection schema 1](https://wstein.github.io/sourcesteward/schemas/v1/inspect.schema.json) |
| `stw inspect --json` (selected native jj) | [inspect.schema.json](../schemas/v2/inspect.schema.json) | [Native jj inspection schema 2](https://wstein.github.io/sourcesteward/schemas/v2/inspect.schema.json) |

These files use JSON Schema 2020-12. They contain only internal references, so downloaded
files validate offline with a compatible validator. Site packaging serves the checked-in
bytes. Publication URLs become available after the feature merges and exact-revision
Pages deployment succeeds; source checks alone do not prove live publication.

## Output and errors

Version success exits 0 and emits one JSON object plus a newline, with `schema_version`,
`name` and `version`. Version usage errors exit 2 and do not emit JSON.

Git inspection success exits 0; its `status` is `ok`, `backend` is `git`, `repository` contains
identity, and `diagnostics` is an empty array. Git and version output retain schema 1.
Recognized native jj ownership selects schema 2 for both success and operational failures;
callers must dispatch by `schema_version`, rather than guessing from field presence. Operational failures exit 1 and emit an
inspection result with `status` `error` or `unsupported`, a null `repository`, and a nonempty
`diagnostics` array. They also write a concise `Error:` diagnostic to stderr. Unsupported
managed backends retain detection evidence without pretending to be ordinary Git results.
`input_path` can be empty if early failure prevents an absolute, valid UTF-8 path.

Inspection usage errors exit 2 with no JSON. Writer failures can leave partial output;
callers must check both the exit status and JSON validity. No universal JSON envelope is
promised for every process failure. See the [inspection CLI contract](../specs/005-repository-inspection/contracts/inspect.md)
for exact outcomes and effect limits.

HEAD is attached, detached or unborn. Attached HEAD has both a full branch ref and an oid;
detached HEAD has null ref and an oid; unborn HEAD has a full branch ref and null oid.
Object IDs are full lowercase hex, 40 characters for SHA1 or 64 for SHA256. Bare repositories
have null worktree paths. Paths are opaque JSON strings, not URLs; preserve escaped control
characters and do not interpolate path or diagnostic strings into shell commands.

## Native jj identity (schema 2)

Selected native jj results always have `backend: "jj"` and `repository: null`. Successful
results additionally contain `jj_repository` with `workspace_root`, `metadata_dir`,
`repository_dir`, `workspace_name`, `change_id`, `commit_id`, `operation_id` and `observation`.
Paths are absolute and retain exact valid UTF-8; workspace names are opaque strings.

Change IDs are full 32-character reverse hex (`k`–`z`); commit IDs are full 40-character
lowercase hex; operation IDs are full 128-character lowercase hex. These are distinct
backend identities, not interchangeable Git branches or IDs.

The observation has `working_copy: "stored-unsnapshotted"` and a nonempty `limitation`
explaining freshness and concurrent-state limits. Inspection does not snapshot edited or
untracked disk content, update stale workspaces, merge operations or import/export Git
state. Native execution requires usable read-only sandbox enforcement; unavailable
enforcement returns `jj-sandbox-unavailable` without an unsandboxed retry. The inspected
repository is read-only; a private temporary isolation directory outside it may be used.

Operational failures omit `jj_repository` and have nonempty diagnostics; success has empty
diagnostics. Schema 2 allows additional object members and diagnostic codes but enforces
these known relationships. Ambiguous ownership and unsupported GitButler remain schema 1.
See the [native jj contract](../specs/008-jj-inspection/contracts/inspect.md) for effects,
version and process limits.

## Compatibility

Consumers must ignore additional object members at every nesting level and handle unknown
diagnostic codes. Existing required fields, types, discriminants and documented relationships
remain stable within schema 1. The schemas intentionally allow extra properties; they do not
reject compatible producer extensions. New status/backend/HEAD/object-format alternatives or
incompatible meaning/type changes require a new `schema_version` and a new `schemas/vN/` URL.
Published old versions remain available. Additive optional fields and new diagnostic codes
may extend an existing version. If exact schema bytes matter, pin a Git revision or checksum as well as
the versioned URL.

## Verification and dependencies

`go test ./internal/contracts -v` compiles schemas against the validator's embedded standard
metaschemas, prohibits external references/loading, rejects independently authored invalid
results and validates real CLI output using isolated temporary Git repositories. `make check`
and CI include these tests. Fixtures cover version, attached/detached/unborn HEAD, SHA1/SHA256,
bare and linked worktrees, managed workspace refusals and operational failures. Native jj
fixtures validate standalone, colocated and secondary workspaces, nested paths, unsnapshotted
content and failure results against schema 2. With `STW_REQUIRE_JJ=1`, unavailable jj fails
instead of skipping these fixtures; mandatory conformance CI uses this setting. Hardcoded
checksums also protect published v1 schema bytes from accidental changes.

The established [santhosh-tekuri/jsonschema](https://github.com/santhosh-tekuri/jsonschema)
validator is pinned at v6.0.3 in `go.mod` for tests only, with its dependencies pinned in
`go.mod`/`go.sum`. It supports the chosen dialect and embeds standard metaschemas. Go's
standard library has no JSON Schema engine; maintaining a custom engine would obscure the
contract under test. The runtime CLI does not import the validator; tests check its dependency
graph. Downloading pinned Go modules during initial setup is separate from offline schema
validation after dependencies are cached.

## Inventory schema v1

[Inventory schema](../schemas/v1/inventory.schema.json) has stable publication URL
[Inventory v1](https://wstein.github.io/sourcesteward/schemas/v1/inventory.schema.json).
Its `command` discriminator is `inventory`; do not confuse its independent schema v1
with inspect schema v1/v2. The envelope embeds the selected existing inspection identity
and typed workspace/reference/base/file observations, completion limits and an optional
true/false/unknown query. Known false is success (exit 0); required unknown evidence is
partial (exit 1), while operational failure is error (exit 1). Invalid combinations are
rejected by offline fixtures. See [inventory](inventory.md) and its
[normative contract](../specs/009-workspace-inventory/contracts/inventory.md).
Spec 027 keeps this schema byte-identical (the `boundary.limits` constants too) and changes Git
behavior within it: repositories that returned `error` for size now return `ok` or `partial` with
verbatim truncation notes in `boundary.limitations`; a submodule or separate-Git-directory
checkout is described by its checkout path (the old value was the Git directory); `tracked` of a
gitlink-bearing workspace can be `true`; `command_bytes` names the largest unit held at once.
Consumers must read `status` and `boundary.complete`, not infer "too large" from `error`. The
compatibility table is in the
[Spec 027 contract](../specs/027-inventory-limits/contracts/inventory-limits.md).
This source change stages the schema for Pages; it does not prove live deployment.

## Repos schema v1

[Repos schema](../schemas/v1/repos.schema.json) is independently versioned for `stw repos`; its
`command` discriminator is `repos`. One result holds an entry per explicit path and discovered
submodule, each with status, backend, an error object, discovery counts, derived facts and the
unchanged embedded inventory (also valid against the inventory schema), plus a summary, the
boundary (effects, limits, limitations) and diagnostics. Entry statuses are `ok`, `partial`,
`error` and `skipped`; the overall status follows the entries (exit 0 only when every entry is
`ok` or `skipped`, exit 2 for usage errors with no JSON). Inherited inventory limits apply to each
member and are listed in the contract. Offline tests validate real command output and the
fixtures. See [multi-repository inventory](repos.md) and its
[normative contract](../specs/023-multi-repo-inventory/contracts/repos.md).

## Workspace schema v1

[Workspace schema](../schemas/v1/workspace.schema.json) is independently versioned and staged
for publication at [Workspace v1](https://wstein.github.io/sourcesteward/schemas/v1/workspace.schema.json).
Its command discriminator is `workspace-plan`, `workspace-apply`, `workspace-handoff` or
`workspace-retire-plan`. Results contain typed nullable plan/task records, action outcomes,
diagnostics and limitations. Refusal/error output retains the same envelope; completed
actions survive partial failure. Git native commit identity remains distinct from jj
commit/change/operation identities. Embedded inventory uses its established independent contract.
Offline tests validate real operational output and independently invalid fixtures without
fetching external schema references. See [task workspaces](workspaces.md) for authorization,
effects, recovery and bounded execution. Publication evidence remains separate from source checks.

## Tidy results (independent schema 1)

`stw tidy plan --json` emits a tidy envelope with `command`, `status`, `backend`, nullable
`plan`, `actions`, `diagnostics` and `limitations`. The plan binds source, selected references,
base/native identities, digests, candidate decisions/proofs, declared effects and recovery limits.
Stateless plans have empty plan IDs; stored plans have generated immutable IDs. Candidates use
`branch` or `bookmark`, `delete` or `retain`, and `native-ancestry` or `none`; ambiguous retained
jj targets may have empty commit/change IDs only with `reason: "ambiguous-target"` and no proof.
Deletion candidates cannot contain workspace blockers or missing native identity/proof.

Complete observations, including all-retained plans, exit 0. Operational refusal/error/partial
results exit 1 and validate against the same schema. Usage/writer failures may not emit complete
JSON. `tidy-apply` now uses this same unchanged contract. Apply requires a stored plan and explicit
authorization; action details expose native recovery pins and result limitations identify the durable
journal location. Refused/partial outcomes must not be treated as completed deletion. Future incompatible retirement result shapes use a separate schema
version. Existing inspection, inventory, version and workspace schema bytes remain unchanged.

## Owned retirement results (schema 2)

Retirement selection emits `schema_version: 2`, `plan: null` and the separate `retirement` record.
Complete successful results include the record; early operational failures may omit it. The record
binds completed creation plan/task/journal ownership, native source and target identities, physical
path identities, stored/disk manifests, distinct registration/files selections and action blockers.
The envelope retains actions, diagnostics and limitations so partial effects and durable journal
locations remain visible. Authorization refusal for a recognized stored retirement plan also uses
schema 2; an unrecognized plan ID cannot establish its result kind and may use schema 1.

Publish/validate against the independent v2 URL; do not reinterpret a reference tidy v1 plan as
retirement authority. Both versions remain available and the v1 schema's exact bytes are checked by
a regression checksum. Additional object members remain compatible; new incompatible semantics or
field types require another version. See [tidy](tidy.md) for native coupled/separate effects.

## Pin audit (v1, planned)

`stw pins --json` (merged but hidden until the conformance work item of
[Spec 021](../specs/021-submodule-pin-audit/spec.md); feature status stays planned) emits one `PinsResult`
with `schema_version: 1` and `command: "pins"`, validated by
[pins.schema.json](../schemas/v1/pins.schema.json), which is present. Its `status` is `ok`, `incomplete`, `refused` or `error`; refusals and
operational failures keep the same envelope. Finding codes and refusal codes are fixed by the
specification and append-only within schema 1; it is an independent contract and existing schemas keep
their bytes. Exit status 3 (`--strict` with error findings in a complete result) is documented in
[pins](pins.md), which also holds the finding and refusal tables.

## Hosting observations (v1)

[hosting.schema.json](../schemas/v1/hosting.schema.json) is a separate contract shared by `stw hosting` and `gh jj`. It retains native identity, candidates, explicit hosting selection, PR head/base identities, individual CI checks/statuses, diagnostics, sample time and bounded completeness/network effects. Errors may occur before any remote read; missing evidence is never empty successful CI. Existing version/inspection/inventory/workspace/tidy schemas retain their bytes and meanings. See [hosting](hosting.md).

## Release source schemas (unpublished)

Spec 026 remains planned/unreleased. The following checked-in schema v1 files
support current source implementations; they are deliberately excluded from
Pages packaging until T041. Their `$id` values do not prove live publication.
No release schema is promoted by this documentation change.

| Document/result | Source schema |
| --- | --- |
| Project contract | [release-config](../schemas/v1/release-config.schema.json) |
| Release record | [release-record](../schemas/v1/release-record.schema.json) |
| Build manifest | [release-build-manifest](../schemas/v1/release-build-manifest.schema.json) |
| OCI build result | [release-oci-result](../schemas/v1/release-oci-result.schema.json) |
| `release check --json` | [release-check](../schemas/v1/release-check.schema.json) |
| `release plan --json` | [release-plan](../schemas/v1/release-plan.schema.json) |
| `release verify --json` | [release-verify](../schemas/v1/release-verify.schema.json) |
| `release record --json` | [release-record-result](../schemas/v1/release-record-result.schema.json) |
| `release build --json` | [release-build](../schemas/v1/release-build.schema.json) |
| `release dispatch --json` | [release-dispatch](../schemas/v1/release-dispatch.schema.json) |

Result schemas admit additive members; the four contract/data formats reject
unknown properties. The manifest schema references the record schema; offline
validation must resolve that reference to the checked-in file, not fetch its
publication URL. The record object and the `record --json` result envelope are
different documents. Preserve the generated canonical record bytes.

Inspect `status`, `verdict`, gates and boundary qualifications as well as exit
status. Offline `plan`/`verify` cannot clear online gates; `--online` adds bounded
GitHub evidence, and verification additionally checks attestations. Exit 3 also
covers release findings, failed gates, record disagreements and local-verification hash mismatches. See
the [release guide](release.md) for the per-command matrix and remaining gates.
Dispatch source is implemented; its schema is also unpublished. Result-schema
`status` is separate from a gate `verdict` or from workflow completion:

| Results | Status interpretation |
| --- | --- |
| `check` | `ok` has no error findings; `findings` reports validation errors |
| `plan`, `verify`, `dispatch` | `ok` can still carry `fail` or `incomplete` verdicts; inspect gates and qualifications |
| `record` | `ok` produced/checked a record; `failed` reports input or comparison disagreement |
| All above | `refused`/`error` are not passing evidence; inspect the typed error and boundary |

For dispatch, `applied` reports acceptance of the one fixed POST, not completion
of its CI run. The required `request`, `plan_id`, `http_status`, `workflow_url`
and `approval` fields describe plan/apply effects; no-apply performs no POST.
`network` lists requests actually attempted, so an uncertain POST failure must
not be retried on the assumption that nothing started. The
[dispatch contract](../specs/026-release-process/contracts/dispatch.md) defines
its command-specific exit meanings and the separately required build approval.
