//! Read-only readiness evaluation for one pull request.
use crate::github::reads::escape_untrusted;

#[derive(Default, Clone, Debug)]
pub struct Facts {
    pub pull_error: String,
    pub repo_error: String,
    pub checks_error: String,
    pub review_error: String,
    pub parent_error: String,
    pub state: String,
    pub draft: bool,
    pub head: String,
    pub base_ref: String,
    pub body: String,
    pub mergeable: Option<bool>,
    pub mergeable_state: String,
    pub rebaseable: Option<bool>,
    pub default_branch: String,
    pub checks: String,
    pub review: String,
    pub parent_pr: u64,
}
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Status {
    Pass,
    Fail,
    Unknown,
}
impl Status {
    pub fn label(self) -> &'static str {
        match self {
            Self::Pass => "pass",
            Self::Fail => "fail",
            Self::Unknown => "unknown",
        }
    }
}
#[derive(Debug)]
pub struct Condition {
    pub id: &'static str,
    pub status: Status,
    pub evidence: String,
}
fn condition(id: &'static str, status: Status, evidence: String) -> Condition {
    let mut end = evidence.len().min(400);
    while !evidence.is_char_boundary(end) {
        end -= 1;
    }
    Condition {
        id,
        status,
        evidence: evidence[..end].into(),
    }
}
fn esc(s: &str) -> String {
    escape_untrusted(s, 400).0
}
fn short(s: &str) -> &str {
    s.get(..7).unwrap_or(s)
}
pub fn ready(c: &[Condition]) -> bool {
    !c.is_empty() && c.iter().all(|c| c.status == Status::Pass)
}

pub fn evaluate(f: &Facts) -> Vec<Condition> {
    use Status::*;
    let pull = !f.pull_error.is_empty();
    let open = if pull {
        (
            Unknown,
            format!("pull request not read: {}", esc(&f.pull_error)),
        )
    } else if f.state == "open" {
        (Pass, "state open".into())
    } else {
        (Fail, format!("state {}", esc(&f.state)))
    };
    let review = if !f.review_error.is_empty() {
        (
            Unknown,
            format!("review state not read: {}", esc(&f.review_error)),
        )
    } else if pull {
        (Unknown, format!("head not read: {}", esc(&f.pull_error)))
    } else {
        match f.review.as_str() {
            "clear" => (
                Pass,
                format!("CLEAR for head {} per comment", short(&f.head)),
            ),
            "clear-carried" => (
                Pass,
                format!("CLEAR carried over to head {} per comment", short(&f.head)),
            ),
            "findings" => (
                Fail,
                format!("review of head {} has findings", short(&f.head)),
            ),
            "no-review" => (Fail, format!("no CLEAR for head {}", short(&f.head))),
            "unverified-author" => (Fail, "review author is not trusted".into()),
            _ => (Unknown, "review state unknown".into()),
        }
    };
    let checks = if !f.checks_error.is_empty() {
        (
            Unknown,
            format!("checks not read: {}", esc(&f.checks_error)),
        )
    } else if pull {
        (Unknown, format!("head not read: {}", esc(&f.pull_error)))
    } else {
        match f.checks.as_str() {
            "green" => (
                Pass,
                format!(
                    "all check runs and statuses of head {} are green",
                    short(&f.head)
                ),
            ),
            "red" | "pending" => (
                Fail,
                format!("checks of head {} are {}", short(&f.head), f.checks),
            ),
            "none" => (
                Fail,
                format!("no checks reported for head {}", short(&f.head)),
            ),
            _ => (
                Unknown,
                format!("check class {} is not known", esc(&f.checks)),
            ),
        }
    };
    let base = if pull {
        (Unknown, format!("base not read: {}", esc(&f.pull_error)))
    } else if !f.repo_error.is_empty() {
        (
            Unknown,
            format!("default branch not read: {}", esc(&f.repo_error)),
        )
    } else if f.default_branch.is_empty() {
        (Unknown, "default branch unknown".into())
    } else if f.base_ref == f.default_branch {
        (
            Pass,
            format!("base {} is the default branch", esc(&f.base_ref)),
        )
    } else if !f.parent_error.is_empty() {
        (
            Unknown,
            format!("open pull requests not read: {}", esc(&f.parent_error)),
        )
    } else if f.parent_pr > 0 {
        (
            Pass,
            format!(
                "stack child: base {} is the head branch of open #{}, merge #{} first",
                esc(&f.base_ref),
                f.parent_pr,
                f.parent_pr
            ),
        )
    } else {
        (Fail, format!("base {} is not the default branch {} and no other open pull request has it as head branch", esc(&f.base_ref), esc(&f.default_branch)))
    };
    let blocker = if pull {
        (Unknown, format!("text not read: {}", esc(&f.pull_error)))
    } else if let Some(why) = blocker(&f.body) {
        (Fail, why)
    } else {
        (Pass, "no Blocker line names a blocker".into())
    };
    let conflict = if pull {
        (
            Unknown,
            format!("mergeability not read: {}", esc(&f.pull_error)),
        )
    } else {
        match f.mergeable {
            None => (
                Unknown,
                "mergeable not computed by GitHub yet, run again".into(),
            ),
            Some(false) => (
                Fail,
                format!(
                    "conflict with the base (mergeable false, state {})",
                    esc(&f.mergeable_state)
                ),
            ),
            Some(true) if f.mergeable_state == "dirty" => (
                Fail,
                "conflict with the base (mergeable true, state dirty)".into(),
            ),
            Some(true) => (
                Pass,
                format!(
                    "no conflict with the base (state {})",
                    esc(&f.mergeable_state)
                ),
            ),
        }
    };
    let rebase = if pull {
        (
            Unknown,
            format!("rebaseable not read: {}", esc(&f.pull_error)),
        )
    } else {
        match f.rebaseable {
            None => (
                Unknown,
                "rebaseable not computed by GitHub yet, run again".into(),
            ),
            Some(false) => (Fail, "rebaseable is false".into()),
            Some(true) => (Pass, "rebaseable is true".into()),
        }
    };
    [
        "open",
        "review",
        "checks",
        "base",
        "blocker",
        "conflict",
        "rebaseable",
    ]
    .into_iter()
    .zip([open, review, checks, base, blocker, conflict, rebase])
    .map(|(id, (s, e))| condition(id, s, e))
    .collect()
}

fn heading(s: &str) -> bool {
    let s = s.trim_start();
    let n = s.bytes().take_while(|b| *b == b'#').count();
    (1..=6).contains(&n) && s[n..].chars().next().is_none_or(char::is_whitespace)
}
fn none_value(s: &str) -> bool {
    let s = s.trim();
    let s = if let Some(rest) = s.strip_prefix(['-', '*', '+']) {
        if rest.starts_with(char::is_whitespace) {
            rest.trim_start()
        } else {
            s
        }
    } else {
        s
    };
    let digits = s.bytes().take_while(u8::is_ascii_digit).count();
    let s = if digits > 0
        && matches!(s.as_bytes().get(digits), Some(b'.' | b')'))
        && s[digits + 1..].starts_with(char::is_whitespace)
    {
        s[digits + 1..].trim_start()
    } else {
        s
    };
    let s = s
        .trim_matches(|c: char| c == '|' || c.is_whitespace())
        .trim_end_matches(['.', ',', ';', ' ']);
    matches!(
        s.to_lowercase().as_str(),
        "none" | "keiner" | "keine" | "n/a" | "-" | "no"
    )
}
fn labels(line: &str) -> (Vec<String>, bool, bool) {
    let lower = line.to_ascii_lowercase();
    let mut found = Vec::new();
    let mut pos = 0;
    while let Some(off) = lower[pos..].find("blocker") {
        let start = pos + off;
        let mut end = start + 7;
        if start > 0 && lower.as_bytes()[start - 1].is_ascii_alphanumeric()
            || start > 0 && lower.as_bytes()[start - 1] == b'_'
        {
            pos = end;
            continue;
        }
        if lower[end..].starts_with('s') {
            end += 1;
        }
        if lower[end..]
            .chars()
            .next()
            .is_some_and(|c| c.is_ascii_alphanumeric() || c == '_')
        {
            pos = end;
            continue;
        }
        let mut p = end;
        while let Some(c) = line[p..].chars().next() {
            if !c.is_whitespace() {
                break;
            }
            p += c.len_utf8();
        }
        if line[p..].starts_with('(') {
            if let Some(close) = line[p..].find(')') {
                p += close + 1;
                while let Some(c) = line[p..].chars().next() {
                    if !c.is_whitespace() {
                        break;
                    }
                    p += c.len_utf8();
                }
            }
        }
        let sep = line[p..].chars().next();
        let len = match sep {
            Some(
                ':' | '：' | '|' | '−' | '֊' | '־' | '᐀' | '᠆' | '‐' | '‑' | '‒' | '–' | '—' | '―'
                | '⸗' | '⸚' | '⸺' | '⸻' | '⹀' | '⹝' | '〜' | '〰' | '゠' | '︱' | '︲' | '﹘'
                | '﹣' | '－' | '𐺭',
            ) => sep.unwrap().len_utf8(),
            Some('-') if line[p + 1..].starts_with('-') => {
                line[p..].bytes().take_while(|b| *b == b'-').count()
            }
            Some('-') if line[p + 1..].chars().next().is_none_or(char::is_whitespace) => 1,
            _ => 0,
        };
        if len > 0 {
            found.push((start, p + len));
            pos = p + len;
        } else {
            pos = end;
        }
    }
    if !found.is_empty() {
        let vals: Vec<String> = found
            .iter()
            .enumerate()
            .map(|(i, (_, end))| {
                line[*end..found.get(i + 1).map_or(line.len(), |x| x.0)]
                    .trim()
                    .to_string()
            })
            .collect();
        let section = vals.last().is_some_and(String::is_empty);
        return (vals, section, true);
    }
    let bare = line
        .trim()
        .trim_start_matches(['#', '>', '-', '+', ' '])
        .trim();
    let bare = bare
        .trim_start_matches(|c: char| c.is_ascii_digit() || c == '.' || c == ')')
        .trim();
    let heading_word = heading(line)
        && lower
            .split(|c: char| !c.is_ascii_alphanumeric() && c != '_')
            .any(|w| w == "blocker" || w == "blockers");
    let ok = heading_word
        || bare.eq_ignore_ascii_case("blocker")
        || bare.eq_ignore_ascii_case("blockers");
    (vec![], ok, ok)
}
fn blocker(body: &str) -> Option<String> {
    let lines: Vec<_> = body.lines().collect();
    let mut i = 0;
    while i < lines.len() {
        let line = lines[i].trim_end_matches('\r').replace(['*', '_', '`'], "");
        let (vals, section, ok) = labels(&line);
        if !ok {
            i += 1;
            continue;
        }
        for v in vals {
            if !v.is_empty() && !none_value(&v) {
                return Some(format!("Blocker line: {}", esc(&v)));
            }
        }
        if section {
            let h = heading(&line);
            let mut below = Vec::new();
            i += 1;
            while i < lines.len() {
                let l = lines[i].trim_end_matches('\r');
                if heading(l) || (!h && l.trim().is_empty() && !below.is_empty()) {
                    break;
                }
                if !l.trim().is_empty() {
                    below.push(l);
                }
                i += 1;
            }
            if below.is_empty() {
                return Some("a Blocker line has no value".into());
            }
            for l in below {
                let cleaned = l.replace(['*', '_', '`'], "");
                if !none_value(&cleaned) {
                    return Some(format!("Blocker section: {}", esc(l.trim())));
                }
            }
            continue;
        }
        i += 1;
    }
    None
}

/// Decode the one-pull response; missing mergeability remains unknown.
pub fn decode_pull(v: &serde_json::Value) -> Result<Facts, String> {
    let malformed = || "pull request response is not usable".to_string();
    let head = v["head"]["sha"].as_str().ok_or_else(malformed)?;
    let base = v["base"]["ref"]
        .as_str()
        .filter(|s| !s.is_empty())
        .ok_or_else(malformed)?;
    let state = v["state"]
        .as_str()
        .filter(|s| !s.is_empty())
        .ok_or_else(malformed)?;
    if head.len() != 40
        || !head
            .bytes()
            .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
    {
        return Err(malformed());
    }
    for key in ["merged", "draft", "mergeable", "rebaseable"] {
        if !v[key].is_null() && !v[key].is_boolean() {
            return Err(malformed());
        }
    }
    for key in ["body", "mergeable_state"] {
        if !v[key].is_null() && !v[key].is_string() {
            return Err(malformed());
        }
    }
    Ok(Facts {
        state: if v["merged"].as_bool().unwrap_or(false) {
            "merged"
        } else {
            state
        }
        .into(),
        head: head.into(),
        base_ref: base.into(),
        draft: v["draft"].as_bool().unwrap_or(false),
        body: v["body"].as_str().unwrap_or("").into(),
        mergeable: v["mergeable"].as_bool(),
        rebaseable: v["rebaseable"].as_bool(),
        mergeable_state: v["mergeable_state"].as_str().unwrap_or("").into(),
        ..Facts::default()
    })
}

use crate::github::reads::{Budget, CheckRun, Comment, CommitStatus, Read, ReadyParent};
use crate::github::Client;
use serde_json::{json, Value};

/// Read-only source, separated so the collection rules can be exercised without a forge.
pub trait Source {
    fn pull(&self, repo: &str, number: u64) -> Result<Value, String>;
    fn repository(&self, repo: &str) -> Result<Value, String>;
    fn parents(&self, repo: &str) -> Result<Read<ReadyParent>, String>;
    fn runs(&self, repo: &str, head: &str) -> Result<Read<CheckRun>, String>;
    fn statuses(&self, repo: &str, head: &str) -> Result<Read<CommitStatus>, String>;
    fn comments(&self, repo: &str, number: u64) -> Result<Read<Comment>, String>;
}
impl Source for Client {
    fn pull(&self, r: &str, n: u64) -> Result<Value, String> {
        self.lint_pull(r, n)
            .map(|p| p.body)
            .map_err(|e| e.to_string())
    }
    fn repository(&self, r: &str) -> Result<Value, String> {
        self.ready_repo(r)
            .map(|p| p.body)
            .map_err(|e| e.to_string())
    }
    fn parents(&self, r: &str) -> Result<Read<ReadyParent>, String> {
        self.ready_parents(r, &Budget::default())
            .map_err(|e| e.to_string())
    }
    fn runs(&self, r: &str, h: &str) -> Result<Read<CheckRun>, String> {
        self.check_runs(r, h, &Budget::default())
            .map_err(|e| e.to_string())
    }
    fn statuses(&self, r: &str, h: &str) -> Result<Read<CommitStatus>, String> {
        self.commit_statuses(r, h, &Budget::default())
            .map_err(|e| e.to_string())
    }
    fn comments(&self, r: &str, n: u64) -> Result<Read<Comment>, String> {
        self.issue_comments(r, n, &Budget::default())
            .map_err(|e| e.to_string())
    }
}
fn complete<T>(r: Result<Read<T>, String>) -> Result<Vec<T>, String> {
    match r? {
        Read::Observed(o) if o.incomplete.is_none() => Ok(o.items),
        Read::Observed(o) => Err(o.incomplete.unwrap_or_default()),
        Read::Denied { message, .. } => Err(message),
        Read::NotFound => Err("not found".into()),
        Read::Failed(e) => Err(e),
    }
}
fn check_class(runs: &[CheckRun], statuses: &[CommitStatus]) -> Result<&'static str, String> {
    use std::collections::HashSet;
    if runs.iter().any(|r| {
        r.status.is_empty()
            || (r.status == "completed" && r.conclusion.as_deref().is_none_or(str::is_empty))
    }) || statuses
        .iter()
        .any(|s| s.context.is_empty() || s.state.is_empty())
    {
        return Err("check runs or statuses lack state evidence".into());
    }
    // The API orders statuses newest first. Keep the first observation of each context across pages.
    let mut seen = HashSet::new();
    let statuses: Vec<_> = statuses
        .iter()
        .filter(|s| seen.insert(&s.context))
        .collect();
    Ok(
        if runs.iter().any(|r| {
            r.status == "completed"
                && matches!(
                    r.conclusion.as_deref(),
                    Some(
                        "failure"
                            | "timed_out"
                            | "cancelled"
                            | "action_required"
                            | "startup_failure"
                    )
                )
        }) || statuses
            .iter()
            .any(|s| matches!(s.state.as_str(), "failure" | "error"))
        {
            "red"
        } else if runs.iter().any(|r| r.status != "completed")
            || statuses.iter().any(|s| s.state == "pending")
        {
            "pending"
        } else if !runs.is_empty() || !statuses.is_empty() {
            "green"
        } else {
            "none"
        },
    )
}
/// Collect facts with GET reads only. Incomplete reads do not count as evidence.
pub fn collect(src: &dyn Source, repo: &str, number: u64) -> (Facts, Vec<(&'static str, String)>) {
    let mut errors = Vec::new();
    let mut f = match src.pull(repo, number).and_then(|p| decode_pull(&p)) {
        Ok(p) => p,
        Err(e) => {
            errors.push(("pull", e.clone()));
            Facts {
                pull_error: e,
                ..Facts::default()
            }
        }
    };
    match src.repository(repo).and_then(|v| {
        v["default_branch"]
            .as_str()
            .map(str::to_string)
            .ok_or_else(|| "repository response is not usable".into())
    }) {
        Ok(b) => f.default_branch = b,
        Err(e) => {
            f.repo_error = e.clone();
            errors.push(("repo", e));
        }
    }
    if f.pull_error.is_empty()
        && f.repo_error.is_empty()
        && !f.default_branch.is_empty()
        && f.base_ref != f.default_branch
    {
        match complete(src.parents(repo)) {
            Ok(items) => {
                f.parent_pr = items
                    .iter()
                    .filter(|p| p.number != number && p.head_ref == f.base_ref)
                    .map(|p| p.number)
                    .min()
                    .unwrap_or(0)
            }
            Err(e) => {
                f.parent_error = e.clone();
                errors.push(("pulls", e));
            }
        }
    }
    if f.pull_error.is_empty() {
        let result = complete(src.runs(repo, &f.head)).and_then(|runs| {
            complete(src.statuses(repo, &f.head)).and_then(|statuses| check_class(&runs, &statuses))
        });
        match result {
            Ok(class) => f.checks = class.into(),
            Err(e) => {
                f.checks_error = e.clone();
                errors.push(("checks", e));
            }
        }
        let read = src.comments(repo, number).unwrap_or_else(Read::Failed);
        let result = crate::status_review::evaluate(number, &f.head, &read);
        f.review = result.label().into();
        if let Some(e) = result.problem {
            f.review_error = e.clone();
            errors.push(("review", e));
        }
    }
    (f, errors)
}
/// One machine-readable readiness document.
pub fn document(repo: &str, number: u64, f: &Facts, errors: &[(&str, String)], now: i64) -> Value {
    let cs = evaluate(f);
    json!({"schema_version":2, "command":"ready-check", "time":crate::lint_pr_cmd::rfc3339_utc(now), "host":"github.com", "repo":repo, "number":number,
        "head": if f.pull_error.is_empty() { Some(&f.head) } else { None }, "draft": if f.pull_error.is_empty() { Some(f.draft) } else { None }, "ready":ready(&cs),
        "conditions":cs.iter().map(|c| json!({"id":c.id,"status":c.status.label(),"evidence":c.evidence})).collect::<Vec<_>>(),
        "errors":errors.iter().map(|(source,message)| json!({"source":source,"code":"read-failed","message":esc(message)})).collect::<Vec<_>>() })
}
