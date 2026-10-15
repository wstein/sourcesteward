# Research: Isolated Task Workspaces

## Scope and constitutional gate
Decision: Spec010 supplies the later reviewed specification needed for native mutations; no
normative constitution change. Rationale: existing principles explicitly define plans/recovery
for future operations. Alternatives: broad policy rewrite would conflate implementation status
with policy; treating historical jj-pending prose as permanent prohibition contradicts Tier1.

## Storage and ownership
Decision: caller supplies existing private absolute state directory; no automatic source config
or workspace marker writes. Immutable random-ID plans, pre-action journals, task records and
exclusive cooperative locking. Rationale: explicit effects and durable recovery without collision
with tracked files. Alternatives: infer ownership from branch/workspace name (unsafe); put hidden
metadata inside source (undisclosed source mutation); trust arbitrary imported JSON (unsafe).
Fsync failures before native mutation refuse; after mutation report incomplete durable evidence.

## Native operations and helpers
Decision: fixed native Git detached-worktree creation and jj workspace creation at full resolved
identity. Base resolution uses literals, never arbitrary executable config/revsets. Git requires
preflight proving no executable checkout hooks/filter/submodule/helper path; preserve config and
refuse rather than suppress hooks. jj configuration isolation and least-write sandbox are required;
no Git writes inside jj and no fallback. Rationale: native semantics without undeclared helpers.
Alternatives: plain subprocess in caller config (helper execution); Git worktree for jj (ownership
violation); no-checkout-only directory (does not satisfy useful isolated task checkout).
Native research findings and exact supported argument/capability evidence are recorded by backend
authors in verification before acceptance; cannot count a stub or unsupported backend as passing.

## Races and recovery
Decision: durable prepared journal precedes creation, source/base/destination samples immediately
before/after, identity-bound retries only. Rationale: Git/jj locks do not isolate arbitrary editors
or destination races. Cooperative store lock prevents duplicate plans from concurrent stw clients;
external races remain explicit limits. Alternatives: automatic delete/reset on error loses user
work; adopt matching path by name confuses foreign workspaces; PID-only lock stealing is unsafe.

## Parallel implementation
Decision: common coordinator and result contract first; independent Git/jj adapter authors, CLI/
schema author and conformance author own disjoint files/worktrees. Root integrates focused commits
then independent source/safety review. Existing mandatory minimum/current fixtures remain required.
No new dependency is justified; use standard-library secure file primitives and current sandbox
patterns, with explicit platform limitations proven through conformance.

## Native research evidence
Git2.46 detached full worktree add invokes native reset and can run reference-transaction and
post-checkout hooks; no-checkout does not eliminate all hooks. Full creation therefore refuses
executable hooks/filter/helper/conditional-include/worktree-config complications and uses an
explicit locked detached full checkout. Native failure can remove its new destination; a private
0700 reservation plus journal prevents adopting preexisting empty directories. Source:
https://raw.githubusercontent.com/git/git/v2.46.0/builtin/worktree.c .
Native experiments on jj0.45.1 and0.46.0 found workspace-add with ignore-working-copy, exact
operation, full commit, `git.colocate=false`, fixed user identity and full sparse patterns
registers without populating. Native workspace update-stale in the owned destination materializes
without source snapshot. Disabling destination colocation prevents otherwise implicit Git child
execution. Exact experiment/CI evidence belongs in verification.md; operation sampling is not CAS.

Native jj source: https://raw.githubusercontent.com/jj-vcs/jj/v0.46.0/cli/src/commands/workspace/add.rs
uses create_worktree for colocated parents with git.colocate=true; false avoids that subprocess.

## Native jj materialization limit
Native workspace update-stale rejects operation pinning in both supported versions.
Only creation is pinned; materialization runs in the owned destination with operation
checks before and after. External/ABA races are not transactional; unexpected observations
produce partial effects and manual recovery evidence, never an automatic source update.

## Checkout name admission
Git checkout admission rejects symlinks, gitlinks and aliased tree names rather than
materializing an ambiguous checkout. Unicode normalization uses the already pinned
golang.org/x/text module, promoted to an explicit dependency for its normalization API;
the standard library has no canonical Unicode normalization facility. This retains
Unicode filenames while detecting canonical-equivalent and case-equivalent collisions.

## Unused filter definitions versus active checkout filters
Linux source CI exposed system-wide Git LFS definitions even in pristine disposable
repositories. The initial broad filter-definition refusal was stricter than required.
Final admission uses bounded native check-attr --source at the pinned base for every
validated leaf path, retains effective info/default-global attributes and fingerprints
results. Active filters refuse without execution or suppression. Configured sentinel
names set/unset/unspecified refuse to avoid ambiguity in native attribute output.
Primary setup evidence: https://github.com/git-lfs/git-lfs/blob/main/debian/postinst .
