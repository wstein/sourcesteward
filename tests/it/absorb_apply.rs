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
