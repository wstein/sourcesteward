# SourceSteward roadmap

SourceSteward is an early version-control toolkit. Help/version, bounded native Git/jj identity inspection, qualified inventory, isolated task workspace creation and bounded local reference/workspace tidy are implemented.
No model, GitButler installation or cloud account is required to run those commands.

## Implemented command families

The [command catalog](commands.md) contains worked examples, effects and prerequisites.
Current commands are `help`, `version`, `inspect`, `inventory`, `workspace plan/apply`,
`workspace handoff/retire-plan`, `tidy` (the wizard and `plan/apply/restore/history`), `sync`, `status`, `watch`,
`pulls`, `issues`, `hosting repo/pr/ci` and the optional jj-only `gh jj repo/pr/ci` extension. `repos` is merged too but its feature entry stays `planned` until an independent re-review of its fixes. PR/CI reads require authenticated GitHub access;
local inspection and workspace/tidy workflows do not require a cloud account.

## Planned features — no current command implementation

Specs 012–019 cover stack inspection/plan, checkpoint/resume, conflict diagnosis and
integration preflight, restacking, stacked PR coordination/writes, work transfer,
change editing and upstream synchronization. Spec 043 plans `stw land`, the owner's fast-forward landing
of a reviewed pull request and, with `--stack`, of a whole linear chain by one push of its tip
(first feature after v0.2.0; dependent parts are by default one pull request with a commit series). Native GitButler integration is also
planned. Current inventory, bounded recovery evidence and PR/CI reads do not implement
those broader features. The following design directions retain those distinctions.

## Implemented foundation
Go/Cobra command parsing, text/JSON version information, Spec Kit workflow, automated
source checks, original family branding, versioned JSON schemas and Hugo-generated GitHub
Pages guides/command reference. Git and jj are Tier 1 peers by policy; native jj final cross-platform
acceptance is tracked in Spec 008.

## Inspect and inventory — native evidence with explicit limits
Current `stw inspect` reports repository/worktree identity, HEAD and recognized backend evidence.
`stw inventory` adds registered workspaces/worktrees, native references, explicit bases,
three-valued predicates and selected file-state scopes with native limitations. Reviewed local reference cleanup and owned retirement are implemented; checkpoint recovery remains planned; see the [inventory guide](inventory.md). Existing inspection has explicit targeting, predictable
exit codes and bounded output without inspected-target writes or implicit fetches. Native jj
creates and removes private temporary isolation/probe files outside that target.

## Spec 008 — native jj identity inspection, implemented
Native inspection supports standalone, colocated and secondary jj workspaces with separate
workspace, commit, change and operation identities. Schema v2 preserves published Git/version
v1 contracts. Usable read-only Linux/macOS sandboxing is mandatory; stored-unsnapshotted
results disclose unknown disk freshness. Minimum/current conformance and independent effects
review are required; see the [acceptance record](../specs/008-jj-inspection/verification.md)
for completed macOS/Linux evidence and final handoff gates.

## Diagnose — planned
Explain problems and propose repair options. Repository text is data, never instructions.
Report unsupported states and uncertainty instead of inventing a successful diagnosis.

## Plan, apply and retain recovery
`stw tidy plan` uses explicit native integration bases and ancestry proof; uncertain, workspace-used
or remote-bound jj references retain. Squash/forge proof is unsupported, never inferred.
`stw tidy apply` requires explicit authorization and freshly declared effects, revalidates state,
and durably records native recovery before changes. Owned retirement has distinct registration
and exact-path file effects, full fresh disk proof and versioned v2 results. Git registration-only
removal of a present worktree retains unsupported; jj forget preserves files. Read the
[tidy guide](tidy.md) and [Spec 011 evidence](../specs/011-workspace-reference-tidy/verification.md).
`stw tidy` without arguments is the workspace-wide wizard of [Spec 041](../specs/041-tidy-wizard/spec.md): read-only overview of all repositories and owners, a stored plan set and `stw tidy apply PLAN-ID`; it is implemented with an unpublished schema and its plan-only dogfood dry run (T021) is recorded in the [evidence](../specs/041-tidy-wizard/evidence/T021.md). [Spec 044](../specs/044-tidy-janitor-wizard/spec.md) turns the human path into a janitor wizard (risk levels, online evidence by default, a grouped overview and one step per group); phase 1 is in progress; the feature stays `planned` until it ships.
External races/ABA remain sampled; recovery is bounded manual evidence, not automatic restoration.

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

The table records product direction; inventory, bounded workspace creation and tidy retirement are implemented with scoped acceptance evidence. Checkpoints, stack editing and forge coordination remain planned. Ratings express
product priority and fit, not measured quality. Command names below are illustrative.

| Proposed capability | Priority | SourceSteward adaptation |
| --- | --- | --- |
| Workspace/worktree inventory | 10/10 | List ownership, path, dirty/unknown state, stale metadata and relevant identities; preserve backend-specific facts. |
| Isolated task workspace lifecycle | 10/10 | Create a Git worktree or native jj workspace, checkpoint work and prepare handoff; explicit ownership and recovery for retirement. |
| Stack inspection and plan | 9/10 | Show layers, reference and pull request relationships and GitHub native stack membership in text/JSON, detect drift after a parent lands, print the exact repair as advisory text; report unknown stack boundaries instead of guessing. |
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

## Specification index

- [009-workspace-inventory: Workspace, State and Reference Inventory](../specs/009-workspace-inventory/spec.md) — 10/10.
- [010-isolated-task-workspaces: Isolated Task Workspaces](../specs/010-isolated-task-workspaces/spec.md) — 10/10.
- [011-workspace-reference-tidy: Workspace and Reference Tidy](../specs/011-workspace-reference-tidy/spec.md) — 10/10.
- [012-stack-inspection: Stack Inspection and Plan](../specs/012-stack-inspection/spec.md) — 9/10.
- [013-checkpoint-resume: Checkpoint, Resume and Selective Undo](../specs/013-checkpoint-resume/spec.md) — 9/10.
- [014-conflict-diagnosis: Conflict Diagnosis and Integration Preflight](../specs/014-conflict-diagnosis/spec.md) — 9/10.
- [015-local-restacking: Local Restacking](../specs/015-local-restacking/spec.md) — 8/10.
- [016-stacked-pr-coordination: Stacked PR Coordination](../specs/016-stacked-pr-coordination/spec.md) — 8/10.

Specs 009 and 010 are implemented and merged; see their
[009 verification](../specs/009-workspace-inventory/verification.md) and
[010 verification](../specs/010-isolated-task-workspaces/verification.md).
Spec 011 implements WI1 evidence-only plans, WI2 authorized local reference cleanup and
WI3 owned workspace retirement; [verification](../specs/011-workspace-reference-tidy/verification.md) records each separate handoff. Each receives planning,
tasks, independent analysis, implementation, review and its own PR. Registration
removal, filesystem removal and reference cleanup have distinct effects; jj
abandonment is outside this delivery. Remaining features are planned, not implemented.

- [017-work-transfer: Selected Work Transfer](../specs/017-work-transfer/spec.md) — 9/10; draft, implementation pending.

- [018-change-editing: Amend, Fix Up and Split Changes](../specs/018-change-editing/spec.md) — 9/10; draft, implementation pending.

- [019-upstream-sync: Explicit Upstream Synchronization](../specs/019-upstream-sync/spec.md) — 7/10; draft, implementation pending.
