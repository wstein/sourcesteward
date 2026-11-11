//! `stw status --review`: the review state of the open pull requests, read from their comments.
//!
//! One comment read per pull request (`github::reads` list read under the default `Budget`), at most
//! `MAX_PARALLEL` at a time and at most `REVIEW_LIMIT` pull requests per run (Go: the detail limit of 200), in
//! repository order, newest pull request first. The state is the M3 state (`review::evaluate_read`): a read that
//! failed, was denied, was cut by a bound or hit the `--timeout` is `unknown`, never a verdict. Trust: the
//! logins of the comments whose `author_association` is OWNER, MEMBER or COLLABORATOR (the trust rule of Go);
//! the comments of other authors are findings and never decide.
//!
//! Hints (`clear-not-ready`, `base-not-default`): derived from the pull request list and the state, no further read.
//!
//! Text from the forge (logins, the cut line of a variant) is escaped before it is printed or put into JSON.
use serde_json::{json, Value};

use crate::github::reads::{escape_untrusted, Budget, Comment, Read};
use crate::github::{Client, OpenPr};
use crate::output::jopt;
use crate::repo::{par_map, MAX_PARALLEL};
use crate::review::{self, Evidence, Line, PrRef, ReviewState, Scope, Status, Verdict};
use crate::status::{escape_for_text, Notice, RepoStatus};

/// Hint: CLEAR (or carried) for the head, but the pull request is a draft (Go `clear-not-ready`).
pub const HINT_CLEAR_NOT_READY: &str = "clear-not-ready";
/// Hint: the target branch is not the default branch of the repository (Go `base-not-default`).
pub const HINT_BASE_NOT_DEFAULT: &str = "base-not-default";

/// Pull requests whose comments are read in one run (Go: `--limit` of the pulls cycle, default 200).
pub const REVIEW_LIMIT: usize = 200;

/// Findings shown per pull request in the `--long` text.
const SHOWN_FINDINGS: usize = 5;

/// The review state of one pull request and why its comment read was not complete.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct PrReview {
    pub state: ReviewState,
    /// None for a complete read; otherwise what was wrong (denied, not found, failed, cut, deadline).
    pub problem: Option<String>,
    /// The read was not started because GitHub had answered with a rate limit before (`problem` says so).
    pub skipped: bool,
}

impl PrReview {
    /// The status label of the JSON: `clear`, `clear-carried`, `findings`, `no-review`, `unverified-author`, `unknown`.
    pub fn label(&self) -> &'static str {
        self.state.status.label()
    }

    /// The label of the text: the same, `?` for `unknown`.
    pub fn text_label(&self) -> &'static str {
        match self.state.status {
            Status::Unknown => "?",
            s => s.label(),
        }
    }
}

fn trusted_logins(items: &[Comment]) -> Vec<&str> {
    items
        .iter()
        .filter(|c| matches!(c.association.as_str(), "OWNER" | "MEMBER" | "COLLABORATOR"))
        .map(|c| c.author.as_str())
        .collect()
}

fn read_problem(read: &Read<Comment>) -> Option<String> {
    match read {
        Read::Observed(o) => o.incomplete.clone(),
        Read::Denied { message, .. } => Some(message.clone()),
        Read::NotFound => Some("the comments were not found (HTTP 404)".into()),
        Read::Failed(m) => Some(m.clone()),
    }
}

fn is_hex40(s: &str) -> bool {
    s.len() == 40 && s.bytes().all(|b| matches!(b, b'0'..=b'9' | b'a'..=b'f'))
}

/// The state of one pull request from the answer of its comment read.
pub fn evaluate(number: u64, head: &str, read: &Read<Comment>) -> PrReview {
    let trusted: Vec<&str> = match read {
        Read::Observed(o) => trusted_logins(&o.items),
        _ => vec![],
    };
    PrReview {
        state: review::evaluate_read(Scope::Pr(number), head, read, &trusted),
        problem: read_problem(read),
        skipped: false,
    }
}

/// The result of a read that was not started after a rate limit: `unknown`, never a verdict.
fn skipped(number: u64, head: &str) -> PrReview {
    let mut r = evaluate(number, head, &Read::Failed(RATE_LIMIT_SKIP.into()));
    r.skipped = true;
    r
}

const RATE_LIMIT_SKIP: &str =
    "not read: GitHub had reached the rate limit, no further comment read was started";

/// The hints of one pull request, in the order of Go (`reviewstate.Hints`), from data of the pull request list and its
/// review state; no further read. `clear-not-ready`: the state is `clear` or `clear-carried` and the pull request is a
/// draft. `base-not-default`: base and default branch are both known and differ (exact comparison, as in Go).
pub fn hints(p: &OpenPr) -> Vec<&'static str> {
    let mut v = Vec::new();
    let clear = p
        .review
        .as_ref()
        .is_some_and(|r| matches!(r.state.status, Status::Clear | Status::ClearCarried));
    if clear && p.draft {
        v.push(HINT_CLEAR_NOT_READY);
    }
    if !p.base.is_empty() && !p.default_branch.is_empty() && p.base != p.default_branch {
        v.push(HINT_BASE_NOT_DEFAULT);
    }
    v
}

/// The `--long` explanation of each hint, one line each; every branch name from the forge is escaped and cut.
pub fn hint_lines(p: &OpenPr) -> Vec<String> {
    hints(p)
        .into_iter()
        .map(|h| match h {
            HINT_CLEAR_NOT_READY => {
                format!(
                    "hint {h}: the review is CLEAR for the head, but the pull request is a draft"
                )
            }
            _ => format!(
                "hint {h}: the base is {}, the default branch is {}",
                esc_cut(&p.base, 100),
                esc_cut(&p.default_branch, 100)
            ),
        })
        .collect()
}

struct Job {
    repo: usize,
    pr: usize,
    slug: String,
    number: u64,
    head: String,
}

/// Read the review state of the open pull requests of `list` (the repositories whose pull request list was read).
/// Pull requests beyond `REVIEW_LIMIT` keep `review: None` (not read).
pub fn read_reviews(list: Vec<RepoStatus>, gh: &Client) -> Vec<RepoStatus> {
    let mut list = list;
    let mut jobs: Vec<Job> = Vec::new();
    for (ri, s) in list.iter_mut().enumerate() {
        let Some(slug) = s.slug.clone().filter(|_| s.error.is_none() && s.pr_read) else {
            continue;
        };
        s.review_read = true;
        for (pi, p) in s.prs.iter().enumerate() {
            if jobs.len() < REVIEW_LIMIT {
                jobs.push(Job {
                    repo: ri,
                    pr: pi,
                    slug: slug.clone(),
                    number: p.number,
                    head: p.head_sha.clone(),
                });
            }
        }
    }
    let results = par_map(&jobs, MAX_PARALLEL, |j| read_one(gh, j));
    for (j, r) in jobs.iter().zip(results) {
        list[j.repo].prs[j.pr].review = Some(r);
    }
    list
}

fn read_one(gh: &Client, j: &Job) -> PrReview {
    // after a rate limit (any repository, any source) no further read starts; reads in flight end on their own
    if gh.rate_limited() {
        return skipped(j.number, &j.head);
    }
    if !is_hex40(&j.head) {
        return evaluate(
            j.number,
            &j.head,
            &Read::Failed("the head of the pull request is not a full lower-case commit id".into()),
        );
    }
    let read = match gh.issue_comments(&j.slug, j.number, &Budget::default()) {
        Ok(r) => r,
        Err(e) => Read::Failed(e.to_string()),
    };
    evaluate(j.number, &j.head, &read)
}

/// The counts of the review states of the pull requests of one repository.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct Tally {
    pub clear: usize,
    pub clear_carried: usize,
    pub findings: usize,
    pub no_review: usize,
    pub unverified_author: usize,
    pub unknown: usize,
    pub not_read: usize,
    /// Pull requests with the hint `clear-not-ready`.
    pub clear_not_ready: usize,
    /// Pull requests with the hint `base-not-default`.
    pub base_not_default: usize,
}

pub fn tally(prs: &[OpenPr]) -> Tally {
    let mut t = Tally::default();
    for p in prs {
        match p.review.as_ref().map(|r| r.state.status) {
            Some(Status::Clear) => t.clear += 1,
            Some(Status::ClearCarried) => t.clear_carried += 1,
            Some(Status::Findings) => t.findings += 1,
            Some(Status::NoReview) => t.no_review += 1,
            Some(Status::UnverifiedAuthor) => t.unverified_author += 1,
            Some(Status::Unknown) => t.unknown += 1,
            None => t.not_read += 1,
        }
        for h in hints(p) {
            match h {
                HINT_CLEAR_NOT_READY => t.clear_not_ready += 1,
                _ => t.base_not_default += 1,
            }
        }
    }
    t
}

impl Tally {
    pub fn json(&self) -> Value {
        json!({"states": {"clear": self.clear, "clear_carried": self.clear_carried, "findings": self.findings,
                          "no_review": self.no_review, "unverified_author": self.unverified_author,
                          "unknown": self.unknown},
               "hints": {"clear_not_ready": self.clear_not_ready, "base_not_default": self.base_not_default},
               "not_read": self.not_read, "limit": REVIEW_LIMIT})
    }

    /// `reviews: 1 clear, 2 no-review, 1 ?, 3 not read (hints: 1 clear-not-ready)`; None without pull requests.
    pub fn text(&self) -> Option<String> {
        let parts: Vec<String> = [
            (self.clear, "clear"),
            (self.clear_carried, "clear-carried"),
            (self.findings, "findings"),
            (self.no_review, "no-review"),
            (self.unverified_author, "unverified-author"),
            (self.unknown, "?"),
            (self.not_read, "not read"),
        ]
        .iter()
        .filter(|(n, _)| *n > 0)
        .map(|(n, l)| format!("{n} {l}"))
        .collect();
        let hints: Vec<String> = [
            (self.clear_not_ready, HINT_CLEAR_NOT_READY),
            (self.base_not_default, HINT_BASE_NOT_DEFAULT),
        ]
        .iter()
        .filter(|(n, _)| *n > 0)
        .map(|(n, l)| format!("{n} {l}"))
        .collect();
        let hints = if hints.is_empty() {
            String::new()
        } else {
            format!(" (hints: {})", hints.join(", "))
        };
        (!parts.is_empty()).then(|| format!("reviews: {}{hints}", parts.join(", ")))
    }
}

/// `escape_untrusted` cut at `max` characters.
fn esc_cut(s: &str, max: usize) -> String {
    escape_untrusted(s, max).0
}

/// A branch name for the JSON: escaped and cut, null when empty.
pub fn esc_opt_cut(s: &str, max: usize) -> Value {
    if s.is_empty() {
        Value::Null
    } else {
        Value::String(esc_cut(s, max))
    }
}

/// The JSON of `pull_requests.open[].hints`: the hint names, null when the review state was not asked for.
pub fn hints_json(p: &OpenPr, review_read: bool) -> Value {
    if review_read {
        json!(hints(p))
    } else {
        Value::Null
    }
}

/// The JSON of `pull_requests.open[].review_state`: the M3 state (version 1) with the text of the forge escaped and
/// cut, and `read_error` (null for a complete read).
pub fn state_json(r: &PrReview) -> Value {
    let mut v = r.state.to_json();
    if let Some(Value::Array(fs)) = v.get_mut("findings") {
        for f in fs {
            if let Some(d) = f["detail"].as_str().map(|d| esc_cut(d, 120)) {
                f["detail"] = Value::String(d);
            }
        }
    }
    if let Value::Object(m) = &mut v {
        m.insert(
            "read_error".into(),
            jopt(r.problem.as_deref().map(|p| esc_cut(p, 300))),
        );
    }
    v
}

fn pr_text(p: PrRef) -> String {
    match p {
        PrRef::Number(n) => n.to_string(),
        PrRef::None => "none".into(),
    }
}

/// The parsed line as it reads in the comment (every field is validated hex or a number).
fn line_text(l: &Line) -> String {
    match l {
        Line::Review {
            verdict,
            head,
            patch_id,
            pr,
        } => format!(
            "Review: {} head={head} patch-id={patch_id} pr={}",
            match verdict {
                Verdict::Clear => "CLEAR",
                Verdict::Findings => "FINDINGS",
            },
            pr_text(*pr)
        ),
        Line::Carried {
            from,
            to,
            patch_id,
            pr,
        } => format!(
            "Carried-over: from={from} to={to} patch-id={patch_id} pr={}",
            pr_text(*pr)
        ),
    }
}

fn evidence_text(e: &Evidence) -> String {
    format!(
        "comment {} of {} at {}{}: {}",
        e.comment_id,
        esc_cut(&e.author, 100),
        esc_cut(&e.created_at, 40),
        if e.edited { ", edited" } else { "" },
        esc_cut(&line_text(&e.line), 200)
    )
}

/// The evidence of a state, one line each (`--long`): the decisive comment with its line, the chain of a carry-over,
/// the findings (at most `SHOWN_FINDINGS`) and why a state is `?`.
pub fn evidence_lines(r: &PrReview) -> Vec<String> {
    let st = &r.state;
    let mut v = Vec::new();
    if let Some(d) = &st.decisive {
        v.push(format!("decided by {}", evidence_text(d)));
    }
    if st.chain.len() > 1 {
        v.push(format!(
            "carried through {} link{}, from {}",
            st.chain.len() - 1,
            if st.chain.len() == 2 { "" } else { "s" },
            evidence_text(&st.chain[0])
        ));
    }
    for f in st.findings.iter().take(SHOWN_FINDINGS) {
        let detail = if f.detail.is_empty() {
            String::new()
        } else {
            format!(": {}", esc_cut(&f.detail, 120))
        };
        v.push(format!(
            "finding {}: comment {} of {}{detail}",
            f.kind.label(),
            f.comment_id,
            esc_cut(&f.author, 100)
        ));
    }
    if st.findings.len() > SHOWN_FINDINGS {
        v.push(format!(
            "and {} more findings",
            st.findings.len() - SHOWN_FINDINGS
        ));
    }
    for why in &st.reasons {
        v.push(format!("unknown because: {}", esc_cut(why, 200)));
    }
    v
}

/// The notice of the whole run for the reads that were not started after a rate limit.
pub fn rate_limit_notice(list: &[RepoStatus]) -> Option<Notice> {
    let n = list
        .iter()
        .flat_map(|s| &s.prs)
        .filter(|p| p.review.as_ref().is_some_and(|r| r.skipped))
        .count();
    (n > 0).then(|| Notice {
        code: "rate-limited",
        source: "reviews",
        repos: Vec::new(),
        message: format!(
            "rate limit reached, {n} read{} skipped (review state ?)",
            if n == 1 { "" } else { "s" }
        ),
        count: n,
    })
}

/// The notices of one repository: pull requests whose comments could not be read completely and pull requests
/// beyond the limit.
pub fn notices(s: &RepoStatus) -> Vec<Notice> {
    let mut out = Vec::new();
    if !s.review_read {
        return out;
    }
    let name = escape_for_text(&s.name);
    let bad: Vec<(&OpenPr, &str)> = s
        .prs
        .iter()
        .filter_map(|p| {
            let r = p.review.as_ref()?;
            (!r.skipped).then_some((p, r.problem.as_deref()?))
        })
        .collect();
    if let Some((p, why)) = bad.first() {
        out.push(Notice {
            code: "review-not-read",
            source: "reviews",
            repos: vec![name.clone()],
            message: format!(
                "{name}: review state not read for {} pull request{} (#{}: {})",
                bad.len(),
                if bad.len() == 1 { "" } else { "s" },
                p.number,
                esc_cut(why, 200)
            ),
            count: bad.len(),
        });
    }
    let t = tally(&s.prs);
    if t.not_read > 0 {
        out.push(Notice {
            code: "review-limit",
            source: "reviews",
            repos: vec![name.clone()],
            message: format!(
                "{name}: review state not read for {} pull request{} beyond the limit of {REVIEW_LIMIT} per run",
                t.not_read,
                if t.not_read == 1 { "" } else { "s" }
            ),
            count: t.not_read,
        });
    }
    out
}
