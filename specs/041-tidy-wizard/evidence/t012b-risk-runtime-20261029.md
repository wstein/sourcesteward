# T012b read-only registration risk source evidence

Date: 2026-10-29. Base: `23ef6edf54421a18a71eef9722e11ffc403afbda`.
Scope: the accepted [risk composition](t012b-risk-adaptation-proposal-20261026.md)
and [overview display amendment](../../011-workspace-reference-tidy/contracts/registration-prune-v4.md#accepted-overview-display-risk-amendment-2026-10-27),
implemented as source only. Manifest-2 wizard selection and apply remain separate work.

## Executed failing-first evidence

Before implementation, `go test ./internal/tidywizard/render -run TestRegistrationRisk -count=1`
failed because native rows lacked risk annotations and the separate summary count.
The final focused command is:

```sh
go test ./internal/tidywizard/... ./internal/cli \
  -run 'TestRegistration|TestTidyOverviewV4|TestEveryWord|TestAllReasons' -count=1
```

Coverage includes exact retained refusal reasons, CLI exits for complete and incomplete
observations, every orange gate, native proof/identity preservation, no registration
PR/group fields, unchanged legacy groups/totals, filtered counts and row/byte output cuts.
The original row's 16 required fields remain; optional display fields grant no selection.

## Executed causal probes

Each probe changed production source, ran `TestRegistrationRisk`, failed an assertion
rather than compilation, and restored the source before the next probe. All 13 died:

- Retained refusal mapped to red instead of kept.
- Native admitted row mapped to green instead of orange.
- Each nonempty destination, task, repository, name and gitdir guard removed separately.
- Native proof gate removed.
- Original task-record owner-source gate removed.
- Derived scope consistency gate disabled.
- Extra effect admitted by removing the one-effect bound.
- Full lowercase evidence-digest gate removed.
- Registration risk counts taken after the row cut.

## Verification and remaining gates

The scoped tests pass. The unmodified-base `make check` was canceled while its full
Go suite was running; it is not a baseline pass. Final full plain Go, static checks,
self-check, hosted CI and exact-head independent review results belong to the PR's
claims table. The owner's session instruction leaves race verification to CI.

No standalone build, live cleanup, apply feature, frozen v2/v3 schema change, ID/digest
encoding change, task/progress completion tick, release or deployment is included.
Observation prepares no recovery. Read-only output keeps metadata-only counts separate
from selected-effect counts; no final apply consequence is claimed.
