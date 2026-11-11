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
    pub issues: Option<IssueCounts>,
    pub issue_error: Option<String>,
    pub branches_local: Option<usize>,
    pub branches_remote: Option<usize>,
    pub worktrees: Option<usize>,
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
        issues: None,
        issue_error: None,
        branches_local: None,
        branches_remote: None,
        worktrees: None,
    }
}

/// The entry of a `.gitmodules` path that is not accepted as a repository: nothing is read from disk.
pub fn rejected_entry(name: &str, error: String) -> RepoStatus {
    let mut s = blank(name.to_string(), String::new());
    s.error = Some(error);
    s
}

pub fn collect_one(r: &RepoRef, root: &Path, gh: Option<&Client>) -> RepoStatus {
    let mut s = blank(r.name.clone(), p2s(&r.path));
    let mut run = || -> Result<()> {
        let repo = repo::open(&r.path)?;
        s.facts = head_facts(&repo)?;
        s.changes = changes(&repo, true)?;
        s.stash = stash_count(&repo);
        s.branches_local = repo::local_branches(&repo).ok().map(|b| b.len());
        s.branches_remote = remote_branch_count(&repo).ok();
        s.worktrees = crate::repo::worktrees::scan(&repo)
            .ok()
            .map(|w| w.linked.len() + w.relative.iter().filter(|l| l.dir.is_dir()).count());
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
        return s;
    }
    if let (Some(c), Some(slug)) = (gh, &s.slug) {
        match c.open_prs(slug) {
            Ok(p) => {
                s.prs = p;
                s.pr_read = true;
            }
            Err(e) => s.pr_error = Some(e.to_string().chars().take(200).collect()),
        }
        match c.open_issues(slug) {
            Ok(i) => s.issues = Some(i),
            Err(e) => s.issue_error = Some(e.to_string().chars().take(200).collect()),
        }
    }
    s
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

pub fn to_json(root: &str, gh: &GithubState, list: &[RepoStatus]) -> Value {
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
                           "author": p.author, "review": jopt(p.review.as_deref()),
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
                "pull_requests": {"read": s.pr_read, "error": jopt(s.pr_error.as_deref()), "open": prs},
                "error": jopt(s.error.as_deref()),
            })
        })
        .collect();
    json!({
        "schema_version": 2, "command": "status", "root": root,
        "engine": engine(),
        "github": gh.client.is_some(), "github_auth": jopt(gh.client.as_ref().map(|c| c.source.label())), "github_note": jopt(gh.note.as_deref()),
        "repos": repos,
        "summary": {"total": list.len(), "attention": attention},
        "totals": counts_json(&totals(list)),
    })
}

/// Blocks, each followed by a blank line: `Counts`, `Needs action (N)`, `Repositories (N)`, `Notices (N)`, the
/// rules block `rules` (already rendered, may be empty), then the summary line.
pub fn render_human(github_note: Option<String>, list: &[RepoStatus], rules: &str) -> String {
    let mut out = counts_block(list);
    out.push('\n');
    let mut attention = 0;
    let mut todo = String::new();
    for s in list {
        let rs = reasons(s);
        if !rs.is_empty() {
            attention += 1;
            todo += &format!("  {:<18} {}\n", esc(&s.name), rs.join(", "));
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
        let mark = if attn { '!' } else { ' ' };
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
        out += &format!("{mark} {:<18} {:<30} {line}\n", esc(&s.name), esc(&where_));
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
                if let Some(r) = &p.review {
                    fl += &format!(", {r}");
                }
                if let Some(m) = &p.merge_state {
                    fl += &format!(", {m}");
                }
                out += &format!("      #{} {} [{}{}]\n", p.number, p.title, p.head, fl);
            }
        }
    }
    out.push('\n');
    let mut notices: Vec<String> = github_note.into_iter().collect();
    for s in list {
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
    }
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
    out
}
