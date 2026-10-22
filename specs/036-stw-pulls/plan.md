# Plan: `stw pulls` (Spec 036)

Test-first throughout (a failing test before behaviour), fake forge, no network. Each work package is for one middle-tier author and
ends with its tests green and `make check` clean. Seams are named in the spec ("Implementation seams"): `internal/pulls` (core cycle over
`watchcmd.Reader`), `internal/hosting` (extra-field list decoder, search read, guard mode), shared exported helpers in `internal/watchcmd`
(checks read, `pulls/N` decode with `head.sha` and `rebaseable`, `esc`), CLI wiring like `internal/cli/status.go`.
Order: **WP-0** then WP-A and WP-B (they share `internal/hosting` and `watchcmd` through WP-0, otherwise disjoint), then WP-C.

| WP | Scope | Done when |
| --- | --- | --- |
| 0 hosting and shared seams | `internal/hosting`: the separate list decoder (`user.type`, `created_at`, `html_url`, reviewers, teams, labels; `PullItem` unchanged), three-mode guard (`one-shot`, `watch`, `pulls`); `watchcmd`: exported unconditional checks and `pulls/N` helpers (`readDetail` also decodes `head.sha`, `rebaseable`), exported `esc`; existing watch and hosting tests byte unchanged | new decoder and helper tests pass; every watch test passes untouched |
| A core | `internal/pulls`: stateless cycle (list all repositories sorted, filters, newest-first budget over the whole set, detail and checks for non-drafts), bot flag, the ordered group/reason table (every row), partial failure, rate-limit stop, parallelism 4; fake-forge tests for US1, US2, US3, US6 | each table row and the budget order are covered; no write and no state file (asserted) |
| B owner mode | the `search/issues` allowlist entry (exact query generator, OWNER validation, search guard mode refused from watch), result decode (`repository_url` host and owner check, case-insensitive dedupe), 10-page bound, `incomplete_results` precedence, 1000 cap, repeated-owner throttle; recorded fixtures (user, organisation, empty, incomplete, truncated); Spec 022 FR-005 amendment text | US4 passes; attack strings (`q` injection, `+`, `:` in OWNER) refused before sending |
| C CLI and output | `stw pulls` command and flags (usage errors of the filter rules, 256 cap), workspace discovery, `--human`, `--json` document, exit codes, `schemas/v1/pulls.schema.json` (valid and broken fixtures, freeze test, `UNPUBLISHED_SCHEMAS` and its test), `docs/pulls.md`, `docs/commands.md`, `docs/json-contracts.md`, `docs/README.md`, `docs/progress.md`; acceptance run of US1 to US6 | `make check`, `scripts/check-docs.py` pass; independent review of the exact head |

Risks: see the spec. No build, release, tag or publication is part of any WP; a `stw` build for dogfooding needs the owner's approval.
