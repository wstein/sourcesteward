# SourceSteward roadmap

SourceSteward is an early version-control toolkit. Help/version and bounded native Git/jj identity inspection are implemented.
No model, GitButler installation or cloud account is required to run those commands.

## Foundation — current milestone
Go/Cobra command parsing, text/JSON version information, Spec Kit workflow, automated
source checks, original family branding, versioned JSON schemas and Hugo-generated GitHub
Pages guides/command reference. Git and jj are Tier 1 peers by policy; native jj final cross-platform
acceptance is tracked in Spec 008.

## Inspect — identity implemented, broader status deferred
Current `stw inspect` reports repository/worktree identity, HEAD and recognized backend evidence.
Broader changes, conflicts, branches/bookmarks, upstreams, linked worktrees/workspaces and
interrupted operations remain deferred. Existing inspection has explicit targeting, predictable
exit codes and bounded output without inspected-target writes or implicit fetches. Native jj
creates and removes private temporary isolation/probe files outside that target.

## Spec 008 — native jj identity inspection, implemented candidate
Native inspection supports standalone, colocated and secondary jj workspaces with separate
workspace, commit, change and operation identities. Schema v2 preserves published Git/version
v1 contracts. Usable read-only Linux/macOS sandboxing is mandatory; stored-unsnapshotted
results disclose unknown disk freshness. Minimum/current conformance and independent effects
review are required; see the [acceptance record](../specs/008-jj-inspection/verification.md)
for completed macOS/Linux evidence and final handoff gates.

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
native jj inspection is bounded by Spec 008 and capability tests. Later lifecycle operations must respect ownership and dirty
state. Detect GitButler capabilities and managed workspaces; use its supported interfaces for
writes and fail unsupported operations. Parallel GitButler branches share a filesystem and
runtime; separate worktrees remain the option for isolated checkout/runtime state.

## Deferred
History editing, remote deletion, publishing automation, a graphical client, agent scheduling
and MCP integration are outside the foundation. A future MCP interface will reuse the same
command core and contracts; it will not grant additional permissions.
