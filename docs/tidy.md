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

Offline planning has a ten-second total deadline, a 1 MiB encoded record/result limit and
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

Retirement uses the same ten-second planning/sixty-second apply deadlines, 1 MiB encoded record
bound and aggregate 1024-entry bound; each stored/disk raw file in the default v2 contract
is bounded to 64 KiB; the retirement blob sample is 1,024 files, the manifest 1,024 entries and user content
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

A plan retires exactly one worktree and needs `--state-dir`; `--report-dir` and the other review
report flags apply to reference plans only. Retirement refuses, with a named code and reason (also
without `--json`): `retirement-file-limit` (more than 1,024 files and directories, or an oversized
file), `retirement-unrecorded-entry` (an untracked, ignored or generated path the stored tree does
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
