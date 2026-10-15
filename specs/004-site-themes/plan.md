# Implementation Plan: Site themes

**Branch**: `codex/004-site-themes` | **Date**: 2026-10-15 | **Spec**: [spec.md](spec.md)

## Summary and technical context
Keep the static HTML/CSS site; add one small dependency-free `docs/assets/theme.js` loaded as a local deferred script. A native select labeled “Theme” offers System/Light/Dark. The control is hidden in HTML until initialization succeeds. Theme state is an optional root `data-theme` attribute and a namespaced localStorage key; only `light` and `dark` are persisted, system removes the key. CSS supplies a light default and system dark media rule, overridden by explicit root choices. All palette-sensitive decorations/buttons/terminal use semantic variables. No CLI change or new package dependency.

## Constitution check
Shared human-readable controls, no repository effects, narrow local storage only, offline runtime, failing tests first and exact-revision independent review satisfy the constitution. Spec/plan/tasks are reviewed before implementation. No violations.

## Design decisions and structure
- `README.md`: prominent project-site link near introduction, existing navigation retained.
- `docs/index.html`, `docs/assets/site.css`, `docs/assets/theme.js`: native select, progressive enhancement and palette tokens.
- `scripts/check-docs.py`: allow exactly one empty deferred local `assets/theme.js` script; reject inline script content, extra scripts and inline events. Existing local asset and link validation retained.
- `scripts/package-pages.py`: allow JavaScript only at `assets/theme.js`; retain symlink and source exclusion checks.
- `scripts/test_docs.py`: negative script/packaging cases and site structure contract.
- `scripts/test_theme.mjs`: Node built-in test/VM harness executes the actual asset with minimal DOM/storage/media stubs; test observable state and events, not a duplicate implementation. `make check` invokes it; CI pins official actions/setup-node v6 by commit and selects Node 24; local checks require Node 22+.
- `specs/004-site-themes/verification.md`, `docs/progress.md`, `docs/feature-status.json`: actual evidence and honest pending browser/review state.

## Verification and delivery
First run new tests failing against the current site, then implement, run make check. Coordinator owns real browser light/dark/system, keyboard/reload/no-script checks at 375/1280, measured contrast, independent review and PR creation. Native select avoids custom ARIA state logic. No flash-prevention inline code is introduced; CSS remains usable before deferred initialization. No application data model/API is needed beyond the three theme values and optional persisted override.
