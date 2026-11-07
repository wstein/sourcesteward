//! Snapshots of the user-visible text: `stw --help` and one JSON document. Review changes with
//! `cargo insta review` (or accept with `INSTA_UPDATE=always`).
mod common;
use common::*;

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
