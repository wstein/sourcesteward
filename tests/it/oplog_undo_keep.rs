//! `stw oplog undo REF --to OLD --expect NEW --keep --yes`: the checked-out branch of the current worktree is moved with
//! `git reset --keep` (docs/oplog-format.md, 8a). The binary runs with git on its PATH here (the only git program of
//! `oplog`); fixtures are built with gix, the merge conflict and the state checks use the installed git.
use std::process::Command;

use crate::common::*;
use crate::oplog_undo::{counter_text, refname_value, refs_log, stw_refs, undo_fx};
use gix::ObjectId;
use serde_json::Value;

const Z: &str = "0000000000000000000000000000000000000000";

struct Run {
    code: i32,
    text: String,
    err: String,
}

fn run_in(dir: &std::path::Path, path_env: &str, args: &[&str]) -> Run {
    assert!(dir.is_absolute(), "unconfined");
    let home = tempfile::tempdir().unwrap();
    let o = Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_clear()
        .env("PATH", path_env)
        .env("HOME", home.path())
        .current_dir(dir)
        .arg("oplog")
        .args(args)
        .output()
        .unwrap();
    Run {
        code: o.status.code().unwrap_or(-1),
        text: String::from_utf8_lossy(&o.stdout).into_owned(),
        err: String::from_utf8_lossy(&o.stderr).into_owned(),
    }
}

/// With git on the PATH.
fn run(fx: &Fx, args: &[&str]) -> Run {
    run_in(&fx.path, "/usr/bin:/bin", args)
}

fn json(fx: &Fx, args: &[&str]) -> Value {
    let mut a: Vec<&str> = args.to_vec();
    a.push("--json");
    let r = run(fx, &a);
    serde_json::from_str(&r.text)
        .unwrap_or_else(|e| panic!("not JSON ({e}): {}\nstderr: {}", r.text, r.err))
}

fn git(dir: &std::path::Path, args: &[&str]) -> (i32, String) {
    assert!(dir.is_absolute(), "unconfined");
    let home = tempfile::tempdir().unwrap();
    let o = Command::new("/usr/bin/git")
        .env_clear()
        .env("PATH", "/usr/bin:/bin")
        .env("HOME", home.path())
        .current_dir(dir)
        .args(args)
        .output()
        .unwrap();
    (
        o.status.code().unwrap_or(-1),
        String::from_utf8_lossy(&o.stdout).into_owned() + &String::from_utf8_lossy(&o.stderr),
    )
}

fn keep_args<'a>(to: &'a str, expect: &'a str) -> Vec<&'a str> {
    vec![
        "undo", "work", "--to", to, "--expect", expect, "--keep", "--yes",
    ]
}

/// `work` (c3) is checked out and its files and index are written: clean.
fn keep_fx() -> (Fx, [ObjectId; 3]) {
    let (fx, ids) = undo_fx();
    fx.set_head("work");
    fx.checkout_head();
    (fx, ids)
}

fn porcelain(fx: &Fx) -> String {
    git(
        &fx.path,
        &["status", "--porcelain=v1", "--untracked-files=no"],
    )
    .1
}

fn set_reflog(fx: &Fx, name: &str, rows: &[(String, ObjectId, &str)]) {
    let mut s = String::new();
    for (i, (o, n, m)) in rows.iter().enumerate() {
        s += &format!(
            "{o} {n} T <t@example.com> {} +0000\t{m}\n",
            1_700_000_000 + 100 * i as i64
        );
    }
    fx.write(&format!(".git/logs/refs/heads/{name}"), &s);
}

#[test]
fn keep_moves_branch_index_and_worktree_and_writes_backup_and_log_lines() {
    let (fx, [_, c2, c3]) = keep_fx();
    assert_eq!(porcelain(&fx), "", "the fixture is clean");
    fx.write_plain("untracked.txt", "mine\n");
    let r = run(&fx, &keep_args(&c2.to_string(), &c3.to_string()));
    assert_eq!(r.code, 0, "{}{}", r.text, r.err);
    assert_eq!(fx.tip("work"), c2);
    assert_eq!(fx.read("f").as_deref(), Some("b\n"), "the work tree moved");
    assert_eq!(porcelain(&fx), "", "index and work tree match the new tip");
    assert_eq!(
        fx.read(".git/HEAD").as_deref(),
        Some("ref: refs/heads/work\n")
    );
    assert_eq!(fx.read("untracked.txt").as_deref(), Some("mine\n"));
    // backup ref, counter, op log lines
    assert_eq!(refname_value(&fx, "refs/stw/backup/1/1"), Some(c3));
    assert!(counter_text(&fx).starts_with("1 "));
    let log = refs_log(&fx);
    let last = log.last().unwrap();
    assert_eq!(last[3], "refs/heads/work", "{log:?}");
    assert_eq!(last[1], c3.to_string());
    assert_eq!(last[2], c2.to_string());
    assert_eq!(last[4], "stw", "{log:?}");
    // text: names the way back with --keep, and what moved
    assert!(
        r.text.contains(&format!(
            "way back:   stw oplog undo refs/heads/work --to {c3} --expect {c2} --keep --yes"
        )),
        "{}",
        r.text
    );
    assert!(r.text.contains("git reset --keep"), "{}", r.text);
    assert!(!r.text.contains("Only the ref was moved"), "{}", r.text);
}

#[test]
fn keep_json_says_keep_via_git_and_the_way_back_has_keep() {
    let (fx, [_, c2, c3]) = keep_fx();
    let v = json(&fx, &keep_args(&c2.to_string(), &c3.to_string()));
    assert_eq!(v["executed"], true, "{v}");
    assert_eq!(v["keep"], true);
    assert_eq!(v["via"], "git");
    assert_eq!(v["backup_ref"], "refs/stw/backup/1/1");
    assert_eq!(
        v["way_back"],
        format!("stw oplog undo refs/heads/work --to {c3} --expect {c2} --keep --yes")
    );
    // the way back works and restores the files
    let wb = run(&fx, &keep_args(&c3.to_string(), &c2.to_string()));
    assert_eq!(wb.code, 0, "{}{}", wb.text, wb.err);
    assert_eq!(fx.tip("work"), c3);
    assert_eq!(fx.read("f").as_deref(), Some("c\n"));
    assert_eq!(porcelain(&fx), "");
}

#[test]
fn a_plain_undo_json_says_keep_false() {
    let (fx, [_, c2, c3]) = undo_fx();
    let v = json(
        &fx,
        &[
            "undo",
            "work",
            "--to",
            &c2.to_string(),
            "--expect",
            &c3.to_string(),
            "--yes",
        ],
    );
    assert_eq!(v["keep"], false, "{v}");
    assert!(v["via"].is_null(), "{v}");
}

#[test]
fn without_yes_keep_is_a_usage_error_and_undo_plan_keep_names_the_apply_command() {
    let (fx, [_, c2, c3]) = keep_fx();
    let before = tree_snapshot(&fx.path);
    let (s2, s3) = (c2.to_string(), c3.to_string());
    let r = run(
        &fx,
        &["undo", "work", "--to", &s2, "--expect", &s3, "--keep"],
    );
    assert_eq!(r.code, 2, "{}{}", r.text, r.err);
    assert!(r.err.contains("--yes"), "{}", r.err);
    let p = run(&fx, &["undo-plan", "work", "--keep"]);
    assert_eq!(p.code, 0, "{}{}", p.text, p.err);
    assert!(
        p.text.contains(&format!(
            "Apply with: stw oplog undo refs/heads/work --to {s2} --expect {s3} --keep --yes"
        )),
        "{}",
        p.text
    );
    let v = json(&fx, &["undo-plan", "work", "--keep"]);
    assert_eq!(
        v["apply"],
        format!("stw oplog undo refs/heads/work --to {s2} --expect {s3} --keep --yes")
    );
    assert_eq!(v["keep"], true);
    assert!(before == tree_snapshot(&fx.path), "a plan wrote");
}

#[test]
fn without_keep_a_checked_out_branch_is_still_refused_and_the_text_names_keep() {
    let (fx, [_, c2, c3]) = keep_fx();
    let before = tree_snapshot(&fx.path);
    let r = run(
        &fx,
        &[
            "undo",
            "work",
            "--to",
            &c2.to_string(),
            "--expect",
            &c3.to_string(),
            "--yes",
        ],
    );
    assert_eq!(r.code, 1, "{}{}", r.text, r.err);
    assert!(r.text.contains("checked out"), "{}", r.text);
    assert!(r.text.contains("add --keep"), "{}", r.text);
    assert!(before == tree_snapshot(&fx.path));
}

fn refused_and_untouched(fx: &Fx, r: &Run, words: &[&str], before: &Before) {
    assert_eq!(r.code, 1, "{}{}", r.text, r.err);
    for w in words {
        assert!(r.text.contains(w), "{w}: {}", r.text);
    }
    assert!(
        stw_refs(fx).is_empty(),
        "a refusal wrote refs: {:?}",
        stw_refs(fx)
    );
    assert!(
        before.0 == tree_snapshot(&fx.path),
        "the repository changed"
    );
}

struct Before(std::collections::BTreeMap<std::path::PathBuf, Option<Vec<u8>>>);

fn before(fx: &Fx) -> Before {
    Before(tree_snapshot(&fx.path))
}

#[test]
fn a_staged_change_refuses_and_nothing_is_written() {
    let (fx, [_, c2, c3]) = keep_fx();
    fx.write_plain("f", "staged\n");
    fx.stage("f");
    let b = before(&fx);
    let r = run(&fx, &keep_args(&c2.to_string(), &c3.to_string()));
    refused_and_untouched(&fx, &r, &["index has staged changes"], &b);
    assert_eq!(fx.tip("work"), c3);
}

#[test]
fn an_unstaged_change_refuses_and_nothing_is_written() {
    let (fx, [_, c2, c3]) = keep_fx();
    fx.write_plain("f", "edited, not staged\n");
    let b = before(&fx);
    let r = run(&fx, &keep_args(&c2.to_string(), &c3.to_string()));
    refused_and_untouched(&fx, &r, &["work tree has unstaged changes"], &b);
    assert_eq!(fx.read("f").as_deref(), Some("edited, not staged\n"));
}

#[test]
fn a_merge_in_progress_and_unmerged_paths_refuse() {
    let (fx, [_, b, c3]) = keep_fx();
    let other = fx.commit(Some(b), &[("f", Some("x\n"))], "other");
    fx.branch("other", other);
    let (code, out) = git(&fx.path, &["merge", "--no-edit", "other"]);
    assert_eq!(code, 1, "{out}");
    assert!(fx.exists(".git/MERGE_HEAD"), "{out}");
    let c2 = b.to_string();
    let before_m = before(&fx);
    let r = run(&fx, &keep_args(&c2, &c3.to_string()));
    refused_and_untouched(&fx, &r, &["operation in progress", "MERGE_HEAD"], &before_m);
    assert!(r.text.contains("unmerged"), "{}", r.text);
    // the marker gone, the unmerged index entry alone refuses
    std::fs::remove_file(fx.path.join(".git/MERGE_HEAD")).unwrap();
    let before_u = before(&fx);
    let r = run(&fx, &keep_args(&c2, &c3.to_string()));
    refused_and_untouched(&fx, &r, &["unmerged"], &before_u);
    assert!(!r.text.contains("operation in progress"), "{}", r.text);
    assert_eq!(fx.tip("work"), c3);
}

#[test]
fn untracked_files_do_not_refuse_and_stay() {
    let (fx, [_, c2, c3]) = keep_fx();
    fx.write_plain("note.txt", "x\n");
    fx.write_plain("dir/deep.txt", "y\n");
    let r = run(&fx, &keep_args(&c2.to_string(), &c3.to_string()));
    assert_eq!(r.code, 0, "{}{}", r.text, r.err);
    assert_eq!(fx.read("note.txt").as_deref(), Some("x\n"));
    assert_eq!(fx.read("dir/deep.txt").as_deref(), Some("y\n"));
}

#[test]
fn a_branch_checked_out_in_another_worktree_is_refused() {
    let (fx, [_, c2, c3]) = undo_fx();
    // main checkout on `main`, `work` checked out in a linked worktree
    fx.checkout_head();
    let wt = real_worktree(&fx, "w2", "work");
    let b = before(&fx);
    let r = run(&fx, &keep_args(&c2.to_string(), &c3.to_string()));
    refused_and_untouched(&fx, &r, &["another worktree", "wt-w2"], &b);
    assert_eq!(fx.tip("work"), c3);
    assert!(wt.join("f").exists());
}

#[test]
fn a_branch_that_a_second_worktree_also_holds_is_refused() {
    let (fx, [_, c2, c3]) = keep_fx();
    // git forbids it, `--ignore-other-worktrees` and a copied registration do not
    real_worktree(&fx, "w3", "work");
    let b = before(&fx);
    let r = run(&fx, &keep_args(&c2.to_string(), &c3.to_string()));
    refused_and_untouched(&fx, &r, &["another worktree", "wt-w3"], &b);
    assert_eq!(fx.read("f").as_deref(), Some("c\n"));
}

#[test]
fn the_linked_worktree_that_holds_the_branch_is_moved_from_inside() {
    let (fx, [_, c2, c3]) = undo_fx();
    fx.checkout_head();
    let wt = real_worktree(&fx, "w4", "work");
    let main_f = fx.read("f");
    let r = run_in(
        &wt,
        "/usr/bin:/bin",
        &keep_args(&c2.to_string(), &c3.to_string()),
    );
    assert_eq!(r.code, 0, "{}{}", r.text, r.err);
    assert_eq!(fx.tip("work"), c2);
    assert_eq!(
        std::fs::read_to_string(wt.join("f")).unwrap(),
        "b\n",
        "the linked work tree moved"
    );
    assert_eq!(fx.read("f"), main_f, "the main work tree is untouched");
    assert!(!porcelain_of(&wt).contains('M'), "{}", porcelain_of(&wt));
}

fn porcelain_of(dir: &std::path::Path) -> String {
    git(dir, &["status", "--porcelain=v1", "--untracked-files=no"]).1
}

#[test]
fn a_branch_that_is_not_checked_out_here_is_refused_with_keep() {
    let (fx, [_, c2, c3]) = undo_fx();
    fx.checkout_head();
    let b = before(&fx);
    let r = run(&fx, &keep_args(&c2.to_string(), &c3.to_string()));
    refused_and_untouched(&fx, &r, &["not checked out in this worktree"], &b);
    assert_eq!(fx.tip("work"), c3);
}

#[test]
fn a_detached_head_is_refused_with_keep() {
    let (fx, [_, c2, c3]) = undo_fx();
    std::fs::write(fx.path.join(".git/HEAD"), format!("{c3}\n")).unwrap();
    fx.checkout_head();
    let b = before(&fx);
    let r = run(&fx, &keep_args(&c2.to_string(), &c3.to_string()));
    refused_and_untouched(&fx, &r, &["detached"], &b);
    assert_eq!(fx.tip("work"), c3);
}

#[test]
fn a_repository_without_git_refuses_before_any_ref_is_written() {
    let (fx, [_, c2, c3]) = keep_fx();
    let b = before(&fx);
    let r = run_in(
        &fx.path,
        "/nonexistent",
        &keep_args(&c2.to_string(), &c3.to_string()),
    );
    refused_and_untouched(&fx, &r, &["git"], &b);
}

/// A directory with a `git` that prints `version_text` for everything.
fn fake_git(version_text: &str) -> tempfile::TempDir {
    use std::os::unix::fs::PermissionsExt;
    let d = scratch();
    let p = d.path().join("git");
    std::fs::write(&p, format!("#!/bin/sh\necho '{version_text}'\n")).unwrap();
    std::fs::set_permissions(&p, std::fs::Permissions::from_mode(0o755)).unwrap();
    d
}

#[test]
fn a_git_older_than_reset_keep_needs_refuses_before_any_ref_is_written() {
    let (fx, [_, c2, c3]) = keep_fx();
    let g = fake_git("git version 2.30.0");
    let b = before(&fx);
    let path = g.path().to_string_lossy().into_owned();
    let r = run_in(
        &fx.path,
        &path,
        &keep_args(&c2.to_string(), &c3.to_string()),
    );
    refused_and_untouched(&fx, &r, &["2.36", "2.30.0"], &b);
}

#[test]
fn a_git_whose_version_cannot_be_read_refuses_before_any_ref_is_written() {
    let (fx, [_, c2, c3]) = keep_fx();
    let g = fake_git("banana");
    let b = before(&fx);
    let path = g.path().to_string_lossy().into_owned();
    let r = run_in(
        &fx.path,
        &path,
        &keep_args(&c2.to_string(), &c3.to_string()),
    );
    refused_and_untouched(&fx, &r, &["usable git"], &b);
}

#[test]
fn a_gitlink_that_points_elsewhere_refuses() {
    let fx = Fx::new();
    let a = fx.commit(None, &[("f", Some("a\n"))], "first");
    let x = fx.commit_gitlink(a, "sub", a, "sub at a");
    let y = fx.commit_gitlink(x, "sub", x, "sub at x");
    fx.branch("work", y);
    fx.set_head("work");
    set_reflog(
        &fx,
        "work",
        &[
            (Z.to_string(), x, "branch: Created"),
            (x.to_string(), y, "commit: sub moved"),
        ],
    );
    fx.checkout_head();
    let b = before(&fx);
    let r = run(&fx, &keep_args(&x.to_string(), &y.to_string()));
    refused_and_untouched(&fx, &r, &["gitlink", "sub"], &b);
}

#[test]
fn a_gitlink_that_differs_between_the_tips_refuses() {
    let fx = Fx::new();
    let a = fx.commit(None, &[("f", Some("a\n"))], "first");
    let b = fx.commit_gitlink(a, "sub", a, "add sub");
    let c = fx.commit(Some(b), &[("f", Some("c\n"))], "third");
    fx.branch("work", c);
    fx.set_head("work");
    set_reflog(
        &fx,
        "work",
        &[
            (Z.to_string(), a, "branch: Created"),
            (a.to_string(), b, "commit: sub"),
            (b.to_string(), c, "commit: third"),
        ],
    );
    fx.checkout_head();
    let before_g = before(&fx);
    // c -> a drops the gitlink
    let r = run(&fx, &keep_args(&a.to_string(), &c.to_string()));
    refused_and_untouched(&fx, &r, &["gitlink", "sub"], &before_g);
    // c -> b keeps it: allowed
    let r = run(&fx, &keep_args(&b.to_string(), &c.to_string()));
    assert_eq!(r.code, 0, "{}{}", r.text, r.err);
    assert_eq!(fx.tip("work"), b);
    assert_eq!(fx.read("f").as_deref(), Some("a\n"));
}

/// `work` at c (g removed again), reflog a -> b (adds g) -> c.
fn g_fx() -> (Fx, [ObjectId; 3]) {
    let fx = Fx::new();
    let a = fx.commit(None, &[("f", Some("a\n"))], "first");
    let b = fx.commit(Some(a), &[("g", Some("g\n"))], "add g");
    let c = fx.commit(Some(b), &[("g", None)], "remove g");
    fx.branch("work", c);
    fx.set_head("work");
    set_reflog(
        &fx,
        "work",
        &[
            (Z.to_string(), a, "branch: Created"),
            (a.to_string(), b, "commit: add g"),
            (b.to_string(), c, "commit: remove g"),
        ],
    );
    fx.checkout_head();
    (fx, [a, b, c])
}

#[test]
fn an_untracked_file_in_the_way_refuses_before_the_backup() {
    let (fx, [_, b, c]) = g_fx();
    fx.write_plain("g", "untracked and precious\n");
    let before_g = before(&fx);
    let r = run(&fx, &keep_args(&b.to_string(), &c.to_string()));
    refused_and_untouched(&fx, &r, &["untracked or ignored", "g"], &before_g);
    assert_eq!(fx.read("g").as_deref(), Some("untracked and precious\n"));
    let p = run(
        &fx,
        &["undo-plan", "work", "--to", &b.to_string(), "--keep"],
    );
    assert_eq!(p.code, 1, "{}", p.text);
}

#[test]
fn an_ignored_file_in_the_way_refuses_before_the_backup() {
    let (fx, [_, b, c]) = g_fx();
    fx.write_plain(".git/info/exclude", "g\n");
    fx.write_plain("g", "PRECIOUS\n");
    let before_g = before(&fx);
    let r = run(&fx, &keep_args(&b.to_string(), &c.to_string()));
    refused_and_untouched(&fx, &r, &["untracked or ignored", "g"], &before_g);
    assert_eq!(fx.read("g").as_deref(), Some("PRECIOUS\n"));
}

#[test]
fn a_file_where_the_target_needs_a_directory_refuses() {
    let fx = Fx::new();
    let a = fx.commit(None, &[("f", Some("a\n"))], "first");
    let b = fx.commit(Some(a), &[("d/x", Some("x\n"))], "add d/x");
    let c = fx.commit(Some(b), &[("d/x", None)], "remove d/x");
    fx.branch("work", c);
    fx.set_head("work");
    set_reflog(
        &fx,
        "work",
        &[
            (Z.to_string(), a, "branch: Created"),
            (a.to_string(), b, "commit: add"),
            (b.to_string(), c, "commit: remove"),
        ],
    );
    fx.checkout_head();
    fx.write_plain("d", "a file named d\n");
    let before_d = before(&fx);
    let r = run(&fx, &keep_args(&b.to_string(), &c.to_string()));
    refused_and_untouched(&fx, &r, &["untracked or ignored", "d/x"], &before_d);
}

#[test]
fn an_untracked_file_that_is_not_in_the_way_stays_when_the_target_adds_another_path() {
    let (fx, [_, b, c]) = g_fx();
    fx.write_plain("other.txt", "mine\n");
    let r = run(&fx, &keep_args(&b.to_string(), &c.to_string()));
    assert_eq!(r.code, 0, "{}{}", r.text, r.err);
    assert_eq!(fx.read("g").as_deref(), Some("g\n"));
    assert_eq!(fx.read("other.txt").as_deref(), Some("mine\n"));
}

#[test]
fn a_filter_driver_in_the_config_stops_the_runner_and_the_branch_stays() {
    let (fx, [_, c2, c3]) = keep_fx();
    let mut cfg = std::fs::read_to_string(fx.path.join(".git/config")).unwrap();
    cfg.push_str("[filter \"x\"]\n\tclean = cat\n");
    std::fs::write(fx.path.join(".git/config"), cfg).unwrap();
    let r = run(&fx, &keep_args(&c2.to_string(), &c3.to_string()));
    assert_eq!(r.code, 1, "{}{}", r.text, r.err);
    assert!(r.err.contains("filter"), "{}{}", r.text, r.err);
    assert_eq!(fx.tip("work"), c3);
    assert_eq!(fx.read("f").as_deref(), Some("c\n"));
}

#[test]
fn a_ref_that_is_no_local_branch_is_still_a_usage_error_with_keep() {
    let (fx, [_, c2, c3]) = keep_fx();
    fx.set_ref("refs/tags/t", c3);
    let r = run(
        &fx,
        &[
            "undo",
            "refs/tags/t",
            "--to",
            &c2.to_string(),
            "--expect",
            &c3.to_string(),
            "--keep",
            "--yes",
        ],
    );
    assert_eq!(r.code, 2, "{}{}", r.text, r.err);
}

#[test]
fn the_help_names_keep() {
    let (fx, _) = keep_fx();
    let r = run(&fx, &["--help"]);
    assert!(r.text.contains("--expect NEW [--keep] --yes"), "{}", r.text);
    let r = run(&fx, &["undo", "--help"]);
    assert!(r.text.contains("\n      --keep"), "{}", r.text);
    let r = run(&fx, &["undo-plan", "--help"]);
    assert!(r.text.contains("\n      --keep"), "{}", r.text);
}

#[test]
fn core_sparse_checkout_refuses() {
    let (fx, [_, c2, c3]) = keep_fx();
    let mut cfg = std::fs::read_to_string(fx.path.join(".git/config")).unwrap();
    cfg.push_str("[core]\n\tsparseCheckout = true\n");
    std::fs::write(fx.path.join(".git/config"), cfg).unwrap();
    let b = before(&fx);
    let r = run(&fx, &keep_args(&c2.to_string(), &c3.to_string()));
    refused_and_untouched(&fx, &r, &["core.sparseCheckout"], &b);
}

// ---- the window between the checks and git ----------------------------------------------------------------------

fn lib_request(to: ObjectId, expect: ObjectId) -> stw::oplog::undo::Request {
    stw::oplog::undo::Request {
        reference: "work".into(),
        to: to.to_string(),
        expect: expect.to_string(),
        keep: true,
    }
}

#[test]
fn a_branch_that_moves_after_the_checks_is_not_reset_and_the_backup_stays() {
    let (fx, [c1, c2, c3]) = keep_fx();
    let ctx = stw::oplog::Ctx::open(&fx.path).unwrap();
    let e = match stw::oplog::undo::undo_with(&ctx, &lib_request(c2, c3), &mut || {
        fx.branch("work", c1);
    }) {
        Err(e) => e.to_string(),
        Ok(_) => panic!("the update went through"),
    };
    assert!(e.contains("changed since"), "{e}");
    assert!(e.contains("refs/stw/backup/1/1"), "{e}");
    assert_eq!(fx.tip("work"), c1, "the other writer's value stays");
    assert_eq!(
        fx.read("f").as_deref(),
        Some("c\n"),
        "the work tree was not touched"
    );
    assert_eq!(refname_value(&fx, "refs/stw/backup/1/1"), Some(c3));
}

#[test]
fn a_head_that_leaves_the_branch_after_the_checks_is_not_reset() {
    let (fx, [_, c2, c3]) = keep_fx();
    let ctx = stw::oplog::Ctx::open(&fx.path).unwrap();
    let e = match stw::oplog::undo::undo_with(&ctx, &lib_request(c2, c3), &mut || {
        fx.set_head("main");
    }) {
        Err(e) => e.to_string(),
        Ok(_) => panic!("the update went through"),
    };
    assert!(e.contains("HEAD is no longer on the branch"), "{e}");
    assert_eq!(fx.tip("work"), c3);
    assert_eq!(fx.read("f").as_deref(), Some("c\n"));
}

#[test]
fn a_target_that_is_not_40_hex_is_refused_before_git_is_asked() {
    let e = stw::oplog::undo_plan::keep_git_check(&"a".repeat(64)).unwrap_err();
    assert!(e.contains("40-digit"), "{e}");
}

// ---- review findings: version in the plan, index flags, bare repository, git that dies, test gaps ---------------------

#[test]
fn undo_plan_keep_checks_the_git_version_like_undo() {
    let (fx, _) = keep_fx();
    let old = fake_git("git version 2.30.0");
    let p = run_in(
        &fx.path,
        &old.path().to_string_lossy(),
        &["undo-plan", "work", "--keep"],
    );
    assert_eq!(p.code, 1, "{}{}", p.text, p.err);
    assert!(
        p.text.contains("2.36") && p.text.contains("2.30.0"),
        "{}",
        p.text
    );
    assert!(!p.text.contains("all checks ok"), "{}", p.text);
}

#[test]
fn git_2_36_0_is_enough_and_2_35_9_is_not() {
    let (fx, _) = keep_fx();
    let ok = fake_git("git version 2.36.0");
    let p = run_in(
        &fx.path,
        &ok.path().to_string_lossy(),
        &["undo-plan", "work", "--keep"],
    );
    assert_eq!(p.code, 0, "{}{}", p.text, p.err);
    let no = fake_git("git version 2.35.9");
    let p = run_in(
        &fx.path,
        &no.path().to_string_lossy(),
        &["undo-plan", "work", "--keep"],
    );
    assert_eq!(p.code, 1, "{}{}", p.text, p.err);
}

#[test]
fn an_intent_to_add_entry_refuses() {
    let (fx, [_, c2, c3]) = keep_fx();
    fx.write_plain("h", "new\n");
    let (code, out) = git(&fx.path, &["add", "-N", "h"]);
    assert_eq!(code, 0, "{out}");
    let b = before(&fx);
    let r = run(&fx, &keep_args(&c2.to_string(), &c3.to_string()));
    refused_and_untouched(&fx, &r, &["intent-to-add"], &b);
}

#[test]
fn assume_unchanged_and_skip_worktree_entries_refuse() {
    for flag in ["--assume-unchanged", "--skip-worktree"] {
        let (fx, [_, c2, c3]) = keep_fx();
        let (code, out) = git(&fx.path, &["update-index", flag, "f"]);
        assert_eq!(code, 0, "{out}");
        let b = before(&fx);
        let r = run(&fx, &keep_args(&c2.to_string(), &c3.to_string()));
        refused_and_untouched(&fx, &r, &["assume-unchanged"], &b);
    }
}

#[test]
fn a_repository_without_a_work_tree_says_so() {
    let (fx, [_, c2, c3]) = keep_fx();
    let mut cfg = std::fs::read_to_string(fx.path.join(".git/config")).unwrap();
    cfg.push_str("[core]\n\tbare = true\n");
    std::fs::write(fx.path.join(".git/config"), cfg).unwrap();
    let ctx = stw::oplog::Ctx::open(&fx.path.join(".git")).unwrap();
    let plan = stw::oplog::undo_plan::plan(
        &ctx,
        &stw::oplog::undo_plan::Request {
            reference: "work".into(),
            to: Some(c2.to_string()),
            steps: None,
            keep: true,
        },
    )
    .unwrap();
    let text = plan.render_human();
    assert!(text.contains("no work tree"), "{text}");
    assert!(!text.contains("HEAD is on"), "{text}");
    assert!(!plan.executable());
    let _ = c3;
}

#[test]
fn a_git_that_dies_during_the_reset_is_reported_as_possibly_changed() {
    use std::os::unix::fs::PermissionsExt;
    let (fx, [_, c2, c3]) = keep_fx();
    let d = scratch();
    let p = d.path().join("git");
    std::fs::write(
        &p,
        "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 'git version 2.47.0'; else kill -9 $$; fi\n",
    )
    .unwrap();
    std::fs::set_permissions(&p, std::fs::Permissions::from_mode(0o755)).unwrap();
    let r = run_in(
        &fx.path,
        &d.path().to_string_lossy(),
        &keep_args(&c2.to_string(), &c3.to_string()),
    );
    assert_eq!(r.code, 1, "{}{}", r.text, r.err);
    assert!(
        r.err.contains("may be changed in part"),
        "{}{}",
        r.text,
        r.err
    );
    assert!(!r.err.contains("was not changed"), "{}", r.err);
}

fn gitlink_chain(first_is_file: bool) -> (Fx, [ObjectId; 3]) {
    // a: `sub` is a file (or a gitlink), b: the other kind, c: only f changes
    let fx = Fx::new();
    let a0 = fx.commit(None, &[("f", Some("a\n"))], "first");
    let (a, b) = if first_is_file {
        let a = fx.commit(Some(a0), &[("sub", Some("file\n"))], "sub file");
        let b = fx.commit_gitlink(a0, "sub", a0, "sub gitlink");
        (a, b)
    } else {
        let a = fx.commit_gitlink(a0, "sub", a0, "sub gitlink");
        let b = fx.commit(Some(a0), &[("sub", Some("file\n"))], "sub file");
        (a, b)
    };
    let c = fx.commit(Some(b), &[("f", Some("c\n"))], "third");
    fx.branch("work", c);
    fx.set_head("work");
    set_reflog(
        &fx,
        "work",
        &[
            (Z.to_string(), a, "branch: Created"),
            (a.to_string(), b, "commit: sub changes kind"),
            (b.to_string(), c, "commit: third"),
        ],
    );
    (fx, [a, b, c])
}

#[test]
fn a_file_that_becomes_a_gitlink_and_back_refuses() {
    for first_is_file in [true, false] {
        let (fx, [_, b, c]) = gitlink_chain(first_is_file);
        // c -> a: the type of `sub` differs between c (kind of b) and a
        let a = {
            let log = std::fs::read_to_string(fx.path.join(".git/logs/refs/heads/work")).unwrap();
            log.lines()
                .nth(1)
                .unwrap()
                .split(' ')
                .next()
                .unwrap()
                .to_string()
        };
        fx.checkout_head();
        let bf = before(&fx);
        let r = run(&fx, &keep_args(&a, &c.to_string()));
        refused_and_untouched(&fx, &r, &["gitlink", "sub"], &bf);
        let _ = b;
    }
}

fn linked_via_symlink(registered_through_link: bool) {
    let (fx, [_, c2, c3]) = undo_fx();
    fx.checkout_head();
    let outer = scratch();
    let real = outer.path().join("real");
    std::fs::create_dir_all(&real).unwrap();
    let link = outer.path().join("link");
    std::os::unix::fs::symlink(&real, &link).unwrap();
    let wt_reg = if registered_through_link {
        link.join("wt")
    } else {
        real.join("wt")
    };
    let wt = real_worktree_at(&fx, "w5", "work", &wt_reg);
    let open_at = if registered_through_link {
        real.join("wt")
    } else {
        link.join("wt")
    };
    let _ = wt;
    let ctx = stw::oplog::Ctx::open(&open_at).unwrap();
    let out = stw::oplog::undo::undo_with(&ctx, &lib_request(c2, c3), &mut || {}).unwrap();
    assert!(
        matches!(out, stw::oplog::undo::Outcome::Done(_)),
        "a worktree reached through a symbolic link is still this worktree"
    );
}

#[test]
fn a_worktree_registered_through_a_symbolic_link_is_still_this_one() {
    linked_via_symlink(true);
}

#[test]
fn a_worktree_opened_through_a_symbolic_link_is_still_this_one() {
    linked_via_symlink(false);
}

#[test]
fn changed_submodule_contents_do_not_count_as_dirty() {
    let fx = Fx::new();
    let a = fx.commit(None, &[("f", Some("a\n"))], "first");
    let x = fx.commit_gitlink(a, "sub", a, "sub");
    let c = fx.commit(Some(x), &[("f", Some("c\n"))], "third");
    fx.branch("work", c);
    fx.set_head("work");
    set_reflog(
        &fx,
        "work",
        &[
            (Z.to_string(), a, "branch: Created"),
            (a.to_string(), x, "commit: sub"),
            (x.to_string(), c, "commit: third"),
        ],
    );
    fx.checkout_head();
    // a real repository in `sub` whose HEAD is not the gitlink commit
    let sub = fx.path.join("sub");
    std::fs::create_dir_all(&sub).unwrap();
    let (code, out) = git(&sub, &["init", "-q", "-b", "m"]);
    assert_eq!(code, 0, "{out}");
    fx.write_plain(
        "sub/.git/config",
        "[core]\n\trepositoryformatversion = 0\n[user]\n\tname = T\n\temail = t@example.com\n",
    );
    let (code, out) = git(&sub, &["commit", "-q", "--allow-empty", "-m", "own"]);
    assert_eq!(code, 0, "{out}");
    let r = run(&fx, &keep_args(&x.to_string(), &c.to_string()));
    assert_eq!(r.code, 0, "{}{}", r.text, r.err);
    assert_eq!(fx.tip("work"), x);
}

#[test]
fn a_tracked_file_that_the_target_turns_into_a_directory_is_not_in_the_way() {
    let fx = Fx::new();
    let a = fx.commit(
        None,
        &[("f", Some("a\n")), ("d", Some("file\n"))],
        "d is a file",
    );
    let b = fx.commit(
        Some(a),
        &[("d", None), ("d/x", Some("x\n"))],
        "d is a directory",
    );
    let c = fx.commit(
        Some(b),
        &[("d/x", None), ("d", Some("file again\n"))],
        "d is a file again",
    );
    fx.branch("work", c);
    fx.set_head("work");
    set_reflog(
        &fx,
        "work",
        &[
            (Z.to_string(), a, "branch: Created"),
            (a.to_string(), b, "commit: dir"),
            (b.to_string(), c, "commit: file"),
        ],
    );
    fx.checkout_head();
    let r = run(&fx, &keep_args(&b.to_string(), &c.to_string()));
    assert_eq!(r.code, 0, "{}{}", r.text, r.err);
    assert_eq!(fx.read("d/x").as_deref(), Some("x\n"));
}
