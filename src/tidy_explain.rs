//! The explanation of `stw tidy plan --explain`: under each branch the evidence that led to the decision, or the evidence
//! that is missing. Text only, derived from the data of the plan (classes, numbers, reasons); it reads nothing else,
//! interprets nothing and prints no command and no advice.
//!
//! The texts of commits that the reports carry (subjects, paths, lines) are masked where they are read
//! (`tidy_similarity::mask_bytes`); only what the plan holds unmasked (the upstream name) is escaped here.
use crate::github::reads::escape_untrusted;
use crate::tidy::{BranchPlan, RebasePlan, SimilarityPlan};
use crate::tidy_rebase::{
    LandedKind, MapEntry, RebaseReport, Remaining, Strength, RB_CONTENT_HIGH,
};
use crate::tidy_similarity::{fmt_permille, Outcome, LOW_PCT};

/// At most this many commit lines, mapping lines and remaining commits per branch; the rest is counted.
const MAX_ROWS: usize = 20;
/// At most this many characters in a line that carries text of a commit.
const MAX_LINE: usize = 120;

/// `s` cut to `max` characters, the last of them `…` where something was cut.
fn cut(s: &str, max: usize) -> String {
    if s.chars().count() <= max {
        return s.to_string();
    }
    let mut c: String = s.chars().take(max - 1).collect();
    c.push('\u{2026}');
    c
}

fn sha7(id: impl ToString) -> String {
    id.to_string().chars().take(7).collect()
}

/// The lines under one branch (no indent). `base` is the base ref as the plan names it.
pub fn lines(b: &BranchPlan, base: &str) -> Vec<String> {
    let base = base.strip_prefix("refs/remotes/").unwrap_or(base);
    let mut out = vec![format!("why: {}", why(b, base))];
    let l = &b.landed_via;
    if l.kind != LandedKind::Unknown {
        out.push(format!(
            "landed via: {} ({}, {})",
            l.kind.as_str(),
            l.basis.as_str(),
            l.strength.as_str()
        ));
    }
    // a deletion with its proof gets the sentence and how it landed, nothing more
    if b.decision == "delete" {
        return out;
    }
    if let Some(sim) = &b.similarity {
        out.extend(similarity(sim));
    }
    if let Some(r) = &b.rebase {
        out.extend(mapping(&r.report, base));
        out.extend(pull_request(r, base));
    }
    out
}

/// `(b7 -> m7 method value %)` for a mapped commit, with the hints that go with it.
fn map_line(m: &MapEntry) -> String {
    let Some(main) = m.main else {
        return format!(
            "  {} -> ambiguous (more than one commit on the base has the same author, time and subject)",
            sha7(m.commit)
        );
    };
    let mut s = format!(
        "  {} -> {} {}",
        sha7(m.commit),
        sha7(main),
        m.method.as_str()
    );
    if let Some(v) = m.value {
        s += &format!(" {} %", fmt_permille(v));
    }
    let mut notes: Vec<&str> = Vec::new();
    if m.later {
        notes.push("later version, hint only");
    }
    if m.tz_differs {
        notes.push("time zone differs");
    }
    if !notes.is_empty() {
        s += &format!(" ({})", notes.join(", "));
    }
    s
}

/// The mapped commits (at most `MAX_ROWS` lines), the `modified` hint, and the commits that remain.
fn mapping(rep: &RebaseReport, base: &str) -> Vec<String> {
    if rep.n == 0 {
        return Vec::new();
    }
    let mut out = vec![format!(
        "mapping: strength {}, {} of {} commit(s) mapped (patch-id {}, identity {}, similarity {}), {}, {}",
        rep.strength.as_str(),
        rep.mapped,
        rep.n,
        rep.by_patch_id,
        rep.by_identity,
        rep.by_similarity,
        if rep.contiguous { format!("contiguous (gap {})", rep.gap) } else { format!("not contiguous (gap {})", rep.gap) },
        if rep.in_order { "in order" } else { "not in order" }
    )];
    out.extend(entries(
        rep.mapping
            .iter()
            .filter(|m| m.main.is_some() || m.ambiguous),
        map_line,
    ));
    if rep.modified {
        out.push(format!(
            "modified: a commit is {LOW_PCT} to {} % like its commit on {base} (hint only)",
            RB_CONTENT_HIGH - 1
        ));
    }
    out.extend(remaining(
        &rep.remaining,
        &format!(
            "remaining: {} commit(s) without a commit on {base}",
            rep.remaining.len()
        ),
    ));
    out
}

/// At most `MAX_ROWS` lines, then the count of the rest.
fn entries<'a, T: 'a>(it: impl Iterator<Item = &'a T>, line: impl Fn(&T) -> String) -> Vec<String> {
    let mut out = Vec::new();
    let mut more = 0;
    for (i, x) in it.enumerate() {
        if i < MAX_ROWS {
            out.push(line(x));
        } else {
            more += 1;
        }
    }
    if more > 0 {
        out.push(format!("  \u{2026} {more} more"));
    }
    out
}

/// The commits without a target with their files: at most `MAX_ROWS` commits and, over all of them, `MAX_ROWS` paths.
fn remaining(list: &[Remaining], head: &str) -> Vec<String> {
    if list.is_empty() {
        return Vec::new();
    }
    let mut budget = MAX_ROWS;
    let mut out = vec![head.to_string()];
    let mut more = 0;
    for (i, r) in list.iter().enumerate() {
        if i >= MAX_ROWS {
            more += 1;
            continue;
        }
        let shown: Vec<&str> = r.files.iter().take(budget).map(String::as_str).collect();
        budget -= shown.len();
        let mut files = format!("{} file(s)", r.files_total);
        if !shown.is_empty() {
            files += &format!(": {}", shown.join(", "));
        }
        if shown.len() < r.files_total {
            files += ", \u{2026}";
        }
        out.push(format!("  {} '{}' ({files})", sha7(r.commit), r.subject));
    }
    if more > 0 {
        out.push(format!("  \u{2026} {more} more commit(s)"));
    }
    out
}

/// What the pull request gave, or why it was not read.
fn pull_request(r: &RebasePlan, _base: &str) -> Vec<String> {
    let a = &r.api;
    if let Some(reason) = a.not_read {
        return vec![format!(
            "pull request: PR mapping not read ({})",
            reason.as_str()
        )];
    }
    let head = match a.head_in_branch {
        Some(true) => "in",
        _ => "not in",
    };
    let mut out = vec![format!(
        "pull request: #{} {} ({}), {} commit(s), the head is {head} the branch, {} commit(s) of the branch come after it",
        a.pr.unwrap_or(0),
        a.kind.as_str(),
        a.strength.as_str(),
        a.k.unwrap_or(0),
        a.remaining.len()
    )];
    if a.kind != LandedKind::Unknown {
        let mut why: Vec<&str> = Vec::new();
        if a.strength != Strength::Strong {
            if !a.remaining.is_empty() {
                why.push("commits of the branch come after the pull request head");
            }
            if a.head_in_branch == Some(false) {
                why.push("the pull request head is not in the branch");
            }
        }
        why.push("the pull request mapping gives no proof of its own");
        let lead = if a.strength == Strength::Strong {
            "strong, but "
        } else {
            "hint only: "
        };
        out.push(format!("  {lead}{}", why.join("; ")));
    }
    if !a.mapping.is_empty() {
        out.push("pull request mapping:".to_string());
        out.extend(entries(a.mapping.iter(), map_line));
    }
    out.extend(remaining(
        &a.remaining,
        &format!(
            "remaining after the pull request head: {} commit(s)",
            a.remaining.len()
        ),
    ));
    out
}

/// The summary of the similarity stage and one line per commit below the high threshold, with its evidence lines.
fn similarity(sim: &SimilarityPlan) -> Vec<String> {
    let r = &sim.report;
    let mut head = format!(
        "similarity: class {}, {} commit(s) judged, {} at or above {} %",
        r.class.as_str(),
        r.plus_commits,
        r.at_or_above,
        sim.high
    );
    if let Some(m) = r.min {
        head += &format!(", lowest {} %", fmt_permille(m.shown_in(sim.high, sim.low)));
    }
    if let Some(d) = r.median_permille {
        head += &format!(", median {} %", fmt_permille(d));
    }
    head += &format!(", {} without candidate", r.without_candidate);
    let mut out = vec![head];
    let mut rows = 0;
    let mut more = 0;
    for c in &r.commits {
        let what = match &c.outcome {
            Outcome::Compared(x) if x.sim.ge_pct(sim.high) => continue,
            Outcome::Compared(x) => format!(
                "best candidate {} {} % ({}; without path {} %)",
                sha7(x.candidate),
                fmt_permille(x.sim.shown_in(sim.high, sim.low)),
                x.reason.as_str(),
                fmt_permille(x.no_path.permille())
            ),
            Outcome::NoCandidate => "no candidate".to_string(),
            Outcome::Skipped(k) => format!("skipped ({})", k.as_str()),
        };
        if rows == MAX_ROWS {
            more += 1;
            continue;
        }
        rows += 1;
        out.push(format!("  {} '{}' {what}", sha7(c.commit), c.subject));
        if let Outcome::Compared(x) = &c.outcome {
            for e in &x.evidence {
                out.push(cut(
                    &format!("    {} {}: {}", e.sign, e.path, e.line),
                    MAX_LINE + 4,
                ));
            }
        }
    }
    if more > 0 {
        out.push(format!("  \u{2026} {more} more commit(s)"));
    }
    out
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
