# Data Model: Tidy v1

## Result
TidyResult: schema_version=1; command tidy-plan/tidy-apply; status ok/refused/partial/error;
backend git/jj/unknown/ambiguous/gitbutler; nullable plan; actions, diagnostics, limitations arrays.
Human and JSON output use this one typed result. Complete retained candidates may be successful
observations; missing/truncated global evidence is partial and cannot authorize actions.

## Immutable plan
TidyPlanRecord binds random plan_id (empty for stateless), digest, canonical source_path/state_dir, native backend/identity,
explicit base selector and resolved commit/change, expected reference/registration/safety/operation
fingerprints, selected candidates, effects and recovery_limits. Selectors/paths valid UTF8≤4096bytes,
ASCII identifiers `[A-Za-z0-9][A-Za-z0-9._-]*`≤128bytes. Arrays nonnil, absent optional values null.
WI1 candidate has Kind branch/bookmark, exact literal Name, full native CommitID/ChangeID,
Decision delete/retain, nonempty bounded Reason, Proof none/native-ancestry and nonnil
WorkspacePaths/Limitations arrays. Native ancestry may be proven while candidate remains retained.
SelectedRefs binds exact caller selection; unknown/duplicate explicit selectors refuse.
Base-selected refs and any matching linked workspace commit/change targets remain protected.
TidyObservation holds Expected Snapshot, Candidates and Limitations; all native before/after
observations must agree. WI1 exact Go fields are in contracts/interface.md.
WI3 separate action IDs/kinds, workspace ownership and recovery requirements are later additions
requiring its own plan/tasks/analyze before implementation, not frozen WI1 published fields.

## Workspace identity
Retirement binds completed Spec010 creation plan/journal/task record, exact task+owner label,
repository/native registration, canonical destination and device/inode identities, current source,
selected base, clean disk/stored tree manifest and configuration. Owner label is not authentication.
Foreign, active/current, replaced, stale, dirty/untracked/ignored-extra/unknown targets retain.
Separate registration and exact filesystem targets require separate flags and separate outcomes.

## Durable state and recovery
Private tidy-plans/<plan-id>.json and tidy-operations/<plan-id>.json use the reviewed secure store
lock/no-follow/strict-JSON/bounds/fsync behavior. Lazily initialize namespaces only for tidy; do not
reinterpret Spec010 plan files or add fields to its strict immutable records. Journal records plan,
phase, native recovery root/identity, expected preconditions, per-action completed/refused/incomplete
outcomes and remaining work. Native recovery pin protects history from GC; journal explains manual
restoration and limits. Recovery never promises restored untracked files or automatic operation undo.
State: planned → prepared durable journal → recovery-root-recorded → per-action completed → complete;
any post-prepared error/cancel/persistence failure is incomplete/partial with known effects retained.
No blind rerun/adoption after interruption; completed retry must verify exact postconditions, otherwise
retain and require inspection. Failed recovery persistence means no destructive action.
