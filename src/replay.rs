//! In-memory replay engine (M1): replays the commits of a range onto a new parent the way `git rebase --onto`
//! does, with gix tree merges. Library only; no command uses it yet.
//!
//! Nothing outside the process is changed: the engine works on a clone of the repository handle with the object
//! memory enabled, so every new tree, blob and commit lives in memory (`Replay::repo` reads them). No ref, index,
//! worktree, reflog or object file is written. Whoever keeps a result has to write the objects and the refs itself.
//!
//! Attribution (decision 6 of the migration plan): a replayed commit keeps author, author date, message and
//! encoding of the original; the committer is the identity the caller passes (`configured_committer` reads it from
//! the Git config, read-only). A commit whose tree and parent do not change is returned as it is.
//!
//! Differences from Git, named: there is no skip by patch ID (`git rebase` drops a commit whose patch is in
//! `onto`); a commit is dropped only when its merge leaves the new parent's tree unchanged (`Empty`), as in the Go
//! restack. A patch that `onto` made and later reverted therefore comes through, as with `git rebase --onto`. Merge drivers of the configuration are never started, a driver named by an attribute is unknown to the merge; rename
//! detection and the diff algorithm are pinned (`merge.renames=true`, `merge.renameLimit=1000`, myers). Attributes
//! come from the index of the repository, as for `git merge-tree`. The replay stops at the first conflict.
use std::fmt;

use gix::actor::Signature;
use gix::bstr::{BString, ByteSlice};
use gix::merge::plumbing::tree::TreatAsUnresolved;
use gix::merge::plumbing::tree::{Resolution, ResolutionFailure};
use gix::ObjectId;

/// Why a replay is refused or failed. Conflicts are results, not errors.
#[derive(Debug)]
pub enum ReplayError {
    /// A commit of the range has no parent; there is nothing to replay it from.
    RootCommit(ObjectId),
    /// A commit of the range has more than one parent; merges are not replayed.
    MergeCommit(ObjectId),
    /// No `committer.*` or `user.*` name and email are configured.
    NoIdentity,
    /// Reading or writing objects, or the merge itself, failed.
    Git(String),
}

impl fmt::Display for ReplayError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            ReplayError::RootCommit(c) => write!(f, "commit {c} is a root commit and cannot be replayed"),
            ReplayError::MergeCommit(c) => write!(f, "commit {c} is a merge commit and cannot be replayed"),
            ReplayError::NoIdentity => f.write_str(
                "no committer identity is configured: set user.name and user.email (stw never sets them)",
            ),
            ReplayError::Git(m) => f.write_str(m),
        }
    }
}

impl std::error::Error for ReplayError {}

fn git_err(e: impl fmt::Display) -> ReplayError {
    ReplayError::Git(e.to_string())
}

/// What happened to one commit. The names follow `git rebase`.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Status {
    /// Merged without conflict; a new commit exists.
    Clean,
    /// Tree and parent equal the original: the original commit is the result.
    Unchanged,
    /// The merge has conflicts; no commit exists and the replay stops here.
    Conflicted,
    /// The replay changed nothing relative to the new parent: dropped, no commit.
    Empty,
    /// Empty before the replay (tree equals the parent's tree): kept as an empty commit, as `git rebase` does.
    EmptyStart,
}

/// A conflicted path with a coarse kind (`content`, `add/add`, `modify/delete`, `rename/rename`, `rename/delete`,
/// `submodule`, `file/directory`, `directory-rename`, `unknown`).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Conflict {
    pub path: BString,
    pub kind: &'static str,
}

/// Result for one original commit.
#[derive(Debug, Clone)]
pub struct Step {
    pub old: ObjectId,
    /// The replayed commit; `None` for `Conflicted` and `Empty`.
    pub new: Option<ObjectId>,
    pub status: Status,
    /// Set for `Conflicted` only.
    pub conflicts: Vec<Conflict>,
}

/// Result of `replay_range`.
pub struct Replay {
    /// The repository handle that holds the new objects in memory. Reading a new id through another handle fails.
    pub repo: gix::Repository,
    /// One step per original commit, oldest first; ends at the first conflict.
    pub steps: Vec<Step>,
    /// The last replayed commit, or `onto` when nothing was replayed. Meaningless when `conflicted()`.
    pub tip: ObjectId,
}

impl Replay {
    pub fn conflicted(&self) -> bool {
        self.steps
            .last()
            .is_some_and(|s| s.status == Status::Conflicted)
    }
}

/// The committer from the Git config: `committer.name`/`committer.email`, then `user.name`/`user.email`. The
/// in-memory fallback of `repo::open`, `GIT_COMMITTER_*` and every other override do not count: only the
/// configuration files do. Read-only.
pub fn configured_committer(
    repo: &gix::Repository,
    now: gix::date::Time,
) -> Result<Signature, ReplayError> {
    use gix::config::Source;
    let snapshot = repo.config_snapshot();
    let cfg = snapshot.plumbing();
    // Only the Git configuration files count; GIT_COMMITTER_* and other overrides come as another source.
    let from_files = |m: &gix::config::file::Metadata| {
        matches!(
            m.source,
            Source::GitInstallation
                | Source::System
                | Source::Git
                | Source::User
                | Source::Local
                | Source::Worktree
        )
    };
    let get = |a: &str, b: &str| {
        let v = cfg
            .string_filter(a, from_files)
            .or_else(|| cfg.string_filter(b, from_files));
        v.filter(|v| !v.trim().is_empty())
    };
    match (
        get("committer.name", "user.name"),
        get("committer.email", "user.email"),
    ) {
        (Some(name), Some(email)) => Ok(Signature {
            name,
            email,
            time: now,
        }),
        _ => Err(ReplayError::NoIdentity),
    }
}

/// A clone of `repo` whose object writes stay in memory and whose merge configuration is pinned.
fn sandbox(repo: &gix::Repository) -> Result<gix::Repository, ReplayError> {
    let mut mem = repo.clone().with_object_memory();
    {
        let mut snap = mem.config_snapshot_mut();
        let file: &mut gix::config::File = &mut snap;
        // Merge drivers run programs named in the (repository) configuration: forget them all.
        let drivers: Vec<_> = file
            .sections_by_name("merge")
            .into_iter()
            .flatten()
            .filter_map(|s| s.header().subsection_name().map(ToOwned::to_owned))
            .collect();
        for sub in drivers {
            while file.remove_section("merge", Some(sub.as_ref())).is_some() {}
        }
        for (key, value) in [
            ("renormalize", "false"),
            ("renames", "true"),
            ("renameLimit", "1000"),
            ("conflictStyle", "merge"),
        ] {
            file.set_raw_value_by("merge", None, key, value)
                .map_err(git_err)?;
        }
        file.set_raw_value_by("diff", None, "algorithm", "myers")
            .map_err(git_err)?;
        snap.commit().map_err(git_err)?;
    }
    Ok(mem)
}

/// Replays `old_base..tip` onto `onto`, oldest commit first. Refuses root and merge commits before anything is
/// replayed.
pub fn replay_range(
    repo: &gix::Repository,
    onto: ObjectId,
    old_base: ObjectId,
    tip: ObjectId,
    committer: &Signature,
) -> Result<Replay, ReplayError> {
    let mem = sandbox(repo)?;
    let chain = chain(&mem, old_base, tip)?;
    let mut steps = Vec::with_capacity(chain.len());
    let mut new_tip = onto;
    for c in chain {
        let step = replay_commit(&mem, c, new_tip, committer)?;
        let stop = step.status == Status::Conflicted;
        if let Some(n) = step.new {
            new_tip = n;
        }
        steps.push(step);
        if stop {
            break;
        }
    }
    Ok(Replay {
        repo: mem,
        steps,
        tip: new_tip,
    })
}

/// The commits of `old_base..tip`, oldest first, all with exactly one parent.
fn chain(
    repo: &gix::Repository,
    old_base: ObjectId,
    tip: ObjectId,
) -> Result<Vec<ObjectId>, ReplayError> {
    let mut parent_of = std::collections::HashMap::new();
    for info in repo
        .rev_walk([tip])
        .with_hidden([old_base])
        .all()
        .map_err(git_err)?
    {
        let info = info.map_err(git_err)?;
        let ids: Vec<ObjectId> = info.parent_ids().map(|p| p.detach()).collect();
        match ids.len() {
            0 => return Err(ReplayError::RootCommit(info.id)),
            1 => {
                parent_of.insert(info.id, ids[0]);
            }
            _ => return Err(ReplayError::MergeCommit(info.id)),
        }
    }
    let mut out = Vec::new();
    let mut at = tip;
    while let Some(p) = parent_of.get(&at) {
        out.push(at);
        at = *p;
    }
    out.reverse();
    Ok(out)
}

/// Replays one commit onto `new_parent`. `repo` should be the handle of `replay_range` (object memory enabled);
/// with a plain handle the new objects are written to the repository.
pub fn replay_commit(
    repo: &gix::Repository,
    commit: ObjectId,
    new_parent: ObjectId,
    committer: &Signature,
) -> Result<Step, ReplayError> {
    let c = repo.find_commit(commit).map_err(git_err)?;
    let parents: Vec<ObjectId> = c.parent_ids().map(|p| p.detach()).collect();
    let old_parent = match parents.as_slice() {
        [] => return Err(ReplayError::RootCommit(commit)),
        [p] => *p,
        _ => return Err(ReplayError::MergeCommit(commit)),
    };
    let tree_of = |id: ObjectId| -> Result<ObjectId, ReplayError> {
        Ok(repo
            .find_commit(id)
            .map_err(git_err)?
            .tree_id()
            .map_err(git_err)?
            .detach())
    };
    let (base_tree, own_tree, new_tree) =
        (tree_of(old_parent)?, tree_of(commit)?, tree_of(new_parent)?);
    let done = |status, new| Step {
        old: commit,
        new,
        status,
        conflicts: vec![],
    };
    if own_tree == base_tree {
        let new = write_commit(
            repo, &c, new_tree, new_parent, old_parent, own_tree, committer,
        )?;
        return Ok(done(Status::EmptyStart, Some(new)));
    }
    let options = repo.tree_merge_options().map_err(git_err)?;
    let labels = gix::merge::blob::builtin_driver::text::Labels {
        ancestor: Some("parent of replayed commit".into()),
        current: Some("new parent".into()),
        other: Some("replayed commit".into()),
    };
    let mut outcome = repo
        .merge_trees(base_tree, new_tree, own_tree, labels, options)
        .map_err(git_err)?;
    let how = TreatAsUnresolved::git();
    if outcome.has_unresolved_conflicts(how) {
        let mut conflicts = Vec::new();
        for conflict in outcome.conflicts.iter().filter(|c| c.is_unresolved(how)) {
            let (a, b) = conflict.changes_in_resolution();
            let kind = kind_of(conflict);
            for path in [a.location(), b.location()] {
                let entry = Conflict {
                    path: path.to_owned(),
                    kind,
                };
                if !conflicts.contains(&entry) {
                    conflicts.push(entry);
                }
            }
        }
        conflicts.sort_by(|a, b| a.path.cmp(&b.path));
        return Ok(Step {
            old: commit,
            new: None,
            status: Status::Conflicted,
            conflicts,
        });
    }
    // gix has no directory renames: a file the commit adds to a directory that onto renamed would bring the old
    // directory back. Git reports a conflict there, so does the engine.
    let conflicts = directory_rename_conflicts(repo, base_tree, own_tree, new_tree)?;
    if !conflicts.is_empty() {
        return Ok(Step {
            old: commit,
            new: None,
            status: Status::Conflicted,
            conflicts,
        });
    }
    let merged = outcome.tree.write().map_err(git_err)?.detach();
    if merged == new_tree {
        return Ok(done(Status::Empty, None));
    }
    let new = write_commit(
        repo, &c, merged, new_parent, old_parent, own_tree, committer,
    )?;
    let status = if new == commit {
        Status::Unchanged
    } else {
        Status::Clean
    };
    Ok(done(status, Some(new)))
}

/// Files that the commit (`base_tree` to `own_tree`) adds below a directory which exists in `base_tree`, is gone
/// from `new_tree` and had a file renamed out of it (`base_tree` to `new_tree`): the directory was renamed.
/// A directory that onto only deleted is not a conflict (git adds the file there). The reported path is the one
/// the commit added; git names the path in the new directory.
fn directory_rename_conflicts(
    repo: &gix::Repository,
    base_tree: ObjectId,
    own_tree: ObjectId,
    new_tree: ObjectId,
) -> Result<Vec<Conflict>, ReplayError> {
    use gix::diff::tree_with_rewrites::Change;
    let tree = |id: ObjectId| repo.find_tree(id).map_err(git_err);
    let (base, own, new) = (tree(base_tree)?, tree(own_tree)?, tree(new_tree)?);
    let diff = |a: &gix::Tree<'_>, b: &gix::Tree<'_>, renames| {
        let options = gix::diff::Options::default().with_rewrites(renames);
        repo.diff_tree_to_tree(a, b, options).map_err(git_err)
    };
    let renamed_from: Vec<BString> = diff(&base, &new, Some(Default::default()))?
        .into_iter()
        .filter_map(|c| match c {
            Change::Rewrite {
                source_location, ..
            } => Some(source_location),
            _ => None,
        })
        .collect();
    if renamed_from.is_empty() {
        return Ok(vec![]);
    }
    let exists = |t: &gix::Tree<'_>, dir: &[u8]| {
        t.lookup_entry(dir.split(|b| *b == b'/'))
            .map(|e| e.is_some())
            .map_err(git_err)
    };
    let mut out = Vec::new();
    for change in diff(&base, &own, None)? {
        let Change::Addition { location, .. } = change else {
            continue;
        };
        let mut end = 0;
        while let Some(i) = location[end..].find_byte(b'/') {
            end += i;
            let dir = &location[..end];
            end += 1;
            let moved = renamed_from
                .iter()
                .any(|s| s.starts_with(dir) && s.get(dir.len()) == Some(&b'/'));
            if moved && exists(&base, dir)? && !exists(&new, dir)? {
                out.push(Conflict {
                    path: location.clone(),
                    kind: "directory-rename",
                });
                break;
            }
        }
    }
    out.sort_by(|a, b| a.path.cmp(&b.path));
    Ok(out)
}

fn kind_of(c: &gix::merge::plumbing::tree::Conflict) -> &'static str {
    use gix::diff::tree_with_rewrites::Change;
    let failure = c.resolution.as_ref().err();
    match failure {
        Some(ResolutionFailure::OursModifiedTheirsDeleted) => return "modify/delete",
        Some(ResolutionFailure::SubmoduleMerge | ResolutionFailure::SubmoduleAddAdd) => {
            return "submodule"
        }
        Some(
            ResolutionFailure::OursRenamedTheirsRenamedDifferently { .. }
            | ResolutionFailure::OursRenamedTheirsRenamedToSameLocation,
        ) => return "rename/rename",
        Some(ResolutionFailure::OursDeletedTheirsRenamed) => return "rename/delete",
        Some(
            ResolutionFailure::OursModifiedTheirsDirectoryThenOursRenamed { .. }
            | ResolutionFailure::OursDirectoryTheirsNonDirectoryTheirsRenamed { .. }
            | ResolutionFailure::OursAddedTheirsAddedTypeMismatch { .. }
            | ResolutionFailure::OursModifiedTheirsRenamedTypeMismatch,
        ) => return "file/directory",
        Some(ResolutionFailure::Unknown) => return "unknown",
        None => {}
    }
    let (a, b) = c.changes_in_resolution();
    if matches!((a, b), (Change::Addition { .. }, Change::Addition { .. })) {
        return "add/add";
    }
    if matches!(
        c.resolution,
        Ok(
            Resolution::OursModifiedTheirsModifiedThenBlobContentMerge { .. }
                | Resolution::OursModifiedTheirsRenamedAndChangedThenRename { .. }
        )
    ) {
        return "content";
    }
    "unknown"
}

/// Writes the replayed commit, or returns `original` when tree and parent do not change.
fn write_commit(
    repo: &gix::Repository,
    original: &gix::Commit<'_>,
    tree: ObjectId,
    parent: ObjectId,
    old_parent: ObjectId,
    old_tree: ObjectId,
    committer: &Signature,
) -> Result<ObjectId, ReplayError> {
    if tree == old_tree && parent == old_parent {
        return Ok(original.id);
    }
    let decoded = original.decode().map_err(git_err)?;
    let commit = gix::objs::Commit {
        tree,
        parents: vec![parent].into(),
        author: decoded.author().map_err(git_err)?.into(),
        committer: committer.clone(),
        encoding: decoded.encoding.map(ToOwned::to_owned),
        message: decoded.message.to_owned(),
        // a signature or other header would not match the rewritten commit
        extra_headers: vec![],
    };
    Ok(repo.write_object(&commit).map_err(git_err)?.detach())
}

/// Convenience for callers that only want the path list of a conflicted step.
pub fn conflict_paths(step: &Step) -> Vec<String> {
    step.conflicts
        .iter()
        .map(|c| c.path.to_str_lossy().into_owned())
        .collect()
}
