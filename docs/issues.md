# Listing open issues with `stw issues`

`stw issues` ([Spec 038](../specs/038-stw-issues/spec.md)) prints a **snapshot of every open issue** (pull requests
excluded) of the workspace repositories, each with one deterministic priority. GitHub's issue inbox lists only what is
assigned to you, mentions you or was opened by you, so a `security` issue opened by someone else, or anything opened by a bot,
never appears there; the repository's issue tab does show them, and so does `stw issues`. It is the sibling of
[`stw pulls`](pulls.md) for issues: read-only, stateless, no state, journal or lock, and the owner acts on the suggested
`gh issue view N -R OWNER/NAME`, `stw` never runs it. The JSON document is described by
[`issues.schema.json`](../schemas/v1/issues.schema.json) (unpublished, byte-frozen, see
[JSON contracts](json-contracts.md#issues-schema-v1-unpublished)).

## Use

```sh
stw issues                                  # the workspace of the current directory (as stw status finds it)
stw issues --workspace ~/work/anchorage     # a given workspace
stw issues --repo wstein/sourcesteward --repo wstein/crewbook
stw issues --owner wstein                   # also the repositories of this owner or organisation outside the workspace
stw issues --label bug --label regression   # any of these labels
stw issues --assignee @me                   # assigned to the authenticated user
stw issues --assignee @none --author alice  # unassigned issues opened by alice
stw issues --limit 50                       # print at most 50 rows, red first
stw issues --json | jq '.issues.red[] | .repo + "#" + (.number|tostring)'
```

Repositories, discovery, the 256 limit, `--host`, `--human` and `--json` work exactly as for `stw pulls`: the **union** of
`--workspace [PATH]`, the `--repo` flags and the repositories discovered by `--owner`, de-duplicated case-insensitively and
sorted by name; with none of the three, workspace discovery runs from the current directory like `stw status` (outside a
workspace that is a usage error, exit 2); `.stw/watch.toml` is not read. Output is human on a terminal and one JSON document
otherwise.

## Priority

Every issue gets one priority from the first matching rule, computed from the list entry only and over **all** labels the
forge returned (GitHub allows 100), before any display cut:

| # | Condition | Priority | Reason |
| --- | --- | --- | --- |
| 1 | a label equals `security` | red | `security-label` |
| 2 | the authenticated user is known and is an assignee | yellow | `assigned-to-you` |
| 3 | the author is a bot (`user.type` is `Bot`, or the login ends in `[bot]`) | violet | `bot` |
| 4 | a label equals `bug` | yellow | `bug-label` |
| 5 | anything else | blue | `none` |

Matching is **exact and ASCII case-insensitive**: `SECURITY` and `Bug` match, `security-review`, `area/security`, `type: bug`,
`security ` (trailing space) and Unicode look-alikes do not. A bot-opened security issue is still red, an assignment beats the
bot rule, and a bot's `bug` stays violet. The labels `security` and `bug` are fixed; there is no configuration. Red and yellow
rows carry the suggested command `gh issue view N -R OWNER/NAME` (effect `read`); blue and violet rows are not actions and carry
none.

## The authenticated user

Rule 2 needs the login the token resolves to, read once per run with `GET user` (no retry). It is read only when an issue read
is attempted. Its outcomes:

| Outcome | Result |
| --- | --- |
| `ok` | rule 2 is active |
| 403 or 404 **without** rate-limit evidence | `viewer` is `skipped` with reason `no-identity`, rule 2 is off, exit 0. Tokens such as an Actions `GITHUB_TOKEN`, an App installation token or a fine-grained token without the permission cannot read `user`; that is not a fault |
| 403 or 429 with rate-limit evidence | `viewer` is `error` `rate-limited`, the run stops reading, exit 1 |
| any other failure (401, network, 5xx, malformed answer) | `viewer` is `error`, rule 2 is off, the issues are still read, exit 1 |
| no repository to read | no request is made, `viewer` is `skipped` with reason `not-github-origin` |

After a `no-identity` skip and after a failed viewer read, stderr carries one line, `notice no-identity: ...` or
`notice viewer-unknown: ...`, saying that issues assigned to you are not flagged; stdout (the table or the JSON) never carries it.

`--assignee @me` needs the login. When it is unknown the run is **failed**: exit 1, the run-level error `viewer-unknown`, no rows
printed (an empty list would mislead), and every repository is `skipped` with reason `viewer-unknown` and counted neither as
read nor as failed. The login is printed only as a matched assignee in a row, never otherwise; the token never.

## Filters and the row cap

`--label NAME` (1 to 50 characters, no control characters, no leading or trailing space), `--assignee LOGIN|@me|@none` and
`--author LOGIN` (a GitHub login, optionally with the `[bot]` suffix; assignees may be bots such as Copilot) are repeatable.
Repeats of one flag are **alternatives**, different flags **intersect**, and the default hides nothing. Filters are applied
locally after the read: they never become a query parameter of a request, so a hostile value cannot reach the forge. `@none`
matches issues without assignee. Pull requests are never listed.

`--limit N` (default 200, 1 to 500) caps the printed rows after the sort (priority, then repository, then number), so a cap never
drops a red row before a blue one. The rest is counted in `summary.omitted`; `summary.counts` always covers everything read with the
filters applied, and is what [`stw status`](commands.md) shows.

Each human line is `REPO#N  AGE  AUTHOR  [labels: a, b]  [assigned: x]  TITLE  (reason)`, grouped under `RED (N)`, `YELLOW (N)`,
`BLUE (N)` and `VIOLET (N)`, with a `run:` line under every red and yellow row. A bot carries a `[bot]` marker only when its login
does not already end in it (never `dependabot[bot][bot]`); a label list cut at 20 ends in `+N more`. A line
`N open issues in M repositories, R red, Y yellow` closes the output, followed by the omitted count and the failures when there are
any. Untrusted text (titles, logins, labels) has control characters escaped and is bounded to 120 characters; JSON strings are cut
at 2048 bytes. No colour is used.

## Other repositories: `--owner`

`--owner OWNER` (a GitHub login, repeatable) discovers repositories outside the workspace with one fixed search,
`search/issues?q=user:OWNER is:issue is:open archived:false`, at most 10 requests per owner, with the same result rules, error codes
(`search-incomplete`, `search-too-many`, `rate-limited`, `malformed-response`) and exit status as the owner mode of
[`stw pulls`](pulls.md#other-repositories---owner). Search discovers repository names only; the issue data always comes from the
per-repository list, because search lags the index.

## Reading and failures

Per repository `ceil((open issues + open pull requests) / 100)` list reads of 100 entries (the endpoint returns pull requests
too; entries with a `pull_request` key are dropped and counted in `repos[].pulls_dropped`), at most 10 pages, plus one `user` read
per run. There is no per-issue read. Repositories are read sequentially, four at a time, with 30 s per request and 1 MiB per page.

- **Issues disabled.** A repository with issues disabled answers 410: that is not an error. It is `skipped` with reason
  `issues-disabled` and zero issues.
- **All or nothing.** A repository whose page fails is listed with its error code and none of its rows (a half list would make the
  counts lie). One failing repository never hides the others.
- **Truncation is never silent.** More than 10 pages (1000 entries, pull requests included) is `too-many-open-issues`: the repository
  is `partial`, the oldest 1000 entries are kept, exit 1. The same holds for `search-too-many` and `search-incomplete`.
- **Rate limit.** On exhaustion (403/429 with evidence) the run stops reading, prints what it has, marks the unread repositories
  `rate-limited` and exits 1; it never sleeps and never loops.

## Exit statuses

| Exit | Meaning |
| --- | --- |
| 0 | every repository was read (also when nothing is open); a `no-identity` viewer skip and a repository with issues disabled do not change it |
| 1 | at least one repository, owner discovery or the viewer read failed, `--assignee @me` without a login, or authentication is missing (before any request); partial output is printed |
| 2 | usage error; nothing is read (a repository set above 256 after `--owner` discovery is also exit 2, after the search requests were sent) |

## Safety and limits

- **Read-only and stateless.** Only fixed REST `GET` requests through the bounded hosting transport (30 s, 1 MiB, no redirects, no
  cache); the reader of `stw issues` is the only one whose guard admits the issue search shape, and the pull request search is refused
  to it (see [hosting](hosting.md#guard-modes-of-the-read-only-transport)). Nothing is written to disk or to the forge; no label,
  assignment, comment or close exists, and no GraphQL. The token is never printed.
- **Bounds.** 256 repositories per run, 20 labels and 20 assignees per row, 4 repositories in parallel, 10 pages per repository.
- **Platforms.** `stw` is Unix-only today; `stw issues` adds no restriction of its own.
- **Label conventions.** Repositories that spell their labels differently (`type: bug`, `area/security`) are not matched; the labels
  are fixed by design.
