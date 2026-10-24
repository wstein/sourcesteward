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
- **US4b (P1, agents narrow).** An agent runs `stw tidy plan --mine`; it sees and plans only its own items, and
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
| `--by-owner LABEL` | only items of that owner label (repeatable); narrows the view in any mode and does not select the run mode: a human may then choose those items (typed confirmation), an agent only sees them |
| `--member NAME` | restrict to member repositories (the workspace repository is `.`); repeatable |
| `--only KINDS` | comma list of `retire`, `branches`, `tracking`, `stale-worktrees`; default all |
| `--member-base NAME=REF` | override the derived base of one member repository (repeatable); there is no global `--base` in the wizard layer |
| `--state-dir DIR` | repeatable: stores to read creation records from and to write plans to (the first is the plan store); adds to the defaults, see [State directories](#state-directories-and-creation-records) |
| `--online` | allow the contract-3 authenticated reads (default off, as today) |
| `--save` | `plan` only: store the plan set of the *safe* items of scope `own` (non-interactive wizard; in agent mode only owners from a task record) |
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
  created it`; such a worktree is **not** treated as owned and offers no deletion path (see the table).
- A wizard run shows the stores it read and how many records each held (`derived[]`, name `state-dirs`).

## Mode detection (an accident guard)

The wizard distinguishes **human mode** from **agent mode**. This is an **accident guard and a policy aid, not a security boundary
against a deliberately evasive process of the same user**: an agent can drive a pty (`script`, `expect`, `tmux`, `herdr send-keys`) or set
environment variables. The deletion safety never rests on it; it rests on the proofs (SI2), the recovery manifest and the stale checks.
`docs/agent-guide.md` additionally forbids driving `stw tidy` through pty wrappers or `send-keys`.

Human mode requires **all** of: stdin and stdout are terminals; `STW_AGENT` is unset; no known agent marker is in the environment
(`CLAUDECODE`, `CLAUDE_CODE_*`, `CODEX_*`, `AGY_*`; a code table with tests, extendable by pull request); and `--mine` is not given.
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
| `decision` | safe in principle but needs the owner (for example a stw-locked worktree whose record does not match, `lock-mismatch`; legacy unregistered worktree) | yes, with an explicit second confirmation per item |

Candidate kinds: `worktree` (linked task worktree: registration, files, optionally its branch), `branch` (local branch or jj bookmark),
`tracking-ref` (local remote-tracking ref, contract 3), `stale-worktree` (registration with a missing directory, `prunable`).

Reason codes (stable, kebab-case; the list extends Spec 011/029 codes, none is renamed): `unmerged` (no proof), `dirty`,
`ignored-extras`, `locked-foreign`, `locked-stw` (decision, `lock-mismatch` only), `unregistered` (information: no stw creation record), `current-worktree`, `primary-checkout`,
`protected`, `recently-active`, `registration-record-missing`, `registration-record-ambiguous`, `scope-not-authorized`, `base-ambiguous`, `base-missing`, `changed-ownership` (the registration no longer binds the worktree: now only for a
real identity change, see below), `registration-moved`, `lock-mismatch`, `branch-attached-elsewhere`, `retirement-file-limit`,
`retirement-unrecorded-entry`, `retirement-sibling-refused`, `git-tree-refused`, `evidence-incomplete`, `state-dir-unsafe`; `workspace-not-found` (no workspace discovered and no `--workspace`: usage error, exit 2) and the notice `workspace-members-none` (only the workspace repository is analysed).

### Required behaviour per case

| Case | Verdict | Effects if selected |
| --- | --- | --- |
| Completed task worktree: exact `stw:<task>` lock, matching creation record in a known store, clean, branch integrated (every `stw workspace apply` worktree is locked this way, so this is the normal owned case) | `safe` | remove registration + files; delete the branch iff it is a proven-integrated local branch in the same plan |
| Same, but branch not integrated | `retain` `unmerged` | none; report the ahead commits count and the closest evidence (for example "3 commits ahead, not in origin/main") |
| Dirty or has untracked/ignored extras the stored tree does not hold | `retain` `dirty` or `ignored-extras` | none |
| Lock in the `stw:<task>` format, no creation record in any known store (usually a wrong or missing `--state-dir`) | `retain` `registration-record-missing`, hint `pass the --state-dir that created it` | none; no deletion path is offered |
| Lock in the `stw:<task>` format, a record exists but does not match (`lock-mismatch`) | `decision` `locked-stw` | only through the legacy completion of Spec 029 (exact path, declared complete by the human's explicit choice); no `git worktree unlock --force` of a lock that is not ours; human mode only |
| Locked with any other lock | `retain` `locked-foreign` | none |
| Worktree of another owner or created by another tool (no stw registration), proven integrated, clean, unlocked, not recently active | `safe`, `scope` `other` or `unknown`, evidence `no-work` when it has no commit of its own | human mode: shown, never preselected, removable only by explicit choice; removal of an unregistered worktree uses the Spec 029 legacy completion (exact path, declared complete, cooperative); agent mode: not shown and not plannable |
| The same, but dirty, unmerged or locked by another | `retain` | none |
| Worktree of scope `other` or `unknown` whose `HEAD`, index or any tracked-file mtime is within the last 24 hours (a possibly running job, for example a fresh `/tmp/whr010-*` at the base commit is trivially an ancestor and clean) | `retain` `recently-active` | none; the window is OQ15 |
| Worktree directory missing, registration present | `safe` (`stale-worktree`) if stw-owned and the Git admin entry proves prunable; else `retain` | registration removal only |
| Branch integrated, not checked out anywhere | `safe` | delete branch with recovery ref (Spec 011 WI2) |
| Branch integrated but checked out in a retained worktree | `retain` `branch-attached-elsewhere` | none |
| The worktree of the current working directory | `retain` `current-worktree` | none |

## Read-only observers (FR-052)

The classifier reads facts; `internal/workspace` supplies them through new exported **read-only observers**, the only evidence source of the
wizard's analysis. `PlanTidyV3` and the retirement planning are not used for the overview: they write plan records into a state store, and the
overview writes nothing. The observers reuse the existing building blocks (the Git planning policy, the filter-free retirement comparison, the
Spec 011 integration proofs, the jj session) and add no new proof.

Per repository one call, `workspace.ObserveWorktrees(ctx, ObserveOptions) (WorktreeObservation, error)`:

- `ObserveOptions{Path string; Base string; Online bool; Remote string}`. `Base` is the reference name the derivation (WP-2,
  `derive.Base`) produced for this repository (for example `refs/remotes/origin/main`, or the jj trunk bookmark); it must exist locally and is
  resolved to a commit once, reported as `BaseOID`; it is never fetched. `Online` and `Remote` are set only by `--online`; without `Online` no network is
  attempted and `Remote` must be empty. The options have no timeout field: `--timeout` bounds the whole analysis (CLI surface), so the analysis owns one deadline, passes it down in the `context`, and each repository's observation and proof phase are bounded by the smaller of that deadline and the 60 s and 60 s windows below.
- `WorktreeObservation{Backend, Base, BaseOID string; Worktrees []WorktreeEvidence; Branches []BranchEvidence; TrackingRefs []TrackingRefEvidence; TrackingSkipped string; Limitations []string}`.
- `WorktreeEvidence`, registration facts (always kept, never zeroed by a later failure): `Path`, `HeadOID`, `HeadRef` (empty when detached), `Primary`,
  `Locked`, `LockReason`, `Prunable`, `DirMissing`. Content facts: `Dirty`, `IgnoredExtras`, `LastActivity`, `Ahead`, `AuthorHint`, `HasOwnCommit`
  (HEAD has a commit the base lacks), `Proof` (`ancestor|patch-id|merge-noop|no-net-change|none` of HEAD against the base, the Spec 011 proofs with
  the default policy, no forge). `Incomplete` is empty or a short reason that applies to the content facts, which are then zero.
- `BranchEvidence` (`Name`, `OID`, `IsBase`, `Proof`, `Ahead`, `AuthorHint`, `Incomplete`) and `TrackingRefEvidence` (`Name`, `Proof`, `Incomplete`) carry their own `Incomplete`. `BranchEvidence.Name` is the full `refs/heads/` name; `IsBase` is true for the base
  reference itself and for the local branch of the same short name as a remote-tracking base (`refs/heads/main` for `refs/remotes/origin/main`). A tracking reference's `Proof` is
  `github-branch-absent` (the typed advertisement lacks the source branch) or `none`.
- **Primary checkout.** The primary checkout is the worktree whose Git directory is the repository's common directory (`GitDir == RepositoryDir` of its native identity). This holds for
  a `.git` directory, a `--separate-git-dir` checkout and a submodule checkout alike (a gitfile `.git` is no sign of a linked worktree), and is decided from the identity read at the registered path, not
  from the shape of `.git`. It is listed with its registration facts only: its content facts stay zero without `Incomplete`, because it is retained as `primary-checkout` whatever its content. When the identity cannot be read, the
  item is `Incomplete`. A repository with a bare registration is refused as a whole (the existing registration rule).
- **The directory is the registered worktree.** Before any content fact is read, the identity at the registered path must have the same repository (common directory), the registered HEAD OID and the registered
  HEAD ref, and, for a linked worktree, an administrative directory whose `gitdir` file names this path. Anything else (another repository, a clone, another linked worktree, a HEAD that moved since the
  registrations were listed) is `Incomplete` `registration-mismatch`, never observed as clean and proven.
- The stw lock form (`stw:<task>`) is read from `LockReason`; the creation record lookup stays in the state-directory code (WP-2) and the adapter
  joins the two. The observers never look at a state store.
- **Tracking references** need the network proof of contract 3 (OQ8). The tracking evidence exists today only inside `tidyV3TrackingCoordinator.planTracking`
  (`tidy_v3_tracking.go`), which also writes a plan; PR B extracts its read-only part (the observation and proof up to, not including, plan validation and the store write) into a function the
  observer and `planTracking` both call, with `planTracking`'s behaviour and tests unchanged. With `Online` the observer calls it (a hosting session, the extra 30 s online phase of `planOnlineTimeout`, no write) and fills `TrackingRefs`; without it the slice is empty and
  `TrackingSkipped` is `tracking-requires-online`.

**Failure rule.** An error, timeout, exceeded bound or unreadable path while observing one worktree, branch or reference sets the `Incomplete` reason of
that item and leaves its content facts zero; the registration facts stay. The adapter maps `Incomplete` to a classifier fact on `Worktree`, `Branch` and
`TrackingRef` (T011a3, test first) that yields `retain` `evidence-incomplete` for every owner scope; the classifier's zero-`LastActivity` rule covers only
other and unknown scopes and is not relied on. Nothing is guessed or defaulted to a safe value, and an `Incomplete` item is never `Proof` `none`. The reasons are closed words or failure codes, never a path or a message of the repository: `worktree-limit`,
`branch-limit`, `registration-mismatch`, `activity-unreadable`, `deadline-exceeded`, `observation-failed`, the proof reasons `proof-limit`, `object-dir-unavailable`, `ambiguous-merge-base`, `proof-budget-exhausted`, `history-override` and
`unsupported-metadata` (shallow or grafted history and alternate object stores are tolerated as hard stops of the proofs, not as a refusal of the whole repository), and the failure code of a refused check (for example `retirement-file-limit`, `git-tree-refused`). An error of the
repository as a whole (not a repository, base missing) is the returned `error`: the repository is `error` in the overview and the others proceed.

**From refusals to facts.** The retirement checks answer yes or refuse; the observers need facts. The mapping is a table in the tests (`TestObserveContentOutcomeTable`, one row per outcome):

| Check | Outcome | Fact |
| --- | --- | --- |
| stored files (HEAD tree and `ls-files --stage`) | success | none, the manifest check follows |
| | `retirement-index-not-clean` (staged, conflicted or missing index entries) | `Dirty`; the untracked check is not made |
| | `retirement-file-limit`, `git-tree-refused`, deadline, any other error | `Incomplete` with that code or word |
| manifest (the disk against the stored tree) | success | clean |
| | tracked file differs from the stored tree, or is missing (sentinel errors `errRetirementStoredDiffers`, `errRetirementStoredMissing`) | `Dirty` |
| | `retirement-unrecorded-entry` (untracked or ignored entry) | `IgnoredExtras` |
| | `retirement-file-limit`, deadline, any other error | `Incomplete` |

The walk stops at the first finding in path order, so when `IgnoredExtras` is reported `Dirty` is not determined, and when `Dirty` comes from the index `IgnoredExtras` is not; both retain. The other content facts (last activity, ahead, author) are kept for a
dirty worktree. The shared proof budget (`tidyV3ProofBudget`) allows only three observations per operation, which would run out after about three worktrees, so each worktree gets its own content budget (a fresh `newTidyV3ProofBudget`,
bounded by the same per-worktree limits as one retirement plan); the repository-level cap stays the 60 s observation deadline and the worktree limit, and a worktree that exceeds its budget is `Incomplete`.

**Git without `git status`.** Every Git command runs under `runSandboxed` with no write roots and the planning policy of `git_planning.go`
(`planningPolicyFactory`: `planningGitOptions`, `planningConfigArgs`, `planningEnvironment`: no global or system configuration, no hooks, no filters, no
replace objects, `core.fsmonitor` off, `GIT_OPTIONAL_LOCKS=0`); the bare `gitRead` is not enough because it leaves global fsmonitor, filter and hook
configuration active. `git status` is not run: it can refresh the index stat information, write the untracked cache, start or update the fsmonitor
token or daemon and write a split index; none of these can happen here because no observer calls it, and the planning policy (locks off, fsmonitor off, no hooks) is the second line of defence for every other command. `Dirty` and `IgnoredExtras` come from the filter-free
comparison of retirement (`retirement_git.go` stored-files check and `retirement_manifest_v3.go` manifest: the disk against the HEAD tree, untracked and ignored entries
against the stored tree) and the same stored-files check compares the index against HEAD with `ls-files --stage` (which reads and never refreshes the index), so `diff-index` is not run: `retirement-index-not-clean` is `Dirty`, and then the untracked check is not made (`IgnoredExtras` stays false, the worktree is retained as dirty anyway). `LastActivity` is the newest
modification time (`lstat`) of the worktree's HEAD file, its index file and every tracked file (when the index differs from HEAD the tracked files are not listed and only HEAD and the index count), at most 8,192 files and directories per worktree (the bound of the
retirement manifest, which counts both, `retirement-file-limit`; the 16,384 `checkoutLeafLimit` bounds only the index listing); more makes the worktree `Incomplete`. A deleted tracked file contributes no time. Other commands: worktree list, for-each-ref, rev-list count and log of the tip. The identity inspection of a path (`inspection.Inspect`, used by every tidy plan) runs outside the sandbox: it executes only read-only `rev-parse` queries with `core.fsmonitor=false` and `GIT_OPTIONAL_LOCKS=0`, which read configuration and refs and run no hook or filter, and the write test below covers it. A repository with an active filter attribute is refused as a whole by the existing admission, so no observer command ever runs a filter.

**jj.** Every jj command goes through the existing `jjSession.query` form: `--ignore-working-copy` and `--at-operation=<observed operation>` (the operation
is observed once per repository and pinned, and observed again at the end), so nothing snapshots the working copy and no operation is written or merged; no jj argument list is added beyond the reads
the existing inspection already makes (`bookmark list`, `workspace list`, `log`, `file list`, `file show`, `diff`, `sparse list`). The new read-only function is `inspection.ObserveJJWorkspaces`
(workspace registrations, local bookmarks, and ahead count and author of each commit); proofs of bookmarks come from `inspection.ObserveJJTidy` and the stored files of a workspace from
`inspection.ObserveJJRetirement`. Facts:

- **Bookmarks** (local only): `Name` is the bookmark name, `IsBase` is true for the bookmark named like the base, `Proof` is `ancestor` (native ancestry) or `none` (jj proves integration by ancestry only, as Spec 011), `Ahead` the commits
  that are not ancestors of the base, `AuthorHint` the author name. **The native tidy decides what may be deleted**: only its `delete` decision is a proof for deletion; every keep reason of the native tidy
  (`remote-counterpart` for a bookmark with a remote counterpart, `recovery-reference` for `stw-recovery-*`, `selected-base` for the base and any alias at the base commit, `workspace-used`) is passed on as `BranchEvidence.Reason`, and the
  classifier retains the bookmark with that reason and no delete effect. (In a colocated repository every bookmark has a `@git` counterpart, so it is retained as `remote-counterpart`, as stw tidy does.) A bookmark with no single target is `Incomplete`
  `ambiguous-target`; a bookmark whose commit differs between the tidy read and the registrations read is `Incomplete` `evidence-changed`.
- **Workspaces** are told apart from bookmarks and from Git worktrees by their facts: `HeadOID` is the working-copy commit, `HeadRef` is empty (a workspace has no attached branch, so a workspace can never carry a
  delete-branch effect), nothing is `Locked`, a missing directory is `DirMissing` and `Prunable`. The primary workspace is the one whose `.jj/repo` is a directory; its content facts stay zero without `Incomplete`.
  The workspace the observation starts from, when it is not the primary one, cannot be compared with itself: `Incomplete` `source-workspace`. A directory at a registered path that is not a workspace of this repository
  (another jj repository, whose own `.jj/repo` is a directory) is `Incomplete` `registration-mismatch` and never primary. jj 0.45.1 reports no root for a workspace whose directory was deleted (it prints `null`; jj 0.46 still reports the
  registered root): the path is then unknown and the workspace is `Incomplete` `workspace-path-unknown`, never present and clean; the tests assert exactly that for the version in use.
- **One operation per run.** The retirement read of each workspace pins its own operation; the observer requires it to equal the operation of the registrations read and the working-copy commit to equal the registered one, otherwise the
  workspace is `Incomplete` `evidence-changed`, so no record mixes facts of two operations.
- **Content** of a secondary workspace is the disk compared with the files stored in its working-copy commit at the pinned operation, as the jj retirement admission does (`ObserveJJRetirement` for the stored
  files, `observeRetirementManifest` for the disk). A tracked file that differs or is missing is `Dirty` and an entry the stored tree does not hold is `IgnoredExtras` (sentinel errors of the contract 2 manifest,
  as for Git; the first finding in path order wins); both are relative to the pinned operation, so an edit that is not yet snapshotted shows as a difference, which is the safe direction. Only a comparison that cannot
  complete is `Incomplete` (its failure code or a closed word). `Proof` is `ancestor` for an integrated working-copy commit, `empty-working-copy-integrated-parent` for an empty working copy on an integrated parent (the empty
  working-copy retirement of Spec 011, SI7, OQ9) and `none` otherwise; `HasOwnCommit` is `Ahead > 0` except for the empty working copy. `LastActivity` is the newest modification time of `.jj/working_copy/tree_state`,
  `.jj/working_copy/checkout` and every stored file.
- **Bounds.** At most 1,024 files and directories per workspace (the contract 2 manifest bound, `retirement-file-limit`: more is `Incomplete`), 10 s per workspace (`jjTimeout`), 60 s for the whole observation, and the shared
  1,024 registration bound. History (ahead count and author) is read for at most 256 commits, the workspace commits first and then the bookmark commits in order; a commit beyond that is absent and its item is `Incomplete`
  `history-unavailable` (about 12 ms a commit measured). The existing bookmark read has its own output bound: 200 bookmarks on distinct commits take about 2.5 s, and about 400 exceed the output limit, which fails the whole repository as an
  `error` (fail closed, no partial observation). Each workspace reads its stored files with one jj process per file, so the number of workspaces a run can finish is bounded by that time; measured: a primary and 20 secondary workspaces
  with ten tracked files each take about 9 s. A read that fails while the observation's context is done is `Incomplete` `deadline-exceeded`; a single jj call that exceeds its own 10 s limit while the observation is still running fails
  as `observation-failed`. Neither is ever clean. A secondary workspace of a colocated repository carries a `.git` file and `.jj/.gitignore` with jj 0.46 (not with 0.45.1), which the retirement
  admission does not hold: they show as `IgnoredExtras` and the workspace is retained, as retirement would refuse it.
- **No lock, no owner in v1.** A jj workspace has no stw lock, so the adapter cannot join it to its creation record: in v1 every jj workspace is `unregistered` (owner by path convention, never `own`, and selectable only by an explicit human
  choice with the Spec 029 legacy completion). stw-created jj workspaces are therefore not treated as owned in v1: a documented, fail-closed gap, closed by T011a5 (join the creation records by destination path).
- **No tracking evidence.** `TrackingSkipped` is `tracking-unsupported-for-jj` online and offline.

Closed reasons added by jj: `source-workspace`, `workspace-path-unknown`, `registration-mismatch`, `ambiguous-target`, `history-unavailable`, `evidence-changed`. jj workspaces thus stay eligible for the empty working-copy rule; this reconciles the safety invariants and OQ9 with the read-only rule.

**Read-only guarantee as a test.** The sandbox mounts the file system read-only, so a probe inside it cannot expose a write. The tests therefore require: a fixture whose files are
stat-dirty and content-clean in the primary checkout and in each linked worktree (each has its own index), with an untracked cache and a configured fsmonitor hook; a snapshot before and after
(repository bytes including refs, config and every `.git/worktrees/<name>/index`, every worktree's files, contents and times, the state directory and the markers directory), required equal after the observers
ran (a) sandboxed and (b) with the sandbox switched off for every native command of the process (a test-only switch at the one place commands are prepared, `prepareSandboxed`, so the reused retirement checks and streaming reads are covered too).
The harness is shown to be able to fail: a plain `git status` with its default environment over the same fixture (and over a fixture with a clean filter) changes a worktree's index and runs the fsmonitor hook and
the filter, and the snapshot shows it. What the tests do **not** claim: that the planning policy is what stops those effects. `git status` through the native runner is neutralized by
`GIT_OPTIONAL_LOCKS=0`, the pinned `core.fsmonitor=false` and the hooks path, and a mutation that adds it there is not detected; the guard against it is that no observer calls `git status`, which the code review and
the mutation probes of the pull request check, while a command that writes despite the policy (an `update-ref`, an index refresh with locks) is detected by (b). A second fixture with an online tracking remote served by the existing
hosting test double covers the tracking path. For jj the snapshot covers the repository (operation log, operation heads, the Git refs of a colocated store), every workspace's files and times and its working-copy state, with a plain `jj status` as the sensitivity check; the jj session proves a read-only sandbox before its first query and refuses to run without it, so the run cannot be repeated without the sandbox.

**Bounds (stated numbers; the 61-worktree fixture of the tests confirms them and this section is amended if it cannot).** At most 1024 worktrees and 1024
branches per repository (`observationLimit`), the surplus sorted by path or name is `Incomplete`. One observation deadline of 60 s (`planObservationTimeout`) and
one integration proof phase of 60 s for the repository (`integrationPhaseTimeout`, Spec 029 FR-008), with the existing per-command timeout of 10 s
(`integrationCommandTimeout`) and per-tip timeout of 20 s (`integrationTipTimeout`); the proof of a commit is computed once per distinct HEAD or tip OID. When the phase is
exhausted, the remaining items are `Incomplete` (`proof-budget-exhausted`), never `none`. With 61 worktrees the phase leaves about 1 s per tip on average, which suffices for the
ancestor and merge-noop proofs. The 61-worktree fixture (`TestObserveSixtyOneRealisticWorktrees`) uses realistic checkouts (2,000 tracked files each, generated at test time, shared object store), sixty distinct tips of which thirty need the patch-id evaluation against a base that moved on, and measured about 10 s in total (observation and proofs) on the development machine, far inside the 60 s and 60 s windows. Native command output is bounded by the existing stream limits.

**Adapter to the classifier (T011a3).** `tidywizard.Adapter` is the `Source` of the classifier over the observers. It maps `Incomplete` to a fact on `Worktree`, `Branch` and `TrackingRef` that the classifier
retains as `evidence-incomplete` for every owner scope (never selectable, no effect, and never a delete-branch effect of a safe worktree). A lock is stw's only in the exact `stw:<task>` form; the creation
record is looked up with `derive.LookupTask`, and a record owns the worktree only when the lookup names a `task-record` source, the record names this task and this path (`RecordFound`); a record that names
another task or path is a lock mismatch, none (or a lookup that gives neither a record nor an ambiguity), a failed lookup and a missing record are missing, and several differing ones are ambiguous. Proofs are
**per kind**: a worktree or a branch is proven by `ancestor`, `patch-id`, `merge-noop` or `no-net-change` (or none), a remote-tracking reference by `github-branch-absent` (or none); a word of another kind, an unknown word, or none
for a finished observation of a linked worktree is `Incomplete` `unknown-proof`. The primary checkout and a registration without directory need no proof. A tracking reference keeps the reason the tracking evidence gave
(`remote-source-present`, a protection such as `workspace-used`) as its retain reason, and its owner convention is matched on the branch part of `refs/remotes/<remote>/<branch>`. A repository with a skip reason is not
observed. The observation's `TrackingSkipped` and `Limitations` travel as the notices of the repository (`RepoResult.Notices`, at most 8, each at most 512 bytes), which the overview shows. Repositories are analysed one after the
other behind the one-line seam of `Classify`; the width 4 of `internal/fanout` waits for Spec 040 (T011a4). An observation error of the repository as a whole is returned and reported as `error` for that repository only.

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

**Human mode (default).** Everything is shown, **grouped by owner**, with the owner column visible; `--by-owner LABEL` narrows it (for
example to look at, and deliberately choose, the items of `codex`) and `--mine` is the agents' narrowing flag (own items only; it selects agent mode). No candidate with `scope` `other` or `unknown` is ever preselected: `a` (all safe) in the wizard selects `own` items only,
the other owners' items are chosen by number or by `o` ("also all safe items of owner LABEL", asked per owner) and carry a clear marking
(`[other: codex]`, `[unknown owner]`) in the overview, in the confirmation prompt and in the stored plan (`scope`, `owner`). The proof
requirements (integration proof, clean tree, no non-stw lock, recovery manifest) are identical for every owner.

**Agent mode** (see [Mode detection](#mode-detection-an-accident-guard)). `plan` lists and plans only `own` items, and `own` in agent mode
requires `owner_source` `task-record` (a path or author guess never makes an item `own` for an agent); `--by-owner LABEL` of another
label is a view only: agent-mode planning admits only own `task-record` items whatever the flags, and only the human path (human mode plus the typed `/dev/tty` confirmation) writes or applies `authorized_scopes`. Mode detection stays an accident guard. `apply` refuses (exit 1, `scope-not-authorized`) any sub-plan with an `other` or `unknown` item **unless the
plan carries `authorized_scopes`**, which is written only by the human path, is part of the content-addressed `plan_id` (editing it
changes the id and the store entry no longer matches the id typed at apply), and is recorded with its mode evidence. `--save` in agent
mode never includes `other` or `unknown` items. The residual risk (an evasive same-user process forging the human path or the
label) is accepted and documented; proofs and the recovery manifest still bound the harm.

**Typed confirmation for foreign items.** Choosing an `other` or `unknown` item in the wizard, and `apply` of a plan with
`authorized_scopes`, requires a confirmation **read from `/dev/tty`** (never stdin): the owner label followed by the first 6 characters
of the plan id, for example `codex 3f9c2a`; for every item of scope `unknown`, and for an owner label that is empty or not one word, the confirmation word is the literal word `unknown` (`unknown 3f9c2a`), asked once per word. An owner label that is literally `unknown` belongs to the unknown group: same word, same prompt. A bare `y` does not suffice; `--yes` is not honoured for such plans. Without a controlling terminal
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
The plan id needs the ids of the sub-plans, and the sub-plans are ordinary stored plans, so they are written to the private store before the plan set is stored. When a typed confirmation is required (a foreign item) and the answer is wrong, the sub-plans stay as stored plans that no plan set references and nothing applies; with no terminal available the build refuses before any sub-plan is made. The plan set itself is stored only after the confirmation.

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
- **FR-006c** `--by-owner LABEL` narrows the view and the plan in any mode and is not a mode input; `--mine` narrows to own items and selects agent mode. Agent mode (see Mode detection) lists and plans own items only, and `own` there requires `owner_source` `task-record`. A human using `--by-owner` stays in human mode and may choose those items with the typed confirmation; an agent with `--by-owner` sees them but cannot plan or apply them.
- **FR-006d** `apply` in agent mode refuses (`scope-not-authorized`, exit 1) any sub-plan with an `other` or `unknown` item unless the stored plan carries `authorized_scopes`, which is part of the content-addressed plan id and recorded with the mode evidence; only the human path applies a plan with `authorized_scopes`.
- **FR-006f** Choosing an `other` or `unknown` item, and applying a plan with `authorized_scopes`, needs the typed confirmation (confirmation word, the owner label or the literal word `unknown`, and plan-id prefix) read from `/dev/tty`; `--yes` is not honoured for such plans.
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
- **FR-052** The wizard's evidence comes from exported read-only observers of `internal/workspace` (no plan record, no store write, no `git status`, no fetch, jj pinned with `--ignore-working-copy --at-operation`); any observation error makes the item `evidence-incomplete`; the read-only guarantee is a test that can fail.

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
- **SC-012** `--mine` lists and plans own items only (agent mode: only `task-record` owners); agent-mode `apply` of a plan with an other or unknown item and no `authorized_scopes` exits 1 `scope-not-authorized` and changes nothing; editing `authorized_scopes` in the store changes the plan id and is refused; a plan with `authorized_scopes` is applied only by the human path: the typed `/dev/tty` confirmation is required and only the human path applies it, so an agent-mode apply of it is refused (exit 1, nothing changed); without a controlling terminal the confirmation itself fails closed.
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
5. **`locked-stw` as `decision`.** Only for `lock-mismatch` (a record exists but does not match); a missing record is `retain` `registration-record-missing`. Recommended: selectable in human mode through the legacy completion with a second confirmation. Alternative: always `retain`.
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
