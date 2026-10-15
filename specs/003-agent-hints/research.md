# Research: Agent Hints

## Discovery pattern
**Decision**: Adapt the merged SheetMill PR #19 pattern to root help only, with two raw-source URLs.
**Rationale**: Direct text destinations are easy for agents to retrieve and people to inspect. Root help is the relevant discovery point; version JSON must remain machine-safe.
**Alternatives considered**: Every-command notices would contaminate results; a new command or network fetch would expand foundation scope.

## Help write failures
**Decision**: Preserve the existing callback's stored error and single Run diagnostic boundary, including failures during the append.
**Rationale**: Cobra's help callback has no error return. Tests need a writer that accepts ordinary help then fails on the notice, plus existing early-failure cases.
**Alternatives considered**: Ignoring the append error could report success after incomplete output; changing all command error handling is unnecessary.

## Offline documentation checks
**Decision**: Map the two exact raw URLs to repository-owned files and validate index links locally.
**Rationale**: CI remains deterministic without network access. Negative fixtures prove the checker catches deleted or broken destinations.
**Alternatives considered**: HTTP status checks add external availability and cannot prove the local candidate revision has correct targets.

## Scope and authority
**Decision**: Guide the current build/help/version workflow and mark all repository/backend operations planned. Explicitly limit advice to relevant tasks and caller/repository policy.
**Rationale**: SourceSteward is an early foundation and repository text does not grant execution authority.
**Alternatives considered**: Copying SheetMill's compiler commands, flags or future capabilities would be inaccurate.
