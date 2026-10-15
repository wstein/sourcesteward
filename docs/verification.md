# Foundation verification

## Local acceptance — 2026-10-15

- CLI tests first failed with missing implementation, then passed after implementation.
- Independent review reproduced invalid help topics succeeding and protocol-relative runtime
  assets bypassing offline checks. Both have failing regression evidence and are fixed.
- make check passes: formatting, Go tests/vet/module verification, five Python contract tests,
  local links/assets/SVG/font notices, feature evidence pointers, architecture and whitespace.
- Browser author inspection at desktop and 375×812: fonts loaded, readable page, mobile
  scrollWidth equals viewport width (375); skip link receives keyboard focus with visible outline.
- The final name/CLI and user-selected open branch/guide-rail mark are checked again before
  handoff. Family palette and local fonts retained. No full accessibility audit claimed.
- A separate read-only reviewer covered CLI, scripts, CI/Pages, community, backend and harness
  contracts. Final revision recheck and deployed-source evidence remain pending.

## Scope and remaining checks

Help/version only are implemented. Git, jj and GitButler adapters, cleanup, recovery and MCP
remain planned. No CLI release or user-repository mutation was performed. Linux CI, live Pages
revision and durable local checkout will be recorded after publication.
