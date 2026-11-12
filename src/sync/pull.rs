//! `stw sync --rebase` and `--merge`: a diverged branch (ahead and behind its upstream) is rebased onto the fetched
//! upstream or merged with it, by the installed `git` (gix has neither). Port of the Go `apply` for the actions
//! `rebase` and `merge` (`internal/stwsync/apply.go`); the fast-forward stays in `fast_forward.rs` (gix).
//!
//! Order: gates (clean tracked tree unless `--autostash`), then the journal entry (recovery ref and record
//! `prepared`, no planned tip), then `git`. A rebase or merge that stops on a conflict is left in progress as git
//! leaves it (`conflict`); the record is `failed` with the reason. After a success the branch must stand on a commit
//! that has the fetched upstream tip as ancestor, else the result is `raced`.
use super::journal::{self, Journal};
use crate::git_runner::{self, GitError, Hex40, Op, RemoteRef, RunId};
use crate::output::p2s;
use crate::repo::{self, RepoRef};
use crate::status::{self, HeadFacts};

/// What `--rebase` / `--merge` / neither do for a diverged branch.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum Diverged {
    /// Report the branch as diverged and change nothing.
    #[default]
    Skip,
    Rebase,
    Merge,
}

impl Diverged {
    pub fn verb(self) -> &'static str {
        match self {
            Diverged::Rebase => "rebase",
            Diverged::Merge => "merge",
            Diverged::Skip => "update",
        }
    }
}

/// The result of one rebase or merge.
pub struct Outcome {
    /// `rebased`, `merged`, `conflict`, `autostash-conflict`, `skip` or `error`.
    pub result: &'static str,
    pub reason: String,
    pub err: Option<String>,
    /// Git's own message of a stopped rebase or merge or a kept autostash (at most 1500 bytes).
    pub detail: Option<String>,
    pub journal: Option<journal::Note>,
}

fn out(result: &'static str, reason: impl Into<String>) -> Outcome {
    Outcome {
        result,
        reason: reason.into(),
        err: None,
        detail: None,
        journal: None,
    }
}

/// Git's refusal in one line: its first two message lines, without hints.
fn first_lines(s: &str) -> String {
    let kept: Vec<&str> = s
        .lines()
        .map(str::trim)
        .filter(|l| !l.is_empty() && !l.starts_with("hint:") && !l.starts_with("Aborting"))
        .take(2)
        .collect();
    if kept.is_empty() {
        "no message".into()
    } else {
        kept.join(" ")
    }
}

fn detail_of(s: &str) -> String {
    let t = s.trim();
    if t.len() <= 1500 {
        return t.to_string();
    }
    let mut end = 1500;
    while !t.is_char_boundary(end) {
        end -= 1;
    }
    format!("{}...", &t[..end])
}

/// A refusal of git that left the branch where it was because local changes are in the way.
fn is_dirty_refusal(text: &str) -> bool {
    let low = text.to_ascii_lowercase();
    [
        "would be overwritten",
        "local changes",
        "unstaged changes",
        "uncommitted changes",
    ]
    .iter()
    .any(|k| low.contains(k))
}

/// Rebase or merge the checked-out branch of `r` (`facts` is the state after the fetch). `autostash` is the explicit
/// choice of the flags, `false` without one: the stash flag of the argument vector is always explicit.
pub fn apply(
    r: &RepoRef,
    facts: &HeadFacts,
    mode: Diverged,
    autostash: bool,
    run: &RunId,
) -> Outcome {
    let repo = match repo::open(&r.path) {
        Ok(x) => x,
        Err(e) => return fail("cannot read repository", e.to_string()),
    };
    let (Some(branch), Some(head), Some(tip), Some(up)) = (
        facts.branch.clone(),
        facts.head,
        facts.upstream.tip,
        facts.upstream.short.clone(),
    ) else {
        return fail("invalid-plan", "no branch, commit or upstream".into());
    };
    let (Some(old), Some(target)) = (
        Hex40::parse(&head.to_string()),
        Hex40::parse(&tip.to_string()),
    ) else {
        return fail("invalid-plan", "not a 40 digit object id".into());
    };
    let Some(upstream) = RemoteRef::parse(&format!("refs/remotes/{up}")) else {
        return fail("invalid-plan", format!("upstream {up} is not under origin"));
    };
    let reference = format!("refs/heads/{branch}");
    let mut j = Journal::new(&repo, run.clone());
    let i = match j.prepare_unplanned(&repo, &reference, &p2s(&r.path), old) {
        Ok(i) => i,
        Err(e) => return fail("journal-failed", e),
    };
    let finish = |j: &mut Journal, state: &str, new: &str, reason: &str| {
        let _ = j.finish(i, state, new, reason);
    };
    // the branch must still be the one that was planned
    let again = status::head_facts(&repo);
    if !matches!(&again, Ok(f) if f.branch.as_deref() == Some(branch.as_str()) && f.head == Some(head))
    {
        finish(&mut j, journal::STATE_FAILED, "", "head-moved");
        let mut o = fail("head-moved", String::new());
        o.journal = Some(j.note(i));
        return o;
    }
    let op = match mode {
        Diverged::Merge => Op::SyncMerge { target, autostash },
        _ => Op::SyncRebase {
            upstream: upstream.clone(),
            autostash,
        },
    };
    let ran = git_runner::shared().run(&r.path, op);
    let after = repo::open(&r.path);
    let mut o = match ran {
        Ok(text) => {
            let text = format!("{}\n{}", String::from_utf8_lossy(&text.stdout), text.stderr);
            finished(&mut j, i, after, &branch, head, tip, mode, &text)
        }
        Err(GitError::Exit { stderr, .. }) => {
            let stopped = after.as_ref().is_ok_and(|a| status::operation(a).is_some());
            if stopped {
                finish(&mut j, journal::STATE_FAILED, "", "stopped on a conflict");
                let reason = match mode {
                    Diverged::Merge => "merge stopped on a conflict; resolve it, then run git commit (or git merge --abort)",
                    _ => "rebase stopped on a conflict; resolve it, then run git rebase --continue (or git rebase --abort)",
                };
                let mut c = out("conflict", reason);
                c.detail = Some(detail_of(&stderr));
                c
            } else if after
                .as_ref()
                .ok()
                .and_then(|a| status::head_facts(a).ok())
                .is_some_and(|f| f.head == Some(head))
                && is_dirty_refusal(&stderr)
            {
                let why = first_lines(&stderr);
                finish(&mut j, journal::STATE_FAILED, "", &why);
                out("skip", format!("git refused: {why}"))
            } else {
                let why = format!("git-failed: {}", first_lines(&stderr));
                finish(&mut j, journal::STATE_FAILED, "", &why);
                let mut f = out("error", "rebase or merge failed");
                f.err = Some(why);
                f
            }
        }
        Err(e) => {
            let why = format!("git-failed: {e}");
            finish(&mut j, journal::STATE_FAILED, "", &why);
            let mut f = out("error", "rebase or merge failed");
            f.err = Some(why);
            f
        }
    };
    o.journal = Some(j.note(i));
    if o.result == "rebased" || o.result == "merged" || o.result == "autostash-conflict" {
        j.retain(&repo);
    }
    o
}

fn fail(what: &str, detail: String) -> Outcome {
    let mut o = out("error", "rebase or merge failed");
    o.err = Some(if detail.is_empty() {
        what.to_string()
    } else {
        format!("{what}: {detail}")
    });
    o
}

/// After a successful git run: where the branch stands, whether the upstream tip is below it, and the record.
#[allow(clippy::too_many_arguments)]
fn finished(
    j: &mut Journal,
    i: usize,
    after: crate::error::Result<gix::Repository>,
    branch: &str,
    old: gix::ObjectId,
    tip: gix::ObjectId,
    mode: Diverged,
    text: &str,
) -> Outcome {
    let raced = |j: &mut Journal| {
        let _ = j.finish(i, journal::STATE_FAILED, "", "raced");
        let mut f = out("error", "rebase or merge failed");
        f.err = Some("raced".into());
        f
    };
    let Ok(repo) = after else {
        return raced(j);
    };
    let on_branch_head = status::head_facts(&repo)
        .ok()
        .is_some_and(|f| f.branch.as_deref() == Some(branch));
    let ref_tip = repo
        .try_find_reference(format!("refs/heads/{branch}").as_str())
        .ok()
        .flatten()
        .and_then(|x| x.target().try_id().map(gix::oid::to_owned));
    let head_tip = repo.head_id().ok().map(gix::Id::detach);
    let (true, Some(new), Some(at)) = (on_branch_head, head_tip, ref_tip) else {
        return raced(j);
    };
    if at != new {
        return raced(j);
    }
    if new == old || !repo::is_ancestor(&repo, tip, new).unwrap_or(false) {
        return raced(j);
    }
    let conflicted = text.contains("resulted in conflicts");
    let note_err = j
        .finish(i, journal::STATE_COMPLETED, &new.to_string(), "")
        .err();
    let (a, b) = (old.to_string(), new.to_string());
    let range = format!("{}..{}", &a[..7], &b[..7]);
    let mut o = out(
        if mode == Diverged::Merge {
            "merged"
        } else {
            "rebased"
        },
        range,
    );
    if let Some(e) = note_err {
        o.reason += &format!("; the journal record stays incomplete: {e}");
    }
    if conflicted {
        o.result = "autostash-conflict";
        o.reason =
            "your changes are kept in the stash; resolve with git stash pop or git stash drop"
                .into();
        o.detail = Some(detail_of(text));
    }
    o
}
