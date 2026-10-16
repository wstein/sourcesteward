# Contract: `stw hooks`

## Command

```text
stw hooks [PATH] [--json] [--strict]
```

`PATH` defaults to the current directory and may be inside the worktree. The command accepts no other flags. It never prompts, opens an editor or pager, runs a hook, or contacts a network.

| Flag | Meaning |
| --- | --- |
| `--json` | Emit one `HooksResult` object, including refusals and operational errors |
| `--strict` | Exit with status 3 when any finding has `error` severity; the result is still printed in full |

## Exit status

| Status | Meaning |
| --- | --- |
| 0 | Complete result; findings do not change this without `--strict` |
| 1 | Refusal, operational failure, cancellation or an incomplete result, with or without `--strict` |
| 2 | Usage error |
| 3 | `--strict`, a complete result and at least one `error` finding; one line on standard error names the count, standard output carries the full result |

Precedence: 2, then 1, then 3, then 0. An incomplete result exits 1 even with error findings, so `hooks-limit` and `hooks-state-changed` never reach status 3. Because `guards-bypassed-on-backend` is an error finding, `stw hooks --strict` in a colocated jj workspace with active guards exits 3 while the jj claim is `documented`; this fails closed by design.

## Backend capability

| Backend | `stw hooks` |
| --- | --- |
| Git (worktree, linked worktree) | Supported; `runs_hooks: yes` at the catalog's evidence level |
| jj with a `.git` entry (colocated, including a worktree-backed secondary workspace) | Supported through the jj adapter (sandbox probe and `--version` only); Git hook configuration read from the Git repository behind `.git`, outside the sandbox; claims at the catalog's evidence level |
| jj without a `.git` entry | Supported; guard set not evaluated (`guards_evaluated: false`, `workspace-has-no-git-configuration`); claims at the catalog's evidence level |
| GitButler-managed | Refused with `unsupported-backend` |
| Conflicting ownership markers | Refused with `ambiguous-backend` |
| Bare Git repository | Refused with `bare-repository` |
| Not a repository | Refused with `not-repository` |

jj sandbox or version problems and the error codes of the existing ownership decision return the codes of the spec's refusal table with status `error`.

## Derivation rules

- When `guards_evaluated` is false no bypass finding is raised.
- At most one finding of each bypass code per operation, `subject` null, naming the affected hooks (bounded): `guards-bypassed-on-backend` when `runs_hooks` is `no` and an active blocking guard exists; `guards-may-be-bypassed` when `runs_hooks` is `unknown` and an active blocking guard exists, or when an unknown-engine blocking configuration hook (spec FR-009) exists under any `runs_hooks`. Active guards alone raise nothing when `runs_hooks` is `yes`. Findings are derived from the OperationHook rows (`bypassed` true or null).
- `backend-hook-behavior-unverified` once (operation null) when any claim used in the result (including `git-config-hooks` when configuration hooks exist) has a level other than `measured` after the version-coverage rule.
- `no-active-guards` once when `guards_evaluated` is true, `guards_active` is 0 for both operations and no unknown-engine blocking configuration hook exists. Both bypass codes may appear on one operation.
- Directory and file findings (`hook-symlink-outside`, `hook-unsafe-permissions`, `hook-unsafe-owner`) carry `subject` `directory` or the hook name.

## Human output

A header with identity and backend, the hooks path and directory, the hooks present, one block per operation with its evidence level, then findings, then a footer:

```text
SourceSteward hooks: /work/harbor (jj colocated, jj 0.46.0, git 2.56.0)
effects: read-only, hooks never executed, hook contents never read

hooks path: .githooks -> /work/harbor/.githooks (local config)
hooks present: commit-msg, pre-commit, pre-push (all active; tracked means present in the Git index, in colocated jj the parent of the working-copy commit)

commit via "jj commit": runs hooks? no (documented, not measured for jj 0.46.0)
  pre-commit            active   BYPASSED
  prepare-commit-msg    absent
  commit-msg            active   BYPASSED
  post-commit           absent
  reference-transaction absent
push via "jj git push": runs hooks? unknown (unverified)
  pre-push              active   MAY BE BYPASSED
  reference-transaction absent

findings: 1 error, 2 warnings, 1 info
  error    commit  guards-bypassed-on-backend: jj commit is documented not to run Git hooks
  warning  push    guards-may-be-bypassed: whether jj git push runs Git hooks is not established
  warning          backend-hook-behavior-unverified: claims jj-commit, jj-push are not measured
  info             hooks-tracked: hooks come from repository content
notes (abbreviated here; JSON carries all): hook contents are not read; overrides such as --no-verify are not detected
```

Names, paths, values and origins have control, format and separator characters and invalid bytes escaped. Output is bounded; a truncated list says so.

## JSON

One `HooksResult` per invocation, validated by `schemas/v1/hooks.schema.json`. A colocated jj workspace with the WorkHarbor hooks (versions are illustrative):

```json
{
 "schema_version": 1,
 "command": "hooks",
 "status": "ok",
 "backend": "jj",
 "input_path": "/work/harbor",
 "identity": {
  "root": "/work/harbor",
  "git_dir": "/work/harbor/.git",
  "git_version": "2.56.0",
  "jj_version": "0.46.0",
  "colocated": true,
  "common_dir": "/work/harbor/.git"
 },
 "boundary": {
  "effects": {
   "network": false,
   "writes": false,
   "hooks_executed": false,
   "hook_contents_read": false,
   "hook_paths_opened": false,
   "git_processes": true,
   "jj_processes": true
  },
  "limits": {
   "deadline_seconds": 10,
   "git_process_seconds": 5,
   "stream_bytes": 65536,
   "result_bytes": 262144,
   "directory_entries": 256,
   "unrecognized_names": 16,
   "config_hooks": 64,
   "value_bytes": 4096,
   "level_records": 16
  },
  "complete": true,
  "notes": [
   "Hook contents are never read, so a missing interpreter or a script that disables itself is not detected.",
   "Per-command overrides and the caller environment are not applied or detected.",
   "The Git version used for jj-push coverage is the Git on the PATH of stw, which may differ from the Git jj spawns.",
   "jj hook behavior is reported at the stated evidence level; the conformance lane establishes it."
  ]
 },
 "configuration": {
  "hooks_path": {
   "state": "configured",
   "effective_value": ".githooks",
   "resolved_directory": "/work/harbor/.githooks",
   "levels": [
    {
     "scope": "local",
     "value": ".githooks",
     "origin": "/work/harbor/.git/config"
    }
   ],
   "overridden": false,
   "levels_truncated": false
  },
  "directory": {
   "path": "/work/harbor/.githooks",
   "kind": "directory",
   "resolves_to": null,
   "inside_repository": true,
   "owner_is_effective_user": true,
   "group_writable": false,
   "world_writable": false,
   "entries_scanned": 3,
   "unrecognized_names": [],
   "unrecognized_truncated": false
  },
  "config_hooks": {
   "engine": "unknown",
   "evidence": {
    "level": "unverified",
    "version_covered": false,
    "detected_versions": {
     "git": "2.56.0",
     "jj": null
    },
    "measured_range": null,
    "sources": [
     {
      "kind": "measurement",
      "reference": "research.md measurement 10 (Git 2.56.0, scratch)"
     }
    ]
   },
   "entries": [],
   "truncated": false
  },
  "worktree_config_enabled": false,
  "caller_environment": []
 },
 "hooks": [
  {
   "name": "commit-msg",
   "hook_scope": "client",
   "kind": "file",
   "state": "active",
   "executable_for_user": true,
   "mode": "0755",
   "size": 412,
   "owner_is_effective_user": true,
   "group_writable": false,
   "world_writable": false,
   "symlink": null,
   "tracked": "tracked"
  },
  {
   "name": "pre-commit",
   "hook_scope": "client",
   "kind": "file",
   "state": "active",
   "executable_for_user": true,
   "mode": "0755",
   "size": 988,
   "owner_is_effective_user": true,
   "group_writable": false,
   "world_writable": false,
   "symlink": null,
   "tracked": "tracked"
  },
  {
   "name": "pre-push",
   "hook_scope": "client",
   "kind": "file",
   "state": "active",
   "executable_for_user": true,
   "mode": "0755",
   "size": 530,
   "owner_is_effective_user": true,
   "group_writable": false,
   "world_writable": false,
   "symlink": null,
   "tracked": "tracked"
  }
 ],
 "operations": [
  {
   "operation": "commit",
   "command": "jj commit",
   "claim_id": "jj-commit",
   "runs_hooks": "no",
   "evidence": {
    "level": "documented",
    "version_covered": false,
    "measured_range": null,
    "sources": [
     {
      "kind": "documentation",
      "reference": "https://docs.jj-vcs.dev/latest/git-compatibility/ (Hooks: No)"
     }
    ],
    "detected_versions": {
     "git": null,
     "jj": "0.46.0"
    }
   },
   "exceptions": [],
   "hooks": [
    {
     "name": "pre-commit",
     "role": "blocking",
     "state": "active",
     "source": "hooks-dir",
     "bypassed": true,
     "skippable_by": []
    },
    {
     "name": "prepare-commit-msg",
     "role": "blocking",
     "state": "absent",
     "source": null,
     "bypassed": false,
     "skippable_by": []
    },
    {
     "name": "commit-msg",
     "role": "blocking",
     "state": "active",
     "source": "hooks-dir",
     "bypassed": true,
     "skippable_by": []
    },
    {
     "name": "post-commit",
     "role": "observer",
     "state": "absent",
     "source": null,
     "bypassed": false,
     "skippable_by": []
    },
    {
     "name": "reference-transaction",
     "role": "blocking",
     "state": "absent",
     "source": null,
     "bypassed": false,
     "skippable_by": []
    }
   ],
   "guards_active": 2,
   "guards_bypassed": 2,
   "guards_evaluated": true
  },
  {
   "operation": "push",
   "command": "jj git push",
   "claim_id": "jj-push",
   "runs_hooks": "unknown",
   "evidence": {
    "level": "unverified",
    "version_covered": false,
    "measured_range": null,
    "sources": [
     {
      "kind": "documentation",
      "reference": "https://docs.jj-vcs.dev/latest/config/ (Git remote interactions are handled by spawning a git subprocess)"
     }
    ],
    "detected_versions": {
     "git": "2.56.0",
     "jj": "0.46.0"
    }
   },
   "exceptions": [],
   "hooks": [
    {
     "name": "pre-push",
     "role": "blocking",
     "state": "active",
     "source": "hooks-dir",
     "bypassed": null,
     "skippable_by": []
    },
    {
     "name": "reference-transaction",
     "role": "blocking",
     "state": "absent",
     "source": null,
     "bypassed": false,
     "skippable_by": []
    }
   ],
   "guards_active": 1,
   "guards_bypassed": 0,
   "guards_evaluated": true
  }
 ],
 "findings": [
  {
   "code": "guards-bypassed-on-backend",
   "severity": "error",
   "operation": "commit",
   "subject": null,
   "message": "jj commit is documented not to run Git hooks; active guards pre-commit, commit-msg are bypassed (evidence: documented)"
  },
  {
   "code": "guards-may-be-bypassed",
   "severity": "warning",
   "operation": "push",
   "subject": null,
   "message": "whether jj git push runs Git hooks is not established; active guard pre-push may be bypassed (evidence: unverified)"
  },
  {
   "code": "backend-hook-behavior-unverified",
   "severity": "warning",
   "operation": null,
   "subject": null,
   "message": "claims jj-commit, jj-push are not measured for jj 0.46.0"
  },
  {
   "code": "hooks-tracked",
   "severity": "info",
   "operation": null,
   "subject": "directory",
   "message": "hooks come from repository content; a checkout without them removes the guards"
  }
 ],
 "summary": {
  "errors": 1,
  "warnings": 2,
  "infos": 1,
  "hooks_present": 3,
  "guards_active": 3,
  "guards_bypassed": 2
 },
 "error": null
}
```

A refusal:

```json
{"schema_version":1,"command":"hooks","status":"refused","backend":"gitbutler","input_path":"/work/butler","identity":null,
 "boundary":{"effects":{"network":false,"writes":false,"hooks_executed":false,"hook_contents_read":false,"hook_paths_opened":false,"git_processes":true,"jj_processes":false},
  "limits":{"deadline_seconds":10,"git_process_seconds":5,"stream_bytes":65536,"result_bytes":262144,"directory_entries":256,"unrecognized_names":16,"config_hooks":64,"value_bytes":4096,"level_records":16},
  "complete":false,"notes":[]},
 "configuration":null,"hooks":[],"operations":[],"findings":[],
 "summary":{"errors":0,"warnings":0,"infos":0,"hooks_present":0,"guards_active":0,"guards_bypassed":0},
 "error":{"code":"unsupported-backend","message":"hook disclosure is not supported for GitButler-managed workspaces; no hook behavior is claimed"}}
```

## Effects

- Reads: Git configuration at system, global, local and worktree level through isolated `git config` and `git rev-parse` reads; `lstat`, `readlink`, `access(X_OK)` (real user) and, for a confined directory only, one bounded listing opened with `O_DIRECTORY|O_NONBLOCK|O_NOFOLLOW`; the index for tracking. For jj, only the existing sandbox probe and `--version`. The tracking read runs `ls-files` from the worktree root with the catalog names as literal pathspecs.
- Writes: none. No lock file remains. The jj working copy and operation log are unchanged.
- Opens: no hook entry and no hook content is opened or read; the only open is the bounded listing of a confined hooks directory.
- Executes: no hook, configuration hook command, `git hook`, filter, helper or alias. Only the fixed Git invocations of the process allowlist in the research record, and, for a jj-owned workspace, the existing sandboxed jj session. Ambiguous ownership refuses before any process starts. GitButler, bare and not-a-repository refusals use only the existing Git detection and start no jj process and no hooks-specific Git read.
- Network: none.

## Compatibility

New command and new schema. `inspect` (schema 1 and 2), `inventory`, `tidy`, `workspace`, `hosting` and `version` contracts are unchanged. Finding codes and the catalog values are append-only within `schema_version: 1`; a change of a claim's `runs_hooks` or evidence level in a later release is a catalog change, not a schema change, and is recorded in the verification note and the changelog of the backend guide.
