//! `stw tidy apply` and `stw tidy restore` as a process: flags, JSON, exit codes, default manifest place,
//! reflog identity of the restored branch.
use std::process::{Command, Output};

use crate::common::*;
use serde_json::Value;

fn build() -> Fx {
    let fx = Fx::new();
    let c0 = fx.commit(None, &[("a.txt", Some("a\n"))], "c0");
    let c1 = fx.commit(Some(c0), &[("a.txt", Some("b\n"))], "main work");
    fx.remote("main", c1);
    fx.origin_head("main");
    fx.branch("main", c1);
    fx.set_head("main");
    fx.checkout_head();
    fx.branch("merged-a", c0);
    fx.branch("merged-b", c0);
    let u = fx.commit(Some(c0), &[("u.txt", Some("u\n"))], "unmerged");
    fx.branch("unmerged", u);
    fx
}

fn stw(home: &std::path::Path, args: &[&str]) -> Output {
    Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_clear()
        .env("HOME", home)
        .env("GIT_CONFIG_GLOBAL", "/dev/null")
        .env("GIT_CONFIG_SYSTEM", "/dev/null")
        .env("GIT_CONFIG_NOSYSTEM", "1")
        .args(args)
        .output()
        .unwrap()
}

fn json(o: &Output) -> Value {
    serde_json::from_slice(&o.stdout).unwrap_or_else(|e| {
        panic!(
            "not JSON ({e}): {} {}",
            String::from_utf8_lossy(&o.stdout),
            String::from_utf8_lossy(&o.stderr)
        )
    })
}

fn has(fx: &Fx, b: &str) -> bool {
    stw::repo::resolve(&fx.repo(), &format!("refs/heads/{b}")).is_some()
}

#[test]
fn tidy_plan_shows_the_plan_and_changes_nothing() {
    let fx = build();
    let home = scratch();
    let r = path_str(&fx.path);
    let o = stw(home.path(), &["tidy", "plan", "--offline", "--root", &r]);
    assert_eq!(
        o.status.code(),
        Some(0),
        "{}",
        String::from_utf8_lossy(&o.stderr)
    );
    let text = String::from_utf8_lossy(&o.stdout).into_owned();
    assert!(text.contains("delete  merged-a"), "{text}");
    assert!(text.contains("read-only: nothing was changed"), "{text}");
    assert!(has(&fx, "merged-a") && has(&fx, "merged-b"));
    assert!(
        std::fs::read_dir(home.path()).unwrap().next().is_none(),
        "no manifest or other file anywhere in HOME"
    );
    let j = json(&stw(
        home.path(),
        &["tidy", "plan", "--offline", "--json", "--root", &r],
    ));
    assert_eq!(j["command"], "tidy-plan");
    assert_eq!(j["read_only"], true);
    assert_eq!(j["summary"]["delete"], 2);
}

#[test]
fn tidy_apply_deletes_without_any_confirmation_flag() {
    let fx = build();
    let home = scratch();
    let r = path_str(&fx.path);
    let md = path_str(&home.path().join("m"));
    let j = json(&stw(
        home.path(),
        &[
            "tidy",
            "apply",
            "--offline",
            "--json",
            "--root",
            &r,
            "--manifest-dir",
            &md,
        ],
    ));
    assert_eq!(j["command"], "tidy-apply");
    assert!(j.get("dry_run").is_none(), "{j}");
    assert_eq!(j["read_only"], false);
    assert_eq!(j["schema_version"], 2);
    assert_eq!(j["summary"]["deleted"], 2);
    assert!(j["summary"].get("would_delete").is_none());
    assert!(!has(&fx, "merged-a") && !has(&fx, "merged-b"));
    assert!(has(&fx, "unmerged"));
    assert!(home.path().join("m").is_dir(), "the manifest is written");
    let dump = j.to_string();
    assert!(
        !dump.contains("next_steps") && !dump.contains("git -C"),
        "state only: {dump}"
    );
}

#[test]
fn bare_tidy_shows_usage_and_yes_is_not_a_flag() {
    let fx = build();
    let home = scratch();
    let r = path_str(&fx.path);
    // no subcommand: usage, exit 2, no plan, nothing changed
    for args in [
        vec!["tidy"],
        vec!["tidy", "--offline", "--root", &r],
        vec!["tidy", "--json", "--root", &r],
    ] {
        let o = stw(home.path(), &args);
        assert_eq!(o.status.code(), Some(2), "{args:?}");
        assert!(o.stdout.is_empty(), "{args:?} printed a plan");
        assert!(has(&fx, "merged-a"), "{args:?}");
    }
    let o = stw(home.path(), &["tidy"]);
    let err = String::from_utf8_lossy(&o.stderr);
    assert!(
        err.contains("Usage:") && err.contains("plan") && err.contains("apply"),
        "{err}"
    );
    assert!(err.contains("Exit codes:"), "{err}");
    for args in [
        vec!["tidy", "apply", "--yes", "--offline", "--root", &r],
        vec!["tidy", "plan", "--yes", "--offline", "--root", &r],
    ] {
        let o = stw(home.path(), &args);
        assert_eq!(o.status.code(), Some(2), "{args:?}");
        assert!(o.stdout.is_empty(), "{args:?}");
        assert!(has(&fx, "merged-a"), "{args:?} must delete nothing");
        let err = String::from_utf8_lossy(&o.stderr);
        assert!(err.contains("unexpected argument '--yes'"), "{err}");
    }
}

#[test]
fn plan_file_then_apply_then_restore() {
    let fx = build();
    let home = scratch();
    let r = path_str(&fx.path);
    let md = path_str(&home.path().join("m"));
    let plan = stw(
        home.path(),
        &["tidy", "plan", "--offline", "--json", "--root", &r],
    );
    let file = home.path().join("plan.json");
    std::fs::write(&file, &plan.stdout).unwrap();
    let f = path_str(&file);
    // merged-b moves after the plan
    let u = fx.tip("unmerged");
    fx.branch("merged-b", u);
    let o = stw(
        home.path(),
        &[
            "tidy",
            "apply",
            "--json",
            "--offline",
            "--root",
            &r,
            "--plan",
            &f,
            "--manifest-dir",
            &md,
        ],
    );
    assert_eq!(o.status.code(), Some(0));
    let j = json(&o);
    assert_eq!(j["summary"]["deleted"], 1);
    assert_eq!(j["summary"]["skipped"], 1);
    assert!(!has(&fx, "merged-a"));
    assert_eq!(fx.tip("merged-b"), u);
    let run = j["run"].as_str().unwrap().to_string();
    assert!(std::path::Path::new(j["repos"][0]["manifest"].as_str().unwrap()).is_file());
    // restore, twice
    let o = stw(
        home.path(),
        &[
            "tidy",
            "restore",
            "--json",
            "--manifest-dir",
            &md,
            "--run",
            &run,
        ],
    );
    assert_eq!(o.status.code(), Some(0));
    let j = json(&o);
    assert_eq!(j["command"], "tidy-restore");
    assert_eq!(j["summary"]["restored"], 1);
    assert!(!j.to_string().contains("git -C"), "state only");
    assert!(has(&fx, "merged-a"));
    let j = json(&stw(
        home.path(),
        &["tidy", "restore", "--json", "--manifest-dir", &md],
    ));
    assert_eq!(j["summary"]["already_present"], 1);
    assert_eq!(j["summary"]["restored"], 0);
}

#[test]
fn the_manifest_goes_to_the_user_data_directory_by_default() {
    let fx = build();
    let home = scratch();
    let r = path_str(&fx.path);
    let o = stw(home.path(), &["tidy", "apply", "--offline", "--root", &r]);
    assert_eq!(o.status.code(), Some(0));
    let dir = if cfg!(target_os = "macos") {
        home.path()
            .join("Library/Application Support/stw/manifests")
    } else {
        home.path().join(".local/share/stw/manifests")
    };
    let n = std::fs::read_dir(&dir).unwrap().count();
    assert_eq!(n, 1, "one manifest file in {}", dir.display());
    // the manifest is not kept in the repository; only the op log directory (stw's own ref lines) is there
    let inside: Vec<_> = std::fs::read_dir(fx.path.join(".git/stw"))
        .map(|d| d.flatten().map(|e| e.file_name()).collect())
        .unwrap_or_default();
    assert!(inside.iter().all(|n| n == "oplog"), "{inside:?}");
    let text = String::from_utf8_lossy(&o.stdout);
    assert!(
        !text.contains("next:") && !text.contains("git "),
        "no commands in the output: {text}"
    );
}

#[test]
fn a_file_that_is_not_a_plan_is_a_usage_error() {
    let fx = build();
    let home = scratch();
    let file = home.path().join("x.json");
    std::fs::write(&file, "{\"command\": \"status\"}").unwrap();
    let o = stw(
        home.path(),
        &[
            "tidy",
            "apply",
            "--offline",
            "--root",
            &path_str(&fx.path),
            "--plan",
            &path_str(&file),
        ],
    );
    assert_eq!(o.status.code(), Some(2));
    assert!(has(&fx, "merged-a"));
}

#[test]
fn restore_without_any_manifest_is_a_usage_error() {
    let home = scratch();
    let o = stw(
        home.path(),
        &[
            "tidy",
            "restore",
            "--manifest-dir",
            &path_str(&home.path().join("none")),
        ],
    );
    assert_eq!(o.status.code(), Some(2));
}

#[test]
fn restore_exits_1_when_something_was_not_restored() {
    let fx = build();
    let home = scratch();
    let r = path_str(&fx.path);
    let md = path_str(&home.path().join("m"));
    stw(
        home.path(),
        &[
            "tidy",
            "apply",
            "--offline",
            "--root",
            &r,
            "--manifest-dir",
            &md,
        ],
    );
    let u = fx.tip("unmerged");
    fx.branch("merged-a", u);
    let o = stw(home.path(), &["tidy", "restore", "--manifest-dir", &md]);
    assert_eq!(o.status.code(), Some(1));
    assert!(String::from_utf8_lossy(&o.stdout).contains("not-overwritten"));
}

#[test]
fn the_restored_branch_carries_the_reflog_identity_rules() {
    for (setup, expect) in [
        (None, "stw <stw@localhost>"),
        (
            Some(("Cfg Name", "cfg@example.com")),
            "Cfg Name <cfg@example.com>",
        ),
    ] {
        let fx = build();
        fx.strip_identity();
        if let Some((n, e)) = setup {
            fx.set_identity(n, e);
        }
        let home = scratch();
        let r = path_str(&fx.path);
        let md = path_str(&home.path().join("m"));
        stw(
            home.path(),
            &[
                "tidy",
                "apply",
                "--offline",
                "--root",
                &r,
                "--manifest-dir",
                &md,
            ],
        );
        assert!(!has(&fx, "merged-a"));
        let o = stw(home.path(), &["tidy", "restore", "--manifest-dir", &md]);
        assert_eq!(o.status.code(), Some(0));
        assert_eq!(fx.last_reflog_identity("refs/heads/merged-a"), expect);
    }
}

#[test]
fn backup_days_reaches_plan_and_apply() {
    let fx = build();
    let c0 = fx.tip("merged-a");
    let b = fx.commit(Some(c0), &[("o.txt", Some("old\n"))], "old backup");
    fx.branch("backup/x", b); // the fixture commit is from 2023: old for 7 days, not for 100000
    let home = scratch();
    let r = path_str(&fx.path);
    let md = path_str(&home.path().join("m"));
    let plan = |d: &str| {
        json(&stw(
            home.path(),
            &[
                "tidy",
                "plan",
                "--offline",
                "--json",
                "--all",
                "--root",
                &r,
                "--backup-days",
                d,
            ],
        ))
    };
    let find = |j: &Value| {
        j["repos"][0]["branches"]
            .as_array()
            .unwrap()
            .iter()
            .find(|b| b["branch"] == "backup/x")
            .unwrap()
            .clone()
    };
    assert_eq!(find(&plan("7"))["decision"], "delete");
    assert_eq!(find(&plan("7"))["proof"], "backup-older-than-days");
    assert_eq!(find(&plan("100000"))["decision"], "keep");
    let o = stw(
        home.path(),
        &[
            "tidy",
            "apply",
            "--offline",
            "--root",
            &r,
            "--manifest-dir",
            &md,
            "--backup-days",
            "100000",
        ],
    );
    assert_eq!(o.status.code(), Some(0));
    assert!(has(&fx, "backup/x"), "young enough for 100000 days");
    stw(
        home.path(),
        &[
            "tidy",
            "apply",
            "--offline",
            "--root",
            &r,
            "--manifest-dir",
            &md,
        ],
    );
    assert!(!has(&fx, "backup/x"));
}

#[test]
fn apply_and_restore_call_no_git_gh_or_ssh() {
    use std::os::unix::fs::PermissionsExt;
    let fx = build();
    let home = scratch();
    let shims = home.path().join("shims");
    std::fs::create_dir_all(&shims).unwrap();
    let log = home.path().join("calls.log");
    for t in ["git", "gh", "ssh", "git-upload-pack"] {
        let p = shims.join(t);
        std::fs::write(
            &p,
            format!(
                "#!/bin/sh\necho \"{t} $*\" >> '{}'\nexit 99\n",
                log.display()
            ),
        )
        .unwrap();
        std::fs::set_permissions(&p, std::fs::Permissions::from_mode(0o755)).unwrap();
    }
    let run = |args: &[&str]| {
        Command::new(env!("CARGO_BIN_EXE_stw"))
            .env_clear()
            .env("HOME", home.path())
            .env("PATH", format!("{}:/usr/bin:/bin", shims.display()))
            .args(args)
            .output()
            .unwrap()
    };
    let r = path_str(&fx.path);
    let md = path_str(&home.path().join("m"));
    assert_eq!(
        run(&[
            "tidy",
            "apply",
            "--offline",
            "--root",
            &r,
            "--manifest-dir",
            &md
        ])
        .status
        .code(),
        Some(0)
    );
    assert!(!has(&fx, "merged-a"));
    assert_eq!(
        run(&["tidy", "restore", "--manifest-dir", &md])
            .status
            .code(),
        Some(0)
    );
    assert!(has(&fx, "merged-a"));
    assert!(
        !log.exists(),
        "a shim was called: {:?}",
        std::fs::read_to_string(&log)
    );
}
