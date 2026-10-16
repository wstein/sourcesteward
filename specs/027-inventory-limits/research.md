# Research: Inventory limits and checkout description

Measurements were made on 2026-10-16 in scratch repositories under `/tmp/claude-1000/.../scratchpad/m` with Git 2.56.0 (`/usr/local/bin/git`, linux/arm64) and, where stated, Git 2.47.3 (`/usr/bin/git`). The current code was exercised through a throwaway Go test (not committed; it calls `Inventory` and prints status, diagnostics, workspace observations and wall time) so that no binary was built. Fixtures: `r150`, `r400`, `r1030`, `r5000`, `r50000` (n files of 12 bytes in directories of 50), `long1030` (1,030 files under four 50-byte path segments, 229,690 bytes of `ls-files -z`), `tags400/500/512/700/1100` (n lightweight tags on a 20-file repository), `big` (one 100,000-byte tracked file), `super` (30 files and two initialized submodules `mods/sub1`, `mods/sub2` of 60 files each) with a linked worktree `super-wt`, `sep` (`git init --separate-git-dir`). jj is not installed in the container; nothing about jj was measured.

## Measurement 1: current failures (HEAD of origin/main, Git 2.56.0)

Default scopes (`disk`, `tracked`, `untracked`) unless noted. Times are wall time of `Inventory`.

| Fixture | Result | Diagnostic | Time |
| --- | --- | --- | --- |
| r150 | `ok`, `tracked` false | | 36 ms |
| r400 | `error` | `git-inventory-failed` "cannot safely observe content attributes" | 15 ms |
| r1030 | `error` | `git-inventory-failed` "inventory record limit exceeded" | 18 ms |
| long1030 | `error` | `git-inventory-failed` "cannot safely observe content attribute usage" | 16 ms |
| r5000, r50000 | `error` | same as long1030 | 15 ms, 19 ms |
| r400 `--scope stored` | `ok` (400 stored files) | | 26 ms |
| r1030, long1030, r5000, r50000 `--scope stored` | `error` | `git-inventory-failed` "cannot observe Git index" | 13-21 ms |
| tags400, tags500 | `ok` | | 39, 42 ms |
| tags512, tags513 | `error` | `inventory-limit` (each lightweight tag costs 2 records: reference and added target) | 28 ms |
| tags700 | `error` | `inventory-limit` | 29 ms |
| tags1100 | `error` | `git-inventory-failed` "cannot enumerate Git references" (`for-each-ref` prints 94,662 bytes) | 13 ms |
| big (100,000-byte tracked file) | `partial`, `tracked` unknown "tracked disk content exceeds 64 KiB per-file observation limit"; the file is listed with change `unknown` | | 24 ms |
| super clean, default scopes | `partial`, both workspaces `tracked` unknown "submodule disk state excluded to avoid recursive helpers" | | 37 ms |
| super with a modified ordinary file | `partial`; 2 file records (scopes `tracked` and `disk`); both workspaces still `tracked` unknown | | 46 ms |
| super `--scope stored` | `ok` (3 files); workspaces `tracked` unknown "raw disk comparison not requested" | | 29 ms |
| `mods/sub1` clean | `ok`, one workspace whose path is `.../super/.git/modules/mods/sub1`, `tracked` **true**, `untracked` false, 0 files | | 33 ms |
| `mods/sub1` with a modified tracked file | same: `tracked` true, 0 files (the modified file is not seen) | | 36 ms |

Byte sizes explaining the thresholds: `check-attr -z` for the five attributes prints 29,850 bytes for 150 files (199 bytes per file with 18-byte paths), so the 65,536-byte cap is crossed near 330 files (the 023 estimate of 150 to 400 is confirmed; r150 passes, r400 fails). `ls-files -z` prints 95,000 bytes for 5,000 files. `ls-files --stage -z` costs about 69 bytes per entry, crossing the cap near 950 entries. `for-each-ref` costs about 86 bytes per tag.

Code locations: attribute check `workspaceTransforms` (`internal/inspection/inventory_git.go`: `ls-files --cached`, then one `check-attr` with all paths as arguments); index read `workspaceStatus` (`ls-files --stage -z`, `if len(fields) > inventoryRecordLimit`); `workspaceTransforms` (`if len(paths) > inventoryRecordLimit`); the shared runner `runGit` in `git.go` fails every command over `outputLimit`. `workspaceStatus` runs even when only `stored` is requested.

Two defects in the same code, found while measuring, are part of this spec: a lightweight tag costs two returned records, so about 511 tags make a repository `error`; and `parseGitInventoryRefs` and `parseGitInventoryWorktrees` apply the returned-record limit to what is scanned.

## Measurement 2: costs of the proposed reads (Git 2.56.0 unless noted)

- `git ls-files -z | git check-attr --stdin -z filter text eol ident working-tree-encoding` on r50000: 9,950,000 bytes of output in 57-67 ms (output identical byte for byte under Git 2.47.3). It works with `-z --stdin` on 2.47.3; the output groups by path then attribute in the order requested, as the argument form does.
- `git ls-files --stage -z` on r50000: 3,450,000 bytes in 7 ms. `ls-files --others` on a clean r50000: 8 ms. `for-each-ref` for 1,100 tags: 8 ms.
- The Go raw comparison loop (`Lstat`, confined open, bounded read) over r5000 took 30 ms and over r50000 269 ms with a warm cache (12-byte files, no hashing). Two passes therefore cost about 0.6 s for 50,000 small files; large files are governed by LIM-11.
- Per-reference ancestry costs one Git process each (`merge-base --is-ancestor`); with `--base`, `--revision` or `--remote` and 1,000 references this would be thousands of processes, which is why those facts are computed only after the listing is truncated.

## Decision 1: stream, do not batch

Alternatives: (a) batch `check-attr` over argument lists of 100 paths (rejected: still one process per batch, argument-length and leading-dash concerns, and the output of other commands still needs a streaming reader); (b) raise `outputLimit` (rejected: changes the schema constant meaning and every command's memory bound); (c) skip the attribute check for repositories without any `.gitattributes` in the index (rejected: attributes also come from `info/attributes`, the global file and uncommitted worktree files; the check is the only complete source); (d) reuse `pinsRunner` (rejected: it passes explicit `--git-dir`/`--work-tree`, which makes Git skip its ownership check, the very class of mistake Spec 021's analysis recorded; inventory discovers the repository by working directory and keeps `safe.directory`). Chosen: a small streaming runner in `internal/inspection` with the shared isolation, a record callback, a scan allowance and process cancellation, plus `check-attr --stdin`. The 021 design (scan allowance per invocation, per-record limit) is followed; its code is not shared.

## Decision 2: the 1,024 records are a returned bound, not a scan bound

The schema pins `records: 1024` with `const`; changing it needs a new schema version and breaks validators of consumers, and the 1 MiB result bound makes more than about 1,000 records unwise anyway (Spec 023 embeds up to 16 inventories in an 8 MiB result). So the value stays and means returned records. Alternatives: schema v2 with a larger bound (rejected: no evidence of need, consumers' size budgets); keep error on overflow (rejected: Werner's decision asks for partial where safe).

## Decision 3: where truncation is safe

A listing of changed files omitted at the end can never turn dirty into clean; the disclosure says omitted paths are not clean. A clean/dirty predicate with a positive record stays known. A truncated reference listing can make a predicate lie only if the predicate reads the listing, so predicates and resolution use the complete scanned list; tags are omitted first because they are not inputs to local-only or merged facts of branches. A reference enumeration cut at an unknown point (scan allowance) stays an error. The fair-share rule keeps either section from starving the other; its shape (half each when both overflow) is a design choice recorded as an open question.

## Decision 4: describe the checkout (CK)

`git worktree list` prints the Git directory for a submodule or separate-Git-directory checkout (measured on submodule and `--separate-git-dir`, Git 2.56.0 and 2.47.3). The identity inspection already knows the checkout (`repository.worktree`, `git rev-parse --show-toplevel`, trusted by Git's own `core.worktree` handling). Substituting it for the entry reuses every existing check (`inspect` with the same common directory, equal HEAD, ownership markers). For a linked worktree of a submodule, `--show-toplevel` is the linked worktree and the main checkout is unknown from the identity; reading `core.worktree` from the common configuration was rejected as an extra repository-controlled path to validate. The entry is qualified instead (CK-2). A linked worktree of a submodule, run from the main checkout, gives the main entry (CK-1) and the linked entry (an ordinary path), measured with `git worktree add` inside `mods/sub1`: the porcelain lists the Git directory and the linked path.

## Decision 5: gitlinks (GL)

False cannot be proven without looking at the submodule worktree, which would need Spec 021's confinement layer (ownership, symlink and commondir checks) and an index-metadata comparison. That is a different feature (`stw pins`). What can be proven without it is positive evidence: a staged gitlink change is visible in `diff --cached` (measured: `M mods/sub1`, `:160000 160000 <old> <new> M`) and ordinary-file differences are already computed. So `tracked` becomes `true` on positive evidence and stays `unknown` otherwise. Alternatives: reuse pins' dirty-state machinery (rejected for this prerequisite: large surface, new hostile paths; recorded as the follow-up that would let a clean superproject be `false`); treat gitlinks as clean (rejected: false claim).

## Decision 6: content read budget (LIM-11)

Without it a repository of 100,000 files of 64 KiB would read 6.4 GB per pass and end in `inventory-timeout`, an `error`, in contradiction with "partial where safe". 256 MiB per pass is about 0.3 s of hashing at 1 GB/s on this machine (SHA-1 throughput not measured separately; the number is a design choice, open to the review). The per-file 64 KiB cap stays (out of scope, listed as a follow-up: files above it make `tracked` unknown, measured on `big`).

## Decision 7: retention and index limits

LIM-7 (100,000 entries, 16 MiB of paths) and LIM-8 (100,000 candidates) are memory guards with a measured time basis: 50,000 entries cost about 0.3 s of comparison per pass. They bound memory to the order of tens of MiB. LIM-9 (20,000 references) bounds parsed reference structures (several hundred bytes each).

## Decision 8: process stop

Stopping a stream early cancels the command context. Go sends SIGKILL to the process; `ls-files`, `check-attr`, `diff` and `for-each-ref` hold no lock when run with `GIT_OPTIONAL_LOCKS=0`, and the verification asserts that no `index.lock` exists and the index bytes and modification time are unchanged after an early stop. Alternative: close the pipe and let SIGPIPE end the process (rejected: a process blocked in a filesystem call would not notice before the deadline).

## Unverified

jj behavior (not installed); Git minimum 2.46 exactly (2.47.3 is the oldest available here); darwin; cold-cache I/O times; SHA-1 throughput; behavior of `check-attr --stdin -z` on Git 2.46.0 (the option exists since 1.8; the `-z` pairing with `--stdin` since 1.8.5; expected, confirmed only on 2.47.3 and 2.56.0).
