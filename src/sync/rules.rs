//! `stw sync` and the rules repository (`wstein/dotagents`), when the workspace has a checkout of it.
//!
//! In a whr workspace the checkout is the git submodule `.agents`: normally a detached HEAD at the commit the
//! superproject records. A plain checkout on the default branch works too.
//!
//! Display (always, read-only): one line with the state of the checkout against `origin/<default>`.
//! Update (`--rules`): with `--apply` fetch `origin` and fast-forward HEAD (the branch, or the detached HEAD) to
//! `origin/<default>`; without `--apply` change nothing, fetch nothing, and report what would happen from what is
//! already fetched. The only refusals protect data: local changes in the working tree, local commits that are
//! not in `origin/<default>`, a non-default branch, or a diverged branch. The superproject is never touched: for
//! a submodule the new commit shows up there as a modified gitlink that the owner commits. New commits by other
//! authors than the configured git identity are listed as information, never refused. stw never runs
//! `install.sh` and never writes outside the checkout.
use std::path::{Path, PathBuf};

use gix::bstr::ByteSlice;
use serde_json::{json, Value};

use super::fast_forward::fast_forward;
use super::fetch;
use crate::error::Result;
use crate::github::Client;
use crate::output::*;
use crate::repo;
use crate::status;

/// Where the checkout lives in a whr workspace: the agent-rules git submodule.
pub const DEFAULT_DIR: &str = ".agents";

/// Paths whose change changes how agents behave: highlighted in the preview.
const SENSITIVE: [&str; 5] = ["rules/", "skills/", "guards/", "adapters/", "install.sh"];

#[derive(Clone, Debug, Default)]
pub struct RulesOpts {
    /// `--rules-path`
    pub path: Option<PathBuf>,
    /// `--rules`: update the checkout
    pub update: bool,
    /// `--apply`: fetch and fast-forward; without it `--rules` only reports
    pub apply: bool,
    pub offline: bool,
}

#[derive(Clone, Debug, Default)]
pub struct NewCommit {
    pub sha: String,
    pub subject: String,
    pub author: String,
    pub committer: String,
}

#[derive(Clone, Debug, Default)]
pub struct RulesRec {
    pub path: Option<String>,
    /// `not-found`, `current`, `behind`, `ahead`, `diverged`, `dirty`, `not-default-branch`, `unknown`
    pub state: String,
    pub branch: Option<String>,
    /// e.g. `origin/main`
    pub default_ref: Option<String>,
    pub ahead: Option<usize>,
    pub behind: Option<usize>,
    pub dirty: usize,
    pub fetch: String,
    pub fetched_ago: Option<u64>,
    /// `none`, `would-update`, `updated`, `refused` (only meaningful with `--rules`)
    pub action: String,
    pub reason: String,
    pub commits: Vec<NewCommit>,
    pub files: Vec<String>,
    pub flagged: Vec<String>,
    pub updated: bool,
    pub requested: bool,
    /// current commit of the checkout
    pub head: Option<String>,
    /// HEAD is detached (the normal state of a submodule)
    pub detached: bool,
    /// the checkout is a git submodule of the workspace (its `.git` is a file)
    pub submodule: bool,
    /// distinct `Name <email>` of authors/committers of the new commits that differ from the git identity
    pub foreign: Vec<String>,
}

fn is_sensitive(path: &str) -> bool {
    SENSITIVE.iter().any(|p| path.starts_with(p))
}

/// A real submodule: its `.git` is a file that points into a `modules` directory of a superproject
/// (a linked worktree points into `worktrees`).
fn is_submodule(path: &Path) -> bool {
    let Ok(text) = std::fs::read_to_string(path.join(".git")) else {
        return false;
    };
    text.trim().strip_prefix("gitdir:").is_some_and(|d| {
        d.trim()
            .replace('\\', "/")
            .split('/')
            .any(|c| c == "modules")
    })
}

/// The checkout that is asked for: `--rules-path`, else the default place in the workspace.
fn wanted(root: &Path, flag: Option<&Path>) -> PathBuf {
    flag.map_or_else(|| root.join(DEFAULT_DIR), Path::to_path_buf)
}

fn changed_files(
    repo: &gix::Repository,
    old: gix::ObjectId,
    new: gix::ObjectId,
) -> Result<Vec<String>> {
    use gix::diff::tree::recorder::Change as C;
    let tree = |id| -> Result<gix::Tree<'_>> { Ok(repo.find_commit(id)?.tree()?) };
    let (a, b) = (tree(old)?, tree(new)?);
    let mut rec = gix::diff::tree::Recorder::default();
    let mut state = gix::diff::tree::State::default();
    gix::diff::tree(
        gix::objs::TreeRefIter::from_bytes(&a.data, a.id.kind()),
        gix::objs::TreeRefIter::from_bytes(&b.data, b.id.kind()),
        &mut state,
        &repo.objects,
        &mut rec,
    )?;
    let mut out: Vec<String> = rec
        .records
        .into_iter()
        .filter_map(|c| match c {
            C::Addition {
                entry_mode, path, ..
            }
            | C::Deletion {
                entry_mode, path, ..
            }
            | C::Modification {
                entry_mode, path, ..
            } if !entry_mode.is_tree() => Some(path.to_str_lossy().into_owned()),
            _ => None,
        })
        .collect();
    out.sort();
    Ok(out)
}

/// The configured git identity (`user.name`, `user.email`); `None` when either is unset.
fn identity(repo: &gix::Repository) -> Option<(String, String)> {
    let cfg = repo.config_snapshot();
    let get = |k: &str| cfg.string(k).map(|v| v.to_str_lossy().into_owned());
    Some((get("user.name")?, get("user.email")?))
}

/// The commits in `target` that `head` lacks, and the distinct authors/committers among them that differ
/// from `me` (compared by the raw name and email fields).
fn new_commits(
    repo: &gix::Repository,
    head: gix::ObjectId,
    target: gix::ObjectId,
    me: Option<&(String, String)>,
) -> Result<(Vec<NewCommit>, Vec<String>)> {
    let mut out = vec![];
    let mut foreign: Vec<String> = vec![];
    for info in repo.rev_walk([target]).with_hidden([head]).all()? {
        let c = repo.find_commit(info?.id)?;
        let mut who = |s: gix::actor::SignatureRef<'_>| {
            let text = format!("{} <{}>", s.name, s.email);
            if let Some((n, e)) = me {
                let same = s.name.to_str_lossy() == n.as_str()
                    && s.email.to_str_lossy().eq_ignore_ascii_case(e);
                if !same && !foreign.contains(&text) {
                    foreign.push(text.clone());
                }
            }
            text
        };
        let (author, committer) = (who(c.author()?), who(c.committer()?));
        out.push(NewCommit {
            sha: c.id.to_string(),
            subject: c.message()?.summary().to_str_lossy().into_owned(),
            author,
            committer,
        });
    }
    Ok((out, foreign))
}

/// State of the checkout, or `Err` when it cannot be read.
fn inspect(
    repo: &gix::Repository,
    rec: &mut RulesRec,
) -> Result<Option<(gix::ObjectId, gix::ObjectId)>> {
    let facts = status::head_facts(repo)?;
    rec.branch = facts.branch.clone();
    rec.detached = facts.branch.is_none();
    rec.head = facts.head.map(|h| h.to_string());
    rec.dirty = status::changes(repo, false)?.dirty_paths;
    rec.fetched_ago = std::fs::metadata(repo.git_dir().join("FETCH_HEAD"))
        .and_then(|m| m.modified())
        .ok()
        .and_then(|t| std::time::SystemTime::now().duration_since(t).ok())
        .map(|d| d.as_secs());
    let Some(full) = repo::base_ref(repo) else {
        rec.state = "unknown".into();
        return Ok(None);
    };
    let short = full
        .strip_prefix("refs/remotes/")
        .unwrap_or(&full)
        .to_string();
    rec.default_ref = Some(short.clone());
    let default_branch = short.strip_prefix("origin/").unwrap_or(&short);
    let (Some(target), Some(head)) = (repo::resolve(repo, &full), facts.head) else {
        rec.state = "unknown".into();
        return Ok(None);
    };
    let (ahead, behind) = (
        repo::count_range(repo, head, target)?,
        repo::count_range(repo, target, head)?,
    );
    rec.ahead = Some(ahead);
    rec.behind = Some(behind);
    rec.state = if rec.dirty > 0 {
        "dirty"
    } else if facts.branch.as_deref().is_some_and(|b| b != default_branch) {
        "not-default-branch"
    } else if ahead > 0 && behind > 0 {
        "diverged"
    } else if behind > 0 {
        "behind"
    } else if ahead > 0 {
        "ahead"
    } else {
        "current"
    }
    .into();
    Ok(Some((head, target)))
}

fn refuse(rec: &mut RulesRec, reason: String) {
    rec.action = "refused".into();
    rec.reason = reason;
}

/// The resolved location of the rules checkout, which the generic repository loop must leave alone.
pub fn checkout_path(root: &Path, flag: Option<&Path>) -> PathBuf {
    let p = wanted(root, flag);
    std::fs::canonicalize(&p).unwrap_or(p)
}

pub fn agent_rules(root: &Path, gh: Option<&Client>, o: &RulesOpts) -> RulesRec {
    let mut rec = RulesRec {
        state: "not-found".into(),
        fetch: "not-run".into(),
        action: "none".into(),
        requested: o.update,
        ..Default::default()
    };
    let path = wanted(root, o.path.as_deref());
    if !path.is_dir() {
        if o.update {
            refuse(
                &mut rec,
                format!("no agent-rules checkout at {}", path.display()),
            );
        }
        return rec;
    }
    rec.path = Some(p2s(&path));
    rec.submodule = is_submodule(&path);
    let Ok(repo) = repo::open(&path) else {
        if o.update {
            refuse(&mut rec, "not a git repository".into());
        }
        return rec;
    };
    if o.update && o.offline {
        rec.fetch = "skipped-offline".into();
    } else if o.update && o.apply {
        let f = fetch::fetch_origin(&repo, gh);
        rec.fetch = f.label().into();
        if rec.fetch == "failed" || rec.fetch == "unsupported-remote" {
            refuse(
                &mut rec,
                format!("fetch failed: {}", f.message().unwrap_or("unknown error")),
            );
        }
    }
    let heads = match inspect(&repo, &mut rec) {
        Ok(h) => h,
        Err(e) => {
            rec.state = "unknown".into();
            if o.update {
                refuse(&mut rec, format!("cannot read checkout: {e}"));
            }
            return rec;
        }
    };
    if !o.update || rec.action == "refused" {
        return rec;
    }
    let default_ref = rec.default_ref.clone().unwrap_or_default();
    let (dirty, branch) = (rec.dirty, rec.branch.clone());
    match rec.state.as_str() {
        "dirty" => refuse(
            &mut rec,
            format!("working tree has {} changed file(s)", dirty),
        ),
        "not-default-branch" => refuse(
            &mut rec,
            format!(
                "{} is checked out, not the default branch ({default_ref})",
                branch.as_deref().unwrap_or("detached HEAD")
            ),
        ),
        "diverged" => refuse(
            &mut rec,
            format!("diverged from {default_ref}: not a fast-forward"),
        ),
        "unknown" => {
            let why = if repo.find_remote("origin").is_err() {
                "the checkout has no remote origin"
            } else if rec.head.is_none() {
                "the checkout has no commits"
            } else if let Some(d) = &rec.default_ref {
                &format!("{d} does not exist (origin/HEAD points to a missing ref)")
            } else {
                "origin has no default branch (no origin/HEAD, origin/main or origin/master)"
            };
            refuse(&mut rec, why.to_string());
        }
        "behind" => {
            let (head, target) = heads.expect("behind has heads");
            if let Err(e) = update_behind(&repo, head, target, o, &mut rec) {
                refuse(&mut rec, e.to_string());
            }
        }
        _ => {}
    }
    rec
}

fn update_behind(
    repo: &gix::Repository,
    head: gix::ObjectId,
    target: gix::ObjectId,
    o: &RulesOpts,
    rec: &mut RulesRec,
) -> Result<()> {
    (rec.commits, rec.foreign) = new_commits(repo, head, target, identity(repo).as_ref())?;
    rec.files = changed_files(repo, head, target)?;
    rec.flagged = rec
        .files
        .iter()
        .filter(|f| is_sensitive(f))
        .cloned()
        .collect();
    if !o.apply {
        rec.action = "would-update".into();
        return Ok(());
    }
    let label = rec.default_ref.clone().unwrap_or_default();
    match fast_forward(repo, target, &label) {
        Ok(_) => {
            rec.action = "updated".into();
            rec.state = "current".into();
            rec.behind = Some(0);
            rec.head = Some(target.to_string());
            rec.updated = true;
        }
        Err(e) => refuse(rec, format!("fast-forward refused: {e}")),
    }
    Ok(())
}

fn state_label(r: &RulesRec) -> String {
    let d = r.default_ref.as_deref().unwrap_or("origin/<default>");
    match r.state.as_str() {
        "current" => "current".into(),
        "behind" => {
            let n = r.behind.unwrap_or(0);
            format!("behind {n} commit{} of {d}", if n == 1 { "" } else { "s" })
        }
        "ahead" => {
            let n = r.ahead.unwrap_or(0);
            format!(
                "ahead {n} commit{} of {d} (local only)",
                if n == 1 { "" } else { "s" }
            )
        }
        "diverged" => "diverged".into(),
        "dirty" => "dirty".into(),
        "not-default-branch" => "not on default branch".into(),
        "unknown" => "no origin default branch known".into(),
        _ => "not found".into(),
    }
}

pub fn render_human(r: &RulesRec) -> String {
    let mut out = format!("agent-rules: {}", state_label(r));
    if r.state != "not-found" && r.fetch != "ok" {
        out += " (as of last fetch)";
    }
    out += "\n";
    if !r.requested {
        return out;
    }
    if let Some(h) = &r.head {
        out += &format!(
            "    rules commit {}{}\n",
            short(h),
            if r.detached { " (detached HEAD)" } else { "" }
        );
    }
    for c in &r.commits {
        out += &format!("    {} {}  [{}]\n", short(&c.sha), c.subject, c.author);
    }
    for f in &r.files {
        out += &format!("    {} {f}\n", if is_sensitive(f) { "!" } else { " " });
    }
    if !r.flagged.is_empty() {
        out += &format!(
            "    ! {} file(s) change agent behavior (rules, skills, guards, adapters or install.sh); stw does not run install.sh\n",
            r.flagged.len()
        );
    }
    match (r.state.as_str(), r.action.as_str()) {
        ("ahead", "none") => {
            let n = r.ahead.unwrap_or(0);
            out += &format!(
                "    ahead of {} by {n} commit{}, nothing to fast-forward\n",
                r.default_ref.as_deref().unwrap_or("origin/<default>"),
                if n == 1 { "" } else { "s" }
            );
        }
        ("current", "none") => out += "    up to date, nothing to fast-forward\n",
        _ => {}
    }
    if !r.foreign.is_empty() {
        out += &format!(
            "    info: new commits by {} (not the configured git identity)\n",
            r.foreign.join(", ")
        );
    }
    match r.action.as_str() {
        "updated" => {
            out += "    updated by fast-forward. This applies to NEW sessions; running agents keep their loaded rules\n";
            if r.submodule {
                let name = r
                    .path
                    .as_deref()
                    .and_then(|p| Path::new(p).file_name())
                    .map_or(DEFAULT_DIR.into(), |n| n.to_string_lossy().into_owned());
                out += &format!("    {name} has a new commit, recorded as modified in the workspace (not committed); the new commit is only kept once the workspace records it, git submodule update can restore the recorded commit (whr-up runs it when a submodule is not yet initialized)\n");
            }
        }
        "would-update" => out +=
            "    dry run (without --apply): would fast-forward; nothing was fetched or written\n",
        "refused" => out += &format!("    refused, nothing changed: {}\n", r.reason),
        _ => {}
    }
    out
}

pub fn to_json(r: &RulesRec) -> Value {
    json!({"path": jopt(r.path.as_deref()), "state": r.state, "branch": jopt(r.branch.as_deref()),
           "default_ref": jopt(r.default_ref.as_deref()), "ahead": r.ahead, "behind": r.behind,
           "dirty_files": r.dirty, "fetch": r.fetch, "last_fetch_seconds_ago": r.fetched_ago,
           "requested": r.requested, "head": jopt(r.head.as_deref()), "detached": r.detached,
           "submodule": r.submodule, "foreign_authors": r.foreign, "action": r.action, "reason": r.reason,
           "commits": r.commits.iter().map(|c| json!({"sha": c.sha, "subject": c.subject, "author": c.author, "committer": c.committer})).collect::<Vec<_>>(),
           "files": r.files, "flagged_files": r.flagged, "updated": r.updated})
}
