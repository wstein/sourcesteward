# Specification: `stw issues`, a read-only inbox of open issues, and its place in `stw status`

**Created**: 2026-10-22 | **Status**: proposed (spec-light: this page, [contracts/issues.md](contracts/issues.md) and
[plan.md](plan.md) are the whole design; no implementation yet). It is the sibling of [Spec 036](../036-stw-pulls/spec.md)
(`stw pulls`) for issues and specifies **WP-G of [Spec 037](../037-stw-status-overview/spec.md)** (the issue source of `stw status`),
which refers here. Spec 037 is merged: the action model, priorities, grouping, exit rules and guard mode `status`
used below are defined there.

`stw pulls` answers "which pull requests are open"; nothing answers "which issues are open" across the workspace. GitHub's issue
inbox lists only what is assigned to, mentions or was created by the owner. `stw issues` prints a **snapshot of every open issue**
(pull requests excluded) of the workspace repositories, each issue with one deterministic priority, and opt-in (`--owner`) of the
repositories of an owner or organisation outside the workspace. `stw status` gets the part that deserves attention: a count per
repository and the red and yellow issues as action items. Read-only, stateless, no state, journal or lock; `stw` suggests
(`gh issue view N -R OWNER/NAME`), the owner acts.

## Decision (owner, 2026-10-22, "Option E")

| Option | Rating | Why |
| --- | --- | --- |
| A. Issues stay out of `stw status` | 2/5 | Nothing new to build, but a `security` issue or one assigned to the owner stays invisible in the one command the owner runs three times a day. |
| B. Count of open issues per repository in `stw status` only | 3/5 | Cheap, but a count of 40 says nothing about the one that matters; no drill-down. |
| C. Per-repository list with priorities in `stw status` | 4/5 | Shows what matters, but without a drill-down command the status output grows with every backlog, and the rest of the backlog is unreachable. |
| D. Owner-wide search for issues (`search/issues`) | 4/5 | Finds repositories outside the workspace, but search is a small, lagging budget (30 per minute, 1000 results) and not an authoritative per-repository read; useful only as discovery, not as the primary read. |
| **E. Staged: counts plus actionable issues in `stw status`, drill-down command `stw issues`** | **5/5, chosen** | The status line says how many are open and puts only red and yellow issues into "needs action"; `stw issues` lists everything (blue and bot issues too), with owner discovery as opt-in (D's search, used for discovery only), exactly like `stw pulls` next to `stw status`. |

Derived rules: (1) the per-repository list is the authoritative read, search only discovers repository names (as in Spec 036);
(2) one priority per issue, one rule table, shared by `stw issues` and `stw status` (one package, no second classifier);
(3) read-only: no label, assignment, comment, close; `stw` never runs a suggested command; (4) a failing source never hides the rest.

## User stories

- **US1 (inbox).** `stw issues` in a workspace prints every open issue of every repository whose remote is GitHub, grouped by
  priority `red`, `yellow`, `blue`, `violet`, each row showing `REPO#N`, title, author with a `[bot]` marker, age, labels,
  assignees, the reason of the priority and a suggested `gh issue view` command for red and yellow rows.
- **US2 (security first).** An issue labelled `security` is `red` and is never hidden silently: the author type and the budget cannot hide it, and a drop by `--limit`, a filter or truncation is always counted or noticed (`summary.omitted`, `summary.counts`, `partial`).
- **US3 (mine).** An issue assigned to the authenticated user is `yellow`; so is a `bug`. The owner sees "assigned to you" without
  opening GitHub.
- **US4 (filters).** `--label`, `--assignee`, `--author` narrow the view; the default hides nothing. Pull requests are never listed.
- **US5 (other repositories).** `stw issues --owner wstein` also lists open issues of the owner's repositories outside the workspace
  (archived excluded).
- **US6 (machine).** `--json` prints one document an agent can filter with `jq`.
- **US7 (partial failure).** A repository that cannot be read is named on stderr and in `errors`; the rest is printed; exit 1.
- **US8 (status).** `stw status` shows, per repository, `issues: N open (R red, Y yellow)`, and the red and yellow issues as items
  of "Needs action", grouped like every other item; blue and bot issues only as counts.

## CLI synopsis

```
stw issues [--workspace [PATH]] [--repo OWNER/NAME ...] [--owner OWNER ...] [--host HOST]
           [--label NAME ...] [--assignee LOGIN|@me|@none ...] [--author LOGIN ...] [--limit N] [--human | --json]
```

- Repositories, discovery, the 256 limit, `--host`, `--human`/`--json` and `--owner` syntax are **exactly those of `stw pulls`**
  ([Spec 036](../036-stw-pulls/spec.md#cli-synopsis)): the union of `--workspace [PATH]`, `--repo` and the `--owner` discovery,
  de-duplicated case-insensitively and sorted by name; with none of them workspace discovery as in `stw status` (outside a workspace
  a usage error, exit 2); `.stw/watch.toml` is not read.
- **Filters are applied locally, after the read**, to the decoded issues. They are never sent to the forge (no `labels=`,
  `assignee=` or `creator=` query parameter exists in the fixed shapes), so a hostile value cannot reach a request. Repeats of one
  flag are alternatives (any of); different flags intersect.
  - `--label NAME`: 1 to 50 characters (UTF-8, no control characters, no leading or trailing space); matched
    **case-insensitively (ASCII case folding only) and exactly** against every label of the issue (`bug` does not match `bugfix` or `type: bug`).
  - `--assignee LOGIN`: GitHub login (`[A-Za-z0-9-]{1,39}` with an optional `[bot]` suffix, because bots such as Copilot can be assignees; ASCII case-insensitive match against the
    assignees); `@me` is the authenticated user (see "The authenticated user"); `@none` matches issues without assignee. `@` cannot occur
    in a login, so the two words never collide with one. `--assignee @me` when the login could not be determined is a failed run: exit 1,
    error `viewer-unknown`, no rows printed (an empty list would mislead).
  - `--author LOGIN`: as `--assignee` without the two words; the author's bot marker is part of the login (`dependabot[bot]`).
- `--limit N` (default 200, 1 to 500) is the **row cap of the output**: after filters and sorting (priority, then repository
  case-insensitively, then number) at most N rows are printed or emitted; the rest is counted in `summary.omitted`. It bounds output
  only: the read is bounded by the page limit below, and counts (`summary`, `repos[].open`) always cover everything read. Sorting by
  priority first means a cap never drops a `red` row before a `blue` one.
- Platform: Unix only; Windows stays unsupported and this spec adds no Windows content.

## Reading shape (verified: no such shape exists today)

`internal/hosting` has no issues shape and no `user` shape (the watch set is PR list, `pulls/N`, `pulls/N/commits`, branches,
commits, `rate_limit`; the one-shot set is releases, tags, runs, repositories). This spec therefore adds **three fixed read-only GET
shapes** (Spec 022 FR-005, see Amendment), each produced by one generator and matched exactly:

1. **Per-repository list**: `repos/OWNER/NAME/issues?state=open&per_page=100&sort=created&direction=asc&page=N`, N 1 to 10, OWNER and
   NAME validated by the existing `validRepo` rules. GitHub's issues endpoint returns pull requests too; **entries with a
   `pull_request` key are dropped** (counted in `repos[].pulls_dropped`, information only). Paging: the page number is typed by the
   reader; `Link: rel="next"` is only the signal that another page exists and its URL is never followed (as in Spec 037). More than
   10 pages (1000 entries, pull requests included) is `too-many-open-issues`: the repository is `partial`, the oldest 1000 entries
   (sort created ascending) are kept, exit 1. Size: 1 MiB per response (FR-006), 100 entries; a larger answer is `page-too-large`.
2. **Authenticated user**: `user`, exactly that string, no query. Read once per run (see below).
3. **Owner search** (only `--owner`): `search/issues?q=user%3AOWNER+is%3Aissue+is%3Aopen+archived%3Afalse&per_page=100&page=N&sort=created&order=asc`,
   N 1 to 10, OWNER a login (ASCII letters, digits, single interior hyphens, 1 to 39, checked byte-wise). It is the shape of Spec 036
   with `is%3Aissue` instead of `is%3Apr`, with the **same** result rules: `repository_url` must have the API host of the run and an owner
   equal to OWNER (else ignored with `malformed-response`), names de-duplicated case-insensitively against workspace and `--repo`
   repositories, 100 per page, at most 10 pages, `incomplete_results` true takes precedence (`search-incomplete`), `total_count` above
   1000 is `search-too-many`, a throttle (403/429 with search evidence) stops that owner (`rate-limited`), other owners continue only if
   budget remains; all of it exit 1 and the repositories seen are still read. Search discovers **repository names only**; the issue data
   always comes from the per-repository list (search lags the index).

Repositories with issues disabled answer 410: that is **not an error**: `repos[].state` `skipped`, reason `issues-disabled`, zero issues.

### Why a new guard mode `issues`, not an extension

Options: (a) add the shapes to `guardPulls`; (b) a fifth mode. Decision: **a new guard mode `issues`** (the mode enum becomes
`one-shot`, `watch`, `pulls`, `status`, `issues`). Least privilege: `stw issues` needs none of the PR-specific shapes (`pulls/N`,
commits, branches, check runs), so admitting them would widen the surface for no use, and `guardPulls` carries the PR owner search that
`stw issues` must not reach. The `issues` mode admits **exactly**: the per-repository list, `user`, the issue search, and the
existing one-shot set that every mode already admits; it refuses the watch set and the PR search. Every other mode keeps refusing all
three new shapes **except** `status`, which admits the per-repository list and `user` (it reads issues for the action model) and still
refuses both searches (status takes no `--owner`, see below). One enum value, one admits-predicate per shape; no existing mode gains or
loses a PR shape. Tests: each mode refuses exactly the shapes outside its set (matrix in the plan).

## The authenticated user

"The authenticated user" is the **login the token resolves to**, as returned by `GET user` (`login`, validated against the login
rules, at most one read per run, no retry). No already-admitted shape gives it (`rate_limit` does not; the issue list does not echo
the viewer), so this is the one unavoidable extra fixed shape. It is read through the same bounded transport, only to compare logins
case-insensitively with `assignees[].login`. Outcomes:

- `ok`: the login is known; the rule "assigned to you" is active.
- **Order of evaluation (no overlapping branches).** (1) A 403 or 429 **with rate-limit evidence** (as defined in Spec 037, "Rate
  limit") is `rate-limited`: source `viewer` `error`, all further requests of the run stop, exit 1. (2) Otherwise a **403 or 404 without
  rate-limit evidence** is the **user-lookup exception**: source `viewer` `skipped`, reason `no-identity`, a notice, exit 0. A token class
  that cannot read `user` (an Actions `GITHUB_TOKEN`, an App installation token, which gets 403 on `GET /user`, a fine-grained token
  without the permission) is not a retryable fault; 404 is kept as a candidate for the same reason. (3) Any other failure (401, network,
  5xx, malformed answer) is source `viewer` `error`, exit 1 (the generic Spec 037 rule: an attempted request failed). In (1) and (3) the
  rule "assigned to you" is inactive for the run and a notice (`viewer-unknown`) says so; issues are still read and classified by the
  other rules, except after a rate-limit stop. Spec 037's exit table lists 401/403 as exit 1; **the user-lookup exception is the only
  deviation of this spec** from it (Spec 037 has its own, separate `not-found` skip for the release workflow). In case (2) the rule is
  inactive and `--assignee @me` fails as above.
- **No request, no lookup.** The `user` read is made only when an issue read is attempted: with `--offline`, with no usable token, or
  when no repository is on GitHub, no `viewer` request is made and the source is `skipped`: reason `offline` with `--offline`, `no-token` without a usable token, and `not-github-origin` when no repository is on GitHub.
- The login is never printed except as a matched assignee in a row; the token never.

## Priority and action model (deterministic, one level per issue)

Per issue, from the list item only (no per-issue detail read). Evaluate **in this order, first match wins** (`priority`, `reason`):

| # | Condition | priority | reason |
| --- | --- | --- | --- |
| 1 | any label equals `security` (ASCII case-insensitive, exact) | red | `security-label` |
| 2 | the viewer login is known and equals (case-insensitively) an assignee | yellow | `assigned-to-you` |
| 3 | the author is a bot (`user.type == "Bot"`, else a login ending in `[bot]`) | violet | `bot` |
| 4 | any label equals `bug` (ASCII case-insensitive, exact) | yellow | `bug-label` |
| 5 | anything else | blue | `none` |

Rationale of the order: a `security` label is the one signal that must never be hidden, so it beats the author type (a bot-opened
security issue is still red); an explicit assignment to the owner beats the bot rule (the owner was asked); a bot-opened issue that is
neither is noise-class (`violet`, like bot pull requests of Spec 037), also when it carries `bug`. Labels are evaluated over **all**
labels the API returned (up to GitHub's 100 per issue) **before** any display truncation; the `reason` field records why, so a
`security` label past the 20 labels kept in a row still makes it red. "Case-insensitive" everywhere in this spec means **ASCII case folding only**, not Unicode folding (`strings.EqualFold` is not used: U+017F LATIN SMALL LETTER LONG S and U+212A KELVIN SIGN must **not** match `security` or `bug`). Matching is exact: `security-review`, `area/security`,
`type: bug` match nothing (residual: repositories that spell labels differently; configurability is an open question). `stw` reads
labels as data and never trusts the forge's own `type` or `security_advisory` fields.

`age_days` = whole days since `created_at`. **Suggested command** (hint, `effect` `read`, never run): `gh issue view N -R OWNER/NAME`
for red and yellow rows (blue and violet rows carry none, they are not actions); OWNER/NAME validated, N an integer.

## Integration into `stw status` (WP-G of Spec 037)

Status consumes the **`issues.Result` of the same package** (no second list, decoder or classifier), in the new source `issues` of
each GitHub repository with local state `ok` (the other Spec 037 sources and their independence are unchanged).

- **Request budget and reads**: per repository `ceil((open issues + open PRs) / 100)` list reads (usually 1; the endpoint returns
  both), plus **one** `user` read per run. Reads are sequential within a repository, in the status worker pool of **4 repositories**
  (a separate pool for the issues source, like the other non-PR sources of Spec 037), 15 s per request, inside the 90 s deadline.
  No search, no `--owner` (see below). Guard mode `status` admits the two shapes.
- **Per repository block**: one line `issues: N open (R red, Y yellow)`; `N` counts every open issue read (blue and violet included; the violet count is the bots count),
  the counts per priority are in the JSON; a truncated read (`too-many-open-issues`) prints `issues: N+ open (R red, Y yellow, truncated)`. A repository whose issues are disabled prints `issues: disabled`; a repository whose read
  failed prints `issues: not read (CODE)` with CODE an error code such as `rate-limited` or `network`; a skipped one prints
  `issues: not read (REASON)` with REASON one of `deadline`, `no-token`, `offline`.
- **Items** (appended to the table of Spec 037; categories 26 to 28 so that no existing number changes; ordering is priority first,
  then category number, as defined there):

  | # | Category id | Subject | Condition | Priority | Suggested command (hint, never run) |
  | --- | --- | --- | --- | --- | --- |
  | 26 | `issue-security` | issue number | rule 1 | red | `gh issue view N -R OWNER/NAME` |
  | 27 | `issue-assigned` | issue number | rule 2 | yellow | `gh issue view N -R OWNER/NAME` |
  | 28 | `issue-bug` | issue number | rule 4 | yellow | `gh issue view N -R OWNER/NAME` |

  Blue (rule 5) and violet (rule 3) issues are **never items**: counts only (`issues` data in the JSON and the line above); the
  drill-down is `stw issues --repo OWNER/NAME`, named in the `remote:` line when any exist.
- **Grouping**: Spec 037's rule applies unchanged: items with the same (priority, category, repository) form one human line with the
  count and the first 3 identifiers (`+N more`), here `3 security issues: #12, #15, #19`; a single item keeps its URL
  (`https` only, the issue's `html_url`); the JSON keeps one item per id; the human section caps lines per priority at 10.
- **Item id**: `issue-security:wstein/sourcesteward:12`. Title (human and JSON): `issue #12 is labelled security: TITLE`,
  `issue #12 is assigned to you: TITLE`, `bug issue #12: TITLE`; TITLE passes `esc` and the 120-character bound.
- **Failure inside the source**: a failed or rate-skipped issue read is **no issue item**; the repository gets one `read-error` item
  (row 5 of Spec 037, subject `issues`, hint `stw issues --repo OWNER/NAME`). A deadline skip is not an item.
- **Deadline, exit and skip semantics: inherited from Spec 037 by reference.** Requests prevented or cancelled by the deadline are
  `skipped`/`deadline` (exit 0), a failed attempted request is an error (exit 1), rate-limit exhaustion stops all remaining reads
  without sleeping, no usable token means no request. The only additions are the `viewer` source and the reasons `issues-disabled` and
  `no-identity` above. **No new exit code.** Items and priorities never affect the status.
- **`--owner` in status: not in this spec.** Status has no owner or filter flags (Spec 037) and the issue search is a separate, small
  budget (30 requests per minute) with its own throttling semantics; adding it would also add a guard mode surface to `status`. Owner
  discovery stays in `stw issues --owner`. A later spec can add `--owner` to status once the owner is willing to pay the seam; recorded
  as an open question.
- **JSON additions** (additive, frozen-schema process, see the contract): `actions[].category` enum +3, `actions[].subject.kind`
  +`issue`, `repos[].remote.sources.issues`, the `viewer` and `issues` entries of `sources`, notice codes `viewer-unknown` and
  `no-identity`, source skip reason `issues-disabled`. Spec 037 carries pointer sentences to this section.

## Cost model

Stateless; every read is a plain 200 (no ETags). `stw issues`, per repository: `ceil((open issues + open PRs) / 100)` pages (at most
10), plus one `user` read for the run, plus for `--owner` at
most 10 search requests per owner. There is no per-issue detail read, so the cost is pages, not issues. Examples: 6 repositories with
40 open issues and 12 PRs in total: 6 list reads plus 1 `user` read. Worst case: 256 repositories times 10 pages plus owner searches,
about 2600 reads, which is over the 1000 an hour of an Actions `GITHUB_TOKEN` and within the 5000 of a personal token; the run stops
at the rate limit (below). Reads: sequential per repository, at most 4 repositories in parallel, 30 s per request (15 s in status).

## Concurrency, rate limit, partial failure

Parallelism 4 across repositories; output sorted deterministically after all reads (priority, repository, number), independent of
timing. **Rate limit** as Spec 035/036: on exhaustion (403/429 with evidence) the run stops reading, prints what it has, marks unread
repositories `rate-limited` and exits 1; **never sleeps, never loops**; no final `rate_limit` read. Search throttling is separate
(core budget and search budget are different limits). **Partial failure**: a repository is all-or-nothing: if a page fails, the
repository's rows are discarded and it is listed with its error code (a half list would make the counts lie), except the two explicit
truncations (`too-many-open-issues`, `search-too-many`/`search-incomplete`) whose partial result is marked. One failing repository
never hides the others. Exit 0 all read (also when nothing is open); 1 at least one repository, owner discovery or the `viewer` read
failed; 2 usage error.

## Security

GET only through the Spec 022 bounded go-gh transport (30 s, 1 MiB, no redirects, no cache); the token stays in the hosting layer and
is never printed, logged or put in an error, notice, command or JSON. Titles, logins, labels and error strings are **untrusted**:
human output escapes control characters (including ANSI escape sequences, CR, NUL, bidi controls) and bounds text exactly like
`internal/watchcmd/human.go` (`esc`, 120 characters); JSON strings are cut at 2048 bytes on a UTF-8 boundary; error messages are
sanitized and at most 512 bytes. `--repo`, `--owner`, `--label`, `--assignee` and `--author` values are validated before any request.
Hostile input the tests must cover: a title with ANSI or newline or 10 kB; a label of 10 kB or with a control character; a label
named `SECURITY` or `Security` (red) and `security ` with trailing space or `security​` (not red, no normalisation); 100 labels with
`security` last (red); an assignee of the viewer in different case; an author login with `[bot]`; a `pull_request` key with `null`
value (still present: dropped) and a key that only looks similar; `number` 0, negative, a string or huge (malformed-response); `labels`
as null, objects without `name`, a non-string `name`; `user` null (deleted ghost user: author `ghost`, not a bot); a page with 101
entries; a 1 MiB plus response; `Link` pointing to another host (ignored: never followed); a `repository_url` of a foreign owner
(ignored). Repository content is data.

## Output and exit status

stdout carries output, stderr diagnostics. JSON is **one document** (not JSON Lines): `schemas/v1/issues.schema.json`, unpublished
until accepted (frozen: digest in `internal/contracts/schema_freeze_test.go`, listed in `UNPUBLISHED_SCHEMAS` of
`scripts/package-pages.py`, like `pulls.schema.json`). Human output, per non-empty priority a heading `RED (N)`, `YELLOW (N)`,
`BLUE (N)`, `VIOLET (N)`, then one line per issue
`REPO#N  AGE  AUTHOR  [labels: a, b]  [assigned: x]  TITLE  (reason)`, and a `run:` hint line under red and yellow rows. AUTHOR carries
a `[bot]` marker only when the login does not already end in `[bot]` (never `dependabot[bot][bot]`). A final line
`N open issues in M repositories, R red, Y yellow` follows, plus the omitted count and the failures. No colour unless a terminal and
`NO_COLOR` unset. Exit status as above.

## Amendment to Spec 022 FR-005 (explicit)

Only the `stw issues` reader and the `stw status` online reader may use the new shapes; one-shot operations, `stw watch` and
`stw pulls` stay unchanged and **cannot reach them**.

1. **Three fixed GET shapes**, generated by one function each and matched exactly (no other parameter, order, escape case or value):
   `repos/OWNER/NAME/issues?state=open&per_page=100&sort=created&direction=asc&page=N` (N 1..10), `user`, and
   `search/issues?q=user%3AOWNER+is%3Aissue+is%3Aopen+archived%3Afalse&per_page=100&page=N&sort=created&order=asc` (N 1..10; OWNER a
   login, ASCII letters, digits, single interior hyphens, 1 to 39, checked byte-wise).
2. **A fifth guard mode `issues`** admitting exactly the three shapes plus the existing one-shot set; refusing the watch set and the PR
   search. **`status`** (Spec 037) additionally admits the per-repository list and `user`, and still refuses both searches.
   `one-shot`, `watch` and `pulls` refuse all three new shapes. Tests: each mode refuses the shapes outside its set; the `pulls`
   search and the `issues` search refuse each other.
3. FR-006 (1 MiB) and FR-007 (30 s per request; status 15 s) apply as in Specs 035 to 037. Spec 022 FR-005 carries a pointer sentence
   to this section; Spec 037 carries one for `status`.

## Non-goals

No writes (no label, assignee, comment, close, transfer), no GraphQL, no closed issues, no issue bodies or comments, no timeline, no
milestone or project reading, no staleness or duplicate detection, no per-issue detail read, no watching or state (`stw watch` is the
transition tool), no execution of suggested commands, no owner discovery in `stw status`, no cross-host runs, no Windows content.

## Risks

Label conventions differ between repositories (`security` and `bug` exact match is deliberate and documented); the issues endpoint
mixes pull requests into the page budget (a repository with more than 1000 open items is truncated, never silently); the `user` read is
unavailable to some token classes (assigned issues then are not flagged, with a notice); search is verified only by recorded fixtures
and lags the index; an Actions `GITHUB_TOKEN` has 1000 requests an hour.

## Decisions recorded (foreman, 2026-10-22)

1. **Labels are fixed.** `security` and `bug` (case-insensitive, exact) are the only labels; configurability is a later spec if needed. Rationale: no config surface before there is a second convention to serve.
2. **No `--owner` in `stw status`** in this spec; a later spec may add it. Rationale: the search budget and its guard surface do not belong in the glance command.
3. **Schema.** The status v2 schema is unpublished: the additions are additive within v2 and re-register the frozen digest through the existing process; no v3 while it is unpublished.
4. **`GET user` 403 or 404 is a skip** (reason `no-identity`: the "assigned to you" rule is disabled with a notice, exit 0), any other error on it is a failed attempted request (exit 1). A 403 with rate-limit evidence is checked first and is `rate-limited` (exit 1). This is the **user-lookup exception** to the exit table of Spec 037 (see "The authenticated user"); Spec 037's `not-found` skip is a separate, existing one. Rationale: the token class, not a retryable fault, withholds the identity.
5. **Assigned to you wins over the bot rule.** An assignment to the owner is always actionable.
6. **Bot-opened issues are only counted in status** (the violet count of the `issues` data is the bots count) and listed in `stw issues`; no status items for them.
7. **Bounded paging is never silent.** A repository whose open items (issues plus pull requests) exceed the 10 pages (1000 entries) is `partial` with error `too-many-open-issues` and a notice, exit 1, like `too-many-open-prs` and `search-too-many` of the pulls owner search; the oldest 1000 are kept.
