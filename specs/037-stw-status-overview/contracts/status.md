# Contract: `stw status --json` (schema 2, unpublished)

Two documents, never mixed:

- `stw status --offline --json` prints the **schema 1** document of [`schemas/v1/status.schema.json`](../../../schemas/v1/status.schema.json),
  byte-identical to today (its frozen digest does not change).
- `stw status --json` (online, the default) prints one **schema 2** document, the consumer contract (also for the owner's later web
  UI, which is out of scope). File `schemas/v2/status.schema.json` (JSON Schema 2020-12, `$id`
  `https://wstein.github.io/sourcesteward/schemas/v2/status.schema.json`), created in WP-F, byte-frozen in
  `internal/contracts/schema_freeze_test.go` (digest registered in the same reviewed commit) and listed in `UNPUBLISHED_SCHEMAS` of
  `scripts/package-pages.py` until accepted. A later change that only adds optional keys keeps `schema_version` 2; renaming, removing
  or retyping a key needs version 3. Keys are never omitted; absent values are `null`; `additionalProperties: false` everywhere
  except where noted. `str` = at most 2048 bytes cut on a UTF-8 boundary; `sha` = 40 lowercase hex.

## Document

| Key | Type | Meaning |
| --- | --- | --- |
| `schema_version` | const 2 | |
| `command` | const `status` | |
| `time` | RFC 3339 UTC | start of the run |
| `host` | str | `github.com` |
| `options` | object | `fetch`, `worktrees` booleans, `icons` null (JSON carries no icons) |
| `root` | str or null | workspace root, as v1 |
| `note` | str or null | as v1 |
| `actions` | array | the action items, ordered as in the spec (priority, category, repo, subject), every item, no cap |
| `repos` | array | one object per repository: **all members of the v1 repository object unchanged** (`name`, `path`, `kind`, `state`, `head`, `branch`, ... `attention`, `reasons`, `error`) plus `remote` (below) |
| `sources` | array | exactly nine entries in the order `local`, `repo`, `pulls`, `releases`, `tags`, `ci-main`, `ci-release`, `issues`, `viewer`, each `{id, state, read, degraded, skipped}`: counts of repositories per outcome (`ok` = `read`, `error` = `degraded`); `state` is `ok` (everything read), `skipped` (nothing read, nothing failed), `error` (everything failed) or `degraded` |
| `notices` | array | `{code, source, repos, message}`: one per missing source, `message` sanitized, at most 512 bytes; `repos` array of repository names affected (at most 256) |
| `summary` | object | see below |
| `error` | object or null | run-level local failure only, as v1 (`code`, `message`) |

### Action item

| Key | Type | Meaning |
| --- | --- | --- |
| `id` | str | `CATEGORY:REPO:SUBJECT`, stable for the same finding across runs (`pr-ready:wstein/sourcesteward:57`); `REPO` is the repository's `OWNER/NAME` or, for a local-only finding, its `name`; `SUBJECT` a number, tag, branch, or `-` |
| `priority` | enum `red`, `yellow`, `green`, `blue`, `violet` | |
| `category` | enum, the 25 category ids of the spec table | |
| `repo` | str | as in `id` |
| `path` | str or null | workspace-relative path of the repository when local |
| `title` | str | one-line text, the same words as the human item line |
| `url` | str or null | deep link, `https` only (PR, release, actions page, repository) |
| `subject` | object | `{kind, number, tag, sha, name}` (`name` is the id part that is neither a number nor a tag: the fact kind, the branch, the path of a gitlink or worktree, the source id; null for `pr`, `release`, `tag`, `repository`): `kind` in `pr`, `release`, `tag`, `branch`, `repository`, `worktree`, `gitlink`, `fact`, `source` (`fact` for the local fact kinds `dirty`, `detached`, `untracked`, `behind`, `diverged`, `no-upstream`, `stash`, `operation`, `conflicts`, `branches`; `source` for `read-error`); the other keys null when not applicable |
| `evidence` | object or null | small fixed facts for the category (for example `{"group":"ready","checks":"green","mergeable_state":"clean"}` for a PR; `{"drafts_visibility":"write-access","assets":4}` for a draft) |
| `command` | str or null | suggested command, a hint that `stw` never runs; validated tokens only; null when no safe command exists (for example `tag-without-release`) |
| `effect` | enum `read`, `owner`, or null | `read` = changes nothing when run; `owner` = changes something and is the owner's operation (`gh pr merge`, `gh release edit`, `git -C ROOT add PATH`); null exactly when `command` is null |
| `hint` | str or null | advice when no command fits (for example: see the repository's `docs/releasing.md`; set the draft variable before the tag is pushed) |

### `repos[].remote`

`{repo, state, reason, sources}`. `repo` `OWNER/NAME` or null; `state` `ok`, `degraded`, `skipped`, `error`; `reason` (only for `skipped`, else null) is one of `not-github-origin`, `other-host`,
`not-read-locally`, `invalid-repo`, `offline`, `no-token`, `deadline`, `not-found`, `not-attempted`; failures are never skip reasons, they are source errors; `sources` an object with the keys `repo`, `pulls`, `releases`, `tags`, `ci_main`,
`ci_release`, each `{state, error, reason, data}` with state `ok`, `skipped` (with a `reason`: `offline`, `no-token`, `deadline`, `not-found` (a 404 for the release workflow), `not-attempted` (a CI source whose input source failed) or one of the repository reasons above) and `error` (a failed read, with a code); `reason` is null unless skipped, `error` null unless failed, `data` null unless ok; the error code is one of (`unauthorized`, `forbidden`,
`rate-limited`, `network`, `http-5xx`, `malformed-response`, `page-too-large`, `repo-moved`, `too-many-releases` (also the tags listing past page 10),
`too-many-open-prs`, `contract-invalid` (`ci_release`: a release contract exists but is not usable), `hosting-api-unavailable` (the reader could not be opened for another reason than a missing token, for example no `gh`); `network` is an error code, never a skip reason). The data of each source:

| Source key | Data (null when the source is not `ok`) |
| --- | --- |
| `repo` | `{default_branch, html_url, drafts_visibility}`; `drafts_visibility` `write-access`, `no-write-access`, `unknown` |
| `pulls` | `{open, ready, checks, conflict, draft, blocked, red, bots, not_read}` (group counts add up to `open`; `red` is the subset of `blocked` with reason `checks-red`) |
| `releases` | `{drafts, truncated}`; `drafts` at most 100 `{name, tag, created_at, assets, url}` (`name` str or null, `created_at` time or null); empty and meaningless unless `drafts_visibility` is `write-access` |
| `tags` | `{latest, unreleased}`; one entry per (declared release tag pattern, tag), in the order of the patterns and newest first within one: `latest` an array of `{pattern, tag}` (the highest-version tag of each pattern that has one, whether released or not; empty when none), `unreleased` at most 5 `{pattern, tag, sha, run}` (`run` `{workflow, conclusion, status, url}` or null), filled pattern by pattern in the declared order. A tag that is both the latest of a pattern and unreleased is in both arrays; a tag that two patterns match has an entry for each, and each unreleased entry counts towards the 5; the latest entries are not capped |
| `ci_main` | `{branch, head, class, behind_origin}`; `class` `none`, `pending`, `green`, `red` or null; `behind_origin` bool or null (local default branch lacks origin's head) |
| `ci_release` | `{tags}`: an array of `{pattern, product, tag, sha, latest, unreleased, workflow, runs}`, one entry per tag entry of the `tags` source (the latest of every pattern and at most 5 unreleased); `workflow` the `ci.release_workflow` file name of the product that declares the pattern, or `release.yml`; `runs` at most 20 `{workflow, conclusion, status, url}`, newest first |

### `summary`

`{repos, attention, errors, priorities: {red, yellow, green, blue, violet}, degraded, failed, notices}`. `repos`, `attention`
and `errors` have their v1 meaning (`attention` is Spec 033's rule, independent of priorities); `priorities` counts all items; `degraded` true when any source is not `ok` (missing token, offline, deadline, failed read), except a skipped source that is a fact of the repository (`issues-disabled`, and `not-found` of `ci-release` when only the default `release.yml` of undeclared patterns is missing: the repository has no release workflow); a 404 for a workflow that a contract declares stays a gap and degrades; `failed` true when a requested remote read failed (the exit status 1 condition); `notices` the count.

## Exit status

0 in exactly these situations: everything attempted succeeded, `--offline`, no usable token before any request (`no-token`), a deadline skip, a `not-found` skip (404 release workflow; it never fails the run); items never matter. 1 on a single condition: an **attempted** request failed (any `error` source, including `network`), plus a repository unreadable locally or a run failure; 2 usage error.

## Amendments recorded with the implementation

Decisions taken while implementing the work packages; they refine the text above and the schema follows them.

- **Per-source reason.** Every source of a repository is `{state, error, reason, data}`, so a deadline, a missing workflow
  (`not-found`) and a source that was never attempted (`not-attempted`) are told apart per source and not only when the whole
  repository was skipped. `remote.state` is `ok` (all six sources ok), `skipped` (all skipped, with the reason of the first),
  `error` (all failed) or `degraded`.
- **Missing release workflow.** Only the default `release.yml` of a pattern that no contract declares is optional: when the
  forge answers 404 for it, that entry is left out and the repository has no release workflow for it. If no entry has one, `ci_release`
  stays `skipped` with the reason `not-found` (the schema vocabulary is unchanged) and is *not applicable*: the run is not degraded,
  no notice is raised, the per-source counts of `sources[]` and the repository `remote.state` count it as read, as they do
  `issues-disabled`, and the text `remote:` line says `no release workflow`. If some entries have the default workflow the
  source is `ok`. A 404 for a workflow that a contract declares (`ci.release_workflow`) keeps its meaning: `ci_release` is
  `skipped`/`not-found`, degraded, with a notice. The runs already read are kept in every case and still yield
  `release-run-failed` items; the read stops only at a real failure (an `error` source). A real failure to read the runs
  is an `error` source and degrades.
- **Dependent sources.** `ci-main` needs the default branch of the `repo` source and `ci-release` the tags of the `tags` source.
  When that source failed, the CI source is not attempted and is `skipped` with `not-attempted` (its failure is reported once, as a
  notice of the source that failed); when it was skipped, the CI source carries the same skip. An unusable release contract is
  still `contract-invalid`.
- **One pool.** All sources but the pull requests share one pool of four repositories; one worker reads `repo`, `releases`,
  `tags`, `ci-main` and `ci-release` of a repository in this order.
- **No usable token.** The text output is exactly the `--offline` output followed by the notices, with no Needs action section,
  no `remote:` lines and no second summary. The JSON keeps the skipped states of the schema.
- **Evidence keys by category** (all other categories have `evidence: null`): the `pr-*` categories `group` and, when set,
  `reason` and `past_budget`; `release-draft` `drafts_visibility` (`write-access`) and `assets`; `release-run-failed`
  `conclusion` and `released`; `main-red` `class`; `read-error` `source` and `code`; `repo-error` `code` (or null);
  `local-changes` `staged` and `unstaged`; `local-operation` `operation` or `conflicts`; `local-behind` `behind` and, when
  diverged, `ahead`; `local-untracked` `untracked`; `local-stash` `stash`; `branches-unmerged` `not_integrated`;
  `gitlink-drift` and `gitlink-ahead` `relation`.
- **Notices** use the singular for a count of one (`1 repository`, `1 source`).
- **Issues.** The issue sources of [Spec 038](../../038-stw-issues/spec.md) (`issues` per repository, `issues` and `viewer` among
  the document-level `sources`, the categories 26 to 28, `subject.kind` `issue`, the notices `no-identity` and `viewer-unknown`)
  are additions to schema 2 while it is unpublished; the schema and its frozen digest carry them.
