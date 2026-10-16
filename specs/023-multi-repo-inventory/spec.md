# Feature Specification: Multi-repository inventory

**Feature Branch**: `codex/023-multi-repo-inventory`

**Created**: 2026-10-16

**Status**: Implementation of WI1 to WI4 reviewed and corrected (analysis.md, round 5); documentation and acceptance (WI5) in progress

**Input**: A read-only `stw repos` command that inventories several repositories in one bounded run, each through its own native backend, and adds a small cross-repository summary.

## User Scenarios & Testing

### User Story 1 — Inventory several repositories in one run (Priority: P1)

A human or agent works in a workspace that holds several member repositories (Git and jj, some with Git linked worktrees or jj workspaces created in temporary directories). Today they run `stw inventory` once per repository and merge the results by hand.

**Why this priority**: This is the motivating use. The merge step is where agents drop a repository or mistake a failure for a clean result.

**Independent Test**: In temporary directories create a Git repository with a linked worktree, a Git repository with a dirty file, and (jj lane) a native jj repository with a secondary workspace. Run `stw repos PATH...` and compare each entry with `stw inventory PATH --json` for the same path.

**Acceptance Scenarios**:

1. **Given** N explicit repository paths, **When** `stw repos P1 … PN --json` runs, **Then** one result carries N entries in argument order; each entry's embedded `inventory` equals what `stw inventory Pi --json` returns for the same state with the backend's scopes (default scopes for Git, `--scope stored` for jj; modulo sampled facts that changed in between), and the existing inventory schema is not modified.
2. **Given** a Git member and a jj member, **When** the command runs, **Then** each is inventoried by its own backend (selected by ownership markers, never by executable availability) and the entry names the backend.
3. **Given** one member with a stale linked worktree (registered path missing) and one with a foreign-managed workspace (for example a Git linked worktree carrying jj metadata), **When** the command runs, **Then** the summary counts them and the entry facts name the counts; the full workspace records stay in the embedded inventory.
4. **Given** `--json` and no flag, **When** the command runs, **Then** human text and JSON come from the same result.

### User Story 2 — One bad repository never hides or damages the others (Priority: P1)

An agent passes a list that includes a path that is not a repository, a repository with a planted hook, filter, fsmonitor or credential helper, a foreign-owned repository, a repository whose backend is unsupported (GitButler, ambiguous), and healthy repositories.

**Independent Test**: Build that set in temporary directories with effect sentinels in every member; run once; verify per-entry codes, the healthy entries intact, no sentinel fired, and exit status 1.

**Acceptance Scenarios**:

1. **Given** any member that cannot be inventoried, **When** the command runs, **Then** its entry has status `error` and an error code from the vocabulary table; the overall status is `partial` (or `error` when no entry has a usable inventory) and the exit status is 1. The command never reports overall success when an entry failed.
2. **Given** hooks, clean/process filters, external diff, textconv, fsmonitor, credential helpers, aliases or a network-capable remote helper in any member, **When** the command runs, **Then** none runs and no member, superproject or `.gitmodules` file, index, ref, reflog or configuration changes; on jj members the working copy and operation log are unchanged.
3. **Given** a member that panics in its worker or exceeds its deadline, **When** the command runs, **Then** other members are still reported and the failing one carries `member-failed` or `inventory-timeout`; a timeout caused by the `repos` deadline is always reported as `inventory-timeout`, whatever code the underlying operation used.

### User Story 3 — Include a superproject's direct submodules (Priority: P1)

A maintainer lists the umbrella superproject and wants its registered submodules inventoried too, without typing five more paths.

**Independent Test**: A temporary superproject with three registered submodules (one uninitialized, one whose path was replaced by a symlink, one with a name containing a dot and a space) and a linked worktree of it.

**Acceptance Scenarios**:

1. **Given** `--submodules` and a Git-owned superproject path, **When** the command runs, **Then** each path named in the superproject's HEAD `.gitmodules` blob that is also a gitlink in the index is added as an entry with `origin: "submodule"` and its parent index, after all explicit entries, sorted by path bytes.
2. **Given** an uninitialized submodule (no checkout: the path is absent or is a directory without a `.git` entry, whatever else it contains), **When** the command runs, **Then** its entry is `skipped` with `submodule-uninitialized`, no process runs inside it, and it does not change the exit status; the summary counts it.
3. **Given** a registered submodule path that is a symlink, contains a symlink component below the superproject root, or whose `.git` entry is a symlink or other non-regular, non-directory entry or whose pointer leaves the superproject's expected modules location, **When** the command runs, **Then** its entry is `error` with `path-unsafe` before any Git process runs in it, and other submodules are still inventoried. A registered path that is lexically invalid (absolute, containing `..`, starting with `-`, not valid UTF-8, too long) can never be inventoried safely: it is not an entry, it is counted in the parent's `discovery.ignored` and named (escaped) in the parent's `notes` as `discovery-ignored: <path>`.
4. **Given** `--submodules` and a jj-owned path, **When** the command runs, **Then** discovery is `not-applicable` for that entry (jj has no Git submodule model), disclosed in the entry and summary; the entry is still inventoried.
5. **Given** a submodule path that equals an explicit path (or another discovered path) after resolution, **When** the command runs, **Then** it is not added again and is counted in the parent's `discovery.deduplicated`; this is not an error.
6. **Given** a submodule entry, **When** facts are derived, **Then** `dirty` is `unknown` with a reason: Git reports a submodule's Git directory, not its checkout, as its worktree, so the inventory's workspace record does not describe the checkout (measured, research Decision 12). The embedded inventory is still included, with that caveat in the entry `notes`.

### User Story 4 — Read a small cross-repository summary (Priority: P2)

A reviewer wants to see, without reading every inventory, which repositories are dirty, have stale or foreign workspaces or conflicted references, and how many entries failed.

**Independent Test**: Fixture set of US1; compare `summary` and per-entry `facts` with values recomputed from the embedded inventories by the derivation table.

**Acceptance Scenarios**:

1. **Given** a completed run, **When** results are rendered, **Then** every summary and facts value is derived from the embedded inventory by the normative derivation table, never from a second observation.
2. **Given** a jj member, **When** facts are derived, **Then** `dirty` is `unknown` with the inventory's reason, never `false`; the jj entry is inventoried with the `stored` scope only (FR-002), so it can be `ok`.
3. **Given** the request for ahead/behind counts, **Then** the result states that the capability is absent (single-repository inventory has no such observation) and fabricates none.

### User Story 5 — Predictable bounds, ordering and exit status (Priority: P1)

**Independent Test**: Table tests for each limit, a fake inventory operation that blocks, panics or returns oversized results, and `-race` runs.

**Acceptance Scenarios**:

1. **Given** no path, more than 16 explicit paths, or an explicit path that is empty, contains NUL, is not valid UTF-8 or exceeds 4,096 bytes, **When** the command runs, **Then** it exits 2 as a usage error before touching any path. Discovery that would exceed 16 total entries yields `discovery-limit` for that parent and adds none of its submodules.
2. **Given** concurrent execution, **When** results are compared across runs on unchanged repositories, **Then** entry order and content are identical regardless of completion order.
3. **Given** the total deadline expires, **When** entries are unfinished, **Then** started ones report `inventory-timeout`, unstarted ones `inventory-not-started`, and the result is still emitted.
4. **Given** embedded inventories that would exceed the 8 MiB output bound, **When** results are assembled, **Then** later entries drop their embedded inventory, keep their facts and carry `output-limit` (counted in `summary.inventories_omitted`); the result stays valid JSON within the bound.

### User Story 6 — Path validation, duplicates and nesting (Priority: P2)

**Acceptance Scenarios**:

1. **Given** a path that is inside a repository but not its root (including an uninitialized submodule directory, which Git resolves to the superproject), **When** the command runs, **Then** the entry is `error` with `not-repository-root` and no inventory is run or embedded.
2. **Given** two paths that denote the same repository (the same path twice, a symlink alias, a Git main checkout and its linked worktree, a jj workspace and its secondary workspace), **When** the command runs, **Then** the first in order is kept and each later one is `error` with `duplicate-repository` naming the kept entry.
3. **Given** one listed repository located inside another listed repository's worktree, **When** the command runs, **Then** both are inventoried independently and the inner entry carries `contained_in`.

### Edge Cases

- No `.gitmodules` in HEAD, an unborn superproject HEAD, a HEAD `.gitmodules` that is not a regular file, a `.gitmodules` entry without a gitlink (no entry is created and `discovery.ignored` counts it), a gitlink without an entry (not discovered; see FR-009), `.gitmodules` over 64 KiB.
- Path names with spaces, newlines, leading dashes, a missing path, a path to a file or a dangling symlink (entry `invalid-path`), a lexically invalid registered path (counted in `discovery.ignored`, never an entry).
- Explicit symlink to a repository root (accepted, resolved path reported); symlink to a subdirectory (refused `not-repository-root`).
- A repository changing while the run is in progress: each entry inherits the inventory's own change detection; no cross-repository atomicity is claimed.
- Submodules checked out in two worktrees of the same superproject (main and linked), each with its own submodule Git directory: distinct repositories, not duplicates (whereas the main checkout and its linked worktree themselves are duplicates, US6-2).
- A bare repository member (inventoried as inventory does).
- GitButler-managed or ambiguous member: `unsupported-backend` or `ambiguous-backend` for that entry only.
- Missing `git` executable or unsupported Git version: every Git entry carries the inventory's existing code.
- Missing jj executable or sandbox (`jj-sandbox-unavailable`): only jj entries fail, with the inventory's code.

## Requirements

### Functional Requirements

- **FR-001**: Provide `stw repos PATH... [--submodules] [--json]` as a new command with its own schema `schemas/v1/repos.schema.json`. At least one `PATH` is required. The command is read-only and non-interactive: no prompt, editor or pager, and it reads no standard input. `stw inventory` and `schemas/v1/inventory.schema.json` do not change.
- **FR-002**: Inventory every selected repository by calling the existing single-repository inventory operation, without a second implementation of any backend. Each entry is first identified with the existing identity inspection (`Inspect`), which selects the backend from ownership markers, never from executable availability; the options of the inventory call then depend on that backend: Git uses the default scopes of `stw inventory`; jj uses the `stored` scope only, because every other jj scope is unavailable without snapshotting and would make every jj entry `partial` (code reading of `inventory_jj.go`; unverified by running, jj is absent from the authoring container). Git and jj are Tier 1 peers: jj members are fully supported because single-repository inventory supports jj. A capability a backend lacks is stated (FR-011), never replaced by a fallback.
- **FR-003**: Accept 1 to 16 explicit paths (limits table). Structural validation before any process runs: non-empty, valid UTF-8, no NUL, at most 4,096 bytes; a violation is a usage error (exit 2). A path that is not an existing directory is a per-entry `invalid-path` error. Symlinks of an explicit path are resolved for identity; both the given and the resolved path are reported.
- **FR-004**: Require the resolved path to equal the repository's own root as reported by the identity inspection that selected the backend (Git worktree root, or the Git directory of a bare repository; jj workspace root). Otherwise the entry is `not-repository-root` and no inventory is run for it. The comparison uses the identity, not a separately computed path.
- **FR-005**: Detect duplicate repositories by native repository identity (Git common directory; jj repository directory) from the identity inspection, before any inventory runs, and keep the first in expansion order; each later one is a `duplicate-repository` error (the caller asked for N repositories and receives fewer inventories, so it is not silent success). Identical explicit paths are treated the same way without a process. Detect containment (path-component prefix of resolved roots) of one listed root inside another and report the nearest containing entry as `contained_in`, without refusing.
- **FR-006**: With `--submodules`, for each explicit path whose identity inspection reports a Git-owned repository and that passed the root and duplicate checks of FR-004 and FR-005 (never before ownership is known and validated; a jj, GitButler or ambiguous parent, a non-root path or a duplicate starts no Git discovery process) read the direct submodule paths (FR-009), run from the identity's worktree root, and add the valid ones as entries. Do not recurse into nested submodules. A bare repository or an unborn HEAD has no discoverable submodules (`discovery.status` `ok`, zero).
- **FR-007**: Expansion order is the argument order of explicit paths, then discovered submodules grouped by parent in parent order and sorted by path bytes. Output order is expansion order. Concurrency never changes order or content.
- **FR-008**: Discovered paths are untrusted data. Accept one only if it is valid UTF-8, at most 4,096 bytes, relative, without a `..`, empty or `.` component, a trailing slash or NUL, and not starting with `-` (otherwise it is only counted in `ignored`, FR-009), and, when present, has no symlink in any component below the superproject root (checked with `Lstat`, never followed) and is a directory (otherwise `path-unsafe`). An absent path, or a directory without a `.git` entry, is uninitialized (`skipped`); a `.git` entry that is neither a directory nor a regular file is `path-unsafe`. Before any process runs in a member, its location is confined by the Spec 021 safety layer (`pinsSafety`), reused through a small adapter: the `.git` entry is read as data with a bounded no-follow read and must be an in-place directory or a pointer file naming exactly the expected Git directory (the superproject's modules path as Git reports it for that worktree, main or linked, joined with the submodule name, or the configured `submodule.<name>.gitdir` under `extensions.submodulePathConfig`), the directories must be owned by the effective user and not writable by others, `core.worktree` and includes are checked, and a `commondir` entry or a non-empty `objects/info/alternates` file is refused outright (so a submodule that borrows objects through alternates is `path-unsafe` and not inventoried). After the identity inspection, its worktree must equal the expected path and its Git directory the pointer target; otherwise `path-unsafe` and no inventory runs. The `extensions.submodulePathConfig` fixtures of T008 run on Git 2.56 and later only and are skipped on older Git. After every inventory whose embedded identity has status `ok`, its identity is compared with the identity of the earlier inspection (backend, Git `git_dir`, `common_dir`, `worktree` and HEAD; jj `workspace_root`, `repository_dir` and operation id; the jj comparison is code reading, unverified until the jj lanes run); a difference is `changed-repository` with `inventory` and `facts` null (the inventory describes a repository other than the one validated), because `Inventory` re-checks only against its own first inspection. Error inventories usually keep the identity of their own first inspection (measured: a `git-inventory-failed` result embeds `identity.status` `ok` and the full repository object), so the comparison applies whenever the embedded `identity.status` is `ok`, whatever the inventory status; an error inventory with no usable identity (an inner `Inspect` that was canceled or failed, an overflow result) is not compared and keeps its own code. Residual race, disclosed in the limitations: a symlink swapped in after the `Lstat` check can be followed by the identity inspection before the post-check rejects it; isolation neutralizes execution, not reading.
- **FR-009**: Registration is read minimally. `ls-tree -z -l HEAD -- .gitmodules` must show a regular blob (mode 100644 or 100755) of at most 65,536 bytes; a symlink, tree or gitlink or a larger blob is `discovery-failed`, and absence means no submodules. Then `config --blob <oid> --null --list --no-includes`, with the blob object id from that same `ls-tree` record (so the checked blob is the parsed blob), gives, per name, the last `submodule.<name>.path` value (Git uses the last); a value-less key is ignored. At most 64 distinct paths are examined (more gives `discovery-limit`). Each path passing FR-008's lexical rules is confirmed with `ls-files --stage -z -- <path>` (the isolation runner sets `--literal-pathspecs`) and accepted only when a record with mode 160000 and exactly that path is returned; a path with no such record, or whose query exceeds the runner's output cap (a directory with many files), counts in `discovery.ignored`. A gitlink without an entry is not discovered in this version. Registration is read from HEAD and confirmation from the index, so a newly staged submodule is ignored until committed and a staged removal is ignored too (it has no gitlink in the index any more, so it is counted in `discovery.ignored`, not discovered); this is disclosed. The reading is deliberately smaller than Spec 021's registry reading; the overlap and later reuse are recorded in the plan.
- **FR-010**: Isolation: all repository text (paths, names, refs, URLs) is data. Per-member work shares no mutable state; each member runs under its own context derived from the total deadline; a panic or failure in one member is recovered into that entry. Hooks, clean/process filters, external diff, textconv, fsmonitor, credential helpers, aliases, network, fetch, prompts and writes are neutralized exactly as `stw inventory` does (inherited by reuse) and by the same Git isolation runner for discovery commands.
- **FR-011**: Summary and facts are derived only from embedded inventories by the derivation table. Unavailable facts are `unknown` with a reason, never guessed. Ahead/behind counts, publication state and task ownership are not provided (single-repository inventory has none); the result carries a `limitations` entry saying so. Inherited inventory limitations apply (section below).
- **FR-012**: Each entry reports status per the entry status table, with an `error` object using the error-code table. An inherited inventory diagnostic code is reused unchanged. The overall status is `ok` (every entry `ok` or `skipped`), `error` (no entry has a usable inventory: `ok` or `partial` with a non-null embedded inventory) or `partial` (otherwise). When an entry has several causes its `error` follows the precedence list below and the others are added to its `notes`.
- **FR-013**: Bounds per the limits table: 16 entries, 4 workers, 10 seconds per member (the existing inventory deadline, not extended), 60 seconds in total, 8 MiB JSON. Exhausting a bound is reported per entry, never as success. No retry.
- **FR-014**: Human and JSON output come from one result type. Exit status per the exit table. Cancellation or an output write failure exits 1.
- **FR-015**: Effect disclosure: the result carries `effects` stating no network, no fetch, no writes to repositories and no hooks, plus the inherited jj private temporary files outside the target. Conformance proves it with sentinels per member (SC-003).
- **FR-016**: Require failing tests first, hostile-input and race tests, schema fixtures, conformance on the existing minimum and current lanes (Git and jj) and independent review before implementation.

### Limits (normative)

| Name | Value | Notes |
| --- | --- | --- |
| `max_entries` | 16 | explicit plus discovered; more explicit paths is a usage error |
| `max_path_bytes` | 4096 | per explicit or discovered path |
| `max_discovery_candidates` | 64 | distinct `.gitmodules` paths examined per parent |
| `per_repository_seconds` | 10 | per call: each identity inspection, discovery step and inventory runs under its own 10-second deadline (the inventory's is fixed by the operation) |
| `total_seconds` | 60 | whole command, including identity inspections and discovery; a worst case of 16 entries can exceed it, which is then reported per entry |
| `parallelism` | 4 | worker goroutines for identity inspections and for inventories |
| `max_output_bytes` | 8388608 | encoded JSON of the whole result |
| `gitmodules_bytes` | 65536 | size of the HEAD `.gitmodules` blob (checked with `ls-tree -l`); larger gives `discovery-failed` |

### Entry status (normative)

| Status | Meaning | Affects exit |
| --- | --- | --- |
| `ok` | inventory status `ok` and entry checks passed | no |
| `partial` | inventory status `partial` (qualified observations), or discovery failed or hit its limit for this parent | yes (1) |
| `error` | no usable inventory for this entry: identity or inventory error, failed entry check, timeout, omission | yes (1) |
| `skipped` | uninitialized submodule, nothing to inventory | no |

`inventory` and `facts` per entry:

| Case | `inventory` | `facts` |
| --- | --- | --- |
| `ok`, or `partial` from inventory (`inventory-partial`) | embedded | derived |
| `partial` from discovery only (`discovery-failed`, `discovery-limit`) | embedded (status `ok` or `partial`) | derived |
| `error` because the inventory ran and returned status `error` (`git-inventory-failed`, the inventory's own `changed-repository`, `inventory-timeout` or `inventory-limit`, jj errors) | embedded as returned (it is a valid inventory result and may carry the operation's own diagnostics) | null |
| `error` `changed-repository` raised by `repos` itself after comparing the inventory's identity with the earlier identity (FR-008) | null | null |
| `error` from the identity inspection, an entry check, `member-failed`, `inventory-not-started` or a mapped timeout before the inventory ran | null | null |
| `error` with `output-limit` | null | derived before omission |

| `skipped` | null | null |

When the 8 MiB bound would omit an embedded inventory that is itself an `error` inventory, the entry keeps its inherited code, `output-limit` is added to `notes`, `facts` stay null, the inventory is nulled and the omission still counts in `inventories_omitted`.

### Error codes (normative)

Inherited codes come unchanged from the identity inspection's or inventory's first diagnostic (`unsupported-backend`, `ambiguous-backend`, `invalid-path`, `not-repository`, `git-failed`, `git-inventory-failed`, `jj-sandbox-unavailable`, `jj-timeout`, `changed-repository`, `inventory-timeout`, `canceled`, `inventory-limit` (the inventory's own record or byte bound) and the others those operations define).

| Code | Origin | Status | When |
| --- | --- | --- | --- |
| `not-repository-root` | new | `error` | path is inside a repository but not its root (FR-004) |
| `duplicate-repository` | new | `error` | same native repository as an earlier entry (FR-005) |
| `path-unsafe` | new | `error` | discovered path or `.git` pointer fails FR-008 |
| `submodule-uninitialized` | new | `skipped` | registered gitlink with no checkout |
| `discovery-failed` | new | `partial` (on the parent) | reading `.gitmodules` or the index failed, too large, or not a regular file |
| `discovery-limit` | new | `partial` (on the parent) | more than 64 candidate paths, or submodules would exceed `max_entries` (none of that parent's submodules are added) |
| `inventory-partial` | new | `partial` | inventory status `partial`; message is the first boundary limitation added after the fixed sampling statement and not beginning with "multiple base candidates" (partial inventories carry no diagnostic; that base-candidate limitation is informational and appears on ordinary clones); every limitation after the sampling statement is also added to `notes` as `inventory-partial: <limitation>` |
| `member-failed` | new | `error` | recovered panic or internal failure of one worker |
| `inventory-not-started` | new (the inventory operation only uses this string as an internal placeholder) | `error` | `repos` deadline or cancellation before the entry's identity inspection or inventory started |
| `output-limit` | new | `error` | embedded inventory omitted because the 8 MiB result bound would be exceeded (`facts` kept) |

Timeout mapping: any `canceled`, `jj-timeout` or `inventory-timeout` of an identity inspection, discovery step or inventory is reported as `inventory-timeout` when the caller's context is not done; its message names the bound that fired (the 60-second total when the `repos` deadline fired, otherwise the 10-second per-call bound), never the inventory's fixed text. This covers `Inspect` rewriting any deadline to `canceled`. When the caller's context is canceled the code is `canceled`. Every identity inspection, discovery step and inventory runs under its own 10-second per-call deadline inside the total.

Precedence when several causes apply (first wins; the others go to `notes`): `path-unsafe`, `not-repository-root`, `duplicate-repository`, inherited identity or inventory error, `inventory-timeout`/`canceled`/`inventory-not-started`, `member-failed`, `output-limit`, `discovery-failed`, `discovery-limit`, `inventory-partial`.

### Inherited inventory limits and their effect (normative disclosure)

Reusing `Inventory` inherits its behavior, measured in the review round on Git 2.56.0 and recorded in research Decision 12: the Git inventory reads content attributes for every cached path with one `check-attr` call whose output the runner caps at 64 KiB, so a repository with roughly 150 to 400 tracked files (depending on path length) gives `git-inventory-failed` ("cannot safely observe content attributes") even without any `.gitattributes` (measured by the round-4 reviewer on Git 2.56.0: 400 short-path files `ok`, 250 files with about 60-byte paths `error`, a 442-file tree `error`); more than 1,024 aggregate records gives `git-inventory-failed` or `inventory-limit`; a jj inventory fails with `jj-output-limit` when any single jj command's output exceeds 64 KiB (code reading, unverified: jj is absent from the authoring container; roughly several hundred changed files or many bookmarks, below the 1,024-record limit); a repository with registered gitlinks and a bare repository are always `partial` under the Git default scopes ("submodule disk state excluded", "bare repository has no disk or index checkout scopes"); a stale linked worktree makes its repository `partial`. Consequences, accepted and disclosed: a workspace of real, large or submodule-bearing repositories mostly yields `partial` or `error` entries and exit status 1, with the cause per entry; exit 0 means every member was fully observed. Lifting these limits is a change to single-repository inventory and a separate specification.

### Facts derivation (normative)

Derived from the embedded inventory `I` of an entry with non-null `inventory` before any output-bound omission. The *root workspace* is the `I.workspaces` record whose `path` equals the entry's resolved root; when no record matches, `dirty` is `unknown` with a reason (always the case for a submodule checkout, see US3-6).

| Fact | Git member | jj member |
| --- | --- | --- |
| `head_state`, `head_ref`, `head_commit_id` | from `I.identity.repository.head` (`attached`, `detached`, `unborn`) | `jj-workspace`, `null`, `I.identity.jj_repository.commit_id` |
| `change_id` | `null` | `I.identity.jj_repository.change_id` |
| `dirty.value` | root workspace: `true` if tracked or untracked is `true`; `false` if both are `false`; else `unknown` (reason from the observation) | `unknown`, reason from the root workspace limitation (disk freshness needs a snapshot) |
| `conflicted_references` | references with at least two `added_targets` or any `removed_targets` | same rule |
| `workspaces.total` | `len(I.workspaces)` | same |
| `workspaces.stale` | registration `missing` | same |
| `workspaces.foreign` | `manager` differs from the entry backend and registration is not `missing` (Git leaves registration `unknown` for foreign or unconfirmed workspaces) | same |
| `workspaces.unverified` | registration `unknown` and not counted as foreign | same |

The summary sums these over entries whose `facts` is non-null (so omission by the output bound never changes it), counts entries by status and backend (`other` includes `unknown`), skipped submodules, explicit entries whose discovery is `not-applicable` (a non-Git parent or an entry that failed identity or validation) and omitted inventories.

### Exit status (normative)

| Status | Meaning |
| --- | --- |
| 0 | overall `ok`: every entry `ok` or `skipped`. Dirty state and counts are facts and do not change the status; a stale workspace makes that repository's own inventory `partial`, hence exit 1 |
| 1 | overall `partial` or `error`, cancellation, or an output failure |
| 2 | usage error (no path, more than 16 paths, structurally invalid path, invalid flag); no JSON is guaranteed |

Precedence: 2, then 1, then 0. There is no status 3; a strict mode is out of scope.

### Key Entities

- Repository entry: given and resolved path, origin, parent, backend, status, error, discovery, containment, facts, embedded inventory.
- Cross-repository summary: counts derived from entries.
- Boundary: effects, limits, completeness, limitations.

## Success Criteria

- **SC-001**: A fixture workspace (Git default scopes, jj `--scope stored`) of five members (two Git, one with a linked worktree and one with registered submodules, two jj in the jj lane) plus one Git member above the inventory record limit yields one result whose entries match per-repository `stw inventory` output and whose summary matches the derivation table.
- **SC-002**: Every acceptance scenario and edge case has a deterministic test; a skipped or unavailable lane never counts as acceptance.
- **SC-003**: Per-member effect sentinels (refs, HEAD, index bytes and modification time, reflogs, configuration, worktree files, no lock files, hook/filter/fsmonitor/helper and network sentinels; for jj the working copy and operation log) prove no member, superproject or `.gitmodules` changed, with a hostile member in the set.
- **SC-004**: Success, partial, error and omitted-inventory fixtures validate offline against `repos.schema.json`, each embedded inventory also validates against `inventory.schema.json`, and existing contract fixtures remain valid.
- **SC-005**: Existing minimum and current Git/jj lanes on ubuntu-24.04 amd64 pass; arm64 and darwin results are local evidence only.

## Scope and Dependencies

Depends on the implemented single-repository inventory (Spec 009) and native jj inspection (Spec 008). Spec 021 (submodule pin audit) is merged: its safety layer (`pinsSafety`, `pinsRunner`) is reused for submodule confinement and the isolation of discovery commands (FR-008, FR-010), while its richer registry reading is not used and may later replace FR-009's minimal reading. Spec 022 and hosting are untouched. Out of scope: ahead/behind or publication counts, recursion into nested submodules, discovery from a jj superproject, any mutation, a strict exit mode, standard-input or file path lists, remote selection (`--remote`) passthrough. Constitution 1.1.0 applies with no amendment.
