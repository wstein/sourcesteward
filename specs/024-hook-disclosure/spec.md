# Feature Specification: Hook and bypass disclosure

**Feature Branch**: `codex/024-hook-disclosure`
**Created**: 2026-10-16
**Status**: Draft — reduced in scope after five analysis rounds (see analysis.md, "Scope reduction applied"); round 6 and its confirmation (READY) recorded there; implementation pending. The jj behavior this feature reports is `unverified` until the conformance lane in FR-014 has measured it.
**Input**: A read-only `stw hooks` command that answers one question: which commit and push hooks would Git run from the repository's effective `core.hooksPath`, and does the backend that owns the workspace (Git or jj) run them, at which evidence level.

## User Scenarios & Testing

### User Story 1 — See which commit and push guards Git would run (Priority: P1)

A maintainer or agent in a Git repository whose `core.hooksPath` points at a tracked `.githooks` directory (the WorkHarbor setup: secret scanning in `pre-commit`, message linting in `commit-msg`, checks in `pre-push`) asks which guards exist and would run.

**Why this priority**: Everything else builds on a trustworthy answer to "what guards does this repository have". A hook that is present but not executable, or a `core.hooksPath` that points nowhere, silently disables a guard.

**Independent Test**: In temporary repositories, set `core.hooksPath` to a tracked directory with `pre-commit`, `commit-msg` and `pre-push` hooks; compare the reported guards with what a real `git commit` and `git push` run (sentinel files), including a non-executable hook, an absent directory and `/dev/null`.

**Acceptance Scenarios**:

1. **Given** `core.hooksPath` set to a relative directory in the repository configuration, **When** `stw hooks` runs from the root, from a subdirectory or from a linked worktree, **Then** the effective hooks directory is reported as one absolute path resolved the way Git resolves it, the scope that set the effective value is named, and each of the six operation hooks present is listed by name, kind, state and mode, never by content.
2. **Given** the three WorkHarbor hooks, **When** the command runs, **Then** the `commit` operation lists `pre-commit` and `commit-msg` as active blocking guards and `post-commit` as absent, and the `push` operation lists `pre-push` as an active blocking guard.
3. **Given** `core.hooksPath` is the null device, an empty value, an absent directory, a non-directory, a directory without search permission, a valueless key or a value Git cannot expand, **When** the command runs, **Then** the matching finding of the findings table is reported and no directory hook is called active.
4. **Given** a hook entry that fails `access(X_OK)` for the real user (no executable bit, a dangling or looping symlink, a directory or FIFO without the executable bit), **When** the command runs, **Then** it is `ignored` with `hook-ignored`, because Git skips it. **Given** an executable entry that is not a regular file (an executable directory, or a symlink to one), **Then** it is `not-runnable` with `hook-not-runnable`, because Git fails the operation.
5. **Given** configuration hooks (`hook.<name>.event`) naming one of the six operation hooks, **When** the command runs, **Then** `config-hooks-not-evaluated` is reported, they are not evaluated, and `no-active-guards` is not reported.

### User Story 2 — Learn which guards a backend bypasses (Priority: P1)

A maintainer considering a move to jj, or an agent about to run `jj commit` or `jj git push` in a colocated repository whose Git hooks hold the guards, asks whether those guards would run.

**Why this priority**: This is the motivating failure. jj does not run Git hooks as far as its documentation says, so a commit made with jj may bypass every guard without any warning.

**Independent Test**: Run the command in a colocated jj workspace whose Git configuration has the WorkHarbor hooks; the result must say, per operation, that the owning backend is jj, what is claimed, at which evidence level, and which active guards are bypassed or possibly bypassed. The conformance lane of FR-014 measures the claim.

**Acceptance Scenarios**:

1. **Given** a colocated jj workspace with active blocking hooks, **When** `stw hooks` runs, **Then** the backend is `jj`, the `commit` operation (`jj commit`) states `runs_hooks: no` at evidence level `documented`, each active guard is `bypassed: true`, and `guards-bypassed-on-backend` (error) and `backend-hook-behavior-unverified` (warning) are reported.
2. **Given** the same workspace, **When** the `push` operation (`jj git push`) is reported, **Then** it states `runs_hooks: unknown` at level `unverified`, each active guard is `bypassed: null`, and `guards-may-be-bypassed` (warning) is reported, because jj documents that remote interactions spawn a `git` subprocess and whether that runs `pre-push` is not established.
3. **Given** the same hooks in a Git-owned repository, **When** the command runs, **Then** both operations state `runs_hooks: yes`, the documented exceptions (such as `--no-verify`) are listed, and no bypass finding is raised.
4. **Given** a jj workspace with no `.git` entry at its root, **When** the command runs, **Then** no Git configuration is read, `guards_evaluated` is false for each operation, no bypass finding is raised, `workspace-has-no-git-configuration` states that jj's `git` subprocess may still consult Git configuration this report does not examine, and the jj claims are still reported at their level. A secondary workspace with a `.git` gitfile is handled as colocated.
5. **Given** a detected backend version outside the inclusive range that the conformance lane measured, **When** the report is produced, **Then** a claim whose catalog level is `measured` is reported as `documented` and `version_covered` is false.
6. **Given** no active blocking guard for either operation, `guards_evaluated` true and no configuration hook naming an operation hook, **When** the report is produced, **Then** no bypass finding is raised and `no-active-guards` (info) is reported.

### User Story 3 — Run safely on a hostile repository (Priority: P1)

An agent runs the report in a checkout whose configuration, hook names or symlinks were written by untrusted code.

**Independent Test**: Use repositories with hooks and configuration hooks that write a sentinel, a FIFO and a device where a hook is expected, symlink loops and dangling links, `core.hooksPath` values with control characters, non-UTF-8 bytes, `~user` forms, very long values and an include of an unreadable file; verify that no sentinel runs, no hook entry is opened, and output stays bounded and escaped.

**Acceptance Scenarios**:

1. **Given** hooks and `hook.<name>.command` entries that would write a sentinel, **When** the command runs, **Then** nothing executes (no hook, configuration hook command, `git hook`, filter, helper or alias) and nothing in the repository, the Git directory or the configuration is written.
2. **Given** any hook entry, **When** the command runs, **Then** it is never opened: only `lstat`, `readlink` and `access` are used. A FIFO named like a hook does not make the command block.
3. **Given** repository text such as `core.hooksPath` values, **When** it is rendered, **Then** it is treated as data and cannot produce terminal control effects in text output.

### User Story 4 — Rely on one bounded contract on each backend (Priority: P1)

Humans and agents use the same result, and every Tier 1 backend states its capability explicitly.

**Independent Test**: Validate success, incomplete, refused and error fixtures against the new schema; run the command in ordinary Git, a linked Git worktree, colocated jj, standalone jj, secondary jj, ambiguous and GitButler workspaces.

**Acceptance Scenarios**:

1. **Given** a Git-owned worktree, **When** `stw hooks [PATH] [--json]` runs, **Then** human output and schema-versioned JSON come from one result and the command exits 0 even when findings exist.
2. **Given** a jj-owned workspace, **When** the command runs, **Then** it uses the jj adapter under the existing mandatory read-only sandbox for its jj facts, and an unavailable sandbox, missing jj or unsupported jj version produces the existing actionable error code, never an unsandboxed retry and never a report from Git alone.
3. **Given** a GitButler-managed, ambiguous, bare or non-repository path, **When** the command runs, **Then** it refuses with the code of the refusal table.

### User Story 5 — Treat jj hook behavior as a measurement (Priority: P2)

A maintainer needs to know whether the jj claim in the report is established or only documented, and needs the lane that will establish it.

**Independent Test**: Review the conformance procedure and the evidence catalog; the procedure is deterministic, uses a local bare remote, and includes positive controls proving that the sentinel hooks fire under plain Git.

**Acceptance Scenarios**:

1. **Given** the minimum and current jj lanes, **When** the hook fixture runs `jj commit` and `jj git push`, **Then** each deciding sentinel is either written or absent and the outcome is recorded; a result that contradicts a `yes` or `no` claim fails the lane until the catalog and the documentation are changed, as the conformance procedure provides.
2. **Given** the catalog before measurement, **When** the report is produced, **Then** no jj claim is labelled `measured`.

### Edge Cases

- Linked Git worktrees: a relative `core.hooksPath` resolves under the worktree in use; the default hooks directory is the common directory's `hooks`.
- `core.hooksPath` set at system, global, local or worktree scope: Git's last value wins; only the scope of the winning value is reported. Includes are applied by Git.
- `core.hooksPath` that is `~/...` (expanded by Git), relative with `..`, absolute, `/dev/null`, empty, a file, a FIFO, a symlink, or a directory without search permission.
- Hooks in the default `<git-dir>/hooks` while `core.hooksPath` points elsewhere are inert and not examined; the notes say so.
- Hooks copied by `git init --template` are ordinary files; their provenance is not claimed.
- Other hook names (the 22 client and server names of `githooks(5)` outside the six) are not examined.
- A hook whose interpreter is missing, or a script that disables itself through an environment variable, is not detectable without reading content; disclosed as a limit.
- Per-command overrides (`git -c core.hooksPath=/dev/null commit`, `--no-verify`, `-n`) and the caller's `GIT_*` environment are never detected or applied; the notes say so.
- A hook changing while the command runs; a value above 4,096 bytes.
- A jj secondary workspace is colocated when it has a `.git` entry (a gitfile into the common directory's `worktrees/`) and has no Git configuration otherwise.
- A colocated jj repository whose Git configuration cannot be read, whose Git directory fails Git's ownership check or differs from the one the `.git` entry designates: `hooks-failed`.
- Nested `.jj` markers: the innermost marker is the workspace root.

## Requirements

### Functional Requirements

- **FR-001**: Provide `stw hooks [PATH] [--json]`. `PATH` defaults to the current directory. The command is read-only and non-interactive: no prompt, editor or pager. It accepts no other flag.
- **FR-002**: Select the backend by ownership. A path with no `.jj` marker in an ancestor is decided by the existing `Inspect` (Git worktree, linked worktree, GitButler, bare, not a repository, and its error codes). A path with a `.jj` marker is decided by the existing filesystem-only jj detection (`detectJJGitButler`, `managed`) without the identity queries of `Inspect`; ambiguity refuses before any process starts. Never substitute Git for jj ownership: in a jj-owned workspace Git is consulted only for the read-only reads of FR-004 and only when a `.git` entry exists at the workspace root; those Git reads run outside the jj sandbox and are disclosed (`git_processes`).
- **FR-003**: For a jj-owned workspace obtain jj facts only through the existing mandatory read-only sandbox session (sandbox probe, `--version`, supported-version check) and report the jj version. No other jj command is run. Failures use the existing codes `jj-unavailable`, `jj-sandbox-unavailable`, `jj-version-unsupported`, `jj-timeout`, `jj-output-limit`, `invalid-metadata` and `canceled` with status `error` and no unsandboxed retry.
- **FR-004**: For a Git-owned worktree, and for a jj workspace with a `.git` entry (a directory, or a gitfile of a worktree-backed secondary workspace), run `git version` (Git 2.46 or newer; `git-unavailable` or `git-failed` otherwise) and read `core.hooksPath` through the allowlisted reads of research.md. `git config` does not apply Git's ownership check, so every read is bracketed by `rev-parse --absolute-git-dir`, which does; a failure of that gate is `hooks-failed`, and `config` exit status 1 means "unset" only after the gate passed (other non-zero statuses are `hooks-failed`). The two gate outputs must equal each other and, on a Git-owned path, `Inspect`'s `git_dir`; for jj they must equal the directory the `.git` entry designates. Values are read with `config --show-scope -z --get-regexp '^core\.hookspath$'` so that a valueless key (state `invalid`) is distinguished from an empty value (state `empty`); the last record is the effective value and its scope is reported. The effective hooks directory is the absolute path from `rev-parse --path-format=absolute --git-path hooks` (not run for an empty value; a failure for a non-empty value is state `invalid`). A jj workspace with no `.git` entry reads no Git and reports `guards_evaluated: false`.
- **FR-005**: Classify each of the six operation hooks of the hook catalog in the resolved directory with `lstat`, `readlink` (symlinks only) and `access(X_OK)` for the real user, as Git does; never open a hook entry, never read content, never execute. Order: `ignored` when `access(X_OK)` fails (no executable bit, dangling or looping symlink); otherwise `not-runnable` when the entry or its symlink target is not a regular file; otherwise `active`. Report kind, mode and, for a symlink, whether it dangles. No directory is listed.
- **FR-006**: Detect configuration hooks by one read of the keys `hook.*.event` and their values (event names only; `config -z --get-regexp '^hook\..*\.event$'`). If any value names one of the six operation hooks, or a `hook.*.event` key is valueless (Git then fails every operation), report `config_hooks_detected: true` and the finding `config-hooks-not-evaluated`; configuration hooks are not evaluated, their enabled state and command are not read, and `no-active-guards` is not reported.
- **FR-007**: Report two operations, `commit` and `push`, for the owning backend from the evidence catalog: the backend command, `runs_hooks` (`yes`, `no`, `unknown`), the evidence, the documented exceptions, `guards_evaluated`, and per hook of the operation (hook catalog) its role, state (`active`, `ignored`, `not-runnable`, `disabled-by-path`, `absent`; `disabled-by-path` when the hooks path state is `null-device`, `empty`, `absent`, `not-directory`, `unreadable` or `invalid`) and `bypassed` (`true` when `runs_hooks` is `no` and the hook is `active`; `null` when `runs_hooks` is `unknown` and the hook is `active`; otherwise `false`). When `guards_evaluated` is false the hook list is empty.
- **FR-008**: Evidence levels are exactly `measured` (a conformance fixture of this release observed the behavior on both the minimum and the current version of the claim's backend tuple, with the same outcome, and the detected versions lie inside that inclusive range; the `jj-push` tuple is jj plus the Git on `PATH`, paired as jj minimum with Git minimum and jj current with Git current), `documented` (only a vendor statement, or a measurement that does not cover the detected version) and `unverified` (no sufficient statement). Each claim has exactly one level. Disagreeing lanes leave a claim `unverified`. Versions compare as numbers after removing a `-<40 hex>` build suffix; a version with a non-numeric component (for example `2.57.0.rc1`) or a null tuple member (no Git process ran) is not covered, and coverage means every detected version lies inside the inclusive measured range of its own member (the lanes measured the pairs jj minimum with Git minimum and jj current with Git current, and the range of each member spans its two lane versions; a combination between them is treated as covered, which is the documented limit of the lanes). The evidence catalog is the only place a claim and its level are defined; the report carries the level with every claim.
- **FR-009**: Never execute a hook, a configuration hook command, `git hook` or any helper, filter, alias, pager or editor; never open or read a hook entry; never write to the repository, the Git directory, the configuration or the jj repository; make no network call. Treat hook paths, configuration values and symlink targets as untrusted data; text output escapes control, format and separator characters and invalid bytes. The jj working copy and operation log are unchanged (effect sentinel).
- **FR-010**: Run Git with the existing isolation prefix (`--no-replace-objects`, `core.fsmonitor=false`, `GIT_*` removed, optional locks, prompts, lazy fetch and pager disabled; `HOME` and `XDG_CONFIG_HOME` kept, so global configuration is read as Git reads it) for the allowlisted read-only invocations only. System and global configuration are in scope as read-through (research.md); the report states only the scope of the winning value.
- **FR-011**: Bound the operation by the limits table. Exhausting a bound marks the result incomplete with `hooks-limit`; it is never reported as a complete success.
- **FR-012**: Detect concurrent change: resample the effective `core.hooksPath` value and each present hook's `lstat` metadata before success; a difference marks the result incomplete with `hooks-state-changed`.
- **FR-013**: Provide understandable human output and a new `schemas/v1/hooks.schema.json` result with `schema_version: 1`, the codes and values of the normative tables, a summary, explicit effects, limits and notes, rendered from one result. Existing published contracts (inspect schema 1 and 2, inventory, tidy, workspace, hosting, version) do not change. Exit status: usage error 2; refusal, operational failure, cancellation or an incomplete result 1; otherwise 0. Findings never change the exit status.
- **FR-014**: Treat jj hook behavior as a measurement with the conformance procedure below, run on the existing minimum and current Git and jj lanes on `ubuntu-24.04` amd64, and make the same script assert, in the colocated fixture, the report's claims and levels and the effect sentinels. Until a lane has recorded an outcome, the catalog states `documented` or `unverified` and the report must not say `measured`.
- **FR-015**: Declare the capability matrix in the backend contract: Git supported; jj supported with the stated evidence levels; GitButler explicitly unsupported; no equivalence between backends is claimed.
- **FR-016**: Require failing behavior tests before implementation, independent review of the specification, plan and tasks, and conformance with effect sentinels (no hook ran, no write, no hook entry opened) on the existing lanes. Other platforms provide local evidence only until CI covers them.

### Hook catalog (normative)

Exactly six hook names are examined. "Skipped by `--no-verify`" was measured once by the author on Git 2.56.0 (research.md, measurements 1 and 2) and is carried as `documented` until the lane records it.

| Hook | Role | Operation | Skipped by `--no-verify` |
| --- | --- | --- | --- |
| `pre-commit` | blocking | `commit` | yes |
| `prepare-commit-msg` | blocking | `commit` | no |
| `commit-msg` | blocking | `commit` | yes |
| `post-commit` | observer | `commit` | no |
| `pre-push` | blocking | `push` | yes |
| `reference-transaction` | blocking | `commit`, `push` | no |

A guard is an active hook of role `blocking`. `reference-transaction` is blocking because `githooks(5)` says Git aborts the operation when it exits non-zero in the `preparing` or `prepared` state (not measured here).

### Evidence catalog (normative)

| Claim | Backend | Operation and command | `runs_hooks` | Level at specification time | Sources |
| --- | --- | --- | --- | --- | --- |
| `git-commit` | Git | `commit`: `git commit` | `yes` | `documented`; promoted to `measured` by FR-014 | `githooks(5)`; measurement 1 on Git 2.56.0 |
| `git-push` | Git | `push`: `git push` | `yes` | `documented`; promoted to `measured` by FR-014 | `githooks(5)`; measurement 2 on Git 2.56.0 |
| `jj-commit` | jj (tuple: jj only; a detected Git version is ignored) | `commit`: `jj commit` | `no` | `documented` | docs.jj-vcs.dev git-compatibility, "Hooks: No"; issue jj-vcs/jj#405 open |
| `jj-push` | jj (tuple: jj plus the Git on `PATH`) | `push`: `jj git push` | `unknown` | `unverified` | the same statement, and docs.jj-vcs.dev config: "Git remote interactions are handled by spawning a `git` subprocess"; whether that subprocess runs `pre-push` is not established |

Exceptions listed with each Git claim, all at level `documented`: `no-verify` (skips `pre-commit` and `commit-msg` on commit and `pre-push` on push; the other catalog hooks still run), `hooks-path-override` (`git -c core.hooksPath=/dev/null`), `caller-config-environment` (`GIT_CONFIG_*` variables), `not-executable` (hook skipped). No exception is listed for jj.

A claim reads `measured` only when the catalog marks it measured for its backend tuple and every detected version is within the inclusive measured range; otherwise it reads the catalog's non-measured level and `version_covered` is false. The jj behavior is `unverified` as a measurement until a lane records it.

### Conformance procedure for jj hook behavior (normative)

A disposable fixture is built under a temporary directory with an isolated home, no user or system configuration, a configured author and committer identity for Git and jj, and a sentinel directory outside every repository. jj runs directly in the fixture, not in the read-only sandbox of `stw`. Hooks `pre-commit`, `commit-msg` and `pre-push` have an absolute sentinel path baked in (no environment variable) and append their name to a sentinel file that is truncated before each step. The tracked `.githooks` directory holds the hooks (the WorkHarbor shape).

| Step | Action | Recorded outcome |
| --- | --- | --- |
| J0 | Positive control: plain Git clone with `core.hooksPath=.githooks`; `git commit` and `git push` to a local bare remote | the three sentinels are written (the `yes` claims) |
| J2 | Colocated jj workspace with the tracked `.githooks` and a relative `core.hooksPath` in `.git/config` | none |
| J2b | The same with an absolute `core.hooksPath` | none |
| J3 | `jj commit -m x` in J2 and J2b | which deciding sentinels (`pre-commit`, `commit-msg`) exist |
| J4 | `jj bookmark create b -r @-`, `jj git remote add origin <bare>`, `jj git push --bookmark b` in J2 and J2b | which deciding sentinel (`pre-push`) exists; precondition: the bare remote holds the pushed commit, otherwise the step is invalid |
| J7 | After J2 to J4, a positive control inside each fixture: run the three hook files through Git in that fixture (`git hook run`, fixture only) | each sentinel written, proving the fixture was still valid |

Rules: the script records per step the Git version, the jj version and the `git` version jj spawns (`git --version` on its `PATH`). It exits 2 when a control or precondition fails (the fixture is invalid and nothing is concluded). For a claim `no` or `yes`, a deciding sentinel written (for `no`) or absent (for `yes`) in any deciding step contradicts the claim and the script exits 1; if the deciding steps disagree with each other within a lane (a partial outcome inside one step, for example `pre-commit` written and `commit-msg` absent in J3, counts as a disagreement), the script exits 1 unless the catalog already states `unverified` with `runs_hooks: unknown` for that claim, in which case it records the disagreement and exits 0. The script sees one lane only (a matrix job cannot see the other); the comparison between lanes (jj minimum with Git minimum, jj current with Git current) is the manual step of T022 from the two lane records. A claim of `unknown` never fails by itself. The catalog changes only by a manual edit with the lane record: all deciding sentinels absent in every deciding step on both lanes sets `no` at `measured`, all written sets `yes` at `measured`, a disagreement sets `unverified` with `runs_hooks: unknown`; if a claim changes, the findings behavior, this specification, the documentation and the tests change in the same change set. J0 is a control and the Git measurement at once: failing to write the three sentinels is an invalid fixture (exit 2) and never a contradiction; its outcomes are recorded and T022 gives the Git claims their `measured` level on the lanes it ran on.

### Refusal table (normative)

| `error.code` | Result `status` | `backend` | When |
| --- | --- | --- | --- |
| `unsupported-backend` | `refused` | `gitbutler` | GitButler-managed workspace; the message names the capability gap |
| `ambiguous-backend` | `refused` | `ambiguous` | ownership markers conflict |
| `bare-repository` | `refused` | `git` | no worktree, so no client-side `commit` |
| `not-repository` | `refused` | `unknown` | no repository at the path |
| `jj-unavailable`, `jj-sandbox-unavailable`, `jj-version-unsupported`, `jj-timeout`, `jj-output-limit` | `error` | `jj` | existing jj session failures, codes reused verbatim |
| `invalid-path`, `metadata-unreadable`, `invalid-metadata`, `git-unavailable`, `git-failed`, `changed-repository`, `canceled` | `error` | as `Inspect` reports (`git` or `unknown`); `jj` when raised after a `.jj` marker was found | the existing ownership decision's codes (and, on the jj path, `git-unavailable`, `git-failed`, `invalid-metadata`, `canceled`), passed through verbatim |
| `hooks-failed` | `error` | `git` or `jj` | a failure specific to this report: a failed or unparseable hooks read, a failure of the ownership gate or of an included configuration file that occurs after `Inspect` succeeded or on the jj path (on a Git-owned path `Inspect` normally fails first with `git-failed`), or a discovered Git directory that differs from the workspace's `.git` |

### Findings (normative)

Codes are append-only within `schema_version: 1`. Severities are fixed. A finding carries `operation` (`commit`, `push` or null) and `message`. Each operation has at most one finding of each bypass code, naming the affected hooks. Derivation, in one place: `guards-bypassed-on-backend` when `runs_hooks` is `no` and an active blocking guard exists; `guards-may-be-bypassed` when `runs_hooks` is `unknown` and an active blocking guard exists; neither when `guards_evaluated` is false; `backend-hook-behavior-unverified` once when any operation claim used has a level other than `measured` after the coverage rule; `no-active-guards` once when `guards_evaluated` is true, no active blocking guard exists for either operation and `config_hooks_detected` is false.

| Code | Severity | Meaning |
| --- | --- | --- |
| `guards-bypassed-on-backend` | error | an active blocking guard exists for an operation whose backend claim is `runs_hooks: no`; the evidence level is in the message |
| `hooks-path-invalid` | error | `core.hooksPath` is a valueless key or a value Git cannot expand; Git fails every hook-running command |
| `hooks-limit` | error | a bound was exhausted; result incomplete |
| `hooks-state-changed` | error | state changed during the report; result incomplete |
| `guards-may-be-bypassed` | warning | an active blocking guard exists for an operation whose backend claim is `runs_hooks: unknown` |
| `backend-hook-behavior-unverified` | warning | a claim used in this result is not `measured` for the detected version |
| `hooks-dir-absent` | warning | the effective hooks directory does not exist (including the default `<git-dir>/hooks` of a fresh repository); no directory hook runs |
| `hooks-path-null-device` | warning | `core.hooksPath` is the null device; no directory hook runs |
| `hooks-path-empty` | warning | `core.hooksPath` is the empty string; Git runs no directory hook |
| `hooks-dir-unusable` | warning | the effective hooks path is not a directory or lacks search permission for the real user |
| `hook-ignored` | warning | a hook entry fails `access(X_OK)` for the real user or is a dangling or looping symlink; Git skips it |
| `hook-not-runnable` | warning | an executable hook entry is not a regular file; Git fails the operation |
| `config-hooks-not-evaluated` | warning | configuration hooks naming an operation hook (or a valueless `hook.*.event` key) exist; they run in addition to directory hooks and are not evaluated |
| `workspace-has-no-git-configuration` | info | a jj workspace without a `.git` entry; the guard set is not evaluated; jj's `git` subprocess may still use Git configuration and the internal store's hooks, which are not examined |
| `no-active-guards` | info | see the derivation above |

### Limits (normative)

| Limit | Value | On exhaustion |
| --- | --- | --- |
| total deadline | 10 seconds | `hooks-limit` |
| per Git process | 5 seconds | `hooks-limit` |
| bytes per Git stream (existing runner) | 65,536 | `hooks-limit` |
| result bytes | 262,144 | `hooks-limit` |
| bytes per reported text value | 4,096 | `hooks-limit` |

### Key Entities

- Hooks configuration: effective `core.hooksPath` value, scope, state, resolved directory, whether configuration hooks name an operation hook.
- Hook entry: one of six names; kind, state, mode, symlink dangling; never content.
- Operation report: operation, backend command, `runs_hooks`, evidence, exceptions, per-hook role, state and bypass.
- Evidence: level, claim, sources, detected and measured versions.
- Finding: stable code, severity, operation, bounded message.
- Hooks result: identity, configuration, hooks, operations, findings, summary, effects, limits, completeness.

## Success Criteria

- **SC-001**: In a deterministic WorkHarbor-shaped fixture (tracked `.githooks` with `pre-commit`, `commit-msg`, `pre-push`; `core.hooksPath` in the local configuration) the command reports the three guards active in a Git worktree, in a linked worktree and in a subdirectory, and a real `git commit` and `git push` fire exactly the reported hooks (sentinels).
- **SC-002**: In a colocated jj workspace with the same hooks, the report states the jj claims at their evidence levels and raises the findings of User Story 2; no jj claim is labelled `measured` before the lane has recorded it.
- **SC-003**: Every acceptance scenario and edge case above has a deterministic test; skipped or missing execution never counts as acceptance.
- **SC-004**: Effect sentinels prove that no hook, configuration hook, helper or filter ran, no hook entry was opened (on Linux an inotify watch on the hooks directory sees no `IN_OPEN` or `IN_ACCESS` of the six names, and a FIFO at each name never blocks; macOS is not proven and is recorded as such), refs, index, configuration, hooks directory and worktree files are unchanged, no lock remains, and for jj the working copy and operation log are unchanged.
- **SC-005**: All success, incomplete, refused and error fixtures validate offline against `hooks.schema.json`, and existing contract fixtures remain valid.
- **SC-006**: The conformance procedure ran on the minimum and current Git and jj lanes on `ubuntu-24.04` amd64, J0 and J7 passed on each, and the recorded outcomes agree with the evidence catalog; an unavailable lane never counts as a pass. arm64 and darwin results are local evidence only.

## Scope and Dependencies

This is a bounded read-only disclosure slice on existing native ownership, Git isolation and the jj sandbox session. Deferred to a later "hook hygiene" specification: hook safety facts (ownership, permissions, symlinks outside the repository), directory listing and misnamed hooks, tracking, the full 28-name catalog, level-by-level `core.hooksPath` reporting, caller-environment reporting, configuration-hook evaluation (engine, enabled state), `--strict` and a dedicated exit status, further operations (merge, rebase, checkout), and further conformance (`--no-verify` matrix, non-colocated and secondary jj sentinels). Out of scope: running or testing hooks, reading hook content, installing, editing or bypassing hooks, a hook-compatibility layer for jj, jj-native or third-party hook mechanisms, server-side hooks, GitButler hook behavior, Windows. No dependency on Spec 021 (exit statuses 0, 1 and 2 only; the small text-escaping helper is implemented here or shared if Spec 021 has landed). Constitution 1.1.0 applies with no amendment: the feature is read-only, discloses its process use, and states the jj capability and evidence level instead of claiming equivalence.
