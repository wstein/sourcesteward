# Feature Specification: Selected Work Transfer

**Feature Branch**: `codex/017-work-transfer-spec` (specification only)
**Created**: 2026-10-15
**Status**: Draft — planning, tasks, cross-artifact analysis and implementation pending
**Input**: Adopt the user-selected Git Toolbelt use case with native Git/jj semantics.
**Priority rating**: 9/10 (product priority, not acceptance evidence)

## User Scenarios & Testing

### User Story 1 — Move work started in the wrong workspace (Priority: P1)

A human or agent needs an explicit, bounded outcome without disturbing unrelated work.

**Why this priority**: This closes a user-selected everyday workflow gap while retaining ownership and recovery guarantees.

**Independent Test**: Exercise supported success, refusal and interrupted cases in temporary Git and jj fixtures and compare relevant before/after state.

**Acceptance Scenarios**:

1. **Given** selected local changes or revisions and an owned destination, **When** transfer is planned, **Then** copy versus move, source/destination effects and retained unrelated work are explicit.
2. **Given** a missing destination with an explicit base, **When** workspace creation is selected, **Then** Spec 010 creates the destination and no original branch is implicitly reset.
3. **Given** mixed selected/unselected changes in a file, **When** transfer is planned, **Then** only explicit selected content is included or the unsupported partial selection refuses.

### User Story 2 — Apply or recover a bounded transfer (Priority: P1)

A human or agent needs an explicit, bounded outcome without disturbing unrelated work.

**Why this priority**: This closes a user-selected everyday workflow gap while retaining ownership and recovery guarantees.

**Independent Test**: Exercise supported success, refusal and interrupted cases in temporary Git and jj fixtures and compare relevant before/after state.

**Acceptance Scenarios**:

1. **Given** authorized unchanged source/destination identities, **When** transfer applies, **Then** native effects and old/new identities are recorded and unrelated files/index state remain unchanged.
2. **Given** conflicts, occupied destination or foreign ownership, **When** apply runs, **Then** unsafe actions refuse and completed effects are reported.
3. **Given** interruption after destination copy but before source removal, **When** resume is requested, **Then** the caller sees partial state and no duplicate copy or unvalidated source deletion occurs.

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

- Mixed selected/unselected edits, staged and disk changes to the same file, binary/rename changes and untracked content.
- Destination collision, shared revisions, concurrent edits and interruption between copy and removal.

## Requirements

### Functional Requirements

- **FR-001**: Support explicit selected file/content changes or revisions within one same-backend repository; cross-repository and cross-backend transfer are excluded.
- **FR-002**: Require explicit copy or move semantics. Moving requires separately planned source removal; copying never implicitly resets the source reference.
- **FR-003**: Disclose Git staged/unstaged selection and jj stored/unsnapshotted selection separately; preserve unselected content and refuse unsupported partial transfer.
- **FR-004**: Use Spec 010 for owned destination creation and state validation. Never adopt an existing foreign workspace, overwrite a destination or mutate a checked-out reference owned elsewhere.
- **FR-005**: Record source/destination recovery evidence and outcomes before consequential actions; no hidden stash, reset, hook bypass, push or automatic conflict resolution.
- **FR-006**: Require explicit native scope for descendants and references; jj change transfer/rewrite cannot silently mean a Git cherry-pick.
- **FR-007**: Support native Git and jj as Tier 1 peers for declared capabilities; preserve distinct identities and explicit capability gaps, refuse ambiguous/GitButler ownership and never substitute raw Git writes for jj.
- **FR-008**: Provide understandable human output and versioned structured success/error results from the same observations, preserving published contracts; no interactive prompt, editor or pager is required.
- **FR-009**: Declare and enforce observation/action scope, output and time limits, cancellation and incomplete-result behavior; never convert missing or truncated evidence into success or permission to mutate.
- **FR-010**: Require acceptance evidence for minimum/current supported Git and jj, including failure, concurrency, ownership and effects cases; missing mandatory coverage cannot count as passed conformance.

### Key Entities

- Transfer plan: selected native content/revisions, copy/move mode, source/destination identities and effects.
- Transfer outcome: mappings, retained content, conflicts and recoverable partial actions.

## Success Criteria

### Measurable Outcomes

- **SC-001**: Every declared supported acceptance case passes for minimum/current Git and jj; missing mandatory execution blocks acceptance.
- **SC-002**: Every stale, ambiguous, foreign-owned or unsupported fixture refuses affected actions and preserves unrelated files, references and metadata.
- **SC-003**: Every success, refusal, cancellation and partial-failure fixture reports accurate outcomes; structured fixtures validate and existing published contracts remain compatible.
- **SC-004**: Every consequential action has retained recovery evidence before changing state; all recovery tests achieve exactly the documented scope without claiming remote/hook/untracked restoration.

## Assumptions

- Depends on Specs 009 and 010; Spec 013 may provide an explicit checkpoint, and Spec 014 may provide optional preflight.
- This feature does not automatically switch the caller checkout, infer a desired destination or reset work on main.
- Depends on the shared mutation contract in Spec 010. This draft grants no execution authority; owner review, planning, tasks and consistency analysis precede implementation.
- Native Git and jj differ; no synthetic staging area or Git fallback is required for jj. GitButler-managed writes remain unsupported.
- Sampled preconditions are not transactional and cannot detect every ABA change; disclose limitations and refuse uncertain actions.
