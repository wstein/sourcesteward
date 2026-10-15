# SourceSteward roadmap

SourceSteward is an early scaffold. Only help and version reporting work today.
No model, GitButler installation or cloud account is required to run those commands.

## Foundation — current milestone
Go/Cobra command parsing, text/JSON version information, Spec Kit workflow, automated
source checks, original family branding and a GitHub Pages overview.

## Inspect — next specification
A shared text/JSON view of repository identity, changes, conflicts, branches, upstreams,
linked worktrees and interrupted operations. Explicit repository targeting, predictable
exit codes and bounded output. No writes or implicit fetches.

## Diagnose
Explain problems and propose repair options. Repository text is data, never instructions.
Report unsupported states and uncertainty instead of inventing a successful diagnosis.

## Plan local cleanup
Retain checked-out/worktree-owned and uncertain branches. Record exact tip/base identities
and evidence: ancestry, squash equivalence, or verified PR integration into the selected
base. A missing upstream alone is never merge proof. API failures retain affected branches.

## Apply and recover
Apply only explicit, validated plans; refuse stale refs or changed relevant state. Preserve
recovery refs and report per-action outcomes, including partial failures. Define worktree,
configuration and reflog effects before implementation. Plans do not supply authorization.

## Worktrees, jj and GitButler
Start with worktree inspection. The [backend contract](backends.md) makes jj readiness explicit;
jj adapter implementation follows its own specification and capability tests. Later lifecycle operations must respect ownership and dirty
state. Detect GitButler capabilities and managed workspaces; use its supported interfaces for
writes and fail unsupported operations. Parallel GitButler branches share a filesystem and
runtime; separate worktrees remain the option for isolated checkout/runtime state.

## Deferred
History editing, remote deletion, publishing automation, a graphical client, agent scheduling
and MCP integration are outside the foundation. A future MCP interface will reuse the same
command core and contracts; it will not grant additional permissions.
