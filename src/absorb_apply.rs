//! `stw absorb --apply`: fold the staged changes the plan named.
//!
//! The plan is computed again and refused unless `--plan-sum` is its checksum. The rewritten
//! commits are built in memory. Before the branch ref moves, the tip tree must be the old HEAD
//! tree plus exactly the absorbed hunks. The index is locked and kept byte-for-byte unchanged,
//! so skipped changes stay staged. Apply does not write the worktree. The branch tip is copied to `refs/stw/backup/<run>/1` first. The way back is
//! `stw oplog undo`.
// SPDX-License-Identifier: EUPL-1.2 AND BSD-3-Clause
use std::collections::{BTreeMap, BTreeSet};
use std::path::{Path, PathBuf};
use std::time::Duration;

use gix::bstr::ByteSlice;
use gix::objs::{tree::EntryKind, Write as _};
use gix::refs::transaction::{Change, LogChange, PreviousValue, RefEdit, RefLog};
use gix::refs::Target;
use gix::ObjectId;
use serde_json::{json, Value};

use crate::absorb::{self, AbsorbedHunk, Fail, Options};
use crate::absorb_commute::{split_lines, Hunk};
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
    let repo = absorb::open_repository(dir, start)?;
    let index_path = repo.index_path();
    let _index_lock = gix::lock::Marker::acquire_to_hold_resource(
        &index_path,
        gix::lock::acquire::Fail::Immediately,
        None,
        Default::default(),
    )
    .map_err(|e| fail("index-lock", format!("could not lock the index: {e}")))?;
    let index_before = index_bytes(&index_path)?;
    let plan = absorb::plan_in(repo.clone(), dir, runner, opts)?;
    let doc = plan.to_json();
    if doc["plan_sum"].as_str() != Some(given_sum) {
        return Err(fail(
            "plan-sum-mismatch",
            "the plan checksum does not match this index; nothing was written",
        ));
    }
    if plan.absorbed.is_empty() {
        return Err(fail(
            "nothing-to-apply",
            "the plan absorbs nothing; nothing was written",
        ));
    }
    let reference = std::str::from_utf8(&plan.reference)
        .ok()
        .filter(|r| r.starts_with("refs/heads/"))
        .ok_or_else(|| {
            fail(
                "apply-failed",
                "the plan has no supported local branch; nothing was written",
            )
        })?;
    let branch = &reference["refs/heads/".len()..];
    let old_head = plan.head;
    let files = worktree_bytes(&repo, &plan.absorbed)?;
    let keep = worktree_matches_index(&repo, &files);
    let mut sandbox = replay::sandbox(&repo).map_err(|e| {
        fail(
            "apply-failed",
            format!("could not initialize the rewrite: {e}"),
        )
    })?;
    let expected = patch_tree(
        &sandbox,
        tree_of(&sandbox, old_head)?,
        plan.absorbed.iter().map(|h| (h.path.as_slice(), &h.staged)),
    )?;
    let tip = rewritten_tip(&sandbox, &plan.rewrite_commits, &plan.absorbed)?;
    if tree_of(&sandbox, tip)? != expected {
        return Err(fail(
            "tree-mismatch",
            "the rewritten tree is not the old HEAD plus exactly the absorbed hunks; the branch was not moved",
        ));
    }
    if let Some(mut objects) = sandbox.objects.take_object_memory() {
        for (_, (kind, data)) in objects.drain() {
            repo.objects.write_buf(kind, &data).map_err(|e| {
                fail(
                    "apply-failed",
                    format!("could not persist rewritten objects: {e}"),
                )
            })?;
        }
    }
    if index_bytes(&index_path)? != index_before {
        return Err(fail(
            "index-changed",
            "the index changed while apply ran; the branch was not moved",
        ));
    }
    if let Err(reason) = same_bytes(&repo, &files) {
        return Err(fail(
            "worktree-changed",
            format!("{reason}; the branch was not moved"),
        ));
    }
    let backup_ref = snapshot(&repo, old_head)?;
    if index_bytes(&index_path)? != index_before {
        return Err(fail(
            "index-changed",
            "the index changed while apply ran; the branch was not moved",
        ));
    }
    if let Err(e) = move_branch(&repo, reference, old_head, tip) {
        return Err(fail(
            "apply-failed",
            format!(
                "the branch was not moved ({e}); the backup {backup_ref} stays and holds {}",
                old_head
            ),
        ));
    }
    log_ref_update(
        &repo,
        &RefUpdate {
            refname: reference,
            old: Some(old_head),
            new: Some(tip),
        },
    );
    if let Err(reason) = same_bytes(&repo, &files) {
        let outcome = match move_branch(&repo, reference, tip, old_head) {
            Ok(()) => {
                log_ref_update(
                    &repo,
                    &RefUpdate {
                        refname: reference,
                        old: Some(tip),
                        new: Some(old_head),
                    },
                );
                format!("the branch was moved back to {}", old_head)
            }
            Err(error) => format!("could not move the branch back to {} ({error})", old_head),
        };
        return Err(fail(
            "worktree-changed",
            format!("{reason}; {outcome}; backup {backup_ref}"),
        ));
    }
    Ok(Applied {
        old_head: old_head.to_string(),
        new_head: tip.to_string(),
        branch: branch.to_owned(),
        reference: reference.to_owned(),
        backup_ref,
        plan_sum: given_sum.to_string(),
        keep,
    })
}

fn tree_of(repo: &gix::Repository, id: ObjectId) -> Result<ObjectId, Fail> {
    repo.find_commit(id)
        .and_then(|c| c.tree_id().map(|t| t.detach()))
        .map_err(|e| fail("git-failed", e.to_string()))
}

fn patch_tree<'a>(
    repo: &gix::Repository,
    tree: ObjectId,
    hunks: impl Iterator<Item = (&'a [u8], &'a Hunk)>,
) -> Result<ObjectId, Fail> {
    let mut paths = BTreeMap::<&[u8], Vec<&Hunk>>::new();
    for (path, hunk) in hunks {
        paths.entry(path).or_default().push(hunk);
    }
    if paths.is_empty() {
        return Ok(tree);
    }
    let tree_obj = repo
        .find_tree(tree)
        .map_err(|e| fail("git-failed", e.to_string()))?;
    let mut editor = repo
        .edit_tree(tree)
        .map_err(|e| fail("git-failed", e.to_string()))?;
    for (path, mut hunks) in paths {
        let display = path.as_bstr();
        let entry = tree_obj
            .lookup_entry(path.split(|b| *b == b'/'))
            .map_err(|e| fail("git-failed", e.to_string()))?
            .filter(|entry| {
                matches!(
                    entry.mode().kind(),
                    EntryKind::Blob | EntryKind::BlobExecutable
                )
            })
            .ok_or_else(|| {
                fail(
                    "apply-failed",
                    format!("the tree has no regular file at {display}"),
                )
            })?;
        let blob = repo
            .find_blob(entry.object_id())
            .map_err(|e| fail("git-failed", e.to_string()))?;
        let lines = split_lines(&blob.data);
        let mut patched = Vec::new();
        let mut cursor = 0;
        hunks.sort_by_key(|h| line_offset(h));
        for hunk in hunks {
            let offset = line_offset(hunk);
            let end = offset.saturating_add(hunk.rem.len());
            if offset < cursor
                || end > lines.len()
                || !lines[offset..end]
                    .iter()
                    .copied()
                    .eq(hunk.rem.iter().map(Vec::as_slice))
            {
                return Err(fail(
                    "apply-failed",
                    format!("an absorbed hunk does not match {display}"),
                ));
            }
            for line in &lines[cursor..offset] {
                patched.extend_from_slice(line);
            }
            for line in &hunk.add {
                patched.extend_from_slice(line);
            }
            cursor = end;
        }
        for line in &lines[cursor..] {
            patched.extend_from_slice(line);
        }
        let id = repo
            .write_blob(patched)
            .map_err(|e| fail("git-failed", e.to_string()))?;
        editor
            .upsert(display, entry.mode().kind(), id.detach())
            .map_err(|e| fail("apply-failed", e.to_string()))?;
    }
    editor
        .write()
        .map(|id| id.detach())
        .map_err(|e| fail("git-failed", e.to_string()))
}

fn line_offset(hunk: &Hunk) -> usize {
    let start = if hunk.rem.is_empty() {
        hunk.add_start
    } else {
        hunk.rem_start
    };
    usize::try_from(start.saturating_sub(1)).unwrap_or(usize::MAX)
}

fn rewritten_tip(
    repo: &gix::Repository,
    commits: &[ObjectId],
    hunks: &[AbsorbedHunk],
) -> Result<ObjectId, Fail> {
    let committer = replay::configured_committer(repo, gix::date::Time::now_utc())
        .map_err(|e| fail("apply-failed", format!("the rewrite has no committer: {e}")))?;
    let mut parent = None;
    for (i, &id) in commits.iter().enumerate() {
        let commit = repo
            .find_commit(id)
            .map_err(|e| fail("git-failed", e.to_string()))?;
        let original_parent = commit.parent_ids().next().map(|p| p.detach());
        if i == 0 {
            parent = original_parent;
        }
        let original_tree = tree_of(repo, id)?;
        let amended_tree = patch_tree(
            repo,
            original_tree,
            hunks
                .iter()
                .filter(|h| h.target == id)
                .map(|h| (h.path.as_slice(), &h.historical)),
        )?;
        let amended = if amended_tree == original_tree {
            id
        } else {
            write_like(
                repo,
                id,
                amended_tree,
                original_parent.into_iter().collect(),
                &committer,
            )?
        };
        let new_id = match parent {
            None => amended,
            Some(onto) if Some(onto) == original_parent => amended,
            Some(onto) => {
                let step = replay::replay_commit(repo, amended, onto, &committer).map_err(|e| {
                    fail(
                        "apply-failed",
                        format!("replaying {id} failed ({e}); the branch was not moved"),
                    )
                })?;
                if step.status == Status::Conflicted {
                    return Err(fail(
                        "apply-failed",
                        format!("replaying {id} conflicted; the branch was not moved"),
                    ));
                }
                step.new.unwrap_or(onto)
            }
        };
        parent = Some(new_id);
    }
    parent.ok_or_else(|| {
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

fn index_bytes(path: &Path) -> Result<Vec<u8>, Fail> {
    std::fs::read(path)
        .map_err(|e| fail("index-unreadable", format!("could not read the index: {e}")))
}

#[derive(PartialEq, Eq)]
enum WorktreeEntry {
    Missing,
    File(Vec<u8>),
    Symlink(PathBuf),
}

fn worktree_entry(path: &Path) -> std::io::Result<WorktreeEntry> {
    let metadata = match std::fs::symlink_metadata(path) {
        Ok(metadata) => metadata,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(WorktreeEntry::Missing),
        Err(e) => return Err(e),
    };
    if metadata.is_symlink() {
        std::fs::read_link(path).map(WorktreeEntry::Symlink)
    } else if metadata.is_file() {
        std::fs::read(path).map(WorktreeEntry::File)
    } else {
        Err(std::io::Error::other("expected a regular file or symlink"))
    }
}

type WorktreeSnapshot = Vec<(Vec<u8>, WorktreeEntry)>;

fn worktree_path(work: &Path, path: &[u8]) -> Result<PathBuf, String> {
    gix::path::from_bstr(path.as_bstr())
        .map(|path| work.join(path))
        .map_err(|e| format!("{} is not a valid worktree path: {e}", path.as_bstr()))
}

fn worktree_bytes(
    repo: &gix::Repository,
    hunks: &[AbsorbedHunk],
) -> Result<WorktreeSnapshot, Fail> {
    let work = repo
        .workdir()
        .ok_or_else(|| fail("apply-failed", "the repository has no work tree"))?;
    let paths: BTreeSet<_> = hunks.iter().map(|h| h.path.as_slice()).collect();
    paths
        .into_iter()
        .map(|path| {
            let full = worktree_path(work, path).map_err(|e| fail("worktree-unreadable", e))?;
            let state = worktree_entry(&full).map_err(|e| {
                fail(
                    "worktree-unreadable",
                    format!("could not read {}: {e}", path.as_bstr()),
                )
            })?;
            Ok((path.to_vec(), state))
        })
        .collect()
}

fn same_bytes(repo: &gix::Repository, files: &WorktreeSnapshot) -> Result<(), String> {
    let work = repo.workdir().ok_or_else(|| "no work tree".to_string())?;
    for (path, before) in files {
        let now = worktree_entry(&worktree_path(work, path)?)
            .map_err(|e| format!("could not verify {}: {e}", path.as_bstr()))?;
        if now != *before {
            return Err(format!("{} changed while apply ran", path.as_bstr()));
        }
    }
    Ok(())
}

fn worktree_matches_index(repo: &gix::Repository, files: &WorktreeSnapshot) -> bool {
    let Ok(index) = repo.index_or_empty() else {
        return false;
    };
    files.iter().all(|(path, state)| {
        let WorktreeEntry::File(bytes) = state else {
            return false;
        };
        let Some(entry) = index.entry_by_path(path.as_bstr()) else {
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
