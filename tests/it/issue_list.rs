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
