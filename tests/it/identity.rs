//! The reflog identity of `sync --apply`: GIT_COMMITTER_*, then user.name/email of the Git config, then
//! the fallback `stw <stw@localhost>`. Runs in an environment without any identity.
use std::process::Command;

use crate::common::*;
use serde_json::Value;

/// main (checked out) is one commit behind origin/main.
fn behind() -> Fx {
    let fx = Fx::new();
    let c0 = fx.commit(None, &[("a.txt", Some("a\n"))], "c0");
    let c1 = fx.commit(Some(c0), &[("b.txt", Some("b\n"))], "c1");
    fx.branch("main", c0);
    fx.set_head("main");
    fx.remote("main", c1);
    fx.origin_head("main");
    fx.track("main");
    fx.checkout_head();
    fx
}

/// Run `stw sync --apply --offline --json` with no identity anywhere except `env`.
fn apply(fx: &Fx, env: &[(&str, &str)]) -> Value {
    let home = tempfile::tempdir().unwrap();
    let o = Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_clear()
        .env("HOME", home.path())
        .env("GIT_CONFIG_GLOBAL", "/dev/null")
        .env("GIT_CONFIG_SYSTEM", "/dev/null")
        .env("GIT_CONFIG_NOSYSTEM", "1")
        .envs(env.iter().copied())
        .args(["sync", "--offline", "--json", "--root"])
        .arg(&fx.path)
        .output()
        .unwrap();
    let v: Value = serde_json::from_slice(&o.stdout)
        .unwrap_or_else(|e| panic!("not JSON ({e}): {}", String::from_utf8_lossy(&o.stderr)));
    assert_eq!(o.status.code(), Some(0), "{v}");
    assert_eq!(v["repos"][0]["result"], "fast-forwarded", "{v}");
    v
}

#[test]
fn without_any_identity_the_reflog_carries_the_fallback() {
    let fx = behind();
    fx.strip_identity();
    apply(&fx, &[]);
    assert!(fx.exists("b.txt"));
    assert_eq!(fx.last_reflog_identity("HEAD"), "stw <stw@localhost>");
    assert_eq!(
        fx.last_reflog_identity("refs/heads/main"),
        "stw <stw@localhost>"
    );
}

#[test]
fn the_git_config_identity_beats_the_fallback() {
    let fx = behind();
    fx.strip_identity();
    fx.set_identity("Cfg Name", "cfg@example.com");
    apply(&fx, &[]);
    assert_eq!(
        fx.last_reflog_identity("HEAD"),
        "Cfg Name <cfg@example.com>"
    );
}

#[test]
fn the_environment_identity_beats_the_git_config() {
    let fx = behind();
    fx.strip_identity();
    fx.set_identity("Cfg Name", "cfg@example.com");
    apply(
        &fx,
        &[
            ("GIT_COMMITTER_NAME", "Env Name"),
            ("GIT_COMMITTER_EMAIL", "env@example.com"),
        ],
    );
    assert_eq!(
        fx.last_reflog_identity("HEAD"),
        "Env Name <env@example.com>"
    );
    assert_eq!(
        fx.last_reflog_identity("refs/heads/main"),
        "Env Name <env@example.com>"
    );
}

#[test]
fn author_variables_are_not_used_for_the_reflog() {
    let fx = behind();
    fx.strip_identity();
    apply(
        &fx,
        &[
            ("GIT_AUTHOR_NAME", "Author"),
            ("GIT_AUTHOR_EMAIL", "author@example.com"),
        ],
    );
    assert_eq!(fx.last_reflog_identity("HEAD"), "stw <stw@localhost>");
}

#[test]
fn the_fallback_is_never_written_to_a_config() {
    let fx = behind();
    fx.strip_identity();
    let before = std::fs::read_to_string(fx.path.join(".git/config")).unwrap();
    apply(&fx, &[]);
    assert_eq!(
        std::fs::read_to_string(fx.path.join(".git/config")).unwrap(),
        before
    );
}

/// Rule: the environment identity counts only when GIT_COMMITTER_NAME and GIT_COMMITTER_EMAIL are both
/// non-empty; otherwise the next step (Git config, then fallback) decides, and no half-mixed identity appears.
#[test]
fn only_a_name_in_the_environment_falls_through_to_the_fallback() {
    let fx = behind();
    fx.strip_identity();
    apply(&fx, &[("GIT_COMMITTER_NAME", "OnlyName")]);
    assert_eq!(fx.last_reflog_identity("HEAD"), "stw <stw@localhost>");
}

#[test]
fn only_an_email_in_the_environment_falls_through_to_the_fallback() {
    let fx = behind();
    fx.strip_identity();
    apply(&fx, &[("GIT_COMMITTER_EMAIL", "only@example.com")]);
    assert_eq!(fx.last_reflog_identity("HEAD"), "stw <stw@localhost>");
}

#[test]
fn empty_environment_values_fall_through_to_the_fallback() {
    let fx = behind();
    fx.strip_identity();
    apply(
        &fx,
        &[("GIT_COMMITTER_NAME", ""), ("GIT_COMMITTER_EMAIL", "")],
    );
    assert_eq!(fx.last_reflog_identity("HEAD"), "stw <stw@localhost>");
}

#[test]
fn a_partial_environment_identity_falls_through_to_the_git_config() {
    let fx = behind();
    fx.strip_identity();
    fx.set_identity("Cfg Name", "cfg@example.com");
    apply(&fx, &[("GIT_COMMITTER_NAME", "OnlyName")]);
    assert_eq!(
        fx.last_reflog_identity("HEAD"),
        "Cfg Name <cfg@example.com>"
    );
}
