# sourcesteward interim scripts

Interim shell/Python scripts that precede the Rust `stw` in the parent folder. They are reference and stopgap, not the
product; workflows move into the Rust `stw` step by step. This folder does not depend on the Rust sources.

| Script | Purpose |
| --- | --- |
| `stw-lib.sh` | shared shell helpers (sourced by the others) |
| `stw-status` | per repository: branch, ahead/behind, changes, stash, pins, open PRs (read-only) |
| `stw-sync` | bring the workspace repositories up to date |
| `stw-tidy-plan` | plan for tidying branches and worktrees (plan only) |
| `stw-push` | guarded fast-forward push flow |
| `stw-workflow` | workflow driver around `stw-push` |
| `test-stw-root` | tests for root resolution (no fixed path) and a guard against user paths in this folder |
| `test-stw-push` | tests for `stw-push` and `stw-workflow` |

The workspace root comes from `--root`, the superproject of the current directory, `$STW_ROOT`, or the git top level of the
current directory; otherwise the scripts exit 2. There is no fixed path. Scripts resolve their own location.

## Tests

    ./test-stw-push                            # fast suite only (about 2 minutes), default
    ./test-stw-push --mutations                # suite plus every mutation proof (opt-in, about an hour)
    ./test-stw-push --mutations-only NAME...   # suite plus only the named mutations
    ./test-stw-push --only t_review            # only the named test groups

    ./test-stw-root                            # root resolution, guard, 2 mutation proofs

Everything runs in throw-away repositories under `${STW_TEST_TMP:-${TMPDIR:-/tmp}}`; no real remotes, no credentials.
