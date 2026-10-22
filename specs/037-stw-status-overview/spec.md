# Specification: `stw status`, the online, action-oriented workspace overview

**Created**: 2026-10-22 | **Status**: proposed (spec-light: this page, [contracts/status.md](contracts/status.md) and
[plan.md](plan.md) are the whole design; no implementation yet). It **amends** [Spec 033](../033-stw-status/spec.md): the default of
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
| **C. Online by default, `--offline` for the old behaviour** | **5/5, chosen** | One command, whole picture, action first. The fast local-only view survives as a flag. Missing token or network degrades to the local view with clear notices. |
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
- **US5 (CI).** The CI state of the default branch and of the latest tag's release workflow run (WP-C, optional).
- **US6 (local).** The existing local facts (changes, operation, behind or diverged, gitlink drift, unmerged branches, linked
  worktrees) plus "local default branch behind origin", mapped to priorities as well.
- **US7 (degrade).** No token, no network, rate limit or timeout: the local view and the sources that worked are printed, with one
  clear notice per missing source; never an error wall.
- **US8 (machine).** `--json` prints one stable, versioned document: action items with ids, priorities, categories, deep links and
  suggested commands, for the owner's later web UI (out of scope here), and the per-repository data.
- **US9 (presentation).** Colour, three icon sets, a bottom summary line, a progress indicator on stderr on a terminal; the output
  stays copy-paste friendly without colour.

## CLI synopsis

```
stw status [PATH] [--offline] [--fetch] [--worktrees] [--icons unicode|nerd|ascii] [--json]
```

- `--offline`: local only, no request, no credential read, Spec 033 output and JSON v1 unchanged. `--fetch` and `--worktrees` keep
  their meaning and compose with both modes (`--fetch` runs git; it is independent of the forge reads).
- `--icons SET` (human output only): `unicode`, `nerd` (Nerd Font glyphs) or `ascii`. Default: `unicode` on a terminal whose locale
  is UTF-8, `ascii` otherwise (a pipe, a file, a non-UTF-8 locale, `TERM=dumb`). An explicit value always wins. `--icons` with
  `--json` is accepted and ignored.
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
| `ci-main` | `branches/DEFAULT`, then check runs and statuses of its head | existing watch shapes | WP-C (optional) |
| `ci-release` | `actions/workflows/FILE/runs?head_sha=SHA&per_page=100&page=1` for the latest tag's commit | existing (Spec 026) | WP-C (optional) |

### Open pull requests (reuse, no new shape)

One call of the `stw pulls` cycle (`internal/pulls`, Spec 036 verdict table and groups, unmodified) over the GitHub repositories with
the default options (no filters, bots included, budget 200 non-draft PRs for the run, no detail for drafts). Status code only
**consumes the `pulls.Result`**: per-repository counts and the PR rows, never its own list, `pulls/N`, check or classification code.
**The single bounded re-read** of lazily unknown mergeability: after its first pass the cycle reads `pulls/N` once more, once, for each
PR whose first detail had `mergeable` null or `mergeable_state` `unknown`, inside the same budget, without sleeping; this is shared cycle
behaviour (Open question 3: the implemented Spec 036 cycle has none yet).

### Release drafts, tags and visibility

- **Drafts.** Releases with `draft` true: `name`, `tag_name`, `created_at`, `html_url`, asset count. Published releases are read and
  used only to know which tags have a release. The `Link` header's `rel="next"` is followed, never beyond page 10 (error
  `too-many-releases`).
- **Visibility.** GitHub shows drafts only to a token whose user has write access, and does not say that it withheld any. The `repo`
  read's `permissions` (`push`, `maintain` or `admin` true) gives `drafts_visibility`: `write-access`, `no-write-access` or `unknown`.
  With `no-write-access`/`unknown` the command **does not report zero**: it says "release drafts not visible to this token (no write
  access)" or "release drafts unknown (write access not determined)" and creates no draft item. A fine-grained token with write
  access but without the releases scope can still withhold drafts; documented residual (Risks).
- **Tags without release.** A tag is "unreleased" when no release (draft or published) carries its `tag_name`. Only **release-like
  tags** count (`v?MAJOR.MINOR.PATCH` with an optional `-prerelease`), and only those with a semantic version **greater than** the
  latest published release's (when no release is published, only the greatest tag), at most 5 per repository. Older unreleased tags
  are history, not action (Open question 5).

### CI state (WP-C, optional)

- **Default branch** (`ci-main`): head SHA of `branches/DEFAULT` and the `internal/watch` class (`none|pending|green|red`) from the
  existing check-run and status reads of `stw pulls`; `red` is an item, `pending` and `none` are information in the repository block.
- **Latest tag's release run** (`ci-release`): the commit of the latest release-like tag (from the `tags` read) and the runs of the
  workflow files declared in the repository's `.stw/release.toml` (Spec 026; no declaration means `skipped`, reason
  `no-release-workflows`) through the existing `runs?head_sha=SHA&per_page=100&page=1` shape. A run with conclusion `failure`,
  `cancelled` or `timed_out` is red; one not completed is information. Why optional: it needs `release.toml` parsing in status, two
  more reads per repository, and its value depends on the owner's release flow; the default-branch part is cheap and has **no** new
  dependency. If WP-C is not built, the items `main-red` and `release-run-failed` do not exist and the sources are `skipped`
  (reason `not-implemented`; the notice names it, never silent). See Open question 4.
- **Local default branch behind origin** needs no fetch: the remote default-branch head SHA (from `branches/DEFAULT`, WP-C, or from
  the `repo` read plus one `branches/DEFAULT` read when WP-C is not built, which is the one WP-B read) is looked up in the local object
  store; absent locally, or not an ancestor-descendant of the local default branch tip, means "origin has commits you do not have"
  (`main-behind-origin`). Local Git only; no write; with `--offline` the item cannot exist.

## The action model

Every finding that deserves a human becomes an **action item**; findings that are only context stay in the repository blocks.

**Priorities** (fixed order, colour in brackets): `red` act now, `yellow` soon, `green` ready to merge or publish, `blue` information
worth knowing, `violet` bot and dependency updates. Priority is a property of the item, separate from Spec 033's `attention`, which
keeps its meaning and is untouched.

| # | Category id | Condition | Priority | Suggested command (owner-side hint, never run) |
| --- | --- | --- | --- | --- |
| 1 | `repo-error` | repository state `error` | red | `stw status --offline PATH` |
| 2 | `local-operation` | rebase, merge, cherry-pick, revert, bisect in progress, or `conflicts` > 0 | red | `git -C PATH status` |
| 3 | `main-red` | `ci-main` class `red` (WP-C) | red | `gh run list --repo OWNER/NAME --branch DEFAULT` |
| 4 | `release-run-failed` | `ci-release` failed for the latest tag (WP-C) | red | `gh run list --repo OWNER/NAME --commit SHA` |
| 5 | `local-changes` | staged or unstaged files, `detached`, a dirty linked worktree | yellow | `git -C PATH status` |
| 6 | `local-behind` | behind or diverged from upstream | yellow | `stw sync --plan PATH` |
| 7 | `main-behind-origin` | origin's default branch has commits the local clone lacks | yellow | `stw sync --plan PATH` |
| 8 | `gitlink-drift` | checked-out commit behind, diverged from or unrelated to the recorded gitlink | yellow | `stw status --offline PATH` |
| 9 | `pr-conflict` | non-bot PR in group `conflict` | yellow | `stw pulls --repo OWNER/NAME` |
| 10 | `pr-blocked` | non-bot PR in group `blocked` (any reason except `not-read`) | yellow | `stw pulls --repo OWNER/NAME` |
| 11 | `tag-without-release` | unreleased release-like tag (rule above) | yellow | `gh release create TAG --draft --repo OWNER/NAME` |
| 12 | `pr-ready` | non-bot PR in group `ready` | green | `gh pr merge N --rebase --repo OWNER/NAME` |
| 13 | `release-draft` | release draft with `write-access` visibility | green | `gh release edit TAG --draft=false --repo OWNER/NAME` |
| 14 | `pr-checks-pending` | non-bot PR in group `checks` | blue | `stw pulls --repo OWNER/NAME` |
| 15 | `pr-unread` | PR past the budget (`not-read`) | blue | `stw pulls --repo OWNER/NAME --limit 500` |
| 16 | `local-untracked` | untracked files, nothing else local | blue | `git -C PATH status` |
| 17 | `gitlink-ahead` | checked-out commit ahead of the recorded gitlink | blue | `git -C ROOT add PATH` |
| 18 | `branches-unmerged` | `branches.not_integrated` > 0 | blue | `stw tidy PATH` |
| 19 | `worktree-stale` | prunable or missing linked worktree | blue | `stw tidy PATH` |
| 20 | `pr-bot` | **any** PR by a bot, whatever its group | violet | `stw pulls --repo OWNER/NAME --bots-only` |

Rules: draft PRs are counted in the block, never an item. A finding matching several rows of the **same subject** (one PR, one
repository fact) takes the first row in table order (a PR cannot be both `pr-bot` and `pr-ready`: bots are tested first, so row 20
is evaluated before 9 to 15 for PRs, the numbering above is the display order within a priority, not the evaluation order for PRs).
Commands that change something (`gh pr merge`, `gh release edit`, `gh release create`) carry `effect: "write"`: they are the owner's
operation, not the tool's; the others are `effect: "read"`. `PATH` and repository names are validated tokens, shell-quoted when
they contain anything outside `[A-Za-z0-9._/-]`; free text from the forge never enters a command except a release tag, which
passes the release-tag validation of Spec 026 or the item has no command.

**Ordering (deterministic).** Items sort by priority (`red`, `yellow`, `green`, `blue`, `violet`), then by category number of the
table, then repository name (case-insensitive), then subject (PR number numerically, tag by semantic version descending, path). The
same data yields the same items, ids and order regardless of timing, parallelism or repository order. **Item id**:
`CATEGORY:REPO:SUBJECT` (`pr-ready:wstein/sourcesteward:57`, `main-red:wstein/sourcesteward:main`, `local-changes:crewbook:-`), stable
across runs for the same finding. At most 500 items; beyond that `summary.items_truncated` is true and the surplus is counted per
priority; the human section shows at most 10 items per priority and `and N more` with the drill-down command.

## Human output

```
Needs action (9)
 ✖ red     wstein/sourcesteward  default branch CI is red (main, 3f2a9c1)            https://github.com/wstein/sourcesteward/actions
           run: gh run list --repo wstein/sourcesteward --branch main
 ▲ yellow  wstein/crewbook       PR #12 has conflicts: Update docs                    https://github.com/wstein/crewbook/pull/12
           run: stw pulls --repo wstein/crewbook
 ✔ green   wstein/sourcesteward  PR #57 is ready to merge: build(deps): bump cobra    https://github.com/wstein/sourcesteward/pull/57
           run: gh pr merge 57 --rebase --repo wstein/sourcesteward
 ✔ green   wstein/bundledock     release draft v0.1.0 (4 assets) is waiting           https://github.com/wstein/bundledock/releases/tag/untagged-abc
           run: gh release edit v0.1.0 --draft=false --repo wstein/bundledock
 ⬆ violet  wstein/workharbor     2 bot PRs (dependabot)                               https://github.com/wstein/workharbor/pulls
           run: stw pulls --repo wstein/workharbor --bots-only

Repositories
  sourcesteward  main  = origin/main  clean
    remote: 2 open PRs (1 ready, 1 draft), main CI red, no release drafts
  ...
Notices
  remote: release drafts of wstein/crewbook are not visible (token has no write access)

summary: 1 red, 1 yellow, 2 green, 3 blue, 2 violet | 7 repositories | 2 notices
```

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
  pull requests, releases and tags were not read`, `network unreachable: remote sources not read`, `rate limit reached at HH:MM UTC:
  3 repositories not read` (the reset time is shown when GitHub gave it; the command never waits), `timed out after 60 s: remote
  sources of N repositories not read`, `release drafts of OWNER/NAME not visible (token has no write access)`. Notices are on stdout
  in the Notices section and in the JSON; progress is the only thing on stderr.
- **No token or no network at all**: the local view plus the one notice, exactly the `--offline` content plus the notice.
- **Per-source independence**: a failing source (`pulls`, `releases`, `tags`, `ci-*`) leaves the others of the repository and
  all other repositories intact; its repository block says which part is missing.
- **Parallelism and time.** The local half is computed first and completely. Remote reads: at most 4 repositories in parallel,
  sequential within a repository, 30 s per request (Spec 035/036 rule) and an **overall deadline of 60 s** for the remote half
  (constant, Open question 6); when it passes, unread sources are marked `timeout`. Output is rendered once, deterministically ordered.
- **Rate limit.** On exhaustion (403/429 with evidence) all remaining remote reads stop, the sources not yet read are
  `rate-limited`, the command **never sleeps or retries**. Search is never used.
- **Exit status.** 0 whenever the command produced its overview, **including** when the remote half is degraded (no token, no network,
  rate limit, timeout, skipped repositories) and when items exist; the document says `summary.degraded` true and lists the notices.
  **1** when a repository cannot be read locally (as in Spec 033) or the run itself fails; 2 usage error. Items and priorities never
  affect the exit status. (Open question 2: a stricter code for degraded runs.)

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
2. **The release reads need no new request string.** `repos/OWNER/NAME/releases?per_page=100&page=N`, `tags?per_page=100&page=N` and
   `actions/workflows/FILE/runs?head_sha=SHA&per_page=100&page=N` (N 1..10) are existing fixed shapes of Spec 026 (`releaseRoute` in
   `internal/hosting/release.go`), part of the one-shot set that **every** mode admits, and `repos/OWNER/NAME` is admitted by
   `tidyRoute`. The amendment therefore records that the status reader reads them through new typed decoders (name, tag, `created_at`,
   `html_url`, `draft`, asset count, tag commit; the existing `ReleaseEntry` is not widened because release verification depends on
   it), with the `Link` bound of 10 pages, the 1 MiB cap (FR-006) and the 30 s per-request context (FR-007). Because these shapes
   are already admitted in all modes, "the other modes refuse the release listing" is **not** claimed; what is tested is that
   `guardStatus` refuses search and everything outside its set, and that the other modes admit exactly what they admitted before.
   Open question 1: a dedicated status-only releases shape instead?
3. Spec 022 FR-005 and Spec 033 carry pointer sentences to this section. FR-006 and FR-007 apply as in Specs 035 and 036.

## Security

GET only through the Spec 022 bounded go-gh transport (30 s, 1 MiB, no redirects, no cache); the token stays in the hosting layer and
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
- A releases page of 100 entries with many assets can pass the 1 MiB cap: that repository reports `page-too-large` for `releases`
  (Open question 7).
- Draft visibility depends on token scope as well as role; `write-access` with a restricted fine-grained token may still hide drafts.
- Tags carry no creation date in the `tags` read; "latest tag" is by semantic version, not time.
- The shared rate limit with other tools of the same token.

## Open questions (for the owner)

1. **New shape or reuse?** The releases shape you named already exists and is admitted in all modes. Reuse as specified
   (recommended), or add a dedicated page-less status-only shape?
2. **Exit code of a degraded run.** 0 as specified (a daily glance is never an error), or a distinct code (for example 3) so a
   script can tell?
3. **Re-read.** The implemented `stw pulls` cycle has no bounded re-read of unknown mergeability (Spec 036 does not describe one).
   Add it to the shared cycle (also changes `stw pulls`) or only when status asks?
4. **WP-C scope.** Build default-branch CI only, both CI sources, or neither in the first slice? The release-run part depends on
   workflows declared in `.stw/release.toml`; repositories without a declaration have no `ci-release`.
5. **Unreleased tags.** The rule (semver greater than the latest published release, at most 5) or every release-like tag?
6. **Overall deadline** 60 s: right, or longer, or a flag?
7. **Large release pages**: accept `page-too-large` per repository, or add a smaller-page shape?
8. **Write-access evidence**: keep the `repos/OWNER/NAME` read (exact message), or drop it and always say "no drafts found (drafts
   need write access)"? Recommended: keep.
9. **Suggested commands** for merge and publish (`gh pr merge N --rebase`, `gh release edit TAG --draft=false`): confirm they match
   your release process (G1 build-once-promote may prefer `stw release ...` for publishing).
10. **Item caps** (10 per priority in text, 500 in JSON) and the violet rule (every bot PR, even a ready one, is violet): confirm.
