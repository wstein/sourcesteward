# Proposed Contract: Explicit Checkpoint Capture

**UNPUBLISHED — unimplemented**, 2026-10-25. No schema file or command is added.
Backend admission/storage choices in [research](../research.md#needs-owner-decision)
block affected implementation. These command forms illustrate a future interface.

## Common interface vocabulary (proposed)

All command spellings and flag admission below remain proposed under OQ-5 and
interface acceptance; they are not newly settled owner decisions or implemented
aliases. Reusing the `--authorize` spelling relates 013 to existing plan/apply
conventions; it does not import Spec 041's wizard or Spec 043's human-only landing
confirmation. 013 remains noninteractive even without a terminal.

| Input/output | Proposed 013 meaning and compatibility boundary |
| --- | --- |
| `--state-dir PRIVATE_DIR` | Required explicit private artifact-store selection outside every admitted or encountered repository/worktree, subject to OQ-3's location/quota decision and the mandatory containment/mode rules below. Store plans, typed records and selected-payload receipts there through the reviewed seam; never treat the path as capture selection or authority. Resume only reads an existing store and never creates or repairs it. |
| `--plan PLAN_ID --authorize` | Both are required for the selected native capture/restore/undo effects; the id binds the exact reviewed plan, and authorization is explicit for that action. An authorization flag alone cannot select targets or advance a plan. Declared private planning writes remain the planning effect only. |
| Default human text / `--json` | Proposed complete-result formats from one model, with no TTY requirement, prompt, editor or pager. `--json` selects one final versioned object; output format never changes authorization or native effects. |
| `--online` | Inapplicable to this offline feature; the proposed parser rejects it as usage (exit 2), rather than broadening the operation. No network mode or implicit fetch is introduced. |
| `--human` | Explicit human-format alias is an open OQ-5 interface recommendation for consistency with 041/043, not accepted syntax in these examples. If adopted, specify its exclusivity with `--json` and test both formats without a TTY before admission; otherwise reject the unknown flag as usage. |
| `--timeout` | Caller deadline override is an open OQ-5 interface recommendation, not accepted syntax here. Until reviewed, the fixed whole-operation caps below govern and the unknown flag is usage. Any accepted override must be positive, bounded by those caps, and must not extend an inherited earlier deadline. |

Specs 033/037 use `--offline` to opt out of their default online behavior;
those existing commands are unchanged. 013 is already offline: this proposal
does not accept a redundant `--offline` flag, and its proposed parser reports
that unknown flag as usage (exit 2). Any later admission remains an OQ-5
interface choice, not an accepted alias or a change to 033/037.

## Selection and effects

```text
stw checkpoint plan --path WORKSPACE --state-dir PRIVATE_DIR [--select-layer LAYER --select-path PATH] [--include-untracked-path PATH] [--include-ignored-path PATH] [--task-text TEXT] [--json]
stw checkpoint apply --plan PLAN_ID --state-dir PRIVATE_DIR --authorize [--json]
```

No explicit selection means none: return a complete `nothing-selected` result,
no native mutation or capture store write. An explicit path/layer pair selects
only that pair; neither tracked status, directory membership, dirty state nor
an agent/task name selects content. Untracked and ignored paths are excluded
unless separately selected, supported and admitted. Reject incompatible layers,
unsupported types and aliases/escapes rather than partially acknowledging them.
No automatic credential/environment/transcript collection. Reject known sensitive
inputs and native/product control directories; do not claim heuristic scanning
can prove that arbitrary caller-selected source bytes contain no secrets.
The caller reviews exact exposure before capture. No payload is printed.
For this proposed first slice, the deterministic sensitive-input deny set is any
caller-declared credential/environment-dump/transcript input, basename `.env` or
`.env.*`, `.netrc`, `credentials`, `id_rsa`, `id_ed25519`, and path components
`.git`, `.jj`, `.gitbutler` or the selected private state directory. Matching is
byte-exact and case-sensitive; platform aliases that defeat it refuse. This is a
bounded exclusion policy, not a proof that other explicitly selected source
bytes contain no secrets. Expanding capture beyond it requires contract review.

Plan may write its disclosed private plan record only for an explicit selection;
it does not snapshot native jj, stage Git content, stash, move refs, rewrite files
or retain bytes. Show selected type/mode/size/layers, exclusions, native ids,
expected state, storage policy, exact allowed apply effects and recovery limits.
Apply requires its exact stored plan id and explicit `--authorize`; neither
observation nor plan creation authorizes apply. A task's old approval cannot
supply a different actor or operation's authorization. Freshly revalidate layout,
all relevant workspaces, task/actor ownership, paths, bytes/layers, config hazards,
capability/version and bounds before the first and each subsequent effect.

The new selected-byte store is distinct from journal metadata. Save exact admitted
index/disk bytes and type/mode/deletion metadata for each selected layer; retain
native committed roots where the capability requires them. Native jj may need a
disclosed selected snapshot effect only if OQ-1 approves and conformance proves
it; otherwise it refuses. Never substitute Git for jj. An operation id alone
cannot establish saved disk state. No implicit snapshot is hidden in planning.

Persist typed capture intent before owned private staging writes. If an admitted
native capture effect would alter existing state, first create its required
pre-action roots/selected-byte recovery receipt and durably prepare intent, as
in the recovery contract; otherwise refuse that capability. Private payload
staging is create-only and cannot replace unrelated state.
Create durable selected payload and receipt, validate integrity, then complete
the typed checkpoint record. If required receipt/root/record durability fails,
return incomplete and retain known evidence; do not report a complete checkpoint.
A crash between payload and receipt leaves owned orphan evidence for inspection,
never automatic deletion or authority to restore. Capture does not clear dirty
files or alter unrelated index entries. Future native effects require a reviewed
capability/effects table before enablement; default is unsupported.

## Private payload storage and explicit deletion

Selected source bytes may contain secrets despite the deny set. Create private
store/staging/checkpoint directories with explicit mode `0700` and all payload,
manifest, plan and journal files with explicit mode `0600`; verify those modes
and current-actor ownership independent of inherited umask, including `0000`.
Refuse an existing foreign-owned or group/world-accessible store/file, including
mode `0666`; do not silently chmod, adopt or follow it. Use no-follow handles and
revalidate ownership/path identity before every write, rename, read or deletion.
Never include captured content in logs, diagnostics, human output or JSON.

`--state-dir` must be outside every repository/worktree relevant to its location
proof, including native metadata and the target's linked-workspace inventory.
Resolve canonical existing ancestors securely, run native Git/jj discovery at
those ancestors and use complete native linked-workspace/layout evidence;
reject a directory inside an encountered ordinary/bare/nested repository,
colocated jj repository or linked worktree, even when ignored. Reject symlink,
replacement, incomplete discovery or uncertain containment. This is a fail-closed
native/layout proof, not a claim to discover every repository on the filesystem.
No private state is written until the external-location proof succeeds.

Retain the 32 MiB payload cap per checkpoint across every selected layer and
invocation, not just per command. OQ-3 recommends a 1 GiB total physical-store
quota including metadata, staging, incomplete/orphan payloads and retained records;
reserve/count in-flight bytes under the reviewed store lock and refuse before
any write would exceed it. Count all physical files, not deduplicated logical
receipts. The exact quota remains an owner choice before store admission; no
unbounded store or automatic eviction is admitted in the meantime.

Proposed OQ-3/OQ-5 interface, not an implemented or approved command:

```text
stw checkpoint drop-plan --checkpoint CHECKPOINT_ID --state-dir PRIVATE_DIR [--json]
stw checkpoint drop --plan DROP_PLAN_ID --state-dir PRIVATE_DIR --authorize --confirm-checkpoint CHECKPOINT_ID [--json]
```

Drop-plan selects one exact checkpoint and enumerates only its original capture
payload files, metadata and checkpoint roots, dependencies, expected digests/OIDs
and physical bytes. It never selects pre-action safety-copy records, payloads or
`undo/safety` roots, including those from completed restores. Safety copies have
independent ids, receipts and retention; deleting the source checkpoint cannot
delete or invalidate their recovery data.
Apply requires that stored plan, `--authorize` and exact checkpoint-id confirmation;
there is no prompt, wildcard/default selection or force bypass. Freshly verify
ownership/modes/location, complete reference/dependency inventory and expected
values. Refuse unresolved prepared mutations or another record's live receipt;
only reconciled incomplete captures with no live dependency may be selected.
Unlink only exclusive owned payloads and delete only exact 013 roots by expected-
OID CAS; never traverse another feature's state or run native GC. No pre-drop
byte backup is created that would defeat the explicit deletion request.

Journal intent without payload content and preserve manifests/receipts until
per-file/root deletion is verified. A crash or changed file/root reports partial
with removed/remaining/uncertain files, roots and byte counts; retain sufficient
metadata for inspection/retry and never claim bytes removed merely because a
receipt was deleted. An already absent verified owned target can reconcile;
replaced or unreadable targets refuse. Completion means the selected private
payloads and exclusive roots are verified absent, with a bounded metadata-only
tombstone. Unlinking is not secure erasure: backups, storage snapshots, retained
native objects/history and prior external copies are outside this guarantee.
No automatic expiry, GC or cross-feature cleanup is authorized.

Proposed OQ-3/OQ-5 safety-copy deletion is a separate interface, not an alias for
checkpoint drop or an implemented command:

```text
stw checkpoint drop-safety-plan --safety-copy SAFETY_ID --state-dir PRIVATE_DIR [--json]
stw checkpoint drop-safety --plan SAFETY_DROP_PLAN_ID --state-dir PRIVATE_DIR --authorize --confirm-safety-copy SAFETY_ID [--json]
```

The caller must explicitly name the safety copy and separately confirm its id;
checkpoint confirmation or completion never authorizes this deletion. Apply the
same exclusive ownership, complete dependency inventory, expected-value root
CAS, per-file verification, partial byte/root counts and tombstone rules. Keep
records/receipts until their corresponding bytes/roots are verified absent.
Unresolved action or live receipt dependencies refuse; no wildcard, implicit
cleanup, automatic retention deletion or secure-erasure claim exists.

## Bounds

Proposed limits per invocation: one repository/workspace, 256 selected paths,
8 MiB per selected payload, 32 MiB total payload, 16 KiB caller task text,
1 MiB complete rendered result and 2 KiB per diagnostic. Read/plan/compare has a
30-second whole-operation deadline; authorized capture/restore/undo has a
120-second whole-operation deadline. No automatic retries. Length/count discovery
may read one sentinel unit beyond the cap to detect overflow; never capture it.
Reject selection that exceeds payload bounds before effects. Mid-read growth,
output excess, malformed data, timeout or cancellation yields explicit
incomplete/partial with no next action. Stop before further effects; safely bound
and reap any child already in flight, then report known/uncertain outcomes.
No result truncation is a complete plan or authority. Exact native version and
capability support must be in the conformance admission matrix.

## Shared human/JSON contract and exits

One typed result renders understandable human text and a single final versioned
JSON object. Proposed envelope fields: `schema_version`, `command`, `operation`,
`backend`, native `repository`/`workspace` identities, `plan_id`, `checkpoint_id`,
`selection`, `exclusions`, `availability`, `effects`, `outcome`, `complete`,
`limitations`, `diagnostics`, `bounds`. Absent/inapplicable values are explicit;
no success-shaped empty result after failed reads. Error results retain any
completed/uncertain effects; diagnostics never expose payloads, secrets or raw
unbounded native output. Proposed fields are logical names, not a published schema.

Human rendering escapes control/ANSI/bidirectional bytes and quotes paths/refs;
structured path transport preserves native bytes reversibly with an escaped
display value. Both modes derive the same outcomes/qualifications. Repository
text, context and diagnostics are inert data, never executable suggestions.
No editor, pager, prompt, helper, repository hook, model or network runs. A fixed
allowlisted native command may run only within its declared backend/effect boundary;
config-selected execution refuses rather than bypassing repository policy.

| Exit | Proposed meaning |
| --- | --- |
| 0 | Complete observation/plan/capture, including `nothing-selected`; capability limits can be observed but never imply apply capability |
| 1 | Refusal, unsupported requested action, stale evidence, incomplete/partial, cancellation, deadline, output/storage/native failure |
| 2 | Invalid syntax, incompatible selectors or missing required explicit arguments |

Future restore and undo use this envelope with their own action fields. Preserve
published contract bytes and existing hidden Spec 015 v1 incomplete exit 3;
013's proposed 0/1/2 scheme is no migration authority. JSON remains UNPUBLISHED
until schemas, valid/invalid fixtures and native conformance are implemented.
