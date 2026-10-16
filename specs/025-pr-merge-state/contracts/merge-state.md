# Contract: `stw hosting merge-state`

## Commands

```text
stw hosting merge-state NUMBER [PATH] [--repo OWNER/NAME] [--host HOST] [--remote NAME] [--json]
gh jj merge-state NUMBER [PATH] [--repo OWNER/NAME] [--host HOST] [--remote NAME] [--json]
```

`NUMBER` is a positive decimal integer. `PATH` defaults to the current directory. Flags and their rules are those of the Spec 020 leaves (`--host` defaults to `github.com`; remote text never selects a credential host; `--repo` never overrides ownership; `--repo` and `--remote` must agree). The command accepts no other flags, never prompts, opens an editor or pager, or reads input. `gh jj merge-state` requires a jj-owned workspace (colocated and secondary included) and refuses Git ownership with `hosting-jj-required`; `stw hosting merge-state` supports native Git and native jj. Both call one shared operation (see [plan.md](../plan.md)); there is no extension-only logic.

| Flag | Meaning |
| --- | --- |
| `--json` | Emit one `MergeStateResult` object, including `incomplete` and `error` results |

## Exit status

The table of statuses, diagnostics and exit codes is normative in [spec.md](../spec.md) (Result status, diagnostics and exit status). Summary: 0 for `ok` (including results full of `waits` or `unknown` items), 1 for `incomplete` and `error` (with a concise `Error:` line on standard error and, with `--json`, the full observation on standard output), 2 for usage errors with no JSON. Findings never change the status; there is no `--strict`. Consumers must check both the exit status and JSON validity, as for the other hosting leaves.

## Human output

Rendered from the same result, bounded, with every control character escaped. Only the fixed lines and item summaries of the data model (normative) appear:

```text
Merge state: #112 example/project (open, not a draft)
GitHub reports: mergeable=true mergeable_state=behind (GitHub's summary, not a conclusion)
Waiting on:
  [observed] required-check "check": Required status context check: pending. (sources: branch-protection, base-branch, check-runs)
  [observed] head-behind-base: The head is 3 commits behind the base branch tip; strict rule: true. (sources: compare, branch-protection)
May be waiting on:
  (none)
Context:
  [observed] strict-required-status-checks: A rule requires the head to be up to date with the base. (sources: branch-protection)
  [observed] auto-merge: Auto-merge is enabled with method rebase. (sources: pull-request)
  [observed] merge-methods: Merge methods not excluded by readable sources: rebase; rule limit: unknown. (sources: repository, branch-protection)
  [observed] linear-history-required: A rule requires linear history. (sources: branch-protection)
  [observed] administrators-included: Rules apply to administrators: true. (sources: branch-protection)
Sources: pull-request ok; repository ok; base-branch ok; branch-protection ok; branch-rules ok; compare ok; check-runs ok; commit-statuses ok; reviews skipped (not-needed)
Complete: yes. Network reads: 10 of at most 50.
Notice: Sampled evidence; it does not assert that the pull request can be merged or will merge, and it authorizes nothing.
```

An `unknown` item prints its template with the appended reason, for example `[unknown] classic-protection-unreadable: Classic branch protection exists but its details are not readable: strict, enforce_admins. Reason: not-found-ambiguous. (sources: base-branch, branch-protection)`. With no `waits` and no `may-wait` items the fixed line `No waiting condition was found in the evidence read.` is printed after the groups; outside the fixed notice and the labelled GitHub line the text never uses the words mergeable, ready, will merge or can merge. This wording is a tested invariant (SC-003).

## JSON

One `MergeStateResult` per invocation, validated by `schemas/v1/merge-state.schema.json`, shape in [data-model.md](../data-model.md). The incident-like observation above, as JSON:

```json
{
 "schema_version": 1,
 "kind": "merge-state",
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
    "ref": "refs/heads/main",
    "oid": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
   }
  },
  "diagnostics": []
 },
 "repository": {
  "host": "github.com",
  "owner": "example",
  "name": "project",
  "selection": "unique-candidate"
 },
 "candidates": [
  {
   "remote": "origin",
   "host": "github.com",
   "owner": "example",
   "name": "project"
  }
 ],
 "pull_request": {
  "number": 112,
  "state": "open",
  "merged": false,
  "draft": false,
  "head_sha": "f709f8be292ea9cd9dfd8a858385dd06d00545d9",
  "base_sha": "f7fa77c033e262d31956eb744cebdedf2012da58",
  "head_ref": "bot/update",
  "base_ref": "main",
  "head_repository": "example/project",
  "base_repository": "example/project",
  "auto_merge": {
   "method": "rebase"
  }
 },
 "github_reported": {
  "mergeable": true,
  "mergeable_state": "behind",
  "computed": true
 },
 "base_branch": {
  "ref": "main",
  "ref_supported": true,
  "tip_sha": "cccccccccccccccccccccccccccccccccccccccc",
  "protected": true
 },
 "verdict": "not-asserted",
 "notice": "Sampled evidence; it does not assert that the pull request can be merged or will merge, and it authorizes nothing.",
 "items": [
  {
   "code": "required-check",
   "subject": "check",
   "effect": "waits",
   "confidence": "observed",
   "reason": null,
   "summary": "Required status context check: pending.",
   "detail": {
    "state": "pending",
    "origin": [
     "branch-protection",
     "base-branch"
    ],
    "app_id": 15368,
    "matches": 1
   },
   "evidence": [
    {
     "source": "branch-protection",
     "fields": [
      "required_status_checks.checks"
     ]
    },
    {
     "source": "base-branch",
     "fields": [
      "protection.required_status_checks.checks"
     ]
    },
    {
     "source": "check-runs",
     "fields": [
      "name",
      "status",
      "conclusion",
      "app.id"
     ]
    }
   ]
  },
  {
   "code": "strict-required-status-checks",
   "subject": null,
   "effect": "info",
   "confidence": "observed",
   "reason": null,
   "summary": "A rule requires the head to be up to date with the base.",
   "detail": {
    "origin": [
     "branch-protection"
    ]
   },
   "evidence": [
    {
     "source": "branch-protection",
     "fields": [
      "required_status_checks.strict"
     ]
    }
   ]
  },
  {
   "code": "head-behind-base",
   "subject": null,
   "effect": "waits",
   "confidence": "observed",
   "reason": null,
   "summary": "The head is 3 commits behind the base branch tip; strict rule: true.",
   "detail": {
    "behind_by": 3,
    "ahead_by": 1,
    "strict": true
   },
   "evidence": [
    {
     "source": "compare",
     "fields": [
      "behind_by",
      "ahead_by"
     ]
    },
    {
     "source": "branch-protection",
     "fields": [
      "required_status_checks.strict"
     ]
    }
   ]
  },
  {
   "code": "auto-merge",
   "subject": null,
   "effect": "info",
   "confidence": "observed",
   "reason": null,
   "summary": "Auto-merge is enabled with method rebase.",
   "detail": {
    "method": "rebase"
   },
   "evidence": [
    {
     "source": "pull-request",
     "fields": [
      "auto_merge.merge_method"
     ]
    }
   ]
  },
  {
   "code": "merge-methods",
   "subject": null,
   "effect": "info",
   "confidence": "observed",
   "reason": null,
   "summary": "Merge methods not excluded by readable sources: rebase; rule limit: unknown.",
   "detail": {
    "allowed": [
     "rebase"
    ],
    "rule_limit": null,
    "sources": [
     "repository",
     "branch-protection"
    ]
   },
   "evidence": [
    {
     "source": "repository",
     "fields": [
      "allow_merge_commit",
      "allow_squash_merge",
      "allow_rebase_merge"
     ]
    },
    {
     "source": "branch-protection",
     "fields": [
      "required_linear_history.enabled"
     ]
    }
   ]
  },
  {
   "code": "linear-history-required",
   "subject": null,
   "effect": "info",
   "confidence": "observed",
   "reason": null,
   "summary": "A rule requires linear history.",
   "detail": {
    "origin": [
     "branch-protection"
    ]
   },
   "evidence": [
    {
     "source": "branch-protection",
     "fields": [
      "required_linear_history.enabled"
     ]
    }
   ]
  },
  {
   "code": "administrators-included",
   "subject": null,
   "effect": "info",
   "confidence": "observed",
   "reason": null,
   "summary": "Rules apply to administrators: true.",
   "detail": {
    "enabled": true
   },
   "evidence": [
    {
     "source": "branch-protection",
     "fields": [
      "enforce_admins.enabled"
     ]
    }
   ]
  }
 ],
 "sources": [
  {
   "id": "pull-request",
   "outcome": "ok",
   "failure_class": null,
   "requests": 2,
   "reason": null
  },
  {
   "id": "repository",
   "outcome": "ok",
   "failure_class": null,
   "requests": 1,
   "reason": null
  },
  {
   "id": "base-branch",
   "outcome": "ok",
   "failure_class": null,
   "requests": 2,
   "reason": null
  },
  {
   "id": "branch-protection",
   "outcome": "ok",
   "failure_class": null,
   "requests": 1,
   "reason": null
  },
  {
   "id": "branch-rules",
   "outcome": "ok",
   "failure_class": null,
   "requests": 1,
   "reason": null
  },
  {
   "id": "compare",
   "outcome": "ok",
   "failure_class": null,
   "requests": 1,
   "reason": null
  },
  {
   "id": "check-runs",
   "outcome": "ok",
   "failure_class": null,
   "requests": 1,
   "reason": null
  },
  {
   "id": "commit-statuses",
   "outcome": "ok",
   "failure_class": null,
   "requests": 1,
   "reason": null
  },
  {
   "id": "reviews",
   "outcome": "skipped",
   "failure_class": null,
   "requests": 0,
   "reason": "not-needed"
  }
 ],
 "summary": {
  "items": 7,
  "waits": 2,
  "may_wait": 0,
  "info": 5,
  "observed": 7,
  "inferred": 0,
  "unknown": 0
 },
 "diagnostics": [],
 "boundary": {
  "complete": true,
  "network": true,
  "requests_made": 10,
  "max_requests": 50,
  "max_pages": 10,
  "max_records": 1000,
  "max_bytes": 1048576,
  "timeout_seconds": 30,
  "max_items": 300,
  "limitations": [
   "Mergeability is not asserted; GitHub's reported state is shown separately.",
   "Evidence is sampled, not a transaction.",
   "Bypass actors, review-thread resolution, merge queue state and reviewer eligibility are not read.",
   "Reading a pull request can cause GitHub to compute its mergeability server side."
  ]
 },
 "observed_at": "2026-10-16T13:30:00Z"
}
```

An error (`status: "error"`) has `items` empty and a nonempty `diagnostics`; its null fields follow the normative rule in the data model (null unless the first pull request read succeeded and a later failure ended the operation); an `incomplete` result keeps the items derived before the failure.

## Effects

- Reads: native Git or jj identity and active remotes exactly as Spec 020 (bounded, isolated, jj sandboxed and snapshot-suppressed); then, over HTTPS to the selected API origin with the caller's selected-host credential, only the fixed `GET` requests of the Sources table.
- Writes: none. No local file, ref, index, jj working copy or operation-log change; no remote, pull request, protection, setting or auto-merge change; no re-run. Reading a pull request may cause GitHub to compute its mergeability server side; that is GitHub's own cache.
- Processes: only Spec 020 and 022 processes (native Git or jj reads, bounded `gh --version` and `gh auth token`). No hook, credential helper, editor or pager. Local repository resolution works without gh; only the remote reads need it.
- Bounds: the limits in the data model and spec; redirects and compressed responses are refused as in Spec 022.
- Output: at most 1 MiB; hostile text is escaped in the human form and bounded in both. Human output consists only of the fixed item summaries of the data model, the labelled GitHub line, source lines and the fixed notice.

## Transport interface (transport-neutral)

The operation depends only on this narrow interface, which both the Spec 022 SDK transport and a Spec 020-style subprocess transport can satisfy:

- `get(ctx, endpoint string) ([]byte, error)` (unchanged) where a non-2xx or failed read returns an error that carries a **failure class** (`unauthorized`, `forbidden`, `not-found`, `rate-limited` (429, 403 with `X-RateLimit-Remaining: 0`, or 403 with `Retry-After`), `other-http`, `too-large`, `malformed`, `transport`, `canceled`) retrievable with `errors.As`; the error message text is unchanged from today's so Spec 020/022 diagnostics do not change.
- Endpoints are generated by the operation from validated parts and are checked against a closed allowlist before any credential is attached; the Sources table lists the only shapes.
- No response body or header text other than the class crosses the interface.

## Compatibility

New leaf and new schema. `hosting repo|pr|ci`, `hosting.schema.json` and its fixtures, `inspect`, `inventory`, `tidy`, `workspace` and `version` contracts are unchanged. Spec 022's published diagnostics for the existing leaves are unchanged. Item codes are only added by a later schema revision; consumers must tolerate unknown codes.
