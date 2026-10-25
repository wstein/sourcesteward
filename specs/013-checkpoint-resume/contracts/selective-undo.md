# Proposed Contract: Explicit Selective Local Undo

**UNPUBLISHED — unimplemented**, 2026-10-25. The admitted native capability set is
**Needs owner decision** OQ-2; these examples select semantics, not implementation.
Shared [bounds/output](checkpoint.md) and [recovery guards](recovery.md) apply.

```text
stw undo plan --path WORKSPACE --state-dir PRIVATE_DIR --target NATIVE_ID --semantics inverse-change [--parent FULL_PARENT_ID] [--select-path PATH] [--json]
stw undo plan --path WORKSPACE --state-dir PRIVATE_DIR --target NATIVE_ID --semantics history-restoration --select-ref FULL_REF [--select-descendant FULL_ID] [--json]
stw undo plan --path WORKSPACE --state-dir PRIVATE_DIR --target NATIVE_OPERATION_ID --semantics operation-restoration [--select-descendant FULL_ID] [--json]
stw undo apply --plan UNDO_PLAN_ID --state-dir PRIVATE_DIR --authorize [--json]
```

## Caller-selected meaning

| Semantics | Exact intended effect and refusal boundary |
| --- | --- |
| `inverse-change` | Make a new supported native inverse change for the selected commit/change delta, optionally narrowed to admitted paths. Original history remains. Bind target, destination baseline and explicit merge parent; conflicts are reported, not resolved or widened automatically. |
| `history-restoration` | Restore selected owned local native refs/history to a retained explicit state with expected-current guards. Plan every rewritten descendant and affected ref/workspace. No implicit ref deletion, default-branch reset, owner-checkout resync or rewrite of shared/foreign work. |
| `operation-restoration` | Native jj-only admitted operation restoration with complete effects/descendants known and selected; refuse if the native operation would affect unselected state. A broad operation undo is never substituted for selective inverse/history undo. |

Without an explicit target and semantics, no undo is selected. An explicit
`inverse-change` target selects its admitted delta; optional path selection can
only narrow that delta. No missing selector defaults to latest commit/operation,
current branch, first merge parent, all descendants or unrelated file layers. Require a full explicit
parent that is actually one of the target merge's parents whenever a merge delta
is reversed; show its native identity and exact delta. Multiple parents or an
unsupported topology cannot be silently flattened. Git commit/merge identity,
jj commit/change identity and jj operation identity are not aliases. A jj
operation id alone cannot restore uncaptured disk edits or external effects.

## Inverse-change destination guard (first-slice proposal)

Recommend Git inverse commits be computed by fixed plumbing in an owned private
object quarantine, following 015 D7's effect discipline, then promoted only by
the reviewed object/ref action with durable pre-action recovery and an exact
expected-old/new CAS. Do not invoke porcelain `git revert` in a real worktree.
Candidate computation cannot write the original object store, index or disk;
conflicts/unknown results refuse before promotion. This is a proposed OQ-2 native
capability, not approval to reuse an unreviewed command or a quarantine SHA as
publication evidence.

For every admitted backend, refuse an inverse destination checked out or used
as the working-copy commit in **any** worktree/workspace, including the invoking
owner's clean workspace. Also refuse dirty selected paths, unreadable state or
unknown/incomplete occupancy inventory. Report `changed-since` with the concrete
occupancy/dirty-state blocker, rechecking before each effect; preserve all index,
disk and workspace state. Never move a held ref and leave its worktree stale,
stash/reset edits, switch branches or silently refresh a working copy. A later
occupied/dirty-destination capability needs explicit OQ-2 acceptance and tests.
Native jj must prove an equivalent native mechanism without Git plumbing or
implicit snapshot/update; unsupported capability refuses.

## Native jj operation guard (OQ-2 proposal)

The operation log is shared by all workspaces of a repository. Inventory every
workspace working-copy commit and the complete operation-head set; the invoking
workspace alone cannot establish safe scope. Every jj repository read uses
`--ignore-working-copy` and explicit `--at-operation` (initial `@` discovery,
then full pinned ids) under enforced read-only isolation. Every restore/undo
invocation also uses `--ignore-working-copy` and the expected current full
operation id; never gain snapshot/update permission by omitting a flag. Any
future explicit capture snapshot is a separate OQ-1 capability/effect.

Recommend operation-restoration admission only when the desired saved view is
the direct parent of one current head and no other workspace's working-copy
commit or unselected state changes. Bind saved view, expected head, parent,
complete workspace effects and exact native semantics in the plan. Restoring
an earlier view creates a new operation and can undo all later operations;
reverse-applying a selected older operation atop later work has different
semantics. Neither is time travel or an implicit selective inverse. The latter
capability and version-specific `restore`/`revert`/older `undo` argv remain
unadmitted until OQ-2/native conformance; these names are not interchangeable.

Recheck sole current head immediately before the effect and verify the expected
new operation/parent and all workspace deltas immediately afterward. Divergent
heads or changed parent/workspace evidence before apply refuse `changed-since`
without automatic reconciliation/merge. An unexpected/divergent head after an
effect is partial/changed-since with retained evidence, not success or an
automatic rollback. If op abandonment/GC has removed a required operation or
object, report `unavailable`, refuse mutation and show the lost guarantee;
never invoke abandon, GC, repair, snapshot or another native operation to hide it.

## Reviewable plan and fresh guards

Resolve explicit target/destination and selected scope offline. Present complete
selected/excluded files, index/disk layers, refs/bookmarks, native identities,
rewritten descendants, operation-log/snapshot effects, expected current values,
recoverable pre-action roots/selected bytes, conflicts, limitations and bounded
evidence. An unsupported selector, omitted required parent or incomplete effect
set refuses instead of producing an applicable plan. Default action is none.

Require current task/actor ownership and native layout proof from OQ-4's approved
source; commit author/config values or a name match do not prove ownership.
Reject shared/published history, foreign/unknown ownership, protected/default
refs, another workspace's active/checked-out target and unselected descendants.
An inverse change is also refused on shared/foreign targets in this slice; it
cannot become a loophole for modifying another owner's workspace. Offline
absence of a remote-tracking ref cannot prove unpublished history; if trusted
local evidence cannot establish required scope, report `ownership-unknown` or
`sharedness-unknown`. Do not fetch or consult credentials to make the proof.

Apply requires explicit local authorization plus the exact stored plan id and
revalidates all relevant current paths/layers/native ids, target parents,
descendant closure, protections, linked workspaces, ownership, capability,
configuration/hook hazards and recovery availability before each effect. A plan
or checkpoint is not transferable authorization; stale/unreadable state refuses.
Use only the native reviewed adapter; jj colocation still uses jj. No raw Git
fallback, hook/helper execution, prompt, network, stash, automatic reset/abort,
push, PR edit, merge, tag or release occurs. New commit identity/attribution is
explicitly reviewed with OQ-2/OQ-4; repository config never invents the actor.
New inverse or rewritten commits cannot silently inherit signature validity.
Disclose source signature/header loss and required-signing capability separately
from patch/range equivalence. Execute no signing helper; refuse an unmet signing
requirement or an undecided signature policy instead of silently signing or
stripping a signature. OQ-2 gates that capability. No runtime signature experiment
is claimed by these artifacts.

## Partial results and retry

Reuse the reviewed typed recovery seam: create valid pre-action roots and any
selected pre-action byte receipts, durably write prepared intent before each
effect, then resample and record actual completion. Retain exact original/new
native identities and mapping. A crash, failed payload/record write, conflict,
timeout or cancellation stops further work with completed/not-started/unknown
effects, retained recovery and precise missing guarantees. Never pretend to
atomically undo multiple refs, descendant rewrites and disk/index writes.

Retry verifies the same action identity and expected state; known desired state
means already-applied, changed state refuses, unknown new head means
journal-incomplete. No blanket native operation undo or reset repairs a partial
run. A recovery restore is a new selected, authorized plan; it cannot reverse
publication, external helpers/hooks, uncaptured buffers/files or expired payloads.

Conformance must separately prove native inverse commit/change behavior, explicit
merge-parent reversal, path narrowing, descendant completeness, shared/foreign
refusal, conflicts, cancellation and crash reconciliation. Unsupported capability
is a typed limit, never a passed mutation test. Publication stays in Spec 016;
Spec 043 remains the human owner's separate landing invocation.
