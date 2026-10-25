# Data Model: Stacked PR Coordination

**Status**: Proposed and UNPUBLISHED; no JSON/schema file or implemented storage
contract changes. All null/unknown facts remain explicit.

## Common identities and bounds

A plan binds exactly **one repository**. `repository` contains backend/layout
identity, validated selected forge/host/owner/name, selection provenance and
observed default ref/full SHA. Native Git commit ids are full backend-supported
object ids; the first GitHub publication capability accepts only full 40-hex
SHA-1 values compatible with current hosting evidence. Unsupported SHA-256 forge
publication refuses; it never truncates an id. jj commit/change/operation ids
remain separate native strings, never converted to a Git SHA.

Common bounds are **16 actions**, **2 open stack levels**, **200 identity-range
commits**, **50 remote requests**, **10 pages per collection**, **1000 records**,
**1 MiB raw and normalized response/result**, **64 KiB input/body text**,
**512-byte displayed text fields**, **2 KiB sanitized remote error text**, **60 s
operation**, **30 s per request/native push** and **5 s per local observation
process**. The shorter remaining operation deadline always wins. `--timeout`
can only shorten the operation bound. Oversize input is refused before decoding
an unbounded allocation; truncation never yields complete success. Display
shortening cannot change the exact identity used by a guard.

Refs eligible for action are fully qualified `refs/heads/...`; each printable
component is ASCII letters/digits/dot/underscore/hyphen, with no leading dot or
hyphen, `..`, `.lock` suffix, control bytes or option-like token; at most 200
bytes. PR ids are positive integers, repository components validated by hosting,
not arbitrary URLs. Host/repository returned by a response must equal selection.
Unprintable observations may be escaped, but cannot become command arguments.

## VerifiedRelationship

| Field | Required evidence/invariant |
| --- | --- |
| `local_layer` | Input-index layer identifier and native ref/commit identity, not a name-only match |
| `pull_request` | Number, selected repository identity, head repository/ref/full SHA, base repository/ref/full SHA, state/draft and observation time |
| `linkage` | `verified`, `ambiguous`, `unknown`; verified needs the full identity comparison, including native commit mapping where supported |
| `native_stack` | Stack identity/member order/source completeness, rewrite state `stable`, `pending`, `changed`, `unknown`; only complete stable evidence may support a dependent effect |
| `dependency` | Real code dependency evidence or `unknown`; open-depth count excludes landed layers; action publication blocks above two open levels |
| `samples` | Relevant before/after identities and source statuses; sampled state is not a transaction/ABA proof |

The input envelope binds producer/command, `kind`, `schema_version`, repository,
backend and exact sampled inputs/options before typed decoding/consumption.
A diagnostic/event string is not a schema discriminator. In particular, the
published Spec 035 `restack-needed` watch event and the proposed 012 finding have
different fields/semantics: 035 old/new parent may describe a merge commit and
branch/argv, whereas 012 layer/native-status findings rely on the sampled base
and explicit plan expectations. Neither payload implies the other. Never use a
035 merge-commit `new_parent` as 012 trunk or 015/016 target without fresh explicit
boundary validation. Unknown producer/kind/version or mismatched inputs refuses.

Supplied 012 observations remain untrusted sampled data until the action consumer
validates repository/backend/full identities and resamples its own scope. 012
short display names never supply an actionable destination ref by themselves.
The explicit 015 receipt may be used without any 012 dependency.

## ForgePlan

`plan_id` is the content identity of a canonical immutable plan; `schema_version`
is 1 and `kind` is `forge-effect-plan`. `created_at`, repository, task binding,
mode summary, limits/options, source statuses, `relationships[]`, `actions[]`,
blockers and boundary are present. `actions` is empty by default; selecting an
action does not authorize it. Changing any target, effect, expected state,
permissions, policy receipt or selection produces a new id. A plan can be
complete with blocked actions; it cannot claim those actions are executable.

`boundary` reports local reads, private-state writes, network requested/attempted,
request count, completeness, cancellation and residual sampling limits.
`--online` permits bounded reads only; it grants no write authorization. An
immutable plan may describe a blocked effect without holding credentials.

## SelectedAction

| Field | Meaning/invariant |
| --- | --- |
| `action_id` | Unique immutable plan-local id, at most 16 actions, ordered deterministically |
| `kind` | Closed proposal: `push-topic`, `create-pr`, `edit-body`, `retarget`, `ready`, `native-link`, `native-unstack`; historical `comment` is deferred and cannot be admitted for execution |
| `target` | Exact own task/ref or own PR/body identity; never a URL or inferred branch match |
| `expected` | Relevant full local/remote head/base/default SHAs, ref existence, PR lifecycle/draft, body digest, member order and capability/permission facts |
| `effects` | Exact declared method/ref/field effects, allowed local journal writes, no implicit helper/hook/network effect |
| `depends_on` | Earlier action ids only; cycles and undeclared dependencies refuse |
| `guards` | Typed `pass`, `fail`, `unknown`, `not-applicable`, with source/SHA/time/reason; unknown relevant guard blocks |
| `authorization` | At apply only, exact plan id plus explicit remote authorization for this action id; not persisted as reusable permission |
| `state` | `planned`, `blocked`, `prepared`, `completed`, `refused`, `failed`, `unknown`, `not-started` |

Push expected state is one exact full ref, full observed remote old SHA and full
actual new SHA. Ref creation uses an explicit absence guard only after separate
capability measurement; currently blocked. Retarget binds before/after head/base
and planned base. Body edits bind body digest and own-body provenance; the original draft's standalone comments remain deferred. Any future comment
capability must bind bounded selected text/action digest and accepted unique
completion proof, never a coincidental text match.

Native link/unstack retain `capability-unavailable` until measured. Inert link
reference data includes selected `GH_REPO`, validated numeric PRs bottom to top
and explicit trunk base; it is not an executable extension command.

## PublicationReceipt and ReadyReceipt

See [receipts](contracts/receipts.md) for the normative gate matrix.
`PublicationReceipt` binds actual old/new heads and complete affected ranges,
`change_class` (`docs-only` or `code`; unknown becomes code), `pure_rebase` (same
count and every range-diff row `=`), local checks, exact-actual-SHA independent
review/tier where needed, trusted owner identity and full default..new range,
config origins, signature policy and native capability.

`ReadyReceipt` separately binds current PR head/base, accepted PR scope,
independent reviewer/provenance/CLEAR at that exact head, required CI contexts
and union-source completeness, `rebaseable` (true/false/null), blockers and fresh
observation time. It is not derived from predicted-clean, 025 `not-asserted`, a
local check or a docs-only push exception. Unknown receipt source blocks action.

## Mode and attribution summaries

Mode records terminal facts per stream and marker **names only**, derived mode
and trusted task ownership. Do not dump environment values. Apply Spec 041
narrowing without inventing a human override flag. Public attribution summary
contains origin kind/time and D5 gate outcome/reason only, never a session id.

The existing Spec 042 state-only task origin keeps its D1–D5 semantics. Codex
session source is unmeasured, so absent stays absent. D5 gates push/Ready when a
recorded session exists: mismatch refuses, current absent refuses. A human or
task without recorded session is not gated by D5; unreadable task record refuses
an agent. Separate own-scope guards still refuse unknown/foreign ownership. No
copy of a session id is placed in a new journal, plan, public result or PR text.

## ActionRecord

A proposed typed private record binds repository, plan/run/action ids, effect
kind, exact expected state, intent time, outcome time, known remote identity and
sanitized outcome/reason. No token/session id/raw response body is stored. This
is a reviewed integration using existing durable primitives, not a new claim
that sync v1 is a remote transaction log.

Transitions: planned → blocked or prepared; prepared → completed/refused/failed/
unknown. Every unattempted suffix is not-started. Write intent durably before the
remote effect; record outcome durably after. Lost response or outcome-write
failure remains unknown even if the effect likely succeeded. Before retry,
resample exact post-state: prove completion → completed without replay; prove
no effect and unchanged guard → a newly authorized attempt may proceed; unknown
or changed state → refuse. Never convert a network timeout into proven no effect.
Unresolved records are retained, not automatically expired.

## OwnerHandoff

Data-only exact old/new pushed SHA, selected remote/ref and non-executed owner
resync argv/text. Separate local-commits and no-local-commits paths; reset-hard
is labelled destructive for local-only commits/uncommitted changes. Owner
checkout paths are never execution targets.

Conditional landing data records `delete_branch_on_merge` as true/false/null,
043 measured-shape status and ordered owner steps. Readable-off: land bottom,
retarget/read child, delete bottom only after all child retargets, land child.
On/unreadable: retarget, re-read, owner land, gated by 043 FR-031/034 and measured
shape admission; unreadable shape cannot pass. No 016 action id is a landing,
default-branch push or deletion action.
