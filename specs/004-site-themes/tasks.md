# Tasks: Site themes

## Setup and contract
- [x] T001 Create reviewed specification, design and requirement checklist in specs/004-site-themes/.
- [x] T002 Add failing behavior/security/packaging tests in scripts/test_docs.py and scripts/test_theme.mjs.

## User Story 1 — Discover the site
- [x] T003 [US1] Promote the Pages link beside the introduction in README.md.

## User Story 2 — Choose appearance
- [x] T004 [US2] Add native labeled system/light/dark select and audited local script in docs/index.html and docs/assets/theme.js.
- [x] T005 [US2] Implement semantic palettes, no-script system fallback, focus and narrow layout in docs/assets/site.css.
- [x] T006 [US2] Narrow script authorization and packaging rules in scripts/check-docs.py and scripts/package-pages.py; integrate behavior tests in Makefile and .github/workflows/ci.yml as necessary.

## Verification and handoff
- [x] T007 Run make check and record contrast/source evidence in specs/004-site-themes/verification.md.
- [x] T008 Complete coordinator browser acceptance and hand off independent review, updating specs/004-site-themes/tasks.md and verification.md.
- [x] T009 Update docs/progress.md, docs/feature-status.json and docs/README.md with honest status; create focused Conventional Commits.
- [x] T010 Coordinator creates PR after acceptance; record handoff in specs/004-site-themes/verification.md.

## Dependencies and strategy
T001 precedes T002; failing tests precede T004–T006. T003 is independently implementable after review. T007 follows implementation; T008 precedes verified status and PR. MVP is discoverability plus native system/light/dark appearance; backend detection and inspection remain separate features.
