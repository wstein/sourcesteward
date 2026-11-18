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
//! Identity is evidence for a human decision (`review`), never a deletion proof. A proof is strong only with a content
//! value of at least 95 % per commit and without a later version (an identity match whose branch commit was committed
//! after the main commit: an amend or fixup after landing keeps the author time). The index stops by a run of old author
//! times, never by the committer time. A rebase followed by `merge --ff-only` leaves the branch commits on main, so
//! the tip is an ancestor and `landed_via` says `merge`/`ancestor` (the name is misleading for that flow).
//!
//! Not applied: mailmap (raw header values are compared), `Co-authored-by`, committer (a rebase merge writes a new one).
//! Zones: only seconds since the epoch are compared; a different offset is noted (`tz_differs`).
use std::collections::{HashMap, HashSet};

use gix::bstr::ByteSlice;
use gix::ObjectId;

use crate::error::Result;
use crate::patch_id::{changed_files, commit_patch_key, PatchKey};
use crate::tidy_similarity::{mask_bytes, Limits, Sim, SimCache, Skip, LOW_PCT};

/// A chain commit counts as old when its author time is older than the oldest author time of the branches by more
/// than this.
pub const RB_SLACK_SECS: i64 = 86_400;
/// The walk stops after this many chain commits in a row with an author time older than the oldest author time of the
/// branches minus `RB_SLACK_SECS`.
pub const RB_STOP_RUN: usize = 64;
/// At most this many commits of the first-parent chain are indexed.
pub const RB_MAX_WINDOW: usize = 50_000;
/// A proof needs this content similarity (percent) per commit; from `LOW_PCT` up to this a commit is `modified`.
pub const RB_CONTENT_HIGH: u32 = 95;
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
    let first = msg.lines().next().unwrap_or_default();
    let ws = |b: &u8| matches!(b, b' ' | b'\t' | b'\r' | b'\n' | 0x0b | 0x0c);
    let end = first.iter().rposition(|b| !ws(b)).map_or(0, |i| i + 1);
    first[..end].to_vec()
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
    let c = repo.find_commit(id)?;
    let a = c.author()?;
    let time = a.time()?;
    Ok(Head {
        id,
        name: a.name.to_vec(),
        key: Key {
            mail: a.email.to_ascii_lowercase().to_vec(),
            secs: a.seconds(),
            subject: subject_of(c.message_raw()?),
        },
        offset: time.offset,
        committer_secs: c.committer()?.seconds(),
        parents: c.parent_ids().count(),
    })
}

/// Why the walk over the first-parent chain of main ended.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Stop {
    /// The first commit of the history.
    Root,
    /// `RB_STOP_RUN` commits in a row older (by author time) than the oldest author time of the branch minus `RB_SLACK_SECS`.
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
        let mut idx = RebaseIndex {
            positions: Vec::new(),
            by_key: HashMap::new(),
            by_id: HashMap::new(),
            stop: Stop::Root,
        };
        let mut next = Some(base_tip);
        let mut old_run = 0;
        while let Some(id) = next {
            if idx.positions.len() >= max_window {
                idx.stop = Stop::Window;
                break;
            }
            // only a missing object ends the walk quietly (shallow or partial clone); any other error is an error
            if !idx.positions.is_empty() && !repo.has_object(id) {
                idx.stop = Stop::Missing(id);
                break;
            }
            let head = read_head(repo, id)?;
            // the committer time is not looked at: it can be set to anything
            if since.is_some_and(|s| head.key.secs < s - RB_SLACK_SECS) {
                old_run += 1;
            } else {
                old_run = 0;
            }
            let pos = idx.positions.len() as u32;
            if head.parents <= 1 {
                idx.by_key.entry(head.key.clone()).or_default().push(pos);
            }
            idx.by_id.insert(id, pos);
            next = repo
                .find_commit(id)?
                .parent_ids()
                .next()
                .map(|p| p.detach());
            idx.positions.push(head);
            if old_run >= RB_STOP_RUN {
                idx.stop = Stop::Time;
                break;
            }
        }
        Ok(idx)
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
    /// The last commit of a walk that ended at a missing object: its parent is not there, so it has no patch to read.
    pub fn is_edge(&self, pos: u32) -> bool {
        matches!(self.stop, Stop::Missing(_)) && pos as usize + 1 == self.positions.len()
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
    let mut set: HashMap<ObjectId, Head> = HashMap::new();
    let mut parents_of: HashMap<ObjectId, Vec<ObjectId>> = HashMap::new();
    for info in repo.rev_walk([tip]).with_hidden([base]).all()? {
        let id = info?.id;
        let c = repo.find_commit(id)?;
        parents_of.insert(id, c.parent_ids().map(|p| p.detach()).collect());
        set.insert(id, read_head(repo, id)?);
    }
    // Kahn: a commit is ready when its parents inside the set are done
    let mut waiting: HashMap<ObjectId, usize> = parents_of
        .iter()
        .map(|(id, ps)| (*id, ps.iter().filter(|p| set.contains_key(*p)).count()))
        .collect();
    let mut children: HashMap<ObjectId, Vec<ObjectId>> = HashMap::new();
    for (id, ps) in &parents_of {
        for p in ps.iter().filter(|p| set.contains_key(*p)) {
            children.entry(*p).or_default().push(*id);
        }
    }
    // ready commits, the oldest author time first (ties by commit ID)
    let mut ready: std::collections::BinaryHeap<std::cmp::Reverse<(i64, ObjectId)>> = waiting
        .iter()
        .filter(|(_, n)| **n == 0)
        .map(|(id, _)| std::cmp::Reverse((set[id].key.secs, *id)))
        .collect();
    let mut out = BranchSide::default();
    while let Some(std::cmp::Reverse((_, id))) = ready.pop() {
        for ch in children.get(&id).into_iter().flatten() {
            let w = waiting.get_mut(ch).expect("child is in the set");
            *w -= 1;
            if *w == 0 {
                ready.push(std::cmp::Reverse((set[ch].key.secs, *ch)));
            }
        }
        let head = &set[&id];
        if head.parents > 1 {
            out.skipped.merges += 1;
            continue;
        }
        let c = repo.find_commit(id)?;
        let old_tree = match c.parent_ids().next() {
            Some(p) => repo.find_commit(p.detach())?.tree_id()?.detach(),
            None => repo.empty_tree().id,
        };
        if old_tree == c.tree_id()?.detach() {
            out.skipped.empty += 1;
        }
        out.oldest_author_secs = Some(
            out.oldest_author_secs
                .map_or(head.key.secs, |o| o.min(head.key.secs)),
        );
        out.commits.push(id);
    }
    Ok(out)
}

/// Patch ID matches: branch commit -> commit on the chain with an equal patch (ID and white-space digest). The
/// oldest branch commit takes the oldest chain commit among equals. Costs one diff per non-merge commit of the
/// window; `patch_hits_with` takes the keys from a lookup (the plan passes its `PatchCache`).
pub fn patch_hits(
    repo: &gix::Repository,
    index: &RebaseIndex,
    commits: &[ObjectId],
) -> Result<HashMap<ObjectId, ObjectId>> {
    patch_hits_with(index, commits, &mut |id| commit_patch_key(repo, id))
}

/// `patch_hits` with the patch keys taken from `key` (the plan passes its `PatchCache`, which already holds the keys of
/// the commits the equal-patches walk read).
pub fn patch_hits_with(
    index: &RebaseIndex,
    commits: &[ObjectId],
    key: &mut dyn FnMut(ObjectId) -> Result<Option<PatchKey>>,
) -> Result<HashMap<ObjectId, ObjectId>> {
    let mut wanted: HashMap<(ObjectId, ObjectId), Vec<ObjectId>> = HashMap::new();
    for c in commits {
        if let Some(k) = key(*c)? {
            wanted.entry(k).or_default().push(*c);
        }
    }
    let mut hits = HashMap::new();
    if wanted.is_empty() {
        return Ok(hits);
    }
    // the lists of chain commits are newest first (`pop` takes the oldest); branch commits are visited oldest first,
    // so the oldest branch commit meets the oldest chain commit. The boundary commit of a shallow clone has no patch.
    let mut on_main: HashMap<(ObjectId, ObjectId), Vec<ObjectId>> = HashMap::new();
    for (pos, h) in index.positions.iter().enumerate() {
        if h.parents > 1 || index.is_edge(pos as u32) {
            continue;
        }
        if let Some(k) = key(h.id)? {
            if wanted.contains_key(&k) {
                on_main.entry(k).or_default().push(h.id);
            }
        }
    }
    for (k, branch) in wanted {
        let mut main = on_main.remove(&k).unwrap_or_default();
        for b in branch {
            if let Some(m) = main.pop() {
                hits.insert(b, m);
            }
        }
    }
    Ok(hits)
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
    /// The branch commit was committed after the main commit: a later version than the landed one.
    pub later: bool,
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
    /// Every commit has a content value of at least `RB_CONTENT_HIGH` and none is a later version (committed after
    /// the main commit, an amend or fixup after landing).
    pub content_ok: bool,
    /// Some value is below `RB_CONTENT_HIGH` but all are at least `LOW_PCT`.
    pub modified: bool,
    pub skipped: SideSkipped,
    pub stop: Stop,
    pub window_commits: usize,
    /// In branch order.
    pub mapping: Vec<MapEntry>,
    pub remaining: Vec<Remaining>,
}

/// Class and decision of a strong proof; `None` for a hint or no match. The decision is `review`.
pub fn evidence_class(r: &RebaseReport) -> Option<(&'static str, &'static str)> {
    (r.strength == Strength::Strong).then_some((RB_CLASS, RB_DECISION))
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
    let cands: Vec<Vec<u32>> = heads
        .iter()
        .map(|h| identity_candidates(index, h))
        .collect();
    let mut a = Assigned {
        pos: vec![None; n],
        method: vec![Method::None; n],
        identity: vec![false; n],
        ambiguous: vec![false; n],
    };
    let mut used: HashSet<u32> = HashSet::new();
    for i in 0..n {
        let hit = hits
            .get(&ids[i])
            .and_then(|m| index.pos_of(*m))
            .filter(|p| index.positions[*p as usize].parents <= 1);
        if let Some(p) = hit {
            if used.insert(p) {
                a.pos[i] = Some(p);
                a.method[i] = Method::PatchId;
                a.identity[i] = cands[i].contains(&p);
            }
        }
    }
    loop {
        let mut progress = false;
        for i in 0..n {
            if a.pos[i].is_some() {
                continue;
            }
            let free: Vec<u32> = cands[i]
                .iter()
                .copied()
                .filter(|p| !used.contains(p))
                .collect();
            let pick = match free.len() {
                0 => None,
                1 => Some(free[0]),
                _ => {
                    let before = i
                        .checked_sub(1)
                        .and_then(|j| a.pos[j])
                        .and_then(|p| p.checked_sub(1));
                    let after = a.pos.get(i + 1).copied().flatten().map(|p| p + 1);
                    [before, after]
                        .into_iter()
                        .flatten()
                        .find(|p| free.contains(p))
                }
            };
            if let Some(p) = pick {
                used.insert(p);
                a.pos[i] = Some(p);
                a.method[i] = Method::Identity;
                a.identity[i] = true;
                progress = true;
            }
        }
        if !progress {
            break;
        }
    }
    for i in 0..n {
        if a.pos[i].is_none() && cands[i].iter().filter(|p| !used.contains(p)).count() > 1 {
            a.ambiguous[i] = true;
        }
    }
    a
}

/// Content of a pair of commits: the similarity, or why there is none.
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
    mask_bytes(text, RB_SUBJECT_CHARS)
}

/// Where the similarity (a gap of one between two mapped neighbours) and the content value come in. Fills the
/// positions in `a` and the values; returns (all values at least `RB_CONTENT_HIGH`, all at least `LOW_PCT`).
fn refine(
    repo: &gix::Repository,
    cache: &mut SimCache,
    index: &RebaseIndex,
    heads: &[Head],
    a: &mut Assigned,
    value: &mut [Option<u32>],
    lim: &Limits,
) -> Result<(bool, bool)> {
    let n = heads.len();
    // every value at least `RB_CONTENT_HIGH`; every value at least `LOW_PCT`
    let (mut high, mut low) = (true, true);
    // a commit between two mapped neighbours with exactly one free position between them: similarity decides
    for i in 1..n.saturating_sub(1) {
        let (Some(up), Some(down)) = (a.pos[i - 1], a.pos[i + 1]) else {
            continue;
        };
        if a.pos[i].is_some() || up != down + 2 {
            continue;
        }
        let q = down + 1;
        if index.positions[q as usize].parents > 1 || index.is_edge(q) || a.pos.contains(&Some(q)) {
            continue;
        }
        if let Content::Value(sim) = content_of(
            repo,
            cache,
            heads[i].id,
            index.positions[q as usize].id,
            lim,
        )? {
            if sim.ge_pct(LOW_PCT) {
                a.pos[i] = Some(q);
                a.method[i] = Method::Similarity;
                value[i] = Some(sim.shown(LOW_PCT));
                high &= sim.ge_pct(RB_CONTENT_HIGH);
            }
        }
    }
    // content value of the identity matches: same metadata with other content is no proof
    for i in 0..n {
        let (Some(p), Method::Identity) = (a.pos[i], a.method[i]) else {
            continue;
        };
        let content = if index.is_edge(p) {
            Content::Unknown
        } else {
            content_of(
                repo,
                cache,
                heads[i].id,
                index.positions[p as usize].id,
                lim,
            )?
        };
        match content {
            Content::Value(sim) => {
                value[i] = Some(sim.permille());
                high &= sim.ge_pct(RB_CONTENT_HIGH);
                low &= sim.ge_pct(LOW_PCT);
            }
            Content::BothEmpty => {}
            Content::Unknown => (high, low) = (false, false),
        }
    }
    Ok((high, low))
}

/// The commits without a target, with their files.
fn remaining_of(repo: &gix::Repository, mapping: &[MapEntry]) -> Result<Vec<Remaining>> {
    let mut remaining = Vec::new();
    for e in mapping.iter().filter(|e| e.main.is_none()) {
        let (files, files_total) = files_of(repo, e.commit)?;
        remaining.push(Remaining {
            commit: e.commit,
            subject: e.subject.clone(),
            files,
            files_total,
        });
    }
    Ok(remaining)
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
fn shape(mapped_at: &[(usize, u32)], n: usize, content_ok: bool, identity_backed: bool) -> Shape {
    let mapped = mapped_at.len();
    let in_order = mapped_at.windows(2).all(|w| w[0].1 > w[1].1);
    let (lo, hi) = (
        mapped_at.iter().map(|x| x.1).min(),
        mapped_at.iter().map(|x| x.1).max(),
    );
    let gap = match (lo, hi) {
        (Some(lo), Some(hi)) => ((hi - lo + 1) as usize).saturating_sub(mapped),
        _ => 0,
    };
    let contiguous = mapped > 0 && gap == 0;
    let (mut runs, mut run_len, mut cur) = (0, 0, 0);
    for (k, x) in mapped_at.iter().enumerate() {
        let linked = k > 0 && mapped_at[k - 1].0 + 1 == x.0 && mapped_at[k - 1].1 == x.1 + 1;
        if linked {
            cur += 1;
        } else {
            runs += 1;
            cur = 1;
        }
        run_len = run_len.max(cur);
    }
    let strength =
        if n >= 1 && mapped == n && contiguous && in_order && content_ok && identity_backed {
            Strength::Strong
        } else if mapped >= 1 {
            Strength::Hint
        } else {
            Strength::None
        };
    Shape {
        contiguous,
        in_order,
        gap,
        runs,
        run_len,
        partial: mapped > 0 && mapped < n && contiguous && in_order,
        strength,
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
    let (high, low) = refine(repo, cache, index, &heads, &mut a, &mut value, lim)?;
    // an identity match committed after the main commit is a later version than the landed one
    let later: Vec<bool> = (0..n)
        .map(|i| match (a.pos[i], a.method[i]) {
            (Some(p), Method::Identity) => {
                heads[i].committer_secs > index.positions[p as usize].committer_secs
            }
            _ => false,
        })
        .collect();

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
            later: later[i],
        });
    }
    let remaining = remaining_of(repo, &mapping)?;

    let mapped_at: Vec<(usize, u32)> = (0..n).filter_map(|i| a.pos[i].map(|p| (i, p))).collect();
    let mapped = mapped_at.len();
    let identity_backed = (0..n).any(|i| a.pos[i].is_some() && a.identity[i]);
    let content_ok = high && !later.iter().any(|x| *x);
    let sh = shape(&mapped_at, n, content_ok, identity_backed);
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
        content_ok,
        modified: !high && low,
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
    /// The request for the pull request commits failed (not a rate limit).
    RequestFailed,
    /// The pull request has a full page of commits or more: the list is not read.
    TooManyCommits,
    /// The branch has a proof that does not need the pull request (equal patches, merged head).
    NotNeeded,
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
            ApiReason::RequestFailed => "request-failed",
            ApiReason::TooManyCommits => "too-many-commits",
            ApiReason::NotNeeded => "not-needed",
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
    /// Merge and squash are strong. A rebase is strong when nothing remains and the PR head is in the branch.
    pub strength: Strength,
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
            strength: Strength::None,
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
    let pr = match input {
        ApiInput::NotRead(r) => return Ok(ApiReport::not_read(*r)),
        ApiInput::Pr(p) => p,
    };
    if !pr.merged {
        return Ok(ApiReport::not_read(ApiReason::NotMerged));
    }
    if repo.find_commit(pr.merge_commit).is_err() {
        return Ok(ApiReport::not_read(ApiReason::MergeCommitNotInClone));
    }
    // distance of the merge commit from the base tip on the first-parent chain
    let mut dist = 0usize;
    let mut cur = base_tip;
    while cur != pr.merge_commit {
        let next = repo
            .find_commit(cur)
            .ok()
            .and_then(|c| c.parent_ids().next());
        match next {
            Some(p) if dist < RB_MAX_WINDOW => {
                cur = p.detach();
                dist += 1;
            }
            _ => return Ok(ApiReport::not_read(ApiReason::NotOnBase)),
        }
    }
    let merge_head = read_head(repo, pr.merge_commit)?;
    let k = pr.commits.len();
    let mut rep = ApiReport {
        not_read: None,
        pr: Some(pr.number),
        merge_commit: Some(pr.merge_commit),
        merge_parents: Some(merge_head.parents),
        k: Some(k),
        tip_matches_run: None,
        head_in_branch: Some(crate::repo::is_ancestor(repo, pr.head, tip)?),
        kind: LandedKind::Unknown,
        strength: Strength::None,
        mapping: Vec::new(),
        remaining: Vec::new(),
    };
    if merge_head.parents == 2 {
        rep.kind = LandedKind::Merge;
        rep.strength = Strength::Strong;
        return Ok(rep);
    }
    if merge_head.parents != 1 {
        return Ok(rep);
    }
    let mut chain: Vec<Head> = Vec::new();
    let mut next = Some(pr.merge_commit);
    while let (Some(id), true) = (next, chain.len() < k) {
        match read_head(repo, id) {
            Ok(h) => chain.push(h),
            // behind a shallow boundary: the PR commits cannot be compared
            Err(_) if !chain.is_empty() => break,
            Err(e) => return Err(e),
        }
        next = repo
            .find_commit(id)?
            .parent_ids()
            .next()
            .map(|p| p.detach());
    }
    // chain[0] is the newest; the PR commit j (oldest first) is chain[k - 1 - j]
    let matches = k >= 1
        && chain.len() == k
        && (0..k).all(|j| same_identity(&chain[k - 1 - j], &pr.commits[j]));
    rep.tip_matches_run = Some(matches);
    if matches {
        rep.kind = LandedKind::Rebase;
        let ids: HashMap<ObjectId, usize> = pr
            .commits
            .iter()
            .enumerate()
            .map(|(j, c)| (c.id, j))
            .collect();
        for id in &side.commits {
            let h = read_head(repo, *id)?;
            let subject = show(&h.key.subject);
            match ids.get(id) {
                Some(j) => {
                    let m = &chain[k - 1 - j];
                    rep.mapping.push(MapEntry {
                        commit: *id,
                        subject,
                        main: Some(m.id),
                        position: Some((dist + k - 1 - j) as u32),
                        method: Method::Api,
                        value: None,
                        identity: true,
                        tz_differs: m.offset != h.offset,
                        ambiguous: false,
                        later: false,
                    });
                }
                None => {
                    let (files, files_total) = files_of(repo, *id)?;
                    rep.remaining.push(Remaining {
                        commit: *id,
                        subject,
                        files,
                        files_total,
                    });
                }
            }
        }
        rep.strength = if rep.remaining.is_empty() && rep.head_in_branch == Some(true) {
            Strength::Strong
        } else {
            Strength::Hint
        };
    } else if pr
        .merged_at
        .is_some_and(|t| (merge_head.key.secs - t).abs() <= RB_API_TIME_TOLERANCE)
    {
        rep.kind = LandedKind::Squash;
        rep.strength = Strength::Strong;
    }
    Ok(rep)
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
    let via = |kind, basis, strength| LandedVia {
        kind,
        basis,
        strength,
    };
    if ancestor {
        return via(LandedKind::Merge, Basis::Ancestor, Strength::Strong);
    }
    if rebase.is_some_and(|r| r.strength == Strength::Strong) {
        return via(LandedKind::Rebase, Basis::CommitMap, Strength::Strong);
    }
    if let Some(a) = api.filter(|a| a.not_read.is_none()) {
        if a.kind != LandedKind::Unknown {
            return via(a.kind, Basis::PrApi, a.strength);
        }
    }
    if rebase.is_some_and(|r| r.n == 1 && r.by_patch_id == 1 && r.mapped == 1) {
        return via(LandedKind::Unknown, Basis::SingleCommitMap, Strength::Hint);
    }
    via(LandedKind::Unknown, Basis::None, Strength::None)
}
