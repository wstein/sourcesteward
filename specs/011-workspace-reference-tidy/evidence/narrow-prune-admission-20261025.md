# Narrow prune: read-only admission slice

Owner requested implementation on 2026-10-25 after spec PR #268. This first
source slice covers read-only prerequisites of FR-013/T502; it does not complete
T502, T503 or T504 and changes no completion checkbox. The private observation
is not a stored plan and never authorizes removal. FR-014 recovery/apply/restore
and the FR-015 frozen-interface decision remain open.

## Delivered and bounded

`observePruneRegistration` reads the original completed creation plan, task and
journal through the existing read-only secure store. It requires exactly one
native registration name, the matching common repository, exact stw lock and
original gitdir/commondir mapping. It samples source safety/references/native
registration inventory twice and refuses another native prunable registration.
This is not the complete protected-sibling physical manifest needed by apply.

`observePruneAbsence` holds the no-follow directory descriptors for the ancestor
chain while observing the final component. Only a direct ENOENT proves absence;
other failures, present entries, aliased/unreadable ancestors, changed original
parent identity or changed sampled evidence refuse. The witness binds directory
device/inode/type/owner/mode and native mount identity. Linux uses statx mount ID;
Darwin uses the filesystem ID/type/mountpoint from fstatfs. Neither proves that a
checkout was deleted rather than moved or excludes same-user ABA/mount races.

`observePruneMetadata` reads an allowlisted registration tree, hashes binary
files with 64 KiB buffers, binds physical identities and records optional absence.
The independent ceilings are 64 MiB/file, 256 MiB total and 1,024 actual entries.
This reader neither uses nor widens the legacy 64 KiB metadata-text limit for
index/reflog contents. No raw recovery sidecars are written: digests cannot
restore an index and are not recovery-confirmed evidence.

No CLI, public selector, JSON schema/enum, stored prune plan, journal variant,
ref update, native repair, unlock, prune, registration removal or checkout write
is added. The moved-checkout fixture retains its files and registration. This
slice changes no existing tidy/wizard dispatch or refusal guard.

## Executed evidence

- The private unavailable seam preceded test-only a75693cc. The initial tests
  failed assertions against that seam. Two link fixtures initially collided with
  Git's existing ORIG_HEAD; removing that fixture file before creating each test
  link corrected setup. Those two setup failures are not semantic RED evidence.
- `go test -json ./internal/workspace -run '^TestPrune' -count=1`: 44 passing test
  identities, no failures/skips, 14.363 s on Linux/current Git. Later quota,
  metadata-kind, large-index and error-classification cases are characterization;
  they are not claimed individually RED-first.
- Eleven compiled Go-overlay mutants were assertion-killed: original-parent
  binding, mount witness comparison, exact ENOENT, selection cardinality, exact
  registration name, lock, unknown metadata, file kind and file/total/entry bounds.
  One initial mount probe had an unused import; that compile failure was excluded
  and the corrected compiled probe failed the intended assertion.
- Linux `go vet ./...`, Darwin/arm64 vet of workspace, module verification,
  formatting, architecture and documentation checks passed. Darwin vet is not
  native execution; minimum-Git and native Darwin acceptance remain unrun here.
- Python discovery ran 389 tests with one missing-Hugo error; 388 passed. The
  full check/native CI outcomes belong in the PR body, not an absent-check PASS.

Reproduce the guard probes by deleting the named comparison and running its
literal TestPrune case. Quota fixtures inject smaller private budgets for real
N/N+1 reads; a separate assertion pins production limits. No test performs a
prune on a user's repository, and no local race run or standalone build ran.

## Remaining implementation sequence

Resolve the exact versioned public/stored plan and restore interface before
choosing frozen-record extensions. Then implement the complete planner and
protected-sibling/mount proof, durable binary recovery sidecars and history pin,
metadata-only confined apply/revalidation/partial replay, separately authorized
restore/native repair and their failure/race matrices. Wizard classification,
plan-set wiring and T012b completion remain the final separate package.

## PR 273 review delta: F1 and F2

The original 44-test evidence above is historical. The current TestPrune selection
passes 66 identities without failures or skips (12.668 s). Per-call context hooks
inject changes between observations without sleeps, global callbacks or a public
interface. Existing defenses are characterization-tested with assertion-RED
compiled guard-removal overlays; this is not a claim that correct production
checks first failed on ordinary fixtures. The initial duplicate-selection fixture
expected the wrong refusal code; the native parser already rejects duplicate paths
with tidy-invalid-registration before the later count check. That setup/expectation
failure is excluded from RED evidence.

| Guard / injected change | Literal regression under TestPrune |
| --- | --- |
| Second absence sample: destination appears | ObservationInjectedRaces/second_absence |
| Fresh metadata comparison: same-size index edit | ObservationInjectedRaces/metadata_compare |
| Completed ownership journal changes | ObservationInjectedRaces/ownership_compare |
| Valid task recovery evidence changes | ObservationChangedTaskRecompare |
| Source reference added before final snapshot | ObservationInjectedRaces/source_snapshot |
| Pointer bytes differ from sampled digest | ObservationInjectedRaces/pointer_digest |
| Selected registration disappears before inventory | ObservationInjectedRaces/selection_disappears |
| Linked source names the selected registration | ObservationNativeBoundaryFixtures/linked_source |
| Parsed ownership backend is jj | ObservationInjectedRaces/backend |
| Absolute foreign common-directory pointer | ObservationNativeBoundaryFixtures/foreign_absolute_common |
| Pinned directory mode changes during the pass | AbsenceInPassRaces/mode_changed |
| Open ancestor renamed away from its path | AbsenceInPassRaces/ancestor_renamed |
| Leaf appears before final descriptor-relative stat | AbsenceInPassRaces/leaf_appears |
| Parent lacks directory search bits (0600) | AbsenceInPassRaces/directory_without_search_bits |
| File replaced between lstat and open | MetadataFileRaces/swapped_before_open |
| File truncated before reading | MetadataFileRaces/truncated_before_read |
| Same-size edit changes final physical facts | MetadataFileRaces/same_size_rewrite |

All 17 compiled guard-removal probes above fail assertions. A first task-comparison
probe did not compile because its unused variable was removed; it is excluded.
The corrected probe retains the variable use and fails the intended assertion.
Some guards provide defense in depth: tests pin the specific refusal stage/reason,
not a claim that removing that check always permits successful admission.

Two redundant clauses are removed with code comments: an exact worktrees/name
mapping already excludes the common directory itself, and the shared native
parser rejects duplicate paths so the selected count cannot exceed one.
The real duplicate registration fixture asserts tidy-invalid-registration;
the disappearing-registration fixture separately pins the remaining zero case.

F2 is explicitly OPEN implementation work: common-directory native mount binding
is not delivered. Native mount witnesses currently cover only the destination
ancestor chain. Registration/common-repository ownership and physical observations
do not substitute for a common-directory mount witness. Complete predicate 3,
including that witness and its revalidation, remains required before planner/apply
acceptance; this read-only slice authorizes no removal. This is a missing capability,
not a new owner decision or a completed safety proof.

Linux currently uses ordinary STATX_MNT_ID, whose IDs can be reused. The optional
unique-ID capability/fallback is not implemented here; physical rechecks reduce
but do not eliminate ABA/mount races. No remount test, native Darwin execution,
public schema change or task-completion tick is claimed. Final broad/native CI
results and static checks are recorded in the PR body.
