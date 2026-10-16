# Data model: Submodule pin audit

All text fields are bounded and rendered as data. Commit identities are full object names. Nullable fields are `null` only when the observation was not possible; the reason is then given in a sibling `reason` or in a finding.

## PinsResult (`schema_version: 1`, `command: "pins"`)

| Field | Type | Notes |
| --- | --- | --- |
| `status` | `ok` \| `incomplete` \| `refused` \| `error` | `ok` only when `complete` is true |
| `backend` | `git` \| `jj` \| `gitbutler` \| `unknown` | selected by native ownership |
| `input_path` | string | as given, bounded |
| `identity` | existing native identity result | superproject root, Git directory, HEAD commit and reference |
| `boundary` | Boundary | effects, limits, completeness, freshness note |
| `submodules` | Submodule[] | sorted by path bytes; empty array when none |
| `findings` | Finding[] | sorted by severity, submodule path, code |
| `summary` | Summary | counts by severity and by publication status |
| `error` | `{code, message}` \| null | refusals and operational failures only |

## Boundary

- `effects`: `{network: false, fetch: false, writes: false, hooks: false}`, always present and always these values for a returned result.
- `limits`: `{max_submodules: 256, per_submodule_seconds: 5, total_seconds: 30, max_output_bytes: 1048576, max_remote_refs: 16, max_count: 10000}`.
- `complete`: boolean.
- `staleness`: fixed text stating that remote-tracking refs are local records and no fetch was made.

## Submodule

| Field | Type | Notes |
| --- | --- | --- |
| `path` | string | repository-relative, as recorded |
| `name` | string \| null | `.gitmodules` name when registered |
| `registration` | `{gitlink_in_head, gitlink_in_index, gitmodules_entry, active}` | booleans; `active` null when unknown |
| `url` | `{configured, has_credentials, remote_url, match}` | URLs redacted; `match` is `match`, `mismatch` or `unknown` |
| `pin` | `{head_commit, index_commit, staged_differs}` | commits null when the source lacks the gitlink |
| `checkout` | Checkout | see below |
| `publication` | Publication | see below |
| `tracking` | `{ref, relation, ahead, behind}` | ref from `.gitmodules` `branch` or the remote default; `relation` is `equal`, `behind`, `ahead`, `diverged` or `unknown` |
| `nested` | `{present: boolean}` | not traversed |
| `refusal` | `{code, message}` \| null | set when the submodule itself was refused |
| `finding_codes` | string[] | codes of findings for this submodule |

### Checkout

`state` (`initialized`, `uninitialized`, `refused`), `head_commit`, `branch` (null when detached), `detached`, `relation_to_pin` (`same`, `ahead`, `behind`, `diverged`, `unrelated`, `unknown`), `ahead` and `behind` (integers capped at 10,000 or null), `dirty` with `tracked` and `untracked` each `present`, `absent` or `unknown`.

### Publication

`status` (`reachable`, `unreachable`, `unknown`), `reason` (text when `unknown`), `reachable_from` (at most 16 ref names), `refs_considered`, `last_fetch` (RFC 3339 time or null, advisory), `truncated` (boolean).

## Finding

`code`, `severity` (`error`, `warning`, `info`), `submodule` (path or null for global findings), `message` (bounded, plain text).

| Code | Severity |
| --- | --- |
| `pin_unpublished` | error |
| `gitlink_without_entry` | error |
| `url_has_credentials` | error |
| `submodule_path_unsafe` | error |
| `state_changed_during_audit` | error |
| `limit_exhausted` | error |
| `staged_pin_differs` | warning |
| `checkout_differs_from_pin` | warning |
| `submodule_dirty` | warning |
| `url_mismatch` | warning |
| `entry_without_gitlink` | warning |
| `pin_object_absent` | warning |
| `submodule_unsafe_owner` | warning |
| `submodule_uninitialized` | info |
| `pin_behind_tracking` | info |
| `nested_submodules_present` | info |
| `remote_refs_may_be_stale` | info (global, always present when any publication was evaluated) |

## Summary

`submodules`, counts of `error`, `warning`, `info` findings, and counts of publication status (`reachable`, `unreachable`, `unknown`).

## State and ordering rules

- `complete` is false whenever a limit is exhausted, a submodule refusal blocks evidence the result would otherwise claim, or a resample differs.
- A submodule with `refusal` still appears, with only the fields that were safe to read.
- `status: "refused"` carries `error.code` of `backend_unsupported`, `ownership_ambiguous`, `bare_repository` or `not_a_repository`, and no submodule list.
