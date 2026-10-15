# Feature Specification: Checkpoint and Resume

**Feature Branch**: `codex/lazyjj-inspired-roadmap` (specification batch; implementation branches deferred)
**Created**: 2026-10-15
**Status**: Draft — specification only; planning, tasks, cross-artifact analysis and implementation pending
**Input**: Preserve progress with backend-specific recovery guarantees.
**Priority rating**: 9/10 (user-selected product priority, not verification evidence)

## User Scenarios & Testing

### User Story 1 — Save a recoverable checkpoint (Priority: P1)

A task owner preserves local progress before a handoff or risky operation.

**Why this priority**: Establishes the primary safe workflow before optional integrations.

**Independent Test**: Exercise the following cases in separate temporary Git and jj repositories; compare declared outcomes and relevant before/after state.

**Acceptance Scenarios**:

1. **Given** an owned workspace with local changes, **When** checkpoint is planned, **Then** included/excluded files, snapshot effects and recovery limits are shown.
2. **Given** an authorized unchanged plan, **When** checkpoint is applied, **Then** a durable checkpoint identifies exactly what was saved without disturbing unrelated work.
3. **Given** untracked files or possible secrets outside selected scope, **When** checkpoint is requested, **Then** no unselected content is silently captured.

### User Story 2 — Resume safely (Priority: P1)

A human or agent resumes from explicit checkpoint evidence.

**Why this priority**: Establishes the primary safe workflow before optional integrations.

**Independent Test**: Exercise the following cases in separate temporary Git and jj repositories; compare declared outcomes and relevant before/after state.

**Acceptance Scenarios**:

1. **Given** a valid checkpoint, **When** resume is inspected, **Then** saved identities, task context, differences and limitations are shown without modifying files.
2. **Given** concurrent edits or missing retained objects, **When** restore is requested, **Then** the operation refuses unsafe replacement and reports unavailable recovery.
3. **Given** an interrupted restore, **When** status is requested, **Then** completed effects and remaining recovery steps are explicit.

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

- Staged versus unstaged edits, untracked/ignored files, binary content, symlinks and deleted files.
- Expired objects, divergent operations, changed workspace ownership and partial capture/restore failures.

## Requirements

### Functional Requirements

- **FR-001**: Reuse Spec 010 mutation/ownership contracts and require explicit checkpoint capture scope, including tracked, staged and untracked content where supported.
- **FR-002**: Preserve native semantics: Git index/worktree state and jj working-copy/change/operation state are not interchangeable.
- **FR-003**: Record durable checkpoint identity, selected content, exclusions, retention and recovery limits before acknowledging success.
- **FR-004**: Make resume inspection read-only; restoration is a separate authorized plan that refuses changed relevant state and never silently discards current work.
- **FR-005**: Keep task context bounded and caller-provided; never execute stored text or automatically capture credentials, environment dumps or agent transcripts.
- **FR-006**: Report partial outcomes and backend-specific unsupported recovery; a jj operation ID alone does not promise restoration of disk edits or external effects.
- **FR-007**: Support native Git and jj as Tier 1 peers for declared capabilities; preserve distinct identities and explicit capability gaps, refuse ambiguous/GitButler ownership and never substitute raw Git writes for jj.
- **FR-008**: Provide understandable human output and versioned structured success/error results from the same observations, preserving published contracts; no interactive prompt, editor or pager is required.
- **FR-009**: Declare and enforce observation/action scope, output and time limits, cancellation and incomplete-result behavior; never convert missing or truncated evidence into success or permission to mutate.
- **FR-010**: Require acceptance evidence for minimum/current supported Git and jj, including failure, concurrency, ownership and effects cases; missing mandatory coverage cannot count as passed conformance.

### Key Entities

- Checkpoint: retained native identities, content scope, task context and recovery limits.
- Resume comparison: checkpoint/current differences and safe next actions.

## Success Criteria

### Measurable Outcomes

- **SC-001**: Every acceptance scenario passes for each declared supported backend/version; unsupported cases return explicit capability limits rather than fabricated success.
- **SC-002**: Every safety/failure fixture preserves state outside declared effects, executes zero undeclared helpers/network actions and reports incomplete or partial outcomes accurately.
- **SC-003**: Every complete structured success/error fixture validates against its documented contract; all existing published-contract regression fixtures remain valid.
- **SC-004**: Every functional requirement has a traceable acceptance case before implementation handoff; no missing mandatory execution is counted as verified.

## Assumptions

- Depends on Specs 009 and 010; stack inspection is optional context.
- Checkpoints are local and explicit; automatic background snapshots, cloud synchronization and remote rollback are excluded.
- This draft does not amend the constitution or authorize execution. Owner review, plan, tasks and consistency analysis precede implementation; later mutation/forge scope must be reconciled with constitutional constraints during planning.
- Sampled state cannot promise a transaction or detect every concurrent/ABA change; limitations must remain explicit and uncertainty never becomes cleanup proof.
