# Design decisions

- **Decision**: Native select labeled Theme with System, Light and Dark. **Rationale**: Browser keyboard and assistive-technology semantics stay native; system mode remains recoverable. **Alternative**: Binary switch cannot directly restore system preference.
- **Decision**: CSS light default plus prefers-color-scheme dark media query, explicit data-theme overrides. **Rationale**: No-script rendering follows the system. **Alternative**: Script-only palette cannot handle unavailable JavaScript.
- **Decision**: One deferred, local, dependency-free JavaScript file; catch storage read/write failures. **Rationale**: Minimal auditable behavior and stable page rendering. **Alternative**: Inline startup code weakens the existing script policy and is unnecessary for this bounded feature.
- **Decision**: Execute the actual file in Node's built-in VM/test modules for deterministic state/storage/event coverage; browser acceptance remains separate. **Rationale**: No test package dependencies; source stubs cannot prove browser layout or accessibility.
- No unresolved design questions; no additional dependencies. Node is a development/check prerequisite and must be explicit in development docs and CI.
