# SourceSteward architecture

SourceSteward is a Go CLI with a static project site. Its backend-neutral executable is `stw`. It must remain useful to humans and agents through one result model.

## Current boundaries
```text
cmd/stw → internal/cli → Cobra + standard library
scripts/       → source/documentation checks and Pages packaging
specs/         → acceptance contracts, plans, tasks and evidence pointers
```
cmd owns process cancellation and exit status. internal/cli owns parsing, rendering and the
single diagnostic boundary. It does not import Git subprocess, filesystem mutation or network
adapters. scripts/check-architecture.py mechanically checks these foundation boundaries.

## Planned operation boundary
```text
CLI / future MCP → application operation → selected Git | jj | GitButler adapter
                        ↓
                shared typed result + explicit backend-specific evidence
```
No backend adapter exists yet. Avoid premature universal interfaces: capabilities, stable
identities, inspection effects and recovery guarantees are specified before implementation.
The [backend contract](docs/backends.md) is the design boundary for jj readiness.

## Knowledge and execution
[AGENTS.md](AGENTS.md) is the short entry map. [docs/README.md](docs/README.md) indexes the
knowledge base. Specifications own user outcomes, plans own design choices, tasks own scoped
implementation work, and [progress](docs/progress.md) records handoffs. [Harness guidance](docs/harness.md)
defines the feedback loop. Website packaging copies only index.html and assets; the deployed
revision is recorded in revision.txt. No release automation or agent scheduler is installed.
