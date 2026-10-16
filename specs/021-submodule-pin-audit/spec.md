# Feature Specification: Submodule pin audit

**Feature Branch**: `codex/021-submodule-pin-audit`  
**Created**: 2026-10-16  
**Status**: Draft — specification revised after independent analysis; implementation pending  
**Input**: A read-only `stw pins` command that reports, for a Git superproject, what each submodule pin records, what is checked out, and whether the recorded commit is published to a remote the local repository knows.  
**Priority rating**: 10/10 (user-selected product priority, not verification evidence)

## User Scenarios & Testing

### User Story 1 — Find a pin that is not published (Priority: P1)

A human or agent is about to rely on a superproject commit whose submodule pin may point at a commit that exists only in a local submodule checkout. A fresh clone of that superproject could not fetch the pinned commit.

**Why this priority**: This is the failure that motivated the feature. A pin to a commit that was never pushed passes every local check and breaks only for the next person or agent.

**Independent Test**: In temporary repositories, create a superproject with submodules whose "remote" is a local bare repository. Pin one submodule to a commit that was never pushed and another to a pushed commit; compare the reported publication evidence.

**Acceptance Scenarios**:

1. **Given** a pin whose commit is reachable from a remote-tracking ref of the submodule repository, **When** `stw pins` runs, **Then** publication is `reachable` and names the refs that contain it.
2. **Given** a pin whose commit exists locally but is reachable from no remote-tracking ref, **When** `stw pins` runs, **Then** publication is `unreachable`, the finding `pin-unpublished` has error severity, and the text says only that local evidence shows no remote contains it, not that the remote lacks it.
3. **Given** a pin whose commit object is absent from the local submodule repository or whose submodule is not initialized, **When** `stw pins` runs, **Then** publication is `unknown` with the reason, never `reachable` or `unreachable`.
4. **Given** remote-tracking refs that may be stale, **When** results are reported, **Then** the result discloses that no fetch was made and reports the advisory last-fetch time when it can be read.

### User Story 2 — Compare pin, staged pin and checkout (Priority: P1)

A reviewer sees where the recorded pin, the staged pin and the checked-out submodule commit disagree, and whether the checkout has uncommitted work.

**Independent Test**: Build temporary superprojects with a staged pin change, a checkout that is ahead, behind or diverged from the pin, a dirty submodule and an uninitialized submodule.

**Acceptance Scenarios**:

1. **Given** a pin staged in the index that differs from the commit recorded at HEAD, **When** `stw pins` runs, **Then** both commits are reported and `staged-pin-differs` is raised.
2. **Given** a checkout equal to, ahead of, behind or diverged from the HEAD pin, **When** `stw pins` runs, **Then** the relation is reported with bounded counts, or `unknown` when the objects needed to decide are missing.
3. **Given** tracked changes or untracked files inside a submodule checkout, **When** `stw pins` runs, **Then** they are reported as dirty state without any file content. Tracked state is decided from the index and file metadata and never by hashing content: a staged difference, or a different size or mode, or a missing file is `present`; identical metadata is `absent`; a file whose only difference is its modification time, change time or inode, a racily clean entry, an entry with skip-worktree, assume-unchanged, intent-to-add or a non-zero stage flag, a gitlink entry, a size-0 smudged entry, an unparseable index metadata record, or a dirty scan that exceeds its own budget is `unknown` with a reason. Exceeding the dirty-scan budget makes that value `unknown` and never makes the result incomplete.
4. **Given** an uninitialized submodule, **When** `stw pins` runs, **Then** it is reported as `uninitialized`; no checkout, fetch or clone is attempted.

### User Story 3 — Run safely on a hostile repository (Priority: P1)

An agent runs the audit in a checkout whose configuration, submodule metadata or paths were written by untrusted or buggy code.

**Independent Test**: Use repositories with planted hooks, fsmonitor, filter and helper configuration, a `.git` file pointing outside the superproject, symlinked submodule directories, `.gitmodules` paths that are `..`, absolute or non-UTF-8, a hostile `core.worktree`, `commondir` or alternates, foreign-owned submodule repositories, URLs with embedded credentials and unusual file names; verify that no sentinel runs and no path escapes.

**Acceptance Scenarios**:

1. **Given** hooks, clean or process filters, external diff, textconv, fsmonitor, credential helpers or aliases configured in the superproject or a submodule, **When** the audit runs, **Then** none executes, including on stat-dirty tracked files.
2. **Given** a submodule path, `.git` pointer, Git directory, `commondir`, alternates or `core.worktree` that escapes the allowed locations or resolves through a symlink, **When** the audit runs, **Then** that submodule is refused with `submodule-path-unsafe`; no Git process runs against the escaped location, and other submodules are still reported.
3. **Given** a submodule worktree or Git directory not owned by the effective user, **When** the audit runs, **Then** it is refused with `submodule-unsafe-owner` before any Git process runs against it.
4. **Given** a configured URL with embedded credentials, **When** results are rendered, **Then** the credentials never appear, `url-has-credentials` is raised, and the URL is never contacted.
5. **Given** repository text such as paths, names, URLs and refs, **When** it is rendered, **Then** it is treated as data and cannot produce terminal control effects.

### User Story 4 — Check that registration is consistent (Priority: P2)

A maintainer finds submodules that are half registered: a gitlink without a `.gitmodules` entry, an entry without a gitlink, or a configured URL that differs from the remote the submodule repository actually uses.

**Independent Test**: Build superprojects with an orphan gitlink, a `.gitmodules` entry with no gitlink, an inactive submodule, a `.gitmodules` modified in the worktree, and a submodule whose own remote URL differs from `.gitmodules`.

**Acceptance Scenarios**:

1. **Given** an orphan gitlink or an entry without a gitlink, **When** `stw pins` runs, **Then** `gitlink-without-entry` or `entry-without-gitlink` is reported and the other fields remain available where they can be read.
2. **Given** a submodule whose own remote URL differs from `.gitmodules`, **When** `stw pins` runs, **Then** `url-mismatch` is reported with both URLs, redacted.
3. **Given** a worktree `.gitmodules` that differs from the HEAD blob or is a symlink, **When** `stw pins` runs, **Then** registration is read from the HEAD blob, the difference is reported as `gitmodules-modified`, and the symlink is never followed.
4. **Given** nested submodules, **When** `stw pins` runs, **Then** they are reported as present and not traversed; the limit is disclosed.

### User Story 5 — Rely on one bounded contract on each backend (Priority: P1)

Humans and agents use the same result, and the Tier 1 backends state their capability explicitly.

**Independent Test**: Validate success and error fixtures against the new schema; run the command in ordinary Git, linked Git worktree, colocated jj, standalone jj and ambiguous workspaces.

**Acceptance Scenarios**:

1. **Given** a Git-owned superproject, **When** `stw pins [PATH] [--json]` runs, **Then** human output and schema-versioned JSON come from one result and the command exits 0 even when findings exist.
2. **Given** `--strict` and a complete result with an error finding, **When** the command runs, **Then** the full result is still emitted and the exit status is 3; without error findings it is 0.
3. **Given** a jj-owned workspace, including a colocated repository, **When** `stw pins` runs, **Then** it refuses with `unsupported-backend`, states that jj has no submodule model, and starts no Git submodule command. Ambiguous ownership, GitButler-managed workspaces and bare repositories also refuse (see the refusal table).
4. **Given** a path argument whose bytes cannot be a path, **When** `stw pins` runs, **Then** it is a usage error: exit status 2 with empty standard output.
5. **Given** a linked Git worktree of the superproject, **When** `stw pins` runs there, **Then** its submodules, which Git keeps under that worktree's own `modules` directory, are audited like those of the main worktree.

### Edge Cases

- Linked Git worktrees of the superproject; old-style in-place `.git` directories inside a submodule; submodule Git directories under the worktree's own `modules` directory.
- Detached or unborn superproject HEAD; submodule with an unborn HEAD; shallow or partial submodule repositories; replaced objects (never honored).
- Submodule names and paths with spaces, newlines, leading dashes and case-only differences; a non-UTF-8 path, which JSON cannot represent, refuses that entry with `submodule-path-unsafe` and is shown in an escaped form with `path_encoding: "escaped"`.
- More than 256 submodules; output beyond 1 MiB; a subprocess producing more than its scan allowance; per-submodule and total deadlines; cancellation.
- A submodule HEAD, the superproject HEAD or its index changing while the audit runs.
- A `.gitmodules` that is a symlink, unreadable, modified in the worktree or different in the index.
- Several remotes in one submodule. "The remote the submodule uses" means the remote whose normalized URL equals the registered URL; when none or several match, the match is `unknown` and publication still considers every remote-tracking ref, naming which remote each ref belongs to.
- A submodule that is itself jj-managed or GitButler-managed is refused individually with `submodule-unsupported-backend`; the others are still audited.

## Requirements

### Functional Requirements

- **FR-001**: Provide `stw pins [PATH] [--json] [--strict]`. `PATH` defaults to the current directory. The command is read-only and non-interactive: no prompt, editor or pager.
- **FR-002**: Reuse the existing ownership detection and verify ownership before any submodule work. A jj-owned workspace, including a colocated or secondary one, and ambiguous ownership are decided from filesystem markers and start no process of any kind. The other refusals (GitButler, bare repository, not a repository) may use the existing detection's Git processes but start no jj process and no submodule command. Support Git-owned superproject worktrees, including linked worktrees. Never substitute Git for jj ownership.
- **FR-003**: Enumerate submodules from the gitlinks recorded in the superproject HEAD tree, the staged index entries and the `.gitmodules` blobs, and report each submodule path once with its registration state.
- **FR-004**: For each submodule report the HEAD pin, the staged pin, whether they differ, the submodule's active state and its configured URL (credentials removed), keeping every commit identity as a full object name.
- **FR-005**: For each initialized submodule report its checked-out commit, branch or detached state, the relation to the HEAD pin (`same`, `ahead`, `behind`, `diverged`, `unrelated`, `unknown`) with each side counted separately and capped at 10,000 (a `capped` flag marks a capped count), and dirty state: tracked changes and the presence of untracked files, each `present`, `absent` or `unknown`, decided without hashing content and within a dirty-scan budget of 3 seconds and 20,000 index entries per submodule.
- **FR-006**: Report publication evidence per pin: `reachable` when the pinned commit is contained in a remote-tracking ref (`refs/remotes/*`) of the submodule repository, naming at most 16 such refs, never counting symbolic refs such as a remote's `HEAD`, and flagging truncation; `unreachable` when the commit object exists and no remote-tracking ref contains it; `unknown` otherwise, with the reason (an absent object or a non-commit is `unknown`, not a failure). Local branches and tags never count as publication evidence.
- **FR-007**: Report the pin's relation to the configured tracking ref when one can be determined from the `.gitmodules` `branch` or the remote's default ref (`equal`, `behind`, `ahead`, `diverged`, `unknown`) with bounded counts. A pin behind its tracking ref is information, not an error.
- **FR-008**: Perform no network access, fetch, clone, `submodule update` or `submodule init`, and make no write to the superproject, any submodule, their Git directories, index, refs, reflogs or configuration. Disclose that remote-tracking refs may be stale, and report an advisory last-fetch time only when it can be read without writing.
- **FR-009**: Treat `.gitmodules`, submodule paths, `.git` pointer files, URLs, refs and Git-directory files (`commondir`, `objects/info/alternates`, `core.worktree`) as untrusted data. Resolve each submodule's working directory and Git directory without following symlinks. Accept a Git directory only at the absolute path Git itself reports for the worktree in use (`rev-parse --path-format=absolute --git-path modules`, joined with the submodule's Git directory name) or as an in-place `.git` directory inside the submodule. Where the superproject enables `extensions.submodulePathConfig`, the name is the configured `submodule.<name>.gitdir`, accepted only when it stays confined under that modules path and refused otherwise with `submodule-path-unsafe` and a distinct message. Refuse a submodule whose `commondir`, alternates or worktree setting lead outside the accepted locations. Always run Git against a submodule with an explicitly validated Git directory and an explicitly validated worktree, and revalidate before success.
- **FR-010**: Run Git with isolation that neutralizes hooks, clean and process filters, external diff, textconv, fsmonitor, optional locks, prompts, lazy fetch, replace objects, credential helpers, aliases and inherited repository selection. Never ask Git to compare or hash worktree contents. Because an explicit Git directory bypasses Git's own ownership check, verify ownership of each submodule worktree and Git directory against the effective user before any Git process runs there, and refuse with `submodule-unsafe-owner` otherwise.
- **FR-011**: Bound the operation: at most 256 submodules, 5 seconds per submodule, 30 seconds in total, a retained-output bound of 1 MiB for the result and a scan allowance per subprocess stream that is consumed incrementally rather than truncated silently. Exhausting one of these bounds marks the result incomplete with `pins-limit`; it is never reported as a complete success. The dirty-scan budget of FR-005 is separate and only yields `unknown` values.
- **FR-012**: Detect concurrent change: resample the superproject HEAD and index identity and each submodule HEAD before success; a changed value marks the result incomplete with `pins-state-changed`.
- **FR-013**: Do not traverse nested submodules; detect their presence from the submodule's HEAD tree (a `.gitmodules` blob) and report `nested-submodules-present` and the limit.
- **FR-014**: Provide understandable human output and a separate `schemas/v1/pins.schema.json` result with `schema_version: 1`, the stable finding codes and severities of the normative table, a summary of counts, explicit effects, limits and diagnostics, rendered from one result. Existing published JSON contracts do not change. Exit status, with this precedence: usage error 2; refusal, operational failure, cancellation or an incomplete result 1; otherwise 3 only with `--strict` and at least one error finding in a complete result; otherwise 0. On status 3 a single line on standard error names the number of error findings; standard output still carries the full result. Update the agent guide's exit-status text and the JSON contract guide.
- **FR-015**: Declare the capability matrix in the backend contract: Git supported; jj and GitButler explicitly unsupported for submodules; no equivalence is claimed.
- **FR-016**: Require failing behavior tests before implementation, independent review of the specification, plan and tasks, and conformance with effect sentinels on the existing minimum and current Git lanes and the jj refusal on the existing jj lanes. Other platforms provide local evidence only until CI covers them.
- **FR-017**: Read registration from the HEAD blob of `.gitmodules` (no includes) and compare the staged blob and the worktree file; never follow a worktree symlink. A difference is reported as `gitmodules-modified`, never silently preferred. With an unborn HEAD, registration comes only from the staged blob and the HEAD pin is null. A HEAD `.gitmodules` that is not a regular file (a symlink or gitlink) is not read: registration is unknown, `gitmodules-modified` is raised, and no `gitlink-without-entry` is claimed. Gitlinks are enumerated recursively through the tree.

### Refusal table (normative)

| `error.code` | Result `status` | `backend` | When |
| --- | --- | --- | --- |
| `unsupported-backend` | `refused` | `jj` or `gitbutler` | jj-owned (including colocated and secondary) or GitButler-managed workspace; message names the capability gap |
| `ambiguous-backend` | `refused` | `ambiguous` | ownership markers conflict |
| `bare-repository` | `refused` | `git` | no worktree to compare |
| `not-repository` | `refused` | `unknown` | no repository at the path, including a path that does not exist or is not a directory |
| `pins-failed` | `error` | `git` or `unknown` | an operational failure such as a failed Git invocation, an unreadable path or cancellation; `git` only once Git ownership has been established, `unknown` for a failure before ownership is decided |

### Findings (normative)

Codes are append-only within `schema_version: 1`. Severities are fixed. `--strict` fails only on error findings; an uninitialized submodule has publication `unknown` and an `info` finding, so a passing `--strict` run does not prove that uninitialized submodules are published.

| Code | Severity | Meaning |
| --- | --- | --- |
| `pin-unpublished` | error | pin exists locally and no remote-tracking ref contains it |
| `gitlink-without-entry` | error | gitlink with no `.gitmodules` entry |
| `url-has-credentials` | error | registered URL embeds credentials |
| `submodule-path-unsafe` | error | submodule refused: path, Git directory, pointer or encoding unsafe |
| `submodule-unsafe-owner` | error | submodule refused: not owned by the effective user |
| `submodule-unsupported-backend` | warning | submodule is itself jj- or GitButler-managed, not audited |
| `pins-limit` | error | a bound was exhausted; result incomplete |
| `pins-state-changed` | error | state changed during the audit; result incomplete |
| `staged-pin-differs` | warning | staged pin differs from the HEAD pin |
| `checkout-differs-from-pin` | warning | checkout is ahead, behind, diverged or unrelated |
| `submodule-dirty` | warning | tracked changes or untracked files present |
| `url-mismatch` | warning | registered URL differs from the submodule's own remote |
| `entry-without-gitlink` | warning | `.gitmodules` entry with no gitlink |
| `gitmodules-modified` | warning | worktree or staged `.gitmodules` differs from the HEAD blob |
| `pin-object-absent` | error | submodule is initialized but the pinned commit object is not in its repository |
| `submodule-uninitialized` | info | submodule has no checkout |
| `pin-behind-tracking` | info | pin is behind its tracking ref |
| `nested-submodules-present` | info | nested submodules exist and were not traversed |
| `remote-refs-may-be-stale` | info | global, present whenever publication was evaluated |

### Key Entities

- Submodule pin: path, name, HEAD pin, staged pin, registration state.
- Checkout evidence: initialization, checked-out commit, relation to the pin, dirty state.
- Publication evidence: status, the remote-tracking refs containing the pin, observation limits.
- Finding: stable code, severity, affected submodule, bounded message.
- Pin audit result: native identity, submodules, findings, summary, effects, limits, completeness.

## Success Criteria

- **SC-001**: A pin to a never-pushed commit is reported as `unreachable` with `pin-unpublished` in a deterministic fixture that reproduces the incident this feature answers, in a main worktree and in a linked worktree.
- **SC-002**: Every acceptance scenario and edge case above has a deterministic test; skipped or missing execution never counts as acceptance.
- **SC-003**: Effect sentinels prove that refs, HEAD, index bytes and modification time, reflogs, configuration, worktree files and Git directories of the superproject and every submodule are unchanged, that no lock file remains, and that no hook, filter, helper or network connection ran (the network sentinel is a registered remote helper whose invocation fails the test). On the jj refusal path the jj working copy and operation log are also unchanged.
- **SC-004**: All success, incomplete, refused and error fixtures validate offline against `pins.schema.json`, and existing contract fixtures remain valid.
- **SC-005**: The existing Git minimum and current lanes and the jj lanes pass on `ubuntu-24.04` amd64 without counting an unavailable lane as a pass. Linux arm64 and darwin arm64 smoke results are recorded as local evidence in the verification record and stay labelled as not CI-covered until the release-readiness work adds those lanes.

## Scope and Dependencies

This is a bounded read-only slice that uses the existing native identity, ownership and Git isolation foundations. It supplies evidence for later work. A plan/apply operation that bumps a pin, fetches or pushes a submodule, traverses nested submodules, repairs registration, or compares a pin with a hosted repository (through `stw hosting`) needs its own specification, and so does adding CI lanes for further platforms. Constitution 1.1.0 applies with no amendment: the feature is read-only, discloses that it makes no network call, and states the jj capability gap instead of claiming equivalence.
