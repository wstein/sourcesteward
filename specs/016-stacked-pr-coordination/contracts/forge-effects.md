# Contract: Selected Forge Effects

**UNPUBLISHED proposal**. No command, API write or schema is implemented by this
contract. Existing published JSON bytes and hidden Spec 015 v1 behavior stay
unchanged. Pending owner choices in [research](../research.md#needs-owner-decision)
block their affected capabilities.

## Proposed command surface

```text
stw stack publish
stw stack publish plan PATH --repo OWNER/NAME --observation FILE
    [--select ACTION_ID ...] [--online] [--state-dir DIR] [--save]
    [--timeout DURATION] [--json | --human]
stw stack publish apply PATH --plan ID --state-dir DIR --online
    --authorize-remote ACTION_ID [--authorize-remote ACTION_ID ...]
    [--timeout DURATION] [--json | --human]
```

The first line shows help only, with no repository/authentication/read/write or
prompt. These names are OQ-SURFACE proposals; `FILE` is bounded input data, never
commands. Validate producer/command, kind, schema_version and exact input
bindings before accepting an observation or receipt; event/diagnostic names alone
never choose a decoder. The published 035 restack-needed event is not a 012
finding or 015 receipt, and its merge-commit new-parent cannot become a trunk or
apply/push target without fresh explicit boundary validation.
A typed 015 actual receipt can be supplied instead of 012 observation
through a surface to be accepted under the same OQ; no hard 012 dependency is
introduced. Actions are identified by typed kind+target in supplied intent;
`--select` chooses exact displayed ids. Unknown/duplicate ids or inconsistent
selection are usage errors before effects. Empty selection yields an empty plan.

`--online` permits disclosed bounded reads only and defaults off. Offline
consumption never calls auth, fetch, HTTP or an extension, including when supplied
evidence has remote fields. Offline dependent guards stay unknown/blocked. Apply
requires explicit online mode, exact stored plan and each selected action's own
`--authorize-remote ACTION_ID`; a missing authorization refuses that action before
any write. Additional authorization ids outside the stored selection are usage
errors. Local `--authorize`, an old receipt or a generic `--yes` is insufficient.
The deterministic draft persistence rule is render-only by default: without
`--save`, do not create/repair a store or write a plan, and emit `saved: false`,
`plan_id: null`. A nonempty plan is persisted only with explicit `--save` AND
`--state-dir DIR`, where DIR is an existing validated private directory. Missing
DIR with `--save` is usage 2. No default directory is selected until OQ-SURFACE/
OQ-STORE accepts one. A supplied `--state-dir` without `--save` may read existing
task evidence only; it never creates/repairs the directory or persists the plan.
Saving an empty selection returns nothing-selected with no store write and null
plan id. A successful nonempty save reports `saved: true` and the immutable
stored plan id; blocked actions can be saved as review data, never permission.
Apply requires that exact existing saved id and explicit validated directory;
an unsaved result, nonexistent id or unreadable record cannot be applied.

No flag widens agent ownership. Standalone comments cannot be authorized by this first surface; they are deferred original-draft scope, not an initial action. No interactive confirmation/editor/pager is used.

Human is default, `--human` forces it, and it excludes `--json`. `--timeout` must
be positive and at most 60 s, only shortening operation limits. Unlike online-default 033/037, this proposal has no
`--offline` flag because offline is already default; unknown `--offline` is usage
2 until OQ-SURFACE accepts any explicit alias. Do not inherit opposite-polarity
flags or network defaults from another command. `--state-dir` is
one validated private store for plans/action records, outside every repository,
user-owned, real, mode 0700, no symlink component; unsafe/unreadable store refuses.
There is no proposed implicit default or automatic store creation; future
default/provenance changes remain OQ-SURFACE/OQ-STORE. No state-path environment
dump. Explicit save is a disclosed private-state write, never a repository mutation.

## Capability/effect table

| Kind | Exact scope and guards | Current proposed enablement |
| --- | --- | --- |
| `push-topic` | One verified own existing native Git topic, exact ref and actual new SHA, observed old SHA, trusted owner range/config/signature and review gates | Requires accepted native transport/hook confinement and receipt policy; absent-ref creation separately unavailable until measured |
| `create-pr` | One own head branch and explicit verified base in same repository; bounded selected title/body, no labels/reviewers/auto-merge extras | Fixed method/path/body/duplicate reconciliation must be measured and allowlisted under OQ-WRITES |
| `edit-body` | Own PR, own-body provenance, expected current body digest and bounded exact replacement; no remote instructions executed | OQ-WRITES gate; raced/foreign body refuses |
| `comment` (deferred original draft) | No standalone comment execution in the first agent capability; own PR body edits are the current proposal | Separate owner scope choice plus OQ-WRITES unique-completion/idempotency gate before any future admission |
| `retarget` | Own open child, exact old head/base and explicit new verified base, head unchanged after edit | Proposed bounded base-only PR edit; requires additive write allowlist acceptance, no other PR field |
| `ready` | Own PR and exact-current-head independent CLEAR, required green CI, `rebaseable == true`, accepted scope/no in-scope blocker, D5 | Exact Ready operation/method unmeasured here; OQ-WRITES must pin it before enablement |
| `native-link`, `native-unstack` | Own selected PRs, verified member order/trunk, stable complete native evidence and permission | Endpoint/method/effects/version/idempotency unmeasured; capability-unavailable, reference data only |

Forbidden action kinds: merge/land, remote deletion, default/protected-branch or
tag push, release, ruleset/settings/protection edit, auto-merge, review submission,
CI rerun, arbitrary command/endpoint and generic sync. Sync is explicit composition
of independently planned local/remote actions, never a write bundle.

Native jj observe/plan consumption is separate from native publication. Unsupported
jj publication, including colocated state, refuses `capability-unavailable`
without a Git push. No universal Git ref/lease claim is applied to jj.

## Transport admission

Resolve native ownership and selected repository locally using 020 before any
authentication. Reuse bounded 022 caller-owned selected-host authentication and
fixed validated HTTPS GET evidence, with relevant 025/030 qualification. Existing
022 modes remain GET-only. Proposed PR writes extend the transport only through
an independently reviewed new closed capability union: exact generated method,
path, accepted body fields/size and response validation. The table above is an
effect description, not a fabricated endpoint allowlist. Until method/path/body
and effects have accepted measurements/tests, that kind refuses
`capability-unavailable` without a request. No arbitrary REST/GraphQL, redirects,
response-selected URLs, repository SDK discovery or dependency extension launch.

Native link reference data has this inert shape:

```text
environment: GH_REPO=OWNER/NAME
argv: [gh, stack, link, --base, TRUNK_REF, BOTTOM_PR_NUMBER, CHILD_PR_NUMBER]
effect: unmeasured remote write; not executed
```

`gh stack link` has no `-R`. Numeric PRs avoid branch arguments that may push, but
numeric-link effects themselves remain unmeasured. No inference from its help,
installed version or native stack GET enables extension execution. Link/unstack
must not race observed/pending GitHub member rewrites; `native-rewrite-pending`
or unknown native state blocks dependent publication. No rebase/force push is
substituted to repair native membership.

## Native Git push

For one existing owned topic only, the fixed vector is:

```text
git push --porcelain --force-with-lease=refs/heads/TOPIC:FULL_OBSERVED_OLD_SHA REMOTE FULL_ACTUAL_NEW_SHA:refs/heads/TOPIC
```

Both SHAs are full, explicit and observed/actual rather than predictions. `REMOTE`
is a validated selected transport with disclosed network effects; credentials,
URLs, push options, aliases, helpers and hook policy cannot be repository-selected
execution. The native push capability stays disabled until its bounded transport,
authentication and no-undeclared-helper/hook behavior are measured; the vector
alone does not solve that boundary. Ordinary Git push can run a pre-push hook
and transport/credential/signing helpers: it is not inherently helper-free.
Installed relevant pre-push hooks or repository-selected helper/signing hazards
refuse before effects until a separately accepted declared execution profile
exists. Never silently skip hooks or substitute an unsandboxed raw push.
Authentication is confined to the accepted bounded caller-owned 020/022 route;
this proposal introduces no generic Git credential-helper route. No `--force`, bare lease, `+`, `--all`,
`--mirror`, tags or alternate refspec. Ref/protection/own-session/policy gates run
fresh immediately before intent and push. Remote old value is read in this run,
not borrowed from a stale local tracking ref. Lease rejection is `remote-changed`,
never an invitation to update the lease and retry automatically.

A newly absent ref needs a distinct expected-absence capability; unknown/not-found
or denied does not prove absence. Creation stays blocked until its contract is
measured and accepted. No remote branch deletion is available.

## Execution, retarget and retry

Actions execute in deterministic dependency order. Independent reads may use
040 indexed fanout with width at most four; dependent reads/writes are sequential
and share one budget. Before every write: immutable plan/authorization, backend,
repository/layout, ownership/task, D5 when applicable, exact local/remote state,
permissions/protections, declared capability and action-specific receipt all pass.
A complete plan with blocked actions is not an apply success.

Record durable intent before the effect and actual/unknown outcome after.
No write occurs if intent cannot be made durable. A response loss, malformed
post-write reply or outcome-write failure is `unknown`, not proven failure.
Stop before the next effect after cancellation/failure/unknown. Retry reads
fresh exact state first; proven completion is not duplicated. Unknown create,
comment, Ready or link outcomes cannot be guessed from names/text, and block
replay until accepted reconciliation proves completion or proven nonexecution.
No automatic inverse action or remote rollback is promised.

Retarget reads child state/head/base before edit and after. It accepts only the
planned base; unchanged head is mandatory, otherwise `head-changed` with truthful
partial result. Base/permission races refuse. Any base edit invalidates prior
Ready receipt: re-read head/base, required CI/rules and rebaseability, independent
review and scope guards. Checks may not rerun after `edited`; old-base evidence
is not automatically reused. 043 owns any ordering with owner landing/deletion.

## Results, limits and exits

Bounds/identity grammars are in [data-model.md](../data-model.md#common-identities-and-bounds).
They are operation-wide, including retries/resamples and native subprocesses:
16 actions, 2 open levels, 200 identity commits, 50 requests, 10 pages/collection,
1000 records, 1 MiB raw/normalized/result, 64 KiB body/input, 60 s total; shorter
per-request/process bounds apply. No budget resets per source/action or fanout
worker. Cancellation starts no next action; an in-flight effect remains completed,
failed or unknown as observed, never declared absent merely because stopped.

One final object from the same human result contains schema_version/kind,
operation, plan id, repository/task/mode summary, relationships, selected actions,
source completeness, blockers, per-action outcomes, boundary, handoff and error.
Session ids/tokens/environment dumps/raw API errors are absent. Native ids remain
separate. Terminal controls/bidi/newlines in untrusted display fields are escaped;
JSON uses correct JSON escaping and safe bounded text, never shell interpolation.
No published schema changes; candidate fixtures remain UNPUBLISHED until native
implementation/conformance and explicit publication review.

| Exit | Future 016 meaning |
| --- | --- |
| 0 | Complete plan observation (blocked effects may be described), or every authorized selected apply effect completed/proven already-completed |
| 1 | Refused, incomplete, partial, cancelled, failed, unknown outcome, output/durability error or unavailable requested apply capability |
| 2 | Invalid usage/flag combination/plan-action selection before effects |

Stable proposed refusal reasons include `ownership-unknown`, `not-owned`,
`attribution-mismatch`, `attribution-unknown`, `plan-stale`, `authorization-missing`,
`permission-unknown`, `capability-unavailable`, `remote-changed`, `head-changed`,
`identity-mismatch`, `identity-config-foreign`, `signature-policy-unknown`,
`review-missing`, `checks-unknown`, `checks-not-green`, `rebaseable-unknown`,
`scope-not-accepted`, `native-rewrite-pending`, `stack-too-deep`,
`outcome-unknown`, `bound-exceeded`, `cancelled`, `journal-failed`.
Codes are command-local namespaces, not undocumented aliases of 012/015 codes.
The historical hidden 015 v1 incomplete exit 3 remains unchanged (OQ-EXIT).

An illustrative UNPUBLISHED empty-selection result (not an executable schema):

```json
{
  "schema_version": 1,
  "kind": "forge-effect-plan",
  "operation": "plan",
  "plan_id": null,
  "saved": false,
  "repository": null,
  "actions": [],
  "blockers": [],
  "boundary": {"network_requested": false, "network_attempted": false, "complete": true},
  "error": null
}
```

This help/empty-selection shape has no resolved repository and no stored plan id;
nonempty persisted plans require their full binding. Valid/invalid future fixtures
must cover both forms without weakening nonempty-action invariants.
