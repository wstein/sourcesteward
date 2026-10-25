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
