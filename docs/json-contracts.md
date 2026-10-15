# CLI JSON contracts

Use `stw version --json` or `stw inspect [PATH] --json` for structured results.
`schema_version` identifies the contract independently of the program version.

| Command | Authoritative schema | Stable publication URL |
| --- | --- | --- |
| `stw version --json` | [version.schema.json](../schemas/v1/version.schema.json) | [Version schema 1](https://wstein.github.io/sourcesteward/schemas/v1/version.schema.json) |
| `stw inspect --json` | [inspect.schema.json](../schemas/v1/inspect.schema.json) | [Inspection schema 1](https://wstein.github.io/sourcesteward/schemas/v1/inspect.schema.json) |

These files use JSON Schema 2020-12. They contain only internal references, so downloaded
files validate offline with a compatible validator. Site packaging serves the checked-in
bytes. Publication URLs become available after the feature merges and exact-revision
Pages deployment succeeds; source checks alone do not prove live publication.

## Output and errors

Version success exits 0 and emits one JSON object plus a newline, with `schema_version`,
`name` and `version`. Version usage errors exit 2 and do not emit JSON.

Inspection success exits 0; its `status` is `ok`, `backend` is `git`, `repository` contains
identity, and `diagnostics` is an empty array. Operational failures exit 1 and emit an
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

## Compatibility

Consumers must ignore additional object members at every nesting level and handle unknown
diagnostic codes. Existing required fields, types, discriminants and documented relationships
remain stable within schema 1. The schemas intentionally allow extra properties; they do not
reject compatible producer extensions. New status/backend/HEAD/object-format alternatives or
incompatible meaning/type changes require a new `schema_version` and a new `schemas/vN/` URL.
Published old versions remain available. Additive optional fields and new diagnostic codes
may extend version 1. If exact schema bytes matter, pin a Git revision or checksum as well as
the versioned URL.

## Verification and dependencies

`go test ./internal/contracts -v` compiles schemas against the validator's embedded standard
metaschemas, prohibits external references/loading, rejects independently authored invalid
results and validates real CLI output using isolated temporary Git repositories. `make check`
and CI include these tests. Fixtures cover version, attached/detached/unborn HEAD, SHA1/SHA256,
bare and linked worktrees, managed workspace refusals and operational failures.

The established [santhosh-tekuri/jsonschema](https://github.com/santhosh-tekuri/jsonschema)
validator is pinned at v6.0.3 in `go.mod` for tests only, with its dependencies pinned in
`go.mod`/`go.sum`. It supports the chosen dialect and embeds standard metaschemas. Go's
standard library has no JSON Schema engine; maintaining a custom engine would obscure the
contract under test. The runtime CLI does not import the validator; tests check its dependency
graph. Downloading pinned Go modules during initial setup is separate from offline schema
validation after dependencies are cached.
