# Feature Specification: Amend, Fix Up and Split Changes

**Feature Branch**: `codex/018-change-editing-spec` (specification only)
**Created**: 2026-10-15
**Status**: Draft — planning, tasks, cross-artifact analysis and implementation pending
**Input**: Adopt the user-selected Git Toolbelt use case with native Git/jj semantics.
**Priority rating**: 9/10 (product priority, not acceptance evidence)

## User Scenarios & Testing

### User Story 1 — Amend a chosen change (Priority: P1)

A human or agent needs an explicit, bounded outcome without disturbing unrelated work.

**Why this priority**: This closes a user-selected everyday workflow gap while retaining ownership and recovery guarantees.

**Independent Test**: Exercise supported success, refusal and interrupted cases in temporary Git and jj fixtures and compare relevant before/after state.

**Acceptance Scenarios**:

1. **Given** an explicit revision and selected content, **When** amend/fixup is planned, **Then** the target, content, descendants, references and author/message changes are shown.
2. **Given** an authorized unchanged plan, **When** editing applies, **Then** only selected content changes and all rewritten native identities are recorded.
3. **Given** immutable, published/protected or foreign-owned affected state, **When** editing is requested, **Then** unsupported unsafe scope refuses without hook bypass or autostash.

### User Story 2 — Split work into atomic changes (Priority: P1)

A human or agent needs an explicit, bounded outcome without disturbing unrelated work.

**Why this priority**: This closes a user-selected everyday workflow gap while retaining ownership and recovery guarantees.

**Independent Test**: Exercise supported success, refusal and interrupted cases in temporary Git and jj fixtures and compare relevant before/after state.

**Acceptance Scenarios**:

1. **Given** a selected change and explicit content partitions, **When** split is planned, **Then** partition order, messages and any remainder are shown without silently dropping content.
2. **Given** valid unchanged partitions, **When** split applies, **Then** the ordered result reproduces the original selected net content and preserves unselected work.
3. **Given** overlapping/missing partitions or conflict during rewrite, **When** split runs, **Then** ambiguity refuses or partial outcomes expose conflicts and recovery.

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

- Empty partitions, overlapping hunks, binary/rename edits, merge commits and message/author preservation.
- Shared descendants, immutable jj changes, staged/disk differences and partial rewrite interruption.

## Requirements

### Functional Requirements

- **FR-001**: Specify amend, fixup and split as explicit native change-editing operations separate from restacking; require exact targets and selected content.
- **FR-002**: Support explicit noninteractive content partitions with ordering, messages and remainder handling. Reject ambiguous/overlapping selection; unsupported hunk/binary operations must be explicit.
- **FR-003**: Define Git fixup-commit creation versus autosquash rewrite and jj native change amendment/squash separately; no interactive picker or synthetic jj staging requirement.
- **FR-004**: Preview all affected descendants, references, authorship/message and disk/index/snapshot/hook effects; use Spec 010 expected-state validation and recorded recovery.
- **FR-005**: Preserve selected aggregate content and all unrelated work; require equality evidence for supported split/rewrite outcomes and report intentional explicit content changes separately.
- **FR-006**: Respect immutable/protected/shared ownership; no implicit autostash, reset, hook bypass, force-push or automatic resolution. Report conflicts, cancellation and old/new native mappings.
- **FR-007**: Support native Git and jj as Tier 1 peers for declared capabilities; preserve distinct identities and explicit capability gaps, refuse ambiguous/GitButler ownership and never substitute raw Git writes for jj.
- **FR-008**: Provide understandable human output and versioned structured success/error results from the same observations, preserving published contracts; no interactive prompt, editor or pager is required.
- **FR-009**: Declare and enforce observation/action scope, output and time limits, cancellation and incomplete-result behavior; never convert missing or truncated evidence into success or permission to mutate.
- **FR-010**: Require acceptance evidence for minimum/current supported Git and jj, including failure, concurrency, ownership and effects cases; missing mandatory coverage cannot count as passed conformance.

### Key Entities

- Edit plan: native target, operation semantics, content selection, partitions and affected descendants.
- Edit outcome: new identities, aggregate-content comparison, remainder, conflicts and recovery.

## Success Criteria

### Measurable Outcomes

- **SC-001**: Every declared supported acceptance case passes for minimum/current Git and jj; missing mandatory execution blocks acceptance.
- **SC-002**: Every stale, ambiguous, foreign-owned or unsupported fixture refuses affected actions and preserves unrelated files, references and metadata.
- **SC-003**: Every success, refusal, cancellation and partial-failure fixture reports accurate outcomes; structured fixtures validate and existing published contracts remain compatible.
- **SC-004**: Every consequential action has retained recovery evidence before changing state; all recovery tests achieve exactly the documented scope without claiming remote/hook/untracked restoration.

## Assumptions

- Depends on Specs 010 and 012; recovery semantics reuse Spec 013 and conflict diagnosis uses Spec 014.
- This is local editing only. Restacking onto another base remains Spec 015; publication remains Spec 016.
- Depends on the shared mutation contract in Spec 010. This draft grants no execution authority; owner review, planning, tasks and consistency analysis precede implementation.
- Native Git and jj differ; no synthetic staging area or Git fallback is required for jj. GitButler-managed writes remain unsupported.
- Sampled preconditions are not transactional and cannot detect every ABA change; disclose limitations and refuse uncertain actions.
