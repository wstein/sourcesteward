//! `stw tidy plan --explain`: under each branch of the text plan the evidence that led to the decision, or the evidence
//! that is missing. Read-only; the decision and the JSON are the same without it.
use crate::tidy::{build, plan};
use stw::tidy::RepoPlan;

/// The explanation lines under the line of `branch`: the lines that follow it with four spaces of indent.
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

fn explained(p: &RepoPlan) -> String {
    stw::tidy::render_explained(std::slice::from_ref(p), true)
}

fn first(text: &str, branch: &str) -> String {
    under(text, branch).first().cloned().unwrap_or_default()
}

// ---- the sentence for each class ----

#[test]
fn each_class_of_the_plan_gets_one_sentence_with_the_proof_that_decided() {
    let b = build();
    let p = plan(&b, true);
    let t = explained(&p);
    let w = |br: &str| first(&t, br);
    assert_eq!(
        w("merged"),
        "why: delete: the tip is an ancestor of origin/main"
    );
    assert_eq!(
        w("rebased"),
        "why: delete: all 1 commit(s) of the branch have an equal patch on origin/main"
    );
    assert_eq!(
        w("pr-merged"),
        "why: delete: a merged pull request has exactly this tip as its head"
    );
    assert_eq!(
        w("pr-open"),
        "why: keep: an open pull request has this branch as its head"
    );
    assert_eq!(
        w("pr-moved"),
        "why: review: pull request #3 merged, the branch has 2 commit(s) not in origin/main"
    );
    let up = get_upstream(&p, "gone-up");
    assert_eq!(
        w("gone-up"),
        format!("why: review: upstream {up} is gone, 1 commit(s) not in origin/main")
    );
    assert_eq!(
        w("unmerged"),
        "why: keep: 1 of 1 commit(s) have no patch partner on origin/main"
    );
    assert_eq!(
        w("wt-live"),
        "why: keep: checked out in a worktree whose directory exists; without it the proof would be merged-ancestor"
    );
}

fn get_upstream(p: &RepoPlan, br: &str) -> String {
    p.branches
        .iter()
        .find(|b| b.branch == br)
        .and_then(|b| b.upstream.clone())
        .unwrap()
}

// ---- the proofs of the stages, backups, and what a stage replaced ----

use crate::tidy_rebase_plan::{cs, merged_pr, opts, plan_with, world, MERGE_SECS, T0};

fn landed_world() -> crate::tidy_rebase_plan::W {
    let mut w = world();
    let list = cs(0, 3);
    w.branch("feat", &list);
    w.land_resolved(&list, MERGE_SECS);
    w
}

#[test]
fn rebase_identity_and_similar_patches_say_what_was_found() {
    let w = landed_world();
    let t = explained(&plan_with(&w, None, &opts()));
    assert_eq!(
        first(&t, "feat"),
        "why: review: 3 of 3 commit(s) map to commits of origin/main as one run (strong)"
    );
    let mut o = opts();
    o.rebase = false;
    let t = explained(&plan_with(&w, None, &o));
    assert_eq!(
        first(&t, "feat"),
        "why: review: all 3 commit(s) without an equal patch have a similar patch on origin/main (lowest 97.5 %, threshold 95 %)"
    );
}

#[test]
fn a_backup_is_decided_by_the_age_alone() {
    let w = world();
    w.branch("backup/old", &cs(0, 1));
    let mut o = opts();
    o.now = T0 + 30 * 86_400;
    let t = explained(&plan_with(&w, None, &o));
    assert_eq!(
        first(&t, "backup/old"),
        "why: delete: backup name, no other proof applied; the age of the tip commit decides"
    );
    let t = explained(&plan_with(&w, None, &opts()));
    assert_eq!(
        first(&t, "backup/old"),
        "why: keep: backup name, no other proof applied; the age of the tip commit decides"
    );
}

#[test]
fn a_stage_that_replaced_the_pull_request_or_the_upstream_proof_names_them() {
    let w = landed_world();
    let pr = merged_pr("feat", 5, "1".repeat(40), "f".repeat(40), MERGE_SECS);
    let mut o = opts();
    o.rebase = false;
    let t = explained(&plan_with(&w, Some(vec![pr.clone()]), &o));
    assert_eq!(
        first(&t, "feat"),
        "why: review: all 3 commit(s) without an equal patch have a similar patch on origin/main (lowest 97.5 %, threshold 95 %); pull request #5 was merged and the branch moved on"
    );
    let t = explained(&plan_with(&w, Some(vec![pr]), &opts()));
    assert_eq!(
        first(&t, "feat"),
        "why: review: 3 of 3 commit(s) map to commits of origin/main as one run (strong); pull request #5 was merged and the branch moved on"
    );
    // a gone upstream
    w.fx.track("feat");
    let t = explained(&plan_with(&w, None, &o));
    assert_eq!(
        first(&t, "feat"),
        "why: review: all 3 commit(s) without an equal patch have a similar patch on origin/main (lowest 97.5 %, threshold 95 %); upstream origin/feat is gone"
    );
}

#[test]
fn without_the_flag_the_text_is_the_plain_plan() {
    let w = landed_world();
    let p = plan_with(&w, None, &opts());
    let plain = stw::tidy::render_human(std::slice::from_ref(&p), true);
    assert!(
        !plain.contains("why:")
            && plain.contains("[rebase-identity] rebase-identity (3 of 3 commits"),
        "{plain}"
    );
    // every line of the plain text is still there, in its order, in the explained one
    let e = explained(&p);
    let kept: Vec<&str> = e.lines().filter(|l| !l.starts_with("    ")).collect();
    assert_eq!(kept, plain.lines().collect::<Vec<_>>());
}

// ---- the similarity section ----

use crate::common::Stamp;
use crate::tidy_rebase_plan::{c, C, MAIL, NAME};
use stw::tidy::BranchPlan;

fn no_rebase() -> stw::tidy::PlanOpts {
    let mut o = opts();
    o.rebase = false;
    o
}

/// `n` lines `<tag> line <k>`; the lines in `odd` read `<tag> odd <k>`.
fn odd_lines(tag: &str, n: usize, odd: &[usize]) -> String {
    (0..n)
        .map(|k| {
            if odd.contains(&k) {
                format!("{tag} odd {k}\n")
            } else {
                format!("{tag} line {k}\n")
            }
        })
        .collect()
}

fn with_odd(c: &C, odd: &[usize]) -> C {
    C {
        body: odd_lines("c0", 40, odd),
        ..c.clone()
    }
}

fn sim<'a>(p: &'a RepoPlan, br: &str) -> &'a BranchPlan {
    p.branches.iter().find(|b| b.branch == br).unwrap()
}

fn sha7(id: impl ToString) -> String {
    id.to_string()[..7].to_string()
}

#[test]
fn a_landed_branch_has_the_summary_line_and_no_commit_lines() {
    let w = landed_world();
    let p = plan_with(&w, None, &no_rebase());
    let u = under(&explained(&p), "feat");
    assert_eq!(
        u[1..],
        [
            "similarity: class landed, 3 commit(s) judged, 3 at or above 95 %, lowest 97.5 %, median 97.5 %, 0 without candidate"
        ]
    );
}

#[test]
fn a_modified_commit_shows_its_candidate_value_and_at_most_five_evidence_lines() {
    let mut w = world();
    w.branch("feat", &[c(0)]);
    w.land(&[with_odd(&c(0), &[3, 5, 7, 9])], MERGE_SECS);
    let p = plan_with(&w, None, &no_rebase());
    let b = sim(&p, "feat");
    let r = &b.similarity.as_ref().unwrap().report.commits[0];
    let cand = match &r.outcome {
        stw::tidy_similarity::Outcome::Compared(x) => x.candidate,
        o => panic!("{o:?}"),
    };
    let u = under(&explained(&p), "feat");
    let me = sha7(r.commit);
    assert_eq!(
        u[1..],
        [
            "similarity: class modified, 1 commit(s) judged, 0 at or above 95 %, lowest 90.0 %, median 90.0 %, 0 without candidate".to_string(),
            format!("  {me} 'step 0' best candidate {} 90.0 % (subject; without path 90.0 %)", sha7(cand)),
            "    + f0.txt: c0 line 3".into(),
            "    + f0.txt: c0 line 5".into(),
            "    + f0.txt: c0 line 7".into(),
            "    + f0.txt: c0 line 9".into(),
            "    + f0.txt: c0 odd 3".into(),
        ]
    );
}

#[test]
fn a_commit_without_a_candidate_and_a_commit_too_large_are_named() {
    let w = world();
    w.branch("plain", &[c(0)]);
    let big = "x\n".repeat(600_000);
    let s = Stamp::new(NAME, MAIL, T0 + 500);
    let id =
        w.fx.commit_at(Some(w.c0), &[("big.txt", Some(big.as_str()))], "big", s, s);
    w.fx.branch("huge", id);
    let p = plan_with(&w, None, &no_rebase());
    let t = explained(&p);
    let plain = &sim(&p, "plain").similarity.as_ref().unwrap().report.commits[0];
    let u = under(&t, "plain");
    assert_eq!(
        u[1..],
        [
            "similarity: class partial, 1 commit(s) judged, 0 at or above 95 %, 1 without candidate".to_string(),
            format!("  {} 'step 0' no candidate", sha7(plain.commit)),
        ]
    );
    let huge = &sim(&p, "huge").similarity.as_ref().unwrap().report.commits[0];
    let u = under(&t, "huge");
    assert_eq!(
        u.get(2..).unwrap_or_default(),
        [format!("  {} 'big' skipped (too-large)", sha7(huge.commit))]
    );
}

#[test]
fn at_most_twenty_commit_lines_are_shown_and_the_rest_is_counted() {
    let w = world();
    w.branch("many", &cs(0, 25));
    let p = plan_with(&w, None, &no_rebase());
    let u = under(&explained(&p), "many");
    assert_eq!(u.len(), 2 + 20 + 1, "{u:#?}");
    assert!(
        u[2].starts_with("  ") && u[2].contains("no candidate"),
        "{u:#?}"
    );
    assert_eq!(u[22], "  … 5 more commit(s)");
}
