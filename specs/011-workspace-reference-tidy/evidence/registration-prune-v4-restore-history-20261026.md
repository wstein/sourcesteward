# Contract 4 administrative restore and history evidence

Status: candidate T506/T507 source implementation, pending exact-head review and
native CI. No task/progress checkboxes, T508 integration or owner-host acceptance
are claimed. Base: `30b60d7986932b9f0a7c309f6183cd27bcf67d41`.

## Scope and contract boundaries

The accepted [narrow prune contract](../contracts/narrow-registration-prune.md)
and [contract 4](../contracts/registration-prune-v4.md) remain authoritative.
Administrative restore is explicit and separate from the original prune and
from owner-run native repair. It reuses strict immutable plan/journal readers,
complete bounded payload verification, original ownership records, native
observations and common-directory/destination witnesses. No new dependency,
remote operation, cleanup policy or recovery representation is introduced.

Only completed prunes with verified recovery are eligible. Incomplete prune or
restore attempts refuse automatic retry. A completed restore also refuses
another restore: the original name is occupied and the closed journal cannot
prove the identity of a newly recreated directory. Matching bytes are not
identity proof. This avoids adding a new stored identity field or claiming an
unprovable verified replay.

Restore stages raw files in the common directory at
`.stw-registration-restore-<restore-id>` and journals that independently generated
ID under `tidy-registration-v4/restores`. Directories are private while staging;
nested metadata modes are restored before publication; the root stays 0700 until
publication and then receives its original mode through the held descriptor. Linux uses `RENAME_NOREPLACE`;
macOS uses `RENAME_EXCL`, both relative to held descriptors. Unavailable safe
publication fails without overwrite. A partial outcome retains the restore
journal, complete recovery and any staging path; the result names that residual.
No automatic rollback, manual-file transcript or checkout-file restoration is
claimed. Repair guidance is escaped argv data with a found-path placeholder.

History/listing selects optional PATH or the current directory; the state
directory alone does not select a repository. It filters plans from other
repositories while charging their reads to the same scan budget. Totals count
matching records examined within directory/byte bounds and can be a lower bound
when truncated. Listing reads plans and prune journals only, with strict
version/shape checks, record identities and a bounded scan/output. It never reads raw payload
bytes or treats a pointer as verified recovery. A recorded payload has status
`unknown`; restoration inspection verifies actual eligibility separately. The
count limit defaults to 256 and is capped at 1024. Truncation is successful,
explicit and never authorizes omitted records. Encoded records are bounded at
1 MiB before allocation, rather than inheriting the legacy reader's larger limit.
Restore journals are not listed: history cannot identify complete or partial
restores. Preserve the restore result's ID and journal pointer for inspection.

Native maintenance may remove the empty native `worktrees` parent after the last
registration is pruned. Restore refuses a missing/replaced parent with
`prune-registration-parent-changed`; it never recreates it or relaxes the bound
identity. The retained payload remains available for separately reviewed manual
recovery. Applying restore also retains exact stored caller/mode equality,
including volatile editor, agent and terminal markers. A later shell with
different evidence is refused even when the owner is unchanged.

New plan capability wording now describes administrative restore accurately.
Because `recovery_limits` is digest-bound, this changes newly generated plan
digests. Existing immutable stored plans retain their bytes and original digest;
no migration, relabeling or rewrite occurs. Published v2/v3 schemas are unchanged;
the separate unpublished v4 schema adds restore/history shapes.

## Test-first checkpoints

| Checkpoint | Observed result |
| --- | --- |
| `a5758535` restore API tests | RED: restore API did not exist. The fixture Git helper typo was corrected in `dffccfe6` before the backend ran. |
| `dffccfe6` CLI entry test | RED: contract 4 restore returned usage 2, requiring contract 3. |
| `e27f490a` history/list tests | RED before backend/CLI dispatch existed. |
| `039cd2b7` encoded record limit | RED at 1 MiB plus one byte before the narrow ceiling fix. |
| `7ae6a1af` replay ownership/cancellation/payload tests | RED: stale ownership and changed restore payload digest were accepted; cancellation text described planning instead of possible restore effects. |
| `c561ea81` schema test | RED: the old v4 schema rejected real restore results and journals. |
| `b45b7348` slot/completion tests | RED: a new directory with identical bytes was accepted as replay; completion-failure injection was not yet exercised. |
| `80d36593` private staging | RED: the staging root became 0755 before publication. |
| `251f7c13` repair/human output | RED: successful apply omitted repair argv and human output omitted limitations. |

The direct no-replace test is characterization of the chosen native primitive;
an ordinary-rename mutant fails it. Other-plan vocabulary and partial-journal
checks include independent causal probes against the first backend candidate;
the final tests also pin them. No claim that every added test was independently
RED is made.

## Requirement and test map

| T506/T507 acceptance unit | Source test |
| --- | --- |
| Read-only inspection, no registration creation | `TestRegistrationPruneV4RestoreInspectDoesNotWrite` |
| Exact metadata and separate moved-checkout repair, index/lock retained | `TestRegistrationPruneV4RestoreRawMetadataAndRepairMovedCheckout` |
| Separate authorization, name/path collision, bad payload/pin and mode | `TestRegistrationPruneV4RestoreRefusalReasons` |
| Publication race and actual no-replace behavior | `TestRegistrationPruneV4RestorePublicationDoesNotReplace`, `TestRegistrationPruneV4RestoreNoReplacePrimitive`, `TestRegistrationPruneV4RestoreStagingRemainsPrivate` |
| Fresh original ownership on repeated restore | `TestRegistrationPruneV4RestoreReplayRechecksOwnership` |
| Restore payload digest binding and occupied same-bytes replacement refusal | `TestRegistrationPruneV4RestoreJournalBindsPayload`, `TestRegistrationPruneV4RestoreRefusesReusedCompletedSlot` |
| Cancellation and durable readable partial completion | `TestRegistrationPruneV4RestoreCancellationKeepsIntent`, `TestRegistrationPruneV4RestoreCompletionFailureRetainsReadablePartial` |
| Closed other-plan journal vocabulary | `TestRegistrationPruneV4RestoreReaderRejectsOtherPlanVocabulary` |
| Real schema-compatible complete, inspection, partial and history output | `TestRegistrationPruneV4RestoreSchemaNativeRecords`, `TestRegistrationPruneV4RestoreSchemaPartialResult`, `TestRegistrationPruneV4HistorySchemaNativeResult` |
| Escaped repair guidance for inspect/apply and human limitations | `TestRegistrationPruneV4RestoreRepairArgvForInspectAndApply`, `TestTidyPruneV4RestoreHumanDisclosesLimitations` |
| Colocated ownership refuses without protected-state writes | `TestRegistrationPruneV4RestoreRefusesColocatedOwnershipWithoutWrites` |
| Actual command dispatch and usage refusals | `TestTidyPruneV4RestoreExplicitDispatch`, `TestTidyPruneV4RestoreRejectsMixedGrammar`; history tests in `tidy_prune_v4_history_test.go` |

Existing prune ownership/mount/absence/native-state and payload tests remain
applicable; they do not substitute for new restore-specific concurrency probes.
The new mutation path also revalidates these witnesses before publication.

## Verification record and remaining evidence

The fixture runs use temporary repositories on Linux with native Git 2.56.0.
They never prune or repair a real user repository. Focused backend/CLI/schema,
full Go suite, vet, module, docs, script and platform check results are recorded
in the PR body once completed. No local `/tmp` log is required as review evidence;
commands and test names reproduce the source checks.

```sh
umask 022
go test ./internal/workspace ./internal/cli -run 'TestRegistrationPruneV4|TestTidyPruneV4|TestTidyRestore|TestTidyHistory' -count=1
go test -timeout 40m ./...
go vet ./...
go mod verify
GOOS=darwin GOARCH=arm64 go test -exec=true ./internal/workspace ./internal/cli
python3 scripts/check-docs.py
python3 scripts/selfcheck.py origin/main --require-trailer
git diff --check
```

Darwin compilation is not macOS execution. Native macOS restore/repair and Git
2.29 minimum-version conformance are not claimed locally. Required native CI,
exact-head review and those platform limits remain explicit at handoff. The
accepted no-Git-fallback refusal applies to jj/colocated ownership; listing uses
the existing backend admission boundary and restore uses the bound native plan.

T508 remains separate: overview-4 opt-in, manifest-2 hashing/order/dispatch and
wizard T012b require their own reviewed integration after expert restore merge.
That slice must consume the shared `loadV3Chain` / `v3Chain.apply` derivation for
sibling dispatch, rather than mutating saved baselines. No wave-closing ticks
belong to this PR.

## Opus review correction round

The posted [review of b6f1adaf](https://github.com/wstein/sourcesteward/pull/295#pullrequestreview-5648008600)
identified shared-store filtering, causal restore-guard coverage and disclosure
gaps. The correction keeps the accepted authorization and identity rules.
Tests assert diagnostic code and message as well as refusal/partial status,
so a later rejection cannot mask a missing gate. Private observation hooks
exercise changes between validation steps without adding public options.

Each mutation below compiled and failed its named guard test. Reproduce by
disabling only that guard with a Go overlay and running
`go test ./internal/workspace -count=1 -run '^TestRegistrationPruneV4RestoreGuard'`.
The unmodified guard suite passed before the probes (61.3 s). These are targeted
mutation checks, not an exhaustive concurrency proof.

| Disabled guard | Failing test suffix after `TestRegistrationPruneV4RestoreGuard` |
| --- | --- |
| other-owner | `OtherOwnerConfirmation` |
| incomplete-prune | `IncompletePrune/partial` |
| prune-journal-digest | `JournalDigestBinding` |
| restores-private | `PrivateRestoreDirectory` |
| again-under-lock | `RechecksPriorUnderLock` |
| common-root-id | `HeldDescriptorBindings/root` |
| parent-id | `HeldDescriptorBindings/parent` |
| staged-id | `HeldDescriptorBindings/staged` |
| prepublication-revalidate | `RevalidatesBeforePublication/ownership` |
| staged-verification | `RevalidatesBeforePublication/staged-bytes` |
| copy-size-digest | `CopyChecksBytesAfterPayloadRead` |
| metadata-mode | `VerifiesRestoredModes` |
| post-mount | `PostMountAndSiblings/mount` |
| post-sibling | `PostMountAndSiblings/sibling` |
| pre-ownership | `PrepublicationOwnership` |

Separately removing only the private restore directory owner check or only its
0700 mode check also compiled and failed `PrivateRestoreDirectory`: 17 restore
guard probes were caught in total.

The shared-store fixture contains two independently created native repositories.
Three further compiled mutations failed: inverted repository filter, counting
foreign plans in totals, and omitting plan bytes from the scan budget. An invalid
foreign plan still refuses with its specific reason before repository filtering.
Integrated history/list tests passed (workspace 24.124 s; CLI 0.040 s).

Before this round, the full Go suite passed at b6f1adaf (workspace 1449.055 s).
Its enclosing committed-pristine `make check` later failed only because the
Hugo-dependent script test could not find `.tools/hugo` (399 tests, one error).
The review-round script rerun had the same sole missing-tool error (5.951 s).
The earlier full-suite result does not claim a full-suite run on the correction
head. Native CI and the posted delta review remain required for that head.

Affected restore checks also passed before integration: diagnostic regressions
17.8 s, existing restore regressions 77.5 s and CLI restore checks 9.8 s.
The diagnostics and guard cases were observed failing before their corrections;
the corresponding test/code fixes are committed together rather than presented
as test-only RED commits. Integrated vet and Darwin arm64 test compilation
passed. Compilation is not native macOS execution. Spelling checks pass on all
PR-changed files; a whole-repository spelling run reports pre-existing findings
in untouched files, which this package does not change.

The integrated affected-suite run completed in 172.855 s for workspace and
10.840 s for CLI. Workspace had one failure: the partial-result schema test
still expected the former generic refusal code. Its assertion now checks
`prune-registration-occupied` and the exact occupied-slot reason; the corrected
test passed in 3.265 s. All other affected tests passed in that run. The complete
affected suite was not rerun after this test-only assertion correction; native
CI remains the full current-head verification.
