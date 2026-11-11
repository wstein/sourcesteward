//! Go corpus check for M3: every case of `legacy/sourcesteward/internal/reviewstate/{parse,state}_test.go` is one
//! Rust case here (same scenario, named after the Go case) or a named deviation. Plan: docs/migration-plan.md,
//! "Cutover and archive of legacy/sourcesteward", slice 1, step 1.
//!
//! Deviations of Rust from Go (each case below that shows one is marked `DEVIATION`):
//! - D1 grammar: Go takes a 64-hex patch-id, a trailing CR and PR numbers of at most 9 digits, and has no `pr=none`.
//!   Rust: exactly 40 hex, no CR (a variant), any decimal u64 without leading zero, `pr=none` for local reviews.
//! - D2 legacy words: Go reports `Verdict:`, `Urteil:`, `Findings:`, `review: ...` as unparsed (state `unparsed`).
//!   Rust ignores them (not a review line, never a verdict); only lines with review keys are variants.
//! - D3 states: Go has `none`, `clear`, `findings`, `clear-stale`, `clear-carried`, `unparsed`. Rust has `no-review`
//!   (also for a CLEAR of an older head, with the finding `stale-head`), `clear`, `findings`, `clear-carried`,
//!   `unverified-author` and `unknown` (instead of `unparsed`).
//! - D4 head time: Go ignores comments older than the head commit and fails closed without a head time. Rust is pure
//!   (no clock, no commit read); comments are not compared with the head commit.
//! - D5 trust: Go decides by `author_association` (OWNER, MEMBER, COLLABORATOR). Rust takes a list of logins.
//! - D6 chains: Go follows at most 8 links and ends a chain at a repeated head. Rust follows 16 links and a link back
//!   to a head the chain already cleared is invalid (`repeated-head`).
//! - D8 findings after a carry-over: Go checks a carry-over at its own time; Rust invalidates a chain through a head
//!   that got FINDINGS after it was cleared, wherever the FINDINGS lie in the thread.
//! - D7 not ported: `NeedsHeadTime` (no head time, D4), `Hints` (queue and status output, M9), the Go work counter
//!   `ch.calls` (internal; the time bound is tested instead), evidence URL (Rust keeps the comment id).
use std::sync::mpsc;
use std::time::Duration;

use stw::review::{
    evaluate, parse_body, FindingKind, Line, Parsed, PrRef, ReviewComment, ReviewState, Scope,
    Status, Verdict, MAX_LINKS,
};

const H: &str = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
const O: &str = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";
const P: &str = "1111111111111111111111111111111111111111";

fn hex(c: char) -> String {
    c.to_string().repeat(40)
}

/// Stamp `min` minutes from 2026-11-10 09:00 (as `at(min)` in state_test.go, shifted by ten days).
fn at(min: i64) -> String {
    let total = 10 * 1440 + 540 + min;
    format!(
        "2026-11-{:02}T{:02}:{:02}:00Z",
        1 + total / 1440,
        (total % 1440) / 60,
        total % 60
    )
}

fn comment(id: u64, min: i64, author: &str, body: String) -> ReviewComment {
    ReviewComment {
        id,
        author: author.into(),
        created_at: at(min),
        updated_at: at(min),
        body,
        body_truncated: false,
    }
}

fn rev(id: u64, min: i64, verdict: &str, head: &str, patch: &str) -> ReviewComment {
    comment(
        id,
        min,
        "owner",
        format!("Review: {verdict} head={head} patch-id={patch} pr=7\nbody"),
    )
}

fn carry(id: u64, min: i64, from: &str, to: &str, patch: &str) -> ReviewComment {
    comment(
        id,
        min,
        "owner",
        format!("Carried-over: from={from} to={to} patch-id={patch} pr=7\n"),
    )
}

fn free(id: u64, min: i64, body: &str) -> ReviewComment {
    comment(id, min, "owner", body.into())
}

/// The comment as posted by another author (Go: another `author_association`).
fn by(mut c: ReviewComment, author: &str) -> ReviewComment {
    c.author = author.into();
    c
}

fn state(cs: &[ReviewComment]) -> ReviewState {
    evaluate(Scope::Pr(7), H, cs, &["owner"])
}

fn status(cs: &[ReviewComment]) -> Status {
    state(cs).status
}

fn check(name: &str, cs: &[ReviewComment], want: Status) {
    let st = state(cs);
    assert_eq!(
        st.status, want,
        "{name}: {:?} {:?}",
        st.findings, st.reasons
    );
}

fn kinds(st: &ReviewState, kind: FindingKind) -> usize {
    st.findings.iter().filter(|f| f.kind == kind).count()
}

/// Runs `f` on a thread and fails when it does not return in time (Go `evaluateWithin`).
fn within<T: Send + 'static>(f: impl FnOnce() -> T + Send + 'static) -> T {
    let (tx, rx) = mpsc::channel();
    std::thread::spawn(move || {
        let _ = tx.send(f());
    });
    rx.recv_timeout(Duration::from_secs(5))
        .expect("evaluate did not finish within 5s: the chain check is not polynomial")
}

// ---- parse_test.go ----

fn line_of(body: &str) -> Parsed {
    parse_body(body, false)
}

#[test]
fn parse_line_strict_review() {
    let l = format!("Review: CLEAR head={H} patch-id={P} pr=7\nfurther text");
    assert_eq!(
        line_of(&l),
        Parsed::Line(Line::Review {
            verdict: Verdict::Clear,
            head: H.into(),
            patch_id: P.into(),
            pr: PrRef::Number(7)
        })
    );
    // single line, no break
    let l = format!("Review: CLEAR head={H} patch-id={P} pr=7");
    assert!(matches!(line_of(&l), Parsed::Line(_)), "single line");
}

#[test]
fn parse_line_strict_review_crlf_and_64_hex_deviate() {
    // DEVIATION D1: Go accepts CRLF and a 64-hex patch-id (state_test: "CRLF or 64-hex patch-id refused").
    let p64 = "f".repeat(64);
    let l = format!("Review: FINDINGS head={H} patch-id={p64} pr=7\r\nx");
    assert!(matches!(line_of(&l), Parsed::Variant(_)), "CRLF and 64 hex");
    let l = format!("Review: FINDINGS head={H} patch-id={p64} pr=7\nx");
    assert!(matches!(line_of(&l), Parsed::Variant(_)), "64 hex alone");
    let l = format!("Review: FINDINGS head={H} patch-id={P} pr=7\r\nx");
    assert!(matches!(line_of(&l), Parsed::Variant(_)), "CRLF alone");
}

#[test]
fn parse_line_strict_carried() {
    let l = format!("Carried-over: from={H} to={O} patch-id={P} pr=12\n");
    assert_eq!(
        line_of(&l),
        Parsed::Line(Line::Carried {
            from: H.into(),
            to: O.into(),
            patch_id: P.into(),
            pr: PrRef::Number(12)
        })
    );
}

#[test]
fn parse_line_pr_number_width_and_none_deviate() {
    // DEVIATION D1: Go accepts 1..999999999 and no `none`; Rust any canonical u64 and `pr=none`.
    for pr in ["1", "999999999", "1000000000"] {
        let l = format!("Review: CLEAR head={H} patch-id={P} pr={pr}");
        let n: u64 = pr.parse().unwrap();
        assert!(
            matches!(line_of(&l), Parsed::Line(Line::Review { pr: PrRef::Number(m), .. }) if m == n),
            "pr={pr}"
        );
    }
    let l = format!("Review: CLEAR head={H} patch-id={P} pr=18446744073709551616");
    assert!(matches!(line_of(&l), Parsed::Variant(_)), "beyond u64");
    let l = format!("Review: CLEAR head={H} patch-id={P} pr=none");
    assert!(matches!(
        line_of(&l),
        Parsed::Line(Line::Review {
            pr: PrRef::None,
            ..
        })
    ));
}

/// Rows of `TestParseLineNotStrictIsUnparsedOrIgnored`. `Variant` is Go `unparsed`, `NotReview` is Go `ignored`.
#[test]
fn parse_line_not_strict_is_variant_or_not_review() {
    let up = H.to_uppercase();
    let rows: Vec<(&str, String, bool)> = vec![
        (
            "upper hex",
            format!("Review: CLEAR head={up} patch-id={P} pr=7"),
            true,
        ),
        (
            "short patch id",
            format!("Review: CLEAR head={H} patch-id={} pr=7", "1".repeat(20)),
            true,
        ),
        (
            "missing pr",
            format!("Review: CLEAR head={H} patch-id={P}"),
            true,
        ),
        (
            "leading space",
            format!(" Review: CLEAR head={H} patch-id={P} pr=7"),
            true,
        ),
        (
            "double space",
            format!("Review:  CLEAR head={H} patch-id={P} pr=7"),
            true,
        ),
        (
            "trailing text",
            format!("Review: CLEAR head={H} patch-id={P} pr=7 ok"),
            true,
        ),
        (
            "leading zero pr",
            format!("Review: CLEAR head={H} patch-id={P} pr=07"),
            true,
        ),
        (
            "verdict on second line only",
            "thanks\nVerdict: CLEAR".into(),
            false,
        ),
        ("word inside a word", "This is clearly fine".into(), false),
        ("plain comment", "Please rebase.".into(), false),
        ("empty", String::new(), false),
        (
            "only review prose",
            "I will review this tomorrow".into(),
            false,
        ),
    ];
    for (name, body, variant) in rows {
        let got = line_of(&body);
        assert!(
            !matches!(got, Parsed::Line(_)),
            "{name}: must not be a line"
        );
        assert_eq!(
            matches!(got, Parsed::Variant(_)),
            variant,
            "{name}: {got:?}"
        );
    }
}

#[test]
fn parse_line_legacy_words_deviate() {
    // DEVIATION D2: Go classifies these as unparsed; Rust: not a review line (never a verdict, no finding).
    for body in [
        "Verdict: CLEAR".to_string(),
        format!("VERDICT: CLEAR for head {H}"),
        "Urteil: CLEAR".into(),
        "Urteil: in Ordnung".into(),
        "Findings: 3 open".into(),
        "Carried over from the old head".into(),
        "review: looks fine".into(),
    ] {
        assert_eq!(line_of(&body), Parsed::NotReview, "{body}");
    }
}

#[test]
fn parse_line_for_another_pr_is_a_line_with_that_pr() {
    // Go ParseLine(body, 7) answers `unparsed` for pr=8. Rust parses the line and the state evaluation rejects it
    // (`wrong-pr`, see `strict_line_for_another_pr_is_unknown`). DEVIATION in the API shape only.
    let r = format!("Review: CLEAR head={H} patch-id={P} pr=8");
    assert!(matches!(
        line_of(&r),
        Parsed::Line(Line::Review {
            pr: PrRef::Number(8),
            ..
        })
    ));
    let c = format!("Carried-over: from={H} to={O} patch-id={P} pr=8");
    assert!(matches!(
        line_of(&c),
        Parsed::Line(Line::Carried {
            pr: PrRef::Number(8),
            ..
        })
    ));
}

// ---- state_test.go: TestEvaluateStates ----

#[test]
fn evaluate_states_same_scenario() {
    use Status::*;
    let cc = hex('c');
    check("no comments", &[], NoReview);
    check(
        "plain comments only",
        &[free(1, 5, "please rebase")],
        NoReview,
    );
    check("clear current head", &[rev(1, 5, "CLEAR", H, P)], Clear);
    check(
        "findings current head",
        &[rev(1, 5, "FINDINGS", H, P)],
        Findings,
    );
    check(
        "latest wins: findings after clear",
        &[rev(1, 5, "CLEAR", H, P), rev(2, 6, "FINDINGS", H, P)],
        Findings,
    );
    check(
        "latest wins: clear after findings",
        &[rev(1, 5, "FINDINGS", H, P), rev(2, 6, "CLEAR", H, P)],
        Clear,
    );
    check(
        "same time: higher id wins",
        &[rev(2, 5, "FINDINGS", H, P), rev(1, 5, "CLEAR", H, P)],
        Findings,
    );
    check(
        "comment order in input does not matter",
        &[rev(2, 6, "CLEAR", H, P), rev(1, 5, "FINDINGS", H, P)],
        Clear,
    );
    check(
        "comment exactly at head time counts",
        &[rev(1, 0, "CLEAR", H, P)],
        Clear,
    );
    // D3: Go `clear-stale` is `no-review` with the finding stale-head.
    let st = state(&[rev(1, 5, "CLEAR", O, P)]);
    assert_eq!(st.status, NoReview, "clear for older head only");
    assert_eq!(kinds(&st, FindingKind::StaleHead), 1);
    check(
        "findings for older head only",
        &[rev(1, 5, "FINDINGS", O, P)],
        NoReview,
    );
    let mut prefix = H[..39].to_string();
    prefix.push('b');
    check(
        "head prefix is not the head",
        &[rev(1, 5, "CLEAR", &prefix, P)],
        NoReview,
    );
    check(
        "carry-over",
        &[rev(1, -10, "CLEAR", O, P), carry(2, 5, O, H, P)],
        ClearCarried,
    );
    check(
        "carry-over without a CLEAR",
        &[carry(2, 5, O, H, P)],
        NoReview,
    );
    check(
        "carry-over of findings",
        &[rev(1, -10, "FINDINGS", O, P), carry(2, 5, O, H, P)],
        NoReview,
    );
    let st = state(&[rev(1, -10, "CLEAR", O, P), carry(2, 5, O, H, &hex('2'))]);
    assert_eq!(st.status, NoReview, "carry-over with other patch-id");
    assert!(st.findings.iter().any(
        |f| f.kind == FindingKind::InvalidCarryChain && f.detail.contains("patch-id-mismatch")
    ));
    check(
        "carry-over created before its CLEAR",
        &[carry(1, 1, O, H, P), rev(2, 5, "CLEAR", O, P)],
        NoReview,
    );
    check(
        "carry-over from the wrong head",
        &[rev(1, -10, "CLEAR", O, P), carry(2, 5, &cc, H, P)],
        NoReview,
    );
    check(
        "carry-over to another head",
        &[rev(1, -10, "CLEAR", O, P), carry(2, 5, O, &cc, P)],
        NoReview,
    );
    check(
        "origin superseded by findings before the carry",
        &[
            rev(1, -10, "CLEAR", O, P),
            rev(2, -5, "FINDINGS", O, P),
            carry(3, 5, O, H, P),
        ],
        NoReview,
    );
    check(
        "findings after the carry-over wins",
        &[
            rev(1, -10, "CLEAR", O, P),
            carry(2, 5, O, H, P),
            rev(3, 6, "FINDINGS", H, P),
        ],
        Findings,
    );
    check(
        "clear after carry-over is direct",
        &[
            rev(1, -10, "CLEAR", O, P),
            carry(2, 5, O, H, P),
            rev(3, 6, "CLEAR", H, P),
        ],
        Clear,
    );
    check(
        "carry-over after findings wins",
        &[
            rev(1, -10, "CLEAR", O, P),
            rev(2, 2, "FINDINGS", H, P),
            carry(3, 5, O, H, P),
        ],
        ClearCarried,
    );
    check(
        "two links",
        &[
            rev(1, -30, "CLEAR", &cc, P),
            carry(2, -20, &cc, O, P),
            carry(3, 5, O, H, P),
        ],
        ClearCarried,
    );
    check(
        "chain order broken",
        &[
            rev(1, -30, "CLEAR", &cc, P),
            carry(2, 6, &cc, O, P),
            carry(3, 5, O, H, P),
        ],
        NoReview,
    );
}

#[test]
fn strict_line_for_another_pr_is_unknown() {
    // Go: state `unparsed`. Rust: a trusted line of another pr that names the head may be meant for this one: unknown.
    let c = comment(
        1,
        5,
        "owner",
        format!("Review: CLEAR head={H} patch-id={P} pr=8"),
    );
    let st = state(&[c]);
    assert_eq!(st.status, Status::Unknown);
    assert_eq!(kinds(&st, FindingKind::WrongPr), 1);
}

#[test]
fn comments_older_than_the_head_commit_are_not_filtered() {
    // DEVIATION D4: Go: StateNone / StateClearStale (comment before the head commit; head time unknown).
    // Rust has no head time: the comment counts.
    check(
        "comment before head commit",
        &[rev(1, -1, "CLEAR", H, P)],
        Status::Clear,
    );
    check(
        "carry-over before head commit",
        &[rev(1, -20, "CLEAR", O, P), carry(2, -5, O, H, P)],
        Status::ClearCarried,
    );
}

#[test]
fn legacy_verdict_words_have_no_effect() {
    // DEVIATION D2: Go: `unparsed` for the five cases below. Rust: the legacy line is not a review line.
    let v = "Verdict: CLEAR";
    check("unparsed only", &[free(1, 5, v)], Status::NoReview);
    check(
        "unparsed older than decisive is irrelevant",
        &[free(1, 3, v), rev(2, 5, "CLEAR", H, P)],
        Status::Clear,
    );
    check(
        "unparsed newer than decisive",
        &[rev(1, 5, "CLEAR", H, P), free(2, 6, "Urteil: FINDINGS")],
        Status::Clear,
    );
    check(
        "unparsed newer than stale clear",
        &[rev(1, 5, "CLEAR", O, P), free(2, 6, v)],
        Status::NoReview,
    );
    check(
        "unparsed older than stale clear",
        &[free(1, 3, v), rev(2, 5, "CLEAR", O, P)],
        Status::NoReview,
    );
}

#[test]
fn broken_review_line_newer_than_decision_is_unknown() {
    // The Rust counterpart of Go `unparsed` for a line with review keys (the rows "unparsed newer than decisive"
    // and "unparsed newer than stale clear" with a variant instead of a legacy word).
    let broken = |id, min| free(id, min, &format!("Review: FINDINGS head={H} patch-id={P}"));
    check(
        "variant newer than decisive",
        &[rev(1, 5, "CLEAR", H, P), broken(2, 6)],
        Status::Unknown,
    );
    check(
        "variant newer than stale clear",
        &[rev(1, 5, "CLEAR", O, P), broken(2, 6)],
        Status::Unknown,
    );
    check(
        "variant older than decisive",
        &[broken(1, 3), rev(2, 5, "CLEAR", H, P)],
        Status::Clear,
    );
    check("variant only", &[broken(1, 5)], Status::Unknown);
}

// ---- state_test.go: chains ----

fn head_n(i: usize) -> String {
    format!("{:040x}", 0x1000 + i)
}

/// CLEAR of head 0, `links` carry-overs head i -> head i+1, the last one to H.
fn chain(links: usize) -> Vec<ReviewComment> {
    let mut cs = vec![rev(1, -1000, "CLEAR", &head_n(0), P)];
    for i in 0..links {
        let to = if i + 1 == links {
            H.to_string()
        } else {
            head_n(i + 1)
        };
        cs.push(carry(10 + i as u64, -900 + i as i64, &head_n(i), &to, P));
    }
    cs
}

#[test]
fn evaluate_chain_of_nine_and_nine_links_limit_deviates() {
    // DEVIATION D6: Go resolves 8 links and refuses 9 ("chain of ten", "nine links"). Rust: MAX_LINKS.
    assert_eq!(MAX_LINKS, 16);
    check("8 links", &chain(8), Status::ClearCarried);
    check("9 links (Go: refused)", &chain(9), Status::ClearCarried);
    check("10 links (Go: refused)", &chain(10), Status::ClearCarried);
    check("16 links", &chain(MAX_LINKS), Status::ClearCarried);
    let st = state(&chain(MAX_LINKS + 1));
    assert_eq!(st.status, Status::NoReview, "17 links: {:?}", st.findings);
    assert!(st
        .findings
        .iter()
        .any(|f| f.kind == FindingKind::InvalidCarryChain));
}

#[test]
fn evaluate_cycle_terminates() {
    let c = hex('c');
    check(
        "cycle without a CLEAR",
        &[
            carry(1, 1, O, &c, P),
            carry(2, 2, &c, O, P),
            carry(3, 5, O, H, P),
        ],
        Status::NoReview,
    );
    check(
        "equal-time cycle",
        &[
            carry(1, 1, O, &c, P),
            carry(2, 1, &c, O, P),
            carry(3, 5, O, H, P),
        ],
        Status::NoReview,
    );
}

#[test]
fn evaluate_many_self_carry_overs() {
    let mut cs: Vec<ReviewComment> = (0..40)
        .map(|i| carry(10 + i, -50 + i as i64, O, O, P))
        .collect();
    cs.push(carry(99, 5, O, H, P));
    let none = cs.clone();
    assert_eq!(
        within(move || status(&none)),
        Status::NoReview,
        "without a CLEAR"
    );
    cs.insert(0, rev(1, -100, "CLEAR", O, P));
    let st = within(move || state(&cs));
    assert_eq!(
        st.status,
        Status::ClearCarried,
        "self carry-overs are skipped"
    );
    assert_eq!(st.chain.last().unwrap().comment_id, 99);
    assert_eq!(st.chain.first().unwrap().comment_id, 1);
}

fn dense(rounds: usize, root_verdict: &str) -> Vec<ReviewComment> {
    let hs: Vec<String> = "cdef0123".chars().map(hex).collect();
    let mut cs = vec![rev(1, -1000, root_verdict, &hs[0], P)];
    let (mut id, mut min) = (10u64, -900i64);
    for _ in 0..rounds {
        for i in 0..hs.len() {
            for j in 0..hs.len() {
                if i != j {
                    cs.push(carry(id, min, &hs[i], &hs[j], P));
                    id += 1;
                    min += 1;
                }
            }
        }
    }
    cs.push(carry(9999, 5, &hs[7], H, P));
    cs
}

#[test]
fn evaluate_many_heads_many_links() {
    let none = dense(6, "FINDINGS");
    assert_eq!(
        within(move || status(&none)),
        Status::NoReview,
        "no CLEAR anywhere"
    );
    let clear = dense(6, "CLEAR");
    assert_eq!(
        within(move || status(&clear)),
        Status::ClearCarried,
        "a CLEAR at the start of a dense graph"
    );
}

#[test]
fn chain_work_is_bounded() {
    // Go also counts internal checks (`ch.calls <= comments`); Rust memoizes per comment, the count is not
    // observable (D7). The time bound holds for the 4-round graph of the Go test.
    let cs = dense(4, "CLEAR");
    assert_eq!(within(move || status(&cs)), Status::ClearCarried);
}

#[test]
fn repeated_head_does_not_revive_a_superseded_clear() {
    let (a, b) = (hex('b'), hex('c'));
    check(
        "repeated head, superseded",
        &[
            rev(1, -30, "CLEAR", &a, P),
            carry(2, -20, &a, &b, P),
            carry(3, -10, &b, &a, P),
            rev(4, -5, "FINDINGS", &a, P),
            carry(5, 5, &a, H, P),
        ],
        Status::NoReview,
    );
    check(
        "a loop is no origin",
        &[
            carry(2, -20, &a, &b, P),
            carry(3, -10, &b, &a, P),
            carry(5, 5, &a, H, P),
        ],
        Status::NoReview,
    );
}

#[test]
fn loop_back_to_a_repeated_head() {
    // Go: clear-stale with the evidence of the newer direct CLEAR (patch-id P2). Rust: no-review (D3); the
    // carry-over of P is refused.
    let (a, b, p2) = (hex('b'), hex('c'), hex('2'));
    let st = state(&[
        rev(1, -30, "CLEAR", &a, P),
        carry(2, -20, &a, &b, P),
        rev(3, -15, "CLEAR", &a, &p2),
        carry(4, -10, &b, &a, P),
        carry(5, 5, &a, H, P),
    ]);
    assert_eq!(
        st.status,
        Status::NoReview,
        "loop back to A: {:?}",
        st.findings
    );
    let c = hex('d');
    let st = state(&[
        rev(1, -30, "CLEAR", &a, P),
        carry(2, -25, &a, &b, P),
        carry(3, -20, &b, &c, P),
        rev(4, -15, "CLEAR", &b, &p2),
        carry(5, -10, &c, &b, P),
        carry(6, 5, &b, H, P),
    ]);
    assert_eq!(
        st.status,
        Status::NoReview,
        "loop back to B: {:?}",
        st.findings
    );
}

#[test]
fn carry_over_after_findings_does_not_reuse_an_earlier_check() {
    // X is carried to H twice, FINDINGS for X lie between. Go: the first carry-over is valid (state clear-carried,
    // evidence of comment 2), the second is not. DEVIATION D8: Rust judges the whole thread; FINDINGS of a chain
    // member after the line that cleared it invalidate every chain through it (`member-findings`), also a carry-over
    // posted before the FINDINGS. Stricter: no review instead of a carried CLEAR (README M3; case
    // `tests/review_states/findings_on_root_member_after_chain_invalidates.json`).
    let x = hex('b');
    let st = state(&[
        rev(1, -30, "CLEAR", &x, P),
        carry(2, 1, &x, H, P),
        rev(3, 3, "FINDINGS", &x, P),
        carry(4, 5, &x, H, P),
    ]);
    assert_eq!(st.status, Status::NoReview);
    assert_eq!(
        kinds(&st, FindingKind::InvalidCarryChain),
        2,
        "{:?}",
        st.findings
    );
    let why = |id: u64| {
        st.findings
            .iter()
            .find(|f| f.kind == FindingKind::InvalidCarryChain && f.comment_id == id)
            .map(|f| f.detail.clone())
            .unwrap_or_default()
    };
    assert!(why(2).starts_with("member-findings"), "{}", why(2));
    assert!(why(4).starts_with("from is not a CLEAR"), "{}", why(4));
}

// ---- state_test.go: authors ----

#[test]
fn only_trusted_logins_count() {
    // DEVIATION D5: Go: OWNER, MEMBER, COLLABORATOR count; Rust: a login of the trust list, case-insensitive,
    // whole login. The rows of the Go table become logins.
    let c = rev(1, 5, "CLEAR", H, P);
    for (author, want) in [
        ("owner", Status::Clear),
        ("OWNER", Status::Clear),
        ("Owner", Status::Clear),
        ("contributor", Status::UnverifiedAuthor),
        ("", Status::UnverifiedAuthor),
        ("owner ", Status::UnverifiedAuthor),
        ("owners", Status::UnverifiedAuthor),
        ("wner", Status::UnverifiedAuthor),
    ] {
        let st = state(&[by(c.clone(), author)]);
        assert_eq!(st.status, want, "CLEAR by {author:?}");
        if want == Status::Clear {
            assert!(st.decisive.is_some());
        } else {
            assert!(st.decisive.is_none(), "no evidence from {author:?}");
            assert_eq!(kinds(&st, FindingKind::UntrustedAuthor), 1, "counted");
        }
    }
    // An empty trust list trusts nobody.
    assert_eq!(
        evaluate(Scope::Pr(7), H, &[c], &[]).status,
        Status::UnverifiedAuthor
    );
}

#[test]
fn foreign_comments_have_no_effect() {
    use Status::*;
    let n = "stranger";
    let variant = |id, min| free(id, min, &format!("Review: FINDINGS head={H} patch-id={P}"));
    let rows: Vec<(&str, Vec<ReviewComment>, Status, usize)> = vec![
        (
            "foreign FINDINGS after an owner CLEAR",
            vec![rev(1, 5, "CLEAR", H, P), by(rev(2, 6, "FINDINGS", H, P), n)],
            Clear,
            1,
        ),
        (
            "foreign CLEAR after owner FINDINGS",
            vec![rev(1, 5, "FINDINGS", H, P), by(rev(2, 6, "CLEAR", H, P), n)],
            Findings,
            1,
        ),
        (
            "foreign unparsed comment after an owner CLEAR",
            vec![rev(1, 5, "CLEAR", H, P), by(variant(2, 6), n)],
            Clear,
            1,
        ),
        (
            "foreign unparsed comment alone",
            vec![by(variant(1, 6), n)],
            NoReview,
            1,
        ),
        (
            "foreign chat is not counted",
            vec![by(free(1, 6, "looks good to me"), n)],
            NoReview,
            0,
        ),
        (
            "foreign carry-over",
            vec![rev(1, -10, "CLEAR", O, P), by(carry(2, 5, O, H, P), n)],
            UnverifiedAuthor,
            1,
        ),
        (
            "foreign CLEAR as origin of an owner carry-over",
            vec![by(rev(1, -10, "CLEAR", O, P), n), carry(2, 5, O, H, P)],
            NoReview,
            1,
        ),
        (
            "foreign link in the middle of a chain",
            vec![
                rev(1, -30, "CLEAR", &hex('c'), P),
                by(carry(2, -20, &hex('c'), O, P), n),
                carry(3, 5, O, H, P),
            ],
            NoReview,
            1,
        ),
        (
            "foreign CLEAR for an older head is no stale CLEAR",
            vec![by(rev(1, 5, "CLEAR", O, P), n)],
            NoReview,
            1,
        ),
        // Rust shapes of the last two Go rows (D3, D5): an untrusted line that names the head is
        // `unverified-author`; a broken owner line newer than the decision is `unknown`.
        (
            "foreign line and an owner unparsed comment",
            vec![by(rev(1, 5, "CLEAR", H, P), n), variant(2, 6)],
            Unknown,
            1,
        ),
        (
            "many foreign lines are all counted",
            vec![
                by(rev(1, 5, "CLEAR", H, P), n),
                by(rev(2, 6, "CLEAR", H, P), n),
                by(variant(3, 7), n),
            ],
            UnverifiedAuthor,
            3,
        ),
    ];
    for (name, cs, want, count) in rows {
        let st = state(&cs);
        assert_eq!(
            st.status, want,
            "{name}: {:?} {:?}",
            st.findings, st.reasons
        );
        // Go `Ignored` counts every foreign review-like comment (strict or not); Rust has the findings
        // `untrusted-author` (strict lines) and `unparsed-review-variant` (broken lines of anyone).
        let counted = kinds(&st, FindingKind::UntrustedAuthor)
            + st.findings
                .iter()
                .filter(|f| f.kind == FindingKind::UnparsedReviewVariant && f.author == n)
                .count();
        assert_eq!(counted, count, "{name}: foreign lines counted");
    }
}

#[test]
fn foreign_strict_line_for_another_pr() {
    let c = by(
        comment(
            1,
            5,
            "x",
            format!("Review: CLEAR head={H} patch-id={P} pr=8"),
        ),
        "stranger",
    );
    let st = state(&[c]);
    assert_eq!(st.status, Status::NoReview, "{:?}", st.findings);
    assert_eq!(kinds(&st, FindingKind::WrongPr), 1);
}

#[test]
fn association_of_the_decisive_comment_is_its_author() {
    // Go reads Association of the decisive comment; Rust keeps the author (D5).
    let st = evaluate(
        Scope::Pr(7),
        H,
        &[
            by(rev(1, -10, "CLEAR", O, P), "member"),
            by(carry(2, 5, O, H, P), "collab"),
        ],
        &["member", "collab"],
    );
    assert_eq!(st.status, Status::ClearCarried);
    assert_eq!(
        st.decisive.unwrap().author,
        "collab",
        "the carry-over decides"
    );
    let st = evaluate(
        Scope::Pr(7),
        H,
        &[by(rev(1, 5, "CLEAR", O, P), "member")],
        &["member"],
    );
    assert_eq!(st.status, Status::NoReview, "stale (D3)");
    let st = evaluate(
        Scope::Pr(7),
        H,
        &[by(rev(1, 5, "FINDINGS", H, P), "collab")],
        &["collab"],
    );
    assert_eq!(st.status, Status::Findings);
    assert_eq!(st.decisive.unwrap().author, "collab");
}

// ---- state_test.go: evidence ----

#[test]
fn evaluate_evidence() {
    let st = state(&[rev(1, 5, "CLEAR", H, P)]);
    let d = st.decisive.as_ref().unwrap();
    assert_eq!(
        d.line,
        Line::Review {
            verdict: Verdict::Clear,
            head: H.into(),
            patch_id: P.into(),
            pr: PrRef::Number(7)
        }
    );
    assert_eq!(d.created_at, at(5));
    assert!(!d.edited && st.chain.is_empty(), "direct evidence");

    let st = state(&[rev(1, -10, "CLEAR", O, P), carry(2, 5, O, H, P)]);
    assert_eq!(st.status, Status::ClearCarried);
    let d = st.decisive.as_ref().unwrap();
    assert_eq!(d.comment_id, 2);
    assert_eq!(d.created_at, at(5));
    assert_eq!(
        st.chain.iter().map(|e| e.comment_id).collect::<Vec<_>>(),
        [1, 2],
        "carried evidence: root CLEAR of the older head, then the carry-over"
    );
    assert!(matches!(
        &st.chain[0].line,
        Line::Review { head, verdict: Verdict::Clear, .. } if head == O
    ));

    let mut e = rev(1, 5, "CLEAR", H, P);
    e.updated_at = at(9);
    let st = state(&[e]);
    assert_eq!(st.status, Status::Clear);
    assert!(st.decisive.unwrap().edited, "edited");

    // "stale evidence" with Truncated: a CLEAR of an older head gives no evidence for the head in Rust (D3);
    // a truncated read is unknown, see tests/review_read.rs `incomplete_read_is_unknown_even_with_a_clear`.
    let st = state(&[rev(1, 5, "CLEAR", O, P)]);
    assert!(st.decisive.is_none());

    let st = state(&[free(1, 5, "Verdict: CLEAR")]);
    assert!(st.decisive.is_none() && st.chain.is_empty(), "no evidence");
}
