# Feature Specification: Stack Inspection and Diffs

**Feature Branch**: `codex/lazyjj-inspired-roadmap` (specification batch; implementation branches deferred)
**Created**: 2026-10-15
**Status**: Draft — specification only; planning, tasks, cross-artifact analysis and implementation pending
**Input**: Human-readable summaries and versioned JSON; disclose ambiguous boundaries.
**Priority rating**: 9/10 (user-selected product priority, not verification evidence)

## User Scenarios & Testing

### User Story 1 — Understand a stack (Priority: P1)

A reviewer inspects related changes against a chosen base.

**Why this priority**: Establishes the primary safe workflow before optional integrations.

**Independent Test**: Exercise the following cases in separate temporary Git and jj repositories; compare declared outcomes and relevant before/after state.

**Acceptance Scenarios**:

1. **Given** an explicit base and linear supported history, **When** stack inspection is requested, **Then** ordered native revisions and branch/bookmark relationships are reported.
2. **Given** multiple possible bases, merges or divergent changes, **When** stack inspection is requested, **Then** ambiguity is explicit and unsupported topology is not flattened into a guessed stack.

### User Story 2 — Review the scope of changes (Priority: P1)

A human or agent reviews changes without changing checkout state.

**Why this priority**: Establishes the primary safe workflow before optional integrations.

**Independent Test**: Exercise the following cases in separate temporary Git and jj repositories; compare declared outcomes and relevant before/after state.

**Acceptance Scenarios**:

1. **Given** a bounded stack with changed files, **When** diff summary is requested, **Then** the aggregate and per-revision scopes identify their exact endpoints.
2. **Given** binary files, renames or oversized content, **When** diff is requested, **Then** representation and truncation are explicit; incomplete output is not presented as complete.
3. **Given** unsnapshotted jj edits, **When** stored stack diff is requested, **Then** the result does not claim to include those disk edits.

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

- Merge commits, disconnected histories, multiple roots/bases, divergent jj changes and immutable revisions.
- Renames, binary files, unusual filenames, output limits and concurrent ref/operation changes.

## Requirements

### Functional Requirements

- **FR-001**: Require an explicit or unambiguously declared base and report the selected boundaries and native identities.
- **FR-002**: Preserve jj change versus commit identities, divergent versions and bookmark multiplicity; preserve Git topology and branch relationships.
- **FR-003**: Return aggregate/per-revision file summaries and optional bounded diffs with explicit endpoints, binary/rename handling and completeness.
- **FR-004**: Return explicit ambiguity/unsupported topology rather than choosing an arbitrary stack or relying on reference names as proof.
- **FR-005**: Inspect stored history without snapshots, checkout changes, fetches or external diff/helper execution.
- **FR-006**: Support native Git and jj as Tier 1 peers for declared capabilities; preserve distinct identities and explicit capability gaps, refuse ambiguous/GitButler ownership and never substitute raw Git writes for jj.
- **FR-007**: Provide understandable human output and versioned structured success/error results from the same observations, preserving published contracts; no interactive prompt, editor or pager is required.
- **FR-008**: Declare and enforce observation/action scope, output and time limits, cancellation and incomplete-result behavior; never convert missing or truncated evidence into success or permission to mutate.
- **FR-009**: Require acceptance evidence for minimum/current supported Git and jj, including failure, concurrency, ownership and effects cases; missing mandatory coverage cannot count as passed conformance.

### Key Entities

- Stack observation: declared base, ordered/graph relationships, native revisions and ambiguity.
- Diff observation: endpoints, scope, files and completeness.

## Success Criteria

### Measurable Outcomes

- **SC-001**: Every acceptance scenario passes for each declared supported backend/version; unsupported cases return explicit capability limits rather than fabricated success.
- **SC-002**: Every safety/failure fixture preserves state outside declared effects, executes zero undeclared helpers/network actions and reports incomplete or partial outcomes accurately.
- **SC-003**: Every complete structured success/error fixture validates against its documented contract; all existing published-contract regression fixtures remain valid.
- **SC-004**: Every functional requirement has a traceable acceptance case before implementation handoff; no missing mandatory execution is counted as verified.

## Assumptions

- Depends on Spec 008 identity and Spec 009 inventory/reference evidence.
- Stack edits, restacking, publication and live working-directory diffs are excluded.
- This draft does not amend the constitution or authorize execution. Owner review, plan, tasks and consistency analysis precede implementation; later mutation/forge scope must be reconciled with constitutional constraints during planning.
- Sampled state cannot promise a transaction or detect every concurrent/ABA change; limitations must remain explicit and uncertainty never becomes cleanup proof.
