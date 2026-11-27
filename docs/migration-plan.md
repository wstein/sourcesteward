# SourceSteward Rust implementation plan

Assessment: 2026-11-07. Workspace baseline: `9daec32` (`main`). This is an implementation plan,
not a handoff or a claim of completed migration. The milestones are numbered M0–M13.

Tracking: [migration epic #2](https://github.com/wstein/whr-anchorage/issues/2),
[M0 runner/hosting #3](https://github.com/wstein/whr-anchorage/issues/3),
[review workflow #4](https://github.com/wstein/whr-anchorage/issues/4),
[recovery/oplog #5](https://github.com/wstein/whr-anchorage/issues/5),
[conflict workflow #6](https://github.com/wstein/whr-anchorage/issues/6).
These are workspace issues; legacy issues remain historical references and are not closed by this plan.

## Direction and evidence boundary

Build the Git/GitHub workflow subset in Rust. Treat `legacy/sourcesteward` as the source of ports and as a test
reference, not a parity target: every Go command gets a decision (see "Cutover and archive of legacy/sourcesteward"). Keep the existing Rust status/tidy/sync usable while adding workflow reads
and recovery before new history mutations. jj is not supported (owner decision 2026-11-11, decision 8); jj inspires the operation model.

Every new user-facing function without a Go counterpart starts as a script under `spikes/stw/<feature>/`; the owner
accepts it live before product implementation. Ports of existing Go behavior skip spike and live acceptance: they are
written in Rust against test cases derived from Go, deviations are named, and an independent review applies (AGENTS.md,
"Scripts first, then product"). M0 is the exception (see the recommended order). Existing A–D prototypes are available, but live acceptance is still open in the backlog.
Prototype availability, passing historical tests, and product acceptance are separate states. This assessment
does not execute product or prototype suites; their runtime correctness is UNKNOWN here.

Repository work uses local branches and independent review, no PRs; the owner controls fast-forward integration,
push approval, releases, workflow starts, settings, and design decisions. A planned push command does not authorize
an agent to push. Other repositories may use PR workflows; policy must be selected per repository.

Code references below are repository-relative and identify inspected implementations or entry points, not
proof that every path works. Legacy was inspected at `0d0d9bee128a8336626b50763092020122d25fdc` (clean checkout); the workspace gitlink differs. Inspect its current revision when reusing fixtures.
The cutover section below was built at the gitlink `b9ec1f22efbcfaba3072263cf1b7422819fde14e` (`v0.2.0-55`).
Primary jj references for the design comparison: [conflicts](https://www.jj-vcs.dev/latest/conflicts/) and
[CLI reference: operation log and undo](https://www.jj-vcs.dev/latest/cli-reference/). These describe jj; they do
not establish that the Git pending prototype implements jj semantics or its workspace-scoped undo behavior.
The jj subprocess comparison is recorded in the Chartroom decision
`stw runs git only for push, fetch over ssh, range-diff and resolve`; its pinned upstream evidence was not
independently rerun for this plan. No dependency on jj is proposed (decision 8).

## Feature matrix

Rust status: **present** means code exists; **partial** means a narrower contract; **absent** means no public
command in `src/cli.rs`. Decisions are recommendations unless recorded in Chartroom or marked as owner decision in this plan.

| Capability | Go evidence under `legacy/sourcesteward/` | Rust evidence under `projects/sourcesteward/` | Decision |
| --- | --- | --- | --- |
| CLI help/version | `internal/cli/root.go:182` command registration | `src/cli.rs` clap `Command`, `--version` | Keep `stw --version`. Drop the Go `version` subcommand and its JSON output ([#33](https://github.com/wstein/whr-anchorage/issues/33)). |
| Git/jj identity and ref inventory | `internal/inspection/{git,jj,inventory}.go` | `src/status.rs`, `src/repo/` (Git only) | Drop Go `inspect` and `inventory`. Checkout-free inspection remains in [#28](https://github.com/wstein/whr-anchorage/issues/28). Worktree facts belong in `stw status --worktrees --json` ([#33](https://github.com/wstein/whr-anchorage/issues/33)). |
| Workspace repository fanout | `internal/inspection/repos*.go` | `src/repo/`, workspace selection in `src/cli.rs` | Drop Go `repos`. The repository overview belongs in `stw status --json` ([#33](https://github.com/wstein/whr-anchorage/issues/33)). |
| Local status, worktrees, gitlinks | `internal/statuscmd/`, `internal/inspection/pins*.go` | `src/status.rs`, `src/repo/worktrees.rs` | Keep local status and report submodule pin drift in `stw status`. Drop Go `pins` ([#33](https://github.com/wstein/whr-anchorage/issues/33)). Expand worktree fields when a concrete workflow needs them. The status flags `--long --fetch --worktrees --icons --timeout --stats --verbose` are ported (see the `status` row of the port inventory). Done 2026-11-12: the repositories of a workspace are the gitlinks of the index as in Go (README "Submodules: the gitlinks of the index", mutations GL1 to GL9); nested submodules walked depth-first and the root found up to 16 levels up (mutations NS1 to NS16). |
| PR status/review state | `internal/reviewstate/parse.go`, `internal/prviews/` | `src/github.rs:154`, `src/status.rs:318`; `status --review` uses it (src/status_review.rs) | Port strict parser and verifiable comment author and head binding (M3). |
| Sync and rules update | `internal/stwsync/` | `src/sync/`, `src/cli.rs` SyncArgs | Keep ff-only; no automatic stash, reset, merge, or rebase. |
| Tidy plan/apply/restore | `internal/tidywizard/` | `src/tidy.rs`, `src/tidy_apply.rs:495,908` | Keep manifest-first and fresh-state checks; do not copy wizard/authorization ceremony. |
| Tidy recovery retention/history | recovery and journals in `internal/tidywizard/` | `src/tidy_apply.rs:90,936`; JSONL stores OIDs, no retention refs | Add a backup ref per new mutation (M5, slimmed 2026-11-10, see Cutover section); object IDs alone do not survive GC. `tidy history` (done 2026-11-11) lists the manifests; `sync` journals with recovery refs (done 2026-11-11). |
| Isolated workspace creation/retirement | `internal/inspection/jj_workspace.go`, `internal/workspace/` | `src/workspace.rs`: native worktree entry, checkpoint/resume and retirement preview | Retire the Go wizard ([#36](https://github.com/wstein/whr-anchorage/issues/36)). Safe ref recovery remains in [#29](https://github.com/wstein/whr-anchorage/issues/29). M12 still covers owned conflict-resolution worktrees. |
| Stack/conflict plan | `internal/restack/replay.go`, `internal/conflicts/` | `stw restack plan` (`src/restack_plan.rs`); one branch `prepare` / `apply` / `undo` (`src/restack_apply.rs`, engine `src/replay.rs`) | The read-only preview and the single-branch apply are present. Conflicts, merges and a stack of more than one branch stay open. |
| Review carry-over | `internal/carryover/`, especially `git.go` guards | absent | Port proof semantics, not permissive grammar (M4). |
| Ready check | `internal/readycheck/` | absent | Port read-only gates with exact-head evidence and pending guard (M4/M12). |
| PR lint | `internal/lintpr/`, `internal/lintprcmd/` | absent | Port current rules and hostile-text handling (M8). |
| PR queue, stack, debt ledger | `internal/pulls/`, `internal/prviews/` | GitHub reads, `stw status --review`, `stw ready-check` | The owner chose to drop the separate queue after the triage trial ([#15](https://github.com/wstein/sourcesteward/issues/15)). |
| Push/workflow policy | `internal/stackpublish/` (core) | shell scripts `scripts/stw-push`, `scripts/stw-workflow`; no Rust commands | Port accepted script behavior per repo policy (M10). |
| Land stack | `internal/land/shapes.json` has empty shapes | absent | Drop Go `land`. Use Git and `gh` for PR workflows. Reopen automation only for a named workflow under the review and publication rules ([#37](https://github.com/wstein/whr-anchorage/issues/37)). |
| Watch, metrics, verify landed | `internal/watch/`; other scope varies | `stw watch --once`, `--stream` and `--status` | Checks, mergeability, restack events and `--until-event` shipped. See [watch acceptance](#watch-acceptance). |
| Open issues | `internal/issues/` | `stw status` has counts | Retire standalone Go `issues`. Full open-issue lists belong in default text and JSON status. Counts remain a summary ([#35](https://github.com/wstein/whr-anchorage/issues/35)). |
| Release build/verify/dispatch | `internal/release/` | absent | Drop the hidden Go release commands. The owner owns releases ([#37](https://github.com/wstein/whr-anchorage/issues/37)). |
| GitHub access | `internal/hosting/` | `src/auth.rs`, `src/github.rs` REST GETs | Drop Go hosting wrappers. Use GitHub and `gh` for direct queries and keep product reads internal ([#33](https://github.com/wstein/whr-anchorage/issues/33)). |
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
D: **4/5**; full native jj parity: **2/5** (not pursued, decision 8). Scores order experiments, not approval to implement.

| Option | Existing spike and evidence | Product recommendation | Acceptance still needed |
| --- | --- | --- | --- |
| A: conflict preview | `spikes/stw/conflicts/README.md`, `stw-rebase-lib.sh`, `test-stw-stack-preview` | Replay whole explicit stack in memory; show conflicts at every level and inherited conflict uncertainty. | Owner live demo plus gix/Git tree oracle; no refs/index/worktree changes. Rust: `stw restack plan` is built (oracle: real `git rebase`); the live demo of the owner is open. |
| B: oplog/undo | `spikes/stw/oplog/README.md`, `stw-oplog-lib.sh` | Record operation intent and old refs before mutation, retain objects through refs, undo local effects with CAS. Superseded by the Cutover section (2026-11-10): not built, M5 uses a backup ref. | Live demo including GC, concurrent ref move, partial checkout failure, interrupted operation. |
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
oplog hook. Product mutations must require a working recovery path; silently proceeding without a backup ref is not the
product acceptance target. The marker-removal heuristic is not sufficient evidence for all conflict kinds:
binary, modify/delete, rename, and genuine marker text need explicit resolution checks.

Prototype identity behavior is not a product contract: original-committer fallback, ENV identity requirements,
and synthetic `stw-oplog` identities must not be copied into commit-writing commands. Product-created commits
use the configured Git identity, validated read-only; never set user.* or accept identity parameters as a workaround.
New commits get the configured identity as author and committer; commits rewritten by restack, rebase, or
cherry-pick keep their original author and get the configured identity as committer (decision 6). Distinguish Git
commit attribution from the existing Rust reflog-only fallback.

## Milestones and dependencies

Ratings are estimates, not measurements: benefit/effort/risk each 1–5; higher means more. Effort includes fixtures,
review, and live acceptance. Each row names the minimum acceptance evidence; every new automated check must fail
on a targeted mutation in a disposable fixture. No test may run GC, expire reflogs, reset, or delete refs in a real repo.

| ID | Deliverable and dependencies | Acceptance gate | Benefit / Effort / Risk |
| --- | --- | --- | --- |
| M0 | Foundation: one `via: git` runner; enable gix merge only when needed; extend GitHub reads. No dependencies. | Empty PATH gives typed unsupported (exit 1) for Git steps, core reads still work; fixed argv, repository selection/env/locale tests, unknown-option/version errors, credential redaction; measure merge feature size. | 5 / 3 / 3 |
| M1 | In-memory replay engine. Needs M0 merge capability and accepted A spike. | Compare trees with Git on clean, rename, binary, empty-start, became-empty, already-upstream, root/merge rejection fixtures; no disk/ref/index changes; attribution per decision 6. | 5 / 4 / 4 |
| M2 | Whole-stack plan and conflict preview A. Needs M1 and accepted stack input contract. | Explicit bottom-up chain first; later PR-derived edges; reject cycles/ambiguous bases; level-3 and inherited conflicts visible; stable JSON; unchanged repo. Review invalidation annotation adds M3. | 5 / 3 / 3 |
| M3 | Strict review parser/state and comment reads. Needs M0 HTTP read extension. | Shared rule corpus: malformed order, wrong PR, 39/64 hex, uppercase, tabs, CRLF, stale head, later FINDINGS, untrusted author, invalid carry chain; UNKNOWN stays explicit. | 5 / 2 / 3 |
| M4 | Read-only carry-over and ready-check. Needs M3 and M0 range-diff runner. | Equal patch-id, no merge commits, every range-diff entry '='; reordered/whitespace/rewritten-parent counterexamples; exact-head CI, missing/skipped/failed checks, correct base, blockers. No automatic posting/ready mutation. | 5 / 3 / 4 |
| M5 | Superseded by the Cutover section (2026-11-10): a backup ref `refs/stw/backup/<n>` before each mutation, no spike B, no journal, no general undo. Original: retained oplog and local undo B for tidy/sync, then all new mutations. Needs accepted B spike; gix ref/object design. | Slim M5: the backup ref exists before every mutation and survives GC in scratch; undo sets the ref back with CAS on the expected value; `range-diff` shows the change; remote effects are not undone. Original: retain old tips through GC in scratch; validate expected old values; crash journal intent/completion; dry run, idempotence, symbolic refs, missing worktree, dirty/untracked obstruction, rollback failure; remote effects explicitly not undone. | 5 / 4 / 5 |
| M6 | Conflict-free local restack apply. Needs M1/M2/M5. | Plan SHA revalidation/CAS; preserve refs on mismatch; clean worktree update or explicit refusal; range-diff result; undo restores local refs; no implicit push or invented CLEAR/carry-over. M4 required to assess review reuse. | 5 / 4 / 5 |
| M7 | rerere C in explicit resolution. Needs accepted C spike and M0; integrate M12 worktree flow. | Shared cache reuse in two worktrees, exact reused count, failure/no-match fallback; no persistent Git config writes. Preview cache prediction remains optional and UNKNOWN until measured. | 3 / 2 / 3 |
| M8 | PR lint. Needs M3. | Per-rule positive/negative fixtures for identity, attribution, session links, text limits, malformed review claims; escape hostile titles/comments; runtime error distinct from findings. | 4 / 2 / 2 |
| M9 | Standalone PR queue dropped after the triage trial ([#15](https://github.com/wstein/sourcesteward/issues/15)). Any future stack or debt view requires a separate decision. | Count units not PRs; stale drafts, rewritten heads, bottom-up order, ledger parse failures, queue/nit rules against current dotagents; pagination and stale data reported. Read-only with versioned JSON. | 4 / 4 / 3 |
| M10 | Rust push/workflow from accepted scripts. Needs M0/M4/M5, repository policy and local CLEAR evidence contract. | Existing `scripts/test-stw-push` behaviors translated after script acceptance; ff-only default branch, submodules first, no tags/force, exact-head CLEAR; partial remote effects reported and never described as undoable. Pending guard from M12 before deferred flow enabled. | 4 / 3 / 5 |
| M11 | Historical owner-invoked land candidate. Shelved by [#37](https://github.com/wstein/whr-anchorage/issues/37). If a named workflow later warrants reopening it, reassess M2/M4/M5/M6/M10 and seek explicit design approval. | Separate PR/rebase policy from local ff policy; exact reviewed heads and checks, no pending refs, stale remote refusal, fake-forge failure fixtures; owner sandbox live demo before any real publication. Native PR stack shape (child base = previous head branch): retarget/rebase sequence with exact-head review and checks re-evaluated. Required checks come from `rules/branches` (read access); unreadable classic branch protection is a reason to refuse. Go v0.2.1 land fixtures are a shared corpus. | 3 / 5 / 5 |
| M12 | Deferred conflicts D and owned resolve worktrees. Needs M1/M2/M5/M6/M7/M0 and accepted D spike. | Real conflicted branches stay at old tips; pending metadata/ref transaction; parent-first resolve; abort preserves pending; stale CAS; binary/delete conflicts; undo; push/land/ready refuse pending state. | 4 / 5 / 5 |
| M13 | Demand-driven follow-ups: watch, verify landed, metrics, repo profiles, Dayboard integration, merge-noop proof. The Rust watcher includes checks, mergeability, restack events and `--until-event`. See [watch acceptance](#watch-acceptance). Status issue lists are tracked separately in [#35](https://github.com/wstein/whr-anchorage/issues/35). | One owner-selected slice at a time (script/live acceptance only for behavior without a Go counterpart); define scope, contract, and new ratings before implementation. No blanket parity completion criterion. | 2 / 5 / 4 |

Recommended order: **M0 → M3 → M4** for workflow reads; in parallel as a separate workstream,
**accepted A → M1 → M2 → M5 (slim) → M6** for local stacks. M8 is independent after M3.
M7/M12 can follow local restack. M10 follows proven recovery and publication policy. M11 stays shelved.
M7 does not require publication; M6 does not push. M12 publication guards must land before pending state can
be created in a product used with push/ready commands. These are dependency tracks, not permission to start agents.
M0 starts directly in `projects/sourcesteward` without a spike (owner decision: infrastructure, the Git boundary is
already recorded in Chartroom). Scripts-first stays for new behavior without a Go counterpart; ports of Go behavior
(M3, M4, M8 and the single-branch part of M1/M2) go directly to product. The triage decision closed M9 for the standalone PR queue.

## Decisions, tradeoffs, and open evidence

1. **Recorded decision: Git boundary.** Read/merge through gix; installed Git only for push, ssh/local fetch,
   range-diff, and conflict resolution. HTTPS fetch stays gix. Mark each such output step `via: git`.
   Update `tests/it/no_binaries.rs` to permit only the runner module rather than removing the architecture check.
   Currently gix itself can spawn ssh/git-upload-pack for non-HTTPS fetch (`tests/it/remote_kinds.rs`); M0 must close
   that indirect path too. Keep authentication usable; exact environment scrub/credential allowlist and minimum Git
   versions need fixtures. Git version requirements differ: preview spike 2.40, resolve spike 2.45. Status 2026-11-12: Rust `stw` declares a minimum per command (`Op::min_version`: range-diff 2.41, index-reading ops 2.36, fetch 2.30; README "Git versions"); the 2.40 of the preview spike was wrong (`GIT_ATTR_SOURCE` is 2.41).
2. **Open: stack input.** Recommend explicit stack input first. Existing prototypes take `ONTO BRANCH...`; local
   upstream config does not reliably encode stack parentage. Keep explicit ordered input until owner accepts a durable
   local/PR edge model. Reject ambiguity instead of guessing from merge-base. Benefit 5, effort 2, risk 2. The decision
   stays open until the stack-model spike (`spikes/stw/stack-model`, branch `feat/stw-stack-model`, in review) has CLEAR
   and the owner accepted it live. It is needed before M2 and M6. Do not treat its unmerged design as accepted.
3. **Recommend recovery before new mutation.** JSONL manifest IDs do not retain objects; add retained ref snapshots
   and crash detection before restack. Full worktree atomicity remains unavailable; surface incomplete operations.
   Benefit 5, effort 4, risk 5. Keep tidy restore compatibility during migration.
   Superseded 2026-11-10 by the slimmed M5 (Cutover section): backup ref before each mutation, no crash journal.
4. **Recommend controlled binary transition.** `dayboard/LOGBOOK.md` records Go release v0.2.0 remains installed until M3+M4.
   Do not replace that channel with Rust while removing working review gates. Current installed binary/version and
   packaging channel are UNKNOWN in this assessment; owner chooses release/repository/channel and accepts live.
5. **Open: local CLEAR evidence.** Anchorage uses local review for fast-forward integration.
   A review note, local receipt, or external report could carry exact-head evidence. Author
   trust, head binding, and evidence storage are still open in
   [#9](https://github.com/wstein/whr-anchorage/issues/9).
6. **Recorded decision: replay attribution.** New commits carry the configured Git identity as author and committer.
   Commits rewritten by restack, rebase, or cherry-pick keep their original author; the committer is the configured
   identity, as `git rebase` does. A dotagents rule change for this is in progress.
7. **Defer in-memory rerere prediction (native jj: see decision 8).** Rerere reuse during Git resolution works without
   reimplementing Git's conflict-ID algorithm. jj-style structured conflict persistence needs another design, not
   merely commits containing marker text. Benefit 2, effort 5, risk 4 for native parity at this stage (historical; not
   pursued, decision 8).
8. **Recorded decision (owner, 2026-11-11): jj (Jujutsu) is not supported.** Git is the standard. Functions that jj
   offers (undo/oplog, conflict resolution) are added to stw instead of maintaining a second repository model. jj is an
   officially unsupported tool: it may work in a colocated Git repository, without a test and without a promise. There is
   no jj special code in stw (no detection, no warning, no test). This replaces "defer native jj" in the matrix and
   decision 7; the M13 item "native jj opt-in" is removed.

Known limits from the Rust README remain relevant: patch-ID oracle mismatches, partial checkout/rollback crash
windows, platform/filter/sparse-checkout uncertainty, and restore missing-object after pruning. Their historical
counts are not new measurements. Go and Rust test suites, full mutation sweeps, network tests, platform checks,
and all owner live acceptances were **not run** for this documentation assessment.

## Cutover and archive of legacy/sourcesteward

Owner direction (2026-11-10): replace the Go implementation and archive it; no two implementations side by side. Few new
features; migrate what is wanted from Go quickly, against the Go test cases. Acceptance rules: AGENTS.md ("Scripts first,
then product"). This section is the working plan; it does not replace the milestone table above (M-numbers refer to it).
"Proposal" means a proposal for the owner, not a decision.

### Port inventory

Source: `legacy/sourcesteward/internal/cli/root.go:182` (registration) and `stw --help` of a binary built from the gitlink
`b9ec1f2` (hidden commands read from the sources, marked h). 33 leaf commands; `help` is a custom help command (`root.go:183`) and is not counted.
The Rust column records the corresponding command or workflow where present. Go test counts come from source.

| Go command | Rust today | M | Disposition | Test source in Go |
| --- | --- | --- | --- | --- |
| `status` | present. Flags `--long`, `--fetch`, `--worktrees`, `--icons`, `--timeout`, `--stats`, `--verbose`, `--review`: **port (done)**, deviations below | M4 | **port**: core command. Deviation: Go refuses jj-owned paths (`status-unsupported-backend`); Rust has no jj detection (decision 8). Deviation: Go exits 1 when a remote read failed (`statuscmd/run.go` `Failure`); Rust prints a notice and exits 0, exit 1 only for a repository that cannot be read locally. Deviation: Rust text has the blocks `Counts`, `Needs action (N)`, `Repositories (N)`, `Notices (N)` and JSON `counts`/`totals` (additive, schema 2); open issues are read for the counts, one page of 100 (README "Status text"). Flags (README "Status flags"), deviation by deviation: `--long` has no compact counterpart (the Rust default text already prints the detail lines) and explains each reason from the facts instead of printing Go's `run:`/`hint:` commands; Go's priority levels, links and sources release drafts/tags do not exist in Rust; the source `ci-main` (default branch CI) is ported since 2026-11-17 (owner decision) with a fallback to Actions runs on a 403 for check runs, README "CI of the default branch in `stw status`" (deviations 1 to 10: no `behind_origin`, no shape classification, a red head is the reason `main-red`, notice `ci-not-read` per repository, exit 0). `--icons` covers the two repository markers only (no priority glyphs); the terminal default applies to every run, Go keeps ascii in its local layout unless the flag is given. `--fetch` uses the fetch of `sync` (runner for ssh/local paths, gix for https, `--no-prune`, no tags, no submodules), writes FETCH_HEAD, refuses a fetch refspec outside `refs/remotes/origin/` (addition), does not fetch the agent-rules checkout, passes `--no-prune-tags` (Go does not, so `pruneTags` makes Go's fetch create tags), works with `--offline`; the parallel bound is 16 as in Go (shared helper, also `sync`/`tidy`). Correction candidate (not changed, `sync` keeps Go behavior): `sync` without a prune flag has the same `pruneTags` gap. `--timeout` cuts the GitHub reads (pulls, issues, ci-main) only, request cap 30 s (Go 15 s), notices per repository, exit 0. `--stats`/`--verbose`: sources pulls/issues/ci-main/other (the comment reads of `--review` count as `pulls`, as in Go), `cached 304s` always 0, no `viewer` line. `--worktrees`: stale registrations are listed (`missing`, `prunable`) but not counted in `Worktrees` (Go counts them in `linked`), no `N linked worktrees (...)` summary line or per-state counts, sorted by path, JSON `repos[].worktree_list` instead of Go's `worktrees.list`. Was `status --worktrees` covered before? No (resolved, was UNKNOWN): only the number in `Worktrees` and, in `tidy plan`, the worktree of a kept branch and the stale registrations; the list with dirty/locked/prunable/missing is new. `--review` (port, done; README "Review state in `stw status --review`"): one comment read per open pull request, M3 state for the exact head, at most 200 pull requests per run and 16 reads at a time, a failed or cut read is `?` plus a notice, exit 0. Deviations from Go, one by one: (1) the M3 states (`no-review`, `unverified-author`, `unknown`) instead of `none`, `clear-stale`, `unparsed`; (2) the M3 grammar (D1 to D8), a line for another pull request is the finding `wrong-pr`; (3) no commit-date read of the head (Go ignores comments older than the head commit; Rust binds the verdict to the head id, but an old comment edited later counts at its `created_at`, flagged `edited`); (4) carry-over chains of M3 (16 links, declared patch-ids, `FINDINGS` of a chain head ends it) instead of 8 links; (5) trust per login derived from OWNER, MEMBER or COLLABORATOR associations, no `ignored` count or `foreign-review-ignored` hint; (6) a long thread is read up to 500 comments and then `unknown` (Go: first page plus last five pages, `truncated`); (7) hints: `clear-not-ready` and `base-not-default` are ported (data of the pull request list, no extra read; default branch from `base.repo.default_branch`, no `base-not-default` without a known base and default branch); `no-final-review`, `truncated-review-unknown`, `foreign-review-ignored` (covered by the states) and `ready-mergeable` (needs readycheck groups) are deliberately not ported, `parent-merged-retarget` (extra read of closed pull requests per repository with stacks) is a follow-up candidate; no per-group counts; (8) the state of every pull request in text and JSON (`review`, `review_state`, `pull_requests.review`, `options.review`), not only counts; (9) limit 200 per run in repository order, no `--limit`, 16 parallel reads instead of 4 repositories, stop after a rate limit (429, or 403 with exhausted limit, `Retry-After` or "rate limit"/"abuse detection" in the first 4096 bytes of the body, as Go) for the comment reads of the whole run: not started reads are `?` plus one notice `rate limit reached, N reads skipped`, exit 0; Go stops all sources of the run, Rust only the comment reads; (10) exit 0 for a failed review read (Go: 1). JSON additive in schema 2: `notices` (code, source, repos, message, count; the same list as the text block `Notices (N)`, README "Output for agents"; Go has code, source, repos, message), `options`, `stats`, `repos[].fetch`, `fetch_error`, `worktree_list`, `ci_main` (new: the CI of the default branch), `options.review`, `pull_requests.review` (with `hints{clear_not_ready,base_not_default}`), `open[].review_state`, `open[].hints`, `open[].base`, `open[].default_branch`. | `internal/statuscmd` (40), `statusremote` (109), `cli/status_*_test.go`; goldens pin Go text and schema v1, reuse inputs not goldens |
| `sync` | present: acts by default like Go (owner decision 2026-11-11, "standard wie Go: ausführen"); `--plan`/`--dry-run` preview | - | **port** (done): fast-forward of every repository. Deviation: `--apply` is a hidden, ignored alias (no Go flag), a usage error together with `--plan`/`--dry-run`; the Rust plan fetches `origin` like Go's (owner decision 2026-11-13), deviations in README "Deviations from Go in these flags", 14. Deviation: Go refuses jj-owned paths (`sync-unsupported-backend`); Rust has no jj detection (decision 8). | `internal/stwsync` (162), `cli/sync_test.go` |
| `sync --plan`, `--dry-run` (flag) | present (both, the same flag; JSON `mode` `dry-run`, `options.dry_run`) | - | **port** (done, 2026-11-11): the preview; since 2026-11-13 it fetches the remote-tracking refs of `origin` like Go's `--plan` (also `--rules --plan`), not with `--offline`. Deviations: README "Deviations from Go in these flags", 1. | same |
| `sync --ff-only` (flag) | the Rust behavior | - | **port** (done). | same |
| `sync --rebase`, `--merge`, `--autostash`, `--no-autostash` (flag) | present: `stw sync --rebase|--merge [--autostash|--no-autostash]` | - | **port (done)** (owner decision 2026-11-12: sync flags package; no spike, no live acceptance, AGENTS.md "Ports of existing Go behavior"): a diverged branch is rebased onto the fetched upstream (`git rebase --no-update-refs --fork-point`) or merged (`git merge --no-edit --ff`) by the installed git; a fast-forwardable branch is still fast-forwarded; tracked local changes refuse unless `--autostash`; the journal entry (recovery ref, no planned tip) is written first, so `sync restore` undoes it; a conflict stays in progress as git leaves it (`conflict`, exit 1; `autostash-conflict` when the stash cannot be reapplied). Deviations from Go, one by one (README "`sync --rebase` and `--merge`"): the Git configuration (`pull.rebase`, `branch.<name>.rebase`, `pull.ff`, `rebase.autoStash`, `merge.autoStash`) is not followed, only the flags; no `--ff-only` flag and no `--rebase-merges`; no hook runs (runner default); a `filter.*` driver, a `merge.<name>.driver` or another program-starting key in the repository config is refused; `--merge` refuses any tracked change without `--autostash` (Go: overlap only); the gate ignores a changed submodule (as Go); a branch that is only behind is fast-forwarded with tracked changes that do not overlap, `--autostash` is then without effect (Go refuses a rebase there); the argument vector pins `--no-rebase-merges`, `--no-update-refs` and `--no-verify-signatures`, and `branch.<name>.mergeoptions` refuses `--merge`; the stash entry is not claimed (no `stash_oid`, no `orig_head`); a stopped run is journaled `failed` (Go: `conflict`); Git 2.38 for rebase, 2.36 for merge. | `tests/it/sync_pull.rs`; Go: `internal/stwsync/pull_test.go`, `stash_test.go` |
| `sync --prune`, `--no-prune` (flag) | present (`src/sync/fetch.rs`, `src/git_runner.rs` `Op::FetchOriginPrune`) | - | **port** (done): `fetch --prune --no-prune-tags` / `--no-prune --no-prune-tags`, mutually exclusive, in a run and in a plan (a plan fetches; `--plan --offline` only shows the scope). Deviations: always the installed git, also for `https://` (no GitHub token for it); refuses, in a run, a `remote.origin.fetch` that writes outside `refs/remotes/origin/`; never prunes tags; reports the deleted refs as a list (Go: a number), also after a failed fetch; only `origin` (Go: the branch's remote); without a flag gix never prunes https. README "sync --prune". | `internal/stwsync/pull_test.go`, `policy_test.go`, `cli/sync_test.go` |
| `sync --submodules` (flag) | present (`src/sync/submodules.rs`; `Op::SubmoduleSync`, `Op::SubmoduleUpdate`) | - | **port** (done): moves detached pinned submodules to the recorded commit when that is a fast-forward; in a run only, `--plan` shows `would-move`. Deviations: nested submodules are walked and stepped against the submodule that holds the gitlink; stricter path charset, applied to every command (README "Submodules: the gitlinks of the index"; Go checks charset/symlinks only for a `sync --submodules` run, `sync.go:166`, `262-276`); `unreachable` is `error` (`--plan`: `skip`); a run fetches the submodule's `origin` before the check, and a failed fetch means no move (Go moves anyway); the dry run predicts from the index gitlink before the superproject's fast-forward; filter drivers in the submodule config are refused. README "sync --submodules". | `internal/stwsync/submodules_test.go` |
| `sync --rules` (flag) | present with `--rules-path`, `--owner-identity`, `--allowed-committer` | - | **port** (done); owner and committer check **ported** (2026-11-12): every new commit has an owner identity as author and an owner identity or `--allowed-committer` as committer, else the update is refused in a plan and in a run (`not by an owner identity: ...`). Deviations: both flags are a usage error without `--rules`; without an identity at the workspace root the rules checkout's configuration is used; the displayed info line for not-configured identities stays (README "agent-rules"). | `tests/it/sync_rules.rs` |
| `sync restore` (mode) | present: `stw sync restore [PATH] [--run ID]` | M5 | **port (done)** (owner decision 2026-11-11, MCQ: "Go journal in full"; no spike, no live acceptance, AGENTS.md "Ports of existing Go behavior"): every `sync` run that moves a branch writes the Go journal (`<git common dir>/stw/sync/RUN_ID.json`, the Go record format) and the recovery ref `refs/stw/recovery/sync/RUN_ID/N` before the ref moves; `restore` moves each branch back to its journaled old tip only where it still stands on the tip the run left (`changed-since` otherwise), the default run is the highest ID, a second restore is `already-restored`; retention of ten completed runs per repository as in Go. The slimmed M5 (backup ref) is not needed for this; it stays the way for the new mutations of M6 and later. Deviations from Go, one by one (README "`sync restore`", "The journal of a `sync` run"): workspace is `PATH` or `--root`, repositories are the root and the gitlinks of the index, nested ones too, as Go walks them (README "Submodules: the gitlinks of the index"); a malformed `--run` is a usage error (exit 2); stricter record checks (valid branch name, 40 digit OIDs only, a symbolic link as record or directory is refused and reported); any operation in any worktree of the repository stops its restore; a symbolic branch ref is `changed-since`; a failed journal write stops the fast-forward (`journal-failed`) and a refused check leaves no record (Go: a `failed` entry); fast-forwards, rebases and merges are journaled (the last two with no planned tip, `new_oid` set when they complete); recovery refs and records are written through gix (a run needs no `git`), `restore` goes through the runner and needs `git` (refused when the repository's own config names a program); JSON in the Rust layout (`run_id`, `repos[].journal`, `mode` `restore`). | `internal/stwsync/restore_test.go`, `retention_test.go`, `cli/sync_test.go` |
| `tidy plan` | present, Git only | M5 | **port** (done); jj bookmarks and owned-workspace retirement **drop** (owner confirmed 2026-11-11: jj is not supported). Addition without a Go counterpart (2026-11-18): the proof `similar-patches` (decision `review`, never `delete`) with `--similarity`, `--similarity-low`, `--no-similarity`, README "Similar patches in `tidy plan`", deviation 16 of the flags. | `internal/tidywizard` (97), `cli/tidy_*_test.go` |
| `tidy apply` | present, Git only | M5 | **port** (done); backup ref is M5 (slim). | same as `tidy plan` |
| `tidy restore` | present; no journal listing | M5 | **port** (done). | `cli/tidy_v3_restore_test.go`, `tidy_prune_v4_restore_test.go` |
| `tidy history` | present: `stw tidy history [--limit N] [--manifest-dir DIR] [--json]` | M5 | **port (done)** (owner decision 2026-11-11, MCQ: "Go command 1:1"): lists the stored journals newest first with counts, status and every bound as a visible truncation, read-only. The journals are the manifests of `tidy apply`, whose format is unchanged. Deviations from Go, one by one (README "tidy history"): journals are the manifests, not the contract-3 plan and journal files; `--contract`, `--state-dir`, `PATH`, `tidy restore --list` and the contract-4 history (administrative registration prune) are not ported; no `plan_id`, `retained`, `recovery_refs`, `decision_kinds`, `run_mode`; `status` has no `incomplete` for a killed run; order by run ID; a missing directory is an empty history. | `cli/tidy_prune_v4_history_test.go`, `workspace/tidy_v3_history_test.go` |
| `review carry-over` | present | M4 | **port (done)**: reuses a CLEAR after a rebase; proof is equal patch-id, no merge commits, range-diff all `=`, and a fourth condition added with `stw prove` (2026-11-11): the patch-id of every commit is equal, old against new at the same position, asked when the first three held (range-diff shows only "Binary files differ" for a binary change; Go compares the commits themselves). Needs `range-diff` in the runner. | `internal/carryover` (46; repositories are built in `fixture_test.go`, no data files), `cli/review_test.go` |
| `prove` (new) | present: `stw prove ALT NEU [--base BASIS] [--old-parent REV] [--pr N\|none] [--json]` | M4 | **new, no Go counterpart** (owner go and live acceptance, 2026-11-11; LOGBOOK: 14 PASS with restack plan): proves locally, without a pull request and without network, that a rewritten branch is the old one rebased and nothing else. The conditions of `review carry-over` (the same `carryover::check`, no second path; it gained a per-commit patch-id comparison for both commands, because range-diff cannot tell two binary contents apart; nearer to Go, which compares the commits) plus: same set of changed paths, author and committer of every new commit are `user.name <user.email>` of the Git configuration (read only), every `Co-Authored-By` line of an old commit is kept at the same position, no session link in the new commit texts (the rule of `lint pr`). Verdict `proved`/`not proved` first, the `Carried-over:` line last for `proved`. Git boundary: only `range-diff` through the runner; revisions, merge base, commit walk, trees, patch-id, identity and trailers through gix. Named behavior: `ALT == NEU` is `not proved` (`same-commit`; the Go carry-over accepts identical heads); a rebased commit of a foreign author is `not proved` (identity); one revision per side, no ranges; an unknown revision is `not proved` (exit 1), not a usage error; the old base is the merge base of `ALT` and `NEU` or, with `--old-parent REV` (the Go and `restack plan` name; the old parent of `ALT`, for a child branch whose parent was rebased), `REV`; the new base is the same commit or `--base`. README "Local rebase proof". | tests: `tests/it/prove.rs` (29 cases); Go has none |
| `ready-check` | present (`src/ready_check.rs`) | M4 | **port**: pure `Evaluate` over seven conditions (open, review, checks, base, blocker, conflict, rebaseable). Review condition comes from the Rust M3 state. | `internal/readycheck/{check,run,command}_test.go` |
| `lint pr` | present (`src/lint_pr.rs`, `src/lint_pr_cmd.rs`) | M8 | **port** (done): PR text rules; deviations in "Slice 3". | `internal/lintpr` (15), `lintprcmd` (17), `cli/lint_test.go` |
| `lint review`, `lint pr-text` (new) | present (`src/lint_files.rs`) | M8 | **new, no Go counterpart**: the text of a review comment or a PR text from a file, read-only, no network. Shares the functions and rule IDs of `lint pr` (`line_count`, session pattern, `review::parse_strict`); adds `text-line-endings`, `review-patch-id`, `review-head`, `pr-generated-line`. Named behavior: exit 3 for an error finding, as `lint pr`; `lint pr-text` has a Draft/Ready switch (`--ready`: `Blocker: None.`); the `#n` cross-repository check is not part of it. README "Text files". | tests: `tests/it/lint_files.rs` (23 cases); Go has none |
| `pulls` | no separate queue. GitHub reads, `stw status --review` and `stw ready-check` exist | M9 | **drop** the standalone command. Use GitHub for failed job names and sorting ([#15](https://github.com/wstein/sourcesteward/issues/15)). | `internal/pulls` (85), `pullscmd` (65), `prviews` (17), `cli/pulls_test.go` |
| `restack plan` (h) | present: `stw restack plan [BRANCH...] [--onto BASIS] [--old-parent REV] [--json]` | M1/M2 | **port (done) plus whole-stack preview (new)** (owner go, 2026-11-11; no spike for the port part; the whole-stack part and the auto-detection are new behavior without a Go counterpart; owner accepted the plan live on 2026-11-11). Read-only: no ref, file or object on disk, no network, no `git` process (gix only; the in-memory engine of M1). Port: the single-branch prediction `BRANCH --old-parent REV --onto BASIS` (replay, predicted conflicts with paths and kinds, empty commits, merge or root commit refused, old parent no ancestor refused, unrelated histories refused). New: the chain `A B C` replayed level by level, a level above a conflict is `inherited-conflict` (not computed; the spike continued with conflict-marker commits), the chain derived from the local branch topology when no branch is named (starting at the checked-out branch; two candidates, a fork, two names at one commit, a detached head, no common history: refused with the explicit spelling as suggestion), `--onto` default `origin/HEAD` else usage. Fail closed: a level that would be clean is `unknown` with the reason when the replay differs from git (`merge.renames`, `diff.renames`, `merge.renameLimit`, `diff.renameLimit` (pinned 7000, the merge default of git, measured), `diff.algorithm` not at the pinned value, a `merge` attribute in a tree, in the range or in `info/attributes` of the common dir, a commit that changes a submodule); the local default branch is never a level. Limits against a real `git rebase` (directory renames approximated, rename detection pinned, binary files, submodules, merge drivers, rerere unknown, skip by patch-id, dirty work tree not looked at): README "Stack preview". Deviations from Go, one by one (README "Deviations from the Go `restack plan`"): visible, other form and flags (short branch names, `--onto` any revision, old parent optional); not ported: range pairing (needs git range-diff), cherry classification, own-diff equality, identities and the outcome table, overlap, affected refs/worktrees/hooks, lease-push and resync texts, `--expect-head`, `--remote`, `PATH`, attribute and repository hazard refusals (replaced by `unknown`), refusal of a dirty target, operation in progress and bare repository, jj/GitButler refusal, exit status 3 and the byte bound; the gix engine instead of `git merge-tree` (paths of four conflict kinds differ, `src/replay.rs`); exit 0 only for `plan: clean`; own output and JSON (`restack-plan`, schema 2). The oracle is real `git rebase` in disposable repositories (`tests/it/restack_plan.rs`). Go cases that map: `go_*` in that file (clean replay against a real rebase, predicted conflict stops the replay and names the paths, conflict kinds, empty commits, old parent must be an ancestor, unrelated histories, merge commit refused, options validated, nothing written); the others of the 54 + 24 Go tests belong to the evidence that is not ported. | `internal/restack` (54), `conflicts` (24), `cli/testdata/restack-plan-*.json` |
| `land` | Git and `gh` for PR workflows | M11 (historical) | **drop**, owner decision [#37](https://github.com/wstein/whr-anchorage/issues/37). | `internal/land` (42), `landcmd` (25) |
| `watch` | `stw watch --once`, `--stream` (`--interval`, `--max-idle`, `--until-event`) and `--status` | M13 | **port**. W5 accepted on 2026-11-26. See [watch acceptance](#watch-acceptance). | `internal/watch` (89), `watchcmd` (50) |
| `issues` | counts in `stw status`, full lists pending | - | **drop** the standalone command. Add full lists to default `stw status` and `stw status --json` ([#35](https://github.com/wstein/whr-anchorage/issues/35)). | `internal/issues` (39), `issuescmd` (28) |
| `workspace plan` | `workspace worktree-plan` | - | **drop** the Go wizard interface ([#36](https://github.com/wstein/whr-anchorage/issues/36)). | `internal/workspace` (965, `testdata/`), `internal/acceptance` |
| `workspace apply` | `workspace worktree-apply` | - | **drop** the Go wizard interface ([#36](https://github.com/wstein/whr-anchorage/issues/36)). | same |
| `workspace handoff` | `workspace checkpoint` and `workspace resume` | - | **drop** the Go wizard interface ([#36](https://github.com/wstein/whr-anchorage/issues/36)). | same |
| `workspace retire-plan` | `workspace retirement-plan` | - | **drop** the Go wizard interface ([#36](https://github.com/wstein/whr-anchorage/issues/36)). | same |
| `release check` (h) | owner release workflow | - | **drop**, owner decision [#37](https://github.com/wstein/whr-anchorage/issues/37). | `internal/release` (356), `cli/release_*_test.go`, `releaseworkflow` |
| `release plan` (h) | owner release workflow | - | **drop**, owner decision [#37](https://github.com/wstein/whr-anchorage/issues/37). | same |
| `release verify` (h) | owner release workflow | - | **drop**, owner decision [#37](https://github.com/wstein/whr-anchorage/issues/37). | same |
| `release record` (h) | owner release workflow | - | **drop**, owner decision [#37](https://github.com/wstein/whr-anchorage/issues/37). | same |
| `release build` (h) | owner release workflow | - | **drop**, owner decision [#37](https://github.com/wstein/whr-anchorage/issues/37). | same |
| `release dispatch` (h) | owner release workflow | - | **drop**, owner decision [#37](https://github.com/wstein/whr-anchorage/issues/37). | same |
| `hosting repo` | GitHub and `gh` for direct queries | - | **drop** the wrapper ([#33](https://github.com/wstein/whr-anchorage/issues/33)). Product GitHub reads remain internal. | `cli/testdata/hosting-repo.json`, `internal/hosting` (327) |
| `hosting pr` | GitHub and `gh` for direct queries | - | **drop** the wrapper ([#33](https://github.com/wstein/whr-anchorage/issues/33)). Product GitHub reads remain internal. | `cli/testdata/hosting-pr.json` |
| `hosting prs` | GitHub and `gh` for direct queries | - | **drop** the wrapper ([#33](https://github.com/wstein/whr-anchorage/issues/33)). Product GitHub reads remain internal. | `internal/hosting/testdata` |
| `hosting ci` | GitHub and `gh` for direct queries | - | **drop** the wrapper ([#33](https://github.com/wstein/whr-anchorage/issues/33)). Product GitHub reads remain internal. | `cli/testdata/hosting-ci.json` |
| `inspect` | checkout-free PR inspection tracked in [#28](https://github.com/wstein/whr-anchorage/issues/28) | - | **drop** the Go command ([#33](https://github.com/wstein/whr-anchorage/issues/33)). | `internal/inspection` (529) |
| `inventory` | `stw status --worktrees --json` | - | **drop**. Add missing worktree fields for concrete workflows ([#33](https://github.com/wstein/whr-anchorage/issues/33)). | `internal/inspection` |
| `repos` | `stw status --json` | - | **drop**. Use the status repository overview ([#33](https://github.com/wstein/whr-anchorage/issues/33)). | `internal/fanout`, `cli/repos_test.go` |
| `pins` (h) | submodule pin drift in `stw status` | - | **drop** ([#33](https://github.com/wstein/whr-anchorage/issues/33)). | `cli/pins_test.go`, `cli/testdata/pins-*.json` |
| `version` | `stw --version` | - | **drop** the subcommand and its JSON output ([#33](https://github.com/wstein/whr-anchorage/issues/33)). | `cmd/stw/main_test.go` |

Also in the Go repository: `cmd/gh-jj` was dropped by the owner on 2026-11-11.
The owner retired `cmd/stw-docs` in [#33](https://github.com/wstein/whr-anchorage/issues/33). The [command reference](command-reference.md) is generated from the Rust CLI definitions. [#39](https://github.com/wstein/whr-anchorage/issues/39)

Tally: 34 items = the 33 Go leaf commands plus the `sync restore` mode. Flag rows, the two
extra Go binaries, and commands without a Go counterpart are outside this count.
Current dispositions: 12 port, 22 drop.

Test corpus: the Go fixtures are mostly Go code (repository builders such as `internal/carryover/fixture_test.go`), few
data files (`cli/testdata`, `internal/hosting/testdata`). "Shared corpus" therefore means: per Go test, one named Rust
case with the same scenario, or a named deviation. Where the Rust rules are stricter than Go the Rust rule wins:
- Review lines (M3, M4): Go accepts a 64-hex patch-id, drops a trailing CR and accepts PR numbers of at most 9 digits
  and no `pr=none` (`internal/reviewstate/parse.go:36-54`). Rust: exactly 40 hex, no CR, `pr=none` for local reviews.
  The Rust corpus today comes from dotagents (`tests/review_corpus/`), not from the Go tests.
- PR lint and queue rules (M8, M9): not compared with the current dotagents rules; UNKNOWN until each rule is checked.
- Golden outputs of `status` pin Go text and schema v1; Rust has schema 2.

### Owner decisions on the remaining interfaces (2026-11-25)

The inventory records the decisions under [#32](https://github.com/wstein/whr-anchorage/issues/32). Follow-up work is tracked separately.

- **Command reference.** Publish [Markdown in the SourceSteward docs](command-reference.md).
  Generate it from the Rust CLI definitions and test that the checked-in reference
  matches the generated output. [#39](https://github.com/wstein/whr-anchorage/issues/39)
- **PR triage.** The trial covered 76 open PRs. Use `stw status --review` for the list,
  `stw ready-check` for exact-head evidence, and GitHub for sorting and failed
  job names. The owner chose to drop the standalone `pulls` command.
  [Decision](https://github.com/wstein/sourcesteward/issues/15) · [Trial](https://github.com/wstein/whr-anchorage/issues/34)
- **Open issues in status.** By default, terminal and JSON status will list all
  open issues for the main repository and its GitHub gitlink repositories, with
  counts retained as a summary. Batch repositories in GitHub GraphQL and fetch
  further pages per repository and for any paginated issue metadata the output
  promises. Group terminal output by repository and show each issue's number,
  title, labels and assignees. JSON also carries the repository identifier,
  issue URL and last update time. Preserve successful results on partial
  failure, identify the affected repository and mark coverage incomplete. An
  empty list requires a successful read. The local spike must demonstrate a
  second page and partial failure before live acceptance and the Rust port. [#35](https://github.com/wstein/whr-anchorage/issues/35)
- **Workspace entry and handoff.** Native `workspace worktree-plan` and
  `workspace worktree-apply` shipped in [`356a989`](https://github.com/wstein/whr-anchorage/commit/356a989a2a23d14fb1183e025bc1d8447447dd90).
  Rust also has `workspace checkpoint`, `workspace resume` and
  `workspace retirement-plan`. Revisit a guided entry point if a concrete
  usability problem warrants it. The ref-recovery check for changes since the
  preview remains pending in [#29](https://github.com/wstein/whr-anchorage/issues/29). [#36](https://github.com/wstein/whr-anchorage/issues/36)
- **Publication.** In repositories that use PRs, agents can push their own topic
  branches, open drafts, append commits, and rebase or rewrite branches they
  own or have explicitly taken over. Use `--force-with-lease=<ref>:<observed SHA>`
  for a rewrite and stop to inspect if the remote head has moved. An independent
  agent reviews the exact PR head. Once that head has independent CLEAR, the
  agent verifies that it is still current, marks the draft ready for review,
  and hands it over to the owner for the merge decision. A changed head
  requires fresh CLEAR under that repository's rules before handoff. The owner
  also decides when to publish releases.
  Anchorage retains independent CLEAR and fast-forward to main under its own
  rules. Revisit publication automation only for a named Rust review workflow
  with exact-head evidence and fast-forward rules. Track the evidence decision
  in [#9](https://github.com/wstein/whr-anchorage/issues/9). [#37](https://github.com/wstein/whr-anchorage/issues/37)

### Cutover criteria

A Go item meets the cutover criteria through an accepted Rust port or an explicit
owner decision to drop it. A port passes the tests derived from Go, documents its
deviations, and receives independent CLEAR on the exact head. Before archiving,
the owner resolves each deferred command as a port or a drop. The criteria
cover all 34 inventory items, every flag row, and the two extra binaries. Then:
1. Owner: archive the Go repository `wstein/sourcesteward-go` (GitHub setting).
2. Owner: remove the gitlink `legacy/sourcesteward` or freeze it at the last Go commit.
3. Agent: update the references in workspace, AGENTS, Chartroom and skills (`grep -rn "legacy/sourcesteward"`); changes to
   skills go by pull request in `wstein/dotagents`, on owner request.
4. Go v0.2.1 was the last Go release. The [Homebrew formula](https://github.com/wstein/homebrew-tap/blob/main/Formula/stw.rb) now installs Rust v0.5.0. Track the remaining cutover evidence in [#12](https://github.com/wstein/sourcesteward/issues/12).

### CI and release

The owner chose CI first on 2026-11-26. The [SourceSteward CI workflow](../.github/workflows/stw-ci.yml)
runs `cargo test --locked` on Linux and macOS on each push to `main` and
supports manual runs.

The release build produces four archives for Linux and macOS on x86-64 and
ARM64, plus `SHA256SUMS`. The owner published
[Rust v0.5.0](https://github.com/wstein/sourcesteward/releases/tag/v0.5.0)
and the Homebrew tap has switched. The macOS installation smoke-test evidence
still needs to be recorded in [#12](https://github.com/wstein/sourcesteward/issues/12).

### Live acceptance evidence

Owner acceptance is recorded for the scope exercised by each run. Go ports
follow the test and review criteria above.

| Accepted scope | Date and result | Evidence |
| --- | --- | --- |
| `stw prove` and `stw restack plan` | 2026-11-11, 14 checks passed, including `--old-parent`, binary files, and a linked worktree | [Logbook](https://github.com/wstein/whr-anchorage/blob/main/dayboard/LOGBOOK.md) |
| Rust `stw status` | 2026-11-11, 17 checks passed. `status-21` remained open. | [Logbook](https://github.com/wstein/whr-anchorage/blob/main/dayboard/LOGBOOK.md) |
| `stw add` attribute validation | 2026-11-11, 23 checks passed. `add-lfs-01` and the `whr-add` migration remained open. | [Backlog](https://github.com/wstein/whr-anchorage/blob/main/dayboard/BACKLOG.md) |
| `stw absorb` Python plan spike | 2026-11-25, accepted at `7a3163d`. Steps 1, 2, 3, 3b, and 5 passed. Optional step 4 was skipped. | [D-20261125-01](https://github.com/wstein/whr-anchorage/blob/main/dayboard/DECISIONS.md) |
| `stw absorb` Rust plan | 2026-11-25, accepted on the binary from `52e973c8`, with the port introduced at `613a27e8`. Steps 1, 2, 3, 3b, 5, 6, 7, 8, and optional 9 passed. | [Logbook](https://github.com/wstein/whr-anchorage/blob/main/dayboard/LOGBOOK.md) · [D-20261125-09](https://github.com/wstein/whr-anchorage/blob/main/dayboard/DECISIONS.md) |
| Rust daily-use fixture on macOS | Owner reported 38 offline checks passed. The exact SHA is unverified. Live GitHub access remains open. | [Logbook](https://github.com/wstein/whr-anchorage/blob/main/dayboard/LOGBOOK.md) |
| GraphQL issue-list and review-handoff spikes | The owner accepted the spikes in conversation. | [Issue-list PR #46](https://github.com/wstein/whr-anchorage/pull/46) · [Handoff issue #30](https://github.com/wstein/whr-anchorage/issues/30) |

The [`stw absorb --apply` recipe](../acceptance/rezept-stw-absorb-apply.sh)
passed on Linux at `33d62f45` on 2026-11-26. The planned hunk reached HEAD,
the skipped hunk remained staged, the backup pointed to the old HEAD, and
the index plus the tracked and untracked fixture files stayed byte-identical.
The owner accepted this result on 2026-11-26.

The [GraphQL issue-list recipe](../acceptance/rezept-stw-status-issues.sh)
passed against GitHub on Linux at `33d62f45` on 2026-11-26. Terminal and
JSON lists agreed across the workspace. An unavailable submodule was marked
incomplete while the other repositories remained complete. The owner
accepted this result on 2026-11-26.

The [review-handoff recipe](../acceptance/rezept-stw-review-handoff.sh)
passed on Linux at `33d62f45` on 2026-11-26. The selective commit used the
staged version and preserved unrelated staging and worktree edits. A stale
plan was refused. The conflict report reopened with all three saved blob
versions and detected a moved ref, leaving its new tip in place. The owner
accepted this result on 2026-11-26. The full
[cross-agent walkthrough in #31](https://github.com/wstein/whr-anchorage/issues/31)
remains a separate check.

### Watch acceptance

[#38](https://github.com/wstein/whr-anchorage/issues/38) tracks W2 through W4.
W2 and W3 shipped in #41 and #42. W4 is on `main` at `d69caa1`.
[#32](https://github.com/wstein/whr-anchorage/issues/32) records the independent
CLEAR and 21 targeted tests. The live W4 baseline reached GitHub successfully
and emitted no event, as expected for a silent baseline. On Linux at
`33d62f45`, the targeted Rust watch suite passed 157 tests with one ignored.

The W4 live check passed on 2026-11-26 with draft
[#51](https://github.com/wstein/whr-anchorage/pull/51). The first push
emitted a byte-identical `head-changed` event on stdout and in
`journal.jsonl` for `f9ad5be3b633708fd67d92ae5dc24cacd3011fe5`. Codex
relayed the independent reviewer’s CLEAR as a
[submitted GitHub review](https://github.com/wstein/whr-anchorage/pull/51#pullrequestreview-6074490763)
on that commit. The review condition in `ready-check` passed. After the
second push, the review condition failed with no CLEAR for the new head
`69790721a3435189b561ffd1e06c37c1b7a1ad6e`. The PR stayed a draft.

For W5, two exports with Go 1.27.2 from the legacy gitlink `90bde0a`
produced 279 scenarios each. The exports matched each other and the tracked
JSON byte for byte. The oracle needs a temporary directory outside a Git
worktree. The Rust suite exercises the versioned JSON output and strict
state decoder, including the documented separation from Go state. The targeted
mutations `WC2`, `WC8`, and `WC10` each failed their designated test.
Independent review of the oracle, Rust test evidence, and versioned output
found no technical blocker. The owner accepted W5 on 2026-11-26.

### Order of the ports

Owner decisions of 2026-11-10 (chosen by selection):
- **Two port lines in parallel.** Line A: M3 -> M4 -> M8 -> M9 -> M10 (M10 needs the slim M5). Line B: M1 -> M2 -> M6
  (restack, stacks; M6 needs the slim M5).
  Reason: restack makes stacking cheap, and M3/M4 replace the review gates that keep Go v0.2.0 installed.
- **M5 is slimmed.** Instead of the full oplog: a backup ref `refs/stw/backup/<n>` before every mutation, evidence by
  `range-diff`, undo = set the ref back. No journal, no retention logic, no general undo for new mutations (`sync` has the Go journal and `sync restore` since 2026-11-11, a port of existing Go behavior). M6 may start without spike B.
  `tidy history` was tied to this question; the owner decided on 2026-11-11 to port it 1:1 (it lists the `tidy apply` manifests, no
  new journal), and `sync` got the Go journal with `sync restore` (a port of existing Go behavior). The slim M5 is unchanged for new mutations.
- **Ports without spike** apply as soon as AGENTS.md with that rule is on `main` (commit `docs(agents)` of this branch).
- **Go pages job:** the job `pages` on `main` of `wstein/sourcesteward` is red (Pages not set up). Decided: do nothing,
  the legacy repository is archived. Noise until the archive.
- At this stage the 13 deferred commands still needed a disposition, and the
  owner had confirmed the jj-related drops. The 2026-11-25 decisions above
  update the command dispositions. Any unrelated drop proposal still requires
  explicit owner approval. The inventory gives the current tally.
- M3 is in the product as a library (`src/review.rs`); M4 wires it into commands (`status --review`, `review carry-over`).
- M7, M11, M12 and M13 were deferred at this stage; see the milestone table and
  the later owner decisions for the current scope.

### Next slice

State, read from the repository (2026-11-10, main `ca219d0`):
- M0: `src/git_runner.rs` (802 lines) has the runner (argv fixed per `Op`, environment allowlist, bounds, timeout). The
  only `Op` is `FetchOrigin`; there is no `range-diff`. GitHub reads exist in `src/github/reads.rs` (comments, reviews,
  check runs, commit statuses). `gix` has no merge feature in `Cargo.toml` (needed only from M1).
- M3: `src/review.rs` (782 lines), `tests/it/review_lines.rs`, `tests/review_states.rs` with 99 files in `tests/review_states/`,
  `tests/it/review_read.rs`; merged with `8aa9940` and follow-ups up to `469a4f4`. Used by `status --review` (M4).
- M4: absent. No `review` or `ready-check` command, no carry-over check (patch-id/range-diff); `src/review.rs` only parses
  `Carried-over` chains.

Slice 1 (port, no spike), in this order, each a reviewable commit:
1. Go corpus check for M3: for each case in `internal/reviewstate/{parse,state}_test.go` one Rust case in
   `tests/review_states/` or a named deviation (64-hex, CR, `pr=none`, PR-number width). Done when every Go case is
   mapped; mutation of the parser fails the new cases.
2. `Op::RangeDiff` and the fetch of the PR head into a private ref in `src/git_runner.rs` (fixed argv, minimum Git
   version, bounded output). Go fetches `+refs/pull/N/head` into `refs/stw/carry-over/` (`internal/carryover/git.go:21,228`);
   the runner has only `FetchOrigin`. Tests in `tests/it/git_runner.rs`.
3. Carry-over core and `stw review carry-over --pr N OLD_BASE OLD_HEAD NEW_BASE NEW_HEAD`. The output is the exact
   `Carried-over:` line; nothing is posted. Mapping of the 46 Go tests (45 cases and `TestMain`) to Rust cases in
   `tests/it/carryover.rs` unless noted; each gets one case with the same scenario:
   - Request parsing: `TestParseRequestAcceptsOnlyFullLowercaseObjectIDs`, `TestParseRequestRejectsBadCounts`,
     `TestParseRequestValidatesPullNumberAndRemote` (Go allows a PR number of 1 to 999999999, `internal/carryover/types.go:165`).
   - Fixed argv, in `tests/it/git_runner.rs`: `TestGuardAllowsOnlyTheReviewedVectors`,
     `TestGuardRefusesAnExtraArgumentOnEveryShape`, `TestDiffAndLogPinEveryOutputFlag`,
     `TestRunnerRefusesInvalidIDsBeforeRunningGit`. Deviation: Rust fixes the argv in code per `Op`.
   - Fetch of the PR head (step 2): `TestFetchWritesOnlyThePrivateRef`, `TestFetchWritesNoConfiguredRefmapRef`,
     `TestNoHookRunsDuringTheFetch`, `TestMovedPullHeadRefuses`, `TestUnreachableRemoteIsUnknown`.
   - Patch-id: `TestPatchIDIgnoresRenameDetection`, `TestPatchIDSeesBinaryContent` map to `tests/it/patchid.rs` and
     `tests/vectors/` (add the case where missing); `TestNoDiffDriverRunsAndAWhitespaceChangeStaysNo`.
   - Runner environment, existing tests in `tests/it/git_runner.rs` or new: `TestInheritedGitDirDoesNotRedirectTheRunner`,
     `TestEnvironmentDropsTheRepositorySelectingVariables`, `TestACorruptIndexFileOfTheEnvironmentChangesNothing`,
     `TestCheckoutOutsideARepositoryIsAReadFailure`.
   - Hostile configuration: `TestAttributesThatHideTextDoNotHideACommit`, `TestSubmoduleIgnoreSettingsDoNotHideAGitlinkChange`,
     `TestVerdictsOfTheCleanConfiguration`, `TestVerdictsDoNotChangeUnderHostileConfiguration`, `TestOneHostileSettingAtATime`.
   - Output bounds: `TestParseLogRejectsMalformedOutput`, `TestRunnerBoundsTheCommitsPerSide`.
   - Output: `TestHumanOutputEndsWithTheLineOnlyForAYes`, `TestJSONHasTheContractKeys`, `TestLineFormat`. Deviation: schema 2
     instead of Go v1; the line has 40-hex patch-id and `pr=<n|none>`.
   - Scenarios: `TestPureRebaseIsCarriedOverWithTheExactLine`, `TestIdenticalHeadsAreCarriedOver`,
     `TestMergeCommitRefusesAndOnlyThatCondition`, `TestPatchIDChangeRefuses`,
     `TestWhitespaceOnlyChangeKeepsThePatchIDButRangeDiffRefuses`, `TestAddedEmptyCommitRefuses`,
     `TestReorderedCommitsRefuseByPosition`, `TestRenameWithContentChangeRefuses`,
     `TestRewrittenParentIsCarriedOverWithTheFourArgumentForm`, `TestMissingObjectRefuses`, `TestCheckWritesNothing`,
     `TestReadFailuresNeverBecomeAYes`, `TestEmptyPatchIDAndEmptyRangeRefuse`,
     `TestCommitComparisonIgnoresOnlyBlobIDsAndLineNumbers`, `TestCommitComparisonAddedRemovedAndReordered`,
     `TestPullHeadComparison`. Deviation: Go also takes a 64-hex patch-id; Rust takes 40 hex only.
   - `TestMain`: drop (Go harness; the Rust harness stays confined to scratch).
Slice 1 as built (branch `feat/stw-port-m4`): deviations from Go found by the corpus check and the port. Cases are named in
`tests/it/review_go_corpus.rs` (D1 to D8, M3) and `tests/it/carryover.rs` (D1 to D7, M4):
- M3: no head time (Go drops comments older than the head commit); trust by login list, not `author_association`; 16 links, not 8;
  a repeated head invalidates the chain; FINDINGS after a chain member invalidate every chain through it, also a carry-over posted
  before them; legacy words (`Verdict:`, `Urteil:`) are no review line; states: `no-review` includes Go `clear-stale`, `unknown`
  replaces `unparsed`. `NeedsHeadTime` and `Hints` are not ported (M9).
- M4: `git range-diff` replaces the Go commit comparison, so a reorder is `=` for git and refused by position here; `--fetch` fetches
  `origin` only (Go: `--remote NAME`); `--pr none`; a repository config that starts a diff program is refused (Go ignores it);
  schema 2; no `refs/replace` protection in process; the fetch of the PR head runs through the installed git for an ssh (also `ssh+git://`, `git+ssh://`), scp-like, `file://` or local-path `origin` only
  (`GIT_ALLOW_PROTOCOL=file:ssh`, so a user `insteadOf` to https fails in git)
  (git boundary); an http, https or git:// `origin` is refused typed (`transport`, answer no) before git starts, the https path through
  gix is not built (owner decides); a `core.sshCommand` of the user's config runs as with `FetchOrigin`. Conservative differences
  (no where Go says yes): only the author of a commit changed (range-diff names the author), an identical patch twice in a range
  (range-diff pairs it across, the position rule refuses). Notes are ignored (`--no-notes`), as in Go.
- `Op::RangeDiff` has no four-argument form in git; the argv is the two-range form.

Slice 2: `ready-check` (M4) from `internal/readycheck`.

Slice 3 (M8, branch `stw-m8-lint-pr`): `lint pr` from `internal/lintpr`, `internal/lintprcmd`, `cli/lint_test.go`. The 15 rule tests are
`tests/it/lint_pr.rs`, the 17 command tests and the 3 CLI tests are `tests/it/lint_pr_cmd.rs` (one Rust case per Go case, same name
in snake case, except three renames: `TestParseReviewLine` -> `parse_review_line_fields`,
`TestViolationsExitOneAndAreLocated` -> `violations_exit_three_and_are_located`, `TestLintPREndToEndOverTheGuardedReader` ->
`lint_pr_end_to_end_over_the_rest_client`; the CLI cases run the binary against a fake GitHub API). The hand-written regular expressions were also compared with
the Go originals on 140000 generated cases (rules and messages; 0 differences; the harness is not in the repository). Deviations
from Go:
- Output default: text, as every Rust command; Go prints text only on a terminal. `--human` stays and conflicts with `--json`.
- No `--host`: `api.github.com` or `STW_GITHUB_API_URL` only; `host` of the document is `github.com`.
- Document: schema 1 as in Go, no `engine`; the schema file stays in the Go tree; `time` is UTC seconds. The JSON text is raw: Go's
  `json.Encoder` escapes `<`, `>` and `&` as `\u003c`, `\u003e`, `\u0026`, Rust does not (same value after decoding).
- The `page` of a `Link` header is not percent-decoded (Go reads `page=%32` as 2, Rust rejects it); `page=+2` is rejected in both.
- Workspace: the single GitHub `origin` among the repository of PATH (default: current directory; a plain repository is a workspace of
  one) and its checked-out submodules; the Git identity is read with gix, not with `git config --get`.
- Human output escapes with `escape_untrusted` (also the backslash, `\u{hex}`), Go with `untrusted.Esc` (`\xNN`, `\uNNNN`); the
  author and committer message quotes the name with Rust's `{:?}`, Go with `%q` (they differ for non-ASCII and control characters).
- No regex crate: the expressions are written out by hand; case folding is lower case plus U+017F and U+212A (Go's simple folding for the
  letters that occur); other Unicode folds Go knows are not applied. JSON keys are matched case-sensitively (Go's `encoding/json` also
  takes `Title`).
- The review line is the M3 parser (`review::parse_strict`) restricted to the Go lint grammar: `pr=` is 1 to 9 digits, no `pr=none`.
- Redirects are not followed (as in Go); 404 reads as `forbidden`, a 301 or 308 as `repo-moved`, as in Go. Pages are cut at one MiB
  (a body of exactly one MiB is read, one byte more is not; `page-too-large` for comments, `malformed-response` for `pulls/N` and commits, as in Go; a 304 is `malformed-response`); every read has a 30 s timeout (Go: the request timeout of the reader).
- Not ported: the Go guard that admits only the three lint endpoints and the call accounting of the reader (the Rust client has GET
  methods only and three endpoint builders in `src/github/lint_reads.rs`).
- Rules table: the anchors cite `claude/CLAUDE.md` and `claude/skills/agent-workflow/SKILL.md` of agent-rules. The test
  `rules_table_anchors_in_agent_rules` runs only with `AGENT_RULES_DIR` (as in Go). The current `.agents` checkout of this workspace has
  no `claude/` directory, so the anchors are UNKNOWN against the current dotagents layout (see the "PR lint" line in "Test corpus").
The [multi-PR triage trial](https://github.com/wstein/whr-anchorage/issues/34) led to the decision to drop the standalone queue ([#15](https://github.com/wstein/sourcesteward/issues/15)).

## Op log readers (2026-11-12)

`stw oplog timeline | evolog | undo-plan` (read-only) over `docs/oplog-format.md`: new behavior, the formats were accepted
as spikes (`spikes/stw/oplog-reflog`, `oplog-hooks`, `oplog-refhook`), the Rust package is the first of the op log
stack. Deviations and limits: README, "The op log readers". Package 2 (stw's own `refs.log` lines for sync and tidy, the
rotation, `truncated`) is built: README, "stw writes its own ref updates". Next packages, in this order: the backup refs and `stw oplog undo` with CAS (spec 8, including the `core.logAllRefUpdates=false` test);
the hook install (opt-in per repository, `--yes`); the `--keep` path for a checked-out branch; then `stw restack
apply|undo` (conflicts resolved later through the op log, owner decision 2026-11-11).


## M6 preparation: first conflict-free apply and checked undo (2026-11-15)

This section supersedes the older M5/M6 status and ordering statements above, not their historical decisions. At workspace
`ed8a2bd`, `src/cli.rs` exposes `restack plan` only; M1 replay and M2 plan are present. The owner accepted `prove` and
`restack plan` live on 2026-11-11 (LOGBOOK: 14 PASS, including `--old-parent`, binary and linked-worktree cases).
The reflog spike was accepted; the hook-spike recipes and product `oplog undo`, `undo --keep` and hook-install live
acceptances remain open (DAYBOARD). Current product has the oplog readers, writer, backup refs, undo, `--keep`, hook install
and sync fetch. Issues #5, #6 and #8 describe earlier checkpoints; they do not supersede these later records.
The owner selected explicit bottom-up local branches and an explicit onto/trunk (#8). Auto-detection in the existing plan
is not an approval to infer inputs for this first apply. The owner also decided that conflict handling eventually continues
through oplog/evolog; the conflict-free slice below does not implement or settle that later flow.

**Proposed first package, not implemented:** a script in `spikes/stw/` followed by owner live acceptance, then a Rust
package and independent review. Restrict the first demonstration to one explicitly named local branch and an explicit
onto. Both resolve to pinned full commit IDs; the branch must be a direct, nonsymbolic local ref not checked out in any
worktree. The plan must be conflict-free, with supported linear history and a valid configured Git identity; no inferred
stack, checked-out branch, remote write, conflict continuation or automatic review carry-over. The operator sees the
branch, onto, old and proposed new tips and range-diff before an explicit apply. If replay cannot reproduce the shown
result, stop and require a new plan. The exact serialization, identity/time handling and lifetime of the plan are open
implementation decisions: a displayed plan must bind the inputs and the result actually applied, rather than assume
recomputed commit IDs are stable.

**Apply and recovery contract to demonstrate:**

1. Revalidate branch and onto identities and pinned tips, plan inputs, object availability, worktree occupancy and ongoing
   operations immediately before mutation. Refuse a conflict, stale or ambiguous input, missing object, symbolic ref,
   unsupported history, absent identity, unreadable checks or any selected branch checked out in any worktree. Never
   silently switch to `--keep`. Show the reason; before recovery preparation a refusal changes no branch, index or worktree.
2. Retain the original tip before the branch move using the existing oplog backup design: `refs/stw/backup/<run>/<k>`,
   its monotonic counter, run retention and logging rules in `docs/oplog-format.md` section 8. Do not use the obsolete
   `<n>` spelling above. A failed backup means no branch move. A later failure may leave counter, backup and pruning effects;
   report the actual stage and retained ref. Log writes are best effort with a visible gap/notice, not proof of a transaction.
3. Move the branch with a no-deref expected-old CAS; a lost race leaves the competing value untouched and the backup
   available. The onto tip must also be protected from changing between revalidation and the ref update; choose and prove
   a mechanism before implementation, or refuse rather than claim this guarantee. Ref CAS alone does not protect worktree
   registration: the existing undo spec explicitly documents that race. Do not claim atomicity across worktree state,
   refs, objects and logs. Record actual old/new tips and rewrite pairs needed for an actionable recovery report.
4. Checked undo identifies that exact apply and moves the same branch from its recorded new tip back to its retained old
   tip, only after fresh checks and an expected-current CAS. Preserve a backup of the current tip as required by section 8.
   If the branch moved, is checked out, the record or object is missing, or another check is unreadable, refuse without
   moving it. The output identifies the backup and recovery steps; a partial or interrupted operation is reported as such,
   never as success. This is local ref recovery, not undo of pushes, arbitrary files, index state or other worktrees.

**Acceptance evidence for the proposed slice:** in disposable repositories demonstrate a clean single-branch replay,
compare its tree and history with Git and show the range-diff; apply and checked undo restore the exact original ref while
other refs, index and worktrees stay unchanged. Exercise stale branch and onto, a competing ref update at CAS, checked-out
branches in this and another worktree, conflict, dirty/ongoing or unreadable state, failed backup, interruption after backup
and before/after branch movement, missing retained object and failed log append. For each assert the actual refs, retained
backup and reported recovery state; targeted mutations must make each new safety check fail for its intended reason.
Demonstrate backup survival through reflog expiry and GC only in a disposable repository. No full transaction or
power-loss guarantee follows from these tests. Present the script and its recovery demonstration live to the owner; passing
fixtures and prior plan acceptance do not constitute acceptance of apply or undo. Product implementation follows that gate.

**Owner decisions still needed before the slice is finalized:** whether single-branch, unchecked-only scope is sufficient
for the first acceptance; how the explicit plan and consent are presented; how a conflict-free subset relates to the later
decided continue-on-conflict behavior; and the accepted handling of the onto and worktree-registration races. The existing
`oplog undo --keep` live gate remains separate; this slice does not depend on claiming that it passed.

## Status update (2026-11-16)

At workspace main `137997dfae174ef2606bceb8a3466a6b1c450c15`, M1 replay, M2 restack plan, review carry-over, lint and oplog reader/writer/undo are present. This supersedes older absence and ordering statements above without removing their history. The conflict-free M6 apply/undo spike landed in [#20](https://github.com/wstein/whr-anchorage/pull/20); it is not a product command and its separate owner live acceptance remains open. Deferred conflicts and rerere retain their own spike and live gates. The read-only `ready-check` (seven conditions, port of `internal/readycheck`) has landed: `src/ready_check.rs`, README "Ready check".

Go [v0.2.1](https://github.com/wstein/sourcesteward-go/releases/tag/v0.2.1) was published with four archives and SHA256SUMS; [tap #33](https://github.com/wstein/homebrew-tap/pull/33) merged with green PR checks. Receipt trust/schema and mount [#9](https://github.com/wstein/whr-anchorage/issues/9) remain open; gh-stack [#10](https://github.com/wstein/whr-anchorage/issues/10) remains deferred.
