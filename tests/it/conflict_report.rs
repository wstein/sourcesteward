//! Product tests for the conflict report accepted in the #30 fixture.
use std::fs;
use std::path::Path;
use std::process::{Command, Stdio};

use crate::common::{tree_snapshot, Fx};
use serde_json::{json, Value};

fn run(fx: &Fx, store: &Path, args: &[&str], expected: i32) -> Value {
    let output = Command::new(env!("CARGO_BIN_EXE_stw"))
        .arg("restack")
        .args(args)
        .arg("--store")
        .arg(store)
        .arg("--repo")
        .arg(&fx.path)
        .stdin(Stdio::null())
        .output()
        .unwrap();
    assert_eq!(output.status.code(), Some(expected), "{output:?}");
    serde_json::from_slice(&output.stdout).unwrap()
}

fn fixture(incoming_delete: bool) -> (Fx, Vec<Vec<String>>, String) {
    let fx = Fx::new();
    let base = (1..=35).map(|i| format!("line {i}\n")).collect::<Vec<_>>();
    let first = fx.commit(None, &[("story.txt", Some(&base.concat()))], "base");
    let mut feature = base.clone();
    feature[3] = "lower feature\n".into();
    let bottom = fx.commit(
        Some(first),
        &[("story.txt", Some(&feature.concat()))],
        "bottom",
    );
    let mut incoming = feature.clone();
    incoming[25] = "incoming conflict\n".into();
    let incoming_contents = incoming.concat();
    let next = (!incoming_delete).then_some(incoming_contents.as_str());
    let middle = fx.commit(Some(bottom), &[("story.txt", next)], "middle");
    let top = fx.commit(Some(middle), &[("later", Some("blocked\n"))], "top");
    let mut current = base;
    current[13] = "other main change\n".into();
    current[25] = "current conflict\n".into();
    let main = fx.commit(
        Some(first),
        &[("story.txt", Some(&current.concat()))],
        "advance main",
    );
    for (name, oid) in [
        ("main", main),
        ("bottom", bottom),
        ("middle", middle),
        ("top", top),
    ] {
        fx.branch(name, oid);
    }
    fx.set_head("main");
    fx.checkout_head();
    current[3] = feature[3].clone();
    (fx, vec![feature, current, incoming], middle.to_string())
}

#[test]
fn reopens_the_actual_replay_stages_and_reports_stale_inputs() {
    let (fx, contents, middle) = fixture(false);
    let store = tempfile::tempdir().unwrap();
    let before = tree_snapshot(&fx.path);
    let created = run(
        &fx,
        store.path(),
        &[
            "conflict-report",
            "bottom",
            "middle",
            "top",
            "--onto",
            "main",
        ],
        0,
    );
    let id = created["id"].as_str().unwrap();
    let reopened = run(&fx, store.path(), &["show-conflict", id], 0);
    let version = &reopened["conflict_versions"][0];
    assert_eq!(version["path"], "story.txt");
    assert_eq!(version["commit"], middle);
    assert_eq!(reopened["blocked_descendants"][0]["branch"], "top");
    assert_eq!(reopened["stale"], json!([]));
    for (side, bytes) in ["base", "current", "incoming"].into_iter().zip(contents) {
        let entry = &version[side];
        assert_eq!(entry["mode"], "100644");
        assert_eq!(
            fs::read_to_string(store.path().join(entry["file"].as_str().unwrap())).unwrap(),
            bytes.concat()
        );
    }
    let current =
        gix::ObjectId::from_hex(version["current"]["oid"].as_str().unwrap().as_bytes()).unwrap();
    assert!(fx.repo().find_blob(current).is_err());
    assert_eq!(tree_snapshot(&fx.path), before);

    let old_top = fx.tip("top").to_string();
    fx.branch("top", fx.tip("middle"));
    let after_move = tree_snapshot(&fx.path);
    let stale = run(&fx, store.path(), &["show-conflict", id], 1);
    assert_eq!(
        stale["stale"],
        json!([{"revision":"top", "expected":old_top, "observed":middle}])
    );
    assert_eq!(stale["conflict_versions"], reopened["conflict_versions"]);
    assert_eq!(tree_snapshot(&fx.path), after_move);
    let file = version["current"]["file"].as_str().unwrap();
    fs::write(store.path().join(file), "changed saved blob\n").unwrap();
    let refused = Command::new(env!("CARGO_BIN_EXE_stw"))
        .args(["restack", "show-conflict", id, "--store"])
        .arg(store.path())
        .arg("--repo")
        .arg(&fx.path)
        .output()
        .unwrap();
    assert_eq!(refused.status.code(), Some(1));
    let doc: Value = serde_json::from_slice(&refused.stdout).unwrap();
    assert_eq!(doc["schema_version"], 1);
    assert_eq!(doc["outcome"], "refused");
    assert!(doc["blocker"]
        .as_str()
        .unwrap()
        .contains("saved blob content does not match"));
    assert!(refused.stderr.is_empty());
}

#[test]
fn reports_a_missing_stage_when_the_incoming_side_deleted_the_path() {
    let (fx, _, _) = fixture(true);
    let store = tempfile::tempdir().unwrap();
    let created = run(
        &fx,
        store.path(),
        &["conflict-report", "bottom", "middle", "--onto", "main"],
        0,
    );
    let id = created["id"].as_str().unwrap();
    let reopened = run(&fx, store.path(), &["show-conflict", id], 0);
    let version = &reopened["conflict_versions"][0];
    assert!(!version["base"].is_null());
    assert!(!version["current"].is_null());
    assert!(version["incoming"].is_null());
}
