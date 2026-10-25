# Tidy v3 CLI, action and recovery contract

Status: proposed implementation contract, subordinate to the [specification](../spec.md); stacked-integration amendment awaits renewed review. Existing tidy v1/v2 invocations remain valid and offline.

## Commands and selection

```text
stw tidy plan [PATH] --contract 3 --base REF [--state-dir ABS] [--ref FULL_REF ...] [ONLINE] [--json]
stw tidy plan [PATH] --contract 3 --base REF --state-dir ABS --completed-worktree ABS --owner LABEL --remove-registration --remove-files ABS [--protect-worktree ABS ...] [ONLINE] [--json]
stw tidy plan [PATH] --contract 3 --base REF --state-dir ABS --task ID --owner LABEL --remove-registration --remove-files ABS [ONLINE] [--json]
stw tidy plan [PATH] --contract 3 --base REF [--state-dir ABS] --tracking-remote NAME --online [--ref FULL_TRACKING_REF ...] [--repo OWNER/NAME] [--host github.com] [--json]
stw tidy apply --contract 3 --plan ID --state-dir ABS --authorize [--online] [--completed-worktree ABS --owner LABEL --protect-worktree ABS ...] [--json]
ONLINE := --online --remote NAME [--repo OWNER/NAME] [--host github.com] [--pr FULL_OID=NUMBER ...]
```

`--contract` accepts exactly 3 for this path; omission selects existing v1/v2 dispatch. New flags without 3, wrong-mode combinations, duplicate selectors, negative/malformed PR numbers, hints for unselected OIDs and arbitrary native arguments are syntax errors. Retirements, local refs and tracking refs are mutually exclusive plan modes; one legacy path per plan. A hint maps a full OID to one PR and cannot override repository/base/head checks. Absent ref selection means complete bounded selection in that mode. Tracking remote is also its online remote selector; PR hint flags are invalid there. Online local refs/retirement require one selected remote and the exact chosen local base selector/OID; repo override must agree with that remote's canonical provider repository. Host is fixed github.com initially.

Existing 4,096-byte selector/path and 128-byte identifier limits apply. Paths must be canonical, no aliases; state is private and outside every protected workspace. Omitted state means read-only stateless reference planning only. No custom/stdin plans. `--remove-files` must equal the selected exact destination. `--completed-worktree` asserts completed exclusive retirement authority and that the supplied protection set contains all caller-known active worktrees; no process absence is inferred. Apply of a legacy plan must repeat exact path/owner/protection set and thereby reassert completion now. A newly known active path requires refusal/replanning, never ignoring the new protection. Owned retirement uses its original task journals, not this declaration.

Plan online selection permits only declared host reads plus optional state persistence. Apply requires `--online` exactly when the stored plan declares online effects; an old or offline plan rejects it. `--authorize` covers only exact stored local effects. Missing acknowledgment refuses before auth/network. Version mismatch or same ID in multiple plan namespaces refuses. Apply does not accept replacement base/PR/remote/ref inputs.

## Versioned operation boundary

Add sibling `workspace.PlanTidyV3(ctx, V3PlanOptions) V3Result` and `workspace.ApplyTidyV3(ctx, V3ApplyOptions) V3Result`. CLI explicit contract selection calls these typed operations; it never reads state or starts HTTP/processes. Existing `PlanTidy`/`ApplyTidy` and v1/v2 result types remain unchanged. Shared helpers take an explicit validated policy, not an ambient global switch. V3 gets its own renderer over the same result fields as JSON.

Top-level result fields/types and immutable plan/journal fields are defined in [data model](../data-model.md). Publish `schemas/v3/tidy.schema.json` only with accepted implementation, preserve all old bytes, and add the schema to source allowlist/copy tests. No Pages build is implied.

## Planning and application

Planning first validates native ownership/layout, base, complete reference/registration state and state disjointness. Workspace-used refs retain before PR lookup; provider proof can annotate but cannot override protection. Choose native ancestry when it suffices; online failure still marks an explicitly requested online observation incomplete, never silently claims that requested evidence succeeded. Retain negative candidates with exact reasons. An incomplete run returns no persistable executable plan; observations remain explicitly incomplete.

Reference apply: validate immutable record/selection/safety and current evidence; durably journal planned recovery; create and confirm exact recovery root; resample online proof and all local protections; use the native prepared transaction below; verify target absence, recovery and permitted source delta; persist outcome. Only expected recovery/deletion deltas advance the journal snapshot. Stop on the first unproven transition and report earlier effects. Native ancestry proof is rechecked against the same chosen base; provider proof follows [online contract](online.md).

Legacy retirement reuses the three-plan/eight-apply observation boundaries in [stream contract](streams.md), substituting explicit legacy provenance/physical proof for original task creation only in the legacy mode. Verify canonical same-user source/common/registration/destination/parent identities and reciprocal `.git`, `gitdir`, `commondir` pointers; inspect branch/HEAD, exact index/stored tree and complete disk manifests. Reject protected/current/source/primary/overlapping/foreign targets, locks, ongoing native operations and incomplete evidence. Every known registration remains protected while planning another action. Do not bypass native Git ownership or use the source checkout as a stand-in for a target.

Legacy native removal is fixed `git worktree remove -- DEST`, without `--force` or separate unlock. It runs only after confirmed committed-history recovery and final complete clean/protection proof, with parent write grants and read-only sibling protections identical in purpose to current retirement. Failure remains partial; do not retry with force. Owned v3 retirement keeps its exact original task-lock semantics and existing disclosed combined native removal; larger user content does not broaden lock ownership. Git registration/file effects remain separately selected and separately reported even though native execution couples them. Confirm both absences and preserved branch/recovery. Do not delete the attached branch, detach it or restore automatically.

Git v3 retirement records both writable grant parents and their other children: destination `parent`/`siblings` and required `registration_parent`/nonnull `registration_siblings`. The registration parent is the exact canonical native `worktrees` parent; complete same-user physical identities and sorted unique directory/single-link regular siblings are bound by the immutable plan. Reuse the existing admission traversal, excluding the selected registration from the stored other-sibling list. Each pre-effect observation must equal those full planning facts, never substitute an apply-first baseline. After confirmed selected child removal, parent device/inode/UID/type/mode stay identical, with only removal-induced size/link-count/time changes permitted; every remaining sibling stays exact. Existing aggregate/record/budget limits and read-only sibling grants remain unchanged. Missing evidence refuses, without v1/v2/jj migration or extra proof passes.

## Prepared direct-reference deletion

Use one fixed network-denied native transaction process with bounded stdin/stdout/stderr and the same admitted native executable/configuration and ref-write/sibling protections. The protocol is `start`, `option no-deref`, `delete REF OLD_OID`, `prepare`; require the exact bounded acknowledgment for each phase. Only validated full refnames/OIDs are encoded with the native NUL grammar; no expressions or caller command text.

After preparation locks the queued target, re-read its direct identity and all required local protections. A symbolic target or observed OID change aborts; never follow a symbolic referent. Re-read affected provider evidence at this final boundary as well (whole-operation HTTP deadline/budget still applies). If all facts remain valid, send `commit`, require acknowledgment and successful process exit, then verify absence and recovery. Otherwise `abort`/close and wait; report incomplete native locks/effects if cancellation prevents confirmation. This protects against cooperating native writers; raw filesystem/ABA races remain disclosed. No mutation is claimed rolled back after a lost commit response.

The transaction creates temporary native ref locks, a disclosed part of the local effect. A recovered lock cannot be blindly removed on retry. Old-OID plus no-deref alone is insufficient to claim that a concurrently changed symbolic ref was rejected; the prepared readback is mandatory. Minimum/current native tests must prove lock/readback/abort behavior for packed and loose refs; missing capability refuses this v3 deletion path. A provider failure while prepared aborts before commit, preserving recovery.

## Recovery and result reasons

Store v3 plans/operations in `tidy-v3-plans` and `tidy-v3-operations` using existing secure-store primitives; no migration/adoption of v1/v2 records. Plan-scoped legacy completion is embedded in the immutable plan, not written as an original task. Recovery roots use the existing private native recovery namespace with a v3 plan/action-derived unique name, and are created with absent-name/zero-OID condition. Exact recovery OID and allowed namespace are validated before any deletion.

Journal phases: prepared → recovery-confirmed → target-prepared (reference locks only) → action-complete → complete. Retirement records registration-complete and files-complete separately. Before the first effect, refusal has no native mutation. After recovery/preparation, interruption or uncertain outcome is partial with known effects. Completed retries verify exact postconditions and never repeat writes; all other retries require manual inspection. Recovery retains committed history, not original index/reflog/untracked data or an automatic restoration promise.

V3 uses `ok|refused|partial|error`, command `tidy-plan|tidy-apply`, and candidate decisions `delete|retain`. Proof kinds: `none|native-ancestry|github-merged-head|github-branch-absent`. Reasons include existing selected-base/workspace-used/integration-not-proven plus `pr-head-mismatch`, `pr-target-mismatch`, `pr-ambiguous`, `hosting-proof-unsupported`, `hosting-evidence-unavailable`, `remote-source-present`, `tracking-mapping-unsupported`, `symbolic-reference`, `completion-required`, `active-workspace`, `dirty-workspace`, `content-limit`, `evidence-changed`. Operational diagnostics carry exact failed bounds/capabilities without raw credential/API bodies. Unknown reasons do not become eligibility; apply recognizes exact policy/proof/effect combinations.

## Journal entry field added by Spec 031 T045

`schema_version` stays 3. A local-reference journal entry may carry the optional `branch_evidence` object (`config_status`, `config`, `config_truncated`, `branch_oid`, `reflog_tip`): it is written once, together with the transition to `recovery-confirmed` and before the native deletion is prepared, never changes afterwards and is absent from journals written earlier, which remain valid and unchanged. Details and bounds: Spec 031 [restore contract](../../031-tidy-integration-proofs/contracts/restore.md#branch-configuration-t045-fr-048).

## Proposed narrow registration prune boundary

The future [Spec 011 narrow prune contract](../../011-workspace-reference-tidy/contracts/narrow-registration-prune.md)
is separate from the modes above. Its one named stw-locked missing-directory target cannot
satisfy live-checkout retirement by fabricated disk evidence. Reuse secure-store/journal and
history-pin discipline only where valid; raw registration recovery needs an additional private
payload and explicit restoration. The frozen plan/apply contract and strict readers remain
unchanged pending owner schema/interface decision. Existing branch restore does not restore
registrations. Absence/deletion witness support is a separate pre-implementation gate.
