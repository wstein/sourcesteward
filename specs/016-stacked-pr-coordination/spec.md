# Feature Specification: Stacked PR Coordination

**Feature Branch**: `codex/lazyjj-inspired-roadmap` (specification batch; implementation branches deferred)
**Created**: 2026-10-15
**Status**: Draft — specification only; planning, tasks, cross-artifact analysis and implementation pending
**Input**: Preview dependencies and base updates; explicitly authorize remote effects.
**Priority rating**: 8/10 (user-selected product priority, not verification evidence)

## User Scenarios & Testing

### User Story 1 — Review forge relationships (Priority: P1)

A caller maps a local stack to its pull requests and dependencies.

**Why this priority**: Establishes the primary safe workflow before optional integrations.

**Independent Test**: Exercise the following cases in separate temporary Git and jj repositories; compare declared outcomes and relevant before/after state.

**Acceptance Scenarios**:

1. **Given** an explicit forge/repository and local stack, **When** coordination is inspected, **Then** verified PR heads, bases, statuses and dependencies are shown with observation time.
2. **Given** missing credentials, ambiguous matches or unavailable forge, **When** inspection runs, **Then** local stack information remains available and remote uncertainty is explicit.

### User Story 2 — Publish only authorized actions (Priority: P2)

A caller reviews and authorizes remote effects separately.

**Why this priority**: Adds useful handoff or coordination without weakening the primary workflow.

**Independent Test**: Exercise the following cases in separate temporary Git and jj repositories; compare declared outcomes and relevant before/after state.

**Acceptance Scenarios**:

1. **Given** a proposed stack update, **When** a publication plan is requested, **Then** pushes, new refs, PR creation, base edits and comments appear as individually selectable effects.
2. **Given** unchanged remote/local evidence and explicit authorization, **When** selected actions run, **Then** each result records its remote identity and partial outcomes without duplicating completed work on retry.
3. **Given** changed heads/bases, insufficient permissions or policy restrictions, **When** apply runs, **Then** affected actions refuse and no force-push, merge or remote deletion is substituted.

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

- PRs merged out of order, rewritten heads, deleted branches, forks and cross-repository bases.
- Rate limits, credential expiry, concurrent PR edits and network failure after an action may already have succeeded.

## Requirements

### Functional Requirements

- **FR-001**: Use explicit forge/repository selection and verified head/base identities; never infer PR linkage solely from matching names.
- **FR-002**: Keep local stack inspection independent of authentication; remote reads must be explicitly requested, bounded and reported as network effects.
- **FR-003**: Preview dependency order, PR bases and all proposed remote effects; local mutation authorization does not authorize pushes, PR edits or comments.
- **FR-004**: Revalidate relevant local and remote evidence and authorization before each external action; refuse stale plans and preserve per-action audit outcomes.
- **FR-005**: Support resumable partial outcomes with duplicate prevention; report remote recovery limits instead of claiming local undo reverses published effects.
- **FR-006**: Do not merge PRs, delete remote refs, force-push, enable auto-merge or alter protections in this slice.
- **FR-007**: Start with GitHub as an explicitly declared forge capability; preserve native Git/jj stack evidence and disclose unsupported forge/backend combinations.
- **FR-008**: Support native Git and jj as Tier 1 peers for declared capabilities; preserve distinct identities and explicit capability gaps, refuse ambiguous/GitButler ownership and never substitute raw Git writes for jj.
- **FR-009**: Provide understandable human output and versioned structured success/error results from the same observations, preserving published contracts; no interactive prompt, editor or pager is required.
- **FR-010**: Declare and enforce observation/action scope, output and time limits, cancellation and incomplete-result behavior; never convert missing or truncated evidence into success or permission to mutate.
- **FR-011**: Require acceptance evidence for minimum/current supported Git and jj, including failure, concurrency, ownership and effects cases; missing mandatory coverage cannot count as passed conformance.

### Key Entities

- PR relationship: forge/repository/PR identity, exact head/base and sampled status.
- Publication action: selected remote effect, expected state, authorization scope and durable outcome.

## Success Criteria

### Measurable Outcomes

- **SC-001**: Every acceptance scenario passes for each declared supported backend/version; unsupported cases return explicit capability limits rather than fabricated success.
- **SC-002**: Every safety/failure fixture preserves state outside declared effects, executes zero undeclared helpers/network actions and reports incomplete or partial outcomes accurately.
- **SC-003**: Every complete structured success/error fixture validates against its documented contract; all existing published-contract regression fixtures remain valid.
- **SC-004**: Every functional requirement has a traceable acceptance case before implementation handoff; no missing mandatory execution is counted as verified.

## Assumptions

- Depends on Specs 010 and 012; does not automatically restack or resolve conflicts.
- Remote reads/writes require caller credentials and task authorization; no model, forge or account is required for local features.
- This draft does not amend the constitution or authorize execution. Owner review, plan, tasks and consistency analysis precede implementation; later mutation/forge scope must be reconciled with constitutional constraints during planning.
- Sampled state cannot promise a transaction or detect every concurrent/ABA change; limitations must remain explicit and uncertainty never becomes cleanup proof.
