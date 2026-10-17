# Data model: stack observation v1

Types live in `internal/inspection` (local observation, plan) and `internal/hosting` (remote evidence), and reuse the native identity and diagnostic types of Spec 005 and Spec 020. JSON names are snake_case. Arrays are non-null even when empty; absent optional values are `null`. All text is valid UTF-8, bounded as stated, and is data. The normative tables for commands, capabilities, findings, linkage, remote sources, plan rules, argument rules, limits and statuses are in [spec.md](spec.md); this file adds field shapes, invariants and summary templates and does not repeat those tables.

## StackResult (`schema_version: 1`, `kind: "stack"`)

| Field | Type | Notes |
| --- | --- | --- |
| `schema_version` | integer | constant 1 |
| `kind` | string | constant `stack` |
| `mode` | `show` \| `plan` | from the command |
| `after_fetch` | boolean | whether `--after-fetch` was given (always `false` for `show`) |
| `status` | `ok` \| `incomplete` \| `error` | spec status table |
| `backend` | `git` \| `jj` \| `gitbutler` \| `ambiguous` \| `unknown` | as the hosting result |
| `identity` | native identity or `null` | the existing identity shapes, copied into the schema (drift-guarded) |
| `base` | Ref or `null` | `null` for `error` before resolution |
| `tip` | Ref or `null` | |
| `fork_commit_id` | commit id or `null` | the commit the lowest layer is based on; `null` with no layer |
| `layers` | Layer[] | at most 32, bottom first, `index` 1.. |
| `findings` | Finding[] | at most 400, order of the spec table, then layer, then subject |
| `plan` | Plan or `null` | non-null if and only if `mode` is `plan` and `status` is not `error` |
| `remote` | Remote or `null` | non-null if and only if `--pull-requests` was given and repository selection succeeded |
| `diagnostics` | `{code, message}`[] | empty for `ok`; codes from the status table (plus existing `hosting-*`); messages are fixed text without repository or response data |
| `boundary` | Boundary | |
| `notice` | string | constant: "Sampled advisory evidence; stw ran none of the printed commands and a plan authorizes nothing." |
| `observed_at` | RFC 3339 UTC | time of the final sample |

## Ref

`name` (as given, at most 200 bytes after escaping rules; the original is never rewritten), `kind` (`branch`, `remote-tracking`, `bookmark`, `remote-bookmark`), `commit_id` (full lowercase hex), `change_id` (jj, else `null`), `remote` (remote name or `null`), `trunk_ref` (the branch name at the remote that `base` follows, or `null` when it cannot be established), `ref_supported` (boolean; false when the name fails the Argument rules, so it is shown but never printed or requested).

## Layer

| Field | Type | Notes |
| --- | --- | --- |
| `index` | integer | 1-based, bottom first, contiguous |
| `refs` | string[] | 1 to 8 branch or bookmark names pointing at `tip_commit_id`; the first (by byte order) is the `primary_ref` |
| `primary_ref` | string | element of `refs` |
| `tip_commit_id`, `tip_change_id` | commit id, change id or `null` | |
| `fork_commit_id` | commit id | tip of layer `index - 1`, or the stack fork commit for layer 1 |
| `commit_count` | integer | 1..500 |
| `commits` | `{commit_id, change_id, subject}`[] | the layer's own commits bottom first, at most 20 shown; `subject` is the escaped first line, at most 200 bytes; `commit_count` is authoritative |
| `upstream` | Upstream or `null` | |
| `landed` | `pending` \| `applied` \| `partial` \| `unknown` | `applied` and `partial` only from patch equivalence (Git); `unknown` on jj and when not computed |
| `checked_out_in` | string[] | Git linked worktree paths other than the inspected one, at most 8; empty on jj |
| `jj` | `{conflicted, immutable}` or `null` | `null` on Git |
| `pull_request` | PullRequestRef or `null` | `null` without `--pull-requests` or when no pull request is linked |

Invariants: `landed` `applied` implies every commit is patch-equivalent; layers partition the range (sum of `commit_count` equals the range size); `primary_ref` is in `refs`; `fork_commit_id` of layer k+1 equals `tip_commit_id` of layer k.

## Upstream

`ref` (name of the configured upstream or tracked remote bookmark), `commit_id` (or `null` when the object is absent), `ahead_by` and `behind_by` (integers, capped at 10000, merge commits counted, or `null` when `commit_id` is `null`), `relation` (`same`, `ahead`, `behind`, `diverged`, `unknown` when the upstream ref exists but its object is absent, `gone` when an upstream is configured but its ref no longer exists; then `commit_id` is `null`).

## PullRequestRef

`number`, `state` (`open` \| `closed`), `merged` (boolean), `draft`, `head_ref`, `head_sha`, `base_ref`, `base_sha`, `merge_commit_sha` (or `null`), `link` (`verified` \| `name-matched`), `stack` (`{number, position, size}` or `null`), `candidates` (all candidate numbers when the layer is ambiguous, else `[number]`). Titles and logins are deliberately not carried.

## Remote

`repository` (`host`, `owner`, `name`, `selection` as in Spec 020), `sources` (one `{id, outcome, reason, requests}` per Remote sources table id, in table order, including `skipped`), `stacks` (`{number, open, base_ref, members: [{number, state, merged, head_ref, head_sha, base_ref}]}`, at most 8, members at most 32), `requests` (integer, at most 50).

Source `outcome` is `ok`, `denied`, `not-found`, `rate-limited`, `failed` or `skipped`; `reason` is `null` for `ok` and otherwise a reason of the table below.

## Finding

| Field | Type | Notes |
| --- | --- | --- |
| `code` | enum | the spec Findings table codes |
| `layer` | integer or `null` | layer index; `null` for stack-wide findings |
| `subject` | string or `null` | at most 256 bytes; used by `capability-unavailable` (`patch-equivalence`) |
| `severity` | `action` \| `notice` \| `info` | |
| `confidence` | `observed` \| `inferred` \| `unknown` | |
| `reason` | reason enum or `null` | non-null if and only if `confidence` is `unknown` |
| `summary` | string | the fixed template of the code (below) with escaped parameters, at most 512 bytes; no other text |
| `detail` | object | keys per the table below; no other keys |
| `evidence` | `{source, fields[]}`[] | one to six entries; `source` is a local source id (`git-refs`, `git-range`, `git-cherry`, `git-worktrees`, `jj-bookmarks`, `jj-range`) or a Remote sources table id |

`action` is never `unknown`. `unknown` findings say that a condition may matter, never that it does not.

### Reasons (normative)

| Reason | Meaning |
| --- | --- |
| `permission-denied` | the source answered `unauthorized` or `forbidden` |
| `not-found-ambiguous` | the source answered `not-found`; GitHub gives the same answer for absent and not permitted, and a host may lack the endpoint, so absence is not concluded |
| `rate-limited` | the source answered `rate-limited` |
| `source-failed` | the source failed with another class (including an oversized or malformed response) |
| `object-not-local` | a commit needed for a local comparison is not in the repository (not fetched) |
| `trunk-ref-unknown` | `--base` does not name a branch at a known remote, so the trunk ref is unknown |
| `jj-capability-gap` | the capability is not available on jj (spec Capability table) |
| `dependency-unavailable` | a source needed was skipped or unavailable |
| `unsafe-name` | a name fails the Argument rules, so it was neither requested nor printed |

### Detail keys per code

| Code | `detail` keys |
| --- | --- |
| `stack-behind-base` | `behind_by` (integer, capped at 10000, `capped` boolean) |
| `layer-ref-multiple` | `refs` (string[]) |
| `fork-point` | `refs` (string[], at most 20), `omitted` (integer) |
| `head-differs-from-upstream` | `upstream` (string), `relation` (`ahead` \| `behind` \| `diverged`) |
| `layer-applied-in-base`, `layer-partially-applied` | `equivalent` (integer), `total` (integer) |
| `layer-checked-out-elsewhere` | `worktrees` (string[], at most 8) |
| `capability-unavailable` | `capability` (`patch-equivalence`) |
| `layer-conflicted`, `layer-immutable` | `commits` (commit id[], at most 8) |
| `layer-ref-deleted` | `refs` (string[], deleted remote bookmark names, at most 8) |
| `pull-request-missing` | `refs` (string[] looked up) |
| `pull-request-ambiguous` | `numbers` (integer[], at most 5), `truncated` (boolean, true when the page was full) |
| `pull-request-closed-unmerged` | `number` |
| `head-sha-differs` | `number`, `local` (commit id), `remote` (commit id), `relation` (`local-ahead` \| `remote-ahead` \| `diverged` \| `unknown`), `link` (`verified` \| `name-matched`) |
| `base-mismatch` | `number`, `actual` (string), `expected` (string or `null`) |
| `stack-link-missing` | `numbers` (integer[], bottom first, at most 32) |
| `stack-split` | `stacks` (integer[]) |
| `stack-member-not-local` | `stack` (integer), `numbers` (integer[], at most 32) |
| `stack-order-differs` | `stack` (integer), `layers` (integer[]) |
| `native-stack-managed` | `stack` (integer), `position`, `size` |
| `parent-merged-child-stale` | `parent` (pull request number), `parent_merge` (`rebase` \| `merge` \| `rewritten`), `native_stack` (boolean), `link` (`verified` \| `name-matched`) |
| `parent-merge-not-local` | `number`, `merge_commit_sha` |

`stack-landed` has an empty `detail` object; no code has keys other than those listed.

### Summary templates (normative)

Each `summary` is exactly its template with `{…}` filled by escaped, bounded values; human output prints no other free text per finding.

| Code | Template |
| --- | --- |
| `stack-landed` | The tip is already contained in the base; there is nothing to stack. |
| `stack-behind-base` | The stack is based {behind_by} commit(s) behind the base. |
| `layer-ref-multiple` | Layer {layer} is named by more than one reference: {refs}. |
| `fork-point` | Other local references contain layer {layer}: {refs}. |
| `head-differs-from-upstream` | Layer {layer} differs from its upstream {upstream}: {relation}. |
| `layer-applied-in-base` | All {total} commits of layer {layer} are already in the base as equivalent patches. |
| `layer-partially-applied` | {equivalent} of {total} commits of layer {layer} are already in the base as equivalent patches. |
| `layer-checked-out-elsewhere` | Layer {layer} is checked out in another worktree: {worktrees}. |
| `capability-unavailable` | {capability} is not available for jj; pull request evidence can supply it. |
| `layer-conflicted` | Layer {layer} contains conflicted commits. |
| `layer-immutable` | Layer {layer} contains immutable commits. |
| `layer-ref-deleted` | Layer {layer} contains commits whose remote bookmarks were deleted: {refs}; they have probably landed. |
| `pull-request-missing` | No pull request from this repository was found for layer {layer} ({refs}). |
| `pull-request-ambiguous` | Layer {layer} has several candidate pull requests: {numbers}. |
| `pull-request-closed-unmerged` | The pull request for layer {layer} is closed and not merged. |
| `head-sha-differs` | The head of pull request {number} differs from layer {layer}: {relation} ({link}). |
| `base-mismatch` | Pull request {number} has base {actual}; expected {expected}. |
| `stack-link-missing` | Pull requests {numbers} are chained by base but not linked as a native stack. |
| `stack-split` | Layers belong to different native stacks: {stacks}. |
| `stack-member-not-local` | Native stack {stack} lists pull requests with no local layer: {numbers}. |
| `stack-order-differs` | The native stack {stack} orders layers {layers} differently from the local stack. |
| `native-stack-managed` | Layer {layer} is in native stack {stack} at position {position} of {size}; GitHub manages retargeting and rebasing. |
| `parent-merged-child-stale` | Pull request {parent} was merged ({parent_merge}) and layer {layer} still contains its old commits. |
| `parent-merge-not-local` | Pull request {number} is merged but its merge commit is not in this repository; fetch first. |

## Plan

| Field | Type | Notes |
| --- | --- | --- |
| `advisory` | boolean | constant `true` |
| `blocked` | `{layer, reason}` or `null` | `reason` is `layer-checked-out-elsewhere`, `layer-partially-applied`, `layer-conflicted`, `layer-immutable`, `pull-request-ambiguous`, `layer-ref-deleted`, `merged-layer-not-bottom` (layer: the lowest landed layer above a not-landed one) or `unsafe-name` |
| `native_checked` | boolean | `true` only under the exact condition of the Plan rules (remote section present and the pull request and native stack sources `ok`); `false` otherwise, including `--pull-requests` with a failed remote read (the boundary then carries the native-stack limitation) |
| `steps` | Step[] | at most 64, ordered |

### Step

| Field | Type | Notes |
| --- | --- | --- |
| `id` | string | `s1`, `s2`, … |
| `kind` | `fetch` \| `rebase` \| `link` \| `observe` | |
| `note` | `run-again` \| `object-unreachable` \| `stale-base` \| `take-upstream` \| `native-rewrite-pending` \| `upstream-ahead` \| `diverged-undecided` \| `null` | fixed text for `observe` steps; `null` otherwise |
| `layer` | integer or `null` | layer the step rewrites or describes; `null` for `fetch`, `link` and the `run-again` observe; set for every other `observe` (one step per affected layer, ordered by layer index bottom first) |
| `findings` | string[] | codes that cause the step |
| `effect` | `none` \| `local-network-read` \| `local-history-rewrite` \| `remote-write` | `observe` is `none`, `fetch` is `local-network-read`, `rebase` is `local-history-rewrite` (when run, checks out the layer branch and needs a clean worktree; the jj step also moves the working-copy commit with its uncommitted edits and every bookmark or commit descending from the moved change), `link` is `remote-write` (pushes unpushed commits, fast-forward only) |
| `performed_by` | `maintainer` \| `gh-stack` | `gh-stack` only for `link` |
| `argv` | string[] or `null` | the program and arguments; `null` for `observe` and for an unprintable step |
| `display` | string or `null` | `argv` with each element single-quoted where needed, at most 1024 bytes; `null` when `argv` is `null` |
| `environment` | `{name, value}`[] | only `link`: `GH_REPO` |
| `expected` | object | `rebase`: `{layer_tip, old_parent_tip, new_parent, new_parent_commit_id}` (commit ids; `new_parent` a name; `new_parent_commit_id` the sampled base commit for the first rebase step, `null` for later ones); `fetch`: `{remote}`; `link`: `{numbers}`; empty for `observe` |
| `requires` | string[] | ids of earlier steps |

Invariants: `rebase` steps are consecutive layers bottom up with each `requires` the previous one; `old_parent_tip` is the sampled `fork_commit_id` of that layer; no step has `effect` other than the table's; a `blocked` plan has no step for the blocked layer or above; an unsafe name gives `blocked`, never a partially quoted step.

## Boundary

`complete` (boolean; false for `incomplete` and `error`), `network` (boolean; true if and only if at least one remote request was made), `freshness` (`sampled`), `limitations` (string[], fixed statements such as "remote-tracking refs may be stale; nothing was fetched"), `limits` (`deadline_seconds`, `commits`, `layers`, `findings`, `steps`, `requests`, `result_bytes`).

## Schema notes

`stack.schema.json` is self-contained (no cross-file reference); identity definitions are copies guarded by a drift test; per-code `detail` and the invariants above that JSON Schema can express (`reason` non-null iff `unknown`, `plan` null when mode is `show` or status is `error`, non-null otherwise, `argv` null iff `display` null) are expressed with `if/then`; the remaining invariants are enforced by Go tests over every fixture. `hosting.schema.json`, `inventory.schema.json` and all other schemas are compared byte for byte against the base.
