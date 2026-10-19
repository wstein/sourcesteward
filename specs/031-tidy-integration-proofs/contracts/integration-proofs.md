# Integration proof contract

Subordinate to the [specification](../spec.md) FR-001..FR-012.

## Fixed native operations

`U` base OID, `B` tip OID, `M` the single merge base. All OIDs come from an earlier native read.

| Class | Native operations (fixed typed argv) | Result rule |
| --- | --- | --- |
| `ancestor` | `merge-base --is-ancestor B U` | exit 0 proves |
| `no-net-change` | `merge-base --all U B` (exactly one); `rev-parse B^{tree} M^{tree}` | equal trees: deletable `no-net-change`, never labelled integrated |
| `patch-id` / `patch-exact` | `rev-list --count --no-merges M..B`, `M..U`, `rev-list --count --merges M..B`; `log --no-merges --format='%H %T %P' M..B` (empty commits); `cherry U B` (default ids); `diff-tree --stdin -p --binary --full-index --no-ext-diff --no-textconv --no-renames --no-color -r --root` piped to `patch-id --verbatim` for each side; `diff-tree --stdin --no-commit-id --name-only -r -z` for touched paths | every non-empty branch commit has a default match: `patch-id`; also a verbatim match: `patch-exact`; whitespace-only differences on a whitespace-significant path retain |
| `merge-noop` | `merge-base --all U B` (exactly one); `rev-parse B^{tree} M^{tree} U^{tree}`; `diff-tree -r --raw --no-renames M B` and `M U` (gitlink check); `merge-tree --write-tree --no-messages U B` with `--attr-source=U^{tree}`; attribute sources per FR-010a | see below |

`merge-noop` proves only when: no `merge` attribute from any source (`merge-attributes-present` otherwise), `B^{tree} != M^{tree}` (equal trees are `no-net-change`), no gitlink (mode 160000) path differs in `M..B` or `M..U`, one merge base, and `merge-tree` exits 0 printing a tree equal to `U^{tree}`. Exit 1 is `conflict`; a different clean tree is `would-add-content`; a gitlink difference is `not-evaluated: gitlink-change`.

## Whitespace-significance guard

Default significant list (basename globs): `*.py`, `*.yml`, `*.yaml`, `Makefile`, `*.mk`. Extend with the repeatable `--whitespace-significant GLOB` (at most 64 globs of at most 128 bytes, bound into plan identity); there is no option that removes defaults. A commit is whitespace-only different when its default patch-id has a base-side match but its verbatim patch-id has none. If any touched path (from `--name-only -z`) matches the list, the branch is retained with evidence `whitespace-significant-difference` and up to 16 paths. Otherwise the branch is deletable with subclass `integrated-modulo-whitespace`. `git cherry` patch-ids ignore whitespace (reproduced on Git 2.56: an indented call inside a Python block matched a base commit that does not indent it) and show an empty commit as `+` with no counterpart on the base (observed); `patch-id --verbatim` keeps whitespace and ignores only hunk line numbers, which a rebase changes. Line endings, trailing whitespace, indentation style, import ordering and gofmt-style reformatting add no further refusal. Reformatting that changes the default patch-id is unmatched and retained; `merge-noop` still proves it when the base has the final content.

All commands use the planning environment and argument list of the [configuration policy](configuration-policy.md) and the private object directory below.

## Command bounds

Stdout is read by the bounded stream executor, one grammar per command; OID is `[0-9a-f]{40}` or `[0-9a-f]{64}` (SHA-256), so a line is at most 65 bytes with newline. A line outside its grammar, a record or byte bound exceeded, an unexpected exit, timeout or cancellation makes the affected kind `not-evaluated` (`proof-limit` or the failure class); stderr is at most 65,536 bytes for all. Each row has its own test at the bound (accepted) and one over (refused), for SHA-1 and SHA-256 widths.

| Command | Max records | Max bytes | Line grammar |
| --- | --- | --- | --- |
| `merge-base --is-ancestor` | 0 | 0 | none; exit 0 or 1 |
| `merge-base --all U B` | 8 (more than 1 is `ambiguous-merge-base`) | 520 | `OID` |
| `rev-parse` trees | 3 | 195 | `OID` |
| `rev-list --count` | 1 | 11 | `[0-9]{1,7}` |
| `rev-list --no-merges` commit lists | 4,096 base, 1,024 branch | 266,240 / 66,560 | `OID` |
| `log --no-merges --format='%H %T %P'` | 1,024 | 201,728 | `OID OID OID` |
| `diff-tree -r --raw --no-renames --no-abbrev` (gitlink check) | 8,192 | 34,603,008 (8,192 x 4,224) | `:[0-7]{6} [0-7]{6} OID OID [A-Z]` TAB `PATH`, PATH at most 4,096 bytes |
| `ls-tree -r -z` (attribute files) | 1,024 (Spec 029 leaf bound) | 4,325,376 | `MODE TYPE OID` TAB `PATH`, NUL terminated |
| `cat-file --batch` (attribute files) | 64 files | 1 MiB each | batch header then content |
| `merge-tree --write-tree --no-messages` | first line `OID`; a conflict listing counts only against the byte bound | 4,325,376 | first line `OID` |
| `cherry` | 1,024 | 68,608 (1,024 x 67) | `^[+-] OID$` |
| `diff-tree --stdin --no-commit-id --name-only -r -z` | 8,192 | 34,603,008 | PATH NUL, at most 4,096 bytes |
| `diff-tree --stdin -p --binary` patch stream | opaque | 64 MiB per tip | none; piped, never buffered |
| `patch-id --verbatim` | 5,120 | 537,600 (5,120 x 105) | `[0-9a-f]{40} OID` |

## Private object directory

`GIT_OBJECT_DIRECTORY=<fresh mode-0700 dir>`, `GIT_ALTERNATE_OBJECT_DIRECTORIES=<repository objects directory only>`, removed on every exit path. Git ignores the repository's own `objects/info/alternates` when `GIT_OBJECT_DIRECTORY` is set, so a repository with an alternates file is `not-evaluated: unsupported-metadata`. The repository `objects/`, refs and `packed-refs` are compared byte-wise before and after in tests.

## History integrity

`info/grafts`, a `shallow` file: every proof `not-evaluated: history-override`. Replace refs: ignored by `--no-replace-objects`, plan note `replace-refs-ignored`.

## Refusal and retention rules

1. Protected refs (base, symbolic, workspace-used) and uncertain targets: evidence recorded, disposition retained.
2. `merge-noop`: `merge.*.driver` or executable `filter.*`: `not-evaluated: git-unsafe-configuration`; any merge attribute: `not-evaluated: merge-attributes-present`.
3. Patch classes: count bounds exceeded `proof-limit`; whitespace-only difference on a whitespace-significant path: retained `whitespace-significant-difference`.
4. Per-command 10 s, per-tip 20 s or the 60 s forge-free proof phase exceeded: remaining tips `proof-budget-exhausted`.
5. Everything else not proven is retained `integration-not-proven` with classification. No branch is deleted by cherry-pick replay, subject similarity or a user-named range.

## What each class does not claim

`merge-noop`: not review, not a PR, not authorship. `patch-exact`/`patch-id`: not that the change is still present. `integrated-modulo-whitespace`: not that formatting matches. `no-net-change`: not integration; only that nothing is unmerged. `ancestor`: not review. None says anything about CI or other branches. The journal and recovery root make each local deletion reversible.
