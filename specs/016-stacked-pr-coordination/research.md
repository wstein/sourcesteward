# Research: Stacked PR Coordination

**Date**: 2026-10-25 | **Status**: Local source/policy research and proposed
choices; no new live forge measurement or runtime implementation.

## Evidence and settled boundaries

| Source | Evidence used and limit |
| --- | --- |
| Repository AGENTS and containing workspace workflow | Own topic branches/PRs; full explicit leases; two-level code-dependent stacks; per-PR Ready; owner-only merge; commit/config identity and rewrite review tier |
| Specs 020/022 | Native ownership and explicit repository resolution, bounded caller-owned auth, fixed HTTPS GET transport, redirect/body/time controls; current transport adds no generic write capability |
| Specs 025/030 | Exact sampled PR head/base and lifecycle, unknown/denied sources; 025 explains waits without Ready/merge promise; 030 lookup can return zero/one/many and does not establish ownership |
| Spec 012 | Local stack/PR comparison and advisory native stack steps; its proposed refresh adds evidence, not remote effects; online scope/name remains OPEN |
| Spec 015 D4–D9/D12–D13 | Git-only first read-only plan, explicit boundaries independent of 012, plumbing/quarantine plus CAS, push owned here; historical v1 prediction has fixed committer and incomplete exit 3 |
| Specs 013/014/032 | Checkpoint/undo, isolated preflight and local journal/recovery are distinct; existing sync record is not generic remote undo; preflight grants no actual-SHA review or publication permission |
| Spec 040 | Indexed bounded independent reads at the edge; no new per-repository budget or competing writes |
| Specs 041/042 | Mode accident guard; own scope from task evidence; real D5 attribution semantics, Codex session absent/unmeasured, public session ids forbidden |
| Spec 043 decisions 6/9, FR-031/034 | Owner human landing only; readable-off measured post-land retarget sequence; on/unreadable pre-retarget/re-read order remains gated and unmeasured; unreadable shapes fail closed |

Native stack evidence in the workspace records a GitHub rebase/retarget with
`delete_branch_on_merge=true`. This does not prove the result with deletion off
or the independent cause of the rewrite. `gh stack link` uses `GH_REPO` rather
than unsupported `-R`; help distinguishes numeric PR arguments from branch
arguments that push. Numeric-link write endpoints/effects are not measured here.
No new API method, dependency execution or native jj publication is inferred.

## Design decisions

### R1 Separate selections and evidence from authority

**Decision**: The proposed plan starts with no selected actions. Each action has
an immutable id, expected state and separate remote authorization bound to its
plan. Offline supplied evidence is usable as qualified data; it never causes
network refresh. Online reads and remote writes are separately explicit.

**Rationale**: A local plan/apply, a native stack name or a hidden-v1 predicted
receipt cannot authorize remote work. Partial outcomes require action-level
binding and durable intent, rather than a command that silently performs a bundle.

**Alternatives considered**: An automatic observe/rebase/push/link/Ready pipeline
would combine unrelated permissions and hide partial failures; rejected. An
interactive prompt by default violates the none-default agent contract.

### R2 Use actual candidate and action-specific policy

**Decision**: Consume 015 actual-candidate evidence only after the candidate
exists. Compute full affected-range change class, pure-rebase equality, trusted
owner identity over observed-remote-default..actual-new-head, bounded `user.*`
origins and explicit signature policy. Rewritten docs-only push needs checks and
pure-rebase equality where applicable; code needs exact actual-SHA independent
review before push, middle tier allowed for pure equal rebase. Ready separately
always requires independent exact-current-head CLEAR, required CI green,
rebaseability true and accepted PR scope.

**Rationale**: Prediction committers and child-only identity ranges miss real
rewritten or still-open parent commits. Equal range-diff does not prove identity,
signatures, semantics, CI or permission. A before-push docs exception cannot weaken
Ready. Spec 043 `--reviewed SHA` is the human owner's statement for its separate
invocation; it is not an agent review receipt for this command.

**Alternatives considered**: Review a predicted SHA, accept repeatable caller
identity allowlists or make every docs push await independent review; each fails
to reproduce the governing workflow, so rejected.

### R3 Capability-gated bounded transport, native adapters

**Decision**: Keep existing GET-only modes unchanged. New write operations need
a new narrow reviewed mode with fixed validated method/path/body union and tests.
Git lease pushes use a separately admitted native capability. Link/unstack and jj
publication remain unavailable until measured. Reference commands remain data.

**Rationale**: A pinned SDK or installed extension is not an effect allowlist.
Native colocation must never give permission to bypass jj ownership with raw Git.

**Alternatives considered**: Generic `gh api`/GraphQL forwarding, extension shell
execution and SDK repository discovery are rejected. No write endpoint or claimed
side effect is invented to fill an unmeasured capability.

### R4 Honest retry and owner-only operations

**Decision**: Use immutable plan and intent/outcome records with a proposed typed
private-state integration. Reconcile remote post-state before retry; unknown
write completion blocks duplication. Return owner resync/landing data only.

**Rationale**: A remote write can succeed before a lost response. Local checkpoint
or CAS restore cannot reverse a pushed branch, base edit, comment or native link.
043 alone owns landing and deletion admission/order.

**Alternatives considered**: Blind automatic retry, automatic inverse write and
reuse of sync restore for remote effects are rejected.

## Needs owner decision

No row below is an owner approval. Affected capabilities remain disabled; no row
reopens settled Spec 015 D4–D9/D12–D13 or Spec 043's conditional landing design.

| ID | Unsettled choice | Recommendation and blocking boundary |
| --- | --- | --- |
| OQ-SURFACE | Exact publish group/action selection syntax and shared flags; 012 online scope/name | Adopt `stack publish plan/apply`, `--online`, `--human`/`--json`, shortening `--timeout`, one `--state-dir`, `--plan ID`, per-action `--authorize-remote ID`; keep local `--authorize`. Reconcile 012 `--pull-requests` as an explicit alias only if approved. Its main question 3 is open; only `--after-fetch` has recorded acceptance. Surface stays proposed. |
| OQ-POLICY | Trusted owner identity, independent review provenance/tier, local check and accepted-scope receipt source | Use trusted workspace policy, never repository text/caller-widened lists/config-derived identity expectation; record exact SHA/evidence kind and allowlisted independent reviewer. Global owner identity is read-only corroboration consistent with workspace rules; record source choice explicitly. Missing source blocks push/Ready rather than invent a ledger or provider. 043's human statement remains separate. |
| OQ-WRITES | Which fixed method/path/body shapes, permissions and idempotency measurements enable PR create/body/retarget/Ready and absent-ref creation? | Measure in a separately authorized disposable forge fixture, then review an additive 022 capability allowlist with request/body bounds and fresh post-state tests. Until then plan those actions as capability-unavailable; current GET modes never write. Standalone comments are deferred original-draft scope, requiring a separate owner capability decision rather than automatic first-slice admission. No arbitrary endpoint/GraphQL fallback. |
| OQ-NATIVE | Native stack link/unstack effects, supported versions, and native jj publication | Measure numeric link with explicit trunk base and `GH_REPO`, native unstack, side effects and retry reconciliation separately; do not run a dependency extension or install one. First Git publication is independent; jj stays capability-unavailable until native semantics/conformance, with no Git fallback. |
| OQ-SIGN | Signed moved commits or required signatures in the actual candidate | Keep 015 candidates explicitly unsigned/no signing helper and report signature-dropped per source commit. Recommend refusing publication when any signature was dropped or signature requirements cannot be satisfied/read, until owner accepts a distinct policy. Pending 015 signing decision and separate 043 OQ6 are preserved; no silently dropped signature. |
| OQ-STORE | Typed plan/action journal integration, retention and interrupted action lookup | Reuse reviewed private store/durability primitives with a new typed remote-action record, immutable ids and fsync discipline; retain unresolved actions without automatic expiry. Do not mutate sync v1 record semantics or promise its restore reverses publication. Decide concrete retention before enabling writes. |
| OQ-EXIT | Future shared result version and incomplete exit | Propose 0 complete/1 refused-incomplete-partial-error/2 usage for new 016; preserve hidden 015 v1 exit 3 and every implemented/published contract. Any migration needs an explicit versioned owner decision. |

Single-repository plans deliberately exclude multi-repository/workspace action
fanout. A future batch command would need explicit repository selections,
per-member capability/action authorization and one run-wide budget; it is not
silently supplied by internal read fanout or Spec 023 inventory.

## Validation implications

Every FR and SC is traced individually in [analysis.md](analysis.md). Future
fixtures must exercise the real CLI/operation edges with fake transport/native
sentinels rather than canned safe verdicts, including full affected ranges,
actual candidate SHA, lost response, unknown permissions, both owner-handoff
orders and the exact D5 absence cases. No source/model/forge acceptance is claimed
from document checks alone.
