# Implementation Plan: Stack inspection and plan (v2)

> **Owner-decision amendment — 2026-10-25:** all former OQ IDs are decided;
> [current profiles](../016-stacked-pr-coordination/contracts/owner-decisions.md) supersede earlier undecided recommendations.
> Only --online opts into reads; single repository now, batch deferred.
> This is a documentation target, not implementation or native acceptance.

**Branch**: `codex/spec-012-stack-toolbelt` | **Date**: 2026-10-25 | **Spec**: [spec.md](spec.md)

## Summary

Add `stw stack show` and `stw stack plan`: one shared, read-only operation that resolves an explicit base and tip through the native Git or jj adapter, derives the linear layers between them, produces drift findings through a pure, table-driven function, and, only with `--online`, reads the pull requests and native stacks of the layers through the Spec 022 bounded transport. `plan` adds an advisory, print-only list of steps. The result is one typed value rendered as text or as the new `stack` JSON schema. No existing contract changes; nothing is written, fetched, pushed, linked or executed. The optional later effects are catalogued in [contracts/later-effects.md](contracts/later-effects.md): local apply belongs to Spec 015, selected forge effects to Spec 016; historical native-stack merge remains unscheduled, and Spec 043 landing is a separate human-only operation.

**Delivery status**: design refresh only. `stack show|plan` and their schema are unimplemented; Spec 015's hidden Git `restack plan` v1 is separate implemented source, not evidence for this feature. New JSON shapes here and in the contract are UNPUBLISHED Markdown proposals. No JSON file is changed by this refresh.

## Technical Context

**Language/Version**: Go 1.27.2; existing supported Git (minimum 2.46.0, current 2.56.0) and jj (minimum 0.45.1, current 0.46.0) lanes of `scripts/backend-releases.json`; `gh` >= 2.102.0 for authentication only, and only with `--online`.

**Primary Dependencies**: Existing Cobra v1.10.2, `github.com/cli/go-gh/v2` v2.16.2 (Spec 022), the standard library and the existing JSON Schema test dependency. No new dependency. `gh stack` is never started.

**Storage**: No stack store or HTTP cache. Offline evidence is limited to existing native objects and remote-tracking refs, with sampled freshness. Explicit `--online` reads are bounded and uncached.

**Testing**: Go table tests for the pure derivation and plan builder; real temporary Git and jj repositories for the local adapters (ordinary, linked worktree, colocated, secondary workspace); in-process fake HTTP through a `StackWithTransport`-style seam for remote evidence; schema fixtures; effect sentinels; a smoke extension; existing architecture and documentation checks.

**Target Platform**: Linux amd64/arm64, darwin arm64 as for the existing commands. CI evidence exists for ubuntu-24.04 amd64 only; other results are local evidence.

**Project Type**: CLI with a shared operation and a thin command layer.

**Performance Goals**: LIM-1 to LIM-6 of the spec (30 seconds, 500 commits, 32 layers, 50 requests, 1 MiB).

**Constraints**: Read-only, no fetch, no process other than the allowlist below, no network without `--online`, fixed GET only, closed endpoint allowlist, hostile text, no executed or unprintable-unsafe command.

**Scale/Scope**: Exactly one selected repository, base and tip, at most 32 local layers. OQ-FANOUT is decided single-repository now: no batch CLI/caller/API or unused helper; earlier many-repository invariants are deferred to a separate future decision.

## Constitution Check

Author design assessment: aligned with constitution 1.2.0; no implementation or independent acceptance is claimed. Historical analysis clearance applies only to its recorded old revisions. The dated owner decisions settle network/flag vocabulary, target exit revision and single-repository admission; affected surfaces require implementation and measured conformance, not additional answers.

- **I. Shared contract**: one typed result renders text and versioned JSON; no prompt, editor or pager; exit 0, 1, 2 with a stated mapping.
- **II. Evidence before actions**: findings carry confidence and evidence; the plan is explicit about targets, reasons, expected commit ids and effects, is advisory, and authorizes nothing; later apply must revalidate (Spec 015). The plan never proposes a rewrite that GitHub performs.
- **III. Ownership and recovery**: read-only; native ownership is verified before any explicit-`--git-dir` call (Git skips its own check then); linked worktrees are respected (`layer-checked-out-elsewhere` blocks the plan); no recovery claim is needed because target repository state does not change. Private sandbox setup outside the target remains the existing disclosed adapter effect.
- **IV. Specify and verify**: failing tests first; real temporary Git and jj repositories; hostile names, text and limits; moved references and concurrent changes (resample); effect sentinels including jj working-copy and operation-log checks and a loose-object check; no live network in acceptance.
- **V. Tier 1 peers**: the same operation on both backends; the documented jj gaps (patch equivalence and detecting deleted landed-layer bookmarks) remain explicit; requested PR/native-stack evidence can strengthen the observation without claiming it closes the unguarded plain-chain limitation; no raw Git for jj.
- **Scope**: network reads are opt-in and disclosed; push, merge, link and other remote effects remain later specifications. No amendment or exception is required. The Scope sentence "push/merge/release automation ... require later specifications" is respected: a printed `gh stack link` line is text.

## Project Structure

```text
internal/inspection/stack.go              # options, orchestration, resample, limits, result assembly
internal/inspection/stack_result.go       # result types, constants, status and diagnostic mapping
internal/inspection/stack_git.go          # Git range, layers, upstream, cherry, worktrees (isolated runner)
internal/inspection/stack_jj.go           # jj bookmarks and range on the pinned operation
internal/inspection/stack_findings.go     # pure derivation: observation -> findings (the Findings table)
internal/inspection/stack_plan.go         # pure plan builder, argument rules, quoting
internal/hosting/stack_remote.go          # opt-in sources: pulls by head, native stacks; linkage rule
internal/hosting/transport.go             # additive: two allowlisted request shapes
internal/cli/stack.go                     # command group, flags, text rendering, injection seam
schemas/v1/stack.schema.json              # new contract
internal/cli/testdata/stack-*.json        # schema-valid fixtures
internal/contracts/stack_test.go          # fixtures against the schema, drift guard, unchanged bytes
scripts/smoke-stack.py                    # local-only binary smoke on temporary repositories
docs/stack.md, docs/json-contracts.md, docs/agent-guide.md, docs/commands.md, ARCHITECTURE.md,
docs/README.md, llms.txt, docs/progress.md, docs/roadmap.md
```

`internal/cli` renders and never starts a process or opens a network connection. The result types, including the remote section, are plain data in `internal/inspection/stack_result.go`; `internal/hosting` (which already imports `internal/inspection`) exports `ExplainStack`/`ExplainStackWithTransport`, which call the local operation and, with `--online`, add the remote section. The CLI imports only these reviewed packages, so `scripts/check-architecture.py` needs no exception (its rule allows `internal/cli` to import `internal/inspection` and `internal/hosting`; read in this revision).

## Process allowlist (normative)

Every Git invocation runs through the Spec 021 isolated runner (`pinsRunner`, `pinsIsolation()`, `pinsEnvironment()`; explicit `--git-dir` and `--work-tree` only after the ownership validation of `pins_safe.go`; per-call timeout and scan allowance; stdin empty). The allowed commands, each read-only (M9 to M11):

- `rev-parse --verify --quiet refs/heads/NAME^{commit}` (and `refs/remotes/R/NAME^{commit}`), for the base and the tip at the first read and again at the resample.
- `rev-list --topo-order --parents --max-count=501 BASE..TIP --`, and `rev-list --count --max-count=10001 FORK..BASE --`.
- `for-each-ref --format=<fixed> --contains BOTTOM --merged TIP refs/heads` (layer references), `for-each-ref --contains LAYERTIP --no-merged TIP refs/heads` (fork-point references, one call per layer, results bounded to 21 names), and, per layer reference, `for-each-ref --format=<fixed with %(upstream) %(upstream:track)> refs/heads/NAME`, unpeeled `rev-parse --verify --quiet UPSTREAMREF` and then `UPSTREAMREF^{commit}` (the upstream commit id; exit 1 of the unpeeled read is `gone`, a present ref whose object is absent is `unknown`; `%(upstream:track)` alone cannot tell them apart), and `rev-list --count --max-count=10001 UPSTREAM..TIP --` with `TIP..UPSTREAM` (the counts of the upstream relation, merge commits included).
- `merge-base --is-ancestor A B` (landed tip, upstream relation, pull request head relation).
- `cherry BASE TIP` (no `-v`; commit ids and marks only; it omits merge commits, so it is used only for the applied-layer detection and never as proof for any advice).
- `worktree list --porcelain -z` (checked-out-elsewhere).
- `rev-list --no-walk --parents --format=%s IDS` for bounded subjects, and `rev-list --parents -n1 SHA` for a merge commit's parent count.
- `cat-file -e SHA^{commit}` (object existence by exit status; no object content is read).

Not used: `status`, `diff`, `log`, `merge-tree`, `rebase`, `fetch`, `ls-remote`, `config` writes, `submodule`, anything that reads working-tree content. jj: only the pinned-operation session of Spec 008 (`--no-pager --color=never --ignore-working-copy --at-operation=OP`), `bookmark list --all-remotes --template …` and `log --no-graph -r REVSET --template …` with fixed templates; no `rebase`, `git fetch`, `new`, `edit` or any command that snapshots.

## Design Decisions

1. **Pure derivation core.** The adapters gather one `stackObservation` (refs, range, layers, upstreams, cherry marks, worktrees, jj flags, optional remote evidence with each source's outcome). `deriveFindings(observation)` and `buildPlan(observation, findings)` are pure and implement the spec tables row by row, so every row and every permission combination is table-tested without Git, jj or HTTP. A test iterates the Findings table codes and fails if a code has no test row; another fails if `action` is ever `unknown` or an `unknown` finding lacks a reason.
2. **One adapter interface, two implementations.** `stackSource` has `Resolve`, `Range`, `Layers`, `Upstreams`, `Equivalence` (Git only; jj returns the capability gap), `Worktrees`, `Resample`. The orchestration is backend-neutral; ownership selection reuses `Inspect`'s identity so a jj workspace never reaches the Git adapter.
3. **Isolated runner reuse without renaming.** The Git adapter calls `newPinsRunner` through a thin wrapper; the Spec 021 code is not moved or renamed in this slice (its work-item branches are in flight; a rename can follow separately). Characterization of the wrapper is the first task of the Git adapter.
4. **Layer algorithm.** Range R = commits reachable from TIP and not from BASE, at most 500, one parent each, bottom parent reachable from BASE (else `stack-no-common-base` for a root commit or `stack-ambiguous-topology` for a merge). Layer references are the local branches whose tips are in R; refs with the same tip form one layer. Layer 1's fork commit is the bottom commit's parent; `stack-behind-base` compares it with the base commit. Fork-point references are other local branches that contain a layer tip. Details and counts are bounded by LIM-2 and LIM-5.
5. **Remote evidence is a separate, optional phase.** After the local phase, `--online` resolves the hosting repository exactly as Spec 020 does (before any network access), reads `pulls?head=` per reference name, derives the distinct stack numbers, reads each `stacks/K`, then applies the Linkage rule. A failure of any remote step keeps the local result and yields `incomplete` (`stack-remote-failed`), never an error.
6. **Linkage and comparison.** Linkage uses names only to find candidates and the head commit to qualify (`verified`/`name-matched`). Comparisons use local ancestry of the commits when objects exist (`object-not-local` otherwise). Parent-merge kinds follow the spec: `rebase` from patch equivalence (Git), `merge` from a two-parent merge commit, `rewritten` otherwise. A layer is landed only by patch equivalence or by a merged pull request whose `merge_commit_sha` is a local ancestor of `--base`; otherwise the stale-base or fetch groups apply.
7. **Plan builder.** Every inert Git replay argv includes the fixed `-c rebase.updateRefs=false -c rerere.enabled=false` overrides before `rebase --onto`, irrespective of repository config; Spec 015 D7 still executes no porcelain replay. Steps follow the six rule groups of the spec (blocked, fetch first with the plan-only `--after-fetch` terminator, stale base, reconcile with evidence-based notes, rebase, link) with the stated precedence; the landed set is a contiguous prefix (`merged-layer-not-bottom` otherwise, blocked at the lowest landed layer above a not-landed one). `old_parent_tip` is the sampled fork commit of the layer (a full object name); the new parent is the trunk ref for the lowest remaining layer and the previous remaining layer's primary branch afterwards. jj produces one step for the whole remainder, with names as revset literals and `-o` for the destination. A native stack or an upstream that is behind or diverged never gets a `rebase` or `link` step: it gets `fetch` (only for a missing object that a fetch can supply) or a terminating `observe` whose note is chosen by evidence (`take-upstream`, `native-rewrite-pending`, `upstream-ahead`, `diverged-undecided`, `object-unreachable`); no note advises rebasing onto the upstream or any action that can drop another person's commit. Every `link` argv includes `--base` with the known validated `base.trunk_ref`, recording that trunk in `expected`; an unknown trunk blocks at layer 1 with `trunk-ref-unknown`, an unsafe trunk with `unsafe-name`, and no GitHub default is substituted. `link` is never printed beside a `rebase`. Offline plans have `native_checked: false` and the boundary limitation. A name failing the Argument rules makes the plan `blocked` with `unsafe-name`; no partially quoted step exists. Display strings quote each element with single quotes unless it matches `[A-Za-z0-9._/=:-]+`; an element containing a single quote or a control byte is unprintable by construction.
8. **Transport extension is additive.** The Spec 022 closed allowlist gains exactly `repos/O/N/pulls?head=O:REF&state=all&per_page=5` and `repos/O/N/stacks/K`. Spec 025 plans its own additive shapes and typed failure classes in the same files; whichever work item lands first owns the shared change (typed failure class) and the other rebases. Until typed classes exist, every non-success status is `failed`/`source-failed`. The guard keeps refusing non-GET, `RawPath`, userinfo, fragments, redirects, extra or reordered query pairs; the ref grammar is enforced by the endpoint builder.
9. **Resample.** After all other reads the references (Git: the base, the tip and every layer reference; jj: the operation id and the same bookmarks) are read again, and with `--online` the `pulls?head=` requests are repeated; a difference is `stack-changed`.
10. **Schema.** `stack.schema.json` is self-contained; `if/then` per-code `detail`; identity definitions copied and drift-guarded; every existing schema and fixture compared byte for byte against the base in the tests.
11. **Command layer.** `internal/cli/stack.go` registers `stack show|plan`; the injection seam mirrors `runCLIWithHosting` so CLI tests use the real shared operation with fake HTTP and real repositories, with no production endpoint override.
12. **Exit statuses.** `ok` returns normally; `incomplete` and `error` return `runtimeFailure` (1) after the full result is written; usage errors exit 2 with no JSON.
13. **No polling, no retry.** Server-side rebases by GitHub are eventually consistent (unverified); the result is a sample and `observe` tells the caller to run again.
14. **Conformance.** Local conformance on real native Git (ordinary and linked worktree) and jj (standalone, colocated, secondary) with before/after sentinels on the minimum and current lanes; remote conformance by fake HTTP in the same fixtures; a binary smoke for local cases. A lane that is unavailable is not a pass.

## Toolbelt reconciliation and delivery boundaries

- Spec 012 supplies stack observation and inert human reference commands. Spec 015 first-slice owner D6 independently selects old parent/new parent/branch; optional later observation consumption revalidates expected ids. Owner D7 plumbing/quarantine/compare-and-swap remains binding. Never parse or execute 012 `argv` as an apply plan, and never turn an advisory `git -c rebase.updateRefs=false -c rerere.enabled=false rebase --onto` into a porcelain executor.
- Spec 014 owns isolated integration preflight/replay evidence; observation never runs a simulation in the original repository. Spec 013 owns checkpoint/restore/selective undo and reuses Spec 032 durability/recovery conventions; 012 captures no checkpoint and promises no undo.
- Spec 016 owns separately authorized forge effects and exact lease evidence. After actual Spec 015 local apply, Spec 016 may render a blocked plan; before push it independently applies the actual-head/change-class policy in [the downstream handoff](data-model.md#optional-downstream-evidence-handoff), including the docs-only pre-push exemption and exact-new-SHA code review. Ready still requires exact-head independent CLEAR, required green CI and rebaseability. A predicted fixed-committer quarantine SHA or earlier review never substitutes for the applicable actual-head evidence. Owner resync text is inert, names its destructive limits and is never run by an agent.
- Spec 041 ownership/mode and Spec 042 attribution are effect-owner gates, not inferred from PR names, config identity or stack membership. Spec 043 is a separate human-only landing invocation; no 012/015/016 path invokes it or pushes the default branch.

15. **Advisory depth and stale-parent signal.** Report retained local `depth` separately from complete candidate `open_depth`, excluding only proven-landed layers under the landing/stale-base rules. Emit inferred `stack-too-deep` only for candidate open_depth >2, never for the depth-3/open-depth-2 applied-parent example. Unknown landing stays a qualified candidate upper bound; incomplete/error open depth is null. Keep the independent 32-local-layer cap. `restack-advised` uses qualified parent/patch evidence and explicitly labels unchecked native status. Closed-unmerged parents never become landed; stale child bases use `base-mismatch`. These findings never override the six plan groups or confer effect authority.
16. **Pure core and single-repository boundary (owner decision).** Keep immutable pure derivation and bounded indexed independent reads in adapters. Current operation admits one explicit repository only; reject batch/multi-repository/discovery before workers, auth or network. No stack_batch.go, new caller API or unused production helper is approved. The former 1..16/four-worker proposal is deferred future design requiring a later decision, not current acceptance.
17. **Event/finding namespace.** The existing Spec 035 forge watch event uses top-level kind restack-needed and merge_commit_sha as its new parent; 012’s nested restack-advised finding code has indices/native status and its selected base as first destination. Cross-reference the distinct contracts, never coerce their payloads or execute event argv, and require independent native re-observation before a new effect plan.
18. **Qualified proof.** `cherry BASE TIP` is used only to classify linear-layer patch content; merge omissions and squash/conflict-resolution patch mismatches are disclosed. It is never proof for cleanup, identity, review, semantic validity or lease publication. Exact old/new boundaries are data for optional later plans, not trusted instructions.

## Adopted owner recommendations and deferred work (2026-10-25)

1. Diffs remain withdrawn; Git/jj/GitHub and Spec 009 retain their existing roles.
2. Native link writes belong to 016 only after separately authorized measurements
   and a reviewed bounded 022 allowlist; 012 prints inert reference data only.
3. Network opt-in is ONLY --online, default offline, no --pull-requests alias.
4. Drift stays separate from completion; target exit revision 2 is 0/1/2 and 015
   incomplete becomes 1 only after its code task (CURRENT code 3 still documented).
5. Keep explicit --tip; no unselected derived branch/default is introduced.
6. Fork pull requests remain unsupported/disclosed until native evidence.
7. Keep current 32-local-layer bound; measure GitHub capability before any later
   reviewed bound amendment, never invent a native maximum here.
8. Keep stack-link-missing severity action and its existing qualified evidence.
9. Adopt --human exclusive with --json and shortening positive --timeout;
   no observation store or redundant --offline alias.
10. Single repository now; batch deferred to its own later decision, no approved
    caller/API or dead production helper. Current T037 rejects batch.

## Risks

- **R1** jj parity rests on the reviewers' measurements (M13 to M15) on both pinned versions; T002 re-measures with the task's own scripts before any jj code and may force changes to the Capability table; the contract states the one known gap.
- **R2** Parallel work on `transport.go`/`operations.go` (Spec 025 WI1, any Spec 022 follow-up): the remote work item is last in the order and rebases.
- **R3** The native stack REST resources are new; shapes beyond the measured closed stack are unverified (U4). Every unexpected shape or status degrades to `unknown` or `incomplete`, never to a claim.
- **R4** Patch ids differ when a rebase merge resolved a conflict or a squash combined commits; the layer then looks `pending` locally. The remote evidence (`rewritten`) covers it; the spec never claims a layer is not applied.
- **R5** Server-side rebases lag the merge (unverified): a fresh observation can show the child unrewritten; the plan then prints `observe` `native-rewrite-pending` (GitHub's head equals or is behind the local tip), never a competing `rebase`.
- **R6** Rebase risk with the in-flight Spec 021 work-item branches that touch `pins_run.go`: the adapter uses a wrapper and does not edit those files.
- **R7** `gh stack` is v0.2.1 and changing; the printed `link` line was checked against its `--help` only.
