# Isolated preflight and replay contract

> **Owner-decision amendment — 2026-10-25:** all former OQ IDs are decided;
> [current profiles](../../016-stacked-pr-coordination/contracts/owner-decisions.md) supersede earlier undecided recommendations.
> Shared human/shortening-timeout flags adopted; preflight remains offline/helper-free.
> This is a documentation target, not implementation or native acceptance.

**Status**: UNPUBLISHED design proposal. Original-write denial and Spec 015 D7
are binding safety constraints; dated owner decisions and measurement gates are in [spec](../spec.md#needs-owner-decision).

## Admission and capability matrix

| Capability | Proposed scope | Current evidence and gate |
| --- | --- | --- |
| Git existing diagnosis | Unmerged index stages and recognized interrupted-operation metadata, absent terms explicit | Native temporary-repository tests and no-write conformance still required |
| Native jj existing diagnosis | Stored conflicted commits at a pinned operation, no snapshot | Existing inspection isolation is a foundation; conflict parsing needs minimum/current measurement |
| Git restack/rebase preview | Explicit old-parent..source-head onto destination, single-parent range, plumbing/quarantine | Historical 015 Git 2.56.0 experiment only; complete 014 gates/lanes pending |
| Git merge preview | Explicit heads plus accepted native base/strategy interpretation | OQ-SEM/TOPO/SCRATCH; unavailable until measured and accepted |
| Native jj preview | Native commit/change/operation binding, selected parent/descendant scope, private store | OQ-SEM/TOPO/SCRATCH/JJCONF; unavailable until measured and accepted |
| Selected hunk extraction | Exact paths only, reliable native origin/ranges and finite context | OQ-HUNK; disabled until accepted and conformant |

Git and jj are Tier 1 peers with explicit capability limits, not assumed feature
parity. Ownership chooses native backend including jj colocation. GitButler,
ambiguous ownership, missing enforcement or unsupported native version refuses.
No shared original-store writes, raw Git fallback or unsandboxed fallback.

## Git linear replay profile (proposed initial scope)

Bind full old-parent P, source-head H and destination N. Require P ancestor of H,
complete local history and a unique related-history interpretation. Enumerate
exactly P..H oldest first; record every commit/parent. The initial linear profile
refuses merge commits, shallow/replace/graft-affected history and every execution
hazard below before replay. Empty/redundant commit treatment remains OQ-SEM;
unaccepted variants refuse. OQ-TOPO governs future topology extensions, never
an exception to isolation or hazard refusal.
A recognized unsupported profile is error/capability-unavailable, never best effort.

Replay each moved commit using its original parent as the three-way base and
current scratch tip as destination, through reviewed `merge-tree`/`commit-tree`
plumbing. Original author/date may be carried into disposable commits as 015 does;
fixed scratch committer values are isolated inputs, never inferred owner identities.
No configuration writes or real apply identity checks are performed here.

For this Git linear kernel, stop at first conflict. Emit native path/kind terms with the failing moved commit,
record the clean prefix and mark every suffix commit not-evaluated. Conflicted
preview can be complete for this stop-first contract; it never predicts all later
conflicts. Predicted-clean requires the entire declared range and successful
unchanged resampling/cleanup. Empty overlap, equal trees or marker absence does
not establish semantic correctness, identity verification or mutation permission.

A merge request uses its own measured native strategy and accepted base selection;
never reduce it to this replay profile or infer an ambiguous mainline. No custom
merge driver, rerere, external diff/textconv/filter or repository-selected options.
Gitlinks are identities only; no submodule traversal, initialization or fetch.

Native jj preview must satisfy OQ-JJCONF in addition to scratch isolation: native
rebase can retain committed conflicts and rewrite descendants, including revisions
that later resolve them. Git stop-first/not-evaluated suffix rules do not apply.
The [native coverage model](../data-model.md#replay-coverage-and-scratch-lifecycle)
requires accepted complete graph/aggregate semantics before this capability opens.

## Binding execution isolation and hazard refusal

Both diagnosis and preflight MUST meet the minimum runner controls in
`internal/restack/git.go` and `internal/restack/hazards.go`; their names alone
are not a sandbox. Use fixed argv with no shell and a trusted native executable.
Build the environment from scratch: no inherited `GIT_*`, loader/shell injection,
trace, credential or helper variables. Set private HOME/XDG_CONFIG_HOME,
`GIT_CONFIG_GLOBAL`/`GIT_CONFIG_SYSTEM` to the null device,
`GIT_CONFIG_NOSYSTEM=1`, `GIT_ATTR_NOSYSTEM=1`, `GIT_NO_LAZY_FETCH=1`,
`GIT_OPTIONAL_LOCKS=0`, `GIT_TERMINAL_PROMPT=0`, `LC_ALL=C`, inert askpass/editor
and pager settings. Replay additionally sets only internally selected private Git admin/worktree/index
and object paths, its bound attribute source and read-only source alternates. Start with `--no-optional-locks`,
`--no-replace-objects`, `--no-pager` and the following mandatory `-c` pins:

| Pins | Required values |
| --- | --- |
| Helpers/worktree | core.hooksPath=null device; core.fsmonitor=false; core.autocrlf=false; core.quotePath=true; submodule.recurse=false |
| Merge | merge.renormalize=false; merge.renames=true; merge.directoryRenames=conflict; merge.renameLimit=1000 |
| Diff selection | diff.renames=false; diff.external=empty; diff.algorithm=myers; diff.indentHeuristic=true; diff.orderFile=null device; diff.relative=false; diff.ignoreSubmodules=none |
| Diff format | diff.noprefix=false; diff.mnemonicPrefix=false; diff.context=3; diff.interHunkContext=0; diff.colorMoved=false; diff.wsErrorHighlight=none |
| Maintenance/protocol | gc.auto=0; maintenance.auto=false; protocol.ext.allow=never |
| Rendering | log.showSignature=false; log.decorate=no; log.mailmap=false; color.ui=never; i18n.logOutputEncoding=UTF-8 |

Before any replay, list configuration with `config --list -z --no-includes`
under the isolated environment, without adding the pins to that listing. Refuse
unreadable config, includes/includeIf, extensions.*, core.hooksPath,
core.fsmonitor, core.attributesFile, merge.* driver/name/recursive, filter.*
clean/smudge/process/required, diff.* command/textconv/cachetextconv/binary and
diff.external. Refuse grafts, shallow history, repository object alternates,
nonempty info/attributes and replace refs, as the 015 hazard gate does. Report
hazard names only. Local user.name/email is a disclosed non-refusing diagnostic,
never an expected identity or replay identity source.

For every touched path, run bounded NUL-delimited
`check-attr --source=H -z --stdin merge diff filter` and the same check with
`--source=N`. Refuse every value other than unspecified, including explicitly
set/unset merge/diff/filter attributes and LFS filter attributes. Refuse any moved
.gitattributes file before replay; unreadable attribute evidence also refuses.
A future merge profile must provide equivalent accepted checks over every tree
whose attributes can affect its selected paths, otherwise it remains unavailable.
The actual merge invocation MUST use the same immutable attribute policy, not
merely perform source-tree checks beforehand. For the initial Git linear profile,
set `attribute_source.policy=source-head` and `attribute_source.commit_id=H`, where
H is the full sampled source-head commit id. Every `merge-tree` invocation MUST
include the global `--attr-source=FULL_H` argument before the subcommand. Never
accept an inherited GIT_ATTR_SOURCE or silently omit the argument. The official
[Git option contract](https://git-scm.com/docs/git) documents the equivalent
GIT_ATTR_SOURCE mechanism; this profile selects the explicit argv spelling.
Capability evidence must prove that exact native version honors it during actual
merge-tree execution, not just accepts a flag or returns check-attr output.

Run that replay in a create-owned private Git administrative/worktree/index
context of the matching object format, with only the original object store as a
read-only alternate. Do not copy original config, index or info/attributes into
that context. Keep private info/attributes empty/absent and private worktree/index
free of attribute overrides. This prevents original untracked, modified or
index-only .gitattributes, and original info/attributes, from becoming an additional
attribute source. The original info/attributes refusal and global/system/config
isolation above still apply; --attr-source alone does not suppress that override.

Immediately before every merge-tree call, revalidate the private context's
ownership/no-follow identity, empty attribute-override locations, bound H and N
object identities, full-H argv pin and accepted native capability. Verify the
same context and attribute binding again before the final result. Capture the
actual invocation's effective policy/full commit pin in the result binding and
per-call receipt; declared intent without the enforced invocation is insufficient.
Changed guards yield incomplete/unknown; missing pin, unsupported/unmeasured
native enforcement or inability to exclude mixed attributes refuses
capability-unavailable before replay. No original-worktree/index or unpinned
fallback. Native merge profiles require their own explicit immutable attribute
source with equivalent guards before enablement.

Pinned merge options can differ from the repository's configured merge behavior;
state that limitation in every preflight result rather than claim parity.

Diagnosis uses the same sanitized runner, helper suppression and enforced original
write/network denial. Its Git observations are read-only plumbing such as
`ls-files -u -z`, `rev-parse` and bounded no-follow reads of recognized operation
files; never `git status`, index refresh, filters or fsmonitor. The 015 target-dirty
and operation-in-progress apply refusals are not copied into diagnosis: an existing
interrupted operation is the evidence it reads, not permission to continue it.

Native jj diagnosis/preflight also MUST isolate user configuration: an allowlisted
environment with private HOME/USERPROFILE/XDG config, cache, data and TMPDIR,
`JJ_CONFIG=` suppression as supported by the measured native version, no inherited
JJ_USER/JJ_EMAIL, tracing or helper variables, and fixed no-pager/color settings.
Diagnosis pins `--ignore-working-copy --at-operation=FULL_OPERATION_ID` inside
read-only/network-denied enforcement. Preview requires independently writable
scratch store/config/oplog plus accepted native semantics. Unmeasured config
suppression or enforcement refuses capability-unavailable; original/user settings
must never silently supply merge tools, aliases or identity defaults.

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
owns publication and Ready gates; preflight evidence satisfies none of them.
Spec 043 stays a separate human-only invocation. No evidence here authorizes
apply, lease push, PR edit, Ready, merge, tag, release or settings.
