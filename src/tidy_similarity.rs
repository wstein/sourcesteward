//! Commit similarity for `stw tidy plan`; specification: `spikes/stw/tidy-similarity/README.md`.
//!
//! A commit is the multiset of the keys of its changed lines (`<path> NUL <sign> <trimmed line>`); two commits are
//! compared with Dice, `2 * |A ∩ B| / (|A| + |B|)`, in integers. Pure library functions: the `+` commits are an
//! input (this module does not decide what is merged), nothing here knows the plan.
//!
//! Building blocks, each usable alone: `Keys` (multiset, `sum`, `similarity`), `commit_keys` and `tree_keys` (the keys of
//! a commit or of a cumulative diff `base...tip`), `MainIndex` (what is on main) with `candidates` (search) and the
//! window rule (`in_window`), `evaluate_branch` (best candidate, classes and figures).
use std::cmp::Ordering;
use std::hash::{DefaultHasher, Hasher};

/// Intersection size and the sizes of both multisets: the integers every decision is made from.
/// The measure is Dice, `2 * inter / (a + b)`; containment (`inter / a`, `inter / b`) is given as numerator and denominator.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Sim {
    pub inter: u64,
    pub a: u64,
    pub b: u64,
}

impl Sim {
    fn total(&self) -> u64 {
        self.a + self.b
    }
    /// Dice at or above `pct` percent: `200 * inter >= pct * (a + b)` (no floating point).
    pub fn ge_pct(&self, pct: u32) -> bool {
        200 * self.inter >= pct as u64 * self.total()
    }
    /// Dice in tenths of a percent, rounded half up (`0` for two empty sets).
    pub fn permille(&self) -> u32 {
        let s = self.total();
        if s == 0 {
            return 0;
        }
        ((4000 * self.inter + s) / (2 * s)) as u32
    }
    /// The value to show next to a decision at `threshold_pct`: like `permille`, but a value below the threshold
    /// whose rounded form reaches it is shown one tenth lower, so that display and class never disagree.
    pub fn shown(&self, threshold_pct: u32) -> u32 {
        let p = self.permille();
        let t = threshold_pct * 10;
        if p >= t && !self.ge_pct(threshold_pct) {
            t - 1
        } else {
            p
        }
    }
    /// Order by the exact value (cross multiplication), not by the rounded one.
    pub fn cmp_value(&self, other: &Sim) -> Ordering {
        // inter/total compared as inter*other.total against other.inter*total; empty sets count as 0
        let l = self.inter as u128 * other.total() as u128;
        let r = other.inter as u128 * self.total() as u128;
        match (self.total() == 0, other.total() == 0) {
            (true, true) => Ordering::Equal,
            (true, false) => Ordering::Less,
            (false, true) => Ordering::Greater,
            _ => l.cmp(&r),
        }
    }
    /// Share of the first set that is in the second: (numerator, denominator).
    pub fn containment_in_a(&self) -> (u64, u64) {
        (self.inter, self.a)
    }
    /// Share of the second set that is in the first: (numerator, denominator).
    pub fn containment_in_b(&self) -> (u64, u64) {
        (self.inter, self.b)
    }
}

/// `83.3` for 833.
pub fn fmt_permille(p: u32) -> String {
    format!("{}.{}", p / 10, p % 10)
}

/// One changed line of a commit: the path, the sign (`+`, `-`, or a marker for binary, mode, empty files) and the line.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Entry {
    pub path: Vec<u8>,
    pub sign: u8,
    pub line: Vec<u8>,
}

/// ASCII white space (space, tab, CR, LF, VT, FF) removed at both ends.
pub fn trim_line(raw: &[u8]) -> &[u8] {
    let ws = |b: &u8| matches!(b, b' ' | b'\t' | b'\r' | b'\n' | 0x0b | 0x0c);
    let start = raw.iter().position(|b| !ws(b)).unwrap_or(raw.len());
    let end = raw.iter().rposition(|b| !ws(b)).map_or(start, |i| i + 1);
    &raw[start..end]
}

fn hash_key(path: Option<&[u8]>, sign: u8, line: &[u8]) -> u64 {
    // SipHash with fixed keys: the same value in every run
    let mut h = DefaultHasher::new();
    if let Some(p) = path {
        h.write(p);
        h.write_u8(0);
    }
    h.write_u8(sign);
    h.write(line);
    h.finish()
}

/// Sorted multisets of 64-bit hashes of the keys, with and without the path.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Keys {
    with_path: Vec<u64>,
    no_path: Vec<u64>,
}

/// Size of the multiset intersection of two sorted vectors (sum of the smaller multiplicity per value).
fn intersect(a: &[u64], b: &[u64]) -> u64 {
    let (mut i, mut j, mut n) = (0, 0, 0u64);
    while i < a.len() && j < b.len() {
        match a[i].cmp(&b[j]) {
            Ordering::Less => i += 1,
            Ordering::Greater => j += 1,
            Ordering::Equal => {
                n += 1;
                i += 1;
                j += 1;
            }
        }
    }
    n
}

fn merged(a: &[u64], b: &[u64]) -> Vec<u64> {
    let mut v = Vec::with_capacity(a.len() + b.len());
    v.extend_from_slice(a);
    v.extend_from_slice(b);
    v.sort_unstable();
    v
}

impl Keys {
    pub fn from_entries(entries: &[Entry]) -> Keys {
        let mut with_path: Vec<u64> = entries
            .iter()
            .map(|e| hash_key(Some(&e.path), e.sign, &e.line))
            .collect();
        let mut no_path: Vec<u64> = entries
            .iter()
            .map(|e| hash_key(None, e.sign, &e.line))
            .collect();
        with_path.sort_unstable();
        no_path.sort_unstable();
        Keys { with_path, no_path }
    }
    pub fn len(&self) -> usize {
        self.with_path.len()
    }
    pub fn is_empty(&self) -> bool {
        self.with_path.is_empty()
    }
    pub fn similarity(&self, other: &Keys) -> Sim {
        Sim {
            inter: intersect(&self.with_path, &other.with_path),
            a: self.with_path.len() as u64,
            b: other.with_path.len() as u64,
        }
    }
    pub fn similarity_no_path(&self, other: &Keys) -> Sim {
        Sim {
            inter: intersect(&self.no_path, &other.no_path),
            a: self.no_path.len() as u64,
            b: other.no_path.len() as u64,
        }
    }
    /// The multiset of several commits together (a squash is the sum of its parts).
    pub fn sum(&self, other: &Keys) -> Keys {
        Keys {
            with_path: merged(&self.with_path, &other.with_path),
            no_path: merged(&self.no_path, &other.no_path),
        }
    }
}

use crate::error::Result;
use crate::patch_id::{changed_files, content, is_binary, mode_str};
use gix::diff::blob::{Algorithm, Diff, InternedInput};
use gix::objs::tree::EntryKind;
use gix::ObjectId;

/// Why a commit has no value.
#[derive(Clone, Copy, Debug, PartialEq, Eq, PartialOrd, Ord)]
pub enum Skip {
    /// More than one parent: no patch.
    Merge,
    /// The tree equals the parent's tree.
    Empty,
    /// More keys or a bigger blob than `Limits` allow.
    TooLarge,
}

impl Skip {
    pub fn as_str(self) -> &'static str {
        match self {
            Skip::Merge => "merge",
            Skip::Empty => "empty",
            Skip::TooLarge => "too-large",
        }
    }
}

/// Bounds and constants of one run.
#[derive(Clone, Debug)]
pub struct Limits {
    /// A candidate may be at most this many seconds older than the commit (author time).
    pub window_secs: i64,
    /// A commit with more keys than this is `TooLarge`.
    pub max_keys: usize,
    /// A blob bigger than this (either side) makes the commit `TooLarge`.
    pub max_blob_bytes: u64,
    /// At most this many candidates are compared per commit.
    pub max_candidates: usize,
    /// At most this many evidence lines per commit below the high threshold.
    pub max_evidence: usize,
}

pub const WINDOW_DAYS: i64 = 7;
pub const HIGH_PCT: u32 = 95;
pub const LOW_PCT: u32 = 70;

impl Default for Limits {
    fn default() -> Self {
        Limits {
            window_secs: WINDOW_DAYS * 86_400,
            max_keys: 20_000,
            max_blob_bytes: 1 << 20,
            max_candidates: 100,
            max_evidence: 5,
        }
    }
}

/// The changed lines of a commit or of a tree diff, or the reason there are none.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Entries {
    Lines(Vec<Entry>),
    Skipped(Skip),
}

/// The entries of `commit` against its first parent.
pub fn commit_entries(repo: &gix::Repository, commit: ObjectId, lim: &Limits) -> Result<Entries> {
    let c = repo.find_commit(commit)?;
    let parents: Vec<ObjectId> = c.parent_ids().map(|p| p.detach()).collect();
    if parents.len() > 1 {
        return Ok(Entries::Skipped(Skip::Merge));
    }
    let new_tree = c.tree_id()?.detach();
    let old_tree = match parents.first() {
        Some(p) => repo.find_commit(*p)?.tree_id()?.detach(),
        None => repo.empty_tree().id,
    };
    if old_tree == new_tree {
        return Ok(Entries::Skipped(Skip::Empty));
    }
    tree_entries(repo, old_tree, new_tree, lim)
}

fn tree_of(repo: &gix::Repository, id: ObjectId) -> Result<gix::Tree<'_>> {
    if id == repo.empty_tree().id {
        return Ok(repo.empty_tree());
    }
    Ok(repo.find_tree(id)?)
}

/// The entries of the difference between two trees (the cumulative diff `base...tip` is `tree_entries(base tree, tip tree)`).
///
/// Same tree diff as the patch ID (`patch_id::changed_files`, no rename detection). Hunks come from gix-imara-diff
/// (Myers, no slider heuristic), not from `git diff -U0`: where git (xdiff, indent heuristic) picks another minimal diff,
/// the multiset can differ; a hunk that only moves over equal lines does not change it.
pub fn tree_entries(
    repo: &gix::Repository,
    old: ObjectId,
    new: ObjectId,
    lim: &Limits,
) -> Result<Entries> {
    let (old_tree, new_tree) = (tree_of(repo, old)?, tree_of(repo, new)?);
    let mut out: Vec<Entry> = Vec::new();
    for f in changed_files(repo, &old_tree, &new_tree)? {
        let path = f.path.to_vec();
        let plain = |sign: u8, line: Vec<u8>| Entry {
            path: path.clone(),
            sign,
            line,
        };
        if let (Some((ko, _)), Some((kn, _))) = (f.old, f.new) {
            if ko != kn {
                out.push(plain(
                    b'M',
                    format!("{}>{}", mode_str(ko), mode_str(kn)).into_bytes(),
                ));
            }
        }
        for (kind, id) in [f.old, f.new].into_iter().flatten() {
            if kind != EntryKind::Commit && repo.find_header(id)?.size() > lim.max_blob_bytes {
                return Ok(Entries::Skipped(Skip::TooLarge));
            }
        }
        let before = match f.old {
            Some((k, id)) => content(repo, k, id)?,
            None => Vec::new(),
        };
        let after = match f.new {
            Some((k, id)) => content(repo, k, id)?,
            None => Vec::new(),
        };
        if before == after {
            // only the mode changed, or an empty file was added or deleted
            if f.old.is_none() {
                out.push(plain(b'N', Vec::new()));
            } else if f.new.is_none() {
                out.push(plain(b'X', Vec::new()));
            }
            continue;
        }
        if is_binary(&before) || is_binary(&after) {
            let line = match (f.new, f.old) {
                (Some((_, id)), _) => format!("bin {id}"),
                (None, Some((_, id))) => format!("bin-deleted {id}"),
                (None, None) => String::new(),
            };
            out.push(plain(b'B', line.into_bytes()));
        } else {
            push_hunk_lines(&mut out, &path, &before, &after);
        }
        if out.len() > lim.max_keys {
            return Ok(Entries::Skipped(Skip::TooLarge));
        }
    }
    if out.is_empty() {
        return Ok(Entries::Skipped(Skip::Empty));
    }
    Ok(Entries::Lines(out))
}

/// `-` lines of the old side and `+` lines of the new side of every hunk (context 0), trimmed.
fn push_hunk_lines(out: &mut Vec<Entry>, path: &[u8], before: &[u8], after: &[u8]) {
    let input = InternedInput::new(before, after);
    let mut diff = Diff::compute(Algorithm::Myers, &input);
    diff.postprocess_no_heuristic(&input);
    for h in diff.hunks() {
        let sides = [
            (
                b'-',
                &input.before[h.before.start as usize..h.before.end as usize],
            ),
            (
                b'+',
                &input.after[h.after.start as usize..h.after.end as usize],
            ),
        ];
        for (sign, tokens) in sides {
            for t in tokens {
                out.push(Entry {
                    path: path.to_vec(),
                    sign,
                    line: trim_line(input.interner[*t]).to_vec(),
                });
            }
        }
    }
}
