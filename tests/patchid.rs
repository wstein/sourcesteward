mod common;
use common::*;
use stw::patch_id::commit_patch_id;

fn pid(fx: &Fx, c: gix::ObjectId) -> gix::ObjectId {
    commit_patch_id(&fx.repo(), c)
        .unwrap()
        .expect("has a patch id")
}

#[test]
fn same_change_on_different_base_has_equal_patch_id() {
    let fx = Fx::new();
    let base = fx.commit(
        None,
        &[("a.txt", Some(&lines(40, "x"))), ("b.txt", Some("b\n"))],
        "base",
    );
    // two histories: the change is the same single-line edit, but line numbers differ (extra lines on top)
    let more = fx.commit(
        Some(base),
        &[(
            "a.txt",
            Some(&format!("new1\nnew2\nnew3\n{}", lines(40, "x"))),
        )],
        "more lines on top",
    );
    let edit_a = fx.commit(
        Some(base),
        &[("a.txt", Some(&lines(40, "x").replace("x20\n", "CHANGED\n")))],
        "edit",
    );
    let edit_b = fx.commit(
        Some(more),
        &[(
            "a.txt",
            Some(&format!(
                "new1\nnew2\nnew3\n{}",
                lines(40, "x").replace("x20\n", "CHANGED\n")
            )),
        )],
        "edit on shifted file",
    );
    assert_eq!(
        pid(&fx, edit_a),
        pid(&fx, edit_b),
        "line numbers must not matter"
    );
}

#[test]
fn whitespace_differences_do_not_matter_but_content_does() {
    let fx = Fx::new();
    let base = fx.commit(None, &[("a.txt", Some("one\ntwo\nthree\n"))], "base");
    let a = fx.commit(
        Some(base),
        &[("a.txt", Some("one\ntwo changed\nthree\n"))],
        "a",
    );
    let b = fx.commit(
        Some(base),
        &[("a.txt", Some("one\n  two   changed\t\nthree\n"))],
        "b",
    );
    let c = fx.commit(
        Some(base),
        &[("a.txt", Some("one\ntwo different\nthree\n"))],
        "c",
    );
    assert_eq!(
        pid(&fx, a),
        pid(&fx, b),
        "whitespace is removed before hashing"
    );
    assert_ne!(
        pid(&fx, a),
        pid(&fx, c),
        "a different change has a different id"
    );
}

#[test]
fn file_order_and_message_do_not_matter() {
    let fx = Fx::new();
    let base = fx.commit(
        None,
        &[("a.txt", Some("1\n")), ("z.txt", Some("1\n"))],
        "base",
    );
    let a = fx.commit(
        Some(base),
        &[("a.txt", Some("2\n")), ("z.txt", Some("2\n"))],
        "message one",
    );
    let b = fx.commit(
        Some(base),
        &[("z.txt", Some("2\n")), ("a.txt", Some("2\n"))],
        "another message",
    );
    assert_eq!(pid(&fx, a), pid(&fx, b));
    let only_a = fx.commit(Some(base), &[("a.txt", Some("2\n"))], "only a");
    assert_ne!(pid(&fx, a), pid(&fx, only_a));
}

#[test]
fn path_matters() {
    let fx = Fx::new();
    let base = fx.commit(
        None,
        &[("a.txt", Some("1\n")), ("b.txt", Some("1\n"))],
        "base",
    );
    let a = fx.commit(Some(base), &[("a.txt", Some("2\n"))], "a");
    let b = fx.commit(Some(base), &[("b.txt", Some("2\n"))], "b");
    assert_ne!(pid(&fx, a), pid(&fx, b));
}

#[test]
fn merge_and_empty_commits_have_no_patch_id() {
    let fx = Fx::new();
    let base = fx.commit(None, &[("a.txt", Some("1\n"))], "base");
    let x = fx.commit(Some(base), &[("a.txt", Some("2\n"))], "x");
    let y = fx.commit(Some(base), &[("b.txt", Some("2\n"))], "y");
    let merge = fx.commit_with_parents(vec![x, y], Some(x), &[("b.txt", Some("2\n"))], "merge");
    assert!(commit_patch_id(&fx.repo(), merge).unwrap().is_none());
    let empty = fx.commit(Some(base), &[], "empty");
    assert!(commit_patch_id(&fx.repo(), empty).unwrap().is_none());
}

/// Literal produced by `git patch-id --stable` on `git diff-tree -p --no-renames -r --root <c>` for the
/// same file contents (captured once with the oracle; see tests/oracle/README in SPIKE.md).
#[test]
fn matches_git_patch_id_stable_on_known_vectors() {
    let fx = Fx::new();
    let before = "alpha\nbeta\ngamma\ndelta\nepsilon\nzeta\neta\ntheta\n";
    let after = "alpha\nbeta\ngamma\nDELTA\nepsilon\nzeta\neta\ntheta\niota\n";
    let base = fx.commit(
        None,
        &[("dir/f.txt", Some(before)), ("g.txt", Some("g\n"))],
        "base",
    );
    let c = fx.commit(
        Some(base),
        &[
            ("dir/f.txt", Some(after)),
            ("g.txt", None),
            ("new file.txt", Some("n\n")),
        ],
        "change",
    );
    assert_eq!(
        pid(&fx, c).to_string(),
        include_str!("vectors/modify_delete_add.id").trim()
    );
    // no newline at end of file: the marker line is not hashed
    let d = fx.commit(Some(c), &[("dir/f.txt", Some("alpha\nbeta"))], "no newline");
    assert_eq!(
        pid(&fx, d).to_string(),
        include_str!("vectors/no_newline.id").trim()
    );
}

/// Paths that git quotes in diff headers (`core.quotePath=true`: control characters, `"`, `\\`, bytes >= 0x80
/// as octal escapes) and paths that are not UTF-8. Expected IDs are the first field of
/// `git diff-tree -p --no-renames -r --root <c> | git patch-id --stable` for a commit adding the file
/// `new\n` on top of a commit with the file `base` (captured once with git 2.x as the oracle).
#[test]
fn quoted_and_non_utf8_paths_match_git_patch_id_stable() {
    let cases: &[(&[u8], &str)] = &[
        (b"\xff.txt", "f971163cd195fbabde5f9bd07f365b69797d2259"),
        (b"\xfe.txt", "76708d575358335c5231c6aa61f1320390b96ae5"),
        (b"\xc3\xbc.txt", "a12c693af8ae99d796ab163cc4d391e913f19adb"),
        (b"t\tab.txt", "faae4ad69a0dc6143099a6fceff270da7fb62492"),
        (b"q\"uote.txt", "ff18615e2275a8a676ab1526f29ab14125a1db0a"),
        (b"sp ace.txt", "a0a1149c3668767a807914ec85e9b042e0e99b16"),
        (b"a\\b.txt", "a776b496cf58e9ec19a57a9c1eb38424e1394a27"),
    ];
    let fx = Fx::new();
    let base = fx.commit(None, &[("base", Some("x\n"))], "base");
    for (name, want) in cases {
        let c = fx.commit_bytes_path(base, name, b"new\n");
        assert_eq!(
            pid(&fx, c).to_string(),
            *want,
            "path {:?}",
            String::from_utf8_lossy(name)
        );
    }
}

/// Two different non-UTF-8 paths must never produce the same patch ID (tidy treats equal IDs as proof
/// that a branch is already merged and deletes it).
#[test]
fn distinct_non_utf8_paths_have_distinct_patch_ids() {
    let fx = Fx::new();
    let base = fx.commit(None, &[("base", Some("x\n"))], "base");
    let a = fx.commit_bytes_path(base, b"\xff.txt", b"new\n");
    let b = fx.commit_bytes_path(base, b"\xfe.txt", b"new\n");
    assert_ne!(pid(&fx, a), pid(&fx, b));
}
