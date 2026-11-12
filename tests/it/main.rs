// One integration-test binary for all tests without process-global state: fewer links, smaller target/.
// New integration tests go here: add tests/it/<name>.rs and a `mod <name>;` line.
// Separate binaries: slug_cases and review_states (harness = false), snapshots_cli (insta snapshot paths),
// snapshots_symlink (sets TMPDIR/CLAUDE_JOB_DIR for the whole process).
#[path = "../common/mod.rs"]
mod common;

mod add;
mod auth;
mod carryover;
mod carryover_cli;
mod cli;
mod credentials;
mod discover_cli;
mod git_runner;
mod git_runner_ops;
mod git_runner_sync_ops;
mod github;
mod github_reads;
mod github_rules;
mod gitlinks;
mod hostile_repo;
mod identity;
mod lint_pr;
mod lint_pr_cmd;
mod network;
mod no_binaries;
mod patchid;
mod patchid_props;
mod prove;
mod remote_kinds;
mod replay;
mod restack_plan;
mod review_go_corpus;
mod review_lines;
mod review_read;
mod slug;
mod status_counts;
mod status_flags;
mod status_notices;
mod status_review;
mod submodule_paths;
mod sync;
mod sync_default;
mod sync_flags;
mod sync_journal;
mod sync_pull;
mod sync_restore;
mod sync_rules;
mod tidy;
mod tidy_apply;
mod tidy_apply_cli;
mod tidy_backup;
mod tidy_history;
mod worktrees;
