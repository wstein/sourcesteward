//! `stw status` and the CI of the default branch: the text line, the attention reason `main-red`, the notice
//! `ci-not-read`, the exit code, and `status PATH`. The read itself is in `status_ci_main`.
use crate::common::FixtureCommand as _;
use std::process::Output;

use super::status_ci_main::*;
use crate::common::*;
use serde_json::{json, Value};

fn text(api: &FakeRoutes, extra: &[&str]) -> (String, i32) {
    let fx = status_fixture();
    let root = path_str(&fx.path);
    let mut args = vec!["status", "--root", &root];
    args.extend_from_slice(extra);
    let o = stw(Some(&api.url), &args);
    (out(&o), o.status.code().unwrap())
}

fn out(o: &Output) -> String {
    String::from_utf8_lossy(&o.stdout).into_owned()
}

fn block<'a>(t: &'a str, head: &str) -> &'a str {
    t.split("\n\n")
        .find(|b| b.starts_with(head))
        .unwrap_or_else(|| panic!("no block {head:?} in:\n{t}"))
}

fn red_via_checks() -> Scn {
    Scn {
        check_runs: checks(vec![cr("Test", "completed", Some("failure"))]),
        ..Scn::default()
    }
}

#[test]
fn the_line_of_a_repository_says_the_class_the_head_and_the_way() {
    let green = Scn {
        check_runs: checks(vec![cr("Test", "completed", Some("success"))]),
        ..Scn::default()
    };
    let (t, code) = text(&fake(green), &[]);
    assert_eq!(code, 0, "{t}");
    assert!(
        t.contains("\n    main CI green (main@163ada585103, via check runs)\n"),
        "{t}"
    );
    let via_actions = Scn {
        check_runs: forbidden(),
        actions: actions(vec![wr(
            1,
            7,
            "push",
            "2026-11-17T17:32:44Z",
            1,
            "completed",
            Some("success"),
        )]),
        ..Scn::default()
    };
    let (t, _) = text(&fake(via_actions), &[]);
    assert!(
        t.contains("\n    main CI green (main@163ada585103, via Actions runs)\n"),
        "{t}"
    );
    let none = Scn {
        check_runs: forbidden(),
        ..Scn::default()
    };
    let (t, code) = text(&fake(none), &[]);
    assert_eq!(code, 0);
    assert!(
        t.contains("\n    main CI none (main@163ada585103, via Actions runs)\n"),
        "{t}"
    );
    let pending = Scn {
        check_runs: checks(vec![cr("Test", "in_progress", None)]),
        ..Scn::default()
    };
    let (t, _) = text(&fake(pending), &[]);
    assert!(
        t.contains("\n    main CI pending (main@163ada585103, via check runs)\n"),
        "{t}"
    );
}

#[test]
fn only_a_red_head_needs_action_and_the_other_classes_do_not() {
    for (s, label) in [
        (
            Scn {
                check_runs: checks(vec![cr("T", "completed", Some("success"))]),
                ..Scn::default()
            },
            "green",
        ),
        (
            Scn {
                check_runs: checks(vec![cr("T", "in_progress", None)]),
                ..Scn::default()
            },
            "pending",
        ),
        (Scn::default(), "none"),
        (
            Scn {
                check_runs: forbidden(),
                ..Scn::default()
            },
            "none via actions",
        ),
        (
            Scn {
                repo: (404, json!({})),
                ..Scn::default()
            },
            "not read",
        ),
    ] {
        let api = fake(s);
        let (t, _) = text(&api, &[]);
        assert!(
            block(&t, "Needs action (").starts_with("Needs action (0)\n  nothing"),
            "{label}: {t}"
        );
        let (v, _) = json_of(&api, &[]);
        assert_eq!(v["repos"][0]["attention"], json!(false), "{label}");
        assert_eq!(v["summary"]["attention"], json!(0), "{label}");
    }
    let api = fake(red_via_checks());
    let (t, code) = text(&api, &[]);
    assert_eq!(code, 0, "a red default branch is no exit code");
    assert!(
        block(&t, "Needs action (").starts_with("Needs action (1)\n"),
        "{t}"
    );
    assert!(block(&t, "Needs action (").contains("main-red"), "{t}");
    assert!(
        t.contains("main CI red (main@163ada585103, via check runs)"),
        "{t}"
    );
    assert!(
        t.trim_end().ends_with("1 of 1 repositories need attention"),
        "{t}"
    );
    let (v, _) = json_of(&api, &[]);
    assert_eq!(v["repos"][0]["attention"], json!(true));
    assert_eq!(v["repos"][0]["reasons"], json!(["main-red"]));
    assert_eq!(v["summary"]["attention"], json!(1));
}

#[test]
fn long_explains_the_red_head_without_a_command() {
    let (t, _) = text(&fake(red_via_checks()), &["--long"]);
    let line = t
        .lines()
        .find(|l| l.contains("- main-red:"))
        .unwrap_or_else(|| panic!("{t}"));
    assert!(
        line.contains("main") && line.contains("163ada585103") && line.contains("failing"),
        "{line}"
    );
    assert!(!line.contains("gh ") && !line.contains("stw "), "{line}");
}

#[test]
fn a_ci_read_that_failed_is_a_line_a_notice_and_exit_0() {
    let api = fake(Scn {
        check_runs: forbidden(),
        actions: forbidden(),
        ..Scn::default()
    });
    let (t, code) = text(&api, &[]);
    assert_eq!(code, 0, "{t}");
    assert!(t.contains("\n    main CI not read (forbidden: "), "{t}");
    let n = block(&t, "Notices (");
    assert!(n.starts_with("Notices (1)\n"), "{n}");
    assert!(n.contains("default branch CI not read (forbidden: "), "{n}");
    assert!(
        n.contains("Checks: Read (classic token: scope repo) or Actions: Read"),
        "{n}"
    );
    let (v, _) = json_of(&api, &[]);
    let note = &v["notices"][0];
    assert_eq!(v["notices"].as_array().unwrap().len(), 1, "{v}");
    assert_eq!(note["code"], json!("ci-not-read"));
    assert_eq!(note["source"], json!("ci"));
    assert_eq!(note["count"], json!(1));
    assert_eq!(note["repos"], json!([v["repos"][0]["name"]]));
    let name = v["repos"][0]["name"].as_str().unwrap();
    assert!(
        note["message"]
            .as_str()
            .unwrap()
            .starts_with(&format!("{name}: default branch CI not read (forbidden")),
        "{note}"
    );
    assert_eq!(v["repos"][0]["attention"], json!(false));
}

#[test]
fn the_notice_of_the_ci_comes_after_the_issues_notice_of_its_repository() {
    // pulls and issues answer 500, the CI 403/403: pulls, issues, then ci
    let api = FakeRoutes::start(|path, _| match path {
        "/repos/test/fixture/pulls" | "/repos/test/fixture/issues" => (500, vec![], "{}".into()),
        "/repos/test/fixture" => (200, vec![], json!({"default_branch": "main"}).to_string()),
        "/repos/test/fixture/branches/main" => {
            (200, vec![], json!({"commit": {"sha": SHA}}).to_string())
        }
        _ => (403, vec![], "{}".into()),
    });
    let (v, _) = json_of(&api, &[]);
    let codes: Vec<&str> = v["notices"]
        .as_array()
        .unwrap()
        .iter()
        .map(|n| n["code"].as_str().unwrap())
        .collect();
    assert_eq!(
        codes,
        ["prs-not-read", "issues-not-read", "ci-not-read"],
        "{v}"
    );
}

#[test]
fn a_read_that_worked_has_no_notice() {
    let (t, _) = text(
        &fake(Scn {
            check_runs: forbidden(),
            ..Scn::default()
        }),
        &[],
    );
    assert!(
        block(&t, "Notices (").starts_with("Notices (0)\n  none"),
        "{t}"
    );
}

#[test]
fn offline_and_without_a_github_origin_print_no_ci_line() {
    let fx = status_fixture();
    let api = fake(Scn::default());
    let o = stw(
        Some(&api.url),
        &["status", "--root", &path_str(&fx.path), "--offline"],
    );
    assert!(!out(&o).contains("main CI"), "{}", out(&o));
    assert!(api.hits().is_empty());
}

fn path_run(api: &FakeRoutes, extra: &[&str]) -> (String, Value) {
    let fx = status_fixture();
    let run = |json: bool| {
        let mut c = std::process::Command::new(env!("CARGO_BIN_EXE_stw"));
        c.fixture_env_clear()
            .env("HOME", "/nonexistent")
            .env("GH_TOKEN", "SECRETTOKEN-0123456789")
            .env("STW_KEYCHAIN_SERVICE", "stw-test-no-such-service")
            .env("STW_GITHUB_API_URL", &api.url)
            .current_dir(&fx.path)
            .args(["status", "."])
            .args(extra);
        if json {
            c.arg("--json");
        }
        c.output().unwrap()
    };
    let t = out(&run(false));
    let v: Value = serde_json::from_slice(&run(true).stdout).unwrap();
    (t, v)
}

#[test]
fn status_path_carries_the_ci_main_in_the_json_the_notice_and_the_attention() {
    let (t, v) = path_run(&fake(red_via_checks()), &[]);
    assert_eq!(v["schema_version"], json!(2));
    assert_eq!(v["repos"][0]["ci_main"]["class"], json!("red"));
    assert_eq!(v["repos"][0]["reasons"], json!(["main-red"]));
    let row = t.lines().find(|l| l.contains("stack")).unwrap();
    assert!(row.starts_with('!'), "{t}");
    assert!(row.trim_end().ends_with("| main red"), "{row}");
    assert!(
        t.trim_end().ends_with("1 of 1 repositories need attention"),
        "{t}"
    );

    let green = Scn {
        check_runs: checks(vec![cr("T", "completed", Some("success"))]),
        ..Scn::default()
    };
    let (t, v) = path_run(&fake(green), &[]);
    assert_eq!(v["repos"][0]["ci_main"]["class"], json!("green"));
    let row = t.lines().find(|l| l.contains("stack")).unwrap();
    assert!(
        row.trim_end().ends_with("PRs 0 | Issues 0"),
        "no cell for a class that needs no action: {row}"
    );

    let (t, v) = path_run(
        &fake(Scn {
            check_runs: forbidden(),
            actions: forbidden(),
            ..Scn::default()
        }),
        &[],
    );
    assert_eq!(v["notices"][0]["code"], json!("ci-not-read"));
    assert!(t.contains("default branch CI not read (forbidden"), "{t}");
}
