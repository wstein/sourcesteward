# Feature Specification: Discoverable site with light and dark themes

**Feature Branch**: `codex/004-site-themes`
**Created**: 2026-10-15
**Status**: Reviewed before implementation
**Input**: Make the project-site link visible in README and add a light/dark Pages switch.

## User Scenarios & Testing

### User Story 1 — Find the project site (Priority: P1)
A README reader can immediately find the public project site near the introduction.
**Independent Test**: Inspect the opening README section for a descriptive direct project-site link.
**Acceptance**: The existing navigation link remains valid and a prominent introductory link points to https://wstein.github.io/sourcesteward/.

### User Story 2 — Choose a readable appearance (Priority: P1)
A visitor can follow their system appearance or choose light/dark explicitly.
**Independent Test**: Open Pages with light/dark system preferences, change the native theme control using a keyboard, then reload.
**Acceptance**:
1. With no saved choice, the site follows system appearance, including system changes.
2. Selecting light or dark applies that appearance and persists the choice on reload where storage works.
3. Selecting system removes the explicit override and follows the current system preference.
4. Blocked storage or invalid saved values do not break rendering or the control.
5. With scripting disabled, the site follows system appearance and hides the inactive switch.

## Requirements
- **FR-001**: README MUST promote the real Pages URL near its introduction without claiming the existing navigation link was absent.
- **FR-002**: Pages MUST support system, light and dark appearances with a stable visible accessible label and native keyboard interaction.
- **FR-003**: Explicit choices MUST persist locally when available; storage failures MUST leave the current visit functional. Unknown saved values MUST act as system.
- **FR-004**: Both palettes MUST preserve navy/orange/white branding, readable text (normal text contrast at least 4.5:1), visible focus and no horizontal overflow at 375 and 1280 CSS pixels.
- **FR-005**: Without JavaScript Pages MUST remain fully readable and follow system appearance; inactive controls MUST stay hidden.
- **FR-006**: Runtime resources MUST remain local. Only the audited theme asset may execute; inline/remote scripts and inline event handlers MUST remain rejected by source checks. Packaging MUST reject other JavaScript assets.
- **FR-007**: CLI/backend behavior, generated brand assets and unrelated features MUST remain unchanged.

## Success Criteria
- **SC-001**: README link inspection and offline source/packaging tests pass.
- **SC-002**: Automated theme tests cover first visit, persistence, system reset, invalid values and unavailable storage. CSS delegates system preference changes to the browser; actual dynamic OS changes require separate browser evidence.
- **SC-003**: Browser acceptance at 375×812 and 1280×800 covers both palettes, keyboard selection/focus and reload without overflow. No-script fallback is inspected separately; record any untested OS preference changes explicitly.
- **SC-004**: Required source checks and independent review pass before a PR handoff; publication remains outside implementation ownership.

## Assumptions and Edge Cases
A native three-option select is preferred over a two-state toggle so visitors can return to system behavior. No dependency, network request, account or telemetry is introduced. Storage may throw on read/write. The header wraps at narrow widths. System appearance defaults to light when unavailable.
