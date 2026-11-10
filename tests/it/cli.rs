//! The `stw` binary: options, JSON envelope, exit codes.
use std::process::{Command, Output};

use crate::common::*;
use serde_json::Value;

fn stw(args: &[&str]) -> Output {
    Command::new(env!("CARGO_BIN_EXE_stw"))
        // keep the run independent of the developer's token and workspace
        .env_remove("GH_TOKEN")
        .env_remove("GITHUB_TOKEN")
        .env_remove("STW_ROOT")
        .env("HOME", "/nonexistent")
        .env("STW_KEYCHAIN_SERVICE", "stw-test-no-such-service")
        .args(args)
        .output()
        .unwrap()
}

fn code(o: &Output) -> i32 {
    o.status.code().unwrap()
}

fn text(b: &[u8]) -> String {
    String::from_utf8_lossy(b).into_owned()
}

/// A workspace root with one commit on `main`, tracking `origin/main`.
fn root() -> Fx {
    let fx = Fx::new();
    let c0 = fx.commit(None, &[("a.txt", Some("a\n"))], "c0");
    fx.branch("main", c0);
    fx.remote("main", c0);
    fx.origin_head("main");
    fx.track("main");
    fx.set_head("main");
    fx.checkout_head();
    fx
}

fn json(o: &Output) -> Value {
    serde_json::from_slice(&o.stdout)
        .unwrap_or_else(|e| panic!("not JSON ({e}): {}", text(&o.stdout)))
}

#[test]
fn json_documents_carry_schema_version_and_command() {
    let fx = root();
    let r = path_str(&fx.path);
    for (args, command) in [
        (
            vec!["status", "--root", &r, "--offline", "--json"],
            "status",
        ),
        (
            vec!["tidy", "plan", "--root", &r, "--offline", "--json"],
            "tidy-plan",
        ),
        (vec!["sync", "--root", &r, "--offline", "--json"], "sync"),
    ] {
        let o = stw(&args);
        assert_eq!(code(&o), 0, "{args:?}: {}", text(&o.stderr));
        let v = json(&o);
        assert_eq!(v["schema_version"], 2, "{args:?}");
        assert_eq!(v["command"], command);
        assert_eq!(v["repos"].as_array().unwrap().len(), 1);
    }
}

/// stw executes; it never prints a command for the user to run. A repository that is behind, dirty and has a
/// stale registration for a merged branch would have produced `next:` lines and `next_steps` before.
#[test]
fn no_output_contains_a_command_or_next_steps() {
    let fx = Fx::new();
    let c0 = fx.commit(None, &[("a.txt", Some("a\n"))], "c0");
    let c1 = fx.commit(Some(c0), &[("a.txt", Some("b\n"))], "c1");
    fx.branch("main", c0);
    fx.remote("main", c1);
    fx.origin_head("main");
    fx.track("main");
    fx.set_head("main");
    fx.checkout_head();
    fx.write("untracked.txt", "x\n");
    fx.branch("merged", c0);
    add_worktree(&fx, "gone", "merged", false, false);
    let r = path_str(&fx.path);
    for args in [
        vec!["status", "--root", &r, "--offline"],
        vec!["status", "--root", &r, "--offline", "--json"],
        vec!["tidy", "plan", "--root", &r, "--offline"],
        vec!["tidy", "plan", "--root", &r, "--offline", "--json"],
        vec!["sync", "--root", &r, "--offline"],
        vec!["sync", "--root", &r, "--offline", "--json"],
        vec!["--help"],
        vec!["sync", "--help"],
    ] {
        let o = stw(&args);
        assert_eq!(code(&o), 0, "{args:?}: {}", text(&o.stderr));
        let out = text(&o.stdout);
        for bad in ["next:", "next_steps", "git -C", "undo:", "suggestion"] {
            assert!(!out.contains(bad), "{args:?} printed {bad:?}: {out}");
        }
    }
}

#[test]
fn help_documents_the_exit_codes_on_every_command() {
    for args in [
        vec!["--help"],
        vec!["status", "--help"],
        vec!["tidy", "--help"],
        vec!["tidy", "plan", "--help"],
        vec!["tidy", "apply", "--help"],
        vec!["tidy", "restore", "--help"],
        vec!["sync", "--help"],
    ] {
        let o = stw(&args);
        assert_eq!(code(&o), 0, "{args:?}");
        let out = text(&o.stdout);
        assert!(out.contains("Exit codes:"), "{args:?}: {out}");
        assert!(out.contains("2  usage error"), "{args:?}: {out}");
    }
}

#[test]
fn usage_errors_exit_2() {
    let fx = root();
    let r = path_str(&fx.path);
    for args in [
        vec![],
        vec!["bogus"],
        vec!["status", "--no-such-flag"],
        vec!["status", "--root"],
        vec!["status", "--all"],
        vec!["sync", "--root", &r, "--repo", "no-such-repo", "--offline"],
        vec!["status", "--root", "/nonexistent/stw-root"],
    ] {
        let o = stw(&args);
        assert_eq!(code(&o), 2, "{args:?}: {}", text(&o.stderr));
        assert!(o.stdout.is_empty(), "{args:?} printed to stdout");
    }
}

#[test]
fn unknown_repository_lists_the_known_names() {
    let fx = root();
    let o = stw(&["status", "--root", &path_str(&fx.path), "--repo", "nope"]);
    let err = text(&o.stderr);
    assert!(err.contains("unknown repository \"nope\""), "{err}");
    assert!(err.contains("repo"), "known names are listed: {err}");
}

#[test]
fn missing_workspace_says_how_to_fix_it() {
    let dir = tempfile::tempdir().unwrap();
    let o = Command::new(env!("CARGO_BIN_EXE_stw"))
        .current_dir(dir.path())
        .env_remove("STW_ROOT")
        .env("GIT_CEILING_DIRECTORIES", dir.path().parent().unwrap())
        .args(["status", "--offline"])
        .output()
        .unwrap();
    assert_eq!(code(&o), 2);
    let err = text(&o.stderr);
    assert!(
        err.contains("--root PATH") && err.contains("STW_ROOT"),
        "{err}"
    );
}

#[test]
fn a_repository_that_cannot_be_read_exits_1_in_every_command() {
    let fx = root();
    fx.write(
        ".gitmodules",
        "[submodule \"broken\"]\n\tpath = broken\n\turl = x\n",
    );
    fx.write("broken/.git", "gitdir: /nonexistent/stw/gitdir\n");
    let r = path_str(&fx.path);
    for args in [
        vec!["status", "--root", &r, "--offline", "--json"],
        vec!["tidy", "plan", "--root", &r, "--offline", "--json"],
        vec!["sync", "--root", &r, "--offline", "--json"],
    ] {
        let o = stw(&args);
        assert_eq!(code(&o), 1, "{args:?}: {}", text(&o.stderr));
        let v = json(&o);
        let broken = &v["repos"][1];
        assert_eq!(broken["name"], "broken");
        assert!(broken["error"].is_string(), "{args:?}: {broken}");
        assert!(v["repos"][0]["error"].is_null());
    }
}

#[test]
fn repo_filter_selects_by_name() {
    let fx = root();
    fx.write(
        ".gitmodules",
        "[submodule \"broken\"]\n\tpath = broken\n\turl = x\n",
    );
    fx.write("broken/.git", "gitdir: /nonexistent/stw/gitdir\n");
    let r = path_str(&fx.path);
    let o = stw(&[
        "status",
        "--root",
        &r,
        "--offline",
        "--json",
        "--repo",
        "repo",
    ]);
    assert_eq!(code(&o), 0);
    assert_eq!(json(&o)["repos"].as_array().unwrap().len(), 1);
}

#[test]
fn json_names_why_pull_requests_were_not_read() {
    let fx = root();
    let o = stw(&["status", "--root", &path_str(&fx.path), "--json"]);
    let v = json(&o);
    assert_eq!(v["github"], false);
    let note = v["github_note"].as_str().unwrap();
    assert!(
        note.contains("GH_TOKEN") && note.contains("--offline"),
        "{note}"
    );
}

#[test]
fn engine_names_the_pinned_gix_version() {
    let manifest =
        std::fs::read_to_string(concat!(env!("CARGO_MANIFEST_DIR"), "/Cargo.toml")).unwrap();
    assert!(
        manifest.contains(&format!(
            "gix = {{ version = \"={}\"",
            stw::output::GIX_VERSION
        )),
        "output::GIX_VERSION and the Cargo.toml pin differ"
    );
}
