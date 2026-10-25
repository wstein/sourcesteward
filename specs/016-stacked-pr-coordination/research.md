# Research: Stacked PR Coordination

**Date**: 2026-10-25 | **Status**: Local source/policy research and proposed
choices; no new live forge measurement or runtime implementation.

## Evidence and settled boundaries

| Source | Evidence used and limit |
| --- | --- |
| Repository AGENTS and containing workspace workflow | Own topic branches/PRs; full explicit leases; two-level code-dependent stacks; per-PR Ready; owner-only merge; commit/config identity and rewrite review tier |
| Specs 020/022 | Native ownership and explicit repository resolution, bounded caller-owned auth, fixed HTTPS GET transport, redirect/body/time controls; current transport adds no generic write capability |
| Specs 025/030 | Exact sampled PR head/base and lifecycle, unknown/denied sources; 025 explains waits without Ready/merge promise; 030 lookup can return zero/one/many and does not establish ownership |
| Spec 012 | Merged design uses nested restack-advised finding, separate from published 035 restack-needed; SC-009 caller fanout remains OQ-FANOUT-gated, online scope/name OPEN; no remote effects |
| Spec 015 D4–D9/D12–D13 | Git-only first read-only plan, explicit boundaries independent of 012, plumbing/quarantine plus CAS, push owned here; historical v1 prediction has fixed committer and incomplete exit 3 |
| Specs 013/014/032 | Merged 013 design has independent pre-action safety copies, invoker-inclusive jj working-copy-effect refusal and no operation-head CAS; 014 binds full-H attribute source/private context and gates jj conflict aggregation; sync is not generic remote undo |
| Spec 040 | Indexed bounded independent reads at the edge; no new per-repository budget or competing writes |
| Specs 041/042 | Mode accident guard; own scope from task evidence; real D5 attribution semantics, Codex session absent/unmeasured, public session ids forbidden |
| Spec 043 decisions 6/9, FR-031/034 | Owner human landing only; readable-off measured post-land retarget sequence; on/unreadable pre-retarget/re-read order remains gated and unmeasured; unreadable shapes fail closed |

Native stack evidence in the workspace records a GitHub rebase/retarget with
`delete_branch_on_merge=true`. This does not prove the result with deletion off
or the independent cause of the rewrite. `gh stack link` uses `GH_REPO` rather
than unsupported `-R`; help distinguishes numeric PR arguments from branch
arguments that push. Numeric-link write endpoints/effects are not measured here.
No new API method, dependency execution or native jj publication is inferred.

## Merged design qualifications

The 012/013/014/015 design artifacts are present on main; this is documentation
integration, not implementation/conformance acceptance. 012's current finding
code is `restack-advised`; its earlier pending `restack-needed` spelling in 015
is a historical snapshot, never a shared schema. The published 035 event remains
unchanged. 012 SC-009 requires isolated indexed 1..16 repository observations
with at most four workers/shared budgets, but OQ-FANOUT must select a reachable
entry before production orchestration; 016 remains single-repository.

013 safety copies are self-contained, have distinct safety ids/roots/payloads and
survive checkpoint drop/completed restore; deletion needs their own separate
plan/authorization. Its native jj restoration has no head CAS/exclusion or
always-detect guarantee, may observe native integrated-away races, and initially
refuses working-copy commit effects for every workspace including the invoker.
Those limitations remain relevant evidence boundaries; no 016 rollback uses them.

014 T033 is the red attribute-source regression gate before replay T016. The
profile pins every actual merge-tree call to full source H using `--attr-source`,
private admin/worktree/index without original attribute overrides, exact H/N and
repeated guards/receipt binding. Missing native version/enforcement refuses.
The related existing hidden-v1 code follow-up is tracked separately in
[issue #241](https://github.com/wstein/sourcesteward/issues/241); a merged design
neither fixes that implementation nor supplies new replay acceptance. 014
OQ-JJCONF blocks native jj conflict-evolution/aggregate semantics: Git stop-first
and a clean final tip cannot replace complete native graph evidence. OQ-SEM,
TOPO, HUNK, SCRATCH, FLAGS and EXIT remain explicit, unapproved capabilities.

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
or Git CAS restore cannot reverse a pushed branch, base edit, comment or native link.
043 alone owns landing and deletion admission/order.

**Alternatives considered**: Blind automatic retry, automatic inverse write and
reuse of sync restore for remote effects are rejected.

## Needs owner decision

No row below is an owner approval. Affected capabilities remain disabled; no row
reopens settled Spec 015 D4–D9/D12–D13 or Spec 043's conditional landing design.

| ID | Unsettled choice | Recommendation and blocking boundary |
| --- | --- | --- |
| OQ-SURFACE | Exact publish group/action selection syntax and shared flags; 012 online scope/name | Adopt `stack publish plan/apply`, `--online`, `--human`/`--json`, shortening `--timeout`, explicit `--save --state-dir DIR` into an existing validated private directory, no implicit store default, `--plan ID`, per-action `--authorize-remote ID`; keep local `--authorize`. Reconcile 012 `--pull-requests` as an explicit alias only if approved. 012 OQ-NET is open; only `--after-fetch` has recorded acceptance. Surface stays proposed. |
| OQ-POLICY | Trusted owner identity, independent review provenance/tier, local check and accepted-scope receipt source | Use trusted workspace policy, never repository text/caller-widened lists/config-derived identity expectation; record exact SHA/evidence kind and allowlisted independent reviewer. Global owner identity is read-only corroboration consistent with workspace rules; record source choice explicitly. Missing source blocks push/Ready rather than invent a ledger or provider. 043's human statement remains separate. |
| OQ-WRITES | Which fixed method/path/body shapes, permissions and idempotency measurements enable PR create/body/retarget/Ready and absent-ref creation? | Measure in a separately authorized disposable forge fixture, then review an additive 022 capability allowlist with request/body bounds and fresh post-state tests. Until then plan those actions as capability-unavailable; current GET modes never write. Standalone comments are deferred original-draft scope, requiring a separate owner capability decision rather than automatic first-slice admission. No arbitrary endpoint/GraphQL fallback. |
| OQ-NATIVE | Native stack link/unstack effects, supported versions, and native jj publication | Measure numeric link with explicit trunk base and `GH_REPO`, native unstack, side effects and retry reconciliation separately; do not run a dependency extension or install one. First Git publication is independent; jj stays capability-unavailable until native semantics/conformance, with no Git fallback. |
| OQ-SIGN | Signed moved commits or required signatures in the actual candidate | Keep 015 candidates explicitly unsigned/no signing helper and report signature-dropped per source commit. Recommend refusing publication when any signature was dropped or signature requirements cannot be satisfied/read, until owner accepts a distinct policy. 015 OQ-8 and separate 043 OQ6 are preserved; no silently dropped signature. |
| OQ-STORE | Typed plan/action journal integration, retention and interrupted action lookup | Reuse reviewed private store/durability primitives with a new typed remote-action record, immutable ids and fsync discipline; retain unresolved actions without automatic expiry. Do not mutate sync v1 record semantics or promise its restore reverses publication. Decide concrete retention before enabling writes. |
| OQ-EXIT | Future shared result version and incomplete exit | Propose 0 complete/1 refused-incomplete-partial-error/2 usage for new 016; preserve hidden 015 v1 exit 3 and every implemented/published contract. Any migration needs an explicit versioned owner decision. |

### Cross-spec decision index

This final-wave index names **all 39 current (spec, ID) pairs** from the linked
source decision sections. It repeats their recommendations or existing deferral;
no row records an owner answer or changes a settled decision. The related 016
group identifies an evidence dependency or owner-handoff boundary, not a demand
that unrelated future work block an accepted independent PR. 043 entries remain
human-only landing choices and never enable a 016 landing/deletion effect.

| Source decision | Existing recommendation or deferral | Blocked capability | Related 016 group |
| --- | --- | --- | --- |
| [012 OQ-NET](../012-stack-inspection/spec.md#needs-owner-decision) | Keep offline default; choose explicit online scope/name, recommending --online with optional approved alias. | 012 remote command surface | OQ-SURFACE |
| [012 OQ-EXIT](../012-stack-inspection/spec.md#needs-owner-decision) | Preserve hidden 015 v1 exit 3; require a versioned decision before unifying 0/1/2. | Shared protocol migration | OQ-EXIT |
| [012 OQ-FLAGS](../012-stack-inspection/spec.md#needs-owner-decision) | Recommend exclusive --human and shortening timeout; keep no store/offline alias unless selected. | 012 shared-flag admission | OQ-SURFACE |
| [012 OQ-FANOUT](../012-stack-inspection/spec.md#needs-owner-decision) | Recommend a named reachable higher-level caller; defer batch code until entry approval. | 012 production batch orchestration | OQ-NATIVE (caller capability; 016 stays single-repository) |
| [013 OQ-1](../013-checkpoint-resume/research.md#needs-owner-decision) | Measure native capture matrix; start ordinary selected Git index/disk paths without false jj parity. | Checkpoint capture adapters/versions | OQ-NATIVE, OQ-STORE |
| [013 OQ-2](../013-checkpoint-resume/research.md#needs-owner-decision) | Recommend explicit owned inverse changes; gate native undo/topology/signing and no-jj-CAS restoration limits. | Undo/operation restoration/signing admission | OQ-NATIVE, OQ-SIGN |
| [013 OQ-3](../013-checkpoint-resume/research.md#needs-owner-decision) | Recommend external private quota-bound payload store and separately authorized drop; independent safety copies survive. | Payload store/quota/drop capability | OQ-STORE |
| [013 OQ-4](../013-checkpoint-resume/research.md#needs-owner-decision) | Recommend trusted task/native layout and actor binding; missing/foreign/shared proof refuses. | Local apply/shared-history ownership admission | OQ-POLICY |
| [013 OQ-5](../013-checkpoint-resume/research.md#needs-owner-decision) | Recommend characterized typed durability/root seam; preserve sync v1, separate namespaces and reviewed restore/flag surface. | Recovery writers, retention mechanism and CLI admission | OQ-STORE, OQ-SURFACE |
| [014 OQ-SEM](../014-conflict-diagnosis/spec.md#needs-owner-decision) | Recommend initial single-parent linear replay; separately measure merge/empty/redundant semantics. | Unaccepted merge/replay variants | OQ-NATIVE |
| [014 OQ-TOPO](../014-conflict-diagnosis/spec.md#needs-owner-decision) | Use explicit measured topology allowlist; refuse unmeasured Git/jj shapes. | Merge/rename/mode/gitlink/native graph support | OQ-NATIVE |
| [014 OQ-HUNK](../014-conflict-diagnosis/spec.md#needs-owner-decision) | Keep paths/types default; require exact selected paths and accepted privacy policy for bounded hunks. | Optional hunk/location/content capability | OQ-NATIVE (evidence privacy) |
| [014 OQ-SCRATCH](../014-conflict-diagnosis/spec.md#needs-owner-decision) | Measure native/platform confinement with no shared writable original store or fallback. | Unproved native preflight/isolation | OQ-NATIVE |
| [014 OQ-JJCONF](../014-conflict-diagnosis/spec.md#needs-owner-decision) | Measure native per-revision conflict evolution and aggregate semantics; clean tip/Git stop-first is insufficient. | Native jj preflight aggregate capability | OQ-NATIVE |
| [014 OQ-FLAGS](../014-conflict-diagnosis/spec.md#needs-owner-decision) | Recommend exclusive human alias/shortening timeout; keep store/online/offline unsupported pending choice. | 014 CLI flag admission | OQ-SURFACE |
| [014 OQ-EXIT](../014-conflict-diagnosis/spec.md#needs-owner-decision) | Propose new 014 0/1/2 only; preserve hidden 015 exit 3 until explicit migration. | Shared result/exit migration | OQ-EXIT |
| [015 OQ-1](../015-local-restacking/spec.md#needs-owner-decision) | Keep v1 frozen; choose opted-in version selector and restore preview/save/apply interface. | Future local protocol/publication, not historical planner | OQ-SURFACE, OQ-EXIT |
| [015 OQ-2](../015-local-restacking/spec.md#needs-owner-decision) | Require trusted task/ref ownership and accepted independent exact-SHA review/check provenance where required. | Trusted local inputs and 016 receipt consumption | OQ-POLICY |
| [015 OQ-3](../015-local-restacking/spec.md#needs-owner-decision) | Recommend explicit actual committer equal to trusted owner, preserve author/date, never config-derived expectation. | Actual candidate identity/date policy | OQ-POLICY |
| [015 OQ-4](../015-local-restacking/spec.md#needs-owner-decision) | Measure native jj expectation/effects/recovery; no Git fallback or claimed Git CAS. | Native jj restack enablement | OQ-NATIVE |
| [015 OQ-5](../015-local-restacking/spec.md#needs-owner-decision) | Reuse reviewed durability seam; retain incomplete runs, no first-slice automatic pruning. | Cross-operation retention deletion | OQ-STORE |
| [015 OQ-6](../015-local-restacking/spec.md#needs-owner-decision) | Initial apply refuses every occupied branch, including caller; any worktree-aware widening needs separate design. | Occupied-ref/worktree-update capability | OQ-NATIVE |
| [015 OQ-7](../015-local-restacking/spec.md#needs-owner-decision) | Keep 015 offline; resolve proposed 012 opt-in network scope in 012 without inventing approval. | Disputed 012 remote capability only | OQ-SURFACE |
| [015 OQ-8](../015-local-restacking/spec.md#needs-owner-decision) | Recommend refusal for signed input/required-or-unknown signatures; unsigned candidate and loss evidence explicit. | Signed/required-signature local acceptance | OQ-SIGN |
| [043 OQ1](../043-stw-land/spec.md#open-questions-not-decided-here) | Owner --reviewed remains a statement; machine-checkable CLEAR source is undecided. | Future owner-landing review receipt source | OQ-POLICY (separate human handoff) |
| [043 OQ2](../043-stw-land/spec.md#open-questions-not-decided-here) | Measure scratch ruleset shape before admission; real direct-push acceptance is unknown. | Owner land apply under unmeasured shapes | OQ-WRITES (owner-handoff boundary only) |
| [043 OQ3](../043-stw-land/spec.md#open-questions-not-decided-here) | No unknown-required-check override in v1 unless asked. | Owner override of checks-unknown | OQ-POLICY (separate human handoff) |
| [043 OQ4](../043-stw-land/spec.md#open-questions-not-decided-here) | Measure on/unreadable-setting pre-retarget order; keep land-unmeasured until admitted shape. | Owner stacked landing for unmeasured deletion setting | OQ-WRITES (conditional handoff only) |
| [043 OQ5](../043-stw-land/spec.md#open-questions-not-decided-here) | Confirm owner identity source; current owner-landing proposal follows read-only global identity. | Final owner identity-source choice | OQ-POLICY (separate human handoff) |
| [043 OQ6](../043-stw-land/spec.md#open-questions-not-decided-here) | Whether require-signed is a later owner option remains deferred; no answer supplied. | Future owner-landing signature option | OQ-SIGN (related, separate decision) |
| [043 OQ7](../043-stw-land/spec.md#open-questions-not-decided-here) | Forks/other forges are deferred; native jj remains refused in owner landing v1. | Owner fork/forge/jj landing support | OQ-NATIVE (boundary only; no 016 landing) |
| [043 OQ8](../043-stw-land/spec.md#open-questions-not-decided-here) | Typed 029/031 journal versus plain audit location remains undecided. | Owner landing journal integration choice | OQ-STORE (separate human operation) |
| [016 OQ-SURFACE](#needs-owner-decision) | Propose explicit publish plan/apply, shared flags, save/store and per-action remote authorization; no implicit defaults. | 016 command/flag/store surface admission | OQ-SURFACE |
| [016 OQ-POLICY](#needs-owner-decision) | Choose trusted owner/review/check/scope inputs; no caller-widened identities or invented issuer ledger. | 016 push/Ready trusted receipts | OQ-POLICY |
| [016 OQ-WRITES](#needs-owner-decision) | Measure fixed method/path/body/effects and idempotency; retain GET-only current modes and deferred comments. | 016 new PR/Ready/ref-creation writes | OQ-WRITES |
| [016 OQ-NATIVE](#needs-owner-decision) | Measure native link/unstack and native jj publication separately; no extension execution or Git fallback. | 016 unmeasured native publication kinds | OQ-NATIVE |
| [016 OQ-SIGN](#needs-owner-decision) | Keep unsigned/loss evidence explicit; recommend refusal of dropped/unmet/unknown signature policy until accepted. | 016 signed-history publication | OQ-SIGN |
| [016 OQ-STORE](#needs-owner-decision) | Choose typed durable immutable action integration/retention; retain unresolved outcomes, preserve sync v1. | 016 durable write/retry enablement | OQ-STORE |
| [016 OQ-EXIT](#needs-owner-decision) | Propose new 016 0/1/2; preserve existing bytes/v1 exit 3 until versioned migration decision. | 016 result-version/shared migration admission | OQ-EXIT |

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
