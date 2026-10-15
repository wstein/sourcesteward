# Feature Specification: Workspace and Worktree Inventory

**Feature Branch**: `codex/lazyjj-inspired-roadmap` (specification batch; implementation branches deferred)
**Created**: 2026-10-15
**Status**: Draft — specification only; planning, tasks, cross-artifact analysis and implementation pending
**Input**: Show ownership, dirty/stale state and identities.
**Priority rating**: 10/10 (user-selected product priority, not verification evidence)

## User Scenarios & Testing

### User Story 1 — Understand all local workspaces (Priority: P1)

A human or agent inventories a repository before assigning work.

**Why this priority**: Establishes the primary safe workflow before optional integrations.

**Independent Test**: Exercise the following cases in separate temporary Git and jj repositories; compare declared outcomes and relevant before/after state.

**Acceptance Scenarios**:

1. **Given** a Git repository with linked worktrees or a jj repository with secondary workspaces, **When** inventory is requested, **Then** each registered workspace has its own path and native identity, with shared repository identity preserved.
2. **Given** a missing path, stale registration or foreign manager, **When** inventory is requested, **Then** the result reports that condition and ownership evidence without repair, pruning or backend fallback.

### User Story 2 — Distinguish known disk state from stored state (Priority: P1)

A caller decides whether a workspace can safely be reused.

**Why this priority**: Establishes the primary safe workflow before optional integrations.

**Independent Test**: Exercise the following cases in separate temporary Git and jj repositories; compare declared outcomes and relevant before/after state.

**Acceptance Scenarios**:

1. **Given** tracked edits, untracked files and conflicts, **When** status is requested, **Then** these states are reported separately where observable without writes; unavailable observations are unknown rather than clean.
2. **Given** a jj workspace with edits absent from its stored commit, **When** inventory is requested, **Then** stored and disk observations remain distinct and no snapshot or operation change occurs.
3. **Given** concurrent changes or unreadable paths, **When** inventory is requested, **Then** uncertainty and observation bounds are reported without claiming a transactional snapshot.

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

- Deleted directories, duplicate or aliased paths, symlinks, inaccessible metadata and unusual filenames.
- Bare/unborn/detached Git, standalone/colocated jj, stale jj workspaces and divergent operations.
- Concurrent workspace registration changes and output exceeding documented limits.

## Requirements

### Functional Requirements

- **FR-001**: Enumerate registered Git worktrees and native jj workspaces within the explicitly selected repository; do not scan arbitrary neighboring directories.
- **FR-002**: Report workspace path, native workspace identity, repository identity, selected manager, registration health and available checkout/revision identities.
- **FR-003**: Report tracked changes, untracked files and conflicts as separate known/unknown observations; disclose observation freshness and limitations.
- **FR-004**: Separate manager ownership from task ownership: do not infer that a workspace belongs to this caller from its name, path or checked-out reference.
- **FR-005**: Never snapshot jj, refresh repository state, repair registrations, fetch or invoke repository-controlled helpers during inventory; fail or qualify unavailable observations.
- **FR-006**: Support native Git and jj as Tier 1 peers for declared capabilities; preserve distinct identities and explicit capability gaps, refuse ambiguous/GitButler ownership and never substitute raw Git writes for jj.
- **FR-007**: Provide understandable human output and versioned structured success/error results from the same observations, preserving published contracts; no interactive prompt, editor or pager is required.
- **FR-008**: Declare and enforce observation/action scope, output and time limits, cancellation and incomplete-result behavior; never convert missing or truncated evidence into success or permission to mutate.
- **FR-009**: Require acceptance evidence for minimum/current supported Git and jj, including failure, concurrency, ownership and effects cases; missing mandatory coverage cannot count as passed conformance.

### Key Entities

- Workspace observation: native identities, path, manager evidence, registration health and qualified disk state.
- Observation boundary: sampled state, completeness, limits and explicit uncertainty.

## Success Criteria

### Measurable Outcomes

- **SC-001**: Every acceptance scenario passes for each declared supported backend/version; unsupported cases return explicit capability limits rather than fabricated success.
- **SC-002**: Every safety/failure fixture preserves state outside declared effects, executes zero undeclared helpers/network actions and reports incomplete or partial outcomes accurately.
- **SC-003**: Every complete structured success/error fixture validates against its documented contract; all existing published-contract regression fixtures remain valid.
- **SC-004**: Every functional requirement has a traceable acceptance case before implementation handoff; no missing mandatory execution is counted as verified.

## Assumptions

- Depends on accepted identity inspection (Spec 008) and preserves its published contracts.
- This feature inventories registered workspaces; task creation, cleanup, stack inference and full filesystem searches are excluded.
- This draft does not amend the constitution or authorize execution. Owner review, plan, tasks and consistency analysis precede implementation; later mutation/forge scope must be reconciled with constitutional constraints during planning.
- Sampled state cannot promise a transaction or detect every concurrent/ABA change; limitations must remain explicit and uncertainty never becomes cleanup proof.
