# Plan: `stw status` online and action-oriented (Spec 037)

Test-first throughout (a failing test before behaviour), fake forge, no network. Each work package is for one middle-tier author and
ends with its tests green and `make check` clean. Seams: `internal/pulls` (cycle and `Result`), `internal/hosting` (guard mode, typed
readers), `internal/watchcmd` (`esc`, `ValidRepo`, `Resolve`), `internal/stwsync` (local status, unchanged), a new
`internal/overview` (action model and ordering, pure functions over data), `internal/cli/status.go` (wiring and rendering). Order:
**WP-0**, then WP-A, WP-B and WP-C in parallel (disjoint after WP-0), then WP-D, then WP-E and WP-F. Spec 036 must be merged or be the
base (`internal/pulls` is reused).

| WP | Scope | Done when |
| --- | --- | --- |
| 0 seams | `internal/pulls`: an exported accessor of the per-repository rows and counts of a `Result`; `pulls.Run` accepts the repository list of status. `internal/hosting`: `guardStatus` enum value (`admitsWatchShapes` true, `admitsSearch` false); an orchestration type `Remote` with per-source outcome, 90 s overall deadline (`--timeout`), 15 s per request, deadline skip distinct from failure, 4-way pool, rate-limit stop. Existing pulls, watch, hosting tests byte unchanged | accessor, guard and pool tests pass; all existing tests pass untouched |
| A pulls integration | Status-side use of the pulls cycle: repository set from `origin` (`ValidRepo`, skip reasons), one `pulls.Run` with defaults, rows to the action model inputs; degradation mapping to source states | US3 passes; counts equal the `stw pulls` document for the same fixture (parity); re-read cases covered |
| B releases, tags, repo, guard | Typed decoders for `releases` (`draft`, name, tag, `created_at`, `html_url`, assets), `tags` (name, commit sha), `repos/OWNER/NAME` (`default_branch`, `html_url`, `permissions`); `Link` paging to 10, `too-many-releases`, `page-too-large`; visibility enum; unreleased-tag rule (semver, at most 5); Spec 022 FR-005 and 033 pointers (committed with this spec) | US4 passes; guard tests; existing `ReleaseEntry` and release tests untouched |
| C CI state (required) | `ci-main`: `branches/DEFAULT` and the existing check reads, class; `behind_origin` via local object lookup; `ci-release`: latest and unreleased tags' commits from `tags`, workflow files from `.stw/release.toml`, newest run via `runs?head_sha=...&page=1`; `no-release-workflows` when undeclared; tag pattern from `release.toml` for the unreleased-tag rule | US5 passes: red main and failed tag run are `red` items |
| D action model | `internal/overview`: the 20 category rules, priority, id, ordering, caps, commands from validated tokens, `effect`, notices and counts; pure, table-driven, no I/O | every row of the spec table and every precedence rule tested; ordering is permutation-invariant; commands never contain unvalidated strings |
| E CLI and output | `--offline`, `--icons`, `--timeout`, default-online wiring (offline path byte-identical), sections Needs action / Repositories / Notices / summary, colour and `NO_COLOR`, three icon sets with the ascii fallback, stderr progress only on a tty, exit codes, `esc` | US1, US2, US6, US7, US9 pass; golden files for the three icon sets with and without colour |
| F schema, docs | `schemas/v2/status.schema.json`, valid and broken fixtures, digest in the freeze test, `UNPUBLISHED_SCHEMAS` (check `package-pages.py` handles the new `v2/` directory), `docs/commands.md`, `docs/json-contracts.md`, `docs/hosting.md` (the `status` mode), `docs/pulls.md` cross-link, `docs/progress.md`, `docs/README.md`; acceptance run US1 to US9 | `make check`, `scripts/check-docs.py` pass; independent review of the exact head |

## Test plan and mutation targets

In-process HTTP injection seam (Spec 022 FR-009), no network. A mutation target is a one-line change a test must kill.

| Area | Tests | Mutation targets |
| --- | --- | --- |
| Offline unchanged (US2) | golden text and v1 JSON of `--offline` equal to the Spec 033 goldens; a transport that fails on any request; no credential read | send a request with `--offline`; change a v1 key; print Needs action offline |
| Guard (B) | `guardStatus` admits the watch set, releases, tags, runs, `repos/OWNER/NAME`; refuses search, other `releases` queries (`per_page=99`, extra parameter, `page=11`, encoded `%2F`, escape case), POST, redirect, other host; other modes admit what they admitted before | admit search in `guardStatus`; widen `releaseRoute`; make `guardWatch` admit search; drop the method check |
| Action model (D) | one fixture per table row; bots tested first (a ready bot PR is `pr-bot` only); draft never an item; precedence for one subject; text cap 10 per priority with N more, JSON complete; id stability; ordering invariant under shuffled repositories and goroutine timing | swap two priorities; test `pr-ready` before the bot rule; list drafts; sort by completion time; unstable id (include a time) |
| PR parity (A) | status counts and groups equal the `stw pulls` document of the same fixture; re-read: unknown then known, unknown twice (blocked `unknown`), exactly one re-read, none for known PRs, no sleep | loop the re-read; treat `unknown` as `ready`; skip bots; spend budget on drafts |
| Releases and tags (B) | drafts among published ones; `no-write-access` never reported as zero and no draft item; `unknown`; `maintain`/`admin` as write access; `Link` next across 2 pages; page 10 with next gives `too-many-releases`; 1 MiB gives `page-too-large`; unreleased tag rule (older tags ignored, prerelease, non-semver, 5 cap) | report zero when not visible; ignore `draft`; follow the `Link` URL instead of the typed page; count every old tag |
| CI (C) | main red/pending/green/none; `behind_origin` with and without the commit in the local store; release run failed/cancelled/in progress; no `release.toml` declaration | red as green; fetch to decide behind-origin; run a workflow not declared |
| Degradation (US7) | no token: local view plus one notice, exit 0; network error (nothing asked) exit 0; one repository 500; rate limit mid-run stops all requests and never sleeps (clock injected, never advanced); deadline marks sources `skipped`/`deadline`, exit 0, one notice; failed remote read exit 1 with partial output; `page-too-large` section degrade exit 1; local failure exit 1; items never change the exit status | exit 1 on a missing token or a deadline skip; one notice per repository; continue after rate limit; hide the local block on remote error |
| Output (E) | each icon set, colour on/off, `NO_COLOR`, non-tty fallback to ascii and non-UTF-8 locale; progress only on a tty and only on stderr; copy-paste: stripping ANSI leaves the same text; `esc` of titles, tags, names with control characters, ANSI, 10 kB; token canary never in stdout, stderr, JSON, commands; commands only from validated tokens | skip `esc` for the tag; write progress to stdout; colour without a tty; a command containing a forge string |
| Schema (F) | valid and broken fixtures for every new key, `additionalProperties` false, v2 freeze digest, v1 digest unchanged, `UNPUBLISHED_SCHEMAS` test, `command`/`effect` null pairing | stale digest; extra keys allowed; `effect` set with null command |
| Concurrency | at most 4 repositories in flight, sequential within a repository, output order independent of timing | unbounded fan-out |

Authors run `go test -race` once per package before the first push; reviewers run the affected tests and their own mutation probes.

## Risks

See the spec. No build, release, tag or publication is part of any WP; a `stw` build for dogfooding needs the owner's approval.
