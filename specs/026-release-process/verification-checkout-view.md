# Spec 026 build checkout view verification

This records the source candidate of the private checkout view that `stw release build`
uses for its tracked-tree comparison (T031 `checkout-mismatch`, T034/SC-005 sentinel
boundary; FR-008, FR-016; #69). It is implemented, pending independent review. T031 and T034
stay unchecked in [tasks.md](tasks.md); no acceptance is claimed.

## What is implemented

- No-follow, descriptor-relative capture of the `.git` anchor, git directory, common
  directory, index (including a split-index base), symbolic metadata, absent ancestry and
  every configuration source, rendered into one frozen native configuration
  (`internal/release/build_checkout_view*.go`).
- A supervised native process group that retains its leader until the whole group and both
  output streams ended, `SIGKILL`s the group on cancellation or overflow, and holds a single
  capture lease.
- A trusted route that re-executes `stw` as `git` for nested gitlink status so it cannot
  freshen the source index; `cmd/stw` hands it over through `cli.RunPrivateRoute` before any
  command parsing, only for argv0 `git` with the private role variable.
- Wiring: `buildRelease` opens the view before the first project invocation, compares
  through it, and revalidates it after the build (a change of bound metadata or of the native
  configuration during the build is `checkout-mismatch`). The one-shot `checkBuildCheckout`
  that earlier tests call now lives in the test files and exercises the same view.
- Architecture: `internal/release/architecture_test.go` and `scripts/check-architecture.py`
  admit exactly the two observer files and the narrow selectors (`syscall.Stat_t`; Linux
  `Getpgid` call and `ESRCH`; Darwin `unix.KinfoProc`, `unix.SizeofKinfoProc` and a direct
  `unix.SysctlRaw("kern.proc.pgrp", ...)` call), with mutation tests on both sides.

## Mutation evidence (Linux, local)

Removing each guarded line makes a named test fail:

| Mutation | Result |
| --- | --- |
| final-symlink re-binding in `openBuildViewFile` removed | fails (symlink and swap tests) |
| `Mode` removed from `sameBuildViewIdentity` | fails (identity and chmod-only ancestor tests) |
| process-group `SIGKILL` removed | fails with a deadline message (no longer a package timeout) |
| post-build `view.check` removed | fails (`TestBuildReleaseRefusesCheckoutMetadataChangedDuringBuild`) |
| `cli.RunPrivateRoute` call removed from `cmd/stw` | fails (`TestRunEntersThePrivateRouteOnlyAsGitWithTheRole`) |
| observer rules widened (any syscall, wildcard file names, any unix selector, value use of `Getpgid`/`SysctlRaw`) | fail (architecture tests) |

Finding recorded by this work: removing `O_NOFOLLOW` or `O_NONBLOCK` from the open flags
alone is not observable on Go 1.27, because `os.Root` resolves in-root symlinks and handles
non-blocking opens itself. The flags remain as defense in depth; the guarantee is enforced and
tested by re-binding the opened descriptor to its name.

## Not verified

- Darwin: only `GOOS=darwin go vet ./internal/release`; the Darwin observer and the supervisor
  have never run on Darwin. Compile success is not Darwin acceptance.
- Windows: `internal/release` does not compile for Windows, because `build.go` (project
  runner, `Setpgid`) is untagged; this is the same on `origin/main` before this work and is not
  changed here.
- A real GitHub Actions checkout, a real product build and any workflow run: none was executed.
  All project invocations in the fixtures are fake.
- Independent review of the exact final head and the cross-artifact analysis.
- Owner review of the isolation chosen where [the build contract](contracts/build.md) named only
  the predicate (see its checkout comparison view section).
