//! Fast-forward of the checked-out branch, including index and working tree, without `git`.
//!
//! gix has no fast-forward or checkout-to-commit API. This module checks first (dirty paths, overlap,
//! untracked files in the way) and writes only after every check passed.
//!
//! Order of writes: checks, then the index lock (`.git/index.lock`, taken and held until the index is
//! committed), then the branch ref (HEAD and branch reflog), then working tree files, then the index. The lock
//! comes first so that a held lock (another git process, a stale lock file) is refused before anything was
//! written. The ref moves before the files because it is the write that can fail for reasons no check sees
//! (permissions, locks, reflog); when it fails, no file was touched.
//!
//! What this does not guarantee: there is no transaction across ref, files and index. If the process is
//! killed (or the machine loses power) after the ref moved and before the index is written, `HEAD` points to
//! the new commit while files and index are still partly or fully old. `git status` then shows the update as
//! local changes. To finish the update: `git reset --hard HEAD` (tested; it also discards unrelated local
//! changes). Without discarding anything else: `git checkout HEAD -- <paths>` with the paths of
//! `git diff --name-only --diff-filter=d <old commit> HEAD` (paths that still exist in HEAD; a path deleted in
//! HEAD makes a plain `git checkout HEAD -- <path>` fail), then `git rm --cached` and remove the files of
//! `--diff-filter=D`. To go back instead: `git reset --hard <old commit>` (same caveat); the old commit is the
//! second-to-last entry of `git reflog show <branch>`.
//!
//! When writing files or the index returns an error, the ref is put back to the old commit (reflog entry
//! "stw: undo fast-forward ..."). Files already written stay, and so does everything the checkout wrote before
//! it failed; the index is written last, so a failure before that leaves it unchanged, and a failed index write
//! leaves the old index too (it is replaced by renaming the lock file). A lock that is held before the call
//! starts is not such a failure: it is refused before the first write. If putting the ref back fails as well, the error names both
//! failures and the ref stays at the new commit with files and index partly old: recover as after a kill.
use std::collections::BTreeSet;
use std::sync::atomic::AtomicBool;

use gix::bstr::{BStr, BString, ByteSlice};
use gix::ObjectId;

use crate::repo;
use crate::status;

/// Why a fast-forward failed. Nothing was changed when a check refused it (`Overlap`, `Conflicts`,
/// `UntrackedInTheWay`, `Unsupported`) or when `Other` reports a held index lock or a failed ref update. `Other`
/// can also report a failure while writing files or the index; then the ref is put back but written files stay.
#[derive(Debug)]
pub enum FfError {
    /// Tracked local changes (index or working tree) on paths the fast-forward would touch.
    Overlap(Vec<String>),
    /// Unmerged paths in the index.
    Conflicts,
    /// Untracked files or non-directories in the way of files the fast-forward adds.
    UntrackedInTheWay(Vec<String>),
    Unsupported(String),
    Other(String),
}

impl std::fmt::Display for FfError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let list = |v: &Vec<String>| {
            let mut s = v.iter().take(5).cloned().collect::<Vec<_>>().join(", ");
            if v.len() > 5 {
                s += &format!(", and {} more", v.len() - 5);
            }
            s
        };
        match self {
            FfError::Overlap(v) => write!(f, "local changes would be overwritten: {}", list(v)),
            FfError::Conflicts => write!(f, "unmerged paths in the index"),
            FfError::UntrackedInTheWay(v) => {
                write!(f, "untracked files would be overwritten: {}", list(v))
            }
            FfError::Unsupported(s) => write!(f, "unsupported change: {s}"),
            FfError::Other(s) => write!(f, "{s}"),
        }
    }
}

impl<E: std::error::Error + Send + Sync + 'static> From<E> for FfError {
    fn from(e: E) -> Self {
        FfError::Other(e.to_string())
    }
}

struct Change {
    path: BString,
    old: Option<(gix::objs::tree::EntryMode, ObjectId)>,
    new: Option<(gix::objs::tree::EntryMode, ObjectId)>,
}

/// Changed paths (present in the new tree) for which the attributes of the new tree set `filter`.
fn filter_attribute_paths(
    repo: &gix::Repository,
    new_tree: &gix::Tree<'_>,
    changes: &[Change],
) -> Result<Vec<String>, FfError> {
    use gix::attrs::StateRef;
    let index = repo.index_from_tree(&new_tree.id)?;
    let mut stack = repo
        .attributes_only(
            &index,
            gix::worktree::stack::state::attributes::Source::IdMapping,
        )
        .map_err(|e| FfError::Other(format!("attributes: {e}")))?;
    let mut out = stack.selected_attribute_matches(["filter"]);
    let mut found = vec![];
    for c in changes {
        let Some((mode, _)) = &c.new else { continue };
        if mode.is_commit() {
            continue;
        }
        let platform = stack
            .at_entry(c.path.as_bstr(), Some(gix::index::entry::Mode::FILE))
            .map_err(|e| FfError::Other(format!("attributes: {e}")))?;
        platform.matching_attributes(&mut out);
        let filtered = out
            .iter_selected()
            .any(|m| matches!(m.assignment.state, StateRef::Set | StateRef::Value(_)));
        if filtered {
            found.push(c.path.to_str_lossy().into_owned());
        }
    }
    Ok(found)
}

fn rel_path(b: &BStr) -> Result<std::path::PathBuf, FfError> {
    Ok(gix::path::from_bstr(b)?.into_owned())
}

fn tree_changes(
    repo: &gix::Repository,
    old: &gix::Tree<'_>,
    new: &gix::Tree<'_>,
) -> Result<Vec<Change>, FfError> {
    use gix::diff::tree::recorder::Change as C;
    let mut rec = gix::diff::tree::Recorder::default();
    let mut state = gix::diff::tree::State::default();
    gix::diff::tree(
        gix::objs::TreeRefIter::from_bytes(&old.data, old.id.kind()),
        gix::objs::TreeRefIter::from_bytes(&new.data, new.id.kind()),
        &mut state,
        &repo.objects,
        &mut rec,
    )?;
    let mut out = vec![];
    for c in rec.records {
        match c {
            C::Addition {
                entry_mode,
                oid,
                path,
                ..
            } if !entry_mode.is_tree() => out.push(Change {
                path,
                old: None,
                new: Some((entry_mode, oid)),
            }),
            C::Deletion {
                entry_mode,
                oid,
                path,
                ..
            } if !entry_mode.is_tree() => out.push(Change {
                path,
                old: Some((entry_mode, oid)),
                new: None,
            }),
            C::Modification {
                previous_entry_mode,
                previous_oid,
                entry_mode,
                oid,
                path,
            } => {
                if previous_entry_mode.is_tree() != entry_mode.is_tree() {
                    return Err(FfError::Unsupported(format!(
                        "{} changes between file and directory",
                        path.to_str_lossy()
                    )));
                }
                if !entry_mode.is_tree() {
                    out.push(Change {
                        path,
                        old: Some((previous_entry_mode, previous_oid)),
                        new: Some((entry_mode, oid)),
                    })
                }
            }
            _ => {}
        }
    }
    Ok(out)
}

/// Move HEAD (and the branch it points to) from `from` to `to`, writing both reflogs, expecting `from`.
fn move_head(
    repo: &gix::Repository,
    from: ObjectId,
    to: ObjectId,
    message: String,
) -> Result<(), FfError> {
    use gix::refs::transaction::{Change as RC, LogChange, PreviousValue, RefEdit, RefLog};
    use gix::refs::Target;
    // the ref that moves: the branch HEAD points to, or HEAD itself when detached
    let moved = match repo.find_reference("HEAD").map(|r| r.target().into_owned()) {
        Ok(Target::Symbolic(n)) => n.as_bstr().to_string(),
        _ => "HEAD".to_string(),
    };
    repo.edit_reference(RefEdit {
        change: RC::Update {
            log: LogChange {
                mode: RefLog::AndReference,
                force_create_reflog: false,
                message: message.into(),
            },
            expected: PreviousValue::MustExistAndMatch(Target::Object(from)),
            new: Target::Object(to),
        },
        name: "HEAD"
            .try_into()
            .map_err(|e: gix::refs::name::Error| FfError::Other(e.to_string()))?,
        deref: true,
    })
    .map_err(|e| FfError::Other(format!("ref update: {e}")))?;
    crate::oplog::write::log_ref_update(
        repo,
        &crate::oplog::write::RefUpdate {
            refname: &moved,
            old: Some(from),
            new: Some(to),
        },
    );
    Ok(())
}

/// Put the ref back from `target` to `head` after writing files or the index failed with `cause`, and return the
/// error to report: `cause` alone when the ref is back, or `cause` plus the reason when it could not be put back
/// (then the ref stays at `target`). Exposed for tests only.
#[doc(hidden)]
pub fn put_ref_back(
    repo: &gix::Repository,
    head: ObjectId,
    target: ObjectId,
    reflog_label: &str,
    cause: FfError,
) -> FfError {
    match move_head(
        repo,
        target,
        head,
        format!("stw: undo fast-forward to {reflog_label}"),
    ) {
        Ok(()) => cause,
        Err(undo) => FfError::Other(format!(
            "{cause}; and the ref could not be put back: {undo}"
        )),
    }
}

/// Fast-forward the checked-out branch of `repo` to `target` (a descendant of HEAD): after all checks, move the
/// branch ref first (writing both reflogs), then update the working tree files for the changed paths, then the
/// index. A refusal by a check, or a failure of the ref update, leaves everything as it was. A failure while
/// writing files or the index puts the ref back (see the module documentation for what stays).
pub fn fast_forward(
    repo: &gix::Repository,
    target: ObjectId,
    reflog_label: &str,
) -> Result<(ObjectId, ObjectId), FfError> {
    fast_forward_with(repo, target, reflog_label, &mut |_, _| Ok(()))
}

/// `fast_forward` with a step before the first write: `before(old, new)` runs after every check passed and the index
/// lock is held, and before the branch ref moves. `stw sync` records the change in its journal there (recovery ref, then
/// record), so that no ref moves without them. An `Err` from `before` stops the fast-forward with
/// `FfError::Other(message)`; nothing was written by then (the lock is released).
pub fn fast_forward_with(
    repo: &gix::Repository,
    target: ObjectId,
    reflog_label: &str,
    before: &mut dyn FnMut(ObjectId, ObjectId) -> Result<(), String>,
) -> Result<(ObjectId, ObjectId), FfError> {
    let workdir = repo
        .workdir()
        .ok_or_else(|| FfError::Other("bare repository".into()))?
        .to_path_buf();
    let head = repo.head_id()?.detach();
    if !repo::is_ancestor(repo, head, target).map_err(|e| FfError::Other(e.to_string()))? {
        return Err(FfError::Other("target is not a descendant of HEAD".into()));
    }
    let old_tree = repo.find_commit(head)?.tree()?;
    let new_tree = repo.find_commit(target)?.tree()?;
    let changes = tree_changes(repo, &old_tree, &new_tree)?;

    // 1. dirty check: tracked changes in the working tree and in the index
    let dirty = status::changes(repo, false).map_err(|e| FfError::Other(e.to_string()))?;
    if dirty.conflicts > 0 {
        return Err(FfError::Conflicts);
    }
    let touched: BTreeSet<&BStr> = changes.iter().map(|c| c.path.as_bstr()).collect();
    // 2. overlap: a changed path that is also locally changed (a deleted file that is gone already counts)
    let overlap: Vec<String> = dirty
        .paths
        .iter()
        .filter(|p| touched.contains(p.as_bstr()))
        .map(|p| p.to_str_lossy().into_owned())
        .collect();
    if !overlap.is_empty() {
        return Err(FfError::Overlap(overlap));
    }
    // 3. untracked files (or a file where a directory is needed) in the way of added files
    let index = repo.index_or_empty()?;
    let mut in_the_way = vec![];
    for c in changes.iter().filter(|c| c.old.is_none()) {
        let rel = rel_path(c.path.as_bstr())?;
        let full = workdir.join(&rel);
        if full.symlink_metadata().is_ok() && index.entry_by_path(c.path.as_bstr()).is_none() {
            in_the_way.push(c.path.to_str_lossy().into_owned());
            continue;
        }
        let mut anc = rel.parent();
        while let Some(a) = anc {
            if a.as_os_str().is_empty() {
                break;
            }
            if let Ok(m) = workdir.join(a).symlink_metadata() {
                if !m.is_dir() {
                    // a tracked file that upstream replaces by a directory is not "untracked in the way"
                    if index
                        .entry_by_path(BString::from(a.to_string_lossy().as_ref()).as_ref())
                        .is_some()
                    {
                        return Err(FfError::Unsupported(format!(
                            "{} changes between file and directory",
                            a.display()
                        )));
                    }
                    in_the_way.push(c.path.to_str_lossy().into_owned());
                    break;
                }
            }
            anc = a.parent();
        }
    }
    if !in_the_way.is_empty() {
        return Err(FfError::UntrackedInTheWay(in_the_way));
    }

    // 3b. a changed path with a `filter=` attribute: stw runs no filter driver (repo::open drops them), so the
    //     checkout would write the raw blob (git-lfs: a pointer file instead of the content). Refuse first.
    let filtered = filter_attribute_paths(repo, &new_tree, &changes)?;
    if !filtered.is_empty() {
        return Err(FfError::Unsupported(format!(
            "{} has a filter attribute (git-lfs or similar); stw runs no filter driver and would write the \
             unconverted content",
            filtered.join(", ")
        )));
    }

    // 4. the index lock, held until the index is committed: a held lock is refused before any write
    let lock = gix::lock::File::acquire_to_update_resource(
        repo.index_path(),
        gix::lock::acquire::Fail::Immediately,
        None,
        0,
    )
    .map_err(|e| FfError::Other(format!("index lock {}: {e}", repo.index_path().display())))?;
    // 4b. the caller's record of the change, before the first write
    before(head, target).map_err(FfError::Other)?;
    // 5. the ref next, expecting the old value (HEAD log and branch log). It is the write that can fail for
    //    reasons no earlier check sees (permissions, locks, reflog); when it does, nothing else was touched.
    move_head(
        repo,
        head,
        target,
        format!("merge {reflog_label}: Fast-forward"),
    )?;
    // 6. files, then the index. If this fails, the ref is put back (a partial checkout can still remain).
    let write_files = move || -> Result<(), FfError> {
        let mut real = gix::index::File::at(
            repo.index_path(),
            repo.object_hash(),
            true,
            Default::default(),
        )
        .or_else(|_| {
            Ok::<_, FfError>(gix::index::File::from_state(
                gix::index::State::new(repo.object_hash()),
                repo.index_path(),
            ))
        })
        .map_err(|e: FfError| e)?;
        let mut partial = repo.index_from_tree(&new_tree.id)?;
        let wanted: BTreeSet<BString> = changes
            .iter()
            .filter(|c| c.new.as_ref().map(|(m, _)| !m.is_commit()).unwrap_or(false))
            .map(|c| c.path.clone())
            .collect();
        // Attributes (eol, filters) are looked up in the index that is checked out. The whole target index is kept
        // for that, and every entry that is not part of the update is marked skip-worktree, which the checkout
        // does not write. Removing those entries instead would hide an unchanged `.gitattributes`.
        let mut any = false;
        for (e, p) in partial.entries_mut_with_paths() {
            if wanted.contains(p) {
                any = true;
            } else {
                e.flags.insert(gix::index::entry::Flags::SKIP_WORKTREE);
            }
        }
        if any {
            let mut opts = repo
                .checkout_options(gix::worktree::stack::state::attributes::Source::IdMapping)
                .map_err(|e| FfError::Other(e.to_string()))?;
            opts.destination_is_initially_empty = false;
            opts.overwrite_existing = true;
            let objects = repo
                .objects
                .clone()
                .into_arc()
                .map_err(|e| FfError::Other(e.to_string()))?;
            let interrupt = AtomicBool::new(false);
            gix::worktree::state::checkout(
                &mut partial,
                &workdir,
                objects,
                &gix::progress::Discard,
                &gix::progress::Discard,
                &interrupt,
                opts,
            )
            .map_err(|e| FfError::Other(format!("checkout: {e}")))?;
        }
        // deletions in the working tree
        for c in changes.iter().filter(|c| c.new.is_none()) {
            let full = workdir.join(rel_path(c.path.as_bstr())?);
            match std::fs::remove_file(&full) {
                Ok(()) => {}
                Err(e) if e.kind() == std::io::ErrorKind::NotFound => {}
                Err(e) => {
                    return Err(FfError::Other(format!(
                        "remove {}: {e}",
                        c.path.to_str_lossy()
                    )))
                }
            }
            let mut dir = full.parent().map(|p| p.to_path_buf());
            while let Some(d) = dir {
                if d == workdir || std::fs::remove_dir(&d).is_err() {
                    break;
                }
                dir = d.parent().map(|p| p.to_path_buf());
            }
        }
        // a new submodule leaves an empty directory, like git does
        for c in changes
            .iter()
            .filter(|c| c.new.as_ref().is_some_and(|(m, _)| m.is_commit()))
        {
            let dir = workdir.join(rel_path(c.path.as_bstr())?);
            std::fs::create_dir_all(&dir)
                .map_err(|e| FfError::Other(format!("create {}: {e}", dir.display())))?;
        }
        // index: replace exactly the changed entries
        let changed_paths: BTreeSet<BString> = changes.iter().map(|c| c.path.clone()).collect();
        real.remove_entries(|_, p, _| changed_paths.contains(p));
        for c in &changes {
            let Some((mode, id)) = c.new else { continue };
            if mode.is_commit() {
                real.dangerously_push_entry(
                    Default::default(),
                    id,
                    gix::index::entry::Flags::empty(),
                    gix::index::entry::Mode::COMMIT,
                    c.path.as_bstr(),
                );
            } else if let Some(e) = partial.entry_by_path(c.path.as_bstr()) {
                let (stat, eid, flags, m) = (e.stat, e.id, e.flags, e.mode);
                real.dangerously_push_entry(stat, eid, flags, m, c.path.as_bstr());
            }
        }
        real.sort_entries();
        real.remove_tree(); // gix does not update the tree cache on write (gix issue 2421)
                            // commit last: write into the lock taken above, then rename it over the index
        let mut out = std::io::BufWriter::with_capacity(64 * 1024, lock);
        real.write_to(&mut out, Default::default())
            .map_err(|e| FfError::Other(format!("index: {e}")))?;
        out.into_inner()
            .map_err(|e| FfError::Other(format!("index: {e}")))?
            .commit()
            .map_err(|e| FfError::Other(format!("index: {e}")))?;

        Ok(())
    };
    if let Err(e) = write_files() {
        return Err(put_ref_back(repo, head, target, reflog_label, e));
    }
    Ok((head, target))
}
