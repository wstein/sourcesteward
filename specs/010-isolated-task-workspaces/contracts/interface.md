# Implementation Interface Freeze

This private Go interface coordinates parallel authors; [workspace contract](workspace.md)
controls the public CLI. Changes require coordinator agreement and synchronized adapter/tests.

```go
package workspace

type PlanOptions struct { Path, Base, Destination, Task, Owner, StateDir string }
type ApplyOptions struct { StateDir, PlanID string; Authorize bool }
type Options struct { Path, StateDir string }
func PlanCreate(context.Context, PlanOptions) Result
func Apply(context.Context, ApplyOptions) Result
func Handoff(context.Context, Options) Result
func RetirePlan(context.Context, Options) Result

type NativeIdentity struct {
    Backend, RepositoryDir, WorkspaceRoot, WorkspaceName, GitDir, HeadRef string
    CommitID, ChangeID, OperationID, ObjectFormat string
}
type Snapshot struct {
    Identity NativeIdentity
    BaseSelector, BaseCommit, BaseChange string
    ReferenceDigest, RegistrationDigest, SafetyDigest string
}
type PathIdentity struct { Path string; Device, Inode uint64 }
type MutationPlan struct {
    SchemaVersion int
    PlanID, TaskID, Owner, SourcePath, Destination, StateDir, BaseInput string
    Expected Snapshot
    DestinationParent PathIdentity
    DestinationIdentity *PathIdentity
    Digest string
    Effects, Recovery []string
}
type NativeWorkspace struct {
    Identity NativeIdentity
    Complete, SourceUnchanged bool
    Phase string // reserved, registered, materialized, unknown
    Effects, Limitations []string
}
type nativeAdapter interface {
    Observe(context.Context, string, string) (Snapshot, error)
    Create(context.Context, MutationPlan) (NativeWorkspace, error)
    Verify(context.Context, MutationPlan) (NativeWorkspace, error)
}
// Implementations gitAdapter{}, jjAdapter{} selected by inspection.Inspect.
```

Core owns exported Result/TaskRecord/action result types, state files and operation orchestration;
CLI/schema author synchronizes its exact JSON shape with core before implementation. Fields use
lowercase snake_case; arrays are initialized nonnil; optional fields serialize null. Results include
schema_version, command, status, backend, plan, task, actions, diagnostics, limitations and handoff
observations. Result statuses are ok/refused/partial/error. Task record binds plan/owner/backend/
destination to stable native registration and creation identity; its immutable plan retains base
and initial observations. Progress may legitimately change HEAD/change/op later: handoff checks
stable repository/workspace ownership without mistaking normal task progress for foreign adoption.

Git RepositoryDir is canonical common dir; GitDir is canonical per-worktree git dir;
WorkspaceRoot is canonical checkout. jj RepositoryDir is canonical repo dir; WorkspaceRoot is
canonical workspace root. Empty Git change/op fields mean absence, not fabricated identities.
Snapshots hash complete bounded safety/native precondition observations. Digest is deterministic
SHA256 of canonical struct serialization excluding Digest, with no map iteration or unknown fields;
digests detect tampering but are not cryptographic caller authentication.
Adapters return known Phase/Effects alongside errors after native start. Verify validates expected
registration/destination/repository identity, preserves dirty/unknown qualification and never writes.
Core persists prepared journal before destination reservation and Create. Only a durable
native-created journal with exact observed identity may finalize a retry; prepared/path matching
alone never authorizes adoption. Native failure may remove its own reserved destination; preserve
this declared partial effect, never extend cleanup to foreign paths.

Private state uses dir-FD anchored os.Root/no-follow IO, canonical inode checks, regular nlink1
records, same-user permissions and a nonblocking OS advisory lock; kernel releases on process exit.
Busy refuses; never steal/reclaim locks by PID/time. Missing platform lock support refuses. File and
directory fsync precede native writes; record/task collisions refuse rather than overwrite foreign
state. Check bounded exact fields and duplicate keys before decode; state files remain untrusted.
