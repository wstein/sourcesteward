# Feature Specification: Native jj Identity Inspection

**Feature Branch**: `codex/008-jj-inspection`
**Created**: 2026-10-15
**Status**: Draft for pre-implementation analysis
**Input**: Implement Tier 1 Git/jj policy and native jj inspection with compatible schemas and conformance CI.

## User Scenarios & Testing

### User Story 1 — Inspect either Tier 1 backend (Priority: P1)
A human or agent identifies a repository through one command while preserving each backend's native identities.

**Why this priority**: Native jj support closes the gap between the Tier 1 promise and actual use.
**Independent Test**: Inspect ordinary Git and temporary jj standalone, colocated and secondary workspaces, including nested paths.

**Acceptance Scenarios**:
1. Given plain Git with jj installed, inspection still selects Git and preserves its existing result.
2. Given a valid jj workspace, inspection selects jj and reports workspace, repository, working-copy change, commit and operation identity without treating change IDs as commits or bookmarks as branches.
3. Given a secondary jj workspace, inspection reports its own workspace identity and shared repository identity.
4. Given missing or incompatible jj, malformed ownership metadata or conflicting manager evidence, inspection fails clearly and never substitutes Git inspection.

### User Story 2 — Inspect without changing state (Priority: P1)
A caller needs identity evidence without snapshots, automatic operation merging or changes to local files.

**Why this priority**: Agents must be able to inspect repositories owned by humans and other agents safely.
**Independent Test**: Compare complete fixture file/metadata contents before and after success and failure, including stale working copies and divergent operation heads.

**Acceptance Scenarios**:
1. Given edited or untracked working-copy files, inspection leaves files, stored working-copy state and operation heads unchanged and states that disk changes were not snapshotted.
2. Given stale working-copy state or divergent operations, inspection either returns an explicitly qualified stored-state observation or an actionable failure; it never updates the workspace or merges operations.
3. Given hostile inherited configuration, aliases, helper settings or routing variables, inspection invokes no helper, editor, pager, prompt, hook or network action and creates no trace file.
4. Given cancellation, malformed output or a changed identity observation, inspection fails within bounded output/process limits and emits no misleading success.

### User Story 3 — Consume stable contracts with conformance evidence (Priority: P1)
Automation validates backend-specific results and maintainers verify equal support commitments continuously.

**Why this priority**: Tier 1 requires versioned agent contracts and mandatory acceptance evidence for both backends.
**Independent Test**: Validate real CLI success/error output against published schemas and run backend fixtures against pinned minimum/current stable releases.

**Acceptance Scenarios**:
1. Given existing Git/version consumers, their v1 schemas and output remain unchanged.
2. Given selected jj success or operational failure, one documented versioned result validates, preserving distinct identities and snapshot limitations.
3. Given invalid identity/result combinations, schema validation fails rather than accepting misleading output.
4. Given an unavailable mandatory backend/version or missing fixture execution in CI, conformance fails rather than silently skipping.

### Edge Cases
- Nested unrelated Git repositories bound ancestor ownership; same-directory jj ownership takes precedence over colocated Git.
- Symlink/wrong-type/inaccessible manager markers, invalid UTF-8, control characters and unusual workspace names fail safely or retain exact valid identity.
- GitButler and jj evidence together is ambiguous; GitButler alone remains explicitly unsupported.
- Stale workspaces, multiple operation heads, corrupt metadata, missing tools and unsupported backend versions are explicit cases.
- Sampling cannot guarantee a transaction or detect ABA changes; stored identity does not claim current disk content.

## Requirements

### Functional Requirements
- **FR-001**: Preserve `stw inspect [PATH] [--json]`, default current directory, help locality and existing usage rules.
- **FR-002**: Select Git or native jj through ownership evidence, preserving nested repository boundaries; never initialize/migrate, automatically switch backends or use raw Git as a jj fallback.
- **FR-003**: Report native jj absolute input/workspace/metadata/shared-repository paths, workspace name, full working-copy change ID, full commit ID and full operation ID as distinct values.
- **FR-004**: Report that jj inspection reads stored state without snapshotting; do not imply the observed commit contains current edited/untracked disk content. Disclose stale/concurrent observation limits.
- **FR-005**: Perform no writes to the inspected repository/workspace or its metadata, snapshots, operation merging, Git import/export, workspace updates, hooks, network, prompts, pagers or editors; do not enumerate file status or history.
- **FR-006**: Preserve conservative GitButler refusal and ambiguous-ownership handling; missing, incompatible or invalid jj ownership/tool state produces a stable actionable error with evidence.
- **FR-007**: Bound subprocess output, cancellation and process lifetime; reject malformed/invalid UTF-8 output and changed observed identity. Repository content and raw tool stderr must not become executable instructions or leaked diagnostics. Private temporary isolation/probe files outside the target may be created and cleaned up; disclose this local effect.
- **FR-008**: Preserve published v1 Git inspection/version schemas and existing Git/version output. Introduce a new version only for jj result alternatives; publish its stable schema URL and validate real success/error output offline.
- **FR-009**: Human and JSON outputs derive from one result, preserve valid identity characters, quote terminal controls, and retain existing exit classes: success 0, operational failure 1, usage 2. Usage/output failures do not promise complete JSON.
- **FR-010**: Run mandatory real-fixture conformance for pinned minimum and current stable Git/jj releases in CI; missing tools, unexpected versions and absent fixture execution must fail. Test both working-copy and operation-store effects, including divergent heads.
- **FR-011**: Update backend guidance, generated command reference, schema publication/checks and evidence accurately; run pre-implementation analysis, tests first and independent source review before PR handoff.

### Key Entities
- **Ownership evidence**: Recognized local manager markers and references determining the selected backend or ambiguity.
- **jj identity**: Workspace and shared repository paths plus independent change, commit and operation identifiers.
- **Observation qualification**: Stored-state/snapshot policy and limitations associated with identity evidence.
- **Conformance case**: An actual temporary repository scenario, pinned backend version, expected outcome and before/after effect evidence.

## Success Criteria

### Measurable Outcomes
- **SC-001**: Every supported Git/jj layout fixture reports the correct backend and native identity in both output modes.
- **SC-002**: Every success/failure conformance case leaves fixture files and repository state unchanged and triggers zero effect sentinels.
- **SC-003**: All existing Git/version contract fixtures remain valid and unchanged; every real jj JSON result validates, while independently authored invalid combinations fail.
- **SC-004**: Both minimum/current backend versions execute mandatory acceptance scenarios in CI, with no skipped run counted as conformance.
- **SC-005**: Requirement/task coverage analysis and independent review have no unresolved blocking findings; evidence distinguishes local, CI and publication results.

## Assumptions
- The Tier 1 policy amendment is a separately reviewed dependency; equal support commitments preserve backend-specific semantics.
- This slice is local identity inspection only. Status enumeration, mutation, recovery, migration, remote management and GitButler adapters remain out of scope.
- Callers supply local paths, installed supported backends and supported enforced read-only execution capability; future releases are accepted only after capability checks, without claiming untested conformance.
- Repeated sampling is not transactional; stored-state inspection cannot certify disk freshness or detect every concurrent/ABA change.
