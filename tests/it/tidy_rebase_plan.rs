//! The rebase stage of `stw tidy plan` (evidence class `rebase-identity`, decision `review`, never `delete`).
//! Rebase merges are rebuilt with gix: the same author, author time and subject on a new parent with a new committer
//! and committer time. A conflict resolution is a landed commit with one changed line, so that the patch ID differs
//! and the identity decides.
use crate::common::*;
use gix::ObjectId;
use stw::github::AnyPr;
use stw::repo::RepoRef;
use stw::tidy::{plan_repo_with_opts, PlanOpts, PrProof, RepoPlan, Similarity};
use stw::tidy_rebase::Strength;

pub const T0: i64 = 1_700_000_130;
pub const NAME: &str = "Werner Stein";
pub const MAIL: &str = "w@example.com";
pub const MERGE_SECS: i64 = T0 + 10_000;

/// One commit of a branch: its own file of 40 lines.
#[derive(Clone)]
pub struct C {
    pub subject: String,
    pub file: String,
    pub body: String,
    pub secs: i64,
}

pub fn lines(tag: &str, n: usize, odd: Option<usize>) -> String {
    (0..n)
        .map(|k| {
            if odd == Some(k) {
                format!("{tag} odd {k}\n")
            } else {
                format!("{tag} line {k}\n")
            }
        })
        .collect()
}

pub fn c(i: usize) -> C {
    C {
        subject: format!("step {i}"),
        file: format!("f{i}.txt"),
        body: lines(&format!("c{i}"), 40, None),
        secs: T0 + 100 * i as i64,
    }
}

pub fn cs(from: usize, n: usize) -> Vec<C> {
    (from..from + n).map(c).collect()
}

/// The same commit after a conflict resolution: one of 40 lines differs (97.5 %, a different patch ID).
pub fn resolved(c: &C) -> C {
    let tag = c.body.split(' ').next().unwrap().to_string();
    C {
        body: lines(&tag, 40, Some(7)),
        ..c.clone()
    }
}

pub struct W {
    pub fx: Fx,
    pub c0: ObjectId,
    pub main: ObjectId,
}

pub fn world() -> W {
    let fx = Fx::new();
    let s = Stamp::new("Other", "o@example.com", T0 - 10_000);
    let c0 = fx.commit_at(None, &[("base.txt", Some("base\n"))], "base", s, s);
    fx.remote("main", c0);
    fx.origin_head("main");
    fx.branch("main", c0);
    fx.set_head("main");
    fx.checkout_head();
    W { fx, c0, main: c0 }
}

fn put(fx: &Fx, parent: ObjectId, c: &C, committer: Stamp) -> ObjectId {
    let author = Stamp::new(NAME, MAIL, c.secs);
    fx.commit_at(
        Some(parent),
        &[(c.file.as_str(), Some(c.body.as_str()))],
        &c.subject,
        author,
        committer,
    )
}

impl W {
    /// The commits on `parent` (default: the first commit), no ref.
    pub fn commits_on(&self, parent: Option<ObjectId>, list: &[C]) -> Vec<ObjectId> {
        let mut out: Vec<ObjectId> = Vec::new();
        for c in list {
            let p = out.last().copied().or(parent).unwrap_or(self.c0);
            out.push(put(&self.fx, p, c, Stamp::new(NAME, MAIL, c.secs)));
        }
        out
    }
    /// The commits on the first commit, with the branch `name` at the last one.
    pub fn branch(&self, name: &str, list: &[C]) -> Vec<ObjectId> {
        let ids = self.commits_on(None, list);
        self.fx.branch(name, *ids.last().unwrap());
        ids
    }
    /// A rebase merge: the commits again on main with a new committer; main moves.
    pub fn land(&mut self, list: &[C], secs: i64) -> Vec<ObjectId> {
        let mut out: Vec<ObjectId> = Vec::new();
        for (k, c) in list.iter().enumerate() {
            let p = out.last().copied().unwrap_or(self.main);
            let m = Stamp::new("Merger", "merger@example.com", secs + k as i64);
            out.push(put(&self.fx, p, c, m));
        }
        self.set_main(*out.last().unwrap());
        out
    }
    pub fn land_resolved(&mut self, list: &[C], secs: i64) -> Vec<ObjectId> {
        let r: Vec<C> = list.iter().map(resolved).collect();
        self.land(&r, secs)
    }
    /// A commit of somebody else on main.
    pub fn foreign(&mut self, i: usize) {
        let s = Stamp::new("Other", "o@example.com", T0 + 50 + i as i64);
        let f = format!("g{i}.txt");
        let m = self.fx.commit_at(
            Some(self.main),
            &[(f.as_str(), Some("g\n"))],
            format!("foreign {i}"),
            s,
            s,
        );
        self.set_main(m);
    }
    pub fn set_main(&mut self, id: ObjectId) {
        self.main = id;
        self.fx.remote("main", id);
        self.fx.branch("main", id);
    }
}

pub fn rr(w: &W) -> RepoRef {
    RepoRef {
        name: "fx".into(),
        path: w.fx.path.clone(),
    }
}

pub fn opts() -> PlanOpts {
    PlanOpts {
        backup_days: 7,
        now: T0,
        similarity: Some(Similarity::default()),
        rebase: true,
    }
}

pub fn plan_with(w: &W, prs: Option<Vec<AnyPr>>, o: &PlanOpts) -> RepoPlan {
    plan_repo_with_opts(
        &rr(w),
        PrProof {
            prs,
            note: "off".into(),
            api: Default::default(),
        },
        o,
    )
}

pub fn plan(w: &W) -> RepoPlan {
    plan_with(w, None, &opts())
}

pub fn get<'a>(p: &'a RepoPlan, branch: &str) -> &'a stw::tidy::BranchPlan {
    p.branches
        .iter()
        .find(|b| b.branch == branch)
        .unwrap_or_else(|| panic!("no branch {branch}"))
}

/// The report of the stage; a missing object is a failed assertion.
pub fn report<'a>(p: &'a RepoPlan, branch: &str) -> &'a stw::tidy_rebase::RebaseReport {
    let b = get(p, branch);
    assert!(
        b.rebase.is_some(),
        "{branch} has no rebase object: {}",
        b.detail
    );
    &b.rebase.as_ref().unwrap().report
}

pub fn proof_of(p: &RepoPlan, branch: &str) -> (&'static str, &'static str) {
    let b = get(p, branch);
    (b.decision, b.proof)
}

// ---- the proof ----

#[test]
fn a_rebase_merge_with_a_conflict_resolution_is_rebase_identity_and_review() {
    let mut w = world();
    let list = cs(0, 3);
    w.branch("feat", &list);
    w.land_resolved(&list, MERGE_SECS);
    let p = plan(&w);
    assert!(p.error.is_none(), "{:?}", p.error);
    assert_eq!(proof_of(&p, "feat"), ("review", "rebase-identity"));
    let b = get(&p, "feat");
    assert_eq!(
        b.detail,
        "rebase-identity (3 of 3 commits, run of 3, min 97.5 %): review, not a deletion proof"
    );
    let r = report(&p, "feat");
    assert_eq!(r.strength, Strength::Strong);
    assert_eq!((r.n, r.mapped, r.by_identity, r.by_patch_id), (3, 3, 3, 0));
    assert!(r.contiguous && r.in_order);
}

#[test]
fn identity_without_a_run_is_a_hint_and_leaves_the_proof_alone() {
    let mut w = world();
    let list = cs(0, 3);
    w.branch("feat", &list);
    // a foreign commit between the landed commits: no run
    let r = w.land_resolved(&list[..1], MERGE_SECS);
    assert_eq!(r.len(), 1);
    w.foreign(1);
    w.land_resolved(&list[1..], MERGE_SECS + 10);
    let mut o = opts();
    o.similarity = None;
    let p = plan_with(&w, None, &o);
    assert_eq!(proof_of(&p, "feat"), ("keep", "unmerged"));
    let r = report(&p, "feat");
    assert_eq!(r.strength, Strength::Hint);
    assert_eq!((r.mapped, r.gap, r.contiguous), (3, 1, false));
}

#[test]
fn a_part_of_the_branch_that_landed_is_partial_and_the_rest_is_named() {
    let mut w = world();
    let list = cs(0, 3);
    w.branch("feat", &list);
    w.land_resolved(&list[..2], MERGE_SECS);
    let mut o = opts();
    o.similarity = None;
    let p = plan_with(&w, None, &o);
    assert_eq!(proof_of(&p, "feat"), ("keep", "unmerged"));
    let r = report(&p, "feat");
    assert_eq!(r.strength, Strength::Hint);
    assert!(r.partial);
    assert_eq!((r.n, r.mapped, r.unmapped), (3, 2, 1));
    assert_eq!(r.remaining.len(), 1);
    assert_eq!(r.remaining[0].files, vec!["f2.txt".to_string()]);
}

#[test]
fn the_rebase_proof_comes_before_pull_request_moved_and_upstream_gone() {
    let mut w = world();
    let list = cs(0, 2);
    w.branch("moved", &list);
    w.branch("gone", &list);
    w.fx.track("gone");
    w.land_resolved(&list, MERGE_SECS);
    let pr = AnyPr {
        head_ref: "moved".into(),
        head_sha: "0".repeat(40),
        number: 5,
        state: "MERGED",
        ..Default::default()
    };
    let p = plan_with(&w, Some(vec![pr]), &opts());
    assert_eq!(proof_of(&p, "moved"), ("review", "rebase-identity"));
    assert_eq!(proof_of(&p, "gone"), ("review", "rebase-identity"));
    // without the stages they are what they were
    let mut o = opts();
    o.rebase = false;
    o.similarity = None;
    let pr = AnyPr {
        head_ref: "moved".into(),
        head_sha: "0".repeat(40),
        number: 5,
        state: "MERGED",
        ..Default::default()
    };
    let p = plan_with(&w, Some(vec![pr]), &o);
    assert_eq!(
        proof_of(&p, "moved"),
        ("review", "pull-request-merged-branch-moved")
    );
    assert_eq!(proof_of(&p, "gone"), ("review", "upstream-gone"));
    assert!(get(&p, "moved").rebase.is_none());
}

#[test]
fn rebase_identity_comes_before_similar_patches() {
    let mut w = world();
    let list = cs(0, 2);
    w.branch("feat", &list);
    w.land_resolved(&list, MERGE_SECS);
    let p = plan(&w);
    assert_eq!(get(&p, "feat").proof, "rebase-identity");
    assert!(
        get(&p, "feat").similarity.is_none(),
        "the similarity stage does not judge a branch the rebase stage has proved"
    );
}

#[test]
fn the_existing_proofs_keep_their_place() {
    let mut w = world();
    let list = cs(0, 2);
    // equal patches: landed without a change
    w.branch("equal", &list);
    w.land(&list, MERGE_SECS);
    // an open pull request and a backup name, both landed with a resolution
    let l2 = cs(2, 2);
    w.branch("opened", &l2);
    w.branch("backup/old", &l2);
    w.land_resolved(&l2, MERGE_SECS + 100);
    // a merged pull request with exactly this head
    let l3 = cs(4, 2);
    let ids = w.branch("headed", &l3);
    w.land_resolved(&l3, MERGE_SECS + 200);
    let prs = vec![
        AnyPr {
            head_ref: "opened".into(),
            head_sha: "0".repeat(40),
            number: 1,
            state: "OPEN",
            ..Default::default()
        },
        AnyPr {
            head_ref: "headed".into(),
            head_sha: ids.last().unwrap().to_string(),
            number: 2,
            state: "MERGED",
            ..Default::default()
        },
    ];
    let p = plan_with(&w, Some(prs), &opts());
    assert_eq!(proof_of(&p, "equal"), ("delete", "equal-patches"));
    assert_eq!(proof_of(&p, "opened"), ("keep", "open-pull-request"));
    assert_eq!(proof_of(&p, "backup/old"), ("keep", "backup-recent"));
    assert_eq!(
        proof_of(&p, "headed"),
        ("delete", "merged-pull-request-head")
    );
    // a proof that is not the rebase proof still gets the object where it is of use; an open pull request and a backup do not
    assert!(get(&p, "equal").rebase.is_some());
    assert!(get(&p, "headed").rebase.is_some());
    assert!(get(&p, "opened").rebase.is_none());
    assert!(get(&p, "backup/old").rebase.is_none());
}

#[test]
fn a_branch_in_a_worktree_stays_protected_and_shows_the_object() {
    let mut w = world();
    let list = cs(0, 2);
    w.branch("feat", &list);
    w.land_resolved(&list, MERGE_SECS);
    real_worktree(&w.fx, "feat", "feat");
    let p = plan(&w);
    assert_eq!(proof_of(&p, "feat"), ("keep", "in-worktree"));
    let r = report(&p, "feat");
    assert_eq!(r.strength, Strength::Strong);
}

#[test]
fn the_stage_off_gives_the_plan_without_it() {
    let mut w = world();
    let list = cs(0, 2);
    w.branch("feat", &list);
    w.land_resolved(&list, MERGE_SECS);
    let mut o = opts();
    o.rebase = false;
    let p = plan_with(&w, None, &o);
    assert_eq!(proof_of(&p, "feat"), ("review", "similar-patches"));
    assert!(get(&p, "feat").rebase.is_none());
}

#[test]
fn a_world_without_evidence_has_the_same_text_with_and_without_the_stage() {
    let mut w = world();
    w.branch("plain", &cs(0, 2));
    w.foreign(1);
    let on = plan(&w);
    let mut o = opts();
    o.rebase = false;
    let off = plan_with(&w, None, &o);
    assert_eq!(
        stw::tidy::render_human(&[on], true),
        stw::tidy::render_human(&[off], true)
    );
}

#[test]
fn a_plan_run_twice_is_the_same() {
    let mut w = world();
    for (i, name) in ["b", "a", "c"].iter().enumerate() {
        let list = cs(3 * i, 2);
        w.branch(name, &list);
        if i != 2 {
            w.land_resolved(&list, MERGE_SECS + 100 * i as i64);
        }
    }
    let line = |p: &RepoPlan| -> Vec<String> {
        p.branches
            .iter()
            .map(|b| format!("{} {} {}", b.branch, b.proof, b.detail))
            .collect()
    };
    assert_eq!(line(&plan(&w)), line(&plan(&w)));
}

#[test]
fn author_times_at_the_edge_of_the_range_do_not_crash_the_plan() {
    for secs in [i64::MIN + 1000, i64::MAX - 1000, 0, -1] {
        let mut w = world();
        let mut list = cs(0, 2);
        list[0].secs = secs;
        w.branch("feat", &list);
        w.land_resolved(&list, MERGE_SECS);
        let p = plan(&w);
        assert!(p.error.is_none(), "{secs}: {:?}", p.error);
        assert!(get(&p, "feat").rebase.is_some(), "{secs}");
    }
}
