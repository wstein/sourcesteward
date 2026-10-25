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
go test ./...
go test ./internal/cli -run '^TestEveryTidyFlagIsClassified$|^TestTidyFlagClasses$|^TestPruneV4CLI' -count=1
go test ./internal/workspace -run '^TestRegistrationPruneV4' -count=1
go vet ./internal/workspace ./internal/cli
go mod verify
python3 -B scripts/check-docs.py
python3 -B scripts/check-architecture.py
python3 -B -m unittest discover -s scripts -p 'test_*.py'
git diff --check
```

The native tests run on Linux. macOS execution and required CI remain
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

The test-only commit `73facde1` records the original CLI behavioral RED before
contract-4 dispatch existed. Commit `76583374` records the missing required-field
RED. Commit `94b39a8d` records the native schema-conformance RED before the new
schema existed. Commit `d4af76c5` records the unpublished-allowlist RED.
Other added native reader, witness and consent cases characterize the existing
planner behavior; they are not claimed as previously failing acceptance tests.

Four compile-valid source overlays were exercised locally, each producing a
failing assertion in its named test: bypass complete-shape comparison
(`TestRegistrationPruneV4ReaderRequiresZeroValuedFields`), bypass the repeated descriptor-level
common-directory mount observation
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

## Review corrections

The [first implementation review](https://github.com/wstein/sourcesteward/pull/287#pullrequestreview-5645530996)
found an unclassified CLI flag pair, reader validation gaps, and masked tests.
Both selectors are now explicitly classified and tested as expert flags.
The CLI uses the library's existing native-name rule, including backslashes on
Unix. The other-agent case asserts its exact ownership message so a later
terminal-confirmation refusal cannot substitute for the agent gate.

The reader now checks exact canonical file bytes plus one LF using the shared
bounded private store byte reader. Metadata paths must be local bounded text;
the ancestor chain starts at the filesystem root, advances one parent at a
time, and ends at the saved parent. Absent metadata requires an empty digest,
zero physical witness, and zero size, matching the source-only schema. No
published schema bytes or legacy reader dispatch change.

Re-signed single-defect plans exercise state/backend/mode/caller binding,
own/other authorization, ownership source, creation and journal digests,
absence bindings, native arrays, metadata path/order/kind/size/count, sibling
order/path, resolved HEAD, and sorted mode lists. Invalid UTF-8 is injected into
a recovery-limit string with the digest computed for the decoder's replacement
character, avoiding the old unrelated-operation defect. Reader refusals assert
the reason; missing-file tests assert the native not-found cause. Parent-path
replacement after the read is separately injected and refused.

Native fixtures cover a second locked missing checkout and a registered sibling
under the state directory. The extracted qualification helper also checks state
paths inside the source and common directory; moving a real ownership store
would otherwise stop at its earlier stored-plan binding. The helper is used by
the native planner and preserves the selected exact-absence exception.

Context-local injection points isolate common-directory path rebinding, the
cross-observation mount comparison, ownership changes, and expected evidence
or registration changes on the second and third observations. Failed native
mount reads are exercised on both common-directory samples. Refused planning
stores no additional plan. These hooks are private and add no CLI, plan, or
global mutable surface; production callers still use the native mount reader.

Commit `4e43cc95` adds reader cases that fail against the permissive reader;
`4889aee9` corrects it. The existing classification test also failed before
`9f29a196`. Existing refusal guards, newly injected native races, and the final
parent recheck are characterization/mutation evidence, not claims that the
original native guards were absent.
