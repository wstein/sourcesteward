//! The three reads of `stw lint pr` (M8): the pull request, its commits (100 per page) and its conversation
//! comments (30 per page), each page as raw JSON plus the `Link` header. GET only; redirects are not followed
//! and the token goes only to the configured API base. Errors carry the code of the Go reader as a prefix
//! (`rate-limited: ...`) so that a read failure says what it was.
use std::time::Duration;

use serde_json::Value;

use super::Client;
use crate::error::{Error, Result};

/// Commits per page and the highest commit page the command reads.
pub const COMMITS_PER_PAGE: usize = 100;
pub const COMMITS_MAX_PAGE: usize = 3;
/// Comments per page and the highest comment page the command reads (300 comments).
pub const COMMENTS_PER_PAGE: usize = 30;
pub const COMMENTS_MAX_PAGE: usize = 10;
/// Body limit of one answer.
const BODY_LIMIT: u64 = 1 << 20;

/// One answer: the parsed body and the `Link` header (empty when absent).
pub struct LintPage {
    pub body: Value,
    pub link: String,
}

fn fail(code: &str, message: &str) -> Error {
    Error::github(format!("{code}: {message}"))
}

/// Go: `page-too-large` ("list page exceeds the one MiB body limit") for comment pages, `malformed-response`
/// ("response exceeds the one MiB body limit") for the other reads.
fn too_large(comment_page: bool) -> Error {
    if comment_page {
        fail("page-too-large", "list page exceeds the one MiB body limit")
    } else {
        fail(
            "malformed-response",
            "response exceeds the one MiB body limit",
        )
    }
}

impl Client {
    /// `size_is_too_large`: as in Go, only comment pages report an oversized body as `page-too-large`; the other reads
    /// report `malformed-response`.
    fn lint_get(&self, path: &str, size_is_too_large: bool) -> Result<LintPage> {
        let url = format!("{}{path}", self.base);
        let mut resp = self
            .agent
            .get(&url)
            .config()
            .max_redirects(0)
            .timeout_global(Some(Duration::from_secs(30)))
            .build()
            .header("Authorization", &format!("Bearer {}", self.token))
            .header("Accept", "application/vnd.github+json")
            .header("X-GitHub-Api-Version", "2022-11-28")
            .call()
            .map_err(|_| fail("network", "HTTP read failed"))?;
        let status = resp.status().as_u16();
        let header = |name: &str| {
            resp.headers()
                .get(name)
                .and_then(|v| v.to_str().ok())
                .map(str::to_string)
        };
        if status != 200 {
            let exhausted = header("x-ratelimit-remaining").is_some_and(|v| v.trim() == "0")
                || header("retry-after").is_some();
            return Err(match status {
                401 => fail("unauthorized", "authentication was refused"),
                429 => fail("rate-limited", "rate limit reached"),
                403 if exhausted => fail("rate-limited", "rate limit reached"),
                403 => {
                    if super::mentions_rate_limit(&super::error_body_head(&mut resp)) {
                        fail("rate-limited", "rate limit reached")
                    } else {
                        fail("forbidden", "access was refused")
                    }
                }
                404 => fail("forbidden", "resource not found or not accessible"),
                301 | 308 => fail(
                    "repo-moved",
                    "repository moved; update the repository (redirect not followed)",
                ),
                304 => fail("malformed-response", "unsolicited 304 response"),
                300..=399 => fail("malformed-response", "redirects are not followed"),
                500..=599 => fail("http-5xx", "server error"),
                _ => fail("malformed-response", "unexpected HTTP status"),
            });
        }
        let link = header("link").unwrap_or_default();
        let body = resp
            .body_mut()
            .with_config()
            .limit(BODY_LIMIT + 1)
            .read_to_vec()
            .map_err(|e| {
                if matches!(e, ureq::Error::BodyExceedsLimit(_)) {
                    too_large(size_is_too_large)
                } else {
                    fail("network", "HTTP read failed")
                }
            })?;
        // Go reads max+1 bytes and refuses a longer body: exactly one MiB is accepted, one byte more is not. ureq already
        // refuses at limit + 1 (above), so this check only guards against a change of that behavior.
        if body.len() as u64 > BODY_LIMIT {
            return Err(too_large(size_is_too_large));
        }
        let body: Value = serde_json::from_slice(&body)
            .map_err(|_| fail("malformed-response", "unexpected document shape"))?;
        Ok(LintPage { body, link })
    }

    /// `pulls/N`: title, text, head and commit count.
    pub fn lint_pull(&self, slug: &str, number: u64) -> Result<LintPage> {
        Self::check_slug(slug)?;
        self.lint_get(&format!("/repos/{slug}/pulls/{number}"), false)
    }

    /// One page (1..=3, 100 per page) of `pulls/N/commits`.
    pub fn lint_commits(&self, slug: &str, number: u64, page: usize) -> Result<LintPage> {
        Self::check_slug(slug)?;
        assert!(
            (1..=COMMITS_MAX_PAGE).contains(&page),
            "commit page out of bounds"
        );
        self.lint_get(
            &format!(
                "/repos/{slug}/pulls/{number}/commits?per_page={COMMITS_PER_PAGE}&page={page}"
            ),
            false,
        )
    }

    /// One page (1..=10, 30 per page) of `issues/N/comments`.
    pub fn lint_comments(&self, slug: &str, number: u64, page: usize) -> Result<LintPage> {
        Self::check_slug(slug)?;
        assert!(
            (1..=COMMENTS_MAX_PAGE).contains(&page),
            "comment page out of bounds"
        );
        self.lint_get(
            &format!(
                "/repos/{slug}/issues/{number}/comments?per_page={COMMENTS_PER_PAGE}&page={page}"
            ),
            true,
        )
    }
}
