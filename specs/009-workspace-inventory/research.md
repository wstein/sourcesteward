# Design research

## Decision: retain the reviewed inspection execution boundary
Rationale: CLI imports only internal/inspection. Existing ownership detection, bounded Git runner and enforced jj sandbox already establish the required safety boundary. Extract jj session setup instead of copying it. No new dependency.
Alternatives: a new generic backend framework increases scope; raw Git inside jj violates native ownership policy.

## Decision: one bounded inventory command with optional predicate
Rationale: one result model serves both human rendering and schemas while explicit query operands distinguish known false from failure. Single total ten-second deadline, 64 KiB native output, 1 MiB result and 1,024 records bound all loops. No silently truncated complete result.
Alternatives: one command per predicate increases the initial API surface; exit-only predicates lose unknown/error distinctions.

## Decision: qualify native disk limitations
Rationale: jj no-snapshot reads stored changes; disk freshness cannot be inferred from the working-copy commit. Requested unsupported scopes become partial with unknown observations. Git staged/disk scopes remain distinct. Configured candidate expressions are not executed; retain evidence/limitations.
Alternatives: snapshotting jj or arbitrary filesystem traversal changes scope and effects.

## Decision: sampled remote evidence only
Rationale: no fetch allowed; local-only, containment and ancestry have explicit operands and sampled identities, never forge/squash/publication claims. Missing upstream or remote coverage stays unknown.
Alternatives: conventional main/origin guesses and implicit fetch violate FR-012/013.

## Repository and primary interface references
Reviewed internal/inspection/{inspection,git,jj,detection,result}.go, docs/backends.md, Spec 008 contracts and CI minimum/current lane setup. Native interface support is verified by independently authored temporary-repository fixtures against Git 2.46.0/2.56.0 and jj 0.45.1/0.46.0; documentation is not execution evidence.
- https://git-scm.com/docs/git-worktree
- https://git-scm.com/docs/git-status
- https://git-scm.com/docs/git-for-each-ref
- https://docs.jj-vcs.dev/latest/cli/
- https://docs.jj-vcs.dev/latest/templates/

All design unknowns are resolved as explicit qualification or implementation tests; no permission to mutate is introduced. No .specify/extensions.yml is present, so before/after hooks do not apply.
