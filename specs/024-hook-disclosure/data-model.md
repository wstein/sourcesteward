# Data model: Hook and bypass disclosure

All text fields are bounded (4,096 bytes) and rendered as data. A field is `null` only when the observation was not possible or not applicable; the reason is then in a sibling `reason` or in a finding. The finding-code and severity table, the refusal table, the hook catalog, the evidence catalog and the limits are normative in [spec.md](spec.md); this file does not repeat them, so the two cannot drift.

## HooksResult (`schema_version: 1`, `command: "hooks"`)

| Field | Type | Notes |
| --- | --- | --- |
| `status` | `ok` \| `incomplete` \| `refused` \| `error` | `ok` only when `boundary.complete` is true |
| `backend` | `git` \| `jj` \| `gitbutler` \| `ambiguous` \| `unknown` | selected by native ownership markers |
| `input_path` | string | as resolved, bounded |
| `identity` | Identity \| null | null on refusal and on errors before identity is known |
| `boundary` | Boundary | effects, limits, completeness, notes |
| `configuration` | Configuration \| null | null for `refused` and `error` |
| `hooks` | HookEntry[] | present catalog names only, sorted by name; empty array when none |
| `operations` | Operation[] | exactly `commit` then `push` when `configuration` is non-null, else empty |
| `findings` | Finding[] | sorted by severity (error, warning, info), then operation (`commit`, `push`, null last), subject, code |
| `summary` | Summary | counts |
| `error` | `{code, message}` \| null | refusals and operational failures only |

## Identity

`{root, git_dir, common_dir, git_version, jj_version, colocated}`. `root` is the worktree or workspace root. `git_dir` and `common_dir` are the absolute Git directory and common Git directory (Git-owned and colocated jj; null otherwise; they differ in a linked worktree). `git_version` is set when a Git process ran, `jj_version` when the jj session ran. `colocated` is `true` when a jj workspace has a `.git` entry at its root (directory or gitfile), `false` otherwise, and null for Git.

## Boundary

- `effects`: `{network: false, writes: false, hooks_executed: false, hook_contents_read: false, hook_paths_opened: false, git_processes: bool, jj_processes: bool}`. The first five are constants (`hook_paths_opened` means no hook entry is opened; the confined hooks directory itself is opened once for the bounded listing); the last two disclose process use (`git_processes` is false for a non-colocated jj workspace and for the ambiguous refusal; `jj_processes` is true only for a jj-owned workspace and then covers only the sandbox probe and `--version`). Git reads in a colocated jj workspace run outside the jj sandbox.
- `limits`: the keys and values of the spec's limits table: `{deadline_seconds: 10, git_process_seconds: 5, stream_bytes: 65536, result_bytes: 262144, directory_entries: 256, unrecognized_names: 16, config_hooks: 64, value_bytes: 4096, level_records: 16}`.
- `complete`: boolean; false for `refused`, `error` and `incomplete`.
- `notes`: empty for `refused` and `error` results; otherwise fixed strings, at least the Git version used for `jj-push` coverage is the one on stw's `PATH`, which may differ from the Git jj spawns, and: the content of hooks is never read, so a missing interpreter or a self-disabling script is not detected; per-command overrides are not detected; the caller environment is not applied.

## Configuration

| Field | Type | Notes |
| --- | --- | --- |
| `hooks_path` | HooksPath | |
| `directory` | Directory \| null | null when no directory hook can run (`state` is `null-device`, `empty`, `absent`, `not-directory`, `unreadable`, `invalid` or `not-applicable`) and when not examined |
| `config_hooks` | ConfigHooks | |
| `worktree_config_enabled` | boolean \| null | `extensions.worktreeConfig`; null when not applicable |
| `caller_environment` | string[] | names only, sorted, from the closed list in the spec edge cases |

### HooksPath

`state`: `default` (unset; `<git-dir>/hooks` of the common directory), `configured`, `null-device`, `empty`, `absent`, `not-directory`, `unreadable` (the directory lacks search permission for the real user), `invalid` (a valueless key, or a value Git cannot expand), `not-applicable` (jj workspace without a `.git` entry). `effective_value` (string or null; the last value), `resolved_directory` (absolute path from Git, or null), `levels`: at most 16 `{scope, value, origin}` records in precedence order, `scope` one of `system`, `global`, `local`, `worktree`; `origin` is the file path for `local` and `worktree` and null for `system` and `global`; `levels_truncated` is a boolean. `overridden`: true when more than one level set a value and the values differ.

### Directory

`{path, kind, resolves_to, inside_repository, owner_is_effective_user, group_writable, world_writable, entries_scanned, unrecognized_names, unrecognized_truncated}`. `kind` is `directory`, `symlink` (a symlink to a directory) or `other`. `resolves_to` is the resolved real path (null when the directory is not a symlink). `inside_repository` is true when the resolved directory lies inside a confinement root (worktree root, Git directory or common Git directory). `entries_scanned` is 0 when the directory is not confined (it is then not listed). `unrecognized_names` holds at most 16 escaped names.

### ConfigHooks

`{engine, evidence, entries, truncated}`. `engine`: `supported` (the detected Git version is within the measured range of the claim `git-config-hooks`) or `unknown`; `evidence` is an Evidence object for that claim. `entries`: `{name, events, enabled, command_present, scopes}` with `enabled` true, false or null (unset), `command_present` a boolean, `scopes` the sorted scopes of the entry's keys, at most 64 entries sorted by name. The command text is never read or reported. Entries count as active guards when `engine` is `supported`; when it is `unknown` they appear in the matching operation with state `unknown`. For `not-applicable` hooks path state: `{engine: unknown, evidence: null, entries: [], truncated: false}`.

## HookEntry

| Field | Type | Notes |
| --- | --- | --- |
| `name` | string | a catalog name |
| `hook_scope` | `client` \| `server` | from the catalog |
| `kind` | `file` \| `symlink` \| `directory` \| `other` | from `lstat` of the entry in the effective directory |
| `state` | `active` \| `ignored` \| `not-runnable` | per FR-007, in that order of evaluation |
| `executable_for_user` | boolean \| null | `access(X_OK)` for the real user, as Git checks |
| `mode` | string | octal permission bits, for example `0755` |
| `size` | integer | bytes from `lstat`; the content is not read |
| `owner_is_effective_user` | boolean | |
| `group_writable`, `world_writable` | boolean | |
| `symlink` | `{target_inside_repository, dangling}` \| null | null unless `kind` is `symlink` |
| `tracked` | `tracked` \| `untracked` \| `unknown` \| `not-applicable` | from the index |

## Operation

| Field | Type | Notes |
| --- | --- | --- |
| `operation` | `commit` \| `push` | |
| `command` | string | the backend command the claim is about, from the evidence catalog |
| `guards_evaluated` | boolean | false when no Git configuration applies (jj workspace without a `.git` entry); then `hooks` is empty and the counts are 0 |
| `claim_id` | string | evidence catalog claim |
| `runs_hooks` | `yes` \| `no` \| `unknown` | |
| `evidence` | Evidence | |
| `exceptions` | `{id, description, evidence_level}`[] | Git claims only; sorted by id |
| `hooks` | OperationHook[] | one per catalog hook of the operation, catalog order |
| `guards_active` | integer | active blocking hooks |
| `guards_bypassed` | integer | those with `bypassed: true` |

### Evidence

`{level, version_covered, detected_versions, measured_range, sources}`. `level` is `measured`, `documented` or `unverified` (definitions in FR-010). `detected_versions` is `{git, jj}` with a version string or null per member of the claim's backend tuple. `measured_range` is `{git, jj}` with `{min, max}` or null, or null when nothing was measured. `sources` is `{kind, reference}`[], `kind` is `documentation` or `measurement`.

### OperationHook

`{name, role, state, source, bypassed, skippable_by}`. `role` is `blocking` or `observer`. `state` is `active`, `unknown` (a configuration hook while the engine is `unknown`), `ignored`, `not-runnable`, `disabled-by-path` (the hooks path state is `null-device`, `empty`, `absent`, `not-directory`, `unreadable` or `invalid`, so directory hooks cannot run; a configuration hook of the same event still counts as `active` when the engine is `supported`, and as `unknown` otherwise) or `absent`. `source` is `hooks-dir`, `config`, `both` or null. `bypassed` is `true`, `false` or `null` per FR-009; state precedence is in FR-009. `skippable_by` lists exception ids that skip this hook.

## Finding

`{code, severity, operation, subject, message}`. `operation` is `commit`, `push` or null; `subject` is a hook name, `directory`, `configuration` or null. Codes and severities come from the spec's findings table.

## Summary

`{errors, warnings, infos, hooks_present, guards_active, guards_bypassed}`, all integers; `guards_active` and `guards_bypassed` count distinct hook names over both operations.

## State and completeness rules

- `boundary.complete` is true when every examination finished and no limit was exhausted and no resample differed. A hook that is `ignored` or `not-runnable`, an absent directory, an `unknown` claim, or a non-measured evidence level is a valid observation and does not make the result incomplete.
- `status: "incomplete"` carries the result built so far with `hooks-limit` or `hooks-state-changed`.
- `status: "refused"` carries an `error.code` from the refusal table, `configuration: null`, empty `hooks`, `operations` and `findings`, and a null `identity` when none could be established.
- `status: "error"` carries a code from the `error` rows of the spec's refusal table (`hooks-failed`, the jj session codes, or an existing ownership-decision code passed through) and the same empty fields. For codes raised after a `.jj` marker was found, `backend` is `jj`.
- Evidence level derivation: the catalog base level, lowered to `documented` when the detected version is outside the catalog's measured range (`version_covered: false`).
- `runs_hooks` for a Git-owned repository comes from the `git-*` claims; for a jj-owned workspace from the `jj-*` claims.
