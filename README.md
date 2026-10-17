# SourceSteward

![SourceSteward — Explainable version control](docs/assets/banner.svg)

[![CI](https://github.com/wstein/sourcesteward/actions/workflows/ci.yml/badge.svg)](https://github.com/wstein/sourcesteward/actions/workflows/ci.yml)
[![Go 1.27.2+](https://img.shields.io/badge/Go-1.27.2%2B-0b2545)](go.mod)
[![License EUPL-1.2](https://img.shields.io/badge/license-EUPL--1.2-0b2545)](LICENSE)
[![Pre-release](https://img.shields.io/badge/status-pre--release-f05032)](docs/roadmap.md)

**Explainable version-control tools for humans and AI agents.**

Inspect state. Understand the plan. Act with confidence.

[Project website](https://wstein.github.io/sourcesteward/) — overview, approach and roadmap.

SourceSteward is a pre-release Go/Cobra toolkit inspired by the convenience of
[nvie/git-toolbelt](https://github.com/nvie/git-toolbelt) and the human/agent workflows of
[GitButler](https://gitbutler.com/). Its focus is predictable inspection,
evidence-based local cleanup and worktree-aware operations, with native Git/jj support and separately planned GitButler integration.
No implementation code is copied from those projects.

**Implemented:** native Git/jj inspection and inventory (also across several repositories with `stw repos`), isolated task workspace creation,
owned-workspace handoff/retirement, evidence-based local branch/bookmark tidy, and
read-only GitHub repository/PR/CI inspection. Git and jj are Tier 1 peers; colocated
repositories use native jj. All data commands have human and versioned JSON output.

**Planned:** a read-only Git submodule pin audit (`stw pins`,
[Spec 021](specs/021-submodule-pin-audit/spec.md), [guide](docs/pins.md); merged but hidden
until its conformance work item), stack inspection/diffs, checkpoint/resume, conflict
diagnosis, restacking, stacked PR writes, work transfer, change editing, upstream
synchronization and native GitButler integration. Bounded recovery evidence is not automatic restoration.

[Website](https://wstein.github.io/sourcesteward/) · [Roadmap](docs/roadmap.md) · [Backend contract](docs/backends.md) ·
[Specification](specs/001-project-foundation/spec.md) ·
[Plan](specs/001-project-foundation/plan.md) · [Tasks](specs/001-project-foundation/tasks.md)

## Try SourceSteward

Requires Go 1.27.2 or later. From the repository root:

```sh
make build
./bin/stw --help
./bin/stw version --json
./bin/stw inspect . --json
./bin/stw inventory . --json
./bin/stw inventory . --query dirty --scope disk --scope untracked --json
./bin/stw hosting repo . --json
```

Or run any command from source with `go run ./cmd/stw`, for example
`go run ./cmd/stw inventory . --json`.

| Implemented commands | Purpose and guide |
| --- | --- |
| `help`, `version` | Help at every command level; human/JSON version |
| `inspect`, `inventory` | [Native identity, workspaces, references, file scopes and state queries](docs/inventory.md) |
| `repos` | [Inventory up to 16 Git/jj repositories and direct submodules in one bounded read-only run](docs/repos.md) |
| `workspace plan`, `workspace apply` | [Review and create isolated Git worktrees/jj workspaces](docs/workspaces.md) |
| `workspace handoff`, `workspace retire-plan` | [Describe owned work and prepare retirement without deleting it](docs/workspaces.md) |
| `tidy plan`, `tidy apply` | [Review/apply local reference cleanup or separately selected owned-workspace retirement](docs/tidy.md) |
| `hosting repo`, `hosting pr`, `hosting ci` | [Resolve locally; read an explicitly numbered GitHub PR and exact-head CI](docs/hosting.md) |
| `gh jj repo`, `gh jj pr`, `gh jj ci` | [Optional jj-only gh extension sharing hosting operations](docs/hosting.md) |

See [all commands and worked examples](docs/commands.md) for workspace creation,
cleanup, PR/CI inspection and extension setup. Workspace/tidy apply requires a stored
plan, fresh checks and explicit `--authorize`. Planning may write private state;
retirement registration and filesystem removal are separate selections. No implicit
fetch, push, setup script or automatic cleanup runs.

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
