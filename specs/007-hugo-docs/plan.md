# Implementation plan: Generated documentation

## Technical context
Go 1.27.2/Cobra v1.10.2; Python standard library build orchestration; Hugo standard
v0.168.0 official release binary. Hugo is a development-only dependency: no Go runtime
module changes. Existing local CSS/theme.js/fonts/assets remain the visual source.

## Constitution check
One actual command tree supplies human CLI and reference documentation. Generation is
read-only with respect to real repositories. Explicit staging limits untrusted content.
Tests precede behavior; this plan requires coordinator review before implementation.
No CLI backend interface or destructive action is added.

## Design
Keep docs/index.html as the landing source, rendered as a repository-owned Hugo home
layout after rebasing its local paths for the project base URL. A Python packager stages
only selected Markdown guides and generated command reference into a temporary Hugo
content tree. Original Markdown stays in docs/. A narrow link transform maps selected
guide links to public pages and other repository links to canonical GitHub source URLs.
Hugo templates own accessible guide shell and navigation; unsafe HTML is disabled.
Public assets and versioned schema files enter the staging tree only after validation.
The final artifact carries revision.txt, .nojekyll and required public llms.txt from feature 003. CLI reference comes from a small Go generation command calling an exported,
read-only CLI documentation function that walks the freshly constructed Cobra tree.
No command callback is executed; command flags and descriptions are emitted as Markdown.

## Build and CI
scripts/install-hugo.py downloads only fixed official URLs, compares committed SHA256,
and safely extracts the named executable. Linux amd64 supports CI; macOS arm64 uses the
official universal installer payload extracted locally without privileged installation.
make pages builds through the same packager used in CI. The check job installs pinned
Hugo, builds and validates the site, and uploads that exact artifact. A main-only deploy
job consumes it after checks without regenerating content or checking a different SHA.

## Risks and mitigations
Generated guide links may refer to private knowledge files: resolve those to GitHub source,
never copy recursively. Reviewed schema and inspection branches are integrated before final acceptance;
both schema contracts and the agent guide are now mandatory. Platform binary extraction is
strictly allowlisted; no installation scripts execute. Browser QA remains coordinator-owned.

## Design review refinement
The exact source→slug allowlist is recorded in contracts/site.md. Source shortcode
openers are rejected before Hugo receives Markdown, avoiding built-in remote embeds.
Final feature-006 integration requires both public version and inspect schemas.
The explicit allowlist includes docs/json-contracts.md as the fifth public guide.
