# Feature Specification: Explicit Upstream Synchronization

**Feature Branch**: `codex/019-upstream-sync-spec` (specification only)
**Created**: 2026-10-15
**Status**: Draft — planning, tasks, cross-artifact analysis and implementation pending
**Input**: Adopt the user-selected Git Toolbelt use case with native Git/jj semantics.
**Priority rating**: 7/10 (product priority, not acceptance evidence)

## User Scenarios & Testing

### User Story 1 — Review an upstream update (Priority: P1)

A human or agent needs an explicit, bounded outcome without disturbing unrelated work.

**Why this priority**: This closes a user-selected everyday workflow gap while retaining ownership and recovery guarantees.

**Independent Test**: Exercise supported success, refusal and interrupted cases in temporary Git and jj fixtures and compare relevant before/after state.

**Acceptance Scenarios**:

1. **Given** an explicit remote and selected reference/upstream pairs, **When** sync is planned, **Then** the caller sees sampled identities, freshness, proposed fetch effects and candidate fast-forward updates.
2. **Given** missing/ambiguous upstreams or divergent refs, **When** sync is planned, **Then** affected references are retained with reasons, not guessed or rewritten.
3. **Given** an owned or checked-out target, **When** sync is planned, **Then** the target is retained and no workspace update is substituted.

### User Story 2 — Fetch and update only authorized targets (Priority: P1)

A human or agent needs an explicit, bounded outcome without disturbing unrelated work.

**Why this priority**: This closes a user-selected everyday workflow gap while retaining ownership and recovery guarantees.

**Independent Test**: Exercise supported success, refusal and interrupted cases in temporary Git and jj fixtures and compare relevant before/after state.

**Acceptance Scenarios**:

1. **Given** explicit authorization for selected network/local effects, **When** fetch runs, **Then** fetched state is recorded and a bound post-fetch update plan is presented for authorization.
2. **Given** an authorized unchanged post-fetch plan, **When** local updates apply, **Then** only supported selected fast-forward updates occur with recovery and outcomes.
3. **Given** remote changes, partial fetch, cancellation or concurrent ownership changes, **When** sync stops, **Then** stale updates refuse and retained/completed state is explicit.

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

- Multiple/fork remotes, missing upstreams, force-updated remote histories and changed bookmark tracking.
- Credentials/rate limits, partial network outcomes, divergent jj operations and newly checked-out targets.

## Requirements

### Functional Requirements

- **FR-001**: Require explicit remote and local/upstream selections with provenance; no assumptions about origin, main or master and no blanket update-all default.
- **FR-002**: Separate network fetch effects, including remote-tracking/object metadata writes, from local branch/bookmark updates; authorize each phase explicitly.
- **FR-003**: Bind local update plans to actual post-fetch evidence; initial plans cannot pre-authorize unknown fetched target identities. A refreshed plan requires new authorization.
- **FR-004**: Allow only declared supported fast-forward updates; retain divergent, foreign-owned, workspace-used or checked-out targets and unknown/unsupported states.
- **FR-005**: Preserve native jj bookmark tracking/import/operation semantics and Git remote-tracking semantics; refuse unsupported native effects and never silently use Git writes for jj.
- **FR-006**: Validate remote configuration without executing repository-provided helpers, hooks or arbitrary commands; disclose permitted authentication/transport effects and refuse unsupported transports.
- **FR-007**: Use Spec 010 state validation/recovery and report partial fetch/update outcomes. No pruning, remote deletion, automatic rebase/merge, workspace refresh, push or force-push.
- **FR-008**: Support native Git and jj as Tier 1 peers for declared capabilities; preserve distinct identities and explicit capability gaps, refuse ambiguous/GitButler ownership and never substitute raw Git writes for jj.
- **FR-009**: Provide understandable human output and versioned structured success/error results from the same observations, preserving published contracts; no interactive prompt, editor or pager is required.
- **FR-010**: Declare and enforce observation/action scope, output and time limits, cancellation and incomplete-result behavior; never convert missing or truncated evidence into success or permission to mutate.
- **FR-011**: Require acceptance evidence for minimum/current supported Git and jj, including failure, concurrency, ownership and effects cases; missing mandatory coverage cannot count as passed conformance.

### Key Entities

- Synchronization selection: explicit remote, local/upstream identity and tracking provenance.
- Post-fetch plan: observed remote state, allowed fast-forward targets, retained targets and explicit effects.

## Success Criteria

### Measurable Outcomes

- **SC-001**: Every declared supported acceptance case passes for minimum/current Git and jj; missing mandatory execution blocks acceptance.
- **SC-002**: Every stale, ambiguous, foreign-owned or unsupported fixture refuses affected actions and preserves unrelated files, references and metadata.
- **SC-003**: Every success, refusal, cancellation and partial-failure fixture reports accurate outcomes; structured fixtures validate and existing published contracts remain compatible.
- **SC-004**: Every consequential action has retained recovery evidence before changing state; all recovery tests achieve exactly the documented scope without claiming remote/hook/untracked restoration.

## Assumptions

- Depends on Specs 009 and 010; authentication is caller-supplied and local features remain available offline.
- Remote writes/publication are excluded. Fetch may change local remote-tracking metadata only within its disclosed scope.
- Depends on the shared mutation contract in Spec 010. This draft grants no execution authority; owner review, planning, tasks and consistency analysis precede implementation.
- Native Git and jj differ; no synthetic staging area or Git fallback is required for jj. GitButler-managed writes remain unsupported.
- Sampled preconditions are not transactional and cannot detect every ABA change; disclose limitations and refuse uncertain actions.
