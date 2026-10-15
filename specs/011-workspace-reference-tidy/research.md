# Research: Tidy Native Effects and Safety

## Local integration evidence
Decision: require explicit base and native commit ancestry; select literal local refs, optionally
repeat --ref. Omitted selection means all bounded local refs. Protect selected base reference,
current/linked-workspace-used targets, symbolic/conflicted refs and incomplete evidence.
Rationale: gone upstream, empty commit, similar name and merged-status alone prove nothing.
Alternatives: squash-equivalence and forge proof remain unsupported, explicitly retained; no network.

## Git native operations
Decision: create collision-checked private refs/stw/recovery/<plan-id>/<action-id> using update-ref
with zero expected old OID before CAS deletion `update-ref --no-deref -d refs/heads/X EXPECTED`.
A journal OID alone is not durable reachability. Reject configured hooks including reference-transaction
before recovery pin/delete; never silently suppress them. Revalidate base/ref/registrations/safety
before every action, account for own recovery pins, and retain private pins after completion.
Native target CAS does not lock base/worktree registrations; external/ABA races remain disclosed.
Native worktree remove couples present-tree removal and registration. Registration-only for a present
Git checkout is unsupported/retained; combined operation requires both exact authorizations, including
owned lock admission. No blanket worktree prune, forced dirty removal or native metadata editing.

## jj native operations
Decision: native `bookmark delete exact:NAME`, recovery `bookmark create NAME -r FULL_COMMIT`,
operation pinning and immediate before/after sampling. Native delete changes later remote intent;
retain any bookmark with any remote counterpart, tracked or not. Retain conflicted targets and
workspace-used commit/change identities. Recovery bookmark collision refuses; no change abandon,
bookmark forget, operation restore or raw Git fallback. jj is sampled, not compare-and-swap.
Native `workspace forget NAME` removes registration and preserves files; filesystem removal is a
separate explicitly authorized action, never inferred from a forgotten name.

## Fresh disk and owned retirement
Decision: prove complete raw disk cleanliness with confined no-follow bounded traversal, including
ignored/untracked extras. jj native file-list template reports path/type/executable/conflict; bounded
raw file-show queries with safely escaped literal root-file selectors preserve binary bytes.
Retain symlinks, gitlinks/submodules, sparse/conflicted/unknown/unreadable/oversized state.
Git refuses transformations/helpers when exact raw/stored equality cannot be established.
Native jj empty working-copy retirement may use a DISTINCT proof only when there is exactly one
parent integrated into base, complete stored tree equality to that parent, and complete clean disk.
Empty alone is insufficient. A normal integrated ancestry proof also applies. Multi-parent/root,
unique stored changes or unknown disk retains. Record and protect native working-copy identity.

## Secure state and filesystem effects
Decision: extend reviewed private store with lazy tidy-plans/tidy-operations namespaces, leaving
old plans/tasks/operations strict decoders and schemas intact. File+directory fsync recovery journal
precedes native writes; private recovery roots precede deletion. Plans/digests are untrusted evidence,
not caller authentication. Refuse state/path replacements and incomplete retries.
Filesystem effects must be confined to exact recorded owned destination identities, never broad
parent grants or recursive deletion from a name. Sandbox/root mount limitations require explicit
reviewed path handling; failure to prove confinement blocks implementation/acceptance, never a
relaxed fallback. Keep registration and file effects separate in results and journals.

## Research evidence and open gates
Native argument/template behavior was inspected experimentally by dedicated Git/jj researchers on
the pinned versions. Implementation acceptance still requires actual real-process fixtures in both
lanes; research is not shipping evidence. WI3 exact filesystem-removal mechanism requires its
pre-implementation analysis to verify confinement and failure reporting before enabling removal.
No extension hooks are configured (.specify/extensions.yml absent).
