# Feature Specification: Workspace and Reference Tidy

**Feature Branch**: `codex/011-tidy-plans` (three sequential implementation work items)
**Created**: 2026-10-15
**Status**: WI1 implemented and independently reviewed; PR/hosted acceptance pending. WI2 design analyzed; implementation pending. WI3 draft design pending analysis
**Input**: Evidence-based plans, stale-state checks and recovery.
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
