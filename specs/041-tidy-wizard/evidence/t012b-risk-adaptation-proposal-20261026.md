# T012b registration-prune risk adaptation proposal

Date: 2026-10-26. Status: **PROPOSED; owner risk decision required before code**.
This document implements nothing and changes no accepted contract, schema or task state.
Baseline: main `bc82305b3841a994f4cbda06fdf2e1ff2dd6d02c`, after PRs #298 and #299.

## Existing capability and the mismatch

The merged classifier admits one exact owned, locked missing registration using native
absence, ownership and mount evidence. Its proof is `owned-missing-registration`, its
only effect is `remove-registration`, and contract 4 is explicit and read-only in the
wizard overview. Absence does not prove deletion rather than a move; recovery is not
prepared by observation. The accepted expert planner, apply, restore, quotas, ownership
and recovery rules are settled and are not reopened by this proposal.

[Spec 044](../../044-tidy-janitor-wizard/spec.md#risk-model) closes its kind/proof sets:
`risk.Evaluate` makes an unfamiliar safe kind/proof red, while a legacy stale-worktree
is always kept. Neither rule describes an admitted registration-prune row. The current
contract-4 row has 16 exact fields and no risk fields. Generic janitor owner-independent
choosability also conflicts with the stricter prune scope gate. These need an explicit
composition rule, not a forged ancestry proof or an implicit schema extension.

## Recommended owner decisions (not adopted)

1. Add a narrow registration-prune exception to Spec 044's risk model, outside the
   integration-proof ladder. A fully admitted safe row is **orange**, with a new stable
   risk reason proposed as `owned-missing-registration`. Its sentence says: "Only the
   registration would be removed; the missing checkout may have been moved."
   Retained ownership/absence refusals are **kept** with their exact existing reason;
   blocked, unknown or incomplete evidence is **red**, `not-judged`, never choosable.
   Missing or contradictory fields must not produce orange. Every unrelated unknown
   kind/proof remains subject to the current fail-closed rules. Owner alternative:
   keep all registration-prune rows structurally kept until wizard selection is ready.
2. Permit additive optional `risk` and `risk_reasons` on contract-4 registration rows,
   using Spec 044's display vocabulary, after a targeted interface amendment is accepted.
   Preserve all 16 existing fields, their meanings, native proof, row/evidence digests
   and existing version domains. Do not edit the current row/schema before acceptance.
   Recommend no `pr` field: online evidence adds nothing to this absence proof.
3. Make selection integration an explicit exception to janitor mode: only safe rows
   with exact native proof, original task-record ownership and a proven own/other scope
   can qualify; agent selection remains own only. Unknown ownership is never admitted,
   including by the typed word `unknown`. Orange prune rows never enter Enter, `a` or
   default selections. Later explicit selection keeps the accepted confirmation rules;
   the risk level alone never grants selection, plan persistence or effects.

## Exact follow-up amendments after acceptance

| Owning surface | Targeted reconciliation |
| --- | --- |
| Spec 044 Levels, ladder and Reasons | Add the narrow native-prune exception above; do not call absence integration or green strength. |
| Spec 044 Janitor mode and keys | Exclude prune rows from owner-independent choosability and green shortcuts; retain the strict prune gate. |
| Spec 044 Groups, retain table and final summary | Separate metadata-only registration groups/counts; no directory or branch count contribution. Legacy unproven stale rows stay kept. |
| Spec 044 Interfaces | Add optional row risk fields and a separate proposed `summary.registration_prunes` risk-count object; keep legacy candidate totals unchanged. |
| Spec 041 T012b amendment and examples | Reconcile historical own-scope "all" wording with green-only shortcuts; show explicit choice and no dependent branch leg. |
| Accepted [contract 4](../../011-workspace-reference-tidy/contracts/registration-prune-v4.md) overview section and schema | Amend only the additive display fields/counts above; do not change the accepted planner, manifest, apply or recovery decisions. |

The consequence sentence must say "N registrations removed (metadata only, no user
files or branches removed)." Keep it separate from the directories-deleted-for-good
summary. Display recovery/restore limits honestly; observation prepares no recovery.
No live output or revised measured counts are claimed by this proposal.

## Implementation and evidence boundary

The merged exports are `risk.Evaluate(Facts, time.Time) Assessment`, `risk.GroupRows`,
`risk.Count` and `risk.Worst`. `Assessment` carries `Level` and `Reasons`; none authorizes
an effect. Reuse the typed registration row, classifier, selection and accepted planner.
A separate registration adapter should avoid passing its proof through ancestry scoring.

After the owner's decisions, write real red tests before a read-only risk/overview slice:
exact proof/effect/scope gates, every refusal reason, missing evidence, and unchanged
legacy classification/JSON. Refusal tests assert the CLI exit and exact reason; causal
swaps must defeat each gate, unsafe default, owner relaxation and file/branch count.
Keep bounds, escaping and native jj/colocated refusal with no raw Git fallback.

Manifest-2 wizard integration remains T012b3/T012b4 work under their existing dependencies;
this proposal neither activates it nor marks it complete. The separate Spec 044 T012c
security gate applies to the later single typed-word receipt, not the already accepted
expert prune apply path. No receipt or apply-path change is proposed here. PR #317's
render/CLI files are outside this proposal. Owner acceptance of this risk composition,
independent exact-head review and required CI are the proposal's remaining gates.
