//! Patch IDs of commits with renames, copies, mode changes and binary files against `git patch-id --stable`
//! on `git diff-tree -p -M --root <commit>` (git's default for `git diff` and `git show`: rename detection on,
//! copy detection off, similarity threshold 50 %).
//!
//! Every case builds one commit on top of a parentless base commit. The expected IDs are the first field of
//! `git patch-id --stable` on that commit with git 2.47.3, produced by the ignored test
//! `oracle_against_git` (it runs the installed git on the fixture repository and prints `name id`):
//! `cargo test --test it patchid_rename::oracle_against_git -- --ignored --nocapture`.
use crate::common::*;
use gix::objs::tree::EntryKind;
use gix::ObjectId;
use stw::patch_id::commit_patch_id;

type Tree = Vec<(&'static str, EntryKind, Vec<u8>)>;

fn t(path: &'static str, content: &str) -> (&'static str, EntryKind, Vec<u8>) {
    (path, EntryKind::Blob, content.as_bytes().to_vec())
}
fn x(path: &'static str, content: &str) -> (&'static str, EntryKind, Vec<u8>) {
    (path, EntryKind::BlobExecutable, content.as_bytes().to_vec())
}
fn l(path: &'static str, target: &str) -> (&'static str, EntryKind, Vec<u8>) {
    (path, EntryKind::Link, target.as_bytes().to_vec())
}
fn bin(path: &'static str, seed: u32, len: u32, tail: &[u8]) -> (&'static str, EntryKind, Vec<u8>) {
    let mut v: Vec<u8> = (0..len)
        .map(|i| ((i * 131 + (i / 7) * 17 + seed) % 251) as u8)
        .collect();
    v.extend_from_slice(tail);
    (path, EntryKind::Blob, v)
}
/// `n` lines "<tag> line <k>".
fn body(n: usize, tag: &str) -> String {
    (1..=n).map(|k| format!("{tag} line {k}\n")).collect()
}

fn commit_tree(fx: &Fx, parent: Option<ObjectId>, tree: &Tree) -> ObjectId {
    let repo = fx.repo();
    let mut ed = repo.edit_tree(repo.empty_tree().id).unwrap();
    for (path, kind, data) in tree {
        let blob = repo.write_blob(data).unwrap().detach();
        ed.upsert(*path, *kind, blob).unwrap();
    }
    let tree = ed.write().unwrap().detach();
    let sig = gix::actor::Signature {
        name: "T".into(),
        email: "t@example.com".into(),
        time: gix::date::Time::new(1_700_000_000, 0),
    };
    let commit = gix::objs::Commit {
        tree,
        parents: parent.into_iter().collect(),
        author: sig.clone(),
        committer: sig,
        encoding: None,
        message: "c".into(),
        extra_headers: vec![],
    };
    repo.write_object(&commit).unwrap().detach()
}

/// The commit that turns the tree `before` into `after`.
fn change(fx: &Fx, before: Tree, after: Tree) -> ObjectId {
    let base = commit_tree(fx, None, &before);
    commit_tree(fx, Some(base), &after)
}

fn pid(fx: &Fx, c: ObjectId) -> String {
    commit_patch_id(&fx.repo(), c)
        .unwrap()
        .expect("has a patch id")
        .to_string()
}

fn edited(text: &str, from: &str, to: &str) -> String {
    assert!(text.contains(from));
    text.replace(from, to)
}

fn pure_rename(fx: &Fx) -> ObjectId {
    change(
        fx,
        vec![t("old.txt", &body(30, "a"))],
        vec![t("new.txt", &body(30, "a"))],
    )
}
fn rename_change(fx: &Fx) -> ObjectId {
    let a = body(30, "a");
    change(
        fx,
        vec![t("old.txt", &a)],
        vec![t("new.txt", &edited(&a, "a line 7\n", "CHANGED\n"))],
    )
}
fn rename_mode(fx: &Fx) -> ObjectId {
    change(
        fx,
        vec![t("old.txt", &body(30, "a"))],
        vec![x("new.txt", &body(30, "a"))],
    )
}
fn rename_mode_change(fx: &Fx) -> ObjectId {
    let a = body(30, "a");
    change(
        fx,
        vec![t("old.txt", &a)],
        vec![x("new.txt", &edited(&a, "a line 7\n", "CHANGED\n"))],
    )
}
fn copy_is_an_addition(fx: &Fx) -> ObjectId {
    let a = body(30, "a");
    change(
        fx,
        vec![t("old.txt", &a)],
        vec![t("old.txt", &a), t("dup.txt", &a)],
    )
}
fn copy_with_change(fx: &Fx) -> ObjectId {
    let a = body(30, "a");
    change(
        fx,
        vec![t("old.txt", &a)],
        vec![
            t("old.txt", &format!("{a}more\n")),
            t("dup.txt", &edited(&a, "a line 7\n", "CHANGED\n")),
        ],
    )
}
fn directory_rename(fx: &Fx) -> ObjectId {
    let (a, b, c) = (body(30, "a"), body(30, "b"), body(30, "c"));
    change(
        fx,
        vec![t("d/a.txt", &a), t("d/b.txt", &b), t("d/c.txt", &c)],
        vec![
            t("e/a.txt", &a),
            t("e/b.txt", &edited(&b, "b line 3\n", "B3\n")),
            t("e/c.txt", &c),
        ],
    )
}
fn below_threshold(fx: &Fx) -> ObjectId {
    let new: String = body(20, "z").replacen("z line", "a line", 5);
    change(
        fx,
        vec![t("old.txt", &body(20, "a"))],
        vec![t("new.txt", &new)],
    )
}
fn exactly_half_similar(fx: &Fx) -> ObjectId {
    // 20 lines of 4 bytes against 10 equal lines and 10 other lines of 4 bytes: 50.00 %
    let old: String = (0..20).map(|k| format!("x{k:02}\n")).collect();
    let new: String = (0..10)
        .map(|k| format!("x{k:02}\n"))
        .chain((0..10).map(|k| format!("z{k:02}\n")))
        .collect();
    change(fx, vec![t("old.txt", &old)], vec![t("new.txt", &new)])
}
fn just_below_half(fx: &Fx) -> ObjectId {
    let old: String = (0..20).map(|k| format!("x{k:02}\n")).collect();
    let new: String = (0..9)
        .map(|k| format!("x{k:02}\n"))
        .chain((0..11).map(|k| format!("z{k:02}\n")))
        .collect();
    change(fx, vec![t("old.txt", &old)], vec![t("new.txt", &new)])
}
fn binary_rename(fx: &Fx) -> ObjectId {
    change(
        fx,
        vec![bin("old.bin", 1, 3000, b"")],
        vec![bin("new.bin", 1, 3000, b"")],
    )
}
fn empty_file_rename(fx: &Fx) -> ObjectId {
    change(
        fx,
        vec![t("old.txt", ""), t("other.txt", &body(5, "k"))],
        vec![t("new.txt", ""), t("other.txt", &body(5, "k"))],
    )
}
fn multi_commit_first(fx: &Fx) -> ObjectId {
    change(
        fx,
        vec![t("one.txt", &body(30, "a"))],
        vec![t("two.txt", &body(30, "a"))],
    )
}
fn multi_commit_second(fx: &Fx) -> ObjectId {
    let first = multi_commit_first(fx);
    let a = body(30, "a");
    commit_tree(
        fx,
        Some(first),
        &vec![t("three.txt", &edited(&a, "a line 9\n", "N9\n"))],
    )
}
fn names_with_blank_and_utf8(fx: &Fx) -> ObjectId {
    change(
        fx,
        vec![t("a b.txt", &body(30, "a"))],
        vec![t("c d \u{e9}.txt", &body(30, "a"))],
    )
}
fn identical_twins(fx: &Fx) -> ObjectId {
    let a = body(30, "a");
    change(
        fx,
        vec![t("x/f.txt", &a), t("y/g.txt", &a)],
        vec![t("z/f.txt", &a)],
    )
}
fn symlink_rename(fx: &Fx) -> ObjectId {
    change(fx, vec![l("old", "target")], vec![l("new", "target")])
}
fn contents_swapped(fx: &Fx) -> ObjectId {
    let (a, b) = (body(30, "a"), body(30, "b"));
    change(
        fx,
        vec![t("p.txt", &a), t("q.txt", &b)],
        vec![t("p.txt", &b), t("q.txt", &a)],
    )
}
fn rename_and_similar_new_file(fx: &Fx) -> ObjectId {
    let a = body(30, "a");
    change(
        fx,
        vec![t("old.txt", &a)],
        vec![
            t("new.txt", &a),
            t("extra.txt", &edited(&a, "a line 2\n", "X2\n")),
        ],
    )
}
fn rename_with_crlf(fx: &Fx) -> ObjectId {
    let a: String = (1..=30).map(|k| format!("a line {k}\r\n")).collect();
    change(
        fx,
        vec![t("old.txt", &a)],
        vec![t("new.txt", &edited(&a, "a line 4\r\n", "a line four\r\n"))],
    )
}
fn deleted_and_added_unrelated(fx: &Fx) -> ObjectId {
    change(
        fx,
        vec![t("gone.txt", &body(30, "a"))],
        vec![t("new.txt", &body(30, "q"))],
    )
}

macro_rules! cases {
    ($($name:ident => $want:expr,)*) => {
        $(
            #[test]
            fn $name() {
                let fx = Fx::new();
                let c = super::$name(&fx);
                assert_eq!(pid(&fx, c), $want, "{}", stringify!($name));
            }
        )*
        const ALL: &[(&str, fn(&Fx) -> ObjectId)] = &[$((stringify!($name), super::$name)),*];
    };
}

mod pinned {
    use super::*;
    cases! {
        pure_rename => "8655929933e516a7331860722ad5ea60579025dd",
        rename_change => "06ae28b928da6868804c8cf1209729489de6eaf8",
        rename_mode => "d8e7bf857543698a8ce6808bab7e0d4a8f5fcaa2",
        rename_mode_change => "51a100b3f7cfb204ac48a3a7f256c4bfa0df2d51",
        copy_is_an_addition => "39eeabe74137ecfc932fef5c2e5e887ce78ba09a",
        copy_with_change => "4416fe786e765328c97af91a78807e8c67105635",
        directory_rename => "65df9ec5e50ad8f3fb0386df6fa7fa299a7b8bdb",
        below_threshold => "2ada010a889083503300136f608048b91b206a7d",
        exactly_half_similar => "9c9c8d99705a166aea142fc2598e9ede5e34988d",
        just_below_half => "2fa9b97aa42cd7e23dbcce263b5dd848eee08899",
        binary_rename => "db690cb30b7aea7a27cd031544ee406d7c401730",
        empty_file_rename => "8655929933e516a7331860722ad5ea60579025dd",
        multi_commit_first => "9794667d2b40f57d292670f9a77d89f357d90475",
        multi_commit_second => "5a96ed9fe4629de3f3f05f15ee1651edcb0fac30",
        names_with_blank_and_utf8 => "4449962a572ccd90b7d2ad9a652ea7c9b7c410ab",
        identical_twins => "b9569c6eaf4c93a870e36e28abf7837b59e3d07c",
        symlink_rename => "8e78bdefc8f0bc26c65eb193a6d5d3cac408bab2",
        contents_swapped => "302ba4b49782057e95797e0093b1c6a3ef85897a",
        rename_and_similar_new_file => "e248fd04bca57a4b299358385809c420d8d8238e",
        rename_with_crlf => "a6855c4eb45e7fa30cffe098b82dcbcb573a5bde",
        deleted_and_added_unrelated => "93e148086273aeb2a0c3b4285094629dd13564bc",
    }

    /// Runs the installed git on every fixture and prints `name id`; with STW_PINNED_CHECK=1 it also fails
    /// when the in-process ID differs (the tests above carry the pinned values).
    #[test]
    #[ignore]
    fn oracle_against_git() {
        let fx = Fx::new();
        let mut differ = 0;
        for (name, build) in ALL {
            let c = build(&fx);
            let diff = std::process::Command::new("git")
                .current_dir(&fx.path)
                .args([
                    "diff-tree",
                    "-p",
                    "-M",
                    "--no-color",
                    "--no-ext-diff",
                    "-r",
                    "--root",
                ])
                .arg(c.to_string())
                .output()
                .unwrap();
            assert!(diff.status.success());
            use std::io::Write;
            let mut child = std::process::Command::new("git")
                .current_dir(&fx.path)
                .args(["patch-id", "--stable"])
                .stdin(std::process::Stdio::piped())
                .stdout(std::process::Stdio::piped())
                .spawn()
                .unwrap();
            child.stdin.take().unwrap().write_all(&diff.stdout).unwrap();
            let out = child.wait_with_output().unwrap();
            let want = String::from_utf8_lossy(&out.stdout)
                .split_whitespace()
                .next()
                .unwrap_or("")
                .to_string();
            let got = commit_patch_id(&fx.repo(), c)
                .unwrap()
                .map(|i| i.to_string())
                .unwrap_or_default();
            println!(
                "{name} {want}{}",
                if want == got {
                    ""
                } else {
                    "  (in-process differs)"
                }
            );
            if want != got {
                differ += 1;
            }
        }
        assert_eq!(differ, 0, "in-process patch IDs differ from git");
    }
}
