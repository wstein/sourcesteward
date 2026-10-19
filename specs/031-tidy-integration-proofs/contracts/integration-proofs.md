# Integration proof contract

Subordinate to the [specification](../spec.md) FR-001..FR-012.

## Fixed native operations

`U` base OID, `B` tip OID, `M` the single merge base. All OIDs come from an earlier native read.

| Class | Native operations (fixed typed argv) | Result rule |
| --- | --- | --- |
| `ancestor` | `merge-base --is-ancestor B U` | exit 0 proves |
| `no-net-change` | `merge-base --all U B` (exactly one); `rev-parse B^{tree} M^{tree}` | equal trees: deletable `no-net-change`, never labelled integrated |
| `patch-id` / `patch-exact` | `rev-list --count --no-merges M..B`, `M..U`, `rev-list --count --merges M..B` (must be 0); `log --no-merges --format='%H %T %P' M..B` (empty commits); `git --attr-source=B^{tree} diff-tree --stdin -p --binary --full-index --no-ext-diff --no-textconv --no-renames --no-color -r --root` for each side, consumed once by `git patch-id --verbatim` and by the stw normalizer (SHA-256); `diff-tree --stdin --no-commit-id --name-only -r -z` (touched paths); `git --attr-source=B^{tree} diff-tree --stdin --numstat -r -z --no-renames` and `git --attr-source=B^{tree} check-attr -z --stdin binary diff text` (binary paths) | multiset match per digest: all by verbatim digest `patch-exact`; all by normalized digest `patch-id`; any merge commit, any unmatched commit or a guarded path retains |
| `merge-noop` | `merge-base --all U B` (exactly one); `rev-parse B^{tree} M^{tree} U^{tree}`; `diff-tree -r --raw --no-renames M B` and `M U` (gitlink check); `merge-tree --write-tree --no-messages U B` with `--attr-source=U^{tree}`; attribute sources per FR-010a | see below |

`merge-noop` proves only when: no `merge` attribute from any source (`merge-attributes-present` otherwise), `B^{tree} != M^{tree}` (equal trees are `no-net-change`), no gitlink (mode 160000) path differs in `M..B` or `M..U`, one merge base, and `merge-tree` exits 0 printing a tree equal to `U^{tree}`. Exit 1 is `conflict`; a different clean tree is `would-add-content`; a gitlink difference is `not-evaluated: gitlink-change`.

## Whitespace-significance guard

Default significant list (basename globs): `*.py`, `*.pyi`, `*.yml`, `*.yaml`, `.clang-format`, `.clang-tidy`, `.yamllint`, `Makefile`, `makefile`, `GNUmakefile`, `*.mk`, `*.make`, `BUILD`, `BUILD.bazel`, `WORKSPACE`, `*.bzl`, `*.hs`, `*.nim`, `*.sass`, `*.pug`, `*.jade`, `*.haml`, `*.coffee`, `*.rst`, `*.tsv`. It is a documented default; extend it with the repeatable `--whitespace-significant GLOB` (at most 64 globs of at most 128 bytes, bound into plan identity); no option removes defaults. A commit matched only by normalized digest (not verbatim) is whitespace-different. If any touched path (from `--name-only -z`) matches the list, the branch is retained with evidence `whitespace-significant-difference` and up to 16 paths. Otherwise the branch is deletable with subclass `integrated-modulo-whitespace`.

Normalization (exact): drop `index` lines and the numbers of `@@` hunk headers; keep file header, mode and rename lines and, per hunk, the ordered `+`, `-` and context-space lines with their path; strip leading whitespace after the marker, trailing whitespace and a trailing CR from each; never touch whitespace between two non-whitespace characters. Context stays in the digest, so the same inserted line at two different positions gives different digests, while indentation-only, trailing-whitespace and CR differences give equal digests and the `rm -rf` token join stays unmatched. `git cherry`'s default patch-id removes all whitespace (reproduced on Git 2.56: an indented call inside a Python block matched a base commit that does not indent it, and `rm -rf "$D" /` matches `rm -rf "$D"/`) and shows an empty commit as `+` with no counterpart on the base (observed), so it is not used. Line endings, trailing whitespace and indentation style add no refusal outside the list; token joining, import ordering and gofmt-style reformatting change the normalized digest and are retained, while `merge-noop` still proves them when the base has the final content.

## Binary paths

A path is binary when `check-attr --source=B^{tree}` reports `binary` set, `diff` unset or `text` unset; with those attributes unspecified, when `diff-tree --numstat` prints `-` (NUL bytes). All of `check-attr`, `diff-tree --numstat` and both `diff-tree -p` streams run with `git --attr-source=B^{tree}`, so worktree and untracked `.gitattributes` files never apply, and with the private empty `HOME`/`XDG_CONFIG_HOME`, null `GIT_CONFIG_GLOBAL`/`GIT_CONFIG_SYSTEM`, `GIT_CONFIG_NOSYSTEM=1` and `GIT_ATTR_NOSYSTEM=1`, so global, XDG and system attributes never apply. `$GIT_DIR/info/attributes` is still read by Git under `--attr-source` (measured on Git 2.47.3 and 2.56.0), so it is not pinned away. It is resolved exactly as the existing code resolves the git/common directory, read with a 1 MiB bound and no symlink following; when it contains any non-blank, non-comment line that defines or unsets `binary`, `diff`, `text`, `eol`, `merge` or `whitespace`, or a macro (`[attr]`), the attribute-dependent decisions (the binary classification of the patch classes and `merge-noop`) are `not-evaluated: info-attributes-present`; `ancestor` and `no-net-change` are unaffected, and the file is ignored when absent or only comments. `whitespace` is not used. A commit touching a binary path matches only by verbatim digest.

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
| `diff-tree --stdin --numstat -r -z --no-renames` | 8,192 | 34,603,008 | `(\d+\|-)` TAB `(\d+\|-)` TAB PATH NUL |
| `check-attr -z --stdin binary diff text` | 24,576 | 103,809,024 (3 x 8,192 x 4,224) | PATH NUL ATTR NUL VALUE NUL |
| stw normalizer | one line | 1 MiB per line, 64 MiB per tip | opaque lines; longer is `proof-limit` |
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
3. Patch classes: count bounds exceeded `proof-limit`; merge commit in `M..B`: retained `merge-commits-in-range`; normalized-only match on a whitespace-significant path: retained `whitespace-significant-difference`.
4. Per-command 10 s, per-tip 20 s or the 60 s forge-free proof phase exceeded: remaining tips `proof-budget-exhausted`.
5. Everything else not proven is retained `integration-not-proven` with classification. No branch is deleted by cherry-pick replay, subject similarity or a user-named range.

## What each class does not claim

`merge-noop`: not review, not a PR, not authorship. `patch-exact`/`patch-id`: not that the change is still present. `integrated-modulo-whitespace`: not that indentation matches. `no-net-change`: not integration; only that nothing is unmerged. `ancestor`: not review. None says anything about CI or other branches. Each local deletion keeps the recovery ref and journal entry described in the spec's design rationale (tip commit and history only; reflog and `branch.<name>.*` configuration are not recorded; restore is manual; local to this clone).
