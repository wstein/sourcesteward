//! `stw add OWNER/REPO [PATH]`: bind an EXISTING GitHub repository as a project (git submodule) of a Workspace.
//!
//! Decision (chartroom, 2026-11-08): `stw` never creates repositories (that stays in `whr-add`). Steps:
//! 1. `gix` clones `https://github.com/OWNER/REPO.git` into `PATH`, a directory this run creates itself, without
//!    writing a work tree. The token reaches only the transport, through the token-only credentials closure
//!    (`sync::fetch::offer_credentials`); no `git` process ever sees it. The user's git config is not read
//!    for the clone.
//! 2. The clone is verified before any file is written to its work tree: its own config has no key that runs a
//!    program, `origin` is exactly the canonical URL, it has a commit, and no attribute file of the tree and no
//!    `info/attributes` sets a `filter`.
//! 3. `git reset --hard` writes the work tree; `git submodule add -- <url> <path>` (local, no transport) and
//!    `git submodule absorbgitdirs -- <path>` bind it; one commit contains `.gitmodules` and the gitlink only.
//!    All of it runs through `git_runner` (`via: git`).
//!
//! A failure before the commit restores what this run changed: the clone directory and the directories this run
//! created, `.gitmodules`, the index, the Workspace config and `.git/modules/<path>`.
//!
//! Boundaries (the spike's `spikes/stw/add/README.md` has the evidence): only a Workspace whose `.git` is a
//! directory is accepted (no linked worktree); `.gitmodules` attributes are checked by the lines of
//! `.gitattributes` and `info/attributes` that can match it; refusals of `filter=` repositories (git-lfs,
//! git-crypt) stay until the owner decides an exception.
use std::path::{Component, Path, PathBuf};
use std::sync::atomic::AtomicBool;

use crate::error::{Error, Result};
use crate::git_runner::{self, Op, Runner};

/// A GitHub owner or repository name: `[A-Za-z0-9._-]+`, not starting with `-`, not `.` or `..`.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct RepoName(String);

impl RepoName {
    pub fn parse(s: &str) -> std::result::Result<RepoName, String> {
        let ok = !s.is_empty()
            && s != "."
            && s != ".."
            && !s.starts_with('-')
            && s.bytes()
                .all(|b| b.is_ascii_alphanumeric() || matches!(b, b'.' | b'_' | b'-'));
        if ok {
            Ok(RepoName(s.to_string()))
        } else {
            Err(format!(
                "{s:?} is not a GitHub name (letters, digits, '.', '_', '-'; not starting with '-')"
            ))
        }
    }
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

/// `https://github.com/OWNER/REPO.git`, the only URL `stw add` clones and records.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct CanonicalUrl(String);

impl CanonicalUrl {
    pub fn new(owner: &RepoName, repo: &RepoName) -> CanonicalUrl {
        CanonicalUrl(format!(
            "https://github.com/{}/{}.git",
            owner.as_str(),
            repo.as_str()
        ))
    }
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

/// Names git treats specially in a path component (any case, trailing dots and spaces ignored); `.git.*` is
/// refused as well, as in the spike.
const SPECIAL: &[&str] = &[
    ".git",
    ".gitmodules",
    ".gitattributes",
    ".gitignore",
    ".mailmap",
];

/// A path relative to the Workspace: `[A-Za-z0-9._/-]`, no empty, `.`, `..` or `-x` component, none of the
/// git-special names. One trailing `/` is dropped.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct SubPath(String);

impl SubPath {
    pub fn parse(s: &str) -> std::result::Result<SubPath, String> {
        let s = s.strip_suffix('/').unwrap_or(s);
        if s.is_empty() {
            return Err("the path is empty".into());
        }
        if s.starts_with('/') {
            return Err(format!(
                "{s:?} is absolute; the path is relative to the workspace"
            ));
        }
        if !s
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || matches!(b, b'.' | b'_' | b'-' | b'/'))
        {
            return Err(format!(
                "{s:?} has characters outside letters, digits, '.', '_', '-' and '/'"
            ));
        }
        for c in s.split('/') {
            if c.is_empty() || c == "." || c == ".." || c.starts_with('-') {
                return Err(format!("{s:?} has an empty, '.', '..' or '-x' component"));
            }
            let t = c.trim_end_matches(['.', ' ']).to_ascii_lowercase();
            if SPECIAL.contains(&t.as_str()) || t.starts_with(".git.") {
                return Err(format!("{s:?}: {c:?} is a name git treats specially"));
            }
        }
        Ok(SubPath(s.to_string()))
    }
    pub fn as_str(&self) -> &str {
        &self.0
    }
    fn components(&self) -> impl Iterator<Item = &str> {
        self.0.split('/')
    }
}

/// What to add.
#[derive(Clone, Debug)]
pub struct Request {
    pub owner: RepoName,
    pub repo: RepoName,
    pub path: SubPath,
}

impl Request {
    pub fn url(&self) -> CanonicalUrl {
        CanonicalUrl::new(&self.owner, &self.repo)
    }
}

/// `OWNER/REPO` or `REPO` (the owner then comes from `default_owner`) and the optional PATH (default
/// `projects/REPO`).
pub fn parse_request(
    spec: &str,
    path: Option<&str>,
    default_owner: Option<&str>,
) -> Result<Request> {
    let (owner, repo) = match spec.split_once('/') {
        Some((o, r)) => (o.to_string(), r.to_string()),
        None => (
            default_owner
                .ok_or_else(|| {
                    Error::usage(
                        "no owner: pass OWNER/REPO (the origin of the workspace is not a GitHub repository)",
                    )
                })?
                .to_string(),
            spec.to_string(),
        ),
    };
    let owner = RepoName::parse(&owner).map_err(Error::usage)?;
    let repo = RepoName::parse(&repo).map_err(Error::usage)?;
    let path = match path {
        Some(p) => SubPath::parse(p),
        None => SubPath::parse(&format!("projects/{}", repo.as_str())),
    }
    .map_err(Error::usage)?;
    Ok(Request { owner, repo, path })
}

/// Where the clone comes from. The product clones with gix over https; tests supply a local repository.
pub trait Cloner {
    /// Create the repository of `url` in the empty directory `dest` WITHOUT writing a work tree.
    fn clone_into(&self, url: &CanonicalUrl, dest: &Path) -> std::result::Result<(), String>;
}

/// The product cloner: gix over https with the token-only credentials closure. The user's and the system git
/// config are not read (isolated): a global `url.<x>.insteadOf` must not turn the https URL into an ssh one
/// that gix would start `ssh` for.
pub struct GixCloner {
    /// `(user, token)` for `github.com`; without it only public repositories can be cloned.
    pub credentials: Option<(String, String)>,
}

impl Cloner for GixCloner {
    fn clone_into(&self, url: &CanonicalUrl, dest: &Path) -> std::result::Result<(), String> {
        let creds = self.credentials.clone();
        let shown = self.credentials.clone();
        let msg = |e: &dyn std::fmt::Display| clone_message(&e.to_string(), shown.as_ref());
        let mut prep = gix::clone::PrepareFetch::new(
            url.as_str(),
            dest,
            gix::create::Kind::WithWorktree,
            gix::create::Options::default(),
            gix::open::Options::isolated(),
        )
        .map_err(|e| msg(&e))?
        .configure_connection(move |c| {
            let creds = creds.clone();
            c.set_credentials(move |action| match &creds {
                Some((user, pass)) => crate::sync::fetch::offer_credentials(action, user, pass),
                None => Ok(None),
            });
            Ok(())
        });
        let (repo, _) = prep
            .fetch_only(gix::progress::Discard, &AtomicBool::new(false))
            .map_err(|e| msg(&e))?;
        drop(repo);
        Ok(())
    }
}

/// The text of a failed clone: credentials and token shapes removed, and gix's "No credentials were returned"
/// (an unknown repository and a refused token look the same to GitHub) said in words.
pub fn clone_message(raw: &str, credentials: Option<&(String, String)>) -> String {
    let secrets: Vec<String> = credentials.map(|(_, t)| t.clone()).into_iter().collect();
    let text = git_runner::redact(raw, &secrets);
    let text = if text.contains("No credentials were returned") {
        match credentials {
            Some(_) => "not found or no access (the token was not accepted for this repository)".to_string(),
            None => "not found or no access (no token found; private repositories need GH_TOKEN or GITHUB_TOKEN)".to_string(),
        }
    } else {
        text
    };
    text.chars().take(400).collect()
}

/// What a successful run did.
#[derive(Debug)]
pub struct Report {
    pub owner: String,
    pub repo: String,
    pub path: String,
    pub url: String,
    pub dry_run: bool,
    /// The new commit of the Workspace (None on a dry run).
    pub commit: Option<String>,
    /// The commit of the repository the gitlink records (None on a dry run).
    pub gitlink: Option<String>,
}

impl Report {
    /// The `--json` object. A dry run writes nothing (`read_only`) and starts no process (`via` and
    /// `clone_via` are null); otherwise the clone ran in gix and the local steps through the git runner.
    pub fn to_json(&self, root: &str) -> serde_json::Value {
        let via = |v: &'static str| (!self.dry_run).then_some(v);
        serde_json::json!({
            "schema_version": 2, "command": "add", "read_only": self.dry_run, "root": root,
            "owner": self.owner, "repo": self.repo, "path": self.path, "url": self.url,
            "dry_run": self.dry_run, "commit": self.commit, "gitlink": self.gitlink,
            "clone_via": via("gix"), "via": via(git_runner::VIA),
        })
    }
}

fn refused(msg: impl Into<String>) -> Error {
    Error::repository(msg)
}

/// Add `req` to the Workspace `ws` (an absolute path, the top level of a git repository).
pub fn add(
    ws: &Path,
    req: &Request,
    cloner: &dyn Cloner,
    runner: &Runner,
    dry_run: bool,
) -> Result<Report> {
    if ws.as_os_str().is_empty() || !ws.is_absolute() {
        return Err(Error::usage("the workspace path must be absolute"));
    }
    let gitdir = ws.join(".git");
    match gitdir.symlink_metadata() {
        Ok(m) if m.is_dir() => {}
        _ => {
            return Err(Error::usage(format!(
                "{} is not the top level of a git repository with a .git directory",
                ws.display()
            )))
        }
    }
    let url = req.url();
    let sub = ws.join(req.path.as_str());

    // identity first: nothing is touched without it
    let wsrepo =
        crate::repo::open(ws).map_err(|e| Error::usage(format!("{}: {e}", ws.display())))?;
    let identity = |k: &str| {
        wsrepo
            .config_snapshot()
            .string(k)
            .map(|v| v.to_string())
            .filter(|v| !v.trim().is_empty())
    };
    if identity("user.name").is_none() || identity("user.email").is_none() {
        return Err(Error::usage(
            "git has no user.name and user.email: set them in your git config (stw never sets or overrides an identity)",
        ));
    }

    git_runner::check_config_for_add(ws, false).map_err(|e| refused(e.to_string()))?;
    check_gitmodules_attributes(ws, runner)?;
    check_path(ws, &gitdir, &wsrepo, req)?;
    check_gitmodules_clean(ws, &wsrepo)?;

    if dry_run {
        return Ok(Report {
            owner: req.owner.as_str().into(),
            repo: req.repo.as_str().into(),
            path: req.path.as_str().into(),
            url: url.as_str().into(),
            dry_run: true,
            commit: None,
            gitlink: None,
        });
    }

    let mut undo = Undo::new(ws, &gitdir, req)?;
    match run_steps(ws, &gitdir, &sub, req, &url, cloner, runner, &mut undo) {
        Ok(report) => Ok(report),
        Err(e) => {
            let note = undo.rollback();
            Err(Error::new(e.kind(), format!("{e}; {note}")))
        }
    }
}

#[allow(clippy::too_many_arguments)]
fn run_steps(
    ws: &Path,
    gitdir: &Path,
    sub: &Path,
    req: &Request,
    url: &CanonicalUrl,
    cloner: &dyn Cloner,
    runner: &Runner,
    undo: &mut Undo,
) -> Result<Report> {
    // directories: parents may exist; the last component must be created by this run (ownership proof)
    undo.create_dirs(ws, req)?;
    cloner.clone_into(url, sub).map_err(|m| {
        Error::repository(format!(
            "cloning {} failed: {m}",
            git_runner::redact(url.as_str(), &[])
        ))
    })?;
    verify_clone(sub, url)?;
    let git = |dir: &Path, op: Op| {
        runner
            .run(dir, op)
            .map(|_| ())
            .map_err(|e| Error::repository(e.to_string()))
    };
    git(sub, Op::CheckoutClone)?;
    let gitlink = {
        let r = crate::repo::open_isolated(sub)?;
        r.head_id()?.to_string()
    };
    undo.mark_git_started();
    git(
        ws,
        Op::SubmoduleAddExisting {
            url: url.clone(),
            path: req.path.clone(),
        },
    )?;
    // a directory or symlink that appeared during the run: refuse; from here on `.git/modules/PATH` is ours
    modules_clear(gitdir, req)?;
    undo.own_modules();
    git(
        ws,
        Op::AbsorbGitDirs {
            path: req.path.clone(),
        },
    )?;
    ignore_worktrees(&gitdir.join("modules").join(req.path.as_str()))?;
    git(
        ws,
        Op::CommitSubmodule {
            name: req.repo.clone(),
            path: req.path.clone(),
        },
    )?;
    let commit = crate::repo::open(ws)?.head_id()?.to_string();
    Ok(Report {
        owner: req.owner.as_str().into(),
        repo: req.repo.as_str().into(),
        path: req.path.as_str().into(),
        url: url.as_str().into(),
        dry_run: false,
        commit: Some(commit),
        gitlink: Some(gitlink),
    })
}

/// `.worktrees/` into the submodule's local exclude (as `whr_ensure_worktrees_ignored`).
fn ignore_worktrees(moddir: &Path) -> Result<()> {
    let info = moddir.join("info");
    std::fs::create_dir_all(&info)?;
    let file = info.join("exclude");
    let mut text = std::fs::read_to_string(&file).unwrap_or_default();
    if !text.lines().any(|l| l.trim() == ".worktrees/") {
        if !text.is_empty() && !text.ends_with('\n') {
            text.push('\n');
        }
        text.push_str(".worktrees/\n");
        std::fs::write(&file, text)?;
    }
    Ok(())
}

// ---------------------------------------------------------------------------------------------- checks

/// Does an attributes file name the attribute `filter` anywhere (`filter`, `-filter`, `!filter`, `filter=x`,
/// also right after a closing quote, in a macro definition)? No pattern matching: any such line counts, which
/// refuses a little too much and cannot be fooled by a pattern form. Used for the files of a fresh clone,
/// where no work tree exists for `git check-attr` yet.
fn sets_filter(text: &str) -> bool {
    // git skips a UTF-8 byte order mark at the start of an attributes file
    let text = text.strip_prefix('\u{feff}').unwrap_or(text);
    for line in text.lines() {
        let line = line.trim().to_ascii_lowercase();
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        let mut from = 0;
        while let Some(i) = line[from..].find("filter") {
            let at = from + i;
            let before = line[..at].chars().next_back();
            let after = line[at + "filter".len()..].chars().next();
            let start_ok = before.is_none_or(|c| c.is_whitespace() || matches!(c, '"' | '-' | '!'));
            let end_ok = after.is_none_or(|c| c.is_whitespace() || c == '=');
            if start_ok && end_ok {
                return true;
            }
            from = at + "filter".len();
        }
    }
    false
}

/// What git would apply to `.gitmodules` when the commit is made: asked of git itself (`check-attr`, from the
/// work tree with its index fallback, and from the index alone), through the runner. Any value but
/// `unspecified` for `filter`, `diff` or `merge`, any other output and any failure refuses (fail closed); a
/// symlink as `.gitattributes` refuses too (git does not follow it and reads the index copy).
fn check_gitmodules_attributes(ws: &Path, runner: &Runner) -> Result<()> {
    if ws
        .join(".gitattributes")
        .symlink_metadata()
        .is_ok_and(|m| m.file_type().is_symlink())
    {
        return Err(refused(
            ".gitattributes is a symlink; git reads its index copy for .gitmodules and stw add does not run git with it",
        ));
    }
    for cached in [false, true] {
        let out = runner
            .run(ws, Op::CheckAttrGitmodules { cached })
            .map_err(|e| refused(format!("git check-attr failed: {e}")))?;
        let text = String::from_utf8_lossy(&out.stdout).into_owned();
        let mut seen = 0;
        for line in text.lines() {
            let mut it = line.rsplitn(3, ": ");
            let (Some(value), Some(attr), Some(path)) = (it.next(), it.next(), it.next()) else {
                return Err(refused(format!(
                    "git check-attr printed {line:?}; not understood"
                )));
            };
            if path != ".gitmodules" || !matches!(attr, "filter" | "diff" | "merge") {
                return Err(refused(format!(
                    "git check-attr printed {line:?}; not understood"
                )));
            }
            if value != "unspecified" {
                return Err(refused(format!(
                    "git applies {attr}: {value} for .gitmodules (an attributes file or info/attributes); stw add does not run git with it"
                )));
            }
            seen += 1;
        }
        if seen != 3 {
            return Err(refused(format!(
                "git check-attr printed {seen} of 3 attributes for .gitmodules; not understood"
            )));
        }
    }
    Ok(())
}

fn check_path(ws: &Path, gitdir: &Path, repo: &gix::Repository, req: &Request) -> Result<()> {
    let p = req.path.as_str();
    // no symlink in the existing part of PATH; PATH itself must not exist
    let mut cur = ws.to_path_buf();
    for c in req.path.components() {
        cur.push(c);
        match cur.symlink_metadata() {
            Ok(m) if m.file_type().is_symlink() => {
                return Err(refused(format!("{} is a symlink", cur.display())))
            }
            Ok(_) if cur == ws.join(p) => {
                return Err(refused(format!("{p} exists already; nothing was changed")))
            }
            Ok(m) if !m.is_dir() => {
                return Err(refused(format!("{} is not a directory", cur.display())))
            }
            Ok(_) => {}
            Err(_) => break,
        }
    }
    // not in the index (also not as a directory prefix)
    let index = repo.index_or_empty()?;
    let prefix = format!("{p}/");
    for e in index.entries() {
        let path = e.path(&index).to_string();
        if path == p || path.starts_with(&prefix) {
            return Err(refused(format!("{p} is in the index already")));
        }
        // a file in the index where a directory of PATH should be
        if p.starts_with(&format!("{path}/")) {
            return Err(refused(format!("{path} is a file in the index")));
        }
    }
    if crate::repo::gitmodules_paths(ws).iter().any(|s| s == p) {
        return Err(refused(format!("{p} is a submodule already (skipped)")));
    }
    modules_clear(gitdir, req)?;
    // no submodule section of PATH in the Workspace config
    let cfg = std::fs::read_to_string(gitdir.join("config")).unwrap_or_default();
    let needle = format!("[submodule \"{p}\"]");
    if cfg.lines().any(|l| l.trim() == needle) {
        return Err(refused(format!(
            "the workspace config has a [submodule \"{p}\"] section"
        )));
    }
    Ok(())
}

/// `.git/modules/PATH` must not exist and no component of it may be a symlink (the absorb writes there).
/// Checked up front and again right before the absorb.
fn modules_clear(gitdir: &Path, req: &Request) -> Result<()> {
    let p = req.path.as_str();
    let mut m = gitdir.join("modules");
    for c in std::iter::once("").chain(req.path.components()) {
        if !c.is_empty() {
            m.push(c);
        }
        if m.symlink_metadata().is_ok() {
            let sym = m
                .symlink_metadata()
                .is_ok_and(|x| x.file_type().is_symlink());
            if sym || m == gitdir.join("modules").join(p) {
                return Err(refused(format!(
                    "{} exists; remove it first if it is a leftover",
                    m.display()
                )));
            }
        }
    }
    Ok(())
}

/// `.gitmodules` must not be a symlink and must equal its index entry: the pathspec of the commit takes
/// the whole file along.
fn check_gitmodules_clean(ws: &Path, repo: &gix::Repository) -> Result<()> {
    let f = ws.join(".gitmodules");
    let meta = f.symlink_metadata();
    let index = repo.index_or_empty()?;
    let entry = index
        .entries()
        .iter()
        .find(|e| e.path(&index) == ".gitmodules");
    match (meta, entry) {
        (Err(_), None) => Ok(()),
        (Ok(m), _) if m.file_type().is_symlink() => Err(refused(".gitmodules is a symlink")),
        (Ok(_), Some(e)) => {
            let bytes = std::fs::read(&f)?;
            let id = gix::objs::compute_hash(repo.object_hash(), gix::object::Kind::Blob, &bytes)
                .map_err(|e| Error::repository(e.to_string()))?;
            if id == e.id {
                Ok(())
            } else {
                Err(refused(
                    ".gitmodules has uncommitted changes; the commit would take them along",
                ))
            }
        }
        _ => Err(refused(
            ".gitmodules and its index entry disagree (untracked or deleted); resolve that first",
        )),
    }
}

/// Checks on the fresh clone before its work tree is written.
fn verify_clone(sub: &Path, url: &CanonicalUrl) -> Result<()> {
    git_runner::check_config_for_add(sub, true).map_err(|e| refused(e.to_string()))?;
    let repo = crate::repo::open_isolated(sub)?;
    let origin = repo.find_remote("origin").ok().and_then(|r| {
        r.url(gix::remote::Direction::Fetch)
            .map(|u| u.to_bstring().to_string())
    });
    if origin.as_deref() != Some(url.as_str()) {
        return Err(refused(format!(
            "origin of the clone is {origin:?}, expected {}",
            url.as_str()
        )));
    }
    let commit = repo.head_commit().map_err(|_| {
        refused(
            "the repository has no commit (empty): make a first commit and push it, then run again",
        )
    })?;
    // info/attributes of the git dir and of the common dir
    for dir in [repo.git_dir(), repo.common_dir()] {
        if let Ok(t) = std::fs::read(dir.join("info/attributes")) {
            if sets_filter(&String::from_utf8_lossy(&t)) {
                return Err(filter_refusal("info/attributes"));
            }
        }
    }
    // every attribute file of the tree: `.gitattributes` in any case, the NTFS short name, any depth
    let tree = commit.tree()?;
    let mut rec = gix::traverse::tree::Recorder::default();
    tree.traverse().breadthfirst(&mut rec)?;
    for e in rec.records {
        if e.mode.is_tree() || e.mode.is_commit() {
            continue;
        }
        let name = e.filepath.rsplit(|b| *b == b'/').next().unwrap_or(b"");
        let lower = name.to_ascii_lowercase();
        let lower = String::from_utf8_lossy(&lower)
            .trim_end_matches(['.', ' '])
            .to_string();
        if lower != ".gitattributes" && lower != "gitatt~1" {
            continue;
        }
        let obj = repo.find_object(e.oid)?;
        if sets_filter(&String::from_utf8_lossy(&obj.data)) {
            return Err(filter_refusal(&e.filepath.to_string()));
        }
    }
    Ok(())
}

fn filter_refusal(file: &str) -> Error {
    refused(format!(
        "{file} of the repository sets a filter (git-lfs, git-crypt); stw add refuses it, because a filter driver would run on checkout"
    ))
}

// ---------------------------------------------------------------------------------------------- rollback

/// What HEAD is.
#[derive(Clone, Debug, PartialEq, Eq)]
enum Head {
    Unborn,
    Id(String),
    /// The repository or its HEAD cannot be read.
    Unreadable,
}

fn head_of(ws: &Path) -> Head {
    let Ok(repo) = crate::repo::open_isolated(ws) else {
        return Head::Unreadable;
    };
    let Ok(head) = repo.head() else {
        return Head::Unreadable;
    };
    if head.is_unborn() {
        return Head::Unborn;
    }
    match head.id() {
        Some(id) => Head::Id(id.to_string()),
        None => Head::Unreadable,
    }
}

/// What a run changed, and how to restore it. Only what this run created is removed.
struct Undo {
    ws: PathBuf,
    gitdir: PathBuf,
    modules: PathBuf,
    /// Taken right before the first git step that writes to the workspace.
    snapshot: Option<Snapshot>,
    created: Vec<PathBuf>,
    /// HEAD before the run (`None`: unborn). A different HEAD at rollback time means the commit exists: from
    /// there on nothing is rolled back, also when the git step that made it reported a failure.
    head_before: Head,
    /// `.git/modules/PATH` was absent right before the absorb, so what exists there afterwards is this run's.
    modules_owned: bool,
}

/// Bytes of the files the git steps change (`None`: the file did not exist).
struct Snapshot {
    gitmodules: Option<Vec<u8>>,
    index: Option<Vec<u8>>,
    config: Option<Vec<u8>>,
}

impl Undo {
    fn new(ws: &Path, gitdir: &Path, req: &Request) -> Result<Undo> {
        Ok(Undo {
            ws: ws.to_path_buf(),
            gitdir: gitdir.to_path_buf(),
            modules: gitdir.join("modules").join(req.path.as_str()),
            snapshot: None,
            created: Vec::new(),
            head_before: head_of(ws),
            modules_owned: false,
        })
    }

    /// Create the missing directories of PATH one by one (`create_dir`, never `create_dir_all`): the last
    /// one must not exist, which proves this run owns it.
    fn create_dirs(&mut self, ws: &Path, req: &Request) -> Result<()> {
        let mut cur = ws.to_path_buf();
        let n = req.path.components().count();
        for (i, c) in req.path.components().enumerate() {
            cur.push(c);
            match std::fs::create_dir(&cur) {
                Ok(()) => self.created.push(cur.clone()),
                Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists && i + 1 < n => {
                    if cur
                        .symlink_metadata()
                        .is_ok_and(|m| m.file_type().is_symlink())
                    {
                        return Err(refused(format!("{} is a symlink", cur.display())));
                    }
                }
                Err(e) => {
                    return Err(refused(format!("cannot create {}: {e}", cur.display())));
                }
            }
        }
        Ok(())
    }

    /// Save what the git steps are about to change, right before the first of them: a change another
    /// process made while the clone ran (a `git add` in another terminal) is part of the saved state.
    fn mark_git_started(&mut self) {
        let read = |p: PathBuf| std::fs::read(p).ok();
        self.snapshot = Some(Snapshot {
            gitmodules: read(self.ws.join(".gitmodules")),
            index: read(self.gitdir.join("index")),
            config: read(self.gitdir.join("config")),
        });
    }

    fn own_modules(&mut self) {
        self.modules_owned = true;
    }

    fn is_committed(&self, now: &Head) -> bool {
        *now != self.head_before
    }

    /// Restore. Returns a sentence for the error message.
    fn rollback(&mut self) -> String {
        let now = head_of(&self.ws);
        if now == Head::Unreadable || self.head_before == Head::Unreadable {
            return "HEAD unreadable: state left as is".to_string();
        }
        if self.is_committed(&now) {
            return "the commit was made; nothing was rolled back".to_string();
        }
        let mut problems = Vec::new();
        let restore = |p: PathBuf, old: &Option<Vec<u8>>, problems: &mut Vec<String>| {
            let r = match old {
                Some(b) => std::fs::write(&p, b),
                None => match std::fs::remove_file(&p) {
                    Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(()),
                    other => other,
                },
            };
            if let Err(e) = r {
                problems.push(format!("{}: {e}", p.display()));
            }
        };
        if let Some(snap) = &self.snapshot {
            restore(self.ws.join(".gitmodules"), &snap.gitmodules, &mut problems);
            restore(self.gitdir.join("index"), &snap.index, &mut problems);
            restore(self.gitdir.join("config"), &snap.config, &mut problems);
            // absent before the run (checked up front); symlinks were refused
            if self.modules_owned
                && self
                    .modules
                    .symlink_metadata()
                    .is_ok_and(|m| m.is_dir() && !m.file_type().is_symlink())
                && self.modules.starts_with(self.gitdir.join("modules"))
            {
                if let Err(e) = std::fs::remove_dir_all(&self.modules) {
                    problems.push(format!("{}: {e}", self.modules.display()));
                }
            }
        }
        // deepest first: the clone directory (this run created it) with its content, then empty parents
        for (i, d) in self.created.iter().enumerate().rev() {
            let is_last = i + 1 == self.created.len();
            let r = if is_last && d.symlink_metadata().is_ok_and(|m| m.is_dir()) {
                std::fs::remove_dir_all(d)
            } else {
                std::fs::remove_dir(d)
            };
            if let Err(e) = r {
                if e.kind() != std::io::ErrorKind::NotFound {
                    problems.push(format!("{}: {e}", d.display()));
                }
            }
        }
        if problems.is_empty() {
            "what this run created was removed, nothing else".to_string()
        } else {
            format!("restoring failed for: {}", problems.join("; "))
        }
    }
}

/// Reject a relative or non-normal path argument for the root (helper for the CLI).
pub fn absolute_root(root: &Path) -> Result<PathBuf> {
    let root = std::fs::canonicalize(root)
        .map_err(|e| Error::usage(format!("{}: {e}", root.display())))?;
    if root.components().any(|c| matches!(c, Component::ParentDir)) {
        return Err(Error::usage("the workspace root is not normalized"));
    }
    Ok(root)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_clone_scan_sees_a_filter_however_it_is_written() {
        for t in [
            "*.bin filter=lfs diff=lfs",
            "* text FILTER=x",
            "[attr]big filter=lfs",
            "\"*\"filter=lfs",
            ".gitmodules\"filter=x",
            "* -filter",
            "* !filter",
            "*\tfilter=x",
            "\u{feff}* filter=x",
        ] {
            assert!(sets_filter(t), "{t:?}");
        }
        for t in [
            "* text=auto\n# filter=x",
            "filter.txt text",
            "src/filter text",
            "*.filtered text",
        ] {
            assert!(!sets_filter(t), "{t:?}");
        }
    }

    #[test]
    fn an_unreadable_head_is_reported_as_such_and_nothing_is_rolled_back() {
        let t = tempfile::tempdir().unwrap();
        let ws = t.path().join("ws");
        std::fs::create_dir_all(ws.join(".git")).unwrap();
        std::fs::write(ws.join(".git/HEAD"), "garbage\n").unwrap();
        let req = Request {
            owner: RepoName::parse("o").unwrap(),
            repo: RepoName::parse("r").unwrap(),
            path: SubPath::parse("projects/r").unwrap(),
        };
        let mut undo = Undo::new(&ws, &ws.join(".git"), &req).unwrap();
        undo.create_dirs(&ws, &req).unwrap();
        let note = undo.rollback();
        assert!(note.contains("HEAD unreadable"), "{note}");
        assert!(!note.contains("commit was made"), "{note}");
        assert!(ws.join("projects/r").is_dir(), "left as it is");
    }
}
