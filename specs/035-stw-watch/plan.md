# Plan: `stw watch` (Spec 035)

Test-first throughout (a failing test before behaviour). Each work package is sized for one author (middle tier) and ends with its
tests green and `make check` clean. Package names are suggestions: `internal/watch` (core, state, config, restack) and
`internal/hosting` (reads). The CLI wiring follows `internal/cli/status.go`. Order: WP-A and WP-B in parallel (disjoint packages,
joined by the `Observer` interface of WP-A), then WP-C, WP-D, WP-E. WP-D depends on A and B; WP-E last.

| WP | Scope | Done when |
| --- | --- | --- |
| A core | `internal/watch`: types, `Observation` -> `Snapshot` diff, debounce, check class, event kinds `new-pr`, `head-changed`, `checks-*`, `checks-pending-timeout`, `mergeable-changed`, `ready-state-changed`, `base-changed`, `merged`, `closed`, `repos-changed`, `watch-*`; ids and tombstones; `state.json`, `journal.jsonl`, `heartbeat`, `lock` (0700/0600, atomic replace, truncated-line healing); poll-cycle order; injected clock and `Observer` interface | US1, US2 (kinds of A), US4, US10 source test pass on a fake `Observer` |
| B hosting reads | In `internal/hosting`: watch reader with new allowlist entries (`pulls?state=open&per_page=100&page=N` up to 10, `pulls/N/commits?per_page=100&page=N` up to 3, `rate_limit`), conditional GET (`If-None-Match`, 304 result, ETag return) usable only by the watch reader, rate-limit reading and slowdown policy, error classification to the `watch-error` codes, per-request 30 s context; the existing one-shot operations and their fixtures stay byte unchanged | US5 and the transport half of US6 pass; existing hosting tests unchanged |
| C CLI, config, discovery | `stw watch` command (flags, modes, `--human`, `--status`, exit codes), `.stw/watch.toml` strict parser (like `release.toml`), `--repo`, `--workspace` discovery reusing `stw status`/`stw sync` helpers, `repos-changed`, ignore authors/labels, hidden until accepted like `stw status` was | US7, US8, US9 pass |
| D restack-needed | candidate selection (`child-of-merged-branch`, `contains-merged-commits` via commits pages), old/new parent, argv, fork and null cases, ids with merge commit | US3 passes including the rebase-merge rewrite fixture |
| E docs, schema, acceptance | `schemas/v1/watch-event.schema.json` (+ status definition), freeze hash, `UNPUBLISHED_SCHEMAS` and test, `docs/commands.md`, `docs/json-contracts.md`, `docs/hosting.md` (pointer from the `ci-wait.sh` text), `docs/progress.md`, mark spec status; schema fixtures valid and broken variants; an acceptance run of the fake-forge suite; parity note against `watch2.sh` and removal plan for `scripts/ci-wait.sh` (the removal itself is a separate owner decision) | `make check` and `scripts/check-docs.py` pass; independent review of the exact head |

Risks: GitHub list `mergeable` is absent (hence the per-PR `pulls/N`); `merge_commit_sha` semantics for rebase merges (the tip
of the rewritten commits: verify with a recorded fixture from a real rebase-merge before WP-D is final); conditional requests
through go-gh must not enable the SDK cache (Spec 022 FR-004), so ETags are kept by the watcher itself in `state.json`.
No build, release, tag or publication is part of any WP.
