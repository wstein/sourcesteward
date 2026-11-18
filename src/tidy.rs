//! `stw tidy plan`: which local branches and worktree registrations can be cleaned up. Read-only.
//!
//! Proof order per branch (first match wins), as in the prototype:
//! in a worktree whose directory exists (keep) > open PR (keep) > ancestor of base (delete) >
//! equal patch IDs in base (delete) > merged PR with exactly this head (delete) > merged PR but the
//! branch moved (review) > upstream gone (review) > unmerged (keep). Nothing is deleted here.
//!
//! Backup branches (see `is_backup`) have no review class: where no proof above applies they are `keep`
//! while the tip commit is younger than `backup_days` days and `delete` from that age on.
use std::collections::HashMap;

use gix::ObjectId;
use serde_json::{json, Value};

use crate::error::{Error, Result};
use crate::github::{AnyPr, Client, GithubState};
use crate::output::*;
use crate::patch_id::{commit_patch_key, PatchKey};
use crate::repo::worktrees::{self, StaleRegistration};
use crate::repo::{self, RepoRef};
use crate::tidy_similarity::{self, BranchReport};

#[derive(Clone, Debug)]
pub struct BranchPlan {
    pub branch: String,
    pub tip: String,
    pub decision: &'static str,
    pub proof: &'static str,
    pub detail: String,
    pub unique: usize,
    pub upstream: Option<String>,
    /// git's `%(upstream:track)`: `[gone]`, `[ahead N]`, `[behind M]`, `[ahead N, behind M]`; None when in sync.
    pub track: Option<String>,
    /// Set when the branch is checked out in a linked worktree and would be `delete` otherwise: `tidy apply`
    /// may remove that worktree (when it is clean) and then the branch. The decision stays `keep`/`in-worktree`.
    pub worktree: Option<WorktreeCandidate>,
    /// What the similarity stage found; None where the stage did not run for this branch.
    pub similarity: Option<SimilarityPlan>,
}

/// The two thresholds of the similarity stage, in percent (`1..=100`, `low <= high`).
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Similarity {
    pub high: u32,
    pub low: u32,
}

impl Default for Similarity {
    fn default() -> Self {
        Similarity {
            high: tidy_similarity::HIGH_PCT,
            low: tidy_similarity::LOW_PCT,
        }
    }
}

/// The result of the similarity stage for one branch.
#[derive(Clone, Debug)]
pub struct SimilarityPlan {
    pub high: u32,
    pub low: u32,
    pub report: BranchReport,
    /// Commits of the base that were searched for partners.
    pub indexed: usize,
    /// Commits of the base left out because their parent is missing (shallow clone).
    pub skipped_shallow: usize,
}

#[derive(Clone, Debug)]
pub struct WorktreeCandidate {
    pub dir: std::path::PathBuf,
    /// The proof the branch would have without the worktree.
    pub proof: &'static str,
    /// The checkout is not below `<repo>/.worktrees/`. It is removed on the same proof as any other.
    pub outside: bool,
    /// Why `tidy apply` would leave this worktree (and so the branch) alone; None when it removes both.
    pub blocker: Option<String>,
}

#[derive(Clone, Debug, Default)]
pub struct RepoPlan {
    pub name: String,
    pub path: String,
    pub base: Option<String>,
    pub base_tip: Option<String>,
    pub slug: Option<String>,
    pub note: String,
    pub branches: Vec<BranchPlan>,
    pub stale: Vec<StaleRegistration>,
    pub error: Option<String>,
}

/// Settings of one plan run.
#[derive(Clone, Debug)]
pub struct PlanOpts {
    /// A backup branch whose tip commit is at least this many days old is `delete`.
    pub backup_days: u64,
    /// "Now" in seconds since the epoch (tests inject it).
    pub now: i64,
    /// The similarity stage (`similar-patches`); None switches it off.
    pub similarity: Option<Similarity>,
}

pub const DEFAULT_BACKUP_DAYS: u64 = 7;

impl Default for PlanOpts {
    fn default() -> Self {
        PlanOpts {
            backup_days: DEFAULT_BACKUP_DAYS,
            now: now_secs(),
            similarity: Some(Similarity::default()),
        }
    }
}

pub fn now_secs() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0)
}

/// Backup branch names: `r9-*`, `r10-*`, `pre-restack*`, `archive/*`, `backup/*`.
pub fn is_backup(branch: &str) -> bool {
    ["r9-", "r10-", "pre-restack", "archive/", "backup/"]
        .iter()
        .any(|p| branch.starts_with(p))
}

/// Patch-key cache of one repository: commit -> `PatchKey` (patch ID and exact digest; None for merges).
#[derive(Default)]
pub struct PatchCache {
    map: HashMap<ObjectId, Option<PatchKey>>,
}

impl PatchCache {
    pub fn get(&mut self, repo: &gix::Repository, c: ObjectId) -> Result<Option<PatchKey>> {
        if let Some(v) = self.map.get(&c) {
            return Ok(*v);
        }
        let v = commit_patch_key(repo, c)?;
        self.map.insert(c, v);
        Ok(v)
    }
}

/// `git cherry <base> <tip>` has no `+` line: every non-merge commit of `base..tip` has an equal
/// patch among the non-merge commits of `tip..base`: equal patch ID and identical diff lines with their
/// white space, file names included (git's patch ID ignores white space in names and content). Needs at least
/// one such commit.
pub fn all_patches_in_base(
    repo: &gix::Repository,
    cache: &mut PatchCache,
    base: ObjectId,
    tip: ObjectId,
) -> Result<bool> {
    Ok(unmatched_commits(repo, cache, base, tip)?.all_matched())
}

/// The commits of `base..tip` that have no equal patch in `tip..base`.
pub struct Unmatched {
    /// Non-merge commits of `base..tip` that have a patch.
    pub with_patch: usize,
    /// Those without a partner, sorted by commit ID (the `+` lines of `git cherry`).
    pub commits: Vec<ObjectId>,
}

impl Unmatched {
    /// At least one commit with a patch, and every one has a partner: the proof `equal-patches`.
    pub fn all_matched(&self) -> bool {
        self.with_patch > 0 && self.commits.is_empty()
    }
}

/// The `+` commits of `git cherry <base> <tip>`, by the equality of `all_patches_in_base`. The walk over
/// `tip..base` stops as soon as every commit has its partner.
pub fn unmatched_commits(
    repo: &gix::Repository,
    cache: &mut PatchCache,
    base: ObjectId,
    tip: ObjectId,
) -> Result<Unmatched> {
    let mut needed: HashMap<PatchKey, Vec<ObjectId>> = HashMap::new();
    let mut with_patch = 0;
    for info in repo.rev_walk([tip]).with_hidden([base]).all()? {
        let info = info?;
        if let Some(p) = cache.get(repo, info.id)? {
            needed.entry(p).or_default().push(info.id);
            with_patch += 1;
        }
    }
    if !needed.is_empty() {
        for info in repo.rev_walk([base]).with_hidden([tip]).all()? {
            let info = info?;
            if let Some(p) = cache.get(repo, info.id)? {
                needed.remove(&p);
                if needed.is_empty() {
                    break;
                }
            }
        }
    }
    let mut commits: Vec<ObjectId> = needed.into_values().flatten().collect();
    commits.sort();
    Ok(Unmatched {
        with_patch,
        commits,
    })
}

/// Pull request proof of one repository: the PR list, or the reason there is none.
pub struct PrProof {
    pub prs: Option<Vec<AnyPr>>,
    pub note: String,
}

pub fn fetch_proof(slug: Option<&str>, gh: Option<&Client>, offline: bool) -> PrProof {
    let mut pr_err = None;
    if let (Some(c), Some(s)) = (gh, slug) {
        match c.all_prs(s, 500) {
            Ok(v) => {
                return PrProof {
                    prs: Some(v),
                    note: "pr-proof-available".into(),
                }
            }
            Err(e) => pr_err = Some(e.to_string()),
        }
    }
    let note = if offline {
        "pull request proof off (--offline)".into()
    } else if slug.is_none() {
        "no GitHub origin: no pull request proof".into()
    } else if let Some(e) = pr_err {
        format!("pull request proof unavailable ({e})")
    } else {
        "pull request proof unavailable (no GitHub token: set GH_TOKEN or log in with gh)".into()
    };
    PrProof { prs: None, note }
}

pub fn plan_repo(r: &RepoRef, gh: Option<&Client>, offline: bool, opts: &PlanOpts) -> RepoPlan {
    let slug = repo::open(&r.path).ok().and_then(|repo| repo::slug(&repo));
    let proof = fetch_proof(slug.as_deref(), gh, offline);
    plan_repo_with_opts(r, proof, opts)
}

/// The plan with the PR proof already in hand and default settings.
pub fn plan_repo_with(r: &RepoRef, proof: PrProof) -> RepoPlan {
    plan_repo_with_opts(r, proof, &PlanOpts::default())
}

/// The plan with the PR proof already in hand (tests inject it here).
pub fn plan_repo_with_opts(r: &RepoRef, proof: PrProof, opts: &PlanOpts) -> RepoPlan {
    let mut plan = RepoPlan {
        name: r.name.clone(),
        path: p2s(&r.path),
        ..Default::default()
    };
    if let Err(e) = plan_inner(r, proof, opts, &mut plan) {
        plan.error = Some(e.to_string());
    }
    plan
}

fn plan_inner(r: &RepoRef, proof: PrProof, opts: &PlanOpts, plan: &mut RepoPlan) -> Result<()> {
    let repo = repo::open(&r.path)?;
    let slug = repo::slug(&repo);
    plan.slug = slug.clone();
    let Some(base) = repo::base_ref(&repo) else {
        plan.note =
            "no base: refs/remotes/origin/HEAD, origin/main and origin/master are all missing"
                .into();
        return Ok(());
    };
    let base_tip = repo::resolve(&repo, &base)
        .ok_or_else(|| Error::repository("base ref does not resolve"))?;
    let basebranch = base
        .strip_prefix("refs/remotes/origin/")
        .unwrap_or(&base)
        .to_string();
    plan.base = Some(base.clone());
    plan.base_tip = Some(short(&base_tip.to_string()).to_string());
    plan.note = proof.note;
    let prs = proof.prs;

    let wt = worktrees::scan(&repo)?;
    plan.stale = wt.stale.clone();
    let mut cache = PatchCache::default();
    // branches decided so far, before the worktree protection; `plus` are the commits the similarity stage judges
    let mut rows: Vec<(BranchPlan, Option<Vec<ObjectId>>)> = Vec::new();
    for (br, tip) in repo::local_branches(&repo)? {
        if br == basebranch {
            continue;
        }
        let up = repo::upstream_of(&repo, &br);
        let track = if up.gone {
            Some("[gone]".to_string())
        } else if let Some(ut) = up.tip {
            let (a, b) = (
                repo::count_range(&repo, tip, ut)?,
                repo::count_range(&repo, ut, tip)?,
            );
            match (a, b) {
                (0, 0) => None,
                (a, 0) => Some(format!("[ahead {a}]")),
                (0, b) => Some(format!("[behind {b}]")),
                (a, b) => Some(format!("[ahead {a}, behind {b}]")),
            }
        } else {
            None
        };
        let uniq = repo::count_range(&repo, tip, base_tip)?;
        let tipstr = tip.to_string();
        let (mut pr_same, mut pr_other, mut pr_open) = (None, None, None);
        if let Some(list) = &prs {
            for pr in list.iter().filter(|x| x.head_ref == br) {
                match pr.state {
                    "OPEN" => pr_open = Some(pr.number),
                    "MERGED" => {
                        if pr.head_sha == tipstr {
                            pr_same = Some(pr.number)
                        } else {
                            pr_other = Some(pr.number)
                        }
                    }
                    _ => {}
                }
            }
        }
        let mut bp = BranchPlan {
            branch: br.clone(),
            tip: tipstr.clone(),
            decision: "keep",
            proof: "unmerged",
            detail: String::new(),
            unique: uniq,
            upstream: up.name.clone(),
            track,
            worktree: None,
            similarity: None,
        };
        let mut unmatched: Option<Vec<ObjectId>> = None;
        if let Some(n) = pr_open {
            bp.proof = "open-pull-request";
            bp.detail = format!("open pull request #{n}");
        } else if repo::is_ancestor(&repo, tip, base_tip)? {
            bp.decision = "delete";
            bp.proof = "merged-ancestor";
            bp.detail = format!("tip is an ancestor of {base}");
        } else if uniq > 0 && {
            let u = unmatched_commits(&repo, &mut cache, base_tip, tip)?;
            let all = u.all_matched();
            unmatched = Some(u.commits);
            all
        } {
            bp.decision = "delete";
            bp.proof = "equal-patches";
            bp.detail = format!(
                "all {uniq} commit(s) have an equal patch in {base} (squash or rebase merge)"
            );
        } else if let Some(n) = pr_same {
            bp.decision = "delete";
            bp.proof = "merged-pull-request-head";
            bp.detail = format!("pull request #{n} merged with exactly this head");
        } else if is_backup(&br) {
            let tip_time = repo.find_commit(tip)?.committer()?.seconds();
            let age = opts.now.saturating_sub(tip_time);
            let limit = opts.backup_days as i64 * 86_400;
            if age >= limit {
                bp.decision = "delete";
                bp.proof = "backup-older-than-days";
                bp.detail = format!(
                    "backup older than {} days (tip commit is {} days old)",
                    opts.backup_days,
                    age / 86_400
                );
            } else {
                bp.proof = "backup-recent";
                bp.detail = format!(
                    "backup younger than {} days (tip commit is {} days old)",
                    opts.backup_days,
                    age / 86_400
                );
            }
        } else if let Some(n) = pr_other {
            bp.decision = "review";
            bp.proof = "pull-request-merged-branch-moved";
            bp.detail = format!(
                "pull request #{n} merged, but the branch has {uniq} commit(s) not in {base}"
            );
        } else if up.gone {
            bp.decision = "review";
            bp.proof = "upstream-gone";
            bp.detail = format!(
                "upstream {} is gone, {uniq} commit(s) not in {base}, no merged pull request found",
                up.name.as_deref().unwrap_or("")
            );
        } else {
            bp.detail = format!("{uniq} commit(s) not in {base}, no merged pull request found");
        }
        // where the proof would be `upstream-gone` or `unmerged` the similarity stage may find a better one
        let judged = matches!(
            bp.proof,
            "pull-request-merged-branch-moved" | "upstream-gone" | "unmerged"
        );
        let plus = if opts.similarity.is_some() && judged {
            unmatched
        } else {
            None
        };
        rows.push((bp, plus));
    }
    if let Some(th) = opts.similarity {
        similarity_stage(&repo, base_tip, &base, th, &mut rows)?;
    }
    for (mut bp, _) in rows {
        let br = bp.branch.clone();
        // a checkout in a directory that exists protects the branch; the proof it would have is kept
        if let Some(dir) = wt.branch_dir(&br) {
            if bp.decision == "delete" {
                let mut cand = WorktreeCandidate {
                    dir: dir.clone(),
                    proof: bp.proof,
                    outside: outside_worktrees_dir(&repo, dir),
                    blocker: None,
                };
                bp.worktree = Some(cand.clone());
                // the same checks `apply` makes, so the plan says what apply will do
                cand.blocker = match wt.linked.iter().find(|l| &l.dir == dir) {
                    None if wt.main_dir.as_ref() == Some(dir) => Some("main worktree".into()),
                    None => Some("not a linked worktree".into()),
                    Some(l) => {
                        crate::tidy_apply::worktree_blocker(&repo, &wt, l, &bp, Some(base_tip))
                    }
                };
                bp.worktree = Some(cand);
            }
            bp.decision = "keep";
            bp.proof = "in-worktree";
            bp.detail = format!("checked out in {}", worktrees::show(dir));
        }
        plan.branches.push(bp);
    }
    Ok(())
}

/// The similarity stage: judges the `+` commits of every branch in `rows` that has some against the base and lets
/// a branch whose commits all have a similar patch there become `review` with the proof `similar-patches`.
/// The index of the base starts one window before the oldest of those commits.
fn similarity_stage(
    repo: &gix::Repository,
    base_tip: ObjectId,
    base: &str,
    th: Similarity,
    rows: &mut [(BranchPlan, Option<Vec<ObjectId>>)],
) -> Result<()> {
    let lim = tidy_similarity::Limits::default();
    let mut oldest: Option<i64> = None;
    for (_, plus) in rows.iter() {
        for id in plus.iter().flatten() {
            let t = tidy_similarity::commit_info(repo, *id)?.author_time;
            oldest = Some(oldest.map_or(t, |o| o.min(t)));
        }
    }
    let Some(oldest) = oldest else {
        return Ok(());
    };
    let mut cache = tidy_similarity::SimCache::default();
    let index = tidy_similarity::MainIndex::build_from_walk(
        repo,
        &mut cache,
        base_tip,
        Some(oldest - lim.window_secs),
    )?;
    for (bp, plus) in rows.iter_mut() {
        let Some(plus) = plus else {
            continue;
        };
        let report = tidy_similarity::evaluate_branch(
            repo, &mut cache, &index, plus, th.high, th.low, &lim,
        )?;
        if report.class == tidy_similarity::Class::Landed {
            let shown = |p: u32| tidy_similarity::fmt_permille(p);
            let min = report.min.map(|m| m.shown_in(th.high, th.low)).unwrap_or(0);
            bp.decision = "review";
            bp.proof = "similar-patches";
            bp.detail = format!(
                "all {} commit(s) without an equal patch have a similar patch in {base} (min {} %, median {} %, threshold {} %): review, not a deletion proof",
                report.plus_commits,
                shown(min),
                shown(report.median_permille.unwrap_or(0)),
                th.high
            );
        }
        bp.similarity = Some(SimilarityPlan {
            high: th.high,
            low: th.low,
            report,
            indexed: index.len(),
            skipped_shallow: index.shallow_skipped(),
        });
    }
    Ok(())
}

/// True when `dir` is not below `<work tree>/.worktrees/` (compared as written and with symbolic links resolved).
pub fn outside_worktrees_dir(repo: &gix::Repository, dir: &std::path::Path) -> bool {
    // `.worktrees/` of the main checkout, also when `repo` was opened at a linked worktree
    let Some(root) = repo
        .main_repo()
        .ok()
        .and_then(|m| m.workdir().map(|w| w.join(".worktrees")))
    else {
        return true;
    };
    if dir.starts_with(&root) {
        return false;
    }
    match (std::fs::canonicalize(dir), std::fs::canonicalize(&root)) {
        (Ok(d), Ok(r)) => !d.starts_with(r),
        _ => true,
    }
}

/// (delete, delete with its worktree, review, keep). A kept branch whose worktree `apply` removes counts as
/// "delete with its worktree", not as kept, because `apply` deletes it. A worktree `apply` leaves alone
/// (dirty, locked, unpushed, ...) keeps its branch in `keep`.
pub fn summary(plans: &[RepoPlan]) -> (usize, usize, usize, usize) {
    let c = |d: &str| {
        plans
            .iter()
            .flat_map(|p| &p.branches)
            .filter(|b| b.decision == d)
            .count()
    };
    let with_wt = plans
        .iter()
        .flat_map(|p| &p.branches)
        .filter(|b| {
            b.decision == "keep" && b.worktree.as_ref().is_some_and(|w| w.blocker.is_none())
        })
        .count();
    (c("delete"), with_wt, c("review"), c("keep") - with_wt)
}

pub fn to_json(root: &str, gh: &GithubState, plans: &[RepoPlan]) -> Value {
    let repos: Vec<Value> = plans
        .iter()
        .map(|p| {
            let branches: Vec<Value> = p
                .branches
                .iter()
                .map(|b| {
                    json!({"branch": b.branch, "commit": b.tip, "decision": b.decision, "proof": b.proof,
                           "detail": b.detail, "unique_commits": b.unique, "upstream": jopt(b.upstream.as_deref()),
                           "upstream_track": jopt(b.track.as_deref()),
                           "worktree": b.worktree.as_ref().map(|w| json!({"path": worktrees::show(&w.dir), "removable_proof": w.proof, "outside_worktrees_dir": w.outside, "blocker": jopt(w.blocker.as_deref())})).unwrap_or(Value::Null)})
                })
                .collect();
            let stale: Vec<Value> = p
                .stale
                .iter()
                .map(|s| {
                    json!({"path": worktrees::show(&s.path), "locked": s.locked, "lock_reason": jopt(s.lock_reason.as_deref())})
                })
                .collect();
            json!({"name": p.name, "path": p.path, "base": jopt(p.base.as_deref()), "base_commit": jopt(p.base_tip.as_deref()),
                   "slug": jopt(p.slug.as_deref()), "note": jne(&p.note), "branches": branches,
                   "stale_worktree_registrations": stale, "error": jopt(p.error.as_deref())})
        })
        .collect();
    let (d, w, r, k) = summary(plans);
    json!({"schema_version": 2, "command": "tidy-plan", "read_only": true, "root": root,
           "engine": engine(),
           "github": gh.client.is_some(), "github_auth": jopt(gh.client.as_ref().map(|c| c.source.label())), "github_note": jopt(gh.note.as_deref()), "repos": repos,
           "summary": {"delete": d, "delete_with_worktree": w, "review": r, "keep": k}})
}

pub fn render_human(plans: &[RepoPlan], all: bool) -> String {
    let mut out = String::new();
    for p in plans {
        out += &format!(
            "{}  base {} {}  ({})\n",
            p.name,
            p.base.as_deref().unwrap_or("none"),
            p.base_tip.as_deref().unwrap_or(""),
            p.note
        );
        if let Some(e) = &p.error {
            out += &format!("  error: {e}\n");
            continue;
        }
        if p.base.is_none() {
            continue;
        }
        let mut any = false;
        for b in &p.branches {
            // a kept branch whose worktree is removable is listed: that is part of what the plan does
            if b.decision == "keep" && !all && b.worktree.is_none() {
                continue;
            }
            any = true;
            let mut note = String::new();
            if let Some(w) = &b.worktree {
                match &w.blocker {
                    None => note += &format!("; removable with its branch (proof: {})", w.proof),
                    Some(why) => note += &format!("; stays: {why}"),
                }
                if w.outside {
                    note += "; outside .worktrees/";
                }
            }
            out += &format!(
                "  {:<7} {}  [{}] {}{}\n",
                b.decision, b.branch, b.proof, b.detail, note
            );
        }
        for s in &p.stale {
            any = true;
            let locked = if s.locked {
                format!(
                    " (locked{})",
                    s.lock_reason
                        .as_deref()
                        .filter(|r| !r.is_empty())
                        .map(|r| format!(": {r}"))
                        .unwrap_or_default()
                )
            } else {
                String::new()
            };
            out += &format!(
                "  worktree registration without directory{locked}: {}\n",
                s.path.display()
            );
        }
        if !any {
            out += "  nothing to clean\n";
        }
    }
    let (d, w, r, k) = summary(plans);
    out += &format!(
        "{d} to delete, {w} to delete with their worktree, {r} to review, {k} kept (read-only: nothing was changed)\n"
    );
    if !all && k > 0 {
        out += "(--all lists the kept branches)\n";
    }
    out
}
