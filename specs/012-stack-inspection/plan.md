# Implementation Plan: Stack inspection and plan (v2)

**Branch**: `codex/012-stack-inspection-v2` | **Date**: 2026-10-17 | **Spec**: [spec.md](spec.md)

## Summary

Add `stw stack show` and `stw stack plan`: one shared, read-only operation that resolves an explicit base and tip through the native Git or jj adapter, derives the linear layers between them, produces drift findings through a pure, table-driven function, and, only with `--pull-requests`, reads the pull requests and native stacks of the layers through the Spec 022 bounded transport. `plan` adds an advisory, print-only list of steps. The result is one typed value rendered as text or as the new `stack` JSON schema. No existing contract changes; nothing is written, fetched, pushed, linked or executed. The optional effects the owner wants later (link, push, sync, unstack, merge, local apply) are catalogued in [contracts/later-effects.md](contracts/later-effects.md) and belong to Specs 015 and 016.

## Technical Context

**Language/Version**: Go 1.27.2; existing supported Git (minimum 2.46.0, current 2.56.0) and jj (minimum 0.45.1, current 0.46.0) lanes of `scripts/backend-releases.json`; `gh` >= 2.102.0 for authentication only, and only with `--pull-requests`.

**Primary Dependencies**: Existing Cobra v1.10.2, `github.com/cli/go-gh/v2` v2.16.2 (Spec 022), the standard library and the existing JSON Schema test dependency. No new dependency. `gh stack` is never started.

**Storage**: None.

**Testing**: Go table tests for the pure derivation and plan builder; real temporary Git and jj repositories for the local adapters (ordinary, linked worktree, colocated, secondary workspace); in-process fake HTTP through a `StackWithTransport`-style seam for remote evidence; schema fixtures; effect sentinels; a smoke extension; existing architecture and documentation checks.

**Target Platform**: Linux amd64/arm64, darwin arm64 as for the existing commands. CI evidence exists for ubuntu-24.04 amd64 only; other results are local evidence.

**Project Type**: CLI with a shared operation and a thin command layer.

**Performance Goals**: LIM-1 to LIM-6 of the spec (30 seconds, 500 commits, 32 layers, 50 requests, 1 MiB).

**Constraints**: Read-only, no fetch, no process other than the allowlist below, no network without `--pull-requests`, fixed GET only, closed endpoint allowlist, hostile text, no executed or unprintable-unsafe command.

**Scale/Scope**: One repository, one explicit base and tip, at most 32 layers.

## Constitution Check

Pre-research assessment: PASS. Post-design assessment: PASS.

- **I. Shared contract**: one typed result renders text and versioned JSON; no prompt, editor or pager; exit 0, 1, 2 with a stated mapping.
- **II. Evidence before actions**: findings carry confidence and evidence; the plan is explicit about targets, reasons, expected commit ids and effects, is advisory, and authorizes nothing; later apply must revalidate (Spec 015). The plan never proposes a rewrite that GitHub performs.
- **III. Ownership and recovery**: read-only; native ownership is verified before any explicit-`--git-dir` call (Git skips its own check then); linked worktrees are respected (`layer-checked-out-elsewhere` blocks the plan); no recovery claim is needed because nothing changes.
- **IV. Specify and verify**: failing tests first; real temporary Git and jj repositories; hostile names, text and limits; moved references and concurrent changes (resample); effect sentinels including jj working-copy and operation-log checks and a loose-object check; no live network in acceptance.
- **V. Tier 1 peers**: the same operation on both backends; the one capability gap (patch equivalence on jj) is stated in the contract and in every result, and closed by pull request evidence rather than by a different mechanism; no raw Git for jj.
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

`internal/cli` renders and never starts a process or opens a network connection. The result types, including the remote section, are plain data in `internal/inspection/stack_result.go`; `internal/hosting` (which already imports `internal/inspection`) exports `ExplainStack`/`ExplainStackWithTransport`, which call the local operation and, with `--pull-requests`, add the remote section. The CLI imports only these reviewed packages, so `scripts/check-architecture.py` needs no exception (its rule allows `internal/cli` to import `internal/inspection` and `internal/hosting`; read in this revision).

## Process allowlist (normative)

Every Git invocation runs through the Spec 021 isolated runner (`pinsRunner`, `pinsIsolation()`, `pinsEnvironment()`; explicit `--git-dir` and `--work-tree` only after the ownership validation of `pins_safe.go`; per-call timeout and scan allowance; stdin empty). The allowed commands, each read-only (M9 to M11):

- `rev-parse --verify --quiet refs/heads/NAME^{commit}` (and `refs/remotes/R/NAME^{commit}`), once for the base and once for the tip.
- `rev-list --topo-order --parents --max-count=501 BASE..TIP --`, and `rev-list --count --max-count=10001 FORK..BASE --`.
- `for-each-ref --format=<fixed> --contains BOTTOM --merged TIP refs/heads` (layer references), `for-each-ref --contains LAYERTIP --no-merged TIP refs/heads` (fork-point references, one call per layer, results bounded to 21 names), and `for-each-ref --format=<fixed with %(upstream)> refs/heads/NAME`.
- `merge-base --is-ancestor A B` (landed tip, upstream relation, pull request head relation).
- `cherry BASE TIP` (no `-v`; commit ids and marks only).
- `worktree list --porcelain -z` (checked-out-elsewhere).
- `rev-list --no-walk --parents --format=%s IDS` for bounded subjects, and `rev-list --parents -n1 SHA` for a merge commit's parent count.
- `cat-file -e SHA^{commit}` (object existence by exit status; no object content is read).

Not used: `status`, `diff`, `log`, `merge-tree`, `rebase`, `fetch`, `ls-remote`, `config` writes, `submodule`, anything that reads working-tree content. jj: only the pinned-operation session of Spec 008 (`--no-pager --color=never --ignore-working-copy --at-operation=OP`), `bookmark list --all-remotes --template …` and `log --no-graph -r REVSET --template …` with fixed templates; no `rebase`, `git fetch`, `new`, `edit` or any command that snapshots.

## Design Decisions

1. **Pure derivation core.** The adapters gather one `stackObservation` (refs, range, layers, upstreams, cherry marks, worktrees, jj flags, optional remote evidence with each source's outcome). `deriveFindings(observation)` and `buildPlan(observation, findings)` are pure and implement the spec tables row by row, so every row and every permission combination is table-tested without Git, jj or HTTP. A test iterates the Findings table codes and fails if a code has no test row; another fails if `action` is ever `unknown` or an `unknown` finding lacks a reason.
2. **One adapter interface, two implementations.** `stackSource` has `Resolve`, `Range`, `Layers`, `Upstreams`, `Equivalence` (Git only; jj returns the capability gap), `Worktrees`, `Resample`. The orchestration is backend-neutral; ownership selection reuses `Inspect`'s identity so a jj workspace never reaches the Git adapter.
3. **Isolated runner reuse without renaming.** The Git adapter calls `newPinsRunner` through a thin wrapper; the Spec 021 code is not moved or renamed in this slice (its work-item branches are in flight; a rename can follow separately). Characterization of the wrapper is the first task of the Git adapter.
4. **Layer algorithm.** Range R = commits reachable from TIP and not from BASE, at most 500, one parent each, bottom parent reachable from BASE (else `stack-no-common-base` for a root commit or `stack-ambiguous-topology` for a merge). Layer references are the local branches whose tips are in R; refs with the same tip form one layer. Layer 1's fork commit is the bottom commit's parent; `stack-behind-base` compares it with the base commit. Fork-point references are other local branches that contain a layer tip. Details and counts are bounded by LIM-2 and LIM-5.
5. **Remote evidence is a separate, optional phase.** After the local phase, `--pull-requests` resolves the hosting repository exactly as Spec 020 does (before any network access), reads `pulls?head=` per reference name, derives the distinct stack numbers, reads each `stacks/K`, then applies the Linkage rule. A failure of any remote step keeps the local result and yields `incomplete` (`stack-remote-failed`), never an error.
6. **Linkage and comparison.** Linkage uses names only to find candidates and the head commit to qualify (`verified`/`name-matched`). Comparisons use local ancestry of the commits when objects exist (`object-not-local` otherwise). Parent-merge kinds follow the spec: `rebase` from patch equivalence (Git), `merge` from a two-parent merge commit, `rewritten` otherwise. A layer is landed only by patch equivalence or by a merged pull request whose `merge_commit_sha` is a local ancestor of `--base`; otherwise the stale-base or fetch groups apply.
7. **Plan builder.** Steps follow the six rule groups of the spec (blocked, fetch first, stale base, reconcile, rebase, link) with the stated precedence; the landed set is a contiguous prefix (`merged-layer-not-bottom` otherwise, blocked at the lowest landed layer above a not-landed one). `old_parent_tip` is the sampled fork commit of the layer (a full object name); the new parent is the trunk ref for the lowest remaining layer and the previous remaining layer's primary branch afterwards. jj produces one step for the whole remainder, with names as revset literals and `-o` for the destination. A native stack or an upstream that is behind or diverged never gets a `rebase` or `link` step: it gets `fetch` (only for a missing object) or `observe` with a terminating `reconcile-upstream` note. `link` is never printed beside a `rebase`. Offline plans have `native_checked: false` and the boundary limitation. A name failing the Argument rules makes the plan `blocked` with `unsafe-name`; no partially quoted step exists. Display strings quote each element with single quotes unless it matches `[A-Za-z0-9._/=:-]+`; an element containing a single quote or a control byte is unprintable by construction.
8. **Transport extension is additive.** The Spec 022 closed allowlist gains exactly `repos/O/N/pulls?head=O:REF&state=all&per_page=5` and `repos/O/N/stacks/K`. Spec 025 plans its own additive shapes and typed failure classes in the same files; whichever work item lands first owns the shared change (typed failure class) and the other rebases. Until typed classes exist, every non-success status is `failed`/`source-failed`. The guard keeps refusing non-GET, `RawPath`, userinfo, fragments, redirects, extra or reordered query pairs; the ref grammar is enforced by the endpoint builder.
9. **Resample.** After all other reads the references (Git: the base, the tip and every layer reference; jj: the operation id and the same bookmarks) are read again, and with `--pull-requests` the `pulls?head=` requests are repeated; a difference is `stack-changed`.
10. **Schema.** `stack.schema.json` is self-contained; `if/then` per-code `detail`; identity definitions copied and drift-guarded; every existing schema and fixture compared byte for byte against the base in the tests.
11. **Command layer.** `internal/cli/stack.go` registers `stack show|plan`; the injection seam mirrors `runCLIWithHosting` so CLI tests use the real shared operation with fake HTTP and real repositories, with no production endpoint override.
12. **Exit statuses.** `ok` returns normally; `incomplete` and `error` return `runtimeFailure` (1) after the full result is written; usage errors exit 2 with no JSON.
13. **No polling, no retry.** Server-side rebases by GitHub are eventually consistent (unverified); the result is a sample and `observe` tells the caller to run again.
14. **Conformance.** Local conformance on real native Git (ordinary and linked worktree) and jj (standalone, colocated, secondary) with before/after sentinels on the minimum and current lanes; remote conformance by fake HTTP in the same fixtures; a binary smoke for local cases. A lane that is unavailable is not a pass.

## Edits to other specifications and documents (minimal, listed for review)

- `specs/015-local-restacking/spec.md`, Assumptions: add "Spec 012 (reworked 2026-10-17) supplies the stack observation and an advisory, print-only plan; this spec owns executing a local rewrite and its recovery evidence and must revalidate the 012 `expected` ids. A rewrite that GitHub performs on a native stack is not restacked here."
- `specs/016-stacked-pr-coordination/spec.md`, Assumptions: add "Spec 012 (reworked 2026-10-17) reads pull requests and native stack membership and prints advisory steps; this spec owns every remote effect of the catalog in `specs/012-stack-inspection/contracts/later-effects.md` (push, link, unstack, sync, merge), through GitHub native stack semantics and the bounded transport. FR-006 is unchanged for the first slice (L2 with a lease and L5 need its amendment). User Story 1 is re-scoped: the read-only mapping of a local stack to pull requests and native stacks is delivered by Spec 012; this spec consumes it and keeps the publication stories."
- `docs/roadmap.md`: the row and the list entry for Spec 012 get the new title and one-line summary.
- `docs/feature-status.json`: the `012-stack-inspection` object already exists with status `planned` and no evidence; it is unchanged (the spec path is unchanged).

## Open questions for the owner (recommendations)

1. **Diffs and changed-file queries**: withdrawn from 012 (Disposition table). Recommendation: keep withdrawn; GitHub and Git or jj already provide stored diffs, Spec 009 covers live files.
2. **How the later `link` effect writes**: Recommendation: Spec 016 specifies it as a REST write through the bounded transport after the endpoints are measured in a disposable repository with your approval; until then 012 only prints `gh stack link`.
3. **Network opt-in flag** `--pull-requests`: Recommendation: keep opt-in.
4. **A strict exit status for CI gating on drift**: Recommendation: defer; not needed for the read-only slice.
5. **Explicit `--tip`**: Recommendation: keep explicit in v1; revisit a default for Git only after use.
6. **Fork pull requests**: Recommendation: unsupported and disclosed in v1; measure before supporting.
7. **Layer cap 32**: GitHub's maximum stack size is unverified. Recommendation: confirm the documented maximum and set LIM-2 to it.
8. **`stack-link-missing` severity `action`**: Recommendation: keep; with GitHub native stacks the unlinked chain is the main drift of a rebase-merge workflow (it loses GitHub's server-side restacking).

## Risks

- **R1** jj parity is design only (no jj binary here); T002 measures before any jj code and may force changes to the Capability table; the contract states the one known gap.
- **R2** Parallel work on `transport.go`/`operations.go` (Spec 025 WI1, any Spec 022 follow-up): the remote work item is last in the order and rebases.
- **R3** The native stack REST resources are new; shapes beyond the measured closed stack are unverified (U4). Every unexpected shape or status degrades to `unknown` or `incomplete`, never to a claim.
- **R4** Patch ids differ when a rebase merge resolved a conflict or a squash combined commits; the layer then looks `pending` locally. The remote evidence (`rewritten`) covers it; the spec never claims a layer is not applied.
- **R5** Server-side rebases lag the merge (unverified): a fresh observation can show the child unrewritten; the plan then prints `fetch`/`observe`, never a competing `rebase`.
- **R6** Rebase risk with the in-flight Spec 021 work-item branches that touch `pins_run.go`: the adapter uses a wrapper and does not edit those files.
- **R7** `gh stack` is v0.2.1 and changing; the printed `link` line was checked against its `--help` only.
