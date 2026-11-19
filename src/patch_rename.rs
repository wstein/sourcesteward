//! Rename detection as `git diff`, `git show` and `git log -p` do it with their defaults (`diff.renames=true`:
//! renames on, copies off, similarity threshold 50 %, `diff.renameLimit` 1000), so that the patch ID of a commit
//! with a rename is the one `git patch-id --stable` computes on `git diff` output. Sources: git's
//! `diffcore-rename.c` (exact renames, then the similarity matrix) and `diffcore-delta.c` (the similarity
//! estimate); both are re-implemented here from their documented behavior and checked against git with
//! `tests/oracle/patch_id_rename_oracle.sh` and `tests/it/patchid_git.rs`.
//!
//! The user's configuration is not read: the result must not depend on it, a patch ID that is printed in a
//! review line has to be the same on every machine. Git itself changes its output with `diff.renames=false`
//! (no renames) or `diff.renames=copies` (copies too), with `diff.renameLimit`, and with `-M<n>`; the
//! patch ID here is the one of git's built-in defaults.
//!
//! Deviations from git (all in rare cases; named in the README):
//! * equal scores in the final order are broken by destination and source order; git's qsort order is not
//!   specified and differs between C libraries (two identical sources competing for two destinations of
//!   equal similarity can be paired the other way round);
//! * git's `diff.renames=copies`, `-C`, `-B` and `diff.renameLimit` are not supported (fixed defaults);
//! * `.gitattributes` (`binary`, `-diff`, `diff=<driver>`) is not read, as for the whole patch ID.
use std::collections::{BTreeMap, HashMap};

use gix::bstr::BString;
use gix::objs::tree::EntryKind;
use gix::ObjectId;

use crate::error::Result;
use crate::patch_id::{content, is_binary, FileChange, Pair};

/// git's `MAX_SCORE`: a score is `copied bytes * MAX_SCORE / larger size`.
const MAX_SCORE: u64 = 60_000;
/// git's `DEFAULT_RENAME_SCORE`: 50 %.
const MINIMUM_SCORE: u64 = 30_000;
/// The base-name pass needs more than the matrix does: git's `min_basename_score` is
/// `minimum_score + GIT_BASENAME_FACTOR * (MAX_SCORE - minimum_score)` with a factor of 0.5, that is
/// 30000 + 0.5 * 30000 = 45000 (75 %). A base-name pair below it is left to the matrix.
const MINIMUM_BASENAME_SCORE: u64 = MINIMUM_SCORE + (MAX_SCORE - MINIMUM_SCORE) / 2;
/// git's default `diff.renameLimit`: inexact detection is skipped when `sources * destinations` is larger
/// than the square of it (and one of them is larger than the limit).
const RENAME_LIMIT: u64 = 1000;
/// git's `NUM_CANDIDATE_PER_DST`.
const CANDIDATES_PER_DST: usize = 4;
/// git's `HASHBASE` in `diffcore-delta.c`.
const HASHBASE: u32 = 107_927;

fn is_regular(kind: EntryKind) -> bool {
    matches!(kind, EntryKind::Blob | EntryKind::BlobExecutable)
}

fn basename(path: &BString) -> &[u8] {
    path.rsplit(|b| *b == b'/').next().unwrap_or(path)
}

/// The byte counts of the lines of a file, keyed by a hash of the line (git's `hash_chars`): a "line" ends at
/// `\n` or after 64 bytes; in text a `\r` before `\n` is not counted.
fn span_hashes(data: &[u8]) -> BTreeMap<u32, u64> {
    let text = !is_binary(data);
    let mut spans: BTreeMap<u32, u64> = BTreeMap::new();
    let (mut accum1, mut accum2, mut n) = (0u32, 0u32, 0u64);
    for (i, &c) in data.iter().enumerate() {
        if text && c == b'\r' && data.get(i + 1) == Some(&b'\n') {
            continue;
        }
        let old_1 = accum1;
        accum1 = (accum1 << 7) ^ (accum2 >> 25);
        accum2 = (accum2 << 7) ^ (old_1 >> 25);
        accum1 = accum1.wrapping_add(c as u32);
        n += 1;
        if n < 64 && c != b'\n' {
            continue;
        }
        let hash = accum1.wrapping_add(accum2.wrapping_mul(0x61)) % HASHBASE;
        *spans.entry(hash).or_insert(0) += n;
        n = 0;
        accum1 = 0;
        accum2 = 0;
    }
    if n > 0 {
        let hash = accum1.wrapping_add(accum2.wrapping_mul(0x61)) % HASHBASE;
        *spans.entry(hash).or_insert(0) += n;
    }
    spans
}

struct Blob {
    size: u64,
    spans: BTreeMap<u32, u64>,
}

/// Contents and span hashes by blob id, read when first needed.
struct Blobs<'a> {
    repo: &'a gix::Repository,
    by_id: HashMap<ObjectId, Blob>,
}

impl Blobs<'_> {
    fn load(&mut self, kind: EntryKind, id: ObjectId) -> Result<()> {
        if !self.by_id.contains_key(&id) {
            let data = content(self.repo, kind, id)?;
            self.by_id.insert(
                id,
                Blob {
                    size: data.len() as u64,
                    spans: span_hashes(&data),
                },
            );
        }
        Ok(())
    }
}

/// git's `estimate_similarity` for two regular files: the share of the larger file that the smaller one has
/// in common, in 1/60000, 0 when the sizes alone rule out 50 %.
fn estimate(
    blobs: &mut Blobs,
    src: (EntryKind, ObjectId),
    dst: (EntryKind, ObjectId),
) -> Result<u64> {
    if !is_regular(src.0) || !is_regular(dst.0) {
        return Ok(0);
    }
    blobs.load(src.0, src.1)?;
    blobs.load(dst.0, dst.1)?;
    let (src_size, dst_size) = (blobs.by_id[&src.1].size, blobs.by_id[&dst.1].size);
    let max_size = src_size.max(dst_size);
    let delta_size = max_size - src_size.min(dst_size);
    if max_size * (MAX_SCORE - MINIMUM_SCORE) < delta_size * MAX_SCORE {
        return Ok(0);
    }
    if dst_size == 0 {
        return Ok(0);
    }
    let src_spans = &blobs.by_id[&src.1].spans;
    let dst_spans = &blobs.by_id[&dst.1].spans;
    let copied: u64 = src_spans
        .iter()
        .map(|(hash, cnt)| dst_spans.get(hash).map_or(0, |d| (*cnt).min(*d)))
        .sum();
    Ok(copied * MAX_SCORE / max_size)
}

struct Candidate {
    score: u64,
    same_name: bool,
    dst: usize,
    src: usize,
}

/// Pairs the deletions and additions of `files` (sorted by path, no renames) the way `git diff` does and
/// returns the entries in the order of `files`: a rename takes the place of its destination, its source
/// is gone; a change of the file type is a deletion plus an addition (`Pair::of`).
pub(crate) fn pair_renames(repo: &gix::Repository, files: Vec<FileChange>) -> Result<Vec<Pair>> {
    let deleted = |f: &FileChange| f.old.is_some() && f.new.is_none();
    let added = |f: &FileChange| f.old.is_none() && f.new.is_some();
    let n = files.len();
    // destination index -> (source index, similarity in percent)
    let mut renamed: Vec<Option<(usize, u64)>> = vec![None; n];
    let mut src_used = vec![false; n];

    // exact renames: same blob id; of several candidates the one with the same base name, else the first
    let mut by_id: HashMap<ObjectId, Vec<usize>> = HashMap::new();
    for (i, f) in files.iter().enumerate().filter(|(_, f)| deleted(f)) {
        by_id.entry(f.old.unwrap().1).or_default().push(i);
    }
    for d in (0..n).filter(|i| added(&files[*i])) {
        let (dkind, did) = files[d].new.unwrap();
        let mut best: Option<(usize, bool)> = None;
        for &s in by_id.get(&did).into_iter().flatten() {
            let skind = files[s].old.unwrap().0;
            if src_used[s] || (skind == EntryKind::Commit) != (dkind == EntryKind::Commit) {
                continue;
            }
            let same = basename(&files[s].path) == basename(&files[d].path);
            if best.is_none_or(|(_, b)| same && !b) {
                best = Some((s, same));
            }
        }
        if let Some((s, _)) = best {
            src_used[s] = true;
            renamed[d] = Some((s, 100));
        }
    }

    // basename-guided pass (git's `find_basename_matches`): a source and a destination whose base name occurs
    // once among the sources and once among the destinations left are scored first; 75 % makes a rename
    let mut blobs = Blobs {
        repo,
        by_id: HashMap::new(),
    };
    let unique = |idx: Vec<usize>| -> HashMap<Vec<u8>, Option<usize>> {
        let mut m: HashMap<Vec<u8>, Option<usize>> = HashMap::new();
        for i in idx {
            m.entry(basename(&files[i].path).to_vec())
                .and_modify(|e| *e = None)
                .or_insert(Some(i));
        }
        m
    };
    let src_names = unique(
        (0..n)
            .filter(|i| deleted(&files[*i]) && !src_used[*i])
            .collect(),
    );
    let dst_names = unique(
        (0..n)
            .filter(|i| added(&files[*i]) && renamed[*i].is_none())
            .collect(),
    );
    for (name, s) in &src_names {
        let (Some(s), Some(Some(d))) = (*s, dst_names.get(name)) else {
            continue;
        };
        let score = estimate(&mut blobs, files[s].old.unwrap(), files[*d].new.unwrap())?;
        if score >= MINIMUM_BASENAME_SCORE {
            src_used[s] = true;
            renamed[*d] = Some((s, score * 100 / MAX_SCORE));
        }
    }

    // inexact renames: the similarity matrix of what is left
    let srcs: Vec<usize> = (0..n)
        .filter(|i| deleted(&files[*i]) && !src_used[*i])
        .collect();
    let dsts: Vec<usize> = (0..n)
        .filter(|i| added(&files[*i]) && renamed[*i].is_none())
        .collect();
    let (ns, nd) = (srcs.len() as u64, dsts.len() as u64);
    let too_many =
        (nd > RENAME_LIMIT || ns > RENAME_LIMIT) && nd * ns > RENAME_LIMIT * RENAME_LIMIT;
    if !srcs.is_empty() && !dsts.is_empty() && !too_many {
        let mut all: Vec<Candidate> = Vec::new();
        for &d in &dsts {
            let dst = files[d].new.unwrap();
            let mut mine: Vec<Candidate> = Vec::new();
            for &s in &srcs {
                let score = estimate(&mut blobs, files[s].old.unwrap(), dst)?;
                if score >= MINIMUM_SCORE {
                    mine.push(Candidate {
                        score,
                        same_name: basename(&files[s].path) == basename(&files[d].path),
                        dst: d,
                        src: s,
                    });
                }
            }
            mine.sort_by_key(|c| std::cmp::Reverse((c.score, c.same_name)));
            mine.truncate(CANDIDATES_PER_DST);
            all.extend(mine);
        }
        all.sort_by_key(|c| std::cmp::Reverse((c.score, c.same_name)));
        for c in all {
            if renamed[c.dst].is_none() && !src_used[c.src] {
                src_used[c.src] = true;
                renamed[c.dst] = Some((c.src, c.score * 100 / MAX_SCORE));
            }
        }
    }

    let mut out = Vec::new();
    for (i, f) in files.iter().enumerate() {
        if src_used[i] && deleted(f) {
            continue;
        }
        match renamed[i] {
            Some((s, percent)) => out.push(Pair {
                old_path: files[s].path.clone(),
                new_path: f.path.clone(),
                old: files[s].old,
                new: f.new,
                similarity: Some(percent),
            }),
            None => out.extend(Pair::of(f)),
        }
    }
    Ok(out)
}
