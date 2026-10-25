# Registration prune: proposed contract 4

Status: **PROPOSED / UNPUBLISHED**. Option B was decided by the owner on
2026-10-25: a separate versioned operation with explicit dispatch, unchanged
frozen tidy-overview v2 and tidy v3 bytes, no silent migration and rejection of
unknown/old incompatible plans. The exact representation below needs independent
CLEAR and explicit owner acceptance before planner/stored-plan implementation.
No schema file is added, generated, frozen, published or enabled by this document.

## Boundary and version domains

The [narrow-prune contract](narrow-registration-prune.md) remains authoritative
for one exact stw-owned locked registration, absence/ownership witnesses, quotas,
no-follow protection, recovery before effects and restore limits. This proposal
only assigns the interface and storage representation. Broad prune, unlocking,
checkout deletion, reference cleanup and automatic repair remain excluded.

Propose CLI **contract 4**, operation `registration-prune`. New result, overview,
plan, journal, restore and history documents use integer `schema_version: 4`.
Their proposed future schema files are `schemas/v4/tidy-overview.schema.json`
and `schemas/v4/registration-prune.schema.json` (named definitions for each
record). These files do not exist yet. Plan sets have their own version domain:
new manifest `schema_version: 2`, proposed `schemas/v2/tidy-plan-set.schema.json`.
Do not confuse the frozen overview's directory v2 with its current JSON
`schema_version: 1`. Existing schema files and their freeze digests stay intact.

Default wizard/expert behavior stays on its current contract. Contract 4 is an
explicit opt-in, not a new default. Old readers remain strict and unchanged.
Future cross-artifact amendments in
Specs 029 and 041 must adopt these proposed interfaces only after acceptance;
this PR neither changes their current commands nor enables T012b.

## Complete impact and dispatch table

| Surface | Proposed representation and dispatch |
| --- | --- |
| Overview and wizard rows | `stw tidy --contract 4 --json` emits the new overview-4 envelope. Preserve the old fields/candidate representation in the new schema and add required `registration_prunes` array, empty when none. Those rows have their own typed proof; never coerce them into an old native-ancestry candidate. The default old overview stays unchanged and continues retaining stale registrations. |
| Expert stored plan | `tidy plan --contract 4` selects only `registration-prune`. New plan version 4, exact singleton registration, operation, private payload destination and immutable expected-state witness. No v3 plan reinterpretation or addition to a frozen v3 enum. |
| Prune journal and payload | Separate version-4 journal with explicit operation, phase, plan/payload digests, history pin and observed effects. Raw bounded payload remains private sidecar data, never JSON output. Never pass these files to a v3 journal reader. |
| Plan-set manifest | New version 2; sub-plans carry explicit operation, contract version and expected stored-plan digest. Mixed legacy/prune sets use this version. Version-1 manifests still use their unchanged readers, hash and existing kinds; they cannot contain prune. |
| Hashing and identities | Version/domain-bound SHA-256 for the complete manifest content, selected rows, mode/scopes, derived values, sub-plan locators/versions/digests and ordered effects. New full 64-hex manifest IDs; no old 12-hex ID reinterpretation. Exact canonicalization is defined below. |
| Ordering and grouping | One prune sub-plan per repository plus exact registration; multiple selected registrations become independent sub-plans. Version-2 ordering: retire, registration-prune, branches, tracking. No automatic branch leg follows prune; same-target retire/prune overlap refuses. |
| Apply dispatch | Explicit version-2 manifest reader and closed `(kind, contract_version, operation)` dispatch. Validate every sub-plan before any effects. New prune requires `(registration-prune, 4, registration-prune)` and the version-4 plan reader. No speculative fallback through v3 readers. |
| Restore reader and effects | Explicit `tidy restore --contract 4` reads only version-4 prune history/payload. Inspection is read-only; `--apply --authorize` separately permits administrative restore, with its own version-4 restore journal and fresh checks. Existing branch restore is unchanged and never used for this capability. |
| History reader | Explicit `tidy history --contract 4` enumerates only the new namespace, validates version/operation and emits version-4 recovery summaries. Old history does not reinterpret or ignore incompatible files inside its own namespace. New history does not scan raw payload bytes. |

## Expert selector and interfaces

These are proposed command grammars, not sample output or claims of implemented
commands. Existing human/JSON rendering, escaping and output-error rules apply.

- `stw tidy plan [PATH] --contract 4 --prune-registration NAME --registration-gitdir ABS_PATH --state-dir DIR [--json]`
- `stw tidy apply --plan PLAN_ID --contract 4 --state-dir DIR --authorize [--json]`
- `stw tidy restore --contract 4 --plan PLAN_ID --state-dir DIR [--apply --authorize] [--json]`
- `stw tidy history [PATH] --contract 4 --state-dir DIR [--limit N] [--json]`

`NAME` and `ABS_PATH` are each required exactly once. The former is the exact
native immediate registration name, the latter its canonical immediate gitdir
path under the admitted common directory. Both must identify the same completed
Spec 010 task, original ownership records and exact lock; neither is inferred
from the other or from a branch/destination basename. Validate the actual native
name under existing bounded path rules, not a new ASCII-only task-ID rule.
No glob, prefix, list, duplicate, implicit selection or multiple registrations
is admitted by an expert plan. Missing/conflicting selectors are usage exit 2;
well-formed but unproven ownership/absence is refusal exit 1.

`--state-dir` is mandatory for the expert stored plan. Its directory is explicit,
private and disjoint from protected checkouts as already required; source
ownership records may be read there but are never migrated or changed. The
plan pins both the name and canonical gitdir, original creation record digests,
repository/backend/common-dir identity, source, registered destination, lock
bytes, HEAD/history target, metadata manifest, protected siblings and complete
absence/ancestor/device/mount witness. Native jj/colocated/GitButler ownership
refuses, without raw Git fallback. Plan generation writes only its disclosed
private immutable plan, not recovery payload, journal, ref or registration.

Reject old branch/retirement/proof selectors with this expert operation, including `--base`,
`--ref`, `--task`, `--owner`, `--remove-registration`, `--remove-files`, `--retire-worktree`,
`--online` and any completion/adoption override. Unknown flags refuse, not silently ignore. No
prompt, editor, pager, network or native mutation runs during planning.

Prune apply uses the named `--plan` expert path, never a positional ID. The
existing `tidy apply <12-hex-id>` path remains manifest 1, with its current
`--yes`/prompt authorization and rejection of `--plan`, `--authorize` and
`--contract`. A positional 32- or 64-hex ID still refuses on that old path.
No ID length or file sniffing silently selects a new operation.

Prune apply requires the matching stored plan and explicit authorization. Restore without both
mutation flags inspects eligibility and writes no journal or recovery file. Matching contract-3
conventions, both `--apply` and `--authorize` are required together for mutation; either flag
alone is usage exit 2. Authorized restore revalidates and writes its separate intent before
changing metadata. `--as` and branch-ref selectors are invalid for registration restore.
Restored registration remains missing and locked; finding a moved checkout is not itself a
refusal. Native `git worktree repair` stays a separately authorized owner step, printed as
escaped argv data; it is never run by either prune or administrative restore.

History follows contract 3: default limit 256 journals, explicit `--limit` in
1..1024, newest first. Zero, negative, repeated, malformed and over-ceiling
limits are usage exit 2. `restore --contract 4 --list --state-dir DIR
[--limit N] [--json]` provides the same read-only listing; `--list` excludes
`--plan`, `--apply` and `--authorize`. Outside `--list`, restore rejects `--limit`.
A declared limit or output bound sets explicit `truncated: true` and exit 1 when
more matching records exist; it is never a complete recovery inventory.

## Prune scope and authorization

Every prune overview row, selected manifest item and expert plan registration
binding contains `scope`: `own`, `other` or `unknown`. Use the existing
`owner.Resolve` rule with the verified completed creation record's owner and
the recorded `mode.caller`: an exact nonempty caller-label match is own, a
verified different owner is other, and missing/unproven ownership is unknown.
A label is not authentication; private-store access plus the complete original
creation/lock proof remains mandatory. Path/author hints cannot grant prune.

Agent mode may plan, save and apply only safe own prune items whose
`owner_source` is `task-record`. Viewing another owner does not authorize that
owner's items. Reject other/unknown agent items even with `--yes`, `--authorize`
or edited `authorized_scopes`. Human mode may select proven own or other items;
unknown ownership is never selectable for this operation and cannot be
confirmed away. Re-derive scope and mode at apply, compare the saved evidence
and refuse incompatible actor/mode changes before any effect.

For own items, `authorized_scopes` stays empty unless other selected legacy
items require it. An explicit manifest ID plus `--yes` can confirm only a set
of own items, under existing mode rules. Without `--yes`, the human path asks
for confirmation; no terminal or declined confirmation refuses. Every prompt
must first show the exact repository, registration name/gitdir, missing
destination, task/owner, scope, plan ID, metadata-only effect, recovery location,
no-expiry payload retention and moved-checkout/restore limitation, escaped and
bounded. Nothing is selected by default.

For a human's other-owned prune item, persist `other` in `authorized_scopes`
only after typed terminal confirmation under the existing `owner.Confirm`
rule: owner word
plus the first six manifest-ID characters, once per confirmation word. A label
that is not one word uses the existing literal `unknown` confirmation word;
that spelling does not admit an unknown-ownership item. `--yes` never replaces
this confirmation. Construct the proposed scope set in memory, bind it and mode before computing
the manifest ID, then confirm that ID. Persist only after confirmation. Recheck
the same owner/scope authorization and typed confirmation at apply; never
insert scopes after hashing or treat a saved scope as effect authorization.
The expert single-plan path applies the same ownership rules: `--authorize`
consents to the declared effect but cannot grant another owner's scope.
Human other-scope expert planning therefore needs the same typed confirmation,
using the expert plan ID, before storing that scope authorization. Expert
apply must obtain that typed confirmation again as well as `--authorize`;
the saved scope alone never authorizes effects.

## Closed record vocabulary

Every new record is strict: unknown/duplicate fields, missing required fields,
wrong types, unknown versions/enums, trailing data, invalid UTF-8 and incomplete
read refuse before writes. New individual plan/journal JSON records retain the
1-MiB encoded-record ceiling; overview/history results are capped at 4 MiB.
Enforce these bounds before allocation/persistence, reporting refusal rather
than truncating an executable plan. Arrays are present and non-null; genuinely absent
optional values are explicit null. Native IDs remain full backend-specific IDs.

| Field | Exact proposed spellings / rule |
| --- | --- |
| `command` | `tidy-overview`, `tidy-plan`, `tidy-apply`, `tidy-restore`, `tidy-history`, as appropriate; disambiguated by schema version and operation. |
| Plan `operation`, row/sub-plan `kind` | `registration-prune` only for new prune records. |
| Restore journal `operation` | `registration-restore`; bind the original prune plan and a distinct restore ID. |
| Row `verdict` | `safe`, `retain`, `blocked`, `unknown`; only safe, freshly proven rows can be selected. |
| Row `proof` | `owned-missing-registration` for complete admission; `none` otherwise. No claim that the user deleted rather than moved the checkout. |
| Row `reason` | `registration-prune-ready`, `registration-prune-absence-unproven`, `registration-prune-ownership-unproven`, `registration-prune-changed`, `registration-prune-unsupported`, `registration-prune-inventory-blocked`, `registration-prune-recovery-unavailable`. |
| Proposed/observed effect | `remove-registration`; restore uses `restore-registration`. Neither authorizes user-file removal. |
| Envelope `status` | `ok`, `refused`, `partial`, `error`. Partial/unknown effects are never ok. |
| Prune journal `phase` | `prepared`, `recovery-confirmed`, `removing`, `complete`, `partial`. |
| Restore journal `phase` | `prepared`, `restoring`, `complete`, `partial`. |
| Per-effect `outcome` | `planned`, `observed`, `refused`, `unknown`; only observed confirms that exact effect. |
| Row/item/registration `scope` | `own`, `other`, `unknown`; selection and authorization follow the scope section. |
| New row `owner_source` | `task-record`, `unknown`; only the former may authorize selection after full ownership proof. |
| Recovery `status` | `not-prepared`, `available`, `unavailable`, `restored`, `unknown`. Available means verified payload and history pin, not merely recorded pointers. |

Each new overview row contains `id`, `kind`, `repo`, `name`, `gitdir`,
`destination`, `owner`, `owner_source`, `scope`, `task`, `verdict`, `reason`, `proof`,
`effects`, `evidence_digest` and `limitations`. Only matched Spec 010 ownership
has `owner_source: task-record`; other ownership remains unproven. A row has
no private payload, raw metadata, lock/config bytes or credential-bearing text.
Its ID uses the row encoding and domain in the digest table below.
Revalidate evidence before
persisting a plan; a stable row ID is not freshness or permission.

The new overview preserves the 4-MiB renderer bound and 4096 combined candidate limit across old
candidates and `registration_prunes`. A registration represented in the new array is omitted
from the legacy candidate array in overview 4, so no target is counted twice. The new overview
`plan_id` is empty until save, then the full 64-hex manifest ID. Truncation or incomplete
inventory never authorizes the omitted or uncertain rows. Contract-4 human wizard starts with no
selection. Later T012b `all`/`--save` may select only safe rows through the new manifest; an
old-contract stale row never becomes safe merely because this proposal exists.

## Digest encodings and domains

Define `C(value)` as UTF-8 compact JSON with recursively sorted object keys in
UTF-8 byte order, integer numeric fields, Go encoding/json Marshal string
escaping (including HTML and U+2028/U+2029), and no insignificant whitespace,
BOM, trailing LF, duplicate keys or omitted fields. Present arrays never become
null. Do not normalize Unicode, case, paths or native IDs while encoding them;
admission must already have established the exact bound spelling/identity.
Array normalization is explicit below; never sort an ordered effect sequence
or the root-to-parent ancestor chain as if it were a set.

`H(domain, bytes)` means lowercase 64-hex SHA-256 of the literal ASCII domain,
then exactly one LF byte, then exactly `bytes`. Domains below have no implicit
NUL, length prefix or other separator. A digest field encodes that hex string,
not binary digest bytes. Existing native/creation-record digests retain their
own original contracts; these domains do not reinterpret or migrate them.

| Value | Exact domain and bytes before hashing |
| --- | --- |
| New row `id` | `H("stw-registration-row-v4", C(["registration-prune", repo, name, gitdir]))`; array order exactly as written. |
| `evidence_digest` | `H("stw-registration-evidence-v4", C({schema_version: 4, operation: "registration-prune", registration: registration, expected: expected}))`. This is a field-shape definition, not program output. Include every registration and expected-state field; omit no witness or original-record binding. |
| Plan `digest`, also the prune/restore journal's `plan_digest` | `H("stw-registration-plan-v4", C(plan without only its root digest field))`. Include plan ID, schema/operation, paths, mode/scopes, expected state, payload location and all declared effects/limits. |
| `payload_digest` | `H("stw-registration-payload-v4", C(payload_manifest))`. Manifest fields are schema_version 4, operation registration-prune, plan_id, plan_digest and entries. It has no self-referential payload_digest field. |
| Manifest-2 sub-plan `plan_digest` for prune | `H("stw-registration-plan-file-v4", exact stored-plan file bytes)`. Verify before decoding; separately verify the internal plan.digest above. |
| Manifest-2 sub-plan `plan_digest` for legacy v3 | `H("stw-tidy-plan-file-v3", exact existing stored-plan file bytes)`. Preserve those bytes and validate with the unchanged v3 reader/internal digest rules. |
| Restore journal's `prune_journal_digest` | `H("stw-registration-journal-file-v4", exact original prune-journal file bytes)`, sampled and revalidated before restore effects; restore never edits that original journal. |
| Manifest-2 `plan_id` | `H("stw-tidy-plan-set-v2", C(manifest without only its root plan_id))`, with manifest array normalization below. |

Payload entries are sorted by exact relative UTF-8 path, with duplicates refused.
Each entry binds `path`, `kind` (`file`, `directory`, `absent`), original `mode`,
`size` and `content_sha256`. Mode is the observed uint32 file-mode value,
encoded as a decimal JSON integer; size is a nonnegative decimal integer.
For a file, content_sha256 is ordinary SHA-256 over
all raw content bytes, without decoding, prefix, LF conversion or truncation;
size is the exact byte length. For a directory, size is zero and content hash
is null. For explicit optional absence, mode/hash are null and size is zero.
This is a logical manifest of bounded private sidecars, not a JSON conversion
of binary metadata. The outer payload domain binds kind/path/mode/length/hash
and its plan; a file hash alone is never complete recovery proof.

Within expected state, metadata entries sort by relative path, protected
siblings by native registration name then canonical gitdir, references by full
native name, and original-record bindings by document role then immutable ID.
Compare UTF-8 bytes and reject duplicate logical keys before sorting. Preserve
ancestor-chain order from root to immediate parent, and declared mutation-step
order. Sort mode marker/owner-filter sets, authorized_scopes and other explicitly
set-valued names lexicographically, rejecting duplicates. No independent array
order guess is allowed. These rules apply to both the evidence and plan digest.
New plan/journal files are `C(full record)` plus one terminal LF; file-domain
hashes include that LF. Raw sidecar bytes remain exact. Test LF/NUL/binary bytes,
field and domain substitution, array permutations, missing witnesses, duplicate
keys and all digest mismatches before any effect.

## Immutable records, storage and recovery

Proposed store namespace: `tidy-registration-v4/`, separate from every legacy
plan/journal namespace. Children are `plans/<plan-id>.json`,
`journals/<plan-id>.json`, `payloads/<plan-id>/`, and
`restores/<restore-id>.json`. Plan/restore IDs are independently generated
32-character lowercase hexadecimal random IDs; stored content uses the
explicit digest encodings above. Directories 0700/files 0600 are set explicitly; no umask reliance,
symlink/hardlink traversal or overwrite of an existing ID. A missing/unrecognized
namespace is not a cue to initialize it during history/restore inspection.

A version-4 plan contains `schema_version`, `operation`, `plan_id`, `digest`,
`source_path`, `state_dir`, `backend`, `mode`, `authorized_scopes`,
`registration`, `expected`, `payload_path`,
`effects`, `recovery_limits`. The registration is exactly one name/gitdir/
destination/task/owner binding. Expected state includes the complete witnesses
from the narrow contract, including mandatory `expected.common_dir_mount`.
Digests alone are not raw metadata recovery.

**Open implementation gate (#273 F2):** the merged admission slice observes
destination-ancestor mount identities but does not yet bind the common
directory mount. Before T503 planner/stored-plan acceptance and T505 apply
acceptance, implement predicate 3 for that common directory: bind its native
mount identity/availability and open-descriptor/path correspondence alongside
device/inode/type/owner/mode, and revalidate them before every effect. Supported
Linux/macOS native fixtures must prove this witness and refusal on its change,
absence or unsupported observation. Destination witnesses or common-directory
device/inode alone are not substitutes. This is mandatory missing source/test
work, not another owner question or a completed guarantee. See the
[admission evidence](../evidence/narrow-prune-admission-20261025.md).

A journal contains `schema_version`, `operation`, `plan_id`, `plan_digest`,
`phase`, `payload_digest`, `history_ref`, `history_oid`, `effects`, `remaining`,
`diagnostics`. A restore journal additionally contains `restore_id` and the
original `prune_journal_digest`. Per-effect entries bind action, exact target,
intent and observed outcome; an unknown outcome remains partial. The native
history pin is `refs/stw/recovery/registration-prune/<plan-id>/head`, created
only at an absent name and verified before removal. It preserves committed
history; only verified payload preserves index/reflog/lock bytes.

Payload bounds, binary fidelity, durable preparation, exact postconditions and
replay rules remain those of the narrow contract: 64 MiB/file, 256 MiB total,
1024 entries and 64-KiB streaming buffers. Journal/payload retention never follows
legacy tidy expiry or silent plan migration. No automatic expiry/deletion is
introduced; unavailable recovery refuses, and partial evidence is retained.
History returns bounded summaries/pointers and recovery availability, never raw
payload. Its total JSON output is capped at 4 MiB; reaching the bound reports
incomplete evidence and exit 1, never a misleading complete list.

## Manifest 2: canonical hashing, ordering and closed dispatch

Propose a separate `tidy-plan-sets-v2/<64-hex-id>.json` namespace; existing
manifest-1 storage and readers remain unchanged. No automatic conversion.
Manifest fields are `schema_version`, `plan_id`, `sub_plans`, `derived`, `items`,
`authorized_scopes`, `mode`. Sub-plans have required `id`, `state_dir`, `repo`,
`kind`, `worktree`, `items`, `contract_version`, `operation`, `plan_digest`;
`plan_digest` uses the version-specific file domain in the digest table,
verified before decoding; the dispatched reader also verifies its internal
digest and semantics. This rule does not alter old plan files.
For prune, `worktree` is the exact registered missing destination, never a
filesystem-delete instruction. Selected prune items additionally bind the registration
name/gitdir, scope and evidence digest; changing a label, effect or witness changes
the manifest ID. All legacy item fields remain bound too.

Use `C` and the manifest-ID domain from the digest table; remove only root
`plan_id` and retain schema_version and every nested field. Normalize manifest
arrays explicitly: sub-plans by the order below; each item-ID, authorized-scope
and effect set lexicographically with duplicates refused; selected items by ID;
derived entries by their complete canonical bytes. Preserve semantically
ordered sequences inside expected-state records. Old hashing is unchanged and
is never accepted as a new ID. Test permutation invariance, every bound-field
mutation, domain/version substitution and duplicate refusal.

| Sub-plan kind | Contract / operation | Apply order and reader |
| --- | --- | --- |
| `retire` | `3` / `tidy-v3` | 0; existing v3 retirement reader/validator. |
| `registration-prune` | `4` / `registration-prune` | 1; new singleton prune reader/validator. |
| `branches` | `3` / `tidy-v3` | 2; existing v3 reference reader/validator. |
| `tracking` | `3` / `tidy-v3` | 3; existing v3 tracking reader/validator. |

Within each rank order by repository identity, worktree path, native registration
name (empty for legacy) and sub-plan ID, comparing UTF-8 bytes. No duplicate
registration or overlapping same-target effects are allowed. Validate all
versions, hashes, modes/scopes, identities and sub-plan membership before the
first write, then revalidate each operation's fresh safety evidence at apply.
No branch cleanup is synthesized from prune; selected branch work needs its
own fresh independent proof. A later per-sub-plan refusal cannot imply that
earlier successful effects were undone; retain explicit per-sub-plan outcomes.

Contract-4 wizard save/apply uses manifest 2 even for a legacy-only selection.
The proposed distinct subcommand is `stw tidy apply-set PLAN_SET_ID
--plan-set-contract 2 --state-dir DIR [--yes] [--json]`. It accepts exactly one
full 64-hex manifest ID and the explicit version selector; it rejects `--plan`,
`--authorize` and `--contract`. This avoids changing today's positional
`tidy apply` reader, parser or authorization. Existing manifest-1 commands,
12-hex IDs and flags remain unchanged; neither path accepts the other's IDs.
The new path retains existing wizard mode, `--yes` and terminal-confirmation
semantics, including typed confirmation for other scopes; it does not invent
an expert `--authorize` bypass for plan sets.

A contract-4 wizard that applies a new selection directly invokes the same
manifest-2 validator/dispatcher and authorization, never the old positional
reader. The selected manifest version, not an ID-length guess, controls its
internal route. Validate the closed version/operation tuple and every stored
sub-plan before effects. Reject unknown/old incompatible records, with no
retry under another version, extension sniffing, best-effort field dropping
or silent upgrade.

## Acceptance and owner gate

The proposed acceptance pairs are T501-T508. Pin the frozen overview-v2 and
plan/journal-v3 bytes and old reader behavior before introducing new types.
Tests must show default old output unchanged, explicit opt-in, exact selector
admission/refusal, strict new records and version-mismatch refusal, full-field
hash sensitivity, deterministic mixed ordering and pre-effect dispatch failure.
Prune/restore safety tests retain the existing entire narrow-contract matrix.

Exit classes are unchanged: 0 complete plan/inspection or confirmed apply/
restore/replay; 1 refusal, unsupported/incomplete/stale evidence, operational
error, cancellation or partial/unknown outcome; 2 invalid usage. A complete
read-only overview with retained rows may succeed; a refused expert selector
cannot. Escape untrusted names, paths and diagnostics in human/JSON output;
never log raw payload or credential-bearing data. No network or new dependency.

Needs owner acceptance: the exact contract-4 interfaces, schema names/version
assignments, manifest-2 model/hash/order/dispatch and restore/history namespace
above. Option B itself is decided and is not reopened. No T501-T508 completion,
planner/apply/restore implementation or T012b enablement is claimed by this PR.
