//! The explanation of `stw tidy plan --explain`: under each branch the evidence that led to the decision, or the evidence
//! that is missing. Text only, derived from the data of the plan (classes, numbers, reasons); it reads nothing else,
//! interprets nothing and prints no command and no advice.
//!
//! The texts of commits that the reports carry (subjects, paths, lines) are masked where they are read
//! (`tidy_similarity::mask_bytes`); only what the plan holds unmasked (the upstream name) is escaped here.
use crate::github::reads::escape_untrusted;
use crate::tidy::BranchPlan;

/// The lines under one branch (no indent). `base` is the base ref as the plan names it.
pub fn lines(b: &BranchPlan, base: &str) -> Vec<String> {
    let base = base.strip_prefix("refs/remotes/").unwrap_or(base);
    vec![format!("why: {}", why(b, base))]
}

/// The sentence: the decision and the proof that led to it, or the proof that is missing.
fn why(b: &BranchPlan, base: &str) -> String {
    let mut s = match b.proof {
        "merged-ancestor" => format!("{}: the tip is an ancestor of {base}", b.decision),
        "equal-patches" => format!(
            "{}: all {} commit(s) of the branch have an equal patch on {base}",
            b.decision, b.unique
        ),
        "merged-pull-request-head" => format!(
            "{}: a merged pull request has exactly this tip as its head",
            b.decision
        ),
        "open-pull-request" => format!(
            "{}: an open pull request has this branch as its head",
            b.decision
        ),
        "backup-older-than-days" | "backup-recent" => format!(
            "{}: backup name, no other proof applied; the age of the tip commit decides",
            b.decision
        ),
        "in-worktree" => {
            let mut s = format!(
                "{}: checked out in a worktree whose directory exists",
                b.decision
            );
            if let Some(w) = &b.worktree {
                s += &format!("; without it the proof would be {}", w.proof);
            }
            s
        }
        "rebase-identity" => match b.rebase.as_ref().map(|r| &r.report) {
            Some(r) => format!(
                "{}: {} of {} commit(s) map to commits of {base} as one run ({})",
                b.decision,
                r.mapped,
                r.n,
                r.strength.as_str()
            ),
            None => format!("{}: rebase identity", b.decision),
        },
        "similar-patches" => match &b.similarity {
            Some(sim) => format!(
                "{}: all {} commit(s) without an equal patch have a similar patch on {base} (lowest {} %, threshold {} %)",
                b.decision,
                sim.report.plus_commits,
                sim.report
                    .min
                    .map(|m| crate::tidy_similarity::fmt_permille(m.shown_in(sim.high, sim.low)))
                    .unwrap_or_default(),
                sim.high
            ),
            None => format!("{}: similar patches", b.decision),
        },
        "pull-request-merged-branch-moved" => format!(
            "{}: pull request #{} merged, the branch has {} commit(s) not in {base}",
            b.decision,
            b.pr_moved.unwrap_or(0),
            b.unique
        ),
        "upstream-gone" => format!(
            "{}: upstream {} is gone, {} commit(s) not in {base}",
            b.decision,
            escape_untrusted(b.upstream.as_deref().unwrap_or(""), 200).0,
            b.unique
        ),
        _ => match &b.similarity {
            Some(sim) => format!(
                "{}: {} of {} commit(s) have no patch partner on {base}",
                b.decision, sim.report.plus_commits, b.unique
            ),
            None => format!(
                "{}: {} commit(s) not in {base}, no proof of landing found",
                b.decision, b.unique
            ),
        },
    };
    // a later stage took the place of the proof that named the pull request or the upstream
    if matches!(b.proof, "similar-patches" | "rebase-identity") {
        if let Some(n) = b.pr_moved {
            s += &format!("; pull request #{n} was merged and the branch moved on");
        } else if b.track.as_deref() == Some("[gone]") {
            s += &format!(
                "; upstream {} is gone",
                escape_untrusted(b.upstream.as_deref().unwrap_or(""), 200).0
            );
        }
    }
    s
}
