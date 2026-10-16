# Feature Specification: Hook and bypass disclosure

**Feature Branch**: `codex/024-hook-disclosure`  
**Created**: 2026-10-16  
**Status**: Draft — revised after independent analysis; implementation pending. The jj behavior this feature reports is `unverified` until the conformance lane in FR-017 has measured it.
**Input**: A read-only `stw hooks` command that reports which commit and push guards (Git hooks) a repository configures, whether each is active and safe, and, for the backend that owns the workspace, whether a `commit` or `push` operation executes them, so that a "guards bypassed on this backend" finding is possible.

## User Scenarios & Testing

### User Story 1 — See which guards are configured and active (Priority: P1)

A maintainer or agent in a Git repository whose `core.hooksPath` points at a tracked `.githooks` directory (the WorkHarbor setup: secret scanning in `pre-commit`, message linting in `commit-msg`, checks in `pre-push`) asks which guards actually exist and would run.

**Why this priority**: Everything else builds on a trustworthy answer to "what guards does this repository have". A hook that is present but not executable, or a `core.hooksPath` that points nowhere, silently disables a guard.

**Independent Test**: In temporary repositories, configure `core.hooksPath` to a tracked directory containing `pre-commit`, `commit-msg` and `pre-push` hooks; compare the reported configuration with what a real `git commit` and `git push` run (sentinel files), including a non-executable hook, an absent directory and `/dev/null`.

**Acceptance Scenarios**:

1. **Given** `core.hooksPath` set to a relative directory in the repository configuration, **When** `stw hooks` runs from the root or a subdirectory, **Then** the effective hooks directory is reported as one absolute path resolved the way Git resolves it (relative to the worktree root in use), the configuration level that set it is named, and each hook file present is listed by name, kind, executable state, mode and size, never its contents.
2. **Given** the three WorkHarbor hooks, **When** the command runs, **Then** the `commit` operation lists `pre-commit` and `commit-msg` as active blocking guards and `post-commit` as absent, and the `push` operation lists `pre-push` as an active blocking guard.
3. **Given** `core.hooksPath` is the null device, an empty value, an absent directory, a non-directory or a directory without search permission, **When** the command runs, **Then** the finding `hooks-path-null-device`, `hooks-path-empty`, `hooks-dir-absent` or `hooks-dir-unusable` is reported and no directory hook is called active. **Given** a valueless `core.hooksPath` key or a value Git cannot expand (`~nosuchuser/x`), **Then** the state is `invalid` with `hooks-path-invalid`, because Git fails every hook-running command.
4. **Given** a hook entry that fails `access(X_OK)` for the real user (a file without the executable bit, a dangling or looping symlink, a directory or FIFO without the executable bit), **When** the command runs, **Then** it is `ignored` with `hook-ignored`, because Git skips it.
5. **Given** an executable entry that is not a regular file (a directory with the executable bit, or a symlink to one), **When** the command runs, **Then** it is `not-runnable` with `hook-not-runnable`, because Git fails the operation.
6. **Given** `extensions.worktreeConfig` with `core.hooksPath` set at worktree level in a linked worktree, **When** the command runs there, **Then** every level that set the value is listed, the worktree level wins, and `hooks-path-overridden` is reported.
7. **Given** Git configuration hooks (`hook.<name>.event`), **When** the command runs, **Then** each is listed by name, events, enabled state, whether a command is configured, and the scopes of its keys, never by its command text, and `config-hooks-present` is reported; an event without a command raises `config-hook-incomplete`, because Git fails the operation.

### User Story 2 — Learn which guards a backend bypasses (Priority: P1)

A maintainer considering a move to jj, or an agent about to run `jj commit` or `jj git push` in a colocated repository whose Git hooks hold the guards, asks whether those guards would run.

**Why this priority**: This is the motivating failure. jj does not run Git hooks as far as its documentation says, so a commit made with jj may bypass every guard without any warning.

**Independent Test**: Run the command in a colocated jj workspace whose Git configuration has the WorkHarbor hooks; the result must say, per operation, that the owning backend is jj, what is claimed, at which evidence level, and which active guards are therefore bypassed or possibly bypassed. The conformance lane of FR-017 measures the claim.

**Acceptance Scenarios**:

1. **Given** a colocated jj workspace with active blocking hooks in its Git configuration, **When** `stw hooks` runs, **Then** the backend is `jj`, the `commit` operation (`jj commit`) states `runs_hooks: no` with evidence level `documented`, each active guard is `bypassed: true`, and `guards-bypassed-on-backend` (error) and `backend-hook-behavior-unverified` (warning) are reported.
2. **Given** the same workspace, **When** the `push` operation (`jj git push`) is reported, **Then** it states `runs_hooks: unknown` with evidence level `unverified`, each active guard is `bypassed: null`, and `guards-may-be-bypassed` (warning) is reported, because jj documents that remote interactions spawn a `git` subprocess and whether that runs `pre-push` is not established.
3. **Given** the same hooks in a Git-owned repository, **When** the command runs, **Then** both operations state `runs_hooks: yes`, the documented and measured exceptions (such as `--no-verify`) are listed per hook, and no bypass finding is raised.
4. **Given** a jj workspace with no `.git` entry at its root (non-colocated, or a secondary workspace created without Git colocation), **When** the command runs, **Then** no Git configuration is read, the guard set is not evaluated (`guards_evaluated: false` on each operation, no bypass finding), `workspace-has-no-git-configuration` states that jj's `git` subprocess may still consult system and global Git configuration and the internal store repository's hooks, which this report does not examine, and the jj claims are still reported with their evidence level. **Given** a secondary workspace of a colocated repository, which jj documents as a Git worktree by default (a `.git` gitfile), **Then** it is handled as colocated: facts are read through Git discovery and the gitfile's target is verified.
5. **Given** a detected backend version outside the inclusive range that the conformance lane measured, **When** the report is produced, **Then** a claim whose catalog level is `measured` is reported as `documented` and `version_covered` is false.
6. **Given** no active blocking guard for an operation, **When** the report is produced, **Then** no bypass finding is raised for it and `no-active-guards` (info) is reported when that holds for both operations.

### User Story 3 — Run safely on a hostile repository (Priority: P1)

An agent runs the report in a checkout whose configuration, hook names, hook directory or symlinks were written by untrusted code.

**Independent Test**: Use repositories with hooks that write a sentinel, FIFOs and devices where hooks are expected, symlinked hook files and directories pointing outside, world-writable and foreign-owned entries, `core.hooksPath` values with control characters, non-UTF-8 bytes, `~user` forms, very long values and includes of unreadable files; verify that no sentinel runs, nothing is opened, and output stays bounded and escaped.

**Acceptance Scenarios**:

1. **Given** hooks and `hook.<name>.command` entries that would write a sentinel, **When** the command runs, **Then** no hook, no configured hook command, no `git hook run`, no filter, helper or alias executes, and no file of the repository, hook directory or Git configuration is written.
2. **Given** any hook path, **When** the command runs, **Then** the path is never opened: only `lstat`, `readlink`, `access` and a bounded directory listing are used. A FIFO named like a hook does not make the command block.
3. **Given** a hook symlink whose target is outside the worktree, the Git directory and the hooks directory, or a hooks directory that resolves outside them, **When** the command runs, **Then** `hook-symlink-outside` (error) is reported and the target is never followed beyond classification.
4. **Given** a hook file or hooks directory that is world-writable or owned by neither the effective user nor root, **When** the command runs, **Then** `hook-unsafe-permissions` or `hook-unsafe-owner` (error) is reported with the subject.
5. **Given** repository text such as `core.hooksPath` values, origins, unrecognized file names and configuration hook names, **When** it is rendered, **Then** it is treated as data and cannot produce terminal control effects in text output.
6. **Given** a hooks directory outside the worktree and the Git directory, **When** the command runs, **Then** only the fixed catalog names are examined there; the directory is not listed (FR-008).

### User Story 4 — Rely on one bounded contract on each backend (Priority: P1)

Humans and agents use the same result, and every Tier 1 backend states its capability explicitly.

**Independent Test**: Validate success, incomplete, refused and error fixtures against the new schema; run the command in ordinary Git, a linked Git worktree, colocated jj, standalone jj, secondary jj, ambiguous and GitButler workspaces.

**Acceptance Scenarios**:

1. **Given** a Git-owned worktree, **When** `stw hooks [PATH] [--json]` runs, **Then** human output and schema-versioned JSON come from one result and the command exits 0 even when findings exist.
2. **Given** `--strict` and a complete result with an error finding, **When** the command runs, **Then** the full result is still emitted and the exit status is 3; without error findings it is 0.
3. **Given** a jj-owned workspace, **When** the command runs, **Then** it uses the jj adapter under the existing mandatory read-only sandbox for its jj facts, and an unavailable sandbox, missing jj or unsupported jj version produces the existing actionable error code, never an unsandboxed retry and never a report from Git alone.
4. **Given** a GitButler-managed, ambiguous, bare or non-repository path, **When** the command runs, **Then** it refuses with the code of the refusal table.

### User Story 5 — Treat jj hook behavior as a measurement (Priority: P2)

A maintainer needs to know whether the jj claim in the report is established or only documented, and needs the lane that will establish it.

**Independent Test**: Review the conformance procedure of FR-017 and the evidence catalog; the procedure is deterministic, uses a local bare remote, and includes a positive control proving that the sentinel hooks fire under plain Git.

**Acceptance Scenarios**:

1. **Given** the minimum and current jj lanes, **When** the hook fixture runs `jj commit` and `jj git push`, **Then** each hook sentinel is either written or absent, and the outcome is recorded; a result that differs from the catalog fails the lane until the catalog and the documentation are changed.
2. **Given** the catalog before measurement, **When** the report is produced, **Then** no jj claim is labelled `measured`.

### Edge Cases

- Linked Git worktrees; main worktree with `extensions.worktreeConfig`; `core.hooksPath` set at system, global, local and worktree level (the last value wins, as in Git); a value that comes from an included file (`include`, `includeIf`).
- `core.hooksPath` that is `~/...` (expanded by Git), relative with `..`, absolute, `/dev/null`, empty, a file, a FIFO, unreadable, or a symlink.
- Hook names with the `.sample` suffix are never active and never counted as unrecognized.
- Hook files copied by `git init --template` or `init.templateDir` are ordinary files; their provenance is not recorded by Git and is not claimed.
- Hooks that exist in the default `<git-dir>/hooks` while `core.hooksPath` points elsewhere (inert).
- Server-side hook names in a client repository (`pre-receive`, `update`, `post-receive`, `post-update`, `proc-receive`, `push-to-checkout`): recognized, listed with `scope: server`, never part of `commit` or `push`.
- A hook whose interpreter is missing or whose content is unreadable: not detectable without reading content; disclosed as a limit, never guessed.
- A hook script that honors an environment variable that disables itself (for example a framework skip variable): not detectable; disclosed as a limit.
- Caller environment variables that change Git's behavior for a later command (`GIT_CONFIG`, `GIT_CONFIG_COUNT`, `GIT_CONFIG_PARAMETERS`, `GIT_CONFIG_GLOBAL`, `GIT_CONFIG_SYSTEM`, `GIT_CONFIG_NOSYSTEM`, `GIT_DIR`, `GIT_WORK_TREE`, `GIT_COMMON_DIR`, `GIT_CEILING_DIRECTORIES`, `GIT_DISCOVERY_ACROSS_FILESYSTEM`; variables read by hook frameworks are not examined): the command never applies them, and names (not values) of those present are reported with `caller-environment-overrides`.
- Per-command overrides (`git -c core.hooksPath=/dev/null commit`, `--no-verify`, `-n`) are listed as exceptions, never detected.
- Hook directory or file changing while the command runs; more than 256 entries in a hooks directory; more than 64 configuration hooks; very long values.
- A colocated jj repository whose Git configuration cannot be read, or whose backing Git directory fails Git's ownership check: `hooks-failed`.
- A jj secondary workspace is colocated when it has a `.git` entry (gitfile into the common directory's `worktrees/`) and non-colocated otherwise.
- Hooks in the default `<git-dir>/hooks` while `core.hooksPath` points elsewhere are inert and not examined; the notes say so.
- A hooks directory that is a FIFO, a file or a device: state `not-directory`; the directory is never opened.

## Requirements

### Functional Requirements

- **FR-001**: Provide `stw hooks [PATH] [--json] [--strict]`. `PATH` defaults to the current directory. The command is read-only and non-interactive: no prompt, editor or pager. `--strict` has the semantics of FR-016.
- **FR-002**: Select the backend by ownership using the existing detection. A jj-owned workspace (standalone, colocated, secondary) uses the jj adapter; ambiguous ownership refuses before any process starts; a GitButler-managed workspace refuses with `unsupported-backend`; a Git-owned worktree, including a linked worktree, uses the Git adapter. Never substitute Git for jj ownership: in a jj-owned workspace Git is consulted only for the read-only configuration reads of FR-005 and only when a `.git` entry exists at the workspace root. Those Git reads run outside the jj sandbox and are disclosed as such (`git_processes`).
- **FR-003**: For the Git adapter, determine the configured and effective `core.hooksPath`: every value with its configuration scope (`system`, `global`, `local`, `worktree`) as reported by `git config --show-scope --show-origin`, the effective value being the last one, whether `extensions.worktreeConfig` is enabled, and the effective hooks directory as the absolute path from `git rev-parse --path-format=absolute --git-path hooks`. Values are read with `config --get-regexp '^core\.hookspath$'` so that a valueless key (Git then fails every hook-running command, state `invalid`) is distinguished from an empty value (`empty`: `--git-path hooks` fails for it while Git itself runs no hooks). A non-empty value for which `--git-path hooks` fails is `invalid`. At most 16 level records are reported. System and global configuration are in scope as read-through (justified in research.md): Git applies them to the user's later command, so omitting them would report a guard as absent that Git runs. Only the scope is reported as the origin of system and global values; the origin path is reported for repository-level values. Values themselves are reported as read and may contain home-directory paths.
- **FR-004**: For the effective hooks directory, report kind (directory, symlink, other), the resolved location, whether it lies inside a confinement root, ownership (the effective user or root counts as safe, as in Git's ownership rule), group- and world-writability, and the number of entries scanned. Examine only the 28 catalog names of the hook catalog table with `lstat` (plus `readlink` and `access(X_OK)` for the real user, as Git does); never open a hook path, never read content, never execute. The one open is the bounded listing of a confined directory (`O_DIRECTORY|O_NONBLOCK|O_NOFOLLOW`, at most 257 entries read). Confinement roots are the worktree root, the Git directory and the common Git directory (a linked worktree's default hooks directory is in the common directory). When the directory is confined, list at most 256 entries to report up to 16 names that are neither catalog names nor `*.sample` (`hook-name-unrecognized`); outside those locations the directory is not listed.
- **FR-005**: For a jj-owned workspace with a `.git` entry at its root (a directory, or a gitfile for a worktree-backed secondary workspace, both validated by the existing jj ownership detection), read the same Git facts as FR-003 and FR-004 with the isolated, read-only Git reads of the process allowlist in research.md. Because `git config` reads do not apply Git's ownership check (measured: they silently return partial data), every read is bracketed by `rev-parse --absolute-git-dir`, which does apply it; a failure of that gate is `hooks-failed`, and `git config` exit status 1 means "unset" only after the gate passed (other non-zero statuses are `hooks-failed`). The same gate applies to the Git-owned adapter. The discovered Git directory must equal the directory the `.git` entry designates. A jj workspace without a `.git` entry reads no Git configuration and reports `workspace-has-no-git-configuration`. jj facts come only from the existing sandboxed jj session (FR-006).
- **FR-006**: For a jj-owned workspace, obtain jj facts only through the existing mandatory read-only sandbox session (sandbox probe, `--version`, supported-version check) and report the jj version it observed. No other jj command is added. The operation decides jj ownership from the existing filesystem-only detection and then runs only the sandbox probe and `--version`; it does not run the identity queries of `stw inspect`. Failures use the existing codes `jj-unavailable`, `jj-sandbox-unavailable`, `jj-version-unsupported`, `jj-timeout`, `jj-output-limit`, `invalid-metadata` and `canceled` with status `error` and no unsandboxed retry.
- **FR-007**: Classify each hook entry present (client or server scope) by the hook catalog, in this order: `ignored` when `access(X_OK)` fails for the real user (no executable bit, dangling or looping symlink; Git skips it, measured); otherwise `not-runnable` when the entry or its symlink target is not a regular file (an executable directory: Git fails the operation, measured); otherwise `active`. Report mode, size, ownership, world-writability and, for a symlink, whether the target lies inside the repository and whether it dangles. Report `tracked` (`tracked`, `untracked`, `unknown`, `not-applicable`) from the index when the hooks directory is inside the worktree.
- **FR-008**: Examine configuration hooks (`hook.<name>.event`, `hook.<name>.enabled`) by reading configuration only, reporting name, events, enabled state, whether a command is configured and the scope of each key, never the command text (it is configuration that may hold secrets; only key names of `.command` are read) and never running `git hook run`. Their effect is reported as `engine: supported` only for Git versions within the measured range of the claim `git-config-hooks`, and `unknown` otherwise; entries are listed in both cases. Entries count as active guards when the engine is `supported`; when it is `unknown` an entry whose event is a catalog hook of an operation appears in that operation with state `unknown` and `bypassed: null` (`true` is never used for an unknown engine), so `guards-may-be-bypassed` is raised and `no-active-guards` is not. Measured on Git 2.56.0: they run in addition to directory hooks and still run when `core.hooksPath` is the null device; an event without a command makes the operation fail.
- **FR-009**: Report two operations, `commit` and `push`, for the owning backend, from the evidence catalog table: the backend command, `runs_hooks` (`yes`, `no`, `unknown`), the evidence level with sources, whether the detected version is covered, the documented and measured exceptions, and, per catalog hook of the operation, its role, state (`active`, `unknown`, `ignored`, `not-runnable`, `disabled-by-path`, `absent`), source (`hooks-dir`, `config` or `both`) and `bypassed` (`true` when `runs_hooks` is `no` and the hook is active; `null` when the hook is active and `runs_hooks` is `unknown`, or when its state is `unknown` (a configuration hook while the engine is `unknown`) under any `runs_hooks`; otherwise `false`). An active directory or supported configuration hook beats `disabled-by-path`; a directory hook and a configuration hook for the same event give source `both`, and `guards_evaluated` (false when no Git configuration applies to the workspace, in which case the hook list is empty, no bypass finding is raised and `workspace-has-no-git-configuration` says why).
- **FR-010**: Evidence levels are exactly `measured` (a conformance fixture of this release observed the behavior on both the minimum and the current version of the backend tuple, with the same outcome, and the detected versions lie inside that inclusive range; disagreeing lanes leave the claim `unverified`; the `jj-push` tuple is jj plus the Git on `PATH` that jj spawns), `documented` (only a vendor statement, or a measurement that does not cover the detected version) and `unverified` (no sufficient statement). The evidence catalog is the only place a claim and its level are defined. The report carries the level with every claim and never states a claim without it.
- **FR-011**: Never execute a hook, a configuration hook command, `git hook run`, a filter, a helper, an alias, a pager or an editor; never read the content of a hook file; never write to the repository, the Git directory, the configuration or the jj repository; make no network call. Treat hook paths, names, configuration values, origins and symlink targets as untrusted data. The jj working copy and operation log are unchanged (effect sentinel).
- **FR-012**: Run Git with the existing isolation prefix (`--no-replace-objects`, `core.fsmonitor=false`, `GIT_*` environment removed, optional locks, prompts, lazy fetch and pager disabled) for the allowlisted read-only invocations only. The caller's `GIT_*` environment is removed, as in the existing runner, while `HOME` and `XDG_CONFIG_HOME` are kept and applied (they locate the global configuration); the report describes configuration files, not the caller's `GIT_*` environment; names of caller variables that would change Git's behavior are reported (`caller-environment-overrides`).
- **FR-013**: Bound the operation by the limits table. Exhausting a bound marks the result incomplete with `hooks-limit`; it is never reported as a complete success.
- **FR-014**: Detect concurrent change: resample the effective `core.hooksPath` value, the hooks directory identity (device, inode, modification time) and each present hook's `lstat` metadata before success; a difference marks the result incomplete with `hooks-state-changed`.
- **FR-015**: Provide understandable human output and a new `schemas/v1/hooks.schema.json` result with `schema_version: 1`, the finding codes and severities of the findings table, the evidence catalog values, a summary, explicit effects, limits and diagnostics, rendered from one result. Existing published contracts (inspect schema 1 and 2, inventory, tidy, workspace, hosting, version) do not change.
- **FR-016**: Exit status, with this precedence: usage error 2; refusal, operational failure, cancellation or an incomplete result 1; otherwise 3 only with `--strict` and at least one `error` finding in a complete result; otherwise 0. On status 3 one line on standard error names the number of error findings; standard output still carries the full result. The strict-findings error type is shared with `stw pins` (Spec 021).
- **FR-017**: Treat jj hook behavior as a measurement. Add a conformance script and lane step that, on the existing minimum and current jj lanes and the minimum and current Git lanes on `ubuntu-24.04` amd64, builds the fixture and procedure of the conformance procedure table, records each outcome, and fails when an outcome differs from the evidence catalog. Until a lane has recorded an outcome, the catalog states `documented` or `unverified` and the report must not say `measured`.
- **FR-018**: Declare the capability matrix in the backend contract: Git supported; jj supported with the stated evidence levels; GitButler explicitly unsupported; no equivalence between backends is claimed.
- **FR-019**: Require failing behavior tests before implementation, independent review of the specification, plan and tasks, and conformance with effect sentinels (no hook ran, no write, no file opened) on the existing lanes. Other platforms provide local evidence only until CI covers them.

### Hook catalog (normative)

The 28 names of the `githooks(5)` manual page found on the author's host (a Git 2.47.3 manual; the installed Git 2.56.0 ships none) are recognized; verifying the list against the 2.46.0 and 2.56.0 documentation is task T027, and a name added or removed there changes this table only. Roles and operations apply only to the rows below; every other client-side name is listed when present, with state, and has no operation. "Skipped by `--no-verify`" is measured on Git 2.56.0 (research.md, Git measurements 1 and 2).

| Hook | Scope | Role | Operation | Skipped by `--no-verify` |
| --- | --- | --- | --- | --- |
| `pre-commit` | client | blocking | `commit` | yes |
| `prepare-commit-msg` | client | blocking | `commit` | no |
| `commit-msg` | client | blocking | `commit` | yes |
| `post-commit` | client | observer | `commit` | no |
| `pre-push` | client | blocking | `push` | yes |
| `reference-transaction` | client | blocking | `commit`, `push` | no |
| `applypatch-msg`, `pre-applypatch`, `post-applypatch`, `pre-merge-commit`, `pre-rebase`, `post-checkout`, `post-merge`, `pre-auto-gc`, `post-rewrite`, `post-index-change`, `sendemail-validate`, `fsmonitor-watchman`, `p4-changelist`, `p4-prepare-changelist`, `p4-post-changelist`, `p4-pre-submit` | client | listed only | none | not assessed |
| `pre-receive`, `update`, `proc-receive`, `post-receive`, `post-update`, `push-to-checkout` | server | listed only | none | not assessed |

A guard is an active hook of role `blocking`. `reference-transaction` is blocking because Git aborts the operation when it exits non-zero in the `preparing` or `prepared` state (documented in `githooks(5)`; its abort behavior is not measured here).

### Evidence catalog (normative)

| Claim | Backend | Operation and command | `runs_hooks` | Level at specification time | Sources |
| --- | --- | --- | --- | --- | --- |
| `git-commit` | Git | `commit`: `git commit` | `yes` | `documented`; promoted to `measured` by FR-017 | `githooks(5)`; measurement 1 on Git 2.56.0 |
| `git-push` | Git | `push`: `git push` | `yes` | `documented`; promoted to `measured` by FR-017 | `githooks(5)`; measurement 2 on Git 2.56.0 |
| `git-config-hooks` | Git | configuration hooks (`hook.<name>.*`) are honored | `yes` | `unverified` (its measured range starts at the lowest lane version that supports the feature, recorded by the lane; one level; the author's single measurement 10 on 2.56.0 does not make it `documented`); promoted to `measured` by FR-017 with a range | measurement 10 on Git 2.56.0 |
| `jj-commit` | jj | `commit`: `jj commit` | `no` | `documented` | docs.jj-vcs.dev git-compatibility, "Hooks: No"; issue jj-vcs/jj#405 open |
| `jj-push` | jj | `push`: `jj git push` | `unknown` | `unverified` | the same statement, and docs.jj-vcs.dev config: "Git remote interactions are handled by spawning a `git` subprocess"; whether that subprocess runs `pre-push` is not established |

Exceptions listed with each Git claim, all measured on Git 2.56.0: `no-verify` (skips `pre-commit` and `commit-msg` on commit and `pre-push` on push; `prepare-commit-msg`, `post-commit` and `reference-transaction` still ran); `hooks-path-override` (`git -c core.hooksPath=/dev/null` ran no hook); `caller-config-environment` (documented only); `not-executable` (hint, hook skipped). No exception is listed for jj; the catalog states `native_hook_mechanism: none-documented` for jj (issue #405 open, access date in research.md).

Each claim has exactly one level. The jj behavior is `unverified` as a measurement until a lane records it; `documented` for `jj-commit` means only that a vendor statement exists. A claim reads `measured` in the report only when the catalog marks it measured for its backend tuple (Git alone; jj alone for `jj-commit`; jj plus the Git on `PATH` for `jj-push`) and every detected version is within the inclusive measured range; otherwise it reads the catalog's non-measured level and `version_covered` is false. Versions are compared as numbers after removing a `-<40 hex>` build suffix, as the existing jj version check does.

### Conformance procedure for jj hook behavior (normative)

A disposable fixture is built under a temporary directory with an isolated home, no user or system configuration, a configured author and committer identity for both Git and jj, and a sentinel directory outside every repository. jj runs directly in the disposable fixture, not in the read-only sandbox of `stw`. Each step writes to its own sentinel file, truncated before the step, so setup and earlier steps cannot be attributed to it. Hooks `pre-commit`, `commit-msg`, `prepare-commit-msg`, `post-commit`, `reference-transaction` and `pre-push` each append their name to a sentinel file; a configuration hook (`hook.sentinel.event`, Git lanes whose version supports it) does the same.

| Step | Action | Recorded outcome |
| --- | --- | --- |
| J0 | Positive control: plain Git clone with the hooks via `core.hooksPath`; `git commit` and `git push` to a local bare remote | every expected sentinel is written, proving the hooks are installed and executable |
| J1 | Colocated jj workspace on the same hooks (`.git/hooks`) | none |
| J2 | Same with a tracked `.githooks` and `core.hooksPath` in `.git/config` (the WorkHarbor shape) | none |
| J3 | `jj commit -m x` in J1, J2 and J2b (J2 with an absolute `core.hooksPath`) | which sentinels exist |
| J4 | `jj bookmark create b -r @-`, `jj git remote add origin <bare>`, `jj git push --bookmark b` | which sentinels exist; precondition: the bare remote holds the pushed commit, otherwise the step is invalid |
| J5 | Non-colocated jj workspace (`jj git init --no-colocate`) with sentinel hooks installed in its internal store repository's hooks directory and a global `core.hooksPath` (isolated `HOME`), then J3 and J4 | which sentinels exist (informs the unevaluated non-colocated case) |
| J6 | Secondary workspaces of J1 by default and with `--no-colocate` (`jj workspace add`), recording whether each has a `.git` entry, then J3 | which sentinels exist; `.git` shape |
| J7 | After J1 to J6, a positive control inside each fixture: run the hook files through Git in that fixture (`git hook run` for each name, in the fixture only) | each sentinel written, proving the fixture was still valid |

Hooks have the absolute sentinel path baked in (no environment variable is needed), so a missing sentinel cannot come from a dropped variable. The comparison is per fixture and per claim: for a claim `no`, any sentinel written by that fixture's step contradicts it; for `yes`, an absent expected sentinel contradicts it; for `unknown`, the outcome is recorded and never fails. The deciding steps are J3 in J1, J2 and J2b for `jj-commit` and J4 in J1, J2 and J2b for `jj-push`; J5 and J6 are recorded as information and never decide. If the deciding steps disagree with each other or between the two lanes, the script exits 1 and the claim is set to `unverified` with `runs_hooks: unknown`. A claim of `unknown` is resolved only by consistent outcomes: all deciding steps in both lanes absent sets `no`, all written sets `yes`, in the catalog edit of T024 with level `measured`; during the run it never fails. J2b uses an absolute `core.hooksPath`, so a subprocess with another working directory still reaches the hooks. The script prints a machine-readable record per step with the Git version, the jj version and the `git` version jj spawns (`git --version` on its `PATH`), exits 2 when a control or precondition fails (the fixture is invalid and nothing is concluded), exits 1 when any outcome differs from the evidence catalog, exits 0 otherwise, and writes the record to the verification note. A sentinel written in a deciding step contradicts a `no` claim: the catalog is then changed to `runs_hooks: yes` for that operation, a bypass finding no longer applies there, and the specification, documentation and tests change before release. The same script records the Git outcomes (J0 equivalents with `--no-verify`) for the `measured` promotion of the Git claims.

### Refusal table (normative)

| `error.code` | Result `status` | `backend` | When |
| --- | --- | --- | --- |
| `unsupported-backend` | `refused` | `gitbutler` | GitButler-managed workspace; the message names the capability gap |
| `ambiguous-backend` | `refused` | `ambiguous` | ownership markers conflict |
| `bare-repository` | `refused` | `git` | no worktree, so no client-side `commit` |
| `not-repository` | `refused` | `unknown` | no repository at the path |
| `jj-unavailable`, `jj-sandbox-unavailable`, `jj-version-unsupported`, `jj-timeout`, `jj-output-limit` | `error` | `jj` | existing jj session failures, codes reused verbatim |
| `invalid-path`, `metadata-unreadable`, `invalid-metadata`, `git-unavailable`, `git-failed`, `changed-repository`, `canceled` | `error` | as `Inspect` reports (`git` or `unknown`) on paths decided by `Inspect`; `jj` on the jj path, where `git-unavailable`, `git-failed`, `invalid-metadata` and `canceled` can also occur | the existing ownership decision's error codes, passed through verbatim; a Git HEAD that changes during that decision blocks the report |
| `hooks-failed` | `error` | `git` or `jj` | a failure specific to this report: a failed or unparseable hooks read, Git's ownership gate, an unparseable included configuration file, or a discovered Git directory that differs from the workspace's `.git`; a missing or too-old Git in a colocated jj workspace is `git-unavailable` or `git-failed` as in the row above |

### Findings (normative)

Codes are append-only within `schema_version: 1`. Severities are fixed. `--strict` fails only on error findings. A finding carries `operation` (or null) and `subject` (a hook name, `directory`, `configuration` or null). The two bypass findings are raised once per operation with `subject` null and name the affected guards in the message; the derivation rules are in the contract.

| Code | Severity | Meaning |
| --- | --- | --- |
| `guards-bypassed-on-backend` | error | an active blocking guard exists for an operation whose backend claim is `runs_hooks: no`; the evidence level is in the finding |
| `hook-symlink-outside` | error | a hook symlink whose target lies outside the confinement roots and the hooks directory, or a hooks directory that is a symlink whose real path leaves the confinement roots |
| `hook-unsafe-permissions` | error | a hook file or the hooks directory is world-writable |
| `hook-unsafe-owner` | error | a hook file or the hooks directory is owned by neither the effective user nor root |
| `hooks-path-invalid` | error | `core.hooksPath` is a valueless key or a value Git cannot expand; Git fails every hook-running command |
| `config-hook-incomplete` | error | a configuration hook has an event and no command; Git fails the operation (measured by the review) |
| `hooks-limit` | error | a bound was exhausted; result incomplete |
| `hooks-state-changed` | error | state changed during the report; result incomplete |
| `guards-may-be-bypassed` | warning | an active blocking guard exists for an operation whose backend claim is `runs_hooks: unknown`, or a configuration hook with an unknown engine exists for the operation under any claim; it may appear together with `guards-bypassed-on-backend` on one operation |
| `backend-hook-behavior-unverified` | warning | a claim used in this result is not `measured` for the detected version |
| `hooks-dir-absent` | warning | the effective hooks directory does not exist; no directory hook runs |
| `hooks-path-null-device` | warning | `core.hooksPath` is the null device; no directory hook runs |
| `hooks-path-empty` | warning | `core.hooksPath` is the empty string; Git runs no directory hook |
| `hooks-dir-unusable` | warning | the effective hooks path is not a directory or lacks search permission for the real user; no directory hook runs |
| `hook-ignored` | warning | a hook entry fails `access(X_OK)` for the real user or is a dangling or looping symlink; Git skips it |
| `hook-not-runnable` | warning | an executable hook entry is not a regular file; Git fails the operation |
| `hooks-path-overridden` | info | a higher-precedence level overrides a lower level's `core.hooksPath` |
| `config-hooks-present` | info | configuration hooks exist; they run in addition to directory hooks |
| `hooks-outside-repository` | info | the effective hooks directory is outside the confinement roots (shared with other repositories); not an error by itself |
| `hooks-tracked` | info | hooks come from repository content, so a checkout without them removes the guard |
| `hook-name-unrecognized` | info | files in a confined hooks directory are not catalog names and not `*.sample` |
| `caller-environment-overrides` | info | the caller environment holds variables that change a later Git command; names only |
| `workspace-has-no-git-configuration` | info | a jj workspace without a `.git` entry; the guard set is not evaluated; jj's `git` subprocess may still use system and global configuration and the internal store's hooks, which are not examined |
| `no-active-guards` | info | no active blocking guard exists for either operation and no configuration hook with an unknown engine exists |

### Limits (normative)

| Limit | Value | On exhaustion |
| --- | --- | --- |
| total deadline | 10 seconds | `hooks-limit` |
| per Git process | 5 seconds | `hooks-limit` |
| bytes per Git stream (existing runner) | 65,536 | `hooks-limit` |
| result bytes | 262,144 | `hooks-limit` |
| directory entries listed | 256 | `hooks-limit` |
| unrecognized names reported | 16 | truncation flag only |
| configuration hooks reported | 64 | truncation flag only |
| bytes per reported text value | 4,096 | the value is replaced by a marker and `hooks-limit` |
| `core.hooksPath` level records | 16 | truncation flag only |

### Key Entities

- Hooks configuration: levels of `core.hooksPath`, effective directory, directory facts, configuration hooks.
- Hook entry: catalog name, scope, kind, state, mode, size, ownership, symlink and tracking facts; never content.
- Operation report: operation, backend command, `runs_hooks`, evidence, exceptions, per-hook role, state and bypass.
- Evidence: level, claim, sources, detected and measured versions.
- Finding: stable code, severity, operation, subject, bounded message.
- Hooks result: identity, configuration, hooks, operations, findings, summary, effects, limits, completeness.

## Success Criteria

- **SC-001**: In a deterministic WorkHarbor-shaped fixture (tracked `.githooks` with `pre-commit`, `commit-msg`, `pre-push`; `core.hooksPath` in the local configuration) the command reports the three guards active in a Git worktree, in a linked worktree and in a subdirectory, and a real `git commit` and `git push` fire exactly the reported hooks (sentinels).
- **SC-002**: In a colocated jj workspace with the same hooks, the report states the jj claims with their evidence levels and raises the findings of User Story 2; no jj claim is labelled `measured` before the lane has recorded it.
- **SC-003**: Every acceptance scenario and edge case above has a deterministic test; skipped or missing execution never counts as acceptance.
- **SC-004**: Effect sentinels prove that no hook, configuration hook, helper or filter ran, no hook path was opened (on Linux an inotify watch on the hooks directory sees no `IN_OPEN` or `IN_ACCESS` of a catalog name, and a FIFO at each catalog name never blocks; macOS is not proven and is recorded as such), refs, index, configuration, hooks directory and worktree files of the repository are unchanged, no lock remains, and, for jj, the working copy and operation log are unchanged.
- **SC-005**: All success, incomplete, refused and error fixtures validate offline against `hooks.schema.json`, and existing contract fixtures remain valid.
- **SC-006**: The conformance procedure ran on the minimum and current Git and jj lanes on `ubuntu-24.04` amd64, J0 and J7 passed on each, and the recorded outcomes agree with the evidence catalog; an unavailable lane never counts as a pass. arm64 and darwin results are local evidence only.

## Scope and Dependencies

This is a bounded read-only disclosure slice on existing native ownership, Git isolation and the jj sandbox session. Explicitly out of scope: running or testing hooks, reading hook content, installing, editing, enabling or bypassing hooks, a hook-compatibility layer for jj, jj-native or third-party hook mechanisms (`jj fix`, `jj run`, external tools), server-side hooks, GitButler hook behavior, Windows, and monitoring hooks over time. A plan/apply feature that installs or repairs hooks needs its own specification. Depends on Spec 021 only for the shared exit-status-3 error and the text-escaping helper of its first work item (whichever lands first provides them; the coordinator resolves the overlap). Constitution 1.1.0 applies with no amendment: the feature is read-only, discloses its process use, and states the jj capability and evidence level instead of claiming equivalence.
