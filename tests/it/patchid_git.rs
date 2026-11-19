//! Patch IDs of commits with renames, copies, mode changes, empty files, type changes and binary files against
//! `git patch-id --stable` on `git diff-tree -p -M --root <commit>` (git's default for `git diff` and `git show`:
//! rename detection on, copy detection off, similarity threshold 50 %).
//!
//! Every case builds one commit on top of a parentless base commit. The expected IDs are the first field of
//! `git patch-id --stable` on that commit with git 2.47.3, produced by the ignored test
//! `oracle_against_git` of each group (it runs the installed git on the fixture repository and prints `name id`):
//! `cargo test --test it patchid_git -- --ignored --nocapture`.
use crate::common::*;
use gix::objs::tree::EntryKind;
use gix::ObjectId;
use stw::patch_id::commit_patch_id;

type Entry = (&'static str, EntryKind, Vec<u8>);
type Tree = Vec<Entry>;
type Cases = &'static [(&'static str, fn(&Fx) -> ObjectId)];

fn t(path: &'static str, content: &str) -> Entry {
    (path, EntryKind::Blob, content.as_bytes().to_vec())
}
fn x(path: &'static str, content: &str) -> Entry {
    (path, EntryKind::BlobExecutable, content.as_bytes().to_vec())
}
fn l(path: &'static str, target: &str) -> Entry {
    (path, EntryKind::Link, target.as_bytes().to_vec())
}
fn g(path: &'static str, commit_hex: &str) -> Entry {
    (path, EntryKind::Commit, commit_hex.as_bytes().to_vec())
}
fn bin(path: &'static str, seed: u32, len: u32, tail: &[u8]) -> Entry {
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
        let id = if *kind == EntryKind::Commit {
            ObjectId::from_hex(data).unwrap() // a gitlink names a commit that is not in this repository
        } else {
            repo.write_blob(data).unwrap().detach()
        };
        ed.upsert(*path, *kind, id).unwrap();
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

/// Runs the installed git on every fixture and prints `name id`; fails when the in-process ID differs.
fn oracle(all: Cases) {
    let fx = Fx::new();
    let mut differ = 0;
    for (name, build) in all {
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

macro_rules! cases {
    ($($name:ident => $want:expr,)*) => {
        mod t {
            use super::*;
            $(
                #[test]
                fn $name() {
                    let fx = Fx::new();
                    let c = super::$name(&fx);
                    assert_eq!(pid(&fx, c), $want, "{}", stringify!($name));
                }
            )*
        }
        const ALL: Cases = &[$((stringify!($name), $name)),*];
        #[test]
        #[ignore]
        fn oracle_against_git() {
            oracle(ALL);
        }
    };
}

mod pinned {
    use super::*;
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
    fn binary_rename_with_change(fx: &Fx) -> ObjectId {
        change(
            fx,
            vec![bin("old.bin", 1, 3000, b"")],
            vec![bin("new.bin", 1, 3000, b"X")],
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
    fn identical_twins_second_matches_name(fx: &Fx) -> ObjectId {
        let a = body(30, "a");
        change(
            fx,
            vec![t("x/g.txt", &a), t("y/f.txt", &a)],
            vec![t("z/f.txt", &a)],
        )
    }
    fn inexact_twins_prefer_same_name(fx: &Fx) -> ObjectId {
        let a = body(30, "a");
        change(
            fx,
            vec![t("x/g.txt", &a), t("y/f.txt", &a)],
            vec![t("z/f.txt", &edited(&a, "a line 7\n", "CHANGED\n"))],
        )
    }
    fn one_source_two_similar_additions(fx: &Fx) -> ObjectId {
        let a = body(30, "a");
        let few = edited(&a, "a line 7\n", "CHANGED\n");
        let more = a.replacen("a line", "b line", 8);
        change(
            fx,
            vec![t("old.txt", &a)],
            vec![t("new1.txt", &few), t("new2.txt", &more)],
        )
    }
    fn rename_of_long_lines(fx: &Fx) -> ObjectId {
        let long = format!("{}A\n", "x".repeat(199));
        let changed = format!("{}B\n", "x".repeat(199));
        change(fx, vec![t("old.txt", &long)], vec![t("new.txt", &changed)])
    }
    fn over_the_rename_limit(fx: &Fx) -> ObjectId {
        // 1001 renames with a change and with other base names (no base-name pass): no exact match, and
        // 1001 * 1001 is above the limit of 1000 * 1000
        let name = |dir: &str, k: usize| -> &'static str {
            Box::leak(
                format!("{dir}/{}{k}.txt", if dir == "d" { "f" } else { "g" }).into_boxed_str(),
            )
        };
        let text = |k: usize, changed: bool| -> String {
            let mut s: String = (0..20).map(|l| format!("file {k} line {l}\n")).collect();
            if changed {
                s = s.replacen("line 3\n", "CHANGED\n", 1);
            }
            s
        };
        let before: Tree = (0..1001)
            .map(|k| t(name("d", k), &text(k, false)))
            .collect();
        let after: Tree = (0..1001).map(|k| t(name("e", k), &text(k, true))).collect();
        change(fx, before, after)
    }
    fn basename_pair_wins_over_better_match(fx: &Fx) -> ObjectId {
        let a = body(30, "a");
        let b = a.replacen("a line", "b line", 2);
        let dst = b.replacen("a line 20\n", "CHANGED\n", 1);
        change(
            fx,
            vec![t("x/m.txt", &a), t("y/other.txt", &b)],
            vec![t("z/m.txt", &dst)],
        )
    }
    fn over_the_rename_limit_with_same_base_names(fx: &Fx) -> ObjectId {
        let name = |dir: &str, k: usize| -> &'static str {
            Box::leak(format!("{dir}/f{k}.txt").into_boxed_str())
        };
        let text = |k: usize, changed: bool| -> String {
            let s: String = (0..20).map(|l| format!("file {k} line {l}\n")).collect();
            if changed {
                s.replacen("line 3\n", "CHANGED\n", 1)
            } else {
                s
            }
        };
        let before: Tree = (0..1001)
            .map(|k| t(name("d", k), &text(k, false)))
            .collect();
        let after: Tree = (0..1001).map(|k| t(name("e", k), &text(k, true))).collect();
        change(fx, before, after)
    }
    fn basename_60_loses_to_other_95(fx: &Fx) -> ObjectId {
        let x = format!("{}{}", body(12, "A"), body(8, "C"));
        let y = format!("{}{}", body(12, "A"), body(8, "B"));
        let dst = format!("{}{}", body(12, "A"), body(8, "B").replace("B line 8", "Q"));
        change(
            fx,
            vec![t("d1/x.txt", &x), t("y.txt", &y)],
            vec![t("d2/x.txt", &dst)],
        )
    }
    fn basename_80_wins_over_other_95(fx: &Fx) -> ObjectId {
        let x = format!("{}{}", body(16, "A"), body(4, "C"));
        let y = format!("{}{}", body(16, "A"), body(4, "B"));
        let dst = format!("{}{}", body(16, "A"), body(4, "B").replace("B line 4", "Q"));
        change(
            fx,
            vec![t("d1/x.txt", &x), t("y.txt", &y)],
            vec![t("d2/x.txt", &dst)],
        )
    }
    fn basename_60_mixed(fx: &Fx) -> ObjectId {
        let x = format!("{}{}", body(12, "A"), body(8, "C"));
        let y = format!("{}{}", body(12, "A"), body(8, "B"));
        let dst = format!("{}{}", body(12, "A"), body(8, "B").replace("B line 8", "Q"));
        change(
            fx,
            vec![
                t("d1/x.txt", &x),
                t("y.txt", &y),
                t("z.txt", &body(20, "Z")),
            ],
            vec![t("d2/x.txt", &dst), t("d2/z2.txt", &body(20, "Z"))],
        )
    }
    fn basename_not_unique_among_sources(fx: &Fx) -> ObjectId {
        let p = format!("{}{}", body(16, "A"), body(4, "C"));
        let q = format!("{}{}", body(16, "A"), body(4, "B"));
        let dst = format!("{}{}", body(16, "A"), body(4, "B").replace("B line 4", "Q"));
        change(
            fx,
            vec![t("p/x.txt", &p), t("q/x.txt", &q)],
            vec![t("r/x.txt", &dst)],
        )
    }
    fn symlink_deleted_regular_added_same_blob(fx: &Fx) -> ObjectId {
        change(fx, vec![l("old", "target")], vec![t("new", "target")])
    }
    fn regular_deleted_symlink_added_same_blob(fx: &Fx) -> ObjectId {
        change(fx, vec![t("old", "target")], vec![l("new", "target")])
    }
    fn gitlink_moved(fx: &Fx) -> ObjectId {
        change(
            fx,
            vec![
                t("k", "k\n"),
                g("sub", "1111111111111111111111111111111111111111"),
            ],
            vec![
                t("k", "k\n"),
                g("sub2", "1111111111111111111111111111111111111111"),
            ],
        )
    }
    fn executable_bit_and_rename(fx: &Fx) -> ObjectId {
        change(fx, vec![x("old", "run\n")], vec![t("new", "run\n")])
    }
    fn slot_order_tie(fx: &Fx) -> ObjectId {
        let head = |k: usize| -> String {
            body(20, "S")
                .lines()
                .take(k)
                .map(|l| format!("{l}\n"))
                .collect()
        };
        let weak = |i: usize, k: usize| format!("{}{}", head(k), body(20 - k, &format!("J{i}")));
        let dst = format!("{}{}", head(15), body(5, "N"));
        change(
            fx,
            vec![
                t("src/a1", &weak(1, 8)),
                t("src/a2", &weak(2, 6)),
                t("src/a3", &weak(3, 4)),
                t("src/a4", &weak(4, 2)),
                t("src/s1", &body(20, "S")),
                t("src/s2", &body(20, "S")),
            ],
            vec![t("dst/d1", &dst), t("dst/d2", &dst)],
        )
    }
    fn identical_sources_two_identical_destinations(fx: &Fx) -> ObjectId {
        let src = body(20, "s");
        let dst: String = format!(
            "{}n1\nn2\nn3\nn4\n",
            src.lines()
                .take(16)
                .map(|l| format!("{l}\n"))
                .collect::<String>()
        );
        change(
            fx,
            vec![t("a/x.bash", &src), t("b/x.bash", &src)],
            vec![t("a/y", &dst), t("b/y", &dst)],
        )
    }
    fn identical_sources_one_destination(fx: &Fx) -> ObjectId {
        let src = body(20, "s");
        let dst: String = format!(
            "{}n1\nn2\nn3\nn4\n",
            src.lines()
                .take(16)
                .map(|l| format!("{l}\n"))
                .collect::<String>()
        );
        change(
            fx,
            vec![t("a/x.bash", &src), t("b/x.bash", &src)],
            vec![t("a/y", &dst)],
        )
    }
    fn at_the_rename_limit(fx: &Fx) -> ObjectId {
        let name = |dir: &str, k: usize| -> &'static str {
            Box::leak(format!("{dir}/{}{k}", if dir == "d" { "a" } else { "b" }).into_boxed_str())
        };
        let text = |k: usize, changed: bool| -> String {
            let s = format!("file {k}\nline two {k}\nthree\nfour\nfive\nsix\n");
            if changed {
                format!("{s}extra\n")
            } else {
                s
            }
        };
        let before: Tree = (1..=1000)
            .map(|k| t(name("d", k), &text(k, false)))
            .collect();
        let after: Tree = (1..=1000)
            .map(|k| t(name("e", k), &text(k, true)))
            .collect();
        change(fx, before, after)
    }
    fn five_identical_sources_five_destinations(fx: &Fx) -> ObjectId {
        // four slots per destination: the fifth destination finds only taken sources
        let src = body(20, "S");
        let dst = format!(
            "{}{}",
            src.lines()
                .take(15)
                .map(|l| format!("{l}\n"))
                .collect::<String>(),
            body(5, "N")
        );
        let names = ["a1", "a2", "a3", "a4", "a5"];
        let dnames = ["d1", "d2", "d3", "d4", "d5"];
        let before: Tree = names
            .iter()
            .map(|n| t(Box::leak(format!("src/{n}").into_boxed_str()), &src))
            .collect();
        let after: Tree = dnames
            .iter()
            .map(|n| t(Box::leak(format!("dst/{n}").into_boxed_str()), &dst))
            .collect();
        change(fx, before, after)
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
            binary_rename_with_change => "c49b936ee514e18b48d50bf17dda2c6c043cb1c7",
            empty_file_rename => "8655929933e516a7331860722ad5ea60579025dd",
            multi_commit_first => "9794667d2b40f57d292670f9a77d89f357d90475",
            multi_commit_second => "5a96ed9fe4629de3f3f05f15ee1651edcb0fac30",
            names_with_blank_and_utf8 => "4449962a572ccd90b7d2ad9a652ea7c9b7c410ab",
            identical_twins => "b9569c6eaf4c93a870e36e28abf7837b59e3d07c",
            identical_twins_second_matches_name => "2d69bc2b85fcdbd83c72b1bad75a7da4dce61071",
            inexact_twins_prefer_same_name => "6662417c0745888c6defd23c31f1bf4efd4449a3",
            one_source_two_similar_additions => "4f98db5f786258764d252ad75ae2c5b907c76299",
            rename_of_long_lines => "d66faf3b2224f2d97c3bc11fac4703f9cabda7d2",
            over_the_rename_limit => "2cb38b08225341f12cc97e09f3e7ddefe85ac168",
            basename_pair_wins_over_better_match => "1d23266830b3621936a2e49822fc548d3882b398",
            over_the_rename_limit_with_same_base_names => "356080e043890aff1c73e55eae758945580f1cf9",
            basename_60_loses_to_other_95 => "f8766b844b4008a6137486f370f17932f5e2e454",
            basename_80_wins_over_other_95 => "6b8cba1ca015bc32680b2fcfc9e73e77c52e248a",
            basename_60_mixed => "f12ca8d29dbe48b2cd26ac33c37fcf1e32b24708",
            basename_not_unique_among_sources => "e334630473e93b1e34194019a85a21452929d32f",
            symlink_deleted_regular_added_same_blob => "8d224db1dc3d8a3f0af1494e7a9d355b5481d70c",
            regular_deleted_symlink_added_same_blob => "12cc1947583dca379bff45a3449a96d33844eca9",
            gitlink_moved => "dd2b61f61d288d4fad21b309729242d23ce99abd",
            executable_bit_and_rename => "1c8bb549e5e25c4a002316fe689f85531c3b02e9",
            slot_order_tie => "869d4c054d80d1f153868060f838e655ed69b446",
            identical_sources_two_identical_destinations => "e258d0c1e6a8e096b996b9e248aeee0755f0e002",
            identical_sources_one_destination => "afd7bad30add69e58e522e830c4909213f37b573",
            at_the_rename_limit => "c0a9756bc84969c411135d8bc5881ebafd586454",
            five_identical_sources_five_destinations => "036cbbdef7e4ea0ec2d1b8f962bf220d9059b566",
            symlink_rename => "8e78bdefc8f0bc26c65eb193a6d5d3cac408bab2",
            contents_swapped => "302ba4b49782057e95797e0093b1c6a3ef85897a",
            rename_and_similar_new_file => "e248fd04bca57a4b299358385809c420d8d8238e",
            rename_with_crlf => "a6855c4eb45e7fa30cffe098b82dcbcb573a5bde",
            deleted_and_added_unrelated => "93e148086273aeb2a0c3b4285094629dd13564bc",
    }
}

/// How git cuts the diff into units for `--stable` and what it hashes of a binary file: a file diff without hunks
/// (mode change, empty file, exact rename) is hashed together with the next file's diff, a binary file adds the
/// abbreviated blob ids of its `index` line and ends its unit, the `diff --git` line after a binary file is not
/// hashed, and the last unit is added even when it is empty.
mod units {
    use super::*;

    fn mode_change_then_text_change(fx: &Fx) -> ObjectId {
        change(
            fx,
            vec![t("a.sh", "echo\n"), t("b.txt", &body(10, "b"))],
            vec![
                x("a.sh", "echo\n"),
                t("b.txt", &edited(&body(10, "b"), "b line 5\n", "B5\n")),
            ],
        )
    }
    fn mode_change_alone(fx: &Fx) -> ObjectId {
        change(fx, vec![t("a.sh", "echo\n")], vec![x("a.sh", "echo\n")])
    }
    fn empty_file_added_then_text_change(fx: &Fx) -> ObjectId {
        change(
            fx,
            vec![t("z.txt", &body(10, "z"))],
            vec![
                t("e", ""),
                t("z.txt", &edited(&body(10, "z"), "z line 5\n", "Z5\n")),
            ],
        )
    }
    fn empty_file_added_alone(fx: &Fx) -> ObjectId {
        change(fx, vec![t("k", "k\n")], vec![t("k", "k\n"), t("e", "")])
    }
    fn empty_file_deleted_alone(fx: &Fx) -> ObjectId {
        change(fx, vec![t("k", "k\n"), t("e", "")], vec![t("k", "k\n")])
    }
    fn binary_modified(fx: &Fx) -> ObjectId {
        change(
            fx,
            vec![bin("x.bin", 1, 3000, b"")],
            vec![bin("x.bin", 2, 3000, b"")],
        )
    }
    fn binary_added(fx: &Fx) -> ObjectId {
        change(
            fx,
            vec![t("k", "k\n")],
            vec![t("k", "k\n"), bin("x.bin", 1, 3000, b"")],
        )
    }
    fn binary_deleted(fx: &Fx) -> ObjectId {
        change(
            fx,
            vec![t("k", "k\n"), bin("x.bin", 1, 3000, b"")],
            vec![t("k", "k\n")],
        )
    }
    fn binary_then_text(fx: &Fx) -> ObjectId {
        change(
            fx,
            vec![bin("a.bin", 1, 3000, b""), t("b.txt", &body(10, "b"))],
            vec![
                bin("a.bin", 2, 3000, b""),
                t("b.txt", &edited(&body(10, "b"), "b line 5\n", "B5\n")),
            ],
        )
    }
    fn text_then_binary(fx: &Fx) -> ObjectId {
        change(
            fx,
            vec![t("a.txt", &body(10, "a")), bin("b.bin", 1, 3000, b"")],
            vec![
                t("a.txt", &edited(&body(10, "a"), "a line 5\n", "A5\n")),
                bin("b.bin", 2, 3000, b""),
            ],
        )
    }
    fn two_binaries(fx: &Fx) -> ObjectId {
        change(
            fx,
            vec![bin("a.bin", 1, 3000, b""), bin("b.bin", 3, 3000, b"")],
            vec![bin("a.bin", 2, 3000, b""), bin("b.bin", 4, 3000, b"")],
        )
    }
    fn binary_then_added_empty_file(fx: &Fx) -> ObjectId {
        change(
            fx,
            vec![bin("a.bin", 1, 3000, b"")],
            vec![bin("a.bin", 2, 3000, b""), t("b", "")],
        )
    }
    fn file_replaced_by_symlink(fx: &Fx) -> ObjectId {
        change(fx, vec![t("f", "x\n")], vec![l("f", "target")])
    }
    fn symlink_replaced_by_file(fx: &Fx) -> ObjectId {
        change(fx, vec![l("f", "target")], vec![t("f", "x\n")])
    }
    fn mode_and_content_change(fx: &Fx) -> ObjectId {
        change(
            fx,
            vec![t("f", &body(10, "f"))],
            vec![x("f", &edited(&body(10, "f"), "f line 5\n", "F5\n"))],
        )
    }
    fn gitlink_raised(fx: &Fx) -> ObjectId {
        change(
            fx,
            vec![g("sub", "1111111111111111111111111111111111111111")],
            vec![g("sub", "2222222222222222222222222222222222222222")],
        )
    }
    fn gitlink_added_with_text_file(fx: &Fx) -> ObjectId {
        change(
            fx,
            vec![t("k", "k\n")],
            vec![
                t("k", "k\n"),
                g("sub", "2222222222222222222222222222222222222222"),
            ],
        )
    }
    fn text_added_without_final_newline(fx: &Fx) -> ObjectId {
        change(
            fx,
            vec![t("k", "k\n")],
            vec![t("k", "k\n"), t("n", "no newline")],
        )
    }

    cases! {
        mode_change_then_text_change => "84e1b5291531c9dda192fdbbb6659faf7e669644",
        mode_change_alone => "ac5adb6f4ebcf1cae3a406d6a420d4adeb514b9f",
        empty_file_added_then_text_change => "6ed30f9aa9dcd0b1cc71304de2a3435a9cfc7304",
        empty_file_added_alone => "f6f78ec27c86abf3aed35b0fab0bdc37e931cf8b",
        empty_file_deleted_alone => "1c55648faf3d815e3fa3ad520d69264e6787fa58",
        binary_modified => "1b1dfb54ef1f6a1cef18737a4bf429693cdcd41c",
        binary_added => "b3d0b87afd68006d337123bada7ccc224e01a38a",
        binary_deleted => "ad873949907d15ec6bfe70f422a69558df0442e8",
        binary_then_text => "f1ced6704a1d972ae7fd5ded8290b0364ab39d7e",
        text_then_binary => "68129ea7b3b3a4abc0db0746a3c671d4f699c820",
        two_binaries => "0ab984fca9deced60a0d0c4236be634d9b99efa0",
        binary_then_added_empty_file => "8ac2343c22323ccfc7bbfea149ac3d3a8f7a92be",
        file_replaced_by_symlink => "7c7d7a26757debf7c6ce992914c290b64f98e293",
        symlink_replaced_by_file => "5b655c8ba543e9323c556eb8d700731c843cf575",
        mode_and_content_change => "877d6ff16dc3483f23e6bb4c21c4b498f44ee546",
        gitlink_raised => "e65c95dd241b938be679f89f6bd0e07966f907e4",
        gitlink_added_with_text_file => "51ac0b2bfabac9599bc2c67f8a778f844b8c0c40",
        text_added_without_final_newline => "420e3325349ac09ec8563a594226a8e48e1aa251",
    }
}
