# Data model: Multi-repository inventory

Text fields are bounded and are data. Codes, statuses, limits, the facts derivation and the exit table are normative in [spec.md](spec.md) and are not repeated here. Arrays are never null.

## ReposResult (`schema_version: 1`, `command: "repos"`)

| Field | Type | Notes |
| --- | --- | --- |
| `status` | `ok` \| `partial` \| `error` | rule in spec FR-012: `ok` when every entry is `ok` or `skipped`; `error` when no entry has a non-null `inventory` with status `ok` or `partial`; otherwise `partial` |
| `input_paths` | string[] | explicit paths as given, in order, at most 16 |
| `submodules_requested` | boolean | the `--submodules` flag |
| `boundary` | Boundary | |
| `entries` | Entry[] | expansion order |
| `summary` | Summary | |
| `diagnostics` | `{code, message}`[] | command-level failures such as cancellation or output-bound fallback |

## Boundary

`complete` (equals `status == "ok"`), `freshness` (`sampled-nontransactional` fixed), `effects` (`{network:false, fetch:false, repository_writes:false, hooks:false, private_temporary_files_for_jj:true}`), `limits` (the spec limits table as `{max_entries, max_path_bytes, max_discovery_candidates, per_repository_seconds, total_seconds, parallelism, max_output_bytes, gitmodules_bytes}`), `limitations` (strings; always includes the ahead/behind, publication and task-ownership statement of FR-011).

## Entry

| Field | Type | Notes |
| --- | --- | --- |
| `index` | integer | position in `entries` |
| `path` | string | as given or as registered (relative to the parent root for submodules); always valid UTF-8 (lexically invalid registered paths never become entries) |
| `resolved_path` | string \| null | symlink-resolved absolute path; null when it cannot be resolved |
| `origin` | `explicit` \| `submodule` | |
| `parent` | integer \| null | index of the superproject entry for `submodule` |
| `status` | `ok` \| `partial` \| `error` \| `skipped` | |
| `error` | `{code, message}` \| null | null when `ok`; required otherwise |
| `backend` | `git` \| `jj` \| `gitbutler` \| `ambiguous` \| `unknown` | from the inventory when available |
| `discovery` | Discovery | |
| `duplicate_of` | integer \| null | set with `duplicate-repository` |
| `contained_in` | integer \| null | index of a listed repository whose root contains this one |
| `facts` | Facts \| null | null unless an inventory was observed (spec, Entry status) |
| `inventory` | InventoryResult \| null | embedded existing inventory object (validated by `inventory.schema.json`); null when not run, omitted or skipped |
| `notes` | string[] | secondary causes (see error precedence), caveats such as the submodule worktree record; bounded; each note is `"<code>: <message>"` |

`Discovery`: `{requested: boolean, status: "not-requested" | "ok" | "not-applicable" | "failed" | "limit", submodules: integer, ignored: integer, deduplicated: integer, reason: string}`. `submodules` counts entries added; `ignored` counts `.gitmodules` paths that are lexically invalid, are not gitlinks or failed the query (lexically invalid ones are also named in the parent's `notes`); `deduplicated` counts paths not added because already listed. `not-applicable` is used for non-Git parents, for explicit entries that failed identity or an entry check (no discovery ran), and for submodule entries themselves (no recursion); `not-requested` when `--submodules` is absent.

`Facts`: `{head_state: "attached"|"detached"|"unborn"|"jj-workspace", head_ref: string|null, head_commit_id: string|null, change_id: string|null, dirty: {value: "true"|"false"|"unknown", reason: string}, conflicted_references: integer, workspaces: {total, stale, foreign, unverified}}`, derived per the spec table.

## Summary

`{entries, by_status: {ok, partial, error, skipped}, by_backend: {git, jj, other}, dirty: {true, false, unknown}, stale_workspaces, foreign_workspaces, unverified_workspaces, conflicted_references, skipped_uninitialized, discovery_not_applicable (explicit entries only) (explicit entries only), inventories_omitted}`, all integers, computed from `entries`.

## Invariants

- `entries[i].index == i`; `parent` and `duplicate_of` point to smaller indexes; `contained_in` may point to any index.
- `status` `ok` implies `error` null and `inventory` non-null with inventory status `ok`; `partial` has an `error` (`inventory-partial`, `discovery-failed` or `discovery-limit`) and, unless the cause is discovery only, an inventory with status `partial`.
- `error.code` is from the spec's error-code table or the inventory's vocabulary.
- `inventory` and `facts` per entry status follow the table in the spec (Entry status); an entry failing the identity inspection or an entry check never runs an inventory, so `inventory` and `facts` are null; omission (`inventory-limit`) nulls `inventory` and keeps `facts`.
