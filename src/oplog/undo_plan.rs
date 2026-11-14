//! `stw oplog undo-plan REF [--to OLD | --steps N]`: what an undo of one local branch would check and do, without doing
//! it (read-only; `docs/oplog-format.md`, 8). The checks are those of the accepted spike `stw-oplog-reflog undo-plan`.
//! The undo itself (`undo.rs`) runs this same plan first; this command only says `all checks ok` and the command that
//! applies it, or names what is refused.
use std::fmt::Write;

use gix::bstr::ByteSlice;
use gix::ObjectId;
use serde_json::{json, Value};

use super::parse::is_zero;
use super::store;
use super::timeline::SCHEMA_VERSION;
use super::Ctx;
use crate::error::{Error, Result};
use crate::git_runner::{self, Hex40, Op};
use crate::github::reads::escape_untrusted;

/// A word for a POSIX shell: unchanged when it holds only characters that no shell treats specially, else in single
/// quotes with each `'` written as `'\''`. Quoted only when needed, so the usual `refs/heads/work` reads as before.
pub fn shell_word(s: &str) -> String {
    let plain = !s.is_empty()
        && s.bytes()
            .all(|b| b.is_ascii_alphanumeric() || b"._/+@:=%,-".contains(&b));
    if plain {
        s.to_string()
    } else {
        format!("'{}'", s.replace('\'', "'\\''"))
    }
}

/// `stw oplog undo REF --to OLD --expect NEW [--keep] --yes`. With `shell` the ref name is quoted for pasting into a shell
/// (the text output); the JSON fields keep the raw name. `keep` adds `--keep` (the undo of a checked-out branch).
pub fn undo_command(reference: &str, to: &str, expect: &str, shell: bool, keep: bool) -> String {
    let name = if shell {
        shell_word(reference)
    } else {
        reference.to_string()
    };
    let keep = if keep { " --keep" } else { "" };
    format!("stw oplog undo {name} --to {to} --expect {expect}{keep} --yes")
}

/// Files of a git directory that mean an operation is running there (the markers the spike checks).
const MARKERS: [&str; 7] = [
    "rebase-merge",
    "rebase-apply",
    "MERGE_HEAD",
    "CHERRY_PICK_HEAD",
    "REVERT_HEAD",
    "BISECT_LOG",
    "sequencer",
];
/// Commits counted for the "reachable only through the reflog" note.
const WALK_LIMIT: usize = 10_000;

pub struct Request {
    pub reference: String,
    pub to: Option<String>,
    pub steps: Option<u64>,
    /// `--keep`: the branch is the one this worktree has checked out, and `git reset --keep` moves it (`undo.rs`).
    pub keep: bool,
}

pub struct Check {
    pub status: &'static str,
    pub text: String,
}

pub struct Plan {
    pub reference: String,
    pub checks: Vec<Check>,
    pub current: Option<String>,
    pub target: Option<String>,
    pub refused: usize,
    pub branch: bool,
    pub common: String,
    /// The plan is for the `--keep` path.
    pub keep: bool,
}

impl Plan {
    fn push(&mut self, status: &'static str, text: String) {
        if status == "refused" {
            self.refused += 1;
        }
        self.checks.push(Check { status, text });
    }
    pub fn executable(&self) -> bool {
        self.refused == 0 && self.branch
    }
}

fn esc(s: &str) -> String {
    escape_untrusted(s, 300).0
}

/// `HEAD`, a full ref name, or a branch name; anything else is a usage error.
pub fn normalize(r: &str) -> Result<String> {
    let full = if r == "HEAD" || r.starts_with("refs/") {
        r.to_string()
    } else {
        format!("refs/heads/{r}")
    };
    if full != "HEAD" && gix::validate::reference::name(full.as_bytes().as_bstr()).is_err() {
        return Err(Error::usage(format!(
            "not a valid ref name: {}",
            esc(&full)
        )));
    }
    Ok(full)
}

pub fn plan(ctx: &Ctx, req: &Request) -> Result<Plan> {
    let repo = ctx.repo()?;
    let reference = normalize(&req.reference)?;
    let mut p = Plan {
        reference: reference.clone(),
        checks: Vec::new(),
        current: None,
        target: None,
        refused: 0,
        branch: reference.starts_with("refs/heads/"),
        common: esc(&ctx.common.to_string_lossy()),
        keep: req.keep,
    };

    // the ref
    let cur: ObjectId = if reference == "HEAD" {
        match repo.head_id() {
            Ok(id) => id.detach(),
            Err(_) => {
                p.push("refused", "HEAD does not point at a commit".into());
                return Ok(p);
            }
        }
    } else {
        let found = repo.try_find_reference(reference.as_str()).ok().flatten();
        let Some(mut r) = found else {
            p.push(
                "refused",
                format!(
                    "ref {} does not exist (deleted? its reflog is gone with it)",
                    esc(&reference)
                ),
            );
            return Ok(p);
        };
        let symbolic = match r.target() {
            gix::refs::TargetRef::Symbolic(n) => Some(n.as_bstr().to_string()),
            gix::refs::TargetRef::Object(_) => None,
        };
        let Ok(id) = r.peel_to_id() else {
            p.push(
                "refused",
                format!("ref {} does not resolve to an object", esc(&reference)),
            );
            return Ok(p);
        };
        if let Some(t) = symbolic {
            // reported after the "ref = value" line below
            p.current = Some(id.to_string());
            p.push("ok", format!("ref {} = {}", esc(&reference), id));
            p.push(
                "refused",
                format!(
                    "{} is a symbolic ref (-> {}): an update would move its target; not supported",
                    esc(&reference),
                    esc(&t)
                ),
            );
            return finish(ctx, req, p, id.detach());
        }
        id.detach()
    };
    p.current = Some(cur.to_string());
    p.push("ok", format!("ref {} = {}", esc(&reference), cur));
    finish(ctx, req, p, cur)
}

/// The checks only `--keep` makes: the branch is checked out in this worktree (and nowhere else), no sparse checkout, a
/// clean index and a clean work tree (untracked files are no change). `git reset --keep` would keep some local changes;
/// stw does not rely on that.
fn keep_checks(p: &mut Plan, repo: &gix::Repository, here_holds: bool, other_holds: bool) {
    let reference = p.reference.clone();
    if repo.workdir().is_none() {
        p.push(
            "refused",
            "this repository has no work tree: --keep needs a checked-out branch".into(),
        );
        return;
    }
    if !here_holds {
        if !other_holds {
            let why = match repo.head_name() {
                Ok(None) => "HEAD is detached".to_string(),
                Ok(Some(n)) => format!("HEAD is on {}", esc(&n.as_bstr().to_string())),
                Err(e) => format!("HEAD cannot be read: {}", esc(&e.to_string())),
            };
            p.push(
                "refused",
                format!(
                    "{} is not checked out in this worktree ({why}); --keep moves the branch this worktree has checked out. Without --keep only the ref moves",
                    esc(&reference)
                ),
            );
        }
        return;
    }
    p.push("ok", "checked out in this worktree".into());
    if repo
        .config_snapshot()
        .boolean("core.sparseCheckout")
        .unwrap_or(false)
    {
        p.push(
            "refused",
            "core.sparseCheckout is set: --keep does not handle a sparse checkout".into(),
        );
    }
    keep_index_flags(p, repo);
    match crate::status::changes_opts(repo, false, true) {
        Err(e) => p.push(
            "refused",
            format!(
                "the work tree status cannot be read ({}): treated as not clean",
                esc(&e.to_string())
            ),
        ),
        Ok(c) => {
            let mut dirty = false;
            if c.conflicts > 0 {
                dirty = true;
                p.push(
                    "refused",
                    format!(
                        "{} unmerged path(s): resolve or abort the merge first",
                        c.conflicts
                    ),
                );
            }
            if c.staged > 0 {
                dirty = true;
                p.push(
                    "refused",
                    format!(
                        "index has staged changes ({} path(s)): --keep needs a clean index",
                        c.staged
                    ),
                );
            }
            if c.unstaged > 0 {
                dirty = true;
                p.push(
                    "refused",
                    format!(
                        "work tree has unstaged changes ({} path(s)): --keep needs a clean work tree",
                        c.unstaged
                    ),
                );
            }
            if !dirty {
                p.push(
                    "ok",
                    "index and work tree are clean (untracked files do not count)".into(),
                );
            }
        }
    }
}

/// Index entries that status does not show as a change but that `git reset --keep` handles in its own way: intent-to-add
/// (`git add -N`, the marker would be lost), assume-unchanged and skip-worktree (a local edit is invisible to status).
fn keep_index_flags(p: &mut Plan, repo: &gix::Repository) {
    use gix::index::entry::Flags;
    let Ok(index) = repo.index_or_empty() else {
        return;
    };
    let count = |f: Flags| {
        index
            .entries()
            .iter()
            .filter(|e| e.flags.intersects(f))
            .count()
    };
    let (ita, hidden) = (
        count(Flags::INTENT_TO_ADD),
        count(Flags::ASSUME_VALID | Flags::SKIP_WORKTREE),
    );
    if ita > 0 {
        p.push(
            "refused",
            format!("{ita} intent-to-add entr(ies) (`git add -N`): --keep would lose the marker; add or remove them first"),
        );
    }
    if hidden > 0 {
        p.push(
            "refused",
            format!("{hidden} assume-unchanged or skip-worktree entr(ies): a local change there is invisible to the status check; --keep does not handle them"),
        );
    }
}

/// The target of `git reset --keep` as the runner takes it (40 hex digits), and a git that can run and knows `reset
/// --keep` (2.36): the reasons a `--keep` undo is refused before any ref is written. Starts `git --version` (once).
pub fn keep_git_check(to: &str) -> std::result::Result<Hex40, String> {
    let target = Hex40::parse(to).ok_or_else(|| {
        format!(
            "--keep supports 40-digit object ids only (the runner of git takes no other); the target is {}",
            esc(to)
        )
    })?;
    let found = git_runner::shared()
        .version()
        .map_err(|e| format!("--keep needs a usable git: {e}"))?;
    let needed = Op::ResetKeep { target }.min_version();
    if found < needed {
        return Err(format!(
            "--keep needs git {} or newer; the installed git is {found}",
            needed.short()
        ));
    }
    Ok(target)
}

/// What `git reset --keep` would do to the tree, looked at before anything is written: `git` itself, the gitlinks and the
/// paths the target adds. `reset --keep` leaves submodule checkouts alone, so a target whose tree has another gitlink than
/// the current one would leave a submodule on the wrong commit; and a path that the target adds is overwritten without
/// a question when an untracked or an ignored file (or a file where a directory is needed) is there. Trees are compared
/// by path, no rename tracking; the work tree is clean, so what is there and not in the current tree is untracked or ignored.
fn keep_target_checks(p: &mut Plan, repo: &gix::Repository, cur: ObjectId, alt: ObjectId) {
    use gix::diff::tree_with_rewrites::Change;
    if let Err(why) = keep_git_check(&alt.to_string()) {
        p.push("refused", why);
        return;
    }
    let trees = (|| -> std::result::Result<_, String> {
        let tree_of = |id: ObjectId| {
            repo.find_commit(id)
                .map_err(|e| e.to_string())?
                .tree()
                .map_err(|e| e.to_string())
        };
        let (a, b) = (tree_of(cur)?, tree_of(alt)?);
        let options = gix::diff::Options::default().with_rewrites(None);
        repo.diff_tree_to_tree(&a, &b, options)
            .map_err(|e| e.to_string())
    })();
    let changes = match trees {
        Err(e) => {
            p.push(
                "refused",
                format!("the trees of the two values cannot be compared ({}): --keep needs to know that no gitlink differs and nothing is in the way", esc(&e)),
            );
            return;
        }
        Ok(c) => c,
    };
    let hit = changes.iter().find_map(|c| match c {
        Change::Addition {
            location,
            entry_mode,
            ..
        }
        | Change::Deletion {
            location,
            entry_mode,
            ..
        } if entry_mode.is_commit() => Some(location.clone()),
        Change::Modification {
            location,
            previous_entry_mode,
            entry_mode,
            ..
        } if previous_entry_mode.is_commit() || entry_mode.is_commit() => Some(location.clone()),
        _ => None,
    });
    if let Some(path) = hit {
        p.push(
            "refused",
            format!(
                "the two values differ in the gitlink {}: `git reset --keep` leaves submodule checkouts as they are, so one would stay on the wrong commit",
                esc(&path.to_string())
            ),
        );
        return;
    }
    p.push("ok", "no gitlink differs between the two values".into());
    let Some(workdir) = repo.workdir() else {
        return;
    };
    let deleted: std::collections::BTreeSet<Vec<u8>> = changes
        .iter()
        .filter_map(|c| match c {
            Change::Deletion { location, .. } => Some(location.to_vec()),
            _ => None,
        })
        .collect();
    let mut blocked: Vec<String> = Vec::new();
    for c in &changes {
        // a directory is reported too, and its entries one by one: only the entries are looked at
        if let Change::Addition {
            location,
            entry_mode,
            ..
        } = c
        {
            if !entry_mode.is_tree() && in_the_way(workdir, location.as_ref(), &deleted) {
                blocked.push(esc(&location.to_string()));
            }
        }
    }
    if blocked.is_empty() {
        p.push(
            "ok",
            "no untracked or ignored file is in the way of a path the target adds".into(),
        );
    } else {
        let more = blocked.len().saturating_sub(3);
        let mut shown = blocked.into_iter().take(3).collect::<Vec<_>>().join(", ");
        if more > 0 {
            shown.push_str(&format!(" and {more} more"));
        }
        p.push(
            "refused",
            format!("the target adds {shown}, but an untracked or ignored file or directory is there (or in the way of its directory): `git reset --keep` would overwrite it; move it away first"),
        );
    }
}

/// An entry exists in the work tree where the target adds `location`, or a file stands where one of its directories
/// is needed (a file that the target itself deletes does not count).
fn in_the_way(
    workdir: &std::path::Path,
    location: &gix::bstr::BStr,
    deleted: &std::collections::BTreeSet<Vec<u8>>,
) -> bool {
    use std::os::unix::ffi::OsStrExt;
    let rel: &[u8] = location.as_ref();
    let at = |bytes: &[u8]| workdir.join(std::ffi::OsStr::from_bytes(bytes));
    if at(rel).symlink_metadata().is_ok() {
        return true;
    }
    rel.iter()
        .enumerate()
        .filter(|(_, b)| **b == b'/')
        .any(|(i, _)| {
            let dir = &rel[..i];
            at(dir)
                .symlink_metadata()
                .is_ok_and(|m| !m.is_dir() && !deleted.contains(dir))
        })
}

fn finish(ctx: &Ctx, req: &Request, mut p: Plan, cur: ObjectId) -> Result<Plan> {
    let repo = ctx.repo()?;
    let reference = p.reference.clone();
    if p.branch {
        p.push("ok", "type: local branch".into());
    } else {
        p.push(
            "note",
            "type: not a local branch; `undo` would refuse it (tags, remote-tracking refs, stash, HEAD, refs/stw)".into(),
        );
    }

    // checked out, registrations without a readable git dir, running operations
    let wt = crate::repo::worktrees::scan(repo)?;
    // the worktree this command runs in: the only one `--keep` may move
    let here = repo.workdir().and_then(|d| std::fs::canonicalize(d).ok());
    let (mut here_holds, mut other_holds) = (false, false);
    for (b, dir) in &wt.in_use {
        if format!("refs/heads/{b}") != reference {
            continue;
        }
        if req.keep && here.is_some() && std::fs::canonicalize(dir).ok() == here {
            here_holds = true;
            continue;
        }
        other_holds = true;
        let shown = esc(&crate::repo::worktrees::show(dir));
        if req.keep {
            p.push(
                "refused",
                format!(
                    "not executed: checked out in another worktree ({shown}); --keep moves only the branch of the worktree it runs in"
                ),
            );
        } else {
            p.push(
                "refused",
                format!(
                    "not executed: checked out in {shown}; merely resetting the ref leaves index/working tree inconsistent; add --keep (clean index and work tree only) or use `git reset --keep`"
                ),
            );
        }
    }
    if req.keep {
        keep_checks(&mut p, repo, here_holds, other_holds);
    }
    for s in &wt.stale {
        p.push(
            "refused",
            format!(
                "worktree {} has no readable git dir (missing or prunable): cannot tell whether an operation runs there; run `git worktree prune` or restore it",
                esc(&crate::repo::worktrees::show(&s.path))
            ),
        );
    }
    let mut dirs = vec![ctx.common.clone()];
    if let Ok(rd) = std::fs::read_dir(ctx.common.join("worktrees")) {
        let mut v: Vec<_> = rd
            .flatten()
            .map(|e| e.path())
            .filter(|p| p.is_dir())
            .collect();
        v.sort();
        dirs.extend(v);
    }
    for d in dirs {
        for m in MARKERS {
            if d.join(m).symlink_metadata().is_ok() {
                p.push(
                    "refused",
                    format!(
                        "operation in progress ({m} in git dir {}): undo while an operation runs can break it; finish or abort it first. Limit: only these markers are checked ({})",
                        esc(&d.to_string_lossy()),
                        MARKERS.join(" ")
                    ),
                );
            }
        }
    }

    // the reflog
    let log_path = if reference == "HEAD" {
        repo.git_dir().join("logs").join("HEAD")
    } else {
        ctx.common.join("logs").join(&reference)
    };
    let rows = store::read_reflog_file(&log_path).unwrap_or_default();
    if rows.is_empty() {
        p.push(
            "refused",
            format!(
                "no reflog entries for {}: no old value known",
                esc(&reference)
            ),
        );
        return Ok(p);
    }
    let tip = &rows[rows.len() - 1].new;
    let cur_s = cur.to_string();
    if *tip == cur_s {
        p.push(
            "ok",
            format!("current value equals the newest reflog value ({tip})"),
        );
    } else {
        p.push(
            "refused",
            format!("ref changed since: current value {cur}, newest reflog value {tip} (changed without a reflog entry, or the log is out of date): rejected"),
        );
    }

    // the target
    let sel: usize = if let Some(to) = &req.to {
        if to.is_empty() || !to.bytes().all(|b| matches!(b, b'0'..=b'9' | b'a'..=b'f')) {
            p.push("refused", "--to must be lower-case hex".into());
            return Ok(p);
        }
        let mut hit = None;
        let mut olds: Vec<&str> = Vec::new();
        for (i, r) in rows.iter().enumerate() {
            if r.old.starts_with(to.as_str()) && !is_zero(&r.old) {
                hit = Some(i + 1);
                if !olds.contains(&r.old.as_str()) {
                    olds.push(&r.old);
                }
            }
        }
        let Some(hit) = hit else {
            p.push(
                "refused",
                format!(
                    "old value {to} is not an old value in the reflog of {}",
                    esc(&reference)
                ),
            );
            return Ok(p);
        };
        if olds.len() > 1 {
            p.push("refused", format!("--to {to} is ambiguous: {} different old values in the reflog start with it; give more digits", olds.len()));
            return Ok(p);
        }
        hit
    } else {
        let n = req.steps.unwrap_or(1) as usize;
        if n == 0 || n > rows.len() {
            p.push("refused", format!("the reflog has fewer than {n} entries"));
            return Ok(p);
        }
        rows.len() - n + 1
    };
    let alt = rows[sel - 1].old.clone();
    let skipped = rows.len() - sel;
    p.target = Some(alt.clone());
    p.push("ok", format!("target: old value of reflog entry {sel}: {alt} ({skipped} later reflog entries are undone as well)"));
    if is_zero(&alt) {
        p.push(
            "refused",
            "the selected entry created the ref; undo would delete it: not supported".into(),
        );
        return Ok(p);
    }

    // the old value
    let alt_id = ObjectId::from_hex(alt.as_bytes())
        .ok()
        .filter(|id| repo.find_commit(*id).is_ok());
    let Some(alt_id) = alt_id else {
        p.push("refused", format!("old value {alt} is not a commit object here (gc removed it, or it is not a commit): cannot restore"));
        return Ok(p);
    };
    p.push("ok", "old value exists as a commit object".into());
    if req.keep && p.refused == 0 {
        keep_target_checks(&mut p, repo, cur, alt_id);
    }
    let base = repo
        .merge_base(alt_id, cur)
        .ok()
        .flatten()
        .map(|b| b.detach());
    let relation = if base == Some(alt_id) {
        "old value is an ancestor of the current value (plain move back)"
    } else if base == Some(cur) {
        "current value is an ancestor of the old value (move forward)"
    } else {
        "old and current value diverged (for example after amend or rebase)"
    };
    p.push("note", relation.into());

    // commits that only the reflog / recovery ref would keep
    let mut hidden = vec![alt_id];
    if let Ok(platform) = repo.references() {
        if let Ok(iter) = platform.all() {
            for mut r in iter.flatten() {
                if r.name().as_bstr() == reference.as_str() {
                    continue;
                }
                if let Ok(id) = r.peel_to_id() {
                    if repo.find_commit(id.detach()).is_ok() {
                        hidden.push(id.detach());
                    }
                }
            }
        }
    }
    if reference != "HEAD" {
        if let Ok(id) = repo.head_id() {
            hidden.push(id.detach());
        }
    }
    let mut lost: Vec<String> = Vec::new();
    let mut count = 0usize;
    let mut capped = false;
    if let Ok(walk) = repo.rev_walk([cur]).with_hidden(hidden).all() {
        for info in walk.flatten() {
            if count >= WALK_LIMIT {
                capped = true;
                break;
            }
            count += 1;
            if lost.len() < 10 {
                let subject = repo
                    .find_commit(info.id)
                    .ok()
                    .and_then(|c| {
                        c.message()
                            .ok()
                            .map(|m| m.title.to_str_lossy().into_owned())
                    })
                    .unwrap_or_default();
                lost.push(format!("{} {}", &info.id.to_string()[..7], esc(&subject)));
            }
        }
    }
    if count == 0 {
        p.push(
            "ok",
            "no commit would become reachable only through the reflog/recovery ref".into(),
        );
    } else {
        let mut t = format!(
            "{count}{} commits would be reachable only through the reflog / recovery ref after the undo (newest first, at most 10):",
            if capped { "+" } else { "" }
        );
        for l in lost {
            t.push_str("\n             ");
            t.push_str(&l);
        }
        p.push("note", t);
    }
    Ok(p)
}

impl Plan {
    fn result(&self) -> String {
        if self.refused > 0 {
            format!(
                "Result: not executable ({} refused). Nothing was changed.",
                self.refused
            )
        } else if !self.branch {
            "Result: preview ok, but `undo` handles local branches only.".into()
        } else {
            format!(
                "Result: all checks ok (preview only, nothing was changed). Apply with: {}",
                undo_command(
                    &self.reference,
                    self.target.as_deref().unwrap_or("?"),
                    self.current.as_deref().unwrap_or("?"),
                    true,
                    self.keep
                )
            )
        }
    }

    /// The `undo` command that applies this plan (full values for `--to` and `--expect`; the raw ref name, as in JSON).
    pub fn apply_command(&self) -> String {
        undo_command(
            &self.reference,
            self.target.as_deref().unwrap_or("?"),
            self.current.as_deref().unwrap_or("?"),
            false,
            self.keep,
        )
    }

    pub fn to_json(&self) -> Value {
        self.to_json_as("oplog undo-plan")
    }

    /// The same document for `command` (`undo` prints it when it refuses); `apply` is new and only set when executable.
    pub fn to_json_as(&self, command: &str) -> Value {
        let apply = self.executable().then(|| self.apply_command());
        let checks: Vec<Value> = self
            .checks
            .iter()
            .map(|c| json!({ "status": c.status, "text": c.text }))
            .collect();
        json!({
            "schema_version": SCHEMA_VERSION, "command": command, "engine": crate::output::engine(),
            "ref": self.reference, "repo": self.common, "current": self.current, "target": self.target,
            "checks": checks, "executable": self.executable(), "result": self.result(), "apply": apply,
            "keep": self.keep,
        })
    }

    pub fn render_human(&self) -> String {
        self.render_human_as("undo-plan", "preview only, nothing is written")
    }

    pub fn render_human_as(&self, command: &str, note: &str) -> String {
        let mut b = String::new();
        let _ = writeln!(
            b,
            "{command} {}  git-dir={}  ({note})",
            esc(&self.reference),
            self.common
        );
        let _ = writeln!(b, "Checks (nothing is changed):");
        for c in &self.checks {
            let word = match c.status {
                "ok" => "ok",
                "refused" => "refused",
                _ => "note",
            };
            let _ = writeln!(b, "  {word:<8} {}", c.text);
        }
        let _ = writeln!(b, "{}", self.result());
        b
    }
}
