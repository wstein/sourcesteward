# Spec 032 verification

`stw sync` was delivered in slices by pull requests #120, #219, #220, #222, #234, #236, #246, #248, #249 and #250 (the review
thread of each holds its findings and verdict), with the missing tests of T009, T016 and T018 in #264 and the acceptance
material of this document in the closing pull request #265. `docs/feature-status.json` records the feature as `implemented`.

## Source smoke

`python3 -B scripts/smoke-sync.py --binary <scratch binary built outside bin/>` runs the real `stw` against disposable
local repositories (bare remotes reached through file paths, `GIT_ALLOW_PROTOCOL=file`, a private HOME and global Git
configuration, a fixture directory name with a space, non-ASCII characters and a quote). It passed 21 cases. Each row says
what the assertions of the script check, nothing more:

| Case | What it asserts |
| --- | --- |
| `S.plan-readonly`, `S.plan-json` | after a plan every ref except the remote-tracking ones, HEAD, status, the working files and the worktree list are unchanged, the remote-tracking ref moved, no journal and no `refs/stw` ref exists; the result has `mode` `plan`, a null `run_id` and `fast-forward` for the root |
| `S.fast-forward-journal`, `S.no-hook-installed` | the run fast-forwards to the upstream tip, writes the journal record named by `run_id` and a recovery ref at the old tip; the hook directories of the root and the submodule are byte-identical to the start, after this case and again after the later ones |
| `S.no-merge-without-flag`, `S.hostile-global-config`, `S.merge-flag` | a diverged branch is `diverged` with `needs a choice`, HEAD unchanged, no merge commit, also under a global configuration with `pull.rebase=false`, `pull.ff=false`, `merge.ff=false`; `--merge` ends with `merged` and exactly one merge commit |
| `S.submodules` | a plain run reports the pinned submodule as `skipped-detached`; `--submodules` reports `updated`, the submodule is at the recorded commit and `git ls-files --stage` of the root is unchanged |
| `S.restore`, `S.restore-refuses-sync-flags` | restore returns the root to the commit before the run and names that run, the submodule stays, a second restore exits 0 with `already-restored`; each of five sync flags given to restore exits 2, prints nothing on stdout and says `does not apply to stw sync restore` on stderr |
| `S.usage-errors` | five invalid option sets exit 2 with nothing on stdout and an `Error:` line on stderr |
| `S.busy` | a merge in progress is `skipped-busy` in a run and in a plan, exit 0, with local refs, HEAD, status, working files, the state files of the Git directory (`HEAD`, index, `ORIG_HEAD`, `MERGE_*`, `packed-refs`) and every ref file unchanged |
| `S.unsupported-path` | a directory with only `.jj` is refused: exit 1, `error.code` `sync-unsupported-backend`, no `repos` |
| `S.quoted-path` | the paths in a plan, a run with `--submodules` and a busy run equal the fixture paths byte for byte (space, `ü`, quote) |
| `S.missing-binary` | a missing binary is a failure, not a skip |
| `S.rebase` | `--rebase` and a global `pull.rebase=true` each end with `rebased`, no merge commit, the local commit on top of the upstream tip |
| `S.rebase-merged-upstream` | the branch was rebase-merged upstream (same change, new commit id, the remote branch deleted): without a flag `diverged`; with `--rebase` `rebased`, HEAD equals the upstream tip, the file is kept and no merge commit exists |
| `S.prune` | without `--prune` a stale remote-tracking ref stays and `pruned` is 0; with `--prune` it is removed, `pruned` is 1, and the local branches `main` and one created from the stale ref are still there |
| `S.autostash` | with a dirty tree `--rebase` is `skipped-dirty` and nothing changes; `--rebase --autostash` ends with `rebased`, an empty stash list and the edit restored |
| `S.autostash-conflict` | `--autostash` with a conflicting edit exits 1 with `autostash-conflict`, `stash_oid` equal to `refs/stash`, HEAD at the upstream tip and one stash entry |
| `S.rebase-conflict` | `--rebase` on a conflicting change exits 1 with `conflict`, a reason naming `git rebase --continue`, and the rebase left in progress; `git rebase --abort` of the fixture returns to the old tip |

The unit tests of the script (`scripts/test_sync_smoke.py`) cover the manifest (21 unique IDs), the failure of a missing
git or binary, a binary that prints nothing (a `JSONDecodeError` at the first result), a binary with an incomplete result,
the `require_complete` check and that the acceptance calls it, and the comparison helpers. Four stand-in wrappers of the
real binary were run against the script and each is rejected: one that creates a branch during a plan, one that always adds
`--merge`, one that always adds `--prune`, one that writes a hook file.

## A real run on a throwaway fixture

One transcript of the commands of a throwaway fixture (a root with a pinned submodule behind its recorded commit, the root
behind its upstream; git 2.56.0; the fixture script is not committed; commit ids and the run ID differ per run):

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
the workspace root and of every submodule were captured (1090 lines) and are identical. Output, with the abbreviated commit
ids replaced by `<old>..<new>`:

```
SourceSteward sync plan: 7 repositories; only remote-tracking refs were fetched
.              main  up to date
bundledock     main  up to date
chartscribe    main  up to date
crewbook       main  up to date
sheetmill      main  up to date
sourcesteward  main  would fast-forward 39 commits <old>..<new>
workharbor     main  up to date
summary: 1 fast forward, 6 up to date
```

A real `stw sync` was not run on the live workspace: the real runs are those of the smoke script and the transcript above.

## Open items of the spec

Item 1 no longer blocks and item 2 is answered in the spec (see its open items); both were reviewed with the closing
pull request of the spec.

## Not exercised

- The hosted lanes. A pull request without the `ci-full` label runs only the current lane (`.github/workflows/ci.yml`);
  the minimum lane runs after the merge, so the smoke has run on the minimum Git version only once that lane has passed.
- Darwin, and a Windows host (not a shipped platform).
- The jj paths: jj is refused by design; only the `.jj`-directory refusal is exercised.
- By the smoke: `--ff-only`, `--no-prune`, `--no-autostash`, `--dry-run`, the failure results of `--submodules`
  (`unreachable`, `skipped-dirty`, `diverged`), restore of an incomplete record and the retention of old runs. These are
  covered by the Go tests only.
