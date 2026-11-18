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
        (3, 3, 2, "66.7"),
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
    assert_eq!(
        s.shown(95),
        949,
        "shown as 94.9 so that display and class agree"
    );
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
    assert_eq!(
        a.similarity(&b).inter,
        1,
        "indentation and CR do not matter"
    );
    let c = Keys::from_entries(&[e("f", b'+', "x( );")]);
    assert_eq!(a.similarity(&c).inter, 0, "inner white space does");
}

#[test]
fn the_comparison_of_values_is_exact() {
    let lo = Sim {
        inter: 1,
        a: 3,
        b: 3,
    };
    let hi = Sim {
        inter: 2,
        a: 5,
        b: 5,
    };
    assert_eq!(lo.cmp_value(&hi), std::cmp::Ordering::Less);
    assert_eq!(hi.cmp_value(&lo), std::cmp::Ordering::Greater);
    let same = Sim {
        inter: 2,
        a: 6,
        b: 6,
    };
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
    assert_eq!(
        s.containment_in_b(),
        (2, 3),
        "two thirds of the squash are c1"
    );
    assert_eq!(
        c1.sum(&c2),
        c2.sum(&c1),
        "the sum does not depend on the order"
    );
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
        &[put(
            "f.txt",
            &lines(20, "x").replace("x10\n", "  new ten\r\n"),
        )],
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
}

#[test]
fn a_binary_file_is_one_key_by_blob_id_without_content_lines() {
    let fx = Fx::new();
    let base = mk(&fx, None, &[put("a.txt", "x\n")], "base", T0);
    let bin = |d: &'static [u8]| -> Edit<'static> { (b"p.bin", Some((EntryKind::Blob, d))) };
    let c1 = mk_full(
        &fx,
        &[base],
        Some(base),
        &[bin(b"\0\x01\x02")],
        "bin1",
        T0 + 60,
    );
    let c2 = mk_full(
        &fx,
        &[base],
        Some(base),
        &[bin(b"\0\x01\x02")],
        "bin1 again",
        T0 + 61,
    );
    let c3 = mk_full(
        &fx,
        &[base],
        Some(base),
        &[bin(b"\0\x09\x09")],
        "bin2",
        T0 + 62,
    );
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
    let crlf = mk(
        &fx,
        None,
        &[put("f.txt", "a\r\nb\r\nc\r\n")],
        "crlf base",
        T0,
    );
    let e_lf = mk(&fx, Some(lf), &[put("f.txt", "a\nX\nc\n")], "x", T0 + 60);
    let e_crlf = mk(
        &fx,
        Some(crlf),
        &[put("f.txt", "a\r\nX\r\nc\r\n")],
        "x",
        T0 + 60,
    );
    assert_eq!(
        lines_of(&entries(&fx, e_lf)),
        lines_of(&entries(&fx, e_crlf))
    );
}

#[test]
fn a_line_ending_conversion_counts_as_a_difference_to_the_real_change() {
    let fx = Fx::new();
    let base = mk(&fx, None, &[put("f.txt", "a\r\nb\r\nc\r\n")], "base", T0);
    let conv = mk(
        &fx,
        Some(base),
        &[put("f.txt", "a\nb\nc\n")],
        "convert",
        T0 + 60,
    );
    let got = lines_of(&entries(&fx, conv));
    assert_eq!(got.len(), 6, "every line is removed and added again");
    let real = mk(
        &fx,
        Some(base),
        &[put("f.txt", "a\r\nB\r\nc\r\n")],
        "real",
        T0 + 61,
    );
    let k = |c| match entries(&fx, c) {
        Entries::Lines(v) => Keys::from_entries(&v),
        _ => panic!(),
    };
    let sim = k(conv).similarity(&k(real));
    assert!(
        !sim.ge_pct(95) && sim.permille() < 500,
        "{}",
        sim.permille()
    );
}

#[test]
fn merge_and_empty_commits_are_skipped_with_a_reason() {
    let fx = Fx::new();
    let base = mk(&fx, None, &[put("f.txt", "a\n")], "base", T0);
    let l = mk(&fx, Some(base), &[put("l.txt", "l\n")], "l", T0 + 60);
    let r = mk(&fx, Some(base), &[put("r.txt", "r\n")], "r", T0 + 61);
    let merge = mk_full(
        &fx,
        &[l, r],
        Some(l),
        &[put("r.txt", "r\n")],
        "merge",
        T0 + 62,
    );
    assert_eq!(entries(&fx, merge), Entries::Skipped(Skip::Merge));
    let empty = mk(&fx, Some(base), &[], "empty", T0 + 63);
    assert_eq!(entries(&fx, empty), Entries::Skipped(Skip::Empty));
}

#[test]
fn a_commit_over_the_limits_is_too_large() {
    let fx = Fx::new();
    let base = mk(&fx, None, &[put("a.txt", "x\n")], "base", T0);
    let big = mk(
        &fx,
        Some(base),
        &[put("f.txt", &lines(10, "n"))],
        "ten lines",
        T0 + 60,
    );
    let lim = Limits {
        max_keys: 9,
        ..Limits::default()
    };
    assert_eq!(
        commit_entries(&fx.repo(), big, &lim).unwrap(),
        Entries::Skipped(Skip::TooLarge)
    );
    let lim = Limits {
        max_keys: 10,
        ..Limits::default()
    };
    assert!(
        matches!(commit_entries(&fx.repo(), big, &lim).unwrap(), Entries::Lines(v) if v.len() == 10)
    );
    let lim = Limits {
        max_blob_bytes: 5,
        ..Limits::default()
    };
    assert_eq!(
        commit_entries(&fx.repo(), big, &lim).unwrap(),
        Entries::Skipped(Skip::TooLarge),
        "blob over the byte limit"
    );
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
    assert_eq!(
        lines_of(&entries(&fx, exec)),
        vec![s("run.sh", b'M', "100644>100755")]
    );
    let empty_file = mk(&fx, Some(base), &[put("e.txt", "")], "empty file", T0 + 61);
    assert_eq!(
        lines_of(&entries(&fx, empty_file)),
        vec![s("e.txt", b'N', "")]
    );
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
    let parts = k(commit_entries(&repo, c1, &lim).unwrap())
        .sum(&k(commit_entries(&repo, c2, &lim).unwrap()));
    assert_eq!(cumulative, parts);
    assert_eq!(cumulative.len(), 3);
}

#[test]
fn a_commit_and_its_cherry_pick_on_another_base_are_100_percent_and_a_wrong_pair_is_0() {
    // counter-check of the handover: a `-` commit (main side) against its partner is 100.0
    let fx = Fx::new();
    let b1 = mk(&fx, None, &[put("f.txt", &lines(30, "x"))], "base", T0);
    let b2 = mk(
        &fx,
        None,
        &[put("f.txt", &format!("top\n{}", lines(30, "x")))],
        "other base",
        T0,
    );
    let edit = |p: ObjectId, pre: &str| {
        mk(
            &fx,
            Some(p),
            &[put(
                "f.txt",
                &format!("{pre}{}", lines(30, "x").replace("x7\n", "seven\n")),
            )],
            "edit",
            T0 + 60,
        )
    };
    let a = edit(b1, "");
    let b = edit(b2, "top\n");
    let k = |c| match entries(&fx, c) {
        Entries::Lines(v) => Keys::from_entries(&v),
        _ => panic!(),
    };
    assert_eq!(shown(k(a).similarity(&k(b))), "100.0");
    let wrong = mk(
        &fx,
        Some(b1),
        &[put("g.txt", &lines(30, "q"))],
        "unrelated",
        T0 + 61,
    );
    assert_eq!(shown(k(a).similarity(&k(wrong))), "0.0");
}

// ---- candidates, best candidate, classes, figures, evidence ----

const DAY: i64 = 86_400;

/// 30 lines `x<i>`; the lines `first..first+count` read `<tag><i>` instead, except those in `odd` (`odd<i>`).
fn edited(first: usize, count: usize, tag: &str, odd: &[usize]) -> String {
    (0..30)
        .map(|i| {
            if odd.contains(&i) {
                format!("odd{i}\n")
            } else if i >= first && i < first + count {
                format!("{tag}{i}\n")
            } else {
                format!("x{i}\n")
            }
        })
        .collect()
}

fn base_of(fx: &Fx) -> ObjectId {
    mk(
        fx,
        None,
        &[put("f.txt", &edited(0, 0, "", &[]))],
        "base",
        T0,
    )
}

fn eval_with(fx: &Fx, main: &[ObjectId], plus: &[ObjectId], lim: &Limits) -> BranchReport {
    let repo = fx.repo();
    let mut cache = SimCache::default();
    let idx = MainIndex::build(&repo, &mut cache, main, None).unwrap();
    evaluate_branch(&repo, &mut cache, &idx, plus, 95, 70, lim).unwrap()
}

fn eval(fx: &Fx, main: &[ObjectId], plus: &[ObjectId]) -> BranchReport {
    eval_with(fx, main, plus, &Limits::default())
}

fn cmp0(r: &BranchReport) -> &Compared {
    match &r.commits[0].outcome {
        Outcome::Compared(c) => c,
        o => panic!("not compared: {o:?}"),
    }
}

#[test]
fn a_rebase_with_a_small_conflict_resolution_has_the_same_subject_and_less_than_100() {
    let fx = Fx::new();
    let base = base_of(&fx);
    let a = mk(
        &fx,
        Some(base),
        &[put("f.txt", &edited(0, 20, "new", &[]))],
        "feat: x",
        T0 + 100,
    );
    let m = mk(
        &fx,
        Some(base),
        &[put("f.txt", &edited(0, 20, "new", &[5]))],
        "feat: x",
        T0 + 200,
    );
    let r = eval(&fx, &[m], &[a]);
    let c = cmp0(&r);
    assert_eq!(c.candidate, m);
    assert_eq!(c.reason, Reason::Subject);
    assert_eq!((c.sim.inter, c.sim.a, c.sim.b), (39, 40, 40));
    assert_eq!(fmt_permille(c.sim.permille()), "97.5");
    assert_eq!(r.class, Class::Landed);
    assert!(
        c.evidence.is_empty(),
        "at or above the high threshold: no evidence"
    );
}

#[test]
fn a_modified_commit_is_between_the_thresholds_and_a_different_one_is_partial() {
    let fx = Fx::new();
    let base = base_of(&fx);
    let a = mk(
        &fx,
        Some(base),
        &[put("f.txt", &edited(0, 20, "new", &[]))],
        "feat: x",
        T0 + 100,
    );
    // 4 of the 20 added lines differ: 36 of 40 keys shared = 90.0
    let m4 = mk(
        &fx,
        Some(base),
        &[put("f.txt", &edited(0, 20, "new", &[1, 3, 5, 7]))],
        "feat: x",
        T0 + 200,
    );
    let r = eval(&fx, &[m4], &[a]);
    assert_eq!(fmt_permille(cmp0(&r).sim.permille()), "90.0");
    assert_eq!(r.class, Class::Modified);
    // 13 of 20 differ: 27 of 40 = 67.5, below 70
    let odd13: Vec<usize> = (0..13).collect();
    let m13 = mk(
        &fx,
        Some(base),
        &[put("f.txt", &edited(0, 20, "new", &odd13))],
        "feat: x",
        T0 + 201,
    );
    let r = eval(&fx, &[m13], &[a]);
    assert_eq!(fmt_permille(cmp0(&r).sim.permille()), "67.5");
    assert_eq!(r.class, Class::Partial);
    // same subject, other content
    let other = mk(
        &fx,
        Some(base),
        &[put("f.txt", &edited(10, 20, "zzz", &[]))],
        "feat: x",
        T0 + 202,
    );
    let r = eval(&fx, &[other], &[a]);
    assert_eq!(r.class, Class::Partial);
    assert!(cmp0(&r).sim.permille() < 700);
}

#[test]
fn indentation_does_not_matter_but_other_reformatting_does() {
    let fx = Fx::new();
    let base = mk(&fx, None, &[put("f.txt", "a\n")], "base", T0);
    let body = |ind: &str, inner: &str| -> String {
        (0..10)
            .map(|i| format!("{ind}call{inner}({i});\n"))
            .collect()
    };
    let a = mk(
        &fx,
        Some(base),
        &[put("f.txt", &body("    ", ""))],
        "feat: calls",
        T0 + 100,
    );
    let tabs = mk(
        &fx,
        Some(base),
        &[put("f.txt", &body("\t", ""))],
        "feat: calls",
        T0 + 200,
    );
    let spaced = mk(
        &fx,
        Some(base),
        &[put("f.txt", &body("", " "))],
        "feat: calls",
        T0 + 201,
    );
    assert_eq!(
        fmt_permille(cmp0(&eval(&fx, &[tabs], &[a])).sim.permille()),
        "100.0"
    );
    let r = eval(&fx, &[spaced], &[a]);
    assert!(
        cmp0(&r).sim.permille() < 700,
        "inner white space changes the line"
    );
}

#[test]
fn a_rename_shows_only_in_the_value_without_the_path() {
    let fx = Fx::new();
    let base = mk(&fx, None, &[put("a.txt", "a\n")], "base", T0);
    let text = lines(10, "t");
    let a = mk(
        &fx,
        Some(base),
        &[put("old/mod.rs", &text)],
        "feat: module",
        T0 + 100,
    );
    let m = mk(
        &fx,
        Some(base),
        &[put("new/mod.rs", &text)],
        "feat: module",
        T0 + 200,
    );
    let r = eval(&fx, &[m], &[a]);
    let c = cmp0(&r);
    assert_eq!(fmt_permille(c.sim.permille()), "0.0");
    assert_eq!(fmt_permille(c.no_path.permille()), "100.0");
    assert_eq!(
        r.class,
        Class::Partial,
        "the value without the path is no part of the class"
    );
}

#[test]
fn a_squash_is_not_recognised() {
    let fx = Fx::new();
    let base = mk(&fx, None, &[put("a.txt", "a\n")], "base", T0);
    let c1 = mk(
        &fx,
        Some(base),
        &[put("f.txt", &lines(10, "p"))],
        "feat: one",
        T0 + 100,
    );
    let c2 = mk(
        &fx,
        Some(c1),
        &[put("g.txt", &lines(10, "q"))],
        "feat: two",
        T0 + 101,
    );
    let squash = mk(
        &fx,
        Some(base),
        &[put("f.txt", &lines(10, "p")), put("g.txt", &lines(10, "q"))],
        "feat: everything",
        T0 + 200,
    );
    let r = eval(&fx, &[squash], &[c1, c2]);
    assert_eq!(r.class, Class::Partial);
    assert_eq!(r.plus_commits, 2);
    for c in &r.commits {
        let Outcome::Compared(x) = &c.outcome else {
            panic!("found via the file overlap")
        };
        assert_eq!(x.reason, Reason::Files);
        assert_eq!(
            fmt_permille(x.sim.permille()),
            "66.7",
            "2*10/(10+20): the squash holds both parts"
        );
    }
}

#[test]
fn no_plus_commit_is_class_none_and_merges_and_empty_commits_are_counted() {
    let fx = Fx::new();
    let base = base_of(&fx);
    let m = mk(
        &fx,
        Some(base),
        &[put("f.txt", &edited(0, 5, "n", &[]))],
        "feat: x",
        T0 + 100,
    );
    let r = eval(&fx, &[m], &[]);
    assert_eq!((r.class, r.plus_commits), (Class::None, 0));
    assert_eq!((r.min, r.median_permille), (None, None));
    let l = mk(&fx, Some(base), &[put("l.txt", "l\n")], "l", T0 + 101);
    let rr = mk(&fx, Some(base), &[put("r.txt", "r\n")], "r", T0 + 102);
    let merge = mk_full(
        &fx,
        &[l, rr],
        Some(l),
        &[put("r.txt", "r\n")],
        "merge",
        T0 + 103,
    );
    let empty = mk(&fx, Some(base), &[], "empty", T0 + 104);
    let r = eval(&fx, &[m], &[merge, empty]);
    assert_eq!(r.class, Class::None, "nothing to judge");
    assert_eq!(
        (r.skipped.merges, r.skipped.empty, r.plus_commits),
        (1, 1, 0)
    );
}

#[test]
fn the_window_is_seven_days_by_author_time() {
    let fx = Fx::new();
    let base = base_of(&fx);
    let t = T0 + 100 * DAY;
    let a = mk(
        &fx,
        Some(base),
        &[put("f.txt", &edited(0, 5, "n", &[]))],
        "feat: x",
        t,
    );
    let same = |time: i64| {
        mk(
            &fx,
            Some(base),
            &[put("f.txt", &edited(0, 5, "n", &[]))],
            "feat: x",
            time,
        )
    };
    let seven = same(t - 7 * DAY);
    let eight = same(t - 7 * DAY - 1);
    assert_eq!(
        cmp0(&eval(&fx, &[seven], &[a])).candidate,
        seven,
        "7 days older is in"
    );
    assert_eq!(
        eval(&fx, &[eight], &[a]).commits[0].outcome,
        Outcome::NoCandidate,
        "one second more is out"
    );
    let newer = same(t + 30 * DAY);
    assert_eq!(
        cmp0(&eval(&fx, &[newer], &[a])).candidate,
        newer,
        "newer is always in"
    );
    assert!(in_window(100, 100 - 7 * DAY, 7 * DAY) && !in_window(100, 99 - 7 * DAY, 7 * DAY));
}

#[test]
fn candidates_by_subject_by_subject_without_prefix_and_by_file_overlap() {
    let fx = Fx::new();
    let base = mk(&fx, None, &[put("a.txt", "a\n")], "base", T0);
    let a = mk(
        &fx,
        Some(base),
        &[put("f.txt", &lines(5, "n"))],
        "fix(core): thing",
        T0 + 100,
    );
    // same subject, other files
    let s1 = mk(
        &fx,
        Some(base),
        &[put("z.txt", "z\n")],
        "fix(core): thing",
        T0 + 101,
    );
    let r = eval(&fx, &[s1], &[a]);
    assert_eq!(cmp0(&r).reason, Reason::Subject);
    // subject differs only in type(scope):
    let s2 = mk(
        &fx,
        Some(base),
        &[put("z.txt", "z\n")],
        "feat(ui)!: thing",
        T0 + 102,
    );
    let r = eval(&fx, &[s2], &[a]);
    assert_eq!(cmp0(&r).reason, Reason::SubjectStripped);
    // other subject, same single file: the overlap is 1/1
    let s3 = mk(
        &fx,
        Some(base),
        &[put("f.txt", &lines(6, "n"))],
        "something else",
        T0 + 103,
    );
    let r = eval(&fx, &[s3], &[a]);
    assert_eq!(cmp0(&r).reason, Reason::Files);
    // 1 file of 3 is less than half: no candidate
    let wide = mk(
        &fx,
        Some(base),
        &[
            put("f.txt", &lines(5, "n")),
            put("g.txt", "g\n"),
            put("h.txt", "h\n"),
        ],
        "wide change",
        T0 + 104,
    );
    let narrow = mk(
        &fx,
        Some(base),
        &[put("f.txt", &lines(6, "n"))],
        "other",
        T0 + 105,
    );
    assert_eq!(
        eval(&fx, &[narrow], &[wide]).commits[0].outcome,
        Outcome::NoCandidate
    );
    // 2 of 3 files: at least half
    let two = mk(
        &fx,
        Some(base),
        &[put("f.txt", &lines(6, "n")), put("g.txt", "g2\n")],
        "other",
        T0 + 106,
    );
    assert_eq!(cmp0(&eval(&fx, &[two], &[wide])).reason, Reason::Files);
}

#[test]
fn the_subject_prefix_is_stripped_like_conventional_commits() {
    assert_eq!(strip_type_scope(b"fix(core): thing"), b"thing");
    assert_eq!(strip_type_scope(b"feat!: thing"), b"thing");
    assert_eq!(strip_type_scope(b"feat(a-b)!:   thing"), b"thing");
    assert_eq!(strip_type_scope(b"thing"), b"thing");
    assert_eq!(
        strip_type_scope(b"Fix: thing"),
        b"Fix: thing",
        "type is lower case"
    );
    assert_eq!(strip_type_scope(b"fix(core: thing"), b"fix(core: thing");
    assert_eq!(
        strip_type_scope(b"fix:"),
        b"fix:",
        "nothing left: unchanged"
    );
}

#[test]
fn the_best_candidate_is_the_highest_and_a_tie_goes_to_the_smaller_id() {
    let fx = Fx::new();
    let base = base_of(&fx);
    let a = mk(
        &fx,
        Some(base),
        &[put("f.txt", &edited(0, 20, "new", &[]))],
        "feat: x",
        T0 + 100,
    );
    let best = mk(
        &fx,
        Some(base),
        &[put("f.txt", &edited(0, 20, "new", &[]))],
        "feat: x",
        T0 + 102,
    );
    // the worse one gets the smaller commit ID (candidates are tried in ID order), so "first" is not "best"
    let worse = (0..)
        .map(|k| {
            mk(
                &fx,
                Some(base),
                &[put("f.txt", &edited(0, 20, "new", &[2, 4, 6]))],
                "feat: x",
                T0 + 101 + k,
            )
        })
        .find(|w| *w < best)
        .unwrap();
    let r = eval(&fx, &[worse, best], &[a]);
    assert_eq!(cmp0(&r).candidate, best);
    assert_eq!(fmt_permille(cmp0(&r).sim.permille()), "100.0");
    // two equal candidates: the smaller commit ID, whatever the input order
    let twin = mk(
        &fx,
        Some(base),
        &[put("f.txt", &edited(0, 20, "new", &[]))],
        "feat: x",
        T0 + 103,
    );
    let want = best.min(twin);
    assert_eq!(cmp0(&eval(&fx, &[best, twin], &[a])).candidate, want);
    assert_eq!(cmp0(&eval(&fx, &[twin, best], &[a])).candidate, want);
}

#[test]
fn candidates_are_cut_at_the_limit_and_the_cut_is_reported() {
    let fx = Fx::new();
    let base = base_of(&fx);
    let a = mk(
        &fx,
        Some(base),
        &[put("f.txt", &edited(0, 5, "n", &[]))],
        "feat: x",
        T0 + 100,
    );
    let ms: Vec<ObjectId> = (0..3)
        .map(|i| {
            mk(
                &fx,
                Some(base),
                &[put("f.txt", &edited(0, 5, "n", &[]))],
                "feat: x",
                T0 + 101 + i,
            )
        })
        .collect();
    let lim = Limits {
        max_candidates: 2,
        ..Limits::default()
    };
    assert!(eval_with(&fx, &ms, &[a], &lim).candidates_cut);
    assert!(!eval(&fx, &ms, &[a]).candidates_cut);
    let repo = fx.repo();
    let mut cache = SimCache::default();
    let mut desc = ms.clone();
    desc.sort_by(|x, y| y.cmp(x));
    let idx = MainIndex::build(&repo, &mut cache, &desc, None).unwrap();
    let info = commit_info(&repo, a).unwrap();
    let files = cache.files(&repo, a).unwrap().unwrap();
    // indexed in descending ID order; the cut keeps the two smallest IDs
    let got: Vec<ObjectId> = idx
        .candidates(&info, &files, &lim)
        .0
        .iter()
        .map(|c| c.id)
        .collect();
    let mut want = ms.clone();
    want.sort();
    want.truncate(2);
    assert_eq!(got, want);
    assert_eq!(idx.len(), 3);
}

#[test]
fn a_commit_without_value_prevents_landed() {
    let fx = Fx::new();
    let base = base_of(&fx);
    let a = mk(
        &fx,
        Some(base),
        &[put("f.txt", &edited(0, 20, "new", &[]))],
        "feat: x",
        T0 + 100,
    );
    let m = mk(
        &fx,
        Some(base),
        &[put("f.txt", &edited(0, 20, "new", &[]))],
        "feat: x",
        T0 + 200,
    );
    assert_eq!(eval(&fx, &[m], &[a]).class, Class::Landed);
    let lim = Limits {
        max_keys: 10,
        ..Limits::default()
    };
    let r = eval_with(&fx, &[m], &[a], &lim);
    assert_eq!(
        (r.class, r.skipped.too_large, r.plus_commits),
        (Class::Partial, 1, 1)
    );
    assert_eq!(r.commits[0].outcome, Outcome::Skipped(Skip::TooLarge));
}

fn rep(sims: &[Option<(u64, u64, u64)>]) -> BranchReport {
    let commits = sims
        .iter()
        .enumerate()
        .map(|(i, s)| CommitReport {
            commit: ObjectId::from_hex(format!("{:040x}", i + 1).as_bytes()).unwrap(),
            subject: String::new(),
            outcome: match s {
                None => Outcome::NoCandidate,
                Some((inter, a, b)) => {
                    let sim = Sim {
                        inter: *inter,
                        a: *a,
                        b: *b,
                    };
                    Outcome::Compared(Compared {
                        candidate: ObjectId::null(gix::hash::Kind::Sha1),
                        candidate_subject: String::new(),
                        reason: Reason::Subject,
                        sim,
                        no_path: sim,
                        evidence: vec![],
                    })
                }
            },
        })
        .collect();
    summarize(commits, SkippedCounts::default(), false, 95, 70)
}

#[test]
fn class_boundaries_are_inclusive() {
    assert_eq!(rep(&[Some((19, 20, 20))]).class, Class::Landed, "95.0");
    assert_eq!(
        rep(&[Some((19, 20, 20)), Some((20, 20, 20))]).class,
        Class::Landed
    );
    assert_eq!(rep(&[Some((18, 19, 19))]).class, Class::Modified, "94.7");
    assert_eq!(
        rep(&[Some((14, 20, 20))]).class,
        Class::Modified,
        "70.0 is not below low"
    );
    assert_eq!(rep(&[Some((27, 40, 40))]).class, Class::Partial, "67.5");
    assert_eq!(
        rep(&[Some((20, 20, 20)), None]).class,
        Class::Partial,
        "one without candidate"
    );
    assert_eq!(
        rep(&[Some((20, 20, 20)), Some((14, 20, 20))]).class,
        Class::Modified
    );
    assert_eq!(
        rep(&[Some((20, 20, 20)), Some((27, 40, 40))]).class,
        Class::Partial,
        "one below low"
    );
    assert_eq!(rep(&[]).class, Class::None);
}

#[test]
fn figures_count_minimum_and_median() {
    // 100.0, 90.0, 80.0 -> median 90.0; two values 100.0 and 80.0 -> 90.0
    let r = rep(&[Some((20, 20, 20)), Some((18, 20, 20)), Some((16, 20, 20))]);
    assert_eq!(
        (r.plus_commits, r.at_or_above, r.without_candidate),
        (3, 1, 0)
    );
    assert_eq!(r.min.map(|m| m.permille()), Some(800));
    assert_eq!(r.median_permille, Some(900));
    let r = rep(&[Some((20, 20, 20)), Some((16, 20, 20)), None]);
    assert_eq!((r.plus_commits, r.without_candidate), (3, 1));
    assert_eq!(
        r.median_permille,
        Some(900),
        "mean of the two middle values"
    );
    // the middle of an unsorted list, odd count
    let r = rep(&[
        Some((20, 20, 20)),
        Some((16, 20, 20)),
        Some((10, 20, 20)),
        Some((18, 20, 20)),
        Some((19, 20, 20)),
    ]);
    assert_eq!(r.median_permille, Some(900));
    assert_eq!(r.min.map(|m| m.permille()), Some(500));
}

#[test]
fn evidence_names_the_biggest_differences_masked_and_at_most_five() {
    let fx = Fx::new();
    let base = base_of(&fx);
    let a = mk(
        &fx,
        Some(base),
        &[put("f.txt", &edited(0, 20, "new", &[]))],
        "feat: x",
        T0 + 100,
    );
    // 8 lines differ, two of them with control and direction characters
    let mut text = edited(0, 20, "new", &[1, 2, 3, 4, 5, 6]);
    text = text
        .replace("odd1\n", "\u{1b}[31mred\u{202e}\n")
        .replace("odd2\n", "tab\there\n");
    let m = mk(&fx, Some(base), &[put("f.txt", &text)], "feat: x", T0 + 200);
    let r = eval(&fx, &[m], &[a]);
    let ev = &cmp0(&r).evidence;
    assert_eq!(ev.len(), 5, "at most 5 of the 12 differing keys");
    assert!(ev
        .iter()
        .all(|e| e.count_a != e.count_b && e.path == "f.txt"));
    let all: String = ev.iter().map(|e| e.line.clone()).collect();
    let shown = evidence_all(&fx, a, m);
    assert!(
        !shown.contains('\u{1b}') && !shown.contains('\u{202e}') && !shown.contains('\t'),
        "{shown:?}"
    );
    assert!(
        shown.contains("\\u{1b}[31mred\\u{202e}") && shown.contains("tab\\there"),
        "{shown:?}"
    );
    let _ = all;
}

fn evidence_all(fx: &Fx, a: ObjectId, m: ObjectId) -> String {
    let repo = fx.repo();
    let lim = Limits {
        max_evidence: 100,
        ..Limits::default()
    };
    match (
        commit_entries(&repo, a, &lim).unwrap(),
        commit_entries(&repo, m, &lim).unwrap(),
    ) {
        (Entries::Lines(x), Entries::Lines(y)) => evidence(&x, &y, 100)
            .iter()
            .map(|e| format!("{}{}\n", e.sign, e.line))
            .collect(),
        _ => panic!(),
    }
}

#[test]
fn masking_handles_bytes_controls_and_length() {
    assert_eq!(mask_bytes(b"a\xffb", 80), "a\\xffb");
    assert_eq!(mask_bytes(b"x\x1b[0m\ty", 80), "x\\u{1b}[0m\\ty");
    assert_eq!(mask_bytes("a\u{202e}b".as_bytes(), 80), "a\\u{202e}b");
    assert_eq!(mask_bytes(b"abcdef", 3), "abc\u{2026}");
    assert_eq!(mask_bytes(b"abc", 3), "abc");
}

#[test]
fn the_same_input_gives_the_same_output_in_any_order() {
    let fx = Fx::new();
    let base = base_of(&fx);
    let a1 = mk(
        &fx,
        Some(base),
        &[put("f.txt", &edited(0, 20, "new", &[]))],
        "feat: x",
        T0 + 100,
    );
    let a2 = mk(
        &fx,
        Some(a1),
        &[put("g.txt", &lines(5, "g"))],
        "feat: y",
        T0 + 101,
    );
    let m1 = mk(
        &fx,
        Some(base),
        &[put("f.txt", &edited(0, 20, "new", &[3]))],
        "feat: x",
        T0 + 200,
    );
    let m2 = mk(
        &fx,
        Some(m1),
        &[put("g.txt", &lines(5, "g"))],
        "feat: y",
        T0 + 201,
    );
    let r1 = eval(&fx, &[m1, m2], &[a1, a2]);
    let r2 = eval(&fx, &[m2, m1], &[a2, a1, a2]);
    assert_eq!(r1, r2);
    assert_eq!(r1, eval(&fx, &[m1, m2], &[a1, a2]));
    assert!(
        r1.commits[0].commit < r1.commits[1].commit,
        "sorted by commit ID"
    );
    assert_eq!(r1.class, Class::Landed);
}

#[test]
fn a_main_commit_older_than_since_is_not_indexed() {
    let fx = Fx::new();
    let base = base_of(&fx);
    let old = mk(
        &fx,
        Some(base),
        &[put("f.txt", &edited(0, 5, "n", &[]))],
        "feat: x",
        T0 + 100,
    );
    let new = mk(
        &fx,
        Some(base),
        &[put("f.txt", &edited(0, 5, "n", &[]))],
        "feat: x",
        T0 + 200,
    );
    let repo = fx.repo();
    let mut cache = SimCache::default();
    assert_eq!(
        MainIndex::build(&repo, &mut cache, &[old, new], None)
            .unwrap()
            .len(),
        2
    );
    assert_eq!(
        MainIndex::build(&repo, &mut cache, &[old, new], Some(T0 + 150))
            .unwrap()
            .len(),
        1
    );
}

#[test]
fn a_walk_over_main_leaves_out_merges() {
    let fx = Fx::new();
    let base = base_of(&fx);
    let l = mk(&fx, Some(base), &[put("l.txt", "l\n")], "l", T0 + 100);
    let r = mk(&fx, Some(base), &[put("r.txt", "r\n")], "r", T0 + 101);
    let merge = mk_full(
        &fx,
        &[l, r],
        Some(l),
        &[put("r.txt", "r\n")],
        "merge",
        T0 + 102,
    );
    let repo = fx.repo();
    let mut cache = SimCache::default();
    let idx = MainIndex::build_from_walk(&repo, &mut cache, merge, None).unwrap();
    assert_eq!(idx.len(), 3, "base, l and r; the merge is not a candidate");
}

#[test]
fn the_defaults_are_95_70_and_seven_days() {
    assert_eq!((HIGH_PCT, LOW_PCT, WINDOW_DAYS), (95, 70, 7));
    let l = Limits::default();
    assert_eq!(l.window_secs, 7 * DAY);
    assert_eq!(
        (
            l.max_keys,
            l.max_blob_bytes,
            l.max_candidates,
            l.max_evidence
        ),
        (20_000, 1 << 20, 100, 5)
    );
}

#[test]
fn evidence_orders_by_difference_then_key() {
    let en = |p: &str, sign: u8, l: &str| Entry {
        path: p.into(),
        sign,
        line: l.into(),
    };
    let a = vec![
        en("f", b'+', "x"),
        en("f", b'+', "x"),
        en("f", b'+', "x"),
        en("f", b'+', "y"),
        en("a", b'+', "z"),
        en("f", b'+', "same"),
        en("f", b'+', "same"),
    ];
    let b = vec![
        en("f", b'+', "y"),
        en("f", b'+', "y"),
        en("f", b'+', "same"),
        en("f", b'+', "same"),
    ];
    let ev = evidence(&a, &b, 10);
    let got: Vec<(String, char, u64, u64)> = ev
        .iter()
        .map(|e| (e.path.clone(), e.sign, e.count_a, e.count_b))
        .collect();
    // x: 3 vs 0 (diff 3) first; then diff 1: ("a", z) before ("f", y); the equal "same" is no evidence
    assert_eq!(
        got,
        vec![
            ("f".into(), '+', 3, 0),
            ("a".into(), '+', 1, 0),
            ("f".into(), '+', 1, 2)
        ]
    );
    assert_eq!(evidence(&a, &b, 1).len(), 1);
}

#[test]
fn the_cumulative_diff_of_equal_trees_is_empty() {
    let fx = Fx::new();
    let base = mk(&fx, None, &[put("a.txt", "x\n")], "base", T0);
    let repo = fx.repo();
    let tree = repo.find_commit(base).unwrap().tree_id().unwrap().detach();
    assert_eq!(
        tree_entries(&repo, tree, tree, &Limits::default()).unwrap(),
        Entries::Skipped(Skip::Empty)
    );
}

#[test]
fn a_value_below_the_low_threshold_is_not_shown_as_the_low_threshold() {
    // 2 * 1749 / 5000 = 69.96: rounds to 70.0 but is below 70
    let s = Sim {
        inter: 1749,
        a: 2500,
        b: 2500,
    };
    assert_eq!(s.permille(), 700);
    assert!(!s.ge_pct(70));
    assert_eq!(s.shown_in(95, 70), 699, "shown as 69.9");
    // a value at or above a threshold keeps its rounded form; the high threshold is still protected
    let ok = Sim {
        inter: 7,
        a: 10,
        b: 10,
    };
    assert_eq!(ok.shown_in(95, 70), 700, "70.0 is at the low threshold");
    let high = Sim {
        inter: 4749,
        a: 5000,
        b: 5001,
    };
    assert_eq!(high.shown_in(95, 70), 949);
    // the median uses the protected value
    let r = rep(&[Some((1749, 2500, 2500))]);
    assert_eq!(r.class, Class::Partial);
    assert_eq!(r.median_permille, Some(699));
}
