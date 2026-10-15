# Tasks: Project Foundation

**Input**: spec.md, plan.md, research.md, data-model.md, contracts/cli.md, quickstart.md.
**Tests**: Required by the constitution; failing behavior tests precede implementation.

## Phase 1: Setup
- [x] T001 Initialize empty root commit and public repository; configure origin in .git/config.
- [x] T002 Initialize Go/Cobra dependencies in go.mod and go.sum.
- [x] T003 Install Spec Kit 1.1.3 infrastructure in .specify/ and .agents/skills/.

## Phase 2: Foundational
- [x] T004 Define governance and scope in .specify/memory/constitution.md and AGENTS.md.
- [x] T005 Define jj-ready identities/capabilities/effects in docs/backends.md.
- [x] T006 Write design artifacts and contracts in specs/001-project-foundation/.

## Phase 3: US1 — Discover and identify (P1)
**Independent test**: Execute CLI help/version/invalid input outside a repository.
- [x] T007 [US1] Write failing behavior, output, cancellation and isolation tests in internal/cli/root_test.go.
- [x] T008 [US1] Implement fresh Cobra command and result/error boundary in internal/cli/root.go.
- [x] T009 [US1] Wire process cancellation and exit handling in cmd/stw/main.go.

## Phase 4: US2 — Contribution contract (P2)
**Independent test**: Follow README to specifications and run source checks.
- [x] T010 [US2] Document current scope, prerequisites and roadmap in README.md and docs/roadmap.md.
- [x] T011 [US2] Add source checks and document/module validation in Makefile and scripts/check-docs.py.
- [x] T012 [US2] Validate broken links/unsafe site packaging using scripts/test_docs.py.

## Phase 5: US3 — Public website (P3)
**Independent test**: View site on desktop/mobile; confirm same-SHA CI→Pages deployment.
- [x] T013 [US3] Create original family mark and licensed local font assets in docs/assets/ and brand/README.md.
- [x] T014 [US3] Implement accessible static landing page in docs/index.html and docs/assets/site.css.
- [x] T015 [US3] Add allowlisted static artifact packaging in scripts/package-pages.py.
- [x] T016 [US3] Add pinned, same-run CI→Pages workflow in .github/workflows/ci.yml.
- [ ] T017 [US3] Configure repository topics/homepage and Actions Pages source using gh; verify live deployment.

## Phase 6: Polish and handoff
- [ ] T018 Review specifications/source for consistency and record evidence in docs/verification.md.
- [ ] T019 Run make check and race tests; publish focused Conventional Commits; create durable local checkout.

## Additional user requirements
- [x] T020 Add Dependabot and community standards in .github/, CONTRIBUTING.md, CODE_OF_CONDUCT.md and SECURITY.md.
- [x] T021 Add architecture checks and harness knowledge/evidence maps in scripts/check-architecture.py, ARCHITECTURE.md and docs/.

## Dependencies and parallel opportunities
Setup → foundation → US1. US2 documentation and US3 artwork may proceed independently after
foundation; source validation and CLI tests gate publication. Final deployment needs all
source checks. US1 tests and US3 design are separate-file work; no parallel authoring is required.

## Implementation strategy
Deliver the smallest working scaffold, then the public entry point. No roadmap operation is
implemented or represented by a stub command. Update checkboxes only with measured evidence.
