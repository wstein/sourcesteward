# Contract: `stw repos`

## Command

```text
stw repos PATH... [--submodules] [--json]
```

One to 16 paths. No standard input, prompt, editor or pager. No other flags.

| Flag | Meaning |
| --- | --- |
| `--json` | one `ReposResult` object, including partial and error results |
| `--submodules` | add each Git-owned explicit path's direct registered submodules as entries |

## Exit status

The normative table is in [spec.md](../spec.md#exit-status-normative): 0 overall `ok`, 1 `partial` or `error` (also cancellation and write failures), 2 usage. Precedence 2, 1, 0.

## Backend capability

| Backend | Inventory entry | `--submodules` |
| --- | --- | --- |
| Git (worktree, linked worktree, bare) | supported (existing inventory) | supported |
| jj (standalone, colocated, secondary workspace) | supported (existing inventory; requires the inventory's sandbox) | `not-applicable`, stated |
| GitButler, ambiguous | entry `error` (`unsupported-backend`, `ambiguous-backend`) | no |

Dirty state of jj members is `unknown` (disk freshness needs a snapshot); this is a backend difference, not an equivalence claim.

## Human output

```text
SourceSteward repos: partial (3 entries)
effects: read-only, no network, no fetch, no repository writes
[0] /w/app  git  ok  head main aa..  dirty=true  workspaces 2 (stale 0, foreign 0)
[1] /w/lib  git  ok  head detached bb..  dirty=false  workspaces 1
[2] /w/app/vendor/lib  submodule of [0]  skipped  submodule-uninitialized
summary: ok 2, partial 0, error 0, skipped 1; dirty 1; stale workspaces 0; foreign 0
limitations: no ahead/behind, publication or task-ownership facts
```

Names and paths are escaped; at most 16 attention workspaces are listed per entry with a "+N more" line; output is bounded.

## JSON

One `ReposResult` per invocation, validated by `schemas/v1/repos.schema.json`; every non-null `inventory` also validates against the published `inventory.schema.json`. Example (compact; abbreviated object ids are full 40-character ids in real output):

```json
{"schema_version":1,"command":"repos","status":"ok","input_paths":["/w/app"],"submodules_requested":true,
 "boundary":{"complete":true,"freshness":"sampled-nontransactional",
  "effects":{"network":false,"fetch":false,"repository_writes":false,"hooks":false,"private_temporary_files_for_jj":true},
  "limits":{"max_entries":16,"max_path_bytes":4096,"per_repository_seconds":10,"total_seconds":60,"parallelism":4,"max_output_bytes":8388608,"gitmodules_bytes":65536},
  "limitations":["No ahead/behind, publication or task-ownership facts: single-repository inventory does not observe them."]},
 "entries":[
  {"index":0,"path":"/w/app","resolved_path":"/w/app","origin":"explicit","parent":null,"status":"ok","error":null,"backend":"git",
   "discovery":{"requested":true,"status":"ok","submodules":1,"ignored":0,"reason":""},"duplicate_of":null,"contained_in":null,
   "facts":{"head_state":"attached","head_ref":"refs/heads/main","head_commit_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","change_id":null,
    "dirty":{"value":"false","reason":""},"conflicted_references":0,"workspaces":{"total":1,"stale":0,"foreign":0,"unverified":0}},
   "inventory":{"schema_version":1,"command":"inventory","status":"ok","backend":"git","input_path":"/w/app",
    "identity":{"schema_version":1,"status":"ok","backend":"git","input_path":"/w/app","evidence":[],"repository":{"git_dir":"/w/app/.git","common_dir":"/w/app/.git","worktree":"/w/app","bare":false,"linked_worktree":false,"object_format":"sha1","head":{"state":"attached","ref":"refs/heads/main","oid":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},"diagnostics":[]},
    "boundary":{"complete":true,"freshness":"sampled-nontransactional","limitations":[],"limits":{"deadline_seconds":10,"command_bytes":65536,"result_bytes":1048576,"records":1024}},
    "workspaces":[{"path":"/w/app","name":"/w/app","manager":"git","registration":"present","task_owner":"unknown","commit_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","change_id":null,"operation_id":null,"tracked":{"value":"false","reason":""},"untracked":{"value":"false","reason":""},"conflicts":{"value":"unknown","reason":"conflict state not observed"},"limitations":[]}],
    "references":[],"bases":[],"files":[],"query":null,"diagnostics":[]}},
  {"index":1,"path":"vendor/lib","resolved_path":"/w/app/vendor/lib","origin":"submodule","parent":0,"status":"skipped","error":{"code":"submodule-uninitialized","message":"registered submodule has no checkout"},"backend":"unknown",
   "discovery":{"requested":true,"status":"not-applicable","submodules":0,"ignored":0,"reason":"nested submodules are not traversed"},"duplicate_of":null,"contained_in":0,"facts":null,"inventory":null}],
 "summary":{"entries":2,"by_status":{"ok":1,"partial":0,"error":0,"skipped":1},"by_backend":{"git":1,"jj":0,"other":0},"dirty":{"true":0,"false":1,"unknown":0},"stale_workspaces":0,"foreign_workspaces":0,"unverified_workspaces":0,"conflicted_references":0,"skipped_uninitialized":1,"discovery_not_applicable":1,"inventories_omitted":0},
 "diagnostics":[]}
```

Exit status for this example: 0. The first entry's embedded `inventory` was validated against `schemas/v1/inventory.schema.json` with the existing contract test helper while writing this spec (Git 2.56.0 era schema, offline); the whole example is validated against `repos.schema.json` by task T002.

## Effects

- Reads: each member's repository and worktree metadata as inventory does; the superproject's HEAD `.gitmodules` blob and index entries for discovery; `.git` pointer files of discovered members; `Lstat` of path components.
- Writes: none to any repository. jj inventories create and remove the inventory's private temporary files outside the target (inherited, disclosed in `boundary.effects`).
- Network, fetch, hooks, filters, helpers, editors, prompts: none.
- Processes: Git and jj processes of the inventory operation per member (inherited), plus the three Git discovery invocations of the plan, each through the existing isolated runner. Explicit-path validation, duplicate and nesting checks, `.git` pointer checks and uninitialized skips start no process.

## Compatibility

New command, new schema. `stw inventory`, `inspect`, `workspace`, `tidy`, `hosting` and `version` contracts are unchanged. Error codes and statuses are append-only within `schema_version: 1`.
