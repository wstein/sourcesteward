# Contract: Ready inspection

## Entry points and results

```text
stw hosting ready-check NUMBER [PATH] --policy-lock PATH [--repo OWNER/NAME] [--host HOST] [--remote NAME] [--json]
gh jj ready-check NUMBER [PATH] --policy-lock PATH [--repo OWNER/NAME] [--host HOST] [--remote NAME] [--json]
```

Existing hosting selector/native-ownership rules apply. The lock is required, explicitly supplied and read by hosting, never by CLI parsing. Refuse symlinks/nonregular or oversized lock files and locks resolving inside the inspected target. An outside path alone does not authenticate its contents: the caller chooses this trust root. Do not search parent directories or PR files for policy. Read no stdin, editor, pager or shell command from data. Authentication remains bounded existing gh authentication; local native resolution precedes network access.

| Result | Meaning | Exit |
| --- | --- | --- |
| eligible | All required components pass in the supported profile | 0 |
| blocked | At least one complete known requirement fails; preserve any other unknowns | 1 |
| indeterminate | No known blocker, but required proof is missing/unsupported/inconsistent | 1 |
| error | Cancellation or operational inability to produce a valid assessment | 1 |
| usage error | Invalid number/selectors/required flag | 2, no JSON |

Each component is `pass`, `fail`, `unknown` or `not-required`, with `complete`, stable reason codes and source references. `not-required` is available only for deployment under an undeployed policy; all other components must pass. Global cancellation, identity drift or budget exhaustion sets review incomplete even if a previous intermediate result looked positive. No partial output is publishable. Writer/process failures may prevent complete JSON; consumers check exit status and parse integrity.

Fixed human notice: "Eligibility under the named policy at the sampled head and base; this grants no permission and does not predict a later merge. Required GitHub checks restrict merge, not the Ready UI transition."

Human values use bounded quoted escaping, including control/bidirectional characters. Output identifies whether the policy is caller-supplied or verified against a deployed activation. Native identity fields retain existing Git/jj representations and limitations.

## Reader allowlist and order

The operation exposes typed methods, not a public string endpoint. Preserve the existing `X-GitHub-Api-Version: 2022-11-28` pin; this design does not authorize an API-version migration. Each builder validates repository segments, numeric IDs, lowercase full 40-hex GitHub SHAs and fixed query keys. Base refs reuse Spec 025's proposed bounded safe ref grammar; do not relax the transport RawPath/redirect/origin guards. A reader returns typed `ok`, `absent-proven`, `denied`, `not-found-ambiguous`, `unsupported`, `malformed`, `limit`, `transport` or `canceled` evidence. HTTP 401/403/404 do not prove policy absence. Raw error bodies/tokens never enter diagnostics.

| Source | Fixed REST read | Purpose |
| --- | --- | --- |
| PR | `repos/O/R/pulls/N` | Number, repo IDs, open/merged/draft flags, head/base refs+SHAs, changed_files, mergeable/rebaseable |
| repository | `repos/O/R` | Immutable ID, default branch, allowed merge methods |
| base branch | `repos/O/R/branches/BASE` | Current tip and classic-protection presence evidence |
| classic protection | `repos/O/R/branches/BASE/protection` | Full required checks/strict and other enforced constraints |
| effective rules | `repos/O/R/rules/branches/BASE?per_page=100&page=P` | Active inherited and repository rule constraints |
| compare | `repos/O/R/compare/BASESHA...HEADSHA?per_page=1&page=2` | Behind/ahead counters; validate base/head identities; no patch consumption |
| checks | `repos/O/R/commits/HEADSHA/check-runs?filter=all&per_page=100&page=P` | Exact-head check ID/app/status/conclusion/output.summary |
| full statuses | `repos/O/R/commits/HEADSHA/statuses?per_page=100&page=P` | Status ID/context/state/creator/timestamps/report locator |
| open PRs | `repos/O/R/pulls?state=open&per_page=100&page=P` | Complete open-PR association set for exact head; singleton scope proof |
| files | `repos/O/R/pulls/N/files?per_page=100&page=P` | Filename, previous filename, change status; full count proof |
| report | `repos/O/R/issues/comments/ID` | Exact selected body, author ID, issue binding, edit timestamp |
| policy | `repos/PO/PR/contents/.stw/ready-policy.json?ref=POLICYSHA` | JSON content envelope; strict base64 decode, raw bytes and digest |
| activation | Selected profile's fixed authenticated reader, defined below | Active lock generation/digest, not PR-provided authority |

The effective-rules and compare shapes require fixture plus hosted profile acceptance; an unsupported API version or undocumented shape fails closed. Do not convert source observations in Spec 025 into proven normative predicates.

Order: native resolution and lock; PR/repository and pinned policy (read the policy source repository identity separately when it differs); active deployment binding if required; complete open-PR association set; files/statuses/report; checks/base/effective constraints/compare; final resamples. Collect complete evidence for a component before evaluating it. A closed/merged PR blocks and skips unnecessary sources. A fork's missing head repository does not change the base repository's authority, but missing head commit/ref identity prevents review.

Activation is a typed deployment interface, not caller-supplied URLs. Proposed App profile reads one named environment variable by fixed `repos/O/R/environments/ENV/variables/STW_READY_BASELINE` GET, with ENV from the approved lock and conservative ASCII validation. This channel's permissions, readable value, environment protections and provenance must be verified during profile acceptance. GitHub documents Environments read/write permissions and collaborator access: neither the variable name nor an environment restriction proves that PR-author credentials cannot edit the variable. If owner-only write isolation cannot be established, this channel is unsupported; do not deploy on its authority. Other profiles cannot use this channel by assumption; until their reader is specified and accepted, installed-profile eligibility is unknown. The API-provided variable value is the complete typed lock, not a partial summary. It must byte-match the full local lock including every transition/null field and routing identity, under the complete-activation rules in [project-policy.md](project-policy.md). Publisher bootstrap pins the expected channel independently; the candidate lock cannot choose a different channel. Compare full document SHA-256 at every activation resample. No activation write exists in this operation.

## Bounds and completeness

Total 30 seconds and 80 HTTP requests including resamples; 1 MiB raw/normalized body and final result; 32 KiB headers; 16 KiB auth buffers; 10 pages of at most 100 records per collection; 1,000 combined check/status rows; 1,000 changed files; 1,000 open PR rows; 100 required check pairs; 100 effective rules; one selected report comment; 64 KiB decoded policy or report; 16 KiB lock; depth 16; strings at most 2,048 bytes unless a field is tighter. No retries, backoff, arbitrary redirects, SDK caches/logging or compressed responses.

All arrays must prove exhaustion. Check total_count must remain stable and equal collected rows; filter=all prevents silently hiding reruns. Files must match PR changed_files with no duplicate path record; changed_files >1,000 refuses before collection. Other paginated arrays need a short final page, so exactly ten full pages cannot prove exhaustion. Duplicate IDs are malformed, not silently deduplicated. Unsupported/missing fields remain unknown. The 80-request ceiling wins over individually permitted collections.

Final bounded resample: PR, base tip when initially read, complete open-PR association set, complete local lock/authenticated activation document, full selected status collection and selected report comments. Compare repository/PR IDs, head/base refs+SHAs, state/merged/draft, base tip, the full associated-open-PR set, entire lock/activation bytes and digest including transition/null, winning review status IDs/digests and report bytes/author/edit metadata. Relevant differences make review unknown and final indeterminate (or retain an independently established blocker). Failed resamples never preserve positive review. Rebaseability must be true in both sampled PR responses for a passing constraint. CI checks may change after sampling: no transactional claim or favorable-state polling is permitted.

## Shared check scope

The pure review predicate requires a complete open-PR enumeration proving exactly one open PR at the sampled head, equal to the requested PR. Draft and differently targeted PRs still count. More than one is `shared-head-ambiguous`; an incomplete/changed set is `association-incomplete` or `sample-changed`; zero is not passing. This conservative v1 subset prevents one PR report from satisfying a commit-scoped check for another PR. The CLI remains read-only; only the separate publisher performs the mandatory shared-check invalidation described in its contract.

## Effective constraint subset

Require known repository rebase enablement and intersect all readable effective allowed-method restrictions; v1 requests rebase only. Require REST mergeable=true and rebaseable=true, each explicit boolean. If strict applies in effective hosting policy or merge.require_strict is true in project policy, compare must prove behind_by=0 against the current sampled base tip. Strict=false is known only when all effective sources are complete. A branch summary proving classic protection disabled plus complete effective-rules read may prove classic absence; a protection 404 alone cannot.

Supported rules: pull request required with no native approval/last-push/code-owner/conversation requirements; required status checks; linear history; rebase method restrictions; force-push/deletion restrictions which do not add a merge predicate. Every unknown type or enforcement parameter blocks eligibility as unsupported. Signature requirements, native reviewer eligibility, conversations, deployments, merge queues, code scanning and bypass-actor eligibility are not approximated. Evaluate without relying on actor bypasses. Aggregate mergeable_state is display-only, including `blocked` caused by Draft/self checks.

Required CI is the union of policy pairs and effective required context/app pairs. Policy must supply an explicit source rule for each required context; a newly effective unconfigured source is unknown, not implicitly trusted. A policy CI row has either a positive app_id with empty creator_ids or null app_id with nonempty creator_ids; reject other combinations. Newest status ID per context wins before issuer validation. For checks, newest ID per name/app wins; all remaining required matching sources must pass. If both a required check and status exist, neither may be ignored. Ambiguous source pinning yields unknown. App-pinned requirements cannot be satisfied by a commit status whose record proves only a creator account. Require completed/success for checks and success for statuses; neutral/skipped are not accepted in v1. Every used check must bind the exact head. If evidence shows the platform requires a test merge commit instead, the v1 exact-head profile is unsupported; do not claim GitHub satisfaction from head evidence alone.

The publisher's own expected context/App pair is excluded only from the review component. Final CLI deployment verifies its latest current check, while CI treats that exact pair through deployment rather than recursively evaluating it. A same-name foreign source is never excluded as self. The expected self source is derived from the accepted deployment binding, so no duplicate policy CI row is needed.
