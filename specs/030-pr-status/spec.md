# Feature Specification: Explicit pull request status observations

**Branch**: `codex/hosting-pr-status-design` | **Created**: 2026-10-18
**Status**: Design checkpoint; incomplete and not independently reviewed. No implementation gate clearance.

## Scope

Extend the existing `stw hosting pr NUMBER [PATH]` and `gh jj pr` operation with five requested capabilities: merged/closed evidence, compact human output with detailed output retained, stable versioned JSON, an exact expected-head constraint, and explicitly requested bounded watching. Preserve native Git/jj ownership, fixed read-only GitHub GETs and one-shot operation by default. Provisioning, arbitrary gh forwarding, Ready evaluation and hosting mutations are excluded.

## Initial acceptance requirements

- **FR-001**: Report GitHub's `merged`, `merged_at`, `closed_at` and `merge_commit_sha`, and distinguish `open`, `closed-unmerged`, `merged`. Do not derive merged state from a non-null SHA. Before merge, that SHA can name GitHub's test merge; after rebase merge it names the commit to which the base was updated, which need not equal the reviewed head. No review or attestation transfers between these identities.
- **FR-002**: Offer a compact single-line human observation, escaping untrusted data; preserve the existing detailed observation through an explicit presentation choice. Human and JSON must derive from the same result.
- **FR-003**: Version the expanded JSON separately from the published strict v1 hosting shape. Preserve `repo` and `ci` v1 contracts. Define explicit nullable fields, `observed_at`, schema compatibility and failure observations before implementation.
- **FR-004**: `--expect-head SHA` compares the observed PR head using the existing hosting full-SHA grammar (exactly 40 lowercase hexadecimal digits); reject short, uppercase and native jj change/operation IDs. An expectation never authorizes writes or asserts review acceptance.
- **FR-005**: Only explicit `--watch` permits repeat reads. One inclusive global deadline must cover native resolution, authentication, requests, decoding, waiting, encoding and delivery. Preserve the existing 30-second upper bound and one MiB output cap. Fix an explicit request cap and bounded backoff before implementation. No new request starts after deadline or parent cancellation.
- **FR-006**: Preserve exit 0 for successful observations, 1 for operational failure and 2 for usage errors. Specify mismatch, observed head changes, deadline, closed-unmerged and watch terminal outcomes before implementation. No hidden retry of null mergeability or HTTP denial.
- **FR-007**: Reuse caller-owned bounded authentication, typed fixed GET transport, refusal of redirects/compression/arbitrary URLs and redacted diagnostics. Both Tier 1 backends retain their existing native behavior and effect boundaries.

## Gate and outstanding decisions

This checkpoint reserves Spec 030. It is not a complete specification. Required before strong independent review: exact CLI compatibility choice; state/field truth table including missing/null/inconsistent fields; complete JSON schema and fixtures; watch state machine, request count, backoff and terminal/error precedence; inclusive output-writer cancellation limits; meaningful TDD/mutant plan; traceability and work-item ownership ledger. No runtime work may begin until the complete cross-artifact gate is independently CLEAR.
