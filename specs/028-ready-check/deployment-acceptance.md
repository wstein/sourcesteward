# Dedicated App profile: proposed acceptance contract

Status: T025 design candidate, not accepted capability or deployment. The owner
selected an autonomous dedicated isolated-App **design** if no suitable existing
App is known; a concretely nominated existing App remains reusable. T024's401
installation response is unknown, not evidence of absence. No resource, credential,
build, publication, configuration or activation operation is authorized here.

This proposal changes the conditional activation-channel design below. Existing
runtime, input schemas, CLI, evaluator and readers remain unchanged. Installed
profiles still refuse before HTTP. Independent design CLEAR precedes any port;
actual profile/security acceptance precedes any positive installed authority.

## Evidence baseline and source identity

The bounded inventory sampled public repository `wstein/sourcesteward`, immutable
ID1420737180, owner account ID36202/type User, main commit
`2e9b9fc99888b605bcce08afa6ef98846359e06a`. It reported main protection disabled,
empty effective-rule/ruleset collections, one CI source workflow and two dynamic
Dependabot entries, CODEOWNERS without reported enforced approval, and only the
`github-pages` environment. No Ready channel/executor or suitable App was proven.
Actions defaults reported read permissions; neither defaults nor the repository's
`permissions.admin` field proves current credential capability or source trust.

T024 used13 of a24-request maximum, no retries; installation metadata401 and
classic protection404 stayed qualified. It read no token, secret or variable
values. These are dated observations, not enduring absence or complete policy.
Local source dependency is separately exact
`8f0f2a2a0870b1749405954bac9340cdba6d5f9f` (typed producer and caller review only).
Prior full-artifact authority remains `51a9015c5912d221e3d3e9fdafb2811c9572aff0`.
The local inventory/provenance receipt is supplied with this candidate's handoff;
no permanent remote evidence archive or hosted acceptance is claimed.

## Concrete proposed trust boundary

Use GitHub-hosted Actions native events in the target repository, with a minimal
owner-approved wrapper and a dedicated App key in a separately approved credential
environment. The wrapper never checks out PR content. It runs one independently
approved immutable profile artifact, verified against a literal SHA-256 before
execution. A separate owner-controlled public **control repository** holds the
complete active lock. Target authors/automation have no control-repository write
role or credential, and the publisher App has no write installation there.

The control repository is a proposal, not an existing approved resource/name.
Reuse an owner-nominated suitable control repository if it meets the same proof;
otherwise its creation is a later explicit owner operation. No name, numeric ID,
App ID, artifact digest, accepted actor list or environment pin is fabricated.
All concrete pins must be filled and independently reviewed before installation.
Public activation contains only the already-public lock/transition, never secrets.

Channel isolation and App-key custody are **two independent mandatory proofs**.
An isolated activation file cannot stop a stolen App key from forging its check.
If candidate actors can relax the credential environment, approve its release,
change credential-reachable execution or mint an accepted publisher token, this
profile is unsupported even when every activation byte is genuine.

## Immutable bootstrap and authentication provenance

The immutable profile bundle contains a closed version1 bootstrap and executor
payload. The wrapper pins the bundle digest; the bootstrap pins the payload
digest and prior reviewed checker source. Neither document hashes itself. Ordinary caller files, report data, events,
repository text and command flags cannot construct or replace it. A generic
unconfigured stw build has no installed capability. No local unsigned assertion,
Accepted/Trusted boolean or opaque token without an actual producer grants trust.

| Bootstrap binding | Required exact value and validation |
| --- | --- |
| Version/profile | Exact version1 and profile app; unknown version/field refuses |
| Target | github.com, immutable target repository ID, canonical full name, main base ref |
| Publisher | Exact App numeric ID, target installation ID, literal review-evidence context; no display-name inference |
| Executor | Approved source repository ID/full name and full40-hex source commit; immutable artifact location and raw64-hex SHA-256 |
| Wrapper | Exact workflow path; independent final acceptance pins its blob/source and bundle digest, avoiding a circular bundle/wrapper hash |
| Credential environment | Exact target environment name; accepted event/ref restrictions and actor/custody evidence identity |
| Activation channel | github.com, separate control repository immutable ID/full name, exact branch main and fixed path .stw/ready-activation.json |
| Closure | Versioned finite execution/credential inventory, its exact raw digest and approved source revision |
| Acceptance binding | A separate final acceptance record binds bundle/bootstrap/payload/wrapper digests and actual actor/permission evidence; it is not an input Accepted flag |

The bootstrap's trust origin is the independently reviewed immutable deployment
bundle installed by an explicit owner operation, not a candidate-selected API
record. Freeze checker source, then payload, then bundle, then wrapper, then final
acceptance evidence in that order; no artifact contains its own final digest or
attests to its own acceptance. Owner installation and independent source/profile
review establish that origin; a purported acceptance JSON supplied by a caller
does not. A later runtime producer must identify this actual immutable origin
without trusting a caller-selected path, profile name or receipt assertion. Its producer is unavailable until that operation's actual source,
identity, custody and profile-acceptance evidence exists. Implementation must
retain typed unavailable evidence before then; this design creates no constructor.

For the proposed channel, first GET the independently pinned control repository
and validate immutable ID/name/host. Read only fixed
`repos/CO/CR/contents/.stw/ready-activation.json?ref=main`; require a regular file,
strict base64/size decoding, strict closed Lock parsing and full raw bytes.
Use the existing bounded read transport and explicitly accepted read credential;
actual access with that credential remains unproven. No anonymous fallback,
extra token, arbitrary endpoint/ref/path or credential bridge is introduced.
A denied/ambiguous read blocks; extending credentials requires separate review.

The authenticated API record and accepted control-repository write isolation,
not a signature field inside JSON, establish approval provenance. Owner actions
are accountable under that accepted channel; individual commit author names,
review account IDs or unsigned local files are insufficient. No new signing
platform, external service, polling scheduler or event bridge is proposed.

Candidate channel/routing/environment/App fields must match bootstrap pins before
any candidate-routed read. The full local lock must independently parse and equal
all authenticated activation bytes, including whitespace and transition/null.
Bind the full raw lock SHA-256, policy ID/version/source commit/raw digest, old
checker source/artifact and generation. Revalidate policy repository immutable
ID and the existing fixed pinned policy path; never fetch candidate policy as the
governing baseline. Repeat fixed control-repository identity, activation, local
lock and policy checks at start/final and publisher write-adjacent samples within
the existing global budgets. Any drift/error revokes positive completeness.

## Complete actor, credential and execution closure

Before acceptance, enumerate every principal/credential with any of the following
powers on target, control repository, App, credential environment or runner. Each
must be either an explicitly trusted owner/admin outside the candidate-author
threat model or demonstrably unable to exercise that power. Unknown is blocking;
repository ownership/admin metadata or a successful GET is not an allowlist.

| Power to account for | Required candidate exclusion/proof |
| --- | --- |
| Change control source/ref/branch protections or write activation | No target candidate/automation write role, installed token or delegated workflow can alter any channel dependency |
| Change environment branch/tag restrictions, reviewers, bypass, secrets or access | Only explicitly trusted owner administrators; enumerate role/token permissions and alternate mutation paths |
| Approve or bypass protected credential release | No candidate actor/self approval; include custom protection integrations and administrator bypass classification |
| Add/modify any workflow or credential-reachable local action/script | Old-baseline source gate protects all workflow additions/renames plus complete closure; newly reachable code fails closed |
| Change repository rules, App registration/installations, keys or permissions | Owner-only trusted administration; unauthorized candidate capability makes profile unsupported |
| Mint/use publisher tokens or recover key material | Key confined to accepted job/environment; no PR checkout/code, cache/artifact restore, upload/log leak or untrusted runner access |
| Change runner/tool/action/executor inputs | GitHub-hosted supported runner only; exact reviewed action/tool/artifact identities; candidate-controlled configuration cannot affect launch |

Inventory all workflow definitions, reusable workflows, local actions, scripts,
Makefiles/configuration affecting privileged launch, downloaded executables,
external actions, credential-loading code and every reachable credential name
and scope. Secret **values** do not belong in the public inventory. The privileged
job executes no target checkout, source build/install, project script or arbitrary
command/URL from event/report/API bytes. It restores no PR-produced cache/artifact.
Only the independently approved executor is runnable after exact digest check;
its libraries/resources and launch dependencies belong to the reviewed closure.

Mandatory roots include every .github/workflows/ addition/change, policy/checker,
bootstrap/wrapper, credential-loading and inventoried execution path. Compare
renamed paths too. An explicit finite allowlist of executed dependencies rejects
new/unknown reachability; listing known paths alone does not prove completeness.
Any dynamic invocation, unaccounted permission actor, mutable dependency or hidden
credential path leaves installed support unknown. A source test cannot certify
actual GitHub role controls or credential nonavailability.

The App's target token is installation-scoped and repository-restricted: checks
write plus only demonstrated PR/contents/status/metadata and required observation
reads. No PR/content/admin/rules/activation writes. It never supplies CLI users
with the private key. The control channel is not writable by any publisher token.
Read-permission feasibility and actual effective constraints remain acceptance
questions; no existing credential is presumed to have the proposed permissions.

## Old-baseline owner transitions and startup

P0/C0 remains governing throughout proposal and approval. For any changed mandatory
root or inventoried dependency, the authenticated full lock.transition must bind
exact target repository/PR/head, old generation/policy digest and all proposed
policy/checker source/artifact fields. All nine fields, null/omission, raw bytes
and source pins are validated; ordinary review issuer approval is not owner
transition approval. Exact old-policy full review and CI remain required. Merely
storing a transition locally or changing generation never activates new roots.

Owner bootstrap requires a separately authorized maintenance sequence: validate
the reviewed workflow/artifact/profile without exposing production App credentials
to candidate code; complete authorized disposable security tests; obtain a genuine
passing exact-source check using an owner-controlled bootstrap execution; apply
only the separately approved source-pinned requirement and protected-root controls;
then provision/release the production credential environment and activate the
matching generation. The owner must establish a controlled startup boundary
until those guards are effective. No unprotected interval may expose the key.
If that sequence cannot be demonstrated, do not activate this profile.

After an approved candidate integrates, new roots remain inactive until a distinct
owner activation installs the matching artifact/bootstrap and complete next lock.
Never let P1/C1 approve its own installation. Bootstrap/closure/channel changes
require new independent profile acceptance and matching owner transition evidence;
no mutable acceptance boolean is reread from the target. Rollback is another exact
owner-approved generation; no last-good fallback. Inconsistent partial updates
block and invoke the publisher's existing prior-success revocation contract.

## Native events, bounded publication and residual gaps

The trusted target wrapper routes native PR create/reopen/synchronize/close/base
changes, review status and selected comment changes, relevant base changes and
explicit authorized reruns to the accepted executor. Event payloads supply only
bounded routing IDs, verified against target API identity. Trigger/API shapes,
secret eligibility and missing/self-event suppression must be measured; this
proposal does not assert platform support from YAML alone. No polling/retry or
unreviewed event bridge compensates for a missing event.

Control-channel commits do not automatically deliver a target native event. Every
owner activation operation must include an explicit bounded reevaluation of all
known affected keys under the existing20-key fanout cap. This is part of that
separately authorized activation, not ongoing human review publication. Missing
activation rerun or incomplete affected-key inventory blocks rollout acceptance.
Report/PR changes publish autonomously only once their actual triggers are proven.
The existing shared-key serialization, singleton resampling, blocking lifecycle,
write/readback budgets and enforcement-unknown behavior remain mandatory. No
atomic merge/association guarantee or Ready-UI control is claimed.

## Proposed contract ports, tests and owner gates

This document proposes replacing only the conditional App activation-variable
reader/channel with the separate owner-control-repository reader. In
contracts/ready-check.md's activation paragraph and project-policy.md's App
consistency rule, environment would identify credential custody, while bootstrap
pins independently identify the activation repository/path/ref. Existing lock
JSON fields stay closed/unchanged; no candidate channel URL or Accepted field is
added. Those normative edits and ready_sources/transport ownership require an
explicit handoff after independent design review. No existing file is ported here.

After design CLEAR, test-first source work can define a real producer only from
the reviewed bootstrap/acceptance origin: reject arbitrary caller files, self
assertions, wrong repository/App/closure pins, candidate routing, unsupported
credential access and incomplete actor proof; preserve every R3 raw-byte/resample
negative, old-baseline transition checks and safe typed request/error provenance.
Structural fixtures can test shape/closed-version rules, never actual owner-only
channel permissions or credential custody. No executable prototype is needed to
pretend those unavailable predicates are passing.

Later independent hosted acceptance must prove hostile workflow additions,
environment-relaxation attempts, alternate approvals, local-action/script escape,
wrong App/check issuer, control-channel mutation isolation, old-success revocation,
same-head races, full transitions and all required event deliveries. Only then
prepare the exact minimal owner settings/rollback operation. Current T024 metadata
satisfies none of those positive hosted obligations.

Minimum proposed resources are one suitable/dedicated App installation on target,
one protected target credential environment, one approved immutable artifact and
one independently owner-controlled public activation repository (reusable when
concretely nominated). No new credential bridge, daemon or signing system.
The original target environment-variable channel remains an alternative only if
actual owner-only mutation and credential-custody proof becomes available; current
metadata does not prove it. Moving credentials/execution to a separate control
runner would need a different accepted event-delivery design and is not silently
substituted. Owner resource/channel acceptance remains a later concrete decision;
this candidate requests independent design assessment, not creation or rollout.
