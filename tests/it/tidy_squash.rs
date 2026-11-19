//! Squash evidence, layer 1, of `stw tidy plan` (package 5a): the merged pull request of a branch and the subject of
//! the commits on the base. Fixtures are built with gix only. Nothing here proves content: layer 2 does (package 5b).
use crate::common::*;
use crate::tidy_rebase_plan::*;
use gix::ObjectId;
use stw::github::AnyPr;
use stw::tidy_squash::{evaluate_pr, pick_pr};

/// The author of a squash commit: the merger's merge time, not the time of the branch commits.
const SQUASH_SECS: i64 = MERGE_SECS;

/// One commit on `parent` (default: main) with the files of all commits of `list`, written the way a squash merge does.
fn squash_on(w: &W, parent: Option<ObjectId>, list: &[C], subject: &str, body: &str) -> ObjectId {
    let edits: Vec<(&str, Option<&str>)> = list
        .iter()
        .map(|c| (c.file.as_str(), Some(c.body.as_str())))
        .collect();
    let gh = Stamp::new("GitHub", "noreply@github.com", SQUASH_SECS);
    let author = Stamp::new(NAME, MAIL, SQUASH_SECS);
    let msg = if body.is_empty() {
        subject.to_string()
    } else {
        format!("{subject}\n\n{body}")
    };
    w.fx.commit_at(Some(parent.unwrap_or(w.main)), &edits, msg, author, gh)
}

/// A squash as GitHub lands it: one commit on main, main moves.
pub fn land_squash(w: &mut W, list: &[C], subject: &str, body: &str) -> ObjectId {
    let id = squash_on(w, None, list, subject, body);
    w.set_main(id);
    id
}

fn pr(number: u64, head: ObjectId, merge: &str) -> AnyPr {
    AnyPr {
        head_ref: "feat".into(),
        head_sha: head.to_string(),
        number,
        state: "MERGED",
        merge_commit_sha: merge.into(),
        merged_at: Some(SQUASH_SECS),
        title: "t".into(),
        head_repo: "test/fixture".into(),
    }
}

// ---- the pull request ----

#[test]
fn a_squash_merge_commit_on_the_base_with_one_parent_is_found_and_the_head_is_the_tip() {
    let mut w = world();
    let list = cs(0, 3);
    let ids = w.branch("feat", &list);
    let m = land_squash(&mut w, &list, "step 0 (#7)", "");
    let e = evaluate_pr(&w.fx.repo(), w.main, ids[2], &pr(7, ids[2], &m.to_string()));
    assert_eq!(e.not_read, None);
    assert_eq!(e.number, Some(7));
    assert_eq!(e.merge_commit, Some(m));
    assert_eq!(e.merge_commit_in_clone, Some(true));
    assert_eq!(e.merge_commit_parents, Some(1));
    assert_eq!(e.merge_commit_on_base, Some(true));
    assert_eq!(e.head_is_tip, Some(true));
    assert_eq!(e.head_in_branch, Some(true));
    assert_eq!(e.commits_after_head, Some(0));
}

#[test]
fn a_merge_commit_that_is_not_an_ancestor_of_the_base_is_not_on_the_base() {
    // the test merge of GitHub (`refs/pull/N/merge`) is in the clone, but main does not contain it
    let mut w = world();
    let list = cs(0, 2);
    let ids = w.branch("feat", &list);
    let side = squash_on(&w, None, &list, "test merge", "");
    w.foreign(1);
    let e = evaluate_pr(
        &w.fx.repo(),
        w.main,
        ids[1],
        &pr(7, ids[1], &side.to_string()),
    );
    assert_eq!(e.merge_commit_in_clone, Some(true));
    assert_eq!(e.merge_commit_parents, Some(1));
    assert_eq!(e.merge_commit_on_base, Some(false));
}

#[test]
fn a_merge_commit_with_two_parents_is_no_squash_even_on_the_base() {
    let mut w = world();
    let list = cs(0, 2);
    let ids = w.branch("feat", &list);
    let main_before = w.main;
    let s = Stamp::new("GitHub", "noreply@github.com", SQUASH_SECS);
    let merge = w.fx.commit_at_parents(
        vec![main_before, ids[1]],
        Some(ids[1]),
        &[],
        "Merge pull request #7",
        s,
        s,
    );
    w.set_main(merge);
    let e = evaluate_pr(
        &w.fx.repo(),
        w.main,
        ids[1],
        &pr(7, ids[1], &merge.to_string()),
    );
    assert_eq!(e.merge_commit_parents, Some(2));
    assert_eq!(e.merge_commit_on_base, Some(false));
}

#[test]
fn a_merge_commit_that_is_not_in_the_clone_leaves_the_answers_open() {
    let mut w = world();
    let list = cs(0, 2);
    let ids = w.branch("feat", &list);
    w.foreign(1);
    let gone = "ab".repeat(20);
    let e = evaluate_pr(&w.fx.repo(), w.main, ids[1], &pr(7, ids[1], &gone));
    assert_eq!(e.merge_commit, Some(gone.parse().unwrap()));
    assert_eq!(e.merge_commit_in_clone, Some(false));
    assert_eq!(e.merge_commit_on_base, None);
    assert_eq!(e.merge_commit_parents, None);
    // the head answers do not depend on the merge commit
    assert_eq!(e.head_is_tip, Some(true));
}

#[test]
fn a_pull_request_without_a_usable_merge_commit_has_no_merge_answers() {
    let mut w = world();
    let ids = w.branch("feat", &cs(0, 1));
    w.foreign(1);
    for bad in ["", "not-a-sha"] {
        let e = evaluate_pr(&w.fx.repo(), w.main, ids[0], &pr(7, ids[0], bad));
        assert_eq!(
            (
                e.merge_commit,
                e.merge_commit_in_clone,
                e.merge_commit_on_base
            ),
            (None, None, None),
            "{bad:?}"
        );
        assert_eq!(e.number, Some(7));
    }
}

#[test]
fn commits_after_the_head_are_counted_and_a_head_outside_the_branch_is_named() {
    let mut w = world();
    let list = cs(0, 4);
    let ids = w.branch("feat", &list);
    let m = land_squash(&mut w, &list[..2], "step 0 (#7)", "");
    // the pull request ended at the second commit, the branch went on with two more
    let e = evaluate_pr(&w.fx.repo(), w.main, ids[3], &pr(7, ids[1], &m.to_string()));
    assert_eq!(e.head_is_tip, Some(false));
    assert_eq!(e.head_in_branch, Some(true));
    assert_eq!(e.commits_after_head, Some(2));
    // a head that the branch does not contain (the branch was rewritten)
    let other = w.commits_on(
        None,
        &[C {
            subject: "other".into(),
            ..c(9)
        }],
    );
    let e = evaluate_pr(
        &w.fx.repo(),
        w.main,
        ids[3],
        &pr(7, other[0], &m.to_string()),
    );
    assert_eq!(e.head_in_branch, Some(false));
    assert_eq!(e.commits_after_head, None);
    // a head that is not in the clone at all
    let e = evaluate_pr(
        &w.fx.repo(),
        w.main,
        ids[3],
        &pr(7, ObjectId::null(gix::hash::Kind::Sha1), &m.to_string()),
    );
    assert_eq!(e.head_in_branch, None);
    assert_eq!(e.commits_after_head, None);
}

// ---- which pull request ----

fn with(mut p: AnyPr, f: impl Fn(&mut AnyPr)) -> AnyPr {
    f(&mut p);
    p
}

#[test]
fn only_a_merged_pull_request_of_the_repository_itself_counts() {
    let id = ObjectId::null(gix::hash::Kind::Sha1);
    let base = pr(5, id, "");
    let list = vec![
        with(base.clone(), |p| {
            p.number = 1;
            p.head_repo = "fork/fixture".into()
        }),
        with(base.clone(), |p| {
            p.number = 2;
            p.head_repo = String::new()
        }),
        with(base.clone(), |p| {
            p.number = 3;
            p.state = "OPEN"
        }),
        with(base.clone(), |p| {
            p.number = 4;
            p.state = "CLOSED"
        }),
        with(base.clone(), |p| {
            p.number = 6;
            p.head_ref = "other".into()
        }),
    ];
    // a fork with the same branch name, a deleted fork, an open, a closed and another branch's pull request: none
    assert!(pick_pr(&list, "feat", Some("test/fixture"), None).is_none());
    // without a slug nothing can be compared: the fork counts (the latest of the merged ones, the higher number)
    assert_eq!(
        pick_pr(&list, "feat", None, None).map(|p| p.number),
        Some(2)
    );
    // the name of the repository is compared without regard to case
    let mut l = list.clone();
    l.push(with(base.clone(), |p| p.head_repo = "Test/Fixture".into()));
    assert_eq!(
        pick_pr(&l, "feat", Some("test/fixture"), None).map(|p| p.number),
        Some(5)
    );
}

#[test]
fn of_several_merged_pull_requests_the_preferred_one_or_the_latest_counts() {
    let id = ObjectId::null(gix::hash::Kind::Sha1);
    let at = |n: u64, t: i64| {
        with(pr(n, id, ""), |p| {
            p.merged_at = Some(t);
        })
    };
    let list = vec![at(8, 100), at(9, 300), at(7, 300), at(6, 200)];
    let n = |p: Option<&AnyPr>| p.map(|p| p.number);
    // the latest merge time; the higher number on a tie
    assert_eq!(
        n(pick_pr(&list, "feat", Some("test/fixture"), None)),
        Some(9)
    );
    // a number the plan already uses wins
    assert_eq!(
        n(pick_pr(&list, "feat", Some("test/fixture"), Some(6))),
        Some(6)
    );
    // a preferred number that is no usable pull request is ignored
    assert_eq!(
        n(pick_pr(&list, "feat", Some("test/fixture"), Some(99))),
        Some(9)
    );
}
