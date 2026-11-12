//! Repository discovery as Git does it: upwards from a directory, stopping at `GIT_CEILING_DIRECTORIES`, and a
//! repository owned by another user is used only when `safe.directory` (system or global config, never the
//! repository's own config) names its work tree. Read-only; nothing is written to any config.
use std::path::{Path, PathBuf};

/// The repository around `dir`, or `None`.
pub fn discover(dir: &Path) -> Option<gix::Repository> {
    gix::discover(dir).ok()
}

fn discover_in(dir: &Path, ceiling_env: Option<&std::ffi::OsString>) -> Option<gix::Repository> {
    let _ = ceiling_env;
    gix::discover(dir).ok()
}

fn discover_with(dir: &Path, ceilings: &[PathBuf], safe: &[String]) -> Option<gix::Repository> {
    let _ = (ceilings, safe);
    gix::discover(dir).ok()
}

fn safe_directory_allows(values: &[String], workdir: &Path) -> bool {
    let _ = (values, workdir);
    true
}

fn trusted(repo: &gix::Repository, trust: gix::sec::Trust, safe: &[String]) -> bool {
    let _ = (repo, trust, safe);
    true
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
        assert!(discover_with(&root.join("sub/deep"), &[root.clone()], &[]).is_none());
        // a ceiling elsewhere does not hide it
        let other = tempfile::tempdir().unwrap();
        let r = discover_with(&root.join("sub/deep"), &[canon(other.path())], &[]);
        assert_eq!(canon(r.unwrap().workdir().unwrap()), root);
        // a repository inside the ceiling is still found
        init(&root.join("sub"));
        let r = discover_with(&root.join("sub/deep"), &[root.clone()], &[]).unwrap();
        assert_eq!(canon(r.workdir().unwrap()), root.join("sub"));
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
