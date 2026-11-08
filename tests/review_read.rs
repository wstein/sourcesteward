//! Review state from read results (`Read<Comment>`): escaped bodies, incomplete and failed reads.
use stw::github::reads::{escape_untrusted, Comment, Denial, Observed, Read};
use stw::review::{evaluate, evaluate_read, ReviewComment, Scope, Status, MAX_LINKS};

const A: &str = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
const P: &str = "ffffffffffffffffffffffffffffffffffffffff";

fn comment(id: u64, author: &str, raw: &str) -> Comment {
    let (body, body_truncated) = escape_untrusted(raw, 16 * 1024);
    Comment {
        id,
        author: author.into(),
        author_type: "User".into(),
        association: "OWNER".into(),
        created_at: format!("2026-01-01T00:00:{:02}Z", id.min(59)),
        updated_at: format!("2026-01-01T00:00:{:02}Z", id.min(59)),
        body,
        body_truncated,
    }
}

fn clear() -> String {
    format!("Review: CLEAR head={A} patch-id={P} pr=15\n\nfine")
}

fn observed(items: Vec<Comment>, incomplete: Option<&str>) -> Read<Comment> {
    Read::Observed(Observed {
        items,
        pages: 1,
        incomplete: incomplete.map(String::from),
    })
}

#[test]
fn escaped_body_of_a_read_is_parsed() {
    let r = observed(vec![comment(1, "owner", &clear())], None);
    let st = evaluate_read(Scope::Pr(15), A, &r, &["owner"]);
    assert_eq!(st.status, Status::Clear);
    let d = st.decisive.unwrap();
    assert_eq!((d.comment_id, d.author.as_str()), (1, "owner"));
    assert_eq!(d.created_at, "2026-01-01T00:00:01Z");
}

#[test]
fn crlf_in_a_read_body_is_a_variant() {
    let raw = clear().replacen('\n', "\r\n", 1);
    let r = observed(vec![comment(1, "owner", &raw)], None);
    let st = evaluate_read(Scope::Pr(15), A, &r, &["owner"]);
    assert_eq!(st.status, Status::Unknown);
    assert_eq!(st.findings[0].kind.label(), "unparsed-review-variant");
}

#[test]
fn incomplete_read_is_unknown_even_with_a_clear() {
    let r = observed(vec![comment(1, "owner", &clear())], Some("page limit"));
    let st = evaluate_read(Scope::Pr(15), A, &r, &["owner"]);
    assert_eq!(st.status, Status::Unknown);
    assert!(
        st.reasons.iter().any(|x| x.contains("page limit")),
        "{:?}",
        st.reasons
    );
}

#[test]
fn incomplete_read_without_any_review_is_unknown_not_no_review() {
    let r = observed(vec![], Some("time budget"));
    assert_eq!(
        evaluate_read(Scope::Pr(15), A, &r, &["owner"]).status,
        Status::Unknown
    );
}

#[test]
fn unreadable_reads_are_unknown() {
    let reads: Vec<(Read<Comment>, &str)> = vec![
        (
            Read::Denied {
                kind: Denial::Forbidden,
                status: 403,
                message: "x".into(),
            },
            "denied",
        ),
        (Read::NotFound, "not-found"),
        (Read::Failed("boom".into()), "failed"),
    ];
    for (r, label) in reads {
        let st = evaluate_read(Scope::Pr(15), A, &r, &["owner"]);
        assert_eq!(st.status, Status::Unknown, "{label}");
        assert!(
            st.reasons.iter().any(|x| x.contains(label)),
            "{label}: {:?}",
            st.reasons
        );
        assert!(st.decisive.is_none());
    }
}

#[test]
fn malformed_escape_makes_the_state_unknown() {
    let mut c = comment(1, "owner", &clear());
    c.body = "bad \\q escape".into();
    let st = evaluate_read(
        Scope::Pr(15),
        A,
        &observed(vec![c, comment(2, "owner", &clear())], None),
        &["owner"],
    );
    assert_eq!(st.status, Status::Unknown);
    assert_eq!(
        st.findings
            .iter()
            .filter(|f| f.kind.label() == "unreadable-body")
            .count(),
        1
    );
}

#[test]
fn json_carries_provenance_and_status() {
    let r = observed(vec![comment(7, "owner", &clear())], None);
    let j = evaluate_read(Scope::Pr(15), A, &r, &["owner"]).to_json();
    assert_eq!(j["status"], "clear");
    assert_eq!(j["decisive"]["comment_id"], 7);
    assert_eq!(j["decisive"]["author"], "owner");
    assert_eq!(j["decisive"]["created_at"], "2026-01-01T00:00:07Z");
    assert_eq!(j["decisive"]["line"]["pr"], 15);
    assert_eq!(j["pr"], "15");
}

fn stamp(id: u64) -> String {
    format!(
        "2026-01-01T{:02}:{:02}:{:02}Z",
        id / 3600,
        id / 60 % 60,
        id % 60
    )
}

fn rc(id: u64, body: String) -> ReviewComment {
    ReviewComment {
        id,
        author: "owner".into(),
        created_at: stamp(id),
        updated_at: stamp(id),
        body,
        body_truncated: false,
    }
}

fn head(n: usize) -> String {
    format!("{n:040x}")
}

fn chain_of(links: usize) -> Vec<ReviewComment> {
    let mut v = vec![rc(
        1,
        format!("Review: CLEAR head={} patch-id={P} pr=15", head(0)),
    )];
    for i in 0..links {
        v.push(rc(
            2 + i as u64,
            format!(
                "Carried-over: from={} to={} patch-id={P} pr=15",
                head(i),
                head(i + 1)
            ),
        ));
    }
    v
}

#[test]
fn chain_length_is_bounded() {
    let ok = chain_of(MAX_LINKS);
    assert_eq!(
        evaluate(Scope::Pr(15), &head(MAX_LINKS), &ok, &["owner"]).status,
        Status::ClearCarried
    );
    let long = chain_of(MAX_LINKS + 1);
    assert_eq!(
        evaluate(Scope::Pr(15), &head(MAX_LINKS + 1), &long, &["owner"]).status,
        Status::NoReview
    );
}

#[test]
fn many_carry_overs_stay_fast() {
    // Memoised: a long thread of valid links must not take exponential time.
    let mut v = chain_of(MAX_LINKS);
    for i in 0..300u64 {
        v.push(rc(
            100 + i,
            format!(
                "Carried-over: from={} to={} patch-id={P} pr=15",
                head(5),
                head(6)
            ),
        ));
    }
    let st = evaluate(Scope::Pr(15), &head(6), &v, &["owner"]);
    assert_eq!(st.status, Status::ClearCarried);
}

#[test]
fn diamond_of_invalid_carry_overs_is_not_exponential() {
    // Two invalid carry-overs per layer and no CLEAR at the bottom: unmemoised this is 2^layers.
    let layers = 26;
    let mut v = Vec::new();
    for i in 1..=layers {
        for k in 0..2u64 {
            v.push(rc(
                (i * 2) as u64 + k,
                format!(
                    "Carried-over: from={} to={} patch-id={P} pr=15",
                    head(i - 1),
                    head(i)
                ),
            ));
        }
    }
    let t = std::time::Instant::now();
    let st = evaluate(Scope::Pr(15), &head(layers), &v, &["owner"]);
    assert_eq!(st.status, Status::NoReview);
    assert!(
        t.elapsed() < std::time::Duration::from_secs(2),
        "{:?}",
        t.elapsed()
    );
}

#[test]
fn thousands_of_carry_overs_do_not_overflow_the_stack() {
    for n in [5000, 50000] {
        let v = chain_of(n);
        let st = evaluate(Scope::Pr(15), &head(n), &v, &["owner"]);
        assert_eq!(st.status, Status::NoReview, "{n}");
        assert!(st
            .findings
            .iter()
            .any(|f| f.kind.label() == "invalid-carry-chain"));
    }
}
