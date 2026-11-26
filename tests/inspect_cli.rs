mod common;

use crate::common::FixtureCommand as _;
use std::fs;
use std::process::{Command, Stdio};

use common::{path_str, FakeRoutes, Fx};
use serde_json::{json, Value};

fn inspect(args: &[&str], api: Option<&str>) -> (i32, Value) {
    let mut command = Command::new(env!("CARGO_BIN_EXE_stw"));
    command
        .fixture_env_clear()
        .env("HOME", "/nonexistent")
        .env("PATH", "/nonexistent");
    if let Some(url) = api {
        command
            .env("STW_GITHUB_API_URL", url)
            .env("GH_TOKEN", "test-token");
    }
    let output = command
        .arg("inspect")
        .args(args)
        .stdin(Stdio::null())
        .output()
        .unwrap();
    let doc = serde_json::from_slice(&output.stdout).unwrap_or_else(|error| {
        panic!(
            "{error}: stdout={:?} stderr={:?}",
            output.stdout, output.stderr
        )
    });
    (output.status.code().unwrap(), doc)
}

#[test]
fn local_rename_uses_both_rule_paths_without_git_or_github() {
    let fx = Fx::new();
    let old = fx.commit(
        None,
        &[
            ("AGENTS.md", Some("root rules\n")),
            ("a/AGENTS.md", Some("old rules\n")),
            ("a/old.rs", Some("fn main() {}\n")),
            ("b/AGENTS.md", Some("new rules\n")),
            ("b/Cargo.toml", Some("[package]\n")),
        ],
        "base",
    );
    let new = fx.commit(
        Some(old),
        &[
            ("a/old.rs", None),
            ("b/tests/new.rs", Some("fn main() {}\n")),
        ],
        "rename",
    );
    let before = common::tree_snapshot(&fx.path);
    let (code, doc) = inspect(
        &[
            "git",
            &path_str(&fx.path),
            &old.to_string(),
            &new.to_string(),
        ],
        None,
    );
    assert_eq!(code, 0, "{doc}");
    assert_eq!(doc["source"], "git");
    assert_eq!(doc["schema_version"], 1);
    assert_eq!(doc["comparison_base"], old.to_string());
    assert_eq!(doc["files"].as_array().unwrap().len(), 1, "{doc}");
    let file = &doc["files"][0];
    assert_eq!(file["path"], "b/tests/new.rs");
    assert_eq!(file["previous_path"], "a/old.rs");
    assert_eq!(file["status"], "renamed");
    assert_eq!(
        file["rules"],
        json!([
            {"side":"base", "path":"AGENTS.md"}, {"side":"base", "path":"a/AGENTS.md"},
            {"side":"head", "path":"AGENTS.md"}, {"side":"head", "path":"b/AGENTS.md"},
        ])
    );
    assert_eq!(
        file["test_suggestions"][0]["argv"],
        json!(["cargo", "test", "--locked", "--test", "new"])
    );
    assert_eq!(common::tree_snapshot(&fx.path), before);
    assert!(!fx.path.join("b/tests/new.rs").exists());
}

#[test]
fn published_comparison_retains_changes_when_ref_moves_and_context_is_truncated() {
    let calls = std::sync::atomic::AtomicUsize::new(0);
    let api = FakeRoutes::start(move |path, _| {
        let body = if path.ends_with("/pulls/7") {
            let head = if calls.fetch_add(1, std::sync::atomic::Ordering::Relaxed) == 0 {
                "b"
            } else {
                "c"
            };
            json!({"base":{"sha":"a".repeat(40)}, "head":{"sha":head.repeat(40)}})
        } else if path == format!("/repos/o/r/compare/{}...{}", "a".repeat(40), "b".repeat(40)) {
            json!({"merge_base_commit":{"sha":"a".repeat(40)}, "files":[{"filename":"src/lib.rs","status":"modified"}]})
        } else if path.contains("/git/trees/") {
            json!({"tree":[{"type":"blob","path":"AGENTS.md"}],"truncated":true})
        } else {
            return (404, vec![], "{}".into());
        };
        (200, vec![], body.to_string())
    });
    let (code, doc) = inspect(&["github", "o/r", "7"], Some(&api.url));
    assert_eq!(code, 1, "{doc}");
    assert_eq!(doc["head"], "b".repeat(40));
    assert_eq!(doc["files"][0]["path"], "src/lib.rs");
    assert_eq!(doc["incomplete"].as_array().unwrap().len(), 3, "{doc}");
    assert_eq!(doc["incomplete"][2], "pull-request-moved");
}

#[test]
fn unavailable_sources_emit_versioned_json() {
    let fx = Fx::new();
    let (code, doc) = inspect(&["git", &path_str(&fx.path), "missing", "HEAD"], None);
    assert_eq!(code, 1);
    assert_eq!(doc["schema_version"], 1);
    assert_eq!(doc["files"], json!([]));
    assert!(doc["incomplete"][0]
        .as_str()
        .unwrap()
        .starts_with("comparison-unavailable"));
    assert!(fs::metadata(fx.path).unwrap().is_dir());
}
