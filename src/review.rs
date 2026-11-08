//! Review state from pull request comments (M3). Pure: no network, no git, no clock.
//!
//! A review verdict is a claim of a comment, never proof. This module checks what the data allows: the strict
//! grammar of the first line, the head binding, the order of comments, carry-over chains and the author. Equal
//! patch-ids of a carry-over are not recomputed here (that is M4); the declared patch-ids are kept as evidence.
//!
//! Grammar (first line of the comment, nothing else on it; fixed keys and order; single spaces; no Markdown, tab or CR):
//! `Review: <CLEAR|FINDINGS> head=<40 hex> patch-id=<40 hex> pr=<n|none>`
//! `Carried-over: from=<40 hex> to=<40 hex> patch-id=<40 hex> pr=<n|none>`
//! Hex is lower case. `n` is a decimal number from 1 without leading zeros. `none` marks a local review (no pull
//! request); the shared dotagents grammar has no `none` yet.
use serde_json::{json, Value};

use crate::github::reads::{Comment, Read};

/// Longest carry-over chain that is followed (links from the CLEAR to the head).
pub const MAX_LINKS: usize = 16;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Verdict {
    Clear,
    Findings,
}

/// The `pr=` field of a line.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum PrRef {
    Number(u64),
    /// `pr=none`: a local review.
    None,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Line {
    Review {
        verdict: Verdict,
        head: String,
        patch_id: String,
        pr: PrRef,
    },
    Carried {
        from: String,
        to: String,
        patch_id: String,
        pr: PrRef,
    },
}

/// Classification of the first line of a comment.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Parsed {
    Line(Line),
    /// Talks about a review but breaks the grammar. Never accepted; reported as `unparsed-review-variant`.
    Variant(String),
    NotReview,
}

fn is_hex40(s: &str) -> bool {
    s.len() == 40 && s.bytes().all(|b| matches!(b, b'0'..=b'9' | b'a'..=b'f'))
}

fn field<'a>(tok: &'a str, key: &str) -> Option<&'a str> {
    tok.strip_prefix(key)?.strip_prefix('=')
}

fn hex_field(tok: &str, key: &str) -> Option<String> {
    field(tok, key).filter(|v| is_hex40(v)).map(str::to_string)
}

fn pr_field(tok: &str) -> Option<PrRef> {
    let v = field(tok, "pr")?;
    if v == "none" {
        return Some(PrRef::None);
    }
    let canonical = !v.is_empty() && v.bytes().all(|b| b.is_ascii_digit()) && !v.starts_with('0');
    if !canonical {
        return None;
    }
    v.parse().ok().map(PrRef::Number)
}

/// Strict parse of one line (no line break in it). None for anything that is not exactly a review line.
pub fn parse_strict(line: &str) -> Option<Line> {
    let t: Vec<&str> = line.split(' ').collect();
    if t.len() != 5 {
        return None;
    }
    match t[0] {
        "Review:" => {
            let verdict = match t[1] {
                "CLEAR" => Verdict::Clear,
                "FINDINGS" => Verdict::Findings,
                _ => return None,
            };
            Some(Line::Review {
                verdict,
                head: hex_field(t[2], "head")?,
                patch_id: hex_field(t[3], "patch-id")?,
                pr: pr_field(t[4])?,
            })
        }
        "Carried-over:" => Some(Line::Carried {
            from: hex_field(t[1], "from")?,
            to: hex_field(t[2], "to")?,
            patch_id: hex_field(t[3], "patch-id")?,
            pr: pr_field(t[4])?,
        }),
        _ => None,
    }
}

/// True for a line that reads like an attempt at a review line.
fn looks_like_review(line: &str) -> bool {
    let lower = line.to_ascii_lowercase();
    let body = lower.trim_start_matches([' ', '\t', '`', '>', '*', '_', '#', '-', '+']);
    let word_end = |w: &str| {
        body.strip_prefix(w)
            .is_some_and(|r| !r.starts_with(|c: char| c.is_ascii_alphanumeric()))
    };
    let prefixed = word_end("review") || body.starts_with("carried");
    let keys = ["head=", "patch-id=", "from=", "pr="]
        .iter()
        .any(|k| lower.contains(k));
    let verdict_word = body
        .split(|c: char| !c.is_ascii_alphanumeric())
        .filter(|w| !w.is_empty())
        .nth(1)
        .is_some_and(|w| w == "clear" || w == "findings");
    (prefixed && (keys || verdict_word))
        || (lower.contains("patch-id=") && (lower.contains("head=") || lower.contains("from=")))
}

/// Classify the first line of a comment body (up to the first line feed). A CR at its end (CRLF), a cut line
/// or any other deviation from the grammar makes a review-like line a variant.
pub fn parse_body(body: &str, truncated: bool) -> Parsed {
    let (line, has_break) = match body.split_once('\n') {
        Some((l, _)) => (l, true),
        None => (body, false),
    };
    if !(truncated && !has_break) {
        if let Some(l) = parse_strict(line) {
            return Parsed::Line(l);
        }
    }
    if looks_like_review(line) {
        let shown: String = line.chars().take(200).collect();
        return Parsed::Variant(shown);
    }
    Parsed::NotReview
}

/// A pull request comment with a raw (unescaped) body.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct ReviewComment {
    pub id: u64,
    pub author: String,
    pub created_at: String,
    pub updated_at: String,
    pub body: String,
    pub body_truncated: bool,
}

/// Undo `escape_untrusted`. None for a malformed escape.
pub fn unescape(s: &str) -> Option<String> {
    let mut out = String::with_capacity(s.len());
    let mut it = s.chars();
    while let Some(c) = it.next() {
        if c != '\\' {
            out.push(c);
            continue;
        }
        match it.next()? {
            '\\' => out.push('\\'),
            'n' => out.push('\n'),
            'r' => out.push('\r'),
            't' => out.push('\t'),
            'u' => {
                if it.next()? != '{' {
                    return None;
                }
                let mut hex = String::new();
                loop {
                    match it.next()? {
                        '}' => break,
                        h if h.is_ascii_hexdigit() && hex.len() < 6 => hex.push(h),
                        _ => return None,
                    }
                }
                out.push(char::from_u32(u32::from_str_radix(&hex, 16).ok()?)?);
            }
            _ => return None,
        }
    }
    Some(out)
}

impl ReviewComment {
    /// From a read comment (its body is escaped). None when the escape is malformed.
    pub fn from_comment(c: &Comment) -> Option<ReviewComment> {
        Some(ReviewComment {
            id: c.id,
            author: c.author.clone(),
            created_at: c.created_at.clone(),
            updated_at: c.updated_at.clone(),
            body: unescape(&c.body)?,
            body_truncated: c.body_truncated,
        })
    }
}

/// What a review state is about.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Scope {
    /// Pull request number: lines must say `pr=<n>`.
    Pr(u64),
    /// Local review: lines must say `pr=none`.
    Local,
}

impl Scope {
    fn accepts(self, pr: PrRef) -> bool {
        match (self, pr) {
            (Scope::Pr(n), PrRef::Number(m)) => n == m,
            (Scope::Local, PrRef::None) => true,
            _ => false,
        }
    }
    fn label(self) -> String {
        match self {
            Scope::Pr(n) => n.to_string(),
            Scope::Local => "none".into(),
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Status {
    /// CLEAR of the head by a trusted author.
    Clear,
    /// CLEAR reached through a valid carry-over chain.
    ClearCarried,
    Findings,
    /// Complete read, no applicable verdict for the head.
    NoReview,
    /// Applicable lines exist but only from authors that are not trusted.
    UnverifiedAuthor,
    /// Evidence is missing, partial, unreadable or ambiguous. Never to be read as "no review" or CLEAR.
    Unknown,
}

impl Status {
    pub fn label(self) -> &'static str {
        match self {
            Status::Clear => "clear",
            Status::ClearCarried => "clear-carried",
            Status::Findings => "findings",
            Status::NoReview => "no-review",
            Status::UnverifiedAuthor => "unverified-author",
            Status::Unknown => "unknown",
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum FindingKind {
    UnparsedReviewVariant,
    WrongPr,
    UntrustedAuthor,
    InvalidCarryChain,
    StaleHead,
    BadTimestamp,
    UnreadableBody,
}

impl FindingKind {
    pub fn label(self) -> &'static str {
        match self {
            FindingKind::UnparsedReviewVariant => "unparsed-review-variant",
            FindingKind::WrongPr => "wrong-pr",
            FindingKind::UntrustedAuthor => "untrusted-author",
            FindingKind::InvalidCarryChain => "invalid-carry-chain",
            FindingKind::StaleHead => "stale-head",
            FindingKind::BadTimestamp => "bad-timestamp",
            FindingKind::UnreadableBody => "unreadable-body",
        }
    }
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Finding {
    pub kind: FindingKind,
    pub comment_id: u64,
    pub author: String,
    pub detail: String,
}

/// A comment that carries a line, with its provenance.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Evidence {
    pub comment_id: u64,
    pub author: String,
    pub created_at: String,
    pub updated_at: String,
    /// `updated_at` differs from `created_at`: the comment was edited after it was written.
    pub edited: bool,
    pub line: Line,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct ReviewState {
    pub scope: Scope,
    pub head: String,
    pub status: Status,
    /// The comment that decides the status (the last carry-over for `ClearCarried`).
    pub decisive: Option<Evidence>,
    /// For `ClearCarried`: the CLEAR review first, then each carry-over up to the decisive one.
    pub chain: Vec<Evidence>,
    pub findings: Vec<Finding>,
    /// Why the status is `Unknown`; empty otherwise.
    pub reasons: Vec<String>,
}

impl ReviewState {
    /// Stable JSON for a future output field; not part of `stw status` yet.
    pub fn to_json(&self) -> Value {
        let ev = |e: &Evidence| {
            let (kind, fields) = match &e.line {
                Line::Review {
                    verdict,
                    head,
                    patch_id,
                    pr,
                } => (
                    "review",
                    json!({"verdict": match verdict { Verdict::Clear => "CLEAR", Verdict::Findings => "FINDINGS" },
                           "head": head, "patch_id": patch_id, "pr": pr_json(*pr)}),
                ),
                Line::Carried {
                    from,
                    to,
                    patch_id,
                    pr,
                } => (
                    "carried-over",
                    json!({"from": from, "to": to, "patch_id": patch_id, "pr": pr_json(*pr)}),
                ),
            };
            json!({"comment_id": e.comment_id, "author": e.author, "created_at": e.created_at,
                   "updated_at": e.updated_at, "edited": e.edited, "kind": kind, "line": fields})
        };
        json!({
            "version": 1,
            "pr": self.scope.label(),
            "head": self.head,
            "status": self.status.label(),
            "decisive": self.decisive.as_ref().map(ev),
            "chain": self.chain.iter().map(ev).collect::<Vec<_>>(),
            "findings": self.findings.iter().map(|f| json!({
                "kind": f.kind.label(), "comment_id": f.comment_id, "author": f.author, "detail": f.detail})).collect::<Vec<_>>(),
            "reasons": self.reasons,
        })
    }
}

fn pr_json(p: PrRef) -> Value {
    match p {
        PrRef::Number(n) => json!(n),
        PrRef::None => json!("none"),
    }
}

/// `YYYY-MM-DDTHH:MM:SSZ`, the only form GitHub sends; compares correctly as a string.
fn valid_stamp(s: &str) -> bool {
    let b = s.as_bytes();
    b.len() == 20
        && b.iter().enumerate().all(|(i, c)| match i {
            4 | 7 => *c == b'-',
            10 => *c == b'T',
            13 | 16 => *c == b':',
            19 => *c == b'Z',
            _ => c.is_ascii_digit(),
        })
}

struct Entry {
    ev: Evidence,
    /// A trusted line for another pull request (or `pr=none`): not evidence, but it may name the head.
    foreign: bool,
}

impl Entry {
    fn names_head(&self, head: &str) -> bool {
        match &self.ev.line {
            Line::Review { head: h, .. } => h == head,
            Line::Carried { to, .. } => to == head,
        }
    }
}

fn is_trusted(author: &str, trusted: &[&str]) -> bool {
    !author.is_empty() && trusted.iter().any(|t| t.eq_ignore_ascii_case(author))
}

/// A valid carry-over chain: position of the CLEAR review, the positions of the carry-overs after it, and the
/// heads the chain has cleared with the position of the line that cleared each (root head first).
#[derive(Clone)]
struct Root {
    root: usize,
    links: Vec<usize>,
    members: Vec<(String, usize)>,
}

struct Chains<'a> {
    e: &'a [Entry],
    memo: Vec<Option<Result<Root, &'static str>>>,
    /// Position of the last `FINDINGS` review per head (any pr).
    last_findings: &'a std::collections::HashMap<String, usize>,
    /// Positions of the lines that name a head (a review of it or a carry-over to it), ascending.
    names: &'a std::collections::HashMap<String, Vec<usize>>,
}

const NO_SOURCE: &str = "from is not a CLEAR or a valid carry-over before this comment";
const REPEATED_HEAD: &str = "repeated-head: the chain returns to a head it already cleared";
const MEMBER_FINDINGS: &str =
    "member-findings: a head of the chain got FINDINGS after it was cleared";
const PATCH_ID_MISMATCH: &str =
    "patch-id-mismatch: the declared patch-id differs from the patch-id of the CLEAR at the root";

impl Chains<'_> {
    /// All results, in position order, so that `compute` only ever finds the results of earlier positions
    /// in the memo (no recursion, however long the thread).
    fn fill(&mut self) {
        for pos in 0..self.e.len() {
            if !self.e[pos].foreign && matches!(self.e[pos].ev.line, Line::Carried { .. }) {
                self.valid(pos).ok();
            }
        }
    }

    /// Valid chain for the carry-over at `pos`: the latest line before it that names `from` must be a CLEAR
    /// review of it or a valid carry-over to it, and every link declares the patch-id of the root CLEAR.
    fn valid(&mut self, pos: usize) -> Result<Root, &'static str> {
        if let Some(m) = &self.memo[pos] {
            return m.clone();
        }
        let r = self.compute(pos);
        self.memo[pos] = Some(r.clone());
        r
    }

    /// True when a `FINDINGS` review of the head of `member` comes after the line that cleared it.
    fn spoiled(&self, member: &(String, usize)) -> bool {
        self.last_findings
            .get(&member.0)
            .is_some_and(|&f| f > member.1)
    }

    fn patch_id(&self, pos: usize) -> &str {
        match &self.e[pos].ev.line {
            Line::Review { patch_id, .. } | Line::Carried { patch_id, .. } => patch_id,
        }
    }

    fn compute(&mut self, pos: usize) -> Result<Root, &'static str> {
        let Line::Carried {
            from, to, patch_id, ..
        } = self.e[pos].ev.line.clone()
        else {
            return Err(NO_SOURCE);
        };
        if from == to {
            return Err(NO_SOURCE);
        }
        let names = self.names;
        let before = names.get(&from).map_or(&[][..], |v| &v[..]);
        for &q in before.iter().rev().filter(|&&q| q < pos) {
            if self.e[q].foreign {
                return Err(NO_SOURCE);
            }
            match &self.e[q].ev.line {
                Line::Review { verdict, head, .. } if *head == from => {
                    if *verdict != Verdict::Clear {
                        return Err(NO_SOURCE);
                    }
                    if self.patch_id(q) != patch_id {
                        return Err(PATCH_ID_MISMATCH);
                    }
                    let first = (from, q);
                    if self.spoiled(&first) {
                        return Err(MEMBER_FINDINGS);
                    }
                    return Ok(Root {
                        root: q,
                        links: vec![pos],
                        members: vec![first, (to, pos)],
                    });
                }
                Line::Carried { to: t, .. } if *t == from => {
                    if let Ok(mut src) = self.valid(q) {
                        if self.patch_id(src.root) != patch_id {
                            return Err(PATCH_ID_MISMATCH);
                        }
                        if src.members.iter().any(|m| m.0 == to) {
                            return Err(REPEATED_HEAD);
                        }
                        if src.members.iter().any(|m| self.spoiled(m)) {
                            return Err(MEMBER_FINDINGS);
                        }
                        src.links.push(pos);
                        src.members.push((to, pos));
                        return if src.links.len() <= MAX_LINKS {
                            Ok(src)
                        } else {
                            Err(NO_SOURCE)
                        };
                    }
                }
                _ => {}
            }
        }
        Err(NO_SOURCE)
    }
}

fn unknown(scope: Scope, head: &str, findings: Vec<Finding>, reasons: Vec<String>) -> ReviewState {
    ReviewState {
        scope,
        head: head.to_string(),
        status: Status::Unknown,
        decisive: None,
        chain: vec![],
        findings,
        reasons,
    }
}

/// Review state of `head` from a complete list of comments. `trusted` lists the logins whose lines count
/// (case-insensitive); an empty list trusts nobody.
pub fn evaluate(
    scope: Scope,
    head: &str,
    comments: &[ReviewComment],
    trusted: &[&str],
) -> ReviewState {
    if !is_hex40(head) {
        return unknown(
            scope,
            head,
            vec![],
            vec!["head is not 40 lower-case hex".into()],
        );
    }
    let mut findings = Vec::new();
    let mut reasons = Vec::new();
    let mut strict: Vec<Entry> = Vec::new();
    let mut untrusted_relevant = false;
    // Trusted comments with a broken review line: (created_at, id, stamp valid).
    let mut variants: Vec<(String, u64, bool)> = Vec::new();
    let find = |kind, c: &ReviewComment, detail: String| Finding {
        kind,
        comment_id: c.id,
        author: c.author.clone(),
        detail,
    };
    for c in comments {
        let is_trusted = is_trusted(&c.author, trusted);
        match parse_body(&c.body, c.body_truncated) {
            Parsed::NotReview => {}
            Parsed::Variant(shown) => {
                findings.push(find(FindingKind::UnparsedReviewVariant, c, shown));
                if is_trusted {
                    variants.push((c.created_at.clone(), c.id, valid_stamp(&c.created_at)));
                }
            }
            Parsed::Line(line) => {
                let pr = match &line {
                    Line::Review { pr, .. } | Line::Carried { pr, .. } => *pr,
                };
                let foreign = !scope.accepts(pr);
                if foreign {
                    let d = match pr {
                        PrRef::Number(n) => format!("pr={n}"),
                        PrRef::None => "pr=none".into(),
                    };
                    findings.push(find(FindingKind::WrongPr, c, d));
                    if !is_trusted {
                        continue;
                    }
                }
                let ev = Evidence {
                    comment_id: c.id,
                    author: c.author.clone(),
                    created_at: c.created_at.clone(),
                    updated_at: c.updated_at.clone(),
                    edited: c.updated_at != c.created_at,
                    line,
                };
                let entry = Entry { ev, foreign };
                if !is_trusted {
                    findings.push(find(FindingKind::UntrustedAuthor, c, String::new()));
                    untrusted_relevant |= entry.names_head(head);
                    continue;
                }
                if !valid_stamp(&c.created_at) {
                    findings.push(find(FindingKind::BadTimestamp, c, c.created_at.clone()));
                    reasons.push(format!("comment {} has no usable created_at", c.id));
                    continue;
                }
                strict.push(entry);
            }
        }
    }
    strict.sort_by(|a, b| {
        (&a.ev.created_at, a.ev.comment_id).cmp(&(&b.ev.created_at, b.ev.comment_id))
    });

    let mut last_findings = std::collections::HashMap::new();
    for (pos, e) in strict.iter().enumerate() {
        if let Line::Review {
            verdict: Verdict::Findings,
            head,
            ..
        } = &e.ev.line
        {
            last_findings.insert(head.clone(), pos);
        }
    }
    let mut names: std::collections::HashMap<String, Vec<usize>> = std::collections::HashMap::new();
    for (pos, e) in strict.iter().enumerate() {
        let h = match &e.ev.line {
            Line::Review { head, .. } => head,
            Line::Carried { to, .. } => to,
        };
        names.entry(h.clone()).or_default().push(pos);
    }
    let mut chains = Chains {
        names: &names,
        last_findings: &last_findings,
        memo: vec![None; strict.len()],
        e: &strict,
    };
    let mut decisive: Option<(usize, Option<Root>)> = None;
    chains.fill();
    // Positions of trusted lines for another pr that name the head.
    let mut foreign_head: Vec<usize> = Vec::new();
    for (pos, entry) in strict.iter().enumerate() {
        if entry.foreign {
            if entry.names_head(head) {
                foreign_head.push(pos);
            }
            continue;
        }
        match &entry.ev.line {
            Line::Review { head: h, .. } if h == head => decisive = Some((pos, None)),
            Line::Carried { to, .. } if to == head => match chains.valid(pos) {
                Ok(root) => decisive = Some((pos, Some(root))),
                Err(why) => findings.push(Finding {
                    kind: FindingKind::InvalidCarryChain,
                    comment_id: entry.ev.comment_id,
                    author: entry.ev.author.clone(),
                    detail: why.into(),
                }),
            },
            _ => {}
        }
    }

    let mut res = unknown(scope, head, findings, vec![]);
    let decisive_pos = decisive.as_ref().map(|d| d.0);
    match decisive {
        None => {
            res.status = if untrusted_relevant {
                Status::UnverifiedAuthor
            } else {
                Status::NoReview
            };
        }
        Some((pos, root)) => {
            res.decisive = Some(strict[pos].ev.clone());
            res.status = match &strict[pos].ev.line {
                Line::Review {
                    verdict: Verdict::Findings,
                    ..
                } => Status::Findings,
                Line::Review { .. } => Status::Clear,
                Line::Carried { .. } => Status::ClearCarried,
            };
            if let Some(r) = root {
                res.chain.push(strict[r.root].ev.clone());
                res.chain
                    .extend(r.links.iter().map(|&l| strict[l].ev.clone()));
            }
        }
    }
    // Lines about other heads that took no part in the decision.
    for e in strict.iter().filter(|e| !e.foreign) {
        if !e.names_head(head) && !res.chain.iter().any(|c| c.comment_id == e.ev.comment_id) {
            res.findings.push(Finding {
                kind: FindingKind::StaleHead,
                comment_id: e.ev.comment_id,
                author: e.ev.author.clone(),
                detail: String::new(),
            });
        }
    }
    // A broken review line of a trusted author newer than the decision (or without any decision) may be a
    // verdict that did not parse: the state is then UNKNOWN, not what the older lines say.
    let newest = res
        .decisive
        .as_ref()
        .map(|d| (d.created_at.clone(), d.comment_id));
    for (stamp, id, ok) in &variants {
        let newer = match &newest {
            None => true,
            Some(n) => !*ok || (stamp.clone(), *id) > *n,
        };
        if newer {
            reasons.push(format!("comment {id} has an unparsed review line"));
        }
    }
    // Likewise a trusted line for another pr that names the head: it may be meant for this one.
    for &fp in &foreign_head {
        if decisive_pos.is_none_or(|d| fp > d) {
            reasons.push(format!(
                "comment {} names the head with another pr",
                strict[fp].ev.comment_id
            ));
        }
    }
    if !reasons.is_empty() {
        res.status = Status::Unknown;
        res.reasons = reasons;
    }
    res
}

/// Review state from a comment read. Anything but a complete read is UNKNOWN; the partial list is still
/// evaluated so that its findings are visible, but its status is never used.
pub fn evaluate_read(
    scope: Scope,
    head: &str,
    read: &Read<Comment>,
    trusted: &[&str],
) -> ReviewState {
    match read {
        Read::Observed(o) => {
            let mut items = Vec::new();
            let mut bad = Vec::new();
            for c in &o.items {
                match ReviewComment::from_comment(c) {
                    Some(r) => items.push(r),
                    None => bad.push(Finding {
                        kind: FindingKind::UnreadableBody,
                        comment_id: c.id,
                        author: c.author.clone(),
                        detail: "malformed escape in body".into(),
                    }),
                }
            }
            let mut st = evaluate(scope, head, &items, trusted);
            let mut reasons = Vec::new();
            if let Some(why) = &o.incomplete {
                reasons.push(format!("comments incomplete: {why}"));
            }
            for f in &bad {
                reasons.push(format!("comment {} body unreadable", f.comment_id));
            }
            st.findings.extend(bad);
            if !reasons.is_empty() {
                st.status = Status::Unknown;
                st.reasons.extend(reasons);
            }
            st
        }
        other => unknown(
            scope,
            head,
            vec![],
            vec![format!("comments not read: {}", other.state())],
        ),
    }
}
