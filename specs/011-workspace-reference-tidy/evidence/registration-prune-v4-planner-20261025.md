# Contract-4 stored registration planner evidence

This source slice implements expert planning and strict stored-plan reads for
T501-T503. It does not claim apply, recovery, restore, plan-set, overview, or
wizard acceptance. Task checkboxes remain unchanged.

## Decision and scope

The owner accepted all eight contract recommendations on 2026-10-25 in
[PR #283](https://github.com/wstein/sourcesteward/pull/283). Its exact-head
[Opus review](https://github.com/wstein/sourcesteward/pull/283#pullrequestreview-5645276090)
is the contract review, not an implementation review of this slice.

The expert path requires explicit contract 4 and both exact registration
selectors. It writes one immutable private plan. It does not unlock or remove a
registration, create recovery refs, copy recovery payloads, run repair, or start
an apply operation. Existing plan dispatch is preserved.

## Reproducible checks

Run with `umask 022` from the repository root:

```sh
go test ./internal/cli -run '^TestPruneV4CLI' -count=1
go test ./internal/workspace -run '^TestRegistrationPruneV4' -count=1
go vet ./internal/workspace ./internal/cli
go mod verify
python3 -B scripts/check-docs.py
python3 -B scripts/check-architecture.py
python3 -B -m unittest discover -s scripts -p 'test_*.py'
git diff --check
```

The focused native tests run on Linux. macOS execution and required CI remain
handoff gates; a successful local Linux run does not establish macOS coverage.
The full Python suite requires the repository's pinned Hugo executable for its
packaging test. An absent Hugo executable is a missing local check, not a pass.

## Tests and boundaries

| Behavior | Evidence |
| --- | --- |
| Exact singleton selectors; duplicate/legacy flags refused with usage reasons | `TestPruneV4CLIRejectsAmbiguousAndLegacyGrammar` |
| Real locked moved-checkout fixture; only private plan persisted | `TestPruneV4CLIStoresOnlyExactOwnedPlan` |
| Other-agent ownership, mismatched gitdir, present destination and missing lock refused with reason codes | `TestPruneV4CLIRefusesUnprovenOrChangedSelection` |
| Human other-owner confirmation binds owner and plan; refusal stores no additional plan | `TestRegistrationPruneV4HumanOtherScopeNeedsConfirmation` |
| Private modes, canonical bytes, native baseline round trip | `TestRegistrationPruneV4PlanRoundTrip` |
| Common-directory native mount, descriptor and path binding; changed observations refused | `TestRegistrationPruneV4CommonMountDetectsChangedObservation` |
| Missing false-valued field refused even when the typed digest would match | `TestRegistrationPruneV4ReaderRequiresZeroValuedFields` |
| Recomputed digests cannot admit invalid version, operation, witness, scope or baseline | `TestRegistrationPruneV4StrictReaderRefusesResignedInvalidPlans` |
| Duplicate/unknown keys, trailing data, wrong-case fields, invalid UTF-8, missing/symlinked/wide-mode records refused without recovery-store creation | `TestRegistrationPruneV4ReaderNeverFallsBackOrCreates` |
| Native plan and result match the closed schema; required fields cannot be omitted | `TestRegistrationPruneV4SchemaMatchesNativePlan` |
| Object-key ordering and domain-separated digests | `TestRegistrationPruneV4DigestDomainsAndCanonicalKeys` |

The schema stays source-only in the existing unpublished allowlist. Frozen
v2/v3 schemas are unchanged. The new schema defines only the plan/result source
slice; it does not publish the future overview, manifest, journal or restore
interfaces.

## Red-first and mutation evidence

The test-only commit `0f833967` records the original CLI behavioral RED before
contract-4 dispatch existed. Commit `82dc749c` records the missing required-field
RED. Commit `27c1c822` records the native schema-conformance RED before the new
schema existed. Commit `dc636e2e` records the unpublished-allowlist RED.
Other added native reader, witness and consent cases characterize the existing
planner behavior; they are not claimed as previously failing acceptance tests.

Four compile-valid source overlays were exercised locally, each producing a
failing assertion in its named test: bypass complete-shape comparison
(`TestRegistrationPruneV4ReaderRequiresZeroValuedFields`), bypass the second
common-directory mount comparison
(`TestRegistrationPruneV4CommonMountDetectsChangedObservation`), bypass the
native-state snapshot binding
(`TestRegistrationPruneV4StrictReaderRefusesResignedInvalidPlans`), and omit hash
domain bytes (`TestRegistrationPruneV4DigestDomainsAndCanonicalKeys`). These
four probes do not establish exhaustive mutation coverage of the admission
layer or the future apply flow.

## Shared planner and apply dependency

Claude owns issues #284 and #285. This slice adds no alternate chain-baseline
engine and changes no legacy retirement planner. It stores `V3NativeState` and
`Snapshot` using `observeTidyV3Git`, `v3NativeRoots`,
`qualifyTidyWorkspaceRoots`, and `v3SnapshotWithState`. The selected destination
gets only the already proven exact-absence exception; unrelated roots remain
qualified and protected.

Future prune apply must reuse #285's verified-prior-subplan baseline derivation
and existing `v3ApplyStateDelta`, rather than trusting arbitrary prior IDs or
weakening the stored baseline. Its final exported helper name is not yet known
because Claude's implementation is still pending. Rebase onto #284/#285 when
merged and name the actual shared helper in the apply PR.

Predicate 3 is an explicit gate: the new plan records a native
`common_dir_mount` witness and rejects changes during planning. Apply acceptance
must revalidate that witness immediately before recovery/removal boundaries.
A stored witness alone grants no apply authority.
