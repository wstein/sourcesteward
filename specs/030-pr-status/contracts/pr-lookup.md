# PR lookup by head SHA (slice 1)

## Command form and why

`stw hosting prs SHA [PATH]` and `gh jj prs SHA [PATH]`, with the existing selectors `--repo`, `--host`, `--remote` and `--json`. SHA is exactly 40 lowercase hexadecimal digits (the existing hosting full-SHA grammar); anything else, including SHA-256 OIDs and jj IDs, is a usage error (exit 2) before native, authentication or network work.

Decision and justification: the existing hosting leaves are `KIND ARG [PATH]` (`pr NUMBER`, `ci NUMBER`) with `Kind` in `hosting.Options`, and `NUMBER` is a required positional validated in the argument hook. A `--by-head SHA` flag on `pr` would make the required NUMBER optional and switch the result between a single PR and a collection, so one command would have two result shapes and an arity that depends on a flag. A new sibling leaf `prs SHA` keeps one shape per command, lets `pr` stay exactly as specified, matches the positional convention (the SHA takes NUMBER's place) and reuses selection, authentication, native resolution and exit rules unchanged. The plural name signals a collection result. Rejected alternatives: `pr --by-head` (arity/shape switch), `ci --find` (CI is not a PR lookup).

JSON is always schema version 2 of kind `prs`; `--json-version` is not accepted on `prs` (there is no v1 form). Human output is the detailed line list and `--compact` (one escaped line per PR) as for `pr`.

## Request and bounds

Exactly one `GET repos/OWNER/NAME/commits/SHA/pulls?per_page=100&page=1`, added to the Spec 022 transport allowlist as the single new route (no `page` other than 1, no other `per_page`, no other `commits/SHA/...` suffix beyond the existing `check-runs` and `statuses`). GET only, same auth, redirect, compression, 1 MiB and 30 second bounds as `pr`. No pagination, no per-PR follow-up request, no retry. HTTP failures follow the [network policy](../spec.md#network-policy-and-rate-limits): 401/403/404/429, redirects, oversize or malformed bodies stop at once, exit 1, safe message, no `Retry-After` sleep.

## Interpretation

The route lists pull requests GitHub associates with the commit: PRs whose branch contains it and, after a merge, the PR whose merge or squash/rebase result is that commit. It is not an exact head-equals-SHA match, so each entry carries `head_match` (the PR's reported head SHA equals the queried SHA). The caller decides what to do with `head_match:false` entries; the command never filters, ranks or selects. The list objects do not carry the `merged` boolean of the single-PR endpoint, so slice 1 derives it as `merged_at != null` and then applies the same coherence table as `pr` (open requires `merged_at` and `closed_at` null; closed without `merged_at` is `closed-unmerged`; closed with `merged_at` is `merged`; any other combination is an evidence error for the whole result, never a guess). `mergeable`, review and check data are not read.

## Result

Envelope as the `pr` v2 result (`schema_version` 2, `kind` `prs`, `status`, `backend`, `identity`, `repository`, `candidates`, `diagnostics`, `observed_at`, `boundary` with `max_requests` 1), without `pull_request`/`observation`, plus:

| Field | Meaning |
| --- | --- |
| `query.head_sha` | the requested SHA |
| `pull_requests` | array, order as GitHub returned it, at most 100; each entry has `number`, `state`, `draft`, `base_ref`, `base_repository`, `base_sha`, `head_ref`, `head_repository` (nullable), `head_sha`, `head_match`, `merged`, `merged_at`, `closed_at`, `merge_commit_sha` (raw), `result_sha`, `lifecycle`, `url`, `title` |
| `count` | length of `pull_requests` |
| `truncated` | true when exactly 100 entries were returned: more may exist, none was requested |

Zero entries is a successful observation (`count:0`), not an error and not proof that no PR exists beyond what GitHub associates with the commit. Many entries is success; the command never picks one. A truncated list adds the limitation `lookup-single-page`. Titles, refs and URLs are display data, escaped in human output like `pr`. No reviewed, approved, merge-eligible or integrated verdict is produced.

## Conformance

Fixtures (no real network): zero, one open, one merged per merge method, many with and without `head_match`, exactly 100 (truncated), 99, missing/non-string state, `merged_at`/state incoherence, invalid SHA/date, null head repository, each HTTP outcome, redirect, oversize body. Request assertion: exactly one request, method GET, the exact allowlisted path and query. Allowlist test: other `commits/SHA/...` suffixes, other pages and non-GET methods are refused.
