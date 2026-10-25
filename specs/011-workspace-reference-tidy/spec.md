# Feature Specification: Workspace and Reference Tidy

**Feature Branch**: `codex/011-tidy-plans` (three sequential implementation work items)
**Created**: 2026-10-15
**Status**: All three WIs implemented and independently reviewed; local verification complete; PRs 36/37/38 opened; WI3 hosted acceptance pending
**Input**: Evidence-based plans, stale-state checks and recovery.

**Issue #62 amendment (2026-10-18)**: Shared Git layout design candidate, independent analysis and implementation pending. The historical status above does not verify this amendment. Native Git worktrees and native jj bookmarks are essential v0.1.0 acceptance; existing native bookmark cleanup remains qualified as below.

**Later work**: the workspace-wide `stw tidy` wizard and plan sets are specified and implemented by [Spec 041](../041-tidy-wizard/spec.md); this specification's plans remain its sub-plans and the expert layer.

**Priority rating**: 10/10 (user-selected product priority, not verification evidence)

## Delivery Work Items

1. WI1: evidence-only reference tidy plans; no native deletion.
2. WI2: authorized local Git branch/jj bookmark cleanup with durable recovery and stale checks.
3. WI3: owned workspace retirement with registration and exact filesystem actions separately authorized.

Unsupported squash/forge proof retains. jj bookmarks with any remote counterpart retain to avoid
changing later remote deletion intent. Git registration-only removal of a present checkout is
unsupported and retains; combined native removal requires both exact action authorizations.
Native jj empty working-copy retirement requires one integrated parent, complete stored-tree
equality to that parent and complete fresh clean disk; emptiness alone is never proof.

## User Scenarios & Testing

### User Story 1 — Review cleanup candidates (Priority: P1)

A human or agent identifies disposable local work while retaining uncertain work.

**Why this priority**: Establishes the primary safe workflow before optional integrations.

**Independent Test**: Exercise the following cases in separate temporary Git and jj repositories; compare declared outcomes and relevant before/after state.

**Acceptance Scenarios**:

1. **Given** integrated and unintegrated local branches/bookmarks, **When** tidy is planned against an explicit base, **Then** each candidate has native identities, evidence and a delete/retain reason.
2. **Given** a missing upstream, an empty commit or a merged PR with a different head/base, **When** tidy is planned, **Then** that fact alone never authorizes deletion.
3. **Given** a dirty, untracked, active, foreign-owned or unknown workspace, **When** retirement is planned, **Then** it is retained with the exact blocking reason.

### User Story 2 — Apply only reviewed valid cleanup (Priority: P1)

A caller applies an explicitly authorized cleanup plan.

**Why this priority**: Establishes the primary safe workflow before optional integrations.

**Independent Test**: Exercise the following cases in separate temporary Git and jj repositories; compare declared outcomes and relevant before/after state.

**Acceptance Scenarios**:

1. **Given** unchanged proven candidates and recorded recovery, **When** apply is requested, **Then** only listed local targets are changed and each action reports an outcome.
2. **Given** a moved tip/base, new worktree owner, changed files or expired remote evidence, **When** apply is requested, **Then** affected actions refuse rather than reuse stale proof.
3. **Given** partial failure, **When** cleanup stops, **Then** completed actions and remaining work are recorded and recovery is offered within declared limits.
4. **Given** a primary or linked Git checkout admitted by the [Spec 010 layout contract](../010-isolated-task-workspaces/contracts/workspace.md#git-checkout-layout-admission-issue-62), **When** reference cleanup or owned retirement is planned/applied, **Then** the same layout proof is required throughout recovery and deletion; only declared target effects occur, with the primary and unrelated linked state preserved.
5. **Given** changed mapping/configuration or unresolved linked ownership, **When** the next cleanup action is evaluated, **Then** it refuses stale or incomplete evidence and retains its recovery pin and any already recorded effects; native ancestry alone never bypasses layout proof.

### User Story 3 — Rely on a bounded agent contract (Priority: P1)

A caller receives the same evidence in human and structured output without hidden effects.

**Why this priority**: Both humans and automation must recognize success, uncertainty and refusal.

**Independent Test**: Exercise normal, unsupported, malformed and cancelled cases for minimum/current supported Git and jj, with effect sentinels and published-contract fixtures.

**Acceptance Scenarios**:

1. **Given** supported success or failure, **When** either output mode is selected, **Then** native identities, qualifications and outcomes agree and complete structured results validate against their documented contract.
2. **Given** ambiguous ownership, a missing/unsupported backend or GitButler-managed state, **When** execution is requested, **Then** it refuses clearly without using another backend as a fallback.
3. **Given** cancellation, malformed data or an exceeded documented bound, **When** execution stops, **Then** incomplete evidence is explicit and never authorizes a consequential action; any completed effects are reported.
4. **Given** repository-controlled helpers, instructions or configuration, **When** the operation runs, **Then** no undeclared executable, hook, prompt or network action occurs and state outside declared effects is preserved.
5. **Given** a missing mandatory backend/version or absent acceptance execution, **When** conformance is evaluated, **Then** the missing evidence blocks acceptance rather than counting as a pass.

### Edge Cases

- Squash/rebase integration, rewritten PR heads, force-updated bases, gone upstreams and unavailable forge evidence.
- Shared jj changes, multiple bookmarks, dirty/untracked work, path replacement races and linked worktree ownership changes.
- Recovery retention expiry and concurrent state changes that cannot be transactionally excluded.

## Requirements

### Functional Requirements

- **FR-001**: Reuse Spec 010 mutation contracts and Spec 009 ownership/state evidence; default to planning without deletion.
- **FR-002**: Require an explicit integration base; distinguish ancestry, squash-equivalence and forge integration evidence and their limits. Unsupported or inconclusive proof retains the target.
- **FR-003**: Forge proof must bind the actual head revision and integration target; merged status or a branch-name match alone is insufficient. Network failure retains affected candidates.
- **FR-004**: Retain checked-out/workspace-used references and dirty, untracked, active, foreign-owned or uncertain workspaces; report blockers in human and structured output.
- **FR-005**: Treat jj change abandonment, bookmark deletion, workspace forgetting and filesystem removal as distinct operations with distinct effects; deleting a bookmark does not imply abandoning changes.
- **FR-006**: Require separate explicit filesystem-removal targets and ownership proof; never derive recursive deletion permission from a workspace name or forgotten registration.
- **FR-007**: Revalidate before each action, preserve recoverable native identities before changing state and report partial outcomes; remote reference deletion and automatic garbage collection are excluded.
- **FR-008**: Support native Git and jj as Tier 1 peers for declared capabilities; preserve distinct identities and explicit capability gaps, refuse ambiguous/GitButler ownership and never substitute raw Git writes for jj.
- **FR-009**: Provide understandable human output and versioned structured success/error results from the same observations, preserving published contracts; no interactive prompt, editor or pager is required.
- **FR-010**: Declare and enforce observation/action scope, output and time limits, cancellation and incomplete-result behavior; never convert missing or truncated evidence into success or permission to mutate.
- **FR-011**: Require acceptance evidence for minimum/current supported Git and jj, including failure, concurrency, ownership and effects cases; missing mandatory coverage cannot count as passed conformance.
- **FR-012**: Reuse the [Spec 010 layout admission and fingerprint](../010-isolated-task-workspaces/contracts/workspace.md#git-checkout-layout-admission-issue-62) for all Git tidy observations, recovery/deletion and retirement source/target validation. Preserve separate native identities, exact ownership, complete protected-workspace evidence and narrow action effects; supporting initialized submodule checkouts never authorizes nested-submodule activation or primary checkout retirement.

### Key Entities

- Cleanup candidate: exact native target, selected base, proof and retain/delete reason.
- Retirement action: metadata, reference, change or filesystem action with separate authorization and recovery.

## Success Criteria

### Measurable Outcomes

- **SC-001**: Every acceptance scenario passes for each declared supported backend/version; unsupported cases return explicit capability limits rather than fabricated success.
- **SC-002**: Every safety/failure fixture preserves state outside declared effects, executes zero undeclared helpers/network actions and reports incomplete or partial outcomes accurately.
- **SC-003**: Every complete structured success/error fixture validates against its documented contract; all existing published-contract regression fixtures remain valid.
- **SC-004**: Every functional requirement has a traceable acceptance case before implementation handoff; no missing mandatory execution is counted as verified.

## Assumptions

- Depends on Specs 009 and 010; optional forge evidence is separately enabled and does not make local proof require network.
- No deletion overrides for uncertain ownership or dirty state are included in this slice.
- This draft does not amend the constitution or authorize execution. Owner review, plan, tasks and consistency analysis precede implementation; later mutation/forge scope must be reconciled with constitutional constraints during planning.
- Sampled state cannot promise a transaction or detect every concurrent/ABA change; limitations must remain explicit and uncertainty never becomes cleanup proof.


## Explicit contract-3 source amendment

Spec 029 adds separately selected Git/GitHub.com exact merged-head evidence, local tracking
reference cleanup and one-operation legacy completion. Existing default v1/v2 behavior and
published schema bytes remain unchanged. Stable provider identities/exact-head attribution
and local reported-integration-result ancestry bind online proof; complete resampled branch
advertisements bind tracking absence. Every workspace/base/symbolic/ownership guard remains.
Legacy completion is independent provenance, never a creation journal or inferred activity.
The new Git-v3-only user-content policy and shared complete metadata channel limits are
specified in Spec 029 FR-017–FR-032. Source implementation and global/native acceptance remain
separate; no server writes, fetch/prune, automatic adoption or cleanup fallback is added.

## Narrow registration prune amendment (2026-10-25)

Owner-approved scope: exactly one named stw-locked Git registration with a missing
directory and durable registration recovery evidence, as specified in the
[narrow prune contract](contracts/narrow-registration-prune.md). Broad worktree prune
remains excluded. This proposed extension is separate from implemented WIs and present
checkout retirement; no existing implementation or conformance claim covers it.

- **FR-013**: Enforce the contract's exact selection, original ownership/lock binding,
  defined missing-path/device/mount predicate, immutable plan, fresh checks, confined effect and explicit backend refusals.
- **FR-014**: Persist and verify bounded private raw registration recovery plus committed-history
  recovery before removal; report partial effects and require separate authorized restoration.
- **FR-015**: Resolve the frozen-schema/interface decision with
  the owner before implementation; preserve published contracts and unified exit classes.

Acceptance for FR-013–FR-015 is the new T501–T508 sequence in [tasks](tasks.md).
The contract defines no-follow ENOENT plus accessible ancestors, same-device and stable
mount evidence. It discloses same-filesystem moves and sampled mount/race limits; complete
raw recovery plus separately authorized native repair bounds the moved-checkout risk.
Until the decisions and implementation are accepted, the current wizard retains stale rows.
