// SPDX-License-Identifier: EUPL-1.2 AND BSD-3-Clause
//! `stw absorb`: which stack commit owns a staged hunk, and the fixups and the checksum that follow from it.
//! Vectors: the cases of the 91 tests of the Python spike (`spikes/stw/absorb/test_stw_absorb_plan.py`).
use serde_json::{json, Value};

use super::absorb_fx::*;

fn target(h: &Value) -> Option<&str> {
    h["target"]["sha"].as_str()
}

#[test]
fn single_hunk_goes_to_the_owner_commit() {
    let fx = Fx::new();
    let (_c1, c2, _c3, t3) = fx.base10(false);
    fx.stage1("a.txt", &replace_line(&t3, 3, "l03 fixed"));
    let p = fx.plan(&[]);
    assert_eq!(p["hunks"].as_array().unwrap().len(), 1);
    let h = hunk(&p, 0);
    assert_eq!(h["assignment"], "absorbed");
    assert_eq!(h["target"], json!({"sha": c2, "subject": "feat: two"}));
    assert_eq!(h["path"], "a.txt");
    assert_eq!(h["old"], json!({"start": 3, "count": 1}));
    assert_eq!(h["new"], json!({"start": 3, "count": 1}));
    assert_eq!(
        (&h["basis"], &h["reason"]),
        (&json!("overlaps-lines"), &Value::Null)
    );
}

#[test]
fn two_hunks_go_to_two_commits_and_keep_order() {
    let fx = Fx::new();
    let (_c1, c2, c3, t3) = fx.base10(false);
    fx.stage1(
        "a.txt",
        &replace_line(&replace_line(&t3, 8, "l08 fixed"), 3, "l03 fixed"),
    );
    let p = fx.plan(&[]);
    assert_eq!(target(hunk(&p, 0)), Some(c2.as_str()));
    assert_eq!(target(hunk(&p, 1)), Some(c3.as_str()));
    assert_eq!(hunk(&p, 0)["id"], "h1");
    assert_eq!(hunk(&p, 1)["id"], "h2");
    assert_eq!(
        p["rewrite"]["count"], 2,
        "the oldest target decides, not the newest"
    );
    assert_eq!(p["rewrite"]["commits"], json!([c2, c3]));
}

#[test]
fn line_numbers_shift_through_newer_commits() {
    let fx = Fx::new();
    let t = l(6);
    let c1 = fx.commit1("a.txt", &t, "feat: one");
    let t2 = format!("new top\n{t}");
    fx.commit1("a.txt", &t2, "feat: top line");
    fx.stage1("a.txt", &replace_line(&t2, 5, "l04 fixed"));
    let p = fx.plan(&[]);
    assert_eq!(target(hunk(&p, 0)), Some(c1.as_str()));
    assert_eq!(hunk(&p, 0)["old"]["start"], 5);
}

#[test]
fn overlap_is_found_after_newer_commits_moved_the_lines() {
    // the removed side of the hunk moves with the newer commits (31 of 123 oracle scenarios differ without it)
    for kind in ["replace", "replace2", "delete"] {
        let fx = Fx::new();
        let t0 = l(14);
        fx.commit1("a.txt", &t0, "chore: init");
        fx.publish("HEAD");
        let t1 = replace_line(&replace_line(&t0, 6, "l06 by c1"), 7, "l07 by c1");
        let c1 = fx.commit1("a.txt", &t1, "feat: one");
        let t2 = format!("top1\ntop2\ntop3\n{t1}");
        fx.commit1("a.txt", &t2, "feat: three lines on top");
        let mut ls: Vec<&str> = t2.split('\n').collect();
        match kind {
            "replace" => ls[8] = "fixed",
            "replace2" => {
                ls.splice(8..10, ["fixed8", "fixed9"]);
            }
            _ => {
                ls.remove(8);
            }
        }
        fx.stage1("a.txt", &ls.join("\n"));
        let p = fx.plan(&[]);
        let h = hunk(&p, 0);
        assert_eq!(h["assignment"], "absorbed", "{kind}");
        assert_eq!(h["reason"], Value::Null, "{kind}");
        assert_eq!(target(h), Some(c1.as_str()), "{kind}");
    }
}

fn three_commits(fx: &Fx) -> (String, String, String) {
    let t1 = l(6);
    fx.commit1("a.txt", &t1, "feat: one");
    let t2 = replace_line(&t1, 2, "l02 by c2");
    let c2 = fx.commit1("a.txt", &t2, "feat: two");
    let t3 = replace_line(&t2, 3, "l03 by c3");
    let c3 = fx.commit1("a.txt", &t3, "feat: three");
    (c2, c3, t3)
}

#[test]
fn hunk_over_two_commits_is_ambiguous() {
    let fx = Fx::new();
    let (c2, c3, t3) = three_commits(&fx);
    fx.stage1("a.txt", &replace_line(&replace_line(&t3, 2, "x2"), 3, "x3"));
    let p = fx.plan(&[]);
    let h = hunk(&p, 0);
    assert_eq!(
        (&h["assignment"], &h["reason"]),
        (&json!("skipped"), &json!("ambiguous"))
    );
    let mut origins: Vec<&str> = h["origins"]
        .as_array()
        .unwrap()
        .iter()
        .map(|o| o["sha"].as_str().unwrap())
        .collect();
    origins.sort();
    let mut want = vec![c2.as_str(), c3.as_str()];
    want.sort();
    assert_eq!(origins, want);
    assert_eq!(
        target(h),
        Some(c3.as_str()),
        "the target that would be chosen stays named"
    );
    assert_eq!(p["fixups"], json!([]));
}

#[test]
fn ambiguous_newest_flag_takes_the_newest() {
    let fx = Fx::new();
    let (_c2, c3, t3) = three_commits(&fx);
    fx.stage1("a.txt", &replace_line(&replace_line(&t3, 2, "x2"), 3, "x3"));
    let p = fx.plan(&["--ambiguous-newest"]);
    let h = hunk(&p, 0);
    assert_eq!(
        (&h["assignment"], &h["reason"]),
        (&json!("absorbed"), &Value::Null)
    );
    assert_eq!(target(h), Some(c3.as_str()));
}

#[test]
fn lines_of_a_commit_and_of_an_older_one_beyond_the_stack_are_ambiguous() {
    let fx = Fx::new();
    let t1 = l(6);
    let c1 = fx.commit1("a.txt", &t1, "feat: one");
    let t2 = replace_line(&t1, 3, "l03 by c2");
    let c2 = fx.commit1("a.txt", &t2, "feat: two");
    fx.stage1("a.txt", &replace_line(&replace_line(&t2, 2, "x2"), 3, "x3"));
    let p = fx.plan(&["--base", &c1]);
    let h = hunk(&p, 0);
    assert_eq!(
        (&h["assignment"], &h["reason"]),
        (&json!("skipped"), &json!("ambiguous"))
    );
    let mut origins: Vec<&str> = h["origins"]
        .as_array()
        .unwrap()
        .iter()
        .map(|o| o["sha"].as_str().unwrap())
        .collect();
    origins.sort();
    let mut want = vec![c1.as_str(), c2.as_str()];
    want.sort();
    assert_eq!(origins, want);
}

#[test]
fn pure_insertion_away_from_changes_has_no_candidate() {
    let fx = Fx::new();
    let (_c1, _c2, _c3, t3) = fx.base10(true);
    fx.stage1("a.txt", &insert_after(&t3, 5, "inserted"));
    let p = fx.plan(&[]);
    let h = hunk(&p, 0);
    assert_eq!(h["assignment"], "skipped");
    assert_eq!(h["reason"], "no-candidate");
    assert_eq!(h["target"], Value::Null);
    assert_eq!(h["origins"], json!([]));
    assert_eq!(h["old"]["count"], 0);
}

#[test]
fn insertion_touching_a_changed_line_goes_to_that_commit() {
    let fx = Fx::new();
    let (_c1, c2, _c3, t3) = fx.base10(false);
    fx.stage1("a.txt", &insert_after(&t3, 3, "inserted after line 3"));
    assert_eq!(target(hunk(&fx.plan(&[]), 0)), Some(c2.as_str()));
    fx.g(&["reset", "-q", "--", "a.txt"]);
    fx.stage1("a.txt", &insert_after(&t3, 2, "inserted before line 3"));
    assert_eq!(target(hunk(&fx.plan(&[]), 0)), Some(c2.as_str()));
}

#[test]
fn insertion_one_line_away_commutes() {
    let fx = Fx::new();
    let (_c1, _c2, _c3, t3) = fx.base10(true);
    fx.stage1("a.txt", &insert_after(&t3, 4, "inserted after line 4"));
    assert_eq!(hunk(&fx.plan(&[]), 0)["reason"], "no-candidate");
}

#[test]
fn deleted_lines_go_to_their_commit() {
    let fx = Fx::new();
    let (_c1, _c2, c3, t3) = fx.base10(false);
    let mut ls: Vec<&str> = t3.split('\n').collect();
    ls.remove(7);
    fx.stage1("a.txt", &ls.join("\n"));
    let p = fx.plan(&[]);
    let h = hunk(&p, 0);
    assert_eq!(target(h), Some(c3.as_str()));
    assert_eq!(
        (&h["new"]["count"], &h["old"]["count"]),
        (&json!(0), &json!(1))
    );
}

#[test]
fn file_added_by_a_commit_is_its_target_for_any_hunk() {
    let fx = Fx::new();
    fx.commit1("a.txt", &l(3), "feat: base");
    let t = lines(20, "n");
    let c2 = fx.commit1("n.txt", &t, "feat: new file");
    fx.commit1("a.txt", &format!("{}more\n", l(3)), "feat: more");
    fx.stage1("n.txt", &replace_line(&t, 15, "far"));
    let p = fx.plan(&[]);
    let h = hunk(&p, 0);
    assert_eq!(target(h), Some(c2.as_str()));
    assert_eq!(h["basis"], "file-added");
}

#[test]
fn hunks_of_one_file_use_the_offsets_of_the_hunks_before_them() {
    let fx = Fx::new();
    let t1 = l(12);
    fx.commit1("a.txt", &t1, "chore: init");
    fx.publish("HEAD");
    let t2 = replace_line(&t1, 10, "l10 by c2");
    let c2 = fx.commit1("a.txt", &t2, "feat: two");
    let ls: Vec<&str> = t2.split('\n').collect();
    let mut v: Vec<&str> = ls[..2].to_vec();
    v.extend(["extra1", "extra2", "extra3"]);
    v.extend(&ls[2..]);
    let new = replace_line(&v.join("\n"), 13, "l10 fixed");
    fx.stage1("a.txt", &new);
    let p = fx.plan(&[]);
    assert_eq!(p["hunks"].as_array().unwrap().len(), 2);
    assert_eq!(hunk(&p, 0)["reason"], "no-candidate");
    assert_eq!(target(hunk(&p, 1)), Some(c2.as_str()));
    assert_eq!(hunk(&p, 1)["old"]["start"], 10);
}

#[test]
fn second_hunk_insertion_uses_isolated_coordinates() {
    let fx = Fx::new();
    let t1 = l(12);
    fx.commit1("a.txt", &t1, "chore: init");
    fx.publish("HEAD");
    let t2 = replace_line(&t1, 10, "l10 by c2");
    let c2 = fx.commit1("a.txt", &t2, "feat: two");
    let mut ls: Vec<&str> = t2.split('\n').collect();
    ls.insert(10, "after ten");
    ls.insert(2, "e1");
    ls.insert(2, "e2");
    fx.stage1("a.txt", &ls.join("\n"));
    let p = fx.plan(&[]);
    assert_eq!(hunk(&p, 0)["reason"], "no-candidate");
    assert_eq!(hunk(&p, 1)["reason"], Value::Null);
    assert_eq!(target(hunk(&p, 1)), Some(c2.as_str()));
}

#[test]
fn untouched_files_commute_and_other_files_are_independent() {
    let fx = Fx::new();
    let c1 = fx.commit(
        &[("a.txt", Some(&l(5))), ("b.txt", Some(&lines(5, "b")))],
        "feat: one",
    );
    let b2 = replace_line(&lines(5, "b"), 2, "b02 by c2");
    let c2 = fx.commit1("b.txt", &b2, "feat: two");
    fx.stage(&[
        ("a.txt", Some(&replace_line(&l(5), 2, "fixed a"))),
        ("b.txt", Some(&replace_line(&b2, 2, "fixed b"))),
    ]);
    let p = fx.plan(&[]);
    assert_eq!(
        (hunk(&p, 0)["path"].as_str(), target(hunk(&p, 0))),
        (Some("a.txt"), Some(c1.as_str()))
    );
    assert_eq!(
        (hunk(&p, 1)["path"].as_str(), target(hunk(&p, 1))),
        (Some("b.txt"), Some(c2.as_str()))
    );
}

#[test]
fn whitespace_change_is_a_change() {
    let fx = Fx::new();
    let (_c1, c2, _c3, t3) = fx.base10(false);
    fx.stage1("a.txt", &replace_line(&t3, 3, "l03 by c2 "));
    assert_eq!(target(hunk(&fx.plan(&[]), 0)), Some(c2.as_str()));
}

#[test]
fn crlf_lines() {
    let fx = Fx::new();
    let t = l(6).replace('\n', "\r\n");
    let c1 = fx.commit1("a.txt", &t, "feat: crlf");
    fx.commit1("b.txt", "x\n", "feat: other");
    fx.stage1("a.txt", &replace_line(&t, 3, "l03 fixed\r"));
    assert_eq!(target(hunk(&fx.plan(&[]), 0)), Some(c1.as_str()));
}

#[test]
fn no_newline_at_end_of_file() {
    let fx = Fx::new();
    let c1 = fx.commit1("a.txt", "one\ntwo\nthree", "feat: no eol");
    fx.commit1("b.txt", "x\n", "feat: other");
    fx.stage1("a.txt", "one\ntwo\nthree fixed");
    let p = fx.plan(&[]);
    let h = hunk(&p, 0);
    assert_eq!(h["assignment"], "absorbed");
    assert_eq!(target(h), Some(c1.as_str()));
}

#[test]
fn paths_with_spaces_quotes_and_non_ascii_stay_exact() {
    let fx = Fx::new();
    let names = ["dir/a b.txt", "q\"uote.txt", "üä.txt"];
    let files: Vec<(&str, Option<&str>)> = names
        .iter()
        .map(|n| (*n, Some("l01\nl02\nl03\n")))
        .collect();
    let c1 = fx.commit(&files, "feat: odd names");
    fx.commit1("z.txt", "z\n", "feat: other");
    let fixed = replace_line("l01\nl02\nl03\n", 2, "fixed");
    let staged: Vec<(&str, Option<&str>)> =
        names.iter().map(|n| (*n, Some(fixed.as_str()))).collect();
    fx.stage(&staged);
    let p = fx.plan(&[]);
    let mut got: Vec<&str> = p["hunks"]
        .as_array()
        .unwrap()
        .iter()
        .map(|h| h["path"].as_str().unwrap())
        .collect();
    got.sort();
    let mut want = names.to_vec();
    want.sort();
    assert_eq!(got, want);
    assert!(p["hunks"]
        .as_array()
        .unwrap()
        .iter()
        .all(|h| target(h) == Some(c1.as_str())));
}

// --- fixups, rewrite, checksum --------------------------------------------------------------------------------------

#[test]
fn one_fixup_per_target_with_the_subject_of_a_unique_commit() {
    let fx = Fx::new();
    let (_c1, c2, c3, t3) = fx.base10(false);
    fx.stage1(
        "a.txt",
        &replace_line(&replace_line(&t3, 8, "l08 fixed"), 3, "l03 fixed"),
    );
    assert_eq!(
        fx.plan(&[])["fixups"],
        json!([{"target": c2, "subject": "fixup! feat: two", "hunks": ["h1"]},
               {"target": c3, "subject": "fixup! feat: three", "hunks": ["h2"]}])
    );
}

#[test]
fn two_hunks_of_one_commit_share_a_fixup() {
    let fx = Fx::new();
    let t1 = l(12);
    let c1 = fx.commit1("a.txt", &t1, "feat: one");
    fx.commit1("b.txt", "x\n", "feat: two");
    fx.stage1(
        "a.txt",
        &replace_line(&replace_line(&t1, 2, "f2"), 10, "f10"),
    );
    let p = fx.plan(&[]);
    assert_eq!(p["hunks"].as_array().unwrap().len(), 2);
    assert_eq!(
        p["fixups"],
        json!([{"target": c1, "subject": "fixup! feat: one", "hunks": ["h1", "h2"]}])
    );
}

#[test]
fn a_subject_that_is_not_unique_in_the_stack_uses_the_full_sha() {
    let fx = Fx::new();
    let t1 = l(6);
    let c1 = fx.commit1("a.txt", &t1, "fix: same");
    fx.commit1("b.txt", "x\n", "fix: same");
    fx.stage1("a.txt", &replace_line(&t1, 2, "f2"));
    assert_eq!(fx.plan(&[])["fixups"][0]["subject"], format!("fixup! {c1}"));
}

#[test]
fn rewrite_counts_the_target_and_its_descendants() {
    let fx = Fx::new();
    let (_c1, c2, c3, t3) = fx.base10(false);
    fx.stage1("a.txt", &replace_line(&t3, 3, "fixed"));
    let p = fx.plan(&[]);
    assert_eq!(
        p["rewrite"],
        json!({"count": 2, "commits": [c2, c3], "ref": "refs/heads/main", "new_shas": null})
    );
    assert_eq!(
        p["summary"],
        json!({"hunks": 1, "absorbed": 1, "skipped": 0, "fixups": 1, "rewritten": 2})
    );
}

#[test]
fn nothing_absorbed_rewrites_nothing() {
    let fx = Fx::new();
    let (_c1, _c2, _c3, t3) = fx.base10(true);
    fx.stage1("a.txt", &insert_after(&t3, 5, "x"));
    let p = fx.plan(&[]);
    assert_eq!(p["fixups"], json!([]));
    assert_eq!(p["rewrite"]["count"], 0);
    assert_eq!(p["rewrite"]["commits"], json!([]));
    assert_eq!(p["summary"]["skipped"], 1);
}

fn is_sha256(s: &Value) -> bool {
    s.as_str().is_some_and(|s| {
        s.len() == 64
            && s.bytes()
                .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
    })
}

#[test]
fn plan_sum_is_stable_and_follows_the_plan() {
    let fx = Fx::new();
    let (_c1, _c2, _c3, t3) = fx.base10(false);
    fx.stage1("a.txt", &replace_line(&t3, 3, "fixed"));
    let (a, b) = (fx.plan(&[]), fx.plan(&[]));
    assert_eq!(a, b);
    assert!(is_sha256(&a["plan_sum"]), "{}", a["plan_sum"]);
    fx.stage1("a.txt", &replace_line(&t3, 3, "fixed again"));
    assert_ne!(fx.plan(&[])["plan_sum"], a["plan_sum"]);
}

#[test]
fn plan_sum_binds_the_options_the_ref_and_the_result() {
    let fx = Fx::new();
    let t1 = l(6);
    fx.commit1("a.txt", &t1, "chore: init");
    fx.publish("HEAD");
    let t2 = replace_line(&t1, 2, "l02 by c2");
    fx.commit1("a.txt", &t2, "feat: two");
    let t3 = replace_line(&t2, 3, "l03 by c3");
    fx.commit1("a.txt", &t3, "feat: three");
    fx.stage1("a.txt", &replace_line(&replace_line(&t3, 2, "x2"), 3, "x3"));
    let base = fx.plan(&[]);
    assert_eq!(hunk(&base, 0)["reason"], "ambiguous");
    let mut sums = vec![base["plan_sum"].clone()];
    for args in [
        &["--ambiguous-newest"][..],
        &["--max-stack", "11"],
        &["--base", "HEAD~1"],
    ] {
        let s = fx.plan(args)["plan_sum"].clone();
        assert!(!sums.contains(&s), "{args:?}");
        sums.push(s);
    }
    fx.g(&["branch", "-m", "renamed"]);
    let s = fx.plan(&[])["plan_sum"].clone();
    assert!(!sums.contains(&s), "the ref is part of the plan");
    assert_eq!(fx.plan(&[])["plan_sum"], s);
}

#[test]
fn plan_sum_binds_the_index_digest() {
    // a staged binary file is skipped with the same reason whatever its content: only the digest tells the two plans apart
    let fx = Fx::new();
    fx.commit1("a.txt", &l(4), "feat: one");
    fx.write("bin.dat", [0u8, 1, 2]);
    fx.g(&["add", "bin.dat"]);
    fx.commit(&[], "feat: binary");
    let mut seen = Vec::new();
    for last in [3u8, 4] {
        fx.write("bin.dat", [0u8, 1, last]);
        fx.g(&["add", "bin.dat"]);
        seen.push(fx.plan(&[]));
    }
    assert_eq!(seen[0]["files_skipped"], seen[1]["files_skipped"]);
    assert_ne!(seen[0]["index_digest"], seen[1]["index_digest"]);
    assert_ne!(seen[0]["plan_sum"], seen[1]["plan_sum"]);
}

#[test]
fn two_runs_give_identical_bytes() {
    let fx = Fx::new();
    let (_c1, _c2, _c3, t3) = fx.base10(false);
    fx.stage1("a.txt", &replace_line(&replace_line(&t3, 8, "f8"), 3, "f3"));
    let (a, b) = (fx.run(&["--json"]), fx.run(&["--json"]));
    assert_eq!(a.rc, 0, "{}", a.err);
    assert!(!a.out.is_empty());
    assert_eq!((&a.out, a.rc), (&b.out, b.rc));
    let (a, b) = (fx.run(&[]), fx.run(&[]));
    assert!(!a.out.is_empty());
    assert_eq!(a.out, b.out);
}

#[test]
fn origins_count_the_lines_of_each_commit() {
    let fx = Fx::new();
    let t1 = l(8);
    fx.commit1("a.txt", &t1, "feat: one");
    let t2 = replace_line(&replace_line(&t1, 3, "l03 by c2"), 4, "l04 by c2");
    let c2 = fx.commit1("a.txt", &t2, "feat: two");
    fx.stage1("a.txt", &replace_line(&replace_line(&t2, 3, "x3"), 4, "x4"));
    let p = fx.plan(&[]);
    let h = hunk(&p, 0);
    assert_eq!(h["assignment"], "absorbed");
    assert_eq!(
        h["origins"],
        json!([{"sha": c2, "subject": "feat: two", "lines": 2}])
    );
}

#[test]
fn origins_come_in_the_order_of_the_first_line_of_each_commit() {
    let fx = Fx::new();
    let t1 = l(8);
    fx.commit1("a.txt", &t1, "feat: one");
    let t2 = replace_line(&t1, 4, "l04 by c2");
    let c2 = fx.commit1("a.txt", &t2, "feat: two");
    let t3 = replace_line(&t2, 3, "l03 by c3");
    let c3 = fx.commit1("a.txt", &t3, "feat: three");
    fx.stage1("a.txt", &replace_line(&replace_line(&t3, 3, "x3"), 4, "x4"));
    let p = fx.plan(&[]);
    // line 3 (c3) comes before line 4 (c2) although c2 is the older commit
    assert_eq!(
        hunk(&p, 0)["origins"],
        json!([{"sha": c3, "subject": "feat: three", "lines": 1}, {"sha": c2, "subject": "feat: two", "lines": 1}])
    );
}
