# Contract: `stw stack show` and `stw stack plan`

**UNPUBLISHED Markdown contract**: refreshed 2026-10-25; `stw stack` and `schemas/v1/stack.schema.json` remain unimplemented. Examples are proposed results, not measured CLI output. Published JSON files, including the implemented hidden Spec 015 v1 contract, remain unchanged.

## Commands

```text
stw stack show [PATH] --base REF --tip REF [--pull-requests [--repo OWNER/NAME] [--host HOST] [--remote NAME]] [--json]
stw stack plan [PATH] --base REF --tip REF [--pull-requests [--repo OWNER/NAME] [--host HOST] [--remote NAME]] [--after-fetch] [--json]
```

`PATH` defaults to the current directory. `--base` and `--tip` are required and each names one reference; `--tip` is a local branch or jj bookmark, `--base` may also be a remote-tracking branch or a tracked remote bookmark. `--repo`, `--host` and `--remote` are valid only with `--pull-requests` and follow Spec 020 (host default `github.com`, remote text never selects a credential host, `--repo` never overrides ownership, `--repo` and `--remote` must agree). Without an explicit `show` or `plan` subcommand the group shows help and performs no observation, prompt or mutation. Under the current proposed surface the commands accept no other flags, never prompt, open an editor or pager, or read input. No `gh jj` entry point exists for them.

| Flag | Meaning |
| --- | --- |
| `--base REF` | the reference the stack is based on; required |
| `--tip REF` | the top layer of the stack; required |
| `--pull-requests` | also read the pull requests and native stacks of the layers from GitHub (network effect, disclosed) |
| `--after-fetch` | `plan` only: the caller declares a fetch was just done; not verified; a still-missing object becomes the terminating note `object-unreachable` |
| `--json` | emit one `StackResult` object, including `incomplete` and `error` results |

## Shared flag vocabulary and owner decision gates

This proposed contract uses `--pull-requests` only; it has no implicit `--online` alias. Spec 041 uses shared `--online` and `--timeout`; Specs 041/043 both use `--json`/`--human`. `--state-dir` is inapplicable here because no observation store exists. This draft defaults to human output and a fixed 30-second deadline; it does not yet accept `--human` or `--timeout`. OQ-NET recommends shared `--online` with an optional explicit `--pull-requests` alias; OQ-FLAGS recommends explicit `--human` exclusive with `--json` and a timeout that only shortens LIM-1. These are recommendations, not selected flags. Owner decisions precede implementation of the affected CLI/remote surface; the current table/examples describe only the draft, with no approved network exception inferred from older text.

## Exit status

The statuses, diagnostics and exit codes are normative in [spec.md](../spec.md) (Status and exit table). Summary: 0 for `ok` (including results full of findings), 1 for `incomplete` and `error` (a concise `Error:` line on standard error and, with `--json`, the full observation on standard output), 2 for usage errors with no JSON. Findings never change the exit status; `plan` exits 0 when it prints steps or a `blocked` reason.

012 uses 0/1/2 as designed. The implemented hidden Spec 015 `restack plan` v1 uses exit 3 for incomplete; this contract does not change it. Future shared normalization requires the owner migration decision OQ-EXIT, not a silent interpretation of 3 as a usage error.

## Human output

Rendered from the same result, bounded, with every control character escaped. Only the fixed lines and the finding summaries of the data model (normative) appear:

```text
Stack: base main (adcdcf6) .. tip s3 (88da0eb), 3 local layers, candidate open depth <=2, backend git
Layers (bottom first):
  1. s1  5758b57  1 commit   landed: applied
  2. s2  eceac4e  1 commit   landed: pending
  3. s3  88da0eb  1 commit   landed: pending
Findings:
  [action] [observed] layer-applied-in-base (layer 1): All 1 commits of layer 1 are already in the base as equivalent patches. (git-cherry)
  [notice] [inferred] restack-needed: Layers 2 retain old parent commits; local restack advice is conditional on native status: unchecked. (git-cherry, git-range)
  [notice] [observed] stack-behind-base: The stack is based 1 commit(s) behind the base. (git-range)
Plan (advisory; nothing was executed, and a plan authorizes nothing; native stack status not checked):
  s1  git rebase --onto main 5758b572620f6940ef3b8292408c97f91ece4f8b s2
  s2  git rebase --onto s2 eceac4e6d2eb9d8cb7fda6e8425ee7bdc7aeadfa s3   (after s1)
Complete: yes. Network reads: none.
Notice: Sampled advisory evidence; stw ran none of the printed commands and a plan authorizes nothing.
```

Remote evidence adds, per layer, `PR #N (open, stack 10 position 2 of 3, verified)` or `(name-matched)`, a `Native stacks:` line, and its sources line (`pull-requests-by-head ok; native-stack ok`), in the shape of the Spec 025 source summary. Findings in the human list are ordered `action`, `notice`, `info`, and within a severity by the Findings table order. An `unknown` finding prints its summary with the appended reason. With no `action` or `notice` finding the text says only that none was found in the evidence read, never that the stack is correct. A `link` step prints `gh stack link --base <validated base.trunk_ref> <PR numbers bottom first>`, records that trunk in `expected`, and prints the environment hint and the label `performed by gh stack, not by stw`; a `fetch` step prints `git fetch REMOTE` or `jj git fetch --remote REMOTE`; an `observe` step prints, by note: `run-again` "run `stw stack plan ... --after-fetch` after the fetch (the flag makes a still-missing object terminate)"; `object-unreachable` "layer N: if you have fetched (stw cannot verify), an object is still missing; it is not reachable from a fetched branch (deleted branch, fork head or unmerged merge commit) and the layer is not treated as landed"; `stale-base` "--base does not contain the merge commit of a merged pull request; update or fetch --base and run again"; `take-upstream` "GitHub rewrote this branch; take GitHub's head for layer N, saving local-only commits first"; `native-rewrite-pending` "GitHub's head for layer N equals or is behind the local tip: its rewrite has not happened yet or GitHub could not do it (for example a conflict); check the stack on GitHub; stw claims no difference"; `upstream-ahead` "the upstream of layer N has commits it lacks; integrate them before restacking"; `landed-undetectable` "layer N contains the head commit of a merged native stack member whose branch or bookmark is gone, so the landed commit cannot be told apart from your layers and GitHub may still rewrite the remaining heads; check the pull requests; stw prints no rebase"; `diverged-undecided` "layer N and its upstream diverged: M upstream-only and K local-only commits (merge commits counted); stw gives no remedy". An `observe` step prints its layer number when it has one.

## JSON

One `StackResult` per invocation, validated by `schemas/v1/stack.schema.json`, shape in [data-model.md](../data-model.md). The observation above (Git, rebase-merged bottom layer, mode `plan`, no remote evidence), as JSON:

```json
{
 "schema_version": 1,
 "kind": "stack",
 "mode": "plan",
 "after_fetch": false,
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
   "code": "restack-needed",
   "layer": null,
   "subject": null,
   "severity": "notice",
   "confidence": "inferred",
   "reason": null,
   "summary": "Layers 2 retain old parent commits; local restack advice is conditional on native status: unchecked.",
   "detail": {
    "layers": [
     2
    ],
    "native_status": "unchecked"
   },
   "evidence": [
    {
     "source": "git-cherry",
     "fields": [
      "marks"
     ]
    },
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
 "observed_at": "2026-10-17T12:00:00Z",
 "depth": 3,
 "open_depth": 2
}
```

## Effects

- Without `--pull-requests`: local cached objects/remote-tracking refs only, with sampled freshness and no persistent HTTP cache; local reads only, by the isolated Git runner (explicit `--git-dir`/`--work-tree` after the ownership check, the isolation prefix and process allowlist of [plan.md](../plan.md)) or the jj session pinned to one operation (`--ignore-working-copy`). No network, no authentication, no fetch.
- With `--pull-requests`: additionally the fixed `GET` requests of the Remote sources table with the caller's selected-host credentials (Spec 022 token lookup), at most 50, none that writes.
- Never: a write to any ref, file, index, object database (loose objects included), remote, pull request, stack or setting; a jj snapshot or operation-log change; a hook, filter, textconv, external diff, alias, editor, pager or prompt; a started printed command.
- Reading a pull request or stack can cause GitHub to compute server-side state; that is GitHub's cache, not a repository change.

## Compatibility

Every existing command, flag, schema and fixture is unchanged and byte-identical (a test compares them to the base). `stack.schema.json` is a proposed new self-contained schema; publication waits for implementation and conformance. The first-draft contract for diff and changed-file output is not provided (withdrawn; see the Disposition table in [spec.md](../spec.md)).

## Toolbelt handoff and advisory findings

`depth` counts retained local layers, including proven-landed ones. Complete `open_depth` is the candidate upper bound after excluding only proven-landed layers under the landing predicates and stale-base precedence; unknown landing and closed-unmerged layers remain candidates, so this never asserts an exact open-PR count. Incomplete/error results have null `open_depth` and no depth notice. `stack-too-deep` is an inferred notice only when complete candidate open_depth >2; it neither fails completion nor blocks a plan. The example has depth 3 but one proven-applied parent, so open_depth 2 and no notice. All local layers up to 32 remain visible; the 33rd independently exhausts LIM-2.

`restack-needed` uses the exact Findings rule and is conditional when native status is unchecked. Rebase-merge SHA inequality and branch-name matches alone are never landing proof. `git cherry`/patch ids qualify linear patch content; merge commits, squash mismatches and conflict resolutions limit that evidence. A parent that is closed but not merged is not landed; its child's stale base remains only `base-mismatch`. No observation authorizes a retarget, rewrite or publication.

The displayed `git rebase --onto <new parent> <full old parent> <child>` is an inert human reference for the exact replay boundary. Spec 015 owner D7 requires plumbing/quarantine and compare-and-swap; its executor never runs this argv. The field-to-flag table in [data-model.md](../data-model.md#field-to-flag-mapping-for-the-implemented-spec-015-git-v1-plan) maps selected branches to validated `refs/heads/...`, old/new parents and expected child head to full ids, and never derives `--identity` from 012. Later null destinations require the actual predecessor apply outcome; the current 015 v1 accepts only native Git SHA-1, not 012’s jj or 64-hex Git observations. Diagnostics and exit mappings remain per-command namespaces. Actual Spec 015 apply must produce its own actual-head evidence and pass independent exact-head review before a separate Spec 016 lease-push plan/apply, with explicit action authorization and plan id. Neither a predicted head nor `Layer.upstream.commit_id` is a fresh publishable head/lease receipt.

The owner performs Spec 043 landing separately. 012 never starts 015/016/043, fetches, prompts, or resyncs an owner's checkout. OQ-NET keeps online scope and flag naming open; the draft `--pull-requests` mode was a recommendation, never a recorded owner approval.

## Explicit caller orchestration over repositories

The commands above still accept one PATH. A higher-level caller can supply 1..16 explicit entries, each containing its own path, base, tip and optional hosting-read selection. The list and at-most-four-worker bound reuse [Spec 023](../../023-multi-repo-inventory/contracts/repos.md); they do not enable its submodule discovery, add a flag, or change any single-PATH cap. Spec 040 `internal/fanout` runs entries at the edge and retains caller evidence by input index even when Git and jj finish in reverse order.

Each started entry independently verifies native ownership, resolves its own base/tip and optional hosting context, resamples its own references/operation/PRs and preserves its ordinary `StackResult` status, native identities, findings and sample time. A refusal in one repository cannot hide another result, bypass jj ownership or reuse another repository’s credentials/online opt-in. There is no cross-repository atomic snapshot or merged stack topology. An offline entry causes no authentication or request.

The orchestrator shares one 30-second deadline, 50-request counter and 1 MiB output budget across all entries; worker count cannot multiply those allowances. Each native process retains the existing five-second bound and each member retains the other single-repository limits. The caller validates the selection before starting workers, records explicit incomplete/canceled/not-started outcomes for entries that cannot finish, and retains a bounded indexed limit outcome when a full member result cannot fit. It never silently drops an entry or fabricates a complete `StackResult` for a member that was not observed. These completion/error wrappers are caller evidence only, not a published aggregate JSON contract or a new `StackResult` enum. Any future CLI/aggregate-schema surface requires separate owner review; the recommendation is to reuse an explicit higher-level caller.

## Explicit native-link trunk

A link advisory always passes `--base` with the known, Argument-rules-valid `base.trunk_ref`, even when it differs from the selected repository’s default branch. It never forwards a remote-qualified `--base` selector such as `origin/release` instead of the trunk `release`. The step records `{numbers, trunk_ref}` in `expected` and `GH_REPO` in its environment. Unknown trunk blocks at layer 1 with `trunk-ref-unknown`; an unsafe trunk blocks with `unsafe-name`, with no link argv/display and no fallback to GitHub’s default. The local/native result is retained; this advisory block alone does not fail a complete observation. The link remains print-only and authorizes no write.
