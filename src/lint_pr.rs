//! `stw lint pr`, rules (M8): checks the commits, the text and the comments of one pull request against the
//! machine-checkable working rules of the agent-rules core. Pure: it reads nothing and writes nothing. Port of
//! `internal/lintpr` of the Go implementation (`legacy/sourcesteward`); the rules are the data table [`RULES`].
//!
//! The Go code uses regular expressions; this crate has no regex dependency, so each expression is written out
//! by hand and named next to its rule. Case-insensitive matching folds with [`fold`] (lower case plus the two
//! extra characters Go's simple case folding maps to ASCII, U+017F and U+212A).
use crate::review::{self, Line, PrRef};

/// Severity of a finding. Only an error makes `stw lint pr` exit with 3.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Severity {
    Error,
    Warning,
}

impl Severity {
    pub fn as_str(self) -> &'static str {
        match self {
            Severity::Error => "error",
            Severity::Warning => "warning",
        }
    }
}

// Rule IDs: part of the JSON contract, never change meaning.
pub const SESSION_LINK: &str = "session-link";
pub const AUTHOR_IDENTITY: &str = "author-identity";
pub const COMMITTER_IDENTITY: &str = "committer-identity";
pub const CO_AUTHORED_BY: &str = "co-authored-by";
pub const CONVENTIONAL_COMMIT: &str = "conventional-commit";
pub const BODY_LENGTH: &str = "pr-body-length";
pub const BLOCKER_LINE: &str = "pr-blocker-line";
pub const AUTHOR_AGENT_LINE: &str = "pr-author-agent-line";
pub const REVIEW_LINE: &str = "review-line-format";
pub const REVIEW_LENGTH: &str = "review-comment-length";

// Location kinds of a finding.
pub const LOC_PR_TITLE: &str = "pr-title";
pub const LOC_PR_BODY: &str = "pr-body";
pub const LOC_COMMENT: &str = "comment";
pub const LOC_COMMIT: &str = "commit";

const SOURCE_CORE: &str = "claude/CLAUDE.md";
const SOURCE_SKILL: &str = "claude/skills/agent-workflow/SKILL.md";
const SOURCE_REPO: &str = "sourcesteward";

/// One row of the table. `source` and `anchor` tie the row to the text it implements: `anchor` is a phrase that
/// occurs verbatim in `source` (a file of the agent-rules checkout, relative to its root). `limit` and `hard` are
/// the numeric limits of a length rule: above `limit` a warning, above `hard` an error.
pub struct Rule {
    pub id: &'static str,
    pub severity: Severity,
    pub summary: &'static str,
    pub source: &'static str,
    pub anchor: &'static str,
    pub limit: usize,
    pub hard: usize,
}

/// The table, in the order findings are reported.
pub const RULES: [Rule; 10] = [
    Rule {
        id: SESSION_LINK,
        severity: Severity::Error,
        source: SOURCE_CORE,
        summary: "No session link in commit messages, the PR title, the PR text or comments (a Claude session trailer or a code-session URL of the Claude web host).",
        // assembled so that this file does not hold the trailer in plain text
        anchor: concat!("claude", "-session:|claude\\.ai/code/session_"),
        limit: 0,
        hard: 0,
    },
    Rule {
        id: AUTHOR_IDENTITY,
        severity: Severity::Error,
        source: SOURCE_CORE,
        summary: "The author of every commit is an owner identity.",
        anchor: "Autor und Committer sind nur der Owner",
        limit: 0,
        hard: 0,
    },
    Rule {
        id: COMMITTER_IDENTITY,
        severity: Severity::Error,
        source: SOURCE_CORE,
        summary: "The committer of every commit is an owner identity.",
        anchor: "Autor und Committer sind nur der Owner",
        limit: 0,
        hard: 0,
    },
    Rule {
        id: CO_AUTHORED_BY,
        severity: Severity::Error,
        source: SOURCE_CORE,
        summary: "Every commit message has a Co-Authored-By trailer with a name and an email.",
        anchor: "atomare Conventional Commits (mit `Co-Authored-By`)",
        limit: 0,
        hard: 0,
    },
    Rule {
        id: CONVENTIONAL_COMMIT,
        severity: Severity::Error,
        source: SOURCE_CORE,
        summary: "The subject of every commit is a Conventional Commit: type(scope)!: description.",
        anchor: "atomare Conventional Commits",
        limit: 0,
        hard: 0,
    },
    Rule {
        id: BODY_LENGTH,
        severity: Severity::Warning,
        source: SOURCE_SKILL,
        summary: "The PR text is about 25 lines: a warning above 25, an error above 30.",
        anchor: "PR-Text, zusammen ca. 25 Zeilen",
        limit: 25,
        hard: 30,
    },
    Rule {
        id: BLOCKER_LINE,
        severity: Severity::Error,
        source: SOURCE_SKILL,
        summary: "The PR text has a Blocker line (the named blocker or none).",
        anchor: "Blocker oder \"keiner\"",
        limit: 0,
        hard: 0,
    },
    Rule {
        id: AUTHOR_AGENT_LINE,
        severity: Severity::Error,
        source: SOURCE_REPO,
        summary: "The PR text has an Author agent line naming the author agent (repository convention, no core text).",
        anchor: "",
        limit: 0,
        hard: 0,
    },
    Rule {
        id: REVIEW_LINE,
        severity: Severity::Error,
        source: SOURCE_CORE,
        summary: "The first line of a review comment is `Review: <CLEAR|FINDINGS> head=<40 hex> patch-id=<40 hex> pr=<n>` or the Carried-over form, for this PR.",
        anchor: "`Review: <CLEAR|FINDINGS> head=<SHA> patch-id=<hex> pr=<n>`",
        limit: 0,
        hard: 0,
    },
    Rule {
        id: REVIEW_LENGTH,
        severity: Severity::Error,
        source: SOURCE_CORE,
        summary: "A review comment has at most 15 lines.",
        anchor: "Review-Kommentar (max 15 Zeilen)",
        limit: 15,
        hard: 15,
    },
];

pub fn rule(id: &str) -> &'static Rule {
    RULES
        .iter()
        .find(|r| r.id == id)
        .unwrap_or_else(|| panic!("lint_pr: unknown rule {id}"))
}

/// The part of a commit the rules read.
#[derive(Clone, Debug, Default)]
pub struct Commit {
    pub sha: String,
    pub message: String,
    pub author_name: String,
    pub author_email: String,
    pub committer_name: String,
    pub committer_email: String,
}

/// One conversation comment of the pull request.
#[derive(Clone, Debug)]
pub struct Comment {
    pub id: i64,
    pub body: String,
}

/// Everything the rules read.
#[derive(Clone, Debug, Default)]
pub struct Input {
    pub pr: u64,
    pub title: String,
    pub body: String,
    pub commits: Vec<Commit>,
    pub comments: Vec<Comment>,
}

/// A name and an email address.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Identity {
    pub name: String,
    pub email: String,
}

/// Where a finding is. `id` is the commit SHA or the comment ID (empty for the PR title and text); `line` is
/// 1-based, 0 when the finding concerns the whole text.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Location {
    pub kind: &'static str,
    pub id: String,
    pub line: usize,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Finding {
    pub rule: &'static str,
    pub severity: Severity,
    pub location: Location,
    pub message: String,
}

/// Simple case folding of one character, as far as Go's `(?i)` and `strings.EqualFold` go for the texts
/// compared here.
fn fold_char(c: char) -> char {
    match c {
        '\u{17F}' => 's',
        '\u{212A}' => 'k',
        c => {
            let mut l = c.to_lowercase();
            match (l.next(), l.next()) {
                (Some(one), None) => one,
                _ => c,
            }
        }
    }
}

pub fn fold(s: &str) -> String {
    s.chars().map(fold_char).collect()
}

/// `s` without the prefix `key` (lower case, folded character by character), or None. The rest is the original
/// text, and with it comes the last character taken as it is (`\b` after a keyword looks at the characters
/// as they are, not folded: U+017F matches `s` but is no word character).
fn strip_ci<'a>(s: &'a str, key: &str) -> Option<(&'a str, char)> {
    let mut it = s.char_indices();
    let mut last = ' ';
    for k in key.chars() {
        let (_, c) = it.next()?;
        if fold_char(c) != k {
            return None;
        }
        last = c;
    }
    Some((it.next().map(|(i, _)| &s[i..]).unwrap_or(""), last))
}

fn strip_prefix_ci<'a>(s: &'a str, key: &str) -> Option<&'a str> {
    strip_ci(s, key).map(|(r, _)| r)
}

/// `\b` between the last matched character and the rest (ASCII word characters).
fn boundary_after(last: char, rest: &str) -> bool {
    word(last) != rest.chars().next().is_some_and(word)
}

/// `key` followed by `\b`; with `optional_s` the key is followed by `s?` first (the engine backtracks from
/// taking the `s` to leaving it).
fn keyword_ci(s: &str, key: &str, optional_s: bool) -> bool {
    let Some((rest, last)) = strip_ci(s, key) else {
        return false;
    };
    if optional_s {
        if let Some((r2, l2)) = strip_ci(rest, "s") {
            if boundary_after(l2, r2) {
                return true;
            }
        }
    }
    boundary_after(last, rest)
}

fn normalize(s: &str) -> String {
    s.replace("\r\n", "\n")
}

/// `\s` of RE2: ASCII blank, tab, line feed, form feed, carriage return.
pub(crate) fn re_space(c: char) -> bool {
    matches!(c, ' ' | '\t' | '\n' | '\u{c}' | '\r')
}

fn word(c: char) -> bool {
    c.is_ascii_alphanumeric() || c == '_'
}

fn first_line(s: &str) -> &str {
    s.split('\n').next().unwrap_or("")
}

/// Lines of a text without trailing blank lines (CRLF counts as LF); 0 for an empty text.
pub fn line_count(s: &str) -> usize {
    let n = normalize(s);
    let t = n.trim_end_matches(['\n', ' ', '\t']);
    if t.is_empty() {
        0
    } else {
        t.matches('\n').count() + 1
    }
}

fn same_identity(owners: &[Identity], name: &str, email: &str) -> bool {
    owners
        .iter()
        .any(|o| o.name == name && fold(&o.email) == fold(email))
}

/// The session trailer or the code-session URL of the Claude web host (see the `anchor` of the table), case-insensitive,
/// on one line.
pub(crate) fn session_pattern(line: &str) -> bool {
    let f = fold(line);
    f.contains(concat!("claude", "-session:")) || f.contains(concat!("claude", ".ai/code/session_"))
}

/// The session-link rule applied to a whole text (line endings normalized, one line at a time): true when any
/// line holds a session trailer or a code-session URL. Shared with `stw prove`.
pub fn has_session_link(text: &str) -> bool {
    normalize(text).split('\n').any(session_pattern)
}

/// The Co-Authored-By trailer lines of a message, in order, as the Co-Authored-By rule reads them (key in any
/// case, a name and an address in angle brackets). Shared with `stw prove`.
pub fn co_authored_lines(message: &str) -> Vec<String> {
    normalize(message)
        .split('\n')
        .filter(|l| co_authored_line(l))
        .map(str::to_string)
        .collect()
}

/// `(?im)^co-authored-by:[ \t]*[^<\r\n]+<[^<>\s]+@[^<>\s]+>[ \t]*\r?$` on one line (no line feed in it).
fn co_authored_line(line: &str) -> bool {
    const KEY: &str = "co-authored-by:";
    let Some(rest) = strip_prefix_ci(line, KEY) else {
        return false;
    };
    let rest = rest.strip_suffix('\r').unwrap_or(rest);
    let rest = rest.trim_end_matches([' ', '\t']);
    let Some(rest) = rest.strip_suffix('>') else {
        return false;
    };
    // the name takes every character up to the first `<` (no `<`, CR or LF in it); at least one
    let Some(p) = rest.find('<') else {
        return false;
    };
    let (name, addr) = (&rest[..p], &rest[p + 1..]);
    if name.is_empty() || name.contains('\r') {
        return false;
    }
    // `[^<>\s]+@[^<>\s]+`: no `<`, `>` or blank, an `@` with at least one character on both sides
    if addr.chars().any(|c| c == '<' || c == '>' || re_space(c)) {
        return false;
    }
    addr.char_indices()
        .any(|(i, c)| c == '@' && i > 0 && i + 1 < addr.len())
}

fn co_authored(message: &str) -> bool {
    message.split('\n').any(co_authored_line)
}

const TYPES: [&str; 10] = [
    "feat", "fix", "docs", "style", "refactor", "perf", "test", "build", "ci", "chore",
];

/// `\A(?:feat|fix|...|revert)(?:\([A-Za-z0-9._/-]+\))?!?: \S` (case-sensitive).
fn conventional(subject: &str) -> bool {
    let mut rest = None;
    for t in TYPES.iter().chain(&["revert"]) {
        if let Some(r) = subject.strip_prefix(t) {
            rest = Some(r);
            break;
        }
    }
    let Some(mut r) = rest else {
        return false;
    };
    if let Some(scoped) = r.strip_prefix('(') {
        let Some(end) = scoped.find(')') else {
            return false;
        };
        let scope = &scoped[..end];
        let ok = !scope.is_empty()
            && scope
                .chars()
                .all(|c| c.is_ascii_alphanumeric() || matches!(c, '.' | '_' | '/' | '-'));
        if !ok {
            return false;
        }
        r = &scoped[end + 1..];
    }
    let r = r.strip_prefix('!').unwrap_or(r);
    let Some(r) = r.strip_prefix(": ") else {
        return false;
    };
    r.chars().next().is_some_and(|c| !re_space(c))
}

/// `(?im)^[\s*_#>-]*<key>` followed by `\b`, on some line of the text; `key` is lower case.
/// For the blocker line the key is `blocker` with an optional `s`.
pub(crate) fn line_starts_with(text: &str, key: &str, optional_s: bool) -> bool {
    normalize(text).split('\n').any(|line| {
        let t =
            line.trim_start_matches(|c| re_space(c) || matches!(c, '*' | '_' | '#' | '>' | '-'));
        keyword_ci(t, key, optional_s)
    })
}

/// Whether a comment body claims to be a review: its first non-blank line starts with the key (review or
/// carried-over), ignoring case, blanks and markdown marks before and after it, and then has a colon, ends, or
/// names a verdict (`**Review**: CLEAR`, `Review CLEAR`, `## Review`). Prose such as "Review of the approach"
/// does not claim to be one.
/// Regular expression of Go: `(?i)\A[\s*_#>`-]*(?:review|carried-over)[\s*_`]*(?::|\z|(?:clear|findings)\b)`.
pub fn is_review_comment(body: &str) -> bool {
    // blank is Unicode white space, as `strings.TrimSpace` in Go
    let Some(line) = body.split('\n').find(|l| !l.trim().is_empty()) else {
        return false;
    };
    let t =
        line.trim_start_matches(|c| re_space(c) || matches!(c, '*' | '_' | '#' | '>' | '`' | '-'));
    let Some(rest) = strip_prefix_ci(t, "review").or_else(|| strip_prefix_ci(t, "carried-over"))
    else {
        return false;
    };
    let rest = rest.trim_start_matches(|c| re_space(c) || matches!(c, '*' | '_' | '`'));
    if rest.is_empty() || rest.starts_with(':') {
        return true;
    }
    ["clear", "findings"]
        .iter()
        .any(|v| keyword_ci(rest, v, false))
}

/// Strict parse of one line (no line break in it) against the grammar of the agent-rules core: fixed keys in
/// fixed order, 40-hex lower-case ids, `pr=` a decimal number of 1 to 9 digits without leading zero (no
/// `none`, which the M3 grammar admits for local reviews).
pub fn parse_review_line(line: &str) -> Option<Line> {
    let l = review::parse_strict(line)?;
    let pr = match &l {
        Line::Review { pr, .. } | Line::Carried { pr, .. } => pr,
    };
    match pr {
        PrRef::Number(n) if *n <= 999_999_999 => Some(l),
        _ => None,
    }
}

fn line_pr(l: &Line) -> u64 {
    match l {
        Line::Review { pr, .. } | Line::Carried { pr, .. } => match pr {
            PrRef::Number(n) => *n,
            PrRef::None => 0,
        },
    }
}

/// Applies the rule table. The result is ordered by rule (table order), then by the order of the input; it is
/// empty when nothing is violated. Without owner identities the two identity rules do not run.
pub fn lint(input: &Input, owners: &[Identity]) -> Vec<Finding> {
    let mut out: Vec<Finding> = Vec::new();
    let mut add = |r: &Rule,
                   severity: Severity,
                   kind: &'static str,
                   id: &str,
                   line: usize,
                   message: String| {
        out.push(Finding {
            rule: r.id,
            severity,
            location: Location {
                kind,
                id: id.to_string(),
                line,
            },
            message,
        });
    };
    for r in RULES.iter() {
        match r.id {
            SESSION_LINK => {
                let mut scan = |kind: &'static str, id: &str, text: &str| {
                    for (i, l) in normalize(text).split('\n').enumerate() {
                        if session_pattern(l) {
                            add(r, r.severity, kind, id, i + 1, "session link".into());
                        }
                    }
                };
                scan(LOC_PR_TITLE, "", &input.title);
                scan(LOC_PR_BODY, "", &input.body);
                for c in &input.comments {
                    scan(LOC_COMMENT, &c.id.to_string(), &c.body);
                }
                for c in &input.commits {
                    scan(LOC_COMMIT, &c.sha, &c.message);
                }
            }
            AUTHOR_IDENTITY | COMMITTER_IDENTITY => {
                if owners.is_empty() {
                    continue;
                }
                for c in &input.commits {
                    let (name, email, role) = if r.id == COMMITTER_IDENTITY {
                        (&c.committer_name, &c.committer_email, "committer")
                    } else {
                        (&c.author_name, &c.author_email, "author")
                    };
                    if !same_identity(owners, name, email) {
                        add(
                            r,
                            r.severity,
                            LOC_COMMIT,
                            &c.sha,
                            0,
                            format!("{role} {name:?} <{email}> is not an owner identity"),
                        );
                    }
                }
            }
            CO_AUTHORED_BY => {
                for c in &input.commits {
                    if !co_authored(&c.message) {
                        add(
                            r,
                            r.severity,
                            LOC_COMMIT,
                            &c.sha,
                            0,
                            "no Co-Authored-By trailer with name and email".into(),
                        );
                    }
                }
            }
            CONVENTIONAL_COMMIT => {
                for c in &input.commits {
                    let s = first_line(&c.message);
                    let s = s.strip_suffix('\r').unwrap_or(s);
                    if !conventional(s) {
                        add(
                            r,
                            r.severity,
                            LOC_COMMIT,
                            &c.sha,
                            1,
                            "subject is not type(scope): description".into(),
                        );
                    }
                }
            }
            BODY_LENGTH => {
                let n = line_count(&input.body);
                if n > r.hard {
                    add(
                        r,
                        Severity::Error,
                        LOC_PR_BODY,
                        "",
                        0,
                        format!("{n} lines, more than {}", r.hard),
                    );
                } else if n > r.limit {
                    add(
                        r,
                        Severity::Warning,
                        LOC_PR_BODY,
                        "",
                        0,
                        format!("{n} lines, about {} expected", r.limit),
                    );
                }
            }
            BLOCKER_LINE => {
                if !line_starts_with(&input.body, "blocker", true) {
                    add(r, r.severity, LOC_PR_BODY, "", 0, "no Blocker line".into());
                }
            }
            AUTHOR_AGENT_LINE => {
                if !line_starts_with(&input.body, "author agent", false) {
                    add(
                        r,
                        r.severity,
                        LOC_PR_BODY,
                        "",
                        0,
                        "no Author agent line".into(),
                    );
                }
            }
            REVIEW_LINE => {
                for c in &input.comments {
                    if !is_review_comment(&c.body) {
                        continue;
                    }
                    let id = c.id.to_string();
                    match parse_review_line(first_line(&c.body)) {
                        None => add(
                            r,
                            r.severity,
                            LOC_COMMENT,
                            &id,
                            1,
                            "first line does not follow the review line grammar".into(),
                        ),
                        Some(l) if line_pr(&l) != input.pr => add(
                            r,
                            r.severity,
                            LOC_COMMENT,
                            &id,
                            1,
                            format!("pr={}, this is pull request {}", line_pr(&l), input.pr),
                        ),
                        Some(_) => {}
                    }
                }
            }
            REVIEW_LENGTH => {
                for c in &input.comments {
                    if !is_review_comment(&c.body) {
                        continue;
                    }
                    let n = line_count(&c.body);
                    if n > r.limit {
                        add(
                            r,
                            r.severity,
                            LOC_COMMENT,
                            &c.id.to_string(),
                            0,
                            format!("{n} lines, at most {}", r.limit),
                        );
                    }
                }
            }
            _ => unreachable!("every rule of the table has a case"),
        }
    }
    out
}
