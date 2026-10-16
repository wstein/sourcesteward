# Contract: `stw pins`

## Command

```text
stw pins [PATH] [--json] [--strict]
```

`PATH` defaults to the current directory and may be inside the superproject worktree. The command accepts no other flags. It never prompts, opens an editor or pager, or contacts a network.

| Flag | Meaning |
| --- | --- |
| `--json` | Emit one `PinsResult` object, including refusals and operational errors |
| `--strict` | Exit with status 3 when any finding has `error` severity; the result is still printed in full |

## Exit status

| Status | Meaning |
| --- | --- |
| 0 | Complete result; findings do not change this without `--strict` |
| 1 | Refusal, operational failure, cancellation or an incomplete result, with or without `--strict` |
| 2 | Usage error |
| 3 | `--strict`, a complete result and at least one `error` finding; one line on standard error names the count, standard output carries the full result |

Precedence: 2, then 1, then 3, then 0. An incomplete result exits 1 even with error findings, because completeness is not optional for an audit; `pins-limit` and `pins-state-changed` therefore never reach status 3.

## Backend capability

| Backend | `stw pins` |
| --- | --- |
| Git (worktree, linked worktree) | Supported |
| jj (standalone, colocated, secondary) | Refused with `unsupported-backend`: jj has no Git submodule model; use a Git-owned checkout |
| GitButler-managed | Refused with `unsupported-backend` |
| Conflicting ownership markers | Refused with `ambiguous-backend` |
| Bare Git repository | Refused with `bare-repository` |
| Not a repository | Refused with `not-repository` |

## Human output

A header with the superproject identity, then one block per submodule, then findings, then a footer:

```text
SourceSteward pins: /path/to/superproject (git, HEAD 4c07ba0…)
effects: read-only, no network, no fetch

bundledock   pin 5d3f37c…  checkout 5d3f37c… (same)  clean
  publication: reachable from origin/main
workharbor   pin c27fc9e…  checkout 54a002f… (diverged, ahead 0, behind 12)  clean
  publication: UNREACHABLE (no remote-tracking ref contains it)

findings: 1 error, 1 warning
  error    workharbor  pin-unpublished: local evidence shows no remote contains the pin
  warning  workharbor  checkout-differs-from-pin
note: remote-tracking refs may be stale; no fetch was made
```

Names, paths, URLs and refs have control characters escaped. Output is bounded; a truncated list says so.

## JSON

One `PinsResult` object per invocation, validated by `schemas/v1/pins.schema.json`. A refusal:

```json
{"schema_version":1,"command":"pins","status":"refused","backend":"jj","input_path":".","identity":null,
 "boundary":{"effects":{"network":false,"fetch":false,"writes":false,"hooks":false},
  "limits":{"max_submodules":256,"per_submodule_seconds":5,"total_seconds":30,"max_output_bytes":1048576,"max_remote_refs":16,"max_count":10000,"dirty_scan_seconds":3,"dirty_scan_entries":20000},
  "complete":false,
  "staleness":"Remote-tracking refs are local records; no fetch was made."},
 "submodules":[],"findings":[],
 "summary":{"submodules":0,"errors":0,"warnings":0,"infos":0,"publication":{"reachable":0,"unreachable":0,"unknown":0}},
 "error":{"code":"unsupported-backend","message":"jj has no Git submodule model; run this in a Git-owned checkout"}}
```

## Effects

- Reads: superproject object database, index file, `.gitmodules` blobs and worktree file (never through a symlink), each validated submodule worktree and Git directory, `FETCH_HEAD` metadata.
- Writes: none. No lock file remains. Index bytes and modification time are unchanged.
- Network, fetch, clone, `submodule init` and `submodule update`: none.
- Processes: only the fixed, isolated Git invocations of the process allowlist in the research record, with no hook, clean or process filter, helper, alias, fsmonitor, textconv or external diff. A jj or ambiguous refusal starts no process. Other refusals use only the existing detection's Git processes and never start jj or a submodule command.

## Compatibility

New command and new schema. `inspect`, `inventory`, `tidy`, `workspace`, `hosting` and `version` contracts are unchanged. Finding codes are append-only within `schema_version: 1`.
