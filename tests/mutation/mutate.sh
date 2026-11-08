#!/bin/bash
# Mutation proofs: each mutation breaks one rule in a COPY of the crate; the named test must then fail
# at its assertion. Usage: mutate.sh SCRATCH_DIR [ID...]   (cargo on PATH). The crate itself is never modified.
# Exit 0 only if every mutation was killed.
set -u
HERE=$(cd "$(dirname "$0")" && pwd)
SRC=$(cd "$HERE/../.." && pwd)
SCRATCH=${1:?usage: mutate.sh SCRATCH_DIR [ID...]}; shift
ONLY=" $* "
WORK=$SCRATCH/mut-crate
export CARGO_TARGET_DIR=$SCRATCH/mut-target

# Each mutation: id | file | old text | new text | test target | test name.
# Old text must occur exactly once in the file (otherwise the mutation is reported as BAD).
IDS=() FILES=() OLDS=() NEWS=() TARGETS=() TESTS=() DESCS=()
m() { IDS+=("$1"); DESCS+=("$2"); FILES+=("$3"); OLDS+=("$4"); NEWS+=("$5"); TARGETS+=("$6"); TESTS+=("$7"); }

m P1 "whitespace is not removed before hashing" src/patch_id.rs \
  ".filter(|b| !matches!(b, b' ' | b'\\t' | b'\\n' | 0x0b | 0x0c | b'\\r'))," ".filter(|_| true)," \
  patchid whitespace_differences_do_not_matter_but_content_does
m P2 "hunk header (line numbers) is hashed" src/patch_id.rs \
  "let start = hunks[i].before.start.saturating_sub(CONTEXT);" \
  "let start = hunks[i].before.start.saturating_sub(CONTEXT); h.line(format!(\"@@ -{} +{}\", hunks[i].before.start, hunks[i].after.start).as_bytes());" \
  patchid same_change_on_different_base_has_equal_patch_id
m P3 "file path is not hashed" src/patch_id.rs \
  "let p = path.to_str_lossy();" "let p = std::borrow::Cow::Borrowed(\"x\");" \
  patchid path_matters
m P4 "context lines are not hashed" src/patch_id.rs \
  "        for k in start..hunks[i].before.start {
            emit(h, b' ', b[k as usize]);" \
  "        for k in start..hunks[i].before.start {
            let _ = k;" \
  patchid matches_git_patch_id_stable_on_known_vectors
m W1 "a missing worktree directory protects the branch" src/repo/worktrees.rs \
  "let dir_exists = base.as_ref().map(|b| b.is_dir()).unwrap_or(false);" "let dir_exists = true;" \
  worktrees missing_directory_protects_nothing_and_is_a_stale_registration
m W2 "linked worktrees do not protect their branch" src/repo/worktrees.rs \
  "w.in_use.push((b, base));" "let _ = (b, base);" \
  worktrees live_worktree_protects_its_branch_and_main_checkout_counts
m W3 "worktree protection ignored by tidy" src/tidy.rs \
  "if let Some(dir) = wt.branch_dir(&br) {" "if let Some(dir) = wt.branch_dir(&br).filter(|_| false) {" \
  tidy classification_with_pull_request_proof
m T1 "equal-patches proof accepts any branch" src/tidy.rs \
  "    Ok(false)
}

/// Pull request proof" "    Ok(true)
}

/// Pull request proof" \
  tidy a_changed_patch_is_not_equal
m T2 "merged PR proof ignores the head SHA" src/tidy.rs \
  "if pr.head_sha == tipstr {" "if true {" \
  tidy classification_with_pull_request_proof
m D1 "overlap check removed (dirty working tree)" src/sync/fast_forward.rs \
  "if !overlap.is_empty() {" "if false && !overlap.is_empty() {" \
  sync unstaged_change_on_a_touched_file_is_an_error_without_any_change
m D2 "staged changes not seen by the dirty check" src/status.rs \
  "staged.insert(loc);" "let _ = loc;" \
  sync staged_change_on_a_touched_file_is_an_error_without_any_change
m D3 "untracked-in-the-way check removed" src/sync/fast_forward.rs \
  "if !in_the_way.is_empty() {" "if false && !in_the_way.is_empty() {" \
  sync untracked_file_in_the_way_of_an_added_file_is_an_error
m D4 "diverged branches are fast-forwarded" src/sync/mod.rs \
  "} else if b > 0 && a > 0 {" "} else if false && a > 0 {" \
  sync diverged_is_skipped_even_with_apply
m D5 "index is not written after the file changes" src/sync/fast_forward.rs \
  "real.write(Default::default())" "Ok::<(), FfError>(())" \
  sync clean_fast_forward_updates_files_index_and_ref
m R1 "a failed non-https fetch is reported as a plain failure" src/sync/fetch.rs \
  '        Some(u) if u.scheme != Scheme::Https => Fetch::Unsupported(format!(' '        Some(u) if false && u.scheme != Scheme::Https => Fetch::Unsupported(format!(' \
  remote_kinds remotes_that_need_other_programs_fail_with_a_typed_error
m C1 "an unreadable repository does not change the exit code" src/cli.rs \
  "Ok(failed(list.iter().any(|s| s.error.is_some())))" "Ok(failed(false))" \
  cli a_repository_that_cannot_be_read_exits_1_in_every_command
m C2 "unknown --repo names are ignored" src/cli.rs \
  "if let Some(bad) = c.repos.iter().find(|n| !known.contains(n)) {" "if let Some(bad) = c.repos.iter().find(|n| false && !known.contains(n)) {" \
  cli unknown_repository_lists_the_known_names
m C3 "usage errors exit 1" src/error.rs \
  "pub const USAGE: u8 = 2;" "pub const USAGE: u8 = 1;" \
  cli usage_errors_exit_2
m C4 "exit codes missing from --help" src/cli.rs \
  "  2  usage error or unusable workspace (nothing was done)" "  2  usage" \
  cli help_documents_the_exit_codes_on_every_command
m C5 "schema_version changed in status" src/status.rs \
  "\"schema_version\": 1, \"command\": \"status\"" "\"schema_version\": 2, \"command\": \"status\"" \
  cli json_documents_carry_schema_version_and_command
m G1 "gix pin text differs from the constant" src/output.rs \
  "pub const GIX_VERSION: &str = \"0.90.0\";" "pub const GIX_VERSION: &str = \"0.89.0\";" \
  cli engine_names_the_pinned_gix_version
m N1 "a process is started in a nested module" src/repo/worktrees.rs \
  "pub fn scan(" "fn spawn() { let _ = std::process::Command::new(\"git\"); }
pub fn scan(" \
  no_binaries src_never_spawns_a_process
m H1 "credentials are chosen by ctx.host, which gix leaves empty" src/sync/fetch.rs \
  '    if !https || host.as_deref() != Some("github.com") {' '    if ctx.host.as_deref() != Some("github.com") {' \
  credentials token_is_offered_for_github_https_urls
m O5 "credentials are never offered" src/sync/fetch.rs \
  '    if !https || host.as_deref() != Some("github.com") {' '    if true {' \
  credentials token_is_offered_for_github_https_urls
m H2 "the token is offered to every host" src/sync/fetch.rs \
  '    if !https || host.as_deref() != Some("github.com") {' '    if false {' \
  credentials token_is_not_offered_to_other_hosts
m S1 "the token is offered over plain http" src/sync/fetch.rs \
  '    if !https || host.as_deref() != Some("github.com") {' '    if host.as_deref() != Some("github.com") {' \
  credentials token_is_not_offered_over_plain_http_or_other_schemes
m X1 "URL credentials are not scrubbed from a fetch failure" src/sync/fetch.rs \
  'let msg: String = scrub(gix_message, url).chars().take(300).collect();' 'let msg: String = gix_message.chars().take(300).collect();' \
  credentials a_fetch_failure_text_never_carries_the_url_password
m L1 "the slug is taken from any URL that contains github.com" src/repo/mod.rs \
  '    if !url.host()?.eq_ignore_ascii_case("github.com") {' '    if false {' \
  slug slug_is_read_from_github_hosts_only
m O1 "the token is printed in a GitHub error message" src/github.rs \
  'code => Err(Error::github(format!("GitHub API HTTP {code} for {path}"))),' \
  'code => Err(Error::github(format!("GitHub API HTTP {code} for {path} (token {})", self.token))),' \
  github the_token_is_sent_but_never_printed_on_any_status_path
m O2 "--offline still asks GitHub" src/github.rs \
  '    pub fn discover(offline: bool) -> GithubState {
        if offline {' '    pub fn discover(offline: bool) -> GithubState {
        if false && offline {' \
  github offline_makes_no_request_and_online_does
m O3 "a JSON field of sync is renamed" src/sync/mod.rs \
  '"dirty_files": s.dirty' '"dirty": s.dirty' \
  github json_field_names_are_the_documented_ones
m O4 "a file in the way of a new directory is overwritten" src/sync/fast_forward.rs \
  '                if !m.is_dir() {' '                if false && !m.is_dir() {' \
  sync a_file_in_the_way_of_a_new_directory_is_an_error
m O6 "a gone upstream is reported as up to date" src/sync/mod.rs \
  '("skip", "upstream branch is gone".into())' '("none", "up to date".into())' \
  sync gone_upstream_is_skipped
m O8 "a URL password is printed" src/sync/fetch.rs \
  '    plain.user = None;
    plain.password = None;' '    let _ = &plain;' \
  remote_kinds remotes_that_need_other_programs_fail_with_a_typed_error
m F3 "a new submodule leaves no directory" src/sync/fast_forward.rs \
  '        std::fs::create_dir_all(&dir)' '        Ok::<(), std::io::Error>(())' \
  sync a_new_submodule_leaves_its_empty_directory_like_git_does
m F4 "a file replaced by a directory is called untracked" src/sync/fast_forward.rs \
  '                        .is_some()
                    {' '                        .is_none()
                    {' \
  sync a_tracked_file_that_becomes_a_directory_is_reported_as_that
m F5 "no hint when the fetch fails without a token" src/sync/mod.rs \
  '            if rec.fetch == "failed" && gh.is_none() {' '            if false && gh.is_none() {' \
  sync a_failed_fetch_without_a_token_says_how_to_provide_one
m Z1 "status JSON has the old schema version" src/status.rs \
  '"schema_version": 2, "command": "status"' '"schema_version": 1, "command": "status"' \
  cli json_documents_carry_schema_version_and_command
m Z2 "status JSON carries next_steps again" src/status.rs \
  '"attention": attn,' '"attention": attn, "next_steps": [],' \
  cli no_output_contains_a_command_or_next_steps
m Z3 "status text prints a command" src/status.rs \
  '"{attention} of {} repositories need attention\n"' '"next: git -C x status\n{attention} of {} repositories need attention\n"' \
  cli no_output_contains_a_command_or_next_steps
m Z4 "tidy plan text prints a command" src/tidy.rs \
  '"{d} to delete, {w} to delete with their worktree, {r} to review, {k} kept (read-only: nothing was changed)\n"' '"next: git -C x branch -d y\n{d} to delete, {w} to delete with their worktree, {r} to review, {k} kept (read-only: nothing was changed)\n"' \
  cli no_output_contains_a_command_or_next_steps
m Z5 "sync text prints a command" src/sync/mod.rs \
  '"dry run: {} would fast-forward, {} skipped. Nothing was written.\n"' '"next: git -C x pull\ndry run: {} would fast-forward, {} skipped. Nothing was written.\n"' \
  cli no_output_contains_a_command_or_next_steps
m Z6 "tidy apply accepts --yes" src/cli.rs \
  '    /// Act on this `stw tidy plan --json` output' '    #[arg(long)]
    yes: bool,
    /// Act on this `stw tidy plan --json` output' \
  tidy_apply_cli bare_tidy_shows_usage_and_yes_is_not_a_flag
m Z7 "a bare tidy shows the plan again" src/cli.rs \
  '    let cli = match Cli::try_parse() {' '    let cli = match Cli::try_parse_from({
        let mut v: Vec<String> = std::env::args().collect();
        if v.get(1).map(String::as_str) == Some("tidy") && v.get(2).is_none_or(|a| a.starts_with('"'"'-'"'"')) {
            v.insert(2, "plan".into());
        }
        v
    }) {' \
  tidy_apply_cli bare_tidy_shows_usage_and_yes_is_not_a_flag
m Z8 "tidy plan does not list the branches it would delete" src/tidy.rs \
  '"  {:<7} {}  [{}] {}{}\n",' '"  {:<8} {}  [{}] {}{}\n",' \
  tidy_apply_cli tidy_plan_shows_the_plan_and_changes_nothing
m Z9 "apply JSON has a dry_run field again" src/tidy_apply.rs \
  '"read_only": false, "run": run,' '"read_only": false, "dry_run": false, "run": run,' \
  tidy_apply_cli tidy_apply_deletes_without_any_confirmation_flag
m E1 "the .worktrees/ line is not written" src/tidy_apply.rs \
  '    add.extend_from_slice(b".worktrees/\n");' '    add.extend_from_slice(b"");' \
  tidy_apply apply_excludes_the_worktrees_directory_in_info_exclude_once
m E2 "the .worktrees/ line is added again on every run" src/tidy_apply.rs \
  '    if present {' '    if false && present {' \
  tidy_apply apply_excludes_the_worktrees_directory_in_info_exclude_once
m E3 "a missing final newline is not added before the line" src/tidy_apply.rs \
  "    if !old.is_empty() && !old.ends_with(b\"\\n\") {" '    if false {' \
  tidy_apply the_exclude_line_keeps_existing_content_and_never_touches_gitignore
m E4 "the line goes to .gitignore instead of info/exclude" src/tidy_apply.rs \
  '    let file = repo.common_dir().join("info/exclude");' '    let file = repo.workdir()?.join(".gitignore");' \
  tidy_apply the_exclude_line_keeps_existing_content_and_never_touches_gitignore
m E5 "only the exact line counts as present" src/tidy_apply.rs \
  '    let wanted = [".worktrees/", "/.worktrees/", ".worktrees", "/.worktrees"];' '    let wanted = [".worktrees/"];' \
  tidy_apply an_equivalent_existing_line_counts_as_present
m V1 "a sibling directory .worktrees-old counts as inside .worktrees/" src/tidy.rs \
  '    if dir.starts_with(&root) {' '    if dir.to_string_lossy().starts_with(&*root.to_string_lossy()) {' \
  tidy worktrees_outside_the_worktrees_directory_are_marked
m V2 "no worktree is marked as outside" src/tidy.rs \
  '                    outside: outside_worktrees_dir(&repo, dir),' '                    outside: false,' \
  tidy worktrees_outside_the_worktrees_directory_are_marked
m V3 "the plan text does not say outside .worktrees/" src/tidy.rs \
  '                if w.outside {' '                if false && w.outside {' \
  tidy worktrees_outside_the_worktrees_directory_are_marked
m V4 "JSON hides the outside flag" src/tidy.rs \
  '"outside_worktrees_dir": w.outside' '"outside_worktrees_dir": false' \
  tidy worktrees_outside_the_worktrees_directory_are_marked
m V5 "a worktree outside .worktrees/ is not removable" src/tidy.rs \
  '            if bp.decision == "delete" {
                bp.worktree' '            if bp.decision == "delete" && !outside_worktrees_dir(&repo, dir) {
                bp.worktree' \
  tidy_apply worktrees_inside_and_outside_the_worktrees_directory_are_removed_on_the_same_proof
m V6 "untracked files in a worktree below .worktrees/ do not count as unclean" src/tidy_apply.rs \
  'c.staged + c.unstaged + c.untracked + c.conflicts == 0' 'c.staged + c.unstaged + c.conflicts == 0' \
  tidy_apply a_dirty_worktree_below_the_worktrees_directory_stays
m V7 "the plan text hides a kept branch whose worktree is removable" src/tidy.rs \
  '            if b.decision == "keep" && !all && b.worktree.is_none() {' '            if b.decision == "keep" && !all {' \
  tidy worktrees_outside_the_worktrees_directory_are_marked
m K1 "the exclude file is truncated before the line is written" src/tidy_apply.rs \
  '            .append(true)' '            .write(true)
            .truncate(true)' \
  tidy_apply the_exclude_file_is_only_appended_to_byte_for_byte
m K2 "a failed exclude write no longer fails the run" src/tidy_apply.rs \
  '            || self.exclude.as_ref().is_some_and(|e| e.action == "failed")' '' \
  tidy_apply an_exclude_file_that_cannot_be_written_fails_the_run_but_not_the_deletions
m K3 "the exclude line goes to the git dir of a linked worktree" src/tidy_apply.rs \
  '    let file = repo.common_dir().join("info/exclude");' '    let file = repo.git_dir().join("info/exclude");' \
  tidy_apply a_linked_worktree_as_root_writes_the_common_exclude_file
m K4 "a .worktrees symbolic link is not resolved" src/tidy.rs \
  '        (Ok(d), Ok(r)) => !d.starts_with(r),' '        (Ok(_), Ok(_)) => true,' \
  tidy a_worktrees_directory_behind_a_symbolic_link_counts_as_inside
m K5 "the exclude file is written before the manifest" src/tidy_apply.rs \
  '    let entries: Vec<ManifestEntry> = targets' '    res.exclude = ensure_worktrees_excluded(&repo);
    let entries: Vec<ManifestEntry> = targets' \
  tidy_apply the_manifest_is_written_before_the_exclude_file
m K6 "the plan summary does not count branches deleted with their worktree" src/tidy.rs \
  '    (c("delete"), with_wt, c("review"), c("keep") - with_wt)' '    (c("delete"), 0, c("review"), c("keep"))' \
  tidy the_summary_counts_what_apply_would_do_with_worktrees
m U1 "the plan counts a dirty worktree as deleted with its branch" src/tidy.rs \
  'b.worktree.as_ref().is_some_and(|w| w.blocker.is_none())' 'b.worktree.is_some()' \
  tidy the_summary_counts_what_apply_would_do_with_worktrees
m U2 "the plan counts every kept branch as deleted with its worktree" src/tidy.rs \
  'b.worktree.as_ref().is_some_and(|w| w.blocker.is_none())' 'true' \
  tidy the_summary_counts_what_apply_would_do_with_worktrees
m U3 "a CRLF line is not recognized in info/exclude" src/tidy_apply.rs \
  'l.trim_ascii() == w.as_bytes()' 'l == w.as_bytes()' \
  tidy_apply a_crlf_line_counts_as_present_and_gets_no_duplicate
m U4 "tidy --help shows no exit codes" src/cli.rs \
  'arg_required_else_help = true, after_help = EXIT_CODES)]' 'arg_required_else_help = true)]' \
  cli help_documents_the_exit_codes_on_every_command
m I1 "no fallback identity for reflogs" src/repo/mod.rs \
  '    repo.committer_or_set_fallback(FALLBACK_NAME, FALLBACK_EMAIL)?;' '    let _ = (FALLBACK_NAME, FALLBACK_EMAIL);' \
  identity without_any_identity_the_reflog_carries_the_fallback
m I2 "the fallback beats the Git config identity" src/repo/mod.rs \
  '    repo.committer_or_set_fallback(FALLBACK_NAME, FALLBACK_EMAIL)?;' '    {
        let mut c = repo.config_snapshot_mut();
        c.set_raw_value(&gix::config::tree::Committer::NAME, FALLBACK_NAME)?;
        c.set_raw_value(&gix::config::tree::Committer::EMAIL, FALLBACK_EMAIL)?;
    }' \
  identity the_git_config_identity_beats_the_fallback
m A1 "the ref is not rolled back when writing files fails" src/sync/fast_forward.rs \
  '    if let Err(e) = write_files() {' '    if let Err(e) = write_files() {
        return Err(e);
    }
    if let Err(e) = Ok::<(), FfError>(()) {' \
  sync a_failed_file_write_puts_the_ref_back
m A2 "a failing ref update is ignored" src/sync/fast_forward.rs \
  '    .map_err(|e| FfError::Other(format!("ref update: {e}")))?;' '    .map_err(|e| FfError::Other(format!("ref update: {e}")))
    .ok();' \
  sync a_ref_that_cannot_be_written_leaves_index_and_working_tree_alone
m I3 "a partial environment identity (name only) is accepted" src/repo/mod.rs \
  'usable("GIT_COMMITTER_NAME") && usable("GIT_COMMITTER_EMAIL")' 'usable("GIT_COMMITTER_NAME") || usable("GIT_COMMITTER_EMAIL")' \
  identity only_a_name_in_the_environment_falls_through_to_the_fallback
m I4 "empty environment values count as an identity" src/repo/mod.rs \
  'is_some_and(|v| !v.is_empty())' 'is_some_and(|_| true)' \
  identity empty_environment_values_fall_through_to_the_fallback
m I5 "a partial environment identity still reaches gix (beats the Git config)" src/repo/mod.rs \
  'gix::sec::Permission::Deny' 'gix::sec::Permission::Allow' \
  identity a_partial_environment_identity_falls_through_to_the_git_config
m A3 "a failed rollback is not reported" src/sync/fast_forward.rs \
  '"{cause}; and the ref could not be put back: {undo}"' '"{cause}{undo:.0}"' \
  sync a_failed_rollback_reports_both_errors_and_leaves_the_ref
m A4 "the ref is not rolled back when the index write fails" src/sync/fast_forward.rs \
  '    if let Err(e) = write_files() {' '    if let Err(e) = write_files() {
        return Err(e);
    }
    if let Err(e) = Ok::<(), FfError>(()) {' \
  sync a_failed_index_write_puts_the_ref_back_and_keeps_the_written_files

# tidy apply / restore / backup rule. OPEN (no mutation kills them; see tests/tidy_apply.rs): the .git-file
# registration check (W9), the registration path check, fsync of the manifest.
m B1 'a backup of exactly N days is kept' src/tidy.rs \
  'if age >= limit {' 'if age > limit {' \
  tidy_backup a_backup_of_exactly_n_days_is_deleted_with_the_age_as_proof
m B2 'the archive/ name pattern is missing' src/tidy.rs \
  '["r9-", "r10-", "pre-restack", "archive/", "backup/"]' '["r9-", "r10-", "pre-restack", "backup/"]' \
  tidy_backup a_backup_younger_than_n_days_is_kept
m B3 'the r9 pattern also matches r90-*' src/tidy.rs \
  '["r9-", "r10-"' '["r9", "r10-"' \
  tidy_backup only_the_listed_name_patterns_are_backups
m B4 '--backup-days is ignored (always 7)' src/tidy.rs \
  'opts.backup_days as i64 * 86_400' '7 * 86_400' \
  tidy_backup the_number_of_days_is_a_parameter
m B5 'a backup with a gone upstream becomes review again' src/tidy.rs \
  '} else if is_backup(&br) {' '} else if is_backup(&br) && !up.gone {' \
  tidy_backup a_backup_is_never_review
m T4 'no manifest is written' src/tidy_apply.rs \
  'if !entries.is_empty() {' 'if false {' \
  tidy_apply apply_deletes_only_the_delete_class
m T5 'a manifest error is ignored and the deletion goes on' src/tidy_apply.rs \
  '.map_err(|e| Error::repository(format!("{e}; nothing was deleted")))?;' '.map_err(|e| Error::repository(format!("{e}; nothing was deleted"))).unwrap_or_default();' \
  tidy_apply a_manifest_that_cannot_be_written_deletes_nothing
m T7 'a branch that moved since the plan is still deleted' src/tidy_apply.rs \
  'if bp.tip != p.commit {' 'if false {' \
  tidy_apply a_branch_that_moved_since_the_plan_is_skipped
m T8 'a proof that no longer holds is not checked' src/tidy_apply.rs \
  'if !holds {' 'if false {' \
  tidy_apply a_proof_that_no_longer_holds_skips_the_branch
m T12 'review and keep branches are deleted too' src/tidy_apply.rs \
  'if bp.decision == "delete" {' 'if bp.decision != "keep" {' \
  tidy_apply apply_deletes_only_the_delete_class
m T13 'the plan file lists review entries as deletable' src/tidy_apply.rs \
  'if b["decision"] == "delete" {' 'if b["decision"] != "keep" {' \
  tidy_apply the_plan_file_lists_only_what_may_be_deleted
m W4 'worktree protection ignored (main checkout branch)' src/tidy.rs \
  'if let Some(dir) = wt.branch_dir(&br) {' 'if let Some(dir) = wt.branch_dir(&br).filter(|_| false) {' \
  tidy_apply the_branch_checked_out_in_the_main_worktree_stays
m W5 'a locked worktree is removed' src/tidy_apply.rs \
  'if l.locked {' 'if false {' \
  tidy_apply a_locked_worktree_stays
m W6 'uncommitted changes are not looked at' src/tidy_apply.rs \
  'Ok(c) if c.staged + c.unstaged + c.untracked + c.conflicts == 0 => {}' 'Ok(_) => {}' \
  tidy_apply a_worktree_with_a_modified_file_stays
m W7 'untracked files do not count as unclean' src/tidy_apply.rs \
  'c.staged + c.unstaged + c.untracked + c.conflicts == 0' 'c.staged + c.unstaged + c.conflicts == 0' \
  tidy_apply a_worktree_with_an_untracked_file_stays
m W8 'unpushed commits are not looked at' src/tidy_apply.rs \
  'if !reachable(base_tip) && !reachable(up_tip) {' 'if false {' \
  tidy_apply a_worktree_whose_commits_are_not_pushed_stays
m W10 'a locked dead registration is removed' src/tidy_apply.rs \
  '        if s.locked {' '        if false {' \
  tidy_apply a_locked_dead_registration_stays_with_its_reason
m W11 'a dead registration is not removed' src/tidy_apply.rs \
  'std::fs::remove_dir_all(&r).map_err(|e| e.to_string())' 'Ok(())' \
  tidy_apply a_dead_registration_is_removed_and_frees_its_branch
m W12 'a worktree of a delete branch is not offered for removal' src/tidy.rs \
  '            if bp.decision == "delete" {' '            if false {' \
  tidy_apply a_clean_worktree_is_removed_with_its_branch
m RS1 'restore reports an existing other branch as failed instead of not-overwritten' src/tidy_apply.rs \
  '"not-overwritten",' '"failed",' \
  tidy_apply restore_never_overwrites_a_branch_at_another_commit
m RS2 'restore treats a branch at the same commit as a conflict' src/tidy_apply.rs \
  'Some(id) if id == sha => return' 'Some(id) if false => return' \
  tidy_apply restore_recreates_deleted_branches_and_is_idempotent
m RS3 'restore creates a branch for a missing object' src/tidy_apply.rs \
  'if !repo.has_object(sha) {' 'if false {' \
  tidy_apply restore_reports_a_missing_object
m RS4 'restore takes the oldest run' src/tidy_apply.rs \
  'None => files.last(),' 'None => files.first(),' \
  tidy_apply restore_picks_the_newest_run_unless_told_and_can_filter_a_branch
m RS5 'restore exit code ignores unrestored entries' src/cli.rs \
  'Ok(failed(!r.complete()))' 'Ok(failed(false))' \
  tidy_apply_cli restore_exits_1_when_something_was_not_restored
m RS6 'the restored branch has no fallback identity' src/repo/mod.rs \
  '    repo.committer_or_set_fallback(FALLBACK_NAME, FALLBACK_EMAIL)?;' '    let _ = (FALLBACK_NAME, FALLBACK_EMAIL);' \
  tidy_apply_cli the_restored_branch_carries_the_reflog_identity_rules
m M1 'default manifest directory is not under the data directory' src/tidy_apply.rs \
  'h.join("Library/Application Support/stw/manifests")' 'h.join("Library/stw")' \
  tidy_apply_cli the_manifest_goes_to_the_user_data_directory_by_default
m N2 'a process is started in the apply module' src/tidy_apply.rs \
  'pub fn new_run_id()' 'fn spawn() { let _ = std::process::Command::new("git"); }
pub fn new_run_id()' \
  no_binaries src_never_spawns_a_process
m Y1 'the ref delete does not check the planned commit' src/tidy_apply.rs \
  'PreviousValue::MustExistAndMatch(Target::Object(sha))' 'PreviousValue::MustExist' \
  tidy_apply a_branch_moved_after_the_plan_is_not_deleted_by_the_ref_update
m Y2 'the checked-out check right before the delete is removed' src/tidy_apply.rs \
  '} else if let Some(dir) = after.branch_dir(&t.bp.branch) {' '} else if let Some(dir) = after.branch_dir(&t.bp.branch).filter(|_| false) {' \
  tidy_apply a_branch_checked_out_after_the_plan_is_not_deleted
m Y3 'a branch named twice in the plan file is handled twice' src/tidy_apply.rs \
  'if !seen.insert(p.branch.as_str()) {' 'if !seen.insert(p.commit.as_str()) && false {' \
  tidy_apply a_branch_listed_twice_in_the_plan_file_is_handled_once
m Y4 'a plan file run removes dead registrations too' src/tidy_apply.rs \
  '.filter(|_| wanted.is_none())' '.filter(|_| true)' \
  tidy_apply a_plan_file_run_does_not_touch_dead_registrations
m Y5 'a relative worktree path is shown with ..' src/repo/worktrees.rs \
  'crate::output::p2s(&normalize(p))' 'crate::output::p2s(p)' \
  tidy_apply a_worktree_with_relative_paths_stays_and_is_shown_without_dot_dot
m Y6 'a registration with .. is treated by its path like any other (dot-dot behind a symlink)' src/repo/worktrees.rs \
  '.any(|c| matches!(c, std::path::Component::ParentDir))' '.any(|_| false)' \
  tidy_apply a_dot_dot_behind_a_symlink_selects_nothing_by_its_lexical_path

m VR1 'a hex field ignores its key (swapped order accepted)' src/review.rs \
  '    tok.strip_prefix(key)?.strip_prefix('\''='\'')
' '    tok.split_once('\''='\'').map(|x| x.1)
' \
  review_states 'run::variant_malformed_order.json'
m VR2 'a 41-digit id is accepted' src/review.rs \
  's.len() == 40 && s.bytes()' 's.len() >= 40 && s.bytes()' \
  review_states 'run::variant_hex41_head.json'
m VR3 'a 39-digit id is accepted' src/review.rs \
  's.len() == 40 && s.bytes()' 's.len() >= 39 && s.bytes()' \
  review_states 'run::variant_hex39_head.json'
m VR4 'a 64-digit patch-id is accepted' src/review.rs \
  's.len() == 40 && s.bytes()' 'matches!(s.len(), 40 | 64) && s.bytes()' \
  review_states 'run::variant_hex64_patch_id.json'
m VR5 'upper-case hex is accepted' src/review.rs \
  'b'\''0'\''..=b'\''9'\'' | b'\''a'\''..=b'\''f'\''))
}

fn field' 'b'\''0'\''..=b'\''9'\'' | b'\''a'\''..=b'\''f'\'' | b'\''A'\''..=b'\''F'\''))
}

fn field' \
  review_states 'run::variant_uppercase_head.json'
m VR6 'a tab separates fields' src/review.rs \
  'line.split('\'' '\'').collect()' 'line.split(['\'' '\'', '\''\t'\'']).collect()' \
  review_states 'run::variant_tab_separator.json'
m VR7 'a double space is skipped' src/review.rs \
  'line.split('\'' '\'').collect()' 'line.split('\'' '\'').filter(|s| !s.is_empty()).collect()' \
  review_states 'run::variant_double_space.json'
m VR8 'a trailing CR is dropped (Go behaviour)' src/review.rs \
  '    if !(truncated && !has_break) {
        if let Some(l) = parse_strict(line) {' '    let line = line.strip_suffix('\''\r'\'').unwrap_or(line);
    if !(truncated && !has_break) {
        if let Some(l) = parse_strict(line) {' \
  review_states 'run::variant_crlf.json'
m VR9 'a cut first line may pass' src/review.rs \
  'if !(truncated && !has_break) {' 'if true {' \
  review_lines 'cr_tab_and_cut_lines_are_variants'
m VR10 'pr=none is not recognised' src/review.rs \
  'if v == "none" {' 'if v == "nonex" {' \
  review_states 'run::pr_none_local_scope.json'
m VR11 'a leading zero in pr is accepted' src/review.rs \
  '&& !v.starts_with('\''0'\'');' '&& true;' \
  review_lines 'numbers_are_canonical_decimals'
m VR12 'any pr number matches the scope' src/review.rs \
  '(Scope::Pr(n), PrRef::Number(m)) => n == m,' '(Scope::Pr(_), PrRef::Number(_)) => true,' \
  review_states 'run::wrong_pr.json'
m VR13 'a numbered line counts in a local scope' src/review.rs \
  '(Scope::Local, PrRef::None) => true,' '(Scope::Local, _) => true,' \
  review_states 'run::pr_number_in_local_scope.json'
m VR14 'the first line is taken from the last line' src/review.rs \
  '        Some((l, _)) => (l, true),' '        Some((_, l)) => (l, true),' \
  review_states 'run::first_line_only.json'
m VR15 'ordinary Review: text is a variant' src/review.rs \
  '(prefixed && (keys || verdict_word))' '(prefixed)' \
  review_states 'run::plain_comment_is_not_a_review.json'
m VS1 'every author is trusted' src/review.rs \
  'trusted.iter().any(|t| t.eq_ignore_ascii_case(author))' '!trusted.is_empty() || true' \
  review_states 'run::default_trusts_nobody.json'
m VS2 'logins compare case-sensitively' src/review.rs \
  't.eq_ignore_ascii_case(author)' '*t == author' \
  review_states 'run::author_login_case_insensitive.json'
m VS3 'an untrusted line for another head makes the state unverified' src/review.rs \
  'untrusted_relevant |= entry.names_head(head);' 'untrusted_relevant |= true;' \
  review_states 'run::untrusted_line_for_other_head_is_not_unverified.json'
m VS4 'the edited flag is never set' src/review.rs \
  'edited: c.updated_at != c.created_at,' 'edited: false,' \
  review_states 'run::edited_comment_is_flagged.json'
m VS5 'comments are not ordered by time' src/review.rs \
  '    strict.sort_by(|a, b| {' '    strict.sort_by(|_a, _b| std::cmp::Ordering::Equal);
    let _ = (|a: &Entry, b: &Entry| {' \
  review_states 'run::order_by_created_at_not_by_input.json'
m VS6 'equal timestamps are not ordered by comment id' src/review.rs \
  '(&a.ev.created_at, a.ev.comment_id).cmp(&(&b.ev.created_at, b.ev.comment_id))' '(&a.ev.created_at,).cmp(&(&b.ev.created_at,))' \
  review_states 'run::same_second_ties_by_id.json'
m VS7 'the first review wins instead of the latest' src/review.rs \
  'Line::Review { head: h, .. } if h == head => decisive = Some((pos, None)),' 'Line::Review { head: h, .. } if h == head => { decisive.get_or_insert((pos, None)); }' \
  review_states 'run::later_findings_cancels_clear.json'
m VS8 'the first carry-over wins instead of the latest' src/review.rs \
  'Some(root) => decisive = Some((pos, Some(root))),' 'Some(root) => { decisive.get_or_insert((pos, Some(root))); }' \
  review_states 'run::later_valid_carry_replaces_findings.json'
m VS9 'a review of another head counts for this head' src/review.rs \
  '            Line::Review { head: h, .. } => h == head,' '            Line::Review { head: h, .. } => h == head || true,' \
  review_states 'run::stale_head_is_not_a_review_of_the_head.json'
m VC1 'a carry-over from a head to itself is valid' src/review.rs \
  '        if from == to {' '        if false && from == to {' \
  review_states 'run::carry_self_is_invalid.json'
m VC2 'a carry-over from FINDINGS is valid' src/review.rs \
  'return (verdict == Verdict::Clear).then(|| (q, vec![pos]));' 'return Some((q, vec![pos]));' \
  review_states 'run::carry_from_findings_is_invalid.json'
m VC3 'chains of carry-overs are not followed' src/review.rs \
  'Line::Carried { to: t, .. } if *t == from => {' 'Line::Carried { to: t, .. } if *t == from && false => {' \
  review_states 'run::carry_chain_of_two.json'
m VC4 'chain length is unbounded' src/review.rs \
  'return (links.len() <= MAX_LINKS).then_some((root, links));' 'return Some((root, links));' \
  review_read 'chain_length_is_bounded'
m VC5 'carry-over results are not memoised' src/review.rs \
  '        self.memo[pos] = Some(r.clone());' '        let _ = &r;' \
  review_read 'diamond_of_invalid_carry_overs_is_not_exponential'
m VU1 'a variant of an untrusted author makes the state unknown' src/review.rs \
  '                if is_trusted {
                    variants.push' '                if true {
                    variants.push' \
  review_states 'run::variant_by_untrusted_does_not_make_unknown.json'
m VU2 'an older variant counts as newer than the decision' src/review.rs \
  '(stamp.clone(), *id) > *n' '(stamp.clone(), *id) < *n' \
  review_states 'run::variant_older_than_clear_keeps_clear.json'
m VU3 'a variant without any decision is ignored' src/review.rs \
  '            None => true,
            Some(n)' '            None => false,
            Some(n)' \
  review_states 'run::variant_malformed_order.json'
m VU4 'a variant with an unusable timestamp is ordered' src/review.rs \
  'Some(n) => !*ok || (stamp' 'Some(n) => (stamp' \
  review_states 'run::variant_with_bad_timestamp_is_unknown.json'
m VU5 'an incomplete read is not noted' src/review.rs \
  'reasons.push(format!("comments incomplete: {why}"));' 'let _ = why;' \
  review_read 'incomplete_read_is_unknown_even_with_a_clear'
m VU6 'UNKNOWN is reported as no review' src/review.rs \
  '        status: Status::Unknown,
        decisive: None,' '        status: Status::NoReview,
        decisive: None,' \
  review_read 'unreadable_reads_are_unknown'
m VU7 'a timestamp without T is accepted' src/review.rs \
  '            10 => *c == b'\''T'\'',' '            10 => true,' \
  review_states 'run::bad_timestamp_without_t.json'
m VU8 'a timestamp with trailing text is accepted' src/review.rs \
  '    b.len() == 20
' '    b.len() >= 20
' \
  review_states 'run::bad_timestamp_trailing_digit.json'
m VU9 'a tab escape is read as a line feed' src/review.rs \
  '            '\''t'\'' => out.push('\''\t'\''),' '            '\''t'\'' => out.push('\''\n'\''),' \
  review_lines 'unescape_reverses_the_reads_escaping'

m VW1 'a trusted line for another pr is not considered' src/review.rs \
  'if decisive_pos.is_none_or(|d| fp > d) {' 'if false && decisive_pos.is_none_or(|d| fp > d) {' \
  review_states 'run::wrong_pr_newer_findings_makes_unknown.json'
m VW2 'a trusted line for another pr without any decision is ignored' src/review.rs \
  'if decisive_pos.is_none_or(|d| fp > d) {' 'if decisive_pos.is_some_and(|d| fp > d) {' \
  review_states 'run::wrong_pr_without_decision_is_unknown.json'
m VW3 'a trusted line for another pr older than the decision counts as newer' src/review.rs \
  'if decisive_pos.is_none_or(|d| fp > d) {' 'if decisive_pos.is_none_or(|d| fp < d) {' \
  review_states 'run::wrong_pr_older_than_clear_keeps_clear.json'
m VW4 'an untrusted line for another pr is evidence' src/review.rs \
  '                    if !is_trusted {
                        continue;
                    }
' '' \
  review_states 'run::wrong_pr_untrusted_is_ignored.json'
m VW6 'a line for another pr naming another head makes the state unknown' src/review.rs \
  '        if entry.foreign {
            if entry.names_head(head) {' '        if entry.foreign {
            if true {' \
  review_states 'run::wrong_pr_for_other_head_is_ignored.json'

m VC7 'the patch-id of a carry-over from a CLEAR review is not compared' src/review.rs \
  '                    if self.patch_id(q) != patch_id {' '                    if false && self.patch_id(q) != patch_id {' \
  review_states 'run::carry_patch_id_mismatch_is_invalid.json'
m VC8 'the patch-id of a later link is not compared with the root' src/review.rs \
  '                        if self.patch_id(root) != patch_id {' '                        if false && self.patch_id(root) != patch_id {' \
  review_states 'run::carry_second_link_patch_id_mismatch_is_invalid.json'
m VC9 'a chain may return to a head it already cleared' src/review.rs \
  'if src.members.iter().any(|m| m.0 == to) {' 'if false && src.members.iter().any(|m| m.0 == to) {' \
  review_states 'run::cycle_back_to_root_is_invalid.json'
m VC10 'FINDINGS on an intermediate chain head is ignored' src/review.rs \
  'if src.members.iter().any(|m| self.spoiled(m)) {' 'if false && src.members.iter().any(|m| self.spoiled(m)) {' \
  review_states 'run::findings_on_intermediate_member_after_carry_invalidates.json'
m VC11 'FINDINGS on the root head after the carry-over is ignored' src/review.rs \
  '                    if self.spoiled(&first) {' '                    if false && self.spoiled(&first) {' \
  review_states 'run::findings_on_chain_member_after_carry_invalidates.json'
m VC12 'FINDINGS before the CLEAR also spoils the chain' src/review.rs \
  '            .is_some_and(|&f| f > member.1)' '            .is_some_and(|_| true)' \
  review_states 'run::findings_on_chain_member_before_the_clear_is_superseded.json'
m VC13 'FINDINGS only counts for the first chain head' src/review.rs \
  '            .is_some_and(|&f| f > member.1)' '            .is_some_and(|&f| f > member.1 && member.1 == 0)' \
  review_states 'run::findings_on_intermediate_member_after_carry_invalidates.json'

m VC14 'a carry-over may use a line posted after it' src/review.rs \
  'for &q in before.iter().rev().filter(|&&q| q < pos) {' 'for &q in before.iter().rev().filter(|&&q| q != pos) {' \
  review_states 'run::carry_before_its_source_is_invalid.json'
m VC15 'only the latest line that names the source decides' src/review.rs \
  '            if self.e[q].foreign {
                return Err(NO_SOURCE);
            }' '            if self.e[q].foreign {
                continue;
            }' \
  review_states 'run::wrong_pr_carry_to_source_breaks_chain.json'

killed=0 bad=0
for i in "${!IDS[@]}"; do
  id=${IDS[$i]}
  if [ -n "${ONLY// /}" ] && [[ "$ONLY" != *" $id "* ]]; then continue; fi
  rm -rf "$WORK"; mkdir -p "$WORK"
  (cd "$SRC" && tar --exclude=target -cf - .) | (cd "$WORK" && tar -xf -)
  f=$WORK/${FILES[$i]}
  # perl instead of bash string replacement: that is quadratic and hangs under macOS bash 3.2 on large files
  old=${OLDS[$i]}
  count=$(OLD="$old" perl -0777 -ne '$o=$ENV{OLD}; $c=0; $p=0; while (($p=index($_,$o,$p))>=0) { $c++; $p+=length($o); } print $c;' "$f")
  if [ "$count" != 1 ]; then echo "$id BAD MUTATION (pattern occurs $count times in ${FILES[$i]})"; bad=$((bad+1)); continue; fi
  OLD="$old" NEW="${NEWS[$i]}" perl -0777 -pi -e '$o=$ENV{OLD}; $n=$ENV{NEW}; $p=index($_,$o); substr($_,$p,length($o))=$n;' "$f"
  out=$(cd "$WORK" && cargo test --test "${TARGETS[$i]}" -- --exact "${TESTS[$i]}" 2>&1)
  if grep -q "could not compile" <<<"$out"; then
    echo "$id COMPILE ERROR: ${DESCS[$i]}"; bad=$((bad+1))
  elif grep -q "test ${TESTS[$i]} ... FAILED" <<<"$out" && grep -q "panicked at" <<<"$out"; then
    why=$(grep -A2 "panicked at" <<<"$out" | sed -n '2,3p' | tr '\n' ' ' | cut -c1-150)
    echo "$id killed by ${TARGETS[$i]}::${TESTS[$i]} [$why]: ${DESCS[$i]}"; killed=$((killed+1))
  else
    echo "$id SURVIVED: ${DESCS[$i]}"; bad=$((bad+1))
  fi
done
echo "$killed killed, $bad not killed"
[ "$bad" = 0 ]
