# Contract: `stw stack show` and `stw stack plan`

## Commands

```text
stw stack show [PATH] --base REF --tip REF [--pull-requests [--repo OWNER/NAME] [--host HOST] [--remote NAME]] [--json]
stw stack plan [PATH] --base REF --tip REF [--pull-requests [--repo OWNER/NAME] [--host HOST] [--remote NAME]] [--json]
```

`PATH` defaults to the current directory. `--base` and `--tip` are required and each names one reference; `--tip` is a local branch or jj bookmark, `--base` may also be a remote-tracking branch or a tracked remote bookmark. `--repo`, `--host` and `--remote` are valid only with `--pull-requests` and follow Spec 020 (host default `github.com`, remote text never selects a credential host, `--repo` never overrides ownership, `--repo` and `--remote` must agree). The commands accept no other flags, never prompt, open an editor or pager, or read input. No `gh jj` entry point exists for them.

| Flag | Meaning |
| --- | --- |
| `--base REF` | the reference the stack is based on; required |
| `--tip REF` | the top layer of the stack; required |
| `--pull-requests` | also read the pull requests and native stacks of the layers from GitHub (network effect, disclosed) |
| `--json` | emit one `StackResult` object, including `incomplete` and `error` results |

## Exit status

The statuses, diagnostics and exit codes are normative in [spec.md](../spec.md) (Status and exit table). Summary: 0 for `ok` (including results full of findings), 1 for `incomplete` and `error` (a concise `Error:` line on standard error and, with `--json`, the full observation on standard output), 2 for usage errors with no JSON. Findings never change the exit status; `plan` exits 0 when it prints steps or a `blocked` reason.

## Human output

Rendered from the same result, bounded, with every control character escaped. Only the fixed lines and the finding summaries of the data model (normative) appear:

```text
Stack: base main (adcdcf6) .. tip s3 (88da0eb), 3 layers, backend git
Layers (bottom first):
  1. s1  5758b57  1 commit   landed: applied
  2. s2  eceac4e  1 commit   landed: pending
  3. s3  88da0eb  1 commit   landed: pending
Findings:
  [action] [observed] layer-applied-in-base (layer 1): All 1 commits of layer 1 are already in the base as equivalent patches. (git-cherry)
  [notice] [observed] stack-behind-base: The stack is based 1 commit(s) behind the base. (git-range)
Plan (advisory; nothing was executed, and a plan authorizes nothing; native stack status not checked):
  s1  git rebase --onto main 5758b572620f6940ef3b8292408c97f91ece4f8b s2
  s2  git rebase --onto s2 eceac4e6d2eb9d8cb7fda6e8425ee7bdc7aeadfa s3   (after s1)
Complete: yes. Network reads: none.
Notice: Sampled advisory evidence; stw ran none of the printed commands and a plan authorizes nothing.
```

Remote evidence adds, per layer, `PR #N (open, stack 10 position 2 of 3, verified)` or `(name-matched)`, a `Native stacks:` line, and its sources line (`pull-requests-by-head ok; native-stack ok`), in the shape of the Spec 025 source summary. Findings in the human list are ordered `action`, `notice`, `info`, and within a severity by the Findings table order. An `unknown` finding prints its summary with the appended reason. With no `action` or `notice` finding the text says only that none was found in the evidence read, never that the stack is correct. A `link` step prints the environment hint and the label `performed by gh stack, not by stw`; a `fetch` step prints `git fetch REMOTE` or `jj git fetch --remote REMOTE`; an `observe` step prints "run `stw stack plan` again".

## JSON

One `StackResult` per invocation, validated by `schemas/v1/stack.schema.json`, shape in [data-model.md](../data-model.md). The observation above (Git, rebase-merged bottom layer, mode `plan`, no remote evidence), as JSON:

```json
{
 "schema_version": 1,
 "kind": "stack",
 "mode": "plan",
 "status": "ok",
 "backend": "git",
 "identity": {
  "schema_version": 1,
  "status": "ok",
  "backend": "git",
  "input_path": "/repo",
  "evidence": [],
  "repository": {
   "git_dir": "/repo/.git",
   "common_dir": "/repo/.git",
   "worktree": "/repo",
   "bare": false,
   "linked_worktree": false,
   "object_format": "sha1",
   "head": {
    "state": "attached",
    "ref": "refs/heads/s3",
    "oid": "88da0eb74325695bbfd47adb9d3771cde353bc28"
   }
  },
  "diagnostics": []
 },
 "base": {
  "name": "main",
  "kind": "branch",
  "commit_id": "adcdcf6554c91c0553bf92e74a68e035970e9bd1",
  "change_id": null,
  "remote": null,
  "trunk_ref": null,
  "ref_supported": true
 },
 "tip": {
  "name": "s3",
  "kind": "branch",
  "commit_id": "88da0eb74325695bbfd47adb9d3771cde353bc28",
  "change_id": null,
  "remote": null,
  "trunk_ref": null,
  "ref_supported": true
 },
 "fork_commit_id": "3a62e7161df6046c0a8da31cbd6d8c5763e0240f",
 "layers": [
  {
   "index": 1,
   "refs": [
    "s1"
   ],
   "primary_ref": "s1",
   "tip_commit_id": "5758b572620f6940ef3b8292408c97f91ece4f8b",
   "tip_change_id": null,
   "fork_commit_id": "3a62e7161df6046c0a8da31cbd6d8c5763e0240f",
   "commit_count": 1,
   "commits": [
    {
     "commit_id": "5758b572620f6940ef3b8292408c97f91ece4f8b",
     "change_id": null,
     "subject": "s1"
    }
   ],
   "upstream": null,
   "landed": "applied",
   "checked_out_in": [],
   "jj": null,
   "pull_request": null
  },
  {
   "index": 2,
   "refs": [
    "s2"
   ],
   "primary_ref": "s2",
   "tip_commit_id": "eceac4e6d2eb9d8cb7fda6e8425ee7bdc7aeadfa",
   "tip_change_id": null,
   "fork_commit_id": "5758b572620f6940ef3b8292408c97f91ece4f8b",
   "commit_count": 1,
   "commits": [
    {
     "commit_id": "eceac4e6d2eb9d8cb7fda6e8425ee7bdc7aeadfa",
     "change_id": null,
     "subject": "s2"
    }
   ],
   "upstream": null,
   "landed": "pending",
   "checked_out_in": [],
   "jj": null,
   "pull_request": null
  },
  {
   "index": 3,
   "refs": [
    "s3"
   ],
   "primary_ref": "s3",
   "tip_commit_id": "88da0eb74325695bbfd47adb9d3771cde353bc28",
   "tip_change_id": null,
   "fork_commit_id": "eceac4e6d2eb9d8cb7fda6e8425ee7bdc7aeadfa",
   "commit_count": 1,
   "commits": [
    {
     "commit_id": "88da0eb74325695bbfd47adb9d3771cde353bc28",
     "change_id": null,
     "subject": "s3"
    }
   ],
   "upstream": null,
   "landed": "pending",
   "checked_out_in": [],
   "jj": null,
   "pull_request": null
  }
 ],
 "findings": [
  {
   "code": "stack-behind-base",
   "layer": null,
   "subject": null,
   "severity": "notice",
   "confidence": "observed",
   "reason": null,
   "summary": "The stack is based 1 commit(s) behind the base.",
   "detail": {
    "behind_by": 1,
    "capped": false
   },
   "evidence": [
    {
     "source": "git-range",
     "fields": [
      "fork_commit_id",
      "base_commit_id"
     ]
    }
   ]
  },
  {
   "code": "layer-applied-in-base",
   "layer": 1,
   "subject": null,
   "severity": "action",
   "confidence": "observed",
   "reason": null,
   "summary": "All 1 commits of layer 1 are already in the base as equivalent patches.",
   "detail": {
    "equivalent": 1,
    "total": 1
   },
   "evidence": [
    {
     "source": "git-cherry",
     "fields": [
      "marks"
     ]
    }
   ]
  }
 ],
 "plan": {
  "advisory": true,
  "blocked": null,
  "steps": [
   {
    "id": "s1",
    "kind": "rebase",
    "note": null,
    "layer": 2,
    "findings": [
     "layer-applied-in-base"
    ],
    "effect": "local-history-rewrite",
    "performed_by": "maintainer",
    "argv": [
     "git",
     "rebase",
     "--onto",
     "main",
     "5758b572620f6940ef3b8292408c97f91ece4f8b",
     "s2"
    ],
    "display": "git rebase --onto main 5758b572620f6940ef3b8292408c97f91ece4f8b s2",
    "environment": [],
    "expected": {
     "layer_tip": "eceac4e6d2eb9d8cb7fda6e8425ee7bdc7aeadfa",
     "old_parent_tip": "5758b572620f6940ef3b8292408c97f91ece4f8b",
     "new_parent": "main",
     "new_parent_commit_id": "adcdcf6554c91c0553bf92e74a68e035970e9bd1"
    },
    "requires": []
   },
   {
    "id": "s2",
    "kind": "rebase",
    "note": null,
    "layer": 3,
    "findings": [
     "layer-applied-in-base"
    ],
    "effect": "local-history-rewrite",
    "performed_by": "maintainer",
    "argv": [
     "git",
     "rebase",
     "--onto",
     "s2",
     "eceac4e6d2eb9d8cb7fda6e8425ee7bdc7aeadfa",
     "s3"
    ],
    "display": "git rebase --onto s2 eceac4e6d2eb9d8cb7fda6e8425ee7bdc7aeadfa s3",
    "environment": [],
    "expected": {
     "layer_tip": "88da0eb74325695bbfd47adb9d3771cde353bc28",
     "old_parent_tip": "eceac4e6d2eb9d8cb7fda6e8425ee7bdc7aeadfa",
     "new_parent": "s2",
     "new_parent_commit_id": null
    },
    "requires": [
     "s1"
    ]
   }
  ],
  "native_checked": false
 },
 "remote": null,
 "diagnostics": [],
 "boundary": {
  "complete": true,
  "network": false,
  "freshness": "sampled",
  "limitations": [
   "Remote-tracking refs may be stale; nothing was fetched.",
   "Native stack status was not checked; if GitHub manages this stack it rewrites member branches after a merge, so fetch and run stack plan --pull-requests before running any rebase step."
  ],
  "limits": {
   "deadline_seconds": 30,
   "commits": 500,
   "layers": 32,
   "findings": 400,
   "steps": 64,
   "requests": 50,
   "result_bytes": 1048576
  }
 },
 "notice": "Sampled advisory evidence; stw ran none of the printed commands and a plan authorizes nothing.",
 "observed_at": "2026-10-17T12:00:00Z"
}
```

## Effects

- Without `--pull-requests`: local reads only, by the isolated Git runner (explicit `--git-dir`/`--work-tree` after the ownership check, the isolation prefix and process allowlist of [plan.md](../plan.md)) or the jj session pinned to one operation (`--ignore-working-copy`). No network, no authentication, no fetch.
- With `--pull-requests`: additionally the fixed `GET` requests of the Remote sources table with the caller's selected-host credentials (Spec 022 token lookup), at most 50, none that writes.
- Never: a write to any ref, file, index, object database (loose objects included), remote, pull request, stack or setting; a jj snapshot or operation-log change; a hook, filter, textconv, external diff, alias, editor, pager or prompt; a started printed command.
- Reading a pull request or stack can cause GitHub to compute server-side state; that is GitHub's cache, not a repository change.

## Compatibility

Every existing command, flag, schema and fixture is unchanged and byte-identical (a test compares them to the base). `stack.schema.json` is new and self-contained. The first-draft contract for diff and changed-file output is not provided (withdrawn; see the Disposition table in [spec.md](../spec.md)).
