// SPDX-License-Identifier: EUPL-1.2 AND BSD-3-Clause
//! The pure functions of `stw absorb`: the commutation test (the unit tests of git-absorb's `commute.rs`, the same
//! numbers; BSD 3-Clause, see LICENSE-git-absorb-BSD-3), the consistency check, the masking and the verdict of one hunk.
use stw::absorb::{decide, sanitize, sanitize_bytes};
use stw::absorb_commute::{commute, commute_diff_before, lines_match, uniform, Hunk};

fn h(add_start: i64, add: &[&str], rem_start: i64, rem: &[&str]) -> Hunk {
    let l = |x: &[&str]| x.iter().map(|s| format!("{s}\n").into_bytes()).collect();
    Hunk {
        add_start,
        add: l(add),
        rem_start,
        rem: l(rem),
    }
}

#[test]
fn commute_two_insertions() {
    let out = commute(&h(2, &["foo"], 1, &[]), &h(1, &["bar"], 0, &[])).expect("commutes");
    assert_eq!(out.add_start, 1);
}

#[test]
fn commute_after_hunk_moves_up_by_the_offset() {
    let patch = [h(1, &["bar"], 0, &[]), h(3, &["bar"], 1, &[])];
    let out = commute_diff_before(&h(5, &["bar"], 4, &[]), &patch).expect("commutes");
    assert_eq!((out.add_start, out.rem_start), (3, 2));
}

#[test]
fn commute_below_the_first_hunk_shifts_both_sides_by_minus_its_offset() {
    let first = h(2, &["a", "b", "c"], 1, &[]);
    let out = commute(&first, &h(10, &["x"], 9, &["y"])).expect("commutes");
    assert_eq!((out.add_start, out.rem_start), (7, 6));
}

#[test]
fn a_hunk_above_the_first_one_is_unchanged() {
    let first = h(10, &["a"], 9, &[]);
    let second = h(3, &["x"], 3, &["y"]);
    assert_eq!(commute(&first, &second), Some(second));
}

#[test]
fn overlapping_hunks_do_not_commute() {
    assert!(commute(&h(3, &["x"], 3, &["y"]), &h(3, &["z"], 3, &["x"])).is_none());
}

#[test]
fn a_hunk_that_does_not_commute_with_one_hunk_of_the_patch_stops_the_chain() {
    let patch = [h(3, &["x"], 3, &["y"]), h(20, &["q"], 20, &["r"])];
    assert!(commute_diff_before(&h(3, &["z"], 3, &["x"]), &patch).is_none());
}

#[test]
fn hunks_of_the_same_repeated_line_commute_whatever_the_offsets() {
    let four = ["bar"; 4];
    let two = ["bar"; 2];
    assert!(commute(&h(1, &four, 0, &[]), &h(1, &two, 0, &[])).is_some());
    assert!(commute(&h(1, &two, 0, &[]), &h(1, &["baz"; 2], 0, &[])).is_none());
}

#[test]
fn two_removals_of_the_same_repeated_line_commute_and_others_do_not() {
    assert!(commute(&h(1, &[], 4, &["bar"; 4]), &h(1, &[], 2, &["bar"; 2])).is_some());
    assert!(commute(&h(1, &[], 4, &["bar"; 4]), &h(1, &[], 2, &["baz"; 2])).is_none());
}

#[test]
fn uniform_is_true_for_equal_items_only() {
    assert!(uniform(&[b"a\n".as_slice(), b"a\n".as_slice()]));
    assert!(!uniform(&[b"a\n".as_slice(), b"b\n".as_slice()]));
    assert!(uniform(&[]));
}

#[test]
fn anchors_of_the_shapes() {
    assert_eq!(h(1, &["a"], 1, &["b", "c"]).anchors(), (0, 3, 0, 2));
    assert_eq!(h(4, &[], 3, &["b", "c"]).anchors(), (2, 5, 2, 3));
    assert_eq!(h(4, &["a", "b"], 3, &[]).anchors(), (3, 4, 3, 6));
    assert_eq!(h(1, &[], 0, &[]).anchors(), (0, 1, 0, 1));
}

#[test]
fn offset_and_shift() {
    assert_eq!(h(1, &["a", "b", "c"], 1, &["x"]).offset(), 2);
    let s = h(5, &["a"], 4, &[]).shifted(-2, 3);
    assert_eq!((s.add_start, s.rem_start), (3, 7));
}

#[test]
fn consistency_check_finds_the_removed_lines_in_the_blob() {
    let hunk = h(3, &["new"], 3, &["l03"]);
    assert!(lines_match(b"l01\nl02\nl03\nl04\n", &hunk));
    assert!(!lines_match(b"l01\nl02\nXXX\nl04\n", &hunk));
    assert!(!lines_match(b"l01\n", &hunk));
    let ins = h(3, &["new"], 2, &[]);
    assert!(lines_match(b"l01\nl02\n", &ins));
    assert!(!lines_match(b"l01\n", &ins));
}

#[test]
fn consistency_check_handles_a_last_line_without_newline() {
    let hunk = Hunk {
        add_start: 3,
        add: vec![b"x".to_vec()],
        rem_start: 3,
        rem: vec![b"three".to_vec()],
    };
    assert!(lines_match(b"one\ntwo\nthree", &hunk));
    assert!(!lines_match(b"one\ntwo\nthree\n", &hunk));
}

#[test]
fn sanitize_masks_controls_bidi_zero_width_and_separators() {
    assert_eq!(
        sanitize(
            "a\u{0}b\u{7f}c\u{85}d\u{2028}e\u{202e}f\u{2066}g\u{200b}h\u{feff}i\u{2060}j\u{61c}k"
        ),
        "a?b?c?d?e?f?g?h?i?j?k"
    );
    assert_eq!(
        sanitize("\u{9f}\u{80}\u{2029}\u{200e}\u{200f}\u{202a}\u{2069}"),
        "???????"
    );
    assert_eq!(sanitize("ü 中 ok"), "ü 中 ok");
}

#[test]
fn sanitize_bytes_masks_every_byte_of_invalid_utf8() {
    assert_eq!(sanitize_bytes(b"k\x80l"), "k?l");
    assert_eq!(sanitize_bytes(b"\xff\xfe"), "??");
    assert_eq!(sanitize_bytes(b"a\xe4\xb8"), "a??");
    assert_eq!(sanitize_bytes("ü\u{1b}".as_bytes()), "ü?");
}

#[test]
fn verdict_of_one_hunk() {
    assert_eq!(decide(true, 1, false), Ok(()));
    assert_eq!(decide(true, 0, false), Ok(()));
    assert_eq!(decide(false, 0, false), Err("conflict"));
    assert_eq!(decide(true, 2, false), Err("ambiguous"));
    assert_eq!(decide(true, 2, true), Ok(()));
    assert_eq!(decide(false, 3, true), Err("conflict"));
    assert_eq!(decide(false, 3, false), Err("conflict"));
}
