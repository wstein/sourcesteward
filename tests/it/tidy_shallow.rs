//! `stw tidy plan` in a shallow clone: a commit at the boundary has no patch to read, so the plan is made, no proof is
//! taken from the commits behind the boundary and a branch that cannot be judged is `review`, never `delete`.
use crate::common::*;
use gix::ObjectId;
use serde_json::Value;
use std::process::Command;
use stw::repo::RepoRef;
use stw::tidy::{plan_repo_with_opts, PlanOpts, PrProof, RepoPlan, Similarity};

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
/// no partner), `fork` (`c0 <- f1`, no partner), `fork_equal` (`c0 <- f2`, the patch of `m1`), `merged` (the tip is `m1`).
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
    ] {
        fx.branch(n, id);
    }
    if shallow {
        let git = fx.path.join(".git");
        std::fs::write(git.join("shallow"), format!("{b1}\n{c0}\n")).unwrap();
        for gone in [old, old2] {
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
    let p = plan_repo_with_opts(
        &RepoRef {
            name: "fx".into(),
            path: fx.path.clone(),
        },
        PrProof {
            prs: None,
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
    assert_eq!(p.branches.len(), 5);
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
        "1 commit(s) not in refs/remotes/origin/main have no equal patch in the part of refs/remotes/origin/main that this shallow clone holds: a partner may lie behind the boundary (git fetch --unshallow gives the full history)"
    );
}

#[test]
fn the_plan_json_names_what_is_missing_per_branch() {
    let fx = shallow_world(true);
    let v = json_of(&fx, plan_of(&fx));
    assert_eq!(
        branch_json(&v, "deep")["shallow"],
        serde_json::json!({"unreadable_commits": 1, "base_cut": false})
    );
    assert_eq!(
        branch_json(&v, "orphan")["shallow"],
        serde_json::json!({"unreadable_commits": 0, "base_cut": true})
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
        t.contains("why: review: 1 commit(s) not in origin/main have a patch without a partner in the part of it this clone holds"),
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
