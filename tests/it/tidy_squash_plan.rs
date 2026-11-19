//! The squash stage of `stw tidy plan` (layer 1, package 5a): what is in `BranchPlan::squash`, for which branches, and
//! that the decision never depends on it. The text of the stage in `--explain` and the JSON are in the same file below.
use crate::common::*;
use crate::tidy_rebase_plan::{json_of, *};
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
    // a similar-patches branch gets the object too: the same change by somebody else with one line resolved
    let mut w = world();
    let t = w.commits_on(None, &[c(0)])[0];
    w.fx.branch("sim", t);
    let r = resolved(&c(0));
    let other = Stamp::new("Other", "o@example.com", MERGE_SECS);
    let m = w.fx.commit_at(
        Some(w.main),
        &[(r.file.as_str(), Some(r.body.as_str()))],
        "something else",
        other,
        other,
    );
    w.set_main(m);
    let p = plan_off(&w, ApiReason::Offline);
    let b = get(&p, "sim");
    assert_eq!((b.decision, b.proof), ("review", "similar-patches"));
    assert!(b.squash.is_some());
    // a strong rebase did not land as a squash
    let mut w = world();
    let list = cs(0, 2);
    w.branch("rb", &list);
    w.land_resolved(&list, MERGE_SECS);
    let p = plan_off(&w, ApiReason::Offline);
    assert_eq!(get(&p, "rb").proof, "rebase-identity");
    assert!(get(&p, "rb").squash.is_none());
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

// ---- the JSON ----

use serde_json::Value;

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

#[test]
fn the_json_has_the_squash_object_with_the_pull_request_and_the_subject() {
    let (w, ids, m) = moved();
    let p = plan_with(&w, Some(vec![pr(7, ids[1], m)]), &opts());
    let v = json_of(&p);
    assert_eq!(v["schema_version"], 2);
    let b = branch_json(&v, "feat");
    let s = &b["squash"];
    assert_eq!(key_list(s), sorted(&["pr", "subject"]));
    assert_eq!(
        key_list(&s["pr"]),
        sorted(&[
            "state",
            "reason",
            "number",
            "merged",
            "merge_commit",
            "merge_commit_in_clone",
            "merge_commit_on_base",
            "merge_commit_parents",
            "head_is_tip",
            "head_in_branch",
            "commits_after_head"
        ])
    );
    let pr = &s["pr"];
    assert_eq!(pr["state"], "read");
    assert_eq!(pr["reason"], Value::Null);
    assert_eq!(pr["number"], 7);
    assert_eq!(pr["merged"], true);
    assert_eq!(pr["merge_commit"], m.to_string());
    assert_eq!(pr["merge_commit_in_clone"], true);
    assert_eq!(pr["merge_commit_on_base"], true);
    assert_eq!(pr["merge_commit_parents"], 1);
    assert_eq!(pr["head_is_tip"], false);
    assert_eq!(pr["head_in_branch"], true);
    assert_eq!(pr["commits_after_head"], 1);
    assert_eq!(
        key_list(&s["subject"]),
        sorted(&[
            "candidate",
            "basis",
            "candidate_subject",
            "candidates",
            "pr_number_in_subject",
            "subject_equal",
            "subjects_total",
            "subjects_in_body",
            "pull_request_trailer",
            "window"
        ])
    );
    let sub = &s["subject"];
    assert_eq!(sub["candidate"], m.to_string());
    assert_eq!(sub["basis"], "merge-commit");
    assert_eq!(sub["candidate_subject"], "step 0 (#7)");
    assert_eq!(sub["candidates"], 1);
    assert_eq!(sub["pr_number_in_subject"], 7);
    assert_eq!(sub["subject_equal"], true);
    assert_eq!(sub["subjects_total"], 3);
    assert_eq!(sub["subjects_in_body"], 0);
    assert_eq!(sub["pull_request_trailer"], false);
    assert_eq!(
        key_list(&sub["window"]),
        sorted(&["days", "commits", "cut"])
    );
    assert_eq!(sub["window"]["days"], 7);
    assert_eq!(sub["window"]["cut"], false);
}

#[test]
fn the_json_names_the_reason_where_the_pull_request_was_not_read_and_leaves_the_rest_null() {
    let mut w = world();
    let list = cs(0, 2);
    w.branch("feat", &list);
    land_squash(&mut w, &list, "other words", "");
    let p = plan_off(&w, ApiReason::NoToken);
    let v = json_of(&p);
    let s = &branch_json(&v, "feat")["squash"];
    let pr = &s["pr"];
    assert_eq!(pr["state"], "not-read");
    assert_eq!(pr["reason"], "no-token");
    for k in [
        "number",
        "merged",
        "merge_commit",
        "merge_commit_in_clone",
        "merge_commit_on_base",
        "merge_commit_parents",
        "head_is_tip",
        "head_in_branch",
        "commits_after_head",
    ] {
        assert_eq!(pr[k], Value::Null, "{k}");
    }
    // nothing found on the base: the keys are there, the candidate is null
    let sub = &s["subject"];
    assert_eq!(sub["candidate"], Value::Null);
    assert_eq!(sub["basis"], Value::Null);
    assert_eq!(sub["candidate_subject"], Value::Null);
    assert_eq!(sub["candidates"], 0);
    assert_eq!(sub["subject_equal"], false);
    assert_eq!(sub["subjects_total"], 2);
}

#[test]
fn the_json_of_a_branch_the_stage_left_out_has_a_null_squash() {
    let mut w = world();
    let list = cs(0, 2);
    w.branch("feat", &list);
    w.land_resolved(&list, MERGE_SECS);
    let p = plan_off(&w, ApiReason::Offline);
    let v = json_of(&p);
    let b = branch_json(&v, "feat");
    assert_eq!(b["proof"], "rebase-identity");
    assert!(b["squash"].is_null());
    assert!(b.as_object().unwrap().contains_key("squash"));
}

// ---- --explain ----

fn under(text: &str, branch: &str) -> Vec<String> {
    let mut it = text.lines();
    let marker = format!("  {branch}  [");
    for l in it.by_ref() {
        if l.contains(&marker) {
            break;
        }
    }
    it.take_while(|l| l.starts_with("    "))
        .map(|l| l[4..].to_string())
        .collect()
}

fn squash_lines(p: &stw::tidy::RepoPlan, branch: &str) -> Vec<String> {
    let t = stw::tidy::render_explained(std::slice::from_ref(p), true);
    under(&t, branch)
        .into_iter()
        .filter(|l| l.starts_with("squash: "))
        .collect()
}

#[test]
fn explain_tells_what_the_pull_request_and_the_subject_found() {
    let (w, ids, m) = moved();
    let p = plan_with(&w, Some(vec![pr(7, ids[1], m)]), &opts());
    let s7 = &m.to_string()[..7];
    assert_eq!(
        squash_lines(&p, "feat"),
        vec![
            format!("squash: pull request #7 merged, merge commit {s7} is on origin/main with 1 parent(s); the head is in the branch, 1 commit(s) after it"),
            format!("squash: subject: candidate {s7} 'step 0 (#7)' (the merge commit of the pull request), 1 candidate(s), number #7 in the subject, title equal to a branch commit: yes, 0 of 3 branch subject(s) listed in its body, Pull-request trailer: no"),
        ]
    );
}

#[test]
fn explain_names_the_reason_the_pull_request_was_not_read_and_the_subject_basis() {
    let mut w = world();
    let list = cs(0, 2);
    w.branch("feat", &list);
    let hit = land_squash(
        &mut w,
        &list,
        "step 1 (#9)",
        "* step 0\n* step 1\n\nPull-request: https://github.com/test/fixture/pull/9",
    );
    let p = plan_off(&w, ApiReason::Offline);
    let s7 = &hit.to_string()[..7];
    assert_eq!(
        squash_lines(&p, "feat"),
        vec![
            "squash: pull request not read (offline)".to_string(),
            format!("squash: subject: candidate {s7} 'step 1 (#9)' (same title as a commit of the branch), 1 candidate(s), number #9 in the subject, title equal to a branch commit: yes, 2 of 2 branch subject(s) listed in its body, Pull-request trailer: no"),
        ]
    );
}

#[test]
fn explain_says_so_where_there_is_no_candidate() {
    let mut w = world();
    w.branch("feat", &cs(0, 2));
    w.foreign(1);
    let p = plan_off(&w, ApiReason::NoToken);
    assert_eq!(
        squash_lines(&p, "feat"),
        vec![
            "squash: pull request not read (no-token)".to_string(),
            "squash: subject: no commit on origin/main with a title of one of the 2 branch commit(s)"
                .to_string(),
        ]
    );
}

#[test]
fn explain_says_where_the_walk_over_the_base_was_cut() {
    let w = world();
    w.branch("feat", &cs(0, 1));
    let mut p = plan_off(&w, ApiReason::Offline);
    let sq = p.branches[0].squash.as_mut().expect("squash object");
    sq.subject.window_cut = true;
    sq.subject.window_commits = 50_000;
    assert_eq!(
        squash_lines(&p, "feat").last().unwrap(),
        "squash: the walk over origin/main stopped after 50000 commit(s) (limit): older commits were not read"
    );
}

#[test]
fn explain_describes_a_merge_commit_that_is_no_squash_or_not_there() {
    let (w, ids, _) = moved();
    let side = w.commits_on(
        None,
        &[C {
            subject: "side".into(),
            ..c(8)
        }],
    )[0];
    let p = plan_with(&w, Some(vec![pr(7, ids[1], side)]), &opts());
    assert_eq!(
        squash_lines(&p, "feat")[0],
        format!(
            "squash: pull request #7 merged, merge commit {} is not on origin/main; the head is in the branch, 1 commit(s) after it",
            &side.to_string()[..7]
        )
    );
    let (w, ids, _) = moved();
    let mut gone = pr(7, ids[1], ids[1]);
    gone.merge_commit_sha = "ab".repeat(20);
    let p = plan_with(&w, Some(vec![gone]), &opts());
    assert_eq!(
        squash_lines(&p, "feat")[0],
        "squash: pull request #7 merged, merge commit abababa is not in the clone; the head is in the branch, 1 commit(s) after it"
    );
}

#[test]
fn explain_masks_the_subject_of_the_candidate() {
    let mut w = world();
    let ids = w.branch("feat", &cs(0, 1));
    let hit = w.fx.commit_at(
        Some(w.main),
        &[("x.txt", Some("x\n"))],
        b"ti\x1b[31mtle \xe2\x80\xae (#7)",
        Stamp::new("Other", "o@example.com", T0 + 30),
        Stamp::new("Other", "o@example.com", T0 + 30),
    );
    w.set_main(hit);
    let mut q = pr(7, ids[0], hit);
    q.head_sha = "0".repeat(40);
    let p = plan_with(&w, Some(vec![q]), &opts());
    let l = squash_lines(&p, "feat").join("\n");
    assert!(l.contains("candidate"), "{l}");
    assert!(!l.contains('\u{1b}') && !l.contains('\u{202e}'), "{l:?}");
}
