# SourceSteward architecture

SourceSteward is a Go CLI with a static project site. Its backend-neutral executable is `stw`. It must remain useful to humans and agents through one result model.

## Current boundaries
```text
cmd/stw | cmd/gh-jj → internal/cli → internal/inspection → fixed Git built-ins | sandboxed native jj + read-only metadata
                       → internal/workspace → fixed native Git/jj creation + private state journal
                       → internal/hosting → native remote evidence + bounded go-gh HTTP GET reads
                       → Cobra + standard library
scripts/       → source/documentation checks and Pages packaging
specs/         → acceptance contracts, plans, tasks and evidence pointers
```
cmd owns process cancellation and exit status. internal/cli owns parsing, rendering and the
single diagnostic boundary. It imports only the reviewed inspection, workspace and hosting operations, not os, os/exec, filesystem or network adapters.
internal/inspection owns local filesystem discovery, typed results, bounded Git plumbing and native jj execution.
internal/workspace owns the reviewed mutation boundary: secure plan/task records,
pre-action recovery journals, stale checks and fixed native adapters. CLI does not access
state files or launch processes. scripts/check-architecture.py mechanically checks this boundary.

For opt-in tidy v3 evidence, the workspace coordinator consumes the typed read-only
hosting session only after native ownership, configuration and protection preflight.
HTTP and authentication remain in internal/hosting; native sandboxes stay network-denied.
The dependency guard stops at the exact internal/hosting package for reviewed release
and workspace operations. Direct network imports, other transitive network paths and
lookalike/subpackages remain rejected. Inspection cannot borrow this boundary, including
by traversing workspace. Explicit `--contract 3` selects the reviewed v3 operation union;
default v1/v2 operations remain separate. Source implementation does not establish native
platform acceptance or authorize activation.

## Planned operation boundary
```text
CLI / future MCP → application operation → selected Git | jj | GitButler adapter
                        ↓
                shared typed result + explicit backend-specific evidence
```
Bounded native Git/jj identity inspection exists; GitButler integration remains planned. Avoid premature universal interfaces: capabilities, stable
identities, inspection effects and recovery guarantees are specified before implementation.
Git and jj are Tier 1 peers with native adapters for supported capabilities; native jj identity
inspection is implemented with final cross-platform acceptance tracked in Spec 008. The [backend contract](docs/backends.md)
defines ownership-based selection, versions, schema compatibility and conformance evidence.
Native jj uses enforced read-only Linux/macOS sandboxing, isolated configuration, snapshot
suppression and operation pinning. No unsandboxed fallback is permitted. Private temporary
isolation/probe files outside the target are the disclosed setup effect. Git/version retain
schema v1; native jj results use schema v2. Conformance verifies working-copy and operation-log
effects independently, including read-side repair refusal.

## Knowledge and execution
[AGENTS.md](AGENTS.md) is the short entry map. [docs/README.md](docs/README.md) indexes the
knowledge base. Specifications own user outcomes, plans own design choices, tasks own scoped
implementation work, and [progress](docs/progress.md) records handoffs. [Harness guidance](docs/harness.md)
defines the feedback loop. Website packaging stages an explicit public-guide allowlist, generated Cobra reference,
local assets and versioned schemas through repo-owned Hugo templates. Internal knowledge
files stay outside the artifact; revision.txt identifies the exact checked source SHA. No release automation or agent scheduler is installed.

The optional gh-jj executable delegates through the same CLI/operation boundary as stw. Hosting repository resolution is local; explicit PR/CI inspection adds bounded GitHub reads through caller-owned bounded gh authentication and the pinned official go-gh/v2 HTTP client. Raw and normalized bodies are bounded, redirects refuse, and native discovery never uses SDK Git helpers. See [hosting](docs/hosting.md). No Rust library/FFI, API writes or automatic extension installation is introduced.
