# Feature Specification: Stack Inspection and Plan

**Feature Branch**: `codex/012-stack-inspection-v2`

**Created**: 2026-10-15 (draft); reworked 2026-10-17

**Status**: Draft v2 — specification reworked after the GitHub native-stack spike; independent analysis (analysis.md), implementation and evidence pending

**Input**: Owner decision (Werner, 2026-10-17, after the spike recorded in [research.md](research.md)): GitHub now has native stacked pull requests, so stack inspection is reworked around them. `jj` is the standard workspace VCS; linear history through rebase-merge is preferred while the merge method stays the maintainer's choice; `stw` shall not duplicate what GitHub does natively. Read-only `stack show` and `stack plan` come first; optional `sync`, `push` and `link` effects are separate later work items.

**Priority rating**: 9/10 (user-selected product priority, not verification evidence)

**Depends on**: [Spec 008](../008-jj-inspection/spec.md) and [Spec 009](../009-workspace-inventory/spec.md) (native identity and reference evidence), [Spec 020](../020-gh-jj-inspection/spec.md) (hosting resolution) and [Spec 022](../022-bounded-go-gh/spec.md) (bounded go-gh transport) for the opt-in pull request evidence. The Git isolation prefix and runner come from the Spec 021 code on `origin/main` (`internal/inspection/pins_run.go`); see [plan.md](plan.md).

## What changed from the 2026-10-15 draft

The first draft assumed that a stack exists only locally and that a forge adds nothing but publication. GitHub now creates, links, rebases, retargets and merges native stacks itself (measured, [research.md](research.md) Measurements M1 to M8). Two consequences:

1. A restack tool that rewrites branches after a parent lands would duplicate, and race with, what GitHub does server side for a native stack. For a plain pull request chain (no native stack) the local rewrite is still needed and is a mistake-prone manual step (`git rebase --onto <new parent> <old parent> <branch>`, applied bottom up with old tips recorded first).
2. The valuable, non-duplicative part is **observation**: which layers exist locally, which pull request and native stack each layer maps to, where local, remote and GitHub's own stack disagree, and what exact command would repair a local-only problem.

The diff and changed-file stories of the first draft (its User Stories 2 and 4 and FR-003, FR-010 to FR-012) are withdrawn from this specification; the disposition of every first-draft requirement is in the **Disposition table** below.

## User Scenarios & Testing

### User Story 1 — Understand a local stack on Git and jj (Priority: P1)

A reviewer or agent names a base and a tip and gets the ordered layers between them, with native identities, branch or bookmark names, upstream relations and the topology problems that make a stack ambiguous.

**Why this priority**: Everything else (drift, plan, pull request mapping) is a reading of this topology.

**Independent Test**: In separate temporary Git and jj repositories (ordinary, linked worktree, colocated jj, secondary jj workspace) build a three-branch linear stack and the refusal topologies; compare the declared layers and diagnostics and the before/after state.

**Acceptance Scenarios**:

1. **Given** `--base` and `--tip` naming local branches or bookmarks and a linear history of three layers, **When** `stw stack show` runs, **Then** three layers are reported bottom first, each with its reference names, tip commit id (and jj change id), fork commit id, commit count and bounded commit list.
2. **Given** a merge commit in the range, a range without a common base, a conflicted or ambiguous jj bookmark, or divergent jj changes, **When** it runs, **Then** the result is an `error` with `stack-ambiguous-topology` or `stack-no-common-base` naming the reason; no stack is guessed and no layer is reported.
3. **Given** two branch names on one layer tip, or another local branch that descends from a layer tip, **When** it runs, **Then** `layer-ref-multiple` and `fork-point` findings disclose it; the stack is not narrowed silently.
4. **Given** a tip that is already contained in the base, **When** it runs, **Then** the result is `ok` with no layer and the finding `stack-landed`.
5. **Given** a jj workspace (including colocated), **When** the command runs, **Then** jj is the backend; Git is never used as a fallback; `unsupported-backend`, `ambiguous-backend` and GitButler refusals match the existing vocabulary.

### User Story 2 — See drift after a parent lands, and print the repair (Priority: P1)

A maintainer who prefers rebase-merge merges the bottom pull request. The remaining layers still carry the old parent commits. `stack show` says so; `stack plan` prints the exact commands and runs none.

**Why this priority**: This is the recurring manual step ("head not rebased after parent merge", "stale child after rebase-merge") and the one place a local tool is not redundant with GitHub for non-native chains.

**Independent Test**: Fixtures built in temporary repositories from the three merge shapes measured in the spike: merge commit (children untouched), rebase-merge (new commit ids, children stale), and squash (a single-commit layer is patch-equivalent, a multi-commit layer is not). Run `show` and `plan` offline, then with fake HTTP evidence.

**Acceptance Scenarios**:

1. **Given** a three-layer stack whose bottom layer was applied to the base with new commit ids (rebase-merge), **When** `stw stack plan` runs on Git, **Then** `layer-applied-in-base` (observed, by patch equivalence) is reported for layer 1 and the plan prints, in order, `git rebase --onto <base> <old tip of layer 1 as a full commit id> <layer 2>` and `git rebase --onto <layer 2> <old tip of layer 2 as a full commit id> <layer 3>`; no step is executed.
2. **Given** the same fixture on jj, **When** `plan` runs, **Then** the local finding for patch equivalence is the stated capability gap (`capability-unavailable`, reason `jj-capability-gap`) and a rebase step for an applied or merged parent is produced only when pull request evidence (`--pull-requests`) shows the parent merged (a base that merely moved, `stack-behind-base`, needs no such evidence); the printed jj step moves the child and its descendants onto the base in one command.
3. **Given** a parent merged with a merge commit (the spike's scenario A), **When** `show` runs after the base is fetched, **Then** no drift finding is reported: the children are untouched and still based on contained commits.
4. **Given** a base that moved, **When** `show` runs, **Then** `stack-behind-base` reports how far; the plan's rebase steps move every layer onto the base, bottom first (Plan rules).
5. **Given** a layer that is checked out in another Git worktree, a conflicted or immutable jj commit, or a partially applied layer, **When** `plan` runs, **Then** the plan stops at that layer with a `blocked` reason and prints no step for it or the layers above.
6. **Given** a squash merge of a multi-commit bottom layer (no patch equivalent) and `--pull-requests` evidence that the parent merged with a one-parent merge commit contained in `--base`, **When** `plan` runs, **Then** the layer is landed (`parent_merge` `rewritten`) and the plan rebases the remaining layers; a squash of a single-commit layer is landed by patch equivalence offline.
7. **Given** the same merged pull request but a `--base` that does not contain its `merge_commit_sha` (stale base), for a merge, rebase or squash, **When** `plan` runs, **Then** no layer is dropped, no `rebase` step is printed and the plan has one `observe` step with note `stale-base`.
8. **Given** a merged layer whose merge commit is not a local object, or an open native-stack layer whose pull request head is not a local object, **When** `plan` runs, **Then** the plan is `fetch` then `observe` (`run-again`); **When** the caller repeats with `--after-fetch` and the object is still missing (head branch deleted and unreachable from any fetched branch, a fork head, or a merge commit never in a fetched branch; measured on the spike: PR #8 merged, head `6e019b0`, branch `t2` deleted), **Then** the plan is a single terminating `observe` with note `object-unreachable`, the layer is not treated as landed, and no `rebase` step is printed. A merged or closed pull request's head commit is never required: `head-sha-differs` is not reported for it.
8a. **Given** all objects local and a native stack whose child GitHub already rewrote (upstream `behind` or `diverged`), **Then** the plan is a single `observe` with note `take-upstream` (described only: the upstream is GitHub's rewrite); **Given** the same finding with upstream `same` or no upstream (GitHub lag, or a conflict GitHub could not resolve), **Then** the note is `native-rewrite-pending`, which claims no difference. **Given** a plain chain whose layers differ from their upstreams, **Then** `upstream-ahead` (upstream `behind`: integrate the upstream first), `push-needed` (upstream `diverged` and the layer tip descends from the `--base` commit: a lease-protected push is the described, later effect L2) or `diverged-undecided` (neither) with no `rebase` and no `link` step. A repeated run after a fetch gives the same plan. On jj, a tracked bookmark moved locally and rewritten by the remote is conflicted and the command refuses (FR-004); a bookmark only fetched moves with the remote and is not divergent.
9. **Given** a merged layer above an unmerged one, **When** `plan` runs, **Then** it is blocked with `merged-layer-not-bottom` naming that layer in `blocked.layer`.
10. **Given** `plan` without `--pull-requests`, or with a failed remote read, **When** it is rendered, **Then** `native_checked` is `false` and the boundary carries the native-stack limitation; **Given** unlinked chained pull requests and unpushed or up-to-date layers, **Then** a `link` step appears only when the plan has no `rebase` step.
11. **Given** any plan, **When** it is rendered, **Then** every step is advisory text, the result states that nothing was executed and that the plan authorizes nothing, and each step names the commit ids it expects so a later executor (Spec 015) can refuse a stale plan.

### User Story 3 — Account for GitHub native stacks (Priority: P1)

With `--pull-requests`, the command reads the pull request of each layer and GitHub's native stack records and reports membership, position and the places where local, pull request chain and native stack disagree.

**Why this priority**: GitHub owns native stack creation, retargeting, rebasing and merging; the value left to `stw` is to show whether the stack GitHub has matches the stack the maintainer has, and to say what is missing (a plain chain that was never linked).

**Independent Test**: Fake HTTP fixtures replaying the measured shapes (stacked pull request with `stack` object, `stacks/N` ordered members, closed stack after merge, unstacked chain without the `stack` key) with real temporary Git and jj repositories.

**Acceptance Scenarios**:

1. **Given** three layers whose pull requests all report the same `stack` number with positions 1 to 3 and the same order as the local layers, **When** `show --pull-requests` runs, **Then** each layer carries its pull request, its stack number and position, `native-stack-managed` is reported, and no drift finding appears.
2. **Given** three open pull requests chained by base ref (each base is the parent head) and none carrying a `stack` object (measured), **When** it runs, **Then** `stack-link-missing` is reported (`action`, observed) and `plan` prints the delegated `gh stack link <bottom PR> <PR> <top PR>` line as an advisory step with the effect `remote-write`, labelled as performed by GitHub's `gh stack`, never by `stw`.
3. **Given** layers in two different native stacks, a stack member that has no local layer, or a different order, **When** it runs, **Then** `stack-split`, `stack-member-not-local` or `stack-order-differs` is reported with both sides' numbers.
4. **Given** the bottom pull request of a native stack was rebase-merged and GitHub has already retargeted the child to the trunk and force-pushed its rebased head (measured), **When** it runs, **Then** the child shows `head-sha-differs` with relation `diverged` (GitHub's rebase produces a head that is not a descendant of the local tip; `unknown` with reason `object-not-local` before the fetch) and `parent-merged-child-stale` with `native_stack: true`; the plan is a `fetch` and `observe` only while an object is missing; with all objects local it is a single `observe` (`take-upstream`, or `native-rewrite-pending` when the upstream equals the local tip) and never a `rebase` step, because the rewrite is GitHub's.
5. **Given** a pull request whose head branch name matches a layer but whose head commit differs from the layer tip, **When** it runs, **Then** the link is labelled `name-matched` (never `verified`), `head-sha-differs` is reported with its relation, and no finding treats the pair as verified.
6. **Given** missing credentials, a denied or not-found optional read, a rate limit, an unavailable stack endpoint (for example GitHub Enterprise Server, unverified) or cancellation, **When** it runs, **Then** the local result is retained, remote findings are `unknown` with a reason, the status is `incomplete` with the matching diagnostic, and nothing is reported as absent.

### User Story 4 — Rely on one bounded, read-only contract (Priority: P1)

A caller receives the same evidence in human and JSON output, without hidden effects, on both Tier 1 backends.

**Why this priority**: Constitution principles I, IV and V.

**Independent Test**: Normal, unsupported, malformed, hostile and cancelled cases on the minimum and current Git and jj lanes with effect sentinels, offline schema fixtures, and a byte comparison of every existing published contract.

**Acceptance Scenarios**:

1. **Given** `--json`, **When** the command runs, **Then** one object valid against `schemas/v1/stack.schema.json` is printed and the human output is rendered from the same result; existing schemas and fixtures are byte-identical to the base.
2. **Given** branch names, bookmark names, commit subjects, pull request titles, login names and stack text containing control bytes, ANSI sequences, bidirectional controls or very long text, **When** rendered, **Then** they are bounded data, escaped in human output, and never become an endpoint, option, or executed or printed command (a ref name that cannot be quoted safely makes its step unprintable with a stated reason).
3. **Given** a moved reference, operation or pull request between the first read and the resample, an exhausted bound, malformed or oversized data, or cancellation, **When** the command ends, **Then** it is `incomplete` or `error` according to the status table and never a complete success with a guessed value.
4. **Given** repository-controlled hooks, filters, external diff, textconv, attributes, aliases, credential helpers or configuration, **When** the command runs, **Then** none is executed, no ref, file, index, object database loose object, jj working copy or jj operation log changes, and no network request is made unless `--pull-requests` was given.

### User Story 5 — Keep consequential effects separate (Priority: P2)

A caller can see which effects a stack workflow would need and that none is part of this feature.

**Why this priority**: The owner wants optional `sync`, `push` and `link` as later, explicitly authorized work items, delegating native stack semantics to GitHub.

**Independent Test**: Documentation and contract checks plus effect sentinels around `plan` over every fixture.

**Acceptance Scenarios**:

1. **Given** the deferred effect catalog ([contracts/later-effects.md](contracts/later-effects.md)), **When** this feature is accepted, **Then** no command, flag or code path performs a push, fetch, rewrite, pull request or stack write, and the catalog names each later effect with its owner spec and authorization needs.
2. **Given** a printed `fetch`, `rebase` or `link` step, **When** any test runs `plan`, **Then** the command that would produce the effect is never started by `stw`.

## Edge Cases

- Detached HEAD, unborn branch, bare repository, stale jj workspace, linked worktrees, a layer checked out elsewhere, submodules in the range (not followed).
- Two branch names on one commit (one layer with several names), a branch that moved between samples, `--base` equal to `--tip`, `--base` not an ancestor of `--tip` (behind or diverged base).
- jj: bookmark conflict, divergent change id, conflicted commit, immutable commit, a working-copy commit inside the range (not snapshotted; stored state only).
- Pull request side: closed unmerged pull request, two pull requests for one head (reopened), draft, fork head repository (unsupported; reported as missing for same-repository heads only and disclosed), a native stack whose PRs are partly merged, a stack larger than the layer bound, `stack` key present but `stacks/N` not readable, GitHub rewriting heads while sampled (eventual consistency; unverified timing).
- Rebase-merge of a multi-commit pull request, squash merge, merge commit; `merge_commit_sha` for the multi-commit and squash cases (unverified).
- Branch names with `/`, leading `-`, spaces, quotes, shell metacharacters or non-ASCII in printed commands.

## Requirements

### Functional Requirements

- **FR-001**: Provide `stw stack show` and `stw stack plan` with the flags of the **Commands table**. Both are read-only, non-interactive and share one operation, parsing and rendering; `plan` adds the advisory `plan` section to the same observation. No `gh jj` entry point is added in this slice.
- **FR-002**: Require an explicit `--base` and `--tip`, each naming one local branch or jj bookmark (`--base` may also name a remote-tracking branch or tracked remote bookmark). No default, no heuristic stack discovery, no use of reference names as proof of relationship. Resolve each to exactly one commit through native adapters; anything else refuses (`stack-ref-invalid`, `stack-ref-ambiguous`).
- **FR-003 (Tier 1)**: Git and jj are supported peers for every capability of the **Capability table**. A jj-managed workspace, including colocated and secondary workspaces, uses jj; unsupported, ambiguous or GitButler-managed ownership refuses with the existing diagnostics; raw Git is never a fallback for jj. Capability gaps are stated in the result, never filled by a different mechanism.
- **FR-004**: Compute the local stack from the commits reachable from the tip and not from the base, bounded by **LIM-2**. The range must be linear (one parent per commit) and connected to the base; layers are the local branches or bookmarks whose tips are in the range, ordered by ancestry; the tip is the top layer. Merge commits, a missing common base, jj divergent changes or conflicted bookmarks make the topology ambiguous or unsupported and refuse; they are never flattened.
- **FR-005**: Without `--pull-requests` perform no network access and no authentication; local evidence only, from the repository's already present objects and remote-tracking refs. Never fetch. State that evidence may be stale and when it was sampled.
- **FR-006**: Produce the findings of the **Findings table** from the observations. Every finding has `code`, `severity`, `confidence`, `evidence` (source ids and fields), a fixed-template `summary`, a `detail` object and, for `unknown` confidence, a `reason`. `unknown` never becomes absence. The Findings table is the only place where severity, confidence and reasons are decided.
- **FR-007**: With `--pull-requests`, read only the endpoints of the **Remote sources table**, by fixed `GET` requests to validated paths on the selected API origin, through the Spec 022 bounded transport only. No GraphQL, no write method, no response-provided URL, no caller-provided endpoint, no subprocess, no `gh` extension. Repository selection, host selection and credentials follow Spec 020 and Spec 022 unchanged; local resolution happens before any network access.
- **FR-008**: Link a layer to a pull request only through the **Linkage rule**: same selected repository, head branch name equal to a layer reference name, and the pull request unique among open candidates (or among merged candidates when none is open). The link is `verified` only when the pull request head commit equals the layer tip commit; otherwise it is `name-matched`. A name match is never treated as verification (consistent with Spec 016 FR-001).
- **FR-009 (no duplication of GitHub)**: Do not create, link, rebase, retarget, merge or unstack anything, do not choose or enforce a merge method, do not explain mergeability (Spec 025), and do not propose a local `rebase` step for a rewrite that GitHub performs on a native stack. The **Delegation table** states which behavior belongs to GitHub, to `gh stack`, or to another spec.
- **FR-010**: `stack plan` prints an ordered, advisory plan under the **Plan rules**: steps of kind `fetch`, `rebase`, `link` and `observe`, each with its effect class, expected commit ids and `argv`. The plan contains a `blocked` reason instead of steps from the first layer it cannot safely describe. `stw` never starts a printed command; a plan is not authorization (constitution II).
- **FR-011**: Enforce the **Limits table**. Exhausting a bound makes the result `incomplete` with `stack-limit`; a single oversized remote response fails that source; neither is ever a complete success.
- **FR-012**: Resample the references (Git) or the pinned operation (jj) and, with `--pull-requests`, the pull requests of all layers' heads, after all other reads. Any difference makes the result `incomplete` with `stack-changed`. The result is sampled evidence, not a transaction.
- **FR-013**: Provide understandable human output and a separate `schemas/v1/stack.schema.json` result with `schema_version: 1` rendered from one result; all existing published contracts are unchanged. Exit status follows the **Status and exit table**.
- **FR-014**: Treat all repository, jj and GitHub text as untrusted data: bounded, valid UTF-8, escaped for terminal rendering, never used as an endpoint, option, command or credential. A printed command is built only from an argv whose elements pass the **Argument rules** (a ref name is passed after `--` or as a full commit id where the tool allows, and the display form is single-quoted); otherwise the step is unprintable with reason `unsafe-name`.
- **FR-015**: Disclose effects: local reads by the isolated Git runner of `internal/inspection` (explicit `--git-dir`/`--work-tree` after the ownership check, isolation prefix, process allowlist in [plan.md](plan.md)) and by the jj session pinned to one operation with `--ignore-working-copy`; network reads of the Remote sources table only with `--pull-requests`; no write to Git, jj, files, index, remote, pull request, stack or settings; no jj snapshot or operation-log change; no hook, filter, textconv, external diff, credential helper beyond Spec 022's gh token lookup, editor or pager.
- **FR-016**: Require failing behavior tests first, independent review, conformance on the existing Git minimum and current lanes and the jj lanes with effect sentinels, in-process fake HTTP for remote evidence, offline schema fixtures and no live network in acceptance. Only `ubuntu-24.04` amd64 is a CI lane; other platforms are local evidence. A missing backend or version never counts as a pass.
- **FR-017**: Add no dependency. The CLI renders and never starts a process or opens a connection; `scripts/check-architecture.py` needs no exception.

### Commands table (normative)

| Command | Flags | Result `mode` |
| --- | --- | --- |
| `stw stack show [PATH]` | `--base REF` (required), `--tip REF` (required), `--pull-requests`, `--repo OWNER/NAME`, `--host HOST`, `--remote NAME`, `--json` | `show` |
| `stw stack plan [PATH]` | the same flags and `--after-fetch` | `plan` |

`PATH` defaults to the current directory. `--repo`, `--host` and `--remote` are valid only with `--pull-requests` and follow Spec 020 (`--host` defaults to `github.com`; `--repo` never overrides ownership). `--after-fetch` (plan only) declares that the caller has just fetched; it is not verified and changes how a missing object is reported (Plan rules group 2). Any other combination, including `--after-fetch` with `show`, is a usage error (exit 2, no JSON).

### Capability table (normative)

| Capability | Git | jj |
| --- | --- | --- |
| Range, layers, fork point, fork-point refs | `rev-list`, `for-each-ref --contains/--merged` | revset reads of the pinned operation |
| Upstream relation of a layer (`head-differs-from-upstream`) | configured upstream, offline | tracked remote bookmark, offline |
| Patch equivalence in the base (`layer-applied-in-base`, `layer-partially-applied`) | `git cherry` (patch ids) | **gap**: reported as `capability-unavailable` (`jj-capability-gap`); the pull request evidence of `parent-merged-child-stale` supplies the same conclusion with `--pull-requests` |
| Conflicted, immutable, divergent | not applicable | `layer-conflicted`, `layer-immutable`, refusal for divergence |
| Layer checked out elsewhere (plan block) | `git worktree list` | not applicable (jj rebases working-copy commits itself) |
| Printed rewrite | `git rebase --onto` per layer | `jj rebase -s` one step |
| Remote evidence | backend independent | backend independent |

### Findings table (normative)

`severity`: `action` (a step by the maintainer is indicated), `notice` (a difference worth knowing), `info`. `confidence`: `observed` (read directly from successfully read, shape-valid data at the sampled state), `inferred` (derived by a rule written below from observed values), `unknown` (not determinable, with a reason). Findings are ordered as listed, then by layer index, then subject.

| Code | Needs | Severity, confidence | Rule |
| --- | --- | --- | --- |
| `stack-landed` | local | `info`, observed | The tip is an ancestor of, or equal to, the base. No layer is reported. |
| `stack-behind-base` | local | `notice`, observed | The fork commit of layer 1 is not the base commit; `detail.behind_by` is the number of base commits not in the stack. |
| `layer-ref-multiple` | local | `notice`, observed | More than one branch or bookmark names a layer tip; all names are listed (at most 8, else `stack-limit`). |
| `fork-point` | local | `notice`, observed | Another local branch (not in the stack) contains a layer tip other than by being in the stack; `detail.refs` lists at most 20, `detail.omitted` counts the rest. Rewriting that layer would leave those refs on the old commits. |
| `head-differs-from-upstream` | local | `notice`, observed; `unknown` when the upstream object is absent (`object-not-local`) | The layer's configured upstream (tracked remote bookmark on jj) is present and its commit differs; `detail.relation` is `ahead`, `behind` or `diverged`. No upstream configured: no finding. |
| `layer-applied-in-base` | local, Git | `action`, observed | Every commit of the layer is patch-equivalent to a commit in the base side (`git cherry` marks all `-`). |
| `layer-partially-applied` | local, Git | `notice`, observed | Some but not all commits of the layer are patch-equivalent; the plan blocks here. |
| `capability-unavailable` | local, jj | `info`, unknown, reason `jj-capability-gap` | Backend jj, subject `patch-equivalence`: emitted once whenever a stack has at least one layer. |
| `layer-conflicted` | local, jj | `action`, observed | A commit of the layer has a stored conflict. |
| `layer-immutable` | local, jj | `notice`, observed | A commit of the layer is immutable; the plan blocks here. |
| `layer-checked-out-elsewhere` | local, Git | `notice`, observed | The layer's primary branch is checked out in another linked worktree (so `git rebase` of it from here would fail or disturb that worktree); `detail.worktrees` lists at most 8 paths; the plan blocks here. |
| `pull-request-missing` | remote | `notice`, observed | The candidate list for the layer's reference names is complete and empty. A candidate list that was not read completely gives `unknown` (reason from the source). |
| `pull-request-ambiguous` | remote | `notice`, observed | After the Linkage rule, more than one open (or, when none is open, more than one merged) candidate remains, or a request returned a full page of five (`detail.truncated` true; the result is also `incomplete` with `stack-limit`); all numbers are listed, none is chosen. |
| `pull-request-closed-unmerged` | remote | `notice`, observed | The only candidates are closed and not merged. |
| `head-sha-differs` | remote | `notice`, observed; only for layers whose linked pull request is **open** (a merged or closed pull request's head may be unreachable once its branch is deleted); `unknown` when ancestry cannot be decided locally (`object-not-local`) | The linked pull request head commit differs from the layer tip; `detail.relation` is `local-ahead` (remote head is an ancestor of the local tip), `remote-ahead` (local tip is an ancestor of the remote head), `diverged` (neither; this is what a server-side rebase of a native stack member gives, M3) or `unknown` (a commit is not local), from local ancestry of the two commits. |
| `base-mismatch` | remote | `notice`, observed; `unknown` with `trunk-ref-unknown` | The pull request base ref differs from the expected one: the parent layer's head ref while the parent pull request is open, otherwise the trunk ref of `--base`. A merged parent makes the trunk ref expected. |
| `stack-link-missing` | remote | `action`, observed | At least two adjacent layers have open linked pull requests chained by base ref and at least one of them has no `stack` object; none of them contradicts another (`stack-split` takes precedence). |
| `stack-split` | remote | `notice`, observed | Linked layers belong to two or more different native stack numbers; the numbers are listed. |
| `stack-member-not-local` | remote | `notice`, observed | A native stack read from `stacks/N` lists an **open** pull request with no linked local layer (merged or closed members are history, not drift); numbers listed, at most 32. |
| `stack-order-differs` | remote | `notice`, observed | The native stack positions of linked layers are not increasing in local layer order. |
| `native-stack-managed` | remote | `info`, observed | A linked pull request carries a `stack` object; `detail` has number, position and size. GitHub retargets and rebases native stack members on merge (measured); `plan` never proposes a local rebase for that rewrite. |
| `parent-merged-child-stale` | remote | `action`, `inferred` (observed when `layer-applied-in-base` already proves rebase) | The pull request of layer k is merged, layer k's commits are still in the range (not contained in the base commit), and k+1 exists. `detail.parent_merge` is `rebase` (patch equivalence), `merge` (merge commit with two parents) or `rewritten` (merge commit has one parent: rebase or squash, not distinguishable; a squash of a single-commit layer is patch-equivalent and therefore `rebase`); `detail.native_stack` is the linked pull request's `stack` presence. A parent merged with a merge commit that the base already contains produces no finding. |
| `parent-merge-not-local` | remote | `notice`, unknown, reason `object-not-local` | The pull request of layer k is merged and its `merge_commit_sha` is not an object in the repository; the plan starts with `fetch`. |

### Linkage rule (normative)

A layer's candidates are the pull requests from the `pull-requests-by-head` source for each of the layer's reference names, restricted to head repository equal to the selected repository (fork heads are not read; disclosed). One candidate per layer is chosen: the single open one; if none is open, the single merged one; if none, the single closed one; otherwise the layer is ambiguous or missing. A request that returns five records (a full page) has an incomplete candidate list: the layer is `pull-request-ambiguous` with `truncated` true, no candidate is chosen, `pull-request-missing` is never reported for it, and the result is `incomplete` with `stack-limit`. `link` is `verified` when `head.sha` equals the layer tip commit, else `name-matched`. Findings that compare commits (`head-sha-differs`, `parent-merged-child-stale`) use the linked pull request in both cases and carry the link quality in `detail`.

### Remote sources table (normative)

Paths are relative to the selected API origin; `O`, `N` are validated owner and name; `REF` a validated layer reference name (grammar of **Argument rules**); `K` a stack number from a pull request `stack` object. Each row is a fixed `GET`. Outcomes: `ok`, `denied` (class `unauthorized` or `forbidden`), `not-found`, `rate-limited`, `failed`, `skipped`; failure classes as in Spec 025 FR-007 once implemented, and until then the Spec 022 errors (all non-success statuses are `failed` with reason `source-failed`).

| Source id | Request | Fields used | Measured |
| --- | --- | --- | --- |
| `pull-requests-by-head` | `repos/O/N/pulls?head=O:REF&state=all&per_page=5` (one request per reference name, at most 20) | per item `number`, `state`, `merged_at`, `draft`, `head.ref`, `head.sha`, `head.repo.full_name`, `base.ref`, `base.sha`, `merge_commit_sha`, `stack.{number,position,size}` (absent when unstacked) | list items carry `stack` (M4); the `head=` filter measured (M4a); refs containing `/` are unverified |
| `native-stack` | `repos/O/N/stacks/K` (one per distinct `K`, at most 8) | `number`, `open`, `base.ref`, ordered `pull_requests[].{number,state,merged_at,head.ref,head.sha,base.ref,base.sha}` | M5 (closed three-member stack); an open stack is unverified |
| `pull-requests-resample` | the `pull-requests-by-head` requests again | as above, compared with the first read | derived |

`pulls/NUM` is not read: list items carry everything used. The `stacks` list (`repos/O/N/stacks`) is measured (M5) but not used in this slice: layer membership comes from the pull requests' own `stack` objects, which avoids paging a repository-wide collection.

### Delegation table (normative)

| Behavior | Owner | Where `stw` stands |
| --- | --- | --- |
| Create, extend or dissolve a native stack (`gh stack submit`, `link`, `unstack`) | GitHub, through `gh stack` semantics | prints the `link` line as an advisory step; a later Spec 016 work item may perform it through the bounded transport after measuring the write endpoints |
| Retarget and rebase native stack members after a merge, atomic stack merge | GitHub. Measured (M3) but confounded: the spike had head deletion on; the timeline puts the child retarget before the head deletion and the rebasing force-push after it, which suggests native behavior, while a deletion-triggered retarget of a plain chain is unmeasured | observes; `native-stack-managed`; never a `rebase` step; `fetch` and `observe` only while an object is missing, otherwise one describing `observe` (`take-upstream` or `native-rewrite-pending`) |
| Choose or enforce the merge method, branch protection, merge queue | the maintainer and GitHub | never decides; Spec 025 explains waiting conditions |
| Local rewrite of a non-native chain or jj stack after a parent landed | the maintainer, with the printed plan | prints; Spec 015 may later execute with recovery evidence |
| Conflict prediction | Spec 014 | not predicted: a merge simulation writes objects (M10), so a real rebase reveals conflicts |
| Push, pull request creation, base edits, comments, force-push | Spec 016 | out of scope |

### Plan rules (normative)

Only `stack plan` computes `plan`; `show` leaves it `null`. Group 1 first finds the lowest blocked layer; groups 2 to 6 then apply to the layers below it (all layers when none is blocked). Of groups 2, 3, 4 and 5 the first applicable decides. Group 6 is evaluated only when groups 2 to 5 produced no step at all.

**Landed layers.** Group 3 is evaluated before landing: a merged pull request whose `merge_commit_sha` is a local object that is **not** an ancestor of the `--base` commit means `--base` is stale, whatever the merge kind, and no layer is dropped, even one that is patch-equivalent (the stale-base group wins). Otherwise a layer is landed when (a) Git: `landed` is `applied` by patch equivalence (its patches are in the base side by construction), or (b) its linked pull request is merged and `merge_commit_sha` is a local object that is an ancestor of the `--base` commit. A merged pull request whose `merge_commit_sha` is not a local object is group 2. The dropped layers are a contiguous prefix 1..j of landed layers; kinds (`parent_merge`) are `rebase`, `rewritten` and `merge` as in the Findings table and only describe how the layer landed. A landed layer above a not-landed layer blocks the plan with `merged-layer-not-bottom` and `blocked.layer` is the lowest landed layer that sits above a not-landed one.

| Group | Condition | Steps |
| --- | --- | --- |
| 1 Blocked | a layer checked out in another Git worktree (`layer-checked-out-elsewhere`), `layer-partially-applied`, `layer-conflicted`, `layer-immutable`, a layer with `pull-request-ambiguous`, a non-prefix landed set, or an unprintable name | `plan.blocked = {layer, reason}`; no step for that layer or above |
| 2 Fetch first | an object needed for a comparison is missing and a fetch can plausibly supply it: `parent-merge-not-local` (merged layer), `head-sha-differs` with relation `unknown` (reason `object-not-local`) on an **open** native-stack layer, or an upstream relation `unknown` (the upstream object is absent) | Without `--after-fetch`: `fetch` (`git fetch REMOTE` or `jj git fetch --remote REMOTE`), then `observe` with note `run-again`. With `--after-fetch` the object is declared unfetchable: one terminating `observe` with note `object-unreachable` (reason `object-not-local`), no `rebase` and no `link`. Heads of merged or closed pull requests are never required. |
| 3 Stale base | a merged pull request whose `merge_commit_sha` is a local object not contained in the `--base` commit (any merge kind) while its layer's commits are still in the range | `observe` with note `stale-base`; no `rebase` and no `link` step |
| 4 Reconcile | all objects are local and (a) a native stack's `parent-merged-child-stale` holds, or (b) a layer's `head-differs-from-upstream` relation is `behind` or `diverged` | `observe` only, no `rebase` and no `link`, with a note chosen by evidence in this order: (a) upstream `behind` or `diverged` gives `take-upstream` (GitHub rewrote the head; the described action is to take the upstream, saving local-only commits first), upstream `same` or none gives `native-rewrite-pending` (GitHub lag or a rewrite it could not do; no difference is claimed); (b) upstream `behind` gives `upstream-ahead` (integrate the upstream, which only adds commits, before restacking); upstream `diverged` with the layer tip descending from the `--base` commit gives `push-needed` (the layer was rewritten locally; publishing needs a lease-protected push, a described later effect, Spec 016 L2); otherwise `diverged-undecided`. These notes only describe; they terminate because after a fetch they describe the state instead of asking to run again |
| 5 Rebase | the dropped set is non-empty, or `stack-behind-base` (a moved base needs no pull request evidence) | Git: for each remaining layer from the bottom, `git rebase --onto N O L` with `L` the layer's primary branch, `O` the full commit id of that layer's pre-rewrite parent tip (its sampled `fork_commit_id`) and `N` the trunk ref name (`--base` as given) for the lowest remaining layer, else the previous remaining layer's primary branch. jj: one step `jj rebase -s C -o N`, `C` the change id of the first commit of the lowest remaining layer and `N` the `--base` revision written as a revset literal (Argument rules; a remote-tracking `--base` such as `main@origin` is written `"main"@"origin"`); descendants move with it. The jj step also moves the working-copy commit when it is a descendant of `C` (carrying uncommitted edits, which jj snapshots first) and every other bookmark or commit that descends from `C` even if it is not a stack layer (measured: a bookmark `side`); the Git step moves only the named branch; the effect text says so |
| 6 Link | `stack-link-missing`, and every layer's upstream relation is `same`, `none` or `ahead` | one `link` step: `gh stack link P1 P2 … Pn` (pull request numbers bottom to top), environment `GH_REPO=O/N` (measured: `gh stack link` rejects `-R`); effect `remote-write`; `performed_by: gh-stack`. It pushes unpushed commits of `ahead` layers (fast-forward). It is never printed in the same plan as a `rebase` step, because rewritten heads would need a non-fast-forward push (Spec 016 FR-006 forbids force-push in its first slice): run `stack plan` again after the rewrite and its push |
| none | no condition | `steps` empty, `blocked` null |

Without `--pull-requests` it is unknown whether the stack is a native GitHub stack. `plan.native_checked` is `true` only when the remote section exists, `pull-requests-by-head` is `ok` and every distinct stack number seen was read with `native-stack` `ok`; in every other case, including `--pull-requests` with a failed remote read, it is `false` and the plan carries the boundary limitation "native stack status was not checked; if GitHub manages this stack it rewrites member branches after a merge, so fetch and run `stack plan --pull-requests` before running any rebase step". Every `rebase` step is advisory text that, when run by the maintainer, checks out the layer branch and needs a clean worktree (effect `local-history-rewrite`).

Every `rebase` step records `expected`: the layer tip, the old parent tip, the new parent name and, for the first rebase step only, the base commit id it was derived from (`null` for later steps, whose parent is produced by the previous step and named in `requires`). The plan is advisory and not authorization.

### Argument rules (normative)

A reference name used in a printed command or request must be at most 200 bytes, valid UTF-8, components separated by `/`, each matching `[A-Za-z0-9._-]+` with no leading `.` or `-`, no `..`, no `.lock` suffix. Other names are shown (escaped) in observations but make dependent printed steps and remote requests unprintable or `skipped` (`unsafe-name`). In printed jj commands a name is a revset, not a plain argument: a trailing `-` (allowed by the grammar above) is an operator there (measured: unquoted `s1-` fails, or silently names the parent revision when a bookmark `s1` exists), so every name is written as a double-quoted revset string literal (`"name"`, which the grammar above makes free of quotes and backslashes; measured to resolve for `"s1-"`, `"a-b"` and `"feat/x"` on both pinned versions). `+` is not in the grammar, so a name containing it is `unsafe-name`. Change ids are written as given by jj. A tracked remote bookmark used as `--base` is written `"name"@"remote"` (measured on both pinned versions: `main@origin` and `"main"@"origin"` resolve, `"main@origin"` fails with "Revision doesn't exist"). Unquoted, a name followed by `-` that also exists as a bookmark silently means the parent of that bookmark (measured), not an error. Printed Git `rebase` commands use full 40-hex (SHA-1) or 64-hex (SHA-256) commit ids for `O` and `--` is not needed because names are validated.

### Limits table (normative)

| Id | Bound |
| --- | --- |
| LIM-1 | 30 seconds total; 5 seconds per Git or jj process; cancellation honoured |
| LIM-2 | at most 500 commits in the range and 32 layers (the GitHub maximum stack size is unverified; the bound is ours) |
| LIM-3 | at most 20 reference names resolved to pull requests (more is `stack-limit` for the remote part only) and at most 8 distinct native stacks, so that two reads of every name plus the stack reads (2 x 20 + 8 = 48) stay within LIM-4 |
| LIM-4 | at most 50 remote requests and 1 MiB per response. The fixed shapes of the Remote sources table are single requests (`per_page=5`, no paging); a full page is handled by the Linkage rule, and page and record limits apply only to a future paged shape |
| LIM-5 | at most 400 findings, 64 plan steps, 8 reference names per layer, 20 fork-point refs |
| LIM-6 | result at most 1 MiB; each Git or jj process output is read with the runner's scan allowance (the 64 KiB retained-output cap of the shared runner is not relied on) |

### Status and exit table (normative)

| `status` | Diagnostic codes | Meaning | Exit |
| --- | --- | --- | --- |
| `ok` | none | Every attempted read succeeded; resample unchanged; no bound exhausted. Findings and `unknown` items do not change this. | 0 |
| `incomplete` | `stack-limit`, `stack-changed`, `stack-remote-failed` (a remote source `failed` or was `rate-limited`, or a remote source was `denied` or unavailable, or the repository or credentials could not be selected: the `hosting-repository-selection`, `hosting-invalid-remote`, `hosting-api-unavailable` codes may accompany it) | Local layers and findings are retained; `boundary.complete` is `false`. | 1 |
| `error` | `not-repository`, `ambiguous-backend`, `unsupported-backend`, `jj-sandbox-unavailable`, `jj-timeout`, `git-failed`, `changed-repository`, `invalid-path` (existing meanings); `stack-ref-invalid`, `stack-ref-ambiguous`, `stack-ambiguous-topology`, `stack-no-common-base`, `stack-canceled` | No usable stack; `layers` empty; `boundary.complete` is `false`. | 1 |
| usage error | — | Invalid arguments or flag combination; no JSON. | 2 |

Spec 025 FR-007 treats a denied or not-found optional source as a complete observation; here the remote evidence is the point of `--pull-requests`, so a denied, not-found, rate-limited or failed remote source gives `incomplete` (local part retained) and the findings that needed it are `unknown`. The two specs deliberately differ because 025 answers a question about one pull request while 012 reports a comparison that is incomplete without the remote side.

There is no strict exit status in this slice: findings never change the exit status (open question 4).

### Key Entities

- Stack observation: base, tip, ordered layers with native identities, findings, optional plan and remote section ([data-model.md](data-model.md)).
- Layer: one branch or bookmark tip, its commits, its upstream relation and its linked pull request.
- Finding and plan step: evidence-bearing facts and advisory text only.

## Success Criteria

### Measurable Outcomes

- **SC-001**: Every acceptance scenario passes for each declared supported backend and version; unsupported cases return explicit capability limits instead of fabricated success.
- **SC-002**: The three merge shapes of the spike (merge commit, rebase-merge, squash) produce the findings and plan stated in User Story 2 on Git and, with pull request evidence, on jj; the merge-commit case produces no drift finding.
- **SC-003**: Fixtures replaying the measured native-stack shapes produce the findings of User Story 3, and no fixture treats a name match as verified or an unreadable source as absence.
- **SC-004**: Effect sentinels prove, for every fixture and both backends, that refs, files, index, loose objects, the jj stored working copy and operation log are unchanged and that no printed command is started; a recorder proves that without `--pull-requests` no request is made and with it only the Remote sources table shapes are requested with method `GET`.
- **SC-005**: Every complete success, incomplete and error fixture validates offline against `stack.schema.json`; every existing published schema and fixture is byte-identical to the base.
- **SC-006**: Hostile-text, limit, moved-reference and cancellation fixtures end in the statuses of the Status table; the printed commands of every fixture contain only arguments that pass the Argument rules.
- **SC-007**: Every functional requirement has a traceable acceptance case before implementation handoff; skipped or missing execution (including an unavailable jj lane) never counts as verified.

## Disposition of the 2026-10-15 draft (normative)

| First-draft item | Disposition |
| --- | --- |
| FR-001, FR-002, FR-004 (explicit base, identities, ambiguity) | kept: FR-002, FR-003, FR-004 |
| FR-005 (no snapshot, checkout change, fetch or external helper) | kept: FR-005, FR-015 |
| FR-006 to FR-009 (Tier 1, human and JSON, limits, acceptance evidence) | kept: FR-003, FR-013, FR-011, FR-016 |
| FR-003, FR-011 (aggregate and per-revision diff summaries, stored versus disk diffs), FR-010, FR-012 (path-filtered changed-file queries, rename and deletion kinds) | **withdrawn from this spec**: GitHub (`gh pr diff`, the files view) and Git or jj (`git diff --stat`, `jj diff --stat`) already provide stored-history diffs per layer; live disk and index observations are Spec 009 queries. A future `stack diff` would need its own specification (open question 1). |
| SC-001 to SC-004 (acceptance per backend, safety fixtures, schema validity, requirement traceability) | kept in substance as SC-001, SC-004, SC-005 and SC-007; SC-002 and SC-003 of this version are new (merge shapes, native-stack fixtures) |
| First-draft US1 acceptance scenarios | kept as User Story 1 |
| First-draft US2 and US4 | withdrawn with the items above |
| First-draft US3 (bounded agent contract) | kept as User Story 4 |

## Scope and Dependencies

This is a read-only slice: local topology plus opt-in GitHub evidence plus an advisory printed plan. It reuses native identity (Specs 005, 008, 009), the isolated Git runner (Spec 021 code), hosting resolution (Spec 020) and the bounded transport (Spec 022).

**Scope split with Specs 015 and 016 (normative)**: Spec 012 owns observation, drift findings, native stack membership reads and the printed plan. Spec 015 owns executing any local rewrite, with recovery evidence, and consumes the 012 observation and plan as input that it must revalidate. Spec 016 owns every remote effect: push, pull request creation and base edits, comments, and the native stack effects `link`, `unstack`, `sync` and merge, through GitHub native stack semantics. Spec 025 owns the explanation of what a single pull request waits for. Specs 015 and 016 carry one amended assumption bullet each ([plan.md](plan.md) lists the exact edits); their requirements are unchanged.

**Ordering**: the Git and jj local adapters and the pure derivation do not depend on any unimplemented spec. The remote work item extends the Spec 022 allowlist additively with two shapes (`pulls?head=` and `stacks/K`); Spec 025 plans its own additive extension of the same files, so the two work items must rebase over each other (plan risk R2). Out of scope, each needing its own specification or work item: applying any printed step, fetch, push, sync, link or unstack execution, native stack merge, conflict prediction, diff and changed-file queries, fork pull requests, GitHub Enterprise Server acceptance and a `gh jj` entry point. Constitution 1.1.0 applies with no amendment: read-only, network reads disclosed and opt-in, Git and jj peers.
