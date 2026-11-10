//! Workspace discovery and the small repository facts all three commands share.
use std::path::{Path, PathBuf};

use gix::bstr::ByteSlice;
use gix::refs::Category;
use gix::remote::Direction;
use gix::ObjectId;

use crate::error::Result;

pub mod worktrees;

/// One repository of the workspace: the root itself or a checked-out submodule.
#[derive(Clone, Debug)]
pub struct RepoRef {
    pub name: String,
    pub path: PathBuf,
}

/// Root choice: `--root`, else the repository (or its superproject) of the current directory,
/// else `$STW_ROOT`. The prototype's hard-coded last resort path is intentionally not copied.
pub fn default_root(cwd: &Path) -> Option<PathBuf> {
    if let Ok(repo) = gix::discover(cwd) {
        if let Some(top) = repo.workdir() {
            let top = top.to_path_buf();
            // a submodule checkout: the superproject is the nearest parent with a .gitmodules naming it
            let mut cur = top.parent();
            while let Some(dir) = cur {
                if dir.join(".gitmodules").is_file() && dir.join(".git").exists() {
                    if let Ok(rel) = top.strip_prefix(dir) {
                        if submodule_paths(dir).iter().any(|p| Path::new(p) == rel) {
                            return Some(dir.to_path_buf());
                        }
                    }
                }
                cur = dir.parent();
            }
            if top.join(".gitmodules").is_file() {
                return Some(top);
            }
        }
    }
    std::env::var_os("STW_ROOT").map(PathBuf::from)
}

/// `submodule.*.path` values of `<root>/.gitmodules`.
pub fn submodule_paths(root: &Path) -> Vec<String> {
    let file = root.join(".gitmodules");
    let Ok(cfg) = gix::config::File::from_path_no_includes(file, gix::config::Source::Worktree)
    else {
        return Vec::new();
    };
    let mut out = Vec::new();
    if let Some(sections) = cfg.sections_by_name("submodule") {
        for s in sections {
            if let Some(p) = s.value("path") {
                out.push(p.to_str_lossy().into_owned());
            }
        }
    }
    out
}

/// The root and every checked-out submodule (a `.git` entry must exist), in `.gitmodules` order.
pub fn list_repos(root: &Path) -> Vec<RepoRef> {
    let name = root
        .file_name()
        .map(|n| n.to_string_lossy().into_owned())
        .unwrap_or_else(|| "root".into());
    let mut v = vec![RepoRef {
        name,
        path: root.to_path_buf(),
    }];
    for p in submodule_paths(root) {
        let path = root.join(&p);
        if path.join(".git").exists() {
            v.push(RepoRef { name: p, path });
        }
    }
    v
}

/// Name and email written to reflogs when neither the environment nor the Git config names a committer.
pub const FALLBACK_NAME: &str = "stw";
pub const FALLBACK_EMAIL: &str = "stw@localhost";

/// Open a repository. The identity for reflog entries is resolved in this order:
/// 1. `GIT_COMMITTER_NAME` and `GIT_COMMITTER_EMAIL`, only when both are set and non-empty. If only one is
///    usable (or one is empty), both are ignored, and so is the `EMAIL` variable, so no half-mixed identity appears;
/// 2. `committer.*`, then `user.*` of the Git config (as gix resolves them);
/// 3. the fallback `stw <stw@localhost>`, which lives in memory only: it is never written to a config, and stw
///    creates no commits.
pub fn open(path: &Path) -> Result<gix::Repository> {
    let usable = |k: &str| std::env::var_os(k).is_some_and(|v| !v.is_empty());
    let env_identity = usable("GIT_COMMITTER_NAME") && usable("GIT_COMMITTER_EMAIL");
    let mut repo = if env_identity {
        gix::open(path)?
    } else {
        let mut perms = gix::open::Permissions::default();
        perms.env.identity = gix::sec::Permission::Deny;
        gix::open_opts(path, gix::open::Options::default().permissions(perms))?
    };
    repo.committer_or_set_fallback(FALLBACK_NAME, FALLBACK_EMAIL)?;
    drop_filter_drivers(&mut repo)?;
    Ok(repo)
}

/// Open a repository reading only its own config files (no user, no system config, no environment
/// selectors). For `stw add`, which inspects a fresh clone and the workspace before any git program runs.
pub fn open_isolated(path: &Path) -> Result<gix::Repository> {
    Ok(gix::open_opts(path, gix::open::Options::isolated())?)
}

/// The top level of the work tree that contains `dir`, if there is one.
pub fn top_level(dir: &Path) -> Option<PathBuf> {
    gix::discover(dir)
        .ok()
        .and_then(|r| r.workdir().map(Path::to_path_buf))
}

/// Forget every `filter.<name>.*` driver of the (in-memory) configuration. gix would start the `clean`,
/// `smudge` or `process` program named there (also from the repository's own config) while it compares a
/// worktree file with the index; stw's reads start no program. Files with a `filter=` attribute are then
/// compared unconverted, so a driver that rewrites content (git-lfs, keyword expansion) can make such a
/// file look modified. Nothing is written to any config file.
fn drop_filter_drivers(repo: &mut gix::Repository) -> Result<()> {
    let mut snapshot = repo.config_snapshot_mut();
    let file: &mut gix::config::File = &mut snapshot;
    let subsections: Vec<Option<gix::bstr::BString>> = file
        .sections_by_name("filter")
        .into_iter()
        .flatten()
        .map(|s| s.header().subsection_name().map(ToOwned::to_owned))
        .collect();
    for sub in subsections {
        while file
            .remove_section("filter", sub.as_ref().map(|b| b.as_ref()))
            .is_some()
        {}
    }
    snapshot.commit()?;
    Ok(())
}

/// `owner/repo` of origin when its host is exactly `github.com` (any scheme, also `git@github.com:o/r`).
pub fn slug(repo: &gix::Repository) -> Option<String> {
    let remote = repo.find_remote("origin").ok()?;
    let url = remote.url(Direction::Fetch)?;
    if !url.host()?.eq_ignore_ascii_case("github.com") {
        return None;
    }
    let path = url.path.to_str_lossy();
    let rest = path.trim_matches('/');
    let rest = rest.strip_suffix(".git").unwrap_or(rest);
    if rest.is_empty() {
        None
    } else {
        Some(rest.to_string())
    }
}

/// Base ref of the repository: target of `refs/remotes/origin/HEAD`, else origin/main, origin/master.
pub fn base_ref(repo: &gix::Repository) -> Option<String> {
    if let Ok(Some(r)) = repo.try_find_reference("refs/remotes/origin/HEAD") {
        if let gix::refs::TargetRef::Symbolic(name) = r.target() {
            return Some(name.as_bstr().to_str_lossy().into_owned());
        }
    }
    for cand in ["refs/remotes/origin/main", "refs/remotes/origin/master"] {
        if matches!(repo.try_find_reference(cand), Ok(Some(_))) {
            return Some(cand.to_string());
        }
    }
    None
}

pub fn resolve(repo: &gix::Repository, full_ref: &str) -> Option<ObjectId> {
    let mut r = repo.try_find_reference(full_ref).ok()??;
    r.peel_to_id().ok().map(|i| i.detach())
}

/// Number of commits reachable from `include` and not from `exclude`.
pub fn count_range(repo: &gix::Repository, include: ObjectId, exclude: ObjectId) -> Result<usize> {
    let walk = repo.rev_walk([include]).with_hidden([exclude]).all()?;
    let mut n = 0;
    for info in walk {
        info?;
        n += 1;
    }
    Ok(n)
}

/// True if `a` is reachable from `b` (or equal).
pub fn is_ancestor(repo: &gix::Repository, a: ObjectId, b: ObjectId) -> Result<bool> {
    if a == b {
        return Ok(true);
    }
    let mut walk = repo.rev_walk([a]).with_hidden([b]).all()?;
    Ok(walk.next().is_none())
}

/// Local branches: (short name, tip).
pub fn local_branches(repo: &gix::Repository) -> Result<Vec<(String, ObjectId)>> {
    let mut out = Vec::new();
    let platform = repo.references()?;
    for r in platform.local_branches()? {
        let mut r = r?;
        let name = r.name().as_bstr().to_str_lossy().into_owned();
        let short = name
            .strip_prefix("refs/heads/")
            .unwrap_or(&name)
            .to_string();
        if let Ok(id) = r.peel_to_id() {
            out.push((short, id.detach()));
        }
    }
    out.sort();
    Ok(out)
}

/// Upstream facts of a local branch.
#[derive(Clone, Debug, Default)]
pub struct Upstream {
    /// `origin/main`, when the tracking ref exists.
    pub short: Option<String>,
    /// Name of the configured upstream even when its tracking ref is gone (git's `%(upstream:short)`).
    pub name: Option<String>,
    pub tip: Option<ObjectId>,
    /// The tracking ref name is known (remote configured) but the ref is missing: git's `[gone]`.
    pub gone: bool,
}

pub fn upstream_of(repo: &gix::Repository, branch: &str) -> Upstream {
    let full = format!("refs/heads/{branch}");
    let Ok(name) = gix::refs::FullName::try_from(full.as_str()) else {
        return Upstream::default();
    };
    match repo.branch_remote_tracking_ref_name(name.as_ref(), Direction::Fetch) {
        Some(Ok(tracking)) => {
            let t = tracking.as_bstr().to_str_lossy().into_owned();
            match resolve(repo, &t) {
                Some(tip) => {
                    let short = t
                        .strip_prefix("refs/remotes/")
                        .map(str::to_string)
                        .unwrap_or(t);
                    Upstream {
                        short: Some(short.clone()),
                        name: Some(short),
                        tip: Some(tip),
                        gone: false,
                    }
                }
                None => {
                    let short = t
                        .strip_prefix("refs/remotes/")
                        .map(str::to_string)
                        .unwrap_or(t);
                    Upstream {
                        short: None,
                        name: Some(short),
                        tip: None,
                        gone: true,
                    }
                }
            }
        }
        // no remote-tracking ref can exist (remote is a URL, or no merge ref): git reports no upstream
        _ => Upstream {
            short: None,
            name: None,
            tip: None,
            gone: false,
        },
    }
}

/// Short branch name for a full ref name, if it is a local branch.
pub fn branch_short(full: &gix::refs::FullNameRef) -> Option<String> {
    match full.category_and_short_name() {
        Some((Category::LocalBranch, s)) => Some(s.to_str_lossy().into_owned()),
        _ => None,
    }
}

/// Run `f` for every repository in parallel; results keep the repository order.
pub fn for_each<T: Send>(list: &[RepoRef], f: impl Fn(&RepoRef) -> T + Sync) -> Vec<T> {
    std::thread::scope(|s| {
        let handles: Vec<_> = list.iter().map(|r| s.spawn(|| f(r))).collect();
        handles
            .into_iter()
            .map(|h| h.join().expect("repository worker panicked"))
            .collect()
    })
}
