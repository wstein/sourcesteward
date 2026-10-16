# Contract: `stw hooks`

## Command

```text
stw hooks [PATH] [--json]
```

`PATH` defaults to the current directory and may be inside the worktree. The command accepts no other flags. It never prompts, opens an editor or pager, runs a hook, or contacts a network.

| Flag | Meaning |
| --- | --- |
| `--json` | Emit one `HooksResult` object, including refusals and operational errors |

## Exit status

| Status | Meaning |
| --- | --- |
| 0 | Complete result; findings of any severity do not change this |
| 1 | Refusal, operational failure, cancellation or an incomplete result |
| 2 | Usage error |

Precedence: 2, then 1, then 0. There is no strict mode in this version; a caller that wants to fail on `guards-bypassed-on-backend` reads the JSON findings.

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

The derivation of the findings is normative in the spec's findings section and is not repeated here. The result is built from the OperationHook rows: `bypassed` true gives `guards-bypassed-on-backend`, `bypassed` null gives `guards-may-be-bypassed`, at most one finding of each code per operation.

## Human output

A header with identity and backend, the hooks path and directory, the hooks present, one block per operation with its evidence level, then findings, then a footer:

```text
SourceSteward hooks: /work/harbor (jj colocated, jj 0.46.0, git 2.56.0)
effects: read-only, hooks never executed, hook contents never read

hooks path: .githooks (local scope) -> /work/harbor/.githooks
hooks present: commit-msg, pre-commit, pre-push (all active)

commit via "jj commit": runs hooks? no (documented, not measured for jj 0.46.0)
  pre-commit            active   BYPASSED
  prepare-commit-msg    absent
  commit-msg            active   BYPASSED
  post-commit           absent
  reference-transaction absent
push via "jj git push": runs hooks? unknown (unverified)
  pre-push              active   MAY BE BYPASSED
  reference-transaction absent

findings: 1 error, 2 warnings, 0 infos
  error    commit  guards-bypassed-on-backend: jj commit is documented not to run Git hooks
  warning  push    guards-may-be-bypassed: whether jj git push runs Git hooks is not established
  warning          backend-hook-behavior-unverified: claims jj-commit, jj-push are not measured
notes (abbreviated here; JSON carries all): hook contents are not read; overrides such as --no-verify are not detected
```

Paths, values and messages have control, format and separator characters and invalid bytes escaped. Output is bounded; a truncated value says so.

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
  "colocated": true
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
   "value_bytes": 4096
  },
  "complete": true,
  "notes": [
   "Hook contents are never read, so a missing interpreter or a script that disables itself is not detected.",
   "Per-command overrides (such as --no-verify or -c core.hooksPath) and the caller's GIT_* environment are not detected or applied.",
   "Hooks in the default hooks directory are inert while core.hooksPath is set and are not examined.",
   "The Git version used for jj-push coverage is the Git on the PATH of stw, which may differ from the Git jj spawns."
  ]
 },
 "configuration": {
  "hooks_path": {
   "state": "configured",
   "effective_value": ".githooks",
   "effective_scope": "local",
   "resolved_directory": "/work/harbor/.githooks"
  },
  "directory": {
   "path": "/work/harbor/.githooks",
   "kind": "directory"
  },
  "config_hooks_detected": false
 },
 "hooks": [
  {
   "name": "commit-msg",
   "kind": "file",
   "state": "active",
   "executable_for_user": true,
   "mode": "0755",
   "symlink": null
  },
  {
   "name": "pre-commit",
   "kind": "file",
   "state": "active",
   "executable_for_user": true,
   "mode": "0755",
   "symlink": null
  },
  {
   "name": "pre-push",
   "kind": "file",
   "state": "active",
   "executable_for_user": true,
   "mode": "0755",
   "symlink": null
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
    "detected_versions": {
     "git": null,
     "jj": "0.46.0"
    },
    "measured_range": null,
    "sources": [
     {
      "kind": "documentation",
      "reference": "https://docs.jj-vcs.dev/latest/git-compatibility/ (Hooks: No)"
     }
    ]
   },
   "exceptions": [],
   "guards_evaluated": true,
   "hooks": [
    {
     "name": "pre-commit",
     "role": "blocking",
     "state": "active",
     "bypassed": true,
     "skippable_by": []
    },
    {
     "name": "prepare-commit-msg",
     "role": "blocking",
     "state": "absent",
     "bypassed": false,
     "skippable_by": []
    },
    {
     "name": "commit-msg",
     "role": "blocking",
     "state": "active",
     "bypassed": true,
     "skippable_by": []
    },
    {
     "name": "post-commit",
     "role": "observer",
     "state": "absent",
     "bypassed": false,
     "skippable_by": []
    },
    {
     "name": "reference-transaction",
     "role": "blocking",
     "state": "absent",
     "bypassed": false,
     "skippable_by": []
    }
   ],
   "guards_active": 2,
   "guards_bypassed": 2
  },
  {
   "operation": "push",
   "command": "jj git push",
   "claim_id": "jj-push",
   "runs_hooks": "unknown",
   "evidence": {
    "level": "unverified",
    "version_covered": false,
    "detected_versions": {
     "git": "2.56.0",
     "jj": "0.46.0"
    },
    "measured_range": null,
    "sources": [
     {
      "kind": "documentation",
      "reference": "https://docs.jj-vcs.dev/latest/config/ (Git remote interactions are handled by spawning a git subprocess)"
     }
    ]
   },
   "exceptions": [],
   "guards_evaluated": true,
   "hooks": [
    {
     "name": "pre-push",
     "role": "blocking",
     "state": "active",
     "bypassed": null,
     "skippable_by": []
    },
    {
     "name": "reference-transaction",
     "role": "blocking",
     "state": "absent",
     "bypassed": false,
     "skippable_by": []
    }
   ],
   "guards_active": 1,
   "guards_bypassed": 0
  }
 ],
 "findings": [
  {
   "code": "guards-bypassed-on-backend",
   "severity": "error",
   "operation": "commit",
   "message": "jj commit is documented not to run Git hooks; active guards pre-commit, commit-msg are bypassed (evidence: documented)"
  },
  {
   "code": "guards-may-be-bypassed",
   "severity": "warning",
   "operation": "push",
   "message": "whether jj git push runs Git hooks is not established; active guard pre-push may be bypassed (evidence: unverified)"
  },
  {
   "code": "backend-hook-behavior-unverified",
   "severity": "warning",
   "operation": null,
   "message": "claims jj-commit, jj-push are not measured for jj 0.46.0"
  }
 ],
 "summary": {
  "errors": 1,
  "warnings": 2,
  "infos": 0,
  "hooks_present": 3,
  "guards_active": 3,
  "guards_bypassed": 2
 },
 "error": null
}
```

A refusal:

```json
{
 "schema_version": 1,
 "command": "hooks",
 "status": "refused",
 "backend": "gitbutler",
 "input_path": "/work/butler",
 "identity": null,
 "boundary": {
  "effects": {
   "network": false,
   "writes": false,
   "hooks_executed": false,
   "hook_contents_read": false,
   "hook_paths_opened": false,
   "git_processes": true,
   "jj_processes": false
  },
  "limits": {
   "deadline_seconds": 10,
   "git_process_seconds": 5,
   "stream_bytes": 65536,
   "result_bytes": 262144,
   "value_bytes": 4096
  },
  "complete": false,
  "notes": []
 },
 "configuration": null,
 "hooks": [],
 "operations": [],
 "findings": [],
 "summary": {
  "errors": 0,
  "warnings": 0,
  "infos": 0,
  "hooks_present": 0,
  "guards_active": 0,
  "guards_bypassed": 0
 },
 "error": {
  "code": "unsupported-backend",
  "message": "hook disclosure is not supported for GitButler-managed workspaces; no hook behavior is claimed"
 }
}
```

## Effects

- Reads: Git configuration through the allowlisted `git config` and `git rev-parse` reads; `lstat`, `readlink` and `access(X_OK)` (real user) for the six hook names. For jj, only the existing sandbox probe and `--version`.
- Writes: none. No lock file remains. The jj working copy and operation log are unchanged.
- Opens: no hook entry, no directory and no hook content is opened or read.
- Executes: no hook, configuration hook command, `git hook`, filter, helper or alias. Only the fixed Git invocations of the process allowlist in the research record and, for a jj-owned workspace, the existing sandbox probe and `--version`. Ambiguous ownership refuses before any process starts. GitButler, bare and not-a-repository refusals use only the existing Git detection and start no jj process and no hooks-specific Git read.
- Network: none.

## Compatibility

New command and new schema. `inspect` (schema 1 and 2), `inventory`, `tidy`, `workspace`, `hosting` and `version` contracts are unchanged. Finding codes and the catalog values are append-only within `schema_version: 1`; a change of a claim's `runs_hooks` or evidence level in a later release is a catalog change, not a schema change, and is recorded in the verification note.
