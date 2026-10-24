# Specification: `stw tidy`, a workspace-wide cleanup wizard

**Created**: 2026-10-23 | **Status**: proposed (spec-light: this page, [plan.md](plan.md) and [tasks.md](tasks.md) are the whole
design; documentation only, no Go code, script or schema is changed by this specification; owner request of 2026-10-23). It builds
on [Spec 011](../011-workspace-reference-tidy/spec.md) (evidence-based plans and apply), [Spec 029](../029-tidy-dogfood-safety/spec.md)
(owned retirement, journals, recovery), [Spec 037](../037-stw-status-overview/spec.md) (workspace discovery, action vocabulary),
[Spec 040](../040-concurrency-discipline/spec.md) (fan-out through `internal/fanout`) and it resolves
[issue #174](https://github.com/wstein/sourcesteward/issues/174) (`changed-ownership`). Open questions carry a recommended default; the
foreman decides them ([Open questions](#open-questions)).

## Problem

Today the owner cannot operate `stw tidy`. `stw tidy plan` works on **one repository** and demands a bundle of typed inputs:
`--base` (a full reference), `--state-dir` (a private directory), `--owner`, `--task`, `--remove-registration`, `--remove-files` or
`--completed-worktree`, plus `--protect-worktree` for every active task. The cleanup the owner actually wants is a question, not a
form: "what in my workspace can go?" Meanwhile dozens of completed task worktrees stay locked and registered (61 observed on
2026-10-23: bundledock 17, workharbor 6, sourcesteward 38) because, once an agent has run `git switch -c <branch>` in a worktree created
by `stw workspace apply`, retirement fails with `changed-ownership` ([#174](https://github.com/wstein/sourcesteward/issues/174)): the
registration is bound to the **detached** target the worktree had at creation.

## Goal

`stw tidy` (no arguments, inside or below a workspace) finds the workspace repository, analyses **every member repository and the
workspace repository itself**, prints one **overview of proposed cleanups** with a verdict and evidence per candidate, **asks at the
end what to do**, stores exactly the chosen plan, and `stw tidy apply <plan-id>` executes only that. Base, remote, owner and state
directory are **derived and shown**, never typed. The existing flags stay as the expert layer. Every safety invariant of Specs 011 and
029 holds unchanged: nothing is deleted without proof of integration, planning never deletes, apply needs a stored plan, explicit
authorization, stale checks and a recorded recovery manifest.

## Non-goals

- No new deletion proof and no relaxed one: the proofs stay ancestor, patch-id, merge-noop and no-net-change (Spec 031); unproven work
  is retained and reported.
- No force, no unlock of a lock that is not stw's, no stash, reset or hook skip. Work of other owners or agents (for example
  `/tmp/whr010-*` of a Codex run) is **visible to a human owner and removable only by explicit choice with the full proof**
  ([Scope of ownership is a view](#scope-of-ownership-is-a-view)); an **agent-run** command never touches it.
- No remote deletion, no push, no fetch beyond what `--online` already allows in contract 3; no GitHub write.
- No Windows support (unchanged); no GUI or full-screen TUI: the wizard is line-based prompts.
- No build, release, tag or publication; applying is the owner's operation or needs the owner's explicit authorization of the one
  plan id.
- Batching is a UX layer: each retirement stays one worktree per stored sub-plan (the Spec 029 rule "one worktree per plan" is kept);
  the wizard stores a **plan set** of independent sub-plans, see [Plan set](#plan-set-and-apply).

## User stories

- **US1 (P1, overview).** `stw tidy` in a workspace prints, for every member repository and the workspace repository, the candidates
  grouped by owner or origin (owner column visible), each with its verdict. No flag is needed, nothing is written
  except the private plan store when the owner chooses.
- **US2 (P1, choose and apply).** After the overview the wizard asks what to do (all safe, a numbered selection, nothing); it stores
  the chosen plan set and prints `stw tidy apply <plan-id>`. Apply with explicit authorization executes only the chosen items.
- **US3 (P1, completed task worktrees, #174).** A task worktree created by `stw workspace apply`, then used (`git switch -c`, commits,
  push, PR merged), is retired when its registration still matches and its branch is integrated: registration, files and the proven
  branch go; nothing else.
- **US4 (P1, retain with reasons).** An unmerged branch, a dirty worktree and a locked worktree whose lock is not stw's are retained
  and listed with a stable reason code; none of them can be selected for removal.
- **US4a (P1, whole workspace in view).** The human owner sees every worktree and branch of every owner or tool (Claude, Codex,
  Google, others, unknown) without typing a flag, grouped by owner, and may select items of other owners by an explicit choice; they are
  never preselected and need the same proof.
- **US4b (P1, agents narrow).** An agent runs `stw tidy plan --mine` (or `--by-owner LABEL`); it sees and plans only its own items, and
  its `apply` never touches another owner's item unless the human explicitly authorized that very plan.
- **US5 (P1, derived defaults).** Base, remote, owner and state directory are derived and printed with their source; an ambiguous
  derivation is never guessed: the wizard asks, or in non-interactive mode refuses (exit 1, `base-ambiguous`).
- **US6 (P2, scripts and agents).** `stw tidy plan --json` prints the whole overview as one document; `stw tidy apply <plan-id> --yes`
  applies a stored plan non-interactively. An agent never applies on the owner's behalf without the owner's authorization.
- **US7 (P2, no TTY).** Without a terminal the wizard does not prompt: it prints the overview, stores nothing and exits 0 with the
  hint to run `stw tidy plan --save` or `--json`; it never blocks on stdin.
- **US8 (P2, expert layer).** Every existing `tidy plan|apply|restore|history` invocation behaves byte-for-byte as before.
- **US9 (P3, recovery).** After apply, `stw tidy restore` and `stw tidy history` work on the sub-plans of a plan set exactly as on
  single plans.

## CLI surface

```
stw tidy                      # wizard: find workspace, analyse, overview, ask, store the chosen plan set (never applies)
stw tidy plan [PATH]          # overview only; with no expert flag it is the wizard's analysis, non-interactive
stw tidy apply [PLAN-ID]      # apply a stored plan set (or a legacy single plan) after explicit authorization
stw tidy restore | history    # unchanged
```

Examples (all additive; the flags in the second block are today's expert layer and keep their meaning):

```
stw tidy                                   # interactive: overview, then "Apply which? [a]ll safe / numbers / [n]one"
stw tidy plan                              # overview in text, stores nothing, exit 0
stw tidy plan --json                       # one document, stores nothing
stw tidy plan --save --json                # also stores the plan set; prints "plan_id"
stw tidy apply 3f9c2a7e41b8                # asks "Apply plan 3f9c2a7e41b8 (7 removals)? [y/N]" on a TTY
stw tidy apply 3f9c2a7e41b8 --yes --json   # non-interactive; the explicit plan id is the authorization
stw tidy plan --member sourcesteward --only retire,branches   # narrow the analysis
stw tidy plan --member-base sourcesteward=refs/heads/trunk    # override one derived base
stw tidy plan --state-dir /s/workspace --state-dir /s/scratch # extra stores holding creation records

stw tidy plan --base refs/heads/main --state-dir S --json                                          # expert (Spec 011), repository of the cwd: stays expert
stw tidy plan /path/to/repo --base refs/heads/main --ref refs/heads/done --state-dir S --json      # expert (Spec 011)
stw tidy plan /path/to/repo --base refs/heads/main --state-dir S --task T --owner O --remove-registration --remove-files /p
stw tidy apply --plan ID --state-dir S --authorize --json          # expert (Spec 011/029)
```

**Mode selection (no overlap, back-compat first).** A command line is the **wizard layer** only if it has **no positional `PATH`** and
**every flag it carries is in the wizard set** (`--workspace`, `--mine`, `--by-owner`, `--member`, `--member-base`, `--only`, `--save`,
`--yes`, and the shared flags `--state-dir`, `--online`, `--timeout`, `--json`, `--human`). **Any other flag, or any `PATH`, selects the expert layer**, exactly as today: that includes
`--base` (so `stw tidy plan --base X` without a `PATH`, which works today on the repository of the working directory, **stays expert**), `--ref`,
`--task`, `--owner`, `--remove-registration`, `--remove-files`, `--completed-worktree`, `--protect-worktree`, `--contract`,
`--remote`, `--tracking-remote`, `--repo`, `--host`, `--pr`, `--integration-proof`, `--accept-whitespace-differences`, `--decisions`,
`--consolidation-range`, `--report-dir` and every `--report-max-*`, `--plan`, `--authorize`. The test enumerates the flags registered
by the tidy commands and fails when a new flag is not classified (T015). Back-compat rule: **an invocation that succeeds today behaves
byte-identically**; only invocations that are a usage error or only help today may become wizard forms (bare `stw tidy`, which prints
help today and keeps doing so with `--help`; `stw tidy plan` and `stw tidy plan --state-dir S` without `--base`, which are usage errors
today). `stw tidy apply` with `--plan` is expert; with a positional plan id it is the wizard layer; both together is a usage error
(exit 2). `--owner` and `--repo` keep their expert meanings; the wizard's names are `--by-owner` and `--member`.

**Flag compatibility table** (every flag registered today by `plan`, `apply`, `restore`, `history`):

| Flag | Wizard layer | Expert layer |
| --- | --- | --- |
| `--state-dir`, `--online`, `--timeout`, `--json` | shared; wizard `--state-dir` is repeatable (extra stores) | unchanged (single value) |
| `--base`, `--ref`, `--task`, `--owner`, `--remove-registration`, `--remove-files`, `--completed-worktree`, `--protect-worktree`, `--contract`, `--remote`, `--tracking-remote`, `--repo`, `--host`, `--pr`, `--integration-proof`, `--accept-whitespace-differences`, `--decisions`, `--consolidation-range`, `--report-dir`, `--report-max-*` | not accepted together with wizard-only flags (usage error, exit 2) | unchanged |
| `--plan`, `--authorize` | not accepted with a positional plan id (exit 2) | unchanged |
| `restore` (`--as`, `--action`, `--list`, `--apply`, ...), `history` | unaffected, separate subcommands | unchanged |
| `--workspace`, `--mine`, `--by-owner`, `--member`, `--member-base`, `--only`, `--save`, `--yes` | new | not accepted (exit 2) |

Wizard-layer flags:

| Flag | Meaning |
| --- | --- |
| `--workspace [PATH]` | workspace to use; default: discovery as `stw status` does (Spec 037); outside a workspace a usage error (exit 2) with the hint to give `PATH` or use the expert layer |
| `--mine` | only items owned by the caller's owner label (derived, see below); the narrowing for agents |
| `--by-owner LABEL` | only items of that owner label (repeatable); read-only view in agent mode unless LABEL is the caller label |
| `--member NAME` | restrict to member repositories (the workspace repository is `.`); repeatable |
| `--only KINDS` | comma list of `retire`, `branches`, `tracking`, `stale-worktrees`; default all |
| `--member-base NAME=REF` | override the derived base of one member repository (repeatable); there is no global `--base` in the wizard layer |
| `--state-dir DIR` | repeatable: stores to read creation records from and to write plans to (the first is the plan store); adds to the defaults, see [State directories](#state-directories-and-creation-records) |
| `--online` | allow the contract-3 authenticated reads (default off, as today) |
| `--save` | `plan` only: store the plan set of all *safe* items (non-interactive wizard) |
| `--yes` | `apply` only: skip the confirmation prompt; requires an explicit plan id; refused with no id; **not honoured for plans containing `other` or `unknown` items** (those always need the typed confirmation below) |
| `--json` / `--human` | as `stw status`: text is the default, `--json` selects JSON, `--human` forces text; they exclude each other (exit 2) |
| `--timeout DURATION` | as in contract 3, over the whole analysis |

## Derivation of defaults (FR-010 to FR-016)

Every derived value is printed in a "Derived" block (text and JSON `derived[]`: `name`, `value`, `source`, `overridden`), so the owner
sees what the wizard assumed and how to change it.

| Value | Derivation, first match wins | If ambiguous or absent |
| --- | --- | --- |
| Workspace | `--workspace`, else discovery from the working directory as in Spec 037 (the umbrella repository whose submodules/members are the set) | usage error, exit 2 |
| Member set | the workspace's direct members (submodules and configured repositories) plus the workspace repository `.` | none found: only `.`, with a notice |
| Remote | `origin` if present; else the single remote; | several remotes, none `origin`: ask, or `remote-ambiguous` (exit 1) |
| Base | per repository: the remote's default branch from the symbolic ref `refs/remotes/<remote>/HEAD` (offline); else a repository setting in workspace config (open question 3); jj: the trunk bookmark | no symbolic HEAD and no config, or HEAD pointing to a missing ref: the repository is **not analysed** (`base-ambiguous`); the wizard asks for it on a TTY, the non-interactive run reports it and exits 1 only for that repository's items (others proceed, overall exit 1) |
| Base freshness | the base ref must exist locally; the wizard never fetches (offline); a stale base can only make integration proofs *fewer*, never wrong | notice `base-stale-possible` with the age of the ref |
| Caller label | human mode: `$STW_OWNER`, else the OS user; agent mode: `$STW_OWNER` only (no `--as` flag; it also collides with `restore --as`); none set: `--mine` is a usage error | used for `--mine`, `scope` and agent mode only |
| Item owner | see [Scope of ownership is a view](#scope-of-ownership-is-a-view) | `unknown` |
| Run mode | see [Mode detection](#mode-detection-an-accident-guard) | agent mode is the narrow, safe one |
| State directories | see [State directories](#state-directories-and-creation-records) | refuse `state-dir-unsafe` (exit 1) |
| Protected worktrees | the current working directory's worktree, primary checkouts, dirty or unmerged worktrees and locks that are not stw's are `retain` automatically; `--protect-worktree` is unnecessary in the wizard layer | n/a |

## State directories and creation records

Owned retirement reads the creation records (`plans`, `tasks`, `operations`) from a private **state directory** (Spec 029); the records of
a worktree are in the directory its creator passed to `stw workspace`, which has no default today. This specification adds one:

- **Shared default.** `stw workspace` and `stw tidy` default to `${XDG_STATE_HOME:-~/.local/state}/stw/workspace` (created 0700;
  must be user-owned, not a symlink, outside every repository, mode exactly 0700, else `state-dir-unsafe`) when `--state-dir` is omitted
  (additive: an explicit `--state-dir` behaves as today). `$STW_STATE_DIR` overrides the default for both commands.
- **Known stores** of one wizard run, in order: the repeatable `--state-dir` values, `$STW_STATE_DIR`, the `state_dirs` list of the workspace
  config (a follow-up if OQ3 defers the config; until then flags and environment), the shared default. Each is validated; an unsafe one is
  reported (`state-dir-unsafe`) and skipped, the run continues (exit 1 at the end). The plan store is the first valid store.
  Helper scripts that create worktrees (for example an agent's scratch store) are found by passing that directory once, for example
  `stw tidy --state-dir "$STW_WT_STATE"`; this is documented in `docs/tidy.md` and `docs/workspaces.md`.
- **Record lookup.** For every linked worktree locked with the exact `stw:<task>` form, the creation record is looked up by task id in
  every known store. Found in one store: owned (`owner_source` `task-record`). Found in several with different content: `retain`
  `registration-record-ambiguous`. Found in none: `retain` `registration-record-missing` with the hint `pass the --state-dir that
  created it`; such a worktree is **not** treated as owned and is classed like an unregistered one (see the table).
- A wizard run shows the stores it read and how many records each held (`derived[]`, name `state-dirs`).

## Mode detection (an accident guard)

The wizard distinguishes **human mode** from **agent mode**. This is an **accident guard and a policy aid, not a security boundary
against a deliberately evasive process of the same user**: an agent can drive a pty (`script`, `expect`, `tmux`, `herdr send-keys`) or set
environment variables. The deletion safety never rests on it; it rests on the proofs (SI2), the recovery manifest and the stale checks.
`docs/agent-guide.md` additionally forbids driving `stw tidy` through pty wrappers or `send-keys`.

Human mode requires **all** of: stdin and stdout are terminals; `STW_AGENT` is unset; no known agent marker is in the environment
(`CLAUDECODE`, `CLAUDE_CODE_*`, `CODEX_*`, `AGY_*`; a code table with tests, extendable by pull request); and neither `--mine` nor `--by-owner` is given.
Otherwise the run is agent mode. `--mine` forces agent mode.

**Mode evidence** (terminal yes/no per stream, markers seen, caller label and its source, derived mode) is recorded in the stored plan,
in the apply journal, and is shown by `stw tidy history`.

## Candidate classes and verdicts

Analysis is **read-only** (Spec 011 evidence, one repository at a time through `internal/fanout`, width 4, repositories in sorted
order for output). Each candidate has exactly one `verdict` and, separately, an `owner` and a `scope` (`own`, `other`, `unknown`; see below):

| Verdict | Meaning | Selectable |
| --- | --- | --- |
| `safe` | proven integrated and every precondition holds | yes |
| `retain` | kept; a stable `reason` explains | no |
| `decision` | safe in principle but needs the owner (for example a locked worktree with an stw lock, legacy unregistered worktree) | yes, with an explicit second confirmation per item |

Candidate kinds: `worktree` (linked task worktree: registration, files, optionally its branch), `branch` (local branch or jj bookmark),
`tracking-ref` (local remote-tracking ref, contract 3), `stale-worktree` (registration with a missing directory, `prunable`).

Reason codes (stable, kebab-case; the list extends Spec 011/029 codes, none is renamed): `unmerged` (no proof), `dirty`,
`ignored-extras`, `locked-foreign`, `locked-stw` (decision), `unregistered` (information: no stw creation record), `current-worktree`, `primary-checkout`,
`protected`, `recently-active`, `registration-record-missing`, `registration-record-ambiguous`, `scope-not-authorized`, `base-ambiguous`, `base-missing`, `changed-ownership` (the registration no longer binds the worktree: now only for a
real identity change, see below), `registration-moved`, `lock-mismatch`, `branch-attached-elsewhere`, `retirement-file-limit`,
`retirement-unrecorded-entry`, `retirement-sibling-refused`, `git-tree-refused`, `evidence-incomplete`, `state-dir-unsafe`.

### Required behaviour per case

| Case | Verdict | Effects if selected |
| --- | --- | --- |
| Completed task worktree: exact `stw:<task>` lock, matching creation record in a known store, clean, branch integrated (every `stw workspace apply` worktree is locked this way, so this is the normal owned case) | `safe` | remove registration + files; delete the branch iff it is a proven-integrated local branch in the same plan |
| Same, but branch not integrated | `retain` `unmerged` | none; report the ahead commits count and the closest evidence (for example "3 commits ahead, not in origin/main") |
| Dirty or has untracked/ignored extras the stored tree does not hold | `retain` `dirty` or `ignored-extras` | none |
| Lock in the `stw:<task>` format, but no record (`registration-record-missing`) or a mismatching record (`lock-mismatch`) | `decision` `locked-stw` | only through the legacy completion of Spec 029 (exact path, declared complete by the human's explicit choice); no `git worktree unlock --force` of a lock that is not ours; human mode only |
| Locked with any other lock | `retain` `locked-foreign` | none |
| Worktree of another owner or created by another tool (no stw registration), proven integrated, clean, unlocked, not recently active | `safe`, `scope` `other` or `unknown`, evidence `no-work` when it has no commit of its own | human mode: shown, never preselected, removable only by explicit choice; removal of an unregistered worktree uses the Spec 029 legacy completion (exact path, declared complete, cooperative); agent mode: not shown and not plannable |
| The same, but dirty, unmerged or locked by another | `retain` | none |
| Worktree of scope `other` or `unknown` whose `HEAD`, index or any tracked-file mtime is within the last 24 hours (a possibly running job, for example a fresh `/tmp/whr010-*` at the base commit is trivially an ancestor and clean) | `retain` `recently-active` | none; the window is OQ15 |
| Worktree directory missing, registration present | `safe` (`stale-worktree`) if stw-owned and the Git admin entry proves prunable; else `retain` | registration removal only |
| Branch integrated, not checked out anywhere | `safe` | delete branch with recovery ref (Spec 011 WI2) |
| Branch integrated but checked out in a retained worktree | `retain` `branch-attached-elsewhere` | none |
| The worktree of the current working directory | `retain` `current-worktree` | none |

## Scope of ownership is a view

Owner amendment (2026-10-23, supersedes any earlier "never touched" wording for other agents' work, which now applies to **agent-run
mode only**). A human owner running `stw tidy` is responsible for everything in the workspace: their own, Claude's, Codex's, Google's and
anyone's worktrees and branches. Ownership therefore **filters the view; it is not a safety exclusion**. Deletion safety rests on the
proofs, not on the label.

**Item owner derivation** (first match wins; the source is shown as `owner_source`):

1. `task-record`: the owner label of the stw creation record or lock (authoritative);
2. `path-convention`: a table of path/branch prefixes maps to a label (default table in code, extendable by workspace config, OQ13), for example `/tmp/whr*` and `codex/` to `codex`, `claude-*` and `claude/` to `claude`;
3. `commit-author`: the author of the branch tip, only as a hint label `author:<name>`, never authoritative (OQ13);
4. otherwise `unknown`.

`scope` is `own` when the owner equals the caller label, `other` when it is a different known label, `unknown` otherwise.

**Human mode (default).** Everything is shown, **grouped by owner**, with the owner column visible; the `--mine` and `--by-owner LABEL` flags
narrow it. No candidate with `scope` `other` or `unknown` is ever preselected: `a` (all safe) in the wizard selects `own` items only,
the other owners' items are chosen by number or by `o` ("also all safe items of owner LABEL", asked per owner) and carry a clear marking
(`[other: codex]`, `[unknown owner]`) in the overview, in the confirmation prompt and in the stored plan (`scope`, `owner`). The proof
requirements (integration proof, clean tree, no non-stw lock, recovery manifest) are identical for every owner.

**Agent mode** (see [Mode detection](#mode-detection-an-accident-guard)). `plan` lists and plans only `own` items, and `own` in agent mode
requires `owner_source` `task-record` (a path or author guess never makes an item `own` for an agent); `--by-owner LABEL` of another
label is a read-only view. `apply` refuses (exit 1, `scope-not-authorized`) any sub-plan with an `other` or `unknown` item **unless the
plan carries `authorized_scopes`**, which is written only by the human path, is part of the content-addressed `plan_id` (editing it
changes the id and the store entry no longer matches the id typed at apply), and is recorded with its mode evidence. `--save` in agent
mode never includes `other` or `unknown` items. The residual risk (an evasive same-user process forging the human path or the
label) is accepted and documented; proofs and the recovery manifest still bound the harm.

**Typed confirmation for foreign items.** Choosing an `other` or `unknown` item in the wizard, and `apply` of a plan with
`authorized_scopes`, requires a confirmation **read from `/dev/tty`** (never stdin): the owner label followed by the first 6 characters
of the plan id, for example `codex 3f9c2a`. A bare `y` does not suffice; `--yes` is not honoured for such plans. Without a controlling terminal
the confirmation fails and nothing is stored or applied.

## Fixing #174: ownership binding after a branch switch

**Two causes, verified against the code at `afef06f`.** Owned observation (`tidy_v3_legacy.go`) refuses a creation-bound worktree for two
independent reasons:

1. **Position.** It requires `target.HeadRef == ""` (detached). After `git switch -c <branch>` the HEAD is attached: `changed-ownership`.
2. **Creation-time safety digest.** It calls `Verify` on the creation plan (`git.go`), which recomputes `gitSafety(source, BaseCommit)`
   and requires equality with the digest stored at creation. That digest covers the whole common config (`config --list --includes
   --show-origin`), the hook fingerprint and the Git binary's size and mtime. Anything legitimate that changes them (a `git push -u`
   or `push.autoSetupRemote` writing `branch.<b>.remote/merge`, a Git upgrade, a devcontainer rebuild) fails `Verify`, surfaced as
   `changed-ownership` too. Fixing only the position still leaves such worktrees refused (a probe on the reviewed head confirmed
   it).

**Design (stated, not silent).**

- **Position rule.** HEAD may be the detached creation commit, or attached to a local branch, or detached at a descendant, provided its
  history **contains the recorded creation (base) commit** (`merge-base --is-ancestor`). An unrelated checkout is `changed-ownership`. Today's
  code has no creation-commit binding at all (it only requires "detached"), so the descendant check is a **new restriction** next to the
  widening. The plan already binds `Target.HeadRef` and `CommitID`; **no new plan field** is needed and plans stored before stay valid.
- **Verify split.** `Verify` is split into (a) an **identity part**: lock `stw:<task>`, gitdir backlink, worktrees parent, layout, creation
  journal entry; and (b) the **creation digest equality**. Retirement uses (a) and replaces (b) with the **fresh `gitSafety` admission it already
  runs** (`tidy_v3_legacy.go`), against the current safety policy. The creation digest equality stays for the workspace and handoff paths,
  unchanged. This is a deliberate, documented change of what retirement requires, **not** a silent weakening of the digest: instead of "the
  safety-relevant state equals what it was at creation", retirement requires "the safety-relevant state is acceptable now", which is the
  question that matters for deleting files, and the plan stores the fresh digest so apply re-checks that it did not change after planning.
  **No new refusal code and no policy change:** retirement runs in planning mode, where hooks, `core.hooksPath`, fsmonitor and `submodule.*`
  are notes that are switched off at apply (Spec 031 FR-020/021). A hook added before planning is a note and the worktree still
  retires; a hook or key added between plan and apply refuses through the existing `evidence-changed` stale check; an unsafe
  layout keeps its existing code. Existing refusal codes are kept (renaming one would change expert-layer output). The split applies at
  **both** `Verify` call sites of the owned retirement path; the older v2 retirement path keeps its rule. The re-bind alternative (a human-authorized, journaled re-registration step in the plan) is
  rejected as default because it asks for a confirmation per worktree (61); it remains available as the `decision` path of the legacy completion.
- **Integration proof of the real HEAD** (amended: before this amendment retirement proved native ancestry only, which retains every
  rebase-merged worktree). Retirement now reuses the Spec 011 forge-free proofs on the real HEAD through the same code as local-reference
  plans: ancestor, patch-id, merge-noop or no-net-change, honouring `--integration-proof` and `--accept-whitespace-differences`. The
  policy is stored in the plan (`proof_policy`); a plan stored without one keeps the ancestry proof. A head the proofs do not cover
  (extra unmerged commit, whitespace-only difference without the flag, a class left out) stays `retain` `integration-not-proven`.
  Clean tree, no extras, no submodule or symlink problem as before. Consequence: Spec 011 US1-AS2 ("an empty commit alone never
  authorizes deletion") no longer holds for retirement, as Spec 031 FR-004a already changed it for references: an empty commit, or a
  commit whose net change is nil, is proven by no-net-change and the worktree is retired. This is safe because the worktree is clean
  and has no extras, and apply pins the HEAD under a recovery reference before anything is removed. The retirement plan deadline
  includes the 60 s forge-free proof phase of local-reference plans; the apply deadline keeps its per-entry scaling.
- **Stale binding.** The stored plan binds HEAD ref, OID, branch tip and base; any change refuses with the existing `evidence-changed` (the stale re-check of the code; no `stale` refusal code exists).

**Branch leg (new plan form, defined).** `owned-retirement` and `legacy-retirement` plans carry no selected refs today and the branch is
attached while planned, so Spec 011 ref planning would retain it. The wizard therefore plans a **dependent branch sub-plan**: it records
branch name, tip OID, proof and `depends_on` (the retirement sub-plan id). At apply it runs only **after the retirement succeeded**, is
re-observed (the branch is then unattached), and goes through the unchanged Spec 011 reference deletion with **all its protections**:
`selected-base`, workspace name and OID, attached elsewhere, symbolic ref, tip equal to the planned OID, proof re-run, recovery ref and
journal. A failed or stale retirement makes the dependent sub-plan `skipped`. A worktree whose HEAD is attached to the base branch itself
(for example `main`) never gets a branch leg. This combined form is new, so it is stated as such; it is **not** "exactly Spec 011/029
plans".

**Compatibility.** Creation records and journals stay valid (same fields). Documented in `docs/tidy.md` as the lifecycle
*workspace -> work -> retirement* (acceptance item of #174).

## Plan set and apply

`stw tidy` stores a **plan set**: a manifest of independent sub-plans (one per repository per effect kind, retirement sub-plans one per
worktree, the existing Spec 011/029 plan kinds with their current ids, plus the dependent branch leg defined above) plus a content-addressed `plan_id` over the sorted sub-plan ids, the
derived values, the selected items, `authorized_scopes` and the mode evidence. Planning never deletes and writes only the private store, and only when the owner confirms
(wizard) or passes `--save`.

- `stw tidy apply <plan-id>` loads the set, verifies the store (mode, owner), re-runs each sub-plan's stale checks, and applies the
  sub-plans **one at a time in a fixed order** (worktree retirements, then branch/bookmark deletions, then tracking refs), each with its own
  recorded journal and recovery manifest. Authorization is explicit: on a TTY the prompt names the plan id and the effect counts; with
  `--yes` the explicit plan id is the authorization (never inferred, never accepted without an id, never from a plan produced in the
  same invocation).
- **Partial progress is honest.** A sub-plan that turns stale or fails is reported (`stale`, `failed`), the remaining sub-plans of other
  repositories continue only when independent, a retirement and its dependent branch leg are applied in that order (the branch leg only after
  the retirement succeeded). Exit 1 if any sub-plan did not complete; the report lists every outcome and the recovery location.
- A plan id of the older single-plan form still works with `apply --plan` (expert layer).
- Agents never run `apply` on the owner's behalf without the owner's authorization of that plan id (repository rule, restated here and
  in `docs/agent-guide.md`); `plan` and `plan --save` are agent-safe.

## Wizard interaction

```
$ stw tidy
Workspace: /workspaces/Anchorage   (source: discovery from the working directory)
Derived:   remote origin, base per repository (see below), state dirs ~/.local/state/stw/workspace (2 records), /s/wt (59 records), mode human (tty yes, no agent marker)
Analysed 7 repositories in 6 s (offline; base last fetched 2 h ago in the oldest repository)

sourcesteward   base refs/heads/main (origin/HEAD)
  [1] safe      worktree  /tmp/.../wt-037  task spec037-x  branch codex/037-x  integrated (patch-id)   remove registration, files, branch
  [2] safe      worktree  /tmp/.../wt-038  task spec038-x  branch codex/038-x  integrated (ancestor)   remove registration, files, branch
  [3] retain    worktree  /tmp/.../wt-040  branch codex/040-x   unmerged: 2 commits ahead of origin/main
  [4] retain    worktree  /tmp/.../wt-041  dirty: 3 modified files
  [5] safe      worktree  /tmp/whr010-a    owner codex (path convention) [other: codex]  integrated (ancestor)  not preselected
bundledock      base refs/heads/main (origin/HEAD)
  [6] safe      branch    codex/old-1   integrated (merge-noop)   delete branch (recovery ref kept)
summary: 4 safe (3 own, 1 other), 2 retain   (13 other worktrees and branches kept; see --json)

Apply which? [a]ll own safe  [1,2,6 / 1-2,6]  [o]wner codex  [d]etails N  [n]one (default n): a
Stored plan set 3f9c2a7e41b8 (3 effects in 2 repositories). Nothing has been changed.
Apply it now? Run:  stw tidy apply 3f9c2a7e41b8        (or answer y here) [y/N]: n
```

Rules: the default answer is always *none*/*no*; only `safe` and `decision` rows get a number that can be chosen (a `decision` row asks
a second confirmation naming the lock); the answer is parsed strictly (anything else re-asks up to 3 times, then exits 1 with
nothing stored); on EOF or no TTY the wizard behaves as `plan` without `--save` (US7). Applying inside the wizard is allowed only after
the explicit `y` on that plan id and runs the very same `apply` code path (no separate implementation). All text passes the shared
`esc` and the 120-character bound; the overview shows at most 10 rows per repository in text with `and N more (use --json or
details)`, and JSON lists every candidate.

## JSON

One document `tidy-overview` (schema `schemas/v2/tidy-overview.schema.json`, additive, v1/v3 tidy schemas unchanged): `workspace`,
`derived[]`, `repositories[]` (`name`, `path`, `backend`, `base`, `remote`, `state` `analysed|skipped|error`, `reason`), `candidates[]`
(`id`, `repo`, `kind`, `verdict`, `reason`, `owner`, `owner_source`, `scope`, `path`, `branch`, `task`, `proof` `ancestor|patch-id|merge-noop|no-net-change|none`,
`effects[]`, `evidence`), `summary` (counts per verdict), `mode` (evidence), `plan_id` (empty unless saved), `notices[]`. Item ids are
`KIND:REPO:SUBJECT`, stable across runs. Output is bounded (candidates 4096, evidence 512 bytes each, document 4 MiB; beyond that
`truncated: true` and exit 1 `output-limit`).

## Exit codes (consistent with existing tidy codes)

| Code | Meaning |
| --- | --- |
| 0 | analysis complete (including retained and other-owner candidates) or apply fully completed; also the no-TTY degradation of the wizard |
| 1 | refusal, incomplete or partial evidence, a repository `error`/`base-ambiguous`, stale or failed sub-plan, cancellation, operational or output failure |
| 2 | invalid usage (mixed layers, `--yes` without id, unknown `--only`, outside a workspace, bad flag value) |

## Safety invariants (carried, not restated away)

SI1 planning never deletes and never writes outside the private store; SI2 no deletion without a proof of integration (Specs 011/031);
SI3 apply needs a stored plan, explicit authorization, fresh stale checks and a recorded recovery root plus journal before native
effects; SI4 other owners' work is touched only by a human's explicit choice with the full proof, never preselected, behind a typed `/dev/tty` confirmation, and never by agent-run mode without that human authorization; mode detection is an accident guard, not a boundary; SI5 derivation never guesses an ambiguous value; SI6 untrusted text is quoted; SI7 Git and
jj both supported as in Spec 011 (jj: bookmarks and empty working-copy retirement under the existing limits); SI8 Windows unsupported;
SI9 no network unless `--online`; SI10 the token never appears in output.

## Requirements

### Functional

- **FR-001** `stw tidy` with no arguments runs the wizard; `stw tidy plan` with no expert flag runs the same analysis non-interactively.
- **FR-002** Mode selection is exactly the rule above (no `PATH` and only wizard-set flags); every invocation that succeeds today, including `plan --base X` without `PATH`, keeps its behaviour byte-identically; the wizard flags `--member` and `--by-owner` do not reuse `--repo` and `--owner`.
- **FR-003** Workspace discovery is that of `stw status`; the member set plus the workspace repository are analysed.
- **FR-004** Analysis runs per repository through `internal/fanout` (width 4); one repository's failure never hides the others.
- **FR-005** Every candidate has exactly one verdict and, for non-`safe`, a stable reason code.
- **FR-006** Every candidate carries `owner`, `owner_source` and `scope` (`own`, `other`, `unknown`); the owner column is always visible in text and present in JSON.
- **FR-006a** Default for a human: all candidates of all owners are visible without any flag, grouped by owner.
- **FR-006b** Candidates with `scope` `other` or `unknown` are never preselected (`a` selects `own` only); they are selected only by explicit choice, marked clearly in the overview, the prompt and the stored plan, and need the same proof as any other.
- **FR-006c** `--mine` and `--by-owner LABEL` narrow the view and the plan; agent mode (see Mode detection) lists and plans own items only, and `own` there requires `owner_source` `task-record`.
- **FR-006d** `apply` in agent mode refuses (`scope-not-authorized`, exit 1) any sub-plan with an `other` or `unknown` item unless the stored plan carries `authorized_scopes`, which is part of the content-addressed plan id and recorded with the mode evidence.
- **FR-006f** Choosing an `other` or `unknown` item, and applying a plan with `authorized_scopes`, needs the typed confirmation (owner label and plan-id prefix) read from `/dev/tty`; `--yes` is not honoured for such plans.
- **FR-006g** Mode detection is documented as an accident guard, not a boundary; human mode needs both streams on terminals, no `STW_AGENT`, no agent marker; mode evidence is recorded in plan and journal and shown by `history`.
- **FR-006h** An `other` or `unknown` worktree that is recently active (default 24 h) is `retain` `recently-active`; a safe one with no commit of its own carries evidence `no-work`.
- **FR-006e** The owner of an item is derived by the ordered rule above; the source is reported; `commit-author` is a hint only and never makes an item `own`.
- **FR-007** The overview shows derived values with their sources; `derived[]` is in JSON.
- **FR-008** The wizard asks after the overview; the default is none; parsing is strict; selection is by number, range or `a`.
- **FR-009** Without a TTY on stdin or stdout the wizard prints the overview, stores nothing, never reads stdin, and exits 0 unless the analysis itself ends in an error (a repository `error` or `base-ambiguous` is exit 1 as in the exit table).
- **FR-010** Base is derived per repository from the remote's symbolic HEAD (or workspace config, OQ3); never guessed; ambiguity asks or refuses.
- **FR-011** Remote derivation: `origin`, else the single remote, else ambiguity as FR-010.
- **FR-012** `stw workspace` and `stw tidy` share the default state directory `${XDG_STATE_HOME:-~/.local/state}/stw/workspace` (0700, user-owned, not a symlink, outside repositories); an unsafe store is reported and skipped.
- **FR-012a** The wizard reads creation records from every known store (repeatable `--state-dir`, `$STW_STATE_DIR`, workspace config, default); a `stw:<task>` worktree without a record is `retain` `registration-record-missing` with a hint, with several differing records `registration-record-ambiguous`.
- **FR-013** Owner for retirement is the registered owner; no typed owner is needed.
- **FR-014** Every derived value is overridable (`--member-base`, `--state-dir`, `--workspace`).
- **FR-015** The worktree of the working directory, primary checkouts and any non-provably-completed worktree are protected automatically.
- **FR-016** `--online` is the only way to network; default offline.
- **FR-020** Ownership identity (admin dir, path, common dir, lock, format, journal) stays exact; HEAD position may be the detached creation commit or any descendant, attached or detached (#174).
- **FR-020a** Retirement does not require equality with the creation-time safety digest; it requires the fresh safety admission, stores the fresh digest in the plan and re-checks it at apply. Hooks and similar settings stay notes in planning mode as today (Spec 031); a change between plan and apply refuses through the existing `evidence-changed`; no refusal code is added or renamed. The split applies at both `Verify` call sites; the v2 retirement path is unchanged. Workspace and handoff paths keep the creation digest check.
- **FR-021** A HEAD that does not contain the creation commit is `changed-ownership`.
- **FR-022** Retirement requires the Spec 011 integration proof of the real HEAD (retirement reuses those proofs and their two flags), a clean tree and no extras; the branch is deleted only if proven integrated, listed in the plan and attached nowhere else.
- **FR-023** The stored retirement plan binds HEAD ref and OID (existing fields); apply refuses (the existing `evidence-changed`) on any change of HEAD, branch tip or base.
- **FR-024** Existing creation records, journals and stored plans need no migration.
- **FR-025** The branch leg is a dependent sub-plan applied after a successful retirement, re-observed, under all Spec 011 ref protections; never planned for the base branch.
- **FR-030** `--save` and the wizard store a plan set of independent sub-plans (one worktree per retirement sub-plan) with a content-addressed `plan_id`.
- **FR-031** `apply <plan-id>` verifies the store, re-runs stale checks and applies sub-plans one at a time in the fixed order, each with its own journal and recovery manifest.
- **FR-032** `--yes` requires an explicit plan id on the command line (never a plan created by the same invocation); without one it is a usage error (exit 2).
- **FR-033** Partial outcomes are reported per sub-plan; exit 1 if any sub-plan is not complete; a failed retirement makes its dependent branch leg `skipped`.
- **FR-034** Planning never deletes; no effect outside apply.
- **FR-040** `--json` emits one `tidy-overview` document; text and JSON carry the same evidence.
- **FR-041** Output is bounded (rows, evidence, document) and truncation is marked, never silent.
- **FR-042** All untrusted strings pass `esc` and the 120-character bound; no ANSI, CR or bidi reaches the terminal.
- **FR-043** Exit codes 0/1/2 as in the table.
- **FR-050** Git and jj backends behave as in Spec 011; unsupported proof retains.
- **FR-051** Windows is unsupported; no Windows content.

### Success criteria

- **SC-001** On a fixture reproducing the 2026-10-23 situation (stw-created worktrees whose records are in a known store passed by `--state-dir`, branches pushed with `-u` and merged by rebase), the overview classifies every worktree, proposes each completed one as `safe`, and `stw tidy` with the answer `a` plus `stw tidy apply` removes them with recovery evidence. Worktrees whose records live in a store not passed are `retain` `registration-record-missing`, never guessed.
- **SC-002** A fixture for every case of the behaviour table yields the stated verdict and reason, and no fixture of `retain` is ever changed by apply (mutation probe: forcing any of them into the plan makes apply refuse).
- **SC-003** The real sequence of #174 (apply workspace detached, `git switch -c b`, commit, `git push -u` to a bare remote, rebase-merge so SHAs differ, `tidy`) retires registration, directory and branch; a changed unrelated config key and a changed Git binary identity retire; a hook added before planning is a note and the worktree still retires, one added between plan and apply refuses with the existing `evidence-changed`; wrong owner, moved or foreign lock, dirty tree and unintegrated branch are refused with their codes.
- **SC-004** `stw tidy` with no TTY never blocks (test with closed and with silent stdin) and stores nothing.
- **SC-005** Existing tidy tests, goldens and schemas pass byte-unchanged; every flag registered by the tidy commands is classified (wizard, expert or shared), and `plan --base X` without `PATH` stays expert.
- **SC-006** An ambiguous base never produces a candidate for that repository (tests: no `refs/remotes/origin/HEAD`, two remotes, dangling HEAD).
- **SC-007** Stale checks: moving HEAD, the branch tip or the base between plan and apply makes apply refuse with the existing `evidence-changed` and change nothing.
- **SC-008** Output bounds hold under 5000 candidates; `esc` holds for ANSI, CR, NUL and bidi in branch names, tasks and paths.
- **SC-009** `make check` and `scripts/check-docs.py` pass; independent review of the exact head is CLEAR.
- **SC-010** With worktrees of Claude, Codex and an unknown owner present, a human `stw tidy` with no flag lists all of them grouped by owner with the owner column; JSON carries `owner`, `owner_source`, `scope` for each.
- **SC-011** Answering `a` selects own safe items only; an other or unknown item is in the stored plan only if chosen by number or `o` with the typed `/dev/tty` confirmation, and is then marked in plan and prompt.
- **SC-012** `--mine` lists and plans own items only (agent mode: only `task-record` owners); agent-mode `apply` of a plan with an other or unknown item and no `authorized_scopes` exits 1 `scope-not-authorized` and changes nothing; editing `authorized_scopes` in the store changes the plan id and is refused; a plan with `authorized_scopes` is applied only by the human path: the typed `/dev/tty` confirmation is required, so an agent-mode apply of it fails closed without a controlling terminal (exit 1, nothing changed).
- **SC-013** State directories: the shared default is used by both commands; repeatable `--state-dir` finds records in several stores; missing and ambiguous records give their reasons and hints; an unsafe store is skipped with exit 1 at the end.
- **SC-014** Mode detection: pty with `STW_AGENT` or an agent marker yields agent mode; both streams on terminals without markers yields human mode; the mode evidence is in plan, journal and `history`; `--mine` forces agent mode.
- **SC-015** The branch leg: HEAD attached to the base branch never gets a leg; a failed retirement skips the leg; the leg re-checks Spec 011 protections and the planned tip OID.
- **SC-016** A recently active other or unknown worktree is `retain` `recently-active`; an old one with no commit carries `no-work`.

## Open questions

Each has a recommended default; the foreman decides.

1. **Ownership binding (#174).** Identity exact plus position-may-advance (recommended), or re-binding by owner confirmation, or storing
   the branch name in the registration at first switch (needs a hook, rejected: stw does not watch worktrees). Default: position-may-advance.
2. **Does the wizard apply itself?** Recommended: yes only after a typed `y` for the plan id, using the same apply code; `stw tidy` alone never applies.
3. **Base source.** Recommended: remote symbolic HEAD only in v1, workspace config (`.stw/tidy.toml`, key `base` per repository) as a follow-up if a
   repository lacks a symbolic HEAD. Alternative: ship the config now.
4. **State directory default location.** Superseded: the shared default is `${XDG_STATE_HOME:-~/.local/state}/stw/workspace` for `stw workspace` and `stw tidy`. Alternative: inside the workspace repository's `.stw/` (rejected: inside a repository).
5. **`locked-stw` as `decision`.** Now only for an stw-format lock without a (matching) record. Recommended: selectable in human mode through the legacy completion with a second confirmation. Alternative: always `retain`.
6. **Plan set versus single plans.** Recommended: plan set of independent sub-plans (keeps the Spec 029 one-worktree rule). Alternative: lift the rule to batch plans (larger review surface).
7. **Wizard flags `--only`, `--member`.** Recommended: keep, small. Alternative: omit in v1.
8. **Tracking refs in the default analysis.** Recommended: only with `--online` (contract 3 needs it); offline wizard skips them with a notice.
9. **jj in the wizard.** Recommended: include bookmarks and empty working-copy retirement exactly as Spec 011; no new jj proof.
10. **Prompt library.** Recommended: plain line prompts on stdin/stdout, no dependency. Alternative: a TUI library (rejected: pinned-dependency cost).
12. **Human versus agent mode detection.** Recommended: the accident guard of Mode detection (both terminals, no `STW_AGENT`, no agent marker table). Alternative: an explicit `--as-agent` flag (rejected: an agent could omit it).
13. **Owner of an unregistered worktree.** Recommended: ordered derivation task record, then path/branch-prefix table (`/tmp/whr*` and `codex/` = codex, `claude/` = claude, others unknown) extendable in workspace config, then `unknown`; the branch-tip author is shown as a hint only and never grants `own`. Alternative: author-based owner (rejected: agents commit under the human's identity).
14. **Removing an unregistered foreign worktree.** Recommended: via the Spec 029 legacy completion path (exact path, declared complete by the human's explicit choice, same proofs). Alternative: refuse until adopted into a registration (safer, slower).
15. **Recently-active window and bare `stw tidy`.** **Decided (foreman):** 24 hours; bare `stw tidy` becomes the wizard, `stw tidy --help` stays help. Original recommendation: 24 hours for `other`/`unknown` scope, and bare `stw tidy` becomes the wizard (it printed help; `--help` stays). Alternative: 4 hours; bare `stw tidy` stays help and the wizard is `stw tidy wizard`.
16. **Workspace config list of state dirs.** **Decided (foreman):** later (flags, `$STW_STATE_DIR` and the shared default first). Original recommendation: ship flags and `$STW_STATE_DIR` first, the config key with OQ3. Alternative: ship both together.
11. **`stw status` hint.** Recommended: the `branches-unmerged` and `worktree-stale` items (Spec 037 rows 23, 24) change their suggested command to `stw tidy` once this ships.

## Docs impact

`docs/tidy.md` (wizard section first, lifecycle workspace -> work -> retirement, the position rule, reason codes, derived defaults, plan set),
`docs/commands.md` (rows and examples), `docs/json-contracts.md` and `schemas/v2/tidy-overview.schema.json` with fixtures and freeze
digest, `docs/agent-guide.md` (agents plan and `--save`, never apply without the owner), `docs/workspaces.md` (retirement after a
branch switch, the shared default state directory of `stw workspace`, `--state-dir` for helper scripts), `docs/status.md` and Spec 037 rows 23 and 24 (suggested command, OQ11), `docs/progress.md`, `docs/README.md`,
`docs/roadmap.md`, `docs/feature-status.json` (status `planned`), `docs/development.md` if a state-directory default needs
mentioning. Specs 011 and 029 get a pointer to this spec; their text is not rewritten.

## Risks

- Position widening could be mistaken for weaker ownership: mitigated by exact identity, the real-HEAD integration proof and stale
  binding of HEAD; covered by refusal fixtures and mutation probes.
- A batch prompt makes "all" tempting: mitigated by default none, a summary with effect counts, `decision` rows never included in `a`.
- Derived state directory may collide with the user's existing store: if a plan store is present it is reused; unsafe modes refuse.
- Wizard analysing many repositories may be slow: bounded by width 4, `--timeout`, and per-repository results kept on expiry.
