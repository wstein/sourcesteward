# Backend readiness: Git, Jujutsu and GitButler

Git and Jujutsu (jj) are Tier 1 peers under constitution 1.1.0. `stw inspect` implements bounded ordinary-Git identity inspection (Git 2.46+).
It detects recognized jj/GitButler evidence and refuses unsupported managed backends.
Native jj inspection is the next bounded feature, Spec 008; it is not implemented yet.
GitButler remains a separately specified future integration. Tier 1 commits to native support,
versioned contracts and conformance evidence, not identical capabilities.
The CLI/application boundary must not assume every workspace is ordinary Git.

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
Each operation declares filesystem/repository writes, snapshotting, index refresh, network,
hooks, editor/prompt requirements and recovery scope. Inspection defaults to no writes where
supported; otherwise disclose the effect and require an explicit effectful invocation.
A jj read-only contract must test both working-copy and operation-log effects.
`--ignore-working-copy` avoids snapshotting but may report stale state; it does not alone
prove absence of operation-log changes. Spec 008 must select and verify a supported native
read mode, including divergent-operation behavior, and disclose freshness limits.

Bind plans to backend, repository/workspace identity, relevant refs, changes and operation
identity where supported. Revalidate before applying; reject stale plans. Recovery is backend-
and operation-specific. Locks inside Git/jj do not isolate agents editing the same files.
Use separate workspaces/worktrees when tasks need separate filesystem or runtime state.

## Tier 1 support and delivery

| Backend | Current accepted capability | Version support | Next acceptance gate |
| --- | --- | --- | --- |
| Git | Bounded identity inspection | Git 2.46+ | Preserve existing fixtures and v1 schema |
| jj | Ownership detection and explicit unsupported result | Native versions pending Spec 008 | Native identity inspection, version policy and no-write evidence |
| GitButler | Recognized ownership detection and explicit unsupported result | No native adapter supported | Separate specification |

Each Tier 1 adapter must declare supported minimum/current versions and capabilities, and
exercise both in conformance CI. Supported capability regressions block release equally for
Git and jj. Tests must cover bounded output, noninteractive execution, missing/unsupported
executables, stale/concurrent state and absence of unintended writes. Capability limits and
backend-specific identities stay visible in human output, JSON schemas and documentation.
Published v1 contracts remain compatible; incompatible result shapes require a new version.

Implement native jj identity inspection through Spec 008, then expand status/diagnosis for
both backends through reviewed specifications. Mutations require separate per-backend plans,
state validation and recovery evidence. Do not implement one backend's missing behavior by
silently performing another backend's writes.

## Primary references
- https://docs.jj-vcs.dev/latest/git-compatibility/
- https://github.com/jj-vcs/jj/blob/main/docs/working-copy.md
- https://github.com/jj-vcs/jj/blob/main/docs/operation-log.md
- https://docs.gitbutler.com/cli-guides/cli-tutorial/scripting
- https://docs.gitbutler.com/ai-agents/parallel-agents
