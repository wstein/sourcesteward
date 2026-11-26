//! `stw status`: the `ci-main` source, the CI class of the head of the default branch (port of Go's `ci-main`),
//! with the Actions runs as the way when the token cannot read check runs. Read and JSON; the text, the
//! attention reason and the notice are in `status_ci_main_text`.
use crate::common::FixtureCommand as _;
use std::process::{Command, Output};

use crate::common::*;
use serde_json::{json, Value};

const TOKEN: &str = "SECRETTOKEN-0123456789";
pub const SHA: &str = "163ada5851030a900b58162e5b47fe15c4c4aac8";
const OTHER: &str = "0123456789abcdef0123456789abcdef01234567";

/// What the fake GitHub answers; `(status, body)` per document.
#[derive(Clone)]
pub struct Scn {
    pub repo: (u16, Value),
    pub branch: (u16, Value),
    pub check_runs: (u16, Value),
    pub actions: (u16, Value),
    pub statuses: (u16, Value),
}

impl Default for Scn {
    fn default() -> Scn {
        Scn {
            repo: (200, json!({"default_branch": "main"})),
            branch: (200, json!({"commit": {"sha": SHA}})),
            check_runs: (200, json!({"total_count": 0, "check_runs": []})),
            actions: (200, json!({"total_count": 0, "workflow_runs": []})),
            statuses: (200, json!([])),
        }
    }
}

pub fn cr(name: &str, status: &str, conclusion: Option<&str>) -> Value {
    json!({"id": 1, "name": name, "status": status, "conclusion": conclusion, "head_sha": SHA})
}

pub fn checks(runs: Vec<Value>) -> (u16, Value) {
    (200, json!({"total_count": runs.len(), "check_runs": runs}))
}

pub fn wr(
    id: u64,
    wf: u64,
    event: &str,
    created: &str,
    attempt: u64,
    status: &str,
    conclusion: Option<&str>,
) -> Value {
    json!({"id": id, "workflow_id": wf, "name": format!("wf{wf}"), "event": event, "status": status,
           "conclusion": conclusion, "head_sha": SHA, "created_at": created, "run_attempt": attempt})
}

pub fn actions(runs: Vec<Value>) -> (u16, Value) {
    (
        200,
        json!({"total_count": runs.len(), "workflow_runs": runs}),
    )
}

pub fn st(context: &str, state: &str) -> Value {
    json!({"id": 1, "state": state, "context": context, "description": "", "created_at": "2026-11-17T10:00:00Z",
           "updated_at": "2026-11-17T10:00:00Z"})
}

pub fn forbidden() -> (u16, Value) {
    (
        403,
        json!({"message": "Resource not accessible by personal access token"}),
    )
}

pub fn fake(s: Scn) -> FakeRoutes {
    FakeRoutes::start(move |path, _| {
        let (status, body) = match path {
            "/repos/test/fixture" => s.repo.clone(),
            "/repos/test/fixture/branches/main" => s.branch.clone(),
            p if p == format!("/repos/test/fixture/commits/{SHA}/check-runs") => {
                s.check_runs.clone()
            }
            p if p == format!("/repos/test/fixture/commits/{SHA}/statuses") => s.statuses.clone(),
            "/repos/test/fixture/actions/runs" => s.actions.clone(),
            "/repos/test/fixture/pulls" | "/repos/test/fixture/issues" => (200, json!([])),
            _ => (404, json!({})),
        };
        (status, vec![], body.to_string())
    })
}

pub fn stw(api: Option<&str>, args: &[&str]) -> Output {
    let mut c = Command::new(env!("CARGO_BIN_EXE_stw"));
    c.fixture_env_clear()
        .env("HOME", "/nonexistent")
        .env("STW_KEYCHAIN_SERVICE", "stw-test-no-such-service")
        .env("GH_TOKEN", TOKEN);
    if let Some(a) = api {
        c.env("STW_GITHUB_API_URL", a);
    }
    c.args(args).output().unwrap()
}

pub fn json_of(api: &FakeRoutes, extra: &[&str]) -> (Value, i32) {
    let fx = status_fixture();
    let mut args = vec!["status", "--root"];
    let root = path_str(&fx.path);
    args.push(&root);
    args.push("--json");
    args.extend_from_slice(extra);
    let o = stw(Some(&api.url), &args);
    let v: Value = serde_json::from_slice(&o.stdout)
        .unwrap_or_else(|e| panic!("{e}: {}", String::from_utf8_lossy(&o.stdout)));
    (v, o.status.code().unwrap())
}

fn ci(v: &Value) -> &Value {
    &v["repos"][0]["ci_main"]
}

fn hits_of(api: &FakeRoutes, what: &str) -> usize {
    api.hits().iter().filter(|h| h.contains(what)).count()
}

fn class_via(s: Scn) -> (String, String) {
    let api = fake(s);
    let (v, code) = json_of(&api, &[]);
    assert_eq!(code, 0, "{v}");
    let c = ci(&v);
    assert_eq!(c["read"], json!(true), "{c}");
    (
        c["class"].as_str().unwrap().to_string(),
        c["source"].as_str().unwrap().to_string(),
    )
}

#[test]
fn check_runs_give_the_class_green_pending_or_red() {
    let cases: [(Vec<Value>, &str); 9] = [
        (vec![cr("a", "completed", Some("success"))], "green"),
        (
            vec![
                cr("a", "completed", Some("success")),
                cr("b", "completed", Some("skipped")),
                cr("c", "completed", Some("neutral")),
            ],
            "green",
        ),
        (vec![cr("a", "in_progress", None)], "pending"),
        (
            vec![
                cr("a", "queued", None),
                cr("b", "completed", Some("success")),
            ],
            "pending",
        ),
        (vec![cr("a", "completed", Some("failure"))], "red"),
        (vec![cr("a", "completed", Some("cancelled"))], "red"),
        (vec![cr("a", "completed", Some("timed_out"))], "red"),
        (vec![cr("a", "completed", Some("action_required"))], "red"),
        (
            vec![
                cr("a", "in_progress", None),
                cr("b", "completed", Some("failure")),
            ],
            "red",
        ),
    ];
    for (runs, want) in cases {
        let s = Scn {
            check_runs: checks(runs.clone()),
            ..Scn::default()
        };
        assert_eq!(
            class_via(s),
            (want.to_string(), "check-runs".into()),
            "{runs:?}"
        );
    }
}

#[test]
fn no_check_run_and_no_status_is_the_class_none_not_a_read_error() {
    assert_eq!(
        class_via(Scn::default()),
        ("none".to_string(), "check-runs".into())
    );
}

#[test]
fn the_json_names_the_branch_the_head_and_the_way() {
    let s = Scn {
        check_runs: checks(vec![cr("a", "completed", Some("success"))]),
        ..Scn::default()
    };
    let api = fake(s);
    let (v, code) = json_of(&api, &[]);
    assert_eq!(code, 0);
    assert_eq!(v["schema_version"], json!(2));
    assert_eq!(
        ci(&v),
        &json!({"read": true, "error": null, "branch": "main", "head": SHA, "class": "green", "source": "check-runs"})
    );
    // check runs read: no Actions request
    assert_eq!(hits_of(&api, "/actions/"), 0, "{:?}", api.hits());
    assert_eq!(hits_of(&api, "/check-runs"), 1);
    assert_eq!(hits_of(&api, "/statuses"), 1);
}

#[test]
fn a_403_on_check_runs_falls_back_to_the_actions_runs_of_the_head() {
    let ok = wr(
        1,
        7,
        "push",
        "2026-11-17T17:32:44Z",
        1,
        "completed",
        Some("success"),
    );
    let s = Scn {
        check_runs: forbidden(),
        actions: actions(vec![ok]),
        ..Scn::default()
    };
    let api = fake(s);
    let (v, code) = json_of(&api, &[]);
    assert_eq!(code, 0);
    assert_eq!(
        ci(&v),
        &json!({"read": true, "error": null, "branch": "main", "head": SHA, "class": "green", "source": "actions-runs"})
    );
    let h = api.hits();
    let at = |what: &str| {
        h.iter()
            .position(|l| l.contains(what))
            .unwrap_or(usize::MAX)
    };
    assert!(at("/check-runs") < at("/actions/runs"), "{h:?}");
    assert!(
        h.iter()
            .any(|l| l.contains(&format!("/actions/runs?head_sha={SHA}&per_page=100"))),
        "{h:?}"
    );
    assert_eq!(hits_of(&api, "/statuses"), 1, "{h:?}");
}

#[test]
fn the_actions_runs_are_classified_with_the_rule_of_the_check_runs() {
    let cases: [(&str, &str, Option<&str>, &str); 10] = [
        ("success", "completed", Some("success"), "green"),
        ("skipped", "completed", Some("skipped"), "green"),
        ("failure", "completed", Some("failure"), "red"),
        ("cancelled", "completed", Some("cancelled"), "red"),
        ("timed_out", "completed", Some("timed_out"), "red"),
        (
            "startup_failure",
            "completed",
            Some("startup_failure"),
            "red",
        ),
        (
            "action_required",
            "completed",
            Some("action_required"),
            "red",
        ),
        ("in_progress", "in_progress", None, "pending"),
        ("queued", "queued", None, "pending"),
        ("waiting", "waiting", None, "pending"),
    ];
    for (label, status, conclusion, want) in cases {
        let s = Scn {
            check_runs: forbidden(),
            actions: actions(vec![wr(
                1,
                7,
                "push",
                "2026-11-17T17:32:44Z",
                1,
                status,
                conclusion,
            )]),
            ..Scn::default()
        };
        assert_eq!(
            class_via(s),
            (want.to_string(), "actions-runs".into()),
            "{label}"
        );
    }
}

#[test]
fn no_actions_run_is_the_state_none_and_neither_forbidden_nor_a_finding() {
    let s = Scn {
        check_runs: forbidden(),
        ..Scn::default()
    };
    let api = fake(s);
    let (v, code) = json_of(&api, &[]);
    assert_eq!(code, 0);
    assert_eq!(ci(&v)["read"], json!(true));
    assert_eq!(ci(&v)["error"], Value::Null);
    assert_eq!(ci(&v)["class"], json!("none"));
    assert_eq!(ci(&v)["source"], json!("actions-runs"));
}

#[test]
fn both_ways_refused_stays_forbidden_and_names_both_rights() {
    let s = Scn {
        check_runs: forbidden(),
        actions: forbidden(),
        ..Scn::default()
    };
    let api = fake(s);
    let (v, code) = json_of(&api, &[]);
    assert_eq!(code, 0, "a failed GitHub read is no exit code in Rust");
    let c = ci(&v);
    assert_eq!(c["read"], json!(false));
    for k in ["branch", "head", "class", "source"] {
        assert_eq!(c[k], Value::Null, "{k}");
    }
    let e = c["error"].as_str().unwrap();
    assert!(e.starts_with("forbidden"), "{e}");
    assert!(
        e.contains("Checks: Read (classic token: scope repo)") && e.contains("Actions: Read"),
        "{e}"
    );
    assert!(!e.contains(TOKEN));
}

#[test]
fn the_newest_run_of_a_workflow_and_event_counts() {
    let new_ok = wr(
        2,
        7,
        "push",
        "2026-11-17T12:00:00Z",
        1,
        "completed",
        Some("success"),
    );
    let old_bad = wr(
        1,
        7,
        "push",
        "2026-11-17T10:00:00Z",
        1,
        "completed",
        Some("failure"),
    );
    let s = Scn {
        check_runs: forbidden(),
        actions: actions(vec![old_bad.clone(), new_ok.clone()]),
        ..Scn::default()
    };
    assert_eq!(class_via(s).0, "green");
    // the order of the answer does not matter
    let s = Scn {
        check_runs: forbidden(),
        actions: actions(vec![new_ok, old_bad]),
        ..Scn::default()
    };
    assert_eq!(class_via(s).0, "green");
    // the time ranks before the id: an older failure with a higher id does not win
    let old_bad_high_id = wr(
        9,
        7,
        "push",
        "2026-11-17T10:00:00Z",
        1,
        "completed",
        Some("failure"),
    );
    let new_ok_low_id = wr(
        2,
        7,
        "push",
        "2026-11-17T12:00:00Z",
        1,
        "completed",
        Some("success"),
    );
    let s = Scn {
        check_runs: forbidden(),
        actions: actions(vec![old_bad_high_id, new_ok_low_id]),
        ..Scn::default()
    };
    assert_eq!(class_via(s).0, "green");
    // the other way round: the newest failed
    let new_bad = wr(
        4,
        7,
        "push",
        "2026-11-17T12:00:00Z",
        1,
        "completed",
        Some("failure"),
    );
    let old_ok = wr(
        3,
        7,
        "push",
        "2026-11-17T10:00:00Z",
        1,
        "completed",
        Some("success"),
    );
    let s = Scn {
        check_runs: forbidden(),
        actions: actions(vec![old_ok, new_bad]),
        ..Scn::default()
    };
    assert_eq!(class_via(s).0, "red");
}

#[test]
fn a_higher_run_attempt_wins_at_the_same_time() {
    let first = wr(
        5,
        7,
        "push",
        "2026-11-17T10:00:00Z",
        1,
        "completed",
        Some("failure"),
    );
    let rerun = wr(
        5,
        7,
        "push",
        "2026-11-17T10:00:00Z",
        2,
        "completed",
        Some("success"),
    );
    let s = Scn {
        check_runs: forbidden(),
        actions: actions(vec![rerun.clone(), first.clone()]),
        ..Scn::default()
    };
    assert_eq!(class_via(s).0, "green");
    let s = Scn {
        check_runs: forbidden(),
        actions: actions(vec![first, rerun]),
        ..Scn::default()
    };
    assert_eq!(class_via(s).0, "green");
}

#[test]
fn every_workflow_and_every_event_is_counted_once() {
    let push_ok = wr(
        1,
        7,
        "push",
        "2026-11-17T12:00:00Z",
        1,
        "completed",
        Some("success"),
    );
    // another workflow that failed: red, although the first one is green
    let other_wf = wr(
        2,
        8,
        "push",
        "2026-11-17T09:00:00Z",
        1,
        "completed",
        Some("failure"),
    );
    let s = Scn {
        check_runs: forbidden(),
        actions: actions(vec![push_ok.clone(), other_wf]),
        ..Scn::default()
    };
    assert_eq!(class_via(s).0, "red");
    // the same workflow on another event is its own entry: an older failure of `schedule` is not hidden by a newer `push`
    let sched_bad = wr(
        3,
        7,
        "schedule",
        "2026-11-17T08:00:00Z",
        1,
        "completed",
        Some("failure"),
    );
    let s = Scn {
        check_runs: forbidden(),
        actions: actions(vec![push_ok.clone(), sched_bad]),
        ..Scn::default()
    };
    assert_eq!(class_via(s).0, "red");
    let pr_bad = wr(
        4,
        7,
        "pull_request",
        "2026-11-17T08:00:00Z",
        1,
        "completed",
        Some("failure"),
    );
    let s = Scn {
        check_runs: forbidden(),
        actions: actions(vec![push_ok, pr_bad]),
        ..Scn::default()
    };
    assert_eq!(class_via(s).0, "red");
}

#[test]
fn an_actions_run_of_another_commit_is_ignored() {
    let mut other = wr(
        1,
        7,
        "push",
        "2026-11-17T12:00:00Z",
        1,
        "completed",
        Some("failure"),
    );
    other["head_sha"] = json!(OTHER);
    let s = Scn {
        check_runs: forbidden(),
        actions: actions(vec![other]),
        ..Scn::default()
    };
    assert_eq!(class_via(s), ("none".to_string(), "actions-runs".into()));
}

#[test]
fn commit_statuses_are_counted_in_both_ways() {
    let green = || checks(vec![cr("a", "completed", Some("success"))]);
    let ok_run = || {
        actions(vec![wr(
            1,
            7,
            "push",
            "2026-11-17T12:00:00Z",
            1,
            "completed",
            Some("success"),
        )])
    };
    // newest first: the first entry of a context is its state
    let cases: [(Vec<Value>, &str); 5] = [
        (vec![st("ci/x", "failure")], "red"),
        (vec![st("ci/x", "error")], "red"),
        (vec![st("ci/x", "pending")], "pending"),
        (vec![st("ci/x", "success"), st("ci/x", "failure")], "green"),
        (vec![st("ci/x", "failure"), st("ci/x", "success")], "red"),
    ];
    for (statuses, want) in cases {
        let s = Scn {
            check_runs: green(),
            statuses: (200, Value::Array(statuses.clone())),
            ..Scn::default()
        };
        assert_eq!(
            class_via(s),
            (want.to_string(), "check-runs".into()),
            "{statuses:?}"
        );
        let s = Scn {
            check_runs: forbidden(),
            actions: ok_run(),
            statuses: (200, Value::Array(statuses.clone())),
            ..Scn::default()
        };
        assert_eq!(
            class_via(s),
            (want.to_string(), "actions-runs".into()),
            "{statuses:?}"
        );
    }
    // only a status, no run: green or red by the status alone
    let s = Scn {
        statuses: (200, json!([st("ci/x", "success")])),
        ..Scn::default()
    };
    assert_eq!(class_via(s).0, "green");
}

fn not_read(s: Scn) -> (Value, FakeRoutes) {
    let api = fake(s);
    let (v, code) = json_of(&api, &[]);
    assert_eq!(code, 0, "{v}");
    assert_eq!(ci(&v)["read"], json!(false), "{}", ci(&v));
    assert_eq!(ci(&v)["class"], Value::Null);
    (v, api)
}

#[test]
fn only_a_403_on_check_runs_tries_the_actions_runs() {
    let limited = FakeRoutes::start(|path, _| {
        if path.ends_with("/check-runs") {
            return (
                403,
                vec![("x-ratelimit-remaining".into(), "0".into())],
                "{}".into(),
            );
        }
        let body = match path {
            "/repos/test/fixture" => json!({"default_branch": "main"}),
            "/repos/test/fixture/branches/main" => json!({"commit": {"sha": SHA}}),
            _ => json!([]),
        };
        (200, vec![], body.to_string())
    });
    let (v, code) = json_of(&limited, &[]);
    assert_eq!(code, 0);
    assert_eq!(ci(&v)["read"], json!(false));
    assert!(
        ci(&v)["error"].as_str().unwrap().contains("rate limit"),
        "{}",
        ci(&v)
    );
    assert_eq!(hits_of(&limited, "/actions/"), 0, "{:?}", limited.hits());
    for (status, text) in [
        (401u16, "token rejected"),
        (404, "not found"),
        (500, "HTTP 500"),
    ] {
        let (v, api) = not_read(Scn {
            check_runs: (status, json!({})),
            ..Scn::default()
        });
        let e = ci(&v)["error"].as_str().unwrap().to_string();
        assert!(e.contains(text), "HTTP {status}: {e}");
        assert_eq!(
            hits_of(&api, "/actions/"),
            0,
            "HTTP {status}: {:?}",
            api.hits()
        );
    }
}

#[test]
fn a_failed_actions_read_or_status_read_is_not_read_with_its_cause() {
    for (status, text) in [
        (401u16, "token rejected"),
        (404, "not found"),
        (500, "HTTP 500"),
    ] {
        let (v, _) = not_read(Scn {
            check_runs: forbidden(),
            actions: (status, json!({})),
            ..Scn::default()
        });
        let e = ci(&v)["error"].as_str().unwrap().to_string();
        assert!(e.contains(text), "HTTP {status}: {e}");
    }
    let (v, _) = not_read(Scn {
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
        statuses: forbidden(),
        ..Scn::default()
    });
    assert!(
        ci(&v)["error"].as_str().unwrap().contains("statuses"),
        "{}",
        ci(&v)
    );
    let (v, _) = not_read(Scn {
        statuses: (500, json!({})),
        ..Scn::default()
    });
    assert!(
        ci(&v)["error"].as_str().unwrap().contains("HTTP 500"),
        "{}",
        ci(&v)
    );
}

#[test]
fn an_incomplete_list_or_a_run_without_state_is_not_read_never_a_class() {
    // a malformed entry: the list is partial, so no class (partial evidence could hide a failure)
    let bad = json!({"id": 9, "workflow_id": 7, "name": 5, "status": "completed"});
    let s = Scn {
        check_runs: forbidden(),
        actions: (200, json!({"total_count": 1, "workflow_runs": [bad]})),
        ..Scn::default()
    };
    let (v, _) = not_read(s);
    assert!(
        ci(&v)["error"].as_str().unwrap().contains("incompletely"),
        "{}",
        ci(&v)
    );
    // completed without conclusion: no state evidence
    let s = Scn {
        check_runs: forbidden(),
        actions: actions(vec![wr(
            1,
            7,
            "push",
            "2026-11-17T17:32:44Z",
            1,
            "completed",
            None,
        )]),
        ..Scn::default()
    };
    let (v, _) = not_read(s);
    assert!(
        ci(&v)["error"]
            .as_str()
            .unwrap()
            .contains("lack state evidence"),
        "{}",
        ci(&v)
    );
}

#[test]
fn an_unreadable_default_branch_is_not_read_and_nothing_else_is_requested() {
    let cases = [
        Scn {
            repo: (404, json!({})),
            ..Scn::default()
        },
        Scn {
            repo: (200, json!({})),
            ..Scn::default()
        },
        Scn {
            repo: (200, json!({"default_branch": "a b"})),
            ..Scn::default()
        },
        Scn {
            branch: (404, json!({})),
            ..Scn::default()
        },
        Scn {
            branch: (200, json!({"commit": {"sha": "abc"}})),
            ..Scn::default()
        },
    ];
    for s in cases {
        let (_, api) = not_read(s);
        assert_eq!(
            hits_of(&api, "/check-runs") + hits_of(&api, "/actions/"),
            0,
            "{:?}",
            api.hits()
        );
    }
}

#[test]
fn a_failed_ci_read_does_not_hide_the_other_counts_and_exits_0() {
    let s = Scn {
        repo: (404, json!({})),
        ..Scn::default()
    };
    let api = fake(s);
    let (v, code) = json_of(&api, &[]);
    assert_eq!(code, 0);
    assert_eq!(v["repos"][0]["pull_requests"]["read"], json!(true));
    assert_eq!(
        v["repos"][0]["counts"]["pull_requests"]["state"],
        json!("ok")
    );
}

#[test]
fn without_a_read_there_is_no_request_and_the_field_says_not_read() {
    let fx = status_fixture();
    let api = fake(Scn::default());
    let o = stw(
        Some(&api.url),
        &[
            "status",
            "--root",
            &path_str(&fx.path),
            "--json",
            "--offline",
        ],
    );
    let v: Value = serde_json::from_slice(&o.stdout).unwrap();
    assert_eq!(
        ci(&v),
        &json!({"read": false, "error": null, "branch": null, "head": null, "class": null, "source": null})
    );
    assert!(api.hits().is_empty(), "{:?}", api.hits());
    // a repository without a GitHub origin
    fx.set_origin_url("https://example.org/x/y.git");
    let o = stw(
        Some(&api.url),
        &["status", "--root", &path_str(&fx.path), "--json"],
    );
    let v: Value = serde_json::from_slice(&o.stdout).unwrap();
    assert_eq!(ci(&v)["read"], json!(false));
    assert_eq!(ci(&v)["error"], Value::Null);
    assert!(api.hits().is_empty(), "{:?}", api.hits());
}

#[test]
fn the_requests_of_the_source_are_counted_under_ci_main_in_the_stats() {
    let s = Scn {
        check_runs: forbidden(),
        ..Scn::default()
    };
    let api = fake(s);
    let fx = status_fixture();
    let o = stw(
        Some(&api.url),
        &["status", "--root", &path_str(&fx.path), "--verbose"],
    );
    let err = String::from_utf8_lossy(&o.stderr).into_owned() + &String::from_utf8_lossy(&o.stdout);
    // repo, branch, check runs, actions runs, statuses
    let line = err
        .lines()
        .find(|l| l.trim_start().starts_with("ci-main:"))
        .unwrap_or_else(|| panic!("{err}"));
    assert!(line.contains(" 5 reqs"), "{line}");
}
