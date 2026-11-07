//! `stw sync`: bring every repository up to date, like `git pull --ff-only`.
//! Dry run by default. Never merges, rebases, stashes, resets or pushes. `--apply` fast-forwards the
//! checked-out branch (index and working tree included) when no local change overlaps.
use serde_json::{json, Value};

use crate::github::Client;

pub mod fast_forward;
pub mod fetch;
pub mod rules;

use crate::error::Result;
use crate::output::*;
use crate::repo::{self, RepoRef};
use crate::status::{self, HeadFacts};
use fast_forward::fast_forward;

#[derive(Clone, Debug, Default)]
pub struct SyncRec {
    pub name: String,
    pub path: String,
    pub branch: Option<String>,
    pub upstream: Option<String>,
    pub ahead: Option<usize>,
    pub behind: Option<usize>,
    pub dirty: usize,
    pub fetch: String,
    pub fetched_ago: Option<u64>,
    pub remote_moved: Option<bool>,
    pub result: String,
    pub reason: String,
    pub err: Option<String>,
}

fn plan_of(f: &HeadFacts, op: Option<&str>) -> (&'static str, String) {
    let (a, b) = (f.ahead.unwrap_or(0), f.behind.unwrap_or(0));
    if let Some(op) = op {
        ("skip", format!("{op} in progress"))
    } else if f.branch.is_none() {
        ("skip", "detached HEAD".into())
    } else if f.upstream.gone {
        ("skip", "upstream branch is gone".into())
    } else if f.upstream.short.is_none() {
        ("skip", "no upstream configured".into())
    } else if b > 0 && a > 0 {
        ("skip", format!("diverged: ahead {a}, behind {b}"))
    } else if b > 0 {
        ("fast-forward", format!("behind {b}"))
    } else if a > 0 {
        (
            "none",
            format!("ahead {a}, nothing to pull (push is yours)"),
        )
    } else {
        ("none", "up to date".into())
    }
}

pub struct Opts {
    pub apply: bool,
    pub offline: bool,
}

pub fn sync_repo(r: &RepoRef, gh: Option<&Client>, o: &Opts) -> SyncRec {
    let mut rec = SyncRec {
        name: r.name.clone(),
        path: p2s(&r.path),
        fetch: "not-run".into(),
        ..Default::default()
    };
    if let Err(e) = sync_inner(r, gh, o, &mut rec) {
        rec.result = "error".into();
        rec.reason = "cannot read repository state".into();
        rec.err = Some(e.to_string());
    }
    rec
}

fn sync_inner(r: &RepoRef, gh: Option<&Client>, o: &Opts, rec: &mut SyncRec) -> Result<()> {
    let repo = repo::open(&r.path)?;
    let mut facts = status::head_facts(&repo)?;
    let mut dirty = status::changes(&repo, false)?.dirty_paths;
    let op = status::operation(&repo);
    rec.fetched_ago = std::fs::metadata(repo.git_dir().join("FETCH_HEAD"))
        .and_then(|m| m.modified())
        .ok()
        .and_then(|t| std::time::SystemTime::now().duration_since(t).ok())
        .map(|d| d.as_secs());
    // dry run: ask GitHub read-only whether the upstream branch moved (replaces `git ls-remote`)
    if !o.offline {
        if let (Some(b), Some(_), Some(c)) = (&facts.branch, &facts.upstream.short, gh) {
            let remote = repo
                .config_snapshot()
                .string(format!("branch.{b}.remote").as_str())
                .map(|v| v.to_string());
            let merge = repo
                .config_snapshot()
                .string(format!("branch.{b}.merge").as_str())
                .map(|v| v.to_string());
            if let (Some(rem), Some(m), Some(slug)) = (remote, merge, repo::slug(&repo)) {
                if rem == "origin" {
                    if let Ok(Some(sha)) =
                        c.remote_branch_sha(&slug, m.strip_prefix("refs/heads/").unwrap_or(&m))
                    {
                        rec.remote_moved =
                            Some(Some(sha) != facts.upstream.tip.map(|t| t.to_string()));
                    }
                }
            }
        }
    }
    let (mut action, mut reason) = plan_of(&facts, op);
    let mut result = action.to_string();
    let mut err = None;
    if o.apply {
        if o.offline {
            rec.fetch = "skipped-offline".into();
        } else {
            let fetched = fetch::fetch_origin(&repo, gh);
            rec.fetch = fetched.label().into();
            err = fetched.message().map(str::to_string);
            if rec.fetch == "failed" && gh.is_none() {
                err = err.map(|e| {
                    format!(
                        "{e}; no GitHub token was found: if the repository is private, set GH_TOKEN or GITHUB_TOKEN to a token that can read it"
                    )
                });
            }
        }
        facts = status::head_facts(&repo)?;
        dirty = status::changes(&repo, false)?.dirty_paths;
        (action, reason) = plan_of(&facts, status::operation(&repo));
        result = action.to_string();
        if rec.fetch == "failed" {
            result = "error".into();
            reason = "fetch failed".into();
        } else if rec.fetch == "unsupported-remote" {
            result = "error".into();
            reason = "fetch failed: use git directly for this remote kind".into();
        }
        if result == "fast-forward" {
            let up = facts.upstream.short.clone().unwrap_or_default();
            match fast_forward(&repo, facts.upstream.tip.expect("behind implies tip"), &up) {
                Ok((a, b)) => {
                    result = "fast-forwarded".into();
                    reason = format!("{a}..{b}");
                    err = None;
                }
                Err(e) => {
                    result = "error".into();
                    reason = "fast-forward refused".into();
                    err = Some(e.to_string());
                }
            }
        }
    }
    if dirty > 0 && result == "fast-forward" {
        reason += &format!("; working tree has {dirty} changed file(s), refused if they overlap");
    }
    rec.branch = facts.branch.clone();
    rec.upstream = facts.upstream.short.clone();
    rec.ahead = facts.ahead;
    rec.behind = facts.behind;
    rec.dirty = dirty;
    rec.result = result;
    rec.reason = reason;
    rec.err = err;
    Ok(())
}

pub fn to_json(root: &str, apply: bool, list: &[SyncRec]) -> Value {
    let count = |r: &str| list.iter().filter(|x| x.result == r).count();
    let repos: Vec<Value> = list
        .iter()
        .map(|s| {
            json!({"name": s.name, "path": s.path, "branch": jopt(s.branch.as_deref()), "upstream": jopt(s.upstream.as_deref()),
                   "ahead": s.ahead, "behind": s.behind, "dirty_files": s.dirty, "fetch": s.fetch,
                   "last_fetch_seconds_ago": s.fetched_ago, "remote_moved": s.remote_moved,
                   "result": s.result, "reason": s.reason, "error": jopt(s.err.as_deref())})
        })
        .collect();
    json!({"schema_version": 2, "command": "sync", "mode": if apply {"apply"} else {"dry-run"}, "root": root,
           "engine": engine(), "repos": repos,
           "summary": {"total": list.len(), "fast_forwarded": count("fast-forwarded"),
                       "would_fast_forward": count("fast-forward"), "skipped": count("skip"), "errors": count("error")}})
}

pub fn render_human(apply: bool, list: &[SyncRec]) -> String {
    let mut out = String::new();
    for s in list {
        let mut note = String::new();
        if !apply {
            match s.remote_moved {
                Some(true) => note = "  (remote has news: fetch needed)".into(),
                Some(false) => note = "  (remote unchanged since last fetch)".into(),
                None => {}
            }
        }
        out += &format!(
            "{:<18} {:<26} {:<16} {}{}\n",
            s.name,
            s.branch.as_deref().unwrap_or("(detached)"),
            s.result,
            s.reason,
            note
        );
        if let Some(e) = &s.err {
            out += &format!("    error: {e}\n");
        }
    }
    let count = |r: &str| list.iter().filter(|x| x.result == r).count();
    if apply {
        out += &format!(
            "{} fast-forwarded, {} skipped, {} failed\n",
            count("fast-forwarded"),
            count("skip"),
            count("error")
        );
    } else {
        out += &format!(
            "dry run: {} would fast-forward, {} skipped. Nothing was written.\n",
            count("fast-forward"),
            count("skip")
        );
    }
    out
}
