# Feature Specification: Ready inspection and review evidence

**Feature Branch**: `codex/64-ready-evidence-contract`

**Created**: 2026-10-18

**Status**: Draft — specification gate pending; no implementation or deployment

**Input**: Owner-approved issue [#64](https://github.com/wstein/sourcesteward/issues/64): a read-only `stw hosting ready-check NUMBER` and a separate required GitHub `review-evidence` check, sharing pure policy evaluation.

## User Scenarios & Testing

### User Story 1 — Assess Ready eligibility (Priority: P1)

An agent or maintainer assesses one explicit PR against an approved project policy before changing its Ready state.

**Why this priority**: Exact-head review, required CI and rebase eligibility are presently assembled by hand; incomplete evidence must not become a positive verdict.

**Independent Test**: Supply bounded hosting fixtures for an open PR, complete current review, green required CI and supported effective constraints; compare human and structured decisions, then independently remove each prerequisite.

**Acceptance Scenarios**:

1. **Given** all policy requirements are satisfied, **When** the command samples a stable head/base/policy, **Then** it reports `eligible` with the exact identities, evidence sources and observation limits.
2. **Given** missing, failed, stale or unverifiable required evidence, **When** evaluated, **Then** it reports `blocked` or `indeterminate`, never `eligible`.
3. **Given** an otherwise eligible Draft PR, **When** assessed, **Then** Draft alone does not prevent eligibility for the Ready transition; the command makes no PR mutation.
4. **Given** an already-Ready PR whose evidence fails, **When** assessed, **Then** the verdict reflects the failed evidence, not the UI flag.
5. **Given** required rebase eligibility is false, absent or not computed, **When** assessed, **Then** it does not pass and the command does not poll.

### User Story 2 — Authenticate a review report (Priority: P1)

A project selects accountable review issuers and review requirements without adopting another project's account names or model policy.

**Why this priority**: A success label or self-declared model name is insufficient evidence of the review that project policy requires.

**Independent Test**: Change each report binding and authenticated issuer field in turn; preserve a successful label to ensure that it cannot hide the invalid evidence.

**Acceptance Scenarios**:

1. **Given** a trusted exact-head success status and its digest-bound structured report, **When** authenticated source, policy, scope, tier, rounds and findings satisfy policy, **Then** review evidence passes.
2. **Given** a copied report, forged issuer name, edited comment, old head/base or unapproved policy revision, **When** evaluated, **Then** review evidence does not pass.
3. **Given** configured model metadata but no runtime attestation, **When** evaluated, **Then** output preserves that distinction; a policy requiring runtime attestation remains unsatisfied.
4. **Given** full-PR review with no open findings and permitted documented follow-ups, **When** evaluated, **Then** only the explicitly permitted dispositions count as resolved. Unresolved Low findings do not disappear.
5. **Given** public review evidence, **When** recorded, **Then** it needs no private conversation link, raw transcript, private agent handle or session identifier.

### User Story 3 — Protect merge with a separate check (Priority: P1)

The owner installs a trusted check which evaluates review evidence on the PR head and makes it a merge requirement after acceptance.

**Why this priority**: The CLI is advisory unless the hosting platform enforces the agreed requirement.

**Independent Test**: In a separately authorized disposable installation, prove issuer spoofing and PR-controlled checker/policy changes cannot produce an accepted check; exercise event repetition and concurrent PR updates.

**Acceptance Scenarios**:

1. **Given** the publisher's own required check is pending, **When** review evidence satisfies policy, **Then** its review projection can pass without waiting for itself or other CI.
2. **Given** a PR edits policy or checker sources, **When** the trusted publisher runs, **Then** the old approved baseline still governs; the PR cannot activate its proposed weaker gate.
3. **Given** an obsolete event or a moved head/base/policy, **When** publication is attempted, **Then** the publisher never knowingly publishes success for the obsolete sample as current evidence.
4. **Given** a required deployed check, **When** a person changes the Ready UI state manually, **Then** the documentation makes no claim to prohibit that transition; GitHub enforces merge restrictions only.
5. **Given** no owner-approved deployment profile, **When** source artifacts are complete, **Then** the feature remains undeployed and settings/credentials are unchanged.

### User Story 4 — Retain bounded read-only operation (Priority: P1)

Git and jj users receive the same policy decision while native identity and existing inspection guarantees remain intact.

**Why this priority**: Hosting evaluation must not introduce hidden local mutations or weaken native ownership boundaries.

**Independent Test**: Exercise ordinary and linked Git worktrees, standalone/colocated/secondary jj workspaces, hostile inputs, cancellation and every collection limit using temporary repositories and fake HTTP.

**Acceptance Scenarios**:

1. **Given** a supported Git or jj workspace, **When** inspected, **Then** its native ownership is preserved and repository effects match existing read-only guarantees.
2. **Given** an oversized, ambiguous, malformed, redirected or incomplete source, **When** read, **Then** the result fails closed within documented limits.
3. **Given** the final head, base or policy resample differs, **When** returning, **Then** the result is `indeterminate`; there is no favorable-state retry.
4. **Given** existing `hosting` and explanatory `merge-state` contracts, **When** this feature is introduced, **Then** their published schemas and conclusions are preserved.

### Edge Cases

- Same context from several issuers, old successful statuses, duplicate records and check reruns.
- Forks, deleted head repositories, unsupported base refs, incomplete changed-file lists and policy-sensitive renames.
- Report edits/deletion, unknown report versions, duplicate JSON keys, invalid encoding and digest mismatch.
- Runtime-model proof unavailable despite a valid configured-model declaration.
- Protected branches whose effective rules are hidden, required conversations/code-owner reviews, merge queues and other unsupported constraints.
- CI reported only for a test merge commit, and pending self checks or Draft flags reflected in opaque aggregate hosting state.
- Base advancement, policy activation and publication races; duplicate and lost events.
- A PR changes the workflow that can access publishing credentials, including adding another workflow.

## Requirements

### Functional Requirements

- **FR-001**: Provide bounded, noninteractive `stw hosting ready-check NUMBER [PATH] --policy-lock PATH` with the existing repository/host/remote selectors and human/`--json` output. NUMBER is explicit and positive. The CLI MUST perform no VCS write, PR mutation, status publication, model invocation or polling.
- **FR-002**: Preserve native Git/jj ownership and read-only effects. The same leaf under `gh jj` MUST retain its jj-only ownership rule. Local resolution precedes network use; an explicit repository selector never overrides native ownership.
- **FR-003**: Return one versioned result with separate review-evidence, required-CI, effective-constraint and final-readiness components. Both entry points and the publisher MUST use the same pure evaluation rules. Human output derives from the same result.
- **FR-004**: Final decisions are `eligible`, `blocked`, `indeterminate` or `error`. Known unsatisfied requirements block; unavailable, unsupported or inconsistent required proof is indeterminate; operational failure is error. Only eligible exits 0; other results exit 1; usage errors exit 2. The result discloses scope, identities, provenance, limits and sampling time.
- **FR-005**: Use explicit versioned PROJECT policy with approved immutable source/digest binding. Policy MUST declare trusted issuers, accepted model/tier mappings, scope, findings/round rules, required CI and merge constraints. No CrewBook-specific account, status context, model or policy is hardcoded into generic evaluation.
- **FR-006**: Establish policy/checker trust from caller/owner-controlled activation evidence outside the PR's control. A path or filename is not proof of trust. PR content MUST NOT replace its governing baseline. A policy transition names the exact candidate, old/new policy/checker identities and owner approval; activation is a separate operation. Drift or incomplete transition evidence fails closed.
- **FR-007**: Require both authenticated exact-head review success and a versioned digest-bound report. Bind repository identity, PR, head, reviewed base, policy, scope, tier, review rounds and finding dispositions. Validate immutable hosting issuer identities from full source records, not names declared inside the report. Do not use the combined status response as creator evidence.
- **FR-008**: Distinguish issuer-authenticated declarations, configured-model metadata, execution evidence and verified runtime attestation. Missing runtime attestation MUST NOT be invented. v1 accepts declared independent execution only when project policy explicitly permits it; stronger proof requirements remain unsupported. Require no private session identifiers or transcripts in public reports. Preserve existing CrewBook gitnote grammar.
- **FR-009**: v1 review scope is full PR at the bound head/base. No inherited clearance for another head and no implicit narrowed-rebase or wording-only exception. Reports MUST expose all declared rounds and findings; parsers cannot establish that an issuer disclosed every real event. Missing or inconsistent required history fails closed.
- **FR-010**: Collect complete effective required checks and constraints, preserving issuer pins and source distinctions. Require exact-head CI under the v1 supported profile; never conflate a test merge commit with the PR head. Unknown effective policy, ambiguous issuer resolution and unsupported constraints MUST prevent eligibility.
- **FR-011**: For rebase policy, require explicit REST `rebaseable: true`, allowed repository/effective merge methods and, where strict CI applies, proof that the current base tip is included. Do not substitute local merge simulation or an aggregate `clean` field. Draft alone is not a failed transition prerequisite; opaque aggregate states are reported separately.
- **FR-012**: Use allowlisted typed hosting reads only. No arbitrary endpoint, fetched executable, response URL forwarding, generic script or untrusted PR code execution is permitted. Bound time, bytes, requests, pages, records and report structure; exhaustion never proves completeness.
- **FR-013**: Resample relevant PR/head/base identities, base tip and active policy generation before return/publication. Revalidate mutable selected report/status bindings. Any relevant drift prevents success. Sampling is not an atomic guarantee about a later merge.
- **FR-014**: Publish a separate `review-evidence` check using only the evaluator's review projection. Its success does not claim CI or full readiness. It MUST NOT depend on itself, the PR's Ready flag or CI completion. The CLI still includes independently required CI/deployment evidence in final eligibility.
- **FR-015**: The publisher MUST execute approved trusted code/policy with least privileges and authenticate its accepted source. PR workflows cannot forge an accepted result. Event handling is bounded, deduplicated and race-aware; self-generated events cannot create loops. Missing events are a disclosed limitation, not permission to poll indefinitely.
- **FR-016**: Present concrete deployment choices and their platform, identity, credential and owner-operation requirements before rollout. Neither feature approval nor source review authorizes an App, credential, environment, ruleset, build, publication or activation. Unsupported hosting capability blocks that deployment profile, not local design work.
- **FR-017**: Prepare the minimal settings diff only after the actual trusted check exists and passes, preserving existing requirements. Required-check enforcement protects merge, never the manual Ready UI transition or all possible administrator bypasses. Settings activation remains owner-only.
- **FR-018**: Keep Spec 025 explanatory with `verdict: not-asserted`; introduce a separate Ready contract and preserve existing published schemas. Spec 025 implementation is not a dependency; reusable reader primitives may be shared.
- **FR-019**: Record meaningful failing regression tests before implementation, independent exact-revision design/source reviews, actual checks and deployment gaps. Planned work stays planned until its acceptance evidence exists.

### Key Entities

- **Policy lock**: explicit trusted activation binding, repository identity, approved policy/checker revisions and digests, deployment identity and generation.
- **Project policy**: immutable requirements governing one supported project/branch profile.
- **Review report**: public, structured issuer statement about exact-head/base review and its disclosed evidence limits.
- **Hosting sample**: bounded typed observations with completeness, provenance and resampling results.
- **Evaluation**: pure component decisions and final eligibility, with stable reason codes.
- **Publication**: authenticated hosted review projection, separate from CLI execution and owner merge authority.

## Success Criteria

- **SC-001**: Every acceptance scenario has deterministic positive/negative fixtures; no missing, stale, forged or unsupported required evidence yields eligible.
- **SC-002**: CLI and publisher review projections agree for identical policy/evidence inputs; self-pending checks do not deadlock review publication.
- **SC-003**: Human and structured results agree on exact identities, decision, failed requirements and evidence qualifications.
- **SC-004**: Bounds and cancellation terminate every hostile-input fixture; Git/jj effect fixtures prove the documented read-only behavior.
- **SC-005**: An authorized deployment test demonstrates that PR-controlled logic/policy and a wrong issuer cannot satisfy the required review check.
- **SC-006**: No rollout is marked complete before exact-source independent review, required checks, approved deployment choice and concrete owner-applied settings evidence.

## Assumptions and scope

The owner approved both features, not infrastructure provisioning. Deployment selection remains a separate concrete decision after reviewable options exist. Current host metadata exposes configured model selection, not runtime attestation. Full-PR review and supported GitHub REST evidence are the v1 subset; missing proof remains explicit. Review declarations are accountable statements, not independently reconstructed execution histories. No release/build authority follows from this specification.

The specification review gate precedes plan creation; the plan review gate precedes task generation. Both are required by the installed Spec Kit workflow. Independent cross-artifact CLEAR precedes implementation. This draft is not a clearance or an accepted deployment design.
