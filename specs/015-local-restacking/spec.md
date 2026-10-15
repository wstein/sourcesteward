# Feature Specification: Local Restacking

**Feature Branch**: `codex/lazyjj-inspired-roadmap` (specification batch; implementation branches deferred)
**Created**: 2026-10-15
**Status**: Draft — specification only; planning, tasks, cross-artifact analysis and implementation pending
**Input**: Add after the mutation/recovery contract.
**Priority rating**: 8/10 (user-selected product priority, not verification evidence)

## User Scenarios & Testing

### User Story 1 — Preview a local stack move (Priority: P1)

A caller understands a stack rewrite before applying it.

**Why this priority**: Establishes the primary safe workflow before optional integrations.

**Independent Test**: Exercise the following cases in separate temporary Git and jj repositories; compare declared outcomes and relevant before/after state.

**Acceptance Scenarios**:

1. **Given** an explicit stack and destination base, **When** restacking is planned, **Then** affected revisions, descendants, references, workspaces and potential conflicts are listed.
2. **Given** ambiguous boundaries, immutable/shared revisions or unsupported topology, **When** restacking is planned, **Then** blocked scope and reasons are shown without guessing or widening the target.

### User Story 2 — Apply and recover a local rewrite (Priority: P1)

A caller authorizes a bounded rewrite with recovery evidence.

**Why this priority**: Establishes the primary safe workflow before optional integrations.

**Independent Test**: Exercise the following cases in separate temporary Git and jj repositories; compare declared outcomes and relevant before/after state.

**Acceptance Scenarios**:

1. **Given** an authorized unchanged plan, **When** restacking is applied, **Then** only declared native effects occur and old/new identities and reference outcomes are recorded.
2. **Given** a new conflict, cancellation or partial failure, **When** restacking stops, **Then** native conflict/interruption state and recovery options are reported without automatic discard.
3. **Given** changed refs, workspace owners or operation state, **When** apply is attempted, **Then** stale actions refuse before their mutations.

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

- Merge topology, empty/redundant changes, immutable jj commits, divergent operations and multiple workspaces.
- Hook failures, conflict creation, cancellation and concurrent reference/operation changes.

## Requirements

### Functional Requirements

- **FR-001**: Depend on the Spec 010 mutation/recovery contract and Spec 012 explicit stack boundaries.
- **FR-002**: Require explicit source scope and destination identity; disclose descendant rewrites, reference movements, disk/index changes, snapshots and hook effects before apply.
- **FR-003**: Respect immutable/protected and shared work; refuse unsupported topology or ownership rather than falling back to another backend.
- **FR-004**: Retain recoverable pre-operation identities and report native old/new mappings, including jj stable change identity versus rewritten commit identity.
- **FR-005**: Handle Git interrupted operations and jj stored conflicts explicitly; neither backend may report a clean completed outcome when unresolved work remains.
- **FR-006**: Revalidate relevant state before consequential actions; expose partial failure, cancellation and recovery limitations. Never fetch, push or force-push as part of local restacking.
- **FR-007**: Support native Git and jj as Tier 1 peers for declared capabilities; preserve distinct identities and explicit capability gaps, refuse ambiguous/GitButler ownership and never substitute raw Git writes for jj.
- **FR-008**: Provide understandable human output and versioned structured success/error results from the same observations, preserving published contracts; no interactive prompt, editor or pager is required.
- **FR-009**: Declare and enforce observation/action scope, output and time limits, cancellation and incomplete-result behavior; never convert missing or truncated evidence into success or permission to mutate.
- **FR-010**: Require acceptance evidence for minimum/current supported Git and jj, including failure, concurrency, ownership and effects cases; missing mandatory coverage cannot count as passed conformance.

### Key Entities

- Restack plan: native source scope, destination, affected descendants and declared effects.
- Rewrite outcome: old/new identities, conflicts and recovery evidence.

## Success Criteria

### Measurable Outcomes

- **SC-001**: Every acceptance scenario passes for each declared supported backend/version; unsupported cases return explicit capability limits rather than fabricated success.
- **SC-002**: Every safety/failure fixture preserves state outside declared effects, executes zero undeclared helpers/network actions and reports incomplete or partial outcomes accurately.
- **SC-003**: Every complete structured success/error fixture validates against its documented contract; all existing published-contract regression fixtures remain valid.
- **SC-004**: Every functional requirement has a traceable acceptance case before implementation handoff; no missing mandatory execution is counted as verified.

## Assumptions

- Depends on Specs 010, 012 and 014; optional checkpoints reuse Spec 013.
- No automatic restacking of every stack, remote history rewrite or implicit base synchronization.
- This draft does not amend the constitution or authorize execution. Owner review, plan, tasks and consistency analysis precede implementation; later mutation/forge scope must be reconciled with constitutional constraints during planning.
- Sampled state cannot promise a transaction or detect every concurrent/ABA change; limitations must remain explicit and uncertainty never becomes cleanup proof.
