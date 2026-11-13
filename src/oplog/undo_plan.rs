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

/// `stw oplog undo REF --to OLD --expect NEW --yes`. With `shell` the ref name is quoted for pasting into a shell (the
/// text output); the JSON fields keep the raw name.
pub fn undo_command(reference: &str, to: &str, expect: &str, shell: bool) -> String {
    let name = if shell {
        shell_word(reference)
    } else {
        reference.to_string()
    };
    format!("stw oplog undo {name} --to {to} --expect {expect} --yes")
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
    for (b, dir) in &wt.in_use {
        if format!("refs/heads/{b}") == reference {
            p.push(
                "refused",
                format!(
                    "not executed: checked out in {}; merely resetting the ref leaves index/working tree inconsistent; use `git reset --keep`",
                    esc(&crate::repo::worktrees::show(dir))
                ),
            );
        }
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
                    true
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
