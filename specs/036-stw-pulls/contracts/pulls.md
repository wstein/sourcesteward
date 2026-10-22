# Contract: `stw pulls --json` (schema v1, unpublished)

The schema file is `schemas/v1/pulls.schema.json` (JSON Schema 2020-12, `$id`
`https://wstein.github.io/sourcesteward/schemas/v1/pulls.schema.json`), created in WP-C, byte-frozen in
`internal/contracts/schema_freeze_test.go` and listed in `UNPUBLISHED_SCHEMAS` of `scripts/package-pages.py` until accepted, like
`status.schema.json` and `watch-event.schema.json`. The output is **one JSON document** (a snapshot, not a stream). Keys are never
omitted; absent values are `null`. `additionalProperties: false` everywhere. Types: `sha` = 40 lowercase hex; `str` = at most 2048 bytes.

## Document

| Key | Type | Meaning |
| --- | --- | --- |
| `schema_version` | integer, const 1 | |
| `command` | const `pulls` | |
| `time` | RFC 3339 UTC | start of the run |
| `host` | str | `github.com` or the Enterprise host |
| `options` | object | `scope` enum(`workspace`, `repos`), `owners` array of str, `no_bots`, `bots_only` bool, `authors` array of str, `detail_drafts` bool, `limit` integer |
| `repos` | array | `{repo, source, open, listed, error}`: `source` enum(`workspace`, `repo`, `config`, `owner`), `open` integer or null (open PRs seen), `listed` integer, `error` code or null |
| `groups` | object | keys `ready`, `checks`, `conflict`, `draft`, `blocked`; each an array of PR rows, newest first, in this fixed key order |
| `summary` | object | `total`, per group counts, `bots` (bot rows), `truncated` (rows beyond `--limit`), `repos_read`, `repos_failed` |
| `errors` | array | `{repo or owner (str), code, message}`; `code` in `rate-limited`, `network`, `unauthorized`, `forbidden`, `http-5xx`, `malformed-response`, `too-many-open-prs`, `page-too-large`, `repo-moved`, `search-incomplete`, `search-too-many`; `message` sanitized, at most 512 bytes |

## PR row

`repo` (str), `number` (integer), `title` (str), `url` (str, `https` only), `author` (str), `bot` (bool), `draft` (bool),
`created_at` (time), `age_days` (integer), `base` (str), `head_ref` (str), `head` (sha), `fork` (bool), `labels` (array of at most 20 str),
`checks` (enum `none|pending|green|red`, or null for a draft without detail), `failing` (array of at most 20 str, red only, else empty),
`mergeable` (bool or null), `mergeable_state` (str or null), `reviewers` (array of at most 20 str), `group` (the key it is listed
under), `reason` (null unless `group` is `blocked`: `checks-red`, `no-checks`, `review-or-protection`, `unstable`, `unknown`),
`ready` (bool, true exactly when `group` is `ready`), `detail` (bool: false for a row beyond `--limit` or a draft without detail,
whose `checks`, `mergeable` and `mergeable_state` are then null and whose group is `draft` or `blocked` with reason `unknown`).

## Example

```json
{"schema_version":1,"command":"pulls","time":"2026-10-22T09:00:00Z","host":"github.com","options":{"scope":"workspace","owners":[],"no_bots":false,"bots_only":false,"authors":[],"detail_drafts":false,"limit":200},"repos":[{"repo":"wstein/sourcesteward","source":"workspace","open":2,"listed":2,"error":null}],"groups":{"ready":[{"repo":"wstein/sourcesteward","number":57,"title":"build(deps): bump cobra","url":"https://github.com/wstein/sourcesteward/pull/57","author":"dependabot[bot]","bot":true,"draft":false,"created_at":"2026-10-19T04:12:00Z","age_days":3,"base":"main","head_ref":"dependabot/go_modules/cobra","head":"3f2a9c1d0b7e4a5f6c8d9e0a1b2c3d4e5f607182","fork":false,"labels":["dependencies"],"checks":"green","failing":[],"mergeable":true,"mergeable_state":"clean","reviewers":[],"group":"ready","reason":null,"ready":true,"detail":true}],"checks":[],"conflict":[],"draft":[],"blocked":[]},"summary":{"total":1,"ready":1,"checks":0,"conflict":0,"draft":0,"blocked":0,"bots":1,"truncated":0,"repos_read":1,"repos_failed":0},"errors":[]}
```

## Human output (`--human`)

One section per non-empty group (`ready`, `checks`, `conflict`, `draft`, `blocked`), a heading `GROUP (N)`, then one line per PR:
`REPO#N  AGE  AUTHOR[bot]  CHECKS  MERGEABLE  [reviewers: a, b]  TITLE`, e.g.
`wstein/sourcesteward#57  3d  dependabot[bot]  green  clean  build(deps): bump cobra`; blocked rows append `(reason)`. A final line
`N open pull requests in M repositories, K ready` and, on failures, `R repositories could not be read` (also on stderr per
repository). Control characters in every untrusted field are escaped, text is bounded to 120 characters, no colour unless a terminal
and `NO_COLOR` unset. The token is never printed.
