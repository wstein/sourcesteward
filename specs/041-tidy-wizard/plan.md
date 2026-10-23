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
| 1 ownership position (#174) | `internal/workspace`: split the retirement binding into identity (exact) and position (detached creation commit, or HEAD attached or detached whose history contains it); integration proof applied to the real HEAD; branch deletion only if proven, listed and attached nowhere else; stored plan binds HEAD ref and OID; additive plan field; old plans still apply when their position is unchanged | the real sequence of #174 passes for Git (and jj where the layout allows); refusal fixtures (dirty, unintegrated, wrong owner, moved or foreign lock, unrelated checkout) keep their codes; existing tests untouched |
| 2 derivation | `internal/tidywizard/derive`: workspace (reuse discovery), remote, base per repository (symbolic HEAD, jj trunk), state directory (XDG default, 0700 checks), owner from registration, `derived[]` with sources; ambiguity is an explicit result, never a guess | table of cases: missing HEAD, dangling HEAD, two remotes, symlinked or world-readable state dir, override flags; no value is invented |
| 2b owner scope | `internal/tidywizard/owner`: item owner derivation (task record, path/branch table, author hint, unknown), `scope`, run-mode detection (TTY, `STW_AGENT`, `--mine`, `--owner`), `authorized_scopes` written only by the interactive path | table tests per rule; an agent-mode call cannot produce `authorized_scopes` |
| 3 classification | `internal/tidywizard`: candidates of the four kinds, verdict and reason codes, owner and scope on every candidate (no stw registration gives `unregistered`), grouping by owner, protected set (cwd worktree, primary, dirty, locked), per-repository analysis through the existing read-only evidence, bounded; sorted deterministic output and ids | every row of the behaviour table has a fixture; no candidate of `retain` or `foreign` carries an effect (asserted); order is permutation-invariant |
| 4 plan set and apply | content-addressed plan set over existing sub-plans (one worktree per retirement), store verification, stale re-check, ordered per-sub-plan apply with journals and recovery manifests, partial reporting, `--yes` rules | stale (HEAD, tip, base moved) refuses and changes nothing; a failing sub-plan leaves the others reported; a retirement and its branch deletion are atomic as one sub-plan; old `apply --plan` unchanged |
| 5 CLI, wizard, JSON | mode selection, flags, strict line prompts (default none, 3 retries, EOF and no-TTY degrade), text rendering with `esc` and bounds, `--json` `tidy-overview`, `schemas/v2/tidy-overview.schema.json` with valid and broken fixtures, freeze digest, `UNPUBLISHED_SCHEMAS`, exit codes | US1 to US7 pass; goldens for text and JSON; stdin never read without a TTY |
| 6 docs and acceptance | `docs/tidy.md`, `docs/commands.md`, `docs/json-contracts.md`, `docs/agent-guide.md`, `docs/workspaces.md`, `docs/status.md`, Spec 037 rows 23 and 24 hint, `docs/progress.md`, `docs/README.md`, `docs/roadmap.md`, `docs/feature-status.json`; dogfood dry run on a copy of the 61-worktree situation (plan only, owner applies); independent review of the exact head | `make check`, `scripts/check-docs.py` pass; SC-001 to SC-009 evidenced in `docs/progress.md` |

## Test plan and mutation targets

A mutation target is a one-line change a test must kill.

| Area | Tests | Mutation targets |
| --- | --- | --- |
| Ownership position (1) | real sequence of #174 with rebase-merge (patch-id); branch tip ancestor, merge-noop, no-net-change variants; unrelated checkout (not containing the creation commit) refuses; identity changes (moved gitdir, changed lock reason, other owner, other common dir) refuse; HEAD or tip moved after plan is `stale`; branch attached in a second worktree is kept | accept any HEAD; skip the descendant check; compare against the old detached target; delete the branch without the same-plan listing; skip the lock check; skip the clean-tree check |
| Derivation (2) | each row of the derivation table; override flags win and are marked `overridden`; ambiguity is never silently resolved; state dir modes (0755, symlink, inside a repository, other owner) refuse | pick the first remote on ambiguity; fall back to `main`; accept mode 0755; follow a symlinked state dir |
| Classification (3) | one fixture per case of the behaviour table including Codex-style `/tmp/whr010-*` foreign worktrees, locked-stw vs locked-foreign, stale registration, cwd worktree, primary, bookmark in jj; a repository that errors does not hide the rest; deterministic order under shuffled repositories and goroutine timing | mark foreign as safe; treat any lock as stw's; include the cwd worktree; sort by completion time; drop the error repository silently |
| Plan set and apply (4) | store checks; stale per sub-plan; ordered application; a failed sub-plan reported, exit 1; recovery refs and journal exist before the native effect; replay of a completed plan is a verified no-op; `--yes` without id exits 2 | apply without the stale re-check; apply without authorization; delete the branch when the retirement failed; accept `--yes` without id |
| Owner scope (2b, 3, 4, 5) | default human view lists all owners grouped with owner column; JSON carries `owner`, `owner_source`, `scope`; `a` selects own only; foreign chosen by number or `o` is marked; `--mine` and `--owner` filter; agent mode plans own only; agent `apply` refuses `other`/`unknown` without `authorized_scopes`, applies with it; commit-author never makes an item own | preselect other-owner items; let agent `--save` include them; let an agent write `authorized_scopes`; derive `own` from the commit author; hide the owner column |
| Wizard (5) | strict parsing (numbers, ranges, `a`, garbage, empty = none), 3 retries then exit 1 with nothing stored, EOF, no TTY (closed and silent stdin), `a` never includes `decision` rows, text bounds, `esc` of ANSI/CR/NUL/bidi, JSON golden, truncation marker and `output-limit` | default answer apply; read stdin without a TTY; include `decision` in `a`; skip `esc` for a branch name; print the token |
| Compat (5) | every existing tidy test, golden and schema digest unchanged; mode selection table (each expert flag selects the expert layer; mixing a positional id with `--plan` exits 2) | route a bare `stw tidy plan PATH --base X` into the wizard |
| Concurrency | at most 4 repositories in flight, results ordered independently of completion | unbounded fan-out |

Authors run `go test -race` once per touched package before the first push; reviewers run the affected tests and their own mutation probes.
No `stw` build for dogfooding without the owner's approval; the dogfood dry run is plan-only and applying stays with the owner.

## Risks

See the spec. WP-1 changes an ownership check and gets an independent Opus review of the exact head before it is Ready; the other WPs
follow the normal two-round cap. The wizard introduces the first interactive input of `stw`; its degradation tests are part of WP-5.
