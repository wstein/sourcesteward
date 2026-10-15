# SourceSteward roadmap

SourceSteward is an early version-control toolkit. Help/version and bounded ordinary-Git identity inspection work today.
No model, GitButler installation or cloud account is required to run those commands.

## Foundation — current milestone
Go/Cobra command parsing, text/JSON version information, Spec Kit workflow, automated
source checks, original family branding, versioned JSON schemas and Hugo-generated GitHub
Pages guides/command reference. Git and jj are Tier 1 peers by policy; native jj capability
acceptance remains pending.

## Inspect — identity implemented, broader status deferred
Current `stw inspect` reports repository/worktree identity, HEAD and recognized backend evidence.
Broader changes, conflicts, branches/bookmarks, upstreams, linked worktrees/workspaces and
interrupted operations remain deferred. Existing inspection has explicit targeting, predictable
exit codes and bounded output without writes or implicit fetches.

## Spec 008 — native jj identity inspection, next
Support native inspection of standalone and colocated jj workspaces with separate workspace,
commit, change and operation identities. Specify minimum/current supported versions, freshness
limits and compatible schemas. Conformance CI must cover both Git and jj and prove bounded,
noninteractive execution without working-copy or operation-log writes. Do not infer jj
capability from detection alone.

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
The [backend contract](backends.md) makes Git/jj Tier 1 support commitments explicit;
native jj inspection follows Spec 008 and capability tests. Later lifecycle operations must respect ownership and dirty
state. Detect GitButler capabilities and managed workspaces; use its supported interfaces for
writes and fail unsupported operations. Parallel GitButler branches share a filesystem and
runtime; separate worktrees remain the option for isolated checkout/runtime state.

## Deferred
History editing, remote deletion, publishing automation, a graphical client, agent scheduling
and MCP integration are outside the foundation. A future MCP interface will reuse the same
command core and contracts; it will not grant additional permissions.
