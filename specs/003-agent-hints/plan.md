# Implementation Plan: Agent Hints

**Branch**: `codex/003-agent-hints` | **Date**: 2026-10-15 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `specs/003-agent-hints/spec.md`

## Summary
Adapt SheetMill's help-discovery pattern to SourceSteward: append a root-only notice, add an accurate agent guide and root index, and mechanically validate destinations offline. Preserve current output/error contracts and foundation architecture.

## Technical Context
**Language/Version**: Go 1.27.2; Python 3 source checks; Markdown/text documentation.
**Primary Dependencies**: Existing pinned Cobra and Go/Python standard libraries; no additions.
**Storage**: Repository files only.
**Testing**: Go CLI behavior/writer-failure tests and Python documentation-check tests.
**Target Platform**: Existing CLI-supported Go platforms and contributor checkouts.
**Project Type**: CLI with static documentation.
**Performance Goals**: Constant-size help append; no network or subprocess work.
**Constraints**: Help/version-only scope; unchanged JSON and error classifications; offline tests; no broader Pages artifact set.
**Scale/Scope**: One help notice, two guidance files, source-check coverage and navigation links.

## Constitution Check
- Shared human/agent contract: root help is understandable; version JSON stays unchanged; no prompt/editor/pager.
- Evidence and ownership: no consequential operations introduced; docs retain task/authority limits.
- Specify/verify: reviewed artifacts precede implementation; failing tests cover help and documentation validation first.
- Composition: existing Cobra and standard libraries only; no backend or network adapter.
- Publication: source edits do not imply remote write or merge authorization.
All gates pass before research and after this design; no amendment required.

## Project Structure
```text
specs/003-agent-hints/{spec,plan,research,data-model,quickstart,tasks}.md
specs/003-agent-hints/contracts/help.md
specs/003-agent-hints/checklists/requirements.md
internal/cli/root.go
internal/cli/root_test.go
scripts/check-docs.py
scripts/test_docs.py
docs/agent-guide.md
llms.txt
README.md
docs/README.md
docs/development.md
docs/progress.md
docs/feature-status.json
```
**Structure Decision**: Extend the existing help callback and documentation checker. Keep URL targets literal and repository-owned; validate their mapped files offline rather than downloading content. The guide/index remain source documentation, outside the unchanged static Pages package.

## Complexity Tracking
No constitution violations or additional abstractions required.
