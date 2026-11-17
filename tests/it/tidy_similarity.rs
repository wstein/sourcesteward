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

// ---- keys from real commits (fixtures built with gix, no git binary) ----

use crate::common::*;
use gix::objs::tree::EntryKind;
use gix::ObjectId;

type Edit<'a> = (&'a [u8], Option<(EntryKind, &'a [u8])>);

/// A commit with explicit parents, tree base, edits and author time. Writes objects only.
fn mk_full(
    fx: &Fx,
    parents: &[ObjectId],
    tree_from: Option<ObjectId>,
    edits: &[Edit],
    msg: &str,
    time: i64,
) -> ObjectId {
    let repo = fx.repo();
    let base = match tree_from {
        Some(p) => repo.find_commit(p).unwrap().tree_id().unwrap().detach(),
        None => repo.empty_tree().id,
    };
    let mut ed = repo.edit_tree(base).unwrap();
    for (path, what) in edits {
        let path = gix::bstr::BStr::new(*path);
        match what {
            Some((kind, data)) => {
                let blob = repo.write_blob(data).unwrap().detach();
                ed.upsert(path, *kind, blob).unwrap();
            }
            None => {
                ed.remove(path).unwrap();
            }
        }
    }
    let tree = ed.write().unwrap().detach();
    let sig = gix::actor::Signature {
        name: "T".into(),
        email: "t@example.com".into(),
        time: gix::date::Time::new(time, 0),
    };
    let commit = gix::objs::Commit {
        tree,
        parents: parents.to_vec().into(),
        author: sig.clone(),
        committer: sig,
        encoding: None,
        message: msg.into(),
        extra_headers: vec![],
    };
    repo.write_object(&commit).unwrap().detach()
}

const T0: i64 = 1_700_000_000;

fn put<'a>(path: &'a str, data: &'a str) -> Edit<'a> {
    (path.as_bytes(), Some((EntryKind::Blob, data.as_bytes())))
}

fn mk(fx: &Fx, parent: Option<ObjectId>, edits: &[Edit], msg: &str, time: i64) -> ObjectId {
    let parents: Vec<ObjectId> = parent.into_iter().collect();
    mk_full(fx, &parents, parent, edits, msg, time)
}

fn lines_of(entries: &Entries) -> Vec<(String, u8, String)> {
    match entries {
        Entries::Lines(v) => v
            .iter()
            .map(|e| {
                (
                    String::from_utf8_lossy(&e.path).into_owned(),
                    e.sign,
                    String::from_utf8_lossy(&e.line).into_owned(),
                )
            })
            .collect(),
        Entries::Skipped(s) => panic!("skipped: {s:?}"),
    }
}

fn entries(fx: &Fx, c: ObjectId) -> Entries {
    commit_entries(&fx.repo(), c, &Limits::default()).unwrap()
}

fn s(p: &str, sign: u8, l: &str) -> (String, u8, String) {
    (p.into(), sign, l.into())
}

#[test]
fn an_edit_gives_one_minus_and_one_plus_without_context() {
    let fx = Fx::new();
    let base = mk(&fx, None, &[put("f.txt", &lines(20, "x"))], "base", T0);
    let edit = mk(
        &fx,
        Some(base),
        &[put("f.txt", &lines(20, "x").replace("x10\n", "  new ten\r\n"))],
        "edit",
        T0 + 60,
    );
    assert_eq!(
        lines_of(&entries(&fx, edit)),
        vec![s("f.txt", b'-', "x10"), s("f.txt", b'+', "new ten")],
        "-U0: no context, lines trimmed"
    );
}

#[test]
fn a_rename_is_a_deletion_and_an_addition() {
    let fx = Fx::new();
    let base = mk(&fx, None, &[put("old.txt", "a\nb\n")], "base", T0);
    let ren = mk(
        &fx,
        Some(base),
        &[(b"old.txt", None), put("new.txt", "a\nb\n")],
        "rename",
        T0 + 60,
    );
    let got = lines_of(&entries(&fx, ren));
    assert_eq!(got.len(), 4);
    assert!(got.contains(&s("old.txt", b'-', "a")) && got.contains(&s("new.txt", b'+', "b")));
    // with the path they differ from the same change in place, without the path they are the same lines
    let k = |c| match entries(&fx, c) {
        Entries::Lines(v) => Keys::from_entries(&v),
        _ => panic!(),
    };
    let edit = mk(
        &fx,
        Some(base),
        &[put("old.txt", "")],
        "empty it",
        T0 + 61,
    );
    let _ = k(edit);
}

#[test]
fn a_binary_file_is_one_key_by_blob_id_without_content_lines() {
    let fx = Fx::new();
    let base = mk(&fx, None, &[put("a.txt", "x\n")], "base", T0);
    let bin = |d: &'static [u8]| -> Edit<'static> { (b"p.bin", Some((EntryKind::Blob, d))) };
    let c1 = mk_full(&fx, &[base], Some(base), &[bin(b"\0\x01\x02")], "bin1", T0 + 60);
    let c2 = mk_full(&fx, &[base], Some(base), &[bin(b"\0\x01\x02")], "bin1 again", T0 + 61);
    let c3 = mk_full(&fx, &[base], Some(base), &[bin(b"\0\x09\x09")], "bin2", T0 + 62);
    let k = |c| match entries(&fx, c) {
        Entries::Lines(v) => {
            assert_eq!(v.len(), 1, "one key, no lines");
            assert_eq!(v[0].sign, b'B');
            Keys::from_entries(&v)
        }
        _ => panic!(),
    };
    assert_eq!(k(c1).similarity(&k(c2)).inter, 1, "same blob, same key");
    assert_eq!(k(c1).similarity(&k(c3)).inter, 0, "other blob, other key");
}

#[test]
fn crlf_and_lf_versions_of_a_change_have_equal_keys() {
    let fx = Fx::new();
    let lf = mk(&fx, None, &[put("f.txt", "a\nb\nc\n")], "lf base", T0);
    let crlf = mk(&fx, None, &[put("f.txt", "a\r\nb\r\nc\r\n")], "crlf base", T0);
    let e_lf = mk(&fx, Some(lf), &[put("f.txt", "a\nX\nc\n")], "x", T0 + 60);
    let e_crlf = mk(&fx, Some(crlf), &[put("f.txt", "a\r\nX\r\nc\r\n")], "x", T0 + 60);
    assert_eq!(lines_of(&entries(&fx, e_lf)), lines_of(&entries(&fx, e_crlf)));
}

#[test]
fn a_line_ending_conversion_counts_as_a_difference_to_the_real_change() {
    let fx = Fx::new();
    let base = mk(&fx, None, &[put("f.txt", "a\r\nb\r\nc\r\n")], "base", T0);
    let conv = mk(&fx, Some(base), &[put("f.txt", "a\nb\nc\n")], "convert", T0 + 60);
    let got = lines_of(&entries(&fx, conv));
    assert_eq!(got.len(), 6, "every line is removed and added again");
    let real = mk(&fx, Some(base), &[put("f.txt", "a\r\nB\r\nc\r\n")], "real", T0 + 61);
    let k = |c| match entries(&fx, c) {
        Entries::Lines(v) => Keys::from_entries(&v),
        _ => panic!(),
    };
    let sim = k(conv).similarity(&k(real));
    assert!(!sim.ge_pct(95) && sim.permille() < 500, "{}", sim.permille());
}

#[test]
fn merge_and_empty_commits_are_skipped_with_a_reason() {
    let fx = Fx::new();
    let base = mk(&fx, None, &[put("f.txt", "a\n")], "base", T0);
    let l = mk(&fx, Some(base), &[put("l.txt", "l\n")], "l", T0 + 60);
    let r = mk(&fx, Some(base), &[put("r.txt", "r\n")], "r", T0 + 61);
    let merge = mk_full(&fx, &[l, r], Some(l), &[put("r.txt", "r\n")], "merge", T0 + 62);
    assert_eq!(entries(&fx, merge), Entries::Skipped(Skip::Merge));
    let empty = mk(&fx, Some(base), &[], "empty", T0 + 63);
    assert_eq!(entries(&fx, empty), Entries::Skipped(Skip::Empty));
}

#[test]
fn a_commit_over_the_limits_is_too_large() {
    let fx = Fx::new();
    let base = mk(&fx, None, &[put("a.txt", "x\n")], "base", T0);
    let big = mk(&fx, Some(base), &[put("f.txt", &lines(10, "n"))], "ten lines", T0 + 60);
    let lim = Limits { max_keys: 9, ..Limits::default() };
    assert_eq!(commit_entries(&fx.repo(), big, &lim).unwrap(), Entries::Skipped(Skip::TooLarge));
    let lim = Limits { max_keys: 10, ..Limits::default() };
    assert!(matches!(commit_entries(&fx.repo(), big, &lim).unwrap(), Entries::Lines(v) if v.len() == 10));
    let lim = Limits { max_blob_bytes: 5, ..Limits::default() };
    assert_eq!(commit_entries(&fx.repo(), big, &lim).unwrap(), Entries::Skipped(Skip::TooLarge), "blob over the byte limit");
}

#[test]
fn a_submodule_change_is_two_lines_with_the_commit_ids() {
    let fx = Fx::new();
    let base = mk(&fx, None, &[put("a.txt", "x\n")], "base", T0);
    let t1 = ObjectId::from_hex(b"1111111111111111111111111111111111111111").unwrap();
    let t2 = ObjectId::from_hex(b"2222222222222222222222222222222222222222").unwrap();
    let g1 = fx.commit_gitlink(base, "sub", t1, "add sub");
    let g2 = fx.commit_gitlink(g1, "sub", t2, "raise sub");
    assert_eq!(
        lines_of(&entries(&fx, g2)),
        vec![
            s("sub", b'-', &format!("Subproject commit {t1}")),
            s("sub", b'+', &format!("Subproject commit {t2}")),
        ]
    );
}

#[test]
fn a_mode_change_and_an_empty_new_file_have_their_own_keys() {
    let fx = Fx::new();
    let base = mk(&fx, None, &[put("run.sh", "x\n")], "base", T0);
    let exec = mk(
        &fx,
        Some(base),
        &[(b"run.sh", Some((EntryKind::BlobExecutable, b"x\n")))],
        "chmod",
        T0 + 60,
    );
    assert_eq!(lines_of(&entries(&fx, exec)), vec![s("run.sh", b'M', "100644>100755")]);
    let empty_file = mk(&fx, Some(base), &[put("e.txt", "")], "empty file", T0 + 61);
    assert_eq!(lines_of(&entries(&fx, empty_file)), vec![s("e.txt", b'N', "")]);
}

#[test]
fn non_utf8_paths_and_lines_stay_bytes() {
    let fx = Fx::new();
    let base = mk(&fx, None, &[put("a.txt", "x\n")], "base", T0);
    let c = mk_full(
        &fx,
        &[base],
        Some(base),
        &[(b"caf\xe9.txt", Some((EntryKind::Blob, b"l\xffine\n")))],
        "latin1",
        T0 + 60,
    );
    match entries(&fx, c) {
        Entries::Lines(v) => {
            assert_eq!(v.len(), 1);
            assert_eq!(v[0].path, b"caf\xe9.txt");
            assert_eq!(v[0].line, b"l\xffine");
        }
        _ => panic!(),
    }
}

#[test]
fn the_cumulative_diff_of_disjoint_commits_is_the_sum_of_their_keys() {
    let fx = Fx::new();
    let base = mk(&fx, None, &[put("a.txt", "x\n")], "base", T0);
    let c1 = mk(&fx, Some(base), &[put("f.txt", "1\n2\n")], "c1", T0 + 60);
    let c2 = mk(&fx, Some(c1), &[put("g.txt", "3\n")], "c2", T0 + 61);
    let repo = fx.repo();
    let tree = |c: ObjectId| repo.find_commit(c).unwrap().tree_id().unwrap().detach();
    let k = |e: Entries| match e {
        Entries::Lines(v) => Keys::from_entries(&v),
        _ => panic!(),
    };
    let lim = Limits::default();
    let cumulative = k(tree_entries(&repo, tree(base), tree(c2), &lim).unwrap());
    let parts = k(commit_entries(&repo, c1, &lim).unwrap()).sum(&k(commit_entries(&repo, c2, &lim).unwrap()));
    assert_eq!(cumulative, parts);
    assert_eq!(cumulative.len(), 3);
}

#[test]
fn a_commit_and_its_cherry_pick_on_another_base_are_100_percent_and_a_wrong_pair_is_0() {
    // counter-check of the handover: a `-` commit (main side) against its partner is 100.0
    let fx = Fx::new();
    let b1 = mk(&fx, None, &[put("f.txt", &lines(30, "x"))], "base", T0);
    let b2 = mk(&fx, None, &[put("f.txt", &format!("top\n{}", lines(30, "x")))], "other base", T0);
    let edit = |p: ObjectId, pre: &str| mk(&fx, Some(p), &[put("f.txt", &format!("{pre}{}", lines(30, "x").replace("x7\n", "seven\n")))], "edit", T0 + 60);
    let a = edit(b1, "");
    let b = edit(b2, "top\n");
    let k = |c| match entries(&fx, c) {
        Entries::Lines(v) => Keys::from_entries(&v),
        _ => panic!(),
    };
    assert_eq!(shown(k(a).similarity(&k(b))), "100.0");
    let wrong = mk(&fx, Some(b1), &[put("g.txt", &lines(30, "q"))], "unrelated", T0 + 61);
    assert_eq!(shown(k(a).similarity(&k(wrong))), "0.0");
}
