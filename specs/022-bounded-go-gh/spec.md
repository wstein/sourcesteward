# Feature Specification: Bounded official go-gh transport

**Feature Branch**: `codex/022-bounded-go-gh`  
**Created**: 2026-10-16  
**Status**: Design for independent analysis  
**Input**: Implement the bounded go-gh/v2 transport in a separate PR, preserving Git/jj behavior and JSON contracts.

## User Scenarios & Testing

### User Story 1 — Preserve authenticated inspection (Priority: P1)

A human or agent reads the same PR/CI evidence through `stw hosting` or `gh jj`, using official SDK HTTP transport and the caller's selected-host gh authentication.

**Independent Test**: Fixed in-memory HTTP responses plus a private gh fixture verify exact authenticated GitHub, Enterprise and ghe.com GET destinations and unchanged operation results.

**Acceptance Scenarios**:
1. **Given** a selected repository, **When** PR/CI inspection runs, **Then** existing native ownership, repository selection, bounded pagination and PR identity resampling remain authoritative.
2. **Given** caller authentication, **When** transport initializes, **Then** an isolated, bounded gh token lookup supplies only the selected host's token to an explicitly addressed HTTPS SDK client.
3. **Given** repository resolution or another local operation, **When** gh is absent, **Then** no new authentication or network dependency is introduced.

### User Story 2 — Bound every transport effect (Priority: P1)

A caller gets a bounded failure rather than unbounded buffering, unintended destinations or credential disclosure.

**Independent Test**: Adversarial HTTP bodies, redirects, cancellation, gh output and repository-linked configuration prove limits and sanitized errors.

**Acceptance Scenarios**:
1. **Given** a success or failure body above one MiB, **When** read, **Then** raw and SDK-normalized byte limits both refuse, with bounded sentinel reads and no raw body/token diagnostic.
2. **Given** cancellation, redirect, hostile endpoint/configuration or transport error, **When** executed, **Then** requests stay within the original deadline and fixed GET/HTTPS destinations, and never follow a redirect or expose credentials.
3. **Given** concurrent inspections of different hosts, **When** credentials differ, **Then** no global environment mutation, shared credential state, SDK logging/cache or cross-host token reuse occurs.

### User Story 3 — Retain Tier 1 and published contracts (Priority: P1)

Git and jj callers receive compatible human/JSON results and unchanged native read-only guarantees.

**Independent Test**: Real temporary native Git/jj repositories with fake HTTP transport exercise PR/CI operations, both CLI adapters and offline schemas; process tests cover extension dispatch and authentication refusal.

**Acceptance Scenarios**:
1. **Given** a Git or supported jj workspace, **When** remote inspection succeeds through fake HTTP, **Then** schemas and local effects match Spec 020 qualifications; `gh jj` still refuses Git ownership.
2. **Given** malformed/moved/paginated PR/CI evidence, **When** inspected, **Then** existing result codes/completeness behavior remain intact.
3. **Given** the migration, **When** checked, **Then** published schema bytes, native adapters and CLI option/result shapes are unchanged.

## Requirements

- **FR-001**: Pin `github.com/cli/go-gh/v2` v2.16.2 and justify its dependency/license footprint; use `api.NewHTTPClient` with owned status/body handling, not unbounded REST convenience decoding.
- **FR-002**: Preserve `apiReader`, Options/Result, native Git/jj discovery/effects, command behavior, pagination/resampling and existing published schemas.
- **FR-003**: Retain trusted gh executable/version/configuration checks and neutral execution directory. Resolve credentials through context-bound, 16 KiB-capped stdout/stderr for `gh auth token --hostname HOST`; accept only a nonempty visible-ASCII single-line token with an optional trailing newline; never use unbounded SDK auth or subprocess helpers.
- **FR-004**: Explicitly set trusted Host, APIHost, AuthToken and Transport; disable SDK cache/logging/environment logging. Derive canonical GitHub/Enterprise/ghe.com API routing from validated selected host, never response/repository URLs or API overrides.
- **FR-005**: Allow only generated fixed PR/check/status GET requests over HTTPS to the selected API origin; refuse redirects and malformed methods/paths/query selections before sending credentials.
- **FR-006**: Limit each raw response and normalized response to 1,048,576 bytes, reading at most one extra sentinel byte to detect excess, including non-success bodies. Close bodies on every exit and return sanitized bounded errors.
- **FR-007**: Preserve the whole 30-second operation deadline and existing 1 MiB version-subprocess/final-output, 10-page and 1000-record bounds; honor cancellation and support concurrent calls without global environment changes.
- **FR-008**: Preserve config/symlink refusal and noninteractive subprocess isolation; caller proxy settings are trusted caller infrastructure, never repository-selected. No API socket/host override, cache, credential persistence or request logging is introduced.
- **FR-009**: Provide an internal in-process HTTP injection seam for tests, without CLI flags or production environment endpoint overrides. Tests must exercise real shared operations and CLI rendering, not replace inspection with canned results.
- **FR-010**: Write failing behavior tests first; independently analyze before implementation and review afterward. Require Git/jj minimum/current conformance, unchanged schema fixtures, source/race/module/docs checks and truthful evidence of gaps.

## Success Criteria

- **SC-001**: Adversarial transport tests prove raw/normalized success/error bounds, routing/auth isolation, denial of redirects, cancellation and safe diagnostics.
- **SC-002**: Both Tier 1 backends pass in-process native PR/CI conformance with fake HTTP, plus real process local/extension dispatch and refusal checks; no network or repository mutation occurs in fixtures.
- **SC-003**: Existing published schema files remain byte-for-byte unchanged, existing contracts pass, and make check/race/module verification pass before handoff.

## Scope

Transport-only adoption supersedes Spec 020 FR-006's subprocess API mechanism, not its fixed read-only REST behavior. Spec 020's fake-gh API success fixtures are replaced with fake HTTP success fixtures; executable dispatch and auth failures remain real-process tests. No native jj dependency/library, SDK repository discovery, write API, live API test, release, user-profile installation or deletion is added. Disposable private fixture extension installation remains permitted. Local test builds are authorized by the owner; publication means a source PR only.
