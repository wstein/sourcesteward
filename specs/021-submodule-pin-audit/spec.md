# Feature Specification: Submodule pin audit

**Feature Branch**: `codex/021-submodule-pin-audit`  
**Created**: 2026-10-16  
**Status**: Draft — specification; plan, tasks and independent analysis follow before implementation  
**Input**: A read-only `stw pins` command that reports, for a Git superproject, what each submodule pin records, what is checked out, and whether the recorded commit is published to a remote the local repository knows.  
**Priority rating**: 10/10 (user-selected product priority, not verification evidence)

## User Scenarios & Testing

### User Story 1 — Find a pin that is not published (Priority: P1)

A human or agent is about to rely on a superproject commit whose submodule pin may point at a commit that exists only in a local submodule checkout. A fresh clone of that superproject could not fetch the pinned commit.

**Why this priority**: This is the failure that motivated the feature. A pin to a commit that was never pushed passes every local check and breaks only for the next person or agent.

**Independent Test**: In temporary repositories, create a superproject with submodules whose "remote" is a local bare repository. Pin one submodule to a commit that was never pushed and another to a pushed commit; compare the reported publication evidence.

**Acceptance Scenarios**:

1. **Given** a pin whose commit is reachable from a remote-tracking ref of the submodule repository, **When** `stw pins` runs, **Then** publication is `reachable` and names the refs that contain it.
2. **Given** a pin whose commit exists locally but is reachable from no remote-tracking ref, **When** `stw pins` runs, **Then** publication is `unreachable`, the finding `pin_unpublished` has error severity, and the text says only that local evidence shows no remote contains it, not that the remote lacks it.
3. **Given** a pin whose commit object is absent from the local submodule repository or whose submodule is not initialized, **When** `stw pins` runs, **Then** publication is `unknown` with the reason, never `reachable` or `unreachable`.
4. **Given** remote-tracking refs that may be stale, **When** results are reported, **Then** the result discloses that no fetch was made and reports the advisory last-fetch time when it can be read.

### User Story 2 — Compare pin, staged pin and checkout (Priority: P1)

A reviewer sees where the recorded pin, the staged pin and the checked-out submodule commit disagree, and whether the checkout has uncommitted work.

**Independent Test**: Build temporary superprojects with a staged pin change, a checkout that is ahead, behind or diverged from the pin, a dirty submodule and an uninitialized submodule.

**Acceptance Scenarios**:

1. **Given** a pin staged in the index that differs from the commit recorded at HEAD, **When** `stw pins` runs, **Then** both commits are reported and `staged_pin_differs` is raised.
2. **Given** a checkout equal to, ahead of, behind or diverged from the HEAD pin, **When** `stw pins` runs, **Then** the relation is reported with bounded counts, or `unknown` when the objects needed to decide are missing.
3. **Given** tracked changes or untracked files inside a submodule checkout, **When** `stw pins` runs, **Then** they are reported as dirty state without any file content, and an unscannable or oversized untracked scan is reported as `unknown`.
4. **Given** an uninitialized submodule, **When** `stw pins` runs, **Then** it is reported as `uninitialized`; no checkout, fetch or clone is attempted.

### User Story 3 — Run safely on a hostile repository (Priority: P1)

An agent runs the audit in a checkout whose configuration, submodule metadata or paths were written by untrusted or buggy code.

**Independent Test**: Use repositories with planted hooks, fsmonitor and helper configuration, a `.git` file pointing outside the superproject, symlinked submodule directories, `..` or absolute `.gitmodules` paths, URLs with embedded credentials, and unusual file names; verify that no sentinel runs and no path escapes.

**Acceptance Scenarios**:

1. **Given** hooks, filters, external diff, fsmonitor, credential helpers or aliases configured in the superproject or a submodule, **When** the audit runs, **Then** none executes.
2. **Given** a submodule `.git` pointer, directory or `.gitmodules` path that escapes the superproject or resolves through a symlink, **When** the audit runs, **Then** that submodule is refused with `submodule_path_unsafe`; no Git process runs against the escaped location, and other submodules are still reported.
3. **Given** a configured URL with embedded credentials, **When** results are rendered, **Then** the credentials never appear, `url_has_credentials` is raised, and the URL is never contacted.
4. **Given** repository text such as paths, names, URLs and refs, **When** it is rendered, **Then** it is treated as data and cannot produce terminal control effects.

### User Story 4 — Check that registration is consistent (Priority: P2)

A maintainer finds submodules that are half registered: a gitlink without a `.gitmodules` entry, an entry without a gitlink, or a configured URL that differs from the remote the submodule repository actually uses.

**Independent Test**: Build superprojects with an orphan gitlink, a `.gitmodules` entry with no gitlink, an inactive submodule and a submodule whose own remote URL differs from `.gitmodules`.

**Acceptance Scenarios**:

1. **Given** an orphan gitlink or an entry without a gitlink, **When** `stw pins` runs, **Then** `gitlink_without_entry` or `entry_without_gitlink` is reported and the other fields remain available where they can be read.
2. **Given** a submodule whose own remote URL differs from `.gitmodules`, **When** `stw pins` runs, **Then** `url_mismatch` is reported with both URLs, redacted.
3. **Given** nested submodules, **When** `stw pins` runs, **Then** they are reported as present and not traversed; the limit is disclosed.

### User Story 5 — Rely on one bounded contract on each backend (Priority: P1)

Humans and agents use the same result, and the Tier 1 backends state their capability explicitly.

**Independent Test**: Validate success and error fixtures against the new schema; run the command in ordinary Git, linked Git worktree, colocated jj, standalone jj and ambiguous workspaces.

**Acceptance Scenarios**:

1. **Given** a Git-owned superproject, **When** `stw pins [PATH] [--json]` runs, **Then** human output and schema-versioned JSON come from one result and the command exits 0 even when findings exist.
2. **Given** `--strict`, **When** any finding has error severity, **Then** the full result is still emitted and the exit status is 3; without error findings it is 0.
3. **Given** a jj-owned workspace, including a colocated repository, **When** `stw pins` runs, **Then** it refuses with `backend_unsupported`, states that jj has no submodule model, and runs no Git submodule command. Ambiguous ownership and GitButler also refuse.
4. **Given** a bare repository, **When** `stw pins` runs, **Then** it refuses as unsupported, because there is no worktree to compare.

### Edge Cases

- Linked Git worktrees of the superproject; submodules whose Git directory lives under the common directory's `modules/`; old-style in-place `.git` directories.
- Detached or unborn superproject HEAD; submodule with an unborn HEAD; shallow or partial submodule repositories; replaced objects.
- Submodule names and paths with spaces, newlines, non-UTF-8 bytes, leading dashes, and case-only differences.
- More than 256 submodules; output beyond 1 MiB; per-submodule and total deadlines; cancellation.
- A submodule HEAD, the superproject HEAD or its index changing while the audit runs.
- Foreign-owned submodule repositories that Git itself refuses; a `.gitmodules` that is itself a symlink or an unreadable file.
- Several remotes in one submodule, including a fork that contains the pin while the configured remote does not.

## Requirements

### Functional Requirements

- **FR-001**: Provide `stw pins [PATH] [--json] [--strict]`. `PATH` defaults to the current directory. The command is read-only and non-interactive: no prompt, editor or pager.
- **FR-002**: Verify native ownership before any submodule work. Support Git-owned superproject worktrees, including linked worktrees. Refuse jj-owned (including colocated) workspaces, ambiguous ownership, GitButler-managed workspaces and bare repositories with a structured `backend_unsupported` error. Never substitute Git for jj ownership.
- **FR-003**: Enumerate submodules from the gitlinks recorded in the superproject HEAD tree, the staged index entries and the `.gitmodules` entries, and report each submodule path once with its registration state.
- **FR-004**: For each submodule report the HEAD pin, the staged pin, whether they differ, the submodule's active state and its configured URL (credentials removed), keeping every commit identity as a full object name.
- **FR-005**: For each initialized submodule report its checked-out commit, branch or detached state, the relation to the HEAD pin (`same`, `ahead`, `behind`, `diverged`, `unrelated`, `unknown`) with counts bounded to 10,000, and dirty state: tracked changes, and the presence of untracked files, each `present`, `absent` or `unknown`.
- **FR-006**: Report publication evidence per pin: `reachable` when the pinned commit is an ancestor of a remote-tracking ref (`refs/remotes/*`) of the submodule repository, naming at most 16 such refs; `unreachable` when the object exists and no remote-tracking ref contains it; `unknown` otherwise, with the reason. Local branches and tags never count as publication evidence.
- **FR-007**: Report the pin's relation to the configured tracking ref when one can be determined from `.gitmodules` `branch` or the remote's default ref (`equal`, `behind`, `ahead`, `diverged`, `unknown`) with bounded counts. A pin behind its tracking ref is information, not an error.
- **FR-008**: Perform no network access, fetch, clone, `submodule update` or `submodule init`, and make no write to the superproject, any submodule, their Git directories, index, refs, reflogs or configuration. Disclose that remote-tracking refs may be stale, and report an advisory last-fetch time only when it can be read without writing.
- **FR-009**: Treat `.gitmodules`, submodule paths, `.git` pointer files, URLs and refs as untrusted data. Resolve submodule directories and Git directories without following symlinks and only inside the superproject; refuse a submodule whose path or Git directory escapes, and run Git against a submodule only through a validated Git directory.
- **FR-010**: Run Git with the existing isolation: no hooks, filters, external diff or textconv, fsmonitor, optional locks, prompts, lazy fetch, replace objects, credential helpers, aliases or inherited repository selection. Leave Git's own ownership checks in force; a repository Git refuses is reported as `submodule_unsafe_owner`, not overridden.
- **FR-011**: Bound the operation: at most 256 submodules, 5 seconds per submodule, 30 seconds in total, 1 MiB of subprocess output and a bounded final result. Exhausting a bound marks the result `complete: false` with a finding; it is never reported as a complete success.
- **FR-012**: Detect concurrent change: resample the superproject HEAD and index identity and each submodule HEAD before success; a changed value refuses a complete result with `state_changed_during_audit`.
- **FR-013**: Do not traverse nested submodules; report `nested_submodules_present` and the limit.
- **FR-014**: Provide understandable human output and a separate `schemas/v1/pins.schema.json` result with `schema_version: 1`, stable finding codes and severities (`error`, `warning`, `info`), a summary of counts, explicit effects, limits and diagnostics, rendered from one result. Existing published JSON contracts do not change. Exit status: 0 success, 1 operational failure or refusal, 2 usage, 3 only with `--strict` and at least one error finding.
- **FR-015**: Declare the capability matrix in the backend contract: Git supported; jj and GitButler explicitly unsupported for submodules; no equivalence is claimed.
- **FR-016**: Require failing behavior tests before implementation, independent review of the specification, plan and tasks, and conformance with effect sentinels on the minimum and current supported Git versions and the jj refusal path on the minimum and current jj lanes.

### Key Entities

- Submodule pin: path, name, HEAD pin, staged pin, registration state.
- Checkout evidence: initialization, checked-out commit, relation to the pin, dirty state.
- Publication evidence: status, the remote-tracking refs containing the pin, observation limits.
- Finding: stable code, severity, affected submodule, bounded message.
- Pin audit result: native identity, submodules, findings, summary, effects, limits, completeness.

## Success Criteria

- **SC-001**: A pin to a never-pushed commit is reported as `unreachable` with `pin_unpublished` in a deterministic fixture that reproduces the incident this feature answers.
- **SC-002**: Every acceptance scenario and edge case above has a deterministic test; skipped or missing execution never counts as acceptance.
- **SC-003**: Effect sentinels prove that refs, HEAD, index bytes, reflogs, configuration, worktree files and Git directories of the superproject and every submodule are unchanged, that no lock file remains, and that no hook, helper or network connection ran.
- **SC-004**: All success and error fixtures validate offline against `pins.schema.json`, and existing contract fixtures remain valid.
- **SC-005**: Minimum and current Git conformance lanes and the jj refusal lanes pass on the supported platforms (linux amd64 and arm64, darwin arm64) without counting an unavailable lane as a pass.

## Scope and Dependencies

This is a bounded read-only slice that uses the existing native identity, ownership and Git isolation foundations. It supplies evidence for later work. A plan/apply operation that bumps a pin, fetches or pushes a submodule, traverses nested submodules, repairs registration, or compares a pin with a hosted repository (through `stw hosting`) needs its own specification. Constitution 1.1.0 applies with no amendment: the feature is read-only, discloses that it makes no network call, and states the jj capability gap instead of claiming equivalence.
