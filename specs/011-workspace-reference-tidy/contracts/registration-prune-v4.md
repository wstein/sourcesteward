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
explicit opt-in, not a new default. Old readers remain strict and unchanged. Future cross-artifact amendments in
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
| Restore reader and effects | Explicit `tidy restore --contract 4` reads only version-4 prune history/payload. Inspection is read-only; `--authorize` separately permits administrative restore, with its own version-4 restore journal and fresh checks. Existing branch restore is unchanged and never used for this capability. |
| History reader | Explicit `tidy history --contract 4` enumerates only the new namespace, validates version/operation and emits version-4 recovery summaries. Old history does not reinterpret or ignore incompatible files inside its own namespace. New history does not scan raw payload bytes. |

## Expert selector and interfaces

These are proposed command grammars, not sample output or claims of implemented
commands. Existing human/JSON rendering, escaping and output-error rules apply.

- `stw tidy plan [PATH] --contract 4 --prune-registration NAME --registration-gitdir ABS_PATH --state-dir DIR [--json]`
- `stw tidy apply PLAN_ID --contract 4 --state-dir DIR --authorize [--json]`
- `stw tidy restore --contract 4 --plan PLAN_ID --state-dir DIR [--authorize] [--json]`
- `stw tidy history [PATH] --contract 4 --state-dir DIR [--json]`

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

Reject old branch/retirement/proof selectors with this expert operation,
including `--base`, `--ref`, `--task`, `--owner`, `--remove-registration`, `--remove-files`, `--retire-worktree`, `--online` and any
completion/adoption override. Unknown flags refuse, not silently ignore.
No prompt, editor, pager, network or native mutation runs during planning.

Prune apply requires the matching stored plan and explicit authorization. Restore
without authorization inspects eligibility and reports the proposed effects;
it writes no journal or recovery file. Authorized restore revalidates and writes
its separate intent before changing metadata. `--as` and branch-ref selectors
are invalid for registration restore. Restored registration remains missing
and locked; finding a moved checkout is not itself a refusal. Native
`git worktree repair` stays a separately authorized owner step, printed as
escaped argv data; it is never run by either prune or administrative restore.

## Closed record vocabulary

Every new record is strict: unknown/duplicate fields, missing required fields,
wrong types, unknown versions/enums, trailing data, invalid UTF-8 and incomplete
read refuse before writes. Arrays are present and non-null; genuinely absent
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
| New row `owner_source` | `task-record`, `unknown`; only the former may authorize selection after full ownership proof. |
| Recovery `status` | `not-prepared`, `available`, `unavailable`, `restored`, `unknown`. Available means verified payload and history pin, not merely recorded pointers. |

Each new overview row contains `id`, `kind`, `repo`, `name`, `gitdir`,
`destination`, `owner`, `owner_source`, `task`, `verdict`, `reason`, `proof`,
`effects`, `evidence_digest` and `limitations`. Only matched Spec 010 ownership
has `owner_source: task-record`; other ownership remains unproven. A row has
no private payload, raw metadata, lock/config bytes or credential-bearing text.
Its ID is the full SHA-256 of canonical `[operation, repo, name, gitdir]`, with
the domain prefix `stw-registration-row-v4\n`. Revalidate evidence before
persisting a plan; a stable row ID is not freshness or permission.

The new overview preserves the 4-MiB renderer bound and 4096 combined candidate
limit across old candidates and `registration_prunes`. A registration represented in the new array is omitted
from the legacy candidate array in overview 4, so no target is counted twice.
The new overview `plan_id` is empty until save, then the full 64-hex manifest ID.
Truncation or incomplete
inventory never authorizes the omitted or uncertain rows. Contract-4 human
wizard starts with no selection. Later T012b `all`/`--save` may select only safe
rows through the new manifest; an old-contract stale row never becomes safe
merely because this proposal exists.

## Immutable records, storage and recovery

Proposed store namespace: `tidy-registration-v4/`, separate from every legacy
plan/journal namespace. Children are `plans/<plan-id>.json`,
`journals/<plan-id>.json`, `payloads/<plan-id>/`, and
`restores/<restore-id>.json`. Plan/restore IDs are independently generated
32-character lowercase hexadecimal random IDs; stored content has its own full
SHA-256 digest. Directories 0700/files 0600 are set explicitly; no umask reliance,
symlink/hardlink traversal or overwrite of an existing ID. A missing/unrecognized
namespace is not a cue to initialize it during history/restore inspection.

A version-4 plan contains `schema_version`, `operation`, `plan_id`, `digest`,
`source_path`, `state_dir`, `backend`, `registration`, `expected`, `payload_path`,
`effects`, `recovery_limits`. The registration is exactly one name/gitdir/
destination/task/owner binding. Expected state includes the complete witnesses
from the narrow contract; digests alone are not raw metadata recovery.

A journal contains `schema_version`, `operation`, `plan_id`, `plan_digest`,
`phase`, `payload_digest`, `history_ref`, `history_oid`, `effects`, `remaining`,
`diagnostics`. A restore journal additionally contains `restore_id` and the
original prune journal digest. Per-effect entries bind action, exact target,
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
`plan_digest` is SHA-256 of the exact immutable stored-plan file bytes, verified
before decoding; the dispatched reader also verifies its existing internal
digest and semantics. This rule does not alter old plan files.
For prune, `worktree` is the exact registered missing destination, never a
filesystem-delete instruction. Selected items additionally bind the registration
name/gitdir and evidence digest; changing a label, effect or witness changes
the manifest ID. All legacy item fields remain bound too.

Canonical bytes use UTF-8 compact JSON: recursively sort object keys by UTF-8
byte order; integer values only in numeric fields; strings use Go encoding/json
Marshal escaping (including HTML and U+2028/U+2029); no insignificant whitespace,
no omitted fields, no duplicate keys, no BOM or trailing LF. Before hashing,
remove only the root `plan_id`; retain schema_version and every nested field.
Normalize array order explicitly: sub-plans by the order below; each item-ID,
authorized-scope and effect set lexicographically with duplicates refused;
selected items by ID; derived entries by their complete canonical bytes.
Preserve semantically ordered sequences inside expected-state records.
The ID is full lowercase hex SHA-256 over `stw-tidy-plan-set-v2\n` followed by
those canonical bytes. Prefixes denote literal ASCII plus one LF. Old hashing
is neither changed nor accepted as a new ID. Test permutation invariance,
field/version tampering, duplicate rejection and cross-version substitution.

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
Apply must explicitly select manifest 2 (proposed `stw tidy apply PLAN_SET_ID
--plan-set-contract 2 --state-dir DIR --authorize`); reject simultaneous `--contract` and
`--plan-set-contract`. The 64-hex ID does not itself select a reader. Legacy
manifest dispatch is unchanged; no new plan kind is routed through it. Reject
unknown/old incompatible records before effects, with no retry under another
version, extension sniffing, best-effort field dropping or silent upgrade.

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
