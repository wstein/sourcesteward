//! The rebase stage of `stw tidy plan` (evidence class `rebase-identity`, decision `review`, never `delete`).
//! Rebase merges are rebuilt with gix: the same author, author time and subject on a new parent with a new committer
//! and committer time. A conflict resolution is a landed commit with one changed line, so that the patch ID differs
//! and the identity decides.
use crate::common::FixtureCommand as _;
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
        // the similarity stage alone
        let mut o = opts();
        o.rebase = false;
        let p = plan_with(&w, None, &o);
        assert!(p.error.is_none(), "{secs}: {:?}", p.error);
        assert!(get(&p, "feat").similarity.is_some(), "{secs}");
    }
}

// ---- landed_via, JSON, text, summary, apply ----

use serde_json::Value;
use stw::github::GithubState;
use stw::tidy_rebase::{Basis, LandedKind};

pub(crate) fn json_of(p: &RepoPlan) -> Value {
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

pub(crate) fn via(p: &RepoPlan, branch: &str) -> (LandedKind, Basis, Strength) {
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
            "squash",
            "shallow",
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
    w.branch("plain", &cs(8, 1));
    let merged = w.commits_on(Some(w.main), &cs(5, 1));
    w.fx.branch("merged", merged[0]);
    w.set_main(merged[0]);
    let text = stw::tidy::render_human(&[plan(&w)], true);
    let merged_line = text
        .lines()
        .find(|l| l.contains("[merged-ancestor]"))
        .unwrap();
    assert!(!merged_line.contains("landed via"), "{merged_line}");
    let plain_line = text.lines().find(|l| l.contains(" plain ")).unwrap();
    assert!(!plain_line.contains("landed via"), "{plain_line}");
    let line = text
        .lines()
        .find(|l| l.contains("[rebase-identity]"))
        .unwrap_or_else(|| panic!("{text}"));
    assert!(
        line.trim_start().starts_with("review  feat  [rebase-identity] rebase-identity (3 of 3 commits, run of 3, min 97.5 %)"),
        "{line}"
    );
    assert!(!line.contains("landed via"), "{line}");
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
    // a second similar branch: the two counts of the summary line differ
    let d3 = cs(5, 1);
    w.branch("d-similar", &d3);
    let mut moved: Vec<C> = d3.iter().map(resolved).collect();
    moved[0].secs += 500;
    w.land(&moved, MERGE_SECS + 300);
    let p = plan(&w);
    assert_eq!(proof_of(&p, "a-equal"), ("delete", "equal-patches"));
    assert_eq!(proof_of(&p, "d-similar"), ("review", "similar-patches"));
    assert_eq!(proof_of(&p, "b-rebase"), ("review", "rebase-identity"));
    assert_eq!(proof_of(&p, "c-similar"), ("review", "similar-patches"));
    let v = json_of(&p);
    assert_eq!(v["summary"]["review"], 3);
    assert_eq!(v["summary"]["rebase"], 1);
    assert_eq!(v["summary"]["similar"], 2);
    let text = stw::tidy::render_human(&[p], true);
    assert!(
        text.contains("3 to review (1 of them rebase-identity, 2 similar-patches)"),
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

// ---- the pull request mapping through a fake API ----

use stw::auth::TokenSource;
use stw::github::Client;
use stw::tidy::PrApi;
use stw::tidy_rebase::ApiReason;

/// `YYYY-MM-DDTHH:MM:SSZ` of seconds since the epoch.
fn iso(secs: i64) -> String {
    let days = secs.div_euclid(86_400);
    let rem = secs.rem_euclid(86_400);
    let z = days + 719_468;
    let era = z.div_euclid(146_097);
    let doe = z - era * 146_097;
    let yoe = (doe - doe / 1460 + doe / 36_524 - doe / 146_096) / 365;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let m = if mp < 10 { mp + 3 } else { mp - 9 };
    let y = yoe + era * 400 + i64::from(m <= 2);
    format!(
        "{y:04}-{m:02}-{d:02}T{:02}:{:02}:{:02}Z",
        rem / 3600,
        rem % 3600 / 60,
        rem % 60
    )
}

fn pr_commit_json(id: ObjectId, c: &C) -> String {
    format!(
        r#"{{"sha":"{id}","commit":{{"author":{{"name":"{NAME}","email":"{MAIL}","date":"{}"}},"message":"{}"}}}}"#,
        iso(c.secs),
        c.subject
    )
}

/// The commits answer of a pull request: the first `k` commits of `list` with the ids they have on the branch.
pub(crate) fn commits_body(ids: &[ObjectId], list: &[C]) -> String {
    let v: Vec<String> = ids
        .iter()
        .zip(list)
        .map(|(i, c)| pr_commit_json(*i, c))
        .collect();
    format!("[{}]", v.join(","))
}

pub(crate) fn merged_pr(
    head: &str,
    number: u64,
    head_sha: String,
    merge: String,
    at: i64,
) -> AnyPr {
    AnyPr {
        head_ref: head.into(),
        head_sha,
        number,
        state: "MERGED",
        merge_commit_sha: merge,
        merged_at: Some(at),
        title: "t".into(),
        ..Default::default()
    }
}

pub(crate) fn plan_api(w: &W, prs: Vec<AnyPr>, api: PrApi, o: &PlanOpts) -> RepoPlan {
    plan_repo_with_opts(
        &rr(w),
        PrProof {
            prs: Some(prs),
            note: "ok".into(),
            api,
        },
        o,
    )
}

pub(crate) fn client(routes: &FakeRoutes) -> Client {
    Client::with_base("tok".into(), TokenSource::Env, routes.url.clone())
}

pub(crate) fn api_of(p: &RepoPlan, branch: &str) -> (Option<ApiReason>, LandedKind, Strength) {
    let b = get(p, branch);
    let a = &b.rebase.as_ref().expect("rebase object").api;
    (a.not_read, a.kind, a.strength)
}

/// A branch of three commits; the pull request holds the first two (`head` is the second), the branch moved on.
pub(crate) struct Moved {
    pub(crate) w: W,
    pub(crate) list: Vec<C>,
    pub(crate) ids: Vec<ObjectId>,
}

pub(crate) fn moved(landed: fn(&mut W, &[C]) -> ObjectId) -> Moved {
    let mut w = world();
    w.fx.set_origin_url("https://github.com/o/r.git");
    let list = cs(0, 3);
    let ids = w.branch("feat", &list);
    let m = landed(&mut w, &list[..2]);
    w.set_main(m);
    Moved { w, list, ids }
}

pub(crate) fn no_sim() -> PlanOpts {
    let mut o = opts();
    o.similarity = None;
    o
}

pub(crate) const MOVED: (&str, &str) = ("review", "pull-request-merged-branch-moved");

#[test]
fn a_pull_request_that_was_rebased_gives_rebase_by_the_pr_api() {
    let m = moved(|w, l| *w.land_resolved(l, MERGE_SECS).last().unwrap());
    let main = m.w.main.to_string();
    let body = commits_body(&m.ids[..2], &m.list[..2]);
    let routes = FakeRoutes::start(move |_, _| (200, vec![], body.clone()));
    let c = client(&routes);
    // the pull request of another branch comes first in the list
    let prs = vec![
        merged_pr("other", 9, "1".repeat(40), "f".repeat(40), MERGE_SECS),
        merged_pr("feat", 5, m.ids[1].to_string(), main.clone(), MERGE_SECS),
    ];
    let p = plan_api(&m.w, prs, PrApi::Client(&c), &no_sim());
    // the proof stays; the commits are mapped by identity, the branch has one more
    assert_eq!(proof_of(&p, "feat"), MOVED);
    assert_eq!(
        api_of(&p, "feat"),
        (None, LandedKind::Rebase, Strength::Hint)
    );
    assert_eq!(
        via(&p, "feat"),
        (LandedKind::Rebase, Basis::PrApi, Strength::Hint)
    );
    let hits = routes.hits();
    assert_eq!(hits.len(), 1, "{hits:?}");
    assert!(
        hits[0].starts_with("GET /repos/o/r/pulls/5/commits?per_page=100 "),
        "{}",
        hits[0]
    );
    let v = json_of(&p);
    let a = &branch_json(&v, "feat")["rebase"]["api"];
    assert_eq!(a["state"], "read");
    assert!(a["reason"].is_null());
    assert_eq!(
        (&a["pr"], &a["k"], &a["merge_parents"]),
        (&5.into(), &2.into(), &1.into())
    );
    assert_eq!(a["merge_commit"], main.as_str());
    assert_eq!(
        (&a["tip_matches_run"], &a["head_in_branch"], &a["kind"]),
        (&true.into(), &true.into(), &"rebase".into())
    );
}

#[test]
fn a_pull_request_with_a_squash_commit_gives_squash_by_the_merge_time() {
    // the squash commit is written by the merger at the merge time: author time = merged_at
    let m = moved(|w, _| {
        let s = Stamp::new("Merger", "merger@example.com", MERGE_SECS);
        w.fx.commit_at(
            Some(w.main),
            &[("squashed.txt", Some("all of it\n"))],
            "Add steps (#5)",
            s,
            s,
        )
    });
    let main = m.w.main.to_string();
    let body = commits_body(&m.ids[..2], &m.list[..2]);
    let routes = FakeRoutes::start(move |_, _| (200, vec![], body.clone()));
    let c = client(&routes);
    let prs = vec![merged_pr(
        "feat",
        5,
        m.ids[1].to_string(),
        main,
        MERGE_SECS + 1,
    )];
    let p = plan_api(&m.w, prs, PrApi::Client(&c), &no_sim());
    assert_eq!(proof_of(&p, "feat"), MOVED);
    assert_eq!(
        via(&p, "feat"),
        (LandedKind::Squash, Basis::PrApi, Strength::Hint),
        "the branch has a commit after the pull request head"
    );
}

#[test]
fn a_pull_request_with_a_merge_commit_gives_merge_by_two_parents() {
    let m = moved(|w, _| {
        // `main` takes the head of the pull request with a merge commit
        let s = Stamp::new("Merger", "merger@example.com", MERGE_SECS);
        let head =
            w.fx.repo()
                .find_reference("refs/heads/feat")
                .unwrap()
                .id()
                .detach();
        let parent_of_head =
            w.fx.repo()
                .find_commit(head)
                .unwrap()
                .parent_ids()
                .next()
                .unwrap()
                .detach();
        w.fx.commit_at_parents(
            vec![w.main, parent_of_head],
            Some(parent_of_head),
            &[],
            "Merge pull request #5",
            s,
            s,
        )
    });
    let main = m.w.main.to_string();
    let body = commits_body(&m.ids[..2], &m.list[..2]);
    let routes = FakeRoutes::start(move |_, _| (200, vec![], body.clone()));
    let c = client(&routes);
    let prs = vec![merged_pr("feat", 5, m.ids[1].to_string(), main, MERGE_SECS)];
    let p = plan_api(&m.w, prs, PrApi::Client(&c), &no_sim());
    assert_eq!(
        via(&p, "feat"),
        (LandedKind::Merge, Basis::PrApi, Strength::Hint),
        "the branch has a commit after the pull request head"
    );
    assert_eq!(
        get(&p, "feat").rebase.as_ref().unwrap().api.merge_parents,
        Some(2)
    );
}

#[test]
fn without_a_usable_pull_request_the_plan_falls_back_to_the_local_evidence_and_says_why() {
    let m = moved(|w, l| *w.land_resolved(l, MERGE_SECS).last().unwrap());
    let main = m.w.main.to_string();
    let head = m.ids[1].to_string();
    let unknown = "f".repeat(40);
    let on_no_chain = m.ids[0].to_string();
    let run = |pr: AnyPr, api: PrApi| plan_api(&m.w, vec![pr], api, &no_sim());
    let pr = |merge: &str| merged_pr("feat", 5, head.clone(), merge.to_string(), MERGE_SECS);
    let reason = |p: &RepoPlan| api_of(p, "feat").0;
    // no network, no token
    let p = run(pr(&main), PrApi::Off(ApiReason::Offline));
    assert_eq!(reason(&p), Some(ApiReason::Offline));
    assert_eq!(proof_of(&p, "feat"), MOVED);
    assert_eq!(
        via(&p, "feat"),
        (LandedKind::Unknown, Basis::None, Strength::None)
    );
    let p = run(pr(&main), PrApi::Off(ApiReason::NoToken));
    assert_eq!(reason(&p), Some(ApiReason::NoToken));
    // the pull request has no merge commit
    let routes = FakeRoutes::start(|_, _| (200, vec![], "[]".into()));
    let c = client(&routes);
    let p = run(pr(""), PrApi::Client(&c));
    assert_eq!(reason(&p), Some(ApiReason::NotMerged));
    // the merge commit is not in the clone, or not on the base
    let p = run(pr(&unknown), PrApi::Client(&c));
    assert_eq!(reason(&p), Some(ApiReason::MergeCommitNotInClone));
    let p = run(pr(&on_no_chain), PrApi::Client(&c));
    assert_eq!(reason(&p), Some(ApiReason::NotOnBase));
    // another branch's pull request: this branch has none that is merged
    let p = run(
        merged_pr("other", 6, head.clone(), main.clone(), MERGE_SECS),
        PrApi::Client(&c),
    );
    assert_eq!(reason(&p), Some(ApiReason::NoPullRequest));
    assert_eq!(proof_of(&p, "feat").1, "unmerged");
    // a full page, a server error, a rate limit
    let page = commits_body(&vec![m.ids[0]; 100], &vec![m.list[0].clone(); 100]);
    let routes = FakeRoutes::start(move |_, _| (200, vec![], page.clone()));
    let c = client(&routes);
    assert_eq!(
        reason(&run(pr(&main), PrApi::Client(&c))),
        Some(ApiReason::TooManyCommits)
    );
    let routes = FakeRoutes::start(|_, _| (500, vec![], "{}".into()));
    let c = client(&routes);
    assert_eq!(
        reason(&run(pr(&main), PrApi::Client(&c))),
        Some(ApiReason::RequestFailed)
    );
    let routes = FakeRoutes::start(|_, _| (429, vec![], "{}".into()));
    let c = client(&routes);
    let p = run(pr(&main), PrApi::Client(&c));
    assert_eq!(reason(&p), Some(ApiReason::RateLimited));
    assert!(p.error.is_none());
    // none of them changed what the local evidence says
    assert_eq!(report(&p, "feat").mapped, 2);
}

#[test]
fn a_strong_local_proof_and_a_head_proof_do_not_ask_the_api() {
    let mut w = world();
    w.fx.set_origin_url("https://github.com/o/r.git");
    let l = cs(0, 2);
    w.branch("strong", &l);
    w.land_resolved(&l, MERGE_SECS);
    let l2 = cs(2, 1);
    let ids = w.branch("headed", &l2);
    w.land(&l2, MERGE_SECS + 100);
    // an equal patch with another author time: no identity, so the mapping is no strong proof either
    let l3 = cs(3, 1);
    let ids3 = w.branch("cherry", &l3);
    let mut moved = l3.clone();
    moved[0].secs += 777;
    w.land(&moved, MERGE_SECS + 200);
    let routes = FakeRoutes::start(|_, _| (200, vec![], "[]".into()));
    let c = client(&routes);
    let main = w.main.to_string();
    let prs = vec![
        merged_pr("strong", 1, "0".repeat(40), main.clone(), MERGE_SECS),
        merged_pr("headed", 2, ids[0].to_string(), main.clone(), MERGE_SECS),
        merged_pr("cherry", 3, ids3[0].to_string(), main, MERGE_SECS),
    ];
    let p = plan_api(&w, prs, PrApi::Client(&c), &opts());
    assert_eq!(proof_of(&p, "strong"), ("review", "rebase-identity"));
    assert_eq!(proof_of(&p, "headed"), ("delete", "equal-patches"));
    assert!(routes.hits().is_empty(), "{:?}", routes.hits());
    assert_eq!(api_of(&p, "strong").0, Some(ApiReason::NotNeeded));
    assert_eq!(api_of(&p, "headed").0, Some(ApiReason::NotNeeded));
    assert_eq!(proof_of(&p, "cherry"), ("delete", "equal-patches"));
    assert_eq!(api_of(&p, "cherry").0, Some(ApiReason::NotNeeded));
}

#[test]
fn at_most_thirty_requests_are_made_per_repository() {
    let w = world();
    w.fx.set_origin_url("https://github.com/o/r.git");
    let mut prs = Vec::new();
    for i in 0..32 {
        let name = format!("b{i:02}");
        let mut one = c(i);
        one.secs = T0 + 100 * i as i64;
        let ids = w.branch(&name, &[one]);
        // the head differs from the tip: the branch moved
        prs.push(merged_pr(
            &name,
            100 + i as u64,
            "1".repeat(40),
            ids[0].to_string(),
            MERGE_SECS,
        ));
    }
    let routes = FakeRoutes::start(|_, _| (200, vec![], "[]".into()));
    let c = client(&routes);
    let p = plan_api(&w, prs, PrApi::Client(&c), &no_sim());
    assert!(p.error.is_none(), "{:?}", p.error);
    assert_eq!(routes.hits().len(), 30);
    let limited = p
        .branches
        .iter()
        .filter(|b| {
            b.rebase
                .as_ref()
                .is_some_and(|r| r.api.not_read == Some(ApiReason::RequestLimit))
        })
        .map(|b| b.branch.as_str())
        .collect::<Vec<_>>();
    assert_eq!(limited, ["b30", "b31"]);
}

// ---- the similarity stage, a shallow clone, the flag ----

#[test]
fn a_commit_with_identity_is_still_judged_by_the_similarity_stage_when_the_branch_is_no_proof() {
    let mut w = world();
    let list = cs(0, 2);
    w.branch("feat", &list);
    // the first commit landed with identity; the second as a similar commit with another author time
    w.land_resolved(&list[..1], MERGE_SECS);
    let mut second = resolved(&list[1]);
    second.secs += 500;
    w.land(&[second], MERGE_SECS + 100);
    let p = plan(&w);
    assert_eq!(proof_of(&p, "feat"), ("review", "similar-patches"));
    let r = report(&p, "feat");
    assert_eq!((r.mapped, r.by_identity), (1, 1));
    let s = get(&p, "feat").similarity.as_ref().unwrap();
    assert_eq!(s.report.plus_commits, 2, "both commits are compared");
}

/// 2 = delete, 1 = review, 0 = keep.
fn rank(p: &RepoPlan, branch: &str) -> u8 {
    match get(p, branch).decision {
        "delete" => 2,
        "review" => 1,
        _ => 0,
    }
}

#[test]
fn a_hint_of_the_rebase_stage_does_not_take_similar_patches_away() {
    let mut w = world();
    // landed in swapped order
    let a = cs(0, 2);
    w.branch("swapped", &a);
    w.land_resolved(&[a[1].clone()], MERGE_SECS);
    w.land_resolved(&[a[0].clone()], MERGE_SECS + 10);
    // a foreign commit between the landed commits
    let b = cs(2, 2);
    w.branch("foreign", &b);
    w.land_resolved(&b[..1], MERGE_SECS + 100);
    w.foreign(7);
    w.land_resolved(&b[1..], MERGE_SECS + 110);
    let p = plan(&w);
    for br in ["swapped", "foreign"] {
        assert_eq!(report(&p, br).strength, Strength::Hint, "{br}");
        assert_eq!(proof_of(&p, br), ("review", "similar-patches"), "{br}");
    }
}

#[test]
fn the_default_plan_is_never_weaker_than_the_plan_without_the_stage() {
    // one world with every kind of landing; every branch is compared
    let mut w = world();
    let mut n = 0;
    let mut take = |k: usize| {
        let l = cs(n, k);
        n += k;
        l
    };
    let l = take(2);
    w.branch("swapped", &l);
    w.land_resolved(&[l[1].clone()], MERGE_SECS);
    w.land_resolved(&[l[0].clone()], MERGE_SECS + 10);
    let l = take(2);
    w.branch("foreign", &l);
    w.land_resolved(&l[..1], MERGE_SECS + 20);
    w.foreign(7);
    w.land_resolved(&l[1..], MERGE_SECS + 30);
    let l = take(3);
    w.branch("partial", &l);
    w.land_resolved(&l[..2], MERGE_SECS + 40);
    let l = take(2);
    w.branch("clean", &l);
    w.land_resolved(&l, MERGE_SECS + 50);
    let l = take(2);
    w.branch("equal", &l);
    w.land(&l, MERGE_SECS + 60);
    let l = take(2);
    w.branch("mixed", &l);
    w.land_resolved(&l[..1], MERGE_SECS + 70);
    let mut second = resolved(&l[1]);
    second.secs += 500;
    w.land(&[second], MERGE_SECS + 80);
    let l = take(2);
    w.branch("untouched", &l);
    let on = plan(&w);
    let mut o = opts();
    o.rebase = false;
    let off = plan_with(&w, None, &o);
    for b in &on.branches {
        assert!(
            rank(&on, &b.branch) >= rank(&off, &b.branch),
            "{}: {:?} against {:?}",
            b.branch,
            proof_of(&on, &b.branch),
            proof_of(&off, &b.branch)
        );
    }
}

#[test]
fn a_shallow_clone_gives_a_plan_and_the_stage_says_where_the_walk_ended() {
    let fx = Fx::new();
    let s0 = Stamp::new("Other", "o@example.com", T0 - 20_000);
    let old = fx.commit_at(None, &[("old.txt", Some("old\n"))], "old", s0, s0);
    let s1 = Stamp::new("Other", "o@example.com", T0 - 10_000);
    let c0 = fx.commit_at(Some(old), &[("base.txt", Some("base\n"))], "base", s1, s1);
    fx.remote("main", c0);
    fx.origin_head("main");
    fx.branch("main", c0);
    fx.set_head("main");
    fx.checkout_head();
    let mut w = W { fx, c0, main: c0 };
    let list = cs(0, 2);
    w.branch("feat", &list);
    w.land_resolved(&list, MERGE_SECS);
    let git = w.fx.path.join(".git");
    std::fs::write(git.join("shallow"), format!("{c0}\n")).unwrap();
    let hex = old.to_string();
    std::fs::remove_file(git.join("objects").join(&hex[..2]).join(&hex[2..])).unwrap();
    let p = plan(&w);
    assert!(p.error.is_none(), "{:?}", p.error);
    assert_eq!(proof_of(&p, "feat"), ("review", "rebase-identity"));
    let v = json_of(&p);
    let win = &branch_json(&v, "feat")["rebase"]["window"];
    assert_eq!(win["stop"], "missing");
    assert_eq!(win["stopped_at"], old.to_string().as_str());
}

pub(crate) fn stw_cli(home: &std::path::Path, args: &[&str]) -> std::process::Output {
    std::process::Command::new(env!("CARGO_BIN_EXE_stw"))
        .fixture_env_clear()
        .env("HOME", home)
        .env("GIT_CONFIG_GLOBAL", "/dev/null")
        .env("GIT_CONFIG_SYSTEM", "/dev/null")
        .env("GIT_CONFIG_NOSYSTEM", "1")
        .args(args)
        .output()
        .unwrap()
}

pub(crate) fn cli_json(w: &W, extra: &[&str]) -> Value {
    let home = scratch();
    let r = path_str(&w.fx.path);
    let mut args = vec!["tidy", "plan", "--offline", "--json", "--root", r.as_str()];
    args.extend_from_slice(extra);
    let o = stw_cli(home.path(), &args);
    assert_eq!(
        o.status.code(),
        Some(0),
        "{}",
        String::from_utf8_lossy(&o.stderr)
    );
    serde_json::from_slice(&o.stdout).unwrap()
}

#[test]
fn the_flag_no_rebase_evidence_switches_the_stage_off_and_is_independent_of_the_other() {
    let mut w = world();
    let list = cs(0, 2);
    w.branch("feat", &list);
    w.land_resolved(&list, MERGE_SECS);
    let on = cli_json(&w, &[]);
    let b = branch_json(&on, "feat");
    assert_eq!(b["proof"], "rebase-identity");
    assert_eq!(b["landed_via"]["kind"], "rebase");
    assert_eq!(on["summary"]["rebase"], 1);
    let off = cli_json(&w, &["--no-rebase-evidence"]);
    let b = branch_json(&off, "feat");
    assert_eq!(b["proof"], "similar-patches");
    assert!(b["rebase"].is_null());
    assert_eq!(b["landed_via"]["kind"], "unknown");
    assert_eq!(off["summary"]["rebase"], 0);
    let neither = cli_json(&w, &["--no-rebase-evidence", "--no-similarity"]);
    assert_eq!(branch_json(&neither, "feat")["proof"], "unmerged");
    let only = cli_json(&w, &["--no-similarity"]);
    assert_eq!(branch_json(&only, "feat")["proof"], "rebase-identity");
}

// ---- defaults, reasons from fetch_proof, apply, the client reaching the stage ----

#[test]
fn the_default_options_run_both_stages() {
    let o = PlanOpts::default();
    assert!(o.rebase);
    assert!(o.similarity.is_some());
}

#[test]
fn without_a_client_the_reason_says_offline_or_no_token() {
    let w = world();
    w.fx.set_origin_url("https://github.com/o/r.git");
    w.branch("plain", &cs(0, 1));
    let reason = |offline: bool| {
        let p = stw::tidy::plan_repo(&rr(&w), None, offline, &opts());
        assert!(p.error.is_none(), "{:?}", p.error);
        api_of(&p, "plain").0
    };
    assert_eq!(reason(true), Some(ApiReason::Offline));
    assert_eq!(reason(false), Some(ApiReason::NoToken));
}

#[test]
fn apply_plans_without_the_rebase_stage() {
    let mut w = world();
    let list = cs(0, 2);
    w.branch("feat", &list);
    w.land_resolved(&list, MERGE_SECS);
    let home = scratch();
    let r = path_str(&w.fx.path);
    let plan = stw_cli(
        home.path(),
        &[
            "tidy",
            "plan",
            "--offline",
            "--json",
            "--no-similarity",
            "--root",
            &r,
        ],
    );
    assert_eq!(plan.status.code(), Some(0));
    let mut v: Value = serde_json::from_slice(&plan.stdout).unwrap();
    assert_eq!(v["repos"][0]["branches"][0]["proof"], "rebase-identity");
    // the plan file says `delete` for it
    v["repos"][0]["branches"][0]["decision"] = "delete".into();
    let file = home.path().join("plan.json");
    std::fs::write(&file, v.to_string()).unwrap();
    let md = path_str(&home.path().join("m"));
    let o = stw_cli(
        home.path(),
        &[
            "tidy",
            "apply",
            "--json",
            "--offline",
            "--root",
            &r,
            "--plan",
            &path_str(&file),
            "--manifest-dir",
            &md,
        ],
    );
    let out: Value = serde_json::from_slice(&o.stdout).unwrap();
    let text = out.to_string();
    assert!(
        text.contains("the proof no longer holds: now keep [unmerged]"),
        "{text}"
    );
    assert!(stw::repo::resolve(&w.fx.repo(), "refs/heads/feat").is_some());
}

#[test]
fn the_client_of_fetch_proof_reaches_the_stage() {
    let m = moved(|w, l| *w.land_resolved(l, MERGE_SECS).last().unwrap());
    let main = m.w.main.to_string();
    let commits = commits_body(&m.ids[..2], &m.list[..2]);
    let list = format!(
        r#"[{{"number":5,"state":"closed","title":"t","merged_at":"{}","merge_commit_sha":"{main}","head":{{"ref":"feat","sha":"{}"}}}}]"#,
        iso(MERGE_SECS),
        m.ids[1]
    );
    let routes = FakeRoutes::start(move |path, _| {
        if path.ends_with("/commits") {
            (200, vec![], commits.clone())
        } else {
            (200, vec![], list.clone())
        }
    });
    let c = client(&routes);
    let p = stw::tidy::plan_repo(&rr(&m.w), Some(&c), false, &no_sim());
    assert!(p.error.is_none(), "{:?}", p.error);
    assert_eq!(api_of(&p, "feat").0, None, "{:?}", routes.hits());
    assert_eq!(
        via(&p, "feat"),
        (LandedKind::Rebase, Basis::PrApi, Strength::Hint)
    );
}
