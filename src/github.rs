//! GitHub access over the REST API (ureq, blocking). No `gh` binary. Tokens come from `auth`.
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Mutex;
use std::time::{Duration, Instant};

use serde_json::Value;

use crate::auth::{self, TokenSource};
use crate::error::{Error, Result};

pub mod lint_reads;
pub mod reads;

const API: &str = "https://api.github.com";
/// Environment variable that points the REST calls elsewhere (GitHub Enterprise, tests); no trailing slash.
pub const API_URL_ENV: &str = "STW_GITHUB_API_URL";

/// Why a read did not happen or was cut: the overall deadline of `--timeout` passed.
fn deadline_message(total: Duration) -> String {
    format!("deadline: the --timeout of {total:?} passed before the read was done; raise --timeout")
}

/// One open pull request as shown by `stw status`.
#[derive(Clone, Debug)]
pub struct OpenPr {
    pub number: u64,
    pub title: String,
    pub head: String,
    /// The commit id of the head (`head.sha`); empty when the answer had none.
    pub head_sha: String,
    /// The target branch (`base.ref`); empty when the answer had none.
    pub base: String,
    /// The default branch of the target repository (`base.repo.default_branch`); empty when the answer had none.
    pub default_branch: String,
    pub draft: bool,
    pub author: String,
    /// The author is an app/bot account (`user.type == "Bot"` or a `[bot]` login).
    pub bot: bool,
    /// The review state from the comments: None unless `stw status --review` read it (REST has no
    /// `reviewDecision`, so there is no other source).
    pub review: Option<crate::status_review::PrReview>,
    /// From `mergeable_state` of the single-PR endpoint, upper-cased like gh's `mergeStateStatus`.
    pub merge_state: Option<String>,
}

/// Open issues of one repository, from one page of the issues list.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct IssueCounts {
    pub open: usize,
    /// Open issues with a label named `bug` (case-insensitive).
    pub bug: usize,
    /// The page was full (`PAGE` entries, pull requests included): more issues may exist.
    pub truncated: bool,
}

/// Page size of the list reads of `stw status` (the REST maximum).
pub const PAGE: usize = 100;

/// One PR of any state, for the proofs of `stw tidy plan` (merged head, rebase and squash mapping).
#[derive(Clone, Debug, Default)]
pub struct AnyPr {
    pub head_ref: String,
    pub head_sha: String,
    pub number: u64,
    /// `OPEN`, `MERGED` or `CLOSED` (gh vocabulary).
    pub state: &'static str,
    /// `merge_commit_sha` of the pull list: the merge commit, the tip of the rewritten commits, or the test merge of
    /// a pull request that is not merged; empty when GitHub sent none.
    pub merge_commit_sha: String,
    /// `merged_at` in seconds since the epoch; `None` for a pull request that is not merged.
    pub merged_at: Option<i64>,
    pub title: String,
    /// `head.repo.full_name`: the repository the head branch lives in (a fork differs from the target repository);
    /// empty when GitHub sent none (the fork was deleted).
    pub head_repo: String,
}

/// The commits of a pull request, or the fact that there are too many to read in one request.
#[derive(Clone, Debug)]
pub enum PrCommitsRead {
    Commits(Vec<crate::tidy_rebase::PrCommit>),
    /// The answer was a full page (`PAGE` commits): the list may go on, it is not read.
    TooMany,
}

/// `YYYY-MM-DDTHH:MM:SSZ` (what GitHub sends) as seconds since the epoch; `None` for anything else.
pub fn parse_utc(s: &str) -> Option<i64> {
    let b = s.as_bytes();
    if b.len() != 20
        || b[4] != b'-'
        || b[7] != b'-'
        || b[10] != b'T'
        || b[13] != b':'
        || b[16] != b':'
        || b[19] != b'Z'
    {
        return None;
    }
    let num = |from: usize, to: usize| -> Option<i64> {
        let t = &s[from..to];
        t.bytes()
            .all(|c| c.is_ascii_digit())
            .then(|| t.parse().ok())?
    };
    let (y, m, d) = (num(0, 4)?, num(5, 7)?, num(8, 10)?);
    let (hh, mm, ss) = (num(11, 13)?, num(14, 16)?, num(17, 19)?);
    if !(1..=12).contains(&m) || hh > 23 || mm > 59 || ss > 60 {
        return None;
    }
    let leap = (y % 4 == 0 && y % 100 != 0) || y % 400 == 0;
    let dim = [
        31,
        if leap { 29 } else { 28 },
        31,
        30,
        31,
        30,
        31,
        31,
        30,
        31,
        30,
        31,
    ][m as usize - 1];
    if !(1..=dim).contains(&d) {
        return None;
    }
    // days from the civil date (Howard Hinnant's algorithm)
    let y2 = if m <= 2 { y - 1 } else { y };
    let era = y2.div_euclid(400);
    let yoe = y2 - era * 400;
    let doy = (153 * (if m > 2 { m - 3 } else { m + 9 }) + 2) / 5 + d - 1;
    let doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
    let days = era * 146_097 + doe - 719_468;
    Some(days * 86_400 + hh * 3600 + mm * 60 + ss)
}

/// Whether GitHub is available for this run, and the reason when it is not.
pub struct GithubState {
    pub client: Option<Client>,
    /// Why pull requests are not read (offline, no token); None when they are.
    pub note: Option<String>,
}

/// The note of a run with `--offline`.
pub const NOTE_OFFLINE: &str = "GitHub not read: --offline";
/// The start of the note of a run without a token.
pub const NOTE_NO_TOKEN: &str = "GitHub not read: no GitHub token found";

impl GithubState {
    /// The stable code of the note (`stw status --json`, `notices`): `offline`, `no-token` (the Go reasons) or
    /// `github-not-read` for any other note; None without a note.
    pub fn note_code(&self) -> Option<&'static str> {
        let n = self.note.as_deref()?;
        Some(if n == NOTE_OFFLINE {
            "offline"
        } else if n.starts_with(NOTE_NO_TOKEN) {
            "no-token"
        } else {
            "github-not-read"
        })
    }

    pub fn discover(offline: bool) -> GithubState {
        if offline {
            return GithubState {
                client: None,
                note: Some(NOTE_OFFLINE.into()),
            };
        }
        match Client::from_env() {
            Some(c) => GithubState {
                client: Some(c),
                note: None,
            },
            None => GithubState {
                client: None,
                note: Some(format!(
                    "{NOTE_NO_TOKEN} (looked at GH_TOKEN, GITHUB_TOKEN, \
                     ~/.config/gh/hosts.yml and the macOS keychain); set GH_TOKEN to a token that can read \
                     the repositories, or pass --offline"
                )),
            },
        }
    }

    pub fn client(&self) -> Option<&Client> {
        self.client.as_ref()
    }
}

/// One request takes at most this long, whatever the overall deadline.
const REQUEST_TIMEOUT: Duration = Duration::from_secs(30);

/// What GitHub said about the rate limit in its last answer that carried it (`X-RateLimit-*`).
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct RateInfo {
    pub limit: u64,
    pub remaining: u64,
    /// Unix time of the reset.
    pub reset: u64,
}

/// The requests of one source (`pulls`, `issues`) of one repository.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct SourceStats {
    /// `owner/name`, or empty for a request that names no repository.
    pub repo: String,
    pub source: &'static str,
    pub requests: usize,
    /// Sum of the durations of the requests (they run in parallel, so it can exceed the elapsed time).
    pub latency: Duration,
}

/// Diagnostic count of the requests of a run (`--stats`, `--verbose`), without any URL or token.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Stats {
    pub requests: usize,
    pub rate: Option<RateInfo>,
    /// Sorted by repository, then source.
    pub sources: Vec<SourceStats>,
}

/// `pulls`, `issues` or `ci-main` and the repository of a REST path `/repos/OWNER/NAME/<source>...`.
fn classify(path: &str) -> (String, &'static str) {
    let mut seg = path.trim_start_matches('/').split('/');
    if seg.next() != Some("repos") {
        return (String::new(), "other");
    }
    let (Some(owner), Some(name)) = (seg.next(), seg.next()) else {
        return (String::new(), "other");
    };
    let source = match seg.next().map(|s| s.split('?').next().unwrap_or("")) {
        Some("pulls") => "pulls",
        // the reads of the default branch CI (`stw status`, source `ci-main` as in Go): the repository document for
        // the default branch name, the branch, the check runs and statuses of a commit and the Actions runs
        None | Some("branches" | "commits" | "actions") => "ci-main",
        // the comments of a pull request (`stw status --review`) belong to the pulls source, as in Go
        Some("issues") => match (seg.next(), seg.next()) {
            (Some(n), Some(c))
                if !n.is_empty()
                    && n.bytes().all(|b| b.is_ascii_digit())
                    && c.split('?').next() == Some("comments") =>
            {
                "pulls"
            }
            _ => "issues",
        },
        _ => "other",
    };
    (format!("{owner}/{name}"), source)
}

/// The answer is a rate limit: 429, or 403 with an exhausted `X-RateLimit-Remaining`, a `Retry-After` header, or
/// (secondary limits can come without headers) "rate limit" or "abuse detection" in the first 4096 bytes of the body,
/// as in Go (`hosting/watch_reader.go`). Reads the body of a 403 without those headers.
fn is_rate_limit(resp: &mut ureq::http::Response<ureq::Body>) -> bool {
    let status = resp.status().as_u16();
    if status == 429 {
        return true;
    }
    if status != 403 {
        return false;
    }
    let headers = resp.headers();
    if headers
        .get("x-ratelimit-remaining")
        .and_then(|v| v.to_str().ok())
        .is_some_and(|v| v.trim() == "0")
        || headers.contains_key("retry-after")
    {
        return true;
    }
    mentions_rate_limit(&error_body_head(resp))
}

/// "rate limit" or "abuse detection" in the (lower-case) text of an error body.
pub(crate) fn mentions_rate_limit(text: &str) -> bool {
    text.contains("rate limit") || text.contains("abuse detection")
}

/// The first 4096 bytes of the body of an error answer, lower-cased; longer bodies are cut (not dropped), invalid
/// UTF-8 is replaced, a read error ends the text.
pub(crate) fn error_body_head(resp: &mut ureq::http::Response<ureq::Body>) -> String {
    use std::io::Read;
    let mut bytes = Vec::new();
    let _ = resp
        .body_mut()
        .as_reader()
        .take(4096)
        .read_to_end(&mut bytes);
    String::from_utf8_lossy(&bytes).to_ascii_lowercase()
}

/// The `X-RateLimit-*` headers of an answer, when all three are there.
fn rate_from(headers: &ureq::http::HeaderMap) -> Option<RateInfo> {
    let h = |k: &str| headers.get(k)?.to_str().ok()?.trim().parse::<u64>().ok();
    Some(RateInfo {
        limit: h("x-ratelimit-limit")?,
        remaining: h("x-ratelimit-remaining")?,
        reset: h("x-ratelimit-reset")?,
    })
}

pub struct Client {
    agent: ureq::Agent,
    token: String,
    base: String,
    pub source: TokenSource,
    /// The overall deadline of the reads (`stw status --timeout`): its end and its length (for the message).
    deadline: Mutex<Option<(Instant, Duration)>>,
    stats: Mutex<Stats>,
    /// GitHub answered with a rate limit (429, or 403 with an exhausted limit or `Retry-After`) once in this run.
    rate_limited: AtomicBool,
}

impl std::fmt::Debug for Client {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "Client {{ source: {:?}, token: <hidden> }}", self.source)
    }
}

impl Client {
    pub fn from_env() -> Option<Client> {
        let (token, source) = auth::find_token(&auth::AuthEnv::from_process())?;
        Some(Client::new(token, source))
    }

    pub fn new(token: String, source: TokenSource) -> Client {
        let base = std::env::var(API_URL_ENV)
            .ok()
            .filter(|v| !v.is_empty())
            .map(|v| v.trim_end_matches('/').to_string())
            .unwrap_or_else(|| API.to_string());
        Client::with_base(token, source, base)
    }

    /// A client for an explicit API base (no trailing slash): tests and GitHub Enterprise.
    pub fn with_base(token: String, source: TokenSource, base: String) -> Client {
        let agent: ureq::Agent = ureq::Agent::config_builder()
            .tls_config(crate::http_tls::config())
            .timeout_global(Some(Duration::from_secs(30)))
            .http_status_as_error(false)
            .user_agent(concat!("stw/", env!("CARGO_PKG_VERSION")))
            .build()
            .into();
        let base = base.trim_end_matches('/').to_string();
        Client {
            agent,
            token,
            base,
            source,
            deadline: Mutex::new(None),
            stats: Mutex::new(Stats::default()),
            rate_limited: AtomicBool::new(false),
        }
    }

    /// A rate limit answer was seen in this run: the reads that are optional (`stw status --review`) start no more
    /// requests (Go: the gate of `statusremote` stops every further request after the first rate-limit evidence).
    pub fn rate_limited(&self) -> bool {
        self.rate_limited.load(Ordering::SeqCst)
    }

    fn note_rate_limit(&self) {
        self.rate_limited.store(true, Ordering::SeqCst);
    }

    /// The requests sent so far.
    pub fn stats(&self) -> Stats {
        let mut s = self.stats.lock().unwrap().clone();
        s.sources
            .sort_by(|a, b| (&a.repo, a.source).cmp(&(&b.repo, b.source)));
        s
    }

    fn record(&self, path: &str, took: Duration, rate: Option<RateInfo>) {
        let (repo, source) = classify(path);
        let mut st = self.stats.lock().unwrap();
        st.requests += 1;
        // the smallest remaining count is the freshest evidence
        if let Some(r) = rate {
            if st.rate.is_none_or(|old| r.remaining <= old.remaining) {
                st.rate = Some(r);
            }
        }
        match st
            .sources
            .iter_mut()
            .find(|x| x.repo == repo && x.source == source)
        {
            Some(x) => {
                x.requests += 1;
                x.latency += took;
            }
            None => st.sources.push(SourceStats {
                repo,
                source,
                requests: 1,
                latency: took,
            }),
        }
    }

    /// Start the overall deadline: every request sent later is cut when `total` has passed since this call, and a
    /// request that would start after it is not sent.
    pub fn arm_deadline(&self, total: Duration) {
        *self.deadline.lock().unwrap() = Some((Instant::now() + total, total));
    }

    /// Time left for a request: `Err` with the message once the deadline has passed.
    fn time_left(&self) -> std::result::Result<Duration, String> {
        let Some((end, total)) = *self.deadline.lock().unwrap() else {
            return Ok(REQUEST_TIMEOUT);
        };
        let left = end.saturating_duration_since(Instant::now());
        if left.is_zero() {
            return Err(deadline_message(total));
        }
        Ok(left.min(REQUEST_TIMEOUT))
    }

    /// The deadline has passed (it was armed and its end is behind us).
    fn deadline_passed(&self) -> Option<Duration> {
        let (end, total) = (*self.deadline.lock().unwrap())?;
        (Instant::now() >= end).then_some(total)
    }

    /// The API base this client sends its requests to (no trailing slash).
    pub fn api_base(&self) -> &str {
        &self.base
    }

    /// Credentials for git-over-HTTPS against github.com (handed to gix's transport, never printed).
    pub fn basic_auth(&self) -> (String, String) {
        ("x-access-token".to_string(), self.token.clone())
    }

    /// GET returning the JSON body, or `None` for 404 (the one status callers treat as an answer).
    fn get_optional(&self, url: &str) -> Result<Option<Value>> {
        let path = url.trim_start_matches(self.base.as_str());
        let timeout = self.time_left().map_err(Error::github)?;
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
            .call()
            .map_err(|e| match self.deadline_passed() {
                Some(total) => Error::github(deadline_message(total)),
                None => Error::github(format!("GitHub API unreachable for {path}: {e}")),
            });
        let rate = sent.as_ref().ok().and_then(|r| rate_from(r.headers()));
        self.record(path, started.elapsed(), rate);
        let mut resp = sent?;
        if is_rate_limit(&mut resp) {
            self.note_rate_limit();
        }
        match resp.status().as_u16() {
            200 => Ok(Some(resp.body_mut().read_json::<Value>().map_err(|e| {
                Error::github(format!("GitHub API sent unreadable JSON for {path}: {e}"))
            })?)),
            404 => Ok(None),
            401 => Err(Error::github(format!(
                "GitHub API HTTP 401 for {path}: token rejected; set a valid GH_TOKEN or log in to GitHub with gh"
            ))),
            403 => Err(Error::github(format!(
                "GitHub API HTTP 403 for {path}: forbidden or rate limited; retry later or check the token scopes"
            ))),
            code => Err(Error::github(format!("GitHub API HTTP {code} for {path}"))),
        }
    }

    /// POST one GraphQL document to `{base}/graphql`. The token stays in the header.
    pub fn graphql(&self, body: &Value) -> Result<Value> {
        let url = format!("{}/graphql", self.base.trim_end_matches('/'));
        let timeout = self.time_left().map_err(Error::github)?;
        let mut resp = self
            .agent
            .post(&url)
            .config()
            .timeout_global(Some(timeout))
            .build()
            .header("Authorization", &format!("Bearer {}", self.token))
            .header("Accept", "application/json")
            .header("Content-Type", "application/json")
            .send_json(body)
            .map_err(|e| Error::github(format!("GitHub GraphQL unreachable: {e}")))?;
        if resp.status().as_u16() != 200 {
            return Err(Error::github(format!(
                "GitHub GraphQL HTTP {}",
                resp.status().as_u16()
            )));
        }
        resp.body_mut()
            .read_json::<Value>()
            .map_err(|e| Error::github(format!("GitHub GraphQL sent unreadable JSON: {e}")))
    }

    fn get(&self, url: &str) -> Result<Value> {
        self.get_optional(url)?.ok_or_else(|| {
            Error::github(format!(
                "GitHub API HTTP 404 for {}: not found, or the token has no access to it",
                url.trim_start_matches(self.base.as_str())
            ))
        })
    }

    /// Open PRs, newest first (limit 100 like the prototype), with merge state from the per-PR endpoint.
    pub fn open_prs(&self, slug: &str) -> Result<Vec<OpenPr>> {
        let api = &self.base;
        let list = self.get(&format!(
            "{api}/repos/{slug}/pulls?state=open&per_page={PAGE}"
        ))?;
        let items = list
            .as_array()
            .ok_or_else(|| Error::github("GitHub API sent an unexpected pulls response"))?;
        let mut prs: Vec<OpenPr> = items
            .iter()
            .map(|p| OpenPr {
                number: p["number"].as_u64().unwrap_or(0),
                title: p["title"].as_str().unwrap_or("").to_string(),
                head: p["head"]["ref"].as_str().unwrap_or("").to_string(),
                head_sha: p["head"]["sha"].as_str().unwrap_or("").to_string(),
                base: p["base"]["ref"].as_str().unwrap_or("").to_string(),
                default_branch: p["base"]["repo"]["default_branch"]
                    .as_str()
                    .unwrap_or("")
                    .to_string(),
                draft: p["draft"].as_bool().unwrap_or(false),
                author: p["user"]["login"].as_str().unwrap_or("").to_string(),
                bot: p["user"]["type"].as_str() == Some("Bot")
                    || p["user"]["login"]
                        .as_str()
                        .is_some_and(|l| l.ends_with("[bot]")),
                review: None,
                merge_state: None,
            })
            .collect();
        // mergeable_state exists only on the single-PR endpoint: one request per PR, in parallel
        std::thread::scope(|s| {
            let hs: Vec<_> = prs
                .iter()
                .map(|p| {
                    let n = p.number;
                    s.spawn(move || {
                        for attempt in 0..2 {
                            if let Ok(v) = self.get(&format!("{api}/repos/{slug}/pulls/{n}")) {
                                if let Some(ms) = v["mergeable_state"].as_str() {
                                    if ms != "unknown" || attempt == 1 {
                                        return Some(ms.to_uppercase());
                                    }
                                }
                            }
                            std::thread::sleep(Duration::from_millis(400));
                        }
                        None
                    })
                })
                .collect();
            for (p, h) in prs.iter_mut().zip(hs) {
                p.merge_state = h.join().ok().flatten();
            }
        });
        Ok(prs)
    }

    /// Open issues (the issues endpoint also lists pull requests; those are skipped). One page of `PAGE`
    /// entries: a full page sets `truncated`, no further pages are read.
    pub fn open_issues(&self, slug: &str) -> Result<IssueCounts> {
        let api = &self.base;
        let v = self.get(&format!(
            "{api}/repos/{slug}/issues?state=open&per_page={PAGE}"
        ))?;
        let items = v
            .as_array()
            .ok_or_else(|| Error::github("GitHub API sent an unexpected issues response"))?;
        let issues: Vec<&Value> = items
            .iter()
            .filter(|i| i.get("pull_request").is_none())
            .collect();
        let bug = issues
            .iter()
            .filter(|i| {
                i["labels"].as_array().is_some_and(|l| {
                    l.iter().any(|x| {
                        x["name"]
                            .as_str()
                            .is_some_and(|n| n.eq_ignore_ascii_case("bug"))
                    })
                })
            })
            .count();
        Ok(IssueCounts {
            open: issues.len(),
            bug,
            truncated: items.len() >= PAGE,
        })
    }

    /// Newest `limit` PRs of every state (the prototype used `gh pr list --state all --limit 500`).
    pub fn all_prs(&self, slug: &str, limit: usize) -> Result<Vec<AnyPr>> {
        let api = &self.base;
        let mut out = Vec::new();
        let mut page = 1;
        while out.len() < limit {
            let v = self.get(&format!(
                "{api}/repos/{slug}/pulls?state=all&sort=created&direction=desc&per_page=100&page={page}"
            ))?;
            let items = v
                .as_array()
                .ok_or_else(|| Error::github("GitHub API sent an unexpected pulls response"))?;
            if items.is_empty() {
                break;
            }
            for p in items {
                let state = if !p["merged_at"].is_null() {
                    "MERGED"
                } else if p["state"].as_str() == Some("open") {
                    "OPEN"
                } else {
                    "CLOSED"
                };
                out.push(AnyPr {
                    head_ref: p["head"]["ref"].as_str().unwrap_or("").to_string(),
                    head_sha: p["head"]["sha"].as_str().unwrap_or("").to_string(),
                    number: p["number"].as_u64().unwrap_or(0),
                    state,
                    merge_commit_sha: p["merge_commit_sha"].as_str().unwrap_or("").to_string(),
                    merged_at: p["merged_at"].as_str().and_then(parse_utc),
                    title: p["title"].as_str().unwrap_or("").to_string(),
                    head_repo: p["head"]["repo"]["full_name"]
                        .as_str()
                        .unwrap_or("")
                        .to_string(),
                });
            }
            if items.len() < 100 {
                break;
            }
            page += 1;
        }
        out.truncate(limit);
        Ok(out)
    }

    /// The commits of pull request `number` (`GET pulls/N/commits?per_page=100`, one request), oldest first.
    pub fn pr_commits(&self, slug: &str, number: u64) -> Result<PrCommitsRead> {
        let api = &self.base;
        let v = self.get(&format!(
            "{api}/repos/{slug}/pulls/{number}/commits?per_page={PAGE}"
        ))?;
        let items = v.as_array().ok_or_else(|| {
            Error::github("GitHub API sent an unexpected pull request commits response")
        })?;
        if items.len() >= PAGE {
            return Ok(PrCommitsRead::TooMany);
        }
        let bad = || Error::github("GitHub API sent an unreadable pull request commit");
        let mut out = Vec::with_capacity(items.len());
        for c in items {
            let id = c["sha"]
                .as_str()
                .and_then(|h| gix::ObjectId::from_hex(h.as_bytes()).ok())
                .ok_or_else(bad)?;
            let a = &c["commit"]["author"];
            out.push(crate::tidy_rebase::PrCommit {
                id,
                name: a["name"].as_str().ok_or_else(bad)?.as_bytes().to_vec(),
                mail: a["email"].as_str().ok_or_else(bad)?.as_bytes().to_vec(),
                secs: a["date"].as_str().and_then(parse_utc).ok_or_else(bad)?,
                subject: crate::tidy_rebase::subject_of(
                    c["commit"]["message"].as_str().ok_or_else(bad)?.as_bytes(),
                ),
            });
        }
        Ok(PrCommitsRead::Commits(out))
    }

    /// SHA of `refs/heads/<branch>` on the remote (read-only replacement of `git ls-remote`).
    pub fn remote_branch_sha(&self, slug: &str, branch: &str) -> Result<Option<String>> {
        let api = &self.base;
        let v = self.get_optional(&format!("{api}/repos/{slug}/git/ref/heads/{branch}"))?;
        Ok(v.and_then(|v| v["object"]["sha"].as_str().map(str::to_string)))
    }
}
