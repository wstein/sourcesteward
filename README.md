# SourceSteward

![SourceSteward — Explainable version control](docs/assets/banner.svg)

[![CI](https://github.com/wstein/sourcesteward/actions/workflows/ci.yml/badge.svg)](https://github.com/wstein/sourcesteward/actions/workflows/ci.yml)
[![Go 1.27.2+](https://img.shields.io/badge/Go-1.27.2%2B-0b2545)](go.mod)
[![License EUPL-1.2](https://img.shields.io/badge/license-EUPL--1.2-0b2545)](LICENSE)
[![Pre-release](https://img.shields.io/badge/status-pre--release-f05032)](docs/roadmap.md)

**Explainable version-control tools for humans and AI agents.**

Inspect state. Understand the plan. Act with confidence.

[Project website](https://wstein.github.io/sourcesteward/) — overview, approach and roadmap.

SourceSteward is an early Go/Cobra scaffold inspired by the convenience of
[nvie/git-toolbelt](https://github.com/nvie/git-toolbelt) and the human/agent workflows of
[GitButler](https://gitbutler.com/). Its planned focus is predictable inspection,
evidence-based local cleanup and worktree-aware operations, with native Git/jj support and separately planned GitButler integration.
No implementation code is copied from those projects.

**Current scope:** help/version, bounded read-only Git/jj identity inspection and qualified workspace/state/reference inventory, reviewable isolated native Git/jj task workspace creation, versioned JSON schemas, specifications, source CI and the project site.
Evidence-based local reference tidy planning and explicitly authorized cleanup are implemented; owned task retirement is implemented through separate reviewed plans and explicit authorization; general recovery and native GitButler integration remain planned. See [tidy](docs/tidy.md) for proof and retention limits. See [task workspaces](docs/workspaces.md) for creation, handoff and retirement preparation. Inventory exposes explicit native capability limits; see [inventory](docs/inventory.md).
Shared read-only GitHub repository/PR/CI inspection is available through `stw hosting` and the optional `gh jj` entry point; see [GitHub inspection](docs/hosting.md).
Spec 008 implements native jj inspection; final cross-platform acceptance is tracked in its [verification record](specs/008-jj-inspection/verification.md).

[Website](https://wstein.github.io/sourcesteward/) · [Roadmap](docs/roadmap.md) · [Backend contract](docs/backends.md) ·
[Specification](specs/001-project-foundation/spec.md) ·
[Plan](specs/001-project-foundation/plan.md) · [Tasks](specs/001-project-foundation/tasks.md)

## Try the scaffold

Requires Go 1.27.2 or later. From the repository root:

```sh
go run ./cmd/stw --help
go run ./cmd/stw version
go run ./cmd/stw version --json
go run ./cmd/stw inspect --json
go run ./cmd/stw inventory --json
```

To install the development CLI with Go:

```sh
go install ./cmd/stw
stw --help
```

The repository/module is `github.com/wstein/sourcesteward`; the executable is `stw`,
which is invoked directly as `stw`. No Git repository or credentials are needed for help/version.
Git inspection requires Git 2.46+. Native jj inspection requires stable jj 0.45.1+ and usable
read-only bubblewrap on Linux or sandbox-exec on macOS; unavailable enforcement fails safely
without a Git fallback. Inspection reports stored identity, not file status or disk freshness.
Git/version retain schema v1; native jj results use schema v2. See the
[backend contract](docs/backends.md) and [jj inspection contract](specs/008-jj-inspection/contracts/inspect.md).
No release binaries have been published.

## Develop with specifications

Spec Kit 1.1.3 was initialized with the Codex skills integration. Read the
[constitution](.specify/memory/constitution.md) and [agent policy](AGENTS.md).
The flow is constitution → specify → plan → tasks → implement → analyze.
Project-local skills live in `.agents/skills`; use `$speckit-specify` for the next feature.
See [development](docs/development.md) for checks and publication behavior.
The [agent guide](docs/agent-guide.md) covers current CLI use;
[llms.txt](llms.txt) indexes troubleshooting documentation and source.

## Community and engineering

[Contributing](CONTRIBUTING.md) · [Code of Conduct](CODE_OF_CONDUCT.md) ·
[Security](SECURITY.md) · [Architecture](ARCHITECTURE.md) · [Harness](docs/harness.md)

Dependabot proposes weekly Go module and GitHub Actions updates. Source checks and review
apply to every proposal; no automatic merging is configured.

## License and branding

Original code and artwork: [EUPL-1.2](LICENSE). Third-party dependencies and fonts retain
their own licenses. [Brand notes](brand/README.md) document the BundleDock/WorkHarbor family
palette and locally hosted fonts. SourceSteward is an independent project.
