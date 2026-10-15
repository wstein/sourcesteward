# Public site contract

Base URL: https://wstein.github.io/sourcesteward/.
Routes: / (landing), /docs/ (guide index), /docs/<slug>/ (selected guides),
/reference/ (CLI command tree), /reference/stw[-<command>]/ (command pages),
/schemas/v1/<name>.schema.json (verbatim contracts), /revision.txt (40-hex SHA plus newline).
Theme: system default, explicit light/dark persistence through existing local theme.js.
CLI-reference export must construct metadata only; it may not execute Run/RunE, load a
repository or access network. Generation order is stable and excludes dynamic timestamps.

## Guide allowlist (source → public slug)
- docs/development.md → /docs/development/
- docs/backends.md → /docs/backends/
- docs/roadmap.md → /docs/roadmap/
- docs/agent-guide.md → /docs/agents/
- docs/json-contracts.md → /docs/json-contracts/

These are the entire guide allowlist. Other Markdown links point to canonical GitHub
source, not copied files. Selected source containing Hugo shortcode delimiters `{{<` or
`{{%` is rejected before staging, even with unsafe HTML disabled. No source shortcode
is executed. Mandatory schema names are version.schema.json and inspect.schema.json. Both are
required in production builds after reviewed feature 006 integration; absence is an error.
