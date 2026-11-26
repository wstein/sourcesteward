//! A saved, checked preview for committing explicitly selected files.
use std::collections::BTreeSet;
use std::fs;
use std::io::Read;
use std::path::{Path, PathBuf};

use gix::bstr::{BStr, ByteSlice};
use gix::index::entry::{Flags, Mode};
use gix::objs::tree::EntryKind;
use gix::refs::transaction::{Change, LogChange, PreviousValue, RefEdit, RefLog};
use gix::refs::Target;
use gix::ObjectId;
use serde::{Deserialize, Serialize};

use crate::error::{Error, Result};
use crate::sha256;

const SCHEMA_VERSION: u8 = 1;

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "kebab-case")]
pub enum Source {
    Index,
    Worktree,
}

#[derive(Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Selected {
    pub path: String,
    pub mode: String,
    pub index_blob: String,
    pub worktree_blob: String,
    pub selected_blob: String,
}

#[derive(Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Plan {
    pub schema_version: u8,
    pub head: String,
    pub r#ref: String,
    pub index_digest: String,
    pub source: Source,
    pub selected: Vec<Selected>,
    pub keep_staged: Vec<String>,
}

#[derive(Debug, Serialize)]
pub struct Applied {
    pub schema_version: u8,
    pub old_head: String,
    pub commit: String,
    pub r#ref: String,
    pub source: Source,
    pub selected: Vec<String>,
    pub keep_staged: Vec<String>,
}

struct Selection {
    kind: EntryKind,
    index_position: usize,
    id: ObjectId,
    worktree: Vec<u8>,
}

struct Preview {
    plan: Plan,
    index: gix::index::File,
    index_bytes: Vec<u8>,
    head: ObjectId,
    head_tree: ObjectId,
    selections: Vec<Selection>,
}

pub fn plan(root: &Path, source: Source, paths: &[PathBuf]) -> Result<Plan> {
    let (repo, root) = open(root)?;
    Ok(preview(&repo, &root, source, paths)?.plan)
}

pub fn apply(root: &Path, saved: &Plan, message: &str) -> Result<Applied> {
    if saved.schema_version != SCHEMA_VERSION {
        return Err(Error::usage("unsupported selective commit plan version"));
    }
    if message.trim().is_empty() {
        return Err(Error::usage("expected a nonempty commit message"));
    }

    let (repo, root) = open(root)?;
    let index_path = repo.index_path();
    let mut lock = gix::lock::File::acquire_to_update_resource(
        &index_path,
        gix::lock::acquire::Fail::Immediately,
        None,
        0,
    )
    .map_err(|e| Error::repository(format!("could not lock the index: {e}")))?;
    let paths: Vec<_> = saved
        .selected
        .iter()
        .map(|s| PathBuf::from(&s.path))
        .collect();
    let mut current = preview(&repo, &root, saved.source, &paths).map_err(stale)?;
    if current.plan != *saved {
        return Err(stale("the branch, index, or selected content changed"));
    }

    let mut changed = false;
    let objects = repo.clone().with_object_memory();
    let mut tree = objects.edit_tree(current.head_tree)?;
    for (selected, planned) in current.selections.iter().zip(&saved.selected) {
        let entry = &mut current.index.entries_mut()[selected.index_position];
        if entry.id != selected.id {
            objects.write_blob(&selected.worktree)?;
            entry.id = selected.id;
            entry.stat = Default::default();
            changed = true;
        }
        tree.upsert(planned.path.as_str(), selected.kind, selected.id)?;
    }
    let tree = tree.write()?.detach();
    if tree == current.head_tree {
        return Err(Error::repository(
            "selected content would make an empty commit",
        ));
    }
    let author = objects
        .author()
        .ok_or_else(|| Error::repository("Git author is missing"))??;
    let committer = objects
        .committer()
        .ok_or_else(|| Error::repository("Git committer is missing"))??;
    let commit = gix::objs::Commit {
        tree,
        parents: [current.head].into_iter().collect(),
        author: author.into(),
        committer: committer.into(),
        encoding: None,
        message: message.into(),
        extra_headers: Vec::new(),
    };
    let commit = objects.write_object(&commit)?.detach();
    if changed {
        // An index tree cache describes the previous entries. Git must rebuild it from the new entries.
        current.index.remove_tree();
        current.index.write_to(&mut lock, Default::default())?;
        lock.with_mut(|file| {
            file.set_permissions(fs::metadata(&index_path)?.permissions())?;
            file.sync_all()
        })?;
    }
    check_inputs(
        &repo,
        &index_path,
        &current.index_bytes,
        &saved.r#ref,
        current.head,
    )?;
    use gix::objs::Write as _;
    if let Some(objects) = objects.objects.reset_object_memory() {
        for (_, (kind, bytes)) in objects.iter() {
            repo.objects.write_buf(*kind, bytes)?;
        }
    }
    for (path, selected) in saved.selected.iter().zip(&current.selections) {
        let bytes = worktree_bytes(&root, Path::new(&path.path)).map_err(stale)?;
        if bytes != selected.worktree {
            return Err(stale("selected worktree content changed"));
        }
    }
    check_inputs(
        &repo,
        &index_path,
        &current.index_bytes,
        &saved.r#ref,
        current.head,
    )?;
    move_ref(
        &repo,
        &saved.r#ref,
        current.head,
        commit,
        "stw workspace commit-apply",
    )?;
    if changed {
        // Ref and index publication are separate. If the rename fails, only restore the ref
        // while it still contains exactly the commit we published.
        if let Err(index_error) = lock.commit() {
            let rollback = move_ref(
                &repo,
                &saved.r#ref,
                commit,
                current.head,
                "stw workspace commit-apply rollback",
            );
            let outcome = match rollback {
                Ok(()) => format!("branch restored to {}", current.head),
                Err(err) => format!(
                    "branch rollback failed ({err}); previous head {}, attempted commit {commit}",
                    current.head
                ),
            };
            return Err(Error::repository(format!(
                "could not publish the index ({index_error}); {outcome}"
            )));
        }
    }
    Ok(Applied {
        schema_version: SCHEMA_VERSION,
        old_head: saved.head.clone(),
        commit: commit.to_string(),
        r#ref: saved.r#ref.clone(),
        source: saved.source,
        selected: saved.selected.iter().map(|s| s.path.clone()).collect(),
        keep_staged: saved.keep_staged.clone(),
    })
}

fn stale(error: impl std::fmt::Display) -> Error {
    Error::repository(format!(
        "stale preview: {error}; preview again before applying"
    ))
}

fn open(path: &Path) -> Result<(gix::Repository, PathBuf)> {
    if let Some(error) = crate::absorb::environment_override() {
        return Err(Error::repository(error.message));
    }
    let root = path.canonicalize()?;
    let repo = crate::repo::open_for_commit(&root)?;
    if repo
        .workdir()
        .map(Path::canonicalize)
        .transpose()?
        .as_deref()
        != Some(root.as_path())
    {
        return Err(Error::usage("expected the repository worktree root"));
    }
    Ok((repo, root))
}

fn read_head(repo: &gix::Repository) -> Result<(String, ObjectId)> {
    let mut head = repo.head()?;
    let reference = head
        .referent_name()
        .ok_or_else(|| Error::repository("expected a named local branch"))?
        .as_bstr()
        .to_str()?;
    if !reference.starts_with("refs/heads/") {
        return Err(Error::repository("expected a named local branch"));
    }
    let reference = reference.to_owned();
    Ok((reference, head.peel_to_commit()?.id))
}

fn check_inputs(
    repo: &gix::Repository,
    index: &Path,
    index_before: &[u8],
    reference: &str,
    head: ObjectId,
) -> Result<()> {
    let (now_ref, now_head) = read_head(repo).map_err(stale)?;
    if now_ref != reference
        || now_head != head
        || fs::read(index).map_err(stale)? != index_before
        || repo.state().is_some()
    {
        return Err(stale("the branch, index, or Git operation changed"));
    }
    Ok(())
}

fn preview(
    repo: &gix::Repository,
    root: &Path,
    source: Source,
    paths: &[PathBuf],
) -> Result<Preview> {
    if paths.is_empty() {
        return Err(Error::usage("select at least one path"));
    }
    if repo.state().is_some() {
        return Err(Error::repository("a Git operation is in progress"));
    }
    let index_path = repo.index_path();
    let index_bytes = fs::read(&index_path)?;
    let (reference, head) = read_head(repo)?;
    let head_tree = repo.find_commit(head)?.tree_id()?.detach();
    let index = gix::index::File::at(&index_path, repo.object_hash(), false, Default::default())?;
    if index.is_sparse() || index.link().is_some() {
        return Err(Error::repository(
            "sparse and split indexes are outside selective commit support",
        ));
    }
    if index
        .entries()
        .iter()
        .any(|entry| entry.stage() != gix::index::entry::Stage::Unconflicted)
    {
        return Err(Error::repository("the index has unmerged entries"));
    }
    let baseline = repo.index_from_tree(&head_tree)?;
    let mut seen = BTreeSet::new();
    let mut selections = Vec::with_capacity(paths.len());
    let mut selected = Vec::with_capacity(paths.len());
    for path in paths {
        let path = normalized(path)?;
        if !seen.insert(path) {
            return Err(Error::usage(format!(
                "selected path {path:?} appears more than once"
            )));
        }
        let key = path.as_bytes().as_bstr();
        let position = index.entry_index_by_path(key).map_err(|_| {
            Error::repository(format!("expected a regular index entry for {path:?}"))
        })?;
        let entry = &index.entries()[position];
        if entry
            .flags
            .intersects(Flags::INTENT_TO_ADD | Flags::SKIP_WORKTREE | Flags::ASSUME_VALID)
        {
            return Err(Error::repository(format!(
                "{path:?} has a hidden or intent-to-add index entry"
            )));
        }
        let (kind, mode) = match entry.mode {
            Mode::FILE => (EntryKind::Blob, "100644"),
            Mode::FILE_EXECUTABLE => (EntryKind::BlobExecutable, "100755"),
            _ => {
                return Err(Error::repository(format!(
                    "expected a regular index entry for {path:?}"
                )))
            }
        };
        if baseline
            .prefixed_entries(format!("{path}/").as_bytes().as_bstr())
            .is_some()
            || Path::new(path)
                .ancestors()
                .skip(1)
                .filter(|p| !p.as_os_str().is_empty())
                .any(|p| {
                    baseline
                        .entry_by_path(p.as_os_str().as_encoded_bytes().as_bstr())
                        .is_some()
                })
        {
            return Err(Error::repository(format!(
                "{path:?} would replace other paths in HEAD"
            )));
        }
        let bytes = worktree_bytes(root, Path::new(path))?;
        let worktree_blob =
            gix::objs::compute_hash(repo.object_hash(), gix::objs::Kind::Blob, &bytes)?;
        let id = match source {
            Source::Index => entry.id,
            Source::Worktree => worktree_blob,
        };
        selected.push(Selected {
            path: path.to_owned(),
            mode: mode.to_owned(),
            index_blob: entry.id.to_string(),
            worktree_blob: worktree_blob.to_string(),
            selected_blob: id.to_string(),
        });
        selections.push(Selection {
            kind,
            index_position: position,
            id,
            worktree: bytes,
        });
    }
    let keep_staged = staged_paths(&baseline, &index, &seen)?;
    check_inputs(repo, &index_path, &index_bytes, &reference, head)?;
    Ok(Preview {
        plan: Plan {
            schema_version: SCHEMA_VERSION,
            head: head.to_string(),
            r#ref: reference,
            index_digest: sha256::hex(&index_bytes),
            source,
            selected,
            keep_staged,
        },
        index,
        index_bytes,
        head,
        head_tree,
        selections,
    })
}

fn staged_paths(
    baseline: &gix::index::File,
    index: &gix::index::File,
    selected: &BTreeSet<&str>,
) -> Result<Vec<String>> {
    let paths: BTreeSet<&BStr> = baseline
        .entries()
        .iter()
        .map(|e| e.path(baseline))
        .chain(index.entries().iter().map(|e| e.path(index)))
        .collect();
    paths
        .into_iter()
        .filter_map(|path| {
            let old = baseline.entry_by_path(path).map(|e| (e.id, e.mode));
            let new = index.entry_by_path(path).map(|e| (e.id, e.mode));
            if old == new || path.to_str().is_ok_and(|path| selected.contains(path)) {
                None
            } else {
                Some(path.to_str().map(str::to_owned).map_err(|_| {
                    Error::repository(
                        "a staged path is not UTF-8; it cannot be reported in this preview",
                    )
                }))
            }
        })
        .collect()
}

fn normalized(path: &Path) -> Result<&str> {
    let path = path
        .to_str()
        .ok_or_else(|| Error::usage("selected paths must be UTF-8"))?;
    if path.contains('\0')
        || path
            .split('/')
            .any(|part| part.is_empty() || matches!(part, "." | ".."))
        || Path::new(path)
            .components()
            .any(|part| !matches!(part, std::path::Component::Normal(_)))
    {
        return Err(Error::usage(format!(
            "expected a normalized repository-relative path: {path:?}"
        )));
    }
    Ok(path)
}

fn worktree_bytes(root: &Path, path: &Path) -> Result<Vec<u8>> {
    let mut file = open_worktree_file(root, path).map_err(|e| {
        Error::repository(format!(
            "could not open regular worktree file {path:?}: {e}"
        ))
    })?;
    if !file.metadata()?.is_file() {
        return Err(Error::repository(format!(
            "expected a regular worktree file: {path:?}"
        )));
    }
    let mut bytes = Vec::new();
    file.read_to_end(&mut bytes)?;
    Ok(bytes)
}

#[cfg(unix)]
fn open_worktree_file(root: &Path, path: &Path) -> std::io::Result<fs::File> {
    use rustix::fs::{openat, Mode, OFlags};
    let mut file = fs::File::open(root)?;
    let mut components = path.components().peekable();
    while let Some(component) = components.next() {
        let mut flags = OFlags::RDONLY | OFlags::CLOEXEC | OFlags::NOFOLLOW | OFlags::NONBLOCK;
        if components.peek().is_some() {
            flags |= OFlags::DIRECTORY;
        }
        // The parent descriptor stays pinned if another process moves the directory.
        file = openat(&file, component.as_os_str(), flags, Mode::empty())?.into();
    }
    Ok(file)
}

#[cfg(not(unix))]
fn open_worktree_file(root: &Path, path: &Path) -> std::io::Result<fs::File> {
    let full = root.join(path);
    if full.canonicalize()? != full || !full.symlink_metadata()?.is_file() {
        return Err(std::io::Error::other(
            "expected a regular file without symlinks",
        ));
    }
    fs::File::open(full)
}

fn move_ref(
    repo: &gix::Repository,
    name: &str,
    from: ObjectId,
    to: ObjectId,
    message: &str,
) -> Result<()> {
    let mut store = repo.refs.clone();
    store.write_reflog = gix::refs::store::WriteReflog::Always;
    let edit = RefEdit {
        name: gix::refs::FullName::try_from(name)?,
        deref: false,
        change: Change::Update {
            log: LogChange {
                mode: RefLog::AndReference,
                force_create_reflog: true,
                message: message.into(),
            },
            expected: PreviousValue::MustExistAndMatch(Target::Object(from)),
            new: Target::Object(to),
        },
    };
    let lock = gix::lock::acquire::Fail::Immediately;
    store
        .transaction()
        .prepare([edit], lock, lock)?
        .commit(repo.committer().transpose()?)?;
    Ok(())
}
