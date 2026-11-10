//! `stw add`: an existing repository becomes a submodule of a workspace, in one local commit, and a refused or
//! failed run leaves the workspace as it was. The clone itself is replaced by a local cloner (no network); the
//! git steps (`reset`, `submodule add`, `absorbgitdirs`, `commit`) run through the real runner and the real git.
use std::cell::Cell;
use std::collections::BTreeMap;
use std::ffi::OsString;
use std::path::Path;

use crate::common::*;
use stw::add::{self, CanonicalUrl, Cloner, RepoName, Request, SubPath};
use stw::error::Kind;
use stw::git_runner::Runner;

/// Cloner over a closure that builds the repository in `dest`.
struct FnCloner<F: Fn(&CanonicalUrl, &Path) -> Result<(), String>> {
    f: F,
    calls: Cell<u32>,
}

impl<F: Fn(&CanonicalUrl, &Path) -> Result<(), String>> Cloner for FnCloner<F> {
    fn clone_into(&self, url: &CanonicalUrl, dest: &Path) -> Result<(), String> {
        self.calls.set(self.calls.get() + 1);
        (self.f)(url, dest)
    }
}

fn cloner<F: Fn(&CanonicalUrl, &Path) -> Result<(), String>>(f: F) -> FnCloner<F> {
    FnCloner {
        f,
        calls: Cell::new(0),
    }
}

fn copy_dir(from: &Path, to: &Path) {
    std::fs::create_dir_all(to).unwrap();
    for e in std::fs::read_dir(from).unwrap() {
        let e = e.unwrap();
        let (src, dst) = (e.path(), to.join(e.file_name()));
        if e.file_type().unwrap().is_dir() {
            copy_dir(&src, &dst);
        } else {
            std::fs::copy(&src, &dst).unwrap();
        }
    }
}

/// A source repository with one commit holding `files`; its `.git` is what the local cloner copies.
fn source(files: &[(&str, &str)]) -> (Fx, gix::ObjectId) {
    let fx = Fx::new();
    let edits: Vec<(&str, Option<&str>)> = files.iter().map(|(p, c)| (*p, Some(*c))).collect();
    let c = fx.commit(None, &edits, "first");
    fx.branch("main", c);
    fx.set_head("main");
    (fx, c)
}

fn req(owner: &str, repo: &str, path: &str) -> Request {
    Request {
        owner: RepoName::parse(owner).unwrap(),
        repo: RepoName::parse(repo).unwrap(),
        path: SubPath::parse(path).unwrap(),
    }
}

fn workspace() -> Fx {
    let fx = Fx::new();
    let c0 = fx.commit(None, &[("README", Some("ws\n"))], "c0");
    fx.branch("main", c0);
    fx.set_head("main");
    fx.checkout_head();
    fx
}

/// Runner over the real git, a scratch HOME (no global config, no filters) and a hostile caller environment.
fn runner(home: &Path) -> Runner {
    runner_with_path(home, &std::env::var("PATH").unwrap())
}

fn runner_with_path(home: &Path, path: &str) -> Runner {
    let osv = |k: &str, v: &str| (OsString::from(k), OsString::from(v));
    Runner::from_env(vec![
        osv("PATH", path),
        osv("HOME", home.to_str().unwrap()),
        osv("XDG_CONFIG_HOME", home.join(".config").to_str().unwrap()),
        osv("GIT_AUTHOR_NAME", "Evil"),
        osv("GIT_AUTHOR_EMAIL", "evil@example.com"),
        osv("GIT_COMMITTER_NAME", "Evil"),
        osv("GIT_COMMITTER_EMAIL", "evil@example.com"),
        osv("GIT_DIR", "/nonexistent"),
        osv("GIT_CONFIG_GLOBAL", "/nonexistent"),
    ])
}

/// Every file below `dir` except the object store, with its bytes; directories as `<dir>`.
fn fingerprint(dir: &Path) -> BTreeMap<String, Vec<u8>> {
    fn walk(root: &Path, dir: &Path, out: &mut BTreeMap<String, Vec<u8>>) {
        for e in std::fs::read_dir(dir).unwrap() {
            let p = e.unwrap().path();
            let rel = p.strip_prefix(root).unwrap().to_string_lossy().into_owned();
            let ft = p.symlink_metadata().unwrap().file_type();
            if rel == ".git/objects" {
                continue;
            }
            if ft.is_symlink() {
                out.insert(
                    rel,
                    std::fs::read_link(&p)
                        .unwrap()
                        .into_os_string()
                        .into_encoded_bytes(),
                );
            } else if ft.is_dir() {
                out.insert(format!("{rel}/"), Vec::new());
                walk(root, &p, out);
            } else {
                out.insert(rel, std::fs::read(&p).unwrap());
            }
        }
    }
    let mut m = BTreeMap::new();
    walk(dir, dir, &mut m);
    m
}

struct Env {
    ws: Fx,
    home: tempfile::TempDir,
}

fn env() -> Env {
    Env {
        ws: workspace(),
        home: scratch(),
    }
}

impl Env {
    fn run(&self, c: &dyn Cloner, r: &Request, dry: bool) -> stw::error::Result<add::Report> {
        add::add(&self.ws.path, r, c, &runner(self.home.path()), dry)
    }
}

fn local(src: &Fx, owner: &str, repo: &str) -> impl Cloner {
    let path = src.path.clone();
    let url = format!("https://github.com/{owner}/{repo}.git");
    cloner(move |u, dest| {
        assert_eq!(u.as_str(), url);
        clone_from_path(&path, dest, u.as_str());
        Ok(())
    })
}

/// Copy the source's git dir into `dest` (no work tree, no index) and point origin at `origin`.
fn clone_from_path(src: &Path, dest: &Path, origin: &str) {
    copy_dir(&src.join(".git"), &dest.join(".git"));
    let cfg = dest.join(".git/config");
    let text = std::fs::read_to_string(&cfg).unwrap();
    std::fs::write(cfg, text.replace("https://github.com/test/fixture", origin)).unwrap();
}

#[test]
fn an_existing_repository_becomes_a_submodule_in_one_local_commit() {
    let (src, tip) = source(&[("hello.txt", "hi\n"), ("docs/a.md", "a\n")]);
    let e = env();
    let before = e.ws.tip("main");
    let c = local(&src, "octo", "tool");
    let report = e
        .run(&c, &req("octo", "tool", "projects/tool"), false)
        .unwrap();

    assert_eq!(report.gitlink.as_deref(), Some(tip.to_string().as_str()));
    let repo = e.ws.repo();
    let head = repo.head_commit().unwrap();
    assert_eq!(
        report.commit.as_deref(),
        Some(head.id().to_string().as_str())
    );
    let parents: Vec<_> = head.parent_ids().collect();
    assert_eq!(parents.len(), 1, "exactly one new commit");
    assert_eq!(parents[0].detach(), before);
    assert_eq!(
        head.message_raw_sloppy().to_string().trim(),
        "add tool submodule"
    );
    // author and committer come from the config, never from the caller's GIT_* variables
    let (a, cm) = (head.author().unwrap(), head.committer().unwrap());
    assert_eq!(
        (a.name.to_string(), a.email.to_string()),
        ("T".into(), "t@example.com".into())
    );
    assert_eq!(
        (cm.name.to_string(), cm.email.to_string()),
        ("T".into(), "t@example.com".into())
    );

    // the commit changes .gitmodules and the gitlink only
    let paths = |c: &gix::Commit<'_>| -> BTreeMap<String, (bool, String)> {
        let mut rec = gix::traverse::tree::Recorder::default();
        c.tree().unwrap().traverse().breadthfirst(&mut rec).unwrap();
        rec.records
            .into_iter()
            .map(|r| {
                (
                    r.filepath.to_string(),
                    (r.mode.is_commit(), r.oid.to_string()),
                )
            })
            .collect()
    };
    let old = paths(&repo.find_commit(before).unwrap());
    let new = paths(&head);
    let added: Vec<&String> = new.keys().filter(|k| old.get(*k) != new.get(*k)).collect();
    assert_eq!(added, [".gitmodules", "projects", "projects/tool"]);
    assert_eq!(
        new["projects/tool"],
        (true, tip.to_string()),
        "gitlink at the clone's HEAD"
    );
    for k in old.keys() {
        assert_eq!(old[k], new[k], "{k} unchanged");
    }

    // .gitmodules, the work tree and the absorbed git dir
    let gm = e.ws.read(".gitmodules").unwrap();
    assert!(gm.contains("path = projects/tool"), "{gm}");
    assert!(
        gm.contains("url = https://github.com/octo/tool.git"),
        "{gm}"
    );
    assert_eq!(
        e.ws.read("projects/tool/hello.txt").as_deref(),
        Some("hi\n")
    );
    assert_eq!(e.ws.read("projects/tool/docs/a.md").as_deref(), Some("a\n"));
    assert_eq!(
        e.ws.read("projects/tool/.git").unwrap().trim(),
        "gitdir: ../../.git/modules/projects/tool"
    );
    assert!(e.ws.exists(".git/modules/projects/tool/HEAD"));
    assert!(e
        .ws
        .read(".git/modules/projects/tool/info/exclude")
        .unwrap()
        .contains(".worktrees/"));
    // the workspace is clean afterwards and stw sees the project
    assert_eq!(stw::repo::submodule_paths(&e.ws.path), ["projects/tool"]);
}

#[test]
fn a_dry_run_runs_the_checks_and_changes_nothing() {
    let e = env();
    let before = fingerprint(&e.ws.path);
    let c = cloner(|_, _| panic!("a dry run clones nothing"));
    let r = e
        .run(&c, &req("octo", "tool", "projects/tool"), true)
        .unwrap();
    assert!(r.dry_run && r.commit.is_none());
    assert_eq!(fingerprint(&e.ws.path), before);
    assert_eq!(c.calls.get(), 0);
    // the checks still apply
    e.ws.write("projects/tool/x", "x");
    assert!(e
        .run(&c, &req("octo", "tool", "projects/tool"), true)
        .is_err());
}

/// Run a refused add: it must fail with a message containing `needle`, leave the workspace byte for byte as
/// it was and leave no clone directory.
fn refuses(e: &Env, c: &dyn Cloner, r: &Request, needle: &str) {
    let before = fingerprint(&e.ws.path);
    let err = e.run(c, r, false).expect_err("must be refused");
    assert!(err.to_string().contains(needle), "{err}");
    assert_eq!(fingerprint(&e.ws.path), before, "workspace changed: {err}");
}

#[test]
fn a_tracked_filter_attribute_is_refused_before_the_work_tree_is_written() {
    for (name, content) in [
        (
            ".gitattributes",
            "*.bin filter=lfs diff=lfs merge=lfs -text\n",
        ),
        ("sub/dir/.GitAttributes", "* filter=crypt\n"),
        (".gitattributes", "[attr]secret filter=crypt\n* secret\n"),
    ] {
        let (src, _) = source(&[("a.txt", "a\n"), (name, content)]);
        let e = env();
        let c = local(&src, "octo", "tool");
        refuses(
            &e,
            &c,
            &req("octo", "tool", "projects/tool"),
            "sets a filter",
        );
    }
}

#[test]
fn an_info_attributes_filter_of_the_clone_is_refused() {
    let (src, _) = source(&[("a.txt", "a\n")]);
    let e = env();
    let p = src.path.clone();
    let c = cloner(move |u, dest| {
        clone_from_path(&p, dest, u.as_str());
        std::fs::create_dir_all(dest.join(".git/info")).unwrap();
        std::fs::write(dest.join(".git/info/attributes"), "* filter=x\n").unwrap();
        Ok(())
    });
    refuses(
        &e,
        &c,
        &req("octo", "tool", "projects/tool"),
        "sets a filter",
    );
}

#[test]
fn a_clone_with_another_origin_is_refused() {
    let (src, _) = source(&[("a.txt", "a\n")]);
    let e = env();
    let p = src.path.clone();
    let c = cloner(move |_, dest| {
        clone_from_path(&p, dest, "https://github.com/evil/fork.git");
        Ok(())
    });
    refuses(
        &e,
        &c,
        &req("octo", "tool", "projects/tool"),
        "origin of the clone",
    );
}

#[test]
fn an_empty_repository_is_refused_with_the_next_step() {
    let e = env();
    let c = cloner(|u, dest| {
        gix::init(dest).map_err(|e| e.to_string())?;
        let mut cfg = std::fs::OpenOptions::new()
            .append(true)
            .open(dest.join(".git/config"))
            .unwrap();
        use std::io::Write;
        write!(cfg, "[remote \"origin\"]\n\turl = {}\n", u.as_str()).unwrap();
        Ok(())
    });
    refuses(
        &e,
        &c,
        &req("octo", "tool", "projects/tool"),
        "make a first commit",
    );
}

#[test]
fn a_clone_config_that_runs_a_program_is_refused() {
    for section in [
        "[filter \"lfs\"]\n\tsmudge = evil\n",
        "[core]\n\tfsmonitor = evil\n",
        "[core]\n\tworktree = /elsewhere\n",
        "[submodule \"x\"]\n\tupdate = !evil\n",
        "[credential]\n\thelper = !evil\n",
    ] {
        let (src, _) = source(&[("a.txt", "a\n")]);
        let e = env();
        let p = src.path.clone();
        let c = cloner(move |u, dest| {
            clone_from_path(&p, dest, u.as_str());
            let mut cfg = std::fs::OpenOptions::new()
                .append(true)
                .open(dest.join(".git/config"))
                .unwrap();
            use std::io::Write;
            cfg.write_all(section.as_bytes()).unwrap();
            Ok(())
        });
        refuses(
            &e,
            &c,
            &req("octo", "tool", "projects/tool"),
            "git config sets",
        );
    }
}

#[test]
fn a_workspace_config_that_runs_a_program_is_refused_before_anything_is_cloned() {
    for section in [
        "[filter \"lfs\"]\n\tclean = evil\n",
        "[core]\n\tfsmonitor = evil\n",
        "[commit]\n\tgpgsign = true\n",
        "[gpg]\n\tprogram = evil\n",
        "[alias]\n\tci = !evil\n",
    ] {
        let e = env();
        e.ws.write(".git/config.add", "");
        let mut cfg = std::fs::OpenOptions::new()
            .append(true)
            .open(e.ws.path.join(".git/config"))
            .unwrap();
        use std::io::Write;
        cfg.write_all(section.as_bytes()).unwrap();
        let c = cloner(|_, _| panic!("nothing is cloned into a refused workspace"));
        refuses(
            &e,
            &c,
            &req("octo", "tool", "projects/tool"),
            "git config sets",
        );
        assert_eq!(c.calls.get(), 0);
    }
}

#[test]
fn a_workspace_attribute_on_gitmodules_is_refused() {
    for (file, text) in [
        (".gitattributes", "* filter=x\n"),
        (".gitattributes", ".gitmodules diff=evil\n"),
        (".git/info/attributes", "*.gitmodules merge=evil\n"),
    ] {
        let e = env();
        e.ws.write(file, text);
        let c = cloner(|_, _| panic!("not cloned"));
        refuses(
            &e,
            &c,
            &req("octo", "tool", "projects/tool"),
            "for .gitmodules",
        );
    }
    // an attribute for other files is fine
    let (src, _) = source(&[("a.txt", "a\n")]);
    let e = env();
    e.ws.write(".gitattributes", "*.png filter=lfs\n");
    let c = local(&src, "octo", "tool");
    e.run(&c, &req("octo", "tool", "projects/tool"), false)
        .unwrap();
}

#[test]
fn existing_paths_and_leftovers_are_refused_and_kept() {
    let (src, _) = source(&[("a.txt", "a\n")]);
    // a directory, a file, a symlink to an outside directory at PATH or above it
    let e = env();
    e.ws.write("projects/tool/keep.txt", "mine\n");
    refuses(
        &e,
        &local(&src, "octo", "tool"),
        &req("octo", "tool", "projects/tool"),
        "exists already",
    );

    let e = env();
    let out = scratch();
    std::fs::create_dir_all(e.ws.path.join("projects")).unwrap();
    std::os::unix::fs::symlink(out.path(), e.ws.path.join("projects/tool")).unwrap();
    refuses(
        &e,
        &local(&src, "octo", "tool"),
        &req("octo", "tool", "projects/tool"),
        "symlink",
    );
    assert!(
        std::fs::read_dir(out.path()).unwrap().next().is_none(),
        "the link target is untouched"
    );

    let e = env();
    let out = scratch();
    std::os::unix::fs::symlink(out.path(), e.ws.path.join("projects")).unwrap();
    refuses(
        &e,
        &local(&src, "octo", "tool"),
        &req("octo", "tool", "projects/tool"),
        "symlink",
    );
    assert!(std::fs::read_dir(out.path()).unwrap().next().is_none());

    // a leftover git dir of the submodule
    let e = env();
    e.ws.write(".git/modules/projects/tool/HEAD", "ref: refs/heads/main\n");
    refuses(
        &e,
        &local(&src, "octo", "tool"),
        &req("octo", "tool", "projects/tool"),
        "exists",
    );

    // a symlink in `.git/modules`
    let e = env();
    let out = scratch();
    std::os::unix::fs::symlink(out.path(), e.ws.path.join(".git/modules")).unwrap();
    refuses(
        &e,
        &local(&src, "octo", "tool"),
        &req("octo", "tool", "projects/tool"),
        "exists",
    );
    assert!(std::fs::read_dir(out.path()).unwrap().next().is_none());

    // a registered section and an entry in .gitmodules
    let e = env();
    e.ws.write(".git/config.tmp", "");
    std::fs::remove_file(e.ws.path.join(".git/config.tmp")).unwrap();
    let mut cfg = std::fs::OpenOptions::new()
        .append(true)
        .open(e.ws.path.join(".git/config"))
        .unwrap();
    use std::io::Write;
    cfg.write_all(b"[submodule \"projects/tool\"]\n\turl = https://x\n")
        .unwrap();
    refuses(
        &e,
        &local(&src, "octo", "tool"),
        &req("octo", "tool", "projects/tool"),
        "[submodule",
    );
}

#[test]
fn an_already_registered_submodule_and_a_dirty_gitmodules_are_refused() {
    let (src, _) = source(&[("a.txt", "a\n")]);
    // first add works, the second refuses as "already"
    let e = env();
    e.run(
        &local(&src, "octo", "tool"),
        &req("octo", "tool", "projects/tool"),
        false,
    )
    .unwrap();
    refuses(
        &e,
        &local(&src, "octo", "tool"),
        &req("octo", "tool", "projects/tool"),
        "exists already",
    );
    // a second project is fine and keeps .gitmodules committed
    e.run(
        &local(&src, "octo", "two"),
        &req("octo", "two", "projects/two"),
        false,
    )
    .unwrap();
    assert_eq!(
        stw::repo::submodule_paths(&e.ws.path),
        ["projects/tool", "projects/two"]
    );
    // uncommitted change of .gitmodules: refused, the change stays
    let gm = e.ws.read(".gitmodules").unwrap();
    e.ws.write(".gitmodules", &format!("{gm}# mine\n"));
    refuses(
        &e,
        &local(&src, "octo", "three"),
        &req("octo", "three", "projects/three"),
        "uncommitted",
    );
    // .gitmodules that is a symlink
    let e = env();
    e.run(
        &local(&src, "octo", "tool"),
        &req("octo", "tool", "projects/tool"),
        false,
    )
    .unwrap();
    std::fs::remove_file(e.ws.path.join(".gitmodules")).unwrap();
    std::os::unix::fs::symlink("README", e.ws.path.join(".gitmodules")).unwrap();
    refuses(
        &e,
        &local(&src, "octo", "three"),
        &req("octo", "three", "projects/three"),
        "symlink",
    );
}

#[test]
fn without_a_git_identity_nothing_is_touched() {
    let (src, _) = source(&[("a.txt", "a\n")]);
    let e = env();
    // an empty local value wins over any global one
    let cfg = e.ws.path.join(".git/config");
    let text = std::fs::read_to_string(&cfg)
        .unwrap()
        .replace("\tname = T\n", "\tname =\n");
    std::fs::write(&cfg, text).unwrap();
    let before = fingerprint(&e.ws.path);
    let c = local(&src, "octo", "tool");
    let err = e
        .run(&c, &req("octo", "tool", "projects/tool"), false)
        .unwrap_err();
    assert_eq!(err.kind(), Kind::Usage, "{err}");
    assert!(err.to_string().contains("user.name"), "{err}");
    assert_eq!(fingerprint(&e.ws.path), before);
}

#[test]
fn a_failed_clone_leaves_no_trace_not_even_created_parents() {
    let e = env();
    let c = cloner(|_, dest| {
        std::fs::write(dest.join("partial"), "x").unwrap();
        Err("connection refused".into())
    });
    refuses(
        &e,
        &c,
        &req("octo", "tool", "deep/er/tool"),
        "connection refused",
    );
    // an existing parent with content of its own stays
    let e = env();
    e.ws.write("projects/other/file", "mine\n");
    refuses(
        &e,
        &c,
        &req("octo", "tool", "projects/tool"),
        "connection refused",
    );
    assert!(e.ws.exists("projects/other/file"));
}

#[test]
fn a_failure_after_the_add_restores_gitmodules_index_config_and_the_git_dir() {
    let (src, _) = source(&[("a.txt", "a\n")]);
    // an existing .gitmodules with another project, committed
    let e = env();
    e.run(
        &local(&src, "octo", "first"),
        &req("octo", "first", "projects/first"),
        false,
    )
    .unwrap();
    let before = fingerprint(&e.ws.path);
    // the commit cannot update the branch: its ref is locked
    let lock = e.ws.path.join(".git/refs/heads/main.lock");
    let (p, l) = (src.path.clone(), lock.clone());
    let c = cloner(move |u, dest| {
        clone_from_path(&p, dest, u.as_str());
        std::fs::write(&l, "").unwrap();
        Ok(())
    });
    let err = e
        .run(&c, &req("octo", "two", "projects/two"), false)
        .unwrap_err();
    assert!(
        err.to_string()
            .contains("what this run created was removed"),
        "{err}"
    );
    std::fs::remove_file(&lock).unwrap();
    // git's own scratch file of the failed commit is not part of the workspace state
    let mut after = fingerprint(&e.ws.path);
    let mut before = before;
    after.remove(".git/COMMIT_EDITMSG");
    before.remove(".git/COMMIT_EDITMSG");
    assert_eq!(
        after.keys().collect::<Vec<_>>(),
        before.keys().collect::<Vec<_>>(),
        "files and directories as before"
    );
    for (k, v) in &before {
        assert_eq!(&after[k], v, "{k} restored");
    }
    assert!(!e.ws.exists(".git/modules/projects/two"));
    // the first project is untouched and a later run succeeds
    assert!(e.ws.exists("projects/first/.git"));
    e.run(
        &local(&src, "octo", "two"),
        &req("octo", "two", "projects/two"),
        false,
    )
    .unwrap();
}

#[test]
fn names_and_paths_are_validated() {
    for bad in ["", ".", "..", "-x", "a b", "a/b", "a;b", "ä"] {
        assert!(RepoName::parse(bad).is_err(), "{bad:?}");
    }
    for good in ["tool", ".github", "a.b_c-d", "x1"] {
        assert!(RepoName::parse(good).is_ok(), "{good}");
    }
    for bad in [
        "",
        "/abs",
        "a//b",
        "a/../b",
        "..",
        ".",
        "./a",
        "a/./b",
        "-x",
        "a/-x",
        ".git",
        ".GIT",
        ".git.",
        "a/.gitmodules",
        "a/.GitAttributes/b",
        ".gitignore",
        ".git.foo",
        "a/.GIT.x/b",
        ".mailmap",
        "a b",
        "a~1",
        "a\\b",
    ] {
        assert!(SubPath::parse(bad).is_err(), "{bad:?}");
    }
    for good in [
        "projects/tool",
        "tool",
        ".github",
        "a/.githubx",
        "projects/tool/",
    ] {
        assert!(SubPath::parse(good).is_ok(), "{good}");
    }
    assert_eq!(
        SubPath::parse("projects/tool/").unwrap().as_str(),
        "projects/tool"
    );
    // owner from the origin, default path
    let r = add::parse_request("tool", None, Some("octo")).unwrap();
    assert_eq!(
        (r.owner.as_str(), r.repo.as_str(), r.path.as_str()),
        ("octo", "tool", "projects/tool")
    );
    assert_eq!(r.url().as_str(), "https://github.com/octo/tool.git");
    let r = add::parse_request("a/b", Some("x/y"), None).unwrap();
    assert_eq!(r.path.as_str(), "x/y");
    assert!(add::parse_request("tool", None, None).is_err());
    assert!(add::parse_request("a/b/c", None, None).is_err());
}

#[test]
fn a_relative_or_non_repository_workspace_is_a_usage_error() {
    let c = cloner(|_, _| panic!("not cloned"));
    let home = scratch();
    let r = req("octo", "tool", "projects/tool");
    let e = add::add(Path::new("rel"), &r, &c, &runner(home.path()), false).unwrap_err();
    assert_eq!(e.kind(), Kind::Usage);
    let e = add::add(Path::new(""), &r, &c, &runner(home.path()), false).unwrap_err();
    assert_eq!(e.kind(), Kind::Usage);
    let e = add::add(home.path(), &r, &c, &runner(home.path()), false).unwrap_err();
    assert_eq!(e.kind(), Kind::Usage);
}

#[test]
fn other_staged_and_unstaged_changes_stay_out_of_the_commit_and_stay_as_they_were() {
    let (src, _) = source(&[("a.txt", "a\n")]);
    let e = env();
    e.ws.write("staged.txt", "staged\n");
    e.ws.stage("staged.txt");
    e.ws.write("README", "edited\n");
    e.ws.write("untracked.txt", "u\n");
    let report = e
        .run(
            &local(&src, "octo", "tool"),
            &req("octo", "tool", "projects/tool"),
            false,
        )
        .unwrap();
    let repo = e.ws.repo();
    let head = repo.head_commit().unwrap();
    assert_eq!(
        report.commit.as_deref(),
        Some(head.id().to_string().as_str())
    );
    let tree = head.tree().unwrap();
    assert!(
        tree.lookup_entry_by_path("staged.txt").unwrap().is_none(),
        "staged file not committed"
    );
    assert!(tree
        .lookup_entry_by_path("untracked.txt")
        .unwrap()
        .is_none());
    let readme = tree
        .lookup_entry_by_path("README")
        .unwrap()
        .unwrap()
        .object()
        .unwrap();
    assert_eq!(readme.data, b"ws\n", "unstaged edit not committed");
    let index = repo.index_or_empty().unwrap();
    assert!(
        index
            .entries()
            .iter()
            .any(|en| en.path(&index) == "staged.txt"),
        "still staged"
    );
    assert_eq!(e.ws.read("README").as_deref(), Some("edited\n"));
    assert_eq!(e.ws.read("untracked.txt").as_deref(), Some("u\n"));
}

/// A workspace attribute that attaches a filter to `.gitmodules` in any way git understands must refuse the
/// add before a git step that would run the driver. `git check-attr` answers; the workspace's global config
/// has a driver that leaves a marker file when it runs.
#[test]
fn every_way_to_attach_a_filter_to_gitmodules_is_refused_and_no_filter_runs() {
    type Setup = Box<dyn Fn(&Env)>;
    let attrs = |text: &'static str| -> Setup {
        Box::new(move |e: &Env| e.ws.write(".gitattributes", text))
    };
    let info = |text: &'static str| -> Setup {
        Box::new(move |e: &Env| e.ws.write(".git/info/attributes", text))
    };
    let commit_then_remove: Setup = Box::new(|e: &Env| {
        e.ws.write(".gitattributes", ".gitmodules filter=evil\n");
        e.ws.stage(".gitattributes");
        std::fs::remove_file(e.ws.path.join(".gitattributes")).unwrap();
    });
    let symlinked: Setup = Box::new(|e: &Env| {
        e.ws.write(".gitattributes", ".gitmodules filter=evil\n");
        e.ws.stage(".gitattributes");
        std::fs::remove_file(e.ws.path.join(".gitattributes")).unwrap();
        e.ws.write("harmless", "# harmless\n");
        std::os::unix::fs::symlink("harmless", e.ws.path.join(".gitattributes")).unwrap();
    });
    // (name, setup, must be refused); a name git does not match may only be harmless
    let cases: Vec<(&str, Setup, bool)> = vec![
        ("plain", attrs(".gitmodules filter=evil\n"), true),
        ("star", attrs("* filter=evil\n"), true),
        ("backslash", attrs("\\.gitmodules filter=evil\n"), true),
        ("bom", attrs("\u{feff}.gitmodules filter=evil\n"), true),
        ("bom star", attrs("\u{feff}* filter=evil\n"), true),
        (
            "quoted pattern",
            attrs("\".gitmodules\"filter=evil\n"),
            true,
        ),
        ("quoted star", attrs("\"*\"filter=evil\n"), true),
        (
            "octal escape",
            attrs("\"\\056gitmodules\" filter=evil\n"),
            true,
        ),
        (
            "double star twice",
            attrs("**/**/.gitmodules filter=evil\n"),
            true,
        ),
        (
            "leading slash double star",
            attrs("/**/**/.gitmodules filter=evil\n"),
            true,
        ),
        ("tab", attrs(".gitmodules\tfilter=evil\n"), true),
        (
            "macro",
            attrs("[attr]evil2 filter=evil\n.gitmodules evil2\n"),
            true,
        ),
        ("diff", attrs("\\.gitmodules diff=evil\n"), true),
        ("merge", attrs("*.gitmodules merge=evil\n"), true),
        ("info quoted", info("\"*\"filter=evil\n"), true),
        ("info backslash", info("\\.gitmodules filter=evil\n"), true),
        ("index only", commit_then_remove, true),
        ("symlink", symlinked, true),
        ("upper case name", attrs(".GITMODULES filter=evil\n"), false),
    ];
    for (name, setup, must_refuse) in cases {
        let (src, _) = source(&[("a.txt", "a\n")]);
        let e = env();
        let marker = e.home.path().join("filter-ran");
        std::fs::write(
            e.home.path().join(".gitconfig"),
            format!(
                "[filter \"evil\"]\n\tclean = touch {} && cat\n\tsmudge = touch {} && cat\n",
                marker.display(),
                marker.display()
            ),
        )
        .unwrap();
        setup(&e);
        let c = local(&src, "octo", "tool");
        let r = e.run(&c, &req("octo", "tool", "projects/tool"), false);
        assert!(!marker.exists(), "{name}: a filter program ran");
        if must_refuse {
            let err = r.expect_err(name);
            assert!(err.to_string().contains("for .gitmodules"), "{name}: {err}");
        }
    }
}

#[test]
fn a_filter_in_the_clones_attributes_is_found_however_it_is_written() {
    for text in [
        "*.bin filter=lfs\n",
        "\"*\"filter=lfs\n",
        "\".gitmodules\"filter=x\n",
        "* -filter\n",
        "* !filter\n",
        "*\tfilter=x\n",
        "\u{feff}* filter=x\n",
        "[attr]big filter=lfs\n",
        "* FILTER=x\n",
    ] {
        let (src, _) = source(&[("a.txt", "a\n"), ("sub/.gitattributes", text)]);
        let e = env();
        refuses(
            &e,
            &local(&src, "octo", "tool"),
            &req("octo", "tool", "projects/tool"),
            "sets a filter",
        );
    }
    for text in [
        "*.png text=auto\n",
        "# filter=x\n",
        "filter.txt text\n",
        "src/filter text\n",
    ] {
        let (src, _) = source(&[("a.txt", "a\n"), (".gitattributes", text)]);
        let e = env();
        e.run(
            &local(&src, "octo", "tool"),
            &req("octo", "tool", "projects/tool"),
            false,
        )
        .unwrap_or_else(|er| panic!("{text:?} refused: {er}"));
    }
}

#[test]
fn changes_made_while_the_clone_runs_survive_a_later_failure() {
    let (src, _) = source(&[("a.txt", "a\n")]);
    let e = env();
    let lock = e.ws.path.join(".git/refs/heads/main.lock");
    let (p, l, ws) = (src.path.clone(), lock.clone(), &e.ws);
    let c = cloner(move |u, dest| {
        clone_from_path(&p, dest, u.as_str());
        // another terminal: a `git add` and a config edit while the clone was running
        ws.write("staged-meanwhile.txt", "s\n");
        ws.stage("staged-meanwhile.txt");
        let mut cfg = std::fs::OpenOptions::new()
            .append(true)
            .open(ws.path.join(".git/config"))
            .unwrap();
        use std::io::Write;
        cfg.write_all(b"[note]\n\tmeanwhile = yes\n").unwrap();
        std::fs::write(&l, "").unwrap();
        Ok(())
    });
    let err = e
        .run(&c, &req("octo", "tool", "projects/tool"), false)
        .unwrap_err();
    assert!(err.to_string().contains("removed"), "{err}");
    std::fs::remove_file(&lock).unwrap();
    let repo = e.ws.repo();
    let index = repo.index_or_empty().unwrap();
    assert!(
        index
            .entries()
            .iter()
            .any(|en| en.path(&index) == "staged-meanwhile.txt"),
        "the staged file was dropped by the rollback"
    );
    assert!(e
        .ws
        .read(".git/config")
        .unwrap()
        .contains("meanwhile = yes"));
    assert!(!e.ws.exists("projects/tool"));
}

#[test]
fn a_git_dir_that_appears_during_the_run_is_refused_and_never_removed() {
    let (src, _) = source(&[("a.txt", "a\n")]);
    // a symlink at `.git/modules` that appears after the first check: the absorb must not write through it
    let e = env();
    let out = scratch();
    let (p, ws, o) = (
        src.path.clone(),
        e.ws.path.clone(),
        out.path().to_path_buf(),
    );
    let c = cloner(move |u, dest| {
        clone_from_path(&p, dest, u.as_str());
        std::os::unix::fs::symlink(&o, ws.join(".git/modules")).unwrap();
        Ok(())
    });
    let err = e
        .run(&c, &req("octo", "tool", "projects/tool"), false)
        .unwrap_err();
    assert!(err.to_string().contains("exists"), "{err}");
    assert!(
        std::fs::read_dir(out.path()).unwrap().next().is_none(),
        "the absorb wrote through the symlink"
    );
    assert!(!e.ws.exists("projects/tool"));
    // a foreign directory at `.git/modules/PATH` that appears during the run stays
    let e = env();
    let (p, ws) = (src.path.clone(), e.ws.path.clone());
    let c = cloner(move |u, dest| {
        clone_from_path(&p, dest, u.as_str());
        std::fs::create_dir_all(ws.join(".git/modules/projects/tool")).unwrap();
        std::fs::write(ws.join(".git/modules/projects/tool/theirs"), "x").unwrap();
        Ok(())
    });
    let err = e
        .run(&c, &req("octo", "tool", "projects/tool"), false)
        .unwrap_err();
    assert!(err.to_string().contains("exists"), "{err}");
    assert_eq!(
        e.ws.read(".git/modules/projects/tool/theirs").as_deref(),
        Some("x"),
        "a directory this run did not create was removed"
    );
}

#[test]
fn the_json_report_says_what_ran_and_whether_it_wrote() {
    let (src, _) = source(&[("a.txt", "a\n")]);
    let e = env();
    let dry = e
        .run(
            &local(&src, "octo", "tool"),
            &req("octo", "tool", "projects/tool"),
            true,
        )
        .unwrap()
        .to_json("/ws");
    assert_eq!(dry["read_only"], true);
    assert!(dry["via"].is_null() && dry["clone_via"].is_null());
    let done = e
        .run(
            &local(&src, "octo", "tool"),
            &req("octo", "tool", "projects/tool"),
            false,
        )
        .unwrap()
        .to_json("/ws");
    assert_eq!(done["read_only"], false);
    assert_eq!(done["via"], "git");
    assert_eq!(done["clone_via"], "gix");
    assert_eq!(done["command"], "add");
}

#[test]
fn an_index_entry_below_path_refuses_even_without_the_directory() {
    let e = env();
    e.ws.write("projects/tool/x", "x\n");
    e.ws.stage("projects/tool/x");
    std::fs::remove_dir_all(e.ws.path.join("projects")).unwrap();
    refuses(
        &e,
        &cloner(|_, _| panic!("refused before the clone")),
        &req("octo", "tool", "projects/tool"),
        "in the index already",
    );
    // a file in the index where a directory of PATH should be
    let e = env();
    e.ws.write("projects", "a file\n");
    e.ws.stage("projects");
    std::fs::remove_file(e.ws.path.join("projects")).unwrap();
    refuses(
        &e,
        &cloner(|_, _| panic!("refused before the clone")),
        &req("octo", "tool", "projects/tool"),
        "is a file in the index",
    );
}

#[test]
fn clone_errors_hide_the_token_and_say_what_a_missing_credential_means() {
    let creds = Some(("x-access-token".to_string(), "tok_SECRET_123".to_string()));
    let m = add::clone_message("request to tok_SECRET_123 failed", creds.as_ref());
    assert!(!m.contains("tok_SECRET_123"), "{m}");
    let m = add::clone_message("No credentials were returned at all as if x", None);
    assert!(m.contains("not found or no access"), "{m}");
    assert!(m.contains("no token"), "{m}");
    let m = add::clone_message(
        "No credentials were returned at all as if x",
        creds.as_ref(),
    );
    assert!(m.contains("not found or no access"), "{m}");
    assert!(!m.contains("no token"), "{m}");
    assert_eq!(
        add::clone_message("connection refused", None),
        "connection refused"
    );
}

/// A `git` that does what git does and then exits 1 after a `commit`: the commit exists although the step
/// failed (a timeout kill after the ref was written looks the same). Nothing may be rolled back then.
#[test]
fn a_commit_that_exists_is_never_rolled_back_even_when_git_reports_failure() {
    let (src, _) = source(&[("a.txt", "a\n")]);
    let e = env();
    let real = std::env::split_paths(&std::env::var_os("PATH").unwrap())
        .map(|d| d.join("git"))
        .find(|p| p.is_file())
        .unwrap();
    let bin = scratch();
    let fake = bin.path().join("git");
    std::fs::write(
        &fake,
        format!(
            "#!/bin/bash\nc=\nfor a in \"$@\"; do [ \"$a\" = commit ] && c=1; done\n{} \"$@\"\nrc=$?\n[ -n \"$c\" ] && exit 1\nexit $rc\n",
            real.display()
        ),
    )
    .unwrap();
    std::fs::set_permissions(&fake, std::os::unix::fs::PermissionsExt::from_mode(0o755)).unwrap();
    let before = e.ws.tip("main");
    let r = runner_with_path(
        e.home.path(),
        &format!(
            "{}:{}",
            bin.path().display(),
            std::env::var("PATH").unwrap()
        ),
    );
    let c = local(&src, "octo", "tool");
    let err = add::add(
        &e.ws.path,
        &req("octo", "tool", "projects/tool"),
        &c,
        &r,
        false,
    )
    .unwrap_err();
    assert!(err.to_string().contains("nothing was rolled back"), "{err}");
    let repo = e.ws.repo();
    let head = repo.head_commit().unwrap();
    assert_ne!(head.id().detach(), before, "the commit exists");
    assert!(e.ws.exists("projects/tool/.git"));
    assert!(e.ws.read(".gitmodules").unwrap().contains("projects/tool"));
    assert!(e.ws.exists(".git/modules/projects/tool/HEAD"));
}
