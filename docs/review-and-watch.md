# Pull request review and watch

[SourceSteward 101](../README.md) · [Command reference](command-reference.md) · [GitHub access](status-and-sync.md#github-access)

## GitHub reads for review and readiness (internal API)

`stw::github::reads` (`stw status --review` uses `issue_comments`): `Client::issue_comments`, `pr_reviews`, `check_runs`, `commit_statuses`. Every request is counted in the stats (`stw status --stats`) and none starts after the `--timeout`.
REST GET, 100 per page, under a `Budget` (10 pages, 500 items, 16 KiB text per body, 60 s). A list that hit a bound, lost a
page or had a malformed entry is `Observed` with `incomplete: Some(reason)`; `Read::is_complete()` is true only for a fully
read list, so partial or unreadable evidence cannot make a pull request Ready. 401, 403 (no permission, for example Checks)
and 429/exhausted rate limit are `Read::Denied`, 404 is `NotFound`, other failures `Failed`; none is a crash. GitHub text is
untrusted: cut, then escaped reversibly (`\\`, `\n`, `\r`, `\t`, `\u{hex}` for control, zero-width and direction
characters). Slug and SHA are validated before they enter a path. Tests: `tests/it/github_reads.rs` with the fixtures in
`tests/fixtures/github/` (see its README for which are recorded and which are hand-written).

## Review state from comments (internal API, M3)

`stw::review` (used by `stw status --review`, below). Pure functions on the comments read by
`github::reads`; nothing is posted and nothing is marked Ready.

- Grammar (first line of the comment, nothing else on it): `Review: <CLEAR|FINDINGS> head=<40 lower-case hex> patch-id=<40 hex> pr=<n|none>` and
  `Carried-over: from=<40 hex> to=<40 hex> patch-id=<40 hex> pr=<n|none>`. Fixed keys and order, single spaces, no Markdown, tab or CR.
  `n` is a decimal number from 1 without leading zeros. `pr=none` is a local review (scope `Local`); dotagents is to adopt
  `pr=([1-9][0-9]*|none)`. A line that reads like a review line but breaks the grammar is never accepted: it is the finding
  `unparsed-review-variant`. Other text is not a review line.
- `evaluate(scope, head, comments, trusted)` and `evaluate_read(.., &Read<Comment>, ..)`. Status: `clear`, `clear-carried`,
  `findings`, `no-review`, `unverified-author`, `unknown`. Trust is a list of logins (case-insensitive, whole login, an empty
  login never; empty list: nobody); `author_association` is not a gate. Only comments of trusted logins decide. Lines of other
  authors are findings (`untrusted-author`); if they are the only lines for the head the state is `unverified-author`, never
  `clear`. Lines about another head that play no part in the decision are `stale-head`.
- A trusted line with another `pr=` (or `pr=none` in a pull request scope, a number in the local scope) is the finding `wrong-pr`.
  If it names the head (a review of it or a carry-over to it) and is newer than the decision, or there is no decision, the state is
  `unknown`; it also ends any carry-over chain that passes through the head it names.
- The latest line for the head by `(created_at, id)` wins: a later `FINDINGS` cancels a `CLEAR`, a later `CLEAR` replaces `FINDINGS`.
  A carry-over to the head counts if the latest line before it that names `from` is a `CLEAR` review, or a carry-over that is
  itself valid. An invalid carry-over to `from` is skipped and the search continues with the earlier lines that name `from`
  (so an older CLEAR of `from` still carries); a trusted line of another pr that names `from` ends the search with an invalid
  chain. A chain is invalid (finding `invalid-carry-chain`, with the reason in `detail`) when: no such source exists; every link
  must declare the patch-id of the root CLEAR (`patch-id-mismatch`; the declared values are compared, nothing is recomputed, M4
  does that from git); a link returns to a head the chain already cleared (`repeated-head`); a `FINDINGS` review of any head of
  the chain (the root and every intermediate head, not the final head) was posted after that head was cleared
  (`member-findings`); more than 16 links. The final head's own later `FINDINGS` simply wins as the latest line. Chains are
  computed iteratively, so thousands of carry-overs are fine.
- `unknown` (with `reasons`): the read is partial, denied, not found or failed; a body has a malformed escape; a trusted line has no
  usable `created_at`; a trusted `unparsed-review-variant` or a trusted wrong-pr line naming the head is newer than the decision (or
  there is no decision). It is never read as `no-review`.
- Provenance per line: comment id, author, `created_at`, `updated_at`, `edited`. An edited comment is only flagged: a decisive or
  chain comment with `edited` counts normally and the flag is in the result. `ReviewState::to_json()` is the stable JSON
  (version 1); `stw status --review` prints it as `review_state`.
- Tests: the dotagents corpus `tests/review_corpus/review-line-examples.md` (copy of `docs/review-line-examples.md`, dotagents
  c7c343c), `tests/it/review_lines.rs`, data-driven cases `tests/review_states/*.json` (`tests/review_states.rs`), `tests/it/review_read.rs`.

## Review state in `stw status --review`

`stw status --review` reads, for each open pull request, its conversation comments (one list read of `GET /repos/OWNER/NAME/issues/N/comments`, 100 per page) and derives the state with `review::evaluate_read` for the exact head commit of the pull request (`head.sha`). Nothing is posted. Trust: the logins of the comments whose `author_association` is OWNER, MEMBER or COLLABORATOR (as in Go) are the trusted authors; lines of anybody else are findings and never decide.

- Text: the pull request line ends `[<head branch>, review <state>, <merge state>]`; the states are `clear`, `clear-carried`, `findings`, `no-review`, `unverified-author` and `?` (`unknown`); `review not read` for a pull request beyond the limit. Under the pull requests of a repository one line counts them (`reviews: 1 clear, 2 no-review, 1 ? (hints: 1 clear-not-ready)`, zero counts left out). A pull request with hints ends `... CLEAN] {clear-not-ready, base-not-default}`. `--long` adds under each pull request the decisive comment (`decided by comment ID of LOGIN at TIME: <the parsed line>`), `carried through N links, from ...`, the findings (at most 5: `finding <kind>: comment ID of LOGIN`, with the cut and escaped line for an unparsed variant) and why a state is `?` (`unknown because: ...`). `--long` also explains each hint (`hint clear-not-ready: ...`, `hint base-not-default: the base is X, the default branch is Y`). Only the parsed line, validated hex and numbers, is ever printed from a comment; any other comment text is not. Logins, titles and every other text from outside are escaped and cut.
- A submitted GitHub review is considered only when its `commit_id` matches the head or carry-over destination named in its evidence line. Pending and dismissed reviews are ignored. Both evidence sources must be read completely. Different trusted evidence lines for a head in the same second give an unknown verdict if the decision or carry chain depends on their order. Submitted evidence uses `review_id` in JSON.
- A comment read that failed (401, 403, 404, 429, other status, unreadable answer), hit its bound (page budget, 500 comments, time) or the `--timeout` gives `review: "unknown"` (`?`), never a verdict, and one notice per repository: `NAME: review state not read for N pull requests (#NUMBER: reason)`. Pull requests beyond the limit of 200 give `review: null` and a notice. Exit stays 0 (as for the other failed remote reads). `Counts` and `totals` do not change.
- JSON (additive, `schema_version` stays 2): per pull request `review` (the status label or null) and `review_state` (null, or `ReviewState::to_json()` version 1: `pr`, `head`, `status`, `decisive`, `chain`, `findings[{kind,comment_id,author,detail}]`, `reasons`; plus `read_error`, null for a complete read; `detail` and every text from the forge are escaped and cut at 120 characters), `hints` (array of hint names, null without `--review`), `base` and `default_branch` (escaped and cut at 200 characters, null when the list had none); per repository `pull_requests.review` (`{states, not_read, limit}`) and top level `options.review`.
- A rate limit answer (429, or 403 with an exhausted limit, `Retry-After` or the words "rate limit"/"abuse detection" in the body) stops the comment reads that have not started, in all repositories: they are `?` and one notice `rate limit reached, N reads skipped (review state ?)` names them (deviation 9). Reads in flight end on their own.
- Requests: one read per pull request (up to 6 requests for a very long thread), at most 16 at a time, at most 200 pull requests per run; with `--stats` or `--verbose` they count as `pulls`.

Deviations from the Go `--review` (`internal/pulls/review.go`, `internal/reviewstate`; Go cases `state_test.go`):

1. States: Go has `none`, `findings`, `clear`, `clear-carried`, `clear-stale`, `unparsed`; Rust has the M3 states `no-review`, `findings`, `clear`, `clear-carried`, `unverified-author`, `unknown`. A CLEAR of another head is `no-review` with the finding `stale-head` (Go: `clear-stale`); an unparsable or wrong-pull-request line is `unknown` or a finding (Go: `unparsed`); a failed read is `unknown` (Go: the review stays unset and the source of the repository is an error).
2. Grammar: the M3 parser, not the Go regular expression (exactly 40 hex patch-ids, no CR, `pr=` of any width from 1 without leading zeros; see `docs/migration-plan.md`, deviations D1 to D8). A line for another pull request is the finding `wrong-pr`, not `unparsed`.
3. The commit date of the head is not read: Go reads one commit per pull request whose comments name the head and ignores comments older than that commit. Rust has no second read; the verdict is bound to the exact head id instead (M3), so a comment can only name a commit id that existed when it was written. A comment that was edited later still counts at its `created_at` (Go ignores a comment older than the head commit): an old CLEAR that its author edited afterwards to name the head is `clear`, and only the flag `edited` (shown by `--long` and in the JSON) marks it. That grants nothing new: only the author or someone with write access can edit a comment (on GitHub that includes OWNER, MEMBER and COLLABORATOR, the trusted associations), and a later FINDINGS wins.
4. Carry-over: the chain rules of M3 (up to 16 links, patch-ids of all links equal, a `FINDINGS` of a chain head ends the chain) instead of Go's up to 8 links; the patch-ids are compared as declared (`stw review carry-over` proves them from git, `stw status` does not).
5. Trust is per login: a login is trusted when one of its comments has the association OWNER, MEMBER or COLLABORATOR (Go: per comment). Go's `ignored` count and the hint `foreign-review-ignored` are the findings `untrusted-author` and the state `unverified-author`.
6. Long threads: Go reads the first page and the last five pages and marks the state `truncated` (hint `truncated-review-unknown`); Rust reads from the first page up to the budget (500 comments) and then says `unknown` with the reason. A verdict on a page that was not read is never guessed.
7. Hints: `clear-not-ready` (state `clear` or `clear-carried` and the pull request is a draft) and `base-not-default` (the base branch differs from the default branch) are ported with Go's conditions and names; they need no read of their own. Differences: the default branch comes from `base.repo.default_branch` of the pull request list, not from a repository read (Go: `RepoInfo`), and a pull request without `base.ref` or without a default branch has no `base-not-default` (Go: only an unknown default branch has none); the comparison is exact, as in Go. Not ported, on purpose: `no-final-review`, `truncated-review-unknown` and `foreign-review-ignored` (the states `no-review`, `unknown` and `unverified-author` already say it), `ready-mergeable` (needs the verdict groups of the Go readiness check, which Rust does not have here, so the Go counts per group do not exist either). Not ported, follow-up candidate: `parent-merged-retarget` (one more read of the closed pull requests per repository with stacked pull requests).
8. Output: Go prints one count line per repository (`reviews: 1 clear, 2 none (hints: ...)`) and the JSON `pulls.data.review{states,hints}`; Rust prints the same kind of count line (with the two hints), the state and the hints of every pull request in the text and in the JSON (`review`, `review_state`) and `pull_requests.review{states,not_read,limit}`. The JSON documents are not the Go ones (see the migration plan).
9. Limit: Go reads the rows of the pulls cycle within `--limit` (default 200, drafts included, newest first); Rust reads at most 200 pull requests per run, in repository order, each repository newest first (a repository has at most 100 open pull requests in the list), with no `--limit` flag. Go reads four repositories at a time and the comments of a repository one after the other; Rust reads at most 16 comment lists at a time. Go stops every further request after the first rate-limit evidence; Rust does the same for the comment reads: after a 429 (or a 403 with `x-ratelimit-remaining: 0`, with `Retry-After`, or with "rate limit" or "abuse detection" in the first 4096 bytes of the body, as in Go) seen by any request of the run, no comment read starts any more, in any repository; the reads in flight (at most 15 besides the one that was limited) end on their own, the ones not started are `?` with the read error `not read: GitHub had reached the rate limit, ...` and are named by one notice for the run (`rate limit reached, N reads skipped (review state ?)`), the exit stays 0. Differences: Go also stops the other sources of the run (releases, issues, CI), Rust stops only the comment reads of `--review` (the pull request and issue lists are read before them); Rust does not wait or retry, as Go.
10. Go exits 1 when a review read failed (a failed remote source); Rust prints the notice and exits 0, as for the other remote reads.

## Carry-over check: `stw review carry-over` (M4)

`stw review carry-over --pr <N|none> [--fetch] [--checkout PATH] [--json] OLD_BASE OLD_HEAD NEW_BASE NEW_HEAD` (read-only; posts
nothing; `stw` does not read the review it is about). Full lower-case 40-hex ids. All three conditions must hold: (1) equal patch-id of
`BASE...HEAD`, old and new, each against its own base (in process, `via: gix`); (2) no merge commit in `NEW_BASE..NEW_HEAD` (gix);
(3) `git range-diff OLD_BASE..OLD_HEAD NEW_BASE..NEW_HEAD` shows `=` for every commit at the same position (`via: git`, Git 2.41+).
A fourth condition, added with `stw prove` and checked only when the three held: the patch-id of every commit is equal, old against new at the same position (`range-diff` shows only "Binary files differ" for a binary change, so a binary file with other contents in an intermediate commit passed the three; Go compares the commits themselves, so this is nearer to Go; JSON `commit_patches{state,differing,via}`, reason `commit-patch-differs`). A step that could not run is `unknown`; the answer is then no. Only for a yes the output ends with
`Carried-over: from=<old head> to=<new head> patch-id=<40 hex> pr=<n|none>` (a line of the M3 grammar). Exit 0 yes, 1 no or a
step could not run (result printed), 2 usage. `--fetch` fetches `refs/pull/N/head` of `origin` into `refs/stw/carry-over/pr-N`
(`via: git`, for an ssh (also `ssh+git://`, `git+ssh://`), scp-like, `file://` or local-path `origin` only, with `GIT_ALLOW_PROTOCOL=file:ssh` so that a `url.<base>.insteadOf` of the user's config cannot turn it into https; an http, https or git:// `origin` is refused typed (`transport`, exit 1, answer no) before git starts, because the https fetch stays with gix and is not built for this refspec; a `core.sshCommand` of the user's config runs as with `FetchOrigin`; no tags, no FETCH_HEAD, empty refmap) and requires it to equal `NEW_HEAD`.
The range-diff step pins what could hide a change (empty attribute source, no `diff.ignoreSubmodules`, size limit, context) and refuses,
typed unsupported, a repository config with `diff.external`, `diff.<driver>.textconv|command` or an active `info/attributes`.
`git range-diff` is told `--no-notes`. It also says no where Go says yes when only the author of a commit changed, and for an
identical patch twice in a range (range-diff pairs it across). JSON schema 2 (`pr`: number or `"none"`; `via` per step; `read_errors`). Deviations from the Go command and the mapping of its 46
tests: header of `tests/it/carryover.rs`; plan: `docs/migration-plan.md`, "Next slice".

## Ready check: `stw ready-check`

`stw ready-check NUMBER [--repo OWNER/NAME] [--human | --json]` reads one pull request and reports `open`, `review`, `checks`, `base`, `blocker`, `conflict`, and `rebaseable`. Every condition must pass; an unreadable or incomplete dependency is unknown and does not pass. Draft is reported but is not a condition. A non-default base passes only when another open pull request has that head branch; the lowest matching number is shown. Review state uses the same trusted-author and carry-over rules as `status --review`. Check runs and commit statuses are read at the exact head. The command uses GET only and does not mark ready or post anything.

`--json` prints schema 2 with `command`, `time`, `host`, `repo`, `number`, `head`, `draft`, `ready`, `conditions` and `errors`. Text is the default. Exit 0 means all conditions pass, 1 means a condition did not pass or a read failed, and 2 means invalid arguments. `--repo` selects a repository; otherwise the workspace must contain exactly one GitHub repository. The GitHub API base follows `STW_GITHUB_API_URL`; there is no `--host` flag. Reads are bounded by the existing GitHub reader. A cut list, malformed CI fields, a truncated status context, or missing check-state evidence is unknown. For each commit-status context only its newest observation counts.

## PR lint: `stw lint pr` (M8)

`stw lint pr PR [PATH | --repo OWNER/NAME] [--owner-identity "Name <email>"]... [--human | --json]` (read-only, GET only). Reads the
pull request, its commits (3 pages of 100; the pull request's own commit count must not be above the commits read) and its
conversation comments (10 pages of 30) and reports one finding per violated rule with rule ID, severity and location (PR title or
text, comment ID, commit SHA, line). The rule table is `src/lint_pr.rs` (`RULES`; `--json` lists it): no session link in commit
messages, title, text and comments; author and committer of every commit are an owner identity (`--owner-identity`, repeatable,
else `user.name`/`user.email` of the Git configuration of the workspace; none: usage); a `Co-Authored-By:` trailer with name and
email; Conventional Commit subjects; the PR text about 25 lines (warning above 25, error above 30), a `Blocker` line and an
`Author agent` line; the first line of a comment that claims to be a review is `Review: <CLEAR|FINDINGS> head=<40 hex>
patch-id=<40 hex> pr=<n>` (or the Carried-over form) for this pull request, and such a comment has at most 15 lines. The repository
is `--repo` or the single GitHub `origin` of the workspace of PATH (default: the current directory). Text of the forge is untrusted:
the human list escapes control, invisible and direction-changing characters and cuts each message at 120 characters.
Exit 0: no error finding (warnings do not fail). Exit 3: an error finding, or a list cut at its bound
(`truncated.commits` / `truncated.comments`; `summary.passed` is false). Exit 1: a failed read or no token (no output). Exit 2: usage.
`--json` is the document of schema 1 of the Go command (`schema_version`, `command: "lint pr"`, `time`, `host`, `repo`,
`pull_request`, `head`, `options`, `rules`, `findings`, `summary`, `truncated`; keys are never omitted). Deviations from the Go
command and the mapping of its 15 + 17 + 3 tests: headers of `tests/it/lint_pr.rs` and `tests/it/lint_pr_cmd.rs`; plan:
`docs/migration-plan.md`, "Slice 3".

## Text files: `stw lint review`, `stw lint pr-text` (new)

New behavior without a Go counterpart; read-only, no network. `lint pr-text` and `lint review` without `--base`/`--head` start no `git`. With `--base` and `--head` the revisions are resolved in process (a tag is peeled to its commit; no merge base is a usage error), and the patch-id is computed by git: `git diff BASE...HEAD` and `git patch-id --stable` through the runner (`Op::DiffRange`, `Op::PatchIdStable`, minimum Git 2.30). The in-process patch-id of `patch_id.rs` is not used here: it follows git's defaults only (see "Patch ID: the value of `git diff | git patch-id --stable`", deviations 1 to 5), and a review line carries git's value for the repository and the user's configuration.
Both read one UTF-8 text from FILE and report findings in the shape of `lint pr` (`--json`: `schema_version` 1, `command`
`lint review` or `lint pr-text`, `time`, `file`, `options`, `rules`, `findings` with `rule`, `severity`, `location` (`kind`
`file`, `line`), `message`, `summary`). Shared rule IDs and functions with `lint pr`: `session-link`, `pr-body-length`,
`pr-blocker-line`, `review-line-format`, `review-comment-length` (`src/lint_files.rs` calls `lint_pr`); the first line is read by
`review::parse_strict`, no second grammar. Own IDs: `text-line-endings`, `review-patch-id`, `review-head`,
`pr-generated-line` (`FILE_RULES`). Exit 0 no error finding (a warning does not fail), 3 an error finding, 2 usage.

`stw lint review FILE [--base REV --head REV] [--pr N|none] [--checkout PATH]`: the first line is exactly
`Review: <CLEAR|FINDINGS> head=<40 hex> patch-id=<40 hex> pr=<n|none>` or `Carried-over: from=<40 hex> to=<40 hex> patch-id=<40 hex>
pr=<n|none>` (fixed keys and order, lower-case hex, no markdown, no blank line before it); at most 15 lines (blank lines at the end
do not count); no carriage return in any line; no session trailer or
code-session URL (the word session alone is no finding). `--pr` compares `pr=`. `--base` and `--head` (only together; `--checkout` or the repository around the current
directory; names, ids or tags) compute the patch-id of `BASE...HEAD` (the first field of `git diff BASE...HEAD | git patch-id --stable`, by git)
and compare it with `patch-id=`, and compare `head=` (`to=`) with the id HEAD resolves to. An empty diff has no patch-id and never
matches.

`stw lint pr-text FILE [--ready]`: about 25 lines (warning above 25, error above 30, the limits of `lint pr`); exactly one Blocker
line, `Blocker: <text>` for a Draft (the default) or, with `--ready`, exactly `Blocker: None.`; the last non-blank line is
`🤖 Generated with [Claude Code](https://claude.com/claude-code)`; no session link; no carriage return. A `#n` that names an issue
of another repository is not checked (planned as its own step).

Fixtures: real comments and PR texts of three pull requests, stored unchanged in `tests/fixtures/lint/` (its README names the
sources); tests: `tests/it/lint_files.rs` (23 cases); mutations `LF1` to `LF39`. Source of the limits and the line forms:
`claude/CLAUDE.md` (review comment, 15 lines, session links) and `claude/skills/agent-workflow/SKILL.md`, section 9 (PR text, 25
lines, the Blocker line, the review line); the attribution line is the one of the tool (`dotagents-adapter.md`). The rules are
not compared with the current dotagents text rule by rule: UNKNOWN, as for `lint pr`.

## Watch scenarios (port corpus)

`tests/fixtures/watch/` holds 279 scenarios derived from the Go watcher.
Use `--repo OWNER/NAME` or `--repo https://github.com/OWNER/NAME` to watch
a specific repository. Both forms select the same watcher state. Without
`--repo` or `--workspace`, Watch uses `.stw/watch.toml` if present. With no
config file, it watches the GitHub `origin` of the current Git repository.
`--workspace` watches the workspace root and its configured submodules.
If workspace discovery and `STW_ROOT` yield no root, it watches the
current standalone Git repository.

`stw watch` requires an explicit mode. `--once` polls once, `--stream`
polls until SIGINT, SIGTERM or `--max-idle`, and `--until-event KIND`
polls until a selected kind appears or the timeout expires. `--status`
reads the heartbeat and accepted repository state without network or lock.
The interval defaults to 60s and has a 10s minimum. A different commit
at the PR head emits `head-changed`. A switch between Draft and Ready
for review emits `ready-state-changed`, including when the head stays the
same. The watcher compares snapshots, so a switch that reverses between
polls is invisible. The cycle also emits PR,
check, mergeability, restack and watcher health events. A restart reprints
journalled events that may have missed stdout. The Go state format is
unsupported. Polling has no jitter.

Use `stw watch --stream --interval 10s -v` to see the watched repositories,
state directory, each poll's outcome and the next wait on stderr. Use
`-vv` to also see repository reads, PR head and readiness comparisons,
checks awaiting confirmation and emitted event IDs. Event output and
the journal keep their usual format.

Stdout defaults to schema-version-1 JSON Lines for event modes. `--human`
prints one escaped, bounded line per event with short SHAs and keeps the
Go `git rebase --onto` hint for actionable `restack-needed` events
(D-20261119-03). Rust uses its shared terminal escaping routine. The
journal always retains full SHAs in JSON Lines, including in human mode.
`--status` returns a JSON report and refuses `--human`. Exit codes are
0 for a successful poll or normal stop, 1 for failure, 2 for usage,
3 for a held watcher lock, 4 for an event timeout, and 5 for a stale heartbeat.

The scenarios are generated from the Go tests of
`legacy/sourcesteward` and are independent of the Go file formats: each gives the inputs (observations per cycle, answers of a
fake forge, flags of the command) and the expected semantic outputs (events with kind, repository, PR, details, order and
step, a semantic view of the accepted state, exit codes). Event ids and journal sequence numbers are only additional
information, marked as format-dependent. Format and semantics: [`tests/fixtures/watch/FORMAT.md`](../tests/fixtures/watch/FORMAT.md).

```
bash tests/oracle/watch_scenarios.sh GO_SRC OUTDIR   # GO_SRC: the Go source tree (read only, copied), OUTDIR: absolute, ignored by Git
```

The driver copies the Go tree into a private work directory, lays the exporter (`tests/oracle/watch_scenarios/`, Go test
files) into the copy, runs `go test` there and writes the scenarios and `index.json`; two exports are byte-identical. The corpus
in the tree is written with `WATCH_SCENARIOS_WRITE_FIXTURES=1` and the path `tests/fixtures/watch`. `tests/it/watch_scenarios.rs`
checks only the structure (required fields, unique names, step order, FORMAT.md names every field); the semantics are played in
the packages of the port. Mutations `WS1` to `WS39`.

The corpus holds 279 scenarios: 194 of kind `watch-cycles` (the core: diff, debounce, checks classes, timeout,
backoff, restack, health, baseline, lost state, replay and crash points of every kind, generated forge sequences), 33
`vectors` (pure functions: classification, shape, backoff, ids, restack candidates, human lines, config, remotes, flags, URL
and rate-limit rules), 14 `observer-session` and 25 `http-reader` (the observer and the HTTP reader
against a fake forge: the answers of the forge as data), 13 `run-loop` (the command with a fake clock: modes, exit
codes). Not exportable as scenarios (written by hand in the packages W1 and W4): the journal file (truncated and corrupt tails),
modes and checks of the state directory (work tree, `.stw`, symlinks), the lock and the second watcher, the heartbeat file, signals,
cross builds, the source checks of the Go packages (no imports, no write verbs), the `gh` token lookup, per-request deadlines and
cancellation of the HTTP client, the layout of the conditional-request store, and the config and FIFO checks that need files.

## Watch core (port, W1)

First package of the port of `stw watch` to Rust (owner decisions D-20261119-02 to -05): the core, `src/watch/` (`mod.rs`,
`event.rs`, `state.rs`, `apply.rs`, `journal.rs`, `lock.rs`, `statedir.rs`). Pure logic and files: no network (the reader is W2), no
cycle (diff, debounce, checks classes, backoff, restack choice: W3), no command, signals or config (W4). Times are parameters, there is no
clock. Tests: `tests/it/watch_core.rs` (own tests and the replay of the corpus of "Watch scenarios"); mutations `WK1` to `WK59`.
New direct dependencies, both pinned exactly: `serde` `=1.0.229` (derive; the formats are written with it) and `serde_json` `=1.0.154`
(was `1`; feature `raw_value` added, no new crate). SHA-256 is own code (`statedir::sha256`, FIPS 180-4 and NIST vectors, padding
boundaries checked against GNU `sha256sum`): the gix feature would add `sha2` and change gix's object-format support. No `libc`, no process.
Licenses: `serde` and `serde_json` are `MIT OR Apache-2.0`; both are in `Cargo.lock` already through gix and ureq.

**The formats are the Rust formats of the port, not the Go ones** (D-20261119-05: a new baseline when switching). So a Go state
directory is never opened by mistake: the directory id hashes the prefix `stw-watch-rs1` (Go: `stw-watch-v1`), and `state.json` carries
`format: "stw-watch-state"`, which a Go file lacks. Both file formats are versioned: `schema_version` 1; another version, another format or any
unknown key is `CorruptState`, never guessed.

State directory `$XDG_STATE_HOME/stw/watch/<id>/` (else `$HOME/.local/state/...`), `<id>` = the first 16 hex digits of SHA-256 over
`stw-watch-rs1\n`, the host, `\n` and `workspace:<root>` or `repos:` plus the lower-cased, deduplicated, sorted repositories joined by `\n`.
`--state-dir` must be absolute (usage error otherwise). The directory has mode 0700 (an existing one with another mode is refused), its files
0600, and it is never inside a Git work tree (a `.git` entry, file or directory, in it or above it; symbolic links are resolved first) or
below `.stw`; nothing is created when the answer is a refusal. Files: `state.json`, `journal.jsonl`, `lock`, `heartbeat`.

`journal.jsonl`: one event line each (the envelope `schema_version, seq, id, time, repo, pr, kind, details`, every key always, details with
exactly the keys of the contract, lists never null), append-only, fsynced before the event is printed, never rotated. `seq` is the `seq` of the
last complete line plus one, read from the lines, never from a file offset (the spike's `tell() - (1 << 20)` error); the file is validated
line by line with memory bounded by one line (limit 4 MiB). Bytes after the last newline are a truncated write: `Journal::junk_bytes`
reports them, `after` ignores them and the next append cuts them off. A complete line that is not a valid event, a `seq` that is not the
previous plus one, a line that is not UTF-8 or longer than the limit is `CorruptState` (not repaired).

`state.json`: one compact JSON line (maps ordered, so the file is deterministic), written atomically (temporary file, fsync, rename, fsync of
the directory): `format`, `schema_version`, `applied_seq`, `printed_seq`, `interval_seconds`, `cycle`, `repos_epoch`, `repos` (null before the first
cycle), `skipped`, `repo` (per repository: `baselined`, `since`, `last_ok`, `default_branch`, `conditional` (opaque, W2/W3), `prs` keyed by
number (`number`, `title`, `author`, `labels`, `draft`, `head`, `head_ref`, `base`, `fork`, `generation`, `epochs` {head, checks, mergeable, ready,
base}, `class` (`""`, `none`, `pending`, `green`, `red`), `class_candidate` (null or a class), `pending_since`, `pending_names`,
`timeout_fired`, `mergeable`, `mergeable_state`, `conflicting`, `behind`, `mergeable_candidate`, `detail_due`, `gone`), `closed` (at most 5000),
`restack_due`, `restack_done`) and `health` per repository and `""` for the run. `seq` is not in it.

Order of a cycle, which W3 follows (as Go `Watcher.Cycle`): the cycle emits an event and applies it to `Core::state` at once (`State::apply`),
then does its own bookkeeping that is no event (`cycle`, `last_ok`, debounce candidates, `detail_due`, silent class changes), and
`Core::commit` then journals each event (fsync), prints it and writes `state.json` with `applied_seq` = `printed_seq` = the last seq.
`commit` does not apply: a second application would undo the bookkeeping (head-changed resets the checks and sets `detail_due`).

Crash model (the "Replay effects" table of Spec 035): an event is journalled exactly once. `Core::open` replays the lines past `applied_seq`
into the state (`apply` is idempotent: counters are raised to the value in the event id and never lowered, every other effect sets or
removes) and prints those past `printed_seq` again (at-least-once). A missing `state.json` with a journal is rebuilt from the journal silently
(no reprint, the set of repositories is learned again, the first cycle is a baseline), also without a forced baseline (as Go). A corrupt
`state.json`, or a state whose `applied_seq` or `printed_seq` is beyond the journal, is an error unless the baseline is forced; then it
is rebuilt the same way. After an error the session is unusable until reopened.

Lock: `std::fs::File::try_lock` on `lock` (D3; no `libc`), a second watcher gets `WatchError::Locked(pid)` (exit 3 in W4), retried for about one
second so a `--status` probe does not look like a second watcher; `holder` probes with a shared lock and never creates the file. The lock
file gets mode 0600 when it is taken (also when it existed with another mode).
`read_status` (`--status --json`): fresh when the heartbeat is younger than `max(3 x interval, 180 s)`; reads only, no lock taken.

Event ids keep the Go form `REPO#PR:KIND:KEY` (`-` for an absent part); the corpus proves that the key functions rebuild every id of the 194
cycle scenarios and that ids are unique per scenario.

Deviations from the Go behavior (named one by one; D numbers are those of the port plan):

1. New file formats and a new directory id (D-20261119-05), see above.
2. D3: the lock is `File::try_lock`; no `lock_other` stub.
3. D4: time is UTC with `Z`, whole seconds, passed in; `format_utc` is the inverse of `github::parse_utc`.
4. D6: text from outside is masked with the project rule (`sanitize`: bytes that are not UTF-8 as `\xNN`, `\`, control, invisible and direction
   characters as escapes, cut in bytes at a character boundary) instead of Go's `?` and space.
5. D7: `serde_json` does not escape `<`, `>`, `&`, U+2028 and U+2029; consumers read JSON, lines are not byte-equal to Go's.
6. D8: no JSON-schema validator; the strict decoder refuses unknown and repeated keys, unknown kinds, schema versions other than 1, trailing data,
   null lists and numbers that are not non-negative integers. Like Go, a missing detail key is the zero value (the corpus case "decode missing
   detail key").
7. `id_counter` accepts digits only (Go's `Atoi` also took `+5`).
8. `repos` is `null` instead of an empty slice for "not set yet", so an empty set is not mistaken for the first run.
9. `restack-needed` keeps `argv` (D-20261119-03). The wired command fills it for class 1. `--human` shows the rebase hint when its arguments can be displayed as plain shell tokens.
10. Lock: an error other than "busy" (the file cannot be opened or locked) is `Io` at once, not `Locked` after five attempts.
11. `lock::holder` reports "not held" when the probe fails with an error other than "busy"; Go reports "held".
12. `journal.jsonl` is opened as it is: a symbolic link is followed (as Go); the state directory itself is checked (mode, work tree), its files are not.
13. A red check keeps `shape` empty. Go classifies the failing set; this cycle does not.
14. `restack-needed` is class 1 only: the child base equals the merged head ref. The contains class (same base, the child commits include the old parent) is absent, and a class-1 child is not checked against the commit list.
15. The 194 `watch-cycles` scenarios are not replayed by this cycle. The checks, mergeability, timeout, and class-1 restack above are covered by hand-written tests in `tests/it/watch_command.rs`.
16. `checks-pending-timeout` is fixed at 45 minutes inside the command. There is no flag. Zero would disable it; the command never passes zero.

What the corpus drives in W1 (`tests/it/watch_core.rs`): vectors `event_id`, `id_counter`, `event_json` (22 cases), `workspace_id` (which inputs share an
id; the Go id values are format-dependent), `default_state_dir`, `status_freshness_seconds`, `merged_due_head_fork`: 8 of the 33 vectors
(the others belong to the cycle, the reader, the command or are Go-only). `watch-cycles`: all 194 scenarios (a) as an `apply` sequence with the
silent baseline of the first read and the state-changing file operations of the steps, compared with `expected_state_view`, (b) through the session
(`Core::open`, `commit`, restart, crash after each durable step, delete or corrupt the state, forced baseline) against the printed ordinals of every step and
the journal ordinals (108 crashed steps: append 41, print 37, state 30; 6 deleted and 1 corrupted state), (c) crash variants against their crash-free
variant (same journal, same replayed state), and the 3 status probes. Compared in (a): `repos`, `skipped`, `repos_epoch`; per PR the set, `head`, `base`,
`draft`, `generation`, the epochs head/mergeable/ready/base (checks epoch at most the view's); closed records; health episode, stall episode, open error,
stall, last code, failures at most the view's. **Not mapped in W1 (cycle effects, W3)**: debounce candidates, backoff times (`next_try`), check classes and
the silent `pending`/`none` class changes (and so the exact checks epoch), `pending_*`, `timeout_fired`, mergeable fields, `detail_due`, `gone`,
`last_ok`, `since`, `cycle`, `interval_seconds`, `restack_due`/`restack_done` after the restack (the cycle clears them), `hints`, `state_after`, the restacker choice.
Not corpus-driven but written by hand: journal truncation and corruption, modes, work-tree refusal, atomic files, the lock (same process and a child process),
the heartbeat file. Not run: the observer-session, http-reader and run-loop scenarios (W2, W4).

## Watch reader (port, W2)

`src/watch_reader/` is the reader and observer of `stw watch` (package W2 of the port; the Go reference is
`internal/hosting/watch_*.go`, `internal/watchcmd/observer.go`, `pullsread.go` and the remote rules of `resolve.go`). It has no
cycle (W3), no state directory or journal (W1) and no command (W4). Every request is a GET; there is no write path (a test scans
the sources for write calls). The token comes from the client (`WatchHttp::from_client`), goes only into the `Authorization`
header and never into an output, an error text, an observation or the store (`token_never_in_output`). No `git` or `gh`
process, no new crate: the transport is `ureq` as in `src/github.rs`, with the same platform trust store.

- `http.rs`: the pure rules (endpoint allowlist, branch segment encoding and guard, Link header, `X-RateLimit-*`, Retry-After,
  `assess_rate_limit`, `rate_limited_until`, `split_remote`, `parse_remote`, `valid_host`) and `WatchHttp`, the reader:
  conditional GET (`If-None-Match`, a 304 is a typed result that returns the validator that was sent), `list_open_pulls`
  (Link paging, 25 per page, halving to 12 on a body over 1 MiB and restart at page 1 with the halved size kept in memory only, a
  probe past a full last page after a 304, bounds 80 / 167 pages and `max_open_prs` 1 to 2000), a deadline of at most 30 s per
  request, a body limit of 1 MiB (also by `Content-Length`), no redirects. Time is never read: the caller passes Unix seconds.
- `cond_store.rs`: `CondStore`, the validators the core keeps between cycles, and its serialized form (see below).
- `obs.rs`: the types of the observation: `RepoObservation` (`default_branch`, `prs`, `gone`, the replacement `cond`), `PrObs`
  (with `DetailObs` mergeable and state, and `ChecksObs` runs, statuses and `via`), `GoneObs` (merged, merge commit, `method_hint`
  merge / rebase-or-squash / unknown), `Hints` and `KnownPr` (what the accepted state tells the observer), `ObserveError`
  (code, run-wide, wait) and `ObserveFailure` (an error, or `Canceled`). `checks_class` gives `none|pending|green|red`; the shape of
  a red head and the failing names belong to W3. The texts are raw GitHub text; they are escaped where an event is made.
- `host.rs`: `HostObserver` over the trait `WatchRead` (`WatchHttp` implements it; the tests use a fake that mirrors the Go
  one). It reads only what the state makes necessary: the list every cycle, `branches/BASE` once per base, `pulls/N` for a new
  PR, a new head, a moved base, after a merge, when due and on the slow refresh, check runs and statuses for pending, unobserved
  or changed PRs (a stored candidate is confirmed with a read without validators), and `pulls/N` once for a PR that left the
  list. `after_cycle` reads `rate_limit` (free): below 10 percent of the limit the interval is to be doubled, below 50
  remaining a hold is armed that the next `observe` reports once as a run-wide `rate-limited` (reset plus 5 s).
  `read_pull_commits` reads a page of `pulls/N/commits` for the restack. For W3: `observe(repo, &Hints, Option<&CondStore>,
  &AtomicBool)` returns the snapshot and the replacement store; the core keeps the store.

Endpoints (all GET, checked against the allowlist before a credential leaves): `pulls?state=open&per_page=25|12&page=N`,
`pulls/N`, `pulls/N/commits?per_page=100&page=1..3`, `branches/SEG`, `commits/SHA`, `commits/SHA/check-runs` and `.../statuses`
(`per_page=100`, pages 1 to 10), `rate_limit`, and (new) `actions/runs?head_sha=SHA` (pages 1 to 10).

Error classes (`Code`): `rate-limited` (429, or 403 with remaining 0, a Retry-After header or the text "rate limit" or "abuse
detection" in the first 4096 bytes), `unauthorized` (401), `forbidden` (other 403, and 404 with its status kept), `http-5xx`,
`malformed-response` (other statuses, bad bodies, an unsolicited 304, a redirect other than 301/308), `too-many-open-prs`,
`page-too-large`, `repo-moved` (301/308, never followed; the new name only for a `/repos/OWNER/NAME/` path on the API host),
`network` (connection failure, deadline). Messages are fixed text of at most 200 bytes.

The store of validators (`schema_version` 1, a new Rust layout; the Go `Conditional` layout is not used): `{"schema_version": 1,
"list": {"per_page": 0|12|25, "pages": [{"etag", "link", "items": [...]}]}, "tips": {BRANCH: {"etag", "sha"}},
"validators": {"pulls/N" | "checks/SHA/runs" | "checks/SHA/statuses": ETAG}}` in a fixed key order with sorted maps. `from_json` is
strict (unknown fields, other versions, bad ETags, SHAs, keys, sizes) and bounded (80 / 167 pages, 25 items per page, 20 000
validators, 5 000 tips); a refused store means one unconditional round. `prune` drops what is no longer open.

Tests: `tests/it/watch_reader.rs`. Replayed from the corpus: all 25 `http-reader` scenarios against a raw HTTP server on
127.0.0.1 (port 0; every request is checked for GET, bearer token and `If-None-Match`), all 14 `observer-session` scenarios
against a fake reader (the Go test fake; the `conditional` flag is, as in the export, the last validator sent under that
call name), and 14 `vectors` files with 198 cases (remotes and hosts, URL rules, branch segments, allowlist, Link, rate limit,
Retry-After, pull detail, default branch, commit list) plus the class field of 43 cases of `classify_checks`. Not replayed in
W2, by design: the other 15 `vectors` files (backoff, freshness, ids, event JSON, restack candidates, human lines, config,
flags, workspace; packages W1, W3, W4), 194 `watch-cycles` and 13 `run-loop` scenarios. Not mappable from the three kinds: 0.
Own tests without a Go counterpart: the store (round trip, golden bytes, 25 refusals, bounds, prune), the Actions fallback,
each rate-limit evidence alone, pages of check runs and statuses, a missing base branch, the request timeout, a refused
connection, hostile Link headers, the page bound of an endless list, masked diagnostics. Mutations `WR1` to `WR89`.

Deviations from Go (W2 part of the list of the plan, section 4):

- D5: error texts are fixed Rust texts (no go-gh text), the code mapping is unchanged; `message` never carries a body or the token.
- D6: the observation holds raw GitHub text; escaping (`escape_untrusted`) is applied where an event or a diagnostic is made. The
  one diagnostic of this layer (a base name that is refused) is escaped here.
- D9: rate-limit and backoff times are Unix seconds passed in; nothing reads the clock, nothing sleeps.
- D10: the default branch is guessed as in Go (`main`, then `master`, from the bases of the open PRs); `base.repo.default_branch`
  of the list items is not used.
- D11 (new): a 403 on `check-runs` falls back to the Actions runs of the head (`actions/runs?head_sha=`, the newest run per
  workflow and event, `status_ci::latest_runs`); the runs have no output text, `ChecksObs::via` is `ActionsRuns`, one line per
  repository on `take_diagnostics` ("checks via actions-runs"). Both refused: `forbidden`. A 404 is not a fallback.
- The corpus ties the observer to a Go `Reader` interface; here it is the trait `WatchRead` (same calls, same order).
- A cancellation (`ObserveFailure::Canceled`) is noticed when a read fails, as in Go; there is no context, a request ends at its
  deadline of 30 s at the latest. Signals belong to W4.
- U+FFFD in a branch name is refused (it is what invalid UTF-8 becomes in the exported corpus); the other branch rules are Go's.
- Dates: `merged_at` is validated with an own RFC 3339 check (`valid_rfc3339`: Go's `time.Parse` form with fraction and offset).
- JSON types are checked strictly where Go's decoder is (a number where a string is expected is `malformed-response`); a value
  of the right type but empty is kept as empty.
- A tip SHA must be 40 lower-case hex (Go: 40 characters), so that a stored tip always passes the strict store.
- A validator that is empty is not stored (Go stored the empty string); a read without validator is the same.
- `max_open_prs` is a `usize`: the corpus values -1 and 0 are the refused value 0.
- The call accounting (`take_calls`) is the reader's own, as in Go; `Client::stats` is not fed.
