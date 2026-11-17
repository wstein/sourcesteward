//! `stw lint review FILE` and `stw lint pr-text FILE` (new, no Go counterpart): the text of a review comment or of
//! a PR text from a file. Pure rules on a text, plus one optional comparison with a local repository (patch-id,
//! head). Reads no network, writes nothing. The rules that `stw lint pr` has too are the same functions
//! (`lint_pr::line_count`, the session pattern, the limits of the table); the first line of a review comment is
//! read by the strict parser of `review` (`review::parse_strict`), not by a second grammar.
use std::path::Path;

use serde_json::{json, Value};

use crate::carryover::{Git, RepoGit};
use crate::error::{Error, Result};
use crate::git_runner;
use crate::lint_pr::{self, Finding, Location, Rule, Severity};
use crate::lint_pr_cmd::rfc3339_utc;
use crate::repo;
use crate::review::{self, Line, PrRef};

// Rule IDs of the rules that only the file commands have (part of the JSON contract). The IDs that `stw lint pr`
// has too are in `lint_pr`: `session-link`, `pr-body-length`, `pr-blocker-line`, `review-line-format`,
// `review-comment-length`.
pub const TEXT_LINE_ENDINGS: &str = "text-line-endings";
pub const REVIEW_PATCH_ID: &str = "review-patch-id";
pub const REVIEW_HEAD: &str = "review-head";
pub const PR_GENERATED_LINE: &str = "pr-generated-line";

/// The location kind of a finding in a file.
pub const LOC_FILE: &str = "file";

/// The attribution line that ends a PR text (the attribution of the tool, see the adapter of the agent rules).
pub const GENERATED_LINE: &str = "🤖 Generated with [Claude Code](https://claude.com/claude-code)";

const SOURCE_CORE: &str = "claude/CLAUDE.md";
const SOURCE_SKILL: &str = "claude/skills/agent-workflow/SKILL.md";

/// The rules only the file commands have. `source` and `anchor` as in `lint_pr::Rule`.
pub const FILE_RULES: [Rule; 4] = [
    Rule {
        id: TEXT_LINE_ENDINGS,
        severity: Severity::Error,
        source: SOURCE_SKILL,
        summary: "The text has no carriage return (no CRLF, no lone CR).",
        anchor: "kein Markdown, kein CRLF",
        limit: 0,
        hard: 0,
    },
    Rule {
        id: REVIEW_PATCH_ID,
        severity: Severity::Error,
        source: SOURCE_SKILL,
        summary: "With --base and --head: patch-id= is the first field of `git diff BASE...HEAD | git patch-id --stable`.",
        anchor: "patch-id = erstes Feld der Ausgabe von `git patch-id --stable`",
        limit: 0,
        hard: 0,
    },
    Rule {
        id: REVIEW_HEAD,
        severity: Severity::Error,
        source: SOURCE_SKILL,
        summary: "With --head: head= (Carried-over: to=) is the full SHA the head revision resolves to.",
        anchor: "volle 40-stellige SHAs",
        limit: 0,
        hard: 0,
    },
    Rule {
        id: PR_GENERATED_LINE,
        severity: Severity::Error,
        source: "dotagents-adapter.md",
        summary: "The last non-blank line of a PR text is the attribution line of the tool.",
        anchor: "",
        limit: 0,
        hard: 0,
    },
];

fn file_rule(id: &str) -> &'static Rule {
    FILE_RULES
        .iter()
        .find(|r| r.id == id)
        .unwrap_or_else(|| lint_pr::rule(id))
}

/// The rules `stw lint review` applies, in the order findings are reported.
pub const REVIEW_RULE_IDS: [&str; 6] = [
    lint_pr::REVIEW_LINE,
    REVIEW_PATCH_ID,
    REVIEW_HEAD,
    lint_pr::REVIEW_LENGTH,
    lint_pr::SESSION_LINK,
    TEXT_LINE_ENDINGS,
];

/// The rules `stw lint pr-text` applies, in the order findings are reported.
pub const PR_TEXT_RULE_IDS: [&str; 5] = [
    lint_pr::BODY_LENGTH,
    lint_pr::BLOCKER_LINE,
    PR_GENERATED_LINE,
    lint_pr::SESSION_LINK,
    TEXT_LINE_ENDINGS,
];

/// What a review line is compared with: the head and the patch-id of `BASE...HEAD` of a local repository.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Expect {
    pub head: String,
    /// Empty when the diff is empty (no patch, nothing a line could name).
    pub patch_id: String,
}

#[derive(Clone, Debug, Default)]
pub struct ReviewOpts {
    /// `--pr`: the `pr=` field must equal it.
    pub pr: Option<PrRef>,
    pub expect: Option<Expect>,
}

fn finding(rule: &'static str, line: usize, message: impl Into<String>) -> Finding {
    Finding {
        rule,
        severity: file_rule(rule).severity,
        location: Location {
            kind: LOC_FILE,
            id: String::new(),
            line,
        },
        message: message.into(),
    }
}

fn first_line(text: &str) -> &str {
    text.split('\n').next().unwrap_or("")
}

/// The bound of `lint pr` (`lint_pr::parse_review_line`): `pr=` has at most nine digits; `none` is allowed here.
fn pr_in_range(l: &Line) -> bool {
    let (Line::Review { pr, .. } | Line::Carried { pr, .. }) = l;
    !matches!(pr, PrRef::Number(n) if *n > 999_999_999)
}

fn pr_text(p: PrRef) -> String {
    match p {
        PrRef::Number(n) => n.to_string(),
        PrRef::None => "none".into(),
    }
}

/// One finding per line that holds a carriage return.
fn line_endings(text: &str, out: &mut Vec<Finding>) {
    for (i, l) in text.split('\n').enumerate() {
        if l.contains('\r') {
            out.push(finding(
                TEXT_LINE_ENDINGS,
                i + 1,
                "carriage return in the line",
            ));
        }
    }
}

fn session_links(text: &str, out: &mut Vec<Finding>) {
    for (i, l) in text.split('\n').enumerate() {
        if lint_pr::session_pattern(l) {
            out.push(finding(lint_pr::SESSION_LINK, i + 1, "session link"));
        }
    }
}

/// Applies the rules to the text of a review comment. Ordered by rule (the order of `REVIEW_RULE_IDS`), then by line.
pub fn lint_review(text: &str, opts: &ReviewOpts) -> Vec<Finding> {
    let mut out = Vec::new();
    let line = first_line(text);
    match review::parse_strict(line).filter(pr_in_range) {
        None => out.push(finding(
            lint_pr::REVIEW_LINE,
            1,
            "first line does not follow the review line grammar",
        )),
        Some(l) => {
            let (pr, head, patch_id) = match &l {
                Line::Review {
                    pr, head, patch_id, ..
                } => (*pr, head, patch_id),
                Line::Carried {
                    pr, to, patch_id, ..
                } => (*pr, to, patch_id),
            };
            if let Some(want) = opts.pr {
                if want != pr {
                    out.push(finding(
                        lint_pr::REVIEW_LINE,
                        1,
                        format!("pr={}, expected pr={}", pr_text(pr), pr_text(want)),
                    ));
                }
            }
            if let Some(e) = &opts.expect {
                if &e.patch_id != patch_id {
                    let want = if e.patch_id.is_empty() {
                        "no patch (empty diff)"
                    } else {
                        &e.patch_id
                    };
                    out.push(finding(
                        REVIEW_PATCH_ID,
                        1,
                        format!("patch-id={patch_id}, the repository gives {want}"),
                    ));
                }
                if &e.head != head {
                    out.push(finding(
                        REVIEW_HEAD,
                        1,
                        format!("head {head}, the head revision is {}", e.head),
                    ));
                }
            }
        }
    }
    let n = lint_pr::line_count(text);
    let r = lint_pr::rule(lint_pr::REVIEW_LENGTH);
    if n > r.limit {
        out.push(finding(
            lint_pr::REVIEW_LENGTH,
            0,
            format!("{n} lines, at most {}", r.limit),
        ));
    }
    session_links(text, &mut out);
    line_endings(text, &mut out);
    out
}

/// Applies the rules to a PR text. `ready`: the PR is to be set Ready (`Blocker: None.`); otherwise it is a Draft
/// and names its blocker. Ordered by rule (the order of `PR_TEXT_RULE_IDS`), then by line.
pub fn lint_pr_text(text: &str, ready: bool) -> Vec<Finding> {
    let mut out = Vec::new();
    let n = lint_pr::line_count(text);
    let r = lint_pr::rule(lint_pr::BODY_LENGTH);
    if n > r.hard {
        out.push(Finding {
            severity: Severity::Error,
            ..finding(
                lint_pr::BODY_LENGTH,
                0,
                format!("{n} lines, more than {}", r.hard),
            )
        });
    } else if n > r.limit {
        out.push(Finding {
            severity: Severity::Warning,
            ..finding(
                lint_pr::BODY_LENGTH,
                0,
                format!("{n} lines, about {} expected", r.limit),
            )
        });
    }
    blocker(text, ready, &mut out);
    let all: Vec<&str> = text.split('\n').collect();
    let last = all
        .iter()
        .enumerate()
        .rfind(|(_, l)| !l.trim().is_empty())
        .map(|(i, l)| (i, *l));
    match last {
        Some((_, l)) if l == GENERATED_LINE => {}
        Some((i, _)) => out.push(finding(
            PR_GENERATED_LINE,
            i + 1,
            "the last non-blank line is not the attribution line",
        )),
        None => out.push(finding(PR_GENERATED_LINE, 0, "the text is empty")),
    }
    session_links(text, &mut out);
    line_endings(text, &mut out);
    out
}

fn blocker(text: &str, ready: bool, out: &mut Vec<Finding>) {
    // a line that reads like a Blocker line (markdown marks allowed): the rule of `lint pr`
    let lines: Vec<(usize, &str)> = text
        .split('\n')
        .map(|l| l.strip_suffix('\r').unwrap_or(l))
        .enumerate()
        .filter(|(_, l)| lint_pr::line_starts_with(l, "blocker", true))
        .collect();
    match lines.as_slice() {
        [] => out.push(finding(lint_pr::BLOCKER_LINE, 0, "no Blocker line")),
        [(i, l)] => {
            let at = i + 1;
            match l.strip_prefix("Blocker: ") {
                None => out.push(finding(
                    lint_pr::BLOCKER_LINE,
                    at,
                    "the line is not `Blocker: None.` or `Blocker: <text>`",
                )),
                Some(rest) if rest.trim().is_empty() => out.push(finding(
                    lint_pr::BLOCKER_LINE,
                    at,
                    "the line is not `Blocker: None.` or `Blocker: <text>`",
                )),
                Some("None.") if !ready => out.push(finding(
                    lint_pr::BLOCKER_LINE,
                    at,
                    "a Draft names its blocker; `Blocker: None.` only for a Ready PR (--ready)",
                )),
                Some(rest) if ready && rest != "None." => out.push(finding(
                    lint_pr::BLOCKER_LINE,
                    at,
                    "a Ready PR has `Blocker: None.`, this one names a blocker",
                )),
                Some(_) => {}
            }
        }
        many => out.push(finding(
            lint_pr::BLOCKER_LINE,
            many[1].0 + 1,
            format!("{} Blocker lines, exactly one expected", many.len()),
        )),
    }
}

/// The head and the patch-id of `BASE...HEAD` in the repository around `checkout` (default: the current
/// directory). Revisions are resolved to full ids; nothing is fetched or written, and `git` is not started.
pub fn expectation(checkout: Option<&Path>, base: &str, head: &str) -> Result<Expect> {
    let start = match checkout {
        Some(p) => p.to_path_buf(),
        None => std::env::current_dir()
            .map_err(|e| Error::usage(format!("no current directory: {e}")))?,
    };
    let start = std::fs::canonicalize(&start).unwrap_or(start);
    let dir = repo::top_level(&start).unwrap_or(start);
    let r = repo::open_isolated(&dir)
        .map_err(|e| Error::usage(format!("{}: not a repository: {e}", dir.display())))?;
    let resolve = |name: &str, rev: &str| -> Result<String> {
        r.rev_parse_single(rev)
            .map(|id| id.detach().to_string())
            .map_err(|e| Error::usage(format!("--{name} {rev}: {e}")))
    };
    let (b, h) = (resolve("base", base)?, resolve("head", head)?);
    let patch_id = RepoGit::new(&dir, git_runner::shared())
        .patch_id(&b, &h)
        .map_err(|e| Error::repository(format!("patch-id of {base}...{head}: {}", e.message)))?;
    Ok(Expect { head: h, patch_id })
}

/// The result of one run.
pub struct Report {
    /// `lint review` or `lint pr-text`.
    pub command: &'static str,
    pub file: String,
    pub lines: usize,
    pub rule_ids: &'static [&'static str],
    pub options: Value,
    pub findings: Vec<Finding>,
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
        self.errors() == 0
    }

    /// The JSON document, in the shape of `lint pr` (schema 1; keys are never omitted).
    pub fn to_json(&self, now_secs: i64) -> Value {
        let findings: Vec<Value> = self
            .findings
            .iter()
            .map(|f| {
                json!({
                    "rule": f.rule,
                    "severity": f.severity.as_str(),
                    "location": {
                        "kind": f.location.kind,
                        "id": Value::Null,
                        "line": if f.location.line > 0 { json!(f.location.line) } else { Value::Null },
                    },
                    "message": f.message,
                })
            })
            .collect();
        let rules: Vec<Value> = self
            .rule_ids
            .iter()
            .map(|id| {
                let r = file_rule(id);
                json!({"id": r.id, "severity": r.severity.as_str(), "summary": r.summary, "source": r.source})
            })
            .collect();
        json!({
            "schema_version": 1,
            "command": self.command,
            "time": rfc3339_utc(now_secs),
            "file": self.file,
            "options": self.options,
            "rules": rules,
            "findings": findings,
            "summary": {
                "lines": self.lines,
                "errors": self.errors(),
                "warnings": self.warnings(),
                "passed": self.passed(),
            },
        })
    }

    pub fn render_human(&self) -> String {
        let verdict = if self.passed() { "pass" } else { "FAIL" };
        let mut b = format!(
            "{}: {verdict}, {} errors, {} warnings ({} lines read)\n",
            self.file,
            self.errors(),
            self.warnings(),
            self.lines
        );
        for f in &self.findings {
            let place = if f.location.line > 0 {
                format!("{}:{}", f.location.kind, f.location.line)
            } else {
                f.location.kind.to_string()
            };
            b.push_str(&format!(
                "{:<7} {:<22} {place}: {}\n",
                f.severity.as_str(),
                f.rule,
                f.message
            ));
        }
        b
    }
}

/// Reads a UTF-8 text file. A missing or unreadable file and other bytes are usage errors (nothing was linted).
pub fn read_text(path: &Path) -> Result<String> {
    let bytes =
        std::fs::read(path).map_err(|e| Error::usage(format!("{}: {e}", path.display())))?;
    String::from_utf8(bytes)
        .map_err(|_| Error::usage(format!("{}: not UTF-8 text", path.display())))
}
