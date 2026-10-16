# Data model: Hook and bypass disclosure

All text fields are bounded (4,096 bytes) and rendered as data. A field is `null` only when the observation was not possible or not applicable. The finding table, the refusal table, the hook catalog, the evidence catalog and the limits are normative in [spec.md](spec.md); this file does not repeat them, so the two cannot drift.

## HooksResult (`schema_version: 1`, `command: "hooks"`)

| Field | Type | Notes |
| --- | --- | --- |
| `status` | `ok` \| `incomplete` \| `refused` \| `error` | `ok` only when `boundary.complete` is true |
| `backend` | `git` \| `jj` \| `gitbutler` \| `ambiguous` \| `unknown` | selected by native ownership markers |
| `input_path` | string | as resolved, bounded |
| `identity` | Identity \| null | null on refusal and on errors before identity is known |
| `boundary` | Boundary | effects, limits, completeness, notes |
| `configuration` | Configuration \| null | null for `refused` and `error` |
| `hooks` | HookEntry[] | present entries among the six catalog names, sorted by name; empty array when none |
| `operations` | Operation[] | exactly `commit` then `push` when `configuration` is non-null, else empty |
| `findings` | Finding[] | sorted by severity (error, warning, info), then operation (`commit`, `push`, null last), then code |
| `summary` | Summary | counts |
| `error` | `{code, message}` \| null | refusals and operational failures only |

## Identity

`{root, git_dir, git_version, jj_version, colocated}`. `root` is the worktree or workspace root (for jj the innermost `.jj` marker's directory). `git_dir` is the absolute Git directory (Git-owned and jj with a `.git` entry; null otherwise). `git_version` is set when a Git process ran, `jj_version` when the jj session ran. `colocated` is `true` when a jj workspace has a `.git` entry at its root (directory or gitfile), `false` otherwise, and null for Git.

## Boundary

- `effects`: `{network: false, writes: false, hooks_executed: false, hook_contents_read: false, hook_paths_opened: false, git_processes: bool, jj_processes: bool}`. The first five are constants (no hook entry and no directory is opened); the last two disclose process use (`git_processes` is false for a jj workspace with no `.git` entry and for the ambiguous refusal; `jj_processes` is true only for a jj-owned workspace and covers only the sandbox probe and `--version`). Git reads in a jj workspace run outside the jj sandbox.
- `limits`: `{deadline_seconds: 10, git_process_seconds: 5, stream_bytes: 65536, result_bytes: 262144, value_bytes: 4096}`, the spec's limits table.
- `complete`: boolean; false for `refused`, `error` and `incomplete`.
- `notes`: empty for `refused` and `error` results; otherwise fixed strings: hook contents are never read (a missing interpreter or a self-disabling script is not detected); per-command overrides and the caller's `GIT_*` environment are not detected or applied; hooks in the default directory are inert while `core.hooksPath` is set and are not examined; on the jj path, the Git version used for `jj-push` coverage is the one on stw's `PATH`, which may differ from the Git jj spawns.

## Configuration

| Field | Type | Notes |
| --- | --- | --- |
| `hooks_path` | HooksPath | |
| `directory` | `{path, kind}` \| null | null when no directory hook can run (state other than `default` and `configured`); `kind` is `directory` or `symlink` (to a directory) |
| `config_hooks_detected` | boolean | true when a `hook.*.event` value names one of the six operation hooks |

### HooksPath

`state`: `default` (unset; `<git-dir>/hooks` of the common directory), `configured`, `null-device`, `empty`, `absent`, `not-directory`, `unreadable` (the directory lacks search permission for the real user), `invalid` (a valueless key, or a value Git cannot expand), `not-applicable` (jj workspace with no `.git` entry). `effective_value` (string or null; the last value), `effective_scope` (`system`, `global`, `local`, `worktree` or null), `resolved_directory` (absolute path from Git, or null).

## HookEntry

| Field | Type | Notes |
| --- | --- | --- |
| `name` | string | one of the six catalog names |
| `kind` | `file` \| `symlink` \| `directory` \| `other` | from `lstat` |
| `state` | `active` \| `ignored` \| `not-runnable` | per FR-005, in that order of evaluation |
| `executable_for_user` | boolean | `access(X_OK)` for the real user, as Git checks |
| `mode` | string | octal permission bits, for example `0755` |
| `symlink` | `{dangling}` \| null | null unless `kind` is `symlink` |

## Operation

| Field | Type | Notes |
| --- | --- | --- |
| `operation` | `commit` \| `push` | |
| `command` | string | the backend command the claim is about |
| `claim_id` | `git-commit` \| `git-push` \| `jj-commit` \| `jj-push` | evidence catalog claim |
| `runs_hooks` | `yes` \| `no` \| `unknown` | |
| `evidence` | Evidence | |
| `exceptions` | `{id, description, evidence_level}`[] | Git claims only; sorted by id |
| `guards_evaluated` | boolean | false when no Git configuration applies (jj workspace with no `.git` entry); then `hooks` is empty and the counts are 0 |
| `hooks` | OperationHook[] | one per catalog hook of the operation, catalog order |
| `guards_active` | integer | active blocking hooks |
| `guards_bypassed` | integer | those with `bypassed: true` |

### Evidence

`{level, version_covered, detected_versions, measured_range, sources}`. `level` is `measured`, `documented` or `unverified` (FR-008). `detected_versions` is `{git, jj}` with a version string or null per member of the claim's backend tuple. `measured_range` is `{git, jj}` with `{min, max}` or null, or null when nothing was measured. `sources` is `{kind, reference}`[], `kind` is `documentation` or `measurement`.

### OperationHook

`{name, role, state, bypassed, skippable_by}`. `role` is `blocking` or `observer`. `state` is `active`, `ignored`, `not-runnable`, `disabled-by-path` (the hooks path state is `null-device`, `empty`, `absent`, `not-directory`, `unreadable` or `invalid`) or `absent`. `bypassed` is `true`, `false` or `null` per FR-007. `skippable_by` lists exception ids that skip this hook.

## Finding

`{code, severity, operation, message}`. Codes and severities come from the spec's findings table.

## Summary

`{errors, warnings, infos, hooks_present, guards_active, guards_bypassed}`, all integers; `guards_active` and `guards_bypassed` count distinct hook names over both operations.

## State and completeness rules

- `boundary.complete` is true when every examination finished, no limit was exhausted and no resample differed. A hook that is `ignored` or `not-runnable`, an absent directory, an `unknown` claim, a non-measured evidence level or detected configuration hooks are valid observations and do not make the result incomplete.
- `status: "incomplete"` carries the result built so far with `hooks-limit` or `hooks-state-changed`.
- `status: "refused"` carries an `error.code` from the `refused` rows of the spec's refusal table, `configuration: null`, empty `hooks`, `operations` and `findings`, and a null `identity` when none could be established.
- `status: "error"` carries a code from the `error` rows of the refusal table and the same empty fields. For codes raised after a `.jj` marker was found, `backend` is `jj`.
- Evidence level derivation: the catalog base level, lowered to `documented` when a detected version is outside the catalog's measured range (`version_covered: false`).
- `runs_hooks` for a Git-owned repository comes from the `git-*` claims; for a jj-owned workspace from the `jj-*` claims.
