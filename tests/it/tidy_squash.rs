//! Squash evidence, layer 1, of `stw tidy plan` (package 5a): the merged pull request of a branch and the subject of
//! the commits on the base. Fixtures are built with gix only. Nothing here proves content: layer 2 does (package 5b).
use crate::common::*;
use crate::tidy_rebase_plan::*;
use gix::ObjectId;
use stw::github::AnyPr;
use stw::tidy_squash::{
    branch_commits, evaluate_pr, evaluate_subject, normalize, pick_pr, pr_number_suffix, PrRef,
    SquashIndex, SubjectBasis, SubjectEvidence,
};

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

// ---- the subject ----

#[test]
fn the_number_of_a_pull_request_is_read_from_the_end_of_a_subject() {
    let n = |s: &str| pr_number_suffix(s.as_bytes());
    assert_eq!(n("Title (#7)"), Some(7));
    assert_eq!(n("Title(#7)"), Some(7));
    assert_eq!(n("Title (#7)  "), Some(7));
    assert_eq!(n("Fix (#1) and (#2)"), Some(2));
    assert_eq!(n("Fix (#1) and more"), None);
    assert_eq!(n("(#7) Title"), None);
    assert_eq!(n("Title (#)"), None);
    assert_eq!(n("Title (#7x)"), None);
    assert_eq!(n("Title (# 7)"), None);
    assert_eq!(n("Title (#-7)"), None);
    assert_eq!(n("Title (#99999999999999999999999)"), None);
    assert_eq!(n("Title"), None);
    assert_eq!(n(""), None);
}

#[test]
fn a_subject_is_reduced_to_its_title_before_two_are_compared() {
    let r = |s: &str| String::from_utf8(normalize(s.as_bytes())).unwrap();
    assert_eq!(r("Title"), "Title");
    assert_eq!(r("  Title  "), "Title");
    assert_eq!(r("Title (#7)"), "Title");
    assert_eq!(r("8306015: Update tests"), "Update tests");
    assert_eq!(r("8306015: Update tests (#5)"), "Update tests");
    assert_eq!(r("[clang][deps] Title (#242385)"), "Title");
    assert_eq!(r("[clang] [deps]  Title"), "Title");
    // what is no suffix, prefix or group stays
    assert_eq!(r("12345:Title"), "12345:Title");
    assert_eq!(r("Fix 12: thing"), "Fix 12: thing");
    assert_eq!(r("Fix [x] thing"), "Fix [x] thing");
    assert_eq!(r("[unclosed Title"), "[unclosed Title");
    // nothing is left of a subject that is only such a decoration
    assert_eq!(r("[wip]"), "");
    assert_eq!(r("123: (#4)"), "");
    assert_eq!(r(""), "");
}

/// main after `n` commits that are not related to the branch (author time `t0 + k`).
fn on_main(w: &mut W, subject: &str, body: &str, secs: i64, tag: &str) -> ObjectId {
    let s = Stamp::new("Other", "o@example.com", secs);
    let f = format!("m-{tag}.txt");
    let msg = if body.is_empty() {
        subject.to_string()
    } else {
        format!("{subject}\n\n{body}")
    };
    let id =
        w.fx.commit_at(Some(w.main), &[(f.as_str(), Some("m\n"))], msg, s, s);
    w.set_main(id);
    id
}

const WINDOW: i64 = 7 * 86_400;

fn subject_of_branch(
    w: &W,
    tip: ObjectId,
    pr: Option<&PrRef>,
    since: Option<i64>,
) -> SubjectEvidence {
    let repo = w.fx.repo();
    let bc = branch_commits(&repo, w.main, tip).unwrap();
    let idx = SquashIndex::build(&repo, w.main, since, 50_000).unwrap();
    evaluate_subject(&repo, &idx, &bc, pr, WINDOW).unwrap()
}

#[test]
fn the_merge_commit_of_the_pull_request_on_the_base_is_the_sure_candidate() {
    let mut w = world();
    let list = cs(0, 3);
    let ids = w.branch("feat", &list);
    let m = land_squash(
        &mut w,
        &list,
        "step 0 (#7)",
        "* step 0\n* step 1\n- step 2\n\nPull-request: https://github.com/test/fixture/pull/7",
    );
    let pr = PrRef {
        number: 7,
        merge_commit: Some(m),
    };
    let e = subject_of_branch(&w, ids[2], Some(&pr), None);
    assert_eq!(e.candidate, Some(m));
    assert_eq!(e.basis, Some(SubjectBasis::MergeCommit));
    assert_eq!(e.candidate_subject, "step 0 (#7)");
    assert_eq!(e.pr_number_in_subject, Some(7));
    assert!(e.subject_equal);
    assert_eq!(e.subjects_total, 3);
    // `* step 0` and `* step 1` are lines of the body, `- step 2` is not a `* ` line
    assert_eq!(e.subjects_in_body, 2);
    assert!(e.pull_request_trailer);
}

#[test]
fn the_pull_request_number_in_a_subject_finds_the_commit_without_a_merge_commit_on_the_base() {
    let mut w = world();
    let list = cs(0, 2);
    let ids = w.branch("feat", &list);
    on_main(&mut w, "unrelated", "", T0 + 20, "a");
    let hit = on_main(&mut w, "A new title (#7)", "", T0 + 30, "b");
    on_main(&mut w, "A new title (#8)", "", T0 + 40, "c");
    let pr = PrRef {
        number: 7,
        merge_commit: None,
    };
    let e = subject_of_branch(&w, ids[1], Some(&pr), None);
    assert_eq!(e.candidate, Some(hit));
    assert_eq!(e.basis, Some(SubjectBasis::PrNumber));
    assert_eq!(e.pr_number_in_subject, Some(7));
    // the title is not the title of a branch commit
    assert!(!e.subject_equal);
    assert_eq!(e.candidates, 1);
}

#[test]
fn without_a_pull_request_the_subject_of_a_branch_commit_finds_the_squash() {
    let mut w = world();
    let list = cs(0, 3);
    let ids = w.branch("feat", &list);
    on_main(&mut w, "unrelated", "", T0 + 20, "a");
    // the number is another one (a repository with other numbers): it does not matter without a pull request
    let hit = on_main(&mut w, "step 1 (#9)", "", T0 + 30, "b");
    let e = subject_of_branch(&w, ids[2], None, None);
    assert_eq!(e.candidate, Some(hit));
    assert_eq!(e.basis, Some(SubjectBasis::Subject));
    assert_eq!(e.pr_number_in_subject, Some(9));
    assert!(e.subject_equal);
    assert!(!e.pull_request_trailer);
    // the number of the pull request is also what a trailer must name
    let pr = PrRef {
        number: 7,
        merge_commit: None,
    };
    let e = subject_of_branch(&w, ids[2], Some(&pr), None);
    assert_eq!(e.basis, Some(SubjectBasis::Subject));
}

#[test]
fn a_prefix_group_or_number_in_a_subject_does_not_hide_the_title() {
    let mut w = world();
    let c1 = C {
        subject: "8306015: Update tests".into(),
        ..c(0)
    };
    let ids = w.branch("feat", &[c1]);
    let hit = on_main(&mut w, "[jdk] Update tests (#5)", "", T0 + 30, "a");
    let e = subject_of_branch(&w, ids[0], None, None);
    assert_eq!(e.candidate, Some(hit));
    assert!(e.subject_equal);
}

#[test]
fn a_subject_that_is_only_decoration_never_matches() {
    let mut w = world();
    let c1 = C {
        subject: "[wip]".into(),
        ..c(0)
    };
    let ids = w.branch("feat", &[c1]);
    on_main(&mut w, "[wip]", "", T0 + 30, "a");
    let e = subject_of_branch(&w, ids[0], None, None);
    assert_eq!((e.candidate, e.basis, e.subject_equal), (None, None, false));
    assert_eq!(e.candidates, 0);
}

#[test]
fn a_candidate_must_not_be_older_than_the_branch_by_more_than_the_window() {
    // the oldest commit of the branch has the author time T0; a commit on main 7 days older fits, 8 days older does not
    let build = |older: i64| {
        let mut w = world();
        let ids = w.branch("feat", &cs(0, 1));
        let hit = w.fx.commit_at(
            Some(w.main),
            &[("old.txt", Some("o\n"))],
            "step 0",
            Stamp::new("Other", "o@example.com", T0 - older),
            Stamp::new("Other", "o@example.com", T0 - older),
        );
        w.set_main(hit);
        // a newer commit on top keeps the old one inside the walk
        on_main(&mut w, "newer", "", T0 + 5, "n");
        let e = subject_of_branch(&w, ids[0], None, None);
        (e.candidate, hit)
    };
    let (c, hit) = build(WINDOW);
    assert_eq!(c, Some(hit));
    let (c, _) = build(WINDOW + 1);
    assert_eq!(c, None);
}

#[test]
fn a_merge_commit_on_the_base_is_never_a_candidate() {
    let mut w = world();
    let ids = w.branch("feat", &cs(0, 1));
    let s = Stamp::new("Other", "o@example.com", T0 + 30);
    let side =
        w.fx.commit_at(Some(w.main), &[("s.txt", Some("s\n"))], "side", s, s);
    let mg =
        w.fx.commit_at_parents(vec![w.main, side], Some(side), &[], "step 0 (#7)", s, s);
    w.set_main(mg);
    let pr = PrRef {
        number: 7,
        merge_commit: None,
    };
    let e = subject_of_branch(&w, ids[0], Some(&pr), None);
    assert_eq!(e.candidate, None);
    assert_eq!(e.candidates, 0);
}

#[test]
fn of_several_commits_with_a_matching_subject_the_oldest_counts() {
    let mut w = world();
    let ids = w.branch("feat", &cs(0, 3));
    let old = on_main(&mut w, "step 0", "", T0 + 30, "a");
    on_main(&mut w, "step 0", "", T0 + 31, "b");
    on_main(&mut w, "step 1", "", T0 + 32, "c");
    let e = subject_of_branch(&w, ids[2], None, None);
    assert_eq!(e.candidates, 3);
    assert_eq!(e.candidate, Some(old));
}

#[test]
fn the_trailer_must_name_the_pull_request() {
    let mut w = world();
    let list = cs(0, 2);
    let ids = w.branch("feat", &list);
    let m = land_squash(
        &mut w,
        &list,
        "step 0 (#7)",
        "Pull-request: https://github.com/test/fixture/pull/7",
    );
    let t = |n: u64| {
        let pr = PrRef {
            number: n,
            merge_commit: Some(m),
        };
        subject_of_branch(&w, ids[1], Some(&pr), None).pull_request_trailer
    };
    assert!(t(7));
    assert!(!t(8));
    assert!(!t(77));
    assert!(!subject_of_branch(&w, ids[1], None, None).pull_request_trailer);
}

#[test]
fn text_of_a_candidate_is_masked_and_the_window_of_the_walk_is_reported() {
    let mut w = world();
    let ids = w.branch("feat", &cs(0, 1));
    let hit = w.fx.commit_at(
        Some(w.main),
        &[("x.txt", Some("x\n"))],
        b"ti\x1b[31mtle \xff (#7)",
        Stamp::new("Other", "o@example.com", T0 + 30),
        Stamp::new("Other", "o@example.com", T0 + 30),
    );
    w.set_main(hit);
    let pr = PrRef {
        number: 7,
        merge_commit: None,
    };
    let e = subject_of_branch(&w, ids[0], Some(&pr), None);
    assert_eq!(e.candidate, Some(hit));
    assert!(
        !e.candidate_subject.contains('\u{1b}'),
        "{}",
        e.candidate_subject
    );
    assert!(
        e.candidate_subject.contains("\\xff"),
        "{}",
        e.candidate_subject
    );
    assert!(!e.window_cut);
    assert_eq!(e.window_commits, 2);
    // a walk cut at 1 commit is reported
    let repo = w.fx.repo();
    let bc = branch_commits(&repo, w.main, ids[0]).unwrap();
    let idx = SquashIndex::build(&repo, w.main, None, 1).unwrap();
    let e = evaluate_subject(&repo, &idx, &bc, Some(&pr), WINDOW).unwrap();
    assert!(e.window_cut);
    assert_eq!(e.window_commits, 1);
}
