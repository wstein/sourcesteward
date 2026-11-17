//! Rebase-merge proof for `stw tidy plan`; specification: `scratch/stw-tidy-rebase/spec.md`, summary in
//! `spikes/stw/tidy-similarity/README.md` (package 1b). Pure library functions: no CLI, no plan, no network.
//!
//! A rebase merge writes the commits of a branch again (new ID, new committer, new parent) and keeps author, author
//! time and subject. The proof is built in four parts, each usable alone:
//! (a) identity: the key `(author mail in lower case, author seconds, subject)` plus the exact author name finds the
//! rewritten commit on the first-parent chain of main (`RebaseIndex`, `evaluate`);
//! (b) one contiguous run: the matched commits sit directly behind each other in the same order;
//! (c) the mapping table `mapping` (branch commit -> main commit, method, value) and the rest `remaining`;
//! (d) the pull request (`merge_commit_sha` and the PR commits, given as input) and `landed_via`.
//! Identity is evidence for a human decision (`review`), never a deletion proof.
//!
//! Not applied: mailmap (raw header values are compared), `Co-authored-by`, committer (a rebase merge writes a new one).
//! Zones: only seconds since the epoch are compared; a different offset is noted (`tz_differs`).
// Shell while the tests of a stage are written first: the functions of later stages are inert.
#![allow(dead_code, unused_imports, unused_variables)]

use std::collections::{HashMap, HashSet};

use gix::bstr::ByteSlice;
use gix::ObjectId;

use crate::error::Result;
use crate::patch_id::{changed_files, commit_patch_key};
use crate::tidy_similarity::{mask_bytes, Limits, Sim, SimCache, Skip, LOW_PCT};

/// The index stops at a commit whose committer time is older than the oldest author time of the branches by more
/// than this (a rebase merge writes a committer time at or after the author time; this covers clock skew).
pub const RB_SLACK_SECS: i64 = 86_400;
/// At most this many commits of the first-parent chain are indexed.
pub const RB_MAX_WINDOW: usize = 50_000;
/// An identity match whose content similarity is below this percentage is only a hint (same metadata, other content).
pub const RB_CONTENT_FLOOR: u32 = 50;
/// At most this many files per commit in `remaining`.
pub const RB_MAX_REMAINING_FILES: usize = 20;
/// A squash commit has the author time of the merge: `merged_at` plus or minus this many seconds.
pub const RB_API_TIME_TOLERANCE: i64 = 2;
/// Subjects are shown cut at this many characters.
pub const RB_SUBJECT_CHARS: usize = 120;
/// Class and decision of a strong proof. The decision is never `delete`.
pub const RB_CLASS: &str = "rebase-identity";
pub const RB_DECISION: &str = "review";

/// First line of a message, white space at the right end removed (bytes; no prefix strip, no `(#N)` strip).
pub fn subject_of(msg: &[u8]) -> Vec<u8> {
    Vec::new()
}

/// What a rebase keeps: author mail (ASCII lower case), author seconds and subject.
#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub struct Key {
    pub mail: Vec<u8>,
    pub secs: i64,
    pub subject: Vec<u8>,
}

/// The header data of one commit that the proof reads (no diff).
#[derive(Clone, Debug)]
pub struct Head {
    pub id: ObjectId,
    pub name: Vec<u8>,
    pub key: Key,
    /// Zone offset of the author time in seconds east of UTC.
    pub offset: i32,
    pub committer_secs: i64,
    pub parents: usize,
}

pub fn read_head(repo: &gix::Repository, id: ObjectId) -> Result<Head> {
    Ok(Head {
        id,
        name: Vec::new(),
        key: Key {
            mail: Vec::new(),
            secs: 0,
            subject: Vec::new(),
        },
        offset: 0,
        committer_secs: 0,
        parents: 0,
    })
}

/// Why the walk over the first-parent chain of main ended.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Stop {
    /// The first commit of the history.
    Root,
    /// A commit older than the oldest author time minus `RB_SLACK_SECS`.
    Time,
    /// `RB_MAX_WINDOW` commits (`bounds.window_cut`).
    Window,
    /// An object is missing (shallow clone, partial fetch): this commit and everything behind it is not indexed.
    Missing(ObjectId),
}

/// The first-parent chain of the base tip: position 0 is the tip, 1 its first parent, and so on. Merge commits are
/// positions but never targets.
pub struct RebaseIndex {
    positions: Vec<Head>,
    by_key: HashMap<Key, Vec<u32>>,
    by_id: HashMap<ObjectId, u32>,
    stop: Stop,
}

impl RebaseIndex {
    /// Walk the chain from `base_tip`. `since` is the oldest author time of the commits to match; without it only
    /// `max_window` limits the walk. The tip itself must exist; a missing object further down ends the walk (`Stop::Missing`).
    pub fn build(
        repo: &gix::Repository,
        base_tip: ObjectId,
        since: Option<i64>,
        max_window: usize,
    ) -> Result<RebaseIndex> {
        Ok(RebaseIndex {
            positions: Vec::new(),
            by_key: HashMap::new(),
            by_id: HashMap::new(),
            stop: Stop::Root,
        })
    }

    pub fn len(&self) -> usize {
        self.positions.len()
    }
    pub fn is_empty(&self) -> bool {
        self.positions.is_empty()
    }
    pub fn stop(&self) -> Stop {
        self.stop
    }
    /// The walk ended at `RB_MAX_WINDOW`: older commits were not looked at.
    pub fn window_cut(&self) -> bool {
        self.stop == Stop::Window
    }
    pub fn head(&self, pos: u32) -> Option<&Head> {
        self.positions.get(pos as usize)
    }
    pub fn pos_of(&self, id: ObjectId) -> Option<u32> {
        self.by_id.get(&id).copied()
    }
    /// Positions of the non-merge commits with this key (ascending: newest first).
    pub fn with_key(&self, key: &Key) -> &[u32] {
        self.by_key.get(key).map_or(&[], |v| v.as_slice())
    }
}

/// Commits of `base..tip` that do not count, for information.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct SideSkipped {
    /// Merge commits of the branch: not part of the mapping.
    pub merges: usize,
    /// Commits without a change: they count (identity needs no diff).
    pub empty: usize,
}

/// The branch side: the non-merge commits of `base..tip`, oldest first (parents before children; ties by author
/// time, then commit ID).
#[derive(Clone, Debug, Default)]
pub struct BranchSide {
    pub commits: Vec<ObjectId>,
    pub skipped: SideSkipped,
    /// The oldest author time of `commits`: the `since` of the index.
    pub oldest_author_secs: Option<i64>,
}

pub fn branch_side(repo: &gix::Repository, base: ObjectId, tip: ObjectId) -> Result<BranchSide> {
    Ok(BranchSide::default())
}

/// Patch ID matches: branch commit -> commit on the chain with an equal patch (ID and white-space digest). The
/// oldest branch commit takes the oldest chain commit among equals. Costs one diff per non-merge commit of the
/// window; a caller that already walked main for equal patches can pass its own map to `evaluate`.
pub fn patch_hits(
    repo: &gix::Repository,
    index: &RebaseIndex,
    commits: &[ObjectId],
) -> Result<HashMap<ObjectId, ObjectId>> {
    Ok(HashMap::new())
}

/// How a branch commit was found on main.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Method {
    PatchId,
    Identity,
    Similarity,
    Api,
    None,
}

impl Method {
    pub fn as_str(self) -> &'static str {
        match self {
            Method::PatchId => "patch-id",
            Method::Identity => "identity",
            Method::Similarity => "similarity",
            Method::Api => "api",
            Method::None => "none",
        }
    }
}

/// One row of the mapping table.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct MapEntry {
    pub commit: ObjectId,
    /// Masked and cut (`mask_bytes`).
    pub subject: String,
    pub main: Option<ObjectId>,
    pub position: Option<u32>,
    pub method: Method,
    /// Tenths of a percent; `None` where no content value was computed.
    pub value: Option<u32>,
    /// The identity key (and the name) matched this main commit, whatever the method says.
    pub identity: bool,
    pub tz_differs: bool,
    /// More than one free main commit has the key and no neighbour decides: counts as not mapped.
    pub ambiguous: bool,
}

/// A commit without a target.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Remaining {
    pub commit: ObjectId,
    pub subject: String,
    /// Paths of the commit against its first parent, sorted, at most `RB_MAX_REMAINING_FILES`, masked.
    pub files: Vec<String>,
    pub files_total: usize,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Strength {
    Strong,
    Hint,
    None,
}

impl Strength {
    pub fn as_str(self) -> &'static str {
        match self {
            Strength::Strong => "strong",
            Strength::Hint => "hint",
            Strength::None => "none",
        }
    }
}

#[derive(Clone, Debug)]
pub struct RebaseReport {
    pub strength: Strength,
    pub n: usize,
    pub mapped: usize,
    pub by_patch_id: usize,
    pub by_identity: usize,
    pub by_similarity: usize,
    pub unmapped: usize,
    pub ambiguous: usize,
    /// The positions of the mapped commits are one block on the chain (`gap == 0`), at least one.
    pub contiguous: bool,
    /// The positions fall strictly in branch order (older commit, greater position).
    pub in_order: bool,
    /// Chain positions between the first and the last target that are not targets (foreign commits, merges).
    pub gap: usize,
    /// Maximal runs: neighbours in the branch that are neighbours on the chain, in order.
    pub runs: usize,
    pub run_len: usize,
    /// Some but not all commits are mapped, as one ordered block.
    pub partial: bool,
    /// No identity match has a content value below `RB_CONTENT_FLOOR` and none is unknown.
    pub content_floor_ok: bool,
    pub skipped: SideSkipped,
    pub stop: Stop,
    pub window_commits: usize,
    /// In branch order.
    pub mapping: Vec<MapEntry>,
    pub remaining: Vec<Remaining>,
}

/// Class and decision of a strong proof; `None` for a hint or no match. The decision is `review`.
pub fn evidence_class(r: &RebaseReport) -> Option<(&'static str, &'static str)> {
    None
}

/// The assignment of branch commits to chain positions.
struct Assigned {
    pos: Vec<Option<u32>>,
    method: Vec<Method>,
    identity: Vec<bool>,
    ambiguous: Vec<bool>,
}

/// Identity candidates of a branch commit: positions with the key and the exact author name.
fn identity_candidates(index: &RebaseIndex, h: &Head) -> Vec<u32> {
    index
        .with_key(&h.key)
        .iter()
        .copied()
        .filter(|p| index.positions[*p as usize].name == h.name)
        .collect()
}

/// Patch ID hits first, then identity. Each position is taken at most once (multiset). Where several free
/// positions have the key, the neighbours decide: the position right in front of the previous commit's target (older is
/// greater) or right behind the next one's.
#[allow(clippy::needless_range_loop)] // parallel arrays indexed by branch position
fn assign(
    index: &RebaseIndex,
    heads: &[Head],
    ids: &[ObjectId],
    hits: &HashMap<ObjectId, ObjectId>,
) -> Assigned {
    let n = heads.len();
    Assigned {
        pos: vec![None; n],
        method: vec![Method::None; n],
        identity: vec![false; n],
        ambiguous: vec![false; n],
    }
}

/// Content of a pair of commits for the floor: the similarity, or why there is none.
enum Content {
    Value(Sim),
    /// Both commits are empty: nothing to compare, nothing against it.
    BothEmpty,
    /// A commit is too large to compare.
    Unknown,
}

fn content_of(
    repo: &gix::Repository,
    cache: &mut SimCache,
    a: ObjectId,
    b: ObjectId,
    lim: &Limits,
) -> Result<Content> {
    let (ka, kb) = (cache.keys(repo, a, lim)?, cache.keys(repo, b, lim)?);
    Ok(match (ka, kb) {
        (Ok(x), Ok(y)) => Content::Value(x.similarity(&y)),
        (Err(Skip::Empty), Err(Skip::Empty)) => Content::BothEmpty,
        (Err(Skip::Empty), Ok(y)) => Content::Value(Sim {
            inter: 0,
            a: 0,
            b: y.len() as u64,
        }),
        (Ok(x), Err(Skip::Empty)) => Content::Value(Sim {
            inter: 0,
            a: x.len() as u64,
            b: 0,
        }),
        _ => Content::Unknown,
    })
}

/// Paths of a commit against its first parent, sorted, masked; (first `RB_MAX_REMAINING_FILES`, total).
fn files_of(repo: &gix::Repository, id: ObjectId) -> Result<(Vec<String>, usize)> {
    let c = repo.find_commit(id)?;
    let new_tree = c.tree()?;
    let old_tree = match c.parent_ids().next() {
        Some(p) => repo.find_commit(p.detach())?.tree()?,
        None => repo.empty_tree(),
    };
    let mut paths: Vec<Vec<u8>> = changed_files(repo, &old_tree, &new_tree)?
        .iter()
        .map(|f| f.path.to_vec())
        .collect();
    paths.sort();
    paths.dedup();
    let total = paths.len();
    let files = paths
        .iter()
        .take(RB_MAX_REMAINING_FILES)
        .map(|p| mask_bytes(p, 200))
        .collect();
    Ok((files, total))
}

/// Subject (or any text of a commit) for output: masked and cut.
fn show(text: &[u8]) -> String {
    String::from_utf8_lossy(text).into_owned()
}

/// Where the similarity (a gap of one between two mapped neighbours) and the content value come in. Fills the
/// positions in `a` and the values; returns whether the content floor holds.
fn refine(
    repo: &gix::Repository,
    cache: &mut SimCache,
    index: &RebaseIndex,
    heads: &[Head],
    a: &mut Assigned,
    value: &mut [Option<u32>],
    lim: &Limits,
) -> Result<bool> {
    Ok(true)
}

/// The commits without a target, with their files.
fn remaining_of(repo: &gix::Repository, mapping: &[MapEntry]) -> Result<Vec<Remaining>> {
    Ok(Vec::new())
}

/// The shape of the mapped positions and the strength of the proof.
struct Shape {
    contiguous: bool,
    in_order: bool,
    gap: usize,
    runs: usize,
    run_len: usize,
    partial: bool,
    strength: Strength,
}

/// `mapped_at` holds (branch index, chain position) of the mapped commits in branch order.
fn shape(mapped_at: &[(usize, u32)], n: usize, floor_ok: bool, identity_backed: bool) -> Shape {
    Shape {
        contiguous: false,
        in_order: false,
        gap: 0,
        runs: 0,
        run_len: 0,
        partial: false,
        strength: Strength::None,
    }
}

/// The proof for one branch: identity (a), run (b), mapping and rest (c).
pub fn evaluate(
    repo: &gix::Repository,
    cache: &mut SimCache,
    index: &RebaseIndex,
    side: &BranchSide,
    hits: &HashMap<ObjectId, ObjectId>,
    lim: &Limits,
) -> Result<RebaseReport> {
    let ids = &side.commits;
    let heads: Vec<Head> = ids
        .iter()
        .map(|id| read_head(repo, *id))
        .collect::<Result<_>>()?;
    let n = ids.len();
    let mut a = assign(index, &heads, ids, hits);
    let mut value: Vec<Option<u32>> = vec![None; n];
    let floor_ok = refine(repo, cache, index, &heads, &mut a, &mut value, lim)?;

    let mut mapping = Vec::with_capacity(n);
    for i in 0..n {
        let main = a.pos[i].map(|p| index.positions[p as usize].id);
        let tz = a.pos[i]
            .filter(|_| a.identity[i])
            .is_some_and(|p| index.positions[p as usize].offset != heads[i].offset);
        mapping.push(MapEntry {
            commit: ids[i],
            subject: show(&heads[i].key.subject),
            main,
            position: a.pos[i],
            method: a.method[i],
            value: if a.method[i] == Method::PatchId {
                Some(1000)
            } else {
                value[i]
            },
            identity: a.identity[i],
            tz_differs: tz,
            ambiguous: a.ambiguous[i],
        });
    }
    let remaining = remaining_of(repo, &mapping)?;

    let mapped_at: Vec<(usize, u32)> = (0..n).filter_map(|i| a.pos[i].map(|p| (i, p))).collect();
    let mapped = mapped_at.len();
    let identity_backed = (0..n).any(|i| a.pos[i].is_some() && a.identity[i]);
    let sh = shape(&mapped_at, n, floor_ok, identity_backed);
    let count = |m: Method| {
        (0..n)
            .filter(|i| a.pos[*i].is_some() && a.method[*i] == m)
            .count()
    };
    Ok(RebaseReport {
        strength: sh.strength,
        n,
        mapped,
        by_patch_id: count(Method::PatchId),
        by_identity: count(Method::Identity),
        by_similarity: count(Method::Similarity),
        unmapped: n - mapped,
        ambiguous: a.ambiguous.iter().filter(|x| **x).count(),
        contiguous: sh.contiguous,
        in_order: sh.in_order,
        gap: sh.gap,
        runs: sh.runs,
        run_len: sh.run_len,
        partial: sh.partial,
        content_floor_ok: floor_ok,
        skipped: side.skipped,
        stop: index.stop(),
        window_commits: index.len(),
        mapping,
        remaining,
    })
}

// ---- (d) the pull request ----

/// A commit of the pull request as the API lists it (author data and subject; oldest first in `PrInput`).
#[derive(Clone, Debug)]
pub struct PrCommit {
    pub id: ObjectId,
    pub name: Vec<u8>,
    pub mail: Vec<u8>,
    pub secs: i64,
    pub subject: Vec<u8>,
}

/// What `GET pulls/N` and `GET pulls/N/commits` gave; read by the caller, this module does not fetch.
#[derive(Clone, Debug)]
pub struct PrInput {
    pub number: u64,
    pub merged: bool,
    pub merge_commit: ObjectId,
    /// Seconds since the epoch.
    pub merged_at: Option<i64>,
    pub head: ObjectId,
    pub commits: Vec<PrCommit>,
}

/// Why there is no pull request mapping.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum ApiReason {
    Offline,
    NoToken,
    NoPullRequest,
    RateLimited,
    NotMerged,
    MergeCommitNotInClone,
    /// The merge commit is not on the first-parent chain of the base within the window.
    NotOnBase,
}

impl ApiReason {
    pub fn as_str(self) -> &'static str {
        match self {
            ApiReason::Offline => "offline",
            ApiReason::NoToken => "no-token",
            ApiReason::NoPullRequest => "no-pull-request",
            ApiReason::RateLimited => "rate-limited",
            ApiReason::NotMerged => "not-merged",
            ApiReason::MergeCommitNotInClone => "merge-commit-not-in-clone",
            ApiReason::NotOnBase => "not-on-base",
        }
    }
}

pub enum ApiInput {
    NotRead(ApiReason),
    Pr(PrInput),
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum LandedKind {
    Merge,
    Rebase,
    Squash,
    Unknown,
}

impl LandedKind {
    pub fn as_str(self) -> &'static str {
        match self {
            LandedKind::Merge => "merge",
            LandedKind::Rebase => "rebase",
            LandedKind::Squash => "squash",
            LandedKind::Unknown => "unknown",
        }
    }
}

#[derive(Clone, Debug)]
pub struct ApiReport {
    /// `None`: read; `Some(reason)`: not read.
    pub not_read: Option<ApiReason>,
    pub pr: Option<u64>,
    pub merge_commit: Option<ObjectId>,
    pub merge_parents: Option<usize>,
    pub k: Option<usize>,
    /// The k commits `merge_commit~0..k-1` are identity-equal to the PR commits, in order.
    pub tip_matches_run: Option<bool>,
    /// The PR head is the branch tip or an ancestor of it.
    pub head_in_branch: Option<bool>,
    pub kind: LandedKind,
    /// Branch commits that are PR commits, with their rewritten counterparts (method `api`).
    pub mapping: Vec<MapEntry>,
    /// Branch commits that are not PR commits (added after the PR head).
    pub remaining: Vec<Remaining>,
}

impl ApiReport {
    fn not_read(reason: ApiReason) -> ApiReport {
        ApiReport {
            not_read: Some(reason),
            pr: None,
            merge_commit: None,
            merge_parents: None,
            k: None,
            tip_matches_run: None,
            head_in_branch: None,
            kind: LandedKind::Unknown,
            mapping: Vec::new(),
            remaining: Vec::new(),
        }
    }
}

fn same_identity(h: &Head, c: &PrCommit) -> bool {
    h.name == c.name
        && h.key.mail == c.mail.to_ascii_lowercase()
        && h.key.secs == c.secs
        && h.key.subject == subject_of(&c.subject)
}

/// The pull request mapping (d): `merge_commit` is the tip of the k rewritten commits.
///
/// - two parents: a merge commit;
/// - one parent and the k commits `merge_commit~0..k-1` equal the PR commits in author, time and subject: rebase;
/// - one parent, author time = `merged_at` (+-`RB_API_TIME_TOLERANCE`): squash;
/// - otherwise unknown.
///
/// `base_tip` is the tip of main; `side` the branch commits, `tip` the branch tip.
pub fn evaluate_api(
    repo: &gix::Repository,
    base_tip: ObjectId,
    side: &BranchSide,
    tip: ObjectId,
    input: &ApiInput,
) -> Result<ApiReport> {
    Ok(ApiReport::not_read(ApiReason::NoPullRequest))
}

// ---- landed_via ----

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Basis {
    Ancestor,
    CommitMap,
    PrApi,
    SingleCommitMap,
    None,
}

impl Basis {
    pub fn as_str(self) -> &'static str {
        match self {
            Basis::Ancestor => "ancestor",
            Basis::CommitMap => "commit-map",
            Basis::PrApi => "pr-api",
            Basis::SingleCommitMap => "single-commit-map",
            Basis::None => "none",
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct LandedVia {
    pub kind: LandedKind,
    pub basis: Basis,
    pub strength: Strength,
}

/// How a branch landed, one value with its basis. Precedence: tip is an ancestor of the base (merge); a strong
/// commit mapping (rebase); the pull request (merge, rebase, squash); a single commit with an equal patch but no
/// identity (unknown, a hint for a squash); otherwise unknown. A partial or weak mapping is `unknown`. The decision
/// of a branch never changes because of `landed_via`.
pub fn landed_via(
    ancestor: bool,
    rebase: Option<&RebaseReport>,
    api: Option<&ApiReport>,
) -> LandedVia {
    LandedVia {
        kind: LandedKind::Unknown,
        basis: Basis::None,
        strength: Strength::None,
    }
}
