//! `stw absorb --apply`: the flag is known, a wrong checksum writes nothing, a matching one
//! snapshots the op log and `stw oplog undo --keep` puts the branch back.
use std::fs;

use super::absorb_fx::Fx;

#[test]
fn apply_is_known_and_the_plan_stays_a_dry_run() {
    let fx = Fx::new();
    fx.commit1("a.txt", "one\n", "feat: one");
    fx.stage1("a.txt", "one\ntwo\n");
    let before = fx.g(&["rev-parse", "HEAD"]);
    let plan = fx.plan(&[]);
    assert_eq!(plan["mode"], "plan");
    assert_eq!(plan["effects"]["writes"].as_array().unwrap().len(), 0);
    assert_eq!(fx.g(&["rev-parse", "HEAD"]), before);
    fx.error("plan-sum-required", &["--apply"]);
    assert_eq!(fx.g(&["rev-parse", "HEAD"]), before);
    assert!(
        fx.g(&["for-each-ref", "--format=%(refname)", "refs/stw/backup"])
            .is_empty(),
        "a missing checksum writes no backup ref"
    );
}

#[test]
fn a_wrong_checksum_writes_nothing() {
    let fx = Fx::new();
    fx.commit1("a.txt", "one\n", "feat: one");
    fx.stage1("a.txt", "one\ntwo\n");
    let before = fx.g(&["rev-parse", "HEAD"]);
    let sum = "a".repeat(64);
    fx.error("plan-sum-mismatch", &["--apply", "--plan-sum", &sum]);
    assert_eq!(fx.g(&["rev-parse", "HEAD"]), before);
    assert!(fx
        .g(&["for-each-ref", "--format=%(refname)", "refs/stw/backup"])
        .is_empty());
    let log = fx.r.join(".git/stw/oplog/refs.log");
    assert!(!log.exists(), "a mismatch writes no op log line");
}

#[test]
fn apply_snapshots_the_oplog_and_undo_restores_the_branch() {
    let fx = Fx::new();
    let original = "one\n";
    fx.commit1("a.txt", original, "feat: one");
    let before = fx.g(&["rev-parse", "HEAD"]);
    fx.stage1("a.txt", "one\ntwo\n");
    let plan = fx.plan(&[]);
    let sum = plan["plan_sum"].as_str().unwrap().to_string();
    let o = fx.run(&["--json", "--apply", "--plan-sum", &sum]);
    assert_eq!(o.rc, 0, "stdout {}\nstderr {}", o.out, o.err);
    let doc: serde_json::Value = serde_json::from_str(&o.out).unwrap();
    assert_eq!(doc["mode"], "apply");
    assert_eq!(doc["old_head"], before);
    assert_ne!(doc["new_head"], before);
    let backup = doc["backup_ref"].as_str().unwrap();
    assert!(backup.starts_with("refs/stw/backup/"), "{backup}");
    assert_eq!(fx.g(&["rev-parse", backup]), before);
    let log = fs::read_to_string(fx.r.join(".git/stw/oplog/refs.log")).unwrap();
    assert!(log.contains(backup), "{log}");
    assert_eq!(
        fx.g(&["rev-parse", "HEAD"]),
        doc["new_head"].as_str().unwrap()
    );
    assert_eq!(
        fs::read_to_string(fx.r.join("a.txt")).unwrap(),
        "one\ntwo\n"
    );

    let undo = fx.stw(&[
        "oplog",
        "undo",
        "refs/heads/main",
        "--to",
        &before,
        "--expect",
        doc["new_head"].as_str().unwrap(),
        "--keep",
        "--yes",
    ]);
    assert_eq!(undo.rc, 0, "stdout {}\nstderr {}", undo.out, undo.err);
    assert_eq!(fx.g(&["rev-parse", "HEAD"]), before);
    assert_eq!(fs::read_to_string(fx.r.join("a.txt")).unwrap(), original);
}

#[test]
fn apply_keeps_an_unstaged_worktree_and_clears_the_index() {
    let fx = Fx::new();
    fx.commit1("a.txt", "A\n", "feat: one");
    fx.stage1("a.txt", "B\n");
    fx.write("a.txt", "C\n");
    let before = fs::read(fx.r.join("a.txt")).unwrap();
    let plan = fx.plan(&[]);
    let sum = plan["plan_sum"].as_str().unwrap().to_string();
    let o = fx.run(&["--json", "--apply", "--plan-sum", &sum]);
    assert_eq!(o.rc, 0, "stdout {}\nstderr {}", o.out, o.err);
    let doc: serde_json::Value = serde_json::from_str(&o.out).unwrap();
    assert_eq!(
        fx.g(&["rev-parse", "HEAD:a.txt"]),
        fx.g(&["rev-parse", ":a.txt"])
    );
    assert_eq!(fx.g(&["show", "HEAD:a.txt"]), "B");
    assert!(fx.g(&["diff", "--cached", "--name-only"]).is_empty());
    assert_eq!(fs::read(fx.r.join("a.txt")).unwrap(), before);
    assert_eq!(fs::read_to_string(fx.r.join("a.txt")).unwrap(), "C\n");
    assert!(!doc["way_back"].as_str().unwrap().contains("--keep"));
}

#[test]
fn a_file_that_was_not_absorbed_stays_staged() {
    let fx = Fx::new();
    fx.commit1("a.txt", "A\n", "feat: one");
    fx.stage(&[("a.txt", Some("B\n")), ("b.txt", Some("new\n"))]);
    let plan = fx.plan(&[]);
    assert!(
        plan["files_skipped"]
            .as_array()
            .unwrap()
            .iter()
            .any(|f| f["path"] == "b.txt"),
        "{}",
        plan
    );
    let sum = plan["plan_sum"].as_str().unwrap().to_string();
    let o = fx.run(&["--json", "--apply", "--plan-sum", &sum]);
    assert_eq!(o.rc, 0, "stdout {}\nstderr {}", o.out, o.err);
    assert_eq!(fx.g(&["show", "HEAD:a.txt"]), "B");
    let cached = fx.g(&["diff", "--cached", "--name-only"]);
    assert!(cached.contains("b.txt"), "{cached}");
    assert!(!cached.contains("a.txt"), "{cached}");
    assert_eq!(fs::read_to_string(fx.r.join("b.txt")).unwrap(), "new\n");
}

#[test]
fn apply_leaves_a_skipped_hunk_in_the_same_file_staged() {
    let fx = Fx::new();
    let base: String = (1..=20).map(|n| format!("line {n}\n")).collect();
    fx.commit1("a.txt", &base, "base");
    fx.g(&["branch", "keep"]);
    let topic = base.replacen("line 1\n", "topic\n", 1);
    fx.commit1("a.txt", &topic, "topic");
    let expected = topic.replacen("topic\n", "corrected\n", 1);
    let staged = expected.replacen("line 12\n", "extra\nline 12\n", 1);
    fx.stage1("a.txt", &staged);
    fx.write("a.txt", &(staged.clone() + "unstaged\n"));
    let index = fx.r.join(".git/index");
    let index_before = fs::read(&index).unwrap();
    let worktree_before = fs::read(fx.r.join("a.txt")).unwrap();
    let plan = fx.plan(&[]);
    assert_eq!(plan["summary"]["absorbed"], 1, "{plan}");
    assert_eq!(plan["summary"]["skipped"], 1, "{plan}");
    let sum = plan["plan_sum"].as_str().unwrap();
    let o = fx.run(&["--json", "--apply", "--plan-sum", sum]);
    assert_eq!(o.rc, 0, "stdout {}\nstderr {}", o.out, o.err);
    assert_eq!(fx.g(&["show", "HEAD:a.txt"]), expected.trim_end());
    assert_eq!(fx.g(&["show", ":a.txt"]), staged.trim_end());
    assert_eq!(fs::read(index).unwrap(), index_before);
    assert_eq!(fs::read(fx.r.join("a.txt")).unwrap(), worktree_before);
}

#[test]
fn apply_refuses_a_locked_index() {
    let fx = Fx::new();
    fx.commit1("a.txt", "one\n", "feat: one");
    fx.stage1("a.txt", "one\ntwo\n");
    let before = fx.g(&["rev-parse", "HEAD"]);
    let plan = fx.plan(&[]);
    let lock = fx.r.join(".git/index.lock");
    fs::write(&lock, b"other writer").unwrap();
    fx.error(
        "index-lock",
        &["--apply", "--plan-sum", plan["plan_sum"].as_str().unwrap()],
    );
    assert_eq!(fx.g(&["rev-parse", "HEAD"]), before);
    assert_eq!(fs::read(&lock).unwrap(), b"other writer");
}

#[test]
fn apply_patches_each_target_at_its_historical_line() {
    let fx = Fx::new();
    let base: String = (1..=20).map(|n| format!("line {n}\n")).collect();
    fx.commit1("a.txt", &base, "base");
    fx.g(&["branch", "keep"]);
    let first = base.replacen("line 3\n", "first\n", 1);
    fx.commit1("a.txt", &first, "first");
    let second = "prefix\n".to_owned() + &first.replacen("line 17\n", "second\n", 1);
    fx.commit1("a.txt", &second, "second");
    let staged = second
        .replacen("first\n", "one\n", 1)
        .replacen("second\n", "two\n", 1);
    fx.stage1("a.txt", &staged);
    let plan = fx.plan(&[]);
    assert_eq!(plan["summary"]["absorbed"], 2, "{plan}");
    assert_eq!(plan["summary"]["fixups"], 2, "{plan}");
    let sum = plan["plan_sum"].as_str().unwrap();
    let o = fx.run(&["--json", "--apply", "--plan-sum", sum]);
    assert_eq!(o.rc, 0, "stdout {}\nstderr {}", o.out, o.err);
    assert_eq!(fx.g(&["show", "HEAD:a.txt"]), staged.trim_end());
    assert_eq!(
        fx.g(&["show", "HEAD^:a.txt"]),
        first.replacen("first\n", "one\n", 1).trim_end()
    );
}

#[test]
fn apply_distinguishes_an_unreadable_path_from_an_empty_file() {
    let fx = Fx::new();
    fx.commit1("a.txt", "one\n", "feat: one");
    fx.stage1("a.txt", "");
    let before = fx.g(&["rev-parse", "HEAD"]);
    let plan = fx.plan(&[]);
    let file = fx.r.join("a.txt");
    fs::remove_file(&file).unwrap();
    fs::create_dir(&file).unwrap();
    fx.error(
        "worktree-unreadable",
        &["--apply", "--plan-sum", plan["plan_sum"].as_str().unwrap()],
    );
    assert_eq!(fx.g(&["rev-parse", "HEAD"]), before);
    assert!(file.is_dir());
}
