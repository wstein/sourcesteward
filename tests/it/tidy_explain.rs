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
        "why: delete: all commits of the branch that have a patch have an equal patch on origin/main (1 commit(s) not in it)"
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
        "why: keep: 1 of the 1 commit(s) not in origin/main have a patch without a partner there"
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

#[test]
fn text_from_commits_is_masked_and_an_evidence_line_has_at_most_120_characters() {
    let mut w = world();
    let mut hostile = c(0);
    hostile.file = format!("d\u{202e}{}.txt", "p".repeat(150));
    hostile.body = odd_lines("c0", 40, &[]).replacen(
        "c0 line 3\n",
        &format!("\u{1b}[31m\u{202e}{}\n", "y".repeat(300)),
        1,
    );
    w.branch("feat", std::slice::from_ref(&hostile));
    w.land(&[with_odd(&hostile, &[3, 5, 7, 9])], MERGE_SECS);
    // a commit whose subject is not UTF-8
    let s = Stamp::new(NAME, MAIL, T0 + 900);
    let id = w.fx.commit_at(
        Some(w.c0),
        &[("l.txt", Some("l\n"))],
        b"caf\xe9\x07 bell",
        s,
        s,
    );
    w.fx.branch("latin1", id);
    let p = plan_with(&w, None, &no_rebase());
    let t = explained(&p);
    let u = under(&t, "feat");
    let ev = u.iter().find(|l| l.starts_with("    + d")).unwrap_or(&u[0]);
    assert!(
        ev.starts_with(&format!("    + d\\u{{202e}}{}", "p".repeat(20))),
        "{ev}"
    );
    assert!(
        ev.chars().count() <= 124 && ev.ends_with('\u{2026}'),
        "{ev}"
    );
    let l = under(&t, "latin1");
    assert!(
        l.iter().any(|x| x.contains("'caf\\xe9\\u{7} bell'")),
        "{l:#?}"
    );
    for ch in t.chars() {
        assert!(
            ch == '\n' || !(ch.is_control() || ('\u{202a}'..='\u{202e}').contains(&ch)),
            "raw {ch:?} in {t}"
        );
    }
}

// ---- the rebase section: landed via, the mapping, what remains, the pull request ----

use crate::common::FakeRoutes;
use crate::tidy_rebase_plan::{client, commits_body, get, moved, no_sim, plan_api, report};
use stw::tidy::PrApi;
use stw::tidy_rebase::{ApiReason, MapEntry};

/// Line `i` of an explanation, empty where there is none (a failed assertion, not an index panic).
fn at(u: &[String], i: usize) -> &str {
    u.get(i).map(String::as_str).unwrap_or("")
}

fn map_line(m: &MapEntry, note: &str) -> String {
    let value = m
        .value
        .map(|v| format!(" {} %", stw::tidy_similarity::fmt_permille(v)))
        .unwrap_or_default();
    format!(
        "  {} -> {} {}{value}{note}",
        sha7(m.commit),
        sha7(m.main.unwrap()),
        m.method.as_str()
    )
}

#[test]
fn a_strong_mapping_lists_each_commit_with_its_commit_on_the_base() {
    let w = landed_world();
    let p = plan_with(&w, None, &opts());
    let r = report(&p, "feat");
    let u = under(&explained(&p), "feat");
    assert_eq!(at(&u, 1), "landed via: rebase (commit-map, strong)");
    assert_eq!(
        at(&u, 2),
        "mapping: strength strong, 3 of 3 commit(s) mapped (patch-id 0, identity 3, similarity 0), contiguous (gap 0), in order"
    );
    let want: Vec<String> = r.mapping.iter().map(|m| map_line(m, "")).collect();
    assert_eq!(want.len(), 3);
    assert_eq!(u.get(3..6).unwrap_or_default(), &want[..]);
    assert_eq!(
        at(&u, 6),
        format!(
            "pull request: PR mapping not read ({})",
            get(&p, "feat")
                .rebase
                .as_ref()
                .unwrap()
                .api
                .not_read
                .unwrap()
                .as_str()
        )
    );
    assert_eq!(u.len(), 7, "{u:#?}");
}

#[test]
fn a_part_that_landed_names_the_commits_that_remain_with_their_files() {
    let mut w = world();
    let list = cs(0, 3);
    w.branch("feat", &list);
    w.land_resolved(&list[..2], MERGE_SECS);
    let mut o = opts();
    o.similarity = None;
    let p = plan_with(&w, None, &o);
    let r = report(&p, "feat");
    let u = under(&explained(&p), "feat");
    assert_eq!(
        at(&u, 0),
        "why: keep: 3 commit(s) not in origin/main, no proof of landing found"
    );
    assert_eq!(
        at(&u, 1),
        "mapping: strength hint, 2 of 3 commit(s) mapped (patch-id 0, identity 2, similarity 0), contiguous (gap 0), in order"
    );
    assert_eq!(at(&u, 2), map_line(&r.mapping[0], ""));
    assert_eq!(at(&u, 3), map_line(&r.mapping[1], ""));
    assert_eq!(
        at(&u, 4),
        "remaining: 1 commit(s) without a commit on origin/main"
    );
    assert_eq!(
        at(&u, 5),
        format!(
            "  {} 'step 2' (1 file(s): f2.txt)",
            sha7(r.remaining[0].commit)
        )
    );
    assert!(
        at(&u, 6).starts_with("pull request: PR mapping not read ("),
        "{u:#?}"
    );
    assert_eq!(u.len(), 7);
}

#[test]
fn a_modified_mapping_and_a_later_version_are_hints_and_say_so() {
    let mut w = world();
    w.branch("mod", &[c(0)]);
    w.land(&[with_odd(&c(0), &[3, 5, 7, 9])], MERGE_SECS);
    // a branch commit committed after its commit on the base
    let mut d = c(1);
    d.secs = T0 + 200;
    let a = Stamp::new(NAME, MAIL, d.secs);
    let later = Stamp::new(NAME, MAIL, MERGE_SECS + 5_000);
    let id = w.fx.commit_at(
        Some(w.c0),
        &[(d.file.as_str(), Some(d.body.as_str()))],
        &d.subject,
        a,
        later,
    );
    w.fx.branch("late", id);
    w.land_resolved(&[d], MERGE_SECS + 100);
    let p = plan_with(&w, None, &opts());
    let t = explained(&p);
    let u = under(&t, "mod");
    let m = &report(&p, "mod").mapping[0];
    assert!(u.contains(&map_line(m, "")), "{u:#?}");
    assert!(
        u.contains(
            &"modified: a commit is at least 70 % and below 95 % like its commit on origin/main (hint only)"
                .to_string()
        ),
        "{u:#?}"
    );
    let u = under(&t, "late");
    let m = &report(&p, "late").mapping[0];
    assert!(m.later);
    assert!(
        u.contains(&map_line(m, " (later version, hint only)")),
        "{u:#?}"
    );
    assert!(
        !under(&t, "mod").iter().any(|l| l.contains("later version")),
        "{t}"
    );
}

#[test]
fn a_delete_gets_the_sentence_and_how_it_landed_and_nothing_else() {
    let mut w = world();
    let eq = cs(3, 2);
    w.branch("equal", &eq);
    w.land(&eq, MERGE_SECS);
    let p = plan_with(&w, None, &opts());
    assert_eq!(proof_of_eq(&p), ("delete", "equal-patches"));
    assert_eq!(
        under(&explained(&p), "equal"),
        [
            "why: delete: all commits of the branch that have a patch have an equal patch on origin/main (2 commit(s) not in it)",
            "landed via: rebase (commit-map, strong)"
        ]
    );
}

fn proof_of_eq(p: &RepoPlan) -> (&'static str, &'static str) {
    let b = get(p, "equal");
    (b.decision, b.proof)
}

#[test]
fn every_reason_of_the_pull_request_mapping_is_named_in_one_line() {
    let w = landed_world();
    let p = plan_with(&w, None, &opts());
    let all = [
        ApiReason::Offline,
        ApiReason::NoToken,
        ApiReason::NoPullRequest,
        ApiReason::RateLimited,
        ApiReason::NotMerged,
        ApiReason::MergeCommitNotInClone,
        ApiReason::NotOnBase,
        ApiReason::RequestFailed,
        ApiReason::TooManyCommits,
        ApiReason::NotNeeded,
        ApiReason::RequestLimit,
        ApiReason::EvaluateFailed,
    ];
    let names = [
        "offline",
        "no-token",
        "no-pull-request",
        "rate-limited",
        "not-merged",
        "merge-commit-not-in-clone",
        "not-on-base",
        "request-failed",
        "too-many-commits",
        "not-needed",
        "request-limit",
        "evaluate-failed",
    ];
    for (reason, name) in all.into_iter().zip(names) {
        let mut q = p.clone();
        for b in &mut q.branches {
            b.rebase.as_mut().unwrap().api.not_read = Some(reason);
        }
        let u = under(&explained(&q), "feat");
        assert_eq!(
            u.last().unwrap(),
            &format!("pull request: PR mapping not read ({name})")
        );
    }
}

#[test]
fn a_pull_request_that_was_read_says_what_it_found_and_why_it_is_a_hint() {
    let m = moved(|w, l| *w.land_resolved(l, MERGE_SECS).last().unwrap());
    let main = m.w.main.to_string();
    let body = commits_body(&m.ids[..2], &m.list[..2]);
    let routes = FakeRoutes::start(move |_, _| (200, vec![], body.clone()));
    let c = client(&routes);
    let prs = vec![merged_pr("feat", 5, m.ids[1].to_string(), main, MERGE_SECS)];
    let p = plan_api(&m.w, prs, PrApi::Client(&c), &no_sim());
    let a = &get(&p, "feat").rebase.as_ref().unwrap().api;
    let u = under(&explained(&p), "feat");
    assert_eq!(
        at(&u, 0),
        "why: review: pull request #5 merged, the branch has 3 commit(s) not in origin/main"
    );
    assert!(
        u.contains(&"landed via: rebase (pr-api, hint)".to_string()),
        "{u:#?}"
    );
    let k = u
        .iter()
        .position(|l| l.starts_with("pull request: #5"))
        .unwrap_or(1000);
    assert_eq!(
        at(&u, k),
        "pull request: #5 rebase (hint), 2 commit(s), the head is in the branch, 1 commit(s) of the branch come after it"
    );
    assert_eq!(
        at(&u, k + 1),
        "  hint only: commits of the branch come after the pull request head; the pull request mapping gives no proof of its own"
    );
    assert_eq!(at(&u, k + 2), "pull request mapping:");
    let want: Vec<String> = a.mapping.iter().map(|m| map_line(m, "")).collect();
    assert_eq!(want.len(), 2);
    assert_eq!(u.get(k + 3..k + 5).unwrap_or_default(), &want[..]);
    assert_eq!(
        at(&u, k + 5),
        "remaining after the pull request head: 1 commit(s)"
    );
    assert_eq!(
        at(&u, k + 6),
        format!(
            "  {} 'step 2' (1 file(s): f2.txt)",
            sha7(a.remaining[0].commit)
        )
    );
}

#[test]
fn the_mapping_is_cut_at_twenty_lines() {
    let mut w = world();
    let list = cs(0, 25);
    w.branch("big", &list);
    w.land_resolved(&list, MERGE_SECS);
    let mut o = opts();
    o.similarity = None;
    let u = under(&explained(&plan_with(&w, None, &o)), "big");
    let rows: Vec<&String> = u.iter().filter(|l| l.contains(" -> ")).collect();
    assert_eq!(rows.len(), 20, "{u:#?}");
    assert!(u.contains(&"  \u{2026} 5 more".to_string()), "{u:#?}");
}

#[test]
fn the_remaining_commits_are_cut_at_twenty_lines() {
    let mut o = opts();
    o.similarity = None;
    let w = world();
    w.branch("open", &cs(0, 25));
    let u = under(&explained(&plan_with(&w, None, &o)), "open");
    let at = u
        .iter()
        .position(|l| l.starts_with("remaining: 25"))
        .unwrap_or(1000);
    assert_eq!(
        u.get(at + 20).map(|s| s.contains("'step 19'")),
        Some(true),
        "{u:#?}"
    );
    assert_eq!(at_(&u, at + 21), "  \u{2026} 5 more commit(s)");
    assert!(at_(&u, at + 22).starts_with("pull request: "), "{u:#?}");
    assert_eq!(u.len(), at + 23);
}

#[test]
fn the_paths_of_all_remaining_commits_together_are_cut_at_twenty() {
    let mut o = opts();
    o.similarity = None;
    let w = world();
    let files = |tag: &str| -> Vec<(String, String)> {
        (0..15)
            .map(|i| (format!("{tag}/f{i:02}.txt"), "x\n".to_string()))
            .collect()
    };
    let mk = |parent, tag: &str, secs| {
        let f = files(tag);
        let e: Vec<(&str, Option<&str>)> = f
            .iter()
            .map(|(n, b)| (n.as_str(), Some(b.as_str())))
            .collect();
        let s = Stamp::new(NAME, MAIL, secs);
        w.fx.commit_at(Some(parent), &e, format!("files {tag}"), s, s)
    };
    let one = mk(w.c0, "a", T0 + 10);
    let two = mk(one, "b", T0 + 20);
    w.fx.branch("files", two);
    let u = under(&explained(&plan_with(&w, None, &o)), "files");
    let at = u
        .iter()
        .position(|l| l.starts_with("remaining: 2"))
        .unwrap_or(1000);
    let first = at_(&u, at + 1);
    let second = at_(&u, at + 2);
    let paths = |l: &str| {
        l.split(": ").nth(1).map(|x| {
            x.split(", ")
                .filter(|p| p.trim_end_matches(')').ends_with(".txt"))
                .count()
        })
    };
    assert_eq!(paths(first), Some(15), "{first}");
    assert_eq!(paths(second), Some(5), "{second}");
    assert!(
        second.contains("(15 file(s): ") && second.ends_with(", \u{2026})"),
        "{second}"
    );
}

fn at_(u: &[String], i: usize) -> &str {
    u.get(i).map(String::as_str).unwrap_or("")
}

// ---- the flag: `tidy plan --explain` ----

use crate::common::{path_str, scratch};
use crate::tidy_rebase_plan::stw_cli;

fn cli(w: &crate::tidy_rebase_plan::W, args: &[&str]) -> (Option<i32>, String, String) {
    let home = scratch();
    let r = path_str(&w.fx.path);
    let mut a: Vec<&str> = args.to_vec();
    a.extend_from_slice(&["--root", r.as_str()]);
    let o = stw_cli(home.path(), &a);
    (
        o.status.code(),
        String::from_utf8_lossy(&o.stdout).into_owned(),
        String::from_utf8_lossy(&o.stderr).into_owned(),
    )
}

#[test]
fn the_flag_adds_the_explanation_to_the_text_plan_and_nothing_else() {
    let w = landed_world();
    let (code, plain, _) = cli(&w, &["tidy", "plan", "--offline", "--all"]);
    assert_eq!(code, Some(0));
    assert!(!plain.contains("why:"), "{plain}");
    let (code, text, err) = cli(&w, &["tidy", "plan", "--offline", "--all", "--explain"]);
    assert_eq!(code, Some(0), "{err}");
    assert!(
        text.contains(
            "    why: review: 3 of 3 commit(s) map to commits of origin/main as one run (strong)\n"
        ),
        "{text}"
    );
    let kept: Vec<&str> = text.lines().filter(|l| !l.starts_with("    ")).collect();
    assert_eq!(kept, plain.lines().collect::<Vec<_>>());
    assert!(
        !text.contains("next command")
            && !text.contains("git branch")
            && !text.contains("stw tidy apply"),
        "{text}"
    );
}

#[test]
fn without_the_flag_the_text_is_the_literal_it_always_was() {
    let w = world();
    let merged = w.commits_on(Some(w.main), &cs(5, 1));
    w.fx.branch("merged", merged[0]);
    let w = {
        let mut w = w;
        w.set_main(merged[0]);
        w
    };
    let (code, plain, _) = cli(&w, &["tidy", "plan", "--offline"]);
    assert_eq!(code, Some(0));
    let name =
        w.fx.path
            .file_name()
            .unwrap()
            .to_string_lossy()
            .into_owned();
    let base = w.main.to_string();
    assert_eq!(
        plain,
        format!(
            "{name}  base refs/remotes/origin/main {}  (pull request proof off (--offline))\n  delete  merged  [merged-ancestor] tip is an ancestor of refs/remotes/origin/main\n1 to delete, 0 to delete with their worktree, 0 to review, 0 kept (read-only: nothing was changed)\n(GitHub not read: --offline)\n",
            &base[..12]
        )
    );
}

#[test]
fn the_flag_does_not_change_the_json() {
    let w = landed_world();
    let (_, plain, _) = cli(&w, &["tidy", "plan", "--offline", "--json"]);
    let (code, with, err) = cli(&w, &["tidy", "plan", "--offline", "--json", "--explain"]);
    assert_eq!(code, Some(0), "{err}");
    assert_eq!(plain, with);
}

#[test]
fn apply_has_no_such_flag() {
    let w = landed_world();
    let (code, out, err) = cli(&w, &["tidy", "apply", "--offline", "--explain"]);
    assert_eq!(code, Some(2), "{out}{err}");
    assert!(err.contains("--explain"), "{err}");
}

#[test]
fn the_sections_of_a_stage_that_is_off_are_left_out() {
    let w = landed_world();
    let (_, both, _) = cli(
        &w,
        &[
            "tidy",
            "plan",
            "--offline",
            "--explain",
            "--no-rebase-evidence",
        ],
    );
    assert!(both.contains("similarity: class landed"), "{both}");
    assert!(
        !both.contains("mapping:") && !both.contains("pull request:"),
        "{both}"
    );
    let (_, only, _) = cli(
        &w,
        &["tidy", "plan", "--offline", "--explain", "--no-similarity"],
    );
    assert!(only.contains("mapping: strength strong"), "{only}");
    assert!(!only.contains("similarity:"), "{only}");
    let (_, none, _) = cli(
        &w,
        &[
            "tidy",
            "plan",
            "--offline",
            "--all",
            "--explain",
            "--no-similarity",
            "--no-rebase-evidence",
        ],
    );
    assert_eq!(
        none.lines()
            .filter(|l| l.starts_with("    "))
            .collect::<Vec<_>>(),
        ["    why: keep: 3 commit(s) not in origin/main, no proof of landing found"]
    );
}

#[test]
fn the_explained_text_is_the_same_each_time_and_for_a_shallow_clone() {
    let w = landed_world();
    let a = cli(&w, &["tidy", "plan", "--offline", "--explain", "--all"]).1;
    let b = cli(&w, &["tidy", "plan", "--offline", "--explain", "--all"]).1;
    assert_eq!(a, b);
    let git = w.fx.path.join(".git");
    let main_hex = w.main.to_string();
    std::fs::write(git.join("shallow"), format!("{main_hex}\n")).unwrap();
    let (code, text, err) = cli(&w, &["tidy", "plan", "--offline", "--explain"]);
    assert_eq!(code, Some(0), "{err}");
    assert!(text.contains("    why: "), "{text}");
}

// ---- the numbers of the sentence, the order of the commit lines, the masked upstream ----

#[test]
fn the_sentence_counts_the_commits_without_a_patch_partner_of_all_that_are_not_on_the_base() {
    let mut w = world();
    let list = cs(0, 4);
    w.branch("feat", &list);
    w.land(&list[..2], MERGE_SECS);
    let p = plan_with(&w, None, &no_rebase());
    assert_eq!(
        first(&explained(&p), "feat"),
        "why: keep: 2 of the 4 commit(s) not in origin/main have a patch without a partner there"
    );
}

#[test]
fn the_commit_lines_follow_the_order_of_the_report() {
    let w = world();
    w.branch("many", &cs(0, 6));
    let p = plan_with(&w, None, &no_rebase());
    let want: Vec<String> = sim(&p, "many")
        .similarity
        .as_ref()
        .unwrap()
        .report
        .commits
        .iter()
        .map(|c| sha7(c.commit))
        .collect();
    let got: Vec<String> = under(&explained(&p), "many")
        .iter()
        .filter(|l| l.starts_with("  ") && l.contains("no candidate"))
        .map(|l| l.trim_start().chars().take(7).collect())
        .collect();
    assert_eq!(got, want);
    assert_eq!(got.len(), 6);
}

#[test]
fn the_name_of_a_gone_upstream_is_escaped() {
    let w = world();
    let name = "feat\u{202e}x";
    w.branch(name, &cs(0, 1));
    w.fx.track(name);
    let t = explained(&plan_with(&w, None, &no_rebase()));
    let line = t
        .lines()
        .find(|l| l.starts_with("    why: review: upstream"))
        .unwrap_or("");
    assert!(
        line.contains("origin/feat\\u{202e}x is gone"),
        "{line:?} in {t}"
    );
    assert!(!line.contains('\u{202e}'), "{line:?}");
}

#[test]
fn the_flag_lists_the_same_branches_as_the_plain_plan_with_and_without_all() {
    let w = landed_world();
    w.branch("plain", &cs(8, 1));
    let p = plan_with(&w, None, &opts());
    for all in [false, true] {
        let plain = stw::tidy::render_human(std::slice::from_ref(&p), all);
        let e = stw::tidy::render_explained(std::slice::from_ref(&p), all);
        let kept: Vec<&str> = e.lines().filter(|l| !l.starts_with("    ")).collect();
        assert_eq!(kept, plain.lines().collect::<Vec<_>>(), "all={all}");
        assert_eq!(e.contains("  plain  ["), all, "{e}");
    }
}

// ---- a pull request that was merged or squashed and the branch that moved on ----

fn merge_commit_of(w: &mut crate::tidy_rebase_plan::W, _: &[C]) -> gix::ObjectId {
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
}

fn squash_commit_of(w: &mut crate::tidy_rebase_plan::W, _: &[C]) -> gix::ObjectId {
    let s = Stamp::new("Merger", "merger@example.com", MERGE_SECS);
    w.fx.commit_at(
        Some(w.main),
        &[("squashed.txt", Some("all of it\n"))],
        "Add steps (#5)",
        s,
        s,
    )
}

fn moved_text(
    landed: fn(&mut crate::tidy_rebase_plan::W, &[C]) -> gix::ObjectId,
    at: i64,
) -> Vec<String> {
    let m = moved(landed);
    let main = m.w.main.to_string();
    let body = commits_body(&m.ids[..2], &m.list[..2]);
    let routes = FakeRoutes::start(move |_, _| (200, vec![], body.clone()));
    let c = client(&routes);
    let prs = vec![merged_pr("feat", 5, m.ids[1].to_string(), main, at)];
    let p = plan_api(&m.w, prs, PrApi::Client(&c), &no_sim());
    under(&explained(&p), "feat")
}

#[test]
fn a_merge_or_squash_that_the_branch_moved_on_from_names_that_and_no_wrong_number() {
    for (kind, u) in [
        ("merge", moved_text(merge_commit_of, MERGE_SECS)),
        ("squash", moved_text(squash_commit_of, MERGE_SECS + 1)),
    ] {
        let k = u
            .iter()
            .position(|l| l.starts_with("pull request: #5"))
            .unwrap_or(1000);
        assert_eq!(
            at(&u, k),
            format!("pull request: #5 {kind} (hint), 2 commit(s), the head is in the branch"),
            "{u:#?}"
        );
        assert_eq!(
            at(&u, k + 1),
            "  hint only: the branch has commits after the pull request head; the pull request mapping gives no proof of its own",
            "{kind}: {u:#?}"
        );
    }
}

// ---- numbers the data proves: empty commits and merge commits ----

#[test]
fn an_empty_commit_is_not_counted_as_a_commit_without_a_partner() {
    let w = world();
    w.branch("other", &cs(0, 1));
    let s = Stamp::new(NAME, MAIL, T0 + 700);
    let e = w.fx.commit_at(Some(w.c0), &[], "empty", s, s);
    w.fx.branch("emp", e);
    let p = plan_with(&w, None, &no_rebase());
    assert_eq!(
        first(&explained(&p), "emp"),
        "why: keep: 0 of the 1 commit(s) not in origin/main have a patch without a partner there"
    );
}

#[test]
fn a_merge_commit_in_a_branch_with_equal_patches_is_not_called_a_patch() {
    let mut w = world();
    let x = w.commits_on(None, &[c(0)])[0];
    let y = w.commits_on(None, &[c(1)])[0];
    let s = Stamp::new(NAME, MAIL, T0 + 800);
    let m = w.fx.commit_at_parents(
        vec![x, y],
        Some(y),
        &[("f0.txt", Some(c(0).body.as_str()))],
        "merge",
        s,
        s,
    );
    w.fx.branch("merged", m);
    w.land(&[c(0), c(1)], MERGE_SECS);
    let p = plan_with(&w, None, &no_rebase());
    assert_eq!(proof_of_br(&p, "merged"), ("delete", "equal-patches"));
    assert_eq!(
        first(&explained(&p), "merged"),
        "why: delete: all commits of the branch that have a patch have an equal patch on origin/main (3 commit(s) not in it)"
    );
}

fn proof_of_br(p: &RepoPlan, br: &str) -> (&'static str, &'static str) {
    let b = get(p, br);
    (b.decision, b.proof)
}
