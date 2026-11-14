//! `stw status`: the hint "op log hooks not installed" (docs/oplog-format.md, 11, question 21). One line under a
//! repository of the text and the additive JSON field `oplog_hooks`; only a hint, nothing is installed or written, the
//! exit code does not change.
use std::process::{Command, Output};

use crate::common::*;
use serde_json::{json, Value};

const POST: &str = "post-rewrite";
const REF: &str = "reference-transaction";
const MISSING_BOTH: &str =
    "    op log hooks: not installed (post-rewrite, reference-transaction); install with: stw oplog hooks install\n";

fn stw_in(fx: &Fx, args: &[&str]) -> Output {
    Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_clear()
        .env("PATH", "/nonexistent")
        .env("HOME", "/nonexistent")
        .env("STW_KEYCHAIN_SERVICE", "stw-test-no-such-service")
        .current_dir(&fx.path)
        .args(args)
        .output()
        .unwrap()
}

fn status(fx: &Fx) -> (i32, String) {
    let o = stw_in(fx, &["status", "--root", &path_str(&fx.path), "--offline"]);
    (
        o.status.code().unwrap(),
        String::from_utf8_lossy(&o.stdout).into_owned(),
    )
}

fn status_json(fx: &Fx) -> Value {
    let o = stw_in(
        fx,
        &[
            "status",
            "--root",
            &path_str(&fx.path),
            "--offline",
            "--json",
        ],
    );
    serde_json::from_slice(&o.stdout).unwrap()
}

fn hooks(v: &Value) -> Value {
    v["repos"][0]["oplog_hooks"].clone()
}

fn fixture() -> Fx {
    let fx = status_fixture();
    fx.write_plain(".git/info/.keep", "");
    fx
}

fn install(fx: &Fx) {
    let o = stw_in(fx, &["oplog", "hooks", "install", "--yes"]);
    assert_eq!(
        o.status.code(),
        Some(0),
        "{}",
        String::from_utf8_lossy(&o.stdout)
    );
}

fn hooks_dir(fx: &Fx) -> std::path::PathBuf {
    fx.path.join(".git/hooks")
}

#[test]
fn missing_hooks_give_one_line_with_the_install_command() {
    let fx = fixture();
    let (code, t) = status(&fx);
    assert_eq!(code, 0, "{t}");
    assert_eq!(t.matches("op log hooks").count(), 1, "{t}");
    assert!(t.contains(MISSING_BOTH), "{t}");
    assert_eq!(
        hooks(&status_json(&fx)),
        json!({"state": "missing", "reason": null, "missing": [POST, REF]})
    );
}

#[test]
fn both_hooks_ours_show_nothing() {
    let fx = fixture();
    install(&fx);
    let (code, t) = status(&fx);
    assert_eq!(code, 0, "{t}");
    assert!(!t.contains("op log hooks"), "{t}");
    assert_eq!(
        hooks(&status_json(&fx)),
        json!({"state": "installed", "reason": null, "missing": []})
    );
}

#[test]
fn one_hook_ours_names_only_the_missing_one() {
    let fx = fixture();
    install(&fx);
    std::fs::remove_file(hooks_dir(&fx).join(POST)).unwrap();
    let (_, t) = status(&fx);
    assert!(
        t.contains("    op log hooks: not installed (post-rewrite); install with: stw oplog hooks install\n"),
        "{t}"
    );
    assert_eq!(
        hooks(&status_json(&fx)),
        json!({"state": "missing", "reason": null, "missing": [POST]})
    );
}

#[test]
fn a_foreign_hook_gives_no_install_advice() {
    let fx = fixture();
    std::fs::create_dir_all(hooks_dir(&fx)).unwrap();
    std::fs::write(hooks_dir(&fx).join(REF), "#!/bin/sh\nexit 0\n").unwrap();
    let (code, t) = status(&fx);
    assert_eq!(code, 0, "{t}");
    assert!(
        t.contains("    op log hooks: not active, install is refused here (another hook is in place); see: stw oplog hooks status\n"),
        "{t}"
    );
    assert!(!t.contains("install with"), "{t}");
    assert_eq!(
        hooks(&status_json(&fx)),
        json!({"state": "blocked", "reason": "foreign-hook", "missing": [POST, REF]})
    );
}

#[test]
fn core_hooks_path_gives_no_install_advice_even_with_our_hooks_in_the_git_dir() {
    let fx = fixture();
    install(&fx);
    let cfg = fx.path.join(".git/config");
    let mut c = std::fs::read_to_string(&cfg).unwrap();
    c.push_str("[core]\n\thooksPath = /nonexistent/stw-hooks\n");
    std::fs::write(&cfg, c).unwrap();
    let (code, t) = status(&fx);
    assert_eq!(code, 0, "{t}");
    assert!(
        t.contains("    op log hooks: not active, install is refused here (core.hooksPath is set); see: stw oplog hooks status\n"),
        "{t}"
    );
    assert_eq!(
        hooks(&status_json(&fx)),
        json!({"state": "blocked", "reason": "hooks-path", "missing": []})
    );
}

#[test]
fn a_hooks_directory_that_is_a_link_is_blocked() {
    let fx = fixture();
    let target = fx.path.join("elsewhere");
    std::fs::create_dir(&target).unwrap();
    std::fs::remove_dir_all(hooks_dir(&fx)).unwrap();
    std::os::unix::fs::symlink(&target, hooks_dir(&fx)).unwrap();
    let (_, t) = status(&fx);
    assert!(
        t.contains("(the hooks directory is a symbolic link); see: stw oplog hooks status\n"),
        "{t}"
    );
    assert_eq!(
        hooks(&status_json(&fx))["reason"],
        json!("hooks-dir-symlink")
    );
}

#[test]
fn the_hint_writes_nothing_and_does_not_change_the_exit_code() {
    let fx = fixture();
    let (clean_code, _) = status(&fx);
    assert_eq!(clean_code, 0);
    assert!(!hooks_dir(&fx).join(POST).exists() && !hooks_dir(&fx).join(REF).exists());
    assert!(!fx.path.join(".git/stw").exists());
    // a repository that needs attention keeps its exit code with the hint shown
    fx.write("untracked.txt", "x\n");
    let (code, t) = status(&fx);
    assert_eq!(code, 0, "{t}");
    assert!(t.contains(MISSING_BOTH), "{t}");
}

#[test]
fn a_repository_that_cannot_be_read_has_no_hint_and_a_null_field() {
    let fx = fixture();
    fx.write(
        ".gitmodules",
        "[submodule \"broken\"]\n\tpath = broken\n\turl = x\n\tignore = all\n",
    );
    fx.write("broken/.git", "gitdir: /nonexistent/stw/gitdir\n");
    let (code, t) = status(&fx);
    assert_eq!(code, 1, "{t}");
    assert_eq!(t.matches("op log hooks").count(), 1, "{t}");
    let v = status_json(&fx);
    let repos = v["repos"].as_array().unwrap();
    let broken = repos.iter().find(|r| r["name"] == "broken").unwrap();
    assert_eq!(broken["oplog_hooks"], Value::Null);
    assert_eq!(v["schema_version"], json!(2));
}

#[test]
fn the_path_form_carries_the_json_field_and_prints_no_hint_line() {
    let fx = fixture();
    let p = path_str(&fx.path);
    let o = stw_in(&fx, &["status", "--offline", "--json", &p]);
    let v: Value = serde_json::from_slice(&o.stdout).unwrap();
    assert_eq!(v["schema_version"], json!(2));
    assert_eq!(v["repos"][0]["oplog_hooks"]["state"], json!("missing"));
    let o = stw_in(&fx, &["status", "--offline", &p]);
    assert!(
        !String::from_utf8_lossy(&o.stdout).contains("op log hooks"),
        "{}",
        String::from_utf8_lossy(&o.stdout)
    );
}
