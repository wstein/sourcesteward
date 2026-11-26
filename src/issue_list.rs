//! Open-issue list for `stw status`, the Rust port of the approved spike in
//! `spikes/stw/status-issues` (pull request 46, issue 35).
//!
//! Repositories share one GraphQL query per page. A further page is fetched per repository
//! and for labels or assignees. A failed repository stays `incomplete`; issues already read
//! for the others stay. An empty list is only a successful read.
use std::collections::HashSet;

use serde_json::{json, Value};

use crate::github::reads::escape_untrusted;

pub const PAGE: usize = 20;
pub const BATCH: usize = 8;

#[derive(Clone, Debug, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Issue {
    pub number: i64,
    pub title: String,
    pub url: String,
    pub updated_at: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub comment_count: Option<u64>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub latest_comment: Option<String>,
    pub labels: Vec<String>,
    pub assignees: Vec<String>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
struct Pending {
    label_cursor: Option<String>,
    assignee_cursor: Option<String>,
    id: Option<String>,
    issue: Issue,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Listed {
    pub name: String,
    pub id: Option<String>,
    /// `complete` or `incomplete`.
    pub coverage: &'static str,
    pub error: Option<String>,
    pub issues: Vec<Issue>,
}

impl Listed {
    fn incomplete(&mut self, reason: impl Into<String>) {
        self.coverage = "incomplete";
        if self.error.is_none() {
            self.error = Some(reason.into());
        }
    }
}

fn checked_nodes<'a>(block: &'a Value, listed: &mut Listed, kind: &str) -> &'a [Value] {
    match block["nodes"].as_array() {
        Some(nodes) => nodes,
        None => {
            listed.incomplete(format!("{kind} response has no node list"));
            &[]
        }
    }
}

fn next_cursor(block: &Value, listed: &mut Listed, kind: &str) -> Option<String> {
    match block["pageInfo"]["hasNextPage"].as_bool() {
        Some(true) => match block["pageInfo"]["endCursor"]
            .as_str()
            .filter(|c| !c.is_empty())
        {
            Some(cursor) => Some(cursor.to_owned()),
            None => {
                listed.incomplete(format!("{kind} response has no next-page cursor"));
                None
            }
        },
        Some(false) => None,
        None => {
            listed.incomplete(format!("{kind} response has no pagination information"));
            None
        }
    }
}

fn metadata_page(
    block: &Value,
    field: &str,
    kind: &str,
    listed: &mut Listed,
) -> (Vec<String>, Option<String>) {
    let mut names = Vec::new();
    for item in checked_nodes(block, listed, kind) {
        if let Some(value) = item[field].as_str() {
            names.push(value.to_owned());
        } else {
            listed.incomplete(format!("{kind} response has an invalid node"));
        }
    }
    (names, next_cursor(block, listed, kind))
}

fn parse_issue(node: &Value, listed: &mut Listed) -> Option<Pending> {
    let mandatory = || {
        Some((
            node["number"].as_i64().filter(|n| *n > 0)?,
            node["title"].as_str()?,
            node["url"].as_str().filter(|s| !s.is_empty())?,
            node["updatedAt"].as_str().filter(|s| !s.is_empty())?,
        ))
    };
    let Some((number, title, url, updated_at)) = mandatory() else {
        listed.incomplete("issues response has an invalid issue");
        return None;
    };
    let (labels, label_cursor) = metadata_page(&node["labels"], "name", "labels", listed);
    let (assignees, assignee_cursor) =
        metadata_page(&node["assignees"], "login", "assignees", listed);
    Some(Pending {
        label_cursor,
        assignee_cursor,
        id: node["id"]
            .as_str()
            .filter(|s| !s.is_empty())
            .map(str::to_owned),
        issue: Issue {
            number,
            title: title.to_owned(),
            url: url.to_owned(),
            updated_at: updated_at.to_owned(),
            comment_count: node["comments"]["totalCount"].as_u64(),
            latest_comment: node["comments"]["nodes"]
                .as_array()
                .and_then(|nodes| nodes.last())
                .and_then(|comment| comment["id"].as_str())
                .map(str::to_owned),
            labels,
            assignees,
        },
    })
}

fn unseen_cursor(
    cursor: String,
    seen: &mut HashSet<String>,
    listed: &mut Listed,
    kind: &str,
) -> Option<String> {
    if seen.insert(cursor.clone()) {
        Some(cursor)
    } else {
        listed.incomplete(format!("{kind} response repeated a pagination cursor"));
        None
    }
}

fn issue_query(repos: &[String], cursors: &[Option<String>], page: usize) -> (String, Value) {
    let mut defs = Vec::new();
    let mut fields = Vec::new();
    let mut variables = json!({});
    for (i, name) in repos.iter().enumerate() {
        let (owner, repo) = name.split_once('/').unwrap_or((name, ""));
        defs.push(format!("$o{i}: String!, $n{i}: String!, $c{i}: String"));
        variables[format!("o{i}")] = json!(owner);
        variables[format!("n{i}")] = json!(repo);
        variables[format!("c{i}")] = cursors
            .get(i)
            .cloned()
            .flatten()
            .map(Value::String)
            .unwrap_or(Value::Null);
        fields.push(format!(
            "r{i}: repository(owner: $o{i}, name: $n{i}) {{ id issues(first: {page}, after: $c{i}, states: [OPEN], orderBy: {{field: CREATED_AT, direction: ASC}}) {{ pageInfo {{ hasNextPage endCursor }} nodes {{ id number title url updatedAt comments(last: 1) {{ totalCount nodes {{ id }} }} labels(first: {page}) {{ pageInfo {{ hasNextPage endCursor }} nodes {{ name }} }} assignees(first: {page}) {{ pageInfo {{ hasNextPage endCursor }} nodes {{ login }} }} }} }} }}"
        ));
    }
    (
        format!("query({}) {{ {} }}", defs.join(", "), fields.join(" ")),
        variables,
    )
}

fn meta_query(kind: &str, page: usize) -> String {
    let field = if kind == "labels" { "name" } else { "login" };
    format!(
        "query($id: ID!, $c: String) {{ node(id: $id) {{ ... on Issue {{ {kind}(first: {page}, after: $c) {{ pageInfo {{ hasNextPage endCursor }} nodes {{ {field} }} }} }} }} }}"
    )
}

fn errors_by_alias(payload: &Value) -> Vec<(String, String)> {
    let mut out = Vec::new();
    for err in payload["errors"].as_array().into_iter().flatten() {
        let message = err["message"]
            .as_str()
            .unwrap_or("graphql error")
            .to_string();
        let alias = err["path"]
            .as_array()
            .and_then(|p| p.first())
            .and_then(|a| a.as_str())
            .unwrap_or("*")
            .to_string();
        out.push((alias, message));
    }
    out
}

fn message_for<'a>(errors: &'a [(String, String)], alias: &str) -> Option<&'a str> {
    errors
        .iter()
        .find(|(a, _)| a == alias)
        .or_else(|| errors.iter().find(|(a, _)| a == "*"))
        .map(|(_, m)| m.as_str())
}

/// Read `slugs` through `post`, which sends one GraphQL document and returns the JSON body.
pub fn read<E>(
    slugs: &[String],
    page: usize,
    batch: usize,
    mut post: impl FnMut(&Value) -> Result<Value, E>,
) -> Vec<Listed>
where
    E: std::fmt::Display,
{
    let mut results: Vec<Listed> = slugs
        .iter()
        .map(|name| Listed {
            name: name.clone(),
            id: None,
            coverage: "complete",
            error: None,
            issues: Vec::new(),
        })
        .collect();
    let mut cursors: Vec<Option<String>> = slugs.iter().map(|_| None).collect();
    let mut seen = vec![HashSet::new(); slugs.len()];
    let mut need: Vec<usize> = (0..slugs.len()).collect();
    while !need.is_empty() {
        let mut nxt = Vec::new();
        for chunk in need.chunks(batch.max(1)) {
            let repos: Vec<String> = chunk.iter().map(|&i| slugs[i].clone()).collect();
            let curs: Vec<Option<String>> = chunk.iter().map(|&i| cursors[i].clone()).collect();
            let (query, variables) = issue_query(&repos, &curs, page);
            let payload = match post(&json!({"query": query, "variables": variables})) {
                Ok(v) => v,
                Err(e) => {
                    for &i in chunk {
                        results[i].incomplete(e.to_string());
                    }
                    continue;
                }
            };
            let failed = errors_by_alias(&payload);
            let data = &payload["data"];
            for (n, &i) in chunk.iter().enumerate() {
                let alias = format!("r{n}");
                let listed = &mut results[i];
                if let Some(message) = message_for(&failed, &alias) {
                    listed.incomplete(message);
                }
                let node = &data[&alias];
                let Some(id) = node["id"].as_str().filter(|s| !s.is_empty()) else {
                    listed.incomplete("no repository data");
                    continue;
                };
                if listed.id.as_deref().is_some_and(|previous| previous != id) {
                    listed.incomplete("repository changed during pagination");
                    continue;
                }
                listed.id = Some(id.to_owned());
                let block = &node["issues"];
                for item in checked_nodes(block, listed, "issues") {
                    if let Some(pending) = parse_issue(item, listed) {
                        follow_side(&mut post, listed, pending, page);
                    }
                }
                if let Some(cursor) = next_cursor(block, listed, "issues")
                    .and_then(|cursor| unseen_cursor(cursor, &mut seen[i], listed, "issues"))
                {
                    cursors[i] = Some(cursor);
                    nxt.push(i);
                }
            }
        }
        need = nxt;
    }
    results
}

fn follow_side<E>(
    post: &mut impl FnMut(&Value) -> Result<Value, E>,
    listed: &mut Listed,
    mut pending: Pending,
    page: usize,
) where
    E: std::fmt::Display,
{
    for (kind, field, mut cursor, values) in [
        (
            "labels",
            "name",
            pending.label_cursor.take(),
            &mut pending.issue.labels,
        ),
        (
            "assignees",
            "login",
            pending.assignee_cursor.take(),
            &mut pending.issue.assignees,
        ),
    ] {
        let mut seen = HashSet::new();
        while let Some(next) = cursor.and_then(|c| unseen_cursor(c, &mut seen, listed, kind)) {
            let Some(id) = &pending.id else {
                listed.incomplete(format!("issue has no id for a further {kind} page"));
                break;
            };
            let variables = json!({"id": id, "c": next});
            let payload =
                match post(&json!({"query": meta_query(kind, page), "variables": variables})) {
                    Ok(v) => v,
                    Err(e) => {
                        listed.incomplete(e.to_string());
                        break;
                    }
                };
            if let Some(error) = message_for(&errors_by_alias(&payload), "node") {
                listed.incomplete(error);
            }
            let block = &payload["data"]["node"][kind];
            let (names, next) = metadata_page(block, field, kind, listed);
            values.extend(names);
            cursor = next;
        }
    }
    listed.issues.push(pending.issue);
}

fn for_terminal(value: &str) -> String {
    let (mut visible, truncated) = escape_untrusted(value, 512);
    if truncated {
        visible.push_str("…");
    }
    visible
}

pub fn text_lines(listed: &Listed) -> Vec<String> {
    let mut lines = Vec::new();
    if listed.coverage == "complete" && listed.issues.is_empty() {
        lines.push("    open issues: none".into());
        return lines;
    }
    if listed.coverage != "complete" && listed.issues.is_empty() {
        lines.push(format!(
            "    open issues: incomplete ({})",
            for_terminal(listed.error.as_deref().unwrap_or("not read"))
        ));
        return lines;
    }
    lines.push("    open issues:".into());
    for issue in &listed.issues {
        let labels = if issue.labels.is_empty() {
            "-".into()
        } else {
            issue
                .labels
                .iter()
                .map(|label| for_terminal(label))
                .collect::<Vec<_>>()
                .join(", ")
        };
        let assignees = if issue.assignees.is_empty() {
            "-".into()
        } else {
            issue
                .assignees
                .iter()
                .map(|login| for_terminal(login))
                .collect::<Vec<_>>()
                .join(", ")
        };
        lines.push(format!(
            "      #{} {}  labels: {labels}  assignees: {assignees}",
            issue.number,
            for_terminal(&issue.title)
        ));
    }
    if listed.coverage != "complete" {
        lines.push(format!(
            "      incomplete: {}",
            for_terminal(listed.error.as_deref().unwrap_or("not read"))
        ));
    }
    lines
}

pub fn json_of(listed: &Listed) -> Value {
    json!({
        "id": listed.id,
        "coverage": listed.coverage,
        "error": listed.error,
        "issues": listed.issues.iter().map(|i| json!({
            "number": i.number,
            "title": i.title,
            "url": i.url,
            "updated_at": i.updated_at,
            "labels": i.labels,
            "assignees": i.assignees,
        })).collect::<Vec<_>>(),
    })
}
