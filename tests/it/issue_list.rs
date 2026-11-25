//! The GraphQL issue list ported from the approved spike (pull request 46).
use serde_json::{json, Value};
use stw::issue_list::{self, Listed};

fn issue(number: i64, title: &str) -> Value {
    json!({
        "id": format!("I_{number}"),
        "number": number,
        "title": title,
        "url": format!("https://github.com/o/a/issues/{number}"),
        "updatedAt": "2026-11-25T12:00:00Z",
        "labels": {"pageInfo": {"hasNextPage": false, "endCursor": null}, "nodes": []},
        "assignees": {"pageInfo": {"hasNextPage": false, "endCursor": null}, "nodes": []},
    })
}

fn repo(id: &str, issues: Vec<Value>, next: Option<&str>) -> Value {
    json!({
        "id": id,
        "issues": {
            "pageInfo": {"hasNextPage": next.is_some(), "endCursor": next},
            "nodes": issues,
        }
    })
}

#[test]
fn a_second_page_is_kept() {
    let mut calls = 0;
    let listed = issue_list::read(&["o/a".into()], 2, 8, |body: &Value| {
        calls += 1;
        let cursor = body["variables"]["c0"].as_str();
        let data = if cursor.is_none() {
            json!({"r0": repo("R_a", vec![issue(1, "first"), issue(2, "second")], Some("CUR1"))})
        } else {
            assert_eq!(cursor, Some("CUR1"));
            json!({"r0": repo("R_a", vec![issue(3, "third")], None)})
        };
        Ok::<_, String>(json!({"data": data}))
    });
    assert_eq!(calls, 2);
    assert_eq!(listed[0].coverage, "complete");
    assert_eq!(
        listed[0]
            .issues
            .iter()
            .map(|i| i.number)
            .collect::<Vec<_>>(),
        vec![1, 2, 3]
    );
}

#[test]
fn a_partial_failure_keeps_the_other_repository() {
    let listed = issue_list::read(&["o/a".into(), "o/gone".into()], 2, 8, |body: &Value| {
        assert!(body["query"].as_str().unwrap().contains("r1"));
        Ok::<_, String>(json!({
            "data": {
                "r0": repo("R_a", vec![issue(7, "kept")], None),
                "r1": null,
            },
            "errors": [{"message": "Could not resolve to a Repository with the name 'o/gone'.", "path": ["r1"]}],
        }))
    });
    assert_eq!(listed[0].coverage, "complete");
    assert_eq!(listed[0].issues[0].title, "kept");
    assert_eq!(listed[1].coverage, "incomplete");
    assert!(listed[1].issues.is_empty());
    let lines = issue_list::text_lines(&listed[1]);
    assert!(lines[0].contains("incomplete"));
    assert!(!lines.iter().any(|l| l.contains("none")));
}

#[test]
fn an_empty_list_is_only_a_successful_read() {
    let listed = issue_list::read(&["o/a".into()], 5, 8, |_body: &Value| {
        Ok::<_, String>(json!({"data": {"r0": repo("R_a", vec![], None)}}))
    });
    assert_eq!(listed[0].coverage, "complete");
    assert_eq!(
        issue_list::text_lines(&listed[0]),
        vec!["    open issues: none".to_string()]
    );
    let gone = Listed {
        name: "o/gone".into(),
        id: None,
        coverage: "incomplete",
        error: Some("missing".into()),
        issues: vec![],
    };
    assert!(!issue_list::text_lines(&gone).join("\n").contains("none"));
}

#[test]
fn json_carries_the_repository_id_url_and_update_time() {
    let mut item = issue(4, "labelled");
    item["labels"]["nodes"] = json!([{"name": "bug"}]);
    let listed = issue_list::read(&["o/a".into()], 5, 8, move |_body: &Value| {
        Ok::<_, String>(json!({"data": {"r0": repo("R_a", vec![item.clone()], None)}}))
    });
    let doc = issue_list::json_of(&listed[0]);
    assert_eq!(doc["id"], "R_a");
    assert_eq!(doc["issues"][0]["url"], "https://github.com/o/a/issues/4");
    assert_eq!(doc["issues"][0]["updated_at"], "2026-11-25T12:00:00Z");
    assert_eq!(doc["issues"][0]["labels"][0], "bug");
}

#[test]
fn partial_alias_data_keeps_valid_issues_from_that_repository() {
    let listed = issue_list::read(&["o/a".into(), "o/b".into()], 2, 8, |_body: &Value| {
        Ok::<_, String>(json!({
            "data": {
                "r0": repo("R_a", vec![issue(1, "valid"), Value::Null], None),
                "r1": repo("R_b", vec![], None),
            },
            "errors": [{"message": "Issue unavailable", "path": ["r0", "issues", "nodes", 1]}],
        }))
    });
    assert_eq!(listed[0].coverage, "incomplete");
    assert_eq!(listed[0].id.as_deref(), Some("R_a"));
    assert_eq!(listed[0].issues[0].number, 1);
    assert_eq!(listed[1].coverage, "complete");
}

#[test]
fn invalid_or_repeated_repository_cursors_stop_without_losing_data() {
    for broken in [Value::Null, json!(""), json!("repeated")] {
        let mut calls = 0;
        let listed = issue_list::read(&["o/a".into()], 1, 8, |_body: &Value| {
            calls += 1;
            assert!(calls <= 2, "invalid cursor triggered another request");
            let mut r = repo("R_a", vec![issue(calls, "valid")], Some("repeated"));
            if calls == 2 {
                r["issues"]["pageInfo"]["endCursor"] = broken.clone();
            }
            Ok::<_, String>(json!({"data": {"r0": r}}))
        });
        assert_eq!(calls, 2);
        assert_eq!(listed[0].coverage, "incomplete");
        assert_eq!(listed[0].issues.len(), 2);
    }
}

#[test]
fn malformed_repository_payload_never_looks_successfully_empty() {
    let listed = issue_list::read(&["o/a".into()], 1, 8, |_body: &Value| {
        Ok::<_, String>(json!({"data": {"r0": {"id": "R_a", "issues": null}}}))
    });
    assert_eq!(listed[0].coverage, "incomplete");
    assert!(!issue_list::text_lines(&listed[0])
        .join("\n")
        .contains("none"));
}

#[test]
fn metadata_pages_keep_partial_names_and_stop_on_repeated_cursors() {
    let mut calls = 0;
    let listed = issue_list::read(&["o/a".into()], 1, 8, |body: &Value| {
        calls += 1;
        assert!(calls <= 3, "invalid label cursor triggered another request");
        if calls == 1 {
            let mut item = issue(1, "details");
            item["labels"] = json!({"pageInfo": {"hasNextPage": true, "endCursor": "L1"}, "nodes": [{"name": "first"}]});
            item["assignees"] = json!({"pageInfo": {"hasNextPage": true, "endCursor": "A1"}, "nodes": [{"login": "first-user"}]});
            Ok::<_, String>(json!({"data": {"r0": repo("R_a", vec![item], None)}}))
        } else if calls == 2 {
            assert_eq!(body["variables"]["c"], "L1");
            Ok(
                json!({"data": {"node": {"labels": {"pageInfo": {"hasNextPage": true, "endCursor": "L1"}, "nodes": [{"name": "second"}, null]}}}, "errors": [{"message": "Incomplete page", "path": ["node", "labels", "nodes", 1]}]}),
            )
        } else {
            assert_eq!(body["variables"]["c"], "A1");
            Ok(
                json!({"data": {"node": {"assignees": {"pageInfo": {"hasNextPage": false, "endCursor": null}, "nodes": [{"login": "second-user"}]}}}}),
            )
        }
    });
    assert_eq!(calls, 3);
    assert_eq!(listed[0].coverage, "incomplete");
    assert_eq!(listed[0].issues[0].labels, ["first", "second"]);
    assert_eq!(listed[0].issues[0].assignees, ["first-user", "second-user"]);
}

#[test]
fn terminal_output_escapes_remote_text_including_diagnostics() {
    let mut item = issue(1, "title\u{202e}\nforged");
    item["labels"]["nodes"] = json!([{"name": "label\u{0085}end"}]);
    let listed = issue_list::read(&["o/a".into()], 1, 8, |_body: &Value| {
        Ok::<_, String>(json!({
            "data": {"r0": repo("R_a", vec![item.clone()], None)},
            "errors": [{"message": "partial\u{001b}[31m\nforged", "path": ["r0", "issues", "nodes", 1]}]
        }))
    });
    let lines = issue_list::text_lines(&listed[0]);
    assert!(lines[1].contains("title\\u{202e}\\nforged"));
    assert!(lines[1].contains("label\\u{85}end"));
    assert!(lines[2].contains("partial\\u{1b}[31m\\nforged"));
}

#[test]
fn graphql_requests_obey_rate_limits_and_report_request_stats() {
    use crate::common::FakeRoutes;
    use stw::{auth::TokenSource, github::Client};

    let api = FakeRoutes::start(|path, _| {
        assert_eq!(path, "/graphql");
        (
            429,
            vec![
                ("X-RateLimit-Limit".into(), "100".into()),
                ("X-RateLimit-Remaining".into(), "0".into()),
                ("X-RateLimit-Reset".into(), "1764072000".into()),
            ],
            "{}".into(),
        )
    });
    let client = Client::with_base("fixture-token".into(), TokenSource::Env, api.url.clone());
    let query = json!({"query": "{ viewer { login } }"});
    assert!(client.graphql(&query).is_err());
    assert!(client.rate_limited());
    assert!(client.graphql(&query).is_err());
    assert_eq!(api.hits().len(), 1);
    let stats = client.stats();
    assert_eq!(stats.requests, 1);
    assert_eq!(stats.rate.unwrap().remaining, 0);
    assert_eq!(stats.sources[0].source, "issues");
}

#[test]
fn graphql_does_not_follow_redirects() {
    use crate::common::FakeRoutes;
    use stw::{auth::TokenSource, github::Client};

    let other = FakeRoutes::start(|_, _| (200, vec![], "{}".into()));
    let redirect = format!("{}/redirected", other.url);
    let api = FakeRoutes::start(move |_, _| {
        (
            307,
            vec![("Location".into(), redirect.clone())],
            "{}".into(),
        )
    });
    let client = Client::with_base("fixture-token".into(), TokenSource::Env, api.url.clone());
    assert!(client
        .graphql(&json!({"query": "{ viewer { login } }"}))
        .is_err());
    assert_eq!(api.hits().len(), 1);
    assert!(other.hits().is_empty());
}

#[test]
fn enterprise_rest_base_uses_the_enterprise_graphql_endpoint() {
    use crate::common::FakeRoutes;
    use stw::{auth::TokenSource, github::Client};

    let api = FakeRoutes::start(|path, _| {
        if path == "/api/graphql" {
            (200, vec![], "{\"data\":{}}".into())
        } else {
            (404, vec![], "{}".into())
        }
    });
    let client = Client::with_base(
        "fixture-token".into(),
        TokenSource::Env,
        format!("{}/api/v3", api.url),
    );
    assert!(client
        .graphql(&json!({"query": "{ viewer { login } }"}))
        .is_ok());
    assert_eq!(api.hits().len(), 1);
}
