//! The pure comparison functions of the tidy oracle (`src/tidy_oracle.rs`): no network, no clone, throw-away fixtures.
use stw::tidy_oracle::*;
use stw::tidy_similarity::{Entry, Keys, Sim};

fn e(path: &str, sign: u8, line: &str) -> Entry {
    Entry {
        path: path.as_bytes().to_vec(),
        sign,
        line: line.as_bytes().to_vec(),
    }
}

fn sim(inter: u64, a: u64, b: u64) -> Sim {
    Sim { inter, a, b }
}

// ---- the diff of git ----

#[test]
fn a_modified_file_gives_its_changed_lines_trimmed_with_the_path() {
    let d = b"diff --git a/src/a.txt b/src/a.txt\nindex 111..222 100644\n--- a/src/a.txt\n+++ b/src/a.txt\n@@ -2 +2,2 @@ ctx\n-  old \t\n+new\r\n+ more\n@@ -9,0 +11 @@\n+tail\n";
    assert_eq!(
        parse_unified_u0(d).unwrap(),
        vec![
            e("src/a.txt", b'-', "old"),
            e("src/a.txt", b'+', "new"),
            e("src/a.txt", b'+', "more"),
            e("src/a.txt", b'+', "tail"),
        ]
    );
}

#[test]
fn an_added_file_takes_the_new_path_and_a_deleted_file_the_old_one() {
    let d = b"diff --git a/n.txt b/n.txt\nnew file mode 100644\nindex 000..111\n--- /dev/null\n+++ b/n.txt\n@@ -0,0 +1 @@\n+one\ndiff --git a/o.txt b/o.txt\ndeleted file mode 100644\nindex 111..000\n--- a/o.txt\n+++ /dev/null\n@@ -1 +0,0 @@\n-gone\n";
    assert_eq!(
        parse_unified_u0(d).unwrap(),
        vec![e("n.txt", b'+', "one"), e("o.txt", b'-', "gone")]
    );
}

#[test]
fn a_changed_line_that_looks_like_a_header_is_a_line() {
    // the removed line "-- x" is printed as "--- x", the added line "++ y" as "+++ y"
    let d = b"diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1 +1 @@\n--- x\n+++ y\n";
    assert_eq!(
        parse_unified_u0(d).unwrap(),
        vec![e("f", b'-', "-- x"), e("f", b'+', "++ y")]
    );
}

#[test]
fn a_binary_file_a_mode_change_and_the_no_newline_marker_give_no_entry() {
    let d = b"diff --git a/b.bin b/b.bin\nindex 1..2 100644\nBinary files a/b.bin and b/b.bin differ\ndiff --git a/x.sh b/x.sh\nold mode 100644\nnew mode 100755\ndiff --git a/t b/t\n--- a/t\n+++ b/t\n@@ -1 +1 @@\n-a\n\\ No newline at end of file\n+b\n\\ No newline at end of file\n";
    assert_eq!(
        parse_unified_u0(d).unwrap(),
        vec![e("t", b'-', "a"), e("t", b'+', "b")]
    );
    assert_eq!(parse_unified_u0(b"").unwrap(), vec![]);
}

#[test]
fn a_path_with_a_space_loses_the_tab_git_adds_and_a_quoted_path_is_an_error() {
    let d =
        b"diff --git a/a b.txt b/a b.txt\n--- a/a b.txt\t\n+++ b/a b.txt\t\n@@ -0,0 +1 @@\n+x\n";
    assert_eq!(parse_unified_u0(d).unwrap(), vec![e("a b.txt", b'+', "x")]);
    let q = b"diff --git \"a/t\\tab\" \"b/t\\tab\"\n--- \"a/t\\tab\"\n+++ \"b/t\\tab\"\n@@ -0,0 +1 @@\n+x\n";
    let err = parse_unified_u0(q).unwrap_err();
    assert!(err.starts_with("a quoted path"), "{err}");
}

#[test]
fn text_keys_leave_out_the_marker_entries_of_gix() {
    let all = vec![
        e("a", b'+', "x"),
        e("a", b'-', "y"),
        e("b", b'B', "bin 1"),
        e("c", b'M', "100644>100755"),
        e("d", b'N', ""),
        e("e", b'X', ""),
    ];
    let text = vec![e("a", b'+', "x"), e("a", b'-', "y")];
    assert_eq!(text_keys(&all), Keys::from_entries(&text));
}

#[test]
fn equal_multisets_do_not_differ_and_a_size_or_content_difference_does() {
    assert!(!differs(&sim(5, 5, 5)));
    assert!(!differs(&sim(0, 0, 0)));
    assert!(differs(&sim(4, 5, 5)));
    assert!(differs(&sim(5, 5, 6)));
    assert!(differs(&sim(5, 6, 5)));
}

// ---- tally and matrix ----

#[test]
fn a_tally_counts_commits_and_differing_ones_and_keeps_the_worst_first() {
    let mut t = Tally::default();
    assert_eq!(t.share_permille(), None);
    assert_eq!(t.worst_permille(), None);
    t.add("aa", sim(5, 5, 5));
    t.add("c2", sim(8, 10, 10));
    t.add("c1", sim(8, 10, 10));
    t.add("bb", sim(3, 10, 10));
    t.add("dd", sim(9, 10, 10));
    assert_eq!((t.total, t.differing), (5, 4));
    let ids: Vec<&str> = t.worst.iter().map(|w| w.0.as_str()).collect();
    // lowest value first; equal values: the smaller commit ID
    assert_eq!(ids, ["bb", "c1", "c2", "dd"]);
    assert_eq!(t.worst_permille(), Some(300));
    // 4 of 5 = 80.0 %
    assert_eq!(t.share_permille(), Some(800));
}

#[test]
fn the_share_is_rounded_half_up_and_the_worst_list_is_cut() {
    let mut t = Tally::default();
    for i in 0..8 {
        t.add(&format!("x{i}"), sim(1, 2, 2));
    }
    for i in 0..1992 {
        t.add(&format!("y{i}"), sim(2, 2, 2));
    }
    // 8 of 2000 = 0.4 %
    assert_eq!(t.share_permille(), Some(4));
    assert_eq!(t.worst.len(), KEEP_WORST);
    let mut u = Tally::default();
    u.add("a", sim(0, 1, 1));
    for _ in 0..199 {
        u.add("z", sim(1, 1, 1));
    }
    // 1 of 200 = 0.5 %
    assert_eq!(u.share_permille(), Some(5));
    let mut h = Tally::default();
    h.add("a", sim(0, 1, 1));
    for _ in 0..2 {
        h.add("z", sim(1, 1, 1));
    }
    // 1 of 3 = 33.33 -> 333
    assert_eq!(h.share_permille(), Some(333));
    let mut r = Tally::default();
    r.add("a", sim(0, 1, 1));
    for _ in 0..1999 {
        r.add("z", sim(1, 1, 1));
    }
    // 1 of 2000 = 0.05 % = 0.5 permille: half up gives 1 permille
    assert_eq!(r.share_permille(), Some(1));
}

#[test]
fn the_matrix_has_one_row_per_setting_in_name_order_and_renders_a_table() {
    let mut m = Matrix::default();
    m.add("git-patience", "1111111111111111", sim(10, 10, 10));
    m.add("git-myers", "aaaaaaaaaaaaaaaaaaaa", sim(9, 10, 10));
    m.add("git-myers", "bbbbbbbbbbbbbbbbbbbb", sim(5, 5, 5));
    assert_eq!(m.get("git-myers").map(|t| t.total), Some(2));
    assert!(m.get("git-minimal").is_none());
    assert_eq!(
        m.render_table(),
        "| setting | commits | differing | share % | worst % |\n|---|---|---|---|---|\n| git-myers | 2 | 1 | 50.0 | 90.0 (aaaaaaaaaaaa) |\n| git-patience | 1 | 0 | 0.0 | - |\n"
    );
}

#[test]
fn counterexamples_name_setting_commit_value_and_sizes() {
    let mut m = Matrix::default();
    m.add("git-histogram", "abcdef0123456789", sim(19, 40, 38));
    m.add("git-myers", "0123456789abcdef", sim(1, 1, 1));
    assert_eq!(
        m.render_counterexamples(),
        "git-histogram abcdef012345 48.7 % (19 of 40 and 38)\n"
    );
}
