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

## Evidence and retention

Only native ancestry proves integration in this WI. Squash equivalence and forge proof are
unsupported; the CLI never contacts GitHub or another forge. A missing upstream, empty commit,
branch-name match or merged PR alone provides no deletion permission.

Checked-out references, workspace-used revisions, the selected integration base, conflicted or
ambiguous bookmark targets and uncertain integration remain retained. Git detached worktrees
also protect references pointing at their HEAD. jj bookmarks and changes are distinct: removing
a bookmark would not imply abandoning a change. Remote-associated jj bookmarks remain retained.

Native Git commit IDs may use SHA1 or SHA256. jj commit, change and operation IDs remain separate.
Native jj observes pinned stored state without snapshotting; disk cleanliness is not established
by stored-unsnapshotted state. Workspace retirement and filesystem removal remain later work.

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

Planning has a ten-second total deadline, native output limited to 64 KiB, a 1 MiB encoded
record/result limit and an aggregate observation limit of 1024 entries. Paths and selectors
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
out. Later apply must revalidate each action and record durable recovery before effects; remote
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

A durable journal is recorded before native effects. Human/JSON output identifies its location,
recovery pins, action outcomes and limitations. Recovery roots remain after success and protect
native committed identities; they do not back up untracked files, restore registrations/files or
promise a general undo command. Review recovery evidence manually before removing any native pin.

Apply has a sixty-second deadline with the same native-output, record and aggregate bounds as plans.
Cancellation or failure after native action start may leave partial effects; results report known
completed/attempted actions and recovery location. An incomplete journal is not permission to retry
blindly. A completed retry succeeds only after its recorded postconditions are verified. Sampled
checks cannot exclude every external or ABA race; jj deletion is not an atomic Git-style CAS.
Workspace registration removal, recursive filesystem removal and jj abandonment are excluded.
