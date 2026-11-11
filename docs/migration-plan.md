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
| CLI help/version | `internal/cli/root.go:182` command registration | `src/cli.rs` clap `Command`, `--version` | Keep flags; do not copy a version subcommand just for parity. |
| Git/jj identity and ref inventory | `internal/inspection/{git,jj,inventory}.go` | `src/status.rs`, `src/repo/` (Git only) | Keep Git identity in status; jj is not supported (decision 8); defer the inventory query language. |
| Workspace repository fanout | `internal/inspection/repos*.go` | `src/repo/`, workspace selection in `src/cli.rs` | Keep shared iteration instead of another `repos` command. |
| Local status, worktrees, gitlinks | `internal/statuscmd/`, `internal/inspection/pins*.go` | `src/status.rs`, `src/repo/worktrees.rs` | Keep; expand only for demonstrated workflow needs. The status flags `--long --fetch --worktrees --icons --timeout --stats --verbose` are ported (see the `status` row of the port inventory). |
| PR status/review state | `internal/reviewstate/parse.go`, `internal/prviews/` | `src/github.rs:154`, `src/status.rs:318`; `status --review` uses it (src/status_review.rs) | Port strict parser and verifiable comment author and head binding (M3). |
| Sync and rules update | `internal/stwsync/` | `src/sync/`, `src/cli.rs` SyncArgs | Keep ff-only; no automatic stash, reset, merge, or rebase. |
| Tidy plan/apply/restore | `internal/tidywizard/` | `src/tidy.rs`, `src/tidy_apply.rs:495,908` | Keep manifest-first and fresh-state checks; do not copy wizard/authorization ceremony. |
| Tidy recovery retention/history | recovery and journals in `internal/tidywizard/` | `src/tidy_apply.rs:90,936`; JSONL stores OIDs, no retention refs | Add a backup ref per mutation (M5, slimmed 2026-11-10, see Cutover section); object IDs alone do not survive GC. |
| Isolated workspace creation/retirement | `internal/inspection/jj_workspace.go`, `internal/workspace/` | worktree inspection/removal, no creation command | jj workspaces: not supported (decision 8). Defer general workspace wizard; add owned resolve worktree lifecycle in M12. |
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
D: **4/5**; full native jj parity: **2/5** (not pursued, decision 8). Scores order experiments, not approval to implement.

| Option | Existing spike and evidence | Product recommendation | Acceptance still needed |
| --- | --- | --- | --- |
| A: conflict preview | `spikes/stw/conflicts/README.md`, `stw-rebase-lib.sh`, `test-stw-stack-preview` | Replay whole explicit stack in memory; show conflicts at every level and inherited conflict uncertainty. | Owner live demo plus gix/Git tree oracle; no refs/index/worktree changes. |
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
| M9 | PR/stack queue and debt view. Needs M2/M3/M4. | Count units not PRs; stale drafts, rewritten heads, bottom-up order, ledger parse failures, queue/nit rules against current dotagents; pagination and stale data reported. Read-only. | 4 / 4 / 3 |
| M10 | Rust push/workflow from accepted scripts. Needs M0/M4/M5, repository policy and local CLEAR evidence contract. | Existing `scripts/test-stw-push` behaviors translated after script acceptance; ff-only default branch, submodules first, no tags/force, exact-head CLEAR; partial remote effects reported and never described as undoable. Pending guard from M12 before deferred flow enabled. | 4 / 3 / 5 |
| M11 | Owner-invoked land stack. Needs M2/M4/M5/M6/M10 and explicit design approval. | Separate PR/rebase policy from local ff policy; exact reviewed heads and checks, no pending refs, stale remote refusal, fake-forge failure fixtures; owner sandbox live demo before any real publication. Native PR stack shape (child base = previous head branch): retarget/rebase sequence with exact-head review and checks re-evaluated. Required checks come from `rules/branches` (read access); unreadable classic branch protection is a reason to refuse. Go v0.2.1 land fixtures are a shared corpus. | 3 / 5 / 5 |
| M12 | Deferred conflicts D and owned resolve worktrees. Needs M1/M2/M5/M6/M7/M0 and accepted D spike. | Real conflicted branches stay at old tips; pending metadata/ref transaction; parent-first resolve; abort preserves pending; stale CAS; binary/delete conflicts; undo; push/land/ready refuse pending state. | 4 / 5 / 5 |
| M13 | Demand-driven follow-ups: watch/issues, verify landed, metrics, repo profiles, Dayboard integration, release tooling, merge-noop proof. | One owner-selected slice at a time (script/live acceptance only for behavior without a Go counterpart); define scope, contract, and new ratings before implementation. No blanket parity completion criterion. | 2 / 5 / 4 |

Recommended order: **M0 → M3 → M4** for workflow reads; in parallel as a separate workstream,
**accepted A → M1 → M2 → M5 (slim) → M6** for local stacks. M8 is independent after M3.
M7/M12 can follow local restack; M10/M11 follow proven recovery and publication policy.
M7 does not require publication; M6 does not push. M12 publication guards must land before pending state can
be created in a product used with push/ready commands. These are dependency tracks, not permission to start agents.
M0 starts directly in `projects/sourcesteward` without a spike (owner decision: infrastructure, the Git boundary is
already recorded in Chartroom). Scripts-first stays for new behavior without a Go counterpart; ports of Go behavior
(M3, M4, M8, M9 and the single-branch part of M1/M2) go directly to product.

## Decisions, tradeoffs, and open evidence

1. **Recorded decision: Git boundary.** Read/merge through gix; installed Git only for push, ssh/local fetch,
   range-diff, and conflict resolution. HTTPS fetch stays gix. Mark each such output step `via: git`.
   Update `tests/it/no_binaries.rs` to permit only the runner module rather than removing the architecture check.
   Currently gix itself can spawn ssh/git-upload-pack for non-HTTPS fetch (`tests/it/remote_kinds.rs`); M0 must close
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
   Superseded 2026-11-10 by the slimmed M5 (Cutover section): backup ref before each mutation, no crash journal.
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
Rust today: `src/cli.rs` has `status`, `tidy plan|apply|restore`, `sync` and the hidden `dev patch-id`; everything else
is **absent** as a command. Test counts are `grep -c '^func Test'` per Go package, not runs.

| Go command | Rust today | M | Proposal | Test source in Go |
| --- | --- | --- | --- | --- |
| `status` | present. Flags `--long`, `--fetch`, `--worktrees`, `--icons`, `--timeout`, `--stats`, `--verbose`, `--review`: **port (done)**, deviations below | M4 | **port**: core command. Deviation: Go refuses jj-owned paths (`status-unsupported-backend`); Rust has no jj detection (decision 8). Deviation: Go exits 1 when a remote read failed (`statuscmd/run.go` `Failure`); Rust prints a notice and exits 0, exit 1 only for a repository that cannot be read locally. Deviation: Rust text has the blocks `Counts`, `Needs action (N)`, `Repositories (N)`, `Notices (N)` and JSON `counts`/`totals` (additive, schema 2); open issues are read for the counts, one page of 100 (README "Status text"). Flags (README "Status flags"), deviation by deviation: `--long` has no compact counterpart (the Rust default text already prints the detail lines) and explains each reason from the facts instead of printing Go's `run:`/`hint:` commands; Go's priority levels, links and sources release drafts/tags/CI do not exist in Rust. `--icons` covers the two repository markers only (no priority glyphs); the terminal default applies to every run, Go keeps ascii in its local layout unless the flag is given. `--fetch` uses the fetch of `sync` (runner for ssh/local paths, gix for https, `--no-prune`, no tags, no submodules), writes FETCH_HEAD, refuses a fetch refspec outside `refs/remotes/origin/` (addition), does not fetch the agent-rules checkout, passes `--no-prune-tags` (Go does not, so `pruneTags` makes Go's fetch create tags), works with `--offline`; the parallel bound is 16 as in Go (shared helper, also `sync`/`tidy`). Correction candidate (not changed, `sync` keeps Go behavior): `sync --apply` without a prune flag has the same `pruneTags` gap. `--timeout` cuts the GitHub reads (pulls, issues) only, request cap 30 s (Go 15 s), notices per repository, exit 0. `--stats`/`--verbose`: sources pulls/issues/other (the comment reads of `--review` count as `pulls`, as in Go), `cached 304s` always 0, no `viewer` line. `--worktrees`: stale registrations are listed (`missing`, `prunable`) but not counted in `Worktrees` (Go counts them in `linked`), no `N linked worktrees (...)` summary line or per-state counts, sorted by path, JSON `repos[].worktree_list` instead of Go's `worktrees.list`. Was `status --worktrees` covered before? No (resolved, was UNKNOWN): only the number in `Worktrees` and, in `tidy plan`, the worktree of a kept branch and the stale registrations; the list with dirty/locked/prunable/missing is new. `--review` (port, done; README "Review state in `stw status --review`"): one comment read per open pull request, M3 state for the exact head, at most 200 pull requests per run and 16 reads at a time, a failed or cut read is `?` plus a notice, exit 0. Deviations from Go, one by one: (1) the M3 states (`no-review`, `unverified-author`, `unknown`) instead of `none`, `clear-stale`, `unparsed`; (2) the M3 grammar (D1 to D8), a line for another pull request is the finding `wrong-pr`; (3) no commit-date read of the head (Go ignores comments older than the head commit; Rust binds the verdict to the head id, but an old comment edited later counts at its `created_at`, flagged `edited`); (4) carry-over chains of M3 (16 links, declared patch-ids, `FINDINGS` of a chain head ends it) instead of 8 links; (5) trust per login derived from OWNER, MEMBER or COLLABORATOR associations, no `ignored` count or `foreign-review-ignored` hint; (6) a long thread is read up to 500 comments and then `unknown` (Go: first page plus last five pages, `truncated`); (7) hints: `clear-not-ready` and `base-not-default` are ported (data of the pull request list, no extra read; default branch from `base.repo.default_branch`, no `base-not-default` without a known base and default branch); `no-final-review`, `truncated-review-unknown`, `foreign-review-ignored` (covered by the states) and `ready-mergeable` (needs readycheck groups) are deliberately not ported, `parent-merged-retarget` (extra read of closed pull requests per repository with stacks) is a follow-up candidate; no per-group counts; (8) the state of every pull request in text and JSON (`review`, `review_state`, `pull_requests.review`, `options.review`), not only counts; (9) limit 200 per run in repository order, no `--limit`, 16 parallel reads instead of 4 repositories, stop after a rate limit (429, or 403 with exhausted limit, `Retry-After` or "rate limit"/"abuse detection" in the first 4096 bytes of the body, as Go) for the comment reads of the whole run: not started reads are `?` plus one notice `rate limit reached, N reads skipped`, exit 0; Go stops all sources of the run, Rust only the comment reads; (10) exit 0 for a failed review read (Go: 1). JSON additive in schema 2: `options`, `stats`, `repos[].fetch`, `fetch_error`, `worktree_list`, `options.review`, `pull_requests.review` (with `hints{clear_not_ready,base_not_default}`), `open[].review_state`, `open[].hints`, `open[].base`, `open[].default_branch`. | `internal/statuscmd` (40), `statusremote` (109), `cli/status_*_test.go`; goldens pin Go text and schema v1, reuse inputs not goldens |
| `sync` | present: acts by default like Go (owner decision 2026-11-11, "standard wie Go: ausführen"); `--plan`/`--dry-run` preview | - | **port** (done): fast-forward of every repository. Deviation: `--apply` is a hidden, ignored alias (no Go flag), a usage error together with `--plan`/`--dry-run`; the Rust plan never fetches (Go's `--plan` fetches the remote-tracking refs). Deviation: Go refuses jj-owned paths (`sync-unsupported-backend`); Rust has no jj detection (decision 8). | `internal/stwsync` (162), `cli/sync_test.go` |
| `sync --plan`, `--dry-run` (flag) | present (both, the same flag; JSON `mode` `dry-run`, `options.dry_run`) | - | **port** (done, 2026-11-11): the preview. Deviation: Rust fetches nothing and writes nothing, Go's `--plan` fetches the remote-tracking refs. README "Deviations from Go in these flags", 1. | same |
| `sync --ff-only` (flag) | the Rust behavior | - | **port** (done). | same |
| `sync --rebase`, `--merge`, `--autostash`, `--no-autostash` (flag) | absent; Rust never merges, rebases or stashes | - | **open package** (earlier decision: drop, matrix: no automatic stash, reset, merge, rebase; since the default change of 2026-11-11 a Rust `sync` acts like Go, so these are the remaining gaps): not ported now. Deviation: Go follows the Git configuration (`pull.rebase`, `branch.<name>.rebase`, `pull.ff`, `rebase.autoStash`, `merge.autoStash`, `fetch.prune`; `sync.go:33-35`); Rust ignores all of them except `fetch.prune` (which applies to a fetch by the installed git, not to gix/https). A diverged branch with `pull.rebase` or `branch.<name>.rebase` set is skipped and the reason says that stw never rebases. | same |
| `sync --prune`, `--no-prune` (flag) | present (`src/sync/fetch.rs`, `src/git_runner.rs` `Op::FetchOriginPrune`) | - | **port** (done): `fetch --prune --no-prune-tags` / `--no-prune --no-prune-tags`, mutually exclusive, in a run only (not with `--plan`); the plan shows the scope. Deviations: always the installed git, also for `https://` (no GitHub token for it); refuses, in a run, a `remote.origin.fetch` that writes outside `refs/remotes/origin/`; never prunes tags; reports the deleted refs as a list (Go: a number), also after a failed fetch; only `origin` (Go: the branch's remote); without a flag gix never prunes https. README "sync --prune". | `internal/stwsync/pull_test.go`, `policy_test.go`, `cli/sync_test.go` |
| `sync --submodules` (flag) | present (`src/sync/submodules.rs`; `Op::SubmoduleSync`, `Op::SubmoduleUpdate`) | - | **port** (done): moves detached pinned submodules to the recorded commit when that is a fast-forward; in a run only, `--plan` shows `would-move`. Deviations: top-level submodules only (no nested walk); stricter path charset, applied to every command (README "Submodule paths of `.gitmodules`"; Go lists index gitlinks and checks charset/symlinks only for a `sync --submodules` run, `sync.go:166`, `262-276`); `unreachable` is `error` (`--plan`: `skip`); a run fetches the submodule's `origin` before the check, and a failed fetch means no move (Go moves anyway); the dry run predicts from the index gitlink before the superproject's fast-forward; filter drivers in the submodule config are refused. README "sync --submodules". | `internal/stwsync/submodules_test.go` |
| `sync --rules` (flag) | present with `--rules-path` | - | **port** (done); **drop** `--owner-identity`, `--allowed-committer`: Go refuses on foreign identities, Rust prints an info line (README: no owner or author check). `--owner-identity` and `--allowed-committer` stay an open package (not ported now). | same |
| `sync restore` (mode) | absent | M5 | **owner decides** (open; not ported now, an open package since 2026-11-11, and with it `sync` has no undo while it acts by default): Go journals each change with recovery refs `refs/stw/recovery/sync/RUN_ID/N` and `restore` resets branches back. Tied to the slimmed M5 (backup ref) as `tidy history` is. | `internal/stwsync/restore_test.go`, `cli/sync_test.go` |
| `tidy plan` | present, Git only | M5 | **port** (done); jj bookmarks and owned-workspace retirement **drop** (owner confirmed 2026-11-11: jj is not supported). | `internal/tidywizard` (97), `cli/tidy_*_test.go` |
| `tidy apply` | present, Git only | M5 | **port** (done); backup ref is M5 (slim). | same as `tidy plan` |
| `tidy restore` | present; no journal listing | M5 | **port** (done). | `cli/tidy_v3_restore_test.go`, `tidy_prune_v4_restore_test.go` |
| `tidy history` | absent | M5 | **owner decides** (open): lists stored journals; Rust `restore` takes `--run` or the newest. Tied to the slimmed M5 (decision 2026-11-10). | `cli/tidy_prune_v4_history_test.go` |
| `review carry-over` | absent | M4 | **port**: reuses a CLEAR after a rebase; proof is equal patch-id, no merge commits, range-diff all `=`. Needs `range-diff` in the runner. | `internal/carryover` (46; repositories are built in `fixture_test.go`, no data files), `cli/review_test.go` |
| `ready-check` | absent | M4 | **port**: pure `Evaluate` over seven conditions (open, review, checks, base, blocker, conflict, rebaseable). Review condition comes from the Rust M3 state. | `internal/readycheck` (25), `cli/readycheck_test.go` |
| `lint pr` | present (`src/lint_pr.rs`, `src/lint_pr_cmd.rs`) | M8 | **port** (done): PR text rules; deviations in "Slice 3". | `internal/lintpr` (15), `lintprcmd` (17), `cli/lint_test.go` |
| `pulls` | absent | M9 | **port** as read-only queue after M2/M3/M4; Rust has open-PR reads only. | `internal/pulls` (85), `pullscmd` (65), `prviews` (17), `cli/pulls_test.go` |
| `restack plan` (h) | absent | M1/M2 | **port** the single-branch prediction (`--branch --old-parent --new-parent`); whole-stack preview is new behavior (spike A). | `internal/restack` (54), `conflicts` (24), `cli/testdata/restack-plan-*.json` |
| `land` | absent | M11 | **defer**: `internal/land/shapes.json` lists no measured shape, so the Go apply path cannot run today (`cli/land.go` help). | `internal/land` (42), `landcmd` (25); v0.2.1 fixtures as in M11 |
| `watch` | absent | M13 | **defer**: helpers wait with background loops meanwhile. | `internal/watch` (89), `watchcmd` (50) |
| `issues` | absent | M13 | **defer**. | `internal/issues` (39), `issuescmd` (28) |
| `workspace plan` | absent | M12 | **defer**: general workspace wizard deferred (matrix); owned resolve worktrees come with M12. | `internal/workspace` (965, `testdata/`), `internal/acceptance` |
| `workspace apply` | absent | M12 | **defer**, as `workspace plan`. | same |
| `workspace handoff` | absent | M12 | **defer**, as `workspace plan`. | same |
| `workspace retire-plan` | absent | M12 | **defer**, as `workspace plan`. | same |
| `release check` (h) | absent | M13 | **defer**: hidden in Go until its docs item (`cli/release.go:29`); publication stays with the owner. | `internal/release` (356), `cli/release_*_test.go`, `releaseworkflow` |
| `release plan` (h) | absent | M13 | **defer**, as `release check`. | same |
| `release verify` (h) | absent | M13 | **defer**, as `release check`. | same |
| `release record` (h) | absent | M13 | **defer**, as `release check`. | same |
| `release build` (h) | absent | M13 | **defer**, as `release check`. | same |
| `release dispatch` (h) | absent | M13 | **defer**, as `release check`. | same |
| `hosting repo` | no command; reads in `src/github.rs`, `src/github/reads.rs` | M4/M9 | **drop** as command (matrix: no hosting CLI wrappers); the reads stay internal. | `cli/testdata/hosting-repo.json`, `internal/hosting` (327) |
| `hosting pr` | as above | M4/M9 | **drop** as command. | `cli/testdata/hosting-pr.json` |
| `hosting prs` | as above | M4/M9 | **drop** as command. | `internal/hosting/testdata` |
| `hosting ci` | as above; `check_runs`, `commit_statuses` exist | M4 | **drop** as command; ready-check needs the reads. | `cli/testdata/hosting-ci.json` |
| `inspect` | absent | - | **drop**: Git identity is in `status` (matrix). | `internal/inspection` (529) |
| `inventory` | absent | - | **drop**: worktree facts are in `src/repo/worktrees.rs` (matrix: no inventory query language). | `internal/inspection` |
| `repos` | absent | - | **drop**: shared workspace iteration instead (matrix). | `internal/fanout`, `cli/repos_test.go` |
| `pins` (h) | no command; `gitlink_state` in `src/status.rs:176` | - | **drop**: hidden in Go until its spec item (`cli/pins.go:39`), gitlinks are in `status`. | `cli/pins_test.go`, `cli/testdata/pins-*.json` |
| `version` | `--version` | - | **drop** (matrix: no version subcommand). | `cmd/stw/main_test.go` |

Also in the Go repository: `cmd/gh-jj` (gh extension wrapper) and `cmd/stw-docs`: **drop** (matrix: no brand/site/gh-jj
wrappers). Owner confirmed 2026-11-11 for `cmd/gh-jj` only (jj is not supported); `cmd/stw-docs` stays a proposal.
Tally: 34 items = the 33 commands plus the mode `sync restore`. Rows marked (flag) are proposals inside their command and
are not counted. Port 10 (5 of them present in Rust: status, sync, tidy plan, apply, restore), drop 9, defer 13, owner
decides 2 (`tidy history`, `sync restore`).

Test corpus: the Go fixtures are mostly Go code (repository builders such as `internal/carryover/fixture_test.go`), few
data files (`cli/testdata`, `internal/hosting/testdata`). "Shared corpus" therefore means: per Go test, one named Rust
case with the same scenario, or a named deviation. Where the Rust rules are stricter than Go the Rust rule wins:
- Review lines (M3, M4): Go accepts a 64-hex patch-id, drops a trailing CR and accepts PR numbers of at most 9 digits
  and no `pr=none` (`internal/reviewstate/parse.go:36-54`). Rust: exactly 40 hex, no CR, `pr=none` for local reviews.
  The Rust corpus today comes from dotagents (`tests/review_corpus/`), not from the Go tests.
- PR lint and queue rules (M8, M9): not compared with the current dotagents rules; UNKNOWN until each rule is checked.
- Golden outputs of `status` pin Go text and schema v1; Rust has schema 2.

### Cutover criteria

A Go command counts as **replaced** when (a) its Rust equivalent passes the test cases derived from Go (deviations
named) and an independent review of the exact head is CLEAR, or (b) the owner decided `drop` for it explicitly.
`defer` is not an end state: before archiving, the owner turns each deferred command into a port (a) or a `drop` (b).
Archive `legacy/sourcesteward` only when all 34 items, every flag row and the two extra binaries are replaced. Then:
1. Owner: archive the repository `wstein/sourcesteward` (GitHub setting).
2. Owner: remove the gitlink `legacy/sourcesteward` or freeze it at the last Go commit.
3. Agent: update the references in workspace, AGENTS, Chartroom and skills (`grep -rn "legacy/sourcesteward"`); changes to
   skills go by pull request in `wstein/dotagents`, on owner request.
4. Owner: last Go version. v0.2.1 is not published (`dayboard/BACKLOG.md`: owner publishes, then the gitlink is raised);
   decide whether to publish it as the last release or archive at v0.2.0. Installed binary stays v0.2.0 until M3+M4
   (decision 4 above).

### Order of the ports

Owner decisions of 2026-11-10 (chosen by selection):
- **Two port lines in parallel.** Line A: M3 -> M4 -> M8 -> M9 -> M10 (M10 needs the slim M5). Line B: M1 -> M2 -> M6
  (restack, stacks; M6 needs the slim M5).
  Reason: restack makes stacking cheap, and M3/M4 replace the review gates that keep Go v0.2.0 installed.
- **M5 is slimmed.** Instead of the full oplog: a backup ref `refs/stw/backup/<n>` before every mutation, evidence by
  `range-diff`, undo = set the ref back. No journal, no retention logic, no general undo. M6 may start without spike B.
  `tidy history` stays tied to this question; port or drop is still open (owner decides).
- **Ports without spike** apply as soon as AGENTS.md with that rule is on `main` (commit `docs(agents)` of this branch).
- **Go pages job:** the job `pages` on `main` of `wstein/sourcesteward` is red (Pages not set up). Decided: do nothing,
  the legacy repository is archived. Noise until the archive.
- **The 13 `defer` commands stay open** (the two flag rows `--prune`/`--no-prune` and `--submodules` are ported, owner go 2026-11-11). Before the archive the owner turns each into a port or a `drop`; agents do not
  decide this.
- M3 is in the product as a library (`src/review.rs`); M4 wires it into commands (`status --review`, `review carry-over`).
- Still deferred: M7, M11, M12, M13.
- **Open: the `drop` proposals.** Criterion (b) needs an explicit owner decision per `drop`: 9 command rows, the (flag) rows
  marked drop and the two extra binaries. Until then they are proposals. Confirmed by the owner (2026-11-11, jj is not
  supported): `tidy` with jj (bookmarks, owned-workspace/task retirement), `cmd/gh-jj`. Deviation, not a drop: Go `status`,
  `sync` (and `sync restore`, watch, tidy via the same check) refuse jj-owned paths; Rust has no jj detection
  (decision 8). All other `drop` rows (hosting, inspect, inventory, repos, pins, version, `cmd/stw-docs`, the sync flags,
  site/docs) are not yet confirmed individually.

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
Then M9 (`pulls`).
