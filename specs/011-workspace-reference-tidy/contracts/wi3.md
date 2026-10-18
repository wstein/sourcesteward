# WI3 Retirement Contract and Interface Freeze

Public selection: `stw tidy plan [PATH] --base REF --state-dir ABS --task ID --owner OWNER
--remove-registration [--remove-files ABS] [--json]`; --ref and retirement flags mutually exclusive.
Exact files path must equal completed owned task destination. Registration-only jj may complete
with files retained; Git present-tree registration-only retains unsupported. Both authorizations are
required for native Git combined remove. Plan output lists each distinct effect, proof and blocker.

Publish a separate tidy retirement schema v2; preserve tidy v1 bytes/fixtures. Backend identities,
base/operation/safety, task/native creation ownership, canonical destination inode, full manifest,
separate registration/files action selections, recovery root and action outcomes must be explicit.
Concrete Go structures below were confirmed by coordinator/native/core authors and independently
analyzed at 84455ef. Source execution remains gated on WI2 PR completion and root dispatch. Do not reinterpret old task records
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

## Exact shared model

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
    Mode uint32 // full raw mode for stale physical state checks
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
recovery reference or later remote deletion intent is allowed. The reviewed private execution context authorizes NO new write grant to the actual source
working_copy, including lock files. Any future additional lock-file grant needs separate concrete
design analysis and source review; it is not authorized by this contract. Initial observation admits
only the supported completed metadata set.

This exact model has coordinator/native/core feasibility confirmation and independent WI3
design clearance. Independent experiments establish observed filename
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
helpers must freeze before source implementation. Core and jj confirmations were subsequently obtained as recorded below.

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

## Frozen shared manifest helper interface
```go
type RetirementStoredFile struct {
    Path, Digest string
    Size int64
    Executable bool
}
type retirementManifestAdmission struct {
    Backend string
    NativeMetadataPaths []string
    StoredFiles []RetirementStoredFile
}
func observeRetirementManifest(context.Context, string, retirementManifestAdmission) (RetirementManifest, error)
func removeRecordedManifest(context.Context, PathIdentity, PathIdentity, RetirementManifest) (TidyNativeOutcome, error)
```
Removal arguments are exact root and parent identities (root.Path is target), then immutable expected
manifest. No redundant unbound path. NativeMetadataPaths are EXACT relative file/directory paths,
never directory-prefix exemptions. Entire physical tree is walked/fingerprinted; admitted metadata
is excluded only from stored user-content equivalence, not physical checks/deletion manifest. Unknown
empty directories retain unless an admitted parent of a known path. StoredFiles comparison includes
exact binary content digest/size/executable and excludes no unknown/untracked/ignored entries.
Confined filesystem helper never fills Outcome.Expected; native/core wrapper independently validates
source native poststate and fills it only with exact permitted delta proof. All helper errors after
removal start preserve Attempted/Effects/incomplete outcomes; root/parent/current manifest checked
immediately before removal, no-follow file identities/content checked per entry, directories removed
only when empty. Native lookup supplies fixed literal paths and raw contents before Go traversal.

Dependency boundary: internal/inspection cannot import internal/workspace. Native jj inspection bridge
returns bounded stored-file facts and exact metadata admission facts in its own native types;
workspace jj adapter converts those facts and invokes the shared workspace manifest walker. Do not
introduce an import cycle or duplicate disk-removal implementation. Core author confirmed this helper
shape; independent source review still must verify descriptor confinement and replacement failure paths.

Native jj author confirms feasibility for both pinned versions: TreeEntry templates expose literal
path/type/executable/conflict, raw bounded file-show with root-file literal selectors preserves
binary bytes, and full sparse layout is exactly native `.\n`; other sparse layouts retain. Existing
64KiB native output/file bound remains unchanged (larger content retains, no silent content runner
bound increase). Empty-WC proof requires exactly one parent, parent native ancestry into base,
empty stored parent/WC delta, no conflicts, full stored manifest and actual disk equality. Existing
reviewed private execution context is reusable for native recovery/forget, subject to new forget
conformance/effect tests. Core and Git author model/helper feasibility confirmations are recorded
above; independent final WI3 analyze and source/acceptance gates remain mandatory.

Physical RetirementEntry.Mode records full raw mode for stale physical identity checks. Native
stored-file facts preserve executable semantics only; recovery from committed trees does not promise
restoring every original filesystem permission/ownership bit. Reject NFC/casefold-aliased physical
paths under the existing Spec010 ordinary-tree admission rules; do not silently normalize or merge
separate names. Permission changes between plan/apply/removal are stale, never cleanup proof.

## Issue #62: layout proof for owned retirement

Apply the [shared Git layout contract](../../010-isolated-task-workspaces/contracts/workspace.md#git-checkout-layout-admission-issue-62)
to both source observation and the exact owned target before recovery/removal. A linked
target may inherit a primary `core.worktree`; prove both mappings without requiring path
equality or treating the primary as owned by this task. Preserve the immutable ownership and
physical metadata manifest, exact lock/removal conditions and complete protected-workspace
set. Primary/configuration and unrelated source/sibling metadata remain outside write grants.
After authorized combined removal, require only the expected target/registration absence;
all remaining layout evidence must still agree. Do not recreate missing metadata for proof,
adopt primary/submodule paths, or weaken complete retry/recovery checks. This design amendment
has no implementation or independent acceptance claim until the new tasks pass.


## Git-v3 retirement policy amendment

Default v2/jj user-file limits, original owned journals/locks and retry meanings above remain.
Explicit Git contract 3 uses the Spec 029 stream/content contract: regular user files at most
16 MiB, 64 MiB per complete user pass, at most 64 KiB buffers and unchanged 64 KiB metadata
files. The selected version/backend policy reaches stored objects, disk manifests, decoded
records and deletion-adjacent proof; no ambient larger constant changes old callers.
Three plan/eight apply observations retain nine/24 full user-content passes, at most three/eight
batched raw-object processes and their one-operation byte/deadline ledgers. No old hash replaces
fresh evidence. Physical/native pointers, target/registration parents and protected sibling sets
remain immutable pre-effect facts. Registration/file outcomes remain distinct even for coupled
native Git removal. Legacy completion has independent exact-path authority, no forced removal
or lock release, and preserves its attached branch for a later fresh reference plan.
