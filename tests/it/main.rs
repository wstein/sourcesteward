// One integration-test binary for all tests without process-global state: fewer links, smaller target/.
// New integration tests go here: add tests/it/<name>.rs and a `mod <name>;` line.
// Separate binaries: slug_cases and review_states (harness = false), snapshots_cli (insta snapshot paths),
// snapshots_symlink (sets TMPDIR/CLAUDE_JOB_DIR for the whole process).
#[path = "../common/mod.rs"]
mod common;

mod add;
mod auth;
mod cli;
mod credentials;
mod git_runner;
mod github;
mod github_reads;
mod github_rules;
mod hostile_repo;
mod identity;
mod network;
mod no_binaries;
mod patchid;
mod patchid_props;
mod remote_kinds;
mod replay;
mod review_lines;
mod review_read;
mod slug;
mod sync;
mod sync_rules;
mod tidy;
mod tidy_apply;
mod tidy_apply_cli;
mod tidy_backup;
mod worktrees;
