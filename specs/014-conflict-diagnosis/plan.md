# Implementation Plan: Conflict diagnosis and isolated integration preflight

> **Owner-decision amendment — 2026-10-25:** all former OQ IDs are decided;
> [current profiles](../016-stacked-pr-coordination/contracts/owner-decisions.md) supersede earlier undecided recommendations.
> Shared human/shortening-timeout flags adopted; preflight remains offline/helper-free.
> This is a documentation target, not implementation or native acceptance.

**Branch**: `codex/spec-014-stack-toolbelt` | **Date**: 2026-10-25 | **Spec**: [spec.md](spec.md)

## Summary and status

Design only. Add read-only existing-conflict diagnosis and a separately selected
private-scratch prediction capability. Restack/rebase replay follows Spec 015 D7
plumbing/quarantine, never porcelain rebase. Native merge and jj capabilities
remain gated by the owner decisions and measurements in [research](research.md).
No code, schema file, dependency, build, test execution or runtime operation is
part of this amendment. New JSON proposals are UNPUBLISHED until implemented and
conformant; hidden restack-plan JSON/schema bytes are preserved. CURRENT incomplete exit 3
changes to target exit 1 only through the explicit 015 revision-2 root/CLI task.

## Technical context

Use the repository's Go 1.27.2/Cobra architecture, native Git and native jj runners,
and standard library first; no dependency is proposed. Existing inspection
minimums (Git 2.46+, jj 0.45.1+) do not establish prediction support. Record exact
minimum/current preflight lanes only after the T003 capability study; an absent
mandatory lane blocks acceptance. Linux bubblewrap/macOS sandbox-exec enforcement
must deny original writes and network; unavailable enforcement refuses safely.

The proposed `internal/conflict` operation owns typed results and a small effect
shell. `internal/cli/conflict.go` only parses/renders; it imports no process,
filesystem or network adapter. Inspection/layout ownership is reused without
creating a workspace or reading authentication. Any shared 015 replay seam is
characterized and reviewed first; no hard 012 dependency is introduced. This is
an implementation path proposal, not an exported API availability claim.

## Constitution check (before and after design)

| Principle | Design control and future evidence |
| --- | --- |
| I shared bounded contract | One result renders human/JSON; no stdin, pager, editor or prompts; T025–T028 |
| II evidence before effects | Prediction explicitly authorizes nothing; exact semantic binding and independent consumer validation; T017–T020 |
| III ownership/recovery | Original store/workspaces remain read-only; scratch cleanup only own private handle; no recovery claim; T005–T006, T021–T022 |
| IV test first/conformance | Red tests before each behavior, minimum/current native lanes and mutation probes; T003–T004, T029–T032 |
| V native peers | Ownership selects native Git/jj including colocation; no fallback; capability table records limits; T007–T010, T023–T024 |

No constitution exception or amendment is requested. Open OQs block the affected
implementations; this plan is ready for design review, not unqualified implementation.

## Structure and functional boundary

- `internal/conflict/result.go`, `derive.go`, `binding.go`: pure typed values,
  classification/completeness and input binding. No I/O, clock, concurrency or
  environment reads in decisions; the shell passes sampled facts and timestamps.
- `internal/conflict/observe_git.go`, `observe_jj.go`: bounded native conflict
  metadata through reviewed runners; stored and disk observations never conflated.
- `internal/conflict/scratch.go`, `replay_git.go`, `preflight_jj.go`: enforce
  disposable ownership, capability admission and the accepted native replay effects. Source objects
  are read-only alternates; writable objects/config/locks/temp files stay private.
- `internal/conflict/preflight.go`: T018 operation shell coordinates admission,
  native input samples, pure binding comparison and declared replay/lifecycle edges.
- `internal/conflict/merge_git.go`: T020 accepted native merge-profile prediction;
  no strategy fallback or enablement before owner/capability gates.
- `internal/conflict/handoff.go`: T012 pure body-free handoff values and inert
  explanations; content/privacy extraction remains at the separate context edge.
- `internal/conflict/render.go`: T028 bounded typed serialization, shared by human
  and structured output without importing native execution into CLI rendering.
- `internal/conflict/context.go`: opt-in selected-path hunk extraction behind
  OQ-HUNK, never an arbitrary file reader or resolver.
- `internal/cli/conflict.go`: no-subcommand help, explicit leaves, typed output
  and status mapping. Future schema wiring is additive only after acceptance.

Independent metadata reads may use Spec 040 `internal/fanout` with a bounded width
of at most four and input-indexed results. No new goroutine pool or shared append.
Git replay is dependent oldest-first work and remains sequential. Native jj
requires accepted graph/conflict semantics under OQ-JJCONF; do not impose a Git
stop-first suffix or parallelize effects/resampling across native dependencies.

## Operation sequence

1. Parse explicit mode and semantics; no leaf means help and no observation.
2. Admit canonical layout and native ownership; capture backend/version and native
   state, full participants and resolved references. Reject unsafe/missing history,
   unmeasured semantic profiles and unsupported native capabilities before replay.
3. Diagnosis runs with the binding sanitized runner/configuration controls in
   [preflight isolation](contracts/preflight.md#binding-execution-isolation-and-hazard-refusal)
   and enforced original-write/network denial. Git uses `ls-files -u -z`,
   `rev-parse` and bounded no-follow operation-file reads, never `git status`,
   index refresh or fsmonitor. Native jj uses isolated HOME/XDG/JJ_CONFIG and
   pinned stored conflict evidence, without resolving, staging or snapshotting.
4. Preflight allocates private scratch only after admission, installs write/network
   confinement and sanitized execution, then performs the declared native kernel.
   For Git linear replay, every actual merge-tree call binds --attr-source to full
   source-head H in guarded private Git admin/worktree/index state. Unsupported
   native enforcement refuses; T033 red regression precedes T016. Source range
   is exactly old-parent..source-head, oldest first;
   first conflict stops and suffix remains not-evaluated. For merge, no replay
   substitution is allowed; an accepted native strategy/topology is mandatory.
   Native jj preview additionally waits for accepted OQ-JJCONF graph/aggregate
   evidence; a clean final tip cannot erase conflicted intermediate revisions.
5. Derive bounded conflict paths/types and optional accepted hunk context. No
   automatic custom merge driver, textconv, filter, hook, fsmonitor or project script.
6. Resample selected references, ownership/layout and operation state. Any change,
   failed resample or missing material prevents a complete prediction. Samples are
   not a transaction and cannot prove absence of every ABA race.
7. Retire only own disposable state on every exit; failed cleanup changes status
   to incomplete and yields a bounded opaque handle, never arbitrary recursive
   cleanup instructions. Emit one final typed result with fixed limitation.

## Phases, sizing and review units

Sizes are rough author effort: S half a day, M one day, L two to three days;
review and CI waits are additional. Each implementation unit follows red/green/refactor.

| Unit | Tasks | Size | Dependencies and independently reviewable outcome |
| --- | --- | --- | --- |
| A0 decisions/capabilities | T001–T004 | M | Owner resolves affected OQs, native measurement and exact-head design review |
| A1 isolation/value foundation | T005–T006 | L | A0; private scratch and pure result/binding model, no CLI exposure |
| A2 diagnosis MVP | T007–T010 | L | A1; independent Git and native jj metadata diagnosis |
| A3 resolver context | T011–T014 | M | A2 and OQ-HUNK; default body-free handoff plus separately accepted hunk capability |
| A4 Git preflight | T015–T022, T033 | L | A1/OQ-SEM/TOPO/SCRATCH; exact linear replay, merge only measured profile, races and cleanup |
| A5 native jj preflight | T023–T024 | L | A0/A1/native spike; capability refusal until isolation/semantics proven |
| A6 output/compatibility | T025–T028 | M | Selected story units; default-none, bounded render/exit, frozen-v1 regression |
| A7 conformance/handoff | T029–T032 | M | Selected capabilities; truthful acceptance and independent exact-head review |

A2 is the MVP, with A6 and its required A7 evidence. A4 is a separate increment;
A5 and hunk/merge extensions remain disabled unless accepted. Git/jj observers
can be authored in disjoint files after common values are stable. Shared runner,
CLI, neutral replay seam and schema wiring serialize. Each unit targets main
independently unless an actual code dependency later justifies a stack.

## Risks and controls

| Risk | Required control |
| --- | --- |
| Worktree/index attributes falsely clean a conflict | Bound full-H --attr-source on every real merge-tree call, private empty override context, repeated guards/actual receipt and T033 regression; H/N check-attr alone is insufficient |
| Git plumbing writes original objects or invokes merge drivers | Private writable object store + read-only source alternate, hazard admission, sandbox deny, write/network recorder and sentinels |
| Scratch jj shares operation log/object store | Native isolated-store measurement, no workspace-add in original, refuse unavailable capability |
| Optional hunk leaks secrets or hostile instructions | Default no bodies, selected exact paths, omission reason and finite bounds; content is DATA, never executed |
| Partial replay appears clean | Git first-conflict coverage/not-evaluated suffix; native jj complete graph and accepted aggregate semantics, unknown on missing evidence |
| Shared 015 refactor changes hidden v1 | Characterize CURRENT exit 3 and preserve frozen JSON file/fixture digests; the explicit 015 revision-2 task changes incomplete to target 1 |
| Clean prediction treated as merge approval | Fixed notice in all result modes; no authorization/review/check field asserting readiness |
| Cancellation leaves scratch | Owned cleanup after reaping children, residue handle and incomplete status; never clean arbitrary supplied path |

See [data model](data-model.md), [contracts](contracts/cli.md),
[quickstart](quickstart.md), [tasks](tasks.md) and [analysis](analysis.md).
