//! Bounded, paginated GitHub reads for the review and readiness milestones: issue comments, pull request
//! reviews, check runs and commit statuses (REST GET only).
//!
//! Rules:
//! - Every list is read page by page under a `Budget` (pages, items, text length, total time). A list that
//!   hit a bound, lost a page or contained a malformed entry is `Observed` with `incomplete: Some(reason)`:
//!   partial evidence is UNKNOWN evidence and `Read::is_complete` is false, so it can never make a PR Ready.
//! - The token may lack a permission (check runs need `Checks: read`): 401/403/rate limit are the typed
//!   `Read::Denied`, 404 is `Read::NotFound`; no read panics or aborts the run.
//! - Text from GitHub is untrusted. Every string is cut at a length bound and escaped (`escape_untrusted`):
//!   backslash, control characters, line breaks and invisible or direction-changing characters become
//!   visible escapes, so a body cannot forge a line, a terminal sequence or a review header in any output.
//!   The escape is reversible (the backslash is escaped too), so a parser can recover the raw text.
//! - The URL pieces (repository slug, number, SHA) are validated before they are put into a path. The token
//!   is only ever sent to the configured API base.
use std::time::{Duration, Instant};

use serde_json::Value;

use super::Client;
use crate::error::{Error, Result};

/// Bounds of one list read.
#[derive(Clone, Debug)]
pub struct Budget {
    /// Items per request (GitHub allows 1..=100).
    pub per_page: usize,
    pub max_pages: usize,
    pub max_items: usize,
    /// Characters kept of a body or description before escaping.
    pub max_text: usize,
    /// Wall-clock budget for the whole list.
    pub time: Duration,
}

impl Default for Budget {
    fn default() -> Self {
        Budget {
            per_page: 100,
            max_pages: 10,
            max_items: 500,
            max_text: 16 * 1024,
            time: Duration::from_secs(60),
        }
    }
}

/// Why the token was refused.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Denial {
    /// 401: token rejected.
    Unauthorized,
    /// 403: the token lacks the permission for this endpoint (e.g. Checks: read).
    Forbidden,
    /// 403/429 with an exhausted rate limit.
    RateLimited,
}

impl Denial {
    pub fn label(self) -> &'static str {
        match self {
            Denial::Unauthorized => "unauthorized",
            Denial::Forbidden => "forbidden",
            Denial::RateLimited => "rate-limited",
        }
    }
}

/// The items read and whether they are all there.
#[derive(Clone, Debug)]
pub struct Observed<T> {
    pub items: Vec<T>,
    pub pages: usize,
    /// None: the whole list was read. Some(reason): partial, treat as UNKNOWN.
    pub incomplete: Option<String>,
}

impl<T> Observed<T> {
    pub fn is_complete(&self) -> bool {
        self.incomplete.is_none()
    }
}

/// Outcome of a list read; never a panic and never an `Err` for an API answer.
#[derive(Clone, Debug)]
pub enum Read<T> {
    Observed(Observed<T>),
    Denied {
        kind: Denial,
        status: u16,
        message: String,
    },
    NotFound,
    /// Unreachable, unreadable or unexpected answer on the first page.
    Failed(String),
}

impl<T> Read<T> {
    /// True only for a fully read list: the only state that may count as evidence for Ready.
    pub fn is_complete(&self) -> bool {
        matches!(self, Read::Observed(o) if o.is_complete())
    }
    /// Stable label for output: `complete`, `partial`, `denied`, `not-found`, `failed`.
    pub fn state(&self) -> &'static str {
        match self {
            Read::Observed(o) if o.is_complete() => "complete",
            Read::Observed(_) => "partial",
            Read::Denied { .. } => "denied",
            Read::NotFound => "not-found",
            Read::Failed(_) => "failed",
        }
    }
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Comment {
    pub submitted_review: bool,
    pub id: u64,
    pub author: String,
    pub author_type: String,
    pub association: String,
    pub created_at: String,
    pub updated_at: String,
    /// Escaped and cut; see `escape_untrusted`.
    pub body: String,
    pub body_truncated: bool,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Review {
    pub id: u64,
    pub author: String,
    pub author_type: String,
    pub association: String,
    /// `APPROVED`, `CHANGES_REQUESTED`, `COMMENTED`, `DISMISSED`, `PENDING` (escaped).
    pub state: String,
    /// The commit the review was made on; None unless it is a 40 or 64 digit lower-case hex id.
    pub commit_id: Option<String>,
    pub submitted_at: Option<String>,
    pub body: String,
    pub body_truncated: bool,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct CheckRun {
    pub id: u64,
    pub name: String,
    /// `queued`, `in_progress`, `completed`, ...
    pub status: String,
    /// None while not completed.
    pub conclusion: Option<String>,
    pub head_sha: Option<String>,
    pub app: Option<String>,
    pub started_at: Option<String>,
    pub completed_at: Option<String>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct CommitStatus {
    pub id: u64,
    /// `error`, `failure`, `pending`, `success`
    pub state: String,
    pub context: String,
    pub description: String,
    pub creator: Option<String>,
    pub created_at: String,
    pub updated_at: String,
}

/// One run of an Actions workflow (`actions/runs`), the fields `stw status` needs for the default branch CI.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct WorkflowRun {
    pub id: u64,
    pub workflow_id: u64,
    /// The workflow name (escaped and cut).
    pub name: String,
    /// `push`, `pull_request`, `schedule`, ... (escaped and cut).
    pub event: String,
    /// `queued`, `in_progress`, `completed`, `waiting`, `requested`, `pending`.
    pub status: String,
    /// None while not completed.
    pub conclusion: Option<String>,
    pub head_sha: Option<String>,
    pub created_at: String,
    /// Which attempt of the run this is (1 when the answer has none).
    pub run_attempt: u64,
}

/// Escape untrusted text for any output. Cuts at `max_chars` characters first (the flag says so), then
/// replaces `\` by `\\`, line feed, carriage return and tab by `\n`, `\r`, `\t`, other control characters and
/// invisible or direction-changing characters (zero-width, bidi, line/paragraph separators, BOM, soft hyphen)
/// by `\u{hex}`. Printable text is unchanged.
pub fn escape_untrusted(s: &str, max_chars: usize) -> (String, bool) {
    let mut out = String::with_capacity(s.len().min(max_chars) + 8);
    let mut truncated = false;
    for (i, c) in s.chars().enumerate() {
        if i >= max_chars {
            truncated = true;
            break;
        }
        match c {
            '\\' => out.push_str("\\\\"),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            c if c.is_control() || is_invisible(c) => {
                out.push_str(&format!("\\u{{{:x}}}", c as u32));
            }
            c => out.push(c),
        }
    }
    (out, truncated)
}

fn is_invisible(c: char) -> bool {
    matches!(c as u32,
        0x00AD | 0x034F | 0x061C | 0x115F | 0x1160 | 0x17B4 | 0x17B5 | 0x180B..=0x180E
        | 0x200B..=0x200F | 0x2028..=0x202E | 0x2060..=0x206F | 0x3164 | 0xFE00..=0xFE0F
        | 0xFEFF | 0xFFA0 | 0xFFF9..=0xFFFB | 0xE0000..=0xE0FFF)
}

fn esc(v: &Value, cap: usize) -> String {
    escape_untrusted(v.as_str().unwrap_or(""), cap).0
}

fn esc_opt(v: &Value, cap: usize) -> Option<String> {
    v.as_str().map(|s| escape_untrusted(s, cap).0)
}

/// A commit id as GitHub sends it, or None for anything else.
fn hex_id(v: &Value) -> Option<String> {
    let s = v.as_str()?;
    (matches!(s.len(), 40 | 64) && s.bytes().all(|b| matches!(b, b'0'..=b'9' | b'a'..=b'f')))
        .then(|| s.to_string())
}

fn valid_slug(slug: &str) -> bool {
    let ok = |p: &str| {
        !p.is_empty()
            && p != "."
            && p != ".."
            && p.bytes()
                .all(|b| b.is_ascii_alphanumeric() || matches!(b, b'-' | b'_' | b'.'))
    };
    matches!(slug.split_once('/'), Some((o, r)) if ok(o) && ok(r))
}

fn user(v: &Value) -> (String, String) {
    (esc(&v["login"], 100), esc(&v["type"], 40))
}

fn parse_comment(v: &Value, b: &Budget) -> Option<Comment> {
    let (body, body_truncated) = escape_untrusted(v["body"].as_str().unwrap_or(""), b.max_text);
    let (author, author_type) = user(&v["user"]);
    Some(Comment {
        submitted_review: false,
        id: v["id"].as_u64()?,
        author,
        author_type,
        association: esc(&v["author_association"], 40),
        created_at: esc(&v["created_at"], 40),
        updated_at: esc(&v["updated_at"], 40),
        body,
        body_truncated,
    })
}

fn parse_review(v: &Value, b: &Budget) -> Option<Review> {
    let (body, body_truncated) = escape_untrusted(v["body"].as_str().unwrap_or(""), b.max_text);
    let (author, author_type) = user(&v["user"]);
    Some(Review {
        id: v["id"].as_u64()?,
        author,
        author_type,
        association: esc(&v["author_association"], 40),
        state: esc(&v["state"], 40),
        commit_id: hex_id(&v["commit_id"]),
        submitted_at: esc_opt(&v["submitted_at"], 40),
        body,
        body_truncated,
    })
}

fn string_or_null(v: &Value) -> bool {
    v.is_null() || v.is_string()
}

fn parse_check_run(v: &Value, _b: &Budget) -> Option<CheckRun> {
    if !v.is_object()
        || !["name", "status", "conclusion"]
            .iter()
            .all(|k| string_or_null(&v[*k]))
    {
        return None;
    }
    let output = &v["output"];
    if !output.is_null()
        && (!output.is_object()
            || !["title", "summary", "text"]
                .iter()
                .all(|k| string_or_null(&output[*k])))
    {
        return None;
    }
    Some(CheckRun {
        id: v["id"].as_u64()?,
        name: esc(&v["name"], 200),
        status: esc(&v["status"], 40),
        conclusion: esc_opt(&v["conclusion"], 40),
        head_sha: hex_id(&v["head_sha"]),
        app: esc_opt(&v["app"]["slug"], 100),
        started_at: esc_opt(&v["started_at"], 40),
        completed_at: esc_opt(&v["completed_at"], 40),
    })
}

fn parse_status(v: &Value, b: &Budget) -> Option<CommitStatus> {
    if !v.is_object()
        || !["state", "context", "description"]
            .iter()
            .all(|k| string_or_null(&v[*k]))
    {
        return None;
    }
    // A truncated context cannot safely identify which history entries belong together.
    if v["context"]
        .as_str()
        .is_some_and(|s| escape_untrusted(s, 200).1)
    {
        return None;
    }
    Some(CommitStatus {
        id: v["id"].as_u64()?,
        state: esc(&v["state"], 40),
        context: esc(&v["context"], 200),
        description: escape_untrusted(
            v["description"].as_str().unwrap_or(""),
            b.max_text.min(1024),
        )
        .0,
        creator: esc_opt(&v["creator"]["login"], 100),
        created_at: esc(&v["created_at"], 40),
        updated_at: esc(&v["updated_at"], 40),
    })
}

pub(crate) fn parse_workflow_run(v: &Value, _b: &Budget) -> Option<WorkflowRun> {
    if !v.is_object()
        || !["name", "event", "conclusion", "created_at"]
            .iter()
            .all(|k| string_or_null(&v[*k]))
        || !v["status"].is_string()
    {
        return None;
    }
    Some(WorkflowRun {
        id: v["id"].as_u64()?,
        workflow_id: v["workflow_id"].as_u64()?,
        name: esc(&v["name"], 200),
        event: esc(&v["event"], 40),
        status: esc(&v["status"], 40),
        conclusion: esc_opt(&v["conclusion"], 40),
        head_sha: hex_id(&v["head_sha"]),
        created_at: esc(&v["created_at"], 40),
        run_attempt: v["run_attempt"].as_u64().unwrap_or(1),
    })
}

/// One answer of the API.
enum Page {
    Json(Value),
    Denied(Denial, u16, String),
    NotFound,
    Failed(String),
}

impl Client {
    /// One GET, cut off after `timeout` (never longer than the client's own 30 s, nor than what is left of the
    /// overall `--timeout`). Counted in the stats like every other request.
    fn page(&self, url: &str, timeout: Duration) -> Page {
        let path = url.trim_start_matches(self.base.as_str()).to_string();
        let timeout = match self.time_left() {
            Ok(left) => timeout.min(left),
            Err(m) => return Page::Failed(m),
        };
        let started = Instant::now();
        let sent = self
            .agent
            .get(url)
            .config()
            .timeout_global(Some(timeout))
            .build()
            .header("Authorization", &format!("Bearer {}", self.token))
            .header("Accept", "application/vnd.github+json")
            .header("X-GitHub-Api-Version", "2022-11-28")
            .call();
        let rate = sent
            .as_ref()
            .ok()
            .and_then(|r| super::rate_from(r.headers()));
        self.record(&path, started.elapsed(), rate);
        let mut resp = match sent {
            Ok(r) => r,
            Err(e) => {
                return Page::Failed(match self.deadline_passed() {
                    Some(total) => super::deadline_message(total),
                    None => format!("GitHub API unreachable for {path}: {e}"),
                })
            }
        };
        let status = resp.status().as_u16();
        let limited = super::is_rate_limit(&mut resp);
        if limited {
            self.note_rate_limit();
        }
        match status {
            200 => match resp.body_mut().with_config().limit(8 << 20).read_json::<Value>() {
                Ok(v) => Page::Json(v),
                Err(e) => Page::Failed(format!("GitHub API sent unreadable JSON for {path}: {e}")),
            },
            404 => Page::NotFound,
            401 => Page::Denied(
                Denial::Unauthorized,
                401,
                format!("GitHub API HTTP 401 for {path}: token rejected"),
            ),
            403 | 429 if limited => Page::Denied(
                Denial::RateLimited,
                status,
                format!("GitHub API HTTP {status} for {path}: rate limit reached; retry later"),
            ),
            403 => Page::Denied(
                Denial::Forbidden,
                403,
                format!("GitHub API HTTP 403 for {path}: the token lacks the permission for this endpoint"),
            ),
            code => Page::Failed(format!("GitHub API HTTP {code} for {path}")),
        }
    }

    /// Read a list endpoint page by page. `items_of` extracts the array from a page, `parse` one entry.
    fn list<T>(
        &self,
        path: &str,
        budget: &Budget,
        items_of: impl Fn(&Value) -> Option<&Vec<Value>>,
        parse: impl Fn(&Value, &Budget) -> Option<T>,
    ) -> Read<T> {
        let per_page = budget.per_page.clamp(1, 100);
        let start = Instant::now();
        let mut out: Vec<T> = Vec::new();
        let mut incomplete: Option<String> = None;
        let mut pages = 0;
        'pages: for page in 1.. {
            if pages >= budget.max_pages {
                incomplete = Some(format!("page budget of {} reached", budget.max_pages));
                break;
            }
            if start.elapsed() >= budget.time {
                incomplete = Some("time budget reached".into());
                break;
            }
            let sep = if path.contains('?') { '&' } else { '?' };
            let url = format!("{}{path}{sep}per_page={per_page}&page={page}", self.base);
            // each request gets what is left of the list's time budget
            let left = budget.time.saturating_sub(start.elapsed());
            let v = match self.page(
                &url,
                left.clamp(Duration::from_millis(1), Duration::from_secs(30)),
            ) {
                Page::Json(v) => v,
                other if pages == 0 => {
                    return match other {
                        Page::Denied(kind, status, message) => Read::Denied {
                            kind,
                            status,
                            message,
                        },
                        Page::NotFound => Read::NotFound,
                        Page::Failed(m) => Read::Failed(m),
                        Page::Json(_) => unreachable!(),
                    }
                }
                Page::Denied(_, _, m) | Page::Failed(m) => {
                    incomplete = Some(format!("page {page}: {m}"));
                    break;
                }
                Page::NotFound => {
                    incomplete = Some(format!("page {page}: not found"));
                    break;
                }
            };
            let Some(items) = items_of(&v) else {
                let m = format!("unexpected response shape on page {page}");
                if pages == 0 {
                    return Read::Failed(m);
                }
                incomplete = Some(m);
                break;
            };
            pages += 1;
            let n = items.len();
            for item in items {
                if out.len() >= budget.max_items {
                    incomplete = Some(format!("item budget of {} reached", budget.max_items));
                    break 'pages;
                }
                match parse(item, budget) {
                    Some(t) => out.push(t),
                    None => incomplete = Some("a malformed entry was skipped".into()),
                }
            }
            if n < per_page {
                break;
            }
        }
        Read::Observed(Observed {
            items: out,
            pages,
            incomplete,
        })
    }

    pub(super) fn check_slug(slug: &str) -> Result<()> {
        if valid_slug(slug) {
            Ok(())
        } else {
            Err(Error::usage(format!("not a repository slug: {slug:?}")))
        }
    }

    fn check_sha(sha: &str) -> Result<()> {
        if matches!(sha.len(), 40 | 64)
            && sha.bytes().all(|b| matches!(b, b'0'..=b'9' | b'a'..=b'f'))
        {
            Ok(())
        } else {
            Err(Error::usage("not a full lower-case commit id"))
        }
    }

    /// Conversation comments of a pull request (the issue comments endpoint), oldest first.
    pub fn issue_comments(
        &self,
        slug: &str,
        number: u64,
        budget: &Budget,
    ) -> Result<Read<Comment>> {
        Self::check_slug(slug)?;
        Ok(self.list(
            &format!("/repos/{slug}/issues/{number}/comments"),
            budget,
            Value::as_array,
            parse_comment,
        ))
    }

    /// Reviews of a pull request, oldest first.
    pub fn pr_reviews(&self, slug: &str, number: u64, budget: &Budget) -> Result<Read<Review>> {
        Self::check_slug(slug)?;
        Ok(self.list(
            &format!("/repos/{slug}/pulls/{number}/reviews"),
            budget,
            Value::as_array,
            parse_review,
        ))
    }

    /// Check runs of one commit. A token without the Checks permission gives `Read::Denied`.
    pub fn check_runs(&self, slug: &str, sha: &str, budget: &Budget) -> Result<Read<CheckRun>> {
        Self::check_slug(slug)?;
        Self::check_sha(sha)?;
        Ok(self.list(
            &format!("/repos/{slug}/commits/{sha}/check-runs"),
            budget,
            |v| {
                let total = &v["total_count"];
                if !total.is_null() && total.as_i64().is_none() {
                    return None;
                }
                v["check_runs"].as_array()
            },
            parse_check_run,
        ))
    }

    /// Commit statuses of one commit, newest first.
    pub fn commit_statuses(
        &self,
        slug: &str,
        sha: &str,
        budget: &Budget,
    ) -> Result<Read<CommitStatus>> {
        Self::check_slug(slug)?;
        Self::check_sha(sha)?;
        Ok(self.list(
            &format!("/repos/{slug}/commits/{sha}/statuses"),
            budget,
            Value::as_array,
            parse_status,
        ))
    }

    /// Actions runs of one commit (`actions/runs?head_sha=`), newest first as GitHub sends them. The fallback of the
    /// default branch CI when a token cannot read check runs: it needs `Actions: read`. Read-only.
    pub fn workflow_runs(
        &self,
        slug: &str,
        sha: &str,
        budget: &Budget,
    ) -> Result<Read<WorkflowRun>> {
        Self::check_slug(slug)?;
        Self::check_sha(sha)?;
        Ok(self.list(
            &format!("/repos/{slug}/actions/runs?head_sha={sha}"),
            budget,
            |v| {
                let total = &v["total_count"];
                if !total.is_null() && total.as_i64().is_none() {
                    return None;
                }
                v["workflow_runs"].as_array()
            },
            parse_workflow_run,
        ))
    }

    /// The default branch of a repository and the commit its head points to: `GET /repos/{slug}` for the name,
    /// `GET /repos/{slug}/branches/{name}` for the head. The name is put into a path only when it has no character
    /// outside `A-Z a-z 0-9 . _ - /` (and no `..`); anything else is an error, not an escaped request.
    pub fn default_branch_head(&self, slug: &str) -> Result<(String, String)> {
        Self::check_slug(slug)?;
        let api = &self.base;
        let repo = self.get(&format!("{api}/repos/{slug}"))?;
        let branch = repo["default_branch"]
            .as_str()
            .filter(|b| plain_branch_name(b))
            .ok_or_else(|| Error::github("the repository answer has no usable default branch"))?
            .to_string();
        let doc = self.get(&format!("{api}/repos/{slug}/branches/{branch}"))?;
        let sha = hex_id(&doc["commit"]["sha"])
            .ok_or_else(|| Error::github("the branch answer has no usable head commit"))?;
        Ok((branch, sha))
    }
}

/// A branch name that can go into a URL path as it is.
fn plain_branch_name(b: &str) -> bool {
    !b.is_empty()
        && b.len() <= 200
        && !b.starts_with('/')
        && !b.ends_with('/')
        && !b.contains("..")
        && !b.contains("//")
        && b.bytes()
            .all(|c| c.is_ascii_alphanumeric() || matches!(c, b'.' | b'_' | b'-' | b'/'))
}

// ---- branch rules and repository merge settings --------------------------------------------------

/// Type of an active branch rule (`rules/branches`); unknown types keep their escaped name.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum RuleKind {
    RequiredStatusChecks,
    PullRequest,
    RequiredLinearHistory,
    NonFastForward,
    Deletion,
    Creation,
    Update,
    RequiredSignatures,
    Other(String),
}

impl RuleKind {
    fn parse(raw: &str) -> RuleKind {
        match raw {
            "required_status_checks" => RuleKind::RequiredStatusChecks,
            "pull_request" => RuleKind::PullRequest,
            "required_linear_history" => RuleKind::RequiredLinearHistory,
            "non_fast_forward" => RuleKind::NonFastForward,
            "deletion" => RuleKind::Deletion,
            "creation" => RuleKind::Creation,
            "update" => RuleKind::Update,
            "required_signatures" => RuleKind::RequiredSignatures,
            other => RuleKind::Other(escape_untrusted(other, 100).0),
        }
    }
}

/// One required status check of a `required_status_checks` rule.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct RequiredCheck {
    pub context: String,
    /// The app that must report it; None when the rule accepts any source.
    pub integration_id: Option<u64>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct BranchRule {
    pub kind: RuleKind,
    /// Only for `RequiredStatusChecks`.
    pub checks: Vec<RequiredCheck>,
    /// `strict_required_status_checks_policy` of a `RequiredStatusChecks` rule.
    pub strict: Option<bool>,
    /// `required_approving_review_count` of a `PullRequest` rule.
    pub required_approvals: Option<u64>,
    /// Repository or organization the ruleset belongs to (escaped).
    pub source: Option<String>,
    pub ruleset_id: Option<u64>,
    /// False when a required-checks rule came without readable parameters: the list is then partial.
    pub parameters_readable: bool,
}

fn parse_rule(v: &Value, _b: &Budget) -> Option<BranchRule> {
    let kind = RuleKind::parse(v["type"].as_str()?);
    let p = &v["parameters"];
    let mut rule = BranchRule {
        kind,
        checks: Vec::new(),
        strict: None,
        required_approvals: None,
        source: esc_opt(&v["ruleset_source"], 200),
        ruleset_id: v["ruleset_id"].as_u64(),
        parameters_readable: true,
    };
    match rule.kind {
        RuleKind::RequiredStatusChecks => match p["required_status_checks"].as_array() {
            Some(list) => {
                rule.strict = p["strict_required_status_checks_policy"].as_bool();
                for c in list {
                    match c["context"].as_str() {
                        Some(ctx) => rule.checks.push(RequiredCheck {
                            context: escape_untrusted(ctx, 200).0,
                            integration_id: c["integration_id"].as_u64(),
                        }),
                        None => rule.parameters_readable = false,
                    }
                }
            }
            None => rule.parameters_readable = false,
        },
        RuleKind::PullRequest => {
            rule.required_approvals = p["required_approving_review_count"].as_u64();
        }
        _ => {}
    }
    Some(rule)
}

/// A repository setting that may be hidden from the token: absent or non-boolean is `Unknown`, never `Off`.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Setting {
    On,
    Off,
    Unknown,
}

impl Setting {
    fn of(v: &Value) -> Setting {
        match v.as_bool() {
            Some(true) => Setting::On,
            Some(false) => Setting::Off,
            None => Setting::Unknown,
        }
    }
}

/// Merge-related fields of the repository object. The `allow_*` fields and `delete_branch_on_merge` are only
/// sent to tokens with admin rights.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct RepoSettings {
    pub default_branch: Option<String>,
    pub delete_branch_on_merge: Setting,
    pub allow_rebase_merge: Setting,
    pub allow_squash_merge: Setting,
    pub allow_merge_commit: Setting,
}

/// Outcome of a single-object read.
#[derive(Clone, Debug)]
pub enum ReadOne<T> {
    Value(T),
    Denied {
        kind: Denial,
        status: u16,
        message: String,
    },
    NotFound,
    Failed(String),
}

fn valid_branch(b: &str) -> bool {
    !b.is_empty()
        && b.len() <= 255
        && !b.starts_with(['-', '/'])
        && !b.ends_with(['/', '.'])
        && !b.ends_with(".lock")
        && b != "@"
        && !b.contains("..")
        && !b.contains("//")
        && !b.contains("@{")
        && !b.contains("/.")
        && !b.chars().any(|c| {
            c.is_control()
                || matches!(
                    c,
                    ' ' | '~' | '^' | ':' | '?' | '*' | '[' | '\\' | '%' | '#'
                )
        })
}

/// Percent-encode everything except unreserved characters (a `/` becomes `%2F`).
fn encode_segment(s: &str) -> String {
    let mut out = String::new();
    for b in s.bytes() {
        if b.is_ascii_alphanumeric() || matches!(b, b'-' | b'.' | b'_' | b'~') {
            out.push(b as char);
        } else {
            out.push_str(&format!("%{b:02X}"));
        }
    }
    out
}

impl Client {
    fn check_owner_repo(owner: &str, repo: &str) -> Result<String> {
        let slug = format!("{owner}/{repo}");
        if !owner.contains('/') && !repo.contains('/') && valid_slug(&slug) {
            Ok(slug)
        } else {
            Err(Error::usage(format!("not a repository: {slug:?}")))
        }
    }

    /// Active rules that apply to a branch (rulesets; readable with read access). Classic branch protection is
    /// not part of this answer and needs admin rights: an empty list does not prove the absence of it.
    /// A required-checks rule without readable parameters makes the list partial.
    pub fn branch_rules(
        &self,
        owner: &str,
        repo: &str,
        branch: &str,
        budget: &Budget,
    ) -> Result<Read<BranchRule>> {
        let slug = Self::check_owner_repo(owner, repo)?;
        if !valid_branch(branch) {
            return Err(Error::usage(format!(
                "not a usable branch name: {branch:?}"
            )));
        }
        let mut read = self.list(
            &format!("/repos/{slug}/rules/branches/{}", encode_segment(branch)),
            budget,
            Value::as_array,
            parse_rule,
        );
        if let Read::Observed(o) = &mut read {
            if o.incomplete.is_none() && o.items.iter().any(|r| !r.parameters_readable) {
                o.incomplete = Some("a required-checks rule had unreadable parameters".into());
            }
        }
        Ok(read)
    }

    /// `default_branch` and the merge-method settings of a repository (the latter `Unknown` without admin rights).
    pub fn repo_settings(&self, owner: &str, repo: &str) -> Result<ReadOne<RepoSettings>> {
        let slug = Self::check_owner_repo(owner, repo)?;
        Ok(
            match self.page(
                &format!("{}/repos/{slug}", self.base),
                Duration::from_secs(30),
            ) {
                Page::Json(v) if v["default_branch"].is_string() => ReadOne::Value(RepoSettings {
                    default_branch: esc_opt(&v["default_branch"], 255),
                    delete_branch_on_merge: Setting::of(&v["delete_branch_on_merge"]),
                    allow_rebase_merge: Setting::of(&v["allow_rebase_merge"]),
                    allow_squash_merge: Setting::of(&v["allow_squash_merge"]),
                    allow_merge_commit: Setting::of(&v["allow_merge_commit"]),
                }),
                Page::Json(_) => {
                    ReadOne::Failed("unexpected response shape: not a repository".into())
                }
                Page::Denied(kind, status, message) => ReadOne::Denied {
                    kind,
                    status,
                    message,
                },
                Page::NotFound => ReadOne::NotFound,
                Page::Failed(m) => ReadOne::Failed(m),
            },
        )
    }
}

/// An open pull request's branch, for finding a stack parent.
#[derive(Clone, Debug)]
pub struct ReadyParent {
    pub number: u64,
    pub head_ref: String,
}
impl Client {
    pub fn ready_parents(&self, slug: &str, budget: &Budget) -> Result<Read<ReadyParent>> {
        Self::check_slug(slug)?;
        Ok(self.list(
            &format!("/repos/{slug}/pulls"),
            budget,
            Value::as_array,
            |v, _| {
                let number = v["number"].as_u64().filter(|n| *n > 0)?;
                let head_ref = v["head"]["ref"].as_str().filter(|s| !s.is_empty())?;
                Some(ReadyParent {
                    number,
                    head_ref: head_ref.into(),
                })
            },
        ))
    }
}
