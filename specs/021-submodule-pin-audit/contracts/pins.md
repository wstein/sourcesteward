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
| 0 | Result produced and complete; findings do not change this without `--strict` |
| 1 | Operational failure, refusal (jj, GitButler, bare, ambiguous), incomplete result, or cancellation |
| 2 | Usage error |
| 3 | `--strict` and at least one `error` finding in a complete result |

An incomplete result exits 1 even without `--strict`, because completeness is not optional for an audit.

## Backend capability

| Backend | `stw pins` |
| --- | --- |
| Git (worktree, linked worktree) | Supported |
| jj (standalone, colocated, secondary) | Refused with `backend_unsupported`: jj has no Git submodule model; use a Git-owned checkout |
| GitButler-managed | Refused |
| Bare Git repository | Refused with `bare_repository` |

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
  error    workharbor  pin_unpublished: local evidence shows no remote contains the pin
  warning  workharbor  checkout_differs_from_pin
note: remote-tracking refs may be stale; no fetch was made
```

Names, paths, URLs and refs have control characters escaped. Output is bounded; a truncated list says so.

## JSON

One `PinsResult` object per invocation, validated by `schemas/v1/pins.schema.json`. A refusal:

```json
{"schema_version":1,"command":"pins","status":"refused","backend":"jj",
 "input_path":".","boundary":{"effects":{"network":false,"fetch":false,"writes":false,"hooks":false},"complete":false},
 "submodules":[],"findings":[],
 "error":{"code":"backend_unsupported","message":"jj has no Git submodule model; run this in a Git-owned checkout"}}
```

## Effects

- Reads: superproject object database, index file, `.gitmodules`, each validated submodule Git directory, `FETCH_HEAD` metadata.
- Writes: none. No lock file remains. Index bytes and modification time are unchanged.
- Network, fetch, clone, `submodule init` and `submodule update`: none.
- Processes: only the fixed, isolated Git invocations listed in the research record, with no hook, filter, helper, alias, fsmonitor, textconv or external diff.

## Compatibility

New command and new schema. `inspect`, `inventory`, `tidy`, `workspace`, `hosting` and `version` contracts are unchanged. Finding codes are append-only within `schema_version: 1`.
