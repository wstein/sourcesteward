//! `stw sync`: bring every repository up to date, like `git pull --ff-only`.
//! Acts by default, like the Go tool; `--plan` / `--dry-run` only report. `Opts::apply` is the internal name of "act" (its
//! default, false, is the dry run, so a caller that builds `Opts` by hand fails closed). Without `--rebase` or `--merge`
//! it never merges, rebases, stashes, resets or pushes (`pull.rs` does with them). A run fast-forwards the checked-out branch (index and working tree included) when no local change
//! overlaps.
//!
//! `--prune` / `--no-prune` choose how the fetch treats remote-tracking refs that are gone on `origin`; only
//! `refs/remotes/origin/*` can be deleted (`fetch::check_prune_scope`), by the installed git's own fetch, before
//! any fast-forward. `--submodules` then moves each pinned (detached) submodule to the commit the superproject
//! records, when that is a fast-forward (`submodules.rs`). Both act only in a run, not with `--plan`.
//!
//! Every branch a run moves is journaled first (`journal.rs`): a recovery ref `refs/stw/recovery/sync/<run id>/<n>` holds
//! the old tip and a record `<git common dir>/stw/sync/<run id>.json` names it; `stw sync restore` moves the branches
//! back (`restore.rs`). `--plan` / `--dry-run` write neither.
use std::collections::{BTreeMap, BTreeSet};
use std::path::Path;

use serde_json::{json, Value};

use crate::github::Client;

pub mod fast_forward;
pub mod fetch;
pub mod journal;
pub mod pull;
pub mod restore;
pub mod rules;
pub mod submodules;

use crate::error::Result;
use crate::git_runner::{Hex40, RunId};
use crate::github::reads::escape_untrusted;
use crate::output::*;
use crate::repo::{self, RepoRef};
use crate::status::{self, HeadFacts};
use fast_forward::fast_forward_with;
use fetch::Prune;
pub use pull::Diverged;
pub use submodules::{rejected_pass, submodule_pass, SubRec};

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
    /// A pinned submodule candidate: detached HEAD with no operation in progress (the `--submodules` pass acts on it).
    pub pinned: bool,
    /// `--prune` (`"on"`) or `--no-prune` (`"off"`) was given; None without either.
    pub prune: Option<&'static str>,
    /// Remote-tracking refs of `origin` that the fetch deleted (apply). Also set when the fetch failed afterwards.
    pub pruned: Vec<String>,
    /// Offline plan with `--prune`: how many refs under `refs/remotes/origin/` are candidates.
    pub tracking_refs: Option<usize>,
    /// `--submodules`: what happened to this pinned submodule.
    pub submodule: Option<SubRec>,
    /// The journal entry of the branch change of a run (recovery ref and record); None when this repository was not
    /// changed, and always in a dry run.
    pub journal: Option<journal::Note>,
    /// Git's own message of a stopped rebase or merge or a kept autostash (`conflict`, `autostash-conflict`).
    pub detail: Option<String>,
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

/// `pull.rebase` or `branch.<name>.rebase` asks for a rebase (any value but a false one). Go would rebase a diverged
/// branch then; stw never does, and says so instead of ignoring the setting silently.
fn rebase_configured(repo: &gix::Repository, branch: &str) -> bool {
    let cfg = repo.config_snapshot();
    ["pull.rebase".to_string(), format!("branch.{branch}.rebase")]
        .iter()
        .any(|k| {
            cfg.string(k.as_str()).is_some_and(|v| {
                !matches!(
                    v.to_string().to_ascii_lowercase().as_str(),
                    "false" | "no" | "off" | "0" | ""
                )
            })
        })
}

fn plan_of(f: &HeadFacts, op: Option<&str>, o: &Opts, dirty: usize) -> (&'static str, String) {
    let (a, b) = (f.ahead.unwrap_or(0), f.behind.unwrap_or(0));
    if let Some(op) = op {
        ("skip", format!("{op} in progress"))
    } else if f.branch.is_none() {
        ("skip", "detached HEAD".into())
    } else if f.upstream.gone {
        ("skip", "upstream branch is gone".into())
    } else if f.upstream.short.is_none() {
        ("skip", "no upstream configured".into())
    } else if b > 0 && a > 0 && o.diverged != Diverged::Skip {
        let verb = o.diverged.verb();
        if dirty > 0 && o.autostash != Some(true) {
            (
                "skip",
                format!("cannot {verb} with local changes (use --autostash)"),
            )
        } else if o.diverged == Diverged::Rebase {
            (
                "rebase",
                format!("would rebase {a} local commit(s) onto {b} new"),
            )
        } else {
            (
                "merge",
                format!("would merge {b} new commit(s) into {a} local"),
            )
        }
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

/// `plan_of`, and for a plan the refusal of a merge that a run would refuse (`config-refused`): the plan must not
/// promise it. It is asked again after the fetch of a plan, because the fetch can change the facts.
fn planned(
    repo: &gix::Repository,
    f: &HeadFacts,
    op: Option<&str>,
    o: &Opts,
    dirty: usize,
) -> (&'static str, String) {
    let (mut action, mut reason) = plan_of(f, op, o, dirty);
    if !o.apply && action == "merge" {
        if let Some(why) = f
            .branch
            .as_deref()
            .and_then(|b| pull::merge_options_refusal(repo, b))
        {
            (action, reason) = ("skip", format!("{why} (would be refused)"));
        }
    }
    (action, reason)
}

#[derive(Clone, Debug, Default)]
pub struct Opts {
    /// Act (fetch, fast-forward, prune, move submodules); false is the dry run (`--plan`, `--dry-run`): it fetches
    /// `origin` (not with `offline`) and changes nothing but the remote-tracking refs, like Go's `--plan`.
    pub apply: bool,
    pub offline: bool,
    pub prune: Prune,
    pub submodules: bool,
    /// `--rebase` / `--merge`: what a diverged branch gets; `Skip` without either flag.
    pub diverged: Diverged,
    /// `--autostash` (`Some(true)`) / `--no-autostash` (`Some(false)`); None without either. The Git configuration
    /// (`rebase.autoStash`, `merge.autoStash`) is not followed: the stash flag of git is always explicit.
    pub autostash: Option<bool>,
    /// The ID of this run, shared by all repositories (a run). None: a repository that is changed gets an ID of its own.
    pub run_id: Option<RunId>,
}

/// Mutate parents before their descendants, since a child checkout changes the
/// gitlink status seen by its parent. Independent repositories at each level
/// still run in parallel, and results retain the caller's repository order.
fn parent_first<I: Sync, T: Send>(
    items: &[I],
    path: impl Fn(&I) -> &Path,
    f: impl Fn(&I) -> T + Sync,
) -> Vec<T> {
    let paths: BTreeSet<_> = items.iter().map(&path).collect();
    let mut levels = BTreeMap::<usize, Vec<_>>::new();
    for (index, item) in items.iter().enumerate() {
        let level = path(item)
            .ancestors()
            .skip(1)
            .filter(|ancestor| paths.contains(ancestor))
            .count();
        levels.entry(level).or_default().push((index, item));
    }
    let mut results = Vec::with_capacity(items.len());
    for level in levels.into_values() {
        results.extend(repo::par_map(
            &level,
            repo::MAX_PARALLEL,
            |&(index, item)| (index, f(item)),
        ));
    }
    results.sort_unstable_by_key(|(index, _)| *index);
    results.into_iter().map(|(_, value)| value).collect()
}

pub fn sync_repos(repos: &[RepoRef], gh: Option<&Client>, opts: &Opts) -> Vec<SyncRec> {
    let sync = |r: &RepoRef| sync_repo(r, gh, opts);
    if opts.apply {
        parent_first(repos, |r| &r.path, sync)
    } else {
        repo::for_each(repos, sync)
    }
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

/// The changed-file count shown for a repository: tracked changes including a changed submodule. A submodule that
/// cannot be read makes that inspection fail; the count of the gate (no look into submodules) stands in then, so the
/// repository stays readable for the run.
fn dirty_with_submodules(repo: &gix::Repository, gate_dirty: usize) -> usize {
    status::changes(repo, false).map_or(gate_dirty, |c| c.dirty_paths)
}

fn sync_inner(r: &RepoRef, gh: Option<&Client>, o: &Opts, rec: &mut SyncRec) -> Result<()> {
    let repo = repo::open(&r.path)?;
    let mut facts = status::head_facts(&repo)?;
    // the gate of --rebase and --merge ignores a changed submodule, as Go does (`--ignore-submodules=all`)
    let mut gate_dirty = status::changes_opts(&repo, false, true)?.dirty_paths;
    let mut dirty = dirty_with_submodules(&repo, gate_dirty);
    let op = status::operation(&repo);
    rec.fetched_ago = std::fs::metadata(repo.git_dir().join("FETCH_HEAD"))
        .and_then(|m| m.modified())
        .ok()
        .and_then(|t| std::time::SystemTime::now().duration_since(t).ok())
        .map(|d| d.as_secs());
    // a run: ask GitHub read-only whether the upstream branch moved (replaces `git ls-remote`); a plan fetches instead
    if !o.offline && o.apply {
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
    let (mut action, mut reason) = planned(&repo, &facts, op, o, gate_dirty);
    rec.pinned = op.is_none() && facts.branch.is_none();
    let mut result = action.to_string();
    let mut err = None;
    rec.prune = match o.prune {
        Prune::Unset => None,
        Prune::On => Some("on"),
        Prune::Off => Some("off"),
    };
    let mut prune_note = String::new();
    if !o.apply && o.offline && o.prune == Prune::On && repo.find_remote("origin").is_ok() {
        // an offline plan never fetches: it can only count what a prune could reach
        let n = tracking_refs(&r.path).map(|s| s.len())?;
        rec.tracking_refs = Some(n);
        prune_note = format!(
            "; --prune would delete the remote-tracking refs of origin that are gone there ({n} under {} checked)",
            fetch::PRUNE_SCOPE
        );
    } else if !o.apply && o.offline && o.prune == Prune::Off {
        prune_note = "; --no-prune: the fetch would not prune".into();
    }
    if o.apply || !o.offline {
        if o.offline {
            rec.fetch = "skipped-offline".into();
        } else {
            // also without a flag: `fetch.prune` of the configuration prunes a fetch by the installed git
            let before = tracking_refs(&r.path)?;
            // a plan fetches too, but a refspec that writes outside `refs/remotes/origin/` would move a local ref
            let refused = if o.apply {
                Ok(())
            } else {
                fetch::check_plan_scope(&repo)
            };
            let fetched = match refused {
                Ok(()) if o.apply => fetch::fetch_origin_with(&repo, gh, o.prune),
                Ok(()) => fetch::fetch_origin_plan(&repo, gh, o.prune),
                Err(m) => fetch::Fetched {
                    fetch: fetch::Fetch::Failed(m),
                    via: None,
                    git_error: Some("refused-refspec"),
                },
            };
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
        gate_dirty = status::changes_opts(&repo, false, true)?.dirty_paths;
        dirty = dirty_with_submodules(&repo, gate_dirty);
        let op_now = status::operation(&repo);
        rec.pinned = op_now.is_none() && facts.branch.is_none();
        (action, reason) = planned(&repo, &facts, op_now, o, gate_dirty);
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
        if o.apply && result == "fast-forward" {
            let up = facts.upstream.short.clone().unwrap_or_default();
            let tip = facts.upstream.tip.expect("behind implies tip");
            let branch = facts.branch.clone().expect("a fast-forward has a branch");
            let run = o.run_id.clone().unwrap_or_else(journal::new_run_id);
            let mut entry: Option<(journal::Journal, usize)> = None;
            // after every check and before the first write: the recovery ref, then the record (`prepared`)
            let outcome = fast_forward_with(&repo, tip, &up, &mut |old, new| {
                let ids = Hex40::parse(&old.to_string()).zip(Hex40::parse(&new.to_string()));
                let (old, new) = ids.ok_or("journal-failed: not a 40 digit object id")?;
                let mut j = journal::Journal::new(&repo, run.clone());
                let i = j
                    .prepare(
                        &repo,
                        &format!("refs/heads/{branch}"),
                        &p2s(&r.path),
                        old,
                        new,
                    )
                    .map_err(|e| format!("journal-failed: {e}"))?;
                entry = Some((j, i));
                Ok(())
            });
            match outcome {
                Ok((a, b)) => {
                    result = "fast-forwarded".into();
                    reason = format!("{a}..{b}");
                    err = None;
                    if let Some((mut j, i)) = entry {
                        match j.finish(i, journal::STATE_COMPLETED, &b.to_string(), "") {
                            Ok(()) => j.retain(&repo),
                            Err(e) => {
                                reason += &format!("; the journal record stays incomplete: {e}")
                            }
                        }
                        rec.journal = Some(j.note(i));
                    }
                }
                Err(e) => {
                    result = "error".into();
                    reason = "fast-forward refused".into();
                    err = Some(e.to_string());
                    if let Some((mut j, i)) = entry {
                        let _ = j.finish(i, journal::STATE_FAILED, "", &e.to_string());
                        rec.journal = Some(j.note(i));
                    }
                }
            }
        }
    }
    if o.apply && (result == "rebase" || result == "merge") {
        let run = o.run_id.clone().unwrap_or_else(journal::new_run_id);
        let done = pull::apply(r, &facts, o.diverged, o.autostash == Some(true), &run);
        result = done.result.into();
        reason = done.reason;
        err = done.err;
        rec.detail = done.detail;
        rec.journal = done.journal;
    }
    if dirty > 0 && result == "fast-forward" {
        reason += &format!("; working tree has {dirty} changed file(s), refused if they overlap");
    }
    reason += &prune_note;
    if result == "skip" && reason.starts_with("diverged") {
        if let Some(b) = &facts.branch {
            if rebase_configured(&repo, b) {
                reason += "; pull.rebase/branch.<name>.rebase is set, stw does not follow it: use --rebase";
            }
        }
    }
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

/// The run ID, when this run journaled a change in any repository (Go: `run_id` is null otherwise).
pub fn run_id_of(list: &[SyncRec]) -> Option<&str> {
    list.iter()
        .find_map(|s| s.journal.as_ref())
        .map(|n| n.run_id.as_str())
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
            if let Some(d) = &s.detail {
                v["detail"] = json!(d);
            }
            if let Some(m) = &s.submodule {
                v["submodule"] = m.to_json();
            }
            // additive: the journal entry of a branch change
            if let Some(n) = &s.journal {
                v["journal"] = json!({"n": n.n, "recovery_ref": n.recovery_ref, "state": n.state});
            }
            v
        })
        .collect();
    json!({"schema_version": 2, "command": "sync", "mode": if apply {"apply"} else {"dry-run"}, "root": root,
           "engine": engine(), "run_id": jopt(run_id_of(list)), "repos": repos,
           "options": {"prune": match o.prune { Prune::Unset => Value::Null, Prune::On => json!(true), Prune::Off => json!(false) },
                       "submodules": o.submodules, "dry_run": !apply,
                       "rebase": (o.diverged == Diverged::Rebase).then_some(true),
                       "merge": o.diverged == Diverged::Merge, "autostash": o.autostash},
           "summary": {"total": list.len(), "fast_forwarded": count("fast-forwarded"),
                       "would_fast_forward": count("fast-forward"), "skipped": count("skip"), "errors": count("error"),
                       "submodules_moved": count("submodule-moved"), "would_move_submodules": count("submodule-move"),
                       "rebased": count("rebased"), "merged": count("merged"),
                       "would_rebase": count("rebase"), "would_merge": count("merge"),
                       "conflicts": count("conflict") + count("autostash-conflict"),
                       "pruned": list.iter().map(|x| x.pruned.len()).sum::<usize>()}})
}

pub fn render_human(apply: bool, list: &[SyncRec], o: &Opts) -> String {
    let mut out = String::new();
    for s in list {
        out += &format!(
            "{:<18} {:<26} {:<16} {}\n",
            escape_untrusted(&s.name, 200).0,
            escape_untrusted(s.branch.as_deref().unwrap_or("(detached)"), 200).0,
            s.result,
            s.reason
        );
        if let Some(e) = &s.err {
            out += &format!("    error: {}\n", escape_untrusted(e, 600).0);
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
        if o.diverged != Diverged::Skip {
            out += &format!(
                ", {} rebased, {} merged, {} stopped on a conflict",
                count("rebased"),
                count("merged"),
                count("conflict") + count("autostash-conflict")
            );
        }
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
        if let Some(id) = run_id_of(list) {
            out += &format!(
                "journal: run {id}, recovery refs under {}{id}\n",
                crate::git_runner::RECOVERY_NAMESPACE
            );
        }
    } else {
        out += &format!(
            "dry run: {} would fast-forward, {} skipped",
            count("fast-forward"),
            count("skip")
        );
        if o.diverged != Diverged::Skip {
            out += &format!(
                ", {} would rebase, {} would merge",
                count("rebase"),
                count("merge")
            );
        }
        if o.submodules {
            out += &format!(", {} submodule(s) would move", count("submodule-move"));
        }
        let failed = list
            .iter()
            .filter(|x| {
                matches!(
                    x.fetch.as_str(),
                    "failed" | "unsupported" | "unsupported-remote"
                )
            })
            .count();
        out += &if o.offline {
            ". Nothing was written. Nothing was fetched either: based on the last fetch.\n"
                .to_string()
        } else if failed > 0 {
            format!(
                ". The fetch failed for {failed} {}; {} plan is based on the last fetch. Nothing else was written.\n",
                if failed == 1 { "repository" } else { "repositories" },
                if failed == 1 { "its" } else { "their" }
            )
        } else {
            ". Only the remote-tracking refs were fetched; nothing else was written.\n".to_string()
        };
    }
    out
}
