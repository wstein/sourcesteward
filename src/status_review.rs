//! `stw status --review`: the review state of the open pull requests, read from their comments.
//!
//! One comment read per pull request (`github::reads` list read under the default `Budget`), at most
//! `MAX_PARALLEL` at a time and at most `REVIEW_LIMIT` pull requests per run (Go: the detail limit of 200), in
//! repository order, newest pull request first. The state is the M3 state (`review::evaluate_read`): a read that
//! failed, was denied, was cut by a bound or hit the `--timeout` is `unknown`, never a verdict. Trust: the
//! logins of the comments whose `author_association` is OWNER, MEMBER or COLLABORATOR (the trust rule of Go);
//! the comments of other authors are findings and never decide.
//!
//! Text from the forge (logins, the cut line of a variant) is escaped before it is printed or put into JSON.
use serde_json::{json, Value};

use crate::github::reads::{escape_untrusted, Budget, Comment, Read};
use crate::github::{Client, OpenPr};
use crate::output::jopt;
use crate::repo::{par_map, MAX_PARALLEL};
use crate::review::{self, Evidence, Line, PrRef, ReviewState, Scope, Status, Verdict};
use crate::status::{escape_for_text, RepoStatus};

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
    }
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
    }
    t
}

impl Tally {
    pub fn json(&self) -> Value {
        json!({"states": {"clear": self.clear, "clear_carried": self.clear_carried, "findings": self.findings,
                          "no_review": self.no_review, "unverified_author": self.unverified_author,
                          "unknown": self.unknown},
               "not_read": self.not_read, "limit": REVIEW_LIMIT})
    }

    /// `reviews: 1 clear, 2 no-review, 1 ?, 3 not read`; None without pull requests.
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
        (!parts.is_empty()).then(|| format!("reviews: {}", parts.join(", ")))
    }
}

/// `escape_untrusted` cut at `max` characters.
fn esc_cut(s: &str, max: usize) -> String {
    escape_untrusted(s, max).0
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

/// The notices of one repository: pull requests whose comments could not be read completely and pull requests
/// beyond the limit.
pub fn notices(s: &RepoStatus) -> Vec<String> {
    let mut out = Vec::new();
    if !s.review_read {
        return out;
    }
    let name = escape_for_text(&s.name);
    let bad: Vec<(&OpenPr, &str)> = s
        .prs
        .iter()
        .filter_map(|p| Some((p, p.review.as_ref()?.problem.as_deref()?)))
        .collect();
    if let Some((p, why)) = bad.first() {
        out.push(format!(
            "{name}: review state not read for {} pull request{} (#{}: {})",
            bad.len(),
            if bad.len() == 1 { "" } else { "s" },
            p.number,
            esc_cut(why, 200)
        ));
    }
    let t = tally(&s.prs);
    if t.not_read > 0 {
        out.push(format!(
            "{name}: review state not read for {} pull request{} beyond the limit of {REVIEW_LIMIT} per run",
            t.not_read,
            if t.not_read == 1 { "" } else { "s" }
        ));
    }
    out
}
