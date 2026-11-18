use crate::common::*;
use stw::github::AnyPr;
use stw::repo::RepoRef;
use stw::tidy::{plan_repo_with, PrProof, RepoPlan};

struct Built {
    fx: Fx,
    m1: gix::ObjectId,
    pr_head: gix::ObjectId,
    moved_head: gix::ObjectId,
}

/// main history c0 -> m1 (edit line 10) -> m2 (edit line 20), origin/main = m2.
fn build() -> Built {
    let fx = Fx::new();
    let c0 = fx.commit(None, &[("a.txt", Some(&lines(30, "x")))], "c0");
    let edit10 = lines(30, "x").replace("x10\n", "TEN\n");
    let m1 = fx.commit(
        Some(c0),
        &[("a.txt", Some(&edit10))],
        "squash-merged change",
    );
    let m2 = fx.commit(
        Some(m1),
        &[("a.txt", Some(&edit10.replace("x20\n", "TWENTY\n")))],
        "later change",
    );
    fx.remote("main", m2);
    fx.origin_head("main");
    fx.branch("main", m2);
    fx.set_head("main");
    fx.checkout_head();

    // ancestor of base
    fx.branch("merged", c0);
    // same patch as m1, created on the old base (rebase/squash merge shape)
    let r1 = fx.commit(
        Some(c0),
        &[("a.txt", Some(&edit10))],
        "same change, other commit",
    );
    assert_ne!(r1, m1);
    fx.branch("rebased", r1);
    // unmerged work
    let u1 = fx.commit(Some(c0), &[("b.txt", Some("new work\n"))], "unmerged");
    fx.branch("unmerged", u1);
    // merged ancestor but checked out in a live worktree / registered in a missing one
    fx.branch("wt-live", c0);
    fx.branch("wt-gone", c0);
    add_worktree(&fx, "wt-live", "wt-live", true, false);
    add_worktree(&fx, "wt-gone", "wt-gone", false, false);
    // open PR
    fx.branch("pr-open", c0);
    // merged PR with exactly this head, content not in base
    let p1 = fx.commit(Some(c0), &[("c.txt", Some("pr work\n"))], "pr work");
    fx.branch("pr-merged", p1);
    // merged PR, branch moved afterwards
    let q1 = fx.commit(Some(c0), &[("d.txt", Some("q\n"))], "q1");
    let q2 = fx.commit(Some(q1), &[("d.txt", Some("q2\n"))], "q2");
    fx.branch("pr-moved", q2);
    // upstream configured, remote ref gone, unique commit
    let g1 = fx.commit(Some(c0), &[("e.txt", Some("g\n"))], "g1");
    fx.branch("gone-up", g1);
    fx.track("gone-up");
    Built {
        fx,
        m1,
        pr_head: p1,
        moved_head: q1,
    }
}

fn plan(b: &Built, with_prs: bool) -> RepoPlan {
    let prs = vec![
        AnyPr {
            head_ref: "pr-open".into(),
            head_sha: "0".repeat(40),
            number: 1,
            state: "OPEN",
            ..Default::default()
        },
        AnyPr {
            head_ref: "pr-merged".into(),
            head_sha: b.pr_head.to_string(),
            number: 2,
            state: "MERGED",
            ..Default::default()
        },
        AnyPr {
            head_ref: "pr-moved".into(),
            head_sha: b.moved_head.to_string(),
            number: 3,
            state: "MERGED",
            ..Default::default()
        },
    ];
    let proof = if with_prs {
        PrProof {
            prs: Some(prs),
            note: "pr-proof-available".into(),
        }
    } else {
        PrProof {
            prs: None,
            note: "pull request proof off (--offline)".into(),
        }
    };
    plan_repo_with(
        &RepoRef {
            name: "fx".into(),
            path: b.fx.path.clone(),
        },
        proof,
    )
}

fn get<'a>(p: &'a RepoPlan, branch: &str) -> &'a stw::tidy::BranchPlan {
    p.branches
        .iter()
        .find(|b| b.branch == branch)
        .unwrap_or_else(|| panic!("no branch {branch}"))
}

#[test]
fn classification_with_pull_request_proof() {
    let b = build();
    let p = plan(&b, true);
    assert!(p.error.is_none(), "{:?}", p.error);
    let expect = [
        ("merged", "delete", "merged-ancestor"),
        ("rebased", "delete", "equal-patches"),
        ("unmerged", "keep", "unmerged"),
        ("wt-live", "keep", "in-worktree"),
        ("wt-gone", "delete", "merged-ancestor"),
        ("pr-open", "keep", "open-pull-request"),
        ("pr-merged", "delete", "merged-pull-request-head"),
        ("pr-moved", "review", "pull-request-merged-branch-moved"),
        ("gone-up", "review", "upstream-gone"),
    ];
    for (br, dec, proof) in expect {
        let bp = get(&p, br);
        assert_eq!((bp.decision, bp.proof), (dec, proof), "{br}: {}", bp.detail);
    }
    assert!(
        p.branches.iter().all(|b| b.branch != "main"),
        "the base branch itself is not listed"
    );
    assert_eq!(p.stale.len(), 1);
    assert_eq!(p.stale[0].id, "wt-gone");
    let _ = b.m1;
}

#[test]
fn offline_has_no_pull_request_proof() {
    let b = build();
    let p = plan(&b, false);
    assert_eq!(get(&p, "pr-merged").decision, "keep");
    assert_eq!(
        get(&p, "pr-open").decision,
        "delete",
        "without PR data an ancestor is still merged"
    );
    assert_eq!(get(&p, "pr-moved").proof, "unmerged");
    assert_eq!(
        get(&p, "rebased").proof,
        "equal-patches",
        "patch proof needs no network"
    );
}

#[test]
fn a_changed_patch_is_not_equal() {
    let b = build();
    // same shape as `rebased` but a different edit: must stay unmerged
    let c0 =
        b.fx.repo()
            .rev_parse_single("refs/heads/merged")
            .unwrap()
            .detach();
    let other = b.fx.commit(
        Some(c0),
        &[("a.txt", Some(&lines(30, "x").replace("x10\n", "NOT-TEN\n")))],
        "different change",
    );
    b.fx.branch("lookalike", other);
    let p = plan(&b, false);
    assert_eq!(
        (get(&p, "lookalike").decision, get(&p, "lookalike").proof),
        ("keep", "unmerged")
    );
}

/// A branch that a stale registration still claims is `delete` (apply releases the registration first); the plan
/// lists the registrations, locked or not, and carries no commands.
#[test]
fn registrations_of_deletable_branches_are_listed_without_commands() {
    let fx = Fx::new();
    let c0 = fx.commit(None, &[("a.txt", Some("a\n"))], "c0");
    fx.remote("main", c0);
    fx.origin_head("main");
    fx.branch("main", c0);
    fx.set_head("main");
    fx.checkout_head();
    fx.branch("stale", c0);
    fx.branch("stale-locked", c0);
    add_worktree(&fx, "stale", "stale", false, false);
    add_worktree(&fx, "stale-locked", "stale-locked", false, true);
    let b = Built {
        fx,
        m1: c0,
        pr_head: c0,
        moved_head: c0,
    };
    let p = plan(&b, false);
    assert_eq!(get(&p, "stale").decision, "delete");
    assert_eq!(get(&p, "stale-locked").decision, "delete");
    let json = stw::tidy::to_json(
        &path_str(&b.fx.path),
        &stw::github::GithubState {
            client: None,
            note: None,
        },
        &[p],
    );
    let stale = json["repos"][0]["stale_worktree_registrations"]
        .as_array()
        .unwrap()
        .clone();
    assert_eq!(stale.len(), 2);
    assert_eq!(stale.iter().filter(|s| s["locked"] == true).count(), 1);
    assert!(
        !json.to_string().contains("next_steps"),
        "no commands in the plan"
    );
}

/// Worktrees below `<repo>/.worktrees/` are the normal place; any other place is marked in the plan, in the
/// text (`outside .worktrees/`) and in JSON (`worktree.outside_worktrees_dir`).
#[test]
fn worktrees_outside_the_worktrees_directory_are_marked() {
    let fx = Fx::new();
    let c0 = fx.commit(None, &[("a.txt", Some("a\n"))], "c0");
    fx.remote("main", c0);
    fx.origin_head("main");
    fx.branch("main", c0);
    fx.set_head("main");
    fx.checkout_head();
    fx.branch("inside", c0);
    fx.branch("outside", c0);
    fx.branch("sibling", c0); // .worktrees-old/x is not .worktrees/x
    add_worktree_at(
        &fx,
        "in",
        "inside",
        &fx.path.join(".worktrees/in"),
        true,
        false,
    );
    add_worktree(&fx, "out", "outside", true, false);
    add_worktree_at(
        &fx,
        "sib",
        "sibling",
        &fx.path.join(".worktrees-old/x"),
        true,
        false,
    );
    let b = Built {
        fx,
        m1: c0,
        pr_head: c0,
        moved_head: c0,
    };
    let p = plan(&b, false);
    let wt = |n: &str| {
        get(&p, n)
            .worktree
            .as_ref()
            .unwrap_or_else(|| panic!("{n}"))
    };
    assert!(!wt("inside").outside);
    assert!(wt("outside").outside);
    assert!(wt("sibling").outside);

    let text = stw::tidy::render_human(std::slice::from_ref(&p), false);
    let line = |n: &str| {
        text.lines()
            .find(|l| l.contains(n) && l.contains("[in-worktree]"))
            .unwrap_or_else(|| panic!("no line for {n}: {text}"))
            .to_string()
    };
    assert!(!line("inside").contains("outside .worktrees/"), "{text}");
    assert!(line("outside").contains("outside .worktrees/"), "{text}");
    assert!(line("sibling").contains("outside .worktrees/"), "{text}");

    let json = stw::tidy::to_json(
        &path_str(&b.fx.path),
        &stw::github::GithubState {
            client: None,
            note: None,
        },
        &[p],
    );
    let branches = json["repos"][0]["branches"].as_array().unwrap();
    let flag = |n: &str| {
        branches.iter().find(|x| x["branch"] == n).unwrap()["worktree"]["outside_worktrees_dir"]
            .clone()
    };
    assert_eq!(flag("inside"), false);
    assert_eq!(flag("outside"), true);
}

/// `<repo>/.worktrees` may be a symbolic link; a checkout registered under its target is still inside.
#[test]
fn a_worktrees_directory_behind_a_symbolic_link_counts_as_inside() {
    let fx = Fx::new();
    let c0 = fx.commit(None, &[("a.txt", Some("a\n"))], "c0");
    fx.remote("main", c0);
    fx.origin_head("main");
    fx.branch("main", c0);
    fx.set_head("main");
    fx.checkout_head();
    fx.branch("linked", c0);
    let elsewhere = scratch();
    std::os::unix::fs::symlink(elsewhere.path(), fx.path.join(".worktrees")).unwrap();
    let target = std::fs::canonicalize(elsewhere.path()).unwrap().join("l");
    add_worktree_at(&fx, "l", "linked", &target, true, false);
    let b = Built {
        fx,
        m1: c0,
        pr_head: c0,
        moved_head: c0,
    };
    let p = plan(&b, false);
    assert!(!get(&p, "linked").worktree.as_ref().unwrap().outside);
}

/// The plan's summary counts what `apply` does: a clean worktree goes with its branch ("delete with their
/// worktree"); a dirty one, and a branch that is kept anyway, stays and is counted as kept.
#[test]
fn the_summary_counts_what_apply_would_do_with_worktrees() {
    let fx = Fx::new();
    let c0 = fx.commit(None, &[("a.txt", Some("a\n"))], "c0");
    fx.remote("main", c0);
    fx.origin_head("main");
    fx.branch("main", c0);
    fx.set_head("main");
    fx.checkout_head();
    fx.branch("plain", c0);
    fx.branch("clean", c0);
    fx.branch("dirty", c0);
    let u = fx.commit(Some(c0), &[("u.txt", Some("u\n"))], "unmerged");
    fx.branch("unmerged", u);
    real_worktree(&fx, "c", "clean");
    let dirty = real_worktree(&fx, "d", "dirty");
    std::fs::write(dirty.join("new.txt"), "x\n").unwrap();
    real_worktree(&fx, "u", "unmerged");
    let b = Built {
        fx,
        m1: c0,
        pr_head: c0,
        moved_head: c0,
    };
    let p = plan(&b, false);
    let text = stw::tidy::render_human(std::slice::from_ref(&p), false);
    assert!(
        text.contains("1 to delete, 1 to delete with their worktree, 0 to review, 2 kept"),
        "{text}"
    );
    let line = |n: &str| {
        text.lines()
            .find(|l| l.contains(&format!(" {n} ")))
            .unwrap_or_else(|| panic!("no line for {n}: {text}"))
            .to_string()
    };
    assert!(
        line("clean").contains("removable with its branch"),
        "{text}"
    );
    assert!(
        line("dirty").contains("stays: uncommitted changes"),
        "{text}"
    );
    let json = stw::tidy::to_json(
        &path_str(&b.fx.path),
        &stw::github::GithubState {
            client: None,
            note: None,
        },
        &[p],
    );
    assert_eq!(json["summary"]["delete"], 1);
    assert_eq!(json["summary"]["delete_with_worktree"], 1);
    assert_eq!(json["summary"]["keep"], 2);
    let branches = json["repos"][0]["branches"].as_array().unwrap();
    let blocker = |n: &str| {
        branches.iter().find(|x| x["branch"] == n).unwrap()["worktree"]["blocker"].clone()
    };
    assert!(blocker("clean").is_null());
    assert!(blocker("dirty").as_str().unwrap().contains("uncommitted"));
}

/// From a linked worktree root, `.worktrees/` is still the one of the main checkout.
#[test]
fn from_a_linked_root_the_worktrees_directory_is_the_main_checkouts() {
    let fx = Fx::new();
    let c0 = fx.commit(None, &[("a.txt", Some("a\n"))], "c0");
    fx.remote("main", c0);
    fx.origin_head("main");
    fx.branch("main", c0);
    fx.set_head("main");
    fx.checkout_head();
    fx.branch("root", c0);
    fx.branch("inside", c0);
    let root = real_worktree(&fx, "root", "root");
    add_worktree_at(
        &fx,
        "in",
        "inside",
        &fx.path.join(".worktrees/in"),
        true,
        false,
    );
    let rr = stw::repo::RepoRef {
        name: "fx".into(),
        path: root,
    };
    let p = stw::tidy::plan_repo_with_opts(
        &rr,
        stw::tidy::PrProof {
            prs: None,
            note: "off".into(),
        },
        &stw::tidy::PlanOpts {
            backup_days: 7,
            now: 1_800_000_000,
            similarity: None,
        },
    );
    assert!(p.error.is_none(), "{:?}", p.error);
    let wt = p
        .branches
        .iter()
        .find(|b| b.branch == "inside")
        .and_then(|b| b.worktree.as_ref())
        .expect("worktree of inside");
    assert!(!wt.outside, "below the main checkout's .worktrees/");
}
