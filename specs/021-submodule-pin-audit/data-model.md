# Data model: Submodule pin audit

All text fields are bounded and rendered as data. Commit identities are full object names. A field is `null` only when the observation was not possible; the reason is then given in a sibling `reason` or in a finding. The normative finding-code and severity table and the refusal table are in [spec.md](spec.md); this file does not repeat them, so the two cannot drift.

## PinsResult (`schema_version: 1`, `command: "pins"`)

| Field | Type | Notes |
| --- | --- | --- |
| `status` | `ok` \| `incomplete` \| `refused` \| `error` | `ok` only when `boundary.complete` is true |
| `backend` | `git` \| `jj` \| `gitbutler` \| `ambiguous` \| `unknown` | selected by native ownership; `unknown` until ownership is decided, so an early failure never claims Git |
| `input_path` | string | as given, bounded |
| `identity` | existing native identity result | superproject root, Git directory, HEAD commit and reference; always null for `refused` and `error` results, and an `ok` or `incomplete` result carries an identity with `status` ok and backend git |
| `boundary` | Boundary | effects, limits, completeness, staleness note |
| `submodules` | Submodule[] | sorted by path bytes; empty array when none |
| `findings` | Finding[] | sorted by severity, submodule path, code |
| `summary` | Summary | counts by severity and by publication status |
| `error` | `{code, message}` \| null | refusals and operational failures only |

## Boundary

- `effects`: `{network: false, fetch: false, writes: false, hooks: false}`, always present with these values.
- `limits`: `{max_submodules: 256, per_submodule_seconds: 5, total_seconds: 30, max_output_bytes: 1048576, max_remote_refs: 16, max_count: 10000, dirty_scan_seconds: 3, dirty_scan_entries: 20000}`.
- `complete`: boolean; always false for `refused` and `error` results.
- `staleness`: fixed text stating that remote-tracking refs are local records and no fetch was made.

## Submodule

| Field | Type | Notes |
| --- | --- | --- |
| `path` | string | repository-relative, as recorded; a path that is not valid UTF-8 is shown escaped (`\xNN`) and the entry is refused |
| `path_encoding` | `utf8` \| `escaped` | `escaped` only for refused non-UTF-8 entries |
| `name` | string \| null | `.gitmodules` name when registered |
| `registration` | `{gitlink_in_head, gitlink_in_index, gitmodules_entry, active}` | booleans; `active` null when unknown |
| `url` | `{configured, has_credentials, remote_name, remote_url, match}` | `configured` is the `.gitmodules` value, redacted (credentials, query and fragment removed); `match` is `match`, `mismatch` or `unknown`; an invalid or unsupported URL scheme is `unknown` with the sanitized URL and a diagnostic, and has no finding code (the published code list does not change) |
| `pin` | `{head_commit, index_commit, staged_differs}` | commits null when the source lacks the gitlink, and `head_commit` null with an unborn superproject HEAD |
| `checkout` | Checkout | see below |
| `publication` | Publication | see below |
| `tracking` | `{ref, relation, ahead, behind, capped}` | ref from `.gitmodules` `branch` or the remote default; `relation` is `equal`, `behind`, `ahead`, `diverged` or `unknown` |
| `nested` | `{present: boolean}` | not traversed |
| `refusal` | `{code, message}` \| null | set when the submodule itself was refused |
| `finding_codes` | string[] | codes of findings for this submodule |

### Checkout

`state` (`initialized`, `uninitialized`, `refused`), `head_commit`, `branch` (null when detached), `detached`, `relation_to_pin` (`same`, `ahead`, `behind`, `diverged`, `unrelated`, `unknown`), `ahead` and `behind` (integers capped at 10,000, or null), `capped` (boolean), and `dirty` with `tracked`, `untracked` (each `present`, `absent` or `unknown`) and `reason` (text whenever either is `unknown`: racily clean or mtime-only difference, or dirty-scan budget exceeded).

### Publication

`status` (`reachable`, `unreachable`, `unknown`), `reason` (text when `unknown`), `reachable_from` (at most 16 entries of `{remote, ref}`), `truncated` (boolean), `last_fetch` (RFC 3339 time or null, advisory).

## Finding

`code`, `severity` (`error`, `warning`, `info`), `submodule` (path or null for global findings), `message` (bounded, plain text). Codes and severities come from the spec's normative table.

## Summary

`{submodules, errors, warnings, infos, publication: {reachable, unreachable, unknown}}`, all integers.

## State and completeness rules

- `boundary.complete` is true when every direct submodule was either examined or explicitly refused, and no limit was exhausted and no resample differed. An `unknown` observation, an uninitialized submodule or an absent pinned object is a valid observation and does not make the result incomplete.
- `boundary.complete` is false, and `status` is `incomplete`, when `pins-limit` or `pins-state-changed` was raised.
- A submodule with `refusal` still appears, with only the fields that were safe to read. Its refusal is an error-severity finding and does not by itself make the result incomplete.
- `status: "refused"` carries an `error.code` from the refusal table, an empty `submodules` array, no findings and a null `identity`.
- `status: "error"` carries `pins-failed` and the same empty fields; its backend is `git` only once Git ownership was established, otherwise `unknown`.
