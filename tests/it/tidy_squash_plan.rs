//! The squash stage of `stw tidy plan` (layer 1, package 5a): what is in `BranchPlan::squash`, for which branches, and
//! that the decision never depends on it. The text of the stage in `--explain` and the JSON are in the same file below.
use crate::common::*;
use crate::tidy_rebase_plan::*;
use crate::tidy_squash::{land_squash, on_main};
use gix::ObjectId;
use stw::github::AnyPr;
use stw::tidy::{plan_repo_with_opts, PrApi, PrProof};
use stw::tidy_rebase::ApiReason;
use stw::tidy_squash::SubjectBasis;

fn pr(n: u64, head: ObjectId, merge: ObjectId) -> AnyPr {
    AnyPr {
        head_ref: "feat".into(),
        head_sha: head.to_string(),
        number: n,
        state: "MERGED",
        merge_commit_sha: merge.to_string(),
        merged_at: Some(MERGE_SECS),
        title: "t".into(),
        head_repo: "test/fixture".into(),
    }
}

/// The plan without pull requests: the reason is the one the proof carries.
fn plan_off(w: &W, reason: ApiReason) -> stw::tidy::RepoPlan {
    plan_repo_with_opts(
        &rr(w),
        PrProof {
            prs: None,
            note: "off".into(),
            api: PrApi::Off(reason),
        },
        &opts(),
    )
}

/// A branch of three commits; the pull request ended after the second, a squash of the first two landed (`#7`).
fn moved() -> (W, Vec<ObjectId>, ObjectId) {
    let mut w = world();
    let list = cs(0, 3);
    let ids = w.branch("feat", &list);
    let m = land_squash(&mut w, &list[..2], "step 0 (#7)", "");
    (w, ids, m)
}

#[test]
fn a_branch_that_moved_after_its_merged_pull_request_has_the_evidence_and_names_the_squash_in_the_detail(
) {
    let (w, ids, m) = moved();
    let p = plan_with(&w, Some(vec![pr(7, ids[1], m)]), &opts());
    let b = get(&p, "feat");
    assert_eq!(
        (b.decision, b.proof),
        ("review", "pull-request-merged-branch-moved")
    );
    let s = b.squash.as_ref().expect("squash object");
    assert_eq!(s.pr.not_read, None);
    assert_eq!(s.pr.number, Some(7));
    assert_eq!(s.pr.merge_commit_on_base, Some(true));
    assert_eq!(s.pr.commits_after_head, Some(1));
    assert_eq!(s.subject.candidate, Some(m));
    assert_eq!(s.subject.basis, Some(SubjectBasis::MergeCommit));
    assert_eq!(
        b.detail,
        format!(
            "pull request #7 merged, but the branch has 3 commit(s) not in refs/remotes/origin/main; pull request merge commit {} on refs/remotes/origin/main, 1 commit(s) after the pull request head",
            &m.to_string()[..7]
        )
    );
}

#[test]
fn the_detail_names_nothing_where_the_merge_commit_is_not_on_the_base_or_the_head_is_not_in_the_branch(
) {
    // the merge commit is a commit that main does not contain
    let (w, ids, _) = moved();
    let side = w.commits_on(
        None,
        &[C {
            subject: "side".into(),
            ..c(8)
        }],
    )[0];
    let p = plan_with(&w, Some(vec![pr(7, ids[1], side)]), &opts());
    let b = get(&p, "feat");
    assert_eq!(
        b.squash.as_ref().unwrap().pr.merge_commit_on_base,
        Some(false)
    );
    assert_eq!(
        b.detail,
        "pull request #7 merged, but the branch has 3 commit(s) not in refs/remotes/origin/main"
    );
    // the head is not in the branch
    let (w, ids, m) = moved();
    let other = w.commits_on(
        None,
        &[C {
            subject: "other".into(),
            ..c(9)
        }],
    )[0];
    let p = plan_with(&w, Some(vec![pr(7, other, m)]), &opts());
    assert_eq!(
        get(&p, "feat").detail,
        format!(
            "pull request #7 merged, but the branch has 3 commit(s) not in refs/remotes/origin/main; pull request merge commit {} on refs/remotes/origin/main, the pull request head is not in the branch",
            &m.to_string()[..7]
        )
    );
    let _ = ids;
}

#[test]
fn without_a_pull_request_list_the_stage_still_runs_on_the_subjects_and_names_the_reason() {
    let mut w = world();
    let list = cs(0, 2);
    w.branch("feat", &list);
    let hit = land_squash(&mut w, &list, "step 0 (#7)", "");
    for reason in [
        ApiReason::Offline,
        ApiReason::NoToken,
        ApiReason::RateLimited,
        ApiReason::RequestFailed,
        ApiReason::NoPullRequest,
    ] {
        let p = plan_off(&w, reason);
        assert!(p.error.is_none(), "{:?}", p.error);
        let b = get(&p, "feat");
        // the squash is no proof: the branch is as unmerged as before
        assert_eq!((b.decision, b.proof), ("keep", "unmerged"), "{reason:?}");
        let s = b.squash.as_ref().expect("squash object");
        assert_eq!(s.pr.not_read, Some(reason));
        assert_eq!(s.pr.number, None);
        assert_eq!(s.subject.candidate, Some(hit));
        assert_eq!(s.subject.basis, Some(SubjectBasis::Subject));
        assert_eq!(s.subject.pr_number_in_subject, Some(7));
    }
}

#[test]
fn a_list_without_a_usable_pull_request_for_the_branch_says_so() {
    let (w, ids, m) = moved();
    let mut fork = pr(7, ids[1], m);
    fork.head_repo = "fork/fixture".into();
    let mut closed = pr(8, ids[1], m);
    closed.state = "CLOSED";
    let p = plan_with(&w, Some(vec![fork, closed]), &opts());
    let b = get(&p, "feat");
    assert_eq!(
        b.squash.as_ref().unwrap().pr.not_read,
        Some(ApiReason::NoPullRequest)
    );
    // the squash evidence does not touch what the fork pull request already meant for the proof (name only, as before)
    assert_eq!(b.proof, "pull-request-merged-branch-moved");
}

#[test]
fn the_stage_leaves_out_what_a_proof_or_a_gate_already_settled() {
    let mut w = world();
    let list = cs(0, 2);
    // merged-ancestor, equal-patches, open pull request, backup, merged head
    let ids = w.branch("feat", &list);
    w.fx.branch("anc", w.c0);
    w.fx.branch("backup/old", ids[1]);
    w.fx.branch("open", ids[1]);
    w.fx.branch("headmerged", ids[1]);
    let landed = w.land(&list, MERGE_SECS);
    let _ = landed;
    let mut o = pr(1, ids[1], ids[1]);
    o.head_ref = "open".into();
    o.state = "OPEN";
    let mut h = pr(2, ids[1], ids[1]);
    h.head_ref = "headmerged".into();
    let p = plan_with(&w, Some(vec![o, h]), &opts());
    for br in ["anc", "backup/old", "open", "headmerged"] {
        assert!(
            get(&p, br).squash.is_none(),
            "{br}: {:?}",
            get(&p, br).proof
        );
    }
    // the rebase landed `feat` and `backup/old` (equal patches): no squash object either
    assert_eq!(get(&p, "feat").proof, "equal-patches");
    assert!(get(&p, "feat").squash.is_none());
}

#[test]
fn a_branch_in_a_worktree_keeps_the_object_and_a_stage_proof_keeps_its_place() {
    let mut w = world();
    let list = cs(0, 2);
    w.branch("feat", &list);
    land_squash(&mut w, &list, "step 0 (#7)", "");
    add_worktree(&w.fx, "wt", "feat", true, false);
    let p = plan_off(&w, ApiReason::Offline);
    let b = get(&p, "feat");
    assert_eq!((b.decision, b.proof), ("keep", "in-worktree"));
    assert!(b.squash.is_some());
    // a similar-patches branch gets the object too
    let mut w = world();
    let t = w.commits_on(None, &[c(0)])[0];
    w.fx.branch("sim", t);
    // the same change as one more commit on main with a different line: 97.5 %
    w.land_resolved(&[c(0)], MERGE_SECS);
    let p = plan_off(&w, ApiReason::Offline);
    let b = get(&p, "sim");
    assert!(
        b.proof == "similar-patches" || b.proof == "rebase-identity",
        "{}",
        b.proof
    );
    assert!(b.squash.is_some());
}

#[test]
fn the_decision_is_the_same_whatever_the_stage_finds() {
    // a squash that looks perfect on the subject changes neither the decision nor the proof
    let mut w = world();
    let list = cs(0, 3);
    w.branch("feat", &list);
    let before = plan_off(&w, ApiReason::Offline);
    land_squash(&mut w, &list, "step 0 (#7)", "* step 0\n* step 1\n* step 2");
    let after = plan_off(&w, ApiReason::Offline);
    let v = |p: &stw::tidy::RepoPlan| (get(p, "feat").decision, get(p, "feat").proof);
    assert_eq!(v(&before), v(&after));
    assert_eq!(v(&after), ("keep", "unmerged"));
    let s = get(&after, "feat").squash.as_ref().unwrap();
    assert_eq!(s.subject.subjects_in_body, 3);
    // a different squash of the same title
    let _ = on_main;
}
