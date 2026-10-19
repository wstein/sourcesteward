# Integration proof contract

Subordinate to the [specification](../spec.md) FR-001..FR-012. `U` base OID, `B` tip OID, `M` the single merge base; all OIDs come from an earlier native read.

## Fixed native operations

| Class | Native operations (fixed typed argv) | Result rule |
| --- | --- | --- |
| `ancestor` | `merge-base --is-ancestor B U` | exit 0 proves |
| `no-net-change` | `merge-base --all U B` (exactly one); `rev-parse B^{tree} M^{tree}` | equal trees: deletable `no-net-change` |
| `patch-id` | `rev-list --count --no-merges M..B`, `M..U`, `rev-list --count --merges M..B`; `log --no-merges --format='%H %T %P' M..B` (empty commits); `log --no-merges -p --binary --no-ext-diff --no-textconv --format=%H` for each side, piped to `patch-id --stable` and `patch-id --verbatim`; `diff-tree --stdin --no-commit-id --name-only -r -z` (touched paths); `check-attr --source=B^{tree} -z --stdin diff`; `diff-tree --stdin --numstat -r -z --no-renames` (binary paths, FR-003b) | all non-empty branch commits matched by default patch-id with multiset counts, no reason: proven; verbatim mismatch on a non-binary path: `whitespace-differs` |
| `merge-noop` | `merge-base --all U B` (exactly one); `rev-parse B^{tree} M^{tree} U^{tree}`; `diff-tree -r --raw --no-renames M B` and `M U` (gitlink check); attribute scan (below); `merge-tree --write-tree --no-messages U B` with `--attr-source=U^{tree}` | `merge-tree` exit 0, tree equals `U^{tree}`, `B^{tree} != M^{tree}`, no gitlink difference, no reason: proven |

Every command runs under the planning environment and argument list of the [configuration policy](configuration-policy.md), with `git --attr-source=<tree>` pinned as stated, and with the private object directory below. Git semantics: `patch-id` is git's default stable patch-id (the semantics of `git cherry`, whitespace-insensitive); `--verbatim` preserves whitespace; both hash a binary patch (`--binary`) by content. Measured on Git 2.47.3 and 2.56.0: two different binary changes to one path give different patch-ids, and the same binary change gives `-` in `git cherry`; so a binary change never matches on path alone.

## File types and modes

Modes come from `git --attr-source=<tip tree> diff-tree -r --raw --no-renames` (100644 regular, 100755 executable, 120000 symlink, 160000 gitlink, 040000 tree). Measured on Git 2.47.3 and 2.56.0: `git patch-id` (default and verbatim) and `git cherry` distinguish a mode-only change (its patch-id is non-empty and differs from a content change and from no change), give equal ids for equal mode flips, differ between the flip directions and files, and differ between symlink retargets (`a` to `b` versus `a` to `c`) and between a symlink-to-file type change and a retarget. No extra match rule is therefore needed; the tests assert it (executable bit flip, symlink retarget, gitlink bump) and that a mode-only change is never silently treated as integrated. If a later Git version stops distinguishing them, the fallback is to compare the `diff-tree --raw` mode column of the matched commits.

## Review reasons

The single definition of every reason; spec, report, data model and tests use these names only. "Clearable" means a reviewer decision may clear it; hard stops are not clearable.

| Reason | Raised when | Applies to | Clearable |
| --- | --- | --- | --- |
| `whitespace-differs` | patch-id matches but no matched base commit has an equal verbatim patch-id, and no touched path is binary | `patch-id` | yes; or `--accept-whitespace-differences` |
| `partial-match` | some, not all, non-empty branch commits have a base match | `patch-id` | yes |
| `multiset-mismatch` | for some patch-id the base-side count is lower than the branch-side count | `patch-id` | yes |
| `merge-commits-in-range` | any merge commit in `M..B` | `patch-id` | yes |
| `ambiguous-merge-base` | `merge-base --all` returns more than one | `patch-id`, `merge-noop`, `no-net-change` | yes |
| `gitlink-change` | a mode-160000 path differs in `M..B` or `M..U`, or in the unintegrated change of the report | `merge-noop`, report | yes |
| `would-add-content` | `merge-tree` is clean with a tree different from `U^{tree}` | `merge-noop` | yes |
| `conflict` | `merge-tree` exits 1 | `merge-noop` | yes |
| `attribute-source` | `$GIT_DIR/info/attributes` (common git directory) is a symlink, a special file, over 1 MiB or has any non-blank, non-comment line | `patch-id`, `merge-noop` | yes |
| `merge-attribute` | any `.gitattributes` in the `U`, `B` or `M` trees defines or uses `merge` or the `binary` macro (conservative token scan, at most 64 files of 1 MiB) | `merge-noop` | yes |
| `diff-attribute` | `check-attr diff` on a path touched by a branch commit is anything but `unspecified` | `patch-id` | yes |
| `merge-driver` | `merge.*.driver` or executable `filter.*.clean|smudge|process` configured | `merge-noop` | yes |
| `proof-limit` | a command bound or count bound of the table below is exceeded | the class affected | yes |
| `proof-budget-exhausted` | the per-tip or phase budget is exhausted | remaining tips | yes |
| `object-dir-unavailable` | the private object directory cannot be created | all | yes |
| `history-override` | `info/grafts` or `shallow` exists | all | no (hard stop) |
| `unsupported-metadata` | `objects/info/alternates` exists | all | no (hard stop) |

A ref with no proven class and one or more clearable reasons is `needs-review`; with a hard stop or protection it is `retained`. A ref that is not proven and has no other reason is `needs-review` with the reasons of the failed classes (`would-add-content`, `conflict` or `partial-match`).

## Command bounds

Stdout is read by the bounded stream executor, one grammar per command; OID is `[0-9a-f]{40}` or `[0-9a-f]{64}` (SHA-256), so a line is at most 65 bytes with newline. A line outside its grammar, a bound exceeded, an unexpected exit, timeout or cancellation gives `proof-limit` (or `object-dir-unavailable`) for the affected class, never a partial proof. stderr is at most 65,536 bytes for all. Each row has a test at the bound (accepted) and one over (refused), for SHA-1 and SHA-256 widths.

| Command | Max records | Max bytes | Line grammar |
| --- | --- | --- | --- |
| `merge-base --is-ancestor` | 0 | 0 | none; exit 0 or 1 |
| `merge-base --all U B` | 8 (more than 1 is `ambiguous-merge-base`) | 520 | `OID` |
| `rev-parse` trees | 3 | 195 | `OID` |
| `rev-list --count` | 1 | 11 | `[0-9]{1,7}` |
| `log --no-merges --format='%H %T %P'` | 1,024 | 201,728 | `OID OID OID` |
| `log --no-merges -p --binary` stream | opaque | 64 MiB per tip | none; piped to `patch-id`, never buffered |
| `patch-id` (default and verbatim) | 5,120 | 537,600 (5,120 x 105) | `[0-9a-f]{40} OID` |
| `diff-tree -r --raw --no-renames --no-abbrev` (gitlink check) | 8,192 | 34,603,008 (8,192 x 4,224) | `:[0-7]{6} [0-7]{6} OID OID [A-Z]` TAB `PATH`, PATH at most 4,096 bytes |
| `diff-tree --stdin --name-only -r -z` | 8,192 | 34,603,008 | PATH NUL, at most 4,096 bytes |
| `diff-tree --stdin --numstat -r -z --no-renames` | 8,192 | 34,603,008 | `(\d+\|-)` TAB `(\d+\|-)` TAB PATH NUL |
| `check-attr -z --stdin diff` | 8,192 | 34,603,008 | PATH NUL `diff` NUL VALUE NUL |
| `ls-tree -r -z` (attribute files) | 1,024 (Spec 029 leaf bound) | 4,325,376 | `MODE TYPE OID` TAB `PATH`, NUL terminated |
| `cat-file --batch` (attribute files) | 64 files | 1 MiB each | batch header then content |
| `merge-tree --write-tree --no-messages` | first line `OID`; a conflict listing counts only against the byte bound | 4,325,376 | first line `OID` |

Report commands have their own table in the [review report contract](review-report.md#command-bounds).

## Private object directory

`GIT_OBJECT_DIRECTORY=<fresh mode-0700 dir>`, `GIT_ALTERNATE_OBJECT_DIRECTORIES=<repository objects directory only>`, removed on every exit path. Git ignores the repository's own `objects/info/alternates` when `GIT_OBJECT_DIRECTORY` is set, so a repository with an alternates file is the hard stop `unsupported-metadata`. The repository `objects/`, refs and `packed-refs` are compared byte-wise before and after in tests.

## History integrity

`info/grafts` or a `shallow` file: every proof is the hard stop `history-override`. Replace refs: ignored by `--no-replace-objects`, plan note `replace-refs-ignored`.

## What each class does not claim

`merge-noop`: not review, not a PR, not authorship. `patch-id`: not that the change is still present; `whitespace-differs` says the formatting differs and needs a look. `no-net-change`: not integration; only that nothing is unmerged. `ancestor`: not review. None says anything about CI or other branches. Each local deletion keeps the recovery ref and journal entry described in the spec's design rationale (tip commit and history only; reflog and `branch.<name>.*` configuration are not recorded; restore is manual; local to this clone).
