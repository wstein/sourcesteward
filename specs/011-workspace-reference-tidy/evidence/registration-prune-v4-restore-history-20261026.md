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
