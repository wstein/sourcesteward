# Feature Specification: Agent Hints

**Feature Branch**: `codex/003-agent-hints`
**Created**: 2026-10-15
**Status**: Specified
**Input**: Add LLM hints like SheetMill PR #19, adapted to SourceSteward's current capabilities.

## User Scenarios & Testing

### User Story 1 — Discover guidance from root help (Priority: P1)
A person or AI agent viewing the command overview finds concise, relevant guidance without changing normal command results.

**Why this priority**: Discovery makes the documentation useful at the point of invocation.
**Independent Test**: Exercise every root-help spelling and unrelated command paths with captured output.
**Acceptance Scenarios**:
1. Given `stw`, `stw --help`, `stw -h` or `stw help`, the output includes one notice linking the agent guide and troubleshooting index.
2. Given version output, structured version output, `stw version --help` or `stw help version`, no notice is appended and existing results are preserved.
3. Given a writer that fails while the notice is being written, the invocation reports failure with status 1.

### User Story 2 — Use accurate agent guidance (Priority: P2)
A human or agent can build and invoke the current CLI and decide whether its documentation is relevant to the task.

**Why this priority**: Advice must accurately reflect the foundation rather than advertise future operations.
**Independent Test**: Follow documented build/help/version examples and compare the guide against actual command behavior.
**Acceptance Scenarios**:
1. The guide documents setup, supported help/version commands, structured version output, exit meanings and troubleshooting.
2. The guide explicitly says to use its advice when relevant to the current task and respect caller/repository authority.
3. Repository inspection, cleanup, recovery and Git/jj/GitButler operations remain labeled planned.

### User Story 3 — Locate trustworthy guidance offline (Priority: P3)
Contributors can check guidance destinations without requesting external resources.

**Why this priority**: Broken discovery links should fail source checks reliably.
**Independent Test**: Offline tests validate the notice's raw-source destinations and every local index link, including deliberately broken fixtures.
**Acceptance Scenarios**:
1. Both notice URLs map to existing files in this repository.
2. The root troubleshooting index links to relevant local documentation, and broken links are diagnosed by source checks.
3. README and the knowledge map expose the guide/index without requiring a website deployment change.

### Edge Cases
- Root help must not duplicate the notice through nested help dispatch.
- Failed standard help output must remain a status-1 failure; a later successful write must not mask it.
- Unknown commands, flags and help topics remain usage errors without new guidance or network effects.
- Offline use can read checked-out guide/index files; the CLI never fetches their URLs.

## Requirements

### Functional Requirements
- **FR-001**: Root help MUST append one concise notice with the agent guide and troubleshooting index destinations for all four supported root-help forms.
- **FR-002**: The notice MUST be absent from version/subcommand-help and error paths; structured version output MUST remain unchanged.
- **FR-003**: Any help-output failure, including the appended notice, MUST preserve status 1 and the existing single diagnostic boundary.
- **FR-004**: The agent guide MUST describe actual setup/use and exit statuses: 0 success, 1 runtime/output/cancellation failure, 2 usage failure.
- **FR-005**: Guidance MUST state task relevance and authority limits; only help/version are implemented, and future backend operations MUST remain planned.
- **FR-006**: A root troubleshooting index and README/knowledge-map links MUST expose the guidance, with offline validation of local destinations and notice URL targets.
- **FR-007**: The CLI MUST introduce no dependency, network call, repository operation or new command; Pages packaging/publication scope MUST remain unchanged.

### Key Entities
- **Guidance notice**: Two repository-owned destinations and a short task-relevance instruction, visible only in root help.
- **Guidance documents**: An agent guide and a troubleshooting index whose local links resolve in a checkout.

## Success Criteria

### Measurable Outcomes
- **SC-001**: All four root-help forms show exactly one notice and both intended destinations.
- **SC-002**: Existing version results and error statuses pass regression checks, including failures during the appended notice.
- **SC-003**: Every guidance/index destination passes offline validation; broken-fixture checks prove missing destinations are caught.
- **SC-004**: A reader can build the binary and execute the documented supported examples; no planned operation is presented as available.

## Assumptions
- SheetMill PR #19 supplies the discovery pattern; SourceSteward's own command contract and policy govern this implementation.
- The notice points to raw files on the repository's main branch; local documentation remains usable without internet access.
- This feature does not authorize merging, remote publication, releases or changes to the Pages artifact inventory.
