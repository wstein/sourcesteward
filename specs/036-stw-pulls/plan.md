# Plan: `stw pulls` (Spec 036)

Test-first throughout (a failing test before behaviour), fake forge, no network. Each work package is for one middle-tier author and
ends with its tests green and `make check` clean. Packages: `internal/pulls` (core), `internal/hosting` (search read), CLI wiring
like `internal/cli/status.go`; discovery and rendering reuse `internal/watchcmd` (`Resolve`, `esc`) and `internal/stwsync`.
WP-A first; WP-B in parallel (disjoint package); WP-C last.

| WP | Scope | Done when |
| --- | --- | --- |
| A core | `internal/pulls`: one stateless cycle per repository through the watch host observer / `WatchReader` (list pages, `pulls/N` for non-drafts, check class from `internal/watch`), rows, bot flag, group and verdict rules, filters, `--limit`, parallelism 4, partial failure and rate-limit stop; fake-forge tests for US1, US2, US3, US6, every group and reason, `mergeable` null, drafts without detail | the rule table of the spec is covered row by row; no write, no state file (asserted) |
| B owner mode | `internal/hosting`: `search/issues` allowlist entry with the exact query generator and OWNER validation (an explicit amendment to Spec 022 FR-005, listed in the spec), result decode (repository from `repository_url`), 10-page bound, `incomplete_results` and 1000 cap, error classification as in watch; recorded fixtures (user, organisation, empty, truncated); existing hosting tests unchanged | US4 passes; attack strings (`q` injection, `OWNER` with `+` or `:`) refused before sending |
| C CLI and output | `stw pulls` command and flags, workspace/`--repo`/config resolution, `--human` (escaping, `[bot]`, groups), `--json` document, exit codes, non-Unix stub (exit 2), `schemas/v1/pulls.schema.json` (valid and broken fixtures, freeze test, `UNPUBLISHED_SCHEMAS` and its test), `docs/pulls.md`, `docs/commands.md`, `docs/json-contracts.md`, `docs/README.md`, `docs/progress.md`; end to end acceptance run of US1 to US6 on the fake forge | `make check`, `scripts/check-docs.py` pass; independent review of the exact head |

Risks (see the spec): search semantics for organisations, `user.type` availability, lazy `mergeable`. No build, release, tag or
publication is part of any WP; a `stw` binary build for dogfooding needs the owner's separate approval.
