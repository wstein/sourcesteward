# Proposed contract: native jj local restacking

> **Owner-decision amendment — 2026-10-25:** all former OQ IDs are decided;
> [current profiles](../../016-stacked-pr-coordination/contracts/owner-decisions.md) supersede earlier undecided recommendations.
> Target exit revision 2 is 0/1/2; current code still 3. Optional actual re-sign is separately gated.
> This is a documentation target, not implementation or native acceptance.

**Status (2026-10-25): UNPUBLISHED, unimplemented, capability gated.** D4 keeps
hidden v1 restack Git-only (`unsupported-backend` for jj, no Git fallback).
This future peer contract does not imply parity of implementation or semantics.
Owner decision OQ-4 and minimum/current native evidence precede enablement.

## Capability and identities

Detect ownership before any native invocation: colocated and standalone jj both
use jj; absent/unsupported jj, ambiguous or GitButler ownership refuses. No Git
write, library installation, initialization, migration or unsandboxed fallback.
Separate native commit id, stable change id, operation id, local bookmark target
and workspace working-copy identity. Never encode a change id as a Git SHA or
claim rewritten commit ids remain stable.

A proposed native plan must pin the observed operation, explicit source commits
and change ids, destination commit, selected owned bookmark and exact descendant
set, plus all affected bookmarks/workspaces. Read-side planning must use native
snapshot-suppressed operation-pinned observations. Disclose isolated scratch
operation-log writes as temporary effects; no original working-copy snapshot,
operation log, files or bookmark changes from observation.

## Clean-only native apply proposal

The capability spike must establish a documented native mechanism that replays
only the selected source range with exact destination semantics and reports all
automatically rewritten descendants/bookmarks. It must also establish an
expected-operation guard that refuses concurrent/divergent operation heads
without merging them and exact bookmark expectations before publishing a native
operation. No `jj rebase` vector is invented here without that proof.

Native jj may rewrite descendants automatically: a plan cannot silently borrow
that effect from Git's single-ref model. Every descendant must be explicitly
selected and owned, or the capability refuses. Immutable/shared changes,
conflicted or divergent bookmarks/operations, stored conflict values, unresolved
ownership and affected foreign workspaces refuse. A clean preview cannot override
a native immutability or ownership restriction.

Before any mutation durably record the native prior operation, commit/change
mapping, selected bookmark targets and recovery roots through the typed recovery
architecture. Before native publication revalidate operation/bookmarks/workspaces,
identities and protections. The exact retained operation and bookmark restoration
effects need measured proof; no claim of native atomic CAS, safe `op undo` or
whole-repository rollback is made until the spike supports it. If an interface
cannot prevent unselected effects, report `capability-unavailable` with reason
`jj-restack-capability-gap` and leave original native state unchanged.

After successful local publication expose actual new commit/change/operation
identities and `review_gate.state: pending-policy-check` bound to the actual new
commit identity, with derived change class; unavailable Git-equivalent purity
evidence means `pure_rebase: false` and never lowers the review tier. A native jj rewrite has its own completeness/equality evidence; do not
fabricate Git range-diff `=` rows. A later GitHub push capability needs explicit
native bookmark export semantics, exact remote advertisement, review/check and
attribution evidence under Spec 016, otherwise it remains unavailable. Never use
Git push as a fallback inside jj.

## Conformance and refusal fixtures

Required minimum/current jj cases: standalone, colocated and secondary workspace;
stored-versus-disk divergence without snapshot; one clean owned bookmark rewrite;
commit id changes with preserved change identity; explicitly selected descendants;
immutable/shared revision; conflicted bookmark or stored conflict; divergent
operations; concurrent operation/bookmark change at commit point; cancellation
before/after native publication; durable recovery failure and guarded restore;
unavailable sandbox and helper/hook/network sentinels. Record all actual effects
on native operation heads, bookmarks, working-copy snapshots, Git colocation
metadata, files and neighbours. Missing native lanes block the capability.

Schema fields remain Markdown proposals until reviewed native semantics,
implementation and offline conformance fixtures exist. Future exits follow the
unified 0/1/2 target; hidden v1 JSON/schema bytes remain frozen. CURRENT code
still returns incomplete 3 until the explicit 015 revision-2 task; native jj remains capability-unavailable until its measured admission. No network, editor, prompt, hook or repository-supplied command runs.
