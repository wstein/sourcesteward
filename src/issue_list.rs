//! Open-issue list for `stw status`, the Rust port of the approved spike in
//! `spikes/stw/status-issues` (pull request 46, issue 35).
//!
//! Repositories share one GraphQL query per page. A further page is fetched per repository
//! and for labels or assignees. A failed repository stays `incomplete`; issues already read
//! for the others stay. An empty list is only a successful read.
use serde_json::{json, Value};

pub const PAGE: usize = 20;
pub const BATCH: usize = 8;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Issue {
    pub number: i64,
    pub title: String,
    pub url: String,
    pub updated_at: String,
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

fn clean(text: &str) -> String {
    text.chars()
        .map(|c| {
            let n = c as u32;
            if n < 32 || n == 127 {
                '?'
            } else {
                c
            }
        })
        .collect()
}

fn names_of(block: &Value, field: &str) -> (Vec<String>, Option<String>) {
    let names = block["nodes"]
        .as_array()
        .map(|items| {
            items
                .iter()
                .filter_map(|item| item[field].as_str().map(clean))
                .collect()
        })
        .unwrap_or_default();
    let cursor = block["pageInfo"]["hasNextPage"]
        .as_bool()
        .unwrap_or(false)
        .then(|| {
            block["pageInfo"]["endCursor"]
                .as_str()
                .unwrap_or("")
                .to_string()
        });
    (names, cursor)
}

fn parse_issue(node: &Value) -> Pending {
    let (labels, label_cursor) = names_of(&node["labels"], "name");
    let (assignees, assignee_cursor) = names_of(&node["assignees"], "login");
    Pending {
        label_cursor,
        assignee_cursor,
        id: node["id"].as_str().map(str::to_string),
        issue: Issue {
            number: node["number"].as_i64().unwrap_or(0),
            title: clean(node["title"].as_str().unwrap_or("")),
            url: node["url"].as_str().unwrap_or("").to_string(),
            updated_at: node["updatedAt"].as_str().unwrap_or("").to_string(),
            labels,
            assignees,
        },
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
            "r{i}: repository(owner: $o{i}, name: $n{i}) {{ id issues(first: {page}, after: $c{i}, states: [OPEN], orderBy: {{field: CREATED_AT, direction: ASC}}) {{ pageInfo {{ hasNextPage endCursor }} nodes {{ id number title url updatedAt labels(first: {page}) {{ pageInfo {{ hasNextPage endCursor }} nodes {{ name }} }} assignees(first: {page}) {{ pageInfo {{ hasNextPage endCursor }} nodes {{ login }} }} }} }} }}"
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
                        results[i].coverage = "incomplete";
                        results[i].error = Some(e.to_string());
                    }
                    continue;
                }
            };
            let failed = errors_by_alias(&payload);
            let data = &payload["data"];
            if message_for(&failed, "*").is_some() && data.is_null() {
                let msg = message_for(&failed, "*").unwrap_or("graphql error");
                for &i in chunk {
                    results[i].coverage = "incomplete";
                    results[i].error = Some(msg.to_string());
                }
                continue;
            }
            for (n, &i) in chunk.iter().enumerate() {
                let alias = format!("r{n}");
                let node = &data[&alias];
                if message_for(&failed, &alias).is_some() || node.is_null() {
                    results[i].coverage = "incomplete";
                    results[i].error = Some(
                        message_for(&failed, &alias)
                            .unwrap_or("no repository data")
                            .to_string(),
                    );
                    continue;
                }
                results[i].id = node["id"].as_str().map(str::to_string);
                for item in node["issues"]["nodes"].as_array().into_iter().flatten() {
                    if !item.is_null() {
                        let pending = parse_issue(item);
                        follow_side(&mut post, &mut results[i], pending, page);
                    }
                }
                if node["issues"]["pageInfo"]["hasNextPage"]
                    .as_bool()
                    .unwrap_or(false)
                {
                    cursors[i] = node["issues"]["pageInfo"]["endCursor"]
                        .as_str()
                        .map(str::to_string);
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
    for (kind, cursor) in [
        ("labels", &mut pending.label_cursor),
        ("assignees", &mut pending.assignee_cursor),
    ] {
        while cursor.is_some() {
            let Some(id) = pending.id.clone() else {
                listed.coverage = "incomplete";
                listed.error = Some(format!("issue has no id for a further {kind} page"));
                break;
            };
            let variables = json!({"id": id, "c": cursor.clone()});
            let payload =
                match post(&json!({"query": meta_query(kind, page), "variables": variables})) {
                    Ok(v) => v,
                    Err(e) => {
                        listed.coverage = "incomplete";
                        listed.error = Some(e.to_string());
                        break;
                    }
                };
            let failed = errors_by_alias(&payload);
            let block = &payload["data"]["node"][kind];
            if !failed.is_empty() || block.is_null() {
                listed.coverage = "incomplete";
                listed.error = Some(
                    failed
                        .first()
                        .map(|(_, m)| m.clone())
                        .unwrap_or_else(|| format!("further {kind} page failed")),
                );
                break;
            }
            let (names, next) = names_of(block, if kind == "labels" { "name" } else { "login" });
            if kind == "labels" {
                pending.issue.labels.extend(names);
            } else {
                pending.issue.assignees.extend(names);
            }
            *cursor = next;
        }
    }
    listed.issues.push(pending.issue);
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
            listed.error.as_deref().unwrap_or("not read")
        ));
        return lines;
    }
    lines.push("    open issues:".into());
    for issue in &listed.issues {
        let labels = if issue.labels.is_empty() {
            "-".into()
        } else {
            issue.labels.join(", ")
        };
        let assignees = if issue.assignees.is_empty() {
            "-".into()
        } else {
            issue.assignees.join(", ")
        };
        lines.push(format!(
            "      #{} {}  labels: {labels}  assignees: {assignees}",
            issue.number, issue.title
        ));
    }
    if listed.coverage != "complete" {
        lines.push(format!(
            "      incomplete: {}",
            listed.error.as_deref().unwrap_or("not read")
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
