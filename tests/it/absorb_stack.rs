// SPDX-License-Identifier: EUPL-1.2 AND BSD-3-Clause
//! `stw absorb`: the stack (which commits can be targets) and the classification of hunks whose lines lie outside it.
use serde_json::{json, Value};

use super::absorb_fx::*;

const OTHER: (&str, &str) = ("Other", "o@example.invalid");

fn stack_shas(p: &Value) -> Vec<&str> {
    p["stack"]
        .as_array()
        .unwrap()
        .iter()
        .map(|s| s["sha"].as_str().unwrap())
        .collect()
}

fn reasons(p: &Value) -> Vec<Value> {
    p["hunks"]
        .as_array()
        .unwrap()
        .iter()
        .map(|h| h["reason"].clone())
        .collect()
}

#[test]
fn stack_limit_defaults_to_ten_and_reports_it() {
    let fx = Fx::new();
    let (shas, t) = fx.many(12);
    fx.stage1("a.txt", &replace_line(&t, 1, "fixed"));
    let p = fx.plan(&[]);
    assert_eq!(p["stack"].as_array().unwrap().len(), 10);
    assert_eq!(
        (&p["stack_end"], &p["max_stack"]),
        (&json!("limit"), &json!(10))
    );
    let h = hunk(&p, 0);
    assert_eq!(
        (&h["assignment"], &h["reason"]),
        (&json!("skipped"), &json!("outside-range"))
    );
    assert_eq!(p["stack"][0]["sha"], *shas.last().unwrap());
}

#[test]
fn max_stack_reaches_further() {
    let fx = Fx::new();
    let (shas, t) = fx.many(12);
    fx.stage1("a.txt", &replace_line(&t, 3, "fixed"));
    assert_eq!(hunk(&fx.plan(&[]), 0)["reason"], "outside-range");
    let p = fx.plan(&["--max-stack", "12"]);
    assert_eq!(hunk(&p, 0)["target"]["sha"], shas[1]);
    assert_eq!(p["stack_end"], "root");
}

#[test]
fn a_stack_of_exactly_the_limit_ends_at_the_root_not_at_the_limit() {
    let fx = Fx::new();
    let (_shas, t) = fx.many(3);
    fx.stage1("a.txt", &replace_line(&t, 3, "fixed"));
    let p = fx.plan(&["--max-stack", "3"]);
    assert_eq!(
        (p["stack"].as_array().unwrap().len(), &p["stack_end"]),
        (3, &json!("root"))
    );
    let p = fx.plan(&["--max-stack", "2"]);
    assert_eq!(
        (p["stack"].as_array().unwrap().len(), &p["stack_end"]),
        (2, &json!("limit"))
    );
}

#[test]
fn base_ignores_the_limit_and_hides_older_commits() {
    let fx = Fx::new();
    let (shas, t) = fx.many(14);
    fx.stage1("a.txt", &replace_line(&t, 5, "fixed"));
    let p = fx.plan(&["--base", &shas[1]]);
    assert_eq!(p["stack"].as_array().unwrap().len(), 12);
    assert_eq!(
        (&p["stack_end"], &p["base"]),
        (&json!("base"), &json!(shas[1]))
    );
    assert_eq!(hunk(&p, 0)["target"]["sha"], shas[2]);
    let p = fx.plan(&["--base", &shas[2]]);
    assert_eq!(hunk(&p, 0)["reason"], "outside-range");
}

#[test]
fn base_may_be_a_revision_expression() {
    let fx = Fx::new();
    let (shas, t) = fx.many(4);
    fx.stage1("a.txt", &replace_line(&t, 5, "fixed"));
    let p = fx.plan(&["--base", "HEAD~2"]);
    assert_eq!(hunk(&p, 0)["target"]["sha"], shas[2]);
    assert_eq!(p["base"], "HEAD~2", "the base is reported as given");
}

#[test]
fn bad_base_is_refused() {
    let fx = Fx::new();
    let (shas, t) = fx.many(3);
    fx.g(&["checkout", "-q", "-b", "other", &shas[0]]);
    let c = fx.commit1("b.txt", "b\n", "feat: other branch");
    fx.g(&["checkout", "-q", "main"]);
    fx.stage1("a.txt", &replace_line(&t, 3, "fixed"));
    fx.error("bad-base", &["--base", "no-such-rev"]);
    fx.error("bad-base", &["--base", &c]);
    fx.error("bad-base", &["--base=-x"]);
}

fn merge_fixture(fx: &Fx) {
    fx.commit1("a.txt", &l(6), "feat: one");
    fx.g(&["checkout", "-q", "-b", "side"]);
    fx.commit1("s.txt", "s\n", "feat: side");
    fx.g(&["checkout", "-q", "main"]);
    fx.commit1("m.txt", "m\n", "feat: main");
    fx.g(&["merge", "-q", "--no-ff", "-m", "merge side", "side"]);
    fx.g(&["branch", "-q", "-D", "side"]);
}

#[test]
fn merge_commit_ends_the_stack() {
    let fx = Fx::new();
    merge_fixture(&fx);
    let t3 = replace_line(&l(6), 5, "by c3");
    let c3 = fx.commit1("a.txt", &t3, "feat: after merge");
    fx.stage1("a.txt", &replace_line(&t3, 2, "fixed"));
    let p = fx.plan(&[]);
    assert_eq!(p["stack_end"], "merge");
    assert_eq!(stack_shas(&p), [c3.as_str()]);
    assert_eq!(hunk(&p, 0)["reason"], "outside-range");
}

#[test]
fn a_hidden_merge_commit_after_the_stack_is_named() {
    let fx = Fx::new();
    merge_fixture(&fx);
    fx.g(&["branch", "keep"]);
    fx.commit1("b.txt", "b\n", "feat: after merge");
    fx.stage1("b.txt", "b2\n");
    let p = fx.plan(&[]);
    assert_eq!(
        (&p["stack_end"], p["stack"].as_array().unwrap().len()),
        (&json!("merge"), 1)
    );
}

#[test]
fn foreign_commit_in_the_middle_ends_the_stack_and_is_classified() {
    let fx = Fx::new();
    let t = l(8);
    fx.commit1("a.txt", &t, "feat: mine");
    let t2 = replace_line(&t, 2, "l02 foreign");
    fx.commit1_by("a.txt", &t2, "feat: theirs", OTHER);
    let t3 = replace_line(&t2, 6, "l06 mine");
    let c3 = fx.commit1("a.txt", &t3, "feat: mine again");
    fx.stage1(
        "a.txt",
        &replace_line(&replace_line(&t3, 6, "fixed6"), 2, "fixed2"),
    );
    let p = fx.plan(&[]);
    assert_eq!(p["stack_end"], "foreign");
    assert_eq!(stack_shas(&p), [c3.as_str()]);
    assert_eq!(reasons(&p), [json!("foreign"), Value::Null]);
    assert_eq!(hunk(&p, 0)["target"], Value::Null);
    assert_eq!(hunk(&p, 1)["target"]["sha"], c3);
}

#[test]
fn same_name_other_email_is_foreign() {
    let fx = Fx::new();
    let t = l(4);
    fx.commit1_by("a.txt", &t, "feat: x", ("Test", "other@example.invalid"));
    fx.commit1("b.txt", "b\n", "feat: y");
    fx.stage1("a.txt", &replace_line(&t, 2, "f"));
    let p = fx.plan(&[]);
    assert_eq!(
        (&p["stack_end"], &hunk(&p, 0)["reason"]),
        (&json!("foreign"), &json!("foreign"))
    );
}

#[test]
fn branch_name_and_ref_are_masked() {
    let fx = Fx::new();
    let t = l(4);
    fx.commit1("a.txt", &t, "feat: one");
    fx.g(&["checkout", "-q", "-b", "feat/\u{202e}x"]);
    fx.commit1("b.txt", "b\n", "feat: two");
    fx.stage1("a.txt", &replace_line(&t, 2, "f"));
    let p = fx.plan(&[]);
    assert_eq!(
        (&p["branch"], &p["rewrite"]["ref"]),
        (&json!("feat/?x"), &json!("refs/heads/feat/?x"))
    );
    let o = fx.run(&[]);
    assert!(!o.out.contains('\u{202e}'), "{}", o.out);
}

#[test]
fn head_by_another_author_is_not_owned() {
    let fx = Fx::new();
    fx.commit1_by("a.txt", &l(4), "feat: theirs", OTHER);
    fx.stage1("a.txt", &format!("{}x\n", l(4)));
    fx.error("not-owned", &[]);
}

#[test]
fn published_head_is_refused() {
    let fx = Fx::new();
    fx.commit1("a.txt", &l(4), "feat: one");
    fx.publish("HEAD");
    fx.stage1("a.txt", &format!("{}x\n", l(4)));
    fx.error("published", &[]);
}

#[test]
fn published_ancestors_end_the_stack_and_are_classified() {
    let fx = Fx::new();
    let t = l(8);
    let c1 = fx.commit1("a.txt", &t, "feat: one");
    fx.publish(&c1);
    let t2 = replace_line(&t, 6, "l06 mine");
    let c2 = fx.commit1("a.txt", &t2, "feat: two");
    fx.stage1("a.txt", &replace_line(&replace_line(&t2, 6, "f6"), 2, "f2"));
    let p = fx.plan(&[]);
    assert_eq!(p["stack_end"], "published");
    assert_eq!(stack_shas(&p), [c2.as_str()]);
    assert_eq!(reasons(&p), [json!("published"), Value::Null]);
}

#[test]
fn published_wins_over_foreign_in_the_priority() {
    let fx = Fx::new();
    let t = l(8);
    let c1 = fx.commit1_by("a.txt", &t, "feat: one", OTHER);
    fx.publish(&c1);
    fx.commit1("b.txt", "b\n", "feat: two");
    fx.stage1("a.txt", &replace_line(&t, 2, "f2"));
    assert_eq!(hunk(&fx.plan(&[]), 0)["reason"], "published");
}

#[test]
fn commits_of_another_local_branch_end_the_stack() {
    let fx = Fx::new();
    let t = l(8);
    fx.commit1("a.txt", &t, "feat: one");
    fx.g(&["branch", "other"]);
    let t2 = replace_line(&t, 6, "l06 mine");
    let c2 = fx.commit1("a.txt", &t2, "feat: two");
    fx.stage1("a.txt", &replace_line(&t2, 6, "f6"));
    let p = fx.plan(&[]);
    assert_eq!(p["stack_end"], "branch");
    assert_eq!(stack_shas(&p), [c2.as_str()]);
    fx.stage1("a.txt", &replace_line(&replace_line(&t2, 6, "f6"), 2, "f2"));
    assert_eq!(
        reasons(&fx.plan(&[])),
        [json!("outside-range"), Value::Null]
    );
}

#[test]
fn explicit_base_does_not_hide_other_local_branches() {
    let fx = Fx::new();
    let t = l(8);
    let c1 = fx.commit1("a.txt", &t, "feat: one");
    let t2 = replace_line(&t, 2, "l02 mine");
    let c2 = fx.commit1("a.txt", &t2, "feat: two");
    fx.g(&["branch", "other"]);
    fx.commit1("b.txt", "b\n", "feat: three");
    fx.stage1("a.txt", &replace_line(&t2, 2, "f2"));
    let p = fx.plan(&[]);
    assert_eq!(
        p["stack"][0]["subject"], "feat: three",
        "without --base the branch hides c2 and c1, and c3 ends the stack"
    );
    assert_eq!(hunk(&p, 0)["reason"], "outside-range");
    let p = fx.plan(&["--base", &c1]);
    assert_eq!(hunk(&p, 0)["target"]["sha"], c2);
}

#[test]
fn the_current_branch_does_not_hide_itself_and_a_branch_at_head_hides_everything() {
    let fx = Fx::new();
    let t = l(8);
    fx.commit1("a.txt", &t, "feat: one");
    fx.g(&["branch", "twin"]);
    fx.stage1("a.txt", &replace_line(&t, 2, "f2"));
    // a second branch at HEAD: every commit is reachable from it, the stack is empty and nothing can be a target
    let p = fx.plan(&[]);
    assert_eq!(p["stack"], json!([]));
    assert_eq!(p["stack_end"], "branch");
    assert_eq!(
        hunk(&p, 0)["reason"],
        "outside-range",
        "the removed line comes from a commit beyond the stack"
    );
    assert_eq!(p["rewrite"]["count"], 0);
}
