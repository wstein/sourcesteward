# Proposed Contract: Explicit Checkpoint Capture

**UNPUBLISHED — unimplemented**, 2026-10-25. No schema file or command is added.
Backend admission/storage choices in [research](../research.md#needs-owner-decision)
block affected implementation. These command forms illustrate a future interface.

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
