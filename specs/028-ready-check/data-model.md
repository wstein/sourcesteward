# Data model: Ready evaluation

The schemas [lock](contracts/lock.schema.json), [policy](contracts/policy.schema.json) and [report](contracts/report.schema.json) describe unpublished inputs. Normative cross-field rules are in their contracts. Examples use fictitious IDs and hashes. Production parsing must reject duplicate keys before schema validation.

## Pure input and output

`Evaluate(policy, sample)` takes immutable values and an explicit observation timestamp. It reads no clock, files, processes, environment or network. Inputs contain authenticated source metadata and completeness states; unverified raw strings do not become trusted identities by conversion.

| Result field | Shape and invariant |
| --- | --- |
| schema_version / kind | `1` / `ready-check` |
| decision | eligible, blocked, indeterminate or error |
| observed_at | RFC3339 UTC supplied by the operation |
| backend / identity | Existing native Git/jj identity, nullable only on early failure |
| repository | Host, immutable repository ID and full name |
| pull_request | Number, head/base SHA/ref, nullable head repository ID, base repository ID, state/merged/draft |
| policy | ID/version/commit/SHA-256/generation, complete active-lock SHA-256 including transition/null, checker source/artifact digest, caller-supplied or deployed-verified authority |
| components | review, ci, constraints, deployment; each has state, complete, reasons and evidence |
| review_evidence | Complete associated-open-PR set and singleton proof; selected status/comment/report IDs and digests, computed tier, configured model/effort, declared independence, runtime_attestation=unavailable |
| github_reported | Nullable raw mergeable/rebaseable and bounded mergeable_state; only explicit predicates use them |
| sources | Fixed source names, outcome, request count, observation identities and safe failure classification |
| boundary | Complete flag, network attempted, budgets/usage, sampled-not-transactional limitations |
| diagnostics | Stable code and fixed escaped human message |

A component reason has code, bounded subject and source references. Sources refer to fixed reader IDs/field paths, not arbitrary URLs or raw response bodies. No report body, token, private identity or transcript is copied into the result. Emit only the public metadata needed to explain the decision.

Evaluation order is deterministic: input validity; identity/policy bindings; review authentication/scope/tier/round/findings; CI; effective merge constraints; deployment; final reduction. List reasons in that order and sort repeated subjects by UTF-8 bytes. Global operational error wins; otherwise any known failed requirement gives blocked, otherwise unknown gives indeterminate, otherwise eligible. A blocked result can still retain unknown components. Review is not-required never: full review is mandatory under this feature's v1 policy.

## Stable reason families

| Family | Codes |
| --- | --- |
| Inputs | policy-invalid, lock-invalid, policy-source-untrusted, policy-digest-mismatch, policy-transition-required, activation-mismatch |
| Identity | repository-mismatch, pull-request-closed, review-binding-mismatch, sample-changed, shared-head-ambiguous, association-incomplete |
| Review | review-missing, review-issuer-untrusted, report-binding-mismatch, report-malformed, report-history-invalid, report-metadata-missing, review-tier-insufficient, independence-unverifiable, review-not-clear |
| CI | ci-missing, ci-pending, ci-failed, ci-source-unknown, ci-head-mismatch, ci-test-merge-unsupported |
| Constraints | policy-incomplete, constraint-unsupported, merge-method-disallowed, merge-conflict, rebase-unavailable, base-behind |
| Deployment | deployment-unverified, deployment-pending, deployment-failed, deployment-stale |
| Collection | evidence-incomplete, source-denied, source-malformed, source-limit, source-failed, hosting-canceled, output-limit |

Subsequent implementations may add reason codes compatibly; consumers must preserve unknown codes and never interpret one as success. Input schema versions remain closed. Unknown required output fields or schema versions prevent publisher execution; ordinary human CLI readers may display the preserved diagnostics.

## State boundaries

A complete review projection can coexist with `ci=unknown` only when that uncertainty does not affect review identity/policy/scope/unique-PR association and global collection completed within bounds. If the final sample changes, review completeness is revoked. The publisher accepts only this complete review projection with all binding checks; it is not permitted to accept an isolated `verdict=clear` string.

An undeployed policy uses `deployment=not-required` and labels final eligibility as caller-policy eligibility. An installed policy requires the exact authenticated active generation and current published check envelope at head/base/policy/full-lock digest with singleton association. A check's mere success, App display name or file path is insufficient. Installed profiles without accepted active-generation readers are unsupported.

## Contract fixtures

The policy/report/lock examples and two activation counterexamples under `contracts/examples/` are shape fixtures only. They intentionally use fictitious repository/account IDs and placeholder hashes, so they are not a verified cross-document sample or usable trust configuration. Implementation adds independently valid cross-bound examples, parser negatives (including duplicate keys), semantic negatives and complete result goldens before behavior is implemented. Passing these design-schema examples establishes only structural coherence. The activation pair differs only in transition; both are shape-valid, but full authenticated-byte comparison must reject substitution. [Counterexample vectors](contracts/counterexamples.md) specify required lifecycle/association outcomes without claiming implementation acceptance.
