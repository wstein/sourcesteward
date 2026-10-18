# Feature Specification: Explicit PR status observations

**Branch**: `codex/hosting-pr-status-design` | **Created**: 2026-10-18
**Status**: Complete design candidate, pending independent strong review; implementation blocked.
**Input**: Extend existing PR inspection with merged/closed evidence, compact human output, stable versioned JSON, expected-head validation and explicit bounded watching.

## Scope and decision status

Extend `stw hosting pr NUMBER [PATH]` and `gh jj pr` through shared operations. Reuse native Git/jj ownership and bounded fixed GitHub GETs. No generic gh wrapper, provisioning, Ready evaluation, merge action, review transfer, installation or publication. Specs 020/022 remain the hosting foundation; 028/029 own separate capabilities.

The five outcomes are requested scope. The exact CLI compatibility, eight-read ceiling, backoff and one-second delivery reserve below are **proposed design decisions**, not existing policy or additional user approval. The proposed command-owned final-delivery guard and its acceptance limits in [contracts/pr-status.md](contracts/pr-status.md#delivery-decision) remain behind an independent design/implementation gate. Independent review must evaluate the complete candidate before runtime implementation; no automatic policy relaxation is selected.

## User scenarios and acceptance

### US1 — Identify closure and merge evidence (P1)

A caller distinguishes a merged PR from an unmerged closure without interpreting a test-merge SHA as integration proof.

1. Open + `merged:false` yields `lifecycle:open`, including when `merge_commit_sha` is non-null; `result_sha` stays null.
2. Closed + `merged:false` yields `closed-unmerged`; closed + `merged:true` yields `merged`, reporting both dates and the available method-dependent GitHub result SHA.
3. Merge/squash/rebase fixtures keep head SHA and reported result separate, even when they differ. No reviewed/approved/integrated verdict is produced.
4. Missing or inconsistent required evidence fails rather than converting missing `merged` to false. Null mergeability does not affect lifecycle or initiate a retry.

### US2 — Choose concise or detailed human output (P1)

`--compact` emits one escaped line from the same enhanced result that powers v2 JSON. Without it human output retains the existing detailed sections and adds labelled merge evidence, expectation and watch outcome. Malicious title/ref/URL bytes never become terminal controls, endpoints or commands. Both forms expose errors and stale last-sample timestamps.

### US3 — Consume an explicit stable machine contract (P1)

`--json --json-version 2` emits exactly one final object conforming to [the candidate schema](contracts/pr-status.schema.json); nullable data is explicit. Ordinary `--json` remains v1; repo/ci, v1 schema and v1 fixtures remain byte unchanged. v1 consumers must explicitly select v2 to get new fields. Full compatibility details and limitations are in the contract.

### US4 — Bind observations to an expected head (P1)

`--expect-head` accepts only 40 lowercase hexadecimal digits, matching the existing hosting contract. A mismatching head fails immediately, even on an otherwise merged sample; expectation is never compared to the result SHA. Unsupported SHA-256 OIDs and jj change/operation IDs are refused as usage errors. A matching head says nothing about review, CI or ancestry.

### US5 — Explicitly wait for a terminal PR observation (P1)

Without `--watch`, one numbered PR GET is made, even for open/null-mergeability PRs. Explicit watch samples until merged or closed-unmerged, or stops on mismatch, observed identity movement, failure, eight requests, cancellation or the inclusive deadline. It emits one final observation, not a stream. A terminal state first observed on the last permitted read succeeds; an open last read exhausts the request budget. Timeout is operational failure, not evidence of closure.

## Requirements

| ID | Required behavior | Acceptance / contract |
| --- | --- | --- |
| FR-001 | Report lifecycle, required `merged` bool, explicit nullable merge/close dates, raw GitHub SHA and qualified result SHA | US1; field/state table |
| FR-002 | Retain detailed human presentation and add opt-in compact single line from the enhanced model | US2; presentation |
| FR-003 | Explicit v2 JSON, explicit nulls/timestamps, documented compatibility; one final object | US3; schema/compatibility |
| FR-004 | Exact hosting full-SHA expectation; mismatch wins before lifecycle success | US4; precedence |
| FR-005 | Watch only by explicit flag; finite total clock, requests, backoff and bytes; no reset per poll | US5; watch algorithm |
| FR-006 | Exit 0/1/2; closed-unmerged is an observed terminal result, not a merge failure | exit table |
| FR-007 | Caller-owned bounded auth, fixed typed GET, redaction, redirect/compression refusal; no new network writes | effects/bounds |
| FR-008 | Preserve native Git/jj capabilities and read-only effects; gh jj still refuses Git ownership | Tier 1 tasks |
| FR-009 | Reject missing/malformed/inconsistent new fields; retain last valid sample only as timestamped incomplete evidence | state/error table |
| FR-010 | Stop on observed head/ref/repository movement; disclose undetectable unobserved ABA and nontransactional sampling | sampling rule |
| FR-011 | Include native/auth/read/decode/backoff/render/final stdout and stderr delivery in watch deadline; no leaking blocked writer | delivery decision |
| FR-012 | Reviewed spec/plan/tasks/analysis before code; meaningful failing tests and mutants; actual acceptance limits recorded | gate/tasks |

## Success criteria

SC-001: all state and exit table rows are exercised through the shared operation and CLI fixtures with exact request assertions. SC-002: schema accepts valid results and rejects documented invalid/missing/null invariants; legacy contract bytes and behavior pass unchanged. SC-003: fake-clock and blocked real-pipe tests prove no request after cancellation/deadline and bounded delivery without leaked goroutines. SC-004: native minimum/current Git and jj fixture conformance preserves target state. SC-005: no implementation begins until independent complete-artifact review is CLEAR and the final-delivery guard design is independently CLEAR and its acceptance prerequisites are assigned; source tests never imply CLI build/live API acceptance.
