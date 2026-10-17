# Tasks: Inventory limits and checkout description

**Input**: [spec.md](spec.md), [plan.md](plan.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/inventory-limits.md](contracts/inventory-limits.md), [analysis.md](analysis.md).
**Tests**: Required by constitution principle IV. Write each behavior test and observe it fail before the implementation. A tick records execution, never design approval.
**Work items**: WI1 to WI7 are stacked, each one reviewable change that ends with `make check` green and atomic Conventional Commits (at most 72 characters, a body that explains why). Tasks name the requirements (FR), stories (US) and normative rows (LIM, FEED, QR, FS, DEG, TXT, GL, SP, CK, COMPAT) they cover.
**Test hygiene (all tasks)**: fixtures use `t.TempDir()` and the existing `git` helper; because the inventory's own Git runs drop `GIT_*` variables, tests set `HOME` and `XDG_CONFIG_HOME` to temporary directories so global configuration cannot leak; no test depends on inode reuse, modification-time ties, the process umask (TestMain sets 022), a symlinked `TMPDIR` (resolve with `filepath.EvalSymlinks` where paths are compared) or timing; limit rows use injected small bounds, not large files, except the scale tests of T005 which use production bounds with real repositories. Run each new test file also under `PATH=/usr/bin:$PATH` (Git 2.47.3).

## Phase 1: Analysis

- [ ] T001 Record independent cross-artifact analysis in specs/027-inventory-limits/analysis.md (four rounds, see analysis.md), resolve every critical, high and medium finding before any code.

## WI1 — Streaming primitive (no behavior change)

- [ ] T002 [FR-001, FR-008, FR-012, LIM-3, LIM-4] Add failing tests in internal/inspection/inventory_stream_test.go against real `git`: record splitting on NUL and on NUL-LF with records split across chunk boundaries; a record over LIM-4 stops the command; stdout and stderr both count toward LIM-3; an early stop (`errStreamStop`) leaves no process and no `index.lock` and does not change index bytes or modification time; context cancel and deadline end a command that prints nothing (fake `git` earlier on `PATH` via `t.Setenv`, never parallel); non-UTF-8 and newline-bearing paths arrive as bytes; the prefix and environment equal `runGit`'s (compare the argument vector through a recording wrapper and prove `GIT_*` inheritance is dropped with a planted `GIT_DIR`).
- [ ] T003 [FR-001] Implement internal/inspection/inventory_stream.go (`streamGit`, `gitStream`, `streamLimitError`, `errStreamStop`, `inventoryBounds`, `defaultInventoryBounds`), and the adapter that turns an injected buffered `runner` into a `gitStream`; add `stream` and `bounds` to `gitInventorySession`; production calls still use the buffered path. Existing tests stay green (the interposition test of references uses the adapter).

## WI2 — Large repositories (US1, US2)

- [ ] T004 [FR-002, FR-003, US1, SC-001] Add failing tests (real Git, production bounds) in internal/inspection/inventory_git_scale_test.go: 150, 400, 1,030, 5,000 short-path files and 1,030 files with 200-byte paths, default scopes: clean gives `ok`, `tracked` false, no files, within the deadline; `--scope stored` on a repository whose root commit holds all files gives `partial` with TXT-1 for 1,030 and 5,000 files (stored records exceed the share) and `tracked` unknown "raw disk comparison not requested"; a real-shaped fixture (5,000 files, ten tracked binaries of 100 KiB to 2 MiB, a font and an image, a sparse checkout, 200 branches and 200 tags) clean gives `ok` and `tracked` false; one modified tracked file and one untracked file give three records (`disk` and `tracked` for the modified file, `untracked` for the new one); an unborn HEAD with 5,000 staged files; `core.autocrlf=true`; a `.gitattributes` `text`, `eol=crlf`, `ident`, `working-tree-encoding` and a configured `filter=` on the last of 5,000 files each give the existing qualification; a conflicted index. Each observed failing on current code with the diagnostics of research Measurement 1.
- [ ] T005 [FR-002, FR-008, SC-004] Add failing hostile tests at scale: planted clean/process/smudge filters, textconv, external diff, `core.fsmonitor`, hooks (`pre-commit`, `post-index-change`, `fsmonitor-watchman`) with sentinels on a 1,030-file repository with stat-dirty files, global `core.attributesFile`, `info/attributes`; assert no sentinel, byte-identical tree (`snapshot`), index mtime unchanged, no lock files, and the process list of the run empty afterwards.
- [ ] T006 (also the WI2 row of plan.md "Spec 023 follow-up": r6Attr and r7Limit in repos_integration_test.go become ok-with-facts, failing first) [FR-001, FR-002, FR-003, FR-010, FR-015, LIM-7, LIM-8, LIM-10, LIM-11, FEED-2 to FEED-4, DEG-5a, DEG-5b, DEG-8, DEG-13, DEG-14, SP-1, SP-2, TXT-3 to TXT-5, TXT-10 to TXT-13] Implement streamed attribute check (`check-attr --stdin -z`, early stop), streamed index passes without retention of clean entries and with a rolling SHA-256 signature, LIM-7 and LIM-11 accounting, and the removal of the scan-time use of the returned-record limit in `workspaceTransforms` and `workspaceStatus`; failing tests with injected small bounds first (index limit, record limit, scan allowance on each feed, read budget) asserting TXT strings verbatim, `tracked` and `conflicts` unknown on index abort with equal signatures in both passes, `partial` for the inspected workspace only, and that a second workspace is qualified without failing the run; the concurrent stdin writer (a test with a repository large enough that a sequential writer would deadlock, and an early stop that ends the writer); files of 64 KiB to 16 MiB compared correctly (the existing oversize test moves its threshold and is rewritten first), a file above 16 MiB unknown with TXT-11; skip-worktree entries absent on disk produce no records and TXT-12 while a present, modified skip-worktree file is reported (`--query clean` false, workspace `tracked` true; the fixture uses `git update-index --skip-worktree` without sparse checkout, because with `core.sparseCheckout` Git clears the bit in memory and shows `H`), the inspected workspace compared first when linked worktrees would exhaust the budget (DEG-8 for the others, not for it); `ls-files --stage -t -z` tags `H`, `S`, `M` accepted and a conflicted index parsed, other tags `invalid-metadata`; the content budget shared across several linked worktrees (LIM-11, second pass compares the first pass's paths), the header length from `st.Size()` with a file growing between stat and read giving `changed-repository`; `core.symlinks=false` (SP-2); `--path` on a 5,000-file repository does not fill the candidate budget (DEG-13); a non-UTF-8 index path is `invalid-metadata` (DEG-14); a fake `git` on `PATH` that blocks gives `inventory-timeout` (DEG-12); a buffered command over 64 KiB keeps `git-inventory-failed` (DEG-11, BUF); more than the base and workspace budget gives DEG-10.
- [ ] T007 [FR-003, FR-004, FR-011, FR-016, FEED-5 to FEED-7, QR-1, DEG-1 to DEG-3, DEG-7, DEG-9, FS-1, TXT-1, TXT-4, TXT-6] Failing tests (including the scan-time record checks of the current loop, `len(r.Workspaces)+len(r.References) >= inventoryRecordLimit` and the file-append checks, which must no longer fire at scan time) then streamed staged, untracked/ignored and stored feeds with candidate retention (LIM-8), path-ordered truncation, the fair share, DEG-9 trimming (200-byte paths), and QR-1: 3,000 modified files plain (partial, TXT-1 with the exact count), `--query dirty` (true, `ok`, `complete` true, exit 0 through the CLI command in internal/cli with a recording runner, note only; also when FS-1 leaves zero files because references fill the budget), `--query clean` with a candidate (false), FEED-5 overflow gives `tracked` unknown and a predicate that is known only when a non-`unknown` candidate exists (DEG-7), each FS-1 branch (files fit, references fit, both overflow), untracked listing early stop (`at least`), stored root commit of 5,000 files, scan allowance exceeded on each feed (observation unknown), an interleaved change between passes still `changed-repository`.
- [ ] T008 [FR-005, US1-5, FEED-1, QR-1, DEG-4, DEG-6, TXT-2] Failing tests then streamed reference scan (including the `--query ref-exists|ancestor` early path, which streams, applies LIM-9 and returns references by FS-1): 700 and 1,100 tags and 25,000 references (DEG-6 error with the exact message), a repository with 600 branches and 600 remote refs (kind order, whole references, nested cost), `refs/pull`-style namespaces not scanned, `ref-exists` for an omitted tag is true, `ancestor` and base/upstream resolution with omitted references, predicate-mode exception, per-reference facts computed only for returned references (count the processes started with a recording runner), recheck by digest (a branch created between the two reads is `changed-repository`, through the stream seam).

## WI3 — Checkout description (US3)

- [ ] T009 [FR-006, CK-1 to CK-3, TXT-7, COMPAT-3] Failing tests (real Git): clean and dirty submodule checkout (path is the checkout, `tracked` false/true, files with checkout-relative paths), `git init --separate-git-dir`, the same from a subdirectory of the checkout, a linked worktree of a submodule (CK-1 resolves the main checkout through `inspect(entry)`, plus the linked entry), CK-2 for a run from a linked worktree whose main entry has no `core.worktree` (the same case run from the main checkout expects CK-1; qualification, no `ls-files`/`diff`/disk read against the Git directory — recording runner — and `partial`), a bare repository and an ordinary repository unchanged, a hostile `core.worktree` pointing at another repository and at a non-existent path (CK-2, candidate never printed, no `ls-files`/`diff`/content read of it), `--query clean` and `--query dirty` on clean and dirty submodule and separate-git-dir checkouts (`unknown` unless the checkout entry was observed; the former false `true` is a failing test first), TXT-7 asserted verbatim, a candidate that disappears during the run reverts to CK-2, a bare repository with `--query clean --scope stored` stays known, a checkout path that disappears during the run.
- [ ] T010 [FR-006] Change the 023 tests, goldens, smoke script and strings of the WI3 rows of plan.md "Spec 023 follow-up" (failing first), then implement CK-1 to CK-3 in the worktree loop of inventory_git.go on top of the existing path, ownership and identity checks.

## WI4 — Gitlink-bearing superprojects (US4)

- [ ] T011 [FR-007, GL-1 to GL-5, QR-1, TXT-8, TXT-9, COMPAT-4] Failing tests (real Git, initialized submodules, linked worktree of the superproject): modified ordinary file (`tracked` true both scopes, TXT-8, plain run `partial`, `--query dirty` true/`ok`/exit 0), staged ordinary file, staged gitlink bump, clean superproject (`unknown`, TXT-9, `partial`, `clean` unknown), gitlink outside the path filter, `--scope untracked` only, a submodule with planted hooks/filters/fsmonitor (sentinel plus a recording runner proving no command runs with the submodule as working directory), transform present with a gitlink (GL-3), an oversize tracked file with a gitlink (unknown), `dirty` with only an `unknown` file record (unknown).
- [ ] T012 [FR-007] Implement the gitlink rule and the query rule in inventory_git.go (positive evidence flag, `gitlinkOnlyIncomplete`), keeping the existing reason strings.

## WI5 — Contract validation and documentation

- [ ] T013 [FR-009, SC-005, COMPAT-8, COMPAT-9] (also the WI5 row for internal/contracts/repos_test.go) Add a test in internal/workspace pinning the handoff fallback for an inventory near 1,023 records, and a test that a jj-managed fixture's inventory path is untouched (skipped without jj, which counts as not run). Extend internal/contracts/inventory_test.go: validate real outputs of the T004, T007, T008, T009 and T011 fixtures against the unchanged schema, validate the data-model example, and assert the schema bytes equal the bytes at the merge base (`git show` of the file is not available to tests; compare to a recorded SHA-256 constant with a comment naming the commit); existing fixtures untouched.
- [ ] T014 [P] [FR-009, FR-014, COMPAT-1 to COMPAT-12, TXT-1 to TXT-13] Update docs/inventory.md (limits paragraph, degradation, checkout and gitlink behavior), docs/json-contracts.md (inventory section: compatibility notes), docs/agent-guide.md and docs/backends.md where they state the old limits, a pointer note in specs/009-workspace-inventory/contracts/inventory.md to this amendment, and the Spec 023 table of the plan kept current; `python3 scripts/check-docs.py` passes.

## WI6 — Conformance and acceptance evidence

- [ ] T015 [FR-013, SC-006] Extend scripts/smoke-inventory.py and scripts/test_inventory_smoke.py with a large-repository case, a submodule-checkout case and a gitlink-superproject case in the style of the existing cases (byte/mode-identical target state, schema validation of the output); the unit test of the script runs in `make check`. The binary acceptance run needs a built `bin/stw` and is not run without the maintainer's approval of a build; record it as not run.
- [ ] T016 [FR-013, SC-001 to SC-007] Run gofmt, `go vet ./...`, `go test -race ./...`, `go test -race -count=3 -shuffle=on ./internal/inspection ./internal/contracts ./internal/cli`, the same inspection tests under `PATH=/usr/bin:$PATH`, `python3 scripts/check-architecture.py`, `python3 scripts/check-docs.py`, `git diff --check`; record versions, platforms, outcomes and everything not run in specs/027-inventory-limits/verification.md.
- [ ] T017 Update docs/progress.md and docs/feature-status.json: the entry stays `planned` until verification evidence exists; no tag, release, publication, image or documentation build.

## WI7 — Spec 023 consequences (merged consumer)

- [ ] T018 [FR-014, SC-007] Apply the WI7 rows of plan.md "Spec 023 follow-up" (texts and documentation only; the tests, goldens, smoke script and production strings of the earlier rows are changed in WI2, WI3 and WI5 with failing tests first): update the 023 contract, spec, research, plan, data-model, tasks, analysis and verification texts and docs/repos.md, docs/agent-guide.md, docs/commands.md, docs/backends.md, docs/README.md and llms.txt; `make check` green.

## Dependencies

WI1 before everything. WI2 before WI3 and WI4 (they use the streamed passes). WI3 and WI4 are independent of each other after WI2. WI5 follows WI4; T014 may start once contracts/inventory-limits.md is stable. WI7 follows WI4 and WI5 (it needs the final behavior). WI6 last.

## Coverage map

| Requirement | Tasks |
| --- | --- |
| FR-001 | T002, T003, T006 |
| FR-002 | T004, T005, T006 |
| FR-003 | T004, T006, T007 |
| FR-004 | T007, T008 |
| FR-005 | T008 |
| FR-006 | T009, T010 |
| FR-007 | T011, T012 |
| FR-008 | T002, T005 |
| FR-009 | T013, T014 |
| FR-010 | T006 |
| FR-011 | T007, T008 |
| FR-012 | T002 |
| FR-013 | T015, T016 |
| FR-014 | T014, T018 and the plan table |
| FR-015 | T006 |
| FR-016 | T007, T011 |
| SC-001 | T004 |
| SC-002 | T006, T007, T008, T013 |
| SC-003 | T009, T011 |
| SC-004 | T005 |
| SC-005 | T013 |
| SC-006 | T016 |
| SC-007 | T014, T018 |
| US1 | T004, T008 |
| US2 | T006, T007 |
| US3 | T009 |
| US4 | T011 |
| US5 | T005, T013, T016 |
