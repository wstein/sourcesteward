# Progress and handoff

## Active work: Spec 029 tidy safety (source candidate, acceptance pending)

Explicit `tidy --contract 3` adds native Git reference proof, local tracking cleanup and
owned or explicitly completed legacy retirement. Default v1/v2 stays offline. Exact reviewed
dependencies, fresh-main reconciliations, safe disposable CLI conformance and remaining
gates are recorded in [integration evidence](../specs/029-tidy-dogfood-safety/verification-integration.md).
The feature remains `planned`: final integrated source review/checks, unavailable pinned
versions/platform lanes, separately approved candidate build/install and fresh real-repository
acceptance are distinct gates. No schema publication, activation or live cleanup is implied.

Tasks T001-T042 are ticked in [tasks](../specs/029-tidy-dogfood-safety/tasks.md) as implemented,
pending independent review; they close fully only after T046. Still open: T044 (pinned Git
2.46.0/2.56.0 and jj 0.45.1/0.46.0 on Linux and macOS lanes), T046 (independent review of the
exact integrated SHA, which differs from the reviewed fold tips), T047 (candidate build and
install, which needs separate owner approval) and T048/T049 (fresh real-repository acceptance).
Windows and Darwin remain unverified here.

## Active work: Spec 015 local restacking, first slice (source implemented, verification pending)

The read-only `stw restack plan` for native Git is implemented in source on `feat/015-restack-plan` (hidden command, unpublished
`schemas/v1/restack-plan.schema.json`): explicit branch, old parent and new parent, a plumbing replay in a private object
quarantine, `range-diff` pairing, `cherry` classification of the parent's commits, own-diff and identity evidence, predicted
conflicts, the overlap risk indicator, effects, and the inert lease push and resync texts, with a closed outcome table that never
reads as safe. See [the specification](../specs/015-local-restacking/spec.md), [tasks](../specs/015-local-restacking/tasks.md)
(20 of 24 done) and [verification](../specs/015-local-restacking/verification.md). Open:
the minimum/current Git lanes (T021), the independent review of the exact head (T023) and the reserved limitation (T024); T004 was not done by design. Status
stays `planned`; `restack apply`, jj and the push are later slices.

## Spec 035 stw watch (accepted, visible, 2026-10-22)

[The specification](../specs/035-stw-watch/spec.md), its [event contract](../specs/035-stw-watch/contracts/watch-events.md) and
[plan](../specs/035-stw-watch/plan.md) describe a read-only `stw watch` that prints pull request transitions (checks, merges,
`restack-needed` after a rebase merge) as JSON Lines, replacing the foreman's `watch2.sh`. Work packages A to E are implemented in
source and the command is listed in `stw --help`; `schemas/v1/watch-event.schema.json` is frozen and unpublished, and `stw watch` has a guide
([watch](watch.md)) and an end to end acceptance suite (US1 to US10, fake forge, no network). The owner accepted it on 2026-10-22 after a dogfood run on the
workspace (discovery of 7 repositories, silent baseline, `--status` fresh). The removal of `scripts/ci-wait.sh` is still open.

## Spec 036 stw pulls (implemented in source, review and acceptance pending, 2026-10-22)

[The specification](../specs/036-stw-pulls/spec.md), its [JSON contract](../specs/036-stw-pulls/contracts/pulls.md) and
[plan](../specs/036-stw-pulls/plan.md) describe a read-only, stateless `stw pulls` snapshot of all open pull requests of the workspace
repositories (bot PRs included, grouped by what needs action, with a `ready to merge` verdict) and an opt-in `--owner` mode that
discovers other repositories through one fixed `search/issues` read. Work packages 0 to C are implemented in source and the command is
listed in `stw --help`; `schemas/v1/pulls.schema.json` is frozen and unpublished, and `stw pulls` has a guide ([pulls](pulls.md)). The tests
use a fake forge only (no network, no build); independent review of the exact head and the owner's acceptance are pending.

## Spec 037 stw status online (work package D, action model implemented in source, 2026-10-22)

[The specification](../specs/037-stw-status-overview/spec.md), its [JSON contract](../specs/037-stw-status-overview/contracts/status.md) and
[plan](../specs/037-stw-status-overview/plan.md) make `stw status` online and action-oriented. Work package D is the pure package
`internal/overview` (no I/O, no network, no clock): the 25 category rules, priorities, stable ids, deterministic ordering, human grouping with
the cap of 10 lines per priority, suggested commands built only from validated tokens with their `effect`, notices and summary counts. Its
tests are table-driven with one fixture per table row and never touch a network. Choices the spec leaves open: local findings use the
repository's local name in the id and every forge-derived finding (including `main-behind-origin`) its `OWNER/NAME`; tags sort by natural
version order (`v1.9.0` before `v1.10.0`) instead of a compiled release scheme; worktree items exist only when the status listed the
linked worktrees (`--worktrees`); a grouped line shows the command its items share or else the drill-down `stw pulls --repo OWNER/NAME`
(`gh release list -R OWNER/NAME` for drafts), and the omitted lines of a capped priority carry the read-only drill-down of the first omitted line's category and repository (or no command).
Text validators and the escape live in the standard-library-only `internal/untrusted` (moved from `watchcmd` and `cli`), so `internal/overview` is on the network-free list of the architecture check; its ids use the command validators (a component that fails them becomes `h-` and 12 hex digits of its SHA-256) and `stwsync.AttentionReasons()` is the single list of Spec 033 reasons that a test maps to rows.
The CLI, hosting, schema and the other work packages are not part of this package; the issue categories of Spec 038 are not either. For work package F (no schema file is touched here): the contract's `subject` object needs a key for the id part that is neither a number nor a tag (`Subject.Name`: fact kind, branch, path, source id), and `evidence` needs a key table per category: pull requests `group`, `reason`, `past_budget`; `release-draft` `drafts_visibility`, `assets`; `release-run-failed` `conclusion`, `released`; `main-red` `class`; `read-error` `source`, `code`; `repo-error` `code`; `local-changes` `staged`, `unstaged`; `local-operation` `operation` or `conflicts`; `local-behind` `ahead`, `behind`; `local-untracked`, `local-stash`, `branches-unmerged` one count each (`untracked`, `stash`, `not_integrated`); `gitlink-drift` and `gitlink-ahead` `relation`; the other categories have none. WP-A maps the verdict strings of `internal/pulls` to the group constants of this package; WP-B and WP-C choose the tag patterns and cap the unreleased tags at 5.

Work package A (pull requests, implemented in source) is `internal/statusremote/pulls.go`: it derives the repository of each workspace repository from its `origin` (a repository that was not read locally is skipped with `not-read-locally`, one without a usable origin with `not-github-origin`, one on another host with `other-host`, one whose path is not `OWNER/NAME` with `invalid-repo`; no request is made for a skipped repository) and runs one `pulls.Run` with the defaults of `stw pulls` over the reader it is given, so the verdict groups, the detail budget and the single bounded re-read of an unknown mergeability are the cycle's own. Each repository ends as `ok`, `error` with the cycle's code (a failed or rate-limit-skipped read; the rows that were read stay) or `skipped` with `deadline`, and the rows and group counts reach the action model unchanged (`PastBudget` only for rows cut by the budget). A test runs the real `stw pulls` command and this package on one fake forge and compares groups, counts and open numbers; no test needs a credential. `watchcmd.SplitRemote` exposes the host and path split that `ParseRemote` already used. The rows of an errored source still reach the action model (it turns the not-read rows into one `read-error` and no PR item), so work packages R and F must set the contract's `data` of the pulls source to null unless its state is `ok`. Not part of this package: the other sources, the orchestration with the overall deadline, the CLI and the schema.

Work package B (releases, tags, repository) is implemented in source: the typed reads `RepoInfo`, `ReleaseList` and `TagList` of the status reader in `internal/hosting` (`status_repo.go`, `status_releases.go`, `status_unreleased.go`), the draft visibility, the unreleased-tag rule over caller-given release tag patterns and `internal/statusremote/releases.go`, the per-repository source that returns the overview inputs and the states of the `repo`, `releases` and `tags` sources (see [hosting](hosting.md)). The default pattern `v*.*.*` is a semantic version with an optional `v` (`1.2.3` and `v1.2.3` are one version scheme; with both present they are listed by name). Only semantic-version tags are considered: calver and other product schemes of Spec 026 need their own later specification. The contract now says that `too-many-releases` also reports a tags listing past page 10, that `tags.latest` is a list of `{pattern, tag}`, that there is one entry per pattern and tag (`overview.ReleaseTag` carries `Pattern`, `Latest` and `Unreleased`; only `Unreleased` makes an item) and that the cap of 5 unreleased entries is filled in pattern order. `overview.Remote` carries the default branch, page and draft truncation of the `repo` and `releases` sources for the later output. Choices the spec leaves open: a source skipped by the overall deadline is `skipped` while a per-request timeout stays a `network` error, and a failed releases read leaves only the latest tag (nothing can be called unreleased without the releases). Tests use a fake forge and a fixture `gh` only; no build was made.

## Spec 038 stw issues (hosting seams in source, 2026-10-22)

[The specification](../specs/038-stw-issues/spec.md), its [JSON contract](../specs/038-stw-issues/contracts/issues.md) and
[plan](../specs/038-stw-issues/plan.md) describe a read-only, stateless `stw issues` snapshot of the open issues of the workspace
repositories and the issue source of `stw status`. Work package 0 is implemented in source on `feat/038-issues-hosting`: in
`internal/hosting` the three fixed read shapes (open issue list, `user`, issue owner search), the guard mode `issues`, the issue list and
`user` in the `status` mode, the typed decoders and `NewIssuesReader` (see [hosting](hosting.md#guard-modes-of-the-read-only-transport)).
The issue cycle, the command, the schema and the status integration (work packages A to D) are not started, and `stw issues` does not
exist yet. The tests use fixtures only (no network, no build); independent review of the exact head is pending.

## Active work: Spec 032 stw sync (slice 1a source implemented, 2026-10-21)

[The specification](../specs/032-stw-sync/spec.md), [plan](../specs/032-stw-sync/plan.md) and [tasks](../specs/032-stw-sync/tasks.md) (4/20 done: T001 to T004) describe a manual one-step `stw sync [--plan]` and `stw sync restore` that replaces `git pull --rebase --prune` per repository (fast-forward only unless `--rebase`, dirty worktrees skipped unless `--autostash`, optional submodules). The independent design review is CLEAR at 3d8d1da (after NOT CLEAR at b0cc02d, F1-F8 resolved). Slice 1a (T002 to T004) is implemented in source on `feat/032-sync-plan`: `stw sync --plan|--dry-run [PATH] [--remote NAME] [--prune] [--json]` fetches once (remote-tracking refs only) and classifies every tracking branch (`internal/stwsync`), with the unpublished `schemas/v1/sync.schema.json`. Tests use temporary repositories with a bare remote and linked worktrees, including a before/after snapshot proving that a plan changes only remote-tracking refs. Not started: the real run, journal, prune reporting beyond the plan, `--rebase`, `--autostash`, `restore`, `--submodules`, acceptance evidence and a smoke script. No build, release artifact or real-repository run was made. Status stays `planned`.

## Active work: Spec 030 PR status (slice 1 source implemented, verification pending)

Slice 1 of [Spec 030](../specs/030-pr-status/spec.md) is implemented in source on `feat/030-pr-status-one-shot`: the schema 2 one-shot `stw hosting pr` / `gh jj pr` (`--json-version 2`, `--compact`, `--expect-head`; `--watch` is a usage error) and the head lookup `stw hosting prs SHA` / `gh jj prs` over the allowlisted `commits/SHA/pulls?per_page=30&page=1` GET. Source tests use HTTP fixtures only; no build, binary smoke, live API or keyring acceptance was run, and GitHub's association behaviour for the lookup is unverified. The feature stays `planned`. The watch, delivery arbiter and descendant verification remain a deferred slice with their NOT CLEAR history ([round 3](../specs/030-pr-status/round3-disposition.md)).

## Active work: Spec 027 inventory limits (implemented, verification pending)

The Git inventory reads large outputs incrementally, degrades to partial results with verbatim disclosures instead of `git-inventory-failed` for repositories of thousands of files or hundreds of tags, describes submodule and separate-Git-directory checkouts by their files and reports `tracked: true` for ordinary changes in gitlink-bearing superprojects. Schema v1 is byte-unchanged; the Spec 023 `stw repos` tests, goldens and texts that pinned the old limits were updated with it. Evidence and what was not run (hosted CI, binary acceptance, Git 2.46.0, jj) are in [verification](../specs/027-inventory-limits/verification.md); the feature stays `planned` until then.

## Active work: Spec 023 multi-repository inventory

`stw repos PATH... [--submodules] [--json]` inventories up to 16 Git/jj repositories and their direct Git submodules in one bounded read-only run ([PR #49](https://github.com/wstein/sourcesteward/pull/49), branch `codex/023-multi-repo-inventory`). Work items 1 to 5 are implemented in source; the feature stays `planned` in the status record until acceptance, and the Spec 027 inventory prerequisite lands before it ships. Two independent implementation reviews at bcb3324 were NOT CLEAR (1 critical, 6 high, 12 medium, 8 low); all findings were fixed test first and recorded in [analysis round 5](../specs/023-multi-repo-inventory/analysis.md#round-5-implementation-reviews-at-bcb3324), and the fixes have not been re-reviewed independently. Submodule confinement reuses the Spec 021 safety layer, so a submodule with alternates is refused. Evidence and what did not run (jj lanes, foreign-owner case, hosted CI, arm64/darwin as local evidence only) are in [verification](../specs/023-multi-repo-inventory/verification.md). Open maintainer decision: apply the usefulness assessment (fix single-repository inventory limits first, and/or cut `--submodules`) or ship as is; until then most real members end `partial` or `error` and the exit status is usually 1. No tag, release or publication is implied. Guide: [multi-repository inventory](repos.md).

## Active work: implemented command documentation refresh

README and Pages now expose all implemented command families and practical examples;
planned Specs 012–019 and GitButler integration remain separate. The public catalog and
Spec 007 US4 amendment distinguish local reads, stored plans, authorized local apply and
GitHub reads. Full local checks, generated-site/link/schema validation and responsive browser checks pass. [PR42](https://github.com/wstein/sourcesteward/pull/42) is open; hosted CI and merge/main publication remain pending. Evidence is tracked in
[Spec 007 verification](../specs/007-hugo-docs/verification.md).

## Earlier work: Spec 022 bounded go-gh transport

Spec 020 shared hosting inspection merged in [PR #40](https://github.com/wstein/sourcesteward/pull/40) at main ee86751. Spec 022 replaces only the API subprocess transport with pinned official go-gh/v2 HTTP middleware and owned bounded decoding. Native Git/jj operations, both CLI entry points and published schema/fixture bytes remain unchanged, except authentication refusal now reports `hosting-api-unavailable` with `boundary.network=false` (before any request). Bounded gh authentication remains required for explicit remote reads; local resolution stays offline.

Plan/tasks/independent analysis preceded implementation in separate transport and compatibility worktrees. Independent integrated source review of the migration found destination/lifecycle guards and concurrent host authentication sound, but the later independent review of b6edd4a was NOT CLEAR; the fixes (hermetic smoke, Content-Encoding, CLI tests, docs) are in follow-up [PR #43](https://github.com/wstein/sourcesteward/pull/43) (Refs #41), pending hosted CI and merge. See [post-merge review follow-up](../specs/022-bounded-go-gh/verification.md#post-merge-review-follow-up). [Spec 022 verification](../specs/022-bounded-go-gh/verification.md) tracks passing full local checks/race, both jj-version SDK fixtures, 28 binary cases and hosted CI. [PR 41](https://github.com/wstein/sourcesteward/pull/41) merged on 2026-10-16; all six final-head source/minimum/current checks passed. No live API/keyring acceptance, user-profile installation, release or deletion is implied.

## Earlier work: Spec 011 workspace and reference tidy

Spec 009 inventory and Spec 010 isolated workspace creation, handoff and retirement
preflight are merged. Spec 010 source and fixture follow-ups landed through PRs
#32–#35. Retirement preflight does not remove registrations, references or files.
The historical records below preserve their original handoff gates.

Delivery now proceeds in three separately reviewed work items:

1. Evidence-only tidy plans with explicit integration bases and retained reasons.
2. Authorized local Git branch/jj bookmark cleanup with durable recovery and stale checks.
3. Owned workspace retirement with separate registration and exact-path file effects.

Each follows plan → tasks → independent analysis → implement → review/test, with
native Git/jj conformance and a separate PR. WI1 implements evidence-only plans;
see [WI1 verification](../specs/011-workspace-reference-tidy/wi1-verification.md).
WI2 implements authorized local reference cleanup with [verification](../specs/011-workspace-reference-tidy/wi2-verification.md).
WI3 implements owned retirement; [verification](../specs/011-workspace-reference-tidy/wi3-verification.md) records local checks and [PR38 handoff](https://github.com/wstein/sourcesteward/pull/38). WI1 is merged as `81c5f07`; exact-head PR36
[CI 38809418076](https://github.com/wstein/sourcesteward/actions/runs/38809418076) passed source and both native Linux lanes. WI2 PR37 exact-head [CI 38812174447](https://github.com/wstein/sourcesteward/actions/runs/38812174447)
also passed source and both native lanes. WI3 source/security review and local full checks/race
plus 36 native CLI cases per lane pass; exact-head hosted acceptance remains required.

## Historical Spec 010 implementation handoff

Implemented native Git/jj task creation through stored plans and explicit authorization,
durable ownership/recovery journals, conservative retry, handoff and retirement preparation.
Plan → tasks → independent analysis preceded test-first implementation in separate core,
Git, jj, CLI/schema and conformance worktrees. No cleanup/deletion, transfer or agent launch.

Independent source review and final consistency analysis clear source `5e32515`. Full
`make check`, race tests and both pinned local native lanes pass: 10 inspection, six jj
inspection, 36 inventory and 45 workspace CLI cases per lane. See
[verification](../specs/010-isolated-task-workspaces/verification.md) for limits and exact evidence.
Scoped review and full checks also clear the post-CI unused-filter admission repair
`e5fbaf9`; active checkout filters and ambiguous sentinel-named drivers still refuse.
Spec 010 is implemented, not globally verified. PR handoff records hosted Linux minimum/current
CI at the final head; owner review/merge and live publication remain separate actions.
Next action: review [PR #32](https://github.com/wstein/sourcesteward/pull/32) and its exact-head checks.

Spec 009 design/implementation and Linux fixture repair were merged by the owner through
PRs #29/#30/#31; local main `e67ae90` passed hosted checks and automatic Pages publication.
The older Spec 009 snapshots below preserve their original pending gates and are historical.

## Historical Spec 009 implementation handoff

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

Stack Inspection and Plan: reworked on 2026-10-17 for GitHub native stacked pull requests (read-only `stack show` and `stack plan`, opt-in pull request evidence, advisory plan; diffs withdrawn); full artifact set and author checks written.
Documentation links and whitespace checks passed before PR handoff. No runtime capability
or independent acceptance is claimed. Independent analysis and implementation remain pending.

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

## Historical Spec 009 next action

Review the Spec 009 two-PR stack (analyzed design, then implementation and evidence)
and confirm hosted Linux minimum/current checks. Spec 009 is implemented with explicit
native qualifications; the earlier proposal/planning entries are historical and do
not override this action. Owner approval/merge and live schema publication are pending.
Do not start task workspace mutations or tidy from inventory evidence alone.

## Historical Spec 009 integration handoff

The owner merged Spec 009 design/implementation PRs #29/#30 into main `9c4f385`.
The conflict-only rebase preserved the full implementation tree. A Linux-only fixture
identity failure, also reproduced locally, is repaired in a focused test-only follow-up.
Independent review and full local checks pass; the next action is to review that repair
and confirm its exact hosted minimum/current checks. Earlier owner-merge-pending statements
are historical snapshots; no live schema publication or global verification is inferred.

## Spec 026 WI4 offline release source candidate

The isolated WI4 candidate implements streamed tag publication diffs, offline tag
planning and file verification, canonical record generation and the verify/record
CLI contracts. It preserves the Git-only tag boundary and uses native operation-pinned
jj committed reads for records; the no-follow writer has a reviewed Linux/Darwin
adapter design. See [WI4 evidence](../specs/026-release-process/verification-wi4.md)
for failing-first fixtures, native lane results, compiler-only Darwin checks and
remaining limits. New result schemas remain unpublished and Spec 026 remains planned.

Full source and race suites passed. Independent review of the exact final head,
after correcting the first review’s target binding, tag observation and isolated
regression coverage findings, remains pending. The image repository clarification
received independent assessment; it adds no network or publication effects.
No product build, Hugo packaging, release, publication, owner tag or consumer activation
was performed. Normal hosted workflows and later release operations retain their
separate owner authorization requirements.

## Spec 026 WI5 online release source candidate

The isolated WI5 source candidate adds bounded fixed release GETs, online gate
selection and fake-process attestation verification, with offline zero-request
sentinels. The [WI5 evidence](../specs/026-release-process/verification-wi5.md)
records executed source checks and the unverified real-gh/Darwin boundaries.
Independent source review and the no-write descriptor-input design assessment
remain pending; T026 acceptance and global Spec 026 completion are not claimed.
No actual credentials, external release operation or product/documentation build
was used. Later effects retain their separate owner authorization requirements.

## Spec 026 WI6 dispatch source candidate

The isolated WI6 candidate implements a stateless canonical plan identity,
one fixed POST transport and `release dispatch` plan/apply integration. It
recomputes native and online build gates, refuses stale ids and local/remote
main disagreement, and exposes attempted writes and HTTP status without remote
bodies. Named products, fetch disclosures and native Git/jj identities remain
explicit. The result schema is unpublished. [WI6 evidence](../specs/026-release-process/verification-wi6.md)
records failing-first fixtures, source guard mutants, native state sentinels and
remaining verification. T028/T029 have exact-source clearance; final T030 checks
and independent review remain pending. Spec 026 stays planned. No actual
workflow, build, publication, tag, release or activation was performed.

## Spec 026 WI7 build checkout view source candidate

`stw release build` now compares the tracked tree through a frozen private view of the
checkout metadata, captured before the first project invocation and revalidated after
the build, with a supervised native process group and a trusted nested-status route
entered from `cmd/stw`. This is source only and pending independent review: T031 and
T034 stay unchecked and Spec 026 stays planned. The
[verification note](../specs/026-release-process/verification-checkout-view.md) lists
the mutation evidence and what was not verified: the Darwin observer has only been
compiled and vetted, `internal/release` does not compile for Windows (also true before
this work), and no real checkout, build, workflow or release was run. The earlier
dead-code state of the view, which had no production caller, is closed by this wiring.

## Decision-file schema registration

The Pages source validator recognizes the exact candidate `v3/tidy-decisions.schema.json` as
unpublished, like the report schema; the unexpected or linked source check is unchanged.

## PR 104 unpublished report schema CI correction

The Pages source validator now recognizes the exact candidate
`v3/tidy-report.schema.json` as unpublished. The public schema allowlist is
unchanged. The failing-first regression reproduced the missing registration;
source tests cover exclusion of both v3 schemas, invalid JSON/UTF-8, linked
files/directories, unlisted sources and byte-identical published schema copies.
All 165 source Python tests passed, excluding the one test that invokes the
actual Hugo/Go documentation build. Documentation and architecture source
checks and whitespace checks passed. The theme tests could not run locally
because Node is unavailable. No documentation/product build was run; hosted
checks and independent exact-head review remain pending.

## Spec 031 restore and history source candidate

`stw tidy restore` plans, and with `--apply --authorize` carries out, the restore of branches that
a contract 3 local-reference apply deleted. `stw tidy restore --list` and `stw tidy history` list
the stored journals. The plan is read-only; apply creates only local branches by compare-and-swap
with `git update-ref --no-deref`, never overwrites or follows a ref, writes an intent record before
and an outcome record after each creation in the `tidy-v3-restores` namespace of the private
store, and keeps the recovery refs. Only the tip commit and its history of this clone come back;
reflog tips and worktree registrations do not. Journals now also record `branch.NAME.*` and the reflog
tip before a deletion (Spec 031 T045), the plan shows which keys can be reapplied and apply reapplies
`remote`, `merge`, `rebase` and `pushRemote` when the remote still exists; older journals restore as
before. The tests build their
repositories with a real tidy v3 apply (see [the evidence](../specs/031-tidy-integration-proofs/verification.md)).
The result schemas `schemas/v3/tidy-restore.schema.json` and `schemas/v3/tidy-history.schema.json`
are unpublished candidates registered in the Pages source validator. This is source only: the
acceptance on the dogfood data set (T052) follows the owner-authorized cleanup apply and is not
done, so Spec 031 stays planned. No product or documentation build, release, tag or `apply` on a
real repository was run for this work.
