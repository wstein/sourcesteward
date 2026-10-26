# Evidence-based tidy

`stw tidy plan` reviews local Git branches or native jj bookmarks against an explicit base.
It reports every selected target as `delete` or `retain`, with native identities, evidence,
workspace blockers and capability limits. A `delete` decision describes eligibility in this
sample; it grants no authorization and the planning command performs no deletion.

```sh
stw tidy plan /path/to/repository --base refs/heads/main --json
stw tidy plan /path/to/repository --base refs/heads/main --ref refs/heads/finished-task --json
```

PATH defaults to the current directory. `--ref` is repeatable and literal; omitting it selects
all bounded local references. Git selections use full names such as `refs/heads/finished-task`;
jj selections use bookmark names such as `finished-task`. Names are not shell commands, globs or arbitrary revsets.
An unknown or duplicate selected reference refuses the plan. Git base selectors also require a full reference name or full commit ID.
The base is required; SourceSteward
does not infer `main`, `master` or `origin`.

## The tidy wizard

`stw tidy` cleans up a whole workspace instead of one repository. It is for the person at a terminal; agents use `stw tidy plan`
(see below). Outside a Git workspace it exits 2 with `workspace-not-found` (discovery is Git only and starts from the working directory; `stw tidy --workspace PATH` names another workspace, or use the
expert layer described in the rest of this guide).

```text
$ stw tidy
Workspace: /ws
Derived:   workspace /ws (discovery), remote:a origin (origin), remote:b origin (origin)
Mode:      human (stdin tty, stdout tty, no agent marker)
Analysed 2 repositories (0 skipped, 0 with errors)

Repositories:
a   base refs/heads/main
b   base refs/heads/main

owner claude (own)
  a
    [1] safe     worktree     /w/own1  task t1  integrated (ancestor)  effects remove-registration, remove-files
    [2] safe     branch       claude/old  integrated (ancestor)  effects delete-branch:claude/old
  b
    [-] retain   worktree     /w/dirty
    [3] safe     tracking-ref refs/remotes/origin/x  integrated (ancestor)  effects remove-registration, remove-files

owner codex (other)
  a
    [4] safe     worktree     /w/whr1  owner codex (path-convention) [other]  integrated (ancestor)  effects remove-registration, remove-files  not preselected

owner unknown
  b
    [5] decision worktree     /w/dec  owner unknown (unknown) [unknown]

summary: 4 safe (3 own, 1 other), 1 retain, 1 decision
Apply which? [a]ll own safe  [1,2,6 / 1-2,6]  [o]wner  [d]etails N  [n]one (default n):
Stored plan set 3f9c2a7e41b8 (5 effects in 2 repositories). Nothing has been changed.
Apply it now? Run: stw tidy apply 3f9c2a7e41b8 (or answer y here) [y/N]:
```

The sample is the real output of the wizard on the fixture of `TestTidyExampleMatchesTheDocs` (the answers `a` and `n` are typed and not echoed; the plan id is the first 12 hex characters of a SHA-256 over the plan content and is shown here as `3f9c2a7e41b8` so the sample stays stable).

**Flags.** Bare `stw tidy` takes `--workspace`, `--mine`, `--by-owner`, `--member`, `--member-base`, `--only`, a repeatable `--state-dir`, `--online`, `--timeout`, `--json` and `--human`; `stw tidy plan` takes the same and `--save`. Bare `stw tidy` refuses `--yes` and `--save` with exit 2 (`--yes` belongs to `apply PLAN-ID` and never authorizes a plan made in the same run; `--save` is `plan` only). `stw tidy --json` prints the overview document like `stw tidy plan --json`: it never prompts, stores or applies and has no `plan_id`.

**Observation window.** The analysis observes each repository under a deadline of 60 s plus 1 s per worktree (jj: workspace) and per local branch (jj: bookmark) of that repository, at most 10 minutes; the Git proof phase gets the rest of that window, at least 60 s, so without `--timeout` it can run up to 60 s past the window; with `--timeout` the given deadline bounds everything. `--timeout` is the deadline over the whole analysis; when given it also replaces that window, so it may be larger than the old fixed 60 s, up to 1 hour (more is a usage error, exit 2). The per-call limits (10 s per jj workspace, 10 s per proof command, 20 s per tip) and the plan, apply and restore deadlines of the expert layer are not scaled. When candidates are retained as `evidence-incomplete` with the evidence `deadline-exceeded` or `proof-budget-exhausted`, the overview ends with a notice `N of M not judged (deadline or proof budget); retry with a larger window, for example --timeout 15m`: N is the number of those candidates (`--timeout` widens the window, the content reads and the proof phase, but not the 20 s per-tip proof budget nor the per-worktree content budget, which also end as `proof-budget-exhausted` and are counted too, so the hint can fail for those items) and M the number of all candidates of the overview, counted after the view is narrowed (`--only`, `--mine`, `--by-owner`) and before the output is cut at the candidate bound. The notice is the text line `notice: ...` and an entry of the `notices` array of `--json`.

**Flow.** Analysis is read only and offline unless `--online`. Each repository gets a derived remote (`origin`, else the single remote)
and base (the local branch of the remote's symbolic HEAD, else the remote-tracking reference); override a base with
`--member-base NAME=REF`. A repository whose remote or base cannot be derived is shown as skipped (`remote-ambiguous`, `base-ambiguous`,
`base-missing`) and makes the run exit 1 after the overview. Creation records are read from the shared state directory
(`${XDG_STATE_HOME:-~/.local/state}/stw/workspace` or `$STW_STATE_DIR`) and every `--state-dir`; the first valid store holds the plan set.

**Modes.** Human mode needs a terminal on both stdin and stdout and no agent marker (`STW_AGENT`, `CLAUDECODE`, `CLAUDE_CODE_*`,
`CODEX_*`, `AGY_*`). Every other run is agent mode, and `--mine` forces it. A human sees every owner, grouped: one heading per owner (`owner NAME (own)`, `owner NAME (other)`, `owner unknown`; own first, other owners sorted, unknown last) with a sub-heading per repository, at most 10 rows per repository in a group, and the owner column;
nothing owned by another owner or of unknown owner is preselected. An agent should narrow the view with `--mine`; in agent mode only
items owned by a task record count as `own`. `--by-owner LABEL` narrows the view in any mode and never selects the mode. The mode is
recorded in the plan set, the apply journal and `stw tidy history`.

**Verdicts.** `safe` (every proof holds), `decision` (for example a `locked-stw` worktree whose lock does not match), `retain`
(a reason such as `unmerged`, `dirty`, `recently-active`, `registration-record-missing`, `evidence-incomplete`,
`stale-registration-no-plan-kind`) rows never carry an effect; items of another or an unknown owner are shown with their `scope` and are never preselected. Only `safe` and `decision` rows get a number.
A worktree whose directory is missing is shown as a `retain` row and is not pruned. Such a registration does not stop `plan --save`: the plan for the other items of the same repository is stored, and the stale registration stays untouched and bound into the stored native evidence (one that appears or disappears before the apply refuses with `evidence-changed`). Only a registration that Git marks `prunable`, that is not locked and whose directory is absent below a real parent directory is accepted this way; a locked, replaced or aliased one still refuses the repository with `unknown-workspace-path`.
Text shows at most 10 rows per repository inside each owner group (`and N more`); `--json` lists all candidates.

**Prompts.** The answer is parsed strictly and the default is none. `a` chooses all safe rows of scope own (never `decision` rows and
never rows of other or unknown owners), numbers and ranges (`1,2,6`, `1-3`) choose rows, `o` asks per owner, `d N` shows the details of
row N, and Enter or `n` chooses nothing. Anything else asks again; after three invalid answers the wizard exits 1 and stores nothing.
A `decision` row asks a second confirmation that names its lock and path. A foreign item needs the typed confirmation read from
`/dev/tty` (the owner label, or `unknown`, followed by the first six characters of the plan id) while the plan set is built
and again when it is applied; `y`, standard input and `--yes` never replace it. With no terminal, in agent mode or when nothing is
choosable, the wizard prints the overview, stores nothing, never reads stdin and exits 0 unless the evidence is incomplete, which exits 1 as for `plan`: a repository that could not be analysed, a repository skipped as `base-ambiguous`, `base-missing` or `remote-ambiguous`, a state directory skipped as unsafe, or an overview cut at its bounds (`output-limit`).

**Plan and apply.** The chosen rows become one stored plan set with a content-addressed id; its sub-plans are the existing
reference, tracking and retirement plans. Storing changes nothing. `stw tidy apply PLAN-ID` applies the sub-plans in a fixed order with
fresh checks, a journal and a recovery reference before each native effect; a retirement's dependent branch deletion runs after, and
only after, that retirement succeeded. A partial failure is reported per sub-plan and exits 1; replaying a completed set is a verified
no-op. In an agent run, an `other` or `unknown` item is refused with `scope-not-authorized`; agents never apply foreign items and must
not drive the wizard through a pty wrapper or `send-keys`. The wizard's own `Apply it now?` question accepts only an explicit `y`, which
runs the same apply code for that plan id.

**Several sub-plans of one repository.** Every sub-plan stores the whole repository as it was when the set was planned, so the first
retirement of a repository would make the next one look changed (a registration is gone, a recovery reference exists). Apply therefore
explains exactly those effects: before it compares a sub-plan with the repository, it adds the effects recorded in the journal of every
other sub-plan of the same repository and state directory whose journal is complete, and those of the branch deletions this run applied,
to the stored baseline. The same baseline serves the integration proofs that are re-evaluated at apply. Three owned worktrees of one
repository retire in one run; retirement and branch sub-plans of a repository explain one another. Nothing else is tolerated: a new
commit, branch, worktree or reference and a new entry beside a target or its registration directory still end the later sub-plan as
`stale` with `evidence-changed`, a dirty or changed target is refused as before, and a sub-plan that failed or was skipped, or whose
journal is not complete, explains nothing. Limit: the branch deletion that follows a retirement is planned and journaled at apply time
under a new plan id that no stored record links to the retirement. A run knows only the branch deletions it applied itself. If an
earlier run applied a retirement with its branch deletion and a later retirement of that repository was not applied in that run (it was
stale, refused, failed or never reached), the later run cannot explain the earlier branch deletion, so that retirement reports `stale`
and needs a new plan.

**Non-interactive use.**

```sh
stw tidy plan --mine --json            # overview of the caller's own items (needs $STW_OWNER, else exit 2; use --by-owner NAME)
stw tidy plan --save --json            # store the safe own items; prints plan_id
stw tidy apply PLAN-ID --yes --json    # explicit id authorizes; refused without an id
```

**Exit codes.** 0 analysis complete (also with retained or other-owner candidates, and the no-terminal degradation when the evidence is complete) or apply fully
completed. 1 refusal, incomplete evidence, a repository `error`, a repository skipped as `base-ambiguous`, `base-missing` or `remote-ambiguous`, an unsafe state directory, stale or failed sub-plan, cancellation
(`--timeout`, interrupt), a store problem, or output cut at its bounds (`output-limit`); the overview is printed first. 2 invalid usage:
mixed layers, `--yes` without an id, `--plan` with a positional id, unknown `--only`, `--mine` without `$STW_OWNER`, a missing workspace.

The JSON form is the `tidy-overview` document (`schemas/v2/tidy-overview.schema.json`, not yet published to Pages).

### Lifecycle and position rule

The lifecycle of a task worktree is *workspace -> work -> retirement*. `stw workspace apply` creates it detached at the recorded
creation commit and locks it `stw:<task>`; the agent then works in it, typically `git switch -c <branch>`, commits and pushes
(`git push -u` writes `branch.<b>.remote` and `branch.<b>.merge` into the common config); the branch is integrated into the base,
possibly by rebase-merge with different commit ids; the wizard then retires the worktree.

An owned retirement used to be refused as `changed-ownership` after such a branch switch (issue #174). It now follows a
**position rule**: HEAD may be the detached creation commit, attached to a local branch, or detached at a descendant, provided its
history contains the recorded creation commit; an unrelated checkout is still `changed-ownership`. Retirement checks the identity of
the worktree (the `stw:<task>` lock, the gitdir backlink, the worktrees parent, the layout and the creation journal entry) and the
**current** safety state, instead of requiring the safety digest of the creation time: an unrelated config key, a Git upgrade or the
upstream written by `git push -u` no longer blocks it, while a hook or key added between plan and apply refuses with
`evidence-changed`. Integration is proved on the real HEAD with the same forge-free proofs as local references (ancestor, `patch-id`,
`merge-noop`, `no-net-change`). The proof-policy flags `--integration-proof` and `--accept-whitespace-differences` belong to the expert
layer (`stw tidy plan PATH`), not to the wizard. In the overview a head the proofs do not cover is `retain` `unmerged`, and a `retain` row cannot be chosen. If a row that was `safe` when observed is no longer proven when the retirement plan re-proves the head, the plan retains it with `integration-not-proven` and building the plan set fails with that reason. The plan binds HEAD reference, commit id, branch tip and base, so any change refuses with
`evidence-changed`. A worktree whose HEAD is attached to the base branch itself never gets a branch deletion.

When the owner chose `delete-branch:NAME` for a retired worktree, the **dependent branch leg** runs right after, and only after, that
retirement succeeded: the branch is re-observed and deleted through the unchanged local-reference rules (recovery reference and
journal included). The leg is derived at apply time, not stored; a failed retirement skips it (code `retirement-not-applied`), a branch
that is not the retirement plan's head refuses the whole set (`branch-leg-mismatch`), and a chosen deletion without a leg is skipped
(code `branch-leg-absent`), so the set is `partial`.

### Derived defaults

Every value the wizard assumed is shown in the overview and in `derived[]` (`name`, `value`, `source`, `overridden`).

| Value | Derivation |
| --- | --- |
| Workspace | `--workspace`, else discovery from the working directory |
| Members | the direct members plus the workspace repository `.` (notice `workspace-members-none` when there is no other member); an uninitialized member is named in a `submodule-uninitialized` notice |
| Remote | `origin`, else the single remote; several remotes without `origin` are `remote-ambiguous` |
| Base | the local branch of the remote's symbolic HEAD, else the remote-tracking reference; `--member-base NAME=REF` overrides; none: `base-ambiguous` or `base-missing`; the wizard never fetches |
| Caller label | `$STW_OWNER`, else (human mode only) the operating-system user; `--mine` without one is a usage error |
| Item owner | the task record, then path and branch convention (`/tmp/whr*` is `codex`; the branch prefixes `codex/`, `claude/` and `claude-`), then a commit author hint that never makes an item `own`, then `unknown` |
| State directories | the repeatable `--state-dir`, `$STW_STATE_DIR`, the shared default; the first valid one holds the plan set |

### Reason codes

Every code below is a string of the code base; the file names are the places to check.

**Why an item is kept (`retain`, or the one `decision`).** The reason is the `reason` of the candidate and the evidence line says why
(`internal/tidywizard/classify.go`).

| Applies to | Reasons |
| --- | --- |
| Worktrees, automatic protections | `primary-checkout`, `current-worktree` |
| Worktrees, in the order they are tested | `evidence-incomplete` (an observation failed or was bounded, or no activity time was observed for an item of another or unknown owner), `locked-foreign` (locked by something other than stw), `registration-record-missing`, `registration-record-ambiguous` (an stw lock whose creation record is in no known store, or in several with different content), `dirty`, `ignored-extras` (untracked or ignored files the stored tree does not hold), `unmerged` (commits ahead of the base), `recently-active` (HEAD, index or a tracked file changed within 24 hours, only for an owner other than the caller or unknown), `jj-retirement-unsupported` (a jj workspace owned by a creation record; no plan kind retires one yet) |
| Worktrees, the `decision` row | `locked-stw`: an stw lock whose creation record does not match; choosing it asks a second confirmation naming the lock |
| Worktrees whose directory is missing | `unregistered` (no stw lock), `registration-record-missing`, `registration-record-ambiguous`, `lock-mismatch` (an stw lock whose creation record does not match), `evidence-incomplete` (the Git administrative entry is not provably prunable), `stale-registration-no-plan-kind` (prunable, but no plan kind removes it yet) |
| Branches | `evidence-incomplete`, `unmerged` (not proven integrated), `branch-attached-elsewhere` (checked out in a worktree that is retained or kept), and for jj the keep reason of the native tidy passed on unchanged (for example `remote-counterpart`, `recovery-reference`, `selected-base`, `workspace-used`) |
| Remote-tracking references | `evidence-incomplete`, `unmerged` (not integrated into the base), and the observer's reason for a reference that stays: `remote-source-present`, `symbolic-reference`, `selected-base`, `workspace-used`. Any other reason is replaced by `evidence-incomplete` with the evidence text `tracking-reason-unrecognized` (`adapter.go`). `github-branch-absent` is not a keep reason: it is the proof of a deletable reference, which is `safe` |

A `safe` worktree without an stw lock carries the reason `unregistered`, and the evidence `no-work` when it has no commit of its own.
Integration proofs of a safe row are `ancestor`, `patch-id`, `merge-noop`, `no-net-change`, `empty-working-copy-integrated-parent`
(jj) and `github-branch-absent`.

**Why a chosen item cannot become a sub-plan** (building the plan set fails; nothing is guessed;
`internal/tidywizard/planset/v3planner.go`):
`plan-repository-unknown` (the repository has no path), `plan-base-missing` (no derived base), `plan-shape-invalid` (an empty
sub-plan, an unknown kind, or a retirement that does not cover exactly one worktree), `plan-state-dir-missing` (no plan store),
`branch-name-unmappable` (not a short branch name), `tracking-remote-missing` (no derived remote), `tracking-online-required` (tracking
references are planned only with `--online`), `tracking-ref-unmappable` (not a reference of that remote), `legacy-owner-unknown` (a
legacy completion needs one plain owner label, not an unknown owner or an author hint), `stale-registration-no-plan-kind`, and the reason
of the creation-record lookup (for example `registration-record-missing`). When the existing plan kind itself retains a chosen item,
that plan's own reason is reported (for example `integration-not-proven`; otherwise `retirement-not-eligible`, `no-candidate` or
`retained`). A planning failure carries the diagnostic code of the failed plan. Before any sub-plan is made, choosing a `retain` row, a `decision` row without its second confirmation, an unknown id, or an item that is not plannable in the current mode (for example an `other` or `unknown` item in agent mode) fails with the untyped error `item cannot be planned: ...` (`planset/planset.go:344-364`).

**Why an apply is refused** (status `refused`, nothing changed, exit 1; `internal/tidywizard/planset/apply.go`):
`mode-evidence-invalid` (the mode evidence cannot be recorded, for example an overlong `STW_OWNER`), `plan-set-not-found`,
`plan-set-invalid` (the stored set fails its id verification; sets from before the branch-leg decision are not migrated),
`scope-not-authorized` (an `other` or `unknown` item in an agent-mode apply, or without `authorized_scopes`), `confirmation-refused`
(the typed confirmation failed or there is no terminal), `authorization-required` (no explicit plan id) and `branch-leg-mismatch`.

**What an apply reports per sub-plan.** The status is `applied`, `stale`, `failed` or `skipped`. A `skipped` sub-plan carries the code
`cancelled` (the run was cancelled before it), `retirement-not-applied` (the retirement a branch deletion depends on did not succeed) or
`branch-leg-absent`. A `stale` sub-plan carries `evidence-changed` (or a code containing `stale`); a `failed` one carries the diagnostic
code of the native apply. The set status is `ok` (every sub-plan applied), `failed` (none applied) or `partial`; anything but `ok` is
exit 1.

## Default v1/v2 evidence and retention

Only native ancestry proves integration in the default v1/v2 contracts. Squash equivalence and
forge proof are unsupported there; those invocations never contact GitHub or another forge. A missing upstream, empty commit,
branch-name match or merged PR alone provides no deletion permission.

Checked-out references, workspace-used revisions, the selected integration base, conflicted or
ambiguous bookmark targets and uncertain integration remain retained. Git detached worktrees
also protect references pointing at their HEAD. jj bookmarks and changes are distinct: removing
a bookmark would not imply abandoning a change. Remote-associated jj bookmarks remain retained.

Native Git commit IDs may use SHA1 or SHA256. jj commit, change and operation IDs remain separate.
Native jj observes pinned stored state without snapshotting; disk cleanliness is not established
by stored-unsnapshotted state. Owned task retirement has a separate versioned plan and explicit registration/files selections; see below.

## Stateless and stored plans

Without `--state-dir`, planning writes no plan store and returns an empty `plan_id`.
Native repository references, registrations, files, configuration and jj operations stay unchanged;
native sandbox setup may create private temporary isolation files outside the target.

To save an immutable plan, provide an already existing private mode0700 state directory:

```sh
stw tidy plan /path/to/repository --base refs/heads/main --ref refs/heads/finished-task --state-dir /private/task-state --json
```

The store must be disjoint from repository/workspace metadata and task destinations. Persistence
writes only disclosed private state records. Store access is the trust boundary; a saved plan
is still evidence, not authorization. Apply supports authorized local reference deletion with durable recovery; see below.

## Bounds, output and failures

Offline planning has a sixty-second total deadline (it was a fixed ten seconds that a loaded runner or a repository of a thousand files could exceed; it is deterministic, not scaled by the repository), a 1 MiB encoded record/result limit (8 MiB for retirement plans, which record every file) and
an aggregate observation limit of 1024 entries. Complete base/target tree metadata and
all-path filter observations use at most 4,325,376 stdout bytes and 4,224 bytes per logical
record, retaining the 16,384-leaf, 4,096-byte path, 16 MiB/blob and 64 MiB aggregate blob
bounds. The 16,384-leaf limit is shared with workspace creation; in tidy it only bounds cost, because these
streams are all-or-refuse (final delimiter, exit status and EOF required) and evidence is never truncated.
The limits that decide a deletion keep their values. Other native output and every stderr channel remain bounded to 64 KiB. Paths and selectors
are bounded UTF-8 text of at most 4096 bytes; IDs are at most 128 bytes. Missing native tools,
unsupported versions, unavailable sandboxing, malformed or truncated output, cancellation,
ambiguous/GitButler ownership and changed observations refuse or report incomplete evidence.
There is no unsandboxed retry or backend fallback.

Human output safely quotes opaque text and reports the same evidence as `--json`. Complete
operational results use the independent [tidy schema v1](../schemas/v1/tidy.schema.json).
Exit 0 means complete planning, including retained candidates. Exit 1 means refusal, incomplete
or partial evidence, cancellation or operational/output failure. Exit 2 means invalid CLI usage.
Writer failures may leave incomplete JSON; always check the exit status and parse validity.

Observations are samples, not a transaction. External changes and ABA races cannot all be ruled
out. Apply revalidates each action and records durable recovery before effects; remote
reference deletion, automatic garbage collection, jj abandonment, hooks and network are excluded.

## Authorized local reference cleanup

Create a fresh stored plan, review its candidates and declared recovery/deletion effects, then
explicitly authorize that exact plan:

```sh
stw tidy plan /path/to/repository --base refs/heads/main --ref refs/heads/finished-task --state-dir /private/task-state --json
stw tidy apply --plan GENERATED_PLAN_ID --state-dir /private/task-state --authorize --json
```

The apply command accepts no repository path, base or replacement targets: it loads the immutable
plan from the same private store. Omitting `--authorize` produces an operational refusal, including
structured JSON when requested. A stateless plan cannot be applied. Plans created by the earlier
planning-only WI lack the reviewed mutation effects and refuse with `replan-required`; generate and
review a new plan rather than editing a stored record.

Before each eligible action, apply checks the selected base, native tip, references, workspace
registrations/identities and safety evidence again. A moved tip/base, new workspace or unrelated
reference/operation change refuses stale proof. Git creates an exact private recovery ref before
old-OID compare-and-swap deletion. jj creates an exact native recovery bookmark before deleting the
selected local bookmark, checks its own operation parent and full allowed reference delta, and never
abandons the underlying change. Remote-associated jj bookmarks remain retained. Neither backend
fetches, pushes, runs hooks or garbage collects recovery roots.

Git cleanup and owned retirement reuse the [workspace layout proof](workspaces.md), including
initialized submodule and separate-Git-directory primary or linked sources. Replacing a
layout pointer or common config, even with identical bytes and unchanged commit IDs, stales
stored evidence. Apply rechecks before recovery and the next deletion/removal; an already
created recovery root stays retained after refusal. A linked task never owns the primary
checkout named by shared `core.worktree`. Layout admission grants no superproject, primary
checkout or sibling metadata writes. Fresh CLI/version acceptance remains a separate
[verification gate](../specs/010-isolated-task-workspaces/verification.md#issue-62-source-implementation).

A durable journal is recorded before native effects. Human/JSON output identifies its location,
recovery pins, action outcomes and limitations. Recovery roots remain after success and protect
native committed identities; they do not back up untracked files, restore registrations/files or
promise a general undo command. Review recovery evidence manually before removing any native pin.

Apply has a sixty-second deadline with the same native-output, record and aggregate bounds as plans.
Cancellation or failure after native action start may leave partial effects; results report known
completed/attempted actions and recovery location. An incomplete journal is not permission to retry
blindly. A completed retry succeeds only after its recorded postconditions are verified. Sampled
checks cannot exclude every external or ABA race; jj deletion is not an atomic Git-style CAS.
Reference cleanup excludes workspace registration removal, filesystem removal and jj abandonment;
default owned retirement uses a separately reviewed version 2 plan.

## Owned task retirement

Retirement selects one completed SourceSteward task, its exact owner and an explicit integration
base. Creation plans, completed journals, native registration identity and physical destination
identity must agree. A matching path or task label alone provides no ownership proof.

```sh
# Plan native registration removal; jj preserves files. Git retains this selection as unsupported.
stw tidy plan /path/to/source --base refs/heads/main --state-dir /private/task-state --task task-id --owner owner-id --remove-registration --json

# Separately select the exact owned destination for filesystem removal.
stw tidy plan /path/to/source --base refs/heads/main --state-dir /private/task-state --task task-id --owner owner-id --remove-registration --remove-files /absolute/owned/task --json
stw tidy apply --plan GENERATED_RETIREMENT_PLAN_ID --state-dir /private/task-state --authorize --json
```

Use a native jj literal base/bookmark instead of the Git full reference in these examples. Retirement
flags are mutually exclusive with `--ref`. The private store, task, owner and registration selection
are required; `--remove-files` must equal the recorded canonical owned destination. These selections
are inspectable proposed effects, while `apply --authorize` grants execution of the reviewed plan.

Git cannot forget a present worktree registration while preserving its files. Registration-only
plans retain that unsupported action. With both selections and complete safe evidence, fixed native
Git removal performs the coupled registration and filesystem effects and removes only the exact
SourceSteward-owned lock. Both action outcomes are reported; no dirty-state override is offered.
jj registration removal natively forgets the exact workspace while preserving disk content. A
separate files action revalidates physical identity and the full manifest, removes only verified
entries through confined descriptors, and retains unexpected/new files rather than following links
or recursively deleting an inferred path. Forgetting alone never authorizes filesystem removal.

The current/source workspace, foreign or incomplete ownership, dirty/untracked/ignored-extra files,
conflicts, symlinks, special files, transformed/sparse trees, unsupported metadata and exceeded bounds
retain or refuse retirement. A full bounded stored/disk comparison includes executable modes and
native metadata identities. jj stored-unsnapshotted state alone is never cleanliness proof; an empty
working-copy commit is eligible only with one integrated parent, identical stored trees and a full
clean disk comparison. Known activity or uncertain freshness retains work; labels cannot establish
that every external editor or agent is idle.

Retirement uses [tidy schema v2](../schemas/v2/tidy.schema.json), with `plan: null` and an optional
`retirement` record (present for complete successful observations, potentially absent on early
failure). It binds original creation ownership, expected source/target identities, full physical
manifests, separate actions/effects/blockers and recovery limits. Reference cleanup retains schema
v1 unchanged. Dispatch on `schema_version`, including operational failures.

A private durable retirement journal and native recovery pin precede removal. Before each action,
apply rechecks ownership, integration, registration and physical state. Recovery retains native
committed content/identities and reports the journal location; it does not promise automatic file
restoration or recovery of untracked content. Interrupted or uncertain effects remain partial and
require manual review; no automatic rollback or blind retry is attempted. Completed retries verify
recorded postconditions and remaining files. Native writes and disk removal still have sampled
external/ABA race limits rather than a repository-wide transaction.

Retirement uses the same sixty-second planning deadline and an apply deadline that scales with the plan (60 s + 5 s + 50 ms per recorded entry), 8 MiB encoded record
bound (a retirement plan records every file and directory) and aggregate 1024-entry bound for references; each stored/disk raw file in the default v2 contract
is bounded to 64 KiB; the retirement blob sample and the manifest are 8,192 entries and user content
64 MiB in total, while the shared 16,384-leaf tree limit only bounds cost. Hooks, fsmonitor and submodule
policy stay strict for retirement. Oversized or
incomplete evidence never authorizes a truncated removal target. Remote deletion, garbage collection,
jj change abandonment and arbitrary forced cleanup remain excluded.


## Explicit Git contract 3

Source contract 3 adds Git-only merged-head proof, local tracking-ref cleanup and explicitly
completed legacy retirement. Select it with `--contract 3`; omission preserves the existing
v1/v2 dispatch and zero-network behavior. These new capabilities do not permit Git writes in
jj- or GitButler-owned workspaces. Source/native checks and remaining acceptance limits are
recorded in the Spec 029 integration evidence. The v3 schema is retained in the source tree at
`schemas/v3/tidy.schema.json` (the review report and the decision file have the candidate schemas
`schemas/v3/tidy-report.schema.json` and `schemas/v3/tidy-decisions.schema.json`, the restore and
history results `schemas/v3/tidy-restore.schema.json` and `schemas/v3/tidy-history.schema.json`);
none is part of the published schema inventory.

A local reference plan remains useful offline. Online merged-head evidence requires explicit
`--online --remote NAME` and one admitted GitHub.com/SHA-1 repository:

```sh
stw tidy plan /absolute/source --contract 3 --base refs/heads/main \
  --ref refs/heads/finished-task --online --remote origin \
  --state-dir /private/task-state --json
stw tidy apply --contract 3 --plan PLAN_ID --state-dir /private/task-state \
  --authorize --online --json
```

Optional `--repo OWNER/NAME` must agree with that remote; `--host` accepts only `github.com`.
An optional repeatable `--pr FULL_HEAD_OID=NUMBER` selects exact-head PR candidates. A branch
name only helps discovery. Proof binds stable repository/PR identities, the exact local tip,
the provider's complete merged tuple and its reported integration commit reaching the exact
chosen local base. The original PR base can be an intermediate stack branch. Amended tips,
forks, missing local integration objects, ambiguity or changed provider facts retain/refuse;
no patch equivalence, inferred PR chain or fetch substitutes for proof. Selected bases,
symbolic references and every registered workspace-used identity remain protected.

Online reads use one 30-second plan/60-second apply deadline, at most 128 HTTP requests and
32 MiB raw response bytes including failures/resampling, 1 MiB per raw/normalized body and
16 KiB per auth channel. Collections require proven exhaustion within ten pages/1,000 unique
records. Native processes remain network-denied. Apply requires `--online` exactly when the
stored plan declares network effects, alongside separate `--authorize` for its local effects;
a saved online plan never enables networking by itself.


### Review report bounds

`stw tidy plan` with `--report-dir` writes the review report; a reviewer decision (`--decisions`)
clears a `needs-review` branch only if its report entry is complete, so every bound that can cut
an entry has a flag. Defaults and ceilings (the plan binds them; the three per-entry ones only
when they differ from the default):

| Flag | Default | Ceiling |
| --- | --- | --- |
| `--report-max-entries` | 512 | 4,096 |
| `--report-max-bytes` (per file, Markdown and JSON) | 4 MiB | 16 MiB |
| `--report-max-diff-bytes` (per entry) | 64 KiB | 1 MiB |
| `--report-max-files` (per entry diff and gitlink list) | 16 | 4,096 |
| `--report-max-lines` (per entry diff) | 1,000 | 200,000 |
| `--report-max-commits` (per entry) | 50 | 1,000 |
| `--timeout` | scaled | none |

The bounds interact: the diff stops at the first of the file, line and byte bound, so past
roughly 5 bytes per line the line bound is never reached unless `--report-max-diff-bytes` is raised
as well (1 MiB holds about 200,000 short lines), and the report files must be big enough for the
entries (`--report-max-bytes`). An entry cut by any bound, including a truncated gitlink list, is
"not fully reviewed": its decision is ignored with a note. To review very large branches raise
all of them, for example `--report-max-entries 4096 --report-max-bytes 16777216
--report-max-diff-bytes 1048576 --report-max-files 4096 --report-max-lines 200000
--report-max-commits 1000`.

### Local tracking refs

Tracking selection is a separate mode. The selected remote must have one canonical fetch URL
and exactly one standard optional-plus full-head mapping; duplicate, negative, custom, mirror,
overlapping or rewritten mappings refuse. Select exact local tracking refs, or omit `--ref`
for the complete bounded namespace:

```sh
stw tidy plan /absolute/source --contract 3 --base refs/heads/main \
  --tracking-remote origin --online --ref refs/remotes/origin/finished-task \
  --state-dir /private/task-state --json
```

Two complete remote branch advertisements must agree at plan time. Fresh complete inventories
and native/configuration proof must still match before recovery and final prepared deletion.
A present source branch retains even at a different OID. Symbolic remote HEAD and its resolved
target, the selected base and workspace-used refs retain. Deletion preserves the exact old OID
in recovery and changes only the local tracking ref; there is no fetch, synchronization, prune,
server deletion or config repair. Remote absence is sampled and cannot prevent later recreation.

### One-operation legacy completion

Legacy retirement selects one exact canonical absolute linked worktree and owner label. The
completion flag declares that the task is complete, no person or agent is using it, and the
supplied protection set includes every caller-known active worktree. This is cooperative
one-operation authority, not authentication, activity detection, adoption or historical stw
creation evidence. Keep active worktrees protected explicitly:

```sh
stw tidy plan /absolute/source --contract 3 --base refs/heads/main \
  --state-dir /private/task-state --completed-worktree /absolute/completed-task \
  --owner operator --protect-worktree /absolute/active-task \
  --remove-registration --remove-files /absolute/completed-task --json
stw tidy apply --contract 3 --plan PLAN_ID --state-dir /private/task-state --authorize \
  --completed-worktree /absolute/completed-task --owner operator \
  --protect-worktree /absolute/active-task --json
```

Repeat the exact completion path, owner and protection set at apply. Newly known active work
requires refusal and replanning. Primary/current/source/ancestor, protected, foreign, locked,
dirty, ignored-extra, aliased, overlapping or incompletely observed paths remain blocked.
Native Git removal couples separately selected registration and exact-path file effects;
legacy removal has no force/unlock fallback and keeps its attached branch. Generate a fresh
reference plan after retirement before selecting that branch for cleanup. Online retirement
adds the same explicit remote selection and apply acknowledgment as other v3 online plans.

Apply has a deadline of 60 s plus 5 s per planned effect plus 50 ms per recorded manifest entry,
with no ceiling (the work bounds it, and the caller's context still ends it). Restore uses the
same rule (60 s plus 5 s per restored branch). `--timeout DURATION` on `tidy plan`, `tidy apply`
and `tidy restore` (contract 3) replaces the default deadline of the whole operation; the default
is the scaled formula above, and `0` or a negative value is a usage error. Restore reads the
repository and the journal before its deadline is set (bounded local reads that end only with the
caller's own context); the deadline then covers the evaluation and the branch creations. If the deadline still fires,
the result is `partial` and lists each ref's journaled outcome; unconfirmed deletions are never
replayed automatically. Restore needs the original absolute repository and state-directory paths
(a copy is refused as `journal-unreadable`); refs of an incomplete journal were not confirmed
deleted and restoring them is a no-op.

A plan retires exactly one worktree and needs `--state-dir`; `--report-dir` and the other review
report flags apply to reference plans only. Retirement refuses, with a named code and reason (also
without `--json`): `retirement-file-limit` (more than 8,192 tracked files, or more than 8,192 files and
directories together, or an oversized file; larger trees need a digest-only record, a follow-up), `retirement-unrecorded-entry` (an untracked, ignored or generated path the stored tree does
not contain), `retirement-sibling-refused` (the parent directory holds a symbolic link, device or
socket; its siblings are recorded as plain files and directories) and `git-tree-refused` (the
commit holds symbolic links or submodule gitlinks: native removal would also delete the Git
directory of an initialized submodule, so retirement, unlike creation, does not admit gitlinks).
Batching several worktrees in one plan is not supported.

Owned v3 retirement instead selects `--task ID --owner LABEL` with the original completed
creation records and exact owned-lock rules. It never substitutes a completion declaration
for owned provenance. Both Git-v3 retirement modes stream regular user files up to 16 MiB
and 64 MiB per user-content pass in buffers no larger than 64 KiB. Metadata remains 64 KiB
per file, except the registration `index` (up to 4 MiB, since it grows with the tracked file
count); old v2/jj policy is unchanged. Full content/identity checks run at three plan/eight
apply observations, with one shared byte/process/deadline budget and no reused fresh hashes.
Timestamp and inode checks do not exclude every same-user/ABA race.

### Restoring deleted branches and the journal history

A local-reference apply keeps a recovery ref `refs/stw/recovery/v3/PLAN/HASH` and a journal entry
for every branch it deletes. `stw tidy restore` creates the branch again from them. It is a
read-only plan by default and works on the clone it is pointed at (`PATH`, default `.`):

```sh
stw tidy restore /absolute/clone --contract 3 --plan PLAN_ID --state-dir /private/task-state --json
stw tidy restore /absolute/clone --contract 3 --plan PLAN_ID --state-dir /private/task-state \
  --action ACTION_ID --as refs/heads/finished-task-again
stw tidy restore /absolute/clone --contract 3 --plan PLAN_ID --state-dir /private/task-state \
  --apply --authorize --json
```

Per journal entry that reached a confirmed recovery the plan shows the full branch ref, the tip
OID, the proof class, flags and the reviewer decision (who, kind, why), and one state:

| State | Meaning | Apply |
| --- | --- | --- |
| `restorable` | the recovery ref is a direct ref at the journal OID, the commit is readable and the branch name is free | creates the branch |
| `already-present` | the branch exists at the journal OID | nothing |
| `name-taken` | the branch exists at another OID, or is a symbolic ref (reason `symbolic-reference`) | nothing; `--as` restores one action under another name |
| `recovery-missing` | the recovery ref is absent (`recovery-ref-absent`), symbolic (`recovery-ref-symbolic`), points elsewhere (`recovery-ref-retargeted`), or its commit is gone (`object-unreadable`); a re-clone has no recovery refs | nothing; the reflog is never searched |
| `journal-incomplete` | the entry has no confirmed recovery, or its branch name is not a valid `refs/heads/` name (`invalid-ref-name`) | nothing; an incomplete journal is never replayed |

Journal text (branch names, reasons, reviewer names) is data: it is validated, escaped on output and
never passed to Git as an option. Only journals of plan mode `local-refs` are restorable; a
tracking-ref or retirement journal is refused as `unsupported-mode`. An unknown plan or action is a
refusal (exit 1); argument errors (a duplicate `--action`, `--as` without exactly one `--action` or
not a `refs/heads/` name, `--authorize` without `--apply` and the reverse, `--list` with `--plan`, a
malformed plan or `--limit`) exit 2 before any store or native work. The plan status is `ok` whenever
the journal was read, even if nothing is restorable.

`--apply --authorize` is the same explicit authorization as `tidy apply`; a plan or a reviewer
decision is never authorization. There is no stored restore plan: the effects are computed at apply
time and every entry is evaluated again immediately before its creation. A branch is created with
`git update-ref --no-deref` against the zero object ID, so an existing branch is never
overwritten or moved and a symbolic ref is never followed; a branch created in between is reported
as `name-taken` and a symbolic one as `symbolic-reference`. The new branch gets one reflog entry
`stw restore PLAN ACTION`. Apply keeps the recovery refs and touches no worktree, remote,
configuration, index, other ref or file; a repository whose hooks would run refuses first. Before each
creation an `intent` record is durable in the `tidy-v3-restores` namespace of the private state
store and an `outcome` record (`created`, `created-recovered`, `name-taken`, `symbolic-reference`,
`recovery-missing` or `error`) follows; records are appended, never rewritten, and the tidy journal is
never modified. A rerun is idempotent. After a crash between intent and outcome the rerun finds the
branch at the journal OID and records `created-recovered`, or finds it absent and creates it; it never
overwrites. A branch that someone else created at the same OID after the crash is reported
`created-recovered` too. The apply status is `ok` when every in-scope entry is `created`,
`created-recovered` or `already-present`, `partial` when some were created and others were not,
`refused` when nothing was created and an entry was not already present, and `error` for an
unexpected failure. `git branch NAME OID` stays a valid manual restore.

Only the tip commit and its history are restored, in this clone. Reflog tips (recorded as evidence
only), untracked files and worktree registrations are not.

A journal written by this version also records, before each branch deletion, the `branch.NAME.*`
configuration of exactly that branch (at most 16 keys, 256 bytes per value, a truncation marker) and the
reflog tip or `unavailable`; an unreadable reflog never stops a deletion. Journals written earlier
lack the record and restore exactly as before. The plan then adds per entry a `branch_config` with
the key names, the remote name of `remote` and `pushremote` (never another value) and one state per
key: `reapply`, `skip-remote-missing` (the remote is gone from this clone, or a recorded remote is not reapplied, which also leaves `merge` and `rebase` alone), `skip-not-recorded` (a key
other than `remote`, `merge`, `rebase` and `pushRemote`, which are never reapplied, or a key written without a value) or another `skip-*`
reason (`skip-already-set`, `skip-renamed` under `--as`, `skip-truncated`, `skip-multi-valued`,
`skip-not-restorable`). `--apply --authorize` writes only the `reapply` keys with `git config --local`
after the branch exists and reports each key `applied`, `skipped` or `failed`; a failed key never
removes the branch, adds the diagnostic `restore-config-failed` and makes the run `partial`. A rerun
writes nothing.

`stw tidy history` and `stw tidy restore --list` (same reader, read-only, `--state-dir` and
`--limit`, default 256, at most 1,024) list the stored journals newest first by journal time: plan
ID, time, mode, numbers deleted, retained and restorable, whether the recovery refs are `present`,
`partial` or `absent`, the number of `human` and `ai` decisions and the journal status `complete`,
`incomplete` or `unreadable` (an unreadable journal is listed with its ID, never skipped). Journals of
other modes show their mode and `restorable` 0. The store scan stops after 4,096 records, the
commit checks are one batched `git cat-file --batch-check` of at most 65,536 lines and 4 MiB, and the
whole call has a 30-second budget. A bound that is reached marks the affected journals `incomplete`
and adds a truncation marker `{unit, shown, total}` (`journals`, `objects` or `checked-journals`);
`total` is a lower bound when `scan_truncated` is true.

V3 human and JSON results report completeness, sampled freshness, actual network attempts,
measured counters, precise candidates/actions, recovery and effects. Complete negative plans
exit 0; unavailable, changed, refused, partial or failed observations exit 1; malformed usage
exits 2. Incomplete evidence has no executable plan. Interrupted effects retain recovery and
report known outcomes; only a completed journal can retry after read-only postcondition
verification. Recovery preserves selected committed history, not untracked files, original
index/reflog/configuration, automatic worktree restoration or a general undo command.

### Owner-size acceptance tests

The normal test run covers worktree retirement, one-plan branch deletion and restore with small
fixtures (3 worktrees with 6 branches, and 20 branches). The owner-size runs (60 worktrees with 120
branches, and 120 branches, or `STW_SCALE_BRANCHES=200`) take many minutes and are not part of CI.
Run them by hand before a release:

```sh
STW_ACCEPTANCE_SCALE=1 go test ./internal/workspace -run 'TestTidyV3ScaleSixty|TestTidyV3ManyBranches' -timeout 60m -v
```
