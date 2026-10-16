# Research: Multi-repository inventory

Date: 2026-10-16. Git used for every measurement below: **git version 2.56.0** (linux arm64, container). Git 2.46 (minimum lane) was not available here; every Git claim is therefore a 2.56.0 measurement, to be re-run on both lanes by the conformance tasks. jj is **not installed** in this container (`which jj` finds nothing; `bwrap` exists): every jj statement is documentation or code reading and is marked unverified. Scratch repositories lived under the session scratchpad, not in any real repository.

## Decision 1: new command, not an extension of `stw inventory`

Ratings are out of 5 (5 best). Criteria: contract stability (existing JSON and exit contract unchanged), bounded output, isolation of failures, clarity for humans and agents, implementation cost.

| Option | Stability | Bounds | Isolation | Clarity | Cost | Total /25 |
| --- | --- | --- | --- | --- | --- | --- |
| A. Extend `stw inventory` with several PATH arguments | 1 | 2 | 3 | 2 | 3 | 11 |
| B. New command `stw repos` embedding per-repository inventories (chosen) | 5 | 5 | 5 | 5 | 4 | 24 |
| C. New command with its own flattened records (no embedding) | 4 | 4 | 4 | 3 | 1 | 16 |
| D. Shell loop or wrapper script in docs only | 5 | 1 | 2 | 1 | 5 | 14 |

Rationale. `inventory` takes one PATH, its schema pins `identity`, `workspaces`, `references` and one `status` for one repository, and its exit status is derived from that one status (`internal/cli/inventory.go`). Accepting several paths would need a changed result shape (an array or a new status), which breaks "the existing contract stays unchanged". Option C re-implements inventory facts and would drift. Option B reuses the operation as is, so an embedded entry is the same observation a caller would get from `stw inventory`. Name: `repos` is short, parallels `pins`, and is not tied to inventory flags. Alternatives `inventory-all`, `survey` and `census` were considered; `repos` won on discoverability. Cost of B is a new schema and a new CLI file, accepted.

## Decision 2: embedding, and why the schema cannot `$ref` inventory

`internal/contracts/contracts_test.go` (`checkLocalReferences`) fails any schema with a non-local `$ref`. So `repos.schema.json` cannot reference `inventory.schema.json`. Options rated: (1) copy inventory's `$defs` into the new schema (drift risk, 2/5); (2) type the embedded value loosely (`schema_version` 1, `command` "inventory", status and backend enums) and have the contract test validate every fixture's embedded value against the real inventory schema (5/5 for drift, 4/5 for published strictness; chosen); (3) flatten (rejected with Decision 1 C). Consumers validate `inventory` against the published inventory schema by `$id`.

## Decision 3: concurrency

Sequential: worst case 16 x 10 s = 160 s, beyond any sensible total. Unbounded parallelism: a process storm (every Git inventory starts many Git processes and every jj inventory a sandbox). Chosen: 4 workers, results slotted by index, so completion order never leaks. Worst case with 16 entries is 4 rounds of 10 s = 40 s, under the 60 s total deadline. Code reading: `internal/inspection` has no package-level mutable state apart from sentinel errors (searched for package `var` declarations), so concurrent calls share nothing in Go; a `-race` test is still required. Whether concurrent jj inventories sharing one repository (two workspaces of one repo) interfere is **unverified** (no jj here); duplicate detection (Decision 6) removes the same-repository case for inputs the command can see, and the conformance task runs two jj members in parallel. Rating: parallel 4 = 4/5, sequential = 2/5, unbounded = 1/5.

## Decision 4: facts, and no ahead/behind

The inventory result has no ahead/behind counts (searched `internal/inspection/inventory*.go`: none) and `local_only` is `unknown` unless remotes are selected. Adding counting would extend single-repository inventory, which is a different spec. Equivalent facts that do exist and are derived without a second observation: head state, dirty (Git only; jj disk freshness is unknown without a snapshot, documented in `docs/inventory.md`), registration `missing` (stale), manager differing from the backend (foreign), conflicted references (jj bookmark targets). Recorded as an open question with a recommendation (Open questions in plan.md).

## Decision 5: embedded size

A single inventory can be 1 MiB. 16 of them is 16 MiB in memory and output. Chosen: 8 MiB bound with omission of later entries' embedded inventory (facts kept, `inventory-limit`). Rejected: shrinking inventories (changes their contract), a facts-only default (loses the point of reusing inventory), lowering to 4 entries (too few for the motivating workspace plus submodules). Rating of chosen: 4/5.

## Decision 6: duplicate and nesting identity

Measured (Git 2.56.0): a main checkout and its linked worktree report the same `--git-common-dir` (`.../super/.git`) but different worktree roots, so the common directory is the duplicate key. A submodule checked out in a linked worktree of its superproject has its Git directory under `.git/worktrees/<id>/modules/<name>`, distinct from the main worktree's `.git/modules/<name>`: not a duplicate. jj: the inventory identity exposes `repository_dir` (code reading, `internal/inspection/result.go`); that a secondary workspace shares it is documented ("multiple working copies backed by a single repo", docs.jj-vcs.dev working-copy page) and **unverified by measurement**. Nesting is by resolved-path prefix; an untracked nested repository (measured: `git -C super/nested rev-parse --show-toplevel` prints `super/nested`) is its own root.

## Decision 7: path validation

Measured (Git 2.56.0):
- `git -C member1/subdir rev-parse --show-toplevel` prints the member root: a subdirectory path would silently inventory the enclosing repository, hence FR-004 (root check).
- In a clone without `--recurse-submodules`, `git -C clone/member1 rev-parse --show-toplevel` prints the **superproject** (the empty submodule directory is not a repository): without the root check an uninitialized submodule would be inventoried as the superproject, a mislabeled duplicate.
- A symlink to a repository subdirectory resolves to the enclosing repository root; and a registered submodule path replaced by a symlink to another repository (`member1 -> ../sub2`) makes `git -C member1 rev-parse --show-toplevel` print the other repository's path. A discovered path must therefore be checked with `Lstat` per component, and the root compared with the expected path.
- With `GIT_TEST_ASSUME_DIFFERENT_OWNER=1`, discovery in a member fails with "detected dubious ownership" (so a foreign-owned member yields the inventory's `git-failed` entry, not a result). Unlike Spec 021, this command never uses an explicit `--git-dir`, so Git's own ownership check applies.
Explicit paths may traverse symlinks (the caller names them) but are reported with both the given and the resolved path; the safety boundary for explicit paths is the root equality check. Rating of "reject any symlink in explicit paths": 2/5 (breaks `/tmp` aliasing on macOS); chosen rule 4/5.

## Decision 8: minimal submodule discovery (overlap with Spec 021)

Measured (Git 2.56.0), commands run with the existing isolation shape (`GIT_OPTIONAL_LOCKS=0`, `--no-replace-objects`, `core.fsmonitor=false`, cleared `GIT_*`):
- `git config --blob HEAD:.gitmodules --null --list --no-includes` lists `submodule.<name>.path` keys; a name with dots (`a.b.c`) and a path with a space (`dir/member 2`) parse by taking the last `.path` suffix and the NUL-separated value.
- The same command exits 128 for an absent blob, an unborn HEAD and a symlink blob, indistinguishable from other failures, so existence and mode are checked first with `git ls-tree -z HEAD -- .gitmodules` (exit 0, empty output when absent; exit 128 on an unborn HEAD, which is skipped using the identity's head state). Git 2.56.0 itself refuses to `git add` a symlink `.gitmodules` (`invalid path`); a mode-120000 blob could still be crafted, and is refused by the mode check (not measured with a crafted blob).
- `git ls-files --stage -z -- ':(literal)<path>'` prints `160000 <oid> 0\t<path>` for a gitlink, nothing (exit 0) for a path with no entry, including `-opt`, and exits 128 for `../../etc` ("outside repository"). Hostile `.gitmodules` values (`../../etc`, `/abs`, `-opt`, `a/../b`) were read back verbatim; the lexical rules of FR-008 reject them before any pathspec is used.
- Index bytes, SHA-1 and modification time of the superproject index were identical before and after both commands; no `.lock` file remained.
- Output cap: the shared runner caps each stream at 64 KiB (`internal/inspection/git.go`), which is why discovery avoids `ls-files --stage` over the whole index (it can exceed 64 KiB) and queries one literal path at a time.
- Linked worktree of a superproject: the submodule pointer resolves to `.git/worktrees/<id>/modules/<name>` and `rev-parse --absolute-git-dir` in the superproject worktree gives `.git/worktrees/<id>`, so "modules path = superproject git_dir + /modules" holds for main and linked worktrees (2.56.0 measured; `extensions.submodulePathConfig`, new in 2.56, was not exercised here and is a named fixture on the current lane).
Overlap with Spec 021: 021 enumerates gitlinks recursively from HEAD tree and index and reads `.gitmodules` from three sources. This spec reads only HEAD `.gitmodules` paths confirmed by the index, a strict subset. Later reuse: when 021's registry reader exists on main, replace `repos_discover.go` with it and add gitlink-without-entry discovery; the contract of this spec (entries, codes) does not change. Alternatives: depend on 021 (blocked on its merge, rejected); `git submodule status` (spawns `git submodule` machinery, human text, rejected); `ls-tree -r HEAD` (64 KiB cap, rejected).

## Decision 9: stale and foreign workspaces

From code reading: Git inventory marks a registered worktree path that does not exist `registration: "missing"` and makes the inventory `partial`; a workspace whose ownership metadata is foreign gets a manager other than `git` (`jj`, `unknown` or a backend name) and a limitation. Measured: after deleting a linked worktree directory, `git worktree list --porcelain -z` reports it `prunable gitdir file points to non-existent location`. jj inventory marks `registration: "missing"` from `os.Stat` and `manager: "jj"` only when the workspace safely matches (code reading; unverified by running).

## Decision 10: process and effect claims reused, not re-measured

The no-hook, no-network, no-write properties of single-repository inventory are those of Spec 009's verification and are reused by calling the same operation. They are not re-measured here (building and running the binary was not authorized in this session); the conformance tasks re-measure them for the combined command with per-member sentinels.

## Decision 11: names and status vocabulary

Statuses `ok`, `partial`, `error` match inventory. A new `skipped` entry status exists only for an uninitialized submodule (a gitlink with no checkout has no repository to inventory; Spec 021 treats it as information). New codes are kebab-case. Alternatives (`refused` status, per-repository exit codes) rejected for vocabulary growth.

## Open measurements for implementation

- Behavior of the Git runner under `-race` with four concurrent inventories (race test).
- jj: two workspaces of one repository inventoried concurrently; secondary workspace `repository_dir` equality; `jj-sandbox-unavailable` entry next to Git entries (unverified).
- Git 2.46 behavior of the commands above (minimum lane).
- A crafted mode-120000 `.gitmodules` blob and `extensions.submodulePathConfig` pointers.
