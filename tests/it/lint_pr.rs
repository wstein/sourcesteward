//! Go corpus check for M8, rules: every case of `legacy/sourcesteward/internal/lintpr/lintpr_test.go` is one Rust
//! case here (same scenario, same name in snake case, except one rename:
//! `TestParseReviewLine` is `parse_review_line_fields`). The command part (`lintprcmd`, `cli/lint_test.go`) is in
//! `lint_pr_cmd.rs`. Plan: docs/migration-plan.md, M8.
//!
//! Deviations of Rust from Go, rules (each is tested below where marked `DEVIATION`):
//! - D1 no regex crate: the expressions are written out by hand. Case-insensitive matching folds with lower case
//!   plus U+017F and U+212A (Go's simple folding for the letters that occur); other Unicode folds that Go knows
//!   are not applied. The finding for a case-folded email compares with the same fold.
//! - D2 the review grammar is the one of the M3 parser (`review::parse_strict`) restricted to the lint grammar of
//!   Go: `pr=` is a number of 1 to 9 digits without leading zero; `pr=none` is no review line here (as in Go).
//! - D3 the author/committer message quotes the name with Rust's `{:?}`, Go uses `%q`; they differ for non-ASCII
//!   and control characters only.
//! - D4 `TestRulesTableAnchorsInAgentRules` is ported (`rules_table_anchors_in_agent_rules`); it runs only with
//!   AGENT_RULES_DIR set, as in Go.
use stw::lint_pr::{
    self, lint, parse_review_line, Comment, Commit, Finding, Identity, Input, Severity,
};
use stw::review::Line;

// The session patterns are assembled so that this file does not contain them in plain text.
const SESSION_TRAILER: &str = concat!("Claude", "-Session: ", "https://example.invalid/x");
const SESSION_URL: &str = concat!(
    "see https://claude",
    ".ai/code/session_",
    "01ABC for details"
);

const HEAD_SHA: &str = "0123456789abcdef0123456789abcdef01234567";
const PATCH_ID: &str = "89abcdef0123456789abcdef0123456789abcdef";
const OWNER: &str = "Octo Owner";
const OWNER_M: &str = "octo@example.com";

fn a40() -> String {
    "a".repeat(40)
}

fn owners() -> Vec<Identity> {
    vec![Identity {
        name: OWNER.into(),
        email: OWNER_M.into(),
    }]
}

fn good_commit() -> Commit {
    Commit {
        sha: a40(),
        message: "feat(lint): add the thing\n\nBody.\n\nCo-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>".into(),
        author_name: OWNER.into(),
        author_email: OWNER_M.into(),
        committer_name: OWNER.into(),
        committer_email: OWNER_M.into(),
    }
}

fn good_body() -> String {
    "Adds the thing.\n\nChanges\n- one\n\nTests: 3 passed\n\nNot verified: nothing\nBlocker: Independent review pending\nCloses #1\n\nAuthor agent: Claude".into()
}

fn good_input() -> Input {
    Input {
        pr: 7,
        title: "feat: x".into(),
        body: good_body(),
        commits: vec![good_commit()],
        comments: vec![],
    }
}

fn comment(id: i64, body: impl Into<String>) -> Comment {
    Comment {
        id,
        body: body.into(),
    }
}

fn ids(fs: &[Finding]) -> Vec<&'static str> {
    fs.iter().map(|f| f.rule).collect()
}

fn has(fs: &[Finding], rule: &str) -> bool {
    fs.iter().any(|f| f.rule == rule)
}

fn lines(n: usize) -> String {
    (0..n)
        .map(|i| format!("line {i}"))
        .collect::<Vec<_>>()
        .join("\n")
}

#[test]
fn clean_input_has_no_findings() {
    let fs = lint(&good_input(), &owners());
    assert!(fs.is_empty(), "findings on clean input: {fs:?}");
}

#[test]
fn session_link_every_location() {
    for bad in [
        SESSION_TRAILER.to_string(),
        SESSION_URL.to_string(),
        SESSION_TRAILER.to_uppercase(),
    ] {
        let mut input = good_input();
        input.commits[0].message += &format!("\n{bad}");
        input.body += &format!("\n{bad}");
        input.title += &format!(" {bad}");
        input.comments = vec![comment(99, format!("Review ok\n{bad}"))];
        let fs = lint(&input, &owners());
        let mut kinds = std::collections::BTreeMap::new();
        for f in fs.iter().filter(|f| f.rule == lint_pr::SESSION_LINK) {
            kinds.insert(f.location.kind, f.location.id.clone());
        }
        assert_eq!(kinds.get("commit"), Some(&a40()), "{bad:?}: {kinds:?}");
        assert_eq!(kinds.get("comment"), Some(&"99".to_string()), "{bad:?}");
        assert_eq!(kinds.get("pr-body"), Some(&String::new()), "{bad:?}");
        assert_eq!(kinds.len(), 4, "{bad:?}: {kinds:?}");
    }
}

#[test]
fn session_link_reports_line() {
    let mut input = good_input();
    input.body = format!("a\nb\n{SESSION_URL}");
    let fs = lint(&input, &owners());
    let hits: Vec<_> = fs
        .iter()
        .filter(|f| f.rule == lint_pr::SESSION_LINK)
        .collect();
    assert_eq!(hits.len(), 1);
    assert_eq!(hits[0].location.line, 3);
}

// DEVIATION D1: Go's (?i) folds U+017F to s and U+212A to k; so does the port.
#[test]
fn session_link_case_folding_like_go() {
    let mut input = good_input();
    input.body = concat!("claude", "-ſeſſion: x").into();
    assert!(has(&lint(&input, &owners()), lint_pr::SESSION_LINK));
    // a different letter is no match
    input.body = concat!("claude", "-sessiom: x").into();
    assert!(!has(&lint(&input, &owners()), lint_pr::SESSION_LINK));
    // CRLF text: the line is found and numbered like Go (CRLF counts as one break)
    input.body = format!("a\r\nb\r\n{SESSION_TRAILER}\r\n");
    let fs = lint(&input, &owners());
    let hit = fs.iter().find(|f| f.rule == lint_pr::SESSION_LINK).unwrap();
    assert_eq!(hit.location.line, 3);
}

#[test]
fn identity() {
    let mut input = good_input();
    input.commits[0].author_name = "Someone Else".into();
    let fs = lint(&input, &owners());
    assert!(
        has(&fs, lint_pr::AUTHOR_IDENTITY) && !has(&fs, lint_pr::COMMITTER_IDENTITY),
        "author only: {:?}",
        ids(&fs)
    );
    let mut input = good_input();
    input.commits[0].committer_email = "bot@example.com".into();
    let fs = lint(&input, &owners());
    assert!(
        !has(&fs, lint_pr::AUTHOR_IDENTITY) && has(&fs, lint_pr::COMMITTER_IDENTITY),
        "committer only: {:?}",
        ids(&fs)
    );
    let mut input = good_input();
    input.commits[0].author_email = OWNER_M.to_uppercase();
    assert!(
        !has(&lint(&input, &owners()), lint_pr::AUTHOR_IDENTITY),
        "email is case-insensitive"
    );
    let mut input = good_input();
    input.commits[0].author_name = OWNER.to_uppercase();
    assert!(
        has(&lint(&input, &owners()), lint_pr::AUTHOR_IDENTITY),
        "name is exact"
    );
    // a second configured owner identity is accepted
    let mut input = good_input();
    input.commits[0].author_name = "Alt".into();
    input.commits[0].author_email = "alt@example.com".into();
    let mut two = owners();
    two.push(Identity {
        name: "Alt".into(),
        email: "alt@example.com".into(),
    });
    assert!(!has(&lint(&input, &two), lint_pr::AUTHOR_IDENTITY));
    // the finding names role, name and email, located at the commit without a line
    let mut input = good_input();
    input.commits[0].author_name = "Someone Else".into();
    let f = &lint(&input, &owners())[0];
    assert_eq!(
        f.message,
        "author \"Someone Else\" <octo@example.com> is not an owner identity"
    );
    assert_eq!(
        (f.location.kind, f.location.id.as_str(), f.location.line),
        ("commit", a40().as_str(), 0)
    );
}

#[test]
fn no_owner_configured_skips_identity_rules() {
    let fs = lint(&good_input(), &[]);
    assert!(!has(&fs, lint_pr::AUTHOR_IDENTITY) && !has(&fs, lint_pr::COMMITTER_IDENTITY));
    // a foreign identity passes without owners, and fails with them
    let mut input = good_input();
    input.commits[0].author_name = "x".into();
    assert!(lint(&input, &[]).is_empty());
    assert!(!lint(&input, &owners()).is_empty());
}

#[test]
fn co_authored_by() {
    for msg in [
        "feat: x\n\nBody only.",
        "feat: x\n\nCo-Authored-By: no email here",
        "feat: x\n\nsee Co-Authored-By: Name <a@b.c> inline",
    ] {
        let mut input = good_input();
        input.commits[0].message = msg.into();
        assert!(
            has(&lint(&input, &owners()), lint_pr::CO_AUTHORED_BY),
            "{msg:?} passed"
        );
    }
    let mut input = good_input();
    input.commits[0].message = "feat: x\n\nco-authored-by: Name <a@b.c>\r\n".into();
    assert!(
        !has(&lint(&input, &owners()), lint_pr::CO_AUTHORED_BY),
        "lower-case trailer with CRLF refused"
    );
}

// The edges of the hand-written trailer expression `^co-authored-by:[ \t]*[^<\r\n]+<[^<>\s]+@[^<>\s]+>[ \t]*\r?$`.
#[test]
fn co_authored_by_edges() {
    let ok = |trailer: &str| {
        let mut input = good_input();
        input.commits[0].message = format!("feat: x\n\n{trailer}\n\nmore");
        !has(&lint(&input, &owners()), lint_pr::CO_AUTHORED_BY)
    };
    for good in [
        "Co-Authored-By: N <a@b>",
        "CO-AUTHORED-BY:N<a@b>",
        "Co-Authored-By:\tName <a@b.c> \t",
        "Co-Authored-By: Name > x <a@b.c>",
        "Co-Authored-By: N <a@b@c>",
        // the blank after the colon is a name character: Go accepts a blank name
        "Co-Authored-By: <a@b.c>",
        "Co-Authored-By:  <a@b.c>",
    ] {
        assert!(ok(good), "{good:?} refused");
    }
    for bad in [
        "Co-Authored-By:<a@b.c>",
        "Co-Authored-By: N <@b.c>",
        "Co-Authored-By: N <a@>",
        "Co-Authored-By: N <ab.c>",
        "Co-Authored-By: N <a @b.c>",
        "Co-Authored-By: N <a@b.c> tail",
        "Co-Authored-By: N <a@b.c>>",
        "Co-Authored-By: N <<a@b.c>",
        " Co-Authored-By: N <a@b.c>",
        "Co-Authored-By: N\r M <a@b.c>",
    ] {
        assert!(!ok(bad), "{bad:?} passed");
    }
}

#[test]
fn conventional_commit() {
    let bad = [
        "Add the thing",
        "feat add: x",
        "feat(): x",
        "feature: x",
        "feat:x",
        "fix(scope) x",
        "feat: ",
        "Feat: x",
        "Add feat: x",
        "WIP fix(a): x",
    ];
    for subject in bad {
        let mut input = good_input();
        input.commits[0].message = format!("{subject}\n\nCo-Authored-By: C <c@d.e>");
        assert!(
            has(&lint(&input, &owners()), lint_pr::CONVENTIONAL_COMMIT),
            "{subject:?} passed"
        );
    }
    for subject in [
        "fix: x",
        "docs(spec): x",
        "test(ci)!: x",
        "refactor(a/b-c.d): x",
        "chore: x",
    ] {
        let mut input = good_input();
        input.commits[0].message = format!("{subject}\n\nCo-Authored-By: C <c@d.e>");
        assert!(
            !has(&lint(&input, &owners()), lint_pr::CONVENTIONAL_COMMIT),
            "{subject:?} refused"
        );
    }
}

// The types of the table and the remaining edges of `\A(type)(\(scope\))?!?: \S`.
#[test]
fn conventional_commit_edges() {
    let ok = |subject: &str| {
        let mut input = good_input();
        input.commits[0].message = format!("{subject}\n\nCo-Authored-By: C <c@d.e>");
        !has(&lint(&input, &owners()), lint_pr::CONVENTIONAL_COMMIT)
    };
    for t in [
        "feat", "fix", "docs", "style", "refactor", "perf", "test", "build", "ci", "chore",
        "revert",
    ] {
        assert!(ok(&format!("{t}: x")), "{t}");
        assert!(ok(&format!("{t}(s)!: x")), "{t} scope bang");
    }
    assert!(ok("fix!: x"));
    assert!(ok("fix: é"));
    assert!(!ok("fix(a b): x"), "blank in scope");
    assert!(!ok("fix(a)(b): x"));
    assert!(!ok("fix(: x"));
    assert!(!ok("fix !: x"));
    assert!(
        !ok("fix:  x"),
        "two blanks: the char after one blank is a blank"
    );
    assert!(!ok("fix:\tx"));
    // a CR at the end of the subject (CRLF message) is cut, as in Go
    assert!(ok("fix: x\r"));
    assert!(!ok("fix: \r"));
}

#[test]
fn body_length() {
    let base = "Blocker: x\nAuthor agent: Claude\n";
    let check = |total: usize, want: Option<Severity>| {
        let mut input = good_input();
        input.body = format!("{base}{}", lines(total - 2));
        let got = lint(&input, &owners())
            .into_iter()
            .find(|f| f.rule == lint_pr::BODY_LENGTH)
            .map(|f| f.severity);
        assert_eq!(got, want, "{total} lines");
    };
    check(25, None);
    check(26, Some(Severity::Warning));
    check(30, Some(Severity::Warning));
    check(31, Some(Severity::Error));
    // trailing blank lines and CRLF do not count
    let mut input = good_input();
    input.body = format!("{base}{}", lines(23)).replace('\n', "\r\n") + "\r\n\r\n\r\n";
    assert!(!has(&lint(&input, &owners()), lint_pr::BODY_LENGTH));
    // the messages name the count and the limit
    let mut input = good_input();
    input.body = format!("{base}{}", lines(28));
    let f = lint(&input, &owners())
        .into_iter()
        .find(|f| f.rule == lint_pr::BODY_LENGTH)
        .unwrap();
    assert_eq!(f.message, "30 lines, about 25 expected");
    input.body = format!("{base}{}", lines(40));
    let f = lint(&input, &owners())
        .into_iter()
        .find(|f| f.rule == lint_pr::BODY_LENGTH)
        .unwrap();
    assert_eq!(f.message, "42 lines, more than 30");
}

#[test]
fn blocker_and_author_agent_lines() {
    let mut input = good_input();
    input.body = "Purpose.\nAuthor agent: Claude".into();
    let fs = lint(&input, &owners());
    assert!(
        has(&fs, lint_pr::BLOCKER_LINE) && !has(&fs, lint_pr::AUTHOR_AGENT_LINE),
        "{:?}",
        ids(&fs)
    );
    input.body = "Purpose.\nBlocker: keiner".into();
    let fs = lint(&input, &owners());
    assert!(
        !has(&fs, lint_pr::BLOCKER_LINE) && has(&fs, lint_pr::AUTHOR_AGENT_LINE),
        "{:?}",
        ids(&fs)
    );
    input.body = "Purpose mentions blocker in prose.\nan Author agent: later in a line".into();
    let fs = lint(&input, &owners());
    assert!(
        has(&fs, lint_pr::BLOCKER_LINE) && has(&fs, lint_pr::AUTHOR_AGENT_LINE),
        "mid-line mentions counted: {:?}",
        ids(&fs)
    );
    input.body = "**Blocker**: none\n- Author agent: Claude".into();
    let fs = lint(&input, &owners());
    assert!(
        !has(&fs, lint_pr::BLOCKER_LINE) && !has(&fs, lint_pr::AUTHOR_AGENT_LINE),
        "markdown prefixes refused: {:?}",
        ids(&fs)
    );
}

// The word boundary and the prefix class of `^[\s*_#>-]*blockers?\b` and `...author agent\b`.
#[test]
fn blocker_and_author_agent_edges() {
    let miss = |body: &str, rule: &str| {
        let mut input = good_input();
        input.body = body.into();
        has(&lint(&input, &owners()), rule)
    };
    assert!(!miss("Blockers: none", lint_pr::BLOCKER_LINE));
    assert!(!miss("BLOCKER", lint_pr::BLOCKER_LINE));
    assert!(!miss("\n\n  > ## _Blocker", lint_pr::BLOCKER_LINE));
    assert!(
        miss("## _Blocker_", lint_pr::BLOCKER_LINE),
        "an underscore after the word is no boundary"
    );
    assert!(!miss("x\r\nBlocker: y", lint_pr::BLOCKER_LINE), "CRLF text");
    assert!(
        !miss("Blocker-free", lint_pr::BLOCKER_LINE),
        "a hyphen is a boundary"
    );
    assert!(miss("Blockerx", lint_pr::BLOCKER_LINE));
    assert!(miss("Blockersx", lint_pr::BLOCKER_LINE));
    assert!(miss("Blocker_x", lint_pr::BLOCKER_LINE));
    assert!(
        miss("`Blocker`", lint_pr::BLOCKER_LINE),
        "a backtick is not in the prefix class"
    );
    assert!(miss("1. Blocker", lint_pr::BLOCKER_LINE));
    // DEVIATION D1: U+017F is an `s` for the case folding but no word character for `\b`, as in Go
    assert!(!miss("Blockerſession", lint_pr::BLOCKER_LINE));
    assert!(!miss("Blockerſ x", lint_pr::BLOCKER_LINE));
    assert!(!miss("Blockerſs", lint_pr::BLOCKER_LINE));
    assert!(miss("Blockerss", lint_pr::BLOCKER_LINE));
    assert!(!miss("BLOCKERS", lint_pr::BLOCKER_LINE));
    assert!(!miss("author agent", lint_pr::AUTHOR_AGENT_LINE));
    assert!(
        miss("Author  agent: x", lint_pr::AUTHOR_AGENT_LINE),
        "one blank only"
    );
    assert!(miss("Author agents: x", lint_pr::AUTHOR_AGENT_LINE));
    assert!(miss("Author-agent: x", lint_pr::AUTHOR_AGENT_LINE));
}

#[test]
fn review_line_format() {
    let good = format!("Review: CLEAR head={HEAD_SHA} patch-id={PATCH_ID} pr=7");
    let carried = format!(
        "Carried-over: from={HEAD_SHA} to={} patch-id={PATCH_ID} pr=7",
        "b".repeat(40)
    );
    let findings = format!("Review: FINDINGS{}", &good["Review: CLEAR".len()..]);
    for body in [
        good.clone(),
        format!("{good}\nrest"),
        carried.clone(),
        findings,
    ] {
        let mut input = good_input();
        input.comments = vec![comment(1, body.clone())];
        let fs = lint(&input, &owners());
        assert!(fs.is_empty(), "{body:?}: {fs:?}");
    }
    let bad: Vec<(&str, String)> = vec![
        ("markdown", format!("**{good}**")),
        ("colon only", "Review: CLEAR".into()),
        (
            "short sha",
            format!("Review: CLEAR head=abc patch-id={PATCH_ID} pr=7"),
        ),
        (
            "upper hex",
            format!(
                "Review: CLEAR head={} patch-id={PATCH_ID} pr=7",
                HEAD_SHA.to_uppercase()
            ),
        ),
        (
            "order",
            format!("Review: CLEAR patch-id={PATCH_ID} head={HEAD_SHA} pr=7"),
        ),
        ("extra", format!("{good} extra")),
        ("crlf", format!("{good}\r\nrest")),
        ("wrong pr", good.replacen("pr=7", "pr=8", 1)),
        ("verdict", good.replacen("CLEAR", "OK", 1)),
        (
            "patch short",
            format!(
                "Review: CLEAR head={HEAD_SHA} patch-id={} pr=7",
                &PATCH_ID[..20]
            ),
        ),
        (
            "carried order",
            format!("Carried-over: to={HEAD_SHA} from={HEAD_SHA} patch-id={PATCH_ID} pr=7"),
        ),
        (
            "long sha",
            format!("Review: CLEAR head={HEAD_SHA}0 patch-id={PATCH_ID} pr=7"),
        ),
        (
            "long patch",
            format!("Review: CLEAR head={HEAD_SHA} patch-id={PATCH_ID}0 pr=7"),
        ),
        ("bold key", format!("**Review**: CLEAR head={HEAD_SHA}")),
        ("no colon", format!("Review CLEAR head={HEAD_SHA}")),
        ("no colon FIND", "review findings".into()),
        ("heading only", "## Review".into()),
        ("heading colon", "### Review:".into()),
        ("carried bold", format!("**Carried-over**: from={HEAD_SHA}")),
        ("leading blank", format!("\n{good}")),
        ("leading space", format!(" {good}")),
        ("lowercase word", good.replacen("Review", "review", 1)),
    ];
    for (name, body) in bad {
        let mut input = good_input();
        input.comments = vec![comment(5, body.clone())];
        let found = lint(&input, &owners()).iter().any(|f| {
            f.rule == lint_pr::REVIEW_LINE && f.location.kind == "comment" && f.location.id == "5"
        });
        assert!(found, "{name}: {body:?} passed");
    }
    // a comment that is not a review is not checked
    let mut input = good_input();
    input.comments = vec![
        comment(2, "Thanks, looks fine\nReview: later"),
        comment(3, "Review of the approach is below."),
        comment(4, format!("Review clearly shows the gap\n{}", lines(30))),
        comment(5, format!("Review findingsheet attached\n{}", lines(30))),
    ];
    let fs = lint(&input, &owners());
    assert!(fs.is_empty(), "non-review comments: {fs:?}");
}

// The folded keywords of the review claim, with the same boundary rule.
#[test]
fn review_claim_case_folding_and_boundary() {
    let claims = |body: &str| {
        let mut input = good_input();
        input.comments = vec![comment(9, format!("{body}\n{}", lines(20)))];
        has(&lint(&input, &owners()), lint_pr::REVIEW_LENGTH)
    };
    assert!(claims("REVIEW CLEAR"));
    assert!(
        !claims("Review findingſ"),
        "no boundary between U+017F and the end"
    );
    assert!(claims("Review findingſs"));
    assert!(claims("Review findings"));
    assert!(claims("> `Carried-over`"));
    assert!(claims("\u{a0}\n \n  Review:"), "blank lines first");
    assert!(!claims("Reviewed: x"));
    assert!(!claims("Review clearly"));
    assert!(!claims("x Review: CLEAR"));
}

// DEVIATION D2: `pr=none` and 10-digit numbers are no review lines for lint; pr=0 and leading zeros neither.
#[test]
fn review_line_pr_field() {
    for pr in ["none", "0", "07", "1234567890", "-1", "", "7x"] {
        let mut input = good_input();
        input.comments = vec![comment(
            5,
            format!("Review: CLEAR head={HEAD_SHA} patch-id={PATCH_ID} pr={pr}"),
        )];
        // not a review line at all (not merely another pull request)
        let fs = lint(&input, &owners());
        assert_eq!(fs.len(), 1, "pr={pr}: {fs:?}");
        assert_eq!(
            (fs[0].rule, fs[0].message.as_str()),
            (
                lint_pr::REVIEW_LINE,
                "first line does not follow the review line grammar"
            ),
            "pr={pr}"
        );
    }
    // nine digits are fine (and then name another pull request)
    let mut input = good_input();
    input.pr = 123_456_789;
    input.comments = vec![comment(
        5,
        format!("Review: CLEAR head={HEAD_SHA} patch-id={PATCH_ID} pr=123456789"),
    )];
    assert!(lint(&input, &owners()).is_empty());
    // the message of a wrong number names both
    let mut input = good_input();
    input.comments = vec![comment(
        5,
        format!("Review: CLEAR head={HEAD_SHA} patch-id={PATCH_ID} pr=8"),
    )];
    let f = &lint(&input, &owners())[0];
    assert_eq!(f.message, "pr=8, this is pull request 7");
    assert_eq!(
        (f.location.kind, f.location.id.as_str(), f.location.line),
        ("comment", "5", 1)
    );
}

#[test]
fn review_comment_length() {
    let first = format!("Review: CLEAR head={HEAD_SHA} patch-id={PATCH_ID} pr=7\n");
    for (n, want) in [(15usize, false), (16, true), (58, true)] {
        let mut input = good_input();
        input.comments = vec![comment(4, format!("{first}{}", lines(n - 1)))];
        let got = has(&lint(&input, &owners()), lint_pr::REVIEW_LENGTH);
        assert_eq!(got, want, "{n} lines");
    }
    // a long comment that is not a review is not limited
    let mut input = good_input();
    input.comments = vec![comment(4, lines(60))];
    assert!(!has(&lint(&input, &owners()), lint_pr::REVIEW_LENGTH));
    // the other spellings of a review comment are limited too
    for key in [
        "**Review**: CLEAR",
        "Review CLEAR",
        "## Review",
        "review findings",
        "**Carried-over**: x",
    ] {
        input.comments = vec![comment(4, format!("{key}\n{}", lines(20)))];
        assert!(
            has(&lint(&input, &owners()), lint_pr::REVIEW_LENGTH),
            "{key:?}: long review not limited"
        );
    }
    // a malformed review line is still a review comment for the length rule
    input.comments = vec![comment(4, format!("Review: CLEAR\n{}", lines(20)))];
    let fs = lint(&input, &owners());
    assert!(
        has(&fs, lint_pr::REVIEW_LENGTH) && has(&fs, lint_pr::REVIEW_LINE),
        "{:?}",
        ids(&fs)
    );
    let f = fs
        .iter()
        .find(|f| f.rule == lint_pr::REVIEW_LENGTH)
        .unwrap();
    assert_eq!(f.message, "21 lines, at most 15");
    assert_eq!(f.location.line, 0);
}

#[test]
fn parse_review_line_fields() {
    let l = parse_review_line(&format!(
        "Review: FINDINGS head={HEAD_SHA} patch-id={PATCH_ID} pr=12"
    ));
    match l {
        Some(Line::Review {
            verdict,
            head,
            patch_id,
            pr,
        }) => {
            assert_eq!(verdict, stw::review::Verdict::Findings);
            assert_eq!((head.as_str(), patch_id.as_str()), (HEAD_SHA, PATCH_ID));
            assert_eq!(pr, stw::review::PrRef::Number(12));
        }
        other => panic!("{other:?}"),
    }
    let c = "c".repeat(40);
    match parse_review_line(&format!(
        "Carried-over: from={HEAD_SHA} to={c} patch-id={PATCH_ID} pr=3"
    )) {
        Some(Line::Carried { from, to, pr, .. }) => {
            assert_eq!((from.as_str(), to.as_str()), (HEAD_SHA, c.as_str()));
            assert_eq!(pr, stw::review::PrRef::Number(3));
        }
        other => panic!("{other:?}"),
    }
    assert!(parse_review_line("Review: CLEAR head=x patch-id=y pr=1").is_none());
    assert!(parse_review_line(&format!(
        "Review: CLEAR head={HEAD_SHA} patch-id={PATCH_ID} pr=0"
    ))
    .is_none());
}

#[test]
fn findings_are_ordered_by_rule_table() {
    let mut input = good_input();
    input.body = "x".into();
    input.commits[0].message = "bad subject".into();
    input.commits[0].author_name = "Other".into();
    let fs = lint(&input, &owners());
    let order = |id: &str| lint_pr::RULES.iter().position(|r| r.id == id).unwrap();
    let mut last = 0;
    for f in &fs {
        assert!(order(f.rule) >= last, "not ordered: {:?}", ids(&fs));
        last = order(f.rule);
    }
    // all five rules fire here; the table order is the report order
    assert_eq!(
        ids(&fs),
        vec![
            "author-identity",
            "co-authored-by",
            "conventional-commit",
            "pr-blocker-line",
            "pr-author-agent-line"
        ]
    );
    // within a rule the input order holds
    let mut input = good_input();
    let mut second = good_commit();
    second.sha = "b".repeat(40);
    second.message = "bad".into();
    let mut first = good_commit();
    first.sha = "c".repeat(40);
    first.message = "bad".into();
    input.commits = vec![first, second];
    let fs: Vec<_> = lint(&input, &owners())
        .into_iter()
        .filter(|f| f.rule == lint_pr::CONVENTIONAL_COMMIT)
        .map(|f| f.location.id)
        .collect();
    assert_eq!(fs, vec!["c".repeat(40), "b".repeat(40)]);
}

// The rule table follows the agent-rules core. Every rule names its source file and an exact anchor phrase of
// that file. With AGENT_RULES_DIR (a checkout of agent-rules) the anchors are looked up; without it the check is
// skipped, so a change of the core is noticed on the next run that has the checkout.
#[test]
fn rules_table_shape() {
    let mut seen = std::collections::BTreeSet::new();
    for r in lint_pr::RULES.iter() {
        assert!(
            !r.id.is_empty() && !r.summary.is_empty() && !r.source.is_empty() && seen.insert(r.id),
            "{}",
            r.id
        );
        if r.source != "sourcesteward" {
            assert!(
                !r.anchor.is_empty(),
                "{}: a core rule needs an anchor",
                r.id
            );
        }
    }
    assert_eq!(lint_pr::rule(lint_pr::REVIEW_LENGTH).limit, 15);
    assert_eq!(lint_pr::rule(lint_pr::BODY_LENGTH).limit, 25);
    assert_eq!(lint_pr::rule(lint_pr::BODY_LENGTH).hard, 30);
    assert_eq!(
        lint_pr::RULES.iter().map(|r| r.id).collect::<Vec<_>>(),
        [
            "session-link",
            "author-identity",
            "committer-identity",
            "co-authored-by",
            "conventional-commit",
            "pr-body-length",
            "pr-blocker-line",
            "pr-author-agent-line",
            "review-line-format",
            "review-comment-length"
        ]
    );
}

#[test]
fn rules_table_anchors_in_agent_rules() {
    let Some(dir) = std::env::var_os("AGENT_RULES_DIR") else {
        eprintln!("AGENT_RULES_DIR is not set: the rule anchors are not checked");
        return;
    };
    for r in lint_pr::RULES.iter() {
        if r.source == "sourcesteward" {
            continue;
        }
        let text = std::fs::read_to_string(std::path::Path::new(&dir).join(r.source))
            .unwrap_or_else(|e| panic!("{}: {e}", r.id));
        assert!(
            text.contains(r.anchor),
            "{}: anchor {:?} not found in {}",
            r.id,
            r.anchor,
            r.source
        );
    }
}
