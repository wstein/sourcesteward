# Feature Specification: Explicit PR status observations

**Branch**: `codex/hosting-pr-status-design` | **Created**: 2026-10-18
**Status**: Re-scoped on 2026-10-21 into slice 1 (one-shot v2 plus head lookup; awaiting independent design review of the exact head) and a deferred watch slice (the three NOT CLEAR rounds and pending round 4 stay attached to it). Runtime implementation not started.
**Input**: Extend existing PR inspection with merged/closed evidence, compact human output, stable versioned JSON, expected-head validation and head-SHA-to-PR lookup. Explicit bounded watching is deferred to a later slice.

## Owner decisions (2026-10-21)

1. Split Spec 030: ship the one-shot v2 observation now; move `--watch` (injected-clock watcher, final-delivery arbiter) to a later slice.
2. A later, separate small spec adds a checks class (pending/green/red) by reusing `hosting ci`; it yields no Ready verdict and is not part of this spec.
3. Add head-SHA-to-PR lookup through the allowlisted `GET repos/OWNER/NAME/commits/SHA/pulls` (a bounded additive extension of the Spec 022 transport allowlist).
4. Keep `scripts/ci-wait.sh` for long waits until a `stw` watch exists.
5. Slice order: this slice-1 design PR first, then runtime for work packages A, B, C and the lookup only after an explicit go (design review and file handoff precede it).

(The coordinator's analysis numbers these differently; this list is the owner's, renumbered consecutively without gaps.)

## Slice 1 scope

Slice 1 is exactly:

- `stw hosting pr NUMBER [PATH] --json --json-version 2 [--expect-head SHA] [--compact]` and the same flags on `gh jj pr`: the v2 schema and decoder (work package A), the one-shot enhanced operation with expected-head binding (B), the CLI flags in both entry points (C), and conformance, fixtures and documentation for those (the part of F that covers them).
- `stw hosting prs SHA [PATH]` (and `gh jj prs SHA [PATH]`), the head-SHA lookup, described in [contracts/pr-lookup.md](contracts/pr-lookup.md).

Extend `stw hosting pr` and `gh jj pr` through shared operations. Reuse native Git/jj ownership and bounded fixed GitHub GETs. No generic gh wrapper, provisioning, Ready evaluation, merge action, review transfer, checks classification, installation or publication. Specs 020/022 remain the hosting foundation; 028/029 own separate capabilities.

`--watch`, `--json-version` combined with watch, the delivery arbiter, the W/T/D time split and descendant-process verification are NOT in slice 1: see [Deferred slice](#deferred-slice-watch). Slice 1 does not accept a `--watch` flag; it is a usage error (exit 2) until the deferred slice ships.

### Why the old design-review blockers do not apply to slice 1

Rounds 1 to 3 returned DESIGN NOT CLEAR, and the pending round 4 concerns one thing: final delivery of the `--watch` output within the inclusive 30 second bound (the command-owned exit arbiter, the 27s/2s/1s time split and the unverifiable state of inherited native/auth descendants). A one-shot read has none of that: it makes exactly one GET (the lookup exactly one), runs under the existing hosting operation 30 second context and writer contract, and "retains its existing operation/writer contract" (contract section Watch algorithm). It adds no teardown phase, no process exit from command code, no helper, no per-poll authentication and no new claim about descendants. Slice 1 therefore has no dependency on the delivery decision, on `baseline-lifetime.md` or on round 4; those documents keep their status and counts for the deferred slice. Slice 1 still needs its own independent design review of the exact head (it is a new artifact scope), and nothing in the old rounds is treated as clearance of it.

## User scenarios and acceptance

### US1 — Identify closure and merge evidence (P1)

A caller distinguishes a merged PR from an unmerged closure without interpreting a test-merge SHA as integration proof.

1. Open + `merged:false` yields `lifecycle:open`, including when `merge_commit_sha` is non-null; `result_sha` stays null.
2. Closed + `merged:false` yields `closed-unmerged`; closed + `merged:true` yields `merged`, reporting both dates and the available method-dependent GitHub result SHA.
3. Merge/squash/rebase fixtures keep head SHA and reported result separate, even when they differ. No reviewed/approved/integrated verdict is produced.
4. Missing or inconsistent required evidence fails rather than converting missing `merged` to false. Null mergeability does not affect lifecycle or initiate a retry.

### US2 — Choose concise or detailed human output (P1)

`--compact` emits one escaped line from the same enhanced result that powers v2 JSON. Without it human output retains the existing detailed sections and adds labelled merge evidence and expectation. Malicious title/ref/URL bytes never become terminal controls, endpoints or commands. Both forms expose errors.

### US3 — Consume an explicit stable machine contract (P1)

`--json --json-version 2` emits exactly one final object conforming to [the candidate schema](contracts/pr-status.schema.json) (slice-1 subset: [pr-status-once.schema.json](contracts/pr-status-once.schema.json); `mode:once` only, see the contract); nullable data is explicit. Ordinary `--json` remains v1; repo/ci, v1 schema and v1 fixtures remain byte unchanged. v1 consumers must explicitly select v2 to get new fields. Full compatibility details and limitations are in the contract.

### US4 — Bind observations to an expected head (P1)

`--expect-head` accepts only 40 lowercase hexadecimal digits, matching the existing hosting contract. The expectation compares the PR's `head_sha` only, never `result_sha`. A mismatching head fails immediately, even on an otherwise merged sample: the command exits 1 like any operational failure and is distinguishable only by `observation.stop` `expectation-mismatch` and diagnostic `hosting-head-mismatch` (the full 40-hex rule is the only accepted form; no abbreviations). Unsupported SHA-256 OIDs and jj change/operation IDs are refused as usage errors. A matching head says nothing about review, CI or ancestry.

### US5 — Find the PRs for a head SHA (P1)

`stw hosting prs SHA` makes one bounded GET of `commits/SHA/pulls` and returns zero, one or many PRs (at most 30, one page), each with number, state, draft, base, head, merged flag and merge commit, and says explicitly when the single page may be incomplete. It never selects one of several PRs, infers a verdict or follows up with another request. Details: [contracts/pr-lookup.md](contracts/pr-lookup.md).

## Requirements

| ID | Required behavior | Acceptance / contract |
| --- | --- | --- |
| FR-001 | Report lifecycle, required `merged` bool, explicit nullable merge/close dates, raw GitHub SHA and qualified result SHA | US1; field/state table |
| FR-002 | Retain detailed human presentation and add opt-in compact single line from the enhanced model | US2; presentation |
| FR-003 | Explicit v2 JSON, explicit nulls/timestamps, documented compatibility; one final object | US3; schema/compatibility |
| FR-004 | Exact hosting full-SHA expectation; mismatch wins before lifecycle success | US4; precedence |
| FR-006 | Exit 0/1/2; closed-unmerged is an observed terminal result, not a merge failure | exit table |
| FR-007 | Caller-owned bounded auth, fixed typed GET, redaction, redirect/compression refusal; no new network writes; HTTP 401/403/404/429 and transport failures stop at once with no retry or `Retry-After` sleep | effects/bounds; [network policy](#network-policy-and-rate-limits) |
| FR-008 | Preserve native Git/jj capabilities and read-only effects; gh jj still refuses Git ownership | Tier 1 tasks |
| FR-009 | Reject missing/malformed/inconsistent new fields; no sample is accepted from an invalid response | state/error table |
| FR-010 | Report observed head/ref/repository identity from the one sample; disclose that one sample is not transactional | sampling rule |
| FR-012 | Reviewed spec/plan/tasks/analysis before code; meaningful failing tests and mutants; actual acceptance limits recorded | gate/tasks |
| FR-013 | `hosting prs SHA` finds PRs by head SHA through one fixed allowlisted GET, zero/one/many, bounded, no selection, truncation disclosed | US5; [pr-lookup contract](contracts/pr-lookup.md) |

Deferred requirements (not in slice 1, retained verbatim for the deferred slice): FR-005 explicit watch with finite total clock, requests, backoff and bytes, no reset per poll; FR-011 bounded operation, owned teardown and final output phases with disclosed descendant uncertainty; the continuity half of FR-010 (stop on observed identity movement across samples).

## Network policy and rate limits

Slice 1 performs reads only, on an explicit command, GET only, through the existing guarded transport: authenticated by the caller-owned `gh` token path, fixed typed endpoints from the allowlist (`pulls/NUMBER`, and for the lookup `commits/SHA/pulls?per_page=30&page=1`), no redirects, no compression, 1 MiB body cap, 30 second operation context. Behaviour by HTTP outcome, identical for both endpoints:

| Outcome | Behaviour |
| --- | --- |
| 200 with valid body | decode, validate, report |
| 401, 403 (including primary or secondary rate limit), 404, and any other non-2xx (for example a 422 for an unknown SHA) | stop immediately; one request counted; no retry; safe generic message (`hosting-pr-failed`, existing wording naming authentication, access and rate limits); no API body, token or URL credential |
| 429 | stop immediately; no retry, no `Retry-After` or `X-RateLimit-*` sleep; same safe message |
| redirect, compressed body, oversize or malformed body, transport failure | stop immediately with the existing safe messages |

A rate-limit stop is an operational failure (exit 1), not an observation of PR state. Rate-limit headers are not parsed in slice 1; a caller that polls must implement its own spacing, or use `scripts/ci-wait.sh`. A sandbox without network yields the existing authentication/transport failure with `boundary.network` as the existing contract defines it.

### What the lookup endpoint answers

`commits/SHA/pulls` is understood to return the PRs GitHub associates with the commit, so `prs` answers "which PRs are associated with this commit", not "whose branch contains it"; for a commit on the default branch the answer may be only the merged PR that introduced it. This could not be verified offline; slice-1 tests use fixtures and prove decoding, bounds and the request shape, not GitHub's semantics. The preview media type is not believed necessary and must be checked by the owner-authorised first live call. Page size 30 is a bound chosen so 30 full list items stay under the 1 MiB body cap (see the lookup contract).

## Deferred slice (watch)

The following moves out of slice 1 and is not authorized by this spec's slice-1 design review: `--watch` (explicit bounded polling to a terminal PR state with up to 8 GETs and 1/2/4s backoff); the injected-clock watcher (work package D); the command-owned final-delivery arbiter and the guarded process exit (E); the W/T/D time split (27s/2s/1s); descendant-process verification and the `native-descendant-state-unverified` qualification; the continuity checks across samples. Their design text stays where it is and is marked deferred: [contracts/pr-status.md](contracts/pr-status.md) sections Watch algorithm, Delivery decision and Owned completion; [baseline-lifetime.md](baseline-lifetime.md), [watch-lifetime.md](watch-lifetime.md), [auth-lifetime.md](auth-lifetime.md), [teardown-assessment.md](teardown-assessment.md) and [round3-disposition.md](round3-disposition.md) hold the review history, including the three DESIGN NOT CLEAR rounds and the pending round 4. A later slice must obtain its own formal design CLEAR. Until a `stw` watch exists, long waits use `scripts/ci-wait.sh`. A checks class (pending/green/red per head SHA) is a separate later small spec that reuses `hosting ci` and gives no Ready verdict.

## Success criteria

SC-001: all slice-1 state and exit table rows are exercised through the shared operation and CLI fixtures with exact request assertions (one GET per invocation). SC-002: schema accepts valid results and rejects documented invalid/missing/null invariants; legacy contract bytes and behavior pass unchanged. SC-004: native minimum/current Git and jj fixture conformance preserves target state. SC-005: no implementation begins until independent complete-artifact review of the slice-1 head is CLEAR; source tests never imply CLI build/live API acceptance. SC-006: the lookup is exercised for zero, one, many and truncated results, every HTTP outcome above and an allowlist test proving no other `commits/SHA/...` route and no non-GET is reachable, and the oversize-body (no partial list) and 30-item cases. SC-007: the shipped slice-1 schema is the strict `once` subset and rejects `mode:watch`, `max_requests` other than 1 and every stop outside `sampled`, `expectation-mismatch`, `failed`, `canceled`, `output-limit`. SC-003 (fake-clock watcher and blocked-pipe delivery) is deferred with the watch.

## Known limits

- The traceability matrix and analysis are re-marked by slice, not rewritten row by row: rows about watch are deferred, the rest apply to slice 1; a missed row is caught by the exact-head review.
- `prs` semantics (which PRs GitHub associates with a commit, preview media type, real item size) are not verified offline; fixtures prove decoding and bounds only, and the owner-authorised first live call checks the rest.
- The lookup reads one page of 30 and discloses truncation; callers needing more must narrow by other means.
