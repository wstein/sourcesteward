# Contract: Publication, Ready and Owner Handoff Receipts

**UNPUBLISHED proposal**; these are typed evidence requirements, not a new ledger,
review provider, signing service or proof that any source already emits them.
Trusted provenance/serialization remain OQ-POLICY. A caller-provided SHA without
accepted source evidence is not an agent CLEAR receipt. Spec 043's separate
human `--reviewed SHA` statement remains its own contract.

## Actual candidate binding

Spec 015 may supply a local apply receipt after its actual candidate exists.
It must name backend/repository/task, full old parent/old head/new parent/actual
new head, exact affected old/new ranges, pairing/own-diff evidence, local check
results, native capabilities, actual identity and signing evidence. A clean 014
prediction or hidden-v1 quarantine SHA never fills actual-new-head. 016 rechecks
the candidate and every publication-specific guard; an apply receipt is not
remote authorization or current remote/default/permission evidence.

The merged 014 design's actual full-H attribute pin, private context and repeated
H/N guards qualify any supplied preflight; an unpinned or contaminated preview
cannot satisfy its profile. T033 gates its future replay implementation, while
issue #241 remains a separate existing-code follow-up. Neither merged design nor
predicted-clean evidence substitutes for actual 015 candidate verification.
Native jj conflict evolution/aggregate policy remains separately capability-gated.
013 independent safety-copy receipts and native operation samples preserve their
own retention/concurrency limits, including no jj operation-head CAS and no
first-slice invoker working-copy commit change; they never authorize remote undo.

A candidate is created first and only then independently reviewed if required.
Reviewing an old head or a prediction does not review the actual candidate.
Record review subject SHA, independent reviewer type/tier, CLEAR/other verdict,
trusted evidence locator/digest and observed source. No session link/id enters
commits, branch names, public output, PR text or a receipt.

## Before-push review matrix

Recompute `publication_range_change_class` from the UNION OF PER-COMMIT PATHS
across `<observed remote default branch>..<full actual new head>`, including every
still-open parent commit. Inspect each commit's changed paths; a net tree diff,
child-only/moved range or caller-supplied path list cannot establish docs-only.
Code added by one parent commit and removed by a later commit still makes this
range code, even if the final tree diff cancels it. Missing/unreadable parent
commits or paths make the publication-range class code (and preserve any separate
incomplete-evidence refusal); never infer docs-only from missing evidence.

The 015-to-016 adapter reads source `review_gate.change_class`, defined by
[015 ReviewGateInput](../../015-local-restacking/data-model.md#proposed-local-apply-entities-unpublished)
and the [015 apply receipt contract](../../015-local-restacking/contracts/restack-apply.md#proposed-result-shape-and-exit-status).
It assigns that source value to the **016-only** `moved_scope_change_class`:
`moved_scope_change_class := source.review_gate.change_class`. 015 emits no field
named `moved_scope_change_class`, and its source `change_class` is never accepted
as 016's effective class.

Before that assignment, validate the accepted 015 producer/command, actual-apply
kind and schema version (the current UNPUBLISHED sketch proposes `restack-apply`
version 2, pending 015 OQ-1), repository/backend/task, complete actual moved-range
scope and matching parent/head identities. In particular `review_gate.head_sha`,
`target.new_head` and the selected actual candidate must be the same full SHA.
Require the source's accepted class value and completeness; missing/malformed or
unrecognized provenance cannot establish docs-only and remains unknown/code with
any separate input refusal. A top-level class, fabricated source alias, hidden-v1
prediction or wrong-head receipt cannot substitute. Source serialization/admission
is still OQ-POLICY; this mapping neither publishes a schema nor accepts a version.
Keep this validated moved-scope alias separate from the recomputed publication-
range class. Effective `change_class` is `code` if EITHER class is
code or unknown; it is `docs-only` only when BOTH are completely proved docs-only.
`docs-only` means only specification/documentation prose, no code, tests, scripts
or generated files. Thus a docs-only child above a still-open code parent needs
the code before-push gate. `pure_rebase` is true
only with the same commit count and every range-diff pairing row `=`; changed,
missing, extra or unknown rows make it false. Preserve complete equality evidence
rather than claiming tree equality alone is a pure rebase.

| Actual branch change | Before-push requirements |
| --- | --- |
| Rewritten docs-only, pure rebase | Green applicable local checks and complete equal range-diff; no independent review before push |
| Rewritten docs-only, other rewrite | Green applicable local checks; no independent review before push |
| Rewritten code/tests/scripts/generated, pure equal rebase | Green applicable checks, equal range-diff and independent CLEAR at exact actual new SHA before push; middle reviewer tier allowed |
| Rewritten code/tests/scripts/generated, other rewrite or unknown class | Green applicable checks and independent CLEAR at exact actual new SHA before push under governing independent review policy |
| New non-rewritten topic publication | Applicable local checks and governing independent review policy; no rewrite exception invented |

The workspace's 2026-10-17 exception permits a later-round wording-only code-branch
fix after exact-SHA CLEAR to use the project's documented docs/lint check plus
`git diff --check`, only when no rule, behavior or contract changes. It excludes
first review, rebase and restack; no documented check means no exception. The
**initial 016 capability does not admit this exception**: every effective-code
push still follows the code row above, including a later wording-only fix. This
is an explicit stricter capability choice because this first receipt protocol
has no admitted proof of that narrow wording-only predicate. It does not change
the broader owner manual-workflow authorization; any future receipt kind needs
separate OQ-POLICY acceptance and those exclusions.

The docs-only rule is solely a before-push exception. Ready always requires its
own exact-current-head independent CLEAR and green required CI. No prediction,
local apply, range-diff equality, semantic preflight or code author's self-review
is independent review. 015's requirement for equal own diffs/range-diff for its
supported apply does not prohibit a different separately reviewed rewrite source;
016 still classifies its actual full range and applies the correct policy.

## Trusted owner identity and signatures

Immediately before every push, read the remote default ref/full commit in this
run. Inspect every actual author AND committer in
`<observed remote default branch>..<full actual new head>`, including all still-open
parent commits. Missing objects, more than 200 commits or unknown range refuses;
no implicit fetch. Compare names/emails to exactly the trusted owner identity,
never a repeatable caller allowlist or repository/effective-config expectation.
The proposed trusted-source selection stays OQ-POLICY; lack of a source blocks.

Read and report bounded, escaped `git config --show-origin --get-regexp '^user\.'`
origin evidence; no foreign `user.name` or `user.email` is allowed. Check local,
worktree and global origins, and distinguish corroboration from identity policy.
Do not write, repair or unset configuration. Report owner-only metadata required
for this check, not arbitrary configuration/environment values or secret-looking
keys. Source code must pin allowed keys before output. GitHub co-author trailers
never replace actual commit fields.

Actual signing policy must be explicit. 015 proposes unsigned actual candidates
using `--no-gpg-sign`, with signing program neutralized and per-signed-source
`signature-dropped` evidence even if range-diff is `=`. No signing helper may run.
Until its signed-source/required-signature decision is accepted, publication
blocks signed input, dropped signatures or unknown/unmet remote requirements.
Spec 043 OQ6 remains separate; it is not silently answered by this gate.

## Mode, ownership and attribution

Spec 041 mode evidence is an accident guard, not a security boundary. Agent
ownership comes from verified trusted task evidence, never branch prefixes,
caller labels, visibility or author hints. Human mode does not empower an agent
session to evade policy. Agent writes remain own-topic/own-PR/own-body only.

Apply real Spec 042 D5 before every push and Ready:

| Task/run condition | D5 result |
| --- | --- |
| Human run | Not blocked by D5; other policy gates remain |
| Task absent, or record has no session id | Not gated by D5; separate ownership still required |
| Recorded and current session equal | D5 passes; separate ownership still required |
| Recorded/current session differ | `attribution-mismatch` |
| Recorded session exists, current absent | `attribution-unknown` |
| Task record unreadable in agent run | Refuse; never treat unreadable as absent |

Codex source is unmeasured and stays absent, never guessed. Existing state-only
origin semantics remain unchanged; no session id is copied into a plan/journal
or displayed refusal. Remote action records carry task binding and gate result,
not private session values. Missing session is not a new blanket refusal when
D5 explicitly leaves a no-session task ungated.

## Ready gate

Ready is individually selected/authorized, never automatic after push, PR create,
retarget or link. Its fresh receipt requires all of:

- own PR/task and mode/attribution guards;
- exact current PR head and relevant base, independent CLEAR on that exact head;
- all required contexts green at that head, with complete trusted required-check
  source evidence; absence/unreadability is not an empty successful set;
- repository merge-method constraint `rebaseable == true`;
- explicitly accepted PR scope and no unmet acceptance within that scope.

Required checks are read through bounded typed hosting evidence, with required
context sources/union and current-head matching made explicit. Spec 025 wait
explanations, `mergeable_state`, lack of conflicts or a readable empty optional
collection are not Ready. Unknown review/CI/rebaseability/scope blocks. A change
to head invalidates exact-head clearance; a base edit requires a fresh complete
Ready evaluation and must not reuse old-base mergeability automatically.

Unfinished follow-up work outside an accepted PR's scope does not block its
Ready state. In-scope missing acceptance does. Task completion, merge and live
operation remain separate gates. Draft output names its blockers precisely.

## Owner resync data

After an actual rewritten push, include exact branch/ref, full old pushed SHA,
full new SHA and these validated non-executed owner choices:

```text
git fetch --prune
# Owner has local commits:
git rebase --onto origin/TOPIC FULL_OLD_PUSHED_SHA TOPIC
# Owner has no local commits; DESTRUCTIVE for local-only work/uncommitted changes:
git switch TOPIC && git reset --hard origin/TOPIC
```

This is the workspace owner-resync reference form. The agent-restack overrides
`-c rebase.updateRefs=false -c rerere.enabled=false` may be displayed as an
explicitly labelled stricter owner variant, not a required owner-resync rule.
`origin` is illustrative; use the actual validated selected remote and ref. Never
substitute old local head for full old pushed SHA. The destructive path is chosen
by the owner, never by 016. No pull/reset/rebase/checkout is run in the owner's
checkout, and no gitlink is changed. If no owner action is needed, say so based
on actual branch context rather than assume it.

## Conditional owner landing data

016 renders both conditions and never invokes 043 apply or an equivalent raw
merge/default push/deletion. Native stack merge is excluded, too.

| Setting/evidence | Data-only sequence and limit |
| --- | --- |
| `delete_branch_on_merge` readable and off | Subject to 043 shape/rules/identity/check gates: owner lands bottom → `gh pr edit CHILD --base DEFAULT` with head/base verification → delete bottom only after every child retargeted and 043 deletion gates → owner lands child. This is the measured order. |
| On or unreadable | 043 FR-031/034 proposes retarget → re-read/re-evaluate all preconditions → owner land. This is unmeasured and may run only for a measured allowlisted shape under 043 decision 9; unreadable shape always fails closed. No unconditional land-first advice. |

The measured native-stack automatic rewrite/retarget had deletion on; it is a
separate observation, not proof that either owner sequence works on real
protected repositories. 043 alone owns the landing and optional deletion policy;
this receipt neither reopens its settled conditional order nor removes its
unmeasured/ruleset gates. Printed owner commands are data with limitations, not
an agent action or authorization.
