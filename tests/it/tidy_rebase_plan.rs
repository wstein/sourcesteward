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

// ---- landed_via, JSON, text, summary, apply ----

use serde_json::Value;
use stw::github::GithubState;
use stw::tidy_rebase::{Basis, LandedKind};

fn json_of(p: &RepoPlan) -> Value {
    let gh = GithubState {
        client: None,
        note: None,
    };
    stw::tidy::to_json("/root", &gh, std::slice::from_ref(p))
}

fn branch_json<'a>(v: &'a Value, name: &str) -> &'a Value {
    v["repos"][0]["branches"]
        .as_array()
        .unwrap()
        .iter()
        .find(|b| b["branch"] == name)
        .unwrap_or_else(|| panic!("no branch {name}"))
}

fn key_list(v: &Value) -> Vec<String> {
    let mut k: Vec<String> = v
        .as_object()
        .map(|m| m.keys().cloned().collect())
        .unwrap_or_default();
    k.sort();
    k
}

fn sorted(names: &[&str]) -> Vec<String> {
    let mut v: Vec<String> = names.iter().map(|s| s.to_string()).collect();
    v.sort();
    v
}

fn via(p: &RepoPlan, branch: &str) -> (LandedKind, Basis, Strength) {
    let l = get(p, branch).landed_via;
    (l.kind, l.basis, l.strength)
}

#[test]
fn landed_via_follows_the_evidence_of_each_branch() {
    let mut w = world();
    // merged: the tip is an ancestor of main
    let l0 = cs(0, 1);
    let ids = w.branch("merged", &l0);
    w.set_main(ids[0]);
    // landed without a change: equal patches, with identity
    let l1 = cs(1, 2);
    w.branch("equal", &l1);
    w.land(&l1, MERGE_SECS);
    // landed with a resolution
    let l2 = cs(3, 2);
    w.branch("resolved", &l2);
    w.land_resolved(&l2, MERGE_SECS + 100);
    // one commit with an equal patch but another author time: a hint for a squash
    let l3 = cs(5, 1);
    w.branch("one", &l3);
    let mut moved = l3.clone();
    moved[0].secs += 777;
    w.land(&moved, MERGE_SECS + 200);
    // two such commits: nothing to say
    let l4 = cs(6, 2);
    w.branch("two", &l4);
    let mut moved = l4.clone();
    for m in &mut moved {
        m.secs += 777;
    }
    w.land(&moved, MERGE_SECS + 300);
    // a branch that did not land, and one with an open pull request
    w.branch("open", &cs(8, 1));
    w.branch("plain", &cs(9, 1));
    let prs = vec![AnyPr {
        head_ref: "open".into(),
        head_sha: "0".repeat(40),
        number: 1,
        state: "OPEN",
        ..Default::default()
    }];
    let p = plan_with(&w, Some(prs), &opts());
    use LandedKind::*;
    assert_eq!(
        via(&p, "merged"),
        (Merge, Basis::Ancestor, Strength::Strong)
    );
    assert_eq!(
        via(&p, "equal"),
        (Rebase, Basis::CommitMap, Strength::Strong)
    );
    assert_eq!(
        via(&p, "resolved"),
        (Rebase, Basis::CommitMap, Strength::Strong)
    );
    assert_eq!(
        via(&p, "one"),
        (Unknown, Basis::SingleCommitMap, Strength::Hint)
    );
    assert_eq!(via(&p, "two"), (Unknown, Basis::None, Strength::None));
    assert_eq!(via(&p, "plain"), (Unknown, Basis::None, Strength::None));
    assert_eq!(via(&p, "open"), (Unknown, Basis::None, Strength::None));
    // the decision never depends on it
    assert_eq!(proof_of(&p, "equal"), ("delete", "equal-patches"));
    assert_eq!(proof_of(&p, "one"), ("delete", "equal-patches"));
}

#[test]
fn the_stage_off_leaves_landed_via_to_the_ancestor_check() {
    let mut w = world();
    let l = cs(0, 2);
    w.branch("equal", &l);
    w.land(&l, MERGE_SECS);
    let ids = w.branch("merged", &cs(2, 1));
    w.set_main(ids[0]);
    let mut o = opts();
    o.rebase = false;
    let p = plan_with(&w, None, &o);
    use LandedKind::*;
    assert_eq!(
        via(&p, "merged"),
        (Merge, Basis::Ancestor, Strength::Strong)
    );
    assert_eq!(via(&p, "equal"), (Unknown, Basis::None, Strength::None));
    let v = json_of(&p);
    assert!(branch_json(&v, "equal")["rebase"].is_null());
    assert_eq!(branch_json(&v, "equal")["landed_via"]["kind"], "unknown");
}

#[test]
fn the_json_has_the_rebase_object_landed_via_and_the_summary() {
    let mut w = world();
    let list = cs(0, 3);
    w.branch("feat", &list);
    w.land_resolved(&list[..3], MERGE_SECS);
    w.branch("rest", &cs(3, 2));
    let p = plan(&w);
    let v = json_of(&p);
    let b = branch_json(&v, "feat");
    assert_eq!(
        key_list(b),
        sorted(&[
            "branch",
            "commit",
            "decision",
            "proof",
            "detail",
            "unique_commits",
            "upstream",
            "upstream_track",
            "worktree",
            "similarity",
            "rebase",
            "landed_via"
        ])
    );
    assert_eq!(b["proof"], "rebase-identity");
    assert_eq!(b["decision"], "review");
    assert_eq!(
        key_list(&b["landed_via"]),
        sorted(&["kind", "basis", "strength"])
    );
    assert_eq!(b["landed_via"]["kind"], "rebase");
    assert_eq!(b["landed_via"]["basis"], "commit-map");
    assert_eq!(b["landed_via"]["strength"], "strong");
    let r = &b["rebase"];
    assert_eq!(
        key_list(r),
        sorted(&[
            "state",
            "strength",
            "n",
            "mapped",
            "by_patch_id",
            "by_identity",
            "by_similarity",
            "unmapped",
            "ambiguous",
            "contiguous",
            "in_order",
            "gap",
            "runs",
            "run_len",
            "partial",
            "content_ok",
            "modified",
            "skipped",
            "window",
            "api",
            "mapping",
            "remaining"
        ])
    );
    assert_eq!(r["state"], "computed");
    assert_eq!(r["strength"], "strong");
    assert_eq!(
        (&r["n"], &r["mapped"], &r["by_identity"]),
        (&3.into(), &3.into(), &3.into())
    );
    assert_eq!(
        (&r["contiguous"], &r["in_order"], &r["run_len"]),
        (&true.into(), &true.into(), &3.into())
    );
    assert_eq!(key_list(&r["skipped"]), sorted(&["merges", "empty"]));
    assert_eq!(
        key_list(&r["window"]),
        sorted(&["commits", "stop", "cut", "stopped_at"])
    );
    assert_eq!(r["window"]["stop"], "root");
    assert_eq!(
        key_list(&r["api"]),
        sorted(&[
            "state",
            "reason",
            "pr",
            "merge_commit",
            "merge_parents",
            "k",
            "tip_matches_run",
            "head_in_branch",
            "kind",
            "strength"
        ])
    );
    assert_eq!(r["api"]["state"], "not-read");
    assert_eq!(r["api"]["reason"], "not-needed");
    let m = &r["mapping"][0];
    assert_eq!(
        key_list(m),
        sorted(&[
            "commit",
            "subject",
            "main",
            "position",
            "method",
            "value",
            "identity",
            "tz_differs",
            "ambiguous",
            "later"
        ])
    );
    assert_eq!(m["method"], "identity");
    assert_eq!(m["value"], 97.5);
    assert_eq!(m["subject"], "step 0");
    assert_eq!(r["mapping"].as_array().unwrap().len(), 3);
    assert!(r["remaining"].as_array().unwrap().is_empty());
    // the other branch: computed, nothing mapped
    let o = &branch_json(&v, "rest")["rebase"];
    assert_eq!(o["strength"], "none");
    assert_eq!(o["api"]["reason"], "offline");
    assert_eq!(o["mapped"], 0);
    assert_eq!(o["mapping"][0]["method"], "none");
    assert!(o["mapping"][0]["main"].is_null());
    assert_eq!(o["remaining"][0]["files"][0], "f3.txt");
    assert_eq!(o["remaining"][0]["files_total"], 1);
    // summary: counted as review, and on its own
    let s = &v["summary"];
    assert_eq!(s["rebase"], 1);
    assert_eq!(s["review"], 1);
    assert_eq!(v["schema_version"], 2);
}

#[test]
fn the_text_names_the_proof_in_one_line_and_gives_no_command() {
    let mut w = world();
    let list = cs(0, 3);
    w.branch("feat", &list);
    w.land_resolved(&list, MERGE_SECS);
    let eq = cs(3, 2);
    w.branch("equal", &eq);
    w.land(&eq, MERGE_SECS + 100);
    let text = stw::tidy::render_human(&[plan(&w)], true);
    let line = text
        .lines()
        .find(|l| l.contains("[rebase-identity]"))
        .unwrap_or_else(|| panic!("{text}"));
    assert!(
        line.trim_start().starts_with("review  feat  [rebase-identity] rebase-identity (3 of 3 commits, run of 3, min 97.5 %)"),
        "{line}"
    );
    assert!(
        !text.contains("next command") && !text.contains("git branch -D"),
        "{text}"
    );
    assert!(
        text.contains("1 to review (1 of them rebase-identity)"),
        "{text}"
    );
    let eq_line = text
        .lines()
        .find(|l| l.contains("[equal-patches]"))
        .unwrap();
    assert!(
        eq_line.ends_with("; landed via rebase (commit-map, strong)"),
        "{eq_line}"
    );
}

#[test]
fn a_mix_of_equal_patches_rebase_identity_and_similar_patches() {
    let mut w = world();
    let a = cs(0, 2);
    w.branch("a-equal", &a);
    w.land(&a, MERGE_SECS);
    let b = cs(2, 2);
    w.branch("b-rebase", &b);
    w.land_resolved(&b, MERGE_SECS + 100);
    // similar by content but no identity: another author time
    let c3 = cs(4, 1);
    w.branch("c-similar", &c3);
    let mut moved: Vec<C> = c3.iter().map(resolved).collect();
    moved[0].secs += 500;
    w.land(&moved, MERGE_SECS + 200);
    let p = plan(&w);
    assert_eq!(proof_of(&p, "a-equal"), ("delete", "equal-patches"));
    assert_eq!(proof_of(&p, "b-rebase"), ("review", "rebase-identity"));
    assert_eq!(proof_of(&p, "c-similar"), ("review", "similar-patches"));
    let v = json_of(&p);
    assert_eq!(v["summary"]["review"], 2);
    assert_eq!(v["summary"]["rebase"], 1);
    assert_eq!(v["summary"]["similar"], 1);
    let text = stw::tidy::render_human(&[p], true);
    assert!(
        text.contains("2 to review (1 of them rebase-identity, 1 similar-patches)"),
        "{text}"
    );
}

#[test]
fn a_rebase_identity_branch_is_never_deleted_by_apply() {
    let mut w = world();
    let list = cs(0, 2);
    w.branch("feat", &list);
    w.land_resolved(&list, MERGE_SECS);
    let p = plan(&w);
    assert_eq!(proof_of(&p, "feat"), ("review", "rebase-identity"));
    let dir = scratch();
    let o = stw::tidy_apply::ApplyOpts {
        manifest_dir: dir.path().to_path_buf(),
        run_id: "0000000000001-1-0".into(),
        now: T0,
    };
    // a fresh run
    let r = stw::tidy_apply::apply_repo(&rr(&w), &p, None, &o);
    assert!(r.error.is_none(), "{:?}", r.error);
    assert!(stw::repo::resolve(&w.fx.repo(), "refs/heads/feat").is_some());
    assert!(
        r.branches.iter().all(|b| b.branch != "feat"),
        "{:?}",
        r.branches
    );
    // a plan file edited by hand: `delete` for this branch
    let hand = vec![stw::tidy_apply::Planned {
        branch: "feat".into(),
        commit: w.fx.tip("feat").to_string(),
        kind: stw::tidy_apply::Kind::Branch,
        worktree: None,
    }];
    let o2 = stw::tidy_apply::ApplyOpts {
        run_id: "0000000000002-1-0".into(),
        ..o
    };
    let r = stw::tidy_apply::apply_repo(&rr(&w), &p, Some(&hand), &o2);
    assert!(stw::repo::resolve(&w.fx.repo(), "refs/heads/feat").is_some());
    let res = r
        .branches
        .iter()
        .find(|b| b.branch == "feat")
        .expect("named");
    assert_eq!(res.action, "skipped");
    assert!(
        res.reason
            .contains("the proof no longer holds: now review [rebase-identity]"),
        "{}",
        res.reason
    );
}

#[test]
fn a_remaining_list_has_at_most_twenty_masked_paths() {
    let w = world();
    let mut c = c(0);
    c.file = "d/\u{202e}evil.txt".into();
    let big: Vec<(String, String)> = (0..25)
        .map(|i| (format!("many/f{i:02}.txt"), "x\n".to_string()))
        .collect();
    let mut edits: Vec<(&str, Option<&str>)> = vec![(c.file.as_str(), Some(c.body.as_str()))];
    edits.extend(big.iter().map(|(f, b)| (f.as_str(), Some(b.as_str()))));
    let s = Stamp::new(NAME, MAIL, c.secs);
    let id = w.fx.commit_at(Some(w.c0), &edits, "many files", s, s);
    w.fx.branch("many", id);
    let p = plan(&w);
    let v = json_of(&p);
    let rem = &branch_json(&v, "many")["rebase"]["remaining"][0];
    assert_eq!(rem["files_total"], 26);
    let files = rem["files"].as_array().unwrap();
    assert_eq!(files.len(), 20);
    assert!(
        files
            .iter()
            .all(|f| !f.as_str().unwrap().contains('\u{202e}')),
        "{files:?}"
    );
}
