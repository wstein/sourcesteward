# Feature Specification: Local Restacking

**Feature Branch**: `codex/lazyjj-inspired-roadmap` (specification batch; implementation branches deferred)
**Created**: 2026-10-15
**Status**: Draft — first slice (`stw restack plan`) implemented in source on 2026-10-21 and hidden; the independent design review (T001), the minimum/current Git conformance (T021) and the independent review of the exact head (T023) are pending; `restack apply`, jj and the push are later slices
**Input**: Add after the mutation/recovery contract.
**Priority rating**: 8/10 (user-selected product priority, not verification evidence)

## Motivating scenario and first slice (owner decisions, 2026-10-21)

The owner rebase-merges a parent pull request. GitHub rewrites the commits, so every child branch still carries the old parent commits under old identities. By hand the coordinator restacks each child and proves the result:

1. `git rebase --onto <new parent> <old parent> <branch>`, naming both full SHAs.
2. `git range-diff <old parent>..<old head> <new parent>..<new head>`: every row `=`.
3. `git cherry <new parent> <old parent>`: every row `-` (the parent commits were dropped because the new parent already contains them); where patch ids differ, tree equivalence of the touched paths.
4. Equality of the branch's own commit diffs, and an author and committer identity check over `base..head`.
5. `git push --force-with-lease=<ref>:<observed old full SHA> origin <full new SHA>:refs/heads/<ref>`.
6. Resync text for the owner: `git fetch --prune`, then `git rebase --onto origin/<branch> <old SHA> <branch>` if the owner has local commits, otherwise `git switch <branch> && git reset --hard origin/<branch>` (destructive for local-only work and uncommitted changes).

The first slice makes steps 1 to 4 and the texts of steps 5 and 6 a read-only, offline, reviewable `stw restack plan` for native Git. Later slices add `stw restack apply` (Git), a jj peer with a stated capability gap, and, outside this spec, the push (Spec 016). The owner decisions of 2026-10-21 that bind this slice:

| # | Decision |
| --- | --- |
| D4 | Spec 015 starts Git-only. jj follows later with a stated capability gap (FR-007 and the Tier 1 rule): `stw restack` on a jj repository returns `unsupported-backend` and never falls back to Git. |
| D5 | The read-only `restack plan` comes first; `restack apply` is a later slice with its own tasks. |
| D6 | The Spec 012 dependency is replaced by an explicit minimal input: old parent, new parent and the branch to restack, each as a full commit id or a fully qualified ref. |
| D7 | Replay uses `merge-tree`/plumbing and a compare-and-swap ref update, never the porcelain `rebase` (it runs hooks and touches worktrees). The plan predicts with plumbing in a private object quarantine; apply is specified later. |
| D8 | Push with lease is not in 015; it belongs to a Spec 016 L2 amendment. The plan only prints the lease command text and never prints `--force` or a bare `--force-with-lease`. |
| D9 | The author and committer identity check over `base..head` is part of the plan evidence. The expected identities come only from the explicit `--identity` flag (a pass requires it; without it the check is `unknown`), and the replay commits use a fixed constant identity (implementation clarification 2026-10-21: the fixed identity is the committer; the original author is kept so `git range-diff` pairs `=`); stw never writes Git configuration. |
| D12 | "Keep-both" ledger conflicts are defined by an explicit path allowlist in stw configuration, default empty. No heuristic. (Classification is a later slice (WP6, with its `--keep-both-config` flag); the first-slice plan reports conflicting paths without classes.) |
| D13 | The plan includes predicted conflicts and the owner resync command text. |
| Later | Verify commands from configuration are a later slice: executed only if declared, in a sandbox, without network, never repository-controlled. |

Semantic breaks after a clean replay (for example a parent changed a signature the branch calls) are not detectable by stw in this slice. The plan states this in every result and reports instead the touched-paths overlap between the parent's merged changes and the branch's own changes as a risk indicator. An empty overlap is not evidence of safety.

## User Scenarios & Testing

### User Story 1 — Preview a local stack move (Priority: P1)

A caller understands a stack rewrite before applying it.

**Why this priority**: Establishes the primary safe workflow before optional integrations.

**Independent Test**: Exercise the following cases in separate temporary Git and jj repositories; compare declared outcomes and relevant before/after state.

**Acceptance Scenarios**:

1. **Given** an explicit stack and destination base, **When** restacking is planned, **Then** affected revisions, descendants, references, workspaces and potential conflicts are listed.
2. **Given** ambiguous boundaries, immutable/shared revisions or unsupported topology, **When** restacking is planned, **Then** blocked scope and reasons are shown without guessing or widening the target.

### User Story 2 — Apply and recover a local rewrite (Priority: P1)

A caller authorizes a bounded rewrite with recovery evidence.

**Why this priority**: Establishes the primary safe workflow before optional integrations.

**Independent Test**: Exercise the following cases in separate temporary Git and jj repositories; compare declared outcomes and relevant before/after state.

**Acceptance Scenarios**:

1. **Given** an authorized unchanged plan, **When** restacking is applied, **Then** only declared native effects occur and old/new identities and reference outcomes are recorded.
2. **Given** a new conflict, cancellation or partial failure, **When** restacking stops, **Then** native conflict/interruption state and recovery options are reported without automatic discard.
3. **Given** changed refs, workspace owners or operation state, **When** apply is attempted, **Then** stale actions refuse before their mutations.

### User Story 3 — Rely on a bounded agent contract (Priority: P1)

A caller receives the same evidence in human and structured output without hidden effects.

**Why this priority**: Both humans and automation must recognize success, uncertainty and refusal.

**Independent Test**: Exercise normal, unsupported, malformed and cancelled cases for minimum/current supported Git and jj, with effect sentinels and published-contract fixtures.

**Acceptance Scenarios**:

1. **Given** supported success or failure, **When** either output mode is selected, **Then** native identities, qualifications and outcomes agree and complete structured results validate against their documented contract.
2. **Given** ambiguous ownership, a missing/unsupported backend or GitButler-managed state, **When** execution is requested, **Then** it refuses clearly without using another backend as a fallback.
3. **Given** cancellation, malformed data or an exceeded documented bound, **When** execution stops, **Then** incomplete evidence is explicit and never authorizes a consequential action; any completed effects are reported.
4. **Given** repository-controlled helpers, instructions or configuration, **When** the operation runs, **Then** no undeclared executable, hook, prompt or network action occurs and state outside declared effects is preserved.
5. **Given** a missing mandatory backend/version or absent acceptance execution, **When** conformance is evaluated, **Then** the missing evidence blocks acceptance rather than counting as a pass.

### Edge Cases

- Merge topology, empty/redundant changes, immutable jj commits, divergent operations and multiple workspaces.
- Hook failures, conflict creation, cancellation and concurrent reference/operation changes.

## Requirements

### Functional Requirements

- **FR-001**: Depend on the Spec 010 mutation/recovery contract and, for the first slice, on the explicit minimal input of FR-011 instead of Spec 012 stack observation (D6).
- **FR-002**: Require explicit source scope and destination identity; disclose descendant rewrites, reference movements, disk/index changes, snapshots and hook effects before apply.
- **FR-003**: Respect immutable/protected and shared work; refuse unsupported topology or ownership rather than falling back to another backend.
- **FR-004**: Retain recoverable pre-operation identities and report native old/new mappings, including jj stable change identity versus rewritten commit identity.
- **FR-005**: Handle Git interrupted operations and jj stored conflicts explicitly; neither backend may report a clean completed outcome when unresolved work remains.
- **FR-006**: Revalidate relevant state before consequential actions; expose partial failure, cancellation and recovery limitations. Never fetch, push or force-push as part of local restacking. The plan prints, as inert text only, the lease push command (`--force-with-lease=<ref>:<observed old full SHA>` with an explicit full new SHA, never `--force`, never a bare lease) and the owner resync commands; stw does not run them, and executing a push belongs to Spec 016 after its own amendment and authorization.
- **FR-007**: Support native Git and jj as Tier 1 peers for declared capabilities; preserve distinct identities and explicit capability gaps, refuse ambiguous/GitButler ownership and never substitute raw Git writes for jj.
- **FR-008**: Provide understandable human output and versioned structured success/error results from the same observations, preserving published contracts; no interactive prompt, editor or pager is required.
- **FR-009**: Declare and enforce observation/action scope, output and time limits, cancellation and incomplete-result behavior; never convert missing or truncated evidence into success or permission to mutate.
- **FR-010**: Require acceptance evidence for minimum/current supported Git and jj, including failure, concurrency, ownership and effects cases; missing mandatory coverage cannot count as passed conformance.

#### First slice: `stw restack plan` (Git, read-only)

- **FR-011**: `stw restack plan` takes the branch to restack, the old parent and the new parent, each as a 40-hex commit id or a fully qualified ref (`refs/...`); short names, abbreviations, revision expressions and `HEAD` are syntax errors. It resolves each to one commit, records the observed branch tip (`old_head`), and may be pinned with `--expect-head`. The old parent must be an ancestor of the old head (`git merge-base --is-ancestor old_parent old_head`); otherwise `old_parent..old_head` would silently move the parent's own commits, and the plan refuses with `old-parent-not-ancestor`. The set of commits to move is then exactly `old_parent..old_head`.
- **FR-012**: The plan is read-only: no ref, index, worktree, configuration or repository-object change, no network, no hook, no helper, no editor, no pager. Predictions use plumbing in a private temporary object quarantine that is removed afterwards; a sentinel test proves the object store and refs are byte-identical afterwards.
- **FR-013**: The plan reports evidence, never a verdict that authorizes anything: (a) `git range-diff`-equivalent pairing of old commits against predicted replayed commits, (b) `git cherry` classification of the old parent's own commits against the new parent with tree equivalence for patch-id mismatches, (c) per-commit own-diff equality, (d) author and committer identity check over the moved commits, (e) predicted conflicts from `merge-tree` with the conflicting paths, (f) the touched-paths overlap risk indicator, (g) affected refs and worktrees, (h) hook and configuration disclosure, and (i) the lease push text and resync text.
- **FR-014**: Refuse, with a stable diagnostic and no partial plan, a target whose worktree is dirty or mid-operation, a branch checked out in another worktree (reported, never touched), an empty moved range (`old_head` equals `old_parent`: `empty-moved-range`, nothing to restack), an old parent that is not an ancestor of the old head (`old-parent-not-ancestor`), unrelated histories (no merge base between old parent and new parent), missing objects, ambiguous or non-unique refs, merge commits in the moved range, shallow or replace/graft-affected history, symlinked or non-canonical repository paths, and configuration or hook hazards listed in the contract (Spec 029 and 031 safeguards: attributes, merge drivers, `core.hooksPath`, `core.fsmonitor`, `include` configuration).
- **FR-015**: The identity check uses expected identities only from `--identity "NAME <EMAIL>"` (repeatable, source `flag`); neither the environment nor any configuration file supplies them, because a placeholder identity leaked into the environment would otherwise be "expected". Without `--identity` the check is `unknown`, never `pass`. stw never writes configuration. The predicted replay commits keep the original author (name, email and date, as a real rebase does, so a clean replay pairs `=` in `git range-diff`) and use a fixed constant committer identity and committer date given through the environment, so no repository-local or global `user.*` is read; they exist only in the quarantine and are never pushed. A repository-local `user.name` or `user.email` is reported as a hazard diagnostic (`local-identity-config`), not refused.
- **FR-016**: The plan states the semantic-break limitation of the motivating scenario in the result (`limitations`) and the overlap indicator; it never reports a clean replay as "safe to push".
- **FR-017**: Results use the new `restack-plan` schema (`schemas/v1/restack-plan.schema.json`, `schema_version: 1`, `kind: "restack-plan"`): versioned, stable and additive; existing published contracts and fixtures do not change. Exit status: 0 for a complete plan (including predicted conflicts or differing evidence), 1 for refusals and errors, 2 for syntax errors, 3 for `incomplete`.
- **FR-018**: Bounds: at most 200 moved commits, 2,000 paths per list, 1 MiB result, 60 s total; exceeding any yields `incomplete` with the bound named, never a truncated success (FR-009).

### Key Entities

- Restack plan: native source scope, destination, affected descendants and declared effects.
- Rewrite outcome: old/new identities, conflicts and recovery evidence.
- Evidence bundle (first slice): the read-only observations of FR-013, see [data model](data-model.md) and [contract](contracts/restack-plan.md).

## Known limits

- Semantic breaks after a clean replay are not detected; only the overlap indicator is reported.
- The prediction can differ from a real rebase (renames, rerere, whitespace options); apply recomputes it.
- Exotic traps (author-date edge cases, unusual attributes beyond the hazard gate, other Git versions than the CI lanes, pack-threshold packing in the quarantine) are not covered by fixtures; the plan then refuses or reports `unknown` where it can detect them, and otherwise the limit stands.
- Mutation controls are required for the safety-critical tasks only (refusals, identity, quarantine, texts, outcome table); other tasks rely on their tests.

## Success Criteria

### Measurable Outcomes

- **SC-001**: Every acceptance scenario passes for each declared supported backend/version; unsupported cases return explicit capability limits rather than fabricated success.
- **SC-002**: Every safety/failure fixture preserves state outside declared effects, executes zero undeclared helpers/network actions and reports incomplete or partial outcomes accurately.
- **SC-003**: Every complete structured success/error fixture validates against its documented contract; all existing published-contract regression fixtures remain valid.
- **SC-004**: Every functional requirement has a traceable acceptance case before implementation handoff; no missing mandatory execution is counted as verified.

## Assumptions

- Depends on Spec 010. The first slice no longer depends on Spec 012 or 014 (D6, 2026-10-21): its explicit input is old parent, new parent and branch. Later slices still reuse Spec 012 stack observation (revalidating its `expected` ids) and Spec 014 conflict diagnosis where they land; optional checkpoints reuse Spec 013.
- Spec 012 (reworked 2026-10-17) supplies the stack observation and an advisory, print-only plan; this spec owns executing a local rewrite and its recovery evidence and must revalidate the Spec 012 `expected` ids. A rewrite that GitHub performs on a native stack is not restacked here.
- The plan reuses the Spec 029/031 proof primitives (patch id, merge-noop, range-diff pairing) and native Git runner discipline; it does not reuse tidy plans or journals.
- No automatic restacking of every stack, remote history rewrite or implicit base synchronization.
- This draft does not amend the constitution or authorize execution. Owner review, plan, tasks and consistency analysis precede implementation; later mutation/forge scope must be reconciled with constitutional constraints during planning.
- Sampled state cannot promise a transaction or detect every concurrent/ABA change; limitations must remain explicit and uncertainty never becomes cleanup proof.
