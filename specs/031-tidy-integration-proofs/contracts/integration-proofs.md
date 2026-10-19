# Integration proof contract

Subordinate to the [specification](../spec.md) FR-001..FR-012.

## Fixed native operations

`U` base OID, `B` tip OID, `M` the single merge base. All OIDs come from an earlier native read.

| Kind | Native operations (fixed typed argv) | Result rule |
| --- | --- | --- |
| `ancestor` | `merge-base --is-ancestor B U` | exit 0 proves |
| `merge-noop` | `merge-base --all U B` (exactly one); `rev-parse B^{tree} M^{tree} U^{tree}`; `diff-tree -r --raw --no-renames M B` and `M U` (gitlink check); `merge-tree --write-tree --no-messages U B` | see below |
| `patch-id` (evidence) | `rev-list --count --no-merges M..B`, `M..U`, `rev-list --count --merges M..B`; `log --no-merges --raw -r --no-renames --format=%H M..B` (empty commits); `cherry U B` | counts only, never proves |
| `patch-exact` (opt-in proof) | branch and base commit lists from `rev-list --no-merges`; `diff-tree --stdin -p --binary --full-index --no-ext-diff --no-textconv --no-renames --no-color -r --root` piped to `patch-id --verbatim` for each side | every non-empty branch-side verbatim patch-id present on base side, at least one |

`merge-noop` proves only when: `B^{tree} != M^{tree}`, no gitlink (mode 160000) path differs in `M..B` or `M..U`, one merge base, and `merge-tree` exits 0 printing a tree equal to `U^{tree}`. `B^{tree} == M^{tree}` is `no-net-change` (a branch that added and reverted work is clean-merging but never integrated). Exit 1 is `conflict`; a different clean tree is `would-add-content`; a gitlink difference is `not-evaluated: gitlink-change`.

`git cherry` patch-ids ignore whitespace (reproduced on Git 2.56: an indented call inside a block matched a base commit that does not indent it) and mark empty commits `-`; therefore `cherry` output is evidence and a candidate filter only. `patch-id --verbatim` keeps whitespace and ignores only hunk line numbers, which a rebase changes.

All commands use the planning environment and argument list of the [configuration policy](configuration-policy.md) and the private object directory below.

## Output and exit handling

Stdout is read by the bounded stream executor. Exact bounds: `cherry` at most 1,024 lines of at most 67 bytes (2 + 64 hex + newline for SHA-256), 68,608 bytes; other commands at most 4,224 bytes, except the patch stream (at most 64 MiB per tip, consumed incrementally, never buffered); stderr at most 65,536 bytes. Lines not matching `^[+-] [0-9a-f]{40,64}$`, nonzero exits other than `merge-tree` exit 1, overflow, timeout and cancellation make the kind `not-evaluated`, never partial proof.

## Private object directory

`GIT_OBJECT_DIRECTORY=<fresh mode-0700 dir>`, `GIT_ALTERNATE_OBJECT_DIRECTORIES=<repository objects directory only>`, removed on every exit path. Git ignores the repository's own `objects/info/alternates` when `GIT_OBJECT_DIRECTORY` is set, so a repository with an alternates file is `not-evaluated: unsupported-metadata`. The repository `objects/`, refs and `packed-refs` are compared byte-wise before and after in tests.

## History integrity

`info/grafts`, a `shallow` file: every proof `not-evaluated: history-override`. Replace refs: ignored by `--no-replace-objects`, plan note `replace-refs-ignored`.

## Refusal and retention rules

1. Protected refs (base, symbolic, workspace-used) and uncertain targets: evidence recorded, disposition retained.
2. `merge-noop`: `merge.*.driver` or executable `filter.*` present: `not-evaluated: git-unsafe-configuration`.
3. Patch kinds: count bounds exceeded `proof-limit`.
4. Aggregate 60 s forge-free deadline exceeded: remaining tips `proof-budget-exhausted`.
5. Everything else not `proven` is retained `integration-not-proven` with classification. No branch is deleted by similarity, cherry-pick replay, whitespace-insensitive patch-id or a user-named range.

## What each kind does not claim

`merge-noop`: not review, not a PR, not authorship. `patch-exact`: not that the change is still present. `patch-id`: nothing beyond evidence. `ancestor`: not review. None says anything about CI or other branches.
