# SourceSteward architecture

SourceSteward is a Go CLI with a static project site. Its backend-neutral executable is `stw`. It must remain useful to humans and agents through one result model.

## Current boundaries
```text
cmd/stw → internal/cli → internal/inspection → fixed Git built-ins + read-only metadata
                       → Cobra + standard library
scripts/       → source/documentation checks and Pages packaging
specs/         → acceptance contracts, plans, tasks and evidence pointers
```
cmd owns process cancellation and exit status. internal/cli owns parsing, rendering and the
single diagnostic boundary. It imports only the reviewed inspection operation, not os, os/exec, filesystem or network adapters.
internal/inspection owns local filesystem discovery, typed results and bounded Git plumbing.
scripts/check-architecture.py mechanically checks the CLI boundary.

## Planned operation boundary
```text
CLI / future MCP → application operation → selected Git | jj | GitButler adapter
                        ↓
                shared typed result + explicit backend-specific evidence
```
Bounded ordinary-Git identity inspection exists; native jj/GitButler adapters do not. Avoid premature universal interfaces: capabilities, stable
identities, inspection effects and recovery guarantees are specified before implementation.
Git and jj are Tier 1 peers with native adapters for supported capabilities; native jj identity
inspection remains pending Spec 008 acceptance. The [backend contract](docs/backends.md)
defines ownership-based selection, versions, schema compatibility and conformance evidence.
Read-only jj acceptance must verify working-copy and operation-log effects independently.

## Knowledge and execution
[AGENTS.md](AGENTS.md) is the short entry map. [docs/README.md](docs/README.md) indexes the
knowledge base. Specifications own user outcomes, plans own design choices, tasks own scoped
implementation work, and [progress](docs/progress.md) records handoffs. [Harness guidance](docs/harness.md)
defines the feedback loop. Website packaging stages an explicit public-guide allowlist, generated Cobra reference,
local assets and versioned schemas through repo-owned Hugo templates. Internal knowledge
files stay outside the artifact; revision.txt identifies the exact checked source SHA. No release automation or agent scheduler is installed.
