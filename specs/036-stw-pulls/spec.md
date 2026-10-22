# Specification: `stw pulls`, a read-only inbox of all open pull requests

**Created**: 2026-10-22 | **Status**: proposed (spec-light: this page, [contracts/pulls.md](contracts/pulls.md) and [plan.md](plan.md)
are the whole design; no implementation yet).

GitHub's pull request inbox lists only PRs where the owner is requested reviewer or assignee. Bot PRs (dependabot, the
github-actions upgrade bot) never appear there, while the repository's PR tab shows them. `stw pulls` prints a **snapshot of every
open PR** of the workspace repositories (mode A), grouped by what needs action, and opt-in (`--owner`, mode C) of the repositories
of an owner or organisation outside the workspace. It is the poll-once, stateless sibling of [`stw watch`](../035-stw-watch/spec.md):
`watch` prints transitions, `pulls` prints the current table. Read-only, no state, no journal, no lock.

## Decisions (owner, 2026-10-22)

1. **A first, C opt-in.** Default scope is the workspace (or the given `--repo`/`--workspace`); `--owner` adds repositories discovered outside it.
2. **Read-only and stateless.** A stateless cycle over the watch reader (see Implementation seams), nothing written to disk or to the forge.
3. **Bots are first-class**, marked in every output; filters narrow the view, the default hides nothing.

## User stories

- **US1 (inbox).** `stw pulls` in the workspace prints every open PR of every repository whose remote is GitHub, in groups
  `ready`, `checks` (waiting on checks), `conflict` (conflicting or behind), `draft`, `blocked`, each row showing `REPO#N`, title,
  author with a `[bot]` marker, age, check class, mergeable, requested reviewers and the verdict.
- **US2 (ready to merge).** A PR is `ready` only by the verdict rule below; the owner can merge from that group without
  opening the PR. The verdict never says anything about the foreman's review ledger (CLEAR on the exact head): that is out of scope.
- **US3 (bots).** `--bots-only` shows only bot PRs (dependabot, the upgrade bot); `--no-bots` hides them; `--author LOGIN`
  (repeatable) shows only those authors.
- **US4 (other repositories).** `stw pulls --owner wstein` also lists open PRs of the owner's repositories that are not in the
  workspace (archived repositories excluded), with the same rows.
- **US5 (machine).** `--json` prints one document an agent can filter with `jq`.
- **US6 (partial failure).** A repository that cannot be read is named on stderr and in the JSON `errors`; the rest is printed and
  the exit status is 1.

## CLI synopsis

```
stw pulls [--workspace [PATH]] [--repo OWNER/NAME ...] [--owner OWNER ...] [--host HOST]
          [--no-bots | --bots-only] [--author LOGIN ...] [--detail-drafts] [--limit N] [--human | --json]
```

- Repositories are the **union** of `--workspace [PATH]`, `--repo` flags and the repositories discovered by `--owner`, de-duplicated
  case-insensitively, sorted by name. With none of the three flags, `stw pulls` uses workspace discovery from the current directory
  exactly like `stw status` (root and initialized submodules with a GitHub `origin`; skipped ones reported on stderr); outside a workspace
  that is a usage error (exit 2). `.stw/watch.toml` is not read. At most 256 repositories (the `watchcmd.Resolve` limit), `--owner` results
  included; more is a usage error naming the count. `--host` defaults to `github.com` (one host per run, Spec 022 host rules).
- `--human` (default on a terminal) and `--json` (default otherwise) are mutually exclusive.
- `--limit N` (default 200, 1 to 500) is the **detail budget**: the number of non-draft PRs (drafts too with `--detail-drafts`) whose `pulls/N` and checks are read.
- `--detail-drafts`: drafts are also read and consume the budget (default: drafts get no detail, they are group `draft`).
- Filters: `--no-bots` and `--bots-only` are mutually exclusive (usage error, exit 2). `--author LOGIN` (repeatable; pattern
  `[A-Za-z0-9-]{1,39}` with an optional `[bot]` suffix) intersects with them (`--bots-only --author alice` is valid and empty unless
  alice is a bot). Filters apply **before** the budget is spent.
- `--owner` takes a login (`[A-Za-z0-9-]{1,39}`), repeatable. A bot marker is `user.type == "Bot"`, else a login ending in `[bot]`; a
  machine-user bot without either is not marked.
- Platform: `stw` as a whole is Unix-only today (release matrix, `internal/release`); `stw pulls` adds no restriction of its own.

## Reading order and budget (deterministic)

1. List every repository's open PRs (all pages, sorted by repository name). 2. Apply the filters. 3. Take the non-draft PRs (drafts too with `--detail-drafts`) of the
whole set newest first (`created_at`, then repository, then number) up to `--limit`, and read their detail and checks. The result
therefore depends only on the data, never on timing or parallelism. Rows past the budget are listed with `detail` false and reason
`not-read` and counted separately in `summary.not_read`.

## Implementation seams

Not `HostObserver.Observe`: it reads `branches/BASE`, `pulls/N` and checks for every PR (drafts too) and cannot apply a budget. `stw pulls`
is its own stateless cycle over the `watchcmd.Reader` interface (`WatchReader`), in a new `internal/pulls` package, with: (a) a separate list
decoder in `internal/hosting` for the extra fields (`user.type`, `created_at`, `html_url`, `requested_reviewers`, `requested_teams`,
labels); `hosting.PullItem` is **not** widened, it is the stored form of watch `state.json`; (b) exported unconditional (no ETag) helpers for
reading and classifying checks and for decoding `pulls/N`, shared with `watchcmd` (`readDetail` additionally decodes `head.sha`, `rebaseable`
and `mergeable_state` is already read); (c) the exported `esc` of `human.go`. No `branches/BASE` read is made: it is not needed for a
snapshot, so it is not in the cost model.

## Verdict and groups (exact rules)

Per PR, from the list item and (within the budget) `pulls/N` plus check runs and statuses of the head SHA (the `internal/watch` class
`none|pending|green|red`, unchanged). `age_days` = whole days since `created_at`; `reviewers` = requested reviewers and team slugs (at most
20). Evaluate **in this order, first match wins** (`group`, `reason`):

| # | Condition | group | reason |
| --- | --- | --- | --- |
| 1 | draft | `draft` | null |
| 2 | no detail read (past budget) | `blocked` | `not-read` |
| 3 | `pulls/N` `head.sha` differs from the listed head | `blocked` | `unknown` |
| 4 | class `red` | `blocked` | `checks-red` |
| 5 | class `none` | `blocked` | `no-checks` |
| 6 | `mergeable` null, or `mergeable_state` `unknown` | `blocked` | `unknown` |
| 7 | class `pending` | `checks` | null |
| 8 | `mergeable_state` `dirty`, or `mergeable` false, or `behind` | `conflict` | null |
| 9 | `mergeable_state` `blocked` | `blocked` | `review-or-protection` |
| 10 | `mergeable_state` `unstable` | `blocked` | `unstable` |
| 11 | `rebaseable` false | `blocked` | `not-rebaseable` |
| 12 | `mergeable_state` `has_hooks` | `blocked` | `unknown` |
| 13 | class `green`, `mergeable` true, `mergeable_state` `clean`, `rebaseable` true | `ready` | null |
| 14 | anything else | `blocked` | `unknown` |

The order matters because GitHub reports pending required checks as `blocked` and a conflicting PR as not rebaseable: tested earlier,
those rows would hide the `checks` and `conflict` groups. All five groups stay reachable (rows 1, 7, 8, 13 and the `blocked` rows).
Reason names, in order: `checks-red`, `no-checks`, `unknown`, `review-or-protection`, `unstable`, `not-rebaseable`, `not-read`.

Decisions: a repository **without any CI can never be `ready`** (reason `no-checks`); `ready` requires `rebaseable` true because the owner
merges by rebase; `has_hooks` is treated as `unknown` (documented in the guide). The review decision is not read (no `pulls/N/reviews`
endpoint); `blocked` by `mergeable_state` is GitHub's answer and requested reviewers are information. `ready` means "GitHub considers it
mergeable by rebase and the exact head is green", nothing about the foreman's CLEAR ledger.

## Cost model

Stateless, so the conditional requests of watch (ETags) are pointless and not used: every read is a plain 200. Per repository:
`ceil(open PRs / 25)` list pages (existing `pulls?state=open&per_page=25&page=N`, page size halving and 1 MiB cap as in Spec 035,
at most 80 pages), and for each non-draft PR, within `--limit`: one `pulls/N` plus one check-run and one status read (more pages only
past 100 items, as in watch): **about 3 reads per non-draft PR**, none for drafts without `--detail-drafts`. Example: 6 repositories with
40 open PRs, 10 drafts: 6 to 8 list reads plus 90 reads. `--limit` (maximum 500) bounds detail to about 1500 reads plus list pages; the
default 200 is about 600. An Actions `GITHUB_TOKEN` has 1000 requests per hour, a personal token 5000. Reads are sequential per repository, at most 4 repositories in parallel, 30 s per request.
Rate-limit behaviour follows Spec 035 for the core limit: on exhaustion (403/429 with evidence) the run stops reading, prints what it has, marks unread repositories `rate-limited` and exits 1; it never sleeps or retries. No final `rate_limit` read is made (nothing uses it); search responses never feed the core assessment.

## Mode C: discovery with `--owner`

Decision: **`search/issues`**, not `users/OWNER/repos` plus a list read per repository. One search request returns up to 100 open PRs
with their repositories, so discovering the repositories that have open PRs costs `ceil(PRs / 100)` requests (at most 10), against
one list read for every repository of the owner (200 repositories would cost 200 mostly empty reads) plus the same repo listing
(`users/` and `orgs/` are two endpoints, the type of the owner is unknown). Allowlist growth is **one** new fixed shape in both options;
search is smaller in cost. Search is only used to **discover repository names**; the PR data comes from the same authoritative
per-repository cycle as mode A (search results lag the index and lack head SHA, mergeable and checks).

- New allowlist entry (Spec 022 FR-005, see Amendments): `search/issues?q=user%3AOWNER+is%3Apr+is%3Aopen+archived%3Afalse&per_page=100&page=1..10&sort=created&order=asc`,
  OWNER validated as above, the query generated by one function and matched exactly (no other `q`). `user:` matches organisations too
  (verified by the reviewer; a recorded fixture of both stays in WP-B).
- Each result's `repository_url` must have the API host of the run and an owner equal (case-insensitively) to OWNER, else it is ignored
  with a `malformed-response` error; names are de-duplicated case-insensitively against workspace and `--repo` repositories.
- Limits: search has 30 requests per minute (separate from core) and a 1000 result cap, so one owner needs at most 10 requests; **repeated
  `--owner` can exceed 30**: on a throttle (403/429 with search evidence) that owner stops (`rate-limited`, partial), other owners continue
  only if budget remains, exit 1. `incomplete_results` true takes precedence over any `total_count` (also 0): the owner is `partial`
  (`search-incomplete`); `total_count` above 1000 is `search-too-many`; both exit 1 and the repositories seen are still read.
- An owner with zero open PRs (and `incomplete_results` false) is not an error. Private repositories appear only when the token can read them.

## Security

GET only through the Spec 022 bounded go-gh transport (30 s, 1 MiB, no redirects, no cache); the token stays in the hosting layer and
is never printed or put in an error. Titles, branch names, author logins, labels and reviewer names are untrusted: human output
escapes control characters and bounds text exactly like `internal/watchcmd/human.go` (`esc`, 120 characters); JSON strings are cut at 2048 bytes on a UTF-8 boundary. `--owner`, `--author` and `--repo` values are validated before any request. Repository content is data.

## Output and exit status

Output is on stdout only; diagnostics on stderr. Exit 0: all repositories read (also when nothing is open); 1: at least one
repository or owner discovery failed (partial output is printed); 2: usage error. A failure of one
repository never hides the others. JSON is **one document** (the contract), not JSON Lines: a snapshot is one value with a
`summary`, groups and `errors`, like `stw status`, and a consumer reads it with a single `jq`; there are no transitions to stream.
Schema: `schemas/v1/pulls.schema.json`, unpublished until accepted (frozen, listed in `UNPUBLISHED_SCHEMAS`).

## Amendments to Spec 022 (explicit)

Only the `stw pulls` reader may use them; one-shot operations and `stw watch` stay unchanged. **FR-005 (allowlist)**: the one search shape
above, matched in a **third guard mode**
(`one-shot`, `watch`, `pulls`) with its own pre-check. The `pulls` mode allows exactly the read shapes `stw watch` already has (list,
`pulls/N`, checks, commits) plus the single search shape and refuses everything else; `stw watch` and the one-shot operations keep their
own modes and **cannot reach the search shape** (tests: each mode refuses the shapes outside its set). FR-006 (1 MiB) and FR-007 (30 s per
request) apply as in Spec 035. The search shape is `search/issues?q=user%3AOWNER+is%3Apr+is%3Aopen+archived%3Afalse&per_page=100&page=N&sort=created&order=asc`: OWNER is
`[A-Za-z0-9-]{1,39}` checked byte-wise (no encoded, Unicode, `+`, `:` or control form), N is 1 to 10, the guard matches the string produced by
the one generator and nothing else (no other parameter, order, case of the escapes or `q`), and a reader built for `watch` refuses it before any request.
Spec 022 FR-005 carries the same text and a pointer to this section.

## Non-goals

No writes of any kind (no review request, comment, label, merge, Ready flip), no GraphQL (POST, Spec 022), no watching or state (use
`stw watch`), no review-decision or CLEAR-ledger reading, no closed or merged PRs, no replacement of `stw watch`, no cross-host runs.

## Risks

`search/issues` shape and organisation matching are verified only by recorded fixtures; `mergeable` is computed lazily by GitHub, so a fresh PR may read `unknown` (blocked, reason `unknown`; run
again); a very large owner is bounded by `--limit` and the search cap, never silently (`summary.not_read`, `errors`).
