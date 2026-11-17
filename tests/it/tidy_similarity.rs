use stw::tidy_similarity::*;

fn e(path: &str, sign: u8, line: &str) -> Entry {
    Entry {
        path: path.as_bytes().to_vec(),
        sign,
        line: trim_line(line.as_bytes()).to_vec(),
    }
}

/// Two multisets of (a, b) keys that share `i` keys. The shared keys are `+s<k>`, the others are unique per side.
fn pair(a: usize, b: usize, i: usize, path_b: &str) -> (Keys, Keys) {
    let mut ea: Vec<Entry> = (0..i).map(|k| e("f.txt", b'+', &format!("s{k}"))).collect();
    let mut eb: Vec<Entry> = (0..i).map(|k| e(path_b, b'+', &format!("s{k}"))).collect();
    ea.extend((0..a - i).map(|k| e("f.txt", b'+', &format!("a{k}"))));
    eb.extend((0..b - i).map(|k| e(path_b, b'+', &format!("b{k}"))));
    (Keys::from_entries(&ea), Keys::from_entries(&eb))
}

fn shown(s: Sim) -> String {
    fmt_permille(s.permille())
}

#[test]
fn dice_numbers() {
    for (a, b, i, want) in [
        (6, 6, 5, "83.3"),
        (45, 45, 41, "91.1"),
        (46, 46, 35, "76.1"),
        (100, 100, 87, "87.0"),
        (20, 20, 19, "95.0"),
        (19, 19, 18, "94.7"),
        (10, 10, 10, "100.0"),
        (10, 10, 0, "0.0"),
    ] {
        let (ka, kb) = pair(a, b, i, "f.txt");
        let s = ka.similarity(&kb);
        assert_eq!((s.inter, s.a, s.b), (i as u64, a as u64, b as u64));
        assert_eq!(shown(s), want, "({a},{b},{i})");
    }
}

#[test]
fn threshold_is_at_or_above_in_integers() {
    let (a, b) = pair(20, 20, 19, "f.txt");
    assert!(a.similarity(&b).ge_pct(95), "exactly 95.0 counts");
    let (a, b) = pair(19, 19, 18, "f.txt");
    assert!(!a.similarity(&b).ge_pct(95), "94.7 is below");
    // 2*I/(A+B) = 94.96...: shown rounds to 95.0 but is below the threshold, the display must not claim 95.0
    let (a, b) = pair(1000, 1000, 950, "f.txt");
    assert!(a.similarity(&b).ge_pct(95));
    let (a, b) = pair(5000, 5001, 4749, "f.txt");
    let s = a.similarity(&b);
    assert_eq!(s.permille(), 950, "rounds up to 95.0");
    assert!(!s.ge_pct(95));
    assert_eq!(s.shown(95), 949, "shown as 94.9 so that display and class agree");
    assert_eq!(fmt_permille(s.shown(95)), "94.9");
}

#[test]
fn a_wrong_pair_with_another_path_is_zero_and_without_path_small() {
    let (a, b) = pair(30, 30, 1, "g.txt");
    assert_eq!(shown(a.similarity(&b)), "0.0");
    assert_eq!(shown(a.similarity_no_path(&b)), "3.3");
}

#[test]
fn the_sign_is_part_of_the_key() {
    let a = Keys::from_entries(&[e("f", b'+', "x")]);
    let b = Keys::from_entries(&[e("f", b'-', "x")]);
    assert_eq!(a.similarity(&b).inter, 0);
    assert_eq!(a.similarity_no_path(&b).inter, 0);
}

#[test]
fn the_path_is_part_of_the_key_but_not_of_the_no_path_value() {
    let a = Keys::from_entries(&[e("f", b'+', "x")]);
    let b = Keys::from_entries(&[e("g", b'+', "x")]);
    assert_eq!(a.similarity(&b).inter, 0);
    assert_eq!(a.similarity_no_path(&b).inter, 1);
}

#[test]
fn a_line_that_occurs_twice_counts_twice() {
    let a = Keys::from_entries(&[e("f", b'+', "x"), e("f", b'+', "x"), e("f", b'+', "x")]);
    let b = Keys::from_entries(&[e("f", b'+', "x")]);
    let s = a.similarity(&b);
    assert_eq!((s.inter, s.a, s.b), (1, 3, 1), "multiset, not set");
    let c = Keys::from_entries(&[e("f", b'+', "x"), e("f", b'+', "x")]);
    assert_eq!(a.similarity(&c).inter, 2);
}

#[test]
fn only_the_white_space_borders_are_trimmed() {
    assert_eq!(trim_line(b"  \t x y \r\n"), b"x y");
    assert_eq!(trim_line(b"\r\n"), b"");
    assert_eq!(trim_line(b"x\x0b\x0c"), b"x");
    assert_eq!(trim_line(b"x  y"), b"x  y", "inner white space stays");
    let a = Keys::from_entries(&[e("f", b'+', "    x();\r\n")]);
    let b = Keys::from_entries(&[e("f", b'+', "x();")]);
    assert_eq!(a.similarity(&b).inter, 1, "indentation and CR do not matter");
    let c = Keys::from_entries(&[e("f", b'+', "x( );")]);
    assert_eq!(a.similarity(&c).inter, 0, "inner white space does");
}

#[test]
fn the_comparison_of_values_is_exact() {
    let lo = Sim { inter: 1, a: 3, b: 3 };
    let hi = Sim { inter: 2, a: 5, b: 5 };
    assert_eq!(lo.cmp_value(&hi), std::cmp::Ordering::Less);
    assert_eq!(hi.cmp_value(&lo), std::cmp::Ordering::Greater);
    let same = Sim { inter: 2, a: 6, b: 6 };
    assert_eq!(lo.cmp_value(&same), std::cmp::Ordering::Equal);
}

#[test]
fn a_sum_of_commits_and_containment() {
    // a squash of two commits contains both; containment is an integer pair
    let c1 = Keys::from_entries(&[e("f", b'+', "1"), e("f", b'+', "2")]);
    let c2 = Keys::from_entries(&[e("g", b'-', "3")]);
    let squash = c1.sum(&c2);
    assert_eq!(squash.len(), 3);
    let s = c1.similarity(&squash);
    assert_eq!(s.containment_in_a(), (2, 2), "all of c1 is in the squash");
    assert_eq!(s.containment_in_b(), (2, 3), "two thirds of the squash are c1");
    assert_eq!(c1.sum(&c2), c2.sum(&c1), "the sum does not depend on the order");
}
