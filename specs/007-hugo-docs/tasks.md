# Tasks: Generated documentation

## Setup and gates
- [x] T001 Create reviewed design artifacts in specs/007-hugo-docs/ (coordinator released implementation after design review).
- [x] T002 Add failing contract tests in scripts/test_hugo.py and internal/cli/docs_test.go.

## US1 — Branded public guides
- [x] T003 [US1] Add local config/templates in site/ preserving landing and theme control.
- [x] T004 [US1] Add explicit staging/link resolution and shortcode rejection in scripts/package-pages.py.
- [x] T005 [US1] Add public documentation navigation in docs/index.html and shared styles in docs/assets/site.css.

## US2 — Generated reference
- [x] T006 [US2] Export deterministic command metadata in internal/cli/docs.go and cmd/stw-docs/main.go.
- [x] T007 [US2] Stage generated command pages and verify actual command/flag coverage in scripts/test_hugo.py.

## US3 — Checked artifact publication
- [x] T008 [US3] Pin official hashes and safe installer in scripts/hugo-release.json and scripts/install-hugo.py.
- [x] T009 [US3] Carry verbatim schemas/public llms and revision through scripts/package-pages.py.
- [x] T010 [US3] Build/check once and deploy identical artifact in .github/workflows/ci.yml; add Makefile target.

## Verification and handoff
- [x] T011 Run source/generated checks, inspect privacy/link/schema boundaries and record specs/007-hugo-docs/verification.md.
- [x] T012 Obtain coordinator browser QA and independent exact-revision review; fix findings before PR handoff.

- [x] T013 Coordinator opens the reviewed Hugo PR against006 and records PR/CI handoff in specs/007-hugo-docs/verification.md.

Dependencies: T001 release → T002; T003/T004/T006/T008 after tests; T005 after shell;
T007 after T006; T009 after T004; T010 after T007/T008/T009; T011/T012 last.
Parallel opportunities: reference export and template work affect distinct files, but one assigned
worktree author performs edits sequentially. Deliver US1 first, then reference and CI artifact gate.

## Command coverage amendment (2026-10-16)

- [x] T014 Amend US4/FR011–012 and the plan for complete implemented command coverage and effect distinctions.
- [x] T015 Refresh README, landing, command catalog, roadmap and agent guidance; add the catalog to the fixed public guide allowlist.
- [x] T016 Check actual command/flag examples, full source checks and generated links/schemas; inspect responsive/theme presentation and record evidence.
- [x] T017 Make focused documentation/site commits and open a PR; preserve historical acceptance and distinguish pending hosted publication.
