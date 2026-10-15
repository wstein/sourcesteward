# Contributing to SourceSteward

Welcome. Start with the [project overview](README.md), [roadmap](docs/roadmap.md),
[architecture](ARCHITECTURE.md) and [development guide](docs/development.md).

For bugs, include the command, expected/observed behavior, operating system, tool/backend
versions and a minimal reproducer. Redact repository secrets and private paths. Report security
issues privately using [SECURITY.md](SECURITY.md). Use GitHub Discussions for questions.

For features, define user-visible outcomes through the [Spec Kit workflow](.specify/memory/constitution.md).
Keep implementation scope small. Preserve backend-specific capabilities and effects; jj-ready
design is required, even when only Git is implemented. Write failing behavior tests first.

Run make check and go test -race ./... before review. For site changes, inspect desktop,
narrow-screen, keyboard and reduced-motion behavior. Do not mark features verified until
their acceptance scenarios pass; update the [progress record](docs/progress.md).

Use codex/ or another descriptive topic branch and multiple atomic, focused Conventional
Commits (feat:, fix:, docs:, test:, chore:, ci:). Separate unrelated changes. PR descriptions
must explain behavior, validation and remaining limitations. Do not bypass checks, hooks or
human authorization. Dependabot proposals are reviewed with the same source checks.

Original contributions are licensed under [EUPL-1.2](LICENSE). Preserve third-party notices.
Follow the [Code of Conduct](CODE_OF_CONDUCT.md). No CLA or DCO sign-off is currently required.
