# Contract: `stw restack plan`

Status: first-slice contract implemented in source on 2026-10-21, hidden and UNPUBLISHED, subordinate to the [specification](../spec.md). `schemas/v1/restack-plan.schema.json` exists and is frozen by digest; publication awaits acceptance (T021/T023). The 2026-10-25 Markdown amendment changes no schema, fixture or command. Proposed local apply is a separate [future contract](restack-apply.md), never authority derived from this observation.

## Command

```text
stw restack plan [PATH] --branch REF --old-parent REF|OID --new-parent REF|OID
                 [--expect-head OID] [--remote NAME] [--identity "NAME <EMAIL>" ...]
                 [--json]
```

- `PATH` defaults to the current directory; it must be a canonical path of a native Git checkout owned by Git (no jj, GitButler or ambiguous ownership: `unsupported-backend`, `ambiguous-ownership`).
- `--branch` must be a fully qualified `refs/heads/...` ref. `--old-parent` and `--new-parent` are a 40-hex lowercase commit id or a fully qualified ref that peels to exactly one commit. Short names, `HEAD`, abbreviations, revision expressions (`^`, `~`, `..`, `@{}`) and duplicate flags are syntax errors (exit 2).
- `--expect-head OID` refuses (`head-changed`) when the observed branch tip differs.
- `--remote NAME` (default `origin`) only names the remote in the printed texts; it is never contacted and never resolved against the network.
- `--identity` is repeatable; each value is `NAME <EMAIL>` and is the only source of expected identities (reported as `source: "flag"`). The environment and configuration never supply them: a placeholder identity that leaked into the environment (for example `t <t@t>`) must not count as expected. Without any `--identity` the identity check is `unknown`, never `pass`. stw never writes configuration.
- There is no configuration input in this slice; the keep-both allowlist and its flag belong to a later classification slice (WP6).
- `--json` emits exactly one `RestackPlanResult` object on standard output, including `incomplete` and `error` results; human output is rendered from the same result.

## Effects (disclosed, enforced)

Mtime caveat: `objects: "quarantine-only"` and the `writes` disclosure describe content, refs, index, worktree and configuration. When `merge-tree` or `commit-tree` writes an object that already exists in the repository, Git may freshen the mtime of that existing object file in the user's `.git/objects` (content byte-identical); the plan does not claim that object mtimes stay unchanged.

Allowed: fixed read-only Git plumbing through the reviewed isolated runner (`rev-parse`, `cat-file`, `rev-list`, `merge-base`, `merge-base --is-ancestor`, `diff-tree`, `patch-id`, `check-attr` with the attribute source pinned to the base tree as Spec 031 does (`--source`/`GIT_ATTR_SOURCE`, never the worktree), `status --porcelain=v2 --untracked-files=no --no-renames` for the dirty-target check, `for-each-ref`, `worktree list --porcelain`, `merge-tree --write-tree`, `commit-tree`, `range-diff`, `cherry`), each with the hardened environment of the tidy runner (`GIT_CONFIG_NOSYSTEM=1`, `HOME` and global configuration neutralized, `gc.auto=0`, `core.hooksPath=/dev/null`, `GIT_OPTIONAL_LOCKS=0`, `GIT_TERMINAL_PROMPT=0`, no pager, no external diff or textconv) and with no hooks, helpers, pager, editor, terminal prompt or network; temporary objects only in a private quarantine directory (`GIT_OBJECT_DIRECTORY`, with the repository objects as `GIT_ALTERNATE_OBJECT_DIRECTORIES`; replay commits keep the original author and carry the fixed committer identity and date of the [plan](../plan.md), so no `user.*` is read) under the stw state area, removed on every exit path. Refused: any ref, index, worktree, configuration, hook, `.git` or object-store change; fetch, push, `rebase`, `reset`, `checkout`, `gc`.

## Exit status

| `status` | Meaning | Exit |
| --- | --- | --- |
| `ok` | complete evidence, whatever it shows (clean, differing, predicted conflicts, unknown identity check) | 0 |
| `error` | a refusal or a failure (diagnostics below); `evidence` is `null` | 1 |
| (syntax) | malformed flags or refs | 2 |
| `incomplete` | a bound, cancellation or a timeout stopped evidence collection (`range-too-large`, `bound-exceeded`, `canceled`, `timeout`); `evidence` is `null` and `boundary.reached` and the diagnostic name what stopped | 3 |

Success never means "safe to push" (FR-016). Standard error carries one concise `Error:` line for exit 1.

## Refusals and diagnostics (stable codes)

`unsupported-backend`, `ambiguous-ownership`, `path-not-canonical`, `symlink-hazard`, `repository-hazard` (`core.hooksPath`, `core.fsmonitor`, `include`/`includeIf`, `extensions.*`, `replace` refs or grafts, shallow history, `merge.*.driver`, filter drivers or non-default `.gitattributes` merge/diff attributes on any touched path, reported with `hazard` names, none executed), `target-dirty`, `operation-in-progress` (rebase, merge, cherry-pick, bisect, sequencer state), `branch-checked-out-elsewhere` (reported with the worktree path, never touched), `ref-not-found`, `ref-ambiguous`, `ref-not-commit`, `object-missing`, `head-changed`, `empty-moved-range` (`old_head` equals `old_parent`: nothing to restack; evidence over an empty set is never a pass, exit 1), `old-parent-not-ancestor` (`merge-base --is-ancestor <old parent> <old head>` failed: the parent's own commits would be moved), `unrelated-histories` (no merge base between old parent and new parent), `merge-commit-in-range`, `git-unsupported` (below the minimum Git version of the existing runner); the bound and interruption codes `range-too-large`, `bound-exceeded`, `canceled` and `timeout` are `incomplete` results (exit 3), not refusals.

Codes added by the implementation (additive): `not-repository`, `inspection-failed`, `bare-repository`, `object-format-unsupported` (only SHA-1 repositories), `plan-environment-unavailable`, `replay-failed`, `worktree-list-failed`, `git-failed` (a read-only command failed; the diagnostic carries no repository text) and, as a non-refusing diagnostic, `text-omitted` (a branch or remote name has characters outside `[A-Za-z0-9._/@+=,%-]`, so the command texts are not printed). `repository-hazard` details list `hazards` by name: `core.hooksPath`, `core.fsmonitor`, `core.attributesFile`, `include`, `extensions`, `merge-driver`, `filter-driver`, `diff-driver`, `replace-refs`, `grafts`, `shallow`, `alternates`, `info-attributes`, `attributes-changed` (a moved `.gitattributes`), `attribute-merge`, `attribute-diff`, `attribute-filter` (a non-default attribute on a moved path in the branch tip tree or the new parent tree), `config-unreadable` and `attributes-unreadable`. More than 200 moved commits is `incomplete` with `range-too-large` (`details.bound` `max_commits`); more than 2,000 paths in a list, a command output over its limit or a result over 1 MiB is `incomplete` with `bound-exceeded`; an incomplete result carries `evidence: null` and names what was reached in `boundary.reached`.

Hazard diagnostics that do not refuse (status stays `ok`, listed in `effects.hazards` and `diagnostics`): `local-identity-config` (a repository-local `user.name` or `user.email` exists; it is never read or used).

A refusal is `status: error`, `evidence: null`, with `diagnostics[]` of `{code, message, details}`; messages are fixed text, details are bounded data.

## Result

Normative field tables are in the [data model](../data-model.md). Top-level fields, in order:

| Field | Type | Notes |
| --- | --- | --- |
| `schema_version` | integer | constant 1 |
| `kind` | string | constant `restack-plan` |
| `status` | `ok` \| `incomplete` \| `error` | |
| `backend` | `git` \| `jj` \| `gitbutler` \| `ambiguous` \| `unknown` | |
| `identity` | native identity or `null` | as the other results |
| `input` | Input | the resolved explicit input (full ids and the refs as given) |
| `outcome` | `evidence-clean` \| `needs-attention` \| `conflicts-predicted` \| `none` | `none` for `incomplete` and `error`; names what the evidence shows, authorizes nothing |
| `notice` | string | constant: "Read-only evidence; it does not assert that the restacked branch builds, passes tests or is semantically correct, and it authorizes nothing." |
| `evidence` | Evidence or `null` | the evidence bundle |
| `effects` | Effects | affected refs and worktrees, hook and configuration disclosure |
| `texts` | Texts | inert command texts: lease push and owner resync |
| `limitations` | string[] | always includes `semantic-breaks-not-detected` and `prediction-is-not-apply`; the limitation string `rewrite-committer-not-predicted` (the identity check covers the existing commits; a by-hand rebase takes the new committer from the rebaser's configuration or environment) is reserved and not emitted until task T024 lands |
| `diagnostics` | Diagnostic[] | empty for `ok` |
| `boundary` | Boundary | bounds applied and `truncated: false` or the reached bound |
| `observed_at` | RFC 3339 UTC | time of the final sample |

## Outcome decision table

`outcome` is a pure function of the evidence, first matching row wins; exactly one row matches every input.

| Row | Condition | `outcome` |
| --- | --- | --- |
| 1 | `status` is `incomplete` or `error` | `none` |
| 2 | `replay.state` is `conflict` | `conflicts-predicted` |
| 3 | every one of the following holds (`no other worktree or ref holds the branch` means: `effects.affected_refs` is empty and no worktree has state `holds-branch`; the latter is refused earlier): `replay.state` is `clean`; `moved.count` is at least 1; every `range_pairing` mark is `=`; every `own_diff_equality` row is `equal`; `parent_dropped.all_dropped` is `true` (non-empty set, every row `dropped` or `patch-equivalent-tree`); `empty_after_replay` is empty; `identity.state` is `pass` with `source` `flag`; `overlap.indicator` is `none`; no other worktree or ref holds the branch | `evidence-clean` |
| 4 | otherwise (any other state or value, known or future) | `needs-attention` |

`evidence-clean` is the positive list of row 3 only; any state the table does not name falls to row 4. `evidence-clean` never means safe: the notice and `limitations` still apply. Examples (abridged):

```json
{"schema_version":1,"kind":"restack-plan","status":"ok","outcome":"evidence-clean","evidence":{"parent_dropped":{"all_dropped":true},"range_pairing":{"all_equal":true},"identity":{"state":"pass","source":"flag"},"overlap":{"indicator":"none"}},"limitations":["semantic-breaks-not-detected","prediction-is-not-apply"]}
{"schema_version":1,"kind":"restack-plan","status":"ok","outcome":"needs-attention","evidence":{"identity":{"state":"unknown","source":"none","reason":"no-expected-identity"},"overlap":{"indicator":"some","count":2}}}
{"schema_version":1,"kind":"restack-plan","status":"ok","outcome":"conflicts-predicted","evidence":{"replay":{"state":"conflict","commits_replayed":1},"conflicts":{"predicted":true,"items":[{"path":"src/a.go","kind":"content"}]}}}
{"schema_version":1,"kind":"restack-plan","status":"error","outcome":"none","evidence":null,"diagnostics":[{"code":"old-parent-not-ancestor","message":"The old parent is not an ancestor of the branch tip.","details":{}}]}
{"schema_version":1,"kind":"restack-plan","status":"error","outcome":"none","evidence":null,"diagnostics":[{"code":"empty-moved-range","message":"The branch tip equals the old parent: nothing to restack.","details":{}}]}
{"schema_version":1,"kind":"restack-plan","status":"incomplete","outcome":"none","diagnostics":[{"code":"bound-exceeded","message":"A documented bound was reached.","details":{"bound":"max_commits"}}]}
```

## Stability

The schema is versioned by `schema_version`. Within version 1 only additive changes are allowed: new optional fields, new enum values for `diagnostics[].code` and `evidence.*.state` where consumers must treat unknown values as `unknown`, and new `limitations` strings. Removing or renaming a field, changing a type or a meaning, or changing an exit code requires `schema_version: 2` as an explicit opt-in. Arrays are non-null; absent optional objects are `null`; all strings are valid UTF-8, bounded, and data.

## Printed command texts

The texts are inert and safe to paste into a POSIX shell: every value that stw knows is a single-quoted literal (ref names validated against Git ref-name rules; a value with a single quote or control character is omitted with a diagnostic rather than emitted), and the only unknown, the new commit id, is a shell variable that the owner must set, never an angle-bracket placeholder (which a shell parses as redirections). `texts.lease_push` is two lines: an assignment line with a refusing default, then the push.

```text
NEW_FULL_SHA=   # set to the full 40-hex id of the new branch tip before running the next line
git push --force-with-lease='refs/heads/feat/x:<observed old 40-hex SHA>' 'origin' "${NEW_FULL_SHA:?set to the full new commit id}:refs/heads/feat/x"
```

`new_sha_known` is `false` in this slice. The pushed value is the full id behind the shell variable; a value that is not 40 hex characters is not checked by the text (known limit: the lease still protects the push, and Git rejects an unresolvable id); an unset or empty variable aborts the command before git runs. The text never contains `--force` without `-with-lease=ref:sha`, never a bare `--force-with-lease`, and is not executed. Tests assert that the rendered text, with the variable line removed, contains no unquoted `<` or `>`. `texts.resync` holds the two owner alternatives with the concrete short branch name (`refs/heads/a/b` gives `a/b`), the second named destructive:

```text
git fetch --prune
git rebase --onto 'origin/feat/x' '<observed old 40-hex SHA>' 'feat/x'        # the owner has local commits on the branch
git switch 'feat/x' && git reset --hard 'origin/feat/x'                       # DESTRUCTIVE for local-only work and uncommitted changes
```

(The angle brackets above stand for the concrete value in this document only; the rendered text holds the 40-hex id.)

## Human output

```text
Restack plan: refs/heads/feat/x onto 9f3c... (old parent 41ab...), 3 commits
Replay (plumbing, nothing written): clean for 3 of 3 commits
Range pairing: = = =    Parent commits dropped: 2 of 2 (cherry -)   Own diffs equal: 3 of 3
Identity: pass (source: flag; 3 authors, 3 committers match 1 expected)
Overlap risk: 2 paths changed by the new parent and by the branch (src/a.go, src/b.go)
Limitation: semantic breaks are not detected; build and test the result.
Lease push (not run): git push --force-with-lease='refs/heads/feat/x:<old sha>' origin "${NEW_FULL_SHA:?set to the full new commit id}:refs/heads/feat/x"
```


## 2026-10-25 compatibility and handoff boundary

Implemented v1 remains exactly 0/1/2/3 with `incomplete` exit3 and unchanged
schema bytes. A common future 0/1/2 convention is an OQ-1 migration proposal,
not an additive v1 wording change. The `rewrite-committer-not-predicted`
limitation stays reserved pending T024; documents do not claim it is emitted.

`texts.lease_push.observed_old_sha` is the local observed branch head, not a fresh
remote advertisement. A consumer cannot use it as Spec 016's remote lease proof.
The existing text is inert human output only. A future 016 action must separately
observe the full remote SHA, bind the exact actual new SHA, verify independent
review/check/identity/attribution and require its own plan id plus authorization.
Owner reset/resync text is never executed. This observation never authorizes the
[proposed apply/recovery](restack-apply.md) or any remote action.
