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

## Useful bounded operations and approval scope

Approval covers a concrete named operation with all reviewed effects, not a
mandatory question per HTTP mutation. Manifest IDs name independent useful
operations, not an approved sequence; gaps in IDs preserve prior references.
Several specified calls can belong to one operation. Materially separate App
installation, key custody, settings, builds and executor installation retain their
own boundaries. No operation is authorized by this proposal or design selection.
Availability/identity mismatch stops without fallback names or broader permissions.
Current-state checks belong to that operation's explicit scope; old T024 metadata
is not reused as current capability proof.

P01 can create the public control repository empty **or** include the exact reviewed
inactive main/README initialization recorded in the manifest. The chosen approval
names all creation/commit effects and exact bytes/hash. Read back owner/repository
ID/visibility and, if initialized, the exact main commit/tree/file. No activation,
workflow or credential is initialized. Partial creation is reported; no automatic
deletion, name substitution or unreviewed initialization follows failure.

P02 registers the App without key generation or installation. P03 separately
installs the exact observed App on target only. P04 is one useful **no-secret
main-restricted environment setup**: preflight the exact target/name absence,
using at most three fixed metadata reads for operating identity, target and the
complete authorized environment list; a404 alone does not prove name absence.
Then create the environment, read back its ID/state, set custom branch-policy mode
while preserving unrelated protections, read back, add main only, and read back
the complete bounded policy set/mode. Its review includes all three mutations
and three post-write readbacks, plus that preflight; no retry. An existing
environment, unsupported policy, identity drift or uncertain response stops and
reports the no-secret/nonactivated partial state, without cleanup or key release.

P07 creates an owner-held key. P08 separately provisions it only after accepted
custody/source guards and controlled startup; no value enters receipts. P09 stays
blocked on an actual passing source-pinned check, current complete settings and a
reviewed preserving diff/rollback. Workflow/source-root integration, artifact
build/publication/install and hosted security acceptance remain distinct operations;
this proposal supplies no generic provisioning/release workflow.

P10 may be approved as **one exact activation lifecycle**, explicitly including its
bounded block/write/reevaluation effects. Before changing the channel, validate
old/new bindings and a complete at-most-20-key inventory, ensure the complete
lifecycle fits global budgets, acquire shared-key ownership in lexical order,
revoke owned successes and confirm blocking. Commit only the exact reviewed lock
against expected old channel state, read back full bytes/identity, then reevaluate
and publish/read back current results under existing fixed resampling rules.
No separate interruption between these named phases is mandated when approval
already covers them. No lifecycle or publication permission is granted here.

Use the existing60-second/120-request total and20-mutation/20-readback limits,
including the CLI30-second/80-request sub-budget; fewer than20 keys may fit.
No channel write precedes confirmed blocking. Cancellation, failed readback,
incomplete key inventory, drift or exhausted budget retains blocking and reports
unconfirmed enforcement, never replays old success or automatically restores the
old channel. Known partial effects and unresolved keys remain explicit. Settings,
executor install and builds are excluded from P10. Autonomous native report/PR
events still depend on actual event/security acceptance.

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

Already-authorized source work can continue independently of resource decisions:
caller-only evaluation/CLI under Spec028 and inactive T020–T023 publisher source,
with denied/uninstalled defaults and honest injected fixtures. Installed authority
remains unavailable; fixtures do not prove hosted acceptance. File ownership and
source dependency/review gates still apply, without a new infrastructure approval.
This source path does not require the owner to select a resource or defer work.

When the owner elects deployment work, the reviewed proposal can support a useful
named resource operation, such as P01 repository creation with exact inactive
initialization or P02 App registration, or reassessment of a nominated resource.
No current ID/source pin is invented to make later operations ready. The earlier
App design selection granted none of these creation/activation operations.

Installed T012/owner transition, full Ready evaluation/CLI, source-port ownership
and hosted/profile acceptance retain their separate limits. Existing runtime and
normative files remain byte-exact. No further inventory, provider API, configuration
write, secret read, build, deployment or activation occurred.
