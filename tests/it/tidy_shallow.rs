//! `stw tidy plan` in a shallow clone: a commit at the boundary has no patch to read, so the plan is made, no proof is
//! taken from the commits behind the boundary and a branch that cannot be judged is `review`, never `delete`.
use crate::common::*;
use gix::ObjectId;
use serde_json::Value;
use std::process::Command;
use stw::github::AnyPr;
use stw::repo::RepoRef;
use stw::tidy::{
    plan_repo_with_opts, unmatched_commits, PatchCache, PlanOpts, PrProof, RepoPlan, Similarity,
};

const T0: i64 = 1_700_000_000;
const DAY: i64 = 86_400;

fn mkc(
    fx: &Fx,
    parent: Option<ObjectId>,
    edits: &[(&str, Option<&str>)],
    msg: &str,
    secs: i64,
) -> ObjectId {
    let s = Stamp::new("T", "t@example.com", secs);
    fx.commit_at(parent, edits, msg, s, s)
}

/// The history of a shallow clone with two boundaries, `c0` and `b1`; the parents of both (`old`, `old2`) are not in the
/// object database. `main` is `c0 <- m1 <- m2`. Branches:
/// `deep` (`b1 <- e1`, `e1` has the patch of `m2`; `b1` is a boundary and not in `main`), `orphan` (a root commit with
/// no partner), `fork` (`c0 <- f1`, no partner), `fork_equal` (`c0 <- f2`, the patch of `m1`), `merged` (the tip is `m1`),
/// `mhidden` (`e2` with the patch of `m2` on a merge commit that is a boundary; both parents are gone), `cut` (`p1 <- p2`,
/// `p2` has the patch of `m2` and is listed in the shallow file although `p1` is there, as when another ref holds `p1`)
/// and `deep_gone` (the tip of `deep` with an upstream that is gone).
fn shallow_world(shallow: bool) -> Fx {
    let fx = Fx::new();
    let old = mkc(&fx, None, &[("z.txt", Some("old\n"))], "old", T0 - 40 * DAY);
    let old2 = mkc(
        &fx,
        None,
        &[("y.txt", Some("old\n"))],
        "old2",
        T0 - 41 * DAY,
    );
    let c0 = mkc(
        &fx,
        Some(old),
        &[("g0.txt", Some("base\n"))],
        "c0",
        T0 - 9 * DAY,
    );
    let m1 = mkc(
        &fx,
        Some(c0),
        &[("m1.txt", Some("m1\n"))],
        "feat: m1",
        T0 - 8 * DAY,
    );
    let m2 = mkc(
        &fx,
        Some(m1),
        &[("landed.txt", Some("L\n"))],
        "feat: landed",
        T0 - 7 * DAY,
    );
    let b1 = mkc(
        &fx,
        Some(old2),
        &[("b.txt", Some("b\n"))],
        "b1",
        T0 - 20 * DAY,
    );
    let e1 = mkc(
        &fx,
        Some(b1),
        &[("landed.txt", Some("L\n"))],
        "feat: landed",
        T0 - 6 * DAY,
    );
    let o1 = mkc(
        &fx,
        None,
        &[("o.txt", Some("o\n"))],
        "orphan work",
        T0 - 5 * DAY,
    );
    let s1 = mkc(
        &fx,
        None,
        &[("s1.txt", Some("s1\n"))],
        "secret one",
        T0 - 30 * DAY,
    );
    let s2 = mkc(
        &fx,
        None,
        &[("s2.txt", Some("s2\n"))],
        "secret two",
        T0 - 29 * DAY,
    );
    let st = Stamp::new("T", "t@example.com", T0 - 12 * DAY);
    let mg = fx.commit_at_parents(
        vec![s1, s2],
        Some(s1),
        &[("s2.txt", Some("s2\n"))],
        "merge secret",
        st,
        st,
    );
    let e2 = mkc(
        &fx,
        Some(mg),
        &[("landed.txt", Some("L\n"))],
        "feat: landed",
        T0 - 2 * DAY,
    );
    let p1 = mkc(
        &fx,
        None,
        &[("hidden.txt", Some("h\n"))],
        "hidden",
        T0 - 15 * DAY,
    );
    let p2 = mkc(
        &fx,
        Some(p1),
        &[("landed.txt", Some("L\n"))],
        "feat: landed",
        T0 - 2 * DAY,
    );
    let f1 = mkc(
        &fx,
        Some(c0),
        &[("f.txt", Some("f\n"))],
        "fork work",
        T0 - 4 * DAY,
    );
    let f2 = mkc(
        &fx,
        Some(c0),
        &[("m1.txt", Some("m1\n"))],
        "feat: m1",
        T0 - 3 * DAY,
    );
    fx.remote("main", m2);
    fx.origin_head("main");
    fx.branch("main", m2);
    fx.set_head("main");
    fx.checkout_head();
    for (n, id) in [
        ("deep", e1),
        ("orphan", o1),
        ("fork", f1),
        ("fork_equal", f2),
        ("merged", m1),
        ("mhidden", e2),
        ("cut", p2),
        ("deep_gone", e1),
    ] {
        fx.branch(n, id);
    }
    fx.track("deep_gone");
    if shallow {
        let git = fx.path.join(".git");
        std::fs::write(git.join("shallow"), format!("{b1}\n{c0}\n{mg}\n{p2}\n")).unwrap();
        for gone in [old, old2, s1, s2] {
            let hex = gone.to_string();
            std::fs::remove_file(git.join("objects").join(&hex[..2]).join(&hex[2..])).unwrap();
        }
    }
    fx
}

fn opts() -> PlanOpts {
    PlanOpts {
        backup_days: 7,
        now: T0,
        similarity: Some(Similarity::default()),
        rebase: true,
    }
}

fn plan_of(fx: &Fx) -> RepoPlan {
    plan_with_prs(fx, None)
}

fn plan_with_prs(fx: &Fx, prs: Option<Vec<AnyPr>>) -> RepoPlan {
    let p = plan_repo_with_opts(
        &RepoRef {
            name: "fx".into(),
            path: fx.path.clone(),
        },
        PrProof {
            prs,
            note: "off".into(),
            api: Default::default(),
        },
        &opts(),
    );
    assert_eq!(
        p.error, None,
        "a shallow clone is no error of the repository"
    );
    p
}

fn get<'a>(p: &'a RepoPlan, branch: &str) -> &'a stw::tidy::BranchPlan {
    p.branches
        .iter()
        .find(|b| b.branch == branch)
        .unwrap_or_else(|| panic!("no branch {branch}"))
}

fn json_of(fx: &Fx, p: RepoPlan) -> Value {
    stw::tidy::to_json(
        &path_str(&fx.path),
        &stw::github::GithubState {
            client: None,
            note: None,
        },
        &[p],
    )
}

fn branch_json<'a>(v: &'a Value, branch: &str) -> &'a Value {
    v["repos"][0]["branches"]
        .as_array()
        .unwrap()
        .iter()
        .find(|b| b["branch"] == branch)
        .unwrap()
}

#[test]
fn a_shallow_clone_gives_a_plan_not_an_error_and_the_boundary_free_branches_keep_their_proofs() {
    let fx = shallow_world(true);
    let p = plan_of(&fx);
    assert_eq!(p.branches.len(), 8);
    let proof = |b: &str| (get(&p, b).decision, get(&p, b).proof);
    assert_eq!(proof("merged"), ("delete", "merged-ancestor"));
    assert_eq!(proof("fork_equal"), ("delete", "equal-patches"));
    assert_eq!(proof("fork"), ("keep", "unmerged"));
}

#[test]
fn a_branch_that_reaches_the_boundary_is_review_with_the_proof_shallow_history() {
    let fx = shallow_world(true);
    let p = plan_of(&fx);
    let b = get(&p, "deep");
    assert_eq!((b.decision, b.proof), ("review", "shallow-history"));
    assert_eq!(
        b.detail,
        "1 of 2 commit(s) not in refs/remotes/origin/main reach the shallow boundary of this clone: their patches are unknown, so the branch cannot be compared with refs/remotes/origin/main (git fetch --unshallow gives the full history)"
    );
    // no stage takes a proof from the commits that could be read
    assert!(b.similarity.is_none() && b.rebase.is_none());
}

#[test]
fn a_branch_without_a_partner_in_the_part_of_the_base_that_is_there_is_not_kept_as_unmerged() {
    let fx = shallow_world(true);
    let p = plan_of(&fx);
    let b = get(&p, "orphan");
    assert_eq!((b.decision, b.proof), ("review", "shallow-history"));
    assert_eq!(
        b.detail,
        "1 of 1 commit(s) not in refs/remotes/origin/main have no equal patch in the part of refs/remotes/origin/main that this shallow clone holds: a partner may lie behind the boundary (git fetch --unshallow gives the full history)"
    );
}

#[test]
fn the_plan_json_names_what_is_missing_per_branch() {
    let fx = shallow_world(true);
    let v = json_of(&fx, plan_of(&fx));
    assert_eq!(
        branch_json(&v, "deep")["shallow"],
        serde_json::json!({"unreadable_commits": 1, "without_partner": 0, "base_cut": false})
    );
    assert_eq!(
        branch_json(&v, "orphan")["shallow"],
        serde_json::json!({"unreadable_commits": 0, "without_partner": 1, "base_cut": true})
    );
    assert_eq!(branch_json(&v, "fork")["shallow"], Value::Null);
    assert_eq!(branch_json(&v, "merged")["shallow"], Value::Null);
}

#[test]
fn the_explained_text_says_which_part_of_the_history_is_missing() {
    let fx = shallow_world(true);
    let t = stw::tidy::render_explained(&[plan_of(&fx)], true);
    assert!(
        t.contains("why: review: 1 of the 2 commit(s) not in origin/main reach the shallow boundary: their patches are unknown"),
        "{t}"
    );
    assert!(
        t.contains("why: review: 1 of the 1 commit(s) not in origin/main have a patch without a partner in the part of it this clone holds"),
        "{t}"
    );
}

#[test]
fn a_full_clone_is_unchanged() {
    let fx = shallow_world(false);
    let p = plan_of(&fx);
    let v = json_of(&fx, p.clone());
    for b in ["deep", "orphan", "fork", "fork_equal", "merged"] {
        assert_eq!(branch_json(&v, b)["shallow"], Value::Null, "{b}");
        assert_ne!(get(&p, b).proof, "shallow-history", "{b}");
    }
    assert_eq!(get(&p, "deep").proof, "unmerged");
}

fn git(dir: &std::path::Path, args: &[&str]) {
    let o = Command::new("git")
        .arg("-C")
        .arg(dir)
        .args(args)
        .env("GIT_CONFIG_GLOBAL", "/dev/null")
        .env("GIT_CONFIG_SYSTEM", "/dev/null")
        .output()
        .unwrap();
    assert!(
        o.status.success(),
        "git {args:?}: {}",
        String::from_utf8_lossy(&o.stderr)
    );
}

#[test]
fn a_real_git_clone_with_depth_gives_a_plan_through_the_cli() {
    let tmp = scratch();
    let origin = tmp.path().join("origin");
    std::fs::create_dir(&origin).unwrap();
    git(&origin, &["init", "-q", "-b", "main"]);
    git(&origin, &["config", "user.name", "T"]);
    git(&origin, &["config", "user.email", "t@example.com"]);
    let add = |name: &str, msg: &str| {
        std::fs::write(origin.join(name), name).unwrap();
        git(&origin, &["add", name]);
        git(&origin, &["commit", "-q", "-m", msg]);
    };
    add("a", "c1");
    add("b", "c2");
    git(&origin, &["branch", "side"]);
    for (n, m) in [("c", "c3"), ("d", "c4"), ("e", "c5")] {
        add(n, m);
    }
    git(&origin, &["checkout", "-q", "side"]);
    add("s", "s1");
    git(&origin, &["checkout", "-q", "main"]);
    let clone = tmp.path().join("clone");
    let url = format!("file://{}", origin.display());
    let o = Command::new("git")
        .args(["clone", "-q", "--depth", "2", "--no-single-branch", &url])
        .arg(&clone)
        .env("GIT_CONFIG_GLOBAL", "/dev/null")
        .env("GIT_CONFIG_SYSTEM", "/dev/null")
        .output()
        .unwrap();
    assert!(o.status.success(), "{}", String::from_utf8_lossy(&o.stderr));
    git(&clone, &["branch", "side", "origin/side"]);
    assert!(clone.join(".git/shallow").is_file());
    let out = stw_stdout(&[
        "tidy",
        "plan",
        "--offline",
        "--all",
        "--json",
        "--root",
        clone.to_str().unwrap(),
    ]);
    let v: Value = serde_json::from_str(&out).unwrap();
    assert_eq!(v["repos"][0]["error"], Value::Null, "{out}");
    let side = branch_json(&v, "side");
    assert_eq!(side["decision"], "review", "{out}");
    assert_eq!(side["proof"], "shallow-history", "{out}");
}

#[test]
fn a_merged_pull_request_with_exactly_this_head_still_deletes_in_a_shallow_clone() {
    let fx = shallow_world(true);
    let pr = AnyPr {
        head_ref: "deep".into(),
        head_sha: fx.tip("deep").to_string(),
        number: 7,
        state: "MERGED",
        ..Default::default()
    };
    let p = plan_with_prs(&fx, Some(vec![pr]));
    let b = get(&p, "deep");
    assert_eq!(
        (b.decision, b.proof),
        ("delete", "merged-pull-request-head")
    );
    assert_eq!(b.shallow.map(|g| g.unreadable_commits), Some(1));
}

fn unmatched_of(fx: &Fx, base: ObjectId, tip: ObjectId) -> stw::tidy::Unmatched {
    let repo = fx.repo();
    unmatched_commits(&repo, &mut PatchCache::default(), base, tip).unwrap()
}

fn make_shallow(fx: &Fx, boundaries: &[ObjectId], gone: &[ObjectId]) {
    let git = fx.path.join(".git");
    let list: String = boundaries.iter().map(|b| format!("{b}\n")).collect();
    std::fs::write(git.join("shallow"), list).unwrap();
    for g in gone {
        let hex = g.to_string();
        std::fs::remove_file(git.join("objects").join(&hex[..2]).join(&hex[2..])).unwrap();
    }
}

#[test]
fn a_boundary_of_the_base_met_before_the_last_partner_is_no_cut() {
    // main = merge(a, b), `a` is a boundary (its parent is gone), `b <- c`, and `c` has the patch of the branch commit
    let fx = Fx::new();
    let gone = mkc(&fx, None, &[("z.txt", Some("z\n"))], "gone", T0 - 40 * DAY);
    let a = mkc(&fx, Some(gone), &[("a.txt", Some("a\n"))], "a", T0 - DAY);
    let c = mkc(&fx, None, &[("p.txt", Some("p\n"))], "c", T0 - 9 * DAY);
    let b = mkc(&fx, Some(c), &[("b.txt", Some("b\n"))], "b", T0 - 8 * DAY);
    let m = fx.commit_at_parents(
        vec![a, b],
        Some(a),
        &[("b.txt", Some("b\n"))],
        "merge",
        Stamp::new("T", "t@example.com", T0 - 2 * DAY),
        Stamp::new("T", "t@example.com", T0 - 2 * DAY),
    );
    let r1 = mkc(&fx, None, &[("p.txt", Some("p\n"))], "r1", T0 - 5 * DAY);
    let r2 = mkc(&fx, None, &[("q.txt", Some("q\n"))], "r2", T0 - 5 * DAY);
    make_shallow(&fx, &[a], &[gone]);
    let u = unmatched_of(&fx, m, r1);
    assert_eq!(
        (u.with_patch, u.commits.len(), u.unreadable.len()),
        (1, 0, 0)
    );
    assert!(
        !u.base_cut,
        "every commit has its partner: nothing hides one"
    );
    assert!(u.all_matched());
    // a commit without a partner makes the same walk reach the boundary: a partner may lie behind it
    let u = unmatched_of(&fx, m, r2);
    assert_eq!(
        (u.with_patch, u.commits.len(), u.unreadable.len()),
        (1, 1, 0)
    );
    assert!(u.base_cut);
    assert!(!u.all_matched());
}

#[test]
fn a_boundary_commit_of_the_branch_is_unreadable_and_blocks_the_proof_a_merge_too() {
    let fx = Fx::new();
    let gone = mkc(&fx, None, &[("z.txt", Some("z\n"))], "gone", T0 - 40 * DAY);
    let gone2 = mkc(&fx, None, &[("y.txt", Some("y\n"))], "gone2", T0 - 41 * DAY);
    let base = mkc(&fx, None, &[("p.txt", Some("p\n"))], "base", T0 - 9 * DAY);
    let edge = mkc(
        &fx,
        Some(gone),
        &[("e.txt", Some("e\n"))],
        "edge",
        T0 - 6 * DAY,
    );
    let s = Stamp::new("T", "t@example.com", T0 - 5 * DAY);
    let merge = fx.commit_at_parents(vec![gone, gone2], Some(gone), &[], "merge", s, s);
    let gone3 = mkc(&fx, None, &[("x.txt", Some("x\n"))], "gone3", T0 - 42 * DAY);
    let unlisted = fx.commit_at_parents(vec![gone3, gone], Some(gone3), &[], "unlisted", s, s);
    make_shallow(&fx, &[edge, merge], &[gone, gone2, gone3]);
    let u = unmatched_of(&fx, base, edge);
    assert_eq!(u.unreadable, vec![edge]);
    assert_eq!(u.with_patch, 0);
    assert!(!u.all_matched());
    // a merge commit listed in the shallow file hides what is behind it: it blocks the proof like any boundary commit
    let u = unmatched_of(&fx, base, merge);
    assert_eq!(
        (u.with_patch, u.commits.len(), u.unreadable),
        (0, 0, vec![merge])
    );
    // a merge commit that is not listed but has a parent that is gone hides what is behind it as well
    let u = unmatched_of(&fx, base, unlisted);
    assert_eq!(u.unreadable, vec![unlisted]);
}

#[test]
fn a_merge_commit_at_the_boundary_hides_the_commits_behind_it() {
    let fx = shallow_world(true);
    let p = plan_of(&fx);
    let b = get(&p, "mhidden");
    assert_eq!((b.decision, b.proof), ("review", "shallow-history"));
    assert_eq!(b.shallow.map(|g| g.unreadable_commits), Some(1));
}

#[test]
fn a_commit_listed_in_the_shallow_file_is_a_boundary_although_its_parent_is_there() {
    let fx = shallow_world(true);
    let p = plan_of(&fx);
    let b = get(&p, "cut");
    assert_eq!((b.decision, b.proof), ("review", "shallow-history"));
    // `git log` stops at `p2`: `p1` is not a commit of the branch in this clone
    assert_eq!(b.unique, 1);
    assert_eq!(b.shallow.map(|g| g.unreadable_commits), Some(1));
}

#[test]
fn a_gone_upstream_or_a_moved_pull_request_stays_what_it_was() {
    let fx = shallow_world(true);
    fx.branch("deep_moved", fx.tip("deep"));
    let pr = AnyPr {
        head_ref: "deep_moved".into(),
        head_sha: "0".repeat(40),
        number: 9,
        state: "MERGED",
        ..Default::default()
    };
    let p = plan_with_prs(&fx, Some(vec![pr]));
    let proof = |b: &str| (get(&p, b).decision, get(&p, b).proof);
    assert_eq!(proof("deep_gone"), ("review", "upstream-gone"));
    assert_eq!(
        proof("deep_moved"),
        ("review", "pull-request-merged-branch-moved")
    );
    // the gap stays in the plan
    assert_eq!(
        get(&p, "deep_gone").shallow.map(|g| g.unreadable_commits),
        Some(1)
    );
}

/// `b0` (a boundary, listed) has the parent `y`, which is in the object database; `t1` is on `y`; `a2 <- a1` is a chain.
fn listed_parent_world() -> (Fx, ObjectId, ObjectId, ObjectId, ObjectId, ObjectId) {
    let fx = Fx::new();
    let y = mkc(&fx, None, &[("y.txt", Some("y\n"))], "y", T0 - 9 * DAY);
    let b0 = mkc(
        &fx,
        Some(y),
        &[("b0.txt", Some("b0\n"))],
        "b0",
        T0 - 8 * DAY,
    );
    let t1 = mkc(
        &fx,
        Some(y),
        &[("t1.txt", Some("t1\n"))],
        "t1",
        T0 - 7 * DAY,
    );
    let a1 = mkc(&fx, None, &[("a1.txt", Some("a1\n"))], "a1", T0 - 6 * DAY);
    let a2 = mkc(
        &fx,
        Some(a1),
        &[("a2.txt", Some("a2\n"))],
        "a2",
        T0 - 5 * DAY,
    );
    make_shallow(&fx, &[b0, a2], &[]);
    (fx, y, b0, t1, a1, a2)
}

#[test]
fn the_range_is_the_one_git_sees_where_the_shallow_file_lists_a_commit_with_its_parent_there() {
    let (fx, y, b0, t1, _, _) = listed_parent_world();
    let repo = fx.repo();
    // git: `b0` has no parents, so `y` is reachable from `t1` only and is not hidden by `b0`
    let mut got = stw::repo::range_commits(&repo, t1, b0).unwrap();
    got.sort();
    let mut want = vec![t1, y];
    want.sort();
    assert_eq!(got, want);
    // and `b0` is not reduced by what `t1` reaches
    assert_eq!(stw::repo::range_commits(&repo, b0, t1).unwrap(), vec![b0]);
    // nothing is listed in a full clone: the plain walk
    std::fs::remove_file(fx.path.join(".git/shallow")).unwrap();
    assert_eq!(stw::repo::range_commits(&repo, b0, t1).unwrap(), vec![b0]);
}

#[test]
fn the_first_parent_index_of_the_base_ends_at_a_listed_commit() {
    use stw::tidy_rebase::{RebaseIndex, Stop};
    let (fx, _, _, _, a1, a2) = listed_parent_world();
    let repo = fx.repo();
    let idx = RebaseIndex::build(&repo, a2, None, 100).unwrap();
    assert_eq!(idx.len(), 1);
    assert_eq!(idx.stop(), Stop::Missing(a1));
}

#[test]
fn a_listed_merge_commit_with_both_parents_there_is_a_boundary_and_no_delete() {
    // `m1 <- m2` is main; `mrg` is `e` (the patch of `m2`) on a merge commit of `q1` and `q2` that `.git/shallow` lists
    // although both parents are in the object database: git does not follow them, the plan must not either
    let fx = Fx::new();
    let m1 = mkc(&fx, None, &[("m1.txt", Some("m1\n"))], "m1", T0 - 8 * DAY);
    let m2 = mkc(
        &fx,
        Some(m1),
        &[("landed.txt", Some("L\n"))],
        "feat: landed",
        T0 - 7 * DAY,
    );
    let q1 = mkc(&fx, None, &[("q1.txt", Some("q1\n"))], "q1", T0 - 30 * DAY);
    let q2 = mkc(&fx, None, &[("q2.txt", Some("q2\n"))], "q2", T0 - 29 * DAY);
    let st = Stamp::new("T", "t@example.com", T0 - 12 * DAY);
    let mg = fx.commit_at_parents(
        vec![q1, q2],
        Some(q1),
        &[("q2.txt", Some("q2\n"))],
        "merge",
        st,
        st,
    );
    let e = mkc(
        &fx,
        Some(mg),
        &[("landed.txt", Some("L\n"))],
        "feat: landed",
        T0 - 2 * DAY,
    );
    fx.remote("main", m2);
    fx.origin_head("main");
    fx.branch("main", m2);
    fx.set_head("main");
    fx.checkout_head();
    fx.branch("mrg", e);
    make_shallow(&fx, &[m1, mg], &[]);
    let p = plan_of(&fx);
    let b = get(&p, "mrg");
    assert_eq!((b.decision, b.proof), ("review", "shallow-history"));
    assert_eq!(b.shallow.map(|g| g.unreadable_commits), Some(1));
}
