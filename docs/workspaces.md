# Isolated task workspaces

SourceSteward creates an isolated native Git worktree or jj workspace from an explicit
base. Git and jj are Tier 1 peers; ownership selects the backend, including native jj
for colocated repositories. GitButler and ambiguous ownership refuse execution.
No automatic initialization, migration, fetch, push, setup script or task launcher occurs.

## Review, then authorize

Create an existing private mode0700 state directory outside the source, destination and
repository metadata. Its access permissions are the local trust boundary; owner labels
are context, not multi-user authentication. Use absolute destination and state paths.

```sh
stw workspace plan /path/to/source --base HEAD \
  --destination /path/to/task --task issue-42 --owner developer \
  --state-dir /path/to/private-state --json
stw workspace apply --plan PLAN_ID --state-dir /path/to/private-state --authorize --json
stw workspace handoff /path/to/task --state-dir /path/to/private-state --json
stw workspace retire-plan /path/to/task --state-dir /path/to/private-state --json
```

Use a supported literal native base selector; jj revset programs are not accepted.
Read the returned plan, native base identities, effects and recovery limits before apply.
The generated plan ID identifies a stored immutable record. A plan alone never grants
permission. Apply requires explicit `--authorize` and revalidates relevant source/base,
registrations, destination and ownership. Changed or uncertain evidence refuses.

Git creates a detached worktree and no branch. Ordinary checkouts, initialized submodule
checkouts and separate-Git-directory checkouts, including their linked worktrees, require
canonical native identities and matching `.git`, `commondir` and registration backlinks.
A shared `core.worktree` must occur once directly in the common repository config and map
back to the independently proven primary checkout; that primary can differ from a linked
source. Configuration is never rewritten to admit a layout. Missing or aliased mapping
metadata refuses with `git-layout-refused`; duplicate or external `core.worktree` origins
refuse with `git-worktree-config-refused`. Other unsafe settings identify the matched rule
without exposing its value. Executable default hooks, active filters, unsupported sparse state
and external helpers retain their refusals. `core.hooksPath` and `submodule.*` configuration no
longer refuse creation: `stw` runs the checkout with hooks, fsmonitor and submodule recursion
disabled on the command line, so it never runs a repository hook, and a managed worktree runs none
of the project's hooks (run them by hand or through `make`). Submodules are never initialized.

Literal layout/config bytes and physical identities are part of stale-plan evidence,
including during completed creation verification. Replan historical records whose safety
fingerprints no longer match. See [layout verification](../specs/010-isolated-task-workspaces/verification.md#issue-62-source-implementation)
for measured source support and pending fresh CLI/version acceptance.
jj creates a named native workspace and working-copy change; native operation metadata changes.
Its isolated configuration and write sandbox deny network and undeclared writes. Source dirty
and untracked work remains in the source workspace without snapshotting it or changing branches.
Missing supported tools or sandbox capability refuse; raw Git never substitutes for jj writes.
On macOS, place a supported real Git executable on `PATH`: the Apple `/usr/bin/git`
developer-tool shim may require undeclared helper execution and cannot satisfy the creation
sandbox. This requirement concerns native creation; historical read-only checks do not prove
that the executable supports the workspace write boundary.

## Recovery and retention

The store records recovery evidence before native creation and journals completed effects.
Interrupted or failed creation may leave a registration, partial destination or metadata change.
Keep the returned journal pointer and inspect declared effects; cancellation does not promise
rollback. There is no automatic cleanup, destination adoption or lock stealing.
A retry reports already-complete only after checking durable ownership and native identity.

Handoff returns task ownership, native identities, current inventory qualifications and remaining
work without launching an agent or transferring ownership. Retirement preparation retains the
workspace and delegates deletion to planned Spec 011. Native jj stored state does not establish
fresh disk cleanliness; unknown state is retention evidence, never deletion permission.

## Bounds and output

Observation commands have a ten-second deadline; apply has sixty seconds. Native command output
is bounded at64KiB, encoded state/results at1MiB and aggregate observations at1,024. Git checkout
preflight additionally limits tracked leaf entries to16,384, each file to16MiB and total stored file bytes to64MiB. Symlink trees, checkout path aliases and unsafe metadata are refused. Gitlinks are accepted as data: the checkout never recurses, so a gitlink becomes an empty directory and no submodule is initialized. jj's time bound
can interrupt materialization with partial effects; this is not a disk-size quota. Native jj source working-copy metadata is limited to16 files, with each sampled metadata/index/config file limited to64KiB; unsupported or oversized evidence refuses creation. Paths/base are
at most4,096 bytes; task/owner labels use ASCII letters/digits plus `.`, `_`, `-`, start with a
letter/digit and are at most128bytes. Sampling cannot detect every external race or ABA change.

Human and `--json` output derive from one result. Exit0 means complete success, exit1 means
refusal/partial/operational failure and exit2 means syntax errors. Structured operational errors
use the independent [workspace schema v1](../schemas/v1/workspace.schema.json), preserving Git
commit IDs separately from jj commit/change/operation identities. Existing inspect, inventory
and version contracts remain unchanged. See [JSON contracts](json-contracts.md) and the
[normative workspace contract](../specs/010-isolated-task-workspaces/contracts/workspace.md).

Unused Git filter definitions (including a system-wide Git LFS installation) are admitted
only after bounded native attribute checks for every path in the pinned base. Committed,
info and default-global attributes remain effective; source dirty/untracked attributes
never substitute for the selected base. Active filters refuse; configured driver names
`set`, `unset` and `unspecified` also refuse because they collide with attribute sentinels.
No filter helper is executed or silently disabled. Configuration and attribute evidence
are included in stale-plan validation.

## Execute reviewed owned retirement

`workspace retire-plan` remains a non-destructive handoff/preparation command. Actual retirement
uses the separate [tidy retirement workflow](tidy.md): exact completed task/owner evidence, explicit
integration base, registration selection and optional exact filesystem target, followed by authorized
apply. Native Git coupled effects and jj separate forget/files effects retain their distinct semantics.
