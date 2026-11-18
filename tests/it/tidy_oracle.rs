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
