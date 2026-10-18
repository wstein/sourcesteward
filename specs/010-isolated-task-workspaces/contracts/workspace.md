# Workspace CLI and Local Mutation Contract v1

## Commands
```text
stw workspace plan [PATH] --base REV --destination ABS --task ID --owner OWNER --state-dir ABS [--json]
stw workspace apply --plan PLAN_ID --state-dir ABS --authorize [--json]
stw workspace handoff [PATH] --state-dir ABS [--json]
stw workspace retire-plan [PATH] --state-dir ABS [--json]
```
PATH defaults to `.`. Creation requires explicit base, destination, task and owner. `--authorize`
is required only for apply; planning persists disclosed plan data, not native repository changes.
No command accepts arbitrary native arguments, revset programs, setup scripts, a remote or
stdin plan. Apply loads the immutable plan by generated ID from the specified secure store.
Unknown flags, extra operands and irrelevant subcommand flags are syntax errors.
Exit0 means complete success (including verified already-complete retry); exit1 means refusal,
unknown/partial state or operational error; exit2 means CLI syntax/validation error. Structured
operational failures use the same versioned envelope as success; normal Cobra help is text.

## Authorization and effects
A stored plan is reviewable evidence, never permission. `--authorize` states caller authorization
for listed local effects; repository policy and filesystem permissions still apply. Owner is a
caller-supplied label; store access is the local trust boundary, not multi-user authentication.
Plan creates only secured store records. Apply creates one native registration and destination,
native backend metadata and journal/task records. Git creation uses a detached worktree at an
explicit commit; it creates no branch. jj creates a uniquely named workspace and native
working-copy change based on the explicit commit; it changes operation metadata. Git full checkout is locked with reason `stw:<task-id>` and creates no named branch. Native Git
may remove its newly reserved destination on failure; this is disclosed and never extended to
foreign paths. Source disk,
index/working-copy state, source HEAD/bookmarks, config and untracked files remain unchanged.
No fetch, push, scripts, stash, source reset, hook suppression, model launch or deletion is permitted.
Native Git worktree-add may internally reset only its new owned destination to materialize the
planned checkout; this is a declared backend effect, never a source checkout reset.

Git safe preflight rejects all executable hooks, configured executable checkout filters and unsafe external attributes sources,
submodule activation and unsupported sparse/promisor/external-helper paths before native action;
no successful operation may silently bypass an installed hook. Missing safety evidence refuses.
Git writes also require a network-denying sandbox limited to the owned destination and new
worktree registration metadata. Native jj uses `git.colocate=false`, fixed explicit identity,
full sparse patterns and pinned-operation workspace-add with source snapshot suppression. Native
workspace update-stale is allowed only in the new owned destination to materialize its checkout;
registration and materialization are distinct journal effects. Native jj writes use isolated
config, no raw Git fallback and a sandbox
allowing only declared backend store/destination writes with network denied. Source workspace
metadata is kept read-only where backend permits; unavoidable repository-store changes are listed.
An unavailable sandbox/config-isolation capability refuses rather than retries unsandboxed.

## Preconditions and bounded execution
Revalidate store integrity, caller ownership, source manager, supported executable version,
repository/native identities, resolved base/reference, jj operation, registrations and destination
parent/no-alias state immediately before native action. Refuse changed relevant evidence. Record
pre-action recovery evidence durably first. Serialize store writers; native commands retain own
locks. Recheck outcome and source state after execution. External races/ABA changes are not fully
prevented; disclose sampling limits and incomplete effects instead of claiming transactionality.
Paths/base ≤4,096 bytes; identifiers ≤128 bytes; reads10seconds; apply60seconds; each native output
≤64KiB; each encoded store/result record ≤1MiB; registrations/aggregate observations≤1,024.
Cancellation is propagated; after action start cancellation cannot imply rollback. Git preflight bounds the checkout to 1,024 tree entries and 64 MiB total stored blob size; reject
unsupported tree objects/paths, symlink escape risks and gitlinks before action. No automatic
cleanup or adoption. Destination registration after failure remains evidence for manual recovery.

## Public Go boundary
`internal/workspace` owns operation filesystem/process effects; CLI parses/renders only.
Conceptual entry points: `PlanCreate(context.Context, PlanOptions) Result`,
`Apply(context.Context, ApplyOptions) Result`, `Handoff(context.Context, Options) Result`,
`RetirePlan(context.Context, Options) Result`.
PlanOptions holds Path, Base, Destination, Task, Owner, StateDir; ApplyOptions holds StateDir,
PlanID, Authorize; Options holds Path, StateDir. Backend adapters expose fixed Observe/Create/
Verify operations using common Snapshot/MutationPlan/NativeWorkspace types; adapters must not
persist store ownership. Common coordinator owns locking/journaling/retry/revalidation. Reuse
reviewed `inspection.Inspect`/`Inventory` for detection/handoff; missing capabilities are qualified,
not guessed. Exact Go field naming can follow repository style without changing this CLI contract.

## Schemas and compatibility
Publish new `schemas/v1/workspace.schema.json` with all success/refusal/error/action states.
Validate real CLI fixtures, malformed inputs and old schemas; never weaken prior contracts.
Schema URLs stay stable under the current Pages schema allowlist. Human output includes plan ID,
backend, destination, base, owner, actions, qualifications and recovery pointer when available.
JSON fields preserve Git and jj native identities distinctly; a jj change ID is never a Git OID.

## Sandbox limitation
Linux bubblewrap enforces declared filesystem write grants and network denial, but does not enforce
an executable allowlist. Fixed native commands, admitted/fingerprinted config, pinned trusted
executables and cooperative caller-owned repositories prevent declared helper paths. External
adversarial concurrent replacement/ABA is not prevented; never claim an adversarial transaction.
Git write grants protect every preexisting linked-worktree metadata directory read-only; only new
registration/destination writes are allowed. Git checkout blob limit is16MiB/file,64MiB total,1024leaf
entries,64KiB tree metadata. Gitlinks and unsafe paths are unsupported; missing proof refuses.

Native jj checkout size is bounded by its60second action deadline and partial-materialization
reporting, not a precomputed Git tree budget; store/result/output bounds still apply. Large jj
checkout may leave owned partial files/registration, which must be reported and retained, never
automatically retried into user-edited files or claimed rolled back.

Unused Git filter definitions (including a system-wide Git LFS installation) are admitted
only after bounded native attribute checks for every path in the pinned base. Committed,
info and default-global attributes remain effective; source dirty/untracked attributes
never substitute for the selected base. Active filters refuse; configured driver names
`set`, `unset` and `unspecified` also refuse because they collide with attribute sentinels.
No filter helper is executed or silently disabled. Configuration and attribute evidence
are included in stale-plan validation.

## Git checkout layout admission (issue #62)

This amendment is a design candidate pending independent analysis and implementation. It
replaces blanket refusal of `core.worktree` only for the proven layouts below. All existing
helper, tree, ownership, bounded execution, sandbox and recovery requirements still apply.
It creates no new command, public schema field, adoption path or configuration-writing effect.

### Supported mappings

| Source layout | Required mapping evidence |
| --- | --- |
| Ordinary primary checkout | Canonical checkout with its own `.git` directory; native Git directory/common directory/top-level agree. |
| Primary initialized submodule or separate-Git-directory checkout | Canonical checkout `.git` is a bounded regular pointer file to the primary Git directory; that directory is also the common directory and has no linked `commondir` redirection. Native identity agrees. Any admitted `core.worktree` resolves back to this exact primary checkout. |
| Linked worktree of either layout | Checkout `.git` points to its exact registration under the common directory's `worktrees/`; registration `commondir` resolves to that common directory and `gitdir` resolves back to this checkout's `.git` file. Native identity and registration evidence agree. A present shared `core.worktree` is validated against the separately proven primary checkout, not against this linked checkout. |

A separate Git directory need not be inside the checkout. Its location, or a `modules/`
component alone, establishes no ownership. An initialized submodule used as the selected
repository is distinct from activating nested submodules or admitting gitlinks in the
checkout base; those existing unsupported cases stay unsupported. Existing no-`core.worktree`
layouts keep their admitted behavior and qualifications; an unavailable primary mapping
must never be invented to satisfy a new proof requirement.

### Proof and trust boundary

1. Keep current workspace root, current Git directory, common directory and any separately
   proven primary checkout distinct. Resolve native identity through the existing bounded,
   isolated read path; cross-check it with reciprocal filesystem metadata before permitting
   an effect. Do not trust `core.worktree`, a native listing or a path basename by itself.
2. Admit a present `core.worktree` only as one nonempty value originating directly in the
   common directory's regular repository `config`. Refuse duplicate occurrences, included,
   global, system, command or worktree-specific origins for this key, even if values agree.
   Resolve a relative value from the primary/common Git directory; ordinary generated `..`
   components are allowed only when the resulting canonical mapping is proven. Absolute
   values require the same proof. Preserve the literal configuration without rewriting it.
3. When `core.worktree` is present, prove the primary checkout independently: its `.git`
   directory is the common directory, or its regular `.git` pointer resolves directly to
   that directory, with no `commondir` indirection. For a linked source with this key, prove
   both this primary mapping and the linked reciprocal pointers. An unrelated checkout, missing target, mismatched backlink or unprovable mapping
   refuses; it is never silently substituted as the source, recovered or traversed for content.
4. Use bounded no-follow metadata reads and canonical directory identities. Refuse symlink,
   special-file, hardlinked metadata-file, duplicate/aliased path, ownership or identity
   ambiguity. The existing 4,096-byte path, 64 KiB native output and aggregate observation
   limits apply; layout pointer/configuration files are at most 64 KiB each. Evaluate pointer
   syntax as data; execute no text from it. Limit extra traversal to the exact directories
   and metadata files needed for the reciprocal proof, not sibling repository contents.
5. Bind literal layout pointer/configuration bytes, their physical identities and the proven
   directory mapping into the existing safety fingerprint, in addition to effective config,
   attributes, hooks and existing native evidence. Re-observe at the existing pre-action and
   post-action boundaries. An observed replacement, mapping or configuration change is stale
   even if it still reaches the same commit. Include the new proof consistently in fresh
   plans and validation; historical plans with incompatible fingerprints refuse and require
   replanning, never rewritten records or an equality bypass. Sampled external/ABA limits remain.
6. Reuse the same admission for creation, local reference observation/recovery/deletion and
   owned retirement source/target validation. A present primary `core.worktree` shared by a
   linked target does not transfer primary ownership to that target. Exact permitted native
   deltas remain action-specific: creation may add its owned registration, reference cleanup
   may add its recovery root/delete its selected ref, and retirement may remove only its owned
   registration/files. None permits changes to configuration, primary checkout, source index,
   unrelated refs or sibling metadata. Existing narrow write grants must protect the common
   config and every preexisting sibling; do not grant write access to the superproject,
   common-directory parent or primary checkout to make a new layout work.

Executable hooks, active checkout filters, unsafe helper/attribute sources, sparse/promisor
state, alternates, worktree-specific configuration and nested-submodule activation retain
existing refusals. Unused filter definitions retain their already specified attribute-based
admission. No raw Git fallback for jj, source snapshots, jj change abandonment or remote
reference deletion is introduced. Missing enforced confinement or complete evidence refuses.
The mandatory acceptance matrix is in [T031–T038](../tasks.md#issue-62-native-git-layout-readiness).
