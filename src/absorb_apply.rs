//! `stw absorb --apply`: fold the staged changes the plan named, and only those.
//!
//! The plan is computed again and refused unless `--plan-sum` is its checksum. Before the first
//! fixup object, the branch tip is copied to `refs/stw/backup/<run>/1` and that update is written
//! to the op log. The way back is `stw oplog undo` of the branch to that tip.
//!
//! Fixup commits are written with gix (one per target, oldest first). The branch is then rewritten
//! with `git rebase --autosquash` through the runner (`GIT_SEQUENCE_EDITOR=true`, no hooks). A
//! failed rebase leaves the backup ref.
// SPDX-License-Identifier: EUPL-1.2 AND BSD-3-Clause
use std::time::Duration;

use gix::bstr::ByteSlice;
use gix::objs::tree::EntryKind;
use gix::refs::transaction::{Change, LogChange, PreviousValue, RefEdit, RefLog};
use gix::refs::Target;
use gix::ObjectId;
use serde_json::{json, Value};

use crate::absorb::{self, Fail, Options};
use crate::git_runner::{Hex40, Op, Runner};
use crate::oplog::undo::{
    create_backup_with, take_run_with, BACKUP_PREFIX, COUNTER_REF, COUNTER_TRIES,
};
use crate::oplog::write::{log_ref_update, RefUpdate};

/// What a successful fold wrote.
pub struct Applied {
    pub old_head: String,
    pub new_head: String,
    pub branch: String,
    pub reference: String,
    pub backup_ref: String,
    pub plan_sum: String,
}

impl Applied {
    pub fn to_json(&self) -> Value {
        json!({
            "schema_version": absorb::SCHEMA_VERSION,
            "command": "absorb",
            "mode": "apply",
            "old_head": self.old_head,
            "new_head": self.new_head,
            "branch": self.branch,
            "ref": self.reference,
            "backup_ref": self.backup_ref,
            "plan_sum": self.plan_sum,
            "way_back": way_back(&self.reference, &self.old_head, &self.new_head),
        })
    }

    pub fn render_human(&self) -> String {
        format!(
            "absorb apply: head {} -> {} on {}\nbackup ref: {} (holds {})\nway back:   {}\n",
            &self.old_head[..7],
            &self.new_head[..7],
            self.branch,
            self.backup_ref,
            &self.old_head[..7],
            way_back(&self.reference, &self.old_head, &self.new_head),
        )
    }
}

fn way_back(reference: &str, old_head: &str, new_head: &str) -> String {
    format!("stw oplog undo {reference} --to {old_head} --expect {new_head} --keep --yes")
}

fn fail(code: &'static str, message: impl Into<String>) -> Fail {
    Fail {
        code,
        message: message.into(),
    }
}

/// Fold the plan's absorbed hunks. A checksum mismatch or an index that is not exactly those
/// hunks writes nothing, including no backup ref.
pub fn apply(
    dir: &std::path::Path,
    start: &std::path::Path,
    runner: &Runner,
    opts: &Options,
    given_sum: &str,
) -> Result<Applied, Fail> {
    if given_sum.len() != 64
        || !given_sum
            .bytes()
            .all(|b| matches!(b, b'0'..=b'9' | b'a'..=b'f'))
    {
        return Err(fail(
            "bad-plan-sum",
            "--plan-sum must be 64 lower-case hex digits; nothing was written",
        ));
    }
    let plan = absorb::plan(dir, start, runner, opts)?;
    let doc = plan.to_json();
    let actual = doc["plan_sum"].as_str().unwrap_or("");
    if actual != given_sum {
        return Err(fail(
            "plan-sum-mismatch",
            "the plan checksum does not match this index; nothing was written",
        ));
    }
    let mut ready = ready(doc)?;
    let repo = gix::open(dir).map_err(|e| {
        fail(
            "git-failed",
            format!("the repository could not be opened: {e}"),
        )
    })?;
    let old_head = ObjectId::from_hex(ready.old_head.as_bytes())
        .map_err(|e| fail("git-failed", e.to_string()))?;
    worktree_matches(&repo, &ready)?;

    let backup_ref = snapshot(&repo, old_head)?;
    let tip = match fixups(&repo, old_head, &mut ready) {
        Ok(id) => id,
        Err(e) => {
            return Err(fail(
                e.code,
                format!(
                    "{}; the backup {backup_ref} stays and holds {}",
                    e.message, ready.old_head
                ),
            ))
        }
    };
    if let Err(e) = move_branch(&repo, &ready.reference, old_head, tip) {
        return Err(fail(
            "apply-failed",
            format!(
                "the branch was not moved ({e}); the backup {backup_ref} stays and holds {}",
                ready.old_head
            ),
        ));
    }
    log_ref_update(
        &repo,
        &RefUpdate {
            refname: &ready.reference,
            old: Some(old_head),
            new: Some(tip),
        },
    );
    let onto = match &ready.onto {
        Some(sha) => Some(Hex40::parse(sha).ok_or_else(|| {
            fail(
                "apply-failed",
                format!("the rebase base is not 40 hex; the backup {backup_ref} stays"),
            )
        })?),
        None => None,
    };
    if let Err(e) = runner.run(dir, Op::AbsorbAutosquash { onto }) {
        return Err(fail(
            "apply-failed",
            format!(
                "the rebase did not finish ({e}). The backup {backup_ref} stays and holds {}. Way back: {}",
                ready.old_head,
                way_back(&ready.reference, &ready.old_head, &tip.to_string())
            ),
        ));
    }
    let mut head_ref = repo.find_reference(&ready.reference).map_err(|e| {
        fail(
            "apply-failed",
            format!("the branch could not be read after the rebase: {e}"),
        )
    })?;
    let new_head = head_ref
        .peel_to_id()
        .map_err(|e| fail("apply-failed", e.to_string()))?;
    if new_head == old_head {
        return Err(fail(
            "apply-failed",
            format!(
                "the rebase left the branch on {}; the backup {backup_ref} stays",
                ready.old_head
            ),
        ));
    }
    Ok(Applied {
        old_head: ready.old_head,
        new_head: new_head.to_string(),
        branch: ready.branch,
        reference: ready.reference,
        backup_ref,
        plan_sum: given_sum.to_string(),
    })
}

struct Ready {
    old_head: String,
    branch: String,
    reference: String,
    /// Parent of the oldest rewritten commit. `None` rebases `--root`.
    onto: Option<String>,
    /// Oldest target first: the target sha, then the index paths of its hunks.
    fixups: Vec<(String, Vec<String>)>,
}

fn ready(doc: &Value) -> Result<Ready, Fail> {
    let skipped = doc["files_skipped"]
        .as_array()
        .map(|a| a.len())
        .unwrap_or(0);
    let hunks = doc["hunks"].as_array().cloned().unwrap_or_default();
    if hunks.is_empty() || hunks.iter().any(|h| h["assignment"] != "absorbed") || skipped > 0 {
        return Err(fail(
            "nothing-to-apply",
            "the plan does not absorb every staged change; nothing was written",
        ));
    }
    let mut by_target: Vec<(String, Vec<String>)> = Vec::new();
    for hunk in &hunks {
        let path = hunk["path"].as_str().unwrap_or("").to_string();
        let sha = hunk["target"]["sha"].as_str().unwrap_or("").to_string();
        if path.is_empty() || sha.len() != 40 {
            return Err(fail(
                "nothing-to-apply",
                "an absorbed hunk has no target; nothing was written",
            ));
        }
        match by_target.iter_mut().find(|(s, _)| s == &sha) {
            Some((_, paths)) if !paths.iter().any(|p| p == &path) => paths.push(path),
            Some(_) => {}
            None => by_target.push((sha, vec![path])),
        }
    }
    let commits = doc["rewrite"]["commits"]
        .as_array()
        .cloned()
        .unwrap_or_default();
    by_target.sort_by_key(|(sha, _)| {
        commits
            .iter()
            .position(|c| c.as_str() == Some(sha))
            .unwrap_or(usize::MAX)
    });
    let reference = doc["rewrite"]["ref"].as_str().unwrap_or("").to_string();
    let branch = doc["branch"].as_str().unwrap_or("").to_string();
    let old_head = doc["head"].as_str().unwrap_or("").to_string();
    if !reference.starts_with("refs/heads/") || old_head.len() != 40 || by_target.is_empty() {
        return Err(fail(
            "apply-failed",
            "the plan has no local branch; nothing was written",
        ));
    }
    let onto = commits.first().and_then(|c| c.as_str()).map(str::to_string);
    Ok(Ready {
        old_head,
        branch,
        reference,
        onto,
        fixups: by_target,
    })
}

fn worktree_matches(repo: &gix::Repository, ready: &Ready) -> Result<(), Fail> {
    let index = repo
        .index_or_empty()
        .map_err(|e| fail("git-failed", e.to_string()))?;
    let work = repo.workdir().ok_or_else(|| {
        fail(
            "apply-failed",
            "the repository has no work tree; nothing was written",
        )
    })?;
    for (_, paths) in &ready.fixups {
        for path in paths {
            let Some(entry) = index.entry_by_path(path.as_bytes().as_bstr()) else {
                return Err(fail(
                    "apply-failed",
                    format!("the index has no {path}; nothing was written"),
                ));
            };
            let blob = repo
                .find_object(entry.id)
                .map_err(|e| fail("git-failed", e.to_string()))?;
            let on_disk = std::fs::read(work.join(path)).map_err(|_| {
                fail(
                    "dirty-work-tree",
                    format!(
                        "{path} in the work tree does not match the index; nothing was written"
                    ),
                )
            })?;
            if on_disk.as_slice() != blob.data.as_slice() {
                return Err(fail(
                    "dirty-work-tree",
                    format!(
                        "{path} in the work tree does not match the index; nothing was written"
                    ),
                ));
            }
        }
    }
    Ok(())
}

fn snapshot(repo: &gix::Repository, head: ObjectId) -> Result<String, Fail> {
    let (run, prev, blob) = take_run_with(repo, COUNTER_TRIES, &mut |_| {}).map_err(|e| {
        fail(
            "apply-failed",
            format!("the op log snapshot was not started: {e}"),
        )
    })?;
    log_ref_update(
        repo,
        &RefUpdate {
            refname: COUNTER_REF,
            old: prev,
            new: Some(blob),
        },
    );
    let backup_ref = format!("{BACKUP_PREFIX}{run}/1");
    create_backup_with(repo, &backup_ref, head, &mut || {}).map_err(|e| {
        fail(
            "apply-failed",
            format!("the backup ref was not created, nothing of the branch was changed: {e}"),
        )
    })?;
    log_ref_update(
        repo,
        &RefUpdate {
            refname: &backup_ref,
            old: None,
            new: Some(head),
        },
    );
    Ok(backup_ref)
}

fn fixups(repo: &gix::Repository, head: ObjectId, ready: &mut Ready) -> Result<ObjectId, Fail> {
    let oldest = ready.onto.clone().ok_or_else(|| {
        fail(
            "nothing-to-apply",
            "the plan rewrites no commit; nothing of the branch was changed",
        )
    })?;
    let oldest_id =
        ObjectId::from_hex(oldest.as_bytes()).map_err(|e| fail("git-failed", e.to_string()))?;
    let oldest_commit = repo
        .find_commit(oldest_id)
        .map_err(|e| fail("git-failed", e.to_string()))?;
    let parents: Vec<ObjectId> = oldest_commit.parent_ids().map(|id| id.detach()).collect();
    ready.onto = parents.first().map(ToString::to_string);
    let (name, email) = identity(repo)?;
    let index = repo
        .index_or_empty()
        .map_err(|e| fail("git-failed", e.to_string()))?;
    let mut tip = head;
    let mut tree = repo
        .find_commit(head)
        .and_then(|c| c.tree_id().map(|id| id.detach()))
        .map_err(|e| fail("git-failed", e.to_string()))?;
    for (sha, paths) in &ready.fixups {
        let subject = subject_of(repo, sha)?;
        let mut editor = repo
            .edit_tree(tree)
            .map_err(|e| fail("git-failed", e.to_string()))?;
        for path in paths {
            let entry = index
                .entry_by_path(path.as_bytes().as_bstr())
                .ok_or_else(|| fail("apply-failed", format!("the index has no {path}")))?;
            let kind = if entry.mode.bits() == 0o100755 {
                EntryKind::BlobExecutable
            } else {
                EntryKind::Blob
            };
            let id = entry.id;
            editor
                .upsert(path.as_str(), kind, id)
                .map_err(|e| fail("apply-failed", e.to_string()))?;
        }
        tree = editor
            .write()
            .map_err(|e| fail("git-failed", e.to_string()))?
            .detach();
        let signature = gix::actor::Signature {
            name: name.clone().into(),
            email: email.clone().into(),
            time: gix::date::Time::now_utc(),
        };
        let commit = gix::objs::Commit {
            tree,
            parents: vec![tip].into(),
            author: signature.clone(),
            committer: signature,
            encoding: None,
            message: format!("fixup! {subject}\n").into(),
            extra_headers: vec![],
        };
        tip = repo
            .write_object(&commit)
            .map_err(|e| fail("git-failed", e.to_string()))?
            .detach();
    }
    Ok(tip)
}

fn subject_of(repo: &gix::Repository, sha: &str) -> Result<String, Fail> {
    let id = ObjectId::from_hex(sha.as_bytes()).map_err(|e| fail("git-failed", e.to_string()))?;
    let commit = repo
        .find_commit(id)
        .map_err(|e| fail("git-failed", e.to_string()))?;
    let message = commit
        .message_raw()
        .map_err(|e| fail("git-failed", e.to_string()))?;
    let line = message
        .to_str()
        .unwrap_or("")
        .lines()
        .next()
        .unwrap_or("")
        .to_string();
    if line.is_empty() {
        return Err(fail("apply-failed", "a target commit has an empty subject"));
    }
    Ok(line)
}

fn identity(repo: &gix::Repository) -> Result<(String, String), Fail> {
    let snap = repo.config_snapshot();
    let one = |key: &'static str| {
        snap.string(key)
            .filter(|v| !v.is_empty())
            .map(|v| v.to_string())
            .ok_or_else(|| fail(key, format!("{key} is not set in the Git configuration")))
    };
    let name = one("user.name").map_err(|_| {
        fail(
            "no-identity",
            "user.name is not set in the Git configuration",
        )
    })?;
    let email = one("user.email").map_err(|_| {
        fail(
            "no-identity",
            "user.email is not set in the Git configuration",
        )
    })?;
    Ok((name, email))
}

fn move_branch(
    repo: &gix::Repository,
    name: &str,
    old: ObjectId,
    new: ObjectId,
) -> Result<(), String> {
    let mut store = repo.refs.clone();
    store.write_reflog = gix::refs::store::WriteReflog::Always;
    let full = gix::refs::FullName::try_from(name).map_err(|e| e.to_string())?;
    let edit = RefEdit {
        change: Change::Update {
            log: LogChange {
                mode: RefLog::AndReference,
                force_create_reflog: true,
                message: "stw absorb apply".into(),
            },
            expected: PreviousValue::MustExistAndMatch(Target::Object(old)),
            new: Target::Object(new),
        },
        name: full,
        deref: false,
    };
    let lock = gix::lock::acquire::Fail::AfterDurationWithBackoff(Duration::from_millis(500));
    let committer = repo.committer().transpose().map_err(|e| e.to_string())?;
    let result = store
        .transaction()
        .prepare(vec![edit], lock, lock)
        .map_err(|e| e.to_string())
        .and_then(|t| t.commit(committer).map(|_| ()).map_err(|e| e.to_string()));
    result
}
