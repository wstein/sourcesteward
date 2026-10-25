# Data model: restack plan v1 (first slice)

Historical v1 types live in `internal/restack` (source implemented, hidden; schema UNPUBLISHED) and reuse the native identity and diagnostic types of `internal/inspection`. JSON names are snake_case; arrays are non-null; optional objects are `null`; text is valid UTF-8, bounded and data. Object ids are 40-hex lowercase.

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
| `range_pairing` | `{rows[], all_equal}` | per moved commit: `{old, mark}` with `mark` in `=`, `!`, `<`, `>`, `unpaired` (the `git range-diff` marks, computed on the quarantine commits; a commit without a replay commit, because a conflict stopped before it or because the new parent already contains its change, is `unpaired`; an unparsable range-diff gives `unknown`) |
| `parent_dropped` | `{rows[], all_dropped}` | the parent's own commits not already on the new parent's chain, `base.new..old_parent` (`base.new` = merge base of `old_parent` and `new_parent`), classified by `git cherry <new_parent> <old_parent>`: `{oid, mark: "-"\|"+", state: "dropped"\|"patch-equivalent-tree"\|"missing"\|"unknown"}` (there is no `present` state); `+` rows get a tree-equivalence probe of their touched paths (`patch-equivalent-tree` when none of them differs between the old and the new parent, `missing` otherwise, `unknown` with reason `limit` when the probe could not be read); more than 200 parent commits is incomplete (`bound-exceeded`, `max_commits`). `all_dropped` is `true` only when the set is non-empty and every row is `dropped` or `patch-equivalent-tree`; an empty set gives `state: "unknown"` with reason `empty-parent-range` (never a pass: a rebase-merge normally leaves a non-empty set, so empty means the parent was already contained or the input is wrong) |
| `empty_after_replay` | string[] | moved commits whose replayed tree equals their predicted parent tree (the change is already contained): a trap, listed explicitly; `range_pairing` marks them `unpaired` and they make `outcome` `needs-attention` |
| `own_diff_equality` | `{rows[], all_equal}` | `{old, state: "equal"|"differs"|"unknown", old_patch_id, predicted_patch_id}` |
| `identity` | Identity | `{state: "pass"\|"fail"\|"unknown", source: "flag"\|"none", expected[], mismatches[]}` over author and committer of the existing `moved` commits only; the committer that a by-hand rebase gives the rewritten commits comes from the rebaser's configuration or environment and is not predicted (limitation string `rewrite-committer-not-predicted`: reserved, not emitted until task T024 lands), so `pass` never covers the commits a rebase will create. `pass` requires at least one `--identity` flag (`source: "flag"`); the environment never supplies expected identities, so without a flag `state` is `unknown`, reason `no-expected-identity`, `source: "none"`. The fixed replay identity is not checked (never pushed) |
| `conflicts` | `{predicted, items[]}` | first conflicting commit and `{path, kind}`; `replay_stopped_at` when later commits could not be predicted |
| `overlap` | Overlap | `{paths[], count, indicator: "none"|"some"|"unknown", note}`: paths changed between `old_parent` and `new_parent` that the moved commits also change. Risk indicator only |

Replay: `{state: "clean"|"conflict"|"incomplete", commits_replayed, final_tree, tree_equals_branch_tip_plus_parent_delta}`; the last field is `true` when one three-way merge of the branch tip, the old parent and the new parent gives the tree that the commit-by-commit replay reached, `false` when it differs or conflicts, `null` when not computed (a conflict stopped the replay). Replay commits keep the original author and carry the fixed committer identity `stw-restack-plan <restack-plan@invalid>` dated 1970-01-01T00:00:00Z. No commit id of the quarantine is a claim: predicted commit ids depend on committer date and identity and are not reported.

State values never mean authorization; any `unknown` is reported with its reason (`reason` enum: `limit`, `attribute-hazard`, `object-missing`, `no-expected-identity`, `empty-parent-range`).

## Effects

| Field | Type | Notes |
| --- | --- | --- |
| `affected_refs` | `{ref, oid, contains_old_head}`[] | local `refs/heads/*` other than the target whose tips contain `old_head`: descendants that a later apply would not move automatically; any entry keeps `outcome` out of `evidence-clean` |
| `worktrees` | `{path, branch, state}`[] | all registered worktrees; the target and any worktree holding the branch with `state` `target` or `holds-branch` |
| `hooks` | `{name, present, would_run_by_porcelain_rebase, run_by_stw: false}`[] | names only (`pre-rebase`, `post-rewrite`, `reference-transaction`, `post-checkout`), never contents |
| `hazards` | string[] | configuration/hook hazard names found (FR-014) |
| `writes` | `{refs: false, index: false, worktree: false, config: false, objects: "quarantine-only"}` | constant disclosure |

## Names

`input.branch.short` is the full ref without the leading `refs/heads/` (`refs/heads/a/b` gives `a/b`); other namespaces are refused.

## Texts

`{lease_push: {assignment, text, observed_old_sha, new_sha_known: false}, resync: {fetch, rebase_onto, reset_hard, destructive_note}}`.

## Boundary

`{max_commits: 200, max_paths: 2000, max_result_bytes: 1048576, timeout_seconds: 60, reached: null|name}`.

## Configuration

None in the first slice. The keep-both path allowlist (`[restack] keep_both_paths`, default empty, exact paths or directory prefixes, no heuristic) and `--keep-both-config` belong to WP6.


## Proposed local apply entities (UNPUBLISHED)

These are Markdown-only additions for [the future contract](contracts/restack-apply.md).
They do not add fields to `RestackPlanResult` or its frozen v1 schema.
All ids are backend-qualified; native Git first apply remains SHA-1/full 40-hex.
Unsupported object formats refuse rather than truncate or reinterpret an id.

| Entity | Required fields and validation | Relationships |
| --- | --- | --- |
| LocalApplyPlan | version/kind, immutable plan id/digest, repository/common-directory identity, backend, one task/ref ownership binding, old parent/head, new parent, ordered moved ids, protected-ref/config/registration/operation expectations, trusted owner-identity binding, explicit assertions/committer policy (caller cannot widen), exact effects, bounds | Optional 012 observation and 014 preflight references are evidence only; equality of exact inputs/semantics/completeness required |
| ActualCandidate | full new head/tree, ordered one-to-one `{old_commit,new_commit}` map, preserved author/date, actual committer/date, all `=` range rows, own-diff and parent-integration proof, actual identity rows against trusted owner only, unsigned candidate policy, per-commit `{old_commit,new_commit,source_signed,signature-dropped}` when source was signed | Built after replay, before target CAS; never reuse v1's unpublished prediction commit id or fixed committer as actual proof |
| DurableRestackAction | type/version, plan/run/action ids, repository/task binding, exact target, old and planned/actual new ids, recovery-root id/value, stage/outcome and evidence digest | Typed reuse of 032 record architecture; each entry is bound to one selected action; foreign sync record never authorizes restack |
| ApplyReceipt | plan/run/action, old/new identities, per-action effects/outcomes, complete actual evidence, actual-SHA policy gate with derived moved-range `change_class`/scope and `pure_rebase`, recovery pointer, limits/diagnostics | Supplies evidence to 016, never permission; `remote_authorized` is false |
| ReviewGateInput | exact actual new head, derived `change_class` (`docs-only` or `code`; unknown→`code`), `pure_rebase` (all rows `=` and same count; unknown→false), moved-range scope, 016-recomputed full-publication-range class (union of per-commit paths from observed remote default to actual new head, including open parents; effective class can only strengthen to code), local check/equality evidence, independent issuer/verdict/freshness where pre-push policy requires it; required-check evidence exact-head, full observed remote-default-to-new-head identity range (including open-parent commits), trusted owner binding and local/global user-config origin evidence | 016 applies the AGENTS.md gate table: docs-only checks/equality without pre-push review, code exact-new-SHA review (middle tier allowed for pure rebase). Ready always independent exact-head CLEAR + green required checks; 043 human `--reviewed` remains a separate owner statement |
| RestorePlan | immutable reverse plan id, original run/action, root, old retained id, current expected new id, repository/task/registration/protection expectations, one declared reverse effect | Separate authorization; CAS only recorded-new→old, never blanket reset |
| JJRestackPlan/Receipt | native commit/change/operation ids, selected bookmark and exact descendant/bookmark/workspace effects, expected operation and recovery operation | Proposed capability only; cannot reuse Git field meanings for native operation semantics |

## Proposed state transitions

`observed → stored-plan → candidate-prepared → recovery-prepared → ref-applied → completed`.
Before recovery preparation, only owned scratch/private plan effects are allowed.
Candidate objects may enter the repository only under recorded intent and explicit
effects. `recovery-prepared` means old root plus known candidate id are durable,
not that the target moved. Each boundary may yield refusal, failed, canceled or
partial/unknown; reconcile from the journal and native old/new state using the
[recovery table](contracts/recovery.md#reconciliation-and-retry).

`completed → restore-plan → restored` requires a new authorization and unchanged
expected new head/ownership. A later ref value gives `changed-since`. Repeated
same action at recorded new/old identity gives proven already-applied/restored;
missing or forged evidence gives `journal-incomplete`, never a guess. Objects,
reflogs, private records and native refs do not form one atomic transaction.

Proposed strings are open for future compatibility; an unknown version, action
kind or gate state cannot grant mutation or be coerced to success. Valid unusual
paths stay data; terminal controls are escaped; arrays are bounded and non-null,
optional facts explicit null. Future schemas and validators are implementation
work only after owner decisions and review; no JSON file is created here.
