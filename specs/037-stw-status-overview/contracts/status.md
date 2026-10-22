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
| `sources` | array | one entry per source id with `{id, state, read, degraded, skipped}`: counts of repositories per outcome (`ok` = `read`) |
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
| `subject` | object | `{kind, number, tag, sha}`: `kind` in `pr`, `release`, `tag`, `branch`, `repository`, `worktree`, `gitlink`, `fact`, `source` (`fact` for the local fact kinds `dirty`, `detached`, `untracked`, `behind`, `diverged`, `no-upstream`, `stash`, `operation`, `conflicts`, `branches`; `source` for `read-error`); the other keys null when not applicable |
| `evidence` | object or null | small fixed facts for the category (for example `{"group":"ready","checks":"green","mergeable_state":"clean"}` for a PR; `{"drafts_visibility":"write-access","assets":4}` for a draft) |
| `command` | str or null | suggested command, a hint that `stw` never runs; validated tokens only; null when no safe command exists (for example `tag-without-release`) |
| `effect` | enum `read`, `owner`, or null | `read` = changes nothing when run; `owner` = changes something and is the owner's operation (`gh pr merge`, `gh release edit`, `git -C ROOT add PATH`); null exactly when `command` is null |
| `hint` | str or null | advice when no command fits (for example: see the repository's `docs/releasing.md`; set the draft variable before the tag is pushed) |

### `repos[].remote`

`{repo, state, reason, sources}`. `repo` `OWNER/NAME` or null; `state` `ok`, `degraded`, `skipped`, `error`; `reason` (only for `skipped`, else null) is one of `not-github-origin`, `other-host`,
`not-read-locally`, `invalid-repo`, `offline`, `no-token`, `deadline`, `not-found`; failures are never skip reasons, they are source errors; `sources` an object with the keys `repo`, `pulls`, `releases`, `tags`, `ci_main`,
`ci_release`, each `{state, error}` with state `ok`, `skipped` (reasons `offline`, `no-token`, `deadline`, `not-found` (a 404 for the release workflow)) and `error` (a failed read) and `error` a code or null (`unauthorized`, `forbidden`,
`rate-limited`, `network`, `http-5xx`, `malformed-response`, `page-too-large`, `repo-moved`, `too-many-releases`,
`too-many-open-prs`; `network` is an error code, never a skip reason). The data of each source:

| Source key | Data (null when the source is not `ok`) |
| --- | --- |
| `repo` | `{default_branch, html_url, drafts_visibility}`; `drafts_visibility` `write-access`, `no-write-access`, `unknown` |
| `pulls` | `{open, ready, checks, conflict, draft, blocked, red, bots, not_read}` (group counts add up to `open`; `red` is the subset of `blocked` with reason `checks-red`) |
| `releases` | `{drafts, truncated}`; `drafts` at most 100 `{name, tag, created_at, assets, url}` (`name` str or null, `created_at` time or null); empty and meaningless unless `drafts_visibility` is `write-access` |
| `tags` | `{latest, unreleased}`; `latest` str or null, `unreleased` at most 5 `{tag, sha, run}` (`run` `{workflow, conclusion, status, url}` or null) |
| `ci_main` | `{branch, head, class, behind_origin}`; `class` `none`, `pending`, `green`, `red` or null; `behind_origin` bool or null (local default branch lacks origin's head) |
| `ci_release` | `{workflow, tags}`: `workflow` the single `ci.release_workflow` (or the default `release.yml`); `tags` an array of `{product, tag, sha, released, runs}` for each product's latest tag and each unreleased tag; `runs` at most 20 `{workflow, conclusion, status, url}`, newest first |

### `summary`

`{repos, attention, errors, priorities: {red, yellow, green, blue, violet}, degraded, failed, notices}`. `repos`, `attention`
and `errors` have their v1 meaning (`attention` is Spec 033's rule, independent of priorities); `priorities` counts all items; `degraded` true when any source is not `ok` (missing token, offline, deadline, failed read); `failed` true when a requested remote read failed (the exit status 1 condition); `notices` the count.

## Exit status

0 in exactly these situations: everything attempted succeeded, `--offline`, no usable token before any request (`no-token`), a deadline skip, a `not-found` skip (404 release workflow); items never matter. 1 on a single condition: an **attempted** request failed (any `error` source, including `network`), plus a repository unreadable locally or a run failure; 2 usage error.
