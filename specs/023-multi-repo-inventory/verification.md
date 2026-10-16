# Verification: Multi-repository inventory

**Status: implemented, not verified.** Source checks and the Git lane of the acceptance script passed locally. The jj lane, the foreign-owner case, the hosted minimum and current lanes and an independent re-review of the round-5 fixes have not run, so nothing here is acceptance (SC-002 and SC-005 stay open) and no release, tag or publication is implied. The maintainer decision on the usefulness assessment is also open (analysis.md).

Source revision checked: `29f7b1c` (branch `codex/023-multi-repo-inventory`, PR #49); the documentation, tasks, analysis and this file were committed after it and change no code.

## Environment

| Item | Value |
| --- | --- |
| Platform | linux/arm64 (Debian 13 in OrbStack, kernel 7.2.9), local evidence only: not the ubuntu-24.04 amd64 hosted lane, no darwin |
| Go | 1.27.2 |
| Git | 2.56.0 (default `PATH`) and 2.47.3 (`/usr/bin`, second run) |
| jj | not installed |
| Python | 3.13.5; Node not installed |
| User | uid 1000 (not root), process umask 0000 (the inspection tests set 022 as the Spec 021 tests do) |

## What ran, and the outcome

| Check | Outcome |
| --- | --- |
| `gofmt -l cmd internal`, `go vet ./...`, `go mod verify` | clean |
| `go test -race -count=1 ./...` (Git 2.56.0) | all packages `ok` |
| `go test -shuffle=on -count=3 ./internal/inspection` (Git 2.56.0) | `ok` |
| `PATH=/usr/bin:$PATH go test -shuffle=on -count=2 ./internal/inspection ./internal/contracts` (Git 2.47.3) | `ok`; the `extensions.submodulePathConfig` test skips before touching a fixture |
| `TMPDIR` set to a symlink to a directory: `go test ./internal/inspection ./internal/contracts ./internal/cli` | `ok` (the previous tests failed here) |
| `umask 077`, `HOME=/nonexistent`, `go test -run Repos ./internal/inspection` | `ok` |
| `python3 scripts/check-docs.py`, `python3 scripts/check-architecture.py`, `git diff --check` | pass |
| Python unit tests of the smoke scripts (`test_repos_smoke`, `test_inventory_smoke`, `test_architecture`, `test_backends`, the others except docs) | pass |
| `python3 -m unittest scripts.test_docs` | 11 pass, 1 error: `test_packaging_excludes_source_and_binds_revision` needs the pinned Hugo under `.tools/` and stops at `hugo version`; this environment cannot run it and the test was not run again. It is unrelated to this change |
| `python3 -B scripts/smoke-repos.py --binary <scratch binary>` (Git lane, 10 cases) | pass: every embedded inventory equals `stw inventory`, the summary equals its recomputation, the fixture tree is byte, mode and mtime identical, no sentinel fired (hook, fsmonitor, clean/smudge/process filter, textconv, external diff, alias, credential helper, editor, pager, gpg, ssh command, remote helper as the network sentinel), a jj marker without jj fails alone as `jj-unavailable` |

`make check` was not run as one command because of the Hugo-dependent packaging test and the missing Node (`node --test scripts/test_theme.mjs`); each other step was run on its own as listed. The scratch `stw` binary for the smoke run was compiled into a scratch directory, not into `bin/` or `dist/`; no release artifact, image, documentation or site build was made.

## Not run

- jj lane of the smoke script (two parallel members, a secondary workspace, a subdirectory, `--submodules` on a jj parent with no Git discovery process, equality with `stw inventory --scope stored`) and every jj-specific claim in the spec (the jj identity comparison, the jj 64 KiB output limit): jj is absent. Written, unexecuted.
- Foreign-owned member in the smoke script: needs root (it chowns a member); not exercised. `GIT_TEST_ASSUME_DIFFERENT_OWNER` cannot be used because the runner strips `GIT_*`.
- Hosted minimum and current lanes (ubuntu-24.04 amd64), darwin, any Git older than 2.47.3, Windows.
- Independent re-review of the round-5 fixes.
- Site, Pages and documentation builds.

## Mutation spot-checks

The reviewers' 18 surviving mutants were mapped onto the fixed code and re-run together with their killed neighbours (every run executes the inspection, cli and contracts packages). All are killed now. Where the code the mutant targeted was replaced, the mapped mutant is named.

| Mutant | Result |
| --- | --- |
| M04 identity comparison ignores `git_dir`, `common_dir`, worktree, HEAD ref, jj operation id (five variants) | killed by the per-field identity table |
| M05 discovery sorting removed | killed (order test with names reversed against paths) |
| M11, M12 parallelism 1 and 8; M13, M14, M15 output, total and per-call limits | killed (exact peak of 4 workers, limits asserted on the boundary object) |
| M16, M17, M36b control characters, newline, Cf/Zl/Zp escaping dropped | killed (text escaping tests) |
| M21 final budget re-check removed | killed by the omission-growth test |
| M27 stale/foreign classification | killed (classifier table and facts counts) |
| M32b, M33b, M39 pointer, symlink and `.git` kind checks (now the safety layer's refusal branch; plus its symlink, kind, alternates, write-bit and pointer checks, five mutants of `pins_safe.go`) | killed by the real hostile fixtures |
| M35 caller cancellation mapped to a timeout | killed |
| M37 post-inspection worktree check, and the new post-inspection Git directory check | killed by hostile inspection results |
| M40 16-entry discovery cap, M41 uninitialized not skipped | killed |

New spot-checks of the fixes (36): non-resolved symlinks, non-UTF-8 path accepted, null diagnostics, empty backend, sampling statement not skipped, note cap 17, omission overwriting the inherited cause, the final fallback relabelling entries without inventory, strings not scrubbed, identity duplicate of a submodule undetected, bare and unborn parents running discovery, superproject Git directory not compared, safety recheck dropped, gitlink confirmation always true, ignored-note prefix, dirty caveat dropped, discovery panic not recovered, foreign guard for an empty manager, escape limit 1024, path dedupe dropped, lexical check dropped, per-call runner and inspection deadlines dropped, name tie-break highest, first instead of last value per name, unborn not special, root equality dropped, refusal message dropped, skipped summary count dropped, changed-repository keeping the inventory. 34 are killed. Two survive and are equivalent: dropping the "caller context first" test in `mapTimeoutError` (the repos context is derived from the caller's, so the second test already covers it) and clearing `entry.Inventory` in the changed-repository branch (the field is still nil there). Five first results of the run were "build failed" (an unused import after the edit), which is not a kill; they were re-run with compile-safe edits and are killed.

## Deviations and residual risks

Listed in analysis.md round 5. In short: `--submodules` refuses what the pin audit refuses (alternates, commondir, directories writable by others, which includes an empty uninitialized submodule directory under `umask 000`); the per-call and total deadlines are variables for tests; the test fixtures for the safety layer need umask 022 (set in the inspection test main, and by chmod in the contracts fixture and the smoke script). Residual race (disclosed in the result limitations): a symlink swapped in after the confinement check can be read by the identity inspection before the post-check rejects it.
