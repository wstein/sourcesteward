# Data model: restack plan v1 (first slice)

Types live in a new `internal/restack` package and reuse the native identity and diagnostic types of `internal/inspection`. JSON names are snake_case; arrays are non-null; optional objects are `null`; text is valid UTF-8, bounded and data. Object ids are 40-hex lowercase.

## RestackPlanResult

The top-level fields are those of the [contract](contracts/restack-plan.md#result).

## Input

| Field | Type | Notes |
| --- | --- | --- |
| `branch` | `{ref, old_head}` | `ref` as given (fully qualified), `old_head` the observed tip |
| `old_parent` | `{given, oid}` | `given` is the 40-hex or the full ref text |
| `new_parent` | `{given, oid}` | |
| `base` | `{old, new}` | `old`: merge base of old parent and old head; `new`: merge base of old parent and new parent |
| `moved` | `{count, commits[]}` | `old_parent..old_head`, oldest first: `{oid, parents[1], tree, author, committer, subject_sha256}`; the subject is never echoed beyond a bounded escaped `subject` |
| `remote` | string | name used only in texts |

## Evidence

| Field | Type | Meaning |
| --- | --- | --- |
| `replay` | Replay | plumbing replay in a private quarantine |
| `range_pairing` | `{rows[], all_equal}` | per moved commit: `{old, predicted_tree, mark}` with `mark` in `=`, `!`, `<`, `>`, `unpaired` (the `git range-diff` marks, computed on the quarantine commits) |
| `parent_dropped` | `{rows[], all_dropped}` | commits of `old.base..old_parent` classified by `git cherry <new_parent> <old_parent>`: `{oid, mark: "-"|"+", state: "dropped"|"patch-equivalent-tree"|"missing"|"unknown"}`; `+` rows get a tree-equivalence probe of their touched paths |
| `own_diff_equality` | `{rows[], all_equal}` | `{old, state: "equal"|"differs"|"unknown", old_patch_id, predicted_patch_id}` |
| `identity` | Identity | `{state: "pass"|"fail"|"unknown", expected[], mismatches[]}` over author and committer of `moved` (and the committer identity a replay would use) |
| `conflicts` | `{predicted, items[]}` | first conflicting commit and `{path, kind, allowlisted}`; `replay_stopped_at` when later commits could not be predicted |
| `overlap` | Overlap | `{paths[], count, indicator: "none"|"some"|"unknown", note}`: paths changed between `old_parent` and `new_parent` that the moved commits also change. Risk indicator only |

Replay: `{state: "clean"|"conflict"|"incomplete", commits_replayed, final_tree, tree_equals_branch_tip_plus_parent_delta}`. No commit id of the quarantine is a claim: predicted commit ids depend on committer date and identity and are not reported.

State values never mean authorization; any `unknown` is reported with its reason (`reason` enum: `limit`, `attribute-hazard`, `object-missing`, `no-expected-identity`).

## Effects

| Field | Type | Notes |
| --- | --- | --- |
| `affected_refs` | `{ref, oid, contains_old_head}`[] | local `refs/heads/*` whose tips contain `old_head`: descendants that a later apply would not move automatically |
| `worktrees` | `{path, branch, state}`[] | all registered worktrees; the target and any worktree holding the branch with `state` `target` or `holds-branch` |
| `hooks` | `{name, present, would_run_by_porcelain_rebase, run_by_stw: false}`[] | names only (`pre-rebase`, `post-rewrite`, `reference-transaction`, `post-checkout`), never contents |
| `hazards` | string[] | configuration/hook hazard names found (FR-014) |
| `writes` | `{refs: false, index: false, worktree: false, config: false, objects: "quarantine-only"}` | constant disclosure |

## Texts

`{lease_push: {text, observed_old_sha, new_sha_known: false}, resync: {fetch, rebase_onto, reset_hard, destructive_note}}`.

## Boundary

`{max_commits: 200, max_paths: 2000, max_result_bytes: 1048576, timeout_seconds: 60, reached: null|name}`.

## Configuration (read as data)

`[restack] keep_both_paths = ["docs/ledger.md", "ledger/"]`: exact paths or directory prefixes; default empty; only meaning in this slice is `allowlisted: true|false` on conflict items. No auto-resolution, ever, in the plan.
