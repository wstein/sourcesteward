# Integration proof contract

Subordinate to the [specification](../spec.md) FR-001..FR-012.

## Fixed native operations

| Kind | Native operations (fixed typed argv, OIDs only) | Success rule |
| --- | --- | --- |
| `ancestor` | `merge-base --is-ancestor TIP BASE` | exit 0 |
| `patch-id` | `merge-base BASE TIP`; `rev-list --count --no-merges BASE..TIP` and `M..BASE` (bounds); `cherry BASE TIP` | every output line `- OID`, at least one line |
| `merge-noop` | `merge-tree --write-tree --no-messages BASE TIP`; `rev-parse BASE^{tree}` | exit 0 and printed tree equals base tree |

All run under the planning environment of the [configuration policy](configuration-policy.md) and the private object directory below. `git cherry` omits merge commits; its `+`/`-` lines give matched/unmatched; the count of skipped merges comes from `rev-list --count --merges M..TIP`.

## Exit and output handling

`cherry`: stdout lines beyond 1,024, any line not matching `^[+-] [0-9a-f]{40,64}$`, nonzero exit or stderr overflow produce `not-evaluated` (`proof-limit` or the failure class). `merge-tree`: exit 0 with a single tree OID line is the only success shape; exit 1 is `conflict` (stdout ignored except the first line already read as tree); any other exit, multi-line output, or a tree that is not an OID is a failure class, not a proof. Timeout and cancellation are failures, never partial proof.

## Private object directory

`GIT_OBJECT_DIRECTORY=<fresh mode-0700 dir under the plan's private temp>`, `GIT_ALTERNATE_OBJECT_DIRECTORIES=<repository objects>`; removed on every exit path including panic and cancellation. Objects written by `merge-tree` live only there. The repository `objects/` tree and `packed-refs`/refs are compared byte-wise by the test before and after.

## Refusal rules

1. Ref is protected (base, symbolic, workspace-used) or uncertain: evidence recorded, disposition retained.
2. `merge-noop` refused: `merge.*.driver`, executable `filter.*`, `diff.external`, private-directory failure, Git older than 2.38 (not applicable on supported lanes).
3. `patch-id` bounds exceeded: `proof-limit`, other kinds continue.
4. Aggregate 60 s deadline exceeded: remaining tips `proof-budget-exhausted`.
5. Anything else not `proven` is retained `integration-not-proven` with its classification. No branch is ever deleted by similarity, by cherry-pick replay or by a user-named range.

## What each kind does not claim

`patch-id`: not that the change is still present. `merge-noop`: not that the commits were reviewed or merged through a PR; not authorship. `ancestor`: not review. None says anything about CI, owners or other branches.
