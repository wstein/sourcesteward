//! Repository discovery as Git does it: upwards from a directory, stopping at `GIT_CEILING_DIRECTORIES`, and a
//! repository owned by another user is used only when `safe.directory` (system or global config, never the
//! repository's own config) names its work tree. Read-only; nothing is written to any config.
use std::ffi::OsString;
use std::path::{Path, PathBuf};

/// The repository around `dir`, or `None`: no repository, one above `GIT_CEILING_DIRECTORIES`, or one that
/// belongs to another user and is not named by `safe.directory`.
pub fn discover(dir: &Path) -> Option<gix::Repository> {
    discover_in(dir, std::env::var_os("GIT_CEILING_DIRECTORIES").as_ref())
}

fn discover_in(dir: &Path, ceiling_env: Option<&OsString>) -> Option<gix::Repository> {
    let ceilings = ceiling_env.map(parse_ceilings).unwrap_or_default();
    let safe = safe_directory_values();
    discover_with(dir, &ceilings, &safe)
}

/// `GIT_CEILING_DIRECTORIES`: absolute entries only, as Git reads it. Deviation: after an empty entry Git stops
/// resolving symlinks in the following entries; stw resolves every entry (stricter: it may hide one more
/// repository than Git).
fn parse_ceilings(v: &OsString) -> Vec<PathBuf> {
    std::env::split_paths(v)
        .filter(|p| p.is_absolute())
        .collect()
}

fn discover_with(dir: &Path, ceilings: &[PathBuf], safe: &[String]) -> Option<gix::Repository> {
    let options = gix::discover::upwards::Options {
        ceiling_dirs: ceilings.to_vec(),
        // Git ignores a ceiling list that does not contain the start
        match_ceiling_dir_or_error: false,
        ..Default::default()
    };
    let repo = gix::ThreadSafeRepository::discover_opts(dir, options, |open, _trust| open)
        .ok()?
        .to_thread_local();
    let tree = repo
        .workdir()
        .unwrap_or_else(|| repo.git_dir())
        .to_path_buf();
    if behind_ceiling(dir, &tree, ceilings) {
        return None;
    }
    trusted(&repo, repo.git_dir_trust(), safe).then_some(repo)
}

/// Git does not enter a ceiling directory: a repository at a ceiling, or above one that contains the start,
/// is not found (a ceiling equal to the start is ignored). gix still returns a repository at the ceiling itself.
fn behind_ceiling(start: &Path, tree: &Path, ceilings: &[PathBuf]) -> bool {
    let canon = |p: &Path| std::fs::canonicalize(p).unwrap_or_else(|_| p.to_path_buf());
    let (start, tree) = (canon(start), canon(tree));
    ceilings
        .iter()
        .map(|c| canon(c))
        .any(|c| start.starts_with(&c) && start != c && c.starts_with(&tree))
}

/// gix decides trust when it opens the repository (`git_dir_trust`: ownership of the git dir, the common dir
/// and the work tree; it also trusts `/dir/*` for `/dir` itself, which Git does not). A repository of another
/// user is kept only if `safe.directory` (system or global config) names its work tree: this check is an
/// additional fallback, not Git's own matching.
fn trusted(repo: &gix::Repository, trust: gix::sec::Trust, safe: &[String]) -> bool {
    if trust == gix::sec::Trust::Full {
        return true;
    }
    let tree = repo.workdir().unwrap_or_else(|| repo.git_dir());
    safe_directory_allows(safe, tree)
}

/// `safe.directory` from the system and global config only (a repository must not vouch for itself). Values
/// given by `-c` or `GIT_CONFIG_COUNT` count only for Git.
fn safe_directory_values() -> Vec<String> {
    let Ok(file) = gix::config::File::from_globals() else {
        return Vec::new();
    };
    file.strings("safe.directory")
        .map(|v| v.iter().map(|s| s.to_string()).collect())
        .unwrap_or_default()
}

/// The fallback match: `*` allows all, a trailing `/*` allows everything below that directory, otherwise the
/// path must be equal (a trailing slash is ignored). An empty value resets the list.
fn safe_directory_allows(values: &[String], workdir: &Path) -> bool {
    let mut allowed = false;
    for v in values {
        if v.is_empty() {
            allowed = false;
        } else if v == "*" {
            allowed = true;
        } else if let Some(prefix) = v.strip_suffix("/*") {
            if workdir.starts_with(prefix) && workdir != Path::new(prefix) {
                allowed = true;
            }
        } else if Path::new(v.trim_end_matches('/')) == workdir {
            allowed = true;
        }
    }
    allowed
}

#[cfg(test)]
mod tests {
    use super::*;

    fn init(p: &Path) {
        std::fs::create_dir_all(p).unwrap();
        gix::init(p).unwrap();
    }

    fn canon(p: &Path) -> PathBuf {
        std::fs::canonicalize(p).unwrap()
    }

    #[test]
    fn finds_the_enclosing_repository_without_a_ceiling() {
        let t = tempfile::tempdir().unwrap();
        let root = canon(t.path());
        init(&root);
        std::fs::create_dir_all(root.join("a/b")).unwrap();
        let r = discover_with(&root.join("a/b"), &[], &[]).unwrap();
        assert_eq!(canon(r.workdir().unwrap()), root);
    }

    #[test]
    fn a_ceiling_hides_a_repository_above_it() {
        let t = tempfile::tempdir().unwrap();
        let root = canon(t.path());
        init(&root);
        std::fs::create_dir_all(root.join("sub/deep")).unwrap();
        // the ceiling is the parent of the start: the repository at `root` is not seen
        assert!(discover_with(&root.join("sub/deep"), std::slice::from_ref(&root), &[]).is_none());
        // a ceiling between start and repository hides it as well
        assert!(discover_with(&root.join("sub/deep"), &[root.join("sub")], &[]).is_none());
        // a ceiling equal to the start is ignored, as Git does
        assert!(discover_with(&root, std::slice::from_ref(&root), &[]).is_some());
        // a ceiling elsewhere does not hide it
        let other = tempfile::tempdir().unwrap();
        let r = discover_with(&root.join("sub/deep"), &[canon(other.path())], &[]);
        assert_eq!(canon(r.unwrap().workdir().unwrap()), root);
        // a repository inside the ceiling is still found
        init(&root.join("sub"));
        let r = discover_with(&root.join("sub/deep"), std::slice::from_ref(&root), &[]).unwrap();
        assert_eq!(canon(r.workdir().unwrap()), root.join("sub"));
    }

    #[test]
    fn a_ceiling_named_through_a_symlink_hides_the_repository() {
        let t = tempfile::tempdir().unwrap();
        let root = canon(t.path()).join("r");
        init(&root);
        std::fs::create_dir_all(root.join("sub")).unwrap();
        let link = canon(t.path()).join("link");
        std::os::unix::fs::symlink(&root, &link).unwrap();
        // Git resolves the entries of GIT_CEILING_DIRECTORIES: the link is the repository root
        assert!(discover_with(&root.join("sub"), &[link], &[]).is_none());
    }

    #[test]
    fn relative_ceiling_entries_are_ignored() {
        let v = OsString::from("sub::/abs/dir:rel/x");
        assert_eq!(parse_ceilings(&v), vec![PathBuf::from("/abs/dir")]);
        let t = tempfile::tempdir().unwrap();
        let root = canon(t.path());
        init(&root);
        std::fs::create_dir_all(root.join("sub")).unwrap();
        let rel = OsString::from("sub");
        assert!(discover_in(&root.join("sub"), Some(&rel)).is_some());
    }

    #[test]
    fn the_environment_ceiling_is_read() {
        let t = tempfile::tempdir().unwrap();
        let root = canon(t.path());
        init(&root);
        std::fs::create_dir_all(root.join("sub")).unwrap();
        let ceil = std::ffi::OsString::from(root.as_os_str());
        assert!(discover_in(&root.join("sub"), Some(&ceil)).is_none());
        assert!(discover_in(&root.join("sub"), None).is_some());
    }

    fn v(s: &[&str]) -> Vec<String> {
        s.iter().map(|x| x.to_string()).collect()
    }

    #[test]
    fn safe_directory_rules() {
        let w = Path::new("/srv/repo");
        assert!(!safe_directory_allows(&[], w));
        assert!(safe_directory_allows(&v(&["/srv/repo"]), w));
        assert!(safe_directory_allows(&v(&["/srv/repo/"]), w));
        assert!(!safe_directory_allows(&v(&["/srv/other"]), w));
        assert!(!safe_directory_allows(&v(&["/srv"]), w));
        assert!(!safe_directory_allows(&v(&["/srv/repo2"]), w));
        assert!(safe_directory_allows(&v(&["*"]), w));
        assert!(safe_directory_allows(&v(&["/srv/*"]), w));
        assert!(!safe_directory_allows(&v(&["/srv/repo/*"]), w));
        assert!(!safe_directory_allows(&v(&["/other/*"]), w));
        // an empty value resets the list, later values count again
        assert!(!safe_directory_allows(&v(&["/srv/repo", ""]), w));
        assert!(safe_directory_allows(&v(&["/x", "", "/srv/repo"]), w));
    }

    #[test]
    fn a_foreign_owner_needs_safe_directory() {
        let t = tempfile::tempdir().unwrap();
        let root = canon(t.path());
        init(&root);
        let ok = |trust, safe: &[&str]| {
            let repo = gix::open(&root).unwrap();
            trusted(&repo, trust, &v(safe))
        };
        assert!(ok(gix::sec::Trust::Full, &[]));
        assert!(!ok(gix::sec::Trust::Reduced, &[]));
        assert!(!ok(gix::sec::Trust::Reduced, &["/elsewhere"]));
        assert!(ok(gix::sec::Trust::Reduced, &[root.to_str().unwrap()]));
    }
}
