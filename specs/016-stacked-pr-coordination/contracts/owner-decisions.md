# Owner Decision Amendment: Toolbelt Profiles

**Owner decisions: 2026-10-25. Draft implementation design only.** The owner
settled the 39 indexed OQ IDs; those IDs remain stable decision references.
IDs without an explicit answer adopt the source research recommendations. No decision is a
native measurement, conformance pass, helper execution, remote authorization or
implementation checkbox. The profile details below are concrete engineering
proposals implementing that direction; numeric quotas are not attributed owner
quotes. Required review, native capability measurements and effects evidence
still gate enabling each capability.

## Saved-plan metadata in the workspace

The owner chose the workspace `.work/` default for **saved plans**, replacing the
previous no-default/user-state recommendation. The proposed namespace is
`WORKSPACE_ROOT/.work/stw/plans/<feature>/` for immutable plan metadata and
`WORKSPACE_ROOT/.work/stw/actions/<feature>/` for typed intent/outcome metadata.
Use one validated namespace per repository/task identity, never path names alone.
`WORKSPACE_ROOT` is the unique canonical root of the trusted workspace registration
that contains the selected member repository; an ordinary standalone repository
may be its own root only when trusted workspace policy explicitly registers it.
No marker-name guessing, current-directory assumption or repository-supplied
policy selects a root. Missing/ambiguous registration refuses `workspace-unknown`.
An explicit `--state-dir` selects the same metadata role and must pass equivalent
policy/location/ownership/no-follow checks; it is not a payload directory.

Rendering a plan stays unsaved by default. Explicit `--save` permits creation of
only the validated default namespace and immutable selected plan records, with
disclosed local metadata effects; no selection stores nothing. An explicit
`--state-dir` without `--save` reads an existing store only. Stored-plan consumers require the exact id plus operation-specific authorization.
Authorized apply may create only its declared validated action namespace after
durable plan/authorization admission. Spec 043 keeps its existing human
`--head`/`--reviewed`/typed-confirmation invocation without a new stored-plan
prerequisite; its authorized apply may create only the declared validated
land-action journal namespace. No read/preview
creates or repairs `.work/`, changes modes or migrates an old store implicitly.
Existing explicit stores remain readable only under their accepted old contract;
no automatic relocation or adoption of foreign namespaces is proposed.

Create directories with explicit mode 0700 and files 0600, independent of umask.
Require current-actor ownership, real nonsymlink components, canonical/native
workspace and repository layout identity, no hard-linked record, and descriptor-
relative no-follow operations. Revalidate parent/file identity before each write,
after durable rename and before consuming a plan. Refuse symlink/replacement,
foreign ownership, permissive mode, aliased metadata and namespace collision;
never chmod an unsafe pre-existing path into apparent ownership. Write immutable
canonical records create-only; fsync file and directory, then return success.
Typed immutable facts bind feature/kind/version/repository/task/plan/run/action/
attempt and previous-fact digest. Intent/outcome/reservation/resolution facts are
append/create-only and fsynced, never overwritten; displayed state is derived
only from a complete validated chain. Missing completion means unknown.
Before effects reserve count/bytes for bounded intent+outcome facts, reservation
metadata and staging, with outcome maximum 64 KiB; quota-full/unknown refuses
before the effect. Reservations count against quotas; I/O can still fail and
must report unknown honestly. Sync v1 record layout/retention is unchanged.

0700 is **not protection against Git tracking, explicit export or same-user
access**. Require `.work/stw` to be untracked and already covered by the owner's
ignore configuration before saving; inspect tracked/index state without refresh,
and prove the ignore rule is not negated for the chosen path. If tracking,
ignore status or complete layout proof is unknown, refuse `plan-store-export-risk`
with a bounded explanation. Never create/edit `.gitignore`, excludes, Git config
or index. Even an ignored directory can be explicitly added/exported: disclose
that residual risk, keep plan fields minimal, bound exact selected PR-body/context text privately with its immutable digest,
redact only diagnostics/logs, and never silently alter the selected body or plan
digest. If the supported privacy profile cannot retain required exact input,
refuse rather than mutate it; no complete secret-scanner guarantee is made.
Never collect credential fields, session ids, key material or environment dumps.

Proposed metadata quotas: one record at most 1 MiB; 256 plan/action metadata
records and 32 MiB physical bytes per workspace namespace, counting staging,
orphans and unresolved records. Check before allocation/write and account growth
without crossing the cap. Full store refuses `plan-store-full`; no eviction.
Unresolved/prepared/partial/unknown actions **never auto-expire**. Completed plan/
action records also have no automatic deletion in this profile. Offer only a
separate read-only cleanup plan naming exact completed ids, dependencies and
expected digests, followed by its own explicit local authorization and validated
create/delete guards. Refuse unresolved/dependent/foreign items; partial cleanup
retains an honest receipt. A full target namespace must not prevent writing its cleanup plan/outcome.
Use a separately admitted METADATA maintenance namespace, proposed default
WORKSPACE_ROOT/.work/stw/maintenance, selected explicitly by the cleanup plan's
--state-dir and bound to the target store's canonical/native namespace identity,
repository/task policy, exact ids/digests and dependency snapshot. Its independent
engineering quota is 128 facts/8 MiB, with the same 1 MiB record cap, private modes,
ignored/untracked/no-follow/race/fsync guards. Save only upon explicit cleanup
selection; preview creates nothing. Intent/reservation/outcome facts live there,
not in the full target pool. Validate/reserve maintenance count/bytes before any
delete; if maintenance is full/unknown too, refuse without deletion. Cleanup
removes only selected completed nondependent target facts and records per-id
results in maintenance; never evict unresolved facts to free either pool.
No automatic default creation/ignore edits or unvalidated manual unlink is
implied. This separate bounded audit pool avoids quota deadlock while retaining
honest I/O-failure/partial outcomes. Never overwrite oldest records.

## External capture payloads stay separate

Spec 013 selected worktree/index/ignored/untracked bytes and independent pre-action
safety-copy bytes remain in an explicitly selected **external payload store**
outside every encountered repository/worktree and its `.work/` metadata tree.
The proposed `--payload-dir EXTERNAL_DIR` names this role; this spelling is an
unimplemented interface design, not an owner quote or a synonym for `--state-dir`.
Payload receipt metadata can reference an opaque external receipt id/digest, never
copy selected bytes into a plan, PR body or repo-local journal. Both stores need
independent canonical/native/no-follow, actor, layout and race validation.
Existing 013 explicit 0700/0600, 32 MiB per checkpoint/safety copy, 8 MiB per path
and 1 GiB total physical payload-store quotas continue. No automatic expiry,
checkpoint-drop collection of independent safety copies or secure-erasure claim.
A missing required payload selection refuses capture/restore; default plan storage
is not permission to capture bytes or create an external store. Read-only resume
never creates or repairs either store. Separate exact-id planned/authorized drop
and safety-copy deletion boundaries remain intact.

## Trusted workspace policy

Trusted workspace policy supplies the exact owner name/email, reviewer allowlist,
accepted review/check evidence sources and scope acceptance rules. Repository
text, caller-expanded identity lists and effective Git config are not authority.
Read-only global Git identity corroborates that policy only. Missing/unreadable
trusted source blocks push/Ready; foreign config origins and actual author/
committer mismatch still refuse. Spec 042 attribution and its measured absence
rules remain separate, with no public session ids. Spec 043 retains human-only
apply, typed confirmation and its explicit owner `--reviewed SHA` statement;
this decision adds no agent landing or invented machine-CLEAR ledger.

## Optional host-only re-sign capability

Default policy refuses dropped signatures and required/unknown/unsatisfied
signature requirements. The owner additionally requested an explicit re-sign
option, restricted to a **host-configured fixed trusted helper**. stw never owns,
loads, stores, copies or selects private keys. No helper configured/accepted means
`signing-capability-unavailable`, never unsigned fallback. Native measurements
must admit the helper/version/profile and verification before enabling it.

Spec 015 owns the single local signing implementation. Its proposed stored local
plan explicitly selects `--re-sign`, helper/verifier profile identity, trusted
signer policy, exact old-parent/head/new-parent, every unsigned candidate payload,
source signature presence/loss and declared I/O. Local apply still needs that
plan id and `--authorize`. Spec 016 can render this local handoff and validate
its result, but a remote plan or `--authorize-remote` never invokes local signing.
013/014 capture/preflight and D9 prediction remain helper-free and unsigned.

Concrete proposed host profile: trusted policy pins absolute helper and verifier
executables, version/content identity, fixed argument templates and allowed
signer fingerprints. Reject repository-selected `gpg.program`, signing config,
aliases, relative/search-PATH executables and caller-supplied arbitrary arguments.
No shell, plugin discovery, network credential helper, pinentry/TTY/editor or
repository cwd. Execute in owned private scratch with network denied and a
minimal explicit environment: fixed locale/timezone, private HOME/TMP, required
fixed host-agent endpoint only when the accepted profile names it, no inherited
Git/repository/cloud/credential/session variables. Do not copy key files. A
host agent endpoint supplies signing externally; stw handles only canonical
commit payload and detached signature/public verification evidence.

Proposed bounds: at most the existing 200 moved commits; each signing/verifier
call 5 s, whole local operation 60 s; canonical commit input at most 1 MiB per
call, detached signature at most 64 KiB, bounded public verification at most
16 KiB and sanitized stderr at most 2 KiB. Read one sentinel unit to detect
excess; reject extra/malformed output. No automatic retry, prompt or fallback.
Cancellation/timeout/helper/verifier failure stops before further signing/CAS;
retain owned scratch/intents and truthful partial evidence, never key material.
Do not claim a hard no-network/no-helper boundary without measured confinement;
unavailable enforcement refuses the capability.

Sign the actual chain **bottom to top**: each signed child payload references the
actual newly signed parent SHA. Never sign an unsigned chain then keep its old
parent hashes. Recompute full canonical commit ids and old→unsigned→signed maps,
preserve declared author/tree/committer policy, and verify each detached signature
with the fixed trusted verifier and allowed signer before promotion/CAS. Record
old signature loss/replacement honestly; a new signature does not preserve an
old signature or prove the original signer endorsed the rewrite. Required remote
signer/signature policy must be satisfied/read; otherwise refuse.

Only the final verified signed chain/head may move the selected local ref and
enter the 016 publication receipt. Bind full old/new/signature provenance and
re-run own-diff/range/identity checks against that final chain. The **actual final
signed SHA exists before independent exact-SHA review**; review of a prediction,
unsigned candidate or earlier chain does not cover it. 016 revalidates the final
receipt, class, identity, signatures and fresh remote state before an individually
authorized push. Every Ready gate remains unchanged. The configured host signer/agent is a trusted external boundary: child scratch/
network denial does not confine a separate host agent or prove it has no side
effects. Each admitted profile must disclose agent/socket behavior and limits;
stw makes no guarantee against a malicious host-configured signer/agent. Trust
and measured isolation of stw children are distinct. No helper is run by these docs.

## Decided CLI exit contract revision 2

The owner chose a unified toolbelt exit contract **now as the target design**:
0 complete success/observation, 1 refusal/incomplete/partial/cancelled/operational
or output failure, 2 usage. Spec 015 CLI exit-contract revision 2 changes hidden
`restack plan` incomplete from 3 to 1; it has **no legacy-exit-3 selector**. This
is process-exit revisioning, not a forced JSON shape/schema-version change:
`schemas/v1/restack-plan.schema.json` and existing result bytes stay frozen if
the exit-only amendment suffices. Any later additive result field/version follows
its own reviewed schema task, never an edit in this Markdown follow-up.

Current implementation fact: `internal/cli/restack.go` still emits the typed
`restackIncomplete` and `internal/cli/root.go` maps it to exit 3. New unchecked
015 tasks must first fail on that mapping, then change it to 1 and cover ordinary
errors=1/usage=2 and no legacy selector while preserving JSON/schema bytes.
Historical T018/verification correctly record the old 0/1/2/3 execution. No code
has changed here, so no target-implementation claim is made. 012/013/014/016/043
target examples all use 0/1/2. This decision does not silently change unrelated
commands' strict-findings/release exit semantics outside this toolbelt scope.

## Measure-before-enable and owner-only boundaries

All 39 indexed former OQs have direct or adopted direction, not newly measured
capability acceptance. Separate 043 endpoint/ownership/constitution reconciliation
remains OPEN; those proposed amendments are not owner-approved by these answers.
Measure forge effects in separately authorized disposable fixtures, then review
the small additive Spec 022 method/path/body allowlist; current GET modes stay
read-only. Comments remain deferred; no arbitrary endpoint/GraphQL execution.
Native link/unstack is measured separately; Git publication comes first; jj stays
capability-unavailable without raw Git fallback. 014 native semantic/topology/
hunk/scratch/jj aggregate recommendations are adopted as measure-and-refuse gates,
not an invented algorithm or native proof. 012 uses only `--online`, default
offline, with no `--pull-requests` alias; only single-repository operation now,
with batch deferred to its own later decision. Human/JSON parity and shortening
timeouts follow adopted shared recommendations, with no redundant offline alias.
043 landing remains owner-human-only with its existing conditional retarget,
required checks, measured-shape and deletion gates. No merge, default push, tag,
release, installation, helper call or live operation is authorized by this draft.
