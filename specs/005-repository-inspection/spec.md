# Feature Specification: Bounded Repository Inspection

**Branch**: `codex/005-repository-inspection` | **Created**: 2026-10-15
**Status**: Implemented and independently verified
**Input**: Implement bounded read-only inspection with Git/jj/GitButler detection, analysis and review.

## User Scenarios & Testing

### User Story 1 — Identify an ordinary repository (Priority: P1)
A human or agent needs repository and workspace identity before considering later operations.
The inspection identifies an ordinary Git repository from a path or the current directory.

**Independent test**: Inspect temporary normal, linked, bare and unborn repositories and compare human/structured results.

**Acceptance Scenarios**:
1. Given a normal repository or nested directory, inspection reports absolute repository/workspace metadata paths and attached HEAD identity.
2. Given a linked worktree, inspection distinguishes its private metadata directory from shared metadata and reports its workspace.
3. Given a bare repository, inspection reports no working tree.
4. Given an unborn or detached HEAD, inspection distinguishes that state without inventing a commit or branch. Only an absent symbolic branch is unborn; corrupt references and missing/noncommit HEAD objects are failures.

### User Story 2 — Respect managed workspace ownership (Priority: P1)
A caller needs detection evidence and an explicit unsupported outcome when jj or GitButler owns the workspace.
Detection is not an adapter, migration or permission to fall back to Git operations.

**Independent test**: Synthetic metadata fixtures exercise jj, GitButler, coexistence and misleading markers.

**Acceptance Scenarios**:
1. Given recognized jj metadata, inspection reports jj evidence and unsupported inspection without executing jj or snapshotting.
2. Given recognized GitButler metadata or references, inspection reports GitButler evidence and unsupported inspection.
3. Given both marker families, inspection reports ambiguity, preserves evidence and chooses no backend.
4. Given malformed/inaccessible metadata or insufficient evidence, inspection fails explicitly rather than guessing ordinary Git.

### User Story 3 — Obtain trustworthy bounded results (Priority: P1)
A caller needs a stable machine contract, no repository changes and clear failures.

**Independent test**: Compare before/after repository bytes, hook/network sentinels, cancellation and changed-HEAD fixtures.

**Acceptance Scenarios**:
1. Given unusual valid paths, structured output preserves their exact characters and human output escapes terminal controls.
2. Given hostile inherited repository/configuration/trace routing variables, inspection targets the requested repository without creating trace files.
3. Given concurrent HEAD changes, missing tools, unsafe ownership, invalid paths or cancellation, inspection returns a stable operational failure and no misleading success.

## Requirements

### Functional Requirements
- **FR-001**: Expose `stw inspect [PATH] [--json]`, defaulting to the current directory; reject extra positional arguments and unknown options.
- **FR-002**: Report schema version, selected backend/detection evidence, absolute input path, workspace root when present, metadata directory, shared metadata directory, bare/linked state, object format and HEAD state for supported ordinary Git repositories.
- **FR-003**: HEAD distinguishes attached, detached and unborn; attached/unborn include a full reference, attached/detached include an object identifier, and unavailable values are explicit nulls.
- **FR-004**: Human and JSON outcomes derive from the same typed inspection result. Success exits 0, operational/unsupported outcomes exit 1, usage errors exit 2. Usage errors have empty stdout and one Error diagnostic. Operational JSON failures emit one versioned result plus one concise Error diagnostic; human failures emit only that diagnostic. Cancellation or broken output must not emit a contradictory success result.
- **FR-005**: Detect jj and GitButler metadata conservatively, include evidence, and report unsupported or ambiguous ownership rather than claiming native support.
- **FR-006**: Perform no repository/filesystem writes, index refresh, working-copy snapshots, hooks, prompts, editors, pagers, remotes or network operations. Do not enumerate changed or untracked files.
- **FR-007**: Prevent inherited Git routing/configuration/trace variables from redirecting or adding effects; retain normal ownership protection and do not whitelist unsafe repositories.
- **FR-008**: Propagate cancellation, bound subprocess output and reject changing HEAD observations. Never expose raw subprocess stderr, configuration values or file content in diagnostics.
- **FR-009**: Preserve spaces, newlines and Unicode in structured paths; quote control characters in human output. Invalid UTF-8 paths produce a documented error rather than lossy identity.
- **FR-010**: Update guidance, architecture and feature evidence accurately; inspection does not add cleanup, mutation, status enumeration or native jj/GitButler adapters.

### Key Entities
- **Inspection result**: Versioned outcome, optional repository identity, backend evidence and stable diagnostics.
- **Repository identity**: Metadata/common/workspace paths, object format, bare/linked classification and observed HEAD.
- **Backend evidence**: Recognized marker type and absolute marker location; evidence indicates detection, not validated backend health.

## Success Criteria
- **SC-001**: All normal, linked, bare, unborn and detached fixtures report correct identity in human and structured modes.
- **SC-002**: Every managed or ambiguous fixture is refused consistently with preserved detection evidence.
- **SC-003**: Every successful and failing fixture leaves repository byte snapshots unchanged and triggers no hook/network/trace sentinel.
- **SC-004**: Every usage, cancellation, ownership, malformed metadata and concurrent HEAD case has the documented exit class and diagnostic.
- **SC-005**: Full source checks and independent analysis/review pass before handoff, with actual evidence and limitations recorded.

## Assumptions and Exclusions
Paths identify local repositories; the tool never initializes or migrates one. Git must already be installed and support the documented identity queries. The implementation targets Git 2.46+; incompatible versions fail explicitly.
Detection is evidence-based and cannot prove absence of every third-party workspace manager, including unregistered GitButler single-branch mode. A nested Git repository bounds ancestor jj discovery; same-directory jj metadata remains ownership evidence. Marker symlinks/wrong types/inaccessibility fail conservatively.
Concurrent sampling detects HEAD changes only, not a transaction, metadata routing/evidence races or protection against ABA changes.
No status/index/file enumeration, branch cleanup, remote discovery, mutation, recovery or backend adapter implementation is included.
