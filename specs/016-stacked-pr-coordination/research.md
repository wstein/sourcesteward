# Research: Stacked PR Coordination

> **Owner-decision amendment — 2026-10-25:** all former OQ IDs are decided;
> [current profiles](contracts/owner-decisions.md) supersede earlier undecided recommendations.
> Default saved-plan metadata is .work; remote authority never runs local signing.
> This is a documentation target, not implementation or native acceptance.

**Date**: 2026-10-25 | **Status**: Local source/policy research and proposed
choices, plus the fixture measurement run `20261025T170737Z` (summarized under
Measured forge effects); no runtime implementation.

## Evidence and settled boundaries

| Source | Evidence used and limit |
| --- | --- |
| Repository AGENTS and containing workspace workflow | Own topic branches/PRs; full explicit leases; two-level code-dependent stacks; per-PR Ready; owner-only merge; commit/config identity and rewrite review tier |
| Specs 020/022 | Native ownership and explicit repository resolution, bounded caller-owned auth, fixed HTTPS GET transport, redirect/body/time controls; current transport adds no generic write capability |
| Specs 025/030 | Exact sampled PR head/base and lifecycle, unknown/denied sources; 025 explains waits without Ready/merge promise; 030 lookup can return zero/one/many and does not establish ownership |
| Spec 012 | Merged design uses nested restack-advised finding, separate from published 035 restack-needed; SC-009 now rejects batch under decided OQ-FANOUT, online scope/name DECIDED --online only; no remote effects |
| Spec 015 D4–D9/D12–D13 | Git-only first read-only plan, explicit boundaries independent of 012, plumbing/quarantine plus CAS, push owned here; historical v1 prediction has fixed committer and incomplete exit 3 |
| Specs 013/014/032 | Merged 013 design has independent pre-action safety copies, invoker-inclusive jj working-copy-effect refusal and no operation-head CAS; 014 binds full-H attribute source/private context and gates jj conflict aggregation; sync is not generic remote undo |
| Spec 040 | Indexed bounded independent reads at the edge; no new per-repository budget or competing writes |
| Specs 041/042 | Mode accident guard; own scope from task evidence; real D5 attribution semantics, Codex session absent/unmeasured, public session ids forbidden |
| Spec 043 decisions 6/9, FR-031/034 | Owner human landing only; readable-off measured post-land retarget sequence; on/unreadable pre-retarget/re-read order remains gated and unmeasured; unreadable shapes fail closed |

Native stack evidence in the workspace records a GitHub rebase/retarget with
`delete_branch_on_merge=true`. This does not prove the result with deletion off
or the independent cause of the rewrite. `gh stack link` uses `GH_REPO` rather
than unsupported `-R`; help distinguishes numeric PR arguments from branch
arguments that push. The numeric-link write endpoints and effects were measured
on the fixture (see Measured forge effects); the owner-only merge probes M1 to
M3, the retarget probe N4, the unstack probe N5 and the second unstack O-N5
were not run.
No new API method, dependency execution or native jj publication is inferred.

## Merged design qualifications

The 012/013/014/015 design artifacts are present on main; this is documentation
integration, not implementation/conformance acceptance. 012's current finding
code is `restack-advised`; its earlier pending `restack-needed` spelling in 015
is a historical snapshot, never a shared schema. The published 035 event remains
unchanged. 012 SC-009 now proves single-repository admission/rejected batch; the old
1..16/four-worker proposal is dated deferred future design. No batch entry is
approved; 016 remains single-repository.

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
The related hidden-v1 runner gap, [issue #241](https://github.com/wstein/sourcesteward/issues/241),
is fixed for the hidden restack runner by PR #279 with an empty attribute source
(see the Spec 015 verification section on the attribute source of the replay, `../015-local-restacking/verification.md`);
that is not the 014 full-H profile, which stays unimplemented, and the Git 2.46
lane has not run. A merged design supplies no new replay acceptance. 014
OQ-JJCONF blocks native jj conflict-evolution/aggregate semantics: Git stop-first
and a clean final tip cannot replace complete native graph evidence. OQ-SEM,
TOPO, HUNK, SCRATCH, FLAGS and EXIT remain explicit, unapproved capabilities.

## Measured forge effects

Run `20261025T170737Z` on the disposable fixture `wstein/stw-forge-fixture`
(private repository on a free plan; the rulesets and branch-protection reads
answer 403, which the owner accepted as no rules in force). Evidence files:
[part 1](evidence/measurement-20261025T170737Z.md) and
[continuation](evidence/measurement-20261025T170737Z-continuation.md), cited
below as `E1:LINE` and `E2:LINE`. Tools: `gh` 2.102.0, `git` 2.56.0, `gh stack`
v0.2.1 (E2:28). These are observations of that one fixture; they accept no
allowlist and enable no capability. The measured behavior is described per
kind in [forge effects](contracts/forge-effects.md#capabilityeffect-table).

| Id | Observed result | Evidence |
| --- | --- | --- |
| W1 | A lease with an empty expected value creates an absent ref (exit 0, one new-branch line). With the ref present, the same push of another commit is rejected as stale (exit 1), as is a lease naming a nonexistent SHA for an absent ref; the remote ref is unchanged and the ref is not created. A repeated identical push exits 0 with `[up to date]`. The Git trace showed credential-helper calls and no hook; the hook and credential boundary stays unmeasured | E1:62-76 |
| W2 | `POST` of a pull request with title, body, head, base and `draft` answers 201 and `draft` follows the field. An identical second `POST` answers 422 and the open list for the head shows one PR. A nonexistent base answers 422 and creates no PR. An unknown body field is ignored (201) | E1:85-89, E1:99 |
| W3 | A body-only `PATCH` changes the body and `updated_at` only. A `PATCH` with a stale `If-Match` answers 400 and the body stays unchanged (both runs): REST has no conditional body write through `If-Match`, so the digest guard is a client-side read before write. A body of 65537 bytes is accepted (200) | E1:103-104, E1:112-116 (inconclusive there), E2:67-76, E2:78-82 |
| W4 | A base-only `PATCH` answers 200 and changes `base.ref`, `base.sha` and `updated_at` only, on a Draft and on a non-Draft PR (`draft` stays false, `head.sha` unchanged). A nonexistent base answers 422 and changes nothing. A `PATCH` to the current base answers 200 and changes nothing | E2:84-95 |
| W5 | `PATCH` with `draft=false` answers 200 and the PR stays Draft: REST cannot set Ready. `gh pr ready` exits 0 and sets `draft` false, changing only `draft` and `updated_at`; its trace holds a `POST /graphql` query `PullRequestByNumber` and a `POST /graphql` mutation `PullRequestReadyForReview` with the field `markPullRequestReadyForReview` and an `input` object holding `pullRequestId`. On an already-Ready PR it exits 0, sends only the lookup and changes nothing | E2:99-114 |
| N1, N2 | `GH_REPO=OWNER/NAME gh stack link --base main A B` with numeric PRs creates one stack of two (exit 0), pushes no ref and leaves both PRs unchanged. Its trace holds `GET /repos/{r}/stacks`, four `POST /graphql` lookups (`FindPRByNumber`) and `POST /repos/{r}/stacks` with the body field `pull_requests` (201). The repeat exits 0 and sends no write | E2:127-151 |
| N3 | `gh stack link STACK N` appends a member through `POST /repos/{r}/stacks/{number}/add` (200). The same `--base` command with all three members is idempotent. The reversed order of two existing members exits 5 with `Cannot update stack` and changes nothing | E2:153-166 |
| Unstack | Observed in cleanup only, not an N5 verdict: `gh stack unstack STACK` (explicit number) exits 0 through `POST /repos/{r}/stacks/{number}/unstack` (204); the stack is gone and the PRs and branches are unchanged | E2:176-185 |

Not measured: the hook and credential boundary of a Git push (E1:73-76); the
source of the pull request node id for the Ready mutation, the mutation's own
response, and a repeat of the mutation (the repeat run of `gh pr ready` sent no
mutation, E2:111-114); the body fields of the stack `add` request; N4 (retarget
of a linked member), N5 and O-N5 (E2:168-172, E2:185); the owner-only merge
probes M1 to M3 (E2:199-200); comments (deferred). Ordering gates of Ready
(review, CI, rebaseability) are policy and were not measured.

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
Git lease pushes use a separately admitted native capability. Link/unstack remain
unavailable until the reviewed allowlist accepts the endpoints observed for them
(unstack only in cleanup), and jj publication until measured. Reference
commands remain data.

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

## Owner decisions (2026-10-25)

All listed former OQ IDs have a **dated disposition on 2026-10-25**.
IDs without an explicit answer adopt the source research recommendations. The
source column distinguishes direct owner answers, adopted recommendations and
cross-spec owner-answer overrides; implementation/native measurement and
review gates remain, never inferred as passing. The concrete [owner profiles](contracts/owner-decisions.md)
distinguish saved-plan metadata, external capture payloads, trusted policy,
optional host-only signing and CLI exit-contract revision 2.

| Decision ID | Dated disposition | Source | Remaining implementation/verification gate |
| --- | --- | --- | --- |
| OQ-SURFACE | DECIDED: shared surface, --online-only opt-in, explicit save with default workspace .work metadata store, single repository now. | owner answer | CLI/store profile implementation, not an owner answer gap. |
| OQ-POLICY | DECIDED: trusted workspace policy supplies exact identity, reviewer allowlist/check source; global config corroborates only. | owner answer | Missing trusted source blocks push/Ready; receipt/provenance implementation. |
| OQ-WRITES | DECIDED: measure in separately authorized disposable forge, then review small closed 022 write allowlist; comments deferred. | owner answer | PR/ref/Ready effects are measured (see Measured forge effects) but stay unavailable until the reviewed closed allowlist is implemented; no GraphQL/arbitrary fallback. Exception (owner decision 2026-10-25): one fixed `markPullRequestReadyForReview` mutation (pull request node id as the only variable, fixed query string, own effect class, validated), W5a has confirmed that REST cannot set Ready (`PATCH` with `draft=false` answered 200 and left the PR Draft; [evidence](evidence/measurement-20261025T170737Z-continuation.md), W5a), so the exception applies; see `measurement-plan.md` section 1. |
| OQ-NATIVE | DECIDED: Git publication first; measure native link/unstack separately; native jj unavailable without measured native proof. | owner answer | No extension installation/execution or Git fallback. |
| OQ-SIGN | DECIDED: default refuse dropped/unmet/unknown signatures; optional local 015 re-sign with trusted host-only helper, no stw keys. | owner answer | 016 validates final signed receipt; remote authorization never invokes local signing; no unsigned fallback. |
| OQ-STORE | DECIDED: immutable typed action record, fsync, bounded validated .work metadata profile and no automatic unresolved expiry; sync v1 unchanged. | owner answer | Durability/race/export-risk/quota and separate manual cleanup implementation. |
| OQ-EXIT | DECIDED: unified target CLI revision 2 uses 0/1/2 now; 015 current code 3 changes in explicit red-first task. | owner answer | Exit-only revision is separate from frozen result JSON schema 1; no legacy 3 selector. |

## Needs owner decision

Compatibility anchor only: the IDs above are settled owner decisions, not open
questions. Remaining unavailable capabilities require implementation/measurement
evidence, not an invented additional owner answer.

## Cross-spec decision index

All **39 (spec, former OQ ID) pairs have dated dispositions on 2026-10-25**.
Nine source IDs have direct owner answers; the other 30 adopt recommendations.
A global owner answer overrides a conflicting recommendation where marked.
IDs without an explicit answer adopt source research recommendations. This index records no
native/helper/forge measurements or implementation passes; remaining gates are
verification/implementation for these indexed IDs. Separate 043 endpoint,
ownership and constitution reconciliation remains OPEN for owner decisions and
gates the affected capability; it is not covered by these 39 dispositions. The concrete
[owner profiles](contracts/owner-decisions.md) refine metadata, external payload,
trusted-policy, optional host-only re-sign and exit-revision boundaries.

| Source decision | Status/date | Source | Adopted direction | Remaining capability gate |
| --- | --- | --- | --- | --- |
| [012 OQ-NET](../012-stack-inspection/spec.md#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | owner answer | default offline; `--online` is the ONLY opt-in, with no `--pull-requests` alias. | Remote GET admission still needs reviewed transport/native evidence. |
| [012 OQ-EXIT](../012-stack-inspection/spec.md#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | adopted recommendation; owner-answer override: 016 OQ-EXIT | unified CLI exit contract revision 2 targets 0/1/2 now, including 015 incomplete=1, no legacy-3 selector. | 015 code/test amendment is unimplemented; current hidden code still returns 3. |
| [012 OQ-FLAGS](../012-stack-inspection/spec.md#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | adopted recommendation | adopt shared --human exclusive with --json and positive shortening --timeout; no redundant --offline or observation store. | CLI registration/output/bounds tests precede implementation. |
| [012 OQ-FANOUT](../012-stack-inspection/spec.md#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | owner answer | single-repository operation now; batch is deferred to its own later decision, with no approved caller entry. | No current batch API/CLI/helper or unused production orchestration; only deferred future invariants remain. |
| [013 OQ-1](../013-checkpoint-resume/research.md#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | adopted recommendation | adopt measured capture matrix recommendation: explicit ordinary Git tracked index/disk paths first, jj independently measured without implicit snapshot. | Unmeasured/special/index/conflicted/ignored capabilities remain unavailable. |
| [013 OQ-2](../013-checkpoint-resume/research.md#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | adopted recommendation | adopt narrow inverse/restore recommendations, occupied-destination refusal and native jj direct-parent/all-workspace/no-head-CAS limits. | Native semantics, signing and expected-state effects need conformance; no broad undo fallback or invoker working-copy change. |
| [013 OQ-3](../013-checkpoint-resume/research.md#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | adopted recommendation | selected bytes and independent safety copies remain EXTERNAL, private, quota bounded and separately deleted; .work is saved-plan metadata only. | Payload-role validation/capture/drop implementation and evidence; no automatic expiry. |
| [013 OQ-4](../013-checkpoint-resume/research.md#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | adopted recommendation | trusted workspace/task/native layout/actor policy supplies ownership; labels/config/session hints do not. | Missing/foreign/shared proof refuses affected local action. |
| [013 OQ-5](../013-checkpoint-resume/research.md#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | adopted recommendation; owner-answer override: 016 OQ-SURFACE (saved-plan location only) | adopt characterized typed durability/root seam, separate namespaces and explicit stored restore plan; shared human/shortening-timeout vocabulary. | Implementation/migration/conformance remains; metadata defaults to validated workspace .work while --payload-dir stays explicit/external. |
| [014 OQ-SEM](../014-conflict-diagnosis/spec.md#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | adopted recommendation | adopt initial single-parent linear replay and refuse empty/redundant commits; merge variants require separately measured explicit profile. | Accepted direction does not prove any native strategy; unmeasured variants unavailable. |
| [014 OQ-TOPO](../014-conflict-diagnosis/spec.md#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | adopted recommendation | use explicit measured topology allowlist and refuse every unmeasured Git/jj shape. | Native topology/rename/mode/gitlink conformance before enablement. |
| [014 OQ-HUNK](../014-conflict-diagnosis/spec.md#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | adopted recommendation | paths/types default; bounded hunks require exact selected paths and accepted measured privacy capability. | Optional context stays disabled without privacy/conformance evidence; no complete secret detector claim. |
| [014 OQ-SCRATCH](../014-conflict-diagnosis/spec.md#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | adopted recommendation | require measured read-only original and isolated native scratch, no shared writable original store/fallback. | Platform/native isolation conformance; no helper execution. |
| [014 OQ-JJCONF](../014-conflict-diagnosis/spec.md#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | adopted recommendation | adopt measure-before-classification recommendation for per-revision/aggregate native conflict evolution. | Algorithm/native graph semantics remain unmeasured; clean tip/Git stop-first cannot prove jj aggregate. |
| [014 OQ-FLAGS](../014-conflict-diagnosis/spec.md#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | adopted recommendation | --human exclusive with --json and positive shortening --timeout; --online/--offline/--state-dir remain inapplicable. | CLI/timeout tests before wiring; fixed maximum process bound stays. |
| [014 OQ-EXIT](../014-conflict-diagnosis/spec.md#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | adopted recommendation; owner-answer override: 016 OQ-EXIT | target unified 0/1/2 CLI revision 2, including 015 incomplete=1 after its code task. | Current 015 implementation still 3; no schema byte edit in these docs. |
| [015 OQ-1](../015-local-restacking/spec.md#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | adopted recommendation; owner-answer override: 016 OQ-EXIT | unify target exits NOW at CLI exit-contract revision 2; incomplete=1, errors=1, usage=2, no legacy-3 selector. | Red root-dispatch/help/status tests then hidden restack CLI/root mapping change; result schema 1 bytes stay frozen. |
| [015 OQ-2](../015-local-restacking/spec.md#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | adopted recommendation; owner-answer override: 016 OQ-POLICY | trusted workspace policy supplies owner, reviewer allowlist/check sources and task/ref/native association. | Missing source blocks push/Ready; human 043 statement remains separate. |
| [015 OQ-3](../015-local-restacking/spec.md#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | adopted recommendation; owner-answer override: 016 OQ-POLICY | actual committer matches trusted owner, preserve author/date, capture timestamp in plan; global identity corroborates only. | Actual candidate creation/identity implementation and conformance. |
| [015 OQ-4](../015-local-restacking/spec.md#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | adopted recommendation | native jj stays capability-unavailable until measured exact operation/bookmark/descendant/recovery behavior. | No raw Git fallback or claimed Git CAS; independent native acceptance. |
| [015 OQ-5](../015-local-restacking/spec.md#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | adopted recommendation; owner-answer override: 016 OQ-STORE | reuse typed fsync durability; preserve sync v1; incomplete/partial actions never auto-expire, no automatic pruning. | Bounded metadata profile/validated manual cleanup implementation. |
| [015 OQ-6](../015-local-restacking/spec.md#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | adopted recommendation | first apply refuses ALL occupied branches including caller; no worktree update permission. | Any worktree-aware widening remains future separate design/measurement. |
| [015 OQ-7](../015-local-restacking/spec.md#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | adopted recommendation; owner-answer override: 012 OQ-NET | 015 stays offline; 012 remote reads use only explicit --online. | 015 consumes qualified supplied evidence; no implicit refresh. |
| [015 OQ-8](../015-local-restacking/spec.md#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | adopted recommendation; owner-answer override: 016 OQ-SIGN | default refuse dropped/unmet/unknown signatures; optional explicit re-sign only through fixed trusted HOST helper, no stw keys. | Signing/verifier profile must be implemented/measured; absent helper disables option; final signed chain SHA before review. |
| [016 OQ-SURFACE](#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | owner answer | shared surface, --online-only opt-in, explicit save with default workspace .work metadata store, single repository now. | CLI/store profile implementation, not an owner answer gap. |
| [016 OQ-POLICY](#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | owner answer | trusted workspace policy supplies exact identity, reviewer allowlist/check source; global config corroborates only. | Missing trusted source blocks push/Ready; receipt/provenance implementation. |
| [016 OQ-WRITES](#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | owner answer | measure in separately authorized disposable forge, then review small closed 022 write allowlist; comments deferred. | PR/ref/Ready effects are measured (see Measured forge effects) but stay unavailable until the reviewed closed allowlist is implemented; no GraphQL/arbitrary fallback. Exception (owner decision 2026-10-25): one fixed `markPullRequestReadyForReview` mutation (pull request node id as the only variable, fixed query string, own effect class, validated), W5a has confirmed that REST cannot set Ready (`PATCH` with `draft=false` answered 200 and left the PR Draft; [evidence](evidence/measurement-20261025T170737Z-continuation.md), W5a), so the exception applies; see `measurement-plan.md` section 1. |
| [016 OQ-NATIVE](#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | owner answer | Git publication first; measure native link/unstack separately; native jj unavailable without measured native proof. | No extension installation/execution or Git fallback. |
| [016 OQ-SIGN](#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | owner answer | default refuse dropped/unmet/unknown signatures; optional local 015 re-sign with trusted host-only helper, no stw keys. | 016 validates final signed receipt; remote authorization never invokes local signing; no unsigned fallback. |
| [016 OQ-STORE](#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | owner answer | immutable typed action record, fsync, bounded validated .work metadata profile and no automatic unresolved expiry; sync v1 unchanged. | Durability/race/export-risk/quota and separate manual cleanup implementation. |
| [016 OQ-EXIT](#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | owner answer | unified target CLI revision 2 uses 0/1/2 now; 015 current code 3 changes in explicit red-first task. | Exit-only revision is separate from frozen result JSON schema 1; no legacy 3 selector. |
| [043 OQ1](../043-stw-land/spec.md#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | adopted recommendation; owner-answer override: 016 OQ-POLICY | adopt trusted workspace reviewer/check-source policy; retain human --reviewed SHA statement and typed confirmation, no invented ledger. | Independent source/check policy implementation; no agent landing. |
| [043 OQ2](../043-stw-land/spec.md#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | adopted recommendation | retain measured-shape scratch gate for ruleset/direct-owner-push acceptance. | Unmeasured/unreadable shapes remain land-unmeasured; no owner settings workaround. |
| [043 OQ3](../043-stw-land/spec.md#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | adopted recommendation | no checks-unknown override in this v1 capability. | Unknown required checks refuse owner apply. |
| [043 OQ4](../043-stw-land/spec.md#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | adopted recommendation | preserve conditional readable-off vs on/unreadable order; measure pre-retarget shape before apply. | Unmeasured stack shape stays land-unmeasured; fresh post-retarget checks remain. |
| [043 OQ5](../043-stw-land/spec.md#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | adopted recommendation; owner-answer override: 016 OQ-POLICY | trusted workspace policy is identity authority; global Git identity corroborates, never supplies authority. | Missing policy/source or foreign config/commit fields refuse. |
| [043 OQ6](../043-stw-land/spec.md#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | adopted recommendation; owner-answer override: 016 OQ-SIGN | refuse unmet/unknown signature policy; optional re-sign is separate local 015 capability before fresh final-SHA review. | 043 neither creates commits nor signs; it lands only the exact already reviewed head. |
| [043 OQ7](../043-stw-land/spec.md#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | adopted recommendation | forks/other forges/native jj landing remain deferred/unavailable in this first human-only slice. | No native or forge fallback. |
| [043 OQ8](../043-stw-land/spec.md#owner-decisions-2026-10-25) | DECIDED 2026-10-25 | adopted recommendation; owner-answer override: 016 OQ-STORE | use typed immutable durable private action record/profile with fsync and no unresolved expiry, distinct land namespace. | Journal integration/conformance before owner apply; sync v1 unchanged. |

## Validation implications

Every FR and SC is traced individually in [analysis.md](analysis.md). Future
fixtures must exercise the real CLI/operation edges with fake transport/native
sentinels rather than canned safe verdicts, including full affected ranges,
actual candidate SHA, lost response, unknown permissions, both owner-handoff
orders and the exact D5 absence cases. No source/model/forge acceptance is claimed
from document checks alone.
