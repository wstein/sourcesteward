{
  "schema_version": 1,
  "command": "inventory",
  "status": "ok",
  "backend": "git",
  "input_path": "/work/super",
  "identity": {
    "schema_version": 1,
    "status": "ok",
    "backend": "git",
    "input_path": "/work/super",
    "evidence": [],
    "repository": {
      "git_dir": "/work/super/.git",
      "common_dir": "/work/super/.git",
      "worktree": "/work/super",
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
      "path": "/work/super",
      "name": "/work/super",
      "manager": "git",
      "registration": "present",
      "task_owner": "unknown",
      "commit_id": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      "change_id": null,
      "operation_id": null,
      "tracked": {
        "value": "true",
        "reason": ""
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
        "submodule disk state not observed; tracked is true from staged or ordinary-file evidence only"
      ]
    },
    {
      "path": "/work/super-wt",
      "name": "/work/super-wt",
      "manager": "git",
      "registration": "present",
      "task_owner": "unknown",
      "commit_id": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
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
  "files": [
    {
      "path": "README.md",
      "old_path": null,
      "scope": "disk",
      "change": "modified",
      "limitations": []
    },
    {
      "path": "README.md",
      "old_path": null,
      "scope": "tracked",
      "change": "modified",
      "limitations": []
    }
  ],
  "query": null,
  "diagnostics": []
}
```
