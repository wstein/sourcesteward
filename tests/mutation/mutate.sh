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
  'let header = [quote_path(b"a/", path), quote_path(b"b/", path)];' 'let header = [quote_path(b"a/", "x".into()), quote_path(b"b/", "x".into())];' \
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
  'self.with_patch > 0 && self.commits.is_empty()' 'true' \
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
  '        real.write_to(&mut out, Default::default())
            .map_err(|e| FfError::Other(format!("index: {e}")))?;
        out.into_inner()
            .map_err(|e| FfError::Other(format!("index: {e}")))?
            .commit()
            .map_err(|e| FfError::Other(format!("index: {e}")))?;
' '        let _ = &real;
        drop(out);
' \
  sync clean_fast_forward_updates_files_index_and_ref
m R1 "a failed non-https fetch is reported as a plain failure" src/sync/fetch.rs \
  '        Some(u) if u.scheme != Scheme::Https => Fetch::Unsupported(format!(' '        Some(u) if false && u.scheme != Scheme::Https => Fetch::Unsupported(format!(' \
  remote_kinds network_remotes_of_gix_fail_typed_without_any_program
m C1 "an unreadable repository does not change the exit code" src/cli.rs \
  '    Ok(if list.iter().any(|s| s.error.is_some()) {
        exit::FAILED' '    Ok(if false {
        exit::FAILED' \
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
  '"schema_version": 2, "command": "status"' '"schema_version": 3, "command": "status"' \
  cli json_documents_carry_schema_version_and_command
m G1 "gix pin text differs from the constant" src/output.rs \
  "pub const GIX_VERSION: &str = \"0.90.0\";" "pub const GIX_VERSION: &str = \"0.89.0\";" \
  cli engine_names_the_pinned_gix_version
m N1 "a process is started in a nested module" src/repo/worktrees.rs \
  "pub fn scan(" "fn spawn() { let _ = std::process::Command::new(\"git\"); }
pub fn scan(" \
  no_binaries only_the_runner_spawns_a_process
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
  remote_kinds network_remotes_of_gix_fail_typed_without_any_program
m F3 "a new submodule leaves no directory" src/sync/fast_forward.rs \
  '        std::fs::create_dir_all(&dir)' '        Ok::<(), std::io::Error>(())' \
  sync a_new_submodule_leaves_its_empty_directory_like_git_does
m F4 "a file replaced by a directory is called untracked" src/sync/fast_forward.rs \
  '                        .is_some()
                    {' '                        .is_none()
                    {' \
  sync a_tracked_file_that_becomes_a_directory_is_reported_as_that
m F5 "no hint when the fetch fails without a token" src/sync/mod.rs \
  '            if rec.fetch == "failed"
                && gh.is_none()
' '            if false
                && gh.is_none()
' \
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
  '"{d} to delete, {w} to delete with their worktree, {r} to review{of_them}, {k} kept (read-only: nothing was changed)\n"' '"next: git -C x branch -d y\n{d} to delete, {w} to delete with their worktree, {r} to review{of_them}, {k} kept (read-only: nothing was changed)\n"' \
  cli no_output_contains_a_command_or_next_steps
m Z5 "sync text prints a command" src/sync/mod.rs \
  '"{} fast-forwarded, {} skipped, {} failed",' '"next: git -C x pull\n{} fast-forwarded, {} skipped, {} failed",' \
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
m V5 "a worktree outside .worktrees/ is not removable" src/tidy_apply.rs \
  '    if wt.main_dir.as_ref() == Some(&l.dir) {
        return Some("main worktree".into());
    }
    if l.locked {' '    if wt.main_dir.as_ref() == Some(&l.dir) {
        return Some("main worktree".into());
    }
    if crate::tidy::outside_worktrees_dir(repo, &l.dir) {
        return Some("outside".into());
    }
    if l.locked {' \
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
  '    #[command(subcommand_required = true, arg_required_else_help = true, after_help = EXIT_CODES)]
    Tidy {' '    #[command(subcommand_required = true, arg_required_else_help = true)]
    Tidy {' \
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
m A4 "the ref is not rolled back when a file write fails" src/sync/fast_forward.rs \
  '    if let Err(e) = write_files() {' '    if let Err(e) = write_files() {
        return Err(e);
    }
    if let Err(e) = Ok::<(), FfError>(()) {' \
  sync a_failed_file_write_puts_the_ref_back

# tidy apply / restore / backup rule. OPEN (no mutation kills them; see tests/it/tidy_apply.rs): the .git-file
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
  no_binaries only_the_runner_spawns_a_process
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
  'Ok(root) => decisive = Some((pos, Some(root))),' 'Ok(root) => { decisive.get_or_insert((pos, Some(root))); }' \
  review_states 'run::later_valid_carry_replaces_findings.json'
m VS9 'a review of another head counts for this head' src/review.rs \
  '            Line::Review { head: h, .. } => h == head,' '            Line::Review { head: h, .. } => h == head || true,' \
  review_states 'run::stale_head_is_not_a_review_of_the_head.json'
m VC1 'a carry-over from a head to itself is valid' src/review.rs \
  '        if from == to {' '        if false && from == to {' \
  review_states 'run::carry_self_is_invalid.json'
m VC2 'a carry-over from FINDINGS is valid' src/review.rs \
  '                    if *verdict != Verdict::Clear {' '                    if false && *verdict != Verdict::Clear {' \
  review_states 'run::carry_from_findings_is_invalid.json'
m VC3 'chains of carry-overs are not followed' src/review.rs \
  'Line::Carried { to: t, .. } if *t == from => {' 'Line::Carried { to: t, .. } if *t == from && false => {' \
  review_states 'run::carry_chain_of_two.json'
m VC4 'chain length is unbounded' src/review.rs \
  '                        return if src.links.len() <= MAX_LINKS {' '                        return if true || src.links.len() <= MAX_LINKS {' \
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
  '                        if self.patch_id(src.root) != patch_id {' '                        if false && self.patch_id(src.root) != patch_id {' \
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

m VS10 'an empty login can be trusted' src/review.rs \
  '    !author.is_empty() && trusted.iter()' '    trusted.iter()' \
  review_states 'run::empty_login_is_never_trusted.json'

m VU10 'a timestamp that does not end in Z is accepted' src/review.rs \
  '            19 => *c == b'\''Z'\'',' '            19 => true,' \
  review_states 'run::bad_timestamp_wrong_zone_letter.json'
m VU11 'truncation of a read body does not reach the parser' src/review.rs \
  '            body_truncated: c.body_truncated,' '            body_truncated: false,' \
  review_read 'truncation_of_a_read_body_reaches_the_parser'
m VU12 'a variant in the same second as the decision is ordered by time only' src/review.rs \
  '(stamp.clone(), *id) > *n' '*stamp > n.0' \
  review_states 'run::variant_same_second_higher_id_is_newer.json'
m VS11 'a login that starts with a trusted login is trusted' src/review.rs \
  't.eq_ignore_ascii_case(author)' 'author.to_ascii_lowercase().starts_with(&t.to_ascii_lowercase())' \
  review_states 'run::trusted_login_is_not_a_prefix.json'
m VS12 'a login that is a prefix of a trusted login is trusted' src/review.rs \
  't.eq_ignore_ascii_case(author)' 't.to_ascii_lowercase().starts_with(&author.to_ascii_lowercase())' \
  review_states 'run::trusted_login_is_not_a_suffix_or_shorter.json'
m VC16 'an invalid carry-over to the source ends the search instead of falling back to earlier lines' src/review.rs \
  '                    if let Ok(mut src) = self.valid(q) {' '                    if self.valid(q).is_err() {
                        return Err(NO_SOURCE);
                    }
                    if let Ok(mut src) = self.valid(q) {' \
  review_states 'run::invalid_carry_to_source_is_skipped_for_an_earlier_line.json'

m SA1 'stw add: a filter attribute in the tree is not refused' src/add.rs \
  'if sets_filter(&String::from_utf8_lossy(&obj.data)) {' 'if false {' \
  add 'a_tracked_filter_attribute_is_refused_before_the_work_tree_is_written'
m SA2 'stw add: the origin of the clone is not compared' src/add.rs \
  'if origin.as_deref() != Some(url.as_str()) {' 'if false {' \
  add 'a_clone_with_another_origin_is_refused'
m SA3 'stw add: rollback keeps the changed workspace config' src/add.rs \
  '            restore(self.gitdir.join("config"), &snap.config, &mut problems);
' '' \
  add 'a_failure_after_the_add_restores_gitmodules_index_config_and_the_git_dir'
m SA4 'stw add: rollback keeps .git/modules/PATH' src/add.rs \
  'if let Err(e) = std::fs::remove_dir_all(&self.modules) {' 'if let Err(e) = Ok::<(), std::io::Error>(()) {' \
  add 'a_failure_after_the_add_restores_gitmodules_index_config_and_the_git_dir'
m SA5 'stw add: the workspace config is not checked' src/add.rs \
  'git_runner::check_config_for_add(ws, false).map_err(|e| refused(e.to_string()))?;' '' \
  add 'a_workspace_config_that_runs_a_program_is_refused_before_anything_is_cloned'
m SA6 'stw add: mergetool/difftool cmd key is not refused' src/git_runner.rs \
  'key == TOOL_COMMAND_KEY' 'false' \
  git_runner 'the_add_config_check_refuses_program_keys_in_workspace_and_clone'
m SA7 'stw add: the commit takes every tracked change (no pathspec)' src/git_runner.rs \
  'a.extend(v(&["--", ".gitmodules"]));
                a.push(path.as_str().to_string());' 'a.extend(v(&["-a"]));' \
  add 'other_staged_and_unstaged_changes_stay_out_of_the_commit_and_stay_as_they_were'
m SA8 'stw add: the local steps may use a transport' src/git_runner.rs \
  '_ => "none",' '_ => "file:ssh:git:http:https",' \
  git_runner 'the_add_vectors_are_fixed_unsigned_filterless_and_without_a_transport'

m SA11 'stw add: STW_ROOT wins over the repository of the current directory' src/cli.rs \
  'repo::add_root(&cwd))' 'repo::default_root(&cwd)).or_else(|| repo::top_level(&cwd))' \
  cli 'add_prefers_the_repository_of_the_current_directory_over_stw_root'
m SA13 'stw add: the token is not masked in clone errors' src/add.rs \
  'let secrets: Vec<String> = credentials.map(|(_, t)| t.clone()).into_iter().collect();' 'let secrets: Vec<String> = Vec::new();' \
  add 'clone_errors_hide_the_token_and_say_what_a_missing_credential_means'
m SA14 'stw add: an index entry below PATH is not seen' src/add.rs \
  'if path == p || path.starts_with(&prefix) {' 'if path == p {' \
  add 'an_index_entry_below_path_refuses_even_without_the_directory'
m SA15 'stw add: the rollback removes a .git/modules/PATH it did not create' src/add.rs \
  'if self.modules_owned' 'if true' \
  add 'a_git_dir_that_appears_during_the_run_is_refused_and_never_removed'
m SA16 'stw add: .git/modules is not checked again before the absorb' src/add.rs \
  '    modules_clear(gitdir, req)?;
    undo.own_modules();' '    undo.own_modules();' \
  add 'a_git_dir_that_appears_during_the_run_is_refused_and_never_removed'

m SA9 'stw add: the check-attr answer is ignored' src/add.rs \
  'if value != "unspecified" {' 'if false {' \
  add 'every_way_to_attach_a_filter_to_gitmodules_is_refused_and_no_filter_runs'
m SA10 'stw add: a failing check-attr is not a refusal' src/add.rs \
  '            .map_err(|e| refused(format!("git check-attr failed: {e}")))?;' '            .unwrap_or(git_runner::Output { stdout: b".gitmodules: filter: unspecified\n.gitmodules: diff: unspecified\n.gitmodules: merge: unspecified\n".to_vec(), stderr: String::new() });' \
  add 'a_failing_or_silent_check_attr_refuses'
m SA20 'stw add: a symlinked .gitattributes is not refused' src/add.rs \
  '        .is_ok_and(|m| m.file_type().is_symlink())
    {
        return Err(refused(
            ".gitattributes is a symlink' '        .is_ok_and(|m| m.file_type().is_symlink() && false)
    {
        return Err(refused(
            ".gitattributes is a symlink' \
  add 'every_way_to_attach_a_filter_to_gitmodules_is_refused_and_no_filter_runs'
m SA12 'stw add: the rollback state is saved before the clone' src/add.rs \
'            snapshot: None,
            created: Vec::new(),
            head_before: head_of(ws),
            modules_owned: false,
        })
    }

    /// Create the missing directories of PATH one by one (`create_dir`, never `create_dir_all`): the last
    /// one must not exist, which proves this run owns it.
    fn create_dirs(&mut self, ws: &Path, req: &Request) -> Result<()> {
        let mut cur = ws.to_path_buf();
        let n = req.path.components().count();
        for (i, c) in req.path.components().enumerate() {
            cur.push(c);
            match std::fs::create_dir(&cur) {
                Ok(()) => self.created.push(cur.clone()),
                Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists && i + 1 < n => {
                    if cur
                        .symlink_metadata()
                        .is_ok_and(|m| m.file_type().is_symlink())
                    {
                        return Err(refused(format!("{} is a symlink", cur.display())));
                    }
                }
                Err(e) => {
                    return Err(refused(format!("cannot create {}: {e}", cur.display())));
                }
            }
        }
        Ok(())
    }

    /// Save what the git steps are about to change, right before the first of them: a change another
    /// process made while the clone ran (a `git add` in another terminal) is part of the saved state.
    fn mark_git_started(&mut self) {
        let read = |p: PathBuf| std::fs::read(p).ok();
' \
'            snapshot: Some(Snapshot {
                gitmodules: std::fs::read(ws.join(".gitmodules")).ok(),
                index: std::fs::read(gitdir.join("index")).ok(),
                config: std::fs::read(gitdir.join("config")).ok(),
            }),
            created: Vec::new(),
            head_before: head_of(ws),
            modules_owned: false,
        })
    }

    /// Create the missing directories of PATH one by one (`create_dir`, never `create_dir_all`): the last
    /// one must not exist, which proves this run owns it.
    fn create_dirs(&mut self, ws: &Path, req: &Request) -> Result<()> {
        let mut cur = ws.to_path_buf();
        let n = req.path.components().count();
        for (i, c) in req.path.components().enumerate() {
            cur.push(c);
            match std::fs::create_dir(&cur) {
                Ok(()) => self.created.push(cur.clone()),
                Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists && i + 1 < n => {
                    if cur
                        .symlink_metadata()
                        .is_ok_and(|m| m.file_type().is_symlink())
                    {
                        return Err(refused(format!("{} is a symlink", cur.display())));
                    }
                }
                Err(e) => {
                    return Err(refused(format!("cannot create {}: {e}", cur.display())));
                }
            }
        }
        Ok(())
    }

    /// Save what the git steps are about to change, right before the first of them: a change another
    /// process made while the clone ran (a `git add` in another terminal) is part of the saved state.
    fn mark_git_started(&mut self) {
        let read = |p: PathBuf| std::fs::read(p).ok();
        if self.snapshot.is_some() {
            return;
        }
' \
  add 'changes_made_while_the_clone_runs_survive_a_later_failure'
m SA17 'stw add: a filter after a closing quote is not seen in the clone' src/add.rs \
  "matches!(c, '\"' | '-' | '!')" "matches!(c, '-' | '!')" \
  add 'a_filter_in_the_clones_attributes_is_found_however_it_is_written'
m SA18 'stw add: a commit that exists is rolled back' src/add.rs \
  '*now != self.head_before' 'false' \
  add 'a_commit_that_exists_is_never_rolled_back_even_when_git_reports_failure'
m SA19 'stw add: the superproject of a submodule is not used' src/repo/mod.rs \
  'Some(superproject_of(&top).unwrap_or(top))' 'Some(top)' \
  cli 'add_run_inside_a_submodule_binds_into_the_superproject'

m SA21 'stw add: the --cached check-attr run is skipped' src/add.rs \
  'for cached in [false, true] {' 'for cached in [false] {' \
  add 'every_way_to_attach_a_filter_to_gitmodules_is_refused_and_no_filter_runs'
m SA22 'stw add: the system attributes file is not switched off' src/git_runner.rs \
  'set("GIT_ATTR_NOSYSTEM", "1".into());' 'let _ = ();' \
  git_runner 'the_add_vectors_are_fixed_unsigned_filterless_and_without_a_transport'

m RP1 'replay: an empty-start commit is not kept' src/replay.rs \
  '    if own_tree == base_tree {' '    if false && own_tree == base_tree {' \
  replay empty_start_commit_is_kept_like_git
m RP2 'replay: a commit that became empty is kept' src/replay.rs \
  '    if merged == new_tree {' '    if false && merged == new_tree {' \
  replay commit_that_became_empty_is_dropped_like_git
m RP5 'replay: a root commit is accepted' src/replay.rs \
  '            0 => return Err(ReplayError::RootCommit(info.id)),' '            0 => {}' \
  replay root_commit_in_the_range_is_refused
m RP6 'replay: a merge commit is replayed as its first parent' src/replay.rs \
  '        _ => return Err(ReplayError::MergeCommit(commit)),' '        _ => parents[0],' \
  replay merge_commit_in_the_range_is_refused_before_anything_is_replayed
m RP7 'replay: the original committer is kept' src/replay.rs \
  '        committer: committer.clone(),' '        committer: decoded.committer().map_err(git_err)?.into(),' \
  replay replayed_commit_keeps_the_author_and_takes_the_configured_committer
m RP8 'replay: the configured identity becomes the author' src/replay.rs \
  '        author: decoded.author().map_err(git_err)?.into(),' '        author: committer.clone(),' \
  replay replayed_commit_keeps_the_author_and_takes_the_configured_committer
m RP9 'replay: objects are written to the repository, not to memory' src/replay.rs \
  '    let mut mem = repo.clone().with_object_memory();' '    let mut mem = repo.clone();' \
  replay nothing_is_written_to_the_repository_and_the_new_objects_live_in_memory
m RP10 'replay: merge drivers of the configuration stay configured' src/replay.rs \
  '            while file.remove_section("merge", Some(sub.as_ref())).is_some() {}' '            let _ = &sub;' \
  replay a_merge_driver_of_the_configuration_is_never_started
m RP11 'replay: an unchanged commit is reported as clean' src/replay.rs \
  '    let status = if new == commit {
        Status::Unchanged
    } else {
        Status::Clean
    };' '    let status = Status::Clean;' \
  replay rerun_over_a_replayed_range_returns_the_original_commits
m RP12 'replay: an unchanged commit is rewritten' src/replay.rs \
  '    if tree == old_tree && parent == old_parent {' '    if false && tree == old_tree && parent == old_parent {' \
  replay rerun_over_a_replayed_range_returns_the_original_commits
m RP13 'replay: the replay goes on after a conflict' src/replay.rs \
  '        if stop {' '        if false && stop {' \
  replay content_conflict_matches_git
m RP14 'replay: conflicts are not detected' src/replay.rs \
  '    if outcome.has_unresolved_conflicts(how) {' '    if false && outcome.has_unresolved_conflicts(how) {' \
  replay content_conflict_matches_git
m RP15 'replay: user.* beats committer.* for the identity' src/replay.rs \
  'config_value(repo, &["committer.name", "user.name"]),' 'config_value(repo, &["user.name", "committer.name"]),' \
  replay committer_comes_from_the_config_only
m RP16 'replay: rename detection is off' src/replay.rs \
  '            ("renames", "true"),' '            ("renames", "false"),' \
  replay rename_on_the_new_side_follows_the_edit
m RP17 'replay: a file added to a renamed directory is not a conflict' src/replay.rs \
  '            if moved && exists(base, dir)? && !exists(renamer, dir)? {' '            if false && moved && exists(base, dir)? && !exists(renamer, dir)? {' \
  replay a_file_added_to_a_directory_that_onto_renamed_is_a_conflict_like_git
m RP18 'replay: the environment counts as the configured identity' src/replay.rs \
  '            | Source::Worktree
    )' '            | Source::Worktree
            | Source::Env
            | Source::EnvOverride
            | Source::Cli
            | Source::Api
    )' \
  replay committer_ignores_the_environment
m RP20 'replay: a directory that still exists counts as renamed' src/replay.rs \
  '            if moved && exists(base, dir)? && !exists(renamer, dir)? {' '            if moved && exists(base, dir)? {' \
  replay a_file_moved_out_of_a_directory_that_stays_is_not_a_directory_rename
m RP21 'replay: a directory name prefix counts as the directory' src/replay.rs \
  "                .any(|s| s.starts_with(dir) && s.get(dir.len()) == Some(&b'/'));" '                .any(|s| s.starts_with(dir));' \
  replay a_renamed_neighbour_directory_does_not_make_a_directory_rename
m RP22 'replay: only files added by the replayed commit are checked' src/replay.rs \
  '    for c in one_way(repo, &base, &own, &new)? {' '    for c in Vec::<Conflict>::new() {' \
  replay a_directory_renamed_by_the_replayed_commit_and_a_file_added_by_onto_is_a_conflict_like_git
m RP23 'replay: a new subdirectory entry is listed as a conflict path' src/replay.rs \
  '        if entry_mode.is_tree() {' '        if false {' \
  replay a_new_subdirectory_in_a_renamed_directory_lists_the_file_only
m RP24 'replay: renames are tracked on the adding side as well' src/replay.rs \
  '    for change in diff(adder, None)? {' '    for change in diff(adder, Some(Default::default()))? {' \
  replay a_file_moved_into_a_directory_that_onto_renamed_is_a_conflict_like_git

m LP1 'lint pr: the session pattern does not match the trailer' src/lint_pr.rs \
  'f.contains(concat!("claude", "-session:"))' 'f.contains(concat!("claude", "-sessionX:"))' \
  lint_pr session_link_every_location
m LP2 'lint pr: the email is compared case-sensitively' src/lint_pr.rs \
  'fold(&o.email) == fold(email)' 'o.email == email' \
  lint_pr identity
m LP3 'lint pr: a trailer without a name passes' src/lint_pr.rs \
  "if name.is_empty() || name.contains('\\r') {" "if name.contains('\\r') {" \
  lint_pr co_authored_by_edges
m LP4 'lint pr: an @ at the edge of the address passes' src/lint_pr.rs \
  "c == '@' && i > 0 && i + 1 < addr.len()" "c == '@'" \
  lint_pr co_authored_by_edges
m LP5 'lint pr: an empty scope passes' src/lint_pr.rs \
  'let ok = !scope.is_empty()' 'let ok = true' \
  lint_pr conventional_commit
m LP6 'lint pr: the hard limit of the PR text is one line higher' src/lint_pr.rs \
  'if n > r.hard {' 'if n > r.hard + 1 {' \
  lint_pr body_length
m LP7 'lint pr: Blockers (plural) is no Blocker line' src/lint_pr.rs \
  '    if optional_s {
        if let Some((r2, l2))' '    if false {
        if let Some((r2, l2))' \
  lint_pr blocker_and_author_agent_edges
m LP8 'lint pr: a keyword boundary ignores the character that was matched' src/lint_pr.rs \
  'word(last) != rest.chars().next().is_some_and(word)' '!rest.chars().next().is_some_and(word)' \
  lint_pr review_claim_case_folding_and_boundary
m LP9 'lint pr: a # before the Blocker line is not allowed' src/lint_pr.rs \
  "matches!(c, '*' | '_' | '#' | '>' | '-')" "matches!(c, '*' | '_' | '>' | '-')" \
  lint_pr blocker_and_author_agent_edges
m LP10 'lint pr: a review claim needs no boundary after the verdict word' src/lint_pr.rs \
  '.any(|v| keyword_ci(rest, v, false))' '.any(|v| strip_prefix_ci(rest, v).is_some())' \
  lint_pr review_line_format
m LP11 'lint pr: blank lines before a review line are not skipped' src/lint_pr.rs \
  "body.split('\\n').find(|l| !l.trim().is_empty())" "body.split('\\n').next()" \
  lint_pr review_claim_case_folding_and_boundary
m LP12 'lint pr: pr numbers have no digit limit' src/lint_pr.rs \
  'PrRef::Number(n) if *n <= 999_999_999 => Some(l),' 'PrRef::Number(_) => Some(l),' \
  lint_pr review_line_pr_field
m LP13 'lint pr: pr=none is a review line' src/lint_pr.rs \
  '        PrRef::Number(n) if *n <= 999_999_999 => Some(l),
        _ => None,' '        PrRef::Number(n) if *n <= 999_999_999 => Some(l),
        PrRef::None => Some(l),
        _ => None,' \
  lint_pr review_line_pr_field
m LP14 'lint pr: a review comment of 16 lines is allowed' src/lint_pr.rs \
  'let n = line_count(&c.body);
                    if n > r.limit {' 'let n = line_count(&c.body);
                    if n > r.limit + 1 {' \
  lint_pr review_comment_length
m LC1 'lint pr: a Link next that skips a page is followed' src/lint_pr_cmd.rs \
  'Some(n) if n == page + 1 => Ok(true),' 'Some(_) => Ok(true),' \
  lint_pr_cmd link_next_that_is_not_the_following_page_is_a_read_failure
m LC2 'lint pr: a commit count equal to the commits read is incomplete' src/lint_pr_cmd.rs \
  'if commit_count > input.commits.len() as i64 {' 'if commit_count >= input.commits.len() as i64 {' \
  lint_pr_cmd commit_count_boundary
m LC3 'lint pr: the comment page bound is not reported' src/lint_pr_cmd.rs \
  'tr_comments = true;' 'tr_comments = false;' \
  lint_pr_cmd each_truncation_alone_fails_closed
m LC4 'lint pr: an incomplete read without an error finding exits 0' src/lint_pr_cmd.rs \
  'let (stderr, code) = if report.errors() == 0 && !report.passed() {' 'let (stderr, code) = if false {' \
  lint_pr_cmd truncation_at_the_page_bounds
m LC5 'lint pr: a cut comment list still passes' src/lint_pr_cmd.rs \
  'self.errors() == 0 && !self.truncated_commits && !self.truncated_comments' 'self.errors() == 0 && !self.truncated_commits' \
  lint_pr_cmd each_truncation_alone_fails_closed
m LC6 'lint pr: a warning exits 3' src/lint_pr_cmd.rs \
  '} else if report.errors() > 0 {' '} else if report.errors() + report.warnings() > 0 {' \
  lint_pr_cmd warning_only_exits_zero
m LC7 'lint pr: the git identity is not the default owner' src/lint_pr_cmd.rs \
  'if owners.is_empty() {
        let dir' 'if false {
        let dir' \
  lint_pr_cmd prepare_default_identity_from_git_config
m LC8 'lint pr: PATH and --repo may be combined' src/lint_pr_cmd.rs \
  'if f.path.is_some() && f.repo.is_some() {' 'if false {' \
  lint_pr_cmd prepare_usage_errors
m LC9 'lint pr: the human list does not escape the message' src/lint_pr_cmd.rs \
  'let (msg, cut) = escape_untrusted(&f.message, 120);' 'let (msg, cut) = (f.message.clone(), false);' \
  lint_pr_cmd human_output_escapes_control_characters
m LC10 'lint pr: a redirect is followed' src/github/lint_reads.rs \
  '.max_redirects(0)' '.max_redirects(5)' \
  lint_pr_cmd lint_pr_status_codes_of_the_api
m LC11 'lint pr: a comment without an id is accepted' src/lint_pr_cmd.rs \
  'if id < 1 {' 'if id < 0 {' \
  lint_pr_cmd read_failures_exit_one_without_document
m LC12 'lint pr: a short head SHA is accepted' src/lint_pr_cmd.rs \
  's.len() == 40 && s.bytes().all' 's.bytes().all' \
  lint_pr_cmd read_failures_exit_one_without_document
m LC13 'lint pr: the commit page bound is not reported' src/lint_pr_cmd.rs \
  '        if page == COMMITS_MAX_PAGE {
            tr_commits = true;
        }' '        if page == COMMITS_MAX_PAGE {}' \
  lint_pr_cmd truncation_at_the_page_bounds

# sync --prune, --no-prune, --submodules (tests/it/sync_flags.rs, tests/it/git_runner_sync_ops.rs)
m SP1 "--prune is not passed to git fetch" src/git_runner.rs \
  'a.extend(v(&["--prune", "--no-prune-tags"]));' 'a.extend(v(&["--no-prune-tags"]));' \
  sync_flags prune_deletes_a_gone_remote_tracking_ref_and_nothing_else
m SP2 "--no-prune-tags is not passed: remote.origin.pruneTags deletes a local tag" src/git_runner.rs \
  'a.extend(v(&["--prune", "--no-prune-tags"]));' 'a.extend(v(&["--prune"]));' \
  sync_flags a_tag_is_never_pruned_even_with_prune_tags_configured
m SP3 "--no-prune is not passed: fetch.prune wins" src/git_runner.rs \
  'a.extend(v(&["--no-prune", "--no-prune-tags"]));' 'a.extend(v(&["--no-tags", "--no-prune-tags"]));' \
  sync_flags no_prune_overrides_fetch_prune
m SP4 "a refspec outside refs/remotes/origin/ is accepted by --prune" src/sync/fetch.rs \
  'if !dst.is_empty() && !dst.starts_with(PRUNE_SCOPE) {' 'if false && !dst.is_empty() && !dst.starts_with(PRUNE_SCOPE) {' \
  sync_flags a_refspec_that_writes_outside_the_namespace_refuses_prune_before_any_fetch
m SP5 "--prune of an https remote is fetched by gix, which never prunes" src/sync/fetch.rs \
  'Scheme::Https | Scheme::Http | Scheme::Git if prune == Prune::On => {' 'Scheme::Https | Scheme::Http | Scheme::Git if false && prune == Prune::On => {' \
  sync_flags prune_for_an_https_remote_goes_through_the_git_runner_and_no_prune_stays_with_gix
m SP6 "the deleted refs are not reported" src/sync/mod.rs \
  'rec.pruned = before.difference(&after).cloned().collect();' 'rec.pruned = Vec::new();' \
  sync_flags prune_deletes_a_gone_remote_tracking_ref_and_nothing_else
m SP7 "the dry run does not count the refs a prune could reach" src/sync/mod.rs \
  'let n = tracking_refs(&r.path).map(|s| s.len())?;' 'let n = 0;' \
  sync_flags the_offline_dry_run_prunes_nothing_fetches_nothing_and_says_what_prune_would_do
m SP8 "--prune and --no-prune do not exclude each other" src/cli.rs \
  '#[arg(long, conflicts_with = "no_prune")]' '#[arg(long)]' \
  sync_flags prune_and_no_prune_exclude_each_other
m SP9 "the fetch of the prune ops allows ext transports in the child" src/git_runner.rs \
  '            Op::FetchOrigin | Op::FetchOriginPrune { .. } | Op::FetchOriginNoPruneTags => {
                "file:ssh:git:http:https"' '            Op::FetchOrigin | Op::FetchOriginPrune { .. } | Op::FetchOriginNoPruneTags => {
                "file:ssh:git:http:https:ext"' \
  git_runner_sync_ops the_submodule_operations_allow_no_transport_and_prune_keeps_the_fetch_transports
m SM1 "the submodule pass is never run" src/cli.rs \
  'if a.submodules {
                let root' 'if false && a.submodules {
                let root' \
  sync_flags a_pinned_submodule_moves_to_the_recorded_commit_and_nothing_else_changes
m SM2 "local changes in the submodule are ignored" src/sync/submodules.rs \
  'if status::changes(&sub, false).map_or(true, |c| c.dirty_paths > 0) {' 'if false {' \
  sync_flags local_changes_in_the_submodule_keep_it_where_it_is
m SM3 "a recorded commit that is not a fast-forward is moved to" src/sync/submodules.rs \
  '    let ff = sub
        .merge_base(head, recorded)
        .is_ok_and(|b| b.is_some_and(|b| b.detach() == head));' '    let ff = true;' \
  sync_flags a_recorded_commit_that_is_not_a_fast_forward_is_not_applied
m SM4 "a missing recorded commit is not noticed" src/sync/submodules.rs \
  '    if !have {' '    if false && !have {' \
  sync_flags a_recorded_commit_the_submodule_lacks_is_unreachable_and_nothing_is_fetched_for_it
m SM5 "the dry run moves the submodule" src/sync/submodules.rs \
  '    if !o.apply {' '    if false && !o.apply {' \
  sync_flags the_dry_run_names_the_move_and_writes_nothing
m SM6 "the rules checkout is listed as an uninitialized submodule" src/sync/submodules.rs \
  '                || canon == skip
' '' \
  sync_flags the_rules_checkout_is_not_reported_as_an_uninitialized_submodule
m SM7 "a --repo filter still adds uninitialized submodules" src/sync/submodules.rs \
  '    if !include_uninitialized {' '    if false {' \
  sync_flags a_repo_filter_does_not_add_uninitialized_submodules
m SM8 "an uninitialized submodule is not reported" src/sync/submodules.rs \
  '        for p in repo::submodule_paths(&holder) {' '        for p in Vec::<String>::new() {' \
  sync_flags an_uninitialized_submodule_is_unreachable_and_never_cloned
m SM9 "repository hooks run during git (core.hooksPath override dropped)" src/git_runner.rs \
  '    "core.hooksPath=/dev/null",' '    "core.pager=cat",' \
  sync_flags hooks_of_the_submodule_do_not_run_during_the_move
m SM10 "a filter driver in the submodule config is accepted" src/git_runner.rs \
  '            let sub = repo.join(path.as_str());
            refuse_config(&sub, &|section, sub_name, key| {
                runs_a_command(section, sub_name, key) || section == "filter"' '            let sub = repo.join(path.as_str());
            refuse_config(&sub, &|section, sub_name, key| {
                runs_a_command(section, sub_name, key)' \
  sync_flags a_filter_driver_in_the_submodule_config_refuses_the_move
m SM11 "the submodule operations allow network transports" src/git_runner.rs \
  '            _ => "none",' '            _ => "file:ssh:git:http:https",' \
  git_runner_sync_ops the_submodule_operations_allow_no_transport_and_prune_keeps_the_fetch_transports
m SM12 "submodule update runs with a fetch" src/git_runner.rs \
  'let mut a = v(&["submodule", "update", "--no-fetch", "--checkout", "--"]);' 'let mut a = v(&["submodule", "update", "--checkout", "--"]);' \
  git_runner_sync_ops the_new_operations_have_fixed_argument_vectors
m SM13 "a submodule path through a symbolic link is followed" src/sync/submodules.rs \
  '    if symlinked_path(root, &rel) {' '    if false && symlinked_path(root, &rel) {' \
  sync_flags a_submodule_path_through_a_symbolic_link_is_left_alone
m SM14 "URL userinfo is printed in the rewrite note" src/sync/submodules.rs \
  'esc(&git_runner::redact(u.as_deref().unwrap_or(""), &[]))' 'esc(u.as_deref().unwrap_or(""))' \
  sync_flags a_changed_url_in_gitmodules_is_reported_without_credentials
m SM15 "a move that did not land is reported as moved" src/sync/submodules.rs \
  '    if landed != Some(recorded) {' '    if false && landed != Some(recorded) {' \
  sync_flags a_move_that_git_did_not_do_is_a_failure_not_a_success
m SM16 "a submodule path with spaces is accepted" src/add.rs \
  "if !s
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || matches!(b, b'.' | b'_' | b'-' | b'/'))" "if false && !s
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || matches!(b, b'.' | b'_' | b'-' | b'/'))" \
  sync_flags a_submodule_path_that_is_not_a_plain_path_is_unreachable
m SM19 "the repository name is printed unescaped in the text output" src/sync/mod.rs \
  '            escape_untrusted(&s.name, 200).0,' '            s.name,' \
  sync_flags hostile_submodule_paths_are_reported_escaped_and_never_touched
m SM20 "the pass recognizes a pinned submodule by the reason text" src/sync/submodules.rs \
  'if !is_root && rec.pinned && rec.result == "skip" {' 'if !is_root && rec.reason.starts_with("detached HEAD") && rec.result == "skip" {' \
  sync_flags the_pass_acts_on_the_typed_pinned_flag_not_on_the_reason_text
m RA "the prune scope has no trailing slash (origin-mirror/ passes)" src/sync/fetch.rs \
  'pub const PRUNE_SCOPE: &str = "refs/remotes/origin/";' 'pub const PRUNE_SCOPE: &str = "refs/remotes/origin";' \
  sync_flags a_refspec_into_a_sibling_namespace_of_origin_refuses_prune

m SH1 "list_repos lists a refused path as a repository" src/repo/mod.rs \
  '        if let Some(why) = path_rejection(dir, &p) {
            w.rejected.push((full(&p), why));
            continue;
        }
        let Some(name) = accepted_name(dir, &p) else {
            continue;
        };' '        if let Some(why) = path_rejection(dir, &p) {
            w.rejected.push((full(&p), why));
        }
        let name = p.clone();' \
  submodule_paths sync_apply_hints_at_refused_paths_and_leaves_their_repositories_alone
m SH2 "the path rules of stw add are not applied to .gitmodules paths" src/repo/mod.rs \
  "let Ok(sub) = crate::add::SubPath::parse(p) else {" "let Ok(sub) = crate::add::SubPath::parse(\"x\") else {" \
  submodule_paths path_rejection_rules
m SH3 "a symbolic link among the components is accepted" src/repo/mod.rs \
  "if std::fs::symlink_metadata(&cur).is_ok_and(|m| m.file_type().is_symlink()) {
            return Some(\"a component is a symbolic link\".into());" \
  "if false {
            return Some(\"a component is a symbolic link\".into());" \
  submodule_paths path_rejection_rules
m SH4 "the real path may leave or equal the workspace root" src/repo/mod.rs \
  "real.starts_with(base) && real != base" "true" \
  submodule_paths below_means_strictly_below
m SH5 "refused paths are not reported by sync" src/sync/submodules.rs \
  "    for (p, why) in rejected {" "    for (p, why) in rejected.iter().take(0) {" \
  submodule_paths sync_dry_run_reports_refused_paths_and_touches_nothing
m SH6 "refused paths are not a notice of status" src/cli.rs \
  "status::collect_notices(&ws.github, &rejected_status_notices(&ws.rejected), &list);" "status::collect_notices(&ws.github, &[], &list);" \
  submodule_paths status_notes_refused_paths_without_a_repository_entry
m SH7 "refused paths are not a notice of tidy plan" src/cli.rs \
  "print!(\"{}\", tidy::render_human(&plans, a.all));
                print_notices(&ws.rejected);" "print!(\"{}\", tidy::render_human(&plans, a.all));" \
  submodule_paths tidy_plan_notes_refused_paths_without_a_repository_entry
m SH8 "a refused path in a dry run with --submodules counts as an error" src/sync/submodules.rs \
  "    let (result, reason) = if o.apply {" "    let (result, reason) = if o.apply || why.contains(\"not accepted\") {" \
  submodule_paths sync_submodules_dry_run_predicts_unreachable_as_a_skip
m SH9 "refused paths are not a notice of tidy apply" src/cli.rs \
  "print!(\"{}\", tidy_apply::apply_render_human(&aopts.run_id, &list));
        print_notices(&ws.rejected);" "print!(\"{}\", tidy_apply::apply_render_human(&aopts.run_id, &list));" \
  submodule_paths tidy_apply_notes_refused_paths_and_does_not_touch_them
m SH10 "a refused path makes status exit 1" src/cli.rs \
  '    Ok(if list.iter().any(|s| s.error.is_some()) {
        exit::FAILED' '    Ok(if list.iter().any(|s| s.error.is_some()) || !ws.rejected.is_empty() {
        exit::FAILED' \
  submodule_paths status_notes_refused_paths_without_a_repository_entry
m SH11 "sync --apply without --submodules fails on a refused path" src/sync/submodules.rs \
  "        if o.submodules {
            unreachable_rec" "        if o.submodules || o.apply {
            unreachable_rec" \
  submodule_paths sync_apply_hints_at_refused_paths_and_leaves_their_repositories_alone
m SH12 "status JSON has no rejected_paths" src/cli.rs \
  '            m.insert("rejected_paths".into(), rejected_json(&ws.rejected));
        }
        print_json(&v);
        if a.verbose {' '            let _ = &ws.rejected;
        }
        print_json(&v);
        if a.verbose {' \
  submodule_paths status_notes_refused_paths_without_a_repository_entry
m SH15 "a submodule named like the root is dropped (dedup by name)" src/repo/mod.rs \
  'if !path.join(".git").exists() || w.seen.contains(&real) {' 'if !path.join(".git").exists() || w.repos.iter().any(|r| r.name == name) {' \
  submodule_paths a_submodule_named_like_the_root_is_listed_and_a_refused_one_is_reported
m SH16 "sync skips a refused path whose name is already in the list" src/sync/submodules.rs \
  "    for (p, why) in rejected {" "    for (p, why) in rejected {
        if list.iter().any(|r| r.name == *p) {
            continue;
        }" \
  submodule_paths a_submodule_named_like_the_root_is_listed_and_a_refused_one_is_reported
m SH17 "refused paths are reported although --repo names repositories" src/cli.rs \
  "    let rejected = if c.repos.is_empty() {" "    let rejected = if true {" \
  submodule_paths repo_names_decide_whether_refused_paths_are_reported
m SH18 "the uninitialized pass visits refused paths too" src/sync/submodules.rs \
  '            let Some(p) = repo::accepted_name(&holder, &p) else {
                continue;
            };' '            let p = p;' \
  submodule_paths sync_with_submodules_names_each_refused_path_once

m SF1 'the unreadable-repository marker is the attention marker' src/status.rs \
  'Icons::Unicode => "\u{2716} ",' 'Icons::Unicode => "\u{25b2} ",' \
  status_flags icons_change_only_the_markers_of_the_repository_lines
m SF2 'the icon default ignores whether stdout is a terminal' src/status.rs \
  'None if t.stdout && t.utf8 && !t.dumb => Icons::Unicode,' 'None if t.utf8 && !t.dumb => Icons::Unicode,' \
  status_flags icons_default_is_unicode_only_on_a_utf8_terminal
m SF3 'a UTF-8 LANG wins over LC_ALL=C' src/status.rs \
  '.find(|v| !v.is_empty());' '.find(|v| v.to_ascii_lowercase().contains("utf"));' \
  status_flags icons_default_is_unicode_only_on_a_utf8_terminal
m SF4 'the icon default ignores TERM=dumb' src/status.rs \
  'None if t.stdout && t.utf8 && !t.dumb => Icons::Unicode,' 'None if t.stdout && t.utf8 => Icons::Unicode,' \
  status_flags icons_default_is_unicode_only_on_a_utf8_terminal
m SF5 'the nerd attention glyph is the ascii one' src/status.rs \
  'Icons::Nerd => "\u{f071} ",' 'Icons::Nerd => "! ",' \
  status_flags icons_change_only_the_markers_of_the_repository_lines
m SF6 'the JSON names an icon set' src/status.rs \
  '"icons": null,' '"icons": "unicode",' \
  status_flags icons_never_reach_the_json_and_a_bad_value_is_a_usage_error
m SF7 'a registration whose checkout is gone is not prunable' src/status.rs \
  'prunable: !st.locked,' 'prunable: false,' \
  status_flags worktrees_lists_the_linked_ones_with_their_state_and_not_the_main_checkout
m SF8 'the lock reason is dropped from the worktree list' src/status.rs \
  'locked: l.locked.then(|| l.lock_reason.clone().unwrap_or_default()),' 'locked: None,' \
  status_flags worktrees_lists_the_linked_ones_with_their_state_and_not_the_main_checkout
m SF9 'a worktree is never dirty' src/status.rs \
  '.map(|c| c.staged + c.unstaged + c.untracked + c.conflicts > 0)' '.map(|c| c.staged + c.unstaged + c.untracked + c.conflicts > 1000)' \
  status_flags worktrees_lists_the_linked_ones_with_their_state_and_not_the_main_checkout
m SF10 'the worktree path is printed unescaped' src/status.rs \
  '                esc(&wt.path),' '                wt.path,' \
  status_flags worktree_text_from_the_repository_is_escaped
m SF11 'relative worktree links are not listed' src/status.rs \
  'for l in w.linked.iter().chain(&w.relative) {' 'for l in w.linked.iter() {' \
  status_flags worktrees_lists_the_linked_ones_with_their_state_and_not_the_main_checkout
m SF12 '--fetch uses fetch.prune of the configuration' src/status.rs \
  'fetch_origin_with(&repo, gh, Prune::Off)' 'fetch_origin_with(&repo, gh, Prune::Unset)' \
  status_flags fetch_never_prunes_not_even_with_fetch_prune_configured
m SF13 '--fetch does not check the fetch refspecs' src/status.rs \
  'if let Err(m) = check_prune_scope(&repo) {' 'if let Err(m) = check_prune_scope(&repo).or(Ok::<(), String>(())) {' \
  status_flags a_fetch_refspec_that_writes_outside_the_remote_tracking_namespace_is_refused_before_any_fetch
m SF14 '--fetch fetches nothing' src/status.rs \
  '    if opts.fetch {' '    if false && opts.fetch {' \
  status_flags fetch_updates_the_remote_tracking_refs_and_nothing_else
m SF15 'a failed fetch is not a notice' src/status.rs \
  'format!("{name}: fetch failed ({})", esc(e))' 'format!("{name}: fetch ({})", esc(e))' \
  status_flags a_failed_fetch_is_a_detail_line_and_a_notice_and_exit_0
m SF16 'the error of a failed fetch is printed unescaped' src/status.rs \
  'out += &format!("    fetch failed: {}\n", esc(e));' 'out += &format!("    fetch failed: {}\n", e);' \
  status_flags a_failed_fetch_is_a_detail_line_and_a_notice_and_exit_0
m SF17 'the deadline of --timeout is never armed' src/cli.rs \
  '        cl.arm_deadline(a.timeout);' '        let _ = cl;' \
  status_flags timeout_cuts_the_github_reads_with_notices_and_question_marks
m SF18 '--timeout accepts values below 1s' src/cli.rs \
  'pub const MIN_TIMEOUT: Duration = Duration::from_secs(1);' 'pub const MIN_TIMEOUT: Duration = Duration::from_millis(1);' \
  status_flags timeout_values_are_go_durations_within_1s_to_10m
m SF19 '--timeout accepts more than 10m' src/cli.rs \
  'pub const MAX_TIMEOUT: Duration = Duration::from_secs(600);' 'pub const MAX_TIMEOUT: Duration = Duration::from_secs(3600);' \
  status_flags timeout_values_are_go_durations_within_1s_to_10m
m SF20 '--timeout is allowed with --offline' src/cli.rs \
  'value_parser = parse_timeout, conflicts_with = "offline")]' 'value_parser = parse_timeout)]' \
  status_flags timeout_out_of_range_or_with_offline_is_a_usage_error_and_nothing_runs
m SF21 'the requests are not counted' src/github.rs \
  '        st.requests += 1;' '        st.requests += 0;' \
  status_flags stats_json_adds_the_stats_member_and_stays_one_document
m SF22 'the rate limit reported is the largest remaining count' src/github.rs \
  'r.remaining <= old.remaining' 'r.remaining > old.remaining' \
  status_flags stats_json_adds_the_stats_member_and_stays_one_document
m SF23 '--verbose with --json prints the stats line to stdout' src/cli.rs \
  'eprintln!("{}", status_stats::stats_line(&stats, &timings));' 'println!("{}", status_stats::stats_line(&stats, &timings));' \
  status_flags verbose_with_json_keeps_stdout_a_document_and_writes_the_breakdown_to_stderr
m SF24 '--verbose does not imply --stats' src/cli.rs \
  'let diag = a.stats || a.verbose;' 'let diag = a.stats;' \
  status_flags verbose_with_json_keeps_stdout_a_document_and_writes_the_breakdown_to_stderr
m SF25 'the reset time shows the wrong minutes' src/status_stats.rs \
  '            &h[..5]' '            &h[..4]' \
  status_flags stats_text_is_one_line_at_the_very_end_with_the_requests_and_the_rate_limit
m SF26 'the civil date of January and February is off' src/status_stats.rs \
  'let m = if mp < 10 { mp + 3 } else { mp - 9 };' 'let m = mp + 3;' \
  status_flags stats_formatting_helpers_follow_the_go_ones
m SF27 'the thousands separator is placed every two digits' src/status_stats.rs \
  '(s.len() - i).is_multiple_of(3)' '(s.len() - i).is_multiple_of(2)' \
  status_flags stats_formatting_helpers_follow_the_go_ones
m SF28 '--verbose is allowed with --offline' src/cli.rs \
  '    #[arg(long, conflicts_with = "offline")]
    verbose: bool,' '    #[arg(long)]
    verbose: bool,' \
  status_flags stats_and_verbose_are_usage_errors_with_offline
m SF29 'the explanation of untracked paths is cut' src/status.rs \
  '"{} untracked: add them, ignore them or remove them",' '"{} untracked",' \
  status_flags long_explains_every_reason_from_the_facts
m SF30 'the card shows no facts' src/status.rs \
  '        if view.long {
            for l in card(s) {' '        if false && view.long {
            for l in card(s) {' \
  status_flags long_adds_cards_and_explanations_and_nothing_is_taken_away
m SF31 'Needs action has no explanations' src/status.rs \
  '            if view.long {
                for r in &rs {' '            if false && view.long {
                for r in &rs {' \
  status_flags long_adds_cards_and_explanations_and_nothing_is_taken_away
m SF32 'the slug of the card is printed unescaped' src/status.rs \
  's.slug.as_deref().map_or("none".to_string(), esc)' 's.slug.as_deref().map_or("none".to_string(), |x| x.to_string())' \
  status_flags long_shows_text_from_the_repository_escaped
m SF33 'the error in an explanation is printed unescaped' src/status.rs \
  'esc(s.error.as_deref().unwrap_or("unknown error"))' 's.error.clone().unwrap_or_default()' \
  status_flags long_explains_every_reason_from_the_facts
m SF34 'the card counts differ from the Counts block' src/status.rs \
  'branches: {} local, {} remote; linked worktrees: {}; stash: {}' 'branches: {} local, {} remote; worktrees: {}; stash: {}' \
  status_flags long_adds_cards_and_explanations_and_nothing_is_taken_away

m SF35 'status --fetch lets git create tags when pruneTags is configured' src/git_runner.rs \
  'a.extend(v(&["--no-prune", "--no-prune-tags"]));' 'a.extend(v(&["--no-prune"]));' \
  status_flags fetch_creates_no_tag_even_with_prune_tags_configured
m SF36 'the repositories are worked on without a bound' src/repo/mod.rs \
  'pub const MAX_PARALLEL: usize = 16;' 'pub const MAX_PARALLEL: usize = 1000;' \
  status_flags the_repositories_are_worked_on_at_most_16_at_a_time_and_in_order
m SF37 'the parallel helper ignores its limit' src/repo/mod.rs \
  'for _ in 0..limit.max(1).min(items.len()) {' 'for _ in 0..items.len() {' \
  status_flags the_repositories_are_worked_on_at_most_16_at_a_time_and_in_order

# stw status --review (src/status_review.rs, tests/it/status_review.rs)
m SR1 'every comment author is trusted' src/status_review.rs \
  '.filter(|c| matches!(c.association.as_str(), "OWNER" | "MEMBER" | "COLLABORATOR"))' \
  '.filter(|_| true)' \
  status_review the_states_of_a_mixed_set_of_pull_requests
m SR2 'the review is evaluated for a local scope, not the pull request' src/status_review.rs \
  'state: review::evaluate_read(Scope::Pr(number), head, read, &trusted),' \
  'state: review::evaluate_read(Scope::Local, head, read, &trusted),' \
  status_review a_clear_at_the_head_is_shown_in_the_text_and_in_the_json
m SR3 'a failed comment read leaves no reason' src/status_review.rs \
  'Read::Failed(m) => Some(m.clone()),' 'Read::Failed(_) => None,' \
  status_review a_comment_read_that_is_denied_missing_or_broken_is_a_question_mark_and_a_notice_never_a_state
m SR4 'one pull request more than the limit is read' src/status_review.rs \
  'if jobs.len() < REVIEW_LIMIT {' 'if jobs.len() <= REVIEW_LIMIT {' \
  status_review at_most_the_review_limit_of_pull_requests_is_read_and_the_rest_is_not_read
m SR5 'a head that is no full commit id is still read' src/status_review.rs \
  'if !is_hex40(&j.head) {' 'if false && !is_hex40(&j.head) {' \
  status_review a_pull_request_with_a_head_that_is_not_a_full_commit_id_is_unknown
m SR6 'the detail of a finding is not escaped in the JSON' src/status_review.rs \
  'if let Some(d) = f["detail"].as_str().map(|d| esc_cut(d, 120)) {' \
  'if let Some(d) = f["detail"].as_str().map(|d| d.to_string()) {' \
  status_review text_of_the_forge_is_escaped_and_cut_in_text_and_json
m SR7 'comment reads are counted as issues, not pulls' src/github.rs \
  '&& n.bytes().all(|b| b.is_ascii_digit())' '&& n.bytes().all(|b| b == b'"'"'x'"'"')' \
  status_review the_comment_reads_are_counted_in_the_stats_as_pulls_requests
m SR8 'a list read starts after the overall deadline' src/github/reads.rs \
  'Err(m) => return Page::Failed(m),' 'Err(_) => timeout,' \
  github_reads a_list_read_obeys_the_overall_deadline
m SR9 'list read requests are not counted' src/github/reads.rs \
  'self.record(&path, started.elapsed(), rate);' 'let _ = rate;' \
  github_reads list_reads_are_counted_in_the_stats_and_comments_belong_to_the_pulls_source
m SR10 'the text label of an unknown state is not a question mark' src/status_review.rs \
  'Status::Unknown => "?",' 'Status::Unknown => "unknown",' \
  status_review a_comment_read_that_is_denied_missing_or_broken_is_a_question_mark_and_a_notice_never_a_state
m SR11 'options.review is always false' src/status.rs \
  '"icons": null, "review": opts.review}' '"icons": null, "review": false}' \
  status_review a_clear_at_the_head_is_shown_in_the_text_and_in_the_json
m SR12 'comments are read without --review' src/cli.rs \
  'if let (true, Some(cl)) = (a.review, ws.github.client()) {' \
  'if let (true, Some(cl)) = (true, ws.github.client()) {' \
  status_review each_pull_request_costs_one_comment_read_and_the_counts_do_not_change
m SR13 'the title of a pull request is printed unescaped' src/status.rs \
  'esc(&p.title),' 'p.title.clone(),' \
  status_review text_of_the_forge_is_escaped_and_cut_in_text_and_json
m SR14 'the evidence lines are never printed' src/status.rs \
  'for l in p.review.iter().flat_map(status_review::evidence_lines) {' \
  'for l in p.review.iter().take(0).flat_map(status_review::evidence_lines) {' \
  status_review long_text_shows_the_evidence_of_each_state
m SR15 'the notice of unread reviews is dropped' src/status_review.rs \
  'if let Some((p, why)) = bad.first() {' 'if let Some((p, why)) = bad.first().filter(|_| false) {' \
  status_review a_comment_read_that_is_denied_missing_or_broken_is_a_question_mark_and_a_notice_never_a_state
m SR16 '--review is allowed with --offline' src/cli.rs \
  '    #[arg(long, conflicts_with = "offline")]
    review: bool,' '    #[arg(long)]
    review: bool,' \
  status_review review_is_a_usage_error_with_offline_and_nothing_is_read
m SR17 'a read that hit its bound still decides' src/status_review.rs \
  'Read::Observed(o) => o.incomplete.clone(),' 'Read::Observed(_) => None,' \
  status_review a_comment_list_that_hits_the_read_budget_is_unknown

m SR18 'only OWNER is trusted' src/status_review.rs \
  '.filter(|c| matches!(c.association.as_str(), "OWNER" | "MEMBER" | "COLLABORATOR"))' \
  '.filter(|c| c.association == "OWNER")' \
  status_review the_states_of_a_mixed_set_of_pull_requests
m SR19 'the head branch is printed unescaped' src/status.rs \
  'esc(&p.head),' 'p.head.clone(),' \
  status_review head_branch_and_merge_state_are_escaped_in_the_pull_request_line
m SR20 'the merge state is printed unescaped' src/status.rs \
  'fl += &format!(", {}", esc(m));' 'fl += &format!(", {}", m);' \
  status_review head_branch_and_merge_state_are_escaped_in_the_pull_request_line

m SR21 'clear-not-ready for a CLEAR that is no draft' src/status_review.rs \
  'if clear && p.draft {' 'if clear {' \
  status_review hints_clear_not_ready_and_base_not_default_in_json_text_and_counts
m SR22 'a carried CLEAR is no CLEAR for clear-not-ready' src/status_review.rs \
  'Status::Clear | Status::ClearCarried));' 'Status::Clear));' \
  status_review hints_clear_not_ready_and_base_not_default_in_json_text_and_counts
m SR23 'base-not-default without known base and default branch' src/status_review.rs \
  'if !p.base.is_empty() && !p.default_branch.is_empty() && p.base != p.default_branch {' \
  'if p.base != p.default_branch {' \
  status_review hints_clear_not_ready_and_base_not_default_in_json_text_and_counts
m SR24 'base and default branch compared case-insensitively' src/status_review.rs \
  '&& p.base != p.default_branch {' '&& !p.base.eq_ignore_ascii_case(&p.default_branch) {' \
  status_review hints_clear_not_ready_and_base_not_default_in_json_text_and_counts
m SR25 'the base branch is printed unescaped in the hint line' src/status_review.rs \
  'esc_cut(&p.base, 100),' 'p.base.clone(),' \
  status_review branch_names_in_hints_are_escaped_and_cut
m SR26 'the base branch is put unescaped into the JSON' src/status.rs \
  'status_review::esc_opt_cut(&p.base, 200),' 'json!(p.base),' \
  status_review branch_names_in_hints_are_escaped_and_cut
m SR27 'base-not-default is not counted' src/status_review.rs \
  '_ => t.base_not_default += 1,' '_ => {}' \
  status_review hints_clear_not_ready_and_base_not_default_in_json_text_and_counts
m SR28 'a failed read still gives clear-not-ready' src/status_review.rs \
  'Status::Clear | Status::ClearCarried));' 'Status::Clear | Status::ClearCarried | Status::Unknown));' \
  status_review a_clear_that_could_not_be_read_gives_no_clear_not_ready

m SR29 'comment reads start after a rate limit' src/status_review.rs \
  'if gh.rate_limited() {' 'if false {' \
  status_review after_a_rate_limit_no_further_comment_read_starts_and_the_rest_is_unknown
m SR30 'a rate limit answer does not set the stop' src/github/reads.rs \
  'if limited {
            self.note_rate_limit();' 'if false {
            self.note_rate_limit();' \
  status_review after_a_rate_limit_no_further_comment_read_starts_and_the_rest_is_unknown
m SR31 'Retry-After is no rate limit evidence' src/github.rs \
  '|| headers.contains_key("retry-after")' '|| false' \
  status_review after_a_rate_limit_no_further_comment_read_starts_and_the_rest_is_unknown
m SR32 'every 403 counts as a rate limit' src/github.rs \
  '    let headers = resp.headers();
    if headers
        .get("x-ratelimit-remaining")' '    if true {
        return true;
    }
    let headers = resp.headers();
    if headers
        .get("x-ratelimit-remaining")' \
  status_review a_forbidden_answer_without_a_rate_limit_does_not_stop_the_reads
m SR33 'the stop covers only the first repository' src/status_review.rs \
  'if gh.rate_limited() {' 'if gh.rate_limited() && j.repo == 0 {' \
  status_review the_rate_limit_stop_covers_every_repository_of_the_run
m SR34 'the notice of skipped reads is dropped' src/status_review.rs \
  '(n > 0).then(|| Notice {' '(n > 100_000).then(|| Notice {' \
  status_review after_a_rate_limit_no_further_comment_read_starts_and_the_rest_is_unknown
m SR35 'skipped reads are also named in the repository notice' src/status_review.rs \
  '(!r.skipped).then_some((p, r.problem.as_deref()?))' 'Some((p, r.problem.as_deref()?))' \
  status_review after_a_rate_limit_no_further_comment_read_starts_and_the_rest_is_unknown

m SR36 'a 403 with the rate limit only in the body does not stop' src/github.rs \
  'text.contains("rate limit") || text.contains("abuse detection")' 'false' \
  status_review after_a_rate_limit_no_further_comment_read_starts_and_the_rest_is_unknown
m SR37 'the abuse detection body is no rate limit' src/github.rs \
  'text.contains("rate limit") || text.contains("abuse detection")' 'text.contains("rate limit")' \
  status_review after_a_rate_limit_no_further_comment_read_starts_and_the_rest_is_unknown
m SR38 'a limited read does not say rate limit reached' src/github/reads.rs \
  'rate limit reached; retry later' 'retry later' \
  status_review after_a_rate_limit_no_further_comment_read_starts_and_the_rest_is_unknown
m SR39 'a rate limit on another read does not set the stop' src/github.rs \
  'if is_rate_limit(&mut resp) {' 'if false {' \
  status_review a_rate_limit_on_another_read_stops_the_comment_reads
m SR40 'base-not-default without a base branch' src/status_review.rs \
  '!p.base.is_empty() && ' '' \
  status_review hints_clear_not_ready_and_base_not_default_in_json_text_and_counts
m SR41 'hints on the pull request line without --review' src/status.rs \
  'false => vec![],' 'false => status_review::hints(p),' \
  status_review hints_are_explained_in_the_long_text_and_absent_without_review

m SR42 'the error body is cut too short' src/github.rs \
  '.take(4096)' '.take(8)' \
  status_review after_a_rate_limit_no_further_comment_read_starts_and_the_rest_is_unknown
m SR43 'the error body is read beyond 4096 bytes' src/github.rs \
  '.take(4096)' '.take(8192)' \
  status_review a_forbidden_answer_without_a_rate_limit_does_not_stop_the_reads
m SR44 'the lint read ignores a long error body' src/github/lint_reads.rs \
  'super::mentions_rate_limit(&super::error_body_head(&mut resp))' 'super::mentions_rate_limit("")' \
  lint_pr_cmd lint_pr_status_codes_of_the_api
m SR45 'a long error body is dropped instead of cut' src/github.rs \
  '    let _ = resp
        .body_mut()
        .as_reader()
        .take(4096)
        .read_to_end(&mut bytes);' '    if resp.body_mut().with_config().limit(4096).read_to_vec().map(|v| bytes = v).is_err() {
        bytes.clear();
    }' \
  status_review after_a_rate_limit_no_further_comment_read_starts_and_the_rest_is_unknown

# sync acts by default; --plan/--dry-run preview; --apply is a hidden alias (tests/it/sync_default.rs)
m SD1 "sync does not act without a flag" src/cli.rs \
  'let execute = !(a.plan || a.dry_run);' 'let execute = a.apply;' \
  sync_default without_a_flag_sync_acts_and_the_refs_move
m SD2 "--dry-run does not preview" src/cli.rs \
  'let execute = !(a.plan || a.dry_run);' 'let execute = !a.plan;' \
  sync_default every_flag_set_acts_by_default_and_changes_nothing_with_plan_or_dry_run
m SD3 "--plan does not preview" src/cli.rs \
  'let execute = !(a.plan || a.dry_run);' 'let execute = !a.dry_run;' \
  sync_default every_flag_set_acts_by_default_and_changes_nothing_with_plan_or_dry_run
m SD4 "--apply together with --plan is accepted" src/cli.rs \
  '#[arg(long, conflicts_with = "apply", overrides_with = "plan")]
    plan: bool,' '#[arg(long, overrides_with = "plan")]
    plan: bool,' \
  sync_default apply_together_with_plan_or_dry_run_is_a_usage_error_and_changes_nothing
m SD5 "--apply together with --dry-run is accepted" src/cli.rs \
  '#[arg(long, conflicts_with = "apply", overrides_with = "dry_run")]
    dry_run: bool,' '#[arg(long, overrides_with = "dry_run")]
    dry_run: bool,' \
  sync_default apply_together_with_plan_or_dry_run_is_a_usage_error_and_changes_nothing
m SD6 "--apply is shown in the help" src/cli.rs \
  '#[arg(long, hide = true)]
    apply: bool,' '#[arg(long)]
    apply: bool,' \
  sync_default the_help_names_plan_and_dry_run_and_hides_apply
m SD7 "the JSON options carry the wrong mode" src/sync/mod.rs \
  '"dry_run": !apply,' '"dry_run": apply,' \
  sync_default every_flag_set_acts_by_default_and_changes_nothing_with_plan_or_dry_run
m SD8 "a plan fast-forwards the branch after its fetch" src/sync/mod.rs \
  'if o.apply && result == "fast-forward" {' 'if result == "fast-forward" {' \
  sync_default every_flag_set_acts_by_default_and_changes_nothing_with_plan_or_dry_run
m SD9 "the plan text loses its marker" src/sync/mod.rs \
  '"dry run: {} would fast-forward, {} skipped"' '"preview: {} would fast-forward, {} skipped"' \
  sync_default the_plan_text_keeps_the_dry_run_marker_and_a_run_does_not_have_it
m SD10 "the rules update does not act by default" src/cli.rs \
  'update: a.rules,
                apply: execute,' 'update: a.rules,
                apply: a.apply,' \
  sync_rules cli_json_carries_agent_rules_and_a_refusal_exits_1
m SD11 "a run acts on a diverged branch" src/sync/mod.rs \
  '} else if b > 0 && a > 0 {' '} else if false && a > 0 {' \
  sync_default a_run_still_leaves_diverged_and_in_progress_repositories_alone
m SD12 "a run ignores an operation in progress" src/sync/mod.rs \
  'if let Some(op) = op {
        ("skip"' 'if let Some(op) = op.filter(|_| false) {
        ("skip"' \
  sync_default a_run_still_leaves_diverged_and_in_progress_repositories_alone
m SD13 "the plan does not report a failed fetch of a run as exit 1" src/cli.rs \
  '"error" | "conflict" | "autostash-conflict"' '"conflict" | "autostash-conflict"' \
  sync_default a_run_without_network_is_exit_1_and_so_is_a_plan_unless_it_is_offline

m SD14 "--rules acts under --plan (R1)" src/cli.rs \
  'update: a.rules,
                apply: execute,' 'update: a.rules,
                apply: !a.plan,' \
  sync_rules plan_and_dry_run_with_rules_fetch_the_tracking_ref_and_move_nothing_online
m SD15 "the plan text loses the note about the last fetch" src/sync/mod.rs \
  ' Nothing was fetched either: based on the last fetch.' '' \
  sync_default the_plan_text_keeps_the_dry_run_marker_and_a_run_does_not_have_it
m SD16 "a diverged skip does not name the ignored rebase setting" src/sync/mod.rs \
  '            if rebase_configured(&repo, b) {' '            if false && rebase_configured(&repo, b) {' \
  sync_default rebase_merge_and_autostash_settings_are_not_followed_and_a_diverged_skip_says_so
m SD17 "the rebase note is always added" src/sync/mod.rs \
  '            if rebase_configured(&repo, b) {' '            if rebase_configured(&repo, b) || true {' \
  sync_default rebase_merge_and_autostash_settings_are_not_followed_and_a_diverged_skip_says_so
m SD18 "a repeated --plan is a usage error" src/cli.rs \
  '#[arg(long, conflicts_with = "apply", overrides_with = "plan")]' '#[arg(long, conflicts_with = "apply")]' \
  sync_default a_repeated_plan_or_dry_run_flag_is_accepted
m SD19 "a repeated --dry-run is a usage error" src/cli.rs \
  '#[arg(long, conflicts_with = "apply", overrides_with = "dry_run")]' '#[arg(long, conflicts_with = "apply")]' \
  sync_default a_repeated_plan_or_dry_run_flag_is_accepted

m NO1 "the JSON has no notices (always an empty array)" src/status.rs \
  '"notices": notices.iter().map(Notice::json).collect::<Vec<_>>(),' '"notices": Vec::<Value>::new(),' \
  status_notices offline_and_no_token_have_the_go_codes_and_the_github_note_as_message
m NO2 "the text block prints no message" src/status.rs \
  '        out += &format!("  {}\n", n.message);' '        out += &format!("  {}\n", "");' \
  status_notices a_failed_pull_request_or_issue_read_is_one_notice_per_source_and_the_old_fields_stay
m NO3 "--offline has the code of no token" src/github.rs \
  '"offline"
        } else if' '"no-token"
        } else if' \
  status_notices offline_and_no_token_have_the_go_codes_and_the_github_note_as_message
m NO4 "a pull request notice has the code of the issues notice" src/status.rs \
  '"prs-not-read",' '"issues-not-read",' \
  status_notices a_failed_pull_request_or_issue_read_is_one_notice_per_source_and_the_old_fields_stay
m NO5 "a repository notice names no repository" src/status.rs \
  '            repos: vec![name.clone()],' '            repos: Vec::new(),' \
  status_notices a_failed_pull_request_or_issue_read_is_one_notice_per_source_and_the_old_fields_stay
m NO6 "the fetch error is not escaped in the notice" src/status.rs \
  'format!("{name}: fetch failed ({})", esc(e)),' 'format!("{name}: fetch failed ({})", e),' \
  status_notices a_failed_fetch_is_a_notice_and_hostile_characters_stay_escaped
m NO7 "the rejected paths come first" src/status.rs \
  '    notices.extend(extra.iter().cloned());' '    notices.splice(0..0, extra.iter().cloned());' \
  submodule_paths refused_paths_are_notices_in_the_json_too_with_hostile_characters_escaped
m NO8 "a rejected path is not escaped in the notice" src/cli.rs \
  'format!("submodule path not accepted: {}: {why}", escape_name(p))' 'format!("submodule path not accepted: {}: {why}", p)' \
  submodule_paths refused_paths_are_notices_in_the_json_too_with_hostile_characters_escaped
m NO9 "review-not-read counts one" src/status_review.rs \
  '            count: bad.len(),' '            count: 1,' \
  status_review the_review_notices_are_in_the_json_with_code_count_and_the_text_order
m NO10 "rate-limited counts one" src/status_review.rs \
  '        count: n,' '        count: 1,' \
  status_review the_review_notices_are_in_the_json_with_code_count_and_the_text_order
m NO11 "review-limit counts one" src/status_review.rs \
  '            count: t.not_read,' '            count: 1,' \
  status_review at_most_the_review_limit_of_pull_requests_is_read_and_the_rest_is_not_read
m NO12 "the rate-limit notice comes before the review notices" src/status.rs \
  '    notices.extend(status_review::rate_limit_notice(list));' '    notices.splice(0..0, status_review::rate_limit_notice(list));' \
  status_review the_review_notices_are_in_the_json_with_code_count_and_the_text_order

m NO13 "the repositories of the notices run in reverse" src/status.rs \
  '    for s in list {
        let name = esc(s.label());
        let one =' '    for s in list.iter().rev() {
        let name = esc(s.label());
        let one =' \
  status_notices notices_of_several_repositories_keep_the_list_order_and_escape_the_name
m NO14 "the repository name is not escaped in the notices" src/status.rs \
  '    for s in list {
        let name = esc(s.label());
        let one =' '    for s in list {
        let name = s.label().to_string();
        let one =' \
  status_notices notices_of_several_repositories_keep_the_list_order_and_escape_the_name

# sync journal, sync restore and tidy history (tests/it/sync_journal.rs, sync_restore.rs, tidy_history.rs)
m JR1 'an existing recovery ref is not refused' src/sync/journal.rs \
  '        return Err("the ref exists already".into());' '        return Ok(());' \
  sync_journal a_run_id_is_never_used_twice
m JR2 'the journal step before the move is never called' src/sync/fast_forward.rs \
  '    before(head, target).map_err(FfError::Other)?;' '    let _ = &before;' \
  sync_journal a_run_that_moves_two_repositories_journals_both
m JR3 'a failed journal step does not stop the move' src/sync/fast_forward.rs \
  '    before(head, target).map_err(FfError::Other)?;' '    let _ = before(head, target);' \
  sync_journal when_the_record_cannot_be_written_the_branch_does_not_move
m JR4 'a plan fetches and then moves the branch, so it journals' src/sync/mod.rs \
  'if o.apply && result == "fast-forward" {' 'if result == "fast-forward" {' \
  sync_journal a_plan_writes_no_journal_and_no_refs
m JR5 'retention keeps eleven runs' src/sync/journal.rs \
  'while done.len() > RETAINED_RUNS - 1 {' 'while done.len() > RETAINED_RUNS {' \
  sync_journal retention_keeps_ten_runs_and_spares_what_is_not_clean
m JR6 'retention deletes a recovery ref whatever it holds' src/sync/journal.rs \
  '            expected: PreviousValue::MustExistAndMatch(Target::Object(id)),' '            expected: PreviousValue::Any,' \
  sync_journal retention_keeps_ten_runs_and_spares_what_is_not_clean
m JR7 'retention drops a record that is not finished' src/sync/journal.rs \
  'self.entries.iter().all(|e| e.state != STATE_PREPARED)' 'self.entries.iter().all(|_| true)' \
  sync_journal retention_keeps_ten_runs_and_spares_what_is_not_clean
m JR8 'a symbolic link as the journal directory is followed' src/sync/journal.rs \
  '            Ok(m) if m.is_dir() => {}' '            Ok(m) if m.is_dir() || m.file_type().is_symlink() => {}' \
  sync_journal a_symbolic_link_as_the_journal_directory_is_refused
m JR9 'the record file is world readable' src/sync/journal.rs \
  'OpenOptionsExt::mode(&mut opts, 0o600);' 'OpenOptionsExt::mode(&mut opts, 0o644);' \
  sync_journal the_record_is_private_to_the_user
m JR10 'the journal directory is world readable' src/sync/journal.rs \
  'DirBuilderExt::mode(&mut b, 0o700);' 'DirBuilderExt::mode(&mut b, 0o755);' \
  sync_journal the_record_is_private_to_the_user
m JR11 'a run ID without microseconds is not padded' src/git_runner.rs \
  '        if k.len() == 15 {' '        if false {' \
  sync_journal run_ids_are_distinct_and_follow_the_clock
m JR12 'a branch name that looks like an option reaches git' src/git_runner.rs \
  'let short = full.strip_prefix("refs/heads/")?;
        if short.is_empty()
            || short.starts_with('\''-'\'')
' 'let short = full.strip_prefix("refs/heads/")?;
        if short.is_empty()
' \
  sync_journal only_a_well_formed_run_id_or_branch_reaches_an_argument_vector
m JR13 'a record that is a symbolic link is read' src/sync/journal.rs \
  '        Ok(m) if m.is_file() => {}' '        Ok(_) => {}' \
  sync_restore a_manipulated_record_fails_before_git_sees_it
m XR1 'the latest run is the lowest run ID' src/sync/restore.rs \
  '.max_by_key(|i| i.order_key());' '.min_by_key(|i| i.order_key());' \
  sync_restore the_highest_run_is_the_default_and_an_older_one_is_named_with_run
m XR2 'a branch that moved since the run is moved back' src/sync/restore.rs \
  '    if expected.map(|e| e.as_str().to_string()) != Some(cur.to_string()) {' '    if false {' \
  sync_restore a_branch_with_a_new_commit_is_changed_since_and_not_touched
m XR3 'a restored branch is not recognized as already restored' src/sync/restore.rs \
  '    if cur == old_id {' '    if false {' \
  sync_restore a_second_restore_says_already_restored
m XR4 'the check of the worktree for local changes is left out' src/sync/restore.rs \
  '            Ok(c) if c.dirty_paths == 0 && c.conflicts == 0 => {}' '            Ok(_) => {}' \
  sync_restore local_changes_in_the_checked_out_worktree_stop_the_restore
m XR5 'a moved submodule counts as a local change of the superproject' src/sync/restore.rs \
  'status::changes_opts(repo, false, true)' 'status::changes_opts(repo, false, false)' \
  sync_restore a_run_that_moved_two_repositories_is_restored_in_both
m XR6 'an operation in progress does not stop the restore' src/sync/restore.rs \
  '    if let Some(op) = status::operation(repo) {' '    if let Some(op) = status::operation(repo).filter(|_| false) {' \
  sync_restore an_operation_in_progress_stops_the_restore
m XR7 'a branch checked out in another worktree is moved' src/sync/restore.rs \
  '    if held_elsewhere(repo, &en.reference) {' '    if false && held_elsewhere(repo, &en.reference) {' \
  sync_restore a_branch_checked_out_in_a_linked_worktree_is_left_alone
m XR8 'an unusable tip in the record is not refused' src/sync/restore.rs \
  '    if matches!(expected, Some(None)) {' '    if false {' \
  sync_restore a_manipulated_record_fails_before_git_sees_it
m XR9 'a sync option together with restore is accepted' src/cli.rs \
  '        .find(|(_, on)| *on)' '        .find(|_| false)' \
  sync_restore a_sync_option_with_restore_is_a_usage_error
m XR10 'an unusable journal directory is skipped silently' src/sync/restore.rs \
  '        if let Err(e) = &s.ids {' '        if let (Err(e), false) = (&s.ids, true) {' \
  sync_restore a_journal_directory_that_is_a_symbolic_link_is_reported
m XR11 'the branch name is not escaped in the restore text' src/sync/restore.rs \
  '            escape_untrusted(x.branch.as_deref().unwrap_or("-"), 200).0,' '            x.branch.as_deref().unwrap_or("-"),' \
  sync_restore a_hostile_branch_name_is_escaped_in_the_text
m XR12 'the expected tip is not part of the compare and swap' src/git_runner.rs \
  '                a.push(expected.as_str().to_string());
' '' \
  sync_journal the_argument_vectors_of_the_journal_operations_are_fixed
m XR13 'an old tip that is gone is not noticed' src/sync/restore.rs \
  '    if repo.find_commit(old_id).is_err() {' '    if false {' \
  sync_restore an_old_tip_that_is_gone_fails_and_nothing_moves
m XR14 'a malformed run ID is accepted by the command line' src/cli.rs \
  '        if git_runner::RunId::parse(id).is_none() {' '        if false {' \
  sync_restore an_unknown_run_and_a_workspace_without_a_journal_are_refused_and_nothing_changes
m TH1 'the history runs oldest first' src/tidy_history.rs \
  'found.sort_by(|a, b| b.0.cmp(&a.0));' 'found.sort_by(|a, b| a.0.cmp(&b.0));' \
  tidy_history journals_come_newest_first_and_the_limit_is_a_visible_truncation
m TH2 'the limit is ignored' src/tidy_history.rs \
  'let shown = &found[..total.min(limit)];' 'let shown = &found[..total];' \
  tidy_history journals_come_newest_first_and_the_limit_is_a_visible_truncation
m TH3 'an existing branch is restorable' src/tidy_history.rs \
  '    let free = matches!(repo.try_find_reference(branch.as_str()), Ok(None));' '    let free = true;' \
  tidy_history a_run_of_tidy_apply_is_listed_with_its_counts_and_restore_reads_the_same_file
m TH4 'a missing commit is restorable' src/tidy_history.rs \
  '    free && repo.has_object(id)' '    let _ = id;
    free' \
  tidy_history the_counts_follow_the_repository
m TH5 'a relative repository path is opened' src/tidy_history.rs \
  '        (p.is_absolute() && p.is_dir())' '        (p.is_dir())' \
  tidy_history the_counts_follow_the_repository
m TH6 'a link or a directory under a manifest name is listed' src/tidy_history.rs \
  '                if !m.is_file() {' '                if false && !m.is_file() {' \
  tidy_history an_unreadable_manifest_is_listed_and_foreign_files_are_not
m TH7 'a hidden file is a manifest' src/tidy_history.rs \
  '        && !s.starts_with('\''.'\'')
' '' \
  tidy_history an_unreadable_manifest_is_listed_and_foreign_files_are_not
m TH8 'the directory name is not escaped in the text' src/tidy_history.rs \
  '    let dir = escape_untrusted(&p2s(&h.manifest_dir), 300).0;' '    let dir = p2s(&h.manifest_dir);' \
  tidy_history an_escape_sequence_in_the_directory_name_is_escaped_in_the_text
m TH9 'the scan bound is ignored' src/tidy_history.rs \
  'if looked >= scan_limit {' 'if false {' \
  tidy_history every_bound_is_a_visible_marker
m TH10 'the entry bound shows no marker' src/tidy_history.rs \
  '    if wanted > counted {' '    if false {' \
  tidy_history every_bound_is_a_visible_marker
m TH11 'the time bound is ignored' src/tidy_history.rs \
  '        if started.elapsed() > budget {
            rows.push(row);' '        if false && started.elapsed() > budget {
            rows.push(row);' \
  tidy_history every_bound_is_a_visible_marker
m TH12 'an unreadable manifest is complete' src/tidy_history.rs \
  '            Err(_) => row.entry.status = "unreadable",' '            Err(_) => row.entry.status = "complete",' \
  tidy_history an_unreadable_manifest_is_listed_and_foreign_files_are_not
m TH13 'a limit of 0 is accepted' src/cli.rs \
  'range(1..=tidy_history::MAX_LIMIT as i64)' 'range(0..=tidy_history::MAX_LIMIT as i64)' \
  tidy_history a_bad_limit_or_a_repeated_flag_is_a_usage_error
m JR14 'retention deletes a recovery ref that is not the record'\''s own' src/sync/journal.rs \
  'e.recovery_ref == own && seen.insert(e.n)' 'seen.insert(e.n)' \
  sync_journal retention_keeps_ten_runs_and_spares_what_is_not_clean
m JR15 'the recovery ref has no reflog' src/sync/journal.rs \
  'force_create_reflog: true,' 'force_create_reflog: false,' \
  sync_journal a_repository_config_that_names_a_program_stops_nothing_and_no_git_is_needed
m XR15 'an operation in a linked worktree does not stop the restore' src/sync/restore.rs \
  '.any(|n| std::fs::symlink_metadata(git_dir.join(n)).is_ok())' '.any(|_| false)' \
  sync_restore a_rebase_of_the_branch_in_a_linked_worktree_stops_the_restore
m XR16 'a reason is escaped twice' src/sync/restore.rs \
  'let named = &en.recovery_ref;' 'let named = &escape_untrusted(&en.recovery_ref, 200).0;' \
  sync_restore reasons_are_escaped_once_and_only_in_the_output
m TH14 'entries that are no manifest do not count for the scan bound' src/tidy_history.rs \
  '                looked += 1;' '                let _ = looked;' \
  tidy_history every_bound_is_a_visible_marker

m LF1 'lint review: a wrong patch-id passes' src/lint_files.rs \
  'if &e.patch_id != patch_id {' 'if false && &e.patch_id != patch_id {' \
  lint_files patch_id_and_head_are_compared_with_the_repository
m LF2 'lint review: a wrong head passes' src/lint_files.rs \
  'if &e.head != head {' 'if false && &e.head != head {' \
  lint_files patch_id_and_head_are_compared_with_the_repository
m LF3 'lint review: a wrong pr= passes' src/lint_files.rs \
  'if want != pr {' 'if false && want != pr {' \
  lint_files patch_id_and_head_are_compared_with_the_repository
m LF4 'lint review: the head of the Carried-over form is its from=' src/lint_files.rs \
  'pr, to, patch_id, ..
                } => (*pr, to, patch_id),' 'pr, from, patch_id, ..
                } => (*pr, from, patch_id),' \
  lint_files patch_id_and_head_are_compared_with_the_repository
m LF5 'lint files: a carriage return is not an error' src/lint_files.rs \
  "if l.contains('\\r') {" "if l.contains('\\u{0}') {" \
  lint_files carriage_returns_are_errors_anywhere
m LF6 'lint review: 16 lines are allowed' src/lint_files.rs \
  'let r = lint_pr::rule(lint_pr::REVIEW_LENGTH);
    if n > r.limit {' 'let r = lint_pr::rule(lint_pr::REVIEW_LENGTH);
    if n > r.limit + 1 {' \
  lint_files a_review_comment_has_at_most_15_lines
m LF7 'lint files: a session link is not looked for' src/lint_files.rs \
  'if lint_pr::session_pattern(l) {' 'if false && lint_pr::session_pattern(l) {' \
  lint_files a_session_link_is_an_error
m LF8 'lint review: blank lines before the first line are skipped' src/lint_files.rs \
  "text.split('\\n').next().unwrap_or(\"\")" "text.trim_start().split('\\n').next().unwrap_or(\"\")" \
  lint_files the_first_line_follows_the_fixed_grammar
m LF9 'lint review: the diff is taken from HEAD to HEAD' src/lint_files.rs \
  'base: hex("base", &b)?,' 'base: hex("base", &h)?,' \
  lint_files the_patch_id_is_the_one_of_real_git
m LF10 'lint files: a file that is not UTF-8 is read lossily' src/lint_files.rs \
  'String::from_utf8(bytes)
        .map_err(|_| Error::usage(format!("{}: not UTF-8 text", path.display())))' 'Ok(String::from_utf8_lossy(&bytes).into_owned())' \
  lint_files usage_errors_exit_two
m LF11 'lint pr-text: the hard limit is one line higher' src/lint_files.rs \
  'if n > r.hard {' 'if n > r.hard + 1 {' \
  lint_files the_length_warns_above_25_and_fails_above_30
m LF12 'lint pr-text: the warning limit is one line higher' src/lint_files.rs \
  '} else if n > r.limit {' '} else if n > r.limit + 1 {' \
  lint_files the_length_warns_above_25_and_fails_above_30
m LF13 'lint pr-text: a missing Blocker line passes' src/lint_files.rs \
  '[] => out.push(finding(lint_pr::BLOCKER_LINE, 0, "no Blocker line")),' '[] => {}' \
  lint_files the_blocker_line_is_exactly_one_line_of_the_fixed_form
m LF14 'lint pr-text: only the first Blocker line counts' src/lint_files.rs \
  '.filter(|(_, l)| lint_pr::line_starts_with(l, "blocker", true))
        .collect();' '.filter(|(_, l)| lint_pr::line_starts_with(l, "blocker", true))
        .take(1)
        .collect();' \
  lint_files the_blocker_line_is_exactly_one_line_of_the_fixed_form
m LF15 'lint pr-text: the colon needs no blank after it' src/lint_files.rs \
  'l.strip_prefix("Blocker: ")' 'l.strip_prefix("Blocker:")' \
  lint_files the_blocker_line_is_exactly_one_line_of_the_fixed_form
m LF16 'lint pr-text: a blank Blocker text passes in a Draft' src/lint_files.rs \
  'Some(rest) if rest.trim().is_empty() => out.push(finding(' 'Some(rest) if rest.is_empty() => out.push(finding(' \
  lint_files the_blocker_line_is_exactly_one_line_of_the_fixed_form
m LF17 'lint pr-text: a Draft may say Blocker: None.' src/lint_files.rs \
  'Some(rest) if !ready && means_none(rest) => out.push(finding(' 'Some(rest) if false && means_none(rest) => out.push(finding(' \
  lint_files a_draft_names_its_blocker_a_ready_pr_has_none
m LF18 'lint pr-text: a Ready PR may name a blocker' src/lint_files.rs \
  'Some(rest) if ready && rest != "None." => out.push(finding(' 'Some(rest) if false && ready && rest != "None." => out.push(finding(' \
  lint_files a_draft_names_its_blocker_a_ready_pr_has_none
m LF19 'lint pr-text: a CR at the end of the Blocker line is part of its text' src/lint_files.rs \
  ".map(|l| l.strip_suffix('\\r').unwrap_or(l))" '.map(|l| l)' \
  lint_files a_session_link_and_a_carriage_return_are_errors
m LF20 'lint pr-text: any last line passes as the attribution line' src/lint_files.rs \
  'Some((_, l)) if l == GENERATED_LINE => {}' 'Some((_, _)) => {}' \
  lint_files the_last_non_blank_line_is_the_attribution_line
m LF21 'lint pr-text: the attribution line may have trailing blanks' src/lint_files.rs \
  'Some((_, l)) if l == GENERATED_LINE => {}' 'Some((_, l)) if l.trim() == GENERATED_LINE => {}' \
  lint_files the_last_non_blank_line_is_the_attribution_line
m LF22 'lint pr-text: the first non-blank line is taken for the last' src/lint_files.rs \
  '.rfind(|(_, l)| !l.trim().is_empty())' '.find(|(_, l)| !l.trim().is_empty())' \
  lint_files the_last_non_blank_line_is_the_attribution_line
m LF23 'lint pr-text: an empty text has no missing attribution line' src/lint_files.rs \
  'None => out.push(finding(PR_GENERATED_LINE, 0, "the text is empty")),' 'None => {}' \
  lint_files the_last_non_blank_line_is_the_attribution_line
m LF24 'lint files: an error finding exits 0' src/cli.rs \
  '    if r.passed() {
        exit::OK
    } else {
        lint_pr_cmd::EXIT_FINDINGS
    }' '    if true {
        exit::OK
    } else {
        lint_pr_cmd::EXIT_FINDINGS
    }' \
  lint_files human_output_lists_each_finding
m LF25 'lint review: --json and --human are combined' src/cli.rs \
  'if a.human && a.json {' 'if false && a.human && a.json {' \
  lint_files usage_errors_exit_two
m LF26 'lint review: --checkout without --base and --head is accepted' src/cli.rs \
  '(None, None) if a.checkout.is_none() => None,' '(None, None) => None,' \
  lint_files usage_errors_exit_two

m LF27 'lint pr-text: a full stop after none is a named blocker in a Draft' src/lint_files.rs \
  'let t = t.strip_suffix('"'"'.'"'"').unwrap_or(t);' 'let t = t;' \
  lint_files none_in_any_spelling_is_no_named_blocker_and_ready_takes_only_the_exact_line
m LF28 'lint pr-text: keiner is a named blocker in a Draft' src/lint_files.rs \
  '["none", "keiner", "keine"]' '["none", "keine"]' \
  lint_files none_in_any_spelling_is_no_named_blocker_and_ready_takes_only_the_exact_line
m LF29 'lint pr-text: keine is a named blocker in a Draft' src/lint_files.rs \
  '["none", "keiner", "keine"]' '["none", "keiner"]' \
  lint_files none_in_any_spelling_is_no_named_blocker_and_ready_takes_only_the_exact_line
m LF30 'lint pr-text: None in capitals is a named blocker in a Draft' src/lint_files.rs \
  '.any(|w| lint_pr::fold(t) == *w)' '.any(|w| t == *w)' \
  lint_files none_in_any_spelling_is_no_named_blocker_and_ready_takes_only_the_exact_line
m LF31 'lint pr-text: blanks after none are a named blocker in a Draft' src/lint_files.rs \
  'let t = rest.trim();' 'let t = rest;' \
  lint_files none_in_any_spelling_is_no_named_blocker_and_ready_takes_only_the_exact_line
m LF32 'lint pr-text: a Ready PR takes any text that begins with None' src/lint_files.rs \
  'Some(rest) if ready && rest != "None." => out.push(finding(' 'Some(rest) if ready && !rest.starts_with("None") => out.push(finding(' \
  lint_files none_in_any_spelling_is_no_named_blocker_and_ready_takes_only_the_exact_line
m LF33 'lint review: pr= has no digit limit' src/lint_files.rs \
  '!matches!(pr, PrRef::Number(n) if *n > 999_999_999)' '!matches!(pr, PrRef::Number(n) if *n > 99_999_999_999)' \
  lint_files pr_has_at_most_nine_digits_as_in_lint_pr
m LF34 'lint review: a tag is not peeled to its commit' src/lint_files.rs \
  'Ok(commit.id)' 'Ok(id.detach())' \
  lint_files an_annotated_tag_is_peeled_and_a_missing_merge_base_is_a_read_error
m LF35 'lint review: a missing merge base is left to git' src/lint_files.rs \
  'if !matches!(r.merge_base(b, h), Ok(Some(_))) {' 'if false {' \
  lint_files an_annotated_tag_is_peeled_and_a_missing_merge_base_is_a_read_error
m LF36 'lint review: git patch-id runs without --stable' src/git_runner.rs \
  'Op::PatchIdStable => v(&["patch-id", "--stable"]),' 'Op::PatchIdStable => v(&["patch-id"]),' \
  lint_files the_patch_id_is_the_one_of_real_git
m LF37 'lint review: git diff detects no renames' src/git_runner.rs \
  '"diff.renames=true",' '"diff.renames=false",' \
  lint_files the_patch_id_is_the_one_of_real_git
m LF38 'lint review: the diff is taken between BASE and HEAD, not from the merge base' src/git_runner.rs \
  'a.push(format!("{}...{}", base.as_str(), head.as_str()));' 'a.push(format!("{}..{}", base.as_str(), head.as_str()));' \
  lint_files the_patch_id_of_a_merge_head_and_of_a_head_that_is_no_descendant_is_the_one_of_real_git
m LF39 'lint review: the diff is not fed to git patch-id' src/lint_files.rs \
  '.run_with_stdin(&dir, Op::PatchIdStable, Some(&diff.stdout))' '.run_with_stdin(&dir, Op::PatchIdStable, Some(b"x"))' \
  lint_files the_patch_id_is_the_one_of_real_git

# stw prove (new): every check of the proof and the verdict
m PV1 'prove: the committer identity is not checked' src/prove.rs \
  'for (role, who) in [("author", &m.author), ("committer", &m.committer)] {' 'for (role, who) in [("author", &m.author)] {' \
  prove a_foreign_committer_is_not_proved
m PV2 'prove: the author identity is not checked' src/prove.rs \
  'for (role, who) in [("author", &m.author), ("committer", &m.committer)] {' 'for (role, who) in [("committer", &m.committer)] {' \
  prove a_foreign_identity_is_not_proved
m PV3 'prove: a lost Co-Authored-By line passes' src/prove.rs \
  'if !have.contains(&line.trim_end().to_string()) {' 'if false {' \
  prove a_missing_co_authored_by_trailer_is_not_proved
m PV4 'prove: session links are not looked for' src/prove.rs \
  '.filter(|m| lint_pr::has_session_link(&m.message))' '.filter(|_| false)' \
  prove a_session_link_in_a_new_commit_text_is_not_proved
m PV5 'prove: the file sets always count as equal' src/prove.rs \
  'if only_old.is_empty() && only_new.is_empty() {' 'if true {' \
  prove an_additional_commit_is_not_proved
m PV6 'prove: ALT equal to NEU is not named' src/prove.rs \
  'if old == new {' 'if false {' \
  prove neu_equal_to_alt_proves_nothing
m PV7 'prove: a base that is no ancestor of NEU is accepted' src/prove.rs \
  'match repo::is_ancestor(&repo, new_base, new) {' 'match repo::is_ancestor(&repo, new_base, new).map(|_| true) {' \
  prove a_rebase_without_base_says_so_and_a_base_that_is_no_ancestor_is_refused
m PV8 'prove: one ok check is enough for the verdict' src/prove.rs \
  '&& p.checks.iter().all(|c| c.state == CheckState::Ok);' '&& p.checks.iter().any(|c| c.state == CheckState::Ok);' \
  prove a_merge_commit_in_the_new_range_is_not_proved
m PV9 'prove: the Carried-over line is printed for not proved too' src/prove.rs \
  'if p.proved {' 'if true {' \
  prove reordered_commits_are_not_proved_although_the_patch_id_is_equal
m PV10 'prove: a missing identity is replaced by the fixture identity' src/prove.rs \
  'let Some((name, email)) = replay::configured_user(repo) else {' \
  'let Some((name, email)) = replay::configured_user(repo).or(Some((gix::bstr::BString::from("T"), gix::bstr::BString::from("t@example.com")))) else {' \
  prove no_configured_identity_is_unknown_never_a_pass
m PV11 'prove: commit subjects are printed raw' src/prove.rs \
  'format!("{} {}", m.id, esc(&m.subject))' 'format!("{} {}", m.id, m.subject)' \
  prove a_session_link_in_a_new_commit_text_is_not_proved
m PV12 'prove: --base is ignored for the new base' src/prove.rs \
  'Some(Some(b)) => (b, "flag"),' 'Some(Some(_)) => (old_base, "merge-base"),' \
  prove a_pure_rebase_is_proved_and_ends_with_the_line
m PV13 'prove: the pull request defaults to number 1' src/prove.rs \
  '        None => PrRef::None,' '        None => PrRef::Number(1),' \
  prove a_pure_rebase_is_proved_and_ends_with_the_line
m PV14 'prove: a repository outside the workspace is used' src/cli.rs \
  '.find(|r| real(&r.path) == top)' '.next()' \
  prove the_repository_comes_from_the_workspace_only
m PV15 'prove: a range is accepted as a revision' src/prove.rs \
  'if rev.contains("..") {' 'if false {' \
  prove usage_errors_exit_two_before_any_read

m PV16 'carry-over: the per-commit patch-ids are not asked' src/carryover.rs \
  '        res.check_commit_patches(g, req);' '        let _ = req;' \
  carryover_cli a_binary_file_with_other_contents_in_an_intermediate_commit_is_no
m PV17 'prove: merge commits of the old range are not named' src/prove.rs \
  'let old_merges = old.iter().filter(|m| m.parents > 1).count();' 'let old_merges = 0;' \
  prove a_merge_in_the_old_range_is_left_out_of_the_pairing_and_named
m PV18 'carry-over: merge commits count in the per-commit patch-ids' src/carryover.rs \
  '            if info.parent_ids.len() > 1 {
                continue;
            }' '            if false {
                continue;
            }' \
  prove a_merge_in_the_old_range_is_left_out_of_the_pairing_and_named
m PV19 'prove: the per-commit patch-id check does not stop a proof' src/prove.rs \
  '        State::Differs => CheckState::Failed,
        State::NotChecked => CheckState::NotRun,' '        State::Differs => CheckState::Ok,
        State::NotChecked => CheckState::NotRun,' \
  prove a_binary_file_with_other_contents_in_an_intermediate_commit_is_not_proved

m PV20 'prove: --old-parent is ignored' src/prove.rs \
  'Some(Some(par)) => {' 'Some(Some(par)) if false => {' \
  prove a_child_branch_restack_is_proved_with_the_old_parent
m PV21 'prove: an old parent that is no ancestor is accepted' src/prove.rs \
  'repo::is_ancestor(&repo, par, old) {' 'repo::is_ancestor(&repo, par, old).map(|_| true) {' \
  prove an_old_parent_that_is_no_ancestor_the_alt_itself_or_too_far_down_is_not_proved
m PV22 'prove: an empty old range is accepted' src/prove.rs \
  'Ok(true) if par == old => {' 'Ok(true) if false => {' \
  prove an_old_parent_that_is_no_ancestor_the_alt_itself_or_too_far_down_is_not_proved
# stw restack plan (port and whole-stack preview): levels, chain detection, fail closed
m RK1 'restack plan: a level above a conflict is not marked as inheriting it' src/restack_plan.rs \
  'l.state = LevelState::InheritedConflict;' 'l.state = LevelState::NotComputed;' \
  restack_plan a_conflict_at_level_two_is_inherited_by_level_three_like_git_rebase
m RK2 'restack plan: a level is replayed onto the base, not onto the level below' src/restack_plan.rs \
  '        new_tip = tip_after;' '        let _ = tip_after;' \
  restack_plan a_level_is_replayed_onto_the_rebased_level_below_not_onto_the_base
m RK3 'restack plan: a branch that is not on the one before is accepted' src/restack_plan.rs \
  'if !repo::is_ancestor(&repo, prev.1, *tip)? {' 'if false {' \
  restack_plan a_branch_that_is_not_on_the_one_before_is_refused_with_the_reason
m RK4 'restack plan: an old parent that is no ancestor is accepted' src/restack_plan.rs \
  'if !ok {' 'if false && !ok {' \
  restack_plan go_the_old_parent_must_be_an_ancestor_of_the_branch
m RK5 'restack plan: a differing rename setting does not make the result unknown' src/restack_plan.rs \
  'if !defaults.contains(&v.as_str()) {' 'if false {' \
  restack_plan a_rename_setting_that_differs_from_the_replay_makes_a_clean_result_unknown
m RK6 'restack plan: a merge attribute in a .gitattributes file is not seen' src/restack_plan.rs \
  'Ok(b) => found |= mentions_merge(&b.data),' 'Ok(b) => found |= { let _ = b; false },' \
  restack_plan a_merge_attribute_makes_a_clean_result_unknown
m RK7 'restack plan: an added submodule is not seen' src/restack_plan.rs \
  '{
            entry_mode.is_commit()
        }' '{
            false
        }' \
  restack_plan a_commit_that_changes_a_submodule_makes_a_clean_result_unknown
m RK8 'restack plan: the replay goes on after a conflict' src/restack_plan.rs \
  '            block = Block::Conflict;' '            block = Block::None;' \
  restack_plan a_conflict_at_level_two_is_inherited_by_level_three_like_git_rebase
m RK9 'restack plan: two branches at one commit are not ambiguous' src/restack_plan.rs \
  'if !same.is_empty() {' 'if false {' \
  restack_plan two_branches_at_one_commit_are_ambiguous_and_the_explicit_spelling_is_suggested
m RK10 'restack plan: every branch below counts as the nearest one' src/restack_plan.rs \
  'if m != n && *t != *u && ancestor(*t, *u)? {' 'if false {' \
  restack_plan the_chain_is_derived_from_the_checked_out_branch
m RK11 'restack plan: a level that main already has is only became-empty' src/restack_plan.rs \
  'if all_found {' 'if false {' \
  restack_plan a_level_that_main_already_has_is_already_upstream_like_git_rebase
m RK12 'restack plan: a merge commit is unknown, not refused' src/restack_plan.rs \
  'l.state = LevelState::Refused;' 'l.state = LevelState::Unknown;' \
  restack_plan a_merge_commit_in_a_level_is_refused_and_the_levels_above_are_not_computed
m RK13 'restack plan: conflicted paths are printed raw' src/restack_plan.rs \
  'l.conflicts.iter().map(|c| esc(&c.path)).collect();' 'l.conflicts.iter().map(|c| c.path.clone()).collect();' \
  restack_plan a_hostile_path_and_subject_are_escaped
m RK14 'restack plan: branches above the starting point are not noted' src/restack_plan.rs \
  'if *t != top.1 && ancestor(top.1, *t)? {' 'if false {' \
  restack_plan the_chain_is_derived_from_the_checked_out_branch
m RK15 'restack plan: a level that became empty is clean' src/restack_plan.rs \
  '} else if l.dropped == steps.len() {' '} else if false {' \
  restack_plan a_level_whose_change_main_made_differently_is_became_empty
m RK16 'restack plan: the exit code is 0 for a conflict' src/cli.rs \
  'Ok(if plan.is_clean() {' 'Ok(if true || plan.is_clean() {' \
  restack_plan a_conflict_at_level_two_is_inherited_by_level_three_like_git_rebase

m PV23 'prove: an empty range goes on to range-diff' src/prove.rs \
  '        if from == to {' '        if false {' \
  prove an_empty_new_range_is_range_empty_and_not_an_unsupported_git
m PV24 'prove: no hint to name the new parent' src/prove.rs \
  'if source == "old-parent" {' 'if false {' \
  prove an_old_parent_without_base_is_named_as_the_source_of_the_new_base_and_hints
m PV25 'prove: the new base from --old-parent is called flag' src/prove.rs \
  'if old_source == "flag" {' 'if false {' \
  prove an_old_parent_without_base_is_named_as_the_source_of_the_new_base_and_hints
m RK17 'restack plan: info/attributes is read from the git dir of the worktree, not the common dir' src/restack_plan.rs \
  'repo.common_dir().join("info/attributes")' 'repo.git_dir().join("info/attributes")' \
  restack_plan a_merge_attribute_in_info_attributes_of_the_common_dir_is_seen_from_a_linked_worktree
m RK18 'restack plan: a local default branch that is ahead becomes a level' src/restack_plan.rs \
  'if default_name.as_deref() == Some(n.as_str()) {' 'if false {' \
  restack_plan a_local_default_branch_that_is_ahead_of_origin_is_not_a_level
m RK19 'restack plan: the checked-out default branch is a starting point' src/restack_plan.rs \
  'if default_name.as_deref() == Some(name.as_str()) {' 'if false {' \
  restack_plan a_local_default_branch_that_is_ahead_of_origin_is_not_a_level
m RK20 'replay: the rename limit is 1000, not the 7000 of git' src/replay.rs \
  '("renameLimit", "7000"),' '("renameLimit", "1000"),' \
  restack_plan the_rename_limit_is_the_one_of_git_for_a_merge
m RK21 'restack plan: a .gitattributes added inside the range is not scanned' src/restack_plan.rs \
  'Ok(blob) if mentions_merge(&blob.data) => why.push(format!(' 'Ok(blob) if false && mentions_merge(&blob.data) => why.push(format!(' \
  restack_plan a_commit_in_the_range_that_adds_a_merge_attribute_makes_a_clean_result_unknown
m RK22 'restack plan: the singular of commit is not used' src/restack_plan.rs \
  'if l.commits == Some(1) {' 'if false {' \
  restack_plan a_refusal_without_levels_prints_no_level_count_and_the_singular_is_used
m RK23 'restack plan: a refusal prints a level count' src/restack_plan.rs \
  'let count = if self.levels.is_empty() {' 'let count = if false {' \
  restack_plan a_refusal_without_levels_prints_no_level_count_and_the_singular_is_used

m RK24 'restack plan: the binary macro is not a merge attribute' src/restack_plan.rs \
  ' | b"binary" | b"-binary" | b"!binary"' '' \
  restack_plan the_binary_macro_makes_a_clean_result_unknown_like_a_merge_attribute
m RK25 'restack plan: a macro definition that sets binary or merge is not seen' src/restack_plan.rs \
  'l.fields().skip(1).any(|t| {' 'l.fields().skip(2).any(|t| {' \
  restack_plan a_macro_that_sets_binary_or_merge_makes_a_clean_result_unknown

m GF1 'range-diff accepts Git 2.40 (GIT_ATTR_SOURCE is 2.41)' src/git_runner.rs \
  'Op::RangeDiff { .. } => Version(2, 41, 0),' 'Op::RangeDiff { .. } => Version(2, 40, 0),' \
  git_runner_ops range_diff_needs_git_2_41
m GF2 'an index-reading op accepts Git below 2.36' src/git_runner.rs \
  '            | Op::ResetKeep { .. } => Version(2, 36, 0),' '            | Op::ResetKeep { .. } => Version(2, 30, 0),' \
  git_runner_ops the_floor_of_every_op_is_declared_and_enforced
m GF3 'the version message prints the found text raw' src/git_runner.rs \
  'escape_untrusted(found, 80).0' 'found' \
  git_runner_ops the_version_message_names_the_command_and_escapes_the_found_text

m GF4 'the command layer does not name the command in the version error' src/cli.rs \
  'git_runner::shared().set_command(git_command_label(&command));' 'let _ = git_command_label(&command);' \
  carryover_cli an_old_git_is_refused_with_the_command_name

# discovery: ceiling and owner check (unit tests in src/repo/discover.rs, target `lib`)
m DC1 'a repository at or above a ceiling is still returned' src/repo/discover.rs \
  'start.starts_with(&c) && start != c && c.starts_with(&tree)' 'false' \
  lib repo::discover::tests::a_ceiling_hides_a_repository_above_it
m DC2 'a ceiling equal to the start hides the repository' src/repo/discover.rs \
  'start.starts_with(&c) && start != c && c.starts_with(&tree)' 'start.starts_with(&c) && c.starts_with(&tree)' \
  lib repo::discover::tests::a_ceiling_hides_a_repository_above_it
m DC3 'GIT_CEILING_DIRECTORIES entries are dropped' src/repo/discover.rs \
  '.filter(|p| p.is_absolute())' '.filter(|_| false)' \
  lib repo::discover::tests::the_environment_ceiling_is_read
m DC4 'a foreign-owned repository is trusted' src/repo/discover.rs \
  'if trust == gix::sec::Trust::Full {' 'if true {' \
  lib repo::discover::tests::a_foreign_owner_needs_safe_directory
m DC5 'safe.directory "*" is not honoured' src/repo/discover.rs \
  '} else if v == "*" {' '} else if v == "*not" {' \
  lib repo::discover::tests::safe_directory_rules
m DC6 'an empty safe.directory value does not reset the list' src/repo/discover.rs \
  '            allowed = false;
        } else if v == "*"' '        } else if v == "*"' \
  lib repo::discover::tests::safe_directory_rules
m DC7 'safe.directory prefix matches the directory itself' src/repo/discover.rs \
  'workdir.starts_with(prefix) && workdir != Path::new(prefix)' 'workdir.starts_with(prefix)' \
  lib repo::discover::tests::safe_directory_rules
m DC8 'safe.directory path match ignores the whole path (prefix match)' src/repo/discover.rs \
  'Path::new(v.trim_end_matches('"'/'"')) == workdir' 'workdir.starts_with(v.trim_end_matches('"'/'"'))' \
  lib repo::discover::tests::safe_directory_rules

m DC9 'default_root calls gix::discover directly' src/repo/mod.rs \
  'if let Some(repo) = discover::discover(cwd) {' 'if let Some(repo) = gix::discover(cwd).ok() {' \
  discover_cli a_ceiling_hides_the_workspace_root_of_status
m DC10 'top_level calls gix::discover directly' src/repo/mod.rs \
  'discover::discover(dir).and_then(' 'gix::discover(dir).ok().and_then(' \
  discover_cli a_ceiling_hides_the_root_of_add
m DC11 'discover() does not read GIT_CEILING_DIRECTORIES' src/repo/discover.rs \
  'discover_in(dir, std::env::var_os("GIT_CEILING_DIRECTORIES").as_ref())' 'discover_in(dir, None)' \
  discover_cli a_ceiling_hides_the_workspace_root_of_status
m DC12 'relative ceiling entries are kept' src/repo/discover.rs \
  '        .filter(|p| p.is_absolute())' '        .filter(|_| true)' \
  lib repo::discover::tests::relative_ceiling_entries_are_ignored
m DC13 'ceiling entries are not resolved through symlinks' src/repo/discover.rs \
  'let canon = |p: &Path| std::fs::canonicalize(p).unwrap_or_else(|_| p.to_path_buf());' 'let canon = |p: &Path| p.to_path_buf();' \
  lib repo::discover::tests::a_ceiling_named_through_a_symlink_hides_the_repository

m SY1 'a rebase or merge with tracked local changes is not refused without --autostash' src/sync/mod.rs \
  'if dirty > 0 && o.autostash != Some(true) {' 'if false && dirty > 0 && o.autostash != Some(true) {' \
  sync_pull local_changes_of_tracked_files_refuse_a_rebase_or_merge_without_autostash
m SY2 'the stash flag of git is always --no-autostash' src/git_runner.rs \
  '    if autostash {
        "--autostash"
    } else {
        "--no-autostash"
    }' '    if autostash {
        "--no-autostash"
    } else {
        "--no-autostash"
    }' \
  sync_journal the_argument_vectors_of_the_journal_operations_are_fixed
m SY3 'the rebase moves other branches of a stack (--no-update-refs dropped)' src/git_runner.rs \
  'v(&["rebase", "--no-update-refs", "--no-rebase-merges"])' 'v(&["rebase", "--no-rebase-merges"])' \
  sync_pull a_stack_branch_that_a_rebase_does_not_own_stays_where_it_is
m SY4 'the rebase has no --fork-point' src/git_runner.rs \
  'a.push("--fork-point".to_string());' 'a.push("--no-fork-point".to_string());' \
  sync_journal the_argument_vectors_of_the_journal_operations_are_fixed
m SY5 'a merge may only fast-forward (--ff replaced by --ff-only)' src/git_runner.rs \
  'v(&["merge", "--no-edit", "--ff", "--no-verify-signatures"])' 'v(&["merge", "--no-edit", "--ff-only", "--no-verify-signatures"])' \
  sync_pull merge_makes_a_merge_commit_of_the_local_branch_and_the_upstream
m SY6 'a stopped rebase is not recognised as a conflict' src/sync/pull.rs \
  'let stopped = after.as_ref().is_ok_and(|a| status::operation(a).is_some());' 'let stopped = false;' \
  sync_pull a_rebase_conflict_is_left_in_progress_and_exits_one
m SY7 'a conflict of the put-back stash is not recognised' src/sync/pull.rs \
  'text.contains("resulted in conflicts")' 'text.contains("never printed by git")' \
  sync_pull a_conflict_while_the_stash_is_put_back_is_an_autostash_conflict
m SY8 'a merge driver in the repository config does not refuse a merge or rebase' src/git_runner.rs \
  '|| (merges && section == "merge" && sub.is_some() && key == "driver")' '|| (false && merges && section == "merge" && sub.is_some() && key == "driver")' \
  sync_pull a_merge_driver_in_the_repository_config_refuses_the_run
m SY9 'a filter driver does not refuse a merge or rebase' src/git_runner.rs \
  '            Op::ResetKeep { .. } | Op::SyncMerge { .. } | Op::SyncRebase { .. }
        ) {' '            Op::ResetKeep { .. }
        ) {' \
  sync_pull a_filter_driver_in_the_repository_config_refuses_the_run
m SY10 'rebase accepts Git below 2.38' src/git_runner.rs \
  'Op::SyncRebase { .. } => Version(2, 38, 0),' 'Op::SyncRebase { .. } => Version(2, 30, 0),' \
  git_runner_ops the_floor_of_every_op_is_declared_and_enforced
m SY11 'the upstream of a rebase may be outside origin' src/git_runner.rs \
  'let short = full.strip_prefix("refs/remotes/origin/")?;' 'let short = full.strip_prefix("refs/remotes/")?;' \
  sync_journal only_a_remote_tracking_ref_of_origin_reaches_the_rebase_argument_vector
m SY12 'a conflict does not make the exit status 1' src/cli.rs \
  '"error" | "conflict" | "autostash-conflict"
                    )' '"error"
                    )' \
  sync_pull a_rebase_conflict_is_left_in_progress_and_exits_one
m SY13 '--merge runs a rebase' src/sync/pull.rs \
  'Diverged::Merge => Op::SyncMerge { target, autostash },' 'Diverged::Merge => Op::SyncRebase { upstream: upstream.clone(), autostash },' \
  sync_pull merge_makes_a_merge_commit_of_the_local_branch_and_the_upstream
m SY14 'the journal entry of a rebase does not record the new tip' src/sync/pull.rs \
  '.finish(i, journal::STATE_COMPLETED, &new.to_string(), "")' '.finish(i, journal::STATE_COMPLETED, "", "")' \
  sync_pull sync_restore_moves_a_rebased_branch_back
m SY15 'the identity flags are accepted without --rules' src/cli.rs \
  'if !a.rules && (!a.owner_identity.is_empty() || !a.allowed_committer.is_empty()) {' 'if false && (!a.owner_identity.is_empty() || !a.allowed_committer.is_empty()) {' \
  sync_pull the_identity_flags_need_rules_and_a_well_formed_identity
m SY16 'the author of a new rules commit is not checked' src/sync/rules.rs \
  'if !is_one_of(owners, &an, &ae) {' 'if false && !is_one_of(owners, &an, &ae) {' \
  sync_rules a_commit_of_a_foreign_author_refuses_the_update_in_a_run_and_in_a_plan
m SY17 'an allowed committer is not admitted' src/sync/rules.rs \
  'if !is_one_of(owners, &kn, &ke) && !is_one_of(committers, &kn, &ke) {' 'if !is_one_of(owners, &kn, &ke) {' \
  sync_rules a_foreign_committer_refuses_unless_it_is_an_allowed_committer
m SY18 'an allowed committer is admitted as author' src/sync/rules.rs \
  'if !is_one_of(owners, &an, &ae) {' 'if !is_one_of(owners, &an, &ae) && !is_one_of(committers, &an, &ae) {' \
  sync_rules an_allowed_committer_is_never_an_allowed_author
m SY19 'the email is compared with case' src/sync/rules.rs \
  '.any(|i| i.name == name && i.email.eq_ignore_ascii_case(email))' '.any(|i| i.name == name && i.email == email)' \
  sync_rules the_email_is_compared_without_case_and_the_name_exactly
m SY20 'the name is compared without case' src/sync/rules.rs \
  '.any(|i| i.name == name && i.email.eq_ignore_ascii_case(email))' '.any(|i| i.name.eq_ignore_ascii_case(name) && i.email.eq_ignore_ascii_case(email))' \
  sync_rules the_email_is_compared_without_case_and_the_name_exactly
m SY21 'a run without any owner identity admits every commit' src/sync/rules.rs \
  '    if owners.is_empty() {
        return Err(crate::error::Error::repository(' '    if false && owners.is_empty() {
        return Err(crate::error::Error::repository(' \
  sync_rules without_any_owner_identity_nothing_is_admitted
m SY22 'more than three faults are named' src/sync/rules.rs \
  'bad.truncate(3);' 'bad.truncate(4);' \
  sync_rules at_most_three_faults_are_named
m SY23 'a plan does not check the owners' src/sync/rules.rs \
  '    let mut bad = not_by_owner(repo, head, target, owners, &o.committers)?;' '    let mut bad = if o.apply { not_by_owner(repo, head, target, owners, &o.committers)? } else { vec![] };' \
  sync_rules a_commit_of_a_foreign_author_refuses_the_update_in_a_run_and_in_a_plan

m SY24 'a result that does not contain the upstream tip is accepted' src/sync/pull.rs \
  'if new == old || !repo::is_ancestor(&repo, tip, new).unwrap_or(false) {' 'if new == old {' \
  sync_pull a_result_that_does_not_contain_the_upstream_tip_is_raced
m SY25 'a refusal for local changes is no skip' src/sync/pull.rs \
  '    .any(|k| low.contains(k))' '    .any(|_| false)' \
  sync_pull a_refusal_for_local_changes_is_a_skip_and_any_other_failure_is_an_error
m SY26 'the journal is not trimmed after a rebase or merge' src/sync/pull.rs \
  '        j.retain(&repo);' '        let _ = &repo;' \
  sync_pull the_journal_keeps_the_ten_latest_runs_after_rebases_too
m SY27 'a merge verifies signatures with the configured gpg program' src/git_runner.rs \
  'v(&["merge", "--no-edit", "--ff", "--no-verify-signatures"])' 'v(&["merge", "--no-edit", "--ff"])' \
  sync_pull a_configured_gpg_program_is_not_started_by_a_merge
m SY28 'a rebase keeps merge commits' src/git_runner.rs \
  'v(&["rebase", "--no-update-refs", "--no-rebase-merges"])' 'v(&["rebase", "--no-update-refs"])' \
  sync_pull a_rebase_flattens_a_local_merge_commit_whatever_rebase_rebase_merges_says
m SY29 'branch.<name>.mergeoptions does not refuse a merge' src/sync/pull.rs \
  '    if mode == Diverged::Merge {
        if let Some(why) = merge_options_refusal(&repo, &branch) {' '    if mode == Diverged::Rebase {
        if let Some(why) = merge_options_refusal(&repo, &branch) {' \
  sync_pull a_configured_merge_option_set_of_the_branch_refuses_a_merge
m SY30 'the first gate counts a changed submodule' src/sync/mod.rs \
  'let mut gate_dirty = status::changes_opts(&repo, false, true)?.dirty_paths;' 'let mut gate_dirty = status::changes_opts(&repo, false, false)?.dirty_paths;' \
  sync_pull a_changed_submodule_is_no_local_change_for_the_gate
m SY31 'the gate after the fetch counts a changed submodule' src/sync/mod.rs \
  '        gate_dirty = status::changes_opts(&repo, false, true)?.dirty_paths;' '        gate_dirty = status::changes_opts(&repo, false, false)?.dirty_paths;' \
  sync_pull a_changed_submodule_is_no_local_change_for_the_gate
m SY32 'a plan does not fetch' src/sync/mod.rs \
  '    if o.apply || !o.offline {
        if o.offline {' '    if o.apply {
        if o.offline {' \
  sync_plan_fetch the_plan_fetches_the_new_commits_and_writes_nothing_but_the_tracking_refs
m SY33 'an offline plan fetches' src/sync/mod.rs \
  '    if o.apply || !o.offline {
        if o.offline {' '    if true {
        if o.offline && o.apply {' \
  sync_plan_fetch the_offline_plan_fetches_nothing
m SY34 'a plan fetches although a refspec writes outside refs/remotes/origin/' src/sync/mod.rs \
  '                fetch::check_plan_scope(&repo)' '                Ok(())' \
  sync_plan_fetch a_refspec_that_writes_outside_the_namespace_refuses_the_plan_before_any_fetch
m SY35 'the plan does not ask again after its fetch whether a run would refuse the merge' src/sync/mod.rs \
  '        (action, reason) = planned(&repo, &facts, op_now, o, gate_dirty);' '        (action, reason) = plan_of(&facts, op_now, o, gate_dirty);' \
  sync_plan_fetch a_merge_that_the_run_would_refuse_stays_refused_in_the_plan_after_the_fetch
m SY36 'an online plan with --prune only counts the refs instead of fetching them' src/sync/mod.rs \
  'if !o.apply && o.offline && o.prune == Prune::On && repo' 'if !o.apply && o.prune == Prune::On && repo' \
  sync_plan_fetch plan_prune_deletes_only_tracking_refs_of_origin_and_names_them
m SY37 'a failed fetch of a plan is no error' src/sync/mod.rs \
  '            result = "error".into();
            reason = "fetch failed".into();' '            reason = "fetch failed".into();' \
  sync_plan_fetch an_unreachable_remote_is_an_error_with_exit_1_and_changes_nothing
m SY38 'an online plan says that nothing was fetched' src/sync/mod.rs \
  '        out += &if o.offline {' '        out += &if true {' \
  sync_plan_fetch the_plan_text_says_that_the_tracking_refs_were_fetched
m SY39 'an online plan says that a run fetches the submodule first' src/sync/submodules.rs \
  '                if o.apply || !o.offline {
                    ""' '                if o.apply {
                    ""' \
  sync_flags a_recorded_commit_the_submodule_lacks_is_unreachable_and_nothing_is_fetched_for_it
m SY40 'a plan fetches without the prune choice' src/sync/mod.rs \
  'Ok(()) => fetch::fetch_origin_plan(&repo, gh, o.prune),' 'Ok(()) => fetch::fetch_origin_plan(&repo, gh, Prune::Unset),' \
  sync_plan_fetch plan_prune_deletes_only_tracking_refs_of_origin_and_names_them
m SY41 'the plan fetch prunes tags like a run (no --no-prune-tags)' src/sync/mod.rs \
  'Ok(()) => fetch::fetch_origin_plan(&repo, gh, o.prune),' 'Ok(()) => fetch::fetch_origin_with(&repo, gh, o.prune),' \
  sync_plan_fetch the_plan_never_prunes_a_tag_whatever_fetch_prune_tags_says
m SY42 'the argument vector of the plan fetch has no --no-prune-tags' src/git_runner.rs \
  '            Op::FetchOriginNoPruneTags => v(&[
                "fetch",
                "--quiet",
                "--no-tags",
                "--no-recurse-submodules",
                "--no-prune-tags",' '            Op::FetchOriginNoPruneTags => v(&[
                "fetch",
                "--quiet",
                "--no-tags",
                "--no-recurse-submodules",' \
  git_runner_sync_ops the_new_operations_have_fixed_argument_vectors
m SY43 'the scope check lets a sibling remote namespace through' src/sync/fetch.rs \
  'if !dst.is_empty() && !dst.starts_with(PRUNE_SCOPE) {' 'if !dst.is_empty() && !dst.starts_with("refs/remotes/") {' \
  sync_plan_fetch a_refspec_that_writes_outside_the_namespace_refuses_the_plan_before_any_fetch
m SY44 'a plan asks GitHub whether the remote moved' src/sync/mod.rs \
  '    if !o.offline && o.apply {' '    if !o.offline {' \
  github an_online_plan_does_not_ask_github_whether_the_remote_moved
m SY45 'the note of the offline plan about --no-prune appears in an online plan' src/sync/mod.rs \
  '    } else if !o.apply && o.offline && o.prune == Prune::Off {' '    } else if !o.apply && o.prune == Prune::Off {' \
  sync_plan_fetch plan_no_prune_keeps_the_ref_even_with_fetch_prune_true
m SY46 'the plan of --rules does not fetch' src/sync/rules.rs \
  '    } else if o.update {
        // like Go' '    } else if o.update && o.apply {
        // like Go' \
  sync_rules the_rules_plan_fetches_a_local_origin_like_go_and_writes_only_the_tracking_ref
m SY47 'the plan of --rules fetches with a refspec outside the namespace' src/sync/rules.rs \
  '            fetch::check_plan_scope(&repo)
        };' '            Ok(())
        };' \
  sync_rules the_rules_plan_refuses_a_refspec_that_writes_outside_the_namespace
m SY48 'the plan text claims a fetch after a failed one' src/sync/mod.rs \
  '        } else if failed > 0 {' '        } else if false {' \
  sync_plan_fetch the_plan_text_after_a_failed_fetch_does_not_claim_that_refs_were_fetched
m SY49 'the rules plan text says nothing was fetched after a fetch' src/sync/rules.rs \
  '            out += if r.fetch == "ok" {' '            out += if false {' \
  sync_rules the_rules_plan_fetches_a_local_origin_like_go_and_writes_only_the_tracking_ref
m SY50 'the help of --plan says that it fetches nothing' src/cli.rs \
  '/// Fetch the remote-tracking refs of origin (not with --offline) and print what a run would do; change nothing else' '/// Print what a run would do and change nothing: no fetch, no write' \
  sync_default the_help_names_plan_and_dry_run_and_hides_apply
m SY51 'the help of --prune says that a plan only says what it would do' src/cli.rs \
  'With --plan it prunes as a run does' 'With --plan it only says what it would do' \
  sync_default the_help_names_plan_and_dry_run_and_hides_apply
m SY52 'the run of --rules fetches with a refspec outside the namespace' src/sync/rules.rs \
  '            fetch::check_rules_scope(&repo)
        } else {' '            Ok(())
        } else {' \
  sync_rules the_rules_run_refuses_a_refspec_that_writes_outside_the_namespace_and_writes_nothing
m SY53 'the run of --rules refuses with the text of the plan' src/sync/rules.rs \
  '            fetch::check_rules_scope(&repo)
        } else {' '            fetch::check_plan_scope(&repo)
        } else {' \
  sync_rules the_rules_run_refuses_a_refspec_that_writes_outside_the_namespace_and_writes_nothing
m SY54 'the offline run of --rules checks the refspec although it fetches nothing' src/sync/rules.rs \
  '    if o.update && o.offline {
        rec.fetch = "skipped-offline"' '    if o.update && o.offline && fetch::check_rules_scope(&repo).is_ok() {
        rec.fetch = "skipped-offline"' \
  sync_rules the_rules_run_checks_the_refspec_only_when_it_fetches

m GL1 'a path of the index that is no gitlink counts as a submodule' src/repo/mod.rs \
  '        .filter(|e| e.mode.is_submodule())
        .map(|e| e.path(&index).to_str_lossy().into_owned())' '        .filter(|_| true)
        .map(|e| e.path(&index).to_str_lossy().into_owned())' \
  gitlinks a_file_of_the_index_is_no_gitlink
m GL2 'a conflicted gitlink (three stages) is listed three times' src/repo/mod.rs \
  '    out.dedup();
    out
}

/// Why a gitlink path' '    out
}

/// Why a gitlink path' \
  gitlinks the_paths_are_sorted_and_a_conflicted_gitlink_is_listed_once
m GL3 'the gitlink paths are not sorted' src/repo/mod.rs \
  '    out.sort();
    out.dedup();' '    out.dedup();' \
  gitlinks an_index_that_is_out_of_order_gives_the_paths_sorted
m GL4 'the repositories come from .gitmodules, not from the index' src/repo/mod.rs \
  '    let subs = submodule_paths(dir);' '    let subs = gitmodules_paths(dir);' \
  gitlinks a_gitmodules_entry_without_a_gitlink_is_no_repository
m GL5 'a repository with gitlinks but no .gitmodules file is no workspace root' src/repo/mod.rs \
  'if top.join(".gitmodules").is_file() || !submodule_paths(&top).is_empty() {' 'if top.join(".gitmodules").is_file() {' \
  gitlinks a_repository_with_gitlinks_and_no_gitmodules_file_is_a_workspace_root
m GL6 'the superproject of a checkout needs a .gitmodules file' src/repo/mod.rs \
  '        .any(|p| Path::new(p) == rel)
        .then_some(dir)' '        .any(|p| Path::new(p) == rel)
        .then_some(dir)
        .filter(|d| d.join(".gitmodules").is_file())' \
  gitlinks the_workspace_of_a_gitlink_checkout_is_its_superproject_without_a_gitmodules_file
m GL7 'a gitlink without a .gitmodules entry is left out' src/repo/mod.rs \
  '        .filter(|e| e.mode.is_submodule())
        .map(|e| e.path(&index).to_str_lossy().into_owned())' '        .filter(|e| e.mode.is_submodule() && gitmodules_paths(root).contains(&e.path(&index).to_str_lossy().into_owned()))
        .map(|e| e.path(&index).to_str_lossy().into_owned())' \
  gitlinks a_gitlink_without_a_gitmodules_entry_is_a_repository

m GL8 'add looks for the path among the gitlinks, not among the .gitmodules entries' src/add.rs \
  'crate::repo::gitmodules_paths(ws).iter().any(|s| s == p)' 'crate::repo::submodule_paths(ws).iter().any(|s| s == p)' \
  add a_gitmodules_entry_for_path_refuses_even_without_a_gitlink

m GL9 '--submodules asks .gitmodules, not the index, for submodules without a checkout' src/sync/submodules.rs \
  '    for p in repo::submodule_paths(&holder) {' '    for p in repo::gitmodules_paths(&holder) {' \
  gitlinks a_gitlink_without_a_checkout_is_unreachable_for_submodules_and_a_gitmodules_entry_is_no_record

m NS1 'a nested gitlink is not descended into (the list is flat)' src/repo/mod.rs \
  '        walk(&path, &name, depth + 1, w);' '        let _ = &name;' \
  nested nested_gitlinks_are_listed_depth_first_with_the_path_below_the_root
m NS2 'a nested repository is named without the path of its holder' src/repo/mod.rs \
  '        let name = full(&name);' '        let name = name.clone();' \
  nested nested_gitlinks_are_listed_depth_first_with_the_path_below_the_root
m NS3 'the root climbs 17 levels, not 16' src/repo/mod.rs \
  'for _ in 0..SUPERPROJECT_LEVELS {' 'for _ in 0..SUPERPROJECT_LEVELS + 1 {' \
  nested the_root_climbs_at_most_sixteen_levels
m NS4 'the root is the nearest superproject, not the top-level one' src/repo/mod.rs \
  '            let sup = topmost_superproject(&top);' '            let sup = superproject_of(&top).unwrap_or_else(|| top.clone());' \
  nested a_checkout_inside_a_nested_submodule_has_the_top_level_superproject_as_root
m NS5 'the walk fails only above limit + 1 repositories' src/repo/mod.rs \
  '        if w.repos.len() > w.limit {' '        if w.repos.len() > w.limit + 1 {' \
  nested the_walk_stops_above_the_repository_limit
m NS6 'a refused nested path is reported without the path of its holder' src/repo/mod.rs \
  '            w.rejected.push((full(&p), why));' '            w.rejected.push((p.clone(), why));' \
  nested a_refused_path_of_a_nested_repository_is_reported_with_its_full_path
m NS7 'the step of a nested pinned submodule runs against the workspace root' src/sync/submodules.rs \
  '            step(&parent, rel, rec, o);' '            let _ = (&parent, &rel); let nm = rec.name.clone(); step(root, nm, rec, o);' \
  nested a_run_moves_a_nested_pinned_submodule_to_the_commit_its_parent_records
m NS8 'an uninitialized nested submodule is named without its holder' src/sync/submodules.rs \
  '.map(|r| (r.path.clone(), r.name.clone())),' '.map(|r| (r.path.clone(), String::new())),' \
  nested an_uninitialized_nested_submodule_is_reported_as_unreachable_and_never_cloned
m NS9 'a workspace above the limit is not an error' src/repo/mod.rs \
  '    if exceeded {
        return Err(crate::error::Error::usage(format!(
            "the workspace has more than {limit} repositories"' '    if false && exceeded {
        return Err(crate::error::Error::usage(format!(
            "the workspace has more than {limit} repositories"' \
  nested the_walk_stops_above_the_repository_limit
m NS10 'status and sync list the workspace without the repository limit' src/cli.rs \
  '    let mut repos = repo::list_repos_checked(&root)?;' '    let mut repos = repo::list_repos(&root);' \
  nested a_workspace_of_more_than_256_repositories_is_refused
m NS11 'prove and restack plan pick their repository without the repository limit' src/cli.rs \
  '    let repos = repo::list_repos_checked(&root)?;' '    let repos = repo::list_repos(&root);' \
  nested a_workspace_of_more_than_256_repositories_is_refused

m NS12 'the holder of a nested gitlink is the shortest name prefix, not the longest' src/sync/submodules.rs \
  'if best.is_none_or(|(b, _)| r.name.len() > b.name.len()) {' 'if best.is_none_or(|(b, _)| r.name.len() < b.name.len()) {' \
  nested a_third_level_pinned_submodule_is_stepped_against_its_own_holder
m NS13 'the holder is looked up in the filtered sync list, not in the walk' src/sync/submodules.rs \
  '    let all = repo::list_repos(root);' '    let all: Vec<repo::RepoRef> = list.iter().filter(|r| !r.path.is_empty()).map(|r| repo::RepoRef { name: r.name.clone(), path: PathBuf::from(&r.path) }).collect();' \
  nested repo_names_a_nested_submodule_whose_holder_is_left_out_of_the_list
m NS14 'the gitlinks of the rules checkout are not looked at for uninitialized submodules' src/sync/submodules.rs \
  '            .filter(|r| r.path != root)' '            .filter(|r| r.path != root && !r.name.starts_with(".agents"))' \
  nested an_uninitialized_gitlink_of_the_rules_checkout_is_reported
m NS15 'the superproject is searched without the ceiling and the owner check' src/repo/mod.rs \
  '    let above = discover::discover(top.parent()?)?;' '    let above = gix::discover(top.parent()?).ok()?;' \
  nested a_foreign_outer_repository_behind_a_ceiling_is_no_superproject
m NS16 'any repository above counts as the superproject, whether or not it records a gitlink there' src/repo/mod.rs \
  '        .any(|p| Path::new(p) == rel)' '        .any(|_| true)' \
  nested an_unrelated_repository_between_a_checkout_and_an_outer_one_ends_the_search

m OL1 'oplog: a glued refs.log line is accepted' src/oplog/parse.rs \
  'if f.len() < 5 || !extensions_ok(&f) {' 'if f.len() < 5 {' \
  oplog_read a_glued_line_is_bad_and_not_accepted
m OL2 'oplog: a glued rewrites.log line is accepted' src/oplog/parse.rs \
  'if f.len() < 4 || !extensions_ok(&f) {' 'if f.len() < 4 {' \
  oplog_read a_glued_rewrites_line_is_bad
m OL3 'oplog: a line over 4 KB is read' src/oplog/parse.rs \
  'if l.len() > MAX_LINE || l.contains(&0) {' 'if l.contains(&0) {' \
  oplog_read a_line_over_4_kb_is_bad_and_the_stream_goes_on
# OL4 (a NUL byte is read) is an equivalent mutant: every field of both logs is validated by a character set that excludes NUL,
# so the explicit NUL check in `lines` changes no result. It stays in the code as the spec rule (section 9); no mutation.
m OL5 'oplog: an upper-case hash is accepted' src/oplog/parse.rs \
  'matches!(b, b'\''0'\''..=b'\''9'\'' | b'\''a'\''..=b'\''f'\''))
}' 'matches!(b, b'\''0'\''..=b'\''9'\'' | b'\''a'\''..=b'\''f'\'' | b'\''A'\''..=b'\''F'\''))
}' \
  oplog_read a_hash_in_upper_case_is_bad
m OL6 'oplog: a pre-old marker does not give the old value' src/oplog/timeline.rs \
  'old = mo;' 'let _ = mo;' \
  oplog_read a_pre_old_marker_gives_the_old_value_of_a_delete
m OL7 'oplog: the second 0 0 line of a deleted ref is kept' src/oplog/timeline.rs \
  'if last_new.get(&name).is_some_and(|n| is_zero(n)) {' 'if false {' \
  oplog_read a_pre_old_marker_gives_the_old_value_of_a_delete
m OL8 'oplog: a pre-same marker does not hide the packing line' src/oplog/timeline.rs \
  'if is_zero(&old) && !is_zero(&line.new) {' 'if false {' \
  oplog_read a_pre_same_marker_hides_the_packing_line
m OL9 'oplog: a marker is consumed by more than one line' src/oplog/timeline.rs \
  'markers.remove(&name)' 'markers.get(&name).cloned()' \
  oplog_read a_marker_is_consumed_by_exactly_one_line
m OL10 'oplog: the current file is read before the old generation' src/oplog/store.rs \
  'for n in [format!("{name}.1"), name.to_string()] {' 'for n in [name.to_string(), format!("{name}.1")] {' \
  oplog_read the_old_generation_is_read_before_the_current_file
m OL11 'oplog: a symbolic link as a log file is read' src/oplog/store.rs \
  'Ok(m) if m.file_type().is_symlink() => File::Unreadable("a symbolic link".into()),' 'Ok(m) if false => File::Unreadable("a symbolic link".into()),' \
  oplog_read a_symlinked_log_is_not_read
m OL12 'oplog: a symbolic link as the oplog directory is followed' src/oplog/store.rs \
  'Ok(m) if m.file_type().is_symlink() => return Err("a symbolic link".into()),' 'Ok(m) if false => return Err("a symbolic link".into()),' \
  oplog_read a_symlinked_oplog_directory_is_not_read
m OL13 'oplog: refs/remotes reflogs are read without --remotes' src/oplog/store.rs \
  'remotes || all
' 'true
' \
  oplog_read remote_tracking_refs_need_remotes_and_other_refs_need_all
m OL14 'oplog: an install record is taken for an uninstall window' src/oplog/store.rs \
  'Some(true) => Coverage::Expected,' 'Some(true) => Coverage::HookAbsent,' \
  oplog_read the_install_window_tells_uncovered_from_pre_install
m OL15 'oplog: a HEAD row is not covered by the branch line' src/oplog/timeline.rs \
  '|| (f.refname == "HEAD" && heads_new.contains(r.new.as_str()))' '|| false' \
  oplog_read the_head_row_of_a_commit_is_covered_by_the_branch_line
m OL16 'oplog: groups are never formed' src/oplog/timeline.rs \
  'Some(&(t, g)) if e.epoch - t <= 1 => g,' 'Some(&(t, g)) if false && e.epoch - t <= 1 => g,' \
  oplog_read groups_are_always_suspected
m OL17 'oplog: a log line covered by a reflog row is shown twice' src/oplog/timeline.rs \
  'if reflog_keys.contains(&(r.refname.clone(), r.line.new.clone())) {' 'if false {' \
  oplog_read a_hook_line_covers_its_reflog_row_and_a_hook_only_line_is_its_own_entry
m OL18 'oplog: the reflog message is shown raw' src/oplog/timeline.rs \
  'message: escape_untrusted(&r.message, NAME_CAP).0,' 'message: r.message,' \
  oplog_read reflog_text_is_escaped
m OL19 'oplog: a rotation counts as a capture gap' src/oplog/timeline.rs \
  'let capture = gap_lines.iter().filter(|(k, _, _)| k != "rotated").count();' 'let capture = gap_lines.len();' \
  oplog_read error_lines_are_capture_gaps_and_a_rotation_is_its_own_row
m OL20 'oplog: the files backend check is off' src/oplog/timeline.rs \
  '.is_none_or(|b| b.eq_ignore_ascii_case("files"));' '.is_none_or(|_| true);' \
  oplog_read the_files_backend_is_required_for_reflog_rows
m OL21 'oplog: bad lines are not reported' src/oplog/timeline.rs \
  'if log.bad > 0 {' 'if false {' \
  oplog_read bad_lines_are_counted_in_a_gap_and_never_shown
m OL22 'oplog: a missing install record is not reported' src/oplog/timeline.rs \
  'if window.is_none() {' 'if false {' \
  oplog_read without_an_install_record_nothing_is_claimed_to_be_uncovered
m OL23 'oplog: a line of an unknown context is dropped silently' src/oplog/timeline.rs \
  '_ => out.other.push(line),' '_ => {}' \
  oplog_read stw_lines_have_origin_stw_and_unknown_contexts_stay_out_of_the_timeline
m OL24 'oplog: names are shown encoded' src/oplog/timeline.rs \
  'Some(b) => String::from_utf8_lossy(&b).into_owned(),' 'Some(_) => encoded.to_string(),' \
  oplog_read ref_names_are_decoded_and_escaped
m OL25 'oplog evolog: a merge is not flagged' src/oplog/evolog.rs \
  'fl.push(format!("merge={}", preds.len()));' 'fl.push(String::new());' \
  oplog_read merges_splits_cycles_and_repeated_commits_are_flagged
m OL26 'oplog evolog: a self pair counts as a pair' src/oplog/evolog.rs \
  'if l.old == l.new || !seen.insert((l.old.clone(), l.new.clone())) {' 'if !seen.insert((l.old.clone(), l.new.clone())) {' \
  oplog_read merges_splits_cycles_and_repeated_commits_are_flagged
m OL27 'oplog evolog: an equal pair is taken twice' src/oplog/evolog.rs \
  'if l.old == l.new || !seen.insert((l.old.clone(), l.new.clone())) {' 'if l.old == l.new {' \
  oplog_read the_first_of_two_equal_pairs_wins_and_an_unknown_kind_is_shown
m OL28 'oplog evolog: the limit does not stop the chain' src/oplog/evolog.rs \
  'if self.count >= self.limit {' 'if false {' \
  oplog_read the_limit_stops_the_chain
m OL29 'oplog evolog: a missing commit has no subject text' src/oplog/evolog.rs \
  'Err(_) => gone(),' 'Err(_) => String::new(),' \
  oplog_read a_predecessor_that_is_gone_says_so
m OL30 'oplog evolog: an amend in the reflog is not compared' src/oplog/evolog.rs \
  'msg.starts_with("commit (amend)") ||' 'false ||' \
  oplog_read a_reflog_rewrite_without_a_hook_pair_is_uncovered
m OL31 'oplog undo-plan: a branch checked out is not refused' src/oplog/undo_plan.rs \
  'if format!("refs/heads/{b}") != reference {' 'if true {' \
  oplog_read the_plan_refuses_what_undo_would_refuse
m OL32 'oplog undo-plan: a running operation is not refused' src/oplog/undo_plan.rs \
  'if d.join(m).symlink_metadata().is_ok() {' 'if false {' \
  oplog_read a_running_operation_and_an_unknown_ref_and_a_symbolic_ref_are_refused
m OL33 'oplog undo-plan: a changed ref is accepted' src/oplog/undo_plan.rs \
  'if *tip == cur_s {' 'if true {' \
  oplog_read the_plan_refuses_what_undo_would_refuse
m OL34 'oplog undo-plan: the creation entry is a target' src/oplog/undo_plan.rs \
  'if is_zero(&alt) {' 'if false {' \
  oplog_read the_plan_refuses_what_undo_would_refuse
m OL35 'oplog undo-plan: a symbolic ref is not refused' src/oplog/undo_plan.rs \
  'gix::refs::TargetRef::Symbolic(n) => Some(n.as_bstr().to_string()),' 'gix::refs::TargetRef::Symbolic(_) => None::<String>,' \
  oplog_read a_running_operation_and_an_unknown_ref_and_a_symbolic_ref_are_refused
m OL36 'oplog undo-plan: a refused plan is executable' src/oplog/undo_plan.rs \
  'self.refused == 0 && self.branch' 'self.branch' \
  oplog_read the_plan_refuses_what_undo_would_refuse
m OL37 'oplog undo-plan: a refused plan exits 0' src/cli.rs \
  'Ok(if plan.refused == 0 {' 'Ok(if true {' \
  oplog_read the_plan_refuses_what_undo_would_refuse
m OL38 'oplog evolog: --limit 0 is accepted' src/cli.rs \
  'if limit == 0 {' 'if false {' \
  oplog_read evolog_usage_errors
m OL39 'oplog undo-plan: --steps 0 is accepted' src/cli.rs \
  'if a.steps == Some(0) {' 'if false {' \
  oplog_read undo_plan_usage_errors
m OL40 'oplog undo-plan: --to accepts upper case' src/cli.rs \
  'if to.is_empty() || !to.bytes().all(|b| matches!(b, b'\''0'\''..=b'\''9'\'' | b'\''a'\''..=b'\''f'\'')) {' 'if false {' \
  oplog_read undo_plan_usage_errors

m OL41 'oplog evolog: the JSON leaves out the unreadable logs' src/oplog/evolog.rs \
  '"log_exists": self.log_exists, "unreadable": unreadable,' '"log_exists": self.log_exists, "unreadable": Vec::<Value>::new(),' \
  oplog_read the_evolog_json_names_an_unreadable_log_and_a_missing_file
m OL42 'oplog: a percent escape is not checked' src/oplog/parse.rs \
  '(s == "HEAD" || s.starts_with("refs/")) && all_bytes(s, name_byte) && percent_ok(s)' '(s == "HEAD" || s.starts_with("refs/")) && all_bytes(s, name_byte)' \
  oplog_read a_name_with_a_broken_percent_escape_is_bad
m OL43 'oplog: the worktree id may hold any character' src/oplog/parse.rs \
  'b.is_ascii_alphanumeric() || b"._?-".contains(&b)' 'true' \
  oplog_read a_worktree_id_outside_the_reduced_character_set_is_bad
m OL44 'oplog: a log that is not a regular file is read' src/oplog/store.rs \
  'Ok(m) if !m.is_file() => File::Unreadable("not a regular file".into()),' 'Ok(m) if false => File::Unreadable("not a regular file".into()),' \
  oplog_read a_log_that_is_a_directory_is_not_read_as_a_regular_file
m OL45 'oplog: the size limit of a log file is not applied' src/oplog/store.rs \
  'Ok(m) if m.len() > MAX_FILE => {' 'Ok(m) if false && m.len() > MAX_FILE => {' \
  oplog_read a_log_over_the_size_limit_is_not_read
m OL46 'oplog undo-plan: an ambiguous --to prefix is accepted' src/oplog/undo_plan.rs \
  'if olds.len() > 1 {' 'if false && olds.len() > 1 {' \
  oplog_read an_ambiguous_to_prefix_is_refused
m OL47 'oplog: an empty line is a bad line' src/oplog/parse.rs \
  '        .filter(|l| !l.is_empty())' '        .filter(|_| true)' \
  lib oplog::parse::tests::empty_lines_are_skipped_and_are_neither_lines_nor_bad
m OL48 'oplog: a line of white space is skipped like an empty one' src/oplog/parse.rs \
  '        .filter(|l| !l.is_empty())' '        .filter(|l| !l.iter().all(u8::is_ascii_whitespace))' \
  lib oplog::parse::tests::bad_lines_are_counted_and_skipped
m OL49 'oplog: an empty line is a bad line in the gaps section of the timeline' src/oplog/parse.rs \
  '        .filter(|l| !l.is_empty())' '        .filter(|_| true)' \
  oplog_read empty_lines_are_not_bad_lines
# `stw status PATH..` (prefix AL: SA is taken by `stw add`)
m AL1 'the depth bound does not cut the walk of the submodules' src/repo/mod.rs \
  '    if depth >= w.max_depth && subs.iter()' '    if false && depth >= w.max_depth && subs.iter()' \
  status_all the_depth_bounds_the_scan_and_a_cut_is_a_notice
m AL2 'a nested repository has the depth of its holder' src/repo/mod.rs \
  '        w.depths.push(depth + 1);' '        w.depths.push(depth);' \
  status_all the_json_is_the_status_document_plus_the_additive_fields
m AL3 'a cut is named by its name, not by its key' src/status_all.rs \
  '            let k = key(&top.join(c));' '            let k = c.clone();' \
  status_all the_depth_bounds_the_scan_and_a_cut_is_a_notice
m AL4 'a run lists more repositories than --limit' src/status_all.rs \
  '            if out.targets.len() > limit {' '            if false && out.targets.len() > limit {' \
  status_all more_repositories_than_the_limit_is_a_usage_error
m AL5 'a remote upstream counts as a local parent' src/status_all.rs \
  '    if remote.as_slice() != b"." {' '    if false && remote.as_slice() != b"." {' \
  status_all the_stack_is_the_chain_of_local_upstreams_of_the_current_branch
m AL6 'the chain has the current branch first' src/status_all.rs \
  '        chain.insert(0, parent.clone());' '        chain.push(parent.clone());' \
  status_all the_stack_is_the_chain_of_local_upstreams_of_the_current_branch
m AL7 'the links count every branch' src/status_all.rs \
  '.filter(|(n, _)| local_parent(repo, n).is_some())' '.filter(|(_, _)| true)' \
  status_all the_stack_is_the_chain_of_local_upstreams_of_the_current_branch
m AL8 'a cycle of local upstreams is not noticed' src/status_all.rs \
  '        if seen.contains(&parent) {' '        if false && seen.contains(&parent) {' \
  status_all a_cycle_of_local_upstreams_ends_the_chain_with_a_notice
m AL9 'the status of a parent does not count its changed submodules' src/status.rs \
  '    for item in platform.into_iter(Vec::<BString>::new())? {' '    let platform = platform.index_worktree_submodules(None);
    for item in platform.into_iter(Vec::<BString>::new())? {' \
  status_all counts_sync_forms_and_attention_follow_the_status_rules
m AL10 'a submodule is not looked into by the status of its parent' src/status.rs \
  '    for item in platform.into_iter(Vec::<BString>::new())? {' '    let platform = platform.index_worktree_submodules(None);
    for item in platform.into_iter(Vec::<BString>::new())? {' \
  status_all a_repository_that_cannot_be_read_is_an_error_row_and_exits_1
m AL11 'an unborn branch has a stack' src/status.rs \
  '        if let (true, Some(b), Some(_)) = (opts.stack, &s.facts.branch, &s.facts.head) {' '        if let (true, Some(b), _) = (opts.stack, &s.facts.branch, &s.facts.head) {' \
  status_all counts_sync_forms_and_attention_follow_the_status_rules
m AL12 'a failed read gives a total of ? instead of n/a' src/status_all.rs \
  '        Source::Read { .. } => number(s),' '        Source::Read { .. } | Source::Failed => number(s),' \
  status_all github_counts_failed_reads_and_totals
m AL13 'a full page is shown as an exact count' src/status_all.rs \
  '        } => format!("{open}{}", if truncated { "+" } else { "" }),' '        } => format!("{open}{}", if truncated && false { "+" } else { "" }),' \
  status_all a_full_page_is_a_lower_bound
m AL14 'HTTP 403 is not told from other errors' src/status_all.rs \
  '    } else if m.starts_with("HTTP 403") {' '    } else if m.starts_with("HTTP 4030") {' \
  status_all github_counts_failed_reads_and_totals
m AL15 'no repository has an attention mark' src/status_all.rs \
  '        let mark = if status::reasons(s).is_empty() {' '        let mark = if true {' \
  status_all counts_sync_forms_and_attention_follow_the_status_rules
m AL16 'the totals do not sum ahead' src/status_all.rs \
  '        ahead += s.facts.ahead.unwrap_or(0);' '        let _ = &ahead;' \
  status_all the_text_lists_every_repository_of_a_nested_workspace_in_one_line_each
m AL17 'a staged change is counted as unstaged in the line' src/status_all.rs \
  '        (c.staged, "staged"),' '        (c.staged, "unstaged"),' \
  status_all counts_sync_forms_and_attention_follow_the_status_rules
m AL18 'an unborn branch has the sync text of a branch without upstream' src/status_all.rs \
  '    } else if f.head.is_none() {' '    } else if false && f.head.is_none() {' \
  status_all counts_sync_forms_and_attention_follow_the_status_rules
m AL19 'a detached head has the sync text of a branch without upstream' src/status_all.rs \
  '    } else if f.branch.is_none() {' '    } else if false && f.branch.is_none() {' \
  status_all counts_sync_forms_and_attention_follow_the_status_rules
m AL20 'the branch is shown without escapes' src/status_all.rs \
  '            Some(b) => status::escape_for_text(b),' '            Some(b) => b.clone(),' \
  status_all text_from_outside_is_shown_escaped_and_the_run_writes_nothing
m AL21 'the root has the kind submodule' src/status_all.rs \
  'json!(if s.depth == 0 { "root" } else { "submodule" }),' 'json!(if s.depth == 99 { "root" } else { "submodule" }),' \
  status_all the_json_is_the_status_document_plus_the_additive_fields
m AL22 'options.timeout is not the deadline' src/status_all.rs \
  '        o.insert("timeout".into(), json!(scan.timeout_secs));' '        o.insert("timeout".into(), json!(0));' \
  status_all the_json_is_the_status_document_plus_the_additive_fields
m AL23 '--all conflicts with --worktrees no more' src/cli.rs \
  'conflicts_with_all = ["long", "icons", "worktrees", "review", "stats", "verbose"])]' 'conflicts_with_all = ["long", "icons", "review", "stats", "verbose"])]' \
  status_all flags_that_do_not_fit_are_usage_errors
m AL24 '--depth is accepted when every PATH is a repository' src/status_all.rs \
  '    if !scanned && (depth.is_some() || limit.is_some()) {' '    if false && !scanned && (depth.is_some() || limit.is_some()) {' \
  status_all depth_and_limit_need_a_directory_that_is_scanned
m AL25 'the stack is not read with a PATH' src/cli.rs \
  '        stack: res.is_some(),' '        stack: false,' \
  status_all the_stack_is_the_chain_of_local_upstreams_of_the_current_branch
m AL26 'a stack cycle notice has another text' src/status_all.rs \
  'format!("{name}: stack cycle at {}", status::escape_for_text(at))' 'format!("{name}: stack loop at {}", status::escape_for_text(at))' \
  status_all a_cycle_of_local_upstreams_ends_the_chain_with_a_notice
m AL27 'the chain is joined without the arrow' src/status_all.rs \
  '                    .join("<-")' '                    .join("-")' \
  status_all the_stack_is_the_chain_of_local_upstreams_of_the_current_branch

# OW: stw writes its own refs.log lines and rotates (package 2)
m OW1 'oplog write: a long name is not cut' src/oplog/write.rs \
  'let cut = &name[..name.len().min(NAME_CUT)];' 'let cut = name;' \
  lib oplog::write::tests::djb2_and_encoding_match_the_spec_vectors
m OW2 'oplog write: percent escapes in lower case' src/oplog/write.rs \
  '"%{b:02X}"' '"%{b:02x}"' \
  lib oplog::write::tests::djb2_and_encoding_match_the_spec_vectors
m OW3 'oplog write: djb2 with another multiplier' src/oplog/write.rs \
  'h.wrapping_mul(33)' 'h.wrapping_mul(31)' \
  lib oplog::write::tests::djb2_and_encoding_match_the_spec_vectors
m OW4 'oplog write: the hash is taken over the cut name' src/oplog/write.rs \
  'djb2(name))' 'djb2(cut))' \
  lib oplog::write::tests::djb2_and_encoding_match_the_spec_vectors
m OW5 'oplog write: a worktree id with a slash is kept' src/oplog/write.rs \
  'if bytes.contains(&b'\''/'\'') {' 'if false {' \
  lib oplog::write::tests::worktree_field_reduces_the_id
m OW6 'oplog write: the worktree id is not cut' src/oplog/write.rs \
  '.take(40)' '.take(400)' \
  lib oplog::write::tests::worktree_field_reduces_the_id
m OW7 'oplog write: no line feed before a line that follows a broken one' src/oplog/write.rs \
  "        buf.push(b'\\n');
    }
    buf.extend_from_slice(line.as_bytes());" "        let _ = &buf;
    }
    buf.extend_from_slice(line.as_bytes());" \
  oplog_write a_broken_last_line_gets_a_line_feed_first
m OW8 'oplog write: a symbolic oplog directory is followed' src/oplog/write.rs \
  'return Err(format!("{} is a symbolic link", p.display()))' '()' \
  oplog_write an_oplog_directory_that_is_a_symbolic_link_fails_no_sync_and_leaves_a_gap_line
m OW9 'oplog write: refs/remotes is logged without remotes=on' src/oplog/write.rs \
  'if u.refname.starts_with("refs/remotes/") && !Oplog::new(common).remotes_on() {' 'if false && u.refname.starts_with("refs/remotes/") && !Oplog::new(common).remotes_on() {' \
  oplog_write stw_lines_below_refs_remotes_follow_the_remotes_switch
m OW10 'oplog write: an update with old equal to new is logged' src/oplog/write.rs \
  '|| u.old == u.new {' '|| false {' \
  oplog_write unlogged_updates_are_skipped
m OW11 'oplog write: a pseudo ref is logged' src/oplog/write.rs \
  '!(u.refname == "HEAD" || u.refname.starts_with("refs/"))' 'false' \
  oplog_write unlogged_updates_are_skipped
m OW12 'oplog write: the rotation limit is doubled' src/oplog/write.rs \
  'm.is_file() && m.len() > max' 'm.is_file() && m.len() > max * 2' \
  oplog_write a_log_over_10_mib_is_rotated_to_one_old_generation_after_the_append
m OW13 'oplog write: a file at the limit is rotated' src/oplog/write.rs \
  'm.is_file() && m.len() > max' 'm.is_file() && m.len() >= max' \
  oplog_write only_a_log_above_the_limit_is_renamed_and_a_rotation_is_no_failure
m OW14 'oplog write: a file that is not large (or missing) is renamed' src/oplog/write.rs \
  'if !big(name) {' 'if false {' \
  oplog_write only_a_log_above_the_limit_is_renamed_and_a_rotation_is_no_failure
m OW15 'oplog write: a held rotate lock does not stop the rotation' src/oplog/write.rs \
  'Err(std::fs::TryLockError::WouldBlock) => return,' 'Err(std::fs::TryLockError::WouldBlock) => {}' \
  oplog_write a_held_rotate_lock_means_no_rotation_no_waiting_and_a_successful_command
m OW16 'oplog write: the rotated gap line has no file name' src/oplog/write.rs \
  'Ok(()) => gap(common, "rotated", 0, name),' 'Ok(()) => gap(common, "rotated", 0, "-"),' \
  oplog_write a_log_over_10_mib_is_rotated_to_one_old_generation_after_the_append
m OW17 'oplog write: a failed write leaves no gap line' src/oplog/write.rs \
  'gap(common, "write-failed", 1, "-");' '' \
  oplog_write an_oplog_directory_that_is_a_symbolic_link_fails_no_sync_and_leaves_a_gap_line
m OW18 'oplog write: no rotation after the append' src/oplog/write.rs \
  'Ok(()) => rotate_if_needed(common, max),' 'Ok(()) => {}' \
  oplog_write a_log_over_10_mib_is_rotated_to_one_old_generation_after_the_append
m OW19 'oplog write: the oplog directory is world readable' src/oplog/write.rs \
  '.mode(0o700)' '.mode(0o755)' \
  oplog_write file_and_directory_modes_are_0600_and_0700
m OW20 'oplog write: the worktree field is always main' src/oplog/write.rs \
  '(repo.git_dir() != common)' '(false)' \
  oplog_write a_linked_worktree_writes_its_id_in_the_worktree_field
m OW21 'oplog write: the sync recovery ref create is not logged' src/sync/journal.rs \
  '            old: None,
            new: Some(id),' '            old: Some(id),
            new: Some(id),' \
  oplog_write a_sync_writes_the_recovery_ref_and_the_branch_move_as_stw_lines
m OW22 'oplog write: the sync recovery ref delete is not logged' src/sync/journal.rs \
  '            old: Some(id),
            new: None,' '            old: Some(id),
            new: Some(id),' \
  oplog_write the_deleted_recovery_refs_of_old_runs_are_written_as_delete_lines
m OW23 'oplog write: the fast-forward is not logged' src/sync/fast_forward.rs \
  '            old: Some(from),
            new: Some(to),' '            old: Some(to),
            new: Some(to),' \
  oplog_write a_sync_writes_the_recovery_ref_and_the_branch_move_as_stw_lines
m OW24 'oplog write: the fast-forward is logged for HEAD, not the branch' src/sync/fast_forward.rs \
  'Ok(Target::Symbolic(n)) => n.as_bstr().to_string(),' 'Ok(Target::Symbolic(_)) => "HEAD".to_string(),' \
  oplog_write a_sync_writes_the_recovery_ref_and_the_branch_move_as_stw_lines
m OW25 'oplog write: the tidy branch delete is not logged' src/tidy_apply.rs \
  '            old: Some(sha),
            new: None,' '            old: Some(sha),
            new: Some(sha),' \
  tidy_apply deleting_and_restoring_branches_are_logged_as_stw_lines
m OW26 'oplog write: the tidy restore is not logged' src/tidy_apply.rs \
  '                    old: None,
                    new: Some(sha),' '                    old: Some(sha),
                    new: Some(sha),' \
  tidy_apply deleting_and_restoring_branches_are_logged_as_stw_lines
m OW27 'oplog timeline: one rotation already says truncated' src/oplog/timeline.rs \
  'if n >= 2 {' 'if n >= 1 {' \
  oplog_read two_rotations_of_one_log_are_reported_as_truncated
m OW28 'oplog timeline: rotations of both logs are added up' src/oplog/timeline.rs \
  'k == "rotated" && last == name' 'k == "rotated"' \
  oplog_read two_rotations_of_one_log_are_reported_as_truncated
m OW29 'oplog write: a symbolic link as the log file is written through' src/oplog/write.rs \
  'Err(format!("{} is a symbolic link", path.display()))' 'Ok(())' \
  oplog_write a_log_file_that_is_a_symbolic_link_is_not_written_through
m OW30 'oplog write: a lost mkdir race is a write failure' src/oplog/write.rs \
  'Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => {' 'Err(e) if false && e.kind() == std::io::ErrorKind::AlreadyExists => {' \
  lib oplog::write::tests::a_directory_that_exists_already_is_accepted_and_nothing_else_is
m OW31 'oplog write: an existing non-directory is accepted after a lost race' src/oplog/write.rs \
  'Ok(_) => Err(format!("{} is not a directory", p.display())),
                Err(e) => Err(format!("{}: {e}", p.display())),
            }
        }' 'Ok(_) => Ok(()),
                Err(e) => Err(format!("{}: {e}", p.display())),
            }
        }' \
  lib oplog::write::tests::a_directory_that_exists_already_is_accepted_and_nothing_else_is
m OW32 'oplog write: a rename failure leaves no gap line' src/oplog/write.rs \
  'Err(_) => gap(common, "rotate-failed", 1, name),' 'Err(_) => {}' \
  oplog_write a_rename_that_fails_is_noted_as_rotate_failed_and_the_command_succeeds
m OW33 'oplog write: rewrites.log is not rotated' src/oplog/write.rs \
  'let names = ["refs.log", "rewrites.log"];' 'let names = ["refs.log"];' \
  oplog_write rewrites_log_is_rotated_too
m OW34 'oplog write: a symbolic rotate.lock is followed' src/oplog/write.rs \
  'if refuse_link(&dir.join("rotate.lock")).is_err() {' 'if false {' \
  oplog_write a_rotate_lock_that_is_a_symbolic_link_stops_the_rotation
m OW35 'oplog write: the worktree id is converted lossily' src/oplog/write.rs \
  'id.as_bytes().to_vec();' 'id.to_string_lossy().into_owned().into_bytes();' \
  lib oplog::write::tests::worktree_field_reduces_the_id
m OW36 'oplog write: a lost mkdir race is a write failure (threads)' src/oplog/write.rs \
  'Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => {' 'Err(e) if false && e.kind() == std::io::ErrorKind::AlreadyExists => {' \
  lib oplog::write::tests::many_writers_on_a_fresh_repository_lose_no_line
m OW37 'oplog write: the wait for the lock of a log never ends' src/oplog/write.rs \
  '        if left.is_zero() {
            return false;
        }' '        let _ = left;' \
  lib oplog::write::tests::a_lock_that_stays_held_does_not_stop_the_append
m OW38 'oplog write: the lock of a log is tried once, no retry' src/oplog/write.rs \
  '    let end = Instant::now() + budget;' '    let end = Instant::now();' \
  lib oplog::write::tests::a_lock_that_is_released_within_the_budget_is_taken
m OW39 'oplog write: a lock that stays held is a write failure' src/oplog/write.rs \
  '    let _ = lock_within(&f, LOCK_BUDGET);' '    if !lock_within(&f, LOCK_BUDGET) {
        return Err("locked".into());
    }' \
  lib oplog::write::tests::a_lock_that_stays_held_does_not_stop_the_append
m OW40 'oplog write: the lock budget is 30 s' src/oplog/write.rs \
  'Duration::from_millis(300);' 'Duration::from_millis(30000);' \
  lib oplog::write::tests::a_lock_that_stays_held_does_not_stop_the_append
m OW41 'oplog write: the lock wait sleeps over the whole rest of the budget' src/oplog/write.rs \
  '        std::thread::sleep(pause.min(left));' '        std::thread::sleep(left);' \
  lib oplog::write::tests::lock_within_takes_a_lock_that_is_released_soon_and_holds_it
m OW42 'oplog write: the first pause of the lock wait is 200 ms' src/oplog/write.rs \
  '    let mut pause = Duration::from_millis(2);' '    let mut pause = Duration::from_millis(200);' \
  lib oplog::write::tests::lock_within_takes_a_lock_that_is_released_soon_and_holds_it
m OW43 'oplog write: the lock is taken and released at once' src/oplog/write.rs \
  '            Ok(()) => return true,' '            Ok(()) => {
                let _ = f.unlock();
                return true;
            }' \
  lib oplog::write::tests::lock_within_takes_a_lock_that_is_released_soon_and_holds_it
m OW44 'oplog write: the lock budget is 3 s' src/oplog/write.rs \
  'Duration::from_millis(300);' 'Duration::from_millis(3000);' \
  lib oplog::write::tests::a_lock_that_stays_held_does_not_stop_the_append
m OW45 'oplog write: lock_within reports a lock it did not get as taken' src/oplog/write.rs \
  '        if left.is_zero() {
            return false;
        }' '        if left.is_zero() {
            return true;
        }' \
  lib oplog::write::tests::lock_within_gives_up_when_the_budget_is_used_up
m AL28 'the depth of a repository is not the one of its target' src/cli.rs \
  '            s.depth = t.depth;' '            s.depth = 0;' \
  status_all a_submodule_named_like_the_root_directory_has_its_own_key
m AL29 'the class of a failed read is found anywhere in the message' src/status_all.rs \
  'm.starts_with("sent an unexpected")' 'message.contains("unexpected")' \
  status_all the_class_of_a_failed_read_comes_from_the_start_of_the_message
m AL30 'a gitlink without a checkout counts for the depth bound' src/repo/mod.rs \
  'subs.iter().any(|p| dir.join(p).join(".git").exists())' '!subs.is_empty()' \
  status_all a_gitlink_without_a_checkout_is_no_depth_limit_notice
m AL31 'the reason of the run is not named in the row' src/status_all.rs \
  'Some(note_code.unwrap_or("not-read"))' 'Some("not-read")' \
  status_all the_row_names_why_github_was_not_read_and_which_read_failed
m AL32 'the reason of a failed issue read is not named in the row' src/status_all.rs \
  '(_, Source::Failed) => s.issue_error.as_deref().map(classify),' '(_, Source::Failed) => None,' \
  status_all the_row_names_why_github_was_not_read_and_which_read_failed
m AL33 'a PATH is allowed with --stats' src/cli.rs \
  '"review", "stats", "verbose"])]' '"review", "verbose"])]' \
  status_all stats_verbose_and_review_are_usage_errors_with_a_path
m AL34 'the key of a repository below the current directory is not relative' src/status_all.rs \
  '    if let Ok(rel) = real.strip_prefix(cwd) {' '    if let Ok(rel) = real.strip_prefix(cwd).map_err(|_| ()).and_then(|_| Err::<&Path, ()>(())) {' \
  status_all the_json_is_the_status_document_plus_the_additive_fields
m AL35 'the key of an absolute path outside the current directory is relative' src/status_all.rs \
  '    if absolute {' '    if false && absolute {' \
  status_all the_key_of_a_repository_is_its_path_and_the_name_is_only_display
m AL36 'the key of a relative path outside the current directory has no .. components' src/status_all.rs \
  '    let mut parts: Vec<String> = vec!["..".into(); b.len() - common];' '    let mut parts: Vec<String> = vec![];' \
  status_all the_key_of_a_repository_is_its_path_and_the_name_is_only_display
m AL37 'a repository named twice is listed twice' src/status_all.rs \
  '            if seen.contains(&real) {' '            if false && seen.contains(&real) {' \
  status_all several_paths_keep_their_order_and_a_repository_is_listed_once
m AL38 'the paths are not taken in the order of the arguments' src/status_all.rs \
  '    for a in &args {' '    for a in args.iter().rev() {' \
  status_all several_paths_keep_their_order_and_a_repository_is_listed_once
m AL39 'the children of a scanned directory are not in path order' src/repo/mod.rs \
  '    kids.sort();' '    kids.sort_by(|a, b| b.cmp(a));' \
  status_all a_directory_without_a_repository_is_scanned_below_in_sorted_order
m AL40 'a scan follows symbolic links' src/repo/mod.rs \
  '        .filter(|e| e.file_type().is_ok_and(|t| t.is_dir()))' '        .filter(|e| e.path().is_dir())' \
  status_all a_directory_without_a_repository_is_scanned_below_in_sorted_order
m AL41 'a scan enters the repositories it found' src/repo/mod.rs \
  '        } else if has_repo {' '        } else if false && has_repo {' \
  status_all a_directory_without_a_repository_is_scanned_below_in_sorted_order
m AL42 'a bare repository is listed by a scan' src/repo/mod.rs \
  '        let has_repo = path.join(".git").exists();' '        let has_repo = path.join(".git").exists() || path.join("HEAD").exists();' \
  status_all a_directory_without_a_repository_is_scanned_below_in_sorted_order
m AL43 'repositories below the depth bound of a scan are not noticed' src/repo/mod.rs \
  '            s.beyond |= has_repo;' '            s.beyond |= false && has_repo;' \
  status_all the_depth_bounds_the_scan_and_a_cut_is_a_notice
m AL44 'a scan looks one level too deep' src/repo/mod.rs \
  '        if level + 1 > max_level {' '        if level + 1 > max_level + 1 {' \
  status_all the_depth_bounds_the_scan_and_a_cut_is_a_notice
m AL45 'the submodules of a found repository ignore the level it was found at' src/status_all.rs \
  '            add(&dir, depth - level, a.absolute, &mut out)?;' '            add(&dir, depth, a.absolute, &mut out)?;' \
  status_all the_depth_bounds_the_scan_and_a_cut_is_a_notice
m AL46 '--repo is accepted with a PATH' src/cli.rs \
  '    if !c.repos.is_empty() {
        return Err(Error::usage("--repo does not apply with a PATH"));' '    if false && !c.repos.is_empty() {
        return Err(Error::usage("--repo does not apply with a PATH"));' \
  status_all flags_that_do_not_fit_are_usage_errors
m AL47 '--depth is accepted without a PATH' src/cli.rs \
  '    if a.paths.is_empty() && (a.depth.is_some() || a.limit.is_some()) {' '    if false && a.paths.is_empty() && (a.depth.is_some() || a.limit.is_some()) {' \
  status_all depth_and_limit_need_a_directory_that_is_scanned
m AL48 'a PATH that does not exist is taken for a missing directory' src/status_all.rs \
  '            .map_err(|e| Error::usage(format!("PATH {shown}: {e}")))?;' '            .unwrap_or_else(|_| cwd.join(p));' \
  status_all a_path_that_does_not_exist_or_cannot_be_read_is_a_usage_error_and_prints_nothing
m AL49 'a PATH that is a file is not refused as such' src/status_all.rs \
  '        if !real.is_dir() {' '        if false && !real.is_dir() {' \
  status_all a_path_that_does_not_exist_or_cannot_be_read_is_a_usage_error_and_prints_nothing
m AL50 'a directory that cannot be read is not refused' src/status_all.rs \
  '        if let Err(e) = std::fs::read_dir(&real) {' '        if let Err(e) = Ok::<(), std::io::Error>(()) {' \
  status_all a_path_that_does_not_exist_or_cannot_be_read_is_a_usage_error_and_prints_nothing
m AL51 'options.paths is missing' src/cli.rs \
  '            o.insert("paths".into(), json!(paths));' '            let _ = paths;' \
  status_all the_json_is_the_status_document_plus_the_additive_fields
m AL52 'the text has no depth bound of a scan in its header' src/status_all.rs \
  '        out += &format!("  depth<={}", o.depth);' '        let _ = o;' \
  status_all the_depth_bounds_the_scan_and_a_cut_is_a_notice
m AL53 'the scan options are there when every PATH is a repository' src/status_all.rs \
  '        scan: scanned.then_some((depth, limit)),' '        scan: Some((depth, limit)),' \
  status_all the_run_of_a_repository_path_has_no_scan_options
m AL54 'a refused gitlink path is not named below the key of its repository' src/status_all.rs \
  '            let shown = if top_key == "." {' '            let shown = if true {' \
  status_all a_refused_gitlink_path_is_reported_below_the_key_of_its_repository
m AL55 'the agent-rules checkout is listed by a scan' src/status_all.rs \
  '            if canon(&dir) == *rules {' '            if false && canon(&dir) == *rules {' \
  status_all a_scan_leaves_out_the_agent_rules_checkout_unless_it_is_named
m AL58 'a scanned directory without a repository is not noticed' src/status_all.rs \
  '        if s.found.is_empty() && !s.beyond {' '        if false && s.found.is_empty() && !s.beyond {' \
  status_all a_scanned_directory_without_a_repository_is_a_notice_and_exit_0
m AL59 'a bare repository is not named as such in the notice' src/status_all.rs \
  '            let bare = a.real.join("HEAD").is_file() && a.real.join("objects").is_dir();' '            let bare = false;' \
  status_all a_scanned_directory_without_a_repository_is_a_notice_and_exit_0
m AL56 'a submodule that is the agent-rules checkout is listed' src/status_all.rs \
  '            if *d > 0 && real == *rules && !explicit.contains(&real) {' '            if false && *d > 0 && real == *rules && !explicit.contains(&real) {' \
  status_all a_scan_leaves_out_the_agent_rules_checkout_unless_it_is_named
m AL57 'a named submodule that is the agent-rules checkout is left out' src/status_all.rs \
  '            if *d > 0 && real == *rules && !explicit.contains(&real) {' '            if *d > 0 && real == *rules {' \
  status_all a_scan_leaves_out_the_agent_rules_checkout_unless_it_is_named

m LR1 'a tail without a visible byte counts as a broken line' src/oplog/write.rs \
  "Ok(last.is_some_and(|b| b != b'\\n'))" "Ok(last.is_none_or(|b| b != b'\\n'))" \
  lib oplog::write::tests::the_tail_check_follows_what_is_visible_not_only_the_size
m LR2 'the tail check reads one chunk only' src/oplog/write.rs \
  '    for _ in 0..16 {
        let n = read_at(&mut chunk, at)?;' '    for _ in 0..1 {
        let n = read_at(&mut chunk, at)?;' \
  lib oplog::write::tests::the_tail_check_follows_what_is_visible_not_only_the_size
m LR3 'the tail check reads the byte at len - 1 only' src/oplog/write.rs \
  '    for _ in 0..16 {
        let n = read_at(&mut chunk, at)?;' '    for _ in 0..1 {
        let n = read_at(&mut chunk[..1], at)?;' \
  lib oplog::write::tests::the_tail_check_follows_what_is_visible_not_only_the_size
m LR4 'append_line takes no lock (FUSE: TMPDIR on the FUSE mount, probabilistic)' src/oplog/write.rs \
  '    let _ = lock_within(&f, LOCK_BUDGET);' '    let _ = &f;' \
  lib oplog::write::tests::parallel_writers_leave_no_empty_line_and_lose_no_line
m LR5 'the plan promises a merge that the merge options of the branch refuse' src/sync/mod.rs \
  '    if !o.apply && action == "merge" {' '    if false && action == "merge" {' \
  sync_pull a_plan_does_not_promise_a_merge_that_the_merge_options_of_the_branch_refuse
m LR6 'the plan refuses a rebase because of the merge options' src/sync/mod.rs \
  '    if !o.apply && action == "merge" {' '    if !o.apply && (action == "merge" || action == "rebase") {' \
  sync_pull a_plan_does_not_promise_a_merge_that_the_merge_options_of_the_branch_refuse
m LR7 'the import check does not see an alias of the crate' tests/it/hostile_repo.rs \
  '        if rest.starts_with("gix as ") || rest == "gix" {' '        if false {' \
  hostile_repo the_import_check_sees_short_names_and_aliases_of_the_opening_functions
m LR8 'the import check flags a module path such as open::Options' tests/it/hostile_repo.rs \
  '        let rest = rest.replace("open::", "").replace("discover::", "");' '        let rest = rest.replace("discover::", "");' \
  hostile_repo the_import_check_sees_short_names_and_aliases_of_the_opening_functions
m LR9 'the import check does not see an imported discover' tests/it/hostile_repo.rs \
  '                    "discover" | "open" | "open_opts" | "ThreadSafeRepository"' '                    "open" | "open_opts" | "ThreadSafeRepository"' \
  hostile_repo the_import_check_sees_short_names_and_aliases_of_the_opening_functions
m LR10 'a source file imports open_opts under a short name' src/status.rs \
  'use gix::bstr::BString;' 'use gix::bstr::BString;
use gix::open_opts as lr10_open;' \
  hostile_repo repositories_are_opened_only_through_repo_open
m LR11 'a relative GIT_CEILING_DIRECTORIES entry is kept' src/repo/discover.rs \
  '.filter(|p| p.is_absolute())' '.filter(|_| true)' \
  lib repo::discover::tests::relative_ceiling_entries_are_ignored
m LR12 'the first read of the changed files of sync fails on an unreadable submodule' src/sync/mod.rs \
  '    let mut dirty = dirty_with_submodules(&repo, gate_dirty);' '    let mut dirty = status::changes(&repo, false)?.dirty_paths;' \
  sync_pull a_submodule_that_cannot_be_read_does_not_stop_the_gate_or_the_run_of_the_superproject
m LR14 'the gate of sync counts an unreadable submodule' src/sync/mod.rs \
  '    let mut gate_dirty = status::changes_opts(&repo, false, true)?.dirty_paths;' '    let mut gate_dirty = status::changes_opts(&repo, false, false)?.dirty_paths;' \
  sync_pull a_submodule_that_cannot_be_read_does_not_stop_the_gate_or_the_run_of_the_superproject
m LR13 'the read after the fetch fails on an unreadable submodule' src/sync/mod.rs \
  '        dirty = dirty_with_submodules(&repo, gate_dirty);' '        dirty = status::changes(&repo, false)?.dirty_paths;' \
  sync_pull a_submodule_that_cannot_be_read_does_not_stop_the_gate_or_the_run_of_the_superproject
m LR15 'a text cut at 300 characters shows no sign of it' src/status.rs \
  "        out.push('\\u{2026}');" "        let _ = &out;" \
  lib status::esc_tests::a_name_cut_at_300_characters_ends_in_an_ellipsis

m OU1 'oplog undo: the first run number is taken without a create-only counter update' src/oplog/undo.rs \
  '            None => PreviousValue::MustNotExist,' '            None => PreviousValue::Any,' \
  oplog_undo a_lost_race_for_the_first_run_takes_the_next_number
m OU2 'oplog undo: a later run number is taken without a compare-and-swap on the counter' src/oplog/undo.rs \
  '            Some(p) => PreviousValue::MustExistAndMatch(Target::Object(p)),' '            Some(p) => PreviousValue::Any,' \
  oplog_undo a_lost_race_for_a_later_run_takes_the_next_number
m OU3 'oplog undo: one try more than asked for a run number' src/oplog/undo.rs \
  '    for attempt in 0..tries {' '    for attempt in 0..tries + 1 {' \
  oplog_undo a_run_number_that_keeps_being_taken_ends_in_an_error
m OU4 'oplog undo: the backup ref is not looked up before the create-only edit' src/oplog/undo.rs \
  '        .is_some()
    {' '        .is_some()
        && false
    {' \
  oplog_undo an_existing_backup_ref_is_never_replaced
m OU5 'oplog undo: retention keeps 9 runs' src/oplog/undo.rs \
  'pub const RETAINED_RUNS: usize = 10;' 'pub const RETAINED_RUNS: usize = 9;' \
  oplog_undo ten_runs_are_all_kept
m OU6 'oplog undo: retention keeps 11 runs' src/oplog/undo.rs \
  'pub const RETAINED_RUNS: usize = 10;' 'pub const RETAINED_RUNS: usize = 11;' \
  oplog_undo the_eleventh_run_prunes_the_oldest_runs_whole_and_never_the_counter_or_the_newest
m OU7 'oplog undo: retention keeps the oldest runs and prunes the newest' src/oplog/undo.rs \
  'runs.iter().rev().take(RETAINED_RUNS)' 'runs.iter().take(RETAINED_RUNS)' \
  oplog_undo the_eleventh_run_prunes_the_oldest_runs_whole_and_never_the_counter_or_the_newest
m OU8 'oplog undo: retention prunes one ref per run only' src/oplog/undo.rs \
  '        if keep.contains(&run) || run == own {' '        if keep.contains(&run) || run == own || name.ends_with("/2") {' \
  oplog_undo the_eleventh_run_prunes_the_oldest_runs_whole_and_never_the_counter_or_the_newest
m OU9 'oplog undo: a name below the backup prefix whose second part is no number is pruned' src/oplog/undo.rs \
  'canonical(k).is_some()) else {' 'true) else {' \
  oplog_undo the_eleventh_run_prunes_the_oldest_runs_whole_and_never_the_counter_or_the_newest
m OU10 'oplog undo: the reflog handle follows core.logAllRefUpdates' src/oplog/undo.rs \
  '    store.write_reflog = gix::refs::store::WriteReflog::Always;' '    store.write_reflog = repo.refs.write_reflog;' \
  oplog_undo the_reflog_entries_are_written_with_log_all_ref_updates_false
m OU11 'oplog undo: the reflog message is not the command' src/oplog/undo.rs \
  'const MESSAGE: &str = "stw oplog undo";' 'const MESSAGE: &str = "undo";' \
  oplog_undo the_reflog_entries_are_written_with_log_all_ref_updates_false
m OU12 'oplog undo: the update of the branch has no expected old value' src/oplog/undo.rs \
  '            PreviousValue::MustExistAndMatch(Target::Object(expect)),' '            PreviousValue::Any,' \
  oplog_undo the_update_is_a_compare_and_swap_and_a_lost_race_keeps_the_backup
m OU13 'oplog undo: a --expect that is not the current value is not refused up front' src/oplog/undo.rs \
  '    if req.expect != cur_s {' '    if false && req.expect != cur_s {' \
  oplog_undo a_wrong_expect_changes_nothing
m OU14 'oplog undo: a refused plan is executed' src/oplog/undo.rs \
  '    if !plan.executable() {' '    if false && !plan.executable() {' \
  oplog_undo a_checked_out_branch_is_refused_with_the_reset_keep_hint
m OU15 'oplog undo: a ref that is no local branch is a refusal, not a usage error' src/oplog/undo.rs \
  '    if !plan.branch {' '    if false && !plan.branch {' \
  oplog_undo a_ref_that_is_no_local_branch_is_a_usage_error
m OU16 'oplog undo: an abbreviated --expect is accepted' src/oplog/undo.rs \
  '    if !is_full_hex(&req.expect, hex_len) {' '    if false && !is_full_hex(&req.expect, hex_len) {' \
  oplog_undo a_wrong_expect_changes_nothing
m OU17 'oplog undo: undo runs without --yes' src/cli.rs \
  '    if !a.yes {' '    if false && !a.yes {' \
  oplog_undo expect_and_yes_and_to_are_required_and_without_yes_nothing_is_written
m OU18 'oplog undo: the backup ref holds the new value, not the current one' src/oplog/undo.rs \
  '    create_backup_with(repo, &backup_ref, expect, &mut || {}).map_err(|e| {' '    create_backup_with(repo, &backup_ref, to, &mut || {}).map_err(|e| {' \
  oplog_undo undo_moves_the_branch_and_writes_backup_counter_and_log_lines
m OU19 'oplog undo: the counter update has no refs.log line' src/oplog/undo.rs \
  '            refname: COUNTER_REF,
            old: prev,' '            refname: "refs/stw/other",
            old: prev,' \
  oplog_undo undo_moves_the_branch_and_writes_backup_counter_and_log_lines
m OU20 'oplog undo: the backup creation has no refs.log line' src/oplog/undo.rs \
  '            refname: &backup_ref,' '            refname: "refs/stw/other",' \
  oplog_undo undo_moves_the_branch_and_writes_backup_counter_and_log_lines
m OU21 'oplog undo: the undo update has no refs.log line' src/oplog/undo.rs \
  '            refname: &plan.reference,
            old: Some(expect),
            new: Some(to),
        },
    );
    let way_back = undo_plan::undo_command(
        &plan.reference,
        &expect.to_string(),
        &to.to_string(),
        false,
        false,' '            refname: "refs/stw/other",
            old: Some(expect),
            new: Some(to),
        },
    );
    let way_back = undo_plan::undo_command(
        &plan.reference,
        &expect.to_string(),
        &to.to_string(),
        false,
        false,' \
  oplog_undo undo_moves_the_branch_and_writes_backup_counter_and_log_lines
m OU22 'oplog undo: the prune has no refs.log lines' src/oplog/undo.rs \
  '                        refname: name,' '                        refname: "refs/stw/other",' \
  oplog_undo the_eleventh_run_prunes_the_oldest_runs_whole_and_never_the_counter_or_the_newest
m OU23 'oplog undo: the next run number is the counter, not counter plus one' src/oplog/undo.rs \
  'let run = counted.max(existing).saturating_add(1);' 'let run = counted.max(existing);' \
  oplog_undo the_counter_is_never_reused_after_the_backups_are_pruned
m OU24 'oplog undo: the way back has --to and --expect swapped' src/oplog/undo.rs \
  '        &expect.to_string(),
        &to.to_string(),
        false,
        false,' '        &to.to_string(),
        &expect.to_string(),
        false,
        false,' \
  oplog_undo json_names_backup_run_and_way_back
m OU25 'oplog undo: a refs.log that is a symbolic link is written through' src/oplog/write.rs \
  '    refuse_link(path)?;' '    let _ = path;' \
  oplog_undo a_refs_log_that_is_a_symbolic_link_does_not_undo_the_undo
m OU26 'oplog undo: undo-plan names the apply command with --to and --expect swapped' src/oplog/undo_plan.rs \
  'format!("stw oplog undo {name} --to {to} --expect {expect}{keep} --yes")' 'format!("stw oplog undo {name} --to {expect} --expect {to}{keep} --yes")' \
  oplog_undo expect_and_yes_and_to_are_required_and_without_yes_nothing_is_written
m OU27 'oplog undo: the run number ignores the runs that exist' src/oplog/undo.rs \
  'let run = counted.max(existing).saturating_add(1);' 'let run = counted.saturating_add(1);' \
  oplog_undo a_deleted_counter_does_not_reuse_a_number_and_the_new_backup_stays
m OU28 'oplog undo: a counter below the highest run hands out a taken number' src/oplog/undo.rs \
  'let run = counted.max(existing).saturating_add(1);' 'let run = counted.saturating_add(1);' \
  oplog_undo a_counter_below_the_highest_run_does_not_reuse_a_number
m OU29 'oplog undo: the prune removes the run it was made for' src/oplog/undo.rs \
  '        if keep.contains(&run) || run == own {' '        if keep.contains(&run) {' \
  oplog_undo the_prune_never_removes_the_run_it_was_made_for
m OU30 'oplog undo: a run name with a leading zero is a run' src/oplog/undo.rs \
  "    if b.is_empty() || b[0] == b'0' || !b.iter().all(u8::is_ascii_digit) {" "    if b.is_empty() || !b.iter().all(u8::is_ascii_digit) {" \
  oplog_undo names_that_are_no_canonical_decimal_are_no_run_and_stay
m OU31 'oplog undo: a run name with a plus sign is a run' src/oplog/undo.rs \
  "    if b.is_empty() || b[0] == b'0' || !b.iter().all(u8::is_ascii_digit) {" "    if b.is_empty() || b[0] == b'0' {" \
  oplog_undo names_that_are_no_canonical_decimal_are_no_run_and_stay
m OU32 'oplog undo: the second part of a backup name is read with u64::parse' src/oplog/undo.rs \
  'canonical(k).is_some()) else {' 'k.parse::<u64>().is_ok()) else {' \
  oplog_undo names_that_are_no_canonical_decimal_are_no_run_and_stay
m OU33 'oplog undo: undo-plan names a ref with shell characters unquoted' src/oplog/undo_plan.rs \
  '                    self.current.as_deref().unwrap_or("?"),
                    true' '                    self.current.as_deref().unwrap_or("?"),
                    false' \
  oplog_undo undo_plan_quotes_a_ref_name_with_shell_characters_and_json_keeps_the_raw_name
m OU34 'oplog undo: the way back of the text output is unquoted' src/oplog/undo.rs \
  '                &self.current,
                true,' '                &self.current,
                false,' \
  oplog_undo undo_quotes_the_way_back_and_json_keeps_the_raw_name
m OU35 'oplog undo: a single quote in a name is not escaped' src/oplog/undo_plan.rs \
  "format!(\"'{}'\", s.replace('\\'', \"'\\\\''\"))" "format!(\"'{}'\", s)" \
  oplog_undo undo_plan_quotes_a_ref_name_with_shell_characters_and_json_keeps_the_raw_name
m OU36 'oplog undo: a plain ref name is quoted too' src/oplog/undo_plan.rs \
  '    let plain = !s.is_empty()' '    let plain = false && !s.is_empty()' \
  oplog_undo a_plain_ref_name_is_not_quoted
m OU37 'oplog undo: a dollar sign counts as plain' src/oplog/undo_plan.rs \
  'b"._/+@:=%,-".contains(&b)' 'b"._/+@:=%,-$".contains(&b)' \
  oplog_undo shell_word_quotes_every_character_a_shell_treats_specially
m OU38 'oplog undo: a backtick counts as plain' src/oplog/undo_plan.rs \
  'b"._/+@:=%,-".contains(&b)' 'b"._/+@:=%,-`".contains(&b)' \
  oplog_undo shell_word_quotes_every_character_a_shell_treats_specially
m OU39 'oplog undo: a semicolon and a pipe count as plain' src/oplog/undo_plan.rs \
  'b"._/+@:=%,-".contains(&b)' 'b"._/+@:=%,-;|".contains(&b)' \
  oplog_undo shell_word_quotes_every_character_a_shell_treats_specially
m OU40 'oplog undo: the JSON apply field is quoted' src/oplog/undo_plan.rs \
  '            self.current.as_deref().unwrap_or("?"),
            false,' '            self.current.as_deref().unwrap_or("?"),
            true,' \
  oplog_undo undo_plan_quotes_a_ref_name_with_shell_characters_and_json_keeps_the_raw_name
m OU41 'oplog undo: the JSON way_back field is quoted' src/oplog/undo.rs \
  '        &to.to_string(),
        false,
        false,' '        &to.to_string(),
        true,
        false,' \
  oplog_undo undo_quotes_the_way_back_and_json_keeps_the_raw_name
m OU42 'oplog undo: the way back of the text output has --to and --expect swapped' src/oplog/undo.rs \
  '                &self.previous,
                &self.current,
                true,' '                &self.current,
                &self.previous,
                true,' \
  oplog_undo undo_moves_the_branch_and_writes_backup_counter_and_log_lines
m OU43 'oplog undo: the backup ref is created without the create-only flag (a ref that appears after the lookup is replaced)' src/oplog/undo.rs \
  '            PreviousValue::MustNotExist,
            value,' '            PreviousValue::Any,
            value,' \
  oplog_undo a_backup_ref_that_appears_between_the_lookup_and_the_edit_is_not_replaced
m OU44 'oplog undo: the command takes a run number once only' src/oplog/undo.rs \
  'pub const COUNTER_TRIES: u32 = 5;' 'pub const COUNTER_TRIES: u32 = 1;' \
  oplog_undo the_command_tries_five_times_for_a_run_number
m OU45 'oplog undo: the command takes 6 tries for a run number' src/oplog/undo.rs \
  'pub const COUNTER_TRIES: u32 = 5;' 'pub const COUNTER_TRIES: u32 = 6;' \
  oplog_undo the_command_gives_up_after_five_lost_races_and_changes_nothing
m OU46 'oplog undo: the blob of the counter has no nonce' src/oplog/undo.rs \
  '.write_blob(format!("{run} {}", nonce()).as_bytes())' '.write_blob(format!("{run}{}", "").as_bytes())' \
  oplog_undo two_first_runs_do_not_share_a_number_even_when_the_rival_wrote_the_same_text

m OH1 'oplog hooks: core.hooksPath does not refuse the install' src/oplog/hooks.rs \
  '    if let Some(h) = &i.hooks_path {
        let _ = writeln!(
            t,
            "refused' '    if let Some(h) = i.hooks_path.as_ref().filter(|_| false) {
        let _ = writeln!(
            t,
            "refused' \
  oplog_hooks core_hookspath_from_the_repository_config_refuses
m OH2 'oplog hooks: an empty core.hooksPath counts as unset' src/oplog/hooks.rs \
  '            found = Some(HooksPath { value, origin });' '            if !value.is_empty() {
                found = Some(HooksPath { value, origin });
            }' \
  oplog_hooks an_empty_core_hookspath_counts_as_set
m OH3 'oplog hooks: GIT_CONFIG_PARAMETERS is not read' src/oplog/hooks.rs \
  'std::env::var_os("GIT_CONFIG_PARAMETERS")' 'std::env::var_os("GIT_CONFIG_PARAMETERS_")' \
  oplog_hooks core_hookspath_from_the_environment_refuses
m OH4 'oplog hooks: the first value in GIT_CONFIG_PARAMETERS wins' src/oplog/hooks.rs \
  '            out = Some(v.to_string());' '            out = out.or(Some(v.to_string()));' \
  lib oplog::hooks::tests::the_last_value_wins_and_other_keys_are_ignored
m OH5 'oplog hooks: the key in GIT_CONFIG_PARAMETERS is case-sensitive' src/oplog/hooks.rs \
  'k.eq_ignore_ascii_case(key)' 'k == key' \
  lib oplog::hooks::tests::parameters_in_the_old_and_the_new_form
m OH6 'oplog hooks: the value in GIT_CONFIG_PARAMETERS is split at the last equals sign' src/oplog/hooks.rs \
  'w.split_once(' 'w.rsplit_once(' \
  lib oplog::hooks::tests::a_quote_and_a_space_inside_a_value_stay_in_the_value
m OH7 'oplog hooks: the config key read is not core.hooksPath' src/oplog/hooks.rs \
  'value_implicit("hooksPath")' 'value_implicit("hooksPathX")' \
  oplog_hooks core_hookspath_from_the_repository_config_refuses
m OH8 'oplog hooks: the hook is put in place by rename (replaces an existing file)' src/oplog/hooks.rs \
  '    let linked = fs::hard_link(&tmp, &target);' '    let linked = fs::rename(&tmp, &target);' \
  lib oplog::hooks::tests::write_hook_never_replaces_an_existing_file_and_leaves_no_temporary_file
m OH9 'oplog hooks: the temporary file stays next to the hook' src/oplog/hooks.rs \
  '    let linked = fs::hard_link(&tmp, &target);
    let _ = fs::remove_file(&tmp);' '    let linked = fs::hard_link(&tmp, &target);' \
  oplog_hooks install_yes_writes_both_hooks_executable_with_their_header_and_an_install_event
m OH10 'oplog hooks: the hook is not executable' src/oplog/hooks.rs \
  'fs::set_permissions(&tmp, fs::Permissions::from_mode(0o755))' 'fs::set_permissions(&tmp, fs::Permissions::from_mode(0o644))' \
  oplog_hooks install_yes_writes_both_hooks_executable_with_their_header_and_an_install_event
m OH11 'oplog hooks: a foreign hook is not refused' src/oplog/hooks.rs \
  '        if i.states[n] == State::Foreign {
            any_foreign = true;' '        if false && i.states[n] == State::Foreign {
            any_foreign = true;' \
  oplog_hooks a_foreign_hook_is_never_overwritten_and_then_nothing_at_all_is_installed
m OH12 'oplog hooks: a refusal for one hook still installs the other' src/oplog/hooks.rs \
  '    if any_foreign {' '    if false && any_foreign {' \
  oplog_hooks a_foreign_hook_is_never_overwritten_and_then_nothing_at_all_is_installed
m OH13 'oplog hooks: the install record has another word' src/oplog/hooks.rs \
  'append_event(&dir, HOOKS[n].events, "install")' 'append_event(&dir, HOOKS[n].events, "installed")' \
  oplog_hooks install_yes_writes_both_hooks_executable_with_their_header_and_an_install_event
m OH14 'oplog hooks: only the first hook gets an install record' src/oplog/hooks.rs \
  '            for &n in &done {
                if let Err(why) = append_event(' '            for &n in done.iter().take(1) {
                if let Err(why) = append_event(' \
  oplog_hooks install_yes_writes_both_hooks_executable_with_their_header_and_an_install_event
m OH15 'oplog hooks: the log directory is not private' src/oplog/hooks.rs \
  '                    .mode(0o700)' '                    .mode(0o755)' \
  oplog_hooks install_yes_writes_both_hooks_executable_with_their_header_and_an_install_event
m OH16 'oplog hooks: the events file is readable by everyone' src/oplog/hooks.rs \
  '        .mode(0o600)' '        .mode(0o644)' \
  oplog_hooks install_json_says_what_was_done_and_the_origin_of_hookspath_is_named
m OH17 'oplog hooks: a symbolic stw directory is not named as such' src/oplog/hooks.rs \
  '            Ok(m) if m.file_type().is_symlink() => {' '            Ok(m) if false && m.file_type().is_symlink() => {' \
  oplog_hooks a_log_directory_that_is_a_symbolic_link_is_not_written_through
m OH18 'oplog hooks: a symbolic hooks directory is not detected' src/oplog/hooks.rs \
  'fs::symlink_metadata(p).is_ok_and(|m| m.file_type().is_symlink())' 'fs::symlink_metadata(p).is_ok_and(|m| false && m.file_type().is_symlink())' \
  oplog_hooks a_hooks_directory_that_is_a_symbolic_link_is_not_written_through
m OH19 'oplog hooks: install writes through a symbolic hooks directory' src/oplog/hooks.rs \
  '    if i.dir_is_link {
        let _ = writeln!(
            t,
            "refused  {} is a symbolic link (the directory may be shared with other repositories). Nothing was written;' '    if false && i.dir_is_link {
        let _ = writeln!(
            t,
            "refused  {} is a symbolic link (the directory may be shared with other repositories). Nothing was written;' \
  oplog_hooks a_hooks_directory_that_is_a_symbolic_link_is_not_written_through
m OH20 'oplog hooks: uninstall removes through a symbolic hooks directory' src/oplog/hooks.rs \
  '    if i.dir_is_link {
        let _ = writeln!(
            t,
            "refused  {} is a symbolic link (the directory may be shared with other repositories). Nothing was removed;' '    if false && i.dir_is_link {
        let _ = writeln!(
            t,
            "refused  {} is a symbolic link (the directory may be shared with other repositories). Nothing was removed;' \
  oplog_hooks a_hooks_directory_that_is_a_symbolic_link_is_not_written_through
m OH21 'oplog hooks: a link counts as our file' src/oplog/hooks.rs \
  '    if !md.file_type().is_file() {
        return State::Foreign;' '    if md.file_type().is_dir() {
        return State::Foreign;' \
  oplog_hooks a_foreign_hook_that_is_a_link_is_foreign_too
m OH22 'oplog hooks: a header that only starts like ours counts as ours' src/oplog/hooks.rs \
  '    if second == Some(header.as_bytes()) {' '    if second.is_some_and(|l| l.starts_with(header.as_bytes())) {' \
  lib oplog::hooks::tests::our_file_needs_the_exact_header_on_line_two
m OH23 'oplog hooks: the header is read from line 1' src/oplog/hooks.rs \
  '.nth(1);
    if second' '.nth(0);
    if second' \
  lib oplog::hooks::tests::our_file_needs_the_exact_header_on_line_two
m OH24 'oplog hooks: any regular file counts as ours' src/oplog/hooks.rs \
  '    } else {
        State::Foreign
    }
}' '    } else {
        State::Ours { executable: true }
    }
}' \
  oplog_hooks uninstall_leaves_a_foreign_hook_and_a_look_alike_header
m OH25 'oplog hooks: uninstall without --yes removes' src/oplog/hooks.rs \
  '    if !yes {
        let _ = writeln!(t, "not executed: --yes is missing");
        j["result"] = json!("not executed: --yes is missing");
        return Ok(Outcome {
            text: t,
            json: j,
            exit: exit::FAILED,
        });
    }
    let dir = oplog_dir' '    if false && !yes {
        let _ = writeln!(t, "not executed: --yes is missing");
        j["result"] = json!("not executed: --yes is missing");
        return Ok(Outcome {
            text: t,
            json: j,
            exit: exit::FAILED,
        });
    }
    let dir = oplog_dir' \
  oplog_hooks uninstall_removes_only_our_files_and_leaves_the_logs
m OH26 'oplog hooks: install without --yes writes' src/oplog/hooks.rs \
  '    if !yes {
        let _ = writeln!(t, "not executed: --yes is missing");
        j["result"] = json!("not executed: --yes is missing");
        return Ok(Outcome {
            text: t,
            json: j,
            exit: exit::FAILED,
        });
    }
    let mut done' '    if false && !yes {
        let _ = writeln!(t, "not executed: --yes is missing");
        j["result"] = json!("not executed: --yes is missing");
        return Ok(Outcome {
            text: t,
            json: j,
            exit: exit::FAILED,
        });
    }
    let mut done' \
  oplog_hooks install_without_yes_prints_the_plan_and_writes_nothing
m OH27 'oplog hooks: the uninstall record has the install word' src/oplog/hooks.rs \
  'append_event(&dir, HOOKS[n].events, "uninstall")' 'append_event(&dir, HOOKS[n].events, "install")' \
  oplog_hooks uninstall_removes_only_our_files_and_leaves_the_logs
m OH28 'oplog hooks: the hooks directory is the git dir of the worktree, not the common dir' src/oplog/hooks.rs \
  '    let hooks_dir = common.join("hooks");' '    let hooks_dir = ctx.repo.as_ref().map_or(common.clone(), |r| r.git_dir().to_path_buf()).join("hooks");' \
  oplog_hooks a_linked_worktree_installs_into_the_common_hooks_and_logs_there
m OH29 'oplog hooks: status does not find submodule git directories' src/oplog/hooks.rs \
  'if p.join("HEAD").is_file() {' 'if p.join("HEADX").is_file() {' \
  oplog_hooks a_submodule_has_its_own_hooks_directory
m OH30 'oplog hooks: status counts the linked worktrees only' src/oplog/hooks.rs \
  '            linked + 1,
            show(&i.hooks_dir),' '            linked,
            show(&i.hooks_dir),' \
  oplog_hooks a_linked_worktree_installs_into_the_common_hooks_and_logs_there
m OH31 'oplog hooks: status calls every hook of ours executable' src/oplog/hooks.rs \
  '            executable: md.permissions().mode() & 0o111 != 0,' '            executable: true,' \
  oplog_hooks status_reports_absent_ours_foreign_and_not_executable
m OH32 'oplog hooks: print accepts an unknown name' src/oplog/hooks.rs \
  'hook_text(name).map(str::to_string)' 'hook_text(name).or(Some(HOOKS[0].text)).map(str::to_string)' \
  oplog_hooks print_writes_the_hook_text_and_nothing_else
m OH33 'oplog hooks: the header constant of post-rewrite differs from the script' src/oplog/hooks.rs \
  '        header: "# stw-oplog-hook v1",' '        header: "# stw-oplog-hook v2",' \
  oplog_hooks install_twice_changes_nothing_the_second_time
m OH34 'oplog hooks: post-rewrite installs the other script' src/oplog/hooks.rs \
  'include_str!("hooks/post-rewrite.sh")' 'include_str!("hooks/reference-transaction.sh")' \
  oplog_hooks install_yes_writes_both_hooks_executable_with_their_header_and_an_install_event
m OH35 'oplog hooks: post-rewrite records its install in the other events file' src/oplog/hooks.rs \
  '        events: "events.log",' '        events: "events-refhook.log",' \
  oplog_hooks install_yes_writes_both_hooks_executable_with_their_header_and_an_install_event
m OH36 'oplog hooks: the plan does not say that reference-transaction runs on every ref change' src/oplog/hooks.rs \
  '    if todo.contains(&1) {' '    if false && todo.contains(&1) {' \
  oplog_hooks install_without_yes_prints_the_plan_and_writes_nothing
m OH37 'oplog hooks: install tries to write a hook that is already ours' src/oplog/hooks.rs \
  '(0..2).filter(|&n| i.states[n] == State::Absent)' '(0..2).filter(|&n| i.states[n] != State::Foreign)' \
  oplog_hooks install_adds_only_the_missing_hook_next_to_ours
m OH38 'oplog hooks: a missing hooks directory is not created' src/oplog/hooks.rs \
  '    fs::create_dir_all(dir).map_err(|e| format!("cannot create {}: {e}", show(dir)))?;' '' \
  oplog_hooks a_missing_hooks_directory_is_created
m OH39 'oplog hooks: an events file that is a link is written through' src/oplog/hooks.rs \
  '        if !m.file_type().is_file() {
            return Err(format!("{} is not a regular file"' '        if false && !m.file_type().is_file() {
            return Err(format!("{} is not a regular file"' \
  oplog_hooks an_events_file_that_is_a_link_or_a_log_directory_that_is_a_file_is_not_written_through
m OH40 'oplog hooks: a log directory that is a file is not named' src/oplog/hooks.rs \
  '            Ok(m) if !m.is_dir() =>' '            Ok(m) if false && !m.is_dir() =>' \
  oplog_hooks an_events_file_that_is_a_link_or_a_log_directory_that_is_a_file_is_not_written_through
m OH41 'oplog hooks: the JSON of a first install says executed false' src/oplog/hooks.rs \
  '    j["executed"] = json!(true);' '    j["executed"] = json!(false);' \
  oplog_hooks install_json_says_what_was_done_and_the_origin_of_hookspath_is_named
m OH42 'oplog hooks: the chain proposal of reference-transaction is the one of post-rewrite' src/oplog/hooks.rs \
  '    let call = if def.name == "reference-transaction" {' '    let call = if def.name == "no-such-hook" {' \
  oplog_hooks the_chain_proposal_differs_per_hook
m OH43 'oplog hooks: the origin of an environment value is not named' src/oplog/hooks.rs \
  '"environment GIT_CONFIG_COUNT".to_string()' '"environment".to_string()' \
  oplog_hooks install_json_says_what_was_done_and_the_origin_of_hookspath_is_named
m OH44 'oplog hooks: the origin of GIT_CONFIG_PARAMETERS is not named' src/oplog/hooks.rs \
  'origin: "environment GIT_CONFIG_PARAMETERS".into(),' 'origin: "environment".into(),' \
  oplog_hooks install_json_says_what_was_done_and_the_origin_of_hookspath_is_named
m OH45 'oplog hooks: the config file of a value is not named' src/oplog/hooks.rs \
  '(Some(p), _) => format!("file {}", p.display()),' '(Some(_), _) => "file".to_string(),' \
  oplog_hooks install_json_says_what_was_done_and_the_origin_of_hookspath_is_named
m OH46 'oplog hooks: a failed removal exits 0' src/oplog/hooks.rs \
  '        exit: if failed { exit::FAILED } else { exit::OK },' '        exit: exit::OK,' \
  oplog_hooks a_hooks_directory_that_cannot_be_written_is_reported_and_nothing_changes

m OH47 'oplog hooks: the common dir of a linked worktree is shown as <git-dir>/worktrees/<id>/../..' src/oplog/hooks.rs \
  '    let common = fs::canonicalize(&ctx.common).unwrap_or_else(|_| ctx.common.clone());' '    let common = ctx.common.clone();' \
  oplog_hooks a_linked_worktree_installs_into_the_common_hooks_and_logs_there
m OH48 'oplog hooks: the script of a hook carries another header than its constant' src/oplog/hooks.rs \
  '        header: "# stw-oplog-refhook v1",' '        header: "# stw-oplog-refhook v2",' \
  lib oplog::hooks::tests::each_script_carries_its_own_header_on_line_two

m OK1 'oplog undo --keep: the branch of this worktree is not recognized (a checked-out branch is always refused)' src/oplog/undo_plan.rs \
  'if req.keep && here.is_some() && std::fs::canonicalize(dir).ok() == here {' 'if false && req.keep && here.is_some() && std::fs::canonicalize(dir).ok() == here {' \
  oplog_undo_keep keep_moves_branch_index_and_worktree_and_writes_backup_and_log_lines
m OK2 'oplog undo --keep: every worktree counts as this one (another worktree holding the branch is not refused)' src/oplog/undo_plan.rs \
  'std::fs::canonicalize(dir).ok() == here {' 'std::fs::canonicalize(dir).ok() == here || true {' \
  oplog_undo_keep a_branch_checked_out_in_another_worktree_is_refused
m OK3 'oplog undo --keep: a second worktree that also holds the branch is not refused' src/oplog/undo_plan.rs \
  '        other_holds = true;
' '        other_holds = false;
        if req.keep && here.is_some() { continue; }
' \
  oplog_undo_keep a_branch_that_a_second_worktree_also_holds_is_refused
m OK4 'oplog undo --keep: a branch that is not checked out here is not refused' src/oplog/undo_plan.rs \
  '        if !other_holds {
            let why' '        if false {
            let why' \
  oplog_undo_keep a_branch_that_is_not_checked_out_here_is_refused_with_keep
m OK5 'oplog undo --keep: a detached HEAD is not named as the reason' src/oplog/undo_plan.rs \
  'Ok(None) => "HEAD is detached".to_string(),' 'Ok(None) => "HEAD has no branch".to_string(),' \
  oplog_undo_keep a_detached_head_is_refused_with_keep
m OK6 'oplog undo --keep: a sparse checkout is not refused' src/oplog/undo_plan.rs \
  '.boolean("core.sparseCheckout")' '.boolean("core.sparseCheckoutX")' \
  oplog_undo_keep core_sparse_checkout_refuses
m OK7 'oplog undo --keep: unmerged paths are not refused' src/oplog/undo_plan.rs \
  '            if c.conflicts > 0 {' '            if false && c.conflicts > 0 {' \
  oplog_undo_keep a_merge_in_progress_and_unmerged_paths_refuse
m OK8 'oplog undo --keep: staged changes are not refused' src/oplog/undo_plan.rs \
  '            if c.staged > 0 {' '            if false && c.staged > 0 {' \
  oplog_undo_keep a_staged_change_refuses_and_nothing_is_written
m OK9 'oplog undo --keep: unstaged changes are not refused' src/oplog/undo_plan.rs \
  '            if c.unstaged > 0 {' '            if false && c.unstaged > 0 {' \
  oplog_undo_keep an_unstaged_change_refuses_and_nothing_is_written
m OK10 'oplog undo --keep: untracked files count as changes' src/oplog/undo_plan.rs \
  '    match crate::status::changes_opts(repo, false, true) {' '    match crate::status::changes_opts(repo, true, true).map(|mut c| { c.unstaged += c.untracked; c }) {' \
  oplog_undo_keep untracked_files_do_not_refuse_and_stay
m OK11 'oplog undo --keep: the cleanliness checks are not run' src/oplog/undo_plan.rs \
  '    if req.keep {
        keep_checks(' '    if false && req.keep {
        keep_checks(' \
  oplog_undo_keep an_unstaged_change_refuses_and_nothing_is_written
m OK12 'oplog undo --keep: the gitlink check is not run' src/oplog/undo_plan.rs \
  '    if req.keep && p.refused == 0 {' '    if false && req.keep && p.refused == 0 {' \
  oplog_undo_keep a_gitlink_that_differs_between_the_tips_refuses
m OK13 'oplog undo --keep: an added or removed gitlink is not seen' src/oplog/undo_plan.rs \
  '        } if entry_mode.is_commit() => Some(location.clone()),' '        } if false && entry_mode.is_commit() => Some(location.clone()),' \
  oplog_undo_keep a_gitlink_that_differs_between_the_tips_refuses
m OK14 'oplog undo --keep: a gitlink that points elsewhere is not seen' src/oplog/undo_plan.rs \
  '} if previous_entry_mode.is_commit() || entry_mode.is_commit() => Some(location.clone()),' '} if false && (previous_entry_mode.is_commit() || entry_mode.is_commit()) => Some(location.clone()),' \
  oplog_undo_keep a_gitlink_that_points_elsewhere_refuses
m OK15 'oplog undo --keep: a git that cannot be asked does not refuse' src/oplog/undo_plan.rs \
  '.map_err(|e| format!("--keep needs a usable git: {e}"))?;' '.map_err(|e| format!("--keep needs a usable git: {e}")).unwrap_or(crate::git_runner::Version(9, 0, 0));' \
  oplog_undo_keep a_git_whose_version_cannot_be_read_refuses_before_any_ref_is_written
m OK16 'oplog undo --keep: an old git does not refuse before the backup' src/oplog/undo_plan.rs \
  '    if found < needed {' '    if false && found < needed {' \
  oplog_undo_keep a_git_older_than_reset_keep_needs_refuses_before_any_ref_is_written
m OK17 'oplog undo --keep: a 64-digit target is cut to 40 digits' src/oplog/undo_plan.rs \
  'let target = Hex40::parse(to).ok_or_else(' 'let target = Hex40::parse(&to[..40.min(to.len())]).ok_or_else(' \
  oplog_undo_keep a_target_that_is_not_40_hex_is_refused_before_git_is_asked
m OK18 'oplog undo --keep: the branch value is not read again before git runs' src/oplog/undo.rs \
  '    if !holds(expect).map_err(stays)? {' '    if false && !holds(expect).map_err(stays)? {' \
  oplog_undo_keep a_branch_that_moves_after_the_checks_is_not_reset_and_the_backup_stays
m OK19 'oplog undo --keep: HEAD is not read again before git runs' src/oplog/undo.rs \
  '    if !head_on_it {' '    if false && !head_on_it {' \
  oplog_undo_keep a_head_that_leaves_the_branch_after_the_checks_is_not_reset
m OK20 'oplog undo --keep: a failed git is not an error' src/oplog/undo.rs \
  'if let Err(e) = git_runner::shared().run(&workdir, Op::ResetKeep { target }) {' 'if let Err(e) = git_runner::shared().run(&workdir, Op::ResetKeep { target }).map(|_| ()).or(Ok::<(), GitError>(())) {' \
  oplog_undo_keep a_filter_driver_in_the_config_stops_the_runner_and_the_branch_stays
m OK21 'oplog undo --keep: the op log line of the branch has no new value' src/oplog/undo.rs \
  '            new: Some(to),
        },
    );
    let way_back = undo_plan::undo_command(
        &plan.reference,
        &expect.to_string(),
        &to.to_string(),
        false,
        true,' '            new: None,
        },
    );
    let way_back = undo_plan::undo_command(
        &plan.reference,
        &expect.to_string(),
        &to.to_string(),
        false,
        true,' \
  oplog_undo_keep keep_moves_branch_index_and_worktree_and_writes_backup_and_log_lines
m OK22 'oplog undo --keep: the way back has no --keep' src/oplog/undo.rs \
  '        &to.to_string(),
        false,
        true,
    );' '        &to.to_string(),
        false,
        false,
    );' \
  oplog_undo_keep keep_json_says_keep_via_git_and_the_way_back_has_keep
m OK23 'oplog undo --keep: the result says keep false' src/oplog/undo.rs \
  '        way_back,
        keep: true,' '        way_back,
        keep: false,' \
  oplog_undo_keep keep_json_says_keep_via_git_and_the_way_back_has_keep
m OK24 'oplog undo --keep: the JSON has no via' src/oplog/undo.rs \
  '"via": self.keep.then_some(git_runner::VIA),' '"via": None::<&str>,' \
  oplog_undo_keep keep_json_says_keep_via_git_and_the_way_back_has_keep
m OK25 'oplog undo --keep: the text result claims only the ref moved' src/oplog/undo.rs \
  '        if self.keep {
            b.push_str("Result: done. The branch,' '        if false {
            b.push_str("Result: done. The branch,' \
  oplog_undo_keep keep_moves_branch_index_and_worktree_and_writes_backup_and_log_lines
m OK26 'oplog undo --keep: the text apply command of the plan has no --keep' src/oplog/undo_plan.rs \
  '                    true,
                    self.keep
' '                    true,
                    false
' \
  oplog_undo_keep without_yes_keep_is_a_usage_error_and_undo_plan_keep_names_the_apply_command
m OK27 'oplog undo --keep: the JSON apply command of the plan has no --keep' src/oplog/undo_plan.rs \
  '            false,
            self.keep,
        )' '            false,
            false,
        )' \
  oplog_undo_keep without_yes_keep_is_a_usage_error_and_undo_plan_keep_names_the_apply_command
m OK28 'oplog undo --keep: the plan JSON has keep false' src/oplog/undo_plan.rs \
  '"keep": self.keep,' '"keep": false,' \
  oplog_undo_keep without_yes_keep_is_a_usage_error_and_undo_plan_keep_names_the_apply_command
m OK29 'oplog undo --keep: the plain refusal does not name --keep' src/oplog/undo_plan.rs \
  'add --keep (clean index and work tree only) or use' 'use' \
  oplog_undo_keep without_keep_a_checked_out_branch_is_still_refused_and_the_text_names_keep
m OK30 'oplog undo --keep: undo-plan ignores --keep' src/cli.rs \
  '            steps: a.steps,
            keep: a.keep,' '            steps: a.steps,
            keep: false,' \
  oplog_undo_keep without_yes_keep_is_a_usage_error_and_undo_plan_keep_names_the_apply_command
m OK31 'oplog undo --keep: undo ignores --keep' src/cli.rs \
  '            expect: a.expect,
            keep: a.keep,' '            expect: a.expect,
            keep: false,' \
  oplog_undo_keep keep_moves_branch_index_and_worktree_and_writes_backup_and_log_lines
m OK32 'oplog undo --keep: the plain undo JSON says keep true' src/oplog/undo.rs \
  '        way_back,
        keep: false,' '        way_back,
        keep: true,' \
  oplog_undo_keep a_plain_undo_json_says_keep_false
m OK33 'oplog undo --keep: git resets to the current value instead of the target' src/oplog/undo.rs \
  'git_runner::shared().run(&workdir, Op::ResetKeep { target })' 'git_runner::shared().run(&workdir, Op::ResetKeep { target: Hex40::parse(&expect.to_string()).unwrap() })' \
  oplog_undo_keep keep_moves_branch_index_and_worktree_and_writes_backup_and_log_lines

m OK34 'oplog undo --keep: a file in the way of an added path is not seen' src/oplog/undo_plan.rs \
  'if !entry_mode.is_tree() && in_the_way(workdir, location.as_ref(), &deleted) {' 'if false && in_the_way(workdir, location.as_ref(), &deleted) {' \
  oplog_undo_keep an_untracked_file_in_the_way_refuses_before_the_backup
m OK35 'oplog undo --keep: a file where a directory is needed is not seen' src/oplog/undo_plan.rs \
  '.is_ok_and(|m| !m.is_dir() && !deleted.contains(dir))' '.is_ok_and(|_| false)' \
  oplog_undo_keep a_file_where_the_target_needs_a_directory_refuses
m OK36 'oplog undo --keep: an existing entry at the added path is not seen' src/oplog/undo_plan.rs \
  'if at(rel).symlink_metadata().is_ok() {' 'if false {' \
  oplog_undo_keep an_ignored_file_in_the_way_refuses_before_the_backup
m OK37 'oplog undo --keep: a file the target deletes itself counts as in the way' src/oplog/undo_plan.rs \
  '!m.is_dir() && !deleted.contains(dir)' '!m.is_dir()' \
  oplog_undo_keep a_tracked_file_that_the_target_turns_into_a_directory_is_not_in_the_way
m OK38 'oplog undo --keep: a new directory is looked at instead of its entries' src/oplog/undo_plan.rs \
  'if !entry_mode.is_tree() && in_the_way(workdir, location.as_ref(), &deleted) {' 'if entry_mode.is_tree() && in_the_way(workdir, location.as_ref(), &deleted) {' \
  oplog_undo_keep an_ignored_file_in_the_way_refuses_before_the_backup
m OK39 'oplog undo --keep: intent-to-add entries are not counted' src/oplog/undo_plan.rs \
  'count(Flags::INTENT_TO_ADD),' 'count(Flags::empty()),' \
  oplog_undo_keep an_intent_to_add_entry_refuses
m OK40 'oplog undo --keep: assume-unchanged entries are not counted' src/oplog/undo_plan.rs \
  'count(Flags::ASSUME_VALID | Flags::SKIP_WORKTREE),' 'count(Flags::SKIP_WORKTREE),' \
  oplog_undo_keep assume_unchanged_and_skip_worktree_entries_refuse
m OK41 'oplog undo --keep: skip-worktree entries are not counted' src/oplog/undo_plan.rs \
  'count(Flags::ASSUME_VALID | Flags::SKIP_WORKTREE),' 'count(Flags::ASSUME_VALID),' \
  oplog_undo_keep assume_unchanged_and_skip_worktree_entries_refuse
m OK42 'oplog undo --keep: a repository without a work tree is not named' src/oplog/undo_plan.rs \
  '    if repo.workdir().is_none() {' '    if false && repo.workdir().is_none() {' \
  oplog_undo_keep a_repository_without_a_work_tree_says_so
m OK43 'oplog undo-plan --keep: the git version is not checked in the plan' src/oplog/undo_plan.rs \
  'if let Err(why) = keep_git_check(&alt.to_string()) {' 'if let Err(why) = keep_git_check(&alt.to_string()).or_else(|_| Hex40::parse(&alt.to_string()).ok_or(String::new())) {' \
  oplog_undo_keep undo_plan_keep_checks_the_git_version_like_undo
m OK44 'oplog undo --keep: git 2.36.0 itself is refused' src/oplog/undo_plan.rs \
  'if found < needed {' 'if found <= needed {' \
  oplog_undo_keep git_2_36_0_is_enough_and_2_35_9_is_not
m OK45 'oplog undo --keep: a git killed by a signal is reported as not having changed anything' src/oplog/undo.rs \
  '| GitError::Exit { code: None, .. } => Error::repository(format!(' '| GitError::Exit { code: Some(255), .. } => Error::repository(format!(' \
  oplog_undo_keep a_git_that_dies_during_the_reset_is_reported_as_possibly_changed
m OK46 'oplog undo --keep: the path of a registered worktree is not made canonical' src/oplog/undo_plan.rs \
  'std::fs::canonicalize(dir).ok() == here {' 'Some(dir.clone()) == here {' \
  oplog_undo_keep a_worktree_registered_through_a_symbolic_link_is_still_this_one
m OK47 'oplog undo --keep: the path of this worktree is not made canonical' src/oplog/undo_plan.rs \
  'let here = repo.workdir().and_then(|d| std::fs::canonicalize(d).ok());' 'let here = repo.workdir().map(|d| d.to_path_buf());' \
  oplog_undo_keep a_worktree_opened_through_a_symbolic_link_is_still_this_one
m OK48 'oplog undo --keep: a file that becomes a gitlink is not seen' src/oplog/undo_plan.rs \
  '} if previous_entry_mode.is_commit() || entry_mode.is_commit() => Some(location.clone()),' '} if previous_entry_mode.is_commit() => Some(location.clone()),' \
  oplog_undo_keep a_file_that_becomes_a_gitlink_and_back_refuses
m OK49 'oplog undo --keep: a gitlink that becomes a file is not seen' src/oplog/undo_plan.rs \
  '} if previous_entry_mode.is_commit() || entry_mode.is_commit() => Some(location.clone()),' '} if entry_mode.is_commit() => Some(location.clone()),' \
  oplog_undo_keep a_file_that_becomes_a_gitlink_and_back_refuses
m OK50 'oplog undo --keep: changed submodule contents count as dirty' src/oplog/undo_plan.rs \
  '    match crate::status::changes_opts(repo, false, true) {' '    match crate::status::changes_opts(repo, false, false) {' \
  oplog_undo_keep changed_submodule_contents_do_not_count_as_dirty

m HH1 'status hint: every hook counts as missing' src/oplog/hooks.rs \
  '.filter(|(_, s)| !matches!(s, State::Ours { .. }))' '.filter(|(_, _)| true)' \
  status_hook_hint both_hooks_ours_show_nothing
m HH2 'status hint: core.hooksPath does not block' src/oplog/hooks.rs \
  'let reason = if hooks_path(repo).is_some() {' 'let reason = if false && hooks_path(repo).is_some() {' \
  status_hook_hint core_hooks_path_gives_no_install_advice_even_with_our_hooks_in_the_git_dir
m HH3 'status hint: a hooks directory that is a link does not block' src/oplog/hooks.rs \
  '} else if link {' '} else if false && link {' \
  status_hook_hint a_hooks_directory_that_is_a_link_is_blocked
m HH4 'status hint: a foreign hook does not block' src/oplog/hooks.rs \
  '} else if states.contains(&State::Foreign) {' '} else if false {' \
  status_hook_hint a_foreign_hook_gives_no_install_advice
m HH5 'status hint: both hooks ours are reported as missing' src/oplog/hooks.rs \
  '(None, true) => "installed",' '(None, true) => "missing",' \
  status_hook_hint both_hooks_ours_show_nothing
m HH6 'status hint: a blocked repository gets the install advice' src/status.rs \
  '"op log hooks: not active, install is refused here ({why}); see: stw oplog hooks status"' '"op log hooks: not active, install is refused here ({why}); install with: stw oplog hooks install"' \
  status_hook_hint a_foreign_hook_gives_no_install_advice
m HH7 'status hint: the text line is never printed' src/status.rs \
  'if let Some(l) = s.oplog_hooks.as_ref().and_then(hooks_line) {' 'if let Some(l) = s.oplog_hooks.as_ref().and_then(hooks_line).filter(|_| false) {' \
  status_hook_hint missing_hooks_give_one_line_with_the_install_command
m HH8 'status hint: a repository that cannot be read reports installed hooks' src/status.rs \
  '"oplog_hooks": s.oplog_hooks.as_ref().map_or(Value::Null, hooks_json),' '"oplog_hooks": s.oplog_hooks.as_ref().map_or(json!({"state": "installed", "reason": null, "missing": []}), hooks_json),' \
  status_hook_hint a_repository_that_cannot_be_read_has_no_hint_and_a_null_field
m HH9 'status hint: a foreign hook is not listed as missing' src/oplog/hooks.rs \
  '.filter(|(_, s)| !matches!(s, State::Ours { .. }))' '.filter(|(_, s)| matches!(s, State::Absent))' \
  status_hook_hint a_foreign_hook_gives_no_install_advice
m HH10 'status hint: the reason word of a foreign hook changes' src/oplog/hooks.rs \
  'Blocked::ForeignHook => "foreign-hook",' 'Blocked::ForeignHook => "foreign",' \
  status_hook_hint a_foreign_hook_gives_no_install_advice
m HH11 'status hint: the missing hooks are not listed by name' src/status.rs \
  'h.missing.join(", ")' 'h.missing.len().to_string()' \
  status_hook_hint one_hook_ours_names_only_the_missing_one

# stw status: the CI of the default branch, ci-main (src/status_ci.rs, tests/it/status_ci_main.rs, status_ci_main_text.rs)
m CM1 'a list query already in the path gets a second ?' src/github/reads.rs \
  $'let sep = if path.contains(\'?\') { \'&\' } else { \'?\' };' $'let sep = \'?\';' \
  github_reads workflow_runs_of_a_commit_are_read_across_pages_with_the_commit_in_every_request
m CM2 'workflow runs are read from the check_runs key' src/github/reads.rs \
  $'v["workflow_runs"].as_array()' $'v["check_runs"].as_array()' \
  github_reads workflow_runs_of_a_commit_are_read_across_pages_with_the_commit_in_every_request
m CM3 'a workflow run without a status is accepted' src/github/reads.rs \
  $'        || !v["status"].is_string()\n' $'' \
  github_reads workflow_run_text_is_escaped_and_a_malformed_entry_makes_the_list_partial
m CM4 'a default branch with .. goes into a path' src/github/reads.rs \
  $'        && !b.ends_with(\'/\')\n        && !b.contains("..")\n' $'        && !b.ends_with(\'/\')\n' \
  github_reads a_default_branch_that_cannot_go_into_a_path_or_a_head_that_is_no_commit_id_is_an_error
m CM5 'a default branch with // goes into a path' src/github/reads.rs \
  $'        && !b.contains("//")\n        && b.bytes()\n            .all(|c| c.is_ascii_alphanumeric()' $'        && b.bytes()\n            .all(|c| c.is_ascii_alphanumeric()' \
  github_reads a_default_branch_that_cannot_go_into_a_path_or_a_head_that_is_no_commit_id_is_an_error
m CM6 'a default branch with a percent sign goes into a path' src/github/reads.rs \
  $'matches!(c, b\'.\' | b\'_\' | b\'-\' | b\'/\')' $'matches!(c, b\'.\' | b\'_\' | b\'-\' | b\'/\' | b\'%\')' \
  github_reads a_default_branch_that_cannot_go_into_a_path_or_a_head_that_is_no_commit_id_is_an_error
m CM7 'the head of the branch is not checked for a commit id' src/github/reads.rs \
  $'hex_id(&doc["commit"]["sha"])' $'doc["commit"]["sha"].as_str().map(str::to_string)' \
  github_reads a_default_branch_that_cannot_go_into_a_path_or_a_head_that_is_no_commit_id_is_an_error
m CM8 'any denial, not only 403, tries the Actions runs' src/status_ci.rs \
  $'Read::Denied {\n            kind: Denial::Forbidden,\n            ..\n        } => Err(Gap::Forbidden),' $'Read::Denied { .. } => Err(Gap::Forbidden),' \
  status_ci_main only_a_403_on_check_runs_tries_the_actions_runs
m CM9 'the newest run counts per workflow only, not per event' src/status_ci.rs \
  $'let key = (r.workflow_id, r.event.as_str());' $'let key = (r.workflow_id, "");' \
  status_ci_main every_workflow_and_every_event_is_counted_once
m CM10 'the run attempt does not rank runs' src/status_ci.rs \
  $'(r.created_at.clone(), r.run_attempt, r.id)' $'(r.created_at.clone(), 0, r.id)' \
  status_ci_main a_higher_run_attempt_wins_at_the_same_time
m CM11 'the oldest run of a workflow counts' src/status_ci.rs \
  $'rank(r) > rank(old)' $'rank(r) < rank(old)' \
  status_ci_main the_newest_run_of_a_workflow_and_event_counts
m CM12 'a run of another commit counts' src/status_ci.rs \
  $'.filter(|r| r.head_sha.as_deref() == Some(sha))' $'.filter(|_| true)' \
  status_ci_main an_actions_run_of_another_commit_is_ignored
m CM13 'commit statuses are not counted' src/status_ci.rs \
  $'check_class(&runs, &statuses)?' $'check_class(&runs, &[])?' \
  status_ci_main commit_statuses_are_counted_in_both_ways
m CM14 'the forbidden cause does not name the Actions right' src/status_ci.rs \
  $' or Actions: Read";' $'";' \
  status_ci_main both_ways_refused_stays_forbidden_and_names_both_rights
m CM15 'the Actions way is labeled as check runs' src/status_ci.rs \
  $'Ok(runs) => (latest_runs(&runs, &head), VIA_ACTIONS_RUNS),' $'Ok(runs) => (latest_runs(&runs, &head), VIA_CHECK_RUNS),' \
  status_ci_main a_403_on_check_runs_falls_back_to_the_actions_runs_of_the_head
m CM16 'a partial list gives a class' src/status_ci.rs \
  $'Read::Observed(o) if o.incomplete.is_none() => Ok(o.items),' $'Read::Observed(o) => Ok(o.items),' \
  status_ci_main an_incomplete_list_or_a_run_without_state_is_not_read_never_a_class
m CM17 'the text names the wrong way' src/status_ci.rs \
  $'"via Actions runs"' $'"via check runs"' \
  status_ci_main_text the_line_of_a_repository_says_the_class_the_head_and_the_way
m CM18 'a red head is no attention reason' src/status.rs \
  $'if s.ci_main.as_ref().is_some_and(|m| m.class == "red") {\n        v.push("main-red".into());' $'if s.ci_main.as_ref().is_some_and(|m| m.class == "pending") {\n        v.push("main-red".into());' \
  status_ci_main_text only_a_red_head_needs_action_and_the_other_classes_do_not
m CM19 'the CI notice has the code of the issues notice' src/status.rs \
  $'"ci-not-read",' $'"issues-not-read",' \
  status_ci_main_text a_ci_read_that_failed_is_a_line_a_notice_and_exit_0
m CM20 'the CI notice has the source pulls' src/status.rs \
  $'                "ci",\n' $'                "pulls",\n' \
  status_ci_main_text a_ci_read_that_failed_is_a_line_a_notice_and_exit_0
m CM21 'the JSON ci_main has no source' src/status.rs \
  $'"class": m.class, "source": m.source}' $'"class": m.class, "source": null}' \
  status_ci_main the_json_names_the_branch_the_head_and_the_way
m CM22 'the JSON ci_main says not read after a read' src/status.rs \
  $'Some(m) => json!({"read": true,' $'Some(m) => json!({"read": false,' \
  status_ci_main the_json_names_the_branch_the_head_and_the_way
m CM23 'the row of status PATH has no cell for a red head' src/status_all.rs \
  $'cell += " | main red";' $'cell += "";' \
  status_ci_main_text status_path_carries_the_ci_main_in_the_json_the_notice_and_the_attention
m CM24 'the commit reads are not counted as ci-main' src/github.rs \
  $'None | Some("branches" | "commits" | "actions") => "ci-main",' $'None | Some("branches" | "actions") => "ci-main",' \
  status_ci_main the_requests_of_the_source_are_counted_under_ci_main_in_the_stats
m CM25 'ready-check takes a denied check run read as no check' src/ready_check.rs \
  $'        self.check_runs(r, h, &Budget::default())\n            .map_err(|e| e.to_string())' $'        self.check_runs(r, h, &Budget::default())\n            .map(|x| match x {\n                Read::Denied { .. } => Read::Observed(crate::github::reads::Observed { items: vec![], pages: 0, incomplete: None }),\n                o => o,\n            })\n            .map_err(|e| e.to_string())' \
  ready_check cli_ready_check_stays_strict_a_403_on_check_runs_is_unknown_and_no_actions_run_is_read
m CM26 'the default branch is not read from the repository document' src/github/reads.rs \
  $'let doc = self.get(&format!("{api}/repos/{slug}/branches/{branch}"))?;' $'let doc = self.get(&format!("{api}/repos/{slug}/branches/main"))?;' \
  github_reads the_default_branch_and_its_head_are_read_from_two_documents
m CM27 'a default branch name of 201 characters is read' src/github/reads.rs \
  $'        && b.len() <= 200\n' $'        && b.len() <= 201\n' \
  github_reads a_default_branch_name_of_200_characters_is_read_and_one_of_201_is_not
m CM28 'the creation time does not rank the runs of a workflow' src/status_ci.rs \
  'let rank = |r: &WorkflowRun| (r.created_at.clone(), r.run_attempt, r.id);' 'let rank = |r: &WorkflowRun| (String::new(), r.run_attempt, r.id);' \
  status_ci_main the_newest_run_of_a_workflow_and_event_counts
m CM29 'action_required is not a failing check' src/ready_check.rs \
  $'                            | "action_required"\n' $'' \
  status_ci_main check_runs_give_the_class_green_pending_or_red
m CM30 'an Actions run keeps no conclusion, every run is a success' src/status_ci.rs \
  'conclusion: r.conclusion.clone(),' 'conclusion: r.conclusion.as_ref().map(|_| "success".into()),' \
  status_ci_main the_actions_runs_are_classified_with_the_rule_of_the_check_runs
m TS1 'default high threshold 95 becomes 99' src/tidy_similarity.rs \
  'pub const HIGH_PCT: u32 = 95;' 'pub const HIGH_PCT: u32 = 99;' \
  tidy_similarity the_defaults_are_95_70_and_seven_days
m TS2 'default low threshold 70 becomes 60' src/tidy_similarity.rs \
  'pub const LOW_PCT: u32 = 70;' 'pub const LOW_PCT: u32 = 60;' \
  tidy_similarity the_defaults_are_95_70_and_seven_days
m TS3 'exactly at the threshold no longer counts (>= becomes >)' src/tidy_similarity.rs \
  '200 * self.inter >= pct as u64 * self.total()' '200 * self.inter > pct as u64 * self.total()' \
  tidy_similarity threshold_is_at_or_above_in_integers
m TS4 'the sign is not part of the key' src/tidy_similarity.rs \
  'h.write_u8(sign);' 'h.write_u8(0);' \
  tidy_similarity the_sign_is_part_of_the_key
m TS5 'the path is not part of the key with path' src/tidy_similarity.rs \
  '.map(|e| hash_key(Some(&e.path), e.sign, &e.line))' '.map(|e| hash_key(None, e.sign, &e.line))' \
  tidy_similarity the_path_is_part_of_the_key_but_not_of_the_no_path_value
m TS6 'the time window is removed' src/tidy_similarity.rs \
  'candidate_time >= commit_time.saturating_sub(window_secs)' 'candidate_time >= i64::MIN' \
  tidy_similarity the_window_is_seven_days_by_author_time
m TS7 'the window excludes exactly 7 days (>= becomes >)' src/tidy_similarity.rs \
  'candidate_time >= commit_time.saturating_sub(window_secs)' 'candidate_time > commit_time.saturating_sub(window_secs)' \
  tidy_similarity the_window_is_seven_days_by_author_time
m TS8 'candidates by file overlap are never accepted' src/tidy_similarity.rs \
  '&& !seen.contains(n)
                    && 2 * *k >=' '&& !seen.contains(n)
                    && false
                    && 2 * *k >=' \
  tidy_similarity candidates_by_subject_by_subject_without_prefix_and_by_file_overlap
m TS9 'the prefix-less subject is not looked up stripped' src/tidy_similarity.rs \
  '.get(strip_type_scope(&c.subject))' '.get(&c.subject)' \
  tidy_similarity candidates_by_subject_by_subject_without_prefix_and_by_file_overlap
m TS10 'file overlap needs only one file in common' src/tidy_similarity.rs \
  '2 * *k >=' '*k >=' \
  tidy_similarity candidates_by_subject_by_subject_without_prefix_and_by_file_overlap
m TS11 'file overlap accepts less than half' src/tidy_similarity.rs \
  '2 * *k >=' '3 * *k >=' \
  tidy_similarity candidates_by_subject_by_subject_without_prefix_and_by_file_overlap
m TS12 'multiset becomes set' src/tidy_similarity.rs \
  'with_path.sort_unstable();' 'with_path.sort_unstable();
        with_path.dedup();' \
  tidy_similarity a_line_that_occurs_twice_counts_twice
m TS13 'lines are not trimmed' src/tidy_similarity.rs \
  'let ws = |b: &u8| matches!(b, b'\'' '\'' | b'\''\t'\'' | b'\''\r'\'' | b'\''\n'\'' | 0x0b | 0x0c);' 'let ws = |_: &u8| false;' \
  tidy_similarity only_the_white_space_borders_are_trimmed
m TS14 'CR is not a white-space border' src/tidy_similarity.rs \
  'b'\'' '\'' | b'\''\t'\'' | b'\''\r'\'' | b'\''\n'\''' 'b'\'' '\'' | b'\''\t'\'' | b'\''\n'\''' \
  tidy_similarity only_the_white_space_borders_are_trimmed
m TS15 'the minimum is the maximum' src/tidy_similarity.rs \
  '.min_by(|a, b| a.cmp_value(b))' '.max_by(|a, b| a.cmp_value(b))' \
  tidy_similarity figures_count_minimum_and_median
m TS16 'the median of an even count takes the lower middle' src/tidy_similarity.rs \
  'Some((shown[n / 2 - 1] + shown[n / 2]).div_ceil(2))' 'Some(shown[n / 2 - 1])' \
  tidy_similarity figures_count_minimum_and_median
m TS17 'the median is taken from an unsorted list' src/tidy_similarity.rs \
  'shown.sort_unstable();' 'let _ = &shown;' \
  tidy_similarity figures_count_minimum_and_median
m TS18 'the low boundary is exclusive' src/tidy_similarity.rs \
  '.any(|s| !s.ge_pct(low))' '.any(|s| !s.ge_pct(low + 1))' \
  tidy_similarity class_boundaries_are_inclusive
m TS19 'a commit without candidate does not prevent landed' src/tidy_similarity.rs \
  'without + skipped.too_large' 'skipped.too_large' \
  tidy_similarity class_boundaries_are_inclusive
m TS20 'a too_large commit does not prevent landed' src/tidy_similarity.rs \
  'without + skipped.too_large' 'without' \
  tidy_similarity a_commit_without_value_prevents_landed
m TS21 'merge commits count as judged' src/tidy_similarity.rs \
  'Outcome::Skipped(Skip::Merge | Skip::Empty)' 'Outcome::Skipped(Skip::Empty)' \
  tidy_similarity no_plus_commit_is_class_none_and_merges_and_empty_commits_are_counted
m TS22 'a tie goes to the larger commit ID' src/tidy_similarity.rs \
  'Ordering::Equal => cand.id < b.id,' 'Ordering::Equal => cand.id > b.id,' \
  tidy_similarity the_best_candidate_is_the_highest_and_a_tie_goes_to_the_smaller_id
m TS23 'the best candidate is the first, not the highest' src/tidy_similarity.rs \
  'Ordering::Greater => true,' 'Ordering::Greater => false,' \
  tidy_similarity the_best_candidate_is_the_highest_and_a_tie_goes_to_the_smaller_id
m TS24 'a binary file has no key' src/tidy_similarity.rs \
  'out.push(plain(b'\''B'\'', line.into_bytes()));' 'let _ = line;' \
  tidy_similarity a_binary_file_is_one_key_by_blob_id_without_content_lines
m TS25 'the key limit is exclusive (> becomes >=)' src/tidy_similarity.rs \
  'if out.len() > lim.max_keys {' 'if out.len() >= lim.max_keys {' \
  tidy_similarity a_commit_over_the_limits_is_too_large
m TS26 'the key limit is not applied' src/tidy_similarity.rs \
  'if out.len() > lim.max_keys {' 'if false && out.len() > lim.max_keys {' \
  tidy_similarity a_commit_over_the_limits_is_too_large
m TS27 'the blob size limit is not applied' src/tidy_similarity.rs \
  'repo.find_header(id)?.size() > lim.max_blob_bytes' 'false && repo.find_header(id)?.size() > lim.max_blob_bytes' \
  tidy_similarity a_commit_over_the_limits_is_too_large
m TS28 'evidence is not cut at the maximum' src/tidy_similarity.rs \
  '.take(max)' '.take(usize::MAX)' \
  tidy_similarity evidence_names_the_biggest_differences_masked_and_at_most_five
m TS29 'evidence lists the smallest difference first' src/tidy_similarity.rs \
  'd(&q.1).cmp(&d(&p.1))' 'd(&p.1).cmp(&d(&q.1))' \
  tidy_similarity evidence_orders_by_difference_then_key
m TS30 'text is not escaped' src/tidy_similarity.rs \
  's.push_str(&escape_untrusted(chunk.valid(), usize::MAX).0);' 's.push_str(chunk.valid());' \
  tidy_similarity masking_handles_bytes_controls_and_length
m TS31 'bytes that are not UTF-8 are not marked' src/tidy_similarity.rs \
  's.push_str(&format!("\\x{byte:02x}"));' 's.push('\''?'\'');' \
  tidy_similarity masking_handles_bytes_controls_and_length
m TS32 'the display is not clamped below the threshold' src/tidy_similarity.rs \
  '!self.ge_pct(threshold_pct) {
            t - 1
' '!self.ge_pct(threshold_pct) {
            t
' \
  tidy_similarity threshold_is_at_or_above_in_integers
m TS33 'rounding is truncation' src/tidy_similarity.rs \
  '((4000 * self.inter + s) / (2 * s)) as u32' '((4000 * self.inter) / (2 * s)) as u32' \
  tidy_similarity dice_numbers
m TS34 'values are ordered the wrong way round' src/tidy_similarity.rs \
  '_ => l.cmp(&r),' '_ => r.cmp(&l),' \
  tidy_similarity the_comparison_of_values_is_exact
m TS35 'the sum is not sorted' src/tidy_similarity.rs \
  'v.sort_unstable();' '' \
  tidy_similarity a_sum_of_commits_and_containment
m TS36 'containment in the first set uses the second size' src/tidy_similarity.rs \
  'pub fn containment_in_a(&self) -> (u64, u64) {
        (self.inter, self.a)' 'pub fn containment_in_a(&self) -> (u64, u64) {
        (self.inter, self.b)' \
  tidy_similarity a_sum_of_commits_and_containment
m TS37 'a merge commit has keys' src/tidy_similarity.rs \
  'if parents.len() > 1 {
        return Ok(Entries::Skipped(Skip::Merge));' 'if parents.len() > 5 {
        return Ok(Entries::Skipped(Skip::Merge));' \
  tidy_similarity merge_and_empty_commits_are_skipped_with_a_reason
m TS38 'an empty cumulative diff has keys' src/tidy_similarity.rs \
  '    if out.is_empty() {
        return Ok(Entries::Skipped(Skip::Empty));' '    if false && out.is_empty() {
        return Ok(Entries::Skipped(Skip::Empty));' \
  tidy_similarity the_cumulative_diff_of_equal_trees_is_empty
m TS39 'a mode change has no key' src/tidy_similarity.rs \
  'if ko != kn {' 'if false && ko != kn {' \
  tidy_similarity a_mode_change_and_an_empty_new_file_have_their_own_keys
m TS40 'an empty new file has no key' src/tidy_similarity.rs \
  'if f.old.is_none() {
                out.push(plain(b'\''N'\''' 'if false {
                out.push(plain(b'\''N'\''' \
  tidy_similarity a_mode_change_and_an_empty_new_file_have_their_own_keys
m TS41 'the prefix check of the subject is skipped' src/tidy_similarity.rs \
  'if subject.get(i) != Some(&b'\'':'\'') {' 'if false {' \
  tidy_similarity the_subject_prefix_is_stripped_like_conventional_commits
m TS42 'the cut flag is never set' src/tidy_similarity.rs \
  'let cut = out.len() > lim.max_candidates;' 'let cut = false;' \
  tidy_similarity candidates_are_cut_at_the_limit_and_the_cut_is_reported
m TS43 'candidates are not truncated' src/tidy_similarity.rs \
  'out.truncate(lim.max_candidates);' '' \
  tidy_similarity candidates_are_cut_at_the_limit_and_the_cut_is_reported
m TS44 'the since filter of the main index is off' src/tidy_similarity.rs \
  'if since.is_some_and(|t| info.author_time < t) {' 'if false {' \
  tidy_similarity a_main_commit_older_than_since_is_not_indexed
m TS45 'merges on main are indexed' src/tidy_similarity.rs \
  'if parents.len() > 1 {
            None' 'if parents.len() > 100 {
            None' \
  tidy_similarity a_walk_over_main_leaves_out_merges
m TS46 'duplicate plus commits are not removed' src/tidy_similarity.rs \
  'ids.dedup();' '' \
  tidy_similarity the_same_input_gives_the_same_output_in_any_order
m TS47 'plus commits are not sorted' src/tidy_similarity.rs \
  'ids.sort();' '' \
  tidy_similarity the_same_input_gives_the_same_output_in_any_order
m TS48 'evidence is given at or above the high threshold too' src/tidy_similarity.rs \
  'let ev = if sim.ge_pct(high) {' 'let ev = if false {' \
  tidy_similarity a_rebase_with_a_small_conflict_resolution_has_the_same_subject_and_less_than_100
m TS49 'the value without the path is the value with the path' src/tidy_similarity.rs \
  'best = Some((cand, sim, keys.similarity_no_path(&ck)));' 'best = Some((cand, sim, keys.similarity(&ck)));' \
  tidy_similarity a_rename_shows_only_in_the_value_without_the_path
m TS50 'the value without the path decides the class' src/tidy_similarity.rs \
  'Outcome::Compared(x) => sims.push(x.sim),' 'Outcome::Compared(x) => sims.push(x.no_path),' \
  tidy_similarity a_rename_shows_only_in_the_value_without_the_path
m TS51 'candidates of one reason are not ordered by commit ID' src/tidy_similarity.rs \
  'by_subject.sort_by_key(|x| (x.reason, x.id));' '' \
  tidy_similarity candidates_are_cut_at_the_limit_and_the_cut_is_reported

m RB1 'the author time is not part of the key' src/tidy_rebase.rs \
  'secs: a.seconds(),' 'secs: 0,' \
  tidy_rebase one_second_more_or_less_is_another_commit
m RB2 'the mail is not part of the key' src/tidy_rebase.rs \
  'mail: a.email.to_ascii_lowercase().to_vec(),' 'mail: Vec::new(),' \
  tidy_rebase the_mail_is_compared_in_lower_case_and_the_name_exactly
m RB3 'the order of the targets is not checked' src/tidy_rebase.rs \
  'let in_order = mapped_at.windows(2).all(|w| w[0].1 > w[1].1);' 'let in_order = true;' \
  tidy_rebase the_same_commits_in_another_order_are_a_hint
m RB4 'the run (no foreign commit between the targets) is not required' src/tidy_rebase.rs \
  'let contiguous = mapped > 0 && gap == 0;' 'let contiguous = mapped > 0;' \
  tidy_rebase a_foreign_commit_between_the_targets_breaks_the_run
m RB5 'the subject is cut to its first 30 bytes' src/tidy_rebase.rs \
  'subject: subject_of(c.message_raw()?),' 'subject: subject_of(c.message_raw()?).into_iter().take(30).collect(),' \
  tidy_rebase the_subject_is_the_whole_first_line_right_trimmed
m RB6 'the author time is rounded to the minute' src/tidy_rebase.rs \
  'secs: a.seconds(),' 'secs: a.seconds() / 60,' \
  tidy_rebase one_second_more_or_less_is_another_commit
m RB7 'the subject is not part of the key (key collision)' src/tidy_rebase.rs \
  'subject: subject_of(c.message_raw()?),' 'subject: Vec::new(),' \
  tidy_rebase the_same_second_in_two_commits_needs_the_subject
m RB8 'the author name is not compared' src/tidy_rebase.rs \
  '.filter(|p| index.positions[*p as usize].name == h.name)' '.filter(|_| true)' \
  tidy_rebase the_mail_is_compared_in_lower_case_and_the_name_exactly
m RB9 'a pull request number is stripped from the subject' src/tidy_rebase.rs \
  'first[..end].to_vec()' 'first[..end].strip_suffix(b" (#7)").unwrap_or(&first[..end]).to_vec()' \
  tidy_rebase a_squash_subject_with_a_pull_request_number_is_not_the_subject_of_the_commit
m RB10 'one mapped commit is enough for a proof' src/tidy_rebase.rs \
  'n >= 1 && mapped == n && contiguous' 'n >= 1 && mapped >= 1 && contiguous' \
  tidy_rebase a_part_of_the_branch_that_landed_as_one_run_is_partial
m RB11 'the decision of the proof is delete' src/tidy_rebase.rs \
  'pub const RB_DECISION: &str = "review";' 'pub const RB_DECISION: &str = "delete";' \
  tidy_rebase a_clean_rebase_merge_is_a_strong_proof_and_the_class_is_review
m RB12 'a hint gets the class too (replaces the plan-order mutation of package 2)' src/tidy_rebase.rs \
  '(r.strength == Strength::Strong).then_some((RB_CLASS, RB_DECISION))' '(r.strength != Strength::None).then_some((RB_CLASS, RB_DECISION))' \
  tidy_rebase a_hint_or_nothing_gives_no_class_and_the_decision_is_never_delete
m RB13 'a merge commit of the chain is a target' src/tidy_rebase.rs \
  'if head.parents <= 1 {' 'if true {' \
  tidy_rebase a_merge_commit_on_the_chain_is_a_position_but_never_a_target
m RB14 'a set instead of a multiset: a position is taken twice' src/tidy_rebase.rs \
  '                .filter(|p| !used.contains(p))
                .collect();' '                .filter(|_| true)
                .collect();' \
  tidy_rebase a_position_is_taken_once_two_branch_commits_do_not_share_one_main_commit
m RB15 'the content value of an identity match is not held to 95 percent' src/tidy_rebase.rs \
  '                high &= sim.ge_pct(RB_CONTENT_HIGH);
                low &= sim.ge_pct(LOW_PCT);' '                low &= sim.ge_pct(LOW_PCT);' \
  tidy_rebase a_proof_needs_95_percent_per_commit_70_to_94_is_modified_below_is_no_proof
m RB16 'the age bound has no slack before the oldest author time' src/tidy_rebase.rs \
  'head.key.secs < s.saturating_sub(RB_SLACK_SECS)' 'head.key.secs < s' \
  tidy_rebase the_age_bound_is_a_day_before_the_oldest_author_time_inclusive
m RB17 'similarity fills any gap, not only one between two neighbours' src/tidy_rebase.rs \
  'if a.pos[i].is_some() || up != down + 2 {' 'if a.pos[i].is_some() {' \
  tidy_rebase similarity_is_not_used_without_the_neighbours_on_both_sides_of_one_free_position
m RB18 'the parent count of the merge commit is ignored' src/tidy_rebase.rs \
  'if merge_head.parents == 2 {' 'if false {' \
  tidy_rebase a_merge_commit_with_two_parents_is_a_merge
m RB19 'the identity of the k commits is not checked' src/tidy_rebase.rs \
  '&& (0..k).all(|j| same_identity(&chain[k - 1 - j], &pr.commits[j]));' '&& true;' \
  tidy_rebase the_commits_behind_the_merge_commit_must_equal_the_pull_request_commits
m RB20 'a pull request that was not read is an error' src/tidy_rebase.rs \
  'ApiInput::NotRead(r) => return Ok(ApiReport::not_read(*r)),' 'ApiInput::NotRead(r) => return Err(crate::error::Error::usage(r.as_str())),' \
  tidy_rebase without_a_pull_request_mapping_the_reason_is_kept_and_nothing_fails
m RB21 'a partial mapping is landed via rebase' src/tidy_rebase.rs \
  'if rebase.is_some_and(|r| r.strength == Strength::Strong) {' 'if rebase.is_some_and(|r| r.strength == Strength::Strong || r.partial) {' \
  tidy_rebase a_partial_or_weak_mapping_is_unknown
m RB22 'subjects are not masked' src/tidy_rebase.rs \
  '    mask_bytes(text, RB_SUBJECT_CHARS)' '    String::from_utf8_lossy(text).into_owned()' \
  tidy_rebase the_mapping_has_one_row_per_commit_with_masked_subjects
m RB23 'remaining has no files' src/tidy_rebase.rs \
  '.take(RB_MAX_REMAINING_FILES)' '.take(0)' \
  tidy_rebase what_is_not_mapped_is_listed_with_its_files
m RB24 'the ancestor flag is ignored by landed_via (replaces the apply mutation of package 2)' src/tidy_rebase.rs \
  '    if ancestor {' '    if false {' \
  tidy_rebase landed_via_follows_the_precedence
m RB25 'the neighbour behind (newer) does not decide among equal keys' src/tidy_rebase.rs \
  'let after = a.pos.get(i + 1).copied().flatten().map(|p| p + 1);' 'let after: Option<u32> = None;' \
  tidy_rebase the_same_key_twice_is_a_multiset_and_the_neighbour_decides
m RB26 'the neighbour in front (older) does not decide among equal keys' src/tidy_rebase.rs \
  'let before = i
                        .checked_sub(1)' 'let before = i
                        .checked_sub(1)
                        .filter(|_| false)' \
  tidy_rebase the_same_key_twice_is_a_multiset_and_the_neighbour_decides
m RB27 'an ambiguous commit is not flagged' src/tidy_rebase.rs \
  'a.ambiguous[i] = true;' 'let _ = i;' \
  tidy_rebase the_same_key_twice_without_a_neighbour_is_ambiguous_and_not_mapped
m RB28 'an equal patch on another commit does not win over the identity' src/tidy_rebase.rs \
  'if let Some(p) = hit {' 'if let Some(p) = hit.filter(|_| false) {' \
  tidy_rebase an_equal_patch_on_another_commit_wins_over_the_identity
m RB29 'the oldest branch commit takes the newest equal patch' src/tidy_rebase.rs \
  'if let Some(m) = main.pop() {' 'if let Some(m) = (!main.is_empty()).then(|| main.remove(0)) {' \
  tidy_rebase equal_patches_pair_branch_commits_with_chain_commits_oldest_with_oldest
m RB30 'the low threshold of the similarity fill is 60' src/tidy_rebase.rs \
  'if sim.ge_pct(LOW_PCT) {' 'if sim.ge_pct(LOW_PCT - 10) {' \
  tidy_rebase a_gap_of_one_between_two_mapped_commits_is_filled_by_similarity
m RB31 'a different zone offset is not noted' src/tidy_rebase.rs \
  '.is_some_and(|p| index.positions[p as usize].offset != heads[i].offset);' '.is_some_and(|_| false);' \
  tidy_rebase a_zone_offset_that_differs_is_noted_and_the_epoch_second_decides
m RB32 'a merge commit of the branch counts as a commit' src/tidy_rebase.rs \
  'if head.parents > 1 {' 'if head.parents > 100 {' \
  tidy_rebase the_branch_side_is_the_non_merge_commits_oldest_first
m RB33 'the branch side takes the newest ready commit first' src/tidy_rebase.rs \
  '.map(|(id, _)| std::cmp::Reverse((set[id].key.secs, *id)))' '.map(|(id, _)| std::cmp::Reverse((-set[id].key.secs, *id)))' \
  tidy_rebase the_branch_side_puts_parents_before_children_even_with_equal_times
m RB34 'the window holds one commit more than the limit' src/tidy_rebase.rs \
  'if idx.positions.len() >= max_window {' 'if idx.positions.len() > max_window {' \
  tidy_rebase the_walk_stops_at_the_window_limit_and_says_so
m RB35 'a missing object is an error for the whole index' src/tidy_rebase.rs \
  'if !idx.positions.is_empty() && !repo.has_object(id) {' 'if false {' \
  tidy_rebase a_missing_parent_object_ends_the_walk_without_an_error
m RB36 'the age bound is exclusive of the day' src/tidy_rebase.rs \
  'head.key.secs < s.saturating_sub(RB_SLACK_SECS)' 'head.key.secs <= s.saturating_sub(RB_SLACK_SECS)' \
  tidy_rebase the_age_bound_is_a_day_before_the_oldest_author_time_inclusive
m RB37 'the content threshold is 96 percent' src/tidy_rebase.rs \
  '                high &= sim.ge_pct(RB_CONTENT_HIGH);
                low &= sim.ge_pct(LOW_PCT);' '                high &= sim.ge_pct(RB_CONTENT_HIGH + 1);
                low &= sim.ge_pct(LOW_PCT);' \
  tidy_rebase a_proof_needs_95_percent_per_commit_70_to_94_is_modified_below_is_no_proof
m RB38 'the position of an API target is counted from the merge commit' src/tidy_rebase.rs \
  'position: Some((dist + k - 1 - j) as u32),' 'position: Some((k - 1 - j) as u32),' \
  tidy_rebase a_pull_request_with_one_parent_and_identity_equal_commits_is_a_rebase
m RB39 'the squash time tolerance is 4 seconds' src/tidy_rebase.rs \
  '<= RB_API_TIME_TOLERANCE as u64' '<= RB_API_TIME_TOLERANCE as u64 + 2' \
  tidy_rebase a_commit_with_the_time_of_the_merge_is_a_squash
m RB40 'an equal patch without identity is a proof' src/tidy_rebase.rs \
  '&& content_ok && identity_backed' '&& content_ok' \
  tidy_rebase one_commit_is_enough_for_a_proof_when_it_has_the_identity
m RB41 'a single mapped commit is no hint' src/tidy_rebase.rs \
  '} else if mapped >= 1 {' '} else if mapped >= 2 {' \
  tidy_rebase one_commit_is_enough_for_a_proof_when_it_has_the_identity
m RB42 'every mapped commit is its own run' src/tidy_rebase.rs \
  '&& mapped_at[k - 1].1 == x.1 + 1;' '&& false;' \
  tidy_rebase a_foreign_commit_between_the_targets_breaks_the_run
m RB43 'the gap is not counted' src/tidy_rebase.rs \
  '(Some(lo), Some(hi)) => ((hi - lo + 1) as usize).saturating_sub(mapped),' '(Some(_), Some(_)) => 0,' \
  tidy_rebase a_foreign_commit_between_the_targets_breaks_the_run
m RB44 'an equal patch has no content value' src/tidy_rebase.rs \
  '                Some(1000)' '                None' \
  tidy_rebase a_conflict_resolution_keeps_the_identity_with_a_content_value
m RB45 'the content threshold is 90 percent (RB_CONTENT_HIGH)' src/tidy_rebase.rs \
  'pub const RB_CONTENT_HIGH: u32 = 95;' 'pub const RB_CONTENT_HIGH: u32 = 90;' \
  tidy_rebase a_proof_needs_95_percent_per_commit_70_to_94_is_modified_below_is_no_proof
m RB46 'a commit filled by similarity is not held to 95 percent' src/tidy_rebase.rs \
  '                value[i] = Some(sim.shown(LOW_PCT));
                high &= sim.ge_pct(RB_CONTENT_HIGH);' '                value[i] = Some(sim.shown(LOW_PCT));' \
  tidy_rebase a_gap_of_one_between_two_mapped_commits_is_filled_by_similarity
m RB47 'modified needs no 70 percent' src/tidy_rebase.rs \
  'modified: !high && low,' 'modified: !high,' \
  tidy_rebase a_proof_needs_95_percent_per_commit_70_to_94_is_modified_below_is_no_proof
m RB48 'an identity value below 70 percent does not count against modified' src/tidy_rebase.rs \
  '                low &= sim.ge_pct(LOW_PCT);
            }
            Content::BothEmpty' '            }
            Content::BothEmpty' \
  tidy_rebase a_proof_needs_95_percent_per_commit_70_to_94_is_modified_below_is_no_proof
m RB49 'a later version is not recognised' src/tidy_rebase.rs \
  'heads[i].committer_secs > index.positions[p as usize].committer_secs' 'false' \
  tidy_rebase an_amend_after_landing_keeps_the_author_time_and_is_at_most_a_hint
m RB50 'the same committer time is a later version' src/tidy_rebase.rs \
  'heads[i].committer_secs > index.positions[p as usize].committer_secs' 'heads[i].committer_secs >= index.positions[p as usize].committer_secs' \
  tidy_rebase an_amend_after_landing_keeps_the_author_time_and_is_at_most_a_hint
m RB51 'an equal patch can be a later version' src/tidy_rebase.rs \
  '(Some(p), Method::Identity) => {
                heads[i].committer_secs' '(Some(p), _) => {
                heads[i].committer_secs' \
  tidy_rebase an_amend_after_landing_keeps_the_author_time_and_is_at_most_a_hint
m RB52 'a later version does not keep the proof at hint' src/tidy_rebase.rs \
  'let content_ok = high && !later.iter().any(|x| *x);' 'let content_ok = high;' \
  tidy_rebase an_amend_after_landing_keeps_the_author_time_and_is_at_most_a_hint
m RB53 'the walk stops after one old commit' src/tidy_rebase.rs \
  'if old_run >= RB_STOP_RUN {' 'if old_run >= 1 {' \
  tidy_rebase a_run_of_old_author_times_ends_the_walk_a_single_one_does_not
m RB54 'the run of old commits is not broken by a recent commit' src/tidy_rebase.rs \
  '            } else {
                old_run = 0;' '            } else {
                old_run += 0;' \
  tidy_rebase a_run_of_old_author_times_ends_the_walk_a_single_one_does_not
m RB55 'the walk stops by the committer time again' src/tidy_rebase.rs \
  'head.key.secs < s.saturating_sub(RB_SLACK_SECS)' 'head.committer_secs < s.saturating_sub(RB_SLACK_SECS)' \
  tidy_rebase an_old_committer_time_does_not_end_the_walk
m RB56 'the stop reason names the tip instead of the missing commit' src/tidy_rebase.rs \
  'idx.stop = Stop::Missing(id);' 'idx.stop = Stop::Missing(base_tip);' \
  tidy_rebase a_missing_parent_object_ends_the_walk_without_an_error
m RB57 'the boundary commit is read for its patch ID' src/tidy_rebase.rs \
  'if h.parents > 1 || index.is_edge(pos as u32) {' 'if h.parents > 1 {' \
  tidy_rebase the_boundary_commit_of_a_shallow_clone_is_skipped_not_an_error
m RB58 'the content of the boundary commit is read' src/tidy_rebase.rs \
  'let content = if index.is_edge(p) {' 'let content = if false {' \
  tidy_rebase the_boundary_commit_of_a_shallow_clone_is_skipped_not_an_error
m RB59 'the boundary commit is not the last one of the walk' src/tidy_rebase.rs \
  'matches!(self.stop, Stop::Missing(_)) && pos as usize + 1 == self.positions.len()' 'matches!(self.stop, Stop::Missing(_)) && pos == 0' \
  tidy_rebase the_boundary_commit_of_a_shallow_clone_is_skipped_not_an_error
m RB60 'an unreadable PR commit behind the merge commit is an error' src/tidy_rebase.rs \
  'Err(_) if !chain.is_empty() => break,' 'Err(_) if false => break,' \
  tidy_rebase a_shallow_boundary_below_the_merge_commit_is_not_an_error
m RB61 'an API rebase with remaining commits is strong' src/tidy_rebase.rs \
  'rep.strength = if rep.remaining.is_empty() && rep.head_in_branch == Some(true) {' 'rep.strength = if rep.head_in_branch == Some(true) {' \
  tidy_rebase commits_after_the_pull_request_head_remain
m RB62 'an API rebase with a head outside the branch is strong' src/tidy_rebase.rs \
  'rep.strength = if rep.remaining.is_empty() && rep.head_in_branch == Some(true) {' 'rep.strength = if rep.remaining.is_empty() {' \
  tidy_rebase commits_after_the_pull_request_head_remain
m RB63 'landed_via does not take the strength of the pull request' src/tidy_rebase.rs \
  'return via(a.kind, Basis::PrApi, a.strength);' 'return via(a.kind, Basis::PrApi, Strength::Strong);' \
  tidy_rebase commits_after_the_pull_request_head_remain
m RB64 'the squash tolerance is exclusive' src/tidy_rebase.rs \
  '<= RB_API_TIME_TOLERANCE as u64' '< RB_API_TIME_TOLERANCE as u64' \
  tidy_rebase the_squash_time_tolerance_is_two_seconds_inclusive_in_both_directions
m RB65 'the squash time difference is not an absolute value' src/tidy_rebase.rs \
  'merge_head.key.secs.abs_diff(t) <= RB_API_TIME_TOLERANCE as u64' '(merge_head.key.secs - t) <= RB_API_TIME_TOLERANCE' \
  tidy_rebase the_squash_time_tolerance_is_two_seconds_inclusive_in_both_directions
m RB66 'the author name of the PR commits is not compared' src/tidy_rebase.rs \
  '    h.name == c.name
        && h.key.mail' '    h.key.mail' \
  tidy_rebase the_pull_request_commits_are_compared_by_name_and_by_mail_in_lower_case
m RB67 'the mail of the PR commits is compared in its own case' src/tidy_rebase.rs \
  'h.key.mail == c.mail.to_ascii_lowercase()' 'h.key.mail == c.mail' \
  tidy_rebase the_pull_request_commits_are_compared_by_name_and_by_mail_in_lower_case
m RB68 'the zone offset of an API mapping is not noted' src/tidy_rebase.rs \
  'tz_differs: m.offset != h.offset,' 'tz_differs: false,' \
  tidy_rebase a_different_zone_offset_of_a_pull_request_commit_is_noted

m TP1 'the similarity stage also runs for a branch that has an equal-patches proof' src/tidy.rs \
  '"pull-request-merged-branch-moved" | "upstream-gone" | "unmerged"
    )' '"pull-request-merged-branch-moved" | "upstream-gone" | "unmerged" | "equal-patches"
    )' \
  tidy_similarity_plan the_existing_proof_has_precedence_over_similarity
m TP2 'an open pull request does not stop the similarity stage' src/tidy.rs \
  'if let Some(n) = pr_open {
            bp.proof = "open-pull-request";' 'if let Some(n) = pr_open.filter(|_| uniq == 0) {
            bp.proof = "open-pull-request";' \
  tidy_similarity_plan similar_patches_comes_before_upstream_gone_and_pull_request_moved_and_after_the_gates
m TP3 'a backup branch is judged by the similarity stage' src/tidy.rs \
  '"pull-request-merged-branch-moved" | "upstream-gone" | "unmerged"
    )' '"pull-request-merged-branch-moved" | "upstream-gone" | "unmerged" | "backup-recent"
    )' \
  tidy_similarity_plan similar_patches_comes_before_upstream_gone_and_pull_request_moved_and_after_the_gates
m TP4 'a worktree does not protect a similar-patches branch' src/tidy.rs \
  'if let Some(dir) = wt.branch_dir(&br) {' 'if let Some(dir) = wt.branch_dir(&br).filter(|_| bp.proof != "similar-patches") {' \
  tidy_similarity_plan similar_patches_comes_before_upstream_gone_and_pull_request_moved_and_after_the_gates
m TP5 'a branch whose proof is pull-request-merged-branch-moved is not judged' src/tidy.rs \
  '"pull-request-merged-branch-moved" | "upstream-gone" | "unmerged"
    )' '"upstream-gone" | "unmerged"
    )' \
  tidy_similarity_plan similar_patches_comes_before_upstream_gone_and_pull_request_moved_and_after_the_gates
m TP6 'a branch whose proof is upstream-gone is not judged' src/tidy.rs \
  '"pull-request-merged-branch-moved" | "upstream-gone" | "unmerged"
    )' '"pull-request-merged-branch-moved" | "unmerged"
    )' \
  tidy_similarity_plan similar_patches_comes_before_upstream_gone_and_pull_request_moved_and_after_the_gates
m TP7 'a similar-patches branch is delete' src/tidy.rs \
  'bp.decision = "review";
            bp.proof = "similar-patches";' 'bp.decision = "delete";
            bp.proof = "similar-patches";' \
  tidy_similarity_plan a_cherry_pick_with_a_conflict_resolution_is_similar_patches_and_review
m TP8 'apply deletes a similar-patches branch' src/tidy_apply.rs \
  'if bp.decision == "delete" {
                    asked.push((bp, Kind::Branch));' 'if bp.decision == "delete" || bp.proof == "similar-patches" {
                    asked.push((bp, Kind::Branch));' \
  tidy_similarity_plan apply_never_deletes_a_similar_patches_branch
m TP9 'modified branches count as similar-patches' src/tidy.rs \
  'if report.class == tidy_similarity::Class::Landed {' 'if report.class != tidy_similarity::Class::Partial {' \
  tidy_similarity_plan modified_partial_squash_and_empty_branches_stay_unmerged_with_their_class
m TP10 'the oldest plus commit sets no start for the index' src/tidy.rs \
  'Some(oldest.saturating_sub(lim.window_secs)),' 'None,' \
  tidy_similarity_plan the_index_starts_a_window_before_the_oldest_plus_commit
m TP11 'the index starts a window after the oldest plus commit' src/tidy.rs \
  'Some(oldest.saturating_sub(lim.window_secs)),' 'Some(oldest.saturating_add(lim.window_secs)),' \
  tidy_similarity_plan a_cherry_pick_with_a_conflict_resolution_is_similar_patches_and_review
m TP12 'the boundary commit of a shallow clone is indexed' src/tidy_similarity.rs \
  'if is_shallow && has_missing_parent(repo, *id)? {' 'if false && has_missing_parent(repo, *id)? {' \
  tidy_similarity_plan a_shallow_clone_gives_a_plan_with_the_boundary_skipped
m TP13 'the boundary commits are not counted' src/tidy_similarity.rs \
  'idx.shallow += 1;' 'idx.shallow += 0;' \
  tidy_similarity the_boundary_of_a_shallow_clone_is_left_out_of_the_index_and_counted
m TP14 'the plan does not report the shallow boundary' src/tidy.rs \
  'skipped_shallow: index.shallow_skipped(),' 'skipped_shallow: 0,' \
  tidy_similarity_plan a_shallow_clone_gives_a_plan_with_the_boundary_skipped
m TP15 '--similarity is ignored' src/cli.rs \
  'high: u32::from(a.similarity),' 'high: 95,' \
  tidy_similarity_plan the_flags_set_the_thresholds_and_the_value_decides_at_the_boundary
m TP16 '--similarity-low is ignored' src/cli.rs \
  'low: u32::from(a.similarity_low),' 'low: 70,' \
  tidy_similarity_plan the_flags_set_the_thresholds_and_the_value_decides_at_the_boundary
m TP17 'a low threshold above the high one is accepted' src/cli.rs \
  'if a.similarity_low > a.similarity {' 'if a.similarity_low > a.similarity + 100 {' \
  tidy_similarity_plan thresholds_out_of_range_or_in_the_wrong_order_are_usage_errors
m TP18 '--no-similarity has no effect' src/cli.rs \
  'similarity: (!a.no_similarity).then(|| tidy::Similarity {' 'similarity: Some(()).map(|_| tidy::Similarity {' \
  tidy_similarity_plan no_similarity_gives_the_plan_of_before
m TP19 'the range of the low threshold starts at 0' src/cli.rs \
  'default_value_t = tidy_similarity::LOW_PCT as u8,
          value_parser = clap::value_parser!(u8).range(1..=100)' 'default_value_t = tidy_similarity::LOW_PCT as u8,
          value_parser = clap::value_parser!(u8).range(0..=100)' \
  tidy_similarity_plan thresholds_out_of_range_or_in_the_wrong_order_are_usage_errors
m TP20 'summary.similar counts equal-patches' src/tidy.rs \
  '.filter(|b| b.proof == "similar-patches")
        .count()' '.filter(|b| b.proof == "equal-patches")
        .count()' \
  tidy_similarity_plan the_summary_counts_similar_branches_as_review_and_names_them
m TP21 'summary.similar is not in the JSON' src/tidy.rs \
  '"similar": similar_count(plans), "rebase"' '"similar": 0, "rebase"' \
  tidy_similarity_plan the_summary_counts_similar_branches_as_review_and_names_them
m TP22 'the key threshold_low is renamed' src/tidy.rs \
  '"threshold_low": s.low,' '"threshold_lo": s.low,' \
  tidy_similarity_plan the_json_of_a_similar_branch_has_the_stable_keys_and_the_figures
m TP23 'the window in the JSON is 0' src/tidy.rs \
  '"window_days": tidy_similarity::WINDOW_DAYS,' '"window_days": 0,' \
  tidy_similarity_plan the_json_of_a_similar_branch_has_the_stable_keys_and_the_figures
m TP24 'the JSON has no evidence lines' src/tidy.rs \
  'x.evidence
                        .iter()' 'x.evidence
                        .iter()
                        .take(0)' \
  tidy_similarity_plan the_json_of_a_modified_branch_has_evidence_lines
m TP25 'the text summary does not name similar-patches' src/tidy.rs \
  '(0, s) => format!(" ({s} of them similar-patches)"),' '(0, _) => String::new(),' \
  tidy_similarity_plan the_text_plan_has_one_line_for_a_similar_branch_and_no_command
m TP26 'a branch without a patch is all matched' src/tidy.rs \
  'self.with_patch > 0 && self.commits.is_empty()' 'self.commits.is_empty()' \
  tidy_similarity_plan modified_partial_squash_and_empty_branches_stay_unmerged_with_their_class
m TP27 'unmatched commits leave out the first of a key' src/tidy.rs \
  'needed.entry(p).or_default().push(info.id);' 'needed.entry(p).or_default();' \
  tidy_similarity_plan unmatched_commits_are_the_branch_commits_without_a_patch_partner_on_main
m TP28 'a value below the low threshold is shown as the low threshold' src/tidy_similarity.rs \
  'if p >= t && !self.ge_pct(low_pct) {
            t - 1' 'if p >= t && !self.ge_pct(low_pct) {
            p' \
  tidy_similarity a_value_below_the_low_threshold_is_not_shown_as_the_low_threshold
m TP29 'the median is taken from values protected for the high threshold only' src/tidy_similarity.rs \
  'sims.iter().map(|s| s.shown_in(high, low)).collect();' 'sims.iter().map(|s| s.shown(high)).collect();' \
  tidy_similarity a_value_below_the_low_threshold_is_not_shown_as_the_low_threshold
m TP30 'the plan does not judge: the + commits are the whole branch' src/tidy.rs \
  'unmatched = Some(u.commits);' 'unmatched = Some(Vec::new());' \
  tidy_similarity_plan a_cherry_pick_with_a_conflict_resolution_is_similar_patches_and_review
m TP31 'the minimum shown ignores the thresholds' src/tidy.rs \
  'let min = report.min.map(|m| m.shown_in(th.high, th.low)).unwrap_or(0);' 'let min = 0;' \
  tidy_similarity_plan a_cherry_pick_with_a_conflict_resolution_is_similar_patches_and_review

m TR1 'the similarity stage also judges a branch with the proof rebase-identity' src/tidy.rs \
  'for row in rows.iter_mut().filter(|r| judged(r.bp.proof)) {' 'for row in rows.iter_mut().filter(|r| judged(r.bp.proof) || r.bp.proof == tidy_rebase::RB_CLASS) {' \
  tidy_rebase_plan rebase_identity_comes_before_similar_patches
m TR2 'rebase-identity replaces an equal-patches proof' src/tidy.rs \
  'if judged(row.bp.proof) && tidy_rebase::evidence_class(&report).is_some() {' 'if (judged(row.bp.proof) || row.bp.proof == "equal-patches") && tidy_rebase::evidence_class(&report).is_some() {' \
  tidy_rebase_plan the_existing_proofs_keep_their_place
m TR3 'rebase-identity replaces a merged-pull-request-head proof' src/tidy.rs \
  'if judged(row.bp.proof) && tidy_rebase::evidence_class(&report).is_some() {' 'if (judged(row.bp.proof) || row.bp.proof == "merged-pull-request-head") && tidy_rebase::evidence_class(&report).is_some() {' \
  tidy_rebase_plan the_existing_proofs_keep_their_place
m TR4 'the rebase stage also runs for open pull requests and backups' src/tidy.rs \
  '            r.unmatched.is_some()
                && (judged(r.bp.proof)
                    || matches!(r.bp.proof, "equal-patches" | "merged-pull-request-head"))' '            true' \
  tidy_rebase_plan the_existing_proofs_keep_their_place
m TR5 'a strong rebase proof is delete' src/tidy.rs \
  'row.bp.decision = tidy_rebase::RB_DECISION;' 'row.bp.decision = "delete";' \
  tidy_rebase_plan a_rebase_merge_with_a_conflict_resolution_is_rebase_identity_and_review
m TR6 'the flag --no-rebase-evidence does not switch the stage off' src/cli.rs \
  'rebase: !a.no_rebase_evidence,' 'rebase: true,' \
  tidy_rebase_plan the_flag_no_rebase_evidence_switches_the_stage_off_and_is_independent_of_the_other
m TR7 'summary.rebase is always 0' src/tidy.rs \
  '"rebase": rebase_count(plans)}})' '"rebase": 0}})' \
  tidy_rebase_plan the_json_has_the_rebase_object_landed_via_and_the_summary
m TR8 'rebase_count counts another proof' src/tidy.rs \
  '.filter(|b| b.proof == tidy_rebase::RB_CLASS)
        .count()' '.filter(|b| b.proof == "similar-patches")
        .count()' \
  tidy_rebase_plan the_json_has_the_rebase_object_landed_via_and_the_summary
m TR9 'the JSON key landed_via has another name' src/tidy.rs \
  '"landed_via": json!({"kind"' '"landed": json!({"kind"' \
  tidy_rebase_plan the_json_has_the_rebase_object_landed_via_and_the_summary
m TR10 'landed_via does not know an ancestor' src/tidy.rs \
  'bp.proof == "merged-ancestor",
            bp.rebase.as_ref().map(|r| &r.report),' 'false,
            bp.rebase.as_ref().map(|r| &r.report),' \
  tidy_rebase_plan landed_via_follows_the_evidence_of_each_branch
m TR11 'landed_via ignores the pull request mapping' src/tidy.rs \
  'bp.rebase.as_ref().map(|r| &r.api),
        );' 'None,
        );' \
  tidy_rebase_plan a_pull_request_that_was_rebased_gives_rebase_by_the_pr_api
m TR12 'the API rule ignores the parent count of the merge commit' src/tidy_rebase.rs \
  'if merge_head.parents == 2 {' 'if false {' \
  tidy_rebase_plan a_pull_request_with_a_merge_commit_gives_merge_by_two_parents
m TR13 'the API rule does not compare the k commits with the pull request commits' src/tidy_rebase.rs \
  '&& (0..k).all(|j| same_identity(&chain[k - 1 - j], &pr.commits[j]));' '&& true;' \
  tidy_rebase_plan a_pull_request_with_a_squash_commit_gives_squash_by_the_merge_time
m TR14 'the API squash rule takes no time tolerance' src/tidy_rebase.rs \
  'merge_head.key.secs.abs_diff(t) <= RB_API_TIME_TOLERANCE as u64' 'merge_head.key.secs.abs_diff(t) <= 0' \
  tidy_rebase_plan a_pull_request_with_a_squash_commit_gives_squash_by_the_merge_time
m TR15 'the request limit is 31' src/tidy.rs \
  'pub const MAX_PR_REQUESTS: usize = 30;' 'pub const MAX_PR_REQUESTS: usize = 31;' \
  tidy_rebase_plan at_most_thirty_requests_are_made_per_repository
m TR16 'the patch keys of the plan are not passed to the mapping' src/tidy.rs \
  'patches.get(self.repo, id))?;' '{ let _ = (&patches, id); Ok(None) })?;' \
  tidy_rebase_plan landed_via_follows_the_evidence_of_each_branch
m TR17 'a branch with an identity hint is not judged by the similarity stage' src/tidy.rs \
  'row.plus = row.unmatched.take();' 'row.plus = row.unmatched.take().filter(|_| row.bp.rebase.as_ref().is_none_or(|r| r.report.mapped == 0));' \
  tidy_rebase_plan a_hint_of_the_rebase_stage_does_not_take_similar_patches_away
m TR18 'a failed stage is not named in the detail' src/tidy.rs \
  'bp.detail += &format!("; rebase evidence not computed ({e})");' 'let _ = (bp, e);' \
  lib tidy::tests::a_failed_stage_is_named_in_the_detail_and_the_proof_stays
m TR19 'the text names landed via for an ancestor too' src/tidy.rs \
  '&& l.basis != Basis::Ancestor
' '
' \
  tidy_rebase_plan the_text_names_the_proof_in_one_line_and_gives_no_command
m TR20 'the text names landed via on a rebase-identity line' src/tidy.rs \
  '&& b.proof != tidy_rebase::RB_CLASS
' '
' \
  tidy_rebase_plan the_text_names_the_proof_in_one_line_and_gives_no_command
m TR21 'the summary line names similar-patches for the rebase count' src/tidy.rs \
  '(r, 0) => format!(" ({r} of them rebase-identity)"),' '(r, 0) => format!(" ({r} of them similar-patches)"),' \
  tidy_rebase_plan the_text_names_the_proof_in_one_line_and_gives_no_command
m TR22 'a strong local proof still asks the pull request' src/tidy.rs \
  'if local_strong || !judged(row.bp.proof) {' 'if !judged(row.bp.proof) {' \
  tidy_rebase_plan a_strong_local_proof_and_a_head_proof_do_not_ask_the_api
m TR23 'a proof that needs no pull request still asks it' src/tidy.rs \
  'if local_strong || !judged(row.bp.proof) {' 'if local_strong {' \
  tidy_rebase_plan a_strong_local_proof_and_a_head_proof_do_not_ask_the_api
m TR24 'the similarity stage subtracts from the oldest author time without a guard' src/tidy.rs \
  'Some(oldest.saturating_sub(lim.window_secs)),' 'Some(oldest - lim.window_secs),' \
  tidy_rebase_plan author_times_at_the_edge_of_the_range_do_not_crash_the_plan
m TR25 'a rate limit answer is a request failure' src/tidy.rs \
  'Err(_) if c.rate_limited() => NotRead(ApiReason::RateLimited),' 'Err(_) if false => NotRead(ApiReason::RateLimited),' \
  tidy_rebase_plan without_a_usable_pull_request_the_plan_falls_back_to_the_local_evidence_and_says_why
m TR26 'a full page of commits is a request failure' src/tidy.rs \
  'Ok(PrCommitsRead::TooMany) => NotRead(ApiReason::TooManyCommits),' 'Ok(PrCommitsRead::TooMany) => NotRead(ApiReason::RequestFailed),' \
  tidy_rebase_plan without_a_usable_pull_request_the_plan_falls_back_to_the_local_evidence_and_says_why
m TR27 'a full page of commits is read as commits (limit one too high)' src/github.rs \
  'if items.len() >= PAGE {' 'if items.len() > PAGE {' \
  tidy_rebase_api a_full_page_of_commits_is_too_many
m TR28 'merged_at of the pull list is not read' src/github.rs \
  'merged_at: p["merged_at"].as_str().and_then(parse_utc),' 'merged_at: None,' \
  tidy_rebase_api the_pull_list_carries_the_merge_commit_the_merge_time_and_the_title
m TR29 'the merge commit of the pull list is not read' src/github.rs \
  'merge_commit_sha: p["merge_commit_sha"].as_str().unwrap_or("").to_string(),' 'merge_commit_sha: String::new(),' \
  tidy_rebase_api the_pull_list_carries_the_merge_commit_the_merge_time_and_the_title
m TR30 'parse_utc has no 400-year rule' src/github.rs \
  'let leap = (y % 4 == 0 && y % 100 != 0) || y % 400 == 0;' 'let leap = y % 4 == 0 && y % 100 != 0;' \
  tidy_rebase_api utc_times_are_read_as_seconds_since_the_epoch
m TR31 'parse_utc has no 100-year rule' src/github.rs \
  'let leap = (y % 4 == 0 && y % 100 != 0) || y % 400 == 0;' 'let leap = y % 4 == 0;' \
  tidy_rebase_api utc_times_are_read_as_seconds_since_the_epoch
m TR32 'the stop of the walk is not reported as missing' src/tidy.rs \
  'Stop::Missing(id) => ("missing", Value::String(id.to_string())),' 'Stop::Missing(_) => ("root", Value::Null),' \
  tidy_rebase_plan a_shallow_clone_gives_a_plan_and_the_stage_says_where_the_walk_ended
m TR33 'the remaining list has no files' src/tidy.rs \
  '"files": m.files, "files_total"' '"files": Vec::<String>::new(), "files_total"' \
  tidy_rebase_plan the_json_has_the_rebase_object_landed_via_and_the_summary
m TR34 'the API state is always not-read' src/tidy.rs \
  'if a.not_read.is_none() { "read" } else { "not-read" }' '"not-read"' \
  tidy_rebase_plan a_pull_request_that_was_rebased_gives_rebase_by_the_pr_api
m TR35 'a rebase-identity branch is counted as keep' src/tidy.rs \
  'row.bp.proof = tidy_rebase::RB_CLASS;' 'row.bp.proof = "unmerged";' \
  tidy_rebase_plan a_rebase_merge_with_a_conflict_resolution_is_rebase_identity_and_review
m TR36 'the pull request of another branch is used' src/tidy.rs \
  '.and_then(|n| prs.iter().find(|p| p.number == n))' '.and_then(|_| prs.first())' \
  tidy_rebase_plan a_pull_request_that_was_rebased_gives_rebase_by_the_pr_api
m TR37 'the stage runs although it is off' src/tidy.rs \
  'if opts.rebase {
        let mut stage' 'if true {
        let mut stage' \
  tidy_rebase_plan the_stage_off_gives_the_plan_without_it
m TR38 'branch_side takes the newest ready commit first' src/tidy_rebase.rs \
  '.map(|(id, _)| std::cmp::Reverse((set[id].key.secs, *id)))' '.map(|(id, _)| std::cmp::Reverse((-set[id].key.secs, *id)))' \
  tidy_rebase the_branch_side_puts_parents_before_children_even_with_equal_times
m TR39 'patch_hits_with does not ask the lookup for the chain' src/tidy_rebase.rs \
  'if let Some(k) = key(h.id)? {' 'if let Some(k) = key(h.id).ok().flatten().filter(|_| false) {' \
  tidy_rebase patch_hits_with_takes_the_keys_from_the_lookup_it_is_given

m TR40 'the window rule subtracts without a guard' src/tidy_similarity.rs \
  'candidate_time >= commit_time.saturating_sub(window_secs)' 'candidate_time >= commit_time - window_secs' \
  tidy_rebase_plan author_times_at_the_edge_of_the_range_do_not_crash_the_plan

m TR41 'a merge by the pull request is strong although the branch moved on' src/tidy_rebase.rs \
  'rep.strength = whole_or_hint;
        return Ok(rep);' 'rep.strength = Strength::Strong;
        return Ok(rep);' \
  tidy_rebase_plan a_pull_request_with_a_merge_commit_gives_merge_by_two_parents
m TR42 'a squash by the pull request is strong although the branch moved on' src/tidy_rebase.rs \
  'rep.kind = LandedKind::Squash;
        rep.strength = whole_or_hint;' 'rep.kind = LandedKind::Squash;
        rep.strength = Strength::Strong;' \
  tidy_rebase_plan a_pull_request_with_a_squash_commit_gives_squash_by_the_merge_time
m TR43 'the branch always counts as the whole pull request' src/tidy_rebase.rs \
  'let whole = crate::repo::is_ancestor(repo, tip, pr.head)?;' 'let whole = true;' \
  tidy_rebase a_merge_or_squash_is_strong_only_when_the_branch_has_nothing_after_the_pull_request_head
m TR44 'the request limit is reported as a rate limit' src/tidy.rs \
  'return NotRead(ApiReason::RequestLimit);' 'return NotRead(ApiReason::RateLimited);' \
  tidy_rebase_plan at_most_thirty_requests_are_made_per_repository
m TR45 'the reason evaluate-failed has another name' src/tidy_rebase.rs \
  'ApiReason::EvaluateFailed => "evaluate-failed",' 'ApiReason::EvaluateFailed => "request-failed",' \
  tidy_rebase the_reasons_of_a_failed_or_skipped_mapping_have_names
m TR46 'the reason request-limit has another name' src/tidy_rebase.rs \
  'ApiReason::RequestLimit => "request-limit",' 'ApiReason::RequestLimit => "rate-limited",' \
  tidy_rebase the_reasons_of_a_failed_or_skipped_mapping_have_names
m TR47 'offline is reported as no-token' src/tidy.rs \
  '    } else if offline {
        ApiReason::Offline' '    } else if offline {
        ApiReason::NoToken' \
  tidy_rebase_plan without_a_client_the_reason_says_offline_or_no_token
m TR48 'apply plans with the rebase stage' src/cli.rs \
  '        similarity: None,
        rebase: false,' '        similarity: None,
        rebase: true,' \
  tidy_rebase_plan apply_plans_without_the_rebase_stage
m TR49 'the default options switch the rebase stage off' src/tidy.rs \
  '            similarity: Some(Similarity::default()),
            rebase: true,' '            similarity: Some(Similarity::default()),
            rebase: false,' \
  tidy_rebase_plan the_default_options_run_both_stages
m TR50 'the text names landed via unknown too' src/tidy.rs \
  'if l.kind != LandedKind::Unknown
' 'if true
' \
  tidy_rebase_plan the_text_names_the_proof_in_one_line_and_gives_no_command
m TR51 'the two counts of the summary line are swapped' src/tidy.rs \
  '(r, s) => format!(" ({r} of them rebase-identity, {s} similar-patches)"),' '(r, s) => format!(" ({s} of them rebase-identity, {r} similar-patches)"),' \
  tidy_rebase_plan a_mix_of_equal_patches_rebase_identity_and_similar_patches
m TR52 'fetch_proof does not hand the client to the stage' src/tidy.rs \
  'api: PrApi::Client(c),' 'api: PrApi::Off(ApiReason::NoToken),' \
  tidy_rebase_plan the_client_of_fetch_proof_reaches_the_stage

killed=0 bad=0
for i in "${!IDS[@]}"; do
  id=${IDS[$i]}
  if [ -n "${ONLY// /}" ] && [[ "$ONLY" != *" $id "* ]]; then continue; fi
  rm -rf "$WORK"; mkdir -p "$WORK"
  (cd "$SRC" && tar --exclude=target -cf - .) | (cd "$WORK" && tar -xf -)
  # a copy keeps old modification times: cargo would take a restored file for unchanged, so touch every file
  (cd "$WORK" && find . -type f -exec touch {} +)
  f=$WORK/${FILES[$i]}
  # perl instead of bash string replacement: that is quadratic and hangs under macOS bash 3.2 on large files
  old=${OLDS[$i]}
  count=$(OLD="$old" perl -0777 -ne '$o=$ENV{OLD}; $c=0; $p=0; while (($p=index($_,$o,$p))>=0) { $c++; $p+=length($o); } print $c;' "$f")
  if [ "$count" != 1 ]; then echo "$id BAD MUTATION (pattern occurs $count times in ${FILES[$i]})"; bad=$((bad+1)); continue; fi
  OLD="$old" NEW="${NEWS[$i]}" perl -0777 -pi -e '$o=$ENV{OLD}; $n=$ENV{NEW}; $p=index($_,$o); substr($_,$p,length($o))=$n;' "$f"
  tgt=${TARGETS[$i]} tst=${TESTS[$i]}
  if [ "$tgt" = lib ]; then
    # unit test inside src/: the test name is its module path
    out=$(cd "$WORK" && cargo test --lib -- --exact "$tst" 2>&1)
  else
    [ -f "$WORK/tests/$tgt.rs" ] || { tst="$tgt::$tst"; tgt=it; }
    out=$(cd "$WORK" && cargo test --test "$tgt" -- --exact "$tst" 2>&1)
  fi
  if grep -q "could not compile" <<<"$out"; then
    echo "$id COMPILE ERROR: ${DESCS[$i]}"; bad=$((bad+1))
  elif grep -q "test $tst ... FAILED" <<<"$out" && grep -q "panicked at" <<<"$out"; then
    why=$(grep -A2 "panicked at" <<<"$out" | sed -n '2,3p' | tr '\n' ' ' | cut -c1-150)
    echo "$id killed by $tgt::$tst [$why]: ${DESCS[$i]}"; killed=$((killed+1))
  else
    echo "$id SURVIVED: ${DESCS[$i]}"; bad=$((bad+1))
  fi
done
echo "$killed killed, $bad not killed"
[ "$bad" = 0 ]
