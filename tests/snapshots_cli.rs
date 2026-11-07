//! Snapshots of the user-visible text: `stw --help` and one JSON document. Review changes with
//! `cargo insta review` (or accept with `INSTA_UPDATE=always`).
mod common;
use std::process::Command;

use common::*;

fn stw(args: &[&str]) -> String {
    let o = Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_remove("GH_TOKEN")
        .env_remove("GITHUB_TOKEN")
        .env_remove("STW_ROOT")
        .env("HOME", "/nonexistent")
        .env("STW_KEYCHAIN_SERVICE", "stw-test-no-such-service")
        .args(args)
        .output()
        .unwrap();
    assert_eq!(o.status.code(), Some(0), "{args:?}");
    String::from_utf8(o.stdout).unwrap()
}

#[test]
fn help_text() {
    insta::assert_snapshot!(stw(&["--help"]));
}

fn fixture() -> Fx {
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

/// Pretty JSON of `stw status` for `root_arg`; the fixture root is redacted.
fn status_json(fx: &Fx, root_arg: &str) -> String {
    let out = stw(&["status", "--root", root_arg, "--offline", "--json"]);
    let pretty =
        serde_json::to_string_pretty(&serde_json::from_str::<serde_json::Value>(&out).unwrap())
            .unwrap();
    pretty.replace(&path_str(&fx.path), "[ROOT]")
}

#[test]
fn status_json_of_a_fixture_workspace() {
    let fx = fixture();
    let pretty = status_json(&fx, &path_str(&fx.path));
    insta::with_settings!({ filters => vec![
        (r#""engine": "[^"]*""#, r#""engine": "[ENGINE]""#),
    ] }, {
        insta::assert_snapshot!(pretty);
    });
}

/// stw canonicalizes `--root`. With a temp dir behind a symlink (macOS: /var -> /private/var) the fixture path is
/// not the path stw prints; the snapshot must still be the same.
#[cfg(unix)]
#[test]
fn status_json_with_the_temp_dir_behind_a_symlink() {
    let real = scratch();
    let link = real.path().join("link");
    std::fs::create_dir(real.path().join("target")).unwrap();
    std::os::unix::fs::symlink(real.path().join("target"), &link).unwrap();
    let old = std::env::var_os("TMPDIR");
    std::env::set_var("TMPDIR", &link);
    let fx = fixture();
    match old {
        Some(v) => std::env::set_var("TMPDIR", v),
        None => std::env::remove_var("TMPDIR"),
    }
    assert!(fx.path.starts_with(&link), "fixture is not behind the link");
    let pretty = status_json(&fx, &path_str(&fx.path));
    insta::with_settings!({ filters => vec![
        (r#""engine": "[^"]*""#, r#""engine": "[ENGINE]""#),
    ] }, {
        insta::assert_snapshot!("status_json_of_a_fixture_workspace", pretty);
    });
}
