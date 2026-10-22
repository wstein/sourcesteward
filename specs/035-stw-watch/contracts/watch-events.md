# Contract: `stw watch` events (schema v1, unpublished)

The schema file is `schemas/v1/watch-event.schema.json` (JSON Schema 2020-12, `$id`
`https://wstein.github.io/sourcesteward/schemas/v1/watch-event.schema.json`). It is created in WP-E, byte-frozen in
`internal/contracts/schema_freeze_test.go` and listed in `UNPUBLISHED_SCHEMAS` of `scripts/package-pages.py` (and its test), exactly
like `sync.schema.json` and `status.schema.json` (Specs 032, 033), until the feature is accepted. Each line of stdout (without
`--human`) and **every** line of `journal.jsonl` validates against it (the journal holds events only; there are no tombstone lines). Keys are never omitted; absent values are `null`.

## Envelope

| Key | Type | Meaning |
| --- | --- | --- |
| `schema_version` | integer, const 1 | |
| `seq` | integer >= 1 | gapless per state directory, derived from the journal tail |
| `id` | string | event id `repo#pr:kind:key` (spec: "Event ids" gives the key of every kind; `-` replaces an absent repo or PR); consumers dedupe on it |
| `time` | string, RFC 3339 UTC | observation time |
| `repo` | string `OWNER/NAME` or null | null only for `repos-changed` and run-level `watch-error` |
| `pr` | integer or null | null for repository-level kinds |
| `kind` | enum | `new-pr`, `head-changed`, `checks-green`, `checks-red`, `checks-pending-timeout`, `mergeable-changed`, `ready-state-changed`, `base-changed`, `merged`, `closed`, `restack-needed`, `repos-changed`, `watch-error`, `watch-recovered`, `watch-stalled` |
| `details` | object | per kind, `additionalProperties: false`, selected by `kind` through `allOf` / `if` / `then` |

The `kind` enum is closed in v1; a later addition is a `schema_version` 2 decision except the reserved `review-state-changed`,
which may be added to v1 before acceptance. Consumers ignore unknown kinds.

## `details` by kind

Types: `sha` = 40 lowercase hex; `str` = string, at most 2048 bytes; `names` = array of at most 20 strings.

- `new-pr`: `title` str, `author` str, `draft` bool, `base` str, `head_ref` str, `head` sha.
- `head-changed`: `old` sha, `new` sha.
- `checks-green`: `head` sha, `draft` bool, `base` str, `total` integer.
- `checks-red`: `head` sha, `failing` names, `failing_total` integer, `shape` enum(`stack-merge-shape`, `real`, `unknown`), `base` str, `draft` bool.
- `checks-pending-timeout`: `head` sha, `pending` names, `since` time.
- `mergeable-changed`: `mergeable` bool or null, `state` str, `conflicting` bool, `behind` bool, `base` str.
- `ready-state-changed`: `from`, `to`: enum(`draft`, `ready`), `from` != `to`.
- `base-changed`: `old` str, `new` str.
- `merged`: `merge_commit` sha or null, `base` str, `head` sha, `head_ref` str, `merged_at` time, `method_hint` enum(`merge`, `rebase-or-squash`, `unknown`).
- `closed`: `head` sha, `base` str.
- `restack-needed`: `base` str, `reason` enum(`child-of-merged-branch`, `contains-merged-commits`), `old_parent` sha, `new_parent` sha or null, `new_parent_source` const `merge_commit_sha`, `branch` str, `argv` array of str or null (null when `new_parent` is null or the head repository is a fork). `merged_pr` integer is part of `details` (the PR whose merge caused it).
- `repos-changed`: `added` array of str, `removed` array of str, `skipped` array of `{path, reason}`.
- `watch-error`: `code` str (`rate-limited`, `network`, `unauthorized`, `forbidden`, `http-5xx`, `malformed-response`, `too-many-open-prs`, `page-too-large`, `repo-moved`, `hosting-api-unavailable`, `state-error`), `message` str (sanitized, at most 512 bytes), `failures` integer, `retry_in_seconds` integer, `repo` str or null.
- `watch-error` mapping notes (hosting layer): a `404` is reported as `forbidden` (GitHub does not tell a missing from an inaccessible resource; the HTTP status stays in the error). A base branch whose name contains `%` is refused before any request (`malformed-response`), so that PR cannot be watched. A `304` on the last list page of exactly `per_page` items makes the watcher also request the next page once, because a `304` has no `Link`. The halved page size (12) is process memory only: WP-A `state.json` must store the page ETags and parsed pages but must not persist or restore the page-size choice, and a restart starts at 25.
- `watch-recovered`: `after_failures` integer, `repo` str or null.
- `watch-stalled`: `since` time, `threshold_seconds` integer, `repo` str.

## Examples

```json
{"schema_version":1,"seq":1,"id":"wstein/sourcesteward#41:new-pr:g0","time":"2026-10-22T09:00:07Z","repo":"wstein/sourcesteward","pr":41,"kind":"new-pr","details":{"title":"docs(035): stw watch","author":"wstein","draft":true,"base":"main","head_ref":"docs/035-stw-watch","head":"3f2a9c1d0b7e4a5f6c8d9e0a1b2c3d4e5f607182"}}
{"schema_version":1,"seq":2,"id":"wstein/sourcesteward#41:checks-red:3f2a9c1d0b7e4a5f6c8d9e0a1b2c3d4e5f607182:c1","time":"2026-10-22T09:04:10Z","repo":"wstein/sourcesteward","pr":41,"kind":"checks-red","details":{"head":"3f2a9c1d0b7e4a5f6c8d9e0a1b2c3d4e5f607182","failing":["docs-check"],"failing_total":1,"shape":"real","base":"main","draft":true}}
{"schema_version":1,"seq":3,"id":"wstein/sourcesteward#41:merged:9d8c7b6a5f4e3d2c1b0a99887766554433221100","time":"2026-10-22T10:30:00Z","repo":"wstein/sourcesteward","pr":41,"kind":"merged","details":{"merge_commit":"9d8c7b6a5f4e3d2c1b0a99887766554433221100","base":"main","head":"3f2a9c1d0b7e4a5f6c8d9e0a1b2c3d4e5f607182","head_ref":"docs/035-stw-watch","merged_at":"2026-10-22T10:29:58Z","method_hint":"rebase-or-squash"}}
{"schema_version":1,"seq":4,"id":"wstein/sourcesteward#42:restack-needed:41:9d8c7b6a5f4e3d2c1b0a99887766554433221100","time":"2026-10-22T10:30:03Z","repo":"wstein/sourcesteward","pr":42,"kind":"restack-needed","details":{"merged_pr":41,"base":"main","reason":"contains-merged-commits","old_parent":"3f2a9c1d0b7e4a5f6c8d9e0a1b2c3d4e5f607182","new_parent":"9d8c7b6a5f4e3d2c1b0a99887766554433221100","new_parent_source":"merge_commit_sha","branch":"feat/035-impl","argv":["git","-c","rebase.updateRefs=false","-c","rerere.enabled=false","rebase","--onto","9d8c7b6a5f4e3d2c1b0a99887766554433221100","3f2a9c1d0b7e4a5f6c8d9e0a1b2c3d4e5f607182","feat/035-impl"]}}
{"schema_version":1,"seq":5,"id":"-:watch-error:rate-limited:e1:f1","time":"2026-10-22T10:31:00Z","repo":null,"pr":null,"kind":"watch-error","details":{"code":"rate-limited","message":"core rate limit exhausted, resets 10:58:00Z","failures":1,"retry_in_seconds":1620,"repo":null}}
```

## Human line (`--human`)

`TIME REPO#PR KIND summary`, one line, control characters escaped, no colour. Examples:
`10:04:10 wstein/sourcesteward#41 checks-red docs-check (real)`,
`10:30:03 wstein/sourcesteward#42 restack-needed after #41: git rebase --onto 9d8c7b6 3f2a9c1 feat/035-impl` (the human line shows
7-character SHAs; JSON always has 40).

## `--status --json`

One object: `schema_version` 1, `state_dir`, `heartbeat` (`time`, `age_seconds`, `pid`) or null, `fresh` bool, `interval_seconds`,
`seq`, `lock_held` bool, `repos` (array of `repo`, `last_ok` or null, `age_seconds`, `failures`, `open_error` code or null). It is
described in the schema file as a second top-level definition `status`; the event schema's root is the envelope.
