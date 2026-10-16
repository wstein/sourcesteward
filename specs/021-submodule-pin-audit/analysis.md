# Pre-implementation analysis

## Round 1

Independent reviewer: read-only Opus review, revision 642bfa4, 2026-10-16. Applied a Spec Kit style cross-artifact analysis against the constitution and the code. Verdict: NOT READY, with four high findings. Three claims were checked again by the author before changing the artifacts: Git keeps `modules` per worktree (measured with a linked worktree: `rev-parse --git-path modules` returned `.git/modules` and `.git/worktrees/wt/modules`, and the submodule pointer read `gitdir: ../../super/.git/worktrees/wt/modules/member`); the shared runner caps each stream at 64 KiB (`internal/inspection/git.go`); and the inventory implementation avoids `git status` on purpose so that configured filters cannot run. The finding that an explicit `--git-dir` skips Git's own ownership check is accepted from the reviewer and is covered by a test task that records which cases the test environment can exercise.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | High | Shared `modules/` rule refuses every submodule of a linked worktree | Plan DD2 uses `rev-parse --git-path modules`; spec US5-4 and edge cases; T006 linked-worktree fixture |
| 2 | High | `git status` can run clean and process filters | Plan DD5 reuses the inventory comparison; FR-010, US3-1; T010, T012 filter sentinels |
| 3 | High | Explicit `--git-dir` bypasses `safe.directory`; `submodule-unsafe-owner` could never fire | Plan DD3 and FR-010 check ownership in the audit; US3-3; T007 |
| 4 | High | SC-005 and T018 assume lanes that CI does not have | SC-005 and FR-016 limited to existing ubuntu-24.04 lanes; arm64 and darwin recorded as local evidence; T020, T021 |
| 5 | Medium | Refusal codes differ across artifacts and from the existing vocabulary | One normative refusal table in the spec, kebab-case like `unsupported-backend` |
| 6 | Medium | Finding codes and severities differ between spec and data model | Normative table in the spec (19 codes); the data model refers to it |
| 7 | Medium | Exit status precedence unclear; docs not updated | FR-014 and contract precedence 2, 1, 3, 0; T005, T018 |
| 8 | Medium | `core.worktree`, `commondir`, alternates and swap-after-validation | FR-009, DD2, T006, T010 |
| 9 | Medium | 64 KiB runner cap, unbounded ref scan, per-side counts | DD4 and DD6 (streaming runner, `for-each-ref --contains --count=17`, two capped `rev-list` calls); FR-005, FR-011; T008, T011, T013 |
| 10 | Medium | Source of `.gitmodules` unspecified | FR-017 and DD1; US4-3; T008 |
| 11 | Medium | Ownership detection may run sandboxed jj; no jj sentinel; jj-managed submodules | FR-002 and DD7 (markers only, no process); SC-003; T004, T015, T020 |
| 12 | Medium | DD2 contradicted T006 on in-place `.git` directories | DD11 |
| 13 | Low | Non-UTF-8 paths cannot be JSON | Edge cases: refused with `submodule-path-unsafe` |
| 14 | Low | `complete` semantics unclear | Data model completeness rules |
| 15 | Low | Missing tags and tests (nested detection, owner, unborn/detached HEAD, replaced objects, symlinked `.gitmodules`, over 1 MiB, multi-remote, network sentinel) | Tasks tagged with FR and US; added T008, T009, T015, T017 |

## Round 2

Independent reviewer: read-only Opus review, revision 9635f69, 2026-10-16, with scratch repositories on Git 2.47.3 and 2.56.0 (Git 2.46 was not available to the reviewer). Verdict: NOT READY. Round-1 findings 1, 3, 4, 5, 6, 7, 12, 14 and 15 were confirmed resolved; 2, 8, 9, 10, 11 and 13 were only partly resolved. Three high and four medium new findings were raised.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | High | "Every refusal starts no process" cannot hold: GitButler, bare and not-a-repository come from Git processes | FR-002, plan DD7, contract Effects, T004: only jj and ambiguous refusals start no process; the others use the existing Git detection and start no jj process and no submodule command |
| 2 | High | Git 2.56 `extensions.submodulePathConfig` stores `modules/a%2fb`; valid submodules would be refused | FR-009, plan DD2, T006: accept the configured gitdir when confined under the modules path; fixture on the current lane; also `--path-format=absolute` |
| 3 | High | Reusing the inventory comparison reports `unknown` for most real submodules and fails above 1,024 records | Plan DD5, FR-005, US2-3, T012: metadata-based tracked state (`diff-index --cached` plus size, mode and presence against `lstat`), a stated `unknown` rate and a per-submodule dirty-scan budget; no inventory refactor |
| 4 | Medium | `for-each-ref --contains` exits 129 for an absent object; symrefs use slots | Plan DD4, FR-006, T013: `cat-file -e` first, `--count=64`, symrefs filtered in Go |
| 5 | Medium | Oversized untracked scan was `unknown` in one place and `pins-limit` in another | FR-011, plan DD5: the dirty-scan budget yields `unknown`; other bounds yield `pins-limit` |
| 6 | Medium | Unborn HEAD and a symlinked HEAD `.gitmodules` undefined | FR-017, T008 |
| 7 | Medium | `--strict` passes when publication is `unknown` | `pin-object-absent` is an error in an initialized submodule; the spec states that uninitialized submodules are not proven by a passing `--strict` run |
| 8 | Low | Non-UTF-8 path cannot be represented | `path_encoding: escaped` in the data model and edge cases |
| 9 | Low | Contract JSON example incomplete | Example now carries identity, limits, staleness and summary |
| 10 | Low | Status 3 plumbing and its standard-error line unspecified | FR-014, plan DD10, T005 |
| 11 | Low | Status and code names differ from inventory and inspect | Recorded choice in research.md |

Also done: `url-mismatch` is named in T009, and the Git measurements of this round are recorded in research.md.

## Round 3

Pending: a narrow independent re-check of the Round 2 resolutions and the cross-artifact consistency. Implementation does not start before it returns `READY`.
