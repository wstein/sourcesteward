# SourceSteward

![SourceSteward — Explainable version control](docs/assets/banner.svg)

[![CI](https://github.com/wstein/sourcesteward/actions/workflows/ci.yml/badge.svg)](https://github.com/wstein/sourcesteward/actions/workflows/ci.yml)
[![Go 1.27.2+](https://img.shields.io/badge/Go-1.27.2%2B-0b2545)](go.mod)
[![License EUPL-1.2](https://img.shields.io/badge/license-EUPL--1.2-0b2545)](LICENSE)
[![Pre-release](https://img.shields.io/badge/status-pre--release-f05032)](docs/roadmap.md)

**Explainable version-control tools for humans and AI agents.**

Inspect state. Understand the plan. Act with confidence.

SourceSteward is an early Go/Cobra scaffold inspired by the convenience of
[nvie/git-toolbelt](https://github.com/nvie/git-toolbelt) and the human/agent workflows of
[GitButler](https://gitbutler.com/). Its planned focus is predictable inspection,
evidence-based local cleanup and worktree-aware operations, with optional jj and GitButler integration.
No implementation code is copied from those projects.

**Current scope:** help and version reporting, specifications, source CI and the project site.
Inspection, cleanup, recovery and jj/GitButler integration are planned, not implemented.

[Website](https://wstein.github.io/sourcesteward/) · [Roadmap](docs/roadmap.md) · [Backend contract](docs/backends.md) ·
[Specification](specs/001-project-foundation/spec.md) ·
[Plan](specs/001-project-foundation/plan.md) · [Tasks](specs/001-project-foundation/tasks.md)

## Try the scaffold

Requires Go 1.27.2 or later. From the repository root:

```sh
go run ./cmd/stw --help
go run ./cmd/stw version
go run ./cmd/stw version --json
```

To install the development CLI with Go:

```sh
go install ./cmd/stw
stw --help
```

The repository/module is `github.com/wstein/sourcesteward`; the executable is `stw`,
which is invoked directly as `stw`. No Git repository or credentials are needed for help/version.
No release binaries have been published.

## Develop with specifications

Spec Kit 1.1.3 was initialized with the Codex skills integration. Read the
[constitution](.specify/memory/constitution.md) and [agent policy](AGENTS.md).
The flow is constitution → specify → plan → tasks → implement → analyze.
Project-local skills live in `.agents/skills`; use `$speckit-specify` for the next feature.
See [development](docs/development.md) for checks and publication behavior.

## Community and engineering

[Contributing](CONTRIBUTING.md) · [Code of Conduct](CODE_OF_CONDUCT.md) ·
[Security](SECURITY.md) · [Architecture](ARCHITECTURE.md) · [Harness](docs/harness.md)

Dependabot proposes weekly Go module and GitHub Actions updates. Source checks and review
apply to every proposal; no automatic merging is configured.

## License and branding

Original code and artwork: [EUPL-1.2](LICENSE). Third-party dependencies and fonts retain
their own licenses. [Brand notes](brand/README.md) document the BundleDock/WorkHarbor family
palette and locally hosted fonts. SourceSteward is an independent project.
