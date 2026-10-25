# Feature Specification: Checkpoint, Resume and Selective Undo

**Feature Branch**: `codex/spec-013-stack-toolbelt` (specification only)
**Created**: 2026-10-15
**Status**: Draft — complete proposed design artifacts, 2026-10-25; owner decisions, independent review and implementation acceptance pending. No checkpoint/resume/undo command or JSON contract is published by this amendment.
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

### User Story 4 — Undo an explicit local operation selectively (Priority: P1)

A human or agent needs an explicit, bounded outcome without disturbing unrelated work.

**Why this priority**: This closes a user-selected everyday workflow gap while retaining ownership and recovery guarantees.

**Independent Test**: Exercise supported success, refusal and interrupted cases in temporary Git and jj fixtures and compare relevant before/after state.

**Acceptance Scenarios**:

1. **Given** a selected commit, merge or native operation, **When** undo is planned, **Then** the caller chooses inverse change or supported history restoration and sees files, references, descendants and retained recovery scope.
2. **Given** a merge with ambiguous parent or unrelated concurrent edits, **When** undo is requested, **Then** explicit parent/semantics selection is required and unsafe replacement refuses.
3. **Given** an authorized unchanged plan with recovery evidence, **When** undo applies, **Then** only selected effects occur, partial outcomes are recorded and remote or hook effects are not claimed reversed.

### Edge Cases

- Staged versus unstaged edits, untracked/ignored files, binary content, symlinks and deleted files.
- Expired objects, divergent operations, changed workspace ownership and partial capture/restore failures.

- Published/shared history, merge parent ambiguity, expired operation/object retention and inverse changes that create conflicts.

## Requirements

### Functional Requirements

- **FR-001**: Reuse Spec 010 mutation/ownership contracts and Spec 032 recovery durability conventions through a reviewed typed integration. Checkpoint capture defaults to none; require explicit path/state selection and show included/excluded content. Untracked and ignored content require separate explicit selection and a supported capture capability; journal metadata is not a saved file payload.
- **FR-002**: Preserve native semantics: Git index/worktree state and jj working-copy/change/operation state are not interchangeable.
- **FR-003**: Record durable checkpoint identity, exact selected content and integrity evidence, exclusions, retention and recovery limits before acknowledging success. Partial capture is incomplete, never a restorable complete checkpoint; protect incomplete recovery records from automatic expiry.
- **FR-004**: Make resume inspection read-only; restoration is a separate authorized plan that refuses changed relevant state and never silently discards current work.
- **FR-005**: Keep task context bounded and caller-provided; never execute stored text or automatically capture credentials, environment dumps or agent transcripts.
- **FR-006**: Report partial outcomes and backend-specific unsupported recovery; a jj operation ID alone does not promise restoration of disk edits or external effects.
- **FR-007**: Support native Git and jj as Tier 1 peers for declared capabilities; preserve distinct identities and explicit capability gaps, refuse ambiguous/GitButler ownership and never substitute raw Git writes for jj.
- **FR-008**: Provide understandable human output and versioned structured success/error results from the same observations, preserving published contracts; no interactive prompt, editor or pager is required.
- **FR-009**: Declare and enforce observation/action scope, output and time limits, cancellation and incomplete-result behavior; never convert missing or truncated evidence into success or permission to mutate. Remain offline with no repository helper, hook, model or stored-text execution; stop before further effects after cancellation and report effects already completed or uncertain.
- **FR-010**: Require acceptance evidence for minimum/current supported Git and jj, including failure, concurrency, ownership and effects cases; missing mandatory coverage cannot count as passed conformance.

- **FR-011**: Extend recovery beyond checkpoints with explicit local commit/merge/native-operation undo plans, reusing Spec 010 expected-state and authorization contracts.
- **FR-012**: Separate creation of an inverse change from history/operation restoration; never choose between these semantics implicitly. Require an explicit mainline/parent when merge reversal is ambiguous.
- **FR-013**: Enumerate affected files, index/disk state, references and rewritten descendants before apply; retain pre-action native identities and selected bytes needed by the declared recovery promise. Refuse shared/foreign-owned or changed relevant state, unknown ownership and unselected descendant effects; restoration is separately authorized and never an automatic rollback.
- **FR-014**: Respect jj change/operation semantics and Git commit/merge semantics; report unsupported selective recovery and never use broad operation restoration as a substitute for an unrequested selective undo.

### Key Entities

- Checkpoint: retained native identities, content scope, task context and recovery limits.
- Resume comparison: checkpoint/current differences and safe next actions.

- Selective undo plan: selected operation/revision, reversal semantics, parent choice, affected files/references/descendants and recovery.

## Success Criteria

### Measurable Outcomes

- **SC-001**: Every acceptance scenario passes for each declared supported backend/version; unsupported cases return explicit capability limits rather than fabricated success.
- **SC-002**: Every safety/failure fixture preserves state outside declared effects, executes zero undeclared helpers/network actions and reports incomplete or partial outcomes accurately.
- **SC-003**: Every complete structured success/error fixture validates against its documented contract; all existing published-contract regression fixtures remain valid.
- **SC-004**: Every functional requirement has a traceable acceptance case before implementation handoff; no missing mandatory execution is counted as verified.

## Assumptions

- Depends on Specs 009 and 010 for ownership/state/expected-state vocabulary and Spec 032 for the existing sync journal durability pattern. The current sync record is not a generic checkpoint store; a proposed typed integration and a distinct selected-payload design are required. Stack observation (012), integration preflight (014) and local restack (015) are optional evidence with fresh consumer validation.
- Checkpoints are local and explicit; automatic background snapshots, cloud synchronization and remote rollback are excluded.
- This draft does not amend the constitution or authorize execution. Owner review, plan, tasks and consistency analysis precede implementation; later mutation/forge scope must be reconciled with constitutional constraints during planning.
- Sampled state cannot promise a transaction or detect every concurrent/ABA change; limitations must remain explicit and uncertainty never becomes cleanup proof.

- Selective local undo is newly in scope; it neither reverses remote publication nor supplies authorization to discard current work.

## Needs owner decision

The OQ-1 through OQ-5 table in [research](research.md#needs-owner-decision)
records recommendations for native capture, undo, payload storage/retention,
trusted ownership and typed journal integration. These are deliberately unsettled;
no affected mutation capability may be enabled until its choice and native
evidence are accepted. Other design work and read-only comparison can proceed
within already settled constraints.

## Design and implementation boundary (2026-10-25)

[Plan](plan.md), [research](research.md), [data model](data-model.md),
[contracts](contracts/checkpoint.md), [quickstart](quickstart.md),
[tasks](tasks.md) and [analysis](analysis.md) complete the proposed artifact set.
All new contracts are UNPUBLISHED until implementation and native conformance.
The requirements describe supported capabilities, not a promise that every Git
or jj state is already capturable or undoable. Backend capture/undo admission,
payload storage/retention, trusted ownership and the journal integration remain
explicit **Needs owner decision** items in research.md. No recommendation is
owner approval; affected implementation tasks remain blocked and unchecked.

Existing Spec 032 sync stores `sync` v1 records and committed-history recovery
roots, not selected worktree/index payloads. Its ten-finished-run retention
(no prepared entries; failed/restored may count) and narrow restore behavior
are unchanged. Resume inspection is offline and
read-only; every restore or undo needs its own reviewed plan and explicit local
authorization. Local recovery reverses neither publication nor arbitrary hooks,
external effects or unsaved buffers. Spec 016 owns separately authorized forge
effects; Spec 043 is the human owner's separate landing invocation.

A future 013 result proposes exit 0 for complete success/observation, 1 for
refusal/incomplete/partial/cancellation/operational failure, and 2 for usage.
This does not change the hidden Spec 015 v1 planner's existing exit 3 for
incomplete evidence; migration belongs to that contract's owner decision.
