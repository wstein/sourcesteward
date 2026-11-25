//! `stw sync restore [PATH] [--run ID]`: move the branches a `sync` run changed back to their journaled old tips.
//!
//! Ported from the Go tool (`internal/stwsync/restore.go`). The run is `--run ID`, else the highest run ID found in
//! the journals of the workspace (the order of the IDs, never a modification time). For every entry of its record in
//! every repository of the workspace:
//!
//! - `restored`: the branch stood on the tip the run left (`new_oid`, or the planned one of a record that was never
//!   completed) and is now back on the old tip. A branch that is checked out in the main worktree moves with
//!   `git reset --keep` (index and files with it), and only when that worktree has no tracked change; a branch checked
//!   out nowhere moves with `git update-ref` and the expected value (compare and swap). Never forced.
//! - `already-restored`: the branch is on the old tip already (also a run that failed before it moved anything).
//! - `changed-since`: anything else that is not the tip the run left (a new commit, the branch is gone, symbolic, checked
//!   out in another worktree, a local change in the worktree, an operation in progress): nothing is touched.
//! - `journal-incomplete`: a record that was never completed and names no planned tip.
//! - `failed`: a record that cannot be used (`bad-record`: a ref that is not `refs/heads/<name>`, an OID that is not 40
//!   lower-case hex digits; unreadable or foreign file), an old tip that is no commit any more, or a git step that failed.
//!
//! The record is data. Nothing in it names a place to work in: the repository is the one whose git directory holds the
//! record, taken from the workspace (`repo::list_repos`, which refuses paths outside it), and the `worktree` field is
//! never read. Pruned remote-tracking refs, stash reapplies and submodule moves are not undone (they are not journaled).
//! Every git step runs through the runner (`via: git`).
use serde_json::{json, Value};

use crate::git_runner::{self, BranchRef, GitError, Hex40, Op, RunId};
use crate::github::reads::escape_untrusted;
use crate::output::*;
use crate::repo::{self, worktrees, RepoRef};
use crate::status;

use super::journal::{self, Entry, Journal, Record};

pub const RESTORED: &str = "restored";
pub const ALREADY_RESTORED: &str = "already-restored";
pub const CHANGED_SINCE: &str = "changed-since";
pub const JOURNAL_INCOMPLETE: &str = "journal-incomplete";
pub const FAILED: &str = "failed";

/// One entry of the run in one repository.
#[derive(Clone, Debug)]
pub struct RestoreRec {
    pub name: String,
    pub path: String,
    pub branch: Option<String>,
    pub result: &'static str,
    /// Where the branch stood before the restore.
    pub old_oid: Option<String>,
    /// Where it stands after a restore (the journaled old tip).
    pub new_oid: Option<String>,
    pub reason: String,
    /// A git step ran (`reset --keep` or `update-ref`).
    pub via: Option<&'static str>,
}

#[derive(Clone, Debug, Default)]
pub struct Restored {
    pub run_id: Option<String>,
    pub repos: Vec<RestoreRec>,
    /// A refusal of the whole command: (code, message).
    pub error: Option<(&'static str, String)>,
}

impl Restored {
    /// 1 when the command was refused or an entry ended failed, changed-since or journal-incomplete.
    pub fn failed(&self) -> bool {
        self.error.is_some()
            || self
                .repos
                .iter()
                .any(|r| matches!(r.result, FAILED | CHANGED_SINCE | JOURNAL_INCOMPLETE))
    }
}

struct Source<'a> {
    r: &'a RepoRef,
    dir: std::path::PathBuf,
    ids: Result<Vec<RunId>, String>,
}

fn rec_for(r: &RepoRef, result: &'static str, reason: String) -> RestoreRec {
    RestoreRec {
        name: r.name.clone(),
        path: p2s(&r.path),
        branch: None,
        result,
        old_oid: None,
        new_oid: None,
        reason,
        via: None,
    }
}

/// Restore run `run` (default: the highest) in `repos`, the repositories of the workspace.
pub fn restore(repos: &[RepoRef], run: Option<&str>) -> Restored {
    let mut out = Restored::default();
    let wanted = match run {
        None => None,
        Some(t) => match RunId::parse(t) {
            Some(id) => Some(id),
            None => {
                out.error = Some((
                    "sync-invalid-options",
                    "--run needs a run ID as printed by stw sync".into(),
                ));
                return out;
            }
        },
    };
    let mut sources: Vec<Source> = Vec::new();
    for r in repos {
        // a repository that cannot be opened has no journal to read here
        if let Ok(repo) = repo::open(&r.path) {
            let dir = journal::dir_of(&repo);
            let ids = journal::list_ids(&dir);
            sources.push(Source { r, dir, ids });
        }
    }
    // a journal directory that is unusable is reported, never skipped silently
    for s in &sources {
        if let Err(e) = &s.ids {
            out.repos.push(rec_for(
                s.r,
                FAILED,
                format!("the journal directory cannot be used: {e}"),
            ));
        }
    }
    let id = match wanted {
        Some(id) => id,
        None => {
            let latest = sources
                .iter()
                .filter_map(|s| s.ids.as_ref().ok())
                .flatten()
                .max_by_key(|i| i.order_key());
            match latest {
                Some(i) => i.clone(),
                None => {
                    out.error = Some((
                        "sync-restore-no-run",
                        "no stw sync run is journaled in this workspace".into(),
                    ));
                    return out;
                }
            }
        }
    };
    // None: the repository has no record of the run
    let found: Vec<Option<Vec<RestoreRec>>> = super::parent_first(
        &sources,
        |s| &s.r.path,
        |s| {
            if !s.ids.as_ref().is_ok_and(|ids| ids.contains(&id)) {
                return None;
            }
            match journal::load(&s.dir, &id) {
                Ok(None) => None,
                Err(e) => Some(vec![rec_for(
                    s.r,
                    FAILED,
                    format!("the journal record cannot be used: {e}"),
                )]),
                Ok(Some(rec)) => Some(restore_record(s, &id, rec)),
            }
        },
    );
    let any = found.iter().any(Option::is_some);
    out.repos.extend(found.into_iter().flatten().flatten());
    if !any && out.repos.is_empty() {
        out.error = Some((
            "sync-restore-no-run",
            format!("run {id} is not journaled in this workspace"),
        ));
        return out;
    }
    out.run_id = Some(id.to_string());
    out
}

fn restore_record(s: &Source, id: &RunId, rec: Record) -> Vec<RestoreRec> {
    // gix::Repository is not Sync: each worker opens its own
    let repo = match repo::open(&s.r.path) {
        Ok(r) => r,
        Err(e) => {
            let e = e.to_string();
            return vec![rec_for(
                s.r,
                FAILED,
                format!("cannot open the repository: {e}"),
            )];
        }
    };
    let n = rec.entries.len();
    let mut j = Journal::from_record(&repo, id.clone(), rec);
    (0..n)
        .map(|k| {
            let en = j.record().entries[k].clone();
            restore_entry(s, &repo, id, &mut j, k, &en)
        })
        .collect()
}

fn changed(mut rp: RestoreRec, why: impl Into<String>) -> RestoreRec {
    rp.result = CHANGED_SINCE;
    rp.reason = why.into();
    rp
}

fn failed(mut rp: RestoreRec, why: impl Into<String>) -> RestoreRec {
    rp.result = FAILED;
    rp.reason = why.into();
    rp
}

fn restore_entry(
    s: &Source,
    repo: &gix::Repository,
    run: &RunId,
    j: &mut Journal,
    k: usize,
    en: &Entry,
) -> RestoreRec {
    let r = s.r;
    let mut rp = rec_for(r, FAILED, String::new());
    let branch = BranchRef::parse(&en.reference);
    if branch.is_some() {
        rp.branch = en.reference.strip_prefix("refs/heads/").map(str::to_string);
    }
    let expected_text = if en.new_oid.is_empty() {
        &en.planned_new_oid
    } else {
        &en.new_oid
    };
    let expected = (!expected_text.is_empty()).then(|| Hex40::parse(expected_text));
    let old = Hex40::parse(&en.old_oid);
    let (Some(branch), Some(old)) = (branch, old) else {
        return failed(rp, "bad-record");
    };
    if matches!(expected, Some(None)) {
        return failed(rp, "bad-record");
    }
    let expected = expected.flatten();
    if en.state == journal::STATE_PREPARED && expected.is_none() {
        let named = &en.recovery_ref;
        rp.result = JOURNAL_INCOMPLETE;
        rp.reason = format!(
            "no planned new tip is recorded; the recovery ref {named} names the old tip for a manual reset"
        );
        return rp;
    }
    // where the branch really is, whatever the state of the entry says
    let cur = match repo.try_find_reference(branch.as_str()) {
        Ok(None) => return changed(rp, "the branch no longer exists"),
        Err(e) => return failed(rp, format!("the branch cannot be read: {e}")),
        Ok(Some(refr)) => match refr.target() {
            gix::refs::TargetRef::Object(id) => id.to_owned(),
            gix::refs::TargetRef::Symbolic(_) => {
                return changed(rp, "the branch ref is symbolic");
            }
        },
    };
    rp.old_oid = Some(cur.to_string());
    let done = |mut rp: RestoreRec, j: &mut Journal, result: &'static str| {
        rp.result = result;
        rp.new_oid = Some(old.as_str().to_string());
        rp.reason = if result == RESTORED {
            format!(
                "moved back from {} to {}",
                short(&cur.to_string()),
                short(old.as_str())
            )
        } else {
            "the branch is on the journaled old tip".into()
        };
        if en.state == journal::STATE_PREPARED {
            if let Err(e) = j.finish(
                k,
                journal::STATE_RESTORED,
                expected_text,
                "restored after an incomplete record",
            ) {
                rp.reason += &format!("; the record stays incomplete: {e}");
            }
        }
        rp
    };
    let old_id = gix::ObjectId::from_hex(old.as_str().as_bytes()).expect("40 hex digits");
    if cur == old_id {
        return done(rp, j, ALREADY_RESTORED);
    }
    // with no recorded tip (a rebase of the Go tool) `expected` is None and the branch cannot be on it
    if expected.map(|e| e.as_str().to_string()) != Some(cur.to_string()) {
        return changed(rp, "the branch moved since the run");
    }
    let Some(expected) = expected else {
        return changed(rp, "the branch moved since the run");
    };
    if repo.find_commit(old_id).is_err() {
        return failed(rp, "old-object-missing");
    }
    if let Some(op) = status::operation(repo) {
        return changed(rp, format!("{op} in progress in the worktree"));
    }
    let head_on_it = repo
        .head_name()
        .ok()
        .flatten()
        .is_some_and(|n| n.as_bstr() == en.reference.as_bytes());
    let runner = git_runner::shared();
    if head_on_it {
        match status::changes_opts(repo, false, true) {
            Ok(c) if c.dirty_paths == 0 && c.conflicts == 0 => {}
            // unreadable counts as dirty: nothing is moved
            _ => return changed(rp, "the worktree has local changes"),
        }
        rp.via = Some(git_runner::VIA);
        if let Err(e) = runner.run(&r.path, Op::ResetKeep { target: old }) {
            let e = e.to_string();
            return failed(rp, format!("git-failed: {e}"));
        }
        let now = repo
            .try_find_reference(branch.as_str())
            .ok()
            .flatten()
            .and_then(|x| x.target().try_id().map(|i| i.to_string()));
        if now.as_deref() != Some(old.as_str()) {
            return failed(rp, "raced");
        }
        return done(rp, j, RESTORED);
    }
    if held_elsewhere(repo, &en.reference) {
        return changed(rp, "the branch is checked out in another worktree");
    }
    rp.via = Some(git_runner::VIA);
    let moved = runner.run(
        &r.path,
        Op::UpdateBranchCas {
            run: run.clone(),
            branch,
            new: old,
            expected,
        },
    );
    match moved {
        Ok(_) => done(rp, j, RESTORED),
        // git refused the expected value: someone moved the branch meanwhile
        Err(GitError::Exit { .. }) => changed(rp, "the branch moved while restoring"),
        Err(e) => {
            let e = e.to_string();
            failed(rp, format!("git-failed: {e}"))
        }
    }
}

/// A rebase, bisect, merge, cherry-pick or revert is running in the worktree whose git directory is `git_dir`.
fn operation_in(git_dir: &std::path::Path) -> bool {
    [
        "rebase-merge",
        "rebase-apply",
        "BISECT_START",
        "MERGE_HEAD",
        "CHERRY_PICK_HEAD",
        "REVERT_HEAD",
    ]
    .iter()
    .any(|n| std::fs::symlink_metadata(git_dir.join(n)).is_ok())
}

/// Another worktree (linked, with relative links, or a dead registration) has the branch checked out, or the
/// worktrees cannot be listed: nothing is moved.
fn held_elsewhere(repo: &gix::Repository, full_ref: &str) -> bool {
    let Some(short_name) = full_ref.strip_prefix("refs/heads/") else {
        return true;
    };
    let Ok(w) = worktrees::scan(repo) else {
        return true;
    };
    // an operation in any other worktree (a rebase or bisect there has HEAD detached and works on the branch) stops
    // the restore of this repository: the branch would be moved under it
    if w.linked
        .iter()
        .chain(w.relative.iter())
        .any(|l| operation_in(&l.git_dir))
        || w.stale.iter().any(|l| operation_in(&l.git_dir))
    {
        return true;
    }
    let on = |b: &Option<String>| b.as_deref() == Some(short_name);
    w.in_use.iter().any(|(b, _)| b == short_name)
        || w.linked.iter().any(|l| on(&l.branch))
        || w.relative.iter().any(|l| on(&l.branch))
        || w.stale.iter().any(|l| on(&l.branch))
}

pub fn to_json(root: &str, r: &Restored, rejected: Value) -> Value {
    let count = |t: &str| r.repos.iter().filter(|x| x.result == t).count();
    let repos: Vec<Value> = r
        .repos
        .iter()
        .map(|x| {
            let mut v = json!({"name": x.name, "path": x.path, "branch": jopt(x.branch.as_deref()),
                               "result": x.result, "old_oid": jopt(x.old_oid.as_deref()),
                               "new_oid": jopt(x.new_oid.as_deref()), "reason": x.reason});
            if let Some(via) = x.via {
                v["via"] = json!(via);
            }
            v
        })
        .collect();
    json!({"schema_version": 2, "command": "sync", "mode": "restore", "root": root, "engine": engine(),
           "run_id": jopt(r.run_id.as_deref()), "repos": repos,
           "summary": {"total": r.repos.len(), "restored": count(RESTORED), "already_restored": count(ALREADY_RESTORED),
                       "changed_since": count(CHANGED_SINCE), "journal_incomplete": count(JOURNAL_INCOMPLETE),
                       "failed": count(FAILED)},
           "error": match &r.error { Some((code, message)) => json!({"code": code, "message": message}), None => Value::Null },
           "rejected_paths": rejected})
}

pub fn render_human(r: &Restored) -> String {
    let mut out = String::new();
    if let Some(id) = &r.run_id {
        out += &format!("sync restore of run {id}\n");
    }
    for x in &r.repos {
        out += &format!(
            "{:<18} {:<26} {:<18} {}\n",
            escape_untrusted(&x.name, 200).0,
            escape_untrusted(x.branch.as_deref().unwrap_or("-"), 200).0,
            x.result,
            escape_untrusted(&x.reason, 600).0
        );
    }
    let count = |t: &str| r.repos.iter().filter(|x| x.result == t).count();
    out += &format!(
        "{} restored, {} already restored, {} changed since, {} journal incomplete, {} failed\n",
        count(RESTORED),
        count(ALREADY_RESTORED),
        count(CHANGED_SINCE),
        count(JOURNAL_INCOMPLETE),
        count(FAILED)
    );
    out
}
