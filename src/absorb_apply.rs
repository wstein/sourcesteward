//! `stw absorb --apply`: fold the staged changes the plan named.
//!
//! The plan is computed again and refused unless `--plan-sum` is its checksum. The rewritten
//! commits are built in memory. Before the branch ref moves, the tip tree must be the old HEAD
//! tree plus exactly the absorbed hunks, and the index must already be what it should be
//! afterwards (absorbed hunks no longer staged, the others still staged). The work tree is not
//! written. The branch tip is copied to `refs/stw/backup/<run>/1` first. The way back is
//! `stw oplog undo`.
// SPDX-License-Identifier: EUPL-1.2 AND BSD-3-Clause
use std::collections::BTreeSet;
use std::time::Duration;

use gix::bstr::ByteSlice;
use gix::objs::tree::EntryKind;
use gix::refs::transaction::{Change, LogChange, PreviousValue, RefEdit, RefLog};
use gix::refs::Target;
use gix::ObjectId;
use serde_json::{json, Value};

use crate::absorb::{self, Fail, Options};
use crate::git_runner::Runner;
use crate::oplog::undo::{
    create_backup_with, take_run_with, BACKUP_PREFIX, COUNTER_REF, COUNTER_TRIES,
};
use crate::oplog::write::{log_ref_update, RefUpdate};
use crate::replay::{self, Status};

/// What a successful fold wrote.
pub struct Applied {
    pub old_head: String,
    pub new_head: String,
    pub branch: String,
    pub reference: String,
    pub backup_ref: String,
    pub plan_sum: String,
    pub keep: bool,
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
            "way_back": way_back(&self.reference, &self.old_head, &self.new_head, self.keep),
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
            way_back(&self.reference, &self.old_head, &self.new_head, self.keep),
        )
    }
}

fn way_back(reference: &str, old_head: &str, new_head: &str, keep: bool) -> String {
    let keep = if keep { " --keep" } else { "" };
    format!("stw oplog undo {reference} --to {old_head} --expect {new_head}{keep} --yes")
}

fn fail(code: &'static str, message: impl Into<String>) -> Fail {
    Fail {
        code,
        message: message.into(),
    }
}

/// Fold the plan's absorbed hunks. A checksum mismatch writes nothing. The branch ref moves only
/// after the in-memory tree check and the index check.
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
    let ready = ready(doc)?;
    let repo = gix::open(dir).map_err(|e| {
        fail(
            "git-failed",
            format!("the repository could not be opened: {e}"),
        )
    })?;
    let old_head = ObjectId::from_hex(ready.old_head.as_bytes())
        .map_err(|e| fail("git-failed", e.to_string()))?;
    let expected = expected_tree(&repo, old_head, &ready)?;
    let tip = rewritten_tip(&repo, &ready)?;
    let tip_tree = tree_of(&repo, tip)?;
    if tip_tree != expected {
        return Err(fail(
            "tree-mismatch",
            "the rewritten tree is not the old HEAD plus exactly the absorbed hunks; the branch was not moved",
        ));
    }
    index_matches(&repo, old_head, expected, &ready)?;
    let files = worktree_bytes(&repo, &ready)?;
    let keep = worktree_matches_index(&repo, &files);
    let backup_ref = snapshot(&repo, old_head)?;
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
    if let Err(path) = same_bytes(&repo, &files) {
        let _ = move_branch(&repo, &ready.reference, tip, old_head);
        return Err(fail(
            "worktree-changed",
            format!(
                "{path} changed while apply ran; the branch was moved back to {}",
                ready.old_head
            ),
        ));
    }
    Ok(Applied {
        old_head: ready.old_head,
        new_head: tip.to_string(),
        branch: ready.branch,
        reference: ready.reference,
        backup_ref,
        plan_sum: given_sum.to_string(),
        keep,
    })
}

struct Ready {
    old_head: String,
    branch: String,
    reference: String,
    /// Oldest rewritten commit first, including descendants.
    commits: Vec<String>,
    /// Target sha, then the index paths of its absorbed hunks.
    fixups: Vec<(String, Vec<String>)>,
}

fn ready(doc: &Value) -> Result<Ready, Fail> {
    let hunks = doc["hunks"].as_array().cloned().unwrap_or_default();
    let mut by_target: Vec<(String, Vec<String>)> = Vec::new();
    for hunk in &hunks {
        if hunk["assignment"] != "absorbed" {
            continue;
        }
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
    if by_target.is_empty() {
        return Err(fail(
            "nothing-to-apply",
            "the plan absorbs nothing; nothing was written",
        ));
    }
    let commits: Vec<String> = doc["rewrite"]["commits"]
        .as_array()
        .cloned()
        .unwrap_or_default()
        .into_iter()
        .filter_map(|c| c.as_str().map(str::to_string))
        .collect();
    by_target.sort_by_key(|(sha, _)| commits.iter().position(|c| c == sha).unwrap_or(usize::MAX));
    let reference = doc["rewrite"]["ref"].as_str().unwrap_or("").to_string();
    let branch = doc["branch"].as_str().unwrap_or("").to_string();
    let old_head = doc["head"].as_str().unwrap_or("").to_string();
    if !reference.starts_with("refs/heads/") || old_head.len() != 40 || commits.is_empty() {
        return Err(fail(
            "apply-failed",
            "the plan has no local branch; nothing was written",
        ));
    }
    Ok(Ready {
        old_head,
        branch,
        reference,
        commits,
        fixups: by_target,
    })
}

fn absorbed_paths(ready: &Ready) -> BTreeSet<&str> {
    ready
        .fixups
        .iter()
        .flat_map(|(_, paths)| paths.iter().map(String::as_str))
        .collect()
}

fn tree_of(repo: &gix::Repository, id: ObjectId) -> Result<ObjectId, Fail> {
    repo.find_commit(id)
        .and_then(|c| c.tree_id().map(|t| t.detach()))
        .map_err(|e| fail("git-failed", e.to_string()))
}

fn blob_at(repo: &gix::Repository, tree: ObjectId, path: &str) -> Result<Option<ObjectId>, Fail> {
    let tree = repo
        .find_tree(tree)
        .map_err(|e| fail("git-failed", e.to_string()))?;
    match tree
        .lookup_entry_by_path(path)
        .map_err(|e| fail("git-failed", e.to_string()))?
    {
        Some(entry) if !entry.mode().is_tree() => Ok(Some(entry.object_id())),
        _ => Ok(None),
    }
}

/// Old HEAD tree with each absorbed path replaced by the blob staged in the index.
fn expected_tree(repo: &gix::Repository, head: ObjectId, ready: &Ready) -> Result<ObjectId, Fail> {
    let paths: Vec<&str> = absorbed_paths(ready).into_iter().collect();
    upsert(repo, tree_of(repo, head)?, &paths)
}

fn upsert(repo: &gix::Repository, tree: ObjectId, paths: &[&str]) -> Result<ObjectId, Fail> {
    let index = repo
        .index_or_empty()
        .map_err(|e| fail("git-failed", e.to_string()))?;
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
            .upsert(*path, kind, id)
            .map_err(|e| fail("apply-failed", e.to_string()))?;
    }
    editor
        .write()
        .map(|id| id.detach())
        .map_err(|e| fail("git-failed", e.to_string()))
}

/// Replay the rewritten commits. A target commit also receives its absorbed index blobs.
/// Objects are written; the branch ref is not.
fn rewritten_tip(repo: &gix::Repository, ready: &Ready) -> Result<ObjectId, Fail> {
    let committer = replay::configured_committer(repo, gix::date::Time::now_utc())
        .map_err(|e| fail("apply-failed", format!("the rewrite has no committer: {e}")))?;
    let mut parent: Option<ObjectId> = None;
    let mut tip = None;
    for (i, sha) in ready.commits.iter().enumerate() {
        let id =
            ObjectId::from_hex(sha.as_bytes()).map_err(|e| fail("git-failed", e.to_string()))?;
        if i == 0 {
            let commit = repo
                .find_commit(id)
                .map_err(|e| fail("git-failed", e.to_string()))?;
            parent = commit.parent_ids().next().map(|p| p.detach());
        }
        let paths = ready
            .fixups
            .iter()
            .find(|(target, _)| target == sha)
            .map(|(_, paths)| paths.as_slice());
        let new_id = match parent {
            None => {
                let mut tree = tree_of(repo, id)?;
                if let Some(paths) = paths {
                    let borrowed: Vec<&str> = paths.iter().map(String::as_str).collect();
                    tree = upsert(repo, tree, &borrowed)?;
                }
                write_like(repo, id, tree, Vec::new(), &committer)?
            }
            Some(onto) => {
                let step = replay::replay_commit(repo, id, onto, &committer).map_err(|e| {
                    fail(
                        "apply-failed",
                        format!("replaying {sha} failed ({e}); the branch was not moved"),
                    )
                })?;
                if step.status == Status::Conflicted {
                    return Err(fail(
                        "apply-failed",
                        format!("replaying {sha} conflicted; the branch was not moved"),
                    ));
                }
                let replayed = step.new.unwrap_or(onto);
                if let Some(paths) = paths {
                    let borrowed: Vec<&str> = paths.iter().map(String::as_str).collect();
                    let tree = upsert(repo, tree_of(repo, replayed)?, &borrowed)?;
                    write_like(repo, id, tree, vec![onto], &committer)?
                } else {
                    replayed
                }
            }
        };
        parent = Some(new_id);
        tip = Some(new_id);
    }
    tip.ok_or_else(|| {
        fail(
            "nothing-to-apply",
            "the plan rewrites no commit; nothing was written",
        )
    })
}

fn write_like(
    repo: &gix::Repository,
    original: ObjectId,
    tree: ObjectId,
    parents: Vec<ObjectId>,
    committer: &gix::actor::Signature,
) -> Result<ObjectId, Fail> {
    let commit = repo
        .find_commit(original)
        .map_err(|e| fail("git-failed", e.to_string()))?;
    let decoded = commit
        .decode()
        .map_err(|e| fail("git-failed", e.to_string()))?;
    let author = decoded
        .author()
        .map_err(|e| fail("git-failed", e.to_string()))?
        .into();
    let written = gix::objs::Commit {
        tree,
        parents: parents.into(),
        author,
        committer: committer.clone(),
        encoding: decoded.encoding.map(ToOwned::to_owned),
        message: decoded.message.to_owned(),
        extra_headers: vec![],
    };
    repo.write_object(&written)
        .map(|id| id.detach())
        .map_err(|e| fail("git-failed", e.to_string()))
}

/// Absorbed paths match the expected tree (they will not stay staged). Every other staged path
/// still differs from that tree.
fn index_matches(
    repo: &gix::Repository,
    old_head: ObjectId,
    expected: ObjectId,
    ready: &Ready,
) -> Result<(), Fail> {
    let old_tree = tree_of(repo, old_head)?;
    let index = repo
        .index_or_empty()
        .map_err(|e| fail("git-failed", e.to_string()))?;
    let absorbed = absorbed_paths(ready);
    let mut staged_left = false;
    for entry in index.entries() {
        let path = entry.path(&index);
        let Ok(path) = std::str::from_utf8(path) else {
            continue;
        };
        let old = blob_at(repo, old_tree, path)?;
        let next = blob_at(repo, expected, path)?;
        let staged = old != Some(entry.id);
        if absorbed.contains(path) {
            if next != Some(entry.id) {
                return Err(fail(
                    "index-mismatch",
                    format!("{path} would not match the absorbed tree; the branch was not moved"),
                ));
            }
        } else if staged {
            staged_left = true;
            if next == Some(entry.id) {
                return Err(fail(
                    "index-mismatch",
                    format!(
                        "{path} is staged and was not absorbed, but the new tree would include it; the branch was not moved"
                    ),
                ));
            }
        } else if next != Some(entry.id) {
            return Err(fail(
                "index-mismatch",
                format!("{path} is not a staged change, but the new tree differs; the branch was not moved"),
            ));
        }
    }
    if absorbed.len() == staged_count(repo, old_tree, &index)? && staged_left {
        return Err(fail(
            "index-mismatch",
            "every staged path was absorbed, but one would stay staged; the branch was not moved",
        ));
    }
    Ok(())
}

fn staged_count(
    repo: &gix::Repository,
    old_tree: ObjectId,
    index: &gix::index::File,
) -> Result<usize, Fail> {
    let mut n = 0;
    for entry in index.entries() {
        let path = entry.path(index);
        let Ok(path) = std::str::from_utf8(path) else {
            continue;
        };
        if blob_at(repo, old_tree, path)? != Some(entry.id) {
            n += 1;
        }
    }
    Ok(n)
}

fn worktree_bytes(repo: &gix::Repository, ready: &Ready) -> Result<Vec<(String, Vec<u8>)>, Fail> {
    let work = repo.workdir().ok_or_else(|| {
        fail(
            "apply-failed",
            "the repository has no work tree; nothing was written",
        )
    })?;
    let mut out = Vec::new();
    for path in absorbed_paths(ready) {
        let bytes = std::fs::read(work.join(path)).unwrap_or_default();
        out.push((path.to_string(), bytes));
    }
    Ok(out)
}

fn same_bytes(repo: &gix::Repository, files: &[(String, Vec<u8>)]) -> Result<(), String> {
    let work = repo.workdir().ok_or_else(|| "no work tree".to_string())?;
    for (path, before) in files {
        let now = std::fs::read(work.join(path)).unwrap_or_default();
        if now != *before {
            return Err(path.clone());
        }
    }
    Ok(())
}

fn worktree_matches_index(repo: &gix::Repository, files: &[(String, Vec<u8>)]) -> bool {
    let Ok(index) = repo.index_or_empty() else {
        return false;
    };
    files.iter().all(|(path, bytes)| {
        let Some(entry) = index.entry_by_path(path.as_bytes().as_bstr()) else {
            return false;
        };
        let Ok(blob) = repo.find_object(entry.id) else {
            return false;
        };
        blob.data.as_slice() == bytes.as_slice()
    })
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
    store
        .transaction()
        .prepare(vec![edit], lock, lock)
        .map_err(|e| e.to_string())
        .and_then(|t| t.commit(committer).map(|_| ()).map_err(|e| e.to_string()))
}
