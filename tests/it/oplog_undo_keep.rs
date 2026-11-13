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
    assert!(r.text.contains("--keep"), "{}", r.text);
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

#[test]
fn an_untracked_file_in_the_way_makes_git_refuse_and_the_branch_stays() {
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
    fx.write_plain("g", "untracked and precious\n");
    let r = run(&fx, &keep_args(&b.to_string(), &c.to_string()));
    assert_eq!(r.code, 1, "{}{}", r.text, r.err);
    assert!(
        r.err.contains("git") && r.err.contains("not changed"),
        "{}{}",
        r.text,
        r.err
    );
    assert_eq!(fx.tip("work"), c, "the branch stays");
    assert_eq!(fx.read("g").as_deref(), Some("untracked and precious\n"));
    // the backup of the run stays, as after a lost compare-and-swap
    assert_eq!(refname_value(&fx, "refs/stw/backup/1/1"), Some(c));
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
