# Cloud integration test baseline

On November 25, 2026, the locked integration suite failed the same 28 tests
on base `64d6848` and on workspace checkpoint head `461ea28`. The latter
passed five additional Workspace tests. The other test targets passed in both
runs. Keep the failures visible while fixing the fixtures and investigating
the remaining failures.

| Count | Group | Next step |
| --- | --- | --- |
| 20 | `watch_core` state-directory fixtures | Run these fixtures outside the cloud sandbox's `/tmp/.git` marker. The focused rerun using a fresh directory under `/dev/shm` gave 81 passes, zero failures, and one ignored test. Make the fixture root explicit and keep the product check that rejects state inside a worktree. |
| 6 | Git identity cases in `lint_pr_cmd`, `oplog_undo`, `prove`, and `sync_rules` | Give the fixtures an explicit Git config boundary or a deterministically configured fixture identity, depending on what each test is proving. The cloud machine has a system Git identity, and some child processes clear inherited environment variables and still read it. Verify both the missing-identity and configured-identity cases. |
| 2 | `sync_default` repository snapshots | Diagnose which path disappears or changes during the full-tree read in `tests/it/sync_default.rs`. Repeat the fixture on the baseline and record the exact path and writer before deciding whether the test fixture or STW needs changing. |

A separate intermittent submodule-ordering failure in `sync_restore` reproduced
with the unmodified baseline executable. It was absent from the final
28-failure comparison. Track it separately from the two snapshot failures.

Fix the watch fixture first, then the identity fixtures. Keep the two snapshot
failures and the intermittent submodule case as investigation work. Re-run the
locked suite after each group and keep regressions visible.
