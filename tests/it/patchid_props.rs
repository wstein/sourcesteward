//! Property tests for the in-process patch ID: invariance under whitespace in the changed line and under
//! shifting the change by extra lines above it; sensitivity to the changed content.
use crate::common::*;
use proptest::prelude::*;
use stw::patch_id::commit_patch_id;

fn pid(fx: &Fx, c: gix::ObjectId) -> gix::ObjectId {
    commit_patch_id(&fx.repo(), c)
        .unwrap()
        .expect("has a patch id")
}

/// A file of `n` distinct lines.
fn file(n: usize) -> String {
    (0..n).map(|i| format!("line{i}\n")).collect()
}

/// `base` with line `at` replaced by `new`, and `above` extra lines on top (shifts the line numbers).
fn edited(n: usize, at: usize, new: &str, above: usize) -> String {
    let mut out: String = (0..above).map(|i| format!("top{i}\n")).collect();
    for i in 0..n {
        if i == at {
            out.push_str(new);
            out.push('\n');
        } else {
            out.push_str(&format!("line{i}\n"));
        }
    }
    out
}

/// Words joined by non-empty runs of spaces and tabs, with optional leading and trailing whitespace.
fn spaced(words: &[String], seps: &[String], lead: &str, trail: &str) -> String {
    let mut s = lead.to_string();
    for (i, w) in words.iter().enumerate() {
        s.push_str(w);
        if i + 1 < words.len() {
            s.push_str(&seps[i % seps.len()]);
        }
    }
    s.push_str(trail);
    s
}

/// Fixed boundary case: the change is the first one with a full 3 lines of context above it. One line added on
/// top must not become context (a context of 4 would hash it).
#[test]
fn a_change_at_line_3_keeps_its_id_when_a_line_is_added_on_top() {
    let (n, at) = (10, 3);
    let fx = Fx::new();
    let base = fx.commit(None, &[("f.txt", Some(&file(n)))], "base");
    let a = fx.commit(
        Some(base),
        &[("f.txt", Some(&edited(n, at, "new", 0)))],
        "edit",
    );
    let shifted_base = fx.commit(
        None,
        &[("f.txt", Some(&edited(n, n, "", 1)))],
        "shifted base",
    );
    let c = fx.commit(
        Some(shifted_base),
        &[("f.txt", Some(&edited(n, at, "new", 1)))],
        "shifted edit",
    );
    assert_eq!(pid(&fx, a), pid(&fx, c));
}

proptest! {
    #![proptest_config(ProptestConfig { cases: 12, failure_persistence: None, ..ProptestConfig::default() })]

    #[test]
    fn whitespace_in_the_changed_line_and_line_shift_do_not_change_the_id(
        n in 8usize..30,
        at_seed in 0usize..1000,
        above in 0usize..6,
        words in proptest::collection::vec("[a-z0-9]{1,6}", 1..5),
        seps in proptest::collection::vec("[ \t]{1,3}", 1..4),
        lead in "[ \t]{0,3}",
        trail in "[ \t]{0,3}",
    ) {
        // at least 3 lines above the change: the lines added on top must not show up as its context
        let at = 3 + at_seed % (n - 3);
        let plain = words.join(" ");
        let fx = Fx::new();
        let base = fx.commit(None, &[("f.txt", Some(&file(n)))], "base");
        let a = fx.commit(Some(base), &[("f.txt", Some(&edited(n, at, &plain, 0)))], "plain");
        let spaced_line = spaced(&words, &seps, &lead, &trail);
        let b = fx.commit(Some(base), &[("f.txt", Some(&edited(n, at, &spaced_line, 0)))], "spaced");
        prop_assert_eq!(pid(&fx, a), pid(&fx, b), "whitespace {:?}", spaced_line);

        // the same change on a file with `above` more lines on top: other line numbers, same id
        let shifted_base = fx.commit(
            None,
            &[("f.txt", Some(&edited(n, n, "", above)))],
            "shifted base",
        );
        let c = fx.commit(
            Some(shifted_base),
            &[("f.txt", Some(&edited(n, at, &plain, above)))],
            "shifted edit",
        );
        prop_assert_eq!(pid(&fx, a), pid(&fx, c), "shift {}", above);
    }

    #[test]
    fn a_different_changed_line_gives_a_different_id(
        n in 8usize..30,
        at_seed in 0usize..1000,
        x in "[a-z]{1,8}",
        y in "[a-z]{1,8}",
    ) {
        prop_assume!(x != y);
        let at = at_seed % n;
        let fx = Fx::new();
        let base = fx.commit(None, &[("f.txt", Some(&file(n)))], "base");
        let a = fx.commit(Some(base), &[("f.txt", Some(&edited(n, at, &x, 0)))], "x");
        let b = fx.commit(Some(base), &[("f.txt", Some(&edited(n, at, &y, 0)))], "y");
        prop_assert_ne!(pid(&fx, a), pid(&fx, b));
    }
}
