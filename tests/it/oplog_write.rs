//! stw writes its own gix ref updates into `refs.log` (context `stw`) and rotates the logs by size
//! (`docs/oplog-format.md`, 3, 6). Fixtures: the repositories of `sync_journal::setup` (real git builds them, stw runs).
use std::fs::OpenOptions;
use std::os::unix::fs::PermissionsExt;
use std::path::{Path, PathBuf};

use crate::sync_journal::{repo, setup, J};

const MIB: usize = 1024 * 1024;

/// `stw oplog ...` in the workspace repository: (exit code, stdout, stderr).
fn oplog_cli(j: &J, args: &[&str]) -> (i32, String, String) {
    let o = std::process::Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_clear()
        .env("PATH", "/nonexistent")
        .env("HOME", &j.w.home)
        .current_dir(&j.ws)
        .arg("oplog")
        .args(args)
        .output()
        .unwrap();
    (
        o.status.code().unwrap_or(-1),
        String::from_utf8_lossy(&o.stdout).into_owned(),
        String::from_utf8_lossy(&o.stderr).into_owned(),
    )
}

fn oplog_dir(j: &J) -> PathBuf {
    j.ws.join(".git/stw/oplog")
}

/// The TAB fields of every line of `refs.log` (all six; a line with another number fails the test).
fn refs_lines(j: &J) -> Vec<Vec<String>> {
    let s = std::fs::read_to_string(oplog_dir(j).join("refs.log")).unwrap_or_default();
    s.lines()
        .map(|l| {
            let f: Vec<String> = l.split('\t').map(str::to_string).collect();
            assert_eq!(f.len(), 6, "{l}");
            f
        })
        .collect()
}

/// A file of `n` MiB plus a little, made of valid `refs.log` lines (so the reader sees no bad line).
fn big_refs_log(dir: &Path, name: &str) {
    let line = format!(
        "2026-01-01T00:00:00Z\t{}\t{}\trefs/heads/old\thook\tmain\n",
        "1".repeat(40),
        "2".repeat(40)
    );
    let mut body = String::new();
    while body.len() <= 10 * MIB {
        body.push_str(&line);
    }
    std::fs::create_dir_all(dir).unwrap();
    std::fs::write(dir.join(name), body).unwrap();
}

#[test]
fn a_sync_writes_the_recovery_ref_and_the_branch_move_as_stw_lines() {
    let j = setup();
    let (code, v, err) = j.w.stw(&j.ws, &[]);
    assert_eq!(code, 0, "{v} {err}");
    let id = v["run_id"].as_str().unwrap();
    let z = "0".repeat(40);
    let lines = refs_lines(&j);
    let rec = format!("refs/stw/recovery/sync/{id}/1");
    let create = lines.iter().find(|f| f[3] == rec).expect("recovery line");
    assert_eq!(
        (&create[1], &create[2], &create[4], &create[5]),
        (&z, &j.ws_old, &"stw".to_string(), &"main".to_string())
    );
    let mv = lines
        .iter()
        .find(|f| f[3] == "refs/heads/main")
        .expect("branch line");
    assert_eq!(
        (&mv[1], &mv[2], &mv[4]),
        (&j.ws_old, &j.ws_new, &"stw".to_string())
    );
    assert!(
        lines.iter().all(|f| f[3] != "HEAD"),
        "the HEAD line repeats the branch line and is not written: {lines:?}"
    );
    assert!(lines
        .iter()
        .all(|f| f[0].ends_with('Z') && f[0].len() == 20));
    // the same file is what the reader shows
    let out = j.w.git(&j.ws, &["rev-parse", "HEAD"]);
    assert_eq!(out, j.ws_new);
}

#[test]
fn file_and_directory_modes_are_0600_and_0700() {
    let j = setup();
    j.w.stw(&j.ws, &[]);
    let mode = |p: PathBuf| std::fs::metadata(p).unwrap().permissions().mode() & 0o777;
    assert_eq!(mode(oplog_dir(&j)), 0o700);
    assert_eq!(mode(oplog_dir(&j).join("refs.log")), 0o600);
}

#[test]
fn the_deleted_recovery_refs_of_old_runs_are_written_as_delete_lines() {
    let j = setup();
    let mut first = String::new();
    for n in 0..12 {
        if n > 0 {
            j.news(&format!("n{n}"));
        }
        let (_, v, err) = j.w.stw(&j.ws, &[]);
        let id = v["run_id"].as_str().unwrap_or_else(|| panic!("{v} {err}"));
        if n == 0 {
            first = id.to_string();
        }
    }
    let name = format!("refs/stw/recovery/sync/{first}/1");
    let z = "0".repeat(40);
    let lines = refs_lines(&j);
    let del = lines
        .iter()
        .find(|f| f[3] == name && f[2] == z)
        .expect("a delete line for the pruned recovery ref");
    assert_eq!((&del[1], &del[4]), (&j.ws_old, &"stw".to_string()));
}

#[test]
fn a_log_over_10_mib_is_rotated_to_one_old_generation_after_the_append() {
    let j = setup();
    big_refs_log(&oplog_dir(&j), "refs.log");
    let before = std::fs::metadata(oplog_dir(&j).join("refs.log"))
        .unwrap()
        .len();
    let (code, v, err) = j.w.stw(&j.ws, &[]);
    assert_eq!(code, 0, "{v} {err}");
    let d = oplog_dir(&j);
    let old = std::fs::read_to_string(d.join("refs.log.1")).unwrap();
    assert!(
        old.len() as u64 > before,
        "the old file keeps the line of the append that crossed the limit"
    );
    assert!(old.contains("refs/heads/old") && old.contains("refs/stw/recovery/sync/"));
    let cur = std::fs::read_to_string(d.join("refs.log")).unwrap();
    assert!(
        cur.contains("\trefs/heads/main\tstw\t") && !cur.contains("refs/heads/old"),
        "the next append creates the new file: {cur}"
    );
    let errors = std::fs::read_to_string(d.join("errors.log")).unwrap();
    assert!(
        errors
            .lines()
            .any(|l| l.split('\t').collect::<Vec<_>>()[1..]
                == ["gap", "rotated", "failed=0", "bad=0", "refs.log"]),
        "{errors}"
    );
    // a second run appends to a new refs.log, the readers see both generations as one stream
    j.news("again");
    j.w.stw(&j.ws, &[]);
    assert!(!refs_lines(&j).is_empty());
    let (c, out, _) = oplog_cli(&j, &["timeline", "--json"]);
    assert_eq!(c, 0);
    assert!(out.contains("refs/heads/old"), "{out}");
}

#[test]
fn a_second_rotation_replaces_the_old_generation_and_the_timeline_says_truncated() {
    let j = setup();
    let d = oplog_dir(&j);
    big_refs_log(&d, "refs.log");
    j.w.stw(&j.ws, &[]);
    let first = std::fs::read(d.join("refs.log.1")).unwrap();
    big_refs_log(&d, "refs.log");
    j.news("two");
    j.w.stw(&j.ws, &[]);
    let second = std::fs::read(d.join("refs.log.1")).unwrap();
    assert_ne!(first, second, "one generation: .1 was replaced");
    assert!(!d.join("refs.log.2").exists());
    let (_, out, _) = oplog_cli(&j, &["timeline", "--json"]);
    let v: serde_json::Value = serde_json::from_str(&out).unwrap();
    let kinds: Vec<(&str, &str)> = v["gaps"]
        .as_array()
        .unwrap()
        .iter()
        .map(|g| (g["kind"].as_str().unwrap(), g["ref"].as_str().unwrap()))
        .collect();
    assert!(kinds.contains(&("truncated", "refs.log")), "{kinds:?}");
}

#[test]
fn a_held_rotate_lock_means_no_rotation_no_waiting_and_a_successful_command() {
    let j = setup();
    let d = oplog_dir(&j);
    big_refs_log(&d, "refs.log");
    let lock = OpenOptions::new()
        .write(true)
        .create(true)
        .truncate(false)
        .open(d.join("rotate.lock"))
        .unwrap();
    lock.try_lock().unwrap();
    let t = std::time::Instant::now();
    let (code, v, err) = j.w.stw(&j.ws, &[]);
    assert_eq!(code, 0, "{v} {err}");
    assert!(t.elapsed().as_secs() < 30);
    assert!(
        !d.join("refs.log.1").exists(),
        "not rotated while the lock is held"
    );
    assert!(!d.join("errors.log").exists(), "a held lock is no failure");
    drop(lock);
    j.news("later");
    j.w.stw(&j.ws, &[]);
    assert!(d.join("refs.log.1").exists(), "the next writer rotates");
}

#[test]
fn rotation_is_checked_again_under_the_lock() {
    // another stw rotated between the size check and the lock: the file is small again, nothing is renamed
    let j = setup();
    let d = oplog_dir(&j);
    std::fs::create_dir_all(&d).unwrap();
    std::fs::write(d.join("refs.log"), "").unwrap();
    stw::oplog::write::rotate_if_needed(&j.ws.join(".git"), 0);
    assert!(
        !d.join("refs.log.1").exists(),
        "an empty file is not above the limit"
    );
    std::fs::write(d.join("refs.log"), "x\n").unwrap();
    stw::oplog::write::rotate_if_needed(&j.ws.join(".git"), 1);
    assert!(d.join("refs.log.1").exists());
}

#[test]
fn a_broken_last_line_gets_a_line_feed_first() {
    let j = setup();
    let d = oplog_dir(&j);
    std::fs::create_dir_all(&d).unwrap();
    std::fs::write(d.join("refs.log"), "2026-01-01T00:00:00Z\t1").unwrap();
    j.w.stw(&j.ws, &[]);
    let s = std::fs::read_to_string(d.join("refs.log")).unwrap();
    assert!(s.starts_with("2026-01-01T00:00:00Z\t1\n2"), "{s:?}");
    assert!(s.ends_with('\n'));
}

#[test]
fn an_oplog_directory_that_is_a_symbolic_link_fails_no_sync_and_leaves_a_gap_line() {
    let j = setup();
    let target = j.w.p("elsewhere");
    std::fs::create_dir(&target).unwrap();
    std::fs::create_dir_all(j.ws.join(".git/stw")).unwrap();
    std::os::unix::fs::symlink(&target, oplog_dir(&j)).unwrap();
    let (code, v, err) = j.w.stw(&j.ws, &[]);
    assert_eq!(code, 0, "{v} {err}");
    assert_eq!(repo(&v, "ws")["result"], "fast-forwarded");
    assert!(err.contains("op log"), "a notice on stderr: {err}");
    assert_eq!(
        std::fs::read_dir(&target).unwrap().count(),
        0,
        "the link is not followed"
    );
    let gaps = std::fs::read_to_string(j.ws.join(".git/stw-oplog-gaps")).unwrap();
    assert!(gaps.contains("\tgap\twrite-failed\t"), "{gaps}");
}

#[test]
fn the_line_is_written_with_core_log_all_ref_updates_off() {
    let j = setup();
    j.w.git(&j.ws, &["config", "core.logAllRefUpdates", "false"]);
    j.w.stw(&j.ws, &[]);
    assert!(refs_lines(&j).iter().any(|f| f[3] == "refs/heads/main"));
}

#[test]
fn stw_lines_below_refs_remotes_follow_the_remotes_switch() {
    let j = setup();
    let repo = stw::repo::open(&j.ws).unwrap();
    let common = j.ws.join(".git");
    let id: gix::ObjectId = j.ws_new.parse().unwrap();
    let u = stw::oplog::write::RefUpdate {
        refname: "refs/remotes/origin/x",
        old: None,
        new: Some(id),
    };
    stw::oplog::write::log_ref_update_in(&common, &repo, &u, u64::MAX);
    assert!(refs_lines(&j).is_empty(), "off by default");
    std::fs::create_dir_all(oplog_dir(&j)).unwrap();
    std::fs::write(oplog_dir(&j).join("config"), "remotes=on\n").unwrap();
    stw::oplog::write::log_ref_update_in(&common, &repo, &u, u64::MAX);
    assert_eq!(refs_lines(&j).len(), 1);
}

#[test]
fn unlogged_updates_are_skipped() {
    let j = setup();
    let repo = stw::repo::open(&j.ws).unwrap();
    let common = j.ws.join(".git");
    let id: gix::ObjectId = j.ws_new.parse().unwrap();
    let log = |name: &str, old, new| {
        let u = stw::oplog::write::RefUpdate {
            refname: name,
            old,
            new,
        };
        stw::oplog::write::log_ref_update_in(&common, &repo, &u, u64::MAX);
    };
    log("ORIG_HEAD", None, Some(id));
    log("refs/heads/x", Some(id), Some(id));
    assert!(refs_lines(&j).is_empty());
    log("HEAD", None, Some(id));
    assert_eq!(refs_lines(&j).len(), 1);
}

#[test]
fn a_linked_worktree_writes_its_id_in_the_worktree_field() {
    let j = setup();
    let wt = j.w.p("linked");
    j.w.git(
        &j.ws,
        &[
            "worktree",
            "add",
            "-q",
            "-b",
            "side",
            wt.to_str().unwrap(),
            &j.ws_old,
        ],
    );
    let repo = stw::repo::open(&wt).unwrap();
    let id: gix::ObjectId = j.ws_new.parse().unwrap();
    let u = stw::oplog::write::RefUpdate {
        refname: "refs/heads/side",
        old: None,
        new: Some(id),
    };
    stw::oplog::write::log_ref_update(&repo, &u);
    let l = refs_lines(&j);
    assert_eq!(l.len(), 1);
    assert_eq!(l[0][5], "linked");
}
