# Contract: trusted review-evidence publication

## Projection and effects

The publisher consumes an already approved immutable `stw` artifact, supplies its approved lock and explicit repository/PR, and validates the complete versioned Ready result. It publishes only a complete passing `review` component. The CLI's exit 1 caused solely by another component does not prevent review publication, but invalid/truncated JSON, operational error, canceled/changed/incomplete sample or unknown result version does. No publisher-specific shortcut may turn unknown review into success.

The publisher writes only create/update operations for its own `review-evidence` check runs at the observed PR head. It never changes PR state, comments/review statuses, branch rules, repository settings, refs or merges. Expected App/native-workflow source identity is part of the selected deployment profile. Its own workflow job result is not necessarily the required published check.

The shared check key is `(repository_id, head_sha, context, app_id)`, not PR number. V1 supports publication only when a complete fresh association read proves exactly one open PR with that head in the repository. The PR number in output is diagnostic binding for CLI readers; GitHub does not use it to isolate a commit check.

Check output.summary is exactly one JSON object (no Markdown wrapper, at most 64 KiB), containing a compact versioned envelope: schema_version, repository ID, PR number, head/base SHA, policy digest/generation, complete active-lock SHA-256, checker source/artifact digest, review verdict and reasons, reviewed report IDs/digests, sampled_at. Check name is exactly `review-evidence`; check head and App identity must match the envelope/profile. CLI verifies these public bindings from the authenticated check record. No automatic expiry or scheduled refresh is introduced; changed bindings require a fresh evaluation, with the event-delivery limitations below.

## Deployment alternatives — no option selected

| Profile | Native trust/control needed | Measured availability | Acceptance obligation and owner operations |
| --- | --- | --- | --- |
| Organization required workflow | Platform binds a required workflow to a trusted source repository/ref; immutable checker/policy and protected source changes | GitHub documents organization/enterprise configuration. Target is a User-owned repository; not a drop-in capability established here. | Prove actual plan/ownership support, exact workflow/source binding, PR tamper rejection and report events. Owner changes/approves applicable organization rules and trusted source. No assumed repository transfer. |
| Human-mediated protected source | Required independent owner/code-owner approval of all credential/workflow/evaluator roots plus the project's review evidence; no autonomous source-authenticity claim from check name alone | GitHub approval/CODEOWNERS mechanisms exist; their suitability/configuration for this repository is unmeasured. | Prove PR-added workflows cannot bypass the independent approval gate or obtain privileged credentials. Owner configures approval protections and accepts ongoing human involvement. v1 cannot prove GitHub code-owner eligibility, so CLI eligibility remains unknown when that rule is effective until a separately reviewed proof extension exists. |
| Suitable existing App | A distinct accepted publisher identity whose credentials/executor cannot be controlled by the PR, plus owner-controlled baseline channel | No App inventory or suitable existing installation was established. Seeing GitHub Actions checks does not prove no other App exists. | Inspect a candidate App's permissions, code provenance, credential access and installation scope. Reuse only if every acceptance criterion passes; owner authorizes installation/configuration changes. |
| New dedicated App | Same trust obligations as existing App; isolated credential environment and immutable executor/policy | Technically plausible; no App/secret/environment has been created or approved. | Owner registers/installs App, grants minimal permissions, provisions isolated credentials/environment/baseline and later required-check setting. New infrastructure/credential choice requires explicit approval. No external daemon is required for the proposed Actions-hosted wrapper. |

A context name plus `integration_id` authenticates an App, not the workflow that emitted it. The generic GitHub Actions App can run PR-defined workflows; default read-only token settings alone do not establish immutable workflow identity. Do not advertise an unprotected generic Actions context as equivalent to a trusted required workflow. Conversely, this design does not claim a new App is universally necessary; inspect native/human-mediated/reusable-App alternatives before asking the owner for infrastructure.

## Proposed App profile proof obligations

A trusted default-branch wrapper uses an immutable approved executor and policy. Store an App private key only in a dedicated protected environment restricted to the approved default branch; ordinary PR and topic-push jobs cannot read it. Do not assume environment naming enforces this: demonstrate event/ref gating and credential nonavailability with hostile fixtures/authorized hosting acceptance. Deny untrusted checkout, artifact/cache restore, local action or script execution in privileged jobs. Pin external actions by full SHA and include them in the trusted dependency inventory.

The App needs checks write, PR/contents/status/metadata read and only the additional read privileges demonstrated necessary for effective protection and activation observations. No contents/PR/admin write. A credential that can mutate rules, push or merge is outside the profile. Existing suitable Apps are judged by the same limits. The CLI still uses its caller's read credentials; it never receives App private keys.

Credential access roots, all workflow additions/changes and policy loading/executor sources are privileged transition paths under the old baseline. An owner-approved transition names exact old/new digests and candidate before these changes can pass. The activation record is outside PR control. Environment-variable APIs alone do not establish owner-only writes; the actual role/token access must be proven isolated. If author credentials can update the channel, this proposed channel is rejected and a separately reviewed owner-controlled alternative is necessary. Default-branch code is not automatically trusted solely because it is on default; the accepted transition chain and protected source form the trust proof. A weakened root cannot approve itself.

This is a supported-profile proposal, not a claim that current settings satisfy it. Before rollout, independently test the full threat matrix below. If the platform cannot establish these controls, do not deploy the profile as an enforced gate.

## Blocking lifecycle and observed invalidation

Every relevant reevaluation first establishes a blocking state for the shared key, including when a previous evaluation succeeded. Under the single-writer lock, read all owned matching check runs, change previous successes to `completed/failure`, and read back the invalidation before proceeding. Create one new `in_progress` evaluation only after prior success invalidation is confirmed; if no prior owned check exists, create `in_progress` directly. Never rely on a new per-PR check or a different external_id to isolate an old successful shared check.

| Observation / phase | Required remote consequence |
| --- | --- |
| Evaluation begins, or relevant invalidation event arrives | Revoke previous owned successes for the shared key, confirm readback, then expose in_progress |
| Complete passing review, exactly one stable open PR, complete authenticated active lock, successful final resamples | Complete the active check as success with all bindings; read back exact ID/head/App/envelope |
| Report edit/deletion, newly failed/pending review status, invalid transition, unsupported/missing proof, or evaluator read/parse error | Complete the active check as failure with fail/unknown/error reason; ensure no previous owned success remains |
| Multiple open PRs share the head, association completeness unavailable, or association set changes | Invalidate the shared key and complete failure (`shared-head-ambiguous` / `association-incomplete` / `sample-changed`); no per-PR success |
| Cancellation before blocking state confirmed | Attempt bounded revocation while the write channel remains usable; otherwise enforcement-unknown with affected known keys/IDs |
| Cancellation after blocking state established | Leave the confirmed blocking state or complete failure; never restore previous success |
| Head or base changes before success | Fail the old attempt/key; route the new known key as a separate bounded evaluation |
| Invalidation, pending-write or failure-write fails | No success write/fallback; exit nonzero with `enforcement-unknown` and affected known keys/check IDs; retain evidence of any unconfirmed remote state |
| Success write/readback fails or becomes uncertain | Use the reserved failure-write/readback budget to revoke that known check; if unconfirmed, exit enforcement-unknown, never report successful enforcement |

`neutral` and `skipped` are forbidden for a blocking outcome because GitHub may accept them as passing required checks. Unknown/error is represented as check conclusion `failure`, while the envelope retains the distinct evaluation reason. Check removal is not a substitute for confirmed blocking state. A failed evaluation never merely leaves a previously observed success untouched when the write channel remains usable.

Writes are bounded: at most 1,000 known matching owned runs, at most 20 mutation requests and 20 readback requests per invocation, all within a 60-second publisher deadline (the CLI retains its own 30-second budget). Reserve one mutation and one readback for revocation before attempting success. The publisher has at most 120 total HTTP requests including the CLI's maximum 80; inherited response/page limits apply to its additional reads. Exhaustion stops with enforcement-unknown, not a claim that revocation succeeded. No write retries/polling. If remote failure prevents revocation, GitHub may still expose an old success: surface that concrete enforcement gap for owner action. This limitation is different from a successful observed invalidation. No contract can promise that an unreachable hosting service accepted a write.

## Events, shared-key concurrency and races

Observe PR creation/opening, synchronization, reopen, close, retarget/base edits, relevant review-status changes, selected report comment creation/edit/deletion, relevant base/activation changes and explicit authorized rerun requests. A newly opened/reopened/retargeted second PR at an already successful head is an invalidation event even when its author has not submitted a report. Ignore publisher self-generated check events and unrelated data; do not wait on CI events because review has no CI dependency. If actual platform events omit a required invalidation, record the deployment acceptance gap rather than claiming immediate revocation.

For every invocation, including events already carrying a PR number, prove the complete set of open PRs in the repository and filter exact `head.sha`. Use fixed `GET repos/O/R/pulls?state=open&per_page=100&page=P`, bounded to ten pages / 1,000 rows with a short final page. Require valid ID/number/state/head SHA/base identity on every row; missing metadata, duplicate IDs, overflow or incomplete pagination gives unknown association, never a singleton. Do not use the commit-associated-PR endpoint as proof of all open same-head PRs. Retargeting does not remove a PR from this set; Draft PRs count. Exactly one associated open PR must equal the evaluated PR. Zero or multiple cannot pass v1 publication. No aggregate multi-PR review is implemented.

Serialize by the actual repository/head/context/App key using the accepted controller. Different PRs on one head must acquire the same lock. Use non-canceling serialization for the write phase and never overlap a canceled old writer with a replacement. For events moving a PR between heads, revoke the known previous key and assess the new key; when two keys are needed, acquire them in lexical key order to avoid cycles. The event supplies routing identifiers only, which are checked against trusted repository/event metadata and API observations; no arbitrary URL is followed.

Dedupe only completed matching evaluations whose report, full active-lock digest and complete open-PR association set remain current. Immediately before success, repeat complete association enumeration and PR/head/base/full activation reads. Compare the full association set, not just the selected PR. Immediately after success readback, resample association/head/base/full activation once more; any observed drift revokes that shared success with failure. The fixed before/after passes are bounded resamples, not polling for a favorable state. The CLI uses the same association predicate and final resampling in its review projection; the publisher's final write-adjacent reads close only its additional orchestration gap.

Maintain evaluation sequence and check ID under the shared single-writer lock. An obsolete attempt cannot publish or invalidate a newer attempt after surrendering ownership. A newer invalidation event is evaluated against current evidence, not replayed as an old success payload. Record the previously known key for lifecycle cleanup; if it cannot be recovered, surface incomplete invalidation rather than claim all old successes were revoked. Repo-wide base/activation events fan out to at most 20 known keys; larger affected sets are a named incomplete-invalidation condition requiring owner action, not silently skipped work.

GitHub offers no atomic check-write conditioned on PR association/base/policy. A second PR can appear after the final resample, and a relevant event can be lost. These residual races are disclosed; once a collision or other invalidation is observed, the blocking lifecycle is mandatory. Strict freshness does not turn a commit-scoped check into a PR-scoped transaction. Deployment must test creation/reopen/retarget and same-head concurrency; JSON PR numbers are not GitHub enforcement.

No schedule, polling service or event bridge is created here. Actual event/ref restrictions, source metadata and acceptance of the fixed API shapes must be established for the selected profile using the existing `2022-11-28` API pin. Missing trigger/access capability blocks that profile's enforcement claim.

## Security acceptance and activation

| Attack or gap | Required measured result |
| --- | --- |
| Same-name check from another App / generic Actions workflow | Cannot satisfy configured expected source/profile |
| PR adds workflow requesting the credential environment | No credential access before approved integration/activation |
| PR weakens policy, classifier, wrapper or credential-loading dependency | Old baseline blocks without exact owner transition approval |
| Public report uses issuer's display name but wrong authenticated ID | Review fails |
| Untrusted artifact/cache contains code or report parser override | Privileged executor never consumes it as executable code |
| Duplicate/out-of-order/self events and concurrent head changes | Shared-key writer isolation; no loop/stale overwrite; observed drift produces confirmed blocking failure |
| Previous success followed by report deletion/edit, failed review, bad transition or API read error | Previous success revoked; check failure preserves distinct unknown/error reason; write failure reports enforcement-unknown |
| Second open/reopened/retargeted PR with the same head | Shared check invalidated; complete association predicate fails for both PRs |
| Only lock.transition differs (including null versus object) | Complete activation authentication rejects it before review can pass |
| Missing event, hidden required policy or unsupported activation read | Explicit gap; no false completeness or enforcement claim |
| Owner approval/activation missing or partial | No active gate or positive deployed eligibility |

After a profile exists and has a real passing exact-head check, prepare a minimal current-versus-proposed settings diff. Preserve all existing rules; add the exact `review-evidence` requirement with the accepted source binding. Include observed check ID/head/App or required-workflow identity, successful security acceptance and rollback operation. Do not infer permission to apply it. The owner decides that concrete operation. Neither a green check nor Ready UI state authorizes merge, builds, publication or activation.
