# Feature Specification: Conflict Diagnosis

**Feature Branch**: `codex/lazyjj-inspired-roadmap` (specification batch; implementation branches deferred)
**Created**: 2026-10-15
**Status**: Draft — specification only; planning, tasks, cross-artifact analysis and implementation pending
**Input**: Provide useful evidence to any agent or human.
**Priority rating**: 9/10 (user-selected product priority, not verification evidence)

## User Scenarios & Testing

### User Story 1 — Understand unresolved conflicts (Priority: P1)

A caller identifies conflicts and their relevant revisions before making changes.

**Why this priority**: Establishes the primary safe workflow before optional integrations.

**Independent Test**: Exercise the following cases in separate temporary Git and jj repositories; compare declared outcomes and relevant before/after state.

**Acceptance Scenarios**:

1. **Given** a Git interrupted merge/rebase or jj conflicted change, **When** diagnosis is requested, **Then** the result identifies native conflict state, affected paths and known participating revisions.
2. **Given** incomplete or unsupported conflict metadata, **When** diagnosis is requested, **Then** unknown facts and supported next steps are explicit without inventing a resolution.

### User Story 2 — Hand evidence to a resolver (Priority: P2)

A human or agent receives bounded conflict context.

**Why this priority**: Adds useful handoff or coordination without weakening the primary workflow.

**Independent Test**: Exercise the following cases in separate temporary Git and jj repositories; compare declared outcomes and relevant before/after state.

**Acceptance Scenarios**:

1. **Given** conflicted text or binary files, **When** a handoff is requested, **Then** bounded evidence distinguishes stored versus disk state and labels repository content as data.
2. **Given** hostile instructions inside repository files, **When** diagnosis runs, **Then** no instruction, helper, model, editor or network action is executed.
3. **Given** concurrent changes, **When** diagnosis completes, **Then** observations disclose uncertainty and never certify the conflict as resolved.

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

- Binary, rename/delete and directory/file conflicts; conflicts on non-current jj changes.
- Missing participants, edited conflict markers, divergent operations and large/sensitive file contents.

## Requirements

### Functional Requirements

- **FR-001**: Diagnose supported Git and native jj conflict states without equating an interrupted Git operation with jj first-class conflicted changes.
- **FR-002**: Report affected paths, known participating identities, conflict type, source/freshness and bounded evidence; unknown fields must remain explicit.
- **FR-003**: Offer explanatory next steps and a machine-readable handoff without selecting an agent provider or executing suggested commands.
- **FR-004**: Never resolve, stage, snapshot, continue/abort operations or rewrite files as part of diagnosis.
- **FR-005**: Bound content exposure, preserve valid unusual paths and safely render terminal controls; repository content and backend diagnostics remain untrusted data.
- **FR-006**: Support native Git and jj as Tier 1 peers for declared capabilities; preserve distinct identities and explicit capability gaps, refuse ambiguous/GitButler ownership and never substitute raw Git writes for jj.
- **FR-007**: Provide understandable human output and versioned structured success/error results from the same observations, preserving published contracts; no interactive prompt, editor or pager is required.
- **FR-008**: Declare and enforce observation/action scope, output and time limits, cancellation and incomplete-result behavior; never convert missing or truncated evidence into success or permission to mutate.
- **FR-009**: Require acceptance evidence for minimum/current supported Git and jj, including failure, concurrency, ownership and effects cases; missing mandatory coverage cannot count as passed conformance.

### Key Entities

- Conflict observation: backend state, affected paths, participant identities and uncertainty.
- Resolver handoff: bounded evidence, context and non-executed next-step suggestions.

## Success Criteria

### Measurable Outcomes

- **SC-001**: Every acceptance scenario passes for each declared supported backend/version; unsupported cases return explicit capability limits rather than fabricated success.
- **SC-002**: Every safety/failure fixture preserves state outside declared effects, executes zero undeclared helpers/network actions and reports incomplete or partial outcomes accurately.
- **SC-003**: Every complete structured success/error fixture validates against its documented contract; all existing published-contract regression fixtures remain valid.
- **SC-004**: Every functional requirement has a traceable acceptance case before implementation handoff; no missing mandatory execution is counted as verified.

## Assumptions

- Depends on Specs 008 and 009; Spec 012 may supply optional history context.
- Conflict resolution and restore/continue actions require later mutation specifications; no correctness claim based solely on marker removal.
- This draft does not amend the constitution or authorize execution. Owner review, plan, tasks and consistency analysis precede implementation; later mutation/forge scope must be reconciled with constitutional constraints during planning.
- Sampled state cannot promise a transaction or detect every concurrent/ABA change; limitations must remain explicit and uncertainty never becomes cleanup proof.
