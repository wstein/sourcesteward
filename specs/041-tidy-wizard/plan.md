# Plan: `stw tidy` wizard (Spec 041)

Test-first throughout (a failing test before behaviour), temporary repositories only, no network, no build or release. Each work package
is for one middle-tier author and ends with its tests green and `make check` clean. Seams: a new `internal/tidywizard` (pure candidate
classification, derivation of defaults, plan-set model; no I/O except through small interfaces), `internal/workspace` (Git/jj evidence,
retirement and the ownership position rule of #174, reuse of the existing proofs), `internal/fanout` (Spec 040, width 4), the discovery
used by `internal/statuscmd`, and `internal/cli/tidy.go` (mode selection, prompts, rendering). The expert layer's code paths are not
edited except where WP-1 changes the position check; its tests and goldens stay byte-unchanged.

Dependency chain: **WP-1** (fix #174; useful alone and shippable first) -> WP-2 (derivation) and WP-3 (classification) in parallel ->
WP-4 (plan set and apply) -> WP-5 (CLI, wizard, JSON, schema) -> WP-6 (docs, acceptance). Spec 040 `fanout` is used if merged, otherwise
the existing `internal/issues` pool pattern is not copied: the WP waits for `fanout` or runs repositories sequentially behind a one-line seam.

| WP | Scope | Done when |
| --- | --- | --- |
| 1 ownership (#174), both causes | `internal/workspace`: (0) retirement reuses the Spec 011 integration proofs (ancestor, patch-id, merge-noop, no-net-change; `--integration-proof`, `--accept-whitespace-differences`; policy stored in the plan) instead of ancestry only; (a) position rule (detached creation commit, or HEAD attached or detached whose history contains it; the descendant check is new); (b) split `Verify` into the identity part and the creation-digest equality: retirement uses the identity part plus its fresh `gitSafety` admission and stores the fresh digest (re-checked at apply), workspace and handoff keep the digest equality; no new refusal code (hooks stay notes in planning mode, Spec 031; a change between plan and apply is the existing `evidence-changed`); the split applies at both `Verify` call sites and the v2 retirement path is unchanged; no new plan field (the plan already binds `Target.HeadRef`/`CommitID`); the dependent branch leg (new form, after a successful retirement, re-observed, Spec 011 protections, never for the base branch) | the real sequence of #174 including `git push -u` to a bare remote passes for Git (and jj where the layout allows); changed unrelated config and Git binary identity retire; a hook added before planning still retires (a note), one added between plan and apply refuses `evidence-changed`; refusal fixtures (dirty, unintegrated, wrong owner, moved or foreign lock, unrelated checkout) keep their codes; existing tests keep their assertions; the exceptions are named in the pull request (the `attached-current-head` validation case is removed, two fixtures use a content change where an empty commit used to stand for unintegrated work, because no-net-change now integrates it, and the online fixtures pin `--integration-proof ancestor`) |
| 2 derivation | `internal/tidywizard/derive`: workspace (reuse discovery), remote, base per repository (symbolic HEAD, jj trunk), state directories (including the `stw workspace` CLI default, T007a; shared default for `stw workspace` and `stw tidy`, repeatable `--state-dir`, `$STW_STATE_DIR`, 0700 checks, record lookup across stores, `registration-record-missing`/`-ambiguous`), owner from registration, `derived[]` with sources; ambiguity is an explicit result, never a guess | table of cases: missing HEAD, dangling HEAD, two remotes, symlinked or world-readable state dir, override flags; no value is invented |
| 2b owner scope | `internal/tidywizard/owner`: item owner derivation (task record, path/branch table, author hint, unknown), `scope`, run-mode detection (both streams TTY, `STW_AGENT`, agent-marker table, `--mine`; `--by-owner` only narrows the view) with recorded mode evidence, `authorized_scopes` inside the content-addressed plan id, the `/dev/tty` typed confirmation, `recently-active`/`no-work` | table tests per rule; editing `authorized_scopes` changes the plan id; agent-mode planning admits only own task-record items and only the human path writes and applies `authorized_scopes` |
| 3 classification | observers `workspace.ObserveWorktrees` (T011a1 Git, T011a2 jj, read-only, spec section Read-only observers) and the adapter (T011a3) feed `internal/tidywizard`: candidates of the four kinds, verdict and reason codes, owner and scope on every candidate (no stw registration gives `unregistered`), grouping by owner, protected set (cwd worktree, primary, dirty, locked), per-repository analysis from the facts the read-only observers supply, bounded; sorted deterministic output and ids | every row of the behaviour table has a fixture; no candidate of `retain` or `foreign` carries an effect (asserted); order is permutation-invariant |
| 4 plan set and apply | content-addressed plan set over the existing plan kinds plus the dependent branch leg (one worktree per retirement), store verification, stale re-check, ordered per-sub-plan apply with journals and recovery manifests, partial reporting, `--yes` rules | stale (HEAD, tip, base moved) refuses and changes nothing; a failing sub-plan leaves the others reported; the dependent branch leg runs only after a successful retirement (not atomic with it) and is skipped when the retirement fails; old `apply --plan` unchanged |
| 5 CLI, wizard, JSON | mode selection (no `PATH` and only wizard-set flags; a test enumerates every registered tidy flag), flags (`--member`, `--member-base`, `--by-owner`, no `--as`), strict line prompts (default none, 3 retries, EOF and no-TTY degrade), text rendering with `esc` and bounds, `--json` `tidy-overview`, `schemas/v2/tidy-overview.schema.json` with valid and broken fixtures, freeze digest, `UNPUBLISHED_SCHEMAS`, exit codes | US1 to US7 pass; goldens for text and JSON; stdin never read without a TTY |
| 6 docs and acceptance | `docs/tidy.md`, `docs/commands.md`, `docs/json-contracts.md`, `docs/agent-guide.md`, `docs/workspaces.md`, `docs/status.md`, Spec 037 rows 23 and 24 hint, `docs/progress.md`, `docs/README.md`, `docs/roadmap.md`, `docs/feature-status.json`; dogfood dry run on a copy of the 61-worktree situation (plan only, owner applies); independent review of the exact head | `make check`, `scripts/check-docs.py` pass; SC-001 to SC-016 evidenced in `docs/progress.md` |

**Pull requests of the classification evidence.** PR A is the spec text of the observers (docs only). PR B is T011a1, the Git observers. PR C is
T011a2 and T011a3, the jj observer and the adapter to the classifier facts. Each stacks linearly on the previous one and needs its own review.

## Test plan and mutation targets

A mutation target is a one-line change a test must kill.

| Area | Tests | Mutation targets |
| --- | --- | --- |
| Ownership position (1) | real sequence of #174 with rebase-merge (patch-id); branch tip ancestor, merge-noop, no-net-change variants; unrelated checkout (not containing the creation commit) refuses; identity changes (moved gitdir, changed lock reason, other owner, other common dir) refuse; HEAD or tip moved after plan is `stale`; branch attached in a second worktree is kept | accept any HEAD; skip the descendant check; compare against the old detached target; delete the branch without the same-plan listing; skip the lock check; skip the clean-tree check |
| Derivation (2) | each row of the derivation table; override flags win and are marked `overridden`; ambiguity is never silently resolved; state dir modes (0755, symlink, inside a repository, other owner) refuse | pick the first remote on ambiguity; fall back to `main`; accept mode 0755; follow a symlinked state dir |
| Classification (3) | one fixture per case of the behaviour table including Codex-style `/tmp/whr010-*` foreign worktrees, locked-stw vs locked-foreign, stale registration, cwd worktree, primary, bookmark in jj; a repository that errors does not hide the rest; deterministic order under shuffled repositories and goroutine timing | mark foreign as safe; treat any lock as stw's; include the cwd worktree; sort by completion time; drop the error repository silently |
| Plan set and apply (4) | store checks; stale per sub-plan; ordered application; a failed sub-plan reported, exit 1; recovery refs and journal exist before the native effect; replay of a completed plan is a verified no-op; `--yes` without id exits 2 | apply without the stale re-check; apply without authorization; delete the branch when the retirement failed; accept `--yes` without id |
| Owner scope (2b, 3, 4, 5) | default human view lists all owners grouped with owner column; JSON carries `owner`, `owner_source`, `scope`; `a` selects own only; foreign chosen by number or `o` is marked; `--mine` and `--by-owner` filter (`--by-owner` is not a mode input); agent mode plans own only; agent `apply` refuses `other`/`unknown` without `authorized_scopes`; a plan with it is applied only by the human path with the typed `/dev/tty` confirmation, and an agent-mode apply of it is refused (only the human path applies `authorized_scopes`); commit-author never makes an item own | preselect other-owner items; let agent `--save` include them; let an agent write `authorized_scopes`; derive `own` from the commit author; hide the owner column |
| Wizard (5) | strict parsing (numbers, ranges, `a`, garbage, empty = none), 3 retries then exit 1 with nothing stored, EOF, no TTY (closed and silent stdin), `a` never includes `decision` rows, text bounds, `esc` of ANSI/CR/NUL/bidi, JSON golden, truncation marker and `output-limit` | default answer apply; read stdin without a TTY; include `decision` in `a`; skip `esc` for a branch name; print the token |
| Compat (5) | every existing tidy test, golden and schema digest unchanged; mode selection table (each registered expert flag selects the expert layer, including `plan --base X` without `PATH`; mixing a positional id with `--plan` exits 2; wizard-only flags with expert flags exit 2; an unclassified new flag fails the test) | route `plan --base X` (no `PATH`) into the wizard; reuse `--repo` or `--owner` in the wizard |
| Verify split and state dirs (1, 2) | `git push -u` to a bare remote, unrelated config key, Git binary identity change each retire; hook added before planning still retires (a note), added between plan and apply refuses `evidence-changed`; creation-digest equality still enforced on workspace and handoff paths; records in a second store found; missing and ambiguous records; unsafe store skipped, exit 1 | drop the fresh admission; skip the stale check for a hook added after planning; delete the digest check of the handoff path; guess an owner for a missing record |
| Mode and confirmation (2b, 5) | pty with `STW_AGENT` or `CLAUDECODE`/Codex/agy marker is agent mode; plain pty is human; `--mine` forces agent, `--by-owner` does not; typed `/dev/tty` confirmation (confirmation word, the label or `unknown`, plus 6-character plan-id prefix) required for foreign items, a bare `y` or stdin input is refused, `--yes` not honoured; mode evidence in plan, journal and `history`; `own` for agents only via task record; `--as` does not exist | accept `y` on stdin for a foreign item; omit a marker; leave `authorized_scopes` out of the plan id; let `--yes` skip the confirmation |
| Branch leg (1, 4) | leg applied only after a successful retirement, re-observed; HEAD attached to the base branch gets no leg; changed tip OID, attached elsewhere, workspace ref, symbolic ref each refuse; failed retirement skips the leg | run the leg before the retirement; skip the `selected-base` protection; ignore the planned tip OID |
| Activity (3) | other/unknown worktree with recent HEAD/index/tracked-file mtime is `retain recently-active`; old and commitless carries `no-work`; own task-record worktrees are not subject | treat a fresh `/tmp/whr*` at the base commit as `safe` |
| Observers (3, T011a1 to T011a3) | the read-only guarantee test (before and after snapshot of repository, worktrees, refs, config and store around every fixture); every error class sets `Incomplete` and never a safe value; 61-worktree fixture within the stated budget; jj without working-copy snapshot | run plain `git status` (refreshes the index; the probe runs without the sandbox so it can fail); default a failed proof to `none`; default a missing mtime to old; write a plan record; fetch |
| Concurrency | at most 4 repositories in flight, results ordered independently of completion | unbounded fan-out |

Authors run `go test -race` once per touched package before the first push; reviewers run the affected tests and their own mutation probes.
No `stw` build for dogfooding without the owner's approval; the dogfood dry run is plan-only and applying stays with the owner.

## Risks

See the spec. WP-1 changes an ownership check and gets an independent Opus review of the exact head before it is Ready; the other WPs
follow the normal two-round cap. The wizard introduces the first interactive input of `stw`; its degradation tests are part of WP-5.

## Traceability

| Requirement | WP and tasks |
| --- | --- |
| FR-001, FR-002, FR-008, FR-009, SC-004, SC-005 | WP-5 T015, T016, T019 |
| FR-003, FR-004, FR-005, SC-002, SC-016 | WP-3 T009, T010, T011, T010a |
| FR-006, FR-006a to FR-006h, SC-010 to SC-012, SC-014 | WP-2b T006a to T006d; WP-3 T009a; WP-4 T013a; WP-5 T016 |
| FR-007, FR-010, FR-011, FR-014, FR-016, SC-006 | WP-2 T006, T008 |
| FR-012, FR-012a, FR-013, SC-013 | WP-2 T007, T008 |
| FR-015 | WP-3 T009 |
| FR-020, FR-020a, FR-021, FR-022, FR-023, FR-024, SC-003, SC-007 | WP-1 T001 to T005 |
| FR-025, SC-015 | WP-1 T004a; WP-4 T013 |
| FR-030 to FR-034 | WP-4 T012 to T014 |
| FR-040 to FR-043, SC-008 | WP-5 T017 to T019 |
| FR-050, FR-051 | WP-3 T011 |
| FR-052 | WP-3 T011a1 to T011a3 |
| SC-001, SC-009 | WP-6 T021, T022 |
