# Feature Specification: Inventory limits and checkout description

**Feature Branch**: `codex/027-inventory-limits`
**Created**: 2026-10-16
**Status**: Draft — first draft; independent analysis pending; implementation not started
**Input**: A follow-up to Spec 009 that makes `stw inventory` useful on ordinary Git repositories. Decision by the maintainer (2026-10-16): fix the inventory prerequisite before `stw repos` (Spec 023) ships.
**Priority rating**: 10/10 (user-selected product priority, not verification evidence)

## Problem (measured, see [research.md](research.md))

On Git 2.56.0 the current inventory returns `error` for a repository with 400 tracked files (`git-inventory-failed`, "cannot safely observe content attributes": one `check-attr` output above the shared runner's 64 KiB cap), for 1,030 files ("inventory record limit exceeded": the index is counted against the 1,024 *returned*-record limit), for 5,000 and 50,000 files (`ls-files --cached` above 64 KiB) and, for the `stored` scope alone, as soon as `ls-files --stage` exceeds 64 KiB (about 950 files). A repository with 512 tags is an `error`, with 700 tags `inventory-limit`, with 1,100 tags `git-inventory-failed`. A clean superproject with initialized submodules is always `partial` with `tracked: unknown`, even when ordinary files changed. A clean submodule checkout reports `tracked: "true"` because the only workspace record `git worktree list` gives is the submodule's Git directory, which has no files, and a dirty file inside the checkout is not seen.

## User Scenarios & Testing

### User Story 1 — Get facts for an ordinary repository (Priority: P1)

A human or agent inventories a repository with thousands of tracked files and a few hundred tags and gets facts, not `git-inventory-failed`.

**Why this priority**: Without it most real repositories are `error` with no facts, and every consumer (including Spec 023) inherits that.

**Independent Test**: In temporary Git repositories with 150, 400, 1,030 and 5,000 tracked files (short and 200-byte paths) and with 700 and 1,100 tags, run the default inventory and `--scope stored`; compare against the declared outcome and against the facts of an equivalent small repository.

**Acceptance Scenarios**:

1. **Given** a clean repository of 5,000 tracked files, **When** `stw inventory` runs with the default scopes, **Then** status is `ok`, `tracked` is `false`, no file record exists and the run finishes within the 10-second deadline.
2. **Given** the same repository with one tracked file modified and one untracked file, **When** it runs, **Then** exactly those two file records are reported with the same classification a 20-file repository gives.
3. **Given** any tracked file with a configured clean filter or `text`/`eol`/`ident`/`working-tree-encoding` attribute among thousands of files, **When** it runs, **Then** the existing transformation qualification applies (`tracked` unknown, files qualified) exactly as for a small repository, and no filter, textconv or helper runs.
4. **Given** `--scope stored` on a repository whose root commit has 5,000 files, **When** it runs, **Then** the stored records are reported up to the record bound with the disclosure of the degradation table.
5. **Given** a repository with 1,100 tags, **When** it runs, **Then** local branches and remote-tracking references are listed first, tags are omitted last with a disclosure, status is `partial`, and `--query ref-exists` for an omitted tag still answers `true`.

### User Story 2 — See honestly when a bound is reached (Priority: P1)

A caller learns that a listing is a bounded sample, and never reads an omission as clean.

**Why this priority**: Constitution principles II and IV and Spec 009 FR-008: truncated evidence never becomes success or permission.

**Independent Test**: Temporary repositories with 3,000 modified files, 3,000 untracked files, a 600-file index limit injected by the test, and an injected scan allowance of a few KiB.

**Acceptance Scenarios**:

1. **Given** more changed files than the record share allows, **When** a plain inventory runs, **Then** the first records in path order are returned, status is `partial`, `boundary.complete` is `false`, and `boundary.limitations` states how many records were omitted and that omitted paths are not clean.
2. **Given** the same state and `--query dirty`, **When** it runs, **Then** the predicate is `true` and the exit status is 0; the truncation is disclosed in `boundary.limitations` only.
3. **Given** `--query clean` and one qualifying file record, **When** it runs, **Then** the predicate is `false`, never `unknown`, even though the listing may be truncated.
4. **Given** an index above the per-workspace index limit, **When** a disk scope is requested, **Then** `tracked` is `unknown` with the index-limit reason and status is `partial`; nothing is reported clean.
5. **Given** a streamed command whose output exceeds its scan allowance, **When** it runs, **Then** the command is stopped, the observation it feeds follows the feed table, and no Git process is left running.
6. **Given** a result that would exceed the 1 MiB result bound, **When** it is assembled, **Then** the file listing is trimmed to fit and disclosed instead of the whole result becoming `inventory-limit`.

### User Story 3 — Describe the checkout, not its Git directory (Priority: P1)

A caller inventories a submodule checkout or a repository created with `--separate-git-dir` and gets facts about the files on disk.

**Independent Test**: A superproject with an initialized submodule and a repository with a separate Git directory, clean and with a modified tracked file, an untracked file and a linked worktree.

**Acceptance Scenarios**:

1. **Given** a clean submodule checkout, **When** inventory runs in it, **Then** the workspace record has the checkout path, `tracked` is `false` and `untracked` is `false` (today `tracked` is `"true"`).
2. **Given** a modified tracked file and an untracked file inside the checkout, **When** inventory runs, **Then** both are reported with their checkout-relative paths.
3. **Given** inventory run in a linked worktree of a submodule, **When** the main entry is the Git directory and the main checkout cannot be identified from the identity, **Then** that entry is reported with registration `present`, all observations `unknown` with the Git-directory reason, and status `partial`; no observation is taken from the Git directory.
4. **Given** a bare repository or an ordinary repository, **When** inventory runs, **Then** the records are unchanged.

### User Story 4 — Report dirty ordinary files in a superproject (Priority: P1)

A caller inventories a superproject with registered submodules and learns that its ordinary files changed.

**Independent Test**: A superproject with two initialized submodules, a linked worktree, and clean and modified ordinary files, staged and unstaged, and a staged gitlink change.

**Acceptance Scenarios**:

1. **Given** a modified ordinary tracked file, **When** inventory runs, **Then** `tracked` is `true` for that workspace, with a limitation that submodule disk state was not observed, and `--query dirty` is `true` with status `ok` and exit 0.
2. **Given** a staged change of a gitlink (pin bump), **When** inventory runs, **Then** `tracked` is `true` (staged evidence is read from the index and HEAD tree, not from the submodule).
3. **Given** a clean superproject with registered gitlinks, **When** inventory runs, **Then** `tracked` stays `unknown` with the existing reason ("submodule disk state excluded to avoid recursive helpers") and status is `partial`: `false` cannot be proven without opening the submodule. `--query clean` is `unknown`.
4. **Given** an untracked file in a superproject with gitlinks and `--query dirty --scope untracked`, **When** it runs, **Then** gitlinks are irrelevant to the selected scope and the result is as before this feature (`ok`).
5. **Given** hostile submodule content (hooks, filters, fsmonitor configured inside the submodule), **When** inventory runs in the superproject, **Then** no process runs in or opens the submodule's worktree.

### User Story 5 — Keep every effect guarantee and every contract (Priority: P1)

**Independent Test**: Existing Spec 009 hostile and effect fixtures, extended to the streaming runner; schema validation of real outputs; the Git minimum-lane run (Git 2.47.3 here; CI minimum lane).

**Acceptance Scenarios**:

1. **Given** planted filters (clean, process, smudge), textconv, external diff, hooks and `core.fsmonitor`, **When** inventory runs on a large repository, **Then** no sentinel is created, the index bytes and modification time, refs, reflogs, configuration and worktree files are unchanged, no `index.lock` or other lock file exists, and no Git process outlives the call, including after an early stop.
2. **Given** every real output of the new tests, **When** it is validated, **Then** it validates against the unchanged `schemas/v1/inventory.schema.json`, and all existing fixtures remain valid.
3. **Given** the CLI, **When** it runs, **Then** exit statuses and flags are unchanged (0 ok or known predicate, 1 partial/error/unknown, 2 usage).
4. **Given** a jj-owned repository, **When** inventory runs, **Then** its behavior is unchanged (jj limits are out of scope and stated).

## Edge Cases

- Paths with newlines, leading dashes, glob syntax, non-UTF-8 bytes (the existing rejection of undecodable records applies to streamed records) and 4,096-byte paths in the attribute and index streams.
- A record longer than the stream record limit; output without a terminating NUL; a process that prints nothing and blocks (per-command and total deadline); a process killed on early stop (no lock file, no index write).
- Index changes between the two comparison passes (unchanged `changed-repository`), references changing during a streamed read (digest compared).
- Unborn HEAD with 5,000 staged files (diff `--cached` against the empty tree); a conflicted index; sparse checkout and skip-worktree entries (existing behavior, untouched); `core.autocrlf` set (existing early return of attribute check).
- A repository of 100,001 index entries (limit), of 1,024 workspaces (unchanged error), of 20,001 references (error).
- A worktree whose registered path vanished during the run (unchanged).
- A submodule checkout whose `core.worktree` was edited to point elsewhere: the identity's `rev-parse --show-toplevel` is trusted only after the existing `inspect()` re-check of the same common directory; an unconfirmed path leaves the entry qualified, never described.
- A linked worktree of the superproject inspected while a submodule inside it is uninitialized (a directory without `.git`): the gitlink is a gitlink; nothing is opened.

## Requirements

### Functional Requirements

Tier 1 statement: Git inventory changes as specified; jj inventory is unchanged and keeps its limits (a jj command printing more than 64 KiB still fails; unverified here because jj is not installed in the authoring container). The two backends do not gain equivalent limits; the gap is stated in the contract, not hidden (constitution V).

- **FR-001**: Every Git command whose output grows with repository size is read incrementally (a streaming runner with the same isolation as the shared runner) instead of through the 64 KiB-capped buffered runner. The streamed commands are listed in the feed table. Buffered commands keep the 64 KiB cap.
- **FR-002**: The attribute check no longer passes paths as arguments and no longer reads its whole output. It feeds the cached paths on standard input to `git check-attr --stdin -z` and reads the output incrementally, stopping at the first value that makes comparisons non-authoritative (the existing decision rule, unchanged).
- **FR-003**: The 1,024 record limit applies to returned records only (`boundary.limits.records` stays 1024). It is not a limit on how much of the index, the attribute output, the stored diff or the untracked listing is examined. Examination is bounded by LIM-3 to LIM-9 of the limits table.
- **FR-004**: Exceeding a bound degrades as the degradation table says: partial with a disclosure wherever omission cannot make a false claim; an error only where the section cannot be truncated safely. A degraded result is never `ok` unless the degradation table says the omitted records are supporting evidence of a known predicate.
- **FR-005**: Reference listings are bounded by the fair-share rule; predicates (`ref-exists`, `ancestor`) and base and upstream resolution use the complete scanned reference list, never the truncated listing. Per-reference ancestry facts are computed for returned references only.
- **FR-006**: The workspace entry that `git worktree list` gives as the Git directory of a checkout (submodule, `--separate-git-dir`) is replaced by the checkout path when the inspected path is that checkout (not linked) and the identity gives its worktree; the existing identity re-check applies. Otherwise the entry is reported with registration `present`, every observation `unknown` and the Git-directory reason, and nothing is read from the Git directory.
- **FR-007**: For a workspace with gitlinks, `tracked` is `true` when a staged change or an authoritative ordinary-file disk difference exists; it is otherwise `unknown` with the existing reason. The submodule worktrees are never opened. For `clean`/`dirty` queries a gitlink-only incompleteness does not block a `true` dirty answer when a non-`unknown` file record exists.
- **FR-008**: Keep all effects: no filter, textconv, external diff, hook, fsmonitor, credential helper, alias, network, prompt or write; no index refresh or lock file; stopping a stream early leaves no process and no lock; ownership checks of Git's discovery (safe.directory) stay in force (no explicit `--git-dir` is introduced).
- **FR-009**: Published contracts stay valid: `schemas/v1/inventory.schema.json` and the constants in `boundary.limits` are byte-unchanged; no new diagnostic code, property or enum value is added; every new disclosure is text in existing string fields. Each behavior change is recorded in the compatibility table of [contracts/inventory-limits.md](contracts/inventory-limits.md) and in docs/inventory.md.
- **FR-010**: Disk comparison keeps its 64 KiB per-file content read. A per-workspace-pass content read budget (LIM-11) makes the remaining files `unknown` with a reason instead of letting the deadline decide.
- **FR-011**: The two comparison passes and the final registration/reference rechecks remain; the index and reference rechecks compare digests of the streams. A change is `changed-repository` as today.
- **FR-012**: The total deadline stays 10 seconds; each streamed command also observes the caller's context; an exceeded deadline remains `inventory-timeout`.
- **FR-013**: Failing tests precede implementation. Real-Git fixtures at the scales above, hostile fixtures with effect sentinels, schema validation of real outputs, the Git minimum lane, and `go test -race -count=3 -shuffle=on`. Tests do not depend on inode reuse, mtime ties, umask (TestMain sets 022), TMPDIR symlinks or global Git configuration.
- **FR-014**: List the Spec 023 artifacts that must change when this lands (tracked in [plan.md](plan.md) and tasks, not changed by this spec).

### Normative tables

The limits, the feed table, the degradation table, the disclosure strings, the gitlink rule and the compatibility table are defined once, in [contracts/inventory-limits.md](contracts/inventory-limits.md); the data model and tasks reference them by identifier (LIM-n, FEED-n, DEG-n, TXT-n, GL-n, CK-n, COMPAT-n) and do not restate values.

## Key Entities

- Streamed command: a Git invocation read record by record under LIM-3/LIM-4.
- Fair-share allocation: the rule that splits the returned-record budget between files and references.
- Checkout entry: the workspace record that stands for the files on disk of a checkout whose Git directory is elsewhere.

## Success Criteria

- **SC-001**: Repositories with 150, 400, 1,030 and 5,000 tracked files (short and long paths) and 1,100 tags return `ok` (clean) or the declared `partial`, never `git-inventory-failed`, within the deadline (US1).
- **SC-002**: Every row of the degradation table and feed table has a deterministic test; a missing or skipped test does not count as a pass (US2).
- **SC-003**: A clean submodule checkout has `tracked: "false"`, and a dirty one reports its files (US3). A superproject with a modified ordinary file has `tracked: "true"`; a clean one is `unknown` (US4).
- **SC-004**: Effect sentinels and hostile fixtures pass on the large fixtures and after early stops (US5-1).
- **SC-005**: Every real output validates against the unchanged schema; schema bytes and all existing contract fixtures are unchanged (US5-2).
- **SC-006**: Git 2.47.3 (the minimum-lane stand-in available here) and 2.56.0 (current) pass; the CI minimum and current lanes are the recorded acceptance evidence; arm64 results are local evidence.
- **SC-007**: The list of Spec 023 changes is complete and each item names an artifact and a section.

## Scope and Dependencies

In scope: the Git inventory adapter (`internal/inspection/inventory_git.go` and a new streaming helper), the documents named above, the unchanged-schema validation. Out of scope: the jj inventory; raising the 64 KiB per-file read; observing submodule worktree state (Spec 021 `stw pins` already does, from index metadata); changing the schema, records limit or deadline constants; Spec 023 changes; tidy (Spec 011), which has its own collectors. Depends on Spec 009 (merged) and reads Spec 021's streaming-runner design (not its code: the pins runner passes explicit `--git-dir`, which bypasses Git's ownership check). No amendment to constitution 1.1.0.
