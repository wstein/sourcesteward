//! Bounded reader of open pull requests (W2). One repository, a page cap, a body cap, and no writes.
//! A short failure (timeout, rate limit, 5xx) is transient: the cycle keeps the last accepted list.
//! Authentication and not-found answers are fatal.

use serde_json::Value;

/// How far one repository read may go.
pub const MAX_PAGES: u32 = 2;
pub const PER_PAGE: u32 = 30;
pub const BODY_LIMIT: u64 = 1_048_576;

/// One open pull request, the fields the cycle compares.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct OpenPr {
    pub number: u64,
    pub title: String,
    pub author: String,
    pub draft: bool,
    pub base: String,
    pub head_ref: String,
    pub head: String,
    pub fork: bool,
}

/// Why a read did not return a complete open list.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum ReadFailure {
    /// Try again later. The accepted list stays. `retry_until` is an absolute Unix
    /// second (the rate-limit reset). It wins over `retry_in_seconds`.
    Transient {
        code: String,
        retry_in_seconds: u64,
        retry_until: Option<i64>,
    },
    /// The repository cannot be read with this token. The accepted list stays.
    Fatal { code: String },
}

impl ReadFailure {
    pub fn code(&self) -> &str {
        match self {
            ReadFailure::Transient { code, .. } | ReadFailure::Fatal { code } => code,
        }
    }
}

/// One HTTP answer. The body is already capped by the caller.
pub struct HttpResponse {
    pub status: u16,
    pub body: Vec<u8>,
    pub retry_after_seconds: Option<u64>,
    /// `X-RateLimit-Remaining` was `0`. A 403 with this set is a rate limit, not a refusal.
    pub remaining_zero: bool,
    /// `X-RateLimit-Reset`, Unix seconds. Used when the budget is zero and `Retry-After` is absent.
    pub rate_reset: Option<i64>,
}

/// The only network seam. Tests supply it; the command uses `ureq`.
pub trait HttpGet {
    fn get(&self, url: &str) -> Result<HttpResponse, ReadFailure>;
}

/// Reads the open pull requests of `owner/name`. A full last page at the page cap is an incomplete
/// read: the caller must not treat the list as the whole set.
pub fn read_open_pulls(
    http: &dyn HttpGet,
    api_base: &str,
    repo: &str,
) -> Result<Vec<OpenPr>, ReadFailure> {
    if !valid_repo(repo) {
        return Err(ReadFailure::Fatal {
            code: "usage".into(),
        });
    }
    let mut all = Vec::new();
    for page in 1..=MAX_PAGES {
        let url = format!(
            "{}/repos/{repo}/pulls?state=open&per_page={PER_PAGE}&page={page}",
            api_base.trim_end_matches('/')
        );
        let resp = http.get(&url)?;
        if resp.status == 200 {
            if resp.body.len() as u64 > BODY_LIMIT {
                return Err(ReadFailure::Fatal {
                    code: "too-large".into(),
                });
            }
            let batch = parse_pulls(&resp.body).ok_or(ReadFailure::Fatal {
                code: "malformed".into(),
            })?;
            let full = batch.len() as u32 == PER_PAGE;
            all.extend(batch);
            if !full {
                return Ok(all);
            }
            if page == MAX_PAGES {
                return Err(ReadFailure::Transient {
                    code: "truncated".into(),
                    retry_in_seconds: 60,
                    retry_until: None,
                });
            }
            continue;
        }
        return Err(classify_status(
            resp.status,
            resp.retry_after_seconds,
            resp.remaining_zero,
            resp.rate_reset,
            &resp.body,
        ));
    }
    Ok(all)
}

/// Classifies a non-success status. A 403 is a rate limit when `Retry-After` is set, the
/// remaining budget is zero, or the first 4096 bytes of the body mention a rate limit. Any
/// other 403 is a permission failure.
pub fn classify_status(
    status: u16,
    retry_after_seconds: Option<u64>,
    remaining_zero: bool,
    rate_reset: Option<i64>,
    body: &[u8],
) -> ReadFailure {
    let limited = remaining_zero
        || retry_after_seconds.is_some()
        || crate::github::mentions_rate_limit(&body_head(body));
    let (retry, until) = retry_plan(retry_after_seconds, remaining_zero, rate_reset);
    match status {
        429 => ReadFailure::Transient {
            code: "rate-limited".into(),
            retry_in_seconds: retry,
            retry_until: until,
        },
        500..=599 => ReadFailure::Transient {
            code: "http-5xx".into(),
            retry_in_seconds: retry,
            retry_until: until,
        },
        403 if limited => ReadFailure::Transient {
            code: "rate-limited".into(),
            retry_in_seconds: retry,
            retry_until: until,
        },
        401 | 403 => ReadFailure::Fatal {
            code: "forbidden".into(),
        },
        404 => ReadFailure::Fatal {
            code: "not-found".into(),
        },
        _ => ReadFailure::Fatal {
            code: "http".into(),
        },
    }
}

pub fn parse_pulls(body: &[u8]) -> Option<Vec<OpenPr>> {
    let value: Value = serde_json::from_slice(body).ok()?;
    let items = value.as_array()?;
    let mut out = Vec::with_capacity(items.len());
    for item in items {
        let number = item.get("number").and_then(Value::as_u64)?;
        let title = item
            .get("title")
            .and_then(Value::as_str)
            .unwrap_or("")
            .to_string();
        let author = item
            .pointer("/user/login")
            .and_then(Value::as_str)
            .unwrap_or("")
            .to_string();
        let draft = item.get("draft").and_then(Value::as_bool).unwrap_or(false);
        let base = item
            .pointer("/base/ref")
            .and_then(Value::as_str)
            .unwrap_or("")
            .to_string();
        let head_ref = item
            .pointer("/head/ref")
            .and_then(Value::as_str)
            .unwrap_or("")
            .to_string();
        let head = item
            .pointer("/head/sha")
            .and_then(Value::as_str)
            .unwrap_or("")
            .to_string();
        let base_repo = item.pointer("/base/repo/full_name").and_then(Value::as_str);
        let head_repo = item.pointer("/head/repo/full_name").and_then(Value::as_str);
        let fork = match (base_repo, head_repo) {
            (Some(b), Some(h)) => !b.eq_ignore_ascii_case(h),
            _ => false,
        };
        out.push(OpenPr {
            number,
            title,
            author,
            draft,
            base,
            head_ref,
            head,
            fork,
        });
    }
    Some(out)
}

/// `Retry-After` is a duration. A primary limit with no such header waits until
/// `X-RateLimit-Reset`. Anything else waits 60s.
fn retry_plan(
    retry_after_seconds: Option<u64>,
    remaining_zero: bool,
    rate_reset: Option<i64>,
) -> (u64, Option<i64>) {
    if let Some(after) = retry_after_seconds {
        return (after, None);
    }
    if remaining_zero {
        if let Some(reset) = rate_reset {
            return (0, Some(reset));
        }
    }
    (60, None)
}

fn body_head(body: &[u8]) -> String {
    let end = body.len().min(4096);
    String::from_utf8_lossy(&body[..end]).to_ascii_lowercase()
}

fn valid_repo(repo: &str) -> bool {
    let mut parts = repo.split('/');
    match (parts.next(), parts.next(), parts.next()) {
        (Some(o), Some(n), None) => {
            !o.is_empty() && !n.is_empty() && !o.contains(' ') && !n.contains(' ')
        }
        _ => false,
    }
}

/// Live GET. The token is sent as a header and never copied into an error.
pub struct UreqGet {
    agent: ureq::Agent,
    token: String,
}

impl UreqGet {
    pub fn new(token: String) -> Self {
        let agent = ureq::Agent::config_builder()
            .tls_config(crate::http_tls::config())
            .timeout_global(Some(std::time::Duration::from_secs(30)))
            .http_status_as_error(false)
            .build()
            .into();
        Self { agent, token }
    }
}

impl HttpGet for UreqGet {
    fn get(&self, url: &str) -> Result<HttpResponse, ReadFailure> {
        let mut resp = self
            .agent
            .get(url)
            .header("Authorization", &format!("Bearer {}", self.token))
            .header("Accept", "application/vnd.github+json")
            .call()
            .map_err(|_| ReadFailure::Transient {
                code: "network".into(),
                retry_in_seconds: 60,
                retry_until: None,
            })?;
        let status = resp.status().as_u16();
        let headers = resp.headers();
        let retry_after_seconds = headers
            .get("retry-after")
            .and_then(|v| v.to_str().ok())
            .and_then(|v| v.parse().ok());
        let remaining_zero = headers
            .get("x-ratelimit-remaining")
            .and_then(|v| v.to_str().ok())
            .is_some_and(|v| v.trim() == "0");
        let rate_reset = headers
            .get("x-ratelimit-reset")
            .and_then(|v| v.to_str().ok())
            .and_then(|v| v.trim().parse().ok());
        let body = resp
            .body_mut()
            .with_config()
            .limit(BODY_LIMIT + 1)
            .read_to_vec()
            .map_err(|_| ReadFailure::Transient {
                code: "network".into(),
                retry_in_seconds: 60,
                retry_until: None,
            })?;
        Ok(HttpResponse {
            status,
            body,
            retry_after_seconds,
            remaining_zero,
            rate_reset,
        })
    }
}
