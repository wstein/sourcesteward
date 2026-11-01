# Listing open pull requests with `stw pulls`

`stw pulls` ([Spec 036](../specs/036-stw-pulls/spec.md)) prints a **snapshot of every open pull request** of the
workspace repositories, grouped by what needs action. GitHub's pull request inbox lists only the pull requests where you
are a requested reviewer or assignee, so bot pull requests (dependabot, the upgrade bot) never appear there; the
repository's pull request tab does show them, and so does `stw pulls`. It is the poll-once, stateless sibling of
[`stw watch`](watch.md): `watch` prints transitions, `pulls` prints the current table. The same snapshot for issues is [`stw issues`](issues.md). It is read-only and keeps no state,
journal or lock. The JSON document is described by [`pulls.schema.json`](../schemas/v1/pulls.schema.json) (unpublished,
byte-frozen, see [JSON contracts](json-contracts.md#pulls-schema-v1-unpublished)).

## Use

```sh
stw pulls                                   # the workspace of the current directory (as stw status finds it)
stw pulls --workspace ~/work/anchorage      # a given workspace
stw pulls --repo wstein/sourcesteward --repo wstein/crewbook
stw pulls --owner wstein                    # also the repositories of this owner or organisation outside the workspace
stw pulls --bots-only                       # only dependabot and the upgrade bot
stw pulls --no-bots --author alice          # only alice, no bots
stw pulls --limit 50 --detail-drafts        # read the detail of at most 50 pull requests, drafts included
stw pulls --json | jq '.groups.ready[] | .repo + "#" + (.number|tostring)'
```

Repositories are the **union** of `--workspace [PATH]`, the `--repo` flags and the repositories discovered by `--owner`,
de-duplicated case-insensitively and sorted by name. With none of the three, workspace discovery runs from the current
directory exactly like `stw status` (root and initialized submodules with a GitHub `origin`; skipped members are named on
stderr); outside a workspace that is a usage error. `.stw/watch.toml` is not read. At most 256 repositories per run, owner
results included. `--host` selects another GitHub host (one per run). Output is human on a terminal and one JSON document
otherwise; `--human` and `--json` force one and exclude each other.

## Groups and verdict

Every pull request lands in exactly one group, by the first matching rule:

| Group | Meaning |
| --- | --- |
| `ready` | GitHub considers it mergeable by rebase, the exact head has green checks and no other rule applies |
| `checks` | the checks of the head are still pending |
| `conflict` | conflicting (`dirty`, not mergeable) or behind its base |
| `draft` | a draft (no detail is read unless `--detail-drafts`) |
| `blocked` | everything else; the row names a reason |

Reasons of `blocked`: `checks-red`, `no-checks`, `unknown`, `review-or-protection`, `unstable`, `not-rebaseable`,
`not-read`. A repository **without any CI is never `ready`** (`no-checks`), `ready` needs `rebaseable` true because the
owner merges by rebase, and `has_hooks` counts as `unknown`. `unknown` also covers a head that changed between the list
and the detail read and a `mergeable` that GitHub has not computed yet: run again. `ready` says nothing about a review
ledger (a CLEAR on the exact head): the review decision is not read, and requested reviewers are information only. The
ordered rule table is in the [specification](../specs/036-stw-pulls/spec.md#verdict-and-groups-exact-rules).

Each human line is `REPO#N  AGE  AUTHOR  CHECKS  MERGEABLE  [reviewers: a, b]  TITLE`, blocked rows end with `(reason)`,
and a line `N open pull requests in M repositories, K ready` closes the table; further lines follow when some were not read (past `--limit`) or a repository or owner could not be read. Bots carry a `[bot]` marker (`user.type` is
`Bot`, or the login ends in `[bot]`). Untrusted text (titles, logins, reviewer names, states) has control characters escaped and is
bounded to 120 characters; JSON strings are cut at 2048 bytes. No colour is used.

## Review state and hints

Agents post their review verdict as a pull request comment ([Spec 046](../specs/046-stw-pr-review-state/spec.md)). `stw pulls` reads the
comments and shows a **review state** per row (a `REVIEW` word after the mergeable state, `review` in the JSON) plus **hints**
(`{hint, ...}`, `hints`). **A verdict is a claim, not proof**: only comments of an author that GitHub associates as `OWNER`, `MEMBER` or `COLLABORATOR` count (the
agents post through such an account); a line of anybody else, with another or no `author_association`, is ignored, counted (`ignored`) and
hinted. The state is what such a comment says ("per comment"), shown with the association of the decisive comment. `stw` checks only what
the data allows and never calls a review "independent".

The first line of a comment is read strictly:

```
Review: <CLEAR|FINDINGS> head=<40-hex> patch-id=<hex> pr=<n>
Carried-over: from=<40-hex> to=<40-hex> patch-id=<hex> pr=<n>
```

| State | Meaning |
| --- | --- |
| `clear`, `findings` | the latest `Review:` comment for the current head (created at or after the committer date of the head commit) |
| `clear-carried` | the latest decisive comment is a `Carried-over:` line whose chain of `from` to `to` heads, with equal `patch-id`, leads from a CLEAR to the current head |
| `clear-stale` | no comment for the current head, but an older head has a CLEAR |
| `unparsed` | a free-form verdict (`Verdict: CLEAR`, `Urteil: CLEAR`, ...) is newer than the decisive comment; it is never guessed |
| `none` | no verdict applies |

| Hint | Meaning |
| --- | --- |
| `clear-not-ready` | CLEAR (or carried) but the pull request is a Draft |
| `no-final-review` | no strict comment for the current head (`none`, `clear-stale`, `unparsed`) |
| `base-not-default` | the base is not the default branch |
| `parent-merged-retarget` | the base branch was the head of a merged pull request (among the 100 most recently updated closed ones): retarget or restack |
| `truncated-review-unknown` | the comment thread was cut (first page and last five pages): a verdict in the middle may be missing, so the review is unknown and the row is never `ready-mergeable` |
| `foreign-review-ignored` | a comment of another author that looks like a review line was ignored |
| `ready-mergeable` | CLEAR or carried, thread not truncated, group `ready` (green head, GitHub `clean`, rebaseable) and the base is the default branch |

```sh
stw pulls --hint clear-not-ready                 # CLEAR, but still a Draft
stw pulls --hint no-final-review                 # no final review comment for the head
stw pulls --review findings --review unparsed    # review states, OR within a flag
stw pulls --no-review                            # skip the comment reads
```

`--review STATE` and `--hint HINT` (each repeatable, OR within a flag, AND across both) keep the matching rows after all reads; a row
whose comments were not read matches neither. They cannot be combined with `--no-review` (exit 2). The comments of a row cost one read
(a long thread: up to six), the head commit one more only when a strict comment names the head; drafts are read too. Rows past `--limit` have `review` null.

## What can I merge now: `--mergeable`

```sh
stw pulls --mergeable          # only the rows with the hint ready-mergeable, oldest first, each with its reason
stw pulls --mergeable --json | jq '.owner_queue[] | .repo + "#" + (.number|tostring)'
```

The owner list answers "what can I merge now" for the owner, who decides and merges. It reads like the list and prints the rows with
`ready-mergeable` per repository (sorted by name), oldest `created_at` first, then by number. Because `ready-mergeable` needs the default
branch as base, only the bottom of a stack qualifies. Each entry has a reason built from facts, for example `CLEAR for head 3f2a9c1 per
comment 2026-11-01T09:00Z, checks green, mergeable clean, base main` (`CLEAR (carried from <sha>)` after a carry-over), and `then: #13, #14`,
the open pull requests that follow it (those based on its head branch, transitively; the head branch of a fork pull request is not a branch of
the repository). The last line counts the non-draft pull requests with `base-not-default` as waiting for a parent. The JSON keeps the groups of the
list and adds `owner_queue` (null without the flag) and `summary.waiting_for_parent`. `--mergeable` cannot be combined with `--review`,
`--hint` or `--no-review` (exit 2). It is a CLEAR per comment and a green head, not an approval: nothing here merges, labels or marks Ready.

## Filters and the detail budget

`--no-bots` and `--bots-only` exclude each other (exit 2); `--author LOGIN` (repeatable, optional `[bot]` suffix)
intersects with them, so `--bots-only --author alice` is valid and empty unless alice is a bot. Filters apply **before**
the budget is spent. `--limit N` (default 200, 1 to 500) is the number of non-draft pull requests (drafts too with
`--detail-drafts`) whose `pulls/N` and checks are read, newest first (`created_at`, then repository, then number), so the
result depends only on the data. A non-draft row past the budget is listed as `blocked` with reason `not-read` and counted
in `summary.not_read`; a row inside the budget whose read failed has the same reason but is reported through
`repos[].error` and `errors` instead.

GitHub computes a pull request's mergeability lazily: the first `pulls/N` read often returns `mergeable` null or state `unknown` for a pull
request that is in fact mergeable, and a second read moments later says `clean`. So that a single run does not under-report `ready`, the
command re-reads such a row once, at the end of the pass (after the other reads gave GitHub time), reading `pulls/N` only (no checks), and
only for a non-draft row whose head checks are green. There is no sleep and no second re-read, so `unknown` can still remain; run the
command again then. The re-reads cost rate limit (at most one read per such row) but not the `--limit` budget, and none is made after a
rate limit.

Cost: about three reads per non-draft pull request (`pulls/N`, check runs, statuses) plus `ceil(open / 25)` list pages per
repository, and no read for drafts without `--detail-drafts`. The default limit is about 600 reads, the maximum about 1500;
an Actions `GITHUB_TOKEN` allows 1000 requests per hour, a personal token 5000.

## Other repositories: `--owner`

`--owner OWNER` (a GitHub login, repeatable) discovers repositories outside the workspace with one fixed search,
`search/issues?q=user:OWNER is:pr is:open archived:false`, at most 10 requests per owner (a user or an organisation;
archived repositories are excluded, private ones appear only when the token can read them). Search is used only to
discover repository names; the pull request data comes from the same per-repository reads as the workspace. An owner with
no open pull requests is not an error. These cases are errors (`errors[]` with `owner` set) and exit 1, and the repositories
seen are still read:

| Code | Cause |
| --- | --- |
| `search-incomplete` | GitHub reported `incomplete_results` |
| `search-too-many` | more than 1000 results (the search cap) |
| `rate-limited` | the separate search limit of 30 requests per minute was hit; later owners are not sent |
| `malformed-response` | a result whose `repository_url` does not belong to this host and owner was ignored, also when another case applies |

## Exit statuses

| Exit | Meaning |
| --- | --- |
| 0 | every repository was read (also when nothing is open) |
| 1 | at least one repository or owner discovery failed, or authentication is missing (before any request); partial output is printed |
| 2 | usage error; no pull request is read (a repository set above 256 after `--owner` discovery is also exit 2, after the search requests were sent) |

A failure of one repository never hides the others: it is named on stderr and in `errors`, and `summary.repos_failed`
counts it. On rate-limit exhaustion (403/429 with evidence) the run stops reading, prints what it has, marks the
unread repositories `rate-limited` and exits 1; it never sleeps and never loops, and makes at most one bounded re-read per pull request whose mergeability GitHub has not computed yet (non-draft, green checks, `mergeable` null or state `unknown`; never after a rate limit).

## Safety and limits

- **Read-only and stateless.** Only fixed REST `GET` requests through the bounded hosting transport (30 s, 1 MiB, no
  redirects, no cache); the reader of `stw pulls` is the only one whose guard admits the search shape, `stw watch` and the
  one-shot hosting operations cannot reach it. Nothing is written to disk or to the forge; the token is never printed.
  No review request, comment, label, merge or Ready flip exists, and no GraphQL.
- **Bounds.** 256 repositories per run, 20 names per list, at most 4 repositories in parallel, 80 list pages per repository.
- **Platforms.** `stw` is Unix-only today; `stw pulls` adds no restriction of its own.
