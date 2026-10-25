# Proposed contract: local restack apply

**Status (2026-10-25): UNPUBLISHED, unimplemented.** All spellings, fields and JSON
examples in this document are proposals requiring owner decisions OQ-1 to OQ-3
and OQ-6 in [the spec](../spec.md#needs-owner-decision). This is a separate future
protocol; [hidden plan v1](restack-plan.md) and its frozen schema stay unchanged.
No JSON file or dependency is added by this amendment.

## Selection and authorization

Proposed syntax, shown as data rather than runnable examples:

```text
stw restack plan [PATH] --contract 2 --branch REF --old-parent OID --new-parent OID
  --expect-head OID --identity "NAME <EMAIL>" --committer "NAME <EMAIL>"
  --task TASK --owner OWNER --state-dir DIR --save [--json]
stw restack apply --plan PLAN_ID --state-dir DIR --authorize [--json]
```

`--contract 2`, `--save`, `--committer`, `--task`, `--owner`, `--state-dir` and
`--authorize` are **proposed**, not current restack flags. Plain v1 planning
keeps its existing options and never stores an executable plan. The future
selector is not chosen until OQ-1 is settled. Unlike historical v1 D9 prediction
flags, actual-apply `--identity`/`--committer` are assertions that must match the
trusted owner identity (OQ-2/OQ-3); repeatable caller values cannot widen it. Omitting an apply plan id or local
authorization produces a usage error; no prompt or effect is substituted.
A complete plan is evidence; authorization names only that plan's local effects.
`--authorize` deliberately reuses Specs 011/029/041 vocabulary with
`--plan ID --state-dir DIR`; the selected plan supplies the local effect scope,
while Spec 016 owns its separate remote plan/action authorization.

The private store must be caller-owned, nonsymlinked and protected against path
replacement; validate native file identity and complete task origin under Specs
010/042, never authenticate by an owner label or branch prefix. OQ-2 must settle
the trusted ref-to-task association. Agent scope is only its own topic refs;
`main`, the configured default/protected refs, foreign and unproven refs refuse.

## Git replay and commit point

The proposed first apply capability is one unoccupied branch. A checked-out
branch anywhere, even the author's task worktree, refuses `branch-in-use`.
That restriction is an OQ-6 recommendation, not a settled permission to change
the owner's workflow. A detached caller may inspect but is never switched.

1. Load and validate the immutable plan, digest, repository binding, exact
   source/destination ids, allowed effects, ownership/attribution and version.
   Revalidate config/hook hazards, protections, common directory, all worktree
   registrations, operation state and selected refs. No fetch or derived base.
2. Replay exactly the ordered nonempty `old_parent..old_head` onto `new_parent`
   in an owned object quarantine using D7 `merge-tree`/`commit-tree` plumbing.
   The inert human reference `git -c rebase.updateRefs=false -c rerere.enabled=false rebase --onto NEW_PARENT OLD_PARENT BRANCH`
   describes semantics only; stw never invokes it. No three-dot range, update of
   other refs, implicit descendant replay, hook, helper or config write. Plumbing
   never reads/writes the original `rr-cache` and never moves any other ref, the
   equivalents of disabling rerere and rebase.updateRefs in the human reference.
3. Materialize actual candidate commits with the proposed explicit committer
   policy of OQ-3, keeping each original author name/email/date. The fixed
   `stw-restack-plan <restack-plan@invalid>` committer of v1 predictions is never
   persisted as the actual rewritten history. Candidates are never signed:
   pass explicit `--no-gpg-sign`, neutralize signing-program/config/environment
   paths and reject any attempt to invoke a signing executable. Detect source
   `gpgsig`/`gpgsig-sha256` headers as bounded metadata only, and report per-commit
   `signature-dropped` with old/new ids whenever a signed source becomes unsigned,
   even when range-diff pairs `=`. This is disclosed loss, not signature validity
   proof. OQ-8 must decide refusal versus disclosed loss for signed input or a
   destination requiring signatures; until accepted, that affected capability
   refuses rather than silently dropping or auto-signing. Unknown required-
   signature policy cannot authorize publication. Spec 043 OQ6 remains separate.
   Record the actual candidate head,
   tree and one-to-one old/new mapping. These ids are now meaningful identities.
4. Require complete range-diff for `old_parent..old_head` against
   `new_parent..candidate_head`, every row `=`, exact moved count, no unpaired,
   dropped, added or empty commit, and equal own diffs. Require parent-integration
   proof from v1's qualified cherry/tree method; unsupported proof refuses.
   Read author AND committer from every actual candidate commit and compare with
   exactly the trusted owner identity under OQ-2/OQ-3; unknown/fail or any
   caller attempt to extend that set refuses. Neither clean
   replay nor equality proves semantic correctness or authorizes publication.
5. Prepare the typed [journal and recovery root](recovery.md), storing the full
   known candidate SHA durably before the target move. Verified candidate objects
   may be promoted to the repository object store after the durable intent; only
   selected candidate/recovery objects and explicitly declared metadata are
   allowed. A failed promotion may leave unreachable objects, recorded as such;
   no automatic GC or unjournaled cleanup runs.
6. Revalidate all guards immediately before a no-dereference, expected-old-value
   update of the one exact `refs/heads/...` target from `old_head` to the actual
   `candidate_head`. Use fixed plumbing, no configured reference-transaction
   hook execution: an installed relevant hook must cause refusal before the
   write rather than be silently skipped. Journal completion and resample the
   ref. A race is refusal or partial/unknown outcome, never an automatic reset.
7. Emit actual identities/evidence/effects and `review_gate.state: pending-policy-check`
   bound to the actual new SHA, with `change_class` (`docs-only` or `code`) and
   `pure_rebase` (all rows `=` and unchanged moved count). Classify actual changed
   paths; any code/test/script/generated or unclassified path means `code`.
   Spec 016 applies the AGENTS.md gate table below after replay; old-head or
   predicted-head review never counts. This apply never pushes or changes PR state.

CAS protects the selected ref value, not a whole repository transaction. The
registration/ownership/config resamples cannot exclude same-user replacement or
ABA races. Refuse observed uncertainty; retain this residual limit in every result.
No atomicity across journal, object promotion and native ref storage is claimed.

## Effects and refusal classes

| Operation | Declared effects | Never included |
| --- | --- | --- |
| Future plan with explicit save | Private immutable plan and owned quarantine during observation | Target refs/objects/index/files, network, hooks |
| Apply preparation | Private journal/owned scratch; create-only recovery root; candidate objects | Any original worktree/HEAD/index/file/config change |
| Local commit point | Exactly selected unoccupied owned topic ref and its disclosed native ref/reflog metadata | Other branches, descendant refs, remote-tracking refs, tags |
| Receipt and handoff | Bounded local evidence and inert owner text | Push, PR creation/link/retarget, Ready, merge, owner reset |

Proposed stable refusal codes: `plan-invalid`, `plan-version-unsupported`,
`plan-stale`, `ownership-unproven`, `attribution-mismatch`, `attribution-unknown`,
`protected-ref`, `branch-in-use`, `repository-hazard`, `operation-in-progress`,
`replay-conflict`, `range-diff-not-equal`, `signature-policy-unresolved`,
`signature-required`, `identity-mismatch`,
`identity-unproven`, `parent-integration-unproven`, `journal-unavailable`,
`recovery-unavailable`, `ref-changed`, `capability-unavailable`, `bound-exceeded`,
`canceled`, `output-failed`. This future set does not rename implemented v1 codes.
A refusal before the target move may still have disclosed scratch, recovery or
unreachable-object effects; completed effects must be listed, never described
as globally unchanged.

## Proposed result shape and exit status

This JSON is an illustrative fragment, **UNPUBLISHED** and not a complete valid
instance. `schema_version: 2` and `kind` below are proposed, not reserved schema
files. No consumer should use this sketch as a production schema.

```json
{
  "schema_version": 2,
  "kind": "restack-apply",
  "status": "ok",
  "plan_id": "immutable-local-plan-id",
  "run_id": "journal-run-id",
  "backend": "git",
  "target": {"ref": "refs/heads/topic", "old_head": "1111111111111111111111111111111111111111", "new_head": "2222222222222222222222222222222222222222"},
  "review_gate": {"state": "pending-policy-check", "head_sha": "2222222222222222222222222222222222222222", "change_class": "code", "pure_rebase": true},
  "remote_authorized": false,
  "actions": [{"id": "local-ref-1", "outcome": "completed"}],
  "limitations": ["semantic-breaks-not-detected", "sampled-state-not-a-transaction"]
}
```

Future protocol proposal: 0 only complete selected success or proven
`already-applied`; 1 refusal, incomplete, partial, cancellation, output or
operational failure; 2 invalid usage. Every operational result includes
completeness, reached bound, diagnostics and per-action effects. A lost output
write after the CAS returns failure but does not undo the ref; the journal is
how a retry discovers the completed effect. Hidden v1 plan keeps exit 3 for
incomplete until a separate explicit migration decision and compatibility tests.

Bounds: one selected ref, 200 moved commits, 2,000 paths per list, 1 MiB final
result, 60 seconds total including owned teardown/result preparation. Exhaustion
never erases durable effects or turns unknown into success. Refuse new work
when remaining budget cannot safely journal and reconcile a commit point.

## Spec 016 handoff

The receipt contains repository/backend identity, exact old/new head and parents,
selected ref, complete actual mapping/equality/identity evidence, ownership
binding, journal/recovery pointers and a policy-gate input for the actual new
SHA, including `change_class`, `pure_rebase` and their complete evidence. A remote plan must independently bind a full **observed remote** SHA; v1's
observed local old head is not a remote advertisement. The agent rewrite policy is:

| Change class and replay | Before push | Before marking Ready |
| --- | --- | --- |
| `docs-only`, pure rebase | Green local docs checks plus all `=` range-diff and same count; no review precedes push | Independent CLEAR for exact current head plus all required checks green |
| `docs-only`, other rewrite | Green local docs checks; no pre-push review required | Independent CLEAR for exact current head plus all required checks green |
| `code`, pure rebase | Independent exact-new-SHA CLEAR before push; middle-tier reviewer allowed with all `=`/same-count evidence; required local checks | Independent CLEAR for exact current head plus all required checks green |
| `code`, other rewrite or unknown classification/purity | Independent exact-new-SHA review before push at the applicable required tier; required local checks | Independent CLEAR for exact current head plus all required checks green |

`pure_rebase` is derived from every actual pairing row and exact old/new count;
unknown is false, never a path to a weaker tier. `change_class` is derived from
actual diffs, not a caller claim; unknown becomes `code`. OQ-2 leaves trusted
review issuer/format/freshness unresolved but cannot remove the established
docs-only exemption or middle-tier pure-rebase allowance. Review-required code
uses accepted independent CLEAR for the exact actual new SHA, never an old or
predicted head. Spec 043 `--reviewed SHA` remains the separately invoked human
owner's statement, not agent independent-review evidence or a policy override.

Before every push, Spec 016 must inspect author AND committer fields across
`OBSERVED_REMOTE_DEFAULT_FULL_SHA..ACTUAL_NEW_HEAD_FULL_SHA`, not merely
`new_parent..candidate_head`. That range includes an open parent's commits when
restacking a child onto its new head. The allowed identity is exactly the owner
from a trusted policy source; caller-supplied repeatable `--identity` values
cannot add another identity. Missing/foreign identities refuse. Also report
bounded `git config --show-origin --get-regexp '^user\.'` evidence for all
user configuration scopes, including local and global origins; any foreign
`user.name`/`user.email` refuses. Read/report only, never write or repair config.
The remote-default identity and native objects must be complete and freshly
bound by the separate 016 plan; no implicit fetch occurs inside 015.

Every selected push/link/retarget has its own authorization flag and remote plan
id, with fresh attribution/identity and the checks required by its scope. A
successful local CAS authorizes none of them. Independent exact-head CLEAR and
green required checks always gate Ready, including docs-only branches.

Retargeting is a separate 016 effect, never bundled with local apply. A future
lease push uses only `--force-with-lease=REF:OBSERVED_FULL_REMOTE_SHA` and explicit
`FULL_NEW_SHA:REF`; no `--force`, bare lease, default-branch push or borrowed task.
Agent landing/merge/tag/release/settings actions remain prohibited. Spec 043 is
the owner's separately invoked landing tool, not an agent executor here.
