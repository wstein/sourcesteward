# Progress and handoff

## Current milestone
001-project-foundation. Public repository exists; initial empty commit is published.
CLI help/version and JSON/error behavior pass local tests. Site, community files, Dependabot
and validation harness are authored. Final independent review, browser checks and CI/Pages
verification are in progress; consult verification.md for measured evidence.

## Decisions
- Repository/module: wstein/sourcesteward; executable: stw; standalone CLI: stw.
- Go 1.27.2 and Cobra v1.10.2; EUPL-1.2 original source/artwork; font licenses retained.
- Git-first and jj-ready, with optional GitButler adapters planned; no backend executes yet.
- Static Pages with local assets and same-run exact-SHA checks; no CLI release automation.
- Atomic Conventional Commits; Spec Kit workflow and small repository-local harness.

## Next bounded action
Finish foundation acceptance, independent review, browser checks and same-SHA Pages deployment.
Then specify ordinary Git inspection plus explicit backend detection. Do not implement
cleanup, mutate a jj/GitButler workspace or migrate a repository as part of foundation.
