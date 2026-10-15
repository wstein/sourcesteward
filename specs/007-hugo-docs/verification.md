# Generated documentation verification

## Design and implementation
Reviewed spec/plan/tasks before behavior changes. Read-only preimplementation analysis
mapped all 10 requirements to 12 tasks with no critical/high issues after explicit guide
allowlist and shortcode-rejection refinements. Official Hugo standard v0.168.0 artifact
names and committed hashes were verified against the official release/checksum manifest.
The macOS universal package was extracted without executing installation scripts; its
local runtime reports v0.168.0-e27a6760c5547237f87d353aebe0580feadb20f4.

## TDD and source evidence (2026-10-15)
- New Go reference test first failed with undefined metadata export. New Python tests first
  failed on absent allowlist/schema functions and Hugo config, then passed after implementation.
- Generated-page missing-theme regression first failed (the validator accepted a page without
  the local theme script), then passed after every generated shell required exactly one script.
- make check passes: Go tests/vet/module verification; 15 Python tests; 12 Node theme cases;
  documentation/architecture/whitespace checks. go test -race ./... passes.
- Nine generated HTML pages pass offline landmark, theme-script, internal link/fragment,
  project-subpath, runtime-asset and private-source exclusion checks.
- Reference includes the actual stw/help/version command tree and version --json flag;
  repeated metadata calls produce equal results. Generation uses go run -buildvcs=false;
  callbacks are not executed and Hugo enableGitInfo is false.
- Asset paths retain /sourcesteward/; the built artifact carries the exact requested SHA.
- Negative tests reject shortcodes, repository-escaping/unsafe links, missing/linked schema
  files, unexpected runtime assets, lost project-base paths and private-source leakage.

## Independent review and remaining gates
Independent reviewer inspected f801c06dfccfbfb88168be534b41a508c4907798 and independently
ran Go/Python/build/HTML checks. One P2 contradiction in old development Website text was
fixed; reviewer confirmed its resolution. Final integrated-SHA review and coordinator browser QA are recorded below.
No source push, PR, merge or publication is claimed by this feature author.

## Reviewed dependency integration
Reviewed006 source98f65dff5dc8be465a2d89e728c0e56fcacab3ce and final documentation
0cd52d36606fe46882151013f033f1779fa595e0 are merged without rewriting either feature.
Both source schemas, all five public guides and llms.txt are mandatory. The fifth guide,
CLI JSON contracts, is explicitly approved in the allowlist and served at /docs/json-contracts/.
Generated reference now includes stw inspect from the real integrated command tree. Landing
claims distinguish working bounded identity inspection from planned diagnosis/cleanup/native adapters.

Integrated make check passes 19 Python cases, 12 Node cases, Go/vet/module/architecture/
whitespace checks. Integrated go test -race ./... passes, including real-output JSON Schema
validation and runtime-validator exclusion. Public schema source root rejects all unexpected
files/directories and symlinks, not only children of v1. New negative tests for unexpected
schema-root files and mandatory agent guide/llms source failed before implementation.
Final generated HTML/schema-byte evidence, browser QA and exact-revision reviewer clearance
are recorded below. No publication is claimed.


## Final local acceptance (e563a6d376ea9aa4b24455db09f27c50dae26d98)
Current user-updated main/theme ancestry was merged without rewriting published history.
The merge changed only three progress-file blank lines relative to reviewed `4e8db2a`.
Independent reviewer cleared exact `e563a6d` and independently compared full artifact trees:
all HTML/assets/schema bytes match the `4e8db2a` artifact; only revision.txt changed. Twelve HTML
pages pass the generated-site validator, and both schemas and llms.txt preserve source bytes.

Coordinator browser QA passed landing, documentation index, JSON contracts and inspect
reference navigation under /sourcesteward/ in light/dark appearance. Desktop 1280×800,
mobile 375×812 and intermediate 720/768 widths show no document overflow. Narrow JSON
contract tables/code remain contained. Shared theme choice survives document navigation;
keyboard/focus, reload and System reset retain the unchanged feature 004 acceptance evidence.
The coordinator also fetched both schema URLs over local HTTP: status 200, application/json,
exact source bytes, and revision.txt matched e563a6d. These are local artifact checks, not
claims that the new routes are already deployed on public Pages.

Postimplementation analysis maps all 10 functional requirements to implementation/tests and
records no critical/high gaps. Its one LOW stale optional-llms phrase in data-model.md is
corrected to match the mandatory production gate. T012 is complete. Coordinator PR/CI
handoff is pending; no main merge, remote push or publication was performed by this author.

## Published-stack ancestry reconciliation
Reviewed schema head ab30b4d3941c492f131b34b216286343cd68e4d4 incorporates the owner-merged
main theme source and reviewed inspection ancestry. It is merged into the published007
branch without rebase, force push or rewriting published commits. Known conflicts retained
Hugo's pinned build, generated routes, narrow theme-script validation and isolated asset
unit tests; full package/schema tests still validate authoritative schemas and exact bytes.
Progress records now acknowledge merged PRs #4/#5 and preserve upstream schema/inspection
review evidence. A duplicate theme knowledge-map entry from automatic merging was removed.

The runtime/source/template/style/workflow inputs remain identical to reviewed79026dc.
Integrated make check passes: 19 Python cases,12 Node cases,Go/vet/module/architecture and
whitespace checks. A complete public artifact generated with a probe revision matches the
reviewed79026dc artifact byte for byte except revision.txt, including all12 HTML pages,
local assets, llms.txt and both schemas. The final exact merge SHA is rebuilt and compared
before coordinator handoff; no new browser behavior requires repetition. Publication of
the resulting branch head and updated PR #8 CI remain coordinator-owned.
