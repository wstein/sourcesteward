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
    /// `shown` for both thresholds of a run: a value below the low threshold is not shown as the low one either.
    pub fn shown_in(&self, high_pct: u32, low_pct: u32) -> u32 {
        let p = self.shown(high_pct);
        let t = low_pct * 10;
        if p >= t && !self.ge_pct(low_pct) {
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

// ---- candidates, evaluation, classes ----

use crate::github::reads::escape_untrusted;
use std::collections::{BTreeMap, HashMap};
use std::sync::Arc;

use gix::bstr::ByteSlice;

/// Author time and the first line of the message of a commit.
#[derive(Clone, Debug)]
pub struct CommitInfo {
    pub id: ObjectId,
    pub author_time: i64,
    pub subject: Vec<u8>,
}

pub fn commit_info(repo: &gix::Repository, id: ObjectId) -> Result<CommitInfo> {
    let c = repo.find_commit(id)?;
    let author_time = c.author()?.seconds();
    let msg = c.message_raw()?;
    let first = msg.lines().next().unwrap_or_default();
    let subject = trim_line(first).to_vec();
    Ok(CommitInfo {
        id,
        author_time,
        subject,
    })
}

/// The subject without a leading `type(scope):` (also `type!:` and `type(scope)!:`), and the white space after it.
/// A subject without such a prefix is returned unchanged.
pub fn strip_type_scope(subject: &[u8]) -> &[u8] {
    let mut i = 0;
    while i < subject.len()
        && (subject[i].is_ascii_lowercase() || subject[i].is_ascii_digit() || subject[i] == b'-')
    {
        i += 1;
    }
    if i == 0 {
        return subject;
    }
    if subject.get(i) == Some(&b'(') {
        match subject[i..].iter().position(|b| *b == b')') {
            Some(close) => i += close + 1,
            None => return subject,
        }
    }
    if subject.get(i) == Some(&b'!') {
        i += 1;
    }
    if subject.get(i) != Some(&b':') {
        return subject;
    }
    let rest = trim_line(&subject[i + 1..]);
    if rest.is_empty() {
        subject
    } else {
        rest
    }
}

/// A parent of `id` is not in the object database (the boundary of a shallow or partial clone).
fn has_missing_parent(repo: &gix::Repository, id: ObjectId) -> Result<bool> {
    let c = repo.find_commit(id)?;
    let missing = c.parent_ids().any(|p| !repo.has_object(p.detach()));
    Ok(missing)
}

/// The window rule: a candidate is at most `window_secs` older than the commit (author time); newer is fine.
pub fn in_window(commit_time: i64, candidate_time: i64, window_secs: i64) -> bool {
    candidate_time >= commit_time - window_secs
}

/// Results per commit, shared by all branches of a repository: keys and path sets are computed once.
#[derive(Default)]
pub struct SimCache {
    keys: HashMap<ObjectId, std::result::Result<Arc<Keys>, Skip>>,
    files: HashMap<ObjectId, Option<Arc<Vec<u64>>>>,
}

impl SimCache {
    /// The keys of `commit`, or why it has none.
    pub fn keys(
        &mut self,
        repo: &gix::Repository,
        commit: ObjectId,
        lim: &Limits,
    ) -> Result<std::result::Result<Arc<Keys>, Skip>> {
        if let Some(v) = self.keys.get(&commit) {
            return Ok(v.clone());
        }
        let v = match commit_entries(repo, commit, lim)? {
            Entries::Lines(e) => Ok(Arc::new(Keys::from_entries(&e))),
            Entries::Skipped(s) => Err(s),
        };
        self.keys.insert(commit, v.clone());
        Ok(v)
    }

    /// The changed paths of `commit` as sorted, distinct hashes; `None` for a merge.
    pub fn files(
        &mut self,
        repo: &gix::Repository,
        commit: ObjectId,
    ) -> Result<Option<Arc<Vec<u64>>>> {
        if let Some(v) = self.files.get(&commit) {
            return Ok(v.clone());
        }
        let c = repo.find_commit(commit)?;
        let parents: Vec<ObjectId> = c.parent_ids().map(|p| p.detach()).collect();
        let v = if parents.len() > 1 {
            None
        } else {
            let new_tree = c.tree_id()?.detach();
            let old_tree = match parents.first() {
                Some(p) => repo.find_commit(*p)?.tree_id()?.detach(),
                None => repo.empty_tree().id,
            };
            let mut h: Vec<u64> =
                changed_files(repo, &tree_of(repo, old_tree)?, &tree_of(repo, new_tree)?)?
                    .iter()
                    .map(|f| hash_key(None, 0, &f.path))
                    .collect();
            h.sort_unstable();
            h.dedup();
            Some(Arc::new(h))
        };
        self.files.insert(commit, v.clone());
        Ok(v)
    }
}

/// Why a main commit is a candidate; the first that applies, in this order.
#[derive(Clone, Copy, Debug, PartialEq, Eq, PartialOrd, Ord)]
pub enum Reason {
    Subject,
    SubjectStripped,
    Files,
}

impl Reason {
    pub fn as_str(self) -> &'static str {
        match self {
            Reason::Subject => "subject",
            Reason::SubjectStripped => "subject-stripped",
            Reason::Files => "files",
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Candidate {
    pub id: ObjectId,
    pub author_time: i64,
    pub reason: Reason,
}

struct MainCommit {
    info: CommitInfo,
    files: Arc<Vec<u64>>,
}

/// The commits of main (no merges) with indexes by subject, subject without prefix and changed file.
pub struct MainIndex {
    commits: Vec<MainCommit>,
    by_subject: HashMap<Vec<u8>, Vec<usize>>,
    by_stripped: HashMap<Vec<u8>, Vec<usize>>,
    by_file: HashMap<u64, Vec<usize>>,
    shallow: usize,
}

impl MainIndex {
    /// Index `ids` (merges are left out). Commits with an author time before `since` are left out: nothing older
    /// than the oldest `+` commit minus the window can be a candidate.
    pub fn build(
        repo: &gix::Repository,
        cache: &mut SimCache,
        ids: &[ObjectId],
        since: Option<i64>,
    ) -> Result<MainIndex> {
        let mut idx = MainIndex {
            commits: Vec::new(),
            by_subject: HashMap::new(),
            by_stripped: HashMap::new(),
            by_file: HashMap::new(),
            shallow: 0,
        };
        let is_shallow = repo.is_shallow().unwrap_or(false);
        for id in ids {
            let info = commit_info(repo, *id)?;
            if since.is_some_and(|t| info.author_time < t) {
                continue;
            }
            // the boundary of a shallow clone: its parent is not there, so it has no patch to read
            if is_shallow && has_missing_parent(repo, *id)? {
                idx.shallow += 1;
                continue;
            }
            let Some(files) = cache.files(repo, *id)? else {
                continue;
            };
            let n = idx.commits.len();
            idx.by_subject
                .entry(info.subject.clone())
                .or_default()
                .push(n);
            idx.by_stripped
                .entry(strip_type_scope(&info.subject).to_vec())
                .or_default()
                .push(n);
            for f in files.iter() {
                idx.by_file.entry(*f).or_default().push(n);
            }
            idx.commits.push(MainCommit { info, files });
        }
        Ok(idx)
    }

    /// One walk over everything reachable from `main_tip`.
    pub fn build_from_walk(
        repo: &gix::Repository,
        cache: &mut SimCache,
        main_tip: ObjectId,
        since: Option<i64>,
    ) -> Result<MainIndex> {
        let mut ids = Vec::new();
        for info in repo.rev_walk([main_tip]).all()? {
            ids.push(info?.id);
        }
        MainIndex::build(repo, cache, &ids, since)
    }

    pub fn len(&self) -> usize {
        self.commits.len()
    }

    /// Commits of main left out because their parent is not in the object database (a shallow clone).
    pub fn shallow_skipped(&self) -> usize {
        self.shallow
    }

    pub fn is_empty(&self) -> bool {
        self.commits.is_empty()
    }

    /// Candidates for `c` (whose changed paths are `files`): same subject, same subject without prefix, then at least
    /// half of the files in common (`2 * common >= max(|F(c)|, |F(m)|)`), all inside the window. Order: subject
    /// matches by commit ID, then file matches by common files (descending) and commit ID. At most
    /// `lim.max_candidates`; the flag says that more were cut. Search and window rule are separate from the comparison.
    pub fn candidates(
        &self,
        c: &CommitInfo,
        files: &[u64],
        lim: &Limits,
    ) -> (Vec<Candidate>, bool) {
        let ok = |n: usize| {
            in_window(
                c.author_time,
                self.commits[n].info.author_time,
                lim.window_secs,
            )
        };
        let cand = |n: usize, reason: Reason| Candidate {
            id: self.commits[n].info.id,
            author_time: self.commits[n].info.author_time,
            reason,
        };
        let mut seen: Vec<usize> = Vec::new();
        let mut out: Vec<Candidate> = Vec::new();
        let mut by_subject: Vec<Candidate> = Vec::new();
        for n in self.by_subject.get(&c.subject).into_iter().flatten() {
            if ok(*n) && !seen.contains(n) {
                seen.push(*n);
                by_subject.push(cand(*n, Reason::Subject));
            }
        }
        for n in self
            .by_stripped
            .get(strip_type_scope(&c.subject))
            .into_iter()
            .flatten()
        {
            if ok(*n) && !seen.contains(n) {
                seen.push(*n);
                by_subject.push(cand(*n, Reason::SubjectStripped));
            }
        }
        by_subject.sort_by_key(|x| (x.reason, x.id));
        out.extend(by_subject);
        let mut common: HashMap<usize, u64> = HashMap::new();
        for f in files {
            for n in self.by_file.get(f).into_iter().flatten() {
                *common.entry(*n).or_default() += 1;
            }
        }
        let mut by_files: Vec<(u64, Candidate)> = common
            .into_iter()
            .filter(|(n, k)| {
                ok(*n)
                    && !seen.contains(n)
                    && 2 * *k >= (files.len().max(self.commits[*n].files.len())) as u64
            })
            .map(|(n, k)| (k, cand(n, Reason::Files)))
            .collect();
        by_files.sort_by(|a, b| b.0.cmp(&a.0).then(a.1.id.cmp(&b.1.id)));
        out.extend(by_files.into_iter().map(|x| x.1));
        let cut = out.len() > lim.max_candidates;
        out.truncate(lim.max_candidates);
        (out, cut)
    }
}

/// One line of evidence: a key whose multiplicity differs most between the commit and its candidate.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Evidence {
    pub path: String,
    pub sign: char,
    pub line: String,
    pub count_a: u64,
    pub count_b: u64,
}

/// Text from a commit made safe for any output: bytes that are not UTF-8 as `\xNN`, then the project's escape rule
/// (`escape_untrusted`: control, invisible and direction characters), cut at `max_chars` characters with `…`.
pub fn mask_bytes(b: &[u8], max_chars: usize) -> String {
    let mut s = String::new();
    for chunk in b.utf8_chunks() {
        s.push_str(&escape_untrusted(chunk.valid(), usize::MAX).0);
        for byte in chunk.invalid() {
            s.push_str(&format!("\\x{byte:02x}"));
        }
    }
    if s.chars().count() > max_chars {
        let mut cut: String = s.chars().take(max_chars).collect();
        cut.push('\u{2026}');
        return cut;
    }
    s
}

/// The `max` keys with the biggest difference in multiplicity (ties: path, sign, line), `a` is the commit.
pub fn evidence(a: &[Entry], b: &[Entry], max: usize) -> Vec<Evidence> {
    type K = (Vec<u8>, u8, Vec<u8>);
    let mut m: BTreeMap<K, (u64, u64)> = BTreeMap::new();
    for e in a {
        m.entry((e.path.clone(), e.sign, e.line.clone()))
            .or_default()
            .0 += 1;
    }
    for e in b {
        m.entry((e.path.clone(), e.sign, e.line.clone()))
            .or_default()
            .1 += 1;
    }
    let mut v: Vec<(K, (u64, u64))> = m.into_iter().filter(|(_, (x, y))| x != y).collect();
    v.sort_by(|p, q| {
        let d = |c: &(u64, u64)| c.0.abs_diff(c.1);
        d(&q.1).cmp(&d(&p.1)).then(p.0.cmp(&q.0))
    });
    v.into_iter()
        .take(max)
        .map(|((path, sign, line), (count_a, count_b))| Evidence {
            path: mask_bytes(&path, 200),
            sign: sign as char,
            line: mask_bytes(&line, 80),
            count_a,
            count_b,
        })
        .collect()
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Compared {
    pub candidate: ObjectId,
    pub candidate_subject: String,
    pub reason: Reason,
    pub sim: Sim,
    /// The value without the path; shown, never part of the class.
    pub no_path: Sim,
    /// Only for a commit below the high threshold.
    pub evidence: Vec<Evidence>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Outcome {
    Skipped(Skip),
    NoCandidate,
    Compared(Compared),
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct CommitReport {
    pub commit: ObjectId,
    pub subject: String,
    pub outcome: Outcome,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Class {
    /// Every `+` commit is at or above the high threshold.
    Landed,
    /// Every `+` commit is at or above the low threshold, at least one below the high one.
    Modified,
    /// A commit without candidate, without value, or below the low threshold.
    Partial,
    /// No `+` commit to judge.
    None,
}

impl Class {
    pub fn as_str(self) -> &'static str {
        match self {
            Class::Landed => "landed",
            Class::Modified => "modified",
            Class::Partial => "partial",
            Class::None => "none",
        }
    }
}

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct SkippedCounts {
    pub merges: usize,
    pub empty: usize,
    pub too_large: usize,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct BranchReport {
    pub class: Class,
    /// Commits that were judged: all but merges and empty commits (a `too_large` commit counts).
    pub plus_commits: usize,
    pub at_or_above: usize,
    /// The lowest value (exact); None when no commit has one.
    pub min: Option<Sim>,
    /// Median of the shown values in tenths of a percent (two middle values: mean, rounded half up).
    pub median_permille: Option<u32>,
    pub without_candidate: usize,
    pub skipped: SkippedCounts,
    pub candidates_cut: bool,
    /// Sorted by commit ID.
    pub commits: Vec<CommitReport>,
}

/// Judge the `+` commits of one branch against main. `high` and `low` are percentages.
pub fn evaluate_branch(
    repo: &gix::Repository,
    cache: &mut SimCache,
    index: &MainIndex,
    plus: &[ObjectId],
    high: u32,
    low: u32,
    lim: &Limits,
) -> Result<BranchReport> {
    let mut ids: Vec<ObjectId> = plus.to_vec();
    ids.sort();
    ids.dedup();
    let mut commits = Vec::new();
    let mut skipped = SkippedCounts::default();
    let mut cut_any = false;
    for id in ids {
        let info = commit_info(repo, id)?;
        let subject = mask_bytes(&info.subject, 120);
        let keys = match cache.keys(repo, id, lim)? {
            Ok(k) => k,
            Err(s) => {
                match s {
                    Skip::Merge => skipped.merges += 1,
                    Skip::Empty => skipped.empty += 1,
                    Skip::TooLarge => skipped.too_large += 1,
                }
                commits.push(CommitReport {
                    commit: id,
                    subject,
                    outcome: Outcome::Skipped(s),
                });
                continue;
            }
        };
        let files = cache.files(repo, id)?.unwrap_or_default();
        let (cands, cut) = index.candidates(&info, &files, lim);
        cut_any |= cut;
        let mut best: Option<(Candidate, Sim, Sim)> = None;
        for cand in cands {
            let Ok(ck) = cache.keys(repo, cand.id, lim)? else {
                continue;
            };
            let sim = keys.similarity(&ck);
            let better = match &best {
                None => true,
                Some((b, bs, _)) => match sim.cmp_value(bs) {
                    Ordering::Greater => true,
                    Ordering::Equal => cand.id < b.id,
                    Ordering::Less => false,
                },
            };
            if better {
                best = Some((cand, sim, keys.similarity_no_path(&ck)));
            }
        }
        let outcome = match best {
            None => Outcome::NoCandidate,
            Some((cand, sim, no_path)) => {
                let ev = if sim.ge_pct(high) {
                    Vec::new()
                } else {
                    match (
                        commit_entries(repo, id, lim)?,
                        commit_entries(repo, cand.id, lim)?,
                    ) {
                        (Entries::Lines(a), Entries::Lines(b)) => {
                            evidence(&a, &b, lim.max_evidence)
                        }
                        _ => Vec::new(),
                    }
                };
                Outcome::Compared(Compared {
                    candidate: cand.id,
                    candidate_subject: mask_bytes(&commit_info(repo, cand.id)?.subject, 120),
                    reason: cand.reason,
                    sim,
                    no_path,
                    evidence: ev,
                })
            }
        };
        commits.push(CommitReport {
            commit: id,
            subject,
            outcome,
        });
    }
    Ok(summarize(commits, skipped, cut_any, high, low))
}

/// Classes and figures from the per-commit results.
pub fn summarize(
    commits: Vec<CommitReport>,
    skipped: SkippedCounts,
    candidates_cut: bool,
    high: u32,
    low: u32,
) -> BranchReport {
    let judged = commits
        .iter()
        .filter(|c| !matches!(c.outcome, Outcome::Skipped(Skip::Merge | Skip::Empty)))
        .count();
    let mut sims: Vec<Sim> = Vec::new();
    let mut without = 0;
    for c in &commits {
        match &c.outcome {
            Outcome::Compared(x) => sims.push(x.sim),
            Outcome::NoCandidate => without += 1,
            Outcome::Skipped(_) => {}
        }
    }
    let at_or_above = sims.iter().filter(|s| s.ge_pct(high)).count();
    let min = sims.iter().copied().min_by(|a, b| a.cmp_value(b));
    let mut shown: Vec<u32> = sims.iter().map(|s| s.shown_in(high, low)).collect();
    shown.sort_unstable();
    let median_permille = match shown.len() {
        0 => None,
        n if n % 2 == 1 => Some(shown[n / 2]),
        n => Some((shown[n / 2 - 1] + shown[n / 2]).div_ceil(2)),
    };
    let without_value = without + skipped.too_large;
    let class = if judged == 0 {
        Class::None
    } else if without_value > 0 || sims.iter().any(|s| !s.ge_pct(low)) {
        Class::Partial
    } else if at_or_above == sims.len() {
        Class::Landed
    } else {
        Class::Modified
    };
    BranchReport {
        class,
        plus_commits: judged,
        at_or_above,
        min,
        median_permille,
        without_candidate: without,
        skipped,
        candidates_cut,
        commits,
    }
}
