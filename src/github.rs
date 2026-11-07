//! GitHub access over the REST API (ureq, blocking). No `gh` binary. Tokens come from `auth`.
use std::time::Duration;

use serde_json::Value;

use crate::auth::{self, TokenSource};
use crate::error::{Error, Result};

pub mod reads;

const API: &str = "https://api.github.com";
/// Environment variable that points the REST calls elsewhere (GitHub Enterprise, tests); no trailing slash.
pub const API_URL_ENV: &str = "STW_GITHUB_API_URL";

/// One open pull request as shown by `stw status`.
#[derive(Clone, Debug)]
pub struct OpenPr {
    pub number: u64,
    pub title: String,
    pub head: String,
    pub draft: bool,
    pub author: String,
    /// REST has no `reviewDecision`; always None (documented gap).
    pub review: Option<String>,
    /// From `mergeable_state` of the single-PR endpoint, upper-cased like gh's `mergeStateStatus`.
    pub merge_state: Option<String>,
}

/// One PR of any state, for the merged-head proof of `stw tidy plan`.
#[derive(Clone, Debug)]
pub struct AnyPr {
    pub head_ref: String,
    pub head_sha: String,
    pub number: u64,
    /// `OPEN`, `MERGED` or `CLOSED` (gh vocabulary).
    pub state: &'static str,
}

/// Whether GitHub is available for this run, and the reason when it is not.
pub struct GithubState {
    pub client: Option<Client>,
    /// Why pull requests are not read (offline, no token); None when they are.
    pub note: Option<String>,
}

impl GithubState {
    pub fn discover(offline: bool) -> GithubState {
        if offline {
            return GithubState {
                client: None,
                note: Some("pull requests not read: --offline".into()),
            };
        }
        match Client::from_env() {
            Some(c) => GithubState {
                client: Some(c),
                note: None,
            },
            None => GithubState {
                client: None,
                note: Some(
                    "pull requests not read: no GitHub token found (looked at GH_TOKEN, GITHUB_TOKEN, \
                     ~/.config/gh/hosts.yml and the macOS keychain); set GH_TOKEN to a token that can read \
                     the repositories, or pass --offline"
                        .into(),
                ),
            },
        }
    }

    pub fn client(&self) -> Option<&Client> {
        self.client.as_ref()
    }
}

pub struct Client {
    agent: ureq::Agent,
    token: String,
    base: String,
    pub source: TokenSource,
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
        }
    }

    /// Credentials for git-over-HTTPS against github.com (handed to gix's transport, never printed).
    pub fn basic_auth(&self) -> (String, String) {
        ("x-access-token".to_string(), self.token.clone())
    }

    /// GET returning the JSON body, or `None` for 404 (the one status callers treat as an answer).
    fn get_optional(&self, url: &str) -> Result<Option<Value>> {
        let path = url.trim_start_matches(self.base.as_str());
        let mut resp = self
            .agent
            .get(url)
            .header("Authorization", &format!("Bearer {}", self.token))
            .header("Accept", "application/vnd.github+json")
            .header("X-GitHub-Api-Version", "2022-11-28")
            .call()
            .map_err(|e| Error::github(format!("GitHub API unreachable for {path}: {e}")))?;
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
        let list = self.get(&format!("{api}/repos/{slug}/pulls?state=open&per_page=100"))?;
        let items = list
            .as_array()
            .ok_or_else(|| Error::github("GitHub API sent an unexpected pulls response"))?;
        let mut prs: Vec<OpenPr> = items
            .iter()
            .map(|p| OpenPr {
                number: p["number"].as_u64().unwrap_or(0),
                title: p["title"].as_str().unwrap_or("").to_string(),
                head: p["head"]["ref"].as_str().unwrap_or("").to_string(),
                draft: p["draft"].as_bool().unwrap_or(false),
                author: p["user"]["login"].as_str().unwrap_or("").to_string(),
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

    /// SHA of `refs/heads/<branch>` on the remote (read-only replacement of `git ls-remote`).
    pub fn remote_branch_sha(&self, slug: &str, branch: &str) -> Result<Option<String>> {
        let api = &self.base;
        let v = self.get_optional(&format!("{api}/repos/{slug}/git/ref/heads/{branch}"))?;
        Ok(v.and_then(|v| v["object"]["sha"].as_str().map(str::to_string)))
    }
}
