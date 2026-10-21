# PR lookup by head SHA (slice 1)

## Command form and why

`stw hosting prs SHA [PATH]` and `gh jj prs SHA [PATH]`, with the existing selectors `--repo`, `--host`, `--remote` and `--json`. SHA is exactly 40 lowercase hexadecimal digits (the existing hosting full-SHA grammar); anything else, including SHA-256 OIDs and jj IDs, is a usage error (exit 2) before native, authentication or network work.

Decision and justification: the existing hosting leaves are `KIND ARG [PATH]` (`pr NUMBER`, `ci NUMBER`) with `Kind` in `hosting.Options`, and `NUMBER` is a required positional validated in the argument hook. A `--by-head SHA` flag on `pr` would make the required NUMBER optional and switch the result between a single PR and a collection, so one command would have two result shapes and an arity that depends on a flag. A new sibling leaf `prs SHA` keeps one shape per command, lets `pr` stay exactly as specified, matches the positional convention (the SHA takes NUMBER's place) and reuses selection, authentication, native resolution and exit rules unchanged. The plural name signals a collection result. Rejected alternatives: `pr --by-head` (arity/shape switch), `ci --find` (CI is not a PR lookup).

JSON is always schema version 2 of kind `prs`; `--json-version` is not accepted on `prs` (there is no v1 form). Human output is the detailed line list and `--compact` (one escaped line per PR) as for `pr`.

## Request and bounds

Exactly one `GET repos/OWNER/NAME/commits/SHA/pulls?per_page=30&page=1`, added to the Spec 022 transport allowlist as the single new route (no other `page`, no other `per_page`, no other `commits/SHA/...` suffix beyond the existing `check-runs` and `statuses`). GET only, same auth, redirect, compression, 1 MiB and 30 second bounds as `pr`. No pagination, no per-PR follow-up request, no retry. HTTP failures follow the [network policy](../spec.md#network-policy-and-rate-limits).

Size decision (body bound versus page size): each list item embeds full head and base repository objects, which this design estimates (not measured; no live call has been made) at up to roughly 20 KB per item. 100 items could then exceed the 1 MiB raw-body cap and fail as oversize before `truncated` could ever be reported, so the page size is 30: 30 x ~20 KB = ~600 KB leaves headroom under 1 MiB (1,048,576 bytes). The decoder reads the whole body within the existing cap and keeps only the fields in the result table. If a body is nevertheless oversize, the existing body limit stops the call: `status:error`, diagnostic `hosting-pr-failed` with the existing safe message, `pull_requests:[]`, `count:0`, no partial list, exit 1. The truncation flag therefore means exactly 30 entries came back. A task (T013) proves both sides with fixtures: a realistic 30-item body of ~20 KB items decodes under the cap, and a body over 1 MiB yields the no-partial error. The owner-authorised first live call must check the real item size; if it is larger than assumed, lower `per_page` in a spec amendment, never raise the cap.

## Interpretation

The route is understood to return the pull requests GitHub associates with the commit. That is a lookup of associated PRs, not "whose branch contains the SHA": for a commit on the default branch the result may be only the merged PR that introduced it, and GitHub's association rules are not asserted here. This design could not verify the API's behaviour offline; slice-1 tests use fixtures only and prove decoding and bounds, not GitHub's semantics. The preview media type once needed by this endpoint is not believed necessary and the transport sends no preview header; the owner-authorised first live call must confirm both. Because association is not head equality, each entry carries `head_match`, never filtered, ranked or selected. The list objects do not carry the `merged` boolean of the single-PR endpoint, so slice 1 derives it as `merged_at != null` and then applies the same coherence table as `pr` (open requires `merged_at` and `closed_at` null; closed without `merged_at` is `closed-unmerged`; closed with `merged_at` is `merged`; any other combination is an evidence error for the whole result, never a guess). `mergeable`, review and check data are not read.

`head_match` compares the PR's current head SHA at observation time with the queried SHA. A PR whose branch was force-pushed away from the SHA is still listed (GitHub associates it) with `head_match:false`; `head_repository` may be a fork or null (deleted fork), and a null head repository does not change `head_match`.

## Result and envelope

Schema: [pr-lookup.schema.json](pr-lookup.schema.json) is the draft; the production file `schemas/v2/hosting-prs.schema.json` is created in T014. Complete examples: [lookup-empty.json](lookup/lookup-empty.json) (count 0) and [lookup-two.json](lookup/lookup-two.json) (count 2, second entry `head_match:false`, merged).

Top-level members, all required: `schema_version` (2), `kind` (`prs`), `status` (`ok` or `error`), `backend`, `identity`, `repository`, `candidates` (as `pr`), `query.head_sha`, `pull_requests`, `count`, `truncated`, `diagnostics` (at most one), `observed_at` (UTC result assembly time), `sampled_at` (yes, kept for parity with `pr`: UTC completion time of the validated response, null when no response was validated, so null on every error), `boundary` (`complete`, `network`, `max_bytes` 1 MiB, `timeout_seconds` 30, `max_requests` 1, `requests` 0 or 1, `limitations`). There is no `observation` object (no head expectation, no watch).

| Field | Meaning |
| --- | --- |
| `pull_requests` | array, order as GitHub returned it, at most 30; each entry: `number`, `title`, `url`, `state`, `draft`, `base_ref`, `base_repository`, `base_sha`, `head_ref`, `head_repository` (nullable), `head_sha`, `head_match`, `merged`, `merged_at`, `closed_at`, `merge_commit_sha` (raw), `result_sha`, `lifecycle` |
| `count` | length of `pull_requests` |
| `truncated` | true when exactly 30 entries were returned: more may exist, none was requested; adds the limitation `lookup-single-page` |

Status, exit and diagnostics:

| Case | `status` | Exit | Diagnostic |
| --- | --- | --- | --- |
| count 0 | ok | 0 | none; not proof that no PR exists beyond what GitHub associates with the commit |
| count 1 or many, truncated or not | ok | 0 | none; the command never picks one |
| native/selection/auth failure | error | 1 | the existing hosting diagnostics; `requests` 0, `network` false |
| HTTP 401/403/404/429, other non-2xx, redirect, compression, oversize, transport | error | 1 | `hosting-pr-failed` (existing safe message) |
| invalid or incoherent evidence in any entry | error | 1 | `hosting-pr-evidence`; the whole result fails, no partial list |
| result over the 1 MiB output cap | error | 1 | `hosting-output-limit`; list, identity and repository cleared |
| cancellation or the 30 s deadline | error | 1 | `hosting-canceled` |
| bad SHA, missing/extra arguments, `--json-version`, `--expect-head`, `--watch` | no result | 2 | usage text only, before native/auth/network |

On every error `pull_requests` is `[]`, `count` 0, `truncated` false, `sampled_at` null and `boundary.complete` false; on success `boundary.complete` is true, `network` true and `requests` 1. Titles, refs and URLs are display data, escaped in human output like `pr`. No reviewed, approved, merge-eligible or integrated verdict is produced.

## Conformance

Fixtures (no real network): zero, one open, one merged per merge method, many with and without `head_match`, exactly 30 (truncated), 29, a realistic 30-item ~20 KB-item body under the cap, a body over 1 MiB (error, no partial list), missing/non-string state, `merged_at`/state incoherence, invalid SHA/date, null head repository, each HTTP outcome, redirect, oversize body. Request assertion: exactly one request, method GET, the exact allowlisted path and query. Allowlist test: other `commits/SHA/...` suffixes, other pages and non-GET methods are refused.
