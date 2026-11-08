//! `stw tidy apply` and `stw tidy restore`.
//!
//! `apply` deletes local branches whose plan decision is `delete`, removes clean linked worktrees of such
//! branches, and removes worktree registrations whose directory is gone. It never touches `review`/`keep`
//! branches, the branch checked out in a worktree that stays, or any remote state.
//!
//! Order for a repository: verify everything against the fresh plan, write the manifest (outside
//! the repository, fsync'ed), remove dead registrations, remove worktrees, delete branches. A branch ref is
//! deleted only if it still points at the planned commit. `restore` re-creates branches from the manifest.
use std::io::Write;
use std::path::{Path, PathBuf};

use gix::refs::transaction::{Change, PreviousValue, RefEdit, RefLog};
use gix::refs::Target;
use serde_json::{json, Value};

use crate::error::{Error, Result};
use crate::output::*;
use crate::repo::worktrees::{self, Linked, Worktrees};
use crate::repo::{self, RepoRef};
use crate::tidy::{BranchPlan, RepoPlan};

// ---------------------------------------------------------------- manifest

/// Where manifests go when `--manifest-dir` is not given: the user's data directory.
/// macOS: `~/Library/Application Support/stw/manifests`; else `$XDG_DATA_HOME/stw/manifests` or
/// `~/.local/share/stw/manifests`.
pub fn default_manifest_dir() -> Option<PathBuf> {
    let var = |k: &str| {
        std::env::var_os(k)
            .filter(|v| !v.is_empty())
            .map(PathBuf::from)
    };
    if cfg!(target_os = "macos") {
        return var("HOME").map(|h| h.join("Library/Application Support/stw/manifests"));
    }
    var("XDG_DATA_HOME")
        .or_else(|| var("HOME").map(|h| h.join(".local/share")))
        .map(|d| d.join("stw/manifests"))
}

/// Unique, sortable run ID: `<milliseconds, 13 digits>-<pid>-<counter>`.
pub fn new_run_id() -> String {
    use std::sync::atomic::{AtomicU32, Ordering};
    static N: AtomicU32 = AtomicU32::new(0);
    let ms = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_millis())
        .unwrap_or(0);
    format!(
        "{ms:013}-{}-{}",
        std::process::id(),
        N.fetch_add(1, Ordering::SeqCst)
    )
}

#[derive(Clone, Debug)]
pub struct ManifestEntry {
    pub run: String,
    pub repo: String,
    pub branch: String,
    pub sha: String,
    pub proof: String,
    pub time: i64,
    pub worktree: Option<String>,
}

impl ManifestEntry {
    fn to_line(&self) -> String {
        let v = json!({"manifest_version": 1, "run": self.run, "repo": self.repo, "branch": self.branch,
                       "sha": self.sha, "proof": self.proof, "time": self.time, "worktree": jopt(self.worktree.as_deref())});
        serde_json::to_string(&v).expect("JSON value serializes")
    }
    fn from_value(v: &Value) -> Option<ManifestEntry> {
        let s = |k: &str| v.get(k).and_then(Value::as_str).map(str::to_string);
        Some(ManifestEntry {
            run: s("run")?,
            repo: s("repo")?,
            branch: s("branch")?,
            sha: s("sha")?,
            proof: s("proof").unwrap_or_default(),
            time: v.get("time").and_then(Value::as_i64).unwrap_or(0),
            worktree: s("worktree"),
        })
    }
}

/// Append `entries` to `<dir>/<run>.jsonl` and fsync the file and the directory. Nothing is deleted before this
/// returned `Ok`.
fn write_manifest(dir: &Path, run: &str, entries: &[ManifestEntry]) -> Result<PathBuf> {
    let fail = |what: &str, e: std::io::Error| {
        Error::repository(format!("manifest {what} {}: {e}", dir.display()))
    };
    std::fs::create_dir_all(dir).map_err(|e| fail("directory", e))?;
    let path = dir.join(format!("{run}.jsonl"));
    let mut f = std::fs::OpenOptions::new()
        .create(true)
        .append(true)
        .open(&path)
        .map_err(|e| fail("file in", e))?;
    let mut text = String::new();
    for e in entries {
        text += &e.to_line();
        text.push('\n');
    }
    f.write_all(text.as_bytes())
        .map_err(|e| fail("write in", e))?;
    f.sync_all().map_err(|e| fail("fsync in", e))?;
    #[cfg(unix)]
    std::fs::File::open(dir)
        .and_then(|d| d.sync_all())
        .map_err(|e| fail("directory fsync of", e))?;
    Ok(path)
}

/// Entries of the manifests in `dir`: the files of `run`, or of the newest run.
fn read_manifest(dir: &Path, run: Option<&str>) -> Result<(String, Vec<ManifestEntry>)> {
    let mut files: Vec<PathBuf> = match std::fs::read_dir(dir) {
        Ok(rd) => rd
            .filter_map(|e| e.ok().map(|e| e.path()))
            .filter(|p| p.extension().is_some_and(|x| x == "jsonl"))
            .collect(),
        Err(e) => {
            return Err(Error::usage(format!(
                "no manifests: cannot read {}: {e}",
                dir.display()
            )))
        }
    };
    files.sort();
    let stem = |p: &Path| p.file_stem().map(|s| s.to_string_lossy().into_owned());
    let file = match run {
        Some(r) => files.iter().find(|p| stem(p).as_deref() == Some(r)),
        None => files.last(),
    }
    .ok_or_else(|| match run {
        Some(r) => Error::usage(format!("no manifest for run {r} in {}", dir.display())),
        None => Error::usage(format!("no manifest in {}", dir.display())),
    })?;
    let text = std::fs::read_to_string(file)?;
    let mut out = Vec::new();
    for (n, line) in text
        .lines()
        .enumerate()
        .filter(|(_, l)| !l.trim().is_empty())
    {
        let v: Value = serde_json::from_str(line).map_err(|e| {
            Error::repository(format!("{}:{}: not JSON: {e}", file.display(), n + 1))
        })?;
        out.push(ManifestEntry::from_value(&v).ok_or_else(|| {
            Error::repository(format!(
                "{}:{}: entry lacks run, repo, branch or sha",
                file.display(),
                n + 1
            ))
        })?);
    }
    Ok((stem(file).unwrap_or_default(), out))
}

// ---------------------------------------------------------------- plan file

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Kind {
    /// The plan said `delete`.
    Branch,
    /// The plan said `keep` / `in-worktree` with a removable proof (`worktree` field).
    WorktreeBranch,
}

#[derive(Clone, Debug)]
pub struct Planned {
    pub branch: String,
    pub commit: String,
    pub kind: Kind,
    /// The worktree the plan named (`WorktreeBranch` only), as shown in the plan.
    pub worktree: Option<String>,
}

/// Read the output of `stw tidy plan --json`: per repository path, the entries `apply` may act on.
/// `review` and `keep` entries (other than a removable worktree) are dropped here: they are never deleted.
pub fn read_plan_file(path: &Path) -> Result<Vec<(String, Vec<Planned>)>> {
    let text = std::fs::read_to_string(path)
        .map_err(|e| Error::usage(format!("cannot read plan {}: {e}", path.display())))?;
    let v: Value = serde_json::from_str(&text)
        .map_err(|e| Error::usage(format!("plan {} is not JSON: {e}", path.display())))?;
    if v.get("command").and_then(Value::as_str) != Some("tidy-plan") {
        return Err(Error::usage(format!(
            "{} is not the output of `stw tidy plan --json`",
            path.display()
        )));
    }
    let mut out = Vec::new();
    for r in v["repos"].as_array().into_iter().flatten() {
        let Some(rp) = r["path"].as_str() else {
            continue;
        };
        let mut list = Vec::new();
        for b in r["branches"].as_array().into_iter().flatten() {
            let (Some(branch), Some(commit)) = (b["branch"].as_str(), b["commit"].as_str()) else {
                continue;
            };
            let kind = if b["decision"] == "delete" {
                Kind::Branch
            } else if b["decision"] == "keep"
                && b["proof"] == "in-worktree"
                && !b["worktree"].is_null()
            {
                Kind::WorktreeBranch
            } else {
                continue;
            };
            list.push(Planned {
                branch: branch.to_string(),
                commit: commit.to_string(),
                kind,
                worktree: b["worktree"]["path"].as_str().map(str::to_string),
            });
        }
        out.push((rp.to_string(), list));
    }
    Ok(out)
}

// ---------------------------------------------------------------- apply

pub struct ApplyOpts {
    pub manifest_dir: PathBuf,
    pub run_id: String,
    pub now: i64,
}

#[derive(Clone, Debug)]
pub struct BranchResult {
    pub branch: String,
    pub commit: String,
    pub proof: String,
    /// `deleted`, `skipped`, `failed`
    pub action: &'static str,
    pub reason: String,
    pub worktree: Option<String>,
}

#[derive(Clone, Debug)]
pub struct ItemResult {
    pub path: String,
    pub branch: Option<String>,
    /// `removed`, `skipped`, `failed`
    pub action: &'static str,
    pub reason: String,
}

#[derive(Clone, Debug, Default)]
pub struct ApplyRepo {
    pub name: String,
    pub path: String,
    pub error: Option<String>,
    pub manifest: Option<String>,
    pub branches: Vec<BranchResult>,
    pub worktrees: Vec<ItemResult>,
    pub registrations: Vec<ItemResult>,
    /// The `.worktrees/` line in `<common dir>/info/exclude`: `added`, `present` or `failed`.
    pub exclude: Option<ItemResult>,
}

impl ApplyRepo {
    pub fn failed(&self) -> bool {
        self.error.is_some()
            || self.branches.iter().any(|b| b.action == "failed")
            || self.worktrees.iter().any(|b| b.action == "failed")
            || self.registrations.iter().any(|b| b.action == "failed")
            || self.exclude.as_ref().is_some_and(|e| e.action == "failed")
    }
}

fn skipped(bp: &BranchPlan, reason: impl Into<String>) -> BranchResult {
    BranchResult {
        branch: bp.branch.clone(),
        commit: bp.tip.clone(),
        proof: bp.proof.to_string(),
        action: "skipped",
        reason: reason.into(),
        worktree: None,
    }
}

/// Why this linked worktree must stay, or None when it may be removed.
pub(crate) fn worktree_blocker(
    repo: &gix::Repository,
    wt: &Worktrees,
    l: &Linked,
    bp: &BranchPlan,
    base_tip: Option<gix::ObjectId>,
) -> Option<String> {
    if wt.main_dir.as_ref() == Some(&l.dir) {
        return Some("main worktree".into());
    }
    if l.locked {
        return Some(format!(
            "locked{}",
            l.lock_reason
                .as_deref()
                .filter(|r| !r.is_empty())
                .map(|r| format!(": {r}"))
                .unwrap_or_default()
        ));
    }
    if l.branch.as_deref() != Some(bp.branch.as_str()) {
        return Some("its HEAD is not this branch".into());
    }
    match std::fs::symlink_metadata(&l.dir) {
        Ok(m) if m.is_dir() => {}
        _ => return Some("not a plain directory".into()),
    }
    // the directory must belong to this registration
    let dotgit = std::fs::read_to_string(l.dir.join(".git")).unwrap_or_default();
    let claimed = dotgit
        .trim()
        .strip_prefix("gitdir:")
        .map(|p| PathBuf::from(p.trim()));
    let same = |a: &Path, b: &Path| match (std::fs::canonicalize(a), std::fs::canonicalize(b)) {
        (Ok(a), Ok(b)) => a == b,
        _ => false,
    };
    if !claimed.is_some_and(|c| same(&c, &l.git_dir)) {
        return Some("its .git file does not point at this registration".into());
    }
    // removing the directory removes everything below it, ignored files included: another checkout there
    // (a worktree an agent made inside this one, any other git repository) would lose its uncommitted work
    let this = std::fs::canonicalize(&l.dir).unwrap_or_else(|_| l.dir.clone());
    let others = wt.main_dir.iter().chain(
        wt.linked
            .iter()
            .filter(|o| o.git_dir != l.git_dir)
            .chain(wt.relative.iter())
            .map(|o| &o.dir),
    );
    for o in others {
        let c = std::fs::canonicalize(o).unwrap_or_else(|_| o.clone());
        if c.starts_with(&this) {
            return Some(format!("contains the worktree {}", worktrees::show(o)));
        }
    }
    if let Some(n) = nested_git(&l.dir) {
        return Some(format!(
            "contains the git repository {}",
            worktrees::show(&n)
        ));
    }
    for sub in repo::submodule_paths(&l.dir) {
        if l.dir.join(&sub).join(".git").exists() {
            return Some(format!("contains the checked-out submodule {sub}"));
        }
    }
    let wt_repo = match repo::open(&l.dir) {
        Ok(r) => r,
        Err(e) => return Some(format!("cannot be read: {e}")),
    };
    if let Some(op) = crate::status::operation(&wt_repo) {
        return Some(format!("{op} in progress"));
    }
    match crate::status::changes(&wt_repo, true) {
        Ok(c) if c.staged + c.unstaged + c.untracked + c.conflicts == 0 => {}
        Ok(c) => {
            return Some(format!(
                "uncommitted changes ({} staged, {} unstaged, {} untracked, {} conflicts)",
                c.staged, c.unstaged, c.untracked, c.conflicts
            ))
        }
        Err(e) => return Some(format!("status failed: {e}")),
    }
    // pushed: the tip is in the base branch or in the upstream
    let tip: Option<gix::ObjectId> = bp.tip.parse().ok();
    let up_tip = repo::upstream_of(repo, &bp.branch).tip;
    let reachable = |to: Option<gix::ObjectId>| match (tip, to) {
        (Some(t), Some(to)) => repo::is_ancestor(repo, t, to).unwrap_or(false),
        _ => false,
    };
    if !reachable(base_tip) && !reachable(up_tip) {
        return Some("commits not pushed (the tip is in neither the base nor the upstream)".into());
    }
    None
}

/// A directory below `dir` that holds a `.git` (file or directory), looked up without following symlinks.
fn nested_git(dir: &Path) -> Option<PathBuf> {
    let mut todo = vec![dir.to_path_buf()];
    while let Some(d) = todo.pop() {
        for e in std::fs::read_dir(&d).into_iter().flatten().flatten() {
            let Ok(t) = e.file_type() else { continue };
            if e.file_name() == ".git" {
                if d != dir {
                    return Some(d);
                }
            } else if t.is_dir() {
                todo.push(e.path());
            }
        }
    }
    None
}

/// Remove `<common dir>/worktrees/<id>` after checking that it is a direct child of that directory and that
/// the checkout it names is gone.
fn remove_registration(repo: &gix::Repository, reg: &Path) -> std::result::Result<(), String> {
    let parent = repo.common_dir().join("worktrees");
    let (Ok(p), Ok(r)) = (std::fs::canonicalize(&parent), std::fs::canonicalize(reg)) else {
        return Err("registration directory not found".into());
    };
    if r.parent() != Some(p.as_path()) || r == p {
        return Err("not a registration directory".into());
    }
    if let Ok(gitdir) = std::fs::read_to_string(r.join("gitdir")) {
        if Path::new(gitdir.trim()).exists() {
            return Err("its checkout exists again".into());
        }
    }
    std::fs::remove_dir_all(&r).map_err(|e| e.to_string())
}

fn delete_branch(
    repo: &gix::Repository,
    branch: &str,
    sha: gix::ObjectId,
) -> std::result::Result<(), String> {
    let name: gix::refs::FullName = format!("refs/heads/{branch}")
        .try_into()
        .map_err(|e: gix::refs::name::Error| e.to_string())?;
    repo.edit_reference(RefEdit {
        change: Change::Delete {
            expected: PreviousValue::MustExistAndMatch(Target::Object(sha)),
            log: RefLog::AndReference,
        },
        name,
        deref: false,
    })
    .map(|_| ())
    .map_err(|e| e.to_string())
}

/// The directory name under which agents keep their worktrees.
pub const WORKTREES_DIR: &str = ".worktrees";

/// Make sure `<common dir>/info/exclude` (what `git rev-parse --git-path info/exclude` names) has the line
/// `.worktrees/`. Idempotent: `.worktrees/`, `/.worktrees/`, `.worktrees` and `/.worktrees` count as present.
/// The `.gitignore` is never touched. A repository without a work tree has nothing to exclude (None).
fn ensure_worktrees_excluded(repo: &gix::Repository) -> Option<ItemResult> {
    repo.workdir()?;
    let file = repo.common_dir().join("info/exclude");
    let item = |action: &'static str, reason: String| ItemResult {
        path: worktrees::show(&file),
        branch: None,
        action,
        reason,
    };
    let old = match std::fs::read(&file) {
        Ok(t) => t,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Vec::new(),
        Err(e) => return Some(item("failed", format!("reading it: {e}"))),
    };
    let wanted = [".worktrees/", "/.worktrees/", ".worktrees", "/.worktrees"];
    let present = old
        .split(|b| *b == b'\n')
        .any(|l| wanted.iter().any(|w| l.trim_ascii() == w.as_bytes()));
    if present {
        return Some(item(
            "present",
            format!("{WORKTREES_DIR}/ is already listed"),
        ));
    }
    // append only: the bytes that are there stay as they are, a crash cannot lose them
    let mut add: Vec<u8> = Vec::new();
    if !old.is_empty() && !old.ends_with(b"\n") {
        add.push(b'\n');
    }
    add.extend_from_slice(b".worktrees/\n");
    let write = || -> std::io::Result<()> {
        use std::io::Write;
        if let Some(dir) = file.parent() {
            std::fs::create_dir_all(dir)?;
        }
        std::fs::OpenOptions::new()
            .create(true)
            .append(true)
            .open(&file)?
            .write_all(&add)
    };
    Some(match write() {
        Ok(()) => item("added", format!("{WORKTREES_DIR}/ is listed now")),
        Err(e) => item("failed", format!("writing it: {e}")),
    })
}

/// Apply the fresh `plan` of one repository. `wanted` is the list from `--plan FILE` (None: the fresh plan itself).
pub fn apply_repo(
    r: &RepoRef,
    fresh: &RepoPlan,
    wanted: Option<&[Planned]>,
    opts: &ApplyOpts,
) -> ApplyRepo {
    let mut res = ApplyRepo {
        name: r.name.clone(),
        path: p2s(&r.path),
        ..Default::default()
    };
    if let Err(e) = apply_inner(r, fresh, wanted, opts, &mut res) {
        res.error = Some(e.to_string());
    }
    res
}

struct Target_<'a> {
    bp: &'a BranchPlan,
    /// A worktree to remove first.
    wt: Option<&'a Linked>,
}

fn apply_inner(
    r: &RepoRef,
    fresh: &RepoPlan,
    wanted: Option<&[Planned]>,
    opts: &ApplyOpts,
    res: &mut ApplyRepo,
) -> Result<()> {
    if let Some(e) = &fresh.error {
        return Err(Error::repository(e.clone()));
    }
    let repo = repo::open(&r.path)?;
    let scan = worktrees::scan(&repo)?;
    let base_tip = fresh.base.as_deref().and_then(|b| repo::resolve(&repo, b));

    // 1. which branches are asked for
    let mut asked: Vec<(&BranchPlan, Kind)> = Vec::new();
    match wanted {
        None => {
            for bp in &fresh.branches {
                if bp.decision == "delete" {
                    asked.push((bp, Kind::Branch));
                } else if bp.worktree.is_some() {
                    asked.push((bp, Kind::WorktreeBranch));
                }
            }
        }
        Some(list) => {
            let mut seen = std::collections::HashSet::new();
            for p in list {
                // a branch named twice is handled once (the first entry)
                if !seen.insert(p.branch.as_str()) {
                    continue;
                }
                let Some(bp) = fresh.branches.iter().find(|b| b.branch == p.branch) else {
                    res.branches.push(BranchResult {
                        branch: p.branch.clone(),
                        commit: p.commit.clone(),
                        proof: String::new(),
                        action: "skipped",
                        reason: "the branch no longer exists".into(),
                        worktree: None,
                    });
                    continue;
                };
                if bp.tip != p.commit {
                    res.branches.push(skipped(
                        bp,
                        format!(
                            "the branch moved since the plan (planned {}, now {})",
                            short(&p.commit),
                            short(&bp.tip)
                        ),
                    ));
                    continue;
                }
                let holds = match p.kind {
                    Kind::Branch => bp.decision == "delete",
                    // the same worktree as planned: a branch moved to another checkout is not the planned one
                    Kind::WorktreeBranch => bp.worktree.as_ref().is_some_and(|w| {
                        p.worktree.as_deref() == Some(worktrees::show(&w.dir).as_str())
                    }),
                };
                if !holds {
                    res.branches.push(skipped(
                        bp,
                        format!(
                            "the proof no longer holds: now {} [{}] {}",
                            bp.decision, bp.proof, bp.detail
                        ),
                    ));
                    continue;
                }
                asked.push((bp, p.kind));
            }
        }
    }

    // 2. what can be done for each of them
    let mut targets: Vec<Target_> = Vec::new();
    for (bp, kind) in asked {
        if let Some(rel) = scan
            .relative
            .iter()
            .find(|l| l.branch.as_deref() == Some(bp.branch.as_str()))
        {
            res.branches.push(skipped(
                bp,
                format!(
                    "claimed by a worktree registration with relative links ({}), which is not followed",
                    worktrees::show(&rel.dir)
                ),
            ));
            continue;
        }
        // a locked dead registration always holds its branch; an unlocked one only when a plan file is
        // applied (a live run removes it first), and git refuses to delete the branch while it exists
        if let Some(reg) = scan.stale.iter().find(|s| {
            s.branch.as_deref() == Some(bp.branch.as_str()) && (s.locked || wanted.is_some())
        }) {
            res.branches.push(skipped(
                bp,
                format!(
                    "claimed by a{} worktree registration for {}",
                    if reg.locked { " locked" } else { " dead" },
                    worktrees::show(&reg.path)
                ),
            ));
            continue;
        }
        let mut wt = None;
        if kind == Kind::WorktreeBranch {
            let dir = &bp.worktree.as_ref().expect("checked above").dir;
            match scan.linked.iter().find(|l| &l.dir == dir) {
                None => {
                    let main = scan.main_dir.as_ref() == Some(dir);
                    res.worktrees.push(ItemResult {
                        path: worktrees::show(dir),
                        branch: Some(bp.branch.clone()),
                        action: "skipped",
                        reason: if main {
                            "main worktree"
                        } else {
                            "not a linked worktree"
                        }
                        .into(),
                    });
                    res.branches.push(skipped(
                        bp,
                        format!("checked out in {}", worktrees::show(dir)),
                    ));
                    continue;
                }
                Some(l) => {
                    if let Some(why) = worktree_blocker(&repo, &scan, l, bp, base_tip) {
                        res.worktrees.push(ItemResult {
                            path: worktrees::show(&l.dir),
                            branch: Some(bp.branch.clone()),
                            action: "skipped",
                            reason: why.clone(),
                        });
                        res.branches.push(skipped(
                            bp,
                            format!(
                                "checked out in {}, which stays: {why}",
                                worktrees::show(&l.dir)
                            ),
                        ));
                        continue;
                    }
                    wt = Some(l);
                }
            }
        }
        targets.push(Target_ { bp, wt });
    }
    // dead registrations: every unlocked one; a locked one is reported as skipped
    let mut dead: Vec<&worktrees::StaleRegistration> = Vec::new();
    // registrations with relative links are never followed, removed or judged: one status each
    if wanted.is_none() {
        for l in &scan.relative {
            res.registrations.push(ItemResult {
                path: worktrees::show(&l.dir),
                branch: l.branch.clone(),
                action: "skipped",
                reason: "relative links are not followed".into(),
            });
        }
    }
    // (a plan file has no field for registrations: only a live plan cleans them up)
    for s in fresh.stale.iter().filter(|_| wanted.is_none()) {
        if s.locked {
            res.registrations.push(ItemResult {
                path: worktrees::show(&s.path),
                branch: s.branch.clone(),
                action: "skipped",
                reason: "locked".into(),
            });
        } else {
            dead.push(s);
        }
    }

    // 3. manifest first; without it nothing happens
    let entries: Vec<ManifestEntry> = targets
        .iter()
        .map(|t| ManifestEntry {
            run: opts.run_id.clone(),
            repo: res.path.clone(),
            branch: t.bp.branch.clone(),
            sha: t.bp.tip.clone(),
            proof: t
                .bp
                .worktree
                .as_ref()
                .map(|w| w.proof)
                .unwrap_or(t.bp.proof)
                .to_string(),
            time: opts.now,
            worktree: t.wt.map(|l| worktrees::show(&l.dir)),
        })
        .collect();
    if !entries.is_empty() {
        let path = write_manifest(&opts.manifest_dir, &opts.run_id, &entries)
            .map_err(|e| Error::repository(format!("{e}; nothing was deleted")))?;
        res.manifest = Some(p2s(&path));
    }

    res.exclude = ensure_worktrees_excluded(&repo);

    // 4. dead registrations, worktrees, branches
    for s in dead {
        let (action, reason) = match remove_registration(&repo, &s.git_dir) {
            Ok(()) => ("removed", "its directory is gone".to_string()),
            Err(e) => ("failed", e),
        };
        res.registrations.push(ItemResult {
            path: worktrees::show(&s.path),
            branch: s.branch.clone(),
            action,
            reason,
        });
    }
    let mut removed_wt: Vec<&str> = Vec::new();
    for t in &targets {
        let Some(l) = t.wt else { continue };
        let step = std::fs::remove_dir_all(&l.dir)
            .map_err(|e| format!("removing the directory: {e}"))
            .and_then(|_| {
                remove_registration(&repo, &l.git_dir)
                    .map_err(|e| format!("removing the registration: {e}"))
            });
        match step {
            Ok(()) => {
                removed_wt.push(&t.bp.branch);
                res.worktrees.push(ItemResult {
                    path: worktrees::show(&l.dir),
                    branch: Some(t.bp.branch.clone()),
                    action: "removed",
                    reason: "clean, nothing unpushed, not locked".into(),
                });
            }
            Err(e) => res.worktrees.push(ItemResult {
                path: worktrees::show(&l.dir),
                branch: Some(t.bp.branch.clone()),
                action: "failed",
                reason: e,
            }),
        }
    }
    // a branch checked out anywhere now is never deleted
    let after = worktrees::scan(&repo)?;
    for t in targets {
        let sha: Option<gix::ObjectId> = t.bp.tip.parse().ok();
        let proof =
            t.bp.worktree
                .as_ref()
                .map(|w| w.proof)
                .unwrap_or(t.bp.proof)
                .to_string();
        let mut out = BranchResult {
            branch: t.bp.branch.clone(),
            commit: t.bp.tip.clone(),
            proof,
            action: "deleted",
            reason: t.bp.detail.clone(),
            worktree: t.wt.map(|l| worktrees::show(&l.dir)),
        };
        if t.wt.is_some() && !removed_wt.contains(&t.bp.branch.as_str()) {
            out.action = "skipped";
            out.reason = "its worktree could not be removed".into();
        } else if let Some(dir) = after.branch_dir(&t.bp.branch) {
            out.action = "skipped";
            out.reason = format!("checked out in {}", worktrees::show(dir));
        } else if let Some(sha) = sha {
            if let Err(e) = delete_branch(&repo, &t.bp.branch, sha) {
                out.action = "failed";
                out.reason = e;
            }
        } else {
            out.action = "failed";
            out.reason = format!("not a commit id: {}", t.bp.tip);
        }
        res.branches.push(out);
    }
    Ok(())
}

pub fn apply_summary(list: &[ApplyRepo]) -> (usize, usize, usize) {
    let c = |a: &str| {
        list.iter()
            .flat_map(|r| &r.branches)
            .filter(|b| b.action == a)
            .count()
    };
    (c("deleted"), c("skipped"), c("failed"))
}

pub fn apply_to_json(root: &str, run: &str, list: &[ApplyRepo]) -> Value {
    let item = |i: &ItemResult| json!({"path": i.path, "branch": jopt(i.branch.as_deref()), "action": i.action, "reason": i.reason});
    let repos: Vec<Value> = list
        .iter()
        .map(|r| {
            let branches: Vec<Value> = r
                .branches
                .iter()
                .map(|b| {
                    json!({"branch": b.branch, "commit": b.commit, "proof": b.proof, "action": b.action,
                           "reason": b.reason, "worktree": jopt(b.worktree.as_deref())})
                })
                .collect();
            json!({"name": r.name, "path": r.path, "error": jopt(r.error.as_deref()), "manifest": jopt(r.manifest.as_deref()),
                   "branches": branches,
                   "worktrees": r.worktrees.iter().map(item).collect::<Vec<_>>(),
                   "exclude": r.exclude.as_ref().map(item).unwrap_or(Value::Null),
                   "stale_worktree_registrations": r.registrations.iter().map(item).collect::<Vec<_>>()})
        })
        .collect();
    let (d, s, f) = apply_summary(list);
    json!({"schema_version": 2, "command": "tidy-apply", "read_only": false, "run": run,
           "root": root, "engine": engine(), "repos": repos,
           "summary": {"deleted": d, "skipped": s, "failed": f}})
}

pub fn apply_render_human(run: &str, list: &[ApplyRepo]) -> String {
    let mut out = String::new();
    for r in list {
        out += &format!("{}\n", r.name);
        if let Some(e) = &r.error {
            out += &format!("  error: {e}\n");
        }
        if let Some(m) = &r.manifest {
            out += &format!("  manifest: {m}\n");
        }
        for b in &r.branches {
            out += &format!(
                "  {:<12} {} ({}) {}\n",
                b.action,
                b.branch,
                short(&b.commit),
                b.reason
            );
        }
        if let Some(e) = r.exclude.as_ref().filter(|e| e.action != "present") {
            out += &format!("  {:<12} exclude {} {}\n", e.action, e.path, e.reason);
        }
        for w in r.worktrees.iter().chain(&r.registrations) {
            out += &format!("  {:<12} worktree {} {}\n", w.action, w.path, w.reason);
        }
        if r.error.is_none()
            && r.branches.is_empty()
            && r.worktrees.is_empty()
            && r.registrations.is_empty()
        {
            out += "  nothing to do\n";
        }
    }
    let (d, s, f) = apply_summary(list);
    out += &format!("{d} deleted, {s} skipped, {f} failed (run {run})\n");
    out
}

// ---------------------------------------------------------------- restore

#[derive(Clone, Debug)]
pub struct RestoreResult {
    pub repo: String,
    pub branch: String,
    pub sha: String,
    /// `restored`, `already-present`, `not-overwritten`, `missing-object`, `failed`
    pub action: &'static str,
    pub reason: String,
}

pub struct RestoreRun {
    pub run: String,
    pub manifest_dir: PathBuf,
    pub entries: Vec<RestoreResult>,
}

impl RestoreRun {
    /// True when every entry is back (or was never gone).
    pub fn complete(&self) -> bool {
        self.entries
            .iter()
            .all(|e| matches!(e.action, "restored" | "already-present"))
    }
}

/// Re-create the branches of a run (default: the newest) at their recorded commit. A branch that exists at that
/// commit is left alone, one at another commit is not overwritten, a missing object is reported.
pub fn restore(dir: &Path, run: Option<&str>, branch: Option<&str>) -> Result<RestoreRun> {
    let (run_id, mut entries) = read_manifest(dir, run)?;
    if let Some(b) = branch {
        entries.retain(|e| e.branch == b);
        if entries.is_empty() {
            return Err(Error::usage(format!(
                "run {run_id} has no entry for branch {b}"
            )));
        }
    }
    let mut out = Vec::new();
    for e in entries {
        let (action, reason) = restore_one(&e, &run_id);
        out.push(RestoreResult {
            repo: e.repo,
            branch: e.branch,
            sha: e.sha,
            action,
            reason,
        });
    }
    Ok(RestoreRun {
        run: run_id,
        manifest_dir: dir.to_path_buf(),
        entries: out,
    })
}

fn restore_one(e: &ManifestEntry, run: &str) -> (&'static str, String) {
    let repo = match repo::open(Path::new(&e.repo)) {
        Ok(r) => r,
        Err(err) => return ("failed", format!("cannot open {}: {err}", e.repo)),
    };
    let Ok(sha) = e.sha.parse::<gix::ObjectId>() else {
        return ("failed", format!("not a commit id: {}", e.sha));
    };
    let full = format!("refs/heads/{}", e.branch);
    match repo::resolve(&repo, &full) {
        Some(id) if id == sha => return ("already-present", "the branch is at this commit".into()),
        Some(id) => {
            return (
                "not-overwritten",
                format!(
                    "the branch exists at {}, the manifest says {}",
                    short(&id.to_string()),
                    short(&e.sha)
                ),
            )
        }
        None => {}
    }
    if !repo.has_object(sha) {
        return (
            "missing-object",
            format!(
                "commit {} is no longer in {} (garbage collected?)",
                e.sha, e.repo
            ),
        );
    }
    match repo.reference(
        full.as_str(),
        sha,
        PreviousValue::MustNotExist,
        format!("stw: tidy restore {run}"),
    ) {
        Ok(_) => ("restored", format!("created at {}", short(&e.sha))),
        Err(err) => ("failed", err.to_string()),
    }
}

pub fn restore_to_json(r: &RestoreRun) -> Value {
    let c = |a: &str| r.entries.iter().filter(|e| e.action == a).count();
    json!({"schema_version": 2, "command": "tidy-restore", "read_only": false, "run": r.run,
           "manifest_dir": p2s(&r.manifest_dir), "engine": engine(),
           "entries": r.entries.iter().map(|e| json!({"repo": e.repo, "branch": e.branch, "commit": e.sha,
                          "action": e.action, "reason": e.reason})).collect::<Vec<_>>(),
           "summary": {"restored": c("restored"), "already_present": c("already-present"),
                       "not_overwritten": c("not-overwritten"), "missing_object": c("missing-object"), "failed": c("failed")}})
}

pub fn restore_render_human(r: &RestoreRun) -> String {
    let mut out = format!("run {}\n", r.run);
    for e in &r.entries {
        out += &format!(
            "  {:<16} {} {} ({}) {}\n",
            e.action,
            e.repo,
            e.branch,
            short(&e.sha),
            e.reason
        );
    }
    let n = |a: &str| r.entries.iter().filter(|e| e.action == a).count();
    out += &format!(
        "{} restored, {} already present, {} not restored\n",
        n("restored"),
        n("already-present"),
        r.entries.len() - n("restored") - n("already-present")
    );
    out
}
