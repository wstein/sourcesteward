# Data model: Conflict observation and integration preflight

> **Owner-decision amendment — 2026-10-25:** all former OQ IDs are decided;
> [current profiles](../016-stacked-pr-coordination/contracts/owner-decisions.md) supersede earlier undecided recommendations.
> Shared human/shortening-timeout flags adopted; preflight remains offline/helper-free.
> This is a documentation target, not implementation or native acceptance.

**Status**: UNPUBLISHED Markdown proposal, 2026-10-25. No schema or state-store
file is created. Names are prospective typed values, not an implemented API.

## Result envelope

| Field | Meaning and validation |
| --- | --- |
| schema_version / kind / operation | Proposed version 1, conflict-evidence, diagnose or preflight; not the restack-plan v1 schema |
| status | complete, incomplete or error; usage failures have no result |
| repository | Canonical layout identity, selected native backend/version and capability revision; absent only if admission failed |
| binding | Explicit semantic input and before/after samples, or null when unresolved; no short display identity used as a binding |
| observations | At most 2,000 path observations in deterministic native-path byte order; empty does not imply clean unless complete |
| replay | Ordered coverage for at most 200 moved commits, or null for diagnosis/non-replay |
| prediction | not-applicable for diagnosis; predicted-clean, predicted-conflicted or unknown for preflight |
| boundary | complete flag, finite limits, observations attempted/omitted, qualifications and fixed limitation |
| scratch | not-created, removed or residue; opaque bounded ownership handle for residue, no user-supplied cleanup path |
| diagnostics | At most 100 typed code/reason records with bounded sanitized messages; no raw stderr |

Complete means all required selected-capability reads/resamples finished unchanged,
all requested coverage is accounted for and cleanup completed. Complete conflict
evidence is possible. Incomplete results preserve usable facts with qualifications;
unknown is not no-conflict. Prediction may retain predicted-conflicted for a
proven Git first conflict on unchanged inputs even if later cleanup fails, but the
result is incomplete; any changed binding invalidates prediction to unknown.

## Semantic binding and sample

Repository identity includes canonical working/common/admin paths plus native
layout/ownership fingerprint; output display paths are escaped. Git retains full
object ids plus object format; native jj retains full commit, change and pinned
operation ids separately. No jj change id is converted to a Git ref identity.

Integration input names operation (merge, rebase or restack), native semantic
profile/revision, exact participants and explicit strategy/options. Git replay
binds old_parent, source_head and destination (new_parent), resolved ref names
with full sampled targets, and every ordered moved commit/parent id. Merge binds
two heads and its explicitly accepted base interpretation; no guessed mainline.
Native jj additionally binds parent/change selection and declared descendant
scope; unsupported profiles have no fabricated range. Names may be displayed but
never replace immutable ids. Before/after samples cover layout/ownership,
selected references and native operation state; failures/changes qualify the result.
No serialized absolute path, timestamp or hash alone confers ownership/authorization.

For initial Git linear replay, binding.attribute_source records policy source-head,
full commit_id H, native capability/version and enforced argv mechanism. Each
actual merge-tree receipt records that full-H --attr-source pin and private
admin/worktree/index guard outcome. Every receipt must match the semantic binding;
a missing receipt/pin or mixed original attribute source cannot satisfy complete
prediction. H/N tree attribute checks and moved-.gitattributes refusal remain
separate admission evidence, not substitutes for the actual invocation pin.

## Conflict observation and path

Each observation has origin (existing-native or predicted-scratch), native kind,
participant identities, native conflict terms/stages where known, source freshness,
completeness and omission reasons. Git stage 1/2/3 absence is explicit; an absent
base is not an empty file. Native jj multi-sided terms retain their own identities,
not synthetic ours/theirs. Marker text never overrides native conflict metadata.

A path is byte-preserving: proposed path_bytes_base64 holds exact repository-relative
bytes; path_display is valid UTF-8 with escaped controls/replacement display only.
Both human and machine displays remain data. Path bytes never become shell code,
endpoint text or an arbitrary filesystem read. Validate repository-relative scope,
no NUL, no traversal or symlink following before any selected context read.

## Optional hunk context

Default hunks is empty with reason not-requested; no file-body bytes. Once
OQ-HUNK is accepted, each hunk names exact selected path and participant/blob id,
origin (stored-blob, disk or scratch), observed byte identity/freshness when
available, line/byte location or explicit unknown, and bounded escaped context.
Binary, unavailable, unsupported-native, sensitive-policy and limit are distinct
omission reasons. Native conflict terms without reliable line ranges remain
unknown; do not invent textual hunks. Default filename metadata has its own
privacy limitation, recorded even if no hunk content is returned.

## Replay coverage and scratch lifecycle

For the Git linear kernel, each replay entry names moved commit/parent,
replay destination and outcome (clean, conflicted, not-evaluated). Stop on the
first Git conflict; suffix entries are not-evaluated, never implicitly clean.
Partial failure without a proven conflict makes prediction unknown. Ephemeral
candidate ids are never retained-object promises, review heads or apply receipts.

Native jj does not inherit this stop-first model. Conflicts can be stored in
commits while native rebase continues rewriting descendants; a later revision
can resolve or simplify an inherited conflict. A clean final tip alone therefore
cannot establish that every affected revision is conflict-free. OQ-JJCONF must
settle native per-revision evidence, introduced/inherited/resolved distinctions
and aggregate scope. Consider introduces/inherits/resolves/clean observations only
as a proposed classification requiring measured parent/result conflict terms,
not as accepted fields or a proven algorithm. The accepted capability must account
for the complete bounded selected descendant graph, its old/new commit/change ids
and pinned operation. An omitted/unknown node prevents complete clean evidence;
never label a jj suffix not-evaluated merely because the first node conflicted.
Until that semantics and scratch capability are accepted, jj preflight refuses
capability-unavailable with no fabricated replay entries or aggregate prediction.
Native stored-conflict diagnosis remains a separate supported-capability question.

Scratch progresses not-created → owned → replaying → removed, or residue after
cleanup failure. Ownership uses a private create-only handle outside originals;
cleanup must verify handle/inode/parent identity and refuse replacements. Never
import objects, move refs, attach a worktree, create a recovery root or advertise
scratch as a checkpoint. No persistent evidence cache is part of this feature.

## Bounds

See [CLI contract](contracts/cli.md#bounds-and-failure). Constraints include:
“at most 200 moved commits”, “at most 2,000 paths”, “1 MiB final result”,
“60 seconds total”, “10 seconds per native process”, “512 MiB scratch writes”,
“at most 32 hunks”, “4 KiB per hunk”, “64 KiB total hunk content” and
“at most 100 diagnostics”. Every exhausted bound makes status incomplete; no
silent truncation of a complete result. Native/profile capability decisions may
lower limits but must not silently raise them.
