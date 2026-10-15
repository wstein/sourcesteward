# Data Model: Isolated Task Workspaces

## Shared operation model
`Result` has schema_version=1, command (`workspace-plan`, `workspace-apply`,
`workspace-handoff`, `workspace-retire-plan`), status (`ok`, `refused`, `partial`, `error`),
backend (`git`, `jj`, `unknown`, `ambiguous`, `gitbutler`), nullable plan/task, action outcomes,
diagnostics and limitations. Arrays are present even when empty. JSON and human rendering
use the same model. Existing inspection/inventory envelopes remain unchanged.

## Mutation plan
Immutable `MutationPlan`: schema_version, plan_id, task_id, owner, source_path, destination,
state_dir, backend, reason, expected native snapshot, effects, recovery limits and plan digest.
`plan_id` is cryptographically random, not caller-controlled file text. `task_id`/owner are
nonempty ASCII `[A-Za-z0-9][A-Za-z0-9._-]*`, at most 128 bytes; owner is a label, not proof of
human identity or authorization. Base/path input is valid UTF-8, at most 4,096 bytes.
Resolved base is an unambiguous full commit identity, with jj change identity when applicable.
Source snapshot includes repository/common-store identity, native workspace identity, source
HEAD or jj commit/change/operation, relevant reference resolution and registration fingerprint.
Destination binds canonical absolute path, absent final component and existing parent identity;
no symlink components, alias, metadata/source/store overlap, parent disappearance or ownership
ambiguity is accepted. Plan records expiry only if implemented; absence of expiry never exempts
stale validation. Store is not portable between machines or independently copied repositories.

## Durable records
Private state root exists, is symlink-free, current-user owned, mode0700, outside source,
repository metadata and destination (neither ancestor nor descendant where writes overlap).
`plans/<plan-id>.json`, `operations/<plan-id>.json`, `tasks/<task-id>.json` use mode0600.
Bounded parsing rejects unknown fields, duplicate fields, invalid identifiers, oversized input
and incompatible schema. Derived paths are only generated identifiers; no path from JSON is
trusted without canonical validation. Store files are untrusted evidence requiring revalidation.
Dir-FD anchored os.Root/no-follow access and inode identity checks protect against path swaps.
Exclusive create + file fsync + directory fsync makes the planned/prepared journal durable before
native mutation. A single exclusive store lock serializes cooperating invocations; the OS advisory lock releases on process exit; busy locks refuse and are never stolen by PID/time. No automatic deletion/recovery.

`TaskRecord` includes task/owner/plan identities, backend, destination, repository/store identity,
native workspace identity, base identity, recovery pointer and completion observations. Native
Git identity includes common dir, per-worktree git dir and HEAD; jj includes repository dir,
workspace name, change, commit and operation. Path name/registration alone cannot confer ownership.

## Journal state machine
planned → prepared (durable before mutation) → native-created → complete.
Any native failure, cancellation or persistence failure after prepared becomes incomplete with
known/unknown effects retained. Never claim no mutation merely because subprocess failed.
Retry first reads journal: complete + matching owned native identity returns already-complete;
prepared/incomplete with no proven completed effects remains incomplete; matching created identity
may finalize only when a durable native-created journal records exact identity proving this operation created it. Foreign/ambiguous or
missing ownership proof refuses. Never delete destination, registration, branch or bookmark.
All completed actions survive a later error in the result and durable journal.

## Handoff and retirement
Handoff returns revalidated task/native identities, current inventory qualifications, owner label,
remaining work and recovery limits, without changing owner or launching an agent. Retirement
preparation returns retention evidence and an explicit delegation to Spec011; never permission
to delete. Unknown dirty/stale state always retains the workspace. jj stored state does not prove
fresh disk cleanliness. Recovery evidence records what can be inspected and what remains manual;
no claim to undo user edits, hook/network effects or arbitrary jj operations.
