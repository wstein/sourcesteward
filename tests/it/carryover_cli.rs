//! `stw review carry-over`: exit codes, output, repository resolution. Core cases: tests/carryover.rs.
//! Harness confinement: repositories live in tempdirs created here; the binary runs with a scrubbed environment.
use std::process::{Command, Output};

use crate::common::*;

fn git_available() -> bool {
    Command::new("git")
        .arg("--version")
        .output()
        .is_ok_and(|o| o.status.success())
}

fn stw(cwd: &std::path::Path, home: &std::path::Path, args: &[&str]) -> Output {
    assert!(cwd.is_absolute() && home.is_absolute(), "unconfined");
    Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_clear()
        .env("PATH", std::env::var_os("PATH").unwrap())
        .env("HOME", home)
        .current_dir(cwd)
        .args(["review", "carry-over"])
        .args(args)
        .output()
        .unwrap()
}

fn rebase_fixture() -> (Fx, [String; 4]) {
    let fx = Fx::new();
    let m1 = fx.commit(
        None,
        &[("a.txt", Some("a\n")), ("c.txt", Some("p\n"))],
        "m1",
    );
    let c1 = fx.commit(Some(m1), &[("a.txt", Some("a\nb\n"))], "c1");
    let m2 = fx.commit(Some(m1), &[("c.txt", Some("p\nq\n"))], "m2");
    let c1n = fx.commit(Some(m2), &[("a.txt", Some("a\nb\n"))], "c1");
    let ids = [m1, c1, m2, c1n].map(|i| i.to_string());
    (fx, ids)
}

#[test]
fn yes_prints_the_line_last_and_exits_zero_from_a_subdirectory() {
    if !git_available() {
        eprintln!("git not installed: not run");
        return;
    }
    let (fx, ids) = rebase_fixture();
    let home = tempfile::tempdir().unwrap();
    let sub = fx.path.join("sub");
    std::fs::create_dir(&sub).unwrap();
    let mut args: Vec<&str> = ids.iter().map(String::as_str).collect();
    args.extend(["--pr", "12"]);
    // the current directory is a subdirectory: the repository around it is used
    let out = stw(&sub, home.path(), &args);
    let text = String::from_utf8_lossy(&out.stdout).into_owned();
    assert_eq!(
        out.status.code(),
        Some(0),
        "{text}\n{}",
        String::from_utf8_lossy(&out.stderr)
    );
    let last = text.trim_end().lines().last().unwrap();
    assert!(
        last.starts_with(&format!(
            "Carried-over: from={} to={} patch-id=",
            ids[1], ids[3]
        )),
        "{text}"
    );
    assert!(last.ends_with(" pr=12"));
    // --json and --checkout
    args.extend(["--json", "--checkout", fx.path.to_str().unwrap()]);
    let out = stw(home.path(), home.path(), &args);
    let v: serde_json::Value = serde_json::from_slice(&out.stdout).unwrap();
    assert_eq!(out.status.code(), Some(0));
    assert_eq!(v["carry_over"], true);
    assert_eq!(v["schema_version"], 2);
    assert_eq!(v["range_diff"]["via"], "git");
}

#[test]
fn no_exits_one_with_the_result_and_without_a_line() {
    if !git_available() {
        eprintln!("git not installed: not run");
        return;
    }
    let (fx, ids) = rebase_fixture();
    let home = tempfile::tempdir().unwrap();
    // old and new head swapped in meaning: the new head is the old one on another base with a changed patch
    let other = fx.commit(
        Some(ids[2].parse().unwrap()),
        &[("a.txt", Some("changed\n"))],
        "c1",
    );
    let other = other.to_string();
    let out = stw(
        &fx.path,
        home.path(),
        &[&ids[0], &ids[1], &ids[2], &other, "--pr", "none"],
    );
    let text = String::from_utf8_lossy(&out.stdout).into_owned();
    assert_eq!(out.status.code(), Some(1), "{text}");
    assert!(text.contains("carry-over: no"), "{text}");
    assert!(!text.contains("Carried-over:"), "{text}");
}

#[test]
fn a_directory_that_is_no_repository_is_a_read_failure_with_exit_one() {
    let home = tempfile::tempdir().unwrap();
    let id = "a".repeat(40);
    let out = stw(home.path(), home.path(), &[&id, &id, &id, &id, "--pr", "1"]);
    let text = String::from_utf8_lossy(&out.stdout).into_owned();
    assert_eq!(out.status.code(), Some(1), "{text}");
    assert!(text.contains("read-failed"), "{text}");
}

#[test]
fn usage_errors_exit_two_before_any_git() {
    let home = tempfile::tempdir().unwrap();
    let id = "a".repeat(40);
    for args in [
        vec![],
        vec![id.as_str(), id.as_str(), id.as_str(), "--pr", "1"],
        vec![id.as_str(), id.as_str(), id.as_str(), id.as_str()],
        vec!["main", id.as_str(), id.as_str(), id.as_str(), "--pr", "1"],
        vec![
            id.as_str(),
            id.as_str(),
            id.as_str(),
            id.as_str(),
            "--pr",
            "0",
        ],
        vec![
            id.as_str(),
            id.as_str(),
            id.as_str(),
            id.as_str(),
            "--pr",
            "none",
            "--fetch",
        ],
    ] {
        let out = stw(home.path(), home.path(), &args);
        assert_eq!(
            out.status.code(),
            Some(2),
            "{args:?}: {}",
            String::from_utf8_lossy(&out.stderr)
        );
        assert!(out.stdout.is_empty(), "{args:?}");
    }
}

#[test]
fn a_binary_file_with_other_contents_in_an_intermediate_commit_is_no() {
    if !git_available() {
        eprintln!("git not installed: not run");
        return;
    }
    // range-diff shows "Binary files differ" only; the cumulative patch-id is equal; the per-commit patch-id is not
    let fx = Fx::new();
    let m1 = fx.commit(
        None,
        &[
            ("img.bin", Some("\0X")),
            ("a.txt", Some("a\n")),
            ("b.txt", Some("b\n")),
        ],
        "m1",
    );
    let c1 = fx.commit(
        Some(m1),
        &[("img.bin", Some("\0Y")), ("a.txt", Some("a2\n"))],
        "c1",
    );
    let c2 = fx.commit(
        Some(c1),
        &[("img.bin", Some("\0Z")), ("b.txt", Some("b2\n"))],
        "c2",
    );
    let m2 = fx.commit(Some(m1), &[("m.txt", Some("m\n"))], "m2");
    let n1 = fx.commit(
        Some(m2),
        &[("img.bin", Some("\0W")), ("a.txt", Some("a2\n"))],
        "c1",
    );
    let n2 = fx.commit(
        Some(n1),
        &[("img.bin", Some("\0Z")), ("b.txt", Some("b2\n"))],
        "c2",
    );
    let home = tempfile::tempdir().unwrap();
    let ids = [m1, c2, m2, n2].map(|i| i.to_string());
    let mut args: Vec<&str> = ids.iter().map(String::as_str).collect();
    args.extend(["--pr", "none", "--json"]);
    let out = stw(&fx.path, home.path(), &args);
    let text = String::from_utf8_lossy(&out.stdout).into_owned();
    assert_eq!(out.status.code(), Some(1), "{text}");
    let v: serde_json::Value = serde_json::from_str(&text).unwrap();
    assert_eq!(v["range_diff"]["state"], "equal", "{text}");
    assert_eq!(v["patch_id"]["state"], "equal", "{text}");
    assert_eq!(v["commit_patches"]["state"], "differs", "{text}");
    assert_eq!(v["commit_patches"]["differing"][0], 1);
    assert_eq!(v["carry_over"], false);
    assert!(v["line"].is_null());
}
