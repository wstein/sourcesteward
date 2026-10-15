# Progress and handoff

## Active work: Spec 009 inventory

The implementation candidate adds bounded native Git/jj inventory and three-valued
queries. Plan/tasks/pre-implementation analysis preceded test-first implementation
in separate Git, jj, CLI/schema and conformance worktrees. M1–M4 organize 36 mandatory
native CLI cases; they are acceptance gates within this implementation work item.
See [verification](../specs/009-workspace-inventory/verification.md) for exact source,
review, local results and remaining hosted/owner gates. Later task/workspace mutations,
tidy, restacking, change editing and forge writes remain planned.

User merged specification PRs #12–#26. Primary main and active implementation worktrees
were updated onto origin/main 57497b1; completed historical worktrees also run current
main, with original branches/backups retained. New implementation PRs form a two-part
stack: analyzed design first, code/tests/evidence second. No owner merge, release or
live schema publication is performed by this handoff. Historical records follow.

## Previous milestone
008-jj-inspection implements native standalone, colocated and secondary workspace identity
with enforced read-only Linux/macOS sandboxing, isolated configuration and schema v2.
Published Git/version v1 contracts remain unchanged. Independently reviewed native source
949c082 passes both macOS jj versions and mandatory minimum/current Linux conformance at
beb3285 (Actions run38777527598), including capability/confinement/network-namespace proof.
The user merged policy PR #9 and implementation PR #10; main cf46592 has the same tree as
accepted beb3285. Final documentation analysis cleared95bb36d; a documentation-only follow-up
PR and CI handoff remain pending. See [Spec 008 evidence](../specs/008-jj-inspection/verification.md).

007-hugo-docs are merged to main through
[PR #7](https://github.com/wstein/sourcesteward/pull/7),
[PR #6](https://github.com/wstein/sourcesteward/pull/6) and
[PR #8](https://github.com/wstein/sourcesteward/pull/8). Their recorded acceptance covers
bounded ordinary-Git identity inspection, versioned JSON contracts and pinned Hugo guides/
command reference. This is merged-source status; deployment evidence remains in each feature's
verification record. Native jj inspection is implemented and merged through Spec 008;
its documentation follow-up handoff remains pending.

The Tier 1 policy amendment was merged by the user through PR #9: Git and jj receive equal support
commitments for explicitly supported capabilities. See [amendment evidence](tier1-policy.md).

004-site-themes is merged to main through [PR #5](https://github.com/wstein/sourcesteward/pull/5).
The README site link and system/light/dark Pages selector passed source, browser and
independent review checks. This records merged source; real OS dark preference/change
remains untested. See [theme evidence](../specs/004-site-themes/verification.md).

003-agent-hints is merged to main through [PR #4](https://github.com/wstein/sourcesteward/pull/4).
Root help links to task-specific agent guidance and llms.txt; version/JSON and error
contracts are preserved. Independent review cleared the implementation. See
[agent hints acceptance](agent-hints-verification.md).

002-wide-tie-brand is complete and published.
The wide tie asset set, navy/orange site, useful README badges and local make build target
pass source/browser checks and independent review. CI/Pages run 38751223604 passed;
live revision 63e5f9b matched the checked HTML/CSS, logo, favicons and social preview. See [branding evidence](../brand/verification.md).

001-project-foundation is complete. The public SourceSteward repository starts with an empty
commit and focused Conventional Commits for specifications, CLI, community, branding, site,
harness and CI. A durable local checkout is available. See [verification](verification.md).

## Decisions

- Repository/module: wstein/sourcesteward; product: SourceSteward; standalone short CLI: stw.
- Go 1.27.2 and Cobra v1.10.2; EUPL-1.2 original source/artwork; font licenses retained.
- Git and jj are Tier 1 peers with bounded native identity inspection implemented. Final
  Spec 008 implementation is merged; documentation follow-up is pending; GitButler and broader mutations require separate specifications.
- Wide navy tie, orange knot and white branch; local family fonts with preserved OFL notices.
- Static Pages with local assets and same-run exact-SHA checks; no CLI release automation.
- Weekly Dependabot, community standards, private security reporting and owner review.
- Spec Kit workflow and small repository-local harness; no agent scheduler or auto-merge.

## Measured state

make check, race tests and six real-process smoke cases passed. Independent source review
cleared fd4d652ce402621bf07b4d83ee5a5da7b70f4fd3 after two regression fixes. Browser inspection
passed at 1280×800 and 375×812 with no horizontal overflow. Initial Linux CI and Pages run
38743805161 succeeded; live revision and static bytes matched the checked source. The final
documentation-only evidence commit will go through the same CI/Pages gate.

## Historical next action — superseded
Open the documentation-only acceptance follow-up PR and confirm its exact-revision CI.
Policy PR #9 and implementation PR #10 were merged by the user; native source/effects and
final artifact analysis are independently cleared. No live schema publication or Pages
verification is claimed here. Broader status, mutation, recovery, repository migration and
GitButler integration require separate specifications.

## Historical LazyJJ-inspired roadmap proposal

The [roadmap](roadmap.md#proposed-workflow-features-inspired-by-lazyjj) now prioritizes
workspace/reference inventory, isolated task workspaces, checkpoints, stack inspection and
evidence-based tidy for native Git/jj. Restacking and forge coordination remain later
specifications. Ratings are planning recommendations; none of these capabilities is
implemented or acceptance-verified. Next feature action: specify a bounded read-only
workspace/reference inventory, then analyze its plan/tasks before implementation.

This documentation-only update was checked with `python3 scripts/check-docs.py` and
`git diff --check`; it does not establish runtime or independent-review evidence.

## Historical workflow specification batch

Specs 009–016 now capture all eight user-selected workflows with prioritized stories,
acceptance scenarios, requirements, measurable outcomes and author quality checklists.
The feature index marks each as planned; no implementation evidence is claimed.
Spec 010 owns the shared mutation contract; Spec 011 owns retirement execution, avoiding
a circular dependency for creation. Spec 009 is the active local Specify feature.
Next: review/plan Spec 009, generate tasks, then perform cross-artifact analysis before
implementation. No plans or tasks have been generated in this specification-only batch.

Specification-batch source checks: `make check` passed with the existing pinned Hugo
provided through `HUGO=/private/tmp/sourcesteward-hugo-docs/.tools/hugo` and cached Go
dependencies. The initial run stopped at the packaging test because this new worktree
had no `.tools/hugo`; rerunning with the existing executable passed all checks.
All eight drafts passed author structure/quality review. This is not independent
review, backend feature acceptance or cross-artifact spec/plan/task analysis.

### 009-workspace-inventory — Toolbelt use-case specification update

Workspace, State and Reference Inventory: draft scenarios, requirements and author quality checks updated.
Documentation links and whitespace checks passed before PR handoff. No runtime capability
or independent acceptance is claimed. Planning/tasks/analysis remain pending.

### 012-stack-inspection — Toolbelt use-case specification update

Stack Inspection and Diffs: draft scenarios, requirements and author quality checks updated.
Documentation links and whitespace checks passed before PR handoff. No runtime capability
or independent acceptance is claimed. Planning/tasks/analysis remain pending.

### 013-checkpoint-resume — Toolbelt use-case specification update

Checkpoint, Resume and Selective Undo: draft scenarios, requirements and author quality checks updated.
Documentation links and whitespace checks passed before PR handoff. No runtime capability
or independent acceptance is claimed. Planning/tasks/analysis remain pending.

### 014-conflict-diagnosis — Toolbelt use-case specification update

Conflict Diagnosis and Integration Preflight: draft scenarios, requirements and author quality checks updated.
Documentation links and whitespace checks passed before PR handoff. No runtime capability
or independent acceptance is claimed. Planning/tasks/analysis remain pending.

### 017-work-transfer — Toolbelt use-case specification update

Selected Work Transfer: draft scenarios, requirements and author quality checks updated.
Documentation links and whitespace checks passed before PR handoff. No runtime capability
or independent acceptance is claimed. Planning/tasks/analysis remain pending.

### 018-change-editing — Toolbelt use-case specification update

Amend, Fix Up and Split Changes: draft scenarios, requirements and author quality checks updated.
Documentation links and whitespace checks passed before PR handoff. No runtime capability
or independent acceptance is claimed. Planning/tasks/analysis remain pending.

### 019-upstream-sync — Toolbelt use-case specification update

Explicit Upstream Synchronization: draft scenarios, requirements and author quality checks updated.
Documentation links and whitespace checks passed before PR handoff. No runtime capability
or independent acceptance is claimed. Planning/tasks/analysis remain pending.

### Toolbelt specification batch verification

Integrated `make check` passed at `739abf5dced84f14987aa9d9b85fb16aa91fc269` with cached Go dependencies and the existing
pinned Hugo executable selected via `HUGO=/private/tmp/sourcesteward-hugo-docs/.tools/hugo`.
This covered Go tests/vet/module integrity, 32 Python tests, 12 theme cases, 112 authored
document checks, architecture boundaries and whitespace. All seven affected/new drafts
passed author structure, consecutive requirement numbering and checklist review; all
remain planned. This is source/documentation evidence, not new backend acceptance,
independent review or cross-artifact analysis.

Follow-up draft PRs #20–#26 target their predecessors, beginning on original draft PR #19.
Original PRs #12–#19 were preserved. Next bounded step remains planning Spec 009 and
generating tasks before cross-artifact analysis; no implementation was performed.

## Current next action

Review the Spec 009 two-PR stack (analyzed design, then implementation and evidence)
and confirm hosted Linux minimum/current checks. Spec 009 is implemented with explicit
native qualifications; the earlier proposal/planning entries are historical and do
not override this action. Owner approval/merge and live schema publication are pending.
Do not start task workspace mutations or tidy from inventory evidence alone.
