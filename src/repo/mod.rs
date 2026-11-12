//! Workspace discovery and the small repository facts all three commands share.
use std::path::{Path, PathBuf};

use gix::bstr::ByteSlice;
use gix::refs::Category;
use gix::remote::Direction;
use gix::ObjectId;

use crate::error::Result;

pub mod discover;
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
            if let Some(sup) = superproject_of(&top) {
                return Some(sup);
            }
            if top.join(".gitmodules").is_file() {
                return Some(top);
            }
        }
    }
    std::env::var_os("STW_ROOT").map(PathBuf::from)
}

/// A submodule checkout: the superproject is the nearest parent with a `.gitmodules` naming it.
fn superproject_of(top: &Path) -> Option<PathBuf> {
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
    None
}

/// Root for `stw add`: the superproject of the repository of `cwd`, else that repository's top level (a
/// workspace without `.gitmodules` yet is a valid target), and only without any repository `$STW_ROOT`.
pub fn add_root(cwd: &Path) -> Option<PathBuf> {
    if let Some(top) = top_level(cwd) {
        return Some(superproject_of(&top).unwrap_or(top));
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

/// Why a `.gitmodules` path is not accepted as a repository of the workspace; None when it is. The text of the
/// path is untrusted: the rules of `stw add` (`SubPath::parse`: `[A-Za-z0-9._/-]`, relative, no `..`, no `-x`
/// component, no git-special name), no symbolic link among its components, and the real path of an existing
/// directory stays below the real path of the root. The reason does not repeat the path.
pub fn path_rejection(root: &Path, p: &str) -> Option<String> {
    if p.bytes()
        .any(|b| !(b.is_ascii_alphanumeric() || matches!(b, b'.' | b'_' | b'-' | b'/')))
    {
        return Some("rename with git mv; stw handles only [A-Za-z0-9._/-]".into());
    }
    let Ok(sub) = crate::add::SubPath::parse(p) else {
        return Some(
            "not a plain relative path (no empty, '.', '..' or '-x' component, no git-special name)"
                .into(),
        );
    };
    let mut cur = root.to_path_buf();
    for seg in sub.as_str().split('/') {
        cur.push(seg);
        if std::fs::symlink_metadata(&cur).is_ok_and(|m| m.file_type().is_symlink()) {
            return Some("a component is a symbolic link".into());
        }
    }
    if let (Ok(real), Ok(base)) = (std::fs::canonicalize(&cur), std::fs::canonicalize(root)) {
        if !is_below(&real, &base) {
            return Some("the directory is not below the workspace".into());
        }
    }
    None
}

/// The `.gitmodules` paths of `root` that `path_rejection` refuses, with the reason (each path once, a trailing
/// `/` ignored, in file order). They are never listed as repositories; the commands report them.
pub fn rejected_paths(root: &Path) -> Vec<(String, String)> {
    let mut out: Vec<(String, String)> = Vec::new();
    for p in submodule_paths(root) {
        if let Some(why) = path_rejection(root, &p) {
            let key = p.strip_suffix('/').unwrap_or(&p);
            if !out
                .iter()
                .any(|(q, _)| q.strip_suffix('/').unwrap_or(q) == key)
            {
                out.push((p, why));
            }
        }
    }
    out
}

/// The normalized name of an accepted `.gitmodules` path (no trailing `/`), else None.
pub fn accepted_name(root: &Path, p: &str) -> Option<String> {
    if path_rejection(root, p).is_some() {
        return None;
    }
    crate::add::SubPath::parse(p)
        .ok()
        .map(|s| s.as_str().to_string())
}

/// The root and every checked-out submodule (a `.git` entry must exist), in `.gitmodules` order. A path that
/// `path_rejection` refuses is left out (see `rejected_paths`). The name is the normalized path; a directory
/// named twice (`a`, `a/`) is listed once, and never twice with the root.
pub fn list_repos(root: &Path) -> Vec<RepoRef> {
    let name = root
        .file_name()
        .map(|n| n.to_string_lossy().into_owned())
        .unwrap_or_else(|| "root".into());
    let real = |p: &Path| std::fs::canonicalize(p).unwrap_or_else(|_| p.to_path_buf());
    let mut seen = vec![real(root)];
    let mut v = vec![RepoRef {
        name,
        path: root.to_path_buf(),
    }];
    for p in submodule_paths(root) {
        let Some(name) = accepted_name(root, &p) else {
            continue;
        };
        let path = root.join(&name);
        if path.join(".git").exists() && !seen.contains(&real(&path)) {
            seen.push(real(&path));
            v.push(RepoRef { name, path });
        }
    }
    v
}

/// `real` lies strictly below `base` (both real paths).
pub fn is_below(real: &Path, base: &Path) -> bool {
    real.starts_with(base) && real != base
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

/// At most this many repositories are worked on at once (the bound of the Go `stw status`).
pub const MAX_PARALLEL: usize = 16;

/// `f` on every item, at most `limit` at a time; the results are in the order of `items`.
pub fn par_map<I: Sync, T: Send>(items: &[I], limit: usize, f: impl Fn(&I) -> T + Sync) -> Vec<T> {
    use std::sync::atomic::{AtomicUsize, Ordering};
    let next = AtomicUsize::new(0);
    let out: std::sync::Mutex<Vec<Option<T>>> =
        std::sync::Mutex::new((0..items.len()).map(|_| None).collect());
    std::thread::scope(|s| {
        for _ in 0..limit.max(1).min(items.len()) {
            s.spawn(|| loop {
                let i = next.fetch_add(1, Ordering::SeqCst);
                let Some(item) = items.get(i) else { break };
                let v = f(item);
                out.lock().unwrap()[i] = Some(v);
            });
        }
    });
    out.into_inner()
        .unwrap()
        .into_iter()
        .map(|v| v.expect("worker finished"))
        .collect()
}

/// Run `f` on every item of `list`, at most `MAX_PARALLEL` at a time, and give the items back in order.
pub fn for_each_mut<T: Send>(list: Vec<T>, f: impl Fn(&mut T) + Sync) -> Vec<T> {
    let cells: Vec<std::sync::Mutex<T>> = list.into_iter().map(std::sync::Mutex::new).collect();
    par_map(&cells, MAX_PARALLEL, |c| f(&mut c.lock().unwrap()));
    cells.into_iter().map(|c| c.into_inner().unwrap()).collect()
}

/// Run `f` for every repository, at most `MAX_PARALLEL` at a time; results keep the repository order.
pub fn for_each<T: Send>(list: &[RepoRef], f: impl Fn(&RepoRef) -> T + Sync) -> Vec<T> {
    par_map(list, MAX_PARALLEL, f)
}
