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
    if let Some(repo) = discover::discover(cwd) {
        if let Some(top) = repo.workdir() {
            let top = top.to_path_buf();
            let sup = topmost_superproject(&top);
            if sup != top {
                return Some(sup);
            }
            if top.join(".gitmodules").is_file() || !submodule_paths(&top).is_empty() {
                return Some(top);
            }
        }
    }
    std::env::var_os("STW_ROOT").map(PathBuf::from)
}

/// The levels `topmost_superproject` climbs at most (Go: `workspaceRoot`, `sync.go:215`).
pub const SUPERPROJECT_LEVELS: usize = 16;

/// The top-level superproject of a checkout, found by climbing `superproject_of` up to `SUPERPROJECT_LEVELS` times;
/// `top` itself when it is no submodule checkout. Go: `git rev-parse --show-superproject-working-tree`, repeated.
pub fn topmost_superproject(top: &Path) -> PathBuf {
    let mut cur = top.to_path_buf();
    for _ in 0..SUPERPROJECT_LEVELS {
        match superproject_of(&cur) {
            Some(up) => cur = up,
            None => break,
        }
    }
    cur
}

/// A submodule checkout: the superproject is the nearest repository above it (`discover`: it honors
/// `GIT_CEILING_DIRECTORIES` and the owner check, as git does), provided its index has a gitlink at that path. Only
/// the nearest repository counts, as for `git rev-parse --show-superproject-working-tree`: a repository between the
/// checkout and an outer one that names it ends the search.
fn superproject_of(top: &Path) -> Option<PathBuf> {
    let above = discover::discover(top.parent()?)?;
    let dir = above.workdir()?.to_path_buf();
    let real = |p: &Path| std::fs::canonicalize(p).unwrap_or_else(|_| p.to_path_buf());
    let rel = real(top).strip_prefix(real(&dir)).ok()?.to_path_buf();
    submodule_paths(&dir)
        .iter()
        .any(|p| Path::new(p) == rel)
        .then_some(dir)
}

/// Root for `stw add`: the superproject of the repository of `cwd`, else that repository's top level (a
/// workspace without `.gitmodules` yet is a valid target), and only without any repository `$STW_ROOT`.
pub fn add_root(cwd: &Path) -> Option<PathBuf> {
    if let Some(top) = top_level(cwd) {
        return Some(superproject_of(&top).unwrap_or(top));
    }
    std::env::var_os("STW_ROOT").map(PathBuf::from)
}

/// `submodule.*.path` values of `<root>/.gitmodules`. Only `stw add` reads them (it writes the file and the
/// gitlink together); the repositories of a workspace come from `submodule_paths`.
pub fn gitmodules_paths(root: &Path) -> Vec<String> {
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

/// The paths of the gitlinks (mode 160000, any conflict stage) of the index of the work tree at `root`, in path
/// order, each once: the direct submodules, as Go lists them (`stwsync` `submodules`, `ls-files --stage -z`).
/// `.gitmodules` is not read: a gitlink needs no entry there, a path it names without a gitlink is no
/// repository, and a clone that no gitlink names is never one. Only the repository's own config is read
/// (`open_isolated`); a root that is no repository, or an unreadable index, gives no paths (Go: the run fails
/// with "cannot list the submodules"). A path that is not
/// UTF-8 is listed lossily and is refused by `path_rejection` (charset).
pub fn submodule_paths(root: &Path) -> Vec<String> {
    let Ok(repo) = open_isolated(root) else {
        return Vec::new();
    };
    let Ok(index) = repo.index_or_empty() else {
        return Vec::new();
    };
    let mut out: Vec<String> = index
        .entries()
        .iter()
        .filter(|e| e.mode.is_submodule())
        .map(|e| e.path(&index).to_str_lossy().into_owned())
        .collect();
    // as Go (`sort.Strings`, `slices.Compact`): gix does not sort what it reads, and the copies of a conflicted
    // gitlink (stages 1 to 3) must be adjacent for the compaction
    out.sort();
    out.dedup();
    out
}

/// Why a gitlink path is not accepted as a repository of the workspace; None when it is. The text of the
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

/// The repositories Go lists in one run at most (`repoLimit`, `sync.go:22`); above it the run fails.
pub const REPO_LIMIT: usize = 256;

/// What one depth-first walk over the nested gitlinks finds.
struct Walk {
    repos: Vec<RepoRef>,
    rejected: Vec<(String, String)>,
    seen: Vec<PathBuf>,
    limit: usize,
    exceeded: bool,
    /// Submodule nesting depth of each entry of `repos` (the root is 0).
    depths: Vec<usize>,
    /// Gitlinks of a repository at this depth are not followed (`usize::MAX`: no bound).
    max_depth: usize,
    /// Names (`""` is the root) of the repositories that have gitlinks which the depth bound left out.
    cut: Vec<String>,
}

/// Lists the repository at `dir` (name `rel`, "" for the root's children) and then, depth-first, each checked-out
/// gitlink of its index (Go: `walk`). A gitlink path is checked against the repository that holds it
/// (`path_rejection`), so a nested path never leaves the workspace either; an unchecked-out gitlink has nothing to
/// descend into. A refused path is kept with its path below the root (`outer/inner`).
fn walk(dir: &Path, prefix: &str, depth: usize, w: &mut Walk) {
    let subs = submodule_paths(dir);
    // only a gitlink with a checkout is left out by the bound (an unchecked-out one is never listed)
    if depth >= w.max_depth && subs.iter().any(|p| dir.join(p).join(".git").exists()) {
        w.cut.push(prefix.to_string());
        return;
    }
    for p in subs {
        if w.exceeded {
            return;
        }
        let full = |n: &str| {
            if prefix.is_empty() {
                n.to_string()
            } else {
                format!("{prefix}/{n}")
            }
        };
        if let Some(why) = path_rejection(dir, &p) {
            w.rejected.push((full(&p), why));
            continue;
        }
        let Some(name) = accepted_name(dir, &p) else {
            continue;
        };
        let path = dir.join(&name);
        let real = std::fs::canonicalize(&path).unwrap_or_else(|_| path.clone());
        if !path.join(".git").exists() || w.seen.contains(&real) {
            continue;
        }
        w.seen.push(real);
        let name = full(&name);
        w.repos.push(RepoRef {
            name: name.clone(),
            path: path.clone(),
        });
        w.depths.push(depth + 1);
        if w.repos.len() > w.limit {
            w.exceeded = true;
            return;
        }
        walk(&path, &name, depth + 1, w);
    }
}

fn walk_root(root: &Path, limit: usize, max_depth: usize) -> Walk {
    let name = root
        .file_name()
        .map(|n| n.to_string_lossy().into_owned())
        .unwrap_or_else(|| "root".into());
    let real = std::fs::canonicalize(root).unwrap_or_else(|_| root.to_path_buf());
    let mut w = Walk {
        repos: vec![RepoRef {
            name,
            path: root.to_path_buf(),
        }],
        rejected: Vec::new(),
        seen: vec![real],
        limit,
        exceeded: false,
        depths: vec![0],
        max_depth,
        cut: Vec::new(),
    };
    walk(root, "", 0, &mut w);
    w
}

/// The workspace as `stw status --all` lists it: the repositories of `list_repos` with their nesting depth (the root
/// is 0), and the repositories whose gitlinks lie below `max_depth` and are therefore not followed.
#[derive(Debug)]
pub struct Listing {
    pub repos: Vec<RepoRef>,
    /// Parallel to `repos`.
    pub depths: Vec<usize>,
    /// Names of the repositories with gitlinks below the depth bound (the root by its own name).
    pub cut: Vec<String>,
    /// More than `limit` repositories (`repos` holds at most `limit` + 1).
    pub exceeded: bool,
}

/// `list_repos_limited` with a bound on the nesting depth: gitlinks of a repository at depth `max_depth` are not
/// followed (0 lists the root only).
pub fn list_repos_depth(root: &Path, limit: usize, max_depth: usize) -> Listing {
    let w = walk_root(root, limit, max_depth);
    let root_name = w.repos[0].name.clone();
    let cut = w
        .cut
        .iter()
        .map(|c| {
            if c.is_empty() {
                root_name.clone()
            } else {
                c.clone()
            }
        })
        .collect();
    Listing {
        repos: w.repos,
        depths: w.depths,
        cut,
        exceeded: w.exceeded,
    }
}

/// The gitlink paths of the workspace that `path_rejection` refuses, with the reason, in walk order; a nested
/// path is given below the root (`outer/inner`). They are never listed as repositories; the commands report them.
pub fn rejected_paths(root: &Path) -> Vec<(String, String)> {
    walk_root(root, REPO_LIMIT, usize::MAX).rejected
}

/// The normalized name of an accepted gitlink path, else None.
pub fn accepted_name(root: &Path, p: &str) -> Option<String> {
    if path_rejection(root, p).is_some() {
        return None;
    }
    crate::add::SubPath::parse(p)
        .ok()
        .map(|s| s.as_str().to_string())
}

/// The root and every checked-out submodule, nested ones too (a gitlink of the index and a `.git` entry), depth-first
/// in path order, as Go walks them. A path that `path_rejection` refuses is left out (see `rejected_paths`). The name
/// is the normalized path below the root (`outer/inner`); a directory is listed once, and never twice with the
/// root. The list stops at `REPO_LIMIT` + 1 entries; `list_repos_checked` fails there, as Go does.
pub fn list_repos(root: &Path) -> Vec<RepoRef> {
    walk_root(root, REPO_LIMIT, usize::MAX).repos
}

/// `list_repos` with another limit: the repositories found (at most `limit` + 1) and whether there are more than `limit`.
pub fn list_repos_limited(root: &Path, limit: usize) -> (Vec<RepoRef>, bool) {
    let w = walk_root(root, limit, usize::MAX);
    (w.repos, w.exceeded)
}

/// `list_repos`, but a workspace of more than `REPO_LIMIT` repositories is an error (Go: `sync-limit`).
pub fn list_repos_checked(root: &Path) -> Result<Vec<RepoRef>> {
    list_repos_checked_with(root, REPO_LIMIT)
}

/// `list_repos_checked` with another limit.
pub fn list_repos_checked_with(root: &Path, limit: usize) -> Result<Vec<RepoRef>> {
    let (repos, exceeded) = list_repos_limited(root, limit);
    if exceeded {
        return Err(crate::error::Error::usage(format!(
            "the workspace has more than {limit} repositories"
        )));
    }
    Ok(repos)
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
    discover::discover(dir).and_then(|r| r.workdir().map(Path::to_path_buf))
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
