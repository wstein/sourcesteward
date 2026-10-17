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
| jj (standalone, colocated, secondary workspace) | supported (existing inventory with the `stored` scope only, requires the inventory's sandbox; dirty state `unknown`) | `not-applicable`, stated |
| GitButler, ambiguous | entry `error` (`unsupported-backend`, `ambiguous-backend`) | no |

## Inherited limits

`repos` embeds the single-repository inventory unchanged, so it inherits that operation's limits, and their effect on the exit status is part of this contract:

- **Content attributes.** The Git inventory reads attributes for every cached path with one `check-attr` call whose output is capped at 64 KiB. A repository with roughly 150 to 400 tracked files (depending on path length) therefore yields the entry error `git-inventory-failed` with the message `cannot safely observe content attributes`, even without any `.gitattributes`.
- **Record limit.** More than 1,024 aggregate inventory records yields `git-inventory-failed` with the message `inventory record limit exceeded` (or the inventory's own `inventory-limit`).
- **jj output.** A jj inventory fails with `jj-output-limit` when any single jj command prints more than 64 KiB (code reading, not yet measured: jj is absent from the authoring container).
- **Always partial.** A repository with registered gitlinks and a bare repository are `partial` under the default Git scopes; a stale linked worktree makes its repository `partial`.

Such an entry is `error` or `partial` with the cause on the entry, and the run ends with exit status 1; the other members are unaffected. Exit status 0 therefore means every member was fully observed. A workspace of large or submodule-bearing repositories mostly ends with exit status 1; lifting these limits is a change to single-repository inventory and a separate specification. Whether to fix that prerequisite first, or to cut `--submodules`, is an open maintainer decision (analysis, round 5).

`--submodules` also inherits the submodule pin audit's refusals (Spec 021 safety layer): a submodule is refused with `path-unsafe`, before any process runs in it, when its path or Git directory goes through a symlink, its `.git` pointer is not exactly the expected Git directory, the Git directory or its configuration is owned by another user or writable by others, its `core.worktree` or an include differs, or it has a `commondir` entry or a non-empty `objects/info/alternates` file. A submodule that borrows objects through alternates is therefore not inventoried by `repos`. The ownership rule is applied to every directory it walks, including the directory of an uninitialized submodule: Git creates directories writable by everyone under `umask 000`, and such a submodule directory is `path-unsafe` (the message names `chmod -R o-w`) rather than `skipped`. Two `.gitmodules` names for one path use the lowest name, and a Git directory that does not match that name is refused rather than guessed.

Dirty state of jj members is `unknown` (disk freshness needs a snapshot); a Git superproject with registered gitlinks and a bare repository are `partial` under the Git default scopes; these are backend or inherited differences, not equivalence claims.

## Human output

```text
SourceSteward repos: partial (3 entries)
effects: read-only, no network, no fetch, no repository writes
[0] /w/lib  git  ok       head main aaaaaaa  dirty=true  workspaces 1
[1] /w/app  git  partial  head main bbbbbbb  dirty=unknown  workspaces 1
      inventory-partial: submodule disk state excluded to avoid recursive helpers
[2] vendor/lib  submodule of [1]  skipped  submodule-uninitialized
summary: ok 1, partial 1, error 0, skipped 1; dirty 1; stale 0; foreign 0; conflicted refs 0
limitations: No ahead/behind, publication or task-ownership facts: single-repository inventory does not observe them.
limitations: Submodule checkouts: dirty state is unknown (Git reports a submodule's Git directory as its worktree).
```

Names and paths are escaped. Each boundary limitation is printed on its own `limitations:` line. An *attention workspace* is one that is stale, foreign or unverified; at most 16 are listed per entry from the embedded inventory, then a "+N more" line. The text is bounded by construction (at most 16 entries, at most 24 lines each (header, error, notes, 16 attention workspaces and the "+N more" line), each line escaped and truncated to 512 bytes, so below 256 KiB), tested in T004.

## JSON

One `ReposResult` per invocation, validated by `schemas/v1/repos.schema.json`; every non-null `inventory` also validates against the published `inventory.schema.json`. Example:

```json
{
 "schema_version": 1,
 "command": "repos",
 "status": "partial",
 "input_paths": [
  "/w/lib",
  "/w/app"
 ],
 "submodules_requested": true,
 "boundary": {
  "complete": false,
  "freshness": "sampled-nontransactional",
  "effects": {
   "network": false,
   "fetch": false,
   "repository_writes": false,
   "hooks": false,
   "private_temporary_files_for_jj": true
  },
  "limits": {
   "max_entries": 16,
   "max_path_bytes": 4096,
   "max_discovery_candidates": 64,
   "per_repository_seconds": 10,
   "total_seconds": 60,
   "parallelism": 4,
   "max_output_bytes": 8388608,
   "gitmodules_bytes": 65536
  },
  "limitations": [
   "No ahead/behind, publication or task-ownership facts: single-repository inventory does not observe them.",
   "Submodule checkouts: dirty state is unknown (Git reports a submodule's Git directory as its worktree).",
   "Submodule locations are confined by the submodule pin audit's checks: a submodule whose Git directory has alternates or a commondir entry, or that is not owned by the effective user or is writable by others, is refused with path-unsafe.",
   "Discovered submodule paths are checked before Git runs, but a path swapped for a symlink in between can be read before it is rejected."
  ]
 },
 "entries": [
  {
   "index": 0,
   "path": "/w/lib",
   "resolved_path": "/w/lib",
   "origin": "explicit",
   "parent": null,
   "status": "ok",
   "error": null,
   "backend": "git",
   "discovery": {
    "requested": true,
    "status": "ok",
    "submodules": 0,
    "ignored": 0,
    "deduplicated": 0,
    "reason": ""
   },
   "duplicate_of": null,
   "contained_in": null,
   "facts": {
    "head_state": "attached",
    "head_ref": "refs/heads/main",
    "head_commit_id": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "change_id": null,
    "dirty": {
     "value": "true",
     "reason": ""
    },
    "conflicted_references": 0,
    "workspaces": {
     "total": 1,
     "stale": 0,
     "foreign": 0,
     "unverified": 0
    }
   },
   "inventory": {
    "schema_version": 1,
    "command": "inventory",
    "status": "ok",
    "backend": "git",
    "input_path": "/w/lib",
    "identity": {
     "schema_version": 1,
     "status": "ok",
     "backend": "git",
     "input_path": "/w/lib",
     "evidence": [],
     "repository": {
      "git_dir": "/w/lib/.git",
      "common_dir": "/w/lib/.git",
      "worktree": "/w/lib",
      "bare": false,
      "linked_worktree": false,
      "object_format": "sha1",
      "head": {
       "state": "attached",
       "ref": "refs/heads/main",
       "oid": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
      }
     },
     "diagnostics": []
    },
    "boundary": {
     "complete": true,
     "freshness": "sampled-nontransactional",
     "limitations": [
      "Sampled observations are not transactional; concurrent and ABA changes may be missed."
     ],
     "limits": {
      "deadline_seconds": 10,
      "command_bytes": 65536,
      "result_bytes": 1048576,
      "records": 1024
     }
    },
    "workspaces": [
     {
      "path": "/w/lib",
      "name": "/w/lib",
      "manager": "git",
      "registration": "present",
      "task_owner": "unknown",
      "commit_id": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      "change_id": null,
      "operation_id": null,
      "tracked": {
       "value": "false",
       "reason": ""
      },
      "untracked": {
       "value": "true",
       "reason": ""
      },
      "conflicts": {
       "value": "false",
       "reason": ""
      },
      "limitations": []
     }
    ],
    "references": [],
    "bases": [],
    "files": [
     {
      "path": "f.txt",
      "old_path": null,
      "scope": "untracked",
      "change": "untracked",
      "limitations": []
     }
    ],
    "query": null,
    "diagnostics": []
   },
   "notes": []
  },
  {
   "index": 1,
   "path": "/w/app",
   "resolved_path": "/w/app",
   "origin": "explicit",
   "parent": null,
   "status": "partial",
   "error": {
    "code": "inventory-partial",
    "message": "submodule disk state excluded to avoid recursive helpers"
   },
   "backend": "git",
   "discovery": {
    "requested": true,
    "status": "ok",
    "submodules": 1,
    "ignored": 0,
    "deduplicated": 0,
    "reason": ""
   },
   "duplicate_of": null,
   "contained_in": null,
   "facts": {
    "head_state": "attached",
    "head_ref": "refs/heads/main",
    "head_commit_id": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
    "change_id": null,
    "dirty": {
     "value": "unknown",
     "reason": "submodule disk state excluded to avoid recursive helpers"
    },
    "conflicted_references": 0,
    "workspaces": {
     "total": 1,
     "stale": 0,
     "foreign": 0,
     "unverified": 0
    }
   },
   "inventory": {
    "schema_version": 1,
    "command": "inventory",
    "status": "partial",
    "backend": "git",
    "input_path": "/w/app",
    "identity": {
     "schema_version": 1,
     "status": "ok",
     "backend": "git",
     "input_path": "/w/app",
     "evidence": [],
     "repository": {
      "git_dir": "/w/app/.git",
      "common_dir": "/w/app/.git",
      "worktree": "/w/app",
      "bare": false,
      "linked_worktree": false,
      "object_format": "sha1",
      "head": {
       "state": "attached",
       "ref": "refs/heads/main",
       "oid": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
      }
     },
     "diagnostics": []
    },
    "boundary": {
     "complete": false,
     "freshness": "sampled-nontransactional",
     "limitations": [
      "Sampled observations are not transactional; concurrent and ABA changes may be missed.",
      "submodule disk state excluded to avoid recursive helpers"
     ],
     "limits": {
      "deadline_seconds": 10,
      "command_bytes": 65536,
      "result_bytes": 1048576,
      "records": 1024
     }
    },
    "workspaces": [
     {
      "path": "/w/app",
      "name": "/w/app",
      "manager": "git",
      "registration": "present",
      "task_owner": "unknown",
      "commit_id": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
      "change_id": null,
      "operation_id": null,
      "tracked": {
       "value": "unknown",
       "reason": "submodule disk state excluded to avoid recursive helpers"
      },
      "untracked": {
       "value": "false",
       "reason": ""
      },
      "conflicts": {
       "value": "false",
       "reason": ""
      },
      "limitations": [
       "submodule disk state excluded to avoid recursive helpers"
      ]
     }
    ],
    "references": [],
    "bases": [],
    "files": [],
    "query": null,
    "diagnostics": []
   },
   "notes": [
    "inventory-partial: submodule disk state excluded to avoid recursive helpers"
   ]
  },
  {
   "index": 2,
   "path": "vendor/lib",
   "resolved_path": "/w/app/vendor/lib",
   "origin": "submodule",
   "parent": 1,
   "status": "skipped",
   "error": {
    "code": "submodule-uninitialized",
    "message": "submodule is uninitialized"
   },
   "backend": "unknown",
   "discovery": {
    "requested": false,
    "status": "not-applicable",
    "submodules": 0,
    "ignored": 0,
    "deduplicated": 0,
    "reason": "discovery is not recursive"
   },
   "duplicate_of": null,
   "contained_in": 1,
   "facts": null,
   "inventory": null,
   "notes": []
  }
 ],
 "summary": {
  "entries": 3,
  "by_status": {
   "ok": 1,
   "partial": 1,
   "error": 0,
   "skipped": 1
  },
  "by_backend": {
   "git": 2,
   "jj": 0,
   "other": 1
  },
  "dirty": {
   "true": 1,
   "false": 0,
   "unknown": 1
  },
  "stale_workspaces": 0,
  "foreign_workspaces": 0,
  "unverified_workspaces": 0,
  "conflicted_references": 0,
  "skipped_uninitialized": 1,
  "discovery_not_applicable": 0,
  "inventories_omitted": 0
 },
 "diagnostics": []
}
```

Exit status for this example: 1 (overall `partial`: a repository with a registered gitlink is always `partial` under Git's default inventory scopes). Both embedded `inventory` objects were validated against `schemas/v1/inventory.schema.json` with the existing contract test helper while writing this spec (offline, run in a throwaway test that was not committed); the whole example is validated against `repos.schema.json` by task T002. Values of the embedded inventories follow the code's behavior (known `false` conflicts, the sampling limitation always present, an untracked file recorded in `files`); they were not copied from a real run, which the implementation's smoke test provides.

## Effects

- Reads: each member's repository and worktree metadata as inventory does; the superproject's HEAD `.gitmodules` blob and index entries for discovery; `.git` pointer files of discovered members; `Lstat` of path components.
- Writes: none to any repository. jj inventories create and remove the inventory's private temporary files outside the target (inherited, disclosed in `boundary.effects`).
- Network, fetch, hooks, filters, helpers, editors, prompts: none.
- Processes: per explicit and discovered entry one identity inspection (the existing `Inspect`: Git or jj processes as that operation starts them), then the inventory operation's processes (inherited). With `--submodules`, only for a parent whose identity is Git-owned, is not bare, has a born HEAD and passed the root and duplicate checks: `rev-parse --path-format=absolute --git-dir --git-path modules`, `config --bool --get extensions.submodulePathConfig`, `ls-tree -z -l HEAD -- .gitmodules`, `cat-file blob <oid from the ls-tree record>` and `config --file - --null --list --no-includes` on its bytes (never `config --blob`, see the 021 research note on signed-char Git), at most 64 `ls-files --stage -z -- <path>` (global `--literal-pathspecs`) and, only for a submodule that passed the confinement checks, a `config --file <Git directory>/config --null --list --no-includes` read, each through the pin audit's isolated runner (hooks, credential helpers, every transport and system configuration off), all of one parent's discovery calls under one 10-second bound per parent inside the total (identity inspection and inventory keep one 10-second deadline per call). Path validation, duplicate and nesting checks, `.git` pointer checks and uninitialized skips start no process. No Git discovery process runs in a jj-owned, GitButler or ambiguous parent.

## Compatibility

New command, new schema. `stw inventory`, `inspect`, `workspace`, `tidy`, `hosting` and `version` contracts are unchanged. Error codes and statuses are append-only within `schema_version: 1`.
