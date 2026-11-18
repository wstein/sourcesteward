//! The similarity stage of `stw tidy plan` (evidence class `similar-patches`, decision `review`, never `delete`).
use crate::common::*;
use gix::ObjectId;
use stw::repo::RepoRef;
use stw::tidy::{plan_repo_with_opts, unmatched_commits, PatchCache, PlanOpts, PrProof, RepoPlan};

/// A change of 20 lines (`first..first+20` read `<tag><i>`, the lines in `odd` read `odd<i>`): 40 keys.
fn text(tag: &str, odd: &[usize]) -> String {
    (0..30)
        .map(|i| {
            if odd.contains(&i) {
                format!("odd{i}\n")
            } else if i < 20 {
                format!("{tag}{i}\n")
            } else {
                format!("x{i}\n")
            }
        })
        .collect()
}

/// A second file, to give each branch commit its own change: 20 lines `<tag><i>` replace `x<i>`.
fn file_text(tag: &str, odd: &[usize]) -> String {
    text(tag, odd)
}

struct W {
    fx: Fx,
    c0: ObjectId,
    main: ObjectId,
}

/// `main` is `c0` plus `m1` (a first main commit with an unrelated file); `origin/main` is `main`.
fn world() -> W {
    let fx = Fx::new();
    let c0 = fx.commit(
        None,
        &[
            ("f1.txt", Some(&lines(30, "x"))),
            ("f2.txt", Some(&lines(30, "x"))),
            ("f3.txt", Some(&lines(30, "x"))),
        ],
        "c0",
    );
    let main = c0;
    fx.remote("main", main);
    fx.origin_head("main");
    fx.branch("main", main);
    fx.set_head("main");
    fx.checkout_head();
    W { fx, c0, main }
}

/// Land a copy of `edits` on main as a new commit (`odd` differs in a few lines: a conflict resolution).
fn land(w: &mut W, edits: &[(&str, Option<&str>)], msg: &str) {
    let m = w.fx.commit(Some(w.main), edits, msg);
    w.main = m;
    w.fx.remote("main", m);
    w.fx.branch("main", m);
}

fn rr(w: &W) -> RepoRef {
    RepoRef {
        name: "fx".into(),
        path: w.fx.path.clone(),
    }
}

fn plan_with(w: &W, opts: &PlanOpts) -> RepoPlan {
    plan_repo_with_opts(
        &rr(w),
        PrProof {
            prs: None,
            note: "off".into(),
        },
        opts,
    )
}

fn get<'a>(p: &'a RepoPlan, branch: &str) -> &'a stw::tidy::BranchPlan {
    p.branches
        .iter()
        .find(|b| b.branch == branch)
        .unwrap_or_else(|| panic!("no branch {branch}"))
}

fn unmatched(w: &W, branch: &str) -> (usize, Vec<ObjectId>) {
    let repo = w.fx.repo();
    let mut cache = PatchCache::default();
    let u = unmatched_commits(&repo, &mut cache, w.main, w.fx.tip(branch)).unwrap();
    (u.with_patch, u.commits)
}

#[test]
fn unmatched_commits_are_the_branch_commits_without_a_patch_partner_on_main() {
    let mut w = world();
    let txt = file_text("n", &[]);
    let same = [("f1.txt", Some(txt.as_str()))];
    let a = w.fx.commit(Some(w.c0), &same, "feat: one");
    let b =
        w.fx.commit(Some(a), &[("f2.txt", Some("other\n"))], "feat: two");
    w.fx.branch("half", b);
    w.fx.branch("equal", a);
    w.fx.branch("merged", w.c0);
    land(&mut w, &same, "feat: one");
    // `equal` has one commit and it has a partner; `half` has two and the second has none
    assert_eq!(unmatched(&w, "equal"), (1, vec![]));
    assert_eq!(unmatched(&w, "half"), (2, vec![b]));
    // an ancestor of main has no commit at all
    assert_eq!(unmatched(&w, "merged"), (0, vec![]));
}
