# Narrow missing-directory registration prune (T012b)

Status: proposed future contract under the owner-approved scope of 2026-10-25.
Implementation and conformance are pending. This amendment does not enable cleanup;
the current wizard retains stale registrations. The schema and absence-witness decisions
below require owner resolution before source work. Broad Git worktree prune stays excluded.

## Selection, ownership and effects

One stored sub-plan MUST name exactly one Git administrative worktree registration.
The selector MUST identify its exact native registration name and canonical gitdir path,
not a wildcard, directory scan, branch name, inferred basename or implicit selection.
More than one or no name refuses. A plan set MAY contain separately selected sub-plans,
but each carries its own evidence, recovery and outcome; no batched native prune runs.

The registration MUST have the exact stw-created lock and matching original completed
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

## Missing versus moved or unmounted: Needs owner decision

ENOENT at the recorded destination, even with Git's prunable indication, proves only
absence at that pathname. It cannot prove that the tree was deleted rather than moved,
or that a mount holding it is available. A matching stw lock proves ownership, not deletion.
A namespace scan, age threshold or owner acknowledgment does not close that proof gap.

A locked missing registration need not carry Git's native prunable indication. That
indication MUST NOT be the new ownership/deletion proof or a required substitute for it.
The future observer/classifier must carry the approved evidence independently.

Planning MUST bind the recorded destination's complete existing ancestor chain with
no-follow identity/type/device/inode/owner/mode evidence, the nearest existing parent,
and the platform mount/filesystem identity and availability evidence. Apply MUST freshly
compare it at every pre-effect boundary. A missing ancestor, changed mount, inaccessible
parent, symlink substitution or incomplete platform observation refuses. Stable ancestors
and mounts still do not rule out a rename of the child elsewhere; report that limitation.

Two implementation choices need owner decision before a deletable plan can exist:

- Require an independently validated deletion witness tied to the original directory
  identity and registration, with complete move/mount coverage on each supported platform.
  The platform adapter and tests must demonstrate what makes that witness authoritative;
  no such portable witness is established by this amendment. Unsupported platforms retain.
- Ship only fail-closed detection/planning until such a witness is available; do not expose
  an executable prune for arbitrary missing paths. This is the recommended initial gate.

Weakening the requirement to pathname absence with an acknowledged moved-tree risk would
be a separate owner-approved contract change, not implementation discretion. Neither a
fresh sample nor exclusive stw store locking excludes arbitrary external filesystem writers.

## Immutable plan and fresh validation

The future plan MUST bind source/backend/common repository identity and admitted layout,
source/current/primary protection identities, exact administrative registration name/path,
registration parent and complete protected sibling set, recorded destination and ancestors,
mount/deletion witness, exact raw lock reason, raw HEAD representation and resolved full
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
entries cannot be called complete recovery. Apply an initial proposed bound of 64 KiB per
native metadata file, 1 MiB total raw payload and 1,024 entries; encoded storage must also
fit its approved record bound, or use digest-bound private sidecars. Overflow refuses
before removal, never truncates. The final storage encoding belongs to the schema decision.

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
Published contracts remain unchanged under every option. Schema resolution and the absence
witness gate both precede implementation; accepting this design alone does not decide them.

## Outcomes and acceptance

Use the unified exit classes: 0 for complete read-only planning (including retained cases)
or confirmed complete apply/verified replay; 1 for refusal, incomplete/unsupported evidence,
stale state, cancellation, operational/output failure or partial/unknown effect; 2 for invalid
usage. Human and structured outcomes must agree and expose recovery availability and known
partial effects. Missing evidence is never success. These are future acceptance requirements.

Acceptance MUST include minimum/current Git on Linux/macOS, a supported witness fixture and
unsupported-witness refusal, moved and unmounted lookalikes, all ownership/selection refusals,
metadata corruption/overflow/binary bytes, private-storage failure, concurrent changes at every
boundary, interrupted removal/restore and completed replay. Compare all protected state and
sentinels, including jj/colocated rejection with no raw Git mutation. New docs examples must
come from real fixture output once implementation exists.
