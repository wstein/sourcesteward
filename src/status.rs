//! `stw status`: where am I in every repository of the workspace. Read-only.
use std::collections::BTreeSet;
use std::path::Path;

use gix::bstr::BString;
use gix::state::InProgress;
use serde_json::{json, Value};

use crate::error::Result;
use crate::github::{Client, GithubState, IssueCounts, OpenPr, PAGE};
use crate::output::*;
use crate::repo::{self, RepoRef, Upstream};
use crate::status_review;

/// Counts like `git status --porcelain=v2`: a path counts once per kind.
#[derive(Clone, Debug, Default)]
pub struct Changes {
    pub staged: usize,
    pub unstaged: usize,
    pub untracked: usize,
    pub conflicts: usize,
    /// Paths with any tracked change (porcelain lines `1`, `2`, `u`); sync's "dirty files".
    pub dirty_paths: usize,
    /// The paths themselves (tracked changes), for the overlap check of a fast-forward.
    pub paths: BTreeSet<BString>,
}

/// Run the index/worktree/HEAD status of `repo` through gix.
pub fn changes(repo: &gix::Repository, with_untracked: bool) -> Result<Changes> {
    use gix::status::index_worktree::Item as W;
    use gix::status::Item;
    let mut staged: BTreeSet<BString> = BTreeSet::new();
    let mut unstaged: BTreeSet<BString> = BTreeSet::new();
    let mut conflicts: BTreeSet<BString> = BTreeSet::new();
    let mut untracked_paths: Vec<BString> = Vec::new();
    let platform = repo
        .status(gix::progress::Discard)?
        .untracked_files(if with_untracked {
            gix::status::UntrackedFiles::Collapsed
        } else {
            gix::status::UntrackedFiles::None
        });
    for item in platform.into_iter(Vec::<BString>::new())? {
        match item? {
            Item::TreeIndex(ch) => {
                use gix::diff::index::Change as C;
                let loc = match &ch {
                    C::Addition { location, .. }
                    | C::Deletion { location, .. }
                    | C::Modification { location, .. } => location.clone().into_owned(),
                    C::Rewrite { location, .. } => location.clone().into_owned(),
                };
                staged.insert(loc);
            }
            Item::IndexWorktree(W::Modification {
                rela_path, status, ..
            }) => {
                use gix::status::plumbing::index_as_worktree::EntryStatus as S;
                match status {
                    S::Conflict { .. } => {
                        conflicts.insert(rela_path);
                    }
                    S::Change(_) => {
                        unstaged.insert(rela_path);
                    }
                    S::NeedsUpdate(_) | S::IntentToAdd => {}
                }
            }
            Item::IndexWorktree(W::DirectoryContents { entry, .. }) => {
                if matches!(entry.status, gix::dir::entry::Status::Untracked) {
                    untracked_paths.push(entry.rela_path.clone());
                }
            }
            Item::IndexWorktree(W::Rewrite { .. }) => {}
        }
    }
    let untracked = if untracked_paths.is_empty() {
        0
    } else {
        collapse_untracked(repo, &untracked_paths)?
    };
    let mut paths: BTreeSet<BString> = BTreeSet::new();
    paths.extend(staged.iter().cloned());
    paths.extend(unstaged.iter().cloned());
    paths.extend(conflicts.iter().cloned());
    Ok(Changes {
        staged: staged.len(),
        unstaged: unstaged.len(),
        untracked,
        conflicts: conflicts.len(),
        dirty_paths: paths.len(),
        paths,
    })
}

/// Count untracked entries the way `git status` shows them: a directory without any tracked file below
/// it is one entry. gix reports nested repositories (e.g. `.worktrees/*`) one by one instead of
/// collapsing them into the parent directory; this closes that gap with an index lookup.
fn collapse_untracked(repo: &gix::Repository, paths: &[BString]) -> Result<usize> {
    let index = repo.index_or_empty()?;
    let mut keys: BTreeSet<BString> = BTreeSet::new();
    for p in paths {
        let bytes: &[u8] = p.as_ref();
        let trimmed = bytes.strip_suffix(b"/").unwrap_or(bytes);
        let mut key = BString::from(trimmed);
        for (i, b) in trimmed.iter().enumerate() {
            if *b == b'/' {
                let dir = &trimmed[..=i];
                if index
                    .prefixed_entries(dir.into())
                    .map(|e| e.is_empty())
                    .unwrap_or(true)
                {
                    key = BString::from(dir);
                    break;
                }
            }
        }
        keys.insert(key);
    }
    Ok(keys.len())
}

/// Name of a running operation, in the prototype's vocabulary.
pub fn operation(repo: &gix::Repository) -> Option<&'static str> {
    Some(match repo.state()? {
        InProgress::ApplyMailbox
        | InProgress::ApplyMailboxRebase
        | InProgress::Rebase
        | InProgress::RebaseInteractive => "rebase",
        InProgress::Merge => "merge",
        InProgress::CherryPick | InProgress::CherryPickSequence => "cherry-pick",
        InProgress::Revert | InProgress::RevertSequence => "revert",
        InProgress::Bisect => "bisect",
    })
}

pub fn stash_count(repo: &gix::Repository) -> usize {
    let Ok(Some(r)) = repo.try_find_reference("refs/stash") else {
        return 0;
    };
    match r.log_iter().all() {
        Ok(Some(it)) => it.count(),
        _ => 0,
    }
}

/// Branch, HEAD and upstream facts.
#[derive(Clone, Debug, Default)]
pub struct HeadFacts {
    pub branch: Option<String>,
    pub head: Option<gix::ObjectId>,
    pub upstream: Upstream,
    pub ahead: Option<usize>,
    pub behind: Option<usize>,
}

pub fn head_facts(repo: &gix::Repository) -> Result<HeadFacts> {
    let mut f = HeadFacts {
        branch: repo
            .head_name()?
            .and_then(|n| repo::branch_short(n.as_ref())),
        ..Default::default()
    };
    f.head = repo.head_id().ok().map(|i| i.detach());
    if let (Some(b), Some(head)) = (&f.branch, f.head) {
        f.upstream = repo::upstream_of(repo, b);
        if let Some(up) = f.upstream.tip {
            f.ahead = Some(repo::count_range(repo, head, up)?);
            f.behind = Some(repo::count_range(repo, up, head)?);
        }
    }
    Ok(f)
}

/// Gitlink recorded in the superproject's HEAD versus the checked-out commit.
pub fn gitlink_state(
    root: &gix::Repository,
    rel: &str,
    sub: &gix::Repository,
) -> (Option<String>, Option<&'static str>) {
    let recorded = (|| -> Option<gix::ObjectId> {
        let tree = root.head_commit().ok()?.tree().ok()?;
        let e = tree.lookup_entry_by_path(rel).ok()??;
        Some(e.object_id())
    })();
    let Some(pin) = recorded else {
        return (None, None);
    };
    let Some(full) = sub.head_id().ok().map(|i| i.detach()) else {
        return (Some(pin.to_string()), None);
    };
    let state = if pin == full {
        "ok"
    } else if repo::is_ancestor(sub, pin, full).unwrap_or(false) {
        "ahead"
    } else if repo::is_ancestor(sub, full, pin).unwrap_or(false) {
        "behind"
    } else {
        "diverged-or-unknown"
    };
    (Some(pin.to_string()), Some(state))
}

/// What a status run is asked to read beyond the default.
#[derive(Clone, Copy, Debug, Default)]
pub struct StatusOpts {
    /// `--worktrees`: list the linked worktrees of each repository.
    pub worktrees: bool,
    /// `--fetch`: fetch `origin` first (remote-tracking refs only, no prune); the one thing that is not read-only.
    pub fetch: bool,
    /// `--review`: read the review state of the open pull requests from their comments.
    pub review: bool,
}

/// What `--fetch` did for one repository.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct FetchFact {
    /// `not-run`, `ok`, `no-remote`, `failed`, `unsupported-remote` or `unsupported` (as `stw sync`).
    pub state: &'static str,
    /// `git` (the installed git through the runner), `gix` (https, no program) or None.
    pub via: Option<&'static str>,
    pub error: Option<String>,
}

impl FetchFact {
    pub fn not_run() -> FetchFact {
        FetchFact {
            state: "not-run",
            via: None,
            error: None,
        }
    }

    fn json(&self) -> Value {
        json!({"state": self.state, "via": jopt(self.via), "error": jopt(self.error.as_deref())})
    }
}

/// `git fetch origin` for `--fetch`, through `sync::fetch` (the runner for ssh and local-path remotes, gix for
/// https): never prunes (`--no-prune` wins over `fetch.prune`), no tags, no submodules, no hooks. Refused before
/// anything starts when a configured fetch refspec of `origin` would write outside `refs/remotes/origin/`.
fn fetch_first(path: &Path, gh: Option<&Client>) -> FetchFact {
    use crate::sync::fetch::{check_prune_scope, fetch_origin_with, Prune};
    let repo = match repo::open(path) {
        Ok(r) => r,
        Err(e) => {
            return FetchFact {
                state: "failed",
                via: None,
                error: Some(e.to_string().chars().take(300).collect()),
            }
        }
    };
    if let Err(m) = check_prune_scope(&repo) {
        return FetchFact {
            state: "failed",
            via: None,
            error: Some(m),
        };
    }
    let f = fetch_origin_with(&repo, gh, Prune::Off);
    let error = f.message().map(str::to_string);
    FetchFact {
        state: f.label(),
        via: f.via,
        error,
    }
}

/// One linked worktree (`--worktrees`); the main checkout is never listed.
#[derive(Clone, Debug)]
pub struct WorktreeInfo {
    pub path: String,
    pub branch: Option<String>,
    pub detached: bool,
    /// Any tracked change or untracked file in it (a worktree that cannot be read counts as dirty).
    pub dirty: bool,
    /// The directory does not exist.
    pub missing: bool,
    /// Locked: the reason, empty when none was given.
    pub locked: Option<String>,
    /// git would prune the registration: its checkout is gone and it is not locked.
    pub prunable: bool,
}

impl WorktreeInfo {
    fn json(&self) -> Value {
        json!({"path": self.path, "branch": jopt(self.branch.as_deref()), "detached": self.detached,
               "dirty": self.dirty, "missing": self.missing, "locked": jopt(self.locked.as_deref()),
               "prunable": self.prunable})
    }
}

/// The linked worktrees of a repository, sorted by path: those whose directory exists (with the dirty check
/// of their own), relative links, and the registrations whose checkout is gone (`missing`, and `prunable`
/// unless locked).
pub fn worktree_list(w: &crate::repo::worktrees::Worktrees) -> Vec<WorktreeInfo> {
    use crate::repo::worktrees::show;
    let mut v: Vec<WorktreeInfo> = Vec::new();
    for l in w.linked.iter().chain(&w.relative) {
        let missing = !l.dir.is_dir();
        let dirty = !missing
            && repo::open(&l.dir)
                .and_then(|r| changes(&r, true))
                .map(|c| c.staged + c.unstaged + c.untracked + c.conflicts > 0)
                .unwrap_or(true);
        v.push(WorktreeInfo {
            path: show(&l.dir),
            detached: l.branch.is_none(),
            branch: l.branch.clone(),
            dirty,
            missing,
            locked: l.locked.then(|| l.lock_reason.clone().unwrap_or_default()),
            prunable: false,
        });
    }
    for st in &w.stale {
        v.push(WorktreeInfo {
            path: show(&st.path),
            detached: st.branch.is_none(),
            branch: st.branch.clone(),
            dirty: false,
            missing: !st.path.is_dir(),
            locked: st
                .locked
                .then(|| st.lock_reason.clone().unwrap_or_default()),
            prunable: !st.locked,
        });
    }
    v.sort_by(|a, b| a.path.cmp(&b.path));
    v
}

#[derive(Clone, Debug)]
pub struct RepoStatus {
    pub name: String,
    pub path: String,
    pub error: Option<String>,
    pub facts: HeadFacts,
    pub changes: Changes,
    pub stash: usize,
    pub op: Option<&'static str>,
    pub pin: Option<String>,
    pub pinstate: Option<&'static str>,
    pub slug: Option<String>,
    pub prs: Vec<OpenPr>,
    pub pr_error: Option<String>,
    pub pr_read: bool,
    /// `--review` took part in this repository: its pull requests have a review state or are counted as not read.
    pub review_read: bool,
    pub issues: Option<IssueCounts>,
    pub issue_error: Option<String>,
    pub branches_local: Option<usize>,
    pub branches_remote: Option<usize>,
    pub worktrees: Option<usize>,
    /// The linked worktrees, only with `--worktrees`.
    pub worktree_list: Option<Vec<WorktreeInfo>>,
    /// `--fetch`.
    pub fetch: FetchFact,
}

/// Remote-tracking branches of `origin` (`refs/remotes/origin/*` without `HEAD`), as of the last fetch.
pub fn remote_branch_count(repo: &gix::Repository) -> Result<usize> {
    let mut n = 0;
    for r in repo.references()?.remote_branches()? {
        let r = r?;
        let name = r.name().as_bstr().to_string();
        if let Some(b) = name.strip_prefix("refs/remotes/origin/") {
            if b != "HEAD" {
                n += 1;
            }
        }
    }
    Ok(n)
}

fn blank(name: String, path: String) -> RepoStatus {
    RepoStatus {
        name,
        path,
        error: None,
        facts: HeadFacts::default(),
        changes: Changes::default(),
        stash: 0,
        op: None,
        pin: None,
        pinstate: None,
        slug: None,
        prs: vec![],
        pr_error: None,
        pr_read: false,
        review_read: false,
        issues: None,
        issue_error: None,
        branches_local: None,
        branches_remote: None,
        worktrees: None,
        worktree_list: None,
        fetch: FetchFact::not_run(),
    }
}

/// The local half and, with a client, the remote half of one repository.
pub fn collect_one(r: &RepoRef, root: &Path, gh: Option<&Client>) -> RepoStatus {
    let mut s = collect_local(r, root, &StatusOpts::default(), None);
    read_remote(&mut s, gh);
    s
}

/// The local half: what git itself tells for the repository.
pub fn collect_local(
    r: &RepoRef,
    root: &Path,
    opts: &StatusOpts,
    gh: Option<&Client>,
) -> RepoStatus {
    let mut s = blank(r.name.clone(), p2s(&r.path));
    if opts.fetch {
        s.fetch = fetch_first(&r.path, gh);
    }
    let mut run = || -> Result<()> {
        let repo = repo::open(&r.path)?;
        s.facts = head_facts(&repo)?;
        s.changes = changes(&repo, true)?;
        s.stash = stash_count(&repo);
        s.branches_local = repo::local_branches(&repo).ok().map(|b| b.len());
        s.branches_remote = remote_branch_count(&repo).ok();
        let wts = crate::repo::worktrees::scan(&repo).ok();
        s.worktrees = wts
            .as_ref()
            .map(|w| w.linked.len() + w.relative.iter().filter(|l| l.dir.is_dir()).count());
        if opts.worktrees {
            s.worktree_list = wts.as_ref().map(worktree_list);
        }
        s.op = operation(&repo);
        s.slug = repo::slug(&repo);
        if r.path != root {
            if let Ok(rel) = r.path.strip_prefix(root) {
                let rr = repo::open(root)?;
                let (pin, st) = gitlink_state(&rr, &rel.to_string_lossy(), &repo);
                s.pin = pin;
                s.pinstate = st;
            }
        }
        Ok(())
    };
    if let Err(e) = run() {
        s.error = Some(e.to_string());
    }
    s
}

/// The remote half: open pull requests and issues of the GitHub origin. A repository that could not be read
/// locally has none.
pub fn read_remote(s: &mut RepoStatus, gh: Option<&Client>) {
    if s.error.is_some() {
        return;
    }
    if let (Some(c), Some(slug)) = (gh, s.slug.clone()) {
        match c.open_prs(&slug) {
            Ok(p) => {
                s.prs = p;
                s.pr_read = true;
            }
            Err(e) => s.pr_error = Some(e.to_string().chars().take(200).collect()),
        }
        match c.open_issues(&slug) {
            Ok(i) => s.issues = Some(i),
            Err(e) => s.issue_error = Some(e.to_string().chars().take(200).collect()),
        }
    }
}

/// One remote-read counter of a repository (or of all): what the hosting said, or why there is no number.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Source {
    /// `open` of `sub` (bot PRs, bug issues); `truncated`: a full page was read, the true number may be higher.
    Read {
        open: usize,
        sub: usize,
        truncated: bool,
    },
    /// Not read on purpose: `--offline` or no token.
    NotRead,
    /// The repository has no GitHub origin (a total ignores it).
    NoRemote,
    /// The read failed (a notice says why) or the repository could not be read locally.
    Failed,
}

impl Source {
    fn state(self) -> &'static str {
        match self {
            Source::Read { .. } => "ok",
            Source::NotRead => "not-read",
            Source::NoRemote => "no-remote",
            Source::Failed => "failed",
        }
    }

    fn json(self, sub_name: &str) -> Value {
        let (open, sub, truncated) = match self {
            Source::Read {
                open,
                sub,
                truncated,
            } => (json!(open), json!(sub), truncated),
            _ => (Value::Null, Value::Null, false),
        };
        json!({"state": self.state(), "open": open, sub_name: sub, "truncated": truncated})
    }

    fn text(self, label: &str, sub_label: &str) -> String {
        match self {
            Source::Read {
                open,
                sub,
                truncated,
            } => {
                let plus = if truncated { "+" } else { "" };
                format!("{label} {open}{plus} ({sub_label} {sub}{plus})")
            }
            Source::Failed => format!("{label} ?"),
            Source::NotRead | Source::NoRemote => format!("{label} n/a"),
        }
    }
}

/// The counters of one repository, or the totals of all.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Counts {
    pub prs: Source,
    pub issues: Source,
    pub branches_local: Option<usize>,
    pub branches_remote: Option<usize>,
    pub worktrees: Option<usize>,
    pub stash: Option<usize>,
}

pub fn counts_of(s: &RepoStatus) -> Counts {
    if s.error.is_some() {
        return Counts {
            prs: Source::Failed,
            issues: Source::Failed,
            branches_local: None,
            branches_remote: None,
            worktrees: None,
            stash: None,
        };
    }
    let remote = |read: Option<(usize, usize, bool)>, failed: bool| match read {
        Some((open, sub, truncated)) => Source::Read {
            open,
            sub,
            truncated,
        },
        None if failed => Source::Failed,
        None if s.slug.is_none() => Source::NoRemote,
        None => Source::NotRead,
    };
    Counts {
        prs: remote(
            s.pr_read.then(|| {
                (
                    s.prs.len(),
                    s.prs.iter().filter(|p| p.bot).count(),
                    s.prs.len() >= PAGE,
                )
            }),
            s.pr_error.is_some(),
        ),
        issues: remote(
            s.issues.map(|i| (i.open, i.bug, i.truncated)),
            s.issue_error.is_some(),
        ),
        branches_local: s.branches_local,
        branches_remote: s.branches_remote,
        worktrees: s.worktrees,
        stash: Some(s.stash),
    }
}

/// `esc` for other modules.
pub fn escape_for_text(s: &str) -> String {
    esc(s)
}

/// Text from outside (repository names, GitHub error messages with the slug from the origin URL) for the
/// human output: control and invisible characters are shown as escapes, never sent to the terminal.
fn esc(s: &str) -> String {
    crate::github::reads::escape_untrusted(s, 300).0
}

fn sum_source(all: impl Iterator<Item = Source>) -> Source {
    let all: Vec<Source> = all.collect();
    if all.contains(&Source::Failed) {
        return Source::Failed;
    }
    let (mut open, mut sub, mut truncated, mut any) = (0, 0, false, false);
    for s in &all {
        if let Source::Read {
            open: o,
            sub: b,
            truncated: t,
        } = s
        {
            (open, sub, truncated, any) = (open + o, sub + b, truncated || *t, true);
        }
    }
    if all.contains(&Source::NotRead) {
        Source::NotRead
    } else if any {
        Source::Read {
            open,
            sub,
            truncated,
        }
    } else {
        // no repository (or none with a GitHub origin): nothing to read
        Source::NoRemote
    }
}

fn sum_opt(all: impl Iterator<Item = Option<usize>>) -> Option<usize> {
    all.sum::<Option<usize>>()
}

/// Totals: a counter is a number only when every repository gave one (repositories without a GitHub origin
/// are left out of the PR and issue totals); otherwise `?` (a read failed) or `n/a` (not read).
pub fn totals(list: &[RepoStatus]) -> Counts {
    let c: Vec<Counts> = list.iter().map(counts_of).collect();
    Counts {
        prs: sum_source(c.iter().map(|c| c.prs)),
        issues: sum_source(c.iter().map(|c| c.issues)),
        branches_local: sum_opt(c.iter().map(|c| c.branches_local)),
        branches_remote: sum_opt(c.iter().map(|c| c.branches_remote)),
        worktrees: sum_opt(c.iter().map(|c| c.worktrees)),
        stash: sum_opt(c.iter().map(|c| c.stash)),
    }
}

fn counts_json(c: &Counts) -> Value {
    json!({
        "pull_requests": c.prs.json("bot"),
        "issues": c.issues.json("bug"),
        "branches": {"local": c.branches_local, "remote": c.branches_remote},
        "worktrees": c.worktrees,
        "stash": c.stash,
    })
}

fn num(n: Option<usize>) -> String {
    n.map_or("?".into(), |n| n.to_string())
}

/// The `Counts` block: one aligned line per repository, then `All`.
fn counts_block(list: &[RepoStatus]) -> String {
    let mut rows: Vec<(String, Counts)> =
        list.iter().map(|s| (esc(&s.name), counts_of(s))).collect();
    rows.push(("All".into(), totals(list)));
    let cells: Vec<Vec<String>> = rows
        .iter()
        .map(|(_, c)| {
            vec![
                c.prs.text("PRs", "bot"),
                c.issues.text("Issues", "bug"),
                format!(
                    "Branches {}/{}",
                    num(c.branches_local),
                    num(c.branches_remote)
                ),
                format!("Worktrees {}", num(c.worktrees)),
                format!("Stash {}", num(c.stash)),
            ]
        })
        .collect();
    let name_w = rows
        .iter()
        .map(|(n, _)| n.chars().count())
        .max()
        .unwrap_or(0);
    let widths: Vec<usize> = (0..5)
        .map(|i| {
            cells
                .iter()
                .map(|r| r[i].chars().count())
                .max()
                .unwrap_or(0)
        })
        .collect();
    let mut out = String::from("Counts\n");
    for ((name, _), row) in rows.iter().zip(&cells) {
        let mut line = format!("  {name:<name_w$}  ");
        for (i, cell) in row.iter().enumerate() {
            if i > 0 {
                line += " | ";
            }
            if i + 1 < row.len() {
                line += &format!("{cell:<w$}", w = widths[i]);
            } else {
                line += cell;
            }
        }
        out += &line;
        out.push('\n');
    }
    out
}

/// Attention reasons, in the prototype's order.
pub fn reasons(s: &RepoStatus) -> Vec<String> {
    let mut v = vec![];
    if let Some(op) = s.op {
        v.push(format!("operation:{op}"));
    }
    if s.changes.conflicts > 0 {
        v.push("conflicts".into());
    }
    if s.changes.staged > 0 {
        v.push("staged".into());
    }
    if s.changes.unstaged > 0 {
        v.push("unstaged".into());
    }
    if s.changes.untracked > 0 {
        v.push("untracked".into());
    }
    if s.facts.behind.unwrap_or(0) > 0 {
        v.push("behind".into());
    }
    if s.facts.branch.is_some() && s.facts.upstream.short.is_none() && !s.facts.upstream.gone {
        v.push("no-upstream".into());
    }
    if s.facts.branch.is_none() && s.error.is_none() {
        v.push("detached".into());
    }
    if s.error.is_some() {
        v.push("error".into());
    }
    v
}

pub fn to_json(
    root: &str,
    gh: &GithubState,
    list: &[RepoStatus],
    opts: &StatusOpts,
    stats: Option<Value>,
) -> Value {
    let mut attention = 0;
    let repos: Vec<Value> = list
        .iter()
        .map(|s| {
            let rs = reasons(s);
            let attn = !rs.is_empty();
            if attn {
                attention += 1;
            }
            let prs: Vec<Value> = s
                .prs
                .iter()
                .map(|p| {
                    json!({"number": p.number, "title": p.title, "head": p.head, "draft": p.draft,
                           "author": p.author, "review": jopt(p.review.as_ref().map(|r| r.label())),
                           "review_state": p.review.as_ref().map_or(Value::Null, status_review::state_json),
                           "hints": status_review::hints_json(p, s.review_read),
                           "base": status_review::esc_opt_cut(&p.base, 200),
                           "default_branch": status_review::esc_opt_cut(&p.default_branch, 200),
                           "merge_state": jopt(p.merge_state.as_deref())})
                })
                .collect();
            json!({
                "name": s.name, "path": s.path,
                "state": if s.error.is_some() { "error" } else { "ok" },
                "branch": jopt(s.facts.branch.as_deref()),
                "detached": s.facts.branch.is_none(),
                "head": jopt(s.facts.head.map(|h| short(&h.to_string()).to_string())),
                "upstream": jopt(s.facts.upstream.short.as_deref()),
                "upstream_gone": s.facts.upstream.gone,
                "ahead": s.facts.ahead, "behind": s.facts.behind,
                "changes": {"staged": s.changes.staged, "unstaged": s.changes.unstaged,
                            "untracked": s.changes.untracked, "conflicts": s.changes.conflicts},
                "stash": s.stash,
                "operation": jopt(s.op),
                "gitlink": {"recorded": jopt(s.pin.as_deref()), "state": jopt(s.pinstate)},
                "slug": jopt(s.slug.as_deref()),
                "attention": attn,
                "reasons": rs,
                "counts": counts_json(&counts_of(s)),
                "fetch": s.fetch.json(),
                "fetch_error": jopt(s.fetch.error.as_deref()),
                "worktree_list": s.worktree_list.as_ref().map_or(Value::Null, |l| Value::Array(l.iter().map(WorktreeInfo::json).collect())),
                "pull_requests": {"read": s.pr_read, "error": jopt(s.pr_error.as_deref()), "open": prs,
                                  "review": if s.review_read { status_review::tally(&s.prs).json() } else { Value::Null }},
                "error": jopt(s.error.as_deref()),
            })
        })
        .collect();
    let mut doc = json!({
        "schema_version": 2, "command": "status", "root": root,
        "options": {"fetch": opts.fetch, "worktrees": opts.worktrees, "icons": null, "review": opts.review},
        "engine": engine(),
        "github": gh.client.is_some(), "github_auth": jopt(gh.client.as_ref().map(|c| c.source.label())), "github_note": jopt(gh.note.as_deref()),
        "repos": repos,
        "summary": {"total": list.len(), "attention": attention},
        "totals": counts_json(&totals(list)),
    });
    if let (Some(stats), Value::Object(m)) = (stats, &mut doc) {
        m.insert("stats".into(), stats);
    }
    doc
}

/// The glyph set of the text output (`--icons`).
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Icons {
    Unicode,
    Nerd,
    Ascii,
}

impl Icons {
    /// Marker of a repository that needs attention, with its trailing space (two columns, like the blank marker).
    pub fn attention(self) -> &'static str {
        match self {
            Icons::Unicode => "\u{25b2} ",
            Icons::Nerd => "\u{f071} ",
            Icons::Ascii => "! ",
        }
    }

    /// Marker of a repository that cannot be read, with its trailing space.
    pub fn failed(self) -> &'static str {
        match self {
            Icons::Unicode => "\u{2716} ",
            Icons::Nerd => "\u{f057} ",
            Icons::Ascii => "! ",
        }
    }
}

/// What the output channels and the environment allow, as far as the icon default needs it.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct Terminal {
    /// stdout is a terminal.
    pub stdout: bool,
    /// The effective locale names UTF-8: the first of `LC_ALL`, `LC_CTYPE`, `LANG` that is set decides.
    pub utf8: bool,
    /// `TERM` is `dumb`.
    pub dumb: bool,
}

impl Terminal {
    /// From the values of the environment variables (None: unset); `stdout_tty` is whether stdout is a terminal.
    pub fn from_env_values(
        stdout_tty: bool,
        lc_all: Option<&str>,
        lc_ctype: Option<&str>,
        lang: Option<&str>,
        term: Option<&str>,
    ) -> Terminal {
        let locale = [lc_all, lc_ctype, lang]
            .into_iter()
            .flatten()
            .find(|v| !v.is_empty());
        let utf8 = locale.is_some_and(|v| {
            let v = v.to_ascii_lowercase();
            v.contains("utf-8") || v.contains("utf8")
        });
        Terminal {
            stdout: stdout_tty,
            utf8,
            dumb: term == Some("dumb"),
        }
    }

    pub fn detect() -> Terminal {
        use std::io::IsTerminal;
        let var = |k: &str| std::env::var(k).ok();
        Terminal::from_env_values(
            std::io::stdout().is_terminal(),
            var("LC_ALL").as_deref(),
            var("LC_CTYPE").as_deref(),
            var("LANG").as_deref(),
            var("TERM").as_deref(),
        )
    }
}

/// The glyph set of `--icons`: an explicit value always wins; the default is unicode on a terminal with a UTF-8
/// locale and ascii everywhere else (a pipe, a file, a locale that is not UTF-8, `TERM=dumb`).
pub fn resolve_icons(flag: Option<&str>, t: &Terminal) -> Icons {
    match flag {
        Some("unicode") => Icons::Unicode,
        Some("nerd") => Icons::Nerd,
        Some(_) => Icons::Ascii,
        None if t.stdout && t.utf8 && !t.dumb => Icons::Unicode,
        None => Icons::Ascii,
    }
}

/// How the text is laid out.
#[derive(Clone, Copy, Debug)]
pub struct View<'a> {
    pub icons: Icons,
    /// `--long`: a card per repository and the full explanation of every attention reason.
    pub long: bool,
    /// Text that follows the summary line (`--stats`, `--verbose`), newline-terminated; may be empty.
    pub diagnostics: &'a str,
}

/// What an attention reason means for this repository, in a sentence that ends the thought (it names no command
/// to run: stw never prints one for someone else). Text from outside is escaped.
pub fn explain(s: &RepoStatus, reason: &str) -> String {
    let c = &s.changes;
    let paths = |n: usize| format!("{n} path{}", if n == 1 { "" } else { "s" });
    let behind = s.facts.behind.unwrap_or(0);
    match reason {
        r if r.starts_with("operation:") => format!(
            "a {} is in progress: finish or abort it before anything else changes this checkout",
            &r["operation:".len()..]
        ),
        "conflicts" => format!(
            "{} with merge conflicts: resolve them and stage the result",
            paths(c.conflicts)
        ),
        "staged" => format!(
            "{} staged but not committed: commit them or unstage them",
            paths(c.staged)
        ),
        "unstaged" => format!(
            "{} with tracked changes in the working tree that are not staged: stage and commit them or discard them",
            paths(c.unstaged)
        ),
        "untracked" => format!(
            "{} untracked: add them, ignore them or remove them",
            paths(c.untracked)
        ),
        "behind" => format!(
            "{} is {behind} commit{} ahead of this branch: the branch can be fast-forwarded to it unless local changes are in the way",
            esc(s.facts.upstream.short.as_deref().unwrap_or("the upstream")),
            if behind == 1 { "" } else { "s" }
        ),
        "no-upstream" => format!(
            "the branch {} has no upstream: nothing says what it is compared with; push it with an upstream or set one",
            esc(s.facts.branch.as_deref().unwrap_or("?"))
        ),
        "detached" => format!(
            "HEAD is detached{}: switch to a branch before committing, or the commits are easy to lose",
            s.facts
                .head
                .map_or(String::new(), |h| format!(" at {}", short(&h.to_string())))
        ),
        "error" => format!(
            "the repository could not be read: {}",
            esc(s.error.as_deref().unwrap_or("unknown error"))
        ),
        other => esc(other),
    }
}

/// The facts of one repository, one per line (`--long`): where it is, what HEAD and the upstream are, the changes by
/// kind, the counts of branches, worktrees and stashes.
fn card(s: &RepoStatus) -> Vec<String> {
    if s.error.is_some() {
        return vec![format!("path: {}", esc(&s.path))];
    }
    let mut v = vec![format!("path: {}", esc(&s.path))];
    v.push(format!(
        "remote: {}",
        s.slug.as_deref().map_or("none".to_string(), esc)
    ));
    v.push(format!(
        "head: {}",
        s.facts
            .head
            .map_or("none (no commit yet)".to_string(), |h| short(
                &h.to_string()
            )
            .to_string())
    ));
    v.push(match (&s.facts.upstream.short, s.facts.upstream.gone) {
        (Some(u), _) => format!(
            "upstream: {} (ahead {}, behind {})",
            esc(u),
            s.facts.ahead.unwrap_or(0),
            s.facts.behind.unwrap_or(0)
        ),
        (None, true) => "upstream: gone".to_string(),
        (None, false) => "upstream: none".to_string(),
    });
    let c = &s.changes;
    v.push(format!(
        "changes: {} staged, {} unstaged, {} untracked, {} conflicts",
        c.staged, c.unstaged, c.untracked, c.conflicts
    ));
    let n = |o: Option<usize>| o.map_or("?".to_string(), |n| n.to_string());
    v.push(format!(
        "branches: {} local, {} remote; linked worktrees: {}; stash: {}",
        n(s.branches_local),
        n(s.branches_remote),
        n(s.worktrees),
        s.stash
    ));
    if let Some(pin) = &s.pin {
        v.push(format!(
            "gitlink: recorded {}, {}",
            short(pin),
            s.pinstate.unwrap_or("checked-out commit unknown")
        ));
    }
    v
}

/// Blocks, each followed by a blank line: `Counts`, `Needs action (N)`, `Repositories (N)`, `Notices (N)`, the
/// rules block `rules` (already rendered, may be empty), then the summary line.
pub fn render_human(
    github_note: Option<String>,
    extra_notices: &[String],
    list: &[RepoStatus],
    rules: &str,
    view: &View,
) -> String {
    let icons = view.icons;
    let mut out = counts_block(list);
    out.push('\n');
    let mut attention = 0;
    let mut todo = String::new();
    for s in list {
        let rs = reasons(s);
        if !rs.is_empty() {
            attention += 1;
            todo += &format!("  {:<18} {}\n", esc(&s.name), rs.join(", "));
            if view.long {
                for r in &rs {
                    todo += &format!("    - {r}: {}\n", explain(s, r));
                }
            }
        }
    }
    out += &format!("Needs action ({attention})\n");
    out += &if todo.is_empty() {
        "  nothing\n".to_string()
    } else {
        todo
    };
    out.push('\n');
    out += &format!("Repositories ({})\n", list.len());
    for s in list {
        let rs = reasons(s);
        let attn = !rs.is_empty();
        let mark = if s.error.is_some() {
            icons.failed()
        } else if attn {
            icons.attention()
        } else {
            "  "
        };
        let where_ = match &s.facts.branch {
            Some(b) => b.clone(),
            None => format!(
                "(detached {})",
                s.facts
                    .head
                    .map(|h| short(&h.to_string()).to_string())
                    .unwrap_or_default()
            ),
        };
        let line = if let Some(e) = &s.error {
            format!("error: {}", esc(e))
        } else {
            let (ahead, behind) = (s.facts.ahead.unwrap_or(0), s.facts.behind.unwrap_or(0));
            let mut l = if let Some(u) = &s.facts.upstream.short {
                if ahead == 0 && behind == 0 {
                    format!("{u} up to date")
                } else {
                    let mut l = u.clone();
                    if ahead > 0 {
                        l += &format!(" ahead {ahead}");
                    }
                    if behind > 0 {
                        l += &format!(" behind {behind}");
                    }
                    l
                }
            } else if s.facts.upstream.gone {
                "upstream gone".to_string()
            } else {
                "no upstream".to_string()
            };
            let c = &s.changes;
            let mut ch = String::new();
            for (n, w) in [
                (c.staged, "staged"),
                (c.unstaged, "unstaged"),
                (c.untracked, "untracked"),
                (c.conflicts, "conflicts"),
            ] {
                if n > 0 {
                    ch += &format!(" {n} {w}");
                }
            }
            if ch.is_empty() {
                ch = " clean".into();
            }
            l += &format!("  {ch}");
            l
        };
        out += &format!("{mark}{:<18} {:<30} {line}\n", esc(&s.name), esc(&where_));
        if view.long {
            for l in card(s) {
                out += &format!("    {l}\n");
            }
        }
        if let Some(op) = s.op {
            out += &format!("    operation in progress: {op}\n");
        }
        if s.stash > 0 {
            out += &format!(
                "    {} stash entr{}\n",
                s.stash,
                if s.stash == 1 { "y" } else { "ies" }
            );
        }
        if let Some(ps @ ("ahead" | "behind" | "diverged-or-unknown")) = s.pinstate {
            out += &format!("    submodule: checked-out commit is {ps} the recorded gitlink\n");
        }
        if let Some(e) = &s.fetch.error {
            out += &format!("    fetch failed: {}\n", esc(e));
        }
        for wt in s.worktree_list.iter().flatten() {
            out += &format!(
                "    worktree {}  {}",
                esc(&wt.path),
                wt.branch.as_deref().map_or("detached".to_string(), esc)
            );
            if wt.dirty {
                out += "  dirty";
            }
            if let Some(reason) = &wt.locked {
                out += "  locked";
                if !reason.is_empty() {
                    out += &format!(": {}", esc(reason));
                }
            }
            if wt.prunable {
                out += "  prunable";
            }
            if wt.missing {
                out += "  missing";
            }
            out.push('\n');
        }
        if let Some(e) = &s.pr_error {
            out += &format!("    pull requests: not read ({})\n", esc(e));
        } else if !s.prs.is_empty() {
            out += &format!(
                "    {} open pull request{}:\n",
                s.prs.len(),
                if s.prs.len() == 1 { "" } else { "s" }
            );
            for p in &s.prs {
                let mut fl = String::new();
                if p.draft {
                    fl += " draft";
                }
                if s.review_read {
                    fl += &format!(
                        ", review {}",
                        p.review.as_ref().map_or("not read", |r| r.text_label())
                    );
                }
                if let Some(m) = &p.merge_state {
                    fl += &format!(", {}", esc(m));
                }
                let hints = match s.review_read {
                    true => status_review::hints(p),
                    false => vec![],
                };
                out += &format!(
                    "      #{} {} [{}{}]{}\n",
                    p.number,
                    esc(&p.title),
                    esc(&p.head),
                    fl,
                    if hints.is_empty() {
                        String::new()
                    } else {
                        format!(" {{{}}}", hints.join(", "))
                    }
                );
                if view.long {
                    for l in p.review.iter().flat_map(status_review::evidence_lines) {
                        out += &format!("        {l}\n");
                    }
                    if s.review_read {
                        for l in status_review::hint_lines(p) {
                            out += &format!("        {l}\n");
                        }
                    }
                }
            }
            if s.review_read {
                out += &status_review::tally(&s.prs)
                    .text()
                    .map_or(String::new(), |t| format!("    {t}\n"));
            }
        }
    }
    out.push('\n');
    let mut notices: Vec<String> = github_note.into_iter().collect();
    for s in list {
        if let Some(e) = &s.fetch.error {
            notices.push(format!("{}: fetch failed ({})", esc(&s.name), esc(e)));
        }
        if let Some(e) = &s.pr_error {
            notices.push(format!(
                "{}: pull requests not read ({})",
                esc(&s.name),
                esc(e)
            ));
        }
        if let Some(e) = &s.issue_error {
            notices.push(format!("{}: issues not read ({})", esc(&s.name), esc(e)));
        }
        notices.extend(status_review::notices(s));
    }
    notices.extend(status_review::rate_limit_notice(list));
    notices.extend(extra_notices.iter().cloned());
    out += &format!("Notices ({})\n", notices.len());
    if notices.is_empty() {
        out += "  none\n";
    }
    for n in &notices {
        out += &format!("  {n}\n");
    }
    out.push('\n');
    if !rules.is_empty() {
        out += rules;
        out.push('\n');
    }
    if attention > 0 {
        out += &format!(
            "{attention} of {} repositories need attention\n",
            list.len()
        );
    } else {
        out += &format!("{} repositories, nothing needs attention\n", list.len());
    }
    out += view.diagnostics;
    out
}
