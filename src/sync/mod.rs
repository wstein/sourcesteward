//! `stw sync`: bring every repository up to date, like `git pull --ff-only`.
//! Dry run by default. Never merges, rebases, stashes, resets or pushes. `--apply` fast-forwards the
//! checked-out branch (index and working tree included) when no local change overlaps.
//!
//! `--prune` / `--no-prune` choose how the fetch treats remote-tracking refs that are gone on `origin`; only
//! `refs/remotes/origin/*` can be deleted (`fetch::check_prune_scope`), by the installed git's own fetch, before
//! any fast-forward. `--submodules` then moves each pinned (detached) submodule to the commit the superproject
//! records, when that is a fast-forward (`submodules.rs`). Both act only with `--apply`.
use std::collections::BTreeSet;

use serde_json::{json, Value};

use crate::github::Client;

pub mod fast_forward;
pub mod fetch;
pub mod rules;
pub mod submodules;

use crate::error::Result;
use crate::output::*;
use crate::repo::{self, RepoRef};
use crate::status::{self, HeadFacts};
use fast_forward::fast_forward;
use fetch::Prune;
pub use submodules::{submodule_pass, SubRec};

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
    /// How the fetch ran: `git` (runner), `gix`; None when none ran.
    pub via: Option<String>,
    /// Typed error kind of a failed git step.
    pub git_error: Option<String>,
    pub fetched_ago: Option<u64>,
    pub remote_moved: Option<bool>,
    pub result: String,
    pub reason: String,
    pub err: Option<String>,
    /// `--prune` (`"on"`) or `--no-prune` (`"off"`) was given; None without either.
    pub prune: Option<&'static str>,
    /// Remote-tracking refs of `origin` that the fetch deleted (apply). Also set when the fetch failed afterwards.
    pub pruned: Vec<String>,
    /// Dry run with `--prune`: how many refs under `refs/remotes/origin/` are candidates.
    pub tracking_refs: Option<usize>,
    /// `--submodules`: what happened to this pinned submodule.
    pub submodule: Option<SubRec>,
}

/// The names of the remote-tracking refs of `origin` (`refs/remotes/origin/*`) except `origin/HEAD`, which a fetch
/// never prunes.
fn tracking_refs(path: &std::path::Path) -> Result<BTreeSet<String>> {
    let repo = repo::open(path)?;
    let mut out = BTreeSet::new();
    for r in repo.references()?.prefixed(fetch::PRUNE_SCOPE)? {
        let r = r.map_err(|e| crate::error::Error::repository(e.to_string()))?;
        let name = r.name().as_bstr().to_string();
        if name != format!("{}HEAD", fetch::PRUNE_SCOPE) {
            out.insert(name);
        }
    }
    Ok(out)
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

#[derive(Clone, Debug, Default)]
pub struct Opts {
    pub apply: bool,
    pub offline: bool,
    pub prune: Prune,
    pub submodules: bool,
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
    rec.prune = match o.prune {
        Prune::Unset => None,
        Prune::On => Some("on"),
        Prune::Off => Some("off"),
    };
    let mut prune_note = String::new();
    if !o.apply && o.prune == Prune::On && repo.find_remote("origin").is_ok() {
        // a dry run never fetches: it can only count what a prune could reach
        let n = tracking_refs(&r.path).map(|s| s.len())?;
        rec.tracking_refs = Some(n);
        prune_note = format!(
            "; --prune would delete the remote-tracking refs of origin that are gone there ({n} under {} checked)",
            fetch::PRUNE_SCOPE
        );
    } else if !o.apply && o.prune == Prune::Off {
        prune_note = "; --no-prune: the fetch would not prune".into();
    }
    if o.apply {
        if o.offline {
            rec.fetch = "skipped-offline".into();
        } else {
            // also without a flag: `fetch.prune` of the configuration prunes a fetch by the installed git
            let before = tracking_refs(&r.path)?;
            let fetched = fetch::fetch_origin_with(&repo, gh, o.prune);
            rec.fetch = fetched.label().into();
            rec.via = fetched.via.map(str::to_string);
            rec.git_error = fetched.git_error.map(str::to_string);
            err = fetched.message().map(str::to_string);
            // after the fetch, also after a failed one: a partial prune must not stay hidden
            let after = tracking_refs(&r.path)?;
            rec.pruned = before.difference(&after).cloned().collect();
            if rec.fetch == "failed"
                && gh.is_none()
                && rec.git_error.as_deref() != Some("refused-refspec")
            {
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
            if !rec.pruned.is_empty() {
                reason += &format!(
                    "; {} remote-tracking ref(s) were pruned before it failed",
                    rec.pruned.len()
                );
            }
        } else if rec.fetch == "unsupported" {
            result = "error".into();
            reason = "fetch needs the installed git, which cannot be used".into();
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
    reason += &prune_note;
    if !rec.pruned.is_empty() && rec.fetch == "ok" {
        let shown: Vec<String> = rec
            .pruned
            .iter()
            .take(5)
            .map(|n| crate::github::reads::escape_untrusted(n, 200).0)
            .collect();
        let more = rec.pruned.len().saturating_sub(5);
        reason += &format!(
            "; pruned {} remote-tracking ref(s): {}{}",
            rec.pruned.len(),
            shown.join(", "),
            if more > 0 {
                format!(", and {more} more")
            } else {
                String::new()
            }
        );
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

pub fn to_json(root: &str, apply: bool, list: &[SyncRec], o: &Opts) -> Value {
    let count = |r: &str| list.iter().filter(|x| x.result == r).count();
    let repos: Vec<Value> = list
        .iter()
        .map(|s| {
            let mut v = json!({"name": s.name, "path": s.path, "branch": jopt(s.branch.as_deref()), "upstream": jopt(s.upstream.as_deref()),
                   "ahead": s.ahead, "behind": s.behind, "dirty_files": s.dirty, "fetch": s.fetch,
                   "last_fetch_seconds_ago": s.fetched_ago, "remote_moved": s.remote_moved,
                   "result": s.result, "reason": s.reason, "error": jopt(s.err.as_deref())});
            // additive: present only when a fetch ran
            if let Some(via) = &s.via {
                v["via"] = json!(via);
            }
            if let Some(k) = &s.git_error {
                v["git_error"] = json!(k);
            }
            // additive: only with --prune / --no-prune or when a ref was pruned
            if let Some(p) = s.prune {
                v["prune"] = json!(p);
            }
            if s.prune.is_some() || !s.pruned.is_empty() {
                v["pruned"] = json!(s.pruned);
            }
            if let Some(n) = s.tracking_refs {
                v["tracking_refs"] = json!(n);
            }
            if let Some(m) = &s.submodule {
                v["submodule"] = m.to_json();
            }
            v
        })
        .collect();
    json!({"schema_version": 2, "command": "sync", "mode": if apply {"apply"} else {"dry-run"}, "root": root,
           "engine": engine(), "repos": repos,
           "options": {"prune": match o.prune { Prune::Unset => Value::Null, Prune::On => json!(true), Prune::Off => json!(false) },
                       "submodules": o.submodules},
           "summary": {"total": list.len(), "fast_forwarded": count("fast-forwarded"),
                       "would_fast_forward": count("fast-forward"), "skipped": count("skip"), "errors": count("error"),
                       "submodules_moved": count("submodule-moved"), "would_move_submodules": count("submodule-move"),
                       "pruned": list.iter().map(|x| x.pruned.len()).sum::<usize>()}})
}

pub fn render_human(apply: bool, list: &[SyncRec], o: &Opts) -> String {
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
            "{} fast-forwarded, {} skipped, {} failed",
            count("fast-forwarded"),
            count("skip"),
            count("error")
        );
        if o.submodules {
            out += &format!(", {} submodule(s) moved", count("submodule-moved"));
        }
        if o.prune != Prune::Unset {
            out += &format!(
                ", {} remote-tracking ref(s) pruned",
                list.iter().map(|x| x.pruned.len()).sum::<usize>()
            );
        }
        out += "\n";
    } else {
        out += &format!(
            "dry run: {} would fast-forward, {} skipped",
            count("fast-forward"),
            count("skip")
        );
        if o.submodules {
            out += &format!(", {} submodule(s) would move", count("submodule-move"));
        }
        out += ". Nothing was written.\n";
    }
    out
}
