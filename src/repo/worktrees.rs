//! Worktree registrations: which branches are checked out in a directory that exists, and which
//! `.git/worktrees/<id>` registrations point to a directory that is gone.
//!
//! gix provides `Repository::worktrees()` with `Proxy::{base, is_locked, is_prunable, lock_reason}`.
//! The one thing it does not expose is the registration's HEAD, which is read from `<git_dir>/HEAD`
//! here. That is the only direct file access in this module.
use std::path::PathBuf;

use crate::error::Result;

#[derive(Clone, Debug)]
pub struct StaleRegistration {
    pub id: String,
    /// Where the registration says the checkout was (may not exist).
    pub path: PathBuf,
    pub locked: bool,
    pub lock_reason: Option<String>,
    /// Branch the registration's HEAD points at; git refuses to delete it while the registration exists.
    pub branch: Option<String>,
    /// The registration directory `<common dir>/worktrees/<id>`.
    pub git_dir: PathBuf,
}

/// A linked worktree whose directory exists.
#[derive(Clone, Debug)]
pub struct Linked {
    pub id: String,
    pub dir: PathBuf,
    /// The registration directory `<common dir>/worktrees/<id>`.
    pub git_dir: PathBuf,
    pub branch: Option<String>,
    pub locked: bool,
    pub lock_reason: Option<String>,
}

#[derive(Clone, Debug, Default)]
pub struct Worktrees {
    /// (branch short name, checkout directory) for every checkout whose directory exists, main one included.
    pub in_use: Vec<(String, PathBuf)>,
    /// Registrations whose `.git` file target is gone (`git worktree prune` would remove the unlocked ones).
    pub stale: Vec<StaleRegistration>,
    /// The main checkout, when the repository has one.
    pub main_dir: Option<PathBuf>,
    /// Linked worktrees whose directory exists.
    pub linked: Vec<Linked>,
    /// Registrations whose checkout path contains `..` (relative links). They are not followed: neither `linked`
    /// nor `stale`, so each registration has exactly one status. `dir` is the path as written.
    pub relative: Vec<Linked>,
}

impl Worktrees {
    pub fn branch_dir(&self, branch: &str) -> Option<&PathBuf> {
        self.in_use
            .iter()
            .find(|(b, _)| b == branch)
            .map(|(_, p)| p)
    }
}

fn head_branch(head_file: &std::path::Path) -> Option<String> {
    let s = std::fs::read_to_string(head_file).ok()?;
    let r = s.trim().strip_prefix("ref:")?.trim();
    r.strip_prefix("refs/heads/").map(str::to_string)
}

/// A path for display: `.` and `a/..` pairs removed without touching the file system (relative worktree links
/// leave `<repo>/.git/worktrees/x/../../../wt` behind). Never use the result to read, test or remove anything:
/// `..` after a symbolic link names another directory than the lexical one.
pub fn show(p: &std::path::Path) -> String {
    crate::output::p2s(&normalize(p))
}

fn normalize(p: &std::path::Path) -> PathBuf {
    use std::path::Component;
    let mut out = PathBuf::new();
    for c in p.components() {
        match c {
            Component::CurDir => {}
            Component::ParentDir
                if matches!(out.components().next_back(), Some(Component::Normal(_))) =>
            {
                out.pop();
            }
            c => out.push(c.as_os_str()),
        }
    }
    out
}

pub fn scan(repo: &gix::Repository) -> Result<Worktrees> {
    let mut w = Worktrees::default();
    // main worktree: HEAD of the common dir
    if let Some(dir) = repo.workdir() {
        if let Ok(Some(name)) = repo.head_name() {
            if let Some(b) = crate::repo::branch_short(name.as_ref()) {
                w.in_use.push((b, dir.to_path_buf()));
            }
        }
        w.main_dir = Some(dir.to_path_buf());
    }
    for p in repo.worktrees()? {
        let id = p.id().map(|i| i.to_string()).unwrap_or_default();
        let base = p.base().ok();
        if let Some(b) = base.as_ref().filter(|b| {
            b.components()
                .any(|c| matches!(c, std::path::Component::ParentDir))
        }) {
            let branch = head_branch(&p.git_dir().join("HEAD"));
            if let (true, Some(br)) = (b.is_dir(), branch.clone()) {
                w.in_use.push((br, b.clone()));
            }
            w.relative.push(Linked {
                id,
                dir: b.clone(),
                git_dir: p.git_dir().to_path_buf(),
                branch,
                locked: p.is_locked(),
                lock_reason: p.lock_reason().map(|r| r.to_string()),
            });
            continue;
        }
        let dir_exists = base.as_ref().map(|b| b.is_dir()).unwrap_or(false);
        if dir_exists {
            if let (Some(b), Some(base)) = (head_branch(&p.git_dir().join("HEAD")), base.clone()) {
                w.in_use.push((b, base));
            }
            if let Some(base) = base.clone() {
                w.linked.push(Linked {
                    id: id.clone(),
                    dir: base,
                    git_dir: p.git_dir().to_path_buf(),
                    branch: head_branch(&p.git_dir().join("HEAD")),
                    locked: p.is_locked(),
                    lock_reason: p.lock_reason().map(|r| r.to_string()),
                });
            }
        }
        // git's own definition: not locked, and the gitdir file's target is gone
        let locked = p.is_locked();
        let target_gone = p.is_prunable()
            || (locked
                && base
                    .as_ref()
                    .map(|b| !b.join(".git").exists())
                    .unwrap_or(true));
        if target_gone {
            w.stale.push(StaleRegistration {
                id,
                path: base.unwrap_or_default(),
                locked,
                lock_reason: p.lock_reason().map(|r| r.to_string()),
                branch: head_branch(&p.git_dir().join("HEAD")),
                git_dir: p.git_dir().to_path_buf(),
            });
        }
    }
    Ok(w)
}
