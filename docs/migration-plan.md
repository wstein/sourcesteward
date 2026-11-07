# SourceSteward Rust implementation plan

Assessment: 2026-11-07. Workspace baseline: `9daec32` (`main`). This is a durable implementation plan,
not a handoff or a claim of completed migration. It preserves the earlier M0–M13 numbering from the local
comparison; dependencies, acceptance gates, and policy corrections below supersede that draft.

Tracking: [migration epic #2](https://github.com/wstein/whr-anchorage/issues/2),
[M0 runner/hosting #3](https://github.com/wstein/whr-anchorage/issues/3),
[review workflow #4](https://github.com/wstein/whr-anchorage/issues/4),
[recovery/oplog #5](https://github.com/wstein/whr-anchorage/issues/5),
[conflict workflow #6](https://github.com/wstein/whr-anchorage/issues/6).
These are workspace issues; legacy issues remain historical references and are not closed by this plan.

## Direction and evidence boundary

Build the useful Git/GitHub workflow subset in Rust. Treat `legacy/sourcesteward` as an edge-case and test
reference, not a parity target. Keep the existing Rust status/tidy/sync usable while adding workflow reads
and recovery before new history mutations. Native jj support is deferred; jj inspires the operation model.

Every new function starts as a script under `spikes/stw/<feature>/`; the owner accepts it live before product
implementation. Existing A–D prototypes are available, but live acceptance is still open in the backlog.
Prototype availability, passing historical tests, and product acceptance are separate states. This assessment
does not execute product or prototype suites; their runtime correctness is UNKNOWN here.

Repository work uses local branches and independent review, no PRs; the owner controls fast-forward integration,
push approval, releases, workflow starts, settings, and design decisions. A planned push command does not authorize
an agent to push. Other repositories may use PR workflows; policy must be selected per repository.

Code references below are repository-relative and identify inspected implementations or entry points, not
proof that every path works. Legacy was inspected at `0d0d9bee128a8336626b50763092020122d25fdc` (clean checkout); the workspace gitlink differs. Inspect its current revision when reusing fixtures.
Primary jj references for the design comparison: [conflicts](https://www.jj-vcs.dev/latest/conflicts/) and
[CLI reference: operation log and undo](https://www.jj-vcs.dev/latest/cli-reference/). These describe jj; they do
not establish that the Git pending prototype implements jj semantics or its workspace-scoped undo behavior.
The jj subprocess comparison is recorded in the Chartroom decision
`stw runs git only for push, fetch over ssh, range-diff and resolve`; its pinned upstream evidence was not
independently rerun for this plan. No new dependency on native jj is proposed.

## Feature matrix

Rust status: **present** means code exists; **partial** means a narrower contract; **absent** means no public
command in `src/cli.rs`. Decisions are recommendations unless already recorded in Chartroom.

| Capability | Go evidence under `legacy/sourcesteward/` | Rust evidence under `projects/sourcesteward/` | Decision |
| --- | --- | --- | --- |
| CLI help/version | `internal/cli/root.go:182` command registration | `src/cli.rs` clap `Command`, `--version` | Keep flags; do not copy a version subcommand just for parity. |
| Git/jj identity and ref inventory | `internal/inspection/{git,jj,inventory}.go` | `src/status.rs`, `src/repo/` (Git only) | Keep Git identity in status; defer native jj and inventory query language. |
| Workspace repository fanout | `internal/inspection/repos*.go` | `src/repo/`, workspace selection in `src/cli.rs` | Keep shared iteration instead of another `repos` command. |
| Local status, worktrees, gitlinks | `internal/statuscmd/`, `internal/inspection/pins*.go` | `src/status.rs`, `src/repo/worktrees.rs` | Keep; expand only for demonstrated workflow needs. |
| PR status/review state | `internal/reviewstate/parse.go`, `internal/prviews/` | `src/github.rs:154`, `src/status.rs:318`; review remains null | Port strict parser and trustworthy comment provenance (M3). |
| Sync and rules update | `internal/stwsync/` | `src/sync/`, `src/cli.rs` SyncArgs | Keep ff-only; no automatic stash, reset, merge, or rebase. |
| Tidy plan/apply/restore | `internal/tidywizard/` | `src/tidy.rs`, `src/tidy_apply.rs:495,908` | Keep manifest-first and fresh-state checks; do not copy wizard/authorization ceremony. |
| Tidy recovery retention/history | recovery and journals in `internal/tidywizard/` | `src/tidy_apply.rs:90,936`; JSONL stores OIDs, no retention refs | Add reachability roots and general undo (M5); object IDs alone do not survive GC. |
| Isolated workspace creation/retirement | `internal/inspection/jj_workspace.go`, `internal/workspace/` | worktree inspection/removal, no creation command | Defer general workspace wizard; add owned resolve worktree lifecycle in M12. |
| Stack/conflict plan | `internal/restack/replay.go`, `internal/conflicts/` | absent | Redesign as whole-stack preview (M1/M2). |
| Review carry-over | `internal/carryover/`, especially `git.go` guards | absent | Port proof semantics, not permissive grammar (M4). |
| Ready check | `internal/readycheck/` | absent | Port read-only gates with exact-head evidence and pending guard (M4/M12). |
| PR lint | `internal/lintpr/`, `internal/lintprcmd/` | absent | Port current rules and hostile-text handling (M8). |
| PR queue, stack, debt ledger | `internal/pulls/`, `internal/prviews/` | open PR reads only in `src/github.rs` | Add read-only queue view after parser/stack contracts (M9). |
| Push/workflow policy | `internal/stackpublish/` (core) | shell scripts `scripts/stw-push`, `scripts/stw-workflow`; no Rust commands | Port accepted script behavior per repo policy (M10). |
| Land stack | `internal/land/shapes.json` has empty shapes | absent | Design explicitly; do not imply Go has a complete usable apply path (M11). |
| Watch/issues/metrics/verify landed | `internal/watch/`, `internal/issues/`; other scope varies | absent | Defer; helpers can wait with background loops meanwhile (M13). |
| Release build/verify/dispatch | `internal/release/` | absent | Defer release tooling; dispatch/publication stays owner-controlled. |
| GitHub access | `internal/hosting/` | `src/auth.rs`, `src/github.rs` REST GETs | Extend internal reads; no need for equivalent hosting CLI wrappers. |
| JSON contracts/exit codes | `schemas/v1/`, `internal/cli/root.go` | schema 2 in README; exits 0/1/2 | Add fixtures for consumed contracts; define findings exit for new commands before adoption. |
| Untrusted text and bounded reads | `internal/untrusted/`, bounded runners | `src/github.rs` HTTP timeout; output boundary needs audit | Add escaping, pagination, output/time limits where new reads need them. |
| Brand/site/gh-jj wrappers | `site/`, optional extension in `internal/cli/root.go` | absent | Do not port as migration prerequisites. |

The Go parser accepts 40 **or 64** hex patch IDs and strips a trailing CR (`internal/reviewstate/parse.go:38–54`).
Current global rules require exactly 40 lowercase hex, fixed key order, and no CRLF. Rust must enforce the current
rules; Go is an oracle only where contracts agree. Comments assert a review; parsing a CLEAR never proves the
independent review, author trust, checked head, or CI results by itself.

## Existing experiments and jj-inspired improvements

Recommendation score: 1 = defer, 2 = low priority, 3 = useful later, 4 = recommended after dependencies,
5 = next priority. This is distinct from benefit, effort, and risk. A: **5/5**; B: **5/5**; C: **4/5**;
D: **4/5**; full native jj parity: **2/5**. Scores order experiments, not approval to implement.

| Option | Existing spike and evidence | Product recommendation | Acceptance still needed |
| --- | --- | --- | --- |
| A: conflict preview | `spikes/stw/conflicts/README.md`, `stw-rebase-lib.sh`, `test-stw-stack-preview` | Replay whole explicit stack in memory; show conflicts at every level and inherited conflict uncertainty. | Owner live demo plus gix/Git tree oracle; no refs/index/worktree changes. |
| B: oplog/undo | `spikes/stw/oplog/README.md`, `stw-oplog-lib.sh` | Record operation intent and old refs before mutation, retain objects through refs, undo local effects with CAS. | Live demo including GC, concurrent ref move, partial checkout failure, interrupted operation. |
| C: rerere | `spikes/stw/deferred/README.md`, `stw-resolve` | Reuse Git's common-dir rr-cache during explicit resolution; report actual reuse. | Resolve same conflict in two worktrees; reject mistaken reuse and preserve user edits. |
| D: deferred conflicts | `spikes/stw/deferred/stw-restack`, `stw-resolve`, `stw-pending-check` | Store pending candidates separately; leave conflicted branches unchanged; block push/land/ready. | Resolve bottom-up, abort/retry, changed parent, stale old tip, undo, and all publication guards. |

A is read-only with respect to refs/index/worktrees, but the shell prototype writes unreachable loose objects.
The Rust goal is an object-memory overlay; compare disk ODB before/after rather than claiming prototype-level
zero writes. Conflict-marker commits are provisional computation, not a native jj structured conflict model.
Later levels based on conflicts may give provisional answers and must say so.

The B prototype stores snapshot objects behind `refs/stw/oplog/<n>` and handles symbolic refs/HEAD separately;
it documents non-atomic worktree and symbolic-ref updates. Product design must retain those limits or improve them,
not promise a filesystem transaction. Undo cannot reverse a push or recover arbitrary untracked files.

The D prototype uses `pending` and `pending-meta` refs, Git cherry-pick in a dedicated worktree, and an optional
oplog hook. Product mutations must require a working recovery path; silently proceeding without oplog is not the
product acceptance target. The marker-removal heuristic is not sufficient evidence for all conflict kinds:
binary, modify/delete, rename, and genuine marker text need explicit resolution checks.

Prototype identity behavior is not a product contract: original-committer fallback, ENV identity requirements,
and synthetic `stw-oplog` identities must not be copied into commit-writing commands. Product-created commits
use the configured Git identity, validated read-only; never set user.*, override author/committer, or accept identity
parameters as a workaround. Distinguish Git commit attribution from the existing Rust reflog-only fallback.
Preserving an original commit author during replay needs an explicit owner decision before implementation because
current global instructions require author and committer to equal the configured identity for pushed commits.

## Milestones and dependencies

Ratings are estimates, not measurements: benefit/effort/risk each 1–5; higher means more. Effort includes fixtures,
review, and live acceptance. Each row names the minimum acceptance evidence; every new automated check must fail
on a targeted mutation in a disposable fixture. No test may run GC, expire reflogs, reset, or delete refs in a real repo.

| ID | Deliverable and dependencies | Acceptance gate | Benefit / Effort / Risk |
| --- | --- | --- | --- |
| M0 | Foundation: one `via: git` runner; enable gix merge only when needed; extend GitHub reads. No dependencies. | Empty PATH gives typed unsupported (exit 1) for Git steps, core reads still work; fixed argv, repository selection/env/locale tests, unknown-option/version errors, credential redaction; measure merge feature size. | 5 / 3 / 3 |
| M1 | In-memory replay engine. Needs M0 merge capability and accepted A spike. | Compare trees with Git on clean, rename, binary, empty-start, became-empty, already-upstream, root/merge rejection fixtures; no disk/ref/index changes; author/committer policy resolved. | 5 / 4 / 4 |
| M2 | Whole-stack plan and conflict preview A. Needs M1 and accepted stack input contract. | Explicit bottom-up chain first; later PR-derived edges; reject cycles/ambiguous bases; level-3 and inherited conflicts visible; stable JSON; unchanged repo. Review invalidation annotation adds M3. | 5 / 3 / 3 |
| M3 | Strict review parser/state and comment reads. Needs M0 HTTP read extension. | Shared rule corpus: malformed order, wrong PR, 39/64 hex, uppercase, tabs, CRLF, stale head, later FINDINGS, untrusted author, invalid carry chain; UNKNOWN stays explicit. | 5 / 2 / 3 |
| M4 | Read-only carry-over and ready-check. Needs M3 and M0 range-diff runner. | Equal patch-id, no merge commits, every range-diff entry '='; reordered/whitespace/rewritten-parent counterexamples; exact-head CI, missing/skipped/failed checks, correct base, blockers. No automatic posting/ready mutation. | 5 / 3 / 4 |
| M5 | Retained oplog and local undo B for tidy/sync, then all new mutations. Needs accepted B spike; gix ref/object design. | Retain old tips through GC in scratch; validate expected old values; crash journal intent/completion; dry run, idempotence, symbolic refs, missing worktree, dirty/untracked obstruction, rollback failure; remote effects explicitly not undone. | 5 / 4 / 5 |
| M6 | Conflict-free local restack apply. Needs M1/M2/M5 and identity decision. | Plan SHA revalidation/CAS; preserve refs on mismatch; clean worktree update or explicit refusal; range-diff result; undo restores local refs; no implicit push or invented CLEAR/carry-over. M4 required to assess review reuse. | 5 / 4 / 5 |
| M7 | rerere C in explicit resolution. Needs accepted C spike and M0; integrate M12 worktree flow. | Shared cache reuse in two worktrees, exact reused count, failure/no-match fallback; no persistent Git config writes. Preview cache prediction remains optional and UNKNOWN until measured. | 3 / 2 / 3 |
| M8 | PR lint. Needs M3. | Per-rule positive/negative fixtures for identity, attribution, session links, text limits, malformed review claims; escape hostile titles/comments; runtime error distinct from findings. | 4 / 2 / 2 |
| M9 | PR/stack queue and debt view. Needs M2/M3/M4. | Count units not PRs; stale drafts, rewritten heads, bottom-up order, ledger parse failures, queue/nit rules against current dotagents; pagination and stale data reported. Read-only. | 4 / 4 / 3 |
| M10 | Rust push/workflow from accepted scripts. Needs M0/M4/M5, repository policy and local CLEAR evidence contract. | Existing `scripts/test-stw-push` behaviors translated after script acceptance; ff-only default branch, submodules first, no tags/force, exact-head CLEAR; partial remote effects reported and never described as undoable. Pending guard from M12 before deferred flow enabled. | 4 / 3 / 5 |
| M11 | Owner-invoked land stack. Needs M2/M4/M5/M6/M10 and explicit design approval. | Separate PR/rebase policy from local ff policy; exact reviewed heads and checks, no pending refs, stale remote refusal, fake-forge failure fixtures; owner sandbox live demo before any real publication. | 3 / 5 / 5 |
| M12 | Deferred conflicts D and owned resolve worktrees. Needs M1/M2/M5/M6/M7/M0 and accepted D spike. | Real conflicted branches stay at old tips; pending metadata/ref transaction; parent-first resolve; abort preserves pending; stale CAS; binary/delete conflicts; undo; push/land/ready refuse pending state. | 4 / 5 / 5 |
| M13 | Demand-driven follow-ups: watch/issues, verify landed, metrics, repo profiles, Dayboard integration, release tooling, merge-noop proof, native jj opt-in. | One owner-selected slice at a time with script/live acceptance; define scope, contract, and new ratings before implementation. No blanket parity completion criterion. | 2 / 5 / 4 |

Recommended order: **M0 → M3 → M4** for workflow reads; in parallel as a separate workstream,
**accepted A/B → M1 → M2 → M5 → M6** for local stacks. M8 is independent after M3.
M7/M12 can follow local restack; M10/M11 follow proven recovery and publication policy.
M7 does not require publication; M6 does not push. M12 publication guards must land before pending state can
be created in a product used with push/ready commands. These are dependency tracks, not permission to start agents.
M0 starts directly in `projects/sourcesteward` without a spike (owner decision: infrastructure, the Git boundary is
already recorded in Chartroom). Scripts-first stays for new user-facing functions.

## Decisions, tradeoffs, and open evidence

1. **Recorded decision: Git boundary.** Read/merge through gix; installed Git only for push, ssh/local fetch,
   range-diff, and conflict resolution. HTTPS fetch stays gix. Mark each such output step `via: git`.
   Update `tests/no_binaries.rs` to permit only the runner module rather than removing the architecture check.
   Currently gix itself can spawn ssh/git-upload-pack for non-HTTPS fetch (`tests/remote_kinds.rs`); M0 must close
   that indirect path too. Keep authentication usable; exact environment scrub/credential allowlist and minimum Git
   versions need fixtures. Git version requirements differ: preview spike 2.40, resolve spike 2.45.
2. **Open: stack input.** Recommend explicit stack input first. Existing prototypes take `ONTO BRANCH...`; local
   upstream config does not reliably encode stack parentage. Keep explicit ordered input until owner accepts a durable
   local/PR edge model. Reject ambiguity instead of guessing from merge-base. Benefit 5, effort 2, risk 2. The decision
   stays open until the stack-model spike (`spikes/stw/stack-model`, branch `feat/stw-stack-model`, in review) has CLEAR
   and the owner accepted it live. It is needed before M2 and M6. Do not treat its unmerged design as accepted.
3. **Recommend recovery before new mutation.** JSONL manifest IDs do not retain objects; add retained ref snapshots
   and crash detection before restack. Full worktree atomicity remains unavailable; surface incomplete operations.
   Benefit 5, effort 4, risk 5. Keep tidy restore compatibility during migration.
4. **Recommend controlled binary transition.** `dayboard/LOGBOOK.md` records Go release v0.2.0 remains installed until M3+M4.
   Do not replace that channel with Rust while removing working review gates. Current installed binary/version and
   packaging channel are UNKNOWN in this assessment; owner chooses release/repository/channel and accepts live.
5. **Deferred: local CLEAR evidence.** The workspace has no PRs. A review note, local receipt, or external report could
   carry exact-head evidence, but notes are editable claims too. Owner deferred the decision until before M10. Define
   author trust, head binding, and evidence storage by then; do not introduce a Git note namespace as an already
   approved decision.
6. **Recorded decision: replay attribution.** New commits carry the configured Git identity as author and committer.
   Commits rewritten by restack, rebase, or cherry-pick keep their original author; the committer is the configured
   identity, as `git rebase` does. A dotagents rule change for this is in progress.
7. **Defer native jj and in-memory rerere prediction.** Rerere reuse during Git resolution is useful without
   reimplementing Git's conflict-ID algorithm. jj-style structured conflict persistence needs another design, not
   merely commits containing marker text. Benefit 2, effort 5, risk 4 for native parity at this stage.

Known limits from the Rust README remain relevant: patch-ID oracle mismatches, partial checkout/rollback crash
windows, platform/filter/sparse-checkout uncertainty, and restore missing-object after pruning. Their historical
counts are not new measurements. Go and Rust test suites, full mutation sweeps, network tests, platform checks,
and all owner live acceptances were **not run** for this documentation assessment.
