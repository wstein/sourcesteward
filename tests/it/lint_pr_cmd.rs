//! Go corpus check for M8, command: every case of `legacy/sourcesteward/internal/lintprcmd/command_test.go` and of
//! `internal/cli/lint_test.go` is one Rust case here (same scenario, same name in snake case, except two
//! renames below) or a named deviation. Renames: `TestViolationsExitOneAndAreLocated` is
//! `violations_exit_three_and_are_located`; `TestLintPREndToEndOverTheGuardedReader` is
//! `lint_pr_end_to_end_over_the_rest_client`. The rules are in `lint_pr.rs`. Plan: docs/migration-plan.md, M8.
//!
//! The Go tests of the command run against a fake `Source`; so do the first group here (`stw::lint_pr_cmd::Source`).
//! The second group runs the binary against a fake GitHub API (`cli/lint_test.go`: end to end over the guarded
//! reader; here the REST client of this crate, GET only, no redirects).
//!
//! `TestOpenFailureExitsOne` (no reader can be built) is the no-token case of `lint_pr_usage_and_read_failures`: the only way
//! the Rust client is not built; `TestPrepare` is `prepare_ok` and `prepare_usage_errors`.
//!
//! Deviations of Rust from Go, command (each is tested below where marked `DEVIATION`):
//! - D1 output default: Go prints the human list on a terminal and JSON otherwise (`--human`, `--json`). Rust,
//!   like every command of this crate, prints text unless `--json` is given; `--human` stays and conflicts with
//!   `--json` (usage, exit 2).
//! - D2 no `--host`: Go reads any GitHub host. Rust reads `api.github.com` or `STW_GITHUB_API_URL` only, like
//!   the other commands; the `host` field of the document is `github.com`.
//! - D3 the document keeps Go's schema 1 (`schema_version` 1, no `engine`); the schema file itself stays with
//!   the Go tree. `time` is UTC `YYYY-MM-DDTHH:MM:SSZ`. The JSON text is written raw: Go's `json.Encoder` escapes
//!   `<`, `>` and `&` as `\u003c`, `\u003e`, `\u0026`, Rust does not (same value after decoding).
//! - D4 workspace: the repository comes from the single GitHub `origin` among the repository of PATH (default:
//!   the current directory) and its checked-out submodules (`repo::add_root`; a plain repository is a workspace
//!   of one). The git identity is read with gix (`user.name`, `user.email`), not with `git config --get`.
//! - D5 human output escapes the message with `escape_untrusted` (also the backslash; `\u{hex}` for control and
//!   invisible characters, cut at 120 characters with `...`), Go with `untrusted.Esc` (`\xNN`, `\uNNNN`).
//! - D6 JSON decoding is strict about the types it reads (as Go) but matches keys case-sensitively; Go's
//!   `encoding/json` also accepts `Title` for `title`. A JSON `null` for a list is the empty list in both.
//! - D7 not ported: the Go guard that refuses every endpoint but the three lint reads (`hosting` guard modes, call
//!   accounting): the Rust client has no write method and the three endpoint builders are the only callers
//!   (`stw::github::lint_reads`).
//! - D8 the `page` of a `Link` header is not percent-decoded: Go reads `page=%32` as 2, Rust takes the page as
//!   malformed (`Link header`). `page=+2` is malformed in both.
//! - D9 a body of more than one MiB (exactly one MiB is read) is `page-too-large` for comments and
//!   `malformed-response` for `pulls/N` and commits, as in Go; a 304 is `malformed-response` (`unsolicited 304 response`).
use std::cell::RefCell;
use std::process::{Command, Output};

use crate::common::*;
use serde_json::Value;
use stw::error::{Error, Kind};
use stw::github::lint_reads::LintPage;
use stw::lint_pr::Identity;
use stw::lint_pr_cmd::{
    execute, parse_identity, prepare, rfc3339_utc, Flags, Outcome, Params, Source, OWNER_FLAG,
    OWNER_GIT_CONFIG,
};

const OWNER_NAME: &str = "Octo Owner";
const OWNER_EMAIL: &str = "octo@example.com";
const OWNER_FLAG_TEXT: &str = "Octo Owner <octo@example.com>";
// assembled so that this file does not hold the URL of the session in plain text
const SESSION_URL: &str = concat!("https://claude", ".ai/code/session_", "01XYZ");

fn sha(c: char) -> String {
    c.to_string().repeat(40)
}

// ---- a fake source: canned pages, every call recorded ----------------------------------------------------

#[derive(Default)]
struct Fake {
    detail: String,
    commits: Vec<(usize, String)>,
    comments: Vec<(usize, String)>,
    links: Vec<(String, String)>,
    err_at: Vec<String>,
    calls: RefCell<Vec<String>>,
}

impl Fake {
    fn page(
        &self,
        kind: &str,
        pages: &[(usize, String)],
        page: usize,
    ) -> stw::error::Result<LintPage> {
        let key = format!("{kind}/{page}");
        self.calls.borrow_mut().push(key.clone());
        if self.err_at.contains(&key) {
            return Err(Error::github("rate-limited: rate limit reached"));
        }
        let body = pages
            .iter()
            .find(|(p, _)| *p == page)
            .map(|(_, b)| b.as_str())
            .unwrap_or("[]");
        let link = self
            .links
            .iter()
            .find(|(k, _)| *k == key)
            .map(|(_, l)| l.clone())
            .unwrap_or_default();
        Ok(LintPage {
            body: serde_json::from_str(body).unwrap(),
            link,
        })
    }
}

impl Source for Fake {
    fn pull(&self, slug: &str, n: u64) -> stw::error::Result<LintPage> {
        self.calls.borrow_mut().push(format!("detail/{slug}#{n}"));
        if self.err_at.iter().any(|e| e == "detail") {
            return Err(Error::github("rate-limited: rate limit reached"));
        }
        Ok(LintPage {
            body: serde_json::from_str(&self.detail).unwrap_or(Value::String(self.detail.clone())),
            link: String::new(),
        })
    }
    fn commits(&self, _: &str, _: u64, page: usize) -> stw::error::Result<LintPage> {
        self.page("commits", &self.commits, page)
    }
    fn comments(&self, _: &str, _: u64, page: usize) -> stw::error::Result<LintPage> {
        self.page("comments", &self.comments, page)
    }
}

fn next_link(page: usize) -> String {
    format!("<https://api.github.com/x?page={page}>; rel=\"next\"")
}

fn json_string(s: &str) -> String {
    Value::String(s.into()).to_string()
}

fn commit_json(sha: &str, msg: &str, an: &str, ae: &str, cn: &str, ce: &str) -> String {
    format!(
        "{{\"sha\":\"{sha}\",\"commit\":{{\"message\":{},\"author\":{{\"name\":{},\"email\":{}}},\"committer\":{{\"name\":{},\"email\":{}}}}}}}",
        json_string(msg),
        json_string(an),
        json_string(ae),
        json_string(cn),
        json_string(ce)
    )
}

fn good_commit_json(sha: &str) -> String {
    commit_json(
        sha,
        "feat(x): y\n\nCo-Authored-By: Claude <noreply@anthropic.com>",
        OWNER_NAME,
        OWNER_EMAIL,
        OWNER_NAME,
        OWNER_EMAIL,
    )
}

const GOOD_BODY: &str =
    "Purpose.\n\nBlocker: Independent review pending\nCloses #1\n\nAuthor agent: Claude";

fn detail_with_commits(title: &str, body: &str, commits: i64) -> String {
    format!(
        "{{\"number\":7,\"title\":{},\"body\":{},\"commits\":{commits},\"head\":{{\"sha\":\"{}\"}}}}",
        json_string(title),
        json_string(body),
        sha('a')
    )
}

fn detail(title: &str, body: &str) -> String {
    detail_with_commits(title, body, 1)
}

fn clean_source() -> Fake {
    Fake {
        detail: detail("feat: y", GOOD_BODY),
        commits: vec![(1, format!("[{}]", good_commit_json(&sha('1'))))],
        comments: vec![(1, r#"[{"id":11,"body":"Thanks"}]"#.into())],
        ..Fake::default()
    }
}

fn params(human: bool) -> Params {
    Params {
        repo: "o/r".into(),
        pr: 7,
        owners: vec![Identity {
            name: OWNER_NAME.into(),
            email: OWNER_EMAIL.into(),
        }],
        owner_source: OWNER_FLAG,
        json: !human,
    }
}

const NOW: i64 = 1_762_819_200; // 2025-11-11T00:00:00Z

fn exec(src: &Fake) -> stw::error::Result<Outcome> {
    execute(src, &params(false), NOW)
}

/// The JSON document, checked against the schema of the Go tree (`schemas/v1/lint-pr.schema.json`).
fn doc(out: &Outcome) -> Value {
    assert_eq!(
        out.stdout.matches('\n').count(),
        1,
        "not exactly one line: {:?}",
        out.stdout
    );
    let v: Value = serde_json::from_str(&out.stdout).unwrap();
    check_schema(&v);
    v
}

fn only_keys(v: &Value, keys: &[&str]) {
    let o = v.as_object().unwrap();
    let got: Vec<&str> = o.keys().map(String::as_str).collect();
    assert_eq!(got, keys, "keys or their order differ");
}

fn is_hex40(s: &str) -> bool {
    s.len() == 40 && s.bytes().all(|b| matches!(b, b'0'..=b'9' | b'a'..=b'f'))
}

/// The schema of the Go tree, written out: required keys (never omitted, no others), types, patterns, enums.
fn check_schema(v: &Value) {
    only_keys(
        v,
        &[
            "schema_version",
            "command",
            "time",
            "host",
            "repo",
            "pull_request",
            "head",
            "options",
            "rules",
            "findings",
            "summary",
            "truncated",
        ],
    );
    assert_eq!(v["schema_version"], 1);
    assert_eq!(v["command"], "lint pr");
    let t = v["time"].as_str().unwrap();
    assert!(
        t.len() == 20 && t.ends_with('Z') && &t[10..11] == "T",
        "time {t}"
    );
    assert!(!v["host"].as_str().unwrap().is_empty());
    assert!(valid_repo_text(v["repo"].as_str().unwrap()));
    assert!(v["pull_request"].as_u64().unwrap() >= 1);
    assert!(v["head"].is_null() || is_hex40(v["head"].as_str().unwrap()));
    only_keys(&v["options"], &["owner_identities", "owner_source"]);
    assert!(v["options"]["owner_identities"]
        .as_array()
        .unwrap()
        .iter()
        .all(Value::is_string));
    assert!(matches!(
        v["options"]["owner_source"].as_str(),
        Some("flag" | "git-config")
    ));
    for r in v["rules"].as_array().unwrap() {
        only_keys(r, &["id", "severity", "summary", "source"]);
        assert!(!r["id"].as_str().unwrap().is_empty());
        assert!(matches!(r["severity"].as_str(), Some("error" | "warning")));
        assert!(r["summary"].is_string() && r["source"].is_string());
    }
    for f in v["findings"].as_array().unwrap() {
        only_keys(f, &["rule", "severity", "location", "message"]);
        assert!(!f["rule"].as_str().unwrap().is_empty());
        assert!(matches!(f["severity"].as_str(), Some("error" | "warning")));
        only_keys(&f["location"], &["kind", "id", "line"]);
        assert!(matches!(
            f["location"]["kind"].as_str(),
            Some("pr-title" | "pr-body" | "comment" | "commit")
        ));
        assert!(f["location"]["id"].is_null() || f["location"]["id"].is_string());
        assert!(f["location"]["line"].is_null() || f["location"]["line"].as_u64().unwrap() >= 1);
        assert!(f["message"].is_string());
    }
    only_keys(
        &v["summary"],
        &["commits", "comments", "errors", "warnings", "passed"],
    );
    for k in ["commits", "comments", "errors", "warnings"] {
        assert!(v["summary"][k].is_u64(), "{k}");
    }
    assert!(v["summary"]["passed"].is_boolean());
    only_keys(&v["truncated"], &["commits", "comments"]);
    assert!(v["truncated"]["commits"].is_boolean() && v["truncated"]["comments"].is_boolean());
}

fn valid_repo_text(s: &str) -> bool {
    stw::lint_pr_cmd::valid_repo(s)
}

fn finding_rules(v: &Value) -> Vec<String> {
    v["findings"]
        .as_array()
        .unwrap()
        .iter()
        .map(|f| f["rule"].as_str().unwrap().to_string())
        .collect()
}

#[test]
fn clean_pull_request_passes() {
    let out = exec(&clean_source()).unwrap();
    assert_eq!((out.code, out.stderr.as_deref()), (0, None));
    let m = doc(&out);
    let s = &m["summary"];
    assert_eq!(
        (
            s["passed"].as_bool(),
            s["errors"].as_u64(),
            s["commits"].as_u64(),
            s["comments"].as_u64()
        ),
        (Some(true), Some(0), Some(1), Some(1))
    );
    assert!(m["findings"].as_array().unwrap().is_empty());
    assert_eq!(m["head"], sha('a'));
    assert_eq!(m["pull_request"], 7);
    assert_eq!(m["repo"], "o/r");
    assert_eq!(
        m["rules"].as_array().unwrap().len(),
        stw::lint_pr::RULES.len()
    );
    assert_eq!(m["options"]["owner_identities"][0], OWNER_FLAG_TEXT);
    assert_eq!(m["options"]["owner_source"], "flag");
    assert_eq!(m["time"], "2025-11-11T00:00:00Z");
    assert_eq!(m["host"], "github.com");
}

#[test]
fn violations_exit_three_and_are_located() {
    let mut src = clean_source();
    src.detail = detail(
        &format!("feat: y {SESSION_URL}"),
        &format!("no structure\n{SESSION_URL}"),
    );
    src.commits = vec![(
        1,
        format!(
            "[{}]",
            commit_json(
                &sha('2'),
                &format!("bad subject {SESSION_URL}"),
                "Other",
                "o@x.y",
                OWNER_NAME,
                OWNER_EMAIL
            )
        ),
    )];
    src.comments = vec![(
        1,
        format!("[{{\"id\":42,\"body\":\"Review: CLEAR\\nrest {SESSION_URL}\"}}]"),
    )];
    let out = exec(&src).unwrap();
    assert_eq!(out.code, 3);
    assert!(!out.stdout.is_empty());
    let m = doc(&out);
    let mut want: std::collections::BTreeMap<&str, bool> = [
        "session-link",
        "author-identity",
        "co-authored-by",
        "conventional-commit",
        "pr-blocker-line",
        "pr-author-agent-line",
        "review-line-format",
    ]
    .into_iter()
    .map(|r| (r, false))
    .collect();
    let mut session_kinds = std::collections::BTreeSet::new();
    for f in m["findings"].as_array().unwrap() {
        let loc = &f["location"];
        let rule = f["rule"].as_str().unwrap();
        want.insert(rule, true);
        match rule {
            "author-identity" | "co-authored-by" | "conventional-commit" => {
                assert_eq!(
                    (loc["kind"].as_str(), loc["id"].as_str()),
                    (Some("commit"), Some(sha('2').as_str())),
                    "{rule}"
                )
            }
            "review-line-format" => assert_eq!(
                (
                    loc["kind"].as_str(),
                    loc["id"].as_str(),
                    loc["line"].as_u64()
                ),
                (Some("comment"), Some("42"), Some(1))
            ),
            "pr-blocker-line" => assert_eq!(
                (loc["kind"].as_str(), loc["id"].is_null()),
                (Some("pr-body"), true)
            ),
            "session-link" => {
                session_kinds.insert(loc["kind"].as_str().unwrap().to_string());
            }
            _ => {}
        }
    }
    for (r, ok) in want {
        assert!(ok, "no {r} finding");
    }
    assert_eq!(session_kinds.len(), 4, "{session_kinds:?}");
    assert_eq!(m["summary"]["passed"], false);
    let err = out.stderr.unwrap();
    assert!(err.contains("errors") && err.contains("warning"), "{err}");
}

#[test]
fn warning_only_exits_zero() {
    let mut src = clean_source();
    src.detail = detail(
        "feat: y",
        &format!("{GOOD_BODY}\n{}", vec!["x"; 24].join("\n")),
    );
    let out = exec(&src).unwrap();
    assert_eq!(out.code, 0);
    let m = doc(&out);
    assert_eq!(finding_rules(&m), ["pr-body-length"]);
    let s = &m["summary"];
    assert_eq!(
        (
            s["warnings"].as_u64(),
            s["errors"].as_u64(),
            s["passed"].as_bool()
        ),
        (Some(1), Some(0), Some(true))
    );
}

#[test]
fn null_body_is_empty() {
    let mut src = clean_source();
    src.detail = format!(
        "{{\"number\":7,\"title\":\"t\",\"body\":null,\"head\":{{\"sha\":\"{}\"}}}}",
        sha('a')
    );
    let out = exec(&src).unwrap();
    assert_eq!(out.code, 3);
    assert!(!finding_rules(&doc(&out)).is_empty());
}

#[test]
fn paging_commits_and_comments() {
    let mut src = clean_source();
    src.detail = detail_with_commits("feat: y", GOOD_BODY, 2);
    src.commits = vec![
        (1, format!("[{}]", good_commit_json(&sha('1')))),
        (2, format!("[{}]", good_commit_json(&sha('3')))),
    ];
    src.links.push(("commits/1".into(), next_link(2)));
    src.comments = vec![
        (1, r#"[{"id":1,"body":"a"}]"#.into()),
        (2, r#"[{"id":2,"body":"b"}]"#.into()),
        (3, r#"[{"id":3,"body":"c"}]"#.into()),
    ];
    src.links.push(("comments/1".into(), next_link(2)));
    src.links.push(("comments/2".into(), next_link(3)));
    let out = exec(&src).unwrap();
    assert_eq!(out.code, 0);
    let m = doc(&out);
    assert_eq!(
        (
            m["summary"]["commits"].as_u64(),
            m["summary"]["comments"].as_u64(),
            m["truncated"]["commits"].as_bool()
        ),
        (Some(2), Some(3), Some(false))
    );
    assert_eq!(
        src.calls.borrow().join(" "),
        "detail/o/r#7 commits/1 commits/2 comments/1 comments/2 comments/3"
    );
}

#[test]
fn truncation_at_the_page_bounds() {
    let mut src = clean_source();
    src.commits.clear();
    src.comments.clear();
    for p in 1..=10usize {
        src.comments
            .push((p, format!("[{{\"id\":{p},\"body\":\"x\"}}]")));
        src.links.push((format!("comments/{p}"), next_link(p + 1)));
        if p <= 3 {
            src.commits.push((
                p,
                format!("[{}]", good_commit_json(&sha(char::from(b'0' + p as u8)))),
            ));
            src.links.push((format!("commits/{p}"), next_link(p + 1)));
        }
    }
    let out = exec(&src).unwrap();
    assert_eq!(out.code, 3, "a truncated read must fail closed");
    assert!(out.stderr.as_deref().unwrap().contains("not read in full"));
    let m = doc(&out);
    assert_eq!(
        (
            m["summary"]["passed"].as_bool(),
            m["summary"]["errors"].as_u64()
        ),
        (Some(false), Some(0))
    );
    assert_eq!(
        (
            m["truncated"]["commits"].as_bool(),
            m["truncated"]["comments"].as_bool(),
            m["summary"]["comments"].as_u64()
        ),
        (Some(true), Some(true), Some(10))
    );
    let calls = src.calls.borrow();
    assert!(
        !calls.iter().any(|c| c == "commits/4" || c == "comments/11"),
        "read past the bound: {calls:?}"
    );
}

#[test]
fn each_truncation_alone_fails_closed() {
    // comments only: ten full pages
    let mut src = clean_source();
    src.comments.clear();
    for p in 1..=10usize {
        src.comments
            .push((p, format!("[{{\"id\":{p},\"body\":\"x\"}}]")));
        src.links.push((format!("comments/{p}"), next_link(p + 1)));
    }
    let out = exec(&src).unwrap();
    let m = doc(&out);
    assert_eq!(
        (
            out.code,
            m["summary"]["passed"].as_bool(),
            m["truncated"]["commits"].as_bool(),
            m["truncated"]["comments"].as_bool()
        ),
        (3, Some(false), Some(false), Some(true))
    );
    // commits only: GitHub lists at most 250 commits, the pull request says 300
    let mut src = clean_source();
    src.detail = detail_with_commits("feat: y", GOOD_BODY, 300);
    let out = exec(&src).unwrap();
    let m = doc(&out);
    assert_eq!(
        (
            out.code,
            m["summary"]["passed"].as_bool(),
            m["truncated"]["commits"].as_bool(),
            m["truncated"]["comments"].as_bool()
        ),
        (3, Some(false), Some(true), Some(false))
    );
    assert!(out.stderr.unwrap().contains("commits were not read"));
    // a pull request without a commits field is not judged by it
    let mut src = clean_source();
    src.detail = format!(
        "{{\"number\":7,\"title\":\"t\",\"body\":{},\"head\":{{\"sha\":\"{}\"}}}}",
        json_string(GOOD_BODY),
        sha('a')
    );
    assert_eq!(exec(&src).unwrap().code, 0);
    // the human output says so
    let mut src = clean_source();
    src.detail = detail_with_commits("feat: y", GOOD_BODY, 300);
    let out = execute(&src, &params(true), NOW).unwrap();
    assert!(out.stdout.contains("incomplete"), "{}", out.stdout);
    assert!(out.stdout.contains("note: not all commits were read"));
}

// The boundary of the commit count: only a count above the number of commits read is an incomplete read.
#[test]
fn commit_count_boundary() {
    for (count, truncated) in [(0, false), (1, false), (2, true), (3, true)] {
        let mut src = clean_source(); // one commit is served
        src.detail = detail_with_commits("feat: y", GOOD_BODY, count);
        let out = exec(&src).unwrap();
        let m = doc(&out);
        assert_eq!(
            m["truncated"]["commits"].as_bool(),
            Some(truncated),
            "count {count}"
        );
        assert_eq!(out.code == 3, truncated, "count {count}");
    }
    // a negative count is as unknown as an absent one
    let mut src = clean_source();
    src.detail = detail_with_commits("feat: y", GOOD_BODY, -5);
    assert_eq!(exec(&src).unwrap().code, 0);
}

#[test]
fn exactly_one_error_exits_three() {
    let mut src = clean_source();
    src.detail = detail("feat: y", "Purpose.\nAuthor agent: Claude");
    let out = exec(&src).unwrap();
    assert_eq!(out.code, 3);
    assert_eq!(doc(&out)["summary"]["errors"], 1);
    assert_eq!(out.stderr.as_deref(), Some("1 error, 0 warnings"));
}

#[test]
fn read_failures_exit_one_without_document() {
    for at in ["detail", "commits/1", "comments/1"] {
        let mut src = clean_source();
        src.err_at.push(at.into());
        let e = exec(&src).err().unwrap_or_else(|| panic!("{at}: no error"));
        assert_eq!(e.kind(), Kind::GitHub, "{at}");
        assert!(e.to_string().contains("rate-limited"), "{at}: {e}");
    }
    for (name, detail) in [
        ("not json", "nope"),
        ("no head", r#"{"number":7,"title":"t","body":""}"#),
        (
            "bad sha",
            r#"{"number":7,"title":"t","body":"","head":{"sha":"abc"}}"#,
        ),
        (
            "upper sha",
            &format!(
                "{{\"title\":\"t\",\"head\":{{\"sha\":\"{}\"}}}}",
                "A".repeat(40)
            ),
        ),
        (
            "title number",
            &format!("{{\"title\":5,\"head\":{{\"sha\":\"{}\"}}}}", sha('a')),
        ),
        (
            "commits string",
            &format!(
                "{{\"commits\":\"2\",\"head\":{{\"sha\":\"{}\"}}}}",
                sha('a')
            ),
        ),
        (
            "commits fraction",
            &format!("{{\"commits\":2.5,\"head\":{{\"sha\":\"{}\"}}}}", sha('a')),
        ),
        ("array", "[]"),
    ] {
        let mut src = clean_source();
        src.detail = detail.to_string();
        let e = exec(&src)
            .err()
            .unwrap_or_else(|| panic!("{name}: no error"));
        assert!(e.to_string().contains("malformed-response"), "{name}: {e}");
    }
    for (name, commits) in [
        ("short sha", r#"[{"sha":"short","commit":{"message":"m"}}]"#),
        ("not a list", r#"{"sha":"x"}"#),
        (
            "message number",
            &format!(
                "[{{\"sha\":\"{}\",\"commit\":{{\"message\":3}}}}]",
                sha('1')
            ),
        ),
        (
            "author string",
            &format!(
                "[{{\"sha\":\"{}\",\"commit\":{{\"author\":\"x\"}}}}]",
                sha('1')
            ),
        ),
    ] {
        let mut src = clean_source();
        src.commits = vec![(1, commits.to_string())];
        assert!(exec(&src).is_err(), "{name}");
    }
    for (name, comments) in [
        ("no id", r#"[{"body":"x"}]"#),
        ("zero id", r#"[{"id":0,"body":"x"}]"#),
        ("float id", r#"[{"id":1.5,"body":"x"}]"#),
        ("body number", r#"[{"id":1,"body":4}]"#),
        ("not a list", r#"{"id":1}"#),
    ] {
        let mut src = clean_source();
        src.comments = vec![(1, comments.to_string())];
        assert!(exec(&src).is_err(), "{name}");
    }
    // a null list is the empty list; a null body is empty; a missing person is empty
    let mut src = clean_source();
    src.commits = vec![(1, "null".into())];
    src.comments = vec![(1, "null".into())];
    let out = exec(&src).unwrap();
    assert_eq!(doc(&out)["summary"]["commits"], 0);
    let mut src = clean_source();
    src.commits = vec![(
        1,
        format!(
            "[{{\"sha\":\"{}\",\"commit\":{{\"message\":\"feat: x\"}}}}]",
            sha('1')
        ),
    )];
    src.comments = vec![(1, r#"[{"id":3,"body":null}]"#.into())];
    let out = exec(&src).unwrap();
    assert!(finding_rules(&doc(&out)).contains(&"author-identity".to_string()));
}

#[test]
fn link_next_that_is_not_the_following_page_is_a_read_failure() {
    for kind in ["commits", "comments"] {
        let mut src = clean_source();
        src.links.push((format!("{kind}/1"), next_link(5)));
        let e = exec(&src).err().unwrap();
        assert!(e.to_string().contains("Link next"), "{kind}: {e}");
    }
    // rel="next" without a usable page is malformed, not the last page
    for link in [
        "<https://x/y>; rel=\"next\"",
        "<https://x/y?page=0>; rel=\"next\"",
        "<https://x/y?page=abc>; rel=\"next\"",
        "<https://x/y?page=+2>; rel=\"next\"",
        // DEVIATION D8: Go reads this as page 2
        "<https://x/y?page=%32>; rel=\"next\"",
        "<https://x/y?page=1000001>; rel=\"next\"",
    ] {
        let mut src = clean_source();
        src.links.push(("commits/1".into(), link.into()));
        let e = exec(&src)
            .err()
            .unwrap_or_else(|| panic!("{link}: no error"));
        assert!(e.to_string().contains("Link header"), "{link}: {e}");
    }
    // other rels and other spellings
    let mut src = clean_source();
    src.links.push((
        "commits/1".into(),
        "<https://x/y?page=1>; rel=\"prev\", <https://x/y?page=9>; rel=\"last\"".into(),
    ));
    assert_eq!(exec(&src).unwrap().code, 0);
    let mut src = clean_source();
    src.detail = detail_with_commits("feat: y", GOOD_BODY, 2);
    src.commits = vec![
        (1, format!("[{}]", good_commit_json(&sha('1')))),
        (2, format!("[{}]", good_commit_json(&sha('2')))),
    ];
    src.links.push(("commits/1".into(), "<https://x/y?page=1>; rel=\"prev\", <https://x/y?per_page=100&page=2&x=1>; REL=\"first NEXT\"".into()));
    assert_eq!(doc(&exec(&src).unwrap())["summary"]["commits"], 2);
}

#[test]
fn human_output_lists_each_finding() {
    let mut src = clean_source();
    src.detail = detail("t", "x");
    let out = execute(&src, &params(true), NOW).unwrap();
    let text = &out.stdout;
    assert_eq!(out.code, 3);
    for needle in ["pr-blocker-line", "error", "pr-body", "o/r#7"] {
        assert!(text.contains(needle), "{needle} in {text}");
    }
    assert!(
        text.starts_with(&format!(
            "o/r#7 head {}: FAIL, 2 errors, 0 warnings (1 commits, 1 comments read)\n",
            &sha('a')[..12]
        )),
        "{text}"
    );
    assert!(
        text.contains(&format!(
            "error   {:<22} pr-body: no Blocker line
",
            "pr-blocker-line"
        )),
        "{text}"
    );
}

// DEVIATION D5
#[test]
fn human_output_escapes_control_characters() {
    let mut src = clean_source();
    src.commits = vec![(
        1,
        format!(
            "[{}]",
            commit_json(
                &sha('1'),
                "feat: y\n\nCo-Authored-By: C <c@d.e>",
                OWNER_NAME,
                "bad\u{1b}[31m@x.y",
                OWNER_NAME,
                OWNER_EMAIL
            )
        ),
    )];
    let out = execute(&src, &params(true), NOW).unwrap();
    assert!(
        !out.stdout.contains('\u{1b}'),
        "raw escape in {:?}",
        out.stdout
    );
    assert!(out.stdout.contains("\\u{1b}"));
    // a bidirectional override and a line break in a name are escaped too, and a long message is cut
    let mut src = clean_source();
    let name = format!("a\u{202e}b\nc{}", "x".repeat(200));
    src.commits = vec![(
        1,
        format!(
            "[{}]",
            commit_json(
                &sha('1'),
                "feat: y\n\nCo-Authored-By: C <c@d.e>",
                &name,
                OWNER_EMAIL,
                OWNER_NAME,
                OWNER_EMAIL
            )
        ),
    )];
    let out = execute(&src, &params(true), NOW).unwrap();
    assert_eq!(out.stdout.matches('\n').count(), 2, "{:?}", out.stdout);
    assert!(
        !out.stdout.contains('\u{202e}')
            && out.stdout.contains("\\u{202e}")
            && out.stdout.contains("...")
    );
    // the JSON keeps the raw text, JSON-escaped
    let out = exec(&src).unwrap();
    assert!(out.stdout.contains("u{202e}") && !out.stdout.contains('\u{202e}'));
    assert_eq!(out.stdout.matches('\n').count(), 1);
}

#[test]
fn rfc3339_utc_dates() {
    assert_eq!(rfc3339_utc(0), "1970-01-01T00:00:00Z");
    assert_eq!(rfc3339_utc(951_782_400), "2000-02-29T00:00:00Z");
    assert_eq!(rfc3339_utc(1_709_251_199), "2024-02-29T23:59:59Z");
    assert_eq!(rfc3339_utc(1_709_251_200), "2024-03-01T00:00:00Z");
    assert_eq!(rfc3339_utc(4_102_444_799), "2099-12-31T23:59:59Z");
    assert_eq!(rfc3339_utc(NOW), "2025-11-11T00:00:00Z");
}

// ---- Prepare -------------------------------------------------------------------------------------------

fn flags() -> Flags {
    Flags {
        pr: "7".into(),
        repo: Some("o/r".into()),
        path: None,
        owners: vec![OWNER_FLAG_TEXT.into()],
        human: false,
        json: false,
    }
}

#[test]
fn prepare_ok() {
    let p = prepare(&flags()).unwrap();
    assert_eq!(
        (p.repo.as_str(), p.pr, p.owner_source, p.json),
        ("o/r", 7, OWNER_FLAG, false)
    );
    assert_eq!(
        p.owners,
        vec![Identity {
            name: OWNER_NAME.into(),
            email: OWNER_EMAIL.into()
        }]
    );
    let mut f = flags();
    f.json = true;
    assert!(prepare(&f).unwrap().json);
    let mut f = flags();
    f.owners = vec![OWNER_FLAG_TEXT.into(), "  Alt  <alt@example.com> ".into()];
    assert_eq!(
        prepare(&f).unwrap().owners[1],
        Identity {
            name: "Alt".into(),
            email: "alt@example.com".into()
        }
    );
}

#[test]
fn prepare_usage_errors() {
    type Change = fn(&mut Flags);
    let cases: Vec<(&str, Change)> = vec![
        ("pr zero", |f| f.pr = "0".into()),
        ("pr negative", |f| f.pr = "-3".into()),
        ("pr word", |f| f.pr = "x".into()),
        ("pr empty", |f| f.pr = "".into()),
        ("pr fraction", |f| f.pr = "1.5".into()),
        ("repo", |f| f.repo = Some("nope".into())),
        ("repo three parts", |f| f.repo = Some("a/b/c".into())),
        ("repo dot start", |f| f.repo = Some("a/.b".into())),
        ("repo empty owner", |f| f.repo = Some("/b".into())),
        ("identity", |f| f.owners = vec!["Name only".into()]),
        ("identity email", |f| {
            f.owners = vec!["N <not-an-email>".into()]
        }),
        ("identity empty", |f| f.owners = vec![" <a@b.c>".into()]),
        ("both outputs", |f| {
            f.human = true;
            f.json = true;
        }),
        ("junk before", |f| {
            f.owners = vec!["x <y Name <a@b.c>".into()]
        }),
        ("junk after", |f| {
            f.owners = vec!["Name <a@b.c> tail".into()]
        }),
        ("two at signs", |f| f.owners = vec!["N <a@b@c>".into()]),
        ("blank in email", |f| f.owners = vec!["N <a @b.c>".into()]),
        ("gt in name", |f| f.owners = vec!["N> M <a@b.c>".into()]),
        ("path and repo", |f| f.path = Some("/w".into())),
    ];
    for (name, change) in cases {
        let mut f = flags();
        change(&mut f);
        let e = prepare(&f)
            .err()
            .unwrap_or_else(|| panic!("{name}: accepted"));
        assert_eq!(e.kind(), Kind::Usage, "{name}: {e}");
    }
    assert_eq!(parse_identity("A B <a@b.c>").unwrap().name, "A B");
    assert!(
        parse_identity("<b>c <a@b.c>").is_none(),
        "no name before the first <"
    );
}

/// A repository with an origin on github.com and `user.name`/`user.email` in its own config; the process
/// environment is not touched (the tests run in one process), so the config of the user is not under test.
fn workspace(origin: &str, identity: Option<(&str, &str)>) -> Fx {
    let fx = Fx::new();
    fx.strip_identity();
    if let Some((n, e)) = identity {
        fx.set_identity(n, e);
    }
    fx.set_origin_url(origin);
    fx.write(".gitmodules", "");
    fx
}

#[test]
fn prepare_default_identity_from_git_config() {
    let fx = workspace(
        "https://github.com/o/r.git",
        Some((OWNER_NAME, OWNER_EMAIL)),
    );
    let mut f = flags();
    f.repo = None;
    f.path = Some(path_str(&fx.path));
    f.owners.clear();
    let p = prepare(&f).unwrap();
    assert_eq!((p.repo.as_str(), p.owner_source), ("o/r", OWNER_GIT_CONFIG));
    assert_eq!(
        p.owners,
        vec![Identity {
            name: OWNER_NAME.into(),
            email: OWNER_EMAIL.into()
        }]
    );
    // an explicit flag wins and the config is not read
    let fx2 = workspace("https://github.com/o/r.git", None);
    let mut f2 = flags();
    f2.repo = None;
    f2.path = Some(path_str(&fx2.path));
    assert_eq!(prepare(&f2).unwrap().owner_source, OWNER_FLAG);
    // without a configured identity: usage error naming the flag (no global config in this run is assumed
    // only when the environment of the test has none: the CLI test below runs with a clean environment)
}

#[test]
fn prepare_repo_from_workspace() {
    let fx = workspace("git@github.com:o/r.git", Some((OWNER_NAME, OWNER_EMAIL)));
    let mut f = flags();
    f.repo = None;
    f.path = Some(path_str(&fx.path));
    let p = prepare(&f).unwrap();
    assert_eq!(p.repo, "o/r");
    // an origin that is not on github.com gives no repository: the workspace has 0
    let other = workspace(
        "https://example.org/o/r.git",
        Some((OWNER_NAME, OWNER_EMAIL)),
    );
    f.path = Some(path_str(&other.path));
    let e = prepare(&f).err().unwrap();
    assert_eq!(e.kind(), Kind::Usage);
    assert!(e.to_string().contains("--repo"), "{e}");
}

#[test]
fn prepare_two_github_repositories_in_the_workspace_need_repo() {
    let fx = workspace(
        "https://github.com/o/r.git",
        Some((OWNER_NAME, OWNER_EMAIL)),
    );
    fx.write(
        ".gitmodules",
        "[submodule \"s\"]\n\tpath = s\n\turl = https://github.com/o/s.git\n",
    );
    let sub = fx.path.join("s");
    gix::init(&sub).unwrap();
    let mut cfg = std::fs::OpenOptions::new()
        .append(true)
        .open(sub.join(".git/config"))
        .unwrap();
    std::io::Write::write_all(
        &mut cfg,
        b"[remote \"origin\"]\n\turl = https://github.com/o/s.git\n",
    )
    .unwrap();
    let mut f = flags();
    f.repo = None;
    f.path = Some(path_str(&fx.path));
    let e = prepare(&f).err().unwrap();
    assert_eq!(e.kind(), Kind::Usage);
    assert!(e.to_string().contains("has 2 GitHub repositories"), "{e}");
    // --repo names the one that is meant
    f.repo = Some("o/s".into());
    f.path = None;
    assert_eq!(prepare(&f).unwrap().repo, "o/s");
}

// ---- the binary against a fake API (cli/lint_test.go) -------------------------------------------------

const TOKEN: &str = "SECRETTOKEN-0123456789";

fn stw(api: &str, cwd: &std::path::Path, args: &[&str]) -> Output {
    Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_clear()
        .env("HOME", "/nonexistent")
        .env("STW_KEYCHAIN_SERVICE", "stw-test-no-such-service")
        .env("GH_TOKEN", TOKEN)
        .env("STW_GITHUB_API_URL", api)
        .current_dir(cwd)
        .args(["lint", "pr"])
        .args(args)
        .output()
        .unwrap()
}

fn forge(body: &str) -> FakeRoutes {
    let body = body.to_string();
    FakeRoutes::start(move |path, query| {
        let ok = |s: String| (200, vec![], s);
        match (path, query) {
            ("/repos/o/r/pulls/7", "") => ok(format!(
                "{{\"number\":7,\"title\":\"feat: x\",\"body\":{},\"head\":{{\"sha\":\"{}\"}}}}",
                json_string(&body),
                sha('a')
            )),
            ("/repos/o/r/pulls/7/commits", "per_page=100&page=1") => ok(format!(
                "[{}]",
                commit_json(
                    &sha('1'),
                    "feat: x\n\nCo-Authored-By: C <c@d.e>",
                    OWNER_NAME,
                    OWNER_EMAIL,
                    OWNER_NAME,
                    OWNER_EMAIL
                )
            )),
            ("/repos/o/r/issues/7/comments", "per_page=30&page=1") => {
                ok(r#"[{"id":5,"body":"hello"}]"#.into())
            }
            _ => (404, vec![], "{}".into()),
        }
    })
}

fn text(o: &Output) -> (String, String) {
    (
        String::from_utf8_lossy(&o.stdout).into_owned(),
        String::from_utf8_lossy(&o.stderr).into_owned(),
    )
}

const LINT_OK: &str = "Purpose.\nBlocker: none\nAuthor agent: Claude";

#[test]
fn lint_pr_end_to_end_over_the_rest_client() {
    let api = forge(LINT_OK);
    let cwd = scratch();
    let o = stw(
        &api.url,
        cwd.path(),
        &[
            "7",
            "--repo",
            "o/r",
            "--owner-identity",
            OWNER_FLAG_TEXT,
            "--json",
        ],
    );
    let (out, err) = text(&o);
    assert_eq!((o.status.code(), err.as_str()), (Some(0), ""), "{out}");
    let m: Value = serde_json::from_str(&out).unwrap();
    check_schema(&m);
    assert_eq!(
        (
            m["summary"]["passed"].as_bool(),
            m["summary"]["commits"].as_u64(),
            m["summary"]["comments"].as_u64()
        ),
        (Some(true), Some(1), Some(1))
    );
    let want = [
        "GET /repos/o/r/pulls/7 ",
        "GET /repos/o/r/pulls/7/commits?per_page=100&page=1 ",
        "GET /repos/o/r/issues/7/comments?per_page=30&page=1 ",
    ];
    let hits = api.hits();
    assert_eq!(hits.len(), 3, "{hits:?}");
    for (h, w) in hits.iter().zip(want) {
        assert!(h.starts_with(w), "{h} vs {w}");
        assert!(h.ends_with(&format!("Bearer {TOKEN}")), "{h}");
    }
    assert!(!out.contains(TOKEN) && !err.contains(TOKEN));
    // the default is the human list (DEVIATION D1)
    let o = stw(
        &api.url,
        cwd.path(),
        &["7", "--repo", "o/r", "--owner-identity", OWNER_FLAG_TEXT],
    );
    let (out, _) = text(&o);
    assert!(
        out.starts_with(
            "o/r#7 head aaaaaaaaaaaa: pass, 0 errors, 0 warnings (1 commits, 1 comments read)\n"
        ),
        "{out}"
    );
}

#[test]
fn lint_pr_violation_exits_three() {
    let api = forge("no structure");
    let cwd = scratch();
    let o = stw(
        &api.url,
        cwd.path(),
        &[
            "7",
            "--repo",
            "o/r",
            "--owner-identity",
            OWNER_FLAG_TEXT,
            "--json",
        ],
    );
    let (out, err) = text(&o);
    assert_eq!(o.status.code(), Some(3));
    assert!(
        out.contains("\"pr-blocker-line\"") && err.contains("error"),
        "{out} {err}"
    );
}

#[test]
fn lint_pr_usage_and_read_failures() {
    let api = forge(LINT_OK);
    let cwd = scratch();
    let ident = ["--owner-identity", "A <a@b.c>"];
    let cases: Vec<(&str, Vec<&str>)> = vec![
        ("no number", vec![]),
        ("not number", vec!["x", "--repo", "o/r"]),
        (
            "zero",
            [vec!["0", "--repo", "o/r"], ident.to_vec()].concat(),
        ),
        (
            "bad repo",
            [vec!["7", "--repo", "nope"], ident.to_vec()].concat(),
        ),
        (
            "bad ident",
            vec!["7", "--repo", "o/r", "--owner-identity", "nope"],
        ),
        (
            "both output",
            [
                vec!["7", "--repo", "o/r"],
                ident.to_vec(),
                vec!["--human", "--json"],
            ]
            .concat(),
        ),
        (
            "path and repo",
            [vec!["7", "/tmp", "--repo", "o/r"], ident.to_vec()].concat(),
        ),
        ("no owner identity", vec!["7", "--repo", "o/r"]),
        ("no workspace", vec!["7"]),
    ];
    for (name, args) in cases {
        let o = stw(&api.url, cwd.path(), &args);
        let (out, _) = text(&o);
        assert_eq!(o.status.code(), Some(2), "{name}: {out}");
        assert!(out.is_empty(), "{name}: usage error prints no document");
    }
    assert!(api.hits().is_empty(), "a usage error reads nothing");
    // a failed read: exit 1, no document
    let gone = FakeRoutes::start(|_, _| (404, vec![], "{}".into()));
    let o = stw(
        &gone.url,
        cwd.path(),
        &[
            "7",
            "--repo",
            "o/r",
            "--owner-identity",
            "A <a@b.c>",
            "--json",
        ],
    );
    let (out, err) = text(&o);
    assert_eq!((o.status.code(), out.as_str()), (Some(1), ""));
    assert!(err.contains("forbidden"), "{err}");
    // no token: exit 1, no document, no request
    let o = Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_clear()
        .env("HOME", "/nonexistent")
        .env("STW_KEYCHAIN_SERVICE", "stw-test-no-such-service")
        .env("STW_GITHUB_API_URL", &api.url)
        .current_dir(cwd.path())
        .args([
            "lint",
            "pr",
            "7",
            "--repo",
            "o/r",
            "--owner-identity",
            "A <a@b.c>",
        ])
        .output()
        .unwrap();
    let (out, err) = text(&o);
    assert_eq!((o.status.code(), out.as_str()), (Some(1), ""));
    assert!(
        err.contains("hosting-api-unavailable") && err.contains("GH_TOKEN"),
        "{err}"
    );
    assert!(api.hits().is_empty());
}

#[test]
fn lint_pr_status_codes_of_the_api() {
    let cwd = scratch();
    let run = |status: u16, headers: Vec<(String, String)>, body: &'static str| {
        let api = FakeRoutes::start(move |_, _| (status, headers.clone(), body.to_string()));
        let o = stw(
            &api.url,
            cwd.path(),
            &["7", "--repo", "o/r", "--owner-identity", "A <a@b.c>"],
        );
        let (out, err) = text(&o);
        assert_eq!(
            (o.status.code(), out.as_str()),
            (Some(1), ""),
            "{status}: {err}"
        );
        err
    };
    assert!(run(401, vec![], "{}").contains("unauthorized"));
    assert!(run(403, vec![], "{}").contains("forbidden"));
    assert!(run(
        403,
        vec![("X-RateLimit-Remaining".into(), "0".into())],
        "{}"
    )
    .contains("rate-limited"));
    assert!(run(
        403,
        vec![],
        r#"{"message":"You have exceeded a secondary rate limit"}"#
    )
    .contains("rate-limited"));
    // a long body is cut at 4096 bytes, not dropped
    let long = format!(r#"{{"message":"rate limit","pad":"{}"}}"#, "x".repeat(5000));
    assert!(run(403, vec![], Box::leak(long.into_boxed_str())).contains("rate-limited"));
    let late = format!(r#"{{"pad":"{}","message":"rate limit"}}"#, "x".repeat(5000));
    assert!(run(403, vec![], Box::leak(late.into_boxed_str())).contains("forbidden"));
    assert!(run(429, vec![], "{}").contains("rate-limited"));
    assert!(run(500, vec![], "{}").contains("http-5xx"));
    assert!(run(
        301,
        vec![(
            "Location".into(),
            "https://api.github.com/repositories/1/pulls/7".into()
        )],
        "{}"
    )
    .contains("repo-moved"));
    assert!(run(
        302,
        vec![("Location".into(), "https://example.org/".into())],
        "{}"
    )
    .contains("redirects are not followed"));
    assert!(run(200, vec![], "not json").contains("malformed-response"));
    assert!(run(418, vec![], "{}").contains("unexpected HTTP status"));
}

#[test]
fn lint_pr_reads_the_repository_and_identity_of_the_workspace() {
    let api = forge(LINT_OK);
    let fx = workspace("https://github.com/o/r", Some((OWNER_NAME, OWNER_EMAIL)));
    let o = stw(&api.url, &fx.path, &["7", "--json"]);
    let (out, err) = text(&o);
    assert_eq!(o.status.code(), Some(0), "{out} {err}");
    let m: Value = serde_json::from_str(&out).unwrap();
    assert_eq!(
        (m["repo"].as_str(), m["options"]["owner_source"].as_str()),
        (Some("o/r"), Some("git-config"))
    );
    assert_eq!(m["options"]["owner_identities"][0], OWNER_FLAG_TEXT);
    // the same through PATH from another directory
    let cwd = scratch();
    let o = stw(&api.url, cwd.path(), &["7", &path_str(&fx.path), "--json"]);
    assert_eq!(o.status.code(), Some(0), "{}", text(&o).1);
    // without user.name and user.email: usage
    let bare = workspace("https://github.com/o/r", None);
    let o = stw(&api.url, &bare.path, &["7"]);
    let (_, err) = text(&o);
    assert_eq!(o.status.code(), Some(2), "{err}");
    assert!(err.contains("--owner-identity"), "{err}");
}

// DEVIATION D9 is the Go behavior: only comment pages report an oversized body as page-too-large
#[test]
fn oversized_bodies_and_304_map_to_the_go_codes() {
    const MAX: usize = 1 << 20;
    let cwd = scratch();
    // valid JSON of exactly `n` bytes
    let comments = |n: usize| {
        let (a, b) = ("[{\"id\":5,\"body\":\"", "\"}]");
        format!("{a}{}{b}", "x".repeat(n - a.len() - b.len()))
    };
    let pull = |n: usize| {
        let (a, b) = ("{\"title\":\"", "\"}");
        format!("{a}{}{b}", "x".repeat(n - a.len() - b.len()))
    };
    assert_eq!((comments(MAX).len(), pull(MAX + 1).len()), (MAX, MAX + 1));
    let run = |target: &'static str, status: u16, body: String| {
        let api = FakeRoutes::start(move |path, _| {
            let hit = match target {
                "pull" => path == "/repos/o/r/pulls/7",
                "commits" => path == "/repos/o/r/pulls/7/commits",
                _ => path == "/repos/o/r/issues/7/comments",
            };
            if hit {
                return (status, vec![], body.clone());
            }
            match path {
                "/repos/o/r/pulls/7" => (
                    200,
                    vec![],
                    format!(
                        "{{\"number\":7,\"title\":\"feat: x\",\"body\":\"b\",\"head\":{{\"sha\":\"{}\"}}}}",
                        sha('a')
                    ),
                ),
                "/repos/o/r/pulls/7/commits" => (200, vec![], "[]".into()),
                _ => (404, vec![], "{}".into()),
            }
        });
        let args = ["7", "--repo", "o/r", "--owner-identity", "A <a@b.c>"];
        let o = stw(&api.url, cwd.path(), &args);
        let (out, err) = text(&o);
        (o.status.code(), out, err)
    };
    // a refused read is a runtime error: exit 1, nothing on stdout
    let refused = |target: &'static str, status: u16, body: String| {
        let (code, out, err) = run(target, status, body);
        assert_eq!((code, out.as_str()), (Some(1), ""), "{target}: {err}");
        err
    };
    let comment_msg = "page-too-large: list page exceeds the one MiB body limit";
    let other_msg = "malformed-response: response exceeds the one MiB body limit";
    // exactly one MiB is accepted
    for (target, body) in [
        ("comments", comments(MAX)),
        ("commits", comments(MAX)),
        ("pull", pull(MAX)),
    ] {
        let e = run(target, 200, body).2;
        assert!(!e.contains("MiB"), "{target}: {e}");
    }
    // one byte more is not
    let e = refused("comments", 200, comments(MAX + 1));
    assert!(e.contains(comment_msg), "{e}");
    let e = refused("commits", 200, comments(MAX + 1));
    assert!(
        e.contains(other_msg) && !e.contains("page-too-large"),
        "{e}"
    );
    let e = refused("pull", 200, pull(MAX + 1));
    assert!(
        e.contains(other_msg) && !e.contains("page-too-large"),
        "{e}"
    );
    let e = refused("pull", 304, String::new());
    assert!(
        e.contains("malformed-response") && e.contains("unsolicited 304 response"),
        "{e}"
    );
}
