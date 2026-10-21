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
| `base` | `{old, new}` | `old`: the old parent itself (checked to be an ancestor of the old head, else `old-parent-not-ancestor`); `new`: merge base of old parent and new parent (`base.new`) |
| `moved` | `{count, commits[]}` | `old_parent..old_head`, oldest first: `{oid, parents[1], tree, author, committer, subject_sha256}`; the subject is never echoed beyond a bounded escaped `subject` |
| `remote` | string | name used only in texts |

## Evidence

| Field | Type | Meaning |
| --- | --- | --- |
| `replay` | Replay | plumbing replay in a private quarantine |
| `range_pairing` | `{rows[], all_equal}` | per moved commit: `{old, predicted_tree, mark}` with `mark` in `=`, `!`, `<`, `>`, `unpaired` (the `git range-diff` marks, computed on the quarantine commits) |
| `parent_dropped` | `{rows[], all_dropped}` | the parent's own commits not already on the new parent's chain, `base.new..old_parent` (`base.new` = merge base of `old_parent` and `new_parent`), classified by `git cherry <new_parent> <old_parent>`: `{oid, mark: "-"\|"+", state: "dropped"\|"patch-equivalent-tree"\|"present"\|"missing"\|"unknown"}`; `+` rows get a tree-equivalence probe of their touched paths. `all_dropped` is `true` only when the set is non-empty and every row is `dropped` or `patch-equivalent-tree`; an empty set gives `state: "unknown"` with reason `empty-parent-range` (never a pass: a rebase-merge normally leaves a non-empty set, so empty means the parent was already contained or the input is wrong) |
| `empty_after_replay` | string[] | moved commits whose replayed tree equals their predicted parent tree (the change is already contained): a trap, listed explicitly; `range_pairing` marks them `unpaired` and they make `outcome` `needs-attention` |
| `own_diff_equality` | `{rows[], all_equal}` | `{old, state: "equal"|"differs"|"unknown", old_patch_id, predicted_patch_id}` |
| `identity` | Identity | `{state: "pass"\|"fail"\|"unknown", source: "flag"\|"none", expected[], mismatches[]}` over author and committer of `moved`. `pass` requires at least one `--identity` flag (`source: "flag"`); the environment never supplies expected identities, so without a flag `state` is `unknown`, reason `no-expected-identity`, `source: "none"`. The fixed replay identity is not checked (never pushed) |
| `conflicts` | `{predicted, items[]}` | first conflicting commit and `{path, kind}`; `replay_stopped_at` when later commits could not be predicted |
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

## Names and dates

`input.branch.short` is the full ref with exactly the leading `refs/heads/` stripped (`refs/heads/a/b` gives `a/b`); other namespaces are refused. Author and committer dates are recorded per moved commit (RFC 3339 with offset); a replay preserves author dates, and neither date takes part in the identity check or the pairing.

## Texts

`{lease_push: {assignment, text, observed_old_sha, new_sha_known: false}, resync: {fetch, rebase_onto, reset_hard, destructive_note}}`.

## Boundary

`{max_commits: 200, max_paths: 2000, max_result_bytes: 1048576, timeout_seconds: 60, reached: null|name}`.

## Configuration

None in the first slice. The keep-both path allowlist (`[restack] keep_both_paths`, default empty, exact paths or directory prefixes, no heuristic) and `--keep-both-config` belong to WP6.
