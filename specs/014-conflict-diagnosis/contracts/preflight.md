# Isolated preflight and replay contract

**Status**: UNPUBLISHED design proposal. Original-write denial and Spec 015 D7
are binding safety constraints; owner choices remain in [spec](../spec.md#needs-owner-decision).

## Admission and capability matrix

| Capability | Proposed scope | Current evidence and gate |
| --- | --- | --- |
| Git existing diagnosis | Unmerged index stages and recognized interrupted-operation metadata, absent terms explicit | Native temporary-repository tests and no-write conformance still required |
| Native jj existing diagnosis | Stored conflicted commits at a pinned operation, no snapshot | Existing inspection isolation is a foundation; conflict parsing needs minimum/current measurement |
| Git restack/rebase preview | Explicit old-parent..source-head onto destination, single-parent range, plumbing/quarantine | Historical 015 Git 2.56.0 experiment only; complete 014 gates/lanes pending |
| Git merge preview | Explicit heads plus accepted native base/strategy interpretation | OQ-SEM/TOPO/SCRATCH; unavailable until measured and accepted |
| Native jj preview | Native commit/change/operation binding, selected parent/descendant scope, private store | OQ-SEM/TOPO/SCRATCH; unavailable until measured and accepted |
| Selected hunk extraction | Exact paths only, reliable native origin/ranges and finite context | OQ-HUNK; disabled until accepted and conformant |

Git and jj are Tier 1 peers with explicit capability limits, not assumed feature
parity. Ownership chooses native backend including jj colocation. GitButler,
ambiguous ownership, missing enforcement or unsupported native version refuses.
No shared original-store writes, raw Git fallback or unsandboxed fallback.

## Git linear replay profile (proposed initial scope)

Bind full old-parent P, source-head H and destination N. Require P ancestor of H,
complete local history and a unique related-history interpretation. Enumerate
exactly P..H oldest first; record every commit/parent. Recommend refusal of merge
commits, empty/redundant commits, shallow history, replace/graft effects and
attribute/config hazards before replay; OQ-SEM/TOPO records final accepted scope.
A recognized unsupported profile is error/capability-unavailable, never best effort.

Replay each moved commit using its original parent as the three-way base and
current scratch tip as destination, through reviewed `merge-tree`/`commit-tree`
plumbing. Original author/date may be carried into disposable commits as 015 does;
fixed scratch committer values are isolated inputs, never inferred owner identities.
No configuration writes or real apply identity checks are performed here.

Stop at first conflict. Emit native path/kind terms with the failing moved commit,
record the clean prefix and mark every suffix commit not-evaluated. Conflicted
preview can be complete for this stop-first contract; it never predicts all later
conflicts. Predicted-clean requires the entire declared range and successful
unchanged resampling/cleanup. Empty overlap, equal trees or marker absence does
not establish semantic correctness, identity verification or mutation permission.

A merge request uses its own measured native strategy and accepted base selection;
never reduce it to this replay profile or infer an ambiguous mainline. No custom
merge driver, rerere, external diff/textconv/filter or repository-selected options.
Gitlinks are identities only; no submodule traversal, initialization or fetch.

## Scratch confinement and retirement

Before native replay, allocate a create-only private 0700 directory outside all
original repositories and linked workspaces; reject symlink/non-owned/replaced
parents. Disclose allowed setup/config/temp/object writes as private scratch.
Every writable object directory, lock, config and temporary path is private.
Original object stores are read-only sources/alternates only, with lazy-fetch,
gc, maintenance and optional original locks disabled. Source read permissions do
not permit object freshening or packing writes. Confine the process to denied
original writes and denied network; output comparison alone is not enforcement.

A linked worktree or jj workspace sharing the original object/operation store is
not enough. Native jj must prove independent writable store/operation metadata,
no original working-copy snapshot, pinned inputs and no descendant effects outside
scratch. If that cannot be enforced, capability-unavailable is the correct result.
Do not use Git to simulate jj merely because the repository is colocated.

Reap children before cleanup. Delete only the create-owned scratch handle after
checking its identity, never a path obtained from repository text or native output.
No glob cleanup, user-selected recursive deletion, asynchronous cleaner or import
of candidate objects. On failure record scratch residue plus bounded opaque handle;
no source path or secret is needed in the handle. Cleanup failure yields incomplete
and prevents a complete predicted-clean result. Crash may leave private residue;
this command makes no automatic crash-cleanup or checkpoint recovery claim.

## Freshness and consumer handoff

Before/after compare canonical layout/ownership, resolved reference targets,
selected commit/change/operation identities and declared native state. Immutable
ids cannot detect every concurrent object deletion, metadata race or ABA move;
missing material or observed changes yields incomplete/unknown. No transaction
or globally stable snapshot is claimed. Do not follow a ref to its newer value
mid-replay or automatically rerun against different inputs.

Spec 015 may reuse typed evidence only after comparing exact backend/capability,
inputs, semantic profile/options and completeness. It must revalidate ownership,
protections, worktrees, configuration, expected refs and all mutation guards;
real apply candidate evidence and actual commit identity checks remain its own.
Spec 013 creates its own checkpoint/recovery and reverse-action plan. Spec 016
requires its separate action authorization, observed full remote SHA and applicable
publication gates; preflight supplies none of them. For a documentation/specification-only
topic push, the change-class policy requires the applicable documentation checks
and, for a pure rebase, equal `git range-diff` proof; independent review before that
push is not required. A branch changing code, tests, scripts or generated files
requires independent review of the exact actual new SHA before push; a pure rebase
with equal range-diff may use the middle-tier reviewer. Ready always requires
independent CLEAR at the exact current head, green required CI, `rebaseable == true`
and no remaining scope blocker, regardless of change class. A head change
invalidates its exact-head review evidence. Spec 043 stays a separate human-only
invocation. No evidence here authorizes apply, lease push, PR edit, Ready, merge,
tag, release or settings.
