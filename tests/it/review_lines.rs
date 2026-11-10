//! Strict parser of the first line of a review comment: the shared dotagents corpus plus per-rule cases.
use stw::review::{parse_body, parse_strict, unescape, Line, Parsed, PrRef, Verdict};

const A: &str = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
const B: &str = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";
const C: &str = "cccccccccccccccccccccccccccccccccccccccc";

fn corpus(prefix: &str) -> Vec<String> {
    let p = format!(
        "{}/tests/review_corpus/review-line-examples.md",
        env!("CARGO_MANIFEST_DIR")
    );
    std::fs::read_to_string(p)
        .unwrap()
        .lines()
        .filter_map(|l| l.strip_prefix(prefix).map(str::to_string))
        .collect()
}

#[test]
fn corpus_ok_lines_parse() {
    let ok = corpus("ok: ");
    assert_eq!(ok.len(), 3);
    for l in ok {
        assert!(parse_strict(&l).is_some(), "{l}");
        assert!(matches!(parse_body(&l, false), Parsed::Line(_)), "{l}");
    }
}

#[test]
fn corpus_bad_lines_are_never_lines() {
    let bad = corpus("bad: ");
    assert!(bad.len() >= 20, "{}", bad.len());
    for l in bad {
        assert!(parse_strict(&l).is_none(), "{l}");
        assert!(!matches!(parse_body(&l, false), Parsed::Line(_)), "{l}");
    }
}

#[test]
fn corpus_variants_are_reported_not_ignored() {
    // Every bad line that starts like a review line (or has the key fields) is a typed variant.
    for l in corpus("bad: ") {
        if l.starts_with("Verdict:") {
            continue;
        }
        assert!(matches!(parse_body(&l, false), Parsed::Variant(_)), "{l}");
    }
}

#[test]
fn review_fields_are_read() {
    let l = format!("Review: FINDINGS head={A} patch-id={B} pr=7");
    assert_eq!(
        parse_strict(&l),
        Some(Line::Review {
            verdict: Verdict::Findings,
            head: A.into(),
            patch_id: B.into(),
            pr: PrRef::Number(7)
        })
    );
    let l = format!("Carried-over: from={A} to={B} patch-id={C} pr=none");
    assert_eq!(
        parse_strict(&l),
        Some(Line::Carried {
            from: A.into(),
            to: B.into(),
            patch_id: C.into(),
            pr: PrRef::None
        })
    );
}

#[test]
fn only_the_first_line_counts_and_a_trailing_line_break_is_fine() {
    let l = format!("Review: CLEAR head={A} patch-id={B} pr=1");
    assert!(matches!(
        parse_body(&format!("{l}\nbody"), false),
        Parsed::Line(_)
    ));
    assert!(matches!(
        parse_body(&format!("{l}\n"), false),
        Parsed::Line(_)
    ));
    assert_eq!(parse_body(&format!("text\n{l}"), false), Parsed::NotReview);
}

#[test]
fn cr_tab_and_cut_lines_are_variants() {
    let l = format!("Review: CLEAR head={A} patch-id={B} pr=1");
    assert!(matches!(
        parse_body(&format!("{l}\r\nbody"), false),
        Parsed::Variant(_)
    ));
    assert!(matches!(
        parse_body(&format!("{l}\r"), false),
        Parsed::Variant(_)
    ));
    assert!(matches!(
        parse_body(&l.replace("CLEAR head", "CLEAR\thead"), false),
        Parsed::Variant(_)
    ));
    // A body cut inside its first line must not pass although the cut happens to leave a valid prefix.
    assert!(matches!(parse_body(&l, true), Parsed::Variant(_)));
    assert!(matches!(
        parse_body(&format!("{l}\nx"), true),
        Parsed::Line(_)
    ));
}

#[test]
fn numbers_are_canonical_decimals() {
    for pr in ["1", "15", "123456789012"] {
        assert!(
            parse_strict(&format!("Review: CLEAR head={A} patch-id={B} pr={pr}")).is_some(),
            "{pr}"
        );
    }
    for pr in [
        "0",
        "01",
        "+1",
        "-1",
        "1a",
        "",
        "None",
        "99999999999999999999999",
    ] {
        assert!(
            parse_strict(&format!("Review: CLEAR head={A} patch-id={B} pr={pr}")).is_none(),
            "{pr}"
        );
    }
}

#[test]
fn ordinary_text_is_not_a_review() {
    for t in [
        "",
        "LGTM",
        "Review: please look at the tests",
        "Reviewed the diff",
        "Carry on",
        "head=abc",
    ] {
        assert_eq!(parse_body(t, false), Parsed::NotReview, "{t:?}");
    }
}

#[test]
fn unescape_reverses_the_reads_escaping() {
    use stw::github::reads::escape_untrusted;
    for s in [
        "plain",
        "a\nb\r\nc\td",
        "back\\slash",
        "zero\u{200b}width",
        "bell\u{7}",
        "é日本",
    ] {
        let (e, _) = escape_untrusted(s, 1000);
        assert_eq!(unescape(&e).as_deref(), Some(s));
    }
    for bad in ["\\", "\\x", "\\u", "\\u{zz}", "\\u{110000}", "\\u{12"] {
        assert_eq!(unescape(bad), None, "{bad}");
    }
}
