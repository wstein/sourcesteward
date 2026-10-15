# Implementation Plan: Project Foundation

**Branch**: `codex/001-project-foundation` | **Date**: 2026-10-15 | **Spec**: [spec.md](spec.md)

**Input**: Foundation specification and the user's clarification that SourceSteward must be jj-ready.

## Summary
Initialize a public Go/Cobra repository with an empty root commit, reproducible Spec Kit
workflow, working help/version commands and a static family-branded GitHub Pages site.
Keep CLI orchestration independent of any repository backend. Define Git, jj and GitButler
capabilities and effects before implementing repository operations.

## Technical Context
**Language/Version**: Go 1.27.2; Python 3 for documentation checks only.
**Primary Dependencies**: Cobra v1.10.2; pflag/mousetrap transitively pinned; Spec Kit 1.1.3
scaffolding; locally hosted SIL OFL fonts. No Node runtime, model API or application service.
**Storage**: No application persistence in foundation; tracked specifications and static assets.
**Testing**: Go table-driven command tests, output-writer failure/cancellation tests, Python
link/SVG/site-asset checks, and desktop/mobile visual inspection.
**Target Platform**: CLI on macOS/Linux/Windows; CI Ubuntu; static website in modern browsers.
**Project Type**: CLI plus static documentation.
**Performance Goals**: Local help/version finish without repository scans or network I/O.
**Constraints**: No Git/jj/GitButler mutations; no prompts; one result model for text/JSON;
Pages deploys only the exact main-push revision whose source checks passed.
**Scale/Scope**: Root help and one version command; one public landing page; one initial feature.

## Constitution Check
Pre-design and post-design gates PASS: shared human/machine identity; no mutation or network
operations in the CLI; tests before behavior; pinned justified dependency; explicit backend
capabilities and effects; original mark and licensed fonts; source checks before Pages.
No exception or unexplained complexity is required. The backend contract is documented now;
implementation waits for inspection/adapter specifications, avoiding unused speculative APIs.

## Project Structure
```text
cmd/stw/main.go           # process/cancellation boundary
internal/cli/root.go              # fresh Cobra command and stream/error contract
internal/cli/root_test.go         # behavior and isolation tests
specs/001-project-foundation/     # spec, plan, research, model, contracts, quickstart, tasks
.specify/                        # managed Specify templates/workflow and constitution
.agents/skills/                  # project-local Specify integration
.github/workflows/ci.yml          # same-run checks → exact-SHA Pages deployment
scripts/check-docs.py             # local links, SVG and site asset validation
scripts/package-pages.py          # allowlisted static artifact packaging
scripts/test_docs.py              # document-check/package failure cases
brand/README.md                   # original mark and family token provenance
docs/index.html                  # accessible static landing page
docs/assets/                     # original SVG, CSS, local fonts/licenses
docs/backends.md                  # jj-ready capability/effect contract
docs/roadmap.md                   # planned features; no fake working commands
```

Future backends remain behind operation/result boundaries: CLI → application operation →
selected Git/jj/GitButler adapter. No CLI command may infer Git semantics from a common field
name. Capability detection precedes any future repository action; see docs/backends.md.

## Phase 0: Research
[research.md](research.md) records verified dependencies, official primary sources,
GitButler/jj behavior and the exact-revision Pages approach. Bounded independent research
was completed; no open technical questions remain for this scaffold.

## Phase 1: Design and Contracts
[data-model.md](data-model.md) defines foundation version information and future backend
identity boundaries. [contracts/cli.md](contracts/cli.md) specifies implemented outputs and
exit behavior. [quickstart.md](quickstart.md) gives validation scenarios.

## Delivery and Verification
Implement only the foundation tasks. Run make check and race tests, inspect the page at desktop
and 375px widths, then publish the reviewed scaffold to main under the user's bootstrap request.
Configure Pages with GitHub Actions; the deploy job needs the check job, main-push condition,
exact github.sha checkout, pages/id-token permissions and github-pages environment.
Package only index.html and assets, with revision.txt provenance. Verify the live workflow and
site, then clone the finished project to the user's normal checkout location.
