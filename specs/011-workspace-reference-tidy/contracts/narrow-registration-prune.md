# Narrow missing-directory registration prune (T012b)

Status: proposed future contract under the owner-approved scope of 2026-10-25.
Implementation and conformance are pending. This amendment does not enable cleanup;
the current wizard retains stale registrations. Only the frozen-schema/interface decision
below requires owner resolution before source work; the absence predicate is defined here. Broad Git worktree prune stays excluded.

## Selection, ownership and effects

One stored sub-plan MUST name exactly one Git administrative worktree registration.
The selector MUST identify its exact native registration name and canonical gitdir path,
not a wildcard, directory scan, branch name, inferred basename or implicit selection.
More than one or no name refuses. A plan set MAY contain separately selected sub-plans,
but each carries its own evidence, recovery and outcome; no batched native prune runs.

The registration MUST have the exact raw lock reason `stw:<task-id>` derived from the
matching original completed
Spec 010 creation plan, task and journal from one unambiguous private store. Bind those
records by digest, original owner/task/repository/destination and native registration
identity. A lock that merely resembles stw's format, or a copied/mismatched record,
is insufficient. No legacy completion declaration, adoption or lock override applies.
The original lock stays present through the final validation; no preliminary unlock.

Refuse unnamed, multiple, unlocked, foreign-locked, unknown/ambiguous ownership,
main/primary or current-worktree registrations, a present destination, changed mapping,
symlink, hardlink, special file, unsafe/aliased path or unexpected metadata. Refuse
ambiguous repository/backend ownership, unreadable/truncated evidence and unsupported
native/platform capabilities. A stale row has no human decision override for these checks.

The only destructive effect is removal of that selected administrative registration.
No user-directory traversal/removal, branch deletion, checkout, index reset, existing-reference
rewrite, remote effect, hook, helper, broad prune or garbage collection is authorized.
A later branch cleanup requires an independent fresh reference plan; no dependent branch
leg is inferred from a missing checkout. Git-only capability does not authorize raw Git
inside jj-managed or colocated jj workspaces, or GitButler-managed state. Native jj
workspace forgetting is outside this amendment and must report its capability gap.

## Defined missing-directory predicate and residual risk

"Verifiably missing" means verified absence at the exact registered pathname under
stable accessible filesystem evidence, not proof that the user's files were deleted.
This contract defines the predicate; it is not another owner decision or a requirement
for an unavailable authoritative deletion witness. All of the following MUST hold at
plan time and freshly at each pre-effect validation boundary:

1. Starting from a trusted root descriptor, every ancestor through the immediate
   parent exists as a real directory under no-follow opens. Bind each ancestor's
   device/inode/type/owner/mode and the open descriptor identities. A missing ancestor,
   symlink, or unreadable/unsearchable parent refuses; absence of a mount or parent
   cannot be treated as absence of the selected directory.
2. A descriptor-relative `lstat` of the selected final path component returns
   precisely ENOENT. A present entry of any type refuses, including a dangling
   symlink. EACCES, EPERM, ENOTDIR, EIO, ESTALE, cancellation and all other failures
   are refusals, never missing evidence. The registered `<destination>/.git` backlink
   is absent; no live backlink is removed or followed.
3. The immediate parent's device equals the admitted repository common directory's
   device. Bind filesystem/mount identity and availability for the common directory
   and ancestor chain, including mount points. Recheck stable mount identity and
   descriptor/path correspondence before effects. Changed devices, mount replacement,
   unavailable/stale mounts or incomplete observation refuse. Cross-device destinations
   are unsupported in this capability. Each supported Linux/macOS adapter MUST prove
   its mount/identity observation in native fixtures; lack of that capability refuses,
   without requiring a platform deletion event or a new owner answer.
4. Exact registration pointers/lock/original creation records remain bound, and the
   complete private recovery payload and history pin can be durably preserved before
   removal. Git's native `prunable` marker is neither required nor deletion proof:
   locked missing registrations do not normally carry it.

A rename/move elsewhere on the same available filesystem can satisfy this predicate.
An unobserved mount transition or same-user ABA race can evade sampled identity checks.
No directory scan or age/acknowledgment heuristic claims to distinguish these cases.
The bounded effect removes administrative metadata only; it never deletes the moved
checkout. A move can temporarily detach that checkout, which the private raw payload,
history pin and explicit restore/repair procedure below make recoverable within the
stated limits. Disclose this residual risk in the plan before authorization. No store
lock excludes arbitrary external filesystem writers.

## Immutable plan and fresh validation

The future plan MUST bind source/backend/common repository identity and admitted layout,
source/current/primary protection identities, exact administrative registration name/path,
registration parent and complete protected sibling set, recorded destination and ancestors,
defined absence/mount evidence, exact raw lock reason, raw HEAD representation and resolved full
HEAD OID, index presence/absence and digest, and the complete admitted registration manifest.
Bind original creation record digests, owner/task, native executable/configuration safety,
selected effect, recovery location/digest/limits and policy. All evidence contributes to
plan integrity and the plan-set content-addressed identity. Never reinterpret old plans.

Planning reads the missing target's registration, not a fabricated target checkout.
Review the existing v3 all-registration and selected-retirement prunable guards explicitly:
any exception must be confined to this selected new operation, never a generic acceptance
of prunable/locked registrations or a weakening of existing reference/retirement guards.
Require an allowlisted bounded manifest of every native registration entry, including
optional absent entries. Reuse the reviewed retirement admission rules only where they
are valid without a live checkout; unknown files, transient locks or unsupported state
refuse. Define exact platform/native-version admission before implementation review.

Apply requires the stored plan and explicit authorization. Under the existing shared
private-store lock, revalidate all bound facts before recovery preparation, after recovery
confirmation and immediately before removal; verify all original records again. Native
recovery-root addition may advance only its declared exact delta. No apply-first baseline,
refreshed manifest, reconstructed lock or automatically repaired pointer is trusted.

Use descriptor-confined, no-follow removal of only the selected administrative entry and
its immutable admitted children, with per-entry identity/content checks and protected
parent/sibling grants. No destination or destination-parent write grant is permitted.
A native remove primitive that could delete a reappearing checkout is insufficient even if an earlier absence check
passed; demonstrate the metadata-only confinement or refuse the capability.
Remove directories only when empty. A replacement or unexpected child stops further deletion and preserves partial evidence. Feasibility of this narrow
filesystem effect, sandbox grants and Git-version compatibility must be demonstrated in
disposable native fixtures before implementation acceptance. Never substitute broad
`git worktree prune`, forced worktree removal or an unlock-and-prune sequence.

Git has no registration old-value transaction equivalent to direct-reference old-OID CAS.
A stw lock/store lock and descriptor confinement constrain cooperating writers and path
replacement; they do not provide atomic comparison of an entire registration against all
external writers or detect every ABA event. Document residual same-user race limits;
unsupported confinement or an unprovable transition refuses rather than claiming CAS.

## Durable private recovery before removal

Before the first destructive effect, durably write and verify a complete bounded private
payload of the admitted registration gitdir: relative entry names/types, raw file bytes,
original mode/identity facts and digests, raw gitdir/commondir pointers, lock reason and HEAD,
index bytes or explicit absence, and admitted reflog/config metadata when present. Preserve
binary bytes; never convert index or raw metadata through UTF-8 text. Unknown or omitted
entries cannot be called complete recovery. Admit at most 64 MiB per regular metadata
file, 256 MiB total raw payload and 1,024 manifest entries (files plus directories).
Index and reflog bytes are mandatory, not regenerable substitutes or truncated histories.
Use digest-bound private binary sidecars for raw contents, streamed and hashed in at most
64 KiB buffers; the manifest binds exact byte lengths and SHA-256 digests. Sidecars obey
the same per-file/total quotas and durable private-store rules. Insufficient storage,
overflow, short/extra bytes or cancellation refuses before removal; never truncate or
silently omit entries. The enclosing schema/record representation still needs the frozen
contract decision, without relaxing these byte-preservation requirements.

The bounds derive from a read-only 2026-10-25 measurement of all 201 regular registration
directories below this live repository's native common-dir/worktrees. The walk used
`lstat`, did not follow symlinks and read sizes only, not file contents. Sizes are bytes:

| Class | Files | Minimum | Median | 95th percentile | Maximum |
| --- | --- | --- | --- | --- | --- |
| index | 201 | 75,628 | 133,435 | 165,018 | 169,564 |
| logs/* | 201 | 108 | 1,675 | 14,348 | 64,931 |
| other regular metadata | 1,167 | 6 | 41 | 527 | 2,630 |

Median total regular-file payload per registration was 140,404 bytes; maximum 214,958.
The earlier 64 KiB per-file bound rejected every measured index. The chosen 64 MiB
per-file ceiling is over 390 times the largest measured index, and 256 MiB total is
over 1,200 times the largest measured registration, permitting substantial growth
without an unbounded recovery store. This snapshot is sizing evidence, not an admission
exception: fresh measured content and all quotas still govern each future operation.

Payload directories MUST be private 0700 and files 0600, outside every protected checkout,
with no symlinks, hardlinks or special files. Use exclusive creation, durable file and parent
sync and content verification before recording recovery-confirmed. Failure or cancellation
before confirmation prevents removal. Metadata may contain sensitive paths, configuration,
reflogs or staged content; ordinary human/JSON output and logs expose only bounded escaped
identifiers, digests and private recovery pointers, never raw payload or lock/config bytes.

Reuse Spec 029's secure-store, intent/outcome journal discipline and native recovery-root
mechanism where semantically valid. Pin and confirm the resolved HEAD commit/history before
removal, with absent-name creation and exact OID verification. A commit recovery ref does
not preserve raw index/reflog/gitdir contents: the private payload is additionally mandatory.
Current branch restore is not a registration restore facility. Do not add raw metadata to
ordinary result documents or silently relax existing strict journal readers.

Journal the immutable plan/payload digest and intended registration effect before it starts.
After removal, verify exact selected-registration absence, unchanged destination absence,
recovery availability and preservation of source/primary/sibling/other-ref facts. Record
observed effects and completion durably. A crash, lost response or failed postcondition is
partial/unknown, never rolled back or automatically retried. Completed replay is a verified
no-op only after checking the exact recorded postconditions and recovery evidence.

## Explicit restoration and refusal

The future recovery path MUST first inspect the plan, private payload and prune journal
read-only and report whether that exact registration can be restored. Restoration requires
separate explicit authorization and its own durable intent/outcome record, never automatic
rollback or reuse of prune authorization. Its CLI spelling and machine shape remain pending
owner schema/interface decision; no existing command is claimed to implement it.

Before writing, verify repository/common-dir and parent identities, all payload digests and
bounds, intact HEAD pin, exact original registration name unoccupied, and destination still
absent with the approved witness/ancestor/mount evidence. Refuse a reused name/path, moved
or present destination, changed lock/ownership, foreign repository, missing/corrupt recovery,
or ambiguous prior partial restore. Never overwrite, adopt or repair such state.

Stage the exact payload in private confined storage, preserve raw bytes including lock,
HEAD and index, and publish only at the absent original registration entry with no-replace
semantics and fresh parent/sibling validation. Verify complete restored metadata and native
registration identity; preserve recovery evidence on success or partial failure. The checkout
remains missing and stw-locked; restoring registration does not recreate user files, replay
hooks, restore file ownership/inodes/timestamps, or recover objects absent from both the
repository and pinned history. No byte-identical user checkout or universal native recovery
is promised. If safe no-replace publication/native verification is unavailable, refuse and
retain the payload for separately reviewed manual recovery; never emit a guessed transcript.

## Structured contracts: Needs owner decision

No frozen schema, enum, version, persisted record or sample output changes in this PR.
The implementation must inventory and review these impacts before choosing a representation:

| Surface | Required impact assessment |
| --- | --- |
| Frozen `schemas/v2/tidy-overview.schema.json` | Existing stale-worktree kind, safe verdict and remove-registration effect may represent the row; prove this against the frozen schema and validators. No invented integration-proof word; safe must carry registration/absence proof honestly. New public witness/reason fields would require a decision. |
| Frozen `schemas/v3/tidy.schema.json`, strict plans and apply journals | A distinct narrow action needs registration/absence binding, payload reference and recovery outcomes. Present-checkout retirement and branch candidates cannot be relabelled to bypass their required evidence. |
| Plan-set manifest, hashing, ordering and apply dispatch | Distinct sub-plan kind and one-registration grouping need strict validation, identity binding and explicit ordering. Reject unknown/old incompatible plans; no silent migration. |
| Restore/history documents and journal readers | Expose bounded recovery location/status and partial evidence without raw payload; branch-only restore cannot claim this capability. |

Options: (A) owner-approved extension of the unpublished frozen contracts with regenerated
freeze evidence and an explicit stored-plan compatibility policy; (B) a separately versioned
contract/operation and explicit dispatch, preserving existing frozen bytes; (C) retain the
current capability until that choice is made. Recommend B for a distinct recovery-bearing
operation, with C as the implementation gate meanwhile. The owner must choose the exact
version, enum spellings, expert selector and restore interface; none is reserved here.
Published contracts remain unchanged under every option. Schema/interface resolution precedes implementation; accepting this design does not
choose a frozen representation. The absence predicate above is already defined.

## Outcomes and acceptance

Use the unified exit classes: 0 for complete read-only planning (including retained cases)
or confirmed complete apply/verified replay; 1 for refusal, incomplete/unsupported evidence,
stale state, cancellation, operational/output failure or partial/unknown effect; 2 for invalid
usage. Human and structured outcomes must agree and expose recovery availability and known
partial effects. Missing evidence is never success. These are future acceptance requirements.

Acceptance MUST include Git 2.29 or later and current Git on Linux/macOS, successful
absence-predicate fixtures and unsupported-observation refusal, moved and unmounted
lookalikes, all ownership/selection refusals,
metadata corruption/overflow/binary bytes, private-storage failure, concurrent changes at every
boundary, interrupted removal/restore and completed replay. Compare all protected state and
sentinels, including jj/colocated rejection with no raw Git mutation. New docs examples must
come from real fixture output once implementation exists.
