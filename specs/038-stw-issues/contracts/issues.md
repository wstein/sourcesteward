# Contract: `stw issues --json` (schema v1, unpublished) and the `stw status` additions

The schema file is `schemas/v1/issues.schema.json` (JSON Schema 2020-12, `$id`
`https://wstein.github.io/sourcesteward/schemas/v1/issues.schema.json`), created in WP-C, byte-frozen in
`internal/contracts/schema_freeze_test.go` and listed in `UNPUBLISHED_SCHEMAS` of `scripts/package-pages.py` until accepted, like
`pulls.schema.json`. The output is **one JSON document** (a snapshot). Keys are never omitted; absent values are `null`.
`additionalProperties: false` everywhere. `str` = at most 2048 bytes, cut on a UTF-8 boundary.

## Document

| Key | Type | Meaning |
| --- | --- | --- |
| `schema_version` | integer, const 1 | |
| `command` | const `issues` | |
| `time` | RFC 3339 UTC | start of the run |
| `host` | str | `github.com` or the Enterprise host |
| `viewer` | object | `{state, login, error}`: `state` `ok`, `skipped` (reason `viewer-unavailable`, a 403) or `error`; `login` str or null (null unless `ok`); `error` a code or null |
| `options` | object | `scope` enum(`workspace`, `repos`, `mixed`) as in the pulls contract, `owners` array of str, `labels`, `assignees`, `authors` arrays of str (as given), `limit` integer |
| `repos` | array | `{repo, source, state, reason, open, pulls_dropped, error}`: `source` enum(`workspace`, `repo`, `owner`); `state` `ok`, `partial`, `skipped`, `error`; `reason` null or `issues-disabled` (only with `skipped`); `open` integer or null (issues read, before filters); `pulls_dropped` integer or null; `error` a code or null |
| `issues` | object | keys `red`, `yellow`, `blue`, `violet`, each an array of issue rows in this fixed key order, sorted by repository (case-insensitive) then number, **after filters and the `--limit` cap** |
| `summary` | object | `total` (rows listed), `red`, `yellow`, `blue`, `violet` (listed rows per priority), `omitted` (rows beyond `--limit`), `counts` object `{red, yellow, blue, violet}` for **everything read, filters applied, cap not** (what `stw status` shows), `bots` (bot rows listed), `repos_read`, `repos_failed` |
| `errors` | array | `{repo, owner, code, message}`: both keys always present and exactly one non-null (`repo` for a repository, `owner` for an owner discovery); a run-level failure (`viewer-unknown` for `--assignee @me`, `viewer` read failure) uses `repo` null and `owner` null with `code` set; `code` in `rate-limited`, `network`, `unauthorized`, `forbidden`, `http-5xx`, `malformed-response`, `too-many-open-issues`, `page-too-large`, `repo-moved`, `search-incomplete`, `search-too-many`, `viewer-unknown`; `message` sanitized, at most 512 bytes |

## Issue row

`repo` (str), `number` (integer, at least 1), `title` (str), `url` (str, `https` only, or null), `author` (str, `ghost` when the user was
deleted), `bot` (bool), `created_at` (time or null), `age_days` (integer, null exactly when `created_at` is null), `labels` (array of at
most 20 str, API order), `labels_total` (integer, all labels returned, so a truncation is visible), `assignees` (array of at most 20 str),
`comments` (integer or null), `priority` (the key it is listed under), `reason` (`security-label`, `assigned-to-you`, `bot`,
`bug-label`, `none`; the ordered rule table is in the spec), `command` (str or null: `gh issue view N -R OWNER/NAME` for `red` and
`yellow`, else null), `effect` (`read` exactly when `command` is not null, else null). The reason is computed over all labels before
the 20-label cut.

## Example

```json
{"schema_version":1,"command":"issues","time":"2026-10-22T09:00:00Z","host":"github.com","viewer":{"state":"ok","login":"wstein","error":null},"options":{"scope":"workspace","owners":[],"labels":[],"assignees":[],"authors":[],"limit":200},"repos":[{"repo":"wstein/sourcesteward","source":"workspace","state":"ok","reason":null,"open":2,"pulls_dropped":1,"error":null}],"issues":{"red":[{"repo":"wstein/sourcesteward","number":41,"title":"token leaks into an error string","url":"https://github.com/wstein/sourcesteward/issues/41","author":"alice","bot":false,"created_at":"2026-10-20T08:00:00Z","age_days":2,"labels":["security","bug"],"labels_total":2,"assignees":[],"comments":0,"priority":"red","reason":"security-label","command":"gh issue view 41 -R wstein/sourcesteward","effect":"read"}],"yellow":[],"blue":[{"repo":"wstein/sourcesteward","number":12,"title":"docs typo","url":"https://github.com/wstein/sourcesteward/issues/12","author":"bob","bot":false,"created_at":"2026-09-01T08:00:00Z","age_days":51,"labels":[],"labels_total":0,"assignees":[],"comments":3,"priority":"blue","reason":"none","command":null,"effect":null}],"violet":[]},"summary":{"total":2,"red":1,"yellow":0,"blue":1,"violet":0,"omitted":0,"counts":{"red":1,"yellow":0,"blue":1,"violet":0},"bots":0,"repos_read":1,"repos_failed":0},"errors":[]}
```

## Additions to `stw status --json` (schema 2 of Spec 037, WP-G)

Additive; the Spec 037 frozen-schema process applies (digest updated in the same reviewed commit; see the open question on versioning).

- `actions[].category` enum gains `issue-security`, `issue-assigned`, `issue-bug` (categories 26 to 28); `actions[].subject.kind` gains
  `issue` (`number` set, other keys null). Evidence: `{"reason":"security-label","labels":[...],"assignees":[...],"author":"alice"}`
  (at most 20 labels and assignees). `command` is `gh issue view N -R OWNER/NAME`, `effect` `read`.
- `repos[].remote.sources` gains the key `issues`: `{state, error}` as the other sources (skip reasons `deadline`, `no-token`,
  `offline`, `issues-disabled`), data `{open, red, yellow, blue, violet, pulls_dropped, truncated}` (null when not `ok`; `open` equals
  the sum of the four priority counts; `truncated` true for `too-many-open-issues`).
- Document-level `sources` gains two entries, `issues` and `viewer` (`{id, state, read, degraded, skipped}` counts as for the others;
  `viewer` has at most one repository-independent attempt). `notices[].code` gains `viewer-unknown` and `viewer-unavailable`.
- `summary.priorities` is unchanged in shape; issue items count in it like any item. Exit status: Spec 037 unchanged (a `viewer`
  read that failed with an attempted request is an error, exit 1; a 403 is a skip, exit 0).
