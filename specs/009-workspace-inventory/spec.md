# Feature Specification: Workspace, State and Reference Inventory

**Feature Branch**: `codex/009-inventory-implementation`
**Created**: 2026-10-15
**Status**: Implemented candidate — independent source review cleared; owner review, hosted conformance and merge pending
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

### User Story 4 — Query state without confusing false and failure (Priority: P1)

A human or agent needs an explicit, bounded outcome without disturbing unrelated work.

**Why this priority**: This closes a user-selected everyday workflow gap while retaining ownership and recovery guarantees.

**Independent Test**: Exercise supported success, refusal and interrupted cases in temporary Git and jj fixtures and compare relevant before/after state.

**Acceptance Scenarios**:

1. **Given** a clean workspace, an absent reference and a false ancestry relation, **When** bounded queries run, **Then** each completed predicate returns true or false with native evidence, not an operational error.
2. **Given** unreadable disk state, missing backend or truncated evidence, **When** a query runs, **Then** unknown/unsupported observations carry reasons and execution failures remain distinct from known false.
3. **Given** changed tracked, staged, untracked and ignored files, **When** file-state queries run, **Then** the requested scopes and path filters are explicit and unavailable jj staging is unsupported rather than fabricated.

### User Story 5 — Inspect references and choose a base (Priority: P1)

A human or agent needs an explicit, bounded outcome without disturbing unrelated work.

**Why this priority**: This closes a user-selected everyday workflow gap while retaining ownership and recovery guarantees.

**Independent Test**: Exercise supported success, refusal and interrupted cases in temporary Git and jj fixtures and compare relevant before/after state.

**Acceptance Scenarios**:

1. **Given** multiple remotes, branches/bookmarks and upstreams, **When** inventory runs, **Then** relationships, local-only reachability and sample freshness are reported using native identities.
2. **Given** configured/default trunk candidates that disagree, **When** base discovery runs, **Then** candidate provenance and ambiguity are reported and explicit selection is required.
3. **Given** a revision reachable from selected references, **When** integration inventory runs, **Then** containment and ancestry are reported separately from squash/forge proof and from publication status.

### Edge Cases

- Deleted directories, duplicate or aliased paths, symlinks, inaccessible metadata and unusual filenames.
- Bare/unborn/detached Git, standalone/colocated jj, stale jj workspaces and divergent operations.
- Concurrent workspace registration changes and output exceeding documented limits.

- Multiple/custom remotes, unborn references, absent upstreams, stale tracking refs and multiple trunk candidates.
- False predicates versus backend failures; filenames containing newlines, leading dashes or glob syntax.

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

- **FR-010**: Offer bounded clean/dirty, reference-existence and ancestry predicates with true/false/unknown observations; distinguish completed false, unknown/unsupported and operational failure without changing existing command exit contracts.
- **FR-011**: Enumerate native local references, sampled remote-tracking references and upstream relationships; report references containing a selected revision and ancestry-based merged/unmerged evidence against an explicit base.
- **FR-012**: Define local-only revisions relative to explicitly selected sampled remote reachability; absent upstreams or stale samples never prove that work was never published. No implicit fetch.
- **FR-013**: Discover configured/default base or trunk candidates with provenance and exact identities; require explicit selection when ambiguous and never assume main, master or origin.
- **FR-014**: Expose path-filtered changed-file queries with explicit current-directory/workspace-root anchoring, literal path selection, tracked/staged/disk/untracked/ignored scopes, exclusions and completeness. Preserve unusual names, deletion and rename information; no implicit regex or shell expansion.
- **FR-015**: Keep containment, ancestry, squash equivalence and verified forge integration distinct. Inventory observations do not authorize tidy; unknown observations never become clean or deletable.

### Key Entities

- Workspace observation: native identities, path, manager evidence, registration health and qualified disk state.
- Observation boundary: sampled state, completeness, limits and explicit uncertainty.

- State query: predicate, explicit scope, true/false/unknown observation and separate execution status.
- Reference observation: native reference/revision, upstream, sampled reachability and freshness.
- Base candidate: native identity, configuration/default provenance and ambiguity.

## Success Criteria

### Measurable Outcomes

- **SC-001**: Every acceptance scenario passes for each declared supported backend/version; unsupported cases return explicit capability limits rather than fabricated success.
- **SC-002**: Every safety/failure fixture preserves state outside declared effects, executes zero undeclared helpers/network actions and reports incomplete or partial outcomes accurately.
- **SC-003**: Every complete structured success/error fixture validates against its documented contract; all existing published-contract regression fixtures remain valid.
- **SC-004**: Every functional requirement has a traceable acceptance case before implementation handoff; no missing mandatory execution is counted as verified.

## Assumptions

- Depends on accepted identity inspection (Spec 008) and preserves its published contracts.
- This feature inventories registered workspaces, references and qualified state; task creation, cleanup, stack inference and full filesystem searches are excluded.
- This draft does not amend the constitution or authorize execution. Owner review, plan, tasks and consistency analysis precede implementation; later mutation/forge scope must be reconciled with constitutional constraints during planning.
- Sampled state cannot promise a transaction or detect every concurrent/ABA change; limitations must remain explicit and uncertainty never becomes cleanup proof.

- This extension adds read-only reference/state queries; cleanup still belongs to Spec 011.
