# Feature Specification: Isolated Task Workspaces

**Feature Branch**: `codex/lazyjj-inspired-roadmap` (specification batch; implementation branches deferred)
**Created**: 2026-10-15
**Status**: Draft — specification only; planning, tasks, cross-artifact analysis and implementation pending
**Input**: Native Git worktrees and jj workspaces, with safe handoff and retirement.
**Priority rating**: 10/10 (user-selected product priority, not verification evidence)

## User Scenarios & Testing

### User Story 1 — Start work in an isolated checkout (Priority: P1)

A caller prepares a task without disturbing an existing checkout.

**Why this priority**: Establishes the primary safe workflow before optional integrations.

**Independent Test**: Exercise the following cases in separate temporary Git and jj repositories; compare declared outcomes and relevant before/after state.

**Acceptance Scenarios**:

1. **Given** a supported repository, explicit base and unused destination, **When** creation is planned, **Then** the caller sees backend, base identity, destination, ownership and all intended effects.
2. **Given** an authorized unchanged plan, **When** creation is applied, **Then** a native Git worktree or jj workspace is created with a durable task identity and the original checkout is preserved.
3. **Given** an occupied or aliased destination or changed base, **When** apply is attempted, **Then** the action refuses without overwriting files or silently selecting another base.

### User Story 2 — Hand off and prepare retirement (Priority: P2)

A task owner transfers context or requests safe retirement.

**Why this priority**: Adds useful handoff or coordination without weakening the primary workflow.

**Independent Test**: Exercise the following cases in separate temporary Git and jj repositories; compare declared outcomes and relevant before/after state.

**Acceptance Scenarios**:

1. **Given** an existing task workspace, **When** handoff is requested, **Then** the caller receives identities, dirty/unknown state and remaining work without launching an agent.
2. **Given** a retirement request, **When** retirement is prepared, **Then** ownership and retention evidence are shown and destructive cleanup is delegated to Spec 011.
3. **Given** an interrupted creation, **When** the caller retries, **Then** the outcome distinguishes completed and incomplete effects and does not adopt a foreign workspace.

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

- Destination races, symlinks, outside-repository destinations, insufficient space and permission failures.
- Interrupted registration, task-name collisions, foreign ownership, stale operations and concurrent base movement.

## Requirements

### Functional Requirements

- **FR-001**: Create isolated native Git worktrees and jj workspaces from an explicit supported base without initializing, migrating or switching repository managers.
- **FR-002**: Define the shared mutation contract before implementation: inspectable targets/reasons/effects, expected-state validation, caller authorization, per-action outcomes and recovery recorded before consequential changes.
- **FR-003**: Bind plans to native repository/workspace identities, relevant base/reference/operation state and destination ownership; refuse changed relevant state and document concurrent-change limits.
- **FR-004**: Record task ownership explicitly; never infer ownership or deletion permission from a filename or workspace registration alone.
- **FR-005**: Preserve the source checkout, configuration and dirty/untracked files; disclose snapshots, hooks and metadata effects before apply and never silently stash/reset or skip hooks.
- **FR-006**: Return a durable handoff and interruption/retry outcome; repository setup scripts, model/session launchers and automatic cleanup are excluded.
- **FR-007**: Prepare retirement without deleting workspace files or references; execution of retirement depends on Spec 011.
- **FR-008**: Support native Git and jj as Tier 1 peers for declared capabilities; preserve distinct identities and explicit capability gaps, refuse ambiguous/GitButler ownership and never substitute raw Git writes for jj.
- **FR-009**: Provide understandable human output and versioned structured success/error results from the same observations, preserving published contracts; no interactive prompt, editor or pager is required.
- **FR-010**: Declare and enforce observation/action scope, output and time limits, cancellation and incomplete-result behavior; never convert missing or truncated evidence into success or permission to mutate.
- **FR-011**: Require acceptance evidence for minimum/current supported Git and jj, including failure, concurrency, ownership and effects cases; missing mandatory coverage cannot count as passed conformance.

### Key Entities

- Task workspace: task identity, owner, native workspace/repository identity and base.
- Mutation plan: explicit targets, preconditions, effects and recovery scope.
- Action outcome: completed/refused/failed actions and durable recovery evidence.

## Success Criteria

### Measurable Outcomes

- **SC-001**: Every acceptance scenario passes for each declared supported backend/version; unsupported cases return explicit capability limits rather than fabricated success.
- **SC-002**: Every safety/failure fixture preserves state outside declared effects, executes zero undeclared helpers/network actions and reports incomplete or partial outcomes accurately.
- **SC-003**: Every complete structured success/error fixture validates against its documented contract; all existing published-contract regression fixtures remain valid.
- **SC-004**: Every functional requirement has a traceable acceptance case before implementation handoff; no missing mandatory execution is counted as verified.

## Assumptions

- Depends on Spec 009 inventory; this specification owns the shared local mutation contract reused by Specs 011, 013 and 015.
- Workspace creation/handoff can ship before retirement execution; Spec 011 is not a prerequisite for creation.
- Recovery restores only documented local state; no claim of restoring untracked files, hook side effects or external effects.
- This draft does not amend the constitution or authorize execution. Owner review, plan, tasks and consistency analysis precede implementation; later mutation/forge scope must be reconciled with constitutional constraints during planning.
- Sampled state cannot promise a transaction or detect every concurrent/ABA change; limitations must remain explicit and uncertainty never becomes cleanup proof.
