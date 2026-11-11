//! `stw restack plan`: the whole-stack preview and the port of the single-branch prediction of the Go
//! `restack plan` (`internal/restack`). Fixtures are built with gix in a scratch repository (`tests/common`); real
//! `git rebase` in a clone inside the same scratch directory is the oracle (plan says clean = git succeeds, plan says
//! conflict = git stops, same files). Go cases that map here are named in `go_*` tests; the evidence the Go command
//! adds (range pairing, cherry classification, identities, overlap, effects, lease texts, hooks, exit 3) is not
//! ported, see README "Deviations from the Go `restack plan`".
use std::path::Path;
use std::process::{Command, Output};

use crate::common::*;
use gix::ObjectId;

type Edits<'a> = Vec<(&'a str, Option<&'a str>)>;

fn git_available() -> bool {
    Command::new("git")
        .arg("--version")
        .output()
        .is_ok_and(|o| o.status.success())
}

fn skip() -> bool {
    if git_available() {
        return false;
    }
    eprintln!("git not installed: not run");
    true
}

fn stw(cwd: &Path, args: &[&str]) -> Output {
    assert!(cwd.is_absolute(), "unconfined");
    let home = tempfile::tempdir().unwrap();
    Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_clear()
        .env("PATH", std::env::var_os("PATH").unwrap())
        .env("HOME", home.path())
        .current_dir(cwd)
        .args(["restack", "plan"])
        .args(args)
        .output()
        .unwrap()
}

struct Run {
    code: i32,
    text: String,
    err: String,
}

impl Run {
    fn first(&self) -> &str {
        self.text.lines().next().unwrap_or("")
    }
    fn json(&self) -> serde_json::Value {
        serde_json::from_str(&self.text).unwrap_or_else(|e| panic!("{e}: {}", self.text))
    }
}

fn run(fx: &Fx, args: &[&str]) -> Run {
    let o = stw(&fx.path, args);
    Run {
        code: o.status.code().unwrap_or(-1),
        text: String::from_utf8_lossy(&o.stdout).into_owned(),
        err: String::from_utf8_lossy(&o.stderr).into_owned(),
    }
}

const BASE: &[(&str, &str)] = &[
    ("a.txt", "a1\na2\na3\n"),
    ("shared.txt", "s1\ns2\ns3\n"),
    ("m.txt", "m1\nm2\nm3\n"),
    ("x.txt", "x1\nx2\nx3\n"),
    ("y.txt", "y1\ny2\ny3\n"),
];

/// A stack a, b, c on `m1`; `main` (origin/main, origin/HEAD) moved on to `m2`. HEAD is on `c`.
struct Stack {
    fx: Fx,
    m1: ObjectId,
    m2: ObjectId,
    tips: [ObjectId; 3],
    /// every commit of the three levels, oldest first
    all: Vec<Vec<ObjectId>>,
}

fn chain(fx: &Fx, parent: ObjectId, commits: &[Edits]) -> Vec<ObjectId> {
    let mut out = Vec::new();
    let mut p = parent;
    for (i, e) in commits.iter().enumerate() {
        p = fx.commit(Some(p), e, &format!("feat: step {i}"));
        out.push(p);
    }
    out
}

fn build_with(base_extra: Edits, main: Edits, a: &[Edits], b: &[Edits], c: &[Edits]) -> Stack {
    let fx = Fx::new();
    let mut base: Edits = BASE.iter().map(|(p, c)| (*p, Some(*c))).collect();
    base.extend(base_extra);
    let m1 = fx.commit(None, &base, "m1");
    let m2 = fx.commit(Some(m1), &main, "m2");
    let ca = chain(&fx, m1, a);
    let cb = chain(&fx, *ca.last().unwrap_or(&m1), b);
    let cc = chain(&fx, *cb.last().unwrap_or(ca.last().unwrap_or(&m1)), c);
    let tip = |v: &Vec<ObjectId>, d: ObjectId| *v.last().unwrap_or(&d);
    let ta = tip(&ca, m1);
    let tb = tip(&cb, ta);
    let tc = tip(&cc, tb);
    fx.branch("a", ta);
    fx.branch("b", tb);
    fx.branch("c", tc);
    fx.branch("main", m2);
    fx.remote("main", m2);
    fx.origin_head("main");
    fx.set_head("c");
    Stack {
        fx,
        m1,
        m2,
        tips: [ta, tb, tc],
        all: vec![ca, cb, cc],
    }
}

fn build(main: Edits, a: &[Edits], b: &[Edits], c: &[Edits]) -> Stack {
    build_with(vec![], main, a, b, c)
}

/// The clean default: main edits `m.txt`, a edits `a.txt` (two commits), b `x.txt`, c `y.txt`.
fn clean_stack() -> Stack {
    build(
        vec![("m.txt", Some("m1\nMAIN\nm3\n"))],
        &[
            vec![("a.txt", Some("A1\na2\na3\n"))],
            vec![("a-new.txt", Some("new\n"))],
        ],
        &[vec![("x.txt", Some("x1\nBEE\nx3\n"))]],
        &[vec![("y.txt", Some("y1\ny2\nSEE\n"))]],
    )
}

fn git(dir: &Path, args: &[&str]) -> (i32, String) {
    assert!(!dir.as_os_str().is_empty(), "empty repository path");
    assert!(
        dir.starts_with(std::env::temp_dir()) || std::env::var_os("CLAUDE_JOB_DIR").is_some(),
        "oracle runs only in a scratch directory: {}",
        dir.display()
    );
    let o = Command::new("git")
        .env_clear()
        .env("PATH", std::env::var_os("PATH").unwrap_or_default())
        .env("HOME", dir.parent().unwrap_or(dir))
        .env("LC_ALL", "C")
        .env("GIT_CONFIG_GLOBAL", "/dev/null")
        .env("GIT_CONFIG_SYSTEM", "/dev/null")
        .env("GIT_CONFIG_NOSYSTEM", "1")
        .env("GIT_EDITOR", "true")
        .env("GIT_AUTHOR_NAME", "T")
        .env("GIT_AUTHOR_EMAIL", "t@example.com")
        .env("GIT_COMMITTER_NAME", "T")
        .env("GIT_COMMITTER_EMAIL", "t@example.com")
        .current_dir(dir)
        .args(args)
        .output()
        .unwrap();
    (
        o.status.code().unwrap_or(-1),
        String::from_utf8_lossy(&o.stdout).into_owned(),
    )
}

/// What real `git rebase` does for the stack, level by level, in a clone: `None` = succeeded, `Some(files)` = stopped
/// with these unmerged files. Stops at the first conflict.
fn oracle(s: &Stack, names: &[&str]) -> Vec<Option<Vec<String>>> {
    let scratch = scratch();
    let clone = scratch.path().join("clone");
    let out = Command::new("git")
        .env_clear()
        .env("PATH", std::env::var_os("PATH").unwrap_or_default())
        .env("GIT_CONFIG_GLOBAL", "/dev/null")
        .env("GIT_CONFIG_SYSTEM", "/dev/null")
        .env("GIT_CONFIG_NOSYSTEM", "1")
        .args(["clone", "-q", "--no-hardlinks"])
        .arg(&s.fx.path)
        .arg(&clone)
        .output()
        .unwrap();
    assert!(out.status.success(), "clone failed");
    for n in names {
        let (c, _) = git(&clone, &["checkout", "-q", "-B", n, &format!("origin/{n}")]);
        assert_eq!(c, 0, "checkout {n}");
    }
    let onto = s.m2.to_string();
    let mut results = Vec::new();
    let mut prev: Option<(String, String)> = None; // branch name, old tip
    for (i, n) in names.iter().enumerate() {
        let (base, old_base) = match &prev {
            None => {
                let (_, mb) = git(&clone, &["merge-base", &onto, &format!("origin/{n}")]);
                (onto.clone(), mb.trim().to_string())
            }
            Some((name, old)) => (name.clone(), old.clone()),
        };
        let old_tip = git(&clone, &["rev-parse", &format!("origin/{n}")])
            .1
            .trim()
            .to_string();
        let (code, _) = git(&clone, &["rebase", "-q", "--onto", &base, &old_base, n]);
        if code == 0 {
            results.push(None);
        } else {
            let (_, files) = git(&clone, &["diff", "--name-only", "--diff-filter=U"]);
            let mut files: Vec<String> = files.lines().map(str::to_string).collect();
            files.sort();
            results.push(Some(files));
            git(&clone, &["rebase", "--abort"]);
            let _ = i;
            break;
        }
        prev = Some((n.to_string(), old_tip));
    }
    results
}

/// The plan against the oracle: clean-like states succeed, a conflict stops with the same files.
fn assert_matches_git(s: &Stack, names: &[&str], plan: &serde_json::Value) {
    let git_result = oracle(s, names);
    let levels = plan["levels"].as_array().unwrap();
    for (i, g) in git_result.iter().enumerate() {
        let state = levels[i]["state"].as_str().unwrap();
        match g {
            None => assert!(
                matches!(state, "clean" | "became-empty" | "already-upstream"),
                "level {i}: git rebase succeeded, the plan says {state}: {plan}"
            ),
            Some(files) => {
                assert_eq!(state, "conflict", "level {i}: git conflicts: {plan}");
                let want: Vec<&str> = files.iter().map(String::as_str).collect();
                let got: Vec<&str> = levels[i]["files"]
                    .as_array()
                    .unwrap()
                    .iter()
                    .map(|f| f.as_str().unwrap())
                    .collect();
                assert_eq!(got, want, "level {i}: files: {plan}");
            }
        }
    }
    // levels after the stop were not run by git: the plan must not call them clean
    for l in levels.iter().skip(git_result.len()) {
        assert!(
            matches!(
                l["state"].as_str().unwrap(),
                "inherited-conflict" | "not-computed"
            ),
            "{plan}"
        );
    }
}

#[test]
fn a_clean_stack_is_clean_like_git_rebase() {
    if skip() {
        return;
    }
    let s = clean_stack();
    let r = run(&s.fx, &["a", "b", "c", "--onto", "main", "--json"]);
    assert_eq!(r.code, 0, "{}\n{}", r.text, r.err);
    let v = r.json();
    assert_eq!(v["plan"], "plan: clean");
    assert_eq!(v["verdict"], "clean");
    assert_matches_git(&s, &["a", "b", "c"], &v);
    let l = v["levels"].as_array().unwrap();
    assert_eq!(l.len(), 3);
    assert_eq!(l[0]["branch"], "a");
    assert_eq!(l[0]["commits"], 2);
    assert_eq!(l[0]["old_base"], s.m1.to_string());
    assert_eq!(l[0]["new_base"], s.m2.to_string());
    assert_eq!(l[1]["old_base"], s.tips[0].to_string());
    assert_ne!(l[1]["new_base"], l[1]["old_base"]);
    assert!(l.iter().all(|x| x["state"] == "clean"));
    assert_eq!(v["summary"]["clean"], 3);
    assert_eq!(v["schema_version"], 2);
    assert_eq!(v["command"], "restack-plan");
    assert_eq!(v["source"], "explicit");
    // text
    let r = run(&s.fx, &["a", "b", "c", "--onto", "main"]);
    assert_eq!(r.code, 0);
    assert_eq!(r.first(), "plan: clean", "{}", r.text);
    assert!(r.text.contains("level 1  a  base "), "{}", r.text);
    assert!(r.text.contains("2 commits  clean"), "{}", r.text);
}

#[test]
fn a_conflict_at_level_two_is_inherited_by_level_three_like_git_rebase() {
    if skip() {
        return;
    }
    let s = build(
        vec![("shared.txt", Some("s1\nMAIN\ns3\n"))],
        &[vec![("a.txt", Some("A1\na2\na3\n"))]],
        &[vec![("shared.txt", Some("s1\nBEE\ns3\n"))]],
        &[vec![("y.txt", Some("y1\ny2\nSEE\n"))]],
    );
    let r = run(&s.fx, &["a", "b", "c", "--onto", "main", "--json"]);
    assert_eq!(r.code, 1, "{}", r.text);
    let v = r.json();
    assert_eq!(v["plan"], "plan: conflicts at b (files shared.txt)");
    let st: Vec<&str> = v["levels"]
        .as_array()
        .unwrap()
        .iter()
        .map(|l| l["state"].as_str().unwrap())
        .collect();
    assert_eq!(st, ["clean", "conflict", "inherited-conflict"]);
    assert_eq!(v["levels"][1]["conflicts"][0]["kind"], "content");
    assert_eq!(
        v["levels"][1]["conflicts"][0]["commit"],
        s.all[1][0].to_string()
    );
    assert!(v["levels"][2]["new_base"].is_null());
    assert_matches_git(&s, &["a", "b", "c"], &v);
    let t = run(&s.fx, &["a", "b", "c", "--onto", "main"]);
    assert_eq!(t.first(), "plan: conflicts at b (files shared.txt)");
    assert!(t.text.contains("conflict in shared.txt"), "{}", t.text);
    assert!(t.text.contains("inherited conflict"), "{}", t.text);
}

#[test]
fn a_conflict_at_level_one_and_at_level_three_like_git_rebase() {
    if skip() {
        return;
    }
    let main = || vec![("shared.txt", Some("s1\nMAIN\ns3\n"))];
    // level 1
    let s = build(
        main(),
        &[vec![("shared.txt", Some("s1\nAAA\ns3\n"))]],
        &[vec![("x.txt", Some("x1\nBEE\nx3\n"))]],
        &[vec![("y.txt", Some("y1\ny2\nSEE\n"))]],
    );
    let r = run(&s.fx, &["a", "b", "c", "--onto", "main", "--json"]);
    assert_eq!(r.code, 1);
    let v = r.json();
    assert_eq!(v["plan"], "plan: conflicts at a (files shared.txt)");
    assert_eq!(v["levels"][1]["state"], "inherited-conflict");
    assert_eq!(v["levels"][2]["state"], "inherited-conflict");
    assert_matches_git(&s, &["a", "b", "c"], &v);
    // level 3 only
    let s = build(
        main(),
        &[vec![("a.txt", Some("A1\na2\na3\n"))]],
        &[vec![("x.txt", Some("x1\nBEE\nx3\n"))]],
        &[vec![("shared.txt", Some("s1\nSEE\ns3\n"))]],
    );
    let r = run(&s.fx, &["a", "b", "c", "--onto", "main", "--json"]);
    assert_eq!(r.code, 1);
    let v = r.json();
    assert_eq!(v["plan"], "plan: conflicts at c (files shared.txt)");
    assert_eq!(v["levels"][0]["state"], "clean");
    assert_eq!(v["levels"][1]["state"], "clean");
    assert_matches_git(&s, &["a", "b", "c"], &v);
}

#[test]
fn a_level_that_main_already_has_is_already_upstream_like_git_rebase() {
    if skip() {
        return;
    }
    // main contains the very patch of a (same diff, so the same patch-id)
    let s = build(
        vec![("a.txt", Some("A1\na2\na3\n"))],
        &[vec![("a.txt", Some("A1\na2\na3\n"))]],
        &[vec![("x.txt", Some("x1\nBEE\nx3\n"))]],
        &[vec![("y.txt", Some("y1\ny2\nSEE\n"))]],
    );
    let r = run(&s.fx, &["a", "b", "c", "--onto", "main", "--json"]);
    assert_eq!(r.code, 0, "{}", r.text);
    let v = r.json();
    assert_eq!(v["levels"][0]["state"], "already-upstream");
    assert_eq!(v["levels"][0]["dropped"], 1);
    assert_eq!(v["levels"][1]["state"], "clean");
    assert_matches_git(&s, &["a", "b", "c"], &v);
}

#[test]
fn a_level_whose_change_main_made_differently_is_became_empty() {
    if skip() {
        return;
    }
    // main has the change of a too, together with another file: the patches differ, the result is empty
    let s = build(
        vec![
            ("a.txt", Some("A1\na2\na3\n")),
            ("m.txt", Some("m1\nMAIN\nm3\n")),
        ],
        &[vec![("a.txt", Some("A1\na2\na3\n"))]],
        &[vec![("x.txt", Some("x1\nBEE\nx3\n"))]],
        &[],
    );
    let r = run(&s.fx, &["a", "b", "--onto", "main", "--json"]);
    assert_eq!(r.code, 0, "{}", r.text);
    let v = r.json();
    assert_eq!(v["levels"][0]["state"], "became-empty");
    assert_matches_git(&s, &["a", "b"], &v);
}

#[test]
fn conflict_kinds_are_named() {
    if skip() {
        return;
    }
    // modify/delete: main deletes the file that a edits
    let s = build(
        vec![("shared.txt", None)],
        &[vec![("shared.txt", Some("s1\nAAA\ns3\n"))]],
        &[],
        &[],
    );
    let v = run(&s.fx, &["a", "--onto", "main", "--json"]).json();
    assert_eq!(
        v["levels"][0]["conflicts"][0]["kind"], "modify/delete",
        "{v}"
    );
    assert_matches_git(&s, &["a"], &v);
    // add/add: both add the same path with different content
    let s = build(
        vec![("new.txt", Some("main\n"))],
        &[vec![("new.txt", Some("branch\n"))]],
        &[],
        &[],
    );
    let v = run(&s.fx, &["a", "--onto", "main", "--json"]).json();
    assert_eq!(v["levels"][0]["conflicts"][0]["kind"], "add/add", "{v}");
    assert_matches_git(&s, &["a"], &v);
}

#[test]
fn a_hostile_path_and_subject_are_escaped() {
    if skip() {
        return;
    }
    let evil = "e\u{1b}[31mvil.txt";
    let s = build_with(
        vec![(evil, Some("1\n2\n3\n"))],
        vec![(evil, Some("1\nMAIN\n3\n"))],
        &[vec![(evil, Some("1\nBRANCH\n3\n"))]],
        &[],
        &[],
    );
    let r = run(&s.fx, &["a", "--onto", "main"]);
    assert_eq!(r.code, 1, "{}", r.text);
    assert!(!r.text.contains('\u{1b}'), "{:?}", r.text);
    assert!(r.text.contains("e\\u{1b}[31mvil.txt"), "{}", r.text);
    let j = run(&s.fx, &["a", "--onto", "main", "--json"]);
    assert!(!j.text.contains('\u{1b}'));
}

#[test]
fn a_merge_commit_in_a_level_is_refused_and_the_levels_above_are_not_computed() {
    if skip() {
        return;
    }
    let s = clean_stack();
    let side =
        s.fx.commit(Some(s.m1), &[("side.txt", Some("s\n"))], "side");
    let merge = s.fx.commit_with_parents(
        vec![s.tips[0], side],
        Some(s.tips[0]),
        &[("side.txt", Some("s\n"))],
        "merge",
    );
    s.fx.branch("b", merge);
    let c2 =
        s.fx.commit(Some(merge), &[("y.txt", Some("y1\ny2\nZ\n"))], "c2");
    s.fx.branch("c", c2);
    let r = run(&s.fx, &["a", "b", "c", "--onto", "main", "--json"]);
    assert_eq!(r.code, 1, "{}", r.text);
    let v = r.json();
    assert_eq!(v["verdict"], "refused");
    assert_eq!(v["levels"][1]["state"], "refused");
    assert_eq!(v["levels"][2]["state"], "not-computed");
    assert!(
        v["plan"]
            .as_str()
            .unwrap()
            .starts_with("plan: refused at b ("),
        "{v}"
    );
    assert!(v["levels"][1]["reasons"][0]
        .as_str()
        .unwrap()
        .contains("merge commit"));
}

#[test]
fn a_branch_that_is_not_on_the_one_before_is_refused_with_the_reason() {
    let s = clean_stack();
    // c is based on b, not the other way round
    let r = run(&s.fx, &["c", "b", "--onto", "main"]);
    assert_eq!(r.code, 1, "{}", r.text);
    assert!(
        r.first()
            .starts_with("plan: refused (not-stacked: b is not based on c"),
        "{}",
        r.text
    );
    let v = run(&s.fx, &["c", "b", "--onto", "main", "--json"]).json();
    assert_eq!(v["refusal"]["code"], "not-stacked");
    assert_eq!(v["levels"].as_array().unwrap().len(), 0);
}

#[test]
fn a_branch_without_common_history_is_refused() {
    let s = clean_stack();
    let root = s.fx.commit(None, &[("z.txt", Some("z\n"))], "root");
    s.fx.branch("orphan", root);
    let r = run(&s.fx, &["orphan", "--onto", "main"]);
    assert_eq!(r.code, 1, "{}", r.text);
    assert!(r.first().contains("no-merge-base"), "{}", r.text);
}

// the Go single-branch form: restack plan BRANCH --old-parent REV --onto BASIS

#[test]
fn go_the_old_parent_form_predicts_the_restack_of_one_branch_after_its_parent_was_rebase_merged() {
    if skip() {
        return;
    }
    // a was rebase-merged into main (main has its patch under other ids); b still sits on the old a
    let s = build(
        vec![("a.txt", Some("A1\na2\na3\n"))],
        &[vec![("a.txt", Some("A1\na2\na3\n"))]],
        &[vec![("x.txt", Some("x1\nBEE\nx3\n"))]],
        &[],
    );
    let r = run(
        &s.fx,
        &["b", "--old-parent", "a", "--onto", "main", "--json"],
    );
    assert_eq!(r.code, 0, "{}", r.text);
    let v = r.json();
    assert_eq!(v["levels"].as_array().unwrap().len(), 1);
    assert_eq!(v["levels"][0]["old_base"], s.tips[0].to_string());
    assert_eq!(v["levels"][0]["commits"], 1);
    assert_eq!(v["levels"][0]["state"], "clean");
    // `git rebase --onto main a b` as the oracle
    let scratch = scratch();
    let clone = scratch.path().join("clone");
    let out = Command::new("git")
        .env_clear()
        .env("PATH", std::env::var_os("PATH").unwrap())
        .env("GIT_CONFIG_GLOBAL", "/dev/null")
        .env("GIT_CONFIG_SYSTEM", "/dev/null")
        .env("GIT_CONFIG_NOSYSTEM", "1")
        .args(["clone", "-q", "--no-hardlinks"])
        .arg(&s.fx.path)
        .arg(&clone)
        .output()
        .unwrap();
    assert!(out.status.success());
    git(&clone, &["checkout", "-q", "-B", "b", "origin/b"]);
    let (code, _) = git(
        &clone,
        &[
            "rebase",
            "-q",
            "--onto",
            &s.m2.to_string(),
            &s.tips[0].to_string(),
            "b",
        ],
    );
    assert_eq!(code, 0);
}

#[test]
fn go_a_predicted_conflict_stops_the_replay_and_names_the_paths() {
    let s = build(
        vec![("x.txt", Some("x1\nMAIN\nx3\n"))],
        &[vec![("a.txt", Some("A1\na2\na3\n"))]],
        &[
            vec![("x.txt", Some("x1\nBEE\nx3\n"))],
            vec![("y.txt", Some("y1\nLATER\ny3\n"))],
        ],
        &[],
    );
    let v = run(
        &s.fx,
        &["b", "--old-parent", "a", "--onto", "main", "--json"],
    )
    .json();
    let l = &v["levels"][0];
    assert_eq!(l["state"], "conflict");
    assert_eq!(l["files"][0], "x.txt");
    // the replay stopped at the first commit: the second was not replayed
    assert_eq!(l["steps"].as_array().unwrap().len(), 1, "{v}");
    assert_eq!(l["commits"], 2);
}

#[test]
fn go_the_old_parent_must_be_an_ancestor_of_the_branch() {
    let s = clean_stack();
    // main is not below b
    let r = run(&s.fx, &["b", "--old-parent", "main", "--onto", "main"]);
    assert_eq!(r.code, 1, "{}", r.text);
    assert!(r.first().contains("old-parent-not-ancestor"), "{}", r.text);
}

#[test]
fn go_a_root_or_merge_commit_in_the_range_is_refused() {
    // merge case is a_merge_commit_in_a_level_is_refused...; the root case: an old parent below a root commit
    let s = clean_stack();
    let root = s.fx.commit(None, &[("z.txt", Some("z\n"))], "root");
    s.fx.branch("orphan", root);
    // unrelated histories are refused (Go: TestUnrelatedHistoriesAreRefused)
    let r = run(&s.fx, &["orphan", "--onto", "main"]);
    assert_eq!(r.code, 1);
    assert!(r.first().contains("no-merge-base"));
}

#[test]
fn go_options_are_validated_before_anything_is_read() {
    let s = clean_stack();
    for args in [
        vec!["-x", "--onto", "main"],
        vec!["a", "a", "--onto", "main"],
        vec!["a", "--onto", "-x"],
        vec!["a", "--onto", "a..b"],
        vec!["a", "b", "--old-parent", "m", "--onto", "main"],
        vec!["a", "--old-parent", "", "--onto", "main"],
        vec!["a", "--repo", "no-such-repo", "--onto", "main"],
    ] {
        let r = run(&s.fx, &args);
        assert_eq!(r.code, 2, "{args:?}: {}{}", r.text, r.err);
        assert!(r.text.is_empty(), "{args:?}");
    }
}

#[test]
fn an_unknown_branch_or_base_is_a_usage_error() {
    let s = clean_stack();
    let r = run(&s.fx, &["nope", "--onto", "main"]);
    assert_eq!(r.code, 2);
    assert!(r.err.contains("unknown local branch nope"), "{}", r.err);
    let r = run(&s.fx, &["a", "--onto", "no-such-rev"]);
    assert_eq!(r.code, 2);
    assert!(r.err.contains("--onto no-such-rev"), "{}", r.err);
}

#[test]
fn onto_defaults_to_the_default_branch_and_is_a_usage_error_without_one() {
    if skip() {
        return;
    }
    let s = clean_stack();
    let r = run(&s.fx, &["a", "b", "c", "--json"]);
    assert_eq!(r.code, 0, "{}{}", r.text, r.err);
    let v = r.json();
    assert_eq!(v["onto"]["rev"], "origin/main");
    assert_eq!(v["onto"]["commit"], s.m2.to_string());
    // no origin/HEAD, no origin/main: nothing to default to
    let fx = Fx::new();
    let m = fx.commit(None, &[("a.txt", Some("a\n"))], "m");
    fx.branch("x", m);
    let c = fx.commit(Some(m), &[("a.txt", Some("b\n"))], "c");
    fx.branch("y", c);
    let r = run(&fx, &["y"]);
    assert_eq!(r.code, 2, "{}", r.text);
    assert!(r.err.contains("pass --onto BASIS"), "{}", r.err);
}

// auto-detection

#[test]
fn the_chain_is_derived_from_the_checked_out_branch() {
    if skip() {
        return;
    }
    let s = clean_stack();
    let r = run(&s.fx, &["--onto", "main", "--json"]);
    assert_eq!(r.code, 0, "{}{}", r.text, r.err);
    let v = r.json();
    assert_eq!(v["source"], "auto-detected");
    let names: Vec<&str> = v["levels"]
        .as_array()
        .unwrap()
        .iter()
        .map(|l| l["branch"].as_str().unwrap())
        .collect();
    assert_eq!(names, ["a", "b", "c"]);
    assert_matches_git(&s, &["a", "b", "c"], &v);
    // from the middle of the stack: only a and b, and a note about c above
    s.fx.set_head("b");
    let v = run(&s.fx, &["--onto", "main", "--json"]).json();
    let names: Vec<&str> = v["levels"]
        .as_array()
        .unwrap()
        .iter()
        .map(|l| l["branch"].as_str().unwrap())
        .collect();
    assert_eq!(names, ["a", "b"]);
    assert!(
        v["notes"][0].as_str().unwrap().contains("sit above b: c"),
        "{v}"
    );
    let t = run(&s.fx, &["--onto", "main"]);
    assert!(t.text.contains("stack auto-detected"), "{}", t.text);
}

#[test]
fn a_branch_already_in_the_base_is_not_part_of_the_chain() {
    let s = clean_stack();
    // the local main equals origin/main: it is the base, not a level (it would be below a)
    let v = run(&s.fx, &["--json"]).json();
    let names: Vec<&str> = v["levels"]
        .as_array()
        .unwrap()
        .iter()
        .map(|l| l["branch"].as_str().unwrap())
        .collect();
    assert_eq!(names, ["a", "b", "c"], "{v}");
}

#[test]
fn two_branches_at_one_commit_are_ambiguous_and_the_explicit_spelling_is_suggested() {
    let s = clean_stack();
    s.fx.branch("b2", s.tips[1]);
    let r = run(&s.fx, &["--onto", "main"]);
    assert_eq!(r.code, 1, "{}", r.text);
    assert!(
        r.first().starts_with("plan: refused (ambiguous:"),
        "{}",
        r.text
    );
    assert!(
        r.text
            .contains("instead: stw restack plan b b2 --onto main"),
        "{}",
        r.text
    );
    let v = run(&s.fx, &["--onto", "main", "--json"]).json();
    assert_eq!(v["refusal"]["code"], "ambiguous");
    assert_eq!(v["levels"].as_array().unwrap().len(), 0);
    // a second name for the starting branch itself, in a stack without the b2 case
    let s = clean_stack();
    s.fx.branch("c2", s.tips[2]);
    let r = run(&s.fx, &["--onto", "main"]);
    assert_eq!(r.code, 1, "{}", r.text);
    assert!(r.first().contains("ambiguous"), "{}", r.text);
    assert!(
        r.text
            .contains("instead: stw restack plan c c2 --onto main"),
        "{}",
        r.text
    );
}

#[test]
fn a_fork_below_the_starting_branch_is_ambiguous() {
    let s = clean_stack();
    // d is a merge of two sibling branches p and q: both lie below d, none below the other
    let p = s.fx.commit(Some(s.m1), &[("p.txt", Some("p\n"))], "p");
    let q = s.fx.commit(Some(s.m1), &[("q.txt", Some("q\n"))], "q");
    let d =
        s.fx.commit_with_parents(vec![p, q], Some(p), &[("q.txt", Some("q\n"))], "d");
    s.fx.branch("p", p);
    s.fx.branch("q", q);
    s.fx.branch("d", d);
    s.fx.set_head("d");
    let r = run(&s.fx, &["--onto", "main", "--json"]);
    assert_eq!(r.code, 1, "{}", r.text);
    let v = r.json();
    assert_eq!(v["refusal"]["code"], "ambiguous");
    assert!(
        v["refusal"]["message"]
            .as_str()
            .unwrap()
            .contains("the stack forks"),
        "{v}"
    );
    assert!(v["refusal"]["suggestion"]
        .as_str()
        .unwrap()
        .contains("stw restack plan"));
}

#[test]
fn a_detached_head_or_a_branch_in_the_base_has_no_starting_point() {
    let s = clean_stack();
    std::fs::write(s.fx.path.join(".git/HEAD"), format!("{}\n", s.tips[2])).unwrap();
    let r = run(&s.fx, &["--onto", "main"]);
    assert_eq!(r.code, 1, "{}", r.text);
    assert!(r.first().contains("detached-head"), "{}", r.text);
    s.fx.set_head("main");
    let r = run(&s.fx, &["--onto", "main"]);
    assert_eq!(r.code, 1);
    assert!(r.first().contains("nothing-to-restack"), "{}", r.text);
}

#[test]
fn a_branch_without_common_history_is_refused_in_the_chain_too() {
    let s = clean_stack();
    let root = s.fx.commit(None, &[("z.txt", Some("z\n"))], "root");
    s.fx.branch("orphan", root);
    s.fx.set_head("orphan");
    let r = run(&s.fx, &["--onto", "main"]);
    assert_eq!(r.code, 1, "{}", r.text);
    assert!(r.first().contains("no-merge-base"), "{}", r.text);
}

// fail closed: the plan never says clean when the replay is not safe

#[test]
fn a_rename_setting_that_differs_from_the_replay_makes_a_clean_result_unknown() {
    let s = clean_stack();
    use std::io::Write;
    let mut cfg = std::fs::OpenOptions::new()
        .append(true)
        .open(s.fx.path.join(".git/config"))
        .unwrap();
    write!(cfg, "[merge]\n\trenames = false\n").unwrap();
    let r = run(&s.fx, &["a", "b", "c", "--onto", "main", "--json"]);
    assert_eq!(r.code, 1, "{}", r.text);
    let v = r.json();
    assert_eq!(v["verdict"], "unknown");
    assert_eq!(v["levels"][0]["state"], "unknown");
    assert!(
        v["levels"][0]["reasons"][0]
            .as_str()
            .unwrap()
            .contains("merge.renames=false"),
        "{v}"
    );
    assert_eq!(v["levels"][1]["state"], "not-computed");
    assert!(v["plan"]
        .as_str()
        .unwrap()
        .starts_with("plan: unknown at a ("));
}

#[test]
fn a_merge_attribute_makes_a_clean_result_unknown() {
    let s = build_with(
        vec![(".gitattributes", Some("*.txt merge=union\n"))],
        vec![("m.txt", Some("m1\nMAIN\nm3\n"))],
        &[vec![("a.txt", Some("A1\na2\na3\n"))]],
        &[],
        &[],
    );
    let r = run(&s.fx, &["a", "--onto", "main", "--json"]);
    assert_eq!(r.code, 1, "{}", r.text);
    let v = r.json();
    assert_eq!(v["levels"][0]["state"], "unknown");
    assert!(
        v["levels"][0]["reasons"][0]
            .as_str()
            .unwrap()
            .contains("merge attribute"),
        "{v}"
    );
}

#[test]
fn a_commit_that_changes_a_submodule_makes_a_clean_result_unknown() {
    let s = clean_stack();
    let target = s.m1;
    let g = s.fx.commit_gitlink(s.tips[0], "sub", target, "feat: sub");
    s.fx.branch("a", g);
    let b =
        s.fx.commit(Some(g), &[("x.txt", Some("x1\nBEE\nx3\n"))], "feat: b");
    s.fx.branch("b", b);
    let r = run(&s.fx, &["a", "b", "--onto", "main", "--json"]);
    assert_eq!(r.code, 1, "{}", r.text);
    let v = r.json();
    assert_eq!(v["levels"][0]["state"], "unknown");
    assert!(
        v["levels"][0]["reasons"][0]
            .as_str()
            .unwrap()
            .contains("changes a submodule"),
        "{v}"
    );
}

#[test]
fn a_conflict_stays_a_conflict_under_such_a_setting() {
    let s = build(
        vec![("shared.txt", Some("s1\nMAIN\ns3\n"))],
        &[vec![("shared.txt", Some("s1\nAAA\ns3\n"))]],
        &[],
        &[],
    );
    use std::io::Write;
    let mut cfg = std::fs::OpenOptions::new()
        .append(true)
        .open(s.fx.path.join(".git/config"))
        .unwrap();
    write!(cfg, "[merge]\n\trenames = false\n").unwrap();
    let v = run(&s.fx, &["a", "--onto", "main", "--json"]).json();
    assert_eq!(v["levels"][0]["state"], "conflict");
}

// read-only

#[test]
fn a_plan_writes_nothing_not_even_an_object() {
    if skip() {
        return;
    }
    let s = build(
        vec![("shared.txt", Some("s1\nMAIN\ns3\n"))],
        &[vec![("a.txt", Some("A1\na2\na3\n"))]],
        &[vec![("shared.txt", Some("s1\nBEE\ns3\n"))]],
        &[vec![("y.txt", Some("y1\ny2\nSEE\n"))]],
    );
    s.fx.checkout_head();
    let before = tree_snapshot(&s.fx.path);
    for args in [
        vec!["a", "b", "c", "--onto", "main"],
        vec!["a", "b", "c", "--onto", "main", "--json"],
        vec!["--onto", "main"],
        vec!["b", "--old-parent", "a", "--onto", "main"],
        vec!["c", "b", "--onto", "main"],
    ] {
        run(&s.fx, &args);
    }
    assert!(
        before == tree_snapshot(&s.fx.path),
        "the repository changed"
    );
}

#[test]
fn the_repository_comes_from_the_workspace_only() {
    let s = clean_stack();
    let other = Fx::new();
    let o = stw(
        &other.path,
        &["a", "--onto", "main", "--root", s.fx.path.to_str().unwrap()],
    );
    assert_eq!(o.status.code(), Some(2));
    assert!(String::from_utf8_lossy(&o.stderr).contains("not part of the workspace"));
}

#[test]
fn a_level_is_replayed_onto_the_rebased_level_below_not_onto_the_base() {
    if skip() {
        return;
    }
    // a changes a line of shared.txt, b changes it again, main does not touch the file: b merges cleanly onto the
    // rebased a, but would conflict onto main alone (where the line is still the old one)
    let s = build(
        vec![("m.txt", Some("m1\nMAIN\nm3\n"))],
        &[vec![("shared.txt", Some("s1\nAAA\ns3\n"))]],
        &[vec![("shared.txt", Some("s1\nBBB\ns3\n"))]],
        &[],
    );
    let r = run(&s.fx, &["a", "b", "--onto", "main", "--json"]);
    assert_eq!(r.code, 0, "{}", r.text);
    let v = r.json();
    assert_eq!(v["levels"][1]["state"], "clean");
    assert_matches_git(&s, &["a", "b"], &v);
}
