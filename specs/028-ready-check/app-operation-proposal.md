# Inactive App operation proposal

This proposal implements no runtime behavior and authorizes no operation. Its
[manifest](app-operation-proposal.json) is review data, **not** a GitHub App
manifest, provider apply payload, trusted bootstrap or installed acceptance record.
Unknown IDs/digests are explicit nulls; no parser may treat this file as authority.
It supplements the [design](deployment-acceptance.md) whose coordinator-reported design CLEAR is at
`67694593697947db0db4d0aa4928d8b15dbda00b`. That commit remains unchanged.

## Exact proposed names and boundaries

| Resource | Proposed identity | Known identity / unresolved proof |
| --- | --- | --- |
| Target | wstein/sourcesteward, main | T024 repository1420737180, owner36202/User; current operating principal/permissions unknown |
| Public owner channel | wstein/sourcesteward-ready-control, main, .stw/ready-activation.json | Not created or availability-checked; numeric ID and actual writer/credential closure unknown |
| Dedicated publisher | Owner-account App SourceSteward Ready; desired slug sourcesteward-ready | Not registered/availability-checked; App/installation IDs null; suitable nominated App may replace proposal after exact reassessment |
| Credential environment | Target stw-ready-publisher; main branch only, no tag policy | Not created; ID, mutation/bypass actors and actual supported controls unknown |
| Key custody | Owner-held key; later environment secret STW_READY_APP_PRIVATE_KEY | No key generated/read; key holders, rotation and candidate exclusion proof unknown |
| Privileged wrapper | Target .github/workflows/ready-evidence.yml | No workflow file exists from this work; exact blob/source pins and native event acceptance unknown |
| Executor | Reviewed SourceSteward source, immutable profile bundle/payload | No artifact built/installed; source SHA, digests, location, closure and final acceptance null |

The public channel discloses only approved lock/transition data, never credentials.
No publisher App installation is proposed on the control repository. Control-repo
writes are owner-only by design, not an observed access guarantee. App installation
is target-only. Requested repository scopes are checks write plus contents, PRs,
statuses and metadata read; no user permission, webhook service, OAuth grant,
contents/PR/admin/rules/activation write or extra credential bridge. Additional
read scope is unknown and requires demonstrated necessity plus its own exact
permission approval. Existing read access to the separate public channel must be
proven; no anonymous/token fallback or inferred access is approved here.

Candidate exclusion requires both channel-write isolation and complete App-key
custody. Inventory actual owner/admin roles, Apps, token minting/key holders,
environment policy mutators, release approvers/bypass actors, all workflows and
reachable local actions/scripts/configuration. An actor able to relax main-only
restriction or run alternate credential-bearing code makes the profile unsupported.
The proposed unattended environment has no interactive release approval: autonomous
publication depends on enforced old-baseline roots and owner-only administration,
not on an ongoing human approval step. This does not assert platform support.

The privileged job has no target checkout, PR cache/artifact, project script or
build/install. Exact approved bundle digest precedes execution; payload/bootstrap
pins and complete dependency closure remain required. Freeze checker source,
payload, bundle, wrapper, then final acceptance in that order to avoid self hashes.
No ordinary source snapshot or caller-created receipt supplies immutable origin.

## One operation per approval

P01–P11 in the manifest are independent approval slices, not an approved sequence.
The owner can select P01 or P02 after independent review; selecting either does
not authorize the other or any downstream slice. Availability/identity mismatch
stops the named operation without a fallback name/account or expanded permission.
An operation's actual current-state precondition must be checked under its explicit
scope; T024 metadata is not reused as present authorization or capability proof.
No action is requested from the owner until the proposal is independently reviewed.

P01 creates an **empty** public repository only. P02 registers the App only, with
no private key generation or installation. P03 separately grants the exact observed
App the target-only installation. P04 creates an empty environment; P05 changes
only its branch-policy mode; P06 adds only main. These distinct mutation effects
are not hidden inside a single “configure App” approval. P07 creates an owner-held
key; P08 separately provisions the named secret only after custody/source guards.
No key/value may appear in an agent log, manifest, PR body or acceptance receipt.

P09 is blocked: the actual source-pinned check, current complete settings and exact
preserving diff/rollback do not exist here. It is a named later operation, not an
apply payload or authority to replace rules. Workflow integration, source-root
protections, immutable artifact build/publication/install and hosted security tests
also remain separately reviewable/authorized operations; no generic provisioning
or release workflow is supplied by this proposal. Missing controls block P08.

P10 is blocked until complete real pins and a reviewed raw-byte channel diff exist.
If the empty control repository needs main/history initialization, that is another
exact separately approved commit operation; P01 includes no such initialization.
P11 is a distinct check-publishing execution requiring its own explicit approval.
An activation change can make earlier successes stale before reevaluation; before
P10 the owner must already have an approved blocking/invalidation step for affected
keys, and preserve that blocking state until P11 confirms current results. P10 is
not approved without that surrounding explicit lifecycle. Missing affected keys,
failed revocation or a set exceeding20 is enforcement-incomplete, never permission
to leave an observed old success green. No combined activation/publish approval
is inferred, and unrelated native report/PR events remain autonomous only after
actual event/security acceptance.

## Rollback and stop conditions

Stop before mutation if the operating identity, owner, target ID, resource name,
permission set or current-state precondition differs. Do not retry with a broader
credential or discover unrelated accounts. Required missing evidence stays unknown.

Repository/App/environment creation is not automatically reversible: deleting a
resource can destroy data, identities or other uses. Any deletion needs a separate
owner-approved operation after fresh scope/dependency inspection. Do not delete
an App to undo a merely pending proposal, or revoke unrelated installations/keys.
A newly generated unused key can be individually revoked by a separately approved
key-ID operation; once in use, revoke only with explicit impact and blocking plan.

For deployed failure, first use the accepted publisher lifecycle to revoke owned
successes/read back blocking state. If key custody may be compromised, owner key
revocation is its own urgent explicit operation and enforcement remains unknown
until readback; channel rollback alone cannot repair a stolen App key. Restore
only a reviewed exact generation/artifact/lock through another owner transition,
not last-good fallback. Required-check or root-guard removal is a separate concrete
settings/rollback approval preserving unrelated policy. No rollback is executed
or automatically authorized by this document.

## Review and next decision

This is a finite concrete choice: create the empty proposed public control repo,
register the proposed target-only-permission App, or defer/nominated reuse, one
operation at a time after independent review. No current resource ID or source
pin is invented to make a later gate look ready. The owner has not selected any
creation operation through the earlier App **design** choice.

Installed T012/owner transition, inactive publisher, full Ready evaluation/CLI,
source-port ownership and hosted/profile acceptance retain their separate limits.
Existing runtime/normative files remain byte-exact. No further inventory, provider
API, configuration write, secret read, build, deployment or activation occurred.
