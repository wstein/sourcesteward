# Contract-4 expert apply and retained recovery evidence

Scope: the apply/recovery source slice of T504-T505 for FR-013 through FR-015.
This record does not close T504-T508 or claim native acceptance on every platform.
T506-T507's administrative inspect/restore/history work and T508's overview,
manifest-2 and wizard integration remain open. No task or progress ticks change.

## Dependency and implementation

This slice depends on the shared chain-baseline helper in SourceSteward PR #289,
merged as `72c0431872a34229aca3d3740563f3e6078f4be5`. It calls
`loadV3Chain` and
`v3Chain.apply` before comparing the immutable stored baseline; it does not
implement a second legacy journal/delta engine. The expert singleton operation
has no new sibling-selection flag. Manifest-2 dispatch and chaining between new
prune sub-plans remain part of the later integration slice.

The explicit expert interface is `tidy apply --contract 4 --plan PLAN_ID
--state-dir DIR --authorize [--json]`. Positional plan-set application keeps its
existing dispatch. Missing effect authorization returns a structured refusal;
usage errors and operational refusals retain the existing CLI exit distinction.
Administrative restore is unavailable in this slice. Retained payloads are
recovery evidence, not a claim that the restore command is implemented.

Apply writes a private durable intent and complete binary sidecars before
creating the exact absent-name history pin. It verifies recovery before every
metadata unlink. Files are streamed in buffers of at most 64 KiB; the accepted
64 MiB/file, 256 MiB total and 1,024-entry quotas are unchanged. Payload and
journal records use only the unpublished version-4 schema. Frozen overview-v2
and tidy-v3 schema files are unchanged.

Removal uses administrative-directory descriptors only. It never calls broad
prune, unlocks the registration, removes a checkout or automatically repairs a
moved checkout. Every effect gets a durable intent and observed outcome; changed
ownership, source safety/references, metadata, absence or common-directory mount
stops removal. Interruption retains partial evidence. Completed replay verifies
postconditions and recovery instead of repeating removal. These are sampled
checks, not an atomic transaction against arbitrary same-user writers or ABA
changes. Retained recovery has no automatic expiry.

## Reproducible tests

- `go test ./internal/workspace -run '^TestRegistrationPruneV4' -count=1`
- `go test ./internal/cli -run '^TestTidyPruneV4Apply' -count=1`
- `GOOS=darwin GOARCH=arm64 go test -exec=true ./internal/workspace ./internal/cli`
  verifies Darwin test compilation only; it does not execute macOS tests.
- `go vet ./...`, `go mod verify`, documentation/architecture/theme checks,
  `git diff --check`, and the author self-check are required at handoff.
- `make check` runs on a committed tree. The script suite's known local limitation
  is the absent `.tools/hugo` executable: 399 tests run, with one error in the
  Hugo-dependent documentation test. No standalone Hugo build is authorized or
  substituted. Native CI remains the required integration/platform evidence.

Measured local tools: Git 2.56.0 and Go 1.27.2 on Linux arm64. No minimum-Git,
macOS execution, real-user cleanup or manual recovery run is claimed here.

Red-first commits cover the missing API, explicit CLI dispatch, exact recovery,
closed journal vocabulary, changed source/ref checks, schema records, quotas,
and the independently reproduced journal-binding/partial-report gaps.
Late-mount interruption and symbolic-pin cases characterize existing guards;
they are not presented as newly failing behavior.

## Guard mutations

Use Go compiler overlays to replace one guard at a time, without changing the
checkout. All six probes below compiled and failed behavior tests. A compiler
failure alone is not a killed mutation.

| Removed guard | Killing test |
| --- | --- |
| Apply authorization | `TestRegistrationPruneV4ApplyRequiresAuthorization` |
| Per-file payload quota | `TestRegistrationPruneV4PayloadExactBounds` |
| Total payload quota | `TestRegistrationPruneV4PayloadExactBounds` |
| Exact sidecar inventory | `TestRegistrationPruneV4PayloadRejectsExtraSidecar` |
| Per-effect common mount comparison | `TestRegistrationPruneV4LateMountAndPartialRemoval` |
| Per-effect exact source references | `TestRegistrationPruneV4SourceChangeBeforeRemoval` |

## Posted review corrections

The [Opus review of de3a6664](https://github.com/wstein/sourcesteward/pull/290#pullrequestreview-5646624309)
identified F1-F5. Appended test-only RED commits reproduce the partial-schema,
removal-budget and final-descriptor gaps before their behavior fixes; the CLI
help correction also has a test-only RED commit. Existing-guard tests are
characterization tests, not newly failing production behavior.

- Schema validation now covers complete and partial results, partial replay,
  partial journals, recovery manifests and a post-plan refusal. Only unpublished
  schema 4 changes; partial output retains its typed plan and recovery pointers.
- Before the first unlink, apply measures a full source/payload verification
  pass and admits removal only when the remaining deadline covers a conservative
  six-pass allowance per non-absent entry plus one second for durable records.
  The short-budget test models a slow pass without sleeps and requires zero
  unlinks with `prune-metadata-limit`. This is admission based on sampled cost,
  not protection against a later disk stall; a later interruption still reports
  partial work and retained recovery.
- The immutable expected state includes `registration_parent`. Apply compares
  the held administrative parent against that witness and checks its final
  name with `os.SameFile` against the held registration before `rmdir`.
- Apply help and invalid-contract messages explicitly describe contracts 3 and 4.

Run `go test ./internal/workspace -run '^TestRegistrationPruneV4Guard' -count=1`
for the added guard matrix. Its integrated run passed in 126.279 seconds; the
preceding contract-4 suite passed in 192.711 seconds and affected CLI checks in
6.367 seconds. The suite and guard matrix ran separately; this is not a claim
that an earlier suite run included tests added afterward.

The 16 review-guard mutation probes below compiled and failed their named tests
in the isolated test-author checkout based on de3a6664. The new guard tests also
pass against the integrated implementation. M21 and three new budget/descriptor
probes were executed against the integrated implementation; all four compiled
and failed behavior tests. No compiler failure counts as a killed mutation.
Reproduce with a Go overlay replacing only the named predicate and the listed
`TestRegistrationPruneV4Guard` suffix (or the full test name where given).

| Probe / removed predicate | Killing test suffix |
| --- | --- |
| M13 other-owner confirmation | `OtherOwnerRequiresFreshConfirmation` |
| M5 completed-replay verification | `CompletedReplayVerifiesCurrentState/registration` |
| M23 exact native post-state | `PostNativeAbsenceAndSiblingChecks/native` |
| M16 post-removal absence | `PostNativeAbsenceAndSiblingChecks/absence` |
| M4 post-removal siblings | `PostNativeAbsenceAndSiblingChecks/sibling` |
| M3 boundary siblings | `MidRemovalStopsAtChangedEvidence/sibling` |
| M24 verified-chain sibling filtering | `VerifiedSiblingChainRemovesOnlyExplainedWitness` |
| M9 boundary destination absence | `MidRemovalStopsAtChangedEvidence/destination` |
| M2 ownership digest comparison | `MidRemovalStopsAtChangedEvidence/ownership` |
| M18 per-file digest/physical comparison | `MidRemovalStopsAtChangedEvidence/file` |
| M20 held registration identity | `HeldRegistrationIdentity` |
| M12 absent-name old OID | `HistoryPinRequiresAbsentName` |
| M6 journal history OID binding | `JournalBindsOIDAndConfirmedOutcomes/history-oid` |
| M25 complete journal confirmed outcomes | `JournalBindsOIDAndConfirmedOutcomes/last-outcome` |
| M7 lock-last ordering | `LockIsPenultimateEffect` |
| M10 private payload-root mode | `PayloadRootMustStayPrivate` |
| M21 second boundary after durable intent | `AfterIntentRechecksBeforeEffect` |
| Removal-budget gate | `TestRegistrationPruneV4RemovalBudgetBeforeFirstUnlink` |
| Administrative-parent comparison | `TestRegistrationPruneV4HeldParentMatchesPlan` |
| Final parent-entry/held-root comparison | `TestRegistrationPruneV4FinalRmdirBindsHeldRegistration` |

The sibling fixture preserves a live sibling's complete bytes and unrelated
references. Existing admission refuses a second already-missing registration;
that behavior is unchanged. The chain fixture plans a sibling while its checkout
is present, applies its verified legacy retirement, and proves that only that
explained sibling is filtered from the immutable prune baseline. No production
admission rule is weakened to manufacture a two-missing-registration fixture.

F6's historical commit-type nit is not rewritten: the review explicitly allows
leaving it when no other rewrite is needed. F7's sampled same-user race,
fail-closed temporary-record residue and lack of singleton CLI sibling dispatch
remain disclosed limitations. No new scope or completion tick is introduced.

## Cross-artifact analysis

FR-013/FR-014's absence, ownership, recovery-before-removal and confinement rules
map to the apply/recovery tests and existing planner/admission fixtures.
FR-015's separate version and unchanged frozen contracts map to explicit CLI
and schema tests. SC-002 maps to refusal, partial-removal and retained-payload
fixtures; SC-003 maps to real record schema validation; SC-001/SC-004 retain
native-version/platform and final acceptance gates.

The older contract/task text still calls the accepted representation proposed
and the common-directory mount witness an open implementation gate. Owner
acceptance is recorded on PR #283, and the planner witness landed in PR #287.
This analysis records the stale wording without editing the accepted contract
or marking those acceptance tasks complete. Apply now binds and rechecks that
witness as well; current native CI and review remain separate readiness gates.
