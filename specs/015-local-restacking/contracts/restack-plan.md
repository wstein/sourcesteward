# Contract: `stw restack plan`

Status: proposed first-slice contract, subordinate to the [specification](../spec.md). It is published as `schemas/v1/restack-plan.schema.json` only with accepted implementation; no existing schema, fixture or command changes.

## Command

```text
stw restack plan [PATH] --branch REF --old-parent REF|OID --new-parent REF|OID
                 [--expect-head OID] [--remote NAME] [--identity "NAME <EMAIL>" ...]
                 [--keep-both-config ABS] [--json]
```

- `PATH` defaults to the current directory; it must be a canonical path of a native Git checkout owned by Git (no jj, GitButler or ambiguous ownership: `unsupported-backend`, `ambiguous-ownership`).
- `--branch` must be a fully qualified `refs/heads/...` ref. `--old-parent` and `--new-parent` are a 40-hex lowercase commit id or a fully qualified ref that peels to exactly one commit. Short names, `HEAD`, abbreviations, revision expressions (`^`, `~`, `..`, `@{}`) and duplicate flags are syntax errors (exit 2).
- `--expect-head OID` refuses (`head-changed`) when the observed branch tip differs.
- `--remote NAME` (default `origin`) only names the remote in the printed texts; it is never contacted and never resolved against the network.
- `--identity` is repeatable; each value is `NAME <EMAIL>`. With none, the identities of the process environment (`GIT_AUTHOR_NAME`/`GIT_AUTHOR_EMAIL`/`GIT_COMMITTER_NAME`/`GIT_COMMITTER_EMAIL`) are used when set. stw never reads `user.*` configuration for this purpose and never writes configuration.
- `--keep-both-config ABS` names a stw configuration file whose `[restack] keep_both_paths` is an explicit list of exact repository paths or directory prefixes (default: empty, no file needed). It is read as data; repository-controlled files are never read as configuration.
- `--json` emits exactly one `RestackPlanResult` object on standard output, including `incomplete` and `error` results; human output is rendered from the same result.

## Effects (disclosed, enforced)

Allowed: fixed read-only Git plumbing through the reviewed isolated runner (`rev-parse`, `cat-file`, `rev-list`, `merge-base`, `diff-tree`, `patch-id`, `for-each-ref`, `worktree list --porcelain`, `merge-tree --write-tree`, `commit-tree`, `range-diff`, `cherry`), with no hooks, helpers, pager, editor, terminal prompt or network; temporary objects only in a private quarantine directory under the stw state area, removed on every exit path. Refused: any ref, index, worktree, configuration, hook, `.git` or object-store change; fetch, push, `rebase`, `reset`, `checkout`, `gc`.

## Exit status

| `status` | Meaning | Exit |
| --- | --- | --- |
| `ok` | complete evidence, whatever it shows (clean, differing, predicted conflicts, unknown identity check) | 0 |
| `incomplete` | a bound, cancellation or a timeout stopped evidence collection (`bound-exceeded`, `canceled`, `timeout`) | 1 |
| `error` | a refusal or a failure (diagnostics below); no `evidence` | 1 |
| (syntax) | malformed flags or refs | 2 |

Success never means "safe to push" (FR-016). Standard error carries one concise `Error:` line for exit 1.

## Refusals and diagnostics (stable codes)

`unsupported-backend`, `ambiguous-ownership`, `path-not-canonical`, `symlink-hazard`, `repository-hazard` (`core.hooksPath`, `core.fsmonitor`, `include`/`includeIf`, `extensions.*`, `replace` refs or grafts, shallow history, `merge.*.driver`, filter drivers or non-default `.gitattributes` merge/diff attributes on any touched path, reported with `hazard` names, none executed), `target-dirty`, `operation-in-progress` (rebase, merge, cherry-pick, bisect, sequencer state), `branch-checked-out-elsewhere` (reported with the worktree path, never touched), `ref-not-found`, `ref-ambiguous`, `ref-not-commit`, `object-missing`, `head-changed`, `unrelated-histories`, `merge-commit-in-range`, `range-too-large` (bound), `bound-exceeded`, `canceled`, `timeout`, `git-unsupported` (below the minimum Git version of the existing runner).

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
| `limitations` | string[] | always includes `semantic-breaks-not-detected` |
| `diagnostics` | Diagnostic[] | empty for `ok` |
| `boundary` | Boundary | bounds applied and `truncated: false` or the reached bound |
| `observed_at` | RFC 3339 UTC | time of the final sample |

## Stability

The schema is versioned by `schema_version`. Within version 1 only additive changes are allowed: new optional fields, new enum values for `diagnostics[].code` and `evidence.*.state` where consumers must treat unknown values as `unknown`, and new `limitations` strings. Removing or renaming a field, changing a type or a meaning, or changing an exit code requires `schema_version: 2` as an explicit opt-in. Arrays are non-null; absent optional objects are `null`; all strings are valid UTF-8, bounded, and data.

## Printed command texts

`texts.lease_push` is a single line with the observed old tip and a placeholder for the new tip, because the new commit ids exist only after a replay (by hand or by a later `restack apply`):

```text
git push --force-with-lease=refs/heads/<branch>:<OBSERVED_OLD_FULL_SHA> <remote> <NEW_FULL_SHA>:refs/heads/<branch>
```

`new_sha_known` is `false` in this slice and the placeholder `<NEW_FULL_SHA>` is printed literally. The text never contains `--force` without `-with-lease=ref:sha`, never a bare `--force-with-lease`, and is not executed. `texts.resync` holds the two owner alternatives, the second named destructive:

```text
git fetch --prune
git rebase --onto origin/<branch> <OBSERVED_OLD_FULL_SHA> <branch>        # the owner has local commits on the branch
git switch <branch> && git reset --hard origin/<branch>                   # DESTRUCTIVE for local-only work and uncommitted changes
```

Branch, remote and ref text is validated against Git ref-name rules and shell-quoted when it contains anything outside `[A-Za-z0-9._/-]`; texts with an unquotable value are omitted with a diagnostic rather than emitted unsafely.

## Human output

```text
Restack plan: refs/heads/feat/x onto 9f3c... (old parent 41ab...), 3 commits
Replay (plumbing, nothing written): clean for 3 of 3 commits
Range pairing: = = =    Parent commits dropped: 2 of 2 (cherry -)   Own diffs equal: 3 of 3
Identity: pass (3 authors, 3 committers match 1 expected)
Overlap risk: 2 paths changed by the new parent and by the branch (src/a.go, src/b.go)
Limitation: semantic breaks are not detected; build and test the result.
Lease push (not run): git push --force-with-lease=refs/heads/feat/x:<old sha> origin <NEW_FULL_SHA>:refs/heads/feat/x
```
