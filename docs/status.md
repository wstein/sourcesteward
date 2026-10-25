# What needs action: `stw status`

`stw status` ([Spec 037](../specs/037-stw-status-overview/spec.md), which amends [Spec 033](../specs/033-stw-status/spec.md)) is
the one command to run in the morning, at midday and in the evening. It answers "what needs me?" for the whole workspace: it
starts with a **prioritised list of things to do**, then shows the state of every repository, then names anything it could
not read. It reads the local state of the workspace root and every submodule and, by default, what GitHub knows about each
repository: open pull requests, open issues, release drafts, tags without a release and the CI of the default branch and of the
release workflow. Everything is read-only. The command **suggests** and the owner acts: a suggested command is a hint that `stw` never
runs, and it says whether running it only reads (`read`) or changes something (`owner`).

```sh
stw status                      # the whole picture: needs action, repositories, notices, summary
stw status --offline            # local state only, no request and no credential read
stw status --timeout 2m         # a longer overall deadline for the remote half
stw status --icons nerd         # glyphs of the text: unicode, nerd or ascii
stw status --worktrees --fetch  # as before: list linked worktrees, fetch first
stw status --json | jq '.actions[] | select(.priority == "red") | .id'
```

## Online by default, `--offline` for the old behaviour

| | Online (default) | `--offline` |
| --- | --- | --- |
| Reads | local state and the GitHub sources below | local state only |
| Requests | `GET` only, through the `gh` token of the other hosting commands | none; no credential is looked up |
| Text | Needs action, Repositories, Notices, summary | the Spec 033 screen, byte for byte |
| `--json` | [schema 2](json-contracts.md#status-schema-v2-unpublished) | [schema 1](json-contracts.md#status-schema-v1-unpublished), unchanged |
| `--timeout` | overall deadline of the remote half | a usage error (exit 2) |

## What is read

Per repository whose `origin` is a GitHub repository on `github.com` (other hosts and members without a usable origin are
skipped, with the reason named):

| Source | Reads | Gives |
| --- | --- | --- |
| `pulls` | the open pull requests, with the cycle and the verdict of [`stw pulls`](pulls.md) | `pr-*` items; the counts equal `stw pulls` for the same repository |
| `repo`, `releases`, `tags` | `repos/OWNER/NAME`, its releases and its tags | release drafts, tags without a release, the draft visibility |
| `ci-main` | the head of the default branch and its checks, and whether the local clone lacks that commit (decided in the local object store, nothing is fetched) | `main-red`, `main-behind-origin` |
| `ci-release` | the newest release workflow run of the latest tag of every release tag pattern and of at most five unreleased tags | `release-run-failed` |
| `issues` | the open issues, with the cycle and the priority rules of [`stw issues`](issues.md), and the login of the token (the `viewer`, one `user` read per run) | `issue-*` items and the line `issues: N open (R red, Y yellow)`; the counts equal `stw issues` for the same repository |

A repository declares its release tag patterns in the `.stw/release*.toml` contracts of its clone (semver and pattern tag
grammars); without a contract the pattern is `v*.*.*` and the workflow `release.yml`. A contract that exists but cannot be used
ends `ci-release` with the error `contract-invalid`; the defaults do not replace it.

**Cost.** A repository with N release tag patterns makes about `3 + 2 + (N + 5)` requests apart from its pull requests: `repos`,
`releases` and `tags` (page 1 each, more pages only when GitHub says there are more, at most 10), the branch and its check
runs and commit statuses, and one workflow-run read (page 1) for the latest tag of each pattern and for each of at most five
unreleased tags, so N+5 at most. Pull requests cost what `stw pulls` costs (one list per repository, the detail of at most 200
pull requests, one bounded re-read of an unknown mergeability). Issues cost one list read per 100 open issues and pull requests
of a repository (at most 10) and one `user` read for the whole run; the list also returns the pull requests, which are dropped.
Search is never used, so `stw status` knows only the repositories of the workspace (`stw issues --owner` discovers others). A run uses far fewer requests than the
hourly limit of a personal token (5000) allows.

## Needs action

Items are ordered by priority, then category, then repository and subject, so the order does not depend on timing.

| Priority | Categories |
| --- | --- |
| red | `repo-error`, `local-operation`, `main-red`, `release-run-failed`, `issue-security` |
| yellow | `read-error`, `local-changes`, `local-detached`, `local-untracked`, `local-behind`, `local-no-upstream`, `main-behind-origin`, `gitlink-drift`, `worktree-dirty`, `pr-conflict`, `pr-blocked`, `tag-without-release`, `issue-assigned`, `issue-bug` |
| green | `pr-ready`, `release-draft` |
| blue | `pr-checks-pending`, `pr-unread`, `local-stash`, `gitlink-ahead`, `branches-unmerged`, `worktree-stale` |
| violet | `pr-bot` |

The suggested command of `branches-unmerged` and `worktree-stale` is `stw tidy`, the whole-workspace [tidy wizard](tidy.md#the-tidy-wizard) (Spec 041 OQ11, accepted 2026-10-25). It takes no path and has effect `read`: the analysis changes nothing until you answer its prompts.

A draft pull request is never an item; a bot pull request is `pr-bot` whatever its group; a tag has exactly one item (a failed
release run wins over "no release"). Release drafts are listed only when the token has write access to the repository,
because GitHub shows drafts to no one else and does not say that it withheld any: otherwise a notice says that drafts are not
visible or unknown, never "0 drafts". The text shows at most ten lines per priority (`N more`), the JSON every item. Each
item has a stable id such as `pr-ready:wstein/sourcesteward:57`, a link (`https` only) and, where one exists, the command to run.

**Issues.** Each repository block has a line `issues: N open (R red, Y yellow)`: N counts every open issue read, the bot issues
and the plain ones included, and the red and yellow ones are the only issues that become items (`issue-security` for a
`security` label, `issue-assigned` for an issue assigned to the authenticated user, `issue-bug` for a `bug` label; the rules and
their precedence are those of [`stw issues`](issues.md), whose `--repo OWNER/NAME` is the drill-down named by a grouped line and, as
`other issues: stw issues --repo OWNER/NAME`, by the `remote:` line when blue or violet issues exist).
A repository with more open items than ten pages of 100 shows `issues: N+ open (R red, Y yellow, truncated)`, one with its issues
disabled `issues: disabled` (a fact: no notice, the run is not degraded), and one that was not read `issues: not read (CODE)`
with a skip reason (`deadline`, `no-token`) or an error code; a failed read is also one `read-error` item that points to
`stw issues --repo OWNER/NAME`. When the token cannot read `user` (a 403 or 404 without rate-limit evidence) the rule "assigned to
you" is off and a notice `no-identity` says so; the status run keeps at most 500 issue rows (the largest cap of `stw issues --limit`),
the counts and `N` stay exact, and only the lowest-priority rows (blue and violet, never items) are dropped first, so an item is
lost only beyond 500 red and yellow issues in the workspace; a failed `user` read is an error (exit 1) with the notice `viewer-unknown`.

## Notices, deadlines and rate limits

What could not be read never hides the rest. Notices name each missing source once, not once per repository. Each request
has a deadline of **15 s**; the remote half as a whole has **90 s** (`--timeout`, 1 s to 10 m). When the deadline passes,
requests not yet sent are dropped, requests in flight are cancelled, the unfinished sources are `skipped` with the reason
`deadline` and a notice says `raise --timeout`. The first rate-limit answer of any source stops every further request. A source
that was not read yet ends `error` `rate-limited`; a CI source (`ci-main` after `repo`, `ci-release` after `tags`) whose input
source failed is not attempted and ends `skipped` `not-attempted`, so its failure is reported once. The command never sleeps and
never retries.

A source ends `ok`, `skipped` (reasons `offline`, `no-token`, `deadline`, `not-found` for a missing release workflow (the default `release.yml` of a pattern no contract declares is a fact about the repository: not applicable, no notice, the run is not degraded, the text line says `no release workflow`; a workflow that a contract declares and the forge does not know is a gap: a notice and a degraded run),
`not-attempted` for a CI source whose input source failed, `issues-disabled` for the issues of a repository that has them
disabled, and the repository reasons `not-github-origin`, `other-host`,
`not-read-locally`, `invalid-repo`) or `error` (`unauthorized`, `forbidden`, `rate-limited`, `network`, `http-5xx`,
`malformed-response`, `page-too-large`, `repo-moved`, `too-many-releases`, `too-many-open-prs`, `contract-invalid`,
`hosting-api-unavailable`). Without a usable token no request is sent at all: the text is exactly the `--offline` output followed by the notice (`run: gh auth
login`), with no Needs action section, no `remote:` lines and no second summary; the JSON keeps the skipped states.

## Exit status

| Exit | When |
| --- | --- |
| 0 | everything attempted succeeded; `--offline`; no usable token (nothing attempted); the deadline cut a read; a release workflow does not exist (404); items never matter |
| 1 | a request that was attempted failed (any `error` source, including `network`, a rate-limit stop and `page-too-large`), a repository cannot be read locally, or the run fails; the output is still printed |
| 2 | usage error (for example `--timeout` with `--offline`, a value out of range) |

The rule depends on what was attempted, never on timing.

## Colour, icons and progress

Colour is used only when stdout is a terminal and `NO_COLOR` is unset or empty; stripping it leaves the same text. `--icons`
chooses the glyphs of the priorities and markers: `unicode` (default on a terminal with a UTF-8 locale), `nerd` (Nerd Font
code points from the Font Awesome range) or `ascii` (default everywhere else: a pipe, a file, a non-UTF-8 locale,
`TERM=dumb`). An explicit `--icons` always wins; the JSON carries no icons (`options.icons` is `null`). Progress is one
rewritten line on **stderr** (`reading remote: N requests`), only when stderr is a terminal, and it is cleared when the run
ends. Apart from it, stderr carries only the one-line `Error: ...` message of an exit status 1 or 2 (a failed request, an
unreadable repository, a usage error); the report itself, notices included, is on stdout. Text from the forge (titles, logins, tags) has control characters escaped.

## JSON

`stw status --json` prints one [schema 2](../schemas/v2/status.schema.json) document (unpublished and byte-frozen until the
feature is accepted): `actions`, `repos` (every v1 member plus `remote`), `sources`, `notices`, `summary` and the run-level
`error`. Every source of a repository is `{state, error, reason, data}` and `data` is `null` unless the state is `ok`; the
`issues` data is `{open, red, yellow, blue, violet, pulls_dropped, truncated}`. `pulls_dropped` is the number of pull
requests that the GitHub issues list returned (the endpoint lists pull requests with the issues) and that `stw` dropped, so
that `open`, the counts and the rows are issues only; it counts only the pages that were read (see `truncated`). It is
information, not an error and not lost data: a repository with open pull requests shows a non-zero value, which is normal
(the pull requests themselves are the `pulls` source). The document-level `sources` list ends with
`issues` and `viewer`. See [JSON contracts](json-contracts.md#status-schema-v2-unpublished).
