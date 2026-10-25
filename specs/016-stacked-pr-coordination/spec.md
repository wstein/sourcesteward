# Feature Specification: Stacked PR Coordination

> **Owner-decision amendment — 2026-10-25:** all former OQ IDs are decided;
> [current profiles](contracts/owner-decisions.md) supersede earlier undecided recommendations.
> Default saved-plan metadata is .work; remote authority never runs local signing.
> This is a documentation target, not implementation or native acceptance.

**Feature Branch**: `codex/spec-016-stack-toolbelt` (specification only)
**Created**: 2026-10-15 | **Updated**: 2026-10-25
**Status**: Draft design; implementation, independent exact-head review and native conformance pending. All new contracts are UNPUBLISHED Markdown proposals.
**Input**: Preview dependencies and base updates; explicitly authorize each remote effect.
**Priority rating**: 8/10 (product priority, not verification evidence)

## Scope and toolbelt boundary

Spec 012 owns stack observation and the verified local-stack/PR comparison. This
feature consumes that evidence and plans individually selected forge effects.
Spec 014 supplies isolated integration preflight/replay evidence; Spec 015 owns
local restack plan/apply; Spec 013 owns checkpoint/restore/selective undo using
Spec 032 journal/recovery conventions. Observation authorizes no local mutation,
and local apply authorizes no remote write. None of those observations is review,
CI acceptance or permission to set Ready.

The proposed first publication capability is native Git, same-repository GitHub,
owned topic branches and owned PRs only. Native jj observation remains a peer
capability; native jj publication must be measured separately and remains
`capability-unavailable`, including colocated jj, without raw Git fallback.
Spec 012's current nested finding is `restack-advised`; the published 035
`restack-needed` event stays a different producer/envelope/destination contract.
012 SC-009 defines proposed bounded caller-orchestration invariants, but OQ-FANOUT
still gates a concrete reachable caller; 016 single-repository action plans do
not enable an otherwise unused batch helper. Spec 013's pre-action safety copies
are independent of the source checkpoint and survive its drop; jj restoration
has no operation-head CAS and its first slice refuses any workspace working-copy
commit effect, invoking workspace included. Spec 014's evidence for any admitted profile
must bind actual full-H attribute-source pin and private context on each Git
merge invocation; native jj aggregate conflict semantics remain OQ-JJCONF-gated.
These local safety requirements do not supply a publication receipt or authority.

No capability is implemented by this document. Spec 015 D4–D9 and D12–D13 remain
binding: first Git-only plan, explicit old/new parent and branch without a hard
012 dependency, plumbing/quarantine plus CAS rather than porcelain rebase, and
push in 016 (D8). JSON schema 1 bytes stay frozen; CURRENT hidden 015 code 3 becomes target1 only after the decided revision2 implementation task.

Spec 043 owns the human owner's separate landing invocation, its conditional
retarget order and deletion gates. An agent never invokes landing apply, merges,
pushes the default branch or tags, releases, changes settings/protections or
enables auto-merge. Native stack merge is excluded from this amendment. The
original FR-006 no-force-push boundary is amended only for the explicit,
owner-policy-compliant full-SHA lease update in FR-014; plain force stays forbidden.
The blanket remote-deletion exclusion is deliberate for this initial capability,
even where the broader workspace policy permits an agent's own landed-branch
cleanup. Spec 043 owner deletion gates and separately authorized cleanup workflows
remain separate; 016 offers no remote-delete action.

## User Scenarios & Testing

### User Story 1 — Review verified relationships and selected effects (Priority: P1)

A caller consumes local stack/PR evidence and sees what a forge action would do.

**Independent Test**: Temporary native Git/jj fixtures and a fixed fake forge;
compare identities, completeness, selected actions and all state sentinels.

**Acceptance Scenarios**:

1. **Given** an explicit repository and complete 012 observation, **When** a plan
   is requested, **Then** local layer, full commit identity, PR number, head/base
   repository and sampled head/base identities are bound together; a name match
   alone cannot select a PR.
2. **Given** missing credentials, ambiguity, stale supplied evidence or offline
   mode, **When** planning runs, **Then** local evidence remains available and
   unavailable remote facts block dependent actions without authentication or
   an implicit network refresh.
3. **Given** no selection, **When** the command group or a plan is requested,
   **Then** no action is selected, performed or prompted; a plan contains an
   empty action set until the caller selects specific effects.
4. **Given** GitHub native stack evidence of a remote rewrite, **When** the child
   differs locally, **Then** coordination reports reconciliation needed and does
   not race GitHub with another rewrite/push. Depth over two open layers, an
   unrelated text-only stack or unknown dependency blocks stack publication.

### User Story 2 — Publish only authorized own work (Priority: P1)

A caller authorizes each selected remote effect against a stored plan.

**Independent Test**: Fake transport plus temporary bare Git remote; inspect
argument vectors, exact leases, fresh guards and before/after remote refs.

**Acceptance Scenarios**:

1. **Given** selected push, PR creation, own-body edit, retarget, Ready or
   capability-gated native link/unstack actions, **When** a plan is reviewed,
   **Then** each has its own identity, expected state, permissions, effects and
   blockers; sync is separate composition, never an automatic action bundle.
   Standalone comments from the original draft are deferred under OQ-WRITES and
   cannot be selected for execution in this first capability.
2. **Given** unchanged evidence and `--plan ID` plus an explicit remote
   authorization for each action, **When** apply runs, **Then** only those
   actions execute, sequentially, with fresh guards immediately before each.
3. **Given** an owned rewritten topic branch, **When** its push is selected,
   **Then** the full observed remote SHA, exact ref and full actual candidate
   SHA are bound; a stale lease refuses without force or a widened refspec.
4. **Given** rewritten documentation only, **When** push is evaluated, **Then**
   green local checks suffice, with equal range-diff for a pure rebase. **Given**
   code/tests/scripts/generated changes or unknown class, **Then** the actual
   new SHA needs independent CLEAR before push; a pure equal rebase may use the
   middle review tier. No predicted quarantine SHA counts as that actual SHA.
5. **Given** a retarget of an owned child, **When** apply runs, **Then** its state,
   head and base are read before/after; only the planned base changes. Ready
   evidence must be recomputed after the edit, since checks may not rerun.
6. **Given** an unowned branch/PR/body, failed attribution gate, foreign identity,
   signed-history uncertainty or unreadable permissions, **When** an action is
   evaluated, **Then** it refuses without borrowing local apply authorization.

### User Story 3 — Recover an honest partial result (Priority: P1)

A caller distinguishes completed, refused and unknown remote outcomes on retry.

**Independent Test**: Crash/lost-response injection before and after each fake
write; compare fresh reconciliation and the bounded durable action record.

**Acceptance Scenarios**:

1. **Given** a remote write succeeded but its response was lost, **When** retry
   reads fresh state, **Then** a proven completed effect is reported without
   duplication; unknown creation/comment/link outcomes block replay rather than
   creating a duplicate or guessing failure.
2. **Given** cancellation, rate limits, moved heads/bases or a permission change,
   **When** execution stops, **Then** no next write starts and each attempted
   effect remains completed, refused, failed or unknown as actually observed.
3. **Given** a completed push or base edit, **When** local restore is offered,
   **Then** it never claims remote rollback; owner resync commands are inert data
   naming the old pushed SHA and their destructive limits.

### User Story 4 — Prepare Ready and owner handoff (Priority: P1)

A caller sees every remaining readiness and owner-only landing blocker.

**Independent Test**: Exact-head review/CI fixtures, rebaseability and scope
matrices, plus both Spec 043 conditional landing orders as non-executed data.

**Acceptance Scenarios**:

1. **Given** independently CLEAR exact current head, green required CI,
   `rebaseable == true` and accepted PR scope, **When** Ready is individually
   authorized, **Then** its fresh guards pass; unknown/missing/stale evidence
   refuses. Unfinished work outside that accepted PR scope is not a blocker.
2. **Given** any missing gate, **When** the handoff is shown, **Then** Draft has a
   named blocker, and no mergeability field or 025 explanation becomes clearance.
3. **Given** readable-off `delete_branch_on_merge`, **When** owner handoff is
   rendered, **Then** it states land bottom → retarget child → delete bottom
   only after retarget → land child, subject to 043 gates. **Given** on/unreadable,
   **Then** it states retarget → re-read → owner land is unmeasured and 043-gated;
   unreadable shapes cannot qualify. Neither sequence is run by 016.

### User Story 5 — Rely on a bounded native contract (Priority: P1)

Humans and agents receive the same qualified evidence without hidden execution.

**Independent Test**: Normal, malformed, hostile, unsupported, raced, cancelled
and boundary fixtures for every declared backend/version/platform capability.

**Acceptance Scenarios**:

1. Human and JSON results agree on native identities, mode, selected effects,
   completeness and blockers; unsupported jj publication refuses natively.
2. No repository text, URL, alias, dependency extension, hook, credential helper,
   signing helper, model, editor or shell instruction is executed. Authentication
   uses only the bounded caller-owned path of Spec 022 when explicitly online.
3. Exhausted request/time/output budgets produce incomplete/partial results;
   absent mandatory native execution never counts as acceptance.

### Edge Cases

- Rebase-merged parent, native GitHub rewrites in flight, deleted/gone branch,
  merged out of order, closed-unmerged PR, cross-repository/fork head or base.
- Lease race/ABA, PR edited during apply, successful write with lost response,
  duplicate-looking PR/comment and interrupted journal durability.
- Open parent commits outside the moved child range, leaked `user.*`, signed
  source commits, signing-required remote, unavailable exact-head review.
- Missing required contexts, unreadable rulesets, null rebaseability, Draft with
  no named blocker, partial/denied native membership and rate limits.
- jj colocation, ambiguous/GitButler ownership, malicious refs/body/terminal bytes,
  symlinked private state, exhausted budgets and owner local commits.

## Requirements

### Functional Requirements

- **FR-001**: Consume explicit forge/repository selection and verified local-stack/PR head/base identities; never infer linkage from names alone. Resample supplied evidence in the selected scope before using it for a remote action. Bind producer/command, kind, schema version and exact inputs; diagnostic names alone never identify a payload contract. The published 035 restack-needed event is not the 012 restack-advised finding, and its merge-commit new-parent is not an unvalidated trunk/apply target.
- **FR-002**: Keep local inspection/plan consumption independent of authentication. Default network off; disclose explicitly selected bounded reads, with no implicit fetch, authentication, refresh or HTTP cache.
- **FR-003**: Preview dependency order, PR bases and every individually selected effect, expected state, permissions and blocker. Local mutation authorization never authorizes a push, PR write or comment.
- **FR-004**: Revalidate relevant local/remote evidence, ownership, permissions, capability and action-specific authorization immediately before each write; stale or unknown evidence refuses with a typed reason.
- **FR-005**: Preserve resumable per-action partial outcomes and duplicate prevention. Retry reconciles fresh exact state; unknown outcome refuses replay. Local recovery never claims reversal of remote effects.
- **FR-006**: Never merge, push default/protected refs or tags, delete remote refs, release, enable auto-merge or alter settings/protections. No plain force, bare lease, arbitrary refspec or native-stack merge exists; only FR-014 permits a selected owned-topic rewrite with a full lease.
- **FR-007**: Declare GitHub as the first forge and native Git publication as a separate capability. Native jj observation/publication capabilities remain distinct, measured and explicit rather than inferred from Git support.
- **FR-008**: Preserve native Git commit/ref and jj commit/change/operation identities; refuse ambiguous/GitButler ownership and unsupported native jj publication without raw Git fallback, including colocation.
- **FR-009**: Use one understandable human/versioned JSON result for success, refusal, incomplete and partial outcomes; preserve published contracts byte-for-byte. Require no prompt, editor or pager.
- **FR-010**: Enforce finite documented request, action, byte, record and time bounds, cancellation and per-source completeness. Missing, truncated or failed evidence never becomes success or permission.
- **FR-011**: Require failing-first safety/conformance tests, independent exact-source review and minimum/current supported Git/jj evidence for every declared capability; missing mandatory execution blocks acceptance.
- **FR-012**: Default selections to none. Command groups show help without reads, writes or prompts; stored plan plus explicit remote authorization for EACH action is required to apply. An authorization for one action or an old plan never authorizes another. Explicit save uses the decided validated .work metadata default; rendering stays unsaved.
- **FR-013**: Restrict agent writes to verified own task branches, own PRs and their explicitly selected own bodies. Standalone comments remain a deferred original-draft proposal under OQ-WRITES, not an enabled agent capability. Names, commit authorship hints, caller labels and visibility never establish ownership or widen scope.
- **FR-014**: A topic push binds exactly one full destination ref, actual full new SHA and full remote old SHA observed online in this run; use only `--force-with-lease=REF:FULL_OLD_SHA` with `FULL_NEW_SHA:REF` for an existing owned topic. No plain force, bare lease, default branch or tags. Creation needs an explicitly absent-ref guard and separately measured capability; unknown absence refuses.
- **FR-015**: Recompute `publication_range_change_class` from the UNION OF PER-COMMIT PATHS across `<observed remote default branch>..<actual new head>`, including every still-open parent commit; never classify from a net tree diff, child-only/moved range or caller path list. Adapt 015 `review_gate.change_class` into the 016-only `moved_scope_change_class` after validating accepted producer/command, kind/version, moved scope and exact actual-head binding; never read that alias as a field emitted by 015. Retain this moved-scope class separately; effective `change_class` is `code` if EITHER moved scope OR publication range contains code/tests/scripts/generated files or is unknown, and `docs-only` only when BOTH are completely proved docs-only. Compute `pure_rebase` from same-count all-`=` range-diff. Rewritten docs-only needs green local checks and, if pure rebase, equal range-diff before push; rewritten code/tests/scripts/generated requires independent review of the exact ACTUAL new SHA before push, with middle tier allowed for pure equal rebase. Create the candidate first, then review; predictions never satisfy this gate. The initial 016 capability explicitly excludes the broader workspace 2026-10-17 later-round wording-only review exception; such code-range pushes still require the code gate, without narrowing the owner's separate manual workflow.
- **FR-016**: Before every push inspect actual author AND committer identities over `<observed remote default branch>..<new head>`, including open parent commits, against exactly the trusted owner identity; callers cannot extend the allowed set. Report bounded `user.*` origin evidence and refuse foreign `user.name`/`user.email`; never write Git configuration.
- **FR-017**: Require explicit actual-candidate signing-policy evidence; never silently discard source signatures or invoke signing helpers. The owner decided default refusal plus optional explicit local 015 host-helper re-sign; 016 validates only the final verified signed receipt, never invokes a helper under remote authorization. Missing helper/profile/signature proof refuses with no unsigned fallback; 043 lands only the exact final reviewed head.
- **FR-018**: Ready is a separately selected remote write requiring fresh independent CLEAR for the exact CURRENT head, required CI green, `rebaseable == true`, accepted PR scope and no remaining in-scope blocker. Unknown fails closed. A docs-only push exception never weakens Ready; 025 observations are not a Ready verdict.
- **FR-019**: Treat native stack membership/rewrites as sampled forge evidence; never compete with an observed or pending GitHub rewrite. Link/unstack execution stays capability-gated until endpoint/method/effects are measured and allowlisted. `gh stack link` is inert reference data using `GH_REPO`, validated numeric PRs bottom to top and explicit trunk base; no `-R`, branch auto-push, extension installation or generic dependency execution.
- **FR-020**: Retarget only an explicitly selected owned child PR to the planned verified base (including default branch). Read state/head/base before and after, require head unchanged, report partial results, and invalidate old Ready evidence after any base edit. Remote permission uncertainty refuses.
- **FR-021**: Reuse bounded hosting resolution/read evidence from Specs 020/022/025/030. Current 022 is GET-only; new writes require a reviewed additive closed method/path/body allowlist and conformance before enablement. Never accept arbitrary endpoints/methods, response URLs, generic `gh` forwarding or hidden native-stack API behavior.
- **FR-022**: Bind immutable plans and durable action intent/outcome records to repository, task, plan/run/action ids and expected state before effects, using reviewed private-state durability conventions from Specs 010/011/032. Journal failure refuses; retry reconciles rather than blindly replays. Store no credentials or session ids in public output.
- **FR-023**: Reconcile shared `--online`, `--human`/`--json`, shortening `--timeout` and `--state-dir` vocabulary explicitly. The owner chose --online only, shared human/shortening-timeout direction and .work saved-plan metadata; captured bytes remain external. Target exit-contract revision2 is 0/1/2; current hidden015code3 persists until T045/T046, with schema1 unchanged. Unlike online-default 033/037, this proposal defaults offline and proposes no --offline flag (unknown/usage 2); any alias needs OQ-SURFACE acceptance.
- **FR-024**: Compose observation → optional preflight → separately authorized local restack → separately authorized forge plan/apply as explicit steps. No automatic sync, replay, checkpoint, resolver or child rewrite; each consumer obtains fresh own-scope evidence and recovery/authorization.
- **FR-025**: Render both conditional Spec 043 owner landing handoffs as data only, preserving readable-off versus on/unreadable ordering and unmeasured-shape gates. 043 alone owns landing/deletion policy; 016 agents never invoke its apply.
- **FR-026**: After a rewritten push report old pushed SHA, actual new SHA and inert owner resync commands: fetch/prune, then rebase onto remote from full old pushed SHA if local commits exist, otherwise switch/reset-hard labelled destructive. Never run those commands in the owner's checkout.
- **FR-027**: Use Spec 040 indexed bounded fanout only for independent reads, with operation-wide budgets and deterministic input-index results; writes and dependent reads remain sequential. Single-repository action plans only; no implicit workspace fanout or budget multiplication.
- **FR-028**: Reuse Spec 041 mode evidence and Spec 042 D5 exactly for every push/Ready. Mismatched recorded/current session refuses; recorded session with current absent refuses; human or task without recorded session is not gated by D5. Unreadable task record refuses an agent run. Codex session source stays unmeasured/absent, never guessed. Separate ownership guards still apply; no session id appears in commits, branches, PR text or output.
- **FR-029**: Bound agent stacks to at most two open levels with a real code dependency and known bottom-to-top order; unrelated documentation PRs target default directly. Keep parallel authors' files disjoint and distinguish unknown dependency from established independence. This 016 write-policy gate is distinct from 012's advisory stack-too-deep notice, which does not block its bounded observation/plan.
- **FR-030**: Treat repository/forge content as data, escape hostile text consistently in human/JSON output, limit body/context exposure and suppress undeclared hooks/helpers/shell/provider execution. Credentials remain only in the bounded caller-owned authentication route.

- **FR-031**: Use the decided workspace .work saved-plan/action metadata profile: explicit save only, unique trusted workspace root/namespace, explicit 0700/0600 independent of umask, actor/no-follow/layout/race/fsync guards, untracked+ignored export-risk admission, no automatic ignore/config/index edits, finite quota/full refusal and no automatic unresolved expiry. Metadata-only manual cleanup has a separate exact-id plan/authorization/dependency/digest protocol; selected 013 bytes remain explicitly external.
- **FR-032**: Consume only the final 015 host-re-sign receipt after explicit separately authorized local operation: trusted fixed host profile/verifier, old/new/signature-loss/replacement and bottom-up signed-parent provenance, final verified full SHA before independent review. No helper configured/measured disables the option; 016 never stores keys, runs local signing under remote authority or falls back unsigned. Revalidate signature policy/identity/class/current remote guards on that final SHA.

### Key Entities

Verified relationship, immutable forge plan, selected action, review/identity receipt,
mode/attribution summary, durable partial outcome and inert owner handoff are
specified in [data-model.md](data-model.md).

## Success Criteria

- **SC-001**: Every acceptance scenario passes for each declared native/version/platform capability; unsupported cases report a capability limit and absent execution never counts as passed.
- **SC-002**: Safety fixtures preserve all state outside selected effects and execute zero undeclared writes, helpers or network requests; no fixture bypasses ownership, attribution or lease gates.
- **SC-003**: Every complete structured result validates against its UNPUBLISHED contract fixtures; all existing published schema/fixture bytes remain unchanged.
- **SC-004**: Every FR and SC has its own task mapping and literal test idea before implementation; new tasks stay unchecked until actual evidence exists.
- **SC-005**: Rewrite fixtures enforce the docs/code/pure-rebase review matrix, full default-to-new-head owner-only identities and actual-candidate signature evidence; no prediction or child-only identity range can pass.
- **SC-006**: Every crash/lost-response/permission/race fixture preserves truthful per-action outcomes and retry makes zero duplicate writes when completion cannot be established.
- **SC-007**: All Ready fixtures require exact-current-head CLEAR, required CI green, true rebaseability and scope acceptance; both conditional owner landing handoffs are printed without execution.
- **SC-008**: Every hostile/limit/cancellation fixture obeys shared budgets, input-index read order and sequential effect order, with no session or secret sentinel in public output.

## Assumptions

- Specs 010/011 supply ownership/private-state conventions; existing sync records
  are not an implemented generic remote transaction journal. The typed integration
  needs review. GitHub operations are not a cross-action transaction and ABA may
  remain undetectable; disclose residual limits rather than infer safety.
- Read-only stack/merge-state/PR evidence does not supply reviewer identity,
  accepted scope or authorization. Trusted workspace policy supplies those sources; missing authority blocks.
- Specs 012/013/014/015 have merged documentation designs. Their runtime
  capabilities remain planned except the hidden Git 015 v1 planner; merged
  documentation is neither native conformance nor permission to execute.
- [Plan](plan.md), [research](research.md), [contract](contracts/forge-effects.md),
  [quickstart](quickstart.md), [tasks](tasks.md) and [analysis](analysis.md) describe
  future implementation only. No constitution amendment or runtime authorization
  is made by this amendment.

## Needs owner decision

Compatibility anchor only: all39 former OQ IDs are DECIDED by the owner on
2026-10-25. [Research](research.md#cross-spec-decision-index) records every dated
disposition; the shared profiles define .work metadata, external payloads,
trusted policy, optional host-only015re-sign and exit revision2. Remaining
source/native/helper/forge evidence gates are unimplemented acceptance work,
not unanswered owner choices. No decision itself authorizes runtime effects.
