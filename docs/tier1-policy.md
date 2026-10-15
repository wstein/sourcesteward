# Tier 1 Git/jj policy amendment

## Rationale and scope

Constitution 1.1.0 materially expands backend support commitments: Git and Jujutsu (jj)
are Tier 1 peers for declared supported capabilities. This replaces the earlier Git-first,
jj-ready positioning without claiming that native jj inspection already works. Spec 008
owns that implementation and its version/acceptance contract. GitButler remains separate.

The constitution-only Spec Kit step resolved the active constitution template successfully;
no extension hooks were registered. It preserves the ratification date, uses a minor version
bump for expanded guidance, and leaves no template placeholders. The temporary sync-impact
comment was removed before commit as required by the constitution skill. Documentation
synchronization is a separate user-authorized step, not an expansion of that skill's scope.

## Impact assessment

- No CLI code, schemas, tests, dependency versions or deployment files change here.
- Published version/inspection v1 schemas and current ordinary-Git behavior stay unchanged.
- Future adapters must select by ownership and document minimum/current versions, capabilities,
  noninteractive bounded results, freshness and effects. Installing jj alone cannot select it.
- Read-only jj acceptance must test working-copy snapshots and operation-log effects; a flag
  name alone is not evidence. Native jj unavailability must not cause silent Git writes.
- Supported Git/jj capability regressions block releases equally; backend semantics and
  capability gaps remain explicit. This does not promise every operation on both backends.
- Existing feature evidence remains historical; shared progress now records merged 005–007
  source without asserting a newly verified live deployment.

## Verification and review

Baseline `make check` passed Go tests, vet and module verification, then stopped because this
new worktree lacked the ignored pinned Hugo binary. The existing official Hugo v0.168.0
binary was copied from the reviewed Hugo worktree; no dependency or installer change was
needed. Final `make check` passed Go tests/vet/module verification, 19 Python tests, 12 Node
tests, documentation links (80 authored documents), architecture boundaries and whitespace.
Independent review is required at the exact committed revision before PR handoff.
Owner review of the amendment is through its pull request; no merge authority is implied.

## Next Actions

The original non-governance intent is native jj inspection with compatible schemas and
conformance CI. Continue through `$speckit-specify` for Spec 008, then plan, tasks, analysis,
implementation and independent review. Do not treat this amendment as implementation evidence.
