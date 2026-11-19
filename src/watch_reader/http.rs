//! Pure rules of the watch reader: what the reader may request, how headers are read, what the rate limit asks for
//! and how remote URLs and host names are judged. Port of `internal/hosting/watch_*.go` and the remote rules of
//! `internal/watchcmd`. No I/O and no clock: times are Unix seconds that the caller passes in.

/// 25 or 12 pull requests per list page; the page bounds below give 2000 open pull requests at most.
pub const MAX_PAGES_25: u64 = 80;
pub const MAX_PAGES_12: u64 = 167;
/// Sanity bound of `max_open_prs`.
pub const MAX_OPEN_PRS: usize = 2000;
/// Check run and status pages are 100 items; at most this many pages are read per head.
pub const MAX_CHECK_PAGES: u64 = 10;
/// The restack reads up to three commit pages.
pub const MAX_COMMIT_PAGES: u64 = 3;
/// A response body is read up to this size; more is an error.
pub const MAX_BODY: usize = 1 << 20;
/// Seconds added to a reset or Retry-After time before the reader tries again.
pub const RESET_SLACK_SECS: i64 = 5;
/// Below this many remaining requests the reader waits for the reset.
pub const RATE_WAIT_BELOW: u64 = 50;

/// Rate-limit state from the `X-RateLimit-*` headers or from `rate_limit`.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct RateInfo {
    pub known: bool,
    pub limit: u64,
    pub remaining: u64,
    /// Unix time of the reset.
    pub reset: i64,
}

/// What the rate limit asks of the caller (a typed signal; this layer never sleeps).
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Advice {
    /// No action.
    None,
    /// Remaining is below 10 percent of the limit: double the interval of the next cycle.
    Slow,
    /// Remaining is below 50: wait until `RateDecision::until`.
    Wait,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct RateDecision {
    pub advice: Advice,
    /// Unix time: reset plus 5 s for `Wait`, 0 otherwise.
    pub until: i64,
}

/// The slowdown policy: below 50 remaining waits for the reset, below 10 percent of the limit slows down, anything
/// else (or an unknown state) needs no action.
pub fn assess_rate_limit(info: &RateInfo) -> RateDecision {
    if !info.known {
        return RateDecision {
            advice: Advice::None,
            until: 0,
        };
    }
    if info.remaining < RATE_WAIT_BELOW {
        return RateDecision {
            advice: Advice::Wait,
            until: info.reset.saturating_add(RESET_SLACK_SECS),
        };
    }
    if u128::from(info.remaining) * 10 < u128::from(info.limit) {
        return RateDecision {
            advice: Advice::Slow,
            until: 0,
        };
    }
    RateDecision {
        advice: Advice::None,
        until: 0,
    }
}

/// When to try again after a rate-limited answer: Retry-After if given, else the reset time if it lies ahead, each
/// plus 5 s; with no usable evidence one minute plus 5 s from `now`.
pub fn rate_limited_until(retry_after: u64, rate: &RateInfo, now: i64) -> i64 {
    if retry_after > 0 {
        let secs = i64::try_from(retry_after).unwrap_or(i64::MAX);
        return now.saturating_add(secs).saturating_add(RESET_SLACK_SECS);
    }
    if rate.known && rate.reset > now {
        return rate.reset.saturating_add(RESET_SLACK_SECS);
    }
    now.saturating_add(60 + RESET_SLACK_SECS)
}

/// The `Retry-After` header in seconds: 0 to 86400 accepted, anything else is no evidence.
pub fn parse_retry_after(header: &str) -> Option<u64> {
    let s: i64 = header.parse().ok()?;
    (0..=24 * 3600).contains(&s).then_some(s as u64)
}

/// The three `X-RateLimit-*` headers; unknown unless all three are numbers with limit and reset above zero and a
/// remaining count that is not negative.
pub fn parse_rate_headers(limit: &str, remaining: &str, reset: &str) -> RateInfo {
    let (Ok(limit), Ok(remaining), Ok(reset)) = (
        limit.parse::<i64>(),
        remaining.parse::<i64>(),
        reset.parse::<i64>(),
    ) else {
        return RateInfo::default();
    };
    if limit <= 0 || remaining < 0 || reset <= 0 {
        return RateInfo::default();
    }
    RateInfo {
        known: true,
        limit: limit as u64,
        remaining: remaining as u64,
        reset,
    }
}

// ---- URL pieces ------------------------------------------------------------------------------------------

fn hex_val(b: u8) -> Option<u8> {
    match b {
        b'0'..=b'9' => Some(b - b'0'),
        b'a'..=b'f' => Some(b - b'a' + 10),
        b'A'..=b'F' => Some(b - b'A' + 10),
        _ => None,
    }
}

/// Percent-decoding as Go's `url.PathUnescape` / `QueryUnescape`: a `%` needs two hex digits, `+` is a space only
/// in a query. `None` for a bad escape.
fn unescape(s: &str, plus_is_space: bool) -> Option<Vec<u8>> {
    let b = s.as_bytes();
    let mut out = Vec::with_capacity(b.len());
    let mut i = 0;
    while i < b.len() {
        match b[i] {
            b'%' => {
                let hi = hex_val(*b.get(i + 1)?)?;
                let lo = hex_val(*b.get(i + 2)?)?;
                out.push(hi * 16 + lo);
                i += 3;
            }
            b'+' if plus_is_space => {
                out.push(b' ');
                i += 1;
            }
            c => {
                out.push(c);
                i += 1;
            }
        }
    }
    Some(out)
}

/// Go's `url.PathEscape` for one path segment: letters, digits and `- _ . ~ $ & + = : @` stay, everything else
/// (including `/ ; , ?`) becomes an uppercase `%XX` of each UTF-8 byte.
fn path_escape(s: &str) -> String {
    const HEX: &[u8; 16] = b"0123456789ABCDEF";
    let mut out = String::with_capacity(s.len());
    for &c in s.as_bytes() {
        if c.is_ascii_alphanumeric() || b"-_.~$&+=:@".contains(&c) {
            out.push(c as char);
        } else {
            out.push('%');
            out.push(HEX[(c >> 4) as usize] as char);
            out.push(HEX[(c & 15) as usize] as char);
        }
    }
    out
}

/// The first value of `key` in a raw query. Pairs with a bad escape or a semicolon are dropped, as in Go.
fn query_get(query: &str, key: &str) -> Option<String> {
    for pair in query.split('&') {
        if pair.is_empty() || pair.contains(';') {
            continue;
        }
        let (k, v) = pair.split_once('=').unwrap_or((pair, ""));
        let (Some(k), Some(v)) = (unescape(k, true), unescape(v, true)) else {
            continue;
        };
        if k == key.as_bytes() {
            return String::from_utf8(v).ok();
        }
    }
    None
}

fn has_ctl(s: &str) -> bool {
    s.bytes().any(|b| b < 0x20 || b == 0x7f)
}

// ---- Link header -----------------------------------------------------------------------------------------

/// The `rel` tokens (lower-cased) of every entry of a Link header.
fn rels(link: &str) -> Vec<String> {
    let mut out = Vec::new();
    for entry in link.split(',') {
        let entry = entry.trim();
        let Some(end) = entry.find('>') else { continue };
        if !entry.starts_with('<') {
            continue;
        }
        for param in entry[end + 1..].split(';') {
            if let Some((k, v)) = param.trim().split_once('=') {
                if k.trim().eq_ignore_ascii_case("rel") {
                    out.extend(
                        v.trim()
                            .trim_matches('"')
                            .split_whitespace()
                            .map(str::to_lowercase),
                    );
                }
            }
        }
    }
    out
}

/// Whether any entry of the Link header has the relation `next`.
pub fn has_next_rel(link: &str) -> bool {
    rels(link).iter().any(|r| r == "next")
}

/// The page number of the `rel="next"` entry of a Link header (`rel` with or without quotes). The target URL itself is
/// never requested: callers ask for the page number through the typed functions, which check it again.
pub fn next_page_from_link(link: &str) -> Option<u64> {
    for entry in link.split(',') {
        let entry = entry.trim();
        if !entry.starts_with('<') {
            continue;
        }
        let Some(end) = entry.find('>') else { continue };
        let mut next = false;
        for param in entry[end + 1..].split(';') {
            if let Some((k, v)) = param.trim().split_once('=') {
                if k.trim().eq_ignore_ascii_case("rel")
                    && v.trim()
                        .trim_matches('"')
                        .split_whitespace()
                        .any(|r| r.eq_ignore_ascii_case("next"))
                {
                    next = true;
                }
            }
        }
        if !next {
            continue;
        }
        let target = &entry[1..end];
        if has_ctl(target) {
            return None;
        }
        let no_fragment = target.split('#').next().unwrap_or("");
        let (head, query) = no_fragment.split_once('?').unwrap_or((no_fragment, ""));
        // a bad escape before the query makes the URL unparsable
        unescape(head, false)?;
        let page: i64 = query_get(query, "page")?.parse().ok()?;
        return (1..=1_000_000).contains(&page).then_some(page as u64);
    }
    None
}

// ---- names and values ------------------------------------------------------------------------------------

fn all_name_chars(s: &str) -> bool {
    s.bytes()
        .all(|b| b.is_ascii_alphanumeric() || matches!(b, b'_' | b'.' | b'-'))
}

/// `[A-Za-z0-9_-][A-Za-z0-9_.-]*` (the route pattern of the Go allowlist).
fn route_segment(s: &str) -> bool {
    let b = s.as_bytes();
    !b.is_empty()
        && (b[0].is_ascii_alphanumeric() || matches!(b[0], b'_' | b'-'))
        && all_name_chars(s)
}

/// An `OWNER/NAME` of the hosting layer: both parts start with a letter, digit or `_`, then letters, digits,
/// `_ . -`, at most 100 bytes each.
pub fn valid_repo(s: &str) -> bool {
    let mut p = s.split('/');
    let (Some(a), Some(b), None) = (p.next(), p.next(), p.next()) else {
        return false;
    };
    let seg = |x: &str| {
        let f = x.as_bytes().first().copied();
        f.is_some_and(|c| c.is_ascii_alphanumeric() || c == b'_')
            && all_name_chars(x)
            && x.len() <= 100
    };
    seg(a) && seg(b)
}

/// An `OWNER/NAME` as `stw` accepts it from the command line (`untrusted.ValidRepo`): a leading `-` is allowed.
fn valid_repo_flag(s: &str) -> bool {
    let mut p = s.split('/');
    let (Some(a), Some(b), None) = (p.next(), p.next(), p.next()) else {
        return false;
    };
    route_segment(a) && route_segment(b)
}

/// 40 lower-case hex digits.
pub fn valid_sha(s: &str) -> bool {
    s.len() == 40 && s.bytes().all(|b| matches!(b, b'0'..=b'9' | b'a'..=b'f'))
}

/// A validator is printable ASCII, at most 256 bytes (header injection and oversized values never leave).
pub fn valid_etag(s: &str) -> bool {
    s.len() <= 256 && s.bytes().all(|b| (0x20..=0x7e).contains(&b))
}

/// Git ref-name rules that matter for a path segment, plus: a literal `%` is refused (no text that is already
/// encoded can pass as a name), and so is U+FFFD (what invalid UTF-8 turns into when it is exported).
fn valid_branch_name(n: &str) -> bool {
    if n.is_empty()
        || n.len() > 255
        || n == "@"
        || n.starts_with('-')
        || n.starts_with('/')
        || n.ends_with('/')
        || n.ends_with('.')
        || n.ends_with(".lock")
        || n.contains("..")
        || n.contains("@{")
        || n.contains("//")
    {
        return false;
    }
    if n.chars()
        .any(|c| (c as u32) < 0x20 || c == '\u{7f}' || c == '\u{fffd}' || " ~^:?*[\\%".contains(c))
    {
        return false;
    }
    n.split('/')
        .all(|part| !part.starts_with('.') && !part.ends_with(".lock"))
}

/// The percent-encoded path segment of `branches/BASE` for a branch name; `None` for a name the reader refuses (the
/// pull request is then watched without the base tip read). A slash becomes `%2F`, so the segment never adds a path
/// component.
pub fn encode_branch_segment(name: &str) -> Option<String> {
    valid_branch_name(name).then(|| path_escape(name))
}

/// Whether `raw` is exactly what `encode_branch_segment` produces: decoded once, the name must be valid and
/// encoding it again must give the same text. Double encoding, dot segments, lowercase escapes and every other
/// alternative spelling are refused.
pub fn branch_segment_valid(raw: &str) -> bool {
    if raw.is_empty() {
        return false;
    }
    let Some(bytes) = unescape(raw, false) else {
        return false;
    };
    let Ok(name) = String::from_utf8(bytes) else {
        return false;
    };
    valid_branch_name(&name) && path_escape(&name) == raw
}

// ---- the allowlist of GET endpoints ----------------------------------------------------------------------

fn digits_from_one(s: &str) -> Option<u64> {
    let b = s.as_bytes();
    (!b.is_empty() && b[0] != b'0' && b.iter().all(u8::is_ascii_digit))
        .then(|| s.parse().ok())
        .flatten()
}

fn is_number_from_one(s: &str) -> bool {
    let b = s.as_bytes();
    !b.is_empty() && b[0] != b'0' && b.iter().all(u8::is_ascii_digit)
}

/// A page of the check, status and Actions reads: 1 to 10.
fn check_page(s: &str) -> bool {
    digits_from_one(s).is_some_and(|p| p <= MAX_CHECK_PAGES)
}

/// Whether `endpoint` (path and query without the API prefix, as it is sent) is one the watch reader may GET: the
/// open pull list at 25 or 12 per page within its page bounds, `pulls/N`, its commits (pages 1 to 3), `branches/SEG`
/// with an encoded segment, `commits/SHA`, its check runs and statuses (pages 1 to 10), `rate_limit`, and the
/// Actions runs of a commit (the fallback for a token without the checks permission; new against Go). Only GET exists:
/// there is no function to send anything else.
pub fn read_endpoint_allowed(endpoint: &str) -> bool {
    if endpoint == "rate_limit" {
        return true;
    }
    let Some(rest) = endpoint.strip_prefix("repos/") else {
        return false;
    };
    let mut it = rest.splitn(3, '/');
    let (Some(owner), Some(name), Some(tail)) = (it.next(), it.next(), it.next()) else {
        return false;
    };
    if !route_segment(owner) || !route_segment(name) || !valid_repo(&format!("{owner}/{name}")) {
        return false;
    }
    if let Some(q) = tail.strip_prefix("pulls?state=open&per_page=") {
        let (limit, page) = if let Some(p) = q.strip_prefix("25&page=") {
            (MAX_PAGES_25, p)
        } else if let Some(p) = q.strip_prefix("12&page=") {
            (MAX_PAGES_12, p)
        } else {
            return false;
        };
        return digits_from_one(page).is_some_and(|p| p <= limit);
    }
    if let Some(q) = tail.strip_prefix("pulls/") {
        if let Some((n, c)) = q.split_once('/') {
            return is_number_from_one(n)
                && c.strip_prefix("commits?per_page=100&page=")
                    .is_some_and(|p| digits_from_one(p).is_some_and(|p| p <= MAX_COMMIT_PAGES));
        }
        return is_number_from_one(q);
    }
    if let Some(seg) = tail.strip_prefix("branches/") {
        return !seg.is_empty() && !seg.contains(['/', '?', '#']) && branch_segment_valid(seg);
    }
    if let Some(q) = tail.strip_prefix("commits/") {
        let (sha, more) = q.split_at(q.len().min(40));
        if !valid_sha(sha) {
            return false;
        }
        if more.is_empty() {
            return true;
        }
        let page = more
            .strip_prefix("/check-runs?per_page=100&page=")
            .or_else(|| more.strip_prefix("/statuses?per_page=100&page="));
        return page.is_some_and(check_page);
    }
    if let Some(q) = tail.strip_prefix("actions/runs?head_sha=") {
        let (sha, more) = q.split_at(q.len().min(40));
        return valid_sha(sha)
            && more
                .strip_prefix("&per_page=100&page=")
                .is_some_and(check_page);
    }
    false
}

// ---- remotes and hosts -----------------------------------------------------------------------------------

/// The host and the path of a remote URL (https, http, ssh://, git:// and scp-like `[user@]host:owner/name`) without
/// judging either; `None` for a local path or another scheme.
pub fn split_remote(remote: &str) -> Option<(String, String)> {
    let remote = remote.trim();
    if let Some((scheme, rest)) = remote.split_once("://") {
        if !["https", "http", "ssh", "git"]
            .iter()
            .any(|s| scheme.eq_ignore_ascii_case(s))
            || has_ctl(rest)
        {
            return None;
        }
        let end = rest.find(['/', '?', '#']).unwrap_or(rest.len());
        let (authority, tail) = rest.split_at(end);
        let host_port = authority.rsplit_once('@').map_or(authority, |(_, h)| h);
        let (host, port) = if let Some(inner) = host_port.strip_prefix('[') {
            let (h, after) = inner.split_once(']')?;
            if after.is_empty() {
                (h, "")
            } else {
                (h, after.strip_prefix(':')?)
            }
        } else {
            host_port.split_once(':').unwrap_or((host_port, ""))
        };
        if !port.bytes().all(|b| b.is_ascii_digit()) || host.contains(' ') {
            return None;
        }
        let raw_path = tail.split(['?', '#']).next().unwrap_or("");
        let path = String::from_utf8(unescape(raw_path, false)?).ok()?;
        return Some((
            host.to_string(),
            path.strip_prefix('/').unwrap_or(&path).to_string(),
        ));
    }
    let (before, after) = remote.split_once(':')?;
    if before.contains('/') {
        return None;
    }
    let host = before.split_once('@').map_or(before, |(_, h)| h);
    Some((host.to_string(), after.to_string()))
}

/// `OWNER/NAME` of a remote URL of `host` (a trailing slash and `.git` removed; another host, extra path segments and
/// names that are not repository names refused).
pub fn parse_remote(remote: &str, host: &str) -> Option<String> {
    let (h, path) = split_remote(remote)?;
    if !h.eq_ignore_ascii_case(host) {
        return None;
    }
    let path = path.strip_suffix('/').unwrap_or(&path);
    let path = path.strip_suffix(".git").unwrap_or(path);
    let path = path.strip_suffix('/').unwrap_or(path);
    (path.matches('/').count() == 1 && valid_repo_flag(path)).then(|| path.to_string())
}

/// A host name with an optional port (`--host`): letters, digits, dots and hyphens, starting and ending with a letter
/// or digit, then an optional `:` and one to five digits.
pub fn valid_host(s: &str) -> bool {
    let (name, port) = match s.split_once(':') {
        Some((n, p)) => (n, Some(p)),
        None => (s, None),
    };
    if let Some(p) = port {
        if p.is_empty() || p.len() > 5 || !p.bytes().all(|b| b.is_ascii_digit()) {
            return false;
        }
    }
    let b = name.as_bytes();
    let (Some(first), Some(last)) = (b.first(), b.last()) else {
        return false;
    };
    first.is_ascii_alphanumeric()
        && last.is_ascii_alphanumeric()
        && b.iter()
            .all(|c| c.is_ascii_alphanumeric() || matches!(c, b'.' | b'-'))
}

// ---- the reader ------------------------------------------------------------------------------------------

use std::collections::{BTreeMap, BTreeSet};
use std::sync::Mutex;
use std::time::Duration;

/// One request takes at most this long (Go: `DefaultRequestTimeout`).
pub const REQUEST_TIMEOUT: Duration = Duration::from_secs(30);

/// The watch-error codes this layer produces (`contracts/watch-events.md`).
#[derive(Clone, Copy, Debug, PartialEq, Eq, PartialOrd, Ord)]
pub enum Code {
    RateLimited,
    Network,
    Unauthorized,
    Forbidden,
    Http5xx,
    Malformed,
    TooManyPrs,
    PageTooLarge,
    RepoMoved,
}

impl Code {
    pub fn as_str(self) -> &'static str {
        match self {
            Code::RateLimited => "rate-limited",
            Code::Network => "network",
            Code::Unauthorized => "unauthorized",
            Code::Forbidden => "forbidden",
            Code::Http5xx => "http-5xx",
            Code::Malformed => "malformed-response",
            Code::TooManyPrs => "too-many-open-prs",
            Code::PageTooLarge => "page-too-large",
            Code::RepoMoved => "repo-moved",
        }
    }
}

/// The only error type of the reader. The message is fixed, bounded text: neither response bodies, headers nor the
/// token reach it (the one variable part, `new_name`, passed `valid_repo`).
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct WatchError {
    pub code: Code,
    pub message: String,
    /// The HTTP status when there was a response. An inaccessible repository is a 404, classified `Forbidden` with
    /// status 404 so that callers can tell a deleted branch (`branches/BASE`) apart.
    pub status: u16,
    /// The `owner/name` of a refused 301, when its Location names the repository (never followed).
    pub new_name: String,
    /// Evidence of `RateLimited`.
    pub rate: RateInfo,
    pub retry_after: u64,
}

impl WatchError {
    pub fn new(code: Code, message: impl Into<String>) -> WatchError {
        WatchError {
            code,
            message: message.into(),
            status: 0,
            new_name: String::new(),
            rate: RateInfo::default(),
            retry_after: 0,
        }
    }

    /// When to try again after a `RateLimited` error (Unix seconds).
    pub fn rate_limited_until(&self, now: i64) -> i64 {
        rate_limited_until(self.retry_after, &self.rate, now)
    }
}

impl std::fmt::Display for WatchError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(
            f,
            "watch read failed: {}: {}",
            self.code.as_str(),
            self.message
        )
    }
}

impl std::error::Error for WatchError {}

fn malformed(msg: &str) -> WatchError {
    WatchError::new(Code::Malformed, msg)
}

/// Groups requests for the per-poll accounting.
#[derive(Clone, Copy, Debug, PartialEq, Eq, PartialOrd, Ord)]
pub enum Kind {
    List,
    Detail,
    Commits,
    Branch,
    Commit,
    Checks,
    RateLimit,
    Actions,
}

/// Requests of one kind; `not_modified` is the subset answered 304 (those cost no rate limit).
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct CallCount {
    pub requests: u64,
    pub not_modified: u64,
}

/// One successful read. For a 304 `not_modified` is set, the body is empty and `etag` is the validator that was
/// sent, unchanged (a weak prefix included).
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct WatchResponse {
    pub status: u16,
    pub not_modified: bool,
    pub etag: String,
    pub link: String,
    pub body: Vec<u8>,
    pub rate: RateInfo,
}

/// The part of a list item the watcher keeps.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct PullItem {
    pub number: u64,
    pub title: String,
    pub user: String,
    pub labels: Vec<String>,
    pub draft: bool,
    pub head_sha: String,
    pub head_ref: String,
    pub base_ref: String,
    pub base_sha: String,
}

/// One stored list page: the ETag and Link header of the response and the parsed items, so that a 304 needs no body.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct PullPage {
    pub etag: String,
    pub link: String,
    pub items: Vec<PullItem>,
}

/// What must be kept between two listings of one repository. `per_page` is 0 (nothing stored), 25 or 12 and only says
/// what size the stored pages have, so that their ETags are reused only for that size; it never selects the size of
/// the next listing (the halved size lives in the reader until the process ends).
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct PullListState {
    pub per_page: u32,
    pub pages: Vec<PullPage>,
}

/// A complete listing. `unchanged`: every page was a 304. The caller stores `state` and passes it to the next call.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct PullListResult {
    pub state: PullListState,
    pub items: Vec<PullItem>,
    pub unchanged: bool,
}

#[derive(Default)]
struct Inner {
    calls: BTreeMap<Kind, CallCount>,
    last: RateInfo,
    halved: BTreeSet<String>,
}

/// The GET-only reader for `stw watch`. It sleeps never and retries never (except the halving of the list page size);
/// callers decide when to poll again. Safe for concurrent use.
pub struct WatchHttp {
    agent: ureq::Agent,
    token: String,
    base: String,
    /// The host (with port) a refused redirect may name the repository on; `api.github.com` for github.com.
    api_host: String,
    timeout: Duration,
    inner: Mutex<Inner>,
}

impl std::fmt::Debug for WatchHttp {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "WatchHttp {{ base: {:?}, token: <hidden> }}", self.base)
    }
}

/// The API host and path prefix of a forge host: `api.github.com` for `github.com`, else `HOST/api/v3`.
pub fn api_destination(host: &str) -> (String, String) {
    if host == "github.com" || host.ends_with(".ghe.com") {
        (format!("api.{host}"), String::new())
    } else {
        (host.to_string(), "/api/v3".to_string())
    }
}

/// The items of one list page (stub).
pub fn parse_pull_items(_body: &[u8]) -> Result<Vec<PullItem>, WatchError> {
    Err(malformed("stub"))
}

impl WatchHttp {
    /// A reader for the API at `base` (no trailing slash; `https://api.github.com`, a GitHub Enterprise URL, or a test
    /// server). `api_host` is the host a refused redirect may name the repository on.
    pub fn new(token: String, base: String, api_host: String) -> WatchHttp {
        let agent: ureq::Agent = ureq::Agent::config_builder().build().into();
        WatchHttp {
            agent,
            token,
            base: base.trim_end_matches('/').to_string(),
            api_host,
            timeout: REQUEST_TIMEOUT,
            inner: Mutex::new(Inner::default()),
        }
    }

    /// The request counts since the last call; resets them.
    pub fn take_calls(&self) -> BTreeMap<Kind, CallCount> {
        std::mem::take(&mut self.inner.lock().unwrap().calls)
    }

    /// The most recent rate-limit state seen in any response.
    pub fn last_rate_limit(&self) -> RateInfo {
        self.inner.lock().unwrap().last
    }

    /// Remember that list pages of `repo` exceeded the body limit at 25.
    pub fn mark_halved(&self, repo: &str) {
        self.inner.lock().unwrap().halved.insert(repo.to_string());
    }

    fn fetch(
        &self,
        kind: Kind,
        endpoint: &str,
        etag: &str,
        list: bool,
    ) -> Result<WatchResponse, WatchError> {
        let _ = (kind, endpoint, etag, list);
        let _ = (
            &self.agent,
            &self.token,
            &self.api_host,
            self.timeout,
            &self.base,
        );
        Err(WatchError::new(Code::Network, "stub"))
    }

    fn repo_path(repo: &str) -> Result<String, WatchError> {
        if valid_repo(repo) {
            Ok(format!("repos/{repo}"))
        } else {
            Err(malformed("invalid repository name"))
        }
    }

    /// One page of open pull requests (`per_page` 25 or 12; the page is bounded by the allowlist).
    pub fn pull_list_page(
        &self,
        repo: &str,
        per_page: u32,
        page: u64,
        etag: &str,
    ) -> Result<WatchResponse, WatchError> {
        let base = Self::repo_path(repo)?;
        self.fetch(
            Kind::List,
            &format!("{base}/pulls?state=open&per_page={per_page}&page={page}"),
            etag,
            true,
        )
    }

    /// `pulls/N`.
    pub fn pull_detail(
        &self,
        repo: &str,
        number: u64,
        etag: &str,
    ) -> Result<WatchResponse, WatchError> {
        let base = Self::repo_path(repo)?;
        self.fetch(Kind::Detail, &format!("{base}/pulls/{number}"), etag, false)
    }

    /// Page 1 to 3 of `pulls/N/commits` (100 per page).
    pub fn pull_commits(
        &self,
        repo: &str,
        number: u64,
        page: u64,
        etag: &str,
    ) -> Result<WatchResponse, WatchError> {
        let base = Self::repo_path(repo)?;
        self.fetch(
            Kind::Commits,
            &format!("{base}/pulls/{number}/commits?per_page=100&page={page}"),
            etag,
            false,
        )
    }

    /// `branches/BASE`; the name is encoded by `encode_branch_segment`.
    pub fn branch(&self, repo: &str, name: &str, etag: &str) -> Result<WatchResponse, WatchError> {
        let base = Self::repo_path(repo)?;
        let seg = encode_branch_segment(name).ok_or_else(|| malformed("invalid branch name"))?;
        self.fetch(Kind::Branch, &format!("{base}/branches/{seg}"), etag, false)
    }

    /// `commits/SHA` (40 lower-case hex).
    pub fn commit(&self, repo: &str, sha: &str, etag: &str) -> Result<WatchResponse, WatchError> {
        let base = Self::repo_path(repo)?;
        self.fetch(Kind::Commit, &format!("{base}/commits/{sha}"), etag, false)
    }

    fn checks(
        &self,
        repo: &str,
        sha: &str,
        which: &str,
        page: u64,
        etag: &str,
    ) -> Result<WatchResponse, WatchError> {
        let base = Self::repo_path(repo)?;
        self.fetch(
            Kind::Checks,
            &format!("{base}/commits/{sha}/{which}?per_page=100&page={page}"),
            etag,
            false,
        )
    }

    /// `commits/SHA/check-runs` page 1 to 10.
    pub fn check_runs(
        &self,
        repo: &str,
        sha: &str,
        page: u64,
        etag: &str,
    ) -> Result<WatchResponse, WatchError> {
        self.checks(repo, sha, "check-runs", page, etag)
    }

    /// `commits/SHA/statuses` page 1 to 10.
    pub fn statuses(
        &self,
        repo: &str,
        sha: &str,
        page: u64,
        etag: &str,
    ) -> Result<WatchResponse, WatchError> {
        self.checks(repo, sha, "statuses", page, etag)
    }

    /// `actions/runs?head_sha=SHA` page 1 to 10: the fallback for a token that cannot read check runs.
    pub fn actions_runs(
        &self,
        repo: &str,
        sha: &str,
        page: u64,
    ) -> Result<WatchResponse, WatchError> {
        let base = Self::repo_path(repo)?;
        self.fetch(
            Kind::Actions,
            &format!("{base}/actions/runs?head_sha={sha}&per_page=100&page={page}"),
            "",
            false,
        )
    }

    /// `rate_limit` (stub).
    pub fn rate_limit(&self) -> Result<RateInfo, WatchError> {
        self.fetch(Kind::RateLimit, "rate_limit", "", false)?;
        Ok(RateInfo::default())
    }

    /// Lists the open pull requests of `repo` (stub).
    pub fn list_open_pulls(
        &self,
        _repo: &str,
        _prior: &PullListState,
        _max_open: usize,
    ) -> Result<PullListResult, WatchError> {
        Err(WatchError::new(Code::Network, "stub"))
    }
}
