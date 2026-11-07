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

#[test]
fn status_json_of_a_fixture_workspace() {
    let fx = Fx::new();
    let c0 = fx.commit(None, &[("a.txt", Some("a\n"))], "c0");
    fx.branch("main", c0);
    fx.remote("main", c0);
    fx.origin_head("main");
    fx.track("main");
    fx.set_head("main");
    fx.checkout_head();
    let root = path_str(&fx.path);
    let out = stw(&["status", "--root", &root, "--offline", "--json"]);
    let pretty =
        serde_json::to_string_pretty(&serde_json::from_str::<serde_json::Value>(&out).unwrap())
            .unwrap();
    insta::with_settings!({ filters => vec![
        (regex_escape(&root).as_str(), "[ROOT]"),
        (r#""engine": "[^"]*""#, r#""engine": "[ENGINE]""#),
    ] }, {
        insta::assert_snapshot!(pretty);
    });
}

fn regex_escape(s: &str) -> String {
    s.chars()
        .flat_map(|c| {
            if c.is_ascii_alphanumeric() || c == '/' || c == '_' || c == '-' {
                vec![c]
            } else {
                vec!['\\', c]
            }
        })
        .collect()
}
