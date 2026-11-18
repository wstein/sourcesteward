//! `stw tidy plan`: which local branches and worktree registrations can be cleaned up. Read-only.
//!
//! Proof order per branch (first match wins): open PR (keep) > ancestor of base (delete) >
//! equal patch IDs in base (delete) > merged PR with exactly this head (delete) > backup name (delete from an age, else
//! keep) > similar patches in base (review, `similarity_stage`) > merged PR but the branch moved (review) >
//! upstream gone (review) > unmerged (keep). A branch checked out in a worktree whose directory exists is
//! `keep`/`in-worktree` at the end, whatever the proof was. Nothing is deleted here.
//!
//! Backup branches (see `is_backup`) have no review class: where no proof above applies they are `keep`
//! while the tip commit is younger than `backup_days` days and `delete` from that age on.
use std::collections::HashMap;

use gix::ObjectId;
use serde_json::{json, Value};

use crate::error::{Error, Result};
use crate::github::{AnyPr, Client, GithubState, PrCommitsRead};
use crate::output::*;
use crate::patch_id::{commit_patch_key, PatchKey};
use crate::repo::worktrees::{self, StaleRegistration};
use crate::repo::{self, RepoRef};
use crate::tidy_rebase::{
    self, landed_via, ApiInput, ApiReason, ApiReport, Basis, LandedKind, LandedVia, PrInput,
    RebaseReport, Strength,
};
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
    /// What the rebase stage found; None where the stage did not run or failed for this branch.
    pub rebase: Option<RebasePlan>,
    /// How the branch landed, with the basis; `unknown` where nothing says.
    pub landed_via: LandedVia,
}

/// The result of the rebase stage for one branch.
#[derive(Clone, Debug)]
pub struct RebasePlan {
    /// The commit mapping (identity, run, mapping table, rest).
    pub report: RebaseReport,
    /// The pull request mapping, or the reason there is none.
    pub api: ApiReport,
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
    /// The rebase stage (`rebase-identity`, `landed_via` from the commits and the pull request); false switches it off.
    pub rebase: bool,
}

pub const DEFAULT_BACKUP_DAYS: u64 = 7;

impl Default for PlanOpts {
    fn default() -> Self {
        PlanOpts {
            backup_days: DEFAULT_BACKUP_DAYS,
            now: now_secs(),
            similarity: Some(Similarity::default()),
            rebase: true,
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
pub struct PrProof<'a> {
    pub prs: Option<Vec<AnyPr>>,
    pub note: String,
    /// Where the commits of a pull request come from.
    pub api: PrApi<'a>,
}

/// The source of the commit list of a pull request: a client, or the reason there is none.
#[derive(Clone, Copy)]
pub enum PrApi<'a> {
    Off(ApiReason),
    Client(&'a Client),
}

impl Default for PrApi<'_> {
    fn default() -> Self {
        PrApi::Off(ApiReason::Offline)
    }
}

pub fn fetch_proof<'a>(slug: Option<&str>, gh: Option<&'a Client>, offline: bool) -> PrProof<'a> {
    let mut pr_err = None;
    if let (Some(c), Some(s)) = (gh, slug) {
        match c.all_prs(s, 500) {
            Ok(v) => {
                return PrProof {
                    prs: Some(v),
                    note: "pr-proof-available".into(),
                    api: PrApi::Client(c),
                }
            }
            Err(e) => pr_err = Some(e.to_string()),
        }
    }
    let why = if gh.is_some_and(|c| c.rate_limited()) {
        ApiReason::RateLimited
    } else if pr_err.is_some() {
        ApiReason::RequestFailed
    } else if offline {
        ApiReason::Offline
    } else if slug.is_none() {
        ApiReason::NoPullRequest
    } else {
        ApiReason::NoToken
    };
    let note = if offline {
        "pull request proof off (--offline)".into()
    } else if slug.is_none() {
        "no GitHub origin: no pull request proof".into()
    } else if let Some(e) = pr_err {
        format!("pull request proof unavailable ({e})")
    } else {
        "pull request proof unavailable (no GitHub token: set GH_TOKEN or log in with gh)".into()
    };
    PrProof {
        prs: None,
        note,
        api: PrApi::Off(why),
    }
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
    let mut rows: Vec<Row> = Vec::new();
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
            rebase: None,
            landed_via: LandedVia {
                kind: LandedKind::Unknown,
                basis: Basis::None,
                strength: Strength::None,
            },
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
        rows.push(Row {
            bp,
            tip,
            unmatched,
            pr_moved: pr_other,
            plus: None,
        });
    }
    let mut sim_cache = tidy_similarity::SimCache::default();
    if opts.rebase {
        let mut stage = RebaseStage {
            repo: &repo,
            base_tip,
            prs: prs.as_deref(),
            api: proof.api,
            slug: slug.as_deref(),
            requests_left: MAX_PR_REQUESTS,
        };
        stage.run(&mut cache, &mut sim_cache, &mut rows);
    }
    // where the proof would be `pull-request-merged-branch-moved`, `upstream-gone` or `unmerged` the similarity stage
    // may find a better one; a commit the rebase stage has vouched for is not judged again
    if opts.similarity.is_some() {
        for row in rows.iter_mut().filter(|r| judged(r.bp.proof)) {
            let solved = solved_by_rebase(row.bp.rebase.as_ref());
            row.plus = row.unmatched.take().map(|u| {
                u.into_iter()
                    .filter(|c| !solved.contains(c))
                    .collect::<Vec<_>>()
            });
        }
    }
    if let Some(th) = opts.similarity {
        similarity_stage(&repo, base_tip, &base, th, &mut sim_cache, &mut rows)?;
    }
    for Row { mut bp, .. } in rows {
        let br = bp.branch.clone();
        bp.landed_via = landed_via(
            bp.proof == "merged-ancestor",
            bp.rebase.as_ref().map(|r| &r.report),
            bp.rebase.as_ref().map(|r| &r.api),
        );
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
    cache: &mut tidy_similarity::SimCache,
    rows: &mut [Row],
) -> Result<()> {
    let lim = tidy_similarity::Limits::default();
    let mut oldest: Option<i64> = None;
    for row in rows.iter() {
        for id in row.plus.iter().flatten() {
            let t = tidy_similarity::commit_info(repo, *id)?.author_time;
            oldest = Some(oldest.map_or(t, |o| o.min(t)));
        }
    }
    let Some(oldest) = oldest else {
        return Ok(());
    };
    let index = tidy_similarity::MainIndex::build_from_walk(
        repo,
        cache,
        base_tip,
        Some(oldest.saturating_sub(lim.window_secs)),
    )?;
    for row in rows.iter_mut() {
        let bp = &mut row.bp;
        let Some(plus) = row.plus.as_ref() else {
            continue;
        };
        let report =
            tidy_similarity::evaluate_branch(repo, cache, &index, plus, th.high, th.low, &lim)?;
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

/// One branch while the stages run: its plan so far and what the stages need besides.
struct Row {
    bp: BranchPlan,
    tip: ObjectId,
    /// The commits of `base..tip` without an equal patch in the base; None where the equal-patches walk did not run.
    unmatched: Option<Vec<ObjectId>>,
    /// The number of a merged pull request of this branch (the proof `pull-request-merged-branch-moved`).
    pr_moved: Option<u64>,
    /// The commits the similarity stage judges; None where it does not.
    plus: Option<Vec<ObjectId>>,
}

/// The proofs the later stages may replace: where the branch would be `review` or `keep` for want of anything better.
fn judged(proof: &str) -> bool {
    matches!(
        proof,
        "pull-request-merged-branch-moved" | "upstream-gone" | "unmerged"
    )
}

/// At most this many extra requests (`pulls/N/commits`) per repository and plan.
pub const MAX_PR_REQUESTS: usize = 30;

/// Commits the rebase stage has vouched for: mapped to a commit of the base by identity with a content value of at
/// least `RB_CONTENT_HIGH` and no later version. The similarity stage does not judge them again.
fn solved_by_rebase(r: Option<&RebasePlan>) -> std::collections::HashSet<ObjectId> {
    let high = tidy_rebase::RB_CONTENT_HIGH * 10;
    r.into_iter()
        .flat_map(|r| &r.report.mapping)
        .filter(|m| {
            m.main.is_some() && !m.ambiguous && !m.later && m.value.is_some_and(|v| v >= high)
        })
        .map(|m| m.commit)
        .collect()
}

/// A stage that failed for a branch leaves the plan standing and says so in the detail.
fn stage_failed(bp: &mut BranchPlan, e: &Error) {
    bp.detail += &format!("; rebase evidence not computed ({e})");
}

/// The rebase stage: maps the commits of each branch to commits of the first-parent chain of the base (identity, equal
/// patch, run), reads the pull request commits where a merged pull request is of use, and lets a branch whose commits all
/// landed as one run become `review` with the proof `rebase-identity`. A hint or a failure leaves the proof alone.
struct RebaseStage<'a> {
    repo: &'a gix::Repository,
    base_tip: ObjectId,
    prs: Option<&'a [AnyPr]>,
    api: PrApi<'a>,
    slug: Option<&'a str>,
    requests_left: usize,
}

impl RebaseStage<'_> {
    fn run(
        &mut self,
        patches: &mut PatchCache,
        sim: &mut tidy_similarity::SimCache,
        rows: &mut [Row],
    ) {
        let eligible = |r: &Row| {
            r.unmatched.is_some()
                && (judged(r.bp.proof)
                    || matches!(r.bp.proof, "equal-patches" | "merged-pull-request-head"))
        };
        let mut sides: Vec<Option<tidy_rebase::BranchSide>> = Vec::new();
        let mut since: Option<i64> = None;
        for row in rows.iter_mut() {
            if !eligible(row) {
                sides.push(None);
                continue;
            }
            match tidy_rebase::branch_side(self.repo, self.base_tip, row.tip) {
                Ok(side) => {
                    if let Some(t) = side.oldest_author_secs {
                        since = Some(since.map_or(t, |o: i64| o.min(t)));
                    }
                    sides.push(Some(side));
                }
                Err(e) => {
                    stage_failed(&mut row.bp, &e);
                    sides.push(None);
                }
            }
        }
        let Some(since) = since else {
            return;
        };
        let index = match tidy_rebase::RebaseIndex::build(
            self.repo,
            self.base_tip,
            Some(since),
            tidy_rebase::RB_MAX_WINDOW,
        ) {
            Ok(i) => i,
            Err(e) => {
                for (row, side) in rows.iter_mut().zip(&sides) {
                    if side.is_some() {
                        stage_failed(&mut row.bp, &e);
                    }
                }
                return;
            }
        };
        let lim = tidy_similarity::Limits::default();
        for (row, side) in rows.iter_mut().zip(sides) {
            let Some(side) = side else {
                continue;
            };
            if let Err(e) = self.branch(patches, sim, &index, &lim, row, &side) {
                stage_failed(&mut row.bp, &e);
            }
        }
    }

    fn branch(
        &mut self,
        patches: &mut PatchCache,
        sim: &mut tidy_similarity::SimCache,
        index: &tidy_rebase::RebaseIndex,
        lim: &tidy_similarity::Limits,
        row: &mut Row,
        side: &tidy_rebase::BranchSide,
    ) -> Result<()> {
        let unmatched: std::collections::HashSet<ObjectId> =
            row.unmatched.iter().flatten().copied().collect();
        let matched: Vec<ObjectId> = side
            .commits
            .iter()
            .filter(|c| !unmatched.contains(*c))
            .copied()
            .collect();
        let hits =
            tidy_rebase::patch_hits_with(index, &matched, &mut |id| patches.get(self.repo, id))?;
        let report = tidy_rebase::evaluate(self.repo, sim, index, side, &hits, lim)?;
        let strong = report.strength == Strength::Strong;
        let input = self.api_input(row, strong);
        let api = tidy_rebase::evaluate_api(self.repo, self.base_tip, side, row.tip, &input)
            .unwrap_or_else(|_| tidy_rebase::ApiReport::not_read(ApiReason::RequestFailed));
        if judged(row.bp.proof) && tidy_rebase::evidence_class(&report).is_some() {
            let min = report
                .mapping
                .iter()
                .filter_map(|m| m.value)
                .min()
                .map(tidy_similarity::fmt_permille)
                .unwrap_or_else(|| "n/a".into());
            row.bp.decision = tidy_rebase::RB_DECISION;
            row.bp.proof = tidy_rebase::RB_CLASS;
            row.bp.detail = format!(
                "rebase-identity ({} of {} commits, run of {}, min {min} %): review, not a deletion proof",
                report.mapped, report.n, report.run_len
            );
        }
        row.bp.rebase = Some(RebasePlan { report, api });
        Ok(())
    }

    /// What the pull request says about this branch, or why it says nothing. At most `MAX_PR_REQUESTS` requests.
    fn api_input(&mut self, row: &Row, local_strong: bool) -> ApiInput {
        use tidy_rebase::ApiInput::NotRead;
        if local_strong || !judged(row.bp.proof) {
            return NotRead(ApiReason::NotNeeded);
        }
        let Some(prs) = self.prs else {
            return NotRead(match self.api {
                PrApi::Off(r) => r,
                PrApi::Client(_) => ApiReason::NoPullRequest,
            });
        };
        let Some(pr) = row
            .pr_moved
            .and_then(|n| prs.iter().find(|p| p.number == n))
        else {
            return NotRead(ApiReason::NoPullRequest);
        };
        let c = match self.api {
            PrApi::Off(r) => return NotRead(r),
            PrApi::Client(c) => c,
        };
        let Some(slug) = self.slug else {
            return NotRead(ApiReason::NoPullRequest);
        };
        let parse = |h: &str| ObjectId::from_hex(h.as_bytes()).ok();
        let (Some(merge_commit), Some(head)) = (parse(&pr.merge_commit_sha), parse(&pr.head_sha))
        else {
            return NotRead(ApiReason::NotMerged);
        };
        if self.requests_left == 0 || c.rate_limited() {
            return NotRead(ApiReason::RateLimited);
        }
        self.requests_left -= 1;
        match c.pr_commits(slug, pr.number) {
            Ok(PrCommitsRead::Commits(commits)) => ApiInput::Pr(PrInput {
                number: pr.number,
                merged: true,
                merge_commit,
                merged_at: pr.merged_at,
                head,
                commits,
            }),
            Ok(PrCommitsRead::TooMany) => NotRead(ApiReason::TooManyCommits),
            Err(_) if c.rate_limited() => NotRead(ApiReason::RateLimited),
            Err(_) => NotRead(ApiReason::RequestFailed),
        }
    }
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
                           "worktree": b.worktree.as_ref().map(|w| json!({"path": worktrees::show(&w.dir), "removable_proof": w.proof, "outside_worktrees_dir": w.outside, "blocker": jopt(w.blocker.as_deref())})).unwrap_or(Value::Null),
                           "similarity": b.similarity.as_ref().map(similarity_json).unwrap_or(Value::Null)})
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
           "summary": {"delete": d, "delete_with_worktree": w, "review": r, "keep": k, "similar": similar_count(plans)}})
}

/// A value in percent with one decimal, as a JSON number.
fn pct(permille: u32) -> Value {
    json!(f64::from(permille) / 10.0)
}

fn similarity_json(s: &SimilarityPlan) -> Value {
    use tidy_similarity::Outcome;
    let shown = |x: &tidy_similarity::Sim| pct(x.shown_in(s.high, s.low));
    let commits: Vec<Value> = s
        .report
        .commits
        .iter()
        .map(|c| {
            let (outcome, skipped, cand, value, no_path, reason, evidence) = match &c.outcome {
                Outcome::Skipped(k) => (
                    "skipped",
                    json!(k.as_str()),
                    None,
                    Value::Null,
                    Value::Null,
                    "none",
                    Vec::new(),
                ),
                Outcome::NoCandidate => (
                    "no-candidate",
                    Value::Null,
                    None,
                    Value::Null,
                    Value::Null,
                    "none",
                    Vec::new(),
                ),
                Outcome::Compared(x) => (
                    "compared",
                    Value::Null,
                    Some(x),
                    shown(&x.sim),
                    pct(x.no_path.permille()),
                    x.reason.as_str(),
                    x.evidence
                        .iter()
                        .map(|e| json!({"path": e.path, "sign": e.sign.to_string(), "line": e.line, "count_a": e.count_a, "count_b": e.count_b}))
                        .collect(),
                ),
            };
            json!({"commit": c.commit.to_string(), "subject": c.subject, "outcome": outcome, "skipped": skipped,
                   "candidate": cand.map(|x| Value::String(x.candidate.to_string())).unwrap_or(Value::Null),
                   "candidate_subject": cand.map(|x| Value::String(x.candidate_subject.clone())).unwrap_or(Value::Null),
                   "reason": reason, "similarity": value, "similarity_no_path": no_path, "evidence": evidence})
        })
        .collect();
    let r = &s.report;
    json!({"class": r.class.as_str(), "threshold": s.high, "threshold_low": s.low,
           "window_days": tidy_similarity::WINDOW_DAYS,
           "plus_commits": r.plus_commits, "at_or_above": r.at_or_above,
           "min": r.min.as_ref().map(shown).unwrap_or(Value::Null),
           "median": r.median_permille.map(pct).unwrap_or(Value::Null),
           "without_candidate": r.without_candidate,
           "skipped": {"merges": r.skipped.merges, "empty": r.skipped.empty, "too_large": r.skipped.too_large, "shallow": s.skipped_shallow},
           "bounds": {"candidates_cut": r.candidates_cut, "main_commits_indexed": s.indexed},
           "commits": commits})
}

/// Branches with the proof `similar-patches` (they are counted in `review`, too).
pub fn similar_count(plans: &[RepoPlan]) -> usize {
    plans
        .iter()
        .flat_map(|p| &p.branches)
        .filter(|b| b.proof == "similar-patches")
        .count()
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
    let similar = similar_count(plans);
    let of_them = if similar > 0 {
        format!(" ({similar} of them similar-patches)")
    } else {
        String::new()
    };
    out += &format!(
        "{d} to delete, {w} to delete with their worktree, {r} to review{of_them}, {k} kept (read-only: nothing was changed)\n"
    );
    if !all && k > 0 {
        out += "(--all lists the kept branches)\n";
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    fn bare(detail: &str) -> BranchPlan {
        BranchPlan {
            branch: "b".into(),
            tip: "0".repeat(40),
            decision: "keep",
            proof: "unmerged",
            detail: detail.into(),
            unique: 1,
            upstream: None,
            track: None,
            worktree: None,
            similarity: None,
            rebase: None,
            landed_via: LandedVia {
                kind: LandedKind::Unknown,
                basis: Basis::None,
                strength: Strength::None,
            },
        }
    }

    #[test]
    fn a_failed_stage_is_named_in_the_detail_and_the_proof_stays() {
        let mut bp = bare("1 commit(s) not in base");
        stage_failed(&mut bp, &Error::repository("object missing"));
        assert_eq!(
            bp.detail,
            "1 commit(s) not in base; rebase evidence not computed (object missing)"
        );
        assert_eq!((bp.decision, bp.proof), ("keep", "unmerged"));
        assert!(bp.rebase.is_none());
    }
}
