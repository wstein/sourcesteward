# WI3 Retirement Contract and Pending Interface Gate

Public selection: `stw tidy plan [PATH] --base REF --state-dir ABS --task ID --owner OWNER
--remove-registration [--remove-files ABS] [--json]`; --ref and retirement flags mutually exclusive.
Exact files path must equal completed owned task destination. Registration-only jj may complete
with files retained; Git present-tree registration-only retains unsupported. Both authorizations are
required for native Git combined remove. Plan output lists each distinct effect, proof and blocker.

Publish a separate tidy retirement schema v2; preserve tidy v1 bytes/fixtures. Backend identities,
base/operation/safety, task/native creation ownership, canonical destination inode, full manifest,
separate registration/files action selections, recovery root and action outcomes must be explicit.
Concrete Go structures below are proposed for coordinator/native-author freeze before independent
WI3 analyze; this document remains a draft design gate, NOT source authorization. Do not reinterpret old task records
or weaken their strict decoders; new retirement journals refer to immutable creation ownership.

Before registration action: stale/owner/integration/disk proof, prepared durable journal, durable
native recovery pin and exact recovery record. Before files action: revalidate owned physical path
and complete disk manifest again; forget success alone never authorizes deletion. An interrupted
operation preserves remaining files and reports known metadata effects. Recovering deleted files
requires native stored tree recovery; no promise of untracked file restoration (these blocked apply).
No automatic rollback, broad prune/GC, abandon, remote intent changes or blind retry.

Git sandbox grants protect every existing sibling regular file/directory under writable parents;
symlink/special entries or list overflow refuse. jj deletion is descriptor-confined and only removes
verified entries, never recursive path-derived broad removal. Same-user external races/ABA cannot
be excluded, are sampled and disclosed, and uncertain effects remain partial/manual recovery.

## Proposed exact shared model for freeze

```go
// Existing API signatures unchanged; PlanTidy dispatches workspace selectors, ApplyTidy
// securely locates plan ID in disjoint namespaces. Both namespaces claiming same ID refuse.
type TidyResult struct {
    // existing v1 fields unchanged
    Retirement *RetirementPlanRecord `json:"retirement,omitempty"`
}
// Retirement envelope SchemaVersion=2, Plan=nil, Retirement nonnil when available;
// v1 Retirement=nil and omitted. Operational early failures preserve selected result version.
type RetirementAction struct {
    Name, Target, Decision, Reason, Proof string
    Effects, Blockers []string
}
type RetirementEntry struct {
    Path, Kind, Digest string // relative literal path; regular/directory; SHA256 content
    Device, Inode uint64
    Size int64
    Executable bool
}
type RetirementManifest struct {
    Entries []RetirementEntry
    Digest string
    Complete bool
}
type RetirementSibling struct {
    Identity PathIdentity
    Kind string // regular/directory; symlink/special unsupported
}
type RetirementObservation struct {
    Expected Snapshot // source/base/ref/registration/safety/operation
    Target NativeIdentity
    Destination, DestinationParent, RegistrationParent PathIdentity
    Manifest RetirementManifest // all disk files, including admitted manager metadata
    Metadata RetirementManifest // exact admitted native registration/working-copy metadata
    Siblings []RetirementSibling // existing siblings under both writable native parents
    SiblingDigest, LockReason string // exact admitted owned native lock reason
    WorkspacePaths []string // complete native registrations for state confinement
    WorkspacePathsComplete bool
    Proof string // native-ancestry or empty-working-copy-integrated-parent or none
    Actions []RetirementAction
    Limitations []string
}
type RetirementPlanRecord struct {
    SchemaVersion int // 2
    PlanID, SourcePath, StateDir, Backend, Digest string
    CreationPlan MutationPlan
    CreationJournalDigest string // canonical SHA256 of strict validated completed creation journal
    Task TaskRecord // immutable completed creation ownership, Inventory=nil
    Expected RetirementObservation
    RemoveRegistration bool
    RemoveFiles string // exact owned canonical path or empty
    Effects, RecoveryLimits []string
}
type RetirementJournal struct {
    SchemaVersion int // 2
    PlanID, PlanDigest, Phase string
    ExpectedSnapshot Snapshot // mutable only after full permitted native delta validation
    PlannedRecovery TidyRecovery
    Recovery *TidyRecovery
    Actions []ActionOutcome
    Diagnostics []Diagnostic
}
type retirementAdapter interface {
    Observe(context.Context, string, string, TaskRecord, MutationPlan) (RetirementObservation, error)
    PrepareRecovery(context.Context, RetirementPlanRecord) (TidyRecovery, TidyNativeOutcome, error)
    RemoveRegistration(context.Context, RetirementPlanRecord, TidyRecovery, Snapshot) (TidyNativeOutcome, error)
    RemoveFiles(context.Context, RetirementPlanRecord, TidyRecovery, Snapshot) (TidyNativeOutcome, error)
}
```

Core implements shared bounded manifest comparison/removal and adapter dispatch, strict validators,
state locking/journals and result rendering model. Git RemoveRegistration performs native coupled
registration+filesystem removal only when both selectors authorized; reports both effects. Git owned lock reason must exactly match stw:<task-id>; native coupled lock removal and
any force needed for that exact owned lock are explicit effects, never an override for dirty/unknown files.
Prefer fixed native worktree remove --force --force -- DEST without a separate unlock phase when
the pinned native capability permits; no unnecessary pre-removal unlock mutation. Git
RemoveFiles verifies exact already-absent owned destination and reports no duplicate mutation;
jj RemoveRegistration forgets only exact native registration and RemoveFiles delegates to shared
confined manifest removal after source/native post-forget validation. Native helpers never persist
store files. Existing TaskRecord is not rewritten; retirement state belongs to the new journal.

Store uses tidy-retirement-plans and tidy-retirement-operations through existing secure openStoreKinds;
reads original plans/tasks/operations for completed ownership validation. Serialized full original plan,
task and their digests bind completion journal independently; no path/name-only ownership.
All complete observations/arrays are bounded and deterministic sorted, decoded strictly; manifest
relative paths reject absolute/parent traversal/NUL, duplicate entries and unsupported nodes.
Digest canonical serialization includes every expected identity/entry/action excluding its own Digest.
Aggregate1024 entries includes manifest/siblings/workspaces/actions/diagnostics/limits; records1MiB;
raw file64KiB; read10seconds/apply60seconds. Overbounds means retain, not truncated deletion targets.

Phases: prepared durable predeclared recovery → confirmed recovery durable → registration-complete
→ files-complete → complete. Git native combined effect can produce registration+files completion
in one call but both outcomes are recorded; any nonzero/cancel after start retains possible effects.
Journal records Expected only after full permitted native delta plus fresh independent observation
matches, with target physical manifest preserved separately after jj forget. Incomplete retry refuses
blind repetition; completed retry verifies registration absence/recovery existence and exact allowed
remaining files state. No restoring/recreating absent target as validation side effect.

Current-workspace protection uses both canonical explicit source workspace and the process working
directory resolved in the operation layer; an explicit alternate PATH must not permit deleting the
caller's actual current workspace. Protected source/target/root overlap refuses. External live editor/
agent activity is not reliably discoverable from names or labels; stale disk/native sampling and
cooperative ownership checks remain the declared limits, never a claim to prove another process idle.
Any known active/foreign/unknown ownership or freshness evidence retains.

## Native metadata admission
Stored user-tree comparison excludes manager metadata only for tree-equivalence purposes; the
physical deletion manifest NEVER excludes it. Git .git must be the exact owned bounded regular
pointer file, fingerprinted by bytes/type/device/inode. Its target registration directory outside
the checkout is separately enumerated against the fixed supported native metadata allowlist;
unknown payloads retain before native worktree remove. Independent disposable minimum/current
Git experiments observed registration regular files HEAD, ORIG_HEAD, commondir, gitdir, index,
locked and logs/HEAD, with logs directory and empty refs directory. Admit exactly this bounded
supported set (native version/operation optional absent files validated explicitly); conflict/merge
metadata or unexpected/nonempty refs retain. jj .jj admits only native repo pointer and
version-supported working_copy metadata filenames checkout, tree_state and type, each fingerprinted
including contents/identity. These files were observed in independent disposable native 0.45.1 and
0.46.0 workspace-add experiments; actual Spec010 two-phase fixtures must confirm the same admission.
Transient locks not present at completed observation are not silently added to the allowlist.
Source-created workspace config markers are admitted only when specifically recognized and bounded;
unknown extra files/directories, symlinks/special files or oversized metadata retain. Shared repo
paths are observed for ownership, never traversed into the target deletion manifest. Native authors
must freeze exact minimum/current filename allowlists before WI3 analyze; not a blanket metadata
exemption. Registration and physical metadata manifests remain bound in the immutable plan.

## jj recovery and lock grants
Native recovery bookmark names use the generated stw-recovery- identity plus a fixed trailing `?`,
so they are valid jj names but cannot be auto-exported as Git refs in colocated repositories. Fixed
literal selector encoding is required on every native lookup/delete. Native git.colocate=false alone
is insufficient to suppress auto-export for an existing colocated workspace; no exported @git
recovery reference or later remote deletion intent is allowed. Native working_copy lock files may
require narrowly declared write permission during jj native actions; their exact paths must be
admitted as action-time effects and verified afterward, never broad metadata-directory grants
covering unknown payload. Initial observation admits only the supported completed metadata set.

This exact model is the coordinator proposal; native/core authors must confirm implementation
feasibility before final WI3 analyze clearance. Independent experiments establish observed filename
sets only, not full safe retirement or accepted native conformance. Mandatory original Spec010
creation/materialization fixtures, binary/metadata payload cases, sandbox and partial-effect tests
remain implementation acceptance gates.

## Exact ownership and native post-action delta
At planning, securely reread the original Spec010 plan, task and completed creation journal through
legacy state namespaces under the shared lock. Validate original plan/store/id/digest, strict journal
phase complete and native creation identity, task filename/task/plan/owner/repository/destination
agreement, original destination dev/inode and native sameOwner relation. Bind the canonical strict
completed journal digest as CreationJournalDigest. Before EACH effect and any retry, reread all three
records and require exact original plan/task/journal digest equality plus current native ownership;
normal task progress may change Target current commit/op but never stable registration identity.
Copied records belonging to another state path/repository/registration or modified completed journals
refuse. Journal phase complete proves creation completion, never external process inactivity.

PrepareRecovery returns TidyNativeOutcome.Expected only after validating the exact predeclared native
recovery-root addition and native sole-parent operation advance (jj), with unchanged base resolution,
workspace registrations, target current identity/tree, original physical Manifest/Metadata and every
unrelated reference/safety observation. Core freshly observes and requires exact Expected snapshot
equality before fsync-confirming recovery or permitting registration removal. No Expected means no
next destructive action. Registration removal permits ONLY selected registration disappearance,
exact admitted owned native metadata/lock changes, source native operation advance (jj), and, for
authorized Git combined removal, exact destination-tree disappearance. Unrelated registrations,
refs/base/source files/sibling paths/safety config remain equal. An adapter returns verified Expected
only after this full delta; core independently resamples source snapshot and compares it exactly.

After jj forget, target native identity lookup is expected unavailable; never replace original physical
manifest with a new unowned observation. Core uses the immutable pre-forget ownership/dev/inode and
physical Manifest/Metadata, verifies current source repository snapshot/recovery root/absent exact
registration, then rechecks complete physical bytes/type/inodes against that original manifest before
files removal. Snapshot field is internal postaction native state; original manifests remain immutable
and separately bound throughout all phases. After Git combined removal, validate exact target path
and registration absence instead of attempting to inspect or recreate them. All mismatches preserve
known completed effects as partial/refused; no arbitrary refreshed baseline is trusted.

Recovery roots preserve the selected target commit/tree, not every historical per-worktree reflog
entry or arbitrary original index/configuration metadata. Native Git worktree removal deletes its
admitted per-worktree reflog/index/lock metadata; manual recovery can recreate stored committed
content within declared native limits, not promise byte-identical restored registration/reflog or
preservation of all otherwise-unreachable historical commits. Plan recovery_limits must state this.

WI3 native jj recovery/forget reuses the independently reviewed WI2 private execution context for
colocated source safety. No actual source Git HEAD/index write grant and no broad repo-parent grant
is added for convenience. Exact context/copy/source-preservation/native operation-delta tests must
cover forget as well as recovery; no hidden workspace initialization or registration.

Git native author confirmed feasibility of the proposed model, requiring complete admitted
registration metadata manifest, stored/index/raw disk proof and parent/sibling snapshots. Binary
stored blobs use bounded raw cat-file output, not a UTF8-enforcing metadata reader. Shared manifest
helpers must freeze before source implementation. jj/core final WI3 confirmation is still pending.

Git raw stored-content reads use fixed bounded cat-file through the reviewed sandbox runner, preserving
arbitrary binary bytes. Recheck complete source/index/native ownership and exact target registration
metadata manifest after recovery-root creation and immediately before forced owned removal; matching
lock reason alone never replaces current complete clean disk/index/stored proof.

Core author confirms conditional feasibility with exact journal binding: RetirementJournal.PlanDigest
must equal immutable RetirementPlanRecord.Digest. Original full retirement observation including
Target/Manifest/Metadata remains only in immutable plan.Expected; mutable ExpectedSnapshot is a
separate journal Snapshot, updated only after exact permitted native deltas plus fresh source sample.
Never rewrite original manifests after forget. Initial retirementAdapter.Observe requires target native
registration; after forget, core uses reviewed native source Observe plus exact ExpectedSnapshot,
explicit selected registration absence and immutable physical manifest/legacy creation record checks.
It must not require vanished current target registration as proof, nor adopt unrelated native state.
RegistrationParent is canonical nonzero for Git; jj uses zero PathIdentity when no registration
filesystem root is removed, with strict backend-specific validator rather than invented paths.
