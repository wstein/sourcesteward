# Cloud integration test baseline

On November 25, 2026, `cargo test --locked --offline -j 2` passed on the
sourcesteward tree of `b1591b2`. Git was 2.47.3 and rustc was 1.99.1.
The worktree also held two later commits that do not touch
`projects/sourcesteward`.

| Target | Result |
| --- | --- |
| `src/lib.rs` | 45 passed |
| `src/main.rs` | 0 passed |
| `tests/it` | 1674 passed, 0 failed, 7 ignored |
| `ready_check` | 26 passed |
| `review_states` | 99 passed |
| `slug_cases` | 10 passed |
| `snapshots_cli` | 3 passed |
| `snapshots_symlink` | 1 passed |
| doc-tests | 0 passed |

The previous baseline, on `64d6848` and `461ea28`, had 28 integration
failures. They are closed on this head:

| Group | Closed by |
| --- | --- |
| 20 `watch_core` state-directory fixtures | `3e9aa9d` runs those fixtures outside a Git worktree |
| 6 Git identity cases in `lint_pr_cmd`, `oplog_undo`, `prove`, and `sync_rules` | `a5c79fc` gives the fixtures an explicit identity boundary |
| 2 `sync_default` repository snapshots | `27e26dc` sets `maintenance.auto` to false on the fixture clone. The path that changed was not recorded |

`87ee1d7` updates superprojects before nested submodules. The `sync_restore`
case passed in this run. The ordering now has a deterministic test. One clean
run does not by itself prove an older intermittent failure cannot return.

The 7 ignored integration tests are unchanged. Re-run the locked suite when
the sourcesteward tree moves.
