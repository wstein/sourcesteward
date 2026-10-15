# Backend readiness: Git, Jujutsu and GitButler

SourceSteward is jj-ready by design. `stw inspect` implements bounded ordinary-Git identity inspection (Git 2.46+).
It detects recognized jj/GitButler evidence and refuses unsupported managed backends.
Native jj and GitButler adapters are optional future integrations.
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
2. Use explicit backend selection when detection is ambiguous; do not initialize or migrate.
3. A colocated jj workspace remains jj-managed. GitButler-managed state remains GitButler-owned.
4. Advertise only commands supported by the detected backend/version and output contract.
5. Never silently fall back to raw Git writes. Read-only Git plumbing is allowed only when
   that adapter has specified and tested its effects and compatibility.
6. If both jj and GitButler metadata appear, report ambiguity and retain state; do not guess.

## Effects and plans
Each operation declares filesystem/repository writes, snapshotting, index refresh, network,
hooks, editor/prompt requirements and recovery scope. Inspection defaults to no writes where
supported; otherwise disclose the effect and require an explicit effectful invocation.
Evaluate jj's documented --ignore-working-copy behavior when designing its inspection adapter;
do not assume that it provides a fully current snapshot without tradeoffs.

Bind plans to backend, repository/workspace identity, relevant refs, changes and operation
identity where supported. Revalidate before applying; reject stale plans. Recovery is backend-
and operation-specific. Locks inside Git/jj do not isolate agents editing the same files.
Use separate workspaces/worktrees when tasks need separate filesystem or runtime state.

## Delivery order
1. Specify ordinary Git inspection and backend detection.
2. Run the same inspection contract fixtures against a capability-gated jj adapter.
3. Define GitButler inspection through supported JSON interfaces; coverage varies by command.
4. Specify mutations separately per backend, with independent evidence and recovery tests.

## Primary references
- https://docs.jj-vcs.dev/latest/git-compatibility/
- https://github.com/jj-vcs/jj/blob/main/docs/working-copy.md
- https://github.com/jj-vcs/jj/blob/main/docs/operation-log.md
- https://docs.gitbutler.com/cli-guides/cli-tutorial/scripting
- https://docs.gitbutler.com/ai-agents/parallel-agents
