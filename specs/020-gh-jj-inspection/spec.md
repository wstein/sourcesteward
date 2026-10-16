# Feature Specification: Shared GitHub inspection and gh jj entry point

Transport mechanism and bounds are superseded by [Spec 022](../022-bounded-go-gh/spec.md) when the SDK migration lands. Native behavior, CLI options and published JSON contracts remain unchanged except the Spec 022 authentication-refusal result (`hosting-api-unavailable`, `network=false`, before any request) and new HTTP failure messages. The original implementation decisions below are historical.

**Feature Branch**: `codex/020-gh-jj-inspection`  
**Created**: 2026-10-16  
**Status**: Draft for analyzed implementation  
**Input**: Implement `gh jj` as a SourceSteward entry point sharing Go operations. Start with repository resolution and read-only PR/CI inspection.

## User Scenarios & Testing

### User Story 1 — Resolve an explicit hosting context (Priority: P1)

A human or agent identifies the GitHub repository associated with their native Git or jj workspace without assuming a remote name or Git branch convention.

**Independent Test**: Resolve ordinary Git, linked Git worktree, colocated jj, standalone jj and secondary jj workspaces with temporary remotes and no network access; verify unchanged local metadata and no gh process.

**Acceptance Scenarios**:

1. **Given** exactly one valid repository candidate among selected-host remotes, **When** `stw hosting repo` runs, **Then** it reports that repository, native identity and candidate provenance without authenticating or making network requests.
2. **Given** distinct fork/upstream candidates, **When** no selection is supplied, **Then** resolution refuses with candidates; `--repo OWNER/NAME` or unambiguous `--remote NAME` selects explicitly without changing remotes.
3. **Given** native jj ownership including colocation, **When** `gh jj repo` runs, **Then** the same operation selects native jj and preserves its distinct identity. Ordinary Git, ambiguous ownership and GitButler refuse in the extension rather than falling back.
4. **Given** unsupported remote formats or malicious URLs, **When** discovery runs, **Then** they cannot become executable commands, credentials or arbitrary API endpoints; any incomplete discovery refuses.

### User Story 2 — Inspect a numbered pull request (Priority: P1)

A caller explicitly requests a PR from the resolved hosting repository through authenticated, bounded GitHub reads.

**Independent Test**: Use a fake gh executable and real temporary local repositories to prove explicit GET endpoints, matching PR identity, structured errors and no local changes.

**Acceptance Scenarios**:

1. **Given** a resolved repository and explicit positive PR number, **When** `stw hosting pr NUMBER` or `gh jj pr NUMBER` runs, **Then** it reports exact head/base SHAs and repository identities, state, draft state and observation time.
2. **Given** missing gh, failed authentication, forbidden/not-found data, malformed response or cancellation, **When** inspection runs, **Then** it emits a bounded error result rather than an empty successful observation.
3. **Given** remote-controlled title/ref text, **When** it is rendered, **Then** text is treated as data and cannot produce terminal control effects or launch helpers.

### User Story 3 — Inspect CI at the exact PR head (Priority: P1)

A caller obtains checks and commit statuses for the explicitly observed PR head, with no claim of merge eligibility or required-check satisfaction.

**Independent Test**: Exercise paginated checks/statuses, errors, moved heads, fork PRs and exhausted limits with deterministic gh responses; verify zero mutation endpoints.

**Acceptance Scenarios**:

1. **Given** a PR with head SHA S, **When** CI inspection runs, **Then** checks and commit statuses are read at S in the resolved base repository, exact identities are retained, and the PR head is resampled before success.
2. **Given** a moved PR head, inaccessible collection, invalid SHA, inconsistent or truncated pagination, **When** inspection completes, **Then** it refuses a complete successful observation instead of interpreting partial data as no checks.
3. **Given** a successfully completed empty collection, **When** CI output is requested, **Then** the result distinguishes that observed emptiness from failure and does not claim CI passed or merging is safe.

### User Story 4 — Rely on one bounded contract (Priority: P1)

Humans and agents use the same results through `stw hosting` and the jj-focused extension.

**Independent Test**: Validate success/error fixtures against the new schema, compare both entry points on the same jj fixture, and run existing schema regressions.

**Acceptance Scenarios**:

1. **Given** equivalent options in a jj workspace, **When** either entry point runs, **Then** both use shared operations and offer equivalent human/JSON evidence, apart from observation timestamps.
2. **Given** missing optional gh, **When** local SourceSteward commands or hosting repository resolution run, **Then** they remain usable; only requested remote inspection requires gh.
3. **Given** hostile repository configuration, **When** inspection runs, **Then** no hooks, credential helpers, aliases, extensions, editors or pagers are executed; native read-only effects remain qualified.
4. **Given** source-only implementation, **When** handed off, **Then** live user installation/release remains separate from feature acceptance; private fixture installation verifies dispatch; no release automation or remote mutation is implied.

### Edge Cases

- Custom remotes, fork/upstream ambiguity, duplicate URLs, SSH/HTTPS URLs, rejected embedded credentials and unsupported custom schemes.
- Bare Git repositories; detached/unborn HEAD; stale jj workspaces; unsupported backends and missing sandbox enforcement.
- Rate limiting, timeouts, >1 MiB output, >10 pages or >1000 combined CI records; API data changed during observation.
- Fork heads, deleted head repositories and inaccessible checks; check statuses unknown to this release remain evidence, not an invented passing aggregate.

## Requirements

### Functional Requirements

- **FR-001**: Implement `cmd/gh-jj` as a thin Go entry point into shared CLI/hosting operations; `stw hosting repo|pr|ci` exposes the same capabilities for native Git and jj.
- **FR-002**: Always verify native ownership before resolution or network access. `gh jj` requires jj including colocated repositories; never bypass ownership with `--repo`, use raw Git writes inside jj, or initialize/migrate repositories.
- **FR-003**: Resolve validated GitHub remote candidates without assuming `origin`, `main`, `master` or a push target. Deduplicate identical repositories, disclose provenance and refuse ambiguity; support explicit host/repository or remote selection for forks.
- **FR-004**: Keep repository resolution local and independent of gh/authentication. Preserve native stored-unsnapshotted jj limitations and existing adapter configuration isolation and sandboxing.
- **FR-005**: Require an explicit positive PR number for PR/CI queries and verify returned identities and full SHAs; no heuristic branch-to-PR mapping.
- **FR-006**: Use only fixed `gh api --hostname HOST --method GET` REST requests with validated explicit repository/PR/SHA paths. No placeholders, generic forwarding, response-provided URLs, GraphQL, writes, hooks or credential helper execution.
- **FR-007**: Isolate gh working directory outside the target, suppress prompts/pagers/debug state and inherited repository selection, preserve only declared user authentication configuration/environment, and never echo tokens or raw subprocess error bodies.
- **FR-008**: Enforce cancellation, a 30-second hosting-operation deadline, 1 MiB subprocess output bound, at most 10 pages per collection and 1000 combined CI records. Never return complete success after malformed, failed, truncated or exceeded reads.
- **FR-009**: Read check runs and commit statuses for the exact observed PR head, verify check SHA identity, resample PR head before success and expose observation/concurrency limitations without fabricating merge eligibility.
- **FR-010**: Provide understandable human output and schema-versioned JSON success/error output from one result. Preserve all existing published JSON contracts; add a separate hosting schema and offline fixtures.
- **FR-011**: Disclose remote reads and bounded effects, keep local inspection usable without gh, and add no push, clone, fetch, PR edits, CI reruns, merge, deletion, extension installation or release effects.
- **FR-012**: Require failing behavior tests before implementation, independent design/source review and native minimum/current Git/jj conformance including effect sentinels, ambiguity, cancellation, malformed data and both entry points.

### Key Entities

- Hosting repository: validated host/owner/name, selection provenance and candidate remotes.
- Pull request: explicitly selected number, exact head/base identities and sampled state.
- CI observation: exact head SHA, individual checks/statuses, completeness and sampled limits.
- Hosting result: native identity, diagnostics, effects boundary and observation time.

## Success Criteria

- **SC-001**: Every acceptance scenario is covered by deterministic tests and mandatory native Git/jj conformance without counting skipped/missing execution as acceptance.
- **SC-002**: All complete success/error fixtures validate offline against the hosting schema, and existing published-contract fixtures remain valid.
- **SC-003**: Effect sentinels prove local refs/files, jj stored working copy and operation log remain unchanged, with zero mutation requests or undeclared helpers.
- **SC-004**: Both entry points share Go operations; local repository resolution works with gh absent; remote failure is never reported as empty success.

## Scope and Dependencies

This bounded read-only feature uses existing native identity/inventory foundations and supplies evidence for Spec 016. Spec 016 still owns future publication plans and remote mutations; local stack inspection remains Spec 012. Extension release packaging, Linux ARM64 release binaries, automatic PR mapping, reviews/required-check policy, stack coordination and mutation/recovery are deferred. SourceSteward implements independently in Go; no third-party extension source is imported.
