# Backend readiness: Git, Jujutsu and GitButler

Git and Jujutsu (jj) are Tier 1 peers under constitution 1.1.0. `stw inspect` implements
bounded native identity inspection for ordinary Git and standalone, colocated and secondary
jj workspaces. Native jj support is implemented in Spec 008; final platform/conformance
acceptance is tracked in the [verification record](../specs/008-jj-inspection/verification.md).
GitButler remains a separately specified future integration. Tier 1 commits to native support,
versioned contracts and conformance evidence, preserving each backend's semantics.

## Capability model
| Concept | Git | Jujutsu (jj) | GitButler |
| --- | --- | --- | --- |
| Revision identity | commit OID | change ID plus current commit OID | documented stable identifiers plus commit OIDs |
| Named references | branches, tags, remote refs | bookmarks, tags, Git refs | branches and stacks, managed workspace state |
| Recovery evidence | operation-specific recovery refs/record | operation ID and supported restore behavior | operation log and supported undo/restore |
| Workspace | index plus worktree | working-copy commit and workspace | managed parallel/stacked branches and shared worktree |
| Inspection effects | command/options may refresh index or read config | many commands snapshot working copy; mode/options matter | command/version may refresh forge or workspace metadata |

Common result fields cover diagnostics, filesystem changes and action outcomes. Preserve
backend-specific facts in explicit extensions. Never equate a jj change ID with a Git OID,
a bookmark with every branch behavior, or an operation log with undoing remote effects.

## Selection and ownership
1. Identity inspection detects recognized backend metadata before ordinary-Git HEAD queries.
   Detection is a heuristic: unregistered GitButler single-branch mode and unknown marker
   conventions may remain undetected. See the [inspection contract](../specs/005-repository-inspection/contracts/inspect.md).
2. Refuse ambiguous ownership and explain the evidence; do not initialize or migrate.
   A future explicit selection option requires its own ownership-safe contract.
3. A colocated jj workspace remains jj-managed and must use its native adapter. Missing or
   unsupported jj must report an actionable error; installing jj does not select it for plain Git.
   GitButler-managed state remains GitButler-owned.
4. Advertise only commands supported by the detected backend/version and output contract.
5. Never silently fall back to raw Git writes. Read-only Git plumbing is allowed only when
   that adapter has specified and tested its effects and compatibility.
6. If both jj and GitButler metadata appear, report ambiguity and retain state; do not guess.

## Effects and plans
Each operation declares filesystem/repository writes, snapshots, index refresh, network,
hooks, editor/prompt requirements and recovery scope. Identity inspection never writes the
inspected workspace or repository. Native jj creates and removes private temporary configuration
and probe files outside the inspected target; it excludes caller/repository configuration text.

Native jj requires usable Linux bubblewrap or macOS sandbox-exec enforcing filesystem write
and network denial. Missing/denied enforcement fails with `jj-sandbox-unavailable`; there is
no unsandboxed retry. Fixed trusted identity commands and isolated configuration prevent helper
invocation; Linux sandboxing does not claim executable denial. macOS additionally restricts
executable launch. Read-side jj index rebuilds or metadata migrations are denied rather than
allowed to repair the repository.

Every native read suppresses snapshots and pins operation selection. Results explicitly say
`stored-unsnapshotted`: disk edits/untracked files are not represented, freshness is unknown,
and sampling cannot guarantee a transaction or detect all concurrent/ABA changes. Divergent
operations and stale workspace state never trigger automatic merging or workspace updates.
Native inspection has a ten-second total deadline and 64 KiB per-command output bound.

Bind plans to backend, repository/workspace identity, relevant refs, changes and operation
identity where supported. Revalidate before applying; reject stale plans. Recovery is backend-
and operation-specific. Locks inside Git/jj do not isolate agents editing the same files.
Use separate workspaces/worktrees when tasks need separate filesystem or runtime state.

## Tier 1 support and delivery

| Backend | Current accepted capability | Version support | Next acceptance gate |
| --- | --- | --- | --- |
| Git | Bounded identity inspection | Git 2.46+ | Preserve existing fixtures and v1 schema |
| jj | Native stored identity inspection implemented; final acceptance tracked in Spec 008 | Stable jj 0.45.1+; enforced sandbox required | Minimum/current conformance and no-target-write evidence |
| GitButler | Recognized ownership detection and explicit unsupported result | No native adapter supported | Separate specification |

Each Tier 1 adapter must declare supported minimum/current versions and capabilities, and
exercise both in conformance CI. Supported capability regressions block release equally for
Git and jj. Tests must cover bounded output, noninteractive execution, missing/unsupported
executables, stale/concurrent state and absence of unintended writes. Capability limits and
backend-specific identities stay visible in human output, JSON schemas and documentation.
Published v1 contracts remain compatible; incompatible result shapes require a new version.

Git conformance pins minimum 2.46.0 and current 2.56.0; jj conformance pins minimum 0.45.1
and current 0.46.0. Later stable versions must retain the supported identity interfaces; an
untested release is not equivalent to recorded conformance. Git/version keep published schema
v1; selected native jj success/errors use schema v2 with distinct workspace, change, commit and
operation identities. Ambiguous/GitButler/pre-selection outcomes retain schema v1. See
[JSON contracts](json-contracts.md).

Expand status/diagnosis for both backends through reviewed specifications. Mutations require separate per-backend plans,
state validation and recovery evidence. Do not implement one backend's missing behavior by
silently performing another backend's writes.

## Primary references
- https://docs.jj-vcs.dev/latest/git-compatibility/
- https://github.com/jj-vcs/jj/blob/main/docs/working-copy.md
- https://github.com/jj-vcs/jj/blob/main/docs/operation-log.md
- https://docs.gitbutler.com/cli-guides/cli-tutorial/scripting
- https://docs.gitbutler.com/ai-agents/parallel-agents

## Inventory capability boundary

`stw inventory` extends identity with bounded native workspace/reference evidence and
explicitly scoped file observations. Both Tier 1 backends support reference-existence
and ancestry predicates with separate false, unknown and execution-failure outcomes.
Git disk/index state and jj stored working-copy state retain different semantics; jj
disk freshness remains unknown and staging unsupported. Missing/foreign registrations
are retained as evidence, not repaired. Configured jj aliases are not evaluated.
See [inventory](inventory.md) for command scopes and [Spec 009 verification](../specs/009-workspace-inventory/verification.md)
for actual acceptance; no new capability is verified by historical Spec 008 evidence.

## Isolated task workspace capability

Reviewed plan/apply creates a detached Git worktree or native jj workspace from an explicit
base; handoff and retirement preparation retain owned state. Native creation has declared
repository/destination/store writes, stale checks and pre-action recovery journals. No backend
fallback, hook bypass or automatic cleanup is supported. See [task workspaces](workspaces.md)
for command effects, capability refusals and bounds. Spec 010 acceptance is independent of
historical read-only inspection/inventory evidence.

## Local reference tidy planning

`stw tidy plan` supports bounded native Git branch and jj bookmark ancestry evidence against an
explicit base. Checked-out/workspace-used targets and unsupported integration proof retain work.
jj remote-associated or conflicted targets remain retained; bookmark cleanup never implies change
abandonment. Optional private plan persistence is distinct from repository effects. Explicitly authorized local reference cleanup records durable native recovery and checks stale state;
Git uses old-OID CAS, while jj checks pinned operations and exact allowed deltas. Owned task retirement requires its separate v2 plan, ownership and complete disk proof. See [tidy](tidy.md); its acceptance evidence is
independent of prior inspection, inventory and workspace creation.


## Owned workspace retirement capability

Native Git supports its coupled worktree registration/files removal only when both actions are
explicitly selected and clean owned state is proven; registration-only is retained as unsupported.
Native jj forgetting preserves files, and separately selected filesystem removal uses complete
manifest/identity revalidation and confined entry removal. Recovery pins retain native identities;
no jj abandonment, remote deletion, broad prune or garbage collection is implied. Full jj stored/disk
equality, including one-integrated-parent empty-WC proof where applicable, establishes freshness;
stored-unsnapshotted inspection alone remains insufficient. See [tidy](tidy.md) for limits.
