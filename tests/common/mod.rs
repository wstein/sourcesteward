//! Fixture repositories built with gix only (no git binary).
#![allow(dead_code)]
use std::path::{Path, PathBuf};
use std::sync::atomic::AtomicBool;

use gix::objs::tree::EntryKind;
use gix::ObjectId;

pub struct Fx {
    pub path: PathBuf,
    _tmp: tempfile::TempDir,
    clock: std::cell::Cell<i64>,
}

impl Fx {
    /// Fresh repository with a GitHub-looking origin and `origin/*` tracking refspec.
    pub fn new() -> Fx {
        let base = std::env::var_os("CLAUDE_JOB_DIR")
            .map(|d| PathBuf::from(d).join("tmp"))
            .filter(|p| p.is_dir())
            .unwrap_or_else(std::env::temp_dir);
        let tmp = tempfile::Builder::new()
            .prefix("stw-fx-")
            .tempdir_in(base)
            .unwrap();
        let path = tmp.path().join("repo");
        gix::init(&path).unwrap();
        let mut cfg = std::fs::OpenOptions::new()
            .append(true)
            .open(path.join(".git/config"))
            .unwrap();
        use std::io::Write;
        write!(
            cfg,
            "[remote \"origin\"]\n\turl = https://github.com/test/fixture\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n[user]\n\tname = T\n\temail = t@example.com\n"
        )
        .unwrap();
        Fx {
            path,
            _tmp: tmp,
            clock: std::cell::Cell::new(1_700_000_000),
        }
    }

    /// Remove the `[user]` section the fixture writes, so that no committer identity is configured.
    pub fn strip_identity(&self) {
        let cfg_path = self.path.join(".git/config");
        let cfg = std::fs::read_to_string(&cfg_path).unwrap();
        let stripped = cfg.replace("[user]\n\tname = T\n\temail = t@example.com\n", "");
        assert_ne!(stripped, cfg, "fixture has a [user] section");
        std::fs::write(cfg_path, stripped).unwrap();
    }

    /// Set `user.name` and `user.email` in the repository config.
    pub fn set_identity(&self, name: &str, email: &str) {
        use std::io::Write;
        let mut cfg = std::fs::OpenOptions::new()
            .append(true)
            .open(self.path.join(".git/config"))
            .unwrap();
        write!(cfg, "[user]\n\tname = {name}\n\temail = {email}\n").unwrap();
    }

    /// The identity (`Name <email>`) of the newest entry in `.git/logs/<name>`.
    pub fn last_reflog_identity(&self, name: &str) -> String {
        let log = std::fs::read_to_string(self.path.join(".git/logs").join(name)).unwrap();
        let line = log.lines().last().expect("reflog has an entry");
        let who = line.split('\t').next().unwrap();
        let (_, rest) = who.split_once(' ').unwrap();
        let (_, rest) = rest.split_once(' ').unwrap();
        let end = rest.rfind('>').unwrap();
        rest[..=end].to_string()
    }

    /// Point `origin` somewhere else.
    pub fn set_origin_url(&self, url: &str) {
        let cfg_path = self.path.join(".git/config");
        let cfg = std::fs::read_to_string(&cfg_path).unwrap();
        let out: Vec<String> = cfg
            .lines()
            .map(|l| {
                if l.trim_start().starts_with("url = ") {
                    format!("\turl = {url}")
                } else {
                    l.to_string()
                }
            })
            .collect();
        std::fs::write(cfg_path, out.join("\n") + "\n").unwrap();
    }

    pub fn repo(&self) -> gix::Repository {
        gix::open(&self.path).unwrap()
    }

    /// Commit on top of `parent` (or a root commit) applying `edits` (`None` content deletes the file).
    /// Writes objects only; no ref is touched.
    pub fn commit(
        &self,
        parent: Option<ObjectId>,
        edits: &[(&str, Option<&str>)],
        msg: &str,
    ) -> ObjectId {
        self.commit_with_parents(parent.into_iter().collect(), parent, edits, msg)
    }

    pub fn commit_with_parents(
        &self,
        parents: Vec<ObjectId>,
        tree_from: Option<ObjectId>,
        edits: &[(&str, Option<&str>)],
        msg: &str,
    ) -> ObjectId {
        let repo = self.repo();
        let base_tree = match tree_from {
            Some(p) => repo.find_commit(p).unwrap().tree_id().unwrap().detach(),
            None => repo.empty_tree().id,
        };
        let mut ed = repo.edit_tree(base_tree).unwrap();
        for (path, content) in edits {
            match content {
                Some(c) => {
                    let blob = repo.write_blob(c.as_bytes()).unwrap().detach();
                    ed.upsert(*path, EntryKind::Blob, blob).unwrap();
                }
                None => {
                    ed.remove(*path).unwrap();
                }
            }
        }
        let tree = ed.write().unwrap().detach();
        let t = self.clock.get() + 60;
        self.clock.set(t);
        let sig = gix::actor::Signature {
            name: "T".into(),
            email: "t@example.com".into(),
            time: gix::date::Time::new(t, 0),
        };
        let commit = gix::objs::Commit {
            tree,
            parents: parents.into(),
            author: sig.clone(),
            committer: sig,
            encoding: None,
            message: msg.into(),
            extra_headers: vec![],
        };
        repo.write_object(&commit).unwrap().detach()
    }

    /// Commit on top of `parent` that adds a submodule entry (gitlink) `path` pointing at `target`.
    pub fn commit_gitlink(
        &self,
        parent: ObjectId,
        path: &str,
        target: ObjectId,
        msg: &str,
    ) -> ObjectId {
        let repo = self.repo();
        let base_tree = repo
            .find_commit(parent)
            .unwrap()
            .tree_id()
            .unwrap()
            .detach();
        let mut ed = repo.edit_tree(base_tree).unwrap();
        ed.upsert(path, EntryKind::Commit, target).unwrap();
        let tree = ed.write().unwrap().detach();
        let sig = gix::actor::Signature {
            name: "T".into(),
            email: "t@example.com".into(),
            time: gix::date::Time::new(1_800_000_000, 0),
        };
        let commit = gix::objs::Commit {
            tree,
            parents: vec![parent].into(),
            author: sig.clone(),
            committer: sig,
            encoding: None,
            message: msg.into(),
            extra_headers: vec![],
        };
        repo.write_object(&commit).unwrap().detach()
    }

    pub fn set_ref(&self, name: &str, id: ObjectId) {
        self.repo()
            .reference(
                name,
                id,
                gix::refs::transaction::PreviousValue::Any,
                "fixture",
            )
            .unwrap();
    }

    pub fn branch(&self, name: &str, id: ObjectId) {
        self.set_ref(&format!("refs/heads/{name}"), id);
    }

    /// Remote-tracking ref `origin/<name>`.
    pub fn remote(&self, name: &str, id: ObjectId) {
        self.set_ref(&format!("refs/remotes/origin/{name}"), id);
    }

    /// `refs/remotes/origin/HEAD` -> `origin/<name>`.
    pub fn origin_head(&self, name: &str) {
        let p = self.path.join(".git/refs/remotes/origin");
        std::fs::create_dir_all(&p).unwrap();
        std::fs::write(p.join("HEAD"), format!("ref: refs/remotes/origin/{name}\n")).unwrap();
    }

    pub fn set_head(&self, branch: &str) {
        std::fs::write(
            self.path.join(".git/HEAD"),
            format!("ref: refs/heads/{branch}\n"),
        )
        .unwrap();
    }

    /// `branch.<b>.remote/merge` so that `origin/<b>` is the upstream.
    pub fn track(&self, branch: &str) {
        use std::io::Write;
        let mut cfg = std::fs::OpenOptions::new()
            .append(true)
            .open(self.path.join(".git/config"))
            .unwrap();
        write!(
            cfg,
            "[branch \"{branch}\"]\n\tremote = origin\n\tmerge = refs/heads/{branch}\n"
        )
        .unwrap();
    }

    /// Materialize HEAD into the working tree and write the index (like a fresh clone checkout).
    pub fn checkout_head(&self) {
        let repo = self.repo();
        let tree = repo.head_commit().unwrap().tree_id().unwrap().detach();
        let mut index = repo.index_from_tree(&tree).unwrap();
        let mut opts = repo
            .checkout_options(gix::worktree::stack::state::attributes::Source::IdMapping)
            .unwrap();
        opts.destination_is_initially_empty = false;
        opts.overwrite_existing = true;
        let objects = repo.objects.clone().into_arc().unwrap();
        gix::worktree::state::checkout(
            &mut index,
            repo.workdir().unwrap(),
            objects,
            &gix::progress::Discard,
            &gix::progress::Discard,
            &AtomicBool::new(false),
            opts,
        )
        .unwrap();
        index.write(Default::default()).unwrap();
    }

    pub fn write(&self, rel: &str, content: &str) {
        let p = self.path.join(rel);
        std::fs::create_dir_all(p.parent().unwrap()).unwrap();
        std::fs::write(p, content).unwrap();
    }

    pub fn read(&self, rel: &str) -> Option<String> {
        std::fs::read_to_string(self.path.join(rel)).ok()
    }

    pub fn exists(&self, rel: &str) -> bool {
        self.path.join(rel).symlink_metadata().is_ok()
    }

    pub fn tip(&self, branch: &str) -> ObjectId {
        stw::repo::resolve(&self.repo(), &format!("refs/heads/{branch}")).unwrap()
    }

    /// Stage a file the way `git add` would: blob into the object store, entry into the index.
    pub fn stage(&self, rel: &str) {
        let repo = self.repo();
        let data = std::fs::read(self.path.join(rel)).unwrap();
        let id = repo.write_blob(&data).unwrap().detach();
        let mut index = gix::index::File::at(
            repo.index_path(),
            repo.object_hash(),
            true,
            Default::default(),
        )
        .unwrap();
        let meta = gix::index::fs::Metadata::from_path_no_follow(&self.path.join(rel)).unwrap();
        let stat = gix::index::entry::Stat::from_fs(&meta).unwrap();
        index.remove_entries(|_, p, _| p == rel.as_bytes() as &[u8]);
        index.dangerously_push_entry(
            stat,
            id,
            gix::index::entry::Flags::empty(),
            gix::index::entry::Mode::FILE,
            rel.into(),
        );
        index.sort_entries();
        index.remove_tree();
        index.write(Default::default()).unwrap();
    }
}

/// A scratch directory next to the fixtures (under `$CLAUDE_JOB_DIR/tmp` when set).
pub fn scratch() -> tempfile::TempDir {
    let base = std::env::var_os("CLAUDE_JOB_DIR")
        .map(|d| PathBuf::from(d).join("tmp"))
        .filter(|p| p.is_dir())
        .unwrap_or_else(std::env::temp_dir);
    tempfile::Builder::new()
        .prefix("stw-scratch-")
        .tempdir_in(base)
        .unwrap()
}

/// A real linked worktree: registration, directory, files of `branch` checked out and the index written.
pub fn real_worktree(fx: &Fx, id: &str, branch: &str) -> PathBuf {
    let wt = fx.path.parent().unwrap().join(format!("wt-{id}"));
    real_worktree_at(fx, id, branch, &wt)
}

/// Like `real_worktree`, in the directory `wt` (for example `<repo>/.worktrees/<id>`).
pub fn real_worktree_at(fx: &Fx, id: &str, branch: &str, wt: &Path) -> PathBuf {
    let wt = add_worktree_at(fx, id, branch, wt, true, false);
    let repo = gix::open(&wt).unwrap();
    let tree = repo.head_commit().unwrap().tree_id().unwrap().detach();
    let mut index = repo.index_from_tree(&tree).unwrap();
    let mut opts = repo
        .checkout_options(gix::worktree::stack::state::attributes::Source::IdMapping)
        .unwrap();
    opts.destination_is_initially_empty = false;
    opts.overwrite_existing = true;
    let objects = repo.objects.clone().into_arc().unwrap();
    gix::worktree::state::checkout(
        &mut index,
        &wt,
        objects,
        &gix::progress::Discard,
        &gix::progress::Discard,
        &AtomicBool::new(false),
        opts,
    )
    .unwrap();
    index.write(Default::default()).unwrap();
    wt
}

pub fn lines(n: usize, tag: &str) -> String {
    (1..=n).map(|i| format!("{tag}{i}\n")).collect()
}

pub fn path_str(p: &Path) -> String {
    p.to_string_lossy().into_owned()
}

/// Register a linked worktree by hand (`.git/worktrees/<id>`), the way `git worktree add` leaves it.
/// `dir_exists == false` leaves the registration without its directory (what a deleted checkout looks like).
pub fn add_worktree(fx: &Fx, id: &str, branch: &str, dir_exists: bool, locked: bool) -> PathBuf {
    let wt = fx.path.parent().unwrap().join(format!("wt-{id}"));
    add_worktree_at(fx, id, branch, &wt, dir_exists, locked)
}

/// Like `add_worktree`, with the checkout in the directory `wt`.
pub fn add_worktree_at(
    fx: &Fx,
    id: &str,
    branch: &str,
    wt: &Path,
    dir_exists: bool,
    locked: bool,
) -> PathBuf {
    let wt = wt.to_path_buf();
    let reg = fx.path.join(".git/worktrees").join(id);
    std::fs::create_dir_all(&reg).unwrap();
    std::fs::write(
        reg.join("gitdir"),
        format!("{}\n", wt.join(".git").display()),
    )
    .unwrap();
    std::fs::write(reg.join("HEAD"), format!("ref: refs/heads/{branch}\n")).unwrap();
    std::fs::write(reg.join("commondir"), "../..\n").unwrap();
    if locked {
        std::fs::write(reg.join("locked"), "test lock\n").unwrap();
    }
    if dir_exists {
        std::fs::create_dir_all(&wt).unwrap();
        std::fs::write(wt.join(".git"), format!("gitdir: {}\n", reg.display())).unwrap();
    }
    wt
}

/// A fake GitHub REST API on 127.0.0.1: answers every request with `status` and `body`, and records the
/// request line and Authorization header of each request.
pub struct FakeApi {
    pub url: String,
    hits: std::sync::Arc<std::sync::Mutex<Vec<String>>>,
}

impl FakeApi {
    pub fn start(status: u16, body: &'static str) -> FakeApi {
        use std::io::{Read, Write};
        let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
        let url = format!("http://{}", listener.local_addr().unwrap());
        let hits = std::sync::Arc::new(std::sync::Mutex::new(Vec::new()));
        let seen = hits.clone();
        std::thread::spawn(move || {
            for stream in listener.incoming() {
                let Ok(mut s) = stream else { continue };
                let mut buf = Vec::new();
                let mut chunk = [0u8; 1024];
                while !buf.windows(4).any(|w| w == b"\r\n\r\n") {
                    match s.read(&mut chunk) {
                        Ok(0) | Err(_) => break,
                        Ok(n) => buf.extend_from_slice(&chunk[..n]),
                    }
                }
                let text = String::from_utf8_lossy(&buf).into_owned();
                let auth = text
                    .lines()
                    .find(|l| l.to_ascii_lowercase().starts_with("authorization:"))
                    .unwrap_or("");
                seen.lock()
                    .unwrap()
                    .push(format!("{} | {auth}", text.lines().next().unwrap_or("")));
                let resp = format!(
                    "HTTP/1.1 {status} X\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
                    body.len()
                );
                let _ = s.write_all(resp.as_bytes());
            }
        });
        FakeApi { url, hits }
    }

    pub fn hits(&self) -> Vec<String> {
        self.hits.lock().unwrap().clone()
    }
}

impl Fx {
    /// Like `commit`, with explicit author and committer (`(name, email)`).
    pub fn commit_by(
        &self,
        parent: Option<ObjectId>,
        edits: &[(&str, Option<&str>)],
        msg: &str,
        author: (&str, &str),
        committer: (&str, &str),
    ) -> ObjectId {
        let plain = self.commit(parent, edits, msg);
        let repo = self.repo();
        let mut c = repo
            .find_commit(plain)
            .unwrap()
            .decode()
            .unwrap()
            .to_owned()
            .unwrap();
        let time = c.author.time;
        let sig = |(n, e): (&str, &str)| gix::actor::Signature {
            name: n.into(),
            email: e.into(),
            time,
        };
        c.author = sig(author);
        c.committer = sig(committer);
        repo.write_object(&c).unwrap().detach()
    }
}

/// Stdout of a successful `stw` run, independent of the developer's token and workspace.
pub fn stw_stdout(args: &[&str]) -> String {
    let o = std::process::Command::new(env!("CARGO_BIN_EXE_stw"))
        .env_remove("GH_TOKEN")
        .env_remove("GITHUB_TOKEN")
        .env_remove("STW_ROOT")
        .env("HOME", "/nonexistent")
        .env("STW_KEYCHAIN_SERVICE", "stw-test-no-such-service")
        .args(args)
        .output()
        .unwrap();
    assert_eq!(o.status.code(), Some(0), "{args:?}");
    String::from_utf8(o.stdout).unwrap()
}

pub fn status_fixture() -> Fx {
    let fx = Fx::new();
    let c0 = fx.commit(None, &[("a.txt", Some("a\n"))], "c0");
    fx.branch("main", c0);
    fx.remote("main", c0);
    fx.origin_head("main");
    fx.track("main");
    fx.set_head("main");
    fx.checkout_head();
    fx
}

/// Pretty JSON of `stw status` for `root_arg`; the fixture root is redacted.
pub fn status_json(fx: &Fx, root_arg: &str) -> String {
    let out = stw_stdout(&["status", "--root", root_arg, "--offline", "--json"]);
    let pretty =
        serde_json::to_string_pretty(&serde_json::from_str::<serde_json::Value>(&out).unwrap())
            .unwrap();
    // stw prints the canonical root; the fixture path may run through a symlink
    let canon = std::fs::canonicalize(&fx.path).unwrap();
    pretty.replace(&path_str(&canon), "[ROOT]")
}
