# Specification: `stw status`, the online, action-oriented workspace overview

**Created**: 2026-10-22 | **Status**: proposed (spec-light: this page, [contracts/status.md](contracts/status.md) and
[plan.md](plan.md) are the whole design; no implementation yet). All former open questions are decided (section "Decisions recorded"). It **amends** [Spec 033](../033-stw-status/spec.md): the default of
`stw status` becomes online and its output action-oriented; the old behaviour stays as `--offline`.

`stw` is a higher-level tool; Git is its persistence layer. Nobody works offline any more and `git status` alone takes seconds, so
`stw status` becomes the **one command** the owner runs in the morning, at midday and in the evening: it gives the state of the whole
workspace at a glance, starts with a **prioritised "needs action" section**, and then shows the per-repository detail. If it takes ten
seconds, it takes ten seconds. Category commands (`stw pulls`, `stw sync`, `stw tidy`, `stw release`) stay as **drill-down targets**:
every action item names one. Everything stays read-only: the command suggests, the owner acts.

## Decision (owner, 2026-10-22, revised after the first draft)

| Option | Rating | Why |
| --- | --- | --- |
| A. Local only (Spec 033 as it is) | 2/5 | Fast, but answers only half of "what needs action": PRs, release drafts, red CI and unpublished tags live on the forge. |
| B. Opt-in `--remote` (first draft of this spec) | 3/5 | Keeps speed by default, but the owner must remember a flag to see the whole picture; superseded by the owner's redirect. |
| **C. Online by default, `--offline` for the old behaviour** | **5/5, chosen** | One command, whole picture, action first. The fast local-only view survives as a flag. A missing token degrades to the local view with a clear notice; a failed request is reported and exits 1. |
| D. A separate overview or `stw releases` command | 2/5 | A second place to look; the owner explicitly wants no extra command for the overview. |

Derived rules: (1) the default is online; `--offline` restores Spec 033 **byte for byte** (text and the v1 JSON); (2) no new
authentication route (the `gh` token of the hosting commands, Spec 022); (3) the command never writes to the forge, the repositories
or the disk, and never runs a suggested command; (4) a failing or unavailable source never hides the rest.

## User stories

- **US1 (glance).** `stw status` prints, first, "Needs action" (every finding that deserves attention, ordered by priority), then
  one block per repository, then one summary line with the counts per priority.
- **US2 (offline).** `stw status --offline` sends no request, reads no credential and prints exactly the Spec 033 output.
- **US3 (open pull requests).** Per repository the open PRs and their `stw pulls` verdict groups (bots included); each PR that needs
  action is an item with its deep link.
- **US4 (releases).** Pending release drafts, tags that have no release, and what is reported when the token cannot see drafts.
- **US5 (CI).** The CI state of the default branch and of the latest tag's release workflow run (WP-C).
- **US6 (local).** **Every** Spec 033 attention reason and information fact (changes, operation, behind or diverged, no upstream,
  detached, gitlink drift, stashes, unmerged branches, linked worktrees) plus "local default branch behind origin" maps to a row of
  the action table with its own subject.
- **US7 (degrade).** No token (nothing attempted), or a failed request, rate limit or deadline: the local view and the sources that worked are printed, with one
  clear notice per missing source; never an error wall.
- **US8 (machine).** `--json` prints one stable, versioned document: action items with ids, priorities, categories, deep links and
  suggested commands, for the owner's later web UI (out of scope here), and the per-repository data.
- **US9 (presentation).** Colour, three icon sets, a bottom summary line, a progress indicator on stderr on a terminal; the output
  stays copy-paste friendly without colour.

## CLI synopsis

```
stw status [PATH] [--offline] [--fetch] [--worktrees] [--icons unicode|nerd|ascii] [--timeout DURATION] [--json]
```

- `--offline`: local only, no request, no credential read, Spec 033 output and JSON v1 unchanged. `--fetch` and `--worktrees` keep
  their meaning and compose with both modes (`--fetch` runs git; it is independent of the forge reads).
- `--icons SET` (human output only): `unicode`, `nerd` (Nerd Font glyphs) or `ascii`. Default: `unicode` on a terminal whose locale
  is UTF-8, `ascii` otherwise (a pipe, a file, a non-UTF-8 locale, `TERM=dumb`). An explicit value always wins. `--icons` with
  `--json` is accepted and ignored; `--icons` with `--offline` is accepted and changes only the glyphs of the human text (without it, `--offline` is byte-identical to today's default output).
- `--timeout DURATION` (Go duration, for example `2m`; default `90s`, 1s to 10m, usage error otherwise): the overall deadline of the remote half; the per-request timeout is fixed at 15 s. `--timeout` together with `--offline` is a usage error (exit 2).
- Host: `github.com`, one host per run (the `stw pulls` default); a repository whose `origin` is not GitHub on that host is `skipped`
  for the remote sources (reason `not-github-origin` or `other-host`, a notice, not an error). No filters, limits or owner flags:
  those belong to `stw pulls`.
- Platform: Unix only; Windows stays unsupported and this spec adds no Windows content.
- Colour: only on a terminal and with `NO_COLOR` unset, as today. The progress indicator (stderr, `reading remote 3/7`, a single
  rewritten line) exists only when stderr is a terminal; stdout never carries it.

## Sources and what each reads

Every repository of Spec 033 (root and initialized submodules, at most 256) has the **local** source. Repositories with a GitHub
`origin` and local state `ok` also have the **remote** sources below. Each source of each repository ends as `ok`, `skipped`,
`degraded` (with a code) so the output can say exactly what is missing.

| Source id | Reads | Request shapes | Mandatory |
| --- | --- | --- | --- |
| `local` | Spec 033 facts (unchanged) | none | yes |
| `repo` | `repos/OWNER/NAME`: `default_branch`, `html_url`, `permissions` | existing | yes (online) |
| `pulls` | open PRs, verdict groups, per-PR rows | existing watch/pulls shapes, reused through the `stw pulls` cycle | yes |
| `releases` | `repos/OWNER/NAME/releases?per_page=100&page=N` | existing release listing, see the amendment | yes |
| `tags` | `repos/OWNER/NAME/tags?per_page=100&page=N` | existing (Spec 026) | yes |
| `ci-main` | `branches/DEFAULT`, then check runs and statuses of its head | existing watch shapes | yes (WP-C) |
| `ci-release` | `actions/workflows/FILE/runs?head_sha=SHA&per_page=100&page=1` for the commit of the latest tag and of each unreleased tag | existing (Spec 026) | yes (WP-C) |
| `issues`, `viewer` | open issues per repository and the authenticated login | new fixed shapes, admitted in `status`, see [Spec 038](../038-stw-issues/spec.md) | yes (WP-G) |

### Open pull requests (reuse, no new shape)

One call of the `stw pulls` cycle (`internal/pulls`, Spec 036 verdict table and groups, unmodified) over the GitHub repositories with
the default options (no filters, bots included, budget 200 non-draft PRs for the run, no detail for drafts). Status code only
**consumes the `pulls.Result`**: per-repository counts and the PR rows, never its own list, `pulls/N`, check or classification code.
**The single bounded re-read** of lazily unknown mergeability is **already part of the shared cycle** (Spec 036 "Lazy mergeability", merged in PR #149): one `pulls/N` re-read at the end of the pass, no sleep, no loop, never after a rate limit, a failed re-read keeps the row `unknown`. Status **inherits it unchanged**; this spec changes nothing in `stw pulls`.

### Release drafts, tags and visibility

- **Drafts.** Releases with `draft` true: `name`, `tag_name`, `created_at`, `html_url`, asset count. Published releases are read and
  used only to know which tags have a release. The `Link` header's `rel="next"` is followed, never beyond page 10 (error
  `too-many-releases`).
- **Visibility.** GitHub shows drafts only to a token whose user has write access, and does not say that it withheld any. The `repo`
  read's `permissions` (`push`, `maintain` or `admin` true) gives `drafts_visibility`: `write-access`, `no-write-access` or `unknown`.
  With `no-write-access`/`unknown` the command **does not report zero**: it says "release drafts not visible to this token (no write
  access)" or "release drafts unknown (write access not determined)" and creates no draft item. A fine-grained token with write
  access but without the releases scope can still withhold drafts; documented residual (Risks).
- **Tags without release.** A tag is "unreleased" when it matches the repository's **release tag pattern**, is **newer than the latest published release** (by the version scheme of the pattern; when no release is published, only the newest matching tag) and **no release, draft or published, carries its `tag_name`**. The pattern is the tag grammar of each product the repository declares in `.stw/release.toml` (Spec 026 D6/D9: `semver`, `calver` or a pattern, for example `bdock/*` and `claude/*`); **latest tag** means the latest per declared product pattern, and for a repository without declarations the pattern `v*.*.*` (semver with an optional `v`). At most 5 per repository, filled pattern by pattern in the order the patterns are declared (each newest first), so that the later patterns are listed only while room is left; there is one entry per (pattern, tag), so a tag that two patterns match counts once for each, and the latest tag of every pattern is listed (not capped), also when it is the unreleased one. Only semantic-version tags are considered; other schemes (`calver`, free patterns) need their own later specification. The finding is `yellow` (`tag-without-release`); a tag whose release workflow run **failed** is reported as `release-run-failed` instead (one item per tag, see CI state and the action model). Older unreleased tags are history, not action.

### CI state (WP-C, required)

Bounded to the **latest** run; no history is read.

- **Default branch** (`ci-main`): head SHA of `branches/DEFAULT` and the `internal/watch` class (`none|pending|green|red`) from the
  existing check-run and status reads of `stw pulls`; `red` is a `red` item (`main-red`), `pending` and `none` are information in the
  repository block.
- **Release workflow run** (`ci-release`): each tag entry of the `tags` read (the latest tag of a release tag pattern or an unreleased tag,
  at most 6 tags per repository in all, in the order of the entries) gets the newest run of the release workflow of **its product**: the
  file name of the `ci.release_workflow` of the contract that declares the pattern (Spec 026 config; a path, its file name is used in the
  request), or `release.yml` (the name the member repositories use) for a pattern no contract declares. A repository without contract files
  uses the defaults. A contract that **exists but is invalid or unreadable** is not replaced by the defaults: the source ends `error` with
  the code `contract-invalid` (exit 1; the owner fixes the contract). A 404 for a workflow means `skipped`, reason `not-found`, with a
  notice (not a failure). The newest run is read through the existing `runs?head_sha=SHA&per_page=100&page=1` shape (at most 20 runs
  are kept in the JSON; a run whose `head_sha` is not the tag's commit is ignored). Conclusion `failure`, `cancelled` or `timed_out` is a
  failed run; one not completed, or with a status or conclusion this specification does not know, is information.
- **One item per tag.** A tag whose run failed yields exactly one item, `release-run-failed` (`red`), whether the tag is released or
  not and whether it is the latest or an unreleased one; the `tag-without-release` item is **not** emitted for that tag. Without a
  failed run an unreleased tag yields `tag-without-release` (`yellow`).
- **Local default branch behind origin** needs no fetch: the remote default-branch head SHA (from `branches/DEFAULT`, `ci-main`) is looked up in the local object
  store; absent locally, or not an ancestor-descendant of the local default branch tip, means "origin has commits you do not have"
  (`main-behind-origin`). Local Git only; no write; with `--offline` the item cannot exist.

## The action model

Every finding that deserves a human becomes an **action item**; findings that are only context stay in the repository blocks.

**Priorities** (fixed order, colour in brackets): `red` act now, `yellow` soon, `green` ready to merge or publish, `blue` information
worth knowing, `violet` bot and dependency updates. Priority is a property of the item, separate from Spec 033's `attention`, which
keeps its meaning and is untouched. Rule of thumb for the mapping below: every Spec 033 attention reason is `yellow` unless it is a
risk to work in progress (`operation`, `conflicts`, `error`: `red`); `blue` is reserved for facts Spec 033 calls information only.

| # | Category id | Subject (id part) | Condition | Priority | Suggested command (hint, never run) |
| --- | --- | --- | --- | --- | --- |
| 1 | `repo-error` | `-` | repository state `error` (Spec 033 attention `error`) | red | `stw status --offline PATH` |
| 2 | `local-operation` | `operation` or `conflicts` | rebase, merge, cherry-pick, revert, bisect in progress; `conflicts` > 0 | red | `git -C PATH status` |
| 3 | `main-red` | default branch | `ci-main` class `red` | red | `gh run list --repo OWNER/NAME --branch DEFAULT` |
| 4 | `release-run-failed` | tag | newest release run of a tag failed (rule "one item per tag") | red | `gh run list --repo OWNER/NAME --commit SHA` |
| 5 | `read-error` | source id | a remote read of the repository failed or was skipped by a rate-limit stop (state `error`); deadline skips are not items | yellow | `stw pulls --repo OWNER/NAME` for source `pulls`; none for the other sources |
| 6 | `local-changes` | `dirty` | staged or unstaged files (attention `staged`, `unstaged`) | yellow | `git -C PATH status` |
| 7 | `local-detached` | `detached` | detached HEAD in the main worktree (attention `detached`) | yellow | `git -C PATH status` |
| 8 | `local-untracked` | `untracked` | untracked files (attention `untracked`) | yellow | `git -C PATH status` |
| 9 | `local-behind` | `behind` or `diverged` | behind or diverged from the upstream (attention `behind`, `diverged`) | yellow | `stw sync --plan PATH` |
| 10 | `local-no-upstream` | `no-upstream` | a branch without upstream (attention `no-upstream`) | yellow | `git -C PATH branch -vv` |
| 11 | `main-behind-origin` | default branch | origin's default branch has commits the local clone lacks (no Spec 033 attention; remote fact) | yellow | `stw sync --plan PATH` |
| 12 | `gitlink-drift` | submodule path | checked-out commit behind, diverged from or unrelated to the recorded one (attention `gitlink-drift`) | yellow | `stw status --offline PATH` |
| 13 | `worktree-dirty` | worktree path | a linked worktree with changes (information in Spec 033, a risk to unsaved work here) | yellow | `git -C WORKTREE status` |
| 14 | `pr-conflict` | PR number | non-bot, non-draft PR in group `conflict` | yellow | `stw pulls --repo OWNER/NAME` |
| 15 | `pr-blocked` | PR number | non-bot, non-draft PR in group `blocked` whose row was read (any reason except past-budget) | yellow | `stw pulls --repo OWNER/NAME` |
| 16 | `tag-without-release` | tag | unreleased tag without a failed run | yellow | none; hint text: see the repository's `docs/releasing.md`; the draft variable must be set **before** the tag is pushed |
| 17 | `pr-ready` | PR number | non-bot, non-draft PR in group `ready` | green | `gh pr merge N -R OWNER/NAME --rebase` |
| 18 | `release-draft` | tag | release draft with `write-access` visibility | green | `gh release edit TAG -R OWNER/NAME --draft=false` |
| 19 | `pr-checks-pending` | PR number | non-bot, non-draft PR in group `checks` | blue | `stw pulls --repo OWNER/NAME` |
| 20 | `pr-unread` | PR number | non-bot PR **past the detail budget** only (the `PastBudget` flag of the row) | blue | `stw pulls --repo OWNER/NAME` |
| 21 | `local-stash` | `stash` | stash count > 0 (information in Spec 033) | blue | `git -C PATH stash list` |
| 22 | `gitlink-ahead` | submodule path | checked-out commit ahead of the recorded gitlink | blue | `git -C ROOT add PATH` (effect `owner`) |
| 23 | `branches-unmerged` | `branches` | `branches.not_integrated` > 0 | blue | `stw tidy plan PATH` |
| 24 | `worktree-stale` | worktree path | linked worktree prunable, missing or locked | blue | `stw tidy plan PATH` |
| 25 | `pr-bot` | PR number | non-draft PR by a bot, whatever its group, including past the budget | violet | `stw pulls --repo OWNER/NAME --bots-only` |

Every suggested command exists in the current CLI (`stw status`, `stw sync [PATH] --plan`, `stw tidy plan [PATH]` of the `tidy`
family, `stw pulls --repo`, `--bots-only`) or is a plain `gh` or `git` command; `stw status --offline` exists once this spec is
implemented. WP-E re-checks every string against `internal/cli` in a test.

**Rules.**

- **Distinct local facts, distinct items.** Local rows have a subject per fact kind (`dirty`, `detached`, `untracked`, `behind` or
  `diverged`, `no-upstream`, `stash`, `branches`, a path), so a repository that is dirty **and** behind yields both items. Precedence
  (first row wins) applies only to the **same subject**: one PR, one tag, one fact kind of one repository.
- **PRs.** Evaluate in this order: (1) a **draft** is never an item, bot or not (counted in the block); (2) a **bot** non-draft PR is
  `pr-bot` (violet) whatever its group, also past the budget; (3) a non-bot PR past the budget is `pr-unread`; (4) otherwise by
  group: `ready` `pr-ready`, `conflict` `pr-conflict`, `blocked` `pr-blocked`, `checks` `pr-checks-pending`. A row whose detail read
  failed or was skipped after a rate limit is not a PR item: the repository gets one `read-error` item (row 5), so no row is
  categoryless or counted twice.
- **Effect of a hint.** `effect` is `read` (changes nothing when run) or `owner` (changes something, and is the **owner's**
  operation: `gh pr merge`, `gh release edit`, `git -C ROOT add PATH`). Hints are strings; **`stw` never runs a suggested command**.
  `PATH` and repository names are validated tokens, shell-quoted when they contain anything outside `[A-Za-z0-9._/-]`; free text from
  the forge never enters a command except a release tag, which passes the release-tag validation of Spec 026 or the item has no
  command.

**Ordering (deterministic).** Items sort by priority (`red`, `yellow`, `green`, `blue`, `violet`), then by category number of the
table, then repository name (case-insensitive), then subject (PR number numerically, tag by version descending, path). The same data
yields the same items, ids and order regardless of timing, parallelism or repository order. **Item id**: `CATEGORY:REPO:SUBJECT`
(`pr-ready:wstein/sourcesteward:57`, `main-red:wstein/sourcesteward:main`, `local-changes:crewbook:dirty`), stable across runs for the
same finding. The JSON lists **every** item, one per id, no cap.

## Human output

```
Needs action: 6 items
 ✖ red     wstein/sourcesteward  default branch CI is red (main, 3f2a9c1)            https://github.com/wstein/sourcesteward/actions
           run: gh run list --repo wstein/sourcesteward --branch main
 ▲ yellow  wstein/crewbook       PR #12 has conflicts: Update docs                    https://github.com/wstein/crewbook/pull/12
           run: stw pulls --repo wstein/crewbook
 ✔ green   wstein/sourcesteward  PR #57 is ready to merge: build(deps): bump cobra    https://github.com/wstein/sourcesteward/pull/57
           run: gh pr merge 57 -R wstein/sourcesteward --rebase
 ✔ green   wstein/bundledock     release draft v0.1.0 (4 assets) is waiting           https://github.com/wstein/bundledock/releases
           run: gh release edit v0.1.0 -R wstein/bundledock --draft=false
 ⬆ violet  wstein/workharbor     2 bot PRs: #3, #4 (dependabot)
           run: stw pulls --repo wstein/workharbor --bots-only

Repositories
  sourcesteward  main  = origin/main  clean
    remote: 2 open PRs (1 ready, 1 draft), main CI red, no release drafts
  ...
Notices
  remote: release drafts of wstein/crewbook are not visible (token has no write access)

summary: 1 red, 1 yellow, 2 green, 0 blue, 2 violet | 7 repositories | 1 notice
```

The header counts **items**, the lines may be fewer: items with the same (priority, category, repository) are **grouped into one line**
with the count and the first 3 identifiers (`+N more` beyond), here the two bot PRs of `wstein/workharbor` (6 items, 5 lines, so the
summary counts 2 violet). A grouped line carries no URL, only the drill-down command; a single item keeps its URL. The JSON keeps
one item per id and no grouping. The human section shows at most 10 lines per priority and `and N more` with the drill-down command.

- Section order: **Needs action**, **Repositories** (the Spec 033 block per repository, plus one `remote:` line), **Notices**, the
  **summary** line (counts per priority in the fixed order, repository count, notice count; `nothing needs action` when empty).
- An item line is `ICON LEVEL  REPO  TEXT  URL`, the next line `run: COMMAND` (indented, no colour needed to read it). Colour paints the
  icon and level word only. A level word is always printed, so the output is complete without colour or icons.
- **Icon sets** (priority icons; every set has one distinct glyph per priority, so the meaning never depends on colour):

  | Priority | `unicode` | `nerd` (Nerd Font, private-use code points, verify at implementation) | `ascii` |
  | --- | --- | --- | --- |
  | red | `✖` | `nf-fa-times_circle` | `[!!]` |
  | yellow | `▲` | `nf-fa-warning` | `[! ]` |
  | green | `✔` | `nf-fa-check_circle` | `[ok]` |
  | blue | `ℹ` | `nf-fa-info_circle` | `[i ]` |
  | violet | `⬆` | `nf-fa-arrow_circle_up` | `[^ ]` |

  A repository block may use further small glyphs per set, defined in one table; the fallback of every glyph is its `ascii` form.
- The `remote:` line names the non-zero categories of the pull requests (ready, checks pending, red, conflict, draft, other blocked),
  the CI state of the default branch (WP-C), and the release drafts (name or tag, creation date, asset count; up to 3, then `and N
  more`) or the visibility notice.
- Untrusted strings (PR titles, release names, tags, error text) pass the same `esc` as `stw pulls` and are cut at 120 characters;
  URLs are `https` only; the token is never printed.

## Degradation, timeouts, exit status

- **Notices.** One notice per **missing source**, not per repository, deterministic wording, e.g. `no GitHub token (run: gh auth login):
  pull requests, releases and tags were not read`, `network error: pull requests of N repositories not read` (a failed attempted request, exit 1), `rate limit reached at HH:MM UTC:
  3 repositories not read` (the reset time is shown when GitHub gave it; the command never waits), `deadline of 90 s reached: remote
  sources of N repositories not read`, `release drafts of OWNER/NAME not visible (token has no write access)`. Notices are on stdout
  in the Notices section and in the JSON; progress is the only thing on stderr.
- **No usable token**: when token resolution finds no usable token **before any request is made**, no request is sent; the local view
  plus the one notice is printed, exactly the `--offline` content plus the notice, exit 0.
- **Per-source independence**: a failing source (`pulls`, `releases`, `tags`, `ci-*`) leaves the others of the repository and
  all other repositories intact; its repository block says which part is missing.
- **Concurrency and time (per source).** The local half is computed first and completely. Remote sources run in two groups: the PR
  source through the pulls cycle's own pool (**parallelism 4 across repositories**, sequential within a repository, as in Spec
  036); the other sources (`repo`, `releases`, `tags`, `ci-main`, `ci-release`) through **their own worker pool of 4 across
  repositories**, sequential per source within a repository. **15 s per request** (a per-request timeout option of the hosting
  reader; other commands keep 30 s) and an **overall deadline of 90 s** (`--timeout`, default 90s) per run. When the deadline
  passes, requests not yet started are not sent and those in flight are cancelled: every such source is **`skipped`, reason
  `deadline`**, a notice says `deadline of 90 s reached: N sources of M repositories not read; raise --timeout`, the repository block
  says `remote: not read (deadline)`. Each source keeps what it finished: partial results per source, never an all-or-nothing run.
  Output is rendered once, deterministically ordered.
- **Rate limit.** On exhaustion (403/429 with evidence) all remaining remote reads stop; the sources not yet read are `error`
  `rate-limited` (a failed read), the command **never sleeps or retries**. Search is never used.
- **Exit status (deterministic).** **Exit 0** in exactly these situations (with a notice where something was not read): everything
  attempted succeeded, `--offline`, no usable token detected before any request (`no-token`), requests prevented or cancelled by the
  deadline (`skipped`, `deadline`), and a 404 for the release workflow (`skipped`, `not-found`). **Exit 1** on a single condition:
  an **attempted** request failed at the transport, API or response level (network error, 401/403, 5xx, rate-limit stop, malformed
  or oversized answer, `repo-moved`); that source reports an error, the partial output is printed, as `stw pulls`. Also 1 when a
  repository cannot be read locally or the run fails; 2 usage error. Items and priorities never affect the status. The result
  depends on what was attempted, not on timing: a network that fails on the first request exits 1 whenever a request was attempted.

## Issues (WP-G, specified in Spec 038)

The issue source (per-repository `issues: N open (R red, Y yellow)` line, the `issue-security`, `issue-assigned` and `issue-bug` items as categories 26 to 28 of the action table, the `viewer` read, the two new request shapes admitted by guard mode `status` and the additive JSON keys) is specified in [Spec 038](../038-stw-issues/spec.md#integration-into-stw-status-wp-g-of-spec-037); the deadline, skip, exit and rate-limit rules of this spec apply to it, with exactly one deviation, the user-lookup exception: a `GET user` answering 403 or 404 **without** rate-limit evidence is a skip (reason `no-identity`, exit 0), whereas this spec's exit table treats 401/403 as exit 1 (a 403 with rate-limit evidence stays `rate-limited`, exit 1); this is separate from the `not-found` skip of the release workflow. No exit code is added.

## Required seams (WP-0)

(a) `pulls.Run` returns the **rows read so far** with `not-read`/`deadline` marks and a `PastBudget` flag on every row when its context
expires, instead of `nil, err` (also an improvement for `stw pulls`, with its own tests; `stw pulls` output changes only on expiry);
(b) a per-request timeout option of the hosting reader (default 30 s unchanged for every other command; status uses 15 s);
(c) the exported `PastBudget` flag, so `pr-unread` means past the budget only. The orchestration with the per-source pools described above is not a WP-0 seam: it is WP-R of the plan.

## Cost model

Per GitHub repository: `repo` 1 read, open PR list `ceil(open/25)` pages, about 3 reads per non-draft PR within the run budget of
200 (plus at most one re-read per unknown-mergeability PR), `releases` 1 page typically (at most 10), `tags` 1 page typically (at
most 10), and with WP-C `branches/DEFAULT` 1, check runs and statuses 2, `runs` 1 per declared release workflow. Example: 7
repositories, 12 open PRs: about 80 requests, 5 to 15 s. Worst case: the budget (about 600 reads) plus at most 30 per repository. A
personal token has 5000 requests an hour, an Actions `GITHUB_TOKEN` 1000; three runs a day are far below either.

## Amendment to Spec 022 FR-005 (explicit)

Only the `stw status` online reader may use the new guard mode; one-shot operations, `stw watch` and `stw pulls` keep their modes.

1. **A fourth guard mode `status`.** The mode enum (`guardOneShot`, `guardWatch`, `guardPulls`) gains `guardStatus`. It admits the shapes of
   `guardWatch` (open PR list, `pulls/N`, `branches/BASE`, check runs, statuses, `rate_limit`) and **refuses `search/issues`**, which only
   `guardPulls` admits. Why a new mode and not `pulls` or `watch`: least privilege. Status never discovers owners, so admitting the
   search shape (a different endpoint class with its own, much smaller limit and an owner string in the query) would widen the
   surface for no use; changing `watch` would alter an accepted reader. A new mode costs one enum value and one line in
   `admitsWatchShapes`; no other mode gains or loses a shape.
2. **No new request string (decided).** `repos/OWNER/NAME/releases?per_page=100&page=N`, `tags?per_page=100&page=N` and
   `actions/workflows/FILE/runs?head_sha=SHA&per_page=100&page=N` (N 1..10) are existing fixed shapes of Spec 026 (`releaseRoute` in
   `internal/hosting/release.go`), part of the one-shot set that **every** mode already admits, and `repos/OWNER/NAME` is admitted by
   `tidyRoute`. The premise that a new releases shape is needed is therefore wrong: the amendment adds none. It records that the status
   reader reads these through new typed decoders (name, tag, `created_at`, `html_url`, `draft`, asset count, tag commit; the existing
   `ReleaseEntry` is not widened because release verification depends on it), with the `Link` bound of 10 pages, the 1 MiB cap (FR-006)
   and a 15 s per-request context (FR-007; a per-request timeout option of the hosting reader, default 30 s kept for other commands). The behaviour of every existing mode is unchanged (they still admit what they admitted, and
   `search/issues` stays `guardPulls` only); no claim and no test says the other modes refuse the release shapes. Tested: `guardStatus`
   refuses `search/issues` and everything outside its set.
3. Spec 022 FR-005 and Spec 033 carry pointer sentences to this section. FR-006 and FR-007 apply as in Specs 035 and 036.

## Security

GET only through the Spec 022 bounded go-gh transport (15 s per request here, 1 MiB, no redirects, no cache); the token stays in the hosting layer and
is never printed, logged or put in an error, notice, command or JSON. OWNER/NAME come from the local `origin` and pass
`watchcmd.ValidRepo` before any request (invalid: `skipped`, `invalid-repo`). All forge strings are untrusted data: human output
escapes control characters and bounds text exactly like `internal/watchcmd/human.go` (`esc`, 120 characters); JSON strings are cut at
2048 bytes on a UTF-8 boundary; errors are sanitized and at most 512 bytes. Suggested commands are strings built from validated
tokens only and are never executed by `stw`. No write, no state file, no lock, no journal; repository content is data.

## Non-goals

No writes of any kind (publishing, merging, tagging, labelling), no execution of suggested commands, no review-decision or foreman
CLEAR-ledger reading, no closed or merged PRs, no owner discovery or filters (use `stw pulls`), no cross-host runs, no web UI (the
JSON is only kept consumer-friendly for it), no state or history between runs, no Windows content, no change to the Spec 033
facts or to `--offline` output.

## Risks

- The default of an accepted command changes (online). Scripts that must stay offline use `--offline`; the v1 JSON is frozen and
  unchanged there.
- `mergeable` is lazily computed (mitigated by the one bounded re-read; a still-unknown PR is `blocked`, reason `unknown`).
- A releases page of 100 entries with many assets can pass the 1 MiB cap: that repository's release section degrades to a `page-too-large` notice (a failed read, exit 1) and the rest is unaffected, as for the search read of `stw pulls`.
- Draft visibility depends on token scope as well as role; `write-access` with a restricted fine-grained token may still hide drafts.
- Tags carry no creation date in the `tags` read; "latest" and "newer" follow the version scheme of the tag pattern, not time.
- The shared rate limit with other tools of the same token.

## Decisions recorded (owner and foreman, 2026-10-22)

1. **Reuse the existing releases, tags and runs shapes; no new request string.** The new `status` guard mode stays and refuses
   `search/issues`; every other mode behaves as before. Rationale: the shapes exist and are admitted already, a new string would be
   pure surface.
2. **Exit codes (refined after review).** `--offline` or no usable token before any request: local status plus one notice per missing
   source, exit 0; an **attempted** request that fails (transport, API, malformed, rate-limit stop) is a source error, partial output,
   exit 1, as `stw pulls`; a deadline skip and a `not-found` skip are not failures; usage error 2. Rationale: the rule depends on what was attempted, never on
   timing.
3. **Re-read of unknown mergeability** is merged in the shared cycle (PR #149); status inherits it, `stw pulls` is unchanged.
4. **CI state is required (WP-C is not optional).** Default-branch CI and the latest release workflow run per repository, latest run
   only; red main or a failed tag run is `red`. Rationale: exactly what the owner must not miss.
5. **Unreleased tag rule.** A tag matching the repository's release tag pattern, newer than the latest published release, with no
   release (draft or published): `yellow` `tag-without-release`; if its release run failed, the single item is the `red` `release-run-failed` instead (never both).
6. **Deadlines.** 15 s per request, 90 s overall, overridable with `--timeout`; on expiry partial results, a notice and sources
   `skipped` with reason `deadline`; a deadline skip is not a failed read (exit 0 if nothing else failed).
7. **A releases page over 1 MiB** degrades that repository's release section to a `page-too-large` notice, exit 1, the rest is
   unaffected (as the search read of `stw pulls`).
8. **Keep the write-access read** (`permissions` of `repos/OWNER/NAME`, an admitted shape). When the token cannot see drafts the command
   says so, and without evidence it says "release draft visibility unknown"; it never prints "0 drafts" as a fact.
9. **Suggested commands** (hints only, never run): merge `gh pr merge N -R OWNER/NAME --rebase` (the owner merges, effect `owner`); publish
   `gh release edit TAG -R OWNER/NAME --draft=false`; the tag item has no command, only a hint text: follow that repository's `docs/releasing.md`; the draft variable must be set **before** the tag is pushed; drill-down `stw pulls --repo OWNER/NAME`.
10. **Caps and the violet rule.** The human output caps each section at 10 lines per priority with "N more" and the drill-down
    command; the JSON is complete. Every bot PR, even a ready one, is `violet`.

Residual points to verify during implementation (not owner questions): the Nerd Font code points, and the exact reading of the
`.stw/release.toml` tag grammar for the unreleased-tag rule.
