//! `stw oplog undo REF --to OLD --expect NEW [--keep] --yes`: move one local branch back to an older reflog value, with a
//! backup ref and a compare-and-swap update (`docs/oplog-format.md`, 8). The checks are those of `undo-plan` (same
//! function); this module adds the backup namespace `refs/stw/backup/<run>/<k>`, the counter ref, the retention, the reflog
//! entries (also with `core.logAllRefUpdates=false`) and the `refs.log` lines. A failed `refs.log` append never undoes the
//! update. With `--keep` the branch is the one the current worktree has checked out and `git reset --keep` (through the
//! runner, the only git program of `oplog`) moves branch, index and work tree (`docs/oplog-format.md`, 8a).
use std::collections::{BTreeMap, BTreeSet};
use std::time::Duration;

use gix::refs::transaction::{Change, LogChange, PreviousValue, RefEdit, RefLog};
use gix::refs::{FullName, Target};
use gix::ObjectId;
use serde_json::{json, Value};

use super::timeline::SCHEMA_VERSION;
use super::undo_plan::{self, Plan};
use super::write::{log_ref_update, RefUpdate};
use super::Ctx;
use crate::error::{Error, Result};
use crate::git_runner::{self, GitError, Hex40, Op};
use crate::github::reads::escape_untrusted;

/// The counter ref: its blob holds `<run> <pid>-<nanos>`; a run number is never reused (spec 8, item 3).
pub const COUNTER_REF: &str = "refs/stw/backup-counter";
/// Prefix of the backup refs: `refs/stw/backup/<run>/<k>`.
pub const BACKUP_PREFIX: &str = "refs/stw/backup/";
/// Runs kept after a new run is created (all `k` of a run together).
pub const RETAINED_RUNS: usize = 10;
/// Tries to take a run number against other writers.
pub const COUNTER_TRIES: u32 = 5;
/// The reflog message of everything undo writes.
const MESSAGE: &str = "stw oplog undo";

pub struct Request {
    pub reference: String,
    pub to: String,
    pub expect: String,
    /// The `--keep` path (a checked-out branch of the current worktree, moved with `git reset --keep`).
    pub keep: bool,
}

pub struct Done {
    pub reference: String,
    pub previous: String,
    pub current: String,
    pub backup_ref: String,
    pub run: u64,
    pub pruned: Vec<String>,
    pub notices: Vec<String>,
    pub way_back: String,
    /// Done through `git reset --keep` (index and work tree moved with the branch).
    pub keep: bool,
}

/// What `undo` returns: the plan that refused (nothing changed), or the done undo.
pub enum Outcome {
    Refused(Box<Plan>),
    Done(Box<Done>),
}

fn esc(s: &str) -> String {
    escape_untrusted(s, 300).0
}

fn full_name(name: &str) -> std::result::Result<FullName, String> {
    name.try_into()
        .map_err(|e: gix::refs::name::Error| e.to_string())
}

/// Apply ref edits through a ref store handle made for this call that writes reflogs whatever `core.logAllRefUpdates`
/// says (spec 8, item 5). With the repository's own handle the setting `false` makes gix skip the entry, and
/// `force_create_reflog` on the edit has no effect then. The repository's config is not touched.
fn edit_with_reflog(
    repo: &gix::Repository,
    edits: Vec<RefEdit>,
) -> std::result::Result<(), String> {
    let mut store = repo.refs.clone();
    store.write_reflog = gix::refs::store::WriteReflog::Always;
    let lock = gix::lock::acquire::Fail::AfterDurationWithBackoff(Duration::from_millis(500));
    let committer = repo.committer().transpose().map_err(|e| e.to_string())?;
    let result = store
        .transaction()
        .prepare(edits, lock, lock)
        .map_err(|e| e.to_string())
        .and_then(|t| t.commit(committer).map(|_| ()).map_err(|e| e.to_string()));
    result
}

fn update_edit(name: FullName, expected: PreviousValue, new: ObjectId) -> RefEdit {
    RefEdit {
        change: Change::Update {
            log: LogChange {
                mode: RefLog::AndReference,
                force_create_reflog: true,
                message: MESSAGE.into(),
            },
            expected,
            new: Target::Object(new),
        },
        name,
        deref: false,
    }
}

fn delete_edit(name: FullName, old: ObjectId) -> RefEdit {
    RefEdit {
        change: Change::Delete {
            expected: PreviousValue::MustExistAndMatch(Target::Object(old)),
            log: RefLog::AndReference,
        },
        name,
        deref: false,
    }
}

/// The value of a direct ref, `None` when it does not exist; an error for a symbolic ref (never followed).
fn direct_value(
    repo: &gix::Repository,
    name: &str,
) -> std::result::Result<Option<ObjectId>, String> {
    match repo.try_find_reference(name).map_err(|e| e.to_string())? {
        None => Ok(None),
        Some(r) => match r.target() {
            gix::refs::TargetRef::Object(id) => Ok(Some(id.to_owned())),
            gix::refs::TargetRef::Symbolic(_) => Err(format!("{name} is a symbolic ref")),
        },
    }
}

/// The run number in the blob of the counter ref (`<run> <nonce>`).
fn counter_run(repo: &gix::Repository, blob: ObjectId) -> std::result::Result<u64, String> {
    let obj = repo.find_object(blob).map_err(|e| e.to_string())?;
    let text = String::from_utf8_lossy(&obj.data).into_owned();
    text.split(' ')
        .next()
        .and_then(|n| n.parse::<u64>().ok())
        .ok_or_else(|| format!("{COUNTER_REF} does not hold a run number"))
}

fn nonce() -> String {
    let nanos = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_nanos())
        .unwrap_or(0);
    format!("{}-{nanos}", std::process::id())
}

/// Take the next run number: the larger of the counter and the highest run that exists, plus one. Read the counter, write a new blob `<run> <pid>-<nanos>` and update the counter ref with a
/// compare-and-swap. A lost race (another run took the number) reads the counter again; after `tries` tries it fails.
/// `between` is called after the counter was read and before the update (the tests make another writer win here).
/// Returns the run and the counter update (old blob, new blob) for the log.
pub fn take_run_with(
    repo: &gix::Repository,
    tries: u32,
    between: &mut dyn FnMut(u32),
) -> std::result::Result<(u64, Option<ObjectId>, ObjectId), String> {
    let mut last = String::new();
    for attempt in 0..tries {
        let prev = direct_value(repo, COUNTER_REF)?;
        // never below a run that exists: a counter that was deleted or lowered by hand must not hand out a number
        // whose backup ref is there already, or whose run the prune would remove at once
        let existing = backup_refs(repo)?
            .into_iter()
            .map(|(run, _, _)| run)
            .max()
            .unwrap_or(0);
        let counted = match prev {
            Some(blob) => counter_run(repo, blob)?,
            None => 0,
        };
        let run = counted.max(existing).saturating_add(1);
        let blob = repo
            .write_blob(format!("{run} {}", nonce()).as_bytes())
            .map_err(|e| e.to_string())?
            .detach();
        between(attempt);
        let expected = match prev {
            Some(p) => PreviousValue::MustExistAndMatch(Target::Object(p)),
            None => PreviousValue::MustNotExist,
        };
        let edit = RefEdit {
            change: Change::Update {
                log: LogChange {
                    mode: RefLog::AndReference,
                    force_create_reflog: false,
                    message: MESSAGE.into(),
                },
                expected,
                new: Target::Object(blob),
            },
            name: full_name(COUNTER_REF)?,
            deref: false,
        };
        match repo.edit_reference(edit) {
            Ok(_) => return Ok((run, prev, blob)),
            Err(e) => last = e.to_string(),
        }
    }
    Err(format!(
        "could not take a run number for the backup ref after {tries} tries (another stw keeps winning): {last}"
    ))
}

/// Create the backup ref at `value`. Create-only: gix lets `MustNotExist` pass when the ref already holds the same value
/// (gix-ref `prepare.rs`), so the ref is looked up first and an existing one, with any value, is an error.
pub fn create_backup_with(
    repo: &gix::Repository,
    name: &str,
    value: ObjectId,
    between: &mut dyn FnMut(),
) -> std::result::Result<(), String> {
    if repo
        .try_find_reference(name)
        .map_err(|e| e.to_string())?
        .is_some()
    {
        return Err(format!(
            "{name} exists already; a backup ref is never replaced"
        ));
    }
    // between the lookup and the edit (the tests let another writer create the ref here)
    between();
    edit_with_reflog(
        repo,
        vec![update_edit(
            full_name(name)?,
            PreviousValue::MustNotExist,
            value,
        )],
    )
}

/// A run number or `k` as written by stw: a canonical decimal number, positive, no sign and no leading zero. `u64::parse`
/// alone also takes `+1` and `01`; such names are no runs and stay untouched.
fn canonical(s: &str) -> Option<u64> {
    let b = s.as_bytes();
    if b.is_empty() || b[0] == b'0' || !b.iter().all(u8::is_ascii_digit) {
        return None;
    }
    s.parse().ok()
}

/// `(run, k)` of the refs below `refs/stw/backup/` with their values; names that do not have the form are ignored.
fn backup_refs(
    repo: &gix::Repository,
) -> std::result::Result<Vec<(u64, String, ObjectId)>, String> {
    let mut out = Vec::new();
    let platform = repo.references().map_err(|e| e.to_string())?;
    let iter = platform
        .prefixed(BACKUP_PREFIX)
        .map_err(|e| e.to_string())?;
    for r in iter.flatten() {
        let name = r.name().as_bstr().to_string();
        let Some(rest) = name.strip_prefix(BACKUP_PREFIX) else {
            continue;
        };
        let Some((run, k)) = rest.split_once('/') else {
            continue;
        };
        let (Some(run), true) = (canonical(run), canonical(k).is_some()) else {
            continue;
        };
        if let gix::refs::TargetRef::Object(id) = r.target() {
            out.push((run, name, id.to_owned()));
        }
    }
    Ok(out)
}

/// Refs deleted by the retention (name, old value) and the failures (text).
pub type Pruned = (Vec<(String, ObjectId)>, Vec<String>);

/// Delete the refs of all runs but the `RETAINED_RUNS` highest and the run `own` that this call was made for (the newest
/// run of the caller is never removed, whatever other writers did meanwhile); the counter ref is not below the prefix and stays.
/// Returns the deleted refs (name, old value) and the failures.
pub fn prune(repo: &gix::Repository, own: u64) -> std::result::Result<Pruned, String> {
    let refs = backup_refs(repo)?;
    let runs: BTreeSet<u64> = refs.iter().map(|(r, _, _)| *r).collect();
    let keep: BTreeSet<u64> = runs.iter().rev().take(RETAINED_RUNS).copied().collect();
    let (mut gone, mut failed) = (Vec::new(), Vec::new());
    for (run, name, id) in refs {
        if keep.contains(&run) || run == own {
            continue;
        }
        match full_name(&name).and_then(|n| edit_with_reflog(repo, vec![delete_edit(n, id)])) {
            Ok(()) => gone.push((name, id)),
            Err(e) => failed.push(format!("{name}: {e}")),
        }
    }
    Ok((gone, failed))
}

fn is_full_hex(s: &str, len: usize) -> bool {
    s.len() == len && s.bytes().all(|b| matches!(b, b'0'..=b'9' | b'a'..=b'f'))
}

/// `undo` with a hook that runs after the backup exists and before the update of the branch (tests move the branch
/// there). The refusals of `undo-plan` and a wrong `--expect` change nothing.
pub fn undo_with(ctx: &Ctx, req: &Request, before_update: &mut dyn FnMut()) -> Result<Outcome> {
    undo_hooked(ctx, req, &mut |_| {}, before_update)
}

/// `undo_with` and a hook for the counter: `between` is called after the counter was read and before it is updated, once
/// per try (the tests let another writer win there, through the same path the command takes).
pub fn undo_hooked(
    ctx: &Ctx,
    req: &Request,
    between: &mut dyn FnMut(u32),
    before_update: &mut dyn FnMut(),
) -> Result<Outcome> {
    let repo = ctx.repo()?;
    let plan = undo_plan::plan(
        ctx,
        &undo_plan::Request {
            reference: req.reference.clone(),
            to: Some(req.to.clone()),
            steps: None,
            keep: req.keep,
        },
    )?;
    let mut plan = plan;
    if !plan.branch {
        return Err(Error::usage(format!(
            "{} is not a local branch; `undo` handles refs/heads/* only",
            esc(&plan.reference)
        )));
    }
    if !plan.executable() {
        return Ok(Outcome::Refused(Box::new(plan)));
    }
    let (Some(cur_s), Some(to_s)) = (plan.current.clone(), plan.target.clone()) else {
        return Ok(Outcome::Refused(Box::new(plan)));
    };
    let hex_len = cur_s.len();
    if !is_full_hex(&req.expect, hex_len) {
        return Err(Error::usage(
            "--expect must be the full current value of the ref (lower-case hex)",
        ));
    }
    if req.expect != cur_s {
        plan.refused += 1;
        plan.checks.push(undo_plan::Check {
            status: "refused",
            text: format!(
                "ref changed since: --expect {} but the ref is at {cur_s}; nothing was changed",
                esc(&req.expect)
            ),
        });
        return Ok(Outcome::Refused(Box::new(plan)));
    }
    let expect =
        ObjectId::from_hex(cur_s.as_bytes()).map_err(|e| Error::repository(e.to_string()))?;
    let to = ObjectId::from_hex(to_s.as_bytes()).map_err(|e| Error::repository(e.to_string()))?;

    // `--keep` needs a usable git before anything is written (no backup ref for an undo that cannot start)
    let keep_target = if req.keep {
        match undo_plan::keep_git_check(&to_s) {
            Ok(t) => Some(t),
            Err(why) => {
                plan.refused += 1;
                plan.checks.push(undo_plan::Check {
                    status: "refused",
                    text: why,
                });
                return Ok(Outcome::Refused(Box::new(plan)));
            }
        }
    } else {
        None
    };

    // the run number and the backup ref
    let (run, prev, blob) =
        take_run_with(repo, COUNTER_TRIES, between).map_err(Error::repository)?;
    log_ref_update(
        repo,
        &RefUpdate {
            refname: COUNTER_REF,
            old: prev,
            new: Some(blob),
        },
    );
    let backup_ref = format!("{BACKUP_PREFIX}{run}/1");
    create_backup_with(repo, &backup_ref, expect, &mut || {}).map_err(|e| {
        Error::repository(format!(
            "backup ref {backup_ref} not created, nothing was changed: {e}"
        ))
    })?;
    log_ref_update(
        repo,
        &RefUpdate {
            refname: &backup_ref,
            old: None,
            new: Some(expect),
        },
    );
    let mut notices = Vec::new();
    let mut pruned = Vec::new();
    match prune(repo, run) {
        Ok((gone, failed)) => {
            for (name, id) in &gone {
                log_ref_update(
                    repo,
                    &RefUpdate {
                        refname: name,
                        old: Some(*id),
                        new: None,
                    },
                );
            }
            notices.extend(
                failed
                    .into_iter()
                    .map(|f| format!("old backup not removed: {f}")),
            );
            pruned = gone.into_iter().map(|(n, _)| n).collect();
        }
        Err(e) => notices.push(format!("backup retention not applied: {e}")),
    }
    before_update();
    if let Some(target) = keep_target {
        return keep_update(
            repo, &plan, expect, to, target, backup_ref, run, pruned, notices,
        );
    }
    // CAS: the ref must still hold --expect; a change since the checks fails the update and leaves the backup
    let name = full_name(&plan.reference).map_err(Error::repository)?;
    edit_with_reflog(
        repo,
        vec![update_edit(
            name,
            PreviousValue::MustExistAndMatch(Target::Object(expect)),
            to,
        )],
    )
    .map_err(|e| {
        Error::repository(format!(
            "the ref was not changed ({e}); it changed since the checks, or the update failed. The backup {backup_ref} stays ({expect})"
        ))
    })?;
    log_ref_update(
        repo,
        &RefUpdate {
            refname: &plan.reference,
            old: Some(expect),
            new: Some(to),
        },
    );
    let way_back = undo_plan::undo_command(
        &plan.reference,
        &expect.to_string(),
        &to.to_string(),
        false,
        false,
    );
    Ok(Outcome::Done(Box::new(Done {
        reference: plan.reference,
        previous: expect.to_string(),
        current: to.to_string(),
        backup_ref,
        run,
        pruned,
        notices,
        way_back,
        keep: false,
    })))
}

/// The `--keep` update: the branch, the index and the work tree are moved by `git reset --keep <old>` in the worktree of
/// `repo`. The ref is read again right before (it must still hold `expect` and HEAD must still be on it: git's own update
/// has no compare-and-swap, so a change inside git's own run is not caught). A failed git leaves the branch where it was;
/// the backup stays.
#[allow(clippy::too_many_arguments)]
fn keep_update(
    repo: &gix::Repository,
    plan: &Plan,
    expect: ObjectId,
    to: ObjectId,
    target: Hex40,
    backup_ref: String,
    run: u64,
    pruned: Vec<String>,
    notices: Vec<String>,
) -> Result<Outcome> {
    let stays = |why: String| {
        Error::repository(format!(
            "the branch was not changed ({why}). The backup {backup_ref} stays ({expect})"
        ))
    };
    let Some(workdir) = repo.workdir().map(std::path::Path::to_path_buf) else {
        return Err(stays("this repository has no work tree".into()));
    };
    let holds = |at: ObjectId| direct_value(repo, &plan.reference).map(|v| v == Some(at));
    if !holds(expect).map_err(stays)? {
        return Err(stays(
            "it changed since the checks; nothing was moved".into(),
        ));
    }
    let head_on_it = repo
        .head_name()
        .ok()
        .flatten()
        .is_some_and(|n| n.as_bstr() == plan.reference.as_bytes());
    if !head_on_it {
        return Err(stays(
            "HEAD is no longer on the branch; nothing was moved".into(),
        ));
    }
    if let Err(e) = git_runner::shared().run(&workdir, Op::ResetKeep { target }) {
        let text = esc(&e.to_string());
        // a git that was killed or stopped may have written part of the work tree; one that exited or never started did not
        return Err(match e {
            GitError::Timeout { .. }
            | GitError::OutputLimit { .. }
            | GitError::Exit { code: None, .. } => Error::repository(format!(
                "git reset --keep did not finish ({text}): the branch and the work tree may be changed in part; look at `git status` and `git reflog`. The backup {backup_ref} stays ({expect})"
            )),
            _ => stays(format!("git reset --keep failed: {text}")),
        });
    }
    log_ref_update(
        repo,
        &RefUpdate {
            refname: &plan.reference,
            old: Some(expect),
            new: Some(to),
        },
    );
    let way_back = undo_plan::undo_command(
        &plan.reference,
        &expect.to_string(),
        &to.to_string(),
        false,
        true,
    );
    Ok(Outcome::Done(Box::new(Done {
        reference: plan.reference.clone(),
        previous: expect.to_string(),
        current: to.to_string(),
        backup_ref,
        run,
        pruned,
        notices,
        way_back,
        keep: true,
    })))
}

pub fn undo(ctx: &Ctx, req: &Request) -> Result<Outcome> {
    undo_with(ctx, req, &mut || {})
}

impl Done {
    pub fn render_human(&self) -> String {
        let mut b = format!(
            "undo {}: {} -> {}\nbackup ref: {} (holds {})\nway back:   {}\n",
            esc(&self.reference),
            self.previous,
            self.current,
            self.backup_ref,
            self.previous,
            // the text is made to be pasted into a shell: the name is quoted there, the JSON field keeps the raw one
            undo_plan::undo_command(
                &self.reference,
                &self.previous,
                &self.current,
                true,
                self.keep
            )
        );
        if !self.pruned.is_empty() {
            b.push_str(&format!(
                "removed old backup refs: {} (the newest {RETAINED_RUNS} runs are kept)\n",
                self.pruned.len()
            ));
        }
        for n in &self.notices {
            b.push_str(&format!("notice: {}\n", esc(n)));
        }
        if self.keep {
            b.push_str("Result: done. The branch, the index and the work tree of this worktree were moved with `git reset --keep` (untracked files that are not in the way, other worktrees and pushes are not touched).\n");
        } else {
            b.push_str("Result: done. Only the ref was moved (index, working tree, other worktrees and pushes are not touched).\n");
        }
        b
    }

    pub fn to_json(&self) -> Value {
        json!({
            "schema_version": SCHEMA_VERSION, "command": "oplog undo", "engine": crate::output::engine(),
            "ref": self.reference, "executed": true, "previous": self.previous, "current": self.current,
            "backup_ref": self.backup_ref, "run": self.run, "pruned": self.pruned,
            "way_back": self.way_back, "notices": self.notices,
            "keep": self.keep, "via": self.keep.then_some(git_runner::VIA),
            "result": "done",
        })
    }
}

/// Run numbers present below the backup prefix with their refs (used by the tests and `--json` readers of the namespace).
pub fn runs(repo: &gix::Repository) -> std::result::Result<BTreeMap<u64, Vec<String>>, String> {
    let mut m: BTreeMap<u64, Vec<String>> = BTreeMap::new();
    for (run, name, _) in backup_refs(repo)? {
        m.entry(run).or_default().push(name);
    }
    Ok(m)
}
