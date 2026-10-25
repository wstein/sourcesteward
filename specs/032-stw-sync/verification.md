# Spec 032 verification

`stw sync` was delivered in slices by pull requests #120, #219, #220, #222, #234, #236, #246, #248, #249 and #250 (the review
thread of each holds its findings and verdict), with the missing tests of T009, T016 and T018 in #264 and the acceptance
material of this document in the closing pull request. `docs/feature-status.json` records the feature as `implemented`.
The hosted minimum and current Git lanes run `scripts/smoke-sync.py` through `.github/workflows/conformance.yml`; their
result is the CI of the pull request, not a claim made here.

## Source smoke

`python3 -B scripts/smoke-sync.py --binary <scratch binary built outside bin/>` runs the real `stw` against disposable
local repositories (bare remotes reached through file paths, `GIT_ALLOW_PROTOCOL=file`, a private HOME and global Git
configuration, a fixture directory name with a space, non-ASCII characters and a quote). It passed 15 cases:

| Case | What it asserts |
| --- | --- |
| `S.plan-readonly`, `S.plan-json` | a plan leaves heads, tags, stash, HEAD, status, files and the worktree list unchanged, moves only the remote-tracking ref, writes no journal, `run_id` is null, the root result is `fast-forward` |
| `S.fast-forward-journal`, `S.no-hook-installed` | the run fast-forwards, writes the journal record and a recovery ref at the old tip, and the hook directories of the root and the submodule stay byte-identical, also after every later case |
| `S.no-merge-without-flag`, `S.hostile-global-config`, `S.merge-flag` | a diverged branch is `diverged` (`needs a choice`) with no merge commit, also under a global configuration with `pull.rebase=false`, `pull.ff=false`, `merge.ff=false`; `--merge` creates exactly one merge commit |
| `S.submodules` | a plain run reports the pinned submodule as `skipped-detached`; `--submodules` updates it to the recorded commit and no gitlink changes |
| `S.restore`, `S.restore-refuses-sync-flags` | restore returns the root branch to the old tip, a rerun is `already-restored`, the submodule stays; five sync flags given to restore exit 2 with no output |
| `S.usage-errors`, `S.busy`, `S.unsupported-path` | contradictory or unknown options exit 2; a merge in progress is `skipped-busy` in a run and a plan with refs, files and `MERGE_HEAD` unchanged; a directory with only `.jj` is refused with `sync-unsupported-backend` |
| `S.quoted-path`, `S.missing-binary` | the paths in the result are the quoted fixture paths; a missing binary is a failure, not a skip |

A stand-in binary that exits 0 without output fails the acceptance (`test_sync_smoke.py`). Three stand-in wrappers of the real
binary were run against the script and each is rejected: one that creates a branch during a plan, one that always adds
`--merge`, one that writes a hook file.

## A real run on a throwaway fixture

One transcript of the commands above, pasted from the program output (git 2.56.0; commit ids and the run ID differ per run):

```
$ stw sync --plan --submodules
SourceSteward sync plan: 2 repositories; only remote-tracking refs were fetched
.    main  would fast-forward 1 commit 5cec256..8e0ff92
sub  -     skipped: detached at c634373 (pinned)
summary: 1 fast forward, 1 skipped detached
[exit 0]

$ stw sync --submodules
SourceSteward sync: 2 repositories
.    main  fast-forwarded 1 commit 5cec256..8e0ff92
sub  -     submodule updated to the recorded commit c634373..79d43a9
summary: 1 fast forwarded, 1 updated
journal: run 20261025T135346497099Z-55955a66, recovery refs under refs/stw/recovery/sync/20261025T135346497099Z-55955a66
[exit 0]

$ stw sync restore --submodules
Error: --submodules does not apply to stw sync restore
[exit 2]

$ stw sync restore
SourceSteward sync restore: 1 branch
.  main  restored 8e0ff92..5cec256
summary: 1 restored
journal: run 20261025T135346497099Z-55955a66, recovery refs under refs/stw/recovery/sync/20261025T135346497099Z-55955a66
[exit 0]

$ stw sync restore
SourceSteward sync restore: 1 branch
.  main  already restored
summary: 1 already restored
journal: run 20261025T135346497099Z-55955a66, recovery refs under refs/stw/recovery/sync/20261025T135346497099Z-55955a66
[exit 0]
```

## A plan on the live workspace

`stw sync --plan /workspaces/Anchorage` (the plan fetches remote-tracking refs, the one change it makes). Before and after,
`git status --porcelain=v2`, `git worktree list`, `git submodule status`, the local branch refs, the stash ref and HEAD of
the workspace root and of every submodule were captured (1090 lines) and are identical. Output:

```
SourceSteward sync plan: 7 repositories; only remote-tracking refs were fetched
.              main  up to date
bundledock     main  up to date
chartscribe    main  up to date
crewbook       main  up to date
sheetmill      main  up to date
sourcesteward  main  would fast-forward 39 commits ad83876..6ba0b06
workharbor     main  up to date
summary: 1 fast forward, 6 up to date
```

A real `stw sync` was not run on the live workspace.

## Open items of the spec

Item 1 no longer blocks and item 2 is answered in the spec (see its open items); both were reviewed with the closing
pull request of the spec.

## Not exercised

The hosted lanes before the pull request's CI, a Windows host, a Git older than the minimum lane, and the jj paths (jj is
refused by design).
