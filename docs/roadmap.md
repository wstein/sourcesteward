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

## Proposed workflow features inspired by LazyJJ

[LazyJJ-dev/LazyJJ](https://github.com/LazyJJ-dev/LazyJJ) is a jj configuration layer
for stacks, isolated Claude workspaces, checkpoints and GitHub PR helpers. This is
separate from Cretezy/lazyjj, the terminal UI. Adopt useful workflows through native
SourceSteward adapters; installing either project is not a prerequisite.

These are proposals, not implemented commands or approved specifications. Ratings express
product priority and fit, not measured quality. Command names below are illustrative.

| Proposed capability | Priority | SourceSteward adaptation |
| --- | --- | --- |
| Workspace/worktree inventory | 10/10 | List ownership, path, dirty/unknown state, stale metadata and relevant identities; preserve backend-specific facts. |
| Isolated task workspace lifecycle | 10/10 | Create a Git worktree or native jj workspace, checkpoint work and prepare handoff; explicit ownership and recovery for retirement. |
| Stack inspection and diff summary | 9/10 | Show revisions, changed files, conflicts and reference relationships in text/JSON; report unknown stack boundaries instead of guessing. |
| Safe workspace and reference tidy | 10/10 | Plan/apply cleanup using integration and ownership evidence; missing upstreams and empty commits alone never prove disposable work. |
| Checkpoint and resume evidence | 9/10 | Record backend identities and task context; disclose what is recoverable. jj operation IDs and Git recovery refs have different guarantees. |
| Conflict diagnosis and handoff | 9/10 | Return affected files/revisions and bounded evidence for any agent or human; preserve unresolved state and avoid automatic model execution. |
| Local stack restacking | 8/10 | Later history-editing specification with per-backend effects, stale-plan refusal, interruption handling and recovery evidence. |
| Stacked PR coordination | 8/10 | Later forge adapter: inspect dependencies and preview PR/base updates; pushing, PR creation and comments are separate explicit network effects. |
| Optional terminal/session integration | 5/10 | Emit useful workspace paths/context first; tmux and agent launchers stay optional and outside the command core. |

### Delivery sequence

1. Specify bounded read-only workspace/reference inventory for both Tier 1 backends.
   Include stack facts only where a declared base and relationships provide sufficient evidence.
   Publish compatible schemas and minimum/current conformance fixtures. Do not promise live
   jj disk status from stored-unsnapshotted identity inspection.
2. Specify the shared mutation contract: inspectable plans, explicit effects, expected-state
   validation, ownership/concurrency checks, durable outcomes and operation-specific recovery.
3. Specify isolated task workspace creation, checkpoint and handoff using that contract.
   Git and jj receive native implementations and independent conformance evidence; partial
   capability gaps remain visible. Repository setup scripts and hooks require explicit effect
   contracts and authorization; never execute them implicitly as workspace setup.
4. Specify workspace retirement and branch/bookmark tidy. Retain dirty, untracked, active,
   foreign-owned or uncertain work. Workspace metadata removal and filesystem removal are
   separate actions; never infer recursive deletion permission from forgetting a workspace.
5. Specify local restacking, then forge/stack submission separately. Local workflows remain
   useful without a cloud account, model, tmux or a forge integration.

Each feature follows specify → plan → tasks → analyze → implement → verify/review. Use
backend-neutral user outcomes with explicit native Git/jj semantics rather than translating
jj changes into synthetic Git branches. GitButler-managed writes remain deferred until a
supported native interface is separately specified. No proposal changes existing CLI/schema
contracts or authorizes history rewriting, external messages, publication or deletion.

## Deferred
History editing, remote deletion, publishing automation, a graphical client, agent scheduling
and MCP integration are outside the foundation. A future MCP interface will reuse the same
command core and contracts; it will not grant additional permissions.

## Draft specification index

- [009-workspace-inventory: Workspace and Worktree Inventory](../specs/009-workspace-inventory/spec.md) — 10/10.
- [010-isolated-task-workspaces: Isolated Task Workspaces](../specs/010-isolated-task-workspaces/spec.md) — 10/10.
- [011-workspace-reference-tidy: Workspace and Reference Tidy](../specs/011-workspace-reference-tidy/spec.md) — 10/10.
- [012-stack-inspection: Stack Inspection and Diffs](../specs/012-stack-inspection/spec.md) — 9/10.
- [013-checkpoint-resume: Checkpoint and Resume](../specs/013-checkpoint-resume/spec.md) — 9/10.
- [014-conflict-diagnosis: Conflict Diagnosis](../specs/014-conflict-diagnosis/spec.md) — 9/10.
- [015-local-restacking: Local Restacking](../specs/015-local-restacking/spec.md) — 8/10.
- [016-stacked-pr-coordination: Stacked PR Coordination](../specs/016-stacked-pr-coordination/spec.md) — 8/10.

All eight drafts require planning, tasks and cross-artifact analysis before implementation.
The active local Specify pointer selects Spec 009 for the next bounded planning step.
