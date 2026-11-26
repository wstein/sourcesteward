//! Snapshots of the user-visible text: `stw --help` and one JSON document. Review changes with
//! `cargo insta review` (or accept with `INSTA_UPDATE=always`).
mod common;
use common::*;

#[test]
fn agent_hint_appears_once_at_the_end_of_short_long_and_nested_help() {
    let hint = "For AI agents: https://wstein.github.io/sourcesteward/agents/";
    for args in [
        &["--help"][..],
        &["watch", "-h"][..],
        &["watch", "--help"][..],
        &["help", "watch"][..],
        &["workspace", "checkpoint", "-h"][..],
    ] {
        let help = stw_stdout(args);
        assert_eq!(help.matches(hint).count(), 1, "{args:?}: {help}");
        assert!(help.trim_end().ends_with(hint), "{args:?}: {help}");
    }
}

#[test]
fn help_text() {
    insta::assert_snapshot!(stw_stdout(&["--help"]));
}

#[test]
fn status_json_of_a_fixture_workspace() {
    let fx = status_fixture();
    let pretty = status_json(&fx, &path_str(&fx.path));
    insta::with_settings!({ filters => vec![
        (r#""engine": "[^"]*""#, r#""engine": "[ENGINE]""#),
    ] }, {
        insta::assert_snapshot!(pretty);
    });
}

#[test]
fn status_text_of_a_fixture_workspace() {
    let fx = status_fixture();
    fx.write("untracked.txt", "x\n");
    insta::assert_snapshot!(stw_stdout(&[
        "status",
        "--root",
        &path_str(&fx.path),
        "--offline"
    ]));
}

#[test]
fn command_reference_matches_the_public_cli() {
    let reference = stw_stdout(&["dev", "command-reference"]);
    assert!(
        reference == include_str!("../docs/command-reference.md"),
        "regenerate docs/command-reference.md with stw dev command-reference"
    );
}
