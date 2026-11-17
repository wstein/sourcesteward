//! The extended GitHub reads (issue comments, PR reviews, check runs, commit statuses) against a fake API
//! that serves recorded and hand-written fixtures from `tests/fixtures/github/`. No network, no real token.
use std::time::Duration;

use crate::common::*;
use serde_json::Value;
use stw::auth::TokenSource;
use stw::github::reads::{
    escape_untrusted, Budget, CheckRun, Comment, CommitStatus, Denial, Read, Review,
};
use stw::github::Client;

const TOKEN: &str = "SECRETTOKEN-0123456789";
const SHA: &str = "3988a274744f92464c6f8ea873c1a836a4d01356";
const SLUG: &str = "cli/cli";

fn fixture(name: &str) -> Value {
    let p = format!(
        "{}/tests/fixtures/github/{name}",
        env!("CARGO_MANIFEST_DIR")
    );
    serde_json::from_str(&std::fs::read_to_string(p).unwrap()).unwrap()
}

fn client(api: &FakeRoutes) -> Client {
    Client::with_base(TOKEN.into(), TokenSource::Env, api.url.clone())
}

/// Serve `array` (or `array_of` inside an object under `key`) page by page.
fn paged(items: Vec<Value>, key: Option<&'static str>, query: &str) -> String {
    let (page, per) = page_of(query);
    let slice: Vec<Value> = items.into_iter().skip((page - 1) * per).take(per).collect();
    match key {
        None => Value::Array(slice).to_string(),
        Some(k) => serde_json::json!({ "total_count": 0, k: slice }).to_string(),
    }
}

fn arr(v: Value) -> Vec<Value> {
    match v {
        Value::Array(a) => a,
        Value::Object(mut o) => match o.remove("check_runs") {
            Some(Value::Array(a)) => a,
            _ => panic!("fixture shape"),
        },
        _ => panic!("fixture shape"),
    }
}

fn no_control(s: &str) {
    for c in s.chars() {
        assert!(
            !c.is_control()
                && !matches!(c as u32, 0x200B..=0x200F | 0x2028..=0x202E | 0x2060..=0x206F | 0xFEFF),
            "unescaped {:?} in {s:?}",
            c
        );
    }
}

fn observed<T>(r: Read<T>) -> stw::github::reads::Observed<T> {
    match r {
        Read::Observed(o) => o,
        other => panic!("expected observed, got state {}", other.state()),
    }
}

fn status_server(status: u16, headers: Vec<(String, String)>, body: String) -> FakeRoutes {
    FakeRoutes::start(move |_, _| (status, headers.clone(), body.clone()))
}

#[test]
fn comments_are_paginated_complete_and_escaped() {
    let all = arr(fixture("issue_comments.json"));
    assert_eq!(all.len(), 63);
    let api = FakeRoutes::start({
        let all = all.clone();
        move |path, q| {
            assert_eq!(path, "/repos/cli/cli/issues/14963/comments");
            (200, vec![], paged(all.clone(), None, q))
        }
    });
    let b = Budget {
        per_page: 25,
        ..Budget::default()
    };
    let o = observed(client(&api).issue_comments(SLUG, 14963, &b).unwrap());
    assert_eq!(o.items.len(), 63);
    assert_eq!(o.pages, 3);
    assert!(o.is_complete());
    let hits = api.hits();
    assert_eq!(hits.len(), 3, "{hits:?}");
    for (i, h) in hits.iter().enumerate() {
        assert!(h.contains(&format!("page={}", i + 1)), "{h}");
        assert!(h.contains(&format!("Bearer {TOKEN}")), "{h}");
    }
    let c: &Comment = &o.items[0];
    assert_eq!(c.author, "cli-triage[bot]");
    assert_eq!(c.author_type, "Bot");
    assert!(c.body.starts_with("**Recommendation:"));
    for c in &o.items {
        no_control(&c.body);
        assert!(!c.body.contains('\n'));
    }
}

#[test]
fn reviews_carry_state_and_the_reviewed_commit_and_real_text_is_escaped() {
    let api = FakeRoutes::start(|path, q| {
        assert_eq!(path, "/repos/cli/cli/pulls/14972/reviews");
        (200, vec![], paged(arr(fixture("pr_reviews.json")), None, q))
    });
    let o = observed(
        client(&api)
            .pr_reviews(SLUG, 14972, &Budget::default())
            .unwrap(),
    );
    assert!(o.is_complete());
    let r: &Review = &o.items[0];
    assert_eq!(r.state, "APPROVED");
    assert_eq!(r.author, "tidy-dev");
    assert_eq!(
        r.commit_id.as_deref(),
        Some("3e6aeafb1b796b862389c1abe4deba5e60fa0b01")
    );
    assert_eq!(r.body, "");
    let bot = &o.items[1];
    assert_eq!(bot.state, "COMMENTED");
    // the recorded body has CRLF, newlines and zero-width spaces
    assert!(
        bot.body.contains("\\r\\n") && bot.body.contains("\\n"),
        "{}",
        bot.body
    );
    assert!(bot.body.contains("\\u{200b}"), "{}", bot.body);
    no_control(&bot.body);
}

#[test]
fn check_runs_are_read_across_pages() {
    let api = FakeRoutes::start(|path, q| {
        assert_eq!(path, format!("/repos/cli/cli/commits/{SHA}/check-runs"));
        (
            200,
            vec![],
            paged(arr(fixture("check_runs.json")), Some("check_runs"), q),
        )
    });
    let b = Budget {
        per_page: 3,
        ..Budget::default()
    };
    let o = observed(client(&api).check_runs(SLUG, SHA, &b).unwrap());
    assert_eq!(o.items.len(), 7);
    assert_eq!(o.pages, 3);
    assert!(o.is_complete());
    let c: &CheckRun = &o.items[0];
    assert_eq!(c.head_sha.as_deref(), Some(SHA));
    assert!(c.status == "completed" && c.conclusion.is_some(), "{c:?}");
    assert!(c.app.is_some());
}

#[test]
fn statuses_with_hostile_text_are_escaped_and_nulls_are_tolerated() {
    let api = FakeRoutes::start(|path, _| {
        assert_eq!(path, format!("/repos/cli/cli/commits/{SHA}/statuses"));
        (200, vec![], fixture("commit_statuses.json").to_string())
    });
    let o = observed(
        client(&api)
            .commit_statuses(SLUG, SHA, &Budget::default())
            .unwrap(),
    );
    assert_eq!(o.items.len(), 3);
    assert!(o.is_complete());
    let lint: &CommitStatus = &o.items[1];
    assert_eq!(lint.state, "failure");
    assert_eq!(lint.context, "ci/lint");
    assert_eq!(
        lint.description,
        "line1\\nline2 \\u{1b}[31mred\\u{1b}[0m \\u{202e}reversed"
    );
    no_control(&lint.description);
    let pending = &o.items[2];
    assert_eq!(pending.description, "");
    assert_eq!(pending.creator, None);
}

#[test]
fn budgets_make_a_list_partial_and_partial_is_never_complete() {
    let all = arr(fixture("issue_comments.json"));
    let api = FakeRoutes::start(move |_, q| (200, vec![], paged(all.clone(), None, q)));
    let c = client(&api);

    let pages = Budget {
        per_page: 10,
        max_pages: 2,
        ..Budget::default()
    };
    let r = c.issue_comments(SLUG, 1, &pages).unwrap();
    assert_eq!(r.state(), "partial");
    assert!(!r.is_complete());
    let o = observed(r);
    assert_eq!((o.items.len(), o.pages), (20, 2));
    assert!(o.incomplete.as_deref().unwrap().contains("page budget"));

    let items = Budget {
        per_page: 10,
        max_items: 15,
        ..Budget::default()
    };
    let o = observed(c.issue_comments(SLUG, 1, &items).unwrap());
    assert_eq!(o.items.len(), 15);
    assert!(o.incomplete.as_deref().unwrap().contains("item budget"));

    let time = Budget {
        time: Duration::ZERO,
        ..Budget::default()
    };
    let before = api.hits().len();
    let o = observed(c.issue_comments(SLUG, 1, &time).unwrap());
    assert!(o.items.is_empty() && o.incomplete.as_deref().unwrap().contains("time budget"));
    assert_eq!(
        api.hits().len(),
        before,
        "an exhausted time budget makes no request"
    );

    let text = Budget {
        max_text: 20,
        ..Budget::default()
    };
    let o = observed(c.issue_comments(SLUG, 1, &text).unwrap());
    assert!(o.is_complete());
    assert!(o
        .items
        .iter()
        .all(|c| c.body_truncated && c.body.chars().count() <= 40));
}

#[test]
fn a_page_that_fails_after_the_first_keeps_the_items_and_marks_the_list_partial() {
    for (status, headers) in [(500u16, vec![]), (403, vec![]), (404, vec![])] {
        let all = arr(fixture("issue_comments.json"));
        let api = FakeRoutes::start(move |_, q| {
            if page_of(q).0 == 1 {
                (200, vec![], paged(all.clone(), None, q))
            } else {
                (status, headers.clone(), "{}".into())
            }
        });
        let b = Budget {
            per_page: 10,
            ..Budget::default()
        };
        let r = client(&api).issue_comments(SLUG, 1, &b).unwrap();
        assert_eq!(r.state(), "partial", "status {status}");
        let o = observed(r);
        assert_eq!(o.items.len(), 10);
        assert!(
            o.incomplete.as_deref().unwrap().contains("page 2"),
            "{:?}",
            o.incomplete
        );
    }
}

#[test]
fn a_malformed_entry_makes_the_list_partial() {
    let body = serde_json::json!([
        {"id": 1, "user": {"login": "a", "type": "User"}, "body": "ok"},
        {"user": {"login": "b"}, "body": "no id"},
        {"id": 3, "user": null, "body": "ghost author"}
    ])
    .to_string();
    let api = status_server(200, vec![], body);
    let o = observed(
        client(&api)
            .issue_comments(SLUG, 1, &Budget::default())
            .unwrap(),
    );
    assert_eq!(o.items.len(), 2);
    assert_eq!(o.items[1].author, "");
    assert!(!o.is_complete());
}

#[test]
fn commit_ids_that_are_not_full_lower_case_hex_are_dropped() {
    let body = serde_json::json!([
        {"id": 1, "state": "APPROVED", "commit_id": SHA, "user": {"login": "a"}},
        {"id": 2, "state": "APPROVED", "commit_id": SHA.to_uppercase(), "user": {"login": "a"}},
        {"id": 3, "state": "APPROVED", "commit_id": "abc123", "user": {"login": "a"}},
        {"id": 4, "state": "APPROVED", "commit_id": "../etc", "user": {"login": "a"}},
        {"id": 5, "state": "APPROVED", "commit_id": null, "user": {"login": "a"}},
        {"id": 6, "state": "APPROVED", "commit_id": format!("{SHA}0"), "user": {"login": "a"}},
        {"id": 7, "state": "APPROVED", "commit_id": &SHA[..39], "user": {"login": "a"}},
        {"id": 8, "state": "APPROVED", "commit_id": format!("{SHA}{}", &SHA[..24]), "user": {"login": "a"}},
        {"id": 9, "state": "APPROVED", "commit_id": format!("{SHA}{}", &SHA[..23]), "user": {"login": "a"}},
        {"id": 10, "state": "APPROVED", "commit_id": format!("{SHA}{SHA}"), "user": {"login": "a"}}
    ])
    .to_string();
    let api = status_server(200, vec![], body);
    let o = observed(
        client(&api)
            .pr_reviews(SLUG, 1, &Budget::default())
            .unwrap(),
    );
    let ids: Vec<Option<&str>> = o.items.iter().map(|r| r.commit_id.as_deref()).collect();
    let sha256 = format!("{SHA}{}", &SHA[..24]);
    assert_eq!(
        ids,
        [
            Some(SHA),
            None,
            None,
            None,
            None,
            None,
            None,
            Some(sha256.as_str()),
            None,
            None
        ]
    );
}

#[test]
fn denied_missing_and_broken_answers_are_typed_never_a_crash() {
    let forbidden = fixture("forbidden_checks.json").to_string();
    type Case = (u16, Vec<(String, String)>, String, &'static str);
    let cases: Vec<Case> = vec![
        (403, vec![], forbidden.clone(), "forbidden"),
        (
            403,
            vec![("x-ratelimit-remaining".into(), "0".into())],
            forbidden.clone(),
            "rate-limited",
        ),
        (429, vec![], "{}".into(), "rate-limited"),
        (401, vec![], "{}".into(), "unauthorized"),
        (404, vec![], "{}".into(), "not-found"),
        (500, vec![], "{}".into(), "failed"),
        (200, vec![], "not json".into(), "failed"),
        (200, vec![], "{\"message\":\"x\"}".into(), "failed"),
    ];
    for (status, headers, body, state) in cases {
        let api = status_server(status, headers, body);
        let c = client(&api);
        let b = Budget::default();
        let r = c.check_runs(SLUG, SHA, &b).unwrap();
        let denied = matches!(state, "forbidden" | "rate-limited" | "unauthorized");
        assert_eq!(
            r.state(),
            if denied { "denied" } else { state },
            "HTTP {status}"
        );
        assert!(!r.is_complete());
        if denied {
            let Read::Denied {
                kind,
                status: s,
                message,
            } = &r
            else {
                panic!("{status}")
            };
            assert_eq!(kind.label(), state);
            assert_eq!(*s, status);
            assert!(!message.contains(TOKEN));
        }
        // the same typed answer for the other three lists
        assert_eq!(
            c.issue_comments(SLUG, 1, &b).unwrap().state(),
            match state {
                "forbidden" | "rate-limited" | "unauthorized" => "denied",
                s => s,
            }
        );
        assert!(!c.pr_reviews(SLUG, 1, &b).unwrap().is_complete());
        assert!(!c.commit_statuses(SLUG, SHA, &b).unwrap().is_complete());
    }
    let d = Denial::Forbidden;
    assert_eq!(d.label(), "forbidden");
}

#[test]
fn url_pieces_are_validated_before_any_request() {
    let api = status_server(200, vec![], "[]".into());
    let c = client(&api);
    let b = Budget::default();
    for slug in [
        "", "a", "a/b/c", "a/../b", "../b", "a b/c", "a/b?x=1", "a/b#f", "a/b%2f", "/b", "a/",
    ] {
        assert!(c.issue_comments(slug, 1, &b).is_err(), "{slug:?}");
        assert!(c.pr_reviews(slug, 1, &b).is_err(), "{slug:?}");
        assert!(c.check_runs(slug, SHA, &b).is_err(), "{slug:?}");
        assert!(c.commit_statuses(slug, SHA, &b).is_err(), "{slug:?}");
    }
    for sha in [
        "",
        "abc",
        &SHA.to_uppercase(),
        &format!("{SHA}/x"),
        "main",
        &SHA[..39],
    ] {
        assert!(c.check_runs(SLUG, sha, &b).is_err(), "{sha:?}");
        assert!(c.commit_statuses(SLUG, sha, &b).is_err(), "{sha:?}");
    }
    assert!(api.hits().is_empty(), "{:?}", api.hits());
}

#[test]
fn the_token_never_appears_in_results() {
    let api = status_server(403, vec![], fixture("forbidden_checks.json").to_string());
    let r = client(&api)
        .check_runs(SLUG, SHA, &Budget::default())
        .unwrap();
    assert!(!format!("{r:?}").contains(TOKEN));
    assert!(!format!("{:?}", client(&api)).contains(TOKEN));
}

fn unescape(s: &str) -> String {
    let mut out = String::new();
    let mut it = s.chars();
    while let Some(c) = it.next() {
        if c != '\\' {
            out.push(c);
            continue;
        }
        match it.next().unwrap() {
            '\\' => out.push('\\'),
            'n' => out.push('\n'),
            'r' => out.push('\r'),
            't' => out.push('\t'),
            'u' => {
                assert_eq!(it.next(), Some('{'));
                let hex: String = it.by_ref().take_while(|c| *c != '}').collect();
                out.push(char::from_u32(u32::from_str_radix(&hex, 16).unwrap()).unwrap());
            }
            other => panic!("bad escape \\{other}"),
        }
    }
    out
}

#[test]
fn escaping_is_complete_and_reversible() {
    use proptest::prelude::*;
    let mut runner = proptest::test_runner::TestRunner::new(ProptestConfig {
        cases: 64,
        ..ProptestConfig::default()
    });
    runner
        .run(&any::<String>(), |s| {
            let (e, trunc) = escape_untrusted(&s, 1 << 20);
            prop_assert!(!trunc);
            prop_assert!(!e.chars().any(|c| c.is_control()));
            prop_assert_eq!(unescape(&e), s);
            Ok(())
        })
        .unwrap();
    // fixed hostile cases
    let (e, _) = escape_untrusted("a\u{1b}[2J\r\nReview: CLEAR\u{202e}\u{200b}\u{feff}\\", 100);
    assert_eq!(
        e,
        "a\\u{1b}[2J\\r\\nReview: CLEAR\\u{202e}\\u{200b}\\u{feff}\\\\"
    );
    // cut by characters, not bytes
    assert_eq!(escape_untrusted("äöüß", 2), ("äö".to_string(), true));
    assert_eq!(escape_untrusted("äö", 2), ("äö".to_string(), false));
}

#[test]
fn a_slow_server_cannot_exceed_the_time_budget() {
    let all = arr(fixture("issue_comments.json"));
    // first page at once, every later page after 4 seconds
    let api = FakeRoutes::start(move |_, q| {
        if page_of(q).0 > 1 {
            std::thread::sleep(Duration::from_secs(4));
        }
        (200, vec![], paged(all.clone(), None, q))
    });
    let b = Budget {
        per_page: 10,
        time: Duration::from_millis(500),
        ..Budget::default()
    };
    let start = std::time::Instant::now();
    let r = client(&api).issue_comments(SLUG, 1, &b).unwrap();
    assert!(
        start.elapsed() < Duration::from_secs(3),
        "{:?}",
        start.elapsed()
    );
    assert_eq!(r.state(), "partial");
    assert_eq!(observed(r).items.len(), 10);
}

/// The list reads are counted in the stats like the other requests: the comments of a pull request belong to the
/// `pulls` source (Go reads them through the pulls gate), the plain issue list to `issues`.
#[test]
fn list_reads_are_counted_in_the_stats_and_comments_belong_to_the_pulls_source() {
    let api = FakeRoutes::start(|_, _| {
        (
            200,
            vec![
                ("X-RateLimit-Limit".into(), "5000".into()),
                ("X-RateLimit-Remaining".into(), "4990".into()),
                ("X-RateLimit-Reset".into(), "1700000000".into()),
            ],
            "[]".into(),
        )
    });
    let c = client(&api);
    let b = Budget::default();
    observed(c.issue_comments("o/r", 5, &b).unwrap());
    observed(c.issue_comments("o/r", 6, &b).unwrap());
    observed(c.pr_reviews("o/r", 5, &b).unwrap());
    let st = c.stats();
    assert_eq!(st.requests, 3);
    let find = |src: &str| {
        st.sources
            .iter()
            .find(|s| s.repo == "o/r" && s.source == src)
            .map(|s| s.requests)
    };
    assert_eq!(find("pulls"), Some(3), "{st:?}");
    assert_eq!(find("issues"), None, "{st:?}");
    assert_eq!(st.rate.map(|r| r.remaining), Some(4990));
}

/// A list read does not start once the overall deadline has passed, and says why.
#[test]
fn a_list_read_obeys_the_overall_deadline() {
    let api = FakeRoutes::start(|_, _| (200, vec![], "[]".into()));
    let c = client(&api);
    c.arm_deadline(Duration::from_millis(1));
    std::thread::sleep(Duration::from_millis(30));
    match c.issue_comments("o/r", 5, &Budget::default()).unwrap() {
        Read::Failed(m) => assert!(m.contains("deadline:") && m.contains("--timeout"), "{m}"),
        other => panic!("{}", other.state()),
    }
    assert!(api.hits().is_empty(), "no request after the deadline");
    assert_eq!(c.stats().requests, 0);
}

fn run(
    id: u64,
    workflow: u64,
    name: &str,
    event: &str,
    status: &str,
    conclusion: Option<&str>,
) -> Value {
    serde_json::json!({"id": id, "workflow_id": workflow, "name": name, "event": event, "status": status,
        "conclusion": conclusion, "head_sha": SHA, "created_at": "2026-11-17T17:32:44Z", "run_attempt": 1})
}

#[test]
fn workflow_runs_of_a_commit_are_read_across_pages_with_the_commit_in_every_request() {
    let runs: Vec<Value> = (1..=5)
        .map(|i| run(i, 7, "Test", "push", "completed", Some("success")))
        .collect();
    let api = FakeRoutes::start(move |path, q| {
        assert_eq!(path, format!("/repos/{SLUG}/actions/runs"));
        assert!(q.contains(&format!("head_sha={SHA}")), "{q}");
        (200, vec![], paged(runs.clone(), Some("workflow_runs"), q))
    });
    let b = Budget {
        per_page: 2,
        ..Budget::default()
    };
    let o = observed(client(&api).workflow_runs(SLUG, SHA, &b).unwrap());
    assert!(o.is_complete());
    assert_eq!(o.pages, 3);
    assert_eq!(o.items.len(), 5);
    let first = &o.items[0];
    assert_eq!(
        (
            first.id,
            first.workflow_id,
            first.name.as_str(),
            first.event.as_str()
        ),
        (1, 7, "Test", "push")
    );
    assert_eq!(first.status, "completed");
    assert_eq!(first.conclusion.as_deref(), Some("success"));
    assert_eq!(first.head_sha.as_deref(), Some(SHA));
    assert_eq!(
        (first.created_at.as_str(), first.run_attempt),
        ("2026-11-17T17:32:44Z", 1)
    );
    assert_eq!(api.hits().len(), 3, "{:?}", api.hits());
}

#[test]
fn workflow_run_text_is_escaped_and_a_malformed_entry_makes_the_list_partial() {
    let mut hostile = run(
        1,
        7,
        "Te\u{1b}[31mst\nx",
        "push",
        "completed",
        Some("failure"),
    );
    hostile["head_sha"] = Value::String(SHA.to_uppercase());
    let bad = serde_json::json!({"id": 2, "workflow_id": 7, "name": 5, "status": "completed"});
    let no_status = serde_json::json!({"id": 3, "workflow_id": 7, "name": "x"});
    let body = serde_json::json!({"total_count": 3, "workflow_runs": [hostile, bad, no_status]})
        .to_string();
    let api = status_server(200, vec![], body);
    let o = observed(
        client(&api)
            .workflow_runs(SLUG, SHA, &Budget::default())
            .unwrap(),
    );
    assert!(!o.is_complete(), "{o:?}");
    assert_eq!(o.items.len(), 1);
    no_control(&o.items[0].name);
    assert!(
        o.items[0].name.contains("\\u{1b}") && o.items[0].name.contains("\\n"),
        "{:?}",
        o.items[0].name
    );
    assert_eq!(
        o.items[0].head_sha, None,
        "an upper-case id is no commit id"
    );
}

#[test]
fn denied_missing_and_broken_answers_of_workflow_runs_are_typed() {
    let cases: [(u16, Vec<(String, String)>, &str, &str); 7] = [
        (403, vec![], "{}", "denied"),
        (
            403,
            vec![("x-ratelimit-remaining".into(), "0".into())],
            "{}",
            "denied",
        ),
        (401, vec![], "{}", "denied"),
        (404, vec![], "{}", "not-found"),
        (500, vec![], "{}", "failed"),
        (200, vec![], "not json", "failed"),
        (200, vec![], "{\"message\":\"x\"}", "failed"),
    ];
    for (status, headers, body, state) in cases {
        let api = status_server(status, headers, body.into());
        let r = client(&api)
            .workflow_runs(SLUG, SHA, &Budget::default())
            .unwrap();
        assert_eq!(r.state(), state, "HTTP {status}");
        assert!(!r.is_complete());
        assert!(!format!("{r:?}").contains(TOKEN));
    }
    let api = status_server(403, vec![], "{}".into());
    let r = client(&api)
        .workflow_runs(SLUG, SHA, &Budget::default())
        .unwrap();
    assert!(
        matches!(
            r,
            Read::Denied {
                kind: Denial::Forbidden,
                status: 403,
                ..
            }
        ),
        "{r:?}"
    );
}

#[test]
fn the_pieces_of_a_workflow_runs_request_are_validated_before_any_request() {
    let api = status_server(200, vec![], "{}".into());
    let c = client(&api);
    let b = Budget::default();
    for slug in ["", "a", "a/b/c", "a/../b", "a b/c", "a/b?x=1"] {
        assert!(c.workflow_runs(slug, SHA, &b).is_err(), "{slug:?}");
        assert!(c.default_branch_head(slug).is_err(), "{slug:?}");
    }
    for sha in [
        "",
        "abc",
        &SHA.to_uppercase(),
        &format!("{SHA}&x=1"),
        "main",
    ] {
        assert!(c.workflow_runs(SLUG, sha, &b).is_err(), "{sha:?}");
    }
    assert!(api.hits().is_empty(), "{:?}", api.hits());
}

#[test]
fn the_default_branch_and_its_head_are_read_from_two_documents() {
    let api = FakeRoutes::start(|path, _| {
        let body = match path {
            "/repos/cli/cli" => serde_json::json!({"default_branch": "trunk/main"}),
            "/repos/cli/cli/branches/trunk/main" => serde_json::json!({"commit": {"sha": SHA}}),
            _ => return (404, vec![], "{}".into()),
        };
        (200, vec![], body.to_string())
    });
    let (branch, head) = client(&api).default_branch_head(SLUG).unwrap();
    assert_eq!((branch.as_str(), head.as_str()), ("trunk/main", SHA));
    assert_eq!(api.hits().len(), 2, "{:?}", api.hits());
}

#[test]
fn a_default_branch_that_cannot_go_into_a_path_or_a_head_that_is_no_commit_id_is_an_error() {
    for name in [
        "a b", "x?y=1", "a/../b", "x%2f", "x#f", "/x", "x/", "a//b", "",
    ] {
        let n = name.to_string();
        let api = FakeRoutes::start(move |path, _| {
            let body = if path == "/repos/cli/cli" {
                serde_json::json!({"default_branch": n})
            } else {
                serde_json::json!({"commit": {"sha": SHA}})
            };
            (200, vec![], body.to_string())
        });
        assert!(client(&api).default_branch_head(SLUG).is_err(), "{name:?}");
        assert_eq!(
            api.hits().len(),
            1,
            "{name:?}: only the repository is read: {:?}",
            api.hits()
        );
    }
    for sha in [
        serde_json::json!(null),
        serde_json::json!("abc"),
        serde_json::json!(5),
    ] {
        let api = FakeRoutes::start(move |path, _| {
            let body = if path == "/repos/cli/cli" {
                serde_json::json!({"default_branch": "main"})
            } else {
                serde_json::json!({"commit": {"sha": sha.clone()}})
            };
            (200, vec![], body.to_string())
        });
        assert!(client(&api).default_branch_head(SLUG).is_err());
    }
    for status in [401u16, 403, 404, 500] {
        let api = status_server(status, vec![], "{}".into());
        let e = client(&api)
            .default_branch_head(SLUG)
            .unwrap_err()
            .to_string();
        assert!(e.contains(&format!("HTTP {status}")), "{e}");
        assert!(!e.contains(TOKEN));
    }
}
