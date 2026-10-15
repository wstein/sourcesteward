# Validation Guide: Isolated Task Workspaces

Run in disposable directories only. Supported pinned Git and jj plus Linux bubblewrap or macOS
sandbox-exec are mandatory. Build `stw` using the repository Makefile. This is a validation guide,
not authorization to execute generated source scripts or delete user work.

1. Create private mode0700 state directory outside both source and destination; initialize a
   temporary ordinary Git repository, configure local test identity, and commit a small fixture.
2. Run `stw workspace plan SOURCE --base HEAD --destination DEST --task demo --owner tester
   --state-dir STATE --json`. Validate the emitted envelope and inspect targets/effects/recovery.
3. Run `stw workspace apply --plan ID --state-dir STATE --authorize --json`. Expect detached Git
   worktree at planned commit, durable task/journal and byte-identical source disk/index/config.
4. Retry apply. Expect already-complete only after ownership/native verification; no duplicate
   registration. Change base ref or occupy destination before a fresh plan's apply; expect refusal.
5. Run handoff and retire-plan on DEST with STATE. Expect owner/native/dirty qualifications and
   retention evidence; no removal and no clean-disk claim from stored jj identity.
6. Repeat with native standalone and colocated jj: native workspace at explicit base, distinct
   commit/change/operation identities, source unsnapshotted files preserved, no raw Git mutation.
7. Exercise hook/filter sentinels, ambiguous/GitButler ownership, unsafe state-dir/symlink aliases,
   missing tools/sandbox, malformed/oversized store data, cancellation, destination race, concurrent
   store writer and interruption after native creation. Verify effects/result/journal agree and
   foreign destinations are never adopted or removed.
8. Run `make check`, race tests, workspace schema tests and mandatory real native conformance
   through `scripts/smoke-workspaces.py` in both pinned version lanes. Missing fixtures/tools or
   skipped required cases fail acceptance. Record exact commands/revisions in verification.md.

The suite must independently snapshot source files/config/native refs and sentinels before/after;
asserting only command exit status is insufficient. Existing inventory/identity contract suites
must also pass. Preserve failure evidence; do not mark checks executed when merely planned.
