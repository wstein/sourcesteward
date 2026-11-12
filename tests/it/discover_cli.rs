//! Repository discovery in the commands: `GIT_CEILING_DIRECTORIES` hides the repository above the start, for
//! the workspace root (`status`), the root of `stw add` and `review carry-over`. Without the variable the same
//! start finds the repository, so each assertion on the hidden case has its visible counterpart.
use std::process::{Command, Output};

use crate::common::*;

fn run(cwd: &std::path::Path, ceiling: Option<&std::path::Path>, args: &[&str]) -> Output {
    assert!(cwd.is_absolute(), "unconfined");
    let mut c = Command::new(env!("CARGO_BIN_EXE_stw"));
    c.env_clear()
        .env("PATH", std::env::var_os("PATH").unwrap())
        .env("HOME", cwd)
        .current_dir(cwd);
    if let Some(p) = ceiling {
        c.env("GIT_CEILING_DIRECTORIES", p);
    }
    c.args(args).output().unwrap()
}

fn text(o: &Output) -> String {
    format!(
        "{}{}",
        String::from_utf8_lossy(&o.stdout),
        String::from_utf8_lossy(&o.stderr)
    )
}

/// A repository with `.gitmodules` (a workspace root) and a start directory below it.
fn world() -> (Fx, std::path::PathBuf, std::path::PathBuf) {
    let fx = Fx::new();
    let top = std::fs::canonicalize(&fx.path).unwrap();
    std::fs::write(top.join(".gitmodules"), "").unwrap();
    let sub = top.join("sub");
    std::fs::create_dir(&sub).unwrap();
    (fx, top, sub)
}

#[test]
fn a_ceiling_hides_the_workspace_root_of_status() {
    let (_fx, top, sub) = world();
    let o = run(&sub, None, &["status", "--offline"]);
    assert_eq!(o.status.code(), Some(0), "{}", text(&o));
    let o = run(&sub, Some(&top), &["status", "--offline"]);
    assert_eq!(o.status.code(), Some(2), "{}", text(&o));
    assert!(text(&o).contains("no workspace root"), "{}", text(&o));
}

#[test]
fn a_ceiling_hides_the_root_of_add() {
    let (_fx, top, sub) = world();
    let o = run(&sub, None, &["add", "x/y"]);
    assert!(!text(&o).contains("no workspace root"), "{}", text(&o));
    let o = run(&sub, Some(&top), &["add", "x/y"]);
    assert_eq!(o.status.code(), Some(2), "{}", text(&o));
    assert!(text(&o).contains("no workspace root"), "{}", text(&o));
}

#[test]
fn a_ceiling_hides_the_repository_of_carry_over() {
    let (_fx, top, sub) = world();
    let id = "a".repeat(40);
    let args = [
        "review",
        "carry-over",
        &id,
        &id,
        &id,
        &id,
        "--pr",
        "1",
        "--json",
    ];
    let o = run(&sub, None, &args);
    assert!(text(&o).contains("\"object-missing\""), "{}", text(&o));
    let o = run(&sub, Some(&top), &args);
    assert_eq!(o.status.code(), Some(1), "{}", text(&o));
    assert!(text(&o).contains("\"read-failed\""), "{}", text(&o));
}
