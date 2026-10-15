# Native jj Inspection Contract

Command/exit streams remain [feature005](../../005-repository-inspection/contracts/inspect.md). Git/default failures preserve schema1. Recognized native jj ownership selects schema2 for success and failure; ambiguous/GitButler outcomes retain schema1. Callers dispatch schema by schema_version, not by guessed field shape.

Schema URL: `https://wstein.github.io/sourcesteward/schemas/v2/inspect.schema.json`.
Version1 URLs/files/output remain unchanged. No new version output schema or `--schema-version` flag is needed.

Successful human output names jj, quoted workspace/shared metadata paths and workspace name, full separately labeled change/commit/operation IDs, and “stored working-copy state; disk changes were not snapshotted”. JSON uses the same typed data and [data model](../data-model.md). No status/history/files/remotes are returned.

Operational jj failures: one schema2 JSON object plus LF on stdout and one concise Error line on stderr with exit1; human mode only the Error line. Unsupported versions/missing jj advise installing a supported backend; no Git fallback. Usage exit2 and writer/cancellation limits retain005 behavior. Help never invokes a backend.

Every backend invocation is noninteractive and read-only with both snapshot suppression and operation pinning. Stale/multiple-head state never triggers automatic snapshot, operation merge, import/export or update. Observations are not transactional and do not certify disk freshness; visible identity changes fail explicitly.

Native jj requires usable Linux bubblewrap or macOS sandbox-exec enforcing read-only target state and network denial. Fixed trusted identity built-ins and isolated configuration (including template aliases), verified by sentinels, prevent helper invocation; Linux sandboxing does not claim process-execution denial. macOS additionally restricts executable launch. Unavailable/denied enforcement produces jj-sandbox-unavailable; never retry unsandboxed. Native execution has a 10-second whole-operation deadline and 64 KiB per-command output bound. A private temporary isolation/config/probe directory outside the inspected target may be created and cleaned up; no target writes are permitted.
