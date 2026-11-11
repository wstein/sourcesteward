//! `stw lint pr` (M8): reads one pull request (GET only), applies the rule table of `lint_pr` and prints the
//! findings. Writes nothing to the forge or to disk. Port of `internal/lintprcmd` of the Go implementation.
//!
//! Exit codes: 0 no finding of severity error (warnings do not fail); 3 at least one error finding or an
//! incomplete read (a list cut at its bound; the document is printed and is not a pass); 1 a failed read or no
//! token (no document); 2 usage error.
use std::path::Path;

use serde_json::{json, Value};

use crate::error::{Error, Result};
use crate::github::lint_reads::{LintPage, COMMENTS_MAX_PAGE, COMMITS_MAX_PAGE};
use crate::github::reads::escape_untrusted;
use crate::github::GithubState;
use crate::lint_pr::{self, re_space, Comment, Commit, Finding, Identity, Input, Severity};
use crate::repo;

/// Where the owner identities of a run come from.
pub const OWNER_FLAG: &str = "flag";
pub const OWNER_GIT_CONFIG: &str = "git-config";

/// Exit status of a run with a finding of severity error or an incomplete read.
pub const EXIT_FINDINGS: u8 = 3;

/// The parsed command line.
pub struct Flags {
    pub pr: String,
    pub repo: Option<String>,
    pub path: Option<String>,
    pub owners: Vec<String>,
    pub human: bool,
    pub json: bool,
}

/// A validated run.
pub struct Params {
    pub repo: String,
    pub pr: u64,
    pub owners: Vec<Identity>,
    pub owner_source: &'static str,
    pub json: bool,
}

/// `\A[A-Za-z0-9_-][A-Za-z0-9_.-]*/[A-Za-z0-9_-][A-Za-z0-9_.-]*\z`
pub fn valid_repo(s: &str) -> bool {
    let part = |p: &str| {
        let mut c = p.chars();
        c.next()
            .is_some_and(|f| f.is_ascii_alphanumeric() || f == '_' || f == '-')
            && c.all(|x| x.is_ascii_alphanumeric() || matches!(x, '_' | '.' | '-'))
    };
    matches!(s.split_once('/'), Some((o, r)) if part(o) && part(r) && !r.contains('/'))
}

/// `\A\s*([^<>\s](?:[^<>]*[^<>\s])?)\s*<([^<>\s@]+@[^<>\s@]+)>\s*\z`: "Name <email>".
pub fn parse_identity(raw: &str) -> Option<Identity> {
    let t = raw.trim_matches(re_space);
    let t = t.strip_suffix('>')?;
    let (name, addr) = t.split_once('<')?;
    let name = name.trim_matches(re_space);
    if name.is_empty() || name.contains('>') {
        return None;
    }
    if addr.chars().any(|c| c == '<' || c == '>' || re_space(c)) {
        return None;
    }
    let (local, domain) = addr.split_once('@')?;
    if local.is_empty() || domain.is_empty() || domain.contains('@') {
        return None;
    }
    Some(Identity {
        name: name.to_string(),
        email: addr.to_string(),
    })
}

/// `user.name` and `user.email` of the Git configuration seen from `dir`, read only; None unless both are set.
pub fn git_identity(dir: &Path) -> Option<Identity> {
    let get = |key: &str| -> Option<String> {
        let v = match repo::top_level(dir).and_then(|top| repo::open(&top).ok()) {
            Some(r) => r.config_snapshot().string(key).map(|v| v.to_string()),
            None => gix::config::File::from_globals()
                .ok()?
                .string(key)
                .map(|v| v.to_string()),
        };
        v.map(|s| s.trim().to_string()).filter(|s| !s.is_empty())
    };
    Some(Identity {
        name: get("user.name")?,
        email: get("user.email")?,
    })
}

/// The single GitHub repository of the workspace around `path` (default: the current directory).
fn workspace_repo(path: Option<&str>) -> Result<(String, std::path::PathBuf)> {
    let start = match path {
        Some(p) => std::path::PathBuf::from(p),
        None => std::env::current_dir()
            .map_err(|e| Error::usage(format!("no current directory: {e}")))?,
    };
    let root = repo::add_root(&start).ok_or_else(|| {
        Error::usage("no workspace root: pass PATH, run inside the workspace, set STW_ROOT, or name the repository with --repo OWNER/NAME")
    })?;
    let root = std::fs::canonicalize(&root).unwrap_or(root);
    let mut slugs: Vec<String> = Vec::new();
    for r in repo::list_repos(&root) {
        if let Some(s) = repo::open_isolated(&r.path)
            .ok()
            .and_then(|g| repo::slug(&g))
        {
            if !slugs.contains(&s) {
                slugs.push(s);
            }
        }
    }
    if slugs.len() != 1 {
        return Err(Error::usage(format!(
            "the workspace has {} GitHub repositories; name the one with --repo OWNER/NAME",
            slugs.len()
        )));
    }
    Ok((slugs.remove(0), root))
}

/// Validates the flags. Every error is a usage error (exit 2): nothing has been read from the forge.
pub fn prepare(f: &Flags) -> Result<Params> {
    let pr: u64 = match f.pr.parse::<i64>() {
        Err(_) => {
            return Err(Error::usage(format!(
                "the pull request {:?} is not a number",
                f.pr
            )))
        }
        Ok(n) if n < 1 => {
            return Err(Error::usage(format!(
                "the pull request number {n} is not a positive number"
            )))
        }
        Ok(n) => n as u64,
    };
    if f.human && f.json {
        return Err(Error::usage("--human and --json are mutually exclusive"));
    }
    if let Some(r) = &f.repo {
        if !valid_repo(r) {
            return Err(Error::usage(format!("--repo {r:?} is not OWNER/NAME")));
        }
    }
    if f.path.is_some() && f.repo.is_some() {
        return Err(Error::usage("PATH and --repo are mutually exclusive"));
    }
    let (repo, root) = match &f.repo {
        Some(r) => (r.clone(), None),
        None => {
            let (r, root) = workspace_repo(f.path.as_deref())?;
            (r, Some(root))
        }
    };
    let mut owners = Vec::new();
    for raw in &f.owners {
        owners.push(parse_identity(raw).ok_or_else(|| {
            Error::usage(format!("--owner-identity {raw:?} is not \"Name <email>\""))
        })?);
    }
    let mut owner_source = OWNER_FLAG;
    if owners.is_empty() {
        let dir = root.unwrap_or_else(|| std::path::PathBuf::from("."));
        let id = git_identity(&dir).ok_or_else(|| {
            Error::usage(format!(
                "no owner identity: pass --owner-identity \"Name <email>\" or set user.name and user.email in the Git configuration of {}",
                dir.display()
            ))
        })?;
        owners = vec![id];
        owner_source = OWNER_GIT_CONFIG;
    }
    Ok(Params {
        repo,
        pr,
        owners,
        owner_source,
        json: f.json,
    })
}

fn shape() -> Error {
    Error::github("malformed-response: unexpected document shape")
}

fn sha40(s: &str) -> bool {
    s.len() == 40 && s.bytes().all(|b| matches!(b, b'0'..=b'9' | b'a'..=b'f'))
}

/// A string field that may be absent or null (then empty); any other type is a shape error.
fn opt_str(v: &Value) -> Result<String> {
    match v {
        Value::Null => Ok(String::new()),
        Value::String(s) => Ok(s.clone()),
        _ => Err(shape()),
    }
}

/// Title, text, head SHA and commit count of `pulls/N` (-1 when absent). A null text is empty.
fn decode_detail(v: &Value) -> Result<(String, String, String, i64)> {
    let o = v.as_object().ok_or_else(shape)?;
    let get = |k: &str| o.get(k).unwrap_or(&Value::Null);
    let title = opt_str(get("title"))?;
    let body = opt_str(get("body"))?;
    let commits = match get("commits") {
        Value::Null => -1,
        n => match n.as_i64() {
            Some(c) if c >= 0 => c,
            Some(_) => -1,
            None => return Err(shape()),
        },
    };
    let head = match get("head") {
        Value::Null => String::new(),
        h => opt_str(
            h.as_object()
                .ok_or_else(shape)?
                .get("sha")
                .unwrap_or(&Value::Null),
        )?,
    };
    if !sha40(&head) {
        return Err(shape());
    }
    Ok((title, body, head, commits))
}

fn person(v: &Value) -> Result<(String, String)> {
    match v {
        Value::Null => Ok((String::new(), String::new())),
        Value::Object(o) => Ok((
            opt_str(o.get("name").unwrap_or(&Value::Null))?,
            opt_str(o.get("email").unwrap_or(&Value::Null))?,
        )),
        _ => Err(shape()),
    }
}

/// One page of `pulls/N/commits`.
fn decode_commits(v: &Value) -> Result<Vec<Commit>> {
    let items = match v {
        Value::Null => return Ok(Vec::new()),
        Value::Array(a) => a,
        _ => return Err(shape()),
    };
    let mut out = Vec::new();
    for it in items {
        let o = it.as_object().ok_or_else(shape)?;
        let sha = opt_str(o.get("sha").unwrap_or(&Value::Null))?;
        if !sha40(&sha) {
            return Err(shape());
        }
        let commit = match o.get("commit").unwrap_or(&Value::Null) {
            Value::Null => &serde_json::Map::new(),
            Value::Object(c) => c,
            _ => return Err(shape()),
        };
        let (author_name, author_email) = person(commit.get("author").unwrap_or(&Value::Null))?;
        let (committer_name, committer_email) =
            person(commit.get("committer").unwrap_or(&Value::Null))?;
        out.push(Commit {
            sha,
            message: opt_str(commit.get("message").unwrap_or(&Value::Null))?,
            author_name,
            author_email,
            committer_name,
            committer_email,
        });
    }
    Ok(out)
}

/// One page of `issues/N/comments`. The ID is required; a null text is empty.
fn decode_comments(v: &Value) -> Result<Vec<Comment>> {
    let items = match v {
        Value::Null => return Ok(Vec::new()),
        Value::Array(a) => a,
        _ => return Err(shape()),
    };
    let mut out = Vec::new();
    for it in items {
        let o = it.as_object().ok_or_else(shape)?;
        let id = o.get("id").and_then(Value::as_i64).unwrap_or(0);
        if id < 1 {
            return Err(shape());
        }
        out.push(Comment {
            id,
            body: opt_str(o.get("body").unwrap_or(&Value::Null))?,
        });
    }
    Ok(out)
}

/// The `rel` tokens (lower case) of every entry of a `Link` header.
fn link_rels(entry: &str) -> Vec<String> {
    let Some(end) = entry.find('>') else {
        return Vec::new();
    };
    let mut rels = Vec::new();
    for param in entry[end + 1..].split(';') {
        if let Some((k, v)) = param.trim().split_once('=') {
            if k.trim().eq_ignore_ascii_case("rel") {
                rels.extend(
                    v.trim()
                        .trim_matches('"')
                        .to_lowercase()
                        .split_whitespace()
                        .map(str::to_string),
                );
            }
        }
    }
    rels
}

/// Whether the `Link` header announces the page after `page`. The URL in the header is never requested.
/// A `rel="next"` without a usable page number, or one that is not the following page, is an error.
fn has_more(link: &str, page: usize) -> Result<bool> {
    let mut next: Option<usize> = None;
    let mut any_next = false;
    for entry in link.split(',') {
        let entry = entry.trim();
        let Some(inner) = entry.strip_prefix('<') else {
            continue;
        };
        if !link_rels(entry).iter().any(|r| r == "next") {
            continue;
        }
        any_next = true;
        let Some(end) = inner.find('>') else {
            continue;
        };
        let url = &inner[..end];
        let query = url.split_once('?').map(|(_, q)| q).unwrap_or("");
        let query = query.split('#').next().unwrap_or("");
        next = query
            .split('&')
            .find_map(|kv| kv.strip_prefix("page="))
            .and_then(|v| v.strip_prefix('+').unwrap_or(v).parse::<i64>().ok())
            .filter(|p| (1..=1_000_000).contains(p))
            .map(|p| p as usize);
        // like Go: the first rel=next entry decides
        break;
    }
    match next {
        Some(n) if n == page + 1 => Ok(true),
        Some(_) => Err(Error::github(
            "malformed-response: Link next is not the following page",
        )),
        None if any_next => Err(Error::github("malformed-response: unusable Link header")),
        None => Ok(false),
    }
}

/// What was read: the input of the rules, the head SHA and the bounds hit.
pub struct Read {
    pub input: Input,
    pub head: String,
    pub truncated_commits: bool,
    pub truncated_comments: bool,
}

/// A page source: the production one is the GitHub client; the seam keeps the paging logic testable.
pub trait Source {
    fn pull(&self, slug: &str, n: u64) -> Result<LintPage>;
    fn commits(&self, slug: &str, n: u64, page: usize) -> Result<LintPage>;
    fn comments(&self, slug: &str, n: u64, page: usize) -> Result<LintPage>;
}

impl Source for crate::github::Client {
    fn pull(&self, slug: &str, n: u64) -> Result<LintPage> {
        self.lint_pull(slug, n)
    }
    fn commits(&self, slug: &str, n: u64, page: usize) -> Result<LintPage> {
        self.lint_commits(slug, n, page)
    }
    fn comments(&self, slug: &str, n: u64, page: usize) -> Result<LintPage> {
        self.lint_comments(slug, n, page)
    }
}

/// Gathers the input: the pull request, its commits (pages 1..=3) and its comments (pages 1..=10).
pub fn read(src: &dyn Source, repo: &str, pr: u64) -> Result<Read> {
    let mut input = Input {
        pr,
        ..Input::default()
    };
    let res = src.pull(repo, pr)?;
    let (title, body, head, commit_count) = decode_detail(&res.body)?;
    input.title = title;
    input.body = body;
    let (mut tr_commits, mut tr_comments) = (false, false);
    for page in 1..=COMMITS_MAX_PAGE {
        let res = src.commits(repo, pr, page)?;
        input.commits.extend(decode_commits(&res.body)?);
        if !has_more(&res.link, page)? {
            break;
        }
        if page == COMMITS_MAX_PAGE {
            tr_commits = true;
        }
    }
    // The commits endpoint lists at most 250 commits whatever the page bound: a count above the one the
    // pull request reports means commits were not read.
    if commit_count > input.commits.len() as i64 {
        tr_commits = true;
    }
    for page in 1..=COMMENTS_MAX_PAGE {
        let res = src.comments(repo, pr, page)?;
        input.comments.extend(decode_comments(&res.body)?);
        if !has_more(&res.link, page)? {
            break;
        }
        if page == COMMENTS_MAX_PAGE {
            tr_comments = true;
        }
    }
    Ok(Read {
        input,
        head,
        truncated_commits: tr_commits,
        truncated_comments: tr_comments,
    })
}

/// `YYYY-MM-DDTHH:MM:SSZ` of seconds since the epoch (UTC).
pub fn rfc3339_utc(secs: i64) -> String {
    let days = secs.div_euclid(86_400);
    let rem = secs.rem_euclid(86_400);
    // civil from days (Howard Hinnant)
    let z = days + 719_468;
    let era = z.div_euclid(146_097);
    let doe = z.rem_euclid(146_097);
    let yoe = (doe - doe / 1460 + doe / 36_524 - doe / 146_096) / 365;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let m = if mp < 10 { mp + 3 } else { mp - 9 };
    let y = yoe + era * 400 + i64::from(m <= 2);
    format!(
        "{y:04}-{m:02}-{d:02}T{:02}:{:02}:{:02}Z",
        rem / 3600,
        rem % 3600 / 60,
        rem % 60
    )
}

/// The result of a run: counts, the findings and what was cut.
pub struct Report {
    pub repo: String,
    pub pr: u64,
    pub head: String,
    pub owners: Vec<Identity>,
    pub owner_source: &'static str,
    pub commits: usize,
    pub comments: usize,
    pub findings: Vec<Finding>,
    pub truncated_commits: bool,
    pub truncated_comments: bool,
}

impl Report {
    pub fn errors(&self) -> usize {
        self.count(Severity::Error)
    }
    pub fn warnings(&self) -> usize {
        self.count(Severity::Warning)
    }
    fn count(&self, s: Severity) -> usize {
        self.findings.iter().filter(|f| f.severity == s).count()
    }
    pub fn passed(&self) -> bool {
        self.errors() == 0 && !self.truncated_commits && !self.truncated_comments
    }

    /// The one JSON document (schema 1 of the Go command; keys are never omitted).
    pub fn to_json(&self, now_secs: i64, host: &str) -> Value {
        let findings: Vec<Value> = self
            .findings
            .iter()
            .map(|f| {
                json!({
                    "rule": f.rule,
                    "severity": f.severity.as_str(),
                    "location": {
                        "kind": f.location.kind,
                        "id": if f.location.id.is_empty() { Value::Null } else { Value::String(f.location.id.clone()) },
                        "line": if f.location.line > 0 { json!(f.location.line) } else { Value::Null },
                    },
                    "message": f.message,
                })
            })
            .collect();
        let rules: Vec<Value> = lint_pr::RULES
            .iter()
            .map(|r| {
                json!({"id": r.id, "severity": r.severity.as_str(), "summary": r.summary, "source": r.source})
            })
            .collect();
        json!({
            "schema_version": 1,
            "command": "lint pr",
            "time": rfc3339_utc(now_secs),
            "host": host,
            "repo": self.repo,
            "pull_request": self.pr,
            "head": self.head,
            "options": {
                "owner_identities": self.owners.iter().map(|o| format!("{} <{}>", o.name, o.email)).collect::<Vec<_>>(),
                "owner_source": self.owner_source,
            },
            "rules": rules,
            "findings": findings,
            "summary": {
                "commits": self.commits,
                "comments": self.comments,
                "errors": self.errors(),
                "warnings": self.warnings(),
                "passed": self.passed(),
            },
            "truncated": {"commits": self.truncated_commits, "comments": self.truncated_comments},
        })
    }

    /// The human list. Text of the forge (names, emails in messages) is escaped and cut.
    pub fn render_human(&self) -> String {
        let mut b = String::new();
        let verdict = if self.passed() {
            "pass"
        } else if self.errors() == 0 {
            "FAIL (incomplete)"
        } else {
            "FAIL"
        };
        b.push_str(&format!(
            "{}#{} head {}: {verdict}, {} errors, {} warnings ({} commits, {} comments read)\n",
            self.repo,
            self.pr,
            &self.head[..12],
            self.errors(),
            self.warnings(),
            self.commits,
            self.comments
        ));
        for f in &self.findings {
            let mut place = f.location.kind.to_string();
            if !f.location.id.is_empty() {
                let id: String = f.location.id.chars().take(12).collect();
                place.push(' ');
                place.push_str(&id);
            }
            if f.location.line > 0 {
                place.push_str(&format!(":{}", f.location.line));
            }
            let (msg, cut) = escape_untrusted(&f.message, 120);
            b.push_str(&format!(
                "{:<7} {:<22} {place}: {msg}{}\n",
                f.severity.as_str(),
                f.rule,
                if cut { "..." } else { "" }
            ));
        }
        if self.truncated_commits {
            b.push_str("note: not all commits were read (the forge lists at most 250, the command reads at most 300)\n");
        }
        if self.truncated_comments {
            b.push_str("note: not all comments were read (the command reads at most 300)\n");
        }
        b
    }
}

/// "1 error", "2 errors".
fn plural(n: usize, one: &str, many: &str) -> String {
    if n == 1 {
        format!("1 {one}")
    } else {
        format!("{n} {many}")
    }
}

/// What a run prints and the exit status it ends with.
pub struct Outcome {
    pub stdout: String,
    pub stderr: Option<String>,
    pub code: u8,
}

/// Reads, lints and renders. `Err` is a failed read (nothing is printed, exit 1); otherwise the document is
/// there and the code is 0 (no error finding) or 3 (an error finding or an incomplete read).
pub fn execute(src: &dyn Source, p: &Params, now_secs: i64) -> Result<Outcome> {
    let r = read(src, &p.repo, p.pr)?;
    let findings = lint_pr::lint(&r.input, &p.owners);
    let report = Report {
        repo: p.repo.clone(),
        pr: p.pr,
        head: r.head,
        owners: p.owners.clone(),
        owner_source: p.owner_source,
        commits: r.input.commits.len(),
        comments: r.input.comments.len(),
        findings,
        truncated_commits: r.truncated_commits,
        truncated_comments: r.truncated_comments,
    };
    let stdout = if p.json {
        let mut text = report.to_json(now_secs, "github.com").to_string();
        text.push('\n');
        text
    } else {
        report.render_human()
    };
    let (stderr, code) = if report.errors() == 0 && !report.passed() {
        let what = match (report.truncated_commits, report.truncated_comments) {
            (true, true) => "commits and comments were",
            (true, false) => "commits were",
            _ => "comments were",
        };
        (
            Some(format!("incomplete read: {what} not read in full (see truncated); the result is not a pass")),
            EXIT_FINDINGS,
        )
    } else if report.errors() > 0 {
        (
            Some(format!(
                "{}, {}",
                plural(report.errors(), "error", "errors"),
                plural(report.warnings(), "warning", "warnings")
            )),
            EXIT_FINDINGS,
        )
    } else {
        (None, 0)
    };
    Ok(Outcome {
        stdout,
        stderr,
        code,
    })
}

/// Runs against GitHub and prints. Ok(0) or Ok(3) as `execute`; Err for a failed read or no token.
pub fn run(p: &Params) -> Result<u8> {
    let github = GithubState::discover(false);
    let Some(client) = github.client() else {
        return Err(Error::github(format!(
            "hosting-api-unavailable: {}",
            github.note.unwrap_or_default()
        )));
    };
    let out = execute(client, p, crate::tidy::now_secs())?;
    print!("{}", out.stdout);
    if let Some(e) = &out.stderr {
        eprintln!("stw: {e}");
    }
    Ok(out.code)
}
