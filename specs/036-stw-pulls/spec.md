# Specification: `stw pulls`, a read-only inbox of all open pull requests

**Created**: 2026-10-22 | **Status**: proposed (spec-light: this page, [contracts/pulls.md](contracts/pulls.md) and [plan.md](plan.md)
are the whole design; no implementation yet).

GitHub's pull request inbox lists only PRs where the owner is requested reviewer or assignee. Bot PRs (dependabot, the
github-actions upgrade bot) never appear there, while the repository's PR tab shows them. `stw pulls` prints a **snapshot of every
open PR** of the workspace repositories (mode A), grouped by what needs action, and opt-in (`--owner`, mode C) of the repositories
of an owner or organisation outside the workspace. It is the poll-once, stateless sibling of [`stw watch`](../035-stw-watch/spec.md):
`watch` prints transitions, `pulls` prints the current table. Read-only, no state, no journal, no lock.

## Decisions (owner, 2026-10-22)

1. **A first, C opt-in.** Default scope is the workspace (or `--repo`); `--owner` adds repositories discovered outside it.
2. **Read-only and stateless.** One cycle of the `stw watch` host observer, nothing written to disk or to the forge.
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
stw pulls [--workspace [PATH] | --repo OWNER/NAME ...] [--owner OWNER ...] [--host HOST] [--config FILE]
          [--no-bots | --bots-only] [--author LOGIN ...] [--detail-drafts] [--limit N] [--human | --json]
```

- Repositories: `--repo` flags, else the workspace found exactly as `stw status`/`stw watch` find it (`watchcmd.Resolve`:
  root and initialized submodules with a GitHub `origin`; skipped ones are reported on stderr), else `repos` of `.stw/watch.toml`;
  none of them is a usage error (exit 2). `--owner` is additive: its repositories are appended and de-duplicated.
  `--host` defaults to `github.com` (one host per run, Spec 022 host rules).
- `--human` (default on a terminal) and `--json` (default otherwise) are mutually exclusive.
- `--limit N` (default 200, 1 to 1000) bounds the PRs **listed in detail** overall (see cost); PRs beyond it are counted in
  `summary.truncated` and shown as rows without verdict. Repositories are read in sorted order, newest PR first.
- `--detail-drafts`: also read `pulls/N` for drafts (default: drafts get no detail, `mergeable` is `null`).
- `--owner` takes the owner or organisation login (`[A-Za-z0-9-]`, at most 39 characters). Non-Unix: exit 2, `not supported on
  this platform` (the shared hosting reader is Unix-only like watch).

## Verdict and groups (exact rules)

Per PR, from the list item and (for non-drafts) `pulls/N` plus check runs and statuses of the exact head SHA (the
`internal/watch` check classification `none|pending|green|red`, unchanged):

| Field | Source |
| --- | --- |
| `bot` | `user.type == "Bot"` or login ends with `[bot]` |
| `age_days` | whole days since `created_at` (UTC) |
| `mergeable_state` | `clean`, `dirty`, `behind`, `blocked`, `unstable`, `has_hooks`, `draft`, `unknown` or null (not computed) |
| `reviewers` | `requested_reviewers` and `requested_teams` logins/slugs, at most 20 |

**Group** (first match wins): `draft` (draft true) > `conflict` (`mergeable_state` `dirty` or `mergeable` false, or `behind`) >
`checks` (class `pending`) > `blocked` > `ready`. **`ready` requires all of:** not draft, class `green` on the head SHA read in the same
cycle, `mergeable` true, `mergeable_state` `clean`. `blocked` is everything else, with one `reason`: `checks-red`, `no-checks`
(class `none`), `review-or-protection` (`mergeable_state` `blocked` with green checks: a required review or rule is open),
`unstable` (non-required check red), `unknown` (`mergeable` null or an unreadable detail). The review decision is **not** read
(no `pulls/N/reviews` endpoint is added); `blocked` by `mergeable_state` is GitHub's own answer, and requested reviewers are shown as
information. `ready` therefore means "GitHub considers it mergeable and the head is green", nothing about internal review rules.

## Cost model

Stateless, so the conditional requests of watch (ETags) are pointless and not used: every read is a plain 200. Per repository:
`ceil(open PRs / 25)` list pages (existing `pulls?state=open&per_page=25&page=N`, page size halving and 1 MiB cap as in Spec 035,
at most 80 pages), and for each non-draft PR, within `--limit`: one `pulls/N` plus one check-run and one status read (more pages only
past 100 items, as in watch): **3 reads per non-draft PR**, none for drafts without `--detail-drafts`. Example: 6 repositories with
40 open PRs in total, 10 drafts: 6 to 8 list reads plus 90 reads, well under the 5000 per hour core limit; `--limit 200` bounds one run to
about 600 reads plus the list pages. Reads are sequential per repository, at most 4 repositories in parallel, 30 s per request.
Rate-limit behaviour is that of Spec 035: `rate_limit` is read once at the end; on exhaustion (403/429 with evidence) the run stops
reading, prints what it has, marks unread repositories with `rate-limited` and exits 1; it never sleeps and never retries.

## Mode C: discovery with `--owner`

Decision: **`search/issues`**, not `users/OWNER/repos` plus a list read per repository. One search request returns up to 100 open PRs
with their repositories, so discovering the repositories that have open PRs costs `ceil(PRs / 100)` requests (at most 10), against
one list read for every repository of the owner (200 repositories would cost 200 mostly empty reads) plus the same repo listing
(`users/` and `orgs/` are two endpoints, the type of the owner is unknown). Allowlist growth is **one** new fixed shape in both options;
search is smaller in cost. Search is only used to **discover repository names**; the PR data comes from the same authoritative
per-repository cycle as mode A (search results lag the index and lack head SHA, mergeable and checks).

- New allowlist entry (Spec 022 FR-005, `pulls` reader only): `search/issues?q=user%3AOWNER+is%3Apr+is%3Aopen+archived%3Afalse&per_page=100&page=1..10`, OWNER
  validated as above, the query string generated by one function and matched exactly (no other `q`).
- Search has its own limit (30 requests per minute authenticated) and a 1000 result cap: at most 10 requests, so never throttled
  in one run; `total_count` above 1000 or `incomplete_results` true marks the owner `partial` (stderr, `errors`, exit 1) and the
  repositories seen are still read. Repositories also found by `--repo` or the workspace are read once.
- An owner whose search returns zero PRs is not an error. Private repositories appear only when the token can read them.
- Verify with a recorded fixture before WP-B is final that `user:` matches an organisation as well as a user (else the query adds
  `org:` as a second request).

## Security

GET only through the Spec 022 bounded go-gh transport (30 s, 1 MiB, no redirects, no cache); the token stays in the hosting layer and
is never printed or put in an error. Titles, branch names, author logins, labels and reviewer names are untrusted: human output
escapes control characters and bounds text exactly like `internal/watchcmd/human.go` (`esc`, 120 characters); JSON carries bounded
strings (2048 bytes). `--owner` and `--author` values are validated before any request. Repository content is data.

## Output and exit status

Output is on stdout only; diagnostics on stderr. Exit 0: all repositories read (also when nothing is open); 1: at least one
repository or owner discovery failed (partial output is printed); 2: usage error or unsupported platform. A failure of one
repository never hides the others. JSON is **one document** (the contract), not JSON Lines: a snapshot is one value with a
`summary`, groups and `errors`, like `stw status`, and a consumer reads it with a single `jq`; there are no transitions to stream.
Schema: `schemas/v1/pulls.schema.json`, unpublished until accepted (frozen, listed in `UNPUBLISHED_SCHEMAS`).

## Non-goals

No writes of any kind (no review request, comment, label, merge, Ready flip), no GraphQL (POST, Spec 022), no watching or state (use
`stw watch`), no review-decision or CLEAR-ledger reading, no closed or merged PRs, no replacement of `stw watch`, no cross-host runs.

## Risks

`search/issues` shape and organisation matching are verified only by recorded fixtures; list items may lack `user.type` on some hosts
(then `[bot]` suffix only); `mergeable` is computed lazily by GitHub, so a fresh PR may read `unknown` (blocked, reason `unknown`; run
again); a very large owner is bounded by `--limit` and the search cap, never silently (`summary.truncated`, `errors`).
