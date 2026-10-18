# Contract: project policy and approved activation

The input schemas and fictitious examples in this directory are unpublished design documents. No example grants trust to an account, App or repository. Parsing and semantic requirements here are additional to schema validation.

## Authority and documents

A lock supplies schema version, repository identity, generation, immutable policy source/commit/raw-byte SHA-256, checker source SHA/artifact SHA-256, deployment profile and optional environment channel. Explicit caller selection is the authority for an undeployed evaluation. Installed-profile evaluation additionally verifies the active generation through the accepted authenticated profile reader. The local lock cannot claim current deployment by itself.

The policy is a closed JSON document with:

| Field | Rule |
| --- | --- |
| schema_version, policy_id, version | Exact supported schema 1; stable policy token and positive policy version |
| repository | Host, immutable numeric repository ID, full name and base ref; must match lock/API identities |
| review.context | One exact context, selected by project; no default CrewBook name |
| review.status_creator_ids / report_author_ids | Nonempty immutable numeric account allowlists; checked independently |
| review.tiers | Explicit ordered strengths with exact configured model ID/effort tokens; no fuzzy family/tier inference |
| review.default_tier / path_tiers | Default minimum and prefix-based minimum increases; strongest applicable tier wins |
| review.max_rounds | 1–20 disclosed NOT CLEAR rounds; reaching the threshold blocks until owner changes policy through the normal approval flow |
| review.independence | `declared-execution` in v1; runtime-attested proof is unsupported, never silently downgraded |
| review.allow_low_followups | Whether Low findings may be resolved as accepted follow-ups with owner/evidence; all open findings block |
| ci | Exact context/source requirements; status sources require creator IDs, check sources require App IDs |
| merge | Rebase only; optional project requirement to enforce strict freshness in addition to effective hosting rules |
| deployment | `none`, `required-workflow`, `protected-source` or `app`; installed profiles require capability acceptance and the matching active identity |
| trust_roots | Additional path prefixes whose changes require owner transition approval; `.github/workflows/` and the approved checker/policy/credential-loading paths are mandatory roots |

Tier ranks are distinct positive integers; every referenced tier exists. Prefixes are literal slash-delimited relative directory prefixes or exact filenames, not globs or regular expressions. A directory prefix ends in `/`; case is byte-exact. Reject empty/absolute/parent-traversing prefixes. Test both filename and previous_filename for renames; additions/deletions count. Every path is reviewed under the baseline, never under candidate-supplied path rules.

Policy/context/issuer IDs, model tokens and other trust values cannot be extracted from AGENTS.md or prose. A user may author a policy expressing those requirements, but this tool does not translate instructions into authority. Gitnote grammar and existing workflow policy remain unchanged.

## Parse and hash rules

Input must be UTF-8 without BOM, one JSON object, no trailing tokens, duplicate object keys or unknown fields. Enforce depth 16 and document limits before allocating nested structures. Reject integer overflow, non-integral IDs, invalid digests and unsupported version. Policy/report hashes are SHA-256 of the exact decoded raw UTF-8 bytes, including whitespace; no reserialization before checking a digest. The local lock uses the same rules with its 16 KiB cap. A fetched policy must come from the pinned full commit at the fixed `.stw/ready-policy.json` path, be a regular file, decode as strict base64 and match its declared size and digest. Its source repository ID must match the lock, not only its mutable name.

Schema validation establishes shape only. Cross-document identities, tier membership, unique IDs/ranks/prefixes, old/new policy transition semantics and effective hosting completeness require semantic validation. Limits never authorize truncation.

## Complete activation authentication

The activation document is exactly the closed `lock.schema.json` document, including every authority-bearing member and explicit `transition: null` or the complete transition object. It is not a summary of generation/policy/checker fields. For an installed profile, the local candidate lock bytes must equal the authenticated remote activation bytes in full after decoding the API value string as UTF-8; both must independently pass strict parsing/schema/semantic validation. Record SHA-256 of these complete raw bytes. Whitespace changes therefore require the same approved bytes at both locations and invalidate an in-flight sample.

The compared fields include repository/host/base identity, generation, policy-source repository/name/commit/digest, checker source/artifact digest, deployment profile/environment/App ID, and every transition field: target repository/PR/head, old generation/policy digest and all proposed new policy/checker identities. Missing transition is invalid; null is not equivalent to an object. A local transition added to an otherwise valid active lock never inherits authentication from unchanged generation/policy/checker values. Remove, substitute or alter only one transition field and authentication must fail.

The publisher's activation channel host/repository/environment and expected publisher identity are pinned by its independently accepted deployment bootstrap, not selected by the candidate lock or report. The lock's routing fields must match those bootstrap pins; they cannot redirect authentication to a different attacker-writable variable. CLI output distinguishes caller-selected trust-root input from independently deployment-verified authority. An installed profile without an accepted bootstrap/complete activation reader cannot pass review publication.

Validate the complete lock against the authenticated activation at collection start, final CLI sample and publisher write-adjacent samples. Reread both sources, compare full raw bytes/digests and reject any drift, including only transition/null/candidate changes. Bind the complete active-lock digest into the result and published check envelope so an unchanged policy generation cannot hide a changed authority record. Undeployed caller-policy mode may inspect a supplied lock but does not authenticate an owner transition or grant transition clearance; privileged-root changes remain blocked until a supported authenticated activation channel proves their exact approval.

## Baseline transition state machine

| State | Allowed consequence |
| --- | --- |
| Current | Owner lock L0 pins policy P0 and checker C0; all ordinary PRs use P0/C0. |
| Proposed | PR proposes P1/C1 or changes any mandatory trust root. It is still evaluated by P0/C0. Without exact owner transition approval it is blocked. |
| Approved candidate | A trusted owner-controlled activation record names repository/PR, exact candidate head, L0 generation/policy digest, current C0 binding and new P1/C1 identities/digests, as part of the fully authenticated lock.transition object. Old-policy full review and ordinary CI remain required. No automatic policy weakening or root activation occurs. |
| Integrated, inactive | Owner merges the reviewed candidate. Proposed roots are not accepted for evaluation until the explicitly approved activation is consistent. A new wrapper unable to prove its active identity fails closed. |
| Activated | A separate owner operation installs the matching lock generation and trusted executor. Reread verifies all pins. Existing reports/checks for P0 do not satisfy P1. |

Approval provenance is the chosen profile's owner-controlled configuration channel. A named environment variable is not sufficient: GitHub documents collaborator variable access and Environments write permissions. Profile acceptance must establish that candidate-author/workflow credentials cannot change this channel; otherwise it is unsupported. A report/comment by an ordinary allowed review issuer is not a transition approval. Local files carried in the PR never establish it. The deployment acceptance must prove that PR authors cannot write this channel or access its credential; administrative owner compromise/bypass is outside this protection claim.

V1 does not automate activation, rollback, settings changes or secret writes. Partial updates block. Rollback requires another explicit generation and matching owner approval; it is not “fall back to last good policy.” A changed generation invalidates the sample even if policy bytes happen to match.

The App profile requires privileged-root transitions because a future merged workflow could otherwise take its credential and ignore immutable executor pins. The gate must cover the complete credential/execution dependency closure, including newly added workflows, local actions and invoked scripts. A missing closure or unenforced owner channel makes this profile unsupported. A path string by itself proves none of this.

Profile consistency: none requires null publisher_app_id/environment and no installed-profile claim. App requires a positive publisher_app_id and an accepted activation environment; policy and lock profiles/App IDs must agree. Other profiles remain schema-representable proposals but cannot pass installed evaluation until their source and activation reader contracts are accepted. A transition must match the current generation/policy and candidate PR; proposing a transition inside an untrusted report grants no approval.
