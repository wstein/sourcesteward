# Feature Specification: Project Foundation

**Feature Branch**: `codex/001-project-foundation`

**Created**: 2026-10-15

**Status**: Specified

> **Status note (2026-10-26):** `docs/feature-status.json` records this feature as `verified` (evidence: `verification.md`); the status line above is the state at writing.

**Input**: Create SourceSteward with an initial empty commit, a command-line scaffold,
specification workflow, public repository metadata, project website and branding related
to BundleDock/WorkHarbor. Serve humans and agents without claiming planned features exist.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Discover and identify the tool (Priority: P1)
A developer or coding agent can discover available commands and identify the tool version
without accessing a repository, using credentials or waiting for input.

**Why this priority**: Every later capability depends on a predictable entry point.

**Independent Test**: Invoke help, version and invalid arguments in an empty directory.

**Acceptance Scenarios**:
1. **Given** no repository or credentials, **When** help is requested, **Then** it succeeds
   and shows supported commands without modifying files or contacting a network service.
2. **Given** a development build, **When** version is requested in text and structured forms,
   **Then** both report the same product/version and the structured form identifies its schema.
3. **Given** unknown commands, unsupported flags or extra positional arguments, **When**
   invoked without a terminal, **Then** execution fails promptly with one useful diagnostic.

### User Story 2 - Understand the product and contribution contract (Priority: P2)
A contributor can find specifications, safeguards, current implementation status and the
next planned milestones in the repository.

**Why this priority**: Contributors must distinguish plans from shipped behavior.

**Independent Test**: Follow README links to the constitution, feature specification,
implementation plan, tasks and roadmap.

**Acceptance Scenarios**:
1. **Given** the initial repository, **When** a contributor follows its development guide,
   **Then** source checks run using documented prerequisites and no release is created.
2. **Given** the roadmap, **When** an agent considers cleanup or GitButler actions,
   **Then** those capabilities are explicitly marked as planned, with no fake commands.
3. **Given** a contributor choosing jj or GitButler, **When** reading the backend contract,
   **Then** capabilities, inspection effects and identity differences are explicit and no
   adapter support is implied by the scaffold.
4. **Given** the Git history, **When** the first commit is inspected, **Then** it contains no files.

### User Story 3 - Visit the project website (Priority: P3)
A visitor finds an accessible project overview with recognizable family branding and links
to source, specifications and development instructions.

**Why this priority**: A stable public entry point explains the project and its maturity.

**Independent Test**: Open the website at desktop and narrow viewport sizes and follow its links.

**Acceptance Scenarios**:
1. **Given** a visitor using keyboard navigation or a small screen, **When** viewing the site,
   **Then** navigation remains usable and content does not require horizontal scrolling.
2. **Given** a proposed source revision with failed checks, **When** automation runs,
   **Then** it cannot deploy that revision to the public website.
3. **Given** successful checks on the default branch, **When** publication runs,
   **Then** the website artifact and deployment come from that same checked revision.

### Edge Cases
- Help/version outside a repository and with unavailable Git/GitHub authentication.
- Unknown commands, unsupported flags, empty input and unexpected extra arguments.
- Output write failures, cancellation and repeated command construction in the same process.
- Pull requests and non-default branches must not deploy the website.
- Unavailable external services must not prevent local help or version reporting.
- Fonts unavailable or disabled: all website text remains readable with local fallbacks.

## Requirements *(mandatory)*

### Functional Requirements
- **FR-001**: The tool MUST expose discoverable help and version information without repository
  access, prompts, editors, credential lookup or network calls.
- **FR-002**: Text and structured version output MUST agree on product and version; structured
  output MUST include a schema version and MUST contain no human preamble.
- **FR-003**: Invalid commands, flags and extra arguments MUST fail promptly with a nonzero
  exit status and one diagnostic on the error stream.
- **FR-004**: Documentation MUST identify implemented and planned behavior separately.
- **FR-005**: Repository history MUST begin with an empty commit; public metadata MUST include
  a description, relevant topics and the website address.
- **FR-006**: Contributors MUST have documented, repeatable source checks and a discoverable
  specification, plan, task list and governing principles.
- **FR-007**: The website MUST provide accessible navigation, local assets and source/spec links,
  use related family branding, and carry an original SourceSteward mark.
- **FR-008**: Website deployment MUST depend on successful source checks for the exact deployed
  revision on the default branch; pull requests MUST only run checks.
- **FR-009**: Foundation execution MUST NOT perform Git mutations or present unimplemented
  inspection, cleanup, recovery or GitButler actions as functional commands.

### Key Entities
- **Version information**: Product identity, build version and structured-output schema version.
- **Feature record**: User scenarios, requirements, measurable outcomes, plan and tasks.
- **Publication revision**: Exact source identity shared by successful checks and website artifact.

## Success Criteria *(mandatory)*

### Measurable Outcomes
- **SC-001**: All help/version acceptance scenarios complete without input or repository state.
- **SC-002**: All invalid-input cases fail with exactly one error diagnostic and no success record.
- **SC-003**: Every repository-local documentation and asset link resolves in automated checks.
- **SC-004**: Website publication has no execution path that bypasses the same-revision source checks.
- **SC-005**: Desktop and 375-pixel-wide layouts have readable content and no horizontal overflow.
- **SC-006**: All advertised working commands have passing behavior tests; roadmap commands
  are labeled planned and absent from the executable command list.

## Assumptions
- The repository is public under wstein/sourcesteward, matching the requested project name and Pages use.
- This milestone establishes the project, not the full Git toolkit discussed earlier.
- jj and GitButler are future optional integrations, not a prerequisite for running the scaffold.
- EUPL-1.2 follows the adjacent family projects; redistributed fonts retain their own licenses.
- Source publication and initial Pages configuration/deployment are authorized by this request.
