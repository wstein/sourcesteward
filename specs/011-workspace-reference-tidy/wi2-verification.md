# WI2 Verification: Authorized Local Reference Cleanup

## Scope and source
Native local Git branch/jj bookmark deletion only through freshly generated plans, exact proposed
effects, explicit authorization and durable native recovery. Old WI1 observation-only plans refuse.
No workspace removal, remote deletion intent, jj abandonment, GC or automatic rollback.
Published tidy v1 bytes remain SHA256 `7b32f169e81b5351c9b542af7b09025a068279de98f3441ec8966d1239ec1161`.

Plan/tasks preceded independent analysis, including the private jj execution-context amendment
cleared at 0538311. Test-first authors used isolated worktrees. Independent source/security review
cleared integrated 73eb1b9. Reviewed fixes cover hardlinked mutable Git metadata, exact symbolic
reference targets, bounded strict journals, stable source/base guards and preserved recovery pointers.

Native jj colocated bookmark commands ordinarily reset Git HEAD/index, even with ignore-working-copy.
The reviewed bounded private execution context copies only known native working-copy metadata,
points to the existing shared repository, and remains read-only during native execution. There is no
native initialization/registration or raw Git fallback. Recovery bookmarks use a Git-invalid suffix
so they cannot auto-export; actual source Git/WC/configuration remain read-only.

## Executed checks
- Integrated make check and go test -race ./... passed with current native Git/jj and required
  sandboxing: Go tests/vet/module verification, 52 Python tests, 12 JavaScript tests, docs/import/diff.
- Real native Git/jj apply CLI outputs and failure/retry fixtures validate unchanged tidy v1.
- smoke-tidy-apply.py passed all 24 native cases per lane: minimum Git 2.46.0/jj 0.45.1 and
  current Git 2.56.0/jj 0.46.0, mandatory executables/sandbox. Independent native reference maps
  allow only the exact selected target deletion/recovery root; protected source files remain equal.
- Current/minimum native author tests passed; jj standalone/colocated source Git refs/index/WC
  bytes remain unchanged, and operation sole-parent/full bookmark deltas are checked.
- Final harness configuration assertion strengthened after running lanes; its four unit tests pass.
  Hosted mandatory lanes will also exercise that assertion. Initial fixture add-with-ignore failure
  was corrected to native fixture setup, then both full lanes passed; production code was unchanged.
- Owner merged WI1 PR36. WI2 was rebased onto origin/main 81c5f07 with a retained backup;
  tracked implementation tree was identical across rebase. Later edits are design/evidence/harness only.

## Limits and handoff
Local macOS evidence does not establish hosted Linux acceptance; exact-head source and both
native CI lanes remain required. External/ABA races are sampled; Git target CAS is not a full
transaction and jj operation-parent checks are not CAS. Incomplete/uncertain operations refuse
replay; inspect the durable journal and exact predeclared recovery root manually. A commit pin
preserves reachable history, not arbitrary filesystem/configuration metadata. Remote-bound jj
bookmarks retain. Private recovery roots are not automatically collected. WI3 removal remains planned.
