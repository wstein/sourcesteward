# Tasks: `stw tidy` wizard (Spec 041)

**Input**: [specification](spec.md), [plan](plan.md). Tests are mandatory: write the failing test, watch it fail for the right reason,
then implement. A box is ticked only with the evidence in `docs/progress.md`. No task builds, releases, tags, applies a plan to a real
repository or pushes the default branch. Tests use temporary repositories only.

## WP-1 Ownership position, issue #174

- [ ] T001 Write the failing acceptance test in `internal/workspace`: create a workspace with `workspace apply` (detached), `git switch -c b`, commit, `git push -u` to a bare remote (adds `branch.b.remote/merge` to the common config), integrate into the base by rebase-merge (different SHAs), expect `tidy plan` retirement to be `ok` (today: `changed-ownership`). Red: the current error.
- [ ] T001a Write failing tests for the Verify split (cause 2): unrelated config key changed, Git binary identity changed (retire ok); hook added before planning (a note, the worktree still retires) and added between plan and apply (refuses with the existing `evidence-changed`), unsafe layout (existing code); the split applies at both `Verify` call sites, the v2 retirement path unchanged; no new refusal code; the creation-digest equality still holds on the workspace and handoff paths (unchanged tests).
- [ ] T002 Write failing refusal tests: dirty tree, unintegrated branch, wrong owner, moved lock, foreign lock, changed gitdir pointer, unrelated checkout that lacks the creation commit; each with its own code.
- [ ] T003 Implement the position rule and the Verify split (identity part plus fresh admission and stored fresh digest for retirement); no new plan field; T001, T001a and T002 green.
- [ ] T004 Stale tests: move HEAD, branch tip, base and the fresh safety digest between plan and apply, each refuses with `stale` and changes nothing; old stored plans still apply.
- [ ] T004a Failing tests and implementation of the dependent branch leg: applied only after a successful retirement and re-observed; no leg when HEAD is attached to the base branch; changed tip OID, attached elsewhere, workspace ref and symbolic ref refuse; failed retirement skips the leg.
- [ ] T005 Mutation probes of the plan table, recorded; `go test -race ./internal/workspace` once; independent review of the exact head.

## WP-2 Derivation

- [ ] T006 Failing table tests for workspace, remote, base (symbolic HEAD present, missing, dangling; two remotes; jj trunk), owner, protected set.
- [ ] T007 Failing tests for state directories: shared default `stw/workspace` used by `stw workspace` and `stw tidy` (explicit `--state-dir` unchanged), creation 0700, refuse 0755, symlink, inside a repository, other owner; `$STW_STATE_DIR`; repeatable `--state-dir`; records found in a second store; `registration-record-missing` with hint; `registration-record-ambiguous`; an unsafe store skipped, exit 1.
- [ ] T007a Failing test and implementation in the CLI of `stw workspace`: without `--state-dir` it uses the shared default `${XDG_STATE_HOME:-~/.local/state}/stw/workspace` (or `$STW_STATE_DIR`), created 0700 with the same checks; an explicit `--state-dir` behaves exactly as today; existing workspace tests unchanged.
- [ ] T008 Implement `internal/tidywizard/derive`; `derived[]` with sources and `overridden`; ambiguity is a typed result.

## WP-2b Owner scope

- [ ] T006a Failing table tests for item-owner derivation (task record, path/branch table incl. `/tmp/whr*`, `codex/`, `claude/`, author hint never `own`, unknown) and `scope`; run-mode detection (TTY, `STW_AGENT`, `--mine`, `--by-owner`).
- [ ] T006b Failing tests: `--save` in agent mode excludes `other` and `unknown` items; `own` in agent mode only via `task-record`; `--as` does not exist; `authorized_scopes` is part of the plan id (editing it changes the id).
- [ ] T006c Failing tests for mode detection: pty plus `STW_AGENT`, plus each marker of the table (`CLAUDECODE`, `CLAUDE_CODE_*`, `CODEX_*`, `AGY_*`), plain pty (human), non-tty, `--mine`; mode evidence recorded in plan and journal and shown by `history`.
- [ ] T006d Failing tests for the typed confirmation read from `/dev/tty` (label plus 6-character plan-id prefix): bare `y`, stdin input and `--yes` refused for foreign items; no controlling terminal fails closed. Implement `internal/tidywizard/owner` and the mode and confirmation code.

## WP-3 Classification

- [ ] T009 Failing fixtures for each case of the behaviour table: safe worktree, unmerged, dirty, ignored extras, locked-stw, locked-foreign, other owner, `/tmp/whr010-*` style worktree (visible to a human, owner `codex` by path convention, never preselected), stale registration, integrated branch, branch attached elsewhere, cwd worktree, primary checkout.
- [ ] T009a Failing tests: default human view lists all owners grouped with the owner column; no candidate of scope other or unknown is preselected; `--mine` and `--by-owner` filter; JSON carries `owner`, `owner_source`, `scope`.
- [ ] T010a Failing tests: other/unknown worktree with recent mtime is `retain recently-active`; old and commitless carries `no-work`; a fresh `/tmp/whr*` at the base commit is never `safe`.
- [ ] T010 Failing tests: no `retain` or `foreign` candidate carries an effect; ids stable; order permutation-invariant; an erroring repository is reported and the rest survives; bounds (4096 candidates, evidence 512 bytes).
- [ ] T011 Implement the classifier over the existing read-only evidence, repositories through `internal/fanout` width 4 (or sequential behind a seam if Spec 040 is not merged); jj bookmarks as in Spec 011.

## WP-4 Plan set and apply

- [ ] T012 Failing tests for the plan set: content-addressed id over sorted sub-plans, derived values and selection; one worktree per retirement sub-plan; store mode checks.
- [ ] T013 Failing tests for apply: stale per sub-plan, fixed order, journal and recovery ref before the native effect, partial failure reporting with exit 1, completed replay is a verified no-op, the dependent branch leg runs after, and only after, a successful retirement (not atomic with it; skipped when the retirement fails), `--yes` without id exits 2, no authorization no effect.
- [ ] T013a Failing tests: agent-mode `apply` of a plan with an other or unknown item and no `authorized_scopes` exits 1 `scope-not-authorized`, nothing changed; a plan with human-written `authorized_scopes` applies only on the human path with the typed `/dev/tty` confirmation, and an agent-mode apply of it fails closed without a terminal; the marking is in the stored plan.
- [ ] T014 Implement plan-set store and `apply <plan-id>` on top of the existing sub-plan apply; `apply --plan` unchanged.

## WP-5 CLI, wizard, JSON, schema

- [ ] T015 Failing mode-selection tests: a table over every flag registered by the tidy commands (wizard, expert or shared; an unclassified flag fails), rows for `plan --base X` without `PATH` (expert, byte-identical), `plan PATH`, bare `stw tidy`, `stw tidy plan`, `stw tidy plan --state-dir S`, positional id, mixing with `--plan` (exit 2), wizard-only flags with expert flags (exit 2), `--member`/`--by-owner` not colliding with `--repo`/`--owner`; all existing tidy tests unchanged.
- [ ] T016 Failing prompt tests: strict parsing, default none, 3 retries then exit 1 storing nothing, EOF, closed and silent stdin without a TTY (never read, never blocks, exit 0, nothing stored), `a` excludes `decision` rows and other/unknown-owner rows, `o` asks per owner, `decision` asks a second confirmation.
- [ ] T017 Failing golden tests: text overview (10 rows per repository, `and N more`), `esc` (ANSI, CR, NUL, bidi, 10 kB names), 120-character bound; JSON `tidy-overview` golden; truncation and `output-limit`.
- [ ] T018 Failing schema tests: `schemas/v2/tidy-overview.schema.json` valid and broken fixtures, `additionalProperties` false, freeze digest, `UNPUBLISHED_SCHEMAS`, check `package-pages.py` for the path.
- [ ] T019 Implement the command layer, flags, prompts and rendering; exit codes 0/1/2; wizard applies only after a typed `y` for the plan id through the same apply code.

## WP-6 Docs and acceptance

- [ ] T020 Update `docs/tidy.md` (wizard first, lifecycle workspace -> work -> retirement, position rule, reason codes, derived defaults, plan set), `docs/commands.md`, `docs/json-contracts.md`, `docs/agent-guide.md` (agents plan and `--save`, never apply foreign items, no pty wrappers or `send-keys`), `docs/workspaces.md` (shared default state directory), `docs/status.md` and Spec 037 rows 23 and 24 (hint `stw tidy`, if OQ11 is accepted), `docs/progress.md`, `docs/README.md`, `docs/roadmap.md`, `docs/feature-status.json`; pointers from Specs 011 and 029.
- [ ] T021 Plan-only dogfood dry run against a copy of the situation of 2026-10-23 (61 worktrees); record counts per verdict; the owner applies.
- [ ] T022 Run `make check` and `scripts/check-docs.py`; record SC-001 to SC-016 evidence; independent review of the exact head; set the feature status with evidence only.

FR and SC traceability for these tasks is the table in [plan.md](plan.md#traceability).
