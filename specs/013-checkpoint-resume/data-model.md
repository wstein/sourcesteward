# Data Model: Checkpoint, Resume and Selective Undo

**Status**: proposed and UNPUBLISHED, 2026-10-25. Fields describe logical
records/results, not a published JSON schema or implemented storage format.

## Shared identity and validation

Repository binding contains backend, native repository identity, admitted layout
fingerprint and exact workspace/task ownership evidence. Paths are repository-
relative native path bytes, not shell words or executable text; the transport
representation preserves those bytes reversibly (including invalid UTF-8 on
supporting platforms), with an escaped display field. Reject absolute/traversing
selection, NUL, control-directory access, path aliases that escape the admitted
root, symlink-following and ambiguous ownership. A symlink's selected link value
may be captured only as its own supported type, never by following its target.
Git commit ids, jj commit ids, jj change ids and jj operation ids have distinct
labelled fields and native full-identity validation; none is interchangeable.

Common record identities are opaque generated values bound to repository, record
type and version; plans have a canonical content digest. Untrusted stored records
supply data, never executable arguments, authorization or arbitrary filesystem
locations. Verify identity, containment, ownership, record type/version, bounds
and integrity on every read. Storage/migration formats await OQ-3/OQ-5.

## CaptureSelection and CapturePlan

CaptureSelection has ordered explicit paths and layer selectors: Git index,
Git disk, jj stored identity and supported jj disk payload. Default is the empty
selection. Untracked and ignored selection are independent opt-ins with exact
paths; selecting a directory is not implicit recursive capture. Each included
entry has state/type/mode, measured size, expected digest or missing/deleted
marker, and selected layers. Exclusions have path and reason without file bytes.
Secrets/environment dumps/transcripts are never discovered or collected as
context; known sensitive inputs and product/native control directories refuse.

CapturePlan adds plan id/digest, native baseline, exact selection, exclusions,
capability/version, actor/task binding, allowed effects, expected state,
retention/availability policy, bounds and recovery limitations. At most 256
selected paths; at most 8 MiB per payload and 32 MiB aggregate; at most 16 KiB
caller task text. Task text is optional inert text, never a process environment
or automatically harvested transcript. Zero selection has no capture effects.

## Checkpoint and PayloadReceipt

Checkpoint records checkpoint id, capture plan id/digest, repository/task binding,
native baseline identities, selected entries, exclusion reasons, creation time,
capability evidence and `complete`, `incomplete` or `unavailable` capture outcome.
PayloadReceipt references private storage with selected entry digests/sizes/types,
byte-layer identity and durable manifest receipt; no bytes or absolute private
path is exposed in public JSON. A committed root alone is not this receipt.
All selected required layers must be durable and verified before `complete`.
A missing payload makes the corresponding restore unavailable, even if native
objects/operation ids remain. Deletions are explicit missing markers, not empty
files. Metadata and selected-byte availability/retention are reported separately.

## ResumeComparison and RestorePlan

ResumeComparison has checkpoint availability/integrity, saved/current native
identities, per-selected-path baseline/current differences, exclusions, completeness
and separate inspect/restore capability. It includes no executable next action.
A complete comparison can report restore unsupported or unavailable. Unknown or
raced observations are explicit; read-only inspection writes no checkpoint,
index, disk, refs, jj snapshot or operation log.

RestorePlan selects exact checkpoint entries/layers and target state, current
expected native/disk/index fingerprints, ownership, affected refs/paths/descendants,
pre-action recovery receipt, limitations and authorization class. It is a new
plan with a new id/digest, never authority inherited from the capture plan.

## UndoPlan

UndoPlan has one explicit semantics value: `inverse-change` or
`history-restoration`/`operation-restoration` as native capability permits. It
binds the selected full commit/change/operation, explicit merge parent identity
when applicable, target ref/change/workspace, exact affected paths/layers/refs,
complete descendant set and expected state. Shared/foreign/unknown ownership
blocks; unknown publication/sharedness is not local-only proof. Every planned
rewritten descendant must be selected and owned. No empty selector defaults to
latest, current merge parent, entire operation or all descendants. If an inverse
would conflict, planning reports predicted conflict/unknown; no broadened undo.

## RecoveryAction and transitions

A proposed typed RecoveryAction records operation kind, plan/run/action ids,
repository/task binding, exact expected old/new native ids and file fingerprints,
root/payload receipt, allowed effects, outcome and diagnostic. It belongs to the
shared recovery architecture; no new independent writer is assumed.

`prepared` requires durable intent plus valid recovery roots and every required
payload before an effect. After an effect, persist `completed`/`failed` with actual
sampled state and per-effect result. An interrupted prepared action stays
protected. Reconciliation classifies no-effect/already-applied/changed-since/
journal-incomplete from trusted typed evidence; unknown new state never becomes
permission to reset. Restoring is a new separately journaled action. Public
result states and metadata durability states are distinct: a write failure after
an effect yields public `partial`/`incomplete`, not fabricated completion.

The exact new record envelope and retention transitions await OQ-3/OQ-5; sync
v1 `prepared`/`completed`/`failed` and its handled-incomplete `restored` state stay
unchanged. No retention count, time expiry or automatic cleanup is inferred.
