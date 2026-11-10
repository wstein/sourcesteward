//! The backup rule of `tidy plan` and `tidy apply`: backup branches (`r9-*`, `r10-*`, `pre-restack*`, `archive/*`, `backup/*`)
//! are `keep` while the tip commit is younger than N days and `delete` from N days on. A proof still wins.
use crate::common::*;
use stw::repo::RepoRef;
use stw::tidy::{plan_repo_with_opts, BranchPlan, PlanOpts, PrProof, RepoPlan};

const DAY: i64 = 86_400;

struct B {
    fx: Fx,
    /// committer time of the tip of every unmerged backup branch
    tip_time: i64,
}

fn build() -> B {
    let fx = Fx::new();
    let c0 = fx.commit(None, &[("a.txt", Some("a\n"))], "c0");
    let c1 = fx.commit(Some(c0), &[("a.txt", Some("b\n"))], "main work");
    fx.remote("main", c1);
    fx.origin_head("main");
    fx.branch("main", c1);
    fx.set_head("main");
    fx.checkout_head();
    // one unmerged commit shared by all backup-named branches and the look-alikes
    let u = fx.commit(Some(c0), &[("u.txt", Some("u\n"))], "unmerged backup work");
    for n in [
        "r9-before",
        "r10-before",
        "pre-restack",
        "pre-restack-2",
        "archive/old",
        "backup/old",
        "r90-not-a-backup",
        "my-archive/x",
        "restack-pre",
        "other",
    ] {
        fx.branch(n, u);
    }
    // a backup whose tip is an ancestor of main: the proof wins over the age
    fx.branch("backup/merged", c0);
    // a backup with a gone upstream (review for any other branch)
    let g = fx.commit(Some(c0), &[("g.txt", Some("g\n"))], "gone upstream work");
    fx.branch("backup/gone-up", g);
    fx.track("backup/gone-up");
    let tip_time = fx
        .repo()
        .find_commit(u)
        .unwrap()
        .committer()
        .unwrap()
        .seconds();
    B { fx, tip_time }
}

fn plan(b: &B, days: u64, now: i64) -> RepoPlan {
    let opts = PlanOpts {
        backup_days: days,
        now,
    };
    plan_repo_with_opts(
        &RepoRef {
            name: "fx".into(),
            path: b.fx.path.clone(),
        },
        PrProof {
            prs: None,
            note: "off".into(),
        },
        &opts,
    )
}

fn get<'a>(p: &'a RepoPlan, branch: &str) -> &'a BranchPlan {
    p.branches
        .iter()
        .find(|b| b.branch == branch)
        .unwrap_or_else(|| panic!("no branch {branch}"))
}

const BACKUPS: [&str; 6] = [
    "r9-before",
    "r10-before",
    "pre-restack",
    "pre-restack-2",
    "archive/old",
    "backup/old",
];

#[test]
fn a_backup_younger_than_n_days_is_kept() {
    let b = build();
    let p = plan(&b, 7, b.tip_time + 7 * DAY - 1);
    assert!(p.error.is_none(), "{:?}", p.error);
    for n in BACKUPS {
        let bp = get(&p, n);
        assert_eq!((bp.decision, bp.proof), ("keep", "backup-recent"), "{n}");
    }
}

#[test]
fn a_backup_of_exactly_n_days_is_deleted_with_the_age_as_proof() {
    let b = build();
    let p = plan(&b, 7, b.tip_time + 7 * DAY);
    for n in BACKUPS {
        let bp = get(&p, n);
        assert_eq!(
            (bp.decision, bp.proof),
            ("delete", "backup-older-than-days"),
            "{n}: {}",
            bp.detail
        );
        assert!(
            bp.detail.contains("backup older than 7 days"),
            "{}",
            bp.detail
        );
    }
}

#[test]
fn the_number_of_days_is_a_parameter() {
    let b = build();
    let now = b.tip_time + 10 * DAY;
    assert_eq!(get(&plan(&b, 10, now), "backup/old").decision, "delete");
    assert_eq!(get(&plan(&b, 11, now), "backup/old").decision, "keep");
    assert_eq!(get(&plan(&b, 3, now), "backup/old").decision, "delete");
}

#[test]
fn only_the_listed_name_patterns_are_backups() {
    let b = build();
    let p = plan(&b, 7, b.tip_time + 100 * DAY);
    for n in ["r90-not-a-backup", "my-archive/x", "restack-pre", "other"] {
        let bp = get(&p, n);
        assert_eq!((bp.decision, bp.proof), ("keep", "unmerged"), "{n}");
    }
}

#[test]
fn a_backup_with_a_proof_follows_the_proof_not_the_age() {
    let b = build();
    // young, but the tip is an ancestor of main
    let p = plan(&b, 7, b.tip_time);
    let bp = get(&p, "backup/merged");
    assert_eq!((bp.decision, bp.proof), ("delete", "merged-ancestor"));
}

#[test]
fn a_backup_is_never_review() {
    let b = build();
    let young = plan(&b, 7, b.tip_time);
    let bp = get(&young, "backup/gone-up");
    assert_eq!((bp.decision, bp.proof), ("keep", "backup-recent"));
    let old = plan(&b, 7, b.tip_time + 1000 * DAY);
    let bp = get(&old, "backup/gone-up");
    assert_eq!(
        (bp.decision, bp.proof),
        ("delete", "backup-older-than-days")
    );
}

#[test]
fn a_backup_in_a_worktree_stays_protected() {
    let b = build();
    add_worktree(&b.fx, "bk", "backup/old", true, false);
    let p = plan(&b, 7, b.tip_time + 100 * DAY);
    let bp = get(&p, "backup/old");
    assert_eq!((bp.decision, bp.proof), ("keep", "in-worktree"));
}
